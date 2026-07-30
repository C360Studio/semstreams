## Context

`ComponentManager.handleComponentConfigUpdate` (`service/component_manager.go:1116`)
restarts an existing enabled component unconditionally on every per-component config
notification — there is no comparison against what the component is currently
running. `restartComponentWithNewConfig` is a full teardown/rebuild (Stop(30s) →
cancel ctx → deregister stores → unregister ports → UnregisterInstance → recreate →
start). The bulk `reconcileComponents` path is already conservative and does not
restart running components; only the per-component individual-notification path
churns.

`ManagedComponent` (`component/lifecycle.go:56`) retains `State` but not the config
the component was created from, so the manager has nothing to compare against.

`types.ComponentConfig` is `{Type, Name, Enabled, Config json.RawMessage}`. The
`Config` field is raw JSON bytes — this is the crux of the equality decision below.

Coordinated shutdown: `Manager.StopAll` (`service/service_manager.go:441`)
aggregates *any* `service.Stop` error into a fatal result. `BaseService.Stop`
(`service/base.go:267`) already returns `nil` on `StatusStopped`/`StatusStopping`,
and `ComponentManager.Stop` is idempotent — so the framework mostly satisfies the
contract today. The gap is defensive: a non-BaseService `Stop`, or a service that
surfaces an "already stopped" error, would still be aggregated as fatal.

## Goals / Non-Goals

**Goals:**

- A per-component config update restarts a running component only when its effective
  config actually changed; an unchanged update is a logged skip.
- Config equality is robust to JSON re-serialization (whitespace / key ordering) so
  a full-config sync that re-marshals from the in-memory struct does not read as
  "changed."
- `StopAll` never fails a clean shutdown on an already-stopped/stopping service, and
  the per-service idempotent-`Stop` contract is explicit and covered by a test.

**Non-Goals:**

- gh#515 `config.Manager.updateConfig` read-modify-write lost-update race (separate).
- Durable persistence of runtime config (already out of scope per gh#388).
- Changing the bulk `reconcileComponents` semantics (already conservative).
- Any change to the `PUT config/<component>` hot-reconfig contract (that path calls
  `UpdateConfig`/reconfig method pair, not restart).

## Decisions

### D1 — Compare effective config with canonical JSON equality, not `reflect.DeepEqual`

`ComponentConfig.Config` is `json.RawMessage`. `reflect.DeepEqual` (or raw `bytes.Equal`)
compares bytes, so two semantically identical configs that differ only in whitespace
or key order — exactly what a re-marshal during a full-config sync produces — would
compare as **not equal** and still trigger a spurious restart, defeating the fix.

Decision: a `ComponentConfig.Equal(other)` (or a package-local helper) that compares
`Type`, `Name`, `Enabled` by value and `Config` by **canonicalized JSON**: compact
both raw messages (`json.Compact`) and, to be order-insensitive, compare via
`json.Marshal(json.Unmarshal(...))` into `any` or an equivalent canonical form. An
empty/`null`/absent `Config` compares equal to another empty/`null`. Malformed JSON
on either side falls back to raw-byte compare (a malformed config is not something we
want to silently treat as equal). Put the helper next to `ComponentConfig` in
`types/component.go` so it is reusable and unit-testable in isolation.

### D2 — Retain effective config on `ManagedComponent`, set at create/restart

Add `Config types.ComponentConfig` to `ManagedComponent` and populate it wherever a
managed component is constructed (`CreateComponent` and the recreate step inside
`restartComponentWithNewConfig`). The compare in `handleComponentConfigUpdate` reads
`existingComp.Config` under the same lock discipline already used to fetch
`existingComp`. This keeps the retained config co-located with the instance it
describes (no parallel map to keep in sync across delete/recreate).

Alternative considered: a `map[string]ComponentConfig` on the ComponentManager.
Rejected — a second structure to keep consistent with `cm.components` across every
create/delete/restart, more race surface for no benefit.

### D3 — Guard placement: in `handleComponentConfigUpdate`, before restart

The equality check goes in the `cfg.Enabled && exists` branch only. If
`existingComp.Config.Equal(cfg)` → log a debug "config unchanged, skipping restart"
and return. Otherwise proceed to `restartComponentWithNewConfig` exactly as today.
Create-missing and disable/remove branches are untouched. This is the narrowest
correct placement and leaves the reconcile path alone.

### D4 — StopAll idempotency: classify already-stopped as success, keep genuine errors

Two-part: (a) make the per-service `Stop` idempotency contract explicit — it is
already satisfied by `BaseService.Stop`; add a contract test asserting a second
`Stop` (and a `Stop` after a self-transition to stopping) returns `nil`. (b) Harden
`StopAll`: rather than trust every service to embed BaseService, treat an
"already-stopped/stopping" signal as success in the aggregation. Prefer a typed
sentinel (`service.ErrAlreadyStopped`) that a `Stop` MAY return and that `StopAll`
recognizes via `errors.Is` and does not aggregate; a `nil` return remains success as
today. This keeps genuine failures (real teardown errors) aggregated and surfaced.

## Risks / Trade-offs

- **Canonical-JSON compare cost**: runs only on per-component config notifications
  (rare, operator/sync-driven), not on the hot path — negligible. The correctness win
  (no spurious restart / no mux panic) dominates.
- **False-negative equality → missed restart**: if the canonical compare wrongly
  reported equal, a real config change would be ignored. Mitigated by comparing all
  four fields and falling back to raw-byte compare on malformed JSON; covered by a
  "changed config restarts exactly once" test.
- **StopAll sentinel scope**: introducing `ErrAlreadyStopped` is additive; existing
  `nil`-returning idempotent Stops keep working unchanged. Low risk.
- **Concurrency**: the retained `ManagedComponent.Config` is read/written under the
  existing `cm.mu` discipline; no new lock. The compare happens outside the lock on a
  copied value, same as the existing `existingComp` snapshot pattern.
