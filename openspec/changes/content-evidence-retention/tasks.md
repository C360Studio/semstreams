# Tasks — content-evidence-retention

## 1. Remove the hard-coded retention (D1)

- [x] 1.1 Delete the `TTL: 24 * time.Hour` field from the `ObjectStoreConfig` literal in `NewStoreWithConfigAndMetrics` (`storage/objectstore/store.go:114`) — remove the field, do not set `TTL: 0`.
- [x] 1.2 Grep for any other `ObjectStoreConfig{` / retention-bearing construction and confirm the shared ctor is the only place retention was stamped (no per-site TTL survives).
- [x] 1.3 Confirm no config surface exposes a TTL knob for content stores (`storage/objectstore/config.go`); the data-cache TTL (`config.go:116`, in-process LRU) is unrelated and stays.

## 2. Reconcile-then-assert boot guard (D2)

- [x] 2.1 Add an ObjectStore backing-stream retention reader analogous to `BucketRetention` (`natsclient/kv.go`): read `OBJ_<bucket>`'s `Config.MaxAge`/`MaxBytes` via the JetStream context (`OBJ_%s` per nats.go v1.48.0).
- [x] 2.2 Add the reconcile step: if the backing stream carries binding `MaxAge`/`MaxBytes`, clear it via `UpdateStream` and emit a WARN naming the bucket and removed retention. Reconciliation deletes no stored object.
- [x] 2.3 Add the assert step reusing the pure `CheckNoLifecycleRetention` (`natsclient/kv.go:158`): after reconcile, if retention is still binding, fail with `ErrGraphBucketRetention` naming the bucket.
- [x] 2.4 Wire reconcile-then-assert into the shared ctor (or store `Start`) so all three sites — `storage/objectstore/component.go:189`, `processor/agentic-loop/component.go:627`, `processor/graph-embedding/component.go:970` — inherit it. Guard order: reconcile first, then assert.
- [x] 2.5 Unit-test the reader + pure check (clean stream passes; `MaxAge`/`MaxBytes` set → detected).
- [x] 2.6 Integration-test (testcontainers) the reconcile-then-assert happy paths: legacy `MaxAge` bucket → stripped + WARN + boots, stored object survives, clean bucket → boots. (Fail-closed on un-strippable retention is covered at the **unit** tier via a fake that denies `UpdateStream` — a real NATS bucket we own won't deny the update.)
- [x] 2.7 Reconcile: short-circuit `return nil` when the first `CheckNoLifecycleRetention` passes on a clean store, so a clean boot does not do a redundant second backing-stream read (reviewer LOW-1).

## 3. Fusion body-hydration reporting (D3)

- [x] 3.1 Add a distinct closed `BodyReason` type in `pkg/fusion` sharing the `not_found`/`error` vocabulary of `UnhydratedReason` (`retrieval.go:100`), plus an omitempty `Node` field carrying it (`contract.go:175`).
- [x] 3.2 Replace the error-swallowing body block in `nodeFor` (`engine_lens.go`) with the spec-correct seam: `Hydrate` err → `error`; `Hydrate` nil ref (no body) → **silent** (no reason, no counter); `ResolveBody` `storage.ErrObjectNotFound` → `not_found`; other `ResolveBody` err → `error`. Leave `Body` empty on failure; never defer or synthesize a `Miss`.
- [x] 3.3 Add a `fusion_body_hydration_failures_total{reason}` counter, incremented on each failure.
- [x] 3.4 Unit-test each fusion spec scenario: present-ref→absent-object → `not_found`; read-fault → `error`; body-less entity → silent (no reason, no counter); partial-result (node present, no defer/miss); hydrated body omits the reason (wire-unchanged); counter increments by reason.
- [x] 3.5 Add/extend a production-decoder JSON round-trip test proving the new `Node` field is omitempty-absent on a fully hydrated response.
- [x] 3.6 Add a backend-agnostic `storage.ErrObjectNotFound` sentinel; `objectstore.Store.Get` returns it (via `errors.Is(err, jetstream.ErrObjectNotFound)`) instead of classifying a not-found as transient, so fusion can distinguish absent-object (`not_found`) from a read fault (`error`). Import cycle verified clean (`storage` does not depend on `objectstore`).
- [x] 3.7 Resolve the body-hydration counter per-registry on the `Engine` (drop the process-global `sync.Once` that pinned it to the first registry); registers directly on the underlying `Registerer` catching `AlreadyRegisteredError` (the `MetricsRegistry.RegisterCounterVec` wrapper doesn't return the existing collector); `WithMetrics` doc updated (reviewer MEDIUM-1).

## 4. Optional observability (non-blocking)

- [ ] 4.1 Emit a CONTENT store size/object-count gauge so unbounded growth is observed — the trigger for the deferred orphan-GC increment. Non-blocking; drop if it expands scope.

## 5. Docs, deltas, coordination

- [x] 5.1 Correct the "ADR-0008" citation to **ADR-068** in `proposal.md` (3 refs); also fixed the substance — the orphaned-blob GC is ADR-068 **increment 6**, not "open item #5 / per-source retention".
- [ ] 5.2 Update the `graph-retention` spec Purpose (at sync/archive) to note it now also covers content ObjectStores, and confirm the ADR-068 capability text stays consistent.
- [ ] 5.3 File the orphan-GC follow-up (reference-aware refcount/mark-and-sweep, ADR-scale) as its own issue/increment; note it here as out-of-scope for this change.
- [ ] 5.4 Draft a `semstreams-asks` note: content-store retention removed + boot-guarded (inherited via the ctor, no sister code change); fusion `Node` gains an additive body-reason field (wire-compatible, opt-in to read).

## 6. Gate before push

- [ ] 6.1 `task lint` (revive clean), `go test -race ./...`, tagged vet (`integration`, `live_llm`), contract tests.
- [ ] 6.2 `task schema:generate` + `git diff schemas/ specs/` — no drift (config surface changed: TTL field removed).
- [ ] 6.3 Framework-package change sweep: `go test -race -tags=integration ./...` on `storage/objectstore/`, `natsclient/`, `pkg/fusion/` and their consumers.
- [ ] 6.4 Run the fusion-touching e2e tier (semantic — exercises evidence bodies/hydration) green before merge; if no tier covers Fuse/batch/unhydrated, note the #599 gap rather than claiming coverage.
