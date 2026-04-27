package federation

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// EventPayload implements message.Payload and graph.Graphable for federation graph events.
// It wraps Event for transport through the semstreams message bus. Because it implements
// Graphable, graph-ingest processes federation entities natively — no separate federation
// processor is needed.
type EventPayload struct {
	Event Event `json:"event"`
}

// Schema returns the message type for the Payload interface.
// The domain is set to "federation" as a default; consuming services register
// their own domain via RegisterPayload.
func (p *EventPayload) Schema() message.Type {
	return message.Type{Domain: "federation", Category: "graph_event", Version: "v1"}
}

// Validate validates the payload for the Payload interface.
func (p *EventPayload) Validate() error {
	if p.Event.Entity.ID == "" && p.Event.Type != EventTypeHEARTBEAT && p.Event.Type != EventTypeRETRACT {
		return fmt.Errorf("entity ID is required for %s events", p.Event.Type)
	}
	return p.Event.Validate()
}

// EntityID implements graph.Graphable. Returns the 6-part entity identifier.
func (p *EventPayload) EntityID() string {
	return p.Event.Entity.ID
}

// Triples implements graph.Graphable. Returns the entity's triples.
func (p *EventPayload) Triples() []message.Triple {
	return p.Event.Entity.Triples
}

// MarshalJSON implements json.Marshaler.
func (p *EventPayload) MarshalJSON() ([]byte, error) {
	type Alias EventPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *EventPayload) UnmarshalJSON(data []byte) error {
	type Alias EventPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// RegisterPayloads registers the federation event payload for a
// specific domain on the supplied registry. Each sem* service calls
// this with its own domain (e.g., "semsource", "semquery"). NOT
// included in payloadbuiltins.Register because it's
// domain-parameterized — callers know their domain at boot time.
func RegisterPayloads(reg *payloadregistry.Registry, domain string) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      domain,
		Category:    "graph_event",
		Version:     "v1",
		Description: "Federation graph event payload for " + domain,
		Factory:     func() any { return &EventPayload{} },
	})
}

// RegisterPayload preserved as a transitional alias to the global —
// downstream products that haven't migrated keep working through C4.
// Deprecated in beta.18; use RegisterPayloads(reg, domain) instead.
func RegisterPayload(domain string) error {
	return RegisterPayloads(payloadregistry.Global(), domain)
}
