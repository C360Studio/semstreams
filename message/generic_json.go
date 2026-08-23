// Package message provides the GenericJSON payload for StreamKit.
package message

import (
	"encoding/json"

	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
)

func buildGenericJSONPayload(fields map[string]any) (any, error) {
	msg := &GenericJSONPayload{}

	if v, ok := fields["data"].(map[string]any); ok {
		msg.Data = v
	}

	if err := msg.Validate(); err != nil {
		return nil, errs.Wrap(err, "GenericJSONPayload", "buildGenericJSONPayload", "validation failed")
	}

	return msg, nil
}

// RegisterPayloads registers the GenericJSON payload type (core.json.v1)
// with the supplied registry. Called from payloadbuiltins.Register at
// process bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "core",
		Category:    "json",
		Version:     "v1",
		Description: "Generic JSON payload for testing, prototyping, and basic data processing",
		Factory: func() any {
			return &GenericJSONPayload{}
		},
		Builder: buildGenericJSONPayload,
		Example: map[string]any{
			"data": map[string]any{
				"sensor_id":   "temp-001",
				"temperature": 23.5,
				"unit":        "celsius",
			},
		},
	})
}

// GenericJSONPayload provides a simple, explicitly flexible payload type
// for testing, prototyping, and basic data processing flows.
//
// This is an intentional, well-known type (core.json.v1) designed for:
//   - Rapid prototyping of flows
//   - Integration testing
//   - Basic JSON data processing (filter, map, transform)
//   - Simple ETL pipelines
//
// Components that work with GenericJSON (JSONFilter, JSONMap) explicitly
// declare they require "core.json.v1" type, providing type safety while
// maintaining flexibility for arbitrary JSON structures.
//
// Example usage:
//
//	payload := &GenericJSONPayload{
//	    Data: map[string]any{
//	        "sensor_id": "temp-001",
//	        "temperature": 23.5,
//	        "unit": "celsius",
//	    },
//	}
type GenericJSONPayload struct {
	// Data contains the JSON payload as a map.
	// This supports arbitrary JSON structures while remaining type-safe
	// at the component level (components declare they work with core.json.v1).
	Data map[string]any `json:"data"`
}

// NewGenericJSON creates a new GenericJSON payload with the given data.
func NewGenericJSON(data map[string]any) *GenericJSONPayload {
	return &GenericJSONPayload{
		Data: data,
	}
}

// Schema returns the payload type identifier for GenericJSON.
// Always returns core.json.v1 as this is the well-known type for
// generic JSON processing in StreamKit.
func (g *GenericJSONPayload) Schema() Type {
	return Type{
		Domain:   "core",
		Category: "json",
		Version:  "v1",
	}
}

// Validate performs basic validation on the GenericJSON payload.
// Ensures the data map is not nil.
func (g *GenericJSONPayload) Validate() error {
	if g.Data == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "GenericJSONPayload", "Validate", "data cannot be nil")
	}
	return nil
}

// MarshalJSON serializes the GenericJSON payload to JSON format.
// The output format matches the input structure with a "data" wrapper.
func (g *GenericJSONPayload) MarshalJSON() ([]byte, error) {
	// Use alias to avoid infinite recursion
	type Alias GenericJSONPayload
	return json.Marshal((*Alias)(g))
}

// UnmarshalJSON deserializes JSON data into the GenericJSON payload.
func (g *GenericJSONPayload) UnmarshalJSON(data []byte) error {
	// Use alias to avoid infinite recursion
	type Alias GenericJSONPayload
	return json.Unmarshal(data, (*Alias)(g))
}

// RuleFields implements RuleReadable: the whole data map IS this payload's
// declared projection.
//
// core.json.v1 exists to carry arbitrary caller-supplied JSON with no schema
// of its own, so there is no narrower honest answer — the payload's author is
// the caller, and the caller already chose every key. It is also the behavior
// every rule written before RuleReadable existed depends on, which is why
// exposing Data verbatim keeps them all valid.
func (g *GenericJSONPayload) RuleFields() map[string]any {
	return g.Data
}
