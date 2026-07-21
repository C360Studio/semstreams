package fusion_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFuse_IncludeScores pins ADR-084 D5. The scores are opt-in and additive; the
// property worth guarding is the JOIN — similarity belongs to an entity, and ranking
// reorders, so anything positional silently mislabels.
func TestFuse_IncludeScores(t *testing.T) {
	// The fixture must make ranking REORDER relative to resolve order, or a positional
	// join would coincidentally produce the right numbers and this test would prove
	// nothing. Bravo resolves SECOND but matches the query lexically, so ranking lifts
	// it to first — and the two seeds carry very different scores.
	alpha := entity("acme.ops.code.repo.symbol.Alpha", "Alpha", "a.go")
	bravo := entity("acme.ops.code.repo.symbol.Bravo", "Bravo", "b.go")
	byID := map[string]*fusion.Entity{alpha.ID: alpha, bravo.ID: bravo}
	const query = "Bravo"

	newGraph := func() *fakeGraph {
		return &fakeGraph{
			status:   readyStatus(),
			seeds:    map[string][]string{query: {alpha.ID, bravo.ID}},
			entities: byID,
			seedsFn: func(fusion.ResolveQuery) []fusion.Seed {
				return []fusion.Seed{
					{ID: alpha.ID, Similarity: 0.20, HasSimilarity: true},
					{ID: bravo.ID, Similarity: 0.90, HasSimilarity: true},
				}
			},
		}
	}

	fuse := func(t *testing.T, g *fakeGraph, req fusion.Request) fusion.Response {
		t.Helper()
		eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err)
		return resp
	}

	t.Run("similarity is joined by entity ID", func(t *testing.T) {
		resp := fuse(t, newGraph(), fusion.Request{Query: query, IncludeScores: true})
		require.Len(t, resp.Nodes, 2)

		// Precondition: ranking really did reorder. Without this the assertions below
		// would pass under a positional join and prove nothing.
		require.Equal(t, "Bravo", resp.Nodes[0].Name,
			"fixture no longer exercises a reorder; the ID-join assertion would be vacuous")

		byName := map[string]fusion.Node{}
		for _, n := range resp.Nodes {
			byName[n.Name] = n
		}
		require.NotNil(t, byName["Alpha"].Similarity)
		require.NotNil(t, byName["Bravo"].Similarity)
		assert.InDelta(t, 0.20, *byName["Alpha"].Similarity, 1e-9,
			"Alpha's score followed a slice position instead of its entity ID")
		assert.InDelta(t, 0.90, *byName["Bravo"].Similarity, 1e-9,
			"Bravo's score followed a slice position instead of its entity ID")
	})

	t.Run("rank is the RESOLVE rank, not the response position", func(t *testing.T) {
		// The distinguishing assertion. Alpha resolved FIRST and came out SECOND
		// because ranking lifted Bravo on lexical affinity. If Rank were the response
		// position it would read 1,2 top-to-bottom and tell the caller nothing they
		// could not count — the gap between resolve rank and position IS the signal.
		resp := fuse(t, newGraph(), fusion.Request{Query: query, IncludeScores: true})
		require.Len(t, resp.Nodes, 2)
		require.Equal(t, "Bravo", resp.Nodes[0].Name, "precondition: ranking must reorder")

		assert.Equal(t, 2, resp.Nodes[0].Rank,
			"Bravo resolved second; reporting 1 here would just be echoing the array index")
		assert.Equal(t, 1, resp.Nodes[1].Rank,
			"Alpha resolved first and was demoted by ranking — exactly the surprise this field explains")
	})

	t.Run("an unscored mode reports no similarity rather than zero", func(t *testing.T) {
		// symbol and prefix resolve carry no score. Emitting 0.0 would advertise a
		// perfect non-match — a claim those wires never made.
		g := newGraph()
		g.seedsFn = nil // default fake seeds are unscored, like the symbol wire
		resp := fuse(t, g, fusion.Request{Query: query, IncludeScores: true})
		require.NotEmpty(t, resp.Nodes)
		for _, n := range resp.Nodes {
			assert.Nil(t, n.Similarity, "an unscored mode must not claim a similarity")
			assert.NotZero(t, n.Rank, "rank is always available — every mode resolves in some order")
		}
	})

	t.Run("scores are opt-in and omitted from the default wire", func(t *testing.T) {
		resp := fuse(t, newGraph(), fusion.Request{Query: query})
		raw, err := json.Marshal(resp)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), `"rank"`,
			"the default response must be byte-unchanged for existing consumers: %s", raw)
		assert.NotContains(t, string(raw), `"similarity"`)
		for _, n := range resp.Nodes {
			assert.Zero(t, n.Rank, "rank must not be populated without the opt-in")
		}
	})
}

// TestRequest_IncludeScoresRoundTrips is the operator-surface discipline: every
// request-reachable field needs a JSON round-trip so a rename or a wrong tag cannot
// silently make the flag unsettable from the wire.
func TestRequest_IncludeScoresRoundTrips(t *testing.T) {
	var req fusion.Request
	require.NoError(t, json.Unmarshal([]byte(`{"query":"q","include_scores":true}`), &req))
	assert.True(t, req.IncludeScores, "include_scores did not decode — the flag is unsettable")

	raw, err := json.Marshal(fusion.Request{Query: "q", IncludeScores: true})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"include_scores":true`)

	// Absent means off, and off is omitted — the pre-ADR-084 request shape.
	raw, err = json.Marshal(fusion.Request{Query: "q"})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "include_scores")
}

// TestFuse_ZeroSimilarityIsDistinguishableFromAbsent pins the review's medium finding.
// A semantic resolve can legitimately score a seed 0.0 — an orthogonal embedding is a
// real answer, not a missing one. With `float64,omitempty` that value vanished from the
// wire, leaving a non-Go consumer unable to tell "scored zero" from "this mode does not
// score". The pointer makes presence the encoding.
func TestFuse_ZeroSimilarityIsDistinguishableFromAbsent(t *testing.T) {
	ent := entity("acme.ops.code.repo.symbol.Orthogonal", "Orthogonal", "o.go")
	g := &fakeGraph{
		status:   readyStatus(),
		seeds:    map[string][]string{"q": {ent.ID}},
		entities: map[string]*fusion.Entity{ent.ID: ent},
		seedsFn: func(fusion.ResolveQuery) []fusion.Seed {
			return []fusion.Seed{{ID: ent.ID, Similarity: 0, HasSimilarity: true}}
		},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q", IncludeScores: true}, refLens{})
	require.NoError(t, err)
	require.Len(t, resp.Nodes, 1)

	require.NotNil(t, resp.Nodes[0].Similarity, "an available score of 0 was erased")
	assert.Equal(t, 0.0, *resp.Nodes[0].Similarity)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"similarity":0`,
		"a genuine zero must reach the wire: %s", raw)

	var back fusion.Response
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Nodes[0].Similarity)
	assert.Equal(t, 0.0, *back.Nodes[0].Similarity)
}
