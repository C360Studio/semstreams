package graphembedding

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

type exactStore struct{ body string }

func (s exactStore) Put(context.Context, string, []byte) error      { return nil }
func (s exactStore) Get(context.Context, string) ([]byte, error)    { return []byte(s.body), nil }
func (s exactStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (s exactStore) Delete(context.Context, string) error           { return nil }
func (s exactStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.body)), nil
}

// Regression guard for the gh#354-session find: #264 (ADR-055 Wave 0) began
// lifting StorageRef onto the EntityState at the ingest seam. graph-embedding
// then committed to the StorageRef→ObjectStore fetch path whenever a StorageRef
// was present — but in configs with no exact StorageInstance registered (e.g. the BM25
// statistical tier), fetchTextFromStorage hard-fails ("content store not
// configured"), markFailed fires, and ALL embedding/search silently collapses
// (statistical known-answer 7/7→0/7, shipped beta.113–115, masked as e2e
// warnings).
//
// The fix: only take the StorageRef path when its exact owner is registered;
// otherwise fall back to inline text extraction. shouldFetchViaStorageRef
// encodes that decision.
func TestShouldFetchViaStorageRef(t *testing.T) {
	storageRef := &message.StorageReference{StorageInstance: "objstore", Key: "k/1"}

	registry := storeregistry.New()
	assert.NoError(t, registry.Register("objstore", exactStore{body: "exact"}))
	assert.NoError(t, registry.Register("foreign", exactStore{body: "foreign"}))

	cases := []struct {
		name     string
		hasRef   bool
		instance string
		registry *storeregistry.Registry
		want     bool
	}{
		{
			name: "exact StorageInstance registered -> fetch offloaded content", hasRef: true,
			instance: "objstore", registry: registry, want: true,
		},
		{
			name: "only a foreign instance registered -> exclude body and continue inline", hasRef: true,
			instance: "missing", registry: registry, want: false,
		},
		{
			name: "exact instance but no registry admitted -> continue inline", hasRef: true,
			instance: "objstore", registry: nil, want: false,
		},
		{
			name: "no StorageRef -> inline path", hasRef: false,
			instance: "objstore", registry: registry, want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es := &graph.EntityState{ID: "c360.platform.test.sys.widget.001"}
			if tc.hasRef {
				ref := *storageRef
				ref.StorageInstance = tc.instance
				es.StorageRef = &ref
			}
			c := &Component{storeRegistry: tc.registry}
			assert.Equal(t, tc.want, c.shouldFetchViaStorageRef(es))
		})
	}
}

// TestExtractTextForEmbedding_RecoversInlineContentForStorageRefEntity locks the
// other half of exclusion behavior: an entity that carries BOTH a StorageRef and
// inline content triples (the exact shape of the e2e document/sensor entities)
// still yields non-empty text via inline extraction, so the no-content-store
// inline continuation actually has something to embed.
func TestExtractTextForEmbedding_RecoversInlineContentForStorageRefEntity(t *testing.T) {
	c := &Component{}
	es := &graph.EntityState{
		ID:         "c360.logistics.maintenance.work.completed.maint-001",
		StorageRef: &message.StorageReference{StorageInstance: "objstore", Key: "k/maint-001"},
		Triples: []message.Triple{
			{Subject: "x", Predicate: "maintenance.text.title", Object: "Hydraulic pump service"},
			{Subject: "x", Predicate: "maintenance.text.description", Object: "Replaced seals on hydraulic reservoir"},
		},
	}
	got := c.extractTextForEmbedding(es)
	assert.NotEmpty(t, got, "inline content triples must remain extractable when the offloaded body is excluded")
	assert.Contains(t, got, "Hydraulic pump service")
	assert.Contains(t, got, "Replaced seals")
}

func TestQueueEntityForEmbedding_UnresolvedBodyWithoutInlineTextSkipsAndDeletesStaleVector(t *testing.T) {
	ctx := context.Background()
	index := newMockKVBucket()
	c := newFindingsTestComponent(t, index)
	c.metrics = getMetrics(nil)
	c.storeRegistry = storeregistry.New()
	require.NoError(t, c.storeRegistry.Register("foreign", exactStore{body: "must not be read"}))

	const entityID = "c360.platform.test.sys.doc.unresolved"
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 4))
	require.NoError(t, c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25", 3, "old", 4))

	state := graph.EntityState{
		ID: entityID,
		StorageRef: &message.StorageReference{
			StorageInstance: "missing",
			Key:             "doc/1",
		},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	before := testutil.ToFloat64(c.metrics.contentUnresolved)
	c.queueEntityForEmbedding(ctx, entityID, 5, data)

	record, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, record, "the ordinary unresolved/no-text path must delete the stale vector")
	count, _, _ := c.failedSnapshot()
	require.Zero(t, count, "an unresolved body alone must not enter failed/degraded accounting")
	require.Equal(t, before+1, testutil.ToFloat64(c.metrics.contentUnresolved))

	require.Equal(t, uint64(1), c.embeddingCompletions.Load(),
		"the unresolved/no-text path must reach the existing terminal skip")
}

// warnCounter is a slog handler that counts Warn+ records.
type warnCounter struct{ n atomic.Int64 }

func (h *warnCounter) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *warnCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.n.Add(1)
	}
	return nil
}
func (h *warnCounter) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCounter) WithGroup(string) slog.Handler      { return h }

// TestReportOffloadedContentExcluded_LoudNotSilent locks the gh#414 fix: an
// entity with offloaded content but no exact registered owner must be OBSERVABLE — the
// content_unresolved_total metric increments per entity, and the actionable
// warning fires once (not per entity, to avoid a flood).
func TestReportOffloadedContentExcluded_LoudNotSilent(t *testing.T) {
	wc := &warnCounter{}
	c := &Component{
		metrics: getMetrics(nil), // default prometheus registry
		logger:  slog.New(wc),
		// storeRegistry nil, noContentStoreWarn zero-value Once
	}

	before := testutil.ToFloat64(c.metrics.contentUnresolved)

	// Two offloaded bodies cannot be resolved (no exact registered owner).
	c.reportOffloadedContentExcluded("c360.platform.test.sys.doc.001", "objstore")
	c.reportOffloadedContentExcluded("c360.platform.test.sys.doc.002", "objstore")

	after := testutil.ToFloat64(c.metrics.contentUnresolved)
	assert.Equal(t, float64(2), after-before, "metric must count every excluded entity")
	assert.Equal(t, int64(1), wc.n.Load(), "warning must fire once, not per entity (no log flood)")
}
