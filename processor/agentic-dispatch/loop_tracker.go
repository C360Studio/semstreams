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
}

// LoopTracker tracks active loops per user and channel
type LoopTracker struct {
	mu           sync.RWMutex
	userLoops    map[string]string    // user_id -> most recent loop_id
	channelLoops map[string]string    // channel_id -> most recent loop_id
	loops        map[string]*LoopInfo // loop_id -> LoopInfo
	logger       *slog.Logger
}

// NewLoopTracker creates a new LoopTracker
func NewLoopTracker() *LoopTracker {
	return &LoopTracker{
		userLoops:    make(map[string]string),
		channelLoops: make(map[string]string),
		loops:        make(map[string]*LoopInfo),
	}
}

// NewLoopTrackerWithLogger creates a new LoopTracker with logging.
func NewLoopTrackerWithLogger(logger *slog.Logger) *LoopTracker {
	return &LoopTracker{
		userLoops:    make(map[string]string),
		channelLoops: make(map[string]string),
		loops:        make(map[string]*LoopInfo),
		logger:       logger,
	}
}

// SetLogger sets the logger for the LoopTracker.
func (t *LoopTracker) SetLogger(logger *slog.Logger) {
	t.logger = logger
}

// Track adds or updates a loop in the tracker
func (t *LoopTracker) Track(info *LoopInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, existed := t.loops[info.LoopID]
	t.loops[info.LoopID] = info
	t.userLoops[info.UserID] = info.LoopID
	if info.ChannelID != "" {
		t.channelLoops[info.ChannelID] = info.LoopID
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
// It also updates the State field to match the terminal state implied by the outcome.
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
