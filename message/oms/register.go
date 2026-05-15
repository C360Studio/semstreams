package oms

import (
	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers the ogc.oms.v3 Observation payload
// with the supplied registry. Called from
// [github.com/c360studio/semstreams/payloadbuiltins.Register] at
// process bootstrap so every production binary picks up the
// type without extra wiring.
//
// Mirrors the [message.RegisterPayloads] / agentic.RegisterPayloads
// shape so the bootstrap aggregator can call this uniformly.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      schemaType.Domain,
		Category:    schemaType.Category,
		Version:     schemaType.Version,
		Description: "OGC OMS v3.0 Observation document (Connected Systems API bundle)",
		Factory: func() any {
			return &Observation{}
		},
		Example: map[string]any{
			"type":             "Observation",
			"id":               "observation-7f3a",
			"procedure":        "http://example.org/procedures/voltmeter",
			"observedProperty": "http://example.org/properties/voltage",
			"resultTime":       "2026-05-15T14:30:00Z",
			"result":           12.4,
		},
	})
}
