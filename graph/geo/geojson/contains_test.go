package geojson

import "testing"

// unitSquare is a 1x1 polygon with corners (0,0), (1,0), (1,1), (0,1).
var unitSquare = Polygon{Coordinates: [][]Position{
	{
		NewPosition(0, 0),
		NewPosition(1, 0),
		NewPosition(1, 1),
		NewPosition(0, 1),
		NewPosition(0, 0),
	},
}}

// donutPolygon is a 10x10 outer square with a 4x4 inner hole
// centered at (5, 5).
var donutPolygon = Polygon{Coordinates: [][]Position{
	{
		NewPosition(0, 0),
		NewPosition(10, 0),
		NewPosition(10, 10),
		NewPosition(0, 10),
		NewPosition(0, 0),
	},
	{
		NewPosition(3, 3),
		NewPosition(7, 3),
		NewPosition(7, 7),
		NewPosition(3, 7),
		NewPosition(3, 3),
	},
}}

func TestPolygon_Contains_UnitSquare(t *testing.T) {
	cases := []struct {
		name  string
		point Position
		want  bool
	}{
		{"center", NewPosition(0.5, 0.5), true},
		{"south-west corner", NewPosition(0, 0), true},
		{"on edge", NewPosition(0.5, 0), true},
		{"just outside south", NewPosition(0.5, -0.001), false},
		{"just outside east", NewPosition(1.001, 0.5), false},
		{"far away", NewPosition(100, 100), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := unitSquare.Contains(c.point)
			if got != c.want {
				t.Errorf("Contains(%v) = %v, want %v", c.point, got, c.want)
			}
		})
	}
}

func TestPolygon_Contains_DonutHole(t *testing.T) {
	cases := []struct {
		name  string
		point Position
		want  bool
	}{
		{"inside outer ring, outside hole", NewPosition(1, 1), true},
		{"inside outer ring, outside hole - east", NewPosition(8, 5), true},
		{"in the hole", NewPosition(5, 5), false},
		{"in hole boundary - left", NewPosition(3, 5), false},
		{"on outer boundary", NewPosition(0, 5), true},
		{"outside outer ring", NewPosition(-1, 5), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := donutPolygon.Contains(c.point)
			if got != c.want {
				t.Errorf("Contains(%v) = %v, want %v", c.point, got, c.want)
			}
		})
	}
}

func TestPolygon_Contains_DegenerateInputs(t *testing.T) {
	empty := Polygon{}
	if empty.Contains(NewPosition(0, 0)) {
		t.Error("empty polygon should contain nothing")
	}

	// Polygon with only 2 positions — not a valid ring.
	tooShort := Polygon{Coordinates: [][]Position{
		{NewPosition(0, 0), NewPosition(1, 1)},
	}}
	if tooShort.Contains(NewPosition(0.5, 0.5)) {
		t.Error("polygon with 2-position ring should contain nothing")
	}

	// Out-of-range query point.
	if unitSquare.Contains(NewPosition(200, 0)) {
		t.Error("out-of-range point should not be reported as inside")
	}
}

// TestPolygon_Contains_RingWithDegeneratePositions confirms that
// a programmatically-constructed Polygon whose ring contains
// degenerate Positions (fewer than 2 elements) does not panic.
// JSON decode rejects this shape, but in-code construction by
// Phase 5+ parsers must not be able to crash the spatial query
// handler.
func TestPolygon_Contains_RingWithDegeneratePositions(t *testing.T) {
	degenerate := Polygon{Coordinates: [][]Position{
		{
			nil,           // empty Position
			Position{1.0}, // single-element Position
			NewPosition(0, 0),
			NewPosition(1, 0),
			NewPosition(1, 1),
			NewPosition(0, 1),
			NewPosition(0, 0),
		},
	}}
	// Must not panic. Behavior is "skip the degenerate edges";
	// well-formed edges still drive the ray-cast, so a
	// known-inside point reports inside.
	if !degenerate.Contains(NewPosition(0.5, 0.5)) {
		t.Error("expected (0.5, 0.5) inside the well-formed portion of the ring")
	}
}

// TestPolygon_Contains_NonConvex tests a notched concave polygon
// where a naive "inside if east of leftmost edge" heuristic would
// fail. The polygon looks like:
//
//	(0,4)─────────(4,4)
//	  │             │
//	  │  (1,2)─(2,2)│   ← concave notch from south
//	  │     │   │   │
//	  │  (1,0)─(2,0)│
//	(0,0)─────────(4,0)
func TestPolygon_Contains_NonConvex(t *testing.T) {
	concave := Polygon{Coordinates: [][]Position{
		{
			NewPosition(0, 0),
			NewPosition(1, 0),
			NewPosition(1, 2),
			NewPosition(2, 2),
			NewPosition(2, 0),
			NewPosition(4, 0),
			NewPosition(4, 4),
			NewPosition(0, 4),
			NewPosition(0, 0),
		},
	}}
	cases := []struct {
		name  string
		point Position
		want  bool
	}{
		{"top center", NewPosition(2, 3), true},
		{"top right", NewPosition(3, 3), true},
		{"left arm", NewPosition(0.5, 1), true},
		{"right arm", NewPosition(3, 1), true},
		{"in the notch (outside)", NewPosition(1.5, 1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := concave.Contains(c.point)
			if got != c.want {
				t.Errorf("Contains(%v) = %v, want %v", c.point, got, c.want)
			}
		})
	}
}
