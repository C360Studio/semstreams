## Why

SemStreams can defer physical semantic GC for v1, but it cannot ship with storage growth that is
unbounded, invisible, or discovered only when JetStream rejects writes. High-rate and large-payload
products need a framework-owned capacity contract that keeps raw history, current graph state,
derived indexes, and referenced objects within an operator-declared envelope.

The immediate opportunity is to use the NATS primitives already underneath the framework instead of
building a Cassandra-like reclamation subsystem: bounded streams for history, stable-key compact
current state, fail-closed capacity ceilings, admission control before hard limits, and ObjectStore
references for large content.

## What Changes

- Add a whole-account storage inventory and pressure model covering ordinary JetStream streams,
  `KV_*` backing streams, and `OBJ_*` ObjectStores. Operators can see configured limits, actual usage,
  growth rate, headroom, and time-to-threshold from one SemStreams surface.
- Require every firehose/event stream managed by SemStreams to have finite `MaxAge` and `MaxBytes`
  bounds, and reconcile editable retention/capacity drift on existing streams.
- Distinguish semantic retention from capacity protection on authoritative graph KV:
  - TTL/age eviction remains forbidden;
  - a verified `DiscardNew` byte ceiling may be used as a fail-closed emergency circuit breaker, not
    as lifecycle cleanup;
  - soft admission thresholds reject new identities and append-shaped growth before the hard ceiling
    while preserving reserve headroom for existing-state replacement and recovery.
- Add entity-shape growth controls: maximum serialized entity size, observable birth/update rates by
  bounded entity type/prefix, and explicit bounds or decomposition requirements for append-evidence
  predicates.
- Promote the backend-neutral `storage.Store` + `StorageReference` contract to the first-class
  large-payload lane, with NATS ObjectStore as one bounded implementation rather than the mandatory
  backend for every large binary:
  - separate lifetime classes (`windowed/ephemeral`, `entity-owned/current`, and `retained/audit`)
    mapped to independently configured store instances or ObjectStore buckets;
  - configurable TTL where legal, `MaxBytes`, replicas, compression, and per-object admission limits;
  - no expiring object may be advertised through a durable live `StorageReference`;
  - writes fail honestly and JetStream inputs are acknowledged only after durable object storage and
    required reference publication succeed;
  - bounded concurrency/backpressure and streaming write/read paths prevent large payloads from
    starving graph and control traffic;
  - object count, bytes, rejected writes, dangling-reference checks, and growth rate are observable.
- Provide operator runbooks and a maintenance-mode derived-index rebuild from current
  `ENTITY_STATES`, so stale/rebuildable projection debris can be reclaimed without deciding whether a
  semantic identity is dead.
- Own post-v1 retained-state upgrades through a versioned report-only preflight and operator-approved migration
  manifest. The manifest declares source/target versions, retained resources, backup/export scope, migration and
  rebuild order, readiness/validation gates, a safe rollback point, and a removal deadline for any temporary
  migration-only compatibility mechanism.

**BREAKING (operational):** production-mode startup and writes may reject previously accepted
unbounded configurations, over-budget entity births, append-shaped state, and ObjectStore writes.
This is deliberate fail-safe behavior. Post-v1, report-only diagnostics, backup/restore proof, an operator-approved
plan, and supported real-NATS upgrade evidence precede destructive migration or stricter enforcement.

## Non-goals

- Physical semantic entity purge, cascade delete, mark/sweep, global GC coordination, or ObjectStore
  reachability GC.
- Using TTL or `DiscardOld` to expire authoritative graph entities or their required current indexes.
- Making high-rate telemetry samples into graph identities. Raw samples remain stream/ObjectStore
  data; only compact current facts belong in `ENTITY_STATES`.
- Product-specific retention periods, entity quotas, or data classifications. Products declare
  budgets against framework primitives.
- Hot, zero-downtime index generation swaps. A readiness-gated maintenance rebuild is sufficient for
  v1.
- Replacing external object storage such as filesystem/S3. NATS ObjectStore remains one implementation
  of the common contract and is not presumed suitable for every large-media workload.
- Treating a permissive legacy reader, indefinite dual writer, or relaxed validation contract as rollback. Rollback
  means returning to the last compatible binary/configuration at the manifest's proven safe point.

## Capabilities

### New Capabilities

- `storage-operability`: account-wide inventory, capacity budgets, pressure states, admission
  behavior, alerts, and operator diagnostics across streams, KV, indexes, and ObjectStore.
- `object-storage`: bounded large-payload storage, lifetime modes, durable reference safety,
  backpressure, acknowledgement semantics, and object capacity observability.

### Modified Capabilities

- `nats-streaming`: require finite stream limits and reconcile editable limit drift on existing
  streams.
- `graph-retention`: retain the ban on reachability-blind lifecycle eviction while permitting a
  verified fail-closed capacity ceiling and adding identity/value-growth admission requirements.

## Impact

- **Framework code:** `config/streams.go`, `config/config.go`, `natsclient` JetStream inventory and
  metrics, `processor/graph-ingest`, graph-index readiness/rebuild surfaces,
  `storage/objectstore`, `storage.Store`, and `message.StorageReference` validation.
- **Operator surface:** production startup validation, storage status/doctor output, Prometheus
  metrics and alerts, generated/recommended NATS account configuration, versioned upgrade manifests,
  backup/restore validation, and pressure/migration runbooks.
- **Consumers:** SemSource and SemLink raw/binary ingestion; SemOps telemetry and evidence; SemConnect
  observations/artifacts; SemTeams/SemDev agent content and trajectories; every product creating
  persistent graph identities.
- **Architecture records:** ADR-068/073 wording must be corrected to distinguish `DiscardOld`
  lifecycle eviction from pinned NATS KV/ObjectStore `DiscardNew` capacity rejection.
- **Verification:** unit and real-NATS integration tests for configuration drift, capacity rejection,
  pressure admission, ObjectStore acknowledgement/backpressure, and dangling-reference prevention;
  relevant structural and semantic e2e tiers before any breaking enforcement lands.
