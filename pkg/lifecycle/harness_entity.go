package lifecycle

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// HarnessMessageType returns the message.Type stamped on every entity the
// harness Manager births — key "lifecycle.harness.v1". Registered by
// RegisterPayloads (via payloadbuiltins.Register) with floor control
// (ADR-103), so a harness birth passes graph-ingest's registered-type gate and
// a harness entity can arrive on the fact lane as itself.
func HarnessMessageType() message.Type {
	return message.Type{
		Domain:   "lifecycle",
		Category: "harness",
		Version:  "v1",
	}
}

// HarnessEntity is the verbatim Graphable carrier for a harness-born entity.
// The harness's triples come from the registered workflow schema
// (Manager.Register), so the carrier holds them as facts rather than deriving
// them: EntityID() is the field and Triples() returns the facts unchanged.
// Registering it makes the fact-lane merge path reachable for lifecycle
// entities: a marshalled harness entity arriving on a Graphable input merges
// by predicate replacement like any other Graphable.
type HarnessEntity struct {
	ID    string           `json:"id"`
	Facts []message.Triple `json:"facts"`
}

// EntityID implements graph.Graphable.
func (e *HarnessEntity) EntityID() string { return e.ID }

// Triples implements graph.Graphable: the facts, verbatim.
func (e *HarnessEntity) Triples() []message.Triple { return e.Facts }

// Schema implements message.Payload.
func (e *HarnessEntity) Schema() message.Type { return HarnessMessageType() }

// Validate implements message.Payload: the carried ID must be a well-formed
// entity ID.
func (e *HarnessEntity) Validate() error { return semtypes.ValidateEntityID(e.ID) }

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *HarnessEntity) MarshalJSON() ([]byte, error) {
	type alias HarnessEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *HarnessEntity) UnmarshalJSON(data []byte) error {
	type alias HarnessEntity
	return json.Unmarshal(data, (*alias)(e))
}

// RegisterPayloads registers lifecycle.harness.v1 with the supplied registry.
// Called from payloadbuiltins.Register at process bootstrap; a product that
// builds its own registry and births lifecycle participants must call it too.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:          "lifecycle",
		Category:        "harness",
		Version:         "v1",
		Description:     "Lifecycle harness participant entity (verbatim carrier)",
		Factory:         func() any { return &HarnessEntity{} },
		IndexingProfile: vocabulary.IndexingProfileControl,
	})
}
