# Flow authoring boundary — surface inventory for #1089

## Checkpoint identity

- Artifact path: `docs/proposals/gh1089-flow-boundary-inventory.md`
- SemStreams code baseline: `5cc0c7fbe569c6398fc534025218639b4c7e0345` (`main`, discovery-only checkout; the
  `.claude/worktrees/*` trees under it were excluded from every grep)
- semstreams-ui inspected baseline: `39f5f04030e54cd7e5ac1b20490b877bb7b7f2dd`
- semteams inspected baseline: `8a70b7e76e25985c2a3e95cbb45d5c57e0d3e326` (pins `semstreams v1.0.0-beta.160`)
- semspec `5a9496ee` (pins beta.134); semsource `4093d3c`, semconnect `d0d06e0`, semdev `ca3956a`, semboids `8c03cc5`,
  semmachina `841c45e` (all beta.160); semops `602c619` (beta.145); semdragon `07f4de9` (beta.135)
- Mechanical measurement: a scratchpad program registering `componentregistry.Register` +
  `graphresearch.RegisterComponents` + `optionalotel.Register` (the exact set `cmd/openapi-generator/main.go:39-51`
  and `cmd/semstreams/main.go:489-504` register) and calling every factory with `json.RawMessage("{}")` and
  `component.Dependencies{}`; its table is reproduced verbatim in §2.3.
- Review state: independent inventory review required; not `INVENTORY PASS`. The design artifact that accompanies
  this file is conditional on that pass.
- SHA-256 of this file is recorded in the design artifact's checkpoint block.
- No target state, options, recommendation, or implementation plan is included here.

## 1. Problem statement

The owner is leaning to option C on #1089: retire the saved-diagram authoring surface (flowstore, flowtemplate,
the Flow-shaped engine, the flow-builder service and its HTTP routes, the flow and flow-template agent tools) and
keep composition validation as substrate — the boot composition (`config.Components` + platform) is validated
offline against the product binary's own catalog, and a diagram is a read-only projection. This inventory measures
the premises that framing rests on: whether port declarations can be static facts of a factory, what the removal
surface actually is, and what breaks outside this repo.

## 2. The claimed gap — measured

### 2.1 Where port declarations come from today

- `component.Registration` carries `Schema ConfigSchema` as static metadata and no ports
  (`component/registry.go:52-62`; `GetComponentSchema` reads it without instantiation, `:310-326`).
- The Registry captures ports from the *constructed* component: `captureComponentDeclaration` calls
  `discoverable.InputPorts()` / `OutputPorts()` (`component/registry.go:564-571`) inside `prepareComponent`
  (`:209-273`), which refuses a nil NATS client before any factory runs (`:228`).
- The engine validator reaches ports only through that seam: `validationRegistry.CreateComponent(..., 
  component.Dependencies{NATSClient: v.natsClient}, nil)` (`engine/validator.go:244-246`).
- **But the port grammar is already config-shaped.** `component.PortDefinition` / `PortConfig` (`component/ports.go:53-113`),
  `PortDefinition.Resolve(direction)` is pure (`component/port_resolver.go:11-49`), `MergePortConfig(defaults,
  overrides)` is pure (`ports.go:153-165`), and 31 of the 34 files in `schemas/` carry a `ports` property
  (`grep -L '"ports"' schemas/*.json` → `gated-dag.v1.json`, `http.v1.json`, `workflow-definition.v1.json`).
- **A pure config→ports lane already exists in the framework:** `config.Validate` (`config/config.go:223-278`) calls
  `ValidateStreamDeclarations` (`:256-269`), which derives JetStream streams from every component's *configured*
  output ports without constructing anything — `extractPortsFromConfig(compCfg.Config)` (`config/streams.go:431-447`)
  then `definition.Resolve` + `port.Facts()` (`config/stream_bounds.go:259-320`). The comment at
  `config/config.go:256-261` names the reason: "Resolution is pure and I/O-free precisely so it can run here".
  That lane reads only the ports present in the raw config; it does not know a factory's **default** ports, which
  live inside each package's `DefaultConfig()` (26 sites, §2.3).

### 2.2 Registered factories vs. schema files

`schemas/` holds 34 files; the registry holds **33** factories (27 core `componentregistry/register.go:36-238`,
5 `frameworkcapabilities/graphresearch/register.go:449`, 1 `frameworkadapters/otel`).
`schemas/workflow-definition.v1.json` has no factory: the generator's own comment says workflow-definition schema
generation was removed (`cmd/openapi-generator/main.go:94`) and `grep -rn "workflow-definition" --include='*.go' .`
(main tree) → 0. It is a stale generated file.

### 2.3 Factory port-declarability table (the load-bearing measurement)

Legend — **source**: where the effective `InputPorts()/OutputPorts()` values are computed. **class**: STATIC (no
config influence), CONFIG-DERIVED (a pure function of the raw config and, for one factory, the instance name;
sub-shape *merge* = `MergePortConfig(defaults, config.ports)`, *replace* = `config.ports` supersedes the defaults
wholesale when present, *derived* = additional ports computed from non-`ports` config fields), RUNTIME-ONLY (needs a
dependency or I/O). **nil-deps `{}`**: the scratchpad probe's outcome calling the factory directly with an empty
config and zero dependencies (bypassing the Registry guard at `registry.go:228`).

| # | factory | type | source (file:line) | class | nil-deps `{}` |
|---|---|---|---|---|---|
| 1 | agentic-dispatch | processor | defaults `processor/agentic-dispatch/config.go:61`; merge + resolve `component.go:200-225` | CONFIG-DERIVED (merge) | ERR `deps.ModelRegistry is required` |
| 2 | agentic-governance | processor | defaults `config.go:275`; merge + resolve `component.go:79-107` | CONFIG-DERIVED (merge) | OK in=3 out=4 |
| 3 | agentic-loop | processor | defaults `config.go:386`; merge + per-port kind checks `component.go:218-260` | CONFIG-DERIVED (merge) | OK in=8 out=11 |
| 4 | agentic-model | processor | defaults `config.go:144`; merge `component.go:112-120`; `resolveConfiguredPorts` `:214-250` | CONFIG-DERIVED (merge) | ERR `model registry is required` |
| 5 | agentic-tools | processor | defaults `config.go:148`; merge + resolve `component.go:109-134` | CONFIG-DERIVED (merge) | OK in=4 out=2 |
| 6 | file (output) | output | defaults `output/file/file.go:60-78`; `config.ports` replaces `:150-152`; resolve `:154-176`; `file_output` FilePort derived from Directory/FilePrefix/Format `:184-196` | CONFIG-DERIVED (replace + derived) | ERR config unmarshal (`SafeUnmarshal` rejects `{}`) |
| 7 | file_input | input | `file_source` FilePort from Path/Format `input/file/file.go:200-205`; output from `config.ports[0]` `:206-212`; replace `:645-648` | CONFIG-DERIVED (replace + derived) | ERR `NATS client is required` (`:655`) |
| 8 | gated-dag | processor | inputs `nil` `processor/gated-dag/component.go:335`; outputs from DispatchStream/DispatchSubject/DispatchStreamRetention + constant `graph_mutations` `:70-97`; no `ports` config lane | inputs STATIC; outputs CONFIG-DERIVED (derived, non-port fields) | ERR `unit_entity_prefix is required` |
| 9 | graph-clustering | processor | defaults/ApplyDefaults `processor/graph-clustering/component.go:476-530`; factory `:670-700` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:670`) |
| 10 | graph-embedding | processor | defaults `component.go:192-210`; factory `:382-420` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:382`) |
| 11 | graph-gateway | gateway | defaults `gateway/graph-gateway/component.go:141-160`; `validateGatewayQueryOutputs` pins exactly the three canonical outputs `:156-190`; factory `:316-345` | CONFIG-DERIVED (replace, contract-pinned) | ERR `NATSClient required` (`:316`) |
| 12 | graph-index | processor | defaults/ApplyDefaults `processor/graph-index/component.go:154-197`; factory `:377-410` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:377`) |
| 13 | graph-index-spatial | processor | defaults `component.go:123-150`; factory `:213-245` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:213`) |
| 14 | graph-index-temporal | processor | defaults `component.go:125-152`; factory `:222-250` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:222`) |
| 15 | graph-ingest | processor | defaults `processor/graph-ingest/component.go:391-405`; a non-empty config must carry `ports` (`Validate` `:355-364`); factory `:646-685` | CONFIG-DERIVED (replace; explicit ports required) | ERR `NATSClient required` (`:646`) |
| 16 | graph-query | processor | defaults `processor/graph-query/component.go:109-125`; `ports` is a Required schema field; factory `:229-246` | CONFIG-DERIVED (replace; ports required) | ERR `ports configuration is required` |
| 17 | http (gateway) | gateway | `InputPorts`/`OutputPorts` return nil `gateway/http/http.go:406-413`; no `ports` config lane | STATIC (empty) | ERR config unmarshal |
| 18 | httppost | output | defaults `output/httppost/httppost.go:66-84`; replace `:150-152`; resolve `:154-175` | CONFIG-DERIVED (replace) | ERR config unmarshal |
| 19 | json_filter | processor | defaults `processor/json_filter/json_filter.go:58`; replace `:129-131`; `resolveMessagePorts` `:133-140,179` | CONFIG-DERIVED (replace) | OK in=1 out=1 |
| 20 | json_generic | processor | defaults `json_generic.go:49`; replace + `resolveMessagePorts` `:113-125,157` | CONFIG-DERIVED (replace) | OK in=1 out=1 |
| 21 | json_map | processor | defaults `json_map.go:61`; replace + `resolveMessagePorts` `:135-147,197` | CONFIG-DERIVED (replace) | OK in=1 out=1 |
| 22 | lifecycle-gateway | gateway | `config.ports` (may be empty) + `ApplyDefaults` appends the canonical `graph_mutations` output when absent `gateway/lifecycle-gateway/component.go:161-190`; defaults `:208` | CONFIG-DERIVED (replace + canonical append) | ERR `deps.LifecycleManager is nil` |
| 23 | objectstore | storage | defaults `storage/objectstore/config.go:97`; replace `component.go:152-155`; `resolveObjectStorePorts(cfg, instanceName)` `:319-345` adds a `store-provide` port whose instance is the instance name `:370-371` (the factory hard-codes `"objectstore"` `:180`) | CONFIG-DERIVED (replace + instance-derived) | OK in=1 out=3 |
| 24 | otel-exporter | output | defaults `output/otel/config.go:54-66`; replace / default input appended when empty `component.go:118-140` | CONFIG-DERIVED (replace) | OK in=2 out=0 |
| 25 | research-graph-assess | processor | defaults `config.go:109`; replace + resolve (same shape as #26) | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`component.go:100`) |
| 26 | research-graph-classify | processor | defaults `processor/research-graph-classify/config.go:77-95`; replace `component.go:103-108`; resolve `:116-140` | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`:116`) |
| 27 | research-graph-execute | processor | defaults `config.go:97`; same shape | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`component.go:90`) |
| 28 | research-graph-route | processor | defaults `config.go:93`; same shape | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`component.go:103`) |
| 29 | research-graph-synthesize | processor | defaults `config.go:110`; same shape | CONFIG-DERIVED (replace) | ERR `NATSClient required` (`component.go:87`) |
| 30 | rule-processor | processor | defaults `processor/rule/config.go:222-238`; replace `factory.go:44-47`; `setupPorts` resolves `processor.go:380-397` | CONFIG-DERIVED (replace) | ERR `NATS client is required` (`factory.go:30`) |
| 31 | udp | input | defaults `input/udp/udp.go:209-225`; merge `:750-758`; `getConfiguredPorts` `:228-249` | CONFIG-DERIVED (merge) | ERR `NATS client is required` (`:763`) |
| 32 | websocket (output) | output | defaults `output/websocket/websocket.go:95-113` (`websocketOutputDefinitions(8081)` `:484-490`); replace `:1870-1874`; resolve `:372-400` | CONFIG-DERIVED (replace) | ERR `NATS client is required` (`:1860`) |
| 33 | websocket_input | input | defaults `input/websocket/config.go:95-106,140-143`; replace via `SafeUnmarshal` into defaults `register.go:17-24`; `dataSubject`/`controlSubject` derived from the resolved outputs `websocket_input.go:314-348` | CONFIG-DERIVED (replace) | ERR `NATS client is required` (`register.go:25`) |

Counts: **STATIC 1** (http; plus gated-dag's input side), **CONFIG-DERIVED 32** (6 merge, 21 replace, 5 with extra
derivation from non-port fields or the instance name: file, file_input, gated-dag, lifecycle-gateway, objectstore),
**RUNTIME-ONLY 0** — no factory reads a dependency, a connection, or I/O to compute a port. Every factory
computes its ports before or independently of its dependency guards; the guards only decide whether a component
*object* is returned.

Nil-dependency construction (`{}` config, zero deps, Registry bypassed): **8 succeed, 25 fail** — 17 on the NATS
guard, 2 on `ModelRegistry`, 1 on `LifecycleManager`, 5 on required config (`file`, `gated-dag`, `graph-query`,
`http`, `httppost`). Through the Registry seam that the engine uses, **0 succeed** (`registry.go:228`).

### 2.4 Where ports are already declared statically in a *generated* artifact

`schemas/<name>.v1.json` is produced from `Registration` by `cmd/openapi-generator/main.go:54-90`
(`extractSchema(name, registration)`); it carries the `ports` config property's schema (the lane), not the factory's
default port values. `openspec/specs/component-discovery/spec.md:38` ("Generated port schema is derived from the
closed binding") governs that generated shape.

## 3. Every current spelling of the fact being modeled

The fact: "which ports (and therefore which connections) a composition of `config.Components` has, and whether the
composition is well-formed".

| Spelling | Where | Evidence class |
|---|---|---|
| Config-declared ports per component | `component.PortConfig` in 31 factory configs (`Ports *component.PortConfig \`json:"ports"\`` — e.g. `input/udp/udp.go:162`, `processor/rule/config.go:20`, `storage/objectstore/config.go:23`) | declaration (config) |
| Factory default ports | `DefaultConfig()` in 26 packages (§2.3 column 4) | declaration (code, not exported on `Registration`) |
| Resolved ports on the instance | `InputPorts()/OutputPorts()` on every `Discoverable` (33 sites, `grep -rn "func (.*) InputPorts() \[\]component.Port"`) | instance |
| Admitted declaration | `component.Registry` `componentDeclaration{InputPorts, OutputPorts, InputFacts, OutputFacts, ExclusiveResources}` (`component/registry.go:76-84,564-590`), exposed as `Snapshots(access)` (`:667-678`) | boot admission (runtime) |
| Stream declarations derived from configured output ports | `config.ValidateStreamDeclarations` → `planStreams` (`config/stream_bounds.go:259-320,440`) via `extractPortsFromConfig` (`config/streams.go:435`) | pure, at `config.Validate` (`config/config.go:256-269`) and `semstreams --validate` (`cmd/semstreams/flags.go:71`, `main.go:102-115`) |
| Effective-config gates at boot | `bootstrapobservability.ValidateEffectiveConfig` (`internal/bootstrapobservability/bootstrap.go:213-235`): `cfg.Validate`, `rulepackcap.ValidateConfig`, `graphresearch.ValidateConfig` | boot (post-arbitration effective config) |
| Connection graph of the admitted composition | `flowgraph.BuildFromRegistry` (`component/flowgraph/flowgraph.go:127-143`) → `ConnectComponentsByPatterns` (`:216`) → `AnalyzeConnectivity` (`:714`) / `ValidateStreamRequirements` (`:955`); cached by `ComponentManager.GetFlowGraph` / `ValidateFlowConnectivity` (`service/component_manager.go:1430-1500`) | runtime observation |
| HTTP projection of that graph and its findings | `GET <components>/flowgraph`, `/validate`, `/gaps`, `/paths` (`service/component_manager_http.go:74-77`, handlers `:618-830`); OpenAPI `specs/openapi.v3.yaml:99-112,815-829` | runtime observation (HTTP) |
| e2e pre-flight consumer of that projection | `test/e2e/client/observability.go:330-400` (`ValidateFlowGraph`, `CheckFlowHealth` → `/components/validate`); called from every tiered scenario's `Setup` (`test/e2e/scenarios/tiered.go:187`) | test |
| Connection graph of a *saved diagram* | `engine.Validator.buildFlowGraph` constructs each node through the Registry with a real NATS client (`engine/validator.go:203-275`), then the same flowgraph functions (`:137-141,162`) | runtime construction of a draft |
| Findings vocabulary over a diagram | `engine/validator.go`: `empty_flow` `:103`, `unknown_component` `:226`, `graph_build_error` `:208,254,269`, `disconnected_node` `:304`, `orphaned_port` `:368` (severity rules `:313-361`), `interface_mismatch` `:560`, `missing_interface` `:592`; `connection_pattern_error` is an execution error today (`:137-141`) | string literals, no constants (Slice C1 in PR #1088 would have constant-ized them in `flowstore`) |
| Findings vocabulary over the running composition | `flowgraph` issue constants `IssueNoPublishers`… (`flowgraph.go:56-62`), `FlowAnalysisResult`/`DisconnectedNode`/`OrphanedPort`/`StreamRequirementWarning` (`flowgraph_analysis.go:6-41`); the HTTP handler adds `validation_status: critical` for stream warnings (`component_manager_http.go:677-683`) | typed, but not the engine's vocabulary |
| Config → diagram derivation | `flowstore.FromComponentConfigs` (`flowstore/converter.go:26-83`: node ID = instance name, `Component` = factory, `Type`, config map, grid position) used once by `FlowService.ensureDefaultFlowFromConfig` (`service/flow_service.go:121-157`), which then borrows the validator's `DiscoveredConnections` to fill edges | store-side projection |
| Diagram → config compilation | `engine.Engine.Compile` (`engine/engine.go:87-113`) → `FlowService.publishCompiledComponentConfigs` → `config.Manager.PutComponentToKV` (`service/flow_service.go:463-536`; `config/manager.go:674-700`) | the only production caller of `PutComponentToKV` (`grep -rn PutComponentToKV --include='*.go' .` → `service/flow_service.go:35,487` and the method itself) |
| Interface compatibility rule | `engine.Validator.areInterfacesCompatible` = exact match (`engine/validator.go:612-623`) | one home, inside the package that would be removed |

Two homes for the same interpreted fact exist today: the engine's `convertAnalysisToResult` re-interprets
`flowgraph.OrphanedPort` into severities (`validator.go:313-361`) while the HTTP handler interprets the same analysis
differently (`component_manager_http.go:677-716`, stream warnings → `critical`; `AnalyzeConnectivity` itself derives
`ValidationStatus` at `flowgraph.go:748-770`). That is a consolidation target, not a pattern to extend.

## 4. Removal surface inventory (measured; disposition column is reported as the issue frames it, not decided here)

LOC are `wc -l` at the baseline; "prod" excludes `_test.go`.

| Surface | prod LOC | test LOC | In-tree dependents (main tree) | Flow-shaped? |
|---|---|---|---|---|
| `flowstore/` (`converter.go` 116, `doc.go` 13, `flow.go` 132, `manager.go` 286) | 547 | 1,099 (`converter_test` 331, `flow_test` 67, `manager_integration_test` 701) | importers: `engine/{engine,validator}.go`, `flowtemplate/template.go:90,120`, `processor/agentic-tools/executors/flows.go`, `service/flow_service.go`, `service/flow_runtime_messages.go`, `cmd/semstreams/main.go:24,245,707-735`, `cmd/e2e-semstreams/main.go:27,185,418-443`; KV bucket `semstreams_flows` (`flowstore/manager.go:60`) | yes |
| `flowtemplate/` (`manager.go` 125, `template.go` 133) | 258 | 314 | importers: `executors/flow_templates.go`, `cmd/semstreams/main.go:25,247,737-760`, `cmd/e2e-semstreams/main.go:28,187,444-460`; renders `*flowstore.Flow` (`template.go:86-125`); KV bucket `FLOW_TEMPLATES` (`flowtemplate/manager.go:14,29`) | yes (Flow JSON templates) |
| `engine/` (`doc.go` 14, `engine.go` 126, `metrics.go` 74, `validator.go` 623) | 837 | 131 (`compile_test` 61, `validator_test` 70) | importer: `service/flow_service.go:15` only; `validator.go:300-623` holds the findings conversion, node/port extraction, discovered-connection extraction, and interface-contract check that operate on a `flowgraph.FlowGraph`, not on a Flow | package yes; ~330 lines of logic are graph-shaped |
| `service/flow_service.go` | 595 | `flow_service_test` 557, `flow_service_lifecycle_test` 139, `flow_publish_test` 194, `flow_surface_test` 209 | registered as `"flow-builder"` (`service/register.go:15`); enabled by `configs/protocol-flow.json:39-42`; OpenAPI via `init()` `:22-24`; routes `:200-213`; hosts `ensureDefaultFlowFromConfig` `:121-157` and `publishCompiledComponentConfigs` `:463-536`; **also hosts `streamOverrideExpiryReporter`** (`:560-585`, `service/stream_override_expiry.go` 130 prod / 114 test) — a stream-provisioning substrate metric that is not flow-shaped | mostly; the override reporter is not |
| `service/flow_runtime_health.go` / `_messages.go` / `_metrics.go` | 347 / 305 / 410 | 434+280 / 352+531 / 369+306 | routes `/flows/{id}/observations/{health,metrics,messages}` (`flow_service.go:207-209`); health reads ComponentManager, metrics reads Prometheus/`/metrics`, messages reads the message logger, all keyed by the diagram's node names | yes (keying); the underlying observations exist at `/components/health`, `/components/status/`, `/metrics`, message-logger |
| `processor/agentic-tools/executors/flows.go` + `register_flows.go` | 248 + 31 | 316 + 43 | `register.go:51,114,201` (`ToolDependencies.FlowManager`, gate `flows`); 5 tools `create_flow`/`update_flow`/`delete_flow`/`list_flows`/`get_flow` | yes |
| `executors/flow_templates.go` + `register_flow_templates.go` | 308 + 30 | 276 | `register.go:53,116,203`; 6 tools `create/update/delete/list/get_flow_template` + `instantiate_flow_template` | yes |
| `cmd/semstreams/main.go` wiring | ≈55 (`:24-25,245,247,707-760`) | — | — | yes |
| `cmd/e2e-semstreams/main.go` wiring | ≈50 (`:27-28,185,187,418-460`) | — | — | yes |
| `test/e2e/client/observability.go:80-114` (`FlowInfo`, `FlowsResponse`, `GetFlows` → `/flowbuilder/flows`) | 35 | — | callers: `grep -rn "GetFlows(" test/` → only the definition | yes |
| OpenAPI rows (generated) | `specs/openapi.v3.yaml:113-337` (`/flows`, `/flows/{id}`, `/validate`, `/publish-component-configs`, three `/observations/*`), schemas `Flow` `:1295`, `FlowCreateRequest` `:1380`, `FlowListResponse` `:1449`, `FlowUpdateRequest` `:1541`, Runtime*Response, `publishComponentConfigsResponse`, tags `:2202-2206` | — | regenerated by `task schema:generate` (`taskfiles/schema.yml:5-9`) | yes |
| `openspec/specs/flow-authoring/spec.md` | 394 lines, 11 requirements | — | Slices A/B truth (`TestManagerUpdate*`, `TestManagerList*`, `TestFlowExecutorListFlowsRealManagerEmpty`, `TestHandleListFlowsEmptyResponseIsNonNullArray`, `TestEnsureDefaultFlowEmptyListUsesTypedOutcome`, `TestFlowOpenAPIPreservesFlowCRUDWireSchema`, `TestFlowUpdateRequestSchemaOmitsServerAuditFields`) | yes |
| `openspec/specs/component-runtime-config/spec.md:350-368` ("Explicit Flow publication reports persistence without activation") | — | — | the publication contract living in a non-flow capability | yes |
| `openspec/specs/component-discovery/spec.md:196` | — | — | lists "Flow" among the change sources that never mutate the sealed Registry (wording only) | no |
| Docs | `docs/concepts/12-flow-architecture.md` 207 (UI mode, two buckets, static→Flow bridge); `docs/operations/migration-boot-only-flow-activation.md` 113 (ADR-096 migration; names `publish-component-configs`); `docs/operations/adopter-tool-effect-metadata.md:130-136` (tool-effect rows for the flow tools and publication); `docs/advanced/12-coordinator-pattern.md:81-154` refers to `configs/flows/*.json` — those are **boot configs**, not diagrams | — | — | the first two yes; the last two are rows/wording |
| ADR clauses | ADR-026 §"Flow-composition tool executors" (`:121-170`), §"Composition model: flows, not composite components" (`:172-198`), Consequences "Flow configs are the coordinator's artifacts — inspectable, versioned in flowstore" (`:251-252`); ADR-029 table rows "Flows"/"Flow-templates" and the Pattern-B `FlowManager`/`FlowTemplateManager` plan (`:28,33,99,154-168,180`); ADR-094 "Flow create, update, validation, and persistence remain supported" (`:47-48`) and the flowstore activation record (`:50-53`); ADR-096 Decision paragraphs 1–3 and 5, Consequences paragraph 1 (`:20-31,37-39,46-47`); ADR-027 reuses "the same runtime composition tools" (`docs/adr/026:314` cross-reference) | — | — | yes |
| `configs/protocol-flow.json:39-42` | 4 | — | the only shipped config enabling `flow-builder` (`grep -rln flow-builder --include='*.json' --include='*.yml' .` → this file) | yes |

Not flow-shaped (kept-class, reported so nothing is deleted by adjacency):

- `component/flowgraph/` — prod 1,245 (`flowgraph.go` 1,041, `flowgraph_analysis.go` 41, `doc.go` 163), test 1,932. Dependents: `engine/validator.go`, `service/component_manager.go:17,1465,1496`.
- `service/component_manager.go:1415-1560` (`GetFlowGraph`, `ValidateFlowConnectivity`, `GetFlowPaths`, `DetectObjectStoreGaps`; cache invalidated at `:1008` while the fixed set is assembled) and `service/component_manager_http.go:74-77,618-830`.
- `config.Validate` + `ValidateStreamDeclarations` + `extractPortsFromConfig` (§3).
- `component.PortDefinition/PortConfig/MergePortConfig/Resolve/Facts` (§2.1).
- `Registration.Schema` as static metadata (`component/registry.go:52-58`).
- `semstreams --validate` (`cmd/semstreams/flags.go:22,71`; `main.go:102-115`).
- `list_components` tool (`processor/agentic-tools/component_catalog_executor.go:15-42`; gate `executors/register.go:54,117,204`) — returns schema + metadata, no ports.
- `GET <components>/types`, `/types/{id}` (`component_manager_http.go:68-69,417-500`) — schema + metadata, no ports.
- `persona/` (Pattern-B sibling; untouched).
- `config.Manager.PutComponentToKV` (`config/manager.go:674-700`) — kept, but its production caller count becomes 0 once `flow_service.go:487` goes.
- `test/e2e/client/observability.go:330-400` (`ValidateFlowGraph`, `CheckFlowHealth`) + `test/e2e/scenarios/tiered.go:184-189`.
- `service/stream_override_expiry.go` (hosted by FlowService, see the table).

## 5. Adjacent claims on the territory

- ADR-094 (`:35-56`) — boot seals composition; "Flow create, update, validation, and persistence remain supported";
  desired `components.*` KV state is consumed at the next boot.
- ADR-096 — Flow is a saved diagram + compiler input; publication is explicit and upsert-only; "The Flow builder
  remains useful for authoring".
- ADR-026 — coordinator "can author durable Flow and Rule definitions"; six flow-composition executors.
- ADR-029 — Pattern-B managers for flows and flow templates.
- `openspec/specs/flow-authoring/spec.md` (11 requirements; Slices A and B landed 2026-08-25/26).
- `openspec/specs/component-discovery/spec.md:6-37,192-262` — one normalized port projection; Registry admission
  is boot-owned and seals; defensive declaration values without handles.
- `openspec/specs/component-runtime-config/spec.md:42-127` (one strict canonical port grammar), `:128-156` (input
  factories validate the effective configuration), `:308-349` (configuration activates only during construction),
  `:350-368` (Flow publication).
- `openspec/specs/stream-provisioning/spec.md:451-501` ("Port-derived stream declarations consume canonical
  normalized facts") — the precedent for a pure config→ports lane.
- `openspec/specs/framework-composition/spec.md:152` ("Component starts form a fail-closed boot barrier"),
  `:315` ("Composition consumes one captured component configuration").
- `openspec/specs/service-composition/spec.md:106,272` — composition seals before services start; fixed at boot.
- Open issues on the milestone `v1.0.0-beta.162`: #1008 (Slice C, claimed by draft PR #1088, paused), #1060
  (Slice D), #1087 (combined-candidate e2e scenarios). Closed: #1009 (Slice A), #1010 (Slice B).
- PR #1088's OpenSpec change `flow-invalid-handling-contract` (unmerged): twenty `flowstore` type constants (12
  structural + 8 graph), `ValidationResult` non-null arrays, `connection_pattern_error` as a finding.
- Foundation B port language (`docs/proposals/foundation-b-port-language-*.md`; commits `b7de684a`…`fe4e5018` on
  `component/ports.go`) — the canonical grammar that makes ports config-shaped.
- `docs/concepts/12-flow-architecture.md:20-60` still describes UI mode, "Real-time deploy/start/stop control", and
  the two-bucket bridge — already contradicted by ADR-094/096.

## 6. The consumer at birth (for surfaces the issue proposes)

Recorded as measurements only; the design decides.

| Proposed surface | Present consumer (measured) |
|---|---|
| Static port declarations on `Registration` | `engine/validator.go:244-246` (would stop constructing), `config/stream_bounds.go:259-320` (reads configured ports only, could read defaults), `cmd/openapi-generator/main.go:76-90` (exports `Registration`), `component_catalog_executor.go:30-60`, `component_manager_http.go:417-500` |
| Pure composition validator | `cmd/semstreams/main.go:102-115` (`--validate`), `internal/bootstrapobservability/bootstrap.go:213-235` (effective config gates), semstreams-ui MCP validate (`src/lib/server/mcp/tools.ts:106`), semteams MCP validate (`ui/src/lib/server/mcp/tools.ts:106`) — both currently against a Flow body |
| CLI verbs | `cmd/semstreams/flags.go:71` (`--validate` exists); product binaries cannot import `internal/bootstrapobservability` (`main.go:29`); semsource composes its own registry and owns its own `validate` verb (`~/Code/c360/semsource/cmd/semsource/run.go:148,264`; `main.go:177-185`) |
| Boot-time check | `ComponentManager.Initialize` (`service/component_manager.go:229-335`); nothing calls `ValidateFlowConnectivity`/`AnalyzeConnectivity` at boot (`grep -rn "AnalyzeConnectivity\|ValidateFlowConnectivity" --include='*.go' service/` → only the HTTP handlers and the cache method) |
| Graph projection (JSON) | `GET <components>/flowgraph` (`component_manager_http.go:618-655`); semstreams-ui does not call it (`grep -rn "flowgraph\|/components/validate" src/` → 0) |
| Graph projection (Mermaid) | `grep -rli mermaid --include='*.go' .` → 0; no consumer in any repo inspected |
| Agent tools `catalog`/`validate_composition`/`graph` | `list_components` exists (`component_catalog_executor.go:16`); no caller of a validate/graph tool exists anywhere; the flow tools are exercised by no e2e tier (`taskfiles/e2e/crud-tools.yml:3-6` scripts `create_rule` only) |
| Test helper `AssertValidComposition` | `grep -rn "AssertValidComposition\|ValidateComposition" --include='*.go' .` → 0; semsource has `cfg.Validate()` calls in its own tests (`cmd/semsource/clustering_edges_test.go:102,127`) |

## 7. Same-class collision table

Semantic class A — **"the catalog of what this binary can compose, with ports"** (a generated/durable registry).

| Dimension | Evidence |
|---|---|
| Owners | `component.Registry.factories` (`component/registry.go:118-136`); `schemas/*.v1.json` + `specs/openapi.v3.yaml` (generated, `cmd/openapi-generator`); `GET /components/types` (`component_manager_http.go:417-500`); `list_components` tool (`component_catalog_executor.go`) |
| Catalogs | the three above; none carries default ports |
| Status | none (a catalog has no readiness) |
| Lifecycle | registered at composition (`cmd/semstreams/main.go:489-516`; `cmd/e2e-semstreams`; product mains), never mutated |
| Ownership | one registry per process; `RegisterFactory` rejects duplicates (`registry.go:154-160`) |
| Readers | schema generator, HTTP types handler, catalog tool, `engine/validator.go:277-298` (`newValidationRegistry` re-registers every factory into a throwaway registry) |
| Writers | the `Register` functions only |
| Recovery | n/a (rebuilt at every boot) |

Semantic class B — **"is this composition well-formed, and what are its connections"** (a computed judgment).

| Dimension | Evidence |
|---|---|
| Owners | `config.Validate` (config-level); `flowgraph.FlowGraph` (graph-level); `engine.Validator` (diagram-level re-interpretation); `component_manager_http.handleFlowValidation` (HTTP re-interpretation); `test/e2e/client.CheckFlowHealth` (test re-interpretation: skips gateway components `observability.go:378-392`) |
| Catalogs | vocabularies: flowgraph issue constants (`flowgraph.go:56-62`); engine string literals (§3); PR #1088's proposed 20 constants (unmerged) |
| Status | `FlowAnalysisResult.ValidationStatus` (`healthy`/`warnings`/`critical`, `flowgraph.go:748-770`); engine `Status` (`valid`/`warnings`/`errors`, `validator.go:36-43`) |
| Lifecycle | engine: per request; ComponentManager: cached graph invalidated during fixed-set assembly (`component_manager.go:1008,1469`) |
| Ownership | none declared; three interpreters of one analysis |
| Readers | HTTP `/components/validate`, e2e Setup, flow-builder UI (validate route), MCP tools in two UIs |
| Writers | none durable (results are never stored; the one stored artifact was the Flow, whose `Connections` were filled from `DiscoveredConnections` at `flow_service.go:138-148`) |
| Recovery | n/a |

Semantic class C — **"the boot composition's desired state"** (durable).

| Dimension | Evidence |
|---|---|
| Owners | the config file (`config.Config{Platform, Services, Components}` `config/config.go:46-52`; loaded at `cmd/semstreams/main.go:750`) and `semstreams_config` KV desired state arbitrated by `config.Manager` at boot (`internal/bootstrapobservability/bootstrap.go:176-210`; ADR-094 `:35-40`) |
| Catalogs | `components.<instance>` keys (`service/flow_service.go:468-472` comment on the key encoder) |
| Status | boot incarnation + digest (ADR-094 `:54-56`) |
| Lifecycle | consumed once at boot; later writes are next-boot desired state |
| Ownership | Config Manager (`config/manager.go:61`) |
| Readers | boot only |
| Writers | `PutComponentToKV` — sole production caller `service/flow_service.go:487` (publication) |
| Recovery | ADR-094 §"Restart-safe shutdown and crash recovery" |

Semantic class D — **"a read-only picture of the composition"** (projection).

| Dimension | Evidence |
|---|---|
| Owners | `GET <components>/flowgraph` (nodes+edges JSON of the admitted composition); `engine.ValidationResult.Nodes/DiscoveredConnections` (diagram); `flowstore.Flow` (saved diagram with canvas positions, `flowstore/flow.go:12-55`) |
| Catalogs | OpenAPI rows `:99-112` (`/flowgraph`), `:1295` (`Flow`) |
| Readers | semstreams-ui (Flow only), semteams admin inventory (Flow list only), e2e (`/flowgraph` not called; `/validate` is) |
| Writers | flowstore (saved), FlowService default import (`:121-157`) |

## 8. Adopter seam inventory (measured facts about who carries each surface today)

Answered for a developer outside this repo writing a component, who has never opened `component/registry.go`.

| Surface | What they must know today | If they do nothing | Where they find out | What SHOULD they need to know |
|---|---|---|---|---|
| Declaring ports | implement `InputPorts()/OutputPorts()` on the component (interface `component.Discoverable`); optionally expose a `ports` config lane and defaults | the Registry captures whatever the methods return at admission; an empty declaration is admitted silently and reported later as `disconnected_node` (warning) | boot log / `/components/validate` (log line rank) | nothing beyond declaring defaults once — which every shipped factory already does in `DefaultConfig()` |
| Validating a composition before boot | run the binary with `--validate` (checks config, streams, rule packs, capabilities — not connections); or boot it and call `/components/validate` | connection errors surface only after boot, and only if something reads the endpoint | HTTP/observability (log line / doc) | run one verb, get one findings list |
| Saved-diagram authoring (today) | flow IDs, node IDs vs instance names (`Compile` uses `node.Name` as the instance name `engine/engine.go:99-101`; the default import uses the config key for both `converter.go:70-73`), version preconditions, publication semantics, reboot | a diagram that was never published is never composed; a published one is composed at the next boot only | HTTP responses + migration doc | — (the surface is what #1089 proposes to retire) |
| Product binary composition | replicate `cmd/semstreams/main.go` (`internal/bootstrapobservability` is not importable) | no validation verb unless the product writes one (semsource did) | nowhere in the framework | one exported entry point |

### Prefer observation to prediction — measured

- A **saved diagram** is a prediction of a composition; publication observed each KV write (`flow_service.go:487-520`)
  but nothing observed whether the published set would connect at boot.
- The **engine's connections** come from constructing real components (observation of construction) but never from
  the boot path (prediction of the next boot's shape).
- `/components/validate` observes the **admitted** composition (the real boundary) but runs only when asked, and
  its interpretation differs from the engine's (§3).

## 9. Downstream breakage, per repo (read-only measurement)

### semstreams-ui @ `39f5f040`

Backend calls (`grep -rn` over `src/`, tests excluded): `/flowbuilder/flows` list+create (`src/routes/flows/+page.ts:9`,
`src/lib/services/opsSummaryApi.ts:202`); `/flowbuilder/flows/{id}` get/put/delete (`src/lib/api/flows.ts:71`,
`src/routes/flows/[id]/+page.ts:8`, `src/lib/services/flowApi.ts:6`); `/validate` (`src/lib/server/mcp/tools.ts:106`,
`flowApi.ts`); `/publish-component-configs` (`src/lib/services/publishApi.ts:89`); `/observations/{health,metrics,messages}`
(`observationsApi.ts:41`, `messagesApi.ts:42`, `opsSummaryApi.ts:391-448`). Non-flow calls: `/components/types`
(`componentTypeApi.ts:15`, `mcp/tools.ts:59`), `/health`, `/health/{name}`. It does not call `/components/flowgraph`
or `/components/validate`. e2e: 10 of 30 specs reference `/flowbuilder` (`grep -rln flowbuilder e2e/`);
`e2e/global-setup.ts:2` imports `reapOrphanedTestFlows`, which lists and deletes `/flowbuilder/flows`
(`e2e/helpers/backend-helpers.ts:339-365`) on every run. Generated types: `src/lib/types/api.generated.ts` carries 16
`flows` occurrences; `npm run generate-types:check` (`package.json:30`) diffs them.

### semteams @ `8a70b7e7` (pins beta.160)

Go: `cmd/semteams/main.go:24-26` imports `engine`, `flowstore`, `flowtemplate`; `:595` calls the six-argument
`flowengine.NewEngine(configMgr, flowMgr, componentRegistry, natsClient, logger, metricsRegistry)`. That signature
exists at `v1.0.0-beta.160` and `v1.0.0-beta.161` (`git show v1.0.0-beta.161:engine/engine.go` `:31-37`); `main`
has the four-argument form (`engine/engine.go:28-33`, PR #990, merged after beta.161 was tagged 2026-08-16).
`go vet ./cmd/semteams/` at its pin succeeds (run read-only; tree unchanged). `cmd/semteams/flowtemplates/loader.go`
seeds `configs/flow-templates/*.json` (3 files) through `flowtemplate.Manager` (`main.go:627-640`), with a contract
test (`test/contract/flow_templates_seed_test.go`). UI: `ui/src/lib/api/flows.ts:92-143` calls
`/flowbuilder/deployment/{id}/{deploy,start,stop}` (retired by ADR-096 — dead on any tag after beta.161 regardless of
#1089); `ui/src/lib/services/flowApi.ts:6` and `ui/src/routes/admin/flows/+page.ts:9` read `/flowbuilder/flows`
(read-only inventory, e2e `ui/e2e/agentic/admin-flows-inventory.spec.ts`); `ui/src/lib/server/mcp/tools.ts:106`
posts to `/validate`.

### semspec @ `5a9496ee` (pins beta.134)

Generated TS types only (`ui/src/lib/types/semstreams.generated.ts`, `api.generated.ts`); `grep -rn
"flowbuilder\|/flows\b\|list_flows\|create_flow" ui/src` excluding generated/tests → 0; no Go import of
`engine`/`flowstore`/`flowtemplate`.

### semsource `4093d3c`, semconnect `d0d06e0`, semdev `ca3956a`, semboids `8c03cc5`, semmachina `841c45e`, semops `602c619`, semdragon `07f4de9`

`grep -rln` for `semstreams/engine"`, `flowstore"`, `flowtemplate"`, `/flows\b`, `publish-component-configs`,
`flowbuilder`, `create_flow`, `list_flows`, `instantiate_flow_template` over Go/TS/Svelte/JSON/MD (node_modules and
ADR history excluded): semsource → two doc mentions (`docs/integration/semteams-ui-profile-feedback.md`, an archived
openspec design); semdragon → generated `ui/static/openapi.json` and `ui/src/lib/api/generated.d.ts` only; all
others → 0.

## 10. Measured premises and PREMISE FAILED lines against the issue's framing

1. "34 schema files" — **FAILED**: 33 registered factories; `schemas/workflow-definition.v1.json` is a stale generated
   file with no factory (§2.2).
2. "Port declarations are instance-only … ports come from `InputPorts()/OutputPorts()` on a constructed component" —
   **PARTIALLY FAILED**: the *values* are read from the instance (`registry.go:564-571`), but every factory computes
   them as a pure function of its raw config (and, for objectstore, the instance name) before any dependency is used
   (§2.3, RUNTIME-ONLY = 0); 31/33 already declare them through the config-shaped `ports` lane. The real gap is that
   factory **default** ports are not on `Registration` and the derivation is not exported.
3. "The static export carries config schemas but no ports" — **HOLDS** for default port values; the export does carry
   the `ports` config lane's schema for 31 factories (§2.4).
4. "Offline validation hinges on one change: port declarations become static facts of the factory" — **HOLDS as a pure
   function, not as a constant**: 1/33 is a constant (http: none); 32/33 are `f(config[, instanceName])`; 5 of those
   derive ports from non-`ports` fields.
5. "Factories such as `udp` refuse a nil client (`input/udp/udp.go:763`)" — **HOLDS** and is stronger: 25/33 refuse
   nil dependencies and the Registry refuses first (`registry.go:228`); "construct to discover ports" through the
   admission seam succeeds for 0/33.
6. "Nothing validates the boot composition at all (`config.Validate` checks platform/security/instance names only)" —
   **FAILED**: `config.Validate` also validates every `ComponentConfig` type and factory name
   (`types/component.go:98-123`), the port-derived stream declarations (`config/config.go:256-269`), and the model
   registry (`:271-275`); boot additionally runs rule-pack and capability composition gates
   (`bootstrap.go:222-232`); `GET /components/validate` runs connectivity and stream-requirement analysis on the
   admitted composition and every tiered e2e Setup consumes it (`tiered.go:187`). What is missing is connectivity and
   interface findings **at boot itself and offline**, under **one** vocabulary.
7. "Connection-level validation exists only behind a saved Flow" — **PARTIALLY FAILED**: `disconnected_node`,
   `orphaned_port`, and stream-requirement findings exist for the running composition without a Flow; only
   `interface_mismatch`, `missing_interface`, `unknown_component`, and `empty_flow` are engine-only
   (`engine/validator.go:491-610,223-235,99-113`).
8. "131 test lines against 1,273 production" — **PARTIALLY**: engine = 837 prod / 131 test; the remaining production
   lines belong to `component/flowgraph` (1,245 prod / 1,932 test) — the well-tested half is the substrate.
9. "The five `*_flow` executors" — **FAILED on count**: 5 flow tools plus 6 flow-template tools (11 tools, two
   executors); the templates render `*flowstore.Flow` (`flowtemplate/template.go:90`) and cannot outlive flowstore.
10. "P7 writes go through the existing boot-only Config Manager path" — **PARTIALLY FAILED**: the lane exists
    (`config/manager.go:684`) but its only production caller is the publication handler the issue removes
    (`service/flow_service.go:487`); after removal no HTTP route or tool reaches it.
11. "semteams … its already-broken `engine` import" — **FAILED at every released tag**: it compiles at beta.160 and
    beta.161; it fails only against unreleased `main` (§9).
12. "P1 … exported by the product's own binary" — **needs an exported entry point**: `internal/bootstrapobservability`
    is not importable by products (`cmd/semstreams/main.go:29`); semsource replicates composition and owns its own
    `validate` verb (§6).
13. Hidden coupling — **FOUND**: `service/stream_override_expiry.go` (stream-provisioning's override-expiry metric) is
    constructed and registered only by `FlowService` (`flow_service.go:560-585`); removing the service removes the
    metric unless it is rehomed.

## 11. Searches that closed empty

- `grep -rn "PortDeclarations\|StaticPorts\|DeclaredPorts\|Ports(config\|AssertValidComposition\|ValidateComposition" --include='*.go' .` → 0 (no prior static-port or composition-assert art).
- `grep -rli mermaid --include='*.go' .` → 0.
- `grep -rn "AnalyzeConnectivity\|ValidateFlowConnectivity" --include='*.go' service/` (non-test) → only the cache method and the HTTP handlers; nothing at boot.
- `grep -rn "GetFlows(" test/` → definition only.
- `grep -rln "flow-builder" --include='*.json' --include='*.yml' --include='*.yaml' .` (main tree) → `configs/protocol-flow.json` only.
- `grep -rln "flows\b\|/flows\|FlowService\|flow-builder" test/e2e --include='*.go'` → `client/observability.go`, `mock/openai_server.go` (word "flows" in prose), `scenarios/lifecycle/scenario.go` (`/lifecycle-gateway/workflows`), `scenarios/validate_entity.go` (prose) — no e2e tier drives `/flowbuilder/*` or the flow tools.
- `grep -rn "flowbuilder\|/flows\b" ui/src` in semspec and semdragon (excluding generated, tests) → 0.
- `grep -n -i "saved flow\|flowstore\|flow-builder\|flowbuilder\|publish-component-configs\|Flow diagram" openspec/specs/*/spec.md` (excluding `flow-authoring`) → `component-runtime-config/spec.md:359,366` only.
- `openspec list` shows no open change touching `flow-authoring` other than PR #1088's unmerged `flow-invalid-handling-contract` (read via the GitHub API at `claude/gh1008-flow-invalid-handling`).

## 12. Open evidence questions (for the inventory reviewer; not design questions)

1. Whether any shipped `configs/*.json` composition would carry an error-severity connectivity finding today is
   unmeasured (no offline validator exists; the e2e tiers measure only their own compose configs through
   `CheckFlowHealth`).
2. `resolveObjectStorePorts` uses the instance name for the `store-provide` port while the factory passes the
   literal `"objectstore"` (`storage/objectstore/component.go:180`); whether the admitted declaration ever carries
   the real instance name was not traced past `captureComponentDeclaration`.
3. semdragon and semspec pin beta.135/134 — pre-ADR-094; their generated artifacts predate every flow change since
   and were not diffed.
