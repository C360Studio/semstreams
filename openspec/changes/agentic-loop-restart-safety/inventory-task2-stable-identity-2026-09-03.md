# Inventory: task 2 stable identity and exact reconciliation

base: 79b0f29f82ce5391013f6c931fae69a28216ac93

frozen-parent: 417beae5552f8f15ad3540edd7d8504c87174c13

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:90` — `- [x] 2.1 RED: prove stable TaskID, random LoopID minting for new work, retained-`TaskMessage` LoopID recovery on`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:93` — `- [x] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:96` — `- [x] 2.3 RED: prove RequestID distinguishes logical provider work and framework execution identity distinguishes tool`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:101` — `- [x] 2.4 Implement RequestID and execution identity only on provider/tool/governance-correlation paths that need`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:103` — `- [ ] 2.5 RED/GREEN: prove ordinary task/control, created, request, response, approval, terminal, governance, and`
- `openspec/changes/agentic-loop-restart-safety/design.md:135` — `## Correlation is lane-scoped`
- `openspec/changes/agentic-loop-restart-safety/design.md:137` — `- Dispatch derives stable TaskID from validated `UserMessage` identity. It mints a random framework LoopID only for`
- `openspec/changes/agentic-loop-restart-safety/design.md:139` — `- Every `AgentRequest` carries a stable RequestID for the provider-work boundary.`
- `openspec/changes/agentic-loop-restart-safety/design.md:140` — `- Provider `ToolCall.ID` remains conversational data. The framework stamps a separate execution identity from`
- `openspec/changes/agentic-loop-restart-safety/design.md:142` — `- Ordinary created, request, approval, continuation, terminal, validated, verdict, ToolResult, and user-response`
- `openspec/changes/agentic-loop-restart-safety/design.md:145` — `- Exact retained reads exist only where a named boundary needs them: provider invocation, completed tool effects,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:132` — `### Requirement: Dispatch task redelivery recovers the committed LoopID`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:203` — `### Requirement: Model response publication is durably at-least-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:152` — `### Requirement: Loop task, request, and tool work use only required correlation`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:75` — `### Requirement: Governance publications are durably at-least-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:49` — `### Requirement: Tool-result publication is durably at-least-once`

## Spellings of the fact

### Shared transport and provable publication outcomes

- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:943` — `return m.publishToStream(ctx, subject, data, "")`
- `natsclient/client.go:947` — `// Nats-Msg-Id header so the server's duplicate-detection window collapses`
- `natsclient/client.go:963` — `func (m *Client) PublishToStreamWithMsgID(ctx context.Context, subject string, data []byte, msgID string) error {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `natsclient/stream.go:977` — `func (c *Client) PublishToStreamWithAck(`
- `natsclient/stream.go:1007` — `ack, err := js.PublishMsg(ctx, msg)`
- `natsclient/publish_msgid_integration_test.go:53` — `"same Nats-Msg-Id within the window must dedup to one stored message")`
- `natsclient/publish_msgid_integration_test.go:57` — `assert.Equal(t, uint64(2), msgCount(), "distinct Nats-Msg-Id must store a new message")`
- `natsclient/publish_msgid_integration_test.go:60` — `require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("x"), ""))`

The synchronous transport produces these currently provable classifications at the publication call boundary:

- `natsclient/client.go:981` — `if err != nil {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `natsclient/client.go:1006` — `if err != nil {`
- `processor/agentic-loop/component.go:1957` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`

A failure before `PublishMsg` is **not sent**. A nil return from synchronous `PublishMsg` is **committed** by PubAck.
An error after invoking `PublishMsg` is **commit-unknown** at this API boundary. In a multi-output sequence, prior nil
publications are **committed**, the failing invocation is **commit-unknown**, and later uncalled publications are
**not sent**.

### Identity carriers and current identity sources

- `message/base_message.go:121` — `func NewBaseMessage(msgType Type, payload Payload, source string, opts ...Option) *BaseMessage {`
- `message/base_message.go:124` — `id:      uuid.New().String(),`
- `agentic/user_types.go:36` — `MessageID`
- `processor/agentic-dispatch/http.go:148` — `MessageID:   uuid.New().String(),`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/user_types.go:316` — `SourceMessageID string `json:"source_message_id,omitempty"``
- `processor/agentic-dispatch/task_recovery.go:21` — `const dispatchTaskIDPrefix = "dispatch-"`
- `processor/agentic-dispatch/task_recovery.go:29` — `type vacantDispatchTaskSlot struct {`
- `processor/agentic-dispatch/task_recovery.go:34` — `type retainedTaskEvidenceReader interface {`
- `processor/agentic-dispatch/task_recovery.go:42` — `func (r natsRetainedTaskEvidenceReader) ReadRetainedTask(`
- `processor/agentic-dispatch/task_recovery.go:64` — `func stableDispatchTaskID(msg agentic.UserMessage) string {`
- `processor/agentic-dispatch/task_recovery.go:75` — `func (c *Component) findRetainedDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:79` — `if err := msg.Validate(); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:82` — `taskID := stableDispatchTaskID(msg)`
- `processor/agentic-dispatch/task_recovery.go:103` — `func (c *Component) prepareNewDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:110` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:120` — `func dispatchTaskAddress(ports []component.PortDefinition, taskID string) (string, string, error) {`
- `processor/agentic-dispatch/task_recovery.go:147` — `func (c *Component) readRetainedDispatchTask(`
- `processor/agentic-dispatch/task_recovery.go:170` — `if err := decoded.Validate(); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:183` — `func validateRetainedDispatchTask(`
- `processor/agentic-loop/state.go:180` — `func (m *LoopManager) GenerateLoopID() string {`
- `processor/agentic-loop/state.go:181` — `return uuid.NewString()`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:1131` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1131` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1132` — `return fmt.Sprintf("%s:req:%s", loopID, uuid.NewString())`
- `agentic/types.go:108` — `type AgentRequest struct {`
- `agentic/types.go:109` — `RequestID`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `agentic/types.go:170` — `RequestID`
- `agentic/tools.go:207` — `type ToolCall struct {`
- `agentic/tools.go:212` — `LoopID`
- `agentic/tools.go:214` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:215` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:216` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `agentic/tools.go:629` — `type ToolResult struct {`
- `agentic/tools.go:630` — `CallID      string         `json:"call_id"``
- `agentic/tools.go:637` — `LoopID      string         `json:"loop_id,omitempty"``
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/handlers.go:1250` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls); err != nil {`
- `processor/agentic-loop/handlers.go:1306` — `if err := stampToolExecutionCorrelation(requestID, toolCalls); err != nil {`
- `processor/agentic-loop/handlers.go:1310` — `h.loopManager.TrackToolOrdinal(toolCall.ExecutionID, uint32(index+1))`
- `processor/agentic-loop/handlers.go:1323` — `RequestID:   tc.RequestID,`
- `processor/agentic-loop/handlers.go:1324` — `ExecutionID: tc.ExecutionID,`
- `processor/agentic-loop/handlers.go:1325` — `CallOrdinal: tc.CallOrdinal,`
- `processor/agentic-loop/handlers.go:1367` — `RequestID:   rejection.Call.RequestID,`
- `processor/agentic-loop/handlers.go:1368` — `ExecutionID: rejection.Call.ExecutionID,`
- `processor/agentic-loop/handlers.go:1369` — `CallOrdinal: rejection.Call.CallOrdinal,`
- `processor/agentic-loop/execution_identity.go:13` — `const toolExecutionIdentityVersion = "v1"`
- `processor/agentic-loop/execution_identity.go:15` — `func stampToolExecutionCorrelation(requestID string, calls []agentic.ToolCall) error {`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:25` — `calls[i].CallOrdinal = ordinal`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-loop/execution_identity.go:31` — `func deriveToolExecutionID(requestID, callID string, ordinal uint32) string {`
- `processor/agentic-loop/execution_identity_test.go:16` — `func TestRequestIDsDistinguishLogicalProviderWork(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:29` — `require.NoError(t, err, "RequestID must retain a full collision-resistant request token")`
- `processor/agentic-loop/execution_identity_test.go:34` — `func TestToolExecutionIdentitySeparatesRepeatedProviderCallID(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:53` — `require.Equal(t, first[0].ExecutionID, firstRedelivery[0].ExecutionID, "redelivery retains execution identity")`
- `processor/agentic-loop/execution_identity_test.go:54` — `require.NotEqual(t, first[0].ExecutionID, second[0].ExecutionID)`
- `processor/agentic-loop/execution_identity_test.go:55` — `require.Equal(t, uint32(2), repeatedInRequest[1].CallOrdinal)`
- `processor/agentic-loop/execution_identity_test.go:56` — `require.NotEqual(t, repeatedInRequest[0].ExecutionID, repeatedInRequest[1].ExecutionID)`
- `processor/agentic-loop/execution_identity_test.go:81` — `func TestApprovalRedispatchSeparatesRepeatedProviderCallIDAcrossLoops(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:121` — `require.Equal(t, callsA[0].Name, envelope.Payload.Name)`
- `processor/agentic-loop/execution_identity_test.go:122` — `require.Equal(t, callsA[0].Arguments, envelope.Payload.Arguments)`
- `processor/agentic-loop/execution_identity_test.go:123` — `require.Equal(t, callsA[0].RequestID, envelope.Payload.RequestID)`
- `processor/agentic-loop/execution_identity_test.go:124` — `require.Equal(t, callsA[0].ExecutionID, envelope.Payload.ExecutionID)`
- `processor/agentic-loop/execution_identity_test.go:125` — `require.Equal(t, callsA[0].CallOrdinal, envelope.Payload.CallOrdinal)`
- `processor/agentic-loop/execution_identity_test.go:131` — `func TestGovernanceProposalCarriesFrameworkExecutionCorrelation(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:141` — `require.Equal(t, call.RequestID, payload.RequestID)`
- `processor/agentic-loop/execution_identity_test.go:142` — `require.Equal(t, call.ExecutionID, payload.ExecutionID)`
- `processor/agentic-loop/execution_identity_test.go:143` — `require.Equal(t, call.CallOrdinal, payload.CallOrdinal)`
- `processor/agentic-loop/execution_identity_test.go:144` — `require.NotEmpty(t, payload.ProposalFingerprint)`
- `processor/agentic-loop/execution_identity_test.go:148` — `func TestGovernanceWaitersSeparateRepeatedProviderCallID(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:171` — `require.Len(t, result.Approved, 2)`
- `processor/agentic-loop/state.go:830` — `func (m *LoopManager) TrackToolOrdinal(executionID string, ordinal uint32) {`

### Dispatch publications

- `processor/agentic-dispatch/config.go:144` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Description: "Agent task requests",`
- `processor/agentic-dispatch/config.go:147` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Description: "Agent control signals",`
- `processor/agentic-dispatch/config.go:156` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Description: "Approval responses submitted via the dispatch HTTP /loops/{id}/approval endpoint, consumed by agentic-loop's approval-response handler",`
- `processor/agentic-dispatch/component.go:972` — `prepared, vacant, found, err := c.findRetainedDispatchTask(ctx, msg)`
- `processor/agentic-dispatch/component.go:982` — `if !found && !c.hasPermission(msg.UserID, "submit_task") {`
- `processor/agentic-dispatch/component.go:1020` — `prepared, err = c.prepareNewDispatchTask(ctx, msg, loopID, vacant)`
- `processor/agentic-dispatch/component.go:1055` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/component.go:1057` — `fmt.Sprintf("task publication for loop %s has unknown durable state", loopID))`
- `processor/agentic-dispatch/http.go:302` — `prepared, vacant, found, err := c.findRetainedDispatchTask(ctx, msg)`
- `processor/agentic-dispatch/http.go:309` — `if !found && !c.hasPermission(msg.UserID, "submit_task") {`
- `processor/agentic-dispatch/http.go:348` — `prepared, err = c.prepareNewDispatchTask(ctx, msg, loopID, vacant)`
- `processor/agentic-dispatch/http.go:356` — `taskID := task.TaskID`
- `processor/agentic-dispatch/http.go:375` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-dispatch/task_recovery.go:94` — `if err := validateRetainedDispatchTask(retained, msg, taskID, msg.ReplyTo); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:121` — `subject, err := component.ResolveSubject(ports, "agent.task", taskID)`
- `processor/agentic-dispatch/task_recovery.go:156` — `raw, found, err := reader.ReadRetainedTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:158` — `return agentic.TaskMessage{}, nil, false, errs.WrapTransient(`
- `processor/agentic-dispatch/component.go:974` — `if errs.IsFatal(err) || errs.IsTransient(err) {`
- `processor/agentic-dispatch/task_recovery_test.go:25` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/task_recovery_test.go:26` — `func TestUnreadableRetainedTaskEvidenceDoesNotMintOrRefuse(t *testing.T) {`
- `processor/agentic-dispatch/task_recovery_test.go:40` — `require.True(t, errs.IsTransient(err), "unreadable evidence must retry instead of pretending this is new work")`
- `processor/agentic-dispatch/task_recovery_test.go:42` — `require.Empty(t, prepared.task.LoopID, "a random LoopID requires an exact retained-absence result")`
- `processor/agentic-dispatch/task_recovery_test.go:45` — `require.True(t, errs.IsTransient(err), "the durable source owner must retry an unreadable evidence check")`
- `processor/agentic-dispatch/task_recovery_test.go:46` — `require.Empty(t, sink.all(), "a retryable evidence outage is not a permanent user refusal")`
- `processor/agentic-dispatch/task_recovery_test.go:47` — `require.Empty(t, c.loopTracker.GetAllLoops(), "unreadable evidence must not track newly minted work")`
- `processor/agentic-dispatch/task_recovery_test.go:50` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/task_recovery_test.go:51` — `func TestInvalidUserMessageIdentityIsRejectedBeforeTaskIdentity(t *testing.T) {`
- `processor/agentic-dispatch/task_recovery_test.go:60` — `prepared, vacant, found, err := c.findRetainedDispatchTask(t.Context(), msg)`
- `processor/agentic-dispatch/task_recovery_test.go:64` — `require.Empty(t, vacant.taskID, "invalid source identity must not derive a TaskID")`
- `processor/agentic-dispatch/task_recovery_test.go:65` — `require.Empty(t, prepared.task.LoopID, "invalid source identity must not mint a LoopID")`
- `processor/agentic-dispatch/restart_identity_integration_test.go:212` — `// Mutable AutoContinue state has moved on while the source was waiting for`
- `processor/agentic-dispatch/restart_identity_integration_test.go:221` — `decision, cause = replacementDispatch.handleUserMessage(ctx, secondSource.Data())`
- `processor/agentic-dispatch/restart_identity_integration_test.go:238` — `require.Equal(t, firstTask.LoopID, secondTask.LoopID,`
- `processor/agentic-dispatch/restart_identity_integration_test.go:245` — `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`
- `processor/agentic-dispatch/restart_identity_integration_test.go:246` — `func TestIntegrationUserMessageTaskMappingConflictQuarantines(t *testing.T) {`
- `processor/agentic-dispatch/restart_identity_integration_test.go:265` — `name:   "retained TaskMessage is malformed",`
- `processor/agentic-dispatch/restart_identity_integration_test.go:302` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-dispatch/restart_identity_integration_test.go:309` — `require.Equal(t, uint64(1), info.State.Msgs, "conflict must not publish or overwrite either mapping")`
- `processor/agentic-dispatch/commands.go:157` — `SignalID:    uuid.New().String(),`
- `processor/agentic-dispatch/commands.go:179` — `if err := c.natsClient.Publish(ctx, subject, signalData); err != nil {`
- `processor/agentic-dispatch/http.go:853` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.approval_response", loopID)`
- `processor/agentic-dispatch/http.go:857` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/component.go:1199` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", resp.ChannelType+"."+resp.ChannelID)`
- `processor/agentic-dispatch/component.go:1203` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:17` — `const terminalResponseIDPrefix = "terminal-user-response:"`
- `processor/agentic-dispatch/terminal_settlement.go:146` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`
- `processor/agentic-dispatch/terminal_settlement.go:173` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {`
- `processor/agentic-dispatch/terminal_origin_integration_test.go:130` — `require.Equal(t, terminalResponseIDPrefix+source.ID, msg.Headers().Get("Nats-Msg-Id"))`
- `processor/agentic-dispatch/terminal_settlement_integration_test.go:65` — `require.Equal(t, uint64(1), userInfo.State.Msgs, "stable Nats-Msg-Id must deduplicate a redelivery inside USER window")`

Current dispatch outcome classification: the deterministic terminal-response path is **committed** on nil and
**commit-unknown** on the transport error; the ordinary task, approval-response, and user-response paths have the same
PubAck classification but no deterministic `Nats-Msg-Id` at the cited calls. The core-NATS signal call has no
JetStream PubAck, so its durable-commit classification is **commit-unknown** after invocation.

The HTTP submission path is a second task producer: it separately mints TaskID and framework-minted v4 LoopID,
publishes without Nats-Msg-Id, and returns a synchronous refusal on publish error. It must be included independently
from the durable `user.message` callback path.

### Model publication

- `processor/agentic-model/config.go:134` — `Subjects: []string{"agent.request.>"}, StreamName: "AGENT",`
- `processor/agentic-model/config.go:143` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-model/component.go:1065` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)`
- `processor/agentic-model/component.go:1069` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-model/config.go:147` — `Name: "agent.stream", Config: component.NATSPort{Subject: "agent.stream.*"}, Description: "Streaming delta chunks (core NATS, fire-and-forget)",`

Current required model-response outcome classification: pre-publication failures are **not sent**, nil synchronous
publication is **committed**, and publication error is **commit-unknown**. RequestID is the present response correlation
field; the envelope ID is minted by `NewBaseMessage`.

### Loop publications

- `processor/agentic-loop/config.go:401` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:405` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:409` — `Name: "tool.result", Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:413` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:417` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:421` — `Name: "agent.toolcall.approved", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.approved.>"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:422` — `Description: "Approve verdicts from rule-driven tool-call governance (ADR-039). Wildcard subscription; demuxed by execution_id.",`
- `processor/agentic-loop/config.go:425` — `Name: "agent.toolcall.rejected", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.rejected.>"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:426` — `Description: "Reject verdicts from rule-driven tool-call governance (ADR-039). Wildcard subscription; demuxed by execution_id.",`
- `processor/agentic-loop/config.go:444` — `Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Description: "Agent model requests (JetStream)",`
- `processor/agentic-loop/config.go:447` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.*"}, StreamName: "AGENT"}, Description: "Tool execution requests (JetStream)",`
- `processor/agentic-loop/config.go:450` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Description: "Agent task completions (JetStream)",`
- `processor/agentic-loop/config.go:453` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Description: "Loop-created lifecycle events (JetStream)",`
- `processor/agentic-loop/config.go:456` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Description: "Loop-failed lifecycle events (JetStream)",`
- `processor/agentic-loop/config.go:462` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Description: "Tool calls awaiting human approval (JetStream)",`
- `processor/agentic-loop/config.go:465` — `Name: "agent.toolcall.proposed", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.proposed.*"}, StreamName: "AGENT"}, Description: "Proposed tool calls awaiting rule-driven governance verdict (ADR-039). Emitted in audit and enforce modes.",`
- `processor/agentic-loop/handlers.go:47` — `type PublishedMessage struct {`
- `processor/agentic-loop/handlers.go:48` — `Subject string`
- `processor/agentic-loop/component.go:1589` — `func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string) {`
- `processor/agentic-loop/component.go:1590` — `errorCtx, cancel := natsclient.DetachContextWithTrace(ctx, 5*time.Second)`
- `processor/agentic-loop/component.go:1603` — `c.persistFailureState(errorCtx, loopID, failure)`
- `processor/agentic-loop/component.go:1621` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1622` — `c.logger.Error("Failed to publish failure event", "error", pubErr, "loop_id", loopID)`
- `processor/agentic-loop/component.go:1957` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/handlers.go:1072` — `requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)`
- `processor/agentic-loop/handlers.go:1076` — `createdSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.created", loopID)`
- `processor/agentic-loop/handlers.go:1733` — `toolSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "tool.execute", tc.Name)`
- `processor/agentic-loop/handlers.go:1744` — `CausalOrdinal:     tc.CallOrdinal,`
- `processor/agentic-loop/handlers.go:2193` — `completionSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.complete", loopID)`
- `processor/agentic-loop/handlers.go:2424` — `subject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.approval_pending", loopID)`
- `processor/agentic-loop/handlers.go:2758` — `failureSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.failed", loopID)`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop         map[string]string                   // executionID -> loopID`
- `processor/agentic-loop/handlers.go:1643` — `h.loopManager.TrackToolCall(tc.ExecutionID, loopID)`
- `processor/agentic-loop/state.go:787` — `func (m *LoopManager) TrackToolCall(executionID, loopID string) {`
- `processor/agentic-loop/state.go:790` — `m.toolCallToLoop[executionID] = loopID`
- `processor/agentic-loop/component.go:1883` — `loopID := c.findLoopIDForToolCall(toolResult.ExecutionID)`
- `processor/agentic-loop/component.go:2163` — `// execution ID. Provider CallID is request-scoped conversation data and is`
- `processor/agentic-loop/component.go:2164` — `// never used as a routing fallback.`
- `processor/agentic-loop/component.go:2165` — `func (c *Component) findLoopIDForToolCall(executionID string) string {`
- `processor/agentic-loop/component.go:2166` — `loopID, exists := c.handler.loopManager.GetLoopForToolCallWithRecovery(executionID)`
- `processor/agentic-loop/state.go:1188` — `// execution ID. Durable recovery is added at the committed response/result`
- `processor/agentic-loop/state.go:1189` — `// boundary; provider CallID is never a routing fallback.`
- `processor/agentic-loop/state.go:1190` — `func (m *LoopManager) GetLoopForToolCallWithRecovery(executionID string) (string, bool) {`
- `processor/agentic-loop/state.go:1191` — `return m.GetLoopForToolCall(executionID)`
- `processor/agentic-loop/state.go:934` — `for executionID, r := range entity.PendingToolResults {`
- `processor/agentic-loop/state.go:936` — `delete(m.toolCallToLoop, executionID)`
- `processor/agentic-loop/execution_identity_test.go:60` — `func TestToolResultRoutingSeparatesRepeatedProviderCallIDAcrossLoops(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:75` — `require.Equal(t, loopA, component.findLoopIDForToolCall(callsA[0].ExecutionID))`
- `processor/agentic-loop/execution_identity_test.go:76` — `require.Equal(t, loopB, component.findLoopIDForToolCall(callsB[0].ExecutionID))`
- `processor/agentic-loop/execution_identity_test.go:77` — `require.NotEqual(t, callsA[0].ExecutionID, callsB[0].ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:606` — `subject := "agent.toolcall.proposed." + loopID`
- `processor/agentic-loop/governance_dispatcher.go:607` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:115` — `RequestID           string         `json:"request_id"``
- `processor/agentic-loop/governance_dispatcher.go:116` — `ExecutionID         string         `json:"execution_id"``
- `processor/agentic-loop/governance_dispatcher.go:118` — `CallOrdinal         uint32         `json:"call_ordinal"``
- `processor/agentic-loop/governance_dispatcher.go:119` — `ProposalFingerprint string         `json:"proposal_fingerprint"``
- `processor/agentic-loop/governance_dispatcher.go:150` — `func (v VerdictPayload) effectiveExecutionID() string {`
- `processor/agentic-loop/governance_dispatcher.go:401` — `channels[call.ExecutionID] = d.registerWaiter(call.ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:437` — `decision, reason := d.awaitVerdict(ctx, channels[call.ExecutionID], call, loopID)`
- `processor/agentic-loop/governance_dispatcher.go:491` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:506` — `ch, ok := d.lookupWaiter(executionID)`
- `processor/agentic-loop/governance_dispatcher.go:548` — `if call.RequestID == "" || call.ExecutionID == "" || call.CallOrdinal == 0 {`
- `processor/agentic-loop/governance_dispatcher.go:567` — `RequestID:    call.RequestID,`
- `processor/agentic-loop/governance_dispatcher.go:568` — `ExecutionID:  call.ExecutionID,`
- `processor/agentic-loop/governance_dispatcher.go:570` — `CallOrdinal:  call.CallOrdinal,`
- `processor/agentic-loop/governance_dispatcher.go:590` — `payload.ProposalFingerprint = fingerprint`
- `processor/agentic-loop/governance_dispatcher.go:613` — `func fingerprintProposedToolCall(payload ProposedToolCallPayload) (string, error) {`
- `processor/agentic-loop/component.go:2323` — `executionID := payload.effectiveExecutionID()`
- `processor/agentic-loop/component.go:2329` — `return dispatcher.HandleVerdict(decision, executionID, data)`
- `processor/agentic-loop/component.go:2381` — `if v, ok := data["request_id"].(string); ok {`
- `processor/agentic-loop/component.go:2384` — `if v, ok := data["execution_id"].(string); ok {`
- `processor/agentic-loop/component.go:2387` — `if v, ok := data["proposal_fingerprint"].(string); ok {`
- `processor/rule/actions.go:2146` — `for _, field := range []string{"request_id", "execution_id", "proposal_fingerprint"} {`
- `processor/rule/actions_test.go:3345` — `Subject: "agent.toolcall.approved.$message.execution_id",`
- `processor/rule/actions_test.go:3354` — `assert.Equal(t, "agent.toolcall.approved.execution-001", got.subject,`
- `processor/rule/actions_test.go:3373` — `assert.Equal(t, "request-001", envelope.Payload.Data["request_id"])`
- `processor/rule/actions_test.go:3374` — `assert.Equal(t, "execution-001", envelope.Payload.Data["execution_id"])`
- `processor/rule/actions_test.go:3375` — `assert.Equal(t, "sha256:proposal", envelope.Payload.Data["proposal_fingerprint"])`
- `agentic/state.go:147` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/state.go:148` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/state.go:150` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/handlers.go:2395` — `entity.PendingApproval.RequestID = toolResult.RequestID`
- `processor/agentic-loop/handlers.go:2396` — `entity.PendingApproval.ExecutionID = toolResult.ExecutionID`
- `processor/agentic-loop/handlers.go:2397` — `entity.PendingApproval.CallOrdinal = toolResult.CallOrdinal`
- `processor/agentic-loop/approval_response_handler.go:121` — `RequestID:   pending.RequestID,`
- `processor/agentic-loop/approval_response_handler.go:122` — `ExecutionID: pending.ExecutionID,`
- `processor/agentic-loop/approval_response_handler.go:123` — `CallOrdinal: pending.CallOrdinal,`
- `processor/agentic-loop/approval_integration_test.go:292` — `original := dispatchedCalls[0]`
- `processor/agentic-loop/approval_integration_test.go:295` — `assert.Equal(t, original.RequestID, approved.RequestID, "re-dispatch must preserve request identity")`
- `processor/agentic-loop/approval_integration_test.go:296` — `assert.Equal(t, original.ExecutionID, approved.ExecutionID, "re-dispatch must preserve execution identity")`
- `processor/agentic-loop/approval_integration_test.go:297` — `assert.Equal(t, original.CallOrdinal, approved.CallOrdinal, "re-dispatch must preserve call ordinal")`
- `processor/agentic-loop/approval_integration_test.go:315` — `assert.Equal(t, approved.RequestID, terminal.RequestID)`
- `processor/agentic-loop/approval_integration_test.go:316` — `assert.Equal(t, approved.ExecutionID, terminal.ExecutionID)`
- `processor/agentic-loop/approval_integration_test.go:317` — `assert.Equal(t, approved.CallOrdinal, terminal.CallOrdinal)`
- `processor/agentic-loop/approval_sweeper.go:151` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/component.go:2263` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`

Current loop outcome classification: every cited synchronous stream call is **committed** on nil and
**commit-unknown** on error; unresolved subject, payload, or source state before a call is **not sent**. `PublishedMessage`
has subject/data fields and no message-ID field. The sequence publisher does not attach a deterministic Msg-Id.

The direct terminal-failure fanout is a separate required-output path. It detaches for five seconds, ignores
persistence return state, loops over multiple publications, and logs publication errors. It currently cannot prove
the terminal output committed and is in task-2 identity/reconciliation scope as well as task-10 lifecycle scope.

### Governance publications

- `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:193` — `Name: "request_validation", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:197` — `Name: "response_validation", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:204` — `Name: "agent.task.validated", Config: component.JetStreamPort{Subjects: []string{"agent.task.validated.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:208` — `Name: "agent.request.validated", Config: component.JetStreamPort{Subjects: []string{"agent.request.validated.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:212` — `Name: "agent.response.validated", Config: component.JetStreamPort{Subjects: []string{"agent.response.validated.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/component.go:432` — `outputSubject, resolveErr := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`

Current governance outcome classification: a blocked policy outcome performs deliberate non-publication (**not sent**);
an allowed output is **committed** on nil synchronous publication and **commit-unknown** on publication error. The
subject suffix uses source envelope ID; no deterministic Msg-Id call or exact validated-output lookup is present.

### Tool publication and completed-outcome reconciliation

- `processor/agentic-tools/config.go:127` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-tools/config.go:142` — `Name: "tool.result", Config: component.JetStreamPort{Subjects: []string{"tool.result.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-tools/README.md:106` — `An effectful executor must use `ToolCall.ExecutionID` as the framework identity supplied to any downstream`
- `processor/agentic-tools/README.md:107` — `idempotency contract. The provider's `ToolCall.ID` is request-scoped conversation data and may repeat under another`
- `processor/agentic-tools/README.md:115` — `executor. Reusing one execution ID for different canonical call content is a permanent collision and is terminated;`
- `processor/agentic-tools/README.md:116` — `reusing a provider CallID under a different RequestID produces a different execution ID.`
- `processor/agentic-tools/doc.go:243` — `//  6. Publish ToolResult to tool.result.{execution_id}`
- `processor/agentic-tools/doc.go:271` — `// Every wire response remains correlated to its request, execution, and`
- `processor/agentic-tools/doc.go:273` — `// coordination: it creates no COMPLETED outcome and leaves the same execution`
- `processor/agentic-tools/outcomes.go:27` — `ExecutionID string             `json:"execution_id"``
- `processor/agentic-tools/outcomes.go:28` — `RequestID   string             `json:"request_id"``
- `processor/agentic-tools/outcomes.go:29` — `CallID      string             `json:"call_id"``
- `processor/agentic-tools/outcomes.go:30` — `CallOrdinal uint32             `json:"call_ordinal"``
- `processor/agentic-tools/outcomes.go:82` — `func toolCallOutcomeKey(executionID string) string {`
- `processor/agentic-tools/outcomes.go:86` — `func toolResultMessageID(executionID string) string {`
- `processor/agentic-tools/outcomes.go:90` — `func toolApprovalRequiredMessageID(executionID string) string {`
- `processor/agentic-tools/outcomes.go:118` — `func newCompletedOutcome(call agentic.ToolCall, result agentic.ToolResult) (completedOutcome, error) {`
- `processor/agentic-tools/outcomes.go:129` — `if result.RequestID != call.RequestID || result.ExecutionID != call.ExecutionID || result.CallOrdinal != call.CallOrdinal {`
- `processor/agentic-tools/outcomes.go:138` — `func validateToolExecutionCorrelation(call agentic.ToolCall) error {`
- `processor/agentic-tools/outcomes.go:151` — `func correlateToolResult(call agentic.ToolCall, result agentic.ToolResult) agentic.ToolResult {`
- `processor/agentic-tools/outcomes.go:177` — `if outcome.ExecutionID != call.ExecutionID {`
- `processor/agentic-tools/outcomes.go:180` — `if outcome.RequestID != call.RequestID {`
- `processor/agentic-tools/outcomes.go:186` — `if outcome.CallOrdinal != call.CallOrdinal {`
- `processor/agentic-tools/component.go:697` — `if err := validateToolExecutionCorrelation(call); err != nil {`
- `processor/agentic-tools/component.go:750` — `err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ExecutionID))`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:852` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ExecutionID), data)`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-tools/component.go:1187` — `func (c *Component) publishResultWithMsgID(ctx context.Context, result agentic.ToolResult, msgID string) error {`
- `processor/agentic-tools/component.go:1196` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "tool.result", result.ExecutionID)`
- `processor/agentic-tools/outcomes_test.go:272` — `assert.Equal(t, toolResultMessageID(call.ExecutionID), observedMsgID)`
- `processor/agentic-tools/outcomes_test.go:522` — `assert.Equal(t, []string{toolResultMessageID(call.ExecutionID), toolResultMessageID(call.ExecutionID)}, msgIDs)`
- `processor/agentic-tools/execution_identity_test.go:11` — `func TestCompletedOutcomeIdentitySeparatesRepeatedProviderCallID(t *testing.T) {`
- `processor/agentic-tools/execution_identity_test.go:20` — `require.NotEqual(t, toolCallOutcomeKey(first.ExecutionID), toolCallOutcomeKey(second.ExecutionID))`
- `processor/agentic-tools/execution_identity_test.go:35` — `require.Error(t, err, "a different logical request cannot replay this outcome")`
- `processor/agentic-tools/execution_identity_test.go:39` — `func TestHostedToolResultCorrelationIsFrameworkStamped(t *testing.T) {`
- `processor/agentic-tools/execution_identity_test.go:49` — `require.Equal(t, call.ID, result.CallID)`
- `processor/agentic-tools/execution_identity_test.go:50` — `require.Equal(t, call.RequestID, result.RequestID)`
- `processor/agentic-tools/execution_identity_test.go:51` — `require.Equal(t, call.ExecutionID, result.ExecutionID)`
- `processor/agentic-tools/execution_identity_test.go:52` — `require.Equal(t, call.CallOrdinal, result.CallOrdinal)`

Current tool outcome classification: exact KV miss leaves no completed-outcome authority; a matching immutable entry
is replayed without executor work; a mismatching fingerprint is a collision before overwrite; successful result
publication is **committed**; result publication error is **commit-unknown** while the cited immutable entry remains
available for exact replay. The key and Msg-Id derive from framework ExecutionID.

### Exact lookup and collision census

- `processor/agentic-loop/create_vs_exists_fence_test.go:76` — `// TestCreateLoopWithIDRefusesExistingTokenWithoutMutation is I7: a refused`
- `processor/agentic-loop/create_vs_exists_fence_test.go:102` — `_, err = lm.CreateLoopWithID(loopID, "task-second", "reviewer", "model-b", 3)`
- `processor/agentic-tools/outcomes_integration_test.go:97` — `entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/outcomes_integration_test.go:102` — `assert.Equal(t, call.RequestID, outcome.Result.RequestID)`
- `processor/agentic-tools/outcomes_integration_test.go:103` — `assert.Equal(t, call.ExecutionID, outcome.Result.ExecutionID)`
- `processor/agentic-tools/outcomes_integration_test.go:104` — `assert.Equal(t, call.CallOrdinal, outcome.Result.CallOrdinal)`
- `processor/agentic-tools/outcomes_integration_test.go:311` — `entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/outcomes_integration_test.go:312` — `require.NoError(t, err, "COMPLETED must precede the failed result publication")`
- `processor/agentic-tools/outcomes_integration_test.go:315` — `require.Equal(t, "executed", authority.Result.Content)`
- `processor/agentic-tools/outcomes_test.go:374` — `key := toolCallOutcomeKey(call.ExecutionID)`
- `processor/agentic-tools/outcomes_test.go:375` — `assert.Equal(t, "v1."+strings.TrimPrefix(toolResultMessageID(call.ExecutionID), "tool-result/v1/"), key)`

The production exact-stream-lookup search for `GetLastMsgForSubject`, `GetMsg`, `GetMessage`, and `DirectGet` in the
five scoped components returned zero. Exact current reconciliation was located for completed tool outcomes through
`TOOL_CALL_OUTCOMES`; exact committed-output lookups were not located for requests, responses, validated governance
outputs, proposals, verdicts, or terminal outputs.

### Interaction with merged identity changes #1168 and #1192

- `docs/adr/104-unique-platform-authority.md:28` — `is unique by default.`
- `docs/adr/104-unique-platform-authority.md:29` — `entropy suffix from`
- `docs/operations/migration-beta162-to-beta163.md:738` — `Unique platform authority (ADR-104, #1168)`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:54` — `1. **A loop instance token is a framework-minted v4 UUID**, carried in canonical RFC 4122 text form (36 bytes,`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:94` — `An adopter conforms when it treats every loop token as an opaque value it received from the framework — echoed in`
- `docs/operations/migration-beta162-to-beta163.md:895` — `Echo, never author.`
- `processor/agentic-loop/state.go:180` — `func (m *LoopManager) GenerateLoopID() string {`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`

The #1168 authority pins describe entity-ID platform authority. The scoped task/request/execution/output identity fields
do not read `platform.id`. The #1192 pins describe canonical framework-minted loop tokens; current dispatch and loop
mint full UUID values, TaskID remains independently random, and RequestID now retains a full UUID suffix.
The frozen parent already contains these ADR-104/ADR-105 surfaces; `git diff` from the frozen parent to base contains no
task-2 identity implementation.

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md:414` — `server dedupe within the stream's duplicate window. Semantic reconciliation is load-bearing after longer downtime.`
- `openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md:1370` — `- Governance deterministic output fingerprint and exact committed-output lookup are target state, not current truth.`
- `openspec/specs/nats-streaming/spec.md:22` — `### Requirement: Synchronous stream publish blocks on the PubAck`
- `docs/operations/migration-gated-dag-semantic-settlement.md:34` — `The dispatch producer uses the logical unit ID as`
- #759 — agentic consumer settlement contract and stacked PR #1156.
- #807 — stable-identity agentic issue returned by the open-issue search.
- #839 — reconciliation issue returned by the open-issue search.
- #857 — Nats-Msg-Id/PubAck issue returned by the open-issue search.
- #1133 — reconciliation issue returned by the open-issue search.
- #1143 — PubAck issue returned by the open-issue search.
- #1145 — reconciliation issue returned by the open-issue search.
- #1146 — this restart-safety change; stacked draft PR #1159.
- #1147 — stable identity, reconciliation, and PubAck issue returned by the open-issue searches.
- #1167 — reconciliation issue returned by the open-issue search.
- semteams: `cmd/semteams/chainpause/decision_handler.go:230` authors `agentic.TaskMessage`; line 345 publishes it through `PublishToStream`.
- semteams: `cmd/semteams/commands/teamhint/command.go:97` publishes through `PublishToStreamWithAck`.
- semdev: `internal/intake/coordinatortask.go:64` authors `agentic.TaskMessage`; intake lines 486 and 622 use `PublishToStreamWithMsgID` for its own front-door deliveries.
- semspec: `processor/lesson-decomposer/component.go:568`, `processor/qa-reviewer/component.go:544`, `processor/researcher-manager/component.go:442`, and `tools/question/executor.go:389` author `agentic.TaskMessage`; their corresponding publication calls are lines 607, 588, 477, and 407.
- semdragon: `processor/bossbattle/evaluator.go:288` and `processor/executor/executor.go:201` author `agentic.AgentRequest` directly.
- semdragon: `processor/questtools/explore.go:85-93` authors TaskMessage with TaskID from quest ID and a supplied
  LoopID; `:138-142` constructs `agent.task.<loop>` and uses generic `PublishToStream`.
- semdragon: `processor/questbridge/handler.go:317-320` authors a non-UUID `quest-...-<nuid>` LoopID and TaskID;
  `:362-370` constructs the task subject and publishes with generic `PublishToStream`.
- semdragon: `processor/questdagexec/handler.go:321-327`, `:360-365` authors/publishes review TaskMessage;
  `:408-415`, `:447-452` authors/publishes clarification TaskMessage; `:743-749`, `:778-785` authors/publishes
  synthesis TaskMessage. All author non-UUID loop tokens, build subjects, and use generic stream publication.
- semsource: no direct agentic task/request/response publication was returned by the focused search; its matched stream publishers are graph/source-manifest surfaces.
- semsage: `tools/spawn/executor.go:176-177`, `:211-231` and `processor/ui-api/http.go:420-440` author UUID
  LoopID/TaskID values and publish TaskMessage through generic `PublishToStream`; semsage pins SemStreams alpha.3.
- semmachina: `internal/persona/spec.go:397-412` authors deterministic TaskIDs; `internal/stage/spawn.go:313-321`
  and `internal/stage/companion.go:348-354` publish with TaskID as Nats-Msg-Id. It pins SemStreams beta.160 and is a
  same-class downstream identity/idempotency owner.
- semops, semmem, semconnect: the focused search returned zero matches.
- semboids: no task-2 agentic publisher; the only focused production match is unrelated zone ingest at
  `internal/zone/ingest.go:19,37`.
- semlink: no task-2 agentic publisher; the only focused production match is unrelated client publication at
  `internal/semstreams/client.go:382`.
- checked-out `semspec-ui-bmad` and `semspec-ui-run-visibility` share semspec's common git directory and are not
  independent sister repositories; the denominator counts semspec once.

Semdragon has six framework-relevant task-publication seams across questtools, questbridge, and questdagexec. They
currently author LoopID despite ADR-105's echo-never-author contract, build `agent.task` subjects, and publish without
Nats-Msg-Id. Task 2 cannot call sisters unaffected: SemStreams must publish an exact migration list, while each sister
owner performs its own code migration. This strengthens the seam finding that adopters should not calculate loop
tokens, subjects, or transport idempotency. It also exposes a pre-existing ADR-105 conformance defect; do not roll it
silently into a universal ExecutionID.

ADR-105 migration impact also reaches semsage spawn. `tools/spawn/executor.go:176-199,211-231` authors the child
LoopID, subscribes to exact `agent.complete/failed.<loopID>` subjects before publication, and places that LoopID in
TaskMessage. This is a coupled mint-plus-terminal-subscription migration, not only a field rewrite. The separate
`processor/ui-api/http.go:420-440` producer omits LoopID and remains a distinct TaskID/subject/generic-publish seam.

## Consumers

### Structural references

- `processor/agentic-dispatch/component.go:1055` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-model/component.go:1069` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/component.go:1957` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `agentic/events.go:14` — `TaskID`
- `agentic/events.go:13` — `LoopID`
- `agentic/trajectory.go:12` — `RequestID`
- `agentic/reasoning.go:46` — `ToolCallID`

`gopls workspace_symbol` returned 21 TaskID symbols, 100 LoopID symbols, 53 RequestID symbols, zero ExecutionID
symbols, 22 PublishToStream symbols, and one PublishToStreamWithMsgID symbol. `gopls references` for the shared
`PublishToStream` declaration returned 35 reference rows; the scoped production callers are pinned in the five
publication-family sections above. The `PublishedMessage` loop container is consumed by the sequence publisher at
`processor/agentic-loop/component.go:1956`.

### Subject consumers and producers

- `processor/agentic-dispatch/config.go:122` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-dispatch/config.go:126` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-dispatch/config.go:130` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-dispatch/config.go:134` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-model/config.go:134` — `Subjects: []string{"agent.request.>"}, StreamName: "AGENT",`
- `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:193` — `Name: "request_validation", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:197` — `Name: "response_validation", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-tools/config.go:127` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.>"}, StreamName: "AGENT"}, Required: true,`

## Problem shape

- `processor/gated-dag/publisher.go:29` — `// The Nats-Msg-Id is the unitID, so within the stream's Duplicates window the`
- `processor/gated-dag/publisher.go:44` — `if err := p.nc.PublishToStreamWithMsgID(ctx, p.subject, data, unitID); err != nil {`
- `docs/operations/migration-gated-dag-semantic-settlement.md:34` — `The dispatch producer uses the logical unit ID as`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:852` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ExecutionID), data)`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-loop/create_vs_exists_fence_test.go:76` — `// TestCreateLoopWithIDRefusesExistingTokenWithoutMutation is I7: a refused`

## Same-class collision table

| Dimension | Inventory evidence |
|---|---|
| Semantic class | Proof that one required logical output committed with the expected canonical content, so replay may avoid duplicate work. |
| Owners | JetStream/PubAck and retained AGENT messages (`natsclient/client.go:942`, `natsclient/client.go:1005`); each publishing component's operation identity/fingerprint; immutable tool completion (`processor/agentic-tools/outcomes.go:27`, `:79`, `:83`); current loop state in AGENT_LOOPS (`processor/agentic-loop/config.go:438`); in-repo gated-DAG logical unit identity (`processor/gated-dag/publisher.go:29,44`); downstream semmachina persona/stage identity owners (`internal/persona/spec.go:397-412`, `internal/stage/spawn.go:313-321`, `internal/stage/companion.go:348-354`). |
| Catalogs | Component port declarations name AGENT subjects/stream (`processor/agentic-dispatch/config.go:144`, `processor/agentic-model/config.go:143`, `processor/agentic-loop/config.go:444`, `processor/agentic-governance/config.go:204`, `processor/agentic-tools/config.go:142`); TOOL_CALL_OUTCOMES is centrally cataloged by current spec. |
| Status | PubAck return/error and existing component health are observable. Focused production search found no output-specific reconciliation status or registry. |
| Lifecycle | JetStream retention and duplicate window bound retained proof (`natsclient/client.go:957-961`); TOOL_CALL_OUTCOMES has immutable Create/read collision behavior (`processor/agentic-tools/component.go:813`, `:860`); AGENT_LOOPS is current state, not an output ledger. |
| Ownership | The producing capability owns canonical identity/content validation; JetStream owns storage/retention; agentic-tools alone owns TOOL_CALL_OUTCOMES. No universal reconciliation owner was found. |
| Readers | Scoped components publish; tests use raw stream exact reads (`processor/agentic-tools/outcomes_integration_test.go:319`, `:375`); production exact reads exist only for completed tool outcomes. No separate semmachina reconciliation reader was located; its stage path is producer-only evidence. |
| Writers | Dispatch/model/loop/governance/tools publication pins above; agentic-tools alone Create-CASes completed outcomes; gated-DAG writes logical-unit-ID as Nats-Msg-Id (`processor/gated-dag/publisher.go:29,44`); semmachina stage writers use deterministic TaskID as Nats-Msg-Id at the cited spawn/companion paths. |
| Recovery | Nats-Msg-Id dedup is bounded (`natsclient/client.go:947-961`); gated-DAG and semmachina use that server-window dedupe; neither proves post-window exact committed-output reconciliation. Completed tool replay avoids executor work; other required outputs have no production exact committed-output lookup. |

## Adopter seam inventory

| External person | What they must know today | If they do nothing | Discovery | What they should have to know |
|---|---|---|---|---|
| Sister-repo TaskMessage producer | Author TaskID; preserve opaque framework-minted LoopID when continuing; choose the correct subject/publisher; decide a retry-stable message ID | Current generic PublishToStream plus newly random IDs can duplicate logical tasks after an uncertain publish | Usually runtime behavior or docs; no compile-time guard | Its own stable source-operation identity and task content; not subject spelling, stream retention, deadline, or transport commit state |
| Raw external AgentRequest/ToolCall producer | Preserve RequestID/provider CallID/ordinal correlation and construct matching subjects | Repeated provider IDs can collide across turns or exact replay is unavailable | Payload validation where present, otherwise runtime mismatch | Conversation data only; framework should derive operation identities and publications |
| Component developer inside another product | Select PublishToStream vs PublishToStreamWithMsgID and invent a deterministic Nats-Msg-Id | Server dedupe is absent or only accidentally stable; commit-unknown retry may duplicate | Documentation and integration failures | Call an operation-specific typed publication seam or supply one stable logical operation key; never predict stream state |
| Operator | Configure stream retention/duplicate policy | A retained exact lookup can become unavailable; absence may be misclassified | Boot/runtime only where admission exists | Configure capacity/retention once; components observe actual policy and fail closed, without adopter-computed horizons |

Every publisher-facing row carries more than two correctness facts. The current broad design risks exporting identity,
subject, fingerprint, and transport-state arithmetic rather than absorbing it behind operation-specific component
behavior.

## Searches

- `git rev-parse HEAD` → 1 (`79b0f29f82ce5391013f6c931fae69a28216ac93`)
- `git rev-parse 417beae5552f8f15ad3540edd7d8504c87174c13` → 1
- `git status --short` → 0
- `sed -n '3,65p' openspec/project.md` → 63 lines
- `git grep -n -E '2\\.1|2\\.2|2\\.3|stable identity|exact reconciliation' -- openspec/changes/agentic-loop-restart-safety` → 123
- `git grep -n -E 'deterministic|identity|reconcil|collision|fingerprint|Nats-Msg-Id|PubAck' -- openspec/changes/agentic-loop-restart-safety/specs openspec/changes/agentic-loop-restart-safety/design.md` → 447
- `git grep -n -E 'PublishToStream|PublishToStreamWithMsgID|PublishToStreamWithAck|PublishMsg|Conn.Publish' -- agentic processor/agentic-* natsclient` → 278
- `git grep -n -E 'agent\\.(task|signal|approval_response|response|request|complete|failed|created|approval_pending|toolcall)|tool\\.(execute|result)'` → 1216
- `git grep -n -E 'TaskID|LoopID|RequestID|ExecutionID|CallID|MessageID|ResponseID|SignalID' -- agentic processor/agentic-* message` → 520
- `git grep -n -E 'GetLastMsgForSubject|GetMsg|GetMessage|DirectGet|outcome|reconcil|fingerprint|collision' -- agentic processor/agentic-* natsclient` → 232
- `git grep -n -E 'NewBaseMessage|uuid.New|GenerateLoopID|GenerateRequestID|toolResultMessageID|toolCallOutcomeKey' -- agentic message processor/agentic-*` → 240
- `git grep -n -E 'deterministic|dedup|duplicate|collision|replay|unknown durable state|PubAck|Nats-Msg-Id' -- '*_test.go'` → 427
- `git grep -n -E 'gated-dag|TOOL_CALL_OUTCOMES|semantic reconciliation|duplicate window' -- processor docs openspec` → 59
- `git grep -n -E 'TaskID|LoopID|RequestID|CallID|MessageID|Nats-Msg-Id' -- docs openspec/specs openspec/changes/agentic-loop-restart-safety` → 30
- `git grep -n -E 'func .*Publish|PublishToStream|PublishMsg' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools natsclient` → 179
- `git grep -n -E 'GetLastMsgForSubject|GetMsg\\(|GetMessage\\(|DirectGet' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 0
- `git grep -n -E 'Nats-Msg-Id|PublishToStreamWithMsgID' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 7
- `gopls workspace_symbol TaskID` → initial sandbox run failed before results; escalated rerun → 21
- `gopls workspace_symbol LoopID` → initial sandbox run failed before results; escalated rerun → 100
- `gopls workspace_symbol RequestID` → initial sandbox run failed before results; escalated rerun → 53
- `gopls workspace_symbol ExecutionID` → initial sandbox run failed before results; escalated rerun → 0
- `gopls workspace_symbol PublishToStream` → initial sandbox run failed before results; escalated rerun → 22
- `gopls workspace_symbol PublishToStreamWithMsgID` → initial sandbox run failed before results; escalated rerun → 1
- `gopls references natsclient/client.go:942:18` → 35
- `gopls call_hierarchy processor/agentic-model/component.go:1059:21` → 143 callers to the selected `NewBaseMessage` symbol (selection did not identify the publish call)
- `gopls call_hierarchy processor/agentic-loop/component.go:1950:21` → 3 callers, 4 callees
- `gopls call_hierarchy processor/agentic-tools/component.go:1195:21` → 2 callers, 10 callees
- `git grep -n -E '// spec: agentic-(dispatch|model|loop|governance|tools) / .*([Ii]dentity|reconcil|duplicate|collision|Nats-Msg-Id|PubAck)' -- '*.go'` → 0
- `git grep -n -E 'PublishToStream(WithAck|WithMsgID)?\\(|TaskMessage\\{|AgentRequest\\{|AgentResponse\\{|ToolCall\\{|ToolResult\\{' -- '*.go'` in semteams → more than 80; output capped at 80
- same focused sister-repository search in semsource → 9
- same focused sister-repository search in semdev → more than 80; output capped at 80
- same focused sister-repository search in semdragon → more than 80; output capped at 80
- same focused sister-repository search in semops → 0
- same focused sister-repository search in semspec → more than 80; output capped at 80
- same focused sister-repository search in semmem → 0
- same focused sister-repository search in semconnect → 0
- `gh issue list --search 'stable identity agentic' --state open --json number,title` → 4
- `gh issue list --search 'Nats-Msg-Id agentic' --state open --json number,title` → 2
- `gh issue list --search 'reconciliation agentic' --state open --json number,title` → 9
- `gh issue list --search 'PubAck agentic' --state open --json number,title` → 5
- `gh pr list --state open --json number,title,body` → 4 open PRs; #1159 and #1156 matched the task-2 stack
- `openspec list` → `agentic-loop-restart-safety` 17/65 tasks and `agentic-loop-semantic-settlement` 44/67 tasks
- `git log --oneline --all --grep='#1168\\|#1192\\|#1210\\|#1178'` → 4 relevant commits
- `git diff --name-only 417beae5552f8f15ad3540edd7d8504c87174c13..79b0f29f82ce5391013f6c931fae69a28216ac93` → 43 paths; no task-2 identity implementation path located
- `git grep -n -E 'source UserMessage|source MessageID|framework stamps|Every required dispatch|operation-specific exact|platform\\.id|entropy|v4 UUID|full canonical|loop token|minting authority|duplicate window|semantic reconciliation|exact committed|GetLastMsgForSubject|DirectGet|GetMsg\\(' -- openspec/changes/agentic-loop-restart-safety docs/adr docs/operations/migration-beta162-to-beta163.md openspec/specs/nats-streaming/spec.md processor/gated-dag docs/operations/migration-gated-dag-semantic-settlement.md` → 64
- `git grep -n -E 'func \\(c \\*Client\\) PublishToStream|func \\(c \\*Client\\) PublishToStreamWithMsgID|js\\.PublishMsg|Nats-Msg-Id|func NewBaseMessage|func GenerateLoopID|func GenerateRequestID|uuid\\.New\\(\\)\\.String\\(\\)|type PublishedMessage struct|PublishToStream\\(ctx|PublishToStreamWithMsgID\\(ctx|toolCallOutcomeKey|toolResultMessageID|requestFingerprint|Create\\(ctx,.*outcome|Get\\(ctx,.*outcome|RequestID[[:space:]]+string|CallID[[:space:]]+string|TrackToolOrdinal|sourceMessageID|ResponseID:.*terminal|ResolveSubject\\(|unknown durable state|agent\\.toolcall\\.proposed' -- natsclient message agentic processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools internal/agentterminal` → more than 260; output capped at 260
- `git grep -n -E 'func \\(m \\*LoopManager\\) (CreateLoopWithID|GenerateLoopID|GenerateRequestID)|func \\(m \\*LoopManager\\) CreateLoop|return uuid.New\\(\\)\\.String\\(\\)|return fmt.Sprintf\\("%s:req|type ToolCall struct|type ToolResult struct|type AgentRequest struct|type AgentResponse struct|TaskID[[:space:]]+string|LoopID[[:space:]]+string|MessageID[[:space:]]+string|Subject:.*agent\\.|Subjects:.*agent\\.|StreamName: "AGENT"|terminalResponseIDPrefix|func \\(c \\*Component\\) publishResultWithMsgID|func fingerprintToolCall|func decodeCompletedOutcome|CreateLoopWithID' -- agentic processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools` → more than 280; output capped at 280
- `git grep -n -E 'PublishToStream.*agent.task|taskID := uuid.New|ResolveSubject.*agent.task' -- processor/agentic-dispatch/http.go` → HTTP task producer located at 344/362/384
- `git grep -n -E 'publishFailureEvents|DetachContextWithTrace|persistFailureState|Failed to publish failure event' -- processor/agentic-loop/component.go` → direct failure fanout located at 1589-1622
- `git grep -n -E 'reconciliation.*(metric|health|status)|committed.output.*(metric|health|status)|duplicate.*(metric|health|status)' -- processor/agentic-* natsclient ':!*_test.go'` → 0 relevant output-specific reconciliation-status owners
- focused `rg -n 'agentic\\.TaskMessage|TaskMessage|PublishToStream'` over semdragon `processor/questtools`,
  `processor/questbridge`, and `processor/questdagexec` → production authors/publishers at the cited paths; the prior
  statement that semdragon only directly authors AgentRequest was incomplete
- focused `rg -n 'agentic\\.TaskMessage|TaskMessage|PublishToStream|PublishToStreamWithMsgID'` over semsage
  `tools/spawn` and `processor/ui-api` → TaskMessage authors/publishers at the cited paths
- focused `rg -n 'TaskID|PublishToStreamWithMsgID|Nats-Msg-Id'` over semmachina `internal/persona` and
  `internal/stage` → deterministic TaskID author plus TaskID-as-Nats-Msg-Id publishers at the cited paths
- focused `rg -n 'agentic\\.TaskMessage|TaskMessage|AgentRequest|AgentResponse|ToolCall|ToolResult|PublishToStream'`
  over semboids production Go paths → zero task-2 publishers; only unrelated zone ingest at the cited path
- same focused search over semlink production Go paths → zero task-2 publishers; only unrelated client publication
  at the cited path
- enumerate root modules from `/Users/coby/Code/c360/*/go.mod`, then select imports of
  `github.com/c360studio/semstreams` → independent roots: semboids, semconnect, semdev, semdragon, semlink,
  semmachina, semmem, semops, semsage, semsource, semspec, and semteams; semstreams itself excluded and semspec UI
  paths counted as shared-git-dir worktrees

### Refresh searches (2026-09-04)

- `git grep -n '^## \(Purpose\|Product Boundary\)' -- openspec/project.md` → 2
- `rg -n 'tasks\.md:(75|82|83|84|86)|design\.md:(129|130|131|132|134|136)|agentic-dispatch/spec\.md:109|agentic-model/spec\.md:203|agentic-loop/spec\.md:151|agentic-governance/spec\.md:75|agentic-tools/spec\.md:49' openspec/changes/agentic-loop-restart-safety/inventory-task2-stable-identity-2026-09-03.md` → 16
- `git grep -n -E 'Approval continuation and dispatch projection|Approval evidence|projection|AutoContinue|incomplete hydration|explicit LoopID|Loop task, request|lane-scoped|at-least-once|edge gateway|agent.task|TaskID' -- openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → 113
- `git grep -n -E 'frozen-parent|Frozen parent|F =|F=|79b0f29|417beae|base:' -- openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/inventory-dispatch-bridge-boundary-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task-loop-cardinality-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task2-stable-identity-2026-09-03.md` → 6
- `shasum -a 256 openspec/changes/agentic-loop-restart-safety/design-dispatch-edge-gateway-2026-09-04.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/tasks.md` → 4
- `git grep -n -E 'pins=.*moved=.*ambiguous|inventory.*verif|malformed.*unparsed' -- . ':!openspec/changes/agentic-loop-restart-safety/inventory-*.md'` → 22
