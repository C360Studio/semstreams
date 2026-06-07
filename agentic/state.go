// Package agentic provides shared types for the agentic components system.
// This includes loop state management, tool execution interfaces, and trajectory tracking.
package agentic

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic/identity"
)

// LoopState represents the current state of an agentic loop
type LoopState string

// Loop states for the agentic state machine.
// The state machine supports fluid transitions (can move backward) except from terminal states.
const (
	// Standard workflow states
	LoopStateExploring    LoopState = "exploring"
	LoopStatePlanning     LoopState = "planning"
	LoopStateArchitecting LoopState = "architecting"
	LoopStateExecuting    LoopState = "executing"
	LoopStateReviewing    LoopState = "reviewing"

	// Terminal states
	LoopStateComplete  LoopState = "complete"
	LoopStateFailed    LoopState = "failed"
	LoopStateCancelled LoopState = "cancelled" // Cancelled by user signal

	// Signal-related states
	LoopStatePaused           LoopState = "paused"            // Paused by user signal
	LoopStateAwaitingApproval LoopState = "awaiting_approval" // Waiting for user approval
)

// String returns the string representation of the state
func (s LoopState) String() string {
	return string(s)
}

// IsTerminal returns true if the state is a terminal state
func (s LoopState) IsTerminal() bool {
	return s == LoopStateComplete || s == LoopStateFailed || s == LoopStateCancelled
}

// LoopEntity represents an agentic loop instance
type LoopEntity struct {
	ID                 string                `json:"id"`
	TaskID             string                `json:"task_id"`
	State              LoopState             `json:"state"`
	Role               string                `json:"role"`
	Model              string                `json:"model"`
	Iterations         int                   `json:"iterations"`
	MaxIterations      int                   `json:"max_iterations"`
	PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // Accumulated tool results by call ID
	StartedAt          time.Time             `json:"started_at,omitempty"`           // When the loop was created
	TimeoutAt          time.Time             `json:"timeout_at,omitempty"`           // When the loop should timeout
	ParentLoopID       string                `json:"parent_loop_id,omitempty"`       // Parent loop ID for architect->editor relationship
	// RunID is the 6-part-derived run anchor; the run loop-id this loop belongs to.
	// Empty for loops not in a run. Inherited at spawn (ADR-053 D7).
	RunID string `json:"run_id,omitempty"`

	// Multi-agent depth tracking
	Depth    int `json:"depth,omitempty"`     // Current depth in agent tree (0 = root)
	MaxDepth int `json:"max_depth,omitempty"` // Maximum allowed depth for spawned agents

	// Signal support fields
	PauseRequested   bool      `json:"pause_requested,omitempty"`    // Pause requested, will pause at next checkpoint
	PauseRequestedBy string    `json:"pause_requested_by,omitempty"` // User who requested pause
	StateBeforePause LoopState `json:"state_before_pause,omitempty"` // State before pause (for resume)
	CancelledBy      string    `json:"cancelled_by,omitempty"`       // User who cancelled the loop
	CancelledAt      time.Time `json:"cancelled_at,omitempty"`       // When the loop was cancelled

	// Approval-gating fields (set when a tool call is rejected by the
	// agentic-tools approval filter). The loop transitions to
	// LoopStateAwaitingApproval and persists the pending call here so
	// it can be re-dispatched on approval. StateBeforeApproval lets us
	// restore the prior workflow state once the approval response
	// arrives.
	PendingApproval     *PendingApprovalState `json:"pending_approval,omitempty"`
	StateBeforeApproval LoopState             `json:"state_before_approval,omitempty"`

	// User context (for routing responses)
	UserID      string `json:"user_id,omitempty"`      // User who initiated the loop
	ChannelType string `json:"channel_type,omitempty"` // cli, slack, discord, web
	ChannelID   string `json:"channel_id,omitempty"`   // Channel/session ID for routing responses

	// Workflow context (for loops created by workflow commands)
	WorkflowSlug string `json:"workflow_slug,omitempty"` // e.g., "add-user-auth"
	WorkflowStep string `json:"workflow_step,omitempty"` // e.g., "design"

	// Completion data (populated when loop completes)
	// These fields enable SSE delivery of results via KV watch
	Outcome     string    `json:"outcome,omitempty"`      // success, failed, cancelled
	Result      string    `json:"result,omitempty"`       // LLM response content
	Error       string    `json:"error,omitempty"`        // Error message on failure
	CompletedAt time.Time `json:"completed_at,omitempty"` // When the loop completed

	// Domain context propagated from TaskMessage through lifecycle events
	Metadata map[string]any `json:"metadata,omitempty"`

	// AGNTCY identity (Phase 2 AGNTCY integration)
	// When set, provides DID-based cryptographic identity for this agent loop.
	Identity *identity.AgentIdentity `json:"identity,omitempty"`
}

// Validate checks if the LoopEntity is valid
func (e *LoopEntity) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("id required")
	}
	if !isValidLoopState(e.State) {
		return fmt.Errorf("invalid state: %s", e.State)
	}
	if e.MaxIterations <= 0 {
		return fmt.Errorf("max_iterations must be greater than 0")
	}
	return nil
}

// isValidLoopState checks if the state is a valid LoopState
func isValidLoopState(s LoopState) bool {
	switch s {
	case LoopStateExploring, LoopStatePlanning, LoopStateArchitecting,
		LoopStateExecuting, LoopStateReviewing, LoopStateComplete,
		LoopStateFailed, LoopStateCancelled, LoopStatePaused,
		LoopStateAwaitingApproval:
		return true
	default:
		return false
	}
}

// TransitionTo transitions the entity to a new state
func (e *LoopEntity) TransitionTo(newState LoopState) error {
	// Allow same-state transitions (no-op)
	if e.State == newState {
		return nil
	}
	// Prevent transitions from terminal states
	if e.State.IsTerminal() {
		return fmt.Errorf("cannot transition from terminal state %s", e.State)
	}
	e.State = newState
	return nil
}

// PendingApprovalState captures the gated tool call so the loop can
// re-dispatch (or reject) it once a human approval response arrives.
// Persisted on LoopEntity so a process restart mid-approval still
// remembers what the human is reviewing.
type PendingApprovalState struct {
	CallID      string         `json:"call_id"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Reason      string         `json:"reason,omitempty"`   // Original "approval_required: ..." rejection reason
	RequestedAt time.Time      `json:"requested_at"`       // When the rejection arrived and the loop paused
	Timeout     time.Duration  `json:"timeout,omitempty"`  // Auto-reject deadline; zero means wait indefinitely
	TraceID     string         `json:"trace_id,omitempty"` // Propagated for audit correlation
}

// BeginAwaitingApproval transitions the loop into
// LoopStateAwaitingApproval and stores the pending call. Returns an
// error if the loop is already terminal or already awaiting approval
// for a different call (which would indicate a logic bug — two
// rejections for the same loop shouldn't be possible while the first
// is still pending).
func (e *LoopEntity) BeginAwaitingApproval(callID, toolName string, arguments map[string]any, reason string, timeout time.Duration, traceID string) error {
	if e.State.IsTerminal() {
		return fmt.Errorf("cannot begin awaiting approval from terminal state %s", e.State)
	}
	if e.PendingApproval != nil && e.PendingApproval.CallID != callID {
		return fmt.Errorf("loop already awaiting approval for call %s", e.PendingApproval.CallID)
	}
	if callID == "" {
		return fmt.Errorf("call_id required")
	}
	if toolName == "" {
		return fmt.Errorf("tool_name required")
	}
	e.StateBeforeApproval = e.State
	e.State = LoopStateAwaitingApproval
	e.PendingApproval = &PendingApprovalState{
		CallID:      callID,
		ToolName:    toolName,
		Arguments:   arguments,
		Reason:      reason,
		RequestedAt: time.Now().UTC(),
		Timeout:     timeout,
		TraceID:     traceID,
	}
	return nil
}

// ResolveApproval clears the pending approval and restores the prior
// state so the loop can resume normal iteration. Caller is
// responsible for re-dispatching the tool (approve/modify) or
// synthesizing a rejection (reject) before invoking this.
func (e *LoopEntity) ResolveApproval() error {
	if e.State != LoopStateAwaitingApproval {
		return fmt.Errorf("loop not awaiting approval (state=%s)", e.State)
	}
	if e.PendingApproval == nil {
		return fmt.Errorf("loop awaiting approval but PendingApproval is nil")
	}
	restore := e.StateBeforeApproval
	if restore == "" || restore == LoopStateAwaitingApproval {
		// Defensive: if we somehow lost the prior state, fall back to
		// executing so the loop can advance. Should not happen because
		// BeginAwaitingApproval always captures it.
		restore = LoopStateExecuting
	}
	e.State = restore
	e.StateBeforeApproval = ""
	e.PendingApproval = nil
	return nil
}

// IncrementIteration increments the iteration counter
func (e *LoopEntity) IncrementIteration() error {
	if e.Iterations >= e.MaxIterations {
		return fmt.Errorf("max iterations reached")
	}
	e.Iterations++
	return nil
}

// NewLoopEntity creates a new LoopEntity with default values
func NewLoopEntity(id, taskID, role, model string, maxIterations ...int) LoopEntity {
	maxIter := 20
	if len(maxIterations) > 0 && maxIterations[0] > 0 {
		maxIter = maxIterations[0]
	}
	return LoopEntity{
		ID:            id,
		TaskID:        taskID,
		State:         LoopStateExploring,
		Role:          role,
		Model:         model,
		Iterations:    0,
		MaxIterations: maxIter,
	}
}
