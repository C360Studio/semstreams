package geojson

// Geometry is the common contract implemented by every RFC 7946
// geometry kind (Point, MultiPoint, LineString, MultiLineString,
// Polygon, MultiPolygon, GeometryCollection). The interface is
// deliberately narrow — Type() is enough to drive a type switch,
// and Normalize() returns a copy with all positions WGS84-
// normalized. Geometry-specific behavior lives on the concrete
// types.
type Geometry interface {
	// Type returns the RFC 7946 type-discriminator string for
	// this geometry ("Point", "LineString", "Polygon", …).
	Type() string

	// Normalize returns a copy of the geometry with every
	// constituent Position run through Position.Normalize.
	// Useful for ingesting third-party data with unwrapped
	// longitudes or out-of-range latitudes.
	Normalize() Geometry
}

// Geometry type-discriminator strings per RFC 7946 §1.4.
const (
	TypePoint              = "Point"
	TypeMultiPoint         = "MultiPoint"
	TypeLineString         = "LineString"
	TypeMultiLineString    = "MultiLineString"
	TypePolygon            = "Polygon"
	TypeMultiPolygon       = "MultiPolygon"
	TypeGeometryCollection = "GeometryCollection"
	TypeFeature            = "Feature"
	TypeFeatureCollection  = "FeatureCollection"
)

// Point is an RFC 7946 §3.1.2 single-position geometry.
type Point struct {
	Coordinates Position `json:"coordinates"`
}

// Type implements Geometry.
func (Point) Type() string { return TypePoint }

// Normalize implements Geometry.
func (p Point) Normalize() Geometry {
	return Point{Coordinates: p.Coordinates.Normalize()}
}

// MultiPoint is an RFC 7946 §3.1.3 geometry — an array of
// positions, each a separate point.
type MultiPoint struct {
	Coordinates []Position `json:"coordinates"`
}

// Type implements Geometry.
func (MultiPoint) Type() string { return TypeMultiPoint }

// Normalize implements Geometry.
func (m MultiPoint) Normalize() Geometry {
	out := MultiPoint{Coordinates: make([]Position, len(m.Coordinates))}
	for i, p := range m.Coordinates {
		out.Coordinates[i] = p.Normalize()
	}
	return out
}

// LineString is an RFC 7946 §3.1.4 geometry — an ordered array
// of 2+ positions connected by line segments.
type LineString struct {
	Coordinates []Position `json:"coordinates"`
}

// Type implements Geometry.
func (LineString) Type() string { return TypeLineString }

// Normalize implements Geometry.
func (l LineString) Normalize() Geometry {
	out := LineString{Coordinates: make([]Position, len(l.Coordinates))}
	for i, p := range l.Coordinates {
		out.Coordinates[i] = p.Normalize()
	}
	return out
}

// MultiLineString is an RFC 7946 §3.1.5 geometry — an array of
// LineString coordinate arrays.
type MultiLineString struct {
	Coordinates [][]Position `json:"coordinates"`
}

// Type implements Geometry.
func (MultiLineString) Type() string { return TypeMultiLineString }

// Normalize implements Geometry.
func (m MultiLineString) Normalize() Geometry {
	out := MultiLineString{Coordinates: make([][]Position, len(m.Coordinates))}
	for i, line := range m.Coordinates {
		out.Coordinates[i] = make([]Position, len(line))
		for j, p := range line {
			out.Coordinates[i][j] = p.Normalize()
		}
	}
	return out
}

// Polygon is an RFC 7946 §3.1.6 geometry — an array of linear
// rings. Per the RFC: the first ring is the outer boundary;
// subsequent rings (if any) are holes. Each ring's first and
// last positions are identical (closed ring). Right-hand rule:
// outer rings are counterclockwise, holes are clockwise — but
// the RFC notes this orientation MAY be relaxed by producers.
type Polygon struct {
	Coordinates [][]Position `json:"coordinates"`
}

// Type implements Geometry.
func (Polygon) Type() string { return TypePolygon }

// Normalize implements Geometry.
func (p Polygon) Normalize() Geometry {
	out := Polygon{Coordinates: make([][]Position, len(p.Coordinates))}
	for i, ring := range p.Coordinates {
		out.Coordinates[i] = make([]Position, len(ring))
		for j, pos := range ring {
			out.Coordinates[i][j] = pos.Normalize()
		}
	}
	return out
}

// OuterRing returns the outer boundary ring of the polygon, or
// nil if the polygon has no rings. Callers iterating rings
// directly should prefer Coordinates[0] for the outer and
// Coordinates[1:] for holes; this accessor is for the common
// case where only the outer boundary matters.
func (p Polygon) OuterRing() []Position {
	if len(p.Coordinates) == 0 {
		return nil
	}
	return p.Coordinates[0]
}

// Holes returns the inner rings (holes) of the polygon, or
// nil if the polygon has no holes.
func (p Polygon) Holes() [][]Position {
	if len(p.Coordinates) < 2 {
		return nil
	}
	return p.Coordinates[1:]
}

// MultiPolygon is an RFC 7946 §3.1.7 geometry — an array of
// Polygon coordinate arrays.
type MultiPolygon struct {
	Coordinates [][][]Position `json:"coordinates"`
}

// Type implements Geometry.
func (MultiPolygon) Type() string { return TypeMultiPolygon }

// Normalize implements Geometry.
func (m MultiPolygon) Normalize() Geometry {
	out := MultiPolygon{Coordinates: make([][][]Position, len(m.Coordinates))}
	for i, poly := range m.Coordinates {
		out.Coordinates[i] = make([][]Position, len(poly))
		for j, ring := range poly {
			out.Coordinates[i][j] = make([]Position, len(ring))
			for k, pos := range ring {
				out.Coordinates[i][j][k] = pos.Normalize()
			}
		}
	}
	return out
}

// GeometryCollection is an RFC 7946 §3.1.8 heterogeneous
// container of Geometry values. RFC §3.1.8 recommends avoiding
// nested GeometryCollections; this package does not reject
// nesting but does not flatten it either.
type GeometryCollection struct {
	Geometries []Geometry `json:"geometries"`
}

// Type implements Geometry.
func (GeometryCollection) Type() string { return TypeGeometryCollection }

// Normalize implements Geometry.
func (g GeometryCollection) Normalize() Geometry {
	out := GeometryCollection{Geometries: make([]Geometry, len(g.Geometries))}
	for i, geom := range g.Geometries {
		out.Geometries[i] = geom.Normalize()
	}
	return out
}
