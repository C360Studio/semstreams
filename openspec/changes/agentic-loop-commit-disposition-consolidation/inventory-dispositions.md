# Inventory: agentic-loop delivery-disposition surface (#1405)

base: 3f2d4617c8d2675f6deb1e3aded420c22e959c79

Brief: `processor/agentic-loop` — how the component decides whether a delivery applies, and what it does when it
does not (`openspec/changes/agentic-loop-commit-disposition-consolidation/proposal.md`). Enumeration only, no
judgment, no verdict on which sites are true duplicates. A sibling explorer covers the terminal fact's durable
homes (`inventory-terminal-homes.md`) and is not duplicated here. Census figures cited per item are the #1146
issuecomment-5854803094 § 4 hypothesis, re-derived (not assumed) at this base; every mismatch is stated, not
resolved.

## 1. "Loop not held → read record → decide" cold arms

Held-loop accessor: `LoopManager.GetLoop` (`state.go:701`), reached through `MessageHandler.GetLoop`
(`handlers.go:3484`); a miss returns the sentinel `ErrLoopNotFound`. The governance-verdict lane's own "held" map is
different — `enforceDispatcher.lookupWaiter` (`governance_dispatcher.go:501`), a channel map keyed by execution ID,
whose miss returns `ErrNoGovernanceWaiter` — and it is the caller in `component.go`, not the dispatcher, that goes on
to read the loop record. `gopls workspace_symbol GetLoop` found 100 matches (declarations, unrelated `graph-*`
`statusMetricsLoop`/`GetLoopRunID` families, and test call sites included); `gopls references` on
`MessageHandler.GetLoop` (`handlers.go:3484:26`) found 81 call sites total. Of those, the ones below are the
"not held → read the durable record → decide" shape; two more (rows 6, 12) reach the same miss but decide without a
record read, and are listed for contrast, not as a fourth record-reading arm.

Thirteen sites found in 7 files (census: 15 in 8) — 11 read the loop record (directly via `readLoopRecord`/
`classifyMissingLoop`, or through the shared step-0 helper `adoptNewerRetainedRequest`) before deciding; 2 decide
on the miss alone, with no record read.

| # | Site (function) | File:Line | Lane | Held/miss check | Record read | Outcomes the code spells |
|---|---|---|---|---|---|---|
| 1 | `settleResponseWithoutLoop` | `component.go:2157` | model-response | `ErrLoopNotFound` from routing lookup | via `adoptNewerRetainedRequest` → `readLoopRecord` | stale → ack, `recordModelResponseDropped("stale_request_id")`; unknown → `WrapTransient` (retry); live → rebuild warm |
| 2 | `settleToolResultWithoutLoop` | `component.go:2866` | tool-result | `ErrLoopNotFound` from routing lookup | via `adoptNewerRetainedRequest` → `readLoopRecord` | stale → ack, `recordToolResultDropped("stale_execution")`; unknown → retry; live → rebuild warm |
| 3 | `settleVerdictWithoutWaiter` | `component.go:3814` | governance-verdict | `ErrNoGovernanceWaiter` from `lookupWaiter` | `readLoopRecord` directly (`component.go:3829`) | via `classifyWaiterlessVerdict`: Ack (stale/older/applied), Terminate (foreign request), Retry (live/unreadable) |
| 4 | `settleUncancellableLoop` | `component.go:3645` | cancel/signal | `errs.IsInvalid`/`ErrLoopNotFound` from `CancelLoop` | `classifyMissingLoop` (`component.go:3654`), then the marker via `adoptDurableCancel` | already-terminal → ack, `recordSignalDropped("already_terminal")`; stale → ack, `recordSignalDropped("stale_loop_id")`; live → adopt durable cancel (ack) or retry |
| 5 | `persistDeferredContinuationMarker` | `component.go:3442` | deferred-turn write | `!held` from `observedLoopRevision` (a revision map, not `GetLoop`) | `readLoopRecord` directly (`component.go:3457`) | unknown → `WrapTransient` (retry); live → writes the marker onto the read record |
| 6 | `resumeDeferredContinuation` | `component.go:3391` | deferred-turn resume | `GetLoop` err or task/prompt mismatch | none — no bucket read | ack without effect (drop), unconditionally, on any miss or mismatch |
| 7 | `classifyRedeliveredTask` | `loop_classification.go:142` | task (birth/republish) | `GetLoop` (`:155`) | `readLoopRecord` (`:158`) | unknown → `WrapTransient` (retry); stale+terminal → `taskApplied` (ack); stale+live → `taskBirth`; live task match → proceed; live task mismatch → refused, error |
| 8 | `adoptNewerRetainedRequest` | `loop_evidence.go:425` | shared step-0 (model-response, tool-result, approval) | (called only after the caller's own miss) | `readLoopRecord` (`:429`) | stale → returns record, caller drops; unknown → `WrapTransient`; live → adopts newer retained request onto the record it returns |
| 9 | `writeRecordCancelled` | `terminal_owner.go:452` | cancel, 2nd writer (inside `adoptDurableCancel`) | (called only once a durable cancel marker is found) | `readLoopRecord` (`:458`) | stale → no-op (`nil, nil`); unknown → `WrapTransient`; live → CAS-write cancelled |
| 10 | `settleTerminalGuard` | `terminal_owner.go:505` | shared terminal guard (8 callers: model-response, tool-result ×5, approval, carrier) | (called only once the result is already flagged terminal-owned-elsewhere) | `readLoopRecord` (`:506`) | stale (absent or terminal) → ack, caller's `recordDrop` callback; live/unknown → `WrapTransient` (retry) |
| 11 | `settleApprovalResponseWithoutLoop` | `approval_response_handler.go:371` | approval | `ErrLoopNotFound` from `HandleApprovalResponse` (`:205`) | via `adoptNewerRetainedRequest` | stale → ack, `recordApprovalInapplicable`; gate mismatch → ack, `recordApprovalInapplicable`; gate/record ID mismatch → `WrapFatal` (Quarantine); live match → rebuild warm or `failContinuationUnavailable` |
| 12 | approval-timeout auto-reject | `approval_sweeper.go:104-105` | approval-timeout sweep | `ErrLoopNotFound` from `HandleApprovalResponse` | none — no bucket read | logs "already released" and continues; no metric, no record read |
| 13 | `lookupWaiter` miss origin | `governance_dispatcher.go:501` | governance-verdict (feeds row 3) | channel-map miss, own held-check | none (the dispatcher itself does not read the loop record) | returns `false`; `HandleVerdict` (`:609-638`) wraps it `ErrNoGovernanceWaiter`, counted `governanceSubscribeBeforePublishFailures{missing_waiter}`, and propagates to row 3 |

- `processor/agentic-loop/state.go:701` — `func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/handlers.go:3484` — `func (h *MessageHandler) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/component.go:2157` — `func (c *Component) settleResponseWithoutLoop(ctx context.Context, requestID string) (bool, error) {`
- `processor/agentic-loop/component.go:2866` — `func (c *Component) settleToolResultWithoutLoop(ctx context.Context, toolResult agentic.ToolResult) (bool, error) {`
- `processor/agentic-loop/component.go:3814` — `func (c *Component) settleVerdictWithoutWaiter(`
- `processor/agentic-loop/component.go:3829` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/component.go:3645` — `func (c *Component) settleUncancellableLoop(ctx context.Context, loopID string, cause error) error {`
- `processor/agentic-loop/component.go:3654` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:3442` — `func (c *Component) persistDeferredContinuationMarker(ctx context.Context, loopID, prompt string) error {`
- `processor/agentic-loop/component.go:3457` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/component.go:3391` — `func (c *Component) resumeDeferredContinuation(ctx context.Context, task agentic.TaskMessage, pending HandlerResult) error {`
- `processor/agentic-loop/component.go:3392` — `entity, err := c.handler.GetLoop(pending.LoopID)`
- `processor/agentic-loop/loop_classification.go:142` — `func (c *Component) classifyRedeliveredTask(`
- `processor/agentic-loop/loop_classification.go:155` — `if _, err := c.handler.GetLoop(loopID); err == nil {`
- `processor/agentic-loop/loop_evidence.go:425` — `func (c *Component) adoptNewerRetainedRequest(ctx context.Context, loopID string) (loopRecord, error) {`
- `processor/agentic-loop/terminal_owner.go:452` — `func (c *Component) writeRecordCancelled(`
- `processor/agentic-loop/terminal_owner.go:458` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/terminal_owner.go:505` — `func (c *Component) settleTerminalGuard(ctx context.Context, result HandlerResult, recordDrop func()) error {`
- `processor/agentic-loop/terminal_owner.go:506` — `record := c.readLoopRecord(ctx, result.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:371` — `func (c *Component) settleApprovalResponseWithoutLoop(`
- `processor/agentic-loop/approval_response_handler.go:205` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/approval_sweeper.go:104` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_sweeper.go:105` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/governance_dispatcher.go:501` — `func (d *enforceDispatcher) lookupWaiter(callID string) (chan verdictArrival, bool) {`

## 2. `ErrLoopNotFound` decision sites

`gopls references` on the sentinel (`state.go:59`) found 18 code-level sites: 13 non-test across 4 files
(`approval_response_handler.go`, `approval_sweeper.go`, `component.go`, `state.go`), 5 test across 3 files. A plain
`grep -n` for the literal `ErrLoopNotFound` (comments included) finds 21 sites in exactly 5 files (adding
`handlers.go`, which mentions the sentinel only in a doc comment) — the census figure exactly, which is evidence the
original census was grep-based (prose-inclusive), not gopls-based (code-only). Of the 13 gopls-found non-test
sites, 7 are decision branches (`errors.Is(err/cause, ErrLoopNotFound)`) across 3 files (`component.go`,
`approval_response_handler.go`, `approval_sweeper.go`); 6 are creation sites — `LoopManager` methods wrapping the
sentinel into a returned error on a map miss — confined to `state.go`.

Decision sites (7):

- `processor/agentic-loop/approval_response_handler.go:205` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/approval_sweeper.go:105` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/component.go:2423` — `case errors.Is(err, ErrLoopNotFound), err == nil && held.State.IsTerminal():`
- `processor/agentic-loop/component.go:2531` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/component.go:3296` — `if !terminalWriter && (errors.Is(err, ErrLoopNotFound) || err == nil && entity.State.IsTerminal()) {`
- `processor/agentic-loop/component.go:3654` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:3662` — `if errors.Is(cause, ErrLoopNotFound) {`

Creation sites (6, all `state.go`, all `LoopManager` methods on a `m.loops[loopID]` map miss):

- `processor/agentic-loop/state.go:59` — `ErrLoopNotFound = errors.New("agentic-loop: loop not found")`
- `processor/agentic-loop/state.go:296` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:602` — `return errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:715` — `return agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "GetLoop", "find loop")`
- `processor/agentic-loop/state.go:837` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:1327` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:2072` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "CancelLoop", "find loop")`

## 3. Adopt functions

Three adopt functions (matches census) plus the marker adopt, declarations and every production caller found by
`gopls references`:

`adoptRetainedRequest` (`loop_evidence.go:340`) — a WARM-lane helper (avoids republishing a request already
retained under the exact minted `RequestID`), not a cold arm; 1 production caller.

- `processor/agentic-loop/loop_evidence.go:340` — `func (c *Component) adoptRetainedRequest(ctx context.Context, loopID, requestID string) (bool, error) {`
- `processor/agentic-loop/component.go:3023` — `published, err := c.adoptRetainedRequest(ctx, result.LoopID, msg.MsgID)`

`adoptNewerRetainedRequest` (`loop_evidence.go:425`) — the shared cold step-0 helper for item 1 rows 1, 2, 11; 3
production callers.

- `processor/agentic-loop/loop_evidence.go:425` — `func (c *Component) adoptNewerRetainedRequest(ctx context.Context, loopID string) (loopRecord, error) {`
- `processor/agentic-loop/approval_response_handler.go:375` — `record, err := c.adoptNewerRetainedRequest(ctx, loopID)`
- `processor/agentic-loop/component.go:2164` — `adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)`
- `processor/agentic-loop/component.go:2872` — `adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)`

`adoptDurableCancel` (`terminal_owner.go:391`) — the cancel lane's marker adopt; 1 production caller (item 1 row 4).

- `processor/agentic-loop/terminal_owner.go:391` — `func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {`
- `processor/agentic-loop/component.go:3666` — `adopted, err := c.adoptDurableCancel(ctx, loopID)`

`createTerminalMarker` (`terminal_owner.go:265`) — the marker adopt named in the proposal; 1 production caller,
inside `commitTerminalSteps`. Its own refusal and adoption branches:

- `processor/agentic-loop/terminal_owner.go:265` — `func (c *Component) createTerminalMarker(`
- `processor/agentic-loop/terminal_owner.go:168` — `outcome, adopted, err := c.createTerminalMarker(ctx, loopID, candidate)`
- `processor/agentic-loop/terminal_owner.go:294` — `c.logger.WarnContext(ctx, "Terminal refused — the loop's durable terminal names another loop",`
- `processor/agentic-loop/terminal_owner.go:310` — `c.logger.WarnContext(ctx, "Terminal adopted the loop's durable terminal",`
- `processor/agentic-loop/terminal_owner.go:316` — `return saved, true, nil`

## 4. Vocabulary for "this delivery is inapplicable"

Seven tokens found (matches the census count of token groups; census also states 42 uses — this measurement's sum
is 45, over 7 rows, via `gopls references` for the three Go identifiers, `git grep -c` for the string-literal
reason families):

| Token | Kind | Declaration | Non-test uses (gopls/grep) |
|---|---|---|---|
| `loopPresenceStale` | enum value | `loop_presence.go:28` | 13 |
| `staleDrop` | `HandlerResult` bool field | `handlers.go:123` | 3 |
| `terminalOwnedElsewhere` | `HandlerResult` bool field | `handlers.go:109` | 6 |
| `verdictDrop*` (7 named consts: `MissingWaiter`, `UnrecoverableIdentity`, `OlderRequest`, `AlreadyApplied`, `LoopAbsent`, `LoopTerminal`, `ForeignRequest`) | string-valued consts | `metrics.go:401-414` | 16 |
| `"stale_request_id"` | reason string literal | `component.go:2085` (first use) | 2 |
| `"stale_execution"` | reason string literal | `component.go:2883` (first use) | 1 |
| `"stale_loop_id"` / `"already_terminal"` | reason string literals (cancel lane) | `component.go:3658` / `component.go:3650` | 1 + 1 |

- `processor/agentic-loop/loop_presence.go:28` — `loopPresenceStale loopPresence = iota`
- `processor/agentic-loop/handlers.go:123` — `staleDrop bool`
- `processor/agentic-loop/handlers.go:109` — `terminalOwnedElsewhere bool`
- `processor/agentic-loop/metrics.go:401` — `verdictDropMissingWaiter         = "missing_waiter"`
- `processor/agentic-loop/metrics.go:402` — `verdictDropUnrecoverableIdentity = "unrecoverable_loop_identity"`
- `processor/agentic-loop/metrics.go:407` — `verdictDropOlderRequest   = "older_request"`
- `processor/agentic-loop/metrics.go:408` — `verdictDropAlreadyApplied = "already_applied"`
- `processor/agentic-loop/metrics.go:409` — `verdictDropLoopAbsent     = "loop_absent"`
- `processor/agentic-loop/metrics.go:410` — `verdictDropLoopTerminal   = "loop_terminal"`
- `processor/agentic-loop/metrics.go:414` — `verdictDropForeignRequest = "foreign_request"`
- `processor/agentic-loop/component.go:2085` — `c.metrics.recordModelResponseDropped("stale_request_id")`
- `processor/agentic-loop/component.go:2171` — `c.metrics.recordModelResponseDropped("stale_request_id")`
- `processor/agentic-loop/component.go:2883` — `c.metrics.recordToolResultDropped("stale_execution")`
- `processor/agentic-loop/component.go:3650` — `c.metrics.recordSignalDropped("already_terminal")`
- `processor/agentic-loop/component.go:3658` — `c.metrics.recordSignalDropped("stale_loop_id")`

## 5. Drop-metric families

Three `*prometheus.CounterVec` families found (matches census), each declared and registered once in `metrics.go`
and incremented through one wrapper method each; every lane's "drop"/"inapplicable" call funnels into one of these
three, including the approval lane (`recordApprovalInapplicable`, `recordTerminalToolResultDropped`) which both
reuse `toolResultsDropped` under a different reason string rather than a fourth counter. The governance-verdict
lane's "settled by record" outcome (`recordVerdictSettledByRecord`) increments a different, non-drop-named counter,
`governanceSubscribeBeforePublishFailures` (`metrics.go:280-293`), not one of these three.

- `processor/agentic-loop/metrics.go:34` — `toolResultsDropped  *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:167` — `toolResultsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{`
- `processor/agentic-loop/metrics.go:605` — `m.toolResultsDropped.WithLabelValues(reason).Inc()`
- `processor/agentic-loop/metrics.go:37` — `modelResponsesDropped *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:174` — `modelResponsesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{`
- `processor/agentic-loop/metrics.go:640` — `m.modelResponsesDropped.WithLabelValues(reason).Inc()`
- `processor/agentic-loop/metrics.go:38` — `signalsDropped        *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:188` — `signalsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{`
- `processor/agentic-loop/metrics.go:622` — `m.signalsDropped.WithLabelValues(reason).Inc()`

## 6. `errs.WrapFatal` sites and the lane-latch path

`git grep -c 'errs.WrapFatal(' -- processor/agentic-loop/*.go` (non-test) → 52, matching the census exactly. All 52
below. Clustered by file: `approval_response_handler.go` (3), `component.go` (21), `handlers.go` (2),
`loop_classification.go` (1), `loop_evidence.go` (13), `terminal_owner.go` (12).

- `processor/agentic-loop/approval_response_handler.go:46` — `err = errs.WrapFatal(fmt.Errorf("approval response handler panicked: %v", r),`
- `processor/agentic-loop/approval_response_handler.go:394` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/approval_response_handler.go:408` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/component.go:1443` — `return errs.WrapFatal(`
- `processor/agentic-loop/component.go:2070` — `return errs.WrapFatal(err, "agentic-loop", "handleResponseMessage",`
- `processor/agentic-loop/component.go:2195` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/component.go:2282` — `return errs.WrapFatal(established, "agentic-loop", "handleLoopFailure",`
- `processor/agentic-loop/component.go:2469` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2485` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2494` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2514` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2538` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2556` — `return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",`
- `processor/agentic-loop/component.go:2723` — `return errs.WrapFatal(`
- `processor/agentic-loop/component.go:2855` — `return errs.WrapFatal(cause, "agentic-loop", "handleToolResultMessage",`
- `processor/agentic-loop/component.go:2906` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/component.go:2998` — `return errs.WrapFatal(err, "agentic-loop", "republishPendingApproval", "build the pending approval")`
- `processor/agentic-loop/component.go:3318` — `return errs.WrapFatal(`
- `processor/agentic-loop/component.go:3452` — `return errs.WrapFatal(`
- `processor/agentic-loop/component.go:3719` — `return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "marshal cancellation after state transition")`
- `processor/agentic-loop/component.go:3725` — `return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "resolve cancellation subject after state transition")`
- `processor/agentic-loop/component.go:3739` — `return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "cancellation terminal state has unknown durability")`
- `processor/agentic-loop/handlers.go:1513` — `return result, errs.WrapFatal(`
- `processor/agentic-loop/handlers.go:3456` — `return result, errs.WrapFatal(errLoopTimedOut, "agentic-loop", op, "check timeout")`
- `processor/agentic-loop/loop_classification.go:321` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:169` — `return agentic.AgentRequest{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:187` — `return agentic.AgentRequest{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:192` — `return agentic.AgentRequest{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:226` — `return agentic.AgentResponse{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:243` — `return agentic.AgentResponse{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:248` — `return agentic.AgentResponse{}, false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:360` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:365` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:474` — `return record, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:482` — `return record, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:495` — `return record, errs.WrapFatal(`
- `processor/agentic-loop/loop_evidence.go:510` — `return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",`
- `processor/agentic-loop/loop_evidence.go:515` — `return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",`
- `processor/agentic-loop/loop_evidence.go:521` — `return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",`
- `processor/agentic-loop/terminal_owner.go:170` — `return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "create the loop's durable terminal")`
- `processor/agentic-loop/terminal_owner.go:175` — `return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "build the adopted terminal's publication")`
- `processor/agentic-loop/terminal_owner.go:180` — `return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "stamp the terminal on the graph")`
- `processor/agentic-loop/terminal_owner.go:183` — `return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "published terminal has unknown durability")`
- `processor/agentic-loop/terminal_owner.go:195` — `return errs.WrapFatal(fmt.Errorf("loop %s: %w", loopID, err),`
- `processor/agentic-loop/terminal_owner.go:205` — `return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "terminal loop record has unknown durability")`
- `processor/agentic-loop/terminal_owner.go:405` — `return false, errs.WrapFatal(fmt.Errorf("loop %s: %w", loopID, err),`
- `processor/agentic-loop/terminal_owner.go:412` — `return false, errs.WrapFatal(`
- `processor/agentic-loop/terminal_owner.go:418` — `return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "stamp the adopted cancel on the graph")`
- `processor/agentic-loop/terminal_owner.go:422` — `return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "build the adopted cancel's publication")`
- `processor/agentic-loop/terminal_owner.go:425` — `return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "published cancel has unknown durability")`
- `processor/agentic-loop/terminal_owner.go:477` — `return nil, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "marshal the cancelled loop record")`
- `processor/agentic-loop/terminal_owner.go:485` — `return nil, errs.WrapFatal(fmt.Errorf("persist cancelled loop state %s: %w", loopID, err),`

`DeliveryDecision` (proposal's cited lines 17-30, confirmed unchanged at this base):

- `natsclient/delivery_settlement.go:17` — `type DeliveryDecision uint8`
- `natsclient/delivery_settlement.go:21` — `DeliveryDecisionInvalid DeliveryDecision = iota`
- `natsclient/delivery_settlement.go:23` — `DeliveryDecisionAck`
- `natsclient/delivery_settlement.go:25` — `DeliveryDecisionRetry`
- `natsclient/delivery_settlement.go:27` — `DeliveryDecisionTerminate`
- `natsclient/delivery_settlement.go:29` — `DeliveryDecisionQuarantine`

How a `WrapFatal` error reaches the latch: within `processor/agentic-loop`, 8 sites explicitly check
`errs.IsFatal(err)` and return `natsclient.DeliveryDecisionQuarantine`; a 9th path (row 6 below) is the generic
closure that every bare-error handler (item 7) is wrapped through, which checks `errs.IsFatal` first, a
`PermanentDeliveryError` second, and falls through to Retry.

- `processor/agentic-loop/approval_response_handler.go:209` — `if errs.IsFatal(coldErr) {`
- `processor/agentic-loop/approval_response_handler.go:210` — `return natsclient.DeliveryDecisionQuarantine, wrapped`
- `processor/agentic-loop/approval_response_handler.go:236` — `if !errs.IsFatal(err) {`
- `processor/agentic-loop/approval_response_handler.go:240` — `return natsclient.DeliveryDecisionQuarantine,`
- `processor/agentic-loop/approval_response_handler.go:246` — `case errs.IsFatal(err):`
- `processor/agentic-loop/approval_response_handler.go:247` — `return natsclient.DeliveryDecisionQuarantine, wrapped`
- `processor/agentic-loop/approval_response_handler.go:287` — `if !errs.IsFatal(err) {`
- `processor/agentic-loop/approval_response_handler.go:291` — `return natsclient.DeliveryDecisionQuarantine,`
- `processor/agentic-loop/component.go:1335` — `if errs.IsFatal(handlerErr) {`
- `processor/agentic-loop/component.go:1336` — `return natsclient.DeliveryDecisionQuarantine, handlerErr`
- `processor/agentic-loop/component.go:3622` — `if errs.IsFatal(err) {`
- `processor/agentic-loop/component.go:3623` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-loop/governance_dispatcher.go:637` — `return natsclient.DeliveryDecisionQuarantine,`
- `processor/agentic-loop/governance_dispatcher.go:659` — `return natsclient.DeliveryDecisionQuarantine, fmt.Errorf("governance waiter for execution_id %q is full", executionID)`

Settlement classification (where the returned `DeliveryDecision` becomes an ack policy) and the latch itself:

- `natsclient/delivery_settlement.go:404` — `case DeliveryDecisionRetry, DeliveryDecisionTerminate, DeliveryDecisionQuarantine:`
- `natsclient/delivery_settlement.go:418` — `quarantined:     work.decision == DeliveryDecisionQuarantine,`
- `natsclient/delivery_settlement.go:419` — `ownerStopNeeded: work.decision == DeliveryDecisionQuarantine,`
- `internal/deliverylane/deliverylane.go:31` — `onFatal   func(natsclient.DeliveryResult)`
- `internal/deliverylane/deliverylane.go:81` — `if a.onFatal != nil {`
- `internal/deliverylane/deliverylane.go:82` — `a.onFatal(result)`
- `processor/agentic-loop/component.go:1091` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`

`settleTerminalGuard` declaration (above, item 3's neighbour) and its callers: `gopls references` found 8, all
production, all in `processor/agentic-loop` (census: 9 — one short; no test caller found either):

- `processor/agentic-loop/approval_response_handler.go:278` — `if err := c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped); err != nil {`
- `processor/agentic-loop/component.go:2083` — `return c.settleTerminalGuard(ctx, result, func() {`
- `processor/agentic-loop/component.go:2382` — `return c.settleTerminalGuard(ctx, result, nil)`
- `processor/agentic-loop/component.go:2424` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2476` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2536` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2546` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2788` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`

## 7. `DeliveryDecision` use vs. the same outcome by another means

Within `processor/agentic-loop`, `natsclient.DeliveryDecision` is mentioned 62 times across 3 files (`git grep -c`,
non-test): `component.go` 34, `approval_response_handler.go` 18, `governance_dispatcher.go` 10. Six function seams
construct/return it explicitly; three sibling handler seams — selected by the same `switch port.Name` at
`component.go:994-1010` — return a bare `error` instead, and are converted into a `DeliveryDecision` by one shared
closure rather than deciding it themselves.

| Seam | File:Line | Shape |
|---|---|---|
| `handleApprovalResponseMessage` | `approval_response_handler.go:186` | returns `(DeliveryDecision, error)` directly |
| `handleSignalMessage` | `component.go:3591` | returns `(DeliveryDecision, error)` directly |
| `handleToolCallVerdictMessage` | `component.go:3771` | returns `(DeliveryDecision, error)` directly |
| `settleVerdictWithoutWaiter` | `component.go:3814` | returns `(DeliveryDecision, error)` directly (item 1 row 3) |
| `GovernanceDispatcher.HandleVerdict` (3 impls: disabled/audit/enforce) | `governance_dispatcher.go:334,393,430,609` | interface + 3 impls return `(DeliveryDecision, error)` |
| `handleTaskMessage` | `component.go:1492` | returns bare `error` |
| `handleResponseMessage` | `component.go:2011` | returns bare `error` |
| `handleToolResultMessage` | `component.go:2680` | returns bare `error` |
| generic closure converting the three above | `component.go:1325-1342` | `handlerErr := handler(...)`; nil → Ack; `errs.IsFatal` → Quarantine; `PermanentDeliveryError` → Terminate; else → Retry |

- `processor/agentic-loop/approval_response_handler.go:186` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:3591` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:3771` — `func (c *Component) handleToolCallVerdictMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:334` — `HandleVerdict(decision, executionID string, verdict VerdictPayload) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:1492` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:2011` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:2680` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:185` — `type inputHandler func(context.Context, []byte) error`
- `processor/agentic-loop/component.go:991` — `settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:999` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:1001` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/component.go:1407` — `err := c.handleTaskMessage(workCtx, data)`
- `processor/agentic-loop/component.go:1325` — `func(workCtx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1326` — `handlerErr := handler(workCtx, data)`
- `processor/agentic-loop/component.go:1328` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/component.go:1335` — `if errs.IsFatal(handlerErr) {`
- `processor/agentic-loop/component.go:1336` — `return natsclient.DeliveryDecisionQuarantine, handlerErr`
- `processor/agentic-loop/component.go:1338` — `var permanent *natsclient.PermanentDeliveryError`
- `processor/agentic-loop/component.go:1340` — `return natsclient.DeliveryDecisionTerminate, handlerErr`
- `processor/agentic-loop/component.go:1342` — `return natsclient.DeliveryDecisionRetry, handlerErr`

Not pinned individually: the other ~50 `DeliveryDecision` mentions in these three files (constructions inside the
lane-specific handler bodies themselves, e.g. `settleUncancellableLoop`'s and `handleToolCallVerdictMessage`'s own
`Ack`/`Retry`/`Terminate` returns) — out of this item's volume budget; the seam-level table above is the
representative surface, not an exhaustive return-statement census.

## 8. Ad hoc test hooks

Four function-valued struct fields found, one per PR, matching the census exactly: 3 on `Component`
(`component.go`), 1 on `MessageHandler` (`handlers.go`).

- `processor/agentic-loop/component.go:167` — `testPublishHook func(subject string, data []byte)`
- `processor/agentic-loop/component.go:170` — `testLineageWriteHook func(context.Context, string, map[string]any) error`
- `processor/agentic-loop/component.go:180` — `testCarrierHook func(loopID, stage string)`
- `processor/agentic-loop/handlers.go:202` — `testApprovedDispatchHook func(loopID, stage string)`

Tests that set them — `testPublishHook` is set through one exported test-only setter (`SetTestPublishHook`,
`export_test.go`) used by multiple test files including `approval_sweeper_test.go`; the other three are set inline,
per test:

- `processor/agentic-loop/export_test.go:47` — `c.testPublishHook = fn`
- `processor/agentic-loop/spawn_identity_failure_test.go:253` — `c.testLineageWriteHook = func(_ context.Context, loopID string, _ map[string]any) error {`
- `processor/agentic-loop/lineage_preflight_test.go:215` — `component.testLineageWriteHook = func(context.Context, string, map[string]any) error {`
- `processor/agentic-loop/carrier_terminal_race_integration_test.go:587` — `c.testCarrierHook = pause.hook`
- `processor/agentic-loop/carrier_terminal_race_integration_test.go:468` — `lane.h.testApprovedDispatchHook = pause.hook`

## Searches

- `git rev-parse HEAD` → `3f2d4617c8d2675f6deb1e3aded420c22e959c79`
- `gh api repos/C360Studio/semstreams/issues/comments/5854803094 --jq .body` → 1 (the census source)
- `ls processor/agentic-loop/*.go` (non-test) → 30 files
- `gopls workspace_symbol -matcher=fuzzy GetLoop` → 100
- `grep -n "func (c \*Component) GetLoop\b" processor/agentic-loop/*.go` → 0
- `grep -n "func.*GetLoop\|func.*HeldLoop\|func.*loops\[" processor/agentic-loop/loop_presence.go processor/agentic-loop/component.go` → 0
- `grep -n "GetLoop\|loops \+\|held loop\|heldLoop" processor/agentic-loop/component.go` → 12
- `gopls references processor/agentic-loop/handlers.go:3484:26` (`MessageHandler.GetLoop`) → 81
- `grep -n "func (c \*Component) readLoopRecord" processor/agentic-loop/loop_evidence.go` → 1
- `gopls references processor/agentic-loop/loop_evidence.go:282:21` (`readLoopRecord`) → 10
- `grep -n "func (c \*Component) classifyMissingLoop" processor/agentic-loop/loop_presence.go` → 1
- `gopls references processor/agentic-loop/loop_presence.go:70:24` (`classifyMissingLoop`) → 4
- `grep -n "loopsBucket.Get(" processor/agentic-loop/*.go` (non-test) → 3
- `grep -n "GetLoopForRequestWithRecovery\|GetLoopForToolCallWithRecovery\|GetLoopForRequest(\|GetLoopForToolCall(" processor/agentic-loop/*.go` (non-test) → 8
- `grep -n "ErrLoopNotFound" processor/agentic-loop/*.go` (non-test, incl. comments) → 21 (5 files — reproduces the census exactly)
- `grep -l "ErrLoopNotFound" processor/agentic-loop/*.go | grep -v _test.go` → 5 files (`approval_response_handler.go`, `approval_sweeper.go`, `component.go`, `handlers.go`, `state.go`)
- `gopls references processor/agentic-loop/state.go:59:2` (`ErrLoopNotFound`) → 18
- `grep -n "NotFound" processor/agentic-loop/terminal_owner.go processor/agentic-loop/handlers.go processor/agentic-loop/loop_classification.go processor/agentic-loop/loop_evidence.go processor/agentic-loop/trajectory_handler_wiring.go` → 4 (all `jetstream.ErrKeyNotFound`/`ErrMsgNotFound`/comment — a different sentinel)
- `grep -n "loopPresence" processor/agentic-loop/*.go` (non-test, excl. `loop_presence.go` itself) → 24
- `grep -n "^func (c \*Component) adopt" processor/agentic-loop/*.go` (non-test) → 3
- `gopls references processor/agentic-loop/loop_evidence.go:340:21` (`adoptRetainedRequest`) → 4
- `gopls references processor/agentic-loop/loop_evidence.go:425:21` (`adoptNewerRetainedRequest`) → 16
- `gopls references processor/agentic-loop/terminal_owner.go:391:21` (`adoptDurableCancel`) → 1
- `gopls references processor/agentic-loop/terminal_owner.go:265:21` (`createTerminalMarker`) → 3
- `grep -n "verdictDrop\|staleDrop\|terminalOwnedElsewhere\|recordSignalDropped\|recordModelResponseDropped\|recordToolResultDropped\|recordApprovalInapplicable\|recordVerdictSettledByRecord\|recordTerminalToolResultDropped" processor/agentic-loop/*.go` (non-test, all mentions incl. call sites) → 68
- `git grep -n '"already_terminal"'` (non-test) → 1; `'"stale_request_id"'` → 3; `'"stale_execution"'` → 2; `'"stale_loop_id"'` → 1; `staleDrop` → 5; `terminalOwnedElsewhere` → 8; `loopPresenceStale` → 15; `verdictDrop` → 16; `recordApprovalInapplicable` → 5; `Inapplicable` → 6
- `gopls references processor/agentic-loop/loop_presence.go:28:2` (`loopPresenceStale`) → 15
- `gopls references processor/agentic-loop/handlers.go:123:2` (`staleDrop`) → 8
- `gopls references processor/agentic-loop/handlers.go:109:2` (`terminalOwnedElsewhere`) → 8
- `grep -n "Dropped\s*\*prometheus\|prometheus.NewCounterVec\|prometheus.NewCounter(" processor/agentic-loop/metrics.go` → 23
- `grep -n "toolResultsDropped\|modelResponsesDropped\|signalsDropped" processor/agentic-loop/*.go` (non-test) → 15
- `git grep -n 'errs.WrapFatal(' -- processor/agentic-loop/*.go` (non-test) → 52
- `grep -n "errs.IsFatal\|DeliveryDecisionQuarantine" processor/agentic-loop/*.go` (non-test) → 16
- `gopls references processor/agentic-loop/terminal_owner.go:505:21` (`settleTerminalGuard`) → 8
- `grep -c "natsclient.DeliveryDecision" processor/agentic-loop/*.go` (non-test, per file) → `approval_response_handler.go` 18, `component.go` 34, `governance_dispatcher.go` 10
- `grep -n "(natsclient.DeliveryDecision, error)" processor/agentic-loop/*.go` (non-test) → 11
- `grep -n "^func (c \*Component) handle.*Message(ctx context.Context, data \[\]byte)" processor/agentic-loop/*.go` (non-test) → 6
- `grep -n "handleTaskMessage\|handleResponseMessage\|handleToolResultMessage" processor/agentic-loop/*.go` (non-test, all mentions incl. comments) → 32
- `gopls references processor/agentic-loop/component.go:185:6` (`inputHandler` type) → 21
- `grep -n "func newLoopHeartbeatDeliveryPolicy" processor/agentic-loop/*.go` → 1
- `grep -n "testHook\|TestHook\|testCarrierHook" processor/agentic-loop/component.go` (non-test) → 5
- `grep -rn "^\s*\(test\|on[A-Z]\).*func(" processor/agentic-loop/*.go` (non-test) → 4
- `git grep -ln testPublishHook -- processor/agentic-loop/*_test.go` → 2 files
- `git grep -ln testLineageWriteHook -- processor/agentic-loop/*_test.go` → 2 files
- `git grep -ln testCarrierHook -- processor/agentic-loop/*_test.go` → 1 file
- `git grep -ln testApprovedDispatchHook -- processor/agentic-loop/*_test.go` → 1 file
- `grep -n "latch" natsclient/*.go processor/agentic-loop/*.go` (non-test) → 26
- `find . -type d -name deliverylane` → 1 (`internal/deliverylane`)
- `grep -rn "DeliveryDecisionQuarantine\|onFatal\|OnFatal" natsclient/*.go internal/deliverylane/*.go` (non-test) → 17
- `gopls references processor/agentic-loop/governance_dispatcher.go:501:25` (`lookupWaiter`) → 11
- `gopls references processor/agentic-loop/governance_dispatcher.go:346:5` (`ErrNoGovernanceWaiter`) → 9
- `openspec/project.md` Purpose section read (not a search; scope-setting read per contract step 1)
