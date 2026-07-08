## Context

`config.Manager` watches a NATS KV bucket and reconciles component/service config
into the running system. To avoid the watcher re-applying (and clobbering) writes
the engine just made, `Manager` tracks `engineHighWaterRev`: its own write methods
capture the KV `Put` revision and bump the watermark, and `handleUpdate` skips
events at/below it.

`ComponentManager` subscribes via `configManager.OnChange("components.*")` and, in
its single-goroutine `watchConfigUpdates`, calls `reconcileComponents(update.Config)`
— diffing running components against `update.Config` (which is `cm.config`) to
spawn/teardown. So a reconcile is correct only if two things hold when the
notification fires: (a) a notification actually fires, and (b) `cm.config` already
reflects the change.

Today neither holds for the engine write methods, and the interleaving that breaks
delete is subtle: NATS KV revisions are bucket-global and monotonic, so a delete of
`source-003` at rev N followed by an add of `source-004` at rev N+1 leaves the
delete event (rev N) **at/below** the high-water (N+1) — engine-owned — and
`handleUpdate` returns before notifying. The add reconciles; the delete silently
does not.

## Goals / Non-Goals

**Goals**
- Runtime add and remove via `PutComponentToKV`/`DeleteComponentFromKV` reliably
  drive a spawn/teardown reconcile.
- Make the code match its documented "notify for both engine and external events"
  contract.

**Non-Goals**
- Root-causing the `PushToKV`-in-handler reconcile-stop deadlock. The primary fix
  routes runtime ops off `PushToKV` onto the non-blocking Put/Delete path, so the
  deadlock is avoided by construction; fixing it is a separate change if it recurs.
- Changing `reconcileComponents`, `notifySubscribers`, the watermark mechanism, or
  any public signature.

## Decisions

### D1. Engine-owned skip drops only the re-apply, never the notification
`handleUpdate` computes `engineOwned = revision != 0 && revision <= highWater`. When
`engineOwned`, it skips `updateConfig` (the engine already applied it synchronously —
see D2 — so re-applying is redundant and can clobber newer state) but **falls
through to the existing non-blocking subscriber notification**. External revisions
keep both the apply and the notify. This is the literal behavior the current doc
comment already promises; the fix is to stop `return`ing early.

*Why not drop the watermark skip entirely?* The skip exists to prevent the lagging
watcher from re-applying a stale value over newer engine state (the Stop-then-Undeploy
race the comment cites). Only the notification was wrongly suppressed.

### D2. Engine write methods apply in memory synchronously (reuse updateConfig)
`PutComponentToKV` and `DeleteComponentFromKV` apply the change to `cm.config` via
the existing `updateConfig(key, data)` / `updateConfig(key, nil)` **before** the KV
write, then write KV (and, for Put, bump the watermark). This makes the documented
engine pattern true and self-contained: a runtime caller invokes only the one method
and gets both the synchronous apply and the (D1) reconcile notification. `updateConfig`
is the same apply the watcher uses, so put-then-watcher and delete-then-watcher are
idempotent (set-to-same / delete-already-absent).

Ordering (`apply memory → write KV → bump watermark`) is deliberate: if the watcher
event races ahead of the bump, it is treated as external and re-applies idempotently
+ notifies; if it lands after the bump, it is engine-owned and skips the re-apply but
still notifies (D1). Either way: applied exactly once in effect, notified.

*Delete's revision is not captured* (the KV Delete API discards it), so
`DeleteComponentFromKV` does not bump the watermark — unchanged from today. With D2
the in-memory delete is applied synchronously regardless, and D1 guarantees the
notification whether the delete event is later classified engine-owned or external.

### D3. The PushToKV deadlock is avoided by routing around it, not fixed
`notifySubscribers` does a genuinely blocking `ch <- update` (after a stale-drain).
The drain makes it non-blocking only in the single-writer case; `handleUpdate` is a
*concurrent* writer to the same buffer-1 channels, so a send can still block. The
reported `PushToKV`-in-handler reconcile-stop deadlock was never root-caused, and
this change does NOT claim to prove it safe. Rather than ship an unproven
`notifySubscribers` change, this change makes the lightweight, non-blocking
`PutComponentToKV`/`DeleteComponentFromKV` path (D1's `handleUpdate` select-default
send) the supported runtime add/remove route. Callers stop driving runtime remove
through `PushToKV` from a request handler, so the deadlock is **avoided by
routing around it**, not disproven. Deploy/bulk still uses the blocking path; if the
deadlock recurs there it gets its own root-caused change.

### D4. enableComponent/disableComponent are idempotent (bundled caller fix)
D1 makes the config watcher notify on engine-owned revisions — which surfaces a
latent ComponentManager bug: its per-key handler (`handleComponentConfigUpdate`)
restarts an already-running enabled component **unconditionally**, with no
config-equality guard. `Engine.Deploy` writes components `Enabled=true` (spawning
them via the bulk reconcile), then `Engine.Start` re-enables each via
`enableComponent` → `PutComponentToKV`. Pre-D1 that redundant write's event was
dropped (engine-owned skip); post-D1 it notifies and spuriously stop-recreates
every running component on every `Start`. Fix at the source: `enableComponent`/
`disableComponent` no-op when `Enabled` is already at the target (a redundant
identical-config KV write was always wasteful; it was only *harmless* because the
watcher dropped it). This is the footgun-surfaces-latent-bug pattern — bundle the
caller fix with the framework fix. The deeper "make `handleComponentConfigUpdate`
config-equality-idempotent" belongs to the ComponentManager (it stores no per-
component config to diff today) and is left as a follow-up; D4 closes the concrete
reachable regression.

## Risks / Trade-offs

- **[Extra notifications on engine-owned events]** D1 makes every engine write emit a
  reconcile notification (previously suppressed). `reconcileComponents` is idempotent
  (diff-based), so redundant notifications are harmless; the per-key send is
  non-blocking. `PushToKV`'s trailing `notifySubscribers` becomes partly redundant —
  acceptable.
- **[Double-apply window]** The apply-memory-then-write-KV ordering can apply the same
  value twice (once synchronously, once from an external-classified watcher event
  before the bump). `updateConfig` is set-to-value / delete-idempotent, so this is a
  no-op, not a correctness issue.
- **[D3 snapshot vs. live subscriber set]** Sending after releasing the lock means a
  concurrently-removed subscriber could receive one late update on a channel about to
  be dropped. Harmless (reconcile is idempotent; the reader is draining anyway) and no
  worse than the existing race window.

## Migration Plan

Purely a behavior correction; no migration, no signature change. semsource retires
its `PushToKV`-in-handler remove workaround in favor of `DeleteComponentFromKV`.
Rollback is a straight revert.

## Open Questions

- Should `PushToKV`'s trailing `notifySubscribers` be dropped now that per-key engine
  writes notify (D1)? Left in place this change (belt-and-suspenders for the bulk
  initial-setup path); can be revisited if the redundant reconcile shows up in
  profiling.
