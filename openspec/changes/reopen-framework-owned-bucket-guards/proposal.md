# Reopen: framework-owned bucket guards — boot ordering, failure propagation, dead-surface deletion

## Why

The merged framework-owned-bucket guard (#622, PR #716 `d03c49f7`) claims a coverage guarantee its boot
sequence does not provide. A Codex retrospective review found, and code inspection confirmed, two P0
correctness holes: (1) the "post-start" retention sweep is not post-component-start —
`ComponentManager.Start` launches component `Start` calls in goroutines and returns immediately, so the
sweep at the tail of `Manager.StartAll` races the very bucket create/adopt it exists to reconcile; and
(2) a component `Start` failure is recorded and logged but never propagated — `RegisterComponentErrorHook`
has no production caller and the health check ignores `StateFailed` — so every component-level fail-closed
assertion (including graph-ingest's retention refusal) fails open at the process level: the process serves
HTTP and reports healthy. The next Epic C increments (#625/#629) consume this guard's semantics and are
paused until the guarantee is real.

## What Changes

- **BREAKING** — `ComponentManager.Start` becomes a component-start barrier: components still launch in
  parallel, but `Start` returns only after every component `Start` call has completed, and returns the
  joined errors of any that failed. Boot fails closed on any component start failure. The old
  fire-and-forget behavior is deleted, not flagged or deprecated (pre-v1 clean-break directive).
- Component start failures surface everywhere they must: `Manager.StartAll` fails (process exits non-zero,
  HTTP never comes up) at boot; post-boot dynamic starts record `StateFailed` and the health check reports
  a failed component (name + error) instead of ignoring it.
- The post-start owned-bucket retention sweep now genuinely runs after every owning component holds its
  bucket handle (the barrier restores the ordering the guard's spec text already claims).
- **BREAKING** — delete the dead `EMBEDDINGS_CACHE` surface: the bucket constant, its creation in
  graph-embedding `Start`, the config validation that requires it as an output, and the retention guard's
  special-case exemption (`retentionGuardedBuckets()` collapses into `FrameworkOwnedBuckets()`). The
  handle is created and never read or written by any non-test code; the guard's only exception exists to
  protect a dead surface. Sister configs that declare the `EMBEDDINGS_CACHE` output must drop it.
- Replace the synchronous-mock integration test with production-wire tests that drive the real
  asynchronous `ComponentManager` (the production concurrency shape), covering: create-race reconcile
  after the barrier, boot failure on component `Start` error, and health reporting `StateFailed`.
- Delete `RegisterComponentErrorHook` if the sister sweep confirms no consumer (grep-for-the-consumer);
  otherwise wire it for real.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-retention` — the "live graph carries no lifecycle retention" requirement's enforcement
  description changes: the post-start coverage pass is ordered by the component-start barrier (previously
  claimed but not provided), the `EMBEDDINGS_CACHE` exclusion disappears with the bucket, and the
  graph-ingest create-time refusal scenario now holds at the process level (its `Start` error fails boot).
- `framework-composition` — new requirement: component start failures fail boot closed and surface in
  health. This generalizes the existing "incomplete protocol behavior is never reported as success"
  posture to the composition root itself.

`graph-embedding` needs no spec delta: its spec never referenced `EMBEDDINGS_CACHE` (the code surface was
never specced — consistent with it being dead).

## Impact

- **Code**: `service/component_manager.go` (barrier, error join, health `StateFailed`),
  `service/service_manager.go` (StartAll comment/ordering truth), `graph/constants.go` +
  `graph/owned_bucket_retention.go` (owned-set collapse, exemption deletion),
  `processor/graph-embedding/component.go` + `doc.go` (cache creation/validation/doc deletion),
  `service/framework_owned_bucket_guards_integration_test.go` (replaced by production-wire tests).
- **Configs**: any config declaring graph-embedding's `EMBEDDINGS_CACHE` output — in-repo e2e/example
  configs and sister repos — must drop it. Generated schemas may change (`task schema:generate`).
- **sem\* consumers**: semsource, semboids, semconnect, semspec all boot through `Manager.StartAll` and
  inherit fail-closed boot + honest health. Sister configs are lockstep-updated per the pre-v1 breaking
  wave posture (greenfield + cross-product = break now).
- **Process**: BREAKING ⇒ at least one relevant e2e tier green before merge (`e2e:statistical` minimum;
  `e2e:semantic` recommended since graph-embedding `Start` changes).

## Non-goals

- **Acquisition-seam enforcement** (`EnsureFrameworkBucket(spec)`) and the **bucket descriptor catalog**
  (name · owner · class · retention · write-policy) — that is the next Epic C increment; this change only
  makes the existing two-pass guarantee true. The barrier remains correct after that migration (the sweep
  demotes to a legacy-drift backstop).
- The adopter-facing module contract, `--validate` performing real registry composition, and the
  first-processor tutorial rewrite — sequenced after the substrate simplifies (rewriting docs now would
  document the current surface, not the target one).
- Continuous (runtime) retention reconciliation — enforcement scope stays boot-time, as the spec already
  states.
- Any predicate/vocabulary surface change (Epic C standing constraint).
- #625 (embedding cleanup repair loop) and #629 (coalescer resurrection) — explicitly paused on this
  change; they consume the guard's semantics.
