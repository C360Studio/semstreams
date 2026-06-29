package fusion

import (
	"strings"
	"testing"
)

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

func TestEvidence_Validate_HappyPaths(t *testing.T) {
	for _, tier := range []string{"0", "1", "2"} {
		t.Run("tier_"+tier, func(t *testing.T) {
			e := &Evidence{EntityID: "x", Tier: tier, Source: "s"}
			if err := e.Validate(); err != nil {
				t.Errorf("Validate(tier=%q) = %v, want nil", tier, err)
			}
		})
	}
}

func TestEvidence_Validate_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		e       Evidence
		wantSub string
	}{
		{"missing entity_id", Evidence{Tier: "0", Source: "x"}, "entity_id"},
		{"missing source", Evidence{EntityID: "e", Tier: "0"}, "source"},
		{"bad tier", Evidence{EntityID: "e", Tier: "bogus", Source: "x"}, "tier"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.e.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q, want substring %q", err, c.wantSub)
			}
		})
	}
}
