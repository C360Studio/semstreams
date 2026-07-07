# Tasks

Increment 1 (gh#497 — despawn + watch-visibility) is this change. Increment 2
(gh#498 — batch, gated on a new graph-ingest batch-entity-write handler) is a
follow-up change; its tasks are stubbed at the end for tracking, not executed
here.

## 1. Emitter delete seam

- [ ] 1.1 Add a `delete(ctx, *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error)` method to the `graphEmitter` interface in `pkg/lifecycle/graph_emit.go`.
- [ ] 1.2 Implement it on `graphEmitterNATS` targeting the existing `graph.mutation.entity.delete` subject (add `graphSubjectEntityDelete` const), reusing `RequestWithRetryClassified` + `lifecycleEmitRetryConfig` for the graph-ingest cold-start race, and the `error: ` payload-prefix check (handler-error convention).
- [ ] 1.3 Implement `delete` on the in-memory/test emitter (the second `graphEmitter` impl) so unit tests can drive despawn without NATS.

## 2. Despawn primitives

- [ ] 2.1 Add `Manager.Despawn(ctx, workflow, entityID) error`: resolve+validate the registered workflow and that its `EntityIDPattern` matches `entityID` (error, no emit, on mismatch); emit `delete`; treat already-absent as idempotent success.
- [ ] 2.2 Add `Manager.DespawnWith(ctx, workflow, entityID, source TransitionSource, note string) error`: transition to the workflow's terminal phase via the existing `Transition` path (produces the phase write + audit `TransitionEvent`), then `Despawn`. Document the non-atomic, recoverable partial-failure contract.
- [ ] 2.3 Document on both methods that reclaim does NOT clean derived indexes (gh#433/ADR-068) — reclaim ≠ index GC — and that `Despawn` reclaims only (no implicit terminal transition).

## 3. WatchEvents delete-visibility

- [ ] 3.1 Add `LifecycleOp` (`Upserted`/`Deleted`) and `LifecycleEvent{ Op; EntityID; Participant }` types to `pkg/lifecycle`.
- [ ] 3.2 Factor the KV-watch projection/dispatch loop in `manager_query.go` so `Watch` and the new `WatchEvents` share it (no duplication).
- [ ] 3.3 Add `Manager.WatchEvents(ctx, workflow) (<-chan LifecycleEvent, error)`: emit `Upserted{EntityID, Participant}` for matching upserts (mirror `Watch`'s initial-values bootstrap), and `Deleted{EntityID, nil}` for `KeyValueDelete`/`KeyValuePurge` whose key matches the `EntityIDPattern` (instead of skipping).
- [ ] 3.4 Leave existing `Watch(ctx, workflow) <-chan Participant` byte-for-byte behaviorally unchanged (upsert-only); assert via a regression test.

## 4. Tests

- [ ] 4.1 Unit tests (in-memory emitter, no NATS): `Despawn` emits delete + idempotent-on-absent + rejects unregistered/non-matching workflow; `DespawnWith` transitions-then-deletes and the partial-failure retry path reclaims.
- [ ] 4.2 Integration test (testcontainer NATS + graph-ingest): `Despawn` actually removes the entity from `ENTITY_STATES` (Get returns not-found).
- [ ] 4.3 `WatchEvents` test: observer receives `Deleted` on reclaim and `Upserted` on create/phase-change; a companion `Watch` observer receives the upsert but NOT the delete (proves the existing surface is unchanged).

## 5. Docs + gates

- [ ] 5.1 Update ADR-047 (or the seeded `lifecycle` spec is the current-truth home) to note reclamation exists; add a short usage note to the lifecycle harness docs. Do NOT re-document mechanics in the ADR — the spec is the current-truth home.
- [ ] 5.2 Pre-push gate: `go test -race ./pkg/lifecycle/...` + integration, `go vet` (incl. `-tags=integration`), `gofmt`, `revive` (no new warnings), `task schema:generate` no-drift. semstreams-reviewer pre-merge (new substrate API + RPC error contract on the new delete emit).

## 6. Increment 2 — batch (gh#498), FOLLOW-UP CHANGE (not executed here)

- [ ] 6.1 Scope a follow-up change adding a graph-ingest batch-entity-write handler (`graph.mutation.entity.write_batch`), applying N create/update ops under single-writer + ADR-055 envelope + CAS, with a `graph-ingest` spec delta authored at that time.
- [ ] 6.2 Add emitter `writeBatch` + `Manager.CreateBatch([]Participant)` / `TransitionBatch(...)` that coalesce a wave into one request (do NOT ship a loop-only stand-in — no coalescing = no gh#498 benefit).
- [ ] 6.3 Decide partial-success reporting (per-entity error slice) vs strict all-or-nothing (open question in design.md).
