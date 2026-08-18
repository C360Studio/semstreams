# Flow Architecture

SemStreams flow diagrams are authoring artifacts. They help an operator arrange components, validate connections, and
compile component configuration candidates. They are not runtime lifecycle owners.

## Runtime composition

A process starts components from the configuration selected and sealed by Config Manager at boot. Service and
component composition stays fixed for that process lifetime. Later configuration writes are desired state for a later
boot; they do not create, start, stop, remove, restart, or replace components in the current process.

The bounded exception is Rule definition hot reload inside an already-started Rule processor. It does not change the
processor's ports, dependencies, watch buckets, integration mode, or projection bindings.

## Saved diagrams

Flow Service persists diagrams in the `semstreams_flows` KV bucket. A diagram contains:

- a server-owned ID, CAS version, and audit timestamps;
- authoring metadata;
- canvas nodes; and
- port-to-port connections.

It contains no desired/effective state, deployment state, component bundle, provenance, or restart field. Creating,
updating, validating, or deleting a diagram changes no component configuration and has no runtime effect.

On the first boot where no diagram exists, Flow Service may create a default diagram from the sealed boot component
map. This makes the boot topology visible for authoring. The diagram never becomes boot authority and is not read to
choose the running component set.

## Validation and compilation

Engine validates diagram structure and uses `component/flowgraph` for static port compatibility and connection
discovery. `Compile` produces detached, enabled component configuration candidates. Validation and compilation do not
persist configuration and do not own lifecycle.

An explicit `POST /flows/{id}/publish-component-configs` request is the only diagram-to-config write. It:

1. loads and validates the saved diagram;
2. compiles nodes into component configuration candidates;
3. sorts component names and upserts each candidate through Config Manager; and
4. reports exact persisted names, any failed name, `runtime_unchanged: true`, and whether the desired component map
   differs from the sealed boot map.

Publication is upsert-only. Removing a node from a diagram does not delete desired component configuration. Deletion
must be an explicit Config Manager operation so a visual omission cannot silently remove runtime intent.

## Observations

The retained health, metrics, and message endpoints use component names declared by a saved diagram as filters. They
are best-effort observations; they do not prove that the diagram owns, deployed, or activated those components.

Completed agent-loop aggregation is exposed separately through `monitor_workflow_runs(workflow_slug)` and has no
flowstore dependency.

## Authoring request contract

Create, update, and draft-validation requests accept only authoring fields. Unknown fields are rejected. The server
owns identity, resulting version, and audit metadata; update callers provide only `expected_version` for optimistic
concurrency. Response objects include the server-owned persistence fields.

## Key files

| File | Purpose |
|---|---|
| `flowstore/flow.go` | Persisted diagram response shape |
| `flowstore/manager.go` | Diagram KV persistence and optimistic concurrency |
| `component/flowgraph/` | Static component-port analysis |
| `engine/engine.go` | Diagram validation and compilation |
| `service/flow_service.go` | Diagram CRUD, validation, explicit publication, and observations |
| `config/manager.go` | Desired configuration persistence and sealed boot snapshot |

See [ADR-096](../adr/096-flow-diagrams-are-not-lifecycle-authority.md) for the decision record.
