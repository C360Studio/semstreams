# ADR-075: Framework Package Admission and Explicit Capability Composition

## Status

**Accepted — 2026-07-15.** This is a pre-v1 clean break. It supersedes the affected ownership and registration
decisions in ADR-025, ADR-042, ADR-044, and ADR-050. It reaffirms ADR-045's graph-research decision.

## Context

SemStreams is the governed graph substrate for the `sem*` product family. Its production composition nevertheless
grew to register every first-party component, payload, and tool. Product-specific OGC, GitHub, and AGNTCY behavior,
examples, optional exporters, and incomplete protocol facades consequently appeared to be framework capabilities.

Disabling a component in configuration does not remove its dependency, generated schema, maintenance contract, or
apparent availability. The global registries therefore made ownership leakage a public API and expanded the entity-ID,
index, retention, schema, and release surfaces that SemStreams had to maintain.

The audit also found a distinct framework need that must not be lost in a package cleanup. ADR-045 recorded that
agents do not reliably choose and compose graph primitives when familiar grep, file, and web-search paths are
available. The bounded graph-research chain turns natural-language intent into graph classification, retrieval,
deterministic fusion, evidence, and a provenance-bearing result. That is an ergonomic bridge to SemStreams' defining
data model, not product-domain policy.

## Decision

### Framework admission

A package belongs in SemStreams only when it is substrate-shaped and satisfies one of two positive tests:

1. two or more independent products reuse its contract; or
2. it is necessary to make SemStreams' defining graph, KV, rule, lifecycle, storage, or agentic substrate usable.

The second test is deliberately narrow. It requires recorded evidence of a framework-level usability or correctness
gap, a product-neutral contract, and no product vocabulary or policy. Standards interest, sponsor interest, an in-repo
example, or first-party authorship does not establish framework ownership.

Graph research satisfies the second test. Its research payloads, five bounded components, R0-R6 rule pack,
classifier/query path, deterministic fusion, ObjectStore evidence, `research_graph`, and `read_loop_result` remain a
coherent SemStreams capability. Products may configure personas and domain policy, but the retained capability stays
graph-focused and product-neutral.

### Explicit composition

Framework binaries select explicit capability sets rather than a global register-everything surface:

- core substrate;
- graph research;
- optional framework adapters;
- product extensions; and
- examples and tooling.

Core, graph research, and optional adapters use separate import roots so selecting core does not link an unselected
capability. Product adapters are registered by their owning binaries. Example processors remain available to example
and E2E binaries but do not ship in the production SemStreams composition. Generated catalogs describe only the
selected composition.

Graph research is selected atomically. An absent capability does not advertise `research_graph`; a partial
configuration fails boot rather than offering a tool whose component, rule, state, evidence, or result path cannot
complete.

### Ownership and removal ledger

| Surface | Decision |
|---|---|
| OMS, SensorML, SWE Common, CS API, and associated vocabulary | SemConnect owns and composes the bundle |
| GitHub webhook, payloads, forge executors, flow, and rule policy | SemDev owns and composes the bundle |
| OASF projection and AGNTCY directory registration | SemTeams owns and composes the bundle |
| A2A and SLIM facades | Remove until a conformant implementation has an explicit owner |
| AGNTCY identity provider stub and durable core coupling | Remove |
| OpenTelemetry exporter | Retain as an explicit optional SemStreams adapter and fail closed |
| IoT and document processors | Retain for examples and E2E, not the production binary |
| Placeholder parser and unused `federation`, `subjects`, and `input/cli` packages | Remove |
| Graph research | Retain as an atomic SemStreams capability |

Generic HTTP input, GeoJSON/spatial primitives, graph APIs, vocabulary registration/export, rules, agent execution,
fusion, ObjectStore, and other product-neutral substrate remain framework-owned. A standards adapter may return only
after it independently satisfies the admission rule; it does not return merely because its product owner copied it
from this repository.

### Clean break

No compatibility registration, deprecated import, dual reader, or alias is retained. Product reference designs update
their imports and composition roots, or remove configurations for deleted facades. Beta NATS state may be wiped and
reseeded. Downstream owner validation is a pre-v1 release gate, not a requirement that SemStreams retain product code
until every downstream commit lands. The
[clean-break owner inventory](../operations/27-framework-package-boundary-clean-break.md) names the exact affected
imports, configurations, schemas, and validation obligations.

Retention follows the selected owner. SemStreams owns generic bounded-store, tombstone, cleanup, and ObjectStore
reachability contracts. A product or optional adapter owns cleanup for every derived record that it creates.

## Consequences

### Positive

- The production binary and generated catalog make honest capability claims.
- Entity-ID, graph-index, retention, schema, and release work operate on the actual framework surface.
- Product protocol and vocabulary changes no longer force SemStreams releases.
- Graph-first agent ergonomics remain a first-class, tested framework capability.
- Incomplete protocol behavior cannot be mistaken for successful interoperability.

### Negative

- SemConnect, SemDev, and SemTeams must take ownership of imports, schemas, configuration, and validation that they
  previously received transitively.
- Explicit composition creates more than one registration entry point and requires dependency-closure tests.
- A future second consumer cannot reuse a moved adapter from SemStreams until the adapter earns framework admission
  under the new rule.

## Supersession

- ADR-025 remains authoritative for generic agentic primitives, but its GitHub-executor ownership and ambient builtin
  registration decisions are superseded.
- ADR-042's SemStreams ownership of OASF taxonomy mapping and registration is superseded by SemTeams ownership.
- ADR-044's generic HTTP and GeoJSON boundary remains, but its framework ownership of OMS, SensorML, SWE Common, and
  product vocabulary is superseded by SemConnect ownership.
- ADR-050's framework ownership of `pkg/swecommon` is superseded by SemConnect ownership.
- ADR-045 is reaffirmed: graph research remains framework-owned and is now composed atomically.
