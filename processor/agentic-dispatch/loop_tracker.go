package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// LoopInfo contains information about an active loop
type LoopInfo struct {
	LoopID string `json:"loop_id"`
	TaskID string `json:"task_id"`
	// Role is the agent role assigned to the loop (e.g., "coordinator",
	// "ops", "research"). Propagated from TaskMessage.Role at creation so
	// operators, UIs, and test harnesses inspecting /loops can filter by
	// role without needing a graph query back to the loop entity.
	Role          string    `json:"role,omitempty"`
	UserID        string    `json:"user_id"`
	ChannelType   string    `json:"channel_type"`
	ChannelID     string    `json:"channel_id"`
	State         string    `json:"state"`
	Iterations    int       `json:"iterations"`
	MaxIterations int       `json:"max_iterations"`
	CreatedAt     time.Time `json:"created_at"`

	// Workflow context (for loops created by workflow commands)
	WorkflowSlug string `json:"workflow_slug,omitempty"` // e.g., "add-user-auth"
	WorkflowStep string `json:"workflow_step,omitempty"` // e.g., "design"

	// Context assembly reference (links to assembled context)
	ContextRequestID string `json:"context_request_id,omitempty"`

	// Domain context propagated from TaskMessage
	Metadata map[string]any `json:"metadata,omitempty"`

	// Completion data (populated when loop completes)
	Outcome     string    `json:"outcome,omitempty"`      // success, failed, cancelled
	Result      string    `json:"result,omitempty"`       // LLM response content
	Error       string    `json:"error,omitempty"`        // Error message on failure
	CompletedAt time.Time `json:"completed_at,omitempty"` // When the loop completed

	// PendingApproval is populated from agent.approval_pending events
	// when the loop transitions to LoopStateAwaitingApproval. The
	// approval HTTP handler reads this to recover the CallID it needs
	// to publish on agent.approval_response. Cleared on terminal-state
	// transitions and on a successful approval response.
	PendingApproval *PendingApprovalInfo `json:"pending_approval,omitempty"`
}

// PendingApprovalInfo mirrors the relevant fields of
// agentic.PendingApprovalState that the dispatch HTTP handler needs
// to surface to a caller and to reconstruct the ApprovalResponse
// envelope. Stored on LoopInfo, not duplicated in a separate map, so
// /loops/{id} GET responses can include it for free. TraceID is
// preserved so a "why is this loop stuck on approval?" investigation
// is greppable across loop, dispatch, and any product UI without a
// graph query. Timeout is intentionally NOT mirrored — only the
// framework's loop enforces it, and exposing it here would invite
// dispatch consumers to second-guess that authority.
type PendingApprovalInfo struct {
	CallID      string         `json:"call_id"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	RequestedAt time.Time      `json:"requested_at"`
	TraceID     string         `json:"trace_id,omitempty"`
}

// pendingApprovalBufferCap caps the early-arrival buffer so a flood
// of approval-pending events for never-tracked loops can't grow the
// tracker unboundedly. Sized for "the same loop's create event is
// almost always already on its way" — exceeding this cap means
// something has gone wrong upstream.
const pendingApprovalBufferCap = 256

// pendingApprovalBufferTTL bounds how long an early-arrival entry
// can sit unmatched before being evicted. The race window between
// agent.created and agent.approval_pending is typically <1s in
// healthy deployments; 60s gives ample slack for slow consumers
// without hoarding orphaned state.
const pendingApprovalBufferTTL = 60 * time.Second

// bufferedPendingApproval holds an approval-pending record that
// arrived before its loop was tracked. Track drains the buffer when
// the matching loop appears.
type bufferedPendingApproval struct {
	Info      *PendingApprovalInfo
	ExpiresAt time.Time
}

// LoopTracker tracks active loops per user and channel
type LoopTracker struct {
	mu           sync.RWMutex
	userLoops    map[string]string    // user_id -> most recent loop_id
	channelLoops map[string]string    // channel_id -> most recent loop_id
	loops        map[string]*LoopInfo // loop_id -> LoopInfo
	// pendingApprovalBuffer absorbs the agent.approval_pending vs
	// agent.created subscription race: when an approval-pending event
	// arrives before its loop has been tracked, we stash the record
	// here keyed by loop_id. Track drains the buffer on insert so the
	// approval lands on the right LoopInfo as soon as the create event
	// catches up. Capped by pendingApprovalBufferCap and entries TTL'd
	// at pendingApprovalBufferTTL so a flood of orphan events can't
	// grow the tracker unboundedly.
	pendingApprovalBuffer map[string]*bufferedPendingApproval
	logger                *slog.Logger
}

// NewLoopTracker creates a new LoopTracker
func NewLoopTracker() *LoopTracker {
	return &LoopTracker{
		userLoops:             make(map[string]string),
		channelLoops:          make(map[string]string),
		loops:                 make(map[string]*LoopInfo),
		pendingApprovalBuffer: make(map[string]*bufferedPendingApproval),
	}
}

// NewLoopTrackerWithLogger creates a new LoopTracker with logging.
func NewLoopTrackerWithLogger(logger *slog.Logger) *LoopTracker {
	return &LoopTracker{
		userLoops:             make(map[string]string),
		channelLoops:          make(map[string]string),
		loops:                 make(map[string]*LoopInfo),
		pendingApprovalBuffer: make(map[string]*bufferedPendingApproval),
		logger:                logger,
	}
}

// SetLogger sets the logger for the LoopTracker.
func (t *LoopTracker) SetLogger(logger *slog.Logger) {
	t.logger = logger
}

// Track adds or updates a loop in the tracker. If an
// approval-pending event arrived before this loop was tracked (the
// race window between agent.created and agent.approval_pending
// subscriptions), the buffered record is attached here so the
// HTTP approval handler can resolve CallID without losing state.
func (t *LoopTracker) Track(info *LoopInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, existed := t.loops[info.LoopID]
	t.loops[info.LoopID] = info
	t.userLoops[info.UserID] = info.LoopID
	if info.ChannelID != "" {
		t.channelLoops[info.ChannelID] = info.LoopID
	}

	// Drain any buffered approval-pending record that arrived before
	// this loop was tracked. Caller-supplied PendingApproval (rare)
	// takes precedence over the buffer.
	if buffered, ok := t.pendingApprovalBuffer[info.LoopID]; ok {
		delete(t.pendingApprovalBuffer, info.LoopID)
		if info.PendingApproval == nil && time.Now().Before(buffered.ExpiresAt) {
			info.PendingApproval = buffered.Info
			if t.logger != nil {
				t.logger.Debug("attached buffered pending-approval to newly-tracked loop",
					slog.String("loop_id", info.LoopID),
					slog.String("call_id", buffered.Info.CallID))
			}
		}
	}

	if t.logger != nil {
		t.logger.Debug("loop tracked",
			slog.String("loop_id", info.LoopID),
			slog.String("task_id", info.TaskID),
			slog.String("role", info.Role),
			slog.String("user_id", info.UserID),
			slog.String("channel_id", info.ChannelID),
			slog.String("state", info.State),
			slog.Bool("new", !existed),
			slog.Int("total_loops", len(t.loops)))
	}
}

// Get retrieves loop info by ID
func (t *LoopTracker) Get(loopID string) *LoopInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.loops[loopID]
}

// GetActiveLoop returns the most recent active loop for a user/channel
func (t *LoopTracker) GetActiveLoop(userID, channelID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Prefer channel-specific loop
	if channelID != "" {
		if loopID, ok := t.channelLoops[channelID]; ok {
			if info := t.loops[loopID]; info != nil && !isTerminalState(info.State) {
				return loopID
			}
		}
	}

	// Fall back to user's most recent loop
	if loopID, ok := t.userLoops[userID]; ok {
		if info := t.loops[loopID]; info != nil && !isTerminalState(info.State) {
			return loopID
		}
	}

	return ""
}

// UpdateState updates the state of a loop
func (t *LoopTracker) UpdateState(loopID, state string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if info, ok := t.loops[loopID]; ok {
		oldState := info.State
		info.State = state

		if t.logger != nil {
			t.logger.Debug("loop state changed",
				slog.String("loop_id", loopID),
				slog.String("user_id", info.UserID),
				slog.String("old_state", oldState),
				slog.String("new_state", state),
				slog.Bool("terminal", isTerminalState(state)))
		}
	} else if t.logger != nil {
		t.logger.Warn("attempted to update state for unknown loop",
			slog.String("loop_id", loopID),
			slog.String("state", state))
	}
}

// UpdateIterations updates the iteration count of a loop
func (t *LoopTracker) UpdateIterations(loopID string, iterations int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if info, ok := t.loops[loopID]; ok {
		info.Iterations = iterations

		if t.logger != nil {
			t.logger.Debug("loop iterations updated",
				slog.String("loop_id", loopID),
				slog.Int("iterations", iterations),
				slog.Int("max_iterations", info.MaxIterations))
		}
	}
}

// UpdateCompletion updates a loop with completion data (outcome, result, error).
// This is called when a loop finishes to populate fields for SSE delivery.
// It also updates the State field to match the terminal state implied by
// the outcome AND clears any stale PendingApproval — once a loop has
// completed, an approval request against it is meaningless.
func (t *LoopTracker) UpdateCompletion(loopID, outcome, result, errMsg string) error {
	if !isValidOutcome(outcome) {
		return errs.WrapInvalid(fmt.Errorf("invalid outcome: %s", outcome), "LoopTracker", "UpdateCompletion", "validate outcome")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.loops[loopID]
	if !ok {
		return errs.WrapInvalid(fmt.Errorf("loop %s not found", loopID), "LoopTracker", "UpdateCompletion", "find loop")
	}

	info.Outcome = outcome
	info.Result = result
	info.Error = errMsg
	info.CompletedAt = time.Now()
	info.State = outcomeToState(outcome)
	info.PendingApproval = nil

	if t.logger != nil {
		t.logger.Debug("loop completion updated",
			slog.String("loop_id", loopID),
			slog.String("outcome", outcome),
			slog.String("state", info.State),
			slog.Int("result_len", len(result)),
			slog.Bool("has_error", errMsg != ""))
	}

	return nil
}

// SetPendingApproval records the gated tool-call info for a loop
// awaiting human approval. Called by the agent.approval_pending
// subscription handler. When the loop isn't tracked yet (the race
// where approval-pending arrives before agent.created), the record
// is stashed in a TTL'd, capped buffer that Track drains on insert.
// Returns true when the record landed on a tracked loop, false when
// it was buffered or dropped — the distinction matters mostly for
// logging/metrics; either way the framework's own state is the
// source of truth.
func (t *LoopTracker) SetPendingApproval(loopID string, pending *PendingApprovalInfo) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if info, ok := t.loops[loopID]; ok {
		info.PendingApproval = pending
		if t.logger != nil {
			t.logger.Debug("loop pending-approval recorded",
				slog.String("loop_id", loopID),
				slog.String("call_id", pending.CallID),
				slog.String("tool_name", pending.ToolName))
		}
		return true
	}

	// Loop isn't tracked yet. Buffer the record so Track can drain
	// it when the matching agent.created arrives. Lazy GC of expired
	// entries on each call keeps the buffer bounded without a
	// background sweeper.
	t.gcPendingApprovalBufferLocked()
	if len(t.pendingApprovalBuffer) >= pendingApprovalBufferCap {
		if t.logger != nil {
			t.logger.Warn("approval-pending buffer full, dropping event",
				slog.String("loop_id", loopID),
				slog.String("call_id", pending.CallID),
				slog.Int("buffer_size", len(t.pendingApprovalBuffer)))
		}
		return false
	}
	t.pendingApprovalBuffer[loopID] = &bufferedPendingApproval{
		Info:      pending,
		ExpiresAt: time.Now().Add(pendingApprovalBufferTTL),
	}
	if t.logger != nil {
		t.logger.Debug("approval-pending buffered for unknown loop",
			slog.String("loop_id", loopID),
			slog.String("call_id", pending.CallID),
			slog.Int("buffer_size", len(t.pendingApprovalBuffer)))
	}
	return false
}

// gcPendingApprovalBufferLocked evicts expired entries from the
// approval-pending buffer. Caller must hold t.mu in write mode.
func (t *LoopTracker) gcPendingApprovalBufferLocked() {
	now := time.Now()
	for loopID, entry := range t.pendingApprovalBuffer {
		if !now.Before(entry.ExpiresAt) {
			delete(t.pendingApprovalBuffer, loopID)
		}
	}
}

// GetPendingApprovalCallID atomically snapshots the CallID of the
// loop's currently-pinned PendingApproval. Returns ("", false) when
// the loop is unknown or has no pending approval. Returning a value-
// type instead of a *PendingApprovalInfo pointer prevents callers
// from racing against concurrent SetPendingApproval / Clear /
// UpdateCompletion mutations — the previous Get→deref pattern read
// mutable fields outside the tracker's RLock and is unsound under
// concurrent HTTP requests.
func (t *LoopTracker) GetPendingApprovalCallID(loopID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	info, ok := t.loops[loopID]
	if !ok || info.PendingApproval == nil {
		return "", false
	}
	return info.PendingApproval.CallID, true
}

// ClearPendingApproval drops the pending-approval record for a loop.
// Called when the loop transitions out of awaiting_approval (the
// approval was resolved, the loop was cancelled, etc.). Idempotent —
// no-op when no pending state exists.
func (t *LoopTracker) ClearPendingApproval(loopID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.loops[loopID]
	if !ok || info.PendingApproval == nil {
		return
	}
	cleared := info.PendingApproval.CallID
	info.PendingApproval = nil

	if t.logger != nil {
		t.logger.Debug("loop pending-approval cleared",
			slog.String("loop_id", loopID),
			slog.String("call_id", cleared))
	}
}

// isValidOutcome checks if the outcome is one of the valid constants.
func isValidOutcome(outcome string) bool {
	switch outcome {
	case agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeCancelled:
		return true
	default:
		return false
	}
}

// outcomeToState maps an outcome to its corresponding terminal state.
func outcomeToState(outcome string) string {
	switch outcome {
	case agentic.OutcomeSuccess:
		return "complete"
	case agentic.OutcomeFailed:
		return "failed"
	case agentic.OutcomeCancelled:
		return "cancelled"
	default:
		return "failed"
	}
}

// UpdateWorkflowContext atomically updates the workflow context for a loop.
// Returns true if the update was applied (loop exists and had no workflow context).
func (t *LoopTracker) UpdateWorkflowContext(loopID, workflowSlug, workflowStep string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.loops[loopID]
	if !ok {
		return false
	}

	// Only update if workflow context is missing
	if info.WorkflowSlug == "" && workflowSlug != "" {
		info.WorkflowSlug = workflowSlug
		info.WorkflowStep = workflowStep

		if t.logger != nil {
			t.logger.Debug("loop workflow context updated",
				slog.String("loop_id", loopID),
				slog.String("workflow_slug", workflowSlug),
				slog.String("workflow_step", workflowStep))
		}
		return true
	}
	return false
}

// UpdateContextRequestID atomically updates the context request ID for a loop.
// Returns true if the update was applied (loop exists and had no context request ID).
func (t *LoopTracker) UpdateContextRequestID(loopID, contextRequestID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.loops[loopID]
	if !ok {
		return false
	}

	// Only update if context request ID is missing
	if info.ContextRequestID == "" && contextRequestID != "" {
		info.ContextRequestID = contextRequestID

		if t.logger != nil {
			t.logger.Debug("loop context request ID updated",
				slog.String("loop_id", loopID),
				slog.String("context_request_id", contextRequestID))
		}
		return true
	}
	return false
}

// Remove removes a loop from the tracker
func (t *LoopTracker) Remove(loopID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.loops[loopID]
	if !ok {
		if t.logger != nil {
			t.logger.Debug("attempted to remove unknown loop",
				slog.String("loop_id", loopID))
		}
		return
	}

	// Clean up user mapping if this was their most recent loop
	if t.userLoops[info.UserID] == loopID {
		delete(t.userLoops, info.UserID)
	}

	// Clean up channel mapping if this was the channel's most recent loop
	if info.ChannelID != "" && t.channelLoops[info.ChannelID] == loopID {
		delete(t.channelLoops, info.ChannelID)
	}

	delete(t.loops, loopID)

	if t.logger != nil {
		t.logger.Debug("loop removed",
			slog.String("loop_id", loopID),
			slog.String("user_id", info.UserID),
			slog.String("final_state", info.State),
			slog.Int("iterations", info.Iterations),
			slog.Int("remaining_loops", len(t.loops)))
	}
}

// GetUserLoops returns all loops for a specific user
func (t *LoopTracker) GetUserLoops(userID string) []*LoopInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*LoopInfo
	for _, info := range t.loops {
		if info.UserID == userID {
			result = append(result, info)
		}
	}
	return result
}

// GetAllLoops returns all tracked loops
func (t *LoopTracker) GetAllLoops() []*LoopInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*LoopInfo, 0, len(t.loops))
	for _, info := range t.loops {
		result = append(result, info)
	}
	return result
}

// Count returns the number of tracked loops
func (t *LoopTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.loops)
}

// isTerminalState checks if a state is terminal
func isTerminalState(state string) bool {
	switch state {
	case "complete", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// SignalMessage represents a control signal sent to a loop.
type SignalMessage struct {
	LoopID    string    `json:"loop_id"`
	Type      string    `json:"type"`   // pause, resume, cancel
	Reason    string    `json:"reason"` // optional reason
	Timestamp time.Time `json:"timestamp"`
}

// Validate implements message.Payload
func (s *SignalMessage) Validate() error {
	return nil
}

// Schema implements message.Payload
func (s *SignalMessage) Schema() message.Type {
	return message.Type{Domain: agentic.Domain, Category: agentic.CategorySignalMessage, Version: agentic.SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (s *SignalMessage) MarshalJSON() ([]byte, error) {
	type Alias SignalMessage
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler
func (s *SignalMessage) UnmarshalJSON(data []byte) error {
	type Alias SignalMessage
	return json.Unmarshal(data, (*Alias)(s))
}

// SendSignal publishes a control signal to a loop via NATS.
func (t *LoopTracker) SendSignal(ctx context.Context, nc *natsclient.Client, loopID, signalType, reason string) error {
	if nc == nil {
		return ErrNATSClientNil
	}

	signal := SignalMessage{
		LoopID:    loopID,
		Type:      signalType,
		Reason:    reason,
		Timestamp: time.Now(),
	}

	signalMsg := message.NewBaseMessage(signal.Schema(), &signal, "agentic-dispatch")
	data, err := json.Marshal(signalMsg)
	if err != nil {
		return errs.Wrap(err, "LoopTracker", "SendSignal", fmt.Sprintf("marshal signal for loop %s", loopID))
	}

	subject := "agent.signal." + loopID
	if err := nc.PublishToStream(ctx, subject, data); err != nil {
		return errs.WrapTransient(err, "LoopTracker", "SendSignal", fmt.Sprintf("publish signal %s to loop %s on subject %s", signalType, loopID, subject))
	}

	if t.logger != nil {
		t.logger.Debug("signal published",
			slog.String("loop_id", loopID),
			slog.String("signal_type", signalType),
			slog.String("subject", subject),
			slog.String("reason", reason))
	}

	return nil
}

// ErrNATSClientNil is returned when NATS client is nil.
var ErrNATSClientNil = errs.ErrNoConnection
