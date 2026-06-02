package researchexecute

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
)

// freezeClock pins nowFunc to a deterministic instant so
// temporal-window assertions don't drift across test runs. Returns
// a restore function for test cleanup.
func freezeClock(t *testing.T, at time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return at }
	t.Cleanup(func() { nowFunc = orig })
}

func TestSubQuery_Validate_HappyPaths(t *testing.T) {
	cases := []SubQuery{
		{Type: SubQueryTypeEntityState, Tier: "0", Source: "x", EntityState: &EntityStateArgs{EntityIDs: []string{"a"}}},
		{Type: SubQueryTypePredicateWalk, Tier: "0", Source: "x", PredicateWalk: &PredicateWalkArgs{Seeds: []string{"a"}}},
		{Type: SubQueryTypeTemporalRange, Tier: "0", Source: "x", TemporalRange: &TemporalRangeArgs{Start: "2026-06-02T00:00:00Z", End: "2026-06-03T00:00:00Z"}},
		{Type: SubQueryTypeBM25, Tier: "1", Source: "x", BM25: &BM25Args{Query: "q"}},
	}
	for _, c := range cases {
		t.Run(string(c.Type), func(t *testing.T) {
			if err := c.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestSubQuery_Validate_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		q       SubQuery
		wantSub string
	}{
		{"bad tier", SubQuery{Type: SubQueryTypeBM25, Tier: "2", Source: "x", BM25: &BM25Args{Query: "q"}}, "tier"},
		{"missing source", SubQuery{Type: SubQueryTypeBM25, Tier: "1", BM25: &BM25Args{Query: "q"}}, "source"},
		{"entity_state missing ids", SubQuery{Type: SubQueryTypeEntityState, Tier: "0", Source: "x", EntityState: &EntityStateArgs{}}, "entity_ids"},
		{"predicate_walk missing seeds", SubQuery{Type: SubQueryTypePredicateWalk, Tier: "0", Source: "x", PredicateWalk: &PredicateWalkArgs{}}, "seeds"},
		{"temporal missing start", SubQuery{Type: SubQueryTypeTemporalRange, Tier: "0", Source: "x", TemporalRange: &TemporalRangeArgs{End: "x"}}, "start"},
		{"bm25 empty query", SubQuery{Type: SubQueryTypeBM25, Tier: "1", Source: "x", BM25: &BM25Args{Query: "  "}}, "query"},
		{"unknown type", SubQuery{Type: "neural", Tier: "0", Source: "x"}, "Phase 1 primitive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.q.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

// --- materializeForWalkSeeds ---

func TestMaterializeForWalkSeeds_WithSeedsProducesThreeQueries(t *testing.T) {
	intent := &research.Intent{Topic: "drone status"}
	resolved := []string{"acme.ops.drone.001", "acme.ops.drone.002"}
	queries := materializeForWalkSeeds(intent, resolved)
	if len(queries) != 3 {
		t.Fatalf("want 3 queries (entity_state + predicate_walk + bm25), got %d", len(queries))
	}
	// Validate each.
	for i, q := range queries {
		if err := q.Validate(); err != nil {
			t.Errorf("query[%d] validate: %v", i, err)
		}
	}
	// First should be entity_state with both seeds.
	if queries[0].Type != SubQueryTypeEntityState {
		t.Errorf("queries[0].Type = %q, want entity_state", queries[0].Type)
	}
	if len(queries[0].EntityState.EntityIDs) != 2 {
		t.Errorf("entity_state ids: got %d, want 2", len(queries[0].EntityState.EntityIDs))
	}
	// Second should be predicate_walk.
	if queries[1].Type != SubQueryTypePredicateWalk {
		t.Errorf("queries[1].Type = %q, want predicate_walk", queries[1].Type)
	}
	// Third should be BM25 on topic.
	if queries[2].Type != SubQueryTypeBM25 {
		t.Errorf("queries[2].Type = %q, want bm25", queries[2].Type)
	}
	if queries[2].BM25.Query != "drone status" {
		t.Errorf("bm25 query: got %q, want %q", queries[2].BM25.Query, "drone status")
	}
}

func TestMaterializeForWalkSeeds_NoSeedsShipsBM25Only(t *testing.T) {
	intent := &research.Intent{Topic: "x"}
	queries := materializeForWalkSeeds(intent, nil)
	if len(queries) != 1 {
		t.Fatalf("want 1 query (bm25 fallback), got %d", len(queries))
	}
	if queries[0].Type != SubQueryTypeBM25 {
		t.Errorf("fallback type = %q, want bm25", queries[0].Type)
	}
}

// --- materializeForDecompose ---

func TestMaterializeForDecompose_EntityTypeAxisAnchorsOnCandidates(t *testing.T) {
	intent := &research.Intent{Topic: "x"}
	args := research.DecomposeArgs{Axes: []string{"entity_type"}, Focus: "drones", Scope: research.DecomposeScopeMedium}
	candidates := []research.Candidate{
		{EntityID: "id-1", Tier: "0", Source: "x"},
		{EntityID: "id-2", Tier: "0", Source: "x"},
	}
	queries := materializeForDecompose(intent, args, candidates)
	// Should produce: BM25 (always) + entity_state (entity_type axis with candidates).
	if len(queries) != 2 {
		t.Fatalf("want 2 queries, got %d", len(queries))
	}
	var sawEntityState bool
	for _, q := range queries {
		if q.Type == SubQueryTypeEntityState {
			sawEntityState = true
			if len(q.EntityState.EntityIDs) != 2 {
				t.Errorf("entity_state should have 2 candidate IDs, got %d", len(q.EntityState.EntityIDs))
			}
		}
	}
	if !sawEntityState {
		t.Errorf("entity_type axis with candidates should produce entity_state subquery; got %+v", queries)
	}
}

func TestMaterializeForDecompose_MultiAxisNoCandidatesShipsOnlyBM25(t *testing.T) {
	// Multiple Tier 0 axes (entity_type + predicate) with no
	// candidates to anchor — both branches must cleanly skip
	// without emitting empty-args sub-queries that would trip
	// Validate at executor dispatch. Only the universal BM25 fires.
	intent := &research.Intent{Topic: "x"}
	args := research.DecomposeArgs{Axes: []string{"entity_type", "predicate"}, Focus: "f"}
	queries := materializeForDecompose(intent, args, nil)
	if len(queries) != 1 || queries[0].Type != SubQueryTypeBM25 {
		t.Errorf("multi-axis no-candidates should ship only BM25; got %+v", queries)
	}
}

func TestMaterializeForDecompose_EntityTypeAxisNoCandidatesSkipsTier0(t *testing.T) {
	intent := &research.Intent{Topic: "x"}
	args := research.DecomposeArgs{Axes: []string{"entity_type"}, Focus: "drones"}
	queries := materializeForDecompose(intent, args, nil)
	// Only BM25 should fire (no Tier 0 anchor).
	if len(queries) != 1 || queries[0].Type != SubQueryTypeBM25 {
		t.Errorf("entity_type with no candidates should ship only BM25; got %+v", queries)
	}
}

func TestMaterializeForDecompose_TimeAxisProducesTemporalRange(t *testing.T) {
	// Freeze clock for deterministic window assertion.
	freezeClock(t, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC))
	intent := &research.Intent{Topic: "x"}
	args := research.DecomposeArgs{Axes: []string{"time"}, Focus: "events"}
	queries := materializeForDecompose(intent, args, nil)
	var temporal *SubQuery
	for i := range queries {
		if queries[i].Type == SubQueryTypeTemporalRange {
			temporal = &queries[i]
			break
		}
	}
	if temporal == nil {
		t.Fatal("time axis should produce temporal_range query")
	}
	// ±24h around frozen now: 2026-06-01T12 → 2026-06-03T12.
	if !strings.HasPrefix(temporal.TemporalRange.Start, "2026-06-01T12:") {
		t.Errorf("temporal start should be -24h from frozen now: %q", temporal.TemporalRange.Start)
	}
	if !strings.HasPrefix(temporal.TemporalRange.End, "2026-06-03T12:") {
		t.Errorf("temporal end should be +24h from frozen now: %q", temporal.TemporalRange.End)
	}
}

func TestMaterializeForDecompose_SpatialAxisDeferredButBM25Survives(t *testing.T) {
	intent := &research.Intent{Topic: "drones at LAX"}
	args := research.DecomposeArgs{Axes: []string{"spatial"}, Focus: "drone positions"}
	queries := materializeForDecompose(intent, args, nil)
	// spatial template defers — only the universal BM25 fires.
	if len(queries) != 1 || queries[0].Type != SubQueryTypeBM25 {
		t.Errorf("spatial deferred should ship only BM25; got %+v", queries)
	}
}

func TestMaterializeForDecompose_UnknownAxisFallsBackToBM25(t *testing.T) {
	intent := &research.Intent{Topic: "x"}
	args := research.DecomposeArgs{Axes: []string{"weather"}, Focus: "f"}
	queries := materializeForDecompose(intent, args, nil)
	// Two BM25s: universal + axis-suffixed fallback.
	bm25Count := 0
	for _, q := range queries {
		if q.Type == SubQueryTypeBM25 {
			bm25Count++
		}
	}
	if bm25Count != 2 {
		t.Errorf("unknown axis should produce universal BM25 + axis-suffixed BM25; got %d BM25s in %+v", bm25Count, queries)
	}
}

func TestMaterializeForDecompose_ScopeCapsCandidateAnchoring(t *testing.T) {
	intent := &research.Intent{Topic: "x"}
	candidates := make([]research.Candidate, 30)
	for i := range candidates {
		candidates[i] = research.Candidate{EntityID: "c-" + string(rune('a'+i)), Tier: "0", Source: "x"}
	}
	cases := []struct {
		scope     string
		wantCount int
	}{
		{research.DecomposeScopeNarrow, 3},
		{research.DecomposeScopeMedium, 8},
		{research.DecomposeScopeBroad, 20},
		{"", 8}, // empty → medium
	}
	for _, c := range cases {
		t.Run("scope_"+c.scope, func(t *testing.T) {
			args := research.DecomposeArgs{Axes: []string{"entity_type"}, Focus: "f", Scope: c.scope}
			queries := materializeForDecompose(intent, args, candidates)
			for _, q := range queries {
				if q.Type == SubQueryTypeEntityState {
					if len(q.EntityState.EntityIDs) != c.wantCount {
						t.Errorf("scope=%q: entity_state count = %d, want %d", c.scope, len(q.EntityState.EntityIDs), c.wantCount)
					}
					return
				}
			}
			t.Errorf("scope=%q: no entity_state subquery produced", c.scope)
		})
	}
}

// --- seed reference resolvers ---

func TestResolveSeedRefByCandidateIndex(t *testing.T) {
	candidates := []research.Candidate{
		{EntityID: "id-0", Tier: "0", Source: "x"},
		{EntityID: "id-1", Tier: "0", Source: "x"},
	}
	cases := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{"valid 0", "0", "id-0", false},
		{"valid 1", "1", "id-1", false},
		{"out of bounds", "5", "", true},
		{"negative", "-1", "", true},
		{"non-int", "abc", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveSeedRefByCandidateIndex(c.ref, candidates)
			if c.wantErr {
				if err == nil {
					t.Errorf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveSeedRefByPartialID(t *testing.T) {
	candidates := []research.Candidate{
		{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "x"},
		{EntityID: "acme.ops.robotics.gcs.drone.002", Tier: "0", Source: "x"},
		{EntityID: "acme.ops.industrial.factory.sensor.555", Tier: "0", Source: "x"},
	}
	cases := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{"suffix match", "drone.001", "acme.ops.robotics.gcs.drone.001", false},
		{"longer suffix", "gcs.drone.002", "acme.ops.robotics.gcs.drone.002", false},
		{"exact match", "acme.ops.industrial.factory.sensor.555", "acme.ops.industrial.factory.sensor.555", false},
		{"no match", "octopus.tentacle.001", "", true},
		{"empty", "", "", true},
		{"ambiguous prefix match shouldn't fire as suffix", "robotics", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveSeedRefByPartialID(c.ref, candidates)
			if c.wantErr {
				if err == nil {
					t.Errorf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
