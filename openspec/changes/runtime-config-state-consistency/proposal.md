## Why

Runtime config state has two consistency defects in the same theme — **no single
authoritative writer/source for a component's effective config** — and they keep
generating one-off bug reports (7+ ComponentManager/config issues in ~10 days).
Rather than patch each in isolation, complete the config-state ownership contract:

- **gh#515** — `config.Manager.updateConfig` does a read-modify-write over the whole
  config map (`cm.config.Get()` clone → mutate → `cm.config.Update()` atomic swap)
  **without holding a lock across the pair**. Two goroutines interleaving (the KV
  watcher applying an external event concurrently with a caller-goroutine
  `PutComponentToKV`/`DeleteComponentFromKV`, which gh#388 added) can drop one
  component: last-writer-wins on the entire map. `-race` does NOT flag it — each
  `SafeConfig` access is individually mutex-protected; this is a higher-level
  atomicity violation.
- **gh#522** — `service.ComponentManager` keeps a second effective-config baseline,
  `componentConfigs`, that is refreshed only on construction and live `PUT`, **not**
  on the KV-watch restart path (only `mc.Config` is, via `CreateComponent`). `GET
  /config/<component>` reads the body from `componentConfigs`, so after a KV-driven
  restart it returns a **stale** body. (Surfaced by the gh#520 review.)

## What Changes

- `config.Manager` **serializes config-map mutations**: the `Get→mutate→Update`
  read-modify-write in `updateConfig` (and the sibling engine RMW sites
  `enableComponent`/`writeComponentConfigs`/`PutComponentToKV`/`DeleteComponentFromKV`)
  runs under a single manager-held mutation lock, so a concurrent runtime add/remove
  is never lost. One owner for the RMW.
- `service.ComponentManager` unifies on a **single effective-config source of truth**
  (`ManagedComponent.Config`, the only field refreshed on all three write paths —
  create, KV-restart, live-PUT). `GET /config/<component>` derives its body from that
  field; `componentConfigs` is dropped or reduced to a thin view so it can no longer
  drift.

Not breaking: both changes only remove a lost-update window and a stale-read; no
public API, config surface, or wire contract changes. `GET /config` starts
returning the *correct* (current) body where it previously could return a stale one.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `component-runtime-config`: add requirements that (1) runtime config-map mutations
  are serialized so a concurrent component add/remove is never lost, and (2) a
  component's effective config has a single source of truth that `GET /config`
  reflects, including after a KV-watch-driven restart.

## Impact

- **Code**: `config/manager.go` (`updateConfig` + engine RMW sites — hold a mutation
  lock across `Get→mutate→Update`; deadlock analysis vs `SafeConfig.Update`
  subscriber-notify is a design decision); `service/component_manager.go` +
  `service/component_manager_http.go` (unify GET-config reads on
  `ManagedComponent.Config`; retire/thin `componentConfigs`).
- **Consumers**: every sem* product doing runtime component add/remove under
  contention (semsource e2e config sync) and any operator reading `GET /config`
  after a KV-driven reconfigure. No consumer code changes required.
- **Issues**: closes gh#515 and gh#522. Completes the ComponentManager config-state
  cluster (gh#459/#417/#388/#508/#514/#520 already shipped).
