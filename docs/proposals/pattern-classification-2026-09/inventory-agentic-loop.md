# Inventory: #1234 slice agentic-loop
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #1035 — agentic-loop: a task rejected at preflight notifies nobody — routing fields carried 'for error notifications' go unused
Named sites
- `processor/agentic-loop/component.go:1161` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1164` — `c.logger.Error("Failed to unmarshal BaseMessage", "error", err)`
- `processor/agentic-loop/component.go:1170` — `c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))`
- `processor/agentic-loop/component.go:1173` — `related, hasLineage, err := c.preflightDecodedTask(task)`
- `processor/agentic-loop/component.go:1176` — `c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)`
- `processor/agentic-loop/component.go:33` — `taskIntakeRejectionLane   = "decoded-task"`
- `processor/agentic-loop/component.go:34` — `taskIntakeRejectionReason = "structural-invalid"`
- `processor/agentic-loop/component.go:1310` — `func (c *Component) preflightDecodedTask(task *agentic.TaskMessage) (map[string]any, bool, error) {`
- `natsclient/heartbeat.go:125` — `if termErr := msg.Term(); termErr != nil {`
- `natsclient/consume_durable.go:42` — `slog.Warn("ConsumeDurable handler error",`
- `agentic/user_types.go:283` — `// User routing info (optional, for error notifications)`
- `processor/agentic-loop/handlers.go:505` — `if err := h.loopManager.SetUserContext(loopID, task.ChannelType, task.ChannelID, task.UserID); err != nil {`
- `processor/agentic-loop/handlers.go:2116` — `ChannelType: entity.ChannelType,`
- `processor/agentic-loop/handlers.go:2700` — `ChannelType:  entity.ChannelType,`
All body line numbers drifted from the cited base commit `7b6ff1e1` by small, function-local offsets; every named function and site is present and located by text.

Refusal and observation
- `processor/agentic-loop/component.go:1312` — `return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "validate decoded task")`
- `processor/agentic-loop/component.go:1316` — `return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "decode related_loops metadata")`
- `processor/agentic-loop/component.go:1331` — `return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "construct prospective lineage subject")`
- `processor/agentic-loop/component.go:1334` — `return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "preflight prospective lineage batch")`
All four are uncoded (`WrapInvalid`, no `Classified*`); the caller (`:1173-1178`) discards the classification into `natsclient.TerminateDelivery(err)` and one compile-time-constant metric pair, never a `Classified`/`ClassifiedCode` return. No `slog`/`logger` emission at the rejection site itself (the only logging is the generic transport-layer `Warn` at `consume_durable.go:42`, which never sees `task_id`).

Nearest pattern instance
- `processor/agentic-loop/component.go:2063` — `return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("trajectory not found: %w", err))`
This is inside `handleTrajectoryQueryWithMaxPayload`, a NATS request/reply handler in the same package that returns a `ClassifiedError` the caller can observe over the wire, unlike `preflightDecodedTask`'s `WrapInvalid` which is converted to `TerminateDelivery` and never reaches the submitter.

## #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
Named sites
- `processor/agentic-loop/state.go:61` — `type LoopManager struct {`
- `processor/agentic-loop/state.go:68` — `cachedMetadata       map[string]map[string]any           // loopID -> metadata (domain context, not persisted)`
- `processor/agentic-loop/state.go:69` — `cachedRequestTimeout map[string]string                   // loopID -> request timeout (from TaskMessage.Timeout, not persisted)`
- `processor/agentic-loop/context_manager.go:45` — `type ContextManager struct {`
- `processor/agentic-loop/context_manager.go:75` — `func NewContextManager(loopID, model string, config ContextConfig, opts ...ContextManagerOption) *ContextManager {`
- `processor/agentic-loop/component.go:469` — `func (c *Component) Start(ctx context.Context) (startErr error) {`
- `processor/agentic-loop/component.go:894` — `handler = adaptVoidInputHandler(c.handleResponseMessage)`
- `processor/agentic-loop/component.go:1808` — `c.logger.Warn("No loop found for tool call", "call_id", toolResult.CallID)`
- `processor/agentic-loop/component.go:1810` — `c.metrics.recordToolResultDropped("stale_callid")`
  - Superseded since this inventory's base `32aeddf7`: the reader routes on framework execution
    identity, so the emitted reason is now `"stale_execution"` (`stable-request-identity`, #1328).
    The pin above is retained unedited as the 2026-09 evidence; `stale_callid` is no longer a label
    this tree emits, and no alert should be written against it.
- `processor/agentic-loop/approval_response_handler.go:161` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:1991` — `func (c *Component) persistCancellationState(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) {`
- `docs/concepts/03-streams-vs-kv-watches.md:184` — ``AGENT_LOOPS` stores the current state of each loop entity: which phase it's in, how many`
- `docs/concepts/17-approval-flow.md:65` — `- **Restart-safe.** `LoopEntity.PendingApproval` lives in the`
- `docs/concepts/27-frontier-harness-mapping.md:117` — `- The loop is a Lifecycle Participant (ADR-049): current-state restart hydration,`
All cited ranges hold at the current base with only small line drift (e.g. `component.go:894` and the three docs pins are byte-identical to the original citation). `component.go:1759-1787` in the original citation is the tail of `runWithBudget`'s select block, not the ToolResult-correlation site; the correlation-missing return the issue describes is inside `handleToolResultMessage`, now at `:1808-1810`, located by text (`"No loop found for tool call"`).

Refusal and observation
- `processor/agentic-loop/component.go:521` — `return errs.Wrap(err, "agentic-loop", "Start", "initialize KV buckets")`
- `processor/agentic-loop/component.go:526` — `return errs.Wrap(err, "agentic-loop", "Start", "setup subscriptions")`
- `processor/agentic-loop/component.go:532` — `return errs.Wrap(err, "agentic-loop", "Start", "resolve trajectory query input")`
- `processor/agentic-loop/component.go:542` — `return errs.Wrap(err, "agentic-loop", "Start", "subscribe to trajectory query")`
- `processor/agentic-loop/component.go:553` — `return errs.Wrap(err, "agentic-loop", "Start", "subscribe to in-flight query")`
- `processor/agentic-loop/approval_response_handler.go:164` — `c.logger.Error("Failed to decode approval response", "error", err)`
- `processor/agentic-loop/approval_response_handler.go:169` — `c.logger.Error("Unexpected approval response payload type",`
- `processor/agentic-loop/approval_response_handler.go:183` — `c.logger.Error("Failed to handle approval response",`
- `processor/agentic-loop/component.go:1998` — `c.logger.Error("Failed to marshal cancellation state", "error", err, "loop_id", loopID)`
- `processor/agentic-loop/component.go:2004` — `c.logger.Error("Failed to persist cancellation state", "error", err, "loop_id", loopID)`
`Start`'s failures are uncoded `errs.Wrap` propagated as boot failure (observed at the process level). `handleApprovalResponseMessage`, `handleToolResultMessage`'s correlation-miss, and `persistCancellationState` are all void-returning: `logger.Error`/`logger.Warn` only, no `errs.` classification, no metric on the approval or cancellation-persistence paths (the tool-result path does increment `recordToolResultDropped`).

Nearest pattern instance
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
`CreateLoopWithID` is this package's own create-vs-exists shape (`ErrLoopAlreadyExists`/`ErrLoopTerminal`/`ErrLoopBusy` sentinels declared at `state.go:17-49`, explicitly modeled on `pkg/lifecycle/errors.go`'s create-vs-exists contract per its own doc comment).

## #1239 — agentic-loop: pause/resume are advertised and unimplemented — PauseRequested is written twice, read never, and its comment promises a checkpoint that does not exist
Named sites
- `processor/agentic-loop/component.go:2218` — `func (c *Component) handlePauseSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-loop/component.go:2239` — `entity.PauseRequested = true`
- `processor/agentic-loop/component.go:2258` — `func (c *Component) handleResumeSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-loop/component.go:2280` — `entity.PauseRequested = false`
- `agentic/state.go:66` — `// Pause requested, will pause at next checkpoint`
- `agentic/state.go:67` — `// User who requested pause`
- `processor/agentic-loop/component.go:2132` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) {`
- `processor/agentic-dispatch/commands.go:21` — `c.registry.Register("cancel", CommandConfig{`
- `processor/agentic-dispatch/commands.go:29` — `c.registry.Register("status", CommandConfig{`
- `processor/agentic-dispatch/commands.go:37` — `c.registry.Register("loops", CommandConfig{`
- `processor/agentic-dispatch/commands.go:45` — `c.registry.Register("help", CommandConfig{`
`agentic/state.go:66-67` and `processor/agentic-dispatch/commands.go:21,29,37,45` are byte-identical to the original citation; `component.go` sites shifted by a uniform +14 lines. Re-swept whole-tree (`grep -rn "PauseRequested" --include="*.go" .` minus `_test.go`/`openspec/`): still exactly two writes (`component.go:2239`, `:2280`) and two declarations (`agentic/state.go:66-67`) — zero reads, matching the issue's claim at the new line numbers.

Refusal and observation
- `processor/agentic-loop/component.go:2224` — `c.logger.Error("Failed to get loop for pause",`
- `processor/agentic-loop/component.go:2232` — `c.logger.Warn("Cannot pause loop",`
- `processor/agentic-loop/component.go:2243` — `c.logger.Error("Failed to update loop state",`
- `processor/agentic-loop/component.go:2252` — `c.logger.Info("Pause requested for loop",`
No `errs.` call and no metric increment anywhere in `handlePauseSignal`/`handleResumeSignal` — only `logger.Error`/`Warn`/`Info`.

Nearest pattern instance
- `processor/agentic-loop/component.go:2162` — `c.metrics.recordLoopFailed("cancelled", entity.Iterations, duration)`
`handleCancelSignal`, the sibling signal handler in the same file, records a metric and publishes a completion event on its real transition (`:2132-2170`) — the pause/resume handlers have neither.

## #1244 — agentic-loop: adopt the StopAll exit contract for loop state — two silent stalls leave a loop wedged with no transition and no observer
Named sites
- `processor/agentic-loop/component.go:1879` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:894` — `handler = adaptVoidInputHandler(c.handleResponseMessage)`
- `processor/agentic-loop/handlers.go:1163` — `_ = h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed)`
- `processor/agentic-loop/handlers.go:1969` — `if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); err != nil {`
- `processor/agentic-loop/handlers.go:2233` — `_ = h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed)`
- `processor/agentic-loop/handlers.go:2482` — `if transitionErr := h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); transitionErr != nil {`
- `processor/agentic-loop/state.go:200` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:225` — `m.loops[loopID] = &entity`
- `processor/agentic-loop/state.go:226` — `m.pendingTools[loopID] = make(map[string]bool)`
- `processor/agentic-loop/handlers.go:904` — `if _, err = h.trajectoryManager.startTrajectory(loopID); err != nil {`
- `processor/agentic-loop/handlers.go:1054` — `return HandlerResult{}, err`
- `processor/agentic-loop/handlers.go:1070` — `return HandlerResult{}, err`
- `processor/agentic-loop/handlers.go:1074` — `return HandlerResult{}, err`
- `processor/agentic-loop/handlers.go:1078` — `return HandlerResult{}, err`
- `processor/agentic-loop/component.go:1200` — `c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)`
- `processor/agentic-loop/component.go:1201` — `return nil`
- `processor/agentic-loop/handlers.go:830` — `if existingID, exists := h.loopManager.HasActiveLoopForTask(task.TaskID); exists {`
- `processor/agentic-loop/handlers.go:889` — `// continuation — reachable only after an earlier HandleTask error on this`
- `agentic/state.go:129` — `func (e *LoopEntity) TransitionTo(newState LoopState) error {`
- `processor/agentic-loop/state.go:663` — `func (m *LoopManager) TransitionLoop(loopID string, newState agentic.LoopState) error {`
- `service/component_manager.go:1019` — `runtime.mu.Lock()`
- `service/component_manager.go:1020` — `if runtime.terminal {`
- `service/component_manager.go:1024` — `if runtime.stopping {`
- `service/component_manager.go:1026` — `return errs.WrapTransient(errors.New("component stop already in progress"), "ComponentManager", "stopLifecycleComponent", "concurrent Stop is unsupported")`
- `pkg/lifecycle/doc.go:44` — `// full harness benefit. This package MUST NOT import any`
- `pkg/lifecycle/doc.go:50` — `//   - Raw telemetry, log entries, agent loops, and transient inputs`
- `processor/agentic-loop/handlers.go:2563` — `h.loopManager.TrackRequest(request.RequestID, loopID)`
- `processor/agentic-loop/state.go:497` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-loop/state.go:516` — `delete(m.requestToLoop, k)`
`component.go:1879-1881`, `handlers.go:1163/1969/2233/2482`, `agentic/state.go:129-139`, `processor/agentic-loop/state.go:663` and `:516`, and `handlers.go:2563` are byte-identical to the original citation. The function at `service/component_manager.go:991` covering lines `1019-1070` is `stopLifecycleComponent`, not literally named `StopAll` — `service.Manager.StopAll` is a distinct function at `service/service_manager.go:838` with a different shape (reverse-registration-order fanout with aggregated errors across services, not the four-outcome per-component exit contract the issue quotes); the four-outcome shape (already-stopped→nil, concurrent-stop→transient refusal, stop-failure→`StateFailed`, success→terminal) is what actually lives at the cited line range, under the other name.

Refusal and observation
(Refusal and observation, continued from Named sites above: the `TransitionLoop(loopID, agentic.LoopStateFailed)` calls are uncoded assignment/discard (`_ =` at `:1163`, `:2233`) or plain `if err != nil` with no further classification (`:1969`, `:2482`); `handlers.go:1054/1070/1074/1078` return bare `err`, no `errs.` wrap, no log, no metric; `component.go:1879-1881` logs at `Error` with no metric and no retry/NAK)
No `errs.Classified*` and no metric increment on any of the two stall paths described by the issue.

Nearest pattern instance
- `service/component_manager.go:1026` — `return errs.WrapTransient(errors.New("component stop already in progress"), "ComponentManager", "stopLifecycleComponent", "concurrent Stop is unsupported")`
This is the in-tree instance the issue itself names as the model to adopt: `stopLifecycleComponent`'s four-outcome exit discipline (already-stopped, concurrent-stop, stop-failure, success), which `LoopState`'s `TransitionTo`/`TransitionLoop` (no transition table, no illegal-edge concept) does not have.

## #1249 — agentrun: make milestone fanout settlement replay-safe without partial ACK
Named sites
- `agentic/agentrun/agentrun.go:467` — `type LoopTerminalEvent struct {`
- `agentic/agentrun/agentrun.go:575` — `func (s *MilestoneSubscriber) HandleEvent(ctx context.Context, data []byte) error {`
- `agentic/agentrun/agentrun.go:576` — `normalized, err := agentterminal.Decode(s.decoder, data)`
- `agentic/agentrun/agentrun.go:606` — `s.logger.Error("agentrun: MilestoneHandler panicked",`
- `agentic/agentrun/agentrun.go:613` — `s.logger.Warn("agentrun: MilestoneHandler error",`
- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
The issue body names no `path:line`; these are located by the described behavior ("AgentRun consumes one durable terminal event and fans it out", "source delivery identity is normalized ... but discarded from LoopTerminalEvent"). `LoopTerminalEvent` (`agentrun.go:467-473`: `LoopID`, `RunID`, `RunEntityID`, `Category`, `Outcome`, `Role`) carries no field corresponding to `agentterminal.Event.SourceMessageID` (`terminal.go:67`), confirming the discard the issue describes.

Refusal and observation
- `agentic/agentrun/agentrun.go:606` — `s.logger.Error("agentrun: MilestoneHandler panicked",`
- `agentic/agentrun/agentrun.go:613` — `s.logger.Warn("agentrun: MilestoneHandler error",`
Both are the exact "logged and erased" sites the issue describes — no `errs.` classification, no metric increment, and `HandleEvent` (`:575`) always returns `nil` after the fan-out loop regardless of handler outcome.

Nearest pattern instance
- `agentic/agentrun/agentrun.go:318` — `if errors.Is(err, lifecycle.ErrAlreadyExists) {`
`Mint` (`:293-341`) is a create-vs-exists instance in the same package/file: `mgr.Create` + `lifecycle.ErrAlreadyExists` fallback to an idempotent `Get`, with an explicit origin-mismatch refusal — the shape the contract names as `pkg/lifecycle/manager.go`'s `Create` pattern, already present here for run minting but not for fanout settlement.

## #1288 — research-graph: completion envelopes do not match read_loop_result and current loop state
Named sites
- `processor/research-graph-synthesize/component.go:472` — `func (c *Component) writeResult(ctx context.Context, loopID string, result *research.SearchResult) {`
- `processor/research-graph-synthesize/adapters.go:169` — `func (s *natsLoopStore) PutLoopCompletion(ctx context.Context, loopID string, envelope []byte) error {`
- `processor/agentic-tools/loop_result.go:101` — `func (e *ReadLoopResultExecutor) readLoopResult(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {`
- `configs/rules/research-graph/05-continuation.json:27` — `"tools": ["read_loop_result"],`
All four named files/functions are present unchanged; no line-drift issue (the issue body names functions and files, not lines).

Refusal and observation
- `processor/agentic-tools/loop_result.go:129` — `}, errs.WrapTransient(err, "ReadLoopResultExecutor", "readLoopResult", "get completion entry")`
- `processor/agentic-tools/loop_result.go:138` — `}, errs.WrapInvalid(err, "ReadLoopResultExecutor", "readLoopResult", "unmarshal completion event")`
- `processor/research-graph-synthesize/component.go:474` — `c.logger.Error("search result failed validation; refusing to emit",`
- `processor/research-graph-synthesize/component.go:477` — `atomic.AddInt64(&c.errors, 1)`
- `processor/research-graph-synthesize/component.go:483` — `c.logger.Error("marshal search result envelope failed",`
- `processor/research-graph-synthesize/component.go:491` — `c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",`
- `processor/research-graph-synthesize/component.go:497` — `c.logger.Error("synthesize.complete trigger write failed; continuation rule will not fire",`
`readLoopResult` decodes `entry.Value` directly into `agentic.LoopCompletedEvent` (no registered/type-discriminated decode) — this specific decode has no dedicated refusal path for "record is a different registered payload type"; the `WrapInvalid`/`WrapTransient` pins above cover KV-not-found and JSON-syntax failure only, neither of which fires on the research envelope (JSON unmarshal into `LoopCompletedEvent` succeeds with `Result` empty, per the issue's own diagnostic).

Nearest pattern instance
- `processor/agentic-tools/loop_result.go:138` — `}, errs.WrapInvalid(err, "ReadLoopResultExecutor", "readLoopResult", "unmarshal completion event")`
Classified refusal + observed signal (typed `ErrorKind` returned to the caller plus a classified error) already exists in the same function for the KV-miss and JSON-syntax cases; there is no analogous branch for a structurally-valid-but-wrong-envelope decode.

## Adjacent claims
- #1035: none of the five draft PRs name it
- #1035: body names none of the 60-set
- #1035: body cites no spec or ADR
- #1146: PR #1156 body names it
- #1146: PR #1159 body names it
- #1146: body names #1140 (in the 60-set); also names #949, #733, #759 (not in the 60-set)
- #1146: body cites no spec/ADR; cites `docs/concepts/03-streams-vs-kv-watches.md`, `docs/concepts/17-approval-flow.md`, `docs/concepts/27-frontier-harness-mapping.md` (pinned under Named sites)
- #1239: PR #1156 body names it
- #1239: PR #1159 body names it
- #1239: body names none of the 60-set (refs #1231, #1233 — neither listed in the 60-set)
- #1239: body cites no spec or ADR
- #1244: PR #1159 body names it
- #1244: body names none of the 60-set (refs #1231, #1227, #1216, #1213 — none listed in the 60-set)
- #1244: body cites no spec/ADR; cites `pkg/lifecycle/doc.go` (pinned under Named sites)
- #1249: PR #1156 body names it
- #1249: PR #1159 body names it
- #1249: body names #1146 (in the 60-set); also names #759, #1155 (not in the 60-set)
- #1249: body cites no spec or ADR
- #1288: PR #1159 body names it
- #1288: body names #1146, #1224 (both in the 60-set); also names #1158 (not in the 60-set)
- #1288: body cites no spec or ADR

## Searches
- `gh issue view 1035,1146,1239,1244,1249,1288 --json number,title,body,labels,milestone` → 6 issue bodies fetched
- `cat docs/proposals/pattern-classification-2026-09/issues.md` → 60-set reference table read (60 rows)
- `for p in 1141 1156 1159 1254 1297; do gh pr view $p --json number,body; done` + python scan for `#1035`,`#1146`,`#1239`,`#1244`,`#1249`,`#1288` → 8 matches (PR#1156×3, PR#1159×5); 0 matches for #1035 in any of the five
- `grep -n "^## " openspec/project.md` → 4 (Purpose, Product Boundary, spec role split, Standing Conventions)
- `sed -n '3,67p' openspec/project.md` → Purpose + Product Boundary read
- `grep -n "taskIntakeRejectionLane\|taskIntakeRejectionReason" processor/agentic-loop/component.go` → 3
- `grep -n "preflightDecodedTask" processor/agentic-loop/component.go` → 2
- `awk 'NR>=1310 && NR<=1340 && /errs\.Wrap/'  processor/agentic-loop/component.go` → 4
- `grep -n "ConsumeWithHeartbeat\|Term()" natsclient/heartbeat.go` → 2
- `grep -n "ConsumeDurable handler error" natsclient/consume_durable.go` → 1
- `grep -n "ChannelType\|ChannelID\|UserID" agentic/user_types.go` → 10 (head)
- `grep -n "for error notifications" agentic/user_types.go` → 1
- `grep -n "SetUserContext" processor/agentic-loop/handlers.go` → 1
- `grep -n "ChannelType\|ChannelID\|UserID" processor/agentic-loop/handlers.go` → 12
- `grep -rn "errs\.Classified" processor/agentic-loop/*.go` (excl `_test.go`) → 10 (head); `grep -rln` → 4 files (component.go, inflight.go, todos.go, trajectory_reader.go)
- `grep -n "^type\|^func" processor/agentic-loop/state.go` → 15 (head)
- `grep -n "^type\|^func" processor/agentic-loop/context_manager.go` → 15 (head)
- `grep -n "func (c \*Component) Start" processor/agentic-loop/component.go` → 1
- `grep -n "adaptVoidInputHandler(c.handleResponseMessage)" processor/agentic-loop/component.go` → 1
- `awk 'NR>=515 && NR<=583 && /errs\.Wrap|logger\.(Error|Warn)|c\.metrics\./' processor/agentic-loop/component.go` → 5
- `awk ... approval_response_handler.go 160-190` → 3
- `awk ... component.go 1988-2010` → 2
- `grep -n "No loop found for tool call\|recordToolResultDropped" processor/agentic-loop/component.go` → 2
- `grep -n "func.*handlePauseSignal\|func.*handleResumeSignal\|func.*handleCancelSignal\|PauseRequested" processor/agentic-loop/component.go` → 5
- `grep -n "PauseRequested" agentic/state.go` → 2
- `grep -n "\"cancel\"\|\"status\"\|\"loops\"\|\"help\"" processor/agentic-dispatch/commands.go` → 4
- `grep -rn "PauseRequested" --include="*.go" .` (excl `_test.go`, `openspec/`) → 4 (2 writes, 2 declarations; zero reads confirmed)
- `awk 'NR>=2218 && NR<=2258 && /logger\.|errs\.|metrics\./' processor/agentic-loop/component.go` → 4
- `grep -n "func.*publishResults\|PublishToStream" processor/agentic-loop/component.go` → 5
- `grep -n "LoopStateFailed\|StatusFailed\|TransitionLoop.*Failed" processor/agentic-loop/handlers.go` → 8
- `grep -n "startTrajectory(" processor/agentic-loop/handlers.go` → 1
- `grep -n "func.*buildTaskRequest\|buildTaskRequest(" processor/agentic-loop/handlers.go` → 2
- `grep -n "reachable" processor/agentic-loop/handlers.go` → 1
- `grep -n "HasActiveLoopForTask(task.TaskID)\|Duplicate task message" processor/agentic-loop/handlers.go` → 2
- `awk 'NR>=1035 && NR<=1090 && /return HandlerResult\{\}, err/' processor/agentic-loop/handlers.go` → 4
- `grep -n "TrackRequest(" processor/agentic-loop/handlers.go` → 3
- `grep -n "m.loops\[loopID\] = &entity\|m.pendingTools\[loopID\] = make" processor/agentic-loop/state.go` → 2
- `grep -n "func.*TransitionTo" agentic/state.go` → 1
- `grep -n "func.*TransitionLoop" processor/agentic-loop/state.go` → 1
- `grep -n "func.*DeleteLoop\|delete(m.requestToLoop" processor/agentic-loop/state.go` → 2
- `grep -n "func.*StopAll" service/component_manager.go` → 0 (zero-hit: no function literally named `StopAll` in this file)
- `grep -rn "func.*StopAll" service/` → 16 (finds the real `Manager.StopAll` at `service/service_manager.go:838`, distinct from the cited path)
- `grep -n "MUST NOT import\|Raw telemetry, log entries" pkg/lifecycle/doc.go` → 2
- `find . -type d -iname "*agentrun*"` → 1 (`agentic/agentrun`)
- `grep -rln "LoopTerminalEvent" --include="*.go" .` (excl `_test.go`) → 1 (`agentic/agentrun/agentrun.go`)
- `grep -n "^func\|^type\|milestone\|Milestone\|handler\|Handler\|recover()\|panic\|LoopTerminalEvent" agentic/agentrun/agentrun.go` → 29 (head 50)
- `grep -n "func (s \*MilestoneSubscriber) HandleEvent\|MilestoneHandler panicked\|MilestoneHandler error\|type LoopTerminalEvent\|OnLoopTerminal(ctx context.Context" agentic/agentrun/agentrun.go` → 5
- `git grep -l "package agentterminal" -- '*.go'` → 2 (`internal/agentterminal/terminal.go`, `terminal_test.go`)
- `grep -n "lifecycle.ErrAlreadyExists" agentic/agentrun/agentrun.go` → 3
- `grep -n "func.*writeResult" processor/research-graph-synthesize/component.go` → 1
- `grep -n "func.*PutLoopCompletion" processor/research-graph-synthesize/adapters.go` → 1
- `grep -n "func.*readLoopResult" processor/agentic-tools/loop_result.go` → 1
- `grep -n "read_loop_result" configs/rules/research-graph/05-continuation.json` → 2
- `awk ... processor/agentic-tools/loop_result.go 101-140` (errs./ErrorKind/Error:) → 10
- `awk ... processor/research-graph-synthesize/component.go 472-502` (logger/atomic) → 7
NOT RUN: `gopls references`/`gopls implementation`/`gopls call_hierarchy` on any symbol in this slice — budget was spent on `git grep`/`sed` line-pinning across six issues with unusually large cited-site counts (#1146 and #1244 alone name ~20 sites each); no interface-implementer or caller-graph question was posed by any of the six bodies, but a caller-side re-derivation (e.g. `gopls references` on `TransitionTo`, `PauseRequested`, `HandleEvent`) was not attempted independently of `grep`.
