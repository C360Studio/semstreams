# Migrate to boot-only flow activation

The release containing ADR-094 makes process composition immutable after boot.
Flow authoring and validation remain available, and dedicated rule-definition
hot reload remains a separate narrow contract. Generic service, component,
port, dependency, integration, and topology changes require a process restart.

## Operator behavior

Flow `deploy`, `start`, `stop`, and `undeploy` operations now update desired
configuration only:

| Operation | Desired transition | Current runtime |
|---|---|---|
| Deploy | `absent` → `disabled` | Unchanged |
| Start | `disabled` → `enabled` | Unchanged |
| Stop | `enabled` → `disabled` | Unchanged |
| Undeploy | `disabled` → `absent` | Unchanged |

Successful operation responses include `desired_state`,
`runtime_unchanged: true`, and `restart_required`. Restart SemStreams to select
the latest desired configuration. A graceful stop drains and joins the
boot-owned generation; an ungraceful process or host loss does not make a
partially applied configuration authoritative. The next successful boot reads
the latest durable desired snapshot.

Flow records replace `runtime_state` with:

- `desired_state`: durable `absent`, `disabled`, or `enabled` authoring state;
- `desired_components`: the exact server-owned component bundle selected by
  that desired state;
- `effective_state`: independently observed runtime state, or `unknown` when no
  observer is available;
- `restart_required`: whether the current desired digest differs from the
  sealed boot-selection digest;
- `desired_provenance`: the canonical current desired digest;
- `boot_applied_provenance`: an observer-attested boot identity and canonical
  applied digest, when available.

The running SemStreams binaries pass the same immutable `BootSelection` to
component construction, flow reads, status streaming, and tools. Before that
selection is available, `effective_state` is `unknown`,
`boot_applied_provenance` is omitted, and `restart_required` is `null`; unknown
is never collapsed into false. Once boot succeeds, the selected flow bundle is
the process-local applied provenance used for comparison. Health remains a
separate signal and cannot prove activation.

There is no compatibility alias for `runtime_state`, `not_deployed`,
`deployed_stopped`, or `running`.

## Removed configuration and HTTP surfaces

- Remove `watch_config` from the `component-manager` service configuration.
- Stop sending `PUT /components/config/{name}`. The endpoint is read-only and
  returns `405 Method Not Allowed` for mutation requests.
- Config KV writes after boot remain valid desired-state writes but cannot
  create, remove, replace, restart, or reconfigure a component in the current
  process.

## Removed Go APIs

Registry replacement and runtime-handle surfaces are removed:

- direct adopter use of `Registry.CreateComponent` (boot admission is now
  internal-token-gated and owned by `ComponentManager`)
- `Registry.ReplaceComponent`
- `Registry.ValidateDeclarationUpdate`
- `Registry.ConfirmDeclarationUpdate`
- `Registry.UnregisterInstance`
- `Registry.Component`
- `Registry.ListComponents`
- `component.Lookup` and `Dependencies.ComponentRegistry`

Composition roots must create one shared flow manager with the process context
using `flowstore.NewManager(ctx, natsClient)` and inject that same instance as
`service.Dependencies.FlowManager`. The official SemStreams binaries already do
this; downstream composition roots receive a compile-time error until migrated.

ComponentManager runtime mutation and handle-leakage surfaces are removed:

- `ComponentManager.CreateComponent`
- `ComponentManager.RemoveComponent`
- `ComponentManager.Component`
- `ComponentManager.ListComponents`
- `ComponentManager.GetManagedComponents`
- `ComponentManager.GetRegistry`

The unwired `deploy_flow`, `start_flow`, `stop_flow`, and `undeploy_flow` agent
tools are also removed. Flow CRUD tools continue to author definitions;
`monitor_flow` reports desired state, independently observed effective state,
restart requirement, and provenance.

## Adopter action

Adopters should remove live-reconfiguration calls, treat configuration writes
as next-boot intent, and display the new flow observation fields. If an adopter
does nothing and uses only static boot configuration, runtime behavior is
unchanged. Direct references to removed Go fields or methods fail compilation
at the exact migration site.

SemStreams agents do not edit sister repositories. Downstream owners apply and
validate these changes in their own repositories.
