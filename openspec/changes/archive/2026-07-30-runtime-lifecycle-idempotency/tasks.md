## 1. ComponentConfig equality (D1)

- [x] 1.1 Add `func (c ComponentConfig) Equal(other ComponentConfig) bool` in `types/component.go`: compare `Type`/`Name`/`Enabled` by value; compare `Config json.RawMessage` by canonical JSON (order/whitespace-insensitive), with a raw-byte fallback on malformed JSON and empty/`null` treated as equal.
- [x] 1.2 Unit-test `Equal` in `types/component_test.go`: identical configs equal; whitespace-only / key-reordered `Config` equal; a changed scalar field unequal; a changed `Config` value unequal; empty vs `null` `Config` equal; malformed-JSON pair falls back to byte compare.

## 2. Retain effective config on the managed component (D2)

- [x] 2.1 Add `Config types.ComponentConfig` to `ManagedComponent` in `component/lifecycle.go` (documented as the effective config the instance is running).
- [x] 2.2 Populate `Config` wherever a `ManagedComponent` is constructed in `service/component_manager.go` (`CreateComponent` and the recreate step in `restartComponentWithNewConfig`).

## 3. No-op config-update guard (D3, case 1)

- [x] 3.1 In `handleComponentConfigUpdate`, `cfg.Enabled && exists` branch: if `existingComp.Config.Equal(cfg)`, log a debug "config unchanged, skipping restart" and return before calling `restartComponentWithNewConfig`. Leave create-missing and disable/remove branches untouched.
- [x] 3.2 Verify the retained config is refreshed to the new config after a real restart (via task 2.2) so a subsequent identical update is a no-op.

## 4. StopAll / Stop idempotency (D4, case 2)

- [x] 4.1 Add `service.ErrAlreadyStopped` sentinel; document the idempotent-`Stop` contract on the `Service` interface / `BaseService.Stop` (behavior already satisfied — this makes it explicit).
- [x] 4.2 In `Manager.StopAll`, recognize an already-stopped/stopping outcome (`errors.Is(err, ErrAlreadyStopped)`, plus the existing `nil`) as success and exclude it from the aggregated error; keep genuine stop errors aggregated and continue stopping remaining services.

## 5. Tests (spec scenarios → tests)

- [x] 5.1 ComponentManager test: existing enabled component receives an identical config update → no `Stop`/`Start` observed (assert via a spy component or state/restart counter).
- [x] 5.2 ComponentManager test: existing enabled component receives a changed config update → exactly one restart observed; retained config updated to C'.
- [x] 5.3 ComponentManager test: bulk `components.*` reconcile with unchanged configs restarts nothing; still creates a missing enabled and stops a removed/disabled one.
- [x] 5.4 Service-shutdown test: `Manager.StopAll` returns `nil` when a service reached stopped/stopping before StopAll visits it; returns non-nil (and continues) on a genuine stop error; a second `Stop` returns `nil` with no double teardown.

## 6. Gates & wrap-up

- [x] 6.1 `gofmt`, `task lint` (revive clean), `go vet ./...` + `-tags=integration` + `-tags=live_llm`.
- [x] 6.2 `go test -race ./...` (unit) and the relevant integration tests for `service/`.
- [x] 6.3 `task schema:generate` → confirm no `schemas/`/`specs/` drift.
- [x] 6.4 semstreams-reviewer pre-merge pass on the diff — APPROVED (round 1 HIGH #1 live-PUT stale baseline fixed + race closed; #2 UseNumber; nits).
- [x] 6.5 `openspec validate runtime-lifecycle-idempotency --strict`; on merge, close gh#520 + gh#514, cross-link gh#515.
      — strict validation passes 2026-07-30; gh#520, gh#514, and gh#515 all verified CLOSED via
      `gh issue view`.
