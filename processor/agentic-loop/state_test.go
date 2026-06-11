package agenticloop_test

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestLoopManager_CreateLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	tests := []struct {
		name          string
		taskID        string
		role          string
		model         string
		maxIterations int
	}{
		{
			name:          "create with default max iterations",
			taskID:        "task-001",
			role:          "general",
			model:         "qwen-32b",
			maxIterations: 0, // 0 means use default
		},
		{
			name:          "create with custom max iterations",
			taskID:        "task-002",
			role:          "architect",
			model:         "deepseek-16b",
			maxIterations: 30,
		},
		{
			name:          "create editor role",
			taskID:        "task-003",
			role:          "editor",
			model:         "qwen-32b",
			maxIterations: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var loopID string
			var err error

			if tt.maxIterations > 0 {
				loopID, err = manager.CreateLoop(tt.taskID, tt.role, tt.model, tt.maxIterations)
			} else {
				loopID, err = manager.CreateLoop(tt.taskID, tt.role, tt.model)
			}

			if err != nil {
				t.Fatalf("CreateLoop() error = %v", err)
			}

			if loopID == "" {
				t.Fatal("CreateLoop() returned empty loop ID")
			}

			// Verify loop was created
			entity, err := manager.GetLoop(loopID)
			if err != nil {
				t.Fatalf("GetLoop() error = %v", err)
			}

			if entity.ID != loopID {
				t.Errorf("Entity.ID = %s, want %s", entity.ID, loopID)
			}
			if entity.TaskID != tt.taskID {
				t.Errorf("Entity.TaskID = %s, want %s", entity.TaskID, tt.taskID)
			}
			if entity.Role != tt.role {
				t.Errorf("Entity.Role = %s, want %s", entity.Role, tt.role)
			}
			if entity.Model != tt.model {
				t.Errorf("Entity.Model = %s, want %s", entity.Model, tt.model)
			}
			if entity.State != agentic.LoopStateExploring {
				t.Errorf("Entity.State = %s, want exploring", entity.State)
			}
			if entity.Iterations != 0 {
				t.Errorf("Entity.Iterations = %d, want 0", entity.Iterations)
			}

			expectedMaxIter := tt.maxIterations
			if expectedMaxIter == 0 {
				expectedMaxIter = 20 // default
			}
			if entity.MaxIterations != expectedMaxIter {
				t.Errorf("Entity.MaxIterations = %d, want %d", entity.MaxIterations, expectedMaxIter)
			}
		})
	}
}

func TestLoopManager_GetLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	tests := []struct {
		name    string
		loopID  string
		wantErr bool
	}{
		{
			name:    "get existing loop",
			loopID:  loopID,
			wantErr: false,
		},
		{
			name:    "get non-existent loop",
			loopID:  "loop-does-not-exist",
			wantErr: true,
		},
		{
			name:    "get with empty ID",
			loopID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := manager.GetLoop(tt.loopID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetLoop() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if entity.ID != tt.loopID {
					t.Errorf("Entity.ID = %s, want %s", entity.ID, tt.loopID)
				}
			}
		})
	}
}

func TestLoopManager_UpdateLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Get the entity
	entity, err := manager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() error = %v", err)
	}

	// Modify the entity
	entity.State = agentic.LoopStatePlanning
	entity.Iterations = 5

	// Update
	err = manager.UpdateLoop(entity)
	if err != nil {
		t.Fatalf("UpdateLoop() error = %v", err)
	}

	// Verify update
	updated, err := manager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() after update error = %v", err)
	}

	if updated.State != agentic.LoopStatePlanning {
		t.Errorf("Updated.State = %s, want planning", updated.State)
	}
	if updated.Iterations != 5 {
		t.Errorf("Updated.Iterations = %d, want 5", updated.Iterations)
	}
}

func TestLoopManager_UpdateLoop_NonExistent(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	entity := agentic.NewLoopEntity("loop-does-not-exist", "task-001", "general", "qwen-32b")

	err := manager.UpdateLoop(entity)
	if err == nil {
		t.Fatal("UpdateLoop() with non-existent ID should return error")
	}

	if !containsIgnoreCase(err.Error(), "not found") && !containsIgnoreCase(err.Error(), "does not exist") {
		t.Errorf("Error should mention not found/does not exist, got: %v", err)
	}
}

func TestLoopManager_DeleteLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Verify it exists
	_, err = manager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before delete error = %v", err)
	}

	// Delete
	err = manager.DeleteLoop(loopID)
	if err != nil {
		t.Fatalf("DeleteLoop() error = %v", err)
	}

	// Verify it's gone
	_, err = manager.GetLoop(loopID)
	if err == nil {
		t.Fatal("GetLoop() after delete should return error")
	}
}

func TestLoopManager_DeleteLoop_NonExistent(_ *testing.T) {
	manager := agenticloop.NewLoopManager()

	err := manager.DeleteLoop("loop-does-not-exist")
	// Deleting non-existent loop may or may not error depending on implementation
	// Just ensure it doesn't panic
	_ = err
}

func TestLoopManager_StateTransition(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	tests := []struct {
		name      string
		fromState agentic.LoopState
		toState   agentic.LoopState
		wantErr   bool
	}{
		{
			name:      "exploring to planning",
			fromState: agentic.LoopStateExploring,
			toState:   agentic.LoopStatePlanning,
			wantErr:   false,
		},
		{
			name:      "planning to architecting",
			fromState: agentic.LoopStatePlanning,
			toState:   agentic.LoopStateArchitecting,
			wantErr:   false,
		},
		{
			name:      "architecting to executing",
			fromState: agentic.LoopStateArchitecting,
			toState:   agentic.LoopStateExecuting,
			wantErr:   false,
		},
		{
			name:      "executing to reviewing",
			fromState: agentic.LoopStateExecuting,
			toState:   agentic.LoopStateReviewing,
			wantErr:   false,
		},
		{
			name:      "reviewing to complete",
			fromState: agentic.LoopStateReviewing,
			toState:   agentic.LoopStateComplete,
			wantErr:   false,
		},
		{
			name:      "backward transition (executing to exploring)",
			fromState: agentic.LoopStateExecuting,
			toState:   agentic.LoopStateExploring,
			wantErr:   false, // Fluid transitions allow backward movement
		},
		{
			name:      "same state transition",
			fromState: agentic.LoopStatePlanning,
			toState:   agentic.LoopStatePlanning,
			wantErr:   false, // Same state is allowed (no-op)
		},
		{
			name:      "transition from terminal state (complete)",
			fromState: agentic.LoopStateComplete,
			toState:   agentic.LoopStateExecuting,
			wantErr:   true, // Cannot transition from terminal state
		},
		{
			name:      "transition from terminal state (failed)",
			fromState: agentic.LoopStateFailed,
			toState:   agentic.LoopStateExploring,
			wantErr:   true, // Cannot transition from terminal state
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get entity and set to fromState
			entity, err := manager.GetLoop(loopID)
			if err != nil {
				t.Fatalf("GetLoop() error = %v", err)
			}

			entity.State = tt.fromState
			err = manager.UpdateLoop(entity)
			if err != nil {
				t.Fatalf("UpdateLoop() to set fromState error = %v", err)
			}

			// Attempt transition
			err = manager.TransitionLoop(loopID, tt.toState)

			if (err != nil) != tt.wantErr {
				t.Errorf("TransitionLoop() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify state changed
				entity, err := manager.GetLoop(loopID)
				if err != nil {
					t.Fatalf("GetLoop() after transition error = %v", err)
				}
				if entity.State != tt.toState {
					t.Errorf("After transition, State = %s, want %s", entity.State, tt.toState)
				}
			}
		})
	}
}

func TestLoopManager_IncrementIteration(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop with max 3 iterations
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b", 3)
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Increment 3 times (should succeed)
	for i := 1; i <= 3; i++ {
		err = manager.IncrementIteration(loopID)
		if err != nil {
			t.Fatalf("IncrementIteration() #%d error = %v", i, err)
		}

		entity, err := manager.GetLoop(loopID)
		if err != nil {
			t.Fatalf("GetLoop() error = %v", err)
		}
		if entity.Iterations != i {
			t.Errorf("After increment #%d, Iterations = %d, want %d", i, entity.Iterations, i)
		}
	}

	// 4th increment should fail (max reached)
	err = manager.IncrementIteration(loopID)
	if err == nil {
		t.Fatal("IncrementIteration() beyond max should return error")
	}

	if !containsIgnoreCase(err.Error(), "max") && !containsIgnoreCase(err.Error(), "limit") {
		t.Errorf("Error should mention max/limit, got: %v", err)
	}
}

func TestLoopManager_IncrementIteration_NonExistent(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	err := manager.IncrementIteration("loop-does-not-exist")
	if err == nil {
		t.Fatal("IncrementIteration() with non-existent ID should return error")
	}
}

func TestLoopManager_PendingTools(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Initially no pending tools
	pending := manager.GetPendingTools(loopID)
	if len(pending) != 0 {
		t.Errorf("GetPendingTools() initially should be empty, got %d", len(pending))
	}

	// Should be complete with no pending tools
	if !manager.AllToolsComplete(loopID) {
		t.Error("AllToolsComplete() should be true with no pending tools")
	}

	// Add pending tools
	err = manager.AddPendingTool(loopID, "call-001")
	if err != nil {
		t.Fatalf("AddPendingTool() error = %v", err)
	}

	err = manager.AddPendingTool(loopID, "call-002")
	if err != nil {
		t.Fatalf("AddPendingTool() error = %v", err)
	}

	err = manager.AddPendingTool(loopID, "call-003")
	if err != nil {
		t.Fatalf("AddPendingTool() error = %v", err)
	}

	// Verify pending tools
	pending = manager.GetPendingTools(loopID)
	if len(pending) != 3 {
		t.Errorf("GetPendingTools() count = %d, want 3", len(pending))
	}

	// Not all complete
	if manager.AllToolsComplete(loopID) {
		t.Error("AllToolsComplete() should be false with pending tools")
	}

	// Remove one tool
	err = manager.RemovePendingTool(loopID, "call-001")
	if err != nil {
		t.Fatalf("RemovePendingTool() error = %v", err)
	}

	pending = manager.GetPendingTools(loopID)
	if len(pending) != 2 {
		t.Errorf("GetPendingTools() after remove count = %d, want 2", len(pending))
	}

	// Remove remaining tools
	err = manager.RemovePendingTool(loopID, "call-002")
	if err != nil {
		t.Fatalf("RemovePendingTool() error = %v", err)
	}

	err = manager.RemovePendingTool(loopID, "call-003")
	if err != nil {
		t.Fatalf("RemovePendingTool() error = %v", err)
	}

	pending = manager.GetPendingTools(loopID)
	if len(pending) != 0 {
		t.Errorf("GetPendingTools() after removing all should be empty, got %d", len(pending))
	}

	// All tools complete again
	if !manager.AllToolsComplete(loopID) {
		t.Error("AllToolsComplete() should be true after removing all pending tools")
	}
}

func TestLoopManager_PendingTools_DuplicateAdd(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Add same call ID twice
	err = manager.AddPendingTool(loopID, "call-001")
	if err != nil {
		t.Fatalf("First AddPendingTool() error = %v", err)
	}

	err = manager.AddPendingTool(loopID, "call-001")
	// Duplicate add may or may not error depending on implementation
	// Just ensure it doesn't panic
	_ = err

	// Should have at least one pending tool
	pending := manager.GetPendingTools(loopID)
	if len(pending) < 1 {
		t.Error("GetPendingTools() should have at least 1 pending tool")
	}
}

func TestLoopManager_PendingTools_RemoveNonExistent(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Remove non-existent call ID
	err = manager.RemovePendingTool(loopID, "call-does-not-exist")
	// Removing non-existent may or may not error
	// Just ensure it doesn't panic
	_ = err
}

func TestLoopManager_PendingTools_NonExistentLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	loopID := "loop-does-not-exist"

	// GetPendingTools on non-existent loop should return empty or error.
	// pending == nil is acceptable (indicates not found); non-nil must be empty.
	pending := manager.GetPendingTools(loopID)
	if pending != nil && len(pending) != 0 {
		t.Errorf("GetPendingTools() on non-existent loop should be empty, got %d", len(pending))
	}

	// AllToolsComplete on non-existent loop
	complete := manager.AllToolsComplete(loopID)
	// May return true (no pending) or false (loop not found)
	// Just ensure it doesn't panic
	_ = complete

	// AddPendingTool on non-existent loop should error
	err := manager.AddPendingTool(loopID, "call-001")
	if err == nil {
		t.Error("AddPendingTool() on non-existent loop should return error")
	}
}

func TestLoopManager_ConcurrentAccess(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Create a loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Concurrent updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			entity, err := manager.GetLoop(loopID)
			if err != nil {
				done <- false
				return
			}
			entity.Iterations++
			_ = manager.UpdateLoop(entity)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Race detector will catch any concurrency issues
	// Final state may vary due to concurrent updates (that's expected)
}

func TestLoopManager_GetAndClearToolResults_EvictsToolCallToLoop(t *testing.T) {
	manager := agenticloop.NewLoopManager()
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Simulate dispatch: register A and B as in-flight tool calls plus their
	// metadata (mirrors what dispatchToolCall populates).
	manager.TrackToolCall("call-A", loopID)
	manager.TrackToolName("call-A", "graph_search")
	manager.TrackToolCall("call-B", loopID)
	manager.TrackToolName("call-B", "http_request")

	// Both results land.
	if err := manager.StoreToolResult(loopID, agentic.ToolResult{CallID: "call-A", Name: "graph_search", Content: "a"}); err != nil {
		t.Fatalf("StoreToolResult(A) error = %v", err)
	}
	if err := manager.StoreToolResult(loopID, agentic.ToolResult{CallID: "call-B", Name: "http_request", Content: "b"}); err != nil {
		t.Fatalf("StoreToolResult(B) error = %v", err)
	}

	// Drain at the turn boundary.
	results := manager.GetAndClearToolResults(loopID)
	if len(results) != 2 {
		t.Fatalf("GetAndClearToolResults() returned %d results, want 2", len(results))
	}

	// Routing must be evicted so a late re-delivery has no loop to attach to —
	// findLoopIDForToolCall reads this map and a non-empty result would let
	// the duplicate flow through HandleToolResult into the next turn's drain.
	if _, ok := manager.GetLoopForToolCall("call-A"); ok {
		t.Errorf("GetLoopForToolCall(call-A) still resolves after drain — a re-delivered result would leak into the next turn")
	}
	if _, ok := manager.GetLoopForToolCall("call-B"); ok {
		t.Errorf("GetLoopForToolCall(call-B) still resolves after drain — a re-delivered result would leak into the next turn")
	}

	// Metadata maps must survive the drain — buildToolMessages's empty-name
	// fallback and the trajectory step builder both read them after drain.
	if name := manager.GetToolName("call-A"); name != "graph_search" {
		t.Errorf("GetToolName(call-A) = %q, want graph_search — eviction must not touch metadata maps", name)
	}
	if name := manager.GetToolName("call-B"); name != "http_request" {
		t.Errorf("GetToolName(call-B) = %q, want http_request — eviction must not touch metadata maps", name)
	}

	// A second drain on a now-empty PendingToolResults must still be a clean
	// no-op (idempotent on the post-bug path; relied on by any future caller
	// that drains defensively).
	if results := manager.GetAndClearToolResults(loopID); len(results) != 0 {
		t.Errorf("second GetAndClearToolResults() returned %d results, want 0", len(results))
	}

	// Re-tracking after eviction must restore the routing — this is the
	// approval-approve flow, where dispatchApprovedCall re-issues the same
	// CallID via dispatchToolCall after the human says yes. If eviction
	// blocked re-track, approval-after-drain would silently lose the result.
	manager.TrackToolCall("call-A", loopID)
	if got, ok := manager.GetLoopForToolCall("call-A"); !ok || got != loopID {
		t.Errorf("GetLoopForToolCall(call-A) after re-track = (%q, %v), want (%q, true)", got, ok, loopID)
	}
}

func TestLoopManager_ConcurrentPendingTools(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Concurrent add/remove of pending tools
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			callID := "call-" + string(rune('0'+n))
			_ = manager.AddPendingTool(loopID, callID)
			_ = manager.GetPendingTools(loopID)
			_ = manager.AllToolsComplete(loopID)
			_ = manager.RemovePendingTool(loopID, callID)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Race detector will catch any concurrency issues
}

func TestLoopManager_UpdateCompletion(t *testing.T) {
	manager := agenticloop.NewLoopManager()
	loopID, _ := manager.CreateLoop("task-001", "general", "qwen-32b", 10)

	tests := []struct {
		name      string
		loopID    string
		outcome   string
		result    string
		errMsg    string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "success completion",
			loopID:  loopID,
			outcome: agentic.OutcomeSuccess,
			result:  "Task completed successfully",
			errMsg:  "",
			wantErr: false,
		},
		{
			name:    "failed completion",
			loopID:  loopID,
			outcome: agentic.OutcomeFailed,
			result:  "",
			errMsg:  "max iterations reached",
			wantErr: false,
		},
		{
			name:    "cancelled completion",
			loopID:  loopID,
			outcome: agentic.OutcomeCancelled,
			result:  "",
			errMsg:  "cancelled by user",
			wantErr: false,
		},
		{
			name:      "invalid outcome",
			loopID:    loopID,
			outcome:   "invalid",
			result:    "",
			errMsg:    "",
			wantErr:   true,
			errSubstr: "invalid outcome",
		},
		{
			name:      "non-existent loop",
			loopID:    "non-existent",
			outcome:   agentic.OutcomeSuccess,
			result:    "test",
			errMsg:    "",
			wantErr:   true,
			errSubstr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdateCompletion(tt.loopID, tt.outcome, tt.result, tt.errMsg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UpdateCompletion() expected error, got nil")
				} else if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("UpdateCompletion() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("UpdateCompletion() unexpected error: %v", err)
				return
			}

			// Verify the entity was updated
			entity, _ := manager.GetLoop(tt.loopID)
			if entity.Outcome != tt.outcome {
				t.Errorf("Outcome = %v, want %v", entity.Outcome, tt.outcome)
			}
			if entity.Result != tt.result {
				t.Errorf("Result = %v, want %v", entity.Result, tt.result)
			}
			if entity.Error != tt.errMsg {
				t.Errorf("Error = %v, want %v", entity.Error, tt.errMsg)
			}
			if entity.CompletedAt.IsZero() {
				t.Error("CompletedAt should be set")
			}
		})
	}
}

func TestLoopManager_CancelLoop(t *testing.T) {
	tests := []struct {
		name        string
		setupState  agentic.LoopState
		cancelledBy string
		wantErr     bool
		errSubstr   string
	}{
		{
			name:        "cancel exploring loop",
			setupState:  agentic.LoopStateExploring,
			cancelledBy: "user-123",
			wantErr:     false,
		},
		{
			name:        "cancel executing loop",
			setupState:  agentic.LoopStateExecuting,
			cancelledBy: "user-456",
			wantErr:     false,
		},
		{
			name:        "cannot cancel completed loop",
			setupState:  agentic.LoopStateComplete,
			cancelledBy: "user-789",
			wantErr:     true,
			errSubstr:   "cannot cancel terminal",
		},
		{
			name:        "cannot cancel failed loop",
			setupState:  agentic.LoopStateFailed,
			cancelledBy: "user-abc",
			wantErr:     true,
			errSubstr:   "cannot cancel terminal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := agenticloop.NewLoopManager()
			loopID, _ := manager.CreateLoop("task-001", "general", "qwen-32b", 10)

			// Set up the loop state
			if tt.setupState != agentic.LoopStateExploring {
				_ = manager.TransitionLoop(loopID, tt.setupState)
			}

			entity, err := manager.CancelLoop(loopID, tt.cancelledBy)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CancelLoop() expected error, got nil")
				} else if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("CancelLoop() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("CancelLoop() unexpected error: %v", err)
				return
			}

			// Verify the entity was updated atomically
			if entity.State != agentic.LoopStateCancelled {
				t.Errorf("State = %v, want %v", entity.State, agentic.LoopStateCancelled)
			}
			if entity.CancelledBy != tt.cancelledBy {
				t.Errorf("CancelledBy = %v, want %v", entity.CancelledBy, tt.cancelledBy)
			}
			if entity.CancelledAt.IsZero() {
				t.Error("CancelledAt should be set")
			}
			if entity.Outcome != agentic.OutcomeCancelled {
				t.Errorf("Outcome = %v, want %v", entity.Outcome, agentic.OutcomeCancelled)
			}
			if entity.CompletedAt.IsZero() {
				t.Error("CompletedAt should be set")
			}
			if entity.Error != "cancelled by user" {
				t.Errorf("Error = %v, want %v", entity.Error, "cancelled by user")
			}
		})
	}
}

func TestLoopManager_CancelLoop_NotFound(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	_, err := manager.CancelLoop("non-existent", "user-123")
	if err == nil {
		t.Error("CancelLoop() expected error for non-existent loop")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("CancelLoop() error = %v, want substring 'not found'", err)
	}
}

func TestLoopManager_TrackToolName(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Track a tool name
	manager.TrackToolName("call_abc", "get_weather")

	// Retrieve it
	name := manager.GetToolName("call_abc")
	if name != "get_weather" {
		t.Errorf("GetToolName() = %q, want %q", name, "get_weather")
	}

	// Non-existent call ID returns empty string
	name = manager.GetToolName("call_unknown")
	if name != "" {
		t.Errorf("GetToolName(unknown) = %q, want empty string", name)
	}

	// Multiple tool names can be tracked
	manager.TrackToolName("call_def", "get_time")
	if got := manager.GetToolName("call_def"); got != "get_time" {
		t.Errorf("GetToolName(call_def) = %q, want %q", got, "get_time")
	}
	// Original still accessible
	if got := manager.GetToolName("call_abc"); got != "get_weather" {
		t.Errorf("GetToolName(call_abc) = %q, want %q", got, "get_weather")
	}
}

func TestLoopManager_TrackToolArguments(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Track arguments for a tool call
	args := map[string]any{"query": "SELECT *", "limit": float64(10)}
	manager.TrackToolArguments("call_abc", args)

	// Retrieve them
	got := manager.GetToolArguments("call_abc")
	if got == nil {
		t.Fatal("GetToolArguments() returned nil, expected arguments")
	}
	if got["query"] != "SELECT *" {
		t.Errorf("GetToolArguments()[query] = %v, want %q", got["query"], "SELECT *")
	}
	if got["limit"] != float64(10) {
		t.Errorf("GetToolArguments()[limit] = %v, want %v", got["limit"], float64(10))
	}

	// Nil args can be tracked without panic
	manager.TrackToolArguments("call_nil", nil)
	nilGot := manager.GetToolArguments("call_nil")
	if nilGot != nil {
		t.Errorf("GetToolArguments(nil args) = %v, want nil", nilGot)
	}

	// Non-existent call ID returns nil
	missing := manager.GetToolArguments("call_unknown")
	if missing != nil {
		t.Errorf("GetToolArguments(unknown) = %v, want nil", missing)
	}

	// Multiple arguments can be tracked independently
	args2 := map[string]any{"path": "/tmp/test"}
	manager.TrackToolArguments("call_def", args2)
	if got2 := manager.GetToolArguments("call_def"); got2["path"] != "/tmp/test" {
		t.Errorf("GetToolArguments(call_def)[path] = %v, want %q", got2["path"], "/tmp/test")
	}
	// Original still accessible
	if orig := manager.GetToolArguments("call_abc"); orig["query"] != "SELECT *" {
		t.Errorf("GetToolArguments(call_abc) overwritten unexpectedly")
	}
}

func TestLoopManager_TrackRequestStart(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Track request start
	manager.TrackRequestStart("req-001")

	// Retrieve it — should be non-zero
	start := manager.GetRequestStart("req-001")
	if start.IsZero() {
		t.Error("GetRequestStart() returned zero time, expected non-zero")
	}

	// Unknown request returns zero time
	unknown := manager.GetRequestStart("req-unknown")
	if !unknown.IsZero() {
		t.Errorf("GetRequestStart(unknown) = %v, want zero time", unknown)
	}
}

func TestLoopManager_TrackToolStart(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// Track tool start
	manager.TrackToolStart("call-001")

	// Retrieve it — should be non-zero
	start := manager.GetToolStart("call-001")
	if start.IsZero() {
		t.Error("GetToolStart() returned zero time, expected non-zero")
	}

	// Unknown call returns zero time
	unknown := manager.GetToolStart("call-unknown")
	if !unknown.IsZero() {
		t.Errorf("GetToolStart(unknown) = %v, want zero time", unknown)
	}
}

func TestLoopManager_HasActiveLoopForTask(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	// No loops — should not find any
	_, exists := manager.HasActiveLoopForTask("task-001")
	if exists {
		t.Fatal("HasActiveLoopForTask should return false when no loops exist")
	}

	// Create an active loop
	loopID, err := manager.CreateLoop("task-001", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	// Should find the active loop
	foundID, exists := manager.HasActiveLoopForTask("task-001")
	if !exists {
		t.Fatal("HasActiveLoopForTask should return true for active loop")
	}
	if foundID != loopID {
		t.Errorf("HasActiveLoopForTask returned %s, want %s", foundID, loopID)
	}

	// Different task ID should not match
	_, exists = manager.HasActiveLoopForTask("task-999")
	if exists {
		t.Fatal("HasActiveLoopForTask should return false for different task ID")
	}

	// Transition to terminal state — should no longer match
	entity, err := manager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() error = %v", err)
	}
	entity.State = agentic.LoopStateComplete
	if err := manager.UpdateLoop(entity); err != nil {
		t.Fatalf("UpdateLoop() error = %v", err)
	}

	_, exists = manager.HasActiveLoopForTask("task-001")
	if exists {
		t.Fatal("HasActiveLoopForTask should return false for terminal loop")
	}
}

// TestLoopManager_GetRunID is the read-side counterpart to SetRunID
// (ADR-053 D7, issue #250). A freshly created loop carries no run anchor;
// after SetRunID it reads back; an unknown loop reads "" rather than
// erroring so the best-effort dispatch stamp stays branchless.
func TestLoopManager_GetRunID(t *testing.T) {
	manager := agenticloop.NewLoopManager()

	loopID, err := manager.CreateLoop("task-run", "general", "qwen-32b")
	if err != nil {
		t.Fatalf("CreateLoop() error = %v", err)
	}

	if got := manager.GetRunID(loopID); got != "" {
		t.Errorf("GetRunID() before SetRunID = %q, want \"\" (loop not part of a run)", got)
	}

	const runID = "run-anchor-xyz"
	if err := manager.SetRunID(loopID, runID); err != nil {
		t.Fatalf("SetRunID() error = %v", err)
	}
	if got := manager.GetRunID(loopID); got != runID {
		t.Errorf("GetRunID() after SetRunID = %q, want %q", got, runID)
	}

	if got := manager.GetRunID("no-such-loop"); got != "" {
		t.Errorf("GetRunID(unknown) = %q, want \"\" (no error, no panic)", got)
	}
}
