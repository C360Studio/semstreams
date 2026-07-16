## Why

SemStreams is the governed graph substrate for the `sem*` product family, but its production composition roots
currently register every first-party component, payload, and tool as though it were framework substrate. That pulls
product-owned OGC, GitHub, and AGNTCY adapters into the core binary and generated public catalog. It also advertises
SLIM and A2A integrations whose transport, authentication, status, or cancellation behavior is incomplete.

The problem is wider than package size. Global registration turns speculative or product-specific behavior into a
framework contract, expands the entity-ID and retention surface, and lets an unavailable capability appear usable.

The audit also confirmed an important counterexample: graph-first agent ergonomics are part of SemStreams' framework
identity. Instrumented agent runs established that agents fall back to familiar grep, file, and web-search behavior
when they must manually drive graph traversal. The `research_graph` capability exists to bridge that trained-
ergonomics gap through bounded classifier, query, fusion, assessment, and synthesis stages. This change preserves that
capability and makes its composition atomic; it does not classify the graph-research surface as product cruft.

## What Changes

- Define explicit framework-core, framework-capability, optional-adapter, product, example, and tooling ownership
  classes.
- Replace kitchen-sink registration with separate Go import roots for core substrate, graph research, and optional
  adapters. Selecting core alone must not link unselected capabilities.
- Preserve `research_graph`, `agentic/research`, the five research components, the R0-R6 rule pack, graph query and
  classification, `pkg/fusion`, ObjectStore evidence, and `read_loop_result` as the framework's graph-research
  capability.
- Require graph research to be composed atomically: the tool MUST NOT be advertised when its components, rule pack,
  state store, and result-reading path are unavailable or incomplete.
- Remove OMS and GitHub payloads, GitHub tools, and GitHub/AGNTCY components from framework-default registration.
- Remove the incomplete SLIM and A2A protocol facades and the unused stub AGNTCY identity provider.
- Remove unused `federation`, `subjects`, and `input/cli` public packages; active federation needs are represented by
  the governed entity, payload-envelope, graph mutation, and NATS subject primitives that replaced them.
- Remove bundled example processors from the production `semstreams` binary while retaining them in example and E2E
  composition roots.
- Keep OpenTelemetry outside core registration as an explicitly selected optional adapter, and fail closed for
  unsupported protocols instead of accepting a configuration that exports nothing.
- Remove dead placeholder parser code.
- Record clean-break owner handoffs for SemConnect, SemDev, and SemTeams. No compatibility shim, dual registration,
  or deprecated alias is introduced.
- Add ADR-075 to supersede the affected ownership/registration decisions while retaining ADR-045's graph-ergonomics
  decision. Historical ADR bodies receive supersession pointers only.

**BREAKING:** this is a pre-v1 clean break. Product reference designs update their imports and composition roots or
wipe/reseed as needed. SemStreams does not preserve deprecated registration entry points or beta adapter packages.

## Non-goals

- Removing or weakening graph-first agent research ergonomics.
- Replacing the rule engine, agent loop, query classifier, fusion engine, ObjectStore, or graph tools.
- Designing conformant replacement implementations for A2A or SLIM.
- Creating a permanent `semstreams/adapters` dumping ground.
- Providing an in-place migration for beta configs, generated schemas, or NATS state.
- Completing the separate graph-index, entity-ID, or operational-retention changes.

## Capabilities

### Added Capabilities

- `framework-composition`: ownership admission, explicit registration, coherent capability composition, and
  product-adapter exclusion.

## Impact

- **Framework code:** component, payload, and tool registries; `cmd/semstreams`; OpenAPI/schema generation; agent loop
  identity shape; OpenTelemetry initialization.
- **Removed framework facades:** SLIM, current A2A, the AGNTCY identity stub, and the placeholder CSV parser.
- **Product ownership:** SemConnect owns OMS/SensorML/SWE/CS API adapters and vocabulary; SemDev owns GitHub webhook
  and forge tools; SemTeams owns AGNTCY/OASF directory projection and registration policy.
- **Retained framework capability:** graph research remains a first-class SemStreams capability with an atomic
  composition contract.
- **Governance:** ADR-075 supersedes the affected ownership/registration decisions in ADR-025, ADR-042, ADR-044,
  and ADR-050. ADR-045 is explicitly reaffirmed rather than superseded.
