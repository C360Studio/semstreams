// Package oms provides Go constants for the OGC Observations,
// Measurements, and Samples (OMS) v3.0 vocabulary — the umbrella
// standard (OGC 20-082r4) that the OGC API Connected Systems v1.0
// observation document encoding builds on.
//
// OMS v3.0 supersedes the historical O&M 2.0 model and ships
// alongside CS API v1.0 with a JSON encoding. SemStreams uses OMS
// IRIs to type the Observation entities emitted by sensor
// pipelines and to wire the BaseMessage ↔ OMS document mapper
// scheduled for Phase 6 (message/oms).
//
// # Namespace pinning
//
// The Namespace constant pins to "http://www.opengis.net/oms/3.0/"
// — matching the package name for readability of compact output
// ("oms:Observation"). If a future OMS revision changes the
// namespace stem, this package gets a major-version change rather
// than a silent rebinding; callers can detect the move via the
// contract test that compares every constant against the declared
// Namespace.
//
// # Coverage
//
// MVP coverage is the load-bearing subset required by Phase 6's
// observation document mapper: Observation, ObservableProperty,
// Procedure, FeatureOfInterest, Result and the time-stamp
// predicates ResultTime / PhenomenonTime. Less-common OMS terms
// (Process, Specimen, samplingDistance, …) are added when a
// concrete capability surfaces a need.
//
// # Standards-at-work, not semweb hell
//
// This package follows the [vocabulary] family pattern. Constants
// are exported strings; no OWL inferencing, no SPARQL, no
// operator-authored RDF. Prefix registration is automatic on
// import.
//
// See [ADR-044] for the framework/sister-repo split rationale.
//
// # External references
//
//   - Spec: https://www.ogc.org/standards/om/ (OGC 20-082r4)
//   - JSON encoding bundled with CS API v1.0:
//     https://docs.ogc.org/DRAFTS/23-002r0.html
//
// [vocabulary]: ..
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
package oms
