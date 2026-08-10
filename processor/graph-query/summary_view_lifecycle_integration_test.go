//go:build integration

package graphquery

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type recordingSummaryReader struct {
	graph.CatalogReader
	watchers chan<- jetstream.KeyWatcher
}

func (r *recordingSummaryReader) WatchAll(
	ctx context.Context,
	opts ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	watcher, err := r.CatalogReader.WatchAll(ctx, opts...)
	if err != nil {
		return nil, err
	}
	select {
	case r.watchers <- watcher:
		return watcher, nil
	case <-ctx.Done():
		_ = watcher.Stop()
		return nil, ctx.Err()
	}
}

func TestIntegration_SummaryViewLossReopensCatalogWithoutGhost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	summaryKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketCommunitySummaries,
	})
	require.NoError(t, err)
	comm := themedCommunity()
	hash := clustering.MembershipHash(comm.Members)
	key := clustering.SummaryKey(comm.Level, hash)
	put := func(summary string) {
		t.Helper()
		_, putErr := summaryKV.Put(ctx, key, mustSummaryJSON(t, clustering.CommunitySummaryRecord{
			MembershipHash: hash,
			Level:          comm.Level,
			LLMSummary:     summary,
			Status:         clustering.SummaryStatusEnhanced,
		}))
		require.NoError(t, putErr)
	}
	put("initial enhanced")

	watchers := make(chan jetstream.KeyWatcher, 2)
	changed := make(chan *graphview.View[clustering.CommunitySummaryRecord], 4)
	applied := make(chan uint64, 16)
	stopped := make(chan *graphview.View[clustering.CommunitySummaryRecord], 2)
	retryEntered := make(chan struct{}, 1)
	allowRetry := make(chan struct{}, 1)
	openCount := 0
	component := &Component{
		config: Config{RecheckInterval: time.Hour},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		openSummaryReader: func(openCtx context.Context) (graph.CatalogReader, error) {
			openCount++
			reader, openErr := graph.OpenCatalogReader(openCtx, natsClient, graph.BucketCommunitySummaries)
			if openErr != nil {
				return nil, openErr
			}
			return &recordingSummaryReader{CatalogReader: reader, watchers: watchers}, nil
		},
		waitSummaryRetry: func(waitCtx context.Context, _ time.Duration) bool {
			retryEntered <- struct{}{}
			select {
			case <-waitCtx.Done():
				return false
			case <-allowRetry:
				return true
			}
		},
		summaryViewChanged: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			changed <- view
		},
		summaryViewApplied: func(_ string, revision uint64) { applied <- revision },
		summaryViewStopped: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			stopped <- view
		},
	}
	done := make(chan struct{})
	go func() {
		component.superviseSummaryView(ctx)
		close(done)
	}()

	firstView := receiveSummaryEvent(t, changed)
	firstWatcher := receiveSummaryEvent(t, watchers)
	receiveSummaryEvent(t, applied)
	waitSummaryCaughtUp(t, firstView)
	require.Equal(t, "initial enhanced", component.resolveCommunitySummary(comm))

	put("updated enhanced")
	receiveSummaryEvent(t, applied)
	require.Equal(t, "updated enhanced", component.resolveCommunitySummary(comm))
	require.NoError(t, summaryKV.Delete(ctx, key))
	receiveSummaryEvent(t, applied)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm))
	put("restored after delete")
	receiveSummaryEvent(t, applied)
	require.NoError(t, summaryKV.Purge(ctx, key))
	receiveSummaryEvent(t, applied)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm))
	put("restored before poison")
	receiveSummaryEvent(t, applied)
	_, err = summaryKV.Put(ctx, key, []byte("{"))
	require.NoError(t, err)
	receiveSummaryEvent(t, applied)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm))
	put("value before loss")
	receiveSummaryEvent(t, applied)
	require.Equal(t, "value before loss", component.resolveCommunitySummary(comm))

	require.NoError(t, firstWatcher.Stop())
	require.Nil(t, receiveSummaryEvent(t, changed))
	require.Same(t, firstView, receiveSummaryEvent(t, stopped))
	receiveSummaryEvent(t, retryEntered)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "loss fails closed")

	// Delete while no view exists. The replacement must reopen the catalog and
	// replay current state instead of inheriting the stopped view's projection.
	require.NoError(t, summaryKV.Delete(ctx, key))
	allowRetry <- struct{}{}
	secondView := receiveSummaryEvent(t, changed)
	receiveSummaryEvent(t, watchers)
	waitSummaryCaughtUp(t, secondView)
	require.Equal(t, 2, openCount)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "fresh replay cannot retain the ghost")

	cancel()
	require.Nil(t, receiveSummaryEvent(t, changed))
	require.Same(t, secondView, receiveSummaryEvent(t, stopped))
	receiveSummaryEvent(t, done)
}
