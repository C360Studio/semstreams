# Inventory: task producer LoopID ownership after tasks 3A.1–3A.3 implementation
base: af829616305afa039dac0550efa78d07e856dd5f
refresh-of: inventory-task-producer-loop-id-post-spec-sync-2026-09-07.md sha256:108c155b26b64e0de2d9e7e60cb994185ddc1f20ea9552759644f2df2fd8a91a

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:144` — `- [x] 3A.1 RED: prove TaskMessage validation returns an ordinary error naming missing LoopID; dispatch and rule produce`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:150` — `- [x] 3A.2 Implement producer-owned task identity. Keep dispatch's retained-byte path; mint rule LoopID once per`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:155` — `- [ ] 3A.3 GREEN: through real NATS, redeliver the exact rule-produced registered bytes across agentic-loop process`
- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id"` // producer-minted loop identity`
- `agentic/user_types.go:408` — `if t.LoopID == "" {`
- `agentic/user_types.go:409` — `return fmt.Errorf("loop_id required")`
- `processor/rule/actions.go:1714` — `LoopID:       uuid.NewString(),`
- `processor/agentic-loop/component.go:1406` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/handlers.go:816` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/handlers.go:868` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/task_loop_id_integration_test.go:36` — `func TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement(t *testing.T) {`

## Spellings of the fact

### TaskMessage declaration, validation, payload, and wire/schema surface

- `agentic/user_types.go:313` — `type TaskMessage struct {`
- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id"` // producer-minted loop identity`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/user_types.go:407` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:408` — `if t.LoopID == "" {`
- `agentic/user_types.go:409` — `return fmt.Errorf("loop_id required")`
- `agentic/user_types.go:436` — `if err := t.validateLoopTokens(); err != nil {`
- `agentic/user_types.go:447` — `// validateLoopTokens refuses any loop instance token whose form cannot be one`
- `agentic/user_types.go:448` — `// the framework minted (ADR-105, #1192). Every one of these four fields is a`
- `agentic/user_types.go:454` — `// Validate is the refusal's one home because it is the gate both sides run: the`
- `agentic/user_types.go:455` — `// rule engine before publishing, durable agentic-loop intake before state, and`
- `agentic/user_types.go:456` — `// direct HandleTask before loop registration. Intake counts and terminates an`
- `agentic/user_types.go:457` — `// invalid delivery; the direct boundary returns a typed invalid error.`
- `agentic/user_types.go:459` — `// LoopID requiredness is checked by Validate before this shared form check.`
- `agentic/user_types.go:467` — `{"loop_id", t.LoopID},`
- `agentic/user_types.go:486` — `// Empty is not refused here. Whether a token is REQUIRED is each carrier's own`
- `agentic/user_types.go:487` — `// question and is asked before this one: TaskMessage requires LoopID but keeps`
- `agentic/user_types.go:488` — `// its continuation tokens optional, while a signal, an approval response, and`
- `agentic/user_types.go:489` — `// an approval-pending event each name a loop that must already exist and reject`
- `agentic/user_types.go:490` — `// an empty token on their own line.`
- `agentic/user_types.go:535` — `func (t *TaskMessage) Schema() message.Type {`
- `agentic/user_types.go:536` — `return message.Type{Domain: Domain, Category: CategoryTask, Version: SchemaVersion}`
- `agentic/user_types.go:540` — `func (t *TaskMessage) MarshalJSON() ([]byte, error) {`
- `agentic/user_types.go:546` — `func (t *TaskMessage) UnmarshalJSON(data []byte) error {`
- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`
- `message/payload.go:50` — `type Payload interface {`
- `message/payload.go:53` — `Schema() Type`
- `message/payload.go:61` — `Validate() error`
- `message/base_message.go:195` — `if err := m.payload.Validate(); err != nil {`
- `message/base_message.go:196` — `return errs.WrapInvalid(err, "BaseMessage", "Validate", "invalid payload")`
- `graph/graphable.go:54` — `type Graphable interface {`
- `agentic/rule_fields.go:259` — `"loop_id": t.LoopID,`
- `agentic/rule_fields_test.go:400` — `// TestZeroPayloadProjectionCoversMandatoryWireFields is the reverse of`
- `agentic/rule_fields_test.go:401` — `// TestRuleFieldsMirrorWireNames: every field the payload ALWAYS puts on the`
- `agentic/rule_fields_test.go:402` — `// wire (no `omitempty`) must either be in the projection or be a declared`
- `agentic/rule_fields_test.go:407` — `// A ZERO payload is the probe precisely because its marshal output is exactly`
- `agentic/rule_fields_test.go:408` — `// the set of non-omitempty fields.`
- `agentic/user_types_test.go:509` — `name: "missing loop_id",`
- `agentic/user_types_test.go:516` — `wantErr: "loop_id required",`
- `agentic/user_types_test.go:678` — `if tt.wantErr != "loop_id required" && tt.task.LoopID == "" {`
- `agentic/user_types_test.go:679` — `tt.task.LoopID = canonicalLoopToken`
- `agentic/user_types_test.go:1018` — `t.Run("empty loop_id is required", func(t *testing.T) {`
- `agentic/user_types_test.go:1022` — `assert.EqualError(t, task.Validate(), "loop_id required")`

No generated component schema names TaskMessage: the recorded `git grep -n -F 'TaskMessage' -- schemas specs`
search returned zero. The generated-schema `loop_id` hits belong to research component descriptions and OpenAPI loop
response/control shapes; the search is recorded below.

### Production TaskMessage construction and producer-local mint spellings

- `processor/agentic-dispatch/task_recovery.go:89` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:98` — `return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil`
- `processor/agentic-dispatch/task_recovery.go:109` — `if loopID == "" {`
- `processor/agentic-dispatch/task_recovery.go:110` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:112` — `task := c.buildTaskMessage(ctx, msg, loopID, slot.taskID)`
- `processor/agentic-dispatch/task_recovery.go:113` — `data, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch"))`
- `processor/agentic-dispatch/component.go:919` — `func (c *Component) buildTaskMessage(ctx context.Context, msg agentic.UserMessage, loopID, taskID string) agentic.TaskMessage {`
- `processor/agentic-dispatch/component.go:920` — `task := agentic.TaskMessage{`
- `processor/agentic-dispatch/component.go:921` — `LoopID:           loopID,`
- `processor/rule/actions.go:1713` — `task := agentic.TaskMessage{`
- `processor/rule/actions.go:1714` — `LoopID:       uuid.NewString(),`
- `test/e2e/scenarios/agentic/approval_signal.go:106` — `func newApprovalGatedTask(now time.Time, suffix, userID string) agentic.TaskMessage {`
- `test/e2e/scenarios/agentic/approval_signal.go:110` — `LoopID:      uuid.NewString(),`
- `test/e2e/scenarios/agentic/scenario.go:602` — `func newTestTask(now time.Time) agentic.TaskMessage {`
- `test/e2e/scenarios/agentic/scenario.go:607` — `LoopID:      uuid.NewString(),`
- `test/e2e/scenarios/research-graph/scenario.go:425` — `parentLoopID := uuid.NewString()`
- `test/e2e/scenarios/research-graph/scenario.go:426` — `task := agentic.TaskMessage{`
- `test/e2e/scenarios/research-graph/scenario.go:427` — `LoopID: parentLoopID,`

### Rule publish_agent construction, validation, marshal, publication, and unit proof

- `processor/rule/actions.go:1690` — `func (e *ActionExecutor) publishAgentOnce(ctx context.Context, action Action, ec *ExecutionContext, iterVarName, iterVarValue string) error {`
- `processor/rule/actions.go:1713` — `task := agentic.TaskMessage{`
- `processor/rule/actions.go:1714` — `LoopID:       uuid.NewString(),`
- `processor/rule/actions.go:1886` — `if err := task.Validate(); err != nil {`
- `processor/rule/actions.go:1887` — `return errs.WrapInvalid(err, "RuleActionExecutor", "publishAgentOnce", "validate substituted task")`
- `processor/rule/actions.go:1957` — `baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")`
- `processor/rule/actions.go:1958` — `data, err := json.Marshal(baseMsg)`
- `processor/rule/actions.go:1963` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/rule/actions_test.go:1040` — `// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary`
- `processor/rule/actions_test.go:1041` — `func TestAction_PublishAgent_PayloadFormat(t *testing.T) {`
- `processor/rule/actions_test.go:1063` — `baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)`
- `processor/rule/actions_test.go:1066` — `task, ok := baseMsg.Payload().(*agentic.TaskMessage)`
- `processor/rule/actions_test.go:1072` — `parsedLoopID, err := uuid.Parse(task.LoopID)`
- `processor/rule/actions_test.go:1074` — `assert.Equal(t, uuid.Version(4), parsedLoopID.Version())`
- `processor/rule/actions_test.go:1075` — `assert.Equal(t, parsedLoopID.String(), task.LoopID, "loop_id must use canonical UUID text")`

### Durable intake, direct HandleTask classification, and fallback-mint census

- `processor/agentic-loop/component.go:1237` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1238` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/component.go:1248` — `related, hasLineage, err := c.preflightDecodedTask(task)`
- `processor/agentic-loop/component.go:1251` — `c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)`
- `processor/agentic-loop/component.go:1253` — `return natsclient.DeliveryDecisionTerminate, err`
- `processor/agentic-loop/component.go:1267` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1269` — `return loopSettlementDecision(err), err`
- `processor/agentic-loop/component.go:1276` — `result, err = c.handler.HandleTask(ctx, *task)`
- `processor/agentic-loop/component.go:1290` — `return loopSettlementDecision(err),`
- `processor/agentic-loop/component.go:1291` — `fmt.Errorf("handle task %q: %w", task.TaskID, err)`
- `processor/agentic-loop/component.go:1405` — `func (c *Component) preflightDecodedTask(task *agentic.TaskMessage) (map[string]any, bool, error) {`
- `processor/agentic-loop/component.go:1406` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/component.go:1407` — `return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "validate decoded task")`
- `processor/agentic-loop/handlers.go:811` — `func (h *MessageHandler) HandleTask(ctx context.Context, task TaskMessage) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:816` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/handlers.go:817` — `return HandlerResult{}, errs.WrapInvalid(`
- `processor/agentic-loop/handlers.go:838` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:839` — `if existingID != task.LoopID {`
- `processor/agentic-loop/handlers.go:840` — `return HandlerResult{}, errs.WrapFatal(`
- `processor/agentic-loop/handlers.go:841` — `fmt.Errorf("task %q is already bound to loop %q; delivery carries loop %q", task.TaskID, existingID, task.LoopID),`
- `processor/agentic-loop/handlers.go:844` — `"task correlation conflict",`
- `processor/agentic-loop/handlers.go:850` — `return HandlerResult{LoopID: existingID}, nil`
- `processor/agentic-loop/handlers.go:868` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/loop_token_intake_test.go:95` — `// spec: entity-id-contract / A loop instance token is minted at its framework birth seam`
- `processor/agentic-loop/loop_token_intake_test.go:96` — `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`
- `processor/agentic-loop/loop_token_intake_test.go:97` — `func TestMissingLoopIDIsTerminatedAtIntakeBeforeState(t *testing.T) {`
- `processor/agentic-loop/loop_token_intake_test.go:125` — `delete(payload, "loop_id")`
- `processor/agentic-loop/loop_token_intake_test.go:137` — `if err == nil || !errs.IsInvalid(err) {`
- `processor/agentic-loop/loop_token_intake_test.go:140` — `if !msg.terminated.Load() || msg.acked.Load() || msg.naked.Load() {`
- `processor/agentic-loop/loop_token_intake_test.go:144` — `if delta := testutil.ToFloat64(comp.metrics.taskIntakeRejections.WithLabelValues(`
- `processor/agentic-loop/loop_token_intake_test.go:148` — `if delta := testutil.ToFloat64(comp.metrics.loopsCreated) - beforeCreated; delta != 0 {`
- `processor/agentic-loop/loop_token_intake_test.go:154` — `if got := len(comp.handler.loopManager.loops); got != 0 {`
- `processor/agentic-loop/delivery_owner_test.go:395` — `// spec: entity-id-contract / A loop instance token is minted at its framework birth seam`
- `processor/agentic-loop/delivery_owner_test.go:396` — `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`
- `processor/agentic-loop/delivery_owner_test.go:397` — `func TestDirectHandleTaskRefusesMissingLoopIDBeforeState(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:404` — `require.Error(t, err)`
- `processor/agentic-loop/delivery_owner_test.go:405` — `require.True(t, errs.IsInvalid(err))`
- `processor/agentic-loop/delivery_owner_test.go:406` — `require.ErrorContains(t, err, "loop_id")`
- `processor/agentic-loop/delivery_owner_test.go:409` — `require.Empty(t, handler.loopManager.loops)`
- `processor/agentic-loop/delivery_owner_test.go:413` — `func TestDirectHandleTaskQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:423` — `_, err = handler.HandleTask(t.Context(), conflict)`
- `processor/agentic-loop/delivery_owner_test.go:426` — `require.True(t, errs.IsFatal(err), "task correlation conflict must quarantine")`
- `processor/agentic-loop/delivery_owner_test.go:427` — `require.ErrorContains(t, err, first.LoopID)`
- `processor/agentic-loop/delivery_owner_test.go:428` — `require.ErrorContains(t, err, conflict.LoopID)`
- `processor/agentic-loop/delivery_owner_test.go:429` — `require.Len(t, handler.loopManager.loops, 1)`
- `processor/agentic-loop/delivery_owner_test.go:434` — `func TestTaskDeliveryQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:446` — `result, admitted := consumeAdmittedDelivery(`
- `processor/agentic-loop/delivery_owner_test.go:451` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(), "cause: %v", result.Err())`
- `processor/agentic-loop/delivery_owner_test.go:452` — `require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:453` — `require.Len(t, handler.loopManager.loops, 1)`

The three scoped fallback searches over `component.go` and `handlers.go` returned zero for `GenerateLoopID()`,
`CreateLoop(`, and `uuid.NewString()`; their commands and counts are recorded below.

### Governance preservation N/A surface

- `processor/agentic-governance/filter.go:37` — `type Message struct {`
- `processor/agentic-governance/filter.go:39` — `ID string `json:"id"``
- `processor/agentic-governance/filter.go:42` — `Type MessageType `json:"type"``
- `processor/agentic-governance/filter.go:45` — `UserID string `json:"user_id"``
- `processor/agentic-governance/filter.go:48` — `SessionID string `json:"session_id"``
- `processor/agentic-governance/filter.go:51` — `ChannelID string `json:"channel_id"``
- `processor/agentic-governance/filter.go:57` — `Content Content `json:"content"``
- `processor/agentic-governance/component.go:426` — `outputMsg := result.ModifiedMessage`
- `processor/agentic-governance/component.go:428` — `outputMsg = &msg`
- `processor/agentic-governance/component.go:438` — `outputData, err := json.Marshal(outputMsg)`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`
- `processor/agentic-governance/tool_filter.go:179` — `// ToolCallToMessage converts an agentic.ToolCall into a governance Message`
- `processor/agentic-governance/tool_filter.go:181` — `func ToolCallToMessage(call agentic.ToolCall, userID, channelID string) *Message {`
- `processor/agentic-governance/tool_filter.go:192` — `"loop_id":   call.LoopID,`

The tracked governance package has zero `TaskMessage` hits. Its two `LoopID`/`loop_id` hits are the ToolCall metadata
conversion and its test. The three searches and counts are recorded below.

### Exact registered bytes and real-NATS process-replacement proof

- `processor/agentic-loop/task_loop_id_integration_test.go:1` — `//go:build integration`
- `processor/agentic-loop/task_loop_id_integration_test.go:27` — `func (p *taskLoopIDStreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {`
- `processor/agentic-loop/task_loop_id_integration_test.go:29` — `p.data = append([]byte(nil), data...)`
- `processor/agentic-loop/task_loop_id_integration_test.go:30` — `return p.client.PublishToStream(ctx, subject, data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:33` — `// spec: entity-id-contract / A loop instance token is minted at its framework birth seam`
- `processor/agentic-loop/task_loop_id_integration_test.go:34` — `// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary`
- `processor/agentic-loop/task_loop_id_integration_test.go:35` — `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`
- `processor/agentic-loop/task_loop_id_integration_test.go:36` — `func TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement(t *testing.T) {`
- `processor/agentic-loop/task_loop_id_integration_test.go:38` — `tc := natsclient.NewTestClient(t, natsclient.WithStreams(`
- `processor/agentic-loop/task_loop_id_integration_test.go:41` — `bucket, err := tc.CreateKVBucket(ctx, "AGENT_LOOPS_TASK3A_REPLACEMENT")`
- `processor/agentic-loop/task_loop_id_integration_test.go:46` — `executor := ruleprocessor.NewActionExecutorFull(nil, nil, publisher, types.PlatformMeta{`
- `processor/agentic-loop/task_loop_id_integration_test.go:50` — `require.NoError(t, executor.Execute(ctx, ruleprocessor.Action{`
- `processor/agentic-loop/task_loop_id_integration_test.go:60` — `stored, err := stream.GetLastMsgForSubject(ctx, publisher.subject)`
- `processor/agentic-loop/task_loop_id_integration_test.go:62` — `require.Equal(t, publisher.data, stored.Data, "the loop must receive the exact registered bytes emitted by the rule")`
- `processor/agentic-loop/task_loop_id_integration_test.go:64` — `decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(stored.Data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:66` — `task, ok := decoded.Payload().(*agentic.TaskMessage)`
- `processor/agentic-loop/task_loop_id_integration_test.go:68` — `require.NoError(t, task.Validate())`
- `processor/agentic-loop/task_loop_id_integration_test.go:69` — `parsedLoopID, err := uuid.Parse(task.LoopID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:71` — `require.Equal(t, uuid.Version(4), parsedLoopID.Version())`
- `processor/agentic-loop/task_loop_id_integration_test.go:73` — `newProcess := func() *Component {`
- `processor/agentic-loop/task_loop_id_integration_test.go:84` — `first := newProcess()`
- `processor/agentic-loop/task_loop_id_integration_test.go:85` — `decision, err := first.handleTaskMessage(ctx, stored.Data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:90` — `require.Equal(t, task.TaskID, firstEntity.TaskID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:91` — `require.Len(t, first.handler.loopManager.loops, 1)`
- `processor/agentic-loop/task_loop_id_integration_test.go:93` — `second := newProcess()`
- `processor/agentic-loop/task_loop_id_integration_test.go:94` — `decision, err = second.handleTaskMessage(ctx, stored.Data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:99` — `require.Equal(t, firstEntity.ID, secondEntity.ID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:100` — `require.Equal(t, firstEntity.TaskID, secondEntity.TaskID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:101` — `require.Len(t, second.handler.loopManager.loops, 1)`
- `processor/agentic-loop/task_loop_id_integration_test.go:103` — `durable, err := bucket.Get(ctx, task.LoopID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:106` — `require.NoError(t, json.Unmarshal(durable.Value(), &durableEntity))`
- `processor/agentic-loop/task_loop_id_integration_test.go:107` — `require.Equal(t, task.LoopID, durableEntity.ID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:108` — `require.Equal(t, task.TaskID, durableEntity.TaskID)`

### Outward spellings, examples, and adopter migration boundary

- `agentic/README.md:113` — `// A new TaskMessage producer is the framework birth seam: it mints a canonical`
- `agentic/README.md:114` — `// v4 UUID before validation and marshal. A continuation echoes its admitted ID.`
- `processor/agentic-loop/README.md:330` — `"loop_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",`
- `processor/agentic-loop/README.md:338` — ``loop_id` is required. A producer creating new loop work calls `uuid.NewString()` once before validation and marshal;`
- `processor/agentic-loop/README.md:339` — `a continuation producer echoes the admitted existing token. Retry the same uncertain publication with the same`
- `processor/agentic-loop/README.md:340` — `serialized bytes. Agentic-loop refuses an absent or noncanonical value before state and never mints a replacement`
- `processor/agentic-loop/doc.go:169` — `//	// Direct HandleTask validates the TaskMessage and does not marshal it.`
- `processor/agentic-loop/doc.go:170` — `//	// A stream producer mints once before wrapping and marshal, then reuses`
- `processor/agentic-loop/doc.go:173` — `//	result, err := handler.HandleTask(ctx, TaskMessage{`
- `processor/agentic-loop/doc.go:174` — `//	    LoopID: uuid.NewString(),`
- `processor/agentic-loop/doc.go:335` — `//	task := agenticloop.TaskMessage{`
- `processor/agentic-loop/doc.go:336` — `//	    LoopID: uuid.NewString(),`
- `processor/agentic-loop/doc.go:343` — `// Production code mints once before marshal and reuses these serialized bytes`
- `processor/agentic-loop/doc.go:348` — `//	natsClient.PublishToStream(ctx, "agent.task.review", taskData)`
- `docs/concepts/13-agentic-systems.md:489` — `The producer of a new `TaskMessage` owns its loop birth identity and fixes a canonical UUID before publication.`
- `docs/concepts/13-agentic-systems.md:490` — `Agentic-loop validates that identity and never repairs an absent value. See`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:62` — `1. **A loop instance token is minted at its framework birth seam as a v4 UUID**, carried in canonical RFC 4122 text`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:63` — `form (36 bytes, lowercase, hyphenated). For newly published TaskMessage work, its producer is that seam and mints`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:67` — `2. **Enforcement lives at accepting seams**, not in a registry or family-table mechanism: `TaskMessage.Validate``
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:76` — `Task intake never repairs absence through minting, derivation, scan, map, ledger, bucket, or another owner.`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:77` — `Producer-local `uuid.NewString()` is the task-birth seam; no exported helper or constructor wraps it.`
- `docs/operations/migration-beta162-to-beta163.md:868` — `- Every `TaskMessage` requires nonempty canonical `loop_id`. `TaskMessage.Validate` returns an ordinary error naming`
- `docs/operations/migration-beta162-to-beta163.md:882` — `### Direct TaskMessage producer migration (breaking)`
- `docs/operations/migration-beta162-to-beta163.md:884` — `Every TaskMessage now requires `loop_id`. For new work, call `uuid.NewString()` once before `Validate` and marshal. For`
- `docs/operations/migration-beta162-to-beta163.md:885` — `continuation work, echo the admitted existing LoopID. When retrying an uncertain publication, reuse the same serialized`
- `docs/operations/migration-beta162-to-beta163.md:886` — `bytes; do not reconstruct the TaskMessage or regenerate LoopID. Missing/noncanonical identity is producer-invalid and`
- `docs/operations/migration-beta162-to-beta163.md:887` — `is refused before loop state. There is no empty-ID compatibility path, legacy reader, scan, mapping, or recovery bucket.`
- `docs/operations/migration-beta162-to-beta163.md:891` — `| semdev `ca3956af2ed8` | `internal/intake/coordinatortask.go` | Add producer-local v4 LoopID before validation/marshal |`
- `docs/operations/migration-beta162-to-beta163.md:892` — `| semteams `ce22c961d300` | `cmd/semteams/chainpause/decision_handler.go` | Add producer-local v4 LoopID before validation/marshal |`
- `docs/operations/migration-beta162-to-beta163.md:893` — `| semspec `5a9496eecc45` | lesson decomposer, QA reviewer, researcher manager, question tool | Add producer-local v4 LoopID at each new-task construction |`
- `docs/operations/migration-beta162-to-beta163.md:894` — `| semmachina `841c45e8bb01` | `internal/persona/spec.go` | Add LoopID before its existing Validate call |`
- `docs/operations/migration-beta162-to-beta163.md:895` — `| semsage `4d28b4dc1210` | UI API and spawn executor | Add LoopID to UI API; spawn executor already conforms |`
- `docs/operations/migration-beta162-to-beta163.md:896` — `| semdragon `07f4de9b6588` | quest bridge, DAG executor, explore tool | Replace prefixed NUID birth tokens with canonical v4 UUIDs |`
- `docs/operations/migration-beta162-to-beta163.md:897` — `| semops, semsource, semconnect, semboids, semembed, semlink, semmem | No direct production constructor found | No direct code migration found |`
- `docs/operations/migration-beta162-to-beta163.md:899` — `Rule JSON users in semdev, semteams, and semspec require no sister code change: SemStreams' rule `publish_agent``
- `docs/operations/migration-beta162-to-beta163.md:900` — `producer mints the new-task LoopID before publishing. Sister repositories remain read-only to SemStreams agents; their`
- `docs/operations/migration-beta162-to-beta163.md:903` — `Use fresh NATS state only after every direct producer is updated. Add no alias, dual format, online migration, or`
- `docs/operations/migration-beta162-to-beta163.md:909` — `- A direct TaskMessage producer that omits `loop_id` now fails loudly before loop state; apply the migration above.`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:3` — `### Requirement: A loop instance token is minted at its framework birth seam`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:7` — `canonical RFC 4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. For a newly published `TaskMessage`, the`
- `openspec/changes/agentic-loop-restart-safety/specs/entity-id-contract/spec.md:31` — `- `TaskMessage.Validate` MUST first return an ordinary validation error naming `loop_id` when LoopID is absent, then`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:152` — `### Requirement: Loop task, request, and tool work use only required correlation`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:154` — `Every TaskMessage producer SHALL supply a nonempty canonical LoopID before validation, envelope marshal, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:177` — `#### Scenario: task identity is fixed before durable publication`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:187` — `#### Scenario: task mapping is stable across process replacement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:189` — `- **GIVEN** the exact registered bytes of a rule-produced TaskMessage have been retained`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:75` — `### Requirement: Publish-agent preserves the registered payload boundary`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:77` — ``publish_agent` SHALL mint one random version 4 LoopID locally for each execution producing new loop work, assign it`
- `openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md:85` — `#### Scenario: publish-agent fixes new-loop identity before publication`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:153` — `Add no exported helper, constructor, configurable generator, deterministic derivation, scan, map, ledger, bucket, or`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:154` — `second owner. Do not alter tasks 9.5–9.7 admission, subject coverage, classifier, registry, or PubAck contracts.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:161` — `Blocked 2026-09-08: the real-NATS proof, affected unit/race tests, full test/race suites, schema generation, lint,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:162` — `contract tests, and strict OpenSpec validation pass. Three serialized `task e2e:agentic` attempts stopped before`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:163` — `build or test execution because Docker Hub metadata for `golang:1.26-alpine` returned `DeadlineExceeded`; compose`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:164` — `cleanup completed after every attempt. Leave this task unchecked until that external gate executes successfully.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:330` — `## 9. AGENT admission, first-party publisher, and loop authority`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:345` — `- [ ] 9.5 RED: add all-six-configuration and four-static-producer real-NATS tests, both `agent.task`/`agent_task``
- `openspec/changes/agentic-loop-restart-safety/tasks.md:351` — `- [ ] 9.6 Implement rule-processor caller-local admission through the same internal validator before evaluator start.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:354` — ``actionPublisher`; do not duplicate the matcher. Covered task subjects use `PublishToStream`/PubAck; uncovered/refused`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:355` — `output fails before post-send side effects. Require registered `TaskMessage` Payload, never Graphable; add no gate,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:357` — `- [ ] 9.7 GREEN: prove six classifier surfaces cannot select core NATS for covered task subjects, four static producer`
- `AGENTS.md:69` — `## Repository ownership boundary (HARD RULE)`
- `AGENTS.md:71` — `SemStreams agents mutate only the SemStreams repository. Sister repositories are read-only inventory sources: agents`
- `AGENTS.md:72` — `may inspect them to measure downstream impact, but must not create branches, edit files, commit, push, open or modify`
- `AGENTS.md:75` — `When a SemStreams change breaks a downstream adopter, record the exact impact and migration instructions in a`
- `AGENTS.md:76` — `SemStreams-owned migration document. The downstream repository owner implements and validates that migration in its`
- #1035 — agentic-loop: a task rejected at preflight notifies nobody — routing fields carried 'for error notifications' go unused
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1145 — framework: declare and prove component restart behavior
- #1147 — epic: make framework restart behavior explicit and provable
- #1177 — beta.163: identity, boundary, and restart — the pre-v1 breaking wave (tracking)
- PR #1159 (draft) — fix(agentic-loop): preserve durable work across process restart

`openspec list` reported `agentic-loop-restart-safety` at 30/84 tasks and `semantic-jetstream-settlement` at 44/67
tasks. The open-PR body search identifies draft PR #1159 as the claim for #1146; other open draft PR bodies were read
and are outside this task-producer surface.

## Consumers

- `agentic/rule_fields.go:257` — `func (t *TaskMessage) RuleFields() map[string]any {`
- `agentic/rule_fields.go:259` — `"loop_id": t.LoopID,`
- `processor/agentic-dispatch/task_recovery.go:192` — `case task.LoopID == "":`
- `processor/agentic-dispatch/task_recovery.go:194` — `case requestedLoopID != "" && task.LoopID != requestedLoopID:`
- `processor/agentic-loop/component.go:1266` — `if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {`
- `processor/agentic-loop/component.go:1267` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1287` — `"error", err, "task_id", task.TaskID, "loop_id", task.LoopID)`
- `processor/agentic-loop/component.go:1418` — `c.deps.Platform.Org, c.deps.Platform.Platform, task.LoopID)`
- `processor/agentic-loop/handlers.go:838` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:868` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/handlers.go:873` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/handlers.go:881` — `loopID = task.LoopID`
- `processor/agentic-loop/lineage_preflight_test.go:49` — `task.LoopID = test.loopID`
- `processor/agentic-loop/lineage_preflight_test.go:59` — `if test.wantSameID && task.LoopID != test.loopID {`
- `processor/agentic-loop/lineage_preflight_test.go:85` — `if task.LoopID != originalLoopID {`
- `processor/agentic-loop/lineage_preflight_test.go:109` — `redelivery := firstTask`
- `processor/agentic-loop/lineage_preflight_test.go:113` — `if redelivery.LoopID != firstTask.LoopID {`
- `processor/agentic-loop/lineage_preflight_test.go:116` — `second, err := component.handler.HandleTask(context.Background(), redelivery)`

`gopls references agentic/user_types.go:314:3` returned 192 references. The pinned consumers above are the production
readers on this task-producer surface plus the identity-preservation tests; the remaining references are payload/test,
other loop-plane, and E2E uses enumerated by that recorded structure search.

## Problem shape

- `processor/agentic-dispatch/task_recovery.go:89` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:98` — `return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil`
- `processor/agentic-dispatch/task_recovery.go:109` — `if loopID == "" {`
- `processor/agentic-dispatch/task_recovery.go:110` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:113` — `data, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch"))`
- `processor/agentic-dispatch/task_recovery.go:117` — `return preparedDispatchTask{task: task, data: data, subject: slot.subject}, nil`
- `processor/agentic-loop/lineage_preflight_test.go:109` — `redelivery := firstTask`
- `processor/agentic-loop/lineage_preflight_test.go:113` — `if redelivery.LoopID != firstTask.LoopID {`
- `processor/agentic-loop/lineage_preflight_test.go:120` — `if second.Created || second.LoopID != first.LoopID {`
- `processor/agentic-loop/task_loop_id_integration_test.go:62` — `require.Equal(t, publisher.data, stored.Data, "the loop must receive the exact registered bytes emitted by the rule")`
- `processor/agentic-loop/task_loop_id_integration_test.go:85` — `decision, err := first.handleTaskMessage(ctx, stored.Data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:94` — `decision, err = second.handleTaskMessage(ctx, stored.Data)`
- `processor/agentic-loop/task_loop_id_integration_test.go:99` — `require.Equal(t, firstEntity.ID, secondEntity.ID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:101` — `require.Len(t, second.handler.loopManager.loops, 1)`
- `processor/agentic-loop/task_loop_id_integration_test.go:107` — `require.Equal(t, task.LoopID, durableEntity.ID)`
- `processor/agentic-loop/task_loop_id_integration_test.go:108` — `require.Equal(t, task.TaskID, durableEntity.TaskID)`

## Searches

### Structure

- `gopls workspace_symbol -matcher=fuzzy TaskMessage` → TaskMessage at `agentic/user_types.go:313`; task handlers and tests also returned.
- `gopls workspace_symbol -matcher=fuzzy LoopID` → loop-token fields/functions across the workspace; initial output was truncated.
- `gopls workspace_symbol -matcher=fuzzy ActionExecutor` → ActionExecutor at `processor/rule/actions.go:537` plus constructors, methods, interface, and tests.
- `gopls workspace_symbol -matcher=fuzzy HandleTask` → HandleTask at `processor/agentic-loop/handlers.go:811` plus component handlers and tests.
- `gopls workspace_symbol -matcher=fuzzy handleTaskDelivery` → no named symbol; initial host-cache attempt failed before package load.
- `gopls workspace_symbol -matcher=fuzzy PublishAgent` → publishAgentOnce at `processor/rule/actions.go:1690`, executePublishAgent, action constant, and tests.
- `gopls workspace_symbol -matcher=fuzzy GovernanceMessage` → governance Message and message metrics under fuzzy matching; initial host-cache attempt emitted cache errors.
- Initial seven `gopls workspace_symbol` calls above without task-local caches → package-load/cache permission errors; rerun with `GOCACHE=/tmp/gh1146-inventory-gocache` and `GOPLSCACHE=/tmp/gh1146-inventory-goplscache`.
- Cached `gopls workspace_symbol -matcher=fuzzy TaskMessage` → 57 lines.
- Cached `gopls workspace_symbol -matcher=fuzzy ActionExecutor` → 72 lines.
- Cached `gopls workspace_symbol -matcher=fuzzy HandleTask` → 21 lines.
- Cached `gopls workspace_symbol -matcher=fuzzy handleTaskMessage` → 2 lines.
- Cached `gopls workspace_symbol -matcher=fuzzy publishAgentOnce` → 1 line.
- Cached `gopls workspace_symbol -matcher=fuzzy Message` → broad workspace result; governance Message at `processor/agentic-governance/filter.go:37`.
- Cached `gopls workspace_symbol -matcher=fuzzy Payload` → Payload at `message/payload.go:50` plus payload symbols.
- Cached `gopls workspace_symbol -matcher=fuzzy Publisher` → rule Publisher at `processor/rule/actions.go:482` plus publisher symbols.
- Cached `gopls workspace_symbol -matcher=fuzzy Graphable` → Graphable at `graph/graphable.go:54` plus implementers/tests.
- Cached `gopls workspace_symbol -matcher=fuzzy preflightDecodedTask` → 3 symbols.
- Cached `gopls workspace_symbol -matcher=fuzzy TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement` → 0 because the file is integration-tagged.
- Cached `gopls workspace_symbol -matcher=fuzzy taskLoopIDStreamPublisher` → 0 because the file is integration-tagged.
- `gopls -build_flags=-tags=integration workspace_symbol -matcher=fuzzy TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement` → command rejected: this gopls version has no `-build_flags` flag.
- `gopls -build_flags=-tags=integration workspace_symbol -matcher=fuzzy taskLoopIDStreamPublisher` → command rejected: this gopls version has no `-build_flags` flag.
- Cached `gopls symbols processor/agentic-loop/task_loop_id_integration_test.go` → taskLoopIDStreamPublisher, Publish, and TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement.
- Cached `gopls implementation agentic/user_types.go:313:8` → 10 interfaces, including `message.Payload` and `message.RuleReadable`; Graphable was not returned.
- Cached `gopls references agentic/user_types.go:314:3` → 192.
- Cached `gopls references agentic/user_types.go:407:25` → 30.
- Cached `gopls references processor/rule/actions.go:1690:30` → 4.
- Cached `gopls references processor/agentic-loop/handlers.go:811:30` → 73.
- Cached `gopls references processor/agentic-loop/component.go:1407:30` → 6.
- Cached `gopls references processor/agentic-governance/filter.go:37:8` → 76.
- Cached `gopls call_hierarchy agentic/user_types.go:407:25` → 17 callers, 5 callees.
- Cached `gopls call_hierarchy processor/rule/actions.go:1690:30` → 1 caller, 32 callees.
- Cached `gopls call_hierarchy processor/agentic-loop/handlers.go:811:30` → 73 callers, 34 callees.
- Cached `gopls call_hierarchy processor/agentic-loop/component.go:1237:25` → 5 callers, 27 callees.
- Cached `gopls workspace_symbol -matcher=fuzzy TestDirectHandleTaskQuarantinesTaskIDLoopIDConflict` → 1.
- Cached `gopls workspace_symbol -matcher=fuzzy TestTaskDeliveryQuarantinesTaskIDLoopIDConflict` → 1.

### Literals and tracked content

- `git grep -n -F 'TaskMessage{' -- '*.go' ':!**/*_test.go'` → 13.
- `git grep -n -E 'NewTaskMessage|NewLoopID|func (New|Prepare|Mint).*Task' -- '*.go'` → 0.
- `git grep -n -E 'GenerateLoopID\(|CreateLoop\(|CreateLoopWithID\(' -- processor/agentic-loop '*.go'` → 126.
- `git grep -n -F 'GenerateLoopID()' -- processor/agentic-loop/component.go processor/agentic-loop/handlers.go` → 0.
- `git grep -n -E 'CreateLoop\(' -- processor/agentic-loop/handlers.go` → 0.
- `git grep -n -F 'uuid.NewString()' -- processor/agentic-loop/component.go processor/agentic-loop/handlers.go` → 0.
- `git grep -n -F 'LoopID: uuid.NewString()' -- processor/rule/actions.go` → 0; spacing-exact spelling did not match gofmt alignment.
- `git grep -n -E 'LoopID:[[:space:]]+uuid.NewString' -- processor/rule/actions.go` → 1.
- `git grep -n -F 'LoopID:      uuid.NewString()' -- test/e2e/scenarios/agentic` → 2.
- `git grep -n -F 'LoopID: parentLoopID' -- test/e2e/scenarios/research-graph/scenario.go` → 1.
- `git grep -n -F 'json:"loop_id' -- '*.go'` → 28.
- `git grep -n -F 'loop_id,omitempty' -- '*.go'` → 9.
- `git grep -n -F 'task.Validate()' -- '*.go'` → 14.
- `git grep -n -F 'uuid.NewString()' -- processor/rule processor/agentic-dispatch test/e2e/scenarios/agentic '*.go'` → 149.
- `git grep -n -F 'TaskMessage' -- processor/agentic-governance` → 0.
- `git grep -n -F 'LoopID' -- processor/agentic-governance` → 2.
- `git grep -n -F 'loop_id' -- processor/agentic-governance` → 2.
- `git grep -n -F 'TaskMessage' -- schemas specs` → 0.
- `git grep -n -F 'loop_id' -- schemas specs` → 18.
- `git grep -n -E 'Required|omitempty|Schema\(\)' -- component/schema.go agentic/user_types.go agentic/rule_fields.go schemas specs` → 50.
- `git grep -n -E 'rule_fields|RuleFields|mandatory.*wire|mandatory.*field' -- agentic '*_test.go' test` → 67.
- `git grep -n -E 'TaskMessage|agentic.task.v1|CategoryTask' -- payloadbuiltins payloadregistry agentic` → 147.
- `git grep -n -F 'Publish-agent preserves the registered payload boundary' -- '*.go'` → 1 tracked hit; the integration-tagged proof is untracked at this checkpoint and was enumerated with `gopls symbols`.
- `git grep -n -F 'Loop task, request, and tool work use only required correlation' -- '*.go'` → 9 tracked hits; the integration-tagged proof is untracked at this checkpoint.
- `git grep -n -F 'A loop instance token is minted at its framework birth seam' -- '*.go'` → 6 tracked hits; the integration-tagged proof is untracked at this checkpoint.
- `git grep -n -E 'TaskMessage|task_message|task-message|task message' -- agentic processor/agentic-dispatch processor/agentic-loop processor/agentic-governance processor/rule test/e2e/scenarios/agentic test/e2e/scenarios/research-graph openspec/changes/agentic-loop-restart-safety docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md docs/concepts/13-agentic-systems.md docs/operations/migration-beta162-to-beta163.md` → 678.
- `git grep -n -E 'LoopID|loop_id|loop-id|loop id' -- agentic processor/agentic-dispatch processor/agentic-loop processor/agentic-governance processor/rule test/e2e/scenarios/agentic test/e2e/scenarios/research-graph openspec/changes/agentic-loop-restart-safety docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md docs/concepts/13-agentic-systems.md docs/operations/migration-beta162-to-beta163.md` → 1968.
- `git grep -n -E 'producer-minted|producer owned|producer-owned|framework birth seam|birth seam' -- agentic processor/agentic-dispatch processor/agentic-loop processor/agentic-governance processor/rule test/e2e/scenarios/agentic test/e2e/scenarios/research-graph openspec/changes/agentic-loop-restart-safety docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md docs/concepts/13-agentic-systems.md docs/operations/migration-beta162-to-beta163.md` → 20.
- `git grep -n -E 'agent\.task|agent_task' -- processor/agentic-dispatch processor/agentic-loop processor/rule openspec/changes/agentic-loop-restart-safety` → 279.
- `git grep -n -E 'TaskID-to-LoopID|TaskID.*LoopID|task_id.*loop_id|task-to-loop' -- agentic processor/agentic-dispatch processor/agentic-loop processor/rule openspec/changes/agentic-loop-restart-safety docs` → 64.
- `git grep -n -F 'loop_id' -- docs/operations/migration-beta162-to-beta163.md` → 7.
- `git grep -n -E 'TaskMessage|uuid.NewString|producer|registered bytes' -- docs/operations/migration-beta162-to-beta163.md` → 18.
- `git grep -n -E 'TaskMessage|loop_id|uuid.NewString|producer' -- agentic/README.md processor/agentic-loop/README.md processor/agentic-loop/doc.go docs/concepts/13-agentic-systems.md` → 31.
- `git grep -n -E 'TaskMessage|loop.?id|framework-mint|mint.*loop|new work' -- openspec/specs openspec/changes/agentic-loop-restart-safety docs/adr` → recorded in the accepted post-spec-sync inventory; not rerun in this pass.
- `git grep -n -E 'TaskMessage|loop_id|registered bytes|process replacement' -- openspec/changes/agentic-loop-restart-safety/specs openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md` → 46.
- `git grep -n -E '9\.5|9\.6|9\.7|admission|PubAck|classifier' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 31.
- `git grep -n -F 'Repository ownership boundary' -- AGENTS.md .agents docs openspec` → 1.
- `git grep -n -E 'Sister repositories|sister repositories|read-only' -- AGENTS.md .agents docs/operations/migration-beta162-to-beta163.md openspec/changes/agentic-loop-restart-safety` → 43.
- `git grep -n -E 'semdev|semteams|semspec|semmachina|semsage|semdragon' -- docs/operations/migration-beta162-to-beta163.md` → 57.
- `git grep -n -E 'task correlation conflict|already bound to loop|existingID != task.LoopID' -- processor/agentic-loop/handlers.go processor/agentic-loop/delivery_owner_test.go` → 4.
- `git grep -n -E 'loopSettlementDecision\(err\)|DeliveryDecisionQuarantine' -- processor/agentic-loop/component.go processor/agentic-loop/delivery_owner_test.go` → 14.
- `git grep -n -E 'form cannot be one|direct HandleTask before loop registration|TaskMessage requires LoopID' -- agentic/user_types.go` → 3.
- `rg -n 'handlers\.go:(860|865|873)|user_types\.go:(447|454|486)|delivery_owner_test\.go:(397|413|434)|component\.go:12(8|9)' openspec/changes/agentic-loop-restart-safety/inventory-task-producer-loop-id-postimpl-2026-09-07.md` → 8.
- `git diff --unified=8 -- processor/agentic-loop/handlers.go processor/agentic-loop/component.go processor/agentic-loop/delivery_owner_test.go agentic/user_types.go | git grep -n -E 'task correlation conflict|loopSettlementDecision|QuarantinesTaskIDLoopIDConflict|direct HandleTask before loop registration|TaskMessage requires LoopID' --no-index -- /dev/stdin` → command rejected: `fatal: unable to resolve revision: --no-index`.
- `git grep -n -E '3A\.1|3A\.2|3A\.3|E2E|real NATS' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 5.
- `rg -n 'openspec/changes/agentic-loop-restart-safety/tasks\.md:' openspec/changes/agentic-loop-restart-safety/inventory-task-producer-loop-id-postimpl-2026-09-07.md` → 11.

### Claims and active work

- `gh issue list --search 'TaskMessage LoopID' --state open --json number,title` → 2 (#1035, #1146).
- `gh issue list --search 'producer LoopID' --state open --json number,title` → 3 (#1035, #857, #1261).
- `gh issue list --search 'agentic loop restart safety' --state open --json number,title` → 9 (#1239, #1146, #1140, #1177, #981, #1155, #1145, #1147, #1261).
- `openspec list` → 2 active changes.
- `gh pr list --state open --json number,title,body,isDraft,headRefName` → 5 open draft PRs; #1159 claims #1146.

## NOT RUN

(none)
