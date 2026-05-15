// Package sensorml provides a Go parser, emitter, and Graphable
// adapter for the OGC SensorML JSON encoding bundled with the OGC
// API Connected Systems v1.0 standard.
//
// SensorML (OGC 12-000r2) is the canonical XML/JSON schema for
// describing observing systems, components, processes, and the
// hardware/software platforms hosting them. The JSON encoding
// shipped alongside CS API v1.0 is what every Connected Systems
// server, client, or processor exchanges to declare what a system
// IS — its identity, capabilities, mode structure, and the
// procedures it executes.
//
// This package is the framework-side adapter that bridges SensorML
// descriptions and SemStreams' Graphable / KV / triple model. It
// is one of the load-bearing pieces of ADR-044 Phase 5.
//
// # Scope
//
// Pinned to the OGC CS API v1.0 SensorML JSON bundle. Coverage is
// the load-bearing subset every CS API consumer needs:
//
//   - [PhysicalSystem]      — composite physical assembly with
//     children and connections (the canonical "system" record).
//   - [PhysicalComponent]   — leaf physical observing unit.
//   - [SimpleProcess]       — leaf algorithmic / procedural unit.
//   - [AggregateProcess]    — composite of child processes.
//   - [AbstractProcess]     — shared fields embedded by all four.
//
// SensorML has roughly 500 schema classes; the four above cover
// the path from "CS API GET /systems/{id}" → Graphable triples →
// downstream graph processors. Less common types (Mode/ModeChoice,
// Algorithm, Configuration, DeployedSystem, Position, Time) are
// intentionally deferred — they extend the schema's coverage but
// are not on the CS API v1.0 critical path. Add them in follow-up
// tags when concrete consumers surface a need.
//
// # Graphable bridge
//
// Parsed SensorML values do NOT directly implement
// [graph.Graphable] because SensorML's local id field is not a
// 6-part SemStreams entity ID. The pairing happens via [Asset]:
//
//	process, err := sensorml.UnmarshalProcess(data)
//	asset := sensorml.NewAsset("acme.ops.robotics.gcs.drone.001", process)
//	// asset.EntityID() → "acme.ops.robotics.gcs.drone.001"
//	// asset.Triples()  → SOSA/SSN-aligned triples
//
// Each [Asset] emits triples using dotted predicate names
// registered by this package against the corresponding
// SOSA/SSN IRIs (see predicates.go). Importing this package runs
// the registration at init time, so RDF/Turtle export through
// [vocabulary/export] produces compacted sosa:/ssn: forms.
//
// # Standards-at-work, not OGC hell
//
// This package adopts the SensorML JSON encoding as first-class Go
// structs. No code-generation from upstream schemas: the surface
// area is small enough to hand-write, and the cost of a
// build-time codegen dependency is not paid by current consumers.
// Auto-generation may make sense once coverage broadens past
// Phase 5; defer until that pain materializes.
//
// # External references
//
//   - OGC 12-000r2 (SensorML): https://www.ogc.org/standard/sensorml/
//   - CS API v1.0 SensorML JSON encoding:
//     https://docs.ogc.org/DRAFTS/23-001r0.html
//
// See [ADR-044] for the framework/sister-repo split rationale and
// the dependency chain that places this package in Phase 5.
//
// [graph.Graphable]: ../../graph
// [vocabulary/export]: ../../vocabulary/export
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
package sensorml
