package fusion_test

import (
	"context"
	"slices"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
)

// TestEngine_RankingFollowsHydrationOrder pins the load-bearing coupling between
// hydration order and rank (ADR-084 D4, task 2.3). The engine's resolve-rank base is
// POSITION-derived — entity i scores resolveScale*(len-i) — so the order Entities
// returns IS the ranking prior, not a presentation detail.
//
// It exists because that contract was being broken in production and nothing caught
// it: graph-ingest's batch hydration returns cache hits first and KV fetches after
// ("callers process as sets"), so cache residency alone could demote the top resolve
// seed. Every fusion test until now used a fake that returns requested order, which
// models the FIXED contract and therefore proves nothing about the broken one.
//
// fusionnats.Entities now restores resolve order before returning (see
// reconcileHydration's "restores request order" case, which pins the transport half).
// This test is the reason that fix is required rather than optional, and it keeps
// passing after it — the engine's dependence on order is deliberate; the transport's
// failure to honor it was the bug. The RetrievalClient contract now states the
// requirement explicitly so a future implementation cannot reintroduce it by accident.
func TestEngine_RankingFollowsHydrationOrder(t *testing.T) {
	// Three entities whose titles share no lexical affinity with the query, so
	// position is the only ranking signal in play and the assertion is unambiguous.
	first := entity("acme.ops.code.repo.symbol.Alpha", "Alpha", "a.go")
	second := entity("acme.ops.code.repo.symbol.Bravo", "Bravo", "b.go")
	third := entity("acme.ops.code.repo.symbol.Charlie", "Charlie", "c.go")
	byID := map[string]*fusion.Entity{first.ID: first, second.ID: second, third.ID: third}
	resolveOrder := []string{first.ID, second.ID, third.ID}

	fuseWith := func(t *testing.T, hydrate func(ids []string) ([]*fusion.Entity, error)) []string {
		t.Helper()
		g := &fakeGraph{
			status:     readyStatus(),
			seeds:      map[string][]string{"q": resolveOrder},
			entities:   byID,
			entitiesFn: hydrate,
		}
		eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
		resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{})
		if err != nil {
			t.Fatalf("Fuse: %v", err)
		}
		got := make([]string, len(resp.Nodes))
		for i, n := range resp.Nodes {
			got[i] = n.Name
		}
		return got
	}

	t.Run("resolve order is preserved in rank", func(t *testing.T) {
		got := fuseWith(t, func(ids []string) ([]*fusion.Entity, error) {
			out := make([]*fusion.Entity, 0, len(ids))
			for _, id := range ids {
				out = append(out, byID[id])
			}
			return out, nil
		})
		want := []string{"Alpha", "Bravo", "Charlie"}
		if !slices.Equal(got, want) {
			t.Errorf("rank = %v, want %v — the engine dropped its position-derived base", got, want)
		}
	})

	t.Run("a transport that reorders demotes the top seed", func(t *testing.T) {
		// The production shape: Charlie was a cache hit, so it arrives first even
		// though it resolved last. Nothing about the query changed — only which
		// entities happened to be resident.
		got := fuseWith(t, func(ids []string) ([]*fusion.Entity, error) {
			out := []*fusion.Entity{byID[ids[2]]}
			for _, id := range ids[:2] {
				out = append(out, byID[id])
			}
			return out, nil
		})
		if got[0] != "Charlie" {
			t.Fatalf("fixture did not exercise the reorder: rank = %v", got)
		}
		if got[len(got)-1] != "Bravo" {
			t.Errorf("rank = %v; the fixture must show hydration order driving rank end to end", got)
		}
		// The point, stated as an assertion: the resolve-top entity lost first place
		// to a cache-residency accident. When a transport guarantees resolve order
		// this case becomes unreachable in production — it stays here as the record
		// of what the guarantee buys.
		if got[0] == "Alpha" {
			t.Error("expected the reordered hydration to demote the resolve-top entity")
		}
	})
}
