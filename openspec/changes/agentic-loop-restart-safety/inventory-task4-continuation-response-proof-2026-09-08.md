# Inventory: task 4 continuation and response application proof

base: af829616305afa039dac0550efa78d07e856dd5f

## Problem statement

Task 4 currently persists a continuation's new `LoopEntity.TaskID` before the continuation's `AgentRequest` receives
PubAck. Process replacement removes `pendingTaskResults`; the exact retained request may still be the prior turn and
carries no TaskID or predecessor identity. Separately, a nonterminal model response can publish stable tool work or a
successor request without leaving a loop-owned durable fact that says which response was applied. The same review also
found malformed loop-specific identities that reach lookup or mutation before permanent-invalid classification, and
loop-birth metrics that fire before durable done or once per continuation. This inventory measures those claims and
the outward adopter seam. It makes no target-state choice.

## 1. Claimed gaps

- `openspec/changes/agentic-loop-restart-safety/tasks.md:166` — `## 4. Loop task and response settlement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:168` — `- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:173` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:179` — `- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:182` — `unsettled, exact current RequestID plus the final marker proves model-response application, and terminal state alone`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:67` — `- **WHEN** a redelivered input's exact identity is present in a committed later request or terminal outcome`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:124` — `Agentic-loop SHALL load the exact `LoopEntity` identified by an incoming delivery and reconstruct only the material`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:163` — `Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.`
- `openspec/specs/agentic-loop/spec.md:715` — `Attaching MUST preserve the redelivery-dedup property that intake already relies on: after a continuation is`
- `openspec/specs/agentic-loop/spec.md:718` — `**Preserve, do not restore.** This requirement is about a loop still held by the running process. Reconstructing`

## 2. Every current spelling of the facts

### 2.1 Task, loop, and continuation identity

- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id"` // producer-minted loop identity`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/state.go:49` — `ID                 string                `json:"id"``
- `agentic/state.go:50` — `TaskID             string                `json:"task_id"``
- `processor/agentic-loop/handlers.go:838` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:853` — `// A supplied token that already names a registered loop is a CONTINUATION,`
- `processor/agentic-loop/handlers.go:873` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/state.go:299` — `entity.TaskID = taskID`
- `processor/agentic-loop/handlers.go:966` — `_ = cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{`
- `processor/agentic-loop/handlers.go:968` — `Content: task.Prompt,`
- `processor/agentic-loop/handlers.go:1001` — `messages = cm.GetContext()`

TaskMessage and LoopEntity carry TaskID, and a continuation immediately rebinds the one LoopEntity TaskID while
appending the new prompt to process-local conversation state.

### 2.2 AgentRequest identity and latest-retained evidence

- `agentic/types.go:108` — `type AgentRequest struct {`
- `agentic/types.go:109` — `RequestID   string           `json:"request_id"``
- `agentic/types.go:110` — `LoopID      string           `json:"loop_id"``
- `agentic/types.go:111` — `Role        string           `json:"role"``
- `agentic/types.go:112` — `Messages    []ChatMessage    `json:"messages"``
- `agentic/types.go:113` — `Model       string           `json:"model"``
- `processor/agentic-loop/handlers.go:1064` — `return agentic.AgentRequest{`
- `processor/agentic-loop/handlers.go:1065` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:1946` — `request := agentic.AgentRequest{`
- `processor/agentic-loop/handlers.go:1947` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2604` — `request := agentic.AgentRequest{`
- `processor/agentic-loop/handlers.go:2605` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/config.go:444` — `Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Description: "Agent model requests (JetStream)",`
- `processor/agentic-loop/settlement_recovery.go:53` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:187` — `if evidence.subject != subject || request.LoopID != loopID ||`
- `processor/agentic-loop/settlement_recovery.go:188` — `!strings.HasPrefix(request.RequestID, loopID+":req:") {`

The public AgentRequest has no TaskID or predecessor RequestID. All three loop request constructors mint a fresh
RequestID. Recovery reads only the latest exact `agent.request.<LoopID>` message and currently validates LoopID and a
string prefix, so a retained prior turn and a retained current turn are not distinguishable by task provenance.

### 2.3 Task settlement ordering and process cache

- `processor/agentic-loop/component.go:101` — `// pendingTaskResults retains the not-yet-published spawn result when a`
- `processor/agentic-loop/component.go:105` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1266` — `if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {`
- `processor/agentic-loop/component.go:1267` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1294` — `if !result.Created {`
- `processor/agentic-loop/component.go:1295` — `pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1300` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1361` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1365` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1366` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1386` — `c.pendingTaskResults[taskID] = result`
- `processor/agentic-loop/settlement_recovery.go:264` — `if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:275` — `request, retained, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:280` — `if request.Role != task.Role || request.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:287` — `assembled := c.handler.assembleSystemPrompt(ctx, task)`
- `processor/agentic-loop/settlement_recovery.go:294` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`
- `processor/agentic-loop/settlement_recovery.go:318` — `result, err := c.handler.buildTaskResultFromRequest(entity.ID, task, entity, request)`

Current task settlement persists LoopEntity before ordinary publications. The cache preserves the full HandlerResult
only in the same process. A replacement loses it. Cold recovery accepts any retained current-subject request whose
role/model match; on retained absence it always rebuilds an initial request from TaskMessage and does not distinguish
an initial birth from a continuation whose prior conversation exists only in the previous retained request.

### 2.4 Real-NATS serial task-delivery exclusion

- `processor/agentic-loop/component.go:988` — `// - Long-running ports (task, response, tool.result) need serial processing,`
- `processor/agentic-loop/component.go:1118` — `if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {`
- `processor/agentic-loop/component.go:1119` — `fixed = 1`
- `processor/agentic-loop/component.go:1121` — `if consumerConfig.MaxAckPending != 0 {`
- `processor/agentic-loop/consumer_policy_test.go:37` — `{portName: "agent.task", fixed: 1},`
- `processor/agentic-loop/consumer_policy_integration_test.go:16` — `func TestIntegrationTaskConsumerSerializesRedeliveryBeforeLaterWork(t *testing.T) {`
- `processor/agentic-loop/consumer_policy_integration_test.go:28` — `_, maxAckPending, err := agenticLoopConsumerPolicy(port)`
- `processor/agentic-loop/consumer_policy_integration_test.go:30` — `require.Equal(t, 1, maxAckPending)`
- `processor/agentic-loop/consumer_policy_integration_test.go:47` — `batch, err := consumer.Fetch(2, jetstream.FetchMaxWait(5*time.Second))`
- `processor/agentic-loop/consumer_policy_integration_test.go:58` — `require.Equal(t, 1, info.NumAckPending,`
- `processor/agentic-loop/consumer_policy_integration_test.go:60` — `require.Equal(t, uint64(1), info.NumPending,`
- `processor/agentic-loop/consumer_policy_integration_test.go:66` — `require.NoError(t, first.DoubleAck(ctx))`
- `processor/agentic-loop/consumer_policy_integration_test.go:69` — `require.Equal(t, []byte("task-n-plus-1"), second.Data())`
- `processor/agentic-loop/consumer_policy_integration_test.go:72` — `require.Equal(t, uint64(1), secondMetadata.NumDelivered,`
- `docs/advanced/11-jetstream-tuning.md:175` — `MaxAckPending: 1,                   // serial — NATS holds next until ack`

The real server exposed task N as the sole AckPending delivery while N+1 remained Pending, then delivered N+1 at
`NumDelivered=1` only after server-confirmed `DoubleAck` of N. An unacknowledged older task therefore blocks a later
continuation, while a confirmed older ACK removes it from redelivery eligibility. The feared ordering “older task
redelivers after a later continuation was delivered” is excluded by the current durable-consumer contract; no
TaskID-history premise remains.

### 2.4a Continuation does not exclude an outstanding model request

- `processor/agentic-loop/state.go:258` — `//   - The loop's task association is rebound to the continuation's task ID.`
- `processor/agentic-loop/state.go:267` — `//     N-1 arriving after turn N has attached is no longer recognised as`
- `processor/agentic-loop/state.go:288` — `if pending := len(m.pendingTools[loopID]); pending > 0 {`
- `processor/agentic-loop/state.go:293` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/state.go:841` — `m.requestToLoop[requestID] = loopID`
- `processor/agentic-loop/handlers.go:1083` — `h.loopManager.TrackRequest(request.RequestID, loopID)`
- `openspec/specs/agentic-loop/spec.md:702` — `flight when the loop holds outstanding tool calls, or when the loop is awaiting a human approval decision.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:176` — `Preserve every exit for #1244 as a declared transition or refusal and never encode log-and-ACK as success. Persist`

`attachContinuation` refuses pending tools and awaiting approval, but it has no outstanding-model-request test. The
request map retains request-to-loop routes rather than an outstanding/completed bit. An accepted continuation can
therefore publish R2 while R1 remains outstanding; R1 then no longer agrees with the exact latest request. #1244 owns
the eventual continuation behavior contract, while task 4 must preserve whatever declared accept/refuse boundary it
receives. The comment claiming turn N-1 may arrive after turn N is stale: §2.4's real-NATS MaxAckPending=1 proof
excludes a later task delivery until N-1 is server-confirmed ACKed, after which N-1 is no longer redeliverable. It is
not a premise for TaskID history or a ledger.

### 2.5 Response-source correlation and applied proof

- `agentic/types.go:170` — `RequestID    string      `json:"request_id"``
- `agentic/state.go:56` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID`
- `agentic/tools.go:214` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:215` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:216` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-loop/handlers.go:47` — `type PublishedMessage struct {`
- `processor/agentic-loop/handlers.go:48` — `Subject string`
- `processor/agentic-loop/handlers.go:49` — `Data    []byte`
- `processor/agentic-loop/handlers.go:1285` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls); err != nil {`
- `processor/agentic-loop/handlers.go:1476` — `h.loopManager.QueueToolCalls(loopID, approved[idx:])`
- `processor/agentic-loop/handlers.go:1784` — `result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{`
- `processor/agentic-loop/state.go:958` — `resultKey := result.ExecutionID`
- `processor/agentic-loop/state.go:965` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/component.go:1787` — `} else if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1791` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2064` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`

Provider work is identified by RequestID. Tool work derives stable ExecutionID from RequestID, provider CallID, and
positive ordinal. A tool-call response can create one live pending route, queue later calls, and publish stable
`tool.execute` bytes. The nonterminal path persists LoopEntity before publication, but LoopEntity has no field naming
an applied response RequestID, and PublishedMessage carries no separate predecessor. Stable execution identity makes
repeat publication correlatable; it is not proof that the AgentResponse source was applied.

- `processor/agentic-loop/handlers.go:1293` — `if h.loopManager.AllToolsComplete(loopID) {`
- `processor/agentic-loop/handlers.go:1294` — `completionResult, err := h.handleToolsComplete(ctx, loopID, entity, cm, &result)`
- `processor/agentic-loop/handlers.go:1946` — `request := agentic.AgentRequest{`
- `processor/agentic-loop/handlers.go:1947` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2604` — `request := agentic.AgentRequest{`
- `processor/agentic-loop/handlers.go:2605` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/component.go:1505` — `// The exact current retained AgentRequest and final durable marker`
- `processor/agentic-loop/component.go:1507` — `// this model response was already applied.`

A terminal response has named proof: exact current retained request plus final terminal LoopEntity marker. A
nonterminal response may emit a successor request, but that request has a fresh RequestID and no predecessor identity,
so retained R2 does not currently prove R1 applied. When R1 emits tool work and no successor exists, no durable
loop-owned R1-applied marker was found.

### 2.5a Complete process-local tool partition and eviction census

- `processor/agentic-loop/state.go:64` — `pendingTools           map[string]map[string]bool          // loopID -> map[callID]bool`
- `processor/agentic-loop/state.go:65` — `queuedToolCalls        map[string][]agentic.ToolCall       // loopID -> remaining calls to dispatch serially`
- `processor/agentic-loop/state.go:72` — `requestToLoop          map[string]string                   // requestID -> loopID`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop         map[string]string                   // executionID -> loopID`
- `processor/agentic-loop/state.go:74` — `executionIDToName      map[string]string                   // executionID -> function name (for Gemini tool result name field)`
- `processor/agentic-loop/state.go:75` — `executionIDToArguments map[string]map[string]any           // executionID -> tool arguments (for trajectory audit)`
- `processor/agentic-loop/state.go:76` — `executionIDToOrdinal   map[string]uint32                   // executionID -> model response order (for trajectory audit)`
- `processor/agentic-loop/state.go:757` — `m.pendingTools[loopID][callID] = true`
- `processor/agentic-loop/state.go:767` — `delete(m.pendingTools[loopID], callID)`
- `processor/agentic-loop/state.go:778` — `pending := m.pendingTools[loopID]`
- `processor/agentic-loop/state.go:796` — `pending := m.pendingTools[loopID]`
- `processor/agentic-loop/state.go:804` — `m.queuedToolCalls[loopID] = append(m.queuedToolCalls[loopID], calls...)`
- `processor/agentic-loop/state.go:812` — `queue := m.queuedToolCalls[loopID]`
- `processor/agentic-loop/state.go:819` — `m.queuedToolCalls[loopID] = queue[1:]`
- `processor/agentic-loop/state.go:834` — `delete(m.queuedToolCalls, loopID)`
- `processor/agentic-loop/state.go:856` — `m.toolCallToLoop[executionID] = loopID`
- `processor/agentic-loop/state.go:864` — `m.executionIDToName[executionID] = name`
- `processor/agentic-loop/state.go:879` — `m.executionIDToArguments[executionID] = args`
- `processor/agentic-loop/state.go:899` — `m.executionIDToOrdinal[executionID] = ordinal`
- `processor/agentic-loop/state.go:941` — `loopID, exists := m.toolCallToLoop[executionID]`
- `processor/agentic-loop/state.go:958` — `resultKey := result.ExecutionID`
- `processor/agentic-loop/state.go:965` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/state.go:1002` — `delete(m.toolCallToLoop, executionID)`
- `processor/agentic-loop/state.go:1004` — `entity.PendingToolResults = nil`
- `processor/agentic-loop/state.go:593` — `delete(m.executionIDToName, executionID)`
- `processor/agentic-loop/state.go:594` — `delete(m.executionIDToArguments, executionID)`
- `processor/agentic-loop/state.go:595` — `delete(m.executionIDToOrdinal, executionID)`
- `processor/agentic-loop/handlers.go:1345` — `h.loopManager.TrackToolOrdinal(toolCall.ExecutionID, uint32(index+1))`
- `processor/agentic-loop/handlers.go:1476` — `h.loopManager.QueueToolCalls(loopID, approved[idx:])`
- `processor/agentic-loop/handlers.go:1674` — `if err := h.loopManager.AddPendingTool(loopID, tc.ID); err != nil {`
- `processor/agentic-loop/handlers.go:1678` — `h.loopManager.TrackToolCall(tc.ExecutionID, loopID)`
- `processor/agentic-loop/handlers.go:1680` — `h.loopManager.TrackToolName(tc.ExecutionID, tc.Name)`
- `processor/agentic-loop/handlers.go:1681` — `h.loopManager.TrackToolArguments(tc.ExecutionID, tc.Arguments)`
- `processor/agentic-loop/handlers.go:2311` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`
- `processor/agentic-loop/handlers.go:2317` — `err = h.loopManager.RemovePendingTool(loopID, toolResult.CallID)`
- `processor/agentic-loop/handlers.go:2563` — `allResults := h.loopManager.GetAndClearToolResults(loopID)`
- `processor/agentic-loop/component.go:1944` — `// Find the process-local route for this tool execution. Empty routes take`
- `processor/agentic-loop/component.go:1979` — `c.handler.loopManager.GetToolName(toolResult.ExecutionID) != toolResult.Name ||`
- `processor/agentic-loop/component.go:1980` — `c.handler.loopManager.GetToolOrdinal(toolResult.ExecutionID) != toolResult.CallOrdinal {`

The live response-applied candidate is a partition spread across these separately locked maps: stored results, the
active pending route, the queued suffix, and per-execution name/arguments/ordinal. `GetAndClearToolResults` removes
execution routes and clears durable-in-LoopEntity pending results while intentionally retaining metadata until later
cleanup; `DeleteLoop` can delete metadata only for execution routes that still name the loop. No one atomic snapshot
or validator of the whole partition was found. This inventory reports that candidate and its eviction gaps; it does
not yet claim it is valid proof.

### 2.5b Durable trajectory observation is not response-applied authority

- `processor/agentic-loop/component.go:64` — `trajectoryRecorder    *trajectoryRecorder`
- `processor/agentic-loop/component.go:808` — `// Immutable trajectory facts are best-effort audit state. A missing bucket`
- `processor/agentic-loop/component.go:821` — `// Clean beta policy: an incompatible retained bucket is never written,`
- `processor/agentic-loop/handlers.go:1179` — `Kind:              agentic.TrajectoryKindModelCompleted,`
- `processor/agentic-loop/handlers.go:1181` — `SourceCorrelation: response.RequestID,`
- `processor/agentic-loop/trajectory_handler_wiring.go:71` — `func (c *Component) recordTrajectoryObservations(ctx context.Context, result HandlerResult) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:150` — `func (c *Component) recordTrajectoryBatch(parent context.Context, observations []trajectoryObservation) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:176` — `c.trajectoryRecorder.record(ctx, observation)`
- `processor/agentic-loop/trajectory_recorder.go:120` — `type trajectoryRecorder struct {`
- `processor/agentic-loop/trajectory_recorder.go:217` — `_, createErr := r.bucket.Create(ctx, key, encoded)`
- `agentic/doc.go:197` — `// One or more terminal facts mean terminal outcomes were observed; they are not`
- `agentic/doc.go:199` — `// transient execution helpers, not durable/public read authority.`
- `openspec/specs/agentic-loop/spec.md:481` — `coverage guarantee. A prefix with no visible facts SHALL return not-found. Facts with no terminal observation SHALL`

The recorder can durably observe a model response under its RequestID, but recording is best-effort and the public
contract is observed-only, not complete. It is therefore a same-class durable carrier but not authoritative
response-applied proof. Task 5 remains the owner of ordered tool-result batch reconstruction; task 4 does not turn
trajectory evidence into a replay ledger or second state owner.

### 2.6 Existing approval-state exception

- `agentic/state.go:146` — `type PendingApprovalState struct {`
- `agentic/state.go:147` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/state.go:148` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/state.go:149` — `CallID      string         `json:"call_id"``
- `agentic/state.go:150` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/handlers.go:2427` — `if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {`
- `processor/agentic-loop/handlers.go:2430` — `entity.PendingApproval.RequestID = toolResult.RequestID`
- `processor/agentic-loop/handlers.go:2431` — `entity.PendingApproval.ExecutionID = toolResult.ExecutionID`
- `processor/agentic-loop/handlers.go:2432` — `entity.PendingApproval.CallOrdinal = toolResult.CallOrdinal`
- `processor/agentic-loop/handlers.go:2438` — `h.loopManager.ClearQueuedTools(loopID)`
- `processor/agentic-loop/handlers.go:2440` — `if err := h.loopManager.UpdateLoop(*entity); err != nil {`

Approval-required handling already persists exact RequestID, ExecutionID, provider CallID, ordinal, and tool name in
current LoopEntity while clearing queued siblings. This can remain after the live pending/queue partition disappears.
It is an existing operation-specific correlation fact; task 6 owns later approval reconstruction, not task 4.

### 2.6a Approval-gate errors currently become successful tool-result settlement

- `processor/agentic-loop/handlers.go:2338` — `if h.checkApprovalGate(loopID, &entity, toolResult, &result) {`
- `processor/agentic-loop/handlers.go:2339` — `return result, nil`
- `processor/agentic-loop/handlers.go:2401` — `pubMsg, err := h.gateForApproval(loopID, entity, toolResult)`
- `processor/agentic-loop/handlers.go:2403` — `h.logger.Warn("failed to gate loop for approval",`
- `processor/agentic-loop/handlers.go:2411` — `return true`
- `processor/agentic-loop/handlers.go:2427` — `if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {`
- `processor/agentic-loop/handlers.go:2440` — `if err := h.loopManager.UpdateLoop(*entity); err != nil {`
- `processor/agentic-loop/handlers.go:2455` — `data, err := json.Marshal(envelope)`
- `processor/agentic-loop/handlers.go:2459` — `subject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.approval_pending", loopID)`
- `processor/agentic-loop/component.go:2005` — `result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)`
- `processor/agentic-loop/component.go:2043` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2050` — `return natsclient.DeliveryDecisionAck, nil`

`gateForApproval` can fail before or after mutating the candidate entity: `BeginAwaitingApproval`, `UpdateLoop`,
envelope marshal, and subject resolution are all error exits. `checkApprovalGate` logs any such error, still sets
`result.State=awaiting_approval`, returns true, and `HandleToolResult` returns nil. The component then persists whatever
HandlerResult remains and ACKs. A missing required `agent.approval_pending` publication can therefore strand the
approval while reporting successful source settlement. This is a task-4 no-log-and-ACK exit. Task 6 still owns
replacement reconstruction after an approval result has validly settled.

### 2.7 Request-ID grammar and classification

- `processor/agentic-loop/state.go:1194` — `// GenerateRequestID creates a structured request ID that embeds the loop ID.`
- `processor/agentic-loop/state.go:1198` — `return fmt.Sprintf("%s:req:%s", loopID, uuid.NewString())`
- `processor/agentic-loop/state.go:1212` — `parts := strings.Split(requestID, ":req:")`
- `processor/agentic-loop/state.go:1213` — `if len(parts) >= 1 && parts[0] != "" {`
- `processor/agentic-loop/state.go:1233` — `if loopID, exists := m.GetLoopForRequest(requestID); exists {`
- `processor/agentic-loop/state.go:1238` — `if loopID := m.ExtractLoopIDFromRequest(requestID); loopID != "" {`
- `processor/agentic-loop/settlement_recovery.go:242` — `func loopIDFromRequestID(requestID string) (string, error) {`
- `processor/agentic-loop/settlement_recovery.go:244` — `if !ok || loopID == "" || suffix == "" || strings.Contains(suffix, ":req:") {`
- `processor/agentic-loop/settlement_recovery.go:330` — `mappedLoopID := c.findLoopIDForRequest(response.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:338` — `if derivedLoopID, err := loopIDFromRequestID(response.RequestID); err == nil && derivedLoopID != mappedLoopID {`
- `processor/agentic-loop/settlement_recovery.go:350` — `loopID, err = loopIDFromRequestID(response.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:357` — `entity, found, err = c.readLoopEntity(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:362` — `return agentic.LoopEntity{}, "", fmt.Errorf("loop %q is not yet observable", loopID)`
- `processor/agentic-loop/settlement_recovery.go:471` — `case errs.IsInvalid(err):`
- `processor/agentic-loop/settlement_recovery.go:472` — `return natsclient.DeliveryDecisionTerminate`

The generator emits `<LoopID>:req:<UUID>`, but the fallback extractor accepts any nonempty prefix, including an ID
with no delimiter. The private parser requires only nonempty pieces and validates neither canonical LoopID nor UUID
suffix. Cold malformed identity can read `AGENT_LOOPS/<arbitrary>` then Retry on absence. Warm lookup happens first,
and a mapped branch ignores parser error because mismatch is tested only when `err == nil`.

- `agentic/types.go:180` — `func (r AgentResponse) Validate() error {`
- `agentic/types.go:181` — `switch r.Status {`
- `processor/agentic-loop/component.go:1587` — `if err := responsePtr.Validate(); err != nil {`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:20` — `Retry means the lane's declared correlation and durable evidence make re-execution safe; Terminate means permanently`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:450` — `already-applied terminal outcome. An unreadable authority or unresolved absence returns Retry. A malformed input,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:451` — `required-correlation conflict, impossible transition, or contradictory durable state returns Quarantine. There is no`

AgentResponse validation checks status only. The general settlement requirement classifies permanent invalid as
Terminate, while the modified late-input paragraph groups malformed input with correlation conflicts as Quarantine.
That is a current spec conflict.

### 2.8 Other permanently malformed loop inputs

- `processor/agentic-loop/execution_identity.go:21` — `if calls[i].ID == "" {`
- `processor/agentic-loop/execution_identity.go:22` — `return fmt.Errorf("tool execution provider call_id required at ordinal %d", ordinal)`
- `processor/agentic-loop/handlers.go:1251` — `if _, addErr := h.trajectoryManager.addStep(loopID, step); addErr != nil {`
- `processor/agentic-loop/handlers.go:1275` — `_ = cm.AddMessage(RegionRecentHistory, response.Message)`
- `processor/agentic-loop/handlers.go:1285` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls); err != nil {`
- `processor/agentic-loop/handlers.go:1341` — `if err := stampToolExecutionCorrelation(requestID, toolCalls); err != nil {`

A StatusToolCall response with empty provider ToolCall.ID passes AgentResponse validation. It is detected only after
trajectory and conversation process mutation, returns an ordinary error, and therefore retries the same permanent
poison.

- `agentic/tools.go:631` — `Name        string         `json:"name,omitempty"` // Tool function name (required by Gemini on tool result messages)`
- `agentic/tools.go:647` — `if t.CallID == "" {`
- `agentic/tools.go:650` — `return nil`
- `processor/agentic-loop/component.go:1938` — `if err := toolResultPtr.Validate(); err != nil {`
- `processor/agentic-loop/component.go:1962` — `if toolResult.RequestID == "" || toolResult.ExecutionID == "" || toolResult.CallOrdinal == 0 {`
- `processor/agentic-loop/component.go:1979` — `c.handler.loopManager.GetToolName(toolResult.ExecutionID) != toolResult.Name ||`
- `processor/agentic-loop/handlers.go:2311` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`

ToolResult validation accepts empty Name even though Name participates in correlation and later provider conversation.
Warm handling rejects it only when a cached name disagrees; no structural accepting-boundary check requires Name before
routing or mutation.

### 2.9 Birth observations

- `processor/agentic-loop/handlers.go:873` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/handlers.go:1042` — `result, err := h.buildTaskRequest(loopID, task, entity, messages, tools)`
- `processor/agentic-loop/handlers.go:1119` — `Created: true,`
- `processor/agentic-loop/component.go:1308` — `c.logger.Debug("Loop created",`
- `processor/agentic-loop/component.go:1355` — `c.metrics.recordLoopCreated()`
- `processor/agentic-loop/component.go:1361` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1366` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1475` — `c.metrics.recordLoopCreated()`
- `processor/agentic-loop/component.go:1477` — `if settleErr := c.handleLoopFailure(ctx, loopID, entity, reason, err); settleErr != nil {`
- `processor/agentic-loop/metrics.go:422` — `m.loopsCreated.Inc()`
- `processor/agentic-loop/metrics.go:423` — `m.activeLoops.Inc()`
- `processor/agentic-loop/metrics.go:429` — `m.activeLoops.Dec()`
- `processor/agentic-loop/metrics.go:436` — `m.loopsFailed.WithLabelValues(reason).Inc()`
- `processor/agentic-loop/metrics.go:437` — `m.activeLoops.Dec()`

The created log/counter/gauge fire before LoopEntity persistence and required PubAcks. Spawn failure increments creation
before its terminal settlement can fail and Retry. Both fresh birth and continuation call the same result builder,
which sets `Created=true`; every accepted continuation increments birth and active gauge, while the one loop has one
terminal decrement. `Created` currently conflates “task outputs exist” with “a fresh loop was born.”

### 2.10 Busy and terminal continuation refusals have no durable consequence

- `processor/agentic-loop/state.go:284` — `return agentic.LoopEntity{}, errs.WrapInvalid(`
- `processor/agentic-loop/state.go:285` — `fmt.Errorf("loop %s is %s: %w", loopID, entity.State, ErrLoopTerminal),`
- `processor/agentic-loop/state.go:289` — `return agentic.LoopEntity{}, errs.WrapTransient(`
- `processor/agentic-loop/state.go:290` — `fmt.Errorf("loop %s has %d tool call(s) still outstanding: %w", loopID, pending, ErrLoopBusy),`
- `processor/agentic-loop/state.go:294` — `return agentic.LoopEntity{}, errs.WrapTransient(`
- `processor/agentic-loop/state.go:295` — `fmt.Errorf("loop %s is awaiting a human approval decision: %w", loopID, ErrLoopBusy),`
- `processor/agentic-loop/component.go:1285` — `if errors.Is(err, ErrLoopBusy) {`
- `processor/agentic-loop/component.go:1288` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-loop/component.go:1290` — `return loopSettlementDecision(err),`
- `processor/agentic-loop/settlement_recovery.go:471` — `case errs.IsInvalid(err):`
- `processor/agentic-loop/settlement_recovery.go:472` — `return natsclient.DeliveryDecisionTerminate`
- `processor/agentic-loop/create_vs_exists_fence_test.go:461` — `if len(result.PublishedMessages) != 0 {`
- `processor/agentic-loop/create_vs_exists_fence_test.go:522` — `t.Fatalf("refused continuation published %d messages", len(result.PublishedMessages))`
- `processor/agentic-loop/create_vs_exists_fence_test.go:578` — `decision, err := c.handleTaskMessage(ctx, data)`
- `processor/agentic-loop/create_vs_exists_fence_test.go:579` — `if err == nil || decision != natsclient.DeliveryDecisionRetry {`
- `processor/agentic-loop/create_vs_exists_fence_test.go:580` — `t.Fatalf("handleTaskMessage returned %v; a refusal is acked, not redelivered", err)`

Busy refusal currently returns Retry with no published consequence. Because the task durable is globally serial at
MaxAckPending=1 and MaxDeliver is bounded, this creates head-of-line retry/exhaustion rather than an owned implicit
queue. Terminal refusal classifies Invalid and terminates with no published consequence. The test expects Retry while
its fatal message says the refusal should be ACKed, a current stale contradiction rather than a contract to preserve.

- `agentic/user_types.go:226` — `type UserResponse struct {`
- `agentic/user_types.go:227` — `ResponseID  string `json:"response_id"``
- `agentic/user_types.go:228` — `ChannelType string `json:"channel_type"``
- `agentic/user_types.go:229` — `ChannelID   string `json:"channel_id"``
- `agentic/user_types.go:237` — `Type    string `json:"type"` // text, status, result, error, prompt, stream`
- `agentic/user_types.go:325` — `// User routing info (optional, for error notifications)`
- `agentic/user_types.go:326` — `ChannelType string `json:"channel_type,omitempty"` // e.g., "http", "cli", "slack"`
- `agentic/user_types.go:327` — `ChannelID   string `json:"channel_id,omitempty"`   // session/channel identifier`
- `agentic/user_types.go:328` — `UserID      string `json:"user_id,omitempty"`      // user who initiated the request`
- `processor/agentic-dispatch/config.go:150` — `Name: "user.response", Config: component.JetStreamPort{`
- `processor/agentic-dispatch/config.go:151` — `Subjects: []string{"user.response.>"}, StreamName: "USER",`
- `processor/agentic-loop/config.go:429` — `Outputs: []component.PortDefinition{`
- `processor/agentic-loop/config.go:444` — `Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Description: "Agent model requests (JetStream)",`
- `processor/agentic-loop/config.go:453` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Description: "Loop-created lifecycle events (JetStream)",`

TaskMessage already carries optional user routing and registered UserResponse already models a typed channel response,
but only dispatch declares the USER-stream `user.response` output. Agentic-loop declares neither that port nor a
TaskRefused/LoopRefused payload or subject. Rule/workflow tasks may carry no user channel, so a user-only response
would not declare their refusal outcome.

### 2.11 Existing durable TaskID correlation carriers and their windows

- `agentic/events.go:12` — `type LoopCreatedEvent struct {`
- `agentic/events.go:13` — `LoopID           string         `json:"loop_id"``
- `agentic/events.go:14` — `TaskID           string         `json:"task_id"``
- `agentic/payload_registry.go:42` — `{Domain: Domain, Category: CategoryLoopCreated, Version: SchemaVersion, Description: "Loop creation event", Factory: func() any { return &LoopCreatedEvent{} }, IndexingProfile: control},`
- `processor/agentic-loop/handlers.go:595` — `created := agentic.LoopCreatedEvent{`
- `processor/agentic-loop/handlers.go:597` — `TaskID:           task.TaskID,`
- `processor/agentic-loop/handlers.go:1111` — `createdSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.created", loopID)`
- `processor/agentic-loop/handlers.go:1126` — `Subject: createdSubject,`
- `processor/agentic-loop/config.go:453` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Description: "Loop-created lifecycle events (JetStream)",`
- `agentic/rule_fields.go:101` — `"task_id":        e.TaskID,`
- `processor/agentic-dispatch/component.go:1104` — `createdPtr, ok := baseMsg.Payload().(*agentic.LoopCreatedEvent)`
- `processor/agentic-dispatch/component.go:1122` — `TaskID:           created.TaskID,`

`LoopCreatedEvent` is a registered TaskID carrier emitted on the exact loop subject beside every task-built request.
The current result builder marks continuations `Created=true`, so the name describes birth while the bytes can describe
a later task. Dispatch consumes it with a stable durable but DeliverPolicy new; it is not an exact task-recovery lookup.

- `agentic/loop_execution_entity.go:127` — `triples = append(triples, triple(agvocab.LoopTask, e.Task.TaskID))`
- `processor/agentic-loop/graph_writer.go:438` — `func (w *graphWriter) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {`
- `processor/agentic-loop/graph_writer.go:463` — `Task:     task,`
- `processor/agentic-loop/graph_writer.go:483` — `if err := w.createEntityWithTriples(ctx, entityState, triples); err != nil {`
- `graph/kvcatalog.go:58` — `entityStates := owned(BucketEntityStates, "graph-ingest",`
- `graph/kvcatalog.go:67` — `entityStates.History = 1`

The loop-execution graph birth carries `agent.loop.task` in authoritative `ENTITY_STATES`, whose current-value history
is one and whose retention policy has no lifecycle expiry. It is a create-time fact, not a per-continuation task
history; terminal graph stamps intentionally do not rewrite it.

- `agentic/events.go:62` — `TaskID       string    `json:"task_id"``
- `agentic/events.go:138` — `TaskID     string `json:"task_id"``
- `agentic/events.go:205` — `TaskID      string `json:"task_id"``
- `agentic/rule_fields.go:131` — `"task_id":    e.TaskID,`
- `agentic/rule_fields.go:181` — `"task_id":    e.TaskID,`
- `agentic/rule_fields.go:211` — `"task_id":      e.TaskID,`
- `processor/agentic-loop/handlers.go:2155` — `TaskID:       entity.TaskID,`
- `processor/agentic-loop/handlers.go:2742` — `TaskID:       entity.TaskID,`
- `processor/agentic-loop/component.go:2345` — `TaskID:       entity.TaskID,`
- `processor/agentic-loop/component.go:2136` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2162` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2186` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:799` — `History: 10,`
- `processor/agentic-loop/component.go:800` — `TTL:     24 * time.Hour,`
- `configs/agentic.json:19` — `"max_age": "24h",`
- `configs/agentic.json:20` — `"max_bytes": 268435456,`
- `configs/agentic.json:21` — `"discard": "old"`
- `internal/agentterminal/terminal.go:102` — `func Decode(decoder *message.Decoder, data []byte) (Event, error) {`
- `internal/agentterminal/terminal.go:119` — `if err := base.Payload().Validate(); err != nil {`
- `internal/agentterminal/terminal.go:136` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `internal/agentterminal/terminal.go:157` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `internal/agentterminal/terminal.go:177` — `event.LoopID, event.TaskID = payload.LoopID, payload.TaskID`
- `output/otel/span_collector.go:216` — `terminal, err := agentterminal.Decode(sc.decoder, data)`
- `output/otel/span_collector.go:323` — `"agent.task_id":   evt.TaskID,`
- `output/otel/span_collector.go:495` — `spanKey := event.LoopID + ":" + event.TaskID`
- `output/otel/span_collector.go:506` — `"agent.task_id": event.TaskID,`
- `output/otel/span_collector.go:523` — `spanKey := event.LoopID + ":" + event.TaskID`
- `output/otel/span_collector.go:548` — `if event.TaskID != "" {`
- `output/otel/span_collector.go:549` — `parentKey = event.LoopID + ":" + event.TaskID`
- `output/otel/span_collector.go:572` — `"agent.task_id":  event.TaskID,`
- `output/otel/span_collector.go:616` — `if event.TaskID != "" {`
- `output/otel/span_collector.go:617` — `spanKey := event.LoopID + ":" + event.TaskID`
- `agentic/agentrun/agentrun.go:563` — `// HandleEvent processes a raw NATS message payload from agent.complete.* or`
- `agentic/agentrun/agentrun.go:576` — `normalized, err := agentterminal.Decode(s.decoder, data)`
- `agentic/agentrun/agentrun.go:580` — `ev := LoopTerminalEvent{`
- `agentic/agentrun/agentrun.go:765` — `// Start wires the MilestoneSubscriber to the live NATS connection using DURABLE`
- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `agentic/agentrun/agentrun.go:829` — `FilterSubject: "agent.complete.*",`
- `agentic/agentrun/agentrun.go:831` — `DeliverPolicy: "new",`
- `agentic/agentrun/agentrun.go:880` — `FilterSubject: "agent.failed.*",`
- `agentic/agentrun/agentrun.go:882` — `DeliverPolicy: "new",`

Completed, failed, and cancelled events carry the LoopEntity's current scalar TaskID, expose it to rules, publish on
AGENT terminal subjects, and are independently copied to `AGENT_LOOPS/COMPLETE_<LoopID>`. These are separate evidence
windows: an AGENT event is bounded by 24 hours and 256 MiB with DiscardOld and may be evicted before age expiry, while
the COMPLETE KV copy has its own 24-hour TTL. The authoritative `ENTITY_STATES` graph birth is a third current-value
carrier with History 1 and no lifecycle expiry, but it records only the create-time TaskID. The shared terminal
normalizer validates and projects TaskID; OTel uses it for span identity and attributes; agentrun durably consumes
terminal events but drops TaskID from its downstream projection. Each terminal carrier names only the task current at
terminal, not prior continuation TaskIDs. None distinguishes a retained prior AgentRequest from the request for the
delivered continuation without scan/history or another correlation field.

## 3. Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:18` — `Decode, correlation, KV, Store, transition, and required publication failures SHALL NOT become successful callback`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:20` — `Retry means the lane's declared correlation and durable evidence make re-execution safe; Terminate means permanently`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:21` — `invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:129` — `- **WHEN** AgentResponse carries a structured RequestID`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:154` — `Every TaskMessage producer SHALL supply a nonempty canonical LoopID before validation, envelope marshal, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:168` — `For every terminal `LoopEntity` transition, the bare `AGENT_LOOPS/<LoopID>` terminal Put SHALL be the final`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:225` — `#### Scenario: Exact model response is proven applied`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:185` — `## 5. Tool result and completed outcome`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:196` — `- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:202` — `idempotency claims use framework execution identity and operation-specific effect authority. Add no claimed/in-`
- `processor/agentic-loop/settlement_recovery.go:457` — `return fmt.Errorf("tool result %q is durably correlated but terminal state is not execution-specific applied proof; task 5 owns that proof", result.ExecutionID)`

Task 4 owns whether task and AgentResponse sources settle after loop-owned durable done. Task 5 owns executor-effect
completion, ToolResult ordered-batch recovery, and completed tool outcomes. Task 6 owns approval continuation after an
approval result has settled. The present inventory records seams but does not absorb those scopes.

## 4. Consumer at birth

- `processor/agentic-loop/settlement_recovery.go:250` — `func (c *Component) recoverTaskDelivery(`
- `processor/agentic-loop/settlement_recovery.go:327` — `func (c *Component) ensureResponseLoop(`
- `processor/agentic-loop/handlers.go:1058` — `func (h *MessageHandler) newTaskRequest(`
- `processor/agentic-loop/handlers.go:1920` — `func (h *MessageHandler) emitRetryRequest(ctx context.Context, loopID string, entity agentic.LoopEntity, cm *ContextManager, result *HandlerResult, postUtilization float64) error {`
- `processor/agentic-loop/handlers.go:2244` — `// HandleToolResult processes a tool execution result`

Any task-turn correlation placed on loop-generated AgentRequest has present consumers in task redelivery and response
application. Any immediate-predecessor correlation has present consumers only when a response produces a successor
request. The generic AgentRequest/AgentResponse DTOs also have direct provider clients outside this loop protocol, so
loop grammar cannot be assumed universal.

## 5. Problem shape

The shape is operation-specific applied proof for an at-least-once input: exact retained output plus authoritative
current state distinguishes “perform/repeat the safe ordinary output” from “already applied,” without a replay ledger.

- `processor/agentic-model/component.go:602` — `// handleRequest processes an agent request and returns only after its required`
- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:624` — `if found {`
- `processor/agentic-model/component.go:628` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/provider_settlement.go:37` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-model/provider_settlement.go:113` — `if evidence.subject != subject || response.RequestID != requestID {`

Agentic-model is the closest existing instance: exact retained response with matching RequestID proves provider work
already produced its required output; confirmed absence permits the documented fresh invocation. Task 4 already adopts
the same read-through shape for current request and terminal response, but lacks a complete nonterminal response proof.
No new named reusable framework primitive exists in the inventory, so an adoption sweep is not triggered.

## Same-class collision table

| Dimension | Current evidence |
|---|---|
| Semantic class | Proof that a task turn or AgentResponse has been durably applied, sufficient to ACK without repeating unsafe work. |
| Owners | TaskMessage/LoopEntity own current TaskID; LoopCreatedEvent, loop-execution graph birth, and terminal events carry independent lifecycle projections with different retention windows; retained AgentRequest owns current request bytes; LoopManager owns the process-local request/tool partition; terminal LoopEntity owns terminal marker; PendingApproval owns approval correlation; AGENT_TRAJECTORIES owns observed-only audit facts; task 5 owns tool effect outcomes. |
| Catalogs | Registered AgentRequest/AgentResponse/TaskMessage/ToolCall/ToolResult plus LoopCreated/Completed/Failed/Cancelled payloads; component port catalog names exact AGENT subjects; RuleFields exposes LoopID/TaskID for all four lifecycle payloads; graph vocabulary exposes `agent.loop.task`. |
| Status | LoopEntity State is current loop status; `COMPLETE_` is terminal result; pending/queue/result maps are process execution state; trajectory and Prometheus birth/terminal observations are non-authoritative. |
| Lifecycle | The shipped AGENT stream has a 24h/256 MiB DiscardOld window and may evict early; the independent AGENT_LOOPS COMPLETE copy has a 24h TTL; authoritative ENTITY_STATES has History 1 and no lifecycle expiry, but its graph birth carries only spawn TaskID. Process maps/cache disappear on replacement; MaxAckPending=1 serializes each long-running input durable. Continuation admission currently excludes tool/approval work but not an outstanding model request. |
| Ownership | Agentic-loop is the one loop-state writer; agentic-model owns provider invocation/output; agentic-tools/task 5 owns executor effect completion. No active/active or second recovery owner was found. |
| Readers | Agentic-loop task/response/tool-result handlers; agentic-model request handler; agentterminal validates and projects terminal TaskID; dispatch consumes created and terminal events; OTel uses TaskID for span identity and attributes; agentrun durably consumes terminal events but drops TaskID downstream; rules read event projections and graph predicates; trajectory query reads observed facts. |
| Writers | Task producers write TaskMessage; loop writes AgentRequest/tool work/LoopEntity, created/terminal events, graph birth, and trajectory observations; model writes AgentResponse; tools write ToolResult/outcomes. |
| Recovery | Exact latest-subject reads, current LoopEntity read-through, terminal final marker, PendingApproval correlation, created/terminal lifecycle carriers, graph birth fact, observed-only trajectory facts, and task-5 completed outcomes. No task history, response-applied ledger, stream scan, atomic live-partition proof, or generic Correlatable implementation was found. |

## Adopter seam inventory

### Specific adopter

A developer outside SemStreams uses `agentic.AgentRequest` directly with an agentic-model client or adapter. They do
not know agentic-loop's internal TaskID, continuation, or structured RequestID grammar.

### Current external reach

- `agentic/types.go:107` — `// AgentRequest represents a request to an agentic service`
- `processor/agentic-model/client.go:440` — `func (c *Client) ChatCompletion(ctx context.Context, req agentic.AgentRequest) (agentic.AgentResponse, error) {`
- `processor/agentic-dispatch/intent_classifier.go:125` — `resp, err := client.ChatCompletion(reqCtx, agentic.AgentRequest{`
- `processor/agentic-detonator/canary.go:204` — `req := agentic.AgentRequest{`
- `agentic/README.md:91` — `request := agentic.AgentRequest{`

Read-only sister sweep found three production direct constructors in SemDragon:
`service/api/handlers.go:1591`, `processor/bossbattle/evaluator.go:288`, and
`processor/executor/executor.go:201`. They use product-local RequestIDs; two omit LoopID and all omit TaskID. No
production sister publisher of typed AgentRequest onto `agent.request.*` was found.

### What must they know today?

They must supply a nonempty RequestID and nonempty Messages to generic AgentRequest validation. Direct provider callers
do not currently need TaskID, predecessor identity, canonical loop UUID, or `:req:` grammar.

- `agentic/types.go:131` — `func (r AgentRequest) Validate() error {`
- `agentic/types.go:132` — `if r.RequestID == "" {`
- `agentic/types.go:135` — `if len(r.Messages) == 0 {`

### What happens if they do nothing?

Their direct provider call remains valid under the current DTO contract. Requiring loop-only correlation globally
would turn unrelated product-local IDs into validation failures, so that outward bill is a design finding.

### Where do they find out?

Today the generic validation failure is a typed runtime error; the README demonstrates a product-local `req_001`.
Loop-specific grammar is internal and not an adopter contract.

### What should they have to know?

Nothing about loop TaskID, predecessor RequestID, durable subjects, or replacement recovery. The framework owns those
facts at loop request construction and loop accepting boundaries. If an adopter deliberately publishes or transforms
loop-protocol AgentRequest bytes, the eventual migration note must state any preserve-on-forward correlation fields;
direct ChatCompletion clients should require no action.

### Prediction check

No adopter should predict current TaskID, predecessor RequestID, consumer timing, or retained state. Those are facts
the loop owns or can observe from its input/current exact evidence.

## Searches run

- `gopls workspace_symbol -matcher=fuzzy AgentRequest` — found the public struct, exact request-address helper, and tests; no second AgentRequest type.
- `gopls references agentic/types.go:108:6` — found registry, loop constructors/recovery, model clients, direct in-repo clients, examples, and tests.
- `gopls references agentic/types.go:168:6` — found model producers, loop consumers, provider adapters, and tests.
- `gopls references agentic/state.go:48:6` — found LoopEntity managers, persistence, handlers, tools, gateways, and tests.
- `gopls references message/behaviors.go:68:6` — no production implementation of Correlatable was found.
- `git grep --untracked -n -E 'LastAppliedRequest|AppliedRequest|PredecessorRequest|PreviousRequest|ParentRequest|CausationID' -- agentic processor/agentic-loop` → 0.
- `git grep --untracked -n 'CorrelationID() string' -- ':!message/behaviors.go' ':!message/doc.go'` → 0 production implementations.
- `git grep --untracked -n -E 'agent\\.request|agent\\.response|tool\\.execute|tool\\.result|AGENT_LOOPS|COMPLETE_' -- processor/agentic-loop agentic openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md` → 399 hits reviewed by declaration/owner family.
- `git grep --untracked -n -E 'recordLoopCreated|loopsCreated|activeLoops|Loop created|Created: true' -- processor/agentic-loop agentic` → 40 hits reviewed.
- `git grep --untracked -n -E 'MaxAckPending|DoubleAck|NumAckPending|NumPending' -- processor/agentic-loop/consumer_policy_integration_test.go processor/agentic-loop/component.go processor/agentic-loop/consumer_policy_test.go docs/advanced/11-jetstream-tuning.md docs/concepts/14-orchestration-layers.md` → 32 hits reviewed.
- `GOCACHE=/tmp/semstreams-task4-gocache gopls references processor/agentic-loop/state.go:64:2` and the same command for fields at `:65:2`, `:73:2`, `:74:2`, `:75:2`, and `:76:2` — enumerated initialization, readers, writers, drain, and terminal eviction for the live tool partition.
- `git grep --untracked -n -E 'Snapshot.*(Tool|Execution|Pending)|snapshot.*(pendingTools|queuedToolCalls|toolCallToLoop)|LastApplied(Request|Response)|Applied(Request|Response)' -- processor/agentic-loop agentic` → one unrelated approval-sweeper test name; no production atomic tool-partition snapshot or applied-request/response field.
- `git grep --untracked -n -E 'LoopCreatedEvent|LoopCompletedEvent|LoopFailedEvent|LoopCancelledEvent|agent\.loop\.task' -- agentic internal/agentterminal output/otel processor/agentic-loop processor/agentic-dispatch processor/rule` → 299 hits, reviewed by declaration, writer, reader, and retention family.
- `gopls references agentic/events.go:12:6` — LoopCreatedEvent constructors, registry, RuleFields, dispatch/OTel consumers, docs, and tests.
- `gopls references agentic/events.go:60:6` — LoopCompletedEvent loop writers, agentterminal/dispatch/agentrun/OTel readers, graph writers, gateways, and tests.
- `gopls references agentic/events.go:136:6` — LoopFailedEvent loop writers, agentterminal/dispatch/agentrun/OTel readers, graph writers, gateways, and tests.
- `gopls references agentic/events.go:203:6` — LoopCancelledEvent loop writers, agentterminal/dispatch/agentrun/OTel readers, graph writers, gateways, and tests.
- `git grep --untracked -n -E 'readRetained.*Created|GetLastMsgForSubject.*created|agent\.created.*GetLast' -- processor agentic` → 0 exact retained-created recovery readers.
- `go test -race -tags=integration ./processor/agentic-loop -run TestIntegrationTaskConsumerSerializesRedeliveryBeforeLaterWork -count=1` → PASS against real testcontainers NATS.

## Open evidence questions

1. Can one atomic process-local LoopManager snapshot prove that every call in response R1 is represented exactly once
   across stored results, the active pending route, and queued suffix, with matching name/arguments/ordinal and no
   extra entries? The current maps contain all pieces but expose separately locked methods, delete routes during batch
   drain, and can retain metadata after its loop link disappears.
2. For a continuation redelivery, what exact current evidence distinguishes retained current request, retained prior
   request, and true retained absence while avoiding a second prompt append in the same process?
3. Which structural checks are universally valid for generic provider DTOs, and which are required only at the
   agentic-loop accepting boundary? Direct provider callers use product-local RequestIDs.
4. Which birth observations are business lifecycle signals versus attempt/debug observations? Current created counter,
   active gauge, created log, created event, and trajectory start do not share one durable-done boundary.
5. Which existing durable outward consequence can represent busy and terminal refusal for both user-routed and
   channel-less rule/workflow tasks? No loop-owned refusal payload/subject exists; Retry is not a declared queue.
6. Which approval-gate failures occur before mutation, after mutation, or after a valid pending event is built, and
   what current durable evidence—if any—distinguishes them? Today all are logged and returned as success to the
   component.
7. Can the existing created event, graph birth fact, or terminal TaskID carrier prove a continuation's current request
   without scan/history? Their subjects, current-value semantics, and retention windows presently cover different
   slices of the task lifecycle.
