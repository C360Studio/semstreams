# Inventory: task 4 loop task, response, and tool-result settlement
base: af829616305afa039dac0550efa78d07e856dd5f

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:138` — `## 4. Loop task and response settlement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:140` — `- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:145` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:149` — `- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:3` — `### Requirement: All six loop input classes settle after owner-specific durable done`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:58` — `#### Scenario: Required output publication fails`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:65` — `#### Scenario: Duplicate is proven applied`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:70` — `#### Scenario: Missing process correlation is not proof of staleness`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:76` — `#### Scenario: Required correlation conflicts`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:122` — `### Requirement: Loop recovery is lane-specific and read-through`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:127` — `#### Scenario: Model response arrives after replacement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:134` — `#### Scenario: Tool result arrives after replacement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:152` — `### Requirement: Loop task, request, and tool work use only required correlation`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:163` — `#### Scenario: Task mapping is stable across redelivery`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:169` — `#### Scenario: Request or execution correlation conflicts`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:175` — `#### Scenario: Ordinary required publication repeats`

## Spellings of the fact

### Payload and durable-correlation fields

- `agentic/user_types.go:313` — `type TaskMessage struct {`
- `agentic/user_types.go:314` — `LoopID          string `json:"loop_id,omitempty"` // loop to continue, or empty for new`
- `agentic/user_types.go:315` — `TaskID          string `json:"task_id"``
- `agentic/user_types.go:407` — `func (t TaskMessage) Validate() error {`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `agentic/types.go:170` — `RequestID    string      `json:"request_id"``
- `agentic/types.go:180` — `func (r AgentResponse) Validate() error {`
- `agentic/tools.go:629` — `type ToolResult struct {`
- `agentic/tools.go:630` — `CallID      string         `json:"call_id"``
- `agentic/tools.go:637` — `LoopID      string         `json:"loop_id,omitempty"``
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `agentic/tools.go:646` — `func (t ToolResult) Validate() error {`
- `agentic/state.go:48` — `type LoopEntity struct {`
- `agentic/state.go:50` — `TaskID             string                `json:"task_id"``
- `agentic/state.go:56` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID`
- `agentic/state.go:102` — `func (e *LoopEntity) Validate() error {`

### Input bindings and settlement owner

- `processor/agentic-loop/component.go:116` — `type streamConsumerBinding struct {`
- `processor/agentic-loop/component.go:123` — `type inputHandler func(context.Context, []byte) error`
- `processor/agentic-loop/component.go:883` — `func (c *Component) setupSubscriptions(setupCtx, consumerCtx context.Context) error {`
- `processor/agentic-loop/component.go:907` — `case "agent.task":`
- `processor/agentic-loop/component.go:908` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:909` — `case "agent.response":`
- `processor/agentic-loop/component.go:910` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:911` — `case "tool.result":`
- `processor/agentic-loop/component.go:912` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/component.go:940` — `func (c *Component) setupConsumer(`
- `processor/agentic-loop/component.go:1048` — `policy, policyErr := newLoopHeartbeatDeliveryPolicy(setupCtx, cfg, heartbeatInterval, port.Name, handler)`
- `processor/agentic-loop/component.go:1054` — `result, admitted := consumeAdmittedDelivery(msgCtx, msg, policy, admission)`
- `processor/agentic-loop/component.go:1087` — `binding := newStreamConsumerBinding(handle)`
- `processor/agentic-loop/component.go:1093` — `c.consumers = append(c.consumers, binding)`
- `processor/agentic-loop/component.go:1140` — `func newLoopHeartbeatDeliveryPolicy(`
- `processor/agentic-loop/component.go:1162` — `handlerErr := handler(workCtx, data)`
- `processor/agentic-loop/component.go:1164` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1168` — `return natsclient.DeliveryDecisionTerminate, handlerErr`
- `processor/agentic-loop/component.go:1170` — `return natsclient.DeliveryDecisionRetry, handlerErr`
- `processor/agentic-loop/delivery_owner.go:74` — `func consumeAdmittedDelivery(`
- `processor/agentic-loop/delivery_owner.go:83` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `processor/agentic-loop/delivery_owner.go:92` — `func (b *streamConsumerBinding) drain() {`

### Task birth and partial-birth seams

- `processor/agentic-loop/component.go:100` — `// pendingTaskResults retains the not-yet-published spawn result when a`
- `processor/agentic-loop/component.go:104` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1231` — `func (c *Component) taskInputHandler(workTimeout time.Duration) inputHandler {`
- `processor/agentic-loop/component.go:1244` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1247` — `c.logger.Error("Failed to unmarshal BaseMessage", "error", err)`
- `processor/agentic-loop/component.go:1248` — `return nil`
- `processor/agentic-loop/component.go:1283` — `c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)`
- `processor/agentic-loop/component.go:1284` — `return nil`
- `processor/agentic-loop/component.go:1287` — `if !result.Created {`
- `processor/agentic-loop/component.go:1293` — `return nil`
- `processor/agentic-loop/component.go:1322` — `if err := c.graphWriter.WriteSpawnIdentity(ctx, result.LoopID, task); err != nil {`
- `processor/agentic-loop/component.go:1331` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1344` — `c.clearPendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1354` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1357` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1358` — `return nil`
- `processor/agentic-loop/handlers.go:811` — `func (h *MessageHandler) HandleTask(ctx context.Context, task TaskMessage) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:830` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:874` — `loopID, err = h.loopManager.CreateLoop(task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/handlers.go:1035` — `func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:1048` — `h.loopManager.TrackRequest(request.RequestID, loopID)`
- `processor/agentic-loop/handlers.go:1084` — `Created: true,`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:223` — `entity := agentic.NewLoopEntity(loopID, taskID, role, model, maxIter)`
- `processor/agentic-loop/state.go:225` — `m.loops[loopID] = &entity`
- `processor/agentic-loop/state.go:305` — `func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {`

### Response path and originating evidence

- `processor/agentic-loop/component.go:1476` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1482` — `entity, _ := c.handler.GetLoop(loopID)`
- `processor/agentic-loop/component.go:1484` — `result, err := c.handler.HandleModelResponse(ctx, loopID, *response)`
- `processor/agentic-loop/component.go:1488` — `return nil`
- `processor/agentic-loop/component.go:1492` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:1510` — `func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, string, bool) {`
- `processor/agentic-loop/component.go:1529` — `loopID := c.findLoopIDForRequest(responsePtr.RequestID)`
- `processor/agentic-loop/component.go:1531` — `c.logger.Warn("No loop found for request", "request_id", responsePtr.RequestID)`
- `processor/agentic-loop/component.go:1535` — `return nil, "", false`
- `processor/agentic-loop/state.go:72` — `requestToLoop          map[string]string                   // requestID -> loopID`
- `processor/agentic-loop/state.go:772` — `func (m *LoopManager) TrackRequest(requestID, loopID string) {`
- `processor/agentic-loop/state.go:775` — `m.requestToLoop[requestID] = loopID`
- `processor/agentic-loop/state.go:1165` — `func (m *LoopManager) GetLoopForRequestWithRecovery(requestID string) (string, bool) {`
- `processor/agentic-loop/state.go:1179` — `m.TrackRequest(requestID, loopID)`
- `processor/agentic-loop/handlers.go:1126` — `func (h *MessageHandler) HandleModelResponse(ctx context.Context, loopID string, response agentic.AgentResponse) (HandlerResult, error) {`

### Tool-result path and task-5 fence

- `processor/agentic-loop/component.go:1857` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1860` — `c.logger.Error("Failed to unmarshal BaseMessage", "error", err)`
- `processor/agentic-loop/component.go:1861` — `return nil`
- `processor/agentic-loop/component.go:1885` — `c.logger.Warn("No loop found for tool execution",`
- `processor/agentic-loop/component.go:1890` — `return nil`
- `processor/agentic-loop/component.go:1919` — `c.logger.Error("Failed to handle tool result", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:1920` — `return nil`
- `processor/agentic-loop/component.go:1943` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop         map[string]string                   // executionID -> loopID`
- `processor/agentic-loop/state.go:787` — `func (m *LoopManager) TrackToolCall(executionID, loopID string) {`
- `processor/agentic-loop/state.go:880` — `func (m *LoopManager) StoreToolResult(loopID string, result agentic.ToolResult) error {`
- `processor/agentic-loop/state.go:899` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/state.go:924` — `func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {`
- `processor/agentic-loop/state.go:1190` — `func (m *LoopManager) GetLoopForToolCallWithRecovery(executionID string) (string, bool) {`
- `processor/agentic-loop/handlers.go:2210` — `func (h *MessageHandler) HandleToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:2276` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`
- `processor/agentic-loop/handlers.go:2469` — `func (h *MessageHandler) handleToolsComplete(`
- `processor/agentic-loop/handlers.go:2528` — `allResults := h.loopManager.GetAndClearToolResults(loopID)`
- `processor/agentic-loop/handlers.go:2582` — `h.loopManager.TrackRequest(request.RequestID, loopID)`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:153` — `## 5. Tool result and completed outcome`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:164` — `- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,`

### KV, publication, Store, graph, and trajectory effects

- `processor/agentic-loop/component.go:62` — `loopsBucket           jetstream.KeyValue`
- `processor/agentic-loop/component.go:793` — `loopsBucket, err := js.KeyValue(ctx, c.config.LoopsBucket)`
- `processor/agentic-loop/component.go:805` — `c.loopsBucket = loopsBucket`
- `processor/agentic-loop/component.go:1304` — `c.recordTrajectoryObservations(ctx, result)`
- `processor/agentic-loop/component.go:1365` — `return c.graphWriter.WriteLineageTriples(ctx, loopID, related)`
- `processor/agentic-loop/component.go:1547` — `func (c *Component) handleLoopFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, reason string, err error) {`
- `processor/agentic-loop/component.go:1558` — `c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", err.Error())`
- `processor/agentic-loop/component.go:1589` — `func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string) {`
- `processor/agentic-loop/component.go:1621` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1709` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1712` — `c.recordHandlerResultTrajectory(ctx, result)`
- `processor/agentic-loop/component.go:1713` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1742` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1768` — `c.graphWriter.WriteLoopCompletion(bctx, completion, evidenceIncomplete)`
- `processor/agentic-loop/component.go:1796` — `c.graphWriter.WriteSyntheticDecide(bctx, req.LoopID, req.Reason)`
- `processor/agentic-loop/component.go:1823` — `c.graphWriter.WriteLoopFailure(bctx, failure, evidenceIncomplete)`
- `processor/agentic-loop/component.go:1951` — `func (c *Component) publishResults(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1957` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:2018` — `func (c *Component) persistCompletionState(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {`
- `processor/agentic-loop/component.go:2030` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2093` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:2108` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/graph_writer.go:347` — `func (w *graphWriter) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) error {`
- `processor/agentic-loop/graph_writer.go:447` — `func (w *graphWriter) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {`

### Lifecycle owner

- `processor/agentic-loop/component.go:476` — `func (c *Component) Start(ctx context.Context) (startErr error) {`
- `processor/agentic-loop/component.go:532` — `if err := c.setupSubscriptions(runCtx, runCtx); err != nil {`
- `processor/agentic-loop/component.go:647` — `func (c *Component) Stop(ctx context.Context) error {`
- `processor/agentic-loop/component.go:699` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-loop/component.go:731` — `for i := range c.consumers {`
- `processor/agentic-loop/component.go:733` — `binding.drain()`
- `processor/agentic-loop/component.go:734` — `closed := binding.handle.Closed()`

### Per-loop in-process owners, writers, readers, and release

| Aggregate | Owner/declaration | Production writer | Production reader | Terminal release |
|---|---|---|---|---|
| current loop entity | `processor/agentic-loop/state.go:62` — `loops map[string]*agentic.LoopEntity` | `processor/agentic-loop/state.go:225` — `m.loops[loopID] = &entity` | `processor/agentic-loop/state.go:318` — `func (m *LoopManager) GetLoop(loopID string)` | `processor/agentic-loop/state.go:496` — `delete(m.loops, loopID)` |
| conversation/context | `processor/agentic-loop/state.go:63` — `contextManagers map[string]*ContextManager` | `processor/agentic-loop/state.go:235` — `m.contextManagers[loopID] = NewContextManager(...)` | `processor/agentic-loop/state.go:534` — `func (m *LoopManager) GetContextManager(loopID string)` | `processor/agentic-loop/state.go:499` — `delete(m.contextManagers, loopID)` |
| outstanding calls | `processor/agentic-loop/state.go:64` — `pendingTools map[string]map[string]bool` | `processor/agentic-loop/state.go:679` — `func (m *LoopManager) AddPendingTool(loopID, callID string)` | `processor/agentic-loop/state.go:726` — `func (m *LoopManager) AllToolsComplete(loopID string)` | `processor/agentic-loop/state.go:497` — `delete(m.pendingTools, loopID)` |
| serial call queue | `processor/agentic-loop/state.go:65` — `queuedToolCalls map[string][]agentic.ToolCall` | `processor/agentic-loop/state.go:735` — `func (m *LoopManager) QueueToolCalls(loopID string, calls []agentic.ToolCall)` | `processor/agentic-loop/state.go:742` — `func (m *LoopManager) DequeueToolCall(loopID string)` | `processor/agentic-loop/state.go:498` — `delete(m.queuedToolCalls, loopID)` |
| tool definitions | `processor/agentic-loop/state.go:66` — `cachedTools ... // runtime cache, not persisted` | `processor/agentic-loop/state.go:541` — `func (m *LoopManager) CacheTools(...)` | `processor/agentic-loop/state.go:548` — `func (m *LoopManager) GetCachedTools(...)` | `processor/agentic-loop/state.go:500` — `delete(m.cachedTools, loopID)` |
| tool choice | `processor/agentic-loop/state.go:67` — `cachedToolChoice ... // runtime cache, not persisted` | `processor/agentic-loop/state.go:555` — `func (m *LoopManager) CacheToolChoice(...)` | `processor/agentic-loop/state.go:562` — `func (m *LoopManager) GetCachedToolChoice(...)` | `processor/agentic-loop/state.go:501` — `delete(m.cachedToolChoice, loopID)` |
| task metadata | `processor/agentic-loop/state.go:68` — `cachedMetadata ... // domain context, not persisted` | `processor/agentic-loop/state.go:570` — `func (m *LoopManager) CacheMetadata(...)` | `processor/agentic-loop/state.go:581` — `func (m *LoopManager) GetCachedMetadata(...)` | `processor/agentic-loop/state.go:502` — `delete(m.cachedMetadata, loopID)` |
| request timeout | `processor/agentic-loop/state.go:69` — `cachedRequestTimeout ... // not persisted` | `processor/agentic-loop/state.go:590` — `func (m *LoopManager) CacheRequestTimeout(...)` | `processor/agentic-loop/state.go:598` — `func (m *LoopManager) GetCachedRequestTimeout(...)` | `processor/agentic-loop/state.go:503` — `delete(m.cachedRequestTimeout, loopID)` |
| response format | `processor/agentic-loop/state.go:70` — `cachedResponseFormat ... // not persisted` | `processor/agentic-loop/state.go:608` — `func (m *LoopManager) CacheResponseFormat(...)` | `processor/agentic-loop/state.go:618` — `func (m *LoopManager) GetCachedResponseFormat(...)` | `processor/agentic-loop/state.go:504` — `delete(m.cachedResponseFormat, loopID)` |
| original prompt | `processor/agentic-loop/state.go:71` — `taskPrompts map[string]string` | `processor/agentic-loop/state.go:627` — `func (m *LoopManager) CacheTaskPrompt(...)` | `processor/agentic-loop/state.go:634` — `func (m *LoopManager) GetTaskPrompt(...)` | `processor/agentic-loop/state.go:505` — `delete(m.taskPrompts, loopID)` |
| request route | `processor/agentic-loop/state.go:72` — `requestToLoop map[string]string` | `processor/agentic-loop/state.go:772` — `func (m *LoopManager) TrackRequest(...)` | `processor/agentic-loop/state.go:779` — `func (m *LoopManager) GetLoopForRequest(...)` | `processor/agentic-loop/state.go:509` — `for k, owner := range m.requestToLoop` |
| execution route | `processor/agentic-loop/state.go:73` — `toolCallToLoop map[string]string` | `processor/agentic-loop/state.go:787` — `func (m *LoopManager) TrackToolCall(...)` | `processor/agentic-loop/state.go:872` — `func (m *LoopManager) GetLoopForToolCall(...)` | `processor/agentic-loop/state.go:515` — `for executionID, owner := range m.toolCallToLoop` |
| execution name/arguments/ordinal | `processor/agentic-loop/state.go:74` — `executionIDToName map[string]string` | `processor/agentic-loop/state.go:795` — `func (m *LoopManager) TrackToolName(...)` | `processor/agentic-loop/state.go:802` — `func (m *LoopManager) GetToolName(...)` | `processor/agentic-loop/state.go:526` — `func (m *LoopManager) deleteToolMetadataLocked(...)` |
| execution arguments | `processor/agentic-loop/state.go:75` — `executionIDToArguments map[string]map[string]any` | `processor/agentic-loop/state.go:810` — `func (m *LoopManager) TrackToolArguments(...)` | `processor/agentic-loop/state.go:817` — `func (m *LoopManager) GetToolArguments(...)` | `processor/agentic-loop/state.go:528` — `delete(m.executionIDToArguments, executionID)` |
| execution ordinal | `processor/agentic-loop/state.go:76` — `executionIDToOrdinal map[string]uint32` | `processor/agentic-loop/state.go:830` — `func (m *LoopManager) TrackToolOrdinal(...)` | `processor/agentic-loop/state.go:837` — `func (m *LoopManager) GetToolOrdinal(...)` | `processor/agentic-loop/state.go:529` — `delete(m.executionIDToOrdinal, executionID)` |
| request timer | `processor/agentic-loop/state.go:77` — `requestStartTimes map[string]time.Time` | `processor/agentic-loop/state.go:844` — `func (m *LoopManager) TrackRequestStart(...)` | `processor/agentic-loop/state.go:851` — `func (m *LoopManager) GetRequestStart(...)` | `processor/agentic-loop/state.go:512` — `delete(m.requestStartTimes, k)` |
| execution timer | `processor/agentic-loop/state.go:78` — `executionStartTimes map[string]time.Time` | `processor/agentic-loop/state.go:858` — `func (m *LoopManager) TrackToolStart(...)` | `processor/agentic-loop/state.go:865` — `func (m *LoopManager) GetToolStart(...)` | `processor/agentic-loop/state.go:530` — `delete(m.executionStartTimes, executionID)` |
| truncation retry count | `processor/agentic-loop/state.go:87` — `truncationRetryAttempts map[string]int` | `processor/agentic-loop/state.go:405` — `m.truncationRetryAttempts[loopID]++` | `processor/agentic-loop/state.go:406` — `return m.truncationRetryAttempts[loopID]`; `processor/agentic-loop/state.go:416` — forward-progress reset | `processor/agentic-loop/state.go:506` — `delete(m.truncationRetryAttempts, loopID)` |
| transient trajectory | `processor/agentic-loop/handlers.go:115` — `trajectoryManager *trajectoryManager` | `processor/agentic-loop/trajectory.go:24` — `func (m *trajectoryManager) startTrajectory(...)` | `processor/agentic-loop/trajectory.go:58` — `func (m *trajectoryManager) getTrajectory(...)` | `processor/agentic-loop/trajectory.go:51` — `func (m *trajectoryManager) discardTrajectory(...)` |
| durable-fact attempt ordinal cache | `processor/agentic-loop/trajectory_recorder.go:127` — `ordinalByLoop map[string]*trajectoryLoopOrdinal` | `processor/agentic-loop/trajectory_recorder.go:273` — `r.ordinalByLoop[loopID] = state` | `processor/agentic-loop/trajectory_recorder.go:269` — exact loop read; `processor/agentic-loop/trajectory_recorder.go:250` — initial value comes from `maximumVisibleAttemptOrdinal` | `processor/agentic-loop/trajectory_recorder.go:140` — initialized at recorder construction; no terminal delete is present |
| observed per-loop audit loss | `processor/agentic-loop/trajectory_observability.go:77` — `loops map[string]struct{}` | `processor/agentic-loop/trajectory_observability.go:91` — `l.loops[loopID] = struct{}{}` | `processor/agentic-loop/trajectory_observability.go:118` — `func (l *loopAuditLoss) observed(loopID string)` | `processor/agentic-loop/trajectory_observability.go:135` — `delete(l.loops, loopID)`; `processor/agentic-loop/trajectory_observability.go:108` — `allLoops` is a separate one-way latch |
| task partial-birth result | `processor/agentic-loop/component.go:104` — `pendingTaskResults map[string]HandlerResult` | `processor/agentic-loop/component.go:1368` — `func (c *Component) rememberPendingTaskResult(...)` | `processor/agentic-loop/component.go:1377` — `func (c *Component) pendingTaskResult(...)` | `processor/agentic-loop/component.go:1384` — `func (c *Component) clearPendingTaskResult(...)` |

#### Additional map pins used by the owner table

- `processor/agentic-loop/state.go:87` — `truncationRetryAttempts map[string]int`
- `processor/agentic-loop/state.go:405` — `m.truncationRetryAttempts[loopID]++`
- `processor/agentic-loop/state.go:406` — `return m.truncationRetryAttempts[loopID]`
- `processor/agentic-loop/state.go:413` — `func (m *LoopManager) ResetTruncationRetry(loopID string) {`
- `processor/agentic-loop/state.go:416` — `delete(m.truncationRetryAttempts, loopID)`
- `processor/agentic-loop/state.go:506` — `delete(m.truncationRetryAttempts, loopID)`
- `processor/agentic-loop/trajectory_recorder.go:127` — `ordinalByLoop map[string]*trajectoryLoopOrdinal`
- `processor/agentic-loop/trajectory_recorder.go:140` — `ordinalByLoop: make(map[string]*trajectoryLoopOrdinal),`
- `processor/agentic-loop/trajectory_recorder.go:250` — `maxOrdinal, err := maximumVisibleAttemptOrdinal(ctx, r.bucket, loopID)`
- `processor/agentic-loop/trajectory_recorder.go:269` — `state := r.ordinalByLoop[loopID]`
- `processor/agentic-loop/trajectory_recorder.go:273` — `r.ordinalByLoop[loopID] = state`
- `processor/agentic-loop/trajectory_observability.go:75` — `type loopAuditLoss struct {`
- `processor/agentic-loop/trajectory_observability.go:77` — `loops    map[string]struct{}`
- `processor/agentic-loop/trajectory_observability.go:81` — `func (l *loopAuditLoss) observe(loopID string) {`
- `processor/agentic-loop/trajectory_observability.go:90` — `l.loops[loopID] = struct{}{}`
- `processor/agentic-loop/trajectory_observability.go:109` — `func (l *loopAuditLoss) observeAllLoops() {`
- `processor/agentic-loop/trajectory_observability.go:112` — `l.allLoops = true`
- `processor/agentic-loop/trajectory_observability.go:118` — `func (l *loopAuditLoss) observed(loopID string) bool {`
- `processor/agentic-loop/trajectory_observability.go:124` — `_, ok := l.loops[loopID]`
- `processor/agentic-loop/trajectory_observability.go:131` — `func (l *loopAuditLoss) release(loopID string) {`
- `processor/agentic-loop/trajectory_observability.go:134` — `delete(l.loops, loopID)`

### Durable facts, runtime caches, and reconstruction task ownership

- `processor/agentic-loop/component.go:2093` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:2108` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `agentic/state.go:56` — `PendingToolResults map[string]ToolResult`
- `processor/agentic-loop/state.go:66` — `cachedTools            map[string][]agentic.ToolDefinition // loopID -> tools (runtime cache, not persisted)`
- `processor/agentic-loop/state.go:67` — `cachedToolChoice       map[string]*agentic.ToolChoice      // loopID -> tool choice (runtime cache, not persisted)`
- `processor/agentic-loop/state.go:68` — `cachedMetadata         map[string]map[string]any           // loopID -> metadata (domain context, not persisted)`
- `processor/agentic-loop/state.go:69` — `cachedRequestTimeout   map[string]string                   // loopID -> request timeout (from TaskMessage.Timeout, not persisted)`
- `processor/agentic-loop/state.go:70` — `cachedResponseFormat   map[string]*agentic.ResponseFormat  // loopID -> response_format (from TaskMessage.ResponseFormat, not persisted)`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:145` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:164` — `- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:182` — `- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:201` — `- [ ] 6.7 Implement one mixed `AGENT_LOOPS` classifier for canonical current `LoopEntity` keys, activity-only`

### Terminal release chain

- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:64` — `c.handler.trajectoryManager.discardTrajectory(loopID)`
- `processor/agentic-loop/trajectory_handler_wiring.go:65` — `c.trajectoryAuditLoss.release(loopID)`
- `processor/agentic-loop/trajectory_handler_wiring.go:67` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/state.go:492` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-loop/component.go:1550` — `defer c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1746` — `c.releaseLoopTransientState(result.LoopID)`
- `processor/agentic-loop/component.go:1917` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2281` — `c.releaseLoopTransientState(loopID)`

### Trajectory fact and evidence I/O disposition

- `processor/agentic-loop/trajectory_recorder.go:68` — `type trajectoryFactBucket interface {`
- `processor/agentic-loop/trajectory_recorder.go:119` — `// evidence resolution goes through StoreRegistry on every operation.`
- `processor/agentic-loop/trajectory_recorder.go:217` — `_, createErr := r.bucket.Create(ctx, key, encoded)`
- `processor/agentic-loop/trajectory_recorder.go:225` — `entry, getErr := r.bucket.Get(ctx, key)`
- `processor/agentic-loop/trajectory_recorder.go:226` — `if getErr == nil && bytes.Equal(entry.Value(), encoded) {`
- `processor/agentic-loop/trajectory_recorder.go:231` — `r.fail(ctx, observation, attempt.ID, trajectoryStageFactVerify, trajectoryReasonIntegrity,`
- `processor/agentic-loop/trajectory_recorder.go:239` — `r.fail(ctx, observation, attempt.ID, stage, trajectoryReasonBackend,`
- `processor/agentic-loop/trajectory_evidence.go:45` — `store, ok := r.stores.Store(r.storageInstance)`
- `processor/agentic-loop/trajectory_evidence.go:53` — `existing, getErr := store.Get(ctx, key)`
- `processor/agentic-loop/trajectory_evidence.go:55` — `case getErr == nil && bytes.Equal(existing, encoded):`
- `processor/agentic-loop/trajectory_evidence.go:60` — `capture.failure = agentic.TrajectoryEvidenceFailureIntegrity`
- `processor/agentic-loop/trajectory_evidence.go:70` — `putErr := store.Put(ctx, key, encoded)`
- `processor/agentic-loop/trajectory_evidence.go:79` — `verifyStore, ok := r.stores.Store(r.storageInstance)`
- `processor/agentic-loop/trajectory_evidence.go:81` — `verified, verifyErr := verifyStore.Get(ctx, key)`
- `processor/agentic-loop/trajectory_handler_wiring.go:175` — `c.trajectoryRecorder.record(ctx, observation)`
- `processor/agentic-loop/trajectory_handler_wiring.go:187` — `c.reportTrajectoryAuditFailure(trajectoryAuditFailure{`
- `processor/agentic-loop/trajectory_observability.go:137` — `func (c *Component) reportTrajectoryAuditFailure(failure trajectoryAuditFailure) {`

### Current task-birth and terminal error disposition

- `processor/agentic-loop/component.go:1246` — `if err != nil {`
- `processor/agentic-loop/component.go:1253` — `c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))`
- `processor/agentic-loop/component.go:1279` — `c.logger.Warn("Task refused — the loop it names still has work in flight",`
- `processor/agentic-loop/component.go:1283` — `c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)`
- `processor/agentic-loop/component.go:1354` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1357` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1559` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:1571` — `c.publishFailureEvents(ctx, loopID, reason, err.Error())`
- `processor/agentic-loop/component.go:1621` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/graph_writer.go:280` — `func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:297` — `w.logger.Warn("graph_writer: failed to write loop completion batch",`
- `processor/agentic-loop/graph_writer.go:306` — `func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:323` — `w.logger.Warn("graph_writer: failed to write loop failure batch",`

### Mixed `AGENT_LOOPS` key owners and reader collision

| Key class | Writer/reader | Concrete pin |
|---|---|---|
| bare `<loopID>` canonical loop | agentic-loop writer | `processor/agentic-loop/component.go:2108` — `loopsBucket.Put(ctx, loopID, data)` |
| bare `<loopID>` research-pipeline loop | graphresearch writer | `frameworkcapabilities/graphresearch/executor.go:248` — constructs a research-pipeline `LoopEntity`; `frameworkcapabilities/graphresearch/executor.go:267` — calls `CreateLoopEntity` |
| bare `<loopID>` research-pipeline loop | NATS adapter | `frameworkcapabilities/graphresearch/register_tool.go:91` — `CreateLoopEntity`; `frameworkcapabilities/graphresearch/register_tool.go:92` — KV `Create(ctx, loopID, value)` |
| `COMPLETE_<loopID>` agent terminal | agentic-loop writer | `processor/agentic-loop/component.go:2029` — constructs `COMPLETE_` key; `processor/agentic-loop/component.go:2030` — KV `Put` |
| `research.request.received.<loopID>` | graphresearch writer | `frameworkcapabilities/graphresearch/executor.go:32` — declares trigger prefix; `frameworkcapabilities/graphresearch/register_tool.go:101` — KV `Put` |
| `classify.complete.` / `classify.snapshot.` | research pipeline | `processor/research-graph-classify/adapters.go:238` — completion key; `processor/research-graph-classify/adapters.go:241` — snapshot key |
| `route.complete.` / `route.snapshot.` | research pipeline | `processor/research-graph-route/adapters.go:82` — completion input; `processor/research-graph-route/adapters.go:84` — snapshot key |
| `execute.complete.` / `execute.snapshot.` | research pipeline | `processor/research-graph-execute/adapters.go:367` — completion key; `processor/research-graph-execute/adapters.go:368` — snapshot key |
| `assess.complete.` / `assess.snapshot.` | research pipeline | `processor/research-graph-assess/adapters.go:83` — completion key; `processor/research-graph-assess/adapters.go:84` — snapshot key |
| `search_result.complete.` / `synthesize.snapshot.` | research pipeline | `processor/research-graph-synthesize/adapters.go:77` — search-result key; `processor/research-graph-synthesize/adapters.go:80` — snapshot key |
| `COMPLETE_<loopID>` research result | research pipeline | `processor/research-graph-synthesize/adapters.go:159` — declares shared completion prefix; `processor/research-graph-synthesize/adapters.go:170` — writes `SearchResult` envelope to it |
| every non-`COMPLETE_` key | current dispatch reader | `processor/agentic-dispatch/http_activity.go:96` — every other key decodes as live `LoopEntity`; `processor/agentic-dispatch/http_activity.go:111` — unmarshals `agentic.LoopEntity` |
| exact bare `<loopID>` | dispatch terminal reader | `processor/agentic-dispatch/terminal_settlement.go:106` — KV `Get(ctx, loopID)`; `processor/agentic-dispatch/terminal_settlement.go:113` — unmarshals `LoopEntity` |
| mixed-key classifier ownership | task 6 | `openspec/changes/agentic-loop-restart-safety/tasks.md:206` — task 6.7 names canonical loop, `COMPLETE_`, and every research namespace |
| bucket acquisition/config authority | task 9 | `openspec/changes/agentic-loop-restart-safety/tasks.md:321` — task 9.8 enumerates owner acquisition cases; `openspec/changes/agentic-loop-restart-safety/tasks.md:324` — task 9.9 owns `loopbucket.AcquireOwner` |

### Concrete adjacent task fences

- `openspec/changes/agentic-loop-restart-safety/tasks.md:153` — `## 5. Tool result and completed outcome`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:164` — `- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:176` — `## 6. Dispatch edge gateway and approval continuation gate`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:182` — `- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:242` — `## 7. Control vocabulary, cancel, approval-response, and verdict fast lanes`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:262` — `- [ ] 7.6 Refactor cancel, approval-response, approved-verdict, and rejected-verdict through their four existing`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:291` — `## 9. AGENT admission, first-party publisher, and loop authority`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:324` — `- [ ] 9.9 Implement internal `loopbucket.AcquireOwner`: KeyValue first; create only for typed`

### Out-of-scope task 6/7 production owners sharing task 4 input and persistence seams

- `processor/agentic-loop/handlers.go:2349` — `func (h *MessageHandler) checkApprovalGate(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult, result *HandlerResult) bool {`
- `processor/agentic-loop/handlers.go:2386` — `func (h *MessageHandler) gateForApproval(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult) (*PublishedMessage, error) {`
- `processor/agentic-loop/approval_response_handler.go:32` — `func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (result HandlerResult, err error) {`
- `processor/agentic-loop/approval_response_handler.go:163` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/approval_response_handler.go:204` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/state.go:435` — `func (m *LoopManager) ResolveApprovalIfPending(loopID, callID string) (agentic.PendingApprovalState, bool, error) {`
- `processor/agentic-loop/component.go:2175` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2195` — `case agentic.SignalCancel:`
- `processor/agentic-loop/component.go:2209` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) error {`
- `processor/agentic-loop/component.go:2311` — `func (c *Component) handleToolCallVerdictMessage(_ context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2329` — `return dispatcher.HandleVerdict(decision, executionID, data)`
- `processor/agentic-loop/governance_dispatcher.go:369` — `func (d *enforceDispatcher) registerWaiter(callID string) chan verdictArrival {`
- `processor/agentic-loop/governance_dispatcher.go:383` — `func (d *enforceDispatcher) lookupWaiter(callID string) (chan verdictArrival, bool) {`
- `processor/agentic-loop/governance_dispatcher.go:491` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:506` — `ch, ok := d.lookupWaiter(executionID)`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:182` — `- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:262` — `- [ ] 7.6 Refactor cancel, approval-response, approved-verdict, and rejected-verdict through their four existing`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/design.md:418` — `Happy-path done is matching `LoopEntity` persistence, required graph birth, and PubAck for the initial`
- `openspec/changes/agentic-loop-restart-safety/design.md:432` — `Happy-path done is committed `LoopEntity` and `COMPLETE_<loopID>` plus required terminal event PubAck. Transient`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:176` — `## 6. Dispatch edge gateway and approval continuation gate`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:242` — `## 7. Control vocabulary, cancel, approval-response, and verdict fast lanes`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:291` — `## 9. AGENT admission, first-party publisher, and loop authority`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:294` — `- [ ] 9.2 RED: add model/dispatch/governance/loop tests for caller-local requirements, divergent configs, resolved`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:321` — `- [ ] 9.8 RED: add fake and real-NATS loop-bucket tests for absent, matching retained, same/foreign-config race,`
- `openspec/changes/agentic-loop-restart-safety/inventory-task2-at-least-once-2026-09-04.md:1` — `# Task 2.5–2.6 At-Least-Once Publication Inventory`
- `openspec/changes/agentic-loop-restart-safety/inventory-task3-provider-settlement-postimpl-2026-09-05.md:1` — `# Inventory: task 3 provider settlement post-implementation`
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1147 — epic: make framework restart behavior explicit and provable
- #1155 — e2e(agentic): prove semantic-settlement quarantine and AgentRun redelivery across process replacement
- #759 — natsclient: establish semantic JetStream settlement as the restart-safety foundation
- #1239 — agentic-loop: pause/resume are advertised and unimplemented — PauseRequested is written twice, read never, and its comment promises a checkpoint that does not exist
- PR #1159 — fix(agentic-loop): preserve durable work across process restart
- PR #1156 — refactor(natsclient): add semantic delivery settlement

## Consumers

### Port and subject consumers

- `processor/agentic-loop/config.go:401` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:405` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:409` — `Name: "tool.result", Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:438` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/config.go:444` — `Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Description: "Agent model requests (JetStream)",`
- `processor/agentic-loop/config.go:447` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.*"}, StreamName: "AGENT"}, Description: "Tool execution requests (JetStream)",`
- `processor/agentic-loop/config.go:450` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Description: "Agent task completions (JetStream)",`
- `processor/agentic-loop/config.go:453` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Description: "Loop-created lifecycle events (JetStream)",`
- `processor/agentic-model/component.go:1091` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)`
- `processor/agentic-tools/component.go:1196` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "tool.result", result.ExecutionID)`
- `processor/agentic-model/config.go:133` — `Name: "agent.request", Config: component.JetStreamPort{`
- `processor/agentic-tools/config.go:127` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-dispatch/config.go:122` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-dispatch/config.go:126` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Required: false,`
- `agentic/agentrun/agentrun.go:829` — `FilterSubject: "agent.complete.*",`

### In-process readers

- `processor/agentic-loop/state.go:318` — `func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/handlers.go:2770` — `func (h *MessageHandler) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/handlers.go:2771` — `return h.loopManager.GetLoop(loopID)`
- `processor/agentic-loop/component.go:2098` — `entity, err := c.handler.GetLoop(loopID)`

### `gopls`-enumerated task, response, and tool-result handler callers

- `processor/agentic-loop/component.go:1235` — `err := c.handleTaskMessage(workCtx, data)`
- `processor/agentic-loop/create_vs_exists_fence_test.go:576` — `if err := c.handleTaskMessage(ctx, data); err != nil {`
- `processor/agentic-loop/component.go:910` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:84` — `newPolicy(t, "agent.response", c.handleResponseMessage)`
- `processor/agentic-loop/terminal_release_test.go:415` — `c.handleResponseMessage(ctx, respData)`
- `processor/agentic-loop/terminal_release_test.go:470` — `c.handleResponseMessage(ctx, respData)`
- `processor/agentic-loop/component.go:912` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/delivery_owner_test.go:110` — `newPolicy(t, "tool.result", c.handleToolResultMessage)`
- `processor/agentic-loop/terminal_release_test.go:404` — `c.handleToolResultMessage(ctx, toolData)`
- `processor/agentic-loop/trajectory_eviction_internal_test.go:118` — `component.handleToolResultMessage(context.Background(), data)`

### Adopter seam

- `openspec/project.md:14` — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- `openspec/project.md:30` — `- **SemStreams provides** the graph substrate: the KV-twofer and NATS/KV`
- `processor/agentic-loop/README.md:132` — `| agent.task | jetstream | agent.task.* | Task requests from external systems |`
- `processor/agentic-loop/README.md:133` — `| agent.response | jetstream | agent.response.> | Model responses from agentic-model |`
- `processor/agentic-loop/README.md:134` — `| tool.result | jetstream | tool.result.> | Tool results from agentic-tools |`
- `processor/agentic-loop/README.md:142` — `| agent.request | jetstream | agent.request.* | Model requests to agentic-model |`
- `processor/agentic-loop/README.md:143` — `| tool.execute | jetstream | tool.execute.* | Tool calls to agentic-tools |`
- `processor/agentic-loop/README.md:144` — `| agent.complete | jetstream | agent.complete.* | Loop completion events |`
- `processor/agentic-loop/README.md:151` — `| loops | AGENT_LOOPS | `{loop_id}` | Loop entity state |`
- `processor/agentic-loop/README.md:152` — `| loops | AGENT_LOOPS | `COMPLETE_{loop_id}` | Completion state for rules engine |`
- `processor/agentic-loop/config.go:54` — `LoopsBucket                       string                   `json:"loops_bucket" schema:"type:string,description:NATS KV bucket name for storing loop state,default:AGENT_LOOPS,category:advanced,required"``
- `processor/agentic-loop/config.go:58` — `Consumer                          ConsumerConfig           `json:"consumer" schema:"type:object,description:JetStream consumer tuning for long-running ports (agent.task/agent.response/tool.result),category:advanced"``

## Problem shape

- `processor/agentic-dispatch/terminal_settlement.go:91` — `func (c *Component) loadPersistedLoop(ctx context.Context, loopID string) (*agentic.LoopEntity, error) {`
- `processor/agentic-dispatch/terminal_settlement.go:106` — `entry, err := kv.Get(ctx, loopID)`
- `processor/agentic-dispatch/terminal_settlement.go:113` — `var persisted agentic.LoopEntity`
- `processor/agentic-dispatch/task_recovery.go:34` — `type retainedTaskEvidenceReader interface {`
- `processor/agentic-dispatch/task_recovery.go:42` — `func (r natsRetainedTaskEvidenceReader) ReadRetainedTask(`
- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-model/provider_settlement.go:37` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-model/provider_settlement.go:76` — `func (c *Component) readRetainedAgentResponse(`
- `natsclient/delivery_settlement.go:297` — `func SettleDelivery(msg jetstream.Msg, decision DeliveryDecision, cause error) DeliveryResult {`
- `natsclient/delivery_settlement.go:412` — `func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {`
- `natsclient/delivery_settlement.go:419` — `case DeliveryDecisionAck:`
- `natsclient/delivery_settlement.go:450` — `return msg.Ack()`

## Searches

- `git rev-parse HEAD` → `af829616305afa039dac0550efa78d07e856dd5f`
- `git status --short` → 0 entries before inventory creation
- `git grep -n '^## \(Purpose\|Product Boundary\)' -- openspec/project.md` → 2
- `gopls workspace_symbol -matcher=fuzzy <term>` for `taskInputHandler`, `responseInputHandler`, `toolResultInputHandler`, `handleTaskMessage`, `handleAgentResponse`, and `handleToolResult` → unavailable; all six failed during workspace load with `package unsafe is not in std (/usr/local/go/src/unsafe)`
- `gopls workspace_symbol -matcher=fuzzy taskInputHandler` with cached Go 1.25.11 and Go 1.26.3 roots → unavailable; both failed during workspace load with `package unsafe is not in std`
- `gopls references processor/agentic-loop/component.go:1244:21` → unavailable; workspace load failed and returned no package metadata
- `gopls call_hierarchy processor/agentic-loop/component.go:1244:21` → unavailable; workspace load failed and returned no package metadata
- `gopls references processor/agentic-loop/component.go:1476:21` → unavailable; workspace load failed and returned no package metadata
- `gopls references processor/agentic-loop/component.go:1857:21` → unavailable; workspace load failed and returned no package metadata
- `git grep -n -E '^4\.[123]|durable-input|LoopEntity|task input|tool-result|response input|log-and-ACK|log and ACK|partial-birth|partial birth|task birth' -- openspec/changes/agentic-loop-restart-safety openspec/specs docs/adr docs/operations/migration-*.md` → 83
- `git grep -n -E 'func \(.*\) (setup|start|stop|handle|process|publish|persist|save|load|recover|create|register|settle|on)[A-Za-z0-9_]*|func [A-Za-z0-9_]*(InputHandler|Handler|Binding|Consumer)|type .*Handler|type .*Binding|agent\.(task|response|tool_result|toolresult)' -- processor/agentic-loop agentic` → 615
- `git grep -n -E 'taskInputHandler|handleTaskMessage|handleResponseMessage|handleToolResultMessage|inputHandler|streamConsumerBinding' -- processor/agentic-loop` → 55
- `git grep -n -E 'agent\.task|agent\.response|tool\.result|agent\.request|agent\.created|agent\.complete|tool\.execute' -- processor/agentic-loop agentic openspec/specs docs/adr openspec/changes/agentic-loop-restart-safety docs/operations/migration-*.md` → 668
- `git grep -n -E 'loopsBucket\.(Get|GetRevision|Put|Watch|Keys)|LoopEntity|GetLoop\(' -- processor/agentic-loop` → 160
- `git grep -n -E 'PublishToStream|PublishedMessages|loopsBucket\.Put|WriteSpawnIdentity|WriteLineageTriples|WriteLoopCompletion|WriteLoopFailure|WriteSyntheticDecide|recordTrajectory|StoreRegistry|ObjectStore' -- processor/agentic-loop` → 250
- `git grep -n -E 'return nil|log-and-ACK|log and ACK|settled-drop|stale_request_id|stale_execution|No loop found|Failed to (handle|unmarshal|publish|persist)' -- processor/agentic-loop` → 263
- `git grep -n -E 'pendingTaskResults|task[-_ ]birth|partial[-_ ]birth|HasActiveLoopForTask|CreateLoopWithID|Created|rememberPendingTaskResult' -- processor/agentic-loop openspec/changes/agentic-loop-restart-safety` → 182
- `git grep -n -E 'TaskID|task_id|task-id|TASK_ID|LoopID|loop_id|loop-id|LOOP_ID|RequestID|request_id|request-id|REQUEST_ID|ExecutionID|execution_id|execution-id|EXECUTION_ID|CallID|call_id|call-id|CALL_ID|CallOrdinal|call_ordinal|call-ordinal|CALL_ORDINAL' -- processor/agentic-loop agentic` → 1934
- `git grep -n -E 'PendingToolResults|GetAndClearToolResults|StoreToolResult|partial batch|ordered batch|completed outcome|TOOL_CALL_OUTCOMES' -- processor/agentic-loop agentic openspec/changes/agentic-loop-restart-safety` → 99
- `git grep -n -E 'agent\.signal|approval_response|toolcall\.approved|toolcall\.rejected|agentstreamadmission|Restart-safe replay|AcquireOwner|loopbucket' -- processor/agentic-loop openspec/changes/agentic-loop-restart-safety` → 212
- `git grep -n -E 'LoopsBucket|loops_bucket|LOOPS_BUCKET|AGENT_LOOPS|ConsumerNameSuffix|consumer_name_suffix|CONSUMER_NAME_SUFFIX|heartbeat_interval|ack_wait|max_deliver|max_ack_pending' -- processor/agentic-loop agentic` → 132
- `git grep -n -E '^type (TaskMessage|AgentResponse|ToolResult|LoopEntity) struct|func \(.*(TaskMessage|AgentResponse|ToolResult|LoopEntity).*\) (Validate|Schema)' -- agentic` → 11
- `git grep -n -E 'spec: agentic-loop / (All six loop input classes settle after owner-specific durable done|Loop recovery is lane-specific and read-through|Loop task, request, and tool work use only required correlation)' -- processor/agentic-loop` → 11
- `git grep -n -E 'GetLastMsgForSubject|ReadRetained|retainedResponse|retained_response|retained-response|originating request|originating response' -- processor/agentic-loop` → 0
- `git grep -n -E 'func \(c \*Component\) (Start|Stop|cleanup)|setupSubscriptions|setupConsumer|newLoopHeartbeatDeliveryPolicy|ConsumeDeliveryWithHeartbeat|SettleDelivery|Closed\(|\.Drain\(' -- processor/agentic-loop` → 41
- `git grep -n -E 'AGENT_LOOPS|LoopEntity|agent\.task|agent\.response|tool\.result|restart|replacement|settlement|partial birth|log-and-ACK' -- openspec/specs docs/adr openspec/changes/agentic-loop-restart-safety docs/operations/migration-*.md` → 1505
- `git grep -n -E '4\.1|4\.2|4\.3|## 4\. Loop task and response settlement' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 4
- `git grep -n -E '5\.1|5\.2|5\.3|## 5\. Tool result and completed outcome' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 4
- `git grep -n -E '6\.[1-9]|7\.[1-8]|9\.[1-9]|9\.10|9\.11|## (6|7|9)\.' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 37
- `git grep -n -E 'adopter|external system|external → loop|framework|configured|configuration|port|subject|bucket' -- processor/agentic-loop/README.md processor/agentic-loop/doc.go processor/agentic-loop/config.go openspec/project.md` → 91
- `git grep -n -E 'agent\.tool\.result|agent_tool_result|agent-tool-result|tool_result|tool-result|tool\.result' -- processor/agentic-loop agentic openspec/changes/agentic-loop-restart-safety` → 113
- `git grep -n -E 'loopsBucket\.(Get|GetRevision|Put|Watch|Keys)|KeyValue.*Get|var .*LoopEntity|LoopEntity\{|json\.(Unmarshal|Marshal).*Loop|GetLoop\(' -- processor/agentic-loop` → 131
- `git grep -n -E 'GetLastMsgForSubject|agent\.request\.|agent\.response\.|ReadRetained|retained|originating|requestToLoop|toolCallToLoop|PendingToolResults|GetAndClearToolResults' -- processor/agentic-loop agentic` → 174
- `git grep -n -E 'DeleteLoop|discardTrajectory|pendingTaskResults|rememberPendingTaskResult|clearPendingTaskResult|Created|HasActiveLoopForTask|CreateLoopWithID|CreateLoop\(' -- processor/agentic-loop/component.go processor/agentic-loop/handlers.go processor/agentic-loop/state.go` → 39
- `git grep -n -E 'return nil|logger\.(Error|Warn)|publishResults\(ctx, result\)|persistLoopState\(ctx, result\.LoopID\)|handleLoopFailure\(' -- processor/agentic-loop/component.go` → 116
- `git grep -n -E '^func \(h \*MessageHandler\) (HandleTask|HandleModelResponse|HandleToolResult|create|build|handle|process)|PublishedMessages:|PublishedMessages = append|TrackRequest|TrackToolCall|AddToolResult|SetPending|PendingApproval|StoreRegistry|\.Store\(|\.Put\(' -- processor/agentic-loop/handlers.go processor/agentic-loop/state.go processor/agentic-loop/approval_response_handler.go processor/agentic-loop/governance_dispatcher.go` → 92
- `git grep -n -E '^func \(c \*Component\) (Start|Stop)|consumers|setupSubscriptions|loopsBucket|GetKeyValue|KeyValue\(|StoreRegistry|ObjectStore|GraphWriter|WriteSpawnIdentity|WriteLineageTriples' -- processor/agentic-loop/component.go processor/agentic-loop/graph_writer.go processor/agentic-loop/handlers.go processor/agentic-loop/state.go` → 47
- `git grep -n -E 'spec: agentic-loop / (All six loop input classes settle after owner-specific durable done|Loop recovery is lane-specific and read-through|Loop task, request, and tool work use only required correlation)' -- processor/agentic-loop` → 11
- `git grep -n -E 'task.*(redeliver|duplicate|birth|publication|persist)|response.*(cold|replacement|stale|duplicate|conflict|retained)|tool.result.*(replacement|stale|log|ACK)|process replacement|PubAck|partial' -- processor/agentic-loop/*_test.go` → 38
- `git grep -n -E '^type (TaskMessage|AgentResponse|ToolResult|LoopEntity) struct|func \(.*(TaskMessage|AgentResponse|ToolResult|LoopEntity).*\) (Validate|Schema)|json:"(task_id|loop_id|request_id|call_id|execution_id|call_ordinal|pending_tool_results)' -- agentic` → 31
- `git grep -n -E 'LoopsBucket|AGENT_LOOPS|agent\.task\.\*|agent\.response\.>|tool\.result\.>|agent\.request\.\*|agent\.created\.\*|agent\.complete\.\*|tool\.execute\.>' -- processor/agentic-loop agentic openspec/specs docs/adr openspec/changes/agentic-loop-restart-safety docs/operations/migration-*.md` → 518
- `git grep -n -E '^func \(c \*Component\) (Start|Stop|cleanup|setupSubscriptions|setupConsumer|taskInputHandler|handleTaskMessage|handleResponseMessage|extractAgentResponse|handleToolResultMessage|publishResults|persistHandlerResult|persistLoopState|persistCompletionState|handleLoopFailure|publishFailureEvents)|^func newLoopHeartbeatDeliveryPolicy|^func consumeAdmittedDelivery|^func \(b \*streamConsumerBinding\) drain' -- processor/agentic-loop/component.go processor/agentic-loop/delivery_owner.go` → 20
- `git grep -n -E '^func \(h \*MessageHandler\) (HandleTask|buildTaskRequest|HandleModelResponse|HandleToolResult|handleToolsComplete|handleCompleteResponse)|^func \(m \*LoopManager\) (CreateLoopWithID|HasActiveLoopForTask|GetLoop|TrackRequest|GetLoopForRequestWithRecovery|GetLoopForToolCallWithRecovery|StoreToolResult|GetAndClearToolResults|TrackToolCall)' -- processor/agentic-loop/handlers.go processor/agentic-loop/state.go` → 21
- `git grep -n -E 'PublishToStream\(|loopsBucket\.Put|WriteSpawnIdentity\(|WriteLineageTriples\(|WriteLoopCompletion\(|WriteLoopFailure\(|WriteSyntheticDecide\(|recordTrajectoryObservations\(|recordHandlerResultTrajectory\(' -- processor/agentic-loop/component.go processor/agentic-loop/graph_writer.go` → 22
- `git grep -n -E 'func NewHeartbeatDeliveryPolicy|type HeartbeatDeliveryPolicy|Decision|Classif|Quarantine|Terminate|Retry|Ack' -- natsclient/*heartbeat* natsclient/delivery*` → 261
- `git grep -n -E 'PublishToStream\(.*agent\.(task|response)|ResolveSubject\(.*"agent\.(task|response)|ResolveSubject\(.*"tool\.result|Name: "agent\.(task|response)"|Name: "tool\.result"|FilterSubject: "agent\.(task|response)|FilterSubject: "tool\.result' -- processor/agentic-dispatch processor/agentic-model processor/agentic-tools processor/agentic-loop processor/rule` → 56
- `git grep -n -E 'agent\.created|agent\.complete|tool\.execute|agent\.request' -- processor/agentic-dispatch processor/agentic-model processor/agentic-tools processor/agentic-loop agentic/agentrun` → 426
- `git grep -n -E '^## (7|9)\.|^- \[ \] (7|9)\.' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 19
- `git ls-files 'openspec/changes/agentic-loop-restart-safety/inventory*task4*' 'openspec/changes/agentic-loop-restart-safety/*inventory*'` → 10
- `git grep -n -E '^type retainedTaskEvidenceReader|^func \(.*\) ReadRetainedTask|GetLastMsgForSubject|^func \(c \*Component\) loadPersistedLoop|\.Get\(ctx, loopID\)|LoopEntity|Validate\(\)' -- processor/agentic-dispatch/task_recovery.go processor/agentic-dispatch/terminal_settlement.go processor/agentic-model/provider_settlement.go` → 14
- `git grep -n -E '^### Requirement: (All six loop input classes settle after owner-specific durable done|Loop recovery is lane-specific and read-through|Loop task, request, and tool work use only required correlation)|^#### Scenario: (Required output publication fails|Duplicate is proven applied|Missing process correlation is not proof of staleness|Required correlation conflicts|Model response arrives after replacement|Tool result arrives after replacement|Task mapping is stable across redelivery|Request or execution correlation conflicts|Ordinary required publication repeats)' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md` → 12
- `git grep -n -E '^- \[ \] 4\.[123]|^## 4\. Loop task and response settlement|^- \[ \] 5\.[123]|^## 5\. Tool result and completed outcome|^## 6\.|^## 7\.|^## 9\.' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 11
- `gh issue list --search 'agentic loop restart settlement' --state open --json number,title` → 8
- `gh issue list --search 'LoopEntity recovery' --state open --json number,title` → 2
- `gh issue list --search 'task birth agentic' --state open --json number,title` → 5
- `gh issue list --search 'tool result restart' --state open --json number,title` → 7
- `gh pr list --state open --json number,title,body,isDraft` → 5
- `openspec list` → 2 active changes
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls workspace_symbol -matcher=fuzzy LoopManager` → 100 symbol results; located the manager, every named map field, and its method surface (the process-global gopls cache emitted non-fatal write warnings).
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/state.go:62:2` → 34 references to `LoopManager.loops`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/trajectory_handler_wiring.go:63:21` → 13 references to `releaseLoopTransientState`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls call_hierarchy processor/agentic-loop/trajectory_handler_wiring.go:63:21` → 12 callers and 3 callees; callers are the four production terminal paths plus tests, and callees are `DeleteLoop`, `discardTrajectory`, and audit-loss `release`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls workspace_symbol -matcher=fuzzy pendingTaskResults` → 1 symbol result.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/component.go:104:2` → 6 references.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/handlers.go:115:2` → 31 references to `MessageHandler.trajectoryManager`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls workspace_symbol -matcher=fuzzy CreateLoopEntity` → 61 fuzzy symbol results; exact declarations were the interface method, production NATS adapter, and test fake.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls implementation frameworkcapabilities/graphresearch/executor.go:52:2` → 2 implementations: production NATS adapter and test fake.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls call_hierarchy frameworkcapabilities/graphresearch/executor.go:198:36` → failed after 2 partial results with a gopls nil-pointer panic; not used as structural evidence.
- `git grep -n -E 'releaseLoopTransientState|DeleteLoop\(|trajectoryManager|pendingTaskResults|contextManagers|pendingTools|queuedToolCalls|cachedTools|cachedToolChoice|cachedMetadata|taskPrompts|requestToLoop|toolCallToLoop|executionIDToName|requestStartTimes|toolStartTimes|toolCallOrdinals|requestTimeout|ResponseFormat|ToolChoice|Metadata' -- processor/agentic-loop/*.go` → 461.
- `git ls-files 'processor/agentic-loop/*evidence*.go'` → 2.
- `git grep -n -E 'captureEvidence|StoreRegistry|\.Get\(|\.Put\(|ObjectStore|resolve|verify' -- processor/agentic-loop/*evidence*.go processor/agentic-loop/trajectory_recorder.go` → 13.
- `git grep -n -E 'AGENT_LOOPS|LoopsBucket|loops_bucket|\.Put\(ctx,.*(loop|key)|\.Get\(ctx,.*(loop|key)|COMPLETE_|RESEARCH|research|LoopEntity' -- processor/agentic-loop processor/frameworkcapabilities/graphresearch processor/agentic-dispatch agentic` → 962; the named `processor/frameworkcapabilities/graphresearch` path does not exist and contributes no hits.
- `git ls-files | git grep --stdin -n 'frameworkcapabilities/graphresearch/\(executor\|register_tool\)\.go'` → failed: `git grep` has no `--stdin` option.
- `git grep -n -E 'func .*Execute|func RegisterTool|search_result\.complete|AGENT_LOOPS' -- '*executor.go' '*register_tool.go'` → 13.
- `git grep -n -E 'research\.(request\.received|classify\.(output|complete)|route\.(output|complete)|execute\.(output|complete)|assess\.(output|complete)|search[-_.]result|search_result)|AGENT_LOOPS' -- processor/research-* frameworkcapabilities/graphresearch agentic/research processor/agentic-dispatch` → 168.
- `git grep -n -E 'func loopStoreKey|\.Put\(|\.Get\(|\.Create\(' -- processor/research-graph-classify processor/research-graph-route processor/research-graph-execute processor/research-graph-assess processor/research-graph-synthesize` → 43.
- `git grep -n -E 'recorder\.record|recordTrajectoryBatchWithin|recordTrajectoryObservations|recordHandlerResultTrajectory|trajectoryAudit' -- processor/agentic-loop/trajectory_handler_wiring.go processor/agentic-loop/trajectory_observability.go processor/agentic-loop/component.go` → 30.
- `git grep -n -E 'publishFailureEvents\(|persistLoopState\(ctx, loopID\)|WriteLoop(Completion|Failure)\(' -- processor/agentic-loop/component.go processor/agentic-loop/graph_writer.go` → 8.
- `git grep -n -E '^func \(m \*LoopManager\) (Cache|GetCached|CacheTask|GetTask|Track|GetRequest|GetTool|Queue|Dequeue|HasQueued|ClearQueued|AddPending|RemovePending|GetPending|AllTools|DeleteLoop)' -- processor/agentic-loop/state.go` → 31.
- `git grep -n -E '^func \(m \*trajectoryManager\)' -- processor/agentic-loop/trajectory.go` → 4.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls workspace_symbol -matcher=fuzzy LoopManager 2>/dev/null | wc -l` → 100 (count-only rerun).
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls workspace_symbol -matcher=fuzzy CreateLoopEntity 2>/dev/null | wc -l` → 61 (count-only rerun).
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/handlers.go:115:2 2>/dev/null | wc -l` → 31 (count-only rerun).
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/component.go:1244:21` → 2 references: `taskInputHandler` and `TestBusyRefusalIsWarnedNotErrored`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls call_hierarchy processor/agentic-loop/component.go:1244:21` → 2 callers and 24 callees; the production caller is `taskInputHandler` and the test caller is `TestBusyRefusalIsWarnedNotErrored`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/component.go:1476:21` → 4 references: one production subscription binding and three test calls.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls call_hierarchy processor/agentic-loop/component.go:1476:21` → 4 callers and 8 callees; the production caller is `setupSubscriptions`.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls references processor/agentic-loop/component.go:1857:21` → 4 references: one production subscription binding and three test calls.
- `env GOCACHE=/private/tmp/semstreams-gh1146-gopls-cache gopls call_hierarchy processor/agentic-loop/component.go:1857:21` → 4 callers and 20 callees; the production caller is `setupSubscriptions`.
- `git grep -n -E '^func \(.*\) (handleApprovalResponseMessage|HandleApprovalResponse|handleSignalMessage|handleCancelSignal|handleToolCallVerdictMessage|HandleApprovedVerdict|HandleRejectedVerdict|ResolveApproval|checkApprovalGate|gateForApproval)|approval-response|approval_response|toolcall\.approved|toolcall\.rejected' -- processor/agentic-loop/*.go` → 56.
- `git grep -n -E 'ordinalByLoop|delete\(.*ordinalByLoop' -- processor/agentic-loop/trajectory_recorder.go` → 4.
- `git grep -n -F 'delete(r.ordinalByLoop' -- processor/agentic-loop/trajectory_recorder.go` → 0.
- `git grep -n -E 'type loopAuditLoss|loops\[loopID\]|allLoops|func \(l \*loopAuditLoss\) (observe|observed|release)' -- processor/agentic-loop/trajectory_observability.go` → 10.
- `git grep -n -E 'truncationRetryAttempts|IncrementTruncationRetry|ResetTruncationRetry' -- processor/agentic-loop/state.go processor/agentic-loop/handlers.go` → 19.
