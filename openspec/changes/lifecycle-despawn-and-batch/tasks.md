# Tasks

Increment 1 (gh#497 — despawn + watch-visibility) is this change. Increment 2
(gh#498 — batch, gated on a new graph-ingest batch-entity-write handler) is a
follow-up change; its tasks are stubbed at the end for tracking, not executed
here.

## 1. Emitter delete seam

- [x] 1.1 Add a `delete(ctx, *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error)` method to the `graphEmitter` interface in `pkg/lifecycle/graph_emit.go`.
- [x] 1.2 Implement it on `graphEmitterNATS` targeting the existing `graph.mutation.entity.delete` subject (add `graphSubjectEntityDelete` const), reusing `RequestWithRetryClassified` + `lifecycleEmitRetryConfig` for the graph-ingest cold-start race. (Delete is idempotent — no not-found sentinel to translate, unlike create/update.)
- [x] 1.3 Implement `delete` on the in-memory/test emitter (`fakeEmitter` + `fakeBucket.remove`; `tokenRecorder` inherits it via embedding) so unit tests can drive despawn without NATS.

## 2. Despawn primitives

- [x] 2.1 Add `Manager.Despawn(ctx, workflow, entityID) error`: resolve+validate the registered workflow and that its `EntityIDPattern` matches `entityID` (`ErrEntityIDPatternMismatch`, no emit, on mismatch); emit `delete`; already-absent is idempotent success (handler reports Deleted:false).
- [x] 2.2 Add `Manager.DespawnWith(ctx, workflow, entityID, source TransitionSource, note string) error`: transition to the workflow's terminal phase (shared `selectReachableTerminal` with `Complete`, source/note on the audit), then `Despawn`. Idempotent: already-terminal skips the transition, already-absent no-ops. Non-atomic, recoverable partial-failure contract documented.
- [x] 2.3 Document on both methods that reclaim does NOT clean derived indexes (gh#433/ADR-068) — reclaim ≠ index GC — and that `Despawn` reclaims only (no implicit terminal transition).

## 3. WatchEvents delete-visibility

- [x] 3.1 Add `EventOp` (`Upserted`/`Deleted`) and `Event{ Op; EntityID; Participant }` types to `pkg/lifecycle` (named `Event`/`EventOp`, not `Lifecycle*`, to avoid the revive package-name-stutter gate).
- [x] 3.2 Factor the KV-watch projection/dispatch loop in `manager_query.go` so `Watch` and the new `WatchEvents` share it (`startWatch` + `runWatchLoop` + `projectWatchEntry`; no duplication).
- [x] 3.3 Add `Manager.WatchEvents(ctx, workflow) (<-chan LifecycleEvent, error)`: emit `Upserted{EntityID, Participant}` for matching upserts (mirror `Watch`'s initial-values bootstrap), and `Deleted{EntityID, nil}` for `KeyValueDelete`/`KeyValuePurge` whose key matches the `EntityIDPattern` (instead of skipping).
- [x] 3.4 Leave existing `Watch(ctx, workflow) <-chan Participant` byte-for-byte behaviorally unchanged (upsert-only); assert via a regression test (the companion-Watch-sees-no-delete assertion in `TestIntegration_WatchEvents_DeliversUpsertAndDelete`).

## 4. Tests

- [x] 4.1 Unit tests (in-memory emitter, no NATS): `Despawn` emits delete + idempotent-on-absent + rejects unregistered/non-matching workflow; `DespawnWith` transitions-then-deletes and the partial-failure retry path reclaims. (5 tests in `manager_test.go`.)
- [x] 4.2 Integration test (testcontainer NATS + real-KV-backed responders): `Despawn` actually removes the entity from `ENTITY_STATES` (Get returns `ErrEntityNotFound`). `TestIntegration_Despawn_RemovesEntity`.
- [x] 4.3 `WatchEvents` test: observer receives `Deleted` on reclaim and `Upserted` on create; a companion `Watch` observer receives the upsert but NOT the delete. `TestIntegration_WatchEvents_DeliversUpsertAndDelete`.

## 5. Docs + gates

- [x] 5.1 Reclamation + delete-visible observation usage note added to the `lifecycle` package `doc.go`; the seeded `lifecycle` spec delta (this change's `specs/lifecycle/spec.md`) is the current-truth home for the mechanics and syncs on archive. ADR-047 not re-documented.
- [ ] 5.2 Pre-push gate: `go test -race ./pkg/lifecycle/...` + integration, `go vet` (incl. `-tags=integration`), `gofmt`, `revive` (no new warnings), `task schema:generate` no-drift. semstreams-reviewer pre-merge (new substrate API + RPC error contract on the new delete emit).

## 6. Increment 2 — batch (gh#498), FOLLOW-UP CHANGE (not executed here)

- [ ] 6.1 Scope a follow-up change adding a graph-ingest batch-entity-write handler (`graph.mutation.entity.write_batch`), applying N create/update ops under single-writer + ADR-055 envelope + CAS, with a `graph-ingest` spec delta authored at that time.
- [ ] 6.2 Add emitter `writeBatch` + `Manager.CreateBatch([]Participant)` / `TransitionBatch(...)` that coalesce a wave into one request (do NOT ship a loop-only stand-in — no coalescing = no gh#498 benefit).
- [ ] 6.3 Decide partial-success reporting (per-entity error slice) vs strict all-or-nothing (open question in design.md).
