package gateddagexec

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// Payload registry coordinates for the dispatch envelope.
const (
	// Domain groups gated-DAG executor payloads.
	Domain = "gateddag"
	// SchemaVersion is the payload schema version.
	SchemaVersion = "v1"
	// CategoryDispatch is the dispatch-reference envelope category.
	CategoryDispatch = "dispatch"
)

// DispatchMessage is the reference envelope published to Config.DispatchSubject
// when a unit becomes dispatchable. Per the orchestration rule "rules/dispatch
// carry references, never content", it carries only identifiers — the consumer
// retrieves the unit's details from the graph on demand.
//
// It is a registered payload (wrapped in a BaseMessage at publish time) so the
// publish honors the payload-registry contract even though the immediate
// consumer may read it raw.
type DispatchMessage struct {
	// UnitEntityID is the 6-part federated ID of the dispatchable unit. The
	// consumer turns this reference into real work (e.g. a publish_agent).
	UnitEntityID string `json:"unit_entity_id"`
	// FanOutWorkflow is the workflow name of the fan-out instance that owns this
	// unit (provenance / routing for the consumer).
	FanOutWorkflow string `json:"fan_out_workflow,omitempty"`
}

// Schema returns the type discriminator for registry routing.
func (d *DispatchMessage) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryDispatch, Version: SchemaVersion}
}

// Validate checks required fields.
func (d *DispatchMessage) Validate() error {
	if d.UnitEntityID == "" {
		return errors.New("dispatch message: unit_entity_id is required")
	}
	return nil
}

// MarshalJSON marshals the payload fields (alias avoids MarshalJSON recursion).
func (d *DispatchMessage) MarshalJSON() ([]byte, error) {
	type Alias DispatchMessage
	return json.Marshal((*Alias)(d))
}

// UnmarshalJSON unmarshals the payload fields (alias avoids recursion).
func (d *DispatchMessage) UnmarshalJSON(data []byte) error {
	type Alias DispatchMessage
	return json.Unmarshal(data, (*Alias)(d))
}

// RegisterPayloads registers the gated-DAG executor payload types with the
// supplied registry. Wired into payloadbuiltins.Register so every binary that
// can run the executor can decode its dispatch envelope.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{Domain: Domain, Category: CategoryDispatch, Version: SchemaVersion, Description: "Gated-DAG dispatch reference", Factory: func() any { return &DispatchMessage{} }},
	}
	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s.%s.%s: %w", r.Domain, r.Category, r.Version, err))
		}
	}
	return errors.Join(errs...)
}
