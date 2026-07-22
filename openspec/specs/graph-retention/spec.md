# graph-retention Specification

## Purpose
Current-truth for how the live graph treats retention and deletion: storage-level
eviction (NATS TTL/MaxBytes/MaxAge) is never a lifecycle mechanism, because it is
reachability-blind. This covers both the live graph's KV buckets AND the content
ObjectStores those entities reference (content-addressed verbatim bodies), which
are equally reachability-blind under age/size eviction. This capability tracks the
ADR-068 increments; today it covers the D1 guardrail (no lifecycle retention on
live graph buckets and content ObjectStores). Later increments
(delete-as-refuse/cascade, tombstones, the per-entity reverse index, the GC worker,
and reference-aware orphaned-blob reclamation — #633) extend it.

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

### Requirement: Content ObjectStores carry no lifecycle retention

Content ObjectStores MUST NOT use NATS TTL (`MaxAge`) or a binding `MaxBytes` as a
lifecycle mechanism. This covers every ObjectStore holding ref-addressed
`ContentStorable` payloads — the generic message store, the agent content bucket, and
the embedding evidence store — because their objects are referenced by live-graph
entities that outlive them, and age/size eviction is reachability-blind (ADR-068): it
would strand an entity pointing at content that has silently expired. The shared
ObjectStore constructor MUST NOT stamp any retention on the backing stream, and no
zero-valued TTL knob is exposed on the configuration surface.

Enforcement is boot-time and self-healing: on start, each content store's backing
stream (`OBJ_<bucket>`) is inspected; any binding `MaxAge`/`MaxBytes` is stripped in
place and logged (covering legacy buckets the constructor's create-or-get path would
otherwise never reconcile), then re-asserted — if retention is still binding, startup
fails closed rather than proceeding to silently expire evidence.

#### Scenario: the constructor stamps no retention on a content store

- **GIVEN** a content ObjectStore is created through the shared constructor
- **WHEN** its backing stream configuration is built
- **THEN** the backing stream carries `MaxAge` `0` and no binding `MaxBytes`, and no
  TTL field is present on the store configuration surface

#### Scenario: boot strips a legacy retention config and warns

- **GIVEN** a content ObjectStore whose backing stream already carries a non-zero
  `MaxAge` (e.g. the historical 24h TTL) from before this contract
- **WHEN** the store starts and inspects the backing stream
- **THEN** the retention is cleared in place via a stream update and a warning is
  logged naming the bucket and the removed retention
- **AND** no stored object is deleted by the reconciliation

#### Scenario: boot fails closed when retention cannot be stripped

- **GIVEN** a content ObjectStore whose backing stream carries a binding
  `MaxAge`/`MaxBytes` that the reconciliation could not clear
- **WHEN** the store re-asserts the backing stream configuration after reconciliation
- **THEN** startup fails with a fatal error naming the bucket and its offending
  retention, rather than proceeding

#### Scenario: a clean content store boots normally

- **GIVEN** a content ObjectStore whose backing stream has `MaxAge` `0` and no binding
  `MaxBytes`
- **WHEN** the store starts and inspects the backing stream
- **THEN** the guardrail passes and startup proceeds

