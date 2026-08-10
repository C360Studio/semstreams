package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const summaryViewTestTimeout = 5 * time.Second

type controlledSummaryWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped chan struct{}
	once    sync.Once
}

func newControlledSummaryWatcher() *controlledSummaryWatcher {
	return &controlledSummaryWatcher{
		updates: make(chan jetstream.KeyValueEntry, 16),
		stopped: make(chan struct{}),
	}
}

func (w *controlledSummaryWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *controlledSummaryWatcher) Stop() error {
	w.once.Do(func() { close(w.stopped) })
	return nil
}

type controlledSummaryReader struct {
	graph.CatalogReader
	watcher *controlledSummaryWatcher
	err     error
	called  chan struct{}
	once    sync.Once
}

type blockingSummaryReader struct {
	graph.CatalogReader
	called chan struct{}
	once   sync.Once
}

func (r *blockingSummaryReader) WatchAll(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	r.once.Do(func() { close(r.called) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *controlledSummaryReader) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	r.once.Do(func() { close(r.called) })
	if r.err != nil {
		return nil, r.err
	}
	return r.watcher, nil
}

type summaryViewEntry struct {
	key       string
	value     []byte
	revision  uint64
	operation jetstream.KeyValueOp
}

type blockingPoisonLogHandler struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingPoisonLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *blockingPoisonLogHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "COMMUNITY_SUMMARIES record poisoned" {
		return nil
	}
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}

func (h *blockingPoisonLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockingPoisonLogHandler) WithGroup(string) slog.Handler      { return h }

func (e summaryViewEntry) Bucket() string                  { return graph.BucketCommunitySummaries }
func (e summaryViewEntry) Key() string                     { return e.key }
func (e summaryViewEntry) Value() []byte                   { return e.value }
func (e summaryViewEntry) Revision() uint64                { return e.revision }
func (e summaryViewEntry) Created() time.Time              { return time.Time{} }
func (e summaryViewEntry) Delta() uint64                   { return 0 }
func (e summaryViewEntry) Operation() jetstream.KeyValueOp { return e.operation }

func TestDecodeCommunitySummaryRecord(t *testing.T) {
	t.Parallel()

	hash := strings.Repeat("a", 64)
	valid := clustering.CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          2,
		LLMSummary:     "enhanced summary",
		Status:         clustering.SummaryStatusEnhanced,
	}
	validJSON, err := json.Marshal(valid)
	require.NoError(t, err)

	tests := []struct {
		name  string
		key   string
		value []byte
		keep  bool
		want  string
	}{
		{name: "enhanced", key: clustering.SummaryKey(2, hash), value: validJSON, keep: true, want: valid.LLMSummary},
		{name: "failed is absence", key: clustering.SummaryKey(2, hash), value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{MembershipHash: hash, Level: 2, Status: clustering.SummaryStatusFailed})},
		{name: "unknown fields tolerated", key: clustering.SummaryKey(2, hash), value: append(validJSON[:len(validJSON)-1], []byte(`,"future":true}`)...), keep: true, want: valid.LLMSummary},
		{name: "negative level", key: "-1." + hash, value: validJSON},
		{name: "noncanonical level", key: "02." + hash, value: validJSON},
		{name: "extra dot", key: "2." + hash + ".extra", value: validJSON},
		{name: "short hash", key: "2.abc", value: validJSON},
		{name: "uppercase hash", key: "2." + strings.Repeat("A", 64), value: validJSON},
		{name: "invalid json", key: clustering.SummaryKey(2, hash), value: []byte("{")},
		{name: "record hash malformed", key: clustering.SummaryKey(2, hash), value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{MembershipHash: "bad", Level: 2, LLMSummary: "x", Status: clustering.SummaryStatusEnhanced})},
		{name: "key record mismatch", key: clustering.SummaryKey(2, hash), value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{MembershipHash: hash, Level: 3, LLMSummary: "x", Status: clustering.SummaryStatusEnhanced})},
		{name: "unknown status", key: clustering.SummaryKey(2, hash), value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{MembershipHash: hash, Level: 2, Status: "pending"})},
		{name: "empty enhanced", key: clustering.SummaryKey(2, hash), value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{MembershipHash: hash, Level: 2, Status: clustering.SummaryStatusEnhanced})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, keep, decodeErr := decodeCommunitySummaryRecord(tt.key, tt.value, graphview.EntryMeta{})
			if tt.keep || tt.name == "failed is absence" {
				require.NoError(t, decodeErr)
				require.Equal(t, tt.keep, keep)
				require.Equal(t, tt.want, record.LLMSummary)
				return
			}
			require.Error(t, decodeErr)
			require.False(t, keep)
		})
	}
}

func TestSignalSummaryViewLossIsNonblockingAndCoalesced(t *testing.T) {
	t.Parallel()

	loss := make(chan struct{}, 1)
	signalSummaryViewLoss(loss)
	done := make(chan struct{})
	go func() {
		signalSummaryViewLoss(loss)
		close(done)
	}()
	<-done
	require.Len(t, loss, 1)
}

func TestSignalSummaryViewPoisonIsNonblockingAndCoalesced(t *testing.T) {
	t.Parallel()

	poison := make(chan summaryPoisonNotice, 1)
	first := summaryPoisonNotice{key: "first", err: errors.New("first poison")}
	signalSummaryViewPoison(poison, first)
	done := make(chan struct{})
	go func() {
		signalSummaryViewPoison(poison, summaryPoisonNotice{key: "second", err: errors.New("second poison")})
		close(done)
	}()
	receiveSummaryEvent(t, done)
	require.Len(t, poison, 1)
	require.Equal(t, first, <-poison)
}

func TestSummaryViewPoisonLoggingCannotBlockWatcherProgress(t *testing.T) {
	handler := &blockingPoisonLogHandler{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseLogger := func() { releaseOnce.Do(func() { close(handler.release) }) }
	t.Cleanup(releaseLogger)

	watcher := newControlledSummaryWatcher()
	reader := &controlledSummaryReader{watcher: watcher, called: make(chan struct{})}
	changed := make(chan *graphview.View[clustering.CommunitySummaryRecord], 2)
	applied := make(chan uint64, 4)
	component := &Component{
		config: Config{RecheckInterval: time.Hour},
		logger: slog.New(handler),
		openSummaryReader: func(context.Context) (graph.CatalogReader, error) {
			return reader, nil
		},
		summaryViewChanged: func(view *graphview.View[clustering.CommunitySummaryRecord]) { changed <- view },
		summaryViewApplied: func(_ string, revision uint64) { applied <- revision },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		component.superviseSummaryView(ctx)
		close(done)
	}()
	view := receiveSummaryEvent(t, changed)
	watcher.updates <- nil
	waitSummaryCaughtUp(t, view)

	watcher.updates <- summaryViewEntry{key: "invalid", value: []byte("{"), revision: 1}
	receiveSummaryEvent(t, handler.entered)
	require.Equal(t, uint64(1), receiveSummaryEvent(t, applied), "poison apply completes while warning handler blocks")

	other := &clustering.Community{Level: 1, Members: []string{entDrone3}, StatisticalSummary: "other floor"}
	otherHash := clustering.MembershipHash(other.Members)
	otherKey := clustering.SummaryKey(other.Level, otherHash)
	watcher.updates <- summaryViewEntry{
		key: otherKey,
		value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{
			MembershipHash: otherHash,
			Level:          other.Level,
			LLMSummary:     "unrelated enhanced summary",
			Status:         clustering.SummaryStatusEnhanced,
		}),
		revision: 2,
	}
	require.Equal(t, uint64(2), receiveSummaryEvent(t, applied), "next update applies while warning handler blocks")
	require.Equal(t, "unrelated enhanced summary", component.resolveCommunitySummary(other))
	watcher.updates <- summaryViewEntry{key: otherKey, revision: 3, operation: jetstream.KeyValueDelete}
	require.Equal(t, uint64(3), receiveSummaryEvent(t, applied), "next delete applies while warning handler blocks")
	require.Equal(t, other.StatisticalSummary, component.resolveCommunitySummary(other))

	releaseLogger()
	cancel()
	require.Nil(t, receiveSummaryEvent(t, changed))
	receiveSummaryEvent(t, done)
}

func TestSummaryViewSupervisorLifecycleAndFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	comm := themedCommunity()
	hash := clustering.MembershipHash(comm.Members)
	key := clustering.SummaryKey(comm.Level, hash)

	failedReader := &controlledSummaryReader{err: errors.New("watch unavailable"), called: make(chan struct{})}
	firstWatcher := newControlledSummaryWatcher()
	firstReader := &controlledSummaryReader{watcher: firstWatcher, called: make(chan struct{})}
	secondWatcher := newControlledSummaryWatcher()
	secondReader := &controlledSummaryReader{watcher: secondWatcher, called: make(chan struct{})}

	openCalls := make(chan int, 8)
	retryEntered := make(chan struct{}, 8)
	allowRetry := make(chan struct{}, 8)
	constructed := make(chan *graphview.View[clustering.CommunitySummaryRecord], 8)
	changed := make(chan *graphview.View[clustering.CommunitySummaryRecord], 8)
	applied := make(chan uint64, 16)
	stopped := make(chan *graphview.View[clustering.CommunitySummaryRecord], 8)
	openCount := 0
	component := &Component{
		config: Config{RecheckInterval: time.Hour},
		logger: logger,
		openSummaryReader: func(context.Context) (graph.CatalogReader, error) {
			openCount++
			openCalls <- openCount
			switch openCount {
			case 1:
				return nil, errors.New("bucket absent")
			case 2:
				return failedReader, nil
			case 3:
				return firstReader, nil
			default:
				return secondReader, nil
			}
		},
		waitSummaryRetry: func(ctx context.Context, _ time.Duration) bool {
			retryEntered <- struct{}{}
			select {
			case <-ctx.Done():
				return false
			case <-allowRetry:
				return true
			}
		},
		summaryViewConstructed: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			constructed <- view
		},
		summaryViewChanged: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			changed <- view
		},
		summaryViewApplied: func(_ string, revision uint64) { applied <- revision },
		summaryViewStopped: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			stopped <- view
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		component.superviseSummaryView(ctx)
		close(done)
	}()

	require.Equal(t, 1, receiveSummaryEvent(t, openCalls))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "absent bucket uses floor")
	receiveSummaryEvent(t, retryEntered)
	allowRetry <- struct{}{}

	require.Equal(t, 2, receiveSummaryEvent(t, openCalls))
	receiveSummaryEvent(t, failedReader.called)
	failedView := receiveSummaryEvent(t, constructed)
	require.Same(t, failedView, receiveSummaryEvent(t, stopped), "failed Start is stopped before retry")
	_, _, err := failedView.Get(key)
	require.ErrorIs(t, err, graphview.ErrViewStopped)
	receiveSummaryEvent(t, retryEntered)
	allowRetry <- struct{}{}

	require.Equal(t, 3, receiveSummaryEvent(t, openCalls))
	firstView := receiveSummaryEvent(t, constructed)
	receiveSummaryEvent(t, firstReader.called)
	require.Same(t, firstView, receiveSummaryEvent(t, changed), "successful Start publishes exactly its view")

	firstWatcher.updates <- summaryViewEntry{
		key: key,
		value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{
			MembershipHash: hash,
			Level:          comm.Level,
			LLMSummary:     "replayed enhanced summary",
			Status:         clustering.SummaryStatusEnhanced,
		}),
		revision: 1,
	}
	require.Equal(t, uint64(1), receiveSummaryEvent(t, applied))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "replay staging is unservable")
	firstWatcher.updates <- nil
	waitSummaryCaughtUp(t, firstView)
	require.Equal(t, "replayed enhanced summary", component.resolveCommunitySummary(comm))

	close(firstWatcher.updates)
	require.Nil(t, receiveSummaryEvent(t, changed), "loss clears the exact published pointer")
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "loss fails closed")
	require.Same(t, firstView, receiveSummaryEvent(t, stopped), "failed view stops before replacement")
	receiveSummaryEvent(t, retryEntered)
	allowRetry <- struct{}{}

	require.Equal(t, 4, receiveSummaryEvent(t, openCalls), "replacement reopens the catalog")
	secondView := receiveSummaryEvent(t, constructed)
	require.NotSame(t, firstView, secondView)
	receiveSummaryEvent(t, secondReader.called)
	require.Same(t, secondView, receiveSummaryEvent(t, changed))
	secondWatcher.updates <- nil
	waitSummaryCaughtUp(t, secondView)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "empty fresh replay removes ghosts")

	cancel()
	require.Nil(t, receiveSummaryEvent(t, changed), "cancellation clears the exact pointer")
	require.Same(t, secondView, receiveSummaryEvent(t, stopped), "cancellation stops the current view")
	receiveSummaryEvent(t, done)
	require.Equal(t, 4, openCount, "cancellation creates no replacement")
	_, _, err = secondView.Get(key)
	require.ErrorIs(t, err, graphview.ErrViewStopped)
}

func TestSummaryViewProjectionUpdateDeletePurgeAndPoison(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	comm := themedCommunity()
	hash := clustering.MembershipHash(comm.Members)
	key := clustering.SummaryKey(comm.Level, hash)
	watcher := newControlledSummaryWatcher()
	reader := &controlledSummaryReader{watcher: watcher, called: make(chan struct{})}
	applied := make(chan uint64, 8)
	view, err := graphview.New[clustering.CommunitySummaryRecord](reader, decodeCommunitySummaryRecord,
		graphview.WithHooks(graphview.Hooks{OnApply: func(_ string, revision uint64) { applied <- revision }}))
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	t.Cleanup(view.Stop)
	component := &Component{logger: logger}
	component.publishSummaryView(view)
	watcher.updates <- nil
	waitSummaryCaughtUp(t, view)

	putSummaryViewRecord(t, watcher, applied, key, hash, comm.Level, "initial", 1)
	require.Equal(t, "initial", component.resolveCommunitySummary(comm))
	putSummaryViewRecord(t, watcher, applied, key, hash, comm.Level, "updated", 2)
	require.Equal(t, "updated", component.resolveCommunitySummary(comm))
	watcher.updates <- summaryViewEntry{key: key, revision: 3, operation: jetstream.KeyValueDelete}
	require.Equal(t, uint64(3), receiveSummaryEvent(t, applied))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "delete uses floor")
	putSummaryViewRecord(t, watcher, applied, key, hash, comm.Level, "restored", 4)
	watcher.updates <- summaryViewEntry{key: key, revision: 5, operation: jetstream.KeyValuePurge}
	require.Equal(t, uint64(5), receiveSummaryEvent(t, applied))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "purge uses floor")

	watcher.updates <- summaryViewEntry{key: key, value: []byte("{"), revision: 6}
	require.Equal(t, uint64(6), receiveSummaryEvent(t, applied))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "poison uses floor")
	other := &clustering.Community{Level: 1, Members: []string{entDrone3}, StatisticalSummary: "other floor"}
	otherHash := clustering.MembershipHash(other.Members)
	otherKey := clustering.SummaryKey(other.Level, otherHash)
	putSummaryViewRecord(t, watcher, applied, otherKey, otherHash, other.Level, "unrelated valid summary", 7)
	require.Equal(t, "unrelated valid summary", component.resolveCommunitySummary(other), "poison is per-key")
}

func TestSummaryViewFailedRecordAndEmptyReplayUseStatisticalFloor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	comm := themedCommunity()
	hash := clustering.MembershipHash(comm.Members)
	key := clustering.SummaryKey(comm.Level, hash)
	watcher := newControlledSummaryWatcher()
	reader := &controlledSummaryReader{watcher: watcher, called: make(chan struct{})}
	applied := make(chan uint64, 1)
	view, err := graphview.New[clustering.CommunitySummaryRecord](reader, decodeCommunitySummaryRecord,
		graphview.WithHooks(graphview.Hooks{OnApply: func(_ string, revision uint64) { applied <- revision }}))
	require.NoError(t, err)
	require.NoError(t, view.Start(context.Background()))
	t.Cleanup(view.Stop)
	component := &Component{logger: logger}
	component.publishSummaryView(view)

	watcher.updates <- nil
	waitSummaryCaughtUp(t, view)
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "empty caught-up view uses floor")
	watcher.updates <- summaryViewEntry{
		key: key,
		value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{
			MembershipHash: hash,
			Level:          comm.Level,
			Status:         clustering.SummaryStatusFailed,
		}),
		revision: 1,
	}
	require.Equal(t, uint64(1), receiveSummaryEvent(t, applied))
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "failed record is valid absence")
	view.Stop()
	require.Equal(t, comm.StatisticalSummary, component.resolveCommunitySummary(comm), "stopped view uses floor")
}

func TestSummaryViewSupervisorCancellationDuringRetry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	retryEntered := make(chan struct{})
	constructed := false
	published := false
	component := &Component{
		config: Config{RecheckInterval: time.Hour},
		logger: logger,
		openSummaryReader: func(context.Context) (graph.CatalogReader, error) {
			return nil, errors.New("bucket absent")
		},
		waitSummaryRetry: func(ctx context.Context, _ time.Duration) bool {
			close(retryEntered)
			<-ctx.Done()
			return false
		},
		summaryViewConstructed: func(*graphview.View[clustering.CommunitySummaryRecord]) { constructed = true },
		summaryViewChanged:     func(*graphview.View[clustering.CommunitySummaryRecord]) { published = true },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		component.superviseSummaryView(ctx)
		close(done)
	}()
	receiveSummaryEvent(t, retryEntered)
	cancel()
	receiveSummaryEvent(t, done)
	require.False(t, constructed)
	require.False(t, published)
}

func TestSummaryViewSupervisorCancellationDuringStartStopsUnpublishedView(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := &blockingSummaryReader{called: make(chan struct{})}
	constructed := make(chan *graphview.View[clustering.CommunitySummaryRecord], 1)
	stopped := make(chan *graphview.View[clustering.CommunitySummaryRecord], 1)
	published := false
	component := &Component{
		config: Config{RecheckInterval: time.Hour},
		logger: logger,
		openSummaryReader: func(context.Context) (graph.CatalogReader, error) {
			return reader, nil
		},
		summaryViewConstructed: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			constructed <- view
		},
		summaryViewChanged: func(*graphview.View[clustering.CommunitySummaryRecord]) { published = true },
		summaryViewStopped: func(view *graphview.View[clustering.CommunitySummaryRecord]) {
			stopped <- view
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		component.superviseSummaryView(ctx)
		close(done)
	}()
	view := receiveSummaryEvent(t, constructed)
	receiveSummaryEvent(t, reader.called)
	cancel()
	require.Same(t, view, receiveSummaryEvent(t, stopped))
	receiveSummaryEvent(t, done)
	require.False(t, published)
	_, _, err := view.Get("unused")
	require.ErrorIs(t, err, graphview.ErrViewStopped)
}

func waitSummaryCaughtUp(t *testing.T, view *graphview.View[clustering.CommunitySummaryRecord]) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), summaryViewTestTimeout)
	defer cancel()
	require.NoError(t, view.WaitCaughtUp(ctx))
}

func putSummaryViewRecord(
	t *testing.T,
	watcher *controlledSummaryWatcher,
	applied <-chan uint64,
	key string,
	hash string,
	level int,
	summary string,
	revision uint64,
) {
	t.Helper()
	watcher.updates <- summaryViewEntry{
		key: key,
		value: mustSummaryJSON(t, clustering.CommunitySummaryRecord{
			MembershipHash: hash,
			Level:          level,
			LLMSummary:     summary,
			Status:         clustering.SummaryStatusEnhanced,
		}),
		revision: revision,
	}
	require.Equal(t, revision, receiveSummaryEvent(t, applied))
}

func receiveSummaryEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(summaryViewTestTimeout):
		t.Fatal("timed out waiting for summary-view synchronization event")
		var zero T
		return zero
	}
}

func mustSummaryJSON(t *testing.T, record clustering.CommunitySummaryRecord) []byte {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	return data
}
