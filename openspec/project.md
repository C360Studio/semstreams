# SemStreams Project Context

## Purpose

SemStreams is the **governed graph substrate and framework** for the C360 `sem*`
family. It turns event data into a semantic knowledge graph on NATS JetStream and
gives every product above it one shared runtime: the KV-twofer (state + events +
history from a single write), Graphable ingestion into `ENTITY_STATES`, graph
mutation/query/gateway APIs, the rule engine, the Lifecycle harness, the agentic
loop/tool/model substrate, deterministic fusion, the payload registry, and the
component/port flow model.

SemStreams is a **framework, not a product**. It owns primitives and contracts;
it does not own any product's domain semantics. If a capability is only meaningful
to one consumer's problem (a game mechanic, a COP fusion rule, a spec-review UI),
it belongs in that product, not here. The test for "does this belong in
SemStreams" is: *would two or more `sem*` products reuse it, and is it expressed
as a substrate primitive rather than one product's mental model?* (See the
`/framework-vs-product-boundary` discipline: unwired-in-the-framework ≠ broken —
classify the gap before building.)

## Product Boundary

- **SemStreams owns** the governed graph substrate: the KV-twofer and NATS/KV
  runtime; Graphable ingestion, `ENTITY_STATES`, the single-writer invariant, and
  graph mutation/query/gateway APIs; the derived indexes (predicate, name, alias,
  incoming/outgoing, context) and indexing profiles; projection contracts,
  ownership claims/leases, and the graph-write-intent taxonomy (ADR-055/056); the
  **Lifecycle harness** (ADR-047) — Participant/Manager, KV-backed workflow state,
  operator gateway; the **rule engine** (conditions, actions, iteration caps,
  `publish_agent`, `for_each`, gated-DAG); the **agentic substrate**
  (loop/tools/model/dispatch/memory/governance primitives — NOT product agent
  personas); **deterministic fusion** (`pkg/fusion`, ADR-062); the **payload
  registry**; the vocabulary registry; and shared runtime services (schema
  generation, health, metrics).
- **Products own their domain semantics and compose SemStreams primitives.**
  SemStreams supports them as substrate; it must not absorb their behavior:
  - **SemSource** — source discovery/parsing, binary/media by-reference, source
    provenance, and source-graph publishing.
  - **SemOps** — COP product semantics, feed fusion, tactical UI, product-level
    graph ownership.
  - **SemConnect** — the OGC Connected Systems API bridge.
  - **SemTeams / SemSpec** — multi-agent team coordination and spec-driven
    development orchestration (agent personas, review surfaces, product workflows).
  - **SemDragon** — game mechanics, trust, and quest semantics (the product whose
    agents do dev work).
- **Cross-repo contracts** (a mutation-API shape, a payload envelope, a readiness
  signal, a vocabulary predicate) are the one place the boundary is shared. They
  are recorded as ADRs (decisions) and specified as specs (current truth), never
  left implicit.

## How we spec (OpenSpec) — the role split

SemStreams adopted OpenSpec at v1.0.0-beta.132+ (the last `sem*` holdout; the
CLI and `.claude/` skills are installed). See `openspec/README.md` for the layout
and `docs/adr/README.md` for the ADR-vs-spec split. In short:

- **`openspec/specs/<capability>/spec.md` — current truth.** Requirement +
  GIVEN/WHEN/THEN scenarios describing what a capability does *today*. **Seeded
  lazily** — write a spec when a change first touches that capability, distilled
  from code + existing docs. Do NOT backfill everything up front.
- **`openspec/changes/<id>/` — proposals with `proposal.md`, `tasks.md`, and
  spec deltas.** Target state is written as a delta against current specs, not by
  mutating a design doc in place. **Archived on completion** (`openspec archive`),
  never left to accumulate as ambient "Proposed" documents.
- **`docs/adr/` — genuine decisions only.** Irreversible choices and cross-repo
  contracts (the *why*). History; history doesn't drift. Existing ADRs stay as
  history; the *mechanics* an ADR implies live in the capability's spec, which
  stays current.
- **`docs/0X-*.md` — retired gradually.** "How it works" content migrates into
  specs as each area is touched; genuinely tutorial/runbook content (getting
  started, operations guides) stays as docs — that is what docs are good at.

## Standing Technical Conventions

- Entity IDs are deterministic 6-part IDs (`org.platform.domain.system.type.instance`).
- Every graph writer path carries a semantic envelope; after ADR-055, no
  producer relies on `triple.add` auto-vivifying an entity.
- `graph-ingest` is the sole writer to `ENTITY_STATES`; other components request
  changes via the `graph.mutation.*` API, never by writing the bucket.
- Communication model follows facts-vs-requests: KV Watch for facts about the
  world, JetStream streams for requests to do something (`/kv-or-stream`).
- Orchestration stays in the two layers — rules trigger, components execute;
  there is no separate workflow engine (`/orchestration-check`).
- The live graph never uses NATS TTL/MaxBytes/MaxAge for lifecycle
  (reachability-blind eviction; ADR-068).
- CI must be green before push: lint (revive warnings = failure), `-race` tests,
  cross-compile, and `task schema:generate` with no uncommitted drift.
- Large or cross-cutting changes go through OpenSpec (proposal + tasks + deltas)
  before code.
