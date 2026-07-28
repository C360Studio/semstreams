# Design: reopen framework-owned bucket guards

## Context

Both P0s were confirmed by direct code inspection (2026-07-28):

- `service/component_manager.go:401-419` — `startAllComponents` fires `go startComponentAsync(...)` per
  component and returns; `ComponentManager.Start` (`:324-372`) therefore returns before any component
  `Start` has run. The "post-start" sweep at `service/service_manager.go:312` races component bucket
  create/adopt; its seam comment ("after every owner holds its handle") is false.
- `service/component_manager.go:428-435` — a component `Start` error sets `StateFailed` + logs;
  `RegisterComponentErrorHook` (`:988`) has no production caller; `performDetailedHealthCheck`
  (`:934-954`) checks only nil-implementation and cancelled-context, and a failed `Start` does not cancel
  the context. Failed components are invisible to boot and to health.
- `processor/graph-embedding/component.go:650,661` — `c.embeddingBucket` is created, stored, and never
  used again in non-test code ("we are the WRITER" is a stale prediction). Gateway/clustering "readers"
  exist only in doc comments.

Owner directive: **clean break, no deprecated code** — pre-v1, delete wrong behavior rather than flag,
shim, or deprecate it.

## Decision 1 — Component-start barrier in `ComponentManager.Start`

`ComponentManager.Start` keeps the parallel goroutine launch (startup latency) but waits for every
component `Start` call to return before it itself returns, and returns `errors.Join` of all failures.

- Mechanism: `startAllComponents` keeps its own `sync.WaitGroup` scoped to the launch batch (the existing
  `cm.wg` also tracks long-lived loops — do not conflate them) plus an error channel / mutex-guarded
  slice; `Start` waits on the batch, joins errors, and fails.
- Component `Start` contracts are init-and-return (state machine stamps `StateStarted` only after `Start`
  returns), so the wait is well-defined. A component that blocks in `Start` now blocks boot — that is
  fail-closed, and the lifecycle ctx bounds it; a hang is a component bug to fix, not to route around.
- The barrier restores the ordering the post-start sweep's spec text already claims. No sweep code
  changes; only the ordering that makes it true.

**Alternative rejected (for this increment):** enforcing retention at bucket acquisition
(`EnsureFrameworkBucket(spec)` + descriptor catalog). That is the durable home and the next Epic C
increment; it is a larger structural migration (every creation site, catalog derivation for
creation/retention/write-guards/diagnostics). The barrier is required regardless — boot must not report
success while components failed — and the sweep demotes to a legacy-drift backstop after that migration.

**Clean break:** the old fire-and-forget semantics are deleted. No opt-out flag, no
`StartAsync` variant kept for compatibility.

## Decision 2 — Failure propagation and health truth

- **Boot**: barrier errors propagate `ComponentManager.Start` → `Manager.StartAll` → composition root.
  HTTP setup (`completeHTTPSetup`) is never reached; the process exits non-zero. This also makes
  graph-ingest's create-time retention refusal (graph-retention spec, existing scenario) true at the
  process level for the first time.
- **Post-boot** (dynamic config add/restart paths, e.g. `component_manager.go:613,1708`): a `Start`
  failure cannot crash the process; it records `StateFailed` + `LastError`, and
  `performDetailedHealthCheck` gains a `StateFailed` check returning an error naming the component and
  its `LastError`. No warn-and-continue.
- **`RegisterComponentErrorHook`**: sweep semstreams + sisters (semsource, semconnect, semboids,
  semspec) for callers. No consumer → delete the hook and the field (grep-for-the-consumer; boot
  propagation supersedes it). A real consumer found → keep and document; do not leave an unwired
  exported hook either way.

## Decision 3 — Delete `EMBEDDINGS_CACHE` entirely

- Delete: `graph.BucketEmbeddingsCache` constant; its membership in `FrameworkOwnedBuckets()`;
  `retentionGuardedBuckets()` (the sweep ranges `FrameworkOwnedBuckets()` directly — the guard's only
  exception dies with the dead surface); creation in graph-embedding `Start`
  (`component.go:649-661` + the `embeddingBucket` field); the config validation requiring the
  `EMBEDDINGS_CACHE` output (`component.go:106-115`) and the default-config output entries
  (`component.go:217,244`); stale doc.go/README/concepts references.
- **Pre-deletion gate**: grep sisters and in-repo configs (YAML/JSON, e2e/docker) for
  `EMBEDDINGS_CACHE`. A reader of a never-written bucket is vestigial by construction, but a hit must be
  surfaced, not silently broken (real consumers live in sister repos).
- Bucket cleanup in running deployments: the orphaned `KV_EMBEDDINGS_CACHE` bucket is inert (nothing
  reads or writes it); note manual `nats kv rm` in the adopter note (`rm` removes the bucket;
  `del` deletes a key and requires a key argument). No migration code (clean break).
- Generated schemas may change (output no longer required/emitted in defaults): run
  `task schema:generate`, commit drift.

## Decision 4 — Production-wire tests (the concurrency shape)

The sync-mock test (`framework_owned_bucket_guards_integration_test.go`) proved an ordering guarantee
production does not have. It is replaced, not kept alongside:

1. **Create-race, real wire**: register a real lifecycle component through `ComponentManager` (the
   production async path) whose `Start` get-or-creates a guarded bucket pre-created dirty; assert the
   post-start sweep strips the TTL after the barrier, preserves keys, and WARNs — driving
   `Manager.StartAll` with the real `ComponentManager`, not a mock service.
2. **Boot fails closed**: a component whose `Start` returns an error ⇒ `StartAll` returns an error
   naming it; HTTP is never brought up.
3. **Health truth**: a component moved to `StateFailed` post-boot ⇒ health check returns an error naming
   the component; recovery clears it.

All race-enabled, explicit synchronization (no sleeps). `service/` is a framework package ⇒ run the
branch integration sweep (`go test -race -tags=integration ./...`).

## Decision 5 — Boot-boundary config drain (Codex round on PR #719)

Deferring the config watcher past the barrier closed the parked-StateInitialized hole but left two
boot-integrity gaps: (1) a mid-boot update's component started on the watcher's DETACHED dynamic
path after the post-start retention sweep — reopening the create-race for exactly that component —
and (2) the cap-1 drop-on-full OnChange buffers could LOSE mid-boot changes outright (a dropped
model_registry change stayed unapplied until the NEXT registry change; a dropped component edit
until the next notification; the initial-snapshot bulk reconcile skips existing components, so it
never healed an edit).

Closure: a **synchronous coalesced boot-boundary transaction**. After the batch barrier,
`ComponentManager.Start` runs a drain loop before returning: each pass consumes whatever the
buffered channels hold and reconciles against the LIVE SafeConfig (state, not events — a dropped
notification cannot hide a change), applying with barrier semantics:

- components: an edit-aware bulk reconcile (shared core with the watcher's `reconcileComponents`,
  extended with a boot mode rather than a parallel reconciler) — new components created and
  barrier-started (failures join boot failure), edits applied by rebuild + barrier start (a rebuild
  failure fails boot: the old instance is already stopped), removals honored as the watcher's
  reconcile does; rule packs stay immutable in-process.
- model_registry: apply-if-different against the baseline captured at Initialize (the registry
  components were built against) — content drift rebuilds `DepModelRegistry` dependents against the
  live registry, barrier-started. The watcher's entry backlog check and its per-event handling use
  the same apply-if-different, so a change landing between the final drain pass and watcher start
  is applied, never discarded, and the initial snapshot never causes a restart storm.
- Quiescence: a pass that drains no events and applies no change terminates the loop. Pathological
  churn is bounded by the lifecycle ctx (cancellation fails boot with the ctx error), not a silent
  pass cap.

**Cutoff honesty**: updates whose local application lands after the final drain pass — component
ADDS and EDITS alike — are POST-BOOT dynamic changes, microsecond-class identical to ones arriving
just after `Start` returns. An add's component starts, and an edit's component restarts (releasing
and re-acquiring its buckets), through the dynamic path after the sweep — outside the boot sweep's
boot-time enforcement scope; the acquisition-seam increment (`EnsureFrameworkBucket`, next Epic C
increment) is the durable closure for that whole class. Boot-time CREATE failures in the drain are
logged and excluded from the boot set (Initialize's best-effort creation posture); `Start`
failures remain fail-closed, and an edit-rebuild failure fails boot (the old instance is already
stopped).

## Risks

- **Boot-time behavior change is intentionally breaking**: deployments with a component that fails
  `Start` today (silently, serving HTTP) will stop booting. That is the fix. Sister lockstep covers the
  blast radius; e2e tiers exercise real boots.
- **Startup latency**: barrier adds max(component Start) to `StartAll` — previously unwaited work that
  happened anyway; HTTP readiness moves later but becomes honest.
- **A component with a genuinely slow `Start`** (e.g. waiting on external service) now delays boot.
  Acceptable pre-v1; the lifecycle ctx bounds it; slowness becomes visible instead of hidden.
