# Tasks — content-evidence-retention

## 1. Remove the hard-coded retention (D1)

- [ ] 1.1 Delete the `TTL: 24 * time.Hour` field from the `ObjectStoreConfig` literal in `NewStoreWithConfigAndMetrics` (`storage/objectstore/store.go:114`) — remove the field, do not set `TTL: 0`.
- [ ] 1.2 Grep for any other `ObjectStoreConfig{` / retention-bearing construction and confirm the shared ctor is the only place retention was stamped (no per-site TTL survives).
- [ ] 1.3 Confirm no config surface exposes a TTL knob for content stores (`storage/objectstore/config.go`); the data-cache TTL (`config.go:116`, in-process LRU) is unrelated and stays.

## 2. Reconcile-then-assert boot guard (D2)

- [ ] 2.1 Add an ObjectStore backing-stream retention reader analogous to `BucketRetention` (`natsclient/kv.go`): read `OBJ_<bucket>`'s `Config.MaxAge`/`MaxBytes` via the JetStream context (`OBJ_%s` per nats.go v1.48.0).
- [ ] 2.2 Add the reconcile step: if the backing stream carries binding `MaxAge`/`MaxBytes`, clear it via `UpdateStream` and emit a WARN naming the bucket and removed retention. Reconciliation deletes no stored object.
- [ ] 2.3 Add the assert step reusing the pure `CheckNoLifecycleRetention` (`natsclient/kv.go:158`): after reconcile, if retention is still binding, fail with `ErrGraphBucketRetention` naming the bucket.
- [ ] 2.4 Wire reconcile-then-assert into the shared ctor (or store `Start`) so all three sites — `storage/objectstore/component.go:189`, `processor/agentic-loop/component.go:627`, `processor/graph-embedding/component.go:970` — inherit it. Guard order: reconcile first, then assert.
- [ ] 2.5 Unit-test the reader + pure check (clean stream passes; `MaxAge`/`MaxBytes` set → detected).
- [ ] 2.6 Integration-test (testcontainers) the reconcile-then-assert: legacy `MaxAge` bucket → stripped + WARN + boots; un-strippable retention → fail-closed; clean bucket → boots.

## 3. Fusion body-hydration reporting (D3)

- [ ] 3.1 Add a distinct closed `BodyReason` type in `pkg/fusion` sharing the `not_found`/`error` vocabulary of `UnhydratedReason` (`retrieval.go:100`), plus an omitempty `Node` field carrying it (`contract.go:175`).
- [ ] 3.2 Replace the error-swallowing `if err == nil` body block at `engine_lens.go:349-354`: on `Hydrate` nil/not-found set reason `not_found`; on `ResolveBody` fault set reason `error`; leave `Body` empty; never defer or synthesize a `Miss`.
- [ ] 3.3 Add a `fusion_body_hydration_failures_total{reason}` counter, incremented on each failure (mirror the metering pattern in `engine_graph.go:20-41`).
- [ ] 3.4 Unit-test each scenario in the fusion spec delta: `not_found` vs `error`, partial-result (node present, no defer/miss), hydrated body omits the reason (wire-unchanged), counter increments by reason.
- [ ] 3.5 Add/extend a production-decoder JSON round-trip test proving the new `Node` field is omitempty-absent on a fully hydrated response.

## 4. Optional observability (non-blocking)

- [ ] 4.1 Emit a CONTENT store size/object-count gauge so unbounded growth is observed — the trigger for the deferred orphan-GC increment. Non-blocking; drop if it expands scope.

## 5. Docs, deltas, coordination

- [ ] 5.1 Correct the "ADR-0008" citation to **ADR-068** in `proposal.md` (and anywhere else in this change).
- [ ] 5.2 Update the `graph-retention` spec Purpose (at sync/archive) to note it now also covers content ObjectStores, and confirm the ADR-068 capability text stays consistent.
- [ ] 5.3 File the orphan-GC follow-up (reference-aware refcount/mark-and-sweep, ADR-scale) as its own issue/increment; note it here as out-of-scope for this change.
- [ ] 5.4 Draft a `semstreams-asks` note: content-store retention removed + boot-guarded (inherited via the ctor, no sister code change); fusion `Node` gains an additive body-reason field (wire-compatible, opt-in to read).

## 6. Gate before push

- [ ] 6.1 `task lint` (revive clean), `go test -race ./...`, tagged vet (`integration`, `live_llm`), contract tests.
- [ ] 6.2 `task schema:generate` + `git diff schemas/ specs/` — no drift (config surface changed: TTL field removed).
- [ ] 6.3 Framework-package change sweep: `go test -race -tags=integration ./...` on `storage/objectstore/`, `natsclient/`, `pkg/fusion/` and their consumers.
- [ ] 6.4 Run the fusion-touching e2e tier (semantic — exercises evidence bodies/hydration) green before merge; if no tier covers Fuse/batch/unhydrated, note the #599 gap rather than claiming coverage.
