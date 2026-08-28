package agentic

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
)

// LoopCreatedEvent is published when a new agentic loop is created.
type LoopCreatedEvent struct {
	LoopID           string         `json:"loop_id"`
	TaskID           string         `json:"task_id"`
	Role             string         `json:"role"`
	Model            string         `json:"model"`
	WorkflowSlug     string         `json:"workflow_slug,omitempty"`
	WorkflowStep     string         `json:"workflow_step,omitempty"`
	ContextRequestID string         `json:"context_request_id,omitempty"`
	MaxIterations    int            `json:"max_iterations"`
	CreatedAt        time.Time      `json:"created_at"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	// RunID is the bare run loop-id this loop belongs to (ADR-053 D8).
	// Empty when the loop is not part of a run.
	RunID string `json:"run_id,omitempty"`
	// RunEntityID is the full 6-part chain execution entity ID for the run
	// (e.g. "org.platform.chain.agent.execution.<runID>"). Empty when RunID is empty.
	RunEntityID string `json:"run_entity_id,omitempty"`
}

// Validate implements message.Payload
func (e *LoopCreatedEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	return nil
}

// Schema implements message.Payload
func (e *LoopCreatedEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryLoopCreated, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (e *LoopCreatedEvent) MarshalJSON() ([]byte, error) {
	type Alias LoopCreatedEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler
func (e *LoopCreatedEvent) UnmarshalJSON(data []byte) error {
	type Alias LoopCreatedEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// LoopCompletedEvent is published when a loop completes successfully.
type LoopCompletedEvent struct {
	LoopID       string    `json:"loop_id"`
	TaskID       string    `json:"task_id"`
	Outcome      string    `json:"outcome"` // OutcomeSuccess
	Role         string    `json:"role"`
	Result       string    `json:"result"`
	Prompt       string    `json:"prompt,omitempty"` // Original user task prompt; enables NL/BM25 search
	Model        string    `json:"model"`
	Iterations   int       `json:"iterations"`
	TokensIn     int       `json:"tokens_in"`
	TokensOut    int       `json:"tokens_out"`
	ParentLoopID string    `json:"parent_loop,omitempty"`
	WorkflowSlug string    `json:"workflow_slug,omitempty"`
	WorkflowStep string    `json:"workflow_step,omitempty"`
	CompletedAt  time.Time `json:"completed_at"`
	// User routing info for response delivery
	ChannelType string         `json:"channel_type,omitempty"`
	ChannelID   string         `json:"channel_id,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	// RunID is the bare run loop-id this loop belongs to (ADR-053 D8).
	// Empty when the loop is not part of a run.
	RunID string `json:"run_id,omitempty"`
	// RunEntityID is the full 6-part chain execution entity ID for the run.
	// Empty when RunID is empty.
	RunEntityID string `json:"run_entity_id,omitempty"`
	// Decision is the typed decision of a `decide` terminal (ADR-101,
	// gh#1094). Nil for every other terminal — a non-decide StopLoop
	// tool, a model-text completion, or a synthesized needs_clarification
	// decision. Result is unchanged either way.
	Decision *CoordinatorDecision `json:"decision,omitempty"`
}

// Validate implements message.Payload
func (e *LoopCompletedEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	// A PRESENT decision must be complete. Both fields are load-bearing:
	// the action selects the terminal's user-facing class and the reason
	// IS the delivered content. Rejecting here means the fail-closed
	// terminal normalizer Terms a malformed decision instead of letting
	// an empty action fall through the classifier as a handoff and
	// silently drop a workflow's answer.
	if e.Decision != nil {
		if e.Decision.Action == "" {
			return fmt.Errorf("decision.action required when decision is present")
		}
		if e.Decision.Reason == "" {
			return fmt.Errorf("decision.reason required when decision is present")
		}
	}
	return nil
}

// Schema implements message.Payload
func (e *LoopCompletedEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryLoopCompleted, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (e *LoopCompletedEvent) MarshalJSON() ([]byte, error) {
	type Alias LoopCompletedEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler
func (e *LoopCompletedEvent) UnmarshalJSON(data []byte) error {
	type Alias LoopCompletedEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// LoopFailedEvent is published when a loop fails.
type LoopFailedEvent struct {
	LoopID     string `json:"loop_id"`
	TaskID     string `json:"task_id"`
	Outcome    string `json:"outcome"` // OutcomeFailed
	Reason     string `json:"reason"`
	Error      string `json:"error"`
	Role       string `json:"role"`
	Prompt     string `json:"prompt,omitempty"` // Original user task prompt; enables NL/BM25 search
	Model      string `json:"model"`
	Iterations int    `json:"iterations"`
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	// ParentLoopID enables ancestry walks from failed loops — required by
	// chain-aware failure handlers (e.g. semteams chainpause writing
	// chain.paused.* triples to the canonical chain entity per ADR-038).
	// Without this, the agent.loop.parent triple wasn't stamped on the
	// failed loop, so an ancestry walk terminated at the failure and
	// returned chain_id == failed_loop_id even when the failed loop
	// wasn't the chain root. Populated at construction the same way
	// LoopCompletedEvent.ParentLoopID is — entity.ParentLoopID flows
	// through.
	ParentLoopID string    `json:"parent_loop,omitempty"`
	WorkflowSlug string    `json:"workflow_slug,omitempty"`
	WorkflowStep string    `json:"workflow_step,omitempty"`
	FailedAt     time.Time `json:"failed_at"`
	// User routing info for error notifications
	ChannelType string         `json:"channel_type,omitempty"`
	ChannelID   string         `json:"channel_id,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	// RunID is the bare run loop-id this loop belongs to (ADR-053 D8).
	// Empty when the loop is not part of a run.
	RunID string `json:"run_id,omitempty"`
	// RunEntityID is the full 6-part chain execution entity ID for the run.
	// Empty when RunID is empty.
	RunEntityID string `json:"run_entity_id,omitempty"`
}

// Validate implements message.Payload
func (e *LoopFailedEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	return nil
}

// Schema implements message.Payload
func (e *LoopFailedEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryLoopFailed, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (e *LoopFailedEvent) MarshalJSON() ([]byte, error) {
	type Alias LoopFailedEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler
func (e *LoopFailedEvent) UnmarshalJSON(data []byte) error {
	type Alias LoopFailedEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// LoopCancelledEvent is published when a loop is cancelled by user action.
type LoopCancelledEvent struct {
	LoopID      string `json:"loop_id"`
	TaskID      string `json:"task_id"`
	Outcome     string `json:"outcome"` // OutcomeCancelled
	CancelledBy string `json:"cancelled_by"`
	// ParentLoopID enables ancestry walks from cancelled loops (parity with
	// LoopFailedEvent.ParentLoopID). Populated from LoopEntity.ParentLoopID
	// at cancellation construction time (ADR-053 D8).
	ParentLoopID string         `json:"parent_loop,omitempty"`
	WorkflowSlug string         `json:"workflow_slug,omitempty"`
	WorkflowStep string         `json:"workflow_step,omitempty"`
	CancelledAt  time.Time      `json:"cancelled_at"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	// RunID is the bare run loop-id this loop belongs to (ADR-053 D8).
	// Empty when the loop is not part of a run.
	RunID string `json:"run_id,omitempty"`
	// RunEntityID is the full 6-part chain execution entity ID for the run.
	// Empty when RunID is empty.
	RunEntityID string `json:"run_entity_id,omitempty"`
}

// Validate implements message.Payload
func (e *LoopCancelledEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	return nil
}

// Schema implements message.Payload
func (e *LoopCancelledEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryLoopCancelled, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (e *LoopCancelledEvent) MarshalJSON() ([]byte, error) {
	type Alias LoopCancelledEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler
func (e *LoopCancelledEvent) UnmarshalJSON(data []byte) error {
	type Alias LoopCancelledEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// ContextEvent represents a context management event (compaction, GC).
type ContextEvent struct {
	Type        string  `json:"type"` // ContextEventCompactionStarting, ContextEventCompactionComplete, ContextEventGCComplete
	LoopID      string  `json:"loop_id"`
	UserID      string  `json:"user_id,omitempty"` // owning user (provenance hook); lets a consumer scope user-scoped artifacts without a separate KV lookup
	Iteration   int     `json:"iteration"`
	Utilization float64 `json:"utilization,omitempty"`
	TokensSaved int     `json:"tokens_saved,omitempty"`
	Summary     string  `json:"summary,omitempty"`
}

// Validate implements message.Payload
func (e *ContextEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.Type == "" {
		return fmt.Errorf("type required")
	}
	return nil
}

// Schema implements message.Payload
func (e *ContextEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryContextEvent, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (e *ContextEvent) MarshalJSON() ([]byte, error) {
	type Alias ContextEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler
func (e *ContextEvent) UnmarshalJSON(data []byte) error {
	type Alias ContextEvent
	return json.Unmarshal(data, (*Alias)(e))
}
