# Inventory: agentic-loop restart safety after nested PR #1251
base: 09ba38b1de5e7200e72281c8e4b8941d81be1da2

## Checkpoint

- `openspec/project.md:5` — `SemStreams is the **semantic graph substrate and framework** for the C360 `sem*``
- `openspec/project.md:14` — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- `openspec/project.md:35` — `**Lifecycle harness** (ADR-049) — `Participant`/`Manager` current state over`
- `agentic/state_pause_removal_test.go:11` — `// TestLoopEntity_DecodesRecordsCarryingRemovedPauseKeys pins the compatibility`
- `agentic/state_pause_removal_test.go:13` — `// PauseRequested / PauseRequestedBy / StateBeforePause, and an AGENT_LOOPS record`
- `agentic/state_pause_removal_test.go:32` — `var got agentic.LoopEntity`
- `agentic/state_pause_removal_test.go:50` — `for _, key := range []string{"pause_requested", "pause_requested_by", "state_before_pause"} {`

## Claimed gap

### Surviving control-signal declaration and validation

- `agentic/user_types.go:30` — `SignalCancel = "cancel" // Stop execution immediately`
- `agentic/user_types.go:120` — `type UserSignal struct {`
- `agentic/user_types.go:122` — `Type        string    `json:"type"` // cancel`
- `agentic/user_types.go:132` — `func (s UserSignal) Validate() error {`
- `agentic/user_types.go:139` — `if !isValidSignalType(s.Type) {`
- `agentic/user_types.go:140` — `if redirect, removed := removedSignalTypes[s.Type]; removed {`
- `agentic/user_types.go:184` — `var validSignalTypes = []string{`
- `agentic/user_types.go:185` — `SignalCancel,`
- `agentic/user_types.go:194` — `var removedSignalTypes = map[string]string{`
- `agentic/user_types.go:195` — `"pause":    "",`
- `agentic/user_types.go:196` — `"resume":   "",`
- `agentic/user_types.go:197` — `"feedback": "",`
- `agentic/user_types.go:198` — `"retry":    "",`
- `agentic/user_types.go:199` — `"approve":  approvalRedirect,`
- `agentic/user_types.go:200` — `"reject":   approvalRedirect,`
- `agentic/user_types.go:208` — `const approvalRedirect = " — publish an ApprovalResponse on agent.approval_response.* instead (ADR-039)"`
- `agentic/user_types.go:210` — `func isValidSignalType(t string) bool {`
- `agentic/user_types.go:211` — `return slices.Contains(validSignalTypes, t)`

The production searches for `SignalPause`, `SignalResume`, `SignalApprove`, `SignalReject`, `SignalFeedback`,
`SignalRetry`, `handlePauseSignal`, `handleResumeSignal`, `PauseRequested`, `PauseRequestedBy`, `StateBeforePause`,
`pause_requested`, `pause_requested_by`, and `state_before_pause` returned zero results in `agentic`, `processor`,
`configs`, and generated schemas, excluding tests. The literal spellings survive in `removedSignalTypes`, tests,
migration text, archived change artifacts, and earlier inventories listed below.

### Signal subscription, handler, and raw settlement

- `processor/agentic-loop/config.go:408` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:409` — `Description: "Control signals for loops (cancel)",`
- `schemas/agentic-loop.v1.json:998` — `"name": "agent.signal",`
- `schemas/agentic-loop.v1.json:1004` — `"description": "Control signals for loops (cancel)",`
- `processor/agentic-loop/component.go:177` — `func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {`
- `processor/agentic-loop/component.go:179` — `handler(ctx, data)`
- `processor/agentic-loop/component.go:180` — `return nil`
- `processor/agentic-loop/component.go:897` — `case "agent.signal":`
- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:1032` — `handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/agentic-loop/component.go:1033` — `if err := handler(msgCtx, msg.Data()); err != nil {`
- `processor/agentic-loop/component.go:1038` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-loop/component.go:2095` — `// handleSignalMessage processes incoming signal messages. Cancel is the only`
- `processor/agentic-loop/component.go:2097` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2098` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/component.go:2104` — `signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)`
- `processor/agentic-loop/component.go:2119` — `case agentic.SignalCancel:`
- `processor/agentic-loop/component.go:2120` — `c.handleCancelSignal(ctx, signal)`
- `processor/agentic-loop/component.go:2121` — `default:`
- `processor/agentic-loop/component.go:2122` — `c.logger.Warn("Unsupported signal type",`
- `processor/agentic-loop/component.go:2129` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-loop/component.go:2141` — `entity, err := c.handler.CancelLoop(loopID, signal.UserID)`
- `processor/agentic-loop/component.go:2154` — `c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:2180` — `completionData, err := json.Marshal(completionMsg)`
- `processor/agentic-loop/component.go:2188` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)`
- `processor/agentic-loop/component.go:2193` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`
- `processor/agentic-loop/component.go:2208` — `c.persistCancellationState(ctx, loopID, &completion)`

### Approval-response lane retained separately

- `agentic/approval.go:104` — `type ApprovalResponse struct {`
- `agentic/constants.go:25` — `CategoryApprovalResponse = "approval_response"`
- `agentic/constants.go:31` — `ApprovalDecisionApprove = "approve"`
- `agentic/constants.go:32` — `ApprovalDecisionReject  = "reject"`
- `agentic/payload_registry.go:48` — `{Domain: Domain, Category: CategoryApprovalResponse, Version: SchemaVersion, Description: "Approval response from human-in-the-loop UI", Factory: func() any { return &ApprovalResponse{} }, IndexingProfile: control},`
- `processor/agentic-loop/config.go:412` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/component.go:899` — `case "agent.approval_response":`
- `processor/agentic-loop/component.go:900` — `handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)`
- `processor/agentic-loop/approval_response_handler.go:31` — `func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (result HandlerResult, err error) {`
- `processor/agentic-loop/approval_response_handler.go:58` — `pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID)`
- `processor/agentic-loop/approval_response_handler.go:161` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/approval_response_handler.go:167` — `respPtr, ok := baseMsg.Payload().(*agentic.ApprovalResponse)`
- `processor/agentic-loop/approval_response_handler.go:181` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_response_handler.go:200` — `c.persistHandlerResult(ctx, result)`

### Post-deletion component line movements from the accepted 2026-09-02 inventory

- `processor/agentic-loop/component.go:2097` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2120` — `c.handleCancelSignal(ctx, signal)`
- `processor/agentic-loop/component.go:2237` — `func (c *Component) handleToolCallVerdictMessage(_ context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2239` — `if dispatcher == nil {`
- `processor/agentic-loop/component.go:2243` — `payload, ok := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/component.go:2252` — `if decision == "" || callID == "" {`
- `processor/agentic-loop/component.go:2259` — `dispatcher.HandleVerdict(decision, callID, data)`

The accepted inventory's pause/resume branch pins at former `component.go:2218-2296` have no production targets at
this base. Its verdict-handler pins at former `component.go:2321-2343` map to `component.go:2237-2259` above.
The config port remains at `config.go:408-409`; its description and generated-schema description changed to
`cancel` only.

### Existing 17 in-scope physical durable-input lanes

| # | Physical input declaration | Current handler or settlement pin | Post-#1251 delta |
|---:|---|---|---|
| 1 | `processor/agentic-dispatch/config.go:118` — `Name: "user.message", Config: component.JetStreamPort{Subjects: []string{"user.message.>"}, StreamName: "USER"}, Required: true, External: true,` | `processor/agentic-dispatch/component.go:557` — `c.handleUserMessage(msgCtx, msg.Data())` | none in #1251 |
| 2 | `processor/agentic-dispatch/config.go:126` — `Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-dispatch/component.go:620` — `c.handleAgentCreated(msgCtx, msg.Data())` | none in #1251 |
| 3 | `processor/agentic-dispatch/config.go:134` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-dispatch/component.go:696` — `c.handleAgentApprovalPending(msgCtx, msg.Data())` | none in #1251 |
| 4 | `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-governance/component.go:316` — `msgType = MessageTypeTask` | none in #1251 |
| 5 | `processor/agentic-governance/config.go:193` — `Name: "request_validation", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-governance/component.go:319` — `msgType = MessageTypeRequest` | none in #1251 |
| 6 | `processor/agentic-governance/config.go:197` — `Name: "response_validation", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-governance/component.go:322` — `msgType = MessageTypeResponse` | none in #1251 |
| 7 | `processor/agentic-loop/config.go:408` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)` | pause/resume cases and handlers deleted; cancel/default survive |
| 8 | `processor/agentic-loop/config.go:412` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-loop/component.go:900` — `handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)` | none in #1251 |
| 9 | `processor/agentic-loop/config.go:416` — `Name: "agent.toolcall.approved", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.approved.>"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-loop/component.go:909` — `handler = adaptVoidInputHandler(c.handleToolCallVerdictMessage)` | handler moved to line 2237 |
| 10 | `processor/agentic-loop/config.go:420` — `Name: "agent.toolcall.rejected", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.rejected.>"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-loop/component.go:909` — `handler = adaptVoidInputHandler(c.handleToolCallVerdictMessage)` | handler moved to line 2237 |
| 11 | `processor/agentic-tools/config.go:127` — `Name: "tool.execute", Config: component.JetStreamPort{Subjects: []string{"tool.execute.>"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-tools/component.go:427` — `return c.handleToolDelivery(workCtx, data)` | none in #1251 |
| 12 | `processor/agentic-dispatch/config.go:122` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-dispatch/component.go:583` — `return c.handleTerminalDelivery(workCtx, data)` | none in #1251 |
| 13 | `processor/agentic-dispatch/config.go:130` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Required: false,` | `processor/agentic-dispatch/component.go:646` — `return c.handleTerminalDelivery(workCtx, data)` | none in #1251 |
| 14 | `processor/agentic-model/config.go:128` — `Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.>"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-model/component.go:403` — `c.handleRequest(workCtx, msg.Data())` | none in #1251 |
| 15 | `processor/agentic-loop/config.go:396` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-loop/component.go:892` — `handler = c.taskInputHandler(30 * time.Minute)` | none in #1251 |
| 16 | `processor/agentic-loop/config.go:400` — `Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.>"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-loop/component.go:894` — `handler = adaptVoidInputHandler(c.handleResponseMessage)` | none in #1251 |
| 17 | `processor/agentic-loop/config.go:404` — `Name: "tool.result", Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}, StreamName: "AGENT"}, Required: true,` | `processor/agentic-loop/component.go:896` — `handler = adaptVoidInputHandler(c.handleToolResultMessage)` | none in #1251 |

## Spellings of the fact

### State and persisted-key spellings

- `agentic/state.go:30` — `// LoopStatePaused remains legacy-valid and is accepted by the exported`
- `agentic/state.go:33` — `LoopStatePaused           LoopState = "paused"`
- `agentic/state.go:51` — `State              LoopState             `json:"state"``
- `agentic/state.go:69` — `CancelledBy string    `json:"cancelled_by,omitempty"` // User who cancelled the loop`
- `agentic/state.go:120` — `LoopStateFailed, LoopStateCancelled, LoopStatePaused,`
- `agentic/state.go:129` — `func (e *LoopEntity) TransitionTo(newState LoopState) error {`
- `agentic/state.go:131` — `if e.State == newState {`
- `agentic/state.go:135` — `if e.State.IsTerminal() {`
- `agentic/state.go:138` — `e.State = newState`
- `processor/agentic-loop/state.go:663` — `func (m *LoopManager) TransitionLoop(loopID string, newState agentic.LoopState) error {`
- `processor/agentic-loop/state.go:672` — `return entity.TransitionTo(newState)`
- `agentic/state_pause_removal_test.go:27` — `"pause_requested": true,`
- `agentic/state_pause_removal_test.go:28` — `"pause_requested_by": "user-7",`
- `agentic/state_pause_removal_test.go:29` — `"state_before_pause": "planning"`

`LoopEntity.TransitionTo` handles a same-state no-op, refuses a terminal current state, and otherwise assigns the
provided `LoopState`. `LoopManager.TransitionLoop` performs its loop lookup and delegates the supplied state to
`LoopEntity.TransitionTo`.

### Registered payload and rule-readable spellings

- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `agentic/rule_fields.go:290` — `// by construction, so it fails closed. `type` is the closed signal vocabulary —`
- `agentic/rule_fields.go:293` — `func (s *UserSignal) RuleFields() map[string]any {`
- `agentic/rule_fields.go:335` — `func (r *ApprovalResponse) RuleFields() map[string]any {`

### Surviving exported signal-shaped carriers outside `agent.signal.*` settlement

- `agentic/user_types.go:242` — `Actions []ResponseAction `json:"actions,omitempty"``
- `agentic/user_types.go:248` — `func (r UserResponse) Validate() error {`
- `agentic/user_types.go:261` — `if !isValidResponseType(r.Type) {`
- `agentic/user_types.go:262` — `return fmt.Errorf("type must be one of: text, status, result, error, prompt, stream")`
- `agentic/user_types.go:301` — `type ResponseAction struct {`
- `agentic/user_types.go:305` — `// Signal is the control signal to send if the action is clicked. The only`
- `agentic/user_types.go:306` — `// signal the loop handles is "cancel"; an approval affordance must publish`
- `agentic/user_types.go:307` — `// an ApprovalResponse on agent.approval_response.* instead (ADR-039).`
- `agentic/user_types.go:308` — `Signal string `json:"signal"``
- `processor/agentic-dispatch/intent_classifier.go:33` — `// ClassifiedIntent is the result of intent classification.`
- `processor/agentic-dispatch/intent_classifier.go:34` — `type ClassifiedIntent struct {`
- `processor/agentic-dispatch/intent_classifier.go:37` — `SignalType string     `json:"signal_type,omitempty"` // For signal intents`
- `processor/agentic-dispatch/intent_classifier.go:97` — `Respond with JSON: {"type": "<intent_type>", "loop_id": "<if applicable>", "signal_type": "<if signal: cancel>", "confidence": <0.0-1.0>}`, loopContext)`

`UserResponse.Actions` embeds `ResponseAction`; `UserResponse.Validate` validates response identity/channel/type and
does not read `Actions` or `ResponseAction.Signal`. `ClassifiedIntent.SignalType` is classifier output. Scoped
production searches found neither carrier reading, publishing, or settling the durable `agent.signal.*` lane; neither
carrier is the `UserSignal` validation vocabulary enumerated above.

### User-facing and normative spellings

- `agentic/README.md:73` — `| `UserSignal` | Control signal — `cancel` only; approve/reject travel as `ApprovalResponse` (ADR-039) |`
- `agentic/README.md:168` — `| `paused` | Legacy-valid; exported transitions accept it; no framework-owned pause signal or semantics (#1239) |`
- `docs/concepts/13-agentic-systems.md:118` — `| `paused` | No | Legacy-valid; exported transitions accept it; no framework-owned pause signal or semantics (#1239) |`
- `docs/concepts/13-agentic-systems.md:145` — `Signals are published to `agent.signal.{loop_id}` and processed by the loop orchestrator. `cancel` is the`
- `docs/concepts/13-agentic-systems.md:148` — `**Approval and rejection are not signals.** They travel as `ApprovalResponse` on`
- `docs/concepts/27-frontier-harness-mapping.md:118` — `operator-visible trajectory, and a writable cancel control (pause/resume were`
- `processor/agentic-dispatch/README.md:187` — `| `agent.signal.{loop_id}` | Publish | Signals (cancel) |`
- `processor/agentic-dispatch/intent_classifier.go:24` — `// IntentSignal sends a control signal. `cancel` is the whole vocabulary`
- `processor/agentic-dispatch/intent_classifier.go:90` — `- "signal": The user wants to control a loop (cancel)`
- `processor/agentic-dispatch/intent_classifier.go:97` — `Respond with JSON: {"type": "<intent_type>", "loop_id": "<if applicable>", "signal_type": "<if signal: cancel>", "confidence": <0.0-1.0>}`, loopContext)`
- `processor/agentic-loop/README.md:35` — `- **Signal Handling**: The `cancel` signal — the entire vocabulary (approval travels as `ApprovalResponse`)`
- `processor/agentic-loop/README.md:135` — `| agent.signal | jetstream | agent.signal.* | Control signals (cancel) |`
- `processor/agentic-loop/README.md:176` — `| `paused` | No | Legacy-valid; exported transitions accept it; no framework-owned pause signal or semantics (#1239) |`
- `processor/agentic-loop/doc.go:72` — `//   - paused: Legacy-valid and accepted by transition APIs; #1239 removes the`
- `processor/agentic-loop/doc.go:84` — `// The loop accepts control signals via the agent.signal.* input port:`
- `openspec/specs/agentic-dispatch/spec.md:246` — `Exactly one payload type MUST travel `agent.signal.<loop_id>`: `agentic.UserSignal`, wrapped in the standard`
- `openspec/specs/agentic-dispatch/spec.md:262` — `message as delivered** — the caller saw success and got nothing. **`cancel` MUST be the entire signal`
- `openspec/specs/agentic-dispatch/spec.md:265` — `unaffected: they travel as `ApprovalResponse` on `agent.approval_response.*` (ADR-039) and were never`
- `openspec/specs/agentic-dispatch/spec.md:302` — `#### Scenario: cancel is the whole vocabulary, and a removed verb is refused by name`
- `docs/operations/migration-beta162-to-beta163.md:969` — `Owner ruling on **#1239** (2026-09-02) deleted the surface rather than implement it. Pause is not wanted pre-v1.`
- `docs/operations/migration-beta162-to-beta163.md:973` — `- Signal verbs `pause` and `resume` (`agentic.SignalPause`, `agentic.SignalResume`).`
- `docs/operations/migration-beta162-to-beta163.md:974` — `- `LoopEntity` fields `PauseRequested`, `PauseRequestedBy`, `StateBeforePause` — and with them the persisted`
- `docs/operations/migration-beta162-to-beta163.md:1007` — `Owner ruling (2026-09-02): reconcile the vocabulary with the handler rather than carry the advertisement.`
- `docs/operations/migration-beta162-to-beta163.md:1008` — `**`SignalApprove`, `SignalReject`, `SignalFeedback` and `SignalRetry` are removed**, alongside`
- `docs/operations/migration-beta162-to-beta163.md:1012` — ``ApprovalResponse` on `agent.approval_response.*` (ADR-039), a different payload with a real handler. If you`

## Adjacent claims

- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/proposal.md:12` — `Issue #1239 — owner ruling option 1, 2026-09-02 — deleted the field, the two verbs, and both handlers. There is`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/proposal.md:32` — `a Tier 1 frozen package (`agentic`): the six signal verbs `SignalPause`, `SignalResume`, `SignalApprove`,`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/proposal.md:46` — `Nine exported symbols leave the Tier 1 frozen `agentic` package. Three owner rulings on #1239 authorize them.`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/proposal.md:72` — `transition APIs still accept it. This change removes the framework-owned pause/resume signal path and pause`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/specs/agentic-dispatch/spec.md:61` — `#### Scenario: cancel is the whole vocabulary, and a removed verb is refused by name`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/tasks.md:5` — `- [x] 1.1 Restate the `One control-signal payload travels the loop signal subject` requirement so the pause/resume`
- `openspec/changes/archive/2026-09-03-retire-loop-pause-resume/tasks.md:34` — `the semsage obligation. The owner's follow-up ruling explicitly authorizes `LoopEntity.StateBeforePause``
- `docs/adr/053-agent-run-substrate.md:280` — `markdown/`write_artifact` (ADR-038 D6); pause/resume semantics (ADR-037);`
- `openspec/changes/agentic-loop-restart-safety/inventory.md:78` — `Dispatch publishes; loop consumes. The void adapter covers pause/cancel/resume`
- `openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md:179` — `- `processor/agentic-loop/component.go:2239` — `entity.PauseRequested = true``
- `openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md:180` — `- `processor/agentic-loop/component.go:2280` — `entity.PauseRequested = false``
- `openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md:187` — `registered `agentic.UserSignal` handled by agentic-loop. Pause and resume fields/handlers remain live pending the ruled,`
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart — OPEN — beta.163
- #1239 — agentic-loop: pause/resume are advertised and unimplemented — PauseRequested is written twice, read never, and its comment promises a checkpoint that does not exist — OPEN — beta.163
- #1244 — agentic-loop: adopt the StopAll exit contract for loop state — two silent stalls leave a loop wedged with no transition and no observer — OPEN — beta.165
- #1249 — agentrun: make milestone fanout settlement replay-safe without partial ACK — OPEN — no milestone
- PR #1156 — refactor(natsclient): add semantic delivery settlement — OPEN DRAFT — `main` ← `codex/gh759-semantic-settlement`
- PR #1159 — fix(agentic-loop): preserve durable work across process restart — OPEN DRAFT — `codex/gh759-semantic-settlement` ← `codex/gh1146-agentic-loop-restart`
- PR #1251 — fix(agentic)!: cancel is the whole signal vocabulary — delete six verbs no handler read — MERGED into PR #1159 at `09ba38b1de5e7200e72281c8e4b8941d81be1da2`
- OpenSpec active changes: `agentic-loop-restart-safety` (8/87 tasks); `semantic-jetstream-settlement` (44/67 tasks).

## Consumers

### Signal payload and subject consumers

- `processor/agentic-dispatch/commands.go:156` — `signal := &agentic.UserSignal{`
- `processor/agentic-dispatch/commands.go:158` — `Type:        agentic.SignalCancel,`
- `processor/agentic-dispatch/commands.go:175` — `subject, err := component.ResolveSubject(c.outputPortDefs(), signalOutputPortName, targetLoopID)`
- `processor/agentic-dispatch/commands.go:179` — `if err := c.natsClient.Publish(ctx, subject, signalData); err != nil {`
- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:2104` — `signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)`
- `processor/agentic-loop/component.go:2120` — `c.handleCancelSignal(ctx, signal)`
- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `agentic/rule_fields.go:293` — `func (s *UserSignal) RuleFields() map[string]any {`
- `test/e2e/scenarios/agentic/approval_signal.go:342` — `signal, ok := baseMsg.Payload().(*agentic.UserSignal)`

### Approval-response producers and consumers

- `processor/agentic-dispatch/http.go:788` — `subject, err := c.publishApprovalResponse(ctx, loopID, callID, &req, approver)`
- `processor/agentic-dispatch/http.go:843` — `func (c *Component) publishApprovalResponse(ctx context.Context, loopID, callID string, req *ApprovalRequest, approver string) (string, error) {`
- `processor/agentic-dispatch/http.go:847` — `response := &agentic.ApprovalResponse{`
- `processor/agentic-dispatch/http.go:862` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.approval_response", loopID)`
- `processor/agentic-loop/approval_sweeper.go:77` — `response := agentic.ApprovalResponse{`
- `processor/agentic-loop/approval_sweeper.go:86` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_sweeper.go:124` — `func (c *Component) publishApprovalResponseToWire(ctx context.Context, response agentic.ApprovalResponse) {`
- `processor/agentic-loop/approval_sweeper.go:138` — `subject, err := component.ResolveSubject(inputs, "agent.approval_response", response.LoopID)`
- `processor/agentic-loop/component.go:900` — `handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)`
- `processor/agentic-loop/approval_response_handler.go:181` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/state.go:435` — `func (m *LoopManager) ResolveApprovalIfPending(loopID, callID string) (agentic.PendingApprovalState, bool, error) {`

## Problem shape

- `processor/agentic-loop/component.go:177` — `func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {`
- `processor/agentic-loop/component.go:180` — `return nil`
- `processor/agentic-loop/component.go:1033` — `if err := handler(msgCtx, msg.Data()); err != nil {`
- `processor/agentic-loop/component.go:1034` — `_ = msg.Nak()`
- `processor/agentic-loop/component.go:1038` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/agentic-loop/component.go:2121` — `default:`
- `processor/agentic-loop/component.go:2122` — `c.logger.Warn("Unsupported signal type",`
- `agentic/user_types.go:139` — `if !isValidSignalType(s.Type) {`
- `agentic/user_types.go:140` — `if redirect, removed := removedSignalTypes[s.Type]; removed {`
- `processor/agentic-loop/approval_response_handler.go:164` — `c.logger.Error("Failed to decode approval response", "error", err)`
- `processor/agentic-loop/approval_response_handler.go:165` — `return`
- `processor/agentic-loop/approval_response_handler.go:183` — `c.logger.Error("Failed to handle approval response",`
- `processor/agentic-loop/approval_response_handler.go:187` — `return`
- `processor/agentic-loop/component.go:2239` — `if dispatcher == nil {`
- `processor/agentic-loop/component.go:2240` — `return`
- `processor/agentic-loop/component.go:2243` — `payload, ok := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/component.go:2247` — `return`
- `processor/agentic-loop/component.go:2252` — `if decision == "" || callID == "" {`
- `processor/agentic-loop/component.go:2253` — `c.logger.Warn("Tool-call verdict payload missing decision or call_id; ignoring",`

## Searches

- `git rev-parse HEAD` → `09ba38b1de5e7200e72281c8e4b8941d81be1da2`.
- `git status --short` → one pre-existing untracked approved design reconciliation file; no inventory file yet.
- `git grep -n '^## \\(Purpose\\|Product Boundary\\)' -- openspec/project.md` → 2 hits.
- `git diff --stat b755e4ff..HEAD` → 24 files, 534 insertions, 298 deletions.
- `git diff --name-status b755e4ff..HEAD` → 24 paths.
- `git grep -n '^## ' -- openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-02-F.md` → 8 hits.
- `git grep -n 'Signal\\|pause\\|resume\\|approval\\|17\\|durable-input' -- openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-02-F.md` → 58 hits.
- `gopls workspace_symbol -matcher=fuzzy UserSignal` → 0.
- `gopls workspace_symbol -matcher=fuzzy SignalCancel` → 0.
- `gopls workspace_symbol -matcher fuzzy UserSignal` → 0.
- `gopls workspace_symbol -matcher=fuzzy ApprovalResponse` → 0.
- `gopls workspace_symbol -matcher=fuzzy handleSignalMessage` → 0.
- `gopls workspace_symbol -matcher=fuzzy PauseRequested` → 0.
- `gopls workspace_symbol -matcher=fuzzy handlePauseSignal` → 0.
- `gopls workspace_symbol -matcher=fuzzy LoopStatePaused` → 0.
- `gopls references agentic/user_types.go:120:6` → 6 references, declaration excluded.
- `gopls references -d agentic/user_types.go:120:6` → 7 results including declaration.
- `gopls references -d agentic/user_types.go:30:2` → 2 results including declaration.
- `gopls references -d processor/agentic-loop/component.go:2097:21` → declaration plus binding at `component.go:898`.
- `gopls call_hierarchy processor/agentic-loop/component.go:2097:21` → caller `setupSubscriptions`; callee `handleCancelSignal`; standard-library logging/formatting callees.
- `gopls references -d processor/agentic-loop/component.go:2129:21` → declaration plus call at `component.go:2120`.
- `gopls references -d processor/agentic-loop/approval_response_handler.go:161:21` → declaration only.
- `gopls references -d agentic/approval.go:104:6` → 7 same-file results including declaration.
- `gopls call_hierarchy processor/agentic-loop/approval_response_handler.go:161:21` → declaration plus standard-library logging/formatting callees; no caller returned.
- `gopls references -d agentic/state.go:33:2` → declaration plus validity-list reference at `state.go:120`.
- `gopls references -d agentic/state.go:51:2` → 15 same-package results including declaration.
- `gopls references -d agentic/state.go:129:22` → declaration only.
- `gopls call_hierarchy agentic/state.go:129:22` → `IsTerminal` and `fmt.Errorf` callees; no caller returned.
- `gopls references -d processor/agentic-loop/state.go:663:23` → declaration only.
- `gopls call_hierarchy processor/agentic-loop/state.go:663:23` → lock/unlock and `fmt.Errorf` callees; no caller returned.
- `gopls references -d agentic/user_types.go:242:2` → declaration only.
- `gopls references -d agentic/user_types.go:301:6` → declaration and `UserResponse.Actions` embedding.
- `gopls references -d agentic/user_types.go:308:2` → declaration only.
- `gopls references -d processor/agentic-dispatch/intent_classifier.go:37:2` → declaration only.
- `git grep -n -E 'type UserSignal|Signal(Cancel|Pause|Resume|Approve|Reject|Feedback|Retry)|signal_type|User control signal|Unsupported signal type|handleSignalMessage|handleCancelSignal|handlePauseSignal|handleResumeSignal|PauseRequested|PauseRequestedBy|StateBeforePause' -- '*.go' '*.md' '*.json' ':!openspec/changes/agentic-loop-restart-safety/**'` → 67 output lines.
- `git grep -n -E 'agent\\.signal|agent\\.approval_response|signal_subject|approval_response_subject|SignalSubject|ApprovalResponseSubject' -- agentic processor configs schemas openspec/specs docs/adr docs/operations ':!openspec/changes/agentic-loop-restart-safety/**'` → 65 output lines.
- `git grep -n -E 'UserSignal|SignalCancel|validSignalTypes|removedSignalTypes|approvalRedirect|isValidSignalType' -- agentic processor test openspec/specs docs/adr docs/operations ':!openspec/changes/agentic-loop-restart-safety/**'` → 121 output lines.
- `git grep -n -E 'SignalPause|SignalResume|SignalApprove|SignalReject|SignalFeedback|SignalRetry' -- agentic processor test schemas openspec/specs docs/adr ':!**/*_test.go'` → 0.
- `git grep -n -E 'handlePauseSignal|handleResumeSignal' -- agentic processor ':!**/*_test.go'` → 0.
- `git grep -n -E 'PauseRequested|PauseRequestedBy|StateBeforePause' -- agentic processor ':!**/*_test.go'` → 0.
- `git grep -n -E 'pause_requested|pause_requested_by|state_before_pause' -- agentic processor configs schemas ':!**/*_test.go'` → 0.
- `git grep -n -E 'type ApprovalResponse|CategoryApprovalResponse|HandleApprovalResponse|handleApprovalResponseMessage|ResolveApprovalIfPending|persistHandlerResult|agent\\.approval_response|ApprovalResponse' -- agentic processor/agentic-loop processor/agentic-dispatch payloadbuiltins openspec/specs docs/adr docs/operations ':!**/*_test.go' ':!openspec/changes/agentic-loop-restart-safety/**'` → 106 output lines.
- `git grep -n -E 'Name: "(user.message|agent.created|agent.approval_pending|agent.complete|agent.failed|task_validation|request_validation|response_validation|tool.execute|agent.request|agent.task|agent.response|tool.result|agent.signal|agent.approval_response|agent.toolcall.approved|agent.toolcall.rejected)"' -- processor/agentic-dispatch/config.go processor/agentic-governance/config.go processor/agentic-tools/config.go processor/agentic-model/config.go processor/agentic-loop/config.go` → 29 hits, including output-port declarations.
- `git grep -n -E 'handle(UserMessage|AgentCreated|AgentApprovalPending)|handleTerminalDelivery|handleMessage\\(msgCtx|handleToolDelivery|handleRequest\\(msgCtx|adaptVoidInputHandler\\(c\\.(handleResponseMessage|handleToolResultMessage|handleSignalMessage|handleApprovalResponseMessage|handleToolCallVerdictMessage)|taskInputHandler' -- processor/agentic-dispatch/component.go processor/agentic-governance/component.go processor/agentic-tools/component.go processor/agentic-model/component.go processor/agentic-loop/component.go` → 24 hits.
- `git grep -n -E 'func \\(c \\*Component\\) (handleTaskMessage|handleResponseMessage|handleToolResultMessage|handleSignalMessage|handleApprovalResponseMessage|handleToolCallVerdictMessage|handleMessage|handleUserMessage|handleRequest|handleToolCall)|case "(agent.task|agent.response|tool.result|agent.signal|agent.approval_response|agent.toolcall.approved|task|request|response|user.message|agent.created|agent.approval_pending)"|ConsumeStreamWithConfigContexts|consumeLongRunningInput' -- processor/agentic-loop processor/agentic-dispatch processor/agentic-model processor/agentic-tools processor/agentic-governance` → 27 output lines.
- `git grep -n -E 'baseMsg, err := c\\.decoder\\.Decode\\(data\\)|c\\.handler\\.CancelLoop|c\\.persistLoopState\\(ctx, loopID\\)|completionData, err := json\\.Marshal|ResolveSubject\\(c\\.config\\.Ports\\.Outputs, "agent\\.complete"|PublishToStream\\(ctx, subject, completionData\\)|c\\.persistCancellationState' -- processor/agentic-loop/component.go` → 11 hits.
- `git grep -n -E 'func \\(c \\*Component\\) handleToolCallVerdictMessage|dispatcher == ""|decodeVerdictPayload\\(c\\.decoder|decision == ""|dispatcher\\.HandleVerdict' -- processor/agentic-loop/component.go` → 5 intended hits; the recorded command used `dispatcher == nil` and returned the five pins above.
- `git grep -n -E '#1239|cancel is the whole|cancel.*whole vocabulary|pause/resume|pause and resume|ApprovalResponse|agent\\.approval_response' -- agentic/README.md processor/agentic-loop/README.md processor/agentic-loop/doc.go processor/agentic-loop/config.go processor/agentic-dispatch/README.md processor/agentic-dispatch/intent_classifier.go docs/concepts/13-agentic-systems.md docs/concepts/27-frontier-harness-mapping.md docs/adr openspec/specs/agentic-dispatch/spec.md openspec/changes/archive/2026-09-03-retire-loop-pause-resume docs/operations/migration-beta162-to-beta163.md` → 55 output lines.
- `git grep -n -iE 'cancel[,/ ].{0,12}pause|pause[,/ ].{0,12}resume|signals? \\(cancel' -- '*.go' '*.md' '*.json'` → 34 output lines; scoped matches include corrected/deletion statements, earlier inventories, migration text, and unrelated graph-worker pause/resume coordination.
- `git grep -n -E '^[[:space:]]*(\\||[-*]|//)[[:space:]]*-?[[:space:]]*`?(pause|resume|approve|reject|feedback|retry)`?[[:space:]]*(\\||:)' -- '*.go' '*.md' '*.json'` → 1 hit (`pkg/errs/doc.go` retry classifier), outside the signal surface.
- `git grep -n -E '"(pause|resume|approve|reject|feedback|retry)"' -- agentic processor/agentic-loop processor/agentic-dispatch configs schemas ':!**/*_test.go'` → 20 output lines; six removed spellings occur in `removedSignalTypes`; other hits are approval decisions, retry configuration, and unrelated prompt vocabulary.
- `git grep -n -E '#1239|pause|resume|Signal(Pause|Resume|Approve|Reject|Feedback|Retry)|PauseRequested|StateBeforePause|handlePauseSignal|handleResumeSignal|agent\\.signal|approval_response' -- openspec/changes/agentic-loop-restart-safety openspec/changes/semantic-jetstream-settlement ':!openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-02-F.md' ':!openspec/changes/agentic-loop-restart-safety/design-reconciliation-F-2026-09-02.md'` → 17 output lines.
- `git grep -n -E 'LoopStatePaused|State[[:space:]]+LoopState|func \\(e \\*LoopEntity\\) TransitionTo|e\\.State = newState|func \\(m \\*LoopManager\\) TransitionLoop|entity\\.TransitionTo\\(newState\\)' -- agentic/state.go processor/agentic-loop/state.go` → 8 hits.
- `git grep -n -E 'Actions \\[\\]ResponseAction|type ResponseAction|Signal string|func \\(r UserResponse\\) Validate|r\\.Actions|action\\.Signal|\\.Actions' -- agentic/user_types.go agentic/user_types_test.go agentic/rule_fields.go processor test ':!openspec/**'` → 62 output lines; 56 are unrelated rule/E2E action collections.
- `git grep -n -E 'type ClassifiedIntent|SignalType|signal_type' -- processor/agentic-dispatch ':!**/*_test.go'` → 3 production hits.
- `git grep -n -E 'SignalType|signal_type' -- processor/agentic-dispatch` → 6 hits: 2 production, 4 tests.
- `git grep -n -E 'ResponseAction|Actions:[[:space:]]*\\[\\]ResponseAction|\\.Actions\\[[^]]*\\]\\.Signal|\\.Actions' -- agentic processor/agentic-dispatch processor/agentic-loop ':!**/*_test.go'` → 4 hits: the README, embedding, type comment, and type declaration.
- `git grep -n -E 'agent\\.signal|UserSignal|SignalCancel' -- processor/agentic-dispatch/intent_classifier.go agentic/user_types.go` → 12 hits, all in `agentic/user_types.go`.
- `git grep -n -E 'agent\\.signal|UserSignal|SignalCancel' -- processor/agentic-dispatch/intent_classifier.go` → 0.
- `git grep -n -E 'r\\.Actions|action\\.Signal|Actions\\[[^]]*\\]\\.Signal' -- agentic processor ':!**/*_test.go'` → 0.
- `git grep -n -E 'ResponseAction|Actions \\[\\]ResponseAction|Signal string|func \\(r UserResponse\\) Validate' -- agentic/user_types.go` → 5 hits.
- `gh issue list --search 'repo:C360Studio/semstreams signal pause resume restart settlement agentic' --state open --json number,title --limit 100` → #1239.
- `gh issue view 1146 --json number,title,state,labels,milestone,body,url` → OPEN, beta.163.
- `gh issue view 1239 --json number,title,state,labels,milestone,body,url` → OPEN, beta.163.
- `gh issue view 1244 --json number,title,state,labels,milestone,body,url` → OPEN, beta.165.
- `gh issue view 1249 --json number,title,state,labels,milestone,body,url` → OPEN, no milestone.
- `gh pr view 1156 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url` → OPEN DRAFT.
- `gh pr view 1159 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url` → OPEN DRAFT.
- `gh pr view 1251 --json number,title,state,isDraft,baseRefName,headRefName,body,mergeCommit,url` → MERGED at this base.
- `openspec list` → 2 active changes.
- `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-03-post-1251.md` → PASS: `pins=181 ok=181 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`.

## Verification

PASS — 181/181 pins verified against the unchanged base and head.
