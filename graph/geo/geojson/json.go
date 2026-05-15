package geojson

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON for Point.
func (p Point) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypePoint, p.Coordinates, nil)
}

// MarshalJSON for MultiPoint.
func (m MultiPoint) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypeMultiPoint, m.Coordinates, nil)
}

// MarshalJSON for LineString.
func (l LineString) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypeLineString, l.Coordinates, nil)
}

// MarshalJSON for MultiLineString.
func (m MultiLineString) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypeMultiLineString, m.Coordinates, nil)
}

// MarshalJSON for Polygon.
func (p Polygon) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypePolygon, p.Coordinates, nil)
}

// MarshalJSON for MultiPolygon.
func (m MultiPolygon) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypeMultiPolygon, m.Coordinates, nil)
}

// MarshalJSON for GeometryCollection.
func (g GeometryCollection) MarshalJSON() ([]byte, error) {
	return marshalGeometry(TypeGeometryCollection, nil, g.Geometries)
}

// marshalGeometry composes the type-discriminator + payload shape
// RFC 7946 requires. Either coordinates (single-geometry case) or
// geometries (collection case) is non-nil; never both.
func marshalGeometry(typ string, coordinates any, geometries []Geometry) ([]byte, error) {
	envelope := struct {
		Type        string     `json:"type"`
		Coordinates any        `json:"coordinates,omitempty"`
		Geometries  []Geometry `json:"geometries,omitempty"`
	}{
		Type:        typ,
		Coordinates: coordinates,
		Geometries:  geometries,
	}
	return json.Marshal(envelope)
}

// UnmarshalGeometry decodes a GeoJSON object into a Geometry of
// the appropriate concrete type, switching on the "type" field
// per RFC 7946 §3.1. The returned Geometry is the concrete value
// type (Point, LineString, …), not a pointer, so callers can
// type-switch directly:
//
//	geom, err := geojson.UnmarshalGeometry(data)
//	switch g := geom.(type) {
//	case geojson.Polygon: …
//	case geojson.Point:   …
//	}
//
// Returns an error if the JSON is malformed or if the "type"
// field names a kind this package does not handle (including
// "Feature" / "FeatureCollection" — use UnmarshalFeature /
// UnmarshalFeatureCollection for those, since they carry
// properties beyond a Geometry).
func UnmarshalGeometry(data []byte) (Geometry, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("geojson: probe type field: %w", err)
	}
	if probe.Type == "" {
		return nil, fmt.Errorf("geojson: object missing required %q field", "type")
	}

	switch probe.Type {
	case TypePoint:
		var g Point
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode Point: %w", err)
		}
		return g, nil
	case TypeMultiPoint:
		var g MultiPoint
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode MultiPoint: %w", err)
		}
		return g, nil
	case TypeLineString:
		var g LineString
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode LineString: %w", err)
		}
		return g, nil
	case TypeMultiLineString:
		var g MultiLineString
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode MultiLineString: %w", err)
		}
		return g, nil
	case TypePolygon:
		var g Polygon
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode Polygon: %w", err)
		}
		return g, nil
	case TypeMultiPolygon:
		var g MultiPolygon
		if err := decodeCoordinates(data, &g.Coordinates); err != nil {
			return nil, fmt.Errorf("geojson: decode MultiPolygon: %w", err)
		}
		return g, nil
	case TypeGeometryCollection:
		return decodeGeometryCollection(data)
	default:
		return nil, fmt.Errorf("geojson: unknown geometry type %q", probe.Type)
	}
}

// decodeCoordinates extracts the coordinates field from a GeoJSON
// envelope into the provided target. The target must be a pointer
// to a value compatible with the geometry's coordinate shape.
func decodeCoordinates(data []byte, target any) error {
	var envelope struct {
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("envelope: %w", err)
	}
	if len(envelope.Coordinates) == 0 {
		return fmt.Errorf("missing %q field", "coordinates")
	}
	return json.Unmarshal(envelope.Coordinates, target)
}

// decodeGeometryCollection recursively decodes the nested
// geometries array. Each element is dispatched through
// UnmarshalGeometry, which permits arbitrarily deep nesting (RFC
// 7946 §3.1.8 discourages but does not forbid).
func decodeGeometryCollection(data []byte) (Geometry, error) {
	var envelope struct {
		Geometries []json.RawMessage `json:"geometries"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	out := GeometryCollection{Geometries: make([]Geometry, 0, len(envelope.Geometries))}
	for i, raw := range envelope.Geometries {
		g, err := UnmarshalGeometry(raw)
		if err != nil {
			return nil, fmt.Errorf("geometries[%d]: %w", i, err)
		}
		out.Geometries = append(out.Geometries, g)
	}
	return out, nil
}
