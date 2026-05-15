package geojson

import (
	"encoding/json"
	"fmt"
)

// Feature is an RFC 7946 §3.2 Feature object — a geometry plus
// an arbitrary JSON properties map, optionally with an id. The
// id, when present, MAY be a string or a number (RFC §3.2);
// callers that need the raw bytes can read RawID.
//
// The Geometry field is nilable: RFC 7946 §3.2 permits a Feature
// to carry a null geometry, used to attach properties to an
// implicit feature without spatial extent.
type Feature struct {
	Geometry   Geometry        `json:"-"`
	Properties map[string]any  `json:"properties,omitempty"`
	RawID      json.RawMessage `json:"id,omitempty"`
}

// Type implements Geometry for compatibility with code that
// type-switches over a unified hierarchy. Note that RFC 7946
// distinguishes Feature from the geometry kinds; this implementation
// gives Feature a Type() result of "Feature" for convenience but
// does not make Feature a substitute for a Geometry in
// UnmarshalGeometry.
func (Feature) Type() string { return TypeFeature }

// Normalize returns a copy of the Feature with its embedded
// Geometry normalized. The Properties map is shared with the
// original (shallow copy semantics) — callers mutating
// normalized.Properties affect the original.
func (f Feature) Normalize() Geometry {
	out := Feature{
		Properties: f.Properties,
		RawID:      f.RawID,
	}
	if f.Geometry != nil {
		out.Geometry = f.Geometry.Normalize()
	}
	return out
}

// MarshalJSON serializes a Feature, dispatching the embedded
// Geometry through its own MarshalJSON so the type-discriminator
// and coordinates land in the expected RFC 7946 shape.
func (f Feature) MarshalJSON() ([]byte, error) {
	var geomRaw json.RawMessage
	if f.Geometry != nil {
		b, err := json.Marshal(f.Geometry)
		if err != nil {
			return nil, fmt.Errorf("geojson: marshal feature geometry: %w", err)
		}
		geomRaw = b
	} else {
		geomRaw = json.RawMessage("null")
	}
	envelope := struct {
		Type       string          `json:"type"`
		Geometry   json.RawMessage `json:"geometry"`
		Properties map[string]any  `json:"properties"`
		ID         json.RawMessage `json:"id,omitempty"`
	}{
		Type:       TypeFeature,
		Geometry:   geomRaw,
		Properties: f.Properties,
		ID:         f.RawID,
	}
	return json.Marshal(envelope)
}

// UnmarshalFeature decodes a GeoJSON Feature object, dispatching
// the embedded geometry through UnmarshalGeometry. Enforces RFC
// 7946 §3.2: a Feature MUST have a "geometry" key (with object
// or null value) and a "properties" key (with object or null
// value). Both keys are required; only their values may be null.
func UnmarshalFeature(data []byte) (Feature, error) {
	// First pass: parse into a generic map to assert presence of
	// the required keys. The decode-into-struct second pass is
	// where we extract typed values.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return Feature{}, fmt.Errorf("geojson: decode feature envelope: %w", err)
	}
	if _, ok := probe["geometry"]; !ok {
		return Feature{}, fmt.Errorf("geojson: feature missing required %q key", "geometry")
	}
	if _, ok := probe["properties"]; !ok {
		return Feature{}, fmt.Errorf("geojson: feature missing required %q key", "properties")
	}

	var envelope struct {
		Type       string          `json:"type"`
		Geometry   json.RawMessage `json:"geometry"`
		Properties map[string]any  `json:"properties"`
		ID         json.RawMessage `json:"id,omitempty"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Feature{}, fmt.Errorf("geojson: decode feature envelope: %w", err)
	}
	if envelope.Type != TypeFeature {
		return Feature{}, fmt.Errorf("geojson: expected type %q, got %q", TypeFeature, envelope.Type)
	}
	f := Feature{
		Properties: envelope.Properties,
		RawID:      envelope.ID,
	}
	if len(envelope.Geometry) > 0 && string(envelope.Geometry) != "null" {
		geom, err := UnmarshalGeometry(envelope.Geometry)
		if err != nil {
			return Feature{}, fmt.Errorf("geojson: decode feature geometry: %w", err)
		}
		f.Geometry = geom
	}
	return f, nil
}

// FeatureCollection is an RFC 7946 §3.3 FeatureCollection — an
// ordered array of Feature objects.
type FeatureCollection struct {
	Features []Feature `json:"features"`
}

// Type implements Geometry.
func (FeatureCollection) Type() string { return TypeFeatureCollection }

// Normalize returns a copy of the FeatureCollection with every
// Feature normalized.
func (fc FeatureCollection) Normalize() Geometry {
	out := FeatureCollection{Features: make([]Feature, len(fc.Features))}
	for i, f := range fc.Features {
		out.Features[i] = f.Normalize().(Feature)
	}
	return out
}

// MarshalJSON for FeatureCollection.
func (fc FeatureCollection) MarshalJSON() ([]byte, error) {
	envelope := struct {
		Type     string    `json:"type"`
		Features []Feature `json:"features"`
	}{
		Type:     TypeFeatureCollection,
		Features: fc.Features,
	}
	return json.Marshal(envelope)
}

// UnmarshalFeatureCollection decodes a GeoJSON FeatureCollection
// object, dispatching each feature through UnmarshalFeature.
func UnmarshalFeatureCollection(data []byte) (FeatureCollection, error) {
	var envelope struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return FeatureCollection{}, fmt.Errorf("geojson: decode feature collection envelope: %w", err)
	}
	if envelope.Type != TypeFeatureCollection {
		return FeatureCollection{}, fmt.Errorf("geojson: expected type %q, got %q", TypeFeatureCollection, envelope.Type)
	}
	fc := FeatureCollection{Features: make([]Feature, 0, len(envelope.Features))}
	for i, raw := range envelope.Features {
		f, err := UnmarshalFeature(raw)
		if err != nil {
			return FeatureCollection{}, fmt.Errorf("geojson: features[%d]: %w", i, err)
		}
		fc.Features = append(fc.Features, f)
	}
	return fc, nil
}
