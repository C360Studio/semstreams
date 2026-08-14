# Restore WebSocket output endpoint path

## Why

Beta.160 deliberately replaced URL-shaped network port subjects with canonical protocol/host/port listener facts,
but the mechanical migration also dropped the WebSocket resource path that JSON-configured output components had
carried in that URL. The factory now hard-codes `/ws` even though the constructor and runtime still own a route.
GitHub #945 records the resulting framework narrowing.

## What changes

- Add component-local `path` configuration to WebSocket output, defaulting to `/ws`.
- Preserve `NetworkPort` as protocol/host/port listener identity with no route field or identity change.
- Validate every JSON and direct-constructor path as a valid path-only Go `http.ServeMux` pattern before mux
  registration.
- Reject the known stale root `endpoint` key and add no legacy URL or port alias.
- Repair the shipped edge/cloud `/stream` fixture pair and add production-factory plus already-run core E2E proof.
- Regenerate schema and correct WebSocket output documentation.

## Non-goals

- No generic port, normalized-facts, resource-identity, exclusivity, discovery, registry, or binary-registration change.
- No live route reconfiguration or new listener-sharing behavior.
- No restoration of URL-in-port `subject`, `endpoint`, `url`, `websocket_path`, or nested network `path` aliases.
- No new federation compose/task harness and no claim that the existing federation scenario is executed.
- No ADR or generic component-runtime/discovery spec delta.
