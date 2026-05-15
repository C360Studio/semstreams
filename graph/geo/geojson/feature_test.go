package geojson

import (
	"encoding/json"
	"testing"
)

func TestFeature_RoundTrip(t *testing.T) {
	f := Feature{
		Geometry: Point{Coordinates: NewPosition(-122.5, 37.7)},
		Properties: map[string]any{
			"name":     "drone-001",
			"altitude": 100.0,
		},
		RawID: json.RawMessage(`"abc-123"`),
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := UnmarshalFeature(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v\nfrom: %s", err, data)
	}
	if got.Geometry == nil {
		t.Fatal("expected non-nil geometry")
	}
	if got.Geometry.Type() != TypePoint {
		t.Errorf("geometry type: got %q, want Point", got.Geometry.Type())
	}
	if got.Properties["name"] != "drone-001" {
		t.Errorf("properties name: got %v, want drone-001", got.Properties["name"])
	}
	if string(got.RawID) != `"abc-123"` {
		t.Errorf("RawID: got %s, want %q", got.RawID, "abc-123")
	}
}

func TestFeature_NullGeometry(t *testing.T) {
	const raw = `{"type":"Feature","geometry":null,"properties":{"k":"v"}}`
	f, err := UnmarshalFeature([]byte(raw))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if f.Geometry != nil {
		t.Errorf("expected nil geometry, got %T", f.Geometry)
	}
}

func TestFeatureCollection_RoundTrip(t *testing.T) {
	fc := FeatureCollection{Features: []Feature{
		{
			Geometry:   Point{Coordinates: NewPosition(0, 0)},
			Properties: map[string]any{"i": float64(1)},
		},
		{
			Geometry:   Point{Coordinates: NewPosition(1, 1)},
			Properties: map[string]any{"i": float64(2)},
		},
	}}
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := UnmarshalFeatureCollection(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(got.Features))
	}
	if got.Features[0].Properties["i"] != float64(1) {
		t.Errorf("first feature property i: got %v, want 1", got.Features[0].Properties["i"])
	}
}

func TestUnmarshalFeature_RejectsBadInput(t *testing.T) {
	cases := []string{
		`{"type": "Point", "coordinates": [0,0]}`, // not a Feature
		`{"type": "Feature"}`,                     // missing geometry+props
		`{"type": "Feature", "geometry": {"type": "Hyperbola"}}`,
	}
	for _, c := range cases {
		_, err := UnmarshalFeature([]byte(c))
		if err == nil {
			t.Errorf("expected error for %s, got nil", c)
		}
	}
}
