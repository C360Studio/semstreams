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

Currently the handler always returns `{"status":"success","message":"…updated…"}`,
which implies a live apply even when nothing was applied. The fix adds a single
**additive** `applied` boolean (so no current client breaks):

```json
{ "status": "success", "applied": true,  "message": "Configuration applied to the running component" }
{ "status": "success", "applied": false, "message": "Component does not support live runtime reconfiguration; ..." }
```

`applied` is true iff a reconfig contract (either spelling) accepted the change
live.

**Why no `restart_required` field (review HIGH).** An earlier draft paired
`applied` with `restart_required: !applied`. That is a *false promise*: this
endpoint updates only the manager's **in-memory** `componentConfigs`; it does not
write the config KV store (the flow is one-directional KV→cm, and an in-handler KV
write is a known deadlock hazard — gh#388). So a "restart will apply it" claim is
wrong — a restart reloads from KV and reverts the change. Emitting
`restart_required: true` would swap the old "fake live success" for a new "fake
restart success" — the very silent-failure class this change fixes. Durable
persistence is out of scope (gh#388); until it lands, the response promises only
what it delivers: a live apply, or an honest `applied: false`.

## Ordering invariant

Validate → apply-live → then update the in-memory view. A `ValidateConfigUpdate`
failure returns a structured 400 and does **not** mutate the in-memory
`componentConfigs` entry. (Today the handler stores *before* probing the hook;
this change moves the store to after a successful apply / explicit no-hook
accept.) This keeps the GET-config read consistent with the last accepted update;
it is NOT a restart-durability guarantee (see above).

## Risks

- **Impedance (`json.RawMessage` → `map[string]any`).** The bridge unmarshals the
  raw config to a map for `ValidateConfigUpdate`. Same shape the KV-config watcher
  already feeds `ApplyConfigUpdate`, so no new decoding semantics — but the bridge
  MUST reuse the component's own validate step, never a second parallel validator.
- **A component implementing BOTH contracts.** Probe `UpdateConfig` first (current
  behavior) and only fall through to the reconfig method pair if it's absent, so
  existing `UpdateConfig` components are unaffected.

## Implementation note — probe the method pair, not the named interface

Discovered during apply: a component does **not** satisfy the *full*
`service.RuntimeConfigurable` interface. `RuntimeConfigurable` embeds
`Configurable`, whose `ConfigSchema()` returns **`service.ConfigSchema`**, but a
component's `ConfigSchema()` returns **`component.ConfigSchema`** — a different
method signature. So `processor/rule` implements the reconfig *methods*
(`ValidateConfigUpdate` / `ApplyConfigUpdate` / `GetRuntimeConfig`) but is **not**
assignable to `service.RuntimeConfigurable` (the `var _ RuntimeConfigurable`
asserts exist only for the *services* `metrics` and `message_logger`, which
return `service.ConfigSchema`).

The bridge therefore type-asserts a **narrow anonymous interface** of exactly the
two methods it calls —
`interface{ ValidateConfigUpdate(map[string]any) error; ApplyConfigUpdate(map[string]any) error }` —
mirroring the existing anonymous `UpdateConfig` probe. Asserting the full
`RuntimeConfigurable` would silently miss **every** component (ConfigSchema type
mismatch) and re-introduce the exact silent no-op this change fixes. This also
sharpens the "two contracts" framing: they are not even nominally compatible at
the `Configurable`/`ConfigSchema` seam, which is another reason a later
unification is a deliberate, breaking change rather than a drop-in.
