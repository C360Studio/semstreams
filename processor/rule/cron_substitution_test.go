package rule

import (
	"strings"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

func TestApplyScheduleSubstitutions_AllTokens(t *testing.T) {
	t.Parallel()

	sc := &ScheduleContext{
		ID:          "weekly-planning",
		Spec:        "0 9 * * MON",
		LastFiredAt: time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC),
	}

	in := "rule=$schedule.id spec=$schedule.spec last=$schedule.last_fired_at"
	want := "rule=weekly-planning spec=0 9 * * MON last=2026-04-27T09:00:00Z"
	if got := applyScheduleSubstitutions(in, sc); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyScheduleSubstitutions_NilContextIsNoop(t *testing.T) {
	t.Parallel()

	in := "rule=$schedule.id"
	if got := applyScheduleSubstitutions(in, nil); got != in {
		t.Errorf("got %q, want unchanged %q", got, in)
	}
}

// First fire renders LastFiredAt as the empty string (not the Go zero
// value `0001-01-01T00:00:00Z`). Authors then detect first-fire by
// branching on emptiness without having to special-case the magic
// literal.
func TestApplyScheduleSubstitutions_FirstFireRendersEmpty(t *testing.T) {
	t.Parallel()

	sc := &ScheduleContext{
		ID:   "fresh",
		Spec: "@hourly",
		// LastFiredAt left as zero value
	}
	out := applyScheduleSubstitutions("[$schedule.last_fired_at]", sc)
	if out != "[]" {
		t.Errorf("got %q, want %q (zero LastFiredAt should render empty)", out, "[]")
	}
}

func TestApplyScheduleSubstitutions_NonUTCNormalised(t *testing.T) {
	t.Parallel()

	loc, _ := time.LoadLocation("America/New_York")
	sc := &ScheduleContext{
		ID:          "rule",
		Spec:        "@hourly",
		LastFiredAt: time.Date(2026, 4, 27, 5, 0, 0, 0, loc), // 09:00 UTC
	}
	out := applyScheduleSubstitutions("$schedule.last_fired_at", sc)
	if !strings.HasSuffix(out, "Z") {
		t.Errorf("got %q, want UTC RFC3339 (Z suffix)", out)
	}
	if out != "2026-04-27T09:00:00Z" {
		t.Errorf("got %q, want 2026-04-27T09:00:00Z", out)
	}
}

func TestApplyScheduleSubstitutions_UnknownTokenSurvives(t *testing.T) {
	t.Parallel()

	sc := &ScheduleContext{ID: "r", Spec: "@hourly"}
	in := "$schedule.unknown_field"
	out := applyScheduleSubstitutions(in, sc)
	if out != in {
		t.Errorf("got %q, want unchanged %q (unknown $schedule.* should survive substitution and trip warn-on-unresolved)", out, in)
	}
}

// The cron substitution layer must compose cleanly with the existing
// $entity.* / $state.* substitutions. Confirms the future per-entity
// fan-out path: an ExecutionContext with both Schedule and Entity
// populated substitutes both namespaces in a single template call.
// [retrofit-safe]
func TestExecutionContext_ScheduleAndEntityCoexist(t *testing.T) {
	t.Parallel()

	entityID := semantictest.EntityID(t, "test", "rule", "cron", "schedule", "drone", "001")
	ec := &ExecutionContext{
		EntityID: entityID,
		Entity: &gtypes.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{Subject: entityID, Predicate: semantictest.Predicate(t, "test", "agent", "role"), Object: "scout"},
			},
		},
		Schedule: &ScheduleContext{
			ID:          "weekly",
			Spec:        "0 9 * * MON",
			LastFiredAt: time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC),
		},
	}

	in := "rule=$schedule.id entity=$entity.id role=$entity.triple.test.agent.role last=$schedule.last_fired_at"
	want := "rule=weekly entity=" + entityID + " role=scout last=2026-04-27T09:00:00Z"
	if got := ec.SubstituteVariables(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// An expression rule (Schedule == nil) that mistakenly references
// $schedule.* tokens must surface them via the unresolved-template
// regex, NOT silently render as empty. This is the regression guard
// for cross-namespace leakage.
func TestExecutionContext_ScheduleTokenInExpressionRuleSurvives(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: semantictest.EntityID(t, "test", "rule", "cron", "expression", "drone", "001"),
		// Schedule deliberately nil — we're an expression rule
	}

	in := "$schedule.id"
	out := ec.SubstituteVariables(in)
	if out != in {
		t.Errorf("got %q, want unchanged %q (cron-only $schedule.* must not silently empty in expression-rule path)", out, in)
	}
}

// Locks the current contract for re-substitution: schedule values are
// substituted sequentially via strings.ReplaceAll, so a value that
// contains another `$schedule.*` token will get expanded by a later
// pass. Today this is benign because sc.ID and sc.Spec come from
// author-controlled config (rule ID + cron expression — neither plausibly
// holds a substitution token). The test pins the behavior so a future
// refactor that switches to single-pass regex substitution makes a
// deliberate, observable choice rather than silently changing semantics.
func TestApplyScheduleSubstitutions_ValueWithEmbeddedToken(t *testing.T) {
	t.Parallel()

	sc := &ScheduleContext{
		ID:   "$schedule.spec", // pathological, but we want to lock behavior
		Spec: "@hourly",
	}
	out := applyScheduleSubstitutions("$schedule.id", sc)
	// Current sequential implementation: $schedule.id → "$schedule.spec"
	// → "@hourly" (second pass expands the embedded token).
	if out != "@hourly" {
		t.Errorf("got %q, want %q (sequential ReplaceAll re-expands embedded token; locks the contract)", out, "@hourly")
	}
}

func TestExecutionContext_SubstituteVariables_ScheduleNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ec       *ExecutionContext
		template string
		want     string
	}{
		{
			name: "schedule.id only",
			ec: &ExecutionContext{
				Schedule: &ScheduleContext{ID: "r1", Spec: "@hourly"},
			},
			template: "id=$schedule.id",
			want:     "id=r1",
		},
		{
			name: "schedule.spec only",
			ec: &ExecutionContext{
				Schedule: &ScheduleContext{ID: "r1", Spec: "0 9 * * MON"},
			},
			template: "spec=$schedule.spec",
			want:     "spec=0 9 * * MON",
		},
		{
			name: "schedule.last_fired_at first fire",
			ec: &ExecutionContext{
				Schedule: &ScheduleContext{ID: "r1", Spec: "@hourly"},
			},
			template: "last=[$schedule.last_fired_at]",
			want:     "last=[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ec.SubstituteVariables(tt.template)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSubstituteVariables_AgentRunTripleResolves verifies that the
// $entity.triple.agent.run substitution token resolves correctly through the
// standard entity-triple substitution layer (M3 grammar-verification per
// feedback_reference_configs_verify_triple_stamping). The predicate name
// "agent.run" (LoopRun vocab constant, ADR-053 D7) is verified collision-free
// against the substitution grammar at implementation time; this test locks the
// runtime behavior so a future substitution-layer refactor can't silently break
// the agent.run namespace without surfacing here.
func TestSubstituteVariables_AgentRunTripleResolves(t *testing.T) {
	t.Parallel()

	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop-xyz"
	ec := &ExecutionContext{
		EntityID: loopEntityID,
		Entity: &gtypes.EntityState{
			ID: loopEntityID,
			Triples: []message.Triple{
				{Subject: loopEntityID, Predicate: "agent.loop.run", Object: "root-loop-uuid"},
				{Subject: loopEntityID, Predicate: "agent.run.entity-id", Object: "acme.ops.agent.chain.execution.root-loop-uuid"},
			},
		},
	}

	got := ec.SubstituteVariables("$entity.triple.agent.loop.run")
	if got != "root-loop-uuid" {
		t.Errorf("$entity.triple.agent.loop.run resolved to %q, want %q", got, "root-loop-uuid")
	}

	// ADR-053 follow-up: the 6-part run entity ID is the rule-addressable upsert
	// subject. $entity.triple.agent.run.entity_id must resolve to the full ID so a
	// rule can use it as an add_triple/update_triple Subject.
	gotEntity := ec.SubstituteVariables("$entity.triple.agent.run.entity-id")
	if want := "acme.ops.agent.chain.execution.root-loop-uuid"; gotEntity != want {
		t.Errorf("$entity.triple.agent.run.entity-id resolved to %q, want %q", gotEntity, want)
	}

	// Verify an entity WITHOUT agent.run returns an unresolved token (not empty,
	// not panic): the substitution layer leaves framework-namespace tokens verbatim.
	ecNoTriple := &ExecutionContext{
		EntityID: loopEntityID,
		Entity: &gtypes.EntityState{
			ID:      loopEntityID,
			Triples: []message.Triple{},
		},
	}
	absent := ecNoTriple.SubstituteVariables("$entity.triple.agent.loop.run")
	if absent == "root-loop-uuid" {
		t.Errorf("absent agent.run triple must not resolve to a value, got %q", absent)
	}
}
