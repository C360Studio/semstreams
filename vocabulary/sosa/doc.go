// Package sosa provides Go constants for the W3C SOSA/SSN vocabulary
// (Sensor, Observation, Sample, and Actuator ontology + Semantic
// Sensor Network ontology), the semantic-sensor backbone of the OGC
// API Connected Systems standard.
//
// SOSA is the lightweight core; SSN is the systems/deployment
// overlay published in the same W3C 2017 TR. SemStreams co-locates
// both in this package — the namespaces are distinct (sosa: and
// ssn:) but the upstream is one document and the consumers always
// import them together. SOSA constants are bare ([Sensor],
// [Observes]); the (small) SSN companion set uses an SSN prefix
// ([SSNSystem], [SSNHasDeployment]) so the namespace switch is
// explicit at call sites — per the ADR-044 guidance to default to
// SOSA and surface SSN deviations.
//
// Convention at a call site:
//
//	triples := []message.Triple{
//	    // SOSA core — observation provenance:
//	    {Subject: platform, Predicate: sosa.Hosts, Object: sensor},
//	    {Subject: sensor, Predicate: sosa.MadeObservation, Object: obs},
//	    // SSN overlay — system / deployment topology:
//	    {Subject: platform, Predicate: sosa.SSNHasDeployment, Object: deployment},
//	}
//
// # Standards-at-work, not semweb hell
//
// This package follows the [vocabulary] family pattern (agentic,
// bfo, cco, oasf, plus the standards.go IRI roster):
//
//   - Adopts an external vocabulary as first-class Go constants
//   - Keeps internal access patterns plain JSON / Go — no OWL
//     inferencing, no SPARQL, no operator-authored RDF
//   - Sub-package boundary contains the schema dependency; the
//     wider semstreams module does not import sosa except where
//     the IRIs themselves are needed in [Graphable.Triples] output
//
// See [ADR-044] for the framework/sister-repo split rationale and
// the dependency chain that places this package in Phase 2.
//
// # Coverage
//
// MVP coverage is the load-bearing subset required by Phase 5
// (parser/sensorml), Phase 6 (message/oms), and the sister-repo
// CS-API gateway: twelve SOSA classes, twelve SOSA predicates,
// two SSN classes, five SSN predicates. Additional constants are
// added as concrete capabilities surface a need; the [IsKnown]
// and [IRIs] helpers report the current coverage.
//
// Prefix registration is automatic — importing this package runs
// an init() that registers sosa: and ssn: with
// [github.com/c360studio/semstreams/vocabulary/export]. Callers
// emitting RDF/Turtle or JSON-LD via that package therefore get
// the compact prefixes without further wiring.
//
// # External references
//
//   - Spec: https://www.w3.org/TR/vocab-ssn/
//   - OGC CS API binding: https://www.ogc.org/standards/ogc-api-connected-systems/
//
// [vocabulary]: ..
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
// [Graphable.Triples]: ../../message
package sosa
