## 1. Serialize config-map RMW in SafeConfig (gh#515, D1)

- [x] 1.1 Add `SafeConfig.Mutate(fn func(*Config) error) error` (`config/config.go`): hold `sc.mu.Lock()` across clone → `fn(draft)` → `Validate` → swap. Document the re-entrancy contract (fn must not call SafeConfig methods).
- [x] 1.2 Migrate `config.Manager.updateConfig` (`config/manager.go:531-608`) — the switch becomes the `fn` body; keep subscriber notification OUTSIDE (it's in the watcher, after updateConfig returns).
- [x] 1.3 Migrate the five engine RMW sites (`engine/engine.go`): `writeComponentConfigs`, `writeToKV`, `enableComponent`, `disableComponent`, `deleteComponentConfig` — read-decide-mutate inside `fn`; capture any value the post-`Mutate` KV write needs (e.g. the enabled `compConfig`) into a local; keep `PushToKV`/`PutComponentToKV`/`DeleteComponentFromKV` after `Mutate` returns.
- [x] 1.4 Grep for any remaining `safeConfig.Get()` → mutate → `.Update()` pattern repo-wide; migrate or justify. Confirm no `fn` re-enters SafeConfig (deadlock).

## 2. Unify GET config on the single source of truth (gh#522, D2)

- [x] 2.1 Audit every `componentConfigs` reader in `component_manager_http.go` (`:339`, `:530`, `:599`, `:654`) and the GET body site — determine each reader's needed data (running effective config vs desired config for a not-running component).
- [x] 2.2 Point GET `/config/<component>` at `cm.components[name].Config` (under the component lock) for running components; fall back to the config Manager's desired config for configured-but-not-running components (D2 option a). Drop `componentConfigs` as an independent baseline, or (option b) reduce it to the not-running fallback if a reader needs data neither source carries — document which and why.
- [x] 2.3 Remove now-dead `componentConfigs` writes (`:181` init, live-PUT `.Config` write) if fully retired; keep the gh#520 `mc.Config` PUT refresh.

## 3. Tests

- [x] 3.1 gh#515 regression (NOT a `-race` test — higher-level atomicity): drive concurrent `PutComponentToKV(add C)` + `DeleteComponentFromKV(B)` (or `updateConfig` for two keys) through the real Manager under contention; assert the final config contains the add AND reflects the remove — neither dropped. Run enough iterations to make a lost update near-certain without the lock.
- [x] 3.2 gh#522 regression: create a component with config C, drive a KV-watch restart to C', assert `GET /config/<component>` returns C' (fails before the fix). Plus: GET after a live PUT returns the PUT body (guards against regressing gh#520's PUT path).
- [x] 3.3 If `componentConfigs` retained for not-running components (option b): a GET for a configured-but-disabled component returns its desired config.

## 4. Gates & wrap-up

- [x] 4.1 `gofmt`, `task lint` (revive clean), `go vet ./...` + `-tags=integration` + `-tags=live_llm`.
- [x] 4.2 `go test -race ./...` (unit) + the relevant integration tests for `config/` and `service/`; run the gh#515 concurrency test many iterations.
- [x] 4.3 `task schema:generate` → confirm no `schemas/`/`specs/` drift.
- [x] 4.4 semstreams-reviewer pre-merge pass — APPROVED (no blocking/high; nits fixed: lock-scope of comp.Config.Name read + manager-level concurrency test + validate-under-lock note).
- [x] 4.5 `openspec validate runtime-config-state-consistency --strict` PASS; on merge close gh#515 + gh#522.
