package agentic

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// spec: agentic-loop / LoopEntity has one operational state contract
func TestOperationalTransitionTableStructure(t *testing.T) {
	if err := loopTransitions.Validate(); err != nil {
		t.Fatal(err)
	}
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestOperationalStateContract(t *testing.T) {
	states := []LoopState{"running", "awaiting_approval", "complete", "failed", "cancelled"}
	for _, from := range states {
		for _, to := range states {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				entity := LoopEntity{State: from, Outcome: "truncated", Result: "keep result", Error: "keep error",
					CompletedAt: time.Unix(11, 0), CancelledAt: time.Unix(12, 0), CancelledBy: "keep user",
					PendingToolResults: map[string]ToolResult{"keep": {CallID: "keep"}}}
				if from == "awaiting_approval" {
					entity.PendingApproval = &PendingApprovalState{CallID: "call", ToolName: "tool"}
				}
				before := entity
				beforeJSON, _ := json.Marshal(entity)
				allowed := from == to || (from == "running" && (to == "complete" || to == "failed" || to == "cancelled")) ||
					(from == "awaiting_approval" && (to == "running" || to == "failed" || to == "cancelled"))
				err := entity.TransitionTo(to)
				if (err == nil) != allowed {
					t.Fatalf("TransitionTo() = %v, allowed %t", err, allowed)
				}
				if !allowed || from == to {
					afterJSON, _ := json.Marshal(entity)
					if string(afterJSON) != string(beforeJSON) {
						t.Fatal("refusal/no-op changed receiver")
					}
					return
				}
				before.State, before.PendingApproval = to, nil
				if !reflect.DeepEqual(entity, before) {
					t.Fatalf("transition changed more than state and gate: %+v", entity)
				}
			})
		}
	}
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestOperationalStateMembershipAndCoherence(t *testing.T) {
	states := []LoopState{"running", "awaiting_approval", "complete", "failed", "cancelled",
		"", "unknown", "exploring", "planning", "architecting", "executing", "reviewing", "paused"}
	for index, state := range states {
		t.Run(string(state), func(t *testing.T) {
			entity := LoopEntity{ID: "loop", State: state, MaxIterations: 1}
			if state == "awaiting_approval" {
				entity.PendingApproval = &PendingApprovalState{CallID: "call", ToolName: "tool"}
			}
			if err := entity.Validate(); (err == nil) != (index < 5) {
				t.Errorf("Validate() = %v", err)
			}
			if got := state.IsTerminal(); got != (index >= 2 && index < 5) {
				t.Errorf("IsTerminal() = %t", got)
			}
			if index >= 5 {
				before, _ := json.Marshal(entity)
				for _, target := range []LoopState{state, "running"} {
					if err := entity.TransitionTo(target); err == nil {
						t.Errorf("unknown/retired source accepted target %q", target)
					}
				}
				after, _ := json.Marshal(entity)
				if string(after) != string(before) {
					t.Error("invalid source mutated")
				}
				entity.State = "running"
				if err := entity.TransitionTo(state); err == nil {
					t.Error("unknown/retired target accepted")
				}
			}
		})
	}
	for _, state := range states[:5] {
		t.Run("contradictory/"+string(state), func(t *testing.T) {
			entity := LoopEntity{ID: "loop", State: state, MaxIterations: 1}
			if state != "awaiting_approval" {
				entity.PendingApproval = &PendingApprovalState{CallID: "call", ToolName: "tool"}
			}
			before, _ := json.Marshal(entity)
			if err := entity.Validate(); err == nil {
				t.Error("contradictory state accepted")
			}
			if err := entity.TransitionTo(state); err == nil {
				t.Error("contradictory no-op accepted")
			}
			if err := entity.BeginAwaitingApproval("call", "tool", nil, "", 0, ""); err == nil {
				t.Error("contradictory Begin accepted")
			}
			if err := entity.ResolveApproval(); err == nil {
				t.Error("contradictory Resolve accepted")
			}
			after, _ := json.Marshal(entity)
			if string(after) != string(before) {
				t.Error("refused mutation changed receiver")
			}
		})
	}
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestOperationalApprovalPublicMethods(t *testing.T) {
	entity := NewLoopEntity("loop", "task", "general", "model")
	if entity.State != "running" {
		t.Errorf("birth state = %q", entity.State)
	}
	entity.State = "running"
	if err := entity.BeginAwaitingApproval("call", "tool", map[string]any{"key": "value"}, "review", time.Minute, "trace"); err != nil {
		t.Fatal(err)
	}
	if err := entity.Validate(); err != nil {
		t.Fatalf("Begin requires hidden stamping: %v", err)
	}
	before, _ := json.Marshal(entity)
	if err := entity.BeginAwaitingApproval("call", "tool", nil, "new reason", 0, ""); err == nil {
		t.Error("second Begin accepted")
	}
	after, _ := json.Marshal(entity)
	if string(after) != string(before) {
		t.Error("second Begin mutated gate")
	}
	var wire map[string]any
	if err := json.Unmarshal(before, &wire); err != nil {
		t.Fatal(err)
	}
	if _, present := wire["state_before_approval"]; present {
		t.Error("retired field emitted")
	}
	if err := entity.ResolveApproval(); err != nil {
		t.Fatal(err)
	}
	if entity.State != "running" || entity.PendingApproval != nil {
		t.Errorf("Resolve = %+v", entity)
	}
	if err := entity.Validate(); err != nil {
		t.Fatal(err)
	}
	local := LoopEntity{State: "running"}
	if err := local.BeginAwaitingApproval("call", "tool", nil, "", 0, ""); err != nil {
		t.Fatal("local Begin acquired ID/budget prerequisite", err)
	}
	if err := local.ResolveApproval(); err != nil {
		t.Fatal("local Resolve acquired ID/budget prerequisite", err)
	}
}
