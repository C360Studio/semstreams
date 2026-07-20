## Context

Two config-state defects in the same theme (single authoritative writer/source):

**gh#515 — lost update in `config.Manager`.** `updateConfig` (`config/manager.go:510-609`)
reads a clone (`cm.config.Get()`, :531), mutates it, and swaps it back
(`cm.config.Update()`, :608) with no lock held across the pair. `SafeConfig` guards
each access individually (so `-race` is silent), but the compound RMW is not atomic.
The KV watcher goroutine and a caller goroutine (`PutComponentToKV`/`DeleteComponentFromKV`,
added by gh#388) can each clone the same base, mutate different keys, and the second
`Update` clobbers the first → a dropped component. `cm.mu` exists but is documented
"Protects subscribers map" (`:33`) and the notify path holds `cm.mu.RLock()` (`:460`).

**gh#522 — stale read in `service.ComponentManager`.** `componentConfigs`
(`component_manager.go:46`) is written only at construction (`:181`) and live PUT
(`component_manager_http.go`), NOT on the KV-restart path (`restartComponentWithNewConfig`
→ `CreateComponent` refreshes `ManagedComponent.Config`, never `componentConfigs`).
`GET /config/<component>` returns the body from `componentConfigs`, so after a
KV-driven restart it returns the stale pre-restart body. `mc.Config` (added by gh#520)
is the field refreshed on all three write paths.

## Goals / Non-Goals

**Goals:**

- A concurrent runtime component add/remove is never lost (serialize the config-map RMW).
- `GET /config/<component>` reflects the effective (running) config, including after a
  KV-watch-driven restart — one source of truth.
- Complete the ComponentManager config-state cluster so the next config change doesn't
  spawn another one-off bug.

**Non-Goals:**

- No change to `GET /config` *semantics* (it already returns effective/running config —
  it reflects a live-PUT that hasn't been persisted; we keep that, just make it correct
  across restart too).
- Durable persistence of runtime config (still out of scope, gh#388).
- The idempotency guard itself (shipped in gh#520).

## Decisions

### D1 — Serialize the RMW inside `SafeConfig` (not `config.Manager`), via a `Mutate(fn)` primitive

The lost update is NOT confined to `config.Manager`: the engine (`engine/engine.go`)
holds the **same** `SafeConfig` instance (`e.configMgr.GetConfig()`) and does its own
`Get → mutate → Update` at five sites (`writeComponentConfigs`, `writeToKV`,
`enableComponent`, `disableComponent`, `deleteComponentConfig`). A `config.Manager`-local
mutex would not serialize against those. So the serialization must live on the shared
object: `SafeConfig`.

Add `SafeConfig.Mutate(fn func(*Config) error) error` that holds the SafeConfig write
lock across the whole read-modify-write: clone the current config under the lock, run
`fn` on the private draft, validate, and swap it in — all before releasing. Every RMW
site routes through it:
```go
func (sc *SafeConfig) Mutate(fn func(*Config) error) error {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    draft := sc.config.Clone()
    if err := fn(draft); err != nil { return err }
    if err := draft.Validate(); err != nil { return fmt.Errorf("config validation failed: %w", err) }
    sc.config = draft
    return nil
}
```
This is strictly better than the current pattern even ignoring the race: the engine's
idempotent-enable/disable *read-decide* (`if compConfig.Enabled { return nil }`) now runs
inside the lock on the authoritative current state, not on a clone that may be stale by
the swap.

**Re-entrancy contract (the deadlock rule):** `fn` MUST NOT call any `SafeConfig` method
(`Get`/`Update`/`Mutate`) — it operates only on the draft it is handed. Post-mutation KV
writes (`PushToKV`/`PutComponentToKV`/`DeleteComponentFromKV`) and subscriber
notification run OUTSIDE `Mutate` (after it returns and the lock is released), exactly as
today — so no path re-acquires the write lock while holding it. `updateConfig` does not
notify (the watcher does, after it returns); the engine sites call KV writes after their
`Update`, which move to after `Mutate`. Migration must preserve that ordering. Values the
post-mutation KV write needs (e.g. the enabled `compConfig`) are captured into a variable
inside `fn`.

Migrate: `Manager.updateConfig` (the switch becomes the `fn` body) and all five engine
RMW sites. `Update` stays for whole-config replacement callers that already have the final
value; `Mutate` is for read-modify-write.

### D2 — `GET /config` derives from `ManagedComponent.Config`; retire `componentConfigs` as a read source

Point the config-read handlers (`component_manager_http.go` GET sites) at
`cm.components[name].Config.Config` under the component lock, so the read follows the
field refreshed on all three write paths. This fixes the stale-after-restart body.

**Subtlety to resolve (the one real design fork):** `componentConfigs` holds configs for
*all configured* components, including disabled / not-yet-created ones that have no
`ManagedComponent`. `mc.Config` covers only *running* components. Options for a
configured-but-not-running component's `GET /config`:
- **(a) Fall back to the config Manager's desired config** (`cm`'s reference to the
  config source) — GET returns the desired config for a not-running component, effective
  for a running one. Preferred: one read path, no redundant `componentConfigs` copy.
- **(b) Keep `componentConfigs` only as the not-running fallback**, reading `mc.Config`
  first when the component is running. Smaller diff, but leaves the two-copies smell.

Decision (refined after auditing every reader): **all four runtime HTTP readers
(`:339` list, `:530` status, `:599` GET-config body, `:654` PUT-validation) are gated on
the component being in `cm.components`, so `mc.Config` is available at each** — the
"not-running component" case does not occur on these paths. Switch those four to
`mc.Config`. `componentConfigs` is NOT fully dropped: `Initialize` legitimately iterates
it over the full configured set **including disabled/not-yet-created components** to
bootstrap creation, which `mc.Config` (running only) cannot supply. So the roles separate
cleanly: `componentConfigs` = the boot desired-set (effectively immutable after boot,
also read by `isInput/isStorage` for the stable `Type`); `mc.Config` = the single mutable
runtime source of truth for effective config, refreshed on all three write paths. Remove
the runtime `componentConfigs` write from the PUT handler (the gh#520-era redundancy) so
it can no longer drift; keep the `mc.Config` PUT refresh. `isInput/isStorage` keep reading
`componentConfigs.Type` (stable, boot-set) — out of #522 scope.

## Risks / Trade-offs

- **`configMu` contention**: config mutations are rare (operator/sync-driven), so a coarse
  mutation lock is fine; it is never held on a hot path. The correctness win (no lost
  add/remove) dominates.
- **Holding `configMu` across `SafeConfig.Update`**: acceptable as long as `Update` is the
  documented atomic swap and does not call back into `configMu`; verified by D1's deadlock
  check.
- **Dropping `componentConfigs` (D2 option a)**: risk that a GET reader depended on a
  config for a not-running component that `mc.Config` lacks — mitigated by the
  desired-config fallback and the per-reader audit; option (b) is the safety net.
- **Testing the lost-update race**: `-race` won't catch it (higher-level atomicity), so the
  regression test must drive concurrent add + remove goroutines through the real
  `updateConfig`/`PutComponentToKV` sites and assert both survived (not a `-race` assertion).
