# Inventory: agentic-loop-committed-terminal-recovery (#1377, PR #1388)
base: 597072c4f0bbcf2f29187229145e01d819144d6a

Brief: enumerate the surface for W1-W4 (lost record CAS, timer-publication failure, held-loop cancel/approval race,
benign-cancel lane quarantine) named on #1377 and the proposal at this base. Design base `9e5d8455` (`refactor
(agentic-loop): one transition-result contract`, #1381) is identical to HEAD on every production file pinned below;
`git diff 9e5d8455 597072c4 --stat` shows only `proposal.md` and the probe test added. No judgment, no options, no
verdict — enumeration only. Pin grammar note: each bullet below is exactly one physical line, `` `path:line` —
`substring` ``; prose that needs more than one fact uses plain numbered paragraphs (no leading `-`) with the pins
listed on their own lines directly after.

## 1. The terminal owner and its ordered effects

`commitTerminal` is `terminal_owner.go:151`; its four steps run inside `commitTerminalSteps` (`:166`). The issue's
original pin (`:146`) now lands mid-comment ("record."), not the func line — DRIFT since #1362 landed; re-pinned
below at current lines.

- `processor/agentic-loop/terminal_owner.go:151` — `func (c *Component) commitTerminal(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
- `processor/agentic-loop/terminal_owner.go:166` — `func (c *Component) commitTerminalSteps(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`

Step 1, marker: `createTerminalMarker` (`:262`) is called at `:168`; its `Create(COMPLETE_<loopID>)` is at `:274`. A
refused Create (conflict) reads the saved marker back and adopts it by loop ID + kind; a mismatched identity is
refused, fatal.

- `processor/agentic-loop/terminal_owner.go:168` — `outcome, adopted, err := c.createTerminalMarker(ctx, loopID, candidate)`
- `processor/agentic-loop/terminal_owner.go:262` — `func (c *Component) createTerminalMarker(`
- `processor/agentic-loop/terminal_owner.go:274` — `_, err = c.loopsBucket.Create(ctx, key, data)`

Step 2, graph: `stampTerminal` (`:335`) is called at `:179`. Step 3, publish: `publishResults` is called at `:182`.
Step 4, record: `persistLoopState` is called at `:198`; on success `recordCommittedTerminal` is called at `:207`.

- `processor/agentic-loop/terminal_owner.go:179` — `if err := c.stampTerminal(ctx, loopID, outcome); err != nil {`
- `processor/agentic-loop/terminal_owner.go:335` — `func (c *Component) stampTerminal(ctx context.Context, loopID string, outcome terminalOutcome) error {`
- `processor/agentic-loop/terminal_owner.go:182` — `if err := c.publishResults(ctx, publication); err != nil {`
- `processor/agentic-loop/terminal_owner.go:198` — `if err := c.persistLoopState(ctx, loopID); err != nil {`
- `processor/agentic-loop/terminal_owner.go:207` — `c.recordCommittedTerminal(held, outcome)`

Failure handling: step 1's own marshal/Create/decode failures are Fatal; step 2/3 failures are also Fatal; step 4's
CAS loss is returned as-is (transient, loop already released); every other step-4 failure is Fatal. `commitTerminal`
releases the loop's transient state on any failure except a lost CAS.

The "accepted CAS-loss residual" (W1) sits at `component.go:2249`, inside `persistHandlerResult`'s terminal branch.
Issue pin `:2261` is now blank — DRIFT since #1381 landed; the residual paragraph is at `component.go:2242-2250`
today.

- `processor/agentic-loop/component.go:2249` — `residual (#1362 issuecomment-5808903072; migration`

Terminal-record writers (three, not one). Writer 1: `commitTerminalSteps`'s own step 4, always followed by
`recordCommittedTerminal`, which has exactly two production callers (confirmed by `gopls references`).

- `processor/agentic-loop/terminal_owner.go:225` — `func (c *Component) recordCommittedTerminal(entity agentic.LoopEntity, outcome terminalOutcome) {`

Writer 2: `writeRecordCancelled`, the cancel lane's cold-branch second writer, reached only from
`adoptDurableCancel` when this process does not hold the loop.

- `processor/agentic-loop/terminal_owner.go:441` — `func (c *Component) writeRecordCancelled(`
- `processor/agentic-loop/terminal_owner.go:385` — `func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {`
- `processor/agentic-loop/terminal_owner.go:468` — `if _, err := c.loopsBucket.Update(ctx, loopID, data, record.revision); err != nil {`
- `processor/agentic-loop/terminal_owner.go:421` — `written, err := c.writeRecordCancelled(ctx, loopID, saved.cancelled)`
- `processor/agentic-loop/terminal_owner.go:429` — `c.recordCommittedTerminal(*written, saved)`

Writer 3, the W3 second writer, OUTSIDE the owner: `publishThenPersistResultState`, reached from
`persistHandlerResult` whenever `result.State` is `LoopStateCancelled`. `persistHandlerResult`'s own `terminal`
predicate tests only Complete/Failed, never Cancelled, so a Cancelled-state `HandlerResult` (the shape
`dispatchApprovedCall`/`checkApprovalGate` produce when they observe the loop cancelled in memory mid-dispatch)
takes the non-terminal branch and CAS-writes the record through `persistLoopState` — never through `commitTerminal`,
never through `recordCommittedTerminal`. This is exactly the shape the probe's
`TestProbeW3HeldLoopCancelDuringApprovalDispatch` observes.

- `processor/agentic-loop/component.go:2306` — `func (c *Component) publishThenPersistResultState(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:2218` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:2219` — `terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed`
- `processor/agentic-loop/component.go:3050` — `data, err := c.marshalLoopRecord(loopID)`
- `processor/agentic-loop/component.go:3071` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`

`failedTerminal` is the OTHER predicate in this file: `err != nil && result.State.IsTerminal() &&
!result.terminalOwnedElsewhere`, and `LoopState.IsTerminal()` DOES include Cancelled. The two predicates disagree on
Cancelled: `failedTerminal` would call a cancelled-with-error result terminal, but `persistHandlerResult`'s own
routing predicate (no error involved) excludes Cancelled outright.

- `processor/agentic-loop/handlers.go:2645` — `func failedTerminal(result HandlerResult, err error) bool {`
- `agentic/state.go:44` — `func (s LoopState) IsTerminal() bool {`
- `processor/agentic-loop/approval_response_handler.go:210` — `if failedTerminal(result, err) {`
- `processor/agentic-loop/approval_sweeper.go:113` — `if failedTerminal(result, err) {`
- `processor/agentic-loop/component.go:2616` — `if failedTerminal(result, cause) {`

## 2. Durable facts that could decide "terminal has committed"

Bucket: `AGENT_LOOPS`, acquired via `loopbucket.AcquireOwner`, created with `History: 10, TTL: 24h` if absent. Both
the loop record (key = loopID) and the `COMPLETE_<loopID>` marker live in this ONE bucket.

- `processor/agentic-loop/config.go:426` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/internal/loopbucket/acquire.go:14` — `func AcquireOwner(ctx context.Context, js jetstream.KeyValueManager, name string) (jetstream.KeyValue, error) {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:20` — `bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})`

Fact 1, `COMPLETE_<loopID>` marker. Writer: the ONE production `Create` site. Reader: the ONE production `Get` site,
reached only from the cancel lane's cold branch (`settleUncancellableLoop`, on `ErrLoopNotFound`). No warm lane and
no other cold branch reads the marker before advancing.

- `processor/agentic-loop/terminal_owner.go:84` — `func terminalMarkerKey(loopID string) string {`
- `processor/agentic-loop/terminal_owner.go:389` — `entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))`
- `processor/agentic-loop/component.go:3315` — `adopted, err := c.adoptDurableCancel(ctx, loopID)`
- `processor/agentic-loop/component.go:3294` — `func (c *Component) settleUncancellableLoop(ctx context.Context, loopID string, cause error) error {`

Fact 2, the loop record's `State` field, read via `readLoopRecord` (8 non-test production callers by `gopls
references`).

- `processor/agentic-loop/loop_evidence.go:282` — `func (c *Component) readLoopRecord(ctx context.Context, loopID string) loopRecord {`
- `processor/agentic-loop/loop_presence.go:70` — `func (c *Component) classifyMissingLoop(ctx context.Context, loopID string) loopPresence {`
- `processor/agentic-loop/component.go:3303` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/terminal_owner.go:447` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/terminal_owner.go:490` — `record := c.readLoopRecord(ctx, result.LoopID)`

Fact 3, the gated record (`PendingApproval` / `awaiting_approval`), read by `settleApprovalResponseWithoutLoop`.

- `processor/agentic-loop/approval_response_handler.go:360` — `func (c *Component) settleApprovalResponseWithoutLoop(`

Fact 4, the terminal event on the stream, published by `terminalPublication`.

- `processor/agentic-loop/terminal_owner.go:315` — `func (c *Component) terminalPublication(loopID string, outcome terminalOutcome) ([]PublishedMessage, error) {`

Fact 5, `TOOL_CALL_OUTCOMES`, declared and cataloged at History 1 (the `owned` closure default, no override for this
bucket).

- `graph/constants.go:50` — `BucketToolCallOutcomes = "TOOL_CALL_OUTCOMES"`
- `graph/kvcatalog.go:150` — `owned(BucketToolCallOutcomes, "agentic-tools",`
- `graph/kvcatalog.go:50` — `History:     1,`

## 3. W1 — lost record CAS

The record CAS is `persistLoopState`'s own `c.loopsBucket.Update`, compared against the revision
`observedLoopRevision` returned. `commitTerminalSteps` step 4 calls this same function rather than writing directly.
On conflict `persistLoopState` releases the loop and returns a transient wrap around
`natsclient.ErrKVRevisionMismatch`; `commitTerminal` passes that through unchanged.

- `processor/agentic-loop/component.go:3025` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`

The residual (accepted, #1362 issuecomment-5808903072) is documented in the same block pinned in § 1: a
compare-and-swap lost to a writer that moved the record to a LATER request is "not reconciled" — the redelivered
terminal input then classifies as older and is acknowledged, so the marker and event that already landed are never
retracted. No input path consults `COMPLETE_<loopID>` before advancing except the cancel-lane cold branch (§ 2 fact
1) — a model/tool/approval lane that meets a record moved past its revision re-reads the record and reclassifies
from THAT, never from the marker.

## 4. W2 — timer-driven terminal publication fails after commitment

`approval_sweeper.go`: issue pin `:113` still lands on the `failedTerminal` check, no drift. The sweeper is
memory-only by design: it reads only the loops THIS process holds, and no startup pass reads `AGENT_LOOPS` to
restore the ones it does not.

- `processor/agentic-loop/approval_sweeper.go:55` — `func (c *Component) runApprovalTimeoutSweeper(ctx context.Context) {`
- `processor/agentic-loop/approval_sweeper.go:41` — `// Memory-only, deliberately (#1330, docket OQ2): the sweep reads the`
- `processor/agentic-loop/approval_sweeper.go:77` — `func (c *Component) sweepExpiredApprovals(ctx context.Context) {`
- `processor/agentic-loop/approval_sweeper.go:81` — `candidates := c.handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())`
- `processor/agentic-loop/approval_sweeper.go:104` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_sweeper.go:123` — `if commitErr := c.persistHandlerResult(ctx, result); commitErr != nil {`
- `processor/agentic-loop/approval_sweeper.go:158` — `if err := c.persistHandlerResult(ctx, result); err != nil {`

The publication-failure residual (accepted, #1362 issuecomment-5809906669) is the comment block naming OQ-B.

- `processor/agentic-loop/approval_sweeper.go:152` — `// A failure is logged, not counted, and not retried (OQ-B): a timer`

Next-tick behaviour: `sweepExpiredApprovals` re-snapshots loops THIS process still holds in memory. A loop whose
commit failed after the marker Create released the loop's transient state, so it drops out of the in-memory
snapshot and the sweeper does NOT revisit it on the next tick; nothing in `sweepExpiredApprovals` checks
`COMPLETE_<loopID>` before or after building its candidate list.
`TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer` proves the record recovers only when a NEW answer is
redelivered, not through any self-driven sweeper adoption — exactly the gap docket option (b) in the proposal
names: no such adoption exists on this tree today.

- `processor/agentic-loop/approval_loop_deadline_test.go:219` — `func TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {`

## 5. W3 / W4 seams (approval, cancel, and dispatch)

Approval lane:

- `processor/agentic-loop/approval_response_handler.go:57` — `pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID, response.ExecutionID)`
- `processor/agentic-loop/approval_response_handler.go:83` — `entity, getErr := h.GetLoop(loopID)`
- `processor/agentic-loop/approval_response_handler.go:101` — `if h.loopManager.IsTimedOut(loopID) {`
- `processor/agentic-loop/approval_response_handler.go:107` — `return result, h.dispatchApprovedCall(loopID, pending, pending.Arguments, response.ApprovedBy, &result)`
- `processor/agentic-loop/approval_response_handler.go:113` — `return result, h.dispatchApprovedCall(loopID, pending, args, response.ApprovedBy, &result)`
- `processor/agentic-loop/approval_response_handler.go:128` — `func (h *MessageHandler) dispatchApprovedCall(loopID string, pending agentic.PendingApprovalState, args map[string]any, approvedBy string, result *HandlerResult) error {`
- `processor/agentic-loop/approval_response_handler.go:221` — `err = c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/approval_response_handler.go:262` — `if result.terminalOwnedElsewhere {`
- `processor/agentic-loop/approval_response_handler.go:267` — `if err := c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:272` — `if err := c.persistHandlerResult(ctx, result); err != nil {`

Dispatch: `AddPendingTool` at `:2036` matches the probe's cited pre-pause point exactly, no drift; the
`resolveRunEntityID` call at `:2067` and the Warn it emits at `:632` match the probe's cited pause point exactly.

- `processor/agentic-loop/handlers.go:2035` — `func (h *MessageHandler) dispatchToolCall(result *HandlerResult, loopID string, tc agentic.ToolCall) error {`
- `processor/agentic-loop/handlers.go:2036` — `if err := h.loopManager.AddPendingTool(loopID, tc.ID); err != nil {`
- `processor/agentic-loop/handlers.go:2065` — `if runID := h.loopManager.GetRunID(loopID); runID != "" {`
- `processor/agentic-loop/handlers.go:2067` — `if runEntityID := h.resolveRunEntityID(runID); runEntityID != "" {`
- `processor/agentic-loop/handlers.go:632` — `h.logger.Warn("resolveRunEntityID: platform identity missing, RunEntityID will be empty",`
- `processor/agentic-loop/handlers.go:1804` — `func (h *MessageHandler) drainPendingToolFailures(loopID, reason string) {`
- `processor/agentic-loop/handlers.go:2388` — `h.drainPendingToolFailures(loopID, fmt.Sprintf("loop failed: %s", reason))`
- `processor/agentic-loop/handlers.go:3021` — `h.drainPendingToolFailures(loopID, "max iterations reached before tool results returned")`

Persistence chain for the W4 Fatal: `marshalLoopRecord`'s own `GetLoop` call produces the exact string the probe
asserts in `health.LastError` ("get loop %s for persistence").

- `processor/agentic-loop/component.go:3167` — `func (c *Component) marshalLoopRecord(loopID string) ([]byte, error) {`
- `processor/agentic-loop/component.go:3168` — `entity, err := c.handler.GetLoop(loopID)`
- `processor/agentic-loop/approval_response_handler.go:281` — `fmt.Errorf("approval result for loop %q has unknown durable state: %w", response.LoopID, err)`

Cancel lane:

- `processor/agentic-loop/component.go:3327` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) error {`
- `processor/agentic-loop/component.go:3336` — `c.handler.drainPendingToolFailures(loopID, fmt.Sprintf("loop cancelled by %s", signal.UserID))`
- `processor/agentic-loop/component.go:3339` — `entity, err := c.handler.CancelLoop(loopID, signal.UserID)`
- `processor/agentic-loop/state.go:1925` — `func (m *LoopManager) CancelLoop(loopID, cancelledBy string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/handlers.go:3436` — `func (h *MessageHandler) CancelLoop(loopID, cancelledBy string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/component.go:3381` — `if err := c.commitTerminal(ctx, terminalOutcome{cancelled: &completion}, HandlerResult{`

Candidate seams for a re-check of the loop's terminal state (listed, not chosen, per the brief): (1)
`approval_response_handler.go:83`, the `GetLoop` right after the resolve race is won, before `IsTimedOut`; (2)
`handlers.go:2036`, the literal pre-`AddPendingTool` point the probe's own header says has NO seam today; (3)
`component.go:2219`, `persistHandlerResult`'s own `terminal` predicate, where a Cancelled state currently falls
through to the non-terminal branch; (4) `terminal_owner.go:389`, `adoptDurableCancel`'s Get, currently scoped to the
cancel lane only.

The latch: `recordDeliveryOwnerFatal` sets `c.deliveryFatalErr` once and never resets it in this file;
`internal/deliverylane`'s `Admission.Latch` is the ONLY `Latch` method in that package, with no `Unlatch` under any
spelling searched. `recordDeliveryRefused` is the log line the probe asserts, and it drains rather than stops the
exact handle. `loopPolicyHandle` is a TEST-ONLY fake `jetstream.ConsumeContext`, not a production type.

- `processor/agentic-loop/component.go:1071` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `internal/deliverylane/deliverylane.go:70` — `func (a *Admission) Latch(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/component.go:1093` — `c.logger.Warn("Loop delivery refused by latched lane",`
- `processor/agentic-loop/consumer_policy_test.go:23` — `type loopPolicyHandle struct {`

## 6. The tools executor's side of W3

`handleToolCall` checks `TOOL_CALL_OUTCOMES` BEFORE admission, approval-filter, or execution; a `found` outcome
short-circuits straight to `publishCompletedResult` (idempotent replay), never re-executing. No call in
`handleToolCall` reads `AGENT_LOOPS` or any loop-state field before executing: admission and the approval filter
both key off the loop's cached advertised-tool set, not its terminal state. The executor therefore has no knowledge
of whether the loop that dispatched a `tool.execute` is already terminal when it runs.

- `processor/agentic-tools/component.go:703` — `func (c *Component) handleToolCall(ctx context.Context, data []byte) error {`
- `processor/agentic-tools/component.go:742` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:839` — `func (c *Component) loadCompletedOutcome(`
- `processor/agentic-tools/component.go:1071` — `func (c *Component) admitToolCall(call agentic.ToolCall) *toolAdmissionRejection {`

`tool_results_dropped_total{reason="terminal_unproven"}` is entirely on the agentic-loop side; its only two
production callers pass it as the `recordDrop` callback to `settleTerminalGuard`, and one separate direct call site
bypasses the wrapper.

- `processor/agentic-loop/terminal_owner.go:508` — `func (c *Component) recordTerminalToolResultDropped() {`
- `processor/agentic-loop/terminal_owner.go:510` — `c.metrics.recordToolResultDropped("terminal_unproven")`
- `processor/agentic-loop/component.go:2560` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/loop_classification.go:296` — `c.metrics.recordToolResultDropped("terminal_unproven")`

## 7. Observation seams

- `processor/agentic-loop/metrics.go:101` — `Name:      "loops_failed_total",`
- `processor/agentic-loop/metrics.go:108` — `Name:      "active_loops",`
- `processor/agentic-loop/metrics.go:166` — `Name:      "tool_results_dropped_total",`
- `processor/agentic-loop/metrics.go:187` — `Name:      "signals_dropped_total",`
- `processor/agentic-loop/metrics.go:510` — `m.activeLoops.Inc()`
- `processor/agentic-loop/metrics.go:523` — `func (m *loopMetrics) recordLoopFailed(reason string, iterations int, durationSeconds float64) {`
- `processor/agentic-loop/metrics.go:525` — `m.activeLoops.Dec()`
- `processor/agentic-loop/metrics.go:615` — `func (m *loopMetrics) recordSignalDropped(reason string) {`
- `processor/agentic-loop/component.go:527` — `LastError:  lastError,`
- `test/e2e/scenarios/agentic/approval_signal.go:539` — `const msgTerminatedAdvisoryPrefix = "$JS.EVENT.ADVISORY.CONSUMER.MSG_TERMINATED."`

The MSG_TERMINATED advisory pattern is the #1238 e2e stage's mechanism for proving a delivery was TERMINATED rather
than silently dropped or redelivered; Health `LastError`'s value is the Fatal chain the probe's
`TestProbeW4ReleasedLoopQuarantinesApprovalLane` asserts (§ 5).

## 8. Spec and residual text to replace

- `openspec/specs/agentic-loop/spec.md:1618` — `is not reconciled: the`
- `openspec/specs/agentic-loop/spec.md:1621` — `and then fails to publish is not reconciled either`
- `docs/operations/migration-beta162-to-beta163.md:2015` — `Two residuals, recorded and not reconciled`
- `docs/operations/migration-beta162-to-beta163.md:2023` — `and then fails to publish is not reconciled either`

The full W1 sentence spans spec.md `:1617-1619`, opening "A terminal whose record update loses its compare-and-swap
after `COMPLETE_<loopID>` and its event have landed is not reconciled". The full W2 sentence spans `:1620-1623`,
opening "An approval-timeout sweep terminal ... that commits `COMPLETE_<loopID>` and then fails to publish is not
reconciled either".

No transition-result table row for #1377 exists in-tree under any spelling searched — every `transition-result` hit
is inside the archived `2026-09-25-agentic-loop-transition-result` change directory, none a per-window table. The
proposal states the per-window table is written onto the GitHub epic issue #1146 from this change's docket, not
into a tracked file.

- `openspec/changes/agentic-loop-committed-terminal-recovery/proposal.md:63` — `the epic's per-window table on #1146 is written from the same docket.`

## 9. Tests already on these paths

- `processor/agentic-loop/partial_publish_settlement_integration_test.go:28` — `func TestIntegrationPartialPublishQuarantinesRatherThanRetrying(t *testing.T) {`
- `processor/agentic-loop/persist_handler_result_test.go:16` — `type failingLoopBucket struct {`
- `processor/agentic-loop/persist_handler_result_test.go:82` — `c := &Component{handler: handler, loopsBucket: failingLoopBucket{err: want}}`
- `processor/agentic-loop/delivery_owner_test.go:283` — `c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}`
- `processor/agentic-loop/partial_publish_settlement_integration_test.go:104` — `c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}`
- `processor/agentic-loop/persist_handler_result_test.go:55` — `func TestPersistHandlerResultReleasesTheLoopWhenItsTerminalPublicationFails(t *testing.T) {`
- `processor/agentic-loop/approval_sweeper_test.go:243` — `func TestSnapshotExpiredApprovals_StableUnderRepeatedSweeps(t *testing.T) {`
- `processor/agentic-loop/approval_sweeper_test.go:260` — `func TestRunApprovalTimeoutSweeper_ExitsOnContextCancel(t *testing.T) {`
- `processor/agentic-loop/approval_sweeper_test.go:295` — `func TestSweepExpiredApprovals_PublishesApprovalResponseToWire(t *testing.T) {`
- `processor/agentic-loop/terminal_release_test.go:563` — `func TestApprovalSweepUnaffectedByTerminalRelease(t *testing.T) {`
- `test/e2e/scenarios/agentic/approval_restart.go:188` — `func (s *Scenario) verifyApprovalAcrossReplacement(`
- `processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:373` — `func TestProbeW3HeldLoopCancelDuringApprovalDispatch(t *testing.T) {`
- `processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:467` — `func TestProbeW4ReleasedLoopQuarantinesApprovalLane(t *testing.T) {`

`TestIntegrationPartialPublishQuarantinesRatherThanRetrying` proves a batch publish that fails partway quarantines
rather than retrying (general partial-publish shape, not terminal-specific). `failingLoopBucket` is a test fixture
(a `jetstream.KeyValue` whose Put/Create/Update always error) used at the three call sites above to force a
commit-unknown write. `TestPersistHandlerResultReleasesTheLoopWhenItsTerminalPublicationFails` proves the loop is
released, not left latched terminal in memory, when the terminal owner's publish step fails. The sweeper tests cover
snapshot stability, ticker lifecycle, and wire echo, none touching the marker-exists-record-gated recovery gap.
`verifyApprovalAcrossReplacement` is the process-replacement approval walk (an e2e stage method, not a `go test`
`Test*` func); it exercises the cold branch across a real process kill/restart, not the W3 held-loop race (same
process, two lanes) the probe forces. `test/e2e/scenarios/agentic/process_replacement_test.go` holds only unit tests
for the harness plumbing (`composeProcessController`, barrier/backoff helpers) that `verifyApprovalAcrossReplacement`
depends on, with no terminal-recovery assertions of their own. The probe tests are this change's own (PR #1388), the
only tests currently forcing W3/W4 on the production lane callbacks.

## Adjacent claims

- #1377 — agentic-loop: make committed terminal outcomes govern recovery (this change's issue; OPEN)
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart (parent epic; OPEN)
- #1177 — beta.163: identity, boundary, and restart — the pre-v1 breaking wave (tracking; OPEN)
- #1365 — agentic-loop: a rebuilt loop recovers its deferred turn text and task prompt (sibling Lane A step; OPEN; claimed by draft PR #1387)
- #1345 — agentic-loop: task intake still logs and ACKs five failure classes; needs resumable intake (OPEN)
- #1374 — agentic-tools/agentic-loop terminal-owner accounting (CLOSED; landed the `recordCommittedTerminal` counting-once contract this inventory's § 1/§ 7 reads)
- #1238 — e2e(agentic): the agentic tier walks neither the approval nor the signal path (CLOSED; landed the MSG_TERMINATED-based stages this inventory cites in § 7/§ 9)
- #1362 — agentic-loop: restart-safety L4b (CLOSED; source of both accepted residuals in § 3/§ 4)
- PR #1388 — this change's own draft PR (`Closes #1377`; HEAD `597072c4`, adds only the probe test)
- PR #1387 — sibling draft PR for #1365 on `claude/gh1365-durable-accepted-input` (OPEN; no file overlap found with this inventory's pins)
- `openspec list` shows exactly one open change: `agentic-loop-committed-terminal-recovery` (this one; "No tasks" — design phase; only `proposal.md` exists)

## Searches

- `git rev-parse HEAD` → `597072c4f0bbcf2f29187229145e01d819144d6a`
- `git rev-parse main` → `763b33ddd5b0448bd8fcd04e36358fd370ac027c` (local `main` ref is behind design base `9e5d8455`; `git log --oneline 9e5d8455 -1` confirms `9e5d8455` is an ancestor commit)
- `git diff 9e5d8455 597072c4 --stat` → 2 files (`proposal.md`, the probe test), 605 insertions, 0 deletions
- `wc -l processor/agentic-loop/{terminal_owner,component,handlers,approval_response_handler,approval_sweeper,loop_evidence,config,state}.go` → 512 / 3642 / 3443 / 432 / 228 / 653 / 458 / 1959 lines respectively
- `git grep -n "failedTerminal" -- 'processor/agentic-loop/*.go'` → 8 (1 doc comment + declaration, 3 production call sites, 3 test references)
- `git grep -n "5808903072\|That is the recorded" processor/agentic-loop/component.go` → 2 (`:2248`, `:2249`)
- `git grep -n "committed, err := c.loopsBucket.Update" processor/agentic-loop/component.go` → 2 (`:3071` persistLoopState, `:3150` persistDeferredContinuationMarker — the latter unrelated, not pinned)
- `git grep -n "classifyMissingLoop" -- '*.go'` → 8 (1 production call site, declaration, doc comment, 4 test references — actual file is `loop_presence.go`, not `loop_evidence.go` as the brief's file guess suggested)
- `git grep -n "AGENT_LOOPS" -- '*.go'` → 200+ hits across every consumer of the loops bucket; used only to locate the bucket declaration and acquisition site, not enumerated exhaustively here
- `git grep -n "TOOL_CALL_OUTCOMES" -- '*.go'` → 6 (constant declaration + 5 doc/comment mentions)
- `git grep -n "BucketToolCallOutcomes" -- '*.go'` → 9 (declaration, catalog entry, 1 production reader/writer site, 5 test references, 2 e2e scenario references)
- `grep -n "owned :=\|derived :=\|History" graph/kvcatalog.go` → 11 (`owned` closure default `History: 1`; `BucketToolCallOutcomes` has no override)
- `git grep -n "adoptDurableCancel(" -- 'processor/agentic-loop/*.go'` → 2 (1 production call, 1 declaration)
- `git grep -n "terminalMarkerKey(" -- 'processor/agentic-loop/*.go'` → 12 total; non-test filtered → 3 (declaration, 1 Create write, 1 Get read)
- `gopls references processor/agentic-loop/terminal_owner.go:151:26` (commitTerminal) → 3 production call sites (component.go:2124, :2251, :3381)
- `gopls references processor/agentic-loop/component.go:2218:33` (persistHandlerResult) → 16 total; 7 production, 9 test
- `gopls workspace_symbol -matcher=fuzzy commitTerminal` → 38 results (2 real symbols, rest unrelated fuzzy matches on "terminal")
- `gopls workspace_symbol -matcher=fuzzy adoptDurableCancel` → 1 result
- `gopls references processor/agentic-loop/loop_evidence.go:282:26` (readLoopRecord) → 10 (8 production, 2 test)
- `gopls references processor/agentic-loop/loop_presence.go:70:26` (classifyMissingLoop) → 4 (1 production, 3 test)
- `gopls references processor/agentic-loop/terminal_owner.go:225:22` (recordCommittedTerminal) → 2 (exactly the two production callers the doc comment claims)
- `git grep -n "drainPendingToolFailures" -- 'processor/agentic-loop/*.go'` → 8 (1 declaration, 3 production calls, 4 test references)
- `git grep -n "Loop delivery refused by latched lane\|type loopPolicyHandle" -- 'processor/agentic-loop/*.go' ':!*_test.go'` → 1 (the log line only)
- `git grep -c "loopPolicyHandle" -- '*.go' ':!*_test.go'` → 0; `git grep -c "loopPolicyHandle" -- '*.go'` → 6 files, all `*_test.go`
- `git grep -n "func.*[Ll]atch" -- 'internal/deliverylane/*.go' ':!*_test.go'` → 1 (`Admission.Latch`; no `Unlatch` under any spelling)
- `grep -n "loadCompletedOutcome(ctx, call"` in `processor/agentic-tools/component.go` → 2 (`:742` before admission, `:900` a different read path)
- `git grep -n "loops_failed_total\|active_loops\|signals_dropped_total\|tool_results_dropped_total\|recordLoopFailed\|recordSignalDropped\|activeLoops\b" processor/agentic-loop/metrics.go` → 12
- `git grep -n "LastError" -- 'processor/agentic-loop/component.go'` → 1
- `git grep -n "MSG_TERMINATED" -- '*.go'` → 8 (1 output/otel, 1 agentic-dispatch, 2 storage/objectstore, 4 test/e2e/scenarios/agentic/approval_signal.go)
- `grep -n "is not reconciled\|recorded residual\|Two residuals" openspec/specs/agentic-loop/spec.md` → 2
- `git grep -n "Two residuals\|not reconciled\|5808903072\|5809906669" docs/operations/migration-beta162-to-beta163.md` → 2
- `git grep -n "#1377" -- 'docs/operations/migration-*.md'` → 0
- `git grep -rn "transition-result" -- '*.md'` → 8 (all inside the archived transition-result change directory)
- `git grep -n "failingLoopBucket" -- 'processor/agentic-loop/*.go'` → 6 (1 type decl, 2 method decls, 3 use sites)
- `git grep -n "^func Test.*[Ss]weep" -- 'processor/agentic-loop/*_test.go'` → 10
- `grep -n "^func Test" test/e2e/scenarios/agentic/approval_restart_test.go test/e2e/scenarios/agentic/process_replacement_test.go` → 12 (harness/helper unit tests, none a terminal-recovery assertion)
- `gh issue list --search "committed terminal recovery" --state open --json number,title` → 3 (#1377, #1146, #1177)
- `gh issue list --search "1377" --state open --json number,title --limit 30` → 5 (#1377, #1365, #1345, #1177, #1146)
- `gh issue view 1374/1238/1146/1362 --json number,title,state` → 1146 OPEN, 1374/1238/1362 CLOSED
- `openspec list` → 1 (`agentic-loop-committed-terminal-recovery`, "No tasks")
- `gh pr list --search "1377" --json number,title,headRefName,state --limit 30` → 2 (#1388 this branch, #1387 sibling for #1365)
- `gh pr list --search "1381" --json number,title,state --limit 10` → 0 (merged, not returned by default search)
- `gh pr list --search "1146" --state open --json number,title,headRefName --limit 10` → 3 (#1388, #1387, #1368 — the last unrelated, a NEAR-audit docs PR incidentally matching the epic number in its body)
- `ls -la openspec/changes/agentic-loop-committed-terminal-recovery/` → 1 file (`proposal.md` only)

### NOT RUN (named for a follow-up pass, not a conclusion)

- `internal/deliverylane/*.go` was not read beyond locating `Admission.Latch`; the full admission-gate lifecycle
  (construction, per-lane wiring, and whether any RESET path exists across a reconnect rather than a process
  restart) is unread.
- `agentic/state.go`'s `LoopManager` internals beyond `CancelLoop` and `IsTerminal` were not swept —
  `ResolveApprovalIfPending`, `SnapshotExpiredApprovals`, `GetPendingTools` are cited by line from callers only, not
  independently pinned at their own declarations.
- `checkApprovalGate` (handlers.go, cited in the archived transition-result inventory at `:2824`) was not re-read at
  this base to confirm its current line; not needed for this brief's W1-W4 seams.
- Sister-repo mentions of `AGENT_LOOPS`, `TOOL_CALL_OUTCOMES`, or the terminal-owner ordering were not searched — out
  of scope (sister repos are read-only inventory, not named in this brief).
- `processor/agentic-tools/executor.go`'s `Execute` and the individual tool executors under
  `processor/agentic-tools/executors/` were not read for a per-tool terminal check; § 6's answer is scoped to the
  ONE dispatch chokepoint (`handleToolCall`), which every `tool.execute` message passes through before reaching any
  executor.
