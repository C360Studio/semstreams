// Package agentic provides shared types for the agentic components system.
// This includes loop state management, tool execution interfaces, and trajectory tracking.
package agentic

import (
	"errors"
	"fmt"
	"time"
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

	// Approval states
	//
	// There is no paused state. SemStreams supports cancellation, durable
	// human approval, safe retry/restart and operational quiescing; it does
	// not support arbitrary execution pause/resume, so no value in this
	// vocabulary may advertise one (owner ruling, #1239, 2026-09-03).
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
	PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID
	// PublishedRequestID names the AgentRequest outstanding for this loop: the
	// RequestID whose PubAck preceded the KV update that wrote this record.
	//
	// It is the loop's durable settlement fact (invariant I1, #1330): while
	// this record exists, an AgentRequest{RequestID: PublishedRequestID,
	// LoopID: ID} is durably retained on agent.request.<loop id>. That is what
	// lets a process with no memory of the loop classify a redelivered model
	// response or tool result by identity — older, current or newer — instead
	// of comparing retained conversation content.
	//
	// Set at birth and by every request-minting transition; never cleared.
	// It is a settlement fact, not an in-flight answer: a record naming a
	// request says nothing about whether any process is still working on it.
	PublishedRequestID string    `json:"published_request_id,omitempty"`
	StartedAt          time.Time `json:"started_at,omitempty"`     // When the loop was created
	TimeoutAt          time.Time `json:"timeout_at,omitempty"`     // When the loop should timeout
	ParentLoopID       string    `json:"parent_loop_id,omitempty"` // Parent loop ID for architect->editor relationship
	// RunID is the 6-part-derived run anchor; the run loop-id this loop belongs to.
	// Empty for loops not in a run. Inherited at spawn (ADR-053 D7).
	RunID string `json:"run_id,omitempty"`

	// Multi-agent depth tracking
	Depth    int `json:"depth,omitempty"`     // Current depth in agent tree (0 = root)
	MaxDepth int `json:"max_depth,omitempty"` // Maximum allowed depth for spawned agents

	// Signal support fields
	CancelledBy string    `json:"cancelled_by,omitempty"` // User who cancelled the loop
	CancelledAt time.Time `json:"cancelled_at,omitempty"` // When the loop was cancelled

	// Approval-gating fields (set when a tool call is rejected by the
	// agentic-tools approval filter). The loop transitions to
	// LoopStateAwaitingApproval and persists the pending call here so
	// it can be re-dispatched on approval. StateBeforeApproval lets us
	// restore the prior workflow state once the approval response
	// arrives.
	PendingApproval     *PendingApprovalState `json:"pending_approval,omitempty"`
	StateBeforeApproval LoopState             `json:"state_before_approval,omitempty"`

	// PendingContinuation is set when a continuation task was admitted to this
	// loop while its model request was still outstanding. The continuation's
	// turn is already in the loop's context; what this marker carries is that
	// the turn must not be lost, so the outstanding response must not complete
	// the loop — it advances to the next iteration and publishes a request that
	// includes it.
	//
	// One outstanding model request per loop is what makes the request name
	// (`<loopID>:req:<iteration>:<retry>`) unique: two requests minted at the
	// same iteration carry the same name and the same Nats-Msg-Id, and the
	// duplicate window drops the second. This marker is how the loop keeps that
	// invariant without a third identity segment (owner ruling Q4).
	//
	// Residual, declared: restoring this across a process replacement is L4's
	// (#1330). In-process it is authoritative; after a replacement the whole
	// loop needs recovery, not just this bit.
	PendingContinuation bool `json:"pending_continuation,omitempty"`

	// PendingContinuationRequestID names the request that carries the deferred
	// turn, empty while no request does. It exists because the marker is
	// persisted BEFORE the publish it describes: persistHandlerResult stamps
	// the entity and only then emits the request, so a marker cleared when the
	// request was BUILT would be durably clear while the publish that justified
	// the clear had unknown durability — the delivery quarantines and the only
	// state that could re-carry the turn is already gone.
	//
	// Recording the carrier instead keeps both obligations: the turn is not
	// carried twice (a request already names it), and a quarantined publish
	// leaves "pending, carried by <requestID>" durable for recovery to act on.
	// It clears when that request's response settles, which is the first moment
	// the send is known to have happened.
	PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"`

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
		LoopStateFailed, LoopStateCancelled,
		LoopStateAwaitingApproval:
		return true
	default:
		return false
	}
}

// TransitionTo transitions the entity to a new state.
//
// The target is validated against the state vocabulary. Before this the method
// took any string, which is how "paused" stayed reachable through an exported
// API after the pause semantics were deleted: removing the constant alone
// leaves LoopState("paused") settable by any caller. Validating the argument
// refuses that and every other value the vocabulary does not define, rather
// than special-casing one string — a reserved-enum shim in reverse is still a
// compatibility shim (owner ruling, #1239, 2026-09-03).
func (e *LoopEntity) TransitionTo(newState LoopState) error {
	// The vocabulary check comes first, ahead of the same-state no-op. An
	// entity decoded from a durable record can already be holding a value the
	// vocabulary does not define, and answering nil to "move it to paused"
	// because it is already paused is the acceptance this ruling removes: the
	// caller cannot tell that answer apart from a state that was allowed.
	if !isValidLoopState(newState) {
		return fmt.Errorf("invalid state: %s", newState)
	}
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
	RequestID   string         `json:"request_id,omitempty"`
	ExecutionID string         `json:"execution_id,omitempty"`
	CallID      string         `json:"call_id"`
	CallOrdinal uint32         `json:"call_ordinal,omitempty"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Reason      string         `json:"reason,omitempty"`   // Original "approval_required: ..." rejection reason
	RequestedAt time.Time      `json:"requested_at"`       // When the rejection arrived and the loop gated
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

// ErrMaxIterationsReached is the typed sentinel LoopEntity.IncrementIteration
// returns when the loop's iteration counter has already met or exceeded its
// configured budget (gh#529). Callers MUST branch on errors.Is against this
// sentinel rather than treating any non-nil IncrementIteration error as
// budget exhaustion — processor/agentic-loop.LoopManager.IncrementIteration
// can also fail with an unrelated "loop not found" error, which is a
// distinct operational failure and must not be misreported as
// max_iterations. processor/agentic-loop.ErrMaxIterationsReached aliases
// this value (the import direction only works agentic → agenticloop) so
// both packages compare against the exact same sentinel.
var ErrMaxIterationsReached = errors.New("max iterations reached")

// IncrementIteration increments the iteration counter
func (e *LoopEntity) IncrementIteration() error {
	if e.Iterations >= e.MaxIterations {
		return ErrMaxIterationsReached
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
