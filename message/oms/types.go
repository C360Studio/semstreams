package oms

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/graph/geo/geojson"
)

// TypeObservation is the JSON "type" discriminator value for
// OMS Observation documents per OGC 20-082r4.
const TypeObservation = "Observation"

// Observation is the OGC OMS v3.0 Observation document. Fields
// mirror the JSON encoding bundled with CS API v1.0. Pointer-
// typed optional fields distinguish "absent" from "empty".
//
// The struct implements [message.Payload] (see payload.go) and
// [graph.Graphable] (see graphable.go) so it can be carried by
// a BaseMessage envelope and contribute triples to the graph
// pipeline.
type Observation struct {
	// ID is the local identifier for the observation. Required
	// when downstream consumers need to address the observation
	// individually; OMS itself does not mandate it.
	ID string `json:"id,omitempty"`

	// Procedure is the IRI of the SOSA Procedure / SensorML
	// process that produced the observation.
	Procedure string `json:"procedure"`

	// ObservedProperty is the IRI of the SOSA ObservableProperty
	// being measured.
	ObservedProperty string `json:"observedProperty"`

	// FeatureOfInterest is either a URI reference or an inline
	// GeoJSON Feature carrying the spatial extent.
	FeatureOfInterest *FeatureOfInterest `json:"featureOfInterest,omitempty"`

	// PhenomenonTime is the time the phenomenon being observed
	// occurred (ISO 8601 instant). Optional — when absent OMS
	// permits consumers to fall back to ResultTime.
	PhenomenonTime string `json:"phenomenonTime,omitempty"`

	// ResultTime is the time the result was produced (ISO 8601
	// instant). Required by OMS for every Observation.
	ResultTime string `json:"resultTime"`

	// Result is the observed value. Phase 6 MVP carries it as a
	// plain JSON value (number, string, boolean). Quantity,
	// Category, and TimeSeries results are deferred to follow-
	// up tags.
	//
	// JSON round-trip canonicalizes all numeric results to
	// float64 per encoding/json's default. Operators producing
	// integer-valued OMS results that need to preserve int64
	// fidelity through a round-trip should either (a) emit the
	// result as a string and convert at consumption, or (b)
	// wait for the typed Quantity / Category result envelopes
	// that land in a follow-up tag.
	Result any `json:"result,omitempty"`
}

// FeatureOfInterest is either a URI reference to an external
// feature definition or an inline GeoJSON Feature carrying the
// spatial extent. Marshaling produces the URI string for the
// URI case and the GeoJSON object for the inline case;
// unmarshaling detects the shape and populates the right field.
type FeatureOfInterest struct {
	// Href is set when the FoI is a bare URI reference.
	Href string

	// Feature is set when the FoI is an inline GeoJSON Feature.
	Feature *geojson.Feature
}

// NewFeatureOfInterestRef constructs a URI-shaped
// FeatureOfInterest.
func NewFeatureOfInterestRef(uri string) *FeatureOfInterest {
	return &FeatureOfInterest{Href: uri}
}

// NewFeatureOfInterestFeature constructs an inline-GeoJSON
// FeatureOfInterest.
func NewFeatureOfInterestFeature(f *geojson.Feature) *FeatureOfInterest {
	return &FeatureOfInterest{Feature: f}
}

// MarshalJSON emits either the bare URI string or the inline
// GeoJSON Feature shape, depending on which field is populated.
// When both are populated, Feature wins (the inline payload is
// strictly more informative). When neither is populated, emits
// JSON null.
func (f *FeatureOfInterest) MarshalJSON() ([]byte, error) {
	if f == nil {
		return []byte("null"), nil
	}
	if f.Feature != nil {
		return json.Marshal(f.Feature)
	}
	if f.Href != "" {
		return json.Marshal(f.Href)
	}
	return []byte("null"), nil
}

// UnmarshalJSON dispatches on the JSON shape: a bare string
// becomes the Href; a JSON object becomes the inline Feature.
// JSON null leaves the value unset.
func (f *FeatureOfInterest) UnmarshalJSON(data []byte) error {
	trimmed := trimWhitespace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	switch trimmed[0] {
	case '"':
		var href string
		if err := json.Unmarshal(data, &href); err != nil {
			return fmt.Errorf("oms: featureOfInterest URI: %w", err)
		}
		f.Href = href
		return nil
	case '{':
		feature, err := geojson.UnmarshalFeature(data)
		if err != nil {
			return fmt.Errorf("oms: featureOfInterest inline GeoJSON: %w", err)
		}
		f.Feature = &feature
		return nil
	default:
		return fmt.Errorf("oms: featureOfInterest must be a string URI or a GeoJSON Feature object, got %q", trimmed[0])
	}
}

// trimWhitespace strips leading ASCII whitespace from a JSON
// byte slice without allocating. Mirrors json.Unmarshal's
// own pre-processing.
func trimWhitespace(data []byte) []byte {
	for i, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return data[i:]
		}
	}
	return nil
}
