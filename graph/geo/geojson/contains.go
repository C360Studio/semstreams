package geojson

// Contains reports whether the polygon contains the given point.
// Containment is determined by ray-casting against the outer
// ring, then ruling out any holes (inner rings) using the same
// algorithm. A point on the boundary of an outer ring is treated
// as inside; a point on the boundary of a hole is treated as
// outside (the hole's edge belongs to the hole, removing it from
// the polygon's interior).
//
// Coordinates are taken at face value — no normalization or
// CRS conversion is performed. Callers feeding data from
// untrusted sources should call Polygon.Normalize first.
//
// Limitations:
//
//   - The algorithm assumes planar 2D geometry. For polygons
//     spanning the antimeridian or covering large fractions of
//     the globe, callers must split the polygon at the
//     antimeridian or convert to a sinusoidal projection first.
//   - Self-intersecting rings produce well-defined but
//     RFC-7946-undefined results; the standard discourages such
//     polygons (§3.1.6 "MUST follow the right-hand rule with
//     respect to the area").
func (p Polygon) Contains(point Position) bool {
	if len(p.Coordinates) == 0 {
		return false
	}
	if !point.Valid() {
		return false
	}
	if !rayCastInRing(p.Coordinates[0], point) {
		return false
	}
	// Inside the outer ring — now exclude any holes.
	for _, hole := range p.Coordinates[1:] {
		if rayCastInRing(hole, point) {
			return false
		}
	}
	return true
}

// rayCastInRing implements the classic Jordan-curve / Sunday
// crossing-number test. Returns true if the point is strictly
// inside the ring OR on its boundary. The ring is treated as
// closed (first and last positions equivalent per RFC 7946
// §3.1.6).
//
// The algorithm casts a ray east from the point and counts how
// many ring edges it crosses; odd count → inside.
func rayCastInRing(ring []Position, point Position) bool {
	if len(ring) < 3 {
		return false
	}
	px, py := point[0], point[1]
	inside := false
	n := len(ring)

	// Iterate edges (ring[i], ring[i+1]). The last edge closes
	// the ring (ring[n-1], ring[0]).
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		// Guard degenerate positions. JSON decode rejects
		// positions shorter than 2 elements, but in-code
		// construction (e.g. Phase 5 SensorML parser, sister-
		// repo CS-API server) can build polygons
		// programmatically — skip degenerate edges rather
		// than panicking.
		if len(ring[i]) < 2 || len(ring[j]) < 2 {
			continue
		}
		ax, ay := ring[i][0], ring[i][1]
		bx, by := ring[j][0], ring[j][1]

		// Boundary check: if point lies on the segment, return
		// true. Tested before the crossing logic so on-edge
		// points don't get misclassified by it.
		if onSegment(ax, ay, bx, by, px, py) {
			return true
		}

		// Standard ray-cast: does the eastbound ray from
		// (px, py) cross edge (a, b)? Crossing happens when one
		// endpoint is above py and the other is at-or-below,
		// AND the x-coordinate of the intersection is east of
		// px. Using strict above / non-strict below for one
		// endpoint avoids double-counting vertices that lie
		// exactly on the ray.
		if (ay > py) != (by > py) {
			xIntersect := ax + (py-ay)/(by-ay)*(bx-ax)
			if px < xIntersect {
				inside = !inside
			}
		}
	}
	return inside
}

// onSegment reports whether point (px, py) lies on the closed
// segment from (ax, ay) to (bx, by) within floating-point
// tolerance. The check is collinearity + bounding-box
// containment.
func onSegment(ax, ay, bx, by, px, py float64) bool {
	const epsilon = 1e-12
	// Cross product — non-zero means the three points are not
	// collinear, so the query point is off the line entirely.
	cross := (px-ax)*(by-ay) - (py-ay)*(bx-ax)
	if cross > epsilon || cross < -epsilon {
		return false
	}
	// Collinear — bounding-box test confirms the point is on
	// the segment rather than on the extended infinite line.
	if px < min2(ax, bx)-epsilon || px > max2(ax, bx)+epsilon {
		return false
	}
	if py < min2(ay, by)-epsilon || py > max2(ay, by)+epsilon {
		return false
	}
	return true
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
