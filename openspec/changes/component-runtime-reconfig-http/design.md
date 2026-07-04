# Design — Component runtime reconfiguration over HTTP (gh#455)

## The decision

**Bridge the two runtime-reconfig contracts at the HTTP seam now; do not unify
the interfaces in this change.** Add an honest applied / restart-required
response.

## Context: two contracts for one responsibility

The framework has two spellings of "apply a validated runtime config change to a
running unit":

| | Component-side | Service-side |
|---|---|---|
| Interface | anonymous `UpdateConfig(ctx, json.RawMessage) error` | named `service.RuntimeConfigurable` |
| Methods | one | `ValidateConfigUpdate(map[string]any)`, `ApplyConfigUpdate(map[string]any)`, `GetRuntimeConfig()`, `ConfigSchema()` |
| Payload | `json.RawMessage` | `map[string]any` |
| Driver | ComponentManager PUT handler (`component_manager_http.go:701`) | service Manager (`service_manager.go` `applyServiceConfigChange`) |
| Validation | inside `UpdateConfig` | explicit `ValidateConfigUpdate` step |

`processor/rule` is a **component** that implements the **service** contract
(because its hot-reload wire format is map-shaped and wants an explicit
validate step). The PUT handler only knows the component spelling, so the rule
processor's reconfig is unreachable over HTTP — the gh#455 defect.

## Options considered

1. **Bridge in the PUT handler (chosen).** After the `UpdateConfig` probe, also
   probe `RuntimeConfigurable`: `json.Unmarshal(req.Config, &map)` →
   `ValidateConfigUpdate` → `ApplyConfigUpdate`. Non-breaking, zero component
   changes, generalizes to every `RuntimeConfigurable` component. Cost: the two
   contracts still coexist (accepted — see below).
2. **Implement `UpdateConfig` on `processor/rule`.** Narrow; fixes only this one
   component and entrenches the split (the next `RuntimeConfigurable` component
   hits the same wall). Rejected.
3. **Unify into one `RuntimeReconfigurable` interface.** The clean end state, but
   breaking: the service Manager path, `hasRuntimeConfigSupport`,
   `GetServiceRuntimeConfig`, and every current implementer (`metrics`,
   `message_logger`, `rule`) would move. Too large for the reported bug; deferred.
4. **Honest response only (no bridge).** Fixes the lie but leaves the rule
   processor un-reconfigurable — doesn't satisfy semboids. Insufficient alone;
   adopted *together with* the bridge.

## Why not unify now

Unification is the right long-term shape (one named responsibility, one payload
type), but it is a breaking cross-cutting refactor of the *service* reconfig path
for no additional benefit to the reported use case. The bridge is the
right-sized, non-breaking first step and it does not foreclose unification — a
later change can collapse `UpdateConfig` into `RuntimeConfigurable` (or a shared
`RuntimeReconfigurable`) and delete the bridge. Naming the seam here (rather than
silently adding a second probe) is what keeps that follow-up honest.

**Follow-up decision to record when unification is scoped:** pick one payload
type (`map[string]any` vs `json.RawMessage`) and one interface name, migrate all
three current implementers + both Manager drivers, and remove the HTTP-seam
bridge. Track as a separate change; reference this one.

## The honest response

Currently the handler always returns `{"status":"success","message":"…updated…"}`.
That conflates three outcomes: applied-live, stored-for-restart, and
validation-rejected (the last already returns 400). The fix distinguishes the
first two with **additive** fields so no current client breaks:

```json
{ "status": "success", "message": "...", "applied": true,  "restart_required": false }  // hot-applied
{ "status": "success", "message": "...", "applied": false, "restart_required": true  }  // stored only
```

`applied` is true iff a reconfig contract (either spelling) accepted the change
live. `restart_required` is its complement for a stored-but-not-applied update.

## Ordering invariant

Validate → apply-live → then treat as stored. A `ValidateConfigUpdate` failure
returns the existing structured 400 and does **not** mutate the in-memory
`componentConfigs` entry, so a rejected update can never be silently loaded on the
next restart. (Today the handler stores *before* probing the hook; this change
moves the store to after a successful apply, or explicitly marks it
restart-pending when no hook exists.)

## Risks

- **Impedance (`json.RawMessage` → `map[string]any`).** The bridge unmarshals the
  raw config to a map for `ValidateConfigUpdate`. Same shape the KV-config watcher
  already feeds `ApplyConfigUpdate`, so no new decoding semantics — but the bridge
  MUST reuse the component's own validate step, never a second parallel validator.
- **A component implementing BOTH contracts.** Probe `UpdateConfig` first (current
  behavior) and only fall through to `RuntimeConfigurable` if it's absent, so
  existing `UpdateConfig` components are unaffected.
