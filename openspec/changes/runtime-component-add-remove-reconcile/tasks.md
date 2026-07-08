# Tasks

## 1. handleUpdate: skip re-apply, still notify (D1)

- [x] 1.1 In `config/manager.go` `handleUpdate`, replace the early `return` on the engine-owned-revision branch with: skip `updateConfig` (the in-memory re-apply) but fall through to the existing non-blocking subscriber-notification loop. External revisions keep both apply + notify.
- [x] 1.2 Fix the `handleUpdate` doc comment to describe the actual behavior (engine-owned → skip re-apply, still notify).

## 2. Engine write methods apply in memory (D2)

- [x] 2.1 `PutComponentToKV`: apply the component to `cm.config` via `updateConfig(key, data)` after a successful `kvStore.Put` (KV-first so a failed Put leaves memory untouched), then bump the watermark. Doc comment updated.
- [x] 2.2 `DeleteComponentFromKV`: apply the in-memory delete via `updateConfig(key, nil)` after `kvStore.Delete` (and on the already-absent path). Doc comment updated.
- [x] 2.3 Idempotency confirmed: put-then-watcher (set-to-same via `updateConfig`) and delete-then-watcher (delete-already-absent) are no-ops — the engine-owned watcher event skips re-apply anyway (D1), and an external-classified event applies idempotently.

## 2b. Engine idempotency (D4 — bundled caller fix for the surfaced latent bug)

- [x] 2b.1 `engine/engine.go` `enableComponent`: no-op (return nil, no KV write) when the component is already `Enabled=true`. Prevents `Start`-after-`Deploy` from redundantly re-writing already-enabled components, which post-D1 would spuriously restart every running component via `handleComponentConfigUpdate`'s unconditional restart.
- [x] 2b.2 `disableComponent`: symmetric no-op when already `Enabled=false`.

## 3. Tests

- [x] 3.1 Unit: `TestHandleUpdate_EngineOwnedRevisionStillNotifies` — engine-owned revision notifies a subscriber AND does not re-apply in memory. (Docker-free, `config/manager_reconcile_test.go`.)
- [x] 3.2 Unit: `TestHandleUpdate_ExternalRevisionAppliesAndNotifies` — external revision applies AND notifies (regression).
- [x] 3.3 Integration: `TestRuntimeComponentAdd_AppliesAndReconciles` — `PutComponentToKV` applies in memory synchronously and delivers a `components.*` Update carrying the component.
- [x] 3.4 Integration: `TestRuntimeComponentRemove_AppliesAndReconciles` — `DeleteComponentFromKV` removes in memory synchronously and delivers a `components.*` Update without the component. (The interleaved-delete-under-high-water reconcile is covered at unit level by 3.1: an engine-owned event still notifies.)
- [x] 3.5 Integration (engine suite): `TestStart_AfterDeploy_DoesNotRewriteAlreadyEnabledComponents` — `Start` after `Deploy` emits NO redundant per-component write (idempotent enable). Verified RED without the 2b guard (fails: "Start re-wrote an already-enabled component"), GREEN with it.

## 4. Docs + gates

- [x] 4.1 `go test -race ./config/` (green) + integration (my tests green; two unrelated tests hit transient testcontainer start-timeouts under local Docker pressure — confirmed infra, pass in 3.3s on re-run after `docker builder prune`), `go vet` both tags (clean), `gofmt` (clean), `revive` via `task lint` (0 warnings), `task schema:generate` no-drift. semstreams-reviewer pre-merge: pending.
