// Package rule - Tests for per-action MaxIterations firing cap.
package rule

import (
	"context"
	"log/slog"
	"testing"
)

// driveEntries fires the rule's on_enter actions N times by alternating
// the entity's matching state through false→true transitions. Returns
// the persisted MatchState after the final invocation. Each
// false→true transition counts as one rule entry, so each iteration
// of this loop contributes one potential firing of every on_enter
// action — gated by per-action MaxIterations.
func driveEntries(t *testing.T, evaluator *StatefulEvaluator, ruleDef Definition, entityID string, n int) MatchState {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		// Force false → true.
		if _, err := evaluator.Evaluate(ctx, Evaluation{
			Rule:              ruleDef,
			EntityID:          entityID,
			CurrentlyMatching: false,
		}); err != nil {
			t.Fatalf("evaluate exit %d: %v", i, err)
		}
		if _, err := evaluator.Evaluate(ctx, Evaluation{
			Rule:              ruleDef,
			EntityID:          entityID,
			CurrentlyMatching: true,
		}); err != nil {
			t.Fatalf("evaluate enter %d: %v", i, err)
		}
	}
	state, err := evaluator.stateTracker.Get(ctx, ruleDef.ID, entityID)
	if err != nil {
		t.Fatalf("get final state: %v", err)
	}
	return state
}

// TestActionMaxIterations_DefaultCapsAtThree pins the framework
// default semantics: an action with MaxIterations unset fires up to
// DefaultActionMaxIterations (3) times across the rule's match-cycle
// for an entity, then is silently skipped on subsequent rule entries.
// The default-on-cap is the load-bearing user-facing change vs. an
// "unset = unbounded" alternative — semspec/semteams confirmed
// 2026-05-05 that structured-output retries are the rule, not the
// exception, so framework defaults must be safe-by-default.
func TestActionMaxIterations_DefaultCapsAtThree(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-default-cap",
		Type: "expression",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
			// MaxIterations unset → DefaultActionMaxIterations (3)
		},
	}

	// 5 entries, but only 3 should fire (default cap).
	driveEntries(t, evaluator, ruleDef, "ent-1", 5)
	if executor.onEnterCalls != DefaultActionMaxIterations {
		t.Errorf("on_enter fires = %d, want %d (framework default cap)",
			executor.onEnterCalls, DefaultActionMaxIterations)
	}
}

// TestActionMaxIterations_ExplicitOneCap covers the typical operator
// override: an explicit `max_iterations: 1` on a publish_agent that
// should fire exactly once even if the rule re-enters. This is the
// shape semteams's dev-via-spec rule 02 / 04 will use to bound the
// reviewer-rejection retry cycle.
func TestActionMaxIterations_ExplicitOneCap(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-cap-one",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:          ActionTypePublish,
				Subject:       "test.entered",
				MaxIterations: intPtr(1),
			},
		},
	}

	driveEntries(t, evaluator, ruleDef, "ent-1", 5)
	if executor.onEnterCalls != 1 {
		t.Errorf("on_enter fires = %d, want 1 (explicit cap)", executor.onEnterCalls)
	}
}

// TestActionMaxIterations_ExplicitZeroIsUnlimited pins the operator's
// opt-out path: setting `max_iterations: 0` in config means "no cap"
// (legacy behaviour). The pointer-to-zero shape distinguishes this
// explicit choice from the unset-pointer default-of-3.
func TestActionMaxIterations_ExplicitZeroIsUnlimited(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-cap-unlimited",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:          ActionTypePublish,
				Subject:       "test.entered",
				MaxIterations: intPtr(0),
			},
		},
	}

	driveEntries(t, evaluator, ruleDef, "ent-1", 10)
	if executor.onEnterCalls != 10 {
		t.Errorf("on_enter fires = %d, want 10 (explicit 0 = unlimited)", executor.onEnterCalls)
	}
}

// TestActionMaxIterations_PerEntity confirms the cap is scoped to
// (rule, entity) — two different entities running through the same
// rule each get their own counter.
func TestActionMaxIterations_PerEntity(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-per-entity",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:          ActionTypePublish,
				Subject:       "test.entered",
				MaxIterations: intPtr(2),
			},
		},
	}

	// 5 entries on each of two entities. Each entity gets its own
	// counter, so each contributes 2 fires (min(5, cap=2)).
	driveEntries(t, evaluator, ruleDef, "entity-A", 5)
	driveEntries(t, evaluator, ruleDef, "entity-B", 5)
	if executor.onEnterCalls != 4 {
		t.Errorf("on_enter total = %d, want 4 (2 per entity × 2 entities)", executor.onEnterCalls)
	}
}

// TestActionMaxIterations_WhileTrueCap pins the high-frequency path:
// WhileTrue actions fire on every steady-state evaluation while the
// rule remains matching. Without per-action caps, a default-3 cap
// here would burn through in three evaluations and silently disable
// the action thereafter — important to confirm the cap survives the
// (TransitionNone + currentlyMatching) re-evaluation loop, not just
// transition entries.
func TestActionMaxIterations_WhileTrueCap(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	ruleDef := Definition{
		ID:   "rule-while-true-cap",
		Type: "expression",
		WhileTrue: []Action{
			{
				Type:          ActionTypePublish,
				Subject:       "test.while-true",
				MaxIterations: intPtr(2),
			},
		},
	}

	ctx := context.Background()
	// 5 steady-state evaluations while matching. Cap=2 means only
	// the first two should fire.
	for range 5 {
		if _, err := evaluator.Evaluate(ctx, Evaluation{
			Rule:              ruleDef,
			EntityID:          "test.rule.actions.max-iterations.entity.ent-wt",
			CurrentlyMatching: true,
		}); err != nil {
			t.Fatalf("evaluate: %v", err)
		}
	}
	if executor.whileTrueCalls != 2 {
		t.Errorf("while_true fires = %d, want 2 (cap holds across steady-state evaluations)", executor.whileTrueCalls)
	}
}

// TestActionMaxIterations_PersistenceRoundTrip confirms that
// MatchState.ActionIterations survives a marshal/unmarshal through
// the StateTracker. Without this invariant, per-action counters
// would silently reset on process restart and the cap effectively
// only applies within a single evaluator instance.
func TestActionMaxIterations_PersistenceRoundTrip(t *testing.T) {
	bucket := newMockKVBucket()
	tracker := NewStateTracker(bucket, slog.Default())
	ctx := context.Background()

	original := MatchState{
		RuleID:     "rule-persist",
		EntityKey:  "ent-1",
		IsMatching: true,
		ActionIterations: map[string]int{
			"action-a": 2,
			"action-b": 0,
			"action-c": 7,
		},
	}
	if err := tracker.Set(ctx, original); err != nil {
		t.Fatalf("Set: %v", err)
	}

	loaded, err := tracker.Get(ctx, "rule-persist", "ent-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := loaded.ActionIterations["action-a"]; got != 2 {
		t.Errorf("action-a count = %d, want 2 (lost on round-trip)", got)
	}
	if got := loaded.ActionIterations["action-c"]; got != 7 {
		t.Errorf("action-c count = %d, want 7 (lost on round-trip)", got)
	}
}

// TestActionMaxIterations_HotReloadWireRoundTrip pins the JSON wire
// shape definitionFromMap consumes when a rule is hot-reloaded from
// KV config. The pointer-int sentinel (nil = default-3, *0 =
// unlimited, *N>0 = N) MUST round-trip correctly through the
// JSON-marshal-then-unmarshal path the parser uses.
func TestActionMaxIterations_HotReloadWireRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]any
		wantNil bool
		wantVal int
	}{
		{
			name: "unset → nil",
			raw: map[string]any{
				"type":    ActionTypePublish,
				"subject": "test.x",
			},
			wantNil: true,
		},
		{
			name: "explicit 0 → pointer to 0 (unlimited)",
			raw: map[string]any{
				"type":           ActionTypePublish,
				"subject":        "test.x",
				"max_iterations": float64(0),
			},
			wantNil: false,
			wantVal: 0,
		},
		{
			name: "explicit 3 → pointer to 3",
			raw: map[string]any{
				"type":           ActionTypePublish,
				"subject":        "test.x",
				"max_iterations": float64(3),
			},
			wantNil: false,
			wantVal: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ruleMap := map[string]any{
				"type":     "expression",
				"on_enter": []any{tt.raw},
			}
			def, err := definitionFromMap("rule-x", ruleMap)
			if err != nil {
				t.Fatalf("definitionFromMap: %v", err)
			}
			if len(def.OnEnter) != 1 {
				t.Fatalf("on_enter length = %d, want 1", len(def.OnEnter))
			}
			got := def.OnEnter[0].MaxIterations
			if tt.wantNil {
				if got != nil {
					t.Errorf("MaxIterations = %v, want nil (unset)", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("MaxIterations = nil, want non-nil pointer")
			}
			if *got != tt.wantVal {
				t.Errorf("*MaxIterations = %d, want %d", *got, tt.wantVal)
			}
		})
	}
}

// TestActionMaxIterations_ExplicitID lets two distinct actions share
// a counter via author-supplied Action.ID. semteams flagged this as
// the 5%-case escape hatch (e.g., a ping-and-fallback shape where
// two publish_agent actions in different on_enter / on_recovery
// branches should share a budget).
func TestActionMaxIterations_ExplicitID(t *testing.T) {
	bucket := newMockKVBucket()
	executor := &mockActionExecutor{}
	evaluator := NewStatefulEvaluator(NewStateTracker(bucket, slog.Default()), executor, slog.Default())

	// Two publish actions with the SAME explicit ID share a counter.
	ruleDef := Definition{
		ID:   "rule-shared-counter",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:          ActionTypePublish,
				Subject:       "test.entered", // mockActionExecutor counts via Subject
				ID:            "shared-counter",
				MaxIterations: intPtr(2),
			},
			{
				Type:          ActionTypePublish,
				Subject:       "test.entered",
				ID:            "shared-counter",
				MaxIterations: intPtr(2),
			},
		},
	}

	// One rule entry → both actions try to fire. They share counter,
	// so cap=2 means both fire on the first entry (counter goes 0→1
	// then 1→2), and neither fires on the second entry (counter
	// already at 2 when checked).
	driveEntries(t, evaluator, ruleDef, "ent-1", 5)
	if executor.onEnterCalls != 2 {
		t.Errorf("on_enter fires = %d, want 2 (shared counter capped at 2)", executor.onEnterCalls)
	}
}

// TestActionLoopMaxIterations_HotReloadWireRoundTrip extends the
// definitionFromMap hot-reload round-trip coverage above (which pins the
// firing-cap Action.MaxIterations wire shape) to the gh#528
// Action.LoopMaxIterations field — the SPAWNED LOOP's iteration budget,
// deliberately distinct from the firing cap. Unlike MaxIterations'
// pointer-int sentinel, LoopMaxIterations is a plain substitutable
// string, so it carries no float64-vs-int JSON-number ambiguity — but
// operator-reachable config fields still need a round-trip test proving
// the actual Action struct (not a shadow/hand-rolled type) marshals and
// unmarshals the field under its real "loop_max_iterations" JSON tag via
// the hot-reload path config_validation.go's parseActions helper drives
// in production.
func TestActionLoopMaxIterations_HotReloadWireRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
		want string
	}{
		{
			name: "unset stays empty",
			raw: map[string]any{
				"type":    ActionTypePublishAgent,
				"subject": "agent.task.test",
				"role":    "general",
				"model":   "m",
				"prompt":  "p",
			},
			want: "",
		},
		{
			name: "literal value round-trips",
			raw: map[string]any{
				"type":                ActionTypePublishAgent,
				"subject":             "agent.task.test",
				"role":                "general",
				"model":               "m",
				"prompt":              "p",
				"loop_max_iterations": "3",
			},
			want: "3",
		},
		{
			name: "substitution template round-trips verbatim (resolved at fire time, not config-load time)",
			raw: map[string]any{
				"type":                ActionTypePublishAgent,
				"subject":             "agent.task.test",
				"role":                "general",
				"model":               "m",
				"prompt":              "p",
				"loop_max_iterations": "$entity.triple.task.spec.budget",
			},
			want: "$entity.triple.task.spec.budget",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ruleMap := map[string]any{
				"type":     "expression",
				"on_enter": []any{tt.raw},
			}
			def, err := definitionFromMap("rule-loop-max-iter", ruleMap)
			if err != nil {
				t.Fatalf("definitionFromMap: %v", err)
			}
			if len(def.OnEnter) != 1 {
				t.Fatalf("on_enter length = %d, want 1", len(def.OnEnter))
			}
			if got := def.OnEnter[0].LoopMaxIterations; got != tt.want {
				t.Errorf("LoopMaxIterations = %q, want %q", got, tt.want)
			}
		})
	}
}
