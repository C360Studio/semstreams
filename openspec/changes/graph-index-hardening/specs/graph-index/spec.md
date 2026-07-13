## ADDED Requirements

### Requirement: List-valued indexes store one sharded key per membership, not a monolithic list

Each migrated multi-membership graph index — `INCOMING`, `NAME`, and `CONTEXT` — MUST store one
KV key per edge/membership pair rather than a single growing list value under a low-cardinality
key, and each write MUST be an unconditional `Put` (no CAS read-modify-write of a growing list).
Ingesting E memberships into any one key's dimension is therefore O(E) total writes, not O(E²),
with no CAS-retry contention because no two writers share a key. This replaces the
O(in-degree²) `INCOMING`, O(shared-name²) `NAME`, and O(context-fan-in²) `CONTEXT` rewrites, and —
because `CONTEXT_INDEX` today uses a non-CAS `Get`+`Put` — also eliminates its concurrent
lost-update.

#### Scenario: a hub dimension ingests members in linear write volume

- **GIVEN** an index dimension (a hub target, a shared name, or a common context value) with K members
- **WHEN** the K memberships are indexed
- **THEN** the total index writes are O(K), not O(K²)
- **AND** no single key is CAS-rewritten as the member count grows

#### Scenario: concurrent writers to the same context do not lose updates

- **GIVEN** two workers concurrently indexing distinct entities that share a context value
- **WHEN** both write to `CONTEXT_INDEX`
- **THEN** both memberships are present afterward (no read-modify-write clobber)

### Requirement: Key axes are KV-safe; entity-ID axes are validated; free-form axes are encoded

Every token of a sharded index key MUST be NATS-KV-safe and unambiguously reconstructable:

- **Open-vocabulary hashed axes** — a name or context value — MUST hash to a fixed-width token so a
  raw dotted value cannot token-position-collide under NATS prefix matching (ADR-065). The
  human-readable value is not recoverable from the hash, so the small per-key value MUST carry it
  (name original-case + priority; context value).
- **The predicate axis** MUST be reversibly **hex-encoded** in the key. graph-ingest accepts any
  non-empty predicate, including KV-unsafe values (spaces, unicode, wildcard tokens); a raw predicate
  token would make the reverse-index `Put` fail while `PREDICATE_INDEX` (hashed) and `ENTITY_STATES`
  succeed, silently desyncing the forward and reverse views. Hex is chosen over a hash so the reader
  recovers the exact predicate from the key with no per-row value lookup (keeping `INCOMING` a pure
  prefix key-scan).
- **Entity-ID axes** MAY stay raw (fixed 6-token, collision-safe as a prefix), but the write path
  MUST validate every entity ID it composes into a key with `IsValidEntityID` and skip-with-log on
  failure. A key MUST NOT contain an empty token.

`CONTEXT_INDEX` — which has no production reader — MUST be keyed with the **entity ID as the prefix**
(`entityID.hash(context).hex(predicate)`), not the context value, so the write path can enumerate an
entity's own memberships by prefix scan to RETRACT superseded rows on update and to self-clean on
delete. `INCOMING` keys on the target ID; `NAME` keys on the name hash.

#### Scenario: a KV-unsafe predicate round-trips through the key

- **GIVEN** a triple whose predicate contains a space or other KV-unsafe character
- **WHEN** the reverse index is written and later read
- **THEN** the write succeeds and the reader reconstructs the exact original predicate

#### Scenario: re-indexing an entity retracts its superseded context memberships

- **GIVEN** an entity previously indexed under context predicates {p1, p2} for a context value
- **WHEN** it is re-indexed carrying only {p1} for that context
- **THEN** the p2 membership is removed (the entity-prefixed CONTEXT index reconciles), not left stale

#### Scenario: a malformed entity ID is skipped, not indexed into a mis-split key

- **WHEN** a write path composes a key from an entity ID that is not a valid 6-token ID
- **THEN** the write is skipped and logged
- **AND** no malformed key is stored

### Requirement: Index reads and deletes enumerate the sharded keyset; wire response types are preserved

Every reader of a sharded index MUST enumerate via the prefix scan and reconstruct entries from
keys — including all query handlers AND internal consumers such as graph-clustering's neighbor
expansion. The NATS query-API wire response types (e.g. `IncomingEntry` populating
`graph.index.query.incoming`) MUST be preserved and reconstructed from keys — only the storage
format changes, not the wire contract.

The entity-delete path removes an entity's `<entityID>.*` keyset by prefix scan, but the semantics
differ by index and MUST be labeled as such:

- For `CONTEXT` (entity-prefixed), the `<entityID>.*` keyset is the entity's OWN memberships, so
  removing it on the entity's death is correct cleanup.
- For `INCOMING` (target-prefixed), the `<entityID>.*` keyset is every row where the entity is the
  TARGET — but each such row is a SOURCE's evidence that it still points at this entity. Deleting
  them on the target's death is a LEGACY HARD-DELETE of a leaf entity and MUST NOT be treated as, or
  reused by, logical retirement (which must retract SOURCE-owned rows via the source, not the
  target). Source-owned retraction is deferred to the retention increment (gh#527).

Removing a deleted entity from *other* entities' keys (where it appears as a non-prefix token) is the
pre-existing gh#433 reciprocal-cleanup gap and remains out of scope.

#### Scenario: deleting an entity removes its own sharded index keys

- **GIVEN** an entity with sharded index keys prefixed by its ID
- **WHEN** the entity is deleted (an ENTITY_STATES delete event)
- **THEN** its `<entityID>.*` keys are removed by prefix scan
- **AND** a later prefix scan for that ID returns no phantom members

#### Scenario: a query response is unchanged in shape

- **WHEN** a `graph.index.query.incoming` request is served after the format change
- **THEN** the response carries the same `IncomingEntry` fields as before, reconstructed from keys

#### Scenario: an internal clustering reader sees the same neighbors

- **GIVEN** community detection expanding incoming neighbors of an entity
- **WHEN** it reads the incoming index after the format change
- **THEN** it observes the same neighbor set as before (no silently-empty read)

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

### Requirement: Readiness is authoritative — no consumer serves partial results or advertises ready after a failed index write or delete

Graph-index MUST NOT advertise readiness, and its reverse-index query/traversal/clustering consumers
MUST NOT return a successful result, while the index is known-incomplete — whether because it is
still building (cutover / cold replay) or because a required index write or delete has failed and not
yet been repaired.

- **Failure honesty.** A required index write (incoming/context/name/alias/predicate/outgoing) or a
  required delete that ultimately fails after bounded retry MUST mark the entity failed and be
  RETURNED as an error, never logged-and-continued as success. The re-index no-op baseline MUST be
  stored only after a successful write, so a failed entity re-attempts rather than being suppressed
  as a no-op.
- **Readiness withholding.** While any entity is in the failed set, `graph.index.query.status` MUST
  report not-ready, and the `INCOMING`, `OUTGOING`, and `byName` query handlers MUST return a typed
  `index_not_ready` error — including after the initial bootstrap has completed (a sticky
  "bootstrapped" flag MUST NOT mask a later failure).
- **Consumer propagation.** Consumers that read the reverse indexes — PathRAG traversal, the graph
  query client's incoming reads, and graph-clustering's community detection — MUST honor the
  not-ready signal (abort/defer) rather than convert it into an empty-but-successful result.
- **Durable recovery.** A failed entity MUST be retried by a background repair loop (not only on the
  next incidental event for that entity), so a transient backend outage self-heals and readiness is
  restored once the failed set drains. The repair re-drives each entity through the SAME keyed
  dispatch as the watcher (re-fetching the latest state), so it is ordered per-key with concurrent
  updates/deletes and does not clobber a newer write. Full per-revision fencing — closing the residual
  window where a delete lands between the repair's read and its re-index — is the ordered-processing
  increment (gh#527); this change delivers the bounded, keyed-ordered repair, not full generation
  safety.
- **Empty graph.** An authoritatively empty graph (initial enumeration complete, 0/0) MUST become
  ready, so a fresh empty graph does not reject every query forever.

#### Scenario: a post-bootstrap write failure withholds readiness

- **GIVEN** the index has bootstrapped and is serving queries
- **WHEN** a required index write for an entity subsequently fails after retry
- **THEN** `graph.index.query.status` reports not-ready
- **AND** an `incoming` or `byName` query returns `index_not_ready`, not a partial result

#### Scenario: a failed delete does not advertise ready

- **WHEN** an entity delete's required index cleanup fails
- **THEN** the entity remains marked failed and readiness is withheld until the cleanup succeeds

#### Scenario: a traversal aborts rather than returning a partial path set

- **GIVEN** the index is not ready
- **WHEN** a PathRAG traversal requests incoming relationships
- **THEN** the traversal returns the not-ready error rather than a truncated/empty path set

#### Scenario: an empty graph becomes ready

- **GIVEN** a fresh graph with no entities and initial enumeration complete
- **WHEN** readiness is evaluated
- **THEN** the index reports ready and reverse-index queries are served

#### Scenario: a byName lookup over a huge shared name is bounded

- **WHEN** a `byName` lookup would hydrate more memberships than the read budget
- **THEN** it returns a typed `resource_exhausted` error rather than an unbounded serial scan
