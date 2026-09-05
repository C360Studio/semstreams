# Inventory: #759 held semantic-settlement bindings after #1210 and #1231
base: 39444c9de649775a4be6866a946b7d73400f4639

## Reproducible read-only sister checkpoints

| Repository | Checkpoint | Porcelain state | Working-tree diff SHA-256 | Staged diff SHA-256 | Untracked-content SHA-256 | Porcelain-state SHA-256 |
|---|---|---|---|---|---|---|
| SemSpec | `5a9496eecc453747f4bc557b95444db6304c1420` | dirty | `4d264d7a61e8259ee4c3c629abfbeaf889b73f263fd4588d07f8a97eb01b6816` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `cedfa283583e3d6672cf76a3b8212ef6bcf604b083b36248120a102931d04918` | `5707ccb2c2a6b2eb652110b4d1b9ce0bd9973d3944dab14a38cc708c181d6b10` |
| SemDragon | `07f4de9b65887801ff18a7273d14233023049321` | clean | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

SemSpec and SemDragon were inspected read-only; no sister repository state was changed.

## Claimed gap

### Shared typed settlement surface

- `natsclient/delivery_settlement.go:16` — `// DeliveryDecision is the owner-supplied semantic outcome for one delivery.`
- `natsclient/delivery_settlement.go:22` — `// DeliveryDecisionAck declares that the owner-defined durable consequence completed.`
- `natsclient/delivery_settlement.go:24` — `// DeliveryDecisionRetry declares a repairable semantic failure.`
- `natsclient/delivery_settlement.go:26` — `// DeliveryDecisionTerminate declares immutable poison for this delivery.`
- `natsclient/delivery_settlement.go:28` — `// DeliveryDecisionQuarantine declares that retry and termination are not proven safe.`
- `natsclient/delivery_settlement.go:32` — `// DeliveryAttempt is the server-observed attempt number for one delivery.`
- `natsclient/delivery_settlement.go:51` — `type DeliveryWork func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error)`
- `natsclient/delivery_settlement.go:229` — `// DeliveryResult is the immutable semantic and local transport observation`
- `natsclient/delivery_settlement.go:268` — `func (r DeliveryResult) ServerConfirmed() bool { return false }`
- `natsclient/delivery_settlement.go:273` — `// OwnerStopRequired reports that the exact delivery owner must stop its lane.`
- `natsclient/delivery_settlement.go:295` — `// ConsumeDeliveryWithHeartbeat runs setup-validated work, renews the delivery`
- `natsclient/delivery_settlement.go:348` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:399` — `func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {`
- `natsclient/delivery_settlement.go:437` — `return msg.Ack()`
- `natsclient/delivery_settlement.go:439` — `return msg.Nak()`
- `natsclient/delivery_settlement.go:441` — `return msg.NakWithDelay(delay)`
- `natsclient/delivery_settlement.go:443` — `return msg.Term()`
- `natsclient/heartbeat.go:77` — `func ConsumeWithHeartbeat(`

### Held model binding

- `processor/agentic-model/component.go:399` — `if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,`
- `processor/agentic-model/component.go:401` — `c.handleRequest(workCtx, msg.Data())`
- `processor/agentic-model/component.go:402` — `return nil`
- `processor/agentic-model/component.go:413` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-model/component.go:583` — `func (c *Component) handleRequest(ctx context.Context, data []byte) {`
- `processor/agentic-model/component.go:590` — `c.logger.Error("Failed to parse agent request", "error", err)`
- `processor/agentic-model/component.go:603` — `c.publishErrorResponse(ctx, req.RequestID, err.Error())`
- `processor/agentic-model/component.go:736` — `c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)`
- `processor/agentic-model/component.go:777` — `c.logger.Error("Failed to publish response", "error", err)`
- `processor/agentic-model/component.go:1049` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-model/component.go:1082` — `c.logger.Error("Failed to publish error response", "error", err)`

### Held loop task, response, and tool-result bindings

- `processor/agentic-loop/component.go:121` — `type inputHandler func(context.Context, []byte) error`
- `processor/agentic-loop/component.go:177` — `func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {`
- `processor/agentic-loop/component.go:891` — `case "agent.task":`
- `processor/agentic-loop/component.go:892` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:893` — `case "agent.response":`
- `processor/agentic-loop/component.go:894` — `handler = adaptVoidInputHandler(c.handleResponseMessage)`
- `processor/agentic-loop/component.go:895` — `case "tool.result":`
- `processor/agentic-loop/component.go:896` — `handler = adaptVoidInputHandler(c.handleToolResultMessage)`
- `processor/agentic-loop/component.go:989` — `case "agent.response", "tool.result":`
- `processor/agentic-loop/component.go:1027` — `if err := consumeLongRunningInput(msgCtx, msg, hi, handler); err != nil {`
- `processor/agentic-loop/component.go:1055` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-loop/component.go:1090` — `func consumeLongRunningInput(`
- `processor/agentic-loop/component.go:1096` — `return natsclient.ConsumeWithHeartbeat(ctx, msg, heartbeatInterval, func(workCtx context.Context) error {`
- `processor/agentic-loop/component.go:1150` — `workCtx, cancel := context.WithTimeout(consumerCtx, workTimeout)`
- `processor/agentic-loop/component.go:1161` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1165` — `return nil`
- `processor/agentic-loop/component.go:1171` — `return nil`
- `processor/agentic-loop/component.go:1178` — `return natsclient.TerminateDelivery(err)`
- `processor/agentic-loop/component.go:1198` — `return nil`
- `processor/agentic-loop/component.go:1201` — `return nil`
- `processor/agentic-loop/component.go:1210` — `return nil`
- `processor/agentic-loop/component.go:1393` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:1448` — `c.logger.Warn("No loop found for request", "request_id", responsePtr.RequestID)`
- `processor/agentic-loop/component.go:1450` — `c.metrics.recordModelResponseDropped("stale_request_id")`
- `processor/agentic-loop/component.go:1780` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:1808` — `c.logger.Warn("No loop found for tool call", "call_id", toolResult.CallID)`
- `processor/agentic-loop/component.go:1810` — `c.metrics.recordToolResultDropped("stale_callid")`
- `processor/agentic-loop/component.go:1841` — `c.logger.Error("Failed to handle tool result", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:1880` — `c.logger.Error("Failed to publish message", "error", err, "subject", msg.Subject)`
- `processor/agentic-loop/component.go:2015` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) {`
- `processor/agentic-loop/component.go:2033` — `c.logger.Error("Failed to persist loop state", "error", err, "loop_id", loopID)`

### Held AgentRun complete and failed bindings

- `agentic/agentrun/agentrun.go:563` — `// HandleEvent processes a raw NATS message payload from agent.complete.* or`
- `agentic/agentrun/agentrun.go:575` — `func (s *MilestoneSubscriber) HandleEvent(ctx context.Context, data []byte) error {`
- `agentic/agentrun/agentrun.go:593` — `s.logger.Warn("agentrun: HandleEvent: run resolution failed — skipping handlers",`
- `agentic/agentrun/agentrun.go:598` — `return nil`
- `agentic/agentrun/agentrun.go:606` — `s.logger.Error("agentrun: MilestoneHandler panicked",`
- `agentic/agentrun/agentrun.go:613` — `s.logger.Warn("agentrun: MilestoneHandler error",`
- `agentic/agentrun/agentrun.go:621` — `return nil`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `agentic/agentrun/agentrun.go:829` — `FilterSubject: "agent.complete.*",`
- `agentic/agentrun/agentrun.go:836` — `runCtx, completeCfg, handleMsg("agent.complete.*"),`
- `agentic/agentrun/agentrun.go:880` — `FilterSubject: "agent.failed.*",`
- `agentic/agentrun/agentrun.go:887` — `runCtx, failedCfg, handleMsg("agent.failed.*"),`

### AgentRun terminal identity, fanout, and recovery facts

- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
- `internal/agentterminal/terminal.go:123` — `event := Event{SourceMessageID: base.ID(), Category: base.Type().Category}`
- `agentic/agentrun/agentrun.go:467` — `type LoopTerminalEvent struct {`
- `agentic/agentrun/agentrun.go:580` — `ev := LoopTerminalEvent{`
- `agentic/agentrun/agentrun.go:590` — `run, err := s.resolveRunForEvent(ctx, ev)`
- `agentic/agentrun/agentrun.go:637` — `return nil, nil //nolint:nilerr // deliberate: non-run loops have no run entity`
- `agentic/agentrun/agentrun.go:602` — `for i, h := range s.handlers {`
- `agentic/agentrun/agentrun.go:606` — `s.logger.Error("agentrun: MilestoneHandler panicked",`
- `agentic/agentrun/agentrun.go:612` — `if handlerErr := handler.OnLoopTerminal(ctx, ev, run); handlerErr != nil {`
- `agentic/agentrun/agentrun.go:621` — `return nil`
- `agentic/agentrun/agentrun.go:559` — `func (s *MilestoneSubscriber) AddHandler(h MilestoneHandler) {`
- `agentic/agentrun/agentrun.go:765` — `// Start wires the MilestoneSubscriber to the live NATS connection using DURABLE`
- `agentic/agentrun/agentrun.go:776` — `// it. Durable consumer offsets remain in NATS for restart recovery.`
- `agentic/agentrun/agentrun.go:797` — `makeDurable := func(suffix string) string {`
- `agentic/agentrun/agentrun.go:832` — `MaxDeliver:    5,`
- `agentic/agentrun/agentrun.go:842` — `// run — they create the AGENT stream and publish agent.complete.* /`
- `agentic/agentrun/agentrun.go:869` — `return func(context.Context) error { return nil }, nil`

`SourceMessageID` is present in the normalized terminal projection but is not a field of `LoopTerminalEvent` and is
not copied by the `LoopTerminalEvent` literal. `resolveRunForEvent` collapses every `runs.Get` error on the wire-ID
fast path to `(nil, nil)`, not only entity-not-found. Handler panic and error are logged per handler; the loop continues
to later handlers and returns nil, so the durable input ACKs after partial fanout. The subscriber has no durable
per-handler receipt authority. The production census found zero product `AddHandler` calls and zero product
`MilestoneHandler` implementations in SemStreams. A deployment with no visible AGENT stream returns a successful
no-op stop; otherwise stable complete/failed durable offsets survive restart, begin at `DeliverPolicy: "new"` on
first creation, and exhaust after five deliveries.

## Spellings of the fact

### Stable loop, request, call, and run identity

- `agentic/user_types.go:141` — `if err := validateLoopTokenField("loop_id", s.LoopID); err != nil {`
- `agentic/user_types.go:434` — `// validateLoopTokenField is the ONE home of the loop-token form refusal for`
- `agentic/approval.go:137` — `if err := validateLoopTokenField("loop_id", r.LoopID); err != nil {`
- `processor/agentic-loop/state.go:177` — `// GenerateLoopID returns an identity with the exact UUID semantics used by`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:1133` — `// GenerateRequestID creates a structured request ID that embeds the loop ID.`
- `processor/agentic-loop/state.go:1136` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1141` — `// GenerateToolCallID creates a structured tool call ID that embeds the loop ID.`
- `processor/agentic-loop/state.go:1169` — `// GetLoopForRequestWithRecovery retrieves the loop ID for a request ID,`
- `processor/agentic-loop/state.go:1193` — `// GetLoopForToolCallWithRecovery retrieves the loop ID for a tool call ID,`
- `agentic/agentrun/agentrun.go:255` — `// rootLoopID must be a framework-minted loop token — a canonical UUID (ADR-105,`
- `agentic/agentrun/agentrun.go:307` — `entityID, err := agentic.TryChainExecutionEntityID(org, platform, rootLoopID)`
- `agentic/agentrun/agentrun.go:581` — `LoopID:      normalized.LoopID,`
- `agentic/agentrun/agentrun.go:582` — `RunID:       normalized.RunID,`
- `agentic/agentrun/agentrun.go:583` — `RunEntityID: normalized.RunEntityID,`

### Process-only and durable state spellings

- `processor/agentic-loop/state.go:61` — `type LoopManager struct {`
- `processor/agentic-loop/state.go:62` — `loops                map[string]*agentic.LoopEntity`
- `processor/agentic-loop/state.go:63` — `contextManagers      map[string]*ContextManager`
- `processor/agentic-loop/state.go:72` — `requestToLoop        map[string]string`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop       map[string]string`
- `processor/agentic-loop/context_manager.go:45` — `type ContextManager struct {`
- `processor/agentic-loop/context_manager.go:51` — `regions          map[RegionType][]contextMessage`
- `processor/agentic-loop/config.go:433` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/component.go:1936` — `// Key pattern: COMPLETE_{loopID} for rules engine to watch.`
- `processor/agentic-loop/component.go:1950` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/trajectory_handler_wiring.go:35` — `// releaseLoopTransientState releases every per-loop in-memory aggregate the`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/state.go:471` — `// This is the release Component.releaseLoopTransientState performs when a loop`

### #1231 admission, continuation, read-through, terminal release, signal, and approval facts

- `processor/agentic-dispatch/loop_admission.go:320` — `// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.`
- `processor/agentic-dispatch/loop_admission.go:346` — `return loopLookup{outcome: loopLookupUnreadable, cause: persistErr}`
- `processor/agentic-dispatch/loop_admission.go:407` — `// mergeLoopFacts reconciles two observations of one loop.`
- `processor/agentic-dispatch/loop_admission.go:441` — `// mergeLoopState resolves the two observations of one loop's state on the same`
- `processor/agentic-loop/handlers.go:851` — `if task.LoopID != "" {`
- `processor/agentic-loop/handlers.go:856` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/state.go:40` — `// ErrLoopBusy is returned when a continuation names a loop that has work`
- `processor/agentic-loop/state.go:54` — `// (releaseLoopTransientState), so absence is the ordinary steady state`
- `processor/agentic-loop/approval_response_handler.go:161` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/approval_response_handler.go:183` — `c.logger.Error("Failed to handle approval response",`
- `processor/agentic-loop/approval_response_handler.go:189` — `if result.staleDrop {`
- `processor/agentic-loop/component.go:2239` — `entity.PauseRequested = true`
- `processor/agentic-loop/component.go:2280` — `entity.PauseRequested = false`
- `agentic/user_types.go:112` — `type UserSignal struct {`
- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `processor/agentic-loop/component.go:2096` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2119` — `c.handleCancelSignal(ctx, signal)`

The dispatch-local `SignalMessage` declaration/registration is deleted; remaining production signal transport is the
registered `agentic.UserSignal` handled by agentic-loop. Pause and resume fields/handlers remain live pending the ruled,
unclaimed #1239 deletion; cancel remains live.

### Durable positive/negative outcomes and downstream publish boundaries

- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `processor/agentic-loop/component.go:1491` — `// publishFailureEvents publishes failure events including workflow callback.`
- `processor/agentic-loop/component.go:1516` — `// Persist failure to KV first so watchers (rules engine,`
- `processor/agentic-loop/component.go:1531` — `// Publish last — every observable side effect is now in place.`
- `processor/agentic-loop/component.go:1538` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1637` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:2196` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`

### Held lane authority and replay matrix

| Held lane | Stable identity | Process-only facts | Durable facts and replay horizon | Readers / writers | Restart selection, discarded fields, and dedup |
|---|---|---|---|---|---|
| model `agent.request` | stable consumer `agentic-model-<subject>` plus `RequestID` | cached client keyed by URL/model, health and metrics | AGENT durable delivery; port `DeliverPolicy`, MaxDeliver, AckWait; response is a new AGENT message | model reads requests and writes `agent.response.<RequestID>`; loop reads response | redelivery resolves capability/endpoint/health again; served endpoint/provider are not durable on `AgentResponse`; response ID, served model, system fingerprint, and Responses previous-response ID do not survive the projection; publish has no deterministic Nats-Msg-Id |
| loop `agent.task` | stable consumer `agentic-loop-<subject>` plus minted loop UUID and task ID | loop/context maps, prompt/tools/metadata/routing maps | AGENT task delivery plus AGENT_LOOPS loop-ID `LoopEntity`; trajectory fact/evidence attempts | loop reads task, writes model/tool requests and loop state | after process replacement, task replay reconstructs a fresh process state from the task payload; intake performs no AGENT_LOOPS collision read and does not rehydrate a LoopEntity from AGENT_LOOPS |
| loop `agent.response` | stable consumer plus structured request ID containing loop ID | request-to-loop map and context manager | AGENT response delivery; AGENT_LOOPS updated loop; trajectory observations | model writes, loop reads | post-#1231 recovery helper requires the loop to remain in `m.loops`; terminal `DeleteLoop` removes loop and routing, so late response drops; no deterministic Nats-Msg-Id on loop/model publish |
| loop `tool.result` | stable consumer plus call ID / loop ID | tool-call routing, pending tools, call audit maps | AGENT result delivery; AGENT_LOOPS updated loop; TOOL_CALL_OUTCOMES belongs to tools, not loop | tools writes, loop reads | post-#1231 `DeleteLoop` removes linked routing; known unlinked model-authored call audit residue remains; late result drops when routing/loop is absent |
| AgentRun complete/failed | stable complete/failed durables plus loop/run IDs | handler slice only | AGENT terminal delivery, lifecycle AgentRun in ENTITY_STATES, durable offsets; first creation uses `new`, MaxDeliver 5 | loop writes; AgentRun reads; product handlers absent in framework | `SourceMessageID` is discarded before handler event; no per-handler receipt, so redelivery cannot resume partial fanout |

- `processor/agentic-model/component.go:337` — `consumerName := fmt.Sprintf("agentic-model-%s", sanitizeSubject(subject))`
- `processor/agentic-model/component.go:375` — `cfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-model/component.go:600` — `client, endpoint, capability, endpointName, err := c.getClientForRequest(req)`
- `processor/agentic-model/component.go:809` — `chain := c.modelRegistry.GetFallbackChain(req.Model)`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `model/wire/types.go:51` — `type ChatCompletionResponse struct {`
- `model/wire/responses/types_response.go:49` — `// PreviousResponseID is set when this response continued from a`
- `processor/agentic-model/client_wire.go:273` — `response := agentic.AgentResponse{RequestID: requestID}`
- `processor/agentic-model/client_responses.go:87` — `response := agentic.AgentResponse{RequestID: requestID}`
- `processor/agentic-loop/component.go:945` — `consumerName := fmt.Sprintf("agentic-loop-%s", sanitizeSubject(subject))`
- `processor/agentic-loop/state.go:471` — `// This is the release Component.releaseLoopTransientState performs when a loop`
- `processor/agentic-loop/state.go:497` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-model/component.go:1049` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`

### Durable authority namespace evidence

- `processor/agentic-loop/component.go:1950` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2032` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/research-graph-synthesize/adapters.go:78` — `return "search_result.complete." + loopID`
- `processor/research-graph-synthesize/adapters.go:170` — `_, err := s.kv.Put(ctx, loopCompletionKeyPrefix+loopID, envelope)`
- `processor/agentic-tools/outcomes.go:21` — `// completedOutcome is the immutable COMPLETED record. There is deliberately`
- `processor/agentic-tools/outcomes.go:79` — `func toolCallOutcomeKey(callID string) string {`
- `processor/agentic-tools/outcomes.go:145` — `return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome fingerprint does not match request")}`
- `graph/kvcatalog.go:58` — `entityStates := owned(BucketEntityStates, "graph-ingest",`
- `pkg/lifecycle/manager.go:186` — `bucket, err := graph.OpenCatalogReader(ctx, m.natsClient, graph.BucketEntityStates)`
- `agentic/trajectory_fact.go:18` — `TrajectoryBucketName = "AGENT_TRAJECTORIES"`
- `processor/agentic-loop/trajectory_recorder.go:119` — `// evidence resolution goes through StoreRegistry on every operation.`

### Context ownership and goroutine joins

- `processor/agentic-model/component.go:263` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-model/component.go:528` — `binding.handle.Drain()`
- `processor/agentic-model/component.go:531` — `closed := binding.handle.Closed()`
- `processor/agentic-loop/component.go:484` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-loop/component.go:580` — `go func() {`
- `processor/agentic-loop/component.go:727` — `binding.handle.Drain()`
- `processor/agentic-loop/component.go:730` — `closed := binding.handle.Closed()`
- `processor/agentic-loop/component.go:1739` — `// runWithBudget runs fn in a goroutine and waits for it to return,`
- `processor/agentic-loop/component.go:1741` — `// returned (the goroutine continues running with a cancelled child`
- `processor/agentic-loop/component.go:1755` — `go func() {`
- `processor/agentic-loop/trajectory_handler_wiring.go:161` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `processor/agentic-loop/trajectory_handler_wiring.go:164` — `go func() {`
- `agentic/agentrun/agentrun.go:720` — `complete.Drain()`
- `agentic/agentrun/agentrun.go:727` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-model/client.go:477` — `if c.logger == nil || !c.logger.Enabled(context.Background(), slog.LevelDebug) {`
- `processor/agentic-loop/component.go:1755` — `go func() {`
- `processor/agentic-loop/component.go:1774` — `case <-bctx.Done():`
- `processor/agentic-loop/trajectory_handler_wiring.go:161` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `processor/agentic-loop/trajectory_handler_wiring.go:164` — `go func() {`
- `processor/agentic-loop/trajectory_handler_wiring.go:184` — `case <-ctx.Done():`

| Held owner | Root / derived authority | Goroutines | Join observation |
|---|---|---|---|
| agentic-model | `Start` derives `runCtx`; debug-level observation independently calls `context.Background()` | native JetStream callbacks are owned by consume handles | `Stop` drains every handle and waits exact `Closed`; no explicit model-owned worker goroutine was found |
| agentic-loop | `Start` derives `runCtx`; task handler derives work timeout; trajectory terminal batches use bounded `WithoutCancel(parent)` | approval sweeper, `runWithBudget`, and trajectory batch goroutines | sweeper has a done channel and is joined; consumer handles drain/join; `runWithBudget` and `recordTrajectoryBatchWithin` return on deadline without joining the still-running goroutine |
| AgentRun | `Start` derives `runCtx` | native complete/failed callbacks | owner drains both handles, awaits both `Closed`, then cancels |

No direct `context.Context` field was found in a production struct on the three held surfaces. Production structs do
store `context.CancelFunc` and function fields whose signatures accept contexts. The two bounded helpers above may
return before their spawned function exits; their contexts are cancelled, but completion is not joined before return.

## Adjacent claims

- `openspec/changes/semantic-jetstream-settlement/tasks.md:72` — `- [ ] 4.9 BLOCKED archive reconciliation: inventory and independently review the current SemSpec/SemDragon`
- `openspec/changes/semantic-jetstream-settlement/tasks.md:74` — `heartbeat requirements or remove natsclient mechanics from that capability. Do not infer their settlement`
- `openspec/specs/gated-dag-dispatch/spec.md:43` — `### Requirement: The framework provides a typed durable-consume primitive`
- `openspec/specs/jetstream-consumer-policy/spec.md:291` — `NewDurableHandler(cfg StreamConsumerConfig, heartbeat time.Duration,`
- #759 — natsclient: establish semantic JetStream settlement as the restart-safety foundation — OPEN, beta.163.
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart — OPEN, beta.163, `status:blocked`.
- #1155 — e2e(agentic): prove semantic-settlement quarantine and AgentRun redelivery across process replacement — OPEN, beta.163.
- #1225 — non-token TaskMessage validation silent drop — CLOSED by #1231.
- #1227 — reply-to continuation ownership/admission — CLOSED by #1231.
- #1228 — non-canonical loop-token carriers — CLOSED by #1231.
- #1233 — unreleased loop context managers — CLOSED by #1231.
- #1238 — approval/signal agentic E2E and `assertions_run=0` — OPEN, beta.163; draft PR #1245 is a claim-only PR with zero files.
- #1239 — pause/resume advertised absent surface — OPEN, beta.163; owner ruled deletion, but no draft PR claims or lands it.
- #1244 — StopAll-style declared loop exit contract — OPEN, beta.165; issue comment records #1146-before-#1244 sequencing.
- PR #1156 — `codex/gh759-semantic-settlement`, draft, closes #759 and #1155.
- PR #1159 — `codex/gh1146-agentic-loop-restart`, draft, closes #1146.
- PR #1245 — `claude/gh1238-agentic-e2e-approval-signal`, draft claim for #1238; GitHub reports zero files.

### Same-class collision table

| Semantic class | Owners | Catalog / declaration | Status / readiness | Lifecycle | Ownership | Readers | Writers | Recovery |
|---|---|---|---|---|---|---|---|---|
| AGENT durable work and terminal events | model, loop, tools, dispatch, AgentRun | component JetStream ports; ordinary stream provisioning | held model/loop/AgentRun lanes still use nil/error `ConsumeWithHeartbeat`; tools/dispatch use typed settlement | component `Start` owns stable native consumers; `Stop` drains and awaits exact `Closed` except the legacy sister adopters | stable durable name per port or AgentRun complete/failed suffix | model, loop, tools, dispatch, AgentRun | loop/model/tools/dispatch | unacked delivery redelivers within configured MaxDeliver; no per-handler AgentRun receipt authority |
| AGENT_LOOPS operational shared namespace | agentic-loop plus research-graph stage components | component KV ports; not in framework bucket catalog | no single owner-only readiness/catalog seam found | handles are component-owned; values survive restart | shared multi-writer bucket with unrelated key/payload families | dispatch read-through/activity, tools `read_loop_result`, research stages | loop ID `LoopEntity`, `COMPLETE_` terminals, research trigger/stage/snapshot keys | loop process maps are rebuilt only by live work; durable readers project records; no general process-state rehydration |
| TOOL_CALL_OUTCOMES completed-effect ledger | agentic-tools | framework bucket catalog: operational, owner-only, history 1, no TTL | acquired before consumers | component-owned KV handle | one owner; create-only digest key and fingerprint collision check | agentic-tools replay path | agentic-tools | completed result replay; absent record leaves the documented ambiguous external-effect window |
| ENTITY_STATES graph/lifecycle authority | graph-ingest | graph KV catalog, authoritative, History 1 | catalog acquisition/readiness | lifecycle and graph watchers join their owners | graph-ingest owns writes; lifecycle and gated-DAG read | lifecycle, gated-DAG, index/rule/graph readers | graph-ingest mutation boundary | current authority/watch bootstrap survives restart; gated-DAG derives status from presence markers |
| AGENT_TRAJECTORIES plus registered Store | agentic-loop | dedicated KV declaration, History 1/no TTL; StoreRegistry-selected evidence owner | unavailable evidence is observed as missing while work continues | recorder work is component-owned, but bounded detached batches may return before goroutine exit | immutable attempt-key facts; content-addressed evidence in selected Store | trajectory page reads facts/references only | agentic-loop recorder / registered Store | observed facts persist; evidence is never admission/recovery authority and missing evidence is not reconstructed |
| Gated-DAG dispatch and marker state | gated-DAG executor plus adopter consumer | ordinary dispatch stream plus cataloged ENTITY_STATES markers/claims | executor stream/readiness and adopter consumer readiness are separate | executor owns watch/dispatcher; adopter owns its durable consumer | claims and terminal/dirtied markers are graph facts; dispatch message uses unit ID | executor, SemSpec execution bridge, SemDragon questdag | executor claim/unclaim/publish; adopter terminal marker writers | Nats-Msg-Id dedup + Unclaim on publish error; consumer redelivery until adopter-defined done; stranded detector alerts |

### Contradictory gated-DAG publish-failure spellings

- `openspec/specs/gated-dag-dispatch/spec.md:30` — `The executor MUST clear a unit's durable claim when its dispatch publish fails`
- `openspec/specs/gated-dag-dispatch/spec.md:31` — `(the ack did not return), because a non-acked publish is proof the message was not`
- `docs/adr/070-gated-dag-durable-dispatch.md:71` — `return an error *after* the server already persisted the message (an ack-read`
- `processor/gated-dag/executor.go:427` — `// claim is surfaced but the claim is NOT rolled back — rolling it back would`
- `processor/gated-dag/executor.go:463` — `if uerr := e.claimer.Unclaim(ctx, unitID); uerr != nil {`

The current spec equates missing PubAck with proof of non-persistence; ADR-070 records the ack-read ambiguous case;
the executor's function comment says no rollback while its implementation calls `Unclaim`.

## Consumers

### Held physical consumers

- `processor/agentic-model/component.go:375` — `cfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-model/component.go:387` — `MaxDeliver:     consumerCfg.MaxDeliver,`
- `processor/agentic-model/component.go:388` — `AckWait:        ackWait,`
- `processor/agentic-loop/component.go:1005` — `cfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-loop/component.go:1011` — `MaxDeliver:     maxDeliver,`
- `processor/agentic-loop/component.go:1012` — `AckWait:        ackWait,`
- `processor/agentic-loop/component.go:1014` — `BackOff:        backOff,`
- `agentic/agentrun/agentrun.go:826` — `completeCfg := natsclient.StreamConsumerConfig{`
- `agentic/agentrun/agentrun.go:877` — `failedCfg := natsclient.StreamConsumerConfig{`

### Loop cache and durable read consumers

- `processor/agentic-dispatch/loop_tracker.go:96` — `type LoopTracker struct {`
- `processor/agentic-dispatch/loop_admission.go:338` — `tracked := c.loopTracker.getSnapshot(loopID)`
- `processor/agentic-dispatch/loop_admission.go:339` — `persisted, persistErr := c.loadPersistedLoop(ctx, loopID)`
- `processor/agentic-loop/component.go:2077` — `func (c *Component) findLoopIDForRequest(requestID string) string {`
- `processor/agentic-loop/component.go:2087` — `func (c *Component) findLoopIDForToolCall(callID string) string {`
- `agentic/agentrun/agentrun.go:627` — `func (s *MilestoneSubscriber) resolveRunForEvent(ctx context.Context, ev LoopTerminalEvent) (*AgentRun, error) {`
- `agentic/agentrun/agentrun.go:630` — `participant, err := s.runs.Get(ctx, WorkflowName, ev.RunEntityID)`

### Exported natsclient adopter seam evidence

| Sister repository | Read-only evidence |
|---|---|
| semspec | processor/execution-bridge/gated_dag_dispatch.go line 36 calls ConsumeDurable. |
| semspec | processor/execution-bridge/gated_dag_dispatch.go line 64 declares handleGatedDagDispatch. |
| semspec | processor/execution-bridge/gated_dag_dispatch.go line 75 waits for the unit terminal marker before returning nil. |
| semspec | processor/execution-bridge/gated_dag_dispatch.go line 97 returns a read/poll error; line 100 returns nil only after terminal observation. |
| semdragon | questdag/component.go line 294 calls ConsumeDurable. |
| semdragon | questdag/handler.go line 15 states nil ACK and nonnil NAK semantics. |
| semdragon | questdag/handler.go line 25 returns nil for an undecodable poison envelope. |
| semdragon | questdag/handler.go line 48 returns nil when a sub-quest is already beyond posted. |
| semdragon | questdag/handler.go line 95 returns an error when claim-start fails. |
| semdragon | questdag/handler.go line 103 returns nil after claim-start and reservation commit. |

| Adopter lane | Registration / enablement at checkpoint | Definition of done | Lifecycle / migration state |
|---|---|---|---|
| SemSpec execution bridge | factory-registered in `cmd/semspec/main.go`; enabled in shipped `configs/semspec.json` and E2E profiles | return nil only after polling ENTITY_STATES until the unit has a completed or failed marker | `Start` launches an untracked retry/consumer goroutine and `Stop` cancels without joining it; dirty, untracked `refocus-on-spec-authoring` material records removal of execution-bridge and gated-dag, but that plan is not committed at the checkpoint |
| SemDragon `questdagexec` | registered twice in component registry entry points and enabled in all three shipped configurations | legacy in-process event loop advances from quest/AGENT observations and owns four joined goroutines | current shipped lane; does not call removed `ConsumeDurable` |
| SemDragon `questdag` | code is present as the staged Stage-B2 replacement, but no `questdag.Register` call and no shipped config entry were found | nil after member reservation and `ClaimAndStartForParty` commit; malformed/already-beyond-posted values also return nil | owns a gated-DAG executor and calls removed `ConsumeDurable`; creates its consumer root with `context.Background`, cancels it on Stop without an exact consumer join; unregistered/unenabled at this checkpoint |

## Problem shape

### Semantic settlement: effect, durable guard, settlement

- `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:46` — `After keyed admission, graph effect precedes durable guard, which precedes ACK. Replayable effects declare stable`
- `processor/graph-ingest/component.go:1121` — `// cleanup preserves effect -> durable guard -> settlement by keeping callback,`
- `processor/graph-ingest/keyed_ingest.go:285` — `// ingestGuardStampDurable persists the last-applied sequence for (entity,`
- `natsclient/client.go:946` — `// PublishToStreamWithMsgID publishes to a JetStream stream stamping the`

### Read-through over a process cache

- `processor/graph-ingest/query.go:741` — `// Repopulate the read-through cache, but drop the Set if this key was`
- `processor/agentic-dispatch/loop_admission.go:320` — `// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.`

### Classified fail-closed owner shutdown

- `processor/agentic-tools/delivery_owner.go:11` — `// deliveryLaneAdmission is private to this component owner. JetStream retains`
- `processor/agentic-tools/delivery_owner.go:31` — `func (a *deliveryLaneAdmission) latch(result natsclient.DeliveryResult) {`
- `processor/agentic-tools/delivery_owner.go:32` — `if !result.OwnerStopRequired() {`
- `processor/agentic-tools/delivery_owner.go:45` — `a.fatal <- result`
- `processor/agentic-tools/delivery_owner.go:57` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `processor/agentic-tools/delivery_owner.go:85` — `binding.drain()`
- `processor/agentic-dispatch/delivery_owner.go:29` — `func (a *deliveryLaneAdmission) latch(result natsclient.DeliveryResult) {`
- `processor/agentic-dispatch/delivery_owner.go:43` — `a.fatal <- result`
- `processor/agentic-dispatch/delivery_owner.go:55` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `processor/agentic-dispatch/delivery_owner.go:84` — `binding.drain()`

## Searches

- `git rev-parse HEAD` → `39444c9de649775a4be6866a946b7d73400f4639`.
- `git merge-base HEAD origin/main` → `78813ec77fa78a8e942a41bf01d42d8caa742244`.
- `git grep -n '^## (Purpose|Product Boundary)' -- openspec/project.md` → 2 hits.
- `gopls workspace_symbol -matcher=fuzzy DeliveryWork` → 14 symbols after cache relocation; the first sandboxed call failed before workspace load.
- `gopls workspace_symbol -matcher=fuzzy DeliveryDecision` → initial combined attempt failed workspace load; the isolated retry is recorded below.
- `gopls workspace_symbol -matcher=fuzzy ConsumeWithHeartbeat` → 22 symbols.
- `gopls workspace_symbol -matcher=fuzzy consumeLongRunningInput` → 1 symbol.
- `gopls workspace_symbol -matcher=fuzzy MilestoneSubscriber` → 15 symbols.
- `gopls workspace_symbol -matcher=fuzzy LoopTracker` → 52 symbols.
- `gopls workspace_symbol -matcher=fuzzy AdmitLoopToken` → 0 symbols.
- `gopls workspace_symbol -matcher=fuzzy ParseLoopToken` → 0 symbols.
- `gopls workspace_symbol -matcher=fuzzy Release` → 64 symbols on the returned workspace page.
- `gopls references natsclient/heartbeat.go:77:6` → 16 references.
- `gopls references natsclient/delivery_settlement.go:51:6` → 6 references.
- `gopls references processor/agentic-loop/component.go:1090:6` → 7 references.
- `gopls references agentic/agentrun/agentrun.go:782:31` → 1 reference.
- `gopls references processor/agentic-dispatch/loop_tracker.go:96:6` → 25 references.
- `gopls references processor/agentic-loop/state.go:201:23` → 9 returned locations.
- `gopls implementation agentic/agentrun/agentrun.go:490:2` → 2 implementers, both tests/compat.
- `gopls call_hierarchy processor/agentic-model/component.go:583:21` → 1 caller, then gopls panicked while computing outgoing calls.
- `gopls call_hierarchy processor/agentic-loop/component.go:1090:6` → 6 callers and 2 callees.
- `gopls call_hierarchy agentic/agentrun/agentrun.go:575:31` → 10 callers and 9 callees.
- `git grep -n -E 'ConsumeWithHeartbeat|ConsumeDeliveryWithHeartbeat|HeartbeatDeliveryPolicy|DeliveryDecision|DeliveryResult|DeliveryAttempt|Ack\\(|Nak\\(|NakWithDelay|Term\\(|InProgress\\(' -- natsclient processor/agentic-model processor/agentic-loop agentic/agentrun openspec/changes/semantic-jetstream-settlement openspec/specs docs/adr docs/operations` → 456 output lines.
- `git grep -n 'ConsumeWithHeartbeat' -- '*.go' ':!**/*_test.go'` → 5 hits: declaration plus model, loop, and AgentRun callers.
- `git grep -n -E 'PublishToStreamWithAck|PublishWithAck|PubAck|msg\\.Ack|msg\\.Nak|msg\\.Term|msg\\.InProgress' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → 2 hits, both loop advisory settlement.
- `git grep -n -E '^func \\(c \\*Component\\) handle(Request|Task|Response|ToolResult)|^func .*InputHandler|type inputHandler|handler = ' -- processor/agentic-model processor/agentic-loop` → 11 hits.
- `git grep -n -E 'agent\\.task|agent\\.request|agent\\.response|tool\\.result|agent\\.complete|agent\\.failed' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → 61 hits.
- `git grep -n -E 'looptoken\\.|validateLoopTokenField|loop admission|admission|LoopAdmission|releaseLoopTransientState|settledLoopResult|read-through|read through|readThrough|POST /loops/.*/signal|PauseRequested|ApprovalResponse' -- agentic processor/agentic-loop processor/agentic-dispatch openspec/specs docs/adr docs/operations openspec/changes/semantic-jetstream-settlement` → 445 output lines.
- `git grep -n -E 'context\\.(Background|TODO|WithoutCancel)|context\\.With(Cancel|Timeout|CancelCause)|go func|errgroup|Wait\\(|Closed\\(|Drain\\(|Stop\\(' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → 34 hits.
- `git grep -n -E 'gated.?dag|GatedDAG|ConsumeWithHeartbeat|heartbeat|Ack\\(|Nak\\(|TerminateDelivery|nil.*ack|error.*nak' -- processor/gated-dag docs/adr/070-gated-dag-durable-dispatch.md openspec/specs openspec/changes/semantic-jetstream-settlement docs/operations/migration-restart-safe-nats-client.md` → 773 output lines across the paired gated-DAG searches.
- `git grep -n -E 'gated.?dag|GatedDAG|ConsumeWithHeartbeat|HeartbeatDeliveryPolicy|DeliveryDecision' -- . ':!vendor' ':!**/*_test.go'` → included in the preceding 773-line paired output.
- `git -C ../semspec grep -n -E 'ConsumeDurable|NewDurableHandler|ConsumeWithHeartbeat' -- '*.go' 'openspec/**/*.md'` → 1 production call.
- `git -C ../semdragon grep -n -E 'ConsumeDurable|NewDurableHandler|ConsumeWithHeartbeat' -- '*.go' 'openspec/**/*.md'` → 2 hits: one production call and one active-change task claim.
- `git -C ../semspec grep -n -E '^func \\(c \\*Component\\) handleGatedDagDispatch|return nil|return .*err|Poll|completed|failed' -- processor/execution-bridge/gated_dag_dispatch.go` → 13 hits.
- `git -C ../semdragon grep -n -E 'func \\(.*handleDispatch|return nil|return .*err|completed|failed|Ack|Nak' -- questdag/component.go questdag/handler.go` → 28 hits.
- `git grep -n -E 'effect.*durable guard|durable guard|read-through|read through|ReadThrough|OwnerStopRequired|closeAdmission|firstFatal|classified.*shutdown|fail closed|fail-closed|admission.*Drain|Drain.*admission' -- natsclient processor pkg graph service docs/adr openspec/specs ':!**/*_test.go'` → 103 hits.
- `git grep -n -E '^func \\(c \\*Client\\) ConsumeDurable|^func NewDurableHandler|^func \\(c \\*Client\\) ConsumeInternalStreamWithConfig|^func \\(c \\*Client\\) ConsumeStreamWithConfigContexts' -- natsclient` → 2 hits; `ConsumeDurable` and `NewDurableHandler` zero.
- `gopls workspace_symbol -matcher=fuzzy ConsumeDurable` → 0 SemStreams symbols.
- `gopls workspace_symbol -matcher=fuzzy ConsumeStreamWithConfigContexts` → 4 symbols.
- `git grep -n -E 'RequestID|LoopID|CallID|Nats-Msg-Id|Nats-Msg-ID|TOOL_CALL_OUTCOMES|AGENT_LOOPS|COMPLETE_|PublishToStream\\(' -- processor/agentic-model/component.go processor/agentic-loop/component.go processor/agentic-loop/handlers.go processor/agentic-loop/state.go agentic/agentrun/agentrun.go processor/agentic-loop/approval_response_handler.go ':!**/*_test.go'` → 174 hits.
- `git grep -n -E 'BucketAgentLoops|AGENT_LOOPS|TOOL_CALL_OUTCOMES|LoopEntityReader|readLoop|loopsBucket.Get|Get\\(ctx,.*loop' -- agentic processor/agentic-loop processor/agentic-dispatch openspec/specs docs/adr ':!**/*_test.go'` → 92 hits.
- `git grep -n -E '^func \\(c \\*Component\\) persistLoopState|Failed to persist loop state|Failed to publish message|No loop found|Failed to handle|Failed to parse|run resolution failed|MilestoneHandler error|panicked' -- processor/agentic-model/component.go processor/agentic-loop/component.go processor/agentic-loop/approval_response_handler.go agentic/agentrun/agentrun.go` → 11 hits.
- `git grep -n 'PublishToStream(ctx' -- natsclient` → 9 hits.
- `git grep -n -E 'func \\(c \\*Client\\) PublishToStream[^W]|func \\(c \\*Client\\) PublishToStream\\(' -- natsclient` → 0 hits because the receiver is named `m`.
- `git grep -n 'PublishToStream(' -- natsclient/*.go` → 15 hits.
- `git diff --stat b060511f..origin/main -- processor/agentic-loop/context_manager.go processor/agentic-loop/state.go processor/agentic-loop/handlers.go processor/agentic-loop/component.go processor/agentic-loop/approval_response_handler.go` → 4 changed files, 344 insertions, 37 deletions; `context_manager.go` zero diff.
- `git diff --name-status b060511f..origin/main -- processor/agentic-model processor/agentic-loop agentic/agentrun processor/agentic-dispatch docs/operations/migration-beta162-to-beta163.md docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md openspec/specs/agentic-loop openspec/specs/agentic-dispatch` → 48 paths.
- `gh issue list --state open --search 'semantic settlement' --json number,title,labels,milestone` → 5 issues.
- `gh issue list --state open --search 'restart safety' --json number,title,labels,milestone` → 6 issues.
- `gh issue list --state open --search 'loop transition' --json number,title,labels,milestone` → 7 issues.
- `gh issue list --state open --search 'gated DAG durable consumer' --json number,title,labels,milestone` → 6 issues.
- `gh issue view 1146,1155,1225,1227,1228,1233,1238,1239,1244,759 --json ...` → all 10 located; states recorded under Adjacent claims.
- `gh pr list --state open --json number,title,isDraft,headRefName,body` → 5 open draft PRs; three touch the named surface and are recorded above.
- `openspec list` → 1 active change, `semantic-jetstream-settlement` at 39/51 tasks.
- `openspec list --specs` → 51 specs, including `agentic-dispatch`, `agentic-loop`, `gated-dag-dispatch`, `jetstream-consumer-policy`, and `nats-streaming`.
- `gopls workspace_symbol -matcher=fuzzy DeliveryDecision` → first attempt failed workspace load on the sandboxed Go cache; retry with `GOCACHE=/tmp/gh759-gocache` returned 20 symbols while emitting non-fatal gopls-cache write diagnostics.
- `git grep -n 'SignalMessage' -- agentic processor/agentic-dispatch processor/agentic-loop` → 13 hits: production has only the live handler comment/name; retired type references are tests.
- `git grep -n -E 'PauseRequestedBy' -- ':!**/*_test.go'` → 1 production declaration.
- `git -C /Users/coby/Code/c360/semspec rev-parse HEAD`; status and SHA-256 checkpoint pipeline → HEAD and dirty hashes recorded under Reproducible read-only sister checkpoints.
- `git -C /Users/coby/Code/c360/semdragon rev-parse HEAD`; status and SHA-256 checkpoint pipeline → HEAD and clean hashes recorded under Reproducible read-only sister checkpoints.
- `git grep -n -E 'SourceMessageID|LoopTerminalEvent|resolveRunForEvent|MilestoneHandler|handlers|receipt|retention|MaxDeliver|AckWait|agent.complete|agent.failed|HandleEvent|ConsumeWithHeartbeat|NotFound|Err.*Found' -- agentic/agentrun agentic processor/agentic-loop openspec/specs docs ':!**/*_test.go'` → 1,151 output lines; AgentRun facts narrowed and pinned above.
- `git grep -n -E 'SourceMessageID|source_message_id|MessageID' -- agentic processor/agentic-loop processor/agentic-dispatch docs/adr/053-agent-run-substrate.md openspec/specs/agent-run* ':!**/*_test.go'` → shell glob failed before execution (`no matches found`).
- `git grep -n -E 'SourceMessageID|source_message_id|MessageID' -- agentic processor/agentic-loop processor/agentic-dispatch docs/adr/053-agent-run-substrate.md openspec/specs ':!**/*_test.go'` → 7 hits.
- `git grep -n -E 'SourceMessageID|source_message_id' -- . ':!**/*_test.go'` → 5 hits.
- `git grep -n -E '\.AddHandler\(|OnLoopTerminal\(' -- '*.go' ':!**/*_test.go'` → 2 declaration/internal-call hits; zero product handlers.
- `git grep -n -E 'AGENT_LOOPS|TOOL_CALL_OUTCOMES|ENTITY_STATES|StreamName.*AGENT|Stream.*AGENT|Trajectory|trajectory|ObjectStore|Store\(' -- processor/agentic-model processor/agentic-loop processor/agentic-tools processor/agentic-dispatch processor/graph-research agentic natsclient storage pkg/lifecycle openspec/specs/framework-bucket-catalog openspec/specs/agentic-loop openspec/specs/agentic-dispatch docs/adr ':!**/*_test.go'` → 1,528 output lines.
- `git grep -n -E 'BucketDescriptor|AGENT_LOOPS|TOOL_CALL_OUTCOMES|ENTITY_STATES|AGENT_TRAJECTORIES' -- natsclient component processor agentic pkg openspec/specs/framework-bucket-catalog ':!**/*_test.go'` → 482 output lines.
- `git grep -n -E 'PubAck|publish.*fail|publish failure|ack.*read|ack-read|Unclaim|unclaim|claim' -- openspec/specs/gated-dag-dispatch/spec.md docs/adr/070-gated-dag-durable-dispatch.md processor/gated-dag/executor.go` → contradictory spec/ADR/executor spellings pinned above.
- `git grep -n -E 'type (claimer|publisher|Reader)|Claim\(|Unclaim\(|Dispatch\(|Mark|marker|Consume|Start\(|Stop\(|Drain|Closed|Durable|ConsumerName|StreamName|Bucket|Store|KV' -- processor/gated-dag pkg/gateddag openspec/specs/gated-dag-dispatch ':!**/*_test.go'` → gated-DAG lifecycle/authority surface located.
- `git -C /Users/coby/Code/c360/semspec grep -n -E 'execution-bridge|gated_dag|gated-dag|ConsumeDurable|NewDurableHandler|Register.*execution|enabled' -- cmd configs processor openspec ':!**/*_test.go'` → 839 output lines.
- `rg -n 'execution-bridge|gated-dag|gated_dag|ConsumeDurable' openspec/changes/refocus-on-spec-authoring docs/audit docs/adr/ADR-052-spec-authoring-product-refocus.md` in SemSpec → 6 removal-plan hits; required because the plan is untracked and absent from `git grep`.
- `git -C /Users/coby/Code/c360/semdragon grep -n -E 'questdagexec|questdag|ConsumeDurable|NewDurableHandler|Register|enabled|questdag.exec' -- cmd configs questdag processor openspec ':!**/*_test.go'` → 541 output lines.
- `git -C /Users/coby/Code/c360/semdragon grep -n -E 'questdagexec\.Register|questdag\.Register|"questdagexec"|"questdag"' -- componentregistry cmd configs ':!**/*_test.go'` → 2 registration hits, both `questdagexec.Register`.
- `git -C /Users/coby/Code/c360/semdragon grep -n -E '"questdagexec"|"questdag"' -- configs '*.json'` → 6 shipped-config hits, all `questdagexec`.
- `gh pr view 1245 --json number,state,isDraft,headRefName,files,commits,mergeStateStatus,statusCheckRollup` → OPEN draft, one claim commit, zero files.
- `gh issue view 1239 --json number,state,title,labels,milestone,comments` → OPEN; owner deletion ruling explicitly says the comment is not a claim.
- `gh issue view 1244 --json number,state,title,labels,milestone,comments` → OPEN; composed exit/settlement sequencing located.
- `git grep -n -E 'context\.(Background|TODO|WithoutCancel)|context\.With(Cancel|Timeout|CancelCause)|go func|Wait\(|Drain\(|Closed\(' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → held-path root, detach, goroutine, drain, and join sites pinned above.
- `git grep -n -E 'context\.Context|ctx[[:space:]]+context\.' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → function/method parameters and function-typed fields; no direct production struct context field located.
- `git grep -n -E 'type Agent(Request|Response)|Provider|Fingerprint|Continuation|PreviousResponse|ResponseID|Model|RequestID|Nats-Msg-Id|PublishToStreamWithMsgID|DeleteLoop|GetLoopForRequestWithRecovery|GetLoopForToolCallWithRecovery|requestToLoop|toolCallToLoop|endpoint' -- agentic processor/agentic-model processor/agentic-loop natsclient ':!**/*_test.go'` → 801 output lines.
- `git grep -n -E 'SystemFingerprint|system_fingerprint|PreviousResponse|previous_response|ResponseID|response_id|resp\.ID|resp\.Model|Provider' -- processor/agentic-model model agentic ':!**/*_test.go'` → provider-response fields and projections located.
- `git grep -n -E 'PublishToStreamWithMsgID|PublishToStream\(' -- processor/agentic-model processor/agentic-loop agentic/agentrun ':!**/*_test.go'` → 9 production publishes, all plain `PublishToStream`; zero `PublishToStreamWithMsgID`.
- `git grep -n -E 'ConsumerName|consumerName :=|makeDurable|GenerateConsumer|StableConsumer|PortConsumer' -- processor/agentic-model processor/agentic-loop agentic/agentrun component natsclient ':!**/*_test.go'` → stable held consumer identities located.
- `git grep -n -E 'loopsBucket\.(Put|Create|Update)|\.Put\(ctx,.*loop|LoopStore.*(Put|Write)|COMPLETE_|FAILED_|search_result|research_' -- processor/research-graph-* agentic/research processor/agentic-loop processor/agentic-dispatch processor/agentic-tools ':!**/*_test.go'` → AGENT_LOOPS multi-writer key/payload families located.
- `git grep -n -E 'TOOL_CALL_OUTCOMES|OutcomeStore|outcomes' -- processor/agentic-tools processor/agentic-loop processor/agentic-dispatch openspec/specs/framework-bucket-catalog ':!**/*_test.go'` → outcome owner/read/write sites located.
- `git grep -n -E 'BucketEntityStates|ENTITY_STATES.*ClassAuthoritative|Owner.*graph-ingest|History.*3|Descriptor' -- natsclient graph processor/graph-ingest pkg/lifecycle openspec/specs/graph-retention openspec/specs/framework-bucket-catalog ':!**/*_test.go'` → ENTITY_STATES catalog owner/readers located.
- `git grep -n -E 'AGENT_TRAJECTORIES|TrajectoryBucketName|trajectory.*Store|StoreResolver|storeregistry|EvidenceProvider|evidence' -- processor/agentic-loop agentic storage openspec/specs ':!**/*_test.go'` → 381 output lines.
- `git grep -n -E 'type UserSignal|CategorySignal|handleSignalMessage|handleCancelSignal|handlePauseSignal|handleResumeSignal|agent.signal|PauseRequestedBy' -- agentic processor/agentic-loop processor/agentic-dispatch docs openspec/specs ':!**/*_test.go'` → live UserSignal/pause/cancel surface located.
- `git -C /Users/coby/Code/c360/semspec grep -n -E 'executionbridge\.Register|execution-bridge.*enabled|"execution-bridge"' -- cmd configs processor ':!**/*_test.go'` → one registration and shipped config declarations.
- `git -C /Users/coby/Code/c360/semdragon grep -n -E 'questdagexec\.Register|questdag\.Register|"questdagexec"|"questdag"' -- componentregistry config questdag processor/questdagexec ':!**/*_test.go'` → two `questdagexec` registrations, three shipped config declarations, zero `questdag.Register`.
- `rg -n 'NOT RUN' openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md` → one stale pre-retry record, corrected above; zero remaining unexecuted searches.
