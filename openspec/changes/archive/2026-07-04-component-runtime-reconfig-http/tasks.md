# Tasks — Component runtime reconfiguration over HTTP (gh#455)

> Scoping change (Proposed). Tasks are unchecked; implementation follows approval.

## 1. Bridge RuntimeConfigurable in the PUT handler

- [x] 1.1 In `service/component_manager_http.go` PUT handler, after the existing
      `UpdateConfig(ctx, json.RawMessage)` probe, fall through to the reconfig
      method pair: `json.Unmarshal(req.Config, &map)` → `ValidateConfigUpdate(map)`
      → `ApplyConfigUpdate(map)`. (Probes the METHOD PAIR, not the full
      `service.RuntimeConfigurable` — ConfigSchema return-type mismatch; see
      design.md "Implementation note".)
- [x] 1.2 Probe order: `UpdateConfig` first, the reconfig method pair only if
      absent (a component implementing both keeps current behavior).
- [x] 1.3 Factor the "try each reconfig contract, report which applied" logic into
      `applyRuntimeConfig` so the probe order + method set live in one place.

## 2. Honest applied response

- [x] 2.1 Add an additive `applied bool` to the PUT response (keep
      `status`/`message`). `applied=true` iff a reconfig contract accepted the
      change live. NO `restart_required` field — the endpoint does not durably
      persist (gh#388), so a restart-time promise would be false (review HIGH).
- [x] 2.2 No-hook component returns `applied:false` (+ an honest message) instead
      of unconditional success; it does not claim a restart-time apply.

## 3. Validate-before-store ordering

- [x] 3.1 Move the in-memory `componentConfigs` update to AFTER a successful
      live-apply (or explicit no-hook accept), so a `ValidateConfigUpdate`
      rejection never leaves a stored-but-unapplied config that a restart would
      load.

## 4. Tests

- [x] 4.1 `PUT config/rule-processor`-shaped: a method-pair component hot-applies
      via the bridge and returns `applied:true`; asserted through a
      reconfig-observable mock (ApplyConfigUpdate ran) + the stored config update.
      (`TestHandlePutComponentConfig_MethodPairAppliesAndReportsApplied`,
      `TestApplyRuntimeConfig_MethodPairBridged`.)
- [x] 4.2 A no-hook component returns `applied:false`, does not lie, and does NOT
      emit a `restart_required` promise the endpoint can't keep.
      (`TestHandlePutComponentConfig_NoHookReportsNotApplied`,
      `TestApplyRuntimeConfig_NoHookNotApplied`.)
- [x] 4.3 A `ValidateConfigUpdate` rejection returns a structured 400 and leaves
      the stored config unchanged (no restart-time silent apply).
      (`TestHandlePutComponentConfig_ValidationRejectionReturns400AndDoesNotStore`.)
- [x] 4.4 A component implementing `UpdateConfig` still uses that path, and a
      component implementing BOTH prefers it (probe order).
      (`TestApplyRuntimeConfig_UpdateConfigPath`,
      `TestApplyRuntimeConfig_UpdateConfigPreferredOverPair`.)

## 5. Spec + close

- [x] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `service`, `task lint`, schema no-drift);
      semstreams-reviewer (APPROVE, HIGH addressed); archive → promote
      `component-runtime-config` into `openspec/specs/`.
- [x] 5.2 Note in gh#455 that the semboids app-side interim gate can be removed
      (posted as an issue comment on merge).
- [ ] 5.3 If/when unification is scoped, open the follow-up change (collapse the
      two reconfig contracts, delete the HTTP-seam bridge) referencing this one.
      Deferred by design — tracked here, not a blocker for this change.
