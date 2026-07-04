package gateddagexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

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
	// CategoryStall is the stall-event category (GH #365.3).
	CategoryStall = "stall"
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

// StallEvent is the edge-triggered wedge-detection event published to
// Config.StallSubject on the 0→non-zero stall transition (GH #365.3): the
// fan-out has held-ready units with no forward progress and nothing in-flight
// (a depends_on cycle, or every non-terminal unit blocked behind a failure).
// References only — the consumer reads the units' state from the graph.
type StallEvent struct {
	// StalledUnits are the held-ready unit IDs with no forward progress.
	StalledUnits []string `json:"stalled_units"`
	// FanOutWorkflow / FanOutInstanceID identify the affected fan-out (provenance).
	FanOutWorkflow   string `json:"fan_out_workflow,omitempty"`
	FanOutInstanceID string `json:"fan_out_instance_id,omitempty"`
}

// Schema returns the type discriminator for registry routing.
func (s *StallEvent) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryStall, Version: SchemaVersion}
}

// Validate checks required fields.
func (s *StallEvent) Validate() error {
	if len(s.StalledUnits) == 0 {
		return errors.New("stall event: stalled_units is required")
	}
	return nil
}

// MarshalJSON marshals the payload fields (alias avoids recursion).
func (s *StallEvent) MarshalJSON() ([]byte, error) {
	type Alias StallEvent
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON unmarshals the payload fields (alias avoids recursion).
func (s *StallEvent) UnmarshalJSON(data []byte) error {
	type Alias StallEvent
	return json.Unmarshal(data, (*Alias)(s))
}

// RegisterPayloads registers the gated-DAG executor payload types with the
// supplied registry. Wired into payloadbuiltins.Register so every binary that
// can run the executor can decode its dispatch + stall envelopes.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{Domain: Domain, Category: CategoryDispatch, Version: SchemaVersion, Description: "Gated-DAG dispatch reference", Factory: func() any { return &DispatchMessage{} }},
		{Domain: Domain, Category: CategoryStall, Version: SchemaVersion, Description: "Gated-DAG stall (wedge) event", Factory: func() any { return &StallEvent{} }},
	}
	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s.%s.%s: %w", r.Domain, r.Category, r.Version, err))
		}
	}
	return errors.Join(errs...)
}

// dispatchDecoder is the shared BaseMessage decoder for the dispatch envelope,
// built once (RegisterPayloads is idempotent per registry but the registry+decoder
// need only be constructed one time).
var (
	dispatchDecoderOnce sync.Once
	dispatchDecoder     *message.Decoder
	dispatchDecoderErr  error
)

func dispatchDecoderInstance() (*message.Decoder, error) {
	dispatchDecoderOnce.Do(func() {
		reg := payloadregistry.New()
		if err := RegisterPayloads(reg); err != nil {
			dispatchDecoderErr = fmt.Errorf("register gateddag payloads: %w", err)
			return
		}
		dispatchDecoder = message.NewDecoder(reg)
	})
	return dispatchDecoder, dispatchDecoderErr
}

// DecodeDispatch decodes a raw dispatch message — a registry-wrapped BaseMessage
// carrying a DispatchMessage, as delivered by the durable dispatch stream — into
// the typed payload. Consumers of the stream import this so the BaseMessage →
// DispatchMessage unwrap is owned once here (above natsclient, which hands the
// raw []byte) rather than reinvented per consumer repo (ADR-070 B5).
func DecodeDispatch(data []byte) (*DispatchMessage, error) {
	dec, err := dispatchDecoderInstance()
	if err != nil {
		return nil, err
	}
	base, err := dec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode dispatch base message: %w", err)
	}
	msg, ok := base.Payload().(*DispatchMessage)
	if !ok {
		return nil, fmt.Errorf("decoded dispatch payload is %T, want *DispatchMessage", base.Payload())
	}
	return msg, nil
}
