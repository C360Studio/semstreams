package graphindexspatial

// ADR-044 Phase 3 polygon-containment fixture test. Exercises
// filterEntitiesByPolygon against hand-built spatialCellData
// fixtures so we can prove the integration without spinning up
// NATS or testcontainers infrastructure. Full wire-level
// validation of the NATS handler is left to the integration
// suite when a polygon-query e2e arrives.

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph/geo/geojson"
)

// makeCell builds a synthetic spatialCellData with the given
// entityID → (lon, lat) entries. The cell's geohash key is not
// used by filterEntitiesByPolygon — it operates on data values,
// not key strings — so callers can ignore cell-key semantics here.
func makeCell(entries map[string][2]float64) spatialCellData {
	data := spatialCellData{Entities: make(map[string]struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
		Alt float64 `json:"alt"`
	}, len(entries))}
	for id, lonLat := range entries {
		data.Entities[id] = struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
			Alt float64 `json:"alt"`
		}{Lat: lonLat[1], Lon: lonLat[0]}
	}
	return data
}

// areaOfInterest is a square polygon covering (0,0)..(10,10).
var areaOfInterest = geojson.Polygon{Coordinates: [][]geojson.Position{
	{
		geojson.NewPosition(0, 0),
		geojson.NewPosition(10, 0),
		geojson.NewPosition(10, 10),
		geojson.NewPosition(0, 10),
		geojson.NewPosition(0, 0),
	},
}}

// areaWithHole is the same square as areaOfInterest with a 4x4
// hole cut from (3,3) to (7,7).
var areaWithHole = geojson.Polygon{Coordinates: [][]geojson.Position{
	{
		geojson.NewPosition(0, 0),
		geojson.NewPosition(10, 0),
		geojson.NewPosition(10, 10),
		geojson.NewPosition(0, 10),
		geojson.NewPosition(0, 0),
	},
	{
		geojson.NewPosition(3, 3),
		geojson.NewPosition(7, 3),
		geojson.NewPosition(7, 7),
		geojson.NewPosition(3, 7),
		geojson.NewPosition(3, 3),
	},
}}

func TestFilterEntitiesByPolygon_BasicContainment(t *testing.T) {
	cell := makeCell(map[string][2]float64{
		"inside-a":  {5, 5},
		"inside-b":  {1, 1},
		"outside-a": {15, 15},
		"outside-b": {-1, -1},
	})

	results := filterEntitiesByPolygon([]spatialCellData{cell}, areaOfInterest, 0)
	got := make(map[string]bool, len(results))
	for _, r := range results {
		got[r.ID] = true
	}

	for _, want := range []string{"inside-a", "inside-b"} {
		if !got[want] {
			t.Errorf("expected %q in results, got %v", want, results)
		}
	}
	for _, exclude := range []string{"outside-a", "outside-b"} {
		if got[exclude] {
			t.Errorf("did not expect %q in results, got %v", exclude, results)
		}
	}
}

func TestFilterEntitiesByPolygon_HonorsHoles(t *testing.T) {
	cell := makeCell(map[string][2]float64{
		"between-rings": {1, 5},  // inside outer ring, outside hole
		"in-the-hole":   {5, 5},  // inside hole — should be excluded
		"way-outside":   {20, 5}, // outside outer ring
	})

	results := filterEntitiesByPolygon([]spatialCellData{cell}, areaWithHole, 0)
	got := make(map[string]bool, len(results))
	for _, r := range results {
		got[r.ID] = true
	}

	if !got["between-rings"] {
		t.Errorf("expected between-rings in results, got %v", results)
	}
	if got["in-the-hole"] {
		t.Errorf("in-the-hole should be excluded (point in hole), got %v", results)
	}
	if got["way-outside"] {
		t.Errorf("way-outside should be excluded, got %v", results)
	}
}

func TestFilterEntitiesByPolygon_RespectsLimit(t *testing.T) {
	cell := makeCell(map[string][2]float64{
		"a": {1, 1},
		"b": {2, 2},
		"c": {3, 3},
		"d": {4, 4},
		"e": {5, 5},
	})

	results := filterEntitiesByPolygon([]spatialCellData{cell}, areaOfInterest, 2)
	if len(results) != 2 {
		t.Errorf("expected len=2 (limit), got %d: %v", len(results), results)
	}

	// limit <= 0 means "all"
	results = filterEntitiesByPolygon([]spatialCellData{cell}, areaOfInterest, 0)
	if len(results) != 5 {
		t.Errorf("expected len=5 (no limit), got %d: %v", len(results), results)
	}
}

func TestFilterEntitiesByPolygon_DedupAcrossCells(t *testing.T) {
	// Same entity appears in two cells (the spatial index permits
	// this at cell boundaries). filterEntitiesByPolygon must
	// return it at most once.
	cellA := makeCell(map[string][2]float64{
		"dup": {5, 5},
	})
	cellB := makeCell(map[string][2]float64{
		"dup":   {5, 5},
		"extra": {6, 6},
	})
	results := filterEntitiesByPolygon([]spatialCellData{cellA, cellB}, areaOfInterest, 0)
	if len(results) != 2 {
		t.Errorf("expected 2 results (dup + extra), got %d: %v", len(results), results)
	}
}

func TestPolygonRequest_RoundTripParse(t *testing.T) {
	// The wire shape sent over NATS — a JSON envelope with the
	// polygon as a nested object. This test confirms the
	// envelope parses and the polygon resolves through
	// geojson.UnmarshalGeometry.
	const raw = `{
        "polygon": {
            "type": "Polygon",
            "coordinates": [[[0,0],[10,0],[10,10],[0,10],[0,0]]]
        },
        "limit": 25
    }`
	var req polygonRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("Unmarshal envelope: %v", err)
	}
	if req.Limit != 25 {
		t.Errorf("limit: got %d, want 25", req.Limit)
	}
	geom, err := geojson.UnmarshalGeometry(req.Polygon)
	if err != nil {
		t.Fatalf("UnmarshalGeometry: %v", err)
	}
	poly, ok := geom.(geojson.Polygon)
	if !ok {
		t.Fatalf("expected Polygon, got %T", geom)
	}
	if len(poly.Coordinates) != 1 {
		t.Errorf("expected 1 ring, got %d", len(poly.Coordinates))
	}
	if len(poly.OuterRing()) != 5 {
		t.Errorf("expected 5 positions in outer ring, got %d", len(poly.OuterRing()))
	}
}
