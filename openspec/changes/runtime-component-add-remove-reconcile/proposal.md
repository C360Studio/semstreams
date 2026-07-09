## Why

Runtime component **add/remove** via the config `Manager`'s own write methods
(`PutComponentToKV` / `DeleteComponentFromKV`) is silently dropped: a component
written to KV at runtime is **never spawned**, and a deleted one is **never torn
down** (gh#388). This blocks the curator runtime add/remove workflow (semsource
ADR-040 external-service source registration) and is timing-dependent — it passes
locally and fails on slower CI runners, which is why it hid.

Two coupled defects in `config/manager.go`:

1. **`handleUpdate` contradicts its own doc.** For an engine-owned revision
   (`revision <= engineHighWaterRev`) it `return`s **before** notifying
   subscribers — but the doc comment promises "Subscribers are notified for both
   engine and external events — the skip only affects the in-memory cache update,
   not subscriber notification." So no `ComponentManager` reconcile fires. This
   bites `DeleteComponentFromKV` precisely when the delete's revision is raised
   under the high-water by a **later** engine PUT (add source-004 after delete
   source-003): the delete event is then treated as engine-owned and skipped
   whole.

2. **`PutComponentToKV`/`DeleteComponentFromKV` never apply in memory.** Their
   docs claim the engine "already applied this change synchronously (write memory
   → write KV)", but they only `kvStore.Put/Delete` + `bumpEngineHighWater`. So
   even once (1) notifies, the notified `update.Config` (= `cm.config`) does not
   reflect the add/remove, and `reconcileComponents` sees no diff → no
   spawn/teardown.

The only path that reconciles today is the heavyweight `PushToKV`, whose
reconcile-*stop* deadlocks (>60s hang) when driven from inside the
`graph.ingest.remove` request handler — a deadlock the issue never fully
root-caused. This change makes the lightweight `PutComponentToKV`/
`DeleteComponentFromKV` path work, so runtime add/remove no longer needs
`PushToKV` from a handler at all — the deadlock is avoided by construction rather
than by an unproven fix.

## What Changes

- **`handleUpdate` honors its contract:** for an engine-owned revision, skip only
  the redundant in-memory re-apply, but **still notify subscribers** (via the
  existing non-blocking per-key send). External revisions are unchanged.
- **`PutComponentToKV` / `DeleteComponentFromKV` apply in memory synchronously**
  (reusing `updateConfig`) before the KV write, making the documented engine
  pattern (write memory → write KV → watcher notifies without re-applying) actually
  true. Runtime add/remove then works through these lightweight methods — no
  `PushToKV` needed.
- **`enableComponent`/`disableComponent` become idempotent** (bundled caller fix):
  D1's notification-on-engine-owned-revisions surfaces a latent ComponentManager
  bug — its per-key handler restarts an already-running component unconditionally.
  `Engine.Start` after `Deploy` re-enables already-enabled components, which would
  now spuriously restart every running component. Fix at the source: skip the
  redundant identical-config KV write when `Enabled` is already at the target.
- Doc comments corrected to match the (now-true) behavior.
- **No signature changes.** The methods keep their signatures; behavior is
  corrected, not extended.

## Capabilities

### Modified Capabilities
- `component-runtime-config`: adds requirements that a runtime component **add**
  and **remove** through the engine write methods drives a reconcile
  (spawn/teardown), and that the engine-owned-revision skip notifies subscribers.
  Distinct from the existing hot-reconfigure-a-running-component requirements.

## Impact

- **`config/manager.go`**: `handleUpdate` (skip → notify), `PutComponentToKV` /
  `DeleteComponentFromKV` (apply in memory). No public API change.
- **`engine/engine.go`**: `enableComponent` / `disableComponent` skip the redundant
  KV write when `Enabled` is already at the target (idempotent). No API change.
- **Follow-up (not in this change):** the `handleComponentConfigUpdate` unconditional
  restart (no config-equality guard; ComponentManager stores no per-component config
  to diff) is filed as gh#514, and the `updateConfig` read-modify-write lost-update
  race (pre-existing class, widened by caller-goroutine `updateConfig` calls) as
  gh#515.
- **Consumers**: semsource curator runtime source add/remove (ADR-040) starts
  working through `PutComponentToKV`/`DeleteComponentFromKV`; any runtime
  add/remove caller benefits. `ComponentManager.reconcileComponents` is unchanged.
- **Related, out of scope:** the `PushToKV`-in-handler reconcile-stop deadlock is
  NOT fixed here (never root-caused). This change makes the non-blocking Put/Delete
  path the supported runtime add/remove route, so callers no longer need `PushToKV`
  from a handler — the deadlock is avoided by construction. If the deadlock is later
  root-caused it gets its own change.

## Non-goals

- Not changing `reconcileComponents` or the `ComponentManager` spawn/teardown
  logic.
- Not changing the engine-high-water watermark mechanism itself (the anti-clobber
  skip of the in-memory re-apply stays; only the missing notification is added).
- Not a general redesign of the config reconcile/notify pipeline.
