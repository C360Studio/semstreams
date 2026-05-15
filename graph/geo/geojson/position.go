package geojson

import (
	"encoding/json"
	"fmt"
	"math"
)

// Position is a single geographic coordinate per RFC 7946 §3.1.1:
// an ordered tuple of [longitude, latitude] or [longitude,
// latitude, altitude]. WGS84 coordinate reference system is
// mandatory; alternative CRSes were deprecated by the RFC.
//
// Coordinate order is the perpetual GeoJSON footgun. RFC 7946
// specifies longitude first, latitude second — opposite of the
// "lat,lon" pair common in geocoders and legacy GIS data. Use the
// Lon / Lat / Alt accessors at call sites rather than indexing
// the underlying slice to keep the convention explicit.
type Position []float64

// NewPosition constructs a 2D Position. For 3D positions with
// altitude, use NewPositionAlt.
func NewPosition(lon, lat float64) Position {
	return Position{lon, lat}
}

// NewPositionAlt constructs a 3D Position with altitude in meters
// above the WGS84 ellipsoid.
func NewPositionAlt(lon, lat, alt float64) Position {
	return Position{lon, lat, alt}
}

// Lon returns the longitude in decimal degrees. Returns NaN if
// the Position is missing its first coordinate (uninitialized).
func (p Position) Lon() float64 {
	if len(p) < 1 {
		return math.NaN()
	}
	return p[0]
}

// Lat returns the latitude in decimal degrees. Returns NaN if
// the Position is missing its second coordinate.
func (p Position) Lat() float64 {
	if len(p) < 2 {
		return math.NaN()
	}
	return p[1]
}

// Alt returns the altitude in meters above the WGS84 ellipsoid,
// or NaN if the Position is 2D (no altitude). Callers expecting
// 3D should check Has3D before reading.
func (p Position) Alt() float64 {
	if len(p) < 3 {
		return math.NaN()
	}
	return p[2]
}

// Has3D reports whether the Position carries an altitude
// component.
func (p Position) Has3D() bool {
	return len(p) >= 3
}

// Valid reports whether the Position has at least longitude and
// latitude in-range per RFC 7946 (longitude in [-180, 180],
// latitude in [-90, 90]). Use Normalize to coerce out-of-range
// values rather than rejecting them; Valid is the strict gate.
func (p Position) Valid() bool {
	if len(p) < 2 {
		return false
	}
	lon, lat := p[0], p[1]
	if math.IsNaN(lon) || math.IsNaN(lat) {
		return false
	}
	return lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90
}

// Normalize returns a copy of the Position with longitude wrapped
// into [-180, 180] and latitude clamped to [-90, 90]. Latitude
// values past the poles are NOT wrapped (that would produce
// a different point) — they are clamped, because exceeding 90°
// latitude generally indicates a producer error rather than a
// meridian-traversal intent.
func (p Position) Normalize() Position {
	if len(p) < 2 {
		return p
	}
	out := make(Position, len(p))
	copy(out, p)
	out[0] = wrapLongitude(out[0])
	out[1] = math.Max(-90, math.Min(90, out[1]))
	return out
}

// wrapLongitude wraps a longitude value into the canonical
// [-180, 180] range. Values exactly at +180 stay at +180; -180
// stays at -180 (RFC 7946 §5 antimeridian guidance). For inputs
// that wrap exactly onto the antimeridian, the result preserves
// the input's sign: positive multiples of 360 + 180 wrap to +180;
// negative ones wrap to -180. This keeps round-trip-stable
// representation for callers that distinguish "just past the
// east edge" from "just past the west edge".
func wrapLongitude(lon float64) float64 {
	if lon >= -180 && lon <= 180 {
		return lon
	}
	// Reduce to [-180, 180]. The shift-by-180 / modulo-360 /
	// shift-back pattern lands wrapped values at floating-point
	// precision; the antimeridian bias below disambiguates the
	// ±180 boundary based on the input's sign.
	wrapped := math.Mod(lon+180, 360)
	if wrapped < 0 {
		wrapped += 360
	}
	result := wrapped - 180
	// Bias the antimeridian boundary toward the input's sign so
	// that 540° (eastbound) lands at +180, not -180. Without
	// this, the modular reduction always lands at -180.
	if result == -180 && lon > 0 {
		return 180
	}
	return result
}

// NormalizePosition is a free-function form of Position.Normalize
// useful when callers want to compose normalization without first
// materializing a Position value (e.g. when stream-processing
// coordinate triples).
func NormalizePosition(lon, lat float64) (float64, float64) {
	return wrapLongitude(lon), math.Max(-90, math.Min(90, lat))
}

// UnmarshalJSON decodes a Position from a JSON array. Rejects
// arrays shorter than 2 elements (RFC 7946 §3.1.1 mandates at
// least longitude and latitude). Extra elements past index 2 are
// preserved (RFC §3.1.1 leaves their semantics implementation-
// defined; downstream consumers wanting strict 2D/3D should check
// len(p) explicitly).
func (p *Position) UnmarshalJSON(data []byte) error {
	var raw []float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("geojson: position must be a JSON array of numbers: %w", err)
	}
	if len(raw) < 2 {
		return fmt.Errorf("geojson: position must have at least 2 elements (lon, lat), got %d", len(raw))
	}
	*p = raw
	return nil
}
