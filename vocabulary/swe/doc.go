// Package swe provides Go constants for the OGC SWE Common Data
// Model v2.1 vocabulary — the typed-data-primitive layer used by
// SOSA observations to declare what kind of value a Result carries
// (Quantity, Category, Time, Count, Boolean, Text, …) and the
// structural roles (label, definition, uom, value, nilValue) that
// describe each typed slot.
//
// SWE Common is part of the bundle published alongside the OGC
// API Connected Systems v1.0 standard. This package's coverage is
// the load-bearing subset that downstream Phase 5 (parser/sensorml)
// and Phase 6 (message/oms) work needs to wire observation results.
// Less-common SWE types (DataChoice, Matrix, encoded streams) are
// added when a concrete capability surfaces a need.
//
// # Namespace pinning
//
// The Namespace constant pins to "http://www.opengis.net/swe/2.0/"
// — the namespace that the CS API v1.0 bundle uses. The trailing
// slash is a SemStreams convention required for CURIE local-name
// concatenation; the canonical OGC XSD targetNamespace is the
// slashless form "http://www.opengis.net/swe/2.0". Both forms
// resolve to the same vocabulary; only the slash form composes
// with local names ("Quantity", "uom", …) for IRI compaction.
//
// SWE Common 3.0 (OGC 24-014) exists and uses
// "http://www.opengis.net/spec/SWE/3.0" as its normative base —
// not this stem. The CS API v1.0 bundle is v2.0, so this package
// pins to v2.0 for now; a future Phase will introduce a
// vocabulary/swe3 sibling rather than rebinding this stem.
// Callers can detect any stem change via the contract test that
// compares every constant against the declared Namespace.
//
// # Standards-at-work, not semweb hell
//
// This package follows the [vocabulary] family pattern. Constants
// are exported strings; no OWL inferencing, no SPARQL, no
// operator-authored RDF. Prefix registration is automatic on
// import — the package's init() registers swe: with
// [github.com/c360studio/semstreams/vocabulary/export].
//
// See [ADR-044] for the framework/sister-repo split rationale and
// the dependency chain that places this package in Phase 2.
//
// # External references
//
//   - Spec: https://www.ogc.org/standards/swecommon/
//   - JSON encoding bundled with CS API v1.0:
//     https://docs.ogc.org/DRAFTS/23-001r0.html
//
// [vocabulary]: ..
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
package swe
