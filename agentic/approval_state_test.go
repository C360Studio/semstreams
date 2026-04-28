package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

func TestLoopEntity_BeginAwaitingApproval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		startSt  agentic.LoopState
		callID   string
		toolName string
		wantErr  string
	}{
		{
			name:     "from executing succeeds",
			startSt:  agentic.LoopStateExecuting,
			callID:   "call-001",
			toolName: "delete_rule",
		},
		{
			name:     "from planning succeeds",
			startSt:  agentic.LoopStatePlanning,
			callID:   "call-002",
			toolName: "delete_rule",
		},
		{
			name:     "from terminal complete fails",
			startSt:  agentic.LoopStateComplete,
			callID:   "call-003",
			toolName: "delete_rule",
			wantErr:  "cannot begin awaiting approval from terminal state",
		},
		{
			name:     "from terminal failed fails",
			startSt:  agentic.LoopStateFailed,
			callID:   "call-004",
			toolName: "delete_rule",
			wantErr:  "cannot begin awaiting approval from terminal state",
		},
		{
			name:     "missing call_id fails",
			startSt:  agentic.LoopStateExecuting,
			callID:   "",
			toolName: "delete_rule",
			wantErr:  "call_id required",
		},
		{
			name:     "missing tool_name fails",
			startSt:  agentic.LoopStateExecuting,
			callID:   "call-005",
			toolName: "",
			wantErr:  "tool_name required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
			entity.State = tt.startSt
			err := entity.BeginAwaitingApproval(tt.callID, tt.toolName, map[string]any{"k": "v"}, "approval_required: deny", 0, "trace-1")

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if entity.State != agentic.LoopStateAwaitingApproval {
				t.Errorf("state = %s, want %s", entity.State, agentic.LoopStateAwaitingApproval)
			}
			if entity.StateBeforeApproval != tt.startSt {
				t.Errorf("StateBeforeApproval = %s, want %s", entity.StateBeforeApproval, tt.startSt)
			}
			if entity.PendingApproval == nil {
				t.Fatalf("PendingApproval is nil")
			}
			if entity.PendingApproval.CallID != tt.callID {
				t.Errorf("CallID = %s, want %s", entity.PendingApproval.CallID, tt.callID)
			}
			if entity.PendingApproval.ToolName != tt.toolName {
				t.Errorf("ToolName = %s, want %s", entity.PendingApproval.ToolName, tt.toolName)
			}
			if entity.PendingApproval.RequestedAt.IsZero() {
				t.Errorf("RequestedAt not set")
			}
		})
	}
}

func TestLoopEntity_BeginAwaitingApproval_DuplicateCall(t *testing.T) {
	t.Parallel()
	entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
	entity.State = agentic.LoopStateExecuting

	if err := entity.BeginAwaitingApproval("call-001", "delete_rule", nil, "", 0, ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Re-invoking with the same call_id is idempotent (refreshes the
	// timestamp, doesn't error).
	if err := entity.BeginAwaitingApproval("call-001", "delete_rule", nil, "", 0, ""); err != nil {
		t.Fatalf("same call_id should be idempotent: %v", err)
	}
	// A different call_id while one is already pending is a logic
	// error and should fail loudly.
	err := entity.BeginAwaitingApproval("call-002", "delete_rule", nil, "", 0, "")
	if err == nil || !strings.Contains(err.Error(), "already awaiting approval") {
		t.Fatalf("want already-awaiting error, got %v", err)
	}
}

func TestLoopEntity_ResolveApproval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(*agentic.LoopEntity)
		wantState agentic.LoopState
		wantErr   string
	}{
		{
			name: "resume from executing",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStateExecuting
				_ = e.BeginAwaitingApproval("c1", "tool", nil, "", 0, "")
			},
			wantState: agentic.LoopStateExecuting,
		},
		{
			name: "resume from planning",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStatePlanning
				_ = e.BeginAwaitingApproval("c2", "tool", nil, "", 0, "")
			},
			wantState: agentic.LoopStatePlanning,
		},
		{
			name:    "resolve when not awaiting fails",
			setup:   func(e *agentic.LoopEntity) { e.State = agentic.LoopStateExecuting },
			wantErr: "loop not awaiting approval",
		},
		{
			name: "missing prior state falls back to executing",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStateAwaitingApproval
				e.PendingApproval = &agentic.PendingApprovalState{CallID: "c3", ToolName: "tool"}
				e.StateBeforeApproval = ""
			},
			wantState: agentic.LoopStateExecuting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
			tt.setup(&entity)
			err := entity.ResolveApproval()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if entity.State != tt.wantState {
				t.Errorf("state = %s, want %s", entity.State, tt.wantState)
			}
			if entity.PendingApproval != nil {
				t.Errorf("PendingApproval not cleared: %+v", entity.PendingApproval)
			}
			if entity.StateBeforeApproval != "" {
				t.Errorf("StateBeforeApproval not cleared: %s", entity.StateBeforeApproval)
			}
		})
	}
}

func TestLoopEntity_ApprovalRoundTripJSON(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
	entity.State = agentic.LoopStateExecuting
	if err := entity.BeginAwaitingApproval("call-001", "delete_rule",
		map[string]any{"rule_id": "rule-42"},
		"approval_required: Tool 'delete_rule' requires human approval",
		30*time.Second, "trace-xyz"); err != nil {
		t.Fatalf("BeginAwaitingApproval: %v", err)
	}

	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got agentic.LoopEntity
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.State != agentic.LoopStateAwaitingApproval {
		t.Errorf("state lost in round-trip: %s", got.State)
	}
	if got.PendingApproval == nil {
		t.Fatalf("PendingApproval lost in round-trip")
	}
	if got.PendingApproval.CallID != "call-001" || got.PendingApproval.ToolName != "delete_rule" ||
		got.PendingApproval.Timeout != 30*time.Second || got.PendingApproval.TraceID != "trace-xyz" {
		t.Errorf("pending approval mismatch: %+v", got.PendingApproval)
	}
	if v, ok := got.PendingApproval.Arguments["rule_id"].(string); !ok || v != "rule-42" {
		t.Errorf("Arguments[rule_id] = %v", got.PendingApproval.Arguments["rule_id"])
	}
}

func TestLoopEntity_ApprovalDoesNotInterfereWithPause(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
	entity.State = agentic.LoopStateExecuting
	entity.PauseRequested = true
	entity.StateBeforePause = agentic.LoopStateExecuting

	if err := entity.BeginAwaitingApproval("call-001", "delete_rule", nil, "", 0, ""); err != nil {
		t.Fatalf("BeginAwaitingApproval: %v", err)
	}

	// Pause fields must remain untouched — they're orthogonal.
	if !entity.PauseRequested {
		t.Errorf("PauseRequested cleared by approval flow")
	}
	if entity.StateBeforePause != agentic.LoopStateExecuting {
		t.Errorf("StateBeforePause overwritten: %s", entity.StateBeforePause)
	}
	if entity.StateBeforeApproval != agentic.LoopStateExecuting {
		t.Errorf("StateBeforeApproval = %s, want executing", entity.StateBeforeApproval)
	}
}
