## Context

PR #524's physical hardening is valuable: one membership per key, O(E) write volume, no shared-list CAS,
per-entity ordered reconciliation, exact watermarks, explicit empty OUTGOING projections, typed readiness,
and bounded repair. Its predicate encoding rationale assumed graph-ingest accepts any non-empty predicate and
that predicate arity varies. The restored predicate contract invalidates those assumptions but does not make
the physical hardening itself wrong.

ADR-068/073 later inferred that entity IDs in non-prefix key positions cannot be found from a bare tombstone
and therefore require manifests or payload-rich tombstones. NATS `ListKeysFiltered` accepts fixed-position
subject wildcards. The API capability is verified; production performance and behavior under mutation are
not.

## Goals / Non-Goals

**Goals:**

- preserve PR #524's correctness and scale invariants;
- decide PREDICATE_INDEX representation from contract and benchmark evidence;
- reconcile stale entity-owned rows through the simplest proven NATS primitive;
- make source/target ownership explicit for INCOMING;
- narrow retention manifests and tombstone payloads to stores that need them;
- keep query results and readiness behavior stable across any cutover.

**Non-Goals:**

- add a general secondary-index planner;
- promise that leading-wildcard enumeration is cheap before measuring it;
- use blind TTL/MaxBytes eviction as semantic cleanup;
- make graph-index the authority for cascade or blob lifecycle.

## Decisions

### 1. Preserve the PR #524 invariants

Every list-valued index remains sharded one membership per key. Entity work remains ordered through the
existing keyed lane and reconciles current ENTITY_STATES at execution. A failed required write/delete keeps
the entity failed and query readiness withheld. Present entities replace OUTGOING with the complete current
array, including `[]`.

The representation study is not permission to regress these properties.

### 2. Describe every store through an ownership/filter matrix

Each derived store declares:

- token layout and exact arity;
- semantic owner of the row;
- forward query filter;
- owner reconciliation filter;
- update, hard-delete, and logical-retirement behavior;
- clean-cutover reset rule;
- read/write budget and readiness consequence on failure.

The initial PR #524 matrix is:

| Store | Current layout | Owner | Owner filter |
|---|---|---|---|
| PREDICATE | `hash(predicate).entity6` | entity | `*.entity6` |
| NAME | `hash(name).entity6.hex(predicate)` | entity | `*.entity6.*` |
| CONTEXT | `entity6.hash(context).hex(predicate)` | entity | `entity6.>` |
| INCOMING | `target6.source6.hex(predicate)` | source assertion | `*.*.*.*.*.*.source6.*` |
| OUTGOING | `entity6` | entity | exact key |
| ALIAS | `alias -> entityID` | entity in value only | unavailable by key filter |

### 3. Prove fixed-position enumeration on real NATS

The spike uses the production NATS client and JetStream server, not only mocks. It verifies exact matching,
no false positives, duplicate handling, concurrent Put/Delete, stale-row retraction, empty buckets, restart,
repair, clean bucket recreation, and cancellation/time budgets.

The benchmark profile is frozen before the first measured run:

- CI guard: 5,000 hot-predicate members plus 20 other predicates; each measured operation completes in less
  than 3 seconds, matching the existing ADR-065 guard.
- Full decision profile: 21,000 entities; one predicate and one INCOMING hub span all entities; NAME and
  CONTEXT each include a 5,000-member hotspot plus unique remainder values.
- Execution: five warmups followed by 30 measured repetitions per filter/candidate on the same server shape.
- Latency: p95 at most 3 seconds, p99 at most 5 seconds, and no operation reaches the 10-second handler bound.
- Resource comparison: client allocated bytes, server CPU time, and server RSS delta are each no more than
  twice an owner-manifest baseline over the same dataset and operation sequence.
- Correctness: zero false matches, omissions, stale survivors, or ownership violations.

The run records matched and scanned keys/bytes when observable, temporary consumer cost, and end-to-end
reconciliation time. The profile and environment fingerprint are versioned with the result; an unregistered
or changed profile cannot select the architecture.

### 4. Benchmark two PREDICATE_INDEX candidates

Candidate A preserves `hash(predicate).entity6` plus PREDICATE_CATALOG. It is grammar-independent but requires
catalog consistency and joins for human names/namespaces.

Candidate B uses the enforced fixed-nine-token
`domain.category.property.org.platform.domain.system.type.instance`. It supports exact predicate and
namespace filters, direct membership watches, entity-position reconciliation, and human-readable keys
without a catalog.

The benchmark compares correctness, O(E) writes, key bytes, server/client resource use, exact and namespace
query latency, direct watch semantics, leading-wildcard cleanup, failure modes, and operational inspection.
The pre-registered rubric ranks correctness and failure convergence first, handler/recovery budgets second,
steady-state durable structures third, and measured resource cost fourth. The result is recorded in a new
superseding ADR. There is no permanent dual-write option; a selected format cuts over by bucket recreation
and clean replay with query readiness withheld.

### 5. Keep storage codecs independent per axis

NAME and CONTEXT remain hashed because they are arbitrary/open content. INCOMING/NAME/CONTEXT predicate hex
may remain as a reversible single-token codec even after raw unsafe predicates are rejected. It is removed
only if a real query or operational requirement outweighs format churn.

PREDICATE_CATALOG raw keys must always obey the canonical predicate grammar. If Candidate A wins, catalog and
membership form one logical required projection, not a cross-bucket transaction: partial success marks the
entity failed, withholds readiness, and schedules idempotent repair until both buckets converge. If Candidate
B wins, the catalog is retired after cutover.

### 6. Reconcile stored owner rows against desired projection

For each entity update, the index owner enumerates that entity's currently stored rows using its proven
owner filter, computes the desired projection from current ENTITY_STATES, deletes stale rows, and puts missing
rows. Results from filtered listing are deduplicated before diffing. The `[A] -> []` transition is required
for every membership index.

If a store passes the frozen filter profile, filtered reconciliation is preferred because it avoids another
durable write structure. If it fails correctness or any numeric budget, it adopts an owner-local manifest or
consumes a tombstone payload under a separately specified contract.

### 7. Preserve INCOMING source ownership

An INCOMING row represents a source entity's assertion about a target. Source update or death
retracts the source-owned row through the source-axis filter. Target logical retirement does not erase live
source evidence; query policy may classify the target as retired while preserving the assertion. The
target-prefix hard-delete is removed rather than retained as a second behavior.

### 8. Make the clean cutover and readiness explicit

The cutover stops readers, deletes/recreates the selected index buckets, replays freshly reingested canonical
ENTITY_STATES, and exposes an exact readiness watermark. No reader recognizes old keys and no code interprets
abandoned formats. Exact/namespace query, traversal, and clustering fixtures must match canonical expected
results. Any required reconciliation or replay failure keeps reads not-ready.

## Risks / Trade-offs

- **Leading wildcard filters may scan too broadly:** benchmark realistic shapes and retain manifests only for
  failed stores.
- **Raw keys couple storage to grammar:** allow them only after canonical source cutover and unconditional
  enforcement are real.
- **Hash/catalog can drift:** make catalog a required repaired projection or retire it with raw keys.
- **Another on-disk cutover adds upgrade risk:** use one bucket reset/replay, no steady-state dual format,
  and query parity evidence.
- **Target cleanup can erase valid evidence:** model INCOMING ownership by source, not physical prefix.

## Cutover Plan

1. Correct/complete/archive `graph-index-hardening` and seed the baseline graph-index spec.
2. Build the real-NATS fixed-position filter test/benchmark without changing production keys.
3. Run Candidate A/B comparison after the predicate grammar and owned producer corpus are clean.
4. Record the winner and precise ADR-065/068/073 supersession in a new ADR.
5. Implement owner reconciliation and the selected catalog/key behavior.
6. Delete/recreate selected buckets, reingest canonical state, and verify query fixtures/readiness.
7. Correct issues/docs and run race, contract, structural, semantic, and affected product e2e gates.

## Open Questions

- Can one filtered-list consumer serve multiple owner filters efficiently, or is per-request setup material?
- Does any real consumer require raw predicate membership watches, or only exact/namespace request-response?
- Which remaining stores justify tombstone payloads independently of graph-index cleanup?
