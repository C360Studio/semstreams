package graphquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
)

// TestSelectDigestEntities covers Lever A of thematic-synthesis-context:
// query-relevant representative selection with PageRank backfill and a full
// PageRank fallback when no relevance scores are present.
func TestSelectDigestEntities(t *testing.T) {
	// A community whose top-PageRank reps are NOT the query-relevant members.
	// Members are deliberately NOT in ID-ascending order: selectDigestEntities
	// builds its scored slice by iterating Members, so an ID-ascending fixture
	// would let a dropped tie-break survive (all-equal scores + insertion sort
	// leaves the already-ascending order in place). A shuffled order makes the
	// tie-break subtest genuinely reorder — dropping the tie-break then fails it.
	comm := &clustering.Community{
		ID:          "c0",
		Level:       0,
		Members:     []string{"m8", "m3", "m1", "m5", "m6", "m2", "m4", "m7"},
		RepEntities: []string{"m1", "m2"}, // PageRank reps (query-agnostic)
	}

	t.Run("nil scores fall back entirely to PageRank RepEntities", func(t *testing.T) {
		got := selectDigestEntities(comm, nil)
		if !slices.Equal(got, comm.RepEntities) {
			t.Errorf("nil scores: got %v, want RepEntities %v unchanged", got, comm.RepEntities)
		}
		got = selectDigestEntities(comm, map[string]float64{})
		if !slices.Equal(got, comm.RepEntities) {
			t.Errorf("empty scores: got %v, want RepEntities %v unchanged", got, comm.RepEntities)
		}
	})

	t.Run("members ranked by score desc, capped at MaxQueryFocusedReps", func(t *testing.T) {
		scores := map[string]float64{
			"m8": 0.9, "m6": 0.8, "m4": 0.7, "m3": 0.6, "m2": 0.5, "m1": 0.4,
		}
		got := selectDigestEntities(comm, scores)
		want := []string{"m8", "m6", "m4", "m3", "m2"} // top 5 by score
		if !slices.Equal(got, want) {
			t.Errorf("query-relevant order: got %v, want %v", got, want)
		}
		if len(got) != MaxQueryFocusedReps {
			t.Errorf("selection not capped: got %d, want %d", len(got), MaxQueryFocusedReps)
		}
	})

	t.Run("ties broken by entity ID ascending (determinism)", func(t *testing.T) {
		// All equal scores → deterministic lexical order, cap 5.
		scores := map[string]float64{
			"m5": 1.0, "m3": 1.0, "m8": 1.0, "m1": 1.0, "m6": 1.0, "m2": 1.0,
		}
		got := selectDigestEntities(comm, scores)
		want := []string{"m1", "m2", "m3", "m5", "m6"} // ID asc, top 5
		if !slices.Equal(got, want) {
			t.Errorf("tie-break: got %v, want %v (score-equal → ID asc)", got, want)
		}
	})

	t.Run("thin scored members backfill from RepEntities to the cap", func(t *testing.T) {
		// Only two members scored, both distinct from the two reps → 2 scored + 2
		// backfilled reps = 4 (fewer than cap because the candidate pool is thin).
		scores := map[string]float64{"m7": 0.9, "m8": 0.8}
		got := selectDigestEntities(comm, scores)
		want := []string{"m7", "m8", "m1", "m2"} // scored first, then reps in order
		if !slices.Equal(got, want) {
			t.Errorf("backfill: got %v, want %v", got, want)
		}
	})

	t.Run("backfill does not duplicate an already-selected rep", func(t *testing.T) {
		// m1 is both a rep AND a scored member; it must appear once.
		scores := map[string]float64{"m1": 0.95, "m7": 0.5}
		got := selectDigestEntities(comm, scores)
		want := []string{"m1", "m7", "m2"} // m1 (scored), m7 (scored), m2 (rep backfill)
		if !slices.Equal(got, want) {
			t.Errorf("dedupe on backfill: got %v, want %v", got, want)
		}
	})

	t.Run("no rep slot lost versus today", func(t *testing.T) {
		// With any relevance signal, the selection must never carry fewer slots
		// than today's pure-RepEntities behavior (len == min(cap, len(reps))).
		floor := len(comm.RepEntities)
		if floor > MaxQueryFocusedReps {
			floor = MaxQueryFocusedReps
		}
		for _, scores := range []map[string]float64{
			{"m7": 0.9},                       // 1 scored, disjoint from reps
			{"m7": 0.9, "m8": 0.8},            // 2 scored, disjoint
			{"m1": 0.9},                       // 1 scored that IS a rep
			{"m3": 0.9, "m4": 0.8, "m5": 0.7}, // 3 scored, disjoint
		} {
			got := selectDigestEntities(comm, scores)
			if len(got) < floor {
				t.Errorf("scores %v: got %d slots (%v), fewer than today's floor %d — a rep slot was lost",
					scores, len(got), got, floor)
			}
		}
	})

	t.Run("nil community returns nil", func(t *testing.T) {
		if got := selectDigestEntities(nil, map[string]float64{"m1": 1}); got != nil {
			t.Errorf("selectDigestEntities(nil) = %v, want nil", got)
		}
	})
}

// findCommunitiesForEntities walks EVERY level, so it is the one producer that can
// emit two summaries sharing a community ID. Each must carry its own level: the
// summary is later re-resolved via GetCommunity(summary.Level, summary.CommunityID),
// so a dropped stamp resolves silently to level 0 — indistinguishable from correct
// behavior in any level-0-only test, because 0 is the Go zero value.
func TestFindCommunitiesForEntities_StampsEachSummaryWithItsOwnLevel(t *testing.T) {
	const collidingID = "acme.ops.robotics.gcs.drone.001"

	cache := NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
	put := func(level int, members []string) {
		t.Helper()
		b, err := json.Marshal(&clustering.Community{ID: collidingID, Level: level, Members: members})
		if err != nil {
			t.Fatal(err)
		}
		cache.handleUpdate(fmt.Sprintf("%d.%s", level, collidingID), b)
	}
	put(0, []string{"e1"})
	put(1, []string{"e1", "e2"})

	c := &Component{
		communityCache: cache,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	got := c.findCommunitiesForEntities([]string{"e1"})
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2 (one per level sharing the ID)", len(got))
	}

	byLevel := map[int]CommunitySummary{}
	for _, s := range got {
		byLevel[s.Level] = s
	}
	for _, want := range []struct{ level, members int }{{0, 1}, {1, 2}} {
		s, ok := byLevel[want.level]
		if !ok {
			t.Fatalf("no summary carried level %d — levels seen: %v", want.level, byLevel)
		}
		if s.MemberCount != want.members {
			t.Errorf("level %d: MemberCount = %d, want %d", want.level, s.MemberCount, want.members)
		}
	}
}

// Community IDs are seed entity IDs and every level re-derives its partition from
// the same entity set, so the SAME id legitimately names different communities at
// different levels. enrichCommunitySummaries must therefore keep each summary's
// rep entities separate.
//
// Round-6 review found it keyed them by bare community ID, so two same-id summaries
// both received the LAST level's rep entities while each kept its own MemberCount —
// a digest stitched from two different communities, returned to agents on
// community_summaries[].entities[] and fed into the answer prompt. This became
// reachable exactly when the round-5 cache fix stopped collapsing levels.
func TestEnrichCommunitySummaries_CollidingIDsKeepTheirOwnRepEntities(t *testing.T) {
	const collidingID = "acme.ops.robotics.gcs.drone.001"

	cache := NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
	put := func(level int, members, reps []string) {
		t.Helper()
		b, err := json.Marshal(&clustering.Community{
			ID: collidingID, Level: level, Members: members, RepEntities: reps,
		})
		if err != nil {
			t.Fatal(err)
		}
		cache.handleUpdate(fmt.Sprintf("%d.%s", level, collidingID), b)
	}

	// Same ID at two levels, deliberately distinct membership AND rep entities.
	put(0, []string{"e1"}, []string{"rep-level0"})
	put(1, []string{"e1", "e2"}, []string{"rep-level1"})

	// No natsClient: resolveEntityLabels degrades to unlabelled digests, which is
	// fine here — this test is about WHICH community's rep entities each summary
	// gets, not about their labels.
	c := &Component{
		communityCache: cache,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	summaries := []CommunitySummary{
		{CommunityID: collidingID, Level: 0},
		{CommunityID: collidingID, Level: 1},
	}

	// nil scores → pure PageRank RepEntities selection (this test asserts each
	// summary keeps its own community's reps; relevance steering is orthogonal).
	got, err := c.enrichCommunitySummaries(context.Background(), summaries, nil)
	if err != nil {
		t.Fatalf("enrichCommunitySummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}

	// Each summary's MemberCount and Entities must come from the SAME community.
	for _, tc := range []struct {
		level          int
		wantMemberCnt  int
		wantRepEntitiy string
	}{
		{level: 0, wantMemberCnt: 1, wantRepEntitiy: "rep-level0"},
		{level: 1, wantMemberCnt: 2, wantRepEntitiy: "rep-level1"},
	} {
		s := got[tc.level] // summaries were supplied in level order
		if s.Level != tc.level {
			t.Fatalf("summary order changed: got level %d at index %d", s.Level, tc.level)
		}
		if s.MemberCount != tc.wantMemberCnt {
			t.Errorf("level %d: MemberCount = %d, want %d", tc.level, s.MemberCount, tc.wantMemberCnt)
		}
		if len(s.Entities) != 1 {
			t.Fatalf("level %d: got %d entity digests, want 1", tc.level, len(s.Entities))
		}
		if s.Entities[0].ID != tc.wantRepEntitiy {
			t.Errorf("level %d: rep entity = %q, want %q — a summary carrying another level's "+
				"rep entities is a digest stitched from two different communities",
				tc.level, s.Entities[0].ID, tc.wantRepEntitiy)
		}
	}
}

// TestEnrichCommunitySummaries_ScoresSteerRepDigests proves the semanticScores
// map threaded from the strategy handlers (handleStrategySemantic and the tier-1
// graphrag caller) is actually consumed by the enrichment path — not silently
// dropped. The pure ranking is covered by TestSelectDigestEntities; this closes
// the seam between it and the response-enrichment plumbing by driving the
// production enrichCommunitySummaries with and without scores and asserting the
// digest reps flip. The handler-level map construction itself is a literal
// mirror of the reviewed tier-1 caller and driving handleStrategySemantic
// end-to-end needs a semantic embedding searcher, so it is not exercised here.
// No natsClient: loadDigestEntities degrades to unlabelled digests, but the
// SELECTED IDs (what the scores steer) still surface as digest IDs.
func TestEnrichCommunitySummaries_ScoresSteerRepDigests(t *testing.T) {
	const commID = "acme.ops.robotics.gcs.drone.001"

	cache := NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b, err := json.Marshal(&clustering.Community{
		ID:          commID,
		Level:       0,
		Members:     []string{"m-a", "m-b", "m-c"},
		RepEntities: []string{"m-a"}, // PageRank rep (query-agnostic)
	})
	if err != nil {
		t.Fatal(err)
	}
	cache.handleUpdate("0."+commID, b)

	c := &Component{
		communityCache: cache,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// With scores steering m-c highest, the first digest must be the
	// query-relevant member m-c — NOT the PageRank rep m-a — proving the scores
	// map reached selectDigestEntities through enrichCommunitySummaries.
	scored, err := c.enrichCommunitySummaries(
		context.Background(),
		[]CommunitySummary{{CommunityID: commID, Level: 0}},
		map[string]float64{"m-c": 0.9, "m-b": 0.5},
	)
	if err != nil {
		t.Fatalf("enrichCommunitySummaries (scores): %v", err)
	}
	if len(scored) != 1 {
		t.Fatalf("scored: got %d summaries, want 1", len(scored))
	}
	if len(scored[0].Entities) == 0 {
		t.Fatal("scored: got 0 rep digests, want >=1 (scores were dropped?)")
	}
	if scored[0].Entities[0].ID != "m-c" {
		t.Errorf("scored digest[0] = %q, want %q (query-relevant member must outrank the PageRank rep)",
			scored[0].Entities[0].ID, "m-c")
	}

	// Control: nil scores → pure PageRank selection → the sole digest is m-a.
	nilScored, err := c.enrichCommunitySummaries(
		context.Background(),
		[]CommunitySummary{{CommunityID: commID, Level: 0}},
		nil,
	)
	if err != nil {
		t.Fatalf("enrichCommunitySummaries (nil): %v", err)
	}
	if len(nilScored) != 1 || len(nilScored[0].Entities) != 1 || nilScored[0].Entities[0].ID != "m-a" {
		t.Errorf("nil-scores digest = %v, want a single PageRank rep [m-a]", nilScored[0].Entities)
	}
}
