package inference

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// HierarchyContainerMessageType returns the message.Type graph-ingest stamps on
// the hierarchy container entities it births — key "graph.hierarchy_container.v1".
// Registered by RegisterPayloads (via payloadbuiltins.Register) with floor
// control (ADR-103, O-16 (a)): the framework's own in-process writer passes
// the same registered-type gate as every other birth. Transitional until
// gh606 retires containers; the registration retires with them.
func HierarchyContainerMessageType() message.Type {
	return message.Type{
		Domain:   "graph",
		Category: "hierarchy_container",
		Version:  "v1",
	}
}

// ContainerEntity is the verbatim Graphable carrier for a hierarchy container:
// EntityID() is the field and Triples() returns the facts unchanged.
type ContainerEntity struct {
	ID    string           `json:"id"`
	Facts []message.Triple `json:"facts"`
}

// EntityID implements graph.Graphable.
func (e *ContainerEntity) EntityID() string { return e.ID }

// Triples implements graph.Graphable: the facts, verbatim.
func (e *ContainerEntity) Triples() []message.Triple { return e.Facts }

// Schema implements message.Payload.
func (e *ContainerEntity) Schema() message.Type { return HierarchyContainerMessageType() }

// Validate implements message.Payload: the carried ID must be a well-formed
// entity ID.
func (e *ContainerEntity) Validate() error { return semtypes.ValidateEntityID(e.ID) }

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *ContainerEntity) MarshalJSON() ([]byte, error) {
	type alias ContainerEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *ContainerEntity) UnmarshalJSON(data []byte) error {
	type alias ContainerEntity
	return json.Unmarshal(data, (*alias)(e))
}

// RegisterPayloads registers graph.hierarchy_container.v1 with the supplied
// registry. Called from payloadbuiltins.Register at process bootstrap;
// graph-ingest refuses to construct with hierarchy enabled unless its registry
// holds this type.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:          "graph",
		Category:        "hierarchy_container",
		Version:         "v1",
		Description:     "Hierarchy container entity born by graph-ingest's structural inference (verbatim carrier)",
		Factory:         func() any { return &ContainerEntity{} },
		IndexingProfile: vocabulary.IndexingProfileControl,
	})
}
