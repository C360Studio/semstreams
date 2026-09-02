package agentic

import (
	"net/http"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// TestStagesAreExactlyThisOrderedList is the guard on the stage list itself.
//
// A count alone cannot carry it: deleting a stage and decrementing the number
// beside it agrees with itself and stays green, which is the shape of a
// coverage claim that quietly shrinks. Naming every stage in order, with the
// asserts flag that decides whether it is counted, makes a removal, a rename,
// a reorder, or a silently-uncounted stage fail in plain `go test`.
//
// The order is load-bearing, not decorative: the approval and signal walks
// spawn loops whose metrics and stream traffic would perturb the counter
// assertions above them, and verify-durable-tool-replay faults the TOOL stream
// for the duration of its own stage.
func TestStagesAreExactlyThisOrderedList(t *testing.T) {
	want := []struct {
		name    string
		asserts bool
	}{
		{"verify-components", true},
		{"capture-baseline", false},
		{"inject-task", false},
		{"wait-for-completion", true},
		{"verify-terminal-response", true},
		{"validate-trajectory", true},
		{"verify-graph-triples", true},
		{"verify-tool-execution", true},
		{"verify-durable-tool-replay", true},
		{"verify-streaming-metrics", true},
		{"verify-tool-call-governance", true},
		{"walk-approval-path", true},
		{"refuse-non-canonical-approval", true},
		{"walk-signal-path", true},
		{"refuse-non-canonical-signal", true},
		{"validate-results", true},
	}

	scenario := NewScenario(nil, DefaultConfig())
	got := scenario.stages()
	if len(got) != len(want) {
		names := make([]string, len(got))
		for i, stage := range got {
			names[i] = stage.name
		}
		t.Fatalf("stages() ran %d stages %v, want %d", len(got), names, len(want))
	}
	for i := range want {
		if got[i].name != want[i].name || got[i].asserts != want[i].asserts {
			t.Errorf("stage %d = %s(asserts=%v), want %s(asserts=%v)",
				i, got[i].name, got[i].asserts, want[i].name, want[i].asserts)
		}
	}

	// The number Execute pins assertions_run against is derived from the same
	// list, so this holds the derivation against the expectation above rather
	// than against a second hand-maintained number.
	wantCounted := 0
	for _, stage := range want {
		if stage.asserts {
			wantCounted++
		}
	}
	if got := scenario.assertingStageCount(); got != wantCounted {
		t.Errorf("assertingStageCount() = %d, want %d", got, wantCounted)
	}
}

func TestNewApprovalGatedTaskForcesTheGatedTool(t *testing.T) {
	task := newApprovalGatedTask(time.Unix(0, 7), "approval", approvalLoopOwner)

	if len(task.Tools) != 1 || task.Tools[0].Name != approvalGatedTool {
		t.Fatalf("tools = %#v, want only %s advertised", task.Tools, approvalGatedTool)
	}
	if task.ToolChoice == nil || task.ToolChoice.FunctionName != approvalGatedTool {
		t.Fatalf("tool choice = %#v, want forced %s", task.ToolChoice, approvalGatedTool)
	}
	if task.UserID != approvalLoopOwner {
		t.Fatalf("user_id = %q, want %q — the cancel lane's ownership check reads it", task.UserID, approvalLoopOwner)
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("task validation failed: %v", err)
	}
}

// TestNonCanonicalTokenIsRefusedByTheMintPredicate pins the refusal fixture
// against the production predicate rather than against this test's belief about
// it: if looptoken ever accepted the uppercase spelling, the e2e refusal
// assertions would be asserting an admit.
func TestNonCanonicalTokenIsRefusedByTheMintPredicate(t *testing.T) {
	minted := pagedLoopToken

	if !looptoken.Valid(minted) {
		t.Fatalf("fixture %q is not a canonical loop token", minted)
	}
	spoiled := nonCanonicalToken(minted)
	if spoiled == minted {
		t.Fatal("nonCanonicalToken() returned the canonical spelling unchanged")
	}
	if looptoken.Valid(spoiled) {
		t.Fatalf("nonCanonicalToken(%q) = %q, which the framework still accepts", minted, spoiled)
	}
}

func TestApprovalRequesterIsNotTheLoopOwner(t *testing.T) {
	if approvalRequester == approvalLoopOwner {
		t.Fatal("the approval walk must submit as a second party; ownership is not consulted for approve")
	}
}

func TestValidateResultsRequiresBothWalks(t *testing.T) {
	complete := func() map[string]any {
		return map[string]any{
			"completion_method":                     "target_trajectory",
			"approval_outcome":                      agentic.OutcomeSuccess,
			"signal_outcome":                        agentic.OutcomeCancelled,
			"approval_refusal_non_canonical_status": http.StatusBadRequest,
			"signal_refusal_non_canonical_count":    float64(1),
		}
	}
	s := NewScenario(nil, DefaultConfig())

	if err := s.validateResults(t.Context(), &scenarios.Result{Details: complete()}); err != nil {
		t.Fatalf("validateResults() error = %v, want nil", err)
	}

	for _, missing := range []string{
		"approval_outcome",
		"signal_outcome",
		"approval_refusal_non_canonical_status",
		"signal_refusal_non_canonical_count",
	} {
		details := complete()
		delete(details, missing)
		if err := s.validateResults(t.Context(), &scenarios.Result{Details: details}); err == nil {
			t.Errorf("validateResults() accepted a run missing %s", missing)
		}
	}
}
