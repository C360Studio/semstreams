// Package fixtures registers the message types the e2e scenarios stamp on
// entity.create as verbatim carriers, so the e2e binary's graph-ingest admits
// them under ADR-103 (a stamp the registry does not know is refused).
//
// Tier → keys: core and lessons → test.fixture.v1; crud-tools → e2e.probe.v1;
// structural → e2e.eventtime.v1, e2e.canonical_create_contract.v1,
// e2e.relationship_contract.v1; research-graph → research.e2e_search_seed.v1.
package fixtures

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// Carrier is the verbatim Graphable payload every e2e fixture type decodes
// to: the factory pre-sets Type, and the wire form carries only the entity ID
// and its facts.
type Carrier struct {
	Type  message.Type     `json:"-"`
	ID    string           `json:"id"`
	Facts []message.Triple `json:"facts"`
}

// EntityID implements graph.Graphable.
func (c *Carrier) EntityID() string { return c.ID }

// Triples implements graph.Graphable: the facts, verbatim.
func (c *Carrier) Triples() []message.Triple { return c.Facts }

// Schema implements message.Payload.
func (c *Carrier) Schema() message.Type { return c.Type }

// Validate implements message.Payload.
func (c *Carrier) Validate() error { return semtypes.ValidateEntityID(c.ID) }

// MarshalJSON implements json.Marshaler with the alias idiom.
func (c *Carrier) MarshalJSON() ([]byte, error) {
	type alias Carrier
	return json.Marshal((*alias)(c))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom; Type stays
// as the factory set it.
func (c *Carrier) UnmarshalJSON(data []byte) error {
	type alias Carrier
	return json.Unmarshal(data, (*alias)(c))
}

// e2eStamps lists every key a scenario stamps on entity.create.
var e2eStamps = []message.Type{
	{Domain: "test", Category: "fixture", Version: "v1"},
	{Domain: "e2e", Category: "probe", Version: "v1"},
	{Domain: "e2e", Category: "eventtime", Version: "v1"},
	{Domain: "e2e", Category: "canonical_create_contract", Version: "v1"},
	{Domain: "e2e", Category: "relationship_contract", Version: "v1"},
	{Domain: "research", Category: "e2e_search_seed", Version: "v1"},
}

// RegisterPayloads registers every e2e stamp as a verbatim carrier with floor
// control. Called from cmd/e2e-semstreams's buildPayloadRegistry after
// payloadbuiltins.Register.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	var errs []error
	for _, mt := range e2eStamps {
		key := mt
		if err := reg.Register(&payloadregistry.Registration{
			Domain:          key.Domain,
			Category:        key.Category,
			Version:         key.Version,
			Description:     "e2e scenario fixture entity (verbatim carrier)",
			Factory:         func() any { return &Carrier{Type: key} },
			IndexingProfile: vocabulary.IndexingProfileControl,
		}); err != nil {
			errs = append(errs, fmt.Errorf("register %s: %w", key.Key(), err))
		}
	}
	return errors.Join(errs...)
}
