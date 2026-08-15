//go:build integration

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_PostBootDynamicEditReconcilesBucketAtSeam is the PROOF test
// for the discharged framework-composition clause "post-cutoff bucket
// acquisition is CLOSED by the acquisition seam": a fully booted process, a
// framework bucket dirtied OUT-OF-BAND after boot (a foreign MaxAge applied to
// KV_EMBEDDING_INDEX's backing stream), and a post-boot dynamic configuration
// EDIT delivered through the REAL config watcher (a semstreams_config KV
// write) that restarts the REAL graph-embedding component — whose Start
// re-acquires EMBEDDING_INDEX through the ensure seam and strips the TTL, with
// NO boot sweep involved (StartAll has none, and the timeline proves it: the
// dirt is applied only after StartAll returned).
func TestIntegration_PostBootDynamicEditReconcilesBucketAtSeam(t *testing.T) {
	ctx := context.Background()

	// Install a recording default logger BEFORE the NATS client is built:
	// natsclient.Client captures slog.Default() at construction, and the
	// seam's strip-WARN is emitted through that logger. Not t.Parallel —
	// slog.SetDefault is process-global.
	rec := &bucketSweepRecordingHandler{}
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(rec))
	defer slog.SetDefault(prevDefault)

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()
	client := testClient.Client

	// graph-embedding waits for ENTITY_STATES (its watch source); provision it
	// the way its owner (graph-ingest) does — through the catalog seam.
	_, err := graph.EnsureCatalogBucket(ctx, client, graph.BucketEntityStates)
	require.NoError(t, err)

	// The REAL graph-embedding factory, registered through the production
	// registry path (bm25 = pure Go, no external embedding service).
	compRegistry := component.NewRegistry()
	require.NoError(t, graphembedding.Register(compRegistry))

	h := newBootDrainHarness(t, client, compRegistry, &config.Config{
		Platform: bootDrainPlatform(),
		Components: config.ComponentConfigs{
			"graph-embedding-comp": types.ComponentConfig{
				Type:    types.ComponentTypeProcessor,
				Name:    "graph-embedding",
				Enabled: true,
				Config:  json.RawMessage(`{"embedder_type":"bm25","batch_size":50}`),
			},
		},
	}, rec)

	// Boot FULLY: StartAll returns only after graph-embedding's Start has run
	// (its first seam acquisition creates EMBEDDING_INDEX clean).
	require.NoError(t, h.manager.StartAll(ctx))
	defer func() { _ = h.manager.StopAll(context.Background()) }()
	bootDone := time.Now()

	status := h.cm.GetComponentStatus()
	require.Contains(t, status, "graph-embedding-comp")
	require.Equal(t, component.StateStarted, status["graph-embedding-comp"].State,
		"the real graph-embedding must be running before the out-of-band dirtying")

	// Out-of-band dirt, applied AFTER boot completed: a foreign 1h MaxAge on
	// the backing stream — the class no boot-time pass can ever see.
	js, err := client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "KV_"+graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	dirtyCfg := info.Config
	dirtyCfg.MaxAge = time.Hour
	_, err = js.UpdateStream(ctx, dirtyCfg)
	require.NoError(t, err)

	dirty, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	maxAge, _, err := natsclient.BucketRetention(ctx, dirty)
	require.NoError(t, err)
	require.Equal(t, time.Hour, maxAge, "precondition: the out-of-band TTL must be live post-boot")

	// The post-boot dynamic EDIT through the production wire: a KV write to
	// semstreams_config that the real config watcher consumes, rebuilding and
	// restarting graph-embedding. Its Start re-acquires EMBEDDING_INDEX
	// through the ensure seam — the reconcile point under test.
	putComponentConfigKV(t, client, "graph-embedding-comp", types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "graph-embedding",
		Enabled: true,
		Config:  json.RawMessage(`{"embedder_type":"bm25","batch_size":60}`),
	})

	// The observable outcome: the foreign TTL is stripped by the restart's
	// seam acquisition. Eventually, because the watcher applies the edit
	// asynchronously; the deadline is generous, the poll cheap.
	require.Eventually(t, func() bool {
		bucket, gerr := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
		if gerr != nil {
			return false
		}
		age, _, rerr := natsclient.BucketRetention(ctx, bucket)
		return rerr == nil && age == 0
	}, 30*time.Second, 250*time.Millisecond,
		"the post-boot dynamic edit's seam re-acquisition must strip the out-of-band TTL")

	// The WARN naming the bucket fired, and it fired AFTER boot completed —
	// the timeline proof that no boot sweep did the strip (none exists).
	require.True(t, rec.warnMentioning(graph.BucketEmbeddingIndex),
		"the seam must WARN naming the stripped bucket")
	assert.True(t, stripWarnAfter(rec, graph.BucketEmbeddingIndex, bootDone),
		"the strip WARN must postdate StartAll returning — proving the seam, not a boot pass, reconciled it")

	// And the component came back up under the edited config.
	require.Eventually(t, func() bool {
		st := h.cm.GetComponentStatus()
		s, ok := st["graph-embedding-comp"]
		return ok && s.State == component.StateStarted
	}, 30*time.Second, 250*time.Millisecond, "graph-embedding must be restarted after the edit")

	// Belt: the bucket still exists (the strip deleted nothing).
	_, err = client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
}

// stripWarnAfter reports whether a WARN mentioning bucket was recorded with a
// timestamp after mark.
func stripWarnAfter(h *bucketSweepRecordingHandler, bucket string, mark time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelWarn || !r.Time.After(mark) {
			continue
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Value.Kind() == slog.KindString && a.Value.String() == bucket {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
