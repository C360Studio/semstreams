# Inventory: agentic-loop transition-result contract (#1376)
base: 7f28a2f3970e386e9e2b69831da33df41ffc6d80

Brief: enumerate every production site that constructs or returns a `HandlerResult` on the task, model, tool,
approval, signal, verdict and timer/sweeper entry paths (Producers), every site that interprets a
`(HandlerResult, error)` pair (Consumers), `carrierOrder`, the spec/design/migration text on settlement order and
terminal ownership, and the tests that pin those orders. No judgment, no options, no verdict — enumeration only.

## HandlerResult — the type

- `processor/agentic-loop/handlers.go:59` — `type HandlerResult struct {`
- `processor/agentic-loop/handlers.go:60` — `LoopID               string`
- `processor/agentic-loop/handlers.go:61` — `State                agentic.LoopState`
- `processor/agentic-loop/handlers.go:62` — `PublishedMessages    []PublishedMessage`
- `processor/agentic-loop/handlers.go:63` — `PendingTools         []string`
- `processor/agentic-loop/handlers.go:64` — `TrajectorySteps      []agentic.TrajectoryStep`
- `processor/agentic-loop/handlers.go:65` — `ContextEvents        []agentic.ContextEvent`
- `processor/agentic-loop/handlers.go:66` — `RetryScheduled       bool`
- `processor/agentic-loop/handlers.go:67` — `MaxIterationsReached bool`
- `processor/agentic-loop/handlers.go:73` — `Deferred bool`
- `processor/agentic-loop/handlers.go:80` — `Created bool`
- `processor/agentic-loop/handlers.go:84` — `CompletionState *agentic.LoopCompletedEvent`
- `processor/agentic-loop/handlers.go:87` — `FailureState *agentic.LoopFailedEvent`
- `processor/agentic-loop/handlers.go:96` — `SyntheticDecide *SyntheticDecideRequest`
- `processor/agentic-loop/handlers.go:109` — `terminalOwnedElsewhere bool`
- `processor/agentic-loop/handlers.go:114` — `trajectoryObservations []trajectoryObservation`
- `processor/agentic-loop/handlers.go:123` — `staleDrop bool`

`RetryScheduled` (`:66`) has no production write site under any spelling searched (`## Searches`); its only
non-declaration references are a test assertion and a test-local mock struct field, both excluded as tests.

## Producers

Grouped by entry path. Each pin is the file:line that constructs or returns the `HandlerResult`; the line
immediately after names the fields it sets (beyond zero value) and whether an error rides with it, and if so which.

### Task entry path — `HandleTask` (handlers.go:866), `buildTaskRequest` (handlers.go:1162), `deferredContinuationResult` (handlers.go:1119)

- `processor/agentic-loop/handlers.go:869` — `return HandlerResult{}, err`
  Zero value; error = `ctx.Err()` (untyped, pre-work cancellation).
- `processor/agentic-loop/handlers.go:874` — `return HandlerResult{}, errs.WrapInvalid(`
  Zero value; error = `errs.WrapInvalid` (max depth exceeded).
- `processor/agentic-loop/handlers.go:896` — `return HandlerResult{LoopID: existingID}, nil`
  LoopID only; error nil (redelivery dedup — active loop already running this TaskID).
- `processor/agentic-loop/handlers.go:924` — `return HandlerResult{}, err`
  Zero value; error = `attachContinuation` failure (`ErrLoopTerminal`/`ErrLoopBusy` reachable here).
- `processor/agentic-loop/handlers.go:934` — `return HandlerResult{}, err`
  Zero value; error = `CreateLoopWithID` default-case error.
- `processor/agentic-loop/handlers.go:939` — `return HandlerResult{}, err`
  Zero value; error = `CreateLoop` failure.
- `processor/agentic-loop/handlers.go:968` — `return HandlerResult{}, err`
  Zero value; error = `startTrajectory` failure.
- `processor/agentic-loop/handlers.go:982` — `return HandlerResult{}, err`
  Zero value; error = re-read `GetLoop` failure.
- `processor/agentic-loop/handlers.go:1097` — `return h.deferredContinuationResult(loopID, task.TaskID, entity), nil`
  Forwards to the producer below; error nil.
- `processor/agentic-loop/handlers.go:1124` — `result := HandlerResult{`
  (inside `deferredContinuationResult`) Sets LoopID, State (=entity.State), `Deferred: true`, empty
  PublishedMessages/TrajectorySteps/ContextEvents; returned with error nil.
- `processor/agentic-loop/handlers.go:1102` — `return HandlerResult{}, err`
  Zero value; error = `buildTaskRequest` failure.
- `processor/agentic-loop/handlers.go:1214` — `result := HandlerResult{`
  (inside `buildTaskRequest`) Sets LoopID, State, `Created: true`, PublishedMessages (agent.request +
  agent.created), TrajectorySteps; returned at `handlers.go:1256` with error nil.
- `processor/agentic-loop/handlers.go:1105` — `return result, nil`
  `HandleTask`'s own final return of whatever `buildTaskRequest` produced; error nil.

### Model entry path — `HandleModelResponse` (handlers.go:1304), `handleToolCallResponse` (handlers.go:1598), `handleLengthTruncation` (handlers.go:2170), `emitRetryRequest` (handlers.go:2287), `handleCompleteResponse` (handlers.go:2509)

- `processor/agentic-loop/handlers.go:1312` — `return HandlerResult{}, fmt.Errorf("%w: %w", errCancelledBeforeMutation, err)`
  Zero value; error wraps sentinel `errCancelledBeforeMutation`.
- `processor/agentic-loop/handlers.go:1316` — `return HandlerResult{}, err`
  Zero value; error = GetLoop failure (handlers.go:1314-1316).
- `processor/agentic-loop/handlers.go:1367` — `return HandlerResult{}, fmt.Errorf("%w: loop %s response names request %q, its record names %q",`
  Zero value; error wraps sentinel `errResponseSuperseded`.
- `processor/agentic-loop/handlers.go:1375` — `return HandlerResult{}, fmt.Errorf("%w: loop %s response names request %q, its record names %q",`
  Zero value; error wraps sentinel `errResponseForeign`.
- `processor/agentic-loop/handlers.go:1382` — `return HandlerResult{`
  Sets LoopID, State (=entity.State, non-terminal), empty PublishedMessages/TrajectorySteps/ContextEvents; error
  (`:1388-1389`) wraps sentinel `errRequestNotYetObservable`.
- `processor/agentic-loop/handlers.go:1419` — `return HandlerResult{}, fmt.Errorf("%w: loop %s response names request %q, which the loop is "+`
  Zero value; error wraps sentinel `errResponseAlreadyApplied`.
- `processor/agentic-loop/handlers.go:1433` — `return terminalGuardResult(loopID, entity.State), nil`
  Producer is `terminalGuardResult` (handlers.go:2629): LoopID, State, empty slices,
  `terminalOwnedElsewhere: true`; error nil.
- `processor/agentic-loop/handlers.go:1441` — `result := HandlerResult{`
  Sets LoopID, State (=entity.State), empty PublishedMessages/TrajectorySteps/ContextEvents — the base every
  success/error return below mutates further.
- `processor/agentic-loop/handlers.go:1469` — `return h.failTimedOutLoop(loopID, result, "HandleModelResponse")`
  Producer is `failTimedOutLoop` (handlers.go:3366): State forced to `LoopStateFailed`, PublishedMessages =
  failure messages, FailureState set; error = `errs.WrapFatal(fmt.Errorf("loop timeout exceeded"), ...)` — a
  **terminal result returned together with a non-nil (Fatal) error**.
- `processor/agentic-loop/handlers.go:1474` — `return result, errs.WrapFatal(`
  State unchanged (still entity.State, non-terminal); error = `errs.WrapFatal` wrapping sentinel
  `ErrMaxIterationsReached` — a **non-terminal-shaped result returned together with a non-nil (Fatal) error**.
- `processor/agentic-loop/handlers.go:1536` — `return result, err`
  (inside `handleToolCallResponse`) result as accumulated to that point; error = governance-dispatch proposal
  failure.
- `processor/agentic-loop/handlers.go:1567` — `return result, err`
  (inside `handleToolCallResponse`) error = per-call dispatch failure.
- `processor/agentic-loop/handlers.go:1576` — `return result, err`
  (inside `handleToolCallResponse`) error propagated from a rejected-call synthesis path.
- `processor/agentic-loop/handlers.go:1581` — `return result, err`
  (inside `handleToolCallResponse`) error propagated from a further dispatch/synthesis branch.
- `processor/agentic-loop/handlers.go:1586` — `return result, err`
  (inside `handleToolCallResponse`) error propagated from the batch's final branch.
- `processor/agentic-loop/handlers.go:1590` — `return result, nil`
  `HandleModelResponse`'s own final success return; error nil.
- `processor/agentic-loop/handlers.go:2404` — `result.FailureState = failure`
  (inside `failLoop`, handlers.go:2383) alongside `:2393` `result.State = agentic.LoopStateFailed` and `:2403`
  `result.PublishedMessages = failMsgs`; `failLoop` itself returns only `error` (nil on success), mutating the
  caller's `*HandlerResult` by pointer.
- `processor/agentic-loop/handlers.go:2513` — `result.State = agentic.LoopStateComplete`
  (inside `handleCompleteResponse`, handlers.go:2509) — mutates the pointer result toward the completion shape.
- `processor/agentic-loop/handlers.go:2586` — `result.SyntheticDecide = &SyntheticDecideRequest{`
  (inside `handleCompleteResponse`) conditional on synthesis opt-in.
- `processor/agentic-loop/handlers.go:2602` — `result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{`
  (inside `handleCompleteResponse`) appends the `agent.complete` publication.
- `processor/agentic-loop/handlers.go:2609` — `result.CompletionState = &completion`
  (inside `handleCompleteResponse`) — `handleCompleteResponse` itself returns only `error` (nil on success),
  mutating the caller's `*HandlerResult` by pointer.

### Tool entry path — `HandleToolResult` (handlers.go:2641), `checkApprovalGate` (handlers.go:2824), `handleToolsComplete` (handlers.go:2982)

- `processor/agentic-loop/handlers.go:2644` — `return HandlerResult{}, fmt.Errorf("%w: %w", errCancelledBeforeMutation, err)`
  Zero value; error wraps sentinel `errCancelledBeforeMutation`.
- `processor/agentic-loop/handlers.go:2649` — `return HandlerResult{}, err`
  Zero value; error = `GetLoop` failure.
- `processor/agentic-loop/handlers.go:2660` — `return terminalGuardResult(loopID, entity.State), nil`
  Same producer as the model lane's `:1433`; `terminalOwnedElsewhere: true`; error nil.
- `processor/agentic-loop/handlers.go:2662` — `result := HandlerResult{`
  Sets LoopID, State, PendingTools, empty PublishedMessages/TrajectorySteps/ContextEvents.
- `processor/agentic-loop/handlers.go:2691` — `return h.failTimedOutLoop(loopID, result, "HandleToolResult")`
  Same producer as the model lane's `:1469` — terminal result + non-nil Fatal error.
- `processor/agentic-loop/handlers.go:2708` — `return HandlerResult{}, err`
  Zero value; error = `StoreToolResult` failure.
- `processor/agentic-loop/handlers.go:2714` — `return HandlerResult{}, err`
  Zero value; error = `RemovePendingTool` failure.
- `processor/agentic-loop/handlers.go:2733` — `if h.checkApprovalGate(loopID, &entity, toolResult, &result) {`
  Gate check; on true, `checkApprovalGate` has mutated `result` (see below) and the caller returns it at `:2734`.
- `processor/agentic-loop/handlers.go:2734` — `return result, nil`
  Gate-created or gate-absorbed-sibling result; error nil.
- `processor/agentic-loop/handlers.go:2877` — `*result = terminalGuardResult(loopID, refused.state)`
  (inside `checkApprovalGate`) the gate lost a race to a settle between the terminal guard and the gate
  (`gateRefusedError`); `terminalOwnedElsewhere: true`.
- `processor/agentic-loop/handlers.go:2888` — `result.State = agentic.LoopStateAwaitingApproval`
  (inside `checkApprovalGate`) the ordinary gate-creation mutation, alongside an appended `PublishedMessages`
  entry (`:2886`).
- `processor/agentic-loop/handlers.go:2782` — `return result, err`
  (inside `HandleToolResult`, StopLoop/carry branch) error from `carryDeferredContinuation`.
- `processor/agentic-loop/handlers.go:2790` — `return result, err`
  (inside `HandleToolResult`, StopLoop branch) error from `handleCompleteResponse`.
- `processor/agentic-loop/handlers.go:2800` — `return result, storeErr`
  (inside `HandleToolResult`, serial-dispatch branch) error from `dispatchedFromQueue`.
- `processor/agentic-loop/handlers.go:2812` — `return h.handleToolsComplete(ctx, loopID, entity, cm, &result)`
  Forwards to `handleToolsComplete`; see its own producers below.
- `processor/agentic-loop/handlers.go:2815` — `return result, nil`
  `HandleToolResult`'s own final fallback return (mid-batch: siblings still outstanding); error nil.
- `processor/agentic-loop/handlers.go:2991` — `return *result, err`
  (inside `handleToolsComplete`) pre-work `ctx.Err()` cancellation.
- `processor/agentic-loop/handlers.go:3005` — `return *result, errs.Wrap(err, "agentic-loop", "handleToolsComplete", "increment iteration")`
  Non-sentinel `IncrementIteration` failure (not `ErrMaxIterationsReached`); result as accumulated so far.
- `processor/agentic-loop/handlers.go:3015` — `return *result, errs.Wrap(transitionErr, "agentic-loop", "handleToolsComplete", fmt.Sprintf("transition loop to failed state (original error: %v)", err))`
  `TransitionLoop` itself failed while handling max-iterations exhaustion.
- `processor/agentic-loop/handlers.go:3017` — `result.State = agentic.LoopStateFailed`
  alongside `:3018` `result.MaxIterationsReached = true`, `:3030` `result.PublishedMessages = failMsgs`, `:3031`
  `result.FailureState = failure`, returned at `:3034` `return *result, nil` — a **terminal result (State=Failed,
  MaxIterationsReached=true, FailureState set) returned with error nil**, the shape the proposal contrasts against
  `failTimedOutLoop`'s terminal-with-Fatal-error shape.
- `processor/agentic-loop/handlers.go:3043` — `return *result, err`
  `publishIterationRequest` failure on the ordinary-advance path.
- `processor/agentic-loop/handlers.go:3046` — `return *result, nil`
  Ordinary-advance success (next iteration's request published); error nil.

### Approval entry path — `HandleApprovalResponse` (approval_response_handler.go:34), `dispatchApprovedCall` (:127), `handleRejectedApproval` (:150)

- `processor/agentic-loop/approval_response_handler.go:45` — `result = HandlerResult{LoopID: response.LoopID}`
  (panic-recover deferred func) LoopID only; error (`:46-47`) = `errs.WrapFatal` wrapping the recovered panic.
- `processor/agentic-loop/approval_response_handler.go:52` — `return HandlerResult{}, errs.WrapInvalid(vErr, "agentic-loop", "HandleApprovalResponse", "validate response")`
  Zero value; error = `errs.WrapInvalid`.
- `processor/agentic-loop/approval_response_handler.go:64` — `return HandlerResult{LoopID: loopID}, resolveErr`
  LoopID only; error = `ResolveApprovalIfPending` failure (`ErrLoopNotFound` reachable here).
- `processor/agentic-loop/approval_response_handler.go:78` — `return HandlerResult{LoopID: loopID, State: state, staleDrop: true}, nil`
  LoopID, State, `staleDrop: true`; error nil (stale/duplicate answer).
- `processor/agentic-loop/approval_response_handler.go:85` — `return HandlerResult{}, getErr`
  Zero value; error = re-read `GetLoop` failure.
- `processor/agentic-loop/approval_response_handler.go:88` — `result = HandlerResult{`
  Sets LoopID, State (=restored entity.State), empty PublishedMessages — the base for approve/modify/reject below.
- `processor/agentic-loop/approval_response_handler.go:102` — `return h.failTimedOutLoop(loopID, result, "HandleApprovalResponse")`
  Same producer as the model/tool lanes — terminal result + non-nil Fatal error.
- `processor/agentic-loop/approval_response_handler.go:107` — `return result, h.dispatchApprovedCall(loopID, pending, pending.Arguments, response.ApprovedBy, &result)`
  Approve decision; `dispatchApprovedCall` mutates `result` by pointer (a dispatched ToolCall) and returns error
  (nil, or a dispatch failure) as the second value.
- `processor/agentic-loop/approval_response_handler.go:113` — `return result, h.dispatchApprovedCall(loopID, pending, args, response.ApprovedBy, &result)`
  Modify decision; same shape as `:107` with substituted arguments.
- `processor/agentic-loop/approval_response_handler.go:115` — `return h.handleRejectedApproval(ctx, loopID, pending, response)`
  Reject decision; forwards to `handleRejectedApproval`, which synthesizes an `agentic.ToolResult` and calls
  `HandleToolResult` (`:163`) — every Tool-entry-path producer above is reachable from a reject.
  Pin: `processor/agentic-loop/approval_response_handler.go:163` — `return h.HandleToolResult(ctx, loopID, synthetic)`
- `processor/agentic-loop/approval_response_handler.go:119` — `return HandlerResult{}, fmt.Errorf("unknown approval decision %q", response.Decision)`
  Zero value; error = unknown-decision guard (unreachable given prior `Validate()`).

### Signal entry path — `handleCancelSignal` (component.go:3362)

- `processor/agentic-loop/component.go:3422` — `if err := c.commitTerminal(ctx, terminalOutcome{cancelled: &completion}, HandlerResult{`
  Constructs a bare `HandlerResult{LoopID, PublishedMessages: [agent.complete]}` passed directly into
  `commitTerminal`, never through `persistHandlerResult` and never returned as `(HandlerResult, error)` — the cancel
  path has no `(result, error)` pair of its own; `commitTerminal`'s own returned `error` is what `handleCancelSignal`
  interprets (see Consumers).

### Verdict entry path — `handleToolCallVerdictMessage` (component.go:3461), `settleVerdictWithoutWaiter` (component.go:3504)

(none — see `## Searches`.) Neither function constructs, returns, or reads a `HandlerResult`: verdicts route
entirely through `GovernanceDispatcher.HandleVerdict` and `classifyWaiterlessVerdict`, and the delivery decision is
computed directly from that, `settled` (a `natsclient.DeliveryDecision`) and `err`, with no `HandlerResult` anywhere
on the path.

### Timer / sweeper entry path — `sweepExpiredApprovals` (approval_sweeper.go:77)

The sweeper constructs no `HandlerResult` of its own; it builds an `agentic.ApprovalResponse` (auto-reject) and
feeds it through `HandleApprovalResponse` — every Approval-entry-path producer above is reachable from the sweeper.

- `processor/agentic-loop/approval_sweeper.go:104` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`

### Other bare `HandlerResult` constructions found outside the seven entry paths

- `processor/agentic-loop/component.go:2103` — `HandlerResult{LoopID: loopID, PublishedMessages: failMsgs})`
  (inside `handleLoopFailure`, component.go:2066 — reached from the Model entry path's error fallthrough) passed
  directly into `commitTerminal`, not through `persistHandlerResult`.
- `processor/agentic-loop/component.go:2816` — `if err := c.publishResults(ctx, HandlerResult{LoopID: loopID, PublishedMessages: []PublishedMessage{*echo}}); err != nil {`
  (inside `republishPendingApproval`) passed directly into `publishResults`, bypassing both `persistHandlerResult`
  and `commitTerminal`.
- `processor/agentic-loop/terminal_owner.go:362` — `if err := c.publishResults(ctx, HandlerResult{LoopID: loopID, PublishedMessages: messages}); err != nil {`
  (inside `writeRecordCancelled`, reached from `adoptDurableCancel` — Signal-adjacent cold path) same shape as
  `:2816`.

## Consumers

### `persistHandlerResult` (component.go:2245) and its production callers

- `processor/agentic-loop/component.go:2245` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, order carrierOrder) error {`
- `processor/agentic-loop/component.go:2246` — `terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed`
- `processor/agentic-loop/component.go:2249` — `gated := result.State == agentic.LoopStateAwaitingApproval`
- `processor/agentic-loop/component.go:2251` — `if result.terminalOwnedElsewhere {`
- `processor/agentic-loop/component.go:2256` — `return c.settleTerminalGuard(ctx, result, nil)`
- `processor/agentic-loop/component.go:2278` — `if err := c.commitTerminal(ctx, terminalOutcomeOf(result), result); err != nil {`
  Reads `terminal` (from `:2246`); routes every terminal result to the terminal owner regardless of `order`.
- `processor/agentic-loop/component.go:2285` — `if order == publishThenWrite && !gated {`
  Reads `order` and `gated`; non-terminal, non-gated results take `publishThenPersistResultState`.

Production call sites (confirmed by `gopls references` on the declaration, 7 sites; a further 9 are in
`*_test.go`, excluded):

- `processor/agentic-loop/approval_response_handler.go:221` — `err = c.persistHandlerResult(ctx, result, writeThenPublish)`
  Order = `writeThenPublish`; called only from the terminal-with-error guard (`:210`).
- `processor/agentic-loop/approval_response_handler.go:272` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`
  Order = `publishThenWrite`; the ordinary (non-terminal-guard, non-error) approval result path.
- `processor/agentic-loop/approval_sweeper.go:123` — `if commitErr := c.persistHandlerResult(ctx, result, writeThenPublish); commitErr != nil {`
  Order = `writeThenPublish`; sweeper's own terminal-with-error guard (`:113`).
- `processor/agentic-loop/approval_sweeper.go:158` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`
  Order = `publishThenWrite`; sweeper's ordinary auto-reject commit.
- `processor/agentic-loop/component.go:1920` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`
  Order = `publishThenWrite`; `handleResponseMessage`'s ordinary (non-terminalOwnedElsewhere) success path.
- `processor/agentic-loop/component.go:2610` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`
  Order = `publishThenWrite`; `handleToolResultMessage`'s ordinary success path (covers terminal-without-error
  too, since `persistHandlerResult` routes terminal results to `commitTerminal` regardless of `order`).
- `processor/agentic-loop/component.go:2664` — `return c.persistHandlerResult(ctx, result, writeThenPublish)`
  Order = `writeThenPublish`; inside `settleFailedToolResult`, guarded only by `result.State.IsTerminal()`
  (`:2659`) — no `!result.terminalOwnedElsewhere` conjunct, the third guard variant the proposal names.

### `settleFailedToolResult` (component.go:2654)

- `processor/agentic-loop/component.go:2654` — `func (c *Component) settleFailedToolResult(`
- `processor/agentic-loop/component.go:2659` — `if result.State.IsTerminal() {`
  Reads `State` only (no `terminalOwnedElsewhere` check); on true, commits via `persistHandlerResult` (`:2664`,
  `writeThenPublish`).
- `processor/agentic-loop/component.go:2668` — `if errors.Is(cause, errCancelledBeforeMutation) {`
  Non-terminal branch: the one retried cause.
- `processor/agentic-loop/component.go:2671` — `return errs.WrapFatal(cause, "agentic-loop", "handleToolResultMessage",`
  Non-terminal, non-cancellation: every other cause is Fatal (Quarantine).
- `processor/agentic-loop/component.go:2582` — `return c.settleFailedToolResult(ctx, loopID, result, err)`
  Called from handleToolResultMessage's HandleToolResult error branch.

### `handleLoopFailure` (component.go:2066) and `failureReasonForHandlerError` (component.go:1929)

- `processor/agentic-loop/component.go:2066` — `func (c *Component) handleLoopFailure(`
- `processor/agentic-loop/component.go:2102` — `established := c.commitTerminal(errorCtx, terminalOutcome{failed: failure},`
  Ignores the `HandlerResult` `HandleModelResponse` returned; builds its own bare `HandlerResult{LoopID,
  PublishedMessages: failMsgs}` (component.go:2103) and commits through `commitTerminal` directly, not
  `persistHandlerResult`.
- `processor/agentic-loop/component.go:2113` — `return errs.WrapFatal(established, "agentic-loop", "handleLoopFailure",`
  Non-`ErrKVRevisionMismatch` commit failures are Fatal; a mismatch (`:2111` `if errors.Is(established, natsclient.ErrKVRevisionMismatch) {`) is returned as-is (transient).
- `processor/agentic-loop/component.go:1929` — `func failureReasonForHandlerError(err error) string {`
  Classifies via `errors.Is(err, ErrMaxIterationsReached)` → `"max_iterations"`, else `"handler_error"`.
- `processor/agentic-loop/component.go:1907` — `return c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)`
  (`handleResponseMessage`'s fallthrough for every `HandleModelResponse` error not matched by the five named
  sentinels — this is where the model lane's `ErrMaxIterationsReached`-with-non-terminal-result shape (producer
  `handlers.go:1474`) is re-derived rather than committed as returned.)
- `processor/agentic-loop/approval_response_handler.go:431` — `return c.handleLoopFailure(ctx, loopID, record.entity, continuationUnavailableReason, cause)`

### `settleTerminalGuard` (terminal_owner.go:423)

- `processor/agentic-loop/terminal_owner.go:423` — `func (c *Component) settleTerminalGuard(ctx context.Context, result HandlerResult, recordDrop func()) error {`
- `processor/agentic-loop/terminal_owner.go:424` — `record := c.readLoopRecord(ctx, result.LoopID)`
- `processor/agentic-loop/terminal_owner.go:425` — `if record.presence == loopPresenceStale {`
  Terminal-or-absent record: Ack without effect (calls `recordDrop` if non-nil).
- `processor/agentic-loop/terminal_owner.go:435` — `fmt.Errorf("loop %s is terminal in memory and its record is not: the terminal commit is not settled",`
  Live or unreadable record: transient (Retry).

Production callers (4 sites):

- `processor/agentic-loop/component.go:1910` — `if result.terminalOwnedElsewhere {`
  handleResponseMessage; guards `processor/agentic-loop/component.go:1913` (settleTerminalGuard call).
- `processor/agentic-loop/component.go:2251` — `if result.terminalOwnedElsewhere {`
  the persistHandlerResult backstop; guards `processor/agentic-loop/component.go:2256`.
- `processor/agentic-loop/component.go:2584` — `if result.terminalOwnedElsewhere {`
  handleToolResultMessage; guards `processor/agentic-loop/component.go:2587` (settleTerminalGuard call).
- `processor/agentic-loop/approval_response_handler.go:262` — `if result.terminalOwnedElsewhere {`
  handleApprovalResponseMessage; guards `processor/agentic-loop/approval_response_handler.go:267` (settleTerminalGuard call).

### `commitTerminal` / `commitTerminalSteps` / `terminalOutcomeOf` (terminal_owner.go)

- `processor/agentic-loop/terminal_owner.go:32` — `func terminalOutcomeOf(result HandlerResult) terminalOutcome {`
  Reads `result.CompletionState`, `result.FailureState`, `result.SyntheticDecide`; drops `LoopID`/`PublishedMessages`
  (the caller passes `result` itself separately as `publication`).
- `processor/agentic-loop/terminal_owner.go:146` — `func (c *Component) commitTerminal(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
- `processor/agentic-loop/terminal_owner.go:161` — `func (c *Component) commitTerminalSteps(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
  Reads `publication.LoopID` and `publication.PublishedMessages` (mutated on adoption); the four-step order:
  marker Create, graph stamps, terminal publish, record compare-and-swap.

Production callers (3 sites, all named in the Producers section above):

Called from component.go:2102 (handleLoopFailure), component.go:2278 (persistHandlerResult, via terminalOutcomeOf), and component.go:3422 (handleCancelSignal) — all three pinned above under Producers.

### `publishThenPersistResultState` (component.go:2333)

- `processor/agentic-loop/component.go:2333` — `func (c *Component) publishThenPersistResultState(ctx context.Context, result HandlerResult) error {`
  Sole caller: `processor/agentic-loop/component.go:2285` — `if order == publishThenWrite && !gated {` (inside
  `persistHandlerResult`), i.e. it is never called directly by a lane, only by `persistHandlerResult` itself.

### Lane mapping of the outcome to a `natsclient.DeliveryDecision`

- `processor/agentic-loop/component.go:1839` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {`
  Returns plain `error`, not a `DeliveryDecision` — this lane (`agent.response`) is one of the three
  "heartbeat" lanes (`agent.task`, `agent.response`, `tool.result`, named at `component.go:1034`) whose
  Ack/Retry/Quarantine classification happens generically outside this file, from the returned error's
  `errs.IsFatal`/transient classification (see `resolveLoopLaneDelivery`, `component.go:1025`).
- `processor/agentic-loop/component.go:2479` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {`
  Same shape: plain `error`, generic heartbeat classification.
- `processor/agentic-loop/component.go:1447` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
  Same shape: plain `error`, generic heartbeat classification.
- `processor/agentic-loop/approval_response_handler.go:175` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
  Explicit decision, mapped in-function: `:224` Ack (terminal-with-error committed), `:230`/`:232`
  Retry/Quarantine (terminal-with-error commit failed, by `errs.IsFatal`), `:239`-`:246` (Quarantine/Terminate/Retry
  by `errs.IsFatal`/`errs.IsInvalid`), `:248` Ack (stale drop), `:270` Ack (terminal-guard settled), `:280`-`:284`
  (Retry/Quarantine from `persistHandlerResult`), `:284` Ack (final success).
- `processor/agentic-loop/component.go:3284` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
  Explicit decision: Terminate on decode/unsupported-type, else Quarantine/Retry by `errs.IsFatal(err)`
  (`:3303`-`:3306`), Ack on success.
- `processor/agentic-loop/component.go:3461` — `func (c *Component) handleToolCallVerdictMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
  Explicit decision, but never through a `HandlerResult` (see Producers § Verdict).

## carrierOrder

- `processor/agentic-loop/component.go:2193` — `type carrierOrder int`
- `processor/agentic-loop/component.go:2199` — `writeThenPublish carrierOrder = iota`
- `processor/agentic-loop/component.go:2205` — `publishThenWrite`

Every mention (production, 8 non-declaration sites; declaration + 2 const lines above):

- `processor/agentic-loop/approval_response_handler.go:221` — `err = c.persistHandlerResult(ctx, result, writeThenPublish)`
- `processor/agentic-loop/approval_response_handler.go:272` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`
- `processor/agentic-loop/approval_sweeper.go:123` — `if commitErr := c.persistHandlerResult(ctx, result, writeThenPublish); commitErr != nil {`
- `processor/agentic-loop/approval_sweeper.go:158` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`
- `processor/agentic-loop/component.go:1920` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`
- `processor/agentic-loop/component.go:2245` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, order carrierOrder) error {`
- `processor/agentic-loop/component.go:2285` — `if order == publishThenWrite && !gated {`
- `processor/agentic-loop/component.go:2610` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`
- `processor/agentic-loop/component.go:2664` — `return c.persistHandlerResult(ctx, result, writeThenPublish)`

Every mention in tests (14 files; `## Searches` records the sweep):

- `processor/agentic-loop/loop_carrier_test.go:76` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), publishThenWrite)`
- `processor/agentic-loop/loop_carrier_test.go:87` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), writeThenPublish)`
- `processor/agentic-loop/persist_handler_result_test.go:69` — `}, writeThenPublish)`
- `processor/agentic-loop/persist_handler_result_test.go:224` — `}, publishThenWrite)`
- `processor/agentic-loop/publish_phase_fatal_test.go:48` — `persistErr := c.persistHandlerResult(t.Context(), result, writeThenPublish)`
- `processor/agentic-loop/terminal_owner_test.go:399` — `persistErr := c.persistHandlerResult(t.Context(), result, publishThenWrite)`
- `processor/agentic-loop/trajectory_eviction_internal_test.go:31` — `}, writeThenPublish)`
- `processor/agentic-loop/task_redelivery_integration_test.go:187` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`
- `processor/agentic-loop/tool_result_redelivery_integration_test.go:216` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`

## Spec and design text

### `openspec/specs/agentic-loop/spec.md` — settlement, publication order, terminal ownership, redelivery, residuals

- `openspec/specs/agentic-loop/spec.md:886` — `### Requirement: Loop input classes settle after owner-specific durable done`
- `openspec/specs/agentic-loop/spec.md:1408` — `### Requirement: Loop-state authority is acquired and observed before loop work`
- `openspec/specs/agentic-loop/spec.md:1467` — `### Requirement: The loop record names its outstanding request`
- `openspec/specs/agentic-loop/spec.md:1487` — `record SHALL be written before the first request is published. An update that CREATES an approval gate, on whichever`
  Birth order.
- `openspec/specs/agentic-loop/spec.md:1488` — `lane produces it, SHALL be written before the gate is published: a gate published before it is written leaves a human`
  Gate order.
- `openspec/specs/agentic-loop/spec.md:1490` — `create-once before its terminal event is published, and the loop entity's terminal state SHALL be written after that`
  Terminal order (marker, event, record).
- `openspec/specs/agentic-loop/spec.md:1498` — `and its event have landed is not reconciled: the`
- `openspec/specs/agentic-loop/spec.md:1501` — `and then fails to publish is not reconciled either: a timer is never redelivered, and the record`

### Archived design — `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md`

- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:86` — `## 3. The carrier: order, form, identity adoption`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:236` — `### 5.1 Task (`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:275` — `### 5.2 Model response (`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:299` — `### 5.3 Tool result (`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:331` — `### 5.4 Approval-required tool result (gate) — L4a, except the warm re-echo (L4b, #1362)`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:343` — `### 5.5 Approval response (`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:368` — `### 5.6 Governance verdict (Q6 — L4b, #1362), timer, startup, cancel`
- `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md:396` — `### 5.7 Terminal (all lanes; one owner replacing the three`

### Archived L4b change — `openspec/changes/archive/2026-09-24-agentic-loop-restart-l4b/`

- `openspec/changes/archive/2026-09-24-agentic-loop-restart-l4b/proposal.md:44` — `The order becomes marker`
- `openspec/changes/archive/2026-09-24-agentic-loop-restart-l4b/tasks.md:69` — `## 3. One terminal owner (design § 5.7, D41/P6; ruling 2026-09-18 on #1330)`
- `openspec/changes/archive/2026-09-24-agentic-loop-restart-l4b/tasks.md:71` — `Replace the three marker`
- `openspec/changes/archive/2026-09-24-agentic-loop-restart-l4b/tasks.md:195` — `logged the timeout failure and discarded it); it now commits that failure through the terminal owner too.`

### `docs/operations/migration-beta162-to-beta163.md` — ordering and the two recorded residuals

- `docs/operations/migration-beta162-to-beta163.md:1965` — `then the event, then the record — BREAKING`
- `docs/operations/migration-beta162-to-beta163.md:2009` — `**Two residuals, recorded and not reconciled** (#1362 issuecomment-5808903072 and issuecomment-5809906669):`
- `docs/operations/migration-beta162-to-beta163.md:2021` — `case; the loop-timeout case is this change's residual beside it.)`
- `docs/operations/migration-beta162-to-beta163.md:2106` — `Counts a loop done once its marker exists, which is now before the event.`

## Tests that pin the orders

- `processor/agentic-loop/loop_carrier_test.go:71` — `func TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind(t *testing.T) {`
- `processor/agentic-loop/loop_carrier_test.go:152` — `func TestAnApprovalGateIsWrittenBeforeItsEventIsPublished(t *testing.T) {`
- `processor/agentic-loop/loop_carrier_test.go:226` — `func TestTheApprovalTimeoutSweepNamesTheRequestItPublished(t *testing.T) {`
- `processor/agentic-loop/loop_carrier_test.go:347` — `func TestBirthRefusesASecondCreateForTheSameLoop(t *testing.T) {`
- `processor/agentic-loop/loop_carrier_test.go:378` — `func TestBirthWhosePublishFailsIsNotAcknowledged(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:208` — `func TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:252` — `func TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:72` — `func TestTerminalOwnerArms(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:186` — `func TestCancelTakesTheTerminalOwnerAndClearsAPendingApproval(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:226` — `func TestLoopFailureTakesTheTerminalOwnersOrder(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:306` — `func TestAResponseMeetingAnUncommittedTerminalWritesNothing(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:324` — `func TestAResponseMeetingACommittedTerminalIsAcknowledged(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:376` — `func TestAToolResultForATerminalLoopTouchesNothing(t *testing.T) {`
- `processor/agentic-loop/terminal_owner_test.go:410` — `func TestAnApprovalWhoseRecordMovedIsRetried(t *testing.T) {`
- `processor/agentic-loop/publication_semantics_integration_test.go:125` — `func TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains(t *testing.T) {`

Tests that pin a delivery decision for a terminal-with-error result specifically (the `failTimedOutLoop` shape):

- `processor/agentic-loop/approval_loop_deadline_test.go:82` — `func TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {`
  Asserts `natsclient.DeliveryDecisionAck` for the approval lane's terminal-with-Fatal-error commit.
- `processor/agentic-loop/approval_loop_deadline_test.go:61` — `func TestAColdApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {`
- `processor/agentic-loop/approval_loop_deadline_test.go:110` — `func TestAnApprovalForAnExpiredLoopDispatchesNothing(t *testing.T) {`
  Reads the handler result directly: `State == LoopStateFailed`, `FailureState != nil`, error non-nil.
- `processor/agentic-loop/approval_loop_deadline_test.go:143` — `func TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout(t *testing.T) {`
- `processor/agentic-loop/approval_loop_deadline_test.go:219` — `func TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {`
- `processor/agentic-loop/approval_timeout_recovery_test.go:50` — `func TestAnApprovalTimeoutTakesTheCarrier(t *testing.T) {`
  Covers the sibling max-iterations-terminal-without-error shape (`handleToolsComplete`, `handlers.go:3017-3034`)
  reached through a rejection, distinct from the timeout-with-error shape above.

## Adjacent claims

- #1376 — agentic-loop: define and enforce one transition-result contract (this change's issue)
- #1381 — refactor(agentic-loop): one transition-result contract — design phase (open draft PR on this branch)
- #1374 — agentic-tools/agentic-loop terminal-owner accounting; open PR #1380 (`fix(agentic-loop): owner-committed failures record loop metrics; loop timeout reports timeout on every lane`) — proposal.md states this change's implementation waits for #1374 to merge
- #1377 — agentic-loop: make committed terminal outcomes govern recovery (named as "next" in proposal.md)
- #1365 — agentic-loop: a rebuilt loop recovers its deferred turn text and task prompt (one row in the planned contract table, per proposal.md)
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart (parent epic; rulings on issuecomment-5828357926 and issuecomment-5828511934 are the ones proposal.md cites as binding for this change)
- #1342 — fix(deliverylane): every refused delivery on a latched agentic lane is declared (open PR #1382 on a sibling branch; adjacent lane-delivery-decision territory, not this change's scope)
- `openspec list` shows exactly one open change: `agentic-loop-transition-result` (this one; "No tasks" — design phase, tasks not yet written)

## Searches

- `gopls workspace_symbol -matcher=fuzzy HandlerResult` → 20 results (struct, fields, two test-only symbols)
- `gopls references processor/agentic-loop/component.go:2245:23` (persistHandlerResult) → 18 (7 production call sites, 9 test call sites, 2 non-call matches)
- `git grep -n "HandlerResult{" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 8
- `git grep -n "result :=" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 4 relevant (`handlers.go:1124,1214,1441,2662`) + 3 unrelated (`graph_writer.go:116`, `state.go:1152`, `trajectory_recorder.go:207`)
- `git grep -n -E '(result|res)\.(State|RetryScheduled|MaxIterationsReached|Deferred|Created|CompletionState|FailureState|terminalOwnedElsewhere|staleDrop|PublishedMessages|PendingTools|SyntheticDecide) *='` (excluding `_test.go`) → 24
- `git grep -n "RetryScheduled" -- '*.go'` → 1 (declaration only; zero production writers)
- `git grep -n "RetryScheduled" -- '*_test.go'` → 2 (one read assertion, one unrelated mock-struct field)
- `git grep -n "staleDrop" -- '*.go'` (excluding `_test.go`) → 5
- `git grep -n "failTimedOutLoop" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 5 (1 definition, 1 doc-comment mention, 3 call sites: approval, model, tool)
- `git grep -n "persistHandlerResult(" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 8 (1 declaration, 7 calls)
- `git grep -n "func terminalOutcomeOf\|terminalOutcomeOf(" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 2
- `git grep -n "settleTerminalGuard(" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 5 (1 declaration, 4 calls)
- `git grep -n "commitTerminal(" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 4 (1 declaration, 3 calls)
- `git grep -n "settleFailedToolResult" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 3
- `git grep -n "handleLoopFailure" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 6
- `git grep -n "failureReasonForHandlerError" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 3
- `git grep -n "result.terminalOwnedElsewhere {" -- '*.go'` (excluding `_test.go` implicitly, none matched in tests) → 6 (4 consumer-guard sites + the 2 combined-with-error guards already counted separately)
- `git grep -n "result.State.IsTerminal() && !result.terminalOwnedElsewhere" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 2 (approval_response_handler.go:210, approval_sweeper.go:113)
- `git grep -n "carrierOrder\|writeThenPublish\|publishThenWrite" -- '*.go'` → 41 total (9 production incl. declaration/consts, 32 in tests across 14 test files)
- `git grep -c "carrierOrder" -- '*.go' | wc -l` → 1 file group count sanity check (superseded by the full listing above)
- `git grep -n "^func (c \*Component)" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 71 (used to enumerate entry-path handler functions)
- `git grep -n "func (h \*MessageHandler)" -- 'processor/agentic-loop/*.go'` grepped for `signal|verdict` → 0 (no MessageHandler-level signal or verdict handler; both live only at the Component layer)
- `git grep -n -iE "settl|publication order|birth|gate|ordinary advance|terminal owner|terminal marker|redeliver|residual" openspec/specs/agentic-loop/spec.md` → 90+ (used to locate the two Requirement headings and the ordering/residual sentences pinned above)
- `git grep -n "^### Requirement" openspec/specs/agentic-loop/spec.md` filtered to lines 1400-1650 → 2
- `git grep -n -i "recorded residual\|two residuals\|not reconciled" openspec/specs/agentic-loop/spec.md` → 2
- `git grep -n -iE "order|residual" docs/operations/migration-beta162-to-beta163.md` → 20
- `git grep -n "^func Test" processor/agentic-loop/*_test.go` filtered to `order|birth|gate|terminal.*owner|owner.*order|publish.*then|write.*then` → 64 (most unrelated to publication order; the 15 pinned above are the ones actually asserting an order or a terminal-owner sequence)
- `git grep -n "^func Test" processor/agentic-loop/*_test.go` filtered to `settled.*failed|failed.*settl|timeout.*approv|approv.*timeout|settleFailedToolResult|TerminalWithError|timedOut|TimedOut` → 16
- `gh issue list --search "transition-result" --state open --json number,title` → 12 (top hit is #1376 itself; #1146, #1374, #1377, #1365 among the rest)
- `openspec list` → 1 (`agentic-loop-transition-result`, "No tasks")
- `gh pr list --search "1376" --json number,title,headRefName,state` → 3 open PRs (#1381 this branch, #1380 for #1374, #1382 for #1342 — none overlapping this change's scope beyond #1381 itself)
- `grep -n "inventory:verify" Taskfile.yml Taskfile.yaml; find . -iname "*inventory*verify*"` → located `scripts/inventory-verify.sh` and its fixture test, read in full to confirm pin grammar before writing this file

### NOT RUN (surface larger than the call budget; named for a follow-up pass, not a conclusion)

- Full line-by-line body of `handleToolCallResponse` (handlers.go:1598-1762) — only its five `return result, err`
  producer lines were pinned, not each field mutation between them.
- `test/` (top-level integration/e2e suites) was not searched for order- or delivery-decision-pinning tests;
  only `processor/agentic-loop/*_test.go` was swept.
- `gh pr list --json number,title,body` bodies were not fetched in full; only `--search` result titles were read.
- `gopls implementation` was not run — `HandlerResult` is a struct, not an interface, so no implementer set applies;
  `carrierOrder`'s two constants were enumerated by `git grep` instead of a second `gopls` call.
- Cross-repo (sister) mentions of `HandlerResult`, `carrierOrder`, or `persistHandlerResult` were not searched —
  these are unexported/package-private and the proposal states no wire/KV/metric change, so no sister-repo seam was
  expected; not independently verified here.
