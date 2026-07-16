## Context

Three aggregation functions currently define the effective public framework surface:

- `componentregistry.Register`
- `payloadbuiltins.Register`
- `executors.RegisterBuiltins`

They combine substrate, optional integrations, examples, and product policy. Configuring a component as disabled does
not remove its compile-time dependency, generated schema, maintenance contract, or apparent availability. The standard
binary separately imports example IoT and document processors, so even the stated example boundary is not real.

The package audit found two distinct failure classes:

1. **Ownership leakage:** real code such as OMS/SensorML, GitHub webhook handling, OASF generation, and AGNTCY
   directory registration has one product owner and should be composed there.
2. **False capability advertisement:** SLIM, A2A, the AGNTCY identity provider, and unsupported OTLP/gRPC paths can
   report configuration or request success without delivering the represented protocol behavior.

Graph research initially appeared application-shaped because its R0-R6 wiring is a concrete chain. That reading is
incomplete. ADR-045 records a framework-level empirical constraint: agents do not reliably choose or compose graph
queries when familiar grep/web/file paths are available. The bounded chain converts natural-language intent into
classifier hits, graph evidence, deterministic fusion, and a provenance-bearing answer using the canonical
rule/component boundary. It is an ergonomic adapter from agent behavior to the framework's defining data model.

## Goals

- make the production composition root match the documented framework/product boundary;
- prevent incomplete capabilities from appearing available;
- retain graph-first research as a coherent framework capability;
- reduce the contracts that entity-ID, indexing, retention, schema, and release work must maintain;
- make pre-v1 ownership changes cleanly, without shims or compatibility layers.

## Decisions

### 1. Framework admission has two positive paths

A package belongs in SemStreams when it is substrate-shaped and either:

1. is reused by two or more independent products; or
2. is required for SemStreams to make its defining graph, KV, rule, lifecycle, storage, or agentic substrate usable.

The second path is deliberately narrow. It requires recorded evidence of a framework-level usability or correctness
gap, a product-neutral contract, and no product vocabulary or policy. Standards interest, sponsor interest, an in-repo
smoke test, or “first party” status is not sufficient.

`research_graph` qualifies through path 2. OMS/SensorML, GitHub workflow semantics, and AGNTCY/OASF projection policy
do not.

### 2. Registration is capability-based and import-isolated

The composition API separates:

- **core substrate:** graph ingest/query/index primitives, rules, lifecycle, storage, generic I/O, payload registry,
  and generic agent loop/model/tool execution;
- **graph research capability:** research payloads, five single-purpose components, graph-research tool, rule pack,
  fusion and evidence/result handling;
- **optional framework adapters:** capabilities such as OpenTelemetry that are generic but not required by every
  binary;
- **product adapters:** registered only by the owning product binary;
- **examples/tooling:** registered only by their dedicated binaries.

Public entry points use explicit names. A compatibility `RegisterAll` or deprecated alias is not retained.
OpenTelemetry is excluded from core registration and is selected only through its optional-adapter entry point.

Separate functions in one Go package are insufficient because package imports are compiled as one unit. Core-only
component, payload, and tool composition therefore has an import root that does not import graph research, product
adapters, optional adapters, examples, or tooling. Graph research has a separate import root that adds its payloads,
five components, tool executor, and configuration validation atomically. Optional adapters likewise remain separate
import roots.

The dependency boundary is a tested contract. A core-only fixture is inspected through `go list -deps` or an
equivalent build-graph assertion. The test fails if an unselected graph-research, product, optional-adapter, or example
package appears in its dependency closure.

### 3. Graph research is one coherent capability

The retained contract is:

```text
research_graph(topic, hints)
  -> classifier and graph-query candidates
  -> bounded route/execute/assess rules and components
  -> deterministic fusion and ObjectStore evidence
  -> provenance-bearing SearchResult
  -> read_loop_result / parent continuation
```

The rule pack remains operator-configurable, but advertisement is atomic. At bootstrap SemStreams MUST validate that
the configured graph-research capability contains all five components, the rule processor with the complete R0-R6
pack, AGENT_LOOPS access, graph ingest/query dependencies, and result retrieval. If the capability is absent, the
`research_graph` tool is absent. If it is partially configured, boot fails with a stable actionable error rather than
registering a tool that will stall.

The prompts implement bounded product-neutral decisions over graph-search evidence. Product-specific prompt policy or
domain vocabulary remains product-owned and is supplied through configuration or personas.

### 4. Product integrations leave framework-default registration

The ownership ledger is:

| Surface | Owner | SemStreams disposition |
|---|---|---|
| OGC/Connected Systems bundle | SemConnect | hand off, remove from defaults, then remove framework copies |
| GitHub webhook and forge bundle | SemDev | hand off, remove from defaults, then remove framework copies |
| OASF, directory, and identity policy | SemTeams | hand off, remove from defaults, then remove framework copies |
| A2A facade | none until conformant | delete |
| SLIM facade | none until real SDK/transport | delete |
| IoT/document processors | examples/E2E | remove from production binary only |
| OpenTelemetry | optional SemStreams adapter | retain, fail closed, explicitly compose |
| research graph | SemStreams | retain as coherent framework capability |
| `federation`, `subjects`, `input/cli` | none | delete unused superseded public packages |

Cross-repo handoff commits may land independently. Exact break notices and downstream updates are pre-v1 release
gates, not local SemStreams merge gates. SemStreams does not retain compatibility imports after the announced clean
break.

### 5. Unsupported behavior fails closed

- A2A and SLIM are removed rather than returning placeholder success.
- The AGNTCY provider stub and optional identity field are removed from the durable core loop shape.
- OpenTelemetry rejects protocols for which it cannot construct an exporter. Export counters advance only after a
  real exporter accepts the batch.
- Placeholder CSV parsing is removed rather than returning `parsed:false` as though parsing succeeded.

### 6. Generated catalogs describe the selected composition

Schema/OpenAPI generation consumes the framework-core plus explicitly selected framework capabilities. Product
adapters are generated by the owning product. The framework catalog therefore no longer exposes A2A, SLIM, GitHub,
OMS, OASF, or directory configuration merely because those packages once lived in this module.

### 7. Retention ownership follows derived-state ownership

SemStreams defines generic bounded-store, tombstone, cleanup, and ObjectStore reachability contracts. A product
adapter owns cleanup for the derived records it creates. Moving OASF/GitHub/OGC projections out of core removes their
domain stores from the framework retention ledger; it does not waive their owning product's cleanup obligation.

## Rejected Alternatives

### Keep one registry and rely on `enabled:false`

Rejected because disabled configuration still creates compile-time, schema, dependency, and maintenance coupling.

### Move the complete research chain to a product

Rejected because it would remove the framework's proven ergonomic bridge between agents and the knowledge graph.
Products would predictably rebuild weaker grep/web/RAG paths or private orchestration.

### Keep protocol facades as placeholders

Rejected because an interoperability or security facade that reports success without implementing the protocol is
more dangerous than an absent capability.

### Create a permanent compatibility adapter package

Rejected because the project is pre-v1, product data may be wiped, and the explicit goal is a clean contract without
deprecated context pollution.

## Migration

1. Publish the ownership ledger and breaking package/config list.
2. Update owning reference designs to register or vendor their adapters.
3. Split SemStreams composition import roots and make graph-research admission atomic.
4. Remove unsafe/dead packages and production example registration.
5. Regenerate framework schemas/OpenAPI from the reduced composition.
6. Run unit, race, integration, contract, schema, core e2e, agentic e2e, and research-graph e2e gates.
7. Resume entity-ID, graph-index, and retention work with the corrected framework inventory.

The SemStreams cleanup may merge before downstream reference-design updates. The pre-v1 release may not proceed until
the break notices have been consumed and affected downstream validation is green. No persisted beta-state migration
or compatibility reader is provided.
