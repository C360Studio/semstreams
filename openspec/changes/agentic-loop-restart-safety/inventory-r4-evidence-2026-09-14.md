# Inventory: R4 task/response settlement evidence
base: 5e0e2259aa7392f7f3255d7f01533869862d8174

worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`
snapshot: 2026-09-14; current dirty source; SHA-256 records below identify the enumerated bytes.
scope: R4 (old C.4, 4.1–4.3); R2 approval proof and retired R3 extraStore are outside this inventory.

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:174` — `- [ ] R4 Close loop task/response and remaining no-premature-ACK proof (old C.4, 4.1–4.3). Account for birth/lineage,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:175` — `initial request and created publication, cold reconstruction, duplicate/conflict/absence, every required durable`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:176` — `write/output failure, and terminal-marker ordering. Bare terminal state alone proves no particular ToolResult.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:177` — `Keep declared transition/refusal exits for #1244, no process-absence success, and no speculative terminal cache.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:178` — `Implemented read-through/heartbeat paths still need complete claim-set evidence, not another owner/runtime.`

## Spellings of the fact

- `processor/agentic-loop/component.go:85` — `settlementEvidence       loopSettlementEvidenceReader`
- `processor/agentic-loop/component.go:105` — `pendingTaskResults map[string]HandlerResult`
- `processor/agentic-loop/component.go:1274` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1275` — `if err != nil {`
- `processor/agentic-loop/component.go:1279` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1302` — `pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1307` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1336` — `if err := c.graphWriter.WriteSpawnIdentity(ctx, result.LoopID, task); err != nil {`
- `processor/agentic-loop/component.go:1343` — `if err := c.writeLineageTriples(ctx, result.LoopID, related); err != nil {`
- `processor/agentic-loop/component.go:1345` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1348` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-loop/component.go:1368` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1369` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1372` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1373` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1376` — `c.clearPendingTaskResult(task.TaskID, result.LoopID)`
- `processor/agentic-loop/component.go:1377` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1495` — `revision, createErr = c.loopsBucket.Create(ctx, loopID, data)`
- `processor/agentic-loop/component.go:1536` — `durable, found, readErr := c.readLoopEntity(ctx, loopID)`
- `processor/agentic-loop/component.go:1540` — `if !found || !durable.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1548` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1559` — `if persistErr := c.persistHandlerResult(ctx, result, revision); persistErr != nil {`
- `processor/agentic-loop/component.go:1581` — `if err := c.persistHandlerResult(ctx, result, revision); err != nil {`
- `processor/agentic-loop/component.go:1755` — `return err`
- `processor/agentic-loop/component.go:1758` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1761` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1809` — `selected, err := c.selectTerminalOutcome(ctx, result.LoopID, marker.TaskID, candidate)`
- `processor/agentic-loop/component.go:1855` — `_ = c.stampLoopCompletionWithBudget(ctx, result.LoopID, saved)`
- `processor/agentic-loop/component.go:1857` — `if err := c.stampSyntheticDecideWithBudget(ctx, &SyntheticDecideRequest{LoopID: saved.LoopID, Reason: saved.Result}); err != nil {`
- `processor/agentic-loop/component.go:1862` — `_ = c.stampLoopFailureWithBudget(ctx, result.LoopID, saved)`
- `processor/agentic-loop/component.go:1880` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1890` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/component.go:1922` — `key := "COMPLETE_" + loopID`
- `processor/agentic-loop/component.go:1923` — `if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {`
- `processor/agentic-loop/component.go:2288` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:2362` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/handlers.go:907` — `loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/handlers.go:932` — `_ = h.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/handlers.go:1125` — `requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)`
- `processor/agentic-loop/handlers.go:1129` — `createdSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.created", loopID)`
- `processor/agentic-loop/handlers.go:1137` — `Created: true,`
- `processor/agentic-loop/handlers.go:1138` — `PublishedMessages: []PublishedMessage{`
- `processor/agentic-loop/settlement_recovery.go:28` — `type loopSettlementEvidenceReader interface {`
- `processor/agentic-loop/settlement_recovery.go:29` — `ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:30` — `ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:56` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:57` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-loop/settlement_recovery.go:125` — `return agentic.LoopEntity{}, 0, errors.New("AGENT_LOOPS is unavailable")`
- `processor/agentic-loop/settlement_recovery.go:145` — `if entity.ID != loopID || entity.TaskID == "" {`
- `processor/agentic-loop/settlement_recovery.go:195` — `if evidence.subject != subject || request.LoopID != loopID ||`
- `processor/agentic-loop/settlement_recovery.go:196` — `!strings.HasPrefix(request.RequestID, loopID+":req:") {`
- `processor/agentic-loop/settlement_recovery.go:272` — `if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {`
- `processor/agentic-loop/settlement_recovery.go:279` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:280` — `return HandlerResult{LoopID: entity.ID, State: entity.State}, nil`
- `processor/agentic-loop/settlement_recovery.go:302` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`
- `processor/agentic-loop/settlement_recovery.go:313` — `_ = c.handler.loopManager.DeleteLoop(entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:338` — `mappedLoopID, _ := c.handler.loopManager.GetLoopForRequest(response.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:370` — `return agentic.LoopEntity{}, 0, fmt.Errorf("loop %q is not yet observable", loopID)`
- `processor/agentic-loop/settlement_recovery.go:383` — `if current.TaskID != entity.TaskID || current.Role != entity.Role || current.Model != entity.Model {`
- `processor/agentic-loop/settlement_recovery.go:403` — `return agentic.LoopEntity{}, 0, fmt.Errorf("request for loop %q is not yet observable", loopID)`
- `processor/agentic-loop/settlement_recovery.go:405` — `if request.RequestID != response.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:407` — `fmt.Errorf("retained request %q conflicts with response request %q", request.RequestID, response.RequestID),`
- `processor/agentic-loop/settlement_recovery.go:421` — `return agentic.LoopEntity{}, 0, fmt.Errorf("restore response correlation: %w", err)`
- `processor/agentic-loop/settlement_recovery.go:707` — `return fmt.Errorf("terminal loop lacks execution-specific retained result for %q", result.ExecutionID)`
- `processor/agentic-loop/settlement_recovery.go:714` — `return errs.WrapFatal(fmt.Errorf("terminal retained result conflicts with execution %q", result.ExecutionID),`
- `processor/agentic-loop/settlement_recovery.go:719` — `// a bare terminal state, cancelled loop, or approval wait does not.`
- `processor/agentic-loop/settlement_recovery.go:730` — `return fmt.Errorf("terminal loop lacks execution-specific applied proof for %q", result.ExecutionID)`
- `processor/agentic-loop/settlement_recovery.go:924` — `return natsclient.DeliveryDecisionQuarantine`
- `processor/agentic-loop/settlement_recovery.go:926` — `return natsclient.DeliveryDecisionTerminate`
- `processor/agentic-loop/settlement_recovery.go:928` — `return natsclient.DeliveryDecisionRetry`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:236` — `completion. ACK means the lane-specific durable transition or defined refusal and every required PubAck completed;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:407` — ``COMPLETE_` record, settlement-required synthetic effects, and terminal-event PubAck where applicable.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:443` — `- **AND** any required ordinary publication may repeat and receives PubAck before source ACK`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:453` — `- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:466` — `- **WHEN** terminal publication receives PubAck but the final bare `LoopEntity` Put has not committed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:478` — `- **WHEN** a cold `ToolResult` is durably correlated but only a bare terminal `LoopEntity` is present`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:301` — `- #1288 owns the inherited research completion-envelope/readback/current-state mismatch. It has no selected API or`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:303` — `is withdrawn from #1146; R1 must remove its introduced dependency without weakening existing stream validation.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:307` — `- #1140 owns governance policy content; #1145 owns framework-wide pattern work; #1244 owns the later declared`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:308` — `transition design. None is silently implemented or closed by this reconciliation.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:309` — `- #1249 retains transferred AgentRun H.1/H.2 from exact post-#1146 checkpoint A. #1155's combined proof stays open.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:310` — `PR #1159 remains based on frozen parent `417beae5552f8f15ad3540edd7d8504c87174c13` in PR #1156. Completing and`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:311` — `merging #1159 into that branch is not landing on main; #1156 still requires #1249 and combined review/proof.`
- #1146 — R4 brief; live issue reconciliation assigned to root.
- #1159 — PR brief; live PR reconciliation assigned to root.
- #1244 — declared transition design; tasks.md:307.
- #1249 — transferred AgentRun H.1/H.2; tasks.md:309.
- #1288 — research completion/readback/current-state; tasks.md:301.

## Consumers

- `processor/agentic-loop/component.go:124` — `type inputHandler func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:916` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:918` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:920` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/component.go:1235` — `decision, err := c.handleTaskMessage(workCtx, data)`
- `processor/agentic-loop/component.go:1244` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1522` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2085` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1235` — `handleTaskMessage`
- `processor/agentic-loop/create_vs_exists_fence_test.go:578` — `handleTaskMessage`
- `processor/agentic-loop/settlement_recovery_test.go:154` — `handleTaskMessage`
- `processor/agentic-loop/settlement_recovery_test.go:177` — `handleTaskMessage`
- `processor/agentic-loop/settlement_recovery_test.go:199` — `handleTaskMessage`
- `processor/agentic-loop/spawn_identity_failure_test.go:426` — `handleTaskMessage`
- `processor/agentic-loop/spawn_identity_failure_test.go:433` — `handleTaskMessage`
- `processor/agentic-loop/tool_result_recovery_test.go:78` — `handleTaskMessage`
- `processor/agentic-loop/component.go:918` — `handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:107` — `handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:227` — `handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:262` — `handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:317` — `handleResponseMessage`
- `processor/agentic-loop/delivery_owner_test.go:388` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:226` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:272` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:363` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:397` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:404` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:478` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:511` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:518` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:555` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:573` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:617` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:626` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery_test.go:633` — `handleResponseMessage`
- `processor/agentic-loop/terminal_release_test.go:423` — `handleResponseMessage`
- `processor/agentic-loop/terminal_release_test.go:468` — `handleResponseMessage`
- `processor/agentic-loop/terminal_selection_test.go:207` — `handleResponseMessage`
- `processor/agentic-loop/terminal_tool_recovery_test.go:106` — `handleResponseMessage`
- `processor/agentic-loop/settlement_recovery.go:33` — `natsLoopSettlementEvidenceReader`
- `processor/agentic-loop/settlement_recovery_test.go:98` — `settlementEvidence`
- `processor/agentic-loop/settlement_recovery.go:170` — `evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:222` — `evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)`

### Unit test names

- `processor/agentic-loop/delivery_owner_test.go:153` — `func TestTaskPersistenceFailureCannotAck(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:177` — `func TestToolTimeoutCommitsTerminalFailureBeforeAck(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:210` — `func TestMalformedLoopWorkTerminatesInsteadOfAcking(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:246` — `func TestRegisteredInvalidLoopPayloadTerminatesBeforeMutation(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:300` — `func TestCancelledResponseRetriesWithoutTerminalEffects(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:332` — `func TestImpossibleSpawnFailureTransitionQuarantinesThroughHeartbeatOwner(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:358` — `func TestMissingProcessCorrelationRetriesInsteadOfAcking(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:406` — `func TestTaskAssemblyFailureRollsBackProcessRegistration(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:423` — `func TestDirectHandleTaskRefusesMissingLoopIDBeforeState(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:439` — `func TestDirectHandleTaskQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:460` — `func TestTaskDeliveryQuarantinesTaskIDLoopIDConflict(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:74` — `func TestResponseAndToolResultPersistenceFailureCannotAck(t *testing.T) {`
- `processor/agentic-loop/lineage_preflight_test.go:125` — `func TestInvalidDecodedLineageTerminatesOnceAndHasNoBusinessSideEffects(t *testing.T) {`
- `processor/agentic-loop/lineage_preflight_test.go:193` — `func TestTransientLineageWriteNAKsThenResumesPendingSpawnOnRedelivery(t *testing.T) {`
- `processor/agentic-loop/lineage_preflight_test.go:20` — `func TestPreflightDecodedTaskLineageIdentitySemantics(t *testing.T) {`
- `processor/agentic-loop/lineage_preflight_test.go:69` — `func TestPreflightDecodedTaskRejectsMalformedLineageWithoutLoopCreation(t *testing.T) {`
- `processor/agentic-loop/lineage_preflight_test.go:94` — `func TestPreflightRetainedIdentityPreservesHandleTaskDedup(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:137` — `func TestRequiredLoopStatePersistenceReturnsErrors(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:157` — `func TestCompletionGraphWriteFailureRemainsNonblocking(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:178` — `func TestFailureGraphWriteFailureRemainsNonblocking(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:199` — `func TestRequiredSyntheticGraphWriteFailureReturnsError(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:218` — `func TestFailureLoopEntityIsFinalAppliedMarker(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:247` — `func TestImpossibleFailureTransitionIsQuarantined(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:70` — `func TestPersistHandlerResultReturnsPublicationFailureAndDiscardsSpeculativeTerminalState(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:98` — `func TestTerminalLoopEntityIsFinalAppliedMarker(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:140` — `func TestColdTaskRedeliveryReusesRetainedRequest(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:166` — `func TestColdTaskRedeliveryConflictingMappingQuarantines(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:187` — `func TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:211` — `func TestColdModelResponseRestoresExactLoopAndCommitsTerminalState(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:240` — `func TestColdModelResponseCorrelationConflictQuarantines(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:283` — `func TestColdToolResultRestoresOriginatingBatch(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:346` — `func TestColdModelResponseFinalMarkerProvesExactRequestApplied(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:372` — `func TestResponsePersistenceRetryDiscardsSpeculativeTurnBeforeRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:412` — `func TestToolPersistenceRetryDiscardsWarmRoutingBeforeColdRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:461` — `func TestProcessLocalTerminalResponseWaitsForDurableFinalMarker(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:485` — `func TestWarmTerminalResponseRequiresCurrentRetainedRequest(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:526` — `func TestWarmNonterminalResponseRequiresCurrentRetainedRequestBeforeMutation(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:582` — `func TestResponseTerminalSignalsFollowFinalMarkerAndDoNotRepeatForAppliedRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:643` — `func TestFailureTerminalSignalsFollowFinalMarker(t *testing.T) {`
- `processor/agentic-loop/spawn_identity_failure_test.go:110` — `func TestHandleSpawnIdentityFailure_GraphStatePoisonFailsLoopPerEntity(t *testing.T) {`
- `processor/agentic-loop/spawn_identity_failure_test.go:173` — `func TestGraphStatePoisonRouting_DistinguishesOperationalErrors(t *testing.T) {`
- `processor/agentic-loop/spawn_identity_failure_test.go:193` — `func TestHandleSpawnIdentityFailure_InvalidSerializationTerminatesAndDiscardsSpeculativeState(t *testing.T) {`
- `processor/agentic-loop/spawn_identity_failure_test.go:249` — `func TestGraphStatePoisonFailsLoopWhileIntakeContinues(t *testing.T) {`
- `processor/agentic-loop/spawn_identity_failure_test.go:399` — `func TestSpawnBirthCreateFailureCannotDeduplicateRedelivery(t *testing.T) {`
- `processor/agentic-loop/terminal_selection_test.go:116` — `TestTerminalSelectionPreservesTruncatedMarker`
- `processor/agentic-loop/terminal_selection_test.go:137` — `TestTerminalSelectionRejectsChangedSupportingRevision`
- `processor/agentic-loop/terminal_selection_test.go:161` — `TestTerminalSelectionRefusesCandidateMarkerMismatchBeforeSelection`
- `processor/agentic-loop/terminal_selection_test.go:188` — `TestTerminalSelectionReplaysStoredSuccess`
- `processor/agentic-loop/terminal_selection_test.go:226` — `TestSelectedSyntheticActionFailureWithholdsTerminalMarker`
- `processor/agentic-loop/terminal_selection_test.go:283` — `TestTerminalSelectionRejectsPoisonAndUncertainStorage`
- `processor/agentic-loop/terminal_selection_test.go:30` — `TestTerminalSelectionPreservesSavedOutcome`
- `processor/agentic-loop/terminal_selection_test.go:78` — `TestCompletionCodecPreservesSyntheticActionObligation`
- `processor/agentic-loop/terminal_selection_test.go:94` — `TestTerminalSelectionRefusesContradictoryPreparedState`
- `processor/agentic-loop/terminal_tool_recovery_test.go:17` — `func TestColdTerminalToolResultRequiresExactAppliedEvidence(t *testing.T) {`

### Native test names

- `processor/agentic-loop/consumer_policy_integration_test.go:16` — `func TestIntegrationTaskConsumerSerializesRedeliveryBeforeLaterWork(t *testing.T) {`
- `processor/agentic-loop/publication_semantics_integration_test.go:16` — `func TestIntegrationOrdinaryLoopPublicationsMayRepeat(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_integration_test.go:22` — `func TestIntegrationTaskAndResponseSettleAcrossProcessReplacement(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_integration_test.go:94` — `func TestIntegrationWarmResponseUsesCurrentRetainedRequestAsAuthority(t *testing.T) {`
- `processor/agentic-loop/task_loop_id_integration_test.go:56` — `func TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement(t *testing.T) {`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:92` — `func TestIntegrationTerminalMarkerFailureRedeliversAfterComponentReplacement(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:171` — `func TestIntegrationMatchingRetainedResponseSkipsProviderAndAcknowledgesSource(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:197` — `func TestIntegrationTypedAbsenceInvokesProviderAndPubAckPrecedesSourceAck(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:219` — `func TestIntegrationProviderErrorPubAckPrecedesSourceAck(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:260` — `func TestIntegrationRetainedResponseRequestIDConflictQuarantinesWithoutProvider(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:287` — `func TestIntegrationRetainedResponseLookupFailureRetriesWithoutProvider(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:310` — `func TestIntegrationPreProviderReplacementSeesAbsenceAndInvokesOnce(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:383` — `func TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain(t *testing.T) {`
- `processor/agentic-loop/terminal_tool_redelivery_integration_test.go:35` — `func TestIntegrationTerminalToolResultAppliedAfterReplacement(t *testing.T) {`

### Adjacent provider unit test names

- `processor/agentic-model/provider_settlement_test.go:60` — `func TestMatchingRetainedResponseAcknowledgesWithoutProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:84` — `func TestRetainedResponseCorrelationConflictQuarantinesBeforeProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:132` — `func TestRetainedResponseLookupFailureRetriesBeforeProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:147` — `func TestTypedRetainedResponseAbsencePermitsProviderPath(t *testing.T) {`

### Existing assertions and failpoints

- `processor/agentic-loop/delivery_owner_test.go:109` — `require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:110` — `require.Zero(t, msg.acks.Load()+msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:111` — `require.Equal(t, int32(1), msg.naks.Load())`
- `processor/agentic-loop/delivery_owner_test.go:112` — `require.ErrorIs(t, result.Err(), injected)`
- `processor/agentic-loop/delivery_owner_test.go:114` — `require.NotContains(t, bucket.values, "COMPLETE_"+loopID, "selected result persistence must be the failing operation")`
- `processor/agentic-loop/delivery_owner_test.go:143` — `require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:144` — `require.Zero(t, msg.acks.Load()+msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:169` — `require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:170` — `require.Zero(t, msg.acks.Load()+msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:171` — `require.Equal(t, int32(1), msg.naks.Load())`
- `processor/agentic-loop/delivery_owner_test.go:238` — `require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:239` — `require.Zero(t, msg.acks.Load()+msg.naks.Load())`
- `processor/agentic-loop/delivery_owner_test.go:240` — `require.Equal(t, int32(1), msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:321` — `require.ErrorIs(t, err, context.Canceled)`
- `processor/agentic-loop/delivery_owner_test.go:322` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/delivery_owner_test.go:323` — `require.NotContains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/delivery_owner_test.go:326` — `require.False(t, durable.State.IsTerminal())`
- `processor/agentic-loop/delivery_owner_test.go:328` — `require.Error(t, lookupErr, "cancelled response retained process-local loop state")`
- `processor/agentic-loop/delivery_owner_test.go:398` — `require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())`
- `processor/agentic-loop/delivery_owner_test.go:399` — `require.Zero(t, msg.acks.Load()+msg.terms.Load())`
- `processor/agentic-loop/delivery_owner_test.go:400` — `require.Equal(t, int32(1), msg.naks.Load())`
- `processor/agentic-loop/delivery_owner_test.go:418` — `require.Error(t, lookupErr, "failed task assembly left a registered loop that redelivery would ACK as a duplicate")`
- `processor/agentic-loop/settlement_recovery_test.go:40` — `failPutKey  string`
- `processor/agentic-loop/settlement_recovery_test.go:41` — `failPutLeft int`
- `processor/agentic-loop/settlement_recovery_test.go:65` — `if key == b.failPutKey && b.failPutLeft > 0 {`
- `processor/agentic-loop/settlement_recovery_test.go:67` — `return 0, errors.New("injected final marker failure")`
- `processor/agentic-loop/settlement_recovery_test.go:157` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:159` — `require.True(t, found, "retained request was not restored as the active provider correlation")`
- `processor/agentic-loop/settlement_recovery_test.go:180` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:182` — `require.Error(t, lookupErr, "conflicting durable mapping was installed in process state")`
- `processor/agentic-loop/settlement_recovery_test.go:202` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:205` — `require.Equal(t, entity.StartedAt, got.StartedAt,`
- `processor/agentic-loop/settlement_recovery_test.go:229` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:230` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/settlement_recovery_test.go:233` — `require.Equal(t, agentic.LoopStateComplete, persisted.State)`
- `processor/agentic-loop/settlement_recovery_test.go:236` — `require.Error(t, lookupErr, "terminal settlement did not release restored process state")`
- `processor/agentic-loop/settlement_recovery_test.go:275` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:276` — `require.NotContains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/settlement_recovery_test.go:329` — `t.Run("terminal marker is not execution-specific proof", func(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:332` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:333` — `require.Contains(t, err.Error(), "execution-specific")`
- `processor/agentic-loop/settlement_recovery_test.go:366` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:368` — `require.Error(t, lookupErr, "applied duplicate should not restore terminal process state")`
- `processor/agentic-loop/settlement_recovery_test.go:398` — `require.ErrorIs(t, err, bucket.putErr)`
- `processor/agentic-loop/settlement_recovery_test.go:399` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:401` — `require.Error(t, lookupErr, "failed response attempt retained speculative process state")`
- `processor/agentic-loop/settlement_recovery_test.go:406` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:408` — `require.Len(t, contextMessages, 2, "redelivery duplicated the assistant turn")`
- `processor/agentic-loop/settlement_recovery_test.go:481` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:513` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:520` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:557` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:561` — `require.Equal(t, entityBefore, entityAfter, "stale response mutated warm loop state")`
- `processor/agentic-loop/settlement_recovery_test.go:567` — `require.NotContains(t, c.loopsBucket.(*settlementBucket).values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/settlement_recovery_test.go:594` — `failPutKey: loopID, failPutLeft: 1,`
- `processor/agentic-loop/settlement_recovery_test.go:619` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:624` — `require.NotContains(t, logs.String(), "Loop completed")`
- `processor/agentic-loop/settlement_recovery_test.go:628` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:631` — `require.Equal(t, 1, strings.Count(logs.String(), "Loop completed"))`
- `processor/agentic-loop/settlement_recovery_test.go:635` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_test.go:639` — `require.Equal(t, 1, strings.Count(logs.String(), "Loop completed"))`
- `processor/agentic-loop/persist_handler_result_test.go:80` — `c := &Component{handler: handler, config: DefaultConfig(), loopsBucket: bucket, natsClient: &natsclient.Client{}, logger: slog.Default()}`
- `processor/agentic-loop/persist_handler_result_test.go:90` — `require.Contains(t, err.Error(), "publish result")`
- `processor/agentic-loop/persist_handler_result_test.go:91` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID, "test did not reach the publication seam after selection")`
- `processor/agentic-loop/persist_handler_result_test.go:92` — `require.Equal(t, settlementLoopRecord(t, entity), bucket.values[loopID], "publication failure committed a terminal marker")`
- `processor/agentic-loop/persist_handler_result_test.go:94` — `require.Error(t, err, "failed terminal attempt retained speculative process state")`
- `processor/agentic-loop/persist_handler_result_test.go:111` — `loopID: loopID, err: errors.New("final marker unavailable"),`
- `processor/agentic-loop/persist_handler_result_test.go:127` — `require.ErrorIs(t, err, bucket.err)`
- `processor/agentic-loop/persist_handler_result_test.go:128` — `require.Equal(t, settlementLoopRecord(t, entity), bucket.values[loopID], "failed final Update changed the prior authority")`
- `processor/agentic-loop/persist_handler_result_test.go:129` — `require.Equal(t, revision, bucket.revisions[loopID])`
- `processor/agentic-loop/persist_handler_result_test.go:130` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID,`
- `processor/agentic-loop/persist_handler_result_test.go:133` — `require.Error(t, lookupErr, "failed final marker retained speculative terminal process state")`
- `processor/agentic-loop/persist_handler_result_test.go:145` — `require.ErrorIs(t, err, want)`
- `processor/agentic-loop/persist_handler_result_test.go:149` — `require.ErrorIs(t, err, want)`
- `processor/agentic-loop/persist_handler_result_test.go:153` — `require.ErrorIs(t, err, want)`
- `processor/agentic-loop/persist_handler_result_test.go:174` — `require.Equal(t, float64(1), testutil.ToFloat64(failures.WithLabelValues("complete", "write_error")))`
- `processor/agentic-loop/persist_handler_result_test.go:195` — `require.Equal(t, float64(1), testutil.ToFloat64(failures.WithLabelValues("failure", "write_error")))`
- `processor/agentic-loop/persist_handler_result_test.go:214` — `require.Contains(t, err.Error(), "synthetic decide graph stamp")`
- `processor/agentic-loop/persist_handler_result_test.go:237` — `require.ErrorIs(t, err, bucket.err)`
- `processor/agentic-loop/persist_handler_result_test.go:238` — `require.Equal(t, settlementLoopRecord(t, entity), bucket.values[loopID], "failed final Update changed the prior authority")`
- `processor/agentic-loop/persist_handler_result_test.go:239` — `require.Equal(t, revision, bucket.revisions[loopID])`
- `processor/agentic-loop/persist_handler_result_test.go:243` — `require.Error(t, lookupErr, "failed final marker retained speculative failure state")`
- `processor/agentic-loop/lineage_preflight_test.go:208` — `component.testLineageWriteHook = func(context.Context, string, map[string]any) error {`
- `processor/agentic-loop/lineage_preflight_test.go:210` — `return errs.WrapTransient(errors.New("injected NATS timeout"),`
- `processor/agentic-loop/lineage_preflight_test.go:233` — `if first.acked.Load() || !first.naked.Load() || first.terminated.Load() {`
- `processor/agentic-loop/lineage_preflight_test.go:242` — `t.Fatal("transient lineage failure did not retain the unpublished spawn result")`
- `processor/agentic-loop/lineage_preflight_test.go:245` — `t.Fatalf("loops-created metric after failed attempt = %v, want 0", delta)`
- `processor/agentic-loop/lineage_preflight_test.go:253` — `if !second.acked.Load() || second.naked.Load() || second.terminated.Load() {`
- `processor/agentic-loop/lineage_preflight_test.go:257` — `if attempts.Load() != 2 {`
- `processor/agentic-loop/spawn_identity_failure_test.go:232` — `require.ErrorContains(t, err, "marshal initial loop", "must refuse at the pre-birth serialization boundary")`
- `processor/agentic-loop/spawn_identity_failure_test.go:233` — `require.NotContains(t, bucket.values, loopID)`
- `processor/agentic-loop/spawn_identity_failure_test.go:234` — `require.NotContains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/spawn_identity_failure_test.go:235` — `require.Empty(t, bucket.values, "invalid birth must not author any durable evidence")`
- `processor/agentic-loop/spawn_identity_failure_test.go:328` — `require.Equal(t, []string{"create:" + poisonedLoopID + ":running", "create:COMPLETE_" + poisonedLoopID, "update:" + poisonedLoopID}, bucket.operations,`
- `processor/agentic-loop/spawn_identity_failure_test.go:410` — `settlementBucket: &settlementBucket{values: make(map[string][]byte), failPutKey: loopID, failPutLeft: 1},`
- `processor/agentic-loop/spawn_identity_failure_test.go:427` — `require.ErrorContains(t, err, "injected final marker failure")`
- `processor/agentic-loop/spawn_identity_failure_test.go:428` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/spawn_identity_failure_test.go:429` — `require.Empty(t, bucket.values, "failed initial Create must not invent durable completion")`
- `processor/agentic-loop/spawn_identity_failure_test.go:430` — `require.Empty(t, perLoopMapCount(c.handler.loopManager, loopID))`
- `processor/agentic-loop/spawn_identity_failure_test.go:435` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/spawn_identity_failure_test.go:436` — `require.Equal(t, 2, lineageCalls, "redelivery must retry birth, not ACK active process deduplication")`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:37` — `if entity.State.IsTerminal() && b.failed.CompareAndSwap(false, true) {`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:38` — `return 0, errors.New("injected final terminal marker Update failure")`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:131` — `require.NoError(t, first.Start(ctx))`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:171` — `require.Equal(t, 1, failed.naks, "required final-marker failure must Retry")`
- `processor/agentic-loop/terminal_marker_redelivery_integration_test.go:172` — `require.Zero(t, failed.acks+failed.terms, "required final-marker failure settled the source")`
- `processor/agentic-loop/settlement_recovery_integration_test.go:65` — `require.Equal(t, natsclient.DeliveryDecisionAck, taskDecision)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:78` — `require.Equal(t, natsclient.DeliveryDecisionAck, responseDecision)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:81` — `require.NoError(t, err, "response ACKed before its durable terminal state committed")`
- `processor/agentic-loop/settlement_recovery_integration_test.go:88` — `require.NoError(t, err, "response ACKed before its terminal publication received PubAck")`
- `processor/agentic-loop/settlement_recovery_integration_test.go:145` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:148` — `require.False(t, warm.State.IsTerminal(), "historical response mutated warm loop before quarantine")`
- `processor/agentic-loop/settlement_recovery_integration_test.go:150` — `require.ErrorIs(t, err, jetstream.ErrKeyNotFound)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:158` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/settlement_recovery_integration_test.go:160` — `require.NoError(t, err, "current retained response did not reach durable terminal settlement")`
- `processor/agentic-loop/task_loop_id_integration_test.go:139` — `require.Equal(t, 1, firstDelivery.acks)`
- `processor/agentic-loop/task_loop_id_integration_test.go:148` — `require.NoError(t, err, "loop birth must commit before the interrupted source ACK")`
- `processor/agentic-loop/task_loop_id_integration_test.go:166` — `require.Equal(t, 1, info.NumAckPending)`
- `processor/agentic-loop/task_loop_id_integration_test.go:167` — `require.Less(t, info.AckFloor.Stream, firstMetadata.Sequence.Stream)`
- `processor/agentic-loop/task_loop_id_integration_test.go:175` — `require.Empty(t, second.handler.loopManager.loops, "replacement must have no process-local loop identity")`
- `processor/agentic-loop/task_loop_id_integration_test.go:203` — `require.Equal(t, firstMetadata.Sequence.Stream, metadata.Sequence.Stream)`
- `processor/agentic-loop/task_loop_id_integration_test.go:204` — `require.Greater(t, metadata.NumDelivered, firstMetadata.NumDelivered)`
- `processor/agentic-loop/task_loop_id_integration_test.go:205` — `require.Equal(t, firstDelivery.Data(), redelivered.Data())`
- `processor/agentic-loop/task_loop_id_integration_test.go:206` — `require.Equal(t, 1, redelivered.acks)`
- `processor/agentic-loop/task_loop_id_integration_test.go:220` — `require.Equal(t, []string{task.LoopID}, keys, "replacement must not create a second durable loop identity")`
- `processor/agentic-loop/publication_semantics_integration_test.go:30` — `require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))`
- `processor/agentic-loop/publication_semantics_integration_test.go:31` — `require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))`
- `processor/agentic-loop/publication_semantics_integration_test.go:50` — `require.Equal(t, 2, count, "%s may repeat after publication uncertainty", published.Subject)`
- `processor/agentic-loop/consumer_policy_integration_test.go:30` — `require.Equal(t, 1, maxAckPending)`
- `processor/agentic-loop/consumer_policy_integration_test.go:58` — `require.Equal(t, 1, info.NumAckPending,`
- `processor/agentic-loop/consumer_policy_integration_test.go:60` — `require.Equal(t, uint64(1), info.NumPending,`
- `processor/agentic-loop/consumer_policy_integration_test.go:66` — `require.NoError(t, first.DoubleAck(ctx))`
- `processor/agentic-loop/consumer_policy_integration_test.go:69` — `require.Equal(t, []byte("task-n-plus-1"), second.Data())`
- `processor/agentic-loop/consumer_policy_integration_test.go:72` — `require.Equal(t, uint64(1), secondMetadata.NumDelivered,`
- `processor/agentic-loop/consumer_policy_integration_test.go:78` — `require.Zero(t, info.NumAckPending)`
- `processor/agentic-loop/consumer_policy_integration_test.go:79` — `require.Zero(t, info.NumPending)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:53` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:54` — `require.Equal(t, before, bucket.values[loopID])`
- `processor/agentic-loop/terminal_tool_recovery_test.go:55` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:86` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:87` — `require.Equal(t, final, bucket.values[loopID])`
- `processor/agentic-loop/terminal_tool_recovery_test.go:89` — `require.Error(t, err, "proven terminal replay must not reinstall process state")`
- `processor/agentic-loop/terminal_tool_recovery_test.go:122` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "a timeout-at-cap marker is not the max-iteration consequence")`
- `processor/agentic-loop/terminal_tool_recovery_test.go:126` — `require.Equal(t, timeoutMarker.Value(), unchanged.Value())`
- `processor/agentic-loop/terminal_tool_recovery_test.go:127` — `require.Equal(t, timeoutMarker.Revision(), unchanged.Revision())`
- `processor/agentic-loop/terminal_tool_recovery_test.go:128` — `require.Equal(t, savedTimeout, timeoutBucket.values["COMPLETE_"+loopID])`
- `processor/agentic-loop/terminal_selection_test.go:202` — `// not rerun a model/tool. The nil graph/client unit seam is not PubAck proof.`
- `processor/agentic-loop/terminal_selection_test.go:209` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/terminal_selection_test.go:210` — `require.Equal(t, savedBytes, f.bucket.values["COMPLETE_"+f.entity.ID])`
- `processor/agentic-loop/terminal_selection_test.go:216` — `assert.True(t, marker.CompletedAt.Equal(saved.CompletedAt), "final marker must use the selected timestamp")`
- `processor/agentic-loop/terminal_selection_test.go:217` — `assert.Equal(t, f.entity.PendingToolResults, marker.PendingToolResults, "selected content does not replace exact source evidence")`
- `processor/agentic-loop/terminal_selection_test.go:220` — `assert.True(t, retained.SyntheticDecideRequired)`
- `processor/agentic-loop/terminal_selection_test.go:222` — `assert.Error(t, err, "settled replacement retained speculative process state")`
- `processor/agentic-loop/terminal_selection_test.go:250` — `require.ErrorContains(t, err, "synthetic decide graph stamp", "saved true obligation must survive the recomputed false candidate")`
- `processor/agentic-loop/terminal_selection_test.go:251` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision, "required-effect failure must preserve approval's no-settlement class")`
- `processor/agentic-loop/terminal_selection_test.go:252` — `assert.Equal(t, savedBytes, f.bucket.values["COMPLETE_"+f.entity.ID])`
- `processor/agentic-loop/terminal_selection_test.go:253` — `assert.Equal(t, before, f.bucket.values[f.entity.ID])`
- `processor/agentic-loop/terminal_selection_test.go:254` — `assert.Equal(t, revision, bucket.revisions[f.entity.ID])`
- `processor/agentic-model/provider_settlement_integration_test.go:192` — `requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-matching", ack.Sequence)`
- `processor/agentic-model/provider_settlement_integration_test.go:193` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:214` — `requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-absence", ack.Sequence)`
- `processor/agentic-model/provider_settlement_integration_test.go:215` — `require.Equal(t, int32(1), calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:282` — `requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-conflict", ack.Sequence)`
- `processor/agentic-model/provider_settlement_integration_test.go:283` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:304` — `requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-lookup-failure", ack.Sequence)`
- `processor/agentic-model/provider_settlement_integration_test.go:305` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:352` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:378` — `require.Equal(t, int32(1), calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:434` — `requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-replacement", ack.Sequence)`
- `processor/agentic-model/provider_settlement_integration_test.go:457` — `require.Equal(t, int32(2), calls.Load())`

## Problem shape

- `processor/agentic-loop/component.go:1897` — `func (c *Component) selectTerminalOutcome(ctx context.Context, loopID, taskID string, candidate message.Payload) (message.Payload, error) {`
- `processor/agentic-loop/component.go:1922` — `key := "COMPLETE_" + loopID`
- `processor/agentic-loop/component.go:1923` — `if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {`
- `processor/agentic-loop/component.go:1935` — `return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "decode saved terminal")`
- `processor/agentic-loop/component.go:1938` — `return nil, errs.WrapFatal(errors.New("saved terminal identity conflict"), "agentic-loop", "selectTerminalOutcome", "validate saved terminal")`
- `processor/agentic-loop/component.go:1952` — `return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "decode saved terminal")`
- `processor/agentic-loop/component.go:1955` — `return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "validate saved terminal")`
- `processor/agentic-loop/terminal_selection_test.go:20` — `type terminalSelectionBucket struct{ *approvalRevisionBucket }`
- `processor/agentic-loop/terminal_selection_test.go:22` — `func (b *terminalSelectionBucket) Create(ctx context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {`
- `processor/agentic-loop/terminal_selection_test.go:23` — `if _, exists := b.values[key]; exists {`
- `processor/agentic-loop/terminal_selection_test.go:24` — `return 0, jetstream.ErrKeyExists`
- `processor/agentic-loop/terminal_selection_test.go:26` — `return b.Put(ctx, key, value)`
- `processor/agentic-loop/terminal_selection_test.go:261` — `type terminalSelectionFaultBucket struct {`
- `processor/agentic-loop/terminal_selection_test.go:264` — `createErr error`
- `processor/agentic-loop/terminal_selection_test.go:265` — `getErr    error`
- `processor/agentic-loop/terminal_selection_test.go:268` — `func (b *terminalSelectionFaultBucket) Create(ctx context.Context, key string, data []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {`
- `processor/agentic-loop/terminal_selection_test.go:269` — `if key == b.key && b.createErr != nil {`
- `processor/agentic-loop/terminal_selection_test.go:270` — `return 0, b.createErr`
- `processor/agentic-loop/terminal_selection_test.go:275` — `func (b *terminalSelectionFaultBucket) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {`
- `processor/agentic-loop/terminal_selection_test.go:276` — `if key == b.key && b.getErr != nil {`
- `processor/agentic-loop/terminal_selection_test.go:277` — `return nil, b.getErr`
- `processor/agentic-loop/terminal_selection_test.go:283` — `func TestTerminalSelectionRejectsPoisonAndUncertainStorage(t *testing.T) {`
- `processor/agentic-loop/terminal_selection_test.go:284` — `for _, name := range []string{"malformed_json", "wrong_loop", "wrong_task", "invalid_outcome", "invalid_decision", "create_uncertain", "collision_get_uncertain"} {`
- `processor/agentic-loop/terminal_selection_test.go:313` — `bucket.createErr, uncertain = storageErr, true`
- `processor/agentic-loop/terminal_selection_test.go:315` — `bucket.getErr, uncertain = storageErr, true`
- `processor/agentic-loop/terminal_selection_test.go:329` — `require.ErrorIs(t, err, storageErr)`
- `processor/agentic-loop/terminal_selection_test.go:331` — `assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/terminal_selection_test.go:334` — `assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/terminal_selection_test.go:339` — `assert.Equal(t, savedBytes, f.bucket.values[bucket.key])`
- `processor/agentic-loop/terminal_selection_test.go:341` — `assert.Equal(t, before, f.bucket.values[f.entity.ID])`
- `processor/agentic-loop/terminal_selection_test.go:342` — `assert.Equal(t, revision, bucket.revisions[f.entity.ID])`
- `processor/agentic-loop/terminal_selection_test.go:344` — `assert.Error(t, lookupErr, "refused selection retained speculative process state")`

## Source hashes

```text
6c1e7b44cba2c53e49e55e1f9f40c73c379922c8eed41fd08c76f6b44bc7a8b7  processor/agentic-loop/component.go
e8878f5d756ed3204ced34043b2194d63661decbd4ff7ffcb71788b31ffdd5ff  processor/agentic-loop/handlers.go
ba9524ddaa2d27a2e0668d6524491ec6646cc6ffaeff6a7acddbc0b56b3816a8  processor/agentic-loop/settlement_recovery.go
bf29ce8cdda9af3cce5ed24903406c9e89ddeabc01325c229f9adb349cd44de9  processor/agentic-loop/state.go
aa6dc93889f2a8cb7fd77bba03519319f8876a5b7f9432917c93c9157741a5dd  processor/agentic-loop/settlement_recovery_test.go
d5c185e6c7b4eb9e27a9238506a8a7759fb4b594bf04483f219c79dae997b5d6  processor/agentic-loop/settlement_recovery_integration_test.go
bda6bac4a7e31348286df19ea61858a3d2f1264b1cdac42455da9a9284400b04  processor/agentic-loop/delivery_owner_test.go
bebae1cd96a9e992a26ea445fac66718f3ff89b5b6d3b3c9d746d7cd25e656a4  processor/agentic-loop/persist_handler_result_test.go
57638b69cad6000ef6e16bf63d0123f08e6e363cb87156483a9dd084cc9ff91d  processor/agentic-loop/lineage_preflight_test.go
2eec99b90345df6b5c8ead13a44ba7a85e1c8232f6b19213750353cd1659435d  processor/agentic-loop/spawn_identity_failure_test.go
7e3fd9e34d77efff1982f0cfe3779b89cf8ea75d0fd6535445305205bc7fa714  processor/agentic-loop/terminal_marker_redelivery_integration_test.go
7864b6aa25e7e330df7d9a7f56798582ebbc7f396b49a8c432a810d73f3458ca  processor/agentic-loop/terminal_tool_recovery_test.go
30e50c17189d163cf859e68eee897be68f2207d356564095b172038a54e12b63  processor/agentic-loop/terminal_tool_redelivery_integration_test.go
ac6fbc8b062e726c40093829e8e45a3dae575a9ccb5069b5883df25175bebead  processor/agentic-loop/task_loop_id_integration_test.go
1b9a6e0eef4a3110244c3d596ea9bc0f812c33c1390f03fe89a956f2696b1f6d  processor/agentic-loop/publication_semantics_integration_test.go
eb98dc4ee7cf8d0a5a7af124cd58e24410d95a3078ade7ee7809d692d5b0096a  processor/agentic-loop/consumer_policy_integration_test.go
f9ae968dfbc5053447519daf5be209ea76215bf89ad256b96c02e8fd660c4b68  processor/agentic-loop/terminal_selection_test.go
e4da129eb2bd196339d85de0ae46f943a2523644f280e6caca38b1871ca48b95  processor/agentic-model/provider_settlement_integration_test.go
ab830157e3b6b75ae1476c3a449116bde8af0c37b970ca209757e1f5559ab26f  processor/agentic-model/provider_settlement_test.go
```

## Searches

- S01 `sed -n '1,260p' .agents/contracts/semstreams-explorer.md` → full contract read in repository root.
- S02 `git rev-parse HEAD` → 1; `5e0e2259aa7392f7f3255d7f01533869862d8174`.
- S03 `sed -n '1,85p' openspec/project.md` → Purpose and Product Boundary read.
- S04 `git grep -n -E 'R4|C\.4|4\.[123]|task.birth|PubAck|cold reconstruction|terminal marker' -- openspec/changes/agentic-loop-restart-safety/tasks.md 'openspec/changes/agentic-loop-restart-safety/inventory-task4*'` → 38.
- S05 `command -v gopls` → 1; `/Users/coby/go/bin/gopls`.
- S06 `git grep -n -E '^func Test|^func test|^func new|^func \(.*(fail|Fail)|failpoint|fail[A-Z]|inject|err[A-Z].*=|failure' -- processor/agentic-loop/settlement* processor/agentic-loop/task* processor/agentic-loop/response* processor/agentic-loop/continuation* processor/agentic-loop/durable* processor/agentic-loop/handlers*` → 0; shell rejected unmatched `response*` before execution.
- S07 `git status --short -- processor/agentic-loop processor/agentic-model agentic openspec/changes/agentic-loop-restart-safety` → dirty tracked files and untracked files; no mutation.
- S08 `gopls workspace_symbol -matcher=fuzzy processTask` → 0; workspace load failed on sandboxed Go cache.
- S09 `git grep -n -E 'processor/agentic-loop/|processor/agentic-model/|agentic/loop' -- openspec/changes/agentic-loop-restart-safety/inventory-task4-loop-settlement-postimpl-2026-09-07.md openspec/changes/agentic-loop-restart-safety/inventory-task4-continuation-response-proof-2026-09-08.md` → 390; first display truncated; repeated below with captured hit count.
- S10 `env GOCACHE=/private/tmp/semstreams-r4-gopls-cache gopls workspace_symbol -matcher=fuzzy handleTaskMessage` → 2 symbols; completed through terminal session 55693; gopls-cache write warnings.
- S11 `git grep -n -E '^func Test|^func test|^func new|failpoint|fail[A-Z]|inject|failure' -- 'processor/agentic-loop/settlement*' 'processor/agentic-loop/persist_handler_result_test.go' 'processor/agentic-loop/delivery_owner_test.go' 'processor/agentic-loop/spawn_identity_failure_test.go' 'processor/agentic-loop/delivery_settlement_integration_test.go' 'processor/agentic-loop/terminal_marker_redelivery_integration_test.go' 'processor/agentic-loop/evidence_integrity_integration_test.go'` → 135.
- S12 `git grep -n -E '1146|1159|1244|1249|1288|R4|bare terminal|PubAck|4\.[123]' -- openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/specs/agentic-loop/spec.md` → 62.
- S13 `git grep -n -E 'handleTaskMessage|handleResponseMessage|handleToolResultMessage|taskInputHandler|inputHandler|SubscribeWith|settlementEvidence|persistHandlerResult|persistLoopState|persistCompletionState|PublishToStream|rememberPendingTask|pendingTaskResult|releaseLoopSpeculative' -- processor/agentic-loop/component.go processor/agentic-loop/delivery_owner.go processor/agentic-loop/settlement_recovery.go` → 62.
- S14 `sed -n '1244,1385p;1522,1592p;1732,1855p;2280,2298p;2346,2383p' processor/agentic-loop/component.go` → 394.
- S15 `git grep -n -E 'func |return |Subject|RequestID|TaskID|LoopID|State.IsTerminal|final marker|bare terminal|ErrMsgNotFound|COMPLETE_' -- processor/agentic-loop/settlement_recovery.go` → 255.
- S16 `git grep -n -E '^func Test.*(Task|Response|Spawn|Birth|Lineage|TerminalMarker|TerminalLoop|FailureLoop|ColdTool|Publication|PersistHandler|PersistenceFailure|MissingProcess|Assembly|Continuation|TerminalTool)|\.Run\(|putErr:|getErr:|createErr:|updateErr:|failPut|publishErr|beforePublish|AfterPublish|AfterPersist' -- 'processor/agentic-loop/*test.go'` → 279.
- S17 `sed -n '1,110p;140,282p;346,410p;461,642p' processor/agentic-loop/settlement_recovery_test.go` → 500.
- S18 `env GOCACHE=/private/tmp/semstreams-r4-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r4-gopls-cache gopls references processor/agentic-loop/component.go:1244:21` → 8; completed through terminal session 86970.
- S19 `env GOCACHE=/private/tmp/semstreams-r4-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r4-gopls-cache gopls references processor/agentic-loop/component.go:1522:21` → 26; completed through terminal session 66038.
- S20 `git grep -n -E '^func Test|require\.|putErr|publish|Publish|fail|Create\(|Update\(|PubAck|\.acks|\.naks|\.terms|MaxDeliver|newProcess|newTerminalMarkerProcess' -- processor/agentic-loop/settlement_recovery_integration_test.go processor/agentic-loop/terminal_marker_redelivery_integration_test.go processor/agentic-loop/task_loop_id_integration_test.go processor/agentic-loop/publication_semantics_integration_test.go processor/agentic-loop/consumer_policy_integration_test.go` → 227.
- S21 `env GOCACHE=/private/tmp/semstreams-r4-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r4-gopls-cache gopls implementation processor/agentic-loop/settlement_recovery.go:28:6` → 3; completed through terminal session 39564; includes adjacent R2 test implementer `approval_restore_order_test.go:15`.
- S22 `git grep -n -E 'request publication|created publication|initial request|initial-request|dropped|DeleteStream|no responders|publish result|publish.*(fail|uncertain)|required.*(write|output)|requestErr|responseErr' -- 'processor/agentic-loop/*test.go' 'processor/agentic-model/*test.go'` → 59.
- S23 `git grep -n -E 'Create\(|Update\(|selectTerminalOutcome|publishResults\(|PublishToStream|stampSynthetic|stampLoop|observeTerminal|terminalPublication|terminalOutcome|COMPLETE_|DeliveryDecisionAck|mustHeartbeat|settleHandler|WorkFunc|LoopEntity' -- processor/agentic-loop/component.go` → 71.
- S24 `sed -n '70,155p;199,245p' processor/agentic-loop/persist_handler_result_test.go` → 132.
- S25 `sed -n '193,263p' processor/agentic-loop/lineage_preflight_test.go` → 71.
- S26 `sed -n '169,181p;300,311p' openspec/changes/agentic-loop-restart-safety/tasks.md` → 25.
- S27 `env GOCACHE=/private/tmp/semstreams-r4-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r4-gopls-cache gopls symbols processor/agentic-loop/terminal_selection_test.go` → 19.
- S28 `git grep -n -E '^func Test|providerCalls|calls.Load|calls,|executions|require.*(Ack|ack|Call|call|Retry)|retained|absence|Absent' -- processor/agentic-model/provider_settlement_integration_test.go processor/agentic-model/provider_settlement_test.go` → 59.
- S29 `shasum -a 256 processor/agentic-loop/component.go processor/agentic-loop/handlers.go processor/agentic-loop/settlement_recovery.go processor/agentic-loop/state.go processor/agentic-loop/settlement_recovery_test.go processor/agentic-loop/settlement_recovery_integration_test.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/persist_handler_result_test.go processor/agentic-loop/lineage_preflight_test.go processor/agentic-loop/spawn_identity_failure_test.go processor/agentic-loop/terminal_marker_redelivery_integration_test.go processor/agentic-loop/terminal_tool_recovery_test.go processor/agentic-loop/terminal_tool_redelivery_integration_test.go processor/agentic-loop/task_loop_id_integration_test.go processor/agentic-loop/publication_semantics_integration_test.go processor/agentic-loop/consumer_policy_integration_test.go processor/agentic-loop/terminal_selection_test.go processor/agentic-model/provider_settlement_integration_test.go processor/agentic-model/provider_settlement_test.go` → 19.
- S30 `git grep -n -E 'processor/agentic-loop/|processor/agentic-model/|agentic/loop' -- openspec/changes/agentic-loop-restart-safety/inventory-task4-loop-settlement-postimpl-2026-09-07.md openspec/changes/agentic-loop-restart-safety/inventory-task4-continuation-response-proof-2026-09-08.md` → 390.
- S31 `sed -n '20,28p;188,257p;261,370p' processor/agentic-loop/terminal_selection_test.go` → 166.
- S32 `git grep -n -E 'loopSettlementEvidenceReader|ReadAgentRequest|ReadAgentResponse|recovered|Created|TaskID|newTaskRequest|PublishedMessages|agent.created|agent.request|DeleteLoop|defer|rollback' -- processor/agentic-loop/handlers.go processor/agentic-loop/settlement_recovery.go` → 83.
- S33 `git grep -n -E 'require\.(Equal|NotEqual|Error|ErrorIs|ErrorContains|Contains|NotContains|Empty|Len|True|False|Zero|NotZero)|putErr|failPut|testLineageWriteHook|NewTestClient|testBefore|testAfter' -- processor/agentic-loop/lineage_preflight_test.go processor/agentic-loop/persist_handler_result_test.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/settlement_recovery_test.go processor/agentic-loop/terminal_tool_recovery_test.go processor/agentic-loop/spawn_identity_failure_test.go` → 323.
- S34 `sed` range outputs above were indexed in memory into file:line pins; no new source search.
- S35 NOT LOCATED — a test directly injecting initial `agent.request` publication failure in `handleTaskMessage`, with source ACK/NAK and `pendingTaskResults` redelivery assertions, under the recorded test-name and publication-literal searches.
- S36 NOT LOCATED — a test directly injecting `agent.created` publication failure in `handleTaskMessage`, with source ACK/NAK and `pendingTaskResults` redelivery assertions, under the recorded searches.
- S37 NOT RUN — further task request/created publication fixture searches; bounded inventory stop.
- S38 NOT RUN — integration-build-tag `gopls references` and remaining whole-symbol consumer expansion; recorded native literal pins only.
- S39 NOT RUN — `gh issue list`, `gh pr list`, live issue/PR bodies and `openspec list`; root-owned pickup per brief.
- S40 NOT RUN — tests, Docker checks, or remote mutations; test execution/host state assigned to root.
- S41 `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r4-evidence-2026-09-14.md` → initial DRIFT on four pins containing escaped inline backticks; pin text quoting corrected.
