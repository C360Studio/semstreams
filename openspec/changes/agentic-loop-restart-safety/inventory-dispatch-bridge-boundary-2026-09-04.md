# Inventory: agentic-loop to agentic-dispatch bridge boundary

base: 79b0f29f82ce5391013f6c931fae69a28216ac93
pre-review-content-sha256: a6a06c59f88e75f0ade65a6b96697f344504258f39e73757e57eaa515a6e696b

The pre-review hash identifies the file reviewed before this re-baseline. The exact reviewed hash of this revision is
recorded in the explorer handoff because embedding a file's own final hash would be self-referential.

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:178` — `## 6. Dispatch edge gateway and approval continuation gate`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:180` — `- [ ] 6.1 RED: run the real-NATS approval replacement gate after an approval-required `ToolResult` fully settles.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:183` — `  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:184` — `- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:188` — `- [ ] 6.3 Implement only operation-specific exact reads for latest `agent.request.<LoopID>` and exact`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:202` — `- [ ] 6.7 Implement one mixed `AGENT_LOOPS` classifier for canonical current `LoopEntity` keys, activity-only`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:203` — `  `COMPLETE_` keys, and every current research-pipeline namespace. Known research records return `keep=false`;`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:206` — `- [ ] 6.8 Add a real mixed-bucket proof covering valid current `LoopEntity`, typed terminal `COMPLETE_`, registered`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:208` — `  tombstone, and healing. Marshal and unmarshal `SearchResult` through the registered production `BaseMessage``
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:82` — `### Requirement: Dispatch uses one authority-backed current-state projection`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:84` — `Dispatch SHALL use one caught-up graph view over `AGENT_LOOPS` for `/activity`, `/loops`, `/debug/state`, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:93` — `#### Scenario: Approval follows replacement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:100` — `#### Scenario: Projection endpoint is unavailable`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:89` — `Explicit LoopID approval, read, continuation, cancellation, terminal-route, and command-owner operations SHALL`
- `openspec/changes/agentic-loop-restart-safety/design.md:342` — `The gap after task PubAck and before first `LoopEntity` birth remains explicit. A second route-only AutoContinue`

## Spellings of the fact

### Dispatch-to-loop bridge census

Dispatch declares every outward family at its component boundary:

- `processor/agentic-dispatch/config.go:144` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Description: "Agent task requests",`
- `processor/agentic-dispatch/config.go:147` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Description: "Agent control signals",`
- `processor/agentic-dispatch/config.go:150` — `Name: "user.response",`
- `processor/agentic-dispatch/config.go:156` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Description: "Approval responses submitted via the dispatch HTTP /loops/{id}/approval endpoint, consumed by agentic-loop's approval-response handler",`

Agentic-loop declares and routes the corresponding durable inputs:

- `processor/agentic-loop/config.go:401` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:405` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:409` — `Name: "tool.result", Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-loop/config.go:413` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:417` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/component.go:907` — `case "agent.task":`
- `processor/agentic-loop/component.go:909` — `case "agent.response":`
- `processor/agentic-loop/component.go:911` — `case "tool.result":`
- `processor/agentic-loop/component.go:913` — `case "agent.signal":`
- `processor/agentic-loop/component.go:915` — `case "agent.approval_response":`

Dispatch producers and the external-response path use those output families:

- `processor/agentic-dispatch/component.go:915` — `func (c *Component) buildTaskMessage(ctx context.Context, msg agentic.UserMessage, loopID, taskID string) agentic.TaskMessage {`
- `processor/agentic-dispatch/component.go:1026` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)`
- `processor/agentic-dispatch/component.go:1051` — `if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {`
- `processor/agentic-dispatch/commands.go:158` — `Type:        agentic.SignalCancel,`
- `processor/agentic-dispatch/commands.go:175` — `subject, err := component.ResolveSubject(c.outputPortDefs(), signalOutputPortName, targetLoopID)`
- `processor/agentic-dispatch/commands.go:179` — `if err := c.natsClient.Publish(ctx, subject, signalData); err != nil {`
- `processor/agentic-dispatch/http.go:843` — `func (c *Component) publishApprovalResponse(ctx context.Context, loopID, callID string, req *ApprovalRequest, approver string) (string, error) {`
- `processor/agentic-dispatch/http.go:862` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.approval_response", loopID)`
- `processor/agentic-dispatch/http.go:866` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/component.go:1195` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", resp.ChannelType+"."+resp.ChannelID)`
- `processor/agentic-dispatch/component.go:1199` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`

### Loop-to-dispatch bridge census

Agentic-loop declares the four lifecycle/projection output families and constructs each event:

- `processor/agentic-loop/config.go:450` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Description: "Agent task completions (JetStream)",`
- `processor/agentic-loop/config.go:453` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Description: "Loop-created lifecycle events (JetStream)",`
- `processor/agentic-loop/config.go:456` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Description: "Loop-failed lifecycle events (JetStream)",`
- `processor/agentic-loop/config.go:462` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Description: "Tool calls awaiting human approval (JetStream)",`
- `processor/agentic-loop/handlers.go:595` — `created := agentic.LoopCreatedEvent{`
- `processor/agentic-loop/handlers.go:1076` — `createdSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.created", loopID)`
- `processor/agentic-loop/handlers.go:2102` — `completion := agentic.LoopCompletedEvent{`
- `processor/agentic-loop/handlers.go:2177` — `completionSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.complete", loopID)`
- `processor/agentic-loop/handlers.go:2390` — `pending := &agentic.ApprovalPendingEvent{`
- `processor/agentic-loop/handlers.go:2405` — `subject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.approval_pending", loopID)`
- `processor/agentic-loop/handlers.go:2686` — `failure := &agentic.LoopFailedEvent{`
- `processor/agentic-loop/handlers.go:2739` — `failureSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.failed", loopID)`

Dispatch declares and consumes those four inputs:

- `processor/agentic-dispatch/config.go:118` — `Name: "user.message", Config: component.JetStreamPort{Subjects: []string{"user.message.>"}, StreamName: "USER"}, Required: true, External: true,`
- `processor/agentic-dispatch/config.go:122` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-dispatch/config.go:126` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-dispatch/config.go:130` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-dispatch/config.go:134` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-dispatch/component.go:623` — `decision, cause := runDispatchDeliveryWork(msgCtx, msg.Data(), c.handleAgentCreated)`
- `processor/agentic-dispatch/component.go:707` — `decision, cause := runDispatchDeliveryWork(msgCtx, msg.Data(), c.handleAgentApprovalPending)`
- `processor/agentic-dispatch/component.go:742` — `func (c *Component) handleTerminalDelivery(`

The external user-message lane is durable, explicit-ack, DeliverLast, and finite MaxDeliver:

- `processor/agentic-dispatch/component.go:537` — `userMsgCfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-dispatch/component.go:541` — `DeliverPolicy: "last",`
- `processor/agentic-dispatch/component.go:543` — `MaxDeliver:    3,`
- `processor/agentic-dispatch/component.go:552` — `decision, cause := runDispatchDeliveryWork(msgCtx, msg.Data(), c.handleUserMessage)`

The four consumers are durable, explicit-ack consumers with DeliverNew policy; their MaxDeliver values differ:

- `processor/agentic-dispatch/component.go:569` — `agentCompleteCfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-dispatch/component.go:573` — `DeliverPolicy: "new",`
- `processor/agentic-dispatch/component.go:574` — `AckPolicy:     "explicit",`
- `processor/agentic-dispatch/component.go:608` — `agentCreatedCfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-dispatch/component.go:612` — `DeliverPolicy: "new",`
- `processor/agentic-dispatch/component.go:613` — `AckPolicy:     "explicit",`
- `processor/agentic-dispatch/component.go:640` — `agentFailedCfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-dispatch/component.go:644` — `DeliverPolicy: "new",`
- `processor/agentic-dispatch/component.go:645` — `AckPolicy:     "explicit",`
- `processor/agentic-dispatch/component.go:646` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:692` — `agentApprovalPendingCfg := natsclient.StreamConsumerConfig{`
- `processor/agentic-dispatch/component.go:696` — `DeliverPolicy: "new",`
- `processor/agentic-dispatch/component.go:697` — `AckPolicy:     "explicit",`
- `processor/agentic-dispatch/component.go:575` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:614` — `MaxDeliver:    3,`
- `processor/agentic-dispatch/component.go:698` — `MaxDeliver:    10,`

Agentic-loop's routing switch also includes the governance verdict family in the named 905–925 seam:

- `processor/agentic-loop/component.go:917` — `case "agent.toolcall.approved", "agent.toolcall.rejected":`
- `processor/agentic-loop/component.go:925` — `settleHandlerFn = c.handleToolCallVerdictMessage`

### LoopTracker state, writers, readers, and exposed seams

The process projection contains three correlation maps, loop snapshots, and a bounded pending-approval arrival buffer:

- `processor/agentic-dispatch/loop_tracker.go:14` — `type LoopInfo struct {`
- `processor/agentic-dispatch/loop_tracker.go:46` — `// PendingApproval is populated from agent.approval_pending events`
- `processor/agentic-dispatch/loop_tracker.go:64` — `type PendingApprovalInfo struct {`
- `processor/agentic-dispatch/loop_tracker.go:78` — `const pendingApprovalBufferCap = 256`
- `processor/agentic-dispatch/loop_tracker.go:85` — `const pendingApprovalBufferTTL = 60 * time.Second`
- `processor/agentic-dispatch/loop_tracker.go:96` — `type LoopTracker struct {`
- `processor/agentic-dispatch/loop_tracker.go:98` — `userLoops    map[string]string    // user_id -> most recent loop_id`
- `processor/agentic-dispatch/loop_tracker.go:99` — `channelLoops map[string]string    // channel_id -> most recent loop_id`
- `processor/agentic-dispatch/loop_tracker.go:100` — `loops        map[string]*LoopInfo // loop_id -> LoopInfo`
- `processor/agentic-dispatch/loop_tracker.go:109` — `pendingApprovalBuffer map[string]*bufferedPendingApproval`
- `processor/agentic-dispatch/loop_tracker.go:114` — `func NewLoopTracker() *LoopTracker {`
- `processor/agentic-dispatch/loop_tracker.go:124` — `func NewLoopTrackerWithLogger(logger *slog.Logger) *LoopTracker {`

Its mutation and enumeration surface is:

- `processor/agentic-dispatch/loop_tracker.go:144` — `func (t *LoopTracker) Track(info *LoopInfo) {`
- `processor/agentic-dispatch/loop_tracker.go:184` — `func (t *LoopTracker) Get(loopID string) *LoopInfo {`
- `processor/agentic-dispatch/loop_tracker.go:193` — `func (t *LoopTracker) getSnapshot(loopID string) *LoopInfo {`
- `processor/agentic-dispatch/loop_tracker.go:205` — `func (t *LoopTracker) GetActiveLoop(userID, channelID string) string {`
- `processor/agentic-dispatch/loop_tracker.go:229` — `func (t *LoopTracker) UpdateState(loopID, state string) {`
- `processor/agentic-dispatch/loop_tracker.go:253` — `func (t *LoopTracker) UpdateIterations(loopID string, iterations int) {`
- `processor/agentic-dispatch/loop_tracker.go:274` — `func (t *LoopTracker) UpdateCompletion(loopID, outcome, result, errMsg string) error {`
- `processor/agentic-dispatch/loop_tracker.go:282` — `func (t *LoopTracker) updateCompletionAt(loopID, outcome, result, errMsg string, completedAt time.Time) (bool, error) {`
- `processor/agentic-dispatch/loop_tracker.go:332` — `func (t *LoopTracker) SetPendingApproval(loopID string, pending *PendingApprovalInfo) bool {`
- `processor/agentic-dispatch/loop_tracker.go:393` — `func (t *LoopTracker) GetPendingApprovalCallID(loopID string) (string, bool) {`
- `processor/agentic-dispatch/loop_tracker.go:408` — `func (t *LoopTracker) ClearPendingApproval(loopID string) {`
- `processor/agentic-dispatch/loop_tracker.go:452` — `func (t *LoopTracker) UpdateWorkflowContext(loopID, workflowSlug, workflowStep string) bool {`
- `processor/agentic-dispatch/loop_tracker.go:479` — `func (t *LoopTracker) UpdateContextRequestID(loopID, contextRequestID string) bool {`
- `processor/agentic-dispatch/loop_tracker.go:503` — `func (t *LoopTracker) Remove(loopID string) {`
- `processor/agentic-dispatch/loop_tracker.go:539` — `func (t *LoopTracker) GetUserLoops(userID string) []*LoopInfo {`
- `processor/agentic-dispatch/loop_tracker.go:553` — `func (t *LoopTracker) GetAllLoops() []*LoopInfo {`
- `processor/agentic-dispatch/loop_tracker.go:565` — `func (t *LoopTracker) Count() int {`

Writers include channel and HTTP task creation, externally observed creation, approval pending, and terminal settlement:

- `processor/agentic-dispatch/component.go:1037` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/http.go:371` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/component.go:1104` — `if existing := c.loopTracker.Get(created.LoopID); existing != nil {`
- `processor/agentic-dispatch/component.go:1106` — `c.loopTracker.UpdateWorkflowContext(created.LoopID, created.WorkflowSlug, created.WorkflowStep)`
- `processor/agentic-dispatch/component.go:1108` — `c.loopTracker.UpdateContextRequestID(created.LoopID, created.ContextRequestID)`
- `processor/agentic-dispatch/component.go:1113` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/component.go:1169` — `if accepted := c.loopTracker.SetPendingApproval(pending.LoopID, &PendingApprovalInfo{`
- `processor/agentic-dispatch/terminal_settlement.go:207` — `trackerChanged, err = c.loopTracker.updateCompletionAt(event.LoopID, event.Outcome, event.Result, event.Error, event.TerminalAt)`

The tracker is exported to embedders and injected into command execution:

- `processor/agentic-dispatch/component.go:1264` — `func (c *Component) LoopTracker() *LoopTracker {`
- `processor/agentic-dispatch/command_registry.go:29` — `type CommandContext struct {`
- `processor/agentic-dispatch/command_registry.go:31` — `LoopTracker   *LoopTracker`
- `processor/agentic-dispatch/component.go:1381` — `cmdCtx := &CommandContext{`

All four AutoContinue resolution sites use process-local user/channel correlation:

- `processor/agentic-dispatch/component.go:861` — `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`
- `processor/agentic-dispatch/component.go:988` — `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`
- `processor/agentic-dispatch/http.go:237` — `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`
- `processor/agentic-dispatch/http.go:323` — `loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)`

List/status/debug and approval lookup expose differing projection behavior:

- `processor/agentic-dispatch/commands.go:196` — `func (c *Component) handleStatusCommand(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error) {`
- `processor/agentic-dispatch/commands.go:235` — `if loopInfo := c.loopTracker.Get(targetLoopID); loopInfo != nil {`
- `processor/agentic-dispatch/commands.go:253` — `loops := c.loopTracker.GetUserLoops(msg.UserID)`
- `processor/agentic-dispatch/http.go:556` — `loops = c.loopTracker.GetUserLoops(userID)`
- `processor/agentic-dispatch/http.go:558` — `loops = c.loopTracker.GetAllLoops()`
- `processor/agentic-dispatch/http.go:683` — `if tracked := c.loopTracker.Get(loopID); tracked != nil {`
- `processor/agentic-dispatch/http.go:686` — `persisted, err := c.loadPersistedLoop(ctx, loopID)`
- `processor/agentic-dispatch/http.go:773` — `callID, awaiting := c.loopTracker.GetPendingApprovalCallID(loopID)`
- `processor/agentic-dispatch/http.go:805` — `c.loopTracker.ClearPendingApproval(loopID)`
- `processor/agentic-dispatch/http.go:951` — `LoopCount:    c.loopTracker.Count(),`
- `processor/agentic-dispatch/http.go:953` — `Loops:        c.loopTracker.GetAllLoops(),`
- `processor/agentic-dispatch/loop_admission.go:228` — `lookup := c.lookupLoop(ctx, req.LoopID)`
- `processor/agentic-dispatch/loop_admission.go:337` — `func (c *Component) lookupLoop(ctx context.Context, loopID string) loopLookup {`
- `processor/agentic-dispatch/loop_admission.go:338` — `tracked := c.loopTracker.getSnapshot(loopID)`
- `processor/agentic-dispatch/loop_admission.go:339` — `persisted, persistErr := c.loadPersistedLoop(ctx, loopID)`
- `processor/agentic-dispatch/terminal_settlement.go:190` — `persisted, err := c.loadPersistedLoop(ctx, event.LoopID)`

Construction creates an empty tracker. No production hydration/rebuild call was located; exact LoopID reads use the
persisted record in the GET/admission/terminal seams above, while list, approval lookup, debug, and AutoContinue use
the tracker-only reads above.

### AGENT_LOOPS authority, keys, retention, and writes

Agentic-loop owns the AGENT_LOOPS write port and creates the bucket with history 10 and a 24-hour TTL:

- `processor/agentic-loop/config.go:385` — `LoopsBucket:                       "AGENT_LOOPS",`
- `processor/agentic-loop/config.go:438` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/component.go:786` — `func (c *Component) initializeKVBuckets(ctx context.Context) error {`
- `processor/agentic-loop/component.go:797` — `Bucket:  c.config.LoopsBucket,`
- `processor/agentic-loop/component.go:798` — `History: 10,`
- `processor/agentic-loop/component.go:799` — `TTL:     24 * time.Hour,`

The keyspace has one current record under loopID and a terminal record under COMPLETE_loopID:

- `processor/agentic-loop/component.go:2015` — `// Key pattern: COMPLETE_{loopID} for rules engine to watch.`
- `processor/agentic-loop/component.go:2028` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2055` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2079` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2107` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`

The first task-path persistence result is ignored, later persistence is required by the handler, while terminal
failure-record persistence logs and returns no error:

- `processor/agentic-loop/approval_sweeper.go:95` — `c.persistLoopState(ctx, cand.LoopID)`
- `processor/agentic-loop/component.go:1357` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1559` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:1707` — `// A required persistence or publication failure leaves the joined delivery in`
- `processor/agentic-loop/component.go:1709` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1713` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1714` — `return err`
- `processor/agentic-loop/component.go:2224` — `if err := c.persistLoopState(ctx, loopID); err != nil {`
- `processor/agentic-loop/component.go:2017` — `func (c *Component) persistCompletionState(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {`
- `processor/agentic-loop/component.go:2029` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2044` — `func (c *Component) persistFailureState(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) {`
- `processor/agentic-loop/component.go:2057` — `c.logger.Error("Failed to persist failure state", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:2069` — `func (c *Component) persistCancellationState(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) error {`
- `processor/agentic-loop/component.go:2092` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`

Dispatch declares AGENT_LOOPS as an optional read port and exact-reads loopID records:

- `processor/agentic-dispatch/config.go:45` — `const agentLoopsPortName = "agent_loops"`
- `processor/agentic-dispatch/config.go:138` — `Name: agentLoopsPortName, Config: component.KVReadPort{Bucket: "AGENT_LOOPS"}, Required: false,`
- `processor/agentic-dispatch/terminal_settlement.go:91` — `func (c *Component) loadPersistedLoop(ctx context.Context, loopID string) (*agentic.LoopEntity, error) {`
- `processor/agentic-dispatch/terminal_settlement.go:106` — `entry, err := kv.Get(ctx, loopID)`
- `processor/agentic-dispatch/terminal_settlement.go:375` — `root, err := c.loadPersistedLoop(ctx, anchor)`
- `processor/agentic-dispatch/terminal_settlement.go:420` — `parent, err := c.loadPersistedLoop(ctx, parentID)`
- `processor/agentic-dispatch/terminal_settlement.go:442` — `root, err := c.loadPersistedLoop(ctx, anchor)`

### Terminal response routing and projection-dependent metrics

- `processor/agentic-dispatch/terminal_settlement.go:123` — `func terminalResponse(event agentterminal.Event, route terminalRoute) agentic.UserResponse {`
- `processor/agentic-dispatch/terminal_settlement.go:157` — `func (c *Component) publishTerminalResponse(ctx context.Context, response agentic.UserResponse, msgID string) error {`
- `processor/agentic-dispatch/terminal_settlement.go:169` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", response.ChannelType+"."+response.ChannelID)`
- `processor/agentic-dispatch/terminal_settlement.go:173` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:179` — `func (c *Component) settleAgentTerminal(ctx context.Context, data []byte) (settleErr error) {`
- `processor/agentic-dispatch/metrics.go:16` — `activeLoops         prometheus.Gauge`
- `processor/agentic-dispatch/metrics.go:309` — `m.activeLoops.Inc()`
- `processor/agentic-dispatch/metrics.go:314` — `m.activeLoops.Dec()`
- `processor/agentic-dispatch/component.go:1049` — `c.metrics.recordLoopStarted()`
- `processor/agentic-dispatch/component.go:1127` — `c.metrics.recordLoopStarted()`
- `processor/agentic-dispatch/terminal_settlement.go:214` — `c.metrics.recordLoopEnded()`

### Present correctness dependencies and projection/notification consumers

The code presently uses created events to install routing/correlation state, pending events to install approval call
identity, and terminal events to publish the caller response and settle terminal projection state:

- `processor/agentic-dispatch/component.go:1104` — `if existing := c.loopTracker.Get(created.LoopID); existing != nil {`
- `processor/agentic-dispatch/component.go:1113` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/component.go:1169` — `if accepted := c.loopTracker.SetPendingApproval(pending.LoopID, &PendingApprovalInfo{`
- `processor/agentic-dispatch/terminal_settlement.go:169` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", response.ChannelType+"."+response.ChannelID)`

The same event/projection facts are also read for list/status/debug, activity SSE, and gauges:

- `processor/agentic-dispatch/http.go:556` — `loops = c.loopTracker.GetUserLoops(userID)`
- `processor/agentic-dispatch/http.go:683` — `if tracked := c.loopTracker.Get(loopID); tracked != nil {`
- `processor/agentic-dispatch/http.go:951` — `LoopCount:    c.loopTracker.Count(),`
- `processor/agentic-dispatch/http_activity.go:374` — `clientID string, snap graphview.Snapshot[activityRecord]) {`
- `processor/agentic-dispatch/metrics.go:195` — `Help:      "1 when the shared AGENT_LOOPS activity view is caught up and its watcher healthy, 0 while bootstrapping or after watcher loss (staleness signal)",`

## Problem shape

| Surface | Catalog / source | Lifecycle and readiness | Atomic snapshot / readers | Writers / ownership | Watcher loss and recovery | Status exposure |
|---|---|---|---|---|---|---|
| Agentic-loop `LoopManager` process execution state | `processor/agentic-loop/state.go:61` — `type LoopManager struct {`; `processor/agentic-loop/state.go:62` — `loops                map[string]*agentic.LoopEntity`; `processor/agentic-loop/state.go:72` — `requestToLoop        map[string]string                   // requestID -> loopID`; `processor/agentic-loop/state.go:73` — `toolCallToLoop       map[string]string                   // callID -> loopID` | `processor/agentic-loop/handlers.go:175` — `loopManager := NewLoopManagerWithConfig(config.Context, loopManagerOpts...)`; `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`; `processor/agentic-loop/trajectory_handler_wiring.go:67` — `_ = c.handler.loopManager.DeleteLoop(loopID)`; `processor/agentic-loop/state.go:497` — `func (m *LoopManager) DeleteLoop(loopID string) error {`; `processor/agentic-loop/state.go:501` — `delete(m.loops, loopID)`; `processor/agentic-loop/state.go:510` — `delete(m.taskPrompts, loopID)` | `processor/agentic-loop/state.go:318` — `func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {`; reads are protected by the manager mutex at `processor/agentic-loop/state.go:319` — `m.mu.RLock()` | `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`; `processor/agentic-loop/state.go:335` — `func (m *LoopManager) UpdateLoop(entity agentic.LoopEntity) error {`; `processor/agentic-loop/handlers.go:178` — `loopManager:       loopManager,` | `processor/agentic-loop/state.go:1171` — `func (m *LoopManager) GetLoopForRequestWithRecovery(requestID string) (string, bool) {`; `processor/agentic-loop/state.go:1185` — `m.TrackRequest(requestID, loopID)`; `processor/agentic-loop/state.go:1195` — `func (m *LoopManager) GetLoopForToolCallWithRecovery(toolCallID string) (string, bool) {`; `processor/agentic-loop/state.go:1209` — `m.TrackToolCall(toolCallID, loopID)` | `processor/agentic-loop/state.go:1219` — `func (m *LoopManager) UpdateCompletion(loopID, outcome, result, errMsg string) error {`; `GetLoop` exposes the resulting LoopEntity state |
| Dispatch `LoopTracker` process projection | `processor/agentic-dispatch/loop_tracker.go:96` — `type LoopTracker struct {`; `processor/agentic-dispatch/loop_tracker.go:100` — `loops        map[string]*LoopInfo // loop_id -> LoopInfo` | `processor/agentic-dispatch/component.go:183` — `loopTracker:   NewLoopTrackerWithLogger(logger),`; the method census contains no Start/Stop lifecycle | `processor/agentic-dispatch/loop_tracker.go:184` — `func (t *LoopTracker) Get(loopID string) *LoopInfo {`; `processor/agentic-dispatch/loop_tracker.go:205` — `func (t *LoopTracker) GetActiveLoop(userID, channelID string) string {`; `processor/agentic-dispatch/loop_tracker.go:553` — `func (t *LoopTracker) GetAllLoops() []*LoopInfo {` | `processor/agentic-dispatch/loop_tracker.go:144` — `func (t *LoopTracker) Track(info *LoopInfo) {`; `processor/agentic-dispatch/loop_tracker.go:274` — `func (t *LoopTracker) UpdateCompletion(loopID, outcome, result, errMsg string) error {`; `processor/agentic-dispatch/loop_tracker.go:332` — `func (t *LoopTracker) SetPendingApproval(loopID string, pending *PendingApprovalInfo) bool {` | `processor/agentic-dispatch/loop_tracker.go:158` — `if buffered, ok := t.pendingApprovalBuffer[info.LoopID]; ok {` recovers an early pending event when creation arrives; no full hydration/rebuild method was located by the zero-hit searches | `processor/agentic-dispatch/commands.go:196` — `func (c *Component) handleStatusCommand(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error) {`; `processor/agentic-dispatch/http.go:683` — `if tracked := c.loopTracker.Get(loopID); tracked != nil {`; `processor/agentic-dispatch/http.go:951` — `LoopCount:    c.loopTracker.Count(),` |
| `pkg/graphview.View` | `pkg/graphview/view.go:173` — `func New[T any](source WatcherSource, decode DecodeFunc[T], opts ...Option) (*View[T], error) {` | `pkg/graphview/view.go:204` — `func (v *View[T]) Start(ctx context.Context) error {`; `pkg/graphview/view.go:333` — `func (v *View[T]) WaitCaughtUp(ctx context.Context) error {`; `pkg/graphview/errors.go:15` — `ErrNotReady = errors.New("graphview: view not ready")` | `pkg/graphview/view.go:442` — `func (v *View[T]) Get(key string) (T, uint64, error) {`; `pkg/graphview/view.go:491` — `func (v *View[T]) SnapshotAndSubscribe(ctx context.Context) (Snapshot[T], *Subscription[T], error) {` | `pkg/graphview/view.go:139` — `type View[T any] struct {`; the source watcher supplies entries to this process-local view | `pkg/graphview/view.go:237` — `func (v *View[T]) Restart() error {`; `pkg/graphview/view.go:608` — `func (v *View[T]) runWatcher(ctx context.Context, watcher jetstream.KeyWatcher) {`; `pkg/graphview/view.go:751` — `func (v *View[T]) failClosed(cause error) {` | readiness/errors are returned through View APIs |
| Dispatch activity instance | `processor/agentic-dispatch/http_activity.go:201` — `kv, err := c.natsClient.GetKeyValueBucket(ctx, bucket)` | `processor/agentic-dispatch/http_activity.go:182` — `func (c *Component) ensureActivityView(ctx context.Context) (*graphview.View[activityRecord], error) {`; `processor/agentic-dispatch/http_activity.go:267` — `err := view.WaitCaughtUp(ctx)` | `processor/agentic-dispatch/http_activity.go:278` — `return view.SnapshotAndSubscribe(ctx)`; `processor/agentic-dispatch/http_activity.go:373` — `func (c *Component) replayActivitySnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,` | `processor/agentic-dispatch/component.go:112` — `// Shared AGENT_LOOPS read view (ADR-081): ONE graphview.View serves every`; `processor/agentic-dispatch/component.go:116` — `activityCommands chan activityViewCommand` | `processor/agentic-dispatch/http_activity.go:268` — `if err != nil && errors.Is(err, graphview.ErrWatcherLost) {`; `processor/agentic-dispatch/http_activity.go:269` — `if rerr := view.Restart(); rerr != nil {` | `processor/agentic-dispatch/metrics.go:195` — `Help:      "1 when the shared AGENT_LOOPS activity view is caught up and its watcher healthy, 0 while bootstrapping or after watcher loss (staleness signal)",` |
| Graph-query summary adoption | `processor/graph-query/component.go:284` — `return graph.OpenCatalogReader(ctx, deps.NATSClient, graph.BucketCommunitySummaries)` | `processor/graph-query/summary_view.go:108` — `func (c *Component) superviseSummaryView(ctx context.Context) {`; `processor/graph-query/summary_view.go:159` — `if err := view.Start(ctx); err != nil {` | `processor/graph-query/summary_view.go:137` — `view, err := graphview.New[clustering.CommunitySummaryRecord](`; `processor/graph-query/summary_view.go:283` — `func (c *Component) summaryFor(comm *clustering.Community) (string, bool) {`; `processor/graph-query/summary_view.go:296` — `record, _, err := view.Get(key)` | `processor/graph-query/component.go:159` — `// Optional COMMUNITY_SUMMARIES serving view. The supervisor is the sole`; `processor/graph-query/component.go:163` — `summaryView            *graphview.View[clustering.CommunitySummaryRecord]`; `processor/graph-query/summary_view.go:241` — `func (c *Component) publishSummaryView(view *graphview.View[clustering.CommunitySummaryRecord]) {` | `processor/graph-query/summary_view.go:144` — `OnWatcherLost: func(error) { signalSummaryViewLoss(loss) },`; `processor/graph-query/summary_view.go:190` — `c.logger.Warn("COMMUNITY_SUMMARIES view lost; using statistical summaries until replacement")` | `processor/graph-query/summary_view.go:124` — `c.logger.Warn("COMMUNITY_SUMMARIES unavailable; using statistical summaries",` |
| Graph catalogs | `graph/kvcatalog.go:172` — `derived(BucketCommunitySummaries, "graph-clustering",` | `graph/kvcatalog.go:242` — `func EnsureCatalogBucket(ctx context.Context, client *natsclient.Client, name string) (jetstream.KeyValue, error) {`; `graph/kvcatalog.go:249` — `return natsclient.EnsureFrameworkBucket(ctx, client, spec)` | `graph/kvcatalog.go:255` — `type CatalogReader interface {`; `graph/kvcatalog.go:258` — `WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)` | `graph/kvcatalog.go:237` — `// EnsureCatalogBucket resolves a catalog bucket by name and acquires it`; `graph/kvcatalog.go:277` — `func OpenCatalogReader(ctx context.Context, client *natsclient.Client, name string) (CatalogReader, error) {` | `graph/kvcatalog.go:284` — `bucket, err := natsclient.OpenFrameworkBucket(ctx, client, spec)`; `graph/kvcatalog.go:285` — `if err != nil {` | `graph/kvcatalog.go:262` — `Status(ctx context.Context) (jetstream.KeyValueStatus, error)` |

No other dispatch-owned KVWritePort, durable bucket creation, full LoopTracker hydration, or process-projection rebuild
was located by the zero-hit searches recorded below.

## Consumers

### In-repository terminal consumers

- `agentic/agentrun/agentrun.go:826` — `completeCfg := natsclient.StreamConsumerConfig{`
- `agentic/agentrun/agentrun.go:829` — `FilterSubject: "agent.complete.*",`
- `agentic/agentrun/agentrun.go:835` — `completeHandle, err := client.ConsumeInternalStreamWithConfig(`
- `agentic/agentrun/agentrun.go:877` — `failedCfg := natsclient.StreamConsumerConfig{`
- `agentic/agentrun/agentrun.go:880` — `FilterSubject: "agent.failed.*",`
- `agentic/agentrun/agentrun.go:886` — `failedHandle, err := client.ConsumeInternalStreamWithConfig(`
- `output/otel/span_collector.go:211` — `// Classification only decides whether this broad input must cross the`
- `output/otel/span_collector.go:215` — `strings.HasPrefix(subject, "agent.complete.") || strings.HasPrefix(subject, "agent.failed.") {`
- `output/otel/span_collector.go:216` — `terminal, err := agentterminal.Decode(sc.decoder, data)`
- `output/otel/span_collector.go:220` — `sc.endLoopSpanTerminal(terminal)`

### In-repository HTTP and approval surface

- `processor/agentic-dispatch/http.go:91` — `mux.HandleFunc("POST "+prefix+"message", c.handleHTTPMessage)`
- `processor/agentic-dispatch/http.go:100` — `mux.HandleFunc("GET "+prefix+"loops", c.handleListLoops)`
- `processor/agentic-dispatch/http.go:101` — `mux.HandleFunc("GET "+prefix+"loops/{id}", c.handleGetLoop)`
- `processor/agentic-dispatch/http.go:102` — `mux.HandleFunc("POST "+prefix+"loops/{id}/approval", c.handleLoopApproval)`
- `processor/agentic-dispatch/http.go:105` — `mux.HandleFunc("GET "+prefix+"activity", c.handleActivityStream)`
- `processor/agentic-dispatch/loop_admission_test.go:214` — `func TestContinuationAfterReplacementIsAdmittedFromDurableRecord(t *testing.T) {`
- `processor/agentic-dispatch/loop_seams_test.go:643` — `func TestReadSeamsAnswerFromTheDurableRecordAfterReplacement(t *testing.T) {`
- `processor/agentic-dispatch/http_loops_test.go:38` — `func TestHandleListLoops(t *testing.T) {`
- `processor/agentic-dispatch/http_loops_test.go:138` — `func TestHandleGetLoop(t *testing.T) {`
- `processor/agentic-dispatch/approval_handler_test.go:102` — `func TestHandleLoopApproval_NotAwaitingApproval(t *testing.T) {`
- `processor/agentic-dispatch/approval_handler_test.go:230` — `func TestHandleLoopApproval_FailedPublishPreservesPendingApproval(t *testing.T) {`
- `processor/agentic-dispatch/terminal_settlement_integration_test.go:23` — `func TestIntegrationTerminalSettlementRestartRouteStableDedupAndUnlimitedAttempts(t *testing.T) {`
- `processor/agentic-dispatch/terminal_origin_integration_test.go:63` — `func TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart(t *testing.T) {`
- `processor/agentic-dispatch/http_activity_test.go:481` — `func TestActivityStream_ReplayThenLiveOrdering(t *testing.T) {`
- `processor/agentic-dispatch/http_activity_test.go:620` — `func TestActivityStream_WatcherLossFailsClosedThenRestartsOnAttach(t *testing.T) {`
- `processor/agentic-dispatch/http_activity_test.go:858` — `func TestComponentStopStopsActivityView(t *testing.T) {`

## Adjacent claims

### Sister-repository callers and terminal consumers

The sister repositories were inspected read-only. Their paths are recorded as external facts rather than local pins.

- semteams `ui/src/lib/services/agentApi.ts:320-321` lists loops, `:331-332` gets a loop, and `:383-401` posts
  approval.
- semteams `ui/src/lib/stores/agentStore.svelte.ts:18-19` names activity and loop endpoints, `:40` fetches the
  initial loop snapshot, and `:135` opens the activity EventSource.
- semteams `ui/src/lib/components/board/PendingApprovalSection.svelte:67-85` submits approval and handles HTTP 409.
- semteams `ui/e2e/agentic/approval-resume.spec.ts:115-125` retries the 409 approval race.
- semteams `configs/flow-bootstrap.json:610` enables AutoContinue.
- semdev `configs/semdev-live-gemini.json:713` enables AutoContinue.
- semsage `tools/spawn/executor.go:182-198` subscribes to complete and failed child-loop terminal subjects.
- semdragon `processor/questbridge/handler.go:432-450` declares its durable complete/failed consumer, and `:500-512`
  routes both terminal subject families.
- semteams `cmd/semteams/main.go:912-942` installs the in-repository agentrun milestone terminal subscriber;
  `cmd/semteams/chainpause/subscriber.go:21,70-87` consumes failed events for product pause projection.
- semmachina `internal/stage/loopfailure.go:27-34,501-506` declares the failed-event consumer and decodes events.
- semteams `cmd/semteams/approvalpause/subscriber.go:15-38` declares the pending/response production subscriber
  families, and `cmd/semteams/main.go:393-416` resolves both configured subjects and starts the subscriber.

### Sister-repository dependency on the removed signal endpoint

- semteams `ui/src/lib/services/agentApi.ts:342-360` still implements sendSignal by posting
  `/loops/{id}/signal`; its non-2xx path throws AgentApiError at `:352-358`.
- semteams `ui/src/lib/components/layout/ChatBar.svelte:139` calls sendSignal for task slash commands and surfaces the
  thrown message at `:158-159`.
- semteams `ui/src/lib/components/board/TaskDetailPanel.svelte:91` calls sendSignal and stores the thrown message in
  signalError at `:92-93`.
- semteams `specs/openapi.v3.yaml:602-605` declares POST `/loops/{id}/signal` for pause, resume, and cancel.
- semteams `ui/src/lib/types/api.generated.ts:1408-1421` retains the generated signal path and POST operation.
- semteams `docs/ui-integration-notes.md:54-60` advertises the signal endpoint; `:136-137` directs approve and reject
  actions to it.

SemStreams' current contract records that the dispatch-local payload had no matching loop consumer and that the route
is absent:

- `openspec/specs/agentic-dispatch/spec.md:249` — `published a dispatch-local type — while the loop's only handler for the subject accepts the first and drops`
- `openspec/specs/agentic-dispatch/spec.md:250` — `anything else as an unexpected payload type. Both halves are closed here, and neither by repair:`
- `openspec/specs/agentic-dispatch/spec.md:257` — `loop was never signalled. Of its three verbs, only cancel was ever implemented on the loop side, and cancel is`
- `openspec/specs/agentic-dispatch/spec.md:293` — `#### Scenario: the loop signal endpoint is gone`
- `openspec/specs/agentic-dispatch/spec.md:295` — `- **GIVEN** dispatch's registered HTTP routes and its published OpenAPI document`
- `docs/operations/migration-beta162-to-beta163.md:915` — `Every request that names an existing loop — continue, cancel, approve, read — now passes one admission gate before`
- `docs/operations/migration-beta162-to-beta163.md:917` — `Sister repositories are **read-only** to SemStreams agents; every obligation is recorded here. Sweeps below were run`
- `docs/operations/migration-beta162-to-beta163.md:918` — `against each sister's working tree on 2026-09-01.`
- `docs/operations/migration-beta162-to-beta163.md:922` — `The route is gone. The same call now returns **404**.`

With no semteams change, sendSignal receives that 404, throws AgentApiError, ChatBar displays the failure as its
message error, and TaskDetailPanel stores it as signalError; no loop control publication occurs.

### Current published claims

- `openspec/specs/agentic-dispatch/spec.md:72` — `### Requirement: Loop existence and ownership are merged facts, never process memory alone`
- `openspec/specs/agentic-dispatch/spec.md:76` — `replacement, and the durable record may be absent for a live loop because persisting it is best-effort. A loop`
- `openspec/specs/agentic-dispatch/spec.md:99` — `- **GIVEN** a loop created before dispatch was replaced, whose `AGENT_LOOPS` record names its owner`
- `openspec/specs/agentic-dispatch/spec.md:114` — `- **GIVEN** an empty tracker and an `AGENT_LOOPS` read that fails with anything other than key absence`
- `openspec/specs/agentic-dispatch/spec.md:121` — `- **GIVEN** an empty loop tracker and an `AGENT_LOOPS` record whose loop is `awaiting_approval``
- `openspec/specs/agentic-dispatch/spec.md:324` — `- **Framework-published loop events** — loop-created, approval-pending, and terminal completion and failure`
- `openspec/specs/agentic-dispatch/spec.md:327` — `  approval-pending arrival buffer.`
- `docs/concepts/17-approval-flow.md:70` — `## Wiring an approval UI`
- `docs/concepts/17-approval-flow.md:77` — `// Subscribe to all pending approvals (or scope by loop_id).`
- `docs/concepts/17-approval-flow.md:78` — `sub, err := js.Subscribe("agent.approval_pending.>", func(msg *nats.Msg) {`
- `docs/concepts/17-approval-flow.md:107` — `js.Publish("agent.approval_response."+pending.LoopID, data)`
- `docs/concepts/17-approval-flow.md:162` — `The approval flow is a **coordination mechanism**, not an`
- `docs/operations/migration-beta21-to-beta22.md:57` — `in-memory tracker (populated by a new `agent.approval_pending.*``
- `docs/operations/migration-beta21-to-beta22.md:65` — `without it; only the HTTP approval endpoint requires the cache.`
- `docs/operations/migration-beta21-to-beta22.md:178` — `between then and the approval click), the handler returns 409 and`
- `docs/adr/081-graph-view-subscription.md:201` — `| Per-client serving surfaces | agentic-dispatch AGENT_LOOPS SSE (`http.go:902`); message-logger KV-watch SSE; semboids graphstream (sister repo); #211 MCP gateway (future) | **Convert — the primary win.** N per-client watchers → 1 view. First mover: the AGENT_LOOPS activity stream (in-repo, purest #579 shape) |`
- `docs/operations/38-agent-terminal-settlement.md:100` — `write never landed.`
- `docs/operations/38-agent-terminal-settlement.md:118` — `attempts while the source terminal remains retained. It does not mean`
- `docs/adr/101-coordinator-reply-vocabulary-and-workflow-terminal-delivery.md:43` — `resolution; a walk that ends at a record with no link and no route (a route-less bus-submitted root, or a hop`
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #759 — natsclient: establish semantic JetStream settlement as the restart-safety foundation
- #1244 — agentic-loop: adopt the StopAll exit contract for loop state
- PR #1159 (draft) — fix(agentic-loop): preserve durable work across process restart
- PR #1156 (draft) — refactor(natsclient): add semantic delivery settlement

## Searches

- `git grep -n '^## \(Purpose\|Product Boundary\)' -- openspec/project.md` → 2
- `gopls workspace_symbol -matcher=fuzzy LoopTracker` → FAILED (sandbox denied Go build-cache read)
- `gopls workspace_symbol -matcher=fuzzy LoopTracker` → 68
- `gopls workspace_symbol -matcher=fuzzy AutoContinue` → 4
- `gopls workspace_symbol -matcher=fuzzy PendingApproval` → 37
- `gopls workspace_symbol -matcher=fuzzy LoopEntity` → 73
- `git grep -n -e 'agent\.created' -e 'agent\.approval_pending' -e 'agent\.complete' -e 'agent\.failed' -- ':!**/*_test.go'` → 330
- `git grep -n -E 'func \(c \*Component\) (handleAgentCreated|handleAgentApprovalPending|handleTerminalDelivery|handleAgentComplete|handleAgentFailed|handleUserMessage|handleLoopApproval|handleActivity|handleLoops|handleLoop|setupSubscriptions)|LoopTracker|AutoContinue|AGENT_LOOPS|agentLoops|approval_pending' -- processor/agentic-dispatch ':!**/*_test.go'` → 81
- `gopls references processor/agentic-dispatch/loop_tracker.go:144:23` → 58
- `gopls references processor/agentic-dispatch/loop_tracker.go:205:23` → 15
- `gopls references processor/agentic-dispatch/loop_tracker.go:332:23` → 15
- `gopls references processor/agentic-dispatch/loop_tracker.go:393:23` → 6
- `gopls references processor/agentic-dispatch/loop_tracker.go:274:23` → 4
- `gopls references processor/agentic-dispatch/loop_tracker.go:282:23` → 5
- `gopls call_hierarchy processor/agentic-dispatch/component.go:1089:21` → 11 relations
- `gopls call_hierarchy processor/agentic-dispatch/loop_admission.go:337:21` → 8 relations
- `git grep -n -E 'LoopCreatedEvent|ApprovalPendingEvent|LoopCompletedEvent|LoopFailedEvent|ResolveSubject\([^\n]*"agent\.(created|approval_pending|complete|failed)"|publish.*(Created|Approval|Completion|Failure)|PublishToStream' -- processor/agentic-loop ':!**/*_test.go'` → 40
- `git grep -n -E 'ResolveSubject\([^\n]*"(agent\.task|agent\.signal|agent\.approval_response|user\.response)"|PublishToStream|buildTaskMessage|ApprovalResponse|SignalMessage|Cancel' -- processor/agentic-dispatch ':!**/*_test.go'` → 52
- `git grep -n -E 'GetKeyValueBucket|\.Get\(|Watch|graphview|loadPersistedLoop|agentLoopsBucket|agentLoopsPortName|COMPLETE_|LOOP_' -- processor/agentic-dispatch ':!**/*_test.go'` → 70
- `git grep -n -E '(hydrate|rehydrate|bootstrap|restore|rebuild|recover|replay|load).*LoopTracker|(LoopTracker|loopTracker).*(hydrate|rehydrate|bootstrap|restore|rebuild|recover|replay|load)|pendingApprovalBuffer.*(persist|durable|KV)|NewLoopTracker' -- processor/agentic-dispatch ':!**/*_test.go'` → 5
- `git grep -n -E 'KVWritePort|CreateKeyValue|Put\(|Update\(|Create\(|Bucket:|bucket|ObjectStore' -- processor/agentic-dispatch ':!**/*_test.go'` → 31
- `git grep -n -E 'KVWritePort|CreateKeyValue|\.Put\(|\.Create\(|\.Update\(' -- 'processor/agentic-dispatch/*.go' ':!**/*_test.go'` → 0
- `git grep -n -E 'activeLoops|recordLoopStarted|recordLoopEnded|loopTracker\.Count|pendingApproval|activityView|CompletionReceived|TerminalSettlement' -- processor/agentic-dispatch/metrics.go processor/agentic-dispatch/component.go processor/agentic-dispatch/terminal_settlement.go ':!**/*_test.go'` → 52
- `git grep -n -E '(^|[^0-9])6\.|task 6|dispatcher|dispatch|LoopTracker|approval_pending|restart|hydrate|shadow state|intermediate' -- openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/specs docs/adr 'docs/operations/migration-*.md'` → 740
- `git grep -n -E 'Loop existence and ownership|process memory|LoopTracker|agent.created|agent.approval_pending|AGENT_LOOPS|approval' -- openspec/specs/agentic-dispatch/spec.md docs/adr/053-agent-run-substrate.md docs/adr/081-graph-view-subscription.md docs/concepts/17-approval-flow.md docs/operations/migration-beta21-to-beta22.md docs/operations/migration-beta24-to-beta25.md processor/agentic-dispatch/README.md` → 78
- `gh issue list --repo C360Studio/semstreams --search 'dispatch LoopTracker restart' --state open --limit 100 --json number,title` → 0
- `gh issue list --repo C360Studio/semstreams --search 'approval pending dispatch' --state open --limit 100 --json number,title` → 5
- `gh issue list --repo C360Studio/semstreams --search 'dispatcher bridge agentic loop' --state open --limit 100 --json number,title` → 0
- `gh pr list --repo C360Studio/semstreams --state open --draft --limit 100 --json number,title,body` → 4
- `openspec list` → 2 active changes
- `gopls workspace_symbol -matcher=fuzzy 'graphview View'` → 95
- `git grep -n -E 'type View|func New|func \(v \*View|ErrNotReady|WatcherLost|SnapshotAndSubscribe|WaitCaughtUp|Restart|failClosed' -- pkg/graphview` → 109
- `git grep -n -E 'Catalog|OpenCatalogReader|summaryView|graphview\.New|WatcherLost|WaitCaughtUp|SnapshotAndSubscribe' -- graph processor/graph-query processor/agentic-dispatch/http_activity.go` → 104
- `git grep -n -E 'CommunitySummaries owns|OpenCatalogReader' -- graph/kvcatalog.go` → 18
- `git grep -n -E 'agent\.task|agent\.signal|agent\.approval_response|user\.response|agent\.response|tool\.result' -- processor/agentic-dispatch processor/agentic-loop ':!**/*_test.go'` → 217
- `git grep -n -E 'Track\(|UpdateWorkflow|UpdateContext|SetPendingApproval|ClearPendingApproval|GetActiveLoop|GetUserLoops|GetAllLoops|loopTracker' -- processor/agentic-dispatch ':!**/*_test.go'` → 94
- `git grep -n -E 'History:|TTL:|COMPLETE_|persistLoopState|storeCompletionRecord|storeFailureRecord|storeCancelRecord|KVWritePort' -- processor/agentic-loop ':!**/*_test.go'` → 46
- `git grep -n -E 'ContinuationAfterDispatchReplacement|ReadSeamsAfterDispatchReplacement|HandleListLoops|HandleGetLoop|NotAwaitingReturns409|PublishFailureRetainsPendingApproval|SettlementCrashRedelivery|RoutesToOriginAfterContinuation|ActivityStream|ActivityView' -- processor/agentic-dispatch/*_test.go` → 31
- `git -C /Users/coby/Code/c360/semteams grep -n -E 'listLoops|getLoop|submitApproval|EventSource|AutoContinue|StatusConflict|409' -- ui/src ui/e2e configs cmd/semteams` → 223
- `git -C /Users/coby/Code/c360/semteams grep -n -E 'listLoops|getLoop|submitApproval|EventSource|auto_continue|StatusConflict|409' -- ui/src/lib/services/agentApi.ts ui/src/lib/stores/agentStore.svelte.ts ui/src/lib/components/board/PendingApprovalSection.svelte ui/e2e/agentic/approval-resume.spec.ts configs/flow-bootstrap.json` → 24
- `git -C /Users/coby/Code/c360/semteams grep -n -E 'agent\.approval_(pending|response)' -- cmd/semteams/approvalpause/subscriber.go cmd/semteams/main.go` → 16
- `git -C /Users/coby/Code/c360/semteams grep -n -E 'agent\.(complete|failed)' -- cmd internal` → 34
- `git -C /Users/coby/Code/c360/semdev grep -n 'AutoContinue' -- configs` → 0
- `git -C /Users/coby/Code/c360/semdev grep -n -E 'auto_continue|AutoContinue' -- configs/semdev-live-gemini.json` → 1
- `git -C /Users/coby/Code/c360/semsage grep -n -E 'agent\.(complete|failed)' -- tools/spawn/executor.go` → 2
- `git -C /Users/coby/Code/c360/semdragon grep -n -E 'agent\.(complete|failed)' -- processor/questbridge/handler.go` → 7
- `git -C /Users/coby/Code/c360/semmachina grep -n -E 'agent\.(complete|failed)|LoopFailed' -- internal/stage/loopfailure.go` → 12
- `git grep -n -E 'AckPolicy|MaxDeliver|agent\.(complete|failed)' -- processor/agentic-dispatch/component.go agentic/agentrun/agentrun.go output/otel/span_collector.go` → 40
- `git grep -n -E 'persistLoopState|persistHandlerResult|loadPersistedLoop' -- processor/agentic-loop/approval_sweeper.go processor/agentic-loop/component.go processor/agentic-dispatch/terminal_settlement.go` → 24
- `git grep -n -E 'summaryView|OpenCatalogReader|EnsureCatalogBucket|CatalogReader|WatcherLost|SnapshotAndSubscribe|WaitCaughtUp|Restart|failClosed' -- pkg/graphview graph/kvcatalog.go processor/graph-query processor/agentic-dispatch/http_activity.go` → 214
- `git grep -n -E '24.?h|bucket-level|Loop current record|loopID.*Loop lifecycle|Best-effort duplicate suppression' -- docs/adr openspec/changes/agentic-loop-restart-safety docs/operations` → 43
- `git grep -n -E 'dispatch replacement|replacement|not awaiting approval|PublishFailure|crash redelivery|origin after continuation|watcher loss|StopsWithComponent|returns 409|Conflict' -- 'processor/agentic-dispatch/*_test.go'` → 20
- `git grep -n -E 'approval.*409|StatusConflict|pending.*retain|watcher.*lost|activity.*replay|terminal.*redeliver' -- 'processor/agentic-dispatch/*_test.go'` → 6
- `git grep -n -E '(hydrate|rehydrate|bootstrap|restore|rebuild|recover).*LoopTracker|LoopTracker.*(hydrate|rehydrate|bootstrap|restore|rebuild|recover)' -- processor/agentic-dispatch ':!**/*_test.go'` → 0
- `git grep -n -E 'KVWritePort|CreateKeyValueBucket|CreateOrUpdateKeyValueBucket' -- processor/agentic-dispatch ':!**/*_test.go'` → 0
- `git grep -n -E 'content-sha256|reviewed hash|pre-review' -- openspec/changes/*/inventory*.md` → 16
- `git grep -n 'pre-review-content-sha256' -- openspec/changes/*/inventory*.md` → 0
- `git grep -n -E 'LoopManager|CreateLoopWithID|GetLoop\(|UpdateLoop|DeleteLoop|WithRecovery|releaseLoopTransientState' -- processor/agentic-loop/state.go processor/agentic-loop/handlers.go processor/agentic-loop/trajectory_handler_wiring.go` → 155
- `git -C /Users/coby/Code/c360/semteams grep -n -E 'sendSignal|/loops/\{id\}/signal|loops/\$\{id\}/signal|SignalRequest|SignalResponse' -- ui/src/lib/services/agentApi.ts ui/src/lib/components/layout/ChatBar.svelte ui/src/lib/components/board/TaskDetailPanel.svelte specs/openapi.v3.yaml ui/src/lib/types/api.generated.ts docs/ui-integration-notes.md` → 18
- `git grep -n -E 'POST /loops/\{id\}/signal|loop signal endpoint|route is gone|same call now returns' -- openspec/specs/agentic-dispatch/spec.md docs/operations/migration-beta162-to-beta163.md` → 7
- `git -C /Users/coby/Code/c360/semteams grep -n 'sendSignal' -- '*.svelte'` → 2
- `git -C /Users/coby/Code/c360/semteams grep -n 'operationId: sendLoopSignal' -- '*.yaml' '*.yml'` → 0
- `git -C /Users/coby/Code/c360/semteams grep -n '/loops/{id}/signal' -- '*.yaml' '*.yml'` → 1
- `git -C /Users/coby/Code/c360/semteams ls-files | rg 'ChatBar\.svelte$|openapi.*ya?ml$'` → 3

### Refresh searches (2026-09-04)

- `git grep -n '^## \(Purpose\|Product Boundary\)' -- openspec/project.md` → 2
- `rg -n 'tasks\.md:(148|150|161|171|173|176|177|181|182)|agentic-dispatch/spec\.md:(60|64|67|91|98)|design\.md:339' openspec/changes/agentic-loop-restart-safety/inventory-dispatch-bridge-boundary-2026-09-04.md` → 15
- `git grep -n -E 'Approval continuation and dispatch projection|Approval evidence|projection|AutoContinue|incomplete hydration|explicit LoopID|Loop task, request|lane-scoped|at-least-once|edge gateway|agent.task|TaskID' -- openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → 113
- `git grep -n -E 'frozen-parent|Frozen parent|F =|F=|79b0f29|417beae|base:' -- openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/inventory-dispatch-bridge-boundary-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task-loop-cardinality-2026-09-04.md openspec/changes/agentic-loop-restart-safety/inventory-task2-stable-identity-2026-09-03.md` → 6
- `shasum -a 256 openspec/changes/agentic-loop-restart-safety/design-dispatch-edge-gateway-2026-09-04.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/proposal.md openspec/changes/agentic-loop-restart-safety/tasks.md` → 4
