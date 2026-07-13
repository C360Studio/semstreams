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

### Requirement: Low-cardinality key axes are hashed; entity-ID axes are validated

A sharded index key MUST hash any low-cardinality or open-vocabulary axis it prefixes on — the
name or context value — to a fixed-width token, so a raw dotted value cannot token-position-collide
under NATS prefix matching (ADR-065; `CONTEXT_INDEX` today uses the raw value and must switch).
An axis that is an entity ID MAY stay raw (fixed 6-token, collision-safe as a prefix), but the
write path MUST validate every entity ID it composes into a key with `IsValidEntityID` and
skip-with-log on failure, so a malformed ID cannot silently mis-split on read or poison the prefix
keyspace. A key MUST NOT contain an empty token (e.g. a missing predicate producing a trailing dot).
Where the human-readable value cannot be recovered from a hashed prefix (name original-case +
priority, context value), the small per-key value MUST carry it.

#### Scenario: a context value cannot over-match a nested context

- **GIVEN** memberships under context `inference.hierarchy` and under `inference.hierarchy.deep`
- **WHEN** the members of `inference.hierarchy` are enumerated
- **THEN** the result excludes `inference.hierarchy.deep` members (the context axis is hashed)

#### Scenario: a malformed entity ID is skipped, not indexed into a mis-split key

- **WHEN** a write path composes a key from an entity ID that is not a valid 6-token ID
- **THEN** the write is skipped and logged
- **AND** no malformed key is stored

### Requirement: Index reads and deletes enumerate the sharded keyset; wire response types are preserved

Every reader of a sharded index MUST enumerate via the prefix scan and reconstruct entries from
keys — including all query handlers AND internal consumers such as graph-clustering's neighbor
expansion — and the entity-delete path MUST remove the entity's own `<entityID>.*` keyset by prefix
scan (its aggregate keys are prefixed by the entity, so this is a clean prefix delete, not a scan
of the whole bucket). The NATS query-API wire response types (e.g. `IncomingEntry` populating
`graph.index.query.incoming`) MUST be preserved and reconstructed from keys — only the storage
format changes, not the wire contract. Removing a deleted entity from *other* entities' keys (where
it appears as a non-prefix token) is the pre-existing gh#433 reciprocal-cleanup gap and remains out
of scope.

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
