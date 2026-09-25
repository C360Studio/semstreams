# Tasks — agentic-loop-transition-result (#1376)

Base: `f15a528e` (+ PR #1380 at its approved head `de1413d7`, which lands first — ruling 1). Pin keys: C
`processor/agentic-loop/component.go`, ARH `approval_response_handler.go`, AS `approval_sweeper.go`, H `handlers.go`,
TO `terminal_owner.go`. Pins are generated from the files at base (`sed -n "${n}p"`); where #1380 moves a line, the
task names it with `(de1413d7)`. Design: `design.md`; rows are § 2; owner questions OQ1–OQ4 are § 0, all recommended
(a). **No task asserts a post-merge fact.** Tasks marked **[OQ1 (b)]** run only if the owner takes that option; under
(a) they are not run and nothing replaces them.

## 0. Gates before any code (design phase closes here)

- [ ] 0.1 Independent design review of `design.md` and the delta (contract § Required workflow 7); then owner
      acceptance of the table and answers to OQ1–OQ4 on #1376. Implementation waits for both and for #1380 to merge.
- [ ] 0.2 After #1380 merges: `task inventory:verify -- openspec/changes/agentic-loop-transition-result/inventory.md`
      and refresh the pins it reports moved (`recordCommittedTerminal(entity, …)` and the held-loop read shift TO;
      `handleLoopFailure`'s signature shifts C; `errLoopTimedOut` shifts H); update `base:`.

## 1. The one decision (design § 4)

- [ ] 1.1 Add the private predicate `failedTerminal(result HandlerResult, err error) bool` beside `terminalGuardResult`
      — `processor/agentic-loop/handlers.go:2629` — `func terminalGuardResult(loopID string, state agentic.LoopState) HandlerResult {` — returning
      `err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere`, with the doc comment from design § 4.
- [ ] 1.2 Replace the inline guard on the approval lane — `processor/agentic-loop/approval_response_handler.go:210` — `if err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere {` —
      with `if failedTerminal(result, err) {`. The branch body is unchanged except 2.2.
- [ ] 1.3 Replace the sweeper's guard — `processor/agentic-loop/approval_sweeper.go:113` — `if err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere {` — the same way.
- [ ] 1.4 Replace the tool lane's guard — `processor/agentic-loop/component.go:2659` — `if result.State.IsTerminal() {` — with
      `if failedTerminal(result, cause) {`. This adds the `!terminalOwnedElsewhere` conjunct; design P3 shows it is a
      no-op (a guard result is returned with nil at `processor/agentic-loop/handlers.go:2660` — `return terminalGuardResult(loopID, entity.State), nil`,
      `processor/agentic-loop/handlers.go:2734` — `return result, nil` and unchanged through `processor/agentic-loop/approval_response_handler.go:169` — `return h.HandleToolResult(ctx, loopID, synthetic)`).
- [ ] 1.5 The model lane is NOT changed (row C2, design § 3.3): the sentinel returns
      `processor/agentic-loop/component.go:1873` — `return err`, `processor/agentic-loop/component.go:1881` — `return nil`,
      `processor/agentic-loop/component.go:1889` — `return nil`, `processor/agentic-loop/component.go:1895` — `return err`,
      `processor/agentic-loop/component.go:1900` — `return errs.WrapFatal(err, "agentic-loop", "handleResponseMessage",` and the fallthrough `processor/agentic-loop/component.go:1907` — `return c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)` stay
      (#1380 form: `c.handleLoopFailure(ctx, loopID, failureReasonForHandlerError(err), err)`).
- [ ] 1.6 The approval lane's producers are NOT changed (rows A8/D3, OQ2 (a)):
      `processor/agentic-loop/approval_response_handler.go:85` — `return HandlerResult{}, getErr` and
      `processor/agentic-loop/approval_response_handler.go:139` — `return errs.Wrap(err, "agentic-loop", "dispatchApprovedCall", "dispatch approved tool call")` keep their classes.

## 2. Remove the caller-selected order (design § 4, P1)

- [ ] 2.1 Delete the parameter from `processor/agentic-loop/component.go:2245` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, order carrierOrder) error {`; delete the type and its constants —
      `processor/agentic-loop/component.go:2193` — `type carrierOrder int`, `processor/agentic-loop/component.go:2199` — `writeThenPublish carrierOrder = iota`,
      `processor/agentic-loop/component.go:2205` — `publishThenWrite`. Replace `processor/agentic-loop/component.go:2285` — `if order == publishThenWrite && !gated {` with
      `if !gated {`. Rewrite the order contract in the doc comment (C:2209-2244) to say the shape decides; keep the
      gate paragraph (OQ-A of #1362) verbatim in substance.
- [ ] 2.2 Drop the argument at the seven production call sites: `processor/agentic-loop/approval_response_handler.go:221` — `err = c.persistHandlerResult(ctx, result, writeThenPublish)`,
      `processor/agentic-loop/approval_response_handler.go:272` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`, `processor/agentic-loop/approval_sweeper.go:123` — `if commitErr := c.persistHandlerResult(ctx, result, writeThenPublish); commitErr != nil {`,
      `processor/agentic-loop/approval_sweeper.go:158` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`, `processor/agentic-loop/component.go:1920` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`,
      `processor/agentic-loop/component.go:2610` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`, `processor/agentic-loop/component.go:2664` — `return c.persistHandlerResult(ctx, result, writeThenPublish)`.
- [ ] 2.3 Drop the argument at the nine test call sites: `processor/agentic-loop/loop_carrier_test.go:76` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), publishThenWrite)`,
      `processor/agentic-loop/loop_carrier_test.go:87` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), writeThenPublish)`, `processor/agentic-loop/persist_handler_result_test.go:69` — `}, writeThenPublish)`,
      `processor/agentic-loop/persist_handler_result_test.go:224` — `}, publishThenWrite)`, `processor/agentic-loop/publish_phase_fatal_test.go:48` — `persistErr := c.persistHandlerResult(t.Context(), result, writeThenPublish)`,
      `processor/agentic-loop/terminal_owner_test.go:399` — `persistErr := c.persistHandlerResult(t.Context(), result, publishThenWrite)`, `processor/agentic-loop/trajectory_eviction_internal_test.go:31` — `}, writeThenPublish)`,
      `processor/agentic-loop/task_redelivery_integration_test.go:187` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`,
      `processor/agentic-loop/tool_result_redelivery_integration_test.go:216` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`.
- [ ] 2.4 Rewrite `processor/agentic-loop/loop_carrier_test.go:71` — `func TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind(t *testing.T) {`: its second subtest (:84-92) drives write-then-publish
      on a NON-terminal result through the deleted parameter; make it a gate-shaped result
      (`State: agentic.LoopStateAwaitingApproval`) or fold it into
      `processor/agentic-loop/persist_handler_result_test.go:208` — `func TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks(t *testing.T) {`, and rename both so neither claims an order a lane
      "asks" for. `git grep -n "carrierOrder\|writeThenPublish\|publishThenWrite" -- '*.go'` must return 0 lines.
- [ ] 2.5 Verify the orders are preserved by the 14 tests design § 5 lists as unchanged — run them by name with
      `-run` and `-count=1` and paste the output; in particular the birth pair
      (`processor/agentic-loop/loop_carrier_test.go:347` — `func TestBirthRefusesASecondCreateForTheSameLoop(t *testing.T) {`, `processor/agentic-loop/loop_carrier_test.go:378` — `func TestBirthWhosePublishFailsIsNotAcknowledged(t *testing.T) {`), the gate pair
      (`processor/agentic-loop/loop_carrier_test.go:152` — `func TestAnApprovalGateIsWrittenBeforeItsEventIsPublished(t *testing.T) {`, `processor/agentic-loop/persist_handler_result_test.go:252` — `func TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged(t *testing.T) {`)
      and the terminal-owner set (`processor/agentic-loop/terminal_owner_test.go:72` — `func TestTerminalOwnerArms(t *testing.T) {` … `processor/agentic-loop/terminal_owner_test.go:410` — `func TestAnApprovalWhoseRecordMovedIsRetried(t *testing.T) {`).

## 3. Boundary validation — none by default (design § 4; E1 has no production trigger, OQ1)

- [ ] 3.1 **[OQ1 (b) only]** In `commitTerminal` (TO:151 at `de1413d7`), the one owner — NOT the carrier's terminal
      branch at `processor/agentic-loop/component.go:2278` — `if err := c.commitTerminal(ctx, terminalOutcomeOf(result), result); err != nil {`, which `handleLoopFailure` bypasses at
      `processor/agentic-loop/component.go:2102` — `established := c.commitTerminal(errorCtx, terminalOutcome{failed: failure},` — refuse `candidate.kind() == ""` before
      `createTerminalMarker`: release the loop and return `errs.WrapFatal` carrying the build error, on the cancel
      lane's precedent `processor/agentic-loop/component.go:3408` — `c.releaseLoopTransientState(loopID)` / `processor/agentic-loop/component.go:3414` — `c.releaseLoopTransientState(loopID)`.
      Make the three `fErr == nil` guards return `fErr` instead of dropping it —
      `processor/agentic-loop/handlers.go:2402` — `if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, reason, errorMsg); fErr == nil {`, `processor/agentic-loop/handlers.go:3029` — `if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, "max_iterations", errorMsg); fErr == nil {`,
      `processor/agentic-loop/handlers.go:3375` — `if failure, failMsgs, fErr := h.BuildFailureMessages(loopID, "timeout", "loop timeout exceeded"); fErr == nil {` — and carry it to the owner. Delete the two counting arms that become
      dead: `case entity.State == agentic.LoopStateComplete:` (TO:238 at `de1413d7`) and `default: reason = "unknown"`
      (TO:240-241 at `de1413d7`). Under (a): not run; the silent `fErr` skip is recorded as a residual in design § 0.

## 4. Tests (design § 5) and spec

- [ ] 4.1 Table-driven test of `failedTerminal` over state × terminal payload × owned-elsewhere × error (nil / plain /
      fatal); every row named, none sampled. `// spec: agentic-loop / Loop input classes settle after owner-specific
      durable done`.
- [ ] 4.2 **[OQ1 (b) only]** A held loop whose event build fails is quarantined with no record written, through
      `commitTerminal` from the carrier (`handleToolResultMessage`) and from `handleLoopFailure`; the counterexample on
      the `handleLoopFailure` path is today's record-then-Fatal (probe-confirmed) — prove it against the pre-3.1 tree
      (`cp` backup + checksum).
- [ ] 4.3 Mutation evidence for the WIRING: delete the `failedTerminal` call at ONE site at a time and run that lane's
      failed-terminal test — approval `processor/agentic-loop/approval_loop_deadline_test.go:82` — `func TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {`, sweeper
      `processor/agentic-loop/approval_loop_deadline_test.go:143` — `func TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout(t *testing.T) {`, tool: #1380's
      `TestAToolLaneTimeoutCountsOneTimeoutFailure` (`terminal_metrics_test.go`, lands with #1380). Each must go red on
      its own site only. Record the three runs in the PR body.
- [ ] 4.4 `openspec validate agentic-loop-transition-result --strict` green; `task spec:properties` count moves by the
      new `// spec:` citations (git add the new test file first).
- [ ] 4.5 Migration note: none under (a) on every owner question (no wire, KV or metric key changes; no disposition
      changes). Under OQ1 (b): one section in `docs/operations/migration-beta162-to-beta163.md` naming the refused
      shape and that no production path produces it.

## 5. Gates

- [ ] 5.1 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines, not "green".
- [ ] 5.2 `task api:compat` reports no Tier 1 break in `processor/agentic-loop` (no exported symbol changes; OQ4 (a)).
- [ ] 5.3 `task e2e:agentic` not required under (a) (proposal § Impact: only a changed observable disposition gates on
      the tier) — say so in the PR body; required only if a (b) landed.
- [ ] 5.4 PR body: `implemented-by: <persona>`, the three mutation runs from 4.3, the OQ answers quoted from #1376.
- [ ] 5.5 Archive: `openspec archive agentic-loop-transition-result --yes` as the last content commit; the MODIFIED
      block syncs into `openspec/specs/agentic-loop/spec.md`.
