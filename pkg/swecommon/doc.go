// Package swecommon provides schema-bound encoders and decoders
// for the OGC SWE Common data model, covering the JSON, text, and
// binary encodings the OGC Connected Systems API negotiates for
// observation results and command payloads.
//
// # Why this package exists
//
// CS API gateways (semconnect and friends) need to advertise a
// per-datastream result schema and stream observation values
// against that schema in three media types:
//
//   - application/swe+json
//   - application/swe+csv
//   - application/swe+binary
//
// Without a schema-bound model, gateways fall back to projecting
// value-only views (time + result), tagging the response with a
// non-conformant subset header. This package lets gateways drop
// the subset header and claim the SWE Common conformance classes.
//
// # Model
//
// A schema is a [DataRecord] of named [Field]s. Each field carries
// a typed [DataComponent] — Quantity, Count, Time, Boolean, Text,
// Category. The scalar types are sealed; adding a new kind
// requires extending this package (and is a compile-time error in
// every consumer until they handle it).
//
// Values are carried as [Values] maps keyed by field name. The
// encoders are tolerant on input — a Quantity field accepts any
// Go numeric type, a Time accepts time.Time or RFC 3339 string.
// Decoders always populate the canonical Go type per the schema.
//
// # Encoding choices
//
//   - JSON values: array of objects, one per row, field names as keys.
//   - Text values: producer-configurable token + block separators
//     (default CSV). Decimal separator must be ".". Optional header.
//   - Binary values: per-record nil bitmap (ceil(nfields/8) bytes,
//     high bit = field 0) + packed primitives. Big-endian by
//     default. No record-count prefix; consumers read until EOF.
//
// # Round-trip stability
//
// Every encoding has a corresponding decoder in the same file and
// every type/kind combination is exercised by the round-trip
// table-driven tests in *_test.go. Producers that depend on a
// shape that does not appear in the test table should add a row.
//
// # Phase 1 scope (this package as introduced by [ADR-050])
//
// Implemented: scalar components + DataRecord + JSON/text/binary
// encoders + decoders + schema marshal/unmarshal + media-type
// constants. Deferred (tracked in
// https://github.com/C360Studio/semstreams/issues/167): DataArray
// (homogeneous element record), DataChoice (discriminated union),
// Vector (axis-frame-bound values), per-component constraints
// (allowedValues, ranges), multi-reason NilValues block, nested
// DataRecord values, SWE XML encoding (CS API does not require it).
package swecommon
