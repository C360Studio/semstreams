# Inventory: task 4 loop settlement post-implementation surface
base: af829616305afa039dac0550efa78d07e856dd5f

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:166` — `## 4. Loop task and response settlement`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:168` — `- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:173` — `- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:177` — `bare terminal LoopEntity last as the lane-applied marker; discard speculative process-local terminal state on`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:179` — `- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:181` — `across real process replacement. Prove the brief COMPLETE/event-before-terminal-LoopEntity window remains`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:182` — `unsettled, exact current RequestID plus the final marker proves model-response application, and terminal state alone`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:183` — `never proves a particular tool result.`

## Spellings of the fact

### Typed input bindings and handlers

- `processor/agentic-loop/component.go:124` — `type inputHandler func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:903` — `settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:909` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:911` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:913` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/component.go:1224` — `func (c *Component) taskInputHandler(workTimeout time.Duration) inputHandler {`
- `processor/agentic-loop/component.go:1230` — `return natsclient.DeliveryDecisionRetry, workCtx.Err()`
- `processor/agentic-loop/component.go:1237` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1484` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1942` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/settlement_recovery.go:443` — `func loopSettlementDecision(err error) natsclient.DeliveryDecision {`
- `processor/agentic-loop/settlement_recovery.go:446` — `return natsclient.DeliveryDecisionQuarantine`
- `processor/agentic-loop/settlement_recovery.go:448` — `return natsclient.DeliveryDecisionTerminate`
- `processor/agentic-loop/settlement_recovery.go:450` — `return natsclient.DeliveryDecisionRetry`

### Exact retained-evidence seam and correlation reads

- `processor/agentic-loop/component.go:85` — `settlementEvidence       loopSettlementEvidenceReader`
- `processor/agentic-loop/settlement_recovery.go:25` — `type loopSettlementEvidenceReader interface {`
- `processor/agentic-loop/settlement_recovery.go:26` — `ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:27` — `ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:46` — `func (r natsLoopSettlementEvidenceReader) readExact(`
- `processor/agentic-loop/settlement_recovery.go:49` — `stream, err := r.client.GetStream(ctx, streamName)`
- `processor/agentic-loop/settlement_recovery.go:53` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:115` — `func (c *Component) readLoopEntity(ctx context.Context, loopID string) (agentic.LoopEntity, bool, error) {`
- `processor/agentic-loop/settlement_recovery.go:137` — `if entity.ID != loopID || entity.TaskID == "" {`
- `processor/agentic-loop/settlement_recovery.go:146` — `func (c *Component) readRetainedAgentRequest(`
- `processor/agentic-loop/settlement_recovery.go:162` — `evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:187` — `if evidence.subject != subject || request.LoopID != loopID ||`
- `processor/agentic-loop/settlement_recovery.go:198` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-loop/settlement_recovery.go:214` — `evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:250` — `func (c *Component) recoverTaskDelivery(`
- `processor/agentic-loop/settlement_recovery.go:264` — `if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:327` — `func (c *Component) ensureResponseLoop(`
- `processor/agentic-loop/settlement_recovery.go:381` — `func (c *Component) validateColdToolResult(`
- `processor/agentic-loop/settlement_recovery.go:443` — `func loopSettlementDecision(err error) natsclient.DeliveryDecision {`

### Partial task birth and recovery

- `processor/agentic-loop/component.go:101` — `// pendingTaskResults retains the not-yet-published spawn result when a`
- `processor/agentic-loop/component.go:105` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1266` — `if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {`
- `processor/agentic-loop/component.go:1267` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1361` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1362` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1365` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1366` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1369` — `c.clearPendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/state.go:337` — `func (m *LoopManager) restoreLoopFromRequest(entity agentic.LoopEntity, request agentic.AgentRequest) error {`
- `processor/agentic-loop/handlers.go:838` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:839` — `if existingID != task.LoopID {`
- `processor/agentic-loop/handlers.go:840` — `return HandlerResult{}, errs.WrapFatal(`
- `processor/agentic-loop/handlers.go:844` — `"task correlation conflict",`
- `processor/agentic-loop/delivery_owner_test.go:413` — `func TestDirectHandleTaskQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:426` — `require.True(t, errs.IsFatal(err), "task correlation conflict must quarantine")`
- `processor/agentic-loop/delivery_owner_test.go:434` — `func TestTaskDeliveryQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:451` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(), "cause: %v", result.Err())`

### Terminal effects, final marker, release, and error propagation

- `processor/agentic-loop/component.go:1613` — `return errs.WrapFatal(`
- `processor/agentic-loop/component.go:1614` — `transErr, "agentic-loop", "handleLoopFailure", "impossible failure transition",`
- `processor/agentic-loop/component.go:1617` — `defer c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1628` — `if err := c.publishFailureEvents(ctx, loopID, reason, err.Error()); err != nil {`
- `processor/agentic-loop/component.go:1631` — `return c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:1662` — `if err := c.persistFailureState(errorCtx, loopID, failure); err != nil {`
- `processor/agentic-loop/component.go:1672` — `_ = c.stampLoopFailureWithBudget(errorCtx, loopID, failure)`
- `processor/agentic-loop/component.go:1682` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1780` — `defer c.releaseLoopTransientState(result.LoopID)`
- `processor/agentic-loop/component.go:1782` — `if err := c.persistCompletionState(ctx, result.LoopID, result.CompletionState); err != nil {`
- `processor/agentic-loop/component.go:1785` — `_ = c.stampLoopCompletionWithBudget(ctx, result.LoopID, result.CompletionState)`
- `processor/agentic-loop/component.go:1798` — `if err := c.stampSyntheticDecideWithBudget(ctx, result.SyntheticDecide); err != nil {`
- `processor/agentic-loop/component.go:1806` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1812` — `return c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1836` — `writeErr = c.graphWriter.WriteLoopCompletion(bctx, completion, evidenceIncomplete)`
- `processor/agentic-loop/component.go:1872` — `writeErr = c.graphWriter.WriteSyntheticDecide(bctx, req.LoopID, req.Reason)`
- `processor/agentic-loop/component.go:1901` — `writeErr = c.graphWriter.WriteLoopFailure(bctx, failure, evidenceIncomplete)`
- `processor/agentic-loop/graph_writer.go:275` — `func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent, evidenceIncomplete bool) error {`
- `processor/agentic-loop/graph_writer.go:290` — `return fmt.Errorf("write loop completion batch: %w", err)`
- `processor/agentic-loop/graph_writer.go:299` — `func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent, evidenceIncomplete bool) error {`
- `processor/agentic-loop/graph_writer.go:314` — `return fmt.Errorf("write loop failure batch: %w", err)`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:67` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/component.go:1493` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1501` — `if !found || !durable.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1513` — `if request.RequestID != response.RequestID {`
- `processor/agentic-loop/component.go:1522` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1536` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1546` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1557` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2002` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2009` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2034` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/metrics.go:51` — `graphEvidenceFailures     *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:224` — `Name:      "graph_evidence_failures_total",`
- `processor/agentic-loop/metrics.go:225` — `Help:      "Nonblocking completion and failure graph-evidence write failures by terminal state and bounded reason.",`
- `processor/agentic-loop/metrics.go:291` — `_ = registry.RegisterCounterVec("agentic-loop", "graph_evidence_failures_total", metrics.graphEvidenceFailures)`
- `processor/agentic-loop/metrics.go:372` — `func (m *loopMetrics) recordGraphEvidenceFailure(state, reason string) {`

## Adjacent claims

- `docs/operations/migration-beta162-to-beta163.md:1107` — `- **Removed metrics:** `semstreams_agentic_loop_model_responses_dropped_total{reason}` and`
- `docs/operations/migration-beta162-to-beta163.md:1108` — ``semstreams_agentic_loop_tool_results_dropped_total{reason}`. Their log-and-ACK producers were removed: missing`
- `docs/operations/migration-beta162-to-beta163.md:1109` — `process correlation now performs lane-specific durable read-through and settles as Retry, Terminate, or Quarantine`
- `docs/operations/migration-beta162-to-beta163.md:1111` — `- **New metric:** `semstreams_agentic_loop_graph_evidence_failures_total{state,reason}` — counts nonblocking`
- `docs/operations/migration-beta162-to-beta163.md:1113` — `terminal settlement continues because this graph evidence is a derived projection, not authoritative loop state.`
- `openspec/changes/agentic-loop-restart-safety/design.md:938` — `| Persist bare terminal LoopEntity last as the lane-applied marker for every terminal loop outcome | `design.md / Per-lane definition of done`; `agentic-loop / Loop task, request, and tool work use only required correlation`; `tasks.md / 4.2–4.3, 6.5, 7.5–7.7` | comment `5571755835`; settlement-required effects precede the marker; pre-marker failure discards speculative process state and retries; ordinary effects may repeat; bare terminal state is not generic ToolResult proof; no second owner/runtime |`
- `openspec/changes/agentic-loop-restart-safety/design.md:942` — `| Make the TaskMessage producer the framework birth mint seam; require LoopID and add no helper or recovery owner | `entity-id-contract / A loop instance token is minted at its framework birth seam`; `agentic-loop / Loop task, request, and tool work use only required correlation`; `rule-agent-publishing / Publish-agent preserves the registered payload boundary`; `tasks.md / 3A.1–3A.3` | comment `5575482141`; producer-local v4 before marshal; ordinary validator error plus classified accepting boundaries; same-byte downstream retry/redelivery only; task 9 remains separate |`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:142` — `## 3A. Task producer identity prerequisite`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:154` — `second owner. Do not alter tasks 9.5–9.7 admission, subject coverage, classifier, registry, or PubAck contracts.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:155` — `- [ ] 3A.3 GREEN: through real NATS, redeliver the exact rule-produced registered bytes across agentic-loop process`
- `openspec/changes/archive/2026-09-02-loop-scoped-request-seams/tasks.md:120` — `model_responses_dropped_total{reason="stale_request_id"}`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:185` — `## 5. Tool result and completed outcome`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:196` — `- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:208` — `## 6. Dispatch edge gateway and approval continuation gate`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:219` — `- [ ] 6.3 Implement only operation-specific exact reads for latest `agent.request.<LoopID>` and exact`
- `processor/agentic-loop/approval_response_handler.go:163` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:277` — `## 7. Control vocabulary, cancel, approval-response, and verdict fast lanes`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:299` — `- [ ] 7.6 Refactor cancel, approval-response, approved-verdict, and rejected-verdict through their four existing`
- `processor/agentic-loop/component.go:2268` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2404` — `func (c *Component) handleToolCallVerdictMessage(_ context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:330` — `## 9. AGENT admission, first-party publisher, and loop authority`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:337` — `- [ ] 9.3 Implement one pure repo-internal `internal/agentstreamadmission.ObserveAndValidate`. Each affected owner`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:363` — `- [ ] 9.9 Implement internal `loopbucket.AcquireOwner`: KeyValue first; create only for typed`

## Consumers

- `processor/agentic-loop/component.go:911` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:204` — `tc.handler = c.handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:185` — `decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))`
- `processor/agentic-loop/component.go:913` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/delivery_owner_test.go:206` — `tc.handler = c.handleToolResultMessage`
- `processor/agentic-loop/settlement_recovery_test.go:285` — `decision, err := c.handleToolResultMessage(t.Context(), settlementEnvelope(t, result))`
- `processor/agentic-loop/component.go:1228` — `decision, err := c.handleTaskMessage(workCtx, data)`
- `processor/agentic-loop/settlement_recovery_test.go:113` — `decision, err := c.handleTaskMessage(t.Context(), settlementEnvelope(t, task))`
- `processor/agentic-loop/settlement_recovery.go:162` — `evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:214` — `evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)`

## Problem shape

- `processor/agentic-loop/settlement_recovery_integration_test.go:21` — `func TestIntegrationTaskAndResponseSettleAcrossProcessReplacement(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_integration_test.go:23` — `tc := natsclient.NewTestClient(t, natsclient.WithStreams(`
- `processor/agentic-loop/settlement_recovery_integration_test.go:29` — `newProcess := func() *Component {`
- `processor/agentic-loop/settlement_recovery_integration_test.go:75` — `responseDecision, err := newProcess().handleResponseMessage(ctx, responseData)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:79` — `completionEntry, err := bucket.Get(ctx, "COMPLETE_"+loopID)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:84` — `stream, err := tc.Client.GetStream(ctx, "AGENT")`
- `processor/agentic-loop/settlement_recovery_integration_test.go:86` — `_, err = stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:87` — `require.NoError(t, err, "response ACKed before its terminal publication received PubAck")`
- `processor/agentic-loop/settlement_recovery_test.go:99` — `func TestColdTaskRedeliveryReusesRetainedRequest(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:146` — `func TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:170` — `func TestColdModelResponseRestoresExactLoopAndCommitsTerminalState(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:242` — `func TestColdToolResultValidatesOriginatingResponseWithoutBatchReconstruction(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:292` — `func TestColdModelResponseFinalMarkerProvesExactRequestApplied(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:404` — `func TestProcessLocalTerminalResponseWaitsForDurableFinalMarker(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:318` — `func TestResponsePersistenceRetryDiscardsSpeculativeTurnBeforeRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:358` — `func TestToolPersistenceRetryDiscardsWarmRoutingBeforeColdRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:428` — `func TestWarmTerminalResponseRequiresCurrentRetainedRequest(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:68` — `func TestPersistHandlerResultReturnsPublicationFailureAndDiscardsSpeculativeTerminalState(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:90` — `func TestTerminalLoopEntityIsFinalAppliedMarker(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:135` — `func TestCompletionGraphWriteFailureRemainsNonblocking(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:156` — `func TestFailureGraphWriteFailureRemainsNonblocking(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:177` — `func TestRequiredSyntheticGraphWriteFailureReturnsError(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:196` — `func TestFailureLoopEntityIsFinalAppliedMarker(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:217` — `func TestImpossibleFailureTransitionIsQuarantined(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:283` — `func TestRunWithBudgetWaitsForCooperativeWorkToJoinAfterCancellation(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:156` — `func TestToolTimeoutCommitsTerminalFailureBeforeAck(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:177` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/delivery_owner_test.go:178` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/delivery_owner_test.go:187` — `func TestMalformedLoopWorkTerminatesInsteadOfAcking(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:215` — `require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:223` — `func TestRegisteredInvalidLoopPayloadTerminatesBeforeMutation(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:268` — `require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)`
- `processor/agentic-loop/delivery_owner_test.go:277` — `func TestCancelledResponseRetriesWithoutTerminalEffects(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:296` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`

## Searches

### Final task 4 refresh

- `rg --files openspec/changes/agentic-loop-restart-safety | sort | rg 'inventory-task4|task4'` → 2.
- Cached `gopls workspace_symbol -matcher=fuzzy handleTaskMessage` → 2.
- Cached `gopls workspace_symbol -matcher=fuzzy handleResponseMessage` → 2.
- Cached `gopls workspace_symbol -matcher=fuzzy handleToolResultMessage` → 1.
- Cached `gopls workspace_symbol -matcher=fuzzy loopSettlementDecision` → 1.
- Cached `gopls workspace_symbol -matcher=fuzzy TaskMessage` → 60.
- Cached `gopls workspace_symbol -matcher=fuzzy LoopEntity` → 100-line result cap.
- Cached `gopls workspace_symbol -matcher=fuzzy AgentResponse` → 100-line result cap.
- Cached `gopls workspace_symbol -matcher=fuzzy ToolResult` → 100-line result cap.
- Cached `gopls workspace_symbol -matcher=fuzzy persistHandlerResult` → 2.
- Cached `gopls workspace_symbol -matcher=fuzzy releaseLoopTransientState` → 1.
- Cached `gopls workspace_symbol -matcher=fuzzy recordGraphEvidenceFailure` → 1.
- `git grep --untracked -n -E 'type inputHandler|settleHandlerFn|handler = c\.|DeliveryDecision(Ack|Retry|Terminate|Quarantine)|loopSettlementDecision|func \(c \*Component\) (handleTaskMessage|handleResponseMessage|handleToolResultMessage|taskInputHandler)' -- processor/agentic-loop/component.go processor/agentic-loop/settlement_recovery.go processor/agentic-loop/delivery_owner.go` → 72.
- `git grep -n -E '^type (TaskMessage|AgentRequest|AgentResponse|ToolResult|LoopEntity) struct|LoopID.*json|TaskID.*json|RequestID.*json|ExecutionID.*json|CallOrdinal.*json|func \([^)]*\) Validate\(\) error' -- agentic/user_types.go agentic/types.go agentic/tools.go agentic/state.go` → 40.
- `git grep --untracked -n -E 'task correlation conflict|response correlation conflict|tool correlation conflict|HasActiveLoopForTask|CreateLoopWithID|restoreLoopFromRequest|readRetainedAgent(Request|Response)|readLoopEntity' -- processor/agentic-loop/handlers.go processor/agentic-loop/state.go processor/agentic-loop/component.go processor/agentic-loop/settlement_recovery.go processor/agentic-loop/*test.go` → 117.
- `git grep --untracked -n -E 'persistHandlerResult|handleLoopFailure|releaseLoopTransientState|persistCompletionState|persistFailureState|stampLoopCompletionWithBudget|stampLoopFailureWithBudget|stampSyntheticDecideWithBudget|recordGraphEvidenceFailure|graphEvidenceFailures|graph_evidence.*failed|responses_dropped|results_dropped' -- processor/agentic-loop/component.go processor/agentic-loop/graph_writer.go processor/agentic-loop/metrics.go docs/operations/migration-beta162-to-beta163.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md` → 57.
- `git grep --untracked -n -E 'releaseLoopTransientState|DeliveryDecisionRetry|context.Canceled|DeadlineExceeded|terminal.*timeout|timeout.*terminal|Lifecycle' -- processor/agentic-loop/component.go processor/agentic-loop/terminal_release_test.go processor/agentic-loop/persist_handler_result_test.go processor/agentic-loop/settlement_recovery_test.go processor/agentic-loop/delivery_owner_test.go` → 59.
- `git grep --untracked -n -E '^func Test.*(Task|Response|ToolResult|Terminal|Settlement|Process|Cold|Partial|Birth|Speculative|Validation|Poison|Timeout|Graph|Evidence|FinalMarker|Correlation)' -- processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/settlement_recovery_test.go processor/agentic-loop/settlement_recovery_integration_test.go processor/agentic-loop/persist_handler_result_test.go processor/agentic-loop/terminal_release_test.go processor/agentic-loop/loop_token_intake_test.go processor/agentic-loop/spawn_identity_failure_test.go` → 38.
- `git grep --untracked -n -E 'WriteLoopCompletion|WriteLoopFailure|WriteSyntheticDecide|GraphMutationBatch|evidence_incomplete|graph_evidence_failures_total' -- processor/agentic-loop/graph_writer.go processor/agentic-loop/graph_writer_*test.go processor/agentic-loop/persist_handler_result_test.go processor/agentic-loop/metrics.go docs/operations/migration-beta162-to-beta163.md` → 26.
- `git grep --untracked -n -E '^func Test.*(Integration|Race|Concurrent|Replacement)|-race|real NATS|process replacement|PubAck' -- processor/agentic-loop/*test.go openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md` → 111.
- `git grep -n -E 'owner|ruling|comment|Task 3A|task 3A|3A\.|task 4|Task 4|tasks 5|tasks 6|tasks 7|tasks 9|TaskID-to' -- openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md` → 129.

- `git rev-parse HEAD; git status --short; git diff --stat; git diff --name-only` → HEAD 1; status 18; tracked stat 15 files, 630 insertions, 216 deletions; tracked names 15
- `git grep --untracked -n -E 'loopSettlementEvidenceReader|settlementEvidence|handleTaskMessage|handleResponseMessage|handleToolResultMessage|taskInputHandler|responseInputHandler|toolResultInputHandler|DeliveryDecision|pendingTaskResults|partialBirth|partial-birth|releaseLoopTransientState|releaseLoopSpeculativeState|WriteLoopCompletion|WriteLoopFailure|WriteSyntheticDecide|correlation|GetLastMsgForSubject|final.*marker|effects-first' -- processor/agentic-loop openspec/changes/agentic-loop-restart-safety` → 615
- `gopls workspace_symbol -matcher=fuzzy loopSettlementEvidenceReader` with task-local `GOCACHE` → 10
- `gopls references processor/agentic-loop/component.go:1237:21` with task-local `GOCACHE` → 5
- `gopls call_hierarchy processor/agentic-loop/component.go:1237:21` with task-local `GOCACHE` → 28 callees
- `gopls references processor/agentic-loop/component.go:1492:21` with task-local `GOCACHE` → 10
- `gopls call_hierarchy processor/agentic-loop/component.go:1492:21` with task-local `GOCACHE` → 13 callees
- `gopls references processor/agentic-loop/component.go:1909:21` with task-local `GOCACHE` → 9
- `gopls call_hierarchy processor/agentic-loop/component.go:1909:21` with task-local `GOCACHE` → 21 callees
- `gopls implementation processor/agentic-loop/settlement_recovery.go:25:6` with task-local `GOCACHE` → 2
- `gopls references processor/agentic-loop/settlement_recovery.go:25:6` with task-local `GOCACHE` → 1
- `gopls call_hierarchy processor/agentic-loop/settlement_recovery.go:46:43` with task-local `GOCACHE` → 2 callers, 3 callees
- `git grep --untracked -n -E 'func \(c \*Component\) handle(ToolResult|Response|Task)Message|type loopSettlementEvidenceReader|func \(c \*Component\) (readLoopEntity|recoverTaskDelivery|ensureResponseLoop|ensureToolResultLoop|persistHandlerResult|handleLoopFailure)|func \(c \*Component\) releaseLoopSpeculativeState|func loopSettlementDecision|func \(r natsLoopSettlementEvidenceReader\)' -- processor/agentic-loop` → 13
- `git grep --untracked -n -E '^func Test.*(Cold|Settlement|Replacement|Restart|FinalMarker|Partial|Pending|Graph|Publish|Terminal|Synthetic)' -- processor/agentic-loop/*test.go` → 46
- `git grep --untracked -n -E 'type inputHandler|settleHandlerFn|handler = c\.|settlementEvidence =|settlementEvidence:|releaseLoopTransientState|releaseLoopSpeculativeState|DeleteLoop\(|pendingTaskResults|persistLoopState\(|persistCompletionState\(|persistFailureState\(|PublishToStream\(' -- processor/agentic-loop/component.go processor/agentic-loop/trajectory_handler_wiring.go processor/agentic-loop/settlement_recovery.go` → 39
- `git grep -n -E '^## (4|5|6|7|9)|^- \[[ x]\] (4|5|6|7|9)\.|Task 5|Task 6|Task 7|Task 9|task 5|task 6|task 7|task 9' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 36
- `git grep -n -E 'handleApprovalResponseMessage|handleSignalMessage|handleToolCallVerdictMessage|approval|verdict|admission|AGENT_LOOPS' -- processor/agentic-loop/component.go processor/agentic-loop/approval*.go processor/agentic-loop/signal*.go processor/agentic-loop/governance_dispatcher.go` → 226
- `git grep -n -E '^func \(c \*Component\) handle(ApprovalResponse|Signal|ToolCallVerdict)Message|^func \(d \*GovernanceDispatcher\)|type GovernanceDispatcher|AcquireOwner|ObserveAndValidate|approval.*handler|verdict.*handler' -- processor/agentic-loop internal processor` → 18
- `git grep -n -E 'tool_results_dropped_total|model_responses_dropped_total|toolResultsDropped|modelResponsesDropped|ToolResultsDropped|ModelResponsesDropped|tool.results.dropped|model.responses.dropped' -- . ':(exclude)openspec/changes/agentic-loop-restart-safety/inventory-task4-loop-settlement-postimpl-2026-09-07.md'` → 4
- `git grep -n -E 'tool_results_dropped_total|model_responses_dropped_total|toolResultsDropped|modelResponsesDropped|ToolResultsDropped|ModelResponsesDropped' -- docs openspec/specs openspec/changes ':(exclude)openspec/changes/agentic-loop-restart-safety/inventory-task4-loop-settlement-postimpl-2026-09-07.md'` → 4
- `git diff -- processor/agentic-loop/metrics.go docs/operations ':(glob)**migration*'` → 1 production file and 1 migration document
- `git grep --untracked -n -E '^func Test(FailureLoopEntityIsFinalAppliedMarker|RequiredFailureGraphWriteFailureReturnsError)' -- processor/agentic-loop` → 2
- `git grep --untracked -n -E '^func TestImpossibleFailureTransitionIsQuarantined|impossible failure transition|WrapFatal\(' -- processor/agentic-loop/component.go processor/agentic-loop/delivery_owner_test.go` → 9
- `git grep --untracked -n 'TestImpossibleFailureTransitionIsQuarantined' -- processor/agentic-loop` → 1
