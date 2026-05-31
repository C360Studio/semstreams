// Package csapi provides Go constants for the OGC API — Connected
// Systems v1.0 (CS API) vocabulary — specifically the §10 Datastream
// concept and its surrounding predicates, which SOSA and OMS do not
// cover.
//
// SOSA models discrete Observations attached to a Sensor; the CS API
// adds the Datastream concept (§10) — a stream of Observations
// produced by one System for one ObservableProperty, with temporal
// bounds and a result-type discriminator. Sister-repo gateways
// (semconnect, future CS API hosts) that publish or list Datastreams
// over `POST /datastreams` / `GET /datastreams` need a shared
// vocabulary primitive so JSON-LD exports resolve and downstream
// graph consumers can discover Datastreams without grepping for
// local IRI strings.
//
// # Namespace pinning
//
// The Namespace constant pins to the OGC spec-rooted IRI stem for
// Connected Systems v1.0. The CS API is still a working draft; when
// the OGC publishes canonical IRIs (which may differ in form), this
// package gets a one-shot constant swap and existing entities re-tag
// via the migration playbook. The constant-name surface is the load-
// bearing API — consumers reference [csapi.Datastream] etc., not
// the string form, so the URI change is invisible at the call site.
//
// # Coverage
//
// MVP coverage is the load-bearing subset for the CS API §10 →
// Datastream representation: the Datastream class plus the four
// predicates needed for the wire shape — ProducedBy, ResultTimeRange,
// PhenomenonTimeRange, ResultType. The Schema field (SWE Common
// DataRecord describing observation result structure) is
// intentionally absent — see [ADR-044] framework-primitives
// reference §Scope-cut for the rationale (Schema flows as a
// StorageRef pointer rather than an inline triple).
//
// # Typed artifact entities (gh#171)
//
// SensorML source documents, SWE Common result schemas, and SWE
// Common command schemas are stored as first-class artifact
// entities — they get their own 6-part EntityID, their own
// singular StorageRef pointing to the document in NATS ObjectStore,
// and are related to parent resources (System, Datastream,
// ControlStream) via vocabulary predicates: HasSource,
// HasResultSchema, HasCommandSchema. This is "Pattern 2" from the
// gh#171 triage: the substrate already supports it, no framework
// primitive change needed, and it cleanly handles the cross-stream
// reuse case (one schema referenced by N Datastreams without
// content duplication). See [docs/concepts/26-typed-artifact-entities.md].
//
// # Standards-at-work, not semweb hell
//
// This package follows the [vocabulary] family pattern. Constants
// are exported strings; no OWL inferencing, no SPARQL, no operator-
// authored RDF. Prefix registration is automatic on import.
//
// # External references
//
//   - Spec (working draft): https://docs.ogc.org/DRAFTS/23-001r0.html
//   - SOSA (compared/parallel): https://www.w3.org/TR/vocab-ssn/
//   - OMS bundle (parallel observation vocabulary): [oms]
//
// [vocabulary]: ..
// [oms]: ../oms
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
package csapi
