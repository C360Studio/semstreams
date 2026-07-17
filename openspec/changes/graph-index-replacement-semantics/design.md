## Context

PR #524's physical hardening stands: one membership per key, O(E) writes, no shared-list CAS, per-entity keyed
ordering with reconcile-at-execution, exact watermarks, explicit empty OUTGOING projections, typed readiness,
bounded repair. The defect is replacement, not garbage collection: NAME, PREDICATE, and source-owned INCOMING are
additive; OUTGOING and CONTEXT already replace/reconcile. The fix extends the existing reconcile pattern —
enumerate stored owner rows via an exact-arity filter, diff against the desired projection from current
ENTITY_STATES, delete stale, put missing — to the three remaining stores.

The enforced predicate and entity-ID contracts are what make the owner filters provable: no 5- or 7-part ID can
exist to alias a wildcard position, and every key/filter maximum is bounded and already unit-proven
(raw PREDICATE 451, NAME/CONTEXT 710, INCOMING 902, OUTGOING 256 bytes at `E = 256`).

The irreversible ownership and discovery choices are recorded in
[ADR-077](../../../docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md). ADR acceptance does not waive the
correctness, maximum-conformance, performance, resource, or activation gates below.

## Decisions

### 1. Preserve the PR #524 invariants

Sharding, keyed ordering, reconcile-at-execution, failure-held readiness, and watermark semantics are
non-negotiable. Nothing in this change is permission to regress them.

### 2. Ownership/filter matrix as pinned tests

Each derived store declares: token layout and arity; semantic owner; literal exact-arity forward filter (or
explicit non-filterability); literal owner filter (or explicit alternate authority); value-overwrite policy;
update/hard-delete/logical-retirement behavior; reset rule; readiness consequence. Literal filter strings, not
prose shorthand:

| Store | Layout | Arity | Owner | Owner filter |
|---|---|---:|---|---|
| PREDICATE | `predicate3.entity6` | 9 | entity | `*.*.*.entity6` |
| PREDICATE_CATALOG | retired by ADR-078 | none | none | none |
| NAME | `hash(name).entity6.hex(predicate)` | 8 | entity | `*.entity6.*` |
| CONTEXT | `entity6.hash(context).hex(predicate)` | 8 | entity | `entity6.*.*` |
| INCOMING | `target6.source6.hex(predicate)` | 13 | source assertion | `*.*.*.*.*.*.source6.*` |
| OUTGOING | `entity6` | 6 | entity | exact `entity6` |
| ALIAS | raw alias value | variable | entity (value only) | unavailable by key — separately owned |

`entity6`/`source6`/`target6` expand to six literal tokens. Every constructed key/filter passes the `nats-kv-keys`
validators before I/O. ALIAS is audited in the matrix and handed to its separate owner; it blocks nothing here.

### 3. Owner-filter proof: correctness exhaustively, performance by absolute budget

Correctness on real NATS: literal filter construction, exact match sets (no false positives, malformed
shorter/longer keys, neighboring-owner and reversed-axis controls), concurrent Put/Delete with exact-key
deduplication before diffing, cancellation, empty buckets, restart, clean bucket recreation. Concurrent-mutation
correctness is judged only after mutations advance to a declared final ENTITY_STATES revision and reconciliation
reaches that watermark: zero false matches, omissions, stale survivors, or ownership violations.

Performance by absolute budget, not comparison: the existing ADR-065 CI guard (5,000 hot members + 20 predicates,
each operation < 3s) plus one sustained-churn run on the 21k profile (full INCOMING hub, one all-entity predicate,
5,000-member NAME/CONTEXT hotspots) at the configured worker shape and one stress shape. Budgets: p95 ≤ 3s,
p99 ≤ 5s, nothing at the 10s handler bound, temporary consumers return to baseline, no unbounded queue growth.
The selected worker maximum is enforced in configuration before activation.

A store that fails correctness or budget defers to a separately specified bounded replacement mechanism — and that
mechanism becomes an explicit completion dependency of this change. Deferral never waives the
`[A] -> [B] -> []` guarantee for a query-visible store, and this change does not silently introduce a manifest or
tombstone payload.

### 4. INCOMING is source-owned evidence

A row is the source's assertion about a target. Source fact replacement retracts the former row; source
removal/tombstone retracts every source-owned row. Target logical retirement, removal, or tombstone does NOT erase
assertions still owned by live sources; query policy may classify the target absent/retired while the evidence
remains. The target-prefix hard-delete is removed, not kept as a compatibility path.

### 5. Deterministic query results

Every unordered KV- or map-derived result is deduplicated and sorted before limits/samples: entity ID ascending for
exact/value-filtered/compound/stats-sample; predicate identity ascending for predicate-list/namespace-list;
INCOMING keeps `(sourceID, predicate)`; NAME keeps its ranking tuple with entity ID as final tie-break.

### 6. Activation is the announced pre-v1 clean cutover

The wipe/reseed is already mandated by the contract changes; activation rides the same release. The old PREDICATE
and PREDICATE_CATALOG state is removed; fresh raw PREDICATE, NAME, and INCOMING buckets initialize behind typed
not-ready responses and rebuild only from freshly reseeded canonical ENTITY_STATES. Readiness stays false until
initial replay reaches the authoritative watermark. No old-key reader, dual format, migration, export, or rollback.
Generation-based maintenance rebuild remains owned by `bounded-storage-operability`.

## Risks / Trade-offs

- **Leading-wildcard owner filters may scan broadly under churn** → the churn run and absolute budgets gate
  activation; a failing store defers to an explicit dependent mechanism instead of shipping unproven.
- **Target cleanup could erase valid evidence** → INCOMING ownership is modeled by source, not physical prefix.
- **Representation cutover could expose mixed truth** → ADR-078 forbids dual format and requires a deployment-scoped
  derived-state wipe followed by a readiness-gated rebuild.

## Open Questions

- Can one filtered-list consumer serve multiple owner filters per update, or is per-request setup material? (The
  churn run answers this.)
