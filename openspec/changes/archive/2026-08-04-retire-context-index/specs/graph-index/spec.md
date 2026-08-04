## ADDED Requirements

### Requirement: Context provenance remains authoritative without a durable context index

Graph-index MUST NOT create, open, write, reconcile, or publish readiness for a
`CONTEXT_INDEX` bucket. Triple provenance MUST remain stored on each authoritative
triple through `Triple.Context` in `ENTITY_STATES`; retiring the derived bucket MUST
NOT erase or rewrite that fact.

The pre-v1 cutover MUST use the coordinated clean wipe/reseed. No legacy bucket
reader, alias, translation, dual write, or online migration is provided.

#### Scenario: fresh graph-index startup creates no context bucket

- **GIVEN** a clean deployment with no `CONTEXT_INDEX`
- **WHEN** graph-index starts and reaches readiness
- **THEN** `CONTEXT_INDEX` remains absent
- **AND** the surviving query indexes initialize normally

#### Scenario: hierarchy provenance remains on authoritative triples

- **GIVEN** hierarchy inference emits triples with context `inference.hierarchy`
- **WHEN** those triples are persisted and read from `ENTITY_STATES`
- **THEN** their `Context` remains `inference.hierarchy`
- **AND** no derived context bucket is required to preserve that provenance

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

Every token of a sharded index key MUST be NATS-KV-safe and unambiguously reconstructable:

- **Open-vocabulary hashed axes** — a name value — MUST hash to a fixed-width token so raw dotted
  values cannot token-position-collide under NATS prefix matching (ADR-065). The human-readable
  value is not recoverable from the hash, so the small per-key value MUST carry its original case
  and priority.
- **The predicate axis** MUST retain PR #524's reversible untagged **hex encoding** in the key. PR #524
  selected that layout when graph-ingest still admitted any non-empty predicate, including KV-unsafe
  text. PR #532 now enforces the canonical three-part predicate contract at authoritative graph writes,
  and graph-index independently revalidates replayed state. The codec is a storage and reconstruction
  layout, not permission to persist a noncanonical predicate. Hex remains so readers recover the exact
  accepted predicate without a per-row value lookup, keeping `INCOMING` a pure prefix key-scan.
- **Entity-ID axes** MAY stay raw (fixed 6-token, collision-safe as a prefix), but the write path
  MUST validate every entity ID it composes into a key with `IsValidEntityID` and skip-with-log on
  failure. A key MUST NOT contain an empty token.

`INCOMING` keys on the target ID; `NAME` keys on the name hash.

`PREDICATE_INDEX` membership keys remain `hash(predicate).entityID`, while `PREDICATE_CATALOG` uses the raw
accepted predicate as its key. Before PR #532, the reverse-key codec and hashed membership could represent a
noncanonical predicate while the raw catalog `Put` failed; that required failure withheld readiness. Current
graph-ingest rejects noncanonical candidates before persistence, and graph-index replay revalidation rejects invalid
preexisting state before membership, catalog, or reverse-index I/O and keeps readiness false.

#### Scenario: codec round-trip does not change predicate acceptance

- **GIVEN** arbitrary bytes are passed directly to the predicate key codec
- **WHEN** the encoded token is decoded
- **THEN** the codec reconstructs the exact original bytes
- **AND** that codec result does not authorize a graph write or index write

#### Scenario: a noncanonical current predicate is rejected before index I/O

- **GIVEN** a current write candidate whose predicate has the wrong arity, whitespace, or a wildcard token
- **WHEN** it reaches the authoritative graph-write contract
- **THEN** the candidate is rejected before membership, catalog, or reverse-index I/O
- **AND** invalid preexisting replay state fails graph-index revalidation and keeps readiness false

#### Scenario: a malformed entity ID is skipped, not indexed into a mis-split key

- **WHEN** a write path composes a key from an entity ID that is not a valid 6-token ID
- **THEN** the write is skipped and logged
- **AND** no malformed key is stored

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

### Requirement: Every surviving derived index declares semantic ownership and reconciliation capability

Each derived graph index MUST declare its physical token layout, exact arity when fixed or explicit variable arity,
semantic row owner, literal fixed-arity forward query filter when available or explicit non-filterability,
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

## MODIFIED Requirements

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

### Requirement: Non-predicate codecs keep their recorded rationale

NAME MUST keep hashed keys for its open-content axis, and NAME and INCOMING MAY keep the
reversible `hex(predicate)` single-token codec; each keep MUST be recorded with its rationale
in the representation ADR and revisited only on a demonstrated query or operational need.
No codec MAY be treated as acceptance authority for a predicate that violates the canonical grammar.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded into a KV-safe token but violates canonical syntax
- **WHEN** graph state is written in enforcement mode
- **THEN** predicate validation rejects it before graph-index processing

## REMOVED Requirements

### Requirement: List-valued indexes store one sharded key per membership, not a monolithic list

**Reason**: the requirement bundled the retired context projection with surviving INCOMING and
NAME membership stores. Its surviving guarantees move to the narrower added requirement above.

**Migration**: no context storage migration is provided; stale beta state is removed by the clean
wipe/reseed. INCOMING and NAME retain their sharded layouts.

### Requirement: Key axes are KV-safe; entity-ID axes are validated; free-form axes are encoded

**Reason**: the requirement bundled the retired context codec with surviving NAME, INCOMING, and
predicate key contracts. Its surviving guarantees move to the narrower added requirement above.

**Migration**: delete the context codec. Surviving key layouts do not change.

### Requirement: Index reads and deletes enumerate the sharded keyset; wire response types are preserved

**Reason**: the requirement bundled context-owned deletion with surviving sharded read and
INCOMING ownership behavior. Its surviving guarantees move to the narrower added requirement.

**Migration**: delete context cleanup. Surviving query wire responses and INCOMING ownership do
not change.

### Requirement: Every derived index declares semantic ownership and reconciliation capability

**Reason**: the requirement's context replacement scenario described a retired view. The
surviving ownership and reconciliation guarantees move to the narrower added requirement.

**Migration**: no replacement context view is introduced. Surviving derived indexes keep their
existing ownership and reconciliation contracts.
