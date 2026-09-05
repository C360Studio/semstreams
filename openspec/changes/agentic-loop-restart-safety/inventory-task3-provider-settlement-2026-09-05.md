# Inventory: task 3 provider settlement
base: 78d5498649b09711eecfe77ba3196110ca00eab8

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:120` — `- [ ] 3.1 RED: add first-delivery, retained-response provider protection, pre-call replacement, post-return/pre-PubAck`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:128` — `3.2 Implement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:137` — `- [ ] 3.3 GREEN: prove by counter that a matching retained response and default unresolved redelivery invoke the`
- `processor/agentic-model/component.go:395` — `func(workCtx context.Context, _ natsclient.DeliveryAttempt, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-model/component.go:396` — `c.handleRequest(workCtx, data)`
- `processor/agentic-model/component.go:397` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:603` — `func (c *Component) handleRequest(ctx context.Context, data []byte) {`
- `processor/agentic-model/component.go:623` — `c.publishErrorResponse(ctx, req.RequestID, err.Error())`
- `processor/agentic-model/component.go:643` — `resp, err := c.executeRequest(ctx, client, req, endpoint, capability)`
- `processor/agentic-model/component.go:649` — `c.handleModelError(ctx, req, resp, err, duration)`
- `processor/agentic-model/component.go:654` — `c.handleModelSuccess(ctx, req, resp, duration)`
- `processor/agentic-model/component.go:756` — `c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)`
- `processor/agentic-model/component.go:796` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/config.go:18` — `type Config struct {`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `agentic/types.go:180` — `func (r AgentResponse) Validate() error {`
- `schemas/agentic-model.v1.json:853` — `"timeout": {`
- `processor/agentic-model/README.md:64` — `| Option | Type | Default | Description |`

## Spellings of the fact

- `natsclient/delivery_settlement.go:34` — `type DeliveryAttempt struct {`
- `natsclient/delivery_settlement.go:39` — `func (a DeliveryAttempt) Number() uint64 { return a.number }`
- `natsclient/delivery_settlement.go:42` — `func (a DeliveryAttempt) MetadataAvailable() bool { return a.number > 0 }`
- `natsclient/delivery_settlement.go:45` — `func (a DeliveryAttempt) IsRedelivery() bool { return a.number > 1 }`
- `natsclient/delivery_settlement.go:51` — `type DeliveryWork func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error)`
- `processor/agentic-model/component.go:390` — `policy, policyErr := natsclient.ValidateHeartbeatDeliveryPolicy(`
- `processor/agentic-model/component.go:414` — `handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-model/component.go:415` — `result, admitted := consumeAdmittedDelivery(msgCtx, msg, policy, admission)`
- `processor/agentic-model/component.go:1058` — `func (c *Component) publishResponse(ctx context.Context, resp agentic.AgentResponse) error {`
- `processor/agentic-model/component.go:1059` — `respMsg := message.NewBaseMessage(resp.Schema(), &resp, "agentic-model")`
- `processor/agentic-model/component.go:1065` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)`
- `processor/agentic-model/component.go:1069` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-model/component.go:1088` — `func (c *Component) publishErrorResponse(ctx context.Context, requestID string, errMsg string) {`
- `processor/agentic-model/component.go:1093` — `func (c *Component) publishErrorResponseWithTokens(ctx context.Context, requestID string, errMsg string, tokens agentic.TokenUsage) {`
- `processor/agentic-model/component.go:1094` — `resp := agentic.AgentResponse{`
- `processor/agentic-model/config.go:19` — `*component.PortConfig`
- `processor/agentic-model/config.go:21` — `Per-request LLM call timeout`
- `processor/agentic-model/config.go:22` — `Retry configuration`
- `processor/agentic-model/config.go:36` — `func (c *Config) Validate() error {`
- `processor/agentic-model/config.go:129` — `// DefaultConfig returns default configuration for agentic-model processor`
- `processor/agentic-model/config.go:133` — `Name: "agent.request", Config: component.JetStreamPort{`
- `processor/agentic-model/config.go:143` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-model/component.go:27` — `var agenticModelSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))`
- `processor/agentic-model/component.go:126` — `func resolveConfig(rawConfig json.RawMessage, method string) (Config, []component.Port, []component.Port, error) {`
- `processor/agentic-model/component.go:140` — `if err := config.Validate(); err != nil {`
- `agentic/types.go:170` — `RequestID    string`
- `agentic/types.go:171` — `Status       string`
- `agentic/types.go:174` — `Error        string`
- `agentic/types.go:190` — `func (r *AgentResponse) Schema() message.Type {`
- `agentic/types.go:195` — `func (r *AgentResponse) MarshalJSON() ([]byte, error) {`
- `agentic/types.go:201` — `func (r *AgentResponse) UnmarshalJSON(data []byte) error {`
- `agentic/payload_registry.go:38` — `{Domain: Domain, Category: CategoryResponse, Version: SchemaVersion, Description: "Agent model response", Factory: func() any { return &AgentResponse{} }, IndexingProfile: trace},`
- `processor/agentic-model/adapter.go:15` — `type ProviderAdapter interface {`
- `processor/agentic-model/adapter.go:49` — `func AdapterFor(provider string) ProviderAdapter {`
- `processor/agentic-model/adapter_generic.go:8` — `type GenericAdapter struct{}`
- `processor/agentic-model/adapter_gemini.go:26` — `type GeminiAdapter struct{}`
- `processor/agentic-model/adapter_ollama.go:26` — `type OllamaAdapter struct {`
- `processor/agentic-model/adapter_openai.go:8` — `type OpenAIAdapter struct{}`
- `processor/agentic-model/adapter_responses.go:18` — `type ResponsesAdapter interface {`
- `processor/agentic-model/adapter_responses.go:49` — `type OpenAIResponsesAdapter struct{}`
- `processor/agentic-model/client.go:27` — `type Client struct {`
- `processor/agentic-model/client.go:37` — `ProviderAdapter`
- `processor/agentic-model/client.go:38` — `responsesAdapter ResponsesAdapter`
- `processor/agentic-model/client.go:60` — `func NewClient(endpoint *model.EndpointConfig) (*Client, error) {`
- `processor/agentic-model/component.go:829` — `chain := c.modelRegistry.GetFallbackChain(req.Model)`
- `processor/agentic-model/component.go:851` — `if candidate := c.modelRegistry.GetEndpoint(req.Model); candidate != nil {`
- `processor/agentic-model/component.go:862` — `if defaultName := c.modelRegistry.GetDefault(); defaultName != "" {`
- `processor/agentic-model/component.go:900` — `client, err := NewClient(ep)`
- `processor/agentic-model/component.go:906` — `client.SetAdapter(AdapterFor(ep.Provider))`
- `processor/agentic-model/component.go:911` — `client.SetResponsesAdapter(ResponsesAdapterFor(ep.Provider))`
- `model/registry.go:392` — `type RegistryReader interface {`
- `model/registry.go:400` — `GetFallbackChain(capability string) []string`
- `model/registry.go:404` — `GetEndpoint(name string) *EndpointConfig`
- `model/registry.go:417` — `GetDefault() string`
- `model/registry.go:420` — `ListCapabilities() []string`
- `model/registry.go:423` — `ListEndpoints() []string`
- `model/registry.go:680` — `func (r *Registry) ListEndpoints() []string {`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:3` — `### Requirement: Model request settlement is bound to a durable response`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:31` — `Exact response lookup SHALL occur only after the model owner's local AGENT replay-admission gate succeeds.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:63` — `### Requirement: Provider commit-unknown behavior is explicit`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:73` — `SHALL be admitted only when every endpoint reachable`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:82` — `No shipped provider at checkpoint P declares this capability`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:149` — `### Requirement: Provider commit-unknown is machine-readable`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:203` — `### Requirement: Model response publication is durably at-least-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:209` — `The operation-specific exact committed-response read exists only at the provider-invocation boundary.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:293` — `## 9. AGENT admission, first-party publisher, and loop authority`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:296` — `- [ ] 9.2 RED: add model/dispatch/governance/loop tests for caller-local requirements, divergent configs, resolved`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:300` — `Implement one pure repo-internal`
- `processor/agentic-model/delivery_owner_test.go:49` — `// spec: agentic-model / Model request settlement is bound to a durable response`
- `processor/agentic-model/publication_semantics_integration_test.go:15` — `// spec: agentic-model / Model response publication is durably at-least-once`
- `processor/agentic-model/publication_semantics_integration_test.go:47` — `require.Equal(t, 2, count, "ordinary response publication may repeat with the same RequestID")`
- `schemas/agentic-model.v1.json:855` — `"description": "Per-request LLM call timeout.`
- `configs/agentic.json:419` — `"agentic-model": {`
- `configs/agentic.json:424` — `"timeout": "30s",`
- `configs/examples/research-graph-pipeline.json:278` — `"agentic-model": {`
- `processor/agentic-model/README.md:31` — `## Configuration`
- `docs/advanced/08-agentic-components.md:186` — `### 2. agentic-model - Model Endpoint Caller`
- `docs/advanced/11-jetstream-tuning.md:188` — `### agentic-model — LLM Calls`
- #759 — natsclient: establish semantic JetStream settlement as the restart-safety foundation
- #1145 — framework: declare and prove component restart behavior
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1147 — epic: make framework restart behavior explicit and provable
- #1155 — e2e(agentic): prove semantic-settlement quarantine and AgentRun redelivery across process replacement
- #1159 (draft) — fix(agentic-loop): preserve durable work across process restart
- #1156 (draft) — refactor(natsclient): add semantic delivery settlement

## Consumers

- `processor/agentic-model/component.go:396` — `c.handleRequest(workCtx, data)`
- `processor/agentic-model/component.go:623` — `c.publishErrorResponse(ctx, req.RequestID, err.Error())`
- `processor/agentic-model/component.go:796` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/component.go:1089` — `c.publishErrorResponseWithTokens(ctx, requestID, errMsg, agentic.TokenUsage{})`
- `processor/agentic-model/component.go:1101` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/publication_semantics_integration_test.go:28` — `require.NoError(t, c.publishResponse(ctx, response))`
- `processor/agentic-model/publication_semantics_integration_test.go:29` — `require.NoError(t, c.publishResponse(ctx, response))`
- `processor/agentic-model/component.go:32` — `config        Config`
- `processor/agentic-model/component.go:33` — `modelRegistry model.RegistryReader`
- `processor/agentic-model/component.go:40` — `decoder      *message.Decoder`
- `processor/agentic-model/component.go:41` — `natsClient   *natsclient.Client`
- `processor/agentic-model/component.go:127` — `defaults := DefaultConfig()`
- `processor/agentic-model/component.go:128` — `config := DefaultConfig()`
- `processor/agentic-model/component.go:155` — `config, inputPorts, outputPorts, err := resolveConfig(rawConfig, "NewComponentWithOptions")`
- `processor/agentic-model/component.go:189` — `deps.ModelRegistry`
- `processor/agentic-model/component.go:191` — `message.NewDecoder(deps.PayloadRegistry)`
- `processor/agentic-model/component.go:193` — `deps.NATSClient`
- `processor/agentic-model/client.go:440` — `func (c *Client) ChatCompletion(ctx context.Context, req agentic.AgentRequest) (agentic.AgentResponse, error) {`
- `processor/agentic-model/component.go:965` — `) (agentic.AgentResponse, error) {`
- `processor/agentic-loop/component.go:1510` — `func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, string, bool) {`
- `agentic/rule_fields.go:504` — `func (r *AgentResponse) RuleFields() map[string]any {`
- `message/base_message.go:195` — `if err := m.payload.Validate(); err != nil {`
- `component/validation.go:193` — `if err := validatable.Validate(); err != nil {`
- `internal/agentterminal/terminal.go:119` — `if err := base.Payload().Validate(); err != nil {`
- `payloadregistry/registry.go:431` — `schema := sp.Schema()`
- `model/registry_test.go:493` — `got := r.ListEndpoints()`
- `processor/agentic-loop/graph_writer.go:245` — `for _, name := range w.modelRegistry.ListEndpoints() {`
- `processor/agentic-model/component.go:837` — `candidate := c.modelRegistry.GetEndpoint(name)`
- `processor/agentic-model/component.go:862` — `if defaultName := c.modelRegistry.GetDefault(); defaultName != "" {`
- `processor/agentic-model/component.go:900` — `client, err := NewClient(ep)`
- `processor/agentic-dispatch/intent_classifier.go:106` — `client, err := agenticmodel.NewClient(ep)`
- `cmd/detonate-injections/main.go:116` — `client, err := agenticmodel.NewClient(endpoint)`

## Problem shape

- `processor/agentic-dispatch/task_recovery.go:34` — `type retainedTaskEvidenceReader interface {`
- `processor/agentic-dispatch/task_recovery.go:35` — `ReadRetainedTask(context.Context, string, string) ([]byte, bool, error)`
- `processor/agentic-dispatch/task_recovery.go:42` — `func (r natsRetainedTaskEvidenceReader) ReadRetainedTask(`
- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-dispatch/task_recovery.go:52` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-dispatch/task_recovery.go:58` — `return append([]byte(nil), raw.Data...), true, nil`
- `processor/agentic-dispatch/task_recovery.go:89` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:94` — `if err := validateRetainedDispatchTask(retained, msg, taskID, msg.ReplyTo); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:95` — `return preparedDispatchTask{}, vacantDispatchTaskSlot{}, false, errs.WrapFatal(`

## Surface absences

- `processor/agentic-model/component.go:329` — `// Wait for stream to be available`
- `processor/agentic-model/component.go:334` — `if err := waitForStream(ctx, streamName); err != nil {`
- `processor/agentic-model/component.go:620` — `client, endpoint, capability, endpointName, err := c.getClientForRequest(req)`
- `processor/agentic-model/adapter.go:15` — `type ProviderAdapter interface {`
- `processor/agentic-model/adapter_responses.go:18` — `type ResponsesAdapter interface {`
- `processor/agentic-model/config.go:18` — `type Config struct {`
- `agentic/types.go:169` — `type AgentResponse struct {`

Zero-hit searches under `## Searches` cover the named reconciliation interface/result, ambiguity config/value,
failure-kind field/type/value, model-local exact-response reads, model-local replay-admission implementation, direct
get spelling, and started-marker spelling.

## Adopter seam inventory

A model operator reaches this surface only through agentic-model configuration and startup/readiness. The current
configuration owner is `processor/agentic-model/config.go:18` — `type Config struct {`; schema generation is owned by
`processor/agentic-model/component.go:27` — `var agenticModelSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))`.

The active target state makes the provider ambiguity policy the operator-visible choice and defaults omission to the
conservative policy: `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:65` — `Agentic-model `Config` SHALL add exact field`, and
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:69` — `SHALL be `fail_commit_unknown`, `at_least_once`, and `provider_reconcile`. Omission or the empty string SHALL default`.

Invalid policy is refused before consumer allocation:
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:144` — `#### Scenario: Provider ambiguity policy is unknown`.

A selected reconciliation policy is refused before request-consumer allocation when a reachable endpoint does not
support the private operation:
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:132` — `#### Scenario: Provider reconciliation is unsupported at setup`.

Replay-bound observation remains framework-owned and caller-local. The operator does not supply or predict retained
response availability:
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:31` — `Exact response lookup SHALL occur only after the model owner's local AGENT replay-admission gate succeeds.`

Reconciliation creates no exported provider-author surface. Its target interface is package-private:
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:74` — `(direct endpoint, capability chain, and default) supplies the package-private `providerCommitReconciler` capability.`

Formatting adapters do not imply reconciliation, so existing provider-adapter authors acquire no hidden contract:
`openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:83` — `is a setup refusal until an independently tested backend implements it. Formatting-only `ProviderAdapter` and`.

The only direct non-component `NewClient` consumers at this checkpoint are
`processor/agentic-dispatch/intent_classifier.go:106` — `client, err := agenticmodel.NewClient(ep)` and
`cmd/detonate-injections/main.go:116` — `client, err := agenticmodel.NewClient(endpoint)`; neither allocates the
agentic-model request consumer or owns its replay-admission boundary.

## Searches

- `git rev-parse HEAD` → `78d5498649b09711eecfe77ba3196110ca00eab8`.
- `git grep -n '^## (Purpose|Product Boundary)' -- openspec/project.md` → 2.
- `gopls workspace_symbol -matcher=fuzzy DeliveryAttempt` → 18.
- `gopls workspace_symbol -matcher=fuzzy handleRequest` → 38.
- `gopls references processor/agentic-model/component.go:603:21` → 1.
- `gopls call_hierarchy processor/agentic-model/component.go:603:21` → 1 caller, then `gopls` panicked during outgoing-call expansion.
- `gopls references processor/agentic-model/component.go:1058:21` → 5.
- `gopls references processor/agentic-model/component.go:1088:21` → 1.
- `gopls workspace_symbol -matcher=fuzzy ProviderAdapter` → 11.
- `gopls workspace_symbol -matcher=fuzzy ResponsesAdapter` → 22.
- `gopls workspace_symbol -matcher=fuzzy AgentResponse` → 100.
- `gopls workspace_symbol -matcher=fuzzy RegistryReader` → 18.
- `gopls implementation processor/agentic-model/adapter.go:15:6` → 4.
- `gopls implementation processor/agentic-model/adapter_responses.go:18:6` → 1.
- `gopls implementation model/registry.go:392:6` → 3.
- `gopls references model/registry.go:423:2` → 2.
- `gopls references processor/agentic-model/client.go:45:6` → 0; invalid position (`column is beyond end of line`).
- `gopls references processor/agentic-model/client.go:60:6` → 46.
- `gopls references agentic/types.go:180:24` → 8.
- `gopls references agentic/types.go:190:25` → 20.
- `gopls references agentic/types.go:195:25` → 1.
- `gopls references processor/agentic-model/adapter.go:15:6` → 9.
- `gopls references processor/agentic-model/adapter_responses.go:18:6` → 5.
- `gopls references processor/agentic-model/config.go:18:6` → 19.
- `gopls references agentic/types.go:169:6` → 100+; output included package-wide construction and consumption sites.
- `gopls references model/registry.go:420:2` → 1.
- `gopls references model/registry.go:417:2` → 8.
- `gopls references model/registry.go:400:2` → 5.
- `git grep -n -e ProviderAmbiguityPolicy -e provider_ambiguity_policy -e provider-ambiguity-policy -e PROVIDER_AMBIGUITY_POLICY -- processor agentic model component schemas config docs openspec` → 10.
- `git grep -n -e fail_commit_unknown -e at_least_once -e provider_reconcile -e provider_commit_unknown -e provider_reconcile_unsupported -- processor agentic model component schemas config docs openspec` → 46.
- `git grep -n <term> -- '*.go' ':!openspec/**' ':!docs/**' ':!vendor/**'` for `ReconcileProviderCommit`, `providerCommitReconciler`, `providerReconcileResult`, `failure_kind`, `AgentResponseFailureKind`, `provider_commit_unknown`, `ProviderAmbiguityPolicy`, `provider_ambiguity_policy`, `fail_commit_unknown`, `at_least_once`, `provider_reconcile`, and `provider_reconcile_unsupported` → 0 for each.
- `git grep -n <term> -- processor/agentic-model agentic model natsclient schemas docs openspec/changes/agentic-loop-restart-safety openspec/specs` → `handleRequest` 12; `publishResponse` 18; `publishErrorResponse` 9; `setupConsumer` 10; `DeliveryAttempt` 69; `ConsumeWithHeartbeat` 78; `GetLastMsgForSubject` 9; `GetMsg` 6; `AgentResponse` 182; `RegisterPayloads` 90; `ProviderAdapter` 21; `ResponsesAdapter` 69; `NewClient` 301; `ListEndpoints` 9; `DefaultEndpoint` 0; `Capability` 262; `replay-admission` 3; `replay admission` 5; `agentstreamadmission` 9; `ObserveAndValidate` 8.
- `git grep -n -F <term> -- processor/agentic-model agentic model schemas config docs/operations docs/advanced openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/tasks.md` → `GetLastMsgForSubject` 0; `GetMsg` 0; `DirectGet` 0; `RequestID` 206; `agent.response.` 40; `CategoryResponse` 3; `Decode` 147; `Payload()` 44; `StreamName: "AGENT"` 19; `Subjects: []string{"agent.response` 9; `consumer_replay` 0.
- `git grep -n -e agentic-model -e agentic_model -- schemas config processor/agentic-model docs/advanced docs/operations openspec/specs` → 187.
- `git grep -n -F <term> -- '*.json' '*.yaml' '*.yml' '*.toml' docs processor openspec` → `"name": "agentic-model"` 12; `"name":"agentic-model"` 0; `type: agentic-model` 0; `name: agentic-model` 0; `agentic-model:` 8.
- `git grep -n -e 'func RegisterPayloads' -e AgentResponse -- agentic/payload_registry.go payloadbuiltins processor/agentic-model agentic/types.go agentic/types_test.go test/contract/message_contract_test.go` → 126.
- `git grep -n -e GetLastMsgForSubject -e ObserveAndValidate -e 'unsupported before' -e 'before consumer allocation' -e 'ListEndpoints()' -- '*.go' 'openspec/specs/**' 'openspec/changes/**/specs/**' 'docs/adr/**' 'docs/operations/migration-*.md'` → 27.
- `git grep -n -F <term> -- processor/agentic-model agentic model openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md docs/adr docs/operations/migration-*.md` → `fingerprint` 14; `request fingerprint` 0; `response fingerprint` 0; `started marker` 4; `started_marker` 0; `Nats-Msg-Id` 11; `NatsMsgId` 0.
- `git grep -n -F <term> -- '*.go' ':(exclude)**/*_test.go'` → `agentstreamadmission` 0; `ObserveAndValidate` 0; `ReconcileProviderCommit` 0; `providerCommitReconciler` 0; `providerReconcileResult` 0; `GetLastMsgForSubject` 9; `DirectGet` 0.
- `git grep -n '^func NewClient' -- processor/agentic-model` → 1.
- `gh issue list --repo C360Studio/semstreams --search 'provider settlement' --state open --limit 50 --json number,title` → 5.
- `gh issue list --repo C360Studio/semstreams --search 'provider commit unknown' --state open --limit 50 --json number,title` → 3.
- `gh issue list --repo C360Studio/semstreams --search 'agentic-model restart' --state open --limit 50 --json number,title` → 8.
- `openspec list` → 2 active changes.
- `gh pr list --repo C360Studio/semstreams --state open --limit 100 --json number,title,body,isDraft` → 5; 4 draft.
