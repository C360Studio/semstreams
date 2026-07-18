# Graph Retention v1 — Adversarial Readiness Review

**Date:** 2026-07-13  
**Status:** living evidence artifact; non-normative; pending ADR and OpenSpec adjudication  
**Reviewed baseline:** PR #524, `fix/gh474-graph-index-hardening` at `fd748005e54cf87e59424877ec40540dc90eba17`  
**Relates to:** ADR-068, ADR-073, gh#433, gh#474, PR #524, and the `graph-retention` capability

## Purpose

This document preserves the code-grounded retention and garbage-collection review that began while
reviewing PR #524. It exists so the evidence and reasoning survive chat compaction and can be consumed
by Codex, Claude, maintainers, and the eventual OpenSpec change.

This is not the implementation contract. It deliberately separates:

- **observed facts** — verified against the reviewed checkout;
- **inferences** — consequences derived from those facts;
- **recommendations** — proposed v1 boundaries and sequencing;
- **decisions** — only items explicitly accepted through ADR/OpenSpec review.

The current architecture decisions remain in:

- `docs/adr/068-graph-retention-deletion-lifecycle.md`;
- `docs/adr/073-graph-ingestion-retention-contract.md`;
- `docs/proposals/graph-retention-10-product-audit.md`;
- `openspec/specs/graph-retention/spec.md`.

The expected next governed artifact is an OpenSpec change for logical retirement and retraction. This
review should be amended as findings are adjudicated, then treated as a point-in-time evidence record.

## Executive verdict

The retention problem is real and release-significant. The current graph is not merely append-oriented;
it also leaves superseded and deleted facts discoverable through derived indexes. A credible v1 needs
logical death and retraction semantics.

The three-tier model in ADR-073 remains directionally sound:

1. firehose data is bounded by time/cardinality outside the identity graph;
2. identity state is not blindly expired;
3. owned blobs follow reachability rather than an unrelated TTL.

The full retention/GC epic is not implementation-ready as currently described. The code does not yet
provide the tombstone state, ordered retraction, durable cleanup success, participant fencing, or blob
reachability needed to make physical purge safe.

The recommended v1 cut is:

- **Retraction:** superseded and retired facts stop appearing in derived query/search surfaces.
- **Retirement:** an entity becomes a durable, idempotent logical tombstone.
- **GC compatibility:** the exact v1 contracts admit a credible future physical-reclamation algorithm,
  demonstrated before release.
- **No production physical purge in v1:** retain tombstones until the reclamation protocol is proven.

PR #524 is a prerequisite because it changes the physical key contract for three core materialized
views. It should not absorb the full retention design, but it should not merge while cementing key,
query, or ownership semantics that the retention increment must immediately reverse.

The Cassandra concern is valid. SemStreams becomes Cassandra-like if v1 takes on cluster-wide
participant membership, compaction epochs, purge floors, replica grace, repair, and physical erasure
as one release feature. It does not become Cassandra-like merely by keeping a small number of
rebuildable composite-key materialized views over authoritative KV state.

The architectural constraint is therefore:

> SemStreams owns semantic liveness and rebuildable projections. NATS owns durable storage. Physical
> reclamation remains deferred until it can be added without turning every projector into a replica in
> a home-grown distributed storage engine.

## Four distinct lifecycle operations

The existing discussion uses "delete" and "GC" too broadly. The v1 contract should distinguish four
operations:

- **Retraction (required):** a formerly supported fact or projection is no longer supported, so it
  disappears from current queries.
- **Retirement (required):** the entity is logically dead but its identity remains resolvable; direct
  reads return a typed retired result.
- **Reclamation (deferred):** physically remove unreachable state after safety proof; bytes and the
  old tombstone may disappear.
- **Hard erasure (separate contract):** deliberately make content unrecoverable, including history
  and blobs.

Retraction and retirement are graph semantics. Reclamation and hard erasure are storage lifecycle
protocols. Treating them as one operation is the main source of accidental complexity.

## Verified current-state findings

### No application tombstone exists

**Observed.** `graph.EntityState` contains live entity fields only; it has no deleting or retired state
(`graph/types.go:24-47`). `graph.DeleteEntityRequest` carries only the entity ID and tracing fields
(`graph/mutation_requests.go:31-36`). Graph-ingest performs an idempotent NATS KV delete
(`processor/graph-ingest/mutations.go:954-984`). Public lifecycle `Despawn` calls that path and
explicitly acknowledges stale derived indexes (`pkg/lifecycle/manager.go:915-932`).

**Consequence.** Existing consumers can distinguish only a KV delete marker from an entity value. A
future application tombstone will arrive as a `KeyValuePut`, so every reader and projector must
recognize it explicitly rather than relying on delete-event handling.

**Recommendation.** Define one versioned tombstone envelope and make public retirement idempotent.
Reserve raw KV purge for a future reclamation worker.

### Final entity state cannot retract historical memberships

**Observed.** PR #524 documents the concrete CONTEXT case: a transition from
`C:{p1,p2}` to `C:{p1}` leaves the old p2 composite key because the new writer only adds current
memberships (`processor/graph-index/context_index.go:36-43`). The same shape exists for old names,
old aliases, prior spatial cells, temporal buckets, and embedding content hashes.

**Consequence.** A tombstone containing only the entity's final triples cannot name memberships that
were removed before retirement. Last-known triples are not sufficient cleanup authority.

**Recommendation.** Every derived-store owner that cannot derive deletions from its current forward
shape needs a durable, exact reverse projection of the keys it last committed for that entity. The
projection should include the source revision and owner schema epoch. This is a small owner-local
manifest, not a global graph reverse index.

### Readiness watermarks are not reclamation cursors

**Observed.** `revlag.Watermark` is in-memory and records delivered work reaching a terminal return
(`pkg/revlag/watermark.go:15-39`). Graph-index completes the watermark after processing returns, not
after every derived write succeeds (`processor/graph-index/component.go:870-885`). PR #524's incoming
batch logs Put failures and may return success with zero writes
(`processor/graph-index/component.go:1311-1349`). CONTEXT also logs Put failures and returns nil
(`processor/graph-index/component.go:1671-1715`).

**Consequence.** "Caught up" is a useful query-readiness claim, but it is not proof that every
retraction was durably applied. Reusing it to authorize physical purge would permit stale projections
or resurrection.

**Recommendation.** Keep readiness and reclamation concepts separate. If physical GC is later added,
it needs a persisted successful-cleanup cursor or a durable rebuild/poison state. Do not expand
`revlag.Watermark` into a cluster GC protocol in v1.

### Updates and deletes do not share one ordered owner path

**Observed.** The graph-index watch handles delete inline while updates execute through a worker pool
(`processor/graph-index/component.go:750-804`). A delete completion may drain an older same-key
revision while its worker is still executing (`pkg/revlag/watermark.go:61-75`).

**Consequence.** Cleanup can run before an older worker writes, allowing a stale projection to be
recreated after delete or retirement.

**Recommendation.** Updates, retractions, and retirement for one entity must share a per-entity
ordered path, or every delayed write must be fenced against the current entity revision/tombstone.

### ObjectStore reachability is currently contradicted by TTL

**Observed.** Every ObjectStore is created with a hard-coded 24-hour TTL
(`storage/objectstore/store.go:110-114`), and the TTL is not configurable
(`storage/objectstore/config.go:20-50`). A live entity may therefore retain a `StorageRef` to expired
content.

**Consequence.** The owned-blob tier cannot claim reachability semantics today. Removing the TTL for
new stores alone would not reconcile existing buckets or old reference replacements.

**Recommendation.** Treat ObjectStore lifecycle as a separate contract. Logical retirement v1 must
not claim blob GC or hard erasure unless attach/swap/detach and store configuration are fixed and
proven independently.

## PR #524 — contract impact and merge boundary

PR #524 changes INCOMING, NAME, and CONTEXT from aggregate values to one KV row per membership. The
direction is appropriate for write amplification: independent facts no longer contend on one shared
JSON list. The change is not inherently overengineered; it is normal materialized-view normalization.

The risk is that it encodes semantic axes directly into physical NATS keys and then reconstructs wire
facts from those keys. That makes validation, ordering, cutover, and ownership part of the observable
query contract.

### Merge blockers that belong in PR #524

#### Encode every free-form predicate axis

**Observed.** The new keys append the raw predicate:

- INCOMING: `targetID.sourceID.predicate` (`processor/graph-index/incoming_index.go:18-35`);
- NAME: `hash(name).entityID.predicate` (`processor/graph-index/name_index.go:41-50`);
- CONTEXT: `hash(context).entityID.predicate` (`processor/graph-index/context_index.go:26-45`).

Their validators reject an empty predicate but do not enforce NATS KV key safety
(`incoming_index.go:83-106`, `name_index.go:93-110`, `context_index.go:61-75`). Graph-ingest accepts
any non-empty predicate (`processor/graph-ingest/component.go:2591-2600`).

**Required correction.** Use one stable encoding or hash for the predicate token and preserve the raw
predicate in the row value for reconstruction. Add a real-NATS test using a semantically accepted but
KV-unsafe predicate. Keep six-part entity IDs raw and validated.

#### Make write failure observable

**Observed.** INCOMING and CONTEXT can convert storage failure into success, while the readiness
watermark advances on return.

**Required correction.** Return partial/zero-write failures, retry transient failures through the
owner path, and prevent readiness from asserting authoritative query coverage after failed writes.
Per-key cleanup must follow the same rule.

#### Make incoming query order deterministic

**Observed.** Incoming query reconstruction appends `KeysByPrefix` results without sorting
(`processor/graph-index/query.go:145-177`). PathRAG caps traversal and therefore may select a
different neighbor when storage/replay order changes.

**Required correction.** Sort by `(FromEntityID, Predicate)` before returning the wire response and
test capped incoming/both traversal across replay.

#### Close the breaking-format rebuild window

**Observed.** Startup starts the ENTITY_STATES watcher and then immediately registers query handlers
(`processor/graph-index/component.go:549-566`). Old aggregate keys are deliberately inert, so an
upgrade answers from an empty-to-partial new index until replay catches up.

**Required correction.** Gate index query availability on bootstrap completion/readiness, or return an
explicit not-ready response that every consumer honors. A documented but unenforced eventual window
is insufficient for a breaking storage cutover.

#### Bound NAME lookup amplification

**Observed.** `byName` lists every matching membership and performs a serial Get for each before
ranking and applying the caller's limit (`processor/graph-index/name_index.go:205-279`).

**Required correction.** Add a hard scan/read budget, pagination, bounded parallelism, or a storage
layout that supports early termination. Prove high-fan-in behavior against real NATS.

#### Preserve source-supported INCOMING evidence

**Observed.** An INCOMING row `(target=A, source=B, predicate=p)` exists because B currently asserts
`B-p->A`. The current delete path removes the entire `A.*` prefix when A is deleted
(`processor/graph-index/component.go:1475-1527`). It leaves rows where A is the source because A is
the middle token.

**Inference.** The cleanup ownership is backwards for logical retirement. B owns the relationship
fact. Retiring A does not retract B's assertion. Removing `A.*` destroys evidence needed to inspect,
repair, refuse, or cascade the remaining reference. Retiring B should remove peer INCOMING rows whose
source is B.

**Required correction.** PR #524 should not enshrine target-prefix deletion as the correct owner
contract. Either:

1. change delete behavior to preserve target-side incoming rows and clearly identify the current raw
   delete as legacy semantics; or
2. explicitly quarantine the delete behavior behind the upcoming retirement contract, with tests and
   design text that do not call target-prefix deletion complete semantic cleanup.

The preferred contract is source ownership:

- B owns `OUTGOING[B]`;
- B owns every INCOMING row with `source=B`;
- B owns its NAME, PREDICATE, ALIAS, CONTEXT, spatial, temporal, embedding, and OASF memberships;
- retiring B retracts B-supported rows;
- retiring target A preserves live B-supported incoming rows until B changes, retires, or a future
  cascade explicitly mutates B.

#### Do not ship knowingly accreting CONTEXT semantics

**Observed.** The new CONTEXT writer fixes the old lost-update race but stops retracting superseded
memberships. CONTEXT has no production reader today.

**Required correction.** Preserve source-owner reconciliation in this PR, or stop populating CONTEXT
until the retention/retraction increment. A write-only index that knowingly accumulates false current
facts is not a useful v1 contract.

#### Make measurement claims honest

**Observed.** The D6 no-op projection counters are private atomics, not Prometheus metrics
(`processor/graph-index/metrics.go:11-89`). The projection omits alias values even though alias writes
are part of the indexing path. The in-memory projection baseline is stored before the derived writes
complete, so a failed write can still become the comparison baseline for a later update.

**Required correction.** Expose the promised changed/unchanged measurement and include every indexed
axis, or remove/narrow the OpenSpec claim. This is not the retention protocol, but it is the evidence
gate used to justify later change detection.

### Work that should not be pulled into PR #524

The following belong in the retention OpenSpec change:

- application tombstone wire/state shape;
- retire/rebirth/restore semantics;
- target-write rejection after retirement;
- durable per-owner exact-key reverse manifests;
- ordered update/retraction/retirement processing;
- tombstone-aware search and enumeration;
- successful-cleanup cursors and recovery behavior;
- cascade, purge, blob GC, or mark-sweep.

PR #524 should establish safe and bounded materialized-view key/query contracts. Retention should
establish logical death and retraction. Combining both would enlarge an already high-risk diff and
make causal review harder.

## Minimal architecture that preserves the KV Twofer

The KV Twofer remains the desired model:

- **State:** `ENTITY_STATES[entityID]` is the current authoritative entity fact.
- **Events:** projectors react to the same KV write through Watch.
- **History/recovery:** Watch bootstrap replays current authoritative values and rebuilds projections.

Logical retirement fits this model: retirement is one durable ENTITY_STATES value transition, and
each projector reacts idempotently. No second retirement event bus is required.

The primitive split follows the restart/fan-out/side-effect tests:

| Concern | Primitive | Reason |
|---|---|---|
| Live or retired entity state | KV value + Watch | Current fact; all owners must observe; replay is correct |
| Owner-local retraction | KV Watch reaction | Fast, idempotent materialized-view maintenance |
| Query/index readiness | KV/current status | Current observable fact |
| Future cascade/referrer repair | JetStream work item | One coordinator; do not replay completed work |
| Cascade progress | KV lifecycle state | Queryable, restart-resumable current status |
| Future purge eligibility | KV fact | Durable safety decision, not an ephemeral command |

If cleanup needs a queue because it is slow or retryable, split the concepts: the tombstone remains a
KV fact, while a durable work item references the entity/revision. Do not replace the authoritative
fact with a command stream.

### Key-contract rules

1. The primary entity key remains the raw canonical six-part ID.
2. Raw six-part IDs may remain visible on secondary-index axes when validated.
3. Free-form axes such as name, context, and predicate use stable opaque tokens in physical keys.
4. Original semantic values ride in row values when queries must reconstruct them.
5. Wire/query response types do not expose the physical storage layout.
6. Every materialized view is rebuildable from authoritative state plus its explicit owner manifest.
7. A derived index is never authoritative for entity existence.
8. Each row has one semantic supporter/owner; cleanup follows that owner, not convenient key prefix.
9. Key schemas are versioned or cut over behind explicit readiness; no silent partial serving.
10. Prefer one small shared token codec over a generic indexing DSL.

These rules preserve simple semantic lookup. Composite secondary indexes are the KV equivalent of a
small set of SQL indexes; they are not a second source of graph truth.

## Complexity budget — avoiding a home-grown Cassandra

The v1 design should pass this test:

> Can one engineer explain entity retirement as one authoritative state transition plus independent,
> rebuildable owner retractions, without explaining a cluster-wide compaction protocol?

If yes, the design remains SemStreams-shaped. If no, the scope has crossed into storage-engine work.

### Allowed in logical-retirement v1

- one versioned tombstone representation;
- one CAS/fence at the authoritative graph-ingest write boundary;
- one owner-local exact reverse manifest where current state cannot express removals;
- per-entity ordering or revision fencing inside each owner;
- rebuild-before-ready behavior;
- explicit query semantics for live versus retired entities;
- deterministic, bounded query results;
- tests proving no stale result or resurrection.

### Explicitly deferred machinery

- cluster-wide GC participant election or leases;
- automatic dead-participant eviction;
- purge-floor negotiation in production;
- replica-style repair protocols;
- compaction epochs spanning all derived stores;
- cascade sagas and cycle detection;
- global mark/sweep;
- ObjectStore refcount GC and hard-erasure certification;
- physical tombstone deletion.

These may become justified later, but none should be smuggled into logical retirement as a helper.

### Warning signs that should stop the design

- a query needs to understand multiple physical key versions indefinitely;
- an index becomes authoritative because its owner manifest is incomplete;
- a tombstone needs every historical entity value to clean derived state;
- readiness is reused as durability proof;
- a failed owner is silently removed from a purge decision;
- an application-level delete depends on wall-clock grace alone;
- every new projector must join a global GC consensus protocol;
- a convenience prefix dictates semantic ownership.

## Recommended logical-retirement v1 contract

### Retire

1. Validate authority and expected predecessor revision at graph-ingest.
2. CAS the live entity to a versioned retired tombstone.
3. Preserve the stable entity ID and the revision from which it retired.
4. Make repeated retirement idempotent without rewriting the tombstone.
5. Reject ordinary update/rebirth while the tombstone exists unless an explicit restore contract is
   later accepted.
6. Reject new operational edges targeting a retired entity.

The assigned KV revision comes from the write result; it cannot be embedded in the value before NATS
assigns it. The value may carry `RetiredFromRevision`, operation identity, reason, and audit metadata.

### Retract

Each derived owner processes the tombstone through the same entity-ordered path as updates:

1. read its durable manifest for the entity;
2. remove the exact source-supported keys it previously committed;
3. preserve target-side incoming references supported by other live entities;
4. commit the new empty/retired manifest only after forward cleanup succeeds;
5. retry on transient failure without advancing successful cleanup state;
6. fence delayed work against the tombstone/current revision.

### Query

- Direct entity read returns a typed retired result rather than ordinary not-found.
- Enumeration, name lookup, alias lookup, predicate search, spatial search, temporal search,
  embeddings, OASF, and clustering exclude retired entities.
- Incoming-reference inspection may still show live entities that reference the retired identity.
- Query readiness never claims success after a failed required projection write.

### Restore

Restore/rebirth is an open decision. The safe v1 default is no ordinary rebirth while a tombstone
exists. If restore is required, it needs a distinct authorized mutation with a new epoch and explicit
projector rebuild semantics.

## GC compatibility architecture gate

Deferring production GC is safe only if v1 proves it has not made future GC impossible. Before v1,
demonstrate a small compatibility spike with the exact proposed contracts.

### Required invariants

- Tombstones are versioned, stable values and cannot be silently overwritten.
- Entity IDs are not reused while a tombstone or purge fence exists.
- Every correctness-sensitive derived owner has a stable owner ID and schema epoch.
- Every owner can name its exact contributions or can clear and rebuild safely.
- A successful cleanup marker means durable side effects succeeded, not merely that processing
  returned.
- A persisted purge floor can later fence a returning stale owner.
- An owner below that floor clears/rebuilds before serving.
- All graph writes pass through the retirement fence.
- Blob attach/swap/detach is explicit before blob reclamation is attempted.
- Absence, retirement, reclamation, and hard erasure remain distinct outcomes.

### Compatibility spike

Use one tombstone and two mock derived owners:

1. retire entity E at revision R;
2. keep owner B offline while owner A retracts and checkpoints beyond R;
3. prove physical purge remains ineligible;
4. restart B and let it retract/checkpoint;
5. persist a candidate purge floor and purge the test tombstone;
6. restart a stale owner below the floor;
7. prove it must clear/rebuild before ready;
8. prove no retired projection or entity can resurrect.

This is a model/integration proof, not authorization to ship production GC in v1.

## Release proof gates

The logical-retirement change should not ship without deterministic tests for:

- application tombstone delivery as a KV Put;
- duplicate retirement requests;
- update-before-retire reordering;
- blocked graph-index worker released after retirement;
- blocked embedding generation released after retirement;
- source relationship removal and source retirement;
- target retirement preserving still-supported incoming evidence;
- old name, predicate, alias, context, spatial, temporal, embedding, and OASF membership removal;
- failed forward cleanup preserving reverse knowledge and withholding success;
- owner restart and current-state reconciliation;
- query/search exclusion of retired entities;
- clustering rebuild that never publishes a partial generation;
- full ingest → retire → every query surface e2e;
- race-enabled integration tests without arbitrary sleeps.

Because this is a breaking graph lifecycle contract, relevant structural and semantic e2e tiers must
be green before merge.

## Decision ledger

| Topic | Current status | Proposed resolution |
|---|---|---|
| Preserve this review in the repo | Accepted | This document |
| Retraction vs retirement vs reclamation | Recommended | Separate contracts and release gates |
| v1 physical GC | Recommended non-goal | Retain tombstones; prove compatibility only |
| Tombstone vs KV delete | Recommended | Versioned application value; raw delete is legacy/purge |
| Cleanup authority | Recommended | Per-owner exact reverse manifests, not final triples |
| INCOMING ownership | Recommended | Source entity owns the supported relationship row |
| Strict refuse | Open | Do not advertise until a delete-intent fence is designed |
| Cascade | Recommended non-goal | Separate durable workflow after predicate ownership policy |
| Restore/rebirth | Open | Default deny in v1 unless explicit contract is accepted |
| ObjectStore GC | Recommended separate epic | Fix TTL and attach/swap/detach before reclamation |
| PR #524 | Block pending corrections | Safe keys, honest writes/readiness, deterministic queries |

## Dependency order

1. Correct and merge PR #524's materialized-view key/query contract.
2. Adjudicate this review's recommended decisions.
3. Amend proposed ADR-073 and mark conflicting ADR-068 passages as amended.
4. Create `openspec/changes/graph-retention-logical-retirement/`.
5. Specify tombstone, mutation fence, owner manifests, query semantics, and proof gates.
6. Implement one owner end-to-end under TDD and validate the pattern.
7. Migrate every correctness-sensitive owner.
8. Run the GC compatibility spike against the exact shipped contracts.
9. Ship logical retirement only after cross-owner integration and e2e proof.
10. Design physical reclamation as a separate epic if operational evidence justifies it.

## References

- `docs/proposals/graph-retention-10-product-audit.md`
- `docs/adr/068-graph-retention-deletion-lifecycle.md`
- `docs/adr/073-graph-ingestion-retention-contract.md`
- `openspec/specs/graph-retention/spec.md`
- `openspec/changes/graph-index-hardening/`
- PR #524: <https://github.com/C360Studio/semstreams/pull/524>
- PR #524 review comment: <https://github.com/C360Studio/semstreams/pull/524#issuecomment-4957325203>
- gh#433: derived-index cleanup gap
- gh#474: graph-index write amplification
