# SemStreams Project Context

## Purpose

SemStreams is the **governed graph substrate and framework** for the C360 `sem*`
family. It turns event data into a semantic knowledge graph on NATS JetStream and
gives every product above it one shared runtime: the KV twofer (current state +
watch notification, with explicitly configured bounded history), Graphable
ingestion into `ENTITY_STATES`, graph
mutation/query/gateway APIs, the rule engine, the Lifecycle harness, the agentic
loop/tool/model substrate, deterministic fusion, the payload registry, and the
component/port flow model.

SemStreams is a **framework, not a product**. It owns primitives and contracts;
it does not own any product's domain semantics. If a capability is only meaningful
to one consumer's problem (a game mechanic, a COP fusion rule, a spec-review UI),
it belongs in that product, not here.

A package is admitted only when it is substrate-shaped and either (1) two or more
independent products reuse its contract or (2) it is required to make SemStreams'
defining graph, KV, rule, lifecycle, storage, or agentic substrate usable. The
second path requires recorded evidence of a framework-level usability or
correctness gap, a product-neutral contract, and no product vocabulary or policy.
Standards interest, sponsor interest, an in-repo example, or first-party authorship
is not sufficient. See ADR-075. Unwired-in-the-framework does not mean broken;
classify ownership before building.

## Product Boundary

- **SemStreams owns** the governed graph substrate: the KV-twofer and NATS/KV
  runtime; Graphable ingestion, `ENTITY_STATES`, the single-writer invariant, and
  graph mutation/query/gateway APIs; the consumed derived indexes (predicate, name,
  alias, incoming/outgoing) and indexing profiles; local projection contracts,
  the typed four-operation graph mutation algebra, and exact authority reads (ADR-091); the
  **Lifecycle harness** (ADR-049) — `Participant`/`Manager` current state over
  graph-ingest-owned `ENTITY_STATES`, plus operator gateway; the **rule engine**
  (conditions, actions, iteration caps,
  `publish_agent`, `for_each`, gated-DAG); the **agentic substrate**
  (loop/tools/model/dispatch/memory/governance primitives — NOT product agent
  personas); **deterministic fusion** (`pkg/fusion`, ADR-062); the **payload
  registry**; the vocabulary registry; the atomic **graph-research capability**
  (classifier/query, bounded research stages, fusion, ObjectStore evidence, and
  result retrieval; ADR-045/075); and shared runtime services (schema generation,
  health, metrics).
- **Products own their domain semantics and compose SemStreams primitives.**
  SemStreams supports them as substrate; it must not absorb their behavior:
  - **SemSource** — source discovery/parsing, binary/media by-reference, source
    provenance, and source-graph publishing.
  - **SemOps** — COP product semantics, feed fusion, tactical UI, product-level
    graph ownership.
  - **SemConnect** — the OGC Connected Systems API bridge, including OMS,
    SensorML, SWE Common, CS API, and associated vocabulary contracts.
  - **SemDev** — GitHub webhook, forge tools, and development-workflow policy.
  - **SemTeams / SemSpec** — multi-agent team coordination and spec-driven
    development orchestration (agent personas, review surfaces, product workflows).
    SemTeams also owns OASF projection and AGNTCY directory registration policy.
  - **SemDragon** — game mechanics, trust, and quest semantics (the product whose
    agents do dev work).
- **Cross-repo contracts** (a mutation-API shape, a payload envelope, a readiness
  signal, a vocabulary predicate) are the one place the boundary is shared. They
  are recorded as ADRs (decisions) and specified as specs (current truth), never
  left implicit.
- Production binaries compose core, retained framework capabilities, optional
  adapters, and product extensions explicitly. Examples and product adapters are
  not imported by framework-core registration merely because they are first party.

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
- Every entity birth carries a semantic envelope. Non-create mutations are must-exist and no
  producer relies on a mutation auto-vivifying an entity.
- `graph-ingest` is the sole writer to `ENTITY_STATES`; other components request
  changes through the typed `nats-request` `graph.mutation.>` component port, never by writing the bucket.
- Communication separates current facts from queued work. KV watchers rehydrate
  current matching values; JetStream consumers resume unacknowledged requests
  (`/kv-or-stream`). History is explicitly bounded, and `ENTITY_STATES` history 1
  is current authority rather than audit or recovery history.
- Orchestration stays in the two layers — rules trigger, components execute;
  there is no separate workflow engine (`/orchestration-check`).
- The live graph never uses NATS TTL/MaxBytes/MaxAge for lifecycle
  (reachability-blind eviction; ADR-068).
- CI must be green before push: lint (revive warnings = failure), `-race` tests,
  cross-compile, and `task schema:generate` with no uncommitted drift.
- Large or cross-cutting changes go through OpenSpec (proposal + tasks + deltas)
  before code.
