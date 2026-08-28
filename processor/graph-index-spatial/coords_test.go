package graphindexspatial

// Regression tests for GH #95: SpatialResult carries Lat/Lon/Alt echoed
// from the filter loop's coords value, so downstream GeoJSON consumers
// (CS API GET /areas) don't need an N+1 round-trip per match.

import (
	"encoding/json"
	"testing"
)

// makeCell3D mirrors makeCell but lets callers set Alt, so coord-echo
// tests can assert vertical-component flow-through in the same shape
// the production index stores.
func makeCell3D(entries map[string][3]float64) spatialCellData {
	data := spatialCellData{Entities: make(map[string]struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
		Alt float64 `json:"alt"`
	}, len(entries))}
	for id, lla := range entries {
		data.Entities[id] = struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
			Alt float64 `json:"alt"`
		}{Lon: lla[0], Lat: lla[1], Alt: lla[2]}
	}
	return data
}

func TestFilterEntitiesByPolygon_EchoesCoordsAndAlt(t *testing.T) {
	cell := makeCell3D(map[string][3]float64{
		"ground":  {5, 5, 0}, // lon, lat, alt
		"rooftop": {6, 6, 30.5},
		"drone":   {7, 7, 120.0},
	})

	results := filterEntitiesByPolygon([]spatialCellData{cell}, areaOfInterest, 0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	want := map[string][3]float64{
		"ground":  {5, 5, 0},
		"rooftop": {6, 6, 30.5},
		"drone":   {7, 7, 120.0},
	}
	for _, r := range results {
		w, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected result ID %q", r.ID)
			continue
		}
		if r.Lon != w[0] || r.Lat != w[1] || r.Alt != w[2] {
			t.Errorf("%s: coords lost — got Lon=%v Lat=%v Alt=%v, want %v",
				r.ID, r.Lon, r.Lat, r.Alt, w)
		}
		if r.Type != "entity" {
			t.Errorf("%s: Type=%q, want %q", r.ID, r.Type, "entity")
		}
	}
}

func TestSpatialResult_JSONShape(t *testing.T) {
	// JSON wire shape — alt omitted when zero so ground-level entities
	// don't pollute responses with an explicit "alt":0 field, but
	// non-zero alt always emits.
	tests := []struct {
		name     string
		result   SpatialResult
		wantJSON string
	}{
		{
			name:     "ground-level entity omits alt",
			result:   SpatialResult{ID: "a.b.c.d.e.f", Type: "entity", Lat: 5, Lon: 5, Alt: 0},
			wantJSON: `{"id":"a.b.c.d.e.f","type":"entity","lat":5,"lon":5}`,
		},
		{
			name:     "elevated entity emits alt",
			result:   SpatialResult{ID: "a.b.c.d.e.drone", Type: "entity", Lat: 7, Lon: 7, Alt: 120.5},
			wantJSON: `{"id":"a.b.c.d.e.drone","type":"entity","lat":7,"lon":7,"alt":120.5}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.result)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if string(got) != test.wantJSON {
				t.Errorf("\n got: %s\nwant: %s", got, test.wantJSON)
			}
		})
	}
}

func TestSpatialResult_JSONRoundTrip(t *testing.T) {
	original := SpatialResult{
		ID:   "acme.logistics.sensor.environmental.temperature.sensor-042",
		Type: "entity",
		Lat:  37.7749,
		Lon:  -122.4194,
		Alt:  15.0,
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpatialResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip lost data:\n got: %+v\nwant: %+v", decoded, original)
	}
}

// Sanity: the existing coord-agnostic tests stay green — assert that a
// SpatialResult constructed via filterEntitiesByPolygon over a 2D cell
// still has zero Alt and the expected Lon/Lat.
func TestFilterEntitiesByPolygon_2DCellHasZeroAlt(t *testing.T) {
	cell := makeCell(map[string][2]float64{"point-a": {5, 5}})
	results := filterEntitiesByPolygon([]spatialCellData{cell}, areaOfInterest, 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Alt != 0 {
		t.Errorf("expected Alt=0 from 2D cell, got %v", results[0].Alt)
	}
	if results[0].Lon != 5 || results[0].Lat != 5 {
		t.Errorf("expected Lon=5 Lat=5, got Lon=%v Lat=%v", results[0].Lon, results[0].Lat)
	}
}
