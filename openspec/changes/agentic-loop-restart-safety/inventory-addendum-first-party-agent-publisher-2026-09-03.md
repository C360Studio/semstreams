# Inventory addendum: first-party `publish_agent` publisher admission
base: 09ba38b1de5e7200e72281c8e4b8941d81be1da2

## Checkpoint

- `openspec/project.md:5` — `SemStreams is the **semantic graph substrate and framework** for the C360 `sem*``
- `openspec/project.md:14` — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- `processor/rule/actions.go:49` — `// ActionTypePublishAgent triggers an agentic loop by publishing a TaskMessage`
- `processor/rule/actions.go:50` — `ActionTypePublishAgent = "publish_agent"`

This addendum enumerates only the framework-owned rule `publish_agent` path that produces `agent.task.*` work. It does
not replace the accepted durable-input census and does not enumerate unrelated publishers.

## Claimed gap

### Declaration, action shape, and registration

- `processor/rule/actions.go:89` — `type Action struct {`
- `processor/rule/actions.go:90` — `// Type specifies the action type (publish, add_triple, remove_triple, update_triple, publish_agent)`
- `processor/rule/actions.go:91` — `Type string `json:"type"``
- `processor/rule/actions.go:94` — `//   - publish / publish_agent: the NATS subject the message is sent to.`
- `processor/rule/actions.go:138` — `Properties map[string]any `json:"properties,omitempty"``
- `processor/rule/actions.go:141` — `Role string `json:"role,omitempty"``
- `processor/rule/actions.go:144` — `Model string `json:"model,omitempty"``
- `processor/rule/actions.go:148` — `Prompt string `json:"prompt,omitempty"``
- `processor/rule/actions.go:160` — `Tools []string `json:"tools,omitempty"``
- `processor/rule/actions.go:368` — `LoopMaxIterations string `json:"loop_max_iterations,omitempty" description:"Iteration budget for the SPAWNED LOOP (agentic-loop's per-iteration cap on the agent this action spawns) — distinct from the action-level firing cap 'max_iterations' above, which bounds how many times this action itself fires. Supports variable substitution (literal or $entity.triple.* reference); must resolve to a positive integer or the action fails."``
- `processor/rule/actions.go:479` — `// Publisher handles publishing messages to NATS subjects.`
- `processor/rule/actions.go:481` — `type Publisher interface {`
- `processor/rule/actions.go:485` — `Publish(ctx context.Context, subject string, data []byte) error`
- `processor/rule/actions.go:848` — `func (e *ActionExecutor) Execute(ctx context.Context, action Action, ec *ExecutionContext) error {`
- `processor/rule/actions.go:861` — `case ActionTypePublishAgent:`
- `processor/rule/actions.go:862` — `return e.executePublishAgent(ctx, action, ec)`

The generated rule schema exposes `type` as an unconstrained string and the action fields as properties; the literal
`publish_agent` does not occur in that generated schema.

- `schemas/rule-processor.v1.json:105` — `"model": {`
- `schemas/rule-processor.v1.json:133` — `"prompt": {`
- `schemas/rule-processor.v1.json:171` — `"role": {`
- `schemas/rule-processor.v1.json:190` — `"subject": {`
- `schemas/rule-processor.v1.json:208` — `"tools": {`
- `schemas/rule-processor.v1.json:219` — `"type": {`
- `schemas/rule-processor.v1.json:220` — `"type": "string"`

### Action execution, payload, and failure surfaces

- `processor/rule/actions.go:1616` — `func (e *ActionExecutor) executePublishAgent(ctx context.Context, action Action, ec *ExecutionContext) error {`
- `processor/rule/actions.go:1619` — `if action.Subject == "" {`
- `processor/rule/actions.go:1620` — `return errors.New("subject is required for publish_agent action")`
- `processor/rule/actions.go:1622` — `if action.Role == "" {`
- `processor/rule/actions.go:1625` — `if action.Model == "" {`
- `processor/rule/actions.go:1628` — `if action.Prompt == "" {`
- `processor/rule/actions.go:1690` — `func (e *ActionExecutor) publishAgentOnce(ctx context.Context, action Action, ec *ExecutionContext, iterVarName, iterVarValue string) error {`
- `processor/rule/actions.go:1700` — `subject := ec.SubstituteVariablesWithIterVar(ctx, action.Subject, iterVarName, iterVarValue)`
- `processor/rule/actions.go:1701` — `if targetsReservedUserResponseSubject(subject) {`
- `processor/rule/actions.go:1713` — `task := agentic.TaskMessage{`
- `processor/rule/actions.go:1714` — `TaskID:       taskID,`
- `processor/rule/actions.go:1715` — `Role:         role,`
- `processor/rule/actions.go:1716` — `Model:        action.Model,`
- `processor/rule/actions.go:1717` — `Prompt:       prompt,`
- `processor/rule/actions.go:1885` — `if err := task.Validate(); err != nil {`
- `agentic/user_types.go:406` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:530` — `func (t *TaskMessage) Schema() message.Type {`
- `agentic/user_types.go:531` — `return message.Type{Domain: Domain, Category: CategoryTask, Version: SchemaVersion}`
- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`
- `payloadbuiltins/register.go:45` — `track(agentic.RegisterPayloads(reg))`
- `processor/rule/actions.go:1956` — `baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")`
- `processor/rule/actions.go:1957` — `data, err := json.Marshal(baseMsg)`
- `processor/rule/actions.go:1962` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/rule/actions.go:1963` — `return fmt.Errorf("publish agent task to %s: %w", subject, err)`
- `processor/rule/actions.go:1973` — `} else if e.logger != nil {`
- `processor/rule/actions.go:1974` — `e.logger.Debug("Agent task not published (no publisher configured)",`
- `processor/rule/actions.go:1991` — `if published && e.tripleMutator != nil && foreignFiring {`
- `processor/rule/actions.go:2002` — `if _, err := e.tripleMutator.AddTriple(ctx, ec.RuleID(), spawnedTriple); err != nil {`
- `processor/rule/actions.go:2003` — `// The agent task already published; returning an error would cause`
- `processor/rule/actions.go:2018` — `return nil`

Required action fields are checked when the action executes. The substituted `TaskMessage` is validated and wrapped
in its registered payload envelope before bytes cross NATS. A configured publisher error is returned. An absent
publisher is a debug-log/no-error path. A post-publication `rule.task.spawned` write failure is logged and not returned
because the task has already been sent.

### `TaskMessage` payload and graph capability boundary

`TaskMessage` is a registered `message.Payload`: it supplies `Schema`, validation, and JSON marshal/unmarshal methods,
and its registry entry uses the control indexing profile. The scoped production method search found no `EntityID` or
`Triples` method on `TaskMessage`; it therefore does not implement the separate `graph.Graphable` interface.

- `agentic/user_types.go:312` — `// TaskMessage represents a task to be executed by an agentic loop`
- `agentic/user_types.go:313` — `type TaskMessage struct {`
- `agentic/user_types.go:314` — `LoopID string `json:"loop_id,omitempty"` // loop to continue, or empty for new`
- `agentic/user_types.go:315` — `TaskID string `json:"task_id"``
- `agentic/user_types.go:318` — `Prompt string `json:"prompt"``
- `agentic/user_types.go:343` — `// The publish_agent rule action exposes this as loop_max_iterations`
- `agentic/user_types.go:370` — `Tools []ToolDefinition `json:"tools"``
- `agentic/user_types.go:377` — `Metadata map[string]any `json:"metadata,omitempty"``
- `agentic/user_types.go:390` — `ResponseFormat *ResponseFormat `json:"response_format,omitempty"``
- `agentic/user_types.go:405` — `// Validate checks if the TaskMessage is valid`
- `agentic/user_types.go:406` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:432` — `if err := t.validateLoopTokens(); err != nil {`
- `agentic/user_types.go:435` — `if raw, ok := t.Metadata[MetadataKeyRelatedLoops]; ok {`
- `agentic/user_types.go:529` — `// Schema implements message.Payload`
- `agentic/user_types.go:530` — `func (t *TaskMessage) Schema() message.Type {`
- `agentic/user_types.go:535` — `func (t *TaskMessage) MarshalJSON() ([]byte, error) {`
- `agentic/user_types.go:540` — `// UnmarshalJSON implements json.Unmarshaler`
- `message/payload.go:50` — `type Payload interface {`
- `message/payload.go:53` — `Schema() Type`
- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`
- `graph/graphable.go:53` — `// Graphable provides entity identification and semantic triples`
- `graph/graphable.go:54` — `type Graphable interface {`
- `graph/graphable.go:56` — `EntityID() string`
- `graph/graphable.go:59` — `Triples() []message.Triple`

### Publication path and current port classification

- `processor/rule/publisher.go:26` — `// actionPublisher implements the Publisher interface for ActionExecutor.`
- `processor/rule/publisher.go:34` — `func newActionPublisher(processor *Processor) *actionPublisher {`
- `processor/rule/publisher.go:40` — `func (p *actionPublisher) Publish(ctx context.Context, subject string, data []byte) error {`
- `processor/rule/publisher.go:46` — `if p.processor.isJetStreamPortBySubject(subject) {`
- `processor/rule/publisher.go:47` — `publishErr = p.processor.natsClient.PublishToStream(ctx, subject, data)`
- `processor/rule/publisher.go:49` — `publishErr = p.processor.natsClient.Publish(ctx, subject, data)`
- `processor/rule/publisher.go:53` — `return errs.WrapTransient(publishErr, "actionPublisher", "Publish", fmt.Sprintf("publish to %s", subject))`
- `processor/rule/publisher.go:65` — `// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream`
- `processor/rule/publisher.go:66` — `func (rp *Processor) isJetStreamPortBySubject(subject string) bool {`
- `processor/rule/publisher.go:72` — `if subjects := facts.NATSSubjects(); len(subjects) == 1 && subjects[0] == subject {`
- `processor/rule/publisher.go:73` — `return facts.Kind() == component.PortKindJetStream`
- `processor/rule/publisher.go:76` — `return false`
- `natsclient/client.go:858` — `func (m *Client) Publish(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:875` — `return conn.PublishMsg(msg)`
- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:943` — `return m.publishToStream(ctx, subject, data, "")`

The repository has six enabled rule-processor instances whose own output array declares `agent.task.*`. Each declares
that output as JetStream/AGENT, and each configuration also explicitly declares stream `AGENT` with subject
`agent.>`. Four of those configurations statically load `publish_agent` definitions; CRUD-tools-test and agentic
declare the output but name no `rules_files`. For the four configurations with statically loaded actions,
`isJetStreamPortBySubject` compares each resolved concrete action subject to the wildcard declaration by exact string
equality, so those calls take its core-NATS branch. A future concrete matching action loaded into either of the two
declaration-only configurations would make the same comparison and also take core NATS. The explicit AGENT stream
still covers those subjects under the composition interpreter recorded below.

| Rule configuration | Rule-processor output subject and port name | Static `rules_files` and loaded `publish_agent` definitions |
|---|---|---|
| deep research | `configs/flows/deep-research.json:507`, port `agent.task` | `deep-research-rules`; loads 01, 03, 04, 05, 06, and 07 from the deep-research pack |
| deep research test | `configs/flows/deep-research-test.json:414`, port `agent.task` | `deep-research-test-rules`; loads the same six definitions |
| CRUD tools test | `configs/flows/crud-tools-test.json:409`, port `agent.task` | `crud-tools-test-rules`; no `rules_files` field, therefore no statically loaded `publish_agent` definition |
| graph research example | `configs/examples/research-graph-pipeline.json:677`, port `agent.task` | `research-graph-example-rules`; loads research-graph `05-continuation.json` |
| agentic | `configs/agentic.json:232`, port `agent_task` at line 236 | `agentic-rules`; no `rules_files` field, therefore no statically loaded `publish_agent` definition |
| graph research e2e | `configs/research-graph-e2e.json:677`, port `agent.task` | `research-graph-e2e-rules`; loads research-graph `05-continuation.json` |

- `configs/flows/deep-research.json:507` — `"agent.task.*"`
- `configs/flows/deep-research-test.json:414` — `"agent.task.*"`
- `configs/flows/crud-tools-test.json:409` — `"agent.task.*"`
- `configs/examples/research-graph-pipeline.json:677` — `"agent.task.*"`
- `configs/agentic.json:232` — `"agent.task.*"`
- `configs/agentic.json:236` — `"name": "agent_task"`
- `configs/research-graph-e2e.json:677` — `"agent.task.*"`
- `configs/flows/deep-research.json:60` — `"AGENT": {`
- `configs/flows/deep-research.json:62` — `"agent.>"`
- `configs/flows/deep-research-test.json:15` — `"AGENT": {`
- `configs/flows/crud-tools-test.json:15` — `"AGENT": {`
- `configs/examples/research-graph-pipeline.json:82` — `"AGENT": {`
- `configs/agentic.json:15` — `"AGENT": {`
- `configs/research-graph-e2e.json:82` — `"AGENT": {`

### First-party rule definitions and pack wiring

The repository contains eleven first-party `publish_agent` actions:

- `configs/rules/agentic-workflow/architect-editor.json:22` — `"type": "publish_agent",`
- `configs/rules/agentic-workflow/architect-editor.json:23` — `"subject": "agent.task.$entity.task_id.editor",`
- `configs/rules/cron/governance-example.json:11` — `"type": "publish_agent",`
- `configs/rules/cron/governance-example.json:12` — `"subject": "agent.task.governance.kill_switch_sweep.$schedule.id",`
- `configs/rules/deep-research/01-spawn-researcher.json:23` — `"type": "publish_agent",`
- `configs/rules/deep-research/01-spawn-researcher.json:24` — `"subject": "agent.task.research",`
- `configs/rules/deep-research/03-fan-out-subtopics.json:23` — `"type": "publish_agent",`
- `configs/rules/deep-research/03-fan-out-subtopics.json:24` — `"subject": "agent.task.subtopic",`
- `configs/rules/deep-research/04-synthesize-evidence.json:24` — `"type": "publish_agent",`
- `configs/rules/deep-research/04-synthesize-evidence.json:25` — `"subject": "agent.task.synthesis",`
- `configs/rules/deep-research/05-retry-insufficient.json:24` — `"type": "publish_agent",`
- `configs/rules/deep-research/05-retry-insufficient.json:25` — `"subject": "agent.task.retry",`
- `configs/rules/deep-research/06-timeout-partial.json:29` — `"type": "publish_agent",`
- `configs/rules/deep-research/06-timeout-partial.json:30` — `"subject": "agent.task.partial-synthesis",`
- `configs/rules/deep-research/07-spawn-coordinator.json:24` — `"type": "publish_agent",`
- `configs/rules/deep-research/07-spawn-coordinator.json:25` — `"subject": "agent.task.research-coordinator",`
- `configs/rules/example-fan-out/01-fan-out-subtopics.json:23` — `"type": "publish_agent",`
- `configs/rules/example-fan-out/01-fan-out-subtopics.json:24` — `"subject": "agent.task.investigator",`
- `configs/rules/example-fan-out/03-synthesize-when-all-complete.json:24` — `"type": "publish_agent",`
- `configs/rules/example-fan-out/03-synthesize-when-all-complete.json:25` — `"subject": "agent.task.synthesis",`
- `configs/rules/research-graph/05-continuation.json:23` — `"type": "publish_agent",`
- `configs/rules/research-graph/05-continuation.json:24` — `"subject": "agent.task.research_continuation",`

The four configurations with static rule files name the loaded packs:

- `configs/flows/deep-research.json:470` — `"pack_id": "deep-research-rules",`
- `configs/flows/deep-research.json:472` — `"/app/configs/rules/deep-research/01-spawn-researcher.json",`
- `configs/flows/deep-research.json:478` — `"/app/configs/rules/deep-research/07-spawn-coordinator.json"`
- `configs/flows/deep-research-test.json:377` — `"pack_id": "deep-research-test-rules",`
- `configs/flows/deep-research-test.json:379` — `"/app/configs/rules/deep-research/01-spawn-researcher.json",`
- `configs/flows/deep-research-test.json:385` — `"/app/configs/rules/deep-research/07-spawn-coordinator.json"`
- `configs/examples/research-graph-pipeline.json:640` — `"pack_id": "research-graph-example-rules",`
- `configs/examples/research-graph-pipeline.json:642` — `"/app/configs/rules/research-graph/00-kickoff-classify.json",`
- `configs/examples/research-graph-pipeline.json:647` — `"/app/configs/rules/research-graph/05-continuation.json"`
- `configs/research-graph-e2e.json:640` — `"pack_id": "research-graph-e2e-rules",`
- `configs/research-graph-e2e.json:647` — `"/app/configs/rules/research-graph/05-continuation.json"`

### Existing subject/admission owners at the collision seam

| Existing owner | Exact knowledge | Gap at this surface |
|---|---|---|
| `component.ResolveSubject` | Selects one uniquely named output, reads its first canonical NATS subject, and replaces a trailing wildcard with the supplied suffix. | `publish_agent` does not call it; the action substitutes its free-form subject directly. |
| dispatch channel + HTTP producers | Both use the shared TaskMessage builder, `ResolveSubject(..., "agent.task", taskID)`, and `PublishToStream`. | These same-class producers do not use `actionPublisher`'s exact-subject classifier. |
| graph-research capability validation | Requires canonical rule 05 to carry type `publish_agent`, the `agent.task.research_continuation` prefix, and `read_loop_result`. | This validator is capability-specific and does not validate every `publish_agent` rule or its configured output-port relationship. |
| flowgraph/composition | `SubjectCovers` understands NATS wildcards; composition treats a named explicit stream whose filter covers the input as fed even when publishers use core NATS. | This is whole-composition stream/connectivity judgment, not action-specific definition admission. |
| component-manager boot | Runs the shared composition analysis over admitted declarations plus explicit streams and refuses an error result before sealing. | It sees declared ports and streams, not a substituted future `Action.Subject`. |

- `component/ports.go:11` — `// ResolveSubject returns the configured NATS subject for one uniquely named`
- `component/ports.go:13` — `func ResolveSubject(ports []PortDefinition, portName, suffix string) (string, error) {`
- `component/ports.go:38` — `return appendSubjectSuffix(subject, suffix), nil`
- `processor/agentic-dispatch/component.go:1005` — `task := c.buildTaskMessage(ctx, msg, loopID, taskID)`
- `processor/agentic-dispatch/component.go:1021` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)`
- `processor/agentic-dispatch/component.go:1047` — `if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {`
- `processor/agentic-dispatch/http.go:347` — `task := c.buildTaskMessage(ctx, msg, loopID, taskID)`
- `processor/agentic-dispatch/http.go:362` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)`
- `processor/agentic-dispatch/http.go:384` — `if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {`
- `frameworkcapabilities/graphresearch/register.go:119` — `"05-continuation.json": {`
- `frameworkcapabilities/graphresearch/register.go:123` — `actions:         []ruleActionSignature{{typeName: "publish_agent", subjectPrefix: "agent.task.research_continuation", tool: "read_loop_result"}},`
- `frameworkcapabilities/graphresearch/register.go:169` — `func ValidateConfig(cfg *config.Config) error {`
- `frameworkcapabilities/graphresearch/register.go:230` — `if err := validateRulePack(ruleFiles); err != nil {`
- `frameworkcapabilities/graphresearch/register.go:432` — `if action.Type != required.typeName || !strings.HasPrefix(action.Subject, required.subjectPrefix) {`
- `component/flowgraph/flowgraph.go:381` — `// SubjectCovers reports whether filter COVERS pattern: every concrete subject`
- `component/flowgraph/flowgraph.go:389` — `func SubjectCovers(filter, pattern string) bool {`
- `composition/analyze.go:13` — `// Analyze is the graph-level half of composition validation: it connects the`
- `composition/analyze.go:20` — `// from this block; a JetStream subscriber whose subjects an explicit stream`
- `composition/analyze.go:21` — `// covers is fed even when its publishers use core NATS, so it is not a`
- `composition/analyze.go:82` — `if explicitStreamCovers(streams, streamNames[portKey{warning.SubscriberComp, warning.SubscriberPort}], warning.Subjects) {`
- `service/component_manager.go:377` — `// ADR-100 P5: validate the admitted composition at the real boundary with`
- `service/component_manager.go:380` — `if err := cm.analyzeBootComposition(); err != nil {`
- `service/component_manager.go:381` — `return err`

## Consumers

The rule processor publishes the registered `TaskMessage`; the agentic-loop owns the required `agent.task.*` input on
the AGENT stream.

- `processor/agentic-loop/config.go:396` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/component.go:892` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:1090` — `func consumeLongRunningInput(`
- `processor/agentic-loop/component.go:1148` — `func (c *Component) taskInputHandler(workTimeout time.Duration) inputHandler {`
- `processor/agentic-loop/component.go:1152` — `err := c.handleTaskMessage(workCtx, data)`
- `processor/agentic-loop/component.go:1161` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1168` — `task, ok := baseMsg.Payload().(*agentic.TaskMessage)`
- `processor/agentic-loop/component.go:1311` — `if err := task.Validate(); err != nil {`
- `configs/agentic.json:323` — `"agent.task.*"`
- `configs/agentic.json:327` — `"name": "agent.task",`

The governance component also declares `agent.task.*` as a required validation input, while first-party flows include
both the ordinary task family and a separate validated family.

- `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `configs/flows/deep-research.json:422` — `"agent.task.validated.*"`
- `configs/flows/deep-research.json:425` — `"name": "agent.task.validated",`

## Lifecycle, context, and settlement interaction

`publish_agent` receives the evaluator operation context and passes that same context through envelope construction,
publication, and the optional spawned-triple write. The evaluator counts the action before execution, logs and counts
non-deny errors without returning them, then persists match/action state after `runActions` returns.

- `processor/rule/stateful_evaluator.go:234` — `e.runActions(ctx, ev.Rule, ec, actions, ev.Entity, stateFields, expression.MessageFields(ev.MessageData), ev.EntityID, ev.RelatedID)`
- `processor/rule/stateful_evaluator.go:247` — `persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), statePersistTimeout)`
- `processor/rule/stateful_evaluator.go:249` — `if err := e.stateTracker.Set(persistCtx, *matchState); err != nil {`
- `processor/rule/stateful_evaluator.go:368` — `func (e *StatefulEvaluator) runActions(`
- `processor/rule/stateful_evaluator.go:403` — `// state lives on ec.State.ActionIterations and is persisted`
- `processor/rule/stateful_evaluator.go:423` — `ec.State.ActionIterations[actionID] = fired + 1`
- `processor/rule/stateful_evaluator.go:426` — `if err := e.actionExecutor.Execute(ctx, action, ec); err != nil {`
- `processor/rule/stateful_evaluator.go:437` — `e.logger.Error("Failed to execute action",`
- `processor/rule/stateful_evaluator.go:448` — `e.metrics.actionFailuresTotal.WithLabelValues(action.Type).Inc()`

The statically loaded first-party actions in four configurations select core NATS, whose call returns from
`PublishMsg`. The two other configurations expose the same classifier surface without a statically loaded
`publish_agent` call. The durable-input `agent.task.*` consumer and its settlement owner are downstream of either send
path.

## Tests and fixtures

- `processor/rule/actions_test.go:2205` — `// T051: Test PublishAgent without publisher (no-op)`
- `processor/rule/actions_test.go:2225` — `// T052: Test PublishAgent error handling`
- `processor/rule/actions_test.go:2247` — `// T053: Test ActionTypePublishAgent constant`
- `processor/rule/actions_test.go:2250` — `assert.Equal(t, "publish_agent", ActionTypePublishAgent)`
- `processor/rule/actions_test.go:3993` — `func TestAction_PublishAgent_CarriesNoChannelFields(t *testing.T) {`
- `processor/rule/action_failure_metrics_test.go:77` — `if got := testutil.ToFloat64(metrics.actionFailuresTotal.WithLabelValues(ActionTypePublishAgent)); got != 1 {`
- `processor/rule/payload_projection_integration_test.go:36` — `//	  -> ActionExecutor substitution -> actionPublisher -> real NATS output`
- `processor/rule/research_graph_pipeline_integration_test.go:409` — `// The subject for publish_agent is the configured agent.task subject`
- `processor/rule/research_graph_pipeline_integration_test.go:412` — `assert.Equal(t, "agent.task.research_continuation", msg.subject)`
- `test/e2e/scenarios/research-graph/scenario.go:22` — `//  7. verify-r6-continuation — confirm a continuation agent.task fired back to the parent role`
- `test/e2e/scenarios/research-graph/scenario.go:831` — `// publish_agent fires but the parent's read_loop_result returns`

The focused test search found no test declaration naming `isJetStreamPortBySubject`. It found publish-agent unit,
metrics, run-scope, projection, pipeline-integration, and research-graph e2e coverage.

## Docs, specifications, ADRs, and claims

- `docs/adr/031-time-trigger-primitive.md:69` — `agent.task.\* / publish-action plumbing.`
- `docs/adr/045-graph-search-rule-chain.md:405` — `- Continuation back to parent → existing rule + `publish_agent``
- `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md:123` — `- `ForEach` works only on `publish_agent` (the only action with the`
- `docs/adr/093-typed-user-response-subject-ownership.md:21` — `all three arbitrary subject-bearing actions—`publish`, `publish_agent`, and `approve`—both from their declared`
- `docs/advanced/08-agentic-components.md:912` — `The rule processor can trigger agents by publishing to `agent.task.*`. This is an **optional integration** —`
- `docs/advanced/12-coordinator-pattern.md:104` — ``publish_agent` stamps `task.Role = "ops"` and `task.Tools` on a new TaskMessage published to `agent.task.*`. The same agentic-loop consumes it, loads the ops persona, runs.`
- `docs/concepts/03-streams-vs-kv-watches.md:148` — `│   agent.task.*               ──► agentic-loop    "Execute this task"        │`
- `docs/concepts/03-streams-vs-kv-watches.md:171` — ``agent.task.*` carries an instruction to do expensive work. If the agentic-loop component`
- `docs/concepts/15-payload-registry.md:544` — `| **Payload registry + BaseMessage** | Stream pub/sub, fan-out, polymorphic consumers | `agent.task.*`, `agent.complete.*` |`
- `docs/concepts/18-rule-driven-artifacts.md:142` — `| `publish_agent` | Spawns an agentic loop with a role + prompt + result subject. | Need rendering, transformation, or any LLM-mediated shape change. |`
- `docs/concepts/25-phased-agentic-chains.md:116` — `| `publish_agent` rule action | ✅ shipped | `processor/rule/actions.go:118` |`
- `docs/proposals/gh952-user-response-contract-inventory.md:57` — `All three call the same `Publisher`. The rule processor installs `actionPublisher` at`
- `docs/proposals/gh952-user-response-contract-inventory.md:58` — ``processor/rule/processor.go:686-704`. That publisher selects JetStream only when a configured output port matches the`
- `docs/proposals/gh952-user-response-contract-inventory.md:59` — `resolved subject; otherwise it uses Core NATS: `processor/rule/publisher.go:26-50`.`
- `openspec/specs/agentic-loop/spec.md:31` — `value and the component `MaxIterations` ceiling. The `publish_agent` rule action MUST expose this as`
- `openspec/specs/composition-validation/spec.md:80` — `share one interpreter. A JetStream input SHALL NOT be a `stream_requirement` finding, even when its only publishers use`
- `openspec/specs/composition-validation/spec.md:81` — `core NATS, when the configuration's explicit `streams` block declares the stream the input binds to BY NAME and one of`
- `openspec/specs/composition-validation/spec.md:82` — `that stream's subjects COVERS (every concrete subject of the input's subject also matches it — not merely overlaps) each`
- `openspec/specs/composition-validation/spec.md:84` — `binds by name, and core-NATS publishes on covered subjects land in it, so the subscriber is fed. A stream declared`
- `openspec/specs/composition-validation/spec.md:147` — `- **GIVEN** a JetStream input port whose only publishers are core NATS outputs, and no explicit stream covering its subjects`
- `openspec/specs/composition-validation/spec.md:149` — `- **THEN** the result carries one `stream_requirement` error naming the subscriber port and every publisher`
- `openspec/specs/composition-validation/spec.md:154` — `- **GIVEN** the same ports and a `streams` declaration whose subjects cover the subscriber's subjects`
- `openspec/specs/composition-validation/spec.md:156` — `- **THEN** neither result carries a `stream_requirement` finding and the edge is still derived`
- `openspec/specs/composition-validation/spec.md:234` — `ComponentManager SHALL run `composition.Analyze` over the admitted Registry declarations and the boot configuration's`
- `openspec/specs/composition-validation/spec.md:235` — `explicit `streams` after the fixed boot set is constructed and before the Registry seals, SHALL log every finding, SHALL fail `Initialize` (and therefore boot) when`
- `openspec/specs/user-response-subject-ownership/spec.md:41` — `Definition validation and post-substitution execution SHALL reject each of `publish`, `publish_agent`, and `approve``

The current capability-spec search found no occurrence of `isJetStreamPortBySubject` or `actionPublisher`. The docs
search found the proposal inventory above; the generated rule schema search found no `publish_agent` literal.

Read-only GitHub claim inspection on 2026-09-03 found:

1. issue #759 OPEN, “natsclient: establish semantic JetStream settlement as the restart-safety foundation”; it claims
  every durable consumer must name semantic completion and #1146 consumes that foundation;
2. issue #1146 OPEN, “agentic-loop: prevent silent ACK and active-state loss across process restart”; it requires task
  settlement after durable consequences and downstream PubAck;
3. issue #1147 OPEN, “epic: make framework restart behavior explicit and provable”; it names #759 then #1146;
4. issue #1158 OPEN, “message/nats: enforce registered payload envelopes on framework-owned subjects”; it owns the
  repository-wide publisher/subject/codec/consumer classification and explicitly keeps #759 byte-oriented;
5. issue #1055 OPEN, “rule: $message/$entity substitution tokens over-consume a .word suffix…”; it records the live
  unreferenced architect-editor subject resolving with a literal token and only a warning;
6. PR #1156 OPEN/DRAFT, `codex/gh759-semantic-settlement` → `main`, refs #759/#1146 and describes the staged settlement
  foundation; and
7. PR #1159 OPEN/DRAFT, `codex/gh1146-agentic-loop-restart` → `codex/gh759-semantic-settlement`, closes #1146 on its
  non-default staging branch.

The closest existing problem-shape owner is #1158 for framework-owned publisher admission and registered envelopes.
#759 owns delivery settlement without decoding payloads, and #1146 owns the first agentic-loop process-replacement
vertical that consumes that settlement foundation. #1055 owns one malformed-subject authoring path in a currently
unloaded first-party rule.

## Spellings of the fact

1. Action spelling: `publish_agent` / `ActionTypePublishAgent`.
2. Declared output spelling: `agent.task.*`; five configured rule processors name the output `agent.task`, while
   `configs/agentic.json` names its rule-processor output `agent_task`.
3. Concrete first-party subjects: `agent.task.research`, `agent.task.subtopic`, `agent.task.synthesis`,
  `agent.task.retry`, `agent.task.partial-synthesis`, `agent.task.research-coordinator`, `agent.task.investigator`,
  `agent.task.research_continuation`, and substituted architect-editor and cron forms.
4. Consumer spelling: input name `agent.task`, subject `agent.task.*`, stream `AGENT`; in `configs/agentic.json` this is
   distinct from the rule-processor output name `agent_task`.
5. Payload spelling: `agentic.TaskMessage`, schema `agent/task/v1`, payload-registry category `task`, BaseMessage source
  `rule-engine`.
6. Publication spellings: `Publisher.Publish`, `actionPublisher.Publish`, `Client.Publish`, and
  `Client.PublishToStream`.
7. State/observability spellings: `rule.task.spawned`, `eventsPublished`, `eventsPublishedTotal`, and
  `actionFailuresTotal`.

## Adjacent but distinct claims

1. The reserved `user.response.*` subject guard is shared by arbitrary subject-bearing actions; it is not AGENT-stream
  admission.
2. Agentic-governance declares an `agent.task.*` validation input and a separate `agent.task.validated.*` flow output;
  this inventory records those surfaces but does not enumerate governance policy behavior.
3. `publish_agent` tool, filesystem, run-scope, lineage, fan-out, and loop-budget fields are payload construction
  concerns adjacent to, but distinct from, publisher admission.
4. Agentic-loop durable-input settlement is already in the #1146 inventory; this addendum records its consumer entry
  only to close the producer-to-consumer path.
5. #1158 is the repository-wide wire-envelope inventory owner; this addendum is only its `publish_agent` slice as it
  intersects #1146.

## Named surfaces not found

1. No `publish_agent` enum/literal was found in `schemas/rule-processor.v1.json`.
2. No definition-load validation tying every `publish_agent` subject to a declared JetStream output family was found
   under `processor/rule`; graph-research has the capability-specific signature check recorded above.
3. No wildcard/pattern match was found in `processor/rule.isJetStreamPortBySubject`; the inspected function uses exact
   equality.
4. No test declaration named `isJetStreamPortBySubject` was found under repository `*_test.go` files.
5. No occurrence of `actionPublisher` or `isJetStreamPortBySubject` was found under `openspec/specs`; the docs proposal
   occurrence is recorded above.

## Searches

All searches ran at the recorded base unless marked otherwise. Zero-result searches are explicit.

1. `sed -n '1,260p' .agents/contracts/semstreams-explorer.md` — explorer contract; complete.
2. `git rev-parse HEAD; git status --short; sed -n '1,90p' openspec/project.md` — base, shared edits, Purpose/Product Boundary.
3. `git grep -n -E 'publish_agent|ActionTypePublishAgent|agent\\.task'` — broad literal inventory; results, output truncated.
4. `git grep -n -E 'ActionTypePublishAgent|executePublishAgent|publishAgentOnce|type Publisher interface' -- processor/rule` — declarations/execution; results.
5. `sed -n '1,260p' processor/rule/actions.go` plus focused action ranges — action structure; results.
6. `sed -n '1,260p' processor/rule/publisher.go` plus stateful evaluator ranges — publication and evaluator behavior; results.
7. `git grep -n -E 'agent.task|AGENT' -- processor/rule/config.go processor/rule/*.go` — local port/config spellings; results.
8. `sed -n` focused `processor/rule/config.go`, `actions.go`, and `config_validation.go` ranges — fields and validation; results.
9. `git grep -n -E 'type Publisher|publisher.Publish|newActionPublisher|isJetStreamPortBySubject' -- processor/rule` — publisher call graph; results.
10. `rg --files processor/rule | sort; git grep -n -E 'publish_agent|PublishAgent|actionPublisher' -- processor/rule/*_test.go` — tests; results.
11. `gopls workspace_symbol ActionTypePublishAgent` — zero results.
12. `gopls workspace_symbol executePublishAgent` — zero results.
13. `gopls workspace_symbol actionPublisher` — zero results.
14. `gopls references processor/rule/actions.go:1616:26` — `executePublishAgent` references; declaration and dispatcher result.
15. `gopls call_hierarchy processor/rule/actions.go:1616:26` — caller `ActionExecutor.Execute`; result.
16. `gopls references processor/rule/actions.go:1690:26` — `publishAgentOnce` references; declaration and iteration/non-iteration calls.
17. `gopls references processor/rule/actions.go:485:2` — `Publisher.Publish` references; interface and publish-agent/publish/approve calls.
18. `sed -n` focused Publisher interface and publisher implementation ranges — results.
19. `gopls implementation processor/rule/actions.go:481:6` — zero results.
20. `gopls references processor/rule/publisher.go:66:22` — declaration and `actionPublisher.Publish` call; results.
21. `git grep -n -E 'publish_agent|agent\\.task' -- configs schemas docs openspec/specs` — first-party configs/schema/docs/specs; results.
22. `git grep -n -E 'action_allowlist|filesystem_policy|loop_max_iterations|model|prompt|role|subject|tools|type' -- schemas/rule-processor.v1.json` — schema fields; results.
23. `git grep -n -E 'Validate.*Subject|subject.*validate|TaskMessage|CategoryTask|RegisterPayloads' -- processor/rule agentic payloadbuiltins` — validation and registry; results.
24. `git grep -n -E 'runActions|ActionIterations|OnRecovery|dispatchAndRecord|Execute\\(ctx' -- processor/rule` plus focused ranges — lifecycle/settlement behavior; results.
25. `git grep -n -E 'stateTracker.Set|Failed to persist rule state|Stop\\(' -- processor/rule` plus focused ranges — state and shutdown; results.
26. `git grep -n -E 'publish_agent|"subject": "agent.task' -- configs/rules` — eleven first-party actions; results.
27. `git grep -n -E 'configs/rules/(deep-research|research-graph)|rule_packs|rules' -- configs/flows configs/examples` — pack loading; results.
28. `git grep -n -E 'func \\(.*\\) Publish(ToStream)?\\(|PublishToStream\\(' -- natsclient processor/agentic-loop processor/rule` — NATS paths; results.
29. `git grep -n -E 'agent\\.task\\.\\*|handleTaskMessage|consumeLongRunningInput|taskInputHandler' -- processor/agentic-loop/config.go processor/agentic-loop/component.go` — consumer; results.
30. `git grep -n -E 'isJetStreamPortBySubject|actionPublisher|publishAgentOnce|ActionTypePublishAgent' -- '*_test.go' 'test/**'` — tests; results; no `isJetStreamPortBySubject` test declaration.
31. `git grep -n -E 'publish_agent|agent\\.task\\.\\*|agent\\.task\\.|AGENT stream|AGENT_STREAM|StreamName.*AGENT|subject-bearing' -- docs/adr docs/concepts docs/advanced openspec/specs README.md` — docs/specs; results.
32. `git grep -n -E 'publish_agent|agent\\.task' -- test/e2e configs` — fixtures/e2e/configs; results.
33. `git grep -n -E 'isJetStreamPortBySubject|actionPublisher' -- docs openspec/specs schemas test` — one
    `actionPublisher` result in `docs/proposals/gh952-user-response-contract-inventory.md`; zero in `openspec/specs`,
    schemas, and tests for the classifier name.
34. `gh issue list -R C360Studio/semstreams --state all --limit 100 --search 'publish_agent OR "agent.task" OR admission in:title,body' ...` — first attempt NOT RUN: sandbox network failure.
35. Same `gh issue list` with read-only network access — results; broad output truncated.
36. `gh pr list -R C360Studio/semstreams --state all --limit 100 --search 'publish_agent OR "agent.task" OR admission in:title,body' ...` — results; broad output truncated.
37. `openspec list` — changes listed; `agentic-loop-restart-safety` 8/96 and `semantic-jetstream-settlement` 44/67.
38. `gh issue view 759`, `1146`, `1147` — targeted read-only claim inspection; results.
39. `gh pr view 1156`, `1159` — targeted read-only claim inspection; results.
40. `sed -n` current inventory headings/status — inventory-format check; results.
41. `rg -l '"name"..."rule-processor"' configs` plus `jq '.components[] | select(.name == "rule-processor") | .config.ports.outputs[]'` — all rule-processor instances; six with `agent.task.*` outputs.
42. `git grep` plus focused `nl -ba` over `component.ResolveSubject`, dispatch publishers, graph-research validation,
    flowgraph/composition, service boot, and current specs — collision-owner declarations and callers; results.
43. `gopls references component/ports.go:13:6` — dispatch and other `ResolveSubject` callers; results.
44. `gopls references component/flowgraph/flowgraph.go:389:6` — composition/test callers of `SubjectCovers`; results.
45. `git grep -n -E 'func validateRulePack|subjectPrefix|typeName|ruleActionSignature' -- frameworkcapabilities/graphresearch/register.go` plus focused source ranges — canonical signature gate; results.
46. `nl -ba agentic/user_types.go:312-435,529-540`; `git grep` for TaskMessage `EntityID`/`Triples`; `gopls implementation agentic/user_types.go:313:6` — payload methods found; no Graphable methods; JSON marshal/unmarshal implementations returned.
47. `rg --files | rg 'graphable\\.go$'`; `git grep` for `Graphable`, `EntityID`, and `Triples`; focused `graph/graphable.go` read — graph interface located; results.
48. `git grep` and focused reads for `message.Payload`, gh952 proposal, composition-validation spec, and requested config output pins — results.
49. `gh issue view 1158`; `gh issue view 1055` — targeted read-only claim inspection; results.
50. `jq '{file,agent_stream:.streams.AGENT}'` across the six rule configurations plus `git grep` for `AGENT`/`agent.>` — all six explicitly declare AGENT covering `agent.>`.
51. `git grep` and focused reads for each of the six rule-processor pack IDs, rule files, and output arrays — results.
52. `git grep -n -E 'TaskMessage' -- processor/agentic-dispatch`; `gopls references` at the channel and HTTP builder calls — both same-class producer paths found in literals; first position resolved, second position returned no object.
53. `gopls references processor/agentic-dispatch/component.go:907:21`; `gopls call_hierarchy` at the same declaration; `gopls implementation agentic/user_types.go:313:6`; scoped TaskMessage/Graphable literal search — channel call returned; JSON interfaces returned; no TaskMessage Graphable methods.
54. Focused `nl -ba` reads of the six rule-processor output arrays — exact output pins; results.
55. `rg -n '^## |^### '` and focused inventory read — correction placement check; results.
56. `git grep -n -E '"name": "agent_task"|"name": "agent.task"' -- configs/agentic.json`; scoped inventory
    search for `six`, output-name, and call wording; focused `configs/agentic.json` read — output `agent_task` at 236
    and consumer input `agent.task` at 327; results.

## Verification

`task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-addendum-first-party-agent-publisher-2026-09-03.md`
PASS: 226/226 pins; moved=0, ambiguous=0, drift=0, malformed=0, unparsed=0.

No code, design, proposal, specification, task, commit, branch, PR, issue, or GitHub state was changed by this inventory pass.
