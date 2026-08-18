# ADR-096: Flow Diagrams Are Not Lifecycle Authority

## Status

**Accepted (2026-08-17).** This decision supersedes only ADR-094's flow-activation clauses. ADR-094 remains immutable
history. Its boot-only component and service composition, dedicated Rule activation, shutdown, recovery, and proof
requirements are unchanged.

## Context

SemStreams needs a useful visual flow authoring model, but no product currently needs a second lifecycle authority
derived from that model. Encoding desired/effective flow state, provenance, component ownership, deployment verbs,
and status streams duplicated Config Manager and ComponentManager truth. It also trained callers to treat a diagram as
a running generation even though runtime composition is fixed at boot.

The useful capability is smaller: save and validate a diagram, compile its nodes into ordinary component configuration
candidates, and intentionally publish those candidates for a later boot.

## Decision

`flowstore.Flow` contains identity, authoring metadata, canvas nodes and connections, audit fields, and a CAS version.
It contains no lifecycle state, component bundle, provenance, or restart field. Diagram create, update, and delete
touch only flowstore.

Engine is a validator/compiler. `Compile` validates first, rejects duplicate instance names, and returns detached,
enabled component configuration candidates. It owns no component or service lifecycle.

The only diagram-to-config write is explicit `POST /flows/{id}/publish-component-configs`. It sorts instance names and
upserts them through Config Manager. It never infers deletion from an omitted node. Partial failure reports exact
persisted names and the failed name; retrying is safe. A success reports `runtime_unchanged: true` and compares the
current desired component map with the sealed boot map to determine `restart_required`.

Config Manager seals a defensive post-arbitration boot config after successful Start. ComponentManager reads it once.
Later desired writes cannot create, remove, restart, or replace components in the current process.

Deployment routes, flow-status streaming, flow-associated logs, and flow lifecycle tools are removed without aliases.
Retained health, metrics, and message endpoints are named saved-diagram observations and only query component names
declared in that diagram. Completed agent-loop aggregation is `monitor_workflow_runs(workflow_slug)` and has no
flowstore dependency.

## Consequences

The flow builder remains useful for authoring, validation, connection discovery, and producing next-boot component
configuration. The framework has one runtime composition authority and ordinary Go lifecycle ownership.

Publishing is upsert-only, so removal is an explicit Config Manager operation outside diagram CRUD. Process
supervision remains responsible for restart. Dirty recovery continues to depend on durable desired config and
effect-before-ACK semantics, not a flow shutdown state machine.

## References

- [ADR-094](094-boot-only-composition-and-observable-rule-activation.md)
- `openspec/changes/require-restart-for-config-activation/`
- `docs/operations/migration-boot-only-flow-activation.md`
