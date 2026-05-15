package geojson

import "math"

// BBox is an RFC 7946 §5 bounding box: a tuple ordered as
// [west, south, east, north] for 2D or
// [west, south, minAlt, east, north, maxAlt] for 3D.
//
// The bounding box is informative metadata — RFC §5.2 specifies
// that consumers MUST recompute it if they need it for spatial
// reasoning. ComputeBBox provides the canonical computation.
type BBox []float64

// West returns the western longitude bound. Returns NaN if the
// BBox is degenerate (fewer than 4 elements).
func (b BBox) West() float64 {
	if len(b) < 4 {
		return math.NaN()
	}
	return b[0]
}

// South returns the southern latitude bound.
func (b BBox) South() float64 {
	if len(b) < 4 {
		return math.NaN()
	}
	return b[1]
}

// East returns the eastern longitude bound.
func (b BBox) East() float64 {
	if len(b) == 4 {
		return b[2]
	}
	if len(b) == 6 {
		return b[3]
	}
	return math.NaN()
}

// North returns the northern latitude bound.
func (b BBox) North() float64 {
	if len(b) == 4 {
		return b[3]
	}
	if len(b) == 6 {
		return b[4]
	}
	return math.NaN()
}

// Has3D reports whether the bbox carries altitude bounds.
func (b BBox) Has3D() bool {
	return len(b) == 6
}

// ComputeBBox returns the 2D bounding box enclosing the given
// geometry. Altitude bounds are ignored even for 3D geometries —
// the spatial-index integration is 2D-only. RFC 7946 §5.2 permits
// callers to recompute bboxes when needed.
//
// For an empty / zero-position geometry the returned BBox has
// length 0.
func ComputeBBox(g Geometry) BBox {
	west := math.Inf(1)
	south := math.Inf(1)
	east := math.Inf(-1)
	north := math.Inf(-1)
	seen := false

	visit := func(p Position) {
		if len(p) < 2 {
			return
		}
		seen = true
		lon, lat := p[0], p[1]
		if lon < west {
			west = lon
		}
		if lon > east {
			east = lon
		}
		if lat < south {
			south = lat
		}
		if lat > north {
			north = lat
		}
	}

	walkPositions(g, visit)

	if !seen {
		return nil
	}
	return BBox{west, south, east, north}
}

// walkPositions invokes visit on every Position in the geometry,
// flattening nested rings / collections.
func walkPositions(g Geometry, visit func(Position)) {
	switch v := g.(type) {
	case Point:
		visit(v.Coordinates)
	case MultiPoint:
		for _, p := range v.Coordinates {
			visit(p)
		}
	case LineString:
		for _, p := range v.Coordinates {
			visit(p)
		}
	case MultiLineString:
		for _, line := range v.Coordinates {
			for _, p := range line {
				visit(p)
			}
		}
	case Polygon:
		for _, ring := range v.Coordinates {
			for _, p := range ring {
				visit(p)
			}
		}
	case MultiPolygon:
		for _, poly := range v.Coordinates {
			for _, ring := range poly {
				for _, p := range ring {
					visit(p)
				}
			}
		}
	case GeometryCollection:
		for _, child := range v.Geometries {
			walkPositions(child, visit)
		}
	case Feature:
		if v.Geometry != nil {
			walkPositions(v.Geometry, visit)
		}
	case FeatureCollection:
		for _, f := range v.Features {
			if f.Geometry != nil {
				walkPositions(f.Geometry, visit)
			}
		}
	}
}
