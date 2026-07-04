# graph-retention Specification

## Purpose
Current-truth for how the live graph's KV buckets treat retention and deletion:
storage-level eviction (NATS TTL/MaxBytes/MaxAge) is never a lifecycle mechanism
on the graph, because it is reachability-blind. This capability tracks the
ADR-068 increments; today it covers the D1 guardrail (no lifecycle retention on
live graph buckets). Later increments (delete-as-refuse/cascade, tombstones, the
per-entity reverse index, the GC worker) extend it.

## Requirements
### Requirement: The live graph carries no lifecycle retention

The live graph KV buckets MUST NOT use NATS TTL (`MaxAge`) or a binding
`MaxBytes` as a lifecycle mechanism. This covers `ENTITY_STATES` and its derived
indexes (`PREDICATE_INDEX`, `INCOMING_INDEX`, `OUTGOING_INDEX`, `NAME_INDEX`,
`ALIAS_INDEX`, `CONTEXT_INDEX`, `SPATIAL_INDEX`). Retention is a semantic
operation (ADR-068), never a storage-policy side effect: age/size eviction is
reachability-blind and would drop an entity that still has live inbound edges.

#### Scenario: No component defaults a shared graph bucket to a TTL

- **GIVEN** the graph-query client builds its default KV configuration
- **WHEN** `DefaultConfig()` is constructed
- **THEN** the `ENTITY_STATES`, `SPATIAL_INDEX`, and `INCOMING_INDEX` bucket TTLs
  are `0` (no expiry)

#### Scenario: graph-ingest refuses to boot on a retention-configured graph

- **GIVEN** the `ENTITY_STATES` bucket exists with a non-zero `MaxAge` (TTL) or a
  binding `MaxBytes` — e.g. because another process won the get-or-create race
  with a retention config
- **WHEN** `graph-ingest` starts and inspects the bucket's backing-stream config
- **THEN** startup fails with a fatal error naming the bucket and its offending
  retention, rather than proceeding to silently expire graph state

#### Scenario: a clean graph bucket boots normally

- **GIVEN** the `ENTITY_STATES` bucket exists with `MaxAge` `0` and no binding
  `MaxBytes`
- **WHEN** `graph-ingest` starts and inspects the bucket
- **THEN** the guardrail passes and startup proceeds

