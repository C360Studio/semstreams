package agentic

import (
	"net/http"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestAgenticAssertingStagesMatchesStageList(t *testing.T) {
	stages := NewScenario(nil, DefaultConfig()).stages()

	counted := 0
	for _, stage := range stages {
		if stage.asserts {
			counted++
		}
	}
	if counted != agenticAssertingStages {
		t.Fatalf("stages() has %d asserting stages, agenticAssertingStages = %d", counted, agenticAssertingStages)
	}
}

func TestStagesWalkApprovalAndSignalLanes(t *testing.T) {
	want := map[string]bool{
		"walk-approval-path":            false,
		"refuse-non-canonical-approval": false,
		"walk-signal-path":              false,
		"refuse-non-canonical-signal":   false,
	}
	for _, stage := range NewScenario(nil, DefaultConfig()).stages() {
		if _, ok := want[stage.name]; ok {
			want[stage.name] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("stages() does not run %s", name)
		}
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
			"signal_refusal_non_canonical_reason":   "form_malformed",
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
		"signal_refusal_non_canonical_reason",
	} {
		details := complete()
		delete(details, missing)
		if err := s.validateResults(t.Context(), &scenarios.Result{Details: details}); err == nil {
			t.Errorf("validateResults() accepted a run missing %s", missing)
		}
	}
}
