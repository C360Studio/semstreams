## Context

This design is a spike specification, not current deployed-product behavior. The shipped graph-index keeps PR #524's
shipped update/delete behavior until each store's owner-filter proof and ownership ADR authorize current-layout
reconciliation. It keeps the hash/catalog PREDICATE representation until the separate representation benchmark and
ADR authorize a cutover. Experimental helpers MUST NOT be wired into production before their applicable gate.
The archived `nats-kv-keys` baseline is a strict prerequisite: every new benchmark, proof, or activation key/filter
uses its validators, stable errors, and budgets before I/O rather than copying private `nats.go` regex behavior. The
`x1_` opaque codec is available only for a separately authorized new or changed axis; current untagged predicate hex
remains unchanged.

PR #524's physical hardening is valuable: one membership per key, O(E) write volume, no shared-list CAS,
per-entity ordered reconciliation, exact watermarks, explicit empty OUTGOING projections, typed readiness,
and bounded repair. Its predicate encoding rationale assumed graph-ingest accepted any non-empty predicate and that
predicate arity varied. PR #532 now enforces canonical three-part predicates at authoritative writes, with independent
graph-index replay revalidation. That invalidates the old permission rationale but does not make the defensive
physical layout itself wrong; the codec remains layout, not acceptance authority.

The immediate defect is replacement, not garbage collection: the production update path reconciles OUTGOING
and CONTEXT but additively writes NAME, PREDICATE, and INCOMING. A source that changes a relationship, name, or
predicate can therefore remain discoverable through its former membership. The production delete path removes
target-prefixed INCOMING rows while leaving the removed entity's source-owned INCOMING, NAME, and PREDICATE rows.

ADR-068/073 later inferred that entity IDs in non-prefix key positions cannot be found from a bare tombstone
and therefore require manifests or payload-rich tombstones. NATS `ListKeysFiltered` accepts fixed-position
subject wildcards. The API capability is verified; production performance and behavior under mutation are
not.

## Goals / Non-Goals

**Goals:**

- preserve PR #524's correctness and scale invariants;
- make current-state replacement true for every query-visible membership, including the empty projection;
- decide PREDICATE_INDEX representation from contract and benchmark evidence;
- reconcile stale entity-owned rows through the simplest proven NATS primitive;
- make source/target ownership explicit for INCOMING;
- emit measured owner-discovery evidence for the separate retention change;
- keep query results and readiness behavior stable across any cutover.

**Non-Goals:**

- add a general secondary-index planner;
- promise that leading-wildcard enumeration is cheap before measuring it;
- use blind TTL/MaxBytes eviction as semantic cleanup;
- make graph-index the authority for cascade or blob lifecycle.
- implement operational retention, ObjectStore reachability, stream sizing, TTL, or global GC.

## Decisions

### 1. Preserve the PR #524 invariants

Every list-valued index remains sharded one membership per key. Entity work remains ordered through the
existing keyed lane and reconciles current ENTITY_STATES at execution. A failed required write/delete keeps
the entity failed and query readiness withheld. Present entities replace OUTGOING with the complete current
array, including `[]`.

The representation study is not permission to regress these properties.

### 2. Describe every store through an ownership/filter matrix

Each derived store declares:

- token layout and fixed or explicit variable arity;
- semantic owner of the row;
- forward query filter or explicit non-filterability;
- owner reconciliation filter or explicit alternate authority;
- update, hard-delete, and logical-retirement behavior;
- clean-cutover reset rule;
- read/write budget and readiness consequence on failure.

The PR #524 contract matrix MUST use literal exact-arity NATS filters, not prose prefix shorthand:

| Store | Layout | Arity | Semantic owner | Current update |
|---|---|---:|---|---|
| PREDICATE | `hash(predicate).entity6` | 7 | entity | additive |
| PREDICATE raw candidate | `predicate3.entity6` | 9 | entity | candidate only |
| PREDICATE_CATALOG | `predicate3` | 3 | global name recovery | monotonic keys today |
| NAME | `hash(name).entity6.hex(predicate)` | 8 | entity | additive |
| CONTEXT | `entity6.hash(context).hex(predicate)` | 8 | entity | reconciled |
| INCOMING | `target6.source6.hex(predicate)` | 13 | source assertion | additive by source |
| OUTGOING | `entity6` | 6 | entity | replaced, including `[]` |
| ALIAS | `alias -> entityID` | variable | entity in value only | additive |

| Store | Exact forward filter | Exact owner filter |
|---|---|---|
| PREDICATE | `hash.*.*.*.*.*.*` | `*.entity6` |
| PREDICATE raw candidate | `predicate3.*.*.*.*.*.*` | `*.*.*.entity6` |
| PREDICATE_CATALOG | exact or `domain.*.*` / `domain.category.*` | global projection |
| NAME | `hash.*.*.*.*.*.*.*` | `*.entity6.*` |
| CONTEXT | none | `entity6.*.*` |
| INCOMING | `target6.*.*.*.*.*.*.*` | `*.*.*.*.*.*.source6.*` |
| OUTGOING | exact `entity6` | exact `entity6` |
| ALIAS | exact alias | unavailable by key |

`entity6`, `source6`, and `target6` each expand to six literal subject tokens in a constructed filter. The spike
pins both the filter string and the real-NATS match set so an accidentally broad `>` filter cannot masquerade as
a fixed-arity proof. Every key/filter newly constructed by this spike's benchmark or later activation implementation
must pass the shared NATS KV key contract before I/O. The prerequisite itself does not alter current wrapper behavior.
The implementation matrix also records value overwrite policy, source removal, target retirement, bucket reset, and
readiness effects before framework activation starts.

The current raw alias value used as the ALIAS key may contain dots and therefore span multiple physical tokens;
“exact alias” means an exact full-key lookup, not a one-token alias promise. Whether an eventual ALIAS layout
remains raw, becomes an opaque token, or gains owner discovery is a graph-index decision outside the NATS KV
prerequisite and outside this change's reconciliation scope. Its maximum is still audited, but an unbounded result
blocks only ALIAS-specific claims or changes and is handed to the separate ALIAS owner.

The matrix MUST also prove maximum token, key, filter-byte, and arity formulas for every shipped layout and filter,
not only raw Candidate B. Current PREDICATE, NAME, CONTEXT, OUTGOING, and INCOMING all embed one or two six-part
entity IDs; NAME, CONTEXT, and INCOMING also carry the up-to-388-byte existing predicate hex token. ALIAS's raw exact
key is audited even though ALIAS reconciliation remains out of scope. A representative corpus is not a maximum-bound
proof. The canonical `E = 256` contract resolves the entity semantic bound and the unit matrix proves every bounded
entity-bearing layout; pinned real-NATS maximum key/filter exact-match conformance still blocks activation. ALIAS audit
failure is recorded for its separate change and does not block unrelated stores.

#### Reviewer-approved key-contract proof checkpoint

Let `E` be total entity-ID bytes and `P` predicate bytes. The proof pins `P <= 194`, with the maximum canonical
predicate encoding to a 388-byte untagged hex token, and records these complete-key formulas:

| Layout | Maximum key bytes |
|---|---:|
| PREDICATE | `65 + E` |
| PREDICATE_CATALOG | `P <= 194` |
| NAME / CONTEXT | `E + 454` |
| INCOMING | `2E + 390` |
| OUTGOING | `E` |
| raw PREDICATE candidate | `E + 195` |

At canonical `E = 256`, the unit matrix proves PREDICATE 321, NAME/CONTEXT 710, INCOMING 902, OUTGOING 256, and raw
PREDICATE 451 bytes through the shared validators. ALIAS remains a representative unbounded raw-key audit handed to
its separate owner and does not block unrelated stores.

The benchmark-only reconciliation helper preflights complete owner filters and desired keys through the shared
validators. Invalid input returns the stable classified code/reason before lister creation, Put, or Delete. Pinned
real-NATS maximum key/filter and exact-match proof for all bounded layouts remains open. This checkpoint wires no
framework activation, reader, writer, configuration, or lifecycle call site.

### 3. Prove fixed-position enumeration on real NATS

The spike uses the production NATS client and JetStream server, not only mocks. It verifies literal filter
construction, exact matching, malformed shorter/longer keys, no false positives, duplicate handling, concurrent
Put/Delete, stale-row retraction, empty buckets, restart, repair, clean bucket recreation, and cancellation/time
budgets.

The benchmark profile is frozen before the first measured run:

- CI guard: 5,000 hot-predicate members plus 20 other predicates; each measured operation completes in less
  than 3 seconds, matching the existing ADR-065 guard.
- Full decision profile: 21,000 entities; one predicate and one INCOMING hub span all entities; NAME and
  CONTEXT each include a 5,000-member hotspot plus unique remainder values.
- Execution: five warmups followed by 30 measured repetitions per filter/candidate on the same server shape,
  plus full replay and sustained churn at representative one- and four-worker shapes, a 16-worker stress shape, and
  a preselected maximum-supported-worker candidate. The approved maximum is enforced in configuration before
  production reconciliation activates.
- Latency: p95 at most 3 seconds, p99 at most 5 seconds, and no operation reaches the 10-second handler bound.
- Resource comparison: client allocated bytes, server CPU time, and server RSS delta are each no more than
  twice a benchmark-only owner-manifest baseline over the same dataset and operation sequence. The baseline is
  not authorization to ship a manifest.
- Correctness: after concurrent mutations advance to a declared final ENTITY_STATES revision and reconciliation
  reaches that watermark, there are zero false matches, omissions, stale survivors, or ownership violations.

The run records matched and scanned keys/bytes when observable, temporary-consumer high-water and return to
baseline after success/cancellation/failure, ingest throughput, queue growth, catch-up time, client allocations,
server CPU/RSS, and end-to-end reconciliation time. It covers full fan-in/fan-out and the maximum supported
memberships for one owner. Duplicate keys may be observed during mutation but MUST be deduplicated by exact key
before diffing. The profile and environment fingerprint are versioned with the result; an unregistered or changed
profile cannot select the architecture.

### 4. Benchmark two PREDICATE_INDEX candidates

Candidate A preserves `hash(predicate).entity6` plus PREDICATE_CATALOG. It is grammar-independent but requires
catalog consistency and joins for human names/namespaces.

Candidate B uses the enforced fixed-nine-token
`domain.category.property.org.platform.domain.system.type.instance`. It supports exact predicate and
namespace filters, direct membership-key observability, entity-position reconciliation, and human-readable keys
without a catalog.

Candidate B's literal filters are:

- exact predicate: `domain.category.property.*.*.*.*.*.*`;
- `domain.category` namespace: `domain.category.*.*.*.*.*.*.*`;
- `domain` namespace: `domain.*.*.*.*.*.*.*.*`;
- entity owner: `*.*.*.<six literal entity tokens>`.

Candidate B cannot win until the maximum 194-byte canonical predicate and canonical `E = 256` entity ID pass the
literal-token, literal-key, arity, and byte budgets established by `nats-kv-keys`, including pinned real-NATS
conformance for the complete 451-byte maximum key.

The benchmark compares correctness, O(E) writes, key bytes, server/client resource use, exact and namespace
query latency, leading-wildcard cleanup, failure modes, and operational inspection. Watch behavior is compared
only if a current consumer is identified; this change does not add a public watch API to favor one candidate.
The pre-registered rubric ranks correctness, failure convergence, and ingest resource headroom first;
handler/recovery budgets and required public-query benefit follow. Hash+catalog remains when both candidates meet
requirements. Before measurement, each eligible public operation receives a numeric or mechanically decidable
material-improvement threshold. Raw keys win only when their worst-case key is bounded and they cross one of those
thresholds for a required query or proven consumer. The result is recorded in a new superseding ADR. There is no
permanent dual-write option; a selected raw format cuts over through the announced incompatible-NATS wipe and
canonical reseed, with query readiness withheld until initial replay reaches the authoritative watermark.

### 5. Keep storage codecs independent per axis

NAME and CONTEXT remain hashed because they are arbitrary/open content. INCOMING/NAME/CONTEXT predicate hex
may remain as a reversible single-token codec even after raw unsafe predicates are rejected. It is removed
only if a real query or operational requirement outweighs format churn.

PREDICATE_CATALOG raw keys must always obey the canonical predicate grammar. If Candidate A wins, catalog and
membership form one logical required projection, not a cross-bucket transaction: partial success marks the entity
failed, withholds readiness, and schedules idempotent repair. The physical catalog MAY remain a monotonic
name-recovery set, but predicate-list and namespace-list expose only names with current memberships; zero-count
historical use is not graph-query truth. Vocabulary declaration/history remains registry-owned. If Candidate B
wins, the catalog is retired after cutover.

### 6. Reconcile stored owner rows against desired projection

This section is conditional future behavior per store. Current-layout reconciliation may activate after every
current layout/filter passes the `E = 256` shared unit budgets and pinned real-NATS maximum/exact-match proof, that
store's exact owner filter passes the frozen profile, and the owner-discovery/INCOMING-ownership ADR is approved.
ALIAS remains frozen and separately owned; its unresolved bound or codec does not block unrelated stores. Activation
MUST NOT wait for the optional raw PREDICATE representation decision once those current-layout prerequisites pass.
Physical PREDICATE key/catalog changes remain inactive until their separate representation benchmark and ADR pass.

For each entity update, the index owner enumerates that entity's currently stored rows using its proven
owner filter, computes the desired projection from current ENTITY_STATES, deletes stale rows, and puts missing
rows. Results from filtered listing are deduplicated before diffing. The `[A] -> [B] -> []` transition is required
for NAME, PREDICATE, CONTEXT, and source-owned INCOMING. Acceptance is measured through graph-index-owned public
exact, namespace, compound, stats, by-name, incoming, and traversal surfaces rather than only by inspecting KV rows.
Reader-less CONTEXT is checked physically; clustering is checked after its own next completed detection cycle.

The owner-discovery ADR selects filtered reconciliation only after comparing complete lifecycle correctness and
resource headroom against the benchmark-only baseline. A store that fails correctness or any numeric budget defers
its alternate authority to a separate dependent specification; this change does not silently introduce a manifest
or tombstone payload. Deferral does not waive this change's replacement guarantee: every affected query-visible
store MUST have an approved and implemented bounded replacement mechanism before query parity can pass or this
change can archive. The dependent specification may supply that mechanism, but it becomes an explicit completion
dependency of this change.

### 7. Preserve INCOMING source ownership

An INCOMING row represents a source entity's assertion about a target. Source fact replacement retracts the
former row; source entity removal/tombstone retracts every row owned by that source. Target logical retirement,
removal, or tombstone does not erase assertions still owned by live sources; query policy may classify the target
as absent or retired while preserving that evidence. Only an authorized cascade may mutate the source fact. The
target-prefix hard-delete is removed rather than retained as a compatibility behavior.

### 8. Define deterministic query and watch semantics

Every unordered KV- or map-derived query result is sorted by canonical entity ID or predicate identity before a
limit or sample is applied. Value-filtered predicate queries sort their candidate IDs before hydration; compound
queries sort the final set before limiting. NAME ranking keeps its existing rank tuple with entity ID as the final
tie-break. Repeated identical queries against unchanged state therefore return the same ordered result.

Membership-watch behavior is decision evidence only unless an existing consumer is identified. If retained, the
semantic contract is Put when a membership is added and Delete/Purge when it is removed. No event is promised for
an entity-value change while the membership identity remains unchanged.

### 9. Make the pre-v1 activation a clean fresh-state cutover

No deployed product state must be preserved. The breaking release announces the selected layouts, updates every
owned source/configuration/fixture, wipes all incompatible authoritative and derived NATS graph resources, restarts,
and reseeds canonical sources. Current-layout reconciliation initializes PREDICATE, PREDICATE_CATALOG, NAME, and
INCOMING behind typed not-ready responses and rebuilds them only from the freshly reseeded ENTITY_STATES. If raw keys
win, their selected representation is included in the same clean release procedure rather than an online format
migration.

Graph-index starts with readiness false before canonical reseed and keeps affected queries typed not-ready until
initial replay reaches the authoritative watermark. Generation-based maintenance rebuild remains owned by
`bounded-storage-operability` tasks 5.1-5.4. No reader recognizes old keys or abandoned formats, and there is no beta
state exporter, inspector, preservation promise, compatibility reader, dual writer, or rollback path. Graph-index
query fixtures must match canonical fresh-state results, and clustering is checked only after its own next detection
cycle. Any required initial replay failure keeps reads not-ready.

## Risks / Trade-offs

- **Leading wildcard filters may scan too broadly:** make a separately specified bounded replacement authority a
  blocking dependency for any store that fails the filter proof.
- **Raw keys couple storage to grammar:** allow them only after canonical source cutover and unconditional
  enforcement are real.
- **Hash/catalog can drift:** make catalog a required repaired projection or retire it with raw keys.
- **The clean cutover discards beta state:** this is acceptable before v1 because every reference design is owned;
  publish the exact wipe/reseed commands and prove product e2e without adding a dual format or rollback path.
- **Target cleanup can erase valid evidence:** model INCOMING ownership by source, not physical prefix.

## Cutover Plan

1. Consume the archived `nats-kv-keys` baseline, then correct/complete/archive `graph-index-hardening` and seed the
   baseline graph-index spec.
2. Use canonical `E = 256` to prove every bounded in-scope current layout/filter fits the shared budgets, then complete
   pinned real-NATS maximum key/filter exact-match conformance. Audit ALIAS independently and hand its unbounded result
   to the separate owner without blocking other stores.
3. Prove current-layout exact owner filters through the shared validators without changing production behavior.
4. Complete the predicate and entity-ID clean-source/configuration/fixture gates; approve the owner-discovery and
   INCOMING-ownership ADR; update every owned reference, wipe incompatible NATS state, restart, and reseed canonical
   sources. Activate filtered reconciliation only for passing stores, first completing an approved dependent bounded
   mechanism for each failed query-visible store.
5. Prove public query, readiness, restart, repair, and lifecycle semantics on the rebuilt shipped representation.
6. Independently benchmark hash+catalog against raw PREDICATE keys after the predicate corpus is clean.
7. Retain hash+catalog unless raw has a bounded key and materially improves a required surface; record the ADR-065
   decision.
8. Only if raw wins and every owned-reference update, fresh-state product e2e, and predicate archive gate is complete,
   include the selected raw buckets in the announced clean wipe/reseed and initialize them behind readiness.
9. Remove every rejected candidate helper/reader, publish the retention evidence handoff, and run the full gates.

## Open Questions

- Can one filtered-list consumer serve multiple owner filters efficiently, or is per-request setup material?
- Does any current consumer require predicate membership watches, or only exact/namespace request-response?
- What ALIAS identity bound and raw/opaque/owner-discovery decision will its separate owning change select?
