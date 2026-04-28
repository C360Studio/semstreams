// Package rule - Workflow trigger payload for rule-to-workflow integration
package rule

import (
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// Workflow trigger payload type constants.
// Uses "rule" domain to avoid conflict with processor/workflow's workflow.trigger.v1
const (
	WorkflowTriggerDomain   = "rule"
	WorkflowTriggerCategory = "workflow_trigger"
	WorkflowTriggerVersion  = "v1"
)

// WorkflowTriggerPayload represents a message sent by the rule processor
// to trigger a reactive workflow. This payload is wrapped in a BaseMessage
// for proper deserialization by the reactive workflow engine.
type WorkflowTriggerPayload struct {
	WorkflowID  string         `json:"workflow_id"`
	EntityID    string         `json:"entity_id"`
	TriggeredAt time.Time      `json:"triggered_at"`
	RelatedID   string         `json:"related_id,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

// Schema returns the message type for workflow trigger payloads.
func (p *WorkflowTriggerPayload) Schema() message.Type {
	return message.Type{
		Domain:   WorkflowTriggerDomain,
		Category: WorkflowTriggerCategory,
		Version:  WorkflowTriggerVersion,
	}
}

// Validate ensures the payload has required fields.
func (p *WorkflowTriggerPayload) Validate() error {
	if p.WorkflowID == "" {
		return &ValidationError{Field: "workflow_id", Message: "required"}
	}
	if p.EntityID == "" {
		return &ValidationError{Field: "entity_id", Message: "required"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
// This marshals just the payload fields - BaseMessage handles the wrapping.
func (p *WorkflowTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias WorkflowTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *WorkflowTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias WorkflowTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ValidationError represents a payload validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "validation error: " + e.Field + " " + e.Message
}

// RegisterPayloads registers the workflow-trigger payload type with the
// supplied registry. Called from payloadbuiltins.Register at process
// bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      WorkflowTriggerDomain,
		Category:    WorkflowTriggerCategory,
		Version:     WorkflowTriggerVersion,
		Description: "Workflow trigger message from rule processor",
		Factory:     func() any { return &WorkflowTriggerPayload{} },
	})
}
