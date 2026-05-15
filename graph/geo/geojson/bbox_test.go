package geojson

import "testing"

func TestComputeBBox_Point(t *testing.T) {
	p := Point{Coordinates: NewPosition(-122.5, 37.7)}
	bbox := ComputeBBox(p)
	if len(bbox) != 4 {
		t.Fatalf("want 4-element bbox, got %d", len(bbox))
	}
	if bbox.West() != -122.5 || bbox.East() != -122.5 {
		t.Errorf("point bbox lon: got [%v, %v], want [-122.5, -122.5]", bbox.West(), bbox.East())
	}
	if bbox.South() != 37.7 || bbox.North() != 37.7 {
		t.Errorf("point bbox lat: got [%v, %v], want [37.7, 37.7]", bbox.South(), bbox.North())
	}
}

func TestComputeBBox_Polygon(t *testing.T) {
	p := Polygon{Coordinates: [][]Position{
		{NewPosition(0, 0), NewPosition(10, 0), NewPosition(10, 5), NewPosition(0, 5), NewPosition(0, 0)},
	}}
	bbox := ComputeBBox(p)
	if bbox.West() != 0 || bbox.East() != 10 {
		t.Errorf("polygon bbox lon: got [%v, %v], want [0, 10]", bbox.West(), bbox.East())
	}
	if bbox.South() != 0 || bbox.North() != 5 {
		t.Errorf("polygon bbox lat: got [%v, %v], want [0, 5]", bbox.South(), bbox.North())
	}
}

func TestComputeBBox_FeatureCollection(t *testing.T) {
	fc := FeatureCollection{Features: []Feature{
		{Geometry: Point{Coordinates: NewPosition(-1, -1)}},
		{Geometry: Point{Coordinates: NewPosition(5, 5)}},
		{Geometry: nil}, // null geometry contributes no points
	}}
	bbox := ComputeBBox(fc)
	if bbox.West() != -1 || bbox.South() != -1 || bbox.East() != 5 || bbox.North() != 5 {
		t.Errorf("FC bbox: got [w=%v s=%v e=%v n=%v], want [-1,-1,5,5]",
			bbox.West(), bbox.South(), bbox.East(), bbox.North())
	}
}

func TestComputeBBox_EmptyGeometry(t *testing.T) {
	mp := MultiPoint{Coordinates: nil}
	bbox := ComputeBBox(mp)
	if bbox != nil {
		t.Errorf("empty MultiPoint bbox should be nil, got %v", bbox)
	}
}
