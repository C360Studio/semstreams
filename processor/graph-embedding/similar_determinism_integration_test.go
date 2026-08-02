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

// TestIntegration_FindSimilar_DeterministicTopKAtTie_BothPaths drives the P1 #3
// regression (Epic B B2) through BOTH real similarity paths against a live KV.
// It seeds MORE than k entities that all share the SAME embedding vector — so
// they all have identical cosine similarity to the query and the k-boundary
// tie-break is the ONLY thing that decides which k are returned. The entities are
// inserted in a NON-lexicographic order so the KV key-scan order (cold path) and
// the map-iteration order (warm path) both differ from the deterministic answer.
//
// The deterministic invariant is that the top-k of an all-equal-similarity group
// is the lexicographically smallest k IDs (similarity ties break by ID ascending
// via embedding.ScoredEntityLess). Before the fix — sort by similarity only —
// the cold KV scan and warm cache each returned an enumeration-dependent subset,
// producing nondeterministic mutual-kNN edges downstream.
func TestIntegration_FindSimilar_DeterministicTopKAtTie_BothPaths(t *testing.T) {
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

	st := embedding.NewStorage(nc, indexBucket, dedupBucket)
	c := &Component{storage: st}

	const k = 8
	// 20 entity IDs inserted in NON-lexicographic order (reversed), all sharing
	// the SAME vector so cosine similarity to the query is identical for every one.
	ids := []string{
		"c360.plat.docs.site.doc.p20", "c360.plat.docs.site.doc.p19",
		"c360.plat.docs.site.doc.p18", "c360.plat.docs.site.doc.p17",
		"c360.plat.docs.site.doc.p16", "c360.plat.docs.site.doc.p15",
		"c360.plat.docs.site.doc.p14", "c360.plat.docs.site.doc.p13",
		"c360.plat.docs.site.doc.p12", "c360.plat.docs.site.doc.p11",
		"c360.plat.docs.site.doc.p10", "c360.plat.docs.site.doc.p09",
		"c360.plat.docs.site.doc.p08", "c360.plat.docs.site.doc.p07",
		"c360.plat.docs.site.doc.p06", "c360.plat.docs.site.doc.p05",
		"c360.plat.docs.site.doc.p04", "c360.plat.docs.site.doc.p03",
		"c360.plat.docs.site.doc.p02", "c360.plat.docs.site.doc.p01",
	}
	for _, id := range ids {
		require.NoError(t, st.SavePending(ctx, id, "hash-"+id, "text", 0))
		require.NoError(t, st.SaveGenerated(ctx, id, []float32{1, 0, 0}, "test", 3, "hash-"+id, 0))
	}

	// Deterministic expectation: lexicographically smallest k IDs.
	wantSorted := append([]string(nil), ids...)
	sort.Strings(wantSorted)
	want := wantSorted[:k]

	query := []float32{1, 1, 0} // equidistant from every seeded vector
	topKIDs := func(results []SimilarEntity) []string {
		got := make([]string, len(results))
		for i, r := range results {
			got[i] = r.EntityID
		}
		return got
	}

	// Cold path: cache never warmed -> findSimilarEntities falls to the KV scan,
	// which must sort through the canonical comparator before truncating to k.
	t.Run("cold KV scan", func(t *testing.T) {
		results, err := c.findSimilarEntities(ctx, "", query, nil, k)
		require.NoError(t, err)
		require.Len(t, results, k)
		require.Equal(t, want, topKIDs(results),
			"cold-path top-k of an all-equal-similarity group must be the lex-smallest k, not KV-scan order")
	})

	// Warm path: same corpus, same query — the in-memory cache path must reach
	// the identical deterministic subset.
	t.Run("warm cache", func(t *testing.T) {
		require.NoError(t, st.StartVectorCache(ctx))
		require.Eventually(t, func() bool {
			_, ok := st.FindSimilarFromCache("", query, nil, 1)
			return ok
		}, 5*time.Second, 50*time.Millisecond, "vector cache should warm")

		// Run several times: map iteration permutes the sort input each call, so a
		// stable result across runs proves the tie-break, not luck.
		for i := 0; i < 10; i++ {
			results, err := c.findSimilarEntities(ctx, "", query, nil, k)
			require.NoError(t, err)
			require.Len(t, results, k)
			require.Equal(t, want, topKIDs(results),
				"warm-path top-k must equal the lex-smallest k on every call")
		}
	})
}
