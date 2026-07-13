## 1. Contract and Inventory

- [ ] 1.1 Add storage-class, budget, threshold, and migration-override configuration types with validation
- [ ] 1.2 Inventory ordinary streams plus `KV_*` and `OBJ_*` backing streams with logical-owner mapping
- [ ] 1.3 Add an optional capacity-reporting capability for registered non-NATS `storage.Store` backends
- [ ] 1.4 Expose storage doctor output and Prometheus metrics for usage, headroom, growth, and time to threshold
- [ ] 1.5 Add unit tests for unknown capacity, ownership mapping, pressure transitions, and typed budget errors

## 2. JetStream Bounds and Reconciliation

- [ ] 2.1 Require finite `MaxAge` and `MaxBytes` on production firehose stream declarations
- [ ] 2.2 Reconcile editable limits and discard policy on existing streams and report incompatible drift
- [ ] 2.3 Inspect KV/ObjectStore backing-stream configuration rather than relying on get-or-create success
- [ ] 2.4 Add real-NATS tests for drift repair, startup rejection, `DiscardNew`, and stream-full errors

## 3. Graph Capacity Admission

- [ ] 3.1 Add maximum serialized entity size and identity-birth/append-growth budget configuration
- [ ] 3.2 Implement warning/high/critical admission while reserving replacement and recovery headroom
- [ ] 3.3 Permit finite graph `MaxBytes` only after verifying `DiscardNew` and rejection observability
- [ ] 3.4 Add tests proving graph TTL/`DiscardOld` rejection and non-evicting capacity failure
- [ ] 3.5 Add product-facing diagnostics for telemetry identities and unbounded append predicates

## 4. Store and Reference Hardening

- [ ] 4.1 Extend store configuration with lifetime class, byte/object limits, in-flight bytes, and backend options
- [ ] 4.2 Separate NATS ObjectStore instances/buckets for ephemeral, current-owned, and retained/audit classes
- [ ] 4.3 Add streaming write support with expected size, digest, content type, and small-object buffering threshold
- [ ] 4.4 Bound concurrent bytes and isolate large-object work from graph/control/read/delete operations
- [ ] 4.5 Ack store inputs only after durable object and required reference commit; classify retry and terminal errors
- [ ] 4.6 Implement write-verify, reference CAS, old-object release, and repair-manifest handling for owned blobs
- [ ] 4.7 Make nested binary-reference publication atomic or record every partial child for repair
- [ ] 4.8 Add object count/byte/growth/rejection metrics and a report-only dangling/orphan scrubber
- [ ] 4.9 Add backend-contract and real-NATS tests for expiry, pressure, ack failure, streaming, and reference races

## 5. Derived-Index Rebuild

- [ ] 5.1 Define a maintenance command that selects and clears rebuildable derived index buckets
- [ ] 5.2 Replay current `ENTITY_STATES` through the single index-owner path with a generation watermark
- [ ] 5.3 Gate NATS handlers, direct clients, traversal, and clustering on the same rebuild readiness state
- [ ] 5.4 Add empty-graph, interrupted-rebuild, partial-write, and successful-rebuild integration tests

## 6. Migration and Release Gates

- [ ] 6.1 Ship report-only inventory and classify existing resources before enabling admission
- [ ] 6.2 Generate migration diagnostics for unbounded resources, mixed classes, drift, and dangling references
- [ ] 6.3 Update ADR-068/073 wording to separate lifecycle retention from `DiscardNew` capacity rejection
- [ ] 6.4 Write pressure, full-resource, reference-repair, and maintenance-rebuild operator runbooks
- [ ] 6.5 Run lint, race tests, schema no-drift checks, contracts, real-NATS integration, and relevant e2e tiers
- [ ] 6.6 Enable startup enforcement, then write admission, only after migration preflight is clean
