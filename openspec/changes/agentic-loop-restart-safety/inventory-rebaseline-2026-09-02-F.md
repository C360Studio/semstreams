# Inventory: #1146 rebaseline against staged settlement foundation F

base: 417beae5552f8f15ad3540edd7d8504c87174c13

## Checkpoint

Worktree HEAD: `35ebf142a0d40f9307419429abe1c1cf7dfb4f39`.
Required and observed merge base: `417beae5552f8f15ad3540edd7d8504c87174c13` (`F`).
`git diff F..HEAD` contains only the active `agentic-loop-restart-safety` OpenSpec change.
Scope: SemStreams only. No sister repository was inspected or changed.

## Claimed gap

### Settlement foundation present at F

- `natsclient/delivery_settlement.go:16` — `// DeliveryDecision is the owner-supplied semantic outcome for one delivery.`
- `natsclient/delivery_settlement.go:32` — `// DeliveryAttempt is the server-observed attempt number for one delivery.`
- `natsclient/delivery_settlement.go:51` — `type DeliveryWork func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error)`
- `natsclient/delivery_settlement.go:153` — `// HeartbeatDeliveryPolicy is an immutable, setup-validated policy for one`
- `natsclient/delivery_settlement.go:165` — `func ValidateHeartbeatDeliveryPolicy(`
- `natsclient/delivery_settlement.go:229` — `// DeliveryResult is the immutable semantic and local transport observation`
- `natsclient/delivery_settlement.go:295` — `// ConsumeDeliveryWithHeartbeat runs setup-validated work, renews the delivery`
- `natsclient/delivery_settlement.go:298` — `func ConsumeDeliveryWithHeartbeat(`
- `natsclient/delivery_settlement.go:348` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:399` — `func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {`
- `natsclient/delivery_settlement.go:437` — `return msg.Ack()`
- `natsclient/delivery_settlement.go:439` — `return msg.Nak()`
- `natsclient/delivery_settlement.go:443` — `return msg.Term()`
- `natsclient/heartbeat.go:76` — `// non-default #759 integration branch while model, loop, and AgentRun migrate.`
- `natsclient/heartbeat.go:79` — `func ConsumeWithHeartbeat(`
- `natsclient/heartbeat.go:103` — `if err := msg.InProgress(); err != nil {`
- `natsclient/consumer_policy_callsite_test.go:415` — `// TestLegacyHeartbeatProductionCallZeroGrowthStagingGuard prevents another`
- `natsclient/consumer_policy_callsite_test.go:419` — `func TestLegacyHeartbeatProductionCallZeroGrowthStagingGuard(t *testing.T) {`
- `natsclient/consumer_policy_callsite_test.go:449` — `t.Fatalf("legacy ConsumeWithHeartbeat callers = %#v, want exact branch-staging set %#v", scan.directCalls, want)`

### Legacy helper callers inherited from F

- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `processor/agentic-loop/component.go:1096` — `return natsclient.ConsumeWithHeartbeat(ctx, msg, heartbeatInterval, func(workCtx context.Context) error {`
- `processor/agentic-model/component.go:399` — `if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,`

### Typed settlement callers inherited from F

- `processor/agentic-dispatch/delivery_owner.go:55` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `processor/agentic-tools/delivery_owner.go:57` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `processor/agentic-tools/delivery_owner.go:77` — `) {`
- `processor/agentic-dispatch/delivery_owner.go:75` — `) {`

### Direct settlement operations in the scoped production surface

- `processor/agentic-loop/component.go:1034` — `_ = msg.Nak()`
- `processor/agentic-loop/component.go:1038` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:558` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:621` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:697` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-governance/component.go:504` — `if ackErr := msg.Ack(); ackErr != nil {`
- `natsclient/stream.go:772` — `_ = msg.Nak()`

### Loop consumer acquisition, latency classification, handle ownership, and drain

- `processor/agentic-loop/component.go:121` — `type inputHandler func(context.Context, []byte) error`
- `processor/agentic-loop/component.go:177` — `func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {`
- `processor/agentic-loop/component.go:891` — `case "agent.task":`
- `processor/agentic-loop/component.go:892` — `handler = c.taskInputHandler(30 * time.Minute)`
- `processor/agentic-loop/component.go:893` — `case "agent.response":`
- `processor/agentic-loop/component.go:894` — `handler = adaptVoidInputHandler(c.handleResponseMessage)`
- `processor/agentic-loop/component.go:895` — `case "tool.result":`
- `processor/agentic-loop/component.go:896` — `handler = adaptVoidInputHandler(c.handleToolResultMessage)`
- `processor/agentic-loop/component.go:897` — `case "agent.signal":`
- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:900` — `handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)`
- `processor/agentic-loop/component.go:909` — `handler = adaptVoidInputHandler(c.handleToolCallVerdictMessage)`
- `processor/agentic-loop/component.go:989` — `case "agent.response", "tool.result":`
- `processor/agentic-loop/component.go:1027` — `if err := consumeLongRunningInput(msgCtx, msg, hi, handler); err != nil {`
- `processor/agentic-loop/component.go:1044` — `consume := c.natsClient.ConsumeStreamWithConfigContexts`
- `processor/agentic-loop/component.go:1055` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-loop/component.go:1090` — `func consumeLongRunningInput(`
- `processor/agentic-loop/component.go:1096` — `return natsclient.ConsumeWithHeartbeat(ctx, msg, heartbeatInterval, func(workCtx context.Context) error {`
- `processor/agentic-loop/component.go:1150` — `workCtx, cancel := context.WithTimeout(consumerCtx, workTimeout)`

### Loop task, response, and tool-result exits

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

### Loop signal, approval, and governance-verdict exits

- `processor/agentic-loop/component.go:2096` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2119` — `c.handleCancelSignal(ctx, signal)`
- `processor/agentic-loop/component.go:2239` — `entity.PauseRequested = true`
- `processor/agentic-loop/component.go:2280` — `entity.PauseRequested = false`
- `processor/agentic-loop/approval_response_handler.go:31` — `func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (result HandlerResult, err error) {`
- `processor/agentic-loop/approval_response_handler.go:161` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/approval_response_handler.go:183` — `c.logger.Error("Failed to handle approval response",`
- `processor/agentic-loop/approval_response_handler.go:189` — `if result.staleDrop {`
- `processor/agentic-loop/component.go:2321` — `func (c *Component) handleToolCallVerdictMessage(_ context.Context, data []byte) {`
- `processor/agentic-loop/governance_dispatcher.go:334` — `type enforceDispatcher struct {`
- `processor/agentic-loop/governance_dispatcher.go:468` — `func (d *enforceDispatcher) HandleVerdict(decision, callID string, data []byte) {`
- `processor/agentic-loop/governance_dispatcher.go:571` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`

### Model request lane

- `processor/agentic-model/component.go:337` — `consumerName := fmt.Sprintf("agentic-model-%s", sanitizeSubject(subject))`
- `processor/agentic-model/component.go:375` — `cfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-model/component.go:399` — `if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,`
- `processor/agentic-model/component.go:583` — `func (c *Component) handleRequest(ctx context.Context, data []byte) {`
- `processor/agentic-model/component.go:590` — `c.logger.Error("Failed to parse agent request", "error", err)`
- `processor/agentic-model/component.go:600` — `client, endpoint, capability, endpointName, err := c.getClientForRequest(req)`
- `processor/agentic-model/component.go:603` — `c.publishErrorResponse(ctx, req.RequestID, err.Error())`
- `processor/agentic-model/component.go:736` — `c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)`
- `processor/agentic-model/component.go:777` — `c.logger.Error("Failed to publish response", "error", err)`
- `processor/agentic-model/component.go:1049` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-model/component.go:1082` — `c.logger.Error("Failed to publish error response", "error", err)`

### Tools call lane and immutable completed-outcome authority

- `processor/agentic-tools/component.go:393` — `// HeartbeatInterval new in this PR — without it, a tool taking longer`
- `processor/agentic-tools/component.go:424` — `deliveryPolicy, err := natsclient.ValidateHeartbeatDeliveryPolicy(`
- `processor/agentic-tools/component.go:510` — `func (c *Component) handleToolDelivery(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-tools/component.go:671` — `func (c *Component) handleToolCall(ctx context.Context, data []byte) error {`
- `processor/agentic-tools/component.go:821` — `return completedOutcome{}, false, fmt.Errorf("read tool-call outcome: %w", err)`
- `processor/agentic-tools/outcomes.go:21` — `// completedOutcome is the immutable COMPLETED record. There is deliberately`
- `processor/agentic-tools/outcomes.go:79` — `func toolCallOutcomeKey(callID string) string {`
- `processor/agentic-tools/outcomes.go:83` — `func toolResultMessageID(callID string) string {`
- `processor/agentic-tools/outcomes.go:145` — `return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome fingerprint does not match request")}`
- `openspec/specs/framework-bucket-catalog/spec.md:13` — `### Requirement: The framework catalog SHALL own durable tool-call outcomes`

### Dispatch user-message, created, approval-pending, and terminal projection lanes

- `processor/agentic-dispatch/component.go:514` — `func (c *Component) consumeStreamHandle(ctx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {`
- `processor/agentic-dispatch/component.go:558` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:621` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:697` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:782` — `func (c *Component) handleUserMessage(ctx context.Context, data []byte) {`
- `processor/agentic-dispatch/component.go:820` — `func (c *Component) handleCommand(ctx context.Context, msg agentic.UserMessage) {`
- `processor/agentic-dispatch/component.go:958` — `func (c *Component) handleTaskSubmission(ctx context.Context, msg agentic.UserMessage) {`
- `processor/agentic-dispatch/component.go:1181` — `func (c *Component) sendResponse(ctx context.Context, resp agentic.UserResponse) {`
- `processor/agentic-dispatch/terminal_settlement.go:96` — `return nil, fmt.Errorf("AGENT_LOOPS client unavailable")`
- `processor/agentic-dispatch/terminal_settlement.go:146` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`
- `processor/agentic-dispatch/terminal_settlement.go:159` — `if err := c.sendTerminalResponseFn(ctx, response, msgID); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:180` — `reason := ""`
- `processor/agentic-dispatch/http.go:709` — `func (c *Component) handleLoopApproval(w http.ResponseWriter, r *http.Request) {`

### Governance validation lanes

- `processor/agentic-governance/component.go:349` — `func (c *Component) handleMessage(ctx context.Context, data []byte, msgType MessageType, outputPortName string) {`
- `processor/agentic-governance/component.go:504` — `if ackErr := msg.Ack(); ackErr != nil {`

### AgentRun boundary retained for #1249

- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
- `internal/agentterminal/terminal.go:123` — `event := Event{SourceMessageID: base.ID(), Category: base.Type().Category}`
- `agentic/agentrun/agentrun.go:467` — `type LoopTerminalEvent struct {`
- `agentic/agentrun/agentrun.go:485` — `// MilestoneHandler is the product-registered handler for terminal loop events.`
- `agentic/agentrun/agentrun.go:559` — `func (s *MilestoneSubscriber) AddHandler(h MilestoneHandler) {`
- `agentic/agentrun/agentrun.go:575` — `func (s *MilestoneSubscriber) HandleEvent(ctx context.Context, data []byte) error {`
- `agentic/agentrun/agentrun.go:580` — `ev := LoopTerminalEvent{`
- `agentic/agentrun/agentrun.go:590` — `run, err := s.resolveRunForEvent(ctx, ev)`
- `agentic/agentrun/agentrun.go:602` — `for i, h := range s.handlers {`
- `agentic/agentrun/agentrun.go:612` — `if handlerErr := handler.OnLoopTerminal(ctx, ev, run); handlerErr != nil {`
- `agentic/agentrun/agentrun.go:621` — `return nil`
- `agentic/agentrun/agentrun.go:637` — `return nil, nil //nolint:nilerr // deliberate: non-run loops have no run entity`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `agentic/agentrun/agentrun.go:829` — `FilterSubject: "agent.complete.*",`
- `agentic/agentrun/agentrun.go:832` — `MaxDeliver:    5,`
- `agentic/agentrun/agentrun.go:880` — `FilterSubject: "agent.failed.*",`
- `cmd/semstreams/main.go:340` — `// agent.failed.*, pre-resolves the run, and fans out to registered product`

## Spellings of the fact

### Stream, bucket, and port names

- `agentic/agentrun/agentrun.go:662` — `// AgentStreamName is the default JetStream stream name for agentic events.`
- `agentic/agentrun/agentrun.go:664` — `const AgentStreamName = "AGENT"`
- `processor/agentic-loop/config.go:54` — `LoopsBucket                       string                   `json:"loops_bucket" schema:"type:string,description:NATS KV bucket name for storing loop state,default:AGENT_LOOPS,category:advanced,required"``
- `processor/agentic-loop/config.go:380` — `LoopsBucket:                       "AGENT_LOOPS",`
- `processor/agentic-loop/config.go:433` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/component.go:1950` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2032` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `agentic/trajectory_fact.go:18` — `TrajectoryBucketName = "AGENT_TRAJECTORIES"`

### Payload and identity spellings

- `agentic/user_types.go:112` — `type UserSignal struct {`
- `agentic/user_types.go:141` — `if err := validateLoopTokenField("loop_id", s.LoopID); err != nil {`
- `agentic/user_types.go:434` — `// validateLoopTokenField is the ONE home of the loop-token form refusal for`
- `agentic/approval.go:137` — `if err := validateLoopTokenField("loop_id", r.LoopID); err != nil {`
- `agentic/state.go:142` — `// PendingApprovalState captures the gated tool call so the loop can`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `processor/agentic-loop/state.go:177` — `// GenerateLoopID returns an identity with the exact UUID semantics used by`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:1136` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1141` — `// GenerateToolCallID creates a structured tool call ID that embeds the loop ID.`
- `processor/agentic-loop/state.go:1169` — `// GetLoopForRequestWithRecovery retrieves the loop ID for a request ID,`
- `processor/agentic-loop/state.go:1193` — `// GetLoopForToolCallWithRecovery retrieves the loop ID for a tool call ID,`

### Payload registry and Store material

- `agentic/payload_registry.go:12` — `// RegisterPayloads registers all agentic payload types with the`
- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `payloadbuiltins/register.go:44` — `track(message.RegisterPayloads(reg))`
- `storage/storage.go:51` — `type Store interface {`
- `storage/storage.go:94` — `type StreamableStore interface {`
- `storage/storeregistry/storeregistry.go:41` — `type Registry struct {`
- `storage/storeregistry/storeregistry.go:97` — `func (r *Registry) Store(instance string) (storage.Store, bool) {`
- `processor/agentic-loop/trajectory_recorder.go:119` — `// evidence resolution goes through StoreRegistry on every operation.`

### Durable and process-only state spellings

- `processor/agentic-loop/state.go:61` — `type LoopManager struct {`
- `processor/agentic-loop/state.go:62` — `loops                map[string]*agentic.LoopEntity`
- `processor/agentic-loop/state.go:63` — `contextManagers      map[string]*ContextManager          // loopID -> ContextManager`
- `processor/agentic-loop/state.go:72` — `requestToLoop        map[string]string                   // requestID -> loopID`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop       map[string]string                   // callID -> loopID`
- `processor/agentic-loop/context_manager.go:45` — `type ContextManager struct {`
- `processor/agentic-loop/context_manager.go:51` — `regions          map[RegionType][]contextMessage`
- `processor/agentic-loop/state.go:240` — `// attachContinuation binds a continuation task to the loop already registered`
- `processor/agentic-loop/state.go:497` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-dispatch/loop_admission.go:320` — `// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.`
- `processor/agentic-dispatch/loop_admission.go:346` — `return loopLookup{outcome: loopLookupUnreadable, cause: persistErr}`
- `processor/agentic-dispatch/loop_admission.go:407` — `// mergeLoopFacts reconciles two observations of one loop. The route fields go`

## Adjacent claims

- Issue #1146 is OPEN, labeled `critical`, and assigned to milestone `beta.163`.
- Draft PR #1159 is OPEN from `codex/gh1146-agentic-loop-restart` into `codex/gh759-semantic-settlement`.
- Issue #759 and draft PR #1156 own the staged typed settlement foundation and final default-branch integration.
- PR #1231 is MERGED at `78813ec7` and supplies the admission-gate/current loop shapes present in F.
- PR #1245 is MERGED at `6733814c`; issue #1238 remains OPEN with `status:blocked` after that narrowed e2e change.
- Issue #1244 is OPEN in milestone `beta.165`; its issue comments sequence its declared-exit/state arm after #1146's durability arm.
- Issue #1249 is OPEN and owns AgentRun complete/failed fanout settlement after post-#1146 staging.

### Active #1146 merge-first and AgentRun/#1148 statements present at HEAD

- `openspec/changes/agentic-loop-restart-safety/design.md:5` — `Owner-accepted target state after independent `DESIGN REVIEW PASS`. Implementation remains blocked by #759.`
- `openspec/changes/agentic-loop-restart-safety/design.md:17` — `- #1146 remains blocked by #759.`
- `openspec/changes/agentic-loop-restart-safety/design.md:19` — `- AgentRun is excluded until #1148 merges and its surface is reinventoried.`
- `openspec/changes/agentic-loop-restart-safety/design.md:22` — `- No production implementation begins until the approved #759 `DeliveryAttempt` addendum merges.`
- `openspec/changes/agentic-loop-restart-safety/design.md:408` — `1. #759 merges the accepted `DeliveryResult` settlement foundation for each touched lane.`
- `openspec/changes/agentic-loop-restart-safety/design.md:420` — `12. AgentRun remains absent until #1148 merges and a new inventory is accepted.`
- `openspec/changes/agentic-loop-restart-safety/design.md:424` — `- AgentRun before #1148 merge and reinventory.`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:30` — `- No production implementation begins until #759 supplies the accepted settlement foundation.`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:44` — `- Blocking prerequisite: #759.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:13` — `- [ ] 1.1 Hold implementation until #759 merges.`

### Greenfield amendment bindings present at F

- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:385` — `AgentRun complete/failed settlement is transferred to #1249 and is not #1146 implementation scope.`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:405` — `- AgentRun is transferred to #1249; no AgentRun production or spec work lands in #1146.`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:439` — `- [ ] 1.4 Reconcile the full accepted design against `F` and post-#1231/#1245 surfaces; stop for reinventory and`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:447` — `AgentRun tasks H.1/H.2 are removed from #1146. #1249 owns its post-#1146 inventory, design, complete/failed migration,`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:496` — `- [ ] 7.2 Shrink the branch-staging zero-growth guard after #1146 and #1249; never describe it as an API allowlist.`

## Consumers

### Per-subscription matrix at F

Lifecycle keys used in every matrix row:

- `processor/agentic-dispatch/component.go:514` — `func (c *Component) consumeStreamHandle(ctx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {`
- `processor/agentic-dispatch/component.go:566` — `c.consumers = append(c.consumers, newStreamConsumerBinding(handle))`
- `processor/agentic-dispatch/component.go:605` — `c.consumers = append(c.consumers, agentCompleteBinding)`
- `processor/agentic-dispatch/component.go:480` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-dispatch/component.go:484` — `binding.drain()`
- `processor/agentic-dispatch/component.go:485` — `stopErr = errors.Join(stopErr, c.awaitConsumerClosed(ctx, binding.handle.Closed()))`
- `processor/agentic-dispatch/component.go:492` — `if done := c.consumers[i].observerDone; done != nil {`
- `processor/agentic-governance/component.go:502` — `handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-governance/component.go:512` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-governance/component.go:615` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-governance/component.go:620` — `binding.handle.Drain()`
- `processor/agentic-governance/component.go:623` — `closed := binding.handle.Closed()`
- `processor/agentic-loop/component.go:1048` — `handle, err := consume(setupCtx, consumerCtx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, handlerFn)`
- `processor/agentic-loop/component.go:1055` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-loop/component.go:692` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-loop/component.go:727` — `binding.handle.Drain()`
- `processor/agentic-loop/component.go:730` — `closed := binding.handle.Closed()`
- `processor/agentic-model/component.go:398` — `handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-model/component.go:413` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-model/component.go:523` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-model/component.go:528` — `binding.handle.Drain()`
- `processor/agentic-model/component.go:531` — `closed := binding.handle.Closed()`
- `processor/agentic-tools/component.go:445` — `handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: setup.port.Name, ComponentOwned: true}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-tools/component.go:459` — `c.consumers = append(c.consumers, binding)`
- `processor/agentic-tools/component.go:616` — `func (c *Component) cleanup(ctx context.Context) error {`
- `processor/agentic-tools/component.go:638` — `binding.drain()`
- `processor/agentic-tools/component.go:639` — `closed := binding.handle.Closed()`
- `processor/agentic-tools/component.go:654` — `if done := c.consumers[i].observerDone; done != nil {`
- `agentic/agentrun/agentrun.go:680` — `mu              sync.Mutex`
- `agentic/agentrun/agentrun.go:718` — `// Both running handles begin Drain before either exact Closed wait.`
- `agentic/agentrun/agentrun.go:720` — `complete.Drain()`
- `agentic/agentrun/agentrun.go:723` — `failed.Drain()`
- `agentic/agentrun/agentrun.go:727` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))`
- `agentic/agentrun/agentrun.go:730` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, failed.Closed(), "failed"))`

| # | Physical subscription: port / subject | Authority and handler | Native handle owner / close sequence | Durable and process correlation / replay horizon | Scope |
|---:|---|---|---|---|---|
| 1 | dispatch `user.message` / `user.message.>` (`config.go:118`) | raw native callback; `handleUserMessage` then unconditional `Ack` (`component.go:556-561`) | dispatch lifecycle key; append `:566`; Drain → exact Closed; no observer | durable USER delivery + payload MessageID; command/task response and LoopTracker facts are process-local; downstream PubAck is not tied to source settlement | #1146 |
| 2 | dispatch `agent.created` / `agent.created.*` (`config.go:126`) | raw; `handleAgentCreated` then unconditional `Ack` (`component.go:619-624`) | dispatch lifecycle key; append `:629`; Drain → exact Closed; no observer | AGENT delivery + LoopID; projection goes only to process `LoopTracker`; authoritative loop record is separately in AGENT_LOOPS | #1146 |
| 3 | dispatch `agent.approval_pending` / `agent.approval_pending.*` (`config.go:134`) | raw; `handleAgentApprovalPending` then unconditional `Ack` (`component.go:695-700`) | dispatch lifecycle key; append `:705`; Drain → exact Closed; no observer | AGENT delivery + LoopID/CallID; PendingApproval is durable in AGENT_LOOPS but this HTTP correlation is process `LoopTracker`/early buffer and has no startup hydration | #1146 |
| 4 | governance `task_validation` / `agent.task.*` (`config.go:189`) | raw; `handleMessage(Task)` then unconditional `Ack` (`component.go:316-318,502-507`) | governance lifecycle key; append `:512`; Drain → exact Closed; no observer | AGENT delivery + decoded Message.ID; violations and validated core-NATS publication have no source-linked durable result | #1146 |
| 5 | governance `request_validation` / `agent.request.*` (`config.go:193`) | raw; `handleMessage(Request)` then unconditional `Ack` (`component.go:319-321,502-507`) | governance lifecycle key; append `:512`; Drain → exact Closed; no observer | AGENT delivery + decoded Message.ID; filter result is process-only and output uses core NATS | #1146 |
| 6 | governance `response_validation` / `agent.response.*` (`config.go:197`) | raw; `handleMessage(Response)` then unconditional `Ack` (`component.go:322-324,502-507`) | governance lifecycle key; append `:512`; Drain → exact Closed; no observer | AGENT delivery + decoded Message.ID; filter result is process-only and output uses core NATS | #1146 |
| 7 | loop `agent.signal` / `agent.signal.*` (`config.go:408`) | raw fast; void adapter → `handleSignalMessage`; nil means `Ack`, error means `Nak` (`component.go:898,1032-1041`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | AGENT delivery + LoopID; business lookup is process `m.loops`; cancellation writes AGENT_LOOPS/COMPLETE and publishes terminal event through void paths | #1146; pause/resume collision #1239 |
| 8 | loop `agent.approval_response` / `agent.approval_response.*` (`config.go:412`) | raw fast; void adapter → `handleApprovalResponseMessage`; outer nil → `Ack` (`component.go:900,1032-1041`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | AGENT delivery + LoopID/CallID; PendingApproval fields persist but resolution requires process `m.loops`; no intake hydration | #1146 |
| 9 | loop `agent.toolcall.approved` / `agent.toolcall.approved.>` (`config.go:416`) | raw fast; void adapter → `handleToolCallVerdictMessage`; outer nil → `Ack` (`component.go:909,1032-1041`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | AGENT delivery + CallID; correlation authority is process-only `waiters`; missing/late waiter drops | #1146 |
| 10 | loop `agent.toolcall.rejected` / `agent.toolcall.rejected.>` (`config.go:420`) | raw fast; same verdict handler and outer settlement as row 9 | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | same as row 9 | #1146 |
| 11 | tools `tool.execute` / `tool.execute.>` (`config.go:127`) | typed heartbeat policy → `handleToolDelivery` (`component.go:424-429,445-450`) | tools lifecycle key; append `:459`; typed owner-stop observer may drain; Stop drains, awaits exact Closed, cancels, joins observer | CallID + immutable owner-private TOOL_CALL_OUTCOMES completed record; completed replay republishes deterministic ToolResult; no claimed/in-progress record | #1146 existing typed authority |
| 12 | dispatch `agent.complete` / `agent.complete.*` (`config.go:122`) | typed heartbeat policy → `handleTerminalDelivery` (`component.go:580-585,590-598`) | dispatch lifecycle key; append `:605`; owner-stop observer may drain; Stop drains, awaits exact Closed, cancels, joins observer | SourceMessageID + exact AGENT_LOOPS reread + deterministic user-response MsgID; unknown publication result quarantines/stops lane | #1146 current typed boundary |
| 13 | dispatch `agent.failed` / `agent.failed.*` (`config.go:130`) | typed heartbeat policy → `handleTerminalDelivery` (`component.go:643-648,653-661`) | dispatch lifecycle key; append `:668`; same observer/close sequence as row 12 | same durable terminal ancestry and publication contract as row 12 | #1146 current typed boundary |
| 14 | model `agent.request` / `agent.request.>` (`config.go:128`) | legacy heartbeat; callback calls void `handleRequest` then returns nil (`component.go:398-407`) | model lifecycle key; append `:413`; Drain → exact Closed; no observer | AGENT request + RequestID; endpoint/provider selection and client cache are process state; response has no durable invocation result authority or deterministic MsgID | #1146 |
| 15 | loop `agent.task` / `agent.task.*` (`config.go:396`) | legacy heartbeat; explicitly long-running, serial, 30m task adapter (`component.go:963-988,1090-1098,1143-1157`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | LoopID/TaskID payload + current AGENT_LOOPS record; intake creates/attaches in `m.loops`, never hydrates from AGENT_LOOPS; transient lineage result is process-only | #1146 |
| 16 | loop `agent.response` / `agent.response.>` (`config.go:400`) | legacy heartbeat; long-running void adapter → `handleResponseMessage` (`component.go:894,989-996,1090-1098`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | RequestID embeds LoopID, but recovery succeeds only if `m.loops[loopID]` exists; no durable model outcome | #1146 |
| 17 | loop `tool.result` / `tool.result.>` (`config.go:404`) | legacy heartbeat; long-running void adapter → `handleToolResultMessage` (`component.go:896,989-996,1090-1098`) | loop lifecycle key; append `:1055`; Drain → exact Closed; no observer | CallID embeds LoopID, but recovery succeeds only if `m.loops[loopID]` exists; tools' completed outcome is not loop-side correlation authority | #1146 |
| 18 | AgentRun internal `agent.complete.*` (`agentrun.go:829`) | legacy heartbeat → `HandleEvent`; handler error becomes Term, logged handler errors/panics become nil (`agentrun.go:810-823`) | milestone owner `complete`; both handles Drain before exact Closed; then cancel | stable durable offset, LoopID/RunID; normalized SourceMessageID is discarded from LoopTerminalEvent; handler slice and partial fanout receipts are process-only | transferred #1249 |
| 19 | AgentRun internal `agent.failed.*` (`agentrun.go:880`) | same legacy heartbeat/HandleEvent path as row 18 | milestone owner `failed`; both handles Drain before exact Closed; then cancel | same as row 18 | transferred #1249 |

### Exact fast no-heartbeat durable-input lanes at F

- `processor/agentic-dispatch/component.go:558` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:621` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-dispatch/component.go:697` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-governance/component.go:349` — `func (c *Component) handleMessage(ctx context.Context, data []byte, msgType MessageType, outputPortName string) {`
- `processor/agentic-governance/component.go:504` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-loop/component.go:897` — `case "agent.signal":`
- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:900` — `handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)`
- `processor/agentic-loop/component.go:909` — `handler = adaptVoidInputHandler(c.handleToolCallVerdictMessage)`
- `processor/agentic-loop/component.go:1038` — `if ackErr := msg.Ack(); ackErr != nil {`

The exact count is ten subscriptions: dispatch `user.message`, `agent.created`, and `agent.approval_pending`; governance
task, request, and response validation; loop `agent.signal`, `agent.approval_response`, `agent.toolcall.approved`, and
`agent.toolcall.rejected`. Their binding callbacks retain native-message settlement authority and pass only context and
read-only bytes to business handlers. The dispatch and governance work is decode/process-cache/filter/publication work;
loop signal and approval work can include KV transition/publication, while verdict work completes an in-process waiter.

### Long-running or heartbeat-classified lanes at F

- `processor/agentic-loop/component.go:989` — `case "agent.response", "tool.result":`
- `processor/agentic-loop/component.go:1027` — `if err := consumeLongRunningInput(msgCtx, msg, hi, handler); err != nil {`
- `processor/agentic-model/component.go:399` — `if hbErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, heartbeatInterval,`
- `processor/agentic-tools/component.go:393` — `// HeartbeatInterval new in this PR — without it, a tool taking longer`
- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`

### Raw message authority and lifecycle closure

- `processor/agentic-loop/component.go:1044` — `consume := c.natsClient.ConsumeStreamWithConfigContexts`
- `processor/agentic-loop/component.go:1055` — `c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})`
- `processor/agentic-tools/delivery_owner.go:12` — `// delivery authority; this latch only prevents new local work after ownership`
- `processor/agentic-tools/delivery_owner.go:77` — `) {`
- `processor/agentic-dispatch/delivery_owner.go:13` — `mu      sync.Mutex`
- `processor/agentic-dispatch/delivery_owner.go:75` — `) {`
- `processor/agentic-loop/component.go:484` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-model/component.go:263` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-tools/component.go:218` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-dispatch/component.go:372` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-governance/component.go:257` — `runCtx, cancel := context.WithCancel(ctx)`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-loop/component.go:1751` — `bctx, cancel := context.WithTimeout(ctx, budget)`
- `processor/agentic-loop/trajectory_handler_wiring.go:161` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `processor/agentic-model/client.go:477` — `if c.logger == nil || !c.logger.Enabled(context.Background(), slog.LevelDebug) {`
- `processor/agentic-tools/recording.go:63` — `ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)`
- `processor/agentic-tools/executors/httprequest.go:217` — `emitCtx, emitCancel := context.WithTimeout(context.WithoutCancel(ctx), webEmitTimeout)`
- `processor/agentic-tools/executors/websearch.go:213` — `emitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), webEmitTimeout)`

## Problem shape

### Callback-to-outer-settlement walk

#### Loop task creation, graph birth, lineage, publication, and persistence

- `processor/agentic-loop/component.go:1162` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/component.go:1165` — `return nil`
- `processor/agentic-loop/component.go:1171` — `return nil`
- `processor/agentic-loop/component.go:1178` — `return natsclient.TerminateDelivery(err)`
- `processor/agentic-loop/component.go:1187` — `result, err := c.handler.HandleTask(ctx, *task)`
- `processor/agentic-loop/component.go:1198` — `return nil`
- `processor/agentic-loop/component.go:1201` — `return nil`
- `processor/agentic-loop/component.go:1210` — `return nil`
- `processor/agentic-loop/component.go:1239` — `if err := c.graphWriter.WriteSpawnIdentity(ctx, result.LoopID, task); err != nil {`
- `processor/agentic-loop/component.go:1243` — `return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)`
- `processor/agentic-loop/component.go:1246` — `if err := c.writeLineageTriples(ctx, result.LoopID, related); err != nil {`
- `processor/agentic-loop/component.go:1248` — `c.rememberPendingTaskResult(task.TaskID, result)`
- `processor/agentic-loop/component.go:1251` — `return err`
- `processor/agentic-loop/component.go:1257` — `return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)`
- `processor/agentic-loop/component.go:1271` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1274` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1275` — `return nil`
- `processor/agentic-loop/component.go:1289` — `c.pendingTaskResults = make(map[string]HandlerResult)`
- `processor/agentic-loop/component.go:1291` — `c.pendingTaskResults[taskID] = result`
- `processor/agentic-loop/component.go:1388` — `c.handleLoopFailure(ctx, loopID, entity, reason, err)`
- `processor/agentic-loop/component.go:1389` — `return nil`

#### Loop response, tool-result, common persistence, and log-only effects

- `processor/agentic-loop/component.go:1394` — `response, loopID, ok := c.extractAgentResponse(data)`
- `processor/agentic-loop/component.go:1396` — `return`
- `processor/agentic-loop/component.go:1401` — `result, err := c.handler.HandleModelResponse(ctx, loopID, *response)`
- `processor/agentic-loop/component.go:1404` — `c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)`
- `processor/agentic-loop/component.go:1405` — `return`
- `processor/agentic-loop/component.go:1409` — `c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:1431` — `return nil, "", false`
- `processor/agentic-loop/component.go:1437` — `return nil, "", false`
- `processor/agentic-loop/component.go:1452` — `return nil, "", false`
- `processor/agentic-loop/component.go:1510` — `failure, failMsgs, err := c.handler.BuildFailureMessages(loopID, reason, errorMsg)`
- `processor/agentic-loop/component.go:1520` — `c.persistFailureState(errorCtx, loopID, failure)`
- `processor/agentic-loop/component.go:1528` — `c.stampLoopFailureWithBudget(errorCtx, loopID, failure)`
- `processor/agentic-loop/component.go:1538` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1539` — `c.logger.Error("Failed to publish failure event", "error", pubErr, "loop_id", loopID)`
- `processor/agentic-loop/component.go:1636` — `c.recordHandlerResultTrajectory(ctx, result)`
- `processor/agentic-loop/component.go:1637` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1641` — `c.persistCompletionState(ctx, result.LoopID, result.CompletionState)`
- `processor/agentic-loop/component.go:1642` — `c.stampLoopCompletionWithBudget(ctx, result.LoopID, result.CompletionState)`
- `processor/agentic-loop/component.go:1644` — `c.stampLoopFailureWithBudget(ctx, result.LoopID, result.FailureState)`
- `processor/agentic-loop/component.go:1656` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1784` — `return`
- `processor/agentic-loop/component.go:1790` — `return`
- `processor/agentic-loop/component.go:1812` — `return`
- `processor/agentic-loop/component.go:1832` — `result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)`
- `processor/agentic-loop/component.go:1842` — `return`
- `processor/agentic-loop/component.go:1865` — `c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:1880` — `c.logger.Error("Failed to publish message", "error", err, "subject", msg.Subject)`
- `processor/agentic-loop/component.go:1900` — `c.logger.Error("Failed to marshal context event", "error", err, "type", event.Type)`
- `processor/agentic-loop/component.go:1906` — `c.logger.Error("Failed to resolve context event subject", "error", err)`
- `processor/agentic-loop/component.go:1910` — `c.logger.Error("Failed to publish context event", "error", err, "subject", subject)`
- `processor/agentic-loop/component.go:1952` — `c.logger.Error("Failed to persist completion state", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:1979` — `c.logger.Error("Failed to persist failure state", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:2004` — `c.logger.Error("Failed to persist cancellation state", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:2033` — `c.logger.Error("Failed to persist loop state", "error", err, "loop_id", loopID)`

#### Governance raw callback exits before unconditional Ack

- `processor/agentic-governance/component.go:352` — `if err := json.Unmarshal(data, &msg); err != nil {`
- `processor/agentic-governance/component.go:355` — `return`
- `processor/agentic-governance/component.go:364` — `result, err := c.chain.Process(ctx, &msg)`
- `processor/agentic-governance/component.go:372` — `return`
- `processor/agentic-governance/component.go:391` — `if err := c.violations.Handle(ctx, violation); err != nil {`
- `processor/agentic-governance/component.go:401` — `if !result.Allowed {`
- `processor/agentic-governance/component.go:408` — `return`
- `processor/agentic-governance/component.go:422` — `outputSubject, resolveErr := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)`
- `processor/agentic-governance/component.go:426` — `return`
- `processor/agentic-governance/component.go:429` — `outputData, err := json.Marshal(outputMsg)`
- `processor/agentic-governance/component.go:433` — `return`
- `processor/agentic-governance/component.go:436` — `if err := c.natsClient.Publish(ctx, outputSubject, outputData); err != nil {`
- `processor/agentic-governance/component.go:502` — `handle, err := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-governance/component.go:503` — `handler(msgCtx, msg.Data())`
- `processor/agentic-governance/component.go:504` — `if ackErr := msg.Ack(); ackErr != nil {`

#### Dispatch raw callbacks and publication exits

- `processor/agentic-dispatch/component.go:785` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-dispatch/component.go:788` — `return`
- `processor/agentic-dispatch/component.go:794` — `return`
- `processor/agentic-dispatch/component.go:808` — `c.handleCommand(ctx, msg)`
- `processor/agentic-dispatch/component.go:811` — `c.handleTaskSubmission(ctx, msg)`
- `processor/agentic-dispatch/component.go:823` — `c.sendResponse(ctx, agentic.UserResponse{`
- `processor/agentic-dispatch/component.go:832` — `return`
- `processor/agentic-dispatch/component.go:846` — `return`
- `processor/agentic-dispatch/component.go:868` — `return`
- `processor/agentic-dispatch/component.go:883` — `return`
- `processor/agentic-dispatch/component.go:886` — `c.sendResponse(ctx, resp)`
- `processor/agentic-dispatch/component.go:970` — `return`
- `processor/agentic-dispatch/component.go:998` — `c.answerRefusedSubmission(ctx, msg, err)`
- `processor/agentic-dispatch/component.go:999` — `return`
- `processor/agentic-dispatch/component.go:1018` — `return`
- `processor/agentic-dispatch/component.go:1025` — `return`
- `processor/agentic-dispatch/component.go:1033` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/component.go:1047` — `if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {`
- `processor/agentic-dispatch/component.go:1050` — `return`
- `processor/agentic-dispatch/component.go:1057` — `c.sendResponse(ctx, agentic.UserResponse{`
- `processor/agentic-dispatch/component.go:1088` — `return`
- `processor/agentic-dispatch/component.go:1095` — `return`
- `processor/agentic-dispatch/component.go:1105` — `return`
- `processor/agentic-dispatch/component.go:1149` — `return`
- `processor/agentic-dispatch/component.go:1156` — `return`
- `processor/agentic-dispatch/component.go:1163` — `return`
- `processor/agentic-dispatch/component.go:1170` — `c.loopTracker.SetPendingApproval(pending.LoopID, &PendingApprovalInfo{`
- `processor/agentic-dispatch/component.go:1189` — `c.logger.Error("Failed to marshal response", slog.String("error", err.Error()))`
- `processor/agentic-dispatch/component.go:1195` — `c.logger.Error("Failed to resolve response subject", slog.String("error", err.Error()))`
- `processor/agentic-dispatch/component.go:1198` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/component.go:1199` — `c.logger.Error("Failed to publish response", slog.String("error", err.Error()))`

#### Signal, approval, and verdict exits before raw outer Ack

- `processor/agentic-loop/component.go:2097` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/component.go:2100` — `return`
- `processor/agentic-loop/component.go:2106` — `return`
- `processor/agentic-loop/component.go:2144` — `entity, err := c.handler.CancelLoop(loopID, signal.UserID)`
- `processor/agentic-loop/component.go:2149` — `return`
- `processor/agentic-loop/component.go:2157` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:2183` — `completionData, err := json.Marshal(completionMsg)`
- `processor/agentic-loop/component.go:2188` — `return`
- `processor/agentic-loop/component.go:2191` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)`
- `processor/agentic-loop/component.go:2194` — `return`
- `processor/agentic-loop/component.go:2196` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`
- `processor/agentic-loop/component.go:2200` — `return`
- `processor/agentic-loop/component.go:2211` — `c.persistCancellationState(ctx, loopID, &completion)`

##### Row 7 unsupported, pause, and resume branch census

- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:1032` — `handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-loop/component.go:1038` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-loop/component.go:177` — `func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {`
- `processor/agentic-loop/component.go:179` — `handler(ctx, data)`
- `processor/agentic-loop/component.go:180` — `return nil`
- `processor/agentic-loop/component.go:2124` — `default:`
- `processor/agentic-loop/component.go:2125` — `c.logger.Warn("Unsupported signal type",`
- `processor/agentic-loop/component.go:2128` — `}`
- `processor/agentic-loop/component.go:2218` — `func (c *Component) handlePauseSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-loop/component.go:2222` — `entity, err := c.handler.GetLoop(loopID)`
- `processor/agentic-loop/component.go:2227` — `return`
- `processor/agentic-loop/component.go:2231` — `if entity.State.IsTerminal() || entity.State == agentic.LoopStatePaused {`
- `processor/agentic-loop/component.go:2235` — `return`
- `processor/agentic-loop/component.go:2242` — `if err := c.handler.UpdateLoop(entity); err != nil {`
- `processor/agentic-loop/component.go:2246` — `return`
- `processor/agentic-loop/component.go:2250` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:2252` — `c.logger.Info("Pause requested for loop",`
- `processor/agentic-loop/component.go:2255` — `}`
- `processor/agentic-loop/component.go:2258` — `func (c *Component) handleResumeSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-loop/component.go:2262` — `entity, err := c.handler.GetLoop(loopID)`
- `processor/agentic-loop/component.go:2267` — `return`
- `processor/agentic-loop/component.go:2271` — `if entity.State != agentic.LoopStatePaused {`
- `processor/agentic-loop/component.go:2275` — `return`
- `processor/agentic-loop/component.go:2283` — `if err := c.handler.UpdateLoop(entity); err != nil {`
- `processor/agentic-loop/component.go:2287` — `return`
- `processor/agentic-loop/component.go:2291` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:2293` — `c.logger.Info("Loop resumed",`
- `processor/agentic-loop/component.go:2296` — `}`

| Row-7 branch | Business-handler effect before return | Adapter result | Native settlement | Recorded collision |
|---|---|---|---|---|
| unsupported signal (`:2124-2128`) | warning only, then implicit function return | `adaptVoidInputHandler` returns nil (`:177-180`) | raw fast callback calls `Ack` (`:1032-1038`) | row 7 remains in #1146; no pause/resume mutation |
| pause lookup failure (`:2222-2227`) | process `m.loops` lookup fails; explicit return | nil | `Ack` | #1239 deletion surface |
| pause terminal/already-paused (`:2231-2235`) | refusal warning; explicit return | nil | `Ack` | #1239 deletion surface |
| pause `UpdateLoop` failure (`:2242-2246`) | process update fails; explicit return | nil | `Ack` | #1239 deletion surface |
| pause persistence path (`:2250-2255`) | `persistLoopState` is void; any KV error is logged inside it; implicit return after info log | nil | `Ack` | #1239 deletion surface |
| resume lookup failure (`:2262-2267`) | process `m.loops` lookup fails; explicit return | nil | `Ack` | #1239 deletion surface |
| resume non-paused refusal (`:2271-2275`) | refusal warning; explicit return | nil | `Ack` | #1239 deletion surface |
| resume `UpdateLoop` failure (`:2283-2287`) | process update fails; explicit return | nil | `Ack` | #1239 deletion surface |
| resume persistence path (`:2291-2296`) | `persistLoopState` is void; any KV error is logged inside it; implicit return after info log | nil | `Ack` | #1239 deletion surface |

- `processor/agentic-loop/approval_response_handler.go:41` — `if r := recover(); r != nil {`
- `processor/agentic-loop/approval_response_handler.go:48` — `err = nil`
- `processor/agentic-loop/approval_response_handler.go:53` — `return HandlerResult{}, errs.WrapInvalid(vErr, "agentic-loop", "HandleApprovalResponse", "validate response")`
- `processor/agentic-loop/approval_response_handler.go:58` — `pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID)`
- `processor/agentic-loop/approval_response_handler.go:81` — `return HandlerResult{LoopID: loopID, State: state, staleDrop: true}, nil`
- `processor/agentic-loop/approval_response_handler.go:164` — `c.logger.Error("Failed to decode approval response", "error", err)`
- `processor/agentic-loop/approval_response_handler.go:165` — `return`
- `processor/agentic-loop/approval_response_handler.go:171` — `return`
- `processor/agentic-loop/approval_response_handler.go:187` — `return`
- `processor/agentic-loop/approval_response_handler.go:194` — `return`
- `processor/agentic-loop/approval_response_handler.go:200` — `c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:2324` — `return`
- `processor/agentic-loop/component.go:2327` — `payload, ok := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/component.go:2331` — `return`
- `processor/agentic-loop/component.go:2340` — `return`
- `processor/agentic-loop/component.go:2343` — `dispatcher.HandleVerdict(decision, callID, data)`
- `processor/agentic-loop/governance_dispatcher.go:341` — `waiters map[string]chan verdictArrival`
- `processor/agentic-loop/governance_dispatcher.go:483` — `ch, ok := d.lookupWaiter(callID)`
- `processor/agentic-loop/governance_dispatcher.go:494` — `return`
- `processor/agentic-loop/governance_dispatcher.go:504` — `d.logger.Debug("Verdict channel already full (duplicate verdict?); dropping",`

#### Model, typed terminal, tools, KV, and Store effect boundaries

- `processor/agentic-model/component.go:401` — `c.handleRequest(workCtx, msg.Data())`
- `processor/agentic-model/component.go:402` — `return nil`
- `processor/agentic-model/component.go:590` — `c.logger.Error("Failed to parse agent request", "error", err)`
- `processor/agentic-model/component.go:592` — `return`
- `processor/agentic-model/component.go:602` — `c.logger.Error("Failed to resolve endpoint", "error", err, "model", req.Model)`
- `processor/agentic-model/component.go:605` — `return`
- `processor/agentic-model/component.go:777` — `c.logger.Error("Failed to publish response", "error", err)`
- `processor/agentic-model/component.go:779` — `return`
- `processor/agentic-model/component.go:1049` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:180` — `reason := ""`
- `processor/agentic-tools/component.go:510` — `func (c *Component) handleToolDelivery(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-tools/component.go:513` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-tools/component.go:518` — `return natsclient.DeliveryDecisionTerminate, err`
- `processor/agentic-tools/component.go:520` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-tools/component.go:522` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-tools/component.go:860` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ID), data)`
- `processor/agentic-loop/trajectory_recorder.go:118` — `// trajectoryRecorder owns immutable fact creation. It holds no storage handle:`
- `processor/agentic-loop/trajectory_recorder.go:119` — `// evidence resolution goes through StoreRegistry on every operation.`
- `processor/agentic-loop/trajectory_recorder.go:217` — `_, createErr := r.bucket.Create(ctx, key, encoded)`
- `processor/agentic-loop/trajectory_recorder.go:239` — `r.fail(ctx, observation, attempt.ID, stage, trajectoryReasonBackend,`

### Shipped AGENT retention and runtime admission observation

- `configs/agentic.json:15` — `"AGENT": {`
- `configs/agentic.json:17` — `"agent.>"`
- `configs/agentic.json:19` — `"max_age": "24h",`
- `configs/agentic.json:20` — `"max_bytes": 268435456,`
- `configs/agentic.json:21` — `"discard": "old"`
- `README.md:215` — `./bin/semstreams --config configs/agentic.json`

The scoped production search for `StreamInfo(` returned zero results. Its sole scoped match was
`processor/agentic-tools/outcomes_integration_test.go:108`, for the TOOL_CALL_OUTCOMES KV backing stream in a test.

### Typed primitive metadata and terminal-method gate

- `natsclient/delivery_settlement.go:309` — `metadata, err := msg.Metadata()`
- `natsclient/delivery_settlement.go:311` — `return unavailableDeliveryMetadata(err)`
- `natsclient/delivery_settlement.go:313` — `if metadata == nil {`
- `natsclient/delivery_settlement.go:316` — `if metadata.NumDelivered == 0 {`
- `natsclient/delivery_settlement.go:319` — `attempt := DeliveryAttempt{number: metadata.NumDelivered}`
- `natsclient/delivery_settlement.go:348` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:357` — `return settleDeliveryDecision(msg, policy.retry, interpretDeliveryWork(joined))`
- `natsclient/delivery_settlement.go:399` — `func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {`
- `natsclient/delivery_settlement.go:410` — `method = terminalMethodNak`
- `natsclient/delivery_settlement.go:412` — `method = terminalMethodNakWithDelay`
- `natsclient/delivery_settlement.go:416` — `method = terminalMethodTerm`
- `natsclient/delivery_settlement.go:437` — `return msg.Ack()`
- `natsclient/delivery_settlement.go:439` — `return msg.Nak()`
- `natsclient/delivery_settlement.go:441` — `return msg.NakWithDelay(delay)`
- `natsclient/delivery_settlement.go:443` — `return msg.Term()`

### Payload registry authority by lane

- `agentic/payload_registry.go:34` — `{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:35` — `{Domain: Domain, Category: CategoryUserMessage, Version: SchemaVersion, Description: "User message from any channel", Factory: func() any { return &UserMessage{} }, IndexingProfile: content},`
- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `agentic/payload_registry.go:37` — `{Domain: Domain, Category: CategoryUserResponse, Version: SchemaVersion, Description: "User response to channel", Factory: func() any { return &UserResponse{} }, IndexingProfile: content},`
- `agentic/payload_registry.go:38` — `{Domain: Domain, Category: CategoryResponse, Version: SchemaVersion, Description: "Agent model response", Factory: func() any { return &AgentResponse{} }, IndexingProfile: trace},`
- `agentic/payload_registry.go:39` — `{Domain: Domain, Category: CategoryToolResult, Version: SchemaVersion, Description: "Tool execution result", Factory: func() any { return &ToolResult{} }, IndexingProfile: trace},`
- `agentic/payload_registry.go:40` — `{Domain: Domain, Category: CategoryRequest, Version: SchemaVersion, Description: "Agent model request", Factory: func() any { return &AgentRequest{} }, IndexingProfile: trace},`
- `agentic/payload_registry.go:41` — `{Domain: Domain, Category: CategoryToolCall, Version: SchemaVersion, Description: "Tool call request", Factory: func() any { return &ToolCall{} }, IndexingProfile: trace},`
- `agentic/payload_registry.go:42` — `{Domain: Domain, Category: CategoryLoopCreated, Version: SchemaVersion, Description: "Loop creation event", Factory: func() any { return &LoopCreatedEvent{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:43` — `{Domain: Domain, Category: CategoryLoopCompleted, Version: SchemaVersion, Description: "Loop completion event", Factory: func() any { return &LoopCompletedEvent{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:44` — `{Domain: Domain, Category: CategoryLoopFailed, Version: SchemaVersion, Description: "Loop failure event", Factory: func() any { return &LoopFailedEvent{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:45` — `{Domain: Domain, Category: CategoryLoopCancelled, Version: SchemaVersion, Description: "Loop cancellation event", Factory: func() any { return &LoopCancelledEvent{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:47` — `{Domain: Domain, Category: CategoryApprovalPending, Version: SchemaVersion, Description: "Approval-pending event for human-in-the-loop tool gating", Factory: func() any { return &ApprovalPendingEvent{} }, IndexingProfile: control},`
- `agentic/payload_registry.go:48` — `{Domain: Domain, Category: CategoryApprovalResponse, Version: SchemaVersion, Description: "Approval response from human-in-the-loop UI", Factory: func() any { return &ApprovalResponse{} }, IndexingProfile: control},`
- `payloadbuiltins/register.go:36` — `func Register(reg *payloadregistry.Registry) error {`

`ApprovalContinuationV1` has zero production declarations, payload-registry rows, reads, writes, or Store use outside
the two active OpenSpec changes. Task/UserMessage/AgentRequest/AgentResponse/ToolCall/ToolResult, signal, approval, and
terminal rows in the matrix use the registrations above. Governance verdicts retain the documented raw-map fallback;
the proposed-call publisher wraps `core.json.v1`. AgentRun consumes registered terminal envelopes after normalization.

### Durable versus process correlation and replay horizon

- `processor/agentic-loop/state.go:62` — `loops                map[string]*agentic.LoopEntity`
- `processor/agentic-loop/state.go:66` — `cachedTools          map[string][]agentic.ToolDefinition // loopID -> tools (runtime cache, not persisted)`
- `processor/agentic-loop/state.go:69` — `cachedRequestTimeout map[string]string                   // loopID -> request timeout (from TaskMessage.Timeout, not persisted)`
- `processor/agentic-loop/state.go:71` — `taskPrompts          map[string]string                   // loopID -> original task prompt (for context recovery)`
- `processor/agentic-loop/state.go:72` — `requestToLoop        map[string]string                   // requestID -> loopID`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop       map[string]string                   // callID -> loopID`
- `processor/agentic-loop/state.go:1181` — `_, exists := m.loops[loopID]`
- `processor/agentic-loop/state.go:1185` — `m.TrackRequest(requestID, loopID)`
- `processor/agentic-loop/state.go:1205` — `_, exists := m.loops[loopID]`
- `processor/agentic-loop/state.go:1209` — `m.TrackToolCall(toolCallID, loopID)`
- `processor/agentic-loop/component.go:779` — `// Initialize loops bucket`
- `processor/agentic-loop/component.go:792` — `c.loopsBucket = loopsBucket`
- `agentic/state.go:53` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // Accumulated tool results by call ID`
- `agentic/state.go:78` — `PendingApproval     *PendingApprovalState `json:"pending_approval,omitempty"``
- `agentic/state.go:98` — `Metadata map[string]any `json:"metadata,omitempty"``
- `processor/agentic-tools/outcomes.go:21` — `// completedOutcome is the immutable COMPLETED record. There is deliberately`
- `processor/agentic-dispatch/terminal_settlement.go:146` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`
- `agentic/agentrun/agentrun.go:770` — `// Two stable durable consumers are created (one per filter subject). They survive`
- `agentic/agentrun/agentrun.go:771` — `// subscriber restarts and resume from the last-acked message.`

### #1239 same-surface collision

- `agentic/state.go:66` — `PauseRequested   bool      `json:"pause_requested,omitempty"`    // Pause requested, will pause at next checkpoint`
- `agentic/state.go:67` — `PauseRequestedBy string    `json:"pause_requested_by,omitempty"` // User who requested pause`
- `processor/agentic-loop/component.go:2120` — `case agentic.SignalPause:`
- `processor/agentic-loop/component.go:2122` — `case agentic.SignalResume:`
- `processor/agentic-loop/component.go:2239` — `entity.PauseRequested = true`
- `processor/agentic-loop/component.go:2280` — `entity.PauseRequested = false`

Issue #1239 is OPEN in `beta.163`. Its 2026-09-02 owner-ruling comment selects option 1: delete pause/resume handlers and
the two persisted fields in a separately claimed change; cancel remains. F still contains the declarations and handlers.

### Complete stale active OpenSpec transfer statements

- `openspec/changes/agentic-loop-restart-safety/proposal.md:10` — `This is the owner-classified critical beta.163 vertical in #1146. It is blocked by #759 because semantic JetStream`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:30` — `- No production implementation begins until #759 supplies the accepted settlement foundation.`
- `openspec/changes/agentic-loop-restart-safety/design.md:5` — `Owner-accepted target state after independent `DESIGN REVIEW PASS`. Implementation remains blocked by #759.`
- `openspec/changes/agentic-loop-restart-safety/design.md:17` — `- #1146 remains blocked by #759.`
- `openspec/changes/agentic-loop-restart-safety/design.md:19` — `- AgentRun is excluded until #1148 merges and its surface is reinventoried.`
- `openspec/changes/agentic-loop-restart-safety/design.md:22` — `- No production implementation begins until the approved #759 `DeliveryAttempt` addendum merges.`
- `openspec/changes/agentic-loop-restart-safety/design.md:408` — `1. #759 merges the accepted `DeliveryResult` settlement foundation for each touched lane.`
- `openspec/changes/agentic-loop-restart-safety/design.md:420` — `12. AgentRun remains absent until #1148 merges and a new inventory is accepted.`
- `openspec/changes/agentic-loop-restart-safety/design.md:424` — `- AgentRun before #1148 merge and reinventory.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:13` — `- [ ] 1.1 Hold implementation until #759 merges.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:16` — `- [ ] 1.4 Reconcile the design against merged #759; stop for reinventory if the surface differs materially.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:125` — `## Hold: AgentRun`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:127` — `- [ ] H.1 After #1148 merges, reinventory AgentRun against the accepted baseline.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:128` — `- [ ] H.2 Add AgentRun only through a separately reviewed and owner-accepted design delta.`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:385` — `AgentRun complete/failed settlement is transferred to #1249 and is not #1146 implementation scope.`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:447` — `AgentRun tasks H.1/H.2 are removed from #1146. #1249 owns its post-#1146 inventory, design, complete/failed migration,`

### Replay-admissibility and partial/cold state

- `processor/agentic-loop/state.go:62` — `loops                map[string]*agentic.LoopEntity`
- `processor/agentic-loop/state.go:63` — `contextManagers      map[string]*ContextManager          // loopID -> ContextManager`
- `processor/agentic-loop/state.go:72` — `requestToLoop        map[string]string                   // requestID -> loopID`
- `processor/agentic-loop/state.go:73` — `toolCallToLoop       map[string]string                   // callID -> loopID`
- `processor/agentic-loop/state.go:1169` — `// GetLoopForRequestWithRecovery retrieves the loop ID for a request ID,`
- `processor/agentic-loop/state.go:1193` — `// GetLoopForToolCallWithRecovery retrieves the loop ID for a tool call ID,`
- `processor/agentic-loop/component.go:1448` — `c.logger.Warn("No loop found for request", "request_id", responsePtr.RequestID)`
- `processor/agentic-loop/component.go:1808` — `c.logger.Warn("No loop found for tool call", "call_id", toolResult.CallID)`
- `processor/agentic-loop/approval_sweeper.go:40` — `// Restart-safe: PendingApproval is KV-persisted with RequestedAt and`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-dispatch/loop_admission.go:320` — `// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.`
- `processor/agentic-dispatch/loop_admission.go:346` — `return loopLookup{outcome: loopLookupUnreadable, cause: persistErr}`

### Publication acknowledgment, reconciliation, and source identity

- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-model/component.go:1049` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-tools/outcomes.go:83` — `func toolResultMessageID(callID string) string {`
- `processor/agentic-dispatch/terminal_settlement.go:146` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`
- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
- `internal/agentterminal/terminal.go:123` — `event := Event{SourceMessageID: base.ID(), Category: base.Type().Category}`
- `agentic/agentrun/agentrun.go:467` — `type LoopTerminalEvent struct {`
- `agentic/agentrun/agentrun.go:580` — `ev := LoopTerminalEvent{`

### Current #1231 and #1245 shapes

- `processor/agentic-dispatch/loop_admission.go:320` — `// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.`
- `processor/agentic-dispatch/loop_admission.go:407` — `// mergeLoopFacts reconciles two observations of one loop. The route fields go`
- `processor/agentic-loop/handlers.go:851` — `if task.LoopID != "" {`
- `processor/agentic-loop/handlers.go:856` — `entity, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/state.go:40` — `// ErrLoopBusy is returned when a continuation names a loop that has work`
- `processor/agentic-loop/state.go:54` — `// (releaseLoopTransientState), so absence is the ordinary steady state`
- `test/e2e/scenarios/agentic/approval_signal.go:18` — `"github.com/nats-io/nats.go/jetstream"`
- `test/e2e/scenarios/agentic/approval_signal.go:63` — `agentStream      = "AGENT"`
- `test/e2e/scenarios/agentic/approval_signal.go:64` — `agentLoopsBucket = "AGENT_LOOPS"`

### Scope and sequencing boundary

- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:385` — `AgentRun complete/failed settlement is transferred to #1249 and is not #1146 implementation scope.`
- `openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md:405` — `- AgentRun is transferred to #1249; no AgentRun production or spec work lands in #1146.`
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:1880` — `c.logger.Error("Failed to publish message", "error", err, "subject", msg.Subject)`

## Searches

- `git rev-parse HEAD`
- `git merge-base HEAD 417beae5552f8f15ad3540edd7d8504c87174c13`
- `git status --short`
- `git diff --name-only 417beae5552f8f15ad3540edd7d8504c87174c13..HEAD`
- `git log --oneline --decorate --graph --max-count=30 --all`
- `git grep -n 'ConsumeWithHeartbeat'`
- `git grep -n 'ConsumeDeliveryWithHeartbeat'`
- `git grep -n -E '\.(Ack|Nak|Term|InProgress)\(' -- '*.go'`
- `git grep -n -E 'ConsumeStreamWithConfig|ConsumeStreamWithConfigContexts' -- agentic processor natsclient`
- `gopls workspace_symbol -matcher=fuzzy ConsumeWithHeartbeat` — zero results.
- `gopls workspace_symbol -matcher=fuzzy DeliveryResult` — zero results.
- `gopls workspace_symbol -matcher=fuzzy ConsumeDeliveryWithHeartbeat` — zero results.
- `gopls references -d natsclient/delivery_settlement.go:298:6`
- `gopls references -d processor/agentic-loop/component.go:1393:21`
- `gopls call_hierarchy processor/agentic-loop/component.go:1393:21`
- `gopls references -d processor/agentic-loop/component.go:1161:21`
- `gopls references -d processor/agentic-loop/component.go:1780:21`
- `gopls references -d processor/agentic-loop/component.go:2096:21`
- `gopls references -d processor/agentic-loop/component.go:2321:21`
- `git grep -n -E 'agent\.(task|request|response|signal|approval|created|complete|failed)|tool\.(execute|result)|toolcall\.(approved|rejected|proposed)' -- agentic processor test openspec`
- `git grep -n -E 'AGENT_LOOPS|TOOL_CALL_OUTCOMES|AGENT_TRAJECTORIES|COMPLETE_'`
- `git grep -n -E 'pendingTaskResults|requestToLoop|toolCallToLoop|contextManagers|PendingApproval|waiters|LoopTracker' -- processor agentic`
- `git grep -n -E 'PublishToStream|PublishToStreamWithMsgID|Nats-Msg-Id|SourceMessageID' -- agentic processor internal natsclient`
- `git grep -n -E 'context\.(Background|TODO|WithoutCancel)|context\.Context|WithCancel\(' -- agentic processor`
- `git grep -n -E 'Drain\(|Closed\(|Stop\(|Wait\(' -- agentic/agentrun processor/agentic-*`
- `git grep -n -E 'StreamInfo|MaxAge|MaxBytes|Discard(New|Old)' -- agentic processor test openspec`
- `git grep -n -E 'RegisterPayloads|payloadbuiltins|StoreRegistry|storeregistry|storage\.Store' -- agentic payloadbuiltins processor storage`
- `git grep -n 'ApprovalContinuationV1' -- ':!openspec/changes/agentic-loop-restart-safety/**' ':!openspec/changes/semantic-jetstream-settlement/**'` — zero results.
- `git grep -n -E 'Load.*AGENT_LOOPS|hydrate|Hydrate' -- processor/agentic-loop processor/agentic-dispatch` — zero production loop-state hydration results; matches were documentation/tests or context-region vocabulary.
- `git grep -n -e 'adaptVoidInputHandler' -e 'ConsumeStreamWithConfigContexts' -e 'StreamInfo(' -- processor/agentic-loop processor/agentic-dispatch processor/agentic-model processor/agentic-tools processor/agentic-governance agentic/agentrun`
- `git grep -n -e 'Implementation remains blocked' -e 'remains blocked by #759' -e 'AgentRun is excluded' -e 'No production implementation begins' -e '#759 merges' -e 'AgentRun remains absent' -e 'AgentRun before #1148' -e 'Hold implementation until #759' -e 'Hold AgentRun' -e 'after #1148 merges' -- openspec/changes/agentic-loop-restart-safety`
- `git grep -n -e 'exact foundation F' -e '#1249' -e '#1244' -e 'post-#1231' -e '#1245' -- openspec/changes/semantic-jetstream-settlement/greenfield-staging-amendment-2026-09-02.md openspec/changes/agentic-loop-restart-safety`
- `git grep -n -e 'func (.*handleTaskMessage' -e 'func (.*handleResponseMessage' -e 'func (.*handleToolResultMessage' -e 'func (.*handleSignalMessage' -e 'func (.*handleApprovalResponseMessage' -e 'func (.*handleToolCallVerdictMessage' -e 'func (.*handleMessage' -e 'func (.*handleUserMessage' -e 'func (.*handleRequest' -e 'func (.*handleToolCall' -- processor/agentic-loop processor/agentic-dispatch processor/agentic-model processor/agentic-tools processor/agentic-governance`
- `gh issue view 1146 --json number,title,state,labels,milestone,body,comments,url`
- `gh issue view 1238 --json number,title,state,labels,milestone,body,comments,url`
- `gh issue view 1244 --json number,title,state,labels,milestone,body,comments,url`
- `gh issue view 1249 --json number,title,state,labels,milestone,body,comments,url`
- `gh pr view 1156 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url`
- `gh pr view 1159 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url`
- `gh pr view 1231 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url`
- `gh pr view 1245 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url`
- `gh issue list --state open --search 'restart settlement agentic' --limit 100`
- `openspec list`
- `git grep -n -E 'Name: "AGENT"|StreamName.*AGENT|MaxAge:|MaxBytes:|Discard(New|Old)|jetstream\.Discard' -- '*.go' '*.yaml' '*.yml' '*.json'`
- `git grep -n -E 'StreamInfo\(' -- agentic processor cmd natsclient | head -100`
- `git grep -n -E 'func \(c \*Component\) cleanup|binding\.drain\(|handle\.Drain\(|handle\.Closed\(|observerDone|wait.*Closed' -- processor/agentic-dispatch processor/agentic-governance processor/agentic-loop processor/agentic-model processor/agentic-tools`
- `git grep -n -E 'func \(c \*Component\) (handleAgentCreated|handleAgentApprovalPending|sendResponse|handleTaskSubmission|handleCommand|handleUserMessage)' -- processor/agentic-dispatch`
- `git grep -n -E 'Failed to (unmarshal|parse|resolve|marshal|publish|persist|handle)|No loop found|stale|duplicate|return nil|return$' -- processor/agentic-dispatch/component.go processor/agentic-dispatch/http.go processor/agentic-loop/approval_response_handler.go processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/component.go processor/agentic-model/component.go processor/agentic-governance/component.go | head -500`
- `git grep -n -E 'ApprovalPendingEvent|ApprovalResponse|LoopCreatedEvent|LoopCompletedEvent|LoopFailedEvent|Category(Task|UserMessage|AgentRequest|AgentResponse|ToolCall|ToolResult)' -- agentic`
- `git grep -n -E 'ApprovalContinuationV1|approval continuation' -- ':!openspec/changes/agentic-loop-restart-safety/**' ':!openspec/changes/semantic-jetstream-settlement/**'` — zero results.
- `git grep -n -E 'H\.1|H\.2|#1148|#759 merges|blocked by #759|No production implementation|merge' -- openspec/changes/agentic-loop-restart-safety/{proposal.md,design.md,tasks.md}`
- `gh issue view 1239 --json number,title,state,labels,milestone,body,comments,url`
- `git grep -n -E 'agentStreamConfig|agentStream|Subjects:.*agent\.>|MaxAge.*168h|MaxBytes.*64' -- config/streams.go`
- `git grep -n -E '"AGENT"[[:space:]]*:|AGENT:' -- '*.yaml' '*.yml' '*.json' '*.go'`
- `git grep -n -E 'Streams.*AGENT|streams.*AGENT|StreamConfig\{Subjects: \[\]string\{"agent\.>"\}' -- config cmd`
- `git grep -n -E 'func \(c \*Component\) cleanup|Drain\(|Closed\(|observerDone' -- processor/agentic-dispatch/component.go processor/agentic-governance/component.go processor/agentic-model/component.go processor/agentic-tools/component.go | sed -n '1,220p'`
- `git grep -n -E 'configs/agentic\.json|DefaultConfigPath|agentic\.json' -- cmd config README.md docs | head -100`
- `git grep -n -E 'StreamInfo\(' -- agentic processor cmd natsclient` — no production scoped matches; two KV/Test matches.
- `git grep -n -E 'StreamInfo\(' -- processor/agentic-loop processor/agentic-model processor/agentic-tools processor/agentic-dispatch processor/agentic-governance agentic/agentrun` — one integration-test match; zero production matches.
- `git grep -n -E 'WriteLoopBirth|birth|lineage|pendingTaskResults|publishResults|persistLoopState|HandleTask\(' -- processor/agentic-loop/component.go processor/agentic-loop/handlers.go processor/agentic-loop/trajectory_handler_wiring.go | head -300`
- `git grep -n -E 'Store\(|Put\(|Create\(|PublishToStream|logger\.(Error|Warn).*store|evidence' -- processor/agentic-loop/trajectory_recorder.go processor/agentic-loop/trajectory_handler_wiring.go processor/agentic-tools/component.go processor/agentic-tools/outcomes.go | head -350`
- `git grep -n -E 'PublishToStream|Publish\(' -- processor/agentic-loop/component.go processor/agentic-model/component.go processor/agentic-dispatch/component.go processor/agentic-dispatch/http.go processor/agentic-governance/component.go processor/agentic-tools/component.go | head -350`
- `git grep -n -E 'func \(c \*Component\) publishApprovalResponse|PublishToStream' -- processor/agentic-dispatch/http.go`
- `git grep -n -E 'handleSignalMessage|Unsupported signal type|handlePauseSignal|Cannot pause loop|PauseRequested|handleResumeSignal|Cannot resume non-paused loop|UpdateLoop\(entity\)|persistLoopState\(ctx, loopID\)|adaptVoidInputHandler\(c\.handleSignalMessage\)|msg\.Ack\(\)' -- processor/agentic-loop/component.go agentic/state.go`

## Verification

Inventory verifier: `pins=555 ok=555 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`.

Inventory pins: 555.

Recorded searches: 64.

Final file SHA-256 is recorded in the explorer handoff after this verification block is finalized.
