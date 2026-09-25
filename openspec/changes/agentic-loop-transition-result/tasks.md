# Tasks — agentic-loop-transition-result (#1376)

Base: `f15a528e` (+ PR #1380 at its approved head `de1413d7`, which lands first — ruling 1). Pin keys: C
`processor/agentic-loop/component.go`, ARH `approval_response_handler.go`, AS `approval_sweeper.go`, H `handlers.go`,
TO `terminal_owner.go`. Pins are generated from the files at base (`sed -n "${n}p"`); where #1380 moves a line, the
task names it with `(de1413d7)`. Design: `design.md`; rows are § 2; owner questions OQ1–OQ4 are § 0, all recommended
(a). **No task asserts a post-merge fact.** Tasks marked **[OQ1 (b)]** run only if the owner takes that option; under
(a) they are not run and nothing replaces them.

## 0. Gates before any code (design phase closes here)

- [x] 0.1 Independent design review of `design.md` and the delta (contract § Required workflow 7); then owner
      acceptance of the table and answers to OQ1–OQ4 on #1376. Implementation waits for both and for #1380 to merge.
      **Done:** design review PASS at `95848963` and owner acceptance, option (a) on OQ1–OQ4, recorded at #1376 issuecomment-5832902240 (2026-09-25); #1380 merged as `eb955c2f`. Implementation rebased onto `97d268d8`.
- [ ] 0.2 After #1380 merges: `task inventory:verify -- openspec/changes/agentic-loop-transition-result/inventory.md`
      and refresh the pins it reports moved (`recordCommittedTerminal(entity, …)` and the held-loop read shift TO;
      `handleLoopFailure`'s signature shifts C; `errLoopTimedOut` shifts H); update `base:`.
      **Not run as written (caller instruction, 2026-09-25):** the pins are pre-change evidence and are not re-pinned. `task inventory:verify` after the implementation commit reports `pins=198 ok=107 moved=38 ambiguous=18 drift=35 malformed=0 unparsed=0` (exit 1) — #1380 and this change moved them. Current lines are in the task notes below.

## 1. The one decision (design § 4)

- [x] 1.1 Add the private predicate `failedTerminal(result HandlerResult, err error) bool` beside `terminalGuardResult`
      — `processor/agentic-loop/handlers.go:2629` — `func terminalGuardResult(loopID string, state agentic.LoopState) HandlerResult {` — returning
      `err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere`, with the doc comment from design § 4.
      **Done:** `processor/agentic-loop/handlers.go:2645`.
- [x] 1.2 Replace the inline guard on the approval lane — `processor/agentic-loop/approval_response_handler.go:210` — `if err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere {` —
      with `if failedTerminal(result, err) {`. The branch body is unchanged except 2.2.
      **Done:** `processor/agentic-loop/approval_response_handler.go:210`.
- [x] 1.3 Replace the sweeper's guard — `processor/agentic-loop/approval_sweeper.go:113` — `if err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere {` — the same way.
      **Done:** `processor/agentic-loop/approval_sweeper.go:113`.
- [x] 1.4 Replace the tool lane's guard — `processor/agentic-loop/component.go:2659` — `if result.State.IsTerminal() {` — with
      `if failedTerminal(result, cause) {`. This adds the `!terminalOwnedElsewhere` conjunct; design P3 shows it is a
      no-op (a guard result is returned with nil at `processor/agentic-loop/handlers.go:2660` — `return terminalGuardResult(loopID, entity.State), nil`,
      `processor/agentic-loop/handlers.go:2734` — `return result, nil` and unchanged through `processor/agentic-loop/approval_response_handler.go:169` — `return h.HandleToolResult(ctx, loopID, synthetic)`).
      **Done:** `processor/agentic-loop/component.go:2591`. P3 re-measured at `97d268d8`: `terminalGuardResult` callers are still `handlers.go:1433`, `:2660` (`, nil`) and `:2877` (inside `checkApprovalGate`, returned at `:2734` with nil).
- [x] 1.5 The model lane is NOT changed (row C2, design § 3.3): the sentinel returns
      `processor/agentic-loop/component.go:1873` — `return err`, `processor/agentic-loop/component.go:1881` — `return nil`,
      `processor/agentic-loop/component.go:1889` — `return nil`, `processor/agentic-loop/component.go:1895` — `return err`,
      `processor/agentic-loop/component.go:1900` — `return errs.WrapFatal(err, "agentic-loop", "handleResponseMessage",` and the fallthrough `processor/agentic-loop/component.go:1907` — `return c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)` stay
      (#1380 form: `c.handleLoopFailure(ctx, loopID, failureReasonForHandlerError(err), err)`).
      **Done (unchanged):** the model lane's switch and `handleLoopFailure` fallthrough are not in the diff.
- [x] 1.6 The approval lane's producers are NOT changed (rows A8/D3, OQ2 (a)):
      `processor/agentic-loop/approval_response_handler.go:85` — `return HandlerResult{}, getErr` and
      `processor/agentic-loop/approval_response_handler.go:139` — `return errs.Wrap(err, "agentic-loop", "dispatchApprovedCall", "dispatch approved tool call")` keep their classes.
      **Done (unchanged):** `approval_response_handler.go:85` and `:139` are not in the diff.

## 2. Remove the caller-selected order (design § 4, P1)

- [x] 2.1 Delete the parameter from `processor/agentic-loop/component.go:2245` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, order carrierOrder) error {`; delete the type and its constants —
      `processor/agentic-loop/component.go:2193` — `type carrierOrder int`, `processor/agentic-loop/component.go:2199` — `writeThenPublish carrierOrder = iota`,
      `processor/agentic-loop/component.go:2205` — `publishThenWrite`. Replace `processor/agentic-loop/component.go:2285` — `if order == publishThenWrite && !gated {` with
      `if !gated {`. Rewrite the order contract in the doc comment (C:2209-2244) to say the shape decides; keep the
      gate paragraph (OQ-A of #1362) verbatim in substance.
      **Done:** `persistHandlerResult(ctx, result)` at `processor/agentic-loop/component.go:2193`; the branch is `if !gated {`; the doc comment states that the shape decides the order and keeps the gate paragraph.
- [x] 2.2 Drop the argument at the seven production call sites: `processor/agentic-loop/approval_response_handler.go:221` — `err = c.persistHandlerResult(ctx, result, writeThenPublish)`,
      `processor/agentic-loop/approval_response_handler.go:272` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`, `processor/agentic-loop/approval_sweeper.go:123` — `if commitErr := c.persistHandlerResult(ctx, result, writeThenPublish); commitErr != nil {`,
      `processor/agentic-loop/approval_sweeper.go:158` — `if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {`, `processor/agentic-loop/component.go:1920` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`,
      `processor/agentic-loop/component.go:2610` — `return c.persistHandlerResult(ctx, result, publishThenWrite)`, `processor/agentic-loop/component.go:2664` — `return c.persistHandlerResult(ctx, result, writeThenPublish)`.
      **Done:** seven production callers — `approval_response_handler.go:221`, `:272`; `approval_sweeper.go:123`, `:158`; `component.go:1918`, `:2542`, `:2595`.
- [x] 2.3 Drop the argument at the nine test call sites: `processor/agentic-loop/loop_carrier_test.go:76` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), publishThenWrite)`,
      `processor/agentic-loop/loop_carrier_test.go:87` — `err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), writeThenPublish)`, `processor/agentic-loop/persist_handler_result_test.go:69` — `}, writeThenPublish)`,
      `processor/agentic-loop/persist_handler_result_test.go:224` — `}, publishThenWrite)`, `processor/agentic-loop/publish_phase_fatal_test.go:48` — `persistErr := c.persistHandlerResult(t.Context(), result, writeThenPublish)`,
      `processor/agentic-loop/terminal_owner_test.go:399` — `persistErr := c.persistHandlerResult(t.Context(), result, publishThenWrite)`, `processor/agentic-loop/trajectory_eviction_internal_test.go:31` — `}, writeThenPublish)`,
      `processor/agentic-loop/task_redelivery_integration_test.go:187` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`,
      `processor/agentic-loop/tool_result_redelivery_integration_test.go:216` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch, publishThenWrite))`.
      **Done, and the count was 26, not 9:** the list above named 9 of the 32 test hits the inventory counted (inventory § carrierOrder, "32 in tests across 14 test files"). The argument was dropped at all 26 `persistHandlerResult` test call sites in 14 files (7 of those files are `//go:build integration`, compiled by `go vet -tags=integration ./...`, exit 0). Non-terminal callers that had passed `writeThenPublish` and now take publish-then-write: `publish_phase_fatal_test.go:48`/`:57` (unit, still green) and `partial_publish_settlement_integration_test.go:53`/`:69` (integration, NOT RUN: integration runs wait for the coordinator's go-ahead).
- [x] 2.4 Rewrite `processor/agentic-loop/loop_carrier_test.go:71` — `func TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind(t *testing.T) {`: its second subtest (:84-92) drives write-then-publish
      on a NON-terminal result through the deleted parameter; make it a gate-shaped result
      (`State: agentic.LoopStateAwaitingApproval`) or fold it into
      `processor/agentic-loop/persist_handler_result_test.go:208` — `func TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks(t *testing.T) {`, and rename both so neither claims an order a lane
      "asks" for. `git grep -n "carrierOrder\|writeThenPublish\|publishThenWrite" -- '*.go'` must return 0 lines.
      **Done:** renamed `TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind` (`loop_carrier_test.go`); its write-first arm now drives an `awaiting_approval` result on a loop gated in memory. `TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks` is renamed `TestAGateIsWrittenBeforeItIsPublished`, body unchanged. `git grep -n "carrierOrder\|writeThenPublish\|publishThenWrite" -- '*.go'` → 0 lines (exit 1).
- [x] 2.5 Verify the orders are preserved by the 14 tests design § 5 lists as unchanged — run them by name with
      `-run` and `-count=1` and paste the output; in particular the birth pair
      (`processor/agentic-loop/loop_carrier_test.go:347` — `func TestBirthRefusesASecondCreateForTheSameLoop(t *testing.T) {`, `processor/agentic-loop/loop_carrier_test.go:378` — `func TestBirthWhosePublishFailsIsNotAcknowledged(t *testing.T) {`), the gate pair
      (`processor/agentic-loop/loop_carrier_test.go:152` — `func TestAnApprovalGateIsWrittenBeforeItsEventIsPublished(t *testing.T) {`, `processor/agentic-loop/persist_handler_result_test.go:252` — `func TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged(t *testing.T) {`)
      and the terminal-owner set (`processor/agentic-loop/terminal_owner_test.go:72` — `func TestTerminalOwnerArms(t *testing.T) {` … `processor/agentic-loop/terminal_owner_test.go:410` — `func TestAnApprovalWhoseRecordMovedIsRetried(t *testing.T) {`).
      **Done:** `go test -race -count=1 -v -run '^(…the 13 unit tests of the 14…|TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind|TestApprovalLanePublishesBeforeItWrites)$' ./processor/agentic-loop/` → 15 `--- PASS`, `ok`. The fourteenth, `TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains`, is integration-tagged and NOT RUN here.

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
      **Not run: OQ1 ruled (a)** (#1376 issuecomment-5832902240).

## 4. Tests (design § 5) and spec

- [x] 4.1 Table-driven test of `failedTerminal` over state × terminal payload × owned-elsewhere × error (nil / plain /
      fatal); every row named, none sampled. `// spec: agentic-loop / Loop input classes settle after owner-specific
      durable done`.
      **Done, as produced rows:** `processor/agentic-loop/transition_result_test.go` — `TestFailedTerminalReadsEachProducedPairOnce`, 27 named rows with literal expectations (every produced design row A1–A8, B1–B8, C1, C2, D1–D3, plus E1, which has no production trigger, and three rows marked "not produced" that pin a clause), and `TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition`, 6 rows driven through the production heartbeat policy. Not the full state × payload × owned-elsewhere × error product the line above names: a product row's expectation would be the predicate's own formula.
- [ ] 4.2 **[OQ1 (b) only]** A held loop whose event build fails is quarantined with no record written, through
      `commitTerminal` from the carrier (`handleToolResultMessage`) and from `handleLoopFailure`; the counterexample on
      the `handleLoopFailure` path is today's record-then-Fatal (probe-confirmed) — prove it against the pre-3.1 tree
      (`cp` backup + checksum).
      **Not run: OQ1 ruled (a).**
- [x] 4.3 Mutation evidence for the WIRING: delete the `failedTerminal` call at ONE site at a time and run that lane's
      failed-terminal test — approval `processor/agentic-loop/approval_loop_deadline_test.go:82` — `func TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {`, sweeper
      `processor/agentic-loop/approval_loop_deadline_test.go:143` — `func TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout(t *testing.T) {`, tool: #1380's
      `TestAToolLaneTimeoutCountsOneTimeoutFailure` (`terminal_metrics_test.go`, lands with #1380). Each must go red on
      its own site only. Record the three runs in the PR body.
      **Done** (`cp` backup, `md5 -q` before and after equal, per site; each mutant `if failedTerminal(…) {` → `if false {`, run against all four failed-terminal tests): approval `:210` → only `TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout` red (approve/modify/reject: "Received unexpected error … check timeout failed: loop timeout exceeded"); sweeper `:113` → only `TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout` red (`expected: "failed"`, `actual: "awaiting_approval"`); tool `component.go:2591` → only `TestAToolLaneTimeoutCountsOneTimeoutFailure` and the C1 row of `TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition` red (`expected: 0x1` Ack, `actual: 0x4`).
- [x] 4.4 `openspec validate agentic-loop-transition-result --strict` green; `task spec:properties` count moves by the
      new `// spec:` citations (git add the new test file first).
      **Done:** `openspec validate agentic-loop-transition-result --strict` → valid; `task spec:properties` → `396/396 citations resolve` (+2 `// spec:` lines, both in the new test file).
- [x] 4.5 Migration note: none under (a) on every owner question (no wire, KV or metric key changes; no disposition
      changes). Under OQ1 (b): one section in `docs/operations/migration-beta162-to-beta163.md` naming the refused
      shape and that no production path produces it.
      **Done: none** — (a) on every owner question; no wire, KV, metric or disposition change.

## 5. Gates

- [ ] 5.1 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines, not "green".
- [ ] 5.2 `task api:compat` reports no Tier 1 break in `processor/agentic-loop` (no exported symbol changes; OQ4 (a)).
      **Run, exit 1, not from this diff:** `task api:compat` compares HEAD with tag `v1.0.0-beta.162` and reports 15 Tier 1 breaks already on main; none in `processor/agentic-loop` names `failedTerminal`, `carrierOrder` or `persistHandlerResult`, and the diff adds or removes no exported declaration.
- [ ] 5.3 `task e2e:agentic` not required under (a) (proposal § Impact: only a changed observable disposition gates on
      the tier) — say so in the PR body; required only if a (b) landed.
- [ ] 5.4 PR body: `implemented-by: <persona>`, the three mutation runs from 4.3, the OQ answers quoted from #1376.
- [ ] 5.5 Archive: `openspec archive agentic-loop-transition-result --yes` as the last content commit; the MODIFIED
      block syncs into `openspec/specs/agentic-loop/spec.md`.
