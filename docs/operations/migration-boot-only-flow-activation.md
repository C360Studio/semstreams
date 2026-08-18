# Migrate to boot-only component activation

Component composition is now a process-boot decision. A running SemStreams
process does not reconcile component or model-registry configuration writes.

## What adopters need to know

- Flow records are saved diagrams only. They no longer contain or imply a
  deployment/runtime lifecycle state.
- Flow CRUD and validation never change component configuration.
- To prepare component configuration from a diagram, call
  `POST /flowbuilder/flows/{id}/publish-component-configs` explicitly.
- Publication performs sorted upserts. A node omitted from the diagram does not
  delete an existing component configuration.
- A successful publication leaves the current process unchanged and requires a
  process restart before the published component candidates can be composed.
- On failure, inspect `persisted_components`, `failed_component`, `error`,
  `runtime_unchanged`, and `restart_required`. Retrying the same publication is
  safe.

No caller needs to predict subjects, Registry internals, readiness, or a runtime
transition. The framework validates the diagram, observes each actual
persistence result, and reports exact progress.

## Removed surfaces

Remove clients and automation that use these retired HTTP operations:

- `PUT /components/config/{name}`; `GET` remains a boot-effective value
  observation.
- `POST /flowbuilder/deployment/{id}/deploy`
- `POST /flowbuilder/deployment/{id}/start`
- `POST /flowbuilder/deployment/{id}/stop`
- `POST /flowbuilder/deployment/{id}/undeploy`
- `GET /flowbuilder/status/stream`
- `GET /flowbuilder/flows/{id}/runtime/logs`

The former runtime observation paths move to the name-keyed observation paths
below. They do not retain their old runtime-ownership meaning.

Remove these retired agent tools from personas, allowlists, and prompts:

- `deploy_flow`
- `start_flow`
- `stop_flow`
- `undeploy_flow`
- `monitor_flow`

Remove `watch_config` from ComponentManager configuration. Unknown-field
validation now rejects it.

Flow lifecycle state and timestamps are removed from the `flowstore.Flow` JSON
schema, including `runtime_state`, `deployed_at`, `started_at`, and
`stopped_at`. The flow status WebSocket, flow-associated runtime log stream,
and lifecycle-specific telemetry are removed without compatibility aliases.

### Go API removals

Code that previously borrowed runtime components from ComponentManager must
move behind the framework's internal manager-owned callback seam or consume a
value-only observation. These `ComponentManager` methods are removed:

- `Component`
- `ListComponents`
- `CreateComponent`
- `RemoveComponent`
- `GetManagedComponents`
- `CreateComponentsFromConfig`

These Registry live-handle, replacement, and observation methods are removed:

- `ReplaceComponent`
- `ValidateDeclarationUpdate`
- `ConfirmDeclarationUpdate`
- `UnregisterInstance`
- `ListComponents`
- `GetComponent`
- `Component`
- `InstanceDependencies`
- `ObserveSnapshots`

Registry boot admission and defensive snapshot methods are framework-internal:
their access token is not an adopter lifecycle API.

Saved-diagram observations remain available at:

- `/flowbuilder/flows/{id}/observations/metrics`
- `/flowbuilder/flows/{id}/observations/health`
- `/flowbuilder/flows/{id}/observations/messages`

These are best-effort observations filtered by component names in the diagram;
they do not prove that the diagram owns or activated a running component.

## If an adopter does nothing

Existing process boot from component configuration continues to work. Editing a
flow remains authoring-only, and later configuration changes remain pending
until a restart. Calls to removed lifecycle or status-stream routes return no
route; callers must remove those assumptions rather than map them to a new
lifecycle API.

## Foreign configuration identity now fails startup

Config Manager no longer continues in a detached mode when the shared bucket
contains a different platform identity. Startup fails before configuration
arbitration, watchers, writes, or dependent construction. Fix the configured
NATS account/bucket or platform identity, then restart.

This failure is intentional: a detached writer could return success without an
observable write, so explicit Flow publication could not report exact persisted
names. See the [implementation conformance ledger][c].

[c]: ../../openspec/changes/require-restart-for-config-activation/pr990-boot-only-implementation-conformance.md
