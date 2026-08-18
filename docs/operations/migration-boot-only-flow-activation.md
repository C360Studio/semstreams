# Migrate flow diagrams to boot-only component configuration

ADR-096 removes flow lifecycle authority. Flow diagrams remain available for CRUD, validation, connection discovery,
and compilation. Component and service composition remains immutable after boot; Rule definitions retain their
separate, narrow hot-reload contract.

## Removed flow fields

Remove every use of these retired lifecycle fields:

- legacy runtime fields: `runtime_state`, `deployed_at`, `started_at`, and `stopped_at`;
- intermediate desired/effective fields: `desired_state`, `desired_components`, `desired_changed_at`, and
  `effective_state`;
- provenance/restart fields: `desired_provenance`, `boot_applied_provenance`, and `restart_required`; and
- aliases that attempted to restate lifecycle: `deployment_state`, `activation_state`, `lifecycle_state`, and
  `flow_state`.

`Flow` now contains only diagram metadata, nodes, connections, server-owned audit fields, and its CAS version.

Creating, updating, or deleting a diagram changes no component configuration and no running component.

## Use authoring request objects

The old write shape reused the persisted response object. For example, callers sent identity, version, audit, and
lifecycle fields back to create/update/validate:

```json
{
  "id": "flow-123",
  "name": "example",
  "description": "example diagram",
  "version": 7,
  "nodes": [],
  "connections": [],
  "runtime_state": "running",
  "deployed_at": "2026-08-17T12:00:00Z",
  "started_at": "2026-08-17T12:01:00Z",
  "stopped_at": null,
  "created_at": "2026-08-16T12:00:00Z",
  "updated_at": "2026-08-17T12:00:00Z",
  "created_by": "operator",
  "last_modified": "2026-08-17T12:00:00Z"
}
```

Do not reuse that shape. `POST /flowbuilder/flows` and body-based
`POST /flowbuilder/flows/{id}/validate` now accept only:

```json
{
  "name": "example",
  "description": "example diagram",
  "nodes": [],
  "connections": []
}
```

`PUT /flowbuilder/flows/{id}` accepts the same authoring fields plus the prior CAS value under the newly named
`expected_version` field:

```json
{
  "name": "example",
  "description": "updated diagram",
  "expected_version": 7,
  "nodes": [],
  "connections": []
}
```

The path owns update and validation identity. The server owns the created ID, resulting version, and all audit fields.
Create and update responses remain the persisted `Flow` response shape:

```json
{
  "id": "server-generated-id",
  "name": "example",
  "description": "updated diagram",
  "version": 8,
  "nodes": [],
  "connections": [],
  "created_at": "2026-08-16T12:00:00Z",
  "updated_at": "2026-08-17T12:00:00Z",
  "created_by": "operator",
  "last_modified": "2026-08-17T12:00:00Z"
}
```

Validation still returns `engine.ValidationResult`; it never returns a `Flow` or lifecycle state.

Do not send the persisted `Flow` response object back as a write request. Unknown fields, including every retired
lifecycle field and server-owned `id`, `version`, and audit field, now return HTTP 400. OpenAPI exposes separate
`FlowCreateRequest`, `FlowUpdateRequest`, and `FlowValidateRequest` schemas with `additionalProperties: false`; response
schemas remain `Flow`.

## Publish component configuration explicitly

To turn a saved diagram into desired component configuration for a later boot, call:

```text
POST /flowbuilder/flows/{id}/publish-component-configs
```

Successful response:

```json
{
  "persisted_components": ["input-main", "processor-main"],
  "runtime_unchanged": true,
  "restart_required": true
}
```

Names are persisted in lexical order. Publishing only upserts nodes present in the diagram; removing a node from a
diagram never deletes an existing component configuration. Use the explicit Config Manager deletion surface when
removal is intended.

A partial failure returns HTTP 500 with the exact successful prefix and failed name:

```json
{
  "persisted_components": ["input-main"],
  "failed_component": "processor-main",
  "runtime_unchanged": true,
  "restart_required": true,
  "error": "..."
}
```

Retry is safe because each write is an idempotent upsert. Restart SemStreams after a successful publish when
`restart_required` is true.

## Removed HTTP and tool surfaces

There are no compatibility aliases for these retired routes:

- `POST /flowbuilder/deployment/{id}/deploy`
- `POST /flowbuilder/deployment/{id}/start`
- `POST /flowbuilder/deployment/{id}/stop`
- `POST /flowbuilder/deployment/{id}/undeploy`
- `/flowbuilder/status/stream`
- `GET /flowbuilder/flows/{id}/runtime/health`
- `GET /flowbuilder/flows/{id}/runtime/metrics`
- `GET /flowbuilder/flows/{id}/runtime/logs`
- `GET /flowbuilder/flows/{id}/runtime/messages`

These exact agent tools are also retired without aliases:

- `deploy_flow`
- `start_flow`
- `stop_flow`
- `undeploy_flow`
- `monitor_flow`

Retained observation paths are:

- `GET /flowbuilder/flows/{id}/observations/health`
- `GET /flowbuilder/flows/{id}/observations/metrics`
- `GET /flowbuilder/flows/{id}/observations/messages`

These query actual framework observations for component names declared by the saved diagram. They do not claim those
components belong to, were activated by, or are controlled by the diagram.

Agent-loop aggregation is now `monitor_workflow_runs` with required `workflow_slug`. Its result contains workflow-run
counts and records only; it does not return flow lifecycle state or provenance.

## Go composition changes

Config Manager must be started before ComponentManager or FlowService construction. `BootConfig()` returns a defensive
copy of the successful post-arbitration boot configuration. `ComponentRestartRequired()` compares exact component-map
membership and canonical component values.

Flow Engine constructors no longer accept flow/config managers. Use `Compile(flow)` for detached candidates. The
official binaries already use the new composition.

If a downstream adopter does nothing and uses static boot configuration, runtime behavior is unchanged. Direct use of
removed fields, routes, or tool names fails at the migration site. SemStreams agents do not edit sister repositories;
downstream owners apply and validate their own migrations.
