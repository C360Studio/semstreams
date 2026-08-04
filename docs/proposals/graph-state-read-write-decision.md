# Graph state, materialized-view, and query decision proposal

**Status:** Owner-approved for careful implementation.

**Date:** 2026-08-03.

**Evidence:**
[`graph-state-read-write-inventory.md`](graph-state-read-write-inventory.md).

## Decision statement

Describe SemStreams as **authoritative current semantic state with role-specific
materialized views**.

Keep `graph-ingest` as the sole writer of current authority in `ENTITY_STATES`.
Keep KV revisions and watches as the internal current-state change feed. Do not
adopt an event-sourced command/event ledger, and do not build a general CQRS
runtime.

ADR-056 can still call the authority/view split a CQRS boundary. That describes a
mechanical command/query separation; it does not make event sourcing or a generic
CQRS framework the product's architectural identity.

## Proposed owner rulings

### 1. Authority and recovery

`ENTITY_STATES` remains canonical current shared semantic state with history 1.
It is not a rebuildable projection of Graphable facts or mutation commands.

The supported authority recovery model should be snapshot/restore of:

- `ENTITY_STATES`;
- referenced ObjectStore content; and
- operational state such as `GRAPH_INGEST_APPLIED_SEQ`, handled with an explicit
  reset/restore policy rather than assumed to be domain history.

Graphable replay is bounded catch-up, not disaster recovery. It is limited by
stream retention, `MaxDeliver`, missing mutation-lane writes, stream-incarnation
changes, and graph-ingest's single-instance ordering requirement.

### 2. Derived capability survival

Apply the deletion test before designing a shared convergence framework.

- **Suffix lookup:** keep and validate stale hits. It has a live resolver;
  authority-scan fallback is not an acceptable large-graph default.
- **Topology, outgoing/incoming:** keep as core traversal and clustering input.
- **Identity/search, alias/predicate/name:** keep for live graph-query consumers.
- **Context provenance:** retire durable `CONTEXT_INDEX`; it has no repository
  production semantic reader.
- **Spatial:** keep if the spatial query capability remains declared; add removal,
  repair, and readiness obligations.
- **Temporal plus reverse bookkeeping:** keep as one family if temporal queries
  remain declared. The reverse bucket is maintenance state, not a product.
- **Embedding plus dedup:** keep as optional enrichment. Dedup inherits the
  capability lifecycle.
- **Community partition plus summaries:** deployment of the community tier is
  optional. When enabled, `COMMUNITY_INDEX` carries required-view obligations;
  `COMMUNITY_SUMMARIES` remains optional enrichment with statistical fallback.
- **Anomaly projection:** keep only with ownership repair. Gateway must stop writing
  the clustering-owned bucket directly. Treat effectful inference application as a
  separate capability, not part of projection recompute.
- **Structural persistence:** retire durable `STRUCTURAL_INDEX`; current anomaly
  use receives structure in memory and no production semantic reader exists.

Adjacent proposed retirements:

- remove the unadopted `graph/query.Client` after confirming sister-repository
  migration scope;
- remove bare `graph.mutation.entity.create` and `.update` after the same check;
- separately evaluate `COMPONENT_STATUS`, which has production writers but no
  production reader and is not part of the sixteen derived buckets.

Pre-v1 compatibility does not veto these removals. The caller census defines the
migration list, not a requirement to preserve every surface.

### 3. Obligations by role

Do not impose one contract on every `ClassDerived` bucket.

#### Required query view

Must declare:

- source and revision space;
- deterministic desired state and key ownership;
- update and removal semantics;
- dependency-change redrive;
- transient, poison, and permanently excluded outcomes;
- online repair and clean rebuild;
- readiness/failure publication; and
- query behavior during bootstrap, lag, degradation, and reset.

Where the source uses `ENTITY_STATES` KV revisions, the view must support
“covered at least revision R” or be rejected as a read-your-writes dependency.

#### Optional enrichment

Must declare capability availability, failure state, dependency redrive, rebuild,
and the lower-tier fallback. It need not promise coverage of every authority
revision when its inputs or schedule use another unit.

#### Internal accelerator, deduplication, or reverse bookkeeping

Inherits the owning capability's lifecycle. It is not independently public,
readiness-bearing, or scored in the deletion test.

#### Reactive consumer

Must declare bootstrap replay, duplicate/coalescing behavior, watcher-loss
behavior, poison handling, execution-time reread, and whether each revision may
cause an external effect.

#### Serving cache

Must declare bootstrap completeness, invalidation/removal, watcher loss, and
whether a miss fails closed, reads storage, or degrades.

#### Authority startup validation

Must remain fail-closed for reads while preserving writer availability, and must
state its dependence on history 1.

#### Effectful inference application

Must be separate from anomaly detection/materialization and declare:

- the live-mode condition that authorizes application;
- durable request correlation and idempotency;
- a loop bound preventing inferred writes from re-triggering without limit;
- authoritative mutation outcome and revision evidence; and
- failure semantics distinct from successful anomaly detection.

### 4. Runtime instance model

Default every surviving durable owner to **single active instance** until it has an
explicit active/active proof.

This includes graph-ingest, graph-index, spatial, temporal, embedding, clustering,
enhancement, and anomaly review. Catalog ownership and `OWNER_CLAIMS` are not
leader election. Graph-ingest failover must preserve authoritative state and its
ingest guard; it must not assume authority can be replayed from facts. Derived
owners may use restart-and-replay or recompute from preserved authority.

Query-only responders intended to scale should use named queue groups. Mutation
responders must not fan out; graph-ingest remains single-active until per-request
idempotency and cross-process per-entity ordering are solved.

### 5. Public read defaults

- **Remote applications:** GraphQL for operations actually implemented in the
  gateway.
- **AI agents:** no MCP graph-read promise until the endpoint and tools exist.
  Current query-access documentation claiming MCP parity must be corrected.
- **Embedded Go services:** a typed client over `graph.query.*`, using
  `pkg/fusion/fusionnats` as the strongest current adopter. Do not make raw KV or
  the unused mixed-storage `graph/query.Client` the default.
- **Lifecycle clients:** lifecycle-gateway is an authority-backed lifecycle domain
  API, not an interchangeable graph-query surface.
- **Projection owners:** catalog-authorized direct bucket access only for their
  declared dependencies and outputs.
- **Operators:** message-logger raw KV reads/watches remain explicitly diagnostic,
  not an application consistency contract.

PathRAG and GraphRAG availability must be documented from implemented handlers,
not aspirational protocol parity.

### 6. Public write defaults

Rename or move `pkg/projection` so “projection” no longer means both an
authoritative owned-fact writer and a materialized read view. A name such as
`pkg/graphwrite` would make the authority side explicit.

Consolidate callers behind typed intent operations:

- create-or-fail;
- replace-owned;
- append-evidence;
- retract-evidence;
- CAS transition; and
- delete/reclaim.

Raw subjects become internal transport constants. Request correlation and
idempotency remain the separate scope of issue #869; materialized-view work must
not claim to solve ambiguous command outcome.

### 7. Inference ownership and feedback exception

Route anomaly review state changes to a graph-clustering-owned command handler or
move review state into a separately owned store. Graph gateway must not directly
write `ANOMALY_INDEX`.

Apply approved relationships through the canonical mutation intent. Retire the
gateway's `graph.events.relationship.create` applier unless that event lane receives
an explicit producer/consumer contract and authoritative-write bridge.

Automatic and human-approved inference both create a feedback edge:

`anomaly -> mutation -> ENTITY_STATES -> indexes/embedding/clustering -> anomaly`.

Separate effectful application from anomaly projection. Detection completion must
not imply application success; current auto-apply errors are logged without failing
the detection run.

### 8. Clean rebuild and proof

Every surviving capability family must provide one operator operation with:

1. declared inputs and dependencies;
2. safe single-active enforcement;
3. destructive scope limited to that family's derived buckets;
4. replay/recompute completion evidence;
5. readiness/failure evidence; and
6. a semantic comparison proving expected keys and stale-key absence.

Derived rebuild/recompute mode must suppress authoritative mutations and other
external effects. If an operator explicitly requests live inference application,
it is a separate operation governed by the idempotency, loop-bound, and outcome
requirements above.

Authority restore is a different runbook and must never be called “projection
rebuild.”

## What option C does and does not resolve

- **Projection deletion, redrive, repair, readiness:** directly governed by role
  obligations and the deletion test.
- **Query-front-door drift:** governed by adopter-specific defaults and surface
  retirement.
- **Stale caches and indexes:** governed for surviving capabilities; deletion
  removes obligations for unused views.
- **Inference feedback loops:** separated from projection recompute, but durable
  idempotency and loop bounds still require implementation.
- **Bounded multi-key output:** remains a per-capability storage design problem.
  The role contract requires an honest outcome.
- **Command timeout/correlation:** not solved. Issue #869 remains separate.
- **Authoritative revision semantics:** partly used as evidence, but metadata and
  wire questions remain separate.
- **E2E reproducibility and proof:** not solved. Test determinism and capability
  assertions remain a separate program.
- **Authority disaster recovery:** not solved by materialized views; it requires a
  snapshot/restore decision and runbook.

## Execution sequence

1. Approve, override, or reject the rulings in this document.
2. Run sister-repository reader/caller scans for proposed removals.
3. Delete unused buckets, clients, subjects, and stale query-access claims first.
4. Separate side-effect-free anomaly recompute from live inference application;
   repair the ownership and mutation-lane exception.
5. Add role declarations and shared status vocabulary for surviving families.
6. Add conformance tests for required obligations.
7. Refactor shared runtime mechanics only if the evidence trigger below is met.
8. Resume package-level graph issues against the approved role contracts.

## Evidence trigger for a shared runtime

Revisit a reusable durable-view runtime only when all are true:

- at least three surviving capability owners need the same bootstrap, ordering,
  removal, repair, watermark, and reset behavior;
- their role-specific semantics can be expressed without escape hatches;
- duplicated conformance implementations or repeated defects demonstrate a
  measurable maintenance cost; and
- a prototype reduces total code and adopter knowledge without weakening failure
  evidence.

Until then, share declarations, vocabulary, status types, and contract tests—not a
framework.

## Owner approval checklist

- [x] Approve current-state authority; reject event sourcing.
- [x] Approve the capability survival table or record overrides.
- [x] Approve role-specific obligations.
- [x] Approve single-active durable owners as the default.
- [x] Approve adopter-specific read defaults.
- [x] Approve typed write intents and raw-subject retirement.
- [x] Approve side-effect-free anomaly rebuild and separate, bounded inference
      application, including repair of the ownership/event-lane exception.
- [x] Approve separate derived rebuild and authority restore runbooks.

## Approval record

On 2026-08-03, the owner approved all eight rulings and authorized the team to
proceed with careful implementation. This record adds no implementation mechanics
beyond the approved rulings.

After the bounded `CONTEXT_INDEX` retirement reached green CI in PR #894, the
owner instructed the team to continue. The next approved slice is the atomic
retirement of durable `STRUCTURAL_INDEX`: preserve pure K-core and pivot
algorithms as internal anomaly prerequisites, but remove their unconsumed durable
store and adopter-facing configuration and port surfaces. This slice is sequenced
after `retire-context-index`; it does not reopen ADR-090 or add a replacement
query capability.
