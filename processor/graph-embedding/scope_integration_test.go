//go:build integration

package graphembedding

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ScopedSearch_BothPaths drives the ADR-071 scope filter through
// BOTH real similarity paths — the cold KV scan (cache not warmed) and the warm
// in-memory cache — against a mixed code+docs corpus in a live KV. It reproduces
// the httpx dilution shape: without scope the large code domain out-ranks the
// small docs domain; with a docs scope only docs entities come back. The
// warm-cache path is the BLOCKING regression the ADR calls out (scope applied
// only on the cold fallback is a silent no-op in warm production).
func TestIntegration_ScopedSearch_BothPaths(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)

	indexBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEmbeddingIndex})
	require.NoError(t, err)
	dedupBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEmbeddingDedup})
	require.NoError(t, err)

	st := embedding.NewStorage(indexBucket, dedupBucket)
	c := &Component{storage: st}

	// Mixed corpus: a large "code" domain and a small "docs" domain. The docs
	// vectors are the closest to the query so a correct scope MUST surface them
	// and an incorrect (unscoped) search buries them under the code majority.
	code := []string{
		"c360.semspec.python.pkg.fn.test_raises",
		"c360.semspec.python.pkg.fn.request",
		"c360.semspec.python.pkg.fn.session",
		"c360.semspec.golang.pkg.fn.Handle",
	}
	docs := []string{
		"c360.semspec.source.chunk.segment.exceptions_0",
		"c360.semspec.source.doc.page.exceptions",
	} // kept sorted so assertDocsOnly can compare against the sorted result
	save := func(id string, vec []float32) {
		require.NoError(t, st.SavePending(ctx, id, "hash-"+id, "text", 0))
		require.NoError(t, st.SaveGenerated(ctx, id, vec, "test", len(vec)))
	}
	for _, id := range code {
		save(id, []float32{1, 0, 0})
	}
	for _, id := range docs {
		save(id, []float32{0, 1, 0})
	}

	query := []float32{0, 1, 0} // closest to the docs vectors
	scope := []string{"c360.semspec.source.doc", "c360.semspec.source.chunk"}

	assertDocsOnly := func(t *testing.T, results []SimilarEntity) {
		t.Helper()
		got := make([]string, len(results))
		for i, r := range results {
			got[i] = r.EntityID
		}
		sort.Strings(got)
		require.Equal(t, docs, got, "scoped search must return only the in-scope docs entities")
	}

	// Cold path: cache never warmed → findSimilarEntities falls to the KV scan,
	// which must filter by scope before the per-candidate GetEmbedding.
	t.Run("cold KV scan", func(t *testing.T) {
		results, err := c.findSimilarEntities(ctx, "", query, scope, 10)
		require.NoError(t, err)
		assertDocsOnly(t, results)
	})

	// Warm path: start and wait for the vector cache, then the SAME query must
	// filter identically inside FindSimilarFromCache.
	t.Run("warm cache", func(t *testing.T) {
		require.NoError(t, st.StartVectorCache(ctx))
		require.Eventually(t, func() bool {
			_, ok := st.FindSimilarFromCache("", query, nil, 1)
			return ok
		}, 5*time.Second, 50*time.Millisecond, "vector cache should warm")

		results, err := c.findSimilarEntities(ctx, "", query, scope, 10)
		require.NoError(t, err)
		assertDocsOnly(t, results)
	})

	// Sanity: unscoped over the same warm corpus is NOT docs-only (the dilution
	// the scope fixes) — proves the scoped assertions above aren't vacuous.
	t.Run("unscoped is diluted", func(t *testing.T) {
		results, err := c.findSimilarEntities(ctx, "", query, nil, 10)
		require.NoError(t, err)
		require.Len(t, results, len(code)+len(docs), "unscoped search sees the whole corpus")
	})
}
