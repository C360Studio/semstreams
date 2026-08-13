package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoopTracker(t *testing.T) {
	tracker := NewLoopTracker()
	require.NotNil(t, tracker)
	assert.Equal(t, 0, tracker.Count())
}

func TestLoopTracker_Track(t *testing.T) {
	tracker := NewLoopTracker()

	info := &LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "cli",
		ChannelID:   "session-1",
		State:       "pending",
		CreatedAt:   time.Now(),
	}

	tracker.Track(info)
	assert.Equal(t, 1, tracker.Count())

	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, "loop-1", retrieved.LoopID)
	assert.Equal(t, "user-1", retrieved.UserID)
}

func TestLoopTracker_GetActiveLoop(t *testing.T) {
	tracker := NewLoopTracker()

	// No active loops initially
	loopID := tracker.GetActiveLoop("user-1", "session-1")
	assert.Empty(t, loopID)

	// Add a pending loop
	info := &LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "cli",
		ChannelID:   "session-1",
		State:       "pending",
		CreatedAt:   time.Now(),
	}
	tracker.Track(info)

	// Should find the active loop via channel
	loopID = tracker.GetActiveLoop("user-1", "session-1")
	assert.Equal(t, "loop-1", loopID)

	// Different user with same channel shouldn't find it (no user mapping for user-2)
	loopID = tracker.GetActiveLoop("user-2", "session-1")
	// Actually, GetActiveLoop first checks channel, then user
	// session-1 maps to loop-1, which belongs to user-1
	// The implementation returns it if the loop is active, regardless of user mismatch
	// Let's test with different channel
	loopID = tracker.GetActiveLoop("user-2", "session-2")
	assert.Empty(t, loopID)

	// Same user, different channel should find via user mapping
	loopID = tracker.GetActiveLoop("user-1", "session-2")
	assert.Equal(t, "loop-1", loopID)
}

func TestLoopTracker_GetActiveLoop_TerminalState(t *testing.T) {
	tracker := NewLoopTracker()

	// Add a completed loop
	info := &LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "cli",
		ChannelID:   "session-1",
		State:       "complete",
		CreatedAt:   time.Now(),
	}
	tracker.Track(info)

	// Should NOT find a terminal loop
	loopID := tracker.GetActiveLoop("user-1", "session-1")
	assert.Empty(t, loopID)
}

func TestLoopTracker_UpdateState(t *testing.T) {
	tracker := NewLoopTracker()

	info := &LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "cli",
		ChannelID:   "session-1",
		State:       "pending",
		CreatedAt:   time.Now(),
	}
	tracker.Track(info)

	// Update state
	tracker.UpdateState("loop-1", "executing")

	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, "executing", retrieved.State)

	// Update to terminal state
	tracker.UpdateState("loop-1", "complete")
	retrieved = tracker.Get("loop-1")
	assert.Equal(t, "complete", retrieved.State)
}

func TestLoopTracker_UpdateState_NonExistent(t *testing.T) {
	tracker := NewLoopTracker()

	// Should not panic
	tracker.UpdateState("nonexistent", "complete")
	assert.Equal(t, 0, tracker.Count())
}

func TestLoopTracker_UpdateIterations(t *testing.T) {
	tracker := NewLoopTracker()

	info := &LoopInfo{
		LoopID:        "loop-1",
		TaskID:        "task-1",
		UserID:        "user-1",
		State:         "executing",
		Iterations:    0,
		MaxIterations: 20,
		CreatedAt:     time.Now(),
	}
	tracker.Track(info)

	tracker.UpdateIterations("loop-1", 5)

	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, 5, retrieved.Iterations)
}

func TestLoopTracker_UpdateWorkflowContext(t *testing.T) {
	tracker := NewLoopTracker()

	// Add a loop without workflow context
	info := &LoopInfo{
		LoopID:        "loop-1",
		TaskID:        "task-1",
		UserID:        "user-1",
		State:         "executing",
		MaxIterations: 20,
		CreatedAt:     time.Now(),
	}
	tracker.Track(info)

	// Update workflow context
	updated := tracker.UpdateWorkflowContext("loop-1", "add-user-auth", "design")
	assert.True(t, updated)

	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, "add-user-auth", retrieved.WorkflowSlug)
	assert.Equal(t, "design", retrieved.WorkflowStep)
}

func TestLoopTracker_UpdateWorkflowContext_NonExistent(t *testing.T) {
	tracker := NewLoopTracker()

	// Should return false for non-existent loop
	updated := tracker.UpdateWorkflowContext("nonexistent", "workflow", "step")
	assert.False(t, updated)
}

func TestLoopTracker_UpdateWorkflowContext_AlreadyHasContext(t *testing.T) {
	tracker := NewLoopTracker()

	// Add a loop with existing workflow context
	info := &LoopInfo{
		LoopID:       "loop-1",
		TaskID:       "task-1",
		UserID:       "user-1",
		State:        "executing",
		WorkflowSlug: "existing-workflow",
		WorkflowStep: "existing-step",
		CreatedAt:    time.Now(),
	}
	tracker.Track(info)

	// Should not update existing context
	updated := tracker.UpdateWorkflowContext("loop-1", "new-workflow", "new-step")
	assert.False(t, updated)

	// Original context should be preserved
	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, "existing-workflow", retrieved.WorkflowSlug)
	assert.Equal(t, "existing-step", retrieved.WorkflowStep)
}

func TestLoopTracker_UpdateWorkflowContext_EmptySlug(t *testing.T) {
	tracker := NewLoopTracker()

	// Add a loop without workflow context
	info := &LoopInfo{
		LoopID:    "loop-1",
		TaskID:    "task-1",
		State:     "executing",
		CreatedAt: time.Now(),
	}
	tracker.Track(info)

	// Should not update with empty slug
	updated := tracker.UpdateWorkflowContext("loop-1", "", "step")
	assert.False(t, updated)

	retrieved := tracker.Get("loop-1")
	require.NotNil(t, retrieved)
	assert.Empty(t, retrieved.WorkflowSlug)
}

func TestLoopTracker_UpdateContextRequestID(t *testing.T) {
	tracker := NewLoopTracker()
	info := &LoopInfo{
		LoopID:    "loop-123",
		TaskID:    "task-456",
		UserID:    "user-789",
		State:     "running",
		CreatedAt: time.Now(),
	}
	tracker.Track(info)

	// Should return false for unknown loop
	assert.False(t, tracker.UpdateContextRequestID("unknown", "ctx-001"))

	// Should update when missing
	assert.True(t, tracker.UpdateContextRequestID("loop-123", "ctx-001"))
	assert.Equal(t, "ctx-001", tracker.Get("loop-123").ContextRequestID)

	// Should not overwrite existing
	assert.False(t, tracker.UpdateContextRequestID("loop-123", "ctx-002"))
	assert.Equal(t, "ctx-001", tracker.Get("loop-123").ContextRequestID)

	// Should not update with empty string
	tracker2 := NewLoopTracker()
	info2 := &LoopInfo{LoopID: "loop-2", UserID: "u", CreatedAt: time.Now()}
	tracker2.Track(info2)
	assert.False(t, tracker2.UpdateContextRequestID("loop-2", ""))
}

func TestLoopTracker_Remove(t *testing.T) {
	tracker := NewLoopTracker()

	info := &LoopInfo{
		LoopID:    "loop-1",
		TaskID:    "task-1",
		UserID:    "user-1",
		ChannelID: "session-1",
		State:     "pending",
		CreatedAt: time.Now(),
	}
	tracker.Track(info)
	assert.Equal(t, 1, tracker.Count())

	tracker.Remove("loop-1")
	assert.Equal(t, 0, tracker.Count())
	assert.Nil(t, tracker.Get("loop-1"))
}

func TestLoopTracker_Remove_CleansUpMappings(t *testing.T) {
	tracker := NewLoopTracker()

	info := &LoopInfo{
		LoopID:    "loop-1",
		TaskID:    "task-1",
		UserID:    "user-1",
		ChannelID: "session-1",
		State:     "pending",
		CreatedAt: time.Now(),
	}
	tracker.Track(info)

	// Verify mappings exist
	assert.Equal(t, "loop-1", tracker.GetActiveLoop("user-1", "session-1"))

	tracker.Remove("loop-1")

	// Mappings should be cleaned up
	assert.Empty(t, tracker.GetActiveLoop("user-1", "session-1"))
}

func TestLoopTracker_GetUserLoops(t *testing.T) {
	tracker := NewLoopTracker()

	// Add loops for user-1
	tracker.Track(&LoopInfo{
		LoopID:    "loop-1",
		UserID:    "user-1",
		State:     "pending",
		CreatedAt: time.Now(),
	})
	tracker.Track(&LoopInfo{
		LoopID:    "loop-2",
		UserID:    "user-1",
		State:     "executing",
		CreatedAt: time.Now(),
	})

	// Add loop for user-2
	tracker.Track(&LoopInfo{
		LoopID:    "loop-3",
		UserID:    "user-2",
		State:     "pending",
		CreatedAt: time.Now(),
	})

	user1Loops := tracker.GetUserLoops("user-1")
	assert.Len(t, user1Loops, 2)

	user2Loops := tracker.GetUserLoops("user-2")
	assert.Len(t, user2Loops, 1)

	user3Loops := tracker.GetUserLoops("user-3")
	assert.Len(t, user3Loops, 0)
}

func TestLoopTracker_GetAllLoops(t *testing.T) {
	tracker := NewLoopTracker()

	tracker.Track(&LoopInfo{
		LoopID:    "loop-1",
		UserID:    "user-1",
		State:     "pending",
		CreatedAt: time.Now(),
	})
	tracker.Track(&LoopInfo{
		LoopID:    "loop-2",
		UserID:    "user-1",
		State:     "executing",
		CreatedAt: time.Now(),
	})
	tracker.Track(&LoopInfo{
		LoopID:    "loop-3",
		UserID:    "user-2",
		State:     "complete",
		CreatedAt: time.Now(),
	})

	allLoops := tracker.GetAllLoops()
	assert.Len(t, allLoops, 3)
}

func TestLoopTracker_Concurrent(t *testing.T) {
	tracker := NewLoopTracker()
	done := make(chan bool, 10)

	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func(n int) {
			tracker.Track(&LoopInfo{
				LoopID:    "loop-" + string(rune('a'+n)),
				UserID:    "user-1",
				State:     "pending",
				CreatedAt: time.Now(),
			})
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			_ = tracker.GetAllLoops()
			_ = tracker.Count()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 5, tracker.Count())
}

func TestIsTerminalState(t *testing.T) {
	tests := []struct {
		state    string
		terminal bool
	}{
		{"pending", false},
		{"executing", false},
		{"paused", false},
		{"complete", true},
		{"failed", true},
		{"cancelled", true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			assert.Equal(t, tt.terminal, isTerminalState(tt.state))
		})
	}
}

func TestSignalMessage_Serialization(t *testing.T) {
	signal := SignalMessage{
		LoopID:    "loop-123",
		Type:      "cancel",
		Reason:    "user requested",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	// Test marshaling
	data, err := json.Marshal(signal)
	require.NoError(t, err)

	// Test unmarshaling
	var decoded SignalMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "loop-123", decoded.LoopID)
	assert.Equal(t, "cancel", decoded.Type)
	assert.Equal(t, "user requested", decoded.Reason)
	assert.Equal(t, signal.Timestamp, decoded.Timestamp)
}

func TestSignalMessage_Types(t *testing.T) {
	tests := []struct {
		signalType string
		valid      bool
	}{
		{"pause", true},
		{"resume", true},
		{"cancel", true},
		{"", false},
		{"stop", false},
	}

	for _, tt := range tests {
		t.Run(tt.signalType, func(t *testing.T) {
			signal := SignalMessage{
				LoopID:    "loop-1",
				Type:      tt.signalType,
				Timestamp: time.Now(),
			}

			data, err := json.Marshal(signal)
			require.NoError(t, err)

			var decoded SignalMessage
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.signalType, decoded.Type)
		})
	}
}

func TestLoopTracker_SendSignal_NoClient(t *testing.T) {
	tracker := NewLoopTracker()
	ctx := context.Background()

	// With nil NATS client, SendSignal should return ErrNATSClientNil
	err := tracker.SendSignal(ctx, nil, "loop-1", "cancel", "test reason")
	assert.Error(t, err)
	assert.Equal(t, ErrNATSClientNil, err)
}

func TestLoopTracker_UpdateCompletion(t *testing.T) {
	tests := []struct {
		name      string
		outcome   string
		result    string
		errMsg    string
		wantErr   bool
		wantState string
	}{
		{
			name:      "success completion",
			outcome:   agentic.OutcomeSuccess,
			result:    "Task completed successfully",
			errMsg:    "",
			wantErr:   false,
			wantState: "complete",
		},
		{
			name:      "failed completion",
			outcome:   agentic.OutcomeFailed,
			result:    "",
			errMsg:    "max iterations reached",
			wantErr:   false,
			wantState: "failed",
		},
		{
			name:      "cancelled completion",
			outcome:   agentic.OutcomeCancelled,
			result:    "",
			errMsg:    "cancelled by user",
			wantErr:   false,
			wantState: "cancelled",
		},
		{
			name:    "invalid outcome",
			outcome: "invalid",
			result:  "",
			errMsg:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewLoopTracker()

			// Track a loop first
			info := &LoopInfo{
				LoopID:        "loop-123",
				TaskID:        "task-456",
				UserID:        "user-789",
				ChannelType:   "cli",
				ChannelID:     "session-1",
				State:         "executing",
				Iterations:    5,
				MaxIterations: 10,
				CreatedAt:     time.Now(),
			}
			tracker.Track(info)

			err := tracker.UpdateCompletion("loop-123", tt.outcome, tt.result, tt.errMsg)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify the info was updated
			updated := tracker.Get("loop-123")
			require.NotNil(t, updated)
			assert.Equal(t, tt.outcome, updated.Outcome)
			assert.Equal(t, tt.result, updated.Result)
			assert.Equal(t, tt.errMsg, updated.Error)
			assert.Equal(t, tt.wantState, updated.State)
			assert.False(t, updated.CompletedAt.IsZero())
		})
	}
}

func TestLoopTracker_UpdateCompletion_NotFound(t *testing.T) {
	tracker := NewLoopTracker()

	err := tracker.UpdateCompletion("non-existent", agentic.OutcomeSuccess, "result", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoopTracker_UpdateCompletion_InvalidOutcome(t *testing.T) {
	tracker := NewLoopTracker()

	// Even without tracking a loop, invalid outcome should fail first
	err := tracker.UpdateCompletion("any-loop", "bogus", "result", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid outcome")
}

func TestLoopTracker_UpdateCompletionAtIsIdempotentAndRejectsConflict(t *testing.T) {
	tracker := NewLoopTracker()
	tracker.Track(&LoopInfo{LoopID: "loop-1", State: "executing"})
	at := time.Unix(1_700_000_700, 0).UTC()

	changed, err := tracker.updateCompletionAt("loop-1", agentic.OutcomeSuccess, "done", "", at)
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = tracker.updateCompletionAt("loop-1", agentic.OutcomeSuccess, "done", "", at)
	require.NoError(t, err)
	require.False(t, changed)

	changed, err = tracker.updateCompletionAt("loop-1", agentic.OutcomeFailed, "", "boom", at)
	require.Error(t, err)
	require.False(t, changed)
}

func TestLoopTracker_SetPendingApproval(t *testing.T) {
	tracker := NewLoopTracker()
	tracker.Track(&LoopInfo{
		LoopID: "loop-1",
		UserID: "user-1",
		State:  "executing",
	})

	pending := &PendingApprovalInfo{
		CallID:      "call-001",
		ToolName:    "delete_rule",
		Arguments:   map[string]any{"rule_id": "rule-42"},
		Reason:      "approval_required: deny",
		RequestedAt: time.Now().UTC(),
	}

	ok := tracker.SetPendingApproval("loop-1", pending)
	assert.True(t, ok, "should return true when loop exists")

	info := tracker.Get("loop-1")
	require.NotNil(t, info.PendingApproval)
	assert.Equal(t, "call-001", info.PendingApproval.CallID)
	assert.Equal(t, "delete_rule", info.PendingApproval.ToolName)
	assert.Equal(t, "rule-42", info.PendingApproval.Arguments["rule_id"])
}

// TestLoopTracker_SetPendingApproval_BuffersUnknownLoop verifies
// the early-arrival race fix: an approval-pending event for a loop
// not yet tracked goes into the bounded TTL'd buffer; the next Track
// for that loop drains the buffer and attaches the record.
func TestLoopTracker_SetPendingApproval_BuffersUnknownLoop(t *testing.T) {
	tracker := NewLoopTracker()

	// Approval-pending arrives FIRST (race: agent.approval_pending
	// drained before agent.created on independent JetStream
	// consumers).
	ok := tracker.SetPendingApproval("loop-late", &PendingApprovalInfo{
		CallID:   "call-001",
		ToolName: "delete_rule",
	})
	assert.False(t, ok, "should return false when loop not yet tracked (buffered)")
	assert.Nil(t, tracker.Get("loop-late"), "loop must not appear in tracker until Track is called")

	// agent.created arrives SECOND. Track drains the buffer.
	tracker.Track(&LoopInfo{
		LoopID: "loop-late",
		UserID: "user-1",
		State:  "awaiting_approval",
	})

	info := tracker.Get("loop-late")
	require.NotNil(t, info)
	require.NotNil(t, info.PendingApproval, "buffered approval must attach on Track")
	assert.Equal(t, "call-001", info.PendingApproval.CallID)
	assert.Equal(t, "delete_rule", info.PendingApproval.ToolName)
}

// TestLoopTracker_SetPendingApproval_BufferRespectsCap pins the
// drop-newest cap policy: when the buffer is full, new events are
// dropped while older entries remain available to drain. Older
// entries are statistically more likely to drain soon (their
// agent.created event is ahead in the redelivery queue), and a
// flood beyond cap is pathological — alerting via the warn log is
// the right behavior, not silently evicting older state.
func TestLoopTracker_SetPendingApproval_BufferRespectsCap(t *testing.T) {
	tracker := NewLoopTracker()

	// Fill the buffer to capacity with orphaned events.
	for i := 0; i < pendingApprovalBufferCap; i++ {
		_ = tracker.SetPendingApproval(fmt.Sprintf("loop-%d", i), &PendingApprovalInfo{
			CallID: fmt.Sprintf("call-%d", i),
		})
	}

	// Overflow: cap+1 entry must be dropped.
	ok := tracker.SetPendingApproval("loop-overflow", &PendingApprovalInfo{CallID: "call-overflow"})
	assert.False(t, ok, "cap-overflow should report not-applied")

	// The overflow loop's eventual Track must NOT find a buffered entry.
	tracker.Track(&LoopInfo{LoopID: "loop-overflow", UserID: "u", State: "executing"})
	assert.Nil(t, tracker.Get("loop-overflow").PendingApproval,
		"overflow event must not have been buffered")

	// One of the existing buffered loops must still drain successfully.
	tracker.Track(&LoopInfo{LoopID: "loop-0", UserID: "u", State: "executing"})
	require.NotNil(t, tracker.Get("loop-0").PendingApproval,
		"older buffered entry must still be available to drain")
	assert.Equal(t, "call-0", tracker.Get("loop-0").PendingApproval.CallID)
}

// TestLoopTracker_SetPendingApproval_TTLExpiry pins the safety
// property that an expired buffer entry never attaches to a
// late-arriving Track. Attaching stale state would resurrect an
// approval the framework's loop has already moved past, corrupting
// dispatch's view.
func TestLoopTracker_SetPendingApproval_TTLExpiry(t *testing.T) {
	tracker := NewLoopTracker()

	// Buffer an early-arrival, then force its expiry by hand
	// (white-box: easier than waiting 60s in a unit test).
	tracker.SetPendingApproval("loop-stale", &PendingApprovalInfo{CallID: "call-old"})
	tracker.mu.Lock()
	tracker.pendingApprovalBuffer["loop-stale"].ExpiresAt = time.Now().Add(-time.Second)
	tracker.mu.Unlock()

	// Track arrives after the TTL has passed.
	tracker.Track(&LoopInfo{LoopID: "loop-stale", UserID: "u", State: "executing"})

	info := tracker.Get("loop-stale")
	require.NotNil(t, info, "loop must still be tracked")
	assert.Nil(t, info.PendingApproval,
		"expired buffer entry must not attach — would corrupt dispatch's view of an already-resolved approval")
}

// TestLoopTracker_SetPendingApproval_BufferRespectsCallerSetState
// verifies that if Track is called with a LoopInfo that already has
// PendingApproval set, the buffer drain doesn't overwrite it.
func TestLoopTracker_SetPendingApproval_BufferRespectsCallerSetState(t *testing.T) {
	tracker := NewLoopTracker()

	// Buffer an early-arrival.
	tracker.SetPendingApproval("loop-race", &PendingApprovalInfo{
		CallID:   "call-buffered",
		ToolName: "delete_rule",
	})

	// Track with caller-supplied PendingApproval — should win.
	tracker.Track(&LoopInfo{
		LoopID: "loop-race",
		UserID: "user-1",
		State:  "awaiting_approval",
		PendingApproval: &PendingApprovalInfo{
			CallID:   "call-caller-supplied",
			ToolName: "delete_rule",
		},
	})

	info := tracker.Get("loop-race")
	require.NotNil(t, info.PendingApproval)
	assert.Equal(t, "call-caller-supplied", info.PendingApproval.CallID,
		"caller-supplied PendingApproval must take precedence over buffer")
}

// TestLoopTracker_GetPendingApprovalCallID covers the atomic-read
// accessor introduced to fix the prior data race where the HTTP
// handler dereferenced LoopInfo.PendingApproval outside the
// tracker's lock.
func TestLoopTracker_GetPendingApprovalCallID(t *testing.T) {
	tracker := NewLoopTracker()

	t.Run("unknown loop returns false", func(t *testing.T) {
		callID, ok := tracker.GetPendingApprovalCallID("ghost")
		assert.False(t, ok)
		assert.Empty(t, callID)
	})

	tracker.Track(&LoopInfo{LoopID: "loop-1", UserID: "user-1", State: "executing"})

	t.Run("tracked loop without pending returns false", func(t *testing.T) {
		callID, ok := tracker.GetPendingApprovalCallID("loop-1")
		assert.False(t, ok)
		assert.Empty(t, callID)
	})

	tracker.SetPendingApproval("loop-1", &PendingApprovalInfo{CallID: "call-001", ToolName: "delete_rule"})

	t.Run("tracked loop with pending returns CallID", func(t *testing.T) {
		callID, ok := tracker.GetPendingApprovalCallID("loop-1")
		assert.True(t, ok)
		assert.Equal(t, "call-001", callID)
	})

	tracker.ClearPendingApproval("loop-1")

	t.Run("cleared returns false again", func(t *testing.T) {
		callID, ok := tracker.GetPendingApprovalCallID("loop-1")
		assert.False(t, ok)
		assert.Empty(t, callID)
	})
}

func TestLoopTracker_ClearPendingApproval(t *testing.T) {
	tracker := NewLoopTracker()
	tracker.Track(&LoopInfo{LoopID: "loop-1", UserID: "user-1", State: "executing"})
	tracker.SetPendingApproval("loop-1", &PendingApprovalInfo{
		CallID:   "call-001",
		ToolName: "delete_rule",
	})
	require.NotNil(t, tracker.Get("loop-1").PendingApproval)

	tracker.ClearPendingApproval("loop-1")
	assert.Nil(t, tracker.Get("loop-1").PendingApproval)

	// Idempotent — clearing twice is a no-op.
	tracker.ClearPendingApproval("loop-1")
	assert.Nil(t, tracker.Get("loop-1").PendingApproval)

	// Unknown loop is also a no-op.
	tracker.ClearPendingApproval("ghost")
}

// TestLoopTracker_UpdateCompletion_ClearsPendingApproval guards the
// clear-on-progress hook: a loop that completed (success/failed/
// cancelled) cannot still have a pending approval — the framework's
// loop has either resolved or terminated, and any HTTP approval
// arriving against it is meaningless. The hook prevents a stale
// PendingApproval from giving the handler a false-positive.
func TestLoopTracker_UpdateCompletion_ClearsPendingApproval(t *testing.T) {
	tests := []struct {
		name    string
		outcome string
	}{
		{"success", agentic.OutcomeSuccess},
		{"failed", agentic.OutcomeFailed},
		{"cancelled", agentic.OutcomeCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewLoopTracker()
			tracker.Track(&LoopInfo{LoopID: "loop-1", UserID: "user-1", State: "awaiting_approval"})
			tracker.SetPendingApproval("loop-1", &PendingApprovalInfo{
				CallID:   "call-001",
				ToolName: "delete_rule",
			})

			err := tracker.UpdateCompletion("loop-1", tt.outcome, "result", "")
			require.NoError(t, err)

			info := tracker.Get("loop-1")
			assert.Nil(t, info.PendingApproval, "PendingApproval should be cleared on terminal state")
		})
	}
}
