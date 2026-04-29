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
}

// Validate implements message.Payload
func (e *LoopCompletedEvent) Validate() error {
	if e.LoopID == "" {
		return fmt.Errorf("loop_id required")
	}
	if e.TaskID == "" {
		return fmt.Errorf("task_id required")
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
	LoopID       string    `json:"loop_id"`
	TaskID       string    `json:"task_id"`
	Outcome      string    `json:"outcome"` // OutcomeFailed
	Reason       string    `json:"reason"`
	Error        string    `json:"error"`
	Role         string    `json:"role"`
	Prompt       string    `json:"prompt,omitempty"` // Original user task prompt; enables NL/BM25 search
	Model        string    `json:"model"`
	Iterations   int       `json:"iterations"`
	TokensIn     int       `json:"tokens_in"`
	TokensOut    int       `json:"tokens_out"`
	WorkflowSlug string    `json:"workflow_slug,omitempty"`
	WorkflowStep string    `json:"workflow_step,omitempty"`
	FailedAt     time.Time `json:"failed_at"`
	// User routing info for error notifications
	ChannelType string         `json:"channel_type,omitempty"`
	ChannelID   string         `json:"channel_id,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
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
	LoopID       string         `json:"loop_id"`
	TaskID       string         `json:"task_id"`
	Outcome      string         `json:"outcome"` // OutcomeCancelled
	CancelledBy  string         `json:"cancelled_by"`
	WorkflowSlug string         `json:"workflow_slug,omitempty"`
	WorkflowStep string         `json:"workflow_step,omitempty"`
	CancelledAt  time.Time      `json:"cancelled_at"`
	Metadata     map[string]any `json:"metadata,omitempty"`
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
	UserID      string  `json:"user_id,omitempty"` // owning user; consumers persist user-scoped artifacts (e.g. lessons-learned) when present
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
