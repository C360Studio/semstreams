# ADR-077: Bounded Owner Discovery and Source-Owned INCOMING Evidence

## Status

**Accepted (2026-07-17).** This record approves fixed-position owner discovery as the replacement mechanism for
the bounded graph-index layouts named below and fixes INCOMING ownership on the source assertion. Acceptance of the
decision does not certify production activation: activation remains blocked until every evidence gate in this ADR
passes and is recorded against the exact implementation revision.

## Context

The composite-key graph indexes introduced by ADR-065 remove shared-list contention, but NAME, PREDICATE, and
INCOMING historically appended memberships. An entity changing from membership A to B and then to an empty
projection could therefore remain query-visible through A or B.

Canonical predicates and bounded six-part entity IDs now make the relevant key positions fixed and provable. NATS
single-token wildcards can enumerate rows owned by one entity without a central reverse manifest, provided that the
complete key and filter are validated, the filter has exact arity, and real-NATS evidence proves the leading-wildcard
shape remains bounded under the production workload.

The physical prefix of an INCOMING row is not its semantic owner. A row
`target6.source6.hex(predicate)` records the source's assertion about the target. Treating the target prefix as delete
authority destroys evidence that a live source still owns.

The governing capability change is
[`graph-index-replacement-semantics`](../../openspec/changes/graph-index-replacement-semantics/proposal.md).

## Decision

### 1. Ownership is semantic and explicit

The graph-index owner matrix is:

| Store | Physical layout | Semantic owner | Owner discovery |
|---|---|---|---|
| PREDICATE, selected by ADR-078 | `predicate3.entity6` | entity | `*.*.*.entity6` |
| NAME | `hash(name).entity6.hex(predicate)` | entity | `*.entity6.*` |
| CONTEXT | `entity6.hash(context).hex(predicate)` | entity | `entity6.*.*` or the equivalent prefix |
| INCOMING | `target6.source6.hex(predicate)` | source assertion | `*.*.*.*.*.*.source6.*` |
| OUTGOING | `entity6` | entity | exact owner key |
| PREDICATE_CATALOG | retired by ADR-078 | none | none |
| ALIAS | variable raw alias key, entity in value | separate owner | unavailable by owner-key filter |

ALIAS, spatial/geohash, embedding, blob/ObjectStore, cascade, and global GC are not made safe by this decision.
Their authorities remain separately specified. Absence from this matrix is never permission for a bucket scan or
best-effort deletion.

### 2. Owner discovery is a bounded fixed-arity technique, not general suffix search

Leading-wildcard discovery is approved only for a layout whose complete arity and every entity-bearing position are
fixed by enforced canonical contracts. It uses literal single-token wildcards; it does not use `>`, accept variable
arity, search arbitrary open content, or infer ownership from a value.

Every forward filter and owner filter must reject shorter, longer, neighboring-owner, and reversed-axis controls.
The proof covers the maximum canonical 256-byte entity ID and the maximum predicate/key/filter values, not only a
representative corpus. The server and Go SDK versions are pinned in the evidence so an SDK or server change cannot
silently inherit the result.

At the 256-byte entity bound, the governed selected-layout maxima are 451 bytes for PREDICATE, 710 for NAME and
CONTEXT, 902 for INCOMING, and 256 for OUTGOING. Unit arithmetic is prerequisite evidence only; every maximum
literal key and filter must also produce the exact expected match set on pinned real NATS.

### 3. Canonical validation and physical preflight precede I/O

For one reconciliation attempt, the component first validates the authoritative owner ID, every referenced entity
ID, every predicate, the literal owner/forward filters, and every desired physical key. Validation uses the shared
entity, predicate, and `nats-kv-keys` contracts. No lister, Get, Put, Delete, or other bucket side effect starts for
an invalid candidate.

A malformed authoritative entity value or key is incompatible graph state. It must hold readiness in typed
reset-required or failed state; the component must not merely log the error and complete as healthy. Validation is
authority, while hashing or hex encoding remains representation only and cannot admit noncanonical input.

### 4. Replacement reconciles complete owner projections

At execution, the keyed entity lane reads current `ENTITY_STATES`, computes the complete desired projection, lists
the stored owner rows, deduplicates exact physical keys, and diffs the two sets. It deletes stale rows, puts missing
rows, and overwrites retained values where the value carries current meaning, as NAME does for case and priority.

The required result is query-visible `[A] -> [B] -> []` replacement for NAME, PREDICATE, source-owned INCOMING, and
the already-reconciling CONTEXT and OUTGOING projections. Predicate-list and namespace-list expose only predicates
with a non-zero current membership; vocabulary declaration and history remain vocabulary-registry responsibilities.

### 5. INCOMING is source-owned evidence

A source fact replacement retracts the former INCOMING row. Source removal retracts all rows discovered on the
source axis across every target. Target retirement, removal, or tombstoning does not delete assertions still owned
by live sources, so target-prefix hard-delete is retired.

This is evidence preservation, not completion of retention semantics. Until ADR-068/ADR-073 retention work supplies
referential policy and tombstones, a query may observe an assertion whose target is absent. Cascade is authorized
only by changing or retracting the source-owned fact through its declared policy; target-prefix deletion is not a
cascade implementation.

This decision supersedes ADR-068 D3 only where D3 inferred one shared graph-index reverse manifest and target-prefix
cleanup. ADR-068's tombstone, refuse/cascade, per-consumer watermark, and GC requirements remain in force as amended
by ADR-073.

### 6. Readiness and recovery remain fail-closed

Per-entity keyed FIFO ordering, execution-time authoritative reads, low-water-of-pending completion, bounded repair,
and query readiness from ADR-066/PR #524 remain mandatory.

A revision may complete in the low-water tracker after an attempted write so one failure cannot deadlock revision
progress. Separately, any required list, delete, put, or decode failure marks the entity or process failed;
`failedCount > 0` or reset-required state keeps all dependent query surfaces not-ready. Success does not record the
no-op projection baseline until every required projection succeeds. Repair re-enters the same keyed lane, reads
current `ENTITY_STATES`, and clears the failure only after complete convergence.

Queries deduplicate and establish their total order before applying limits or samples. Exact, value-filtered,
compound, and stats results order by entity ID; predicate lists order by predicate identity; INCOMING orders by
`(sourceID, predicate)`; NAME retains its ranking tuple with entity ID as the final tie-breaker.

### 7. Reconciliation remains observable

Replacing additive helpers must not erase established operating signals. Reconciliation records:

- one processed watch event at the entity boundary in `semstreams_graph_index_events_processed_total` and the
  bounded event class in `semstreams_graph_index_watch_events_total{event_type}`;
- completed per-index semantic updates for NAME, PREDICATE, INCOMING, CONTEXT, and OUTGOING in
  `semstreams_graph_index_updates_total{index_type}`;
- physical list, put, and delete attempts by bucket in
  `semstreams_graph_index_kv_operations_total{operation,kv_bucket}`;
- terminal required-write failures that hold readiness in `semstreams_graph_index_write_failures_total`, with
  changed/unchanged repair input recorded in `semstreams_graph_index_reindex_events_total{result}`;
- queue depth/high-water, catch-up time, and temporary-consumer high-water/return-to-baseline in gate evidence.

Metric labels remain bounded enumerations and never contain entity IDs, predicates, filters, or caller content. A
retry must not masquerade as an additional successful semantic update; physical-operation metrics may count each
actual attempt.

### 8. Activation is evidence-gated

Production activation is prohibited until all of the following are green for every selected owner filter:

1. unit contract proof for complete layouts, arities, maxima, and pre-I/O rejection;
2. pinned maximum-value real-NATS exact-match conformance;
3. concurrent Put/Delete convergence, cancellation, empty bucket, restart, and clean bucket recreation;
4. the 5,000-hot-member plus 20-predicate CI guard, with each operation below 3 seconds;
5. one 21,000-entity sustained-churn run at the configured worker shape and one stress shape, with p95 at most
   3 seconds, p99 at most 5 seconds, no operation reaching the 10-second handler bound, bounded queue growth, and
   temporary consumers returning to baseline;
6. a selected graph-index worker maximum enforced by configuration validation;
7. real-NATS `[A] -> [B] -> []` proof through the watcher, keyed lane, readiness watermark, repair, restart, and
   shuffled replay, plus affected public queries and the next completed clustering cycle; and
8. fresh-state activation behind typed not-ready responses during the announced pre-v1 wipe/reseed.

Evidence collection follows the workload and resource-recording conventions in the
[`Predicate Layout Evidence Runbook`](../operations/32-predicate-layout-smoke-harness.md). Its supervised 5k and
21k results establish the selected raw PREDICATE representation; they do not complete the component-level
replacement, readiness, public-query, or clustering gates in this ADR. A store that fails correctness or an
absolute budget does not activate. It requires a separately reviewed bounded mechanism that becomes a completion
dependency.

## Relationship to the Raw Predicate Representation Decision

This ADR approves ownership and replacement independently of representation. ADR-078 subsequently selected
`predicate3.entity6`, the `*.*.*.entity6` owner filter, and PREDICATE_CATALOG retirement after the raw layout passed
its absolute representation gates. The pinned results are recorded in
[ADR-078](078-raw-canonical-predicate-membership-keys.md) and the
[`Predicate Layout Evidence Runbook`](../operations/32-predicate-layout-smoke-harness.md).

That evidence does not waive this ADR's production replacement lifecycle, readiness, public-query, restart, repair,
or clustering gates. There is no runtime layout flag, dual reader/writer, mixed-format operation, migration, or
second unannounced wipe.

## Consequences

- Replacement truth is derived from current authoritative entity state instead of additive history.
- Live-source INCOMING assertions survive target retirement and remain available to later referential policy.
- Leading-wildcard listers add measurable server work, temporary consumers, and operational signals that must remain
  inside fixed budgets.
- ALIAS and the retention fleet remain explicit follow-on work rather than being hidden behind a graph-index helper.
- A failed proof delays activation; it does not weaken `[A] -> [B] -> []`, readiness, or validation guarantees.

## Supersession and References

- Extends ADR-065's sharded membership model and ADR-066's honest readiness contract.
- Refines ADR-068 D3 as described above and supplies owner-discovery evidence to ADR-073/gh#527.
- Implements the decision requested by the
  [`graph-index-replacement-semantics` tasks](../../openspec/changes/graph-index-replacement-semantics/tasks.md).
- Physical PREDICATE representation is selected by ADR-078; this ADR remains authoritative for ownership and
  replacement activation.
