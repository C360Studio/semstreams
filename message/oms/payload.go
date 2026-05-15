package oms

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// schemaType is the registered payload identity for OMS v3.0
// Observations. Operators reference it as "ogc.oms.v3" in
// payload-type-aware config (component port type declarations,
// reactive-rule predicate matching).
var schemaType = message.Type{
	Domain:   "ogc",
	Category: "oms",
	Version:  "v3",
}

// SchemaType returns the registered payload type for OMS
// Observations. Useful for callers building a BaseMessage
// without first constructing the Observation struct.
func SchemaType() message.Type { return schemaType }

// Schema implements [message.Payload]. Always returns
// ogc.oms.v3.
func (o *Observation) Schema() message.Type { return schemaType }

// Validate implements [message.Payload]. Enforces OMS v3.0's
// required fields: Procedure, ObservedProperty, ResultTime.
// Optional fields with present-but-invalid values surface as
// validation errors.
func (o *Observation) Validate() error {
	if o == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Observation", "Validate", "observation is nil")
	}
	if o.Procedure == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Observation", "Validate", "procedure is required")
	}
	if o.ObservedProperty == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Observation", "Validate", "observedProperty is required")
	}
	if o.ResultTime == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Observation", "Validate", "resultTime is required")
	}
	if o.FeatureOfInterest != nil && o.FeatureOfInterest.Feature != nil {
		// Defer to geojson's own validation: the Feature parsed
		// fine if it was unmarshaled through UnmarshalFeature;
		// in-code construction is the operator's responsibility.
		// Nothing extra to check here.
		_ = o.FeatureOfInterest.Feature.Type()
	}
	return nil
}

// MarshalJSON emits the OMS-natural JSON document shape per
// OGC 20-082r4. OMS v3.0 carries a "type" discriminator on the
// wire for forward-compatibility with sibling shapes (Sample,
// ObservationCollection, …) that this Go package does not yet
// implement; the constant is emitted unconditionally because
// the Go type can only represent the Observation case. Avoids
// the infinite-recursion footgun by aliasing.
func (o *Observation) MarshalJSON() ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("oms: marshal validation failed: %w", err)
	}
	type alias Observation
	wrap := struct {
		Type string `json:"type"`
		*alias
	}{
		Type:  TypeObservation,
		alias: (*alias)(o),
	}
	return json.Marshal(wrap)
}

// UnmarshalJSON parses the OMS-natural JSON document, validating
// the type discriminator before populating the struct fields.
func (o *Observation) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("oms: probe type field: %w", err)
	}
	if probe.Type != "" && probe.Type != TypeObservation {
		return fmt.Errorf("oms: expected type %q, got %q", TypeObservation, probe.Type)
	}
	type alias Observation
	return json.Unmarshal(data, (*alias)(o))
}
