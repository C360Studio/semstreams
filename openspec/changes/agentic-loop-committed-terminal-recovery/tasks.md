# Tasks — agentic-loop-committed-terminal-recovery (#1377)

Base: main `9e5d8455`; artifacts at `6876fe51` (inventory pinned at `597072c4`, `pins=121 ok=121`). Pin keys: C
`processor/agentic-loop/component.go`, ARH `approval_response_handler.go`, AS `approval_sweeper.go`, H `handlers.go`,
TO `terminal_owner.go`, ST `state.go`, PROBE `held_loop_cancel_approval_race_probe_integration_test.go`, PPF
`publish_phase_fatal_test.go`, ALD `approval_loop_deadline_test.go`. Pins are generated from the files at base
(`sed -n "${n}p"`). Design: `design.md`; the docket is § 1; owner questions OQ1–OQ5 are § 0 (recommended: OQ1 (a),
OQ2 (a), OQ3 entry-only, OQ4 the check, OQ5 the hook). **No task asserts a post-merge fact.** Tasks marked
**[OQ2 (b)]** run only if the owner takes that option; under (a) they are not run and nothing replaces them. Tasks
marked **[OQ3 alt]** run only if the owner asks for the post-publish check.

## 0. Gates before any code (design phase closes here)

- [ ] 0.1 Independent design review of `design.md` and the delta (contract § Required workflow 7); then owner
      acceptance of the docket and answers to OQ1–OQ5 on #1377. Implementation waits for both, and for PR #1387 to
      merge (ruling 5). The (a) rows on W1 and W2 are posted on #1146 as the § 8 table with the amendment flags before
      the epic closes.
- [ ] 0.2 After PR #1387 merges: rebase; re-read `openspec/specs/agentic-loop/spec.md` § "The loop record names its
      outstanding request" and re-base the delta's restated requirement text and scenario list on the synced spec
      (#1387 carries a MODIFIED block on the same requirement — design P10). Pins are pre-change evidence and are NOT
      re-pinned; `task inventory:verify` red on moved lines after the rebase is the correct reading (record the line).

## 1. The carrier reads the loop it publishes for (design § 3.1; W3 and W4)

- [ ] 1.1 In `persistHandlerResult` — `processor/agentic-loop/component.go:2218` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {` —
      after the backstop `processor/agentic-loop/component.go:2224` — `if result.terminalOwnedElsewhere {` and before
      `processor/agentic-loop/component.go:2232` — `c.recordHandlerResultTrajectory(ctx, result)`, add the nine-line
      block from design § 3.1: for `!terminal`, `c.handler.GetLoop(result.LoopID)` failing or returning a terminal
      state routes to `c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)` —
      `processor/agentic-loop/terminal_owner.go:481` — `// settleTerminalGuard decides a result the handler returned from a terminal`,
      `processor/agentic-loop/terminal_owner.go:508` — `func (c *Component) recordTerminalToolResultDropped() {`. The
      `terminal` predicate `processor/agentic-loop/component.go:2219` — `terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed`
      is NOT widened (design P1). Doc comment names #1377 W3/W4 and the publish sub-window (OQ3).
- [ ] 1.2 **[OQ3 alt]** Repeat the two-line condition in `publishThenPersistResultState` —
      `processor/agentic-loop/component.go:2306` — `func (c *Component) publishThenPersistResultState(ctx context.Context, result HandlerResult) error {` —
      after its `publishResults` and before its `persistLoopState`, returning the same guard settlement without writing.
- [ ] 1.3 Doc comment on `recordTerminalToolResultDropped` (TO:506-511) and on the `tool_results_dropped_total` help
      — `processor/agentic-loop/metrics.go:166` — `Name:      "tool_results_dropped_total",` — saying an approval
      answer settled at the carrier counts under `terminal_unproven` (design § 7).

## 2. The test-only hook (design § 3.2; OQ5)

- [ ] 2.1 Add `testApprovedDispatchHook func(loopID string)` to `MessageHandler` (unexported; doc comment cites the
      precedent `processor/agentic-loop/component.go:157` — `testPublishHook func(subject string, data []byte)`).
- [ ] 2.2 Call it in `dispatchApprovedCall` — `processor/agentic-loop/approval_response_handler.go:128` — `func (h *MessageHandler) dispatchApprovedCall(loopID string, pending agentic.PendingApprovalState, args map[string]any, approvedBy string, result *HandlerResult) error {` —
      after `processor/agentic-loop/approval_response_handler.go:138` — `if err := h.dispatchToolCall(result, loopID, tc); err != nil {` returns nil,
      before `return nil`. `git grep -n "testApprovedDispatchHook" -- '*.go' ':!*_test.go'` → exactly 2 lines (field,
      call).

## 3. OQ2 (b) only — the cold branch adopts a failed marker (design § 3.3)

- [ ] 3.1 **[OQ2 (b)]** In `settleApprovalResponseWithoutLoop` — `processor/agentic-loop/approval_response_handler.go:360` — `func (c *Component) settleApprovalResponseWithoutLoop(` —
      between the gate identity check ending at `processor/agentic-loop/approval_response_handler.go:380` — `}` and the I4 check
      `processor/agentic-loop/approval_response_handler.go:382` — `if gate.RequestID != record.entity.PublishedRequestID {`,
      read `COMPLETE_<loopID>` as `processor/agentic-loop/terminal_owner.go:389` — `entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))` does
      (not-found → continue; read error → transient; decode error or foreign loop → fatal, as TO:398-408). A
      `saved.failed != nil` marker: seat and fail as `processor/agentic-loop/approval_response_handler.go:421` — `func (c *Component) failContinuationUnavailable(ctx context.Context, record loopRecord, cause error) error {`
      does (ARH:423-430), then `handleLoopFailure(ctx, loopID, saved.failed.Reason, errors.New(saved.failed.Error))`;
      on nil, `recordApprovalInapplicable(response)` and `return false, nil`. Other kinds fall through.
- [ ] 3.2 **[OQ2 (b)]** Replace the delta's W2 sentence and the third added scenario's THEN clauses with the (b) text
      named in design § 2, and the migration bullet with its bracketed (b) variant (§ 5).

## 4. Tests (design § 4) and spec

- [ ] 4.1 T3/T4: move the probe's harness (`raceProbeBucket` `processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:84` — `type raceProbeBucket struct {`,
      `startProbeLane`, `gatedRunLoop`, metric snapshots, `approvedToolCallsOn`) into a landed integration test file;
      replace the Warn pause (`processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:48` — `type pauseOnLog struct {`)
      with the hook; flip `TestProbeW3HeldLoopCancelDuringApprovalDispatch` (PROBE:373) and
      `TestProbeW4ReleasedLoopQuarantinesApprovalLane` (PROBE:467) into the assertions of design § 4 T3/T4, including
      T3's redelivery (`approval_inapplicable` +1) and T4's second-loop control. Delete PROBE.
      `// spec: agentic-loop / The loop record names its outstanding request`.
- [ ] 4.2 T1: the two-component W1 counterexample on the `predecessor`/replacement pattern —
      `processor/agentic-loop/task_redelivery_integration_test.go:187` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch))` —
      with both arms (same kind converges; different kind refused, marker unchanged). Asserts the documented bound;
      no mutation (an (a) row).
- [ ] 4.3 T2: extend `processor/agentic-loop/approval_loop_deadline_test.go:219` — `func TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {`
      with the iteration-cap arm (record at the cap, `TimeoutAt` zero, sweep with `unpublishableClient`, then an
      approve): under (a) assert one `tool.execute` dispatched and adoption when its result completes the batch;
      **[OQ2 (b)]** assert nothing dispatched, `approval_inapplicable` +1, record `failed` with the saved reason, and
      mutation evidence: delete the marker read of 3.1 → red on "no tool.execute".
- [ ] 4.4 Fix the one test the check changes: `processor/agentic-loop/publish_phase_fatal_test.go:31` — `t.Parallel()` — `TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified`
      creates `loop-publish-phase` in the handler (`CreateLoopWithID`, as `trajectory_eviction_internal_test.go:39`)
      so its non-terminal result passes the check and the classification stays the publish's. Run every
      `persistHandlerResult` test call site (`git grep -n "persistHandlerResult(" -- 'processor/agentic-loop/*_test.go'`
      → 28 at base) and name any other that drives a non-terminal result on an unheld loop.
- [ ] 4.5 Mutation evidence for the WIRING (`cp` backup, `md5 -q` before and after equal): delete the § 3.1 block →
      T3 red on "approved tool.execute unchanged" and "no carrier write", T4 red on acks and health. Record both runs in
      the PR body. **[OQ2 (b)]**: the 4.3 mutation.
- [ ] 4.6 Controls green by name with `-count=1`: the 14 order tests the archived transition-result design § 5 lists,
      `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt` (`processor/agentic-loop/approval_restore_order_test.go:316`),
      `TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind`, `TestApprovalLanePublishesBeforeItWrites`; paste the
      output.
- [ ] 4.7 `openspec validate agentic-loop-committed-terminal-recovery --strict` green; `task spec:properties` count
      moves by the new `// spec:` citations (`git add` the new test file first).
- [ ] 4.8 Migration: replace `docs/operations/migration-beta162-to-beta163.md:2015` — `**Two residuals, recorded and not reconciled** (#1362 issuecomment-5808903072 and issuecomment-5809906669):`
      through `:2025` with design § 5 (the bracketed (b) variant only under OQ2 (b)); replace the spec's residual
      sentences through the delta's sync (4.9).

## 5. Gates

- [ ] 5.1 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines, not "green". The landed W3/W4 tests run under `-race -count=20` once, output pasted.
- [ ] 5.2 `task e2e:agentic` green on the final diff (proposal § Impact: the BREAKING gate walks the approval path;
      `verifyApprovalAcrossReplacement` is the replacement control, design § 4 T5). Paste the tier's summary line.
- [ ] 5.3 `task api:compat`: no exported symbol is added or removed by this diff (the hook is unexported); report the
      pre-existing Tier 1 breaks as the archived task 5.2 did.
- [ ] 5.4 PR body: `implemented-by: <persona>`, the OQ answers quoted from #1377, the 4.5 mutation runs, the 5.1/5.2
      lines; the § 8 table posted on #1146 with the amendment flags before the epic closes.
- [ ] 5.5 Archive: `openspec archive agentic-loop-committed-terminal-recovery --yes` as the last content commit; the
      MODIFIED block syncs into `openspec/specs/agentic-loop/spec.md`.
