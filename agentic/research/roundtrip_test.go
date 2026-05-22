package research_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

// Production-decoder round-trip tests for the three ADR-045 payload
// types per feedback_production_decoder_round_trip_required. Uses the
// real payloadbuiltins.NewTestDecoder rather than an anonymous shape-
// cast so a registry regression (forgotten RegisterPayloads wire-up,
// domain/category mismatch) surfaces here, not in downstream PRs.

func TestResearchIntent_RoundTripThroughDecoder(t *testing.T) {
	original := &research.Intent{
		Topic: "drone hover anomalies in robotics fleet",
		Hints: map[string]string{
			"entity_kind": "drone",
			"domain":      "robotics",
			"recency":     "last_24h",
		},
		BudgetTokens:  8000,
		MaxIterations: 7,
	}

	envelope := message.NewBaseMessage(original.Schema(), original, "research-roundtrip-test")
	wireBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("registry decode: %v\nwire: %s", err, wireBytes)
	}

	if got := decoded.Type(); got != original.Schema() {
		t.Errorf("type: got %+v, want %+v", got, original.Schema())
	}
	got, ok := decoded.Payload().(*research.Intent)
	if !ok {
		t.Fatalf("payload: got %T, want *research.Intent", decoded.Payload())
	}
	if got.Topic != original.Topic {
		t.Errorf("topic: got %q, want %q", got.Topic, original.Topic)
	}
	if got.BudgetTokens != original.BudgetTokens {
		t.Errorf("budget_tokens: got %d, want %d", got.BudgetTokens, original.BudgetTokens)
	}
	if got.MaxIterations != original.MaxIterations {
		t.Errorf("max_iterations: got %d, want %d", got.MaxIterations, original.MaxIterations)
	}
	if len(got.Hints) != len(original.Hints) {
		t.Errorf("hints len: got %d, want %d", len(got.Hints), len(original.Hints))
	}
	for k, v := range original.Hints {
		if got.Hints[k] != v {
			t.Errorf("hints[%q]: got %q, want %q", k, got.Hints[k], v)
		}
	}
}

func TestSearchResult_RoundTripThroughDecoder(t *testing.T) {
	original := &research.SearchResult{
		Evidence: []research.Evidence{
			{
				EntityID:    "acme.ops.robotics.gcs.drone.001",
				Tier:        "0",
				Source:      "classifier",
				Score:       0.92,
				SnippetText: "Drone 001 reported hover instability at 14:32Z.",
			},
			{
				EntityID:       "acme.ops.robotics.gcs.drone.014",
				Tier:           "1",
				Source:         "bm25_index",
				Score:          0.71,
				ObjectStoreRef: "objstore://research/evidence-014.txt",
			},
		},
		Synthesis: "Two drones in the GCS fleet showed hover anomalies within the last 24 hours; drone.001 (Tier 0, classifier) and drone.014 (Tier 1, BM25). See evidence refs.",
		DecompTrace: &research.DecompTrace{
			RouterAction:    research.ActionWalkSeeds,
			SeedEntities:    []string{"acme.ops.robotics.gcs.drone.001"},
			RetightenRounds: 0,
		},
		TokensUsed: 1234,
		Iterations: 2,
	}

	envelope := message.NewBaseMessage(original.Schema(), original, "research-roundtrip-test")
	wireBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("registry decode: %v\nwire: %s", err, wireBytes)
	}

	if got := decoded.Type(); got != original.Schema() {
		t.Errorf("type: got %+v, want %+v", got, original.Schema())
	}
	got, ok := decoded.Payload().(*research.SearchResult)
	if !ok {
		t.Fatalf("payload: got %T, want *research.SearchResult", decoded.Payload())
	}
	if got.Synthesis != original.Synthesis {
		t.Errorf("synthesis drift: got %q, want %q", got.Synthesis, original.Synthesis)
	}
	if len(got.Evidence) != len(original.Evidence) {
		t.Fatalf("evidence len: got %d, want %d", len(got.Evidence), len(original.Evidence))
	}
	for i := range original.Evidence {
		if got.Evidence[i].EntityID != original.Evidence[i].EntityID {
			t.Errorf("evidence[%d].EntityID: got %q, want %q", i, got.Evidence[i].EntityID, original.Evidence[i].EntityID)
		}
		if got.Evidence[i].Tier != original.Evidence[i].Tier {
			t.Errorf("evidence[%d].Tier: got %q, want %q", i, got.Evidence[i].Tier, original.Evidence[i].Tier)
		}
	}
	if got.DecompTrace == nil {
		t.Fatal("decomp_trace lost on round-trip")
	}
	if got.DecompTrace.RouterAction != research.ActionWalkSeeds {
		t.Errorf("router_action: got %q, want %q", got.DecompTrace.RouterAction, research.ActionWalkSeeds)
	}
	if got.TokensUsed != original.TokensUsed {
		t.Errorf("tokens_used: got %d, want %d", got.TokensUsed, original.TokensUsed)
	}
	if got.Iterations != original.Iterations {
		t.Errorf("iterations: got %d, want %d", got.Iterations, original.Iterations)
	}
}

func TestRouteDecision_RoundTripThroughDecoder(t *testing.T) {
	original := &research.RouteDecision{
		Action: research.ActionDecompose,
		Args: map[string]any{
			"sub_queries": []any{
				map[string]any{"type": "predicate_walk", "predicate": "sosa.observes"},
				map[string]any{"type": "temporal_range", "start": "2026-05-22T00:00:00Z", "end": "2026-05-22T23:59:59Z"},
			},
		},
		Rationale: "Topic spans multiple entity kinds and a 24h window; decompose into a predicate walk plus a temporal range and fuse.",
	}

	envelope := message.NewBaseMessage(original.Schema(), original, "research-roundtrip-test")
	wireBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("registry decode: %v\nwire: %s", err, wireBytes)
	}

	got, ok := decoded.Payload().(*research.RouteDecision)
	if !ok {
		t.Fatalf("payload: got %T, want *research.RouteDecision", decoded.Payload())
	}
	if got.Action != original.Action {
		t.Errorf("action: got %q, want %q", got.Action, original.Action)
	}
	if got.Rationale != original.Rationale {
		t.Errorf("rationale drift: got %q, want %q", got.Rationale, original.Rationale)
	}
	if got.Args == nil {
		t.Fatal("args lost on round-trip")
	}
}

func TestResearchIntent_ValidateRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		intent  research.Intent
		wantSub string
	}{
		{
			name:    "missing topic",
			intent:  research.Intent{Topic: ""},
			wantSub: "topic required",
		},
		{
			name:    "whitespace topic",
			intent:  research.Intent{Topic: "   "},
			wantSub: "topic required",
		},
		{
			name:    "empty hint value",
			intent:  research.Intent{Topic: "x", Hints: map[string]string{"k": ""}},
			wantSub: "is empty",
		},
		{
			name:    "negative budget",
			intent:  research.Intent{Topic: "x", BudgetTokens: -1},
			wantSub: "budget_tokens must be >= 0",
		},
		{
			name:    "negative max iterations",
			intent:  research.Intent{Topic: "x", MaxIterations: -1},
			wantSub: "max_iterations must be >= 0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.intent.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("Validate error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestResearchIntent_DefaultsResolvedAtUse(t *testing.T) {
	p := &research.Intent{Topic: "x"}
	if got := p.ResolvedBudgetTokens(); got != research.DefaultBudgetTokens {
		t.Errorf("ResolvedBudgetTokens with zero: got %d, want %d", got, research.DefaultBudgetTokens)
	}
	if got := p.ResolvedMaxIterations(); got != research.DefaultMaxIterations {
		t.Errorf("ResolvedMaxIterations with zero: got %d, want %d", got, research.DefaultMaxIterations)
	}

	q := &research.Intent{Topic: "x", BudgetTokens: 99, MaxIterations: 11}
	if got := q.ResolvedBudgetTokens(); got != 99 {
		t.Errorf("ResolvedBudgetTokens override: got %d, want 99", got)
	}
	if got := q.ResolvedMaxIterations(); got != 11 {
		t.Errorf("ResolvedMaxIterations override: got %d, want 11", got)
	}
}

func TestRouteDecision_ValidateEnforcesClosedActionEnum(t *testing.T) {
	validActions := []string{
		research.ActionSynthesizeDirectly,
		research.ActionRetighten,
		research.ActionWalkSeeds,
		research.ActionDecompose,
	}
	for _, a := range validActions {
		t.Run("accept_"+a, func(t *testing.T) {
			p := &research.RouteDecision{Action: a}
			if err := p.Validate(); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", a, err)
			}
		})
	}

	invalidActions := []string{"", "bogus", "Synthesize_Directly", "decompose ", "synthesise_directly"}
	for _, a := range invalidActions {
		t.Run("reject_"+a, func(t *testing.T) {
			p := &research.RouteDecision{Action: a}
			err := p.Validate()
			if err == nil {
				t.Errorf("Validate(%q) = nil, want error", a)
			}
		})
	}
}

func TestRouteDecision_UnmarshalRejectsInvalidAction(t *testing.T) {
	const raw = `{"action":"bogus","rationale":"won't decode"}`
	var d research.RouteDecision
	if err := json.Unmarshal([]byte(raw), &d); err == nil {
		t.Errorf("Unmarshal accepted bogus action; want decode error")
	}
}

func TestRouteDecision_UnmarshalAcceptsEmptyAction(t *testing.T) {
	// A partially-populated payload (no Action) should decode clean;
	// strict-emit validation lives in Validate, not UnmarshalJSON.
	// Tests + partial-update flows depend on this.
	const raw = `{"rationale":"placeholder; action filled in later"}`
	var d research.RouteDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Errorf("Unmarshal of empty-action payload failed: %v", err)
	}
}

func TestEvidence_ValidateAcceptsPhase2Tier(t *testing.T) {
	// Tier "2" is reserved for Phase 2 neural retrieval but accepted
	// at the schema level today so Phase 1 fixtures can probe
	// forward-compat without churning the validator when Phase 2
	// lands. Lock in the acceptance so a future contributor doesn't
	// tighten the enum without realising Phase 2 fixtures depend on it.
	for _, tier := range []string{"0", "1", "2"} {
		t.Run("tier_"+tier, func(t *testing.T) {
			e := &research.Evidence{EntityID: "x", Tier: tier, Source: "s"}
			if err := e.Validate(); err != nil {
				t.Errorf("Validate(tier=%q) = %v, want nil", tier, err)
			}
		})
	}
}

func TestSearchResult_ValidateRejectsInvalidEvidence(t *testing.T) {
	cases := []struct {
		name    string
		result  research.SearchResult
		wantSub string
	}{
		{
			name:    "missing synthesis",
			result:  research.SearchResult{Synthesis: ""},
			wantSub: "synthesis required",
		},
		{
			name: "evidence missing entity_id",
			result: research.SearchResult{
				Synthesis: "x",
				Evidence:  []research.Evidence{{Tier: "0", Source: "classifier"}},
			},
			wantSub: "entity_id required",
		},
		{
			name: "evidence bad tier",
			result: research.SearchResult{
				Synthesis: "x",
				Evidence:  []research.Evidence{{EntityID: "e", Tier: "bogus", Source: "x"}},
			},
			wantSub: "tier",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.result.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("Validate error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}
