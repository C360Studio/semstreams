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
			name:     "from running succeeds",
			startSt:  agentic.LoopStateRunning,
			callID:   "call-001",
			toolName: "delete_rule",
		},
		{
			name:     "retired planning fails",
			startSt:  agentic.LoopState("planning"),
			callID:   "call-002",
			toolName: "delete_rule",
			wantErr:  "invalid state",
		},
		{
			name:     "from terminal complete fails",
			startSt:  agentic.LoopStateComplete,
			callID:   "call-003",
			toolName: "delete_rule",
			wantErr:  "cannot begin awaiting approval from state",
		},
		{
			name:     "from terminal failed fails",
			startSt:  agentic.LoopStateFailed,
			callID:   "call-004",
			toolName: "delete_rule",
			wantErr:  "cannot begin awaiting approval from state",
		},
		{
			name:     "missing call_id fails",
			startSt:  agentic.LoopStateRunning,
			callID:   "",
			toolName: "delete_rule",
			wantErr:  "call_id required",
		},
		{
			name:     "missing tool_name fails",
			startSt:  agentic.LoopStateRunning,
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
			if err := entity.Validate(); err != nil {
				t.Fatalf("Begin returned invalid local state: %v", err)
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
	entity.State = agentic.LoopStateRunning

	if err := entity.BeginAwaitingApproval("call-001", "delete_rule", nil, "", 0, ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Replaying a prompt must reuse its snapshot, not begin a second gate.
	before, _ := json.Marshal(entity)
	if err := entity.BeginAwaitingApproval("call-001", "delete_rule", nil, "", 0, ""); err == nil {
		t.Fatal("same call_id must not begin a second gate")
	}
	// A different call_id while one is already pending is a logic
	// error and should fail loudly.
	err := entity.BeginAwaitingApproval("call-002", "delete_rule", nil, "", 0, "")
	if err == nil || !strings.Contains(err.Error(), "cannot begin awaiting approval") {
		t.Fatalf("want already-awaiting error, got %v", err)
	}
	after, _ := json.Marshal(entity)
	if string(after) != string(before) {
		t.Fatal("refused Begin changed the existing gate")
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
			name: "resolve returns running",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStateRunning
				_ = e.BeginAwaitingApproval("c1", "tool", nil, "", 0, "")
			},
			wantState: agentic.LoopStateRunning,
		},
		{
			name: "missing pending call refuses",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStateAwaitingApproval
				e.PendingApproval = &agentic.PendingApprovalState{ToolName: "tool"}
			},
			wantErr: "awaiting_approval requires pending call_id and tool_name",
		},
		{
			name:    "resolve when not awaiting fails",
			setup:   func(e *agentic.LoopEntity) { e.State = agentic.LoopStateRunning },
			wantErr: "loop not awaiting approval",
		},
		{
			name: "local pending gate needs no execution stamping",
			setup: func(e *agentic.LoopEntity) {
				e.State = agentic.LoopStateAwaitingApproval
				e.PendingApproval = &agentic.PendingApprovalState{CallID: "c3", ToolName: "tool"}
			},
			wantState: agentic.LoopStateRunning,
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
			if err := entity.Validate(); err != nil {
				t.Fatalf("Resolve returned invalid local state: %v", err)
			}
		})
	}
}

func TestLoopEntity_ApprovalRoundTripJSON(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-001", "task-001", "coordinator", "test-model", 20)
	entity.State = agentic.LoopStateRunning
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
