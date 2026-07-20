# graph-index — delta

Surface-name drift only: this change removed the `graph.index.query.status`
request/reply subject (ADR-083 Break 1), so the readiness-withholding
requirement's references to that subject retarget to the published readiness
envelope (`GRAPH_STATUS` KV). Behavior is unchanged.

## MODIFIED Requirements

### Requirement: Readiness is authoritative and consumers fail closed on incomplete indexes

Graph-index MUST NOT advertise readiness, and its reverse-index query/traversal/clustering consumers
MUST NOT return a successful result, while the index is known-incomplete — whether because it is
still building (cutover / cold replay) or because a required index write or delete has failed and not
yet been repaired.

- **Failure honesty.** A required index write (incoming/context/name/alias/predicate/outgoing) or a
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
