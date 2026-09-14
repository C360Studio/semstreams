# Builder-path and workflow-retirement inventory

base: ea22e6a4e75d12bf7f6050c6d8121de1d089b191

Two explorers enumerated the existing builder example and #457's retirement surface. The source baseline is
unchanged in the claimed worktree. Root independently exercised the existing example, recorded below.
No new framework surface or runtime primitive is proposed; collision/adoption design tables do not apply.

## Current source counterparts

- `component/ports.go:53` — `type PortDefinition struct {`
- `payloadregistry/registry.go:98` — `func (pr *Registry) Register(registration *Registration) error {`

- `processor/rule/entity_pattern_contract.go:17` — `var errEntityWatchBucketUnsupported = errors.New("rule EntityState watcher supports only ENTITY_STATES; typed operational-KV rule adapters require a separately designed decoder and evaluator")`
- `processor/rule/interfaces.go:50` — `EvaluateEntityState(ctx context.Context, entityState *gtypes.EntityState) bool`
- `openspec/specs/graph-ingest/spec.md:5` — `graph-ingest is the **sole writer to `ENTITY_STATES`**, and this capability governs what that`
- `openspec/specs/lifecycle/spec.md:110` — `- **GIVEN** `ENTITY_STATES` retains one KV revision`

- `taskfiles/dev.yml:56` — `- go build -o bin/semstreams ./cmd/semstreams`
- `taskfiles/dev.yml:84` — `- ./bin/semstreams --config configs/hello-world.json`
- `configs/hello-world.json:97` — `"name": "iot_sensor",`
- `configs/hello-world.json:98` — `"enabled": true,`
- `cmd/e2e-semstreams/main.go:909` — `// These are kept out of componentregistry.Register() so that downstream`
- `taskfiles/dev.yml:125` — `echo '     Query with: curl -s http://localhost:8084/graphql -H "Content-Type: application/json" -d '"'"'{"query":"{ entitiesByPrefix(prefix: \"demo\", limit: 10) { entities { id } next_cursor } }"}'"'"''`
- `gateway/graph-gateway/component.go:77` — `StandaloneServer          bool                  `json:"standalone_server" schema:"type:bool,description:Create a standalone HTTP server (for tests/development). When false ServiceManager provides HTTP serving,category:basic"``
- `service/service_manager.go:1550` — `prefix := "/" + name`
- `service/service_manager.go:1551` — `gateway.RegisterHTTPHandlers(prefix, mux)`
- `gateway/graph-gateway/component.go:981` — `graphqlPath := prefix + c.config.GraphQLPath`
- `gateway/graph-gateway/component.go:992` — `mux.HandleFunc(graphqlPath, c.admittedHTTP(c.handleGraphQL))`
- `go.mod:3` — `go 1.26.3`
- `taskfiles/build.yml:16` — `e2e-semstreams:`
- `taskfiles/build.yml:19` — `- go build -o bin/e2e-semstreams ./cmd/e2e-semstreams`
- `Taskfile.yml:26` — `build:`
- `cmd/e2e-semstreams/main.go:72` — `if code, handled := dispatchCompositionVerb(os.Args[1:]); handled {`
- `cmd/e2e-semstreams/main.go:94` — `return compositioncli.Main(args, registry, os.Stdout, os.Stderr), true`
- `cmd/e2e-semstreams/main.go:109` — `if err := registerExampleComponents(registry); err != nil {`
- `cmd/e2e-semstreams/main.go:600` — `validate <config-path>           print composition findings; exit 1 on errors`
- `cmd/e2e-semstreams/main.go:537` — `case arg == "-c" || arg == "--config":`
- `cmd/e2e-semstreams/main.go:548` — `case arg == "--lifecycle-seed":`
- `cmd/e2e-semstreams/main.go:653` — `if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {`
- `cmd/e2e-semstreams/main.go:655` — `} else if len(cfg.NATS.URLs) > 0 {`
- `cmd/e2e-semstreams/main.go:530` — `ShutdownTimeout: 30 * time.Second,`
- `cmd/e2e-semstreams/main.go:531` — `LifecycleSeed:   os.Getenv("SEMSTREAMS_LIFECYCLE_SEED"),`
- `cmd/e2e-semstreams/main.go:302` — `if cliCfg.LifecycleSeed == "" {`
- `cmd/e2e-semstreams/main.go:790` — `signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)`
- `cmd/e2e-semstreams/main.go:825` — `<-shutdownRequested`
- `cmd/e2e-semstreams/main.go:842` — `shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)`
- `configs/hello-world.json:4` — `"org": "demo",`
- `configs/hello-world.json:5` — `"id": "hello-world",`
- `configs/hello-world.json:10` — `"tier": "rules",`
- `configs/hello-world.json:12` — `"urls": ["nats://localhost:4222"]`
- `configs/hello-world.json:18` — `"http_port": 8080,`
- `configs/hello-world.json:34` — `"port": 9090,`
- `configs/hello-world.json:56` — `"max_age": "24h",`
- `configs/hello-world.json:64` — `"max_age": "24h",`
- `configs/hello-world.json:82` — `"raw.sensor.udp"`
- `configs/hello-world.json:89` — `"bind": "0.0.0.0",`
- `configs/hello-world.json:90` — `"port": 14550,`
- `configs/hello-world.json:105` — `"stream_name": "RAW",`
- `configs/hello-world.json:107` — `"raw.sensor.>"`
- `configs/hello-world.json:117` — `"type": "iot.sensor.v1"`
- `configs/hello-world.json:121` — `"sensor.processed.entity"`
- `configs/hello-world.json:156` — `"stream_name": "SENSOR",`
- `configs/hello-world.json:157` — `"subjects": [`
- `configs/hello-world.json:167` — `"bucket": "ENTITY_STATES",`
- `configs/hello-world.json:187` — `"bucket": "ENTITY_STATES",`
- `configs/hello-world.json:231` — `"name": "graph-query",`
- `configs/hello-world.json:256` — `"name": "graph-gateway",`
- `configs/hello-world.json:292` — `"graphql_path": "/graphql",`
- `processor/graph-query/component.go:52` — `MaxDepth             int                   `json:"max_depth,omitempty" schema:"type:int,description:Maximum traversal depth for path search,default:10,min:1,max:100,category:basic"``
- `processor/graph-query/component.go:227` — `if err := json.Unmarshal(rawConfig, &config); err != nil {`
- `examples/processors/iot_sensor/register.go:17` — `Name:        "iot_sensor",`
- `examples/processors/iot_sensor/register.go:18` — `Factory:     NewComponent,`
- `examples/processors/iot_sensor/register.go:19` — `Ports:       DeclarePorts,`
- `examples/processors/iot_sensor/component.go:149` — `_, inputs, outputs, _, err := resolveConfig(rawConfig)`
- `examples/processors/iot_sensor/component.go:158` — `// derivation DeclarePorts and NewComponent share.`
- `examples/processors/iot_sensor/payload.go:106` — `func RegisterPayloads(reg *payloadregistry.Registry) error {`
- `examples/processors/iot_sensor/payload.go:108` — `Domain:      "iot",`
- `cmd/e2e-semstreams/main.go:409` — `if err := iotsensor.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:248` — `svcDeps.PayloadRegistry = payloadReg`
- `cmd/e2e-semstreams/main.go:730` — `if err := registerExampleComponents(componentRegistry); err != nil {`
- `cmd/e2e-semstreams/main.go:912` — `if err := iotsensor.Register(registry); err != nil {`
- `input/udp/udp.go:713` — `publishErr = u.natsClient.PublishToStream(ctx, u.subject, data)`
- `examples/processors/iot_sensor/component.go:615` — `if err := json.Unmarshal(msgData, &data); err != nil {`
- `examples/processors/iot_sensor/component.go:624` — `reading, err := c.processor.Process(data)`
- `examples/processors/iot_sensor/component.go:648` — `c.emitGraphable(ctx, zone, message.Type{`
- `examples/processors/iot_sensor/component.go:672` — `baseMsg := message.NewBaseMessage(msgType, payload, c.name)`
- `examples/processors/iot_sensor/processor.go:136` — `ZoneEntityID:  ZoneEntityID(p.authority, zoneType, locationID),`
- `examples/processors/iot_sensor/processor.go:137` — `EntityIDValue: SensorReadingEntityID(p.authority, sensorType, deviceID),`
- `examples/processors/iot_sensor/payload.go:198` — `func SensorReadingEntityID(authority types.PlatformMeta, sensorType, deviceID string) string {`
- `processor/graph-ingest/component.go:712` — `if deps.PayloadRegistry == nil {`
- `processor/graph-ingest/component.go:753` — `decoder:                       message.NewDecoder(deps.PayloadRegistry),`
- `processor/graph-ingest/component.go:1675` — `baseMsg, err := c.decoder.Decode(data)`
- `message/decoder.go:33` — `func NewDecoder(reg *payloadregistry.Registry) *Decoder {`
- `message/decoder.go:45` — `msg := &BaseMessage{registry: d.registry}`
- `examples/processors/iot_sensor/processor_test.go:16` — `func TestProcessor_Process_JSONTransformation(t *testing.T) {`
- `examples/processors/iot_sensor/processor_test.go:120` — `func TestProcessor_Process_MintsUnderDeploymentAuthority(t *testing.T) {`
- `examples/processors/iot_sensor/payload_test.go:26` — `func TestSensorReading_EntityID_6PartFormat(t *testing.T) {`
- `examples/processors/iot_sensor/payload_test.go:96` — `func TestSensorReading_Triples_SemanticPredicates(t *testing.T) {`
- `examples/processors/iot_sensor/deployment_authority_test.go:25` — `discoverable, err := NewComponent(encoded, testDependencies())`
- `examples/processors/iot_sensor/deployment_authority_test.go:64` — `if decoded.EntityID() != reading.EntityID() {`
- `examples/processors/iot_sensor/payload_build_test.go:22` — `registry := payloadregistry.NewWithSubset(t, RegisterPayloads)`
- `examples/processors/iot_sensor/payload_build_test.go:26` — `built, err := registry.Build(`
- `examples/processors/iot_sensor/component_integration_test.go:1` — `//go:build integration`
- `examples/processors/iot_sensor/component_test.go:38` — `NATSClient: nil, // Will be nil for creation test`
- `gateway/graph-gateway/port_config_test.go:18` — `"configs/hello-world.json",`
- `frameworkcapabilities/graphresearch/register.go:133` — `for _, componentConfig := range cfg.Components {`
- `frameworkcapabilities/graphresearch/register.go:170` — `if !Selected(cfg) {`
- `cmd/e2e-semstreams/main.go:227` — `if err := executors.RegisterBuiltins(ctx, toolRegistry, executors.ToolDependencies{`
- `cmd/e2e-semstreams/main.go:240` — `if graphresearch.Selected(cfg) {`
- `cmd/e2e-semstreams/main.go:270` — `// Start can abort boot on a genuine consumer-start failure; stream absence skips.`
- `taskfiles/dev.yml:13` — `if docker ps -a --format '{{.Names}}' | grep -q '^semstreams-nats$'; then`
- `taskfiles/dev.yml:123` — `echo '{"device_id":"sensor-001","type":"temperature","reading":23.5,"unit":"celsius","location":"warehouse-7","zone_type":"cold-storage"}' | nc -u localhost 14550`
- `pkg/context/doc.go:92` — `// When using [ConstructedContext] with the workflow processor's publish_agent action,`
- `configs/rules/deep-research/03-fan-out-subtopics.json:5` — `"description": "Fires when the research-coordinator decides fan_out. Spawns a sub-researcher that fetches the coordinator's subtopic list via read_loop_result and investigates one. True parallel fan-out (N agents at once) is out of scope for this milestone — that needs the coordinator's flow-composition tools (ADR-026 milestone 2). See ADR-026 + ADR-028.",`
- `processor/rule/actions.go:324` — `// this many times for a given rule+entity match-cycle, regardless of`
- `processor/rule/actions.go:411` — `// ForEach is a substitution-resolvable reference to a list-typed`
- `processor/rule/actions.go:295` — `When []expression.ConditionExpression `json:"when,omitempty"``
- `cmd/e2e-semstreams/main.go:441` — `// effective platform.id carries an entropy suffix minted at first boot, so a`

## Adopter seam

| Reader | Burden and default result | Discovery | Required simplification |
| --- | --- | --- | --- |
| Go builder | Wrong binary omits the example; copied registry and port APIs disagree | Compile/admission errors | Name the example composition and link maintained source |
| Local operator | Advertised query URL differs from mounted route | HTTP connection/route failure | One tested URL and observable sensor fact |
| App coordinator author | Legacy banners coexist with retired builder instructions | Missing package or contradictory docs | Current pattern choices and no retired executable examples |

The domain author still chooses entity semantics and application policy. Compilation, composition validation and
observed dataflow are distinct checks. Current context/coordination destinations contain stale examples too;
linking them does not certify every example. Retained pointers must describe conceptual scope honestly.

## Adjacent claims

- #1304 owns the checked builder path and shortened tutorial; #457 owns full tutorial/concept retirement.
- #486's Markdown portion overlaps; its JSON comment remains outside the edit scope and the issue stays open.
- #1302 / PR #1303 owns the broad audit. #1286 records a related harness guard defect; the observed CI timeout
  does not prove its cause, and no waiver is assumed.

Baseline documentation observations below use immutable links because these are the files this cleanup changes.
Their text is evidence of the starting state, not a claim about the resulting guides.

- [examples/processors/iot_sensor/README.md:136](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/examples/processors/iot_sensor/README.md#L136) — `go test -race ./examples/processors/iot_sensor/...`
- [openspec/project.md:63](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/openspec/project.md#L63) — `- Production binaries compose core, retained framework capabilities, optional`
- [docs/basics/08-workflow-quickstart.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L3) — `> **Legacy guide.** This page describes the retired `processor/reactive``
- [docs/basics/08-workflow-quickstart.md:10](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L10) — `Get started with SemStreams reactive workflow orchestration for multi-step processes.`
- [docs/basics/08-workflow-quickstart.md:32](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L32) — `**Quick decision**: If it needs a loop limit, it's a workflow.`
- [docs/basics/08-workflow-quickstart.md:67](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L67) — `"github.com/c360studio/semstreams/processor/reactive"`
- [docs/basics/08-workflow-quickstart.md:98](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L98) — `return reactive.NewWorkflow("linear-example").`
- [docs/advanced/09-workflow-configuration.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/09-workflow-configuration.md#L3) — `> **Legacy reference.** This page describes the retired`
- [docs/advanced/09-workflow-configuration.md:9](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/09-workflow-configuration.md#L9) — `Complete reference for configuring and building reactive workflows in SemStreams. The reactive workflow`
- [docs/advanced/09-workflow-configuration.md:86](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/09-workflow-configuration.md#L86) — `"github.com/c360studio/semstreams/processor/reactive"`
- [docs/advanced/09-workflow-configuration.md:628](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/09-workflow-configuration.md#L628) — `The rule processor can trigger workflows using the `trigger_workflow` action:`
- [docs/advanced/10-reactive-workflows.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/10-reactive-workflows.md#L3) — `> **Legacy reference.** This page describes the retired`
- [docs/advanced/10-reactive-workflows.md:9](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/10-reactive-workflows.md#L9) — `The reactive workflow engine enables multi-step coordination using typed Go functions instead of JSON configuration with string interpolation. Workflows are defined in Go code, providing compile-time type safety and eliminating serialization bugs.`
- [docs/advanced/10-reactive-workflows.md:334](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/10-reactive-workflows.md#L334) — `See `/cmd/e2e-semstreams/workflows.go` for production-ready workflow examples including:`
- [docs/concepts/23-parallel-agents.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/23-parallel-agents.md#L3) — `> **Legacy pattern guide.** This page documents the older`
- [docs/concepts/23-parallel-agents.md:87](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/23-parallel-agents.md#L87) — `Use reactive workflow rules with `ActionPublishAsync` to spawn multiple agent tasks.`
- [docs/concepts/23-parallel-agents.md:675](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/23-parallel-agents.md#L675) — `1. **Replace parallel steps with reactive rules** that publish multiple `TaskMessage` payloads`
- [docs/concepts/22-context-construction.md:169](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/22-context-construction.md#L169) — `## Integration with Workflows`
- [docs/concepts/22-context-construction.md:192](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/22-context-construction.md#L192) — `"type": "parallel",`
- [pkg/context/README.md:143](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/pkg/context/README.md#L143) — `When using `ConstructedContext` with the workflow processor's `publish_agent` action:`
- [processor/rule/docs/custom-rules.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/processor/rule/docs/custom-rules.md#L3) — `> **⚠️ DEPRECATED**: The JSON-based rules engine is superseded by the Reactive Workflow Engine`
- [processor/rule/docs/operations.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/processor/rule/docs/operations.md#L3) — `> **⚠️ DEPRECATED**: The JSON-based rules engine is superseded by the Reactive Workflow Engine`
- [docs/operations/02-troubleshooting.md:209](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/operations/02-troubleshooting.md#L209) — `## Workflow Issues`
- [docs/concepts/14-orchestration-layers.md:28](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L28) — `Retirement completed in `main`; only `pkg/workflow/` (state-manager`
- [docs/concepts/14-orchestration-layers.md:477](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L477) — `That code is now a migration blocker. It imports the retired`
- [docs/concepts/27-frontier-harness-mapping.md:225](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/27-frontier-harness-mapping.md#L225) — `**The semspec workflow trap** (referenced in ADR-045 §Context "The`
- [docs/README.md:78](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/README.md#L78) — `- [Workflow Quickstart](basics/08-workflow-quickstart.md) - Get started with workflows`
- [docs/README.md:79](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/README.md#L79) — `- [Workflow Configuration](advanced/09-workflow-configuration.md) - Complete schema reference`
- [docs/basics/07-agentic-quickstart.md:386](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/07-agentic-quickstart.md#L386) — `- [Workflow Quickstart](08-workflow-quickstart.md) - Multi-step workflow orchestration`
- [docs/concepts/22-context-construction.md:253](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/22-context-construction.md#L253) — `- [Parallel Agents](23-parallel-agents.md) - Parallel execution with context`
- [docs/concepts/22-context-construction.md:254](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/22-context-construction.md#L254) — `- [Workflow Configuration](../advanced/09-workflow-configuration.md) - Workflow processor`
- [pkg/context/README.md:187](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/pkg/context/README.md#L187) — `- [Workflow Configuration](../../docs/advanced/09-workflow-configuration.md) - Workflow processor`
- [processor/rule/docs/custom-rules.md:5](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/processor/rule/docs/custom-rules.md#L5) — `> See [Reactive Workflows Guide](/docs/advanced/10-reactive-workflows.md).`
- [processor/rule/docs/operations.md:4](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/processor/rule/docs/operations.md#L4) — `> (ADR-021). See [Reactive Workflows Guide](/docs/advanced/10-reactive-workflows.md).`
- [docs/advanced/06-rules-engine.md:13](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/06-rules-engine.md#L13) — `> [Reactive Workflows](./10-reactive-workflows.md) page is a legacy reference only.`
- [docs/basics/08-workflow-quickstart.md:621](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/08-workflow-quickstart.md#L621) — `- [Reactive Workflows Guide](../advanced/10-reactive-workflows.md) — Comprehensive reference documentation`
- [docs/concepts/23-parallel-agents.md:668](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/23-parallel-agents.md#L668) — `- [Reactive Workflows](../advanced/10-reactive-workflows.md) - Legacy reactive workflow engine guide`
- [docs/proposals/agentic-trajectory-contract-inventory.md:268](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/proposals/agentic-trajectory-contract-inventory.md#L268) — `- `docs/concepts/23-parallel-agents.md:145-170`, `:257-275`, `:445-459`, `:693-707`;`
- [docs/proposals/gh865-866-terminal-event-design.md:486](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/proposals/gh865-866-terminal-event-design.md#L486) — `- `docs/concepts/23-parallel-agents.md:77-83` names all three event types, but many examples assert only`
- [docs/proposals/gh865-866-terminal-event-inventory.md:470](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/proposals/gh865-866-terminal-event-inventory.md#L470) — `- `docs/concepts/23-parallel-agents.md:77-83` names all three event types, but many examples assert only`
- [docs/ROADMAP.md:97](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/ROADMAP.md#L97) — `SemStreams no longer carries a separate reactive workflow engine. Current`
- [docs/advanced/06-rules-engine.md:3](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/06-rules-engine.md#L3) — `> **Status: current.** The JSON rules engine (`processor/rule/`) is the blessed`
- [docs/concepts/14-orchestration-layers.md:18](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L18) — `A reactive workflow engine (`processor/reactive/`) shipped early in`
- [docs/operations/migration-beta162-to-beta163.md:278](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/operations/migration-beta162-to-beta163.md#L278) — `workflow processor was retired, and `test/contract` carried a `nonComponentSchemas` exemption so the`
- [docs/advanced/11-jetstream-tuning.md:253](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/advanced/11-jetstream-tuning.md#L253) — `js, err := nc.JetStream(nats.PublishAsyncMaxPending(64))`
- [processor/rule/docs/entity-watching.md:115](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/processor/rule/docs/entity-watching.md#L115) — `| Authoritative `WatchAll` guard | Validates `ENTITY_STATES` values and advances the revision barrier |`
- [docs/concepts/14-orchestration-layers.md:149](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L149) — `### Pattern 2 — Linear pipeline`
- [openspec/specs/lifecycle/spec.md:5](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/openspec/specs/lifecycle/spec.md#L5) — `Define the reusable named-instance lifecycle surface over canonical graph exact reads and mutations. Lifecycle records`
- [docs/concepts/14-orchestration-layers.md:254](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L254) — `### Pattern 5 — Async fan-out / fan-in`
- [docs/concepts/25-phased-agentic-chains.md:49](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/25-phased-agentic-chains.md#L49) — `## Composition`
- [openspec/specs/agentic-terminal-events/spec.md:50](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/openspec/specs/agentic-terminal-events/spec.md#L50) — `The framework SHALL recognize exactly `loop_completed + success`, `loop_failed + failed`, and`
- [docs/concepts/14-orchestration-layers.md:496](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L496) — `### Symptom: action fires multiple times unexpectedly`
- [openspec/project.md:14](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/openspec/project.md#L14) — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- [docs/concepts/14-orchestration-layers.md:199](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L199) — `"when": "$state.validation.passed == true"`

- [README.md:71](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/README.md#L71) — `task dev:start`
- [README.md:97](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/README.md#L97) — `You should see your sensor entity ID under `data.entitiesByPrefix.entities`. If`
- [README.md:267](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/README.md#L267) — `- **Go 1.25+** — [Download](https://go.dev/dl/)`
- [docs/basics/00-prerequisites.md:9](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/00-prerequisites.md#L9) — `| Go | 1.25+ | Build and run SemStreams |`
- [docs/basics/05-first-processor.md:170](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/05-first-processor.md#L170) — `err := payloadregistry.Register(&payloadregistry.Registration{`
- [docs/basics/05-first-processor.md:1170](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/05-first-processor.md#L1170) — `{Name: "input", Type: "nats", Subject: "raw.sensor.>"},`
- [docs/basics/05-first-processor.md:1356](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/05-first-processor.md#L1356) — `- [ ] Payload registered in init() with payloadregistry.Register()`
- [docs/concepts/15-payload-registry.md:75](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/15-payload-registry.md#L75) — `function. There is no `init()` and no package-level singleton — the registry is an explicit instance`
- [docs/README.md:5](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/README.md#L5) — `**Building with a coding agent?** Start with`

- [README.md:92](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/README.md#L92) — `curl -s http://localhost:8084/graphql \`
- [docs/basics/00-prerequisites.md:33](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/00-prerequisites.md#L33) — `Go 1.25 or higher is required.`
- [docs/basics/05-first-processor.md:788](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/05-first-processor.md#L788) — `Type:        "nats",`
- [docs/basics/05-first-processor.md:789](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/05-first-processor.md#L789) — `Subject:     "raw.sensor.>",`
- [docs/README.md:41](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/README.md#L41) — `5. [First Processor](basics/05-first-processor.md) - Complete working example`
- [docs/README.md:80](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/README.md#L80) — `- [Orchestration Layers](concepts/14-orchestration-layers.md) - When to use rules vs. workflows`
- [docs/concepts/15-payload-registry.md:74](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/15-payload-registry.md#L74) — `Every package that owns a payload type exports a `RegisterPayloads(reg *payloadregistry.Registry) error``
- [docs/concepts/15-payload-registry.md:237](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/15-payload-registry.md#L237) — `No `init()`. Add an exported `RegisterPayloads(reg *payloadregistry.Registry) error` function that the`
- [docs/concepts/15-payload-registry.md:287](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/15-payload-registry.md#L287) — `**Critical**: a `RegisterPayloads` function nothing calls never runs — there is no `init()` to fall back on. Add`

- [docs/concepts/14-orchestration-layers.md:146](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L146) — `**State**: lives on the `COMPLETE_{loopID}` KV entry the upstream`
- [docs/concepts/14-orchestration-layers.md:174](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L174) — `entity in an existing KV bucket (e.g., `AGENT_LOOPS`). Bulky payloads`
- [docs/concepts/14-orchestration-layers.md:338](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L338) — `- Use `COMPLETE_{id}` key pattern for rules observability`
- [docs/concepts/14-orchestration-layers.md:345](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L345) — `The rule processor's entity evaluator does not decode these buckets. Its`
- [docs/concepts/14-orchestration-layers.md:88](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/14-orchestration-layers.md#L88) — `threaded per evaluation (`agentic-loop` owns MaxAckPending=1 on`
- [docs/basics/09-building-semsource.md:7](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/09-building-semsource.md#L7) — `This walkthrough follows one document passage from a file into a context response. It explains the implementation`
- [docs/basics/09-building-semsource.md:25](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/basics/09-building-semsource.md#L25) — `| Concern | SemSource owns | SemStreams provides |`

The same orchestration chapter both suggests private completion-key watches and says its evaluator does not
decode those buckets; the current source refusal is pinned above. Its MaxAckPending/fan-out assertion remains
unverified. The SemSource chapter already provides the desired editorial shape: a source-backed walkthrough,
version limits and an application/framework ownership table. No new documentation system is needed.

## Root observations and limits

On 2026-09-14, root ran the existing IoT package race tests, built both existing binaries, and ran lint: pass.
The E2E binary's offline validation of `configs/hello-world.json` exited zero with no errors and seven optional
API/index warnings. Those warnings are retained evidence, not a warning-free composition claim.

An isolated NATS 2.14.4 container and the unchanged E2E binary/config reached HTTP `/readyz` 200. A bounded UDP
netcat send and POST to `http://localhost:8080/graph-gateway/graphql` returned the sensor and zone entities, with
measurement 23.5 and a zone reference. The observed sensor ID was
`demo.hello-world-467884.sensor.environmental.temperature.sensor-001`; deployment identity has a boot-minted suffix.
The explorer's predicted unsuffixed ID was not an observed result. Its `--lifecycle-seed=` candidate is not the
CLI's parsed spelling; no such flag is needed or used in the checked recipe. NATS/lifecycle override variables
were unset in the observed run. No agentic model/loop/embedding component was enabled by this config.

The structural run logged optional community-index/summary unavailability because clustering was not selected.
The prefix query succeeded; no claim is made about semantic search, offline synchronization, benchmark limits,
full E2E tiers or production composition. SIGINT shut down the process with exit zero, then root removed only
its own temporary NATS container. No Go/config/task/CI or sister repository was edited.

The retirement scan separates historical ADRs/proposals/migrations, legitimate reactive observation/NATS APIs,
and current how-to claims. Majority/first-success aggregation and sister migration status were not verified.
Some target examples have old action shapes; they must not gain runnable status merely through a new link.

## Decision-skill checks

The root read canonical orchestration-check and entity-or-bucket skills for the replacement destination's
ownership language. Rules trigger work, components execute, lifecycle describes named entity phase, and
operational artifacts retain separate ownership. Current EntityState rule watches admit only ENTITY_STATES;
generic operational-KV adapters are not implemented. This corrects the old catalog's private-bucket suggestions
without adding a communication path or new primitive. Example actions and aggregation guarantees remain bounded
to inspected current types/contracts, not assumed from a retired tutorial or reference-pack label.

## Searches

The collector logs follow verbatim apart from heading depth. Their unsuccessful filename/structural queries
are explicitly limited and never establish absence. Root also checked fixed-port availability, offline validator
output, readiness, actual query data, shutdown, and `cmd/e2e-semstreams/main.go:518–558, 441` for CLI/identity.

### Builder entry

Counts below are matching lines, recalculated with argument-vector git grep (tracked paths only). `git grep -n -E PATTERN -- PATHS` unless marked -F.

| ID | Pattern | Paths | Hits |
|---|---|---|---:|
|1|`^  [a-z]∣deps:∣cmd:∣cmds:∣go build∣go test∣docker∣bin/∣hello-world∣nats`|taskfiles/build.yml taskfiles/dev.yml Taskfile.yml|126|
|2|`"name"∣"type"∣"enabled"∣"port"∣"url"∣"model∣"tier∣"embed∣"org"∣"platform"∣"id"∣"subjects"∣"stream_name"∣"bucket"∣"default∣"storage∣"timeout`|configs/hello-world.json|65|
|3|`flag\.∣configPath∣Load∣ReadFile∣Register∣research∣Config∣Start∣Timeout∣time.Second`|cmd/e2e-semstreams/main.go|110|
|4|`fixture∣seed∣mission∣publish∣Selected∣Enable`|cmd/e2e-semstreams/fixtures/*.go cmd/e2e-semstreams/main.go|58|
|5|`^func Test∣NewDecoder∣RegisterPayloads∣Process\(∣EntityID∣Triples∣Platform∣Register\(`|examples/processors/iot_sensor/*_test.go|117|
|6|`hello-world.json` (-F)|*_test.go docker configs taskfiles|6|
|7|`8084∣Listen∣Port∣GraphQL∣Validate∣MaxTraversal∣Max.*Nodes`|gateway/graph-gateway/config.go gateway/graph-gateway/component.go|126|
|8|`LookupEnv∣Lookup∣NATS∣nats∣os.Getenv∣Environment∣ExpandEnv`|config/loader.go config/env.go cmd/e2e-semstreams/main.go|58|
|9|`Selected∣func ∣Enabled∣graphresearch∣Component`|frameworkcapabilities/graphresearch/capability.go frameworkcapabilities/graphresearch/register.go|27|
|10|`RegisterHTTPHandlers∣GraphQLPath∣ServiceManager∣HTTPHandler`|gateway/graph-gateway/component.go service/manager.go service/componentmanager.go|24|
|11|`RegisterHTTPHandlers∣HTTPHandler∣httpService∣GetHTTP∣httpPrefix`|service|54|
|12|`go:build∣testcontainers∣docker∣os.Getenv∣http\.∣net\.∣NATS∣nats://`|examples/processors/iot_sensor/*_test.go|22|
|13|`go test∣go run∣task ∣Requirements∣Prerequisite∣Go 1∣nc ∣curl `|examples/processors/iot_sensor/README.md go.mod|6|
|14|`NewDecoder∣decoder∣PayloadRegistry∣RegisterVocabulary∣NewBaseMessage∣json.Unmarshal∣SensorReadingEntityID∣ZoneEntityID∣rawPayload∣RawPayload`|cmd/e2e-semstreams/main.go examples/processors/iot_sensor/component.go examples/processors/iot_sensor/processor.go examples/processors/iot_sensor/payload.go processor/graph-ingest/component.go input/udp/input.go|41|
|15|`max_traversal_depth∣max_traversal_nodes∣MaxTraversal∣func .*Default∣json.Unmarshal`|processor/graph-query/config.go processor/graph-query/component.go|3|
|16|`VerbValidate∣validate <config∣validate <path∣validate <`|*.go|14|
|17|`NewBaseMessage∣GenericJSON∣Generic∣Publish∣Data∣JSON∣payload`|input/udp|60|
|18|`entitiesByPrefix∣limit∣prefix`|gateway/graph-gateway/schema/query.graphql gateway/graph-gateway/schema.graphql gateway/graph-gateway/schema/schema.graphql|0|

In the table `∣` represents literal ASCII `|` alternation in the actual executed pattern (avoids Markdown table splitting).
- `gopls symbols examples/processors/iot_sensor/deployment_authority_test.go` → 2 file symbols; processor_test.go → 7; payload_test.go → 7. Each returned workspace load/cache-write permission failures; file-local symbols only, no complete semantic caller closure.
- `git ls-files 'examples/processors/iot_sensor/*_test.go'` → 11 paths.
- Failed lookup: unquoted `pkg/compositioncli/*.go` in a git grep invocation → zsh glob error, search not executed. Located actual composition/cli/main.go through search16; body NOT READ.
- Failed read: `sed -n '1, ninety p'` → invalid address; deployment_authority_test.go subsequently read successfully lines1–95.
- Selected line reads: project Purpose/Boundary; taskfiles/dev.yml116–178; hello-world1–68,68–133,228–303; E2E main67–115,118–166,213–242,247–307,398–429,504–572,642–662,712–744,766–850,903–926; gateway component70–155,690–762,964–1018; service_manager1531–1558; graphresearch127–175; IoT deployment test1–95, payload build test1–90, register1–65, component88–173,606–693, payload35–75,100–160,176–210,358–375, processor25–125; decoder20–62; gateway port test1–47; graph-query52–124,219–247; go.mod1–8.
- NOT RUN: builds/tests/config validation/live runtime/Docker/HTTP/UDP; GraphQL operation schema resolution after zero path hits; full boot dependency closure; other live configs/harnesses; network/GitHub/OpenSpec issue enumeration (root owns claims); sister-repo reads; complete gopls references/call hierarchy.
- No repository/sister edits. Sole authorized write: this inventory file.

### Workflow retirement

All searches used `git grep -n -E`, adding `-i` where stated. Q1/Q2 were initial worktree orientation; Q3–Q13 explicitly searched ea22e6a4. Hit counts are matching lines. Legacy pages means the four enumerated above; history filtering means docs/adr, docs/proposals, and docs/operations/migration-*; filtering changed display only, not recorded totals.

- Q1 `processor/reactive|ReactiveWorkflow|reactive\.Workflow|WorkflowEngine|workflow engine|Workflow DSL|workflow DSL|workflow\.Manager|workflowmanager|NewWorkflow|RegisterWorkflow` over docs, pkg/context, processor/rule/docs: 75 worktree hits (repeat count recovered after initial display truncation); Q3 same query at base: 69.
- Q2 `(^#|reactive|workflow|deprecated|legacy|parallel|retired)` over concepts/14, attempted concepts/22-workflow-patterns and /27-workflow-execution, ROADMAP: 100. The two attempted names were absent; actual /22-context-construction and /27-frontier-harness-mapping were subsequently enumerated/read.
- Q4 `deprecated|parallel|legacy|retired|workflow` (-i), pkg/context/README.md and processor/rule/docs: 23.
- Q5 `reactive\.|workflow processor|[Dd]eprecated|[Pp]arallel.*(true|false)|"parallel"|WatchAll|WorkflowDefinition|StateManager|WorkflowTrigger` over docs/basics, advanced, concepts, ROADMAP, pkg/context, processor/rule/docs: 254; repeated with per-file counts and all non-legacy matches printed after initial truncation.
- Q6 `08-workflow-quickstart|09-workflow-configuration|10-reactive-workflows|23-parallel-agents` over docs, pkg/context, processor/rule/docs: 14.
- Q7 `for_each|fan.in|[Ff]an.out|MaxIterations|context|publish_agent|parallel` over rule/docs/actions, advanced/06, concepts/25, attempted openspec/specs/rule-action-contract/spec.md: 6; attempted spec absent, actual rule-engine spec subsequently enumerated/read.
- Q8 `workflow processor|Reactive Workflow Engine|trigger_workflow|WatchAll|[Pp]arallel:|"parallel"|\$\{steps\.` over docs, pkg/context, processor/rule/docs: 172; non-legacy/non-ADR/non-proposal matches printed.
- Q9 `reactive|workflow processor|trigger_workflow|workflow_trigger_payload|WithMaxIterations|WithStateBucket|WithStateFactory|PublishAsync|RuleContext|ExecutionState|GetExecutionState|ConditionFunc|ActionPublishAsync|WatchKV|OnJetStreamSubject` (-i), docs, pkg/context/README.md, processor/rule/docs: 523; 40 matches after legacy/history filters, all printed and classified.
- Q10 `for_each|max_iterations|publish_agent|[Ff]an.in|[Ff]an.out|Context|\$entity|\$state` (-i), rule-engine spec and processor/rule/{types,action,actions,agent_actions}.go: 153, first 125 printed. Matches were in actions.go and the spec; relevant source ranges then read.
- Q11 `for_each|for_each_var|completed|expected|array_contains|parallel` (-i), configs/rules/deep-research: 2. Read-only check of the explicitly excluded #486 remainder.
- Q12 `majority|consensus|first.success|partial.failure|duplicate.complet|idempoten|cancel|failed|complete` (-i), concepts/14, concepts/25, agentic-terminal-events spec, lifecycle spec: 62. No first-success/majority matches in that bounded scope.
- Q13 `WORKFLOW_EXECUTIONS|workflow\.execute|current_step|step_results|on_success|on_failure|\$\{steps\.|type.*parallel|trigger_workflow|workflow_trigger_payload` over docs, pkg/context/README.md, processor/rule/docs: 35; 11 non-legacy/non-history matches printed. One hydration_failures metric is an unrelated substring match.
- File enumeration: `rg --files docs pkg/context processor/rule/docs`: 423 current-worktree files; candidate names selected by reactive/workflow/roadmap/orchestration/22-/27-. Baseline `git ls-tree -r --name-only` of four retired paths: zero. Baseline spec-name enumeration located rule-engine, lifecycle, and agentic-terminal-events.
- Reads: architect contract fully; project Purpose/Product Boundary; complete non-fenced prose/headings of all four legacy pages (advanced/09 late code blocks not semantically audited); full concepts/22; relevant portions of concepts/14, /25, /27, ROADMAP, advanced/06, troubleshooting, context README, rule custom-rules/operations, and current source/spec counterparts. Fenced examples were located by literal searches and selected reads, not compiled.
- NOT RUN: any test, build, generator, application, Docker/NATS infrastructure, runtime or sister-repository validation. No new runtime API, config change, or target-state design produced.
