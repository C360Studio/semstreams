## MODIFIED Requirements

### Requirement: The live graph carries no lifecycle retention

The live graph KV buckets MUST NOT use NATS TTL (`MaxAge`) or `DiscardOld` byte eviction as a lifecycle
mechanism. This covers `ENTITY_STATES` and its required current indexes (`PREDICATE_INDEX`,
`INCOMING_INDEX`, `OUTGOING_INDEX`, `NAME_INDEX`, `ALIAS_INDEX`, `CONTEXT_INDEX`, `SPATIAL_INDEX`).
Retention is a semantic operation, never a storage-policy side effect: age or oldest-first eviction is
reachability-blind and could drop an entity that still has live inbound edges.

A finite `MaxBytes` capacity ceiling is permitted only when SemStreams verifies that the backing stream uses
`DiscardNew`, surfaces rejected writes, reserves configured replacement/recovery headroom, and describes the
limit as an outage circuit breaker rather than reclamation. Graph lifecycle cleanup MUST remain semantic.

#### Scenario: No component defaults a shared graph bucket to a TTL

- **GIVEN** the graph-query client builds its default KV configuration
- **WHEN** `DefaultConfig()` is constructed
- **THEN** the `ENTITY_STATES`, `SPATIAL_INDEX`, and `INCOMING_INDEX` bucket TTLs are `0` (no expiry)

#### Scenario: Graph ingest refuses reachability-blind retention

- **GIVEN** `ENTITY_STATES` exists with non-zero `MaxAge` or a backing-stream `DiscardOld` byte limit
- **WHEN** `graph-ingest` starts and inspects the backing-stream configuration
- **THEN** startup fails with a fatal error naming the bucket and offending retention
- **AND** it does not proceed to silently expire graph state

#### Scenario: A verified fail-closed graph capacity ceiling boots

- **GIVEN** the graph bucket has zero `MaxAge`, finite `MaxBytes`, and `DiscardNew`
- **AND** replacement reserve and capacity-rejection reporting are configured
- **WHEN** `graph-ingest` validates the bucket
- **THEN** the capacity guardrail passes
- **AND** no existing graph record is evicted to admit a new write

#### Scenario: An unsafe graph byte ceiling is rejected

- **GIVEN** a graph bucket has finite `MaxBytes` without verified `DiscardNew`, replacement reserve, or
  rejection observability
- **WHEN** `graph-ingest` validates the bucket
- **THEN** startup fails with the missing safety condition

## ADDED Requirements

### Requirement: Graph admission bounds identity and value growth

Production graph configuration MUST declare a maximum serialized entity size and admission budgets for new
identity creation and append-shaped predicates. High-rate samples MUST remain on bounded time-shaped storage;
only compact current facts and explicitly bounded evidence references may enter `ENTITY_STATES`.

#### Scenario: Telemetry attempts to create one identity per sample

- **WHEN** an ingest path exceeds its declared identity-birth or append-growth budget
- **THEN** graph ingest rejects the mutation with a typed budget error
- **AND** the diagnostic directs the producer to a bounded stream or out-of-line store

### Requirement: Derived indexes can be reclaimed by rebuild

SemStreams MUST provide a maintenance operation that clears selected derived index buckets and rebuilds them
from current `ENTITY_STATES`. All query, traversal, and clustering readers of a rebuilding index MUST fail
closed until a shared readiness watermark declares the generation complete.

#### Scenario: Operator rebuilds an index containing legacy debris

- **WHEN** the operator starts a maintenance rebuild
- **THEN** the selected derived index is recreated from current entity state
- **AND** every dependent reader reports not ready until the rebuild watermark is complete
