package geojson

import (
	"encoding/json"
	"testing"
)

// TestRoundTrip_AllGeometries verifies marshal → unmarshal
// preserves the type discriminator and coordinate shape for
// every RFC 7946 geometry kind.
func TestRoundTrip_AllGeometries(t *testing.T) {
	cases := []struct {
		name string
		geom Geometry
	}{
		{"Point", Point{Coordinates: NewPosition(-122.5, 37.7)}},
		{"MultiPoint", MultiPoint{Coordinates: []Position{
			NewPosition(-122.5, 37.7),
			NewPosition(-122.4, 37.8),
		}}},
		{"LineString", LineString{Coordinates: []Position{
			NewPosition(-122.5, 37.7),
			NewPosition(-122.4, 37.8),
		}}},
		{"MultiLineString", MultiLineString{Coordinates: [][]Position{
			{NewPosition(0, 0), NewPosition(1, 1)},
			{NewPosition(2, 2), NewPosition(3, 3)},
		}}},
		{"Polygon", Polygon{Coordinates: [][]Position{
			{
				NewPosition(0, 0),
				NewPosition(1, 0),
				NewPosition(1, 1),
				NewPosition(0, 1),
				NewPosition(0, 0),
			},
		}}},
		{"PolygonWithHole", Polygon{Coordinates: [][]Position{
			{NewPosition(0, 0), NewPosition(10, 0), NewPosition(10, 10), NewPosition(0, 10), NewPosition(0, 0)},
			{NewPosition(3, 3), NewPosition(7, 3), NewPosition(7, 7), NewPosition(3, 7), NewPosition(3, 3)},
		}}},
		{"MultiPolygon", MultiPolygon{Coordinates: [][][]Position{
			{{NewPosition(0, 0), NewPosition(1, 0), NewPosition(1, 1), NewPosition(0, 1), NewPosition(0, 0)}},
			{{NewPosition(5, 5), NewPosition(6, 5), NewPosition(6, 6), NewPosition(5, 6), NewPosition(5, 5)}},
		}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.geom)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got, err := UnmarshalGeometry(data)
			if err != nil {
				t.Fatalf("UnmarshalGeometry: %v\nfrom: %s", err, string(data))
			}
			if got.Type() != c.geom.Type() {
				t.Errorf("Type after roundtrip: got %q, want %q", got.Type(), c.geom.Type())
			}
			// Re-marshal and compare; the simplest deep-equality
			// test that survives unordered-map-key issues. Both
			// should be JSON-equivalent byte streams.
			roundtripped, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(roundtripped) != string(data) {
				t.Errorf("roundtrip differs:\n  first:  %s\n  second: %s", data, roundtripped)
			}
		})
	}
}

func TestGeometryCollection_RoundTrip(t *testing.T) {
	gc := GeometryCollection{Geometries: []Geometry{
		Point{Coordinates: NewPosition(0, 0)},
		LineString{Coordinates: []Position{NewPosition(0, 0), NewPosition(1, 1)}},
	}}
	data, err := json.Marshal(gc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := UnmarshalGeometry(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	gotGC, ok := got.(GeometryCollection)
	if !ok {
		t.Fatalf("expected GeometryCollection, got %T", got)
	}
	if len(gotGC.Geometries) != 2 {
		t.Fatalf("expected 2 geometries, got %d", len(gotGC.Geometries))
	}
	if gotGC.Geometries[0].Type() != TypePoint {
		t.Errorf("first geometry type = %q, want Point", gotGC.Geometries[0].Type())
	}
	if gotGC.Geometries[1].Type() != TypeLineString {
		t.Errorf("second geometry type = %q, want LineString", gotGC.Geometries[1].Type())
	}
}

func TestUnmarshalGeometry_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"empty object", `{}`},
		{"missing coordinates", `{"type": "Point"}`},
		{"unknown type", `{"type": "Hyperbola", "coordinates": [0, 0]}`},
		{"malformed", `{"type":`},
		{"feature passed as geometry", `{"type": "Feature", "geometry": null, "properties": {}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := UnmarshalGeometry([]byte(c.json))
			if err == nil {
				t.Errorf("expected error for %s, got nil", c.name)
			}
		})
	}
}

func TestPolygon_OuterRing_Holes(t *testing.T) {
	p := Polygon{Coordinates: [][]Position{
		{NewPosition(0, 0), NewPosition(10, 0), NewPosition(10, 10), NewPosition(0, 10), NewPosition(0, 0)},
		{NewPosition(3, 3), NewPosition(7, 3), NewPosition(7, 7), NewPosition(3, 7), NewPosition(3, 3)},
	}}
	if len(p.OuterRing()) != 5 {
		t.Errorf("OuterRing(): want 5 positions, got %d", len(p.OuterRing()))
	}
	if len(p.Holes()) != 1 {
		t.Errorf("Holes(): want 1 hole, got %d", len(p.Holes()))
	}

	empty := Polygon{}
	if empty.OuterRing() != nil {
		t.Errorf("OuterRing on empty polygon should be nil")
	}
	if empty.Holes() != nil {
		t.Errorf("Holes on empty polygon should be nil")
	}
}

func TestNormalize_PropagatesThroughCollections(t *testing.T) {
	g := MultiPolygon{Coordinates: [][][]Position{
		{{
			NewPosition(190, 95), // out of range
			NewPosition(0, 0),
			NewPosition(190, 95),
		}},
	}}
	normalized := g.Normalize().(MultiPolygon)
	first := normalized.Coordinates[0][0][0]
	if first.Lon() != -170 || first.Lat() != 90 {
		t.Errorf("normalized first position: got (%v, %v), want (-170, 90)", first.Lon(), first.Lat())
	}
}
