# Tasks — agentic-loop-committed-terminal-recovery (#1377)

Base: main `9e5d8455`; artifacts at `6876fe51`/`40bb373f` (inventory pinned at `597072c4`, `pins=121 ok=121`). Pin
keys: C `processor/agentic-loop/component.go`, ARH `approval_response_handler.go`, AS `approval_sweeper.go`, H
`handlers.go`, TO `terminal_owner.go`, ST `state.go`, LE `loop_evidence.go`, PROBE
`held_loop_cancel_approval_race_probe_integration_test.go`, PPF `publish_phase_fatal_test.go`, ALD
`approval_loop_deadline_test.go`. Pins are generated from the files at base (`sed -n "${n}p"`). Design: `design.md`;
the docket is § 1; owner questions OQ1–OQ7 are § 0 (recommended: OQ1 (a), OQ2 (a), OQ3 (ii), OQ4 the check, OQ5 the
staged hooks, OQ6 two blocks, OQ7 (a)). **No task asserts a post-merge fact.** Tasks marked **[OQ2 (b)]** run only if
the owner takes that option; tasks marked **[OQ3 (ii)]** are the recommendation and are skipped only if the owner
takes (i); **[OQ7 (b)]** likewise. Under an option not taken nothing replaces its tasks.

## 0. Gates before any code (design phase closes here)

- [x] 0.1 Independent design review of `design.md` and the delta (contract § Required workflow 7) — round 1 PASS WITH
      AMENDMENTS at `40bb373f`, amendments applied in this revision; round 2 on this revision; then owner acceptance
      of the docket and answers to OQ1–OQ7 on #1377. Implementation waits for both, and for PR #1387 to merge (ruling
      5). The (a) rows on W1 and W2 and the W3 publication sub-window are posted on #1146 as the § 8 table with the
      amendment flags before the epic closes; OQ6's ruling-2 cost and OQ7's ruling-1 composition are called out on
      #1377 in the same posting. DONE 2026-09-26: round 2 PASS WITH AMENDMENTS at `8817d6d4`, the stamp-ordering HIGH
      corrected at `2a5762bb`; owner acceptance on #1377 (OQ1–OQ7 as recommended, two epic amendments accepted); PR
      #1387 merged as `8d53c084`. The § 8 posting on #1146 is carried by 6.4.
- [x] 0.2 After PR #1387 merges: rebase; re-read `openspec/specs/agentic-loop/spec.md` § "Loop input classes settle
      after owner-specific durable done" — #1387 at `d2b6a20e` MODIFIES that requirement and REMOVES "Task intake is
      the one loop input class this layer does not convert" (design P10) — and re-base the delta's block 1 (its
      restated text and 22 scenarios) on the synced spec; block 2 is re-checked the same way. Pins are pre-change
      evidence and are NOT re-pinned; `task inventory:verify` red on moved lines after the rebase is the correct
      reading (record the line). DONE 2026-09-26 on main `8d53c084`: both blocks re-based by a three-way merge
      (`git merge-file` of the synced block, the `9e5d8455` block and the delta block), no conflict; block 1 now
      restates 30 scenarios (main's eight new ones included) and differs from main by exactly the five edits design
      § 2 names; block 2 restates main's 22 (the #1387 durable-text wording included) and adds six. `openspec validate
      --strict` valid. Recorded: `pins=121 ok=80 moved=38 ambiguous=3 drift=0 malformed=0 unparsed=0` (exit 1, moved
      lines only; the spec pins S:1618/1621 moved to 1688/1691, the migration pins 2015/2023 to 2023/2031).

## 1. The carrier reads the loop it publishes for (design § 3.1; W3 and W4; OQ3 (i) and (ii))

- [x] 1.1 In `persistHandlerResult` — `processor/agentic-loop/component.go:2218` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {` —
      after the backstop `processor/agentic-loop/component.go:2224` — `if result.terminalOwnedElsewhere {` and before
      `processor/agentic-loop/component.go:2232` — `c.recordHandlerResultTrajectory(ctx, result)`, add the block from
      design § 3.1: for `!terminal`, `c.handler.GetLoop(result.LoopID)`; `errors.Is(err, ErrLoopNotFound)` or a
      terminal state routes to `c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)` —
      `processor/agentic-loop/terminal_owner.go:481` — `// settleTerminalGuard decides a result the handler returned from a terminal`,
      `processor/agentic-loop/terminal_owner.go:508` — `func (c *Component) recordTerminalToolResultDropped() {`; any
      other error is returned as it is (an invalid loop ID is never acknowledged as stale). The `terminal` predicate
      `processor/agentic-loop/component.go:2219` — `terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed`
      is NOT widened (design P1). Doc comment names #1377 W3/W4 and the publication in flight. DONE: `persistHandlerResult` entry check (component.go, "if !terminal {" after the backstop); verified by T3/T4 and mutation M1.
- [x] 1.2 Reword `settleTerminalGuard`'s Warn and transient cause —
      `processor/agentic-loop/terminal_owner.go:493` — `slog.String("loop_id", result.LoopID),` (the Warn) and
      the cause "is terminal in memory and its record is not" — to "terminal in memory or no longer held; the record
      decides", logging the record's state beside the result's (design P7). DONE: Warn "…terminal in memory or no longer held, and its record is absent or terminal" with `record_state`; transient cause reworded.
- [x] 1.3 Doc comments on `recordTerminalToolResultDropped` (TO:506-511) and on the metric help —
      `processor/agentic-loop/metrics.go:166` — `Name:      "tool_results_dropped_total",` — saying a result the
      carrier settles counts under `terminal_unproven` on every lane (approval answer, model response, sweeper
      auto-reject included), while the handler-entry guards keep their own families (design § 7). DONE: doc comments on `recordTerminalToolResultDropped` and above `toolResultsDropped` in metrics.go (Help text unchanged).

## 2. The carrier's own write refuses a terminal snapshot (design § 3.2; OQ3 (ii))

- [x] 2.1 **[OQ3 (ii)]** Rename the body of `processor/agentic-loop/component.go:3025` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
      to `writeLoopRecord(ctx, loopID, terminalWriter bool)`; `persistLoopState` becomes the one-line wrapper passing
      `true` (the owner's step 4, `processor/agentic-loop/terminal_owner.go:198` — `if err := c.persistLoopState(ctx, loopID); err != nil {`, unchanged).
      Inside the critical section, at `processor/agentic-loop/component.go:3050` — `data, err := c.marshalLoopRecord(loopID)`,
      read the entity first and, when `!terminalWriter` and the loop is not found or terminal, return the new
      unexported sentinel `errTerminalOwnedElsewhere` (declared beside `processor/agentic-loop/handlers.go:2625` — `var errCancelledBeforeMutation = errors.New("cancelled before any loop mutation")`). DONE: `writeLoopRecord(ctx, loopID, terminalWriter)`; the refusal reads the same `GetLoop` entity it marshals, under `loopRecordMu`; `errTerminalOwnedElsewhere` beside `errCancelledBeforeMutation`. Verified by T7/T8/T9 and mutation M2.
- [x] 2.2 **[OQ3 (ii)]** The two carrier sites call the `false` form and map the sentinel first:
      `processor/agentic-loop/component.go:2276` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
      (the gated tail) and `processor/agentic-loop/component.go:2325` — `if err := c.persistLoopState(ctx,
      result.LoopID); err != nil {` (`publishThenPersistResultState`) — `errors.Is(err, errTerminalOwnedElsewhere)` →
      `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`; the lost-CAS and Fatal mappings
      below each are unchanged. In `publishThenPersistResultState` the stamp's error is tested first too —
      `processor/agentic-loop/component.go:2315` — `if err := c.stampPublishedRequest(result); err != nil {` —
      `errors.Is(err, ErrLoopNotFound)` (from `SetPublishedRequest`, `processor/agentic-loop/state.go:1267` —
      `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`) → the same guard settlement; any other stamp error keeps
      its Fatal wrap (the stamp runs before the render for a result that mints the next request, design § 3.2). DONE: both carrier sites map the sentinel first; the stamp's `ErrLoopNotFound` is mapped in `publishThenPersistResultState`. Verified by T8's tool arm and mutation M3.
- [x] 2.3 **[OQ3 (ii)]** `LoopManager.GetLoop` wraps the sentinel: `processor/agentic-loop/state.go:664` — `return
      agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "GetLoop", "find loop")`
      becomes `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound)` inside the same `errs.Wrap`, as
      `processor/agentic-loop/state.go:785` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),` and
      `processor/agentic-loop/state.go:1936` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager",
      "CancelLoop", "find loop")` do. Verify the blast radius: `git grep -n "errors.Is([a-zA-Z]*, ErrLoopNotFound)" --
      'processor/agentic-loop/*.go' ':!*_test.go'` → 4 sites at base (ARH:194, AS:105, C:3303, C:3311); the one whose
      input changes is ARH:194 via `processor/agentic-loop/approval_response_handler.go:85` — `return HandlerResult{},
      getErr` (row A8 takes the cold branch on its first delivery; block 1's scenario is updated). Alternative if the
      owner refuses the wrap: a private `(entity, held bool)` accessor beside `GetLoop`, used only by 1.1 and 2.1.
      Blast-radius addition: `processor/agentic-loop/approval_sweeper.go:105` — `if errors.Is(err, ErrLoopNotFound) {`
      — now also matches an ARH:85 not-found, so the sweeper logs "already released" (AS:108) instead of "auto-reject
      failed" (AS:131) for a loop released between its snapshot and the re-read — benign; comment it. DONE: `GetLoop` wraps `ErrLoopNotFound`; `errors.Is(…, ErrLoopNotFound)` readers at this revision: ARH `handleApprovalResponseMessage`, AS:105 (commented), the carrier's three new sites, and `settleUncancellableLoop`'s two (their input is `CancelLoop`'s error, unchanged). `TestLoopManager_GetLoop` asserts the sentinel; the A3/A8 fixtures carry the new shape. Mutation M4.

## 3. The test-only hooks (design § 3.3; OQ5)

- [x] 3.1 Add `testApprovedDispatchHook func(loopID, stage string)` to `MessageHandler` and
      `testCarrierHook func(loopID, stage string)` to `Component` (unexported; doc comments cite the precedent
      `processor/agentic-loop/component.go:157` — `testPublishHook func(subject string, data []byte)`). DONE.
- [x] 3.2 Call sites, nil-checked: `before_dispatch` after `processor/agentic-loop/approval_response_handler.go:103` — `}` (the `IsTimedOut` block);
      `dispatched` after `processor/agentic-loop/approval_response_handler.go:138` — `if err := h.dispatchToolCall(result, loopID, tc); err != nil {` returns nil;
      `checked` after the 1.1 block; `published` after `processor/agentic-loop/component.go:2307` — `if err := c.publishResults(ctx, result); err != nil {`'s
      block, before `stampPublishedRequest`. `git grep -n "testApprovedDispatchHook\|testCarrierHook" -- '*.go' ':!*_test.go'`
      → exactly 6 lines (2 fields, 4 calls). Cheaper row (OQ5): the `dispatched` call alone — then T7–T9 stay
      unforced and 4.3 is marked unproven. DONE: 2 fields and 4 nil-checked call sites (`git grep` prints 12 lines: each call site is two lines, each field one line plus one doc-comment line naming it). `before_dispatch` is consumed by `TestALoopReleasedBeforeTheApprovedCallIsRegisteredIsRetriedThenInapplicable` and `TestARejectWhoseLoopWasReleasedAfterItsGateResolvedIsSettledColdOnItsFirstDelivery`; `checked` by `TestACancelBetweenTheCarriersCheckAndItsPublicationLetsOnePublicationOut` (review 1 MEDIUM 3).

## 4. OQ2 (b) and OQ7 (b) only

- [ ] 4.1 **[OQ2 (b)]** In `settleApprovalResponseWithoutLoop` — `processor/agentic-loop/approval_response_handler.go:360` — `func (c *Component) settleApprovalResponseWithoutLoop(` —
      between the gate identity check ending at `processor/agentic-loop/approval_response_handler.go:380` — `}` and
      `processor/agentic-loop/approval_response_handler.go:382` — `if gate.RequestID != record.entity.PublishedRequestID {`,
      read `COMPLETE_<loopID>` as `processor/agentic-loop/terminal_owner.go:389` — `entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))` does
      (not-found → continue; read error → transient; decode error or foreign loop → fatal, as TO:398-408). A
      `saved.failed != nil` marker: seat and fail as `processor/agentic-loop/approval_response_handler.go:421` — `func (c *Component) failContinuationUnavailable(ctx context.Context, record loopRecord, cause error) error {`
      does (ARH:423-430), then `handleLoopFailure(ctx, loopID, saved.failed.Reason, errors.New(saved.failed.Error))`;
      on nil, `recordApprovalInapplicable(response)` and `return false, nil`. Other kinds fall through.
- [ ] 4.2 **[OQ2 (b)]** Replace block 2's W2 sentence and the third added scenario's first THEN with the (b) text named
      in design § 2, and the migration bullet with its bracketed (b) variant (§ 5).
- [ ] 4.3 **[OQ7 (b)]** Only on a revised #1362 ruling 1: in `adoptDurableCancel` —
      `processor/agentic-loop/terminal_owner.go:385` — `func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {` —
      a failed marker over a gated record takes the 4.1 seat-and-fail path and the cancel is acknowledged
      `already_terminal`. Not designed further (design OQ7).

## 5. Tests (design § 4) and spec

- [x] 5.1 T3/T4: move the probe's harness (`raceProbeBucket` `processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:84` — `type raceProbeBucket struct {`,
      `startProbeLane`, `gatedRunLoop`, metric snapshots, `approvedToolCallsOn`) into a landed integration test file;
      replace the Warn pause (`processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go:48` — `type pauseOnLog struct {`)
      with the `dispatched` hook; flip `TestProbeW3HeldLoopCancelDuringApprovalDispatch` (PROBE:373) and
      `TestProbeW4ReleasedLoopQuarantinesApprovalLane` (PROBE:467) into the assertions of design § 4 T3/T4, including
      T3's redelivery (`approval_inapplicable` +1) and T4's second-loop control. Delete PROBE.
      `// spec: agentic-loop / The loop record names its outstanding request`. DONE: `carrier_terminal_race_integration_test.go` (T3 `TestACancelBeforeTheCarriersCheckPublishesAndWritesNothing`, T4 `TestALoopReleasedBeforeTheCarriersCheckIsSettledByItsRecord`); the probe file is deleted.
- [x] 5.2 T7/T8/T9: the three sub-window orderings through the `published` stage, with the cancel lane paused at its
      marker `Create` (T7), run to completion (T8), or paused after its own record `Update` returns and before
      `processor/agentic-loop/component.go:3391` — `c.releaseLoopTransientState(loopID)` (T9; extend
      `raceProbeBucket.Update` with a post-write pause for lane `cancel-lane`). T7's second half delivers the cancel
      to a second `Component` while the first lane stays paused. Assertions per design § 4; under OQ3 (i) the three
      tests record today's orderings A/B/C as the documented counterexamples instead. T8 also runs on the TOOL lane: a
      batch-completing tool result (it mints the next request) paused at `published`, the cancel lane run to
      completion, released — acks=1, no Fatal, no latch (the stamp's not-found is mapped by 2.2); mutation: drop that
      mapping → this arm quarantines and latches. DONE: T7 `TestACancelAfterTheCheckBeforeItsMarkerLeavesTheRecordToItsOwner`, T8 `TestALoopReleasedBetweenPublishAndWriteIsSettledByItsRecord` (approval and tool arms), T9 `TestACarrierWriteAfterTheOwnersRecordIsRefused`. DEVIATION (harness only): T9 pauses the cancel lane on its `active_loops` decrement, not inside `raceProbeBucket.Update` — the owner's `Update` runs under `loopRecordMu` (`writeLoopRecord`), so a pause there holds the carrier out and ordering C cannot be forced. T8's executed call's result is counted `stale_execution` (the tool lane's cold arm), not `terminal_unproven`.
- [x] 5.3 T1: the two-component W1 counterexample on the `predecessor`/replacement pattern —
      `processor/agentic-loop/task_redelivery_integration_test.go:187` — `require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch))` —
      with both arms (same kind converges; different kind refused, marker unchanged) and, if a seam is found, arm (c):
      the in-process step-0 source (`processor/agentic-loop/loop_evidence.go:416` — `// Residual, stated rather than discovered: this write is deliberately not`);
      else name it unproven (design § 10). Asserts the documented bound; no mutation (an (a) row). DONE: `lost_terminal_record_integration_test.go` `TestALostTerminalRecordConvergesAtTheLoopsNextTerminal` (same kind adopts; different kind refused, Fatal). Arm (c) not forced: unproven (design § 10 item 5).
- [x] 5.4 T2: extend `processor/agentic-loop/approval_loop_deadline_test.go:219` — `func TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {`
      with the iteration-cap arm (record at the cap, `TimeoutAt` zero, sweep with `unpublishableClient`, then a reject
      and, separately, an approve) and the cancel arm (a cancel signal on the released W2 loop → Retry, record still
      gated): under (a) assert one `tool.execute` on the approve and adoption when its result completes the batch;
      **[OQ2 (b)]** assert nothing dispatched, `approval_inapplicable` +1, record `failed` with the saved reason, and
      mutation evidence: delete the marker read of 4.1 → red on "no tool.execute". DONE (OQ2 (a)): `TestASweepAtTheIterationCapWhosePublishFailedSettlesOnTheNextAnswer` (reject, approve, cancel arms) in `approval_cap_sweep_integration_test.go`, counting `tool.execute` on a real stream: the reject publishes none, the approve publishes one (review 1 LOW 9).
- [x] 5.5 Fix the one test the check changes: `processor/agentic-loop/publish_phase_fatal_test.go:31` — `t.Parallel()` — `TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified`
      creates `loop-publish-phase` in the handler (`CreateLoopWithID`, as `trajectory_eviction_internal_test.go:39`)
      so its non-terminal result passes the check and the classification stays the publish's. Run every
      `persistHandlerResult` test call site (`git grep -n "persistHandlerResult(" -- 'processor/agentic-loop/*_test.go'`
      → 28 at base) and name any other that drives a non-terminal result on an unheld loop. DONE: `publish_phase_fatal_test.go` holds its loop. With the check applied, the full unit and integration suites for the package showed only this test and the two probe tests red; no other `persistHandlerResult` call site drives a non-terminal result on an unheld loop.
- [x] 5.6 Mutation evidence for the WIRING (`cp` backup, `md5 -q` before and after equal). Observed (the design's
      pre-(ii) predictions for M1 are superseded — under OQ3 (ii) the 2.1 refusal also produces the write refusal, the
      Ack and the `terminal_unproven` count, so stopping the publication is the entry check's only unshared effect):
      M1, delete the 1.1 block → T3 red on "no approved tool.execute is published for a loop cancelled in memory" and
      "the redelivery publishes nothing", T4 red on "nothing is published" (its acks and health stay green);
      the `checked` variant stays green under M1 (the cancel lands after the check). **[OQ3 (ii)]** M2, delete the 2.1
      refusal → T7 red on the disposition, "no carrier write", the record state and the second process's cancel
      (`stale_loop_id`); T8's approval arm red on acks and drain; T9 red on the write count (two writers); the
      `checked` variant red on "no carrier write", the Nak, the revision and "exactly one terminal writer". M3, drop
      the stamp mapping → T8's tool arm red on acks and drain. M4, revert the 2.3 wrap → `state_test.go` red on the
      sentinel, and the released reject red on its first-delivery disposition (Nak instead of the cold Ack, no
      `approval_inapplicable`). Every run restored with equal `md5 -q`; runs recorded for the PR body.
- [x] 5.7 Controls green by name with `-count=1`, output pasted — the 14 order tests the archived transition-result
      design § 5 lists: `TestAnApprovalGateIsWrittenBeforeItsEventIsPublished`,
      `TestTheApprovalTimeoutSweepNamesTheRequestItPublished`, `TestBirthRefusesASecondCreateForTheSameLoop`,
      `TestBirthWhosePublishFailsIsNotAcknowledged` (loop_carrier_test.go:157/:231/:352/:383),
      `TestAGateIsWrittenBeforeItIsPublished`, `TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged`
      (persist_handler_result_test.go:208/:252), `TestTerminalOwnerArms`,
      `TestCancelTakesTheTerminalOwnerAndClearsAPendingApproval`, `TestLoopFailureTakesTheTerminalOwnersOrder`,
      `TestAResponseMeetingAnUncommittedTerminalWritesNothing`, `TestAResponseMeetingACommittedTerminalIsAcknowledged`,
      `TestAToolResultForATerminalLoopTouchesNothing`, `TestAnApprovalWhoseRecordMovedIsRetried`
      (terminal_owner_test.go:72/:186/:226/:306/:324/:376/:410), `TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains`
      (publication_semantics_integration_test.go:125, integration-tagged) — plus
      `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt` (`processor/agentic-loop/approval_restore_order_test.go:316`),
      `TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind`, `TestApprovalLanePublishesBeforeItWrites`
      (loop_carrier_test.go:70/:106). **[OQ3 (ii)]** the A8 test `TestAnApprovalWhoseRecordMovedIsRetried` and the
      S:886 "recovered cold" scenario's test(s) are re-read for the first-delivery cold branch. DONE: all 17 named controls PASS with `-count=1`. A8 re-read: `TestAnApprovalWhoseRecordMovedIsRetried` drives a lost CAS, not ARH's re-read; the literal ARH re-read race stays unforced (no seam between the resolve and `GetLoop`), but block 1's changed scenario is forced on the lane one read later by `TestARejectWhoseLoopWasReleasedAfterItsGateResolvedIsSettledColdOnItsFirstDelivery` (a reject reaching `HandleToolResult`'s `GetLoop`; M4 turns it red).
- [x] 5.8 `openspec validate agentic-loop-committed-terminal-recovery --strict` green; `task spec:properties` count
      moves by the new `// spec:` citations (`git add` the new test file first). DONE: `openspec validate --strict` valid; `task spec:properties` 430 -> 440 (eight in the race file, T1, T2).
- [x] 5.9 Migration: replace `docs/operations/migration-beta162-to-beta163.md:2015` — `**Two residuals, recorded and not reconciled** (#1362 issuecomment-5808903072 and issuecomment-5809906669):`
      through `:2025` with design § 5 (the bracketed (b) variant only under OQ2 (b); the spawn-path source
      `docs/operations/migration-beta162-to-beta163.md:2019` — `a different outcome is quarantined. The same shape follows a spawn-path birth failure under a producer-supplied loop` is kept);
      the spec's residual sentences are replaced through the delta's sync (6.5). DONE.

## 6. Gates

- [x] 6.1 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines, not "green". The landed W3/W4/T7–T9 tests run under `-race -count=20` once, output pasted.
      DONE at `89d71673`: build, lint, tagged vet, `schema:check-changes`, contract, `go test -race ./...` all green;
      integration through the runner 154 `ok`, ONE red = `test/testinfra`
      `TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock` = #1397 (outside this diff; waived by the owner
      for this merge, PR #1388 comment 2026-09-26). `-race -count=20`: the six W3/W4/T7–T9 tests 20/20 at `08f17344`;
      the released-reject test, the `checked` variant, T9 and T2 20/20 at `89d71673`; `DATA RACE: 0` both runs.
- [ ] 6.2 `task e2e:agentic` green on the final diff (proposal § Impact: the BREAKING gate walks the approval path;
      `verifyApprovalAcrossReplacement` is the replacement control, design § 4 T5). Paste the tier's summary line.
- [x] 6.3 `task api:compat`: no exported symbol is added or removed by this diff (the hooks and the sentinel are
      unexported; `GetLoop`'s signature is unchanged); report the pre-existing Tier 1 breaks as the archived task 5.2 did.
      DONE: `compared: 62 clean: 47 incompatible: 15 removed: 0 added: 0` — the 15 are the pre-existing Tier 1 breaks
      against beta.162; the production diff against `282e650e` adds and removes no exported declaration.
- [ ] 6.4 PR body: `implemented-by: <persona>`, the OQ answers quoted from #1377, the 5.6 mutation runs, the 6.1/6.2
      lines; the § 8 table posted on #1146 with the amendment flags before the epic closes.
- [ ] 6.5 Archive: `openspec archive agentic-loop-committed-terminal-recovery --yes` as the last content commit; both
      MODIFIED blocks sync into `openspec/specs/agentic-loop/spec.md`.
