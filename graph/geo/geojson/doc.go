// Package geojson provides Go types and JSON I/O for RFC 7946
// GeoJSON — the canonical geometry exchange format for OGC API
// Connected Systems v1.0 and every other modern geospatial API
// SemStreams routinely interoperates with.
//
// # What it ships
//
// Every RFC 7946 geometry kind as a Go struct, the shared
// [Geometry] interface, the [Position] coordinate primitive, the
// [Feature] / [FeatureCollection] containers, and a polymorphic
// JSON unmarshaler (see [UnmarshalGeometry]) that resolves the
// type-discriminator field RFC 7946 mandates. WGS84 normalization
// is exposed via [NormalizePosition] and [Geometry] receivers.
//
// # What it does NOT ship
//
//   - A general-purpose GIS / spatial algebra library. The only
//     spatial predicate this package implements is point-in-polygon
//     (see [Polygon.Contains]), because that is the predicate the
//     Phase 3 `graph-index-spatial` extension needs and the predicate
//     every CS-API-aligned consumer asks for first. More complex
//     operations (buffer, union, intersection) belong in a downstream
//     package or vendor library if and when a concrete need surfaces.
//   - CRS support beyond WGS84. RFC 7946 deprecated alternative
//     coordinate reference systems; SemStreams enforces that.
//   - XML / GML I/O. Modern OGC APIs are JSON-first; if a downstream
//     consumer needs GML they wrap this package, not the reverse.
//
// # Coordinate order
//
// RFC 7946 specifies [longitude, latitude] order — NOT [latitude,
// longitude] as some legacy formats use. The [Position] type
// enforces this with [Position.Lon] / [Position.Lat] / [Position.Alt]
// accessors and a [NewPosition] constructor that takes lon, lat, alt
// in that order. Reading code that misorders coordinates is a
// frequent source of geospatial bugs; the accessor names make the
// intent explicit at every call site.
//
// # Standards-at-work, not GIS hell
//
// This package follows the same discipline as the vocabulary
// sub-packages: adopt the spec as first-class Go types, keep
// internal access patterns plain Go, contain the schema dependency
// behind a sub-package boundary. The wider semstreams module does
// not depend on geojson except where geometry exchange or
// containment queries are needed.
//
// See [ADR-044] for the framework/sister-repo split rationale and
// the dependency chain that places this package in Phase 3.
//
// # External references
//
//   - Spec: https://datatracker.ietf.org/doc/html/rfc7946
//   - OGC CS API geometry binding:
//     https://docs.ogc.org/DRAFTS/23-001r0.html
//
// [ADR-044]: ../../../docs/adr/044-ogc-connected-systems-framework-split.md
package geojson
