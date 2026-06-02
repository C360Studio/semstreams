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

func TestClassifierOutput_RoundTripThroughDecoder(t *testing.T) {
	original := &research.ClassifierOutput{
		Topic:      "drone hover anomalies in robotics fleet",
		Tier:       "0",
		Intent:     "",
		Confidence: 1.0,
		Hints: map[string]any{
			"path_intent":     true,
			"path_start_node": "drone-001",
		},
		Candidates: []research.Candidate{
			{
				EntityID:    "acme.ops.robotics.gcs.drone.001",
				Label:       "Drone 001",
				Type:        "drone",
				Relevance:   0.92,
				SnippetText: "Drone 001 reported hover instability at 14:32Z.",
				Tier:        "0",
				Source:      "classifier_chain",
			},
			{
				EntityID:  "acme.ops.robotics.gcs.drone.014",
				Type:      "drone",
				Relevance: 0.71,
				Tier:      "0",
				Source:    "search_graph",
			},
		},
		Degraded:       true,
		DegradedReason: "search_graph fell back to semantic strategy",
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
	got, ok := decoded.Payload().(*research.ClassifierOutput)
	if !ok {
		t.Fatalf("payload: got %T, want *research.ClassifierOutput", decoded.Payload())
	}
	if got.Topic != original.Topic {
		t.Errorf("topic: got %q, want %q", got.Topic, original.Topic)
	}
	if got.Tier != original.Tier {
		t.Errorf("tier: got %q, want %q", got.Tier, original.Tier)
	}
	if !got.Degraded || got.DegradedReason != original.DegradedReason {
		t.Errorf("degraded fields drifted: %+v", got)
	}
	if len(got.Candidates) != len(original.Candidates) {
		t.Fatalf("candidates len: got %d, want %d", len(got.Candidates), len(original.Candidates))
	}
	for i := range original.Candidates {
		if got.Candidates[i].EntityID != original.Candidates[i].EntityID {
			t.Errorf("candidates[%d].EntityID: got %q, want %q", i, got.Candidates[i].EntityID, original.Candidates[i].EntityID)
		}
		if got.Candidates[i].Tier != original.Candidates[i].Tier {
			t.Errorf("candidates[%d].Tier: got %q, want %q", i, got.Candidates[i].Tier, original.Candidates[i].Tier)
		}
	}
}

func TestClassifierOutput_ValidateRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		output  research.ClassifierOutput
		wantSub string
	}{
		{
			name:    "missing topic",
			output:  research.ClassifierOutput{Topic: ""},
			wantSub: "topic required",
		},
		{
			name:    "negative confidence",
			output:  research.ClassifierOutput{Topic: "x", Confidence: -0.1},
			wantSub: "confidence",
		},
		{
			name: "candidate missing entity_id",
			output: research.ClassifierOutput{
				Topic:      "x",
				Candidates: []research.Candidate{{Tier: "0", Source: "classifier_chain"}},
			},
			wantSub: "entity_id required",
		},
		{
			name: "candidate bad tier",
			output: research.ClassifierOutput{
				Topic:      "x",
				Candidates: []research.Candidate{{EntityID: "e", Tier: "bogus", Source: "x"}},
			},
			wantSub: "tier",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.output.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("Validate error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestRouteDecision_RoundTripThroughDecoder(t *testing.T) {
	// Intent-shaped args per ADR-045 Amendment 2026-05-23: model
	// emits axes/focus/scope, backend constructs typed sub-queries.
	original := &research.RouteDecision{
		Action: research.ActionDecompose,
		Args: map[string]any{
			"axes":  []any{"entity_type", "time"},
			"focus": "sensor maintenance events",
			"scope": research.DecomposeScopeMedium,
		},
		Rationale: "Topic spans multiple entity kinds and a 24h window; decompose along entity_type and time.",
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

// --- ParseDecomposeArgs ---

func TestParseDecomposeArgs_HappyPath(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionDecompose,
		Args: map[string]any{
			"axes":  []any{"entity_type", "time"},
			"focus": "drone telemetry anomalies",
			"scope": research.DecomposeScopeNarrow,
		},
	}
	got, err := d.ParseDecomposeArgs()
	if err != nil {
		t.Fatalf("ParseDecomposeArgs error: %v", err)
	}
	if len(got.Axes) != 2 || got.Axes[0] != "entity_type" || got.Axes[1] != "time" {
		t.Errorf("axes drift: got %v", got.Axes)
	}
	if got.Focus != "drone telemetry anomalies" {
		t.Errorf("focus drift: got %q", got.Focus)
	}
	if got.Scope != research.DecomposeScopeNarrow {
		t.Errorf("scope drift: got %q", got.Scope)
	}
}

func TestParseDecomposeArgs_EmptyScopeIsValid(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionDecompose,
		Args: map[string]any{
			"axes":  []any{"entity_type"},
			"focus": "x",
		},
	}
	got, err := d.ParseDecomposeArgs()
	if err != nil {
		t.Fatalf("ParseDecomposeArgs error: %v", err)
	}
	if got.Scope != "" {
		t.Errorf("scope: got %q, want empty", got.Scope)
	}
}

func TestParseDecomposeArgs_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		args    map[string]any
		action  string
		wantSub string
	}{
		{
			name:    "wrong action",
			action:  research.ActionWalkSeeds,
			args:    map[string]any{"axes": []any{"x"}, "focus": "y"},
			wantSub: "action is",
		},
		{
			name:    "missing axes",
			action:  research.ActionDecompose,
			args:    map[string]any{"focus": "x"},
			wantSub: "axes required",
		},
		{
			name:    "empty axis",
			action:  research.ActionDecompose,
			args:    map[string]any{"axes": []any{"x", "   "}, "focus": "f"},
			wantSub: "axes[1] is empty",
		},
		{
			name:    "missing focus",
			action:  research.ActionDecompose,
			args:    map[string]any{"axes": []any{"x"}},
			wantSub: "focus required",
		},
		{
			name:    "invalid scope",
			action:  research.ActionDecompose,
			args:    map[string]any{"axes": []any{"x"}, "focus": "f", "scope": "wide"},
			wantSub: "scope",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &research.RouteDecision{Action: c.action, Args: c.args}
			_, err := d.ParseDecomposeArgs()
			if err == nil {
				t.Fatalf("ParseDecomposeArgs = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

// --- ParseWalkSeedsArgs ---

func TestParseWalkSeedsArgs_HappyPath(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionWalkSeeds,
		Args: map[string]any{
			"seeds": []any{
				map[string]any{"ref": "drone-001", "ref_type": research.SeedRefTypeName},
				map[string]any{"ref": "robotics.gcs.drone", "ref_type": research.SeedRefTypePartialID},
				map[string]any{"ref": "0", "ref_type": research.SeedRefTypeCandidateIndex},
			},
		},
	}
	got, err := d.ParseWalkSeedsArgs()
	if err != nil {
		t.Fatalf("ParseWalkSeedsArgs error: %v", err)
	}
	if len(got.Seeds) != 3 {
		t.Fatalf("want 3 seeds, got %d", len(got.Seeds))
	}
	if got.Seeds[0].Ref != "drone-001" || got.Seeds[0].RefType != research.SeedRefTypeName {
		t.Errorf("seed[0] drift: %+v", got.Seeds[0])
	}
	if got.Seeds[2].RefType != research.SeedRefTypeCandidateIndex {
		t.Errorf("seed[2] ref_type drift: %q", got.Seeds[2].RefType)
	}
}

func TestParseWalkSeedsArgs_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		args    map[string]any
		wantSub string
	}{
		{
			name:    "wrong action",
			action:  research.ActionDecompose,
			args:    map[string]any{"seeds": []any{map[string]any{"ref": "x", "ref_type": "name"}}},
			wantSub: "action is",
		},
		{
			name:    "empty seeds",
			action:  research.ActionWalkSeeds,
			args:    map[string]any{"seeds": []any{}},
			wantSub: "seeds required",
		},
		{
			name:    "missing seeds key",
			action:  research.ActionWalkSeeds,
			args:    map[string]any{},
			wantSub: "seeds required",
		},
		{
			name:    "empty ref",
			action:  research.ActionWalkSeeds,
			args:    map[string]any{"seeds": []any{map[string]any{"ref": "   ", "ref_type": "name"}}},
			wantSub: "ref is empty",
		},
		{
			name:    "bad ref_type",
			action:  research.ActionWalkSeeds,
			args:    map[string]any{"seeds": []any{map[string]any{"ref": "x", "ref_type": "full_id"}}},
			wantSub: "ref_type",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &research.RouteDecision{Action: c.action, Args: c.args}
			_, err := d.ParseWalkSeedsArgs()
			if err == nil {
				t.Fatalf("ParseWalkSeedsArgs = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

// --- ParseRetightenArgs ---

func TestParseRetightenArgs_HappyPath(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionRetighten,
		Args: map[string]any{
			"topic": "drone-001 maintenance events in the last 24 hours",
			"hints": map[string]any{"entity_type": "drone", "time": "24h"},
		},
	}
	got, err := d.ParseRetightenArgs()
	if err != nil {
		t.Fatalf("ParseRetightenArgs error: %v", err)
	}
	if got.Topic != "drone-001 maintenance events in the last 24 hours" {
		t.Errorf("topic drift: %q", got.Topic)
	}
	if got.Hints["entity_type"] != "drone" || got.Hints["time"] != "24h" {
		t.Errorf("hints drift: %v", got.Hints)
	}
}

func TestParseRetightenArgs_NoHintsIsValid(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionRetighten,
		Args:   map[string]any{"topic": "refined topic"},
	}
	got, err := d.ParseRetightenArgs()
	if err != nil {
		t.Fatalf("ParseRetightenArgs error: %v", err)
	}
	if len(got.Hints) != 0 {
		t.Errorf("hints: got %v, want empty", got.Hints)
	}
}

func TestParseRetightenArgs_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		args    map[string]any
		wantSub string
	}{
		{
			name:    "wrong action",
			action:  research.ActionDecompose,
			args:    map[string]any{"topic": "x"},
			wantSub: "action is",
		},
		{
			name:    "missing topic",
			action:  research.ActionRetighten,
			args:    map[string]any{"hints": map[string]any{"k": "v"}},
			wantSub: "topic required",
		},
		{
			name:    "whitespace topic",
			action:  research.ActionRetighten,
			args:    map[string]any{"topic": "   "},
			wantSub: "topic required",
		},
		{
			name:    "empty hint value",
			action:  research.ActionRetighten,
			args:    map[string]any{"topic": "x", "hints": map[string]any{"k": ""}},
			wantSub: "is empty",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &research.RouteDecision{Action: c.action, Args: c.args}
			_, err := d.ParseRetightenArgs()
			if err == nil {
				t.Fatalf("ParseRetightenArgs = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

// --- ExecutionOutput ---

func TestExecutionOutput_RoundTripThroughDecoder(t *testing.T) {
	original := &research.ExecutionOutput{
		Topic:  "drone-001 maintenance events in the last 24 hours",
		Action: research.ActionWalkSeeds,
		Evidence: []research.Evidence{
			{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "walk_seeds.entity_state", Score: 0.95},
			{EntityID: "acme.ops.robotics.gcs.event.maint-001", Tier: "0", Source: "walk_seeds.predicate_walk", Score: 0.82, SnippetText: "scheduled maintenance window"},
			{EntityID: "acme.ops.robotics.gcs.alert.battery", Tier: "1", Source: "walk_seeds.bm25_coverage", Score: 0.55},
		},
		SubQueryCount:    3,
		BudgetTokensUsed: 87,
	}

	envelope := message.NewBaseMessage(original.Schema(), original, "research-execution-roundtrip-test")
	wireBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("registry decode: %v\nwire: %s", err, wireBytes)
	}
	got, ok := decoded.Payload().(*research.ExecutionOutput)
	if !ok {
		t.Fatalf("payload: got %T, want *research.ExecutionOutput", decoded.Payload())
	}
	if got.Topic != original.Topic {
		t.Errorf("topic drift: got %q, want %q", got.Topic, original.Topic)
	}
	if got.Action != original.Action {
		t.Errorf("action drift: got %q, want %q", got.Action, original.Action)
	}
	if len(got.Evidence) != 3 {
		t.Fatalf("evidence len: got %d, want 3", len(got.Evidence))
	}
	if got.Evidence[0].EntityID != original.Evidence[0].EntityID {
		t.Errorf("evidence[0].EntityID drift")
	}
	if got.SubQueryCount != 3 {
		t.Errorf("subquery_count drift: got %d, want 3", got.SubQueryCount)
	}
	if got.BudgetTokensUsed != 87 {
		t.Errorf("budget_tokens_used drift: got %d, want 87", got.BudgetTokensUsed)
	}
}

func TestExecutionOutput_DegradedRoundTrip(t *testing.T) {
	// Degraded + empty-Evidence is a valid execution outcome
	// (assess_sufficiency reads it as low-confidence input).
	// Confirm the degrade flags survive round-trip.
	original := &research.ExecutionOutput{
		Topic:          "x",
		Action:         research.ActionDecompose,
		Degraded:       true,
		DegradedReason: "spatial axis deferred to Phase 2",
		SubQueryCount:  2,
	}
	envelope := message.NewBaseMessage(original.Schema(), original, "test")
	wireBytes, _ := json.Marshal(envelope)
	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := decoded.Payload().(*research.ExecutionOutput)
	if !got.Degraded {
		t.Error("Degraded flag lost on round-trip")
	}
	if got.DegradedReason != "spatial axis deferred to Phase 2" {
		t.Errorf("degraded reason drift: %q", got.DegradedReason)
	}
}

func TestExecutionOutput_ValidateRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		output  research.ExecutionOutput
		wantSub string
	}{
		{
			name:    "missing topic",
			output:  research.ExecutionOutput{Action: research.ActionWalkSeeds},
			wantSub: "topic",
		},
		{
			name:    "missing action",
			output:  research.ExecutionOutput{Topic: "x"},
			wantSub: "action",
		},
		{
			name:    "bogus action",
			output:  research.ExecutionOutput{Topic: "x", Action: "bogus"},
			wantSub: "canonical router action",
		},
		{
			name: "evidence with bad tier",
			output: research.ExecutionOutput{
				Topic:    "x",
				Action:   research.ActionWalkSeeds,
				Evidence: []research.Evidence{{EntityID: "e", Tier: "bogus", Source: "x"}},
			},
			wantSub: "tier",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.output.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("Validate error = %q, want substring %q", err, c.wantSub)
			}
		})
	}
}

// --- enum guards ---

func TestIsValidSeedRefType(t *testing.T) {
	for _, v := range []string{
		research.SeedRefTypeName, research.SeedRefTypePartialID, research.SeedRefTypeCandidateIndex,
	} {
		if !research.IsValidSeedRefType(v) {
			t.Errorf("IsValidSeedRefType(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "full_id", "Name", "candidate-index"} {
		if research.IsValidSeedRefType(v) {
			t.Errorf("IsValidSeedRefType(%q) = true, want false", v)
		}
	}
}

func TestIsValidDecomposeScope(t *testing.T) {
	for _, v := range []string{
		"", research.DecomposeScopeNarrow, research.DecomposeScopeMedium, research.DecomposeScopeBroad,
	} {
		if !research.IsValidDecomposeScope(v) {
			t.Errorf("IsValidDecomposeScope(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"wide", "Narrow", "small"} {
		if research.IsValidDecomposeScope(v) {
			t.Errorf("IsValidDecomposeScope(%q) = true, want false", v)
		}
	}
}
