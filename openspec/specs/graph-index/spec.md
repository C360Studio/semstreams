# graph-index Specification

## Purpose

Defines the **derived index layer** (`processor/graph-index`) — the KV-backed indexes that make
canonical `ENTITY_STATES` queryable by axes the authoritative store cannot answer directly:
predicate membership, names, and incoming references.

These indexes are **derived, never authoritative**. Canonical entity state is the source of
truth; an index is a projection that must be reconstructible from it, and any disagreement is
resolved by rebuilding the index rather than by trusting it.

Four postures recur across these requirements and are load-bearing:

- **One sharded key per membership, never a monolithic list.** A list-valued row would make
  every membership change a read-modify-write on a shared key, which is both a contention point
  and a lost-update hazard. Sharded keys make membership changes independent.
- **Readiness is authoritative and consumers fail closed.** An index that has not finished
  rebuilding returns a typed not-ready response rather than a partial answer. Partial index
  results are indistinguishable from correct small results, so serving them silently converts a
  startup window into wrong answers.
- **Ownership is declared, and retraction belongs to the owner.** A derived row is retracted by
  the source that asserted it, not by a target-prefix sweep — so an entity cannot delete
  assertions it does not own.
- **Format cutover is a fresh-state release contract, not an upgrade path.** Buckets rebuild
  from freshly reseeded canonical state behind typed not-ready; no reader recognizes old keys,
  and no dual format, migration, export, preservation, or rollback is provided.

Predicate membership keys are **raw canonical predicates** (ADR-078), which superseded the
earlier hash-plus-catalog design; `PREDICATE_CATALOG` and its consistency/repair machinery were
retired with it.

### What this capability does NOT cover

- **It does not own entity state.** Writes to `ENTITY_STATES`, per-predicate merge, and
  owner-lease enforcement belong to `graph-ingest` and `graph-state-contract`.
- **It does not decide retention or deletion policy.** The measured owner-discovery matrix is
  published as input to the retention epic (ADR-068 / gh#527); selecting a policy is not done
  here, and the live graph never uses NATS TTL/MaxBytes/MaxAge.
- **It does not define query surfaces or result shaping.** Ordering, deduplication, limits, and
  the wire response types live in `graph-query` and the gateway specs.
- **It does not provide a predicate-membership watch.** No consumer watches these buckets;
  watch behaviour is a non-public operational property, not a contract.
- **Activation of reconciliation is gated, and the gate is not discharged here.** The pre-v1
  coordinated wipe/reseed that carries it is tracked as an operational event (gh#827), and the
  halt-if-the-window-closed rule is a requirement, not a runbook footnote.
## Requirements
### Requirement: The index buckets rebuild from entity state on boot after the format cutover

The graph-index component MUST repopulate correctly-formatted sharded keys from `ENTITY_STATES` on
startup via the existing KV-watch replay, keeping each bucket's name unchanged (no rename, to avoid
the operator-config lockstep problem). Old monolithic-format keys MUST be structurally inert under
the new prefix reads, so no custom migration or dual-read code is required.

#### Scenario: old monolithic keys are inert after cutover

- **GIVEN** a bucket holding old monolithic-list keys from before the change
- **WHEN** the new code reads a dimension via the prefix scan
- **THEN** the old bare key is not matched and does not corrupt the result

### Requirement: Re-index churn is instrumented to gate the change-detection follow-up

The graph-index component MUST record, per re-index event, whether the entity's index-input
projection (the set-projections each index consumes) actually changed from what was last indexed —
exposed as a metric — so the effectiveness of a future skip-if-unchanged optimization can be
measured rather than assumed. This instrumentation MUST NOT itself skip any write (it observes
only); it is the data gate for the deferred change-detection work.

#### Scenario: a no-op re-index is counted as unchanged

- **GIVEN** an entity is re-indexed with an index-input projection identical to its last index
- **WHEN** the re-index event is processed
- **THEN** the no-op counter increments
- **AND** the index writes still occur (instrumentation does not change behavior)

### Requirement: Readiness is authoritative and consumers fail closed on incomplete indexes

Graph-index MUST NOT advertise readiness, and its reverse-index query/traversal/clustering consumers
MUST NOT return a successful result, while the index is known-incomplete — whether because it is
still building (cutover / cold replay) or because a required index write or delete has failed and not
yet been repaired.

- **Failure honesty.** A required index write (incoming/name/alias/predicate/outgoing) or a
  required delete that ultimately fails after bounded retry MUST return failure to the entity-work
  reconciler and mark the entity failed; it MUST NOT be treated as successful completion merely
  because the asynchronous watcher continues. The re-index no-op baseline MUST be stored only after
  a successful write, so a failed entity re-attempts rather than being suppressed as a no-op.
- **Authoritative OUTGOING replacement.** Every successful reconciliation of a present authoritative
  `ENTITY_STATES` entity MUST replace `OUTGOING[entityID]` with the entity's complete current
  relationship array, including an explicit empty array when it has no relationships. Only
  authoritative `ENTITY_STATES` absence MUST delete the owner key. Explicit empty values are bounded
  by live-entity cardinality and MUST prevent historical relationship churn from leaving phantom
  outgoing query results.
- **Readiness withholding.** While any entity is in the failed set, the published readiness envelope
  (`GRAPH_STATUS` KV, ADR-083) MUST report not-ready, and the `INCOMING`, `OUTGOING`, `byName`,
  `ALIAS`, and every `PREDICATE` query handler MUST return a typed `index_not_ready` error —
  including after initial bootstrap has completed (a sticky "bootstrapped" flag MUST NOT mask a
  later failure).
- **Consumer propagation.** Consumers that read the reverse indexes — PathRAG traversal, the graph
  query client's incoming reads, and graph-clustering's community detection — MUST honor the
  not-ready signal (abort/defer) rather than convert it into an empty-but-successful result. Direct
  bucket consumers MUST fail closed when status is unavailable or malformed by default. They MAY
  expose `allow_ungated_reads` only as an explicit standalone/test deployment opt-out.
- **Protocol completeness.** PathRAG MUST propagate request and JSON decode failures and MUST reject
  a syntactically valid response whose required `relationships` field is absent. A zero-length
  relationships array is a valid empty result; direction `both` MUST fail if either leg fails.
- **Durable recovery.** A failed entity MUST be retried by a background repair loop (not only on the
  next incidental event for that entity), so a transient backend outage self-heals and readiness is
  restored once the failed set drains. Updates, deletes, coalesced events, and repair MUST use the
  SAME hash-keyed FIFO dispatch per entity, with concurrency permitted across entities. Every work
  item MUST reconcile authoritative `ENTITY_STATES` when it executes, so stale queued work cannot
  clobber a newer write or resurrect a deleted entity. Each repair attempt and its write retries
  MUST remain bounded and fail closed;
  gh#527 retains semantic retention/manifest/retraction scope, not ordering correctness.
- **Exact watermark completion.** Coalescing MUST retain the greatest delivered revision for each
  pending entity and MUST complete the watermark for the exact revision represented by a detached
  batch or dispatched event. Initial enumeration is not processing completion: for a non-empty
  graph, readiness MUST wait until the watermark reaches the query-time ENTITY_STATES target.
- **Status-handle isolation.** Query-time ENTITY_STATES target revision reads MUST use a dedicated
  KV handle rather than sharing the watcher/Get handle, so concurrent status evaluation does not
  race the NATS client's cached stream-info state.
- **Empty graph.** An authoritatively empty graph (initial enumeration complete, 0/0) MUST become
  ready, so a fresh empty graph does not reject every query forever.

#### Scenario: a post-bootstrap write failure withholds readiness

- **GIVEN** the index has bootstrapped and is serving queries
- **WHEN** a required index write for an entity subsequently fails after retry
- **THEN** the published readiness envelope (`GRAPH_STATUS` KV) reports not-ready
- **AND** an `incoming` or `byName` query returns `index_not_ready`, not a partial result

#### Scenario: a failed delete does not advertise ready

- **WHEN** an entity delete's required index cleanup fails
- **THEN** the entity remains marked failed and readiness is withheld until the cleanup succeeds

#### Scenario: a traversal aborts rather than returning a partial path set

- **GIVEN** the index is not ready
- **WHEN** a PathRAG traversal requests incoming relationships
- **THEN** the traversal returns the not-ready error rather than a truncated/empty path set

#### Scenario: stale queued work reconciles current entity truth

- **GIVEN** an older update is queued before a newer update or delete for the same entity
- **WHEN** both execute through the entity's keyed FIFO lane
- **THEN** each operation reads current `ENTITY_STATES` at execution
- **AND** the older work cannot overwrite the newer state or resurrect a deleted entity

#### Scenario: removing the final relationship clears the outgoing projection

- **GIVEN** a present entity previously had an outgoing relationship
- **WHEN** its authoritative `ENTITY_STATES` value is reconciled with no relationships
- **THEN** `OUTGOING[entityID]` is replaced with an explicit empty array
- **AND** outgoing queries return no phantom relationship from the former projection
- **AND** the owner key remains present until authoritative `ENTITY_STATES` absence

#### Scenario: a structurally incomplete PathRAG response is not an empty graph

- **WHEN** an outgoing or incoming handler returns valid JSON without a `relationships` field
- **THEN** PathRAG returns a protocol error rather than a successful empty relationship set

#### Scenario: an empty graph becomes ready

- **GIVEN** a fresh graph with no entities and initial enumeration complete
- **WHEN** readiness is evaluated
- **THEN** the index reports ready and reverse-index queries are served

#### Scenario: a byName lookup over a huge shared name is bounded

- **WHEN** a `byName` lookup would hydrate more memberships than the read budget
- **THEN** it returns a typed `resource_exhausted` error rather than an unbounded serial scan

### Requirement: Fixed-position owner filtering is proven before production reconciliation activates

PREDICATE, NAME, and source-owned INCOMING MUST be tested against real NATS using literal exact-arity
forward and owner filters constructed through the `nats-kv-keys` contract. The proof MUST cover filter-string
construction, malformed longer/shorter keys, matching correctness with no false positives, neighboring-owner and
reversed-axis controls, concurrent Put/Delete with exact-key deduplication, cancellation, empty buckets, restart,
and clean bucket recreation. Concurrent-mutation correctness MUST be evaluated only after mutations advance to a
declared final ENTITY_STATES revision and reconciliation reaches that watermark, with zero false matches,
omissions, stale survivors, or ownership violations.

Performance MUST be gated by absolute budgets, not comparison: the ADR-065 CI guard (5,000 hot members, each
operation under 3 seconds) and one sustained-churn run on the 21,000-entity profile at the configured worker shape
and one stress shape, achieving p95 at most 3 seconds, p99 at most 5 seconds, no operation at the 10-second
handler bound, temporary consumers returning to baseline, and no unbounded queue growth. The selected worker
maximum MUST be enforced in validated configuration before activation.

A store that fails correctness or budget MUST defer its cleanup authority to a separately specified bounded
replacement mechanism; that mechanism becomes a completion dependency of this change, and deferral MUST NOT waive
the required `[A] -> [B] -> []` result for any query-visible store.

#### Scenario: a source entity enumerates only its INCOMING assertions

- **GIVEN** INCOMING rows for multiple targets and sources
- **WHEN** the source-axis fixed-position filter is evaluated for one six-part source ID
- **THEN** every matching row is owned by that source assertion
- **AND** no row owned by another source is returned

#### Scenario: unit maxima do not replace real-NATS proof

- **GIVEN** canonical six-part entity IDs are bounded and every entity-bearing unit maximum fits shared budgets
- **WHEN** production activation is evaluated
- **THEN** unit arithmetic and representative data do not authorize activation
- **AND** activation waits for pinned real-NATS maximum key/filter exact-match conformance

### Requirement: INCOMING rows are retracted by their source owner

An INCOMING row MUST be treated as evidence owned by its source entity. Source fact replacement and source entity
removal/tombstone MUST retract stale source-axis rows. Logical retirement, removal, or tombstone of the target MUST
NOT delete assertions still owned by live sources merely because the target occupies the physical key prefix. The
target-prefix hard-delete behavior MUST be removed rather than preserved as a compatibility path.

#### Scenario: target lifecycle preserves live source evidence

- **GIVEN** a live source still asserts a relationship to a target
- **WHEN** the target is logically retired, removed, or tombstoned without changing the source fact
- **THEN** the source-owned INCOMING assertion remains available to retention/query policy
- **AND** it is removed only when the source retracts it or an authorized cascade changes the source fact

#### Scenario: source removal retracts reciprocal rows

- **GIVEN** one source owns INCOMING assertions across several targets
- **WHEN** the source entity is removed or tombstoned
- **THEN** every row owned by that source is retracted through the selected bounded source-owned mechanism
- **AND** unrelated source assertions to the same targets remain

### Requirement: Activation is gated and starts from canonical fresh state

Production replacement and INCOMING lifecycle behavior MUST remain the documented shipped behavior until the
owner-filter proof passes and the owner-discovery/INCOMING-ownership ADR approves each store's mechanism. When
reconciliation activates for the stable release, every downstream adoption MUST start on newly provisioned NATS
storage. Owned sources, configurations, schemas, fixtures, and queries MUST already use the selected layout.

Fresh raw PREDICATE_INDEX, NAME_INDEX, and INCOMING_INDEX buckets MUST initialize from current ENTITY_STATES behind
typed not-ready responses. Readiness MUST remain false until cold replay reaches the authoritative watermark. Retired
PREDICATE_CATALOG and CONTEXT_INDEX buckets MUST be absent. No reader SHALL recognize old keys, and no dual format,
compatibility alias, persistence migration, preservation, online conversion, or rollback path SHALL be provided.
Discovery of retained deployed NATS state MUST stop that adoption and require a separate owner-reviewed migration or
recovery design.

Typed poison recovery and projection rebuild from current authoritative ENTITY_STATES remain unchanged; this release
premise does not convert recovery into a release operation.

#### Scenario: a spike cannot silently activate reconciliation

- **GIVEN** benchmark-only helper code exists in the graph-index package
- **WHEN** its applicable proof or ADR gate is still open
- **THEN** production entity updates and deletes retain the documented shipped behavior
- **AND** no configuration flag or implicit default can activate the candidate path

#### Scenario: selected-layout activation starts from canonical fresh state

- **GIVEN** owned sources, configurations, schemas, fixtures, and queries use the selected layout
- **AND** the downstream provisions new NATS storage with no retired graph buckets
- **WHEN** graph-index starts and cold replay begins
- **THEN** affected queries return typed not-ready until replay reaches the authoritative watermark
- **AND** ready projections contain only current raw-layout truth
- **AND** no old key or compatibility path is read

#### Scenario: fresh start has no premature-ready window

- **GIVEN** newly provisioned NATS storage and graph-index starts before canonical source replay completes
- **WHEN** initial authoritative replay is incomplete
- **THEN** graph-index readiness remains false
- **AND** affected queries remain not-ready until the initial authoritative watermark is reached

#### Scenario: Retained state stops adoption

- **GIVEN** a downstream intends to adopt the stable release
- **WHEN** retained deployed NATS graph state is discovered
- **THEN** adoption stops before the new release is started against that state
- **AND** a separate owner-reviewed migration or recovery design is required
- **AND** the release does not wipe, reseed, translate, preserve, or roll back that state

### Requirement: Predicate membership keys are raw canonical predicates

PREDICATE_INDEX MUST adopt the fixed-nine-token layout
`domain.category.property.org.platform.domain.system.type.instance`, preserving one membership per key and O(E)
writes. PREDICATE_CATALOG MUST be retired. The complete worst-case key and every constructed filter MUST pass the
`nats-kv-keys` budgets and pinned real-NATS maximum/exact-match conformance. Production activation MUST additionally
pass the replacement lifecycle, CI, sustained-churn, restart, and resource gates. A server, SDK, predicate grammar,
entity-ID bound, or layout change MUST rerun the affected evidence before release. Hash-plus-catalog measurements
MAY be retained as comparative evidence but MUST NOT function as a selection threshold or activation fallback.

#### Scenario: raw keys ship after activation gates pass

- **GIVEN** the worst-case raw key and all filters pass budgets and real-NATS conformance
- **WHEN** the lifecycle fixtures and churn run pass on the raw layout
- **THEN** PREDICATE_INDEX is created with raw nine-token membership keys
- **AND** PREDICATE_CATALOG is absent after cutover

#### Scenario: a changed evidence pin cannot inherit the decision run

- **GIVEN** the selected NATS server digest, `nats.go` version, grammar bound, or physical layout changes
- **WHEN** release evidence is evaluated
- **THEN** the affected maximum, correctness, latency, churn, and resource gates are rerun
- **AND** prior measurements alone cannot activate the changed implementation

#### Scenario: namespace filters do not over-match

- **GIVEN** predicates `alpha.beta.x` and `alpha.betax.y` have current memberships
- **WHEN** the `alpha.beta` namespace filter is evaluated
- **THEN** only `alpha.beta.x` memberships are returned

### Requirement: A key-format cutover never serves mixed partial truth

The selected representation MUST start on newly provisioned NATS storage. Fresh raw buckets MUST initialize behind
typed not-ready responses from canonical ENTITY_STATES, and readiness MUST stay false until initial replay reaches
the authoritative watermark. SemStreams MUST NOT operate a permanent dual-format predicate index, recognize
old-format keys, or provide export, preservation, in-place migration, or rollback. Discovery of retained deployed
state MUST stop the affected adoption for separate owner review.

#### Scenario: cutover holds readiness until the watermark

- **GIVEN** the raw representation starts with newly provisioned NATS storage
- **WHEN** canonical source replay is incomplete
- **THEN** predicate queries return typed not-ready rather than partial results
- **AND** once ready they match canonical fresh-state fixtures without reading abandoned keys

#### Scenario: a closed window halts the cutover

- **GIVEN** retained deployed graph state is present
- **WHEN** adoption is evaluated
- **THEN** the adoption stops
- **AND** no second format reader, automatic deletion, translation, or migration is activated

### Requirement: Non-predicate codecs keep their recorded rationale

NAME MUST keep fixed-width hashed keys for its open-content name axis, with the original case and priority retained in
the small per-membership value. NAME and INCOMING MAY keep reversible `hex(predicate)` in their composite reverse-index
layouts so readers can reconstruct the accepted predicate without a catalog lookup.

`PREDICATE_INDEX` MUST NOT use either codec: its fixed nine-token membership key is the raw canonical three-part
predicate followed by the canonical six-part entity ID, and `PREDICATE_CATALOG` is absent. No codec, hash, or physical
key representation MAY be treated as acceptance authority for a predicate that violates the canonical grammar.
This requirement documents the shipped layout only and SHALL NOT trigger a runtime index migration.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded or hashed into a KV-safe token but violates canonical syntax
- **WHEN** graph state or a derived membership is written
- **THEN** predicate validation rejects it before graph-index I/O

#### Scenario: each surviving codec stays local to its axis

- **WHEN** NAME, INCOMING, and PREDICATE membership layouts are inspected
- **THEN** NAME hashes its normalized name axis, NAME and INCOMING use reversible predicate hex, and PREDICATE uses the
  raw canonical predicate
- **AND** no shared predicate catalog or codec authority is inferred

### Requirement: Context provenance remains authoritative without a durable context index

Graph-index MUST NOT create, open, write, reconcile, or publish readiness for a CONTEXT_INDEX bucket. Triple
provenance MUST remain stored on each authoritative triple through `Triple.Context` in ENTITY_STATES; retiring the
derived bucket MUST NOT erase or rewrite that fact.

Stable-release adoption MUST use newly provisioned NATS storage on which CONTEXT_INDEX is absent. No legacy bucket
reader, alias, translation, dual write, or online migration is provided. Retained deployed state stops adoption for
separate owner review; typed poison recovery remains separately scoped.

#### Scenario: fresh graph-index startup creates no context bucket

- **GIVEN** newly provisioned NATS storage with no CONTEXT_INDEX
- **WHEN** graph-index starts and reaches readiness
- **THEN** CONTEXT_INDEX remains absent
- **AND** the surviving query indexes initialize normally

#### Scenario: hierarchy provenance remains on authoritative triples

- **GIVEN** a hierarchy triple with canonical context provenance
- **WHEN** the authoritative entity state is stored and replayed
- **THEN** the triple retains its `Triple.Context`
- **AND** no durable context index is needed to preserve provenance

### Requirement: INCOMING and NAME memberships use one sharded key per membership

Each multi-membership graph index — `INCOMING` and `NAME` — MUST store one
KV key per edge/membership pair rather than a single growing list value under a low-cardinality
key, and each write MUST be an unconditional `Put` (no CAS read-modify-write of a growing list).
Ingesting E memberships into any one key's dimension is therefore O(E) total writes, not O(E²),
with no CAS-retry contention because no two writers share a key. This replaces the
O(in-degree²) `INCOMING` and O(shared-name²) `NAME` rewrites.

#### Scenario: a hub dimension ingests members in linear write volume

- **GIVEN** an index dimension (a hub target or a shared name) with K members
- **WHEN** the K memberships are indexed
- **THEN** the total index writes are O(K), not O(K²)
- **AND** no single key is CAS-rewritten as the member count grows

### Requirement: Surviving index key axes are KV-safe and reconstructable

Every token of a sharded index key MUST be NATS-KV-safe and unambiguously reconstructable under its declared layout:

- **NAME open-vocabulary axis:** the normalized name value MUST hash to a fixed-width token so raw dotted values cannot
  collide with token positions under NATS prefix matching. Its composite membership key MAY retain the reversible
  predicate hex token; the per-key value MUST retain original case and priority.
- **INCOMING predicate axis:** the already-validated canonical predicate MAY use the reversible untagged hex token
  retained by that storage layout. Decoding reconstructs evidence; it does not authorize acceptance.
- **PREDICATE membership axis:** the raw canonical predicate MUST occupy exactly three leading tokens followed by the
  six-token entity ID, producing one fixed nine-token key per membership. No hash or catalog participates.
- **Entity-ID axes:** raw entity IDs MAY be used only after six-token validation. A writer MUST skip malformed values
  visibly and MUST NOT emit an empty token or mis-split key.

`INCOMING` keys on target ID with source-owner evidence represented in the remainder of its layout; `NAME` keys on the
name hash; `PREDICATE_INDEX` keys on raw predicate plus entity ID. Graph-index replay MUST visibly reject malformed
predicates or entity IDs before membership I/O and keep readiness honest under rejected current state.

#### Scenario: codec round-trip does not change predicate acceptance

- **GIVEN** arbitrary bytes are passed directly to the NAME/INCOMING predicate codec
- **WHEN** the encoded token is decoded
- **THEN** the codec reconstructs the exact original bytes
- **AND** that result does not authorize a graph or index write

#### Scenario: a noncanonical current predicate is rejected before index I/O

- **GIVEN** a current write candidate whose predicate has the wrong arity, whitespace, or a wildcard token
- **WHEN** it reaches the authoritative graph-write contract or graph-index replay validation
- **THEN** the candidate is rejected before membership or reverse-index I/O
- **AND** invalid preexisting replay state keeps readiness false

#### Scenario: a malformed entity ID is skipped, not indexed into a mis-split key

- **WHEN** a write path composes a key from an entity ID that is not a valid six-token ID
- **THEN** the write is skipped and logged
- **AND** no malformed key is stored

#### Scenario: predicate membership needs no catalog reconstruction

- **GIVEN** one current raw `PREDICATE_INDEX` membership key
- **WHEN** a reader reconstructs its semantic axes
- **THEN** the first three tokens yield the exact canonical predicate and the remaining six yield the entity ID
- **AND** no `PREDICATE_CATALOG`, hash lookup, or reversible predicate codec is consulted

### Requirement: Index reads reconstruct sharded keysets and preserve wire response types

Every reader of a sharded index MUST enumerate via the prefix scan and reconstruct entries from
keys — including all query handlers AND internal consumers such as graph-clustering's neighbor
expansion. The NATS query-API wire response types (e.g. `IncomingEntry` populating
`graph.index.query.incoming`) MUST be preserved and reconstructed from keys — only the storage
format changes, not the wire contract.

The entity-delete path removes an entity's `<entityID>.*` keyset by prefix scan, but the semantics
differ by index and MUST be labeled as such:

- For `INCOMING` (target-prefixed), the `<entityID>.*` keyset is every row where the entity is the
  TARGET — but each such row is a SOURCE's evidence that it still points at this entity. Deleting
  them on the target's death is a LEGACY HARD-DELETE of a leaf entity and MUST NOT be treated as, or
  reused by, logical retirement (which must retract SOURCE-owned rows via the source, not the
  target). Source-owned retraction is deferred to the retention increment (gh#527).

Removing a deleted entity from *other* entities' keys (where it appears as a non-prefix token) is the
pre-existing gh#433 reciprocal-cleanup gap and remains out of scope.

#### Scenario: a query response is unchanged in shape

- **WHEN** a `graph.index.query.incoming` request is served after the format change
- **THEN** the response carries the same `IncomingEntry` fields as before, reconstructed from keys

#### Scenario: an internal clustering reader sees the same neighbors

- **GIVEN** community detection expanding incoming neighbors of an entity
- **WHEN** it reads the incoming index after the format change
- **THEN** it observes the same neighbor set as before (no silently-empty read)

### Requirement: Every surviving derived index declares storage responsibility and reconciliation capability

Each derived graph index MUST declare its physical token layout, exact arity when fixed or explicit variable arity,
component responsible for its rows, literal fixed-arity forward query filter when available or explicit non-filterability,
value-overwrite policy, and update/delete/retirement behavior. It MUST declare either a literal
owner-reconciliation filter or explicit non-filterability with alternate authority deferred to a separate
specification. When a proven bounded owner filter exists, reconciliation MUST enumerate the stored owner rows,
deduplicate them by exact key, diff them against the complete desired projection from current ENTITY_STATES,
delete stale rows, and put missing rows.

Owner-filter reconciliation MUST preserve keyed entity ordering, execution-time authoritative reads, bounded
repair, and readiness withholding on any required failure.

#### Scenario: removing the final membership retracts the stored row

- **GIVEN** an entity whose stored owner projection contains one membership
- **WHEN** current ENTITY_STATES yields an empty desired projection for that membership index
- **THEN** reconciliation deletes the stale row
- **AND** queries do not return the former membership

#### Scenario: changing a membership retracts the former row

- **GIVEN** an entity whose stored owner projection contains membership A
- **WHEN** current ENTITY_STATES yields membership B instead
- **THEN** reconciliation deletes A and writes B as one required projection
- **AND** public queries do not return the entity through A after the watermark

#### Scenario: predicate replacement reaches empty

- **GIVEN** an entity uses predicate A
- **WHEN** its current projection changes to predicate B and then contains neither predicate
- **THEN** PREDICATE_INDEX contains only B at the first watermark and neither membership at the second

#### Scenario: name replacement overwrites stable-key metadata

- **GIVEN** an entity is named Alpha through one display-name predicate
- **WHEN** it changes to Beta and then removes the display name
- **THEN** NAME_INDEX retracts Alpha, exposes Beta, and finally contains no membership for the entity
- **AND** a case or priority change whose normalized key stays stable overwrites the stored value

#### Scenario: relationship replacement retracts source-owned incoming rows

- **GIVEN** a source relates to target A
- **WHEN** its relationship changes to target B and then to no target
- **THEN** INCOMING_INDEX contains only the source-owned B row at the first watermark and none at the second

#### Scenario: outgoing replacement remains complete

- **GIVEN** an entity points to target A
- **WHEN** its outgoing projection changes to target B and then to empty
- **THEN** OUTGOING_INDEX replaces the complete array at both watermarks without a phantom edge

#### Scenario: duplicate filtered results do not duplicate work or query results

- **GIVEN** filtered enumeration observes the same key more than once during concurrent mutation
- **WHEN** reconciliation computes the stored owner set
- **THEN** it deduplicates by exact key before diffing
