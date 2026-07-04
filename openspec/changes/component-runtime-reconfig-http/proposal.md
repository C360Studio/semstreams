# Component runtime reconfiguration over HTTP (gh#455)

## Why

`PUT <prefix>config/<componentName>` on the ComponentManager validates the new
config, stores it in memory, and then hot-applies **only** when the component
implements the anonymous interface `UpdateConfig(ctx, json.RawMessage) error`
(`service/component_manager_http.go:701`). Any component that instead implements
the framework's *other* runtime-reconfig contract —
`service.RuntimeConfigurable` (`ValidateConfigUpdate` / `ApplyConfigUpdate` /
`GetRuntimeConfig`, `service/configurable.go:19`) — is **never hot-applied**,
because the only driver of that interface is the **service** Manager
(`service/service_manager.go` `applyServiceConfigChange`), not the component
manager. Nothing bridges the two for component instances.

`processor/rule` is exactly such a component: it implements `ApplyConfigUpdate` /
`ValidateConfigUpdate` (its whole hot-reload wire format, the same path gh#451
just fixed), yet the HTTP PUT can't reach it. Two concrete defects fall out:

1. **The rule engine's hot-reload path is unreachable at runtime via HTTP.** The
   carefully-built `applyRuleChanges` reconcile machinery has no HTTP caller for
   component instances — only the KV-config watcher drives it.
2. **PUT reports success without applying.** The handler returns
   `200 {"status":"success","message":"Configuration updated successfully"}` even
   when no hot-apply hook matched. The stored config changed but the running
   component never saw it, and the client **cannot distinguish "applied live"
   from "stored, will apply on restart"** — a silent-success lie.

Reported by the **semboids** team building live rule toggles (flip `enabled` on a
rule → flock behavior changes without restart): the natural path UI →
`PUT /components/config/rule-processor` silently no-ops. They are shipping an
app-side interim gate, per the "don't carve a parallel path — file the gap"
discipline; this change closes the framework gap so that interim can be removed.

## What Changes

- **Bridge `RuntimeConfigurable` into the ComponentManager PUT handler.** After
  the existing `UpdateConfig(ctx, json.RawMessage)` probe, also probe
  `service.RuntimeConfigurable`: unmarshal `req.Config` to `map[string]any`, call
  `ValidateConfigUpdate` then `ApplyConfigUpdate`. Any component implementing the
  contract becomes live-reconfigurable with **zero changes to the component**.
  Rule-processor is the first beneficiary.
- **Make the PUT response honest.** The handler MUST report whether the config was
  applied live or only stored. Add `applied: bool` and `restart_required: bool`
  to the response (additive — `status`/`message` stay for compatibility). A
  component with **no** reconfig hook returns `applied:false, restart_required:true`
  instead of an unconditional `success` that implies a live apply.
- **Validate-before-store ordering is preserved.** A `ValidateConfigUpdate`
  failure returns the structured 400 already used for schema errors; the in-memory
  config is only updated once a hot-apply path (either contract) has accepted it,
  so a rejected update never leaves a stored-but-unapplied config that a restart
  would then load.
- **Name the two-contract split as a deliberate seam (not unify it here).** The
  component-side `UpdateConfig(json.RawMessage)` and service-side
  `RuntimeConfigurable(map[string]any)` are two spellings of one responsibility —
  "apply a validated runtime config change to a running unit." Unifying them is a
  breaking change across both reconfig paths; this change **bridges** at the HTTP
  seam and records the convergence as a follow-up decision (see `design.md`),
  rather than expanding scope.

## Capabilities

### New Capabilities
- `component-runtime-config` — the contract for applying a runtime configuration
  change to a running component over the ComponentManager HTTP API: which
  reconfig interfaces are honored, the validate-before-apply ordering, and the
  honest applied / restart-required response.

### Modified Capabilities
- None (no existing spec covers the ComponentManager config API yet).

## Impact

- `service/component_manager_http.go`: the PUT handler gains the
  `RuntimeConfigurable` bridge and the honest response fields. A small shared
  reconfig-dispatch helper may factor the "try each reconfig contract, report
  which applied" logic so the component and service managers don't diverge.
- **Response shape:** additive (`applied`, `restart_required`); existing
  `status`/`message` unchanged → non-breaking for current clients.
- No component changes required; `processor/rule` is reached as-is.
- The two-contract convergence is deferred and recorded, not implemented.

## Non-goals

- **Unifying `UpdateConfig` and `RuntimeConfigurable` into one interface** — a
  breaking change across the service reconfig path too; recorded as a follow-up
  decision in `design.md`, not done here.
- **Implementing `UpdateConfig(ctx, json.RawMessage)` on `processor/rule`** — the
  bridge makes it unnecessary and would entrench the split.
- **Persisting the config to KV / restart-time reload semantics** — unchanged;
  this change only governs the live-apply path and the honesty of its response.
- **A generic "which fields are runtime-mutable" schema surface** — orthogonal
  (`PropertySchema.Runtime` already exists); not in scope.

## Consumers

`service` (framework — the ComponentManager HTTP API); semboids (reported
consumer, live rule toggles). Any component implementing `RuntimeConfigurable`
(today `processor/rule`) becomes HTTP-reconfigurable; any operator UI performing
`PUT config/<component>` gets an honest applied/restart-required signal.
