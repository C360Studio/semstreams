# GitHub #945 WebSocket Output Endpoint-Path Inventory

Baseline: `633400328864db7fbf16642189bf80ab7d464ac2`

Phase: `inventory-only`

Body SHA-256: `cfd1cba336e5cf86cb5f0bb5e770d2768d1b76851b9d01d90f5096c53a57e5d6`

## Inventory body

No files were changed and no tests were run during the read-only inventory. The worktree was clean. Live GitHub state
was verified for SemStreams #945 and SemSource #147.

## Problem statement and evidence classification

The JSON-configured WebSocket output could select its endpoint path before beta.160, indirectly through the old
network port's URL-valued `subject`. Beta.160 migrated that port to canonical
`NetworkPort{Protocol, Host, Port}` and changed the factory to unconditional `/ws`.

This is therefore:

- A real beta.160 configuration regression and API narrowing.
- Not merely a pre-existing unreachable `ConstructorConfig.Path` affordance.
- Partly misstated by #945: there was never a dedicated output `Config.path`; the old reachable path lived in
  `ports.outputs[].subject`.
- Not presently blocking the named SemSource consumer: SemSource #147 found no active raw-stream client and adopted
  `/ws` on 2026-08-12, while leaving #945 open as framework narrowing.
- Still a framework contract and E2E gap: shipped edge/cloud fixtures and Core Federation retain `/stream`
  expectations that no current task exercises.

Historical proof:

- Beta.159 `Config` also had no path field: `v1.0.0-beta.159:output/websocket/websocket.go:39-51`.
- Its default network port encoded `http://0.0.0.0:8081/ws` in `Subject`: `:82-115`.
- Its JSON factory parsed both port and path from that URL: `:1645-1660`, defaulting only if absent at `:1673-1681`.
- Current factory hard-codes `/ws`: `output/websocket/websocket.go:1621-1674`.

## Surface inventory

### Claimed path under every observed spelling

| Spelling/value | Current owner/reachability | Observed behavior |
|---|---|---|
| `Path` | Exported `ConstructorConfig.Path`, `output/websocket/websocket.go:55-68`; positional `NewOutput(..., path, ...)`, `:322-330` | Reachable only to direct Go callers; copied to `Output.path` at `:395-407` |
| JSON `path` at output config root | No field in `Config`, `:41-53` | `SafeUnmarshal` ignores unknown top-level fields, `component/validation.go:165-188`; silently remains `/ws` |
| JSON network `config.path` | No field in `NetworkPort`, `component/port_network.go:5-10` | Strict nested port decoding rejects it before initialization, `component/port_codec.go:187-208` |
| JSON `endpoint` | Present only in stale `config/example_config.json:32-38` | Unknown field is ignored; default listener and `/ws` remain |
| JSON `url` | No output field | WebSocket input client owns full URL, including resource path, at `input/websocket/config.go:56-58`; HTTP input similarly owns `url`, `input/http/config.go:35-40,132-148` |
| Old port `subject: http://.../<path>` | Retired beta.159 wire | Current canonical port grammar rejects retired flat fields, `openspec/specs/component-runtime-config/spec.md:245-264` |
| `server.path` | WebSocket input server config, `input/websocket/config.go:45-53` | JSON-reachable; default `/`, `:107-144`; mounted at `input/websocket/websocket_input.go:630-643` |
| `routes[].path` | HTTP gateway, `gateway/types.go:10-29,88-103` | Mounted beneath gateway prefix, `gateway/http/http.go:142-154` |
| `graphql_path`, `mcp_path`, `readiness_path` | Graph gateway component config, `gateway/graph-gateway/component.go:70-84` | Defaults and mux registrations at `:137-152,779-830` |
| `path_prefix` | Lifecycle gateway config, `gateway/lifecycle-gateway/component.go:40-50` | Defaults and mux root at `:149-152,470-488` |
| Metrics service `path` | Standalone service config, `service/metrics.go:28-43` | Strict-decoded JSON with default `/metrics`; passed beside port to `metric.NewServer`, reported through `Path()` and `URL()`, and exposed in the service schema at `service/metrics.go:46-65,97-132,178-219` |
| `prefix + "status/stream"` | Fixed FlowService WebSocket route, `service/flow_service.go:246-288` | Mounted on the shared service mux; upgrades through `handleStatusWebSocketImpl`, `service/flow_runtime_stream.go:217-264` |
| `SEMSOURCE_WS_PATH`, `websocket_path`, `ws_path`, `endpoint_path`, `WebSocketPath` | No repository occurrence | No SemStreams JSON/config/export/schema owner |
| `/ws` | Current factory/runtime default | `DefaultConstructorConfig`, `output/websocket/websocket.go:70-81`; factory `:1633-1664` |
| `/stream` | Historical/shipped federation contract and direct-constructor tests | Archived worklist, cloud fixture, Core Federation, and integration tests |
| `/graph` | Historical SemSource external contract | Current output mux has no such handler, yielding failed upgrade/404 |

### Current execution chain

1. Outer runtime config carries component-specific bytes only as `types.ComponentConfig.Config`:
   `types/component.go:24-32`.
2. Registry validates and passes those raw bytes to the registered factory: `component/registry.go:265-327`.
3. Both production binaries register the same WebSocket output factory: `cmd/semstreams/main.go:443-449`;
   `cmd/e2e-semstreams/main.go:628-633`; registration entry is `componentregistry/register.go:133-143`.
4. `CreateOutput` starts from defaults, unmarshals the JSON into path-less `Config`, and assigns `path := "/ws"`:
   `output/websocket/websocket.go:1621-1664`.
5. `NewOutputFromConfig` gets host/port from normalized `NetworkFacts`, but path from `ConstructorConfig`:
   `output/websocket/websocket.go:333-413`.
6. Initialization checks only nonempty path: `output/websocket/websocket.go:531-553`.
7. Startup creates a private mux with exactly `HandleFunc(w.path, ...)`: `output/websocket/websocket.go:645-654`.
8. Metadata reports the effective path only in a description string: `output/websocket/websocket.go:441-456`; port
   facts and generated port schema cannot report it.

An unmatched route receives Go's no-match handler and produces `page not found`. Invalid or conflicting
`HandleFunc` patterns panic rather than returning a validation error.

## Collision and ownership inventory

| Semantic job | Current owner | Identity/collision behavior | Route visibility |
|---|---|---|---|
| Listener protocol/host/port | `NetworkPort`, `component/port_network.go:5-10` | Exclusive resource ID is only `protocol:host:port`, `:12-20` | No route |
| Normalized listener facts | `NetworkFacts`, `component/port_facts.go:35-40,136-165` | Only Protocol, Host, Port | No route |
| Output WebSocket resource name | `ConstructorConfig.Path` to private `Output.path` | Outside resource identity and conflict checks | Runtime metadata string only |
| Output mux registration | Private output-owned `http.ServeMux` | One standalone listener and one configured pattern | `output/websocket/websocket.go:645-654` |
| WebSocket input server route | `ServerConfig.Path` | Component-local, outside ports | JSON/schema-tagged |
| HTTP gateway route | `RouteMapping.Path` | Multiple routes share the gateway mux/listener | JSON/schema-tagged |
| Graph gateway routes | Component-specific path fields | Multiple handlers share one mux | JSON/schema-tagged |
| Lifecycle route subtree | Component-specific `PathPrefix` | Shared-mux prefix plus component subtree | JSON/schema-tagged |
| Metrics resource path | `service.MetricsConfig.Path` | Standalone `metric.Server`; route is component-local and outside any `NetworkPort` | Strict JSON config, service schema, `Path()`, `URL()`, and startup logs |
| Flow status WebSocket resource | `FlowService.RegisterHTTPHandlers` fixed `prefix + "status/stream"` | Shared service mux; parent service prefix supplies namespace | Fixed production route, OpenAPI description, status-stream client, and real WebSocket integration tests |
| Documentation | Output README and package docs | Conflicting owners: retired URL-in-subject versus constructor-only Path | Stale/misleading |

RFC 6455 separately identifies host, port, and WebSocket resource name, and explicitly uses the resource name to
select among multiple services on one host/port. That matches the in-repo comparator split between listener facts and
component-owned HTTP routes.

The metrics service is the closest standalone-server comparator: it keeps `Port` and `Path` as separate
service-local config fields, applies `/metrics` as the default, passes them separately into `metric.NewServer`, and
publishes the effective path through schema and status accessors (`service/metrics.go:28-65,97-132,178-219`). The
FlowService status stream is the closest shared-mux WebSocket comparator: its resource name is a fixed route beneath
the service prefix, not a listener fact (`service/flow_service.go:246-288`; `service/flow_runtime_stream.go:217-264`).

`NetworkPort` exclusivity also means two output components cannot currently exploit distinct resource names to share
one listener. ADR-063 records the separate pre-existing reconfiguration fragility for exclusive `NetworkPort`:
`docs/adr/063-store-substrate-and-resolver.md:24-32,190-200` (#417).

## Schema, validation, and documentation

- Output `Config` contains only ports, delivery mode, ack timeout, and passthrough:
  `output/websocket/websocket.go:41-53`.
- Its generated component schema is created from that struct: `output/websocket/websocket.go:107-109,469-473`.
- Checked-in `schemas/websocket.v1.json:7-29` exposes only those four properties.
- Canonical network schema derives fields directly from `NetworkPort`, closes additional properties, and therefore
  contains only protocol/host/port: `component/schema_tags.go:696-752`.
- The binding requires protocol and port, defaults host, and has no route field:
  `component/port_codec.go:42-58,265-276`.
- Schema artifacts are generated for every registered component and CI drift-checked:
  `cmd/openapi-generator/main.go:71-92`; `taskfiles/schema.yml:3-9`; `.github/workflows/ci.yml:150-165`.
- Current WebSocket output OpenSpec owns only payload transformation/passthrough, not listener or route behavior:
  `openspec/specs/websocket-output/spec.md:1-77`.
- The standalone metrics service separately exposes port and path, validates the path nonempty, strict-decodes the
  service JSON, and advertises the path in its schema: `service/metrics.go:28-65,197-219`.
- The fixed FlowService WebSocket resource is documented in its OpenAPI description and driven through the shared
  mux by real WebSocket integration tests: `service/flow_service.go:517-525`;
  `service/flow_runtime_stream_integration_test.go:43-442`.
- The current runtime-config spec makes the port envelope strict but does not assign or retire protocol-specific HTTP
  resource names: `openspec/specs/component-runtime-config/spec.md:245-271`.
- Discovery requires generated schema and runtime to agree on the closed port binding:
  `openspec/specs/component-discovery/spec.md:38-65`.
- `output/websocket/README.md:16-57,80-100` still advertises URL-in-`subject`, including arbitrary `/dashboard`,
  `/monitor`, `/alerts`, and `/events` examples at `:220-285`.
- `output/websocket/doc.go:24-30` says the Config struct controls Path, though only `ConstructorConfig` does.

## Beta.160 migration intent

Foundation B deliberately made the generic network binding/facts surface protocol, host, and port:

- Accepted inventory names `NetworkPort` fields as protocol, host, port:
  `docs/proposals/foundation-b-port-language-inventory.md:189-220`.
- Accepted control limits `NetworkFacts` to Protocol, Host, Port:
  `docs/proposals/foundation-b-port-language-control.md:147-150`.
- The strict grammar removed flat `subject` and aliases and allowed only narrow defaults:
  `docs/proposals/foundation-b-port-language-design.md:25-44`.
- Archived design establishes one strict typed envelope and rejects aliases/dual decoding:
  `openspec/changes/archive/2026-08-08-foundation-b-port-language/design.md:66-90`.

However, no accepted artifact explicitly ruled that WebSocket resource paths should cease to be JSON-configurable or
assigned that route to `NetworkPort`. The migration ledger classified the edge federation URL
`http://0.0.0.0:8082/stream` as mechanical, not adjudicated:
`docs/proposals/foundation-b-port-language-worklist.tsv:64-68`. The old Go default URL was likewise inventoried at
`:550`.

Thus the evidence distinguishes:

- Deliberate exclusion of protocol-specific route data from generic normalized listener facts.
- No explicit owner ruling to remove the output component's configurable WebSocket resource name.
- A mechanical migration that dropped that resource name while preserving the constructor field.

The beta.160 migration guide promises strict port failures and downstream flow validation but does not mention this
loss: `docs/operations/migration-beta159-to-beta160.md:1-8,23-33,64-80`. Foundation B recorded `task e2e:all` green,
specifically core health/dataflow/graph-roundtrip: `docs/proposals/foundation-b-release-evidence.md:50-55`.

No active OpenSpec change contains `websocket`, `NetworkPort`, `endpoint path`, `route path`, or `resource name`. No
ADR owns WebSocket output route placement; only ADR-063 mentions `NetworkPort`, for exclusivity/reconfiguration.

Adjacent recorded work:

- SemStreams #945: current upstream narrowing report.
- SemSource #147: historical `/graph` downstream contract; closed after adopting `/ws`, while affirming #945.
- SemStreams #471 archive: WebSocket passthrough only.
- SemStreams #417 via ADR-063: exclusive-port reconfiguration.
- Foundation B archived port-language change and migration ledger.

## Consumer-at-birth inventory

No target field is proposed here; these are the consumers that would exist at birth for either issue-suggested
surface.

| Candidate surface | Immediate reader required for it to have behavior | Other consumers/bills |
|---|---|---|
| Output `Config.path` | `CreateOutput` must copy it into existing `ConstructorConfig.Path`; `Output.setupHTTPServer` already consumes the latter | Generated component schema, JSON authors, docs, config round-trip tests |
| `NetworkPort.path` | No current generic reader consumes it; output factory would still need to project/read it explicitly | Codec, normalization, `ResourceID`, exclusivity semantics, `NetworkFacts`, flow/discovery/capability views, generated closed binding schema, every external port author |
| Existing exported `ConstructorConfig.Path` | `NewOutputFromConfig` and direct Go callers already consume it | Numerous direct-constructor tests, including `/stream` integration tests |

Concrete present consumers:

- No currently active SemSource raw-stream consumer, per #147's consumer audit.
- A concrete in-repo producer/consumer mismatch remains: edge output declares only `0.0.0.0:8082`,
  `configs/edge-federation.json:112-145`; cloud input connects to `ws://edge:8082/stream`,
  `configs/cloud-federation.json:83-92`.
- `config/example_config.json:32-38` is a stale `endpoint` consumer whose value is silently ineffective.
- Direct Go tests consume `ConstructorConfig.Path`; five WebSocket integration cases use `/stream`, but bypass
  JSON/factory: `output/websocket/websocket_integration_test.go:37,143,241,355,459`.
- The only explicit raw-JSON-to-factory seam test covers `passthrough`, not path:
  `output/websocket/passthrough_test.go:69-91`.

## Mandatory adopter seam: SemSource author controlling only JSON

Specific adopter: a SemSource developer who can emit only `types.ComponentConfig` JSON and expects the raw graph
stream at `/graph`.

1. **What must they know?** Neither top-level `path` nor `endpoint` reaches the output; nested network `path` is
   invalid; the registered factory privately chooses `/ws`; the public `ConstructorConfig.Path` is inaccessible from
   their configuration boundary.
2. **What happens if they do nothing?** Their component boots successfully on `/ws`. A WebSocket handshake to
   `/graph` reaches the private mux with no matching handler and returns 404, aborting the upgrade.
3. **What if they try the apparent knobs?** Root `path: /graph` or `endpoint: .../graph` is silently ignored by
   current `SafeUnmarshal`; `ports.outputs[].config.path` fails strict port decoding; beta.159 URL-in-`subject` syntax
   fails current strict canonical decoding.
4. **Where do they find out?** The checked-in schema omits a route; current package docs incorrectly imply Config owns
   Path; the README advertises retired URL-in-subject syntax; absent configuration creates no startup warning.
   Without prior code knowledge, discovery occurs at the client's runtime 404. `Meta()` can reveal `/ws` only after
   inspecting the created component's description.
5. **What should they have to know?** Only their desired WebSocket resource name. The current seam instead requires
   knowledge of an inaccessible constructor/factory split and contradictory schema/docs.

This route is adopter-owned application identity, not a framework-owned value that can be safely observed after
acting; the framework cannot infer that SemSource intends `/graph`.

## Binary, fixture, and E2E coverage inventory

Both `cmd/semstreams` and `cmd/e2e-semstreams` inherit the same registered factory, so any JSON seam affects both
binaries.

Relevant fixtures:

- `configs/edge-federation.json:112-145` — output listener, no route.
- `configs/cloud-federation.json:83-92` — client requires `/stream`.
- `configs/protocol-flow.json:273-304` — core E2E output, no route, therefore `/ws`.
- `config/example_config.json:32-38` — ineffective legacy `endpoint`.
- `schemas/websocket.v1.json:7-29` — no path.

Relevant E2E surface is Core Federation:

- It models Edge WebSocket Output to Cloud WebSocket Input: `test/e2e/scenarios/core_federation.go:55-80`.
- It defaults to `ws://localhost:38082/stream` and dials that endpoint during setup and ack verification:
  `test/e2e/scenarios/core_federation.go:69-70,103-108,310-315`.
- CLI default is also `/stream`, though on port 8082: `cmd/e2e/main.go:129-137`.
- `task e2e:core` runs `--scenario all`: `taskfiles/e2e/core.yml:3-15`.
- `all` includes only health, dataflow, and graph roundtrip, not federation: `cmd/e2e/main.go:568-588`.
- E2E compose exposes output port 8082 through 38082: `docker/compose/e2e.yml:43-75`.
- Documentation claims `task e2e:federation` plus `taskfiles/e2e/federation.yml` and
  `docker/compose/federation.yml`, but neither file nor task exists: `test/e2e/README.md:72-76,98-112`.

Therefore there is no current executed full JSON-config to output listener to `/stream` handshake proof. The relevant
named tier exists as scenario code but is absent from `e2e:core` and `e2e:all` and has no current task/compose harness.

## Exact empty searches and open evidence

Empty at this checkpoint:

- No output JSON tags among `path`, `websocket_path`, `ws_path`, `endpoint_path`, `route_path`, or `url_path` in
  `output/websocket`, `component`, or `types`. The repository-wide closure search also found and classified the
  component/gateway owners above, `service.MetricsConfig.Path`, and FlowService's fixed `status/stream` WebSocket
  route.
- No `Path` in `component/port_network.go` or `component/port_facts.go`.
- No `WebSocketPath`, `websocket_path`, `ws_path`, `endpoint_path`, or `SEMSOURCE_WS_PATH` in repository
  docs/specs/configs/output/component/types.
- No output schema/config route field in `schemas/websocket.v1.json`, `configs/edge-federation.json`, or
  `configs/protocol-flow.json`; unrelated metrics `path` fields are the only matching config hits.
- No `UpdateConfig`, `ValidateConfigUpdate`, or `ApplyConfigUpdate` in `output/websocket`.
- No direct in-repo reference to #945 or SemSource #147.
- No active OpenSpec hit for WebSocket/NetworkPort endpoint-route ownership.
- No applicable ADR beyond ADR-063's exclusive-port note.
- No current federation taskfile or compose file despite documentation references.

Open evidence question for owner adjudication: whether `/stream` in the shipped federation pair is intended current
supported configuration or only abandoned fixture debt. It does not change the regression finding; it determines
whether the in-repo mismatch is a present product contract or solely missing coverage/documentation truth.
