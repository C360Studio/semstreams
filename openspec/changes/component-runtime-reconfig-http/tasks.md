# Tasks — Component runtime reconfiguration over HTTP (gh#455)

> Scoping change (Proposed). Tasks are unchecked; implementation follows approval.

## 1. Bridge RuntimeConfigurable in the PUT handler

- [ ] 1.1 In `service/component_manager_http.go` PUT handler, after the existing
      `UpdateConfig(ctx, json.RawMessage)` probe, fall through to a
      `service.RuntimeConfigurable` probe: `json.Unmarshal(req.Config, &map)` →
      `ValidateConfigUpdate(map)` → `ApplyConfigUpdate(map)`.
- [ ] 1.2 Probe order: `UpdateConfig` first, `RuntimeConfigurable` only if absent
      (a component implementing both keeps current behavior).
- [ ] 1.3 Factor the "try each reconfig contract, report which applied" logic into
      a small shared helper so the component and service managers can't diverge.

## 2. Honest applied / restart-required response

- [ ] 2.1 Add `applied bool` + `restart_required bool` to the PUT response
      (additive; keep `status`/`message`). `applied=true` iff a reconfig contract
      accepted the change live.
- [ ] 2.2 No-hook component returns `applied:false, restart_required:true` instead
      of unconditional success.

## 3. Validate-before-store ordering

- [ ] 3.1 Move the in-memory `componentConfigs` update to AFTER a successful
      live-apply (or explicitly mark restart-pending when no hook exists), so a
      `ValidateConfigUpdate` rejection (structured 400) never leaves a
      stored-but-unapplied config that a restart would load.

## 4. Tests

- [ ] 4.1 Integration: `PUT config/rule-processor` with a valid rule change hot-
      applies via the bridge and returns `applied:true`; the running processor
      reflects the change (assert through a reconfig-observable behavior, not just
      the stored config).
- [ ] 4.2 Integration: `PUT config/<component-with-no-hook>` returns
      `applied:false, restart_required:true` and does not lie.
- [ ] 4.3 Integration: a `ValidateConfigUpdate` rejection returns 400 and leaves
      the stored config unchanged (no restart-time silent apply).
- [ ] 4.4 A component implementing `UpdateConfig` still uses that path (regression
      guard for probe order).

## 5. Spec + close

- [ ] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `service`, `task lint`, schema no-drift);
      semstreams-reviewer; then archive → promote `component-runtime-config` into
      `openspec/specs/`.
- [ ] 5.2 Note in gh#455 that the semboids app-side interim gate can be removed.
- [ ] 5.3 If/when unification is scoped, open the follow-up change (collapse the
      two reconfig contracts, delete the HTTP-seam bridge) referencing this one.
