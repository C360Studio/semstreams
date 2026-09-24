# Tasks — agentic-loop-restart-l4b (#1362)

Base: `c4a79fd5`. Pin keys:

- `processor/agentic-loop/`: C `component.go`, ARH `approval_response_handler.go`, AS `approval_sweeper.go`, H
  `handlers.go`, M `metrics.go`, GD `governance_dispatcher.go`, LE `loop_evidence.go`, LC `loop_classification.go`, ST
  `state.go`
- AG `agentic/state.go`
- KV `natsclient/kv.go`
- TC `processor/agentic-tools/component.go`
- DHA, DM and DC: `processor/agentic-dispatch/{http_activity,metrics,commands}.go`
- CTR `processor/agentic-dispatch/command_target_resolution_test.go`

Archived task numbers are in brackets. Spec-text rulings are OQ-A to OQ-F in #1362 issuecomment-5799118983.

## 1. Approval lane (design § 5.5, and the § 5.4 re-echo)

- [x] 1.1 [3.3] Build the cold KV branch at ARH:58 (`if !ok {`). Today `ErrLoopNotFound` folds into `staleDrop`
  (ARH:78) and is acknowledged (ARH:194-199).
  - Step 0 runs through `adoptNewerRetainedRequest` (LE:425); the gate clear is LE:508-509.
  - Then require `awaiting_approval` with a matching `ExecutionID` and `CallID`; anything else is ACKed as
    inapplicable. As today, only an absent or terminal record is acknowledged (D35).
  - Check I4, and that the gated result is present, against the adopted record.
  - Rebuild through `restoreLoopFromEvidence` (LE:577) and `restoreLoopFromRequest` (ST:364).
  - `republishPendingApproval` shipped in the cold tool arm, not this branch (design § 5.4; owner ruling 1, #1362
    issuecomment-5809906669): a redelivered `approval_required` result whose gate the record still holds re-publishes
    the `ApprovalPendingEvent` from the record without seating the loop, then ACKs; a publish error retries.
  - Approve and modify go through `dispatchApprovedCall` (ARH:117-130); reject goes through `handleRejectedApproval`
    (ARH:139-158).
  - Tests (both new): `approval_timeout_recovery_test.go`, `approval_restore_order_test.go`.
- [x] 1.2 [OQ1] Build one branch, inside 1.1:
  - The retained R, or its response, is confirmed absent → the loop fails with `continuation_unavailable` through the
    terminal owner (§ 3).
  - Transient uncertainty → Retry.
  - Conflicting or poison evidence keeps its existing disposition (owner ruling 2026-09-13 on #1146).
- [x] 1.3 [3.3, D28] Warm re-echo at H:2814 (`if entity.State == agentic.LoopStateAwaitingApproval {`), which today
  returns without an echo. When the result's `ExecutionID` equals `PendingApproval.ExecutionID`, re-echo the
  `ApprovalPendingEvent`, built as in `gateForApproval` (H:2844), and ACK.
- [x] 1.4 [2.1] Flip ARH:208 from `writeThenPublish` to `publishThenWrite`; the carrier's branch at C:2288 does the
  rest (C:2362).
  - Land it in the same commit as 1.1. The flip opens the reject-minted W4 window, and 1.1 closes it (§ 5.5).
  - Update `loop_carrier_test.go:125`, which asserts the old order.
- [x] 1.5 [2.1, D39] Move the sweeper onto the carrier. Replace the publish, stamp and write sequence at AS:120,
  AS:146 and AS:152 with `persistHandlerResult(ctx, result, publishThenWrite)`.
  - L4a already gave the sweeper that order and the CAS, so only its home changes.
  - `approval_timeout_publish_failure_integration_test.go` and the scenario "An approval timeout whose rejection could
    not be published leaves the record as it was" stay green unchanged.
  - Failure signal: log-only (OQ-B).
- [x] 1.6 [2.7, docket OQ8, conditional] Write the test first. It proves that an approval answer arriving before its
  gate is durable is Retried by 1.1's cold branch (the gate branch in `persist_handler_result_test.go`, plus the lane
  case).
  - If the test passes: drop `gated` from C:2284 and C:2288 (uniform publish → `Update`), and swap the delta's gate
    sentence for variant B.
  - If it cannot pass: write → publish stands for gates, and the test is kept as the recorded proof. This is the
    expected outcome (OQ-A): design § 5.5 step 1 ACKs an answer that is not `awaiting_approval`.
  - The PR body records which branch shipped, and why.

## 2. Governance verdict after waiter loss (design § 5.6, D16)

- [x] 2.1 [3.6] At C:3611, inside `settleVerdictWithoutWaiter` (C:3597, entered from C:3572), read the record.
  - Acknowledge when any of these holds: `verdict.RequestID` is older than `PublishedRequestID`; the `ExecutionID` is
    in `PendingToolResults`; the record is absent or terminal.
  - Each acknowledgement carries a reason label beside M:394-395 (recorder M:405).
  - A current, unseen verdict is Retried (C:3618, unchanged).
  - A verdict with an empty `request_id` (GD:141, `omitempty`) is classified on membership only.
  - No retained-verdict reader is built (D14/D15).
  - Test (new): `verdict_wire_test.go`.

## 3. One terminal owner (design § 5.7, D41/P6; ruling 2026-09-18 on #1330)

- [x] 3.1 [3.7] Replace the three marker `Put`s with one owner. The three paths today:
  - carrier: C:2322 → C:2396 (entity) → C:2403/C:2423 → C:2950/C:2981 → stamps → publish C:2341
  - loop failure: C:2042 → C:2060 → C:2102 → C:2117 → C:2139
  - cancel: C:3452 → C:3469 → publish C:3506 → marker C:3521 → C:3005 (the marker is written after the event today)

  The owner runs, in order:
  1. Marker `Create` (KV:211). On `ErrKVKeyExists` (KV:218), read the marker back and adopt it.
  2. Graph stamps.
  3. Publish.
  4. Entity `Update` (`persistLoopState`, C:3173 and C:3219). A revision conflict → Retry.

  Rules for the owner:
  - Arms (a), (b) and (c) are as in § 5.7.
  - Content differences are logged at the audit line and never decide the disposition.
  - Q7(a) stays outside the owner: C:2637 and LC:286 (warm), C:2767 (cold).
  - Clear `PendingApproval` on the terminal transition. Today only `ResolveApproval` clears it (AG:290, not
    AG:199-217).

  Test (new): `terminal_owner_test.go`, arms (a), (b) and (c).

  Landed with checkpoint 1 (PR #1366): the cold cancel adopts its durable cancel marker; a failed commit releases the
  loop; terminal guards decide by the record (terminal → ACK, live → Retry) and run before any mutation or timeout arm
  (#1362 issuecomment-5802753726, -5808903072). Residual, recorded not built: Q7's warm arm
  (`classifyRedeliveredToolResult`) ACKs a tool result for a loop terminal in memory without reading the record; after
  this checkpoint that memory is only a commit in flight, so a failed commit can lose one ACKed result of a loop that
  was ending.

## 4. Evidence

- [x] 4.1 [4.2] New `terminal_tool_redelivery_integration_test.go` (real NATS, `test/e2e/harness/processbarrier`).
  - (i) The approval lane's reject W4. Crash between the carrier's publish (C:2363) and its `Update` (C:2375) for the
    approval result, then restart and redeliver. Expect ACK inapplicable, R(N+1) counted once, and the record `running`
    at R(N+1) with no gate.
  - (ii) The terminal lane crashes after publish and before `Update`. Expect adoption through arm (b). — landed with checkpoint 1
    (`TestATerminalRedeliveredAfterItsPublicationAdoptsTheDurableTerminal`); (i) is checkpoint 2's.
- [x] 4.2 [4.3(d)] Mutation: restore `Put` for the marker in 3.1, dropping the `Create` read-back. Case (ii) must fail.
- [x] 4.3 [4.3(f)] Mutation: drop the gate clear at LE:508-511. Case (i) must fail on I4 and re-run the rejection.

Mutation evidence uses a `cp` backup and a checksum.

## 5. Route-ambiguity metering (owner ruling 2026-09-21, docket OQ6)

- [x] 5.1 [3.9] Meter the refusal at DHA:334, inside `activeLoop` (DHA:321).
  - Meter through `Component.recordLoopAdmissionRefusal`, so the single-caller property (DM:373-377) holds.
  - Add one resolver-seam value (`route`) and one reason value (`route_ambiguous`) to the Help at DM:181.
  - Update the comment at DC:67-71.
  - Rename `TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate` (CTR:313).
  - Replace the absence assertions at CTR:335 and CTR:349 with a positive count of one.
  - Keep CTR:319 (isolation) and CTR:346-348 (the 409).

## 6. E2E and #1155

- [ ] 6.1 [6.2] New `approval_restart.go` and `approval_restart_test.go` in `test/e2e/scenarios/agentic/`, beside
  `stage_a_process_replacement.go`.
  - Park a loop on approval, kill the process, then answer.
  - Assert that the replacement **applies** the answer. Asserting that a deadline fires is not enough.
  - This stage is #1155's remaining acceptance.
  - It proves the cold branch (`settleApprovalResponseWithoutLoop`) only while no startup re-hydration of parked loops
    from AGENT_LOOPS exists; that re-hydration is deferred as OQ2
    (`processor/agentic-loop/approval_sweeper.go:41-47`), and if OQ2 lands this stage must be revisited (owner ruling,
    #1362 issuecomment-5812283590).

## 7. Truth maintenance

- [ ] 7.1 Rewrite every "#1362" deferral that these tasks make false:
  - `processor/agentic-loop/`: AG:76; ARH:205-207; AS:104-119 and :138; C:2227-2233, :2254-2279, :2315 and :3166;
    `doc.go:319`; `loop_carrier_test.go:66`, `:95`, `:125` and `:143`;
    `approval_timeout_publish_failure_integration_test.go:42`
  - `docs/concepts/17-approval-flow.md:74-80`
  - `docs/operations/migration-beta162-to-beta163.md:1904-1910`
  - `docs/operations/17-tool-call-governance.md` (the verdict counter's row in § Observability and its troubleshooting
    section, rewritten with task 2.1)
- [ ] 7.2 Add a migration section to `docs/operations/migration-beta162-to-beta163.md`, after `:1784`. It states:
  - the entity's terminal `state` now lands after `agent.complete` / `agent.failed`;
  - `COMPLETE_` precedes the event on the carrier, loop-failure and cancel paths, and cancel's marker moves ahead of
    its event; the approval-timeout sweeper's own terminal (`max_iterations`) takes the same order;
  - a crashed cancel is settled cold: its redelivery republishes the saved event and writes the record cancelled;
  - a terminal whose record update lost its CAS after `COMPLETE_` and its event landed is not reconciled (the loop may
    keep running; its own later terminal is quarantined), and the same follows a spawn-path birth failure under a
    producer-supplied loop ID (#1362 issuecomment-5808903072);
  - a response delivered on a context cancelled before handling is retried, not failed;
  - terminal records no longer carry `pending_approval` or `state_before_approval`;
  - `continuation_unavailable` is a new failure reason.
  - an approval-timeout `max_iterations` terminal that commits `COMPLETE_` and then fails to publish is not reconciled:
    the record stays `awaiting_approval`, and a later human answer is applied cold on a loop with a durable failed
    terminal (owner ruling 2, #1362 issuecomment-5809906669);
  - `MessageHandler.HandleApprovalResponse` now returns an error wrapping `ErrLoopNotFound` for a loop the process does
    not hold, instead of a stale-drop result with a nil error.
  - `tool_call_governance_subscribe_before_publish_failures_total`: `missing_waiter` counts every waiterless verdict and
    the six settle reasons (`older_request`, `already_applied`, `loop_absent`, `loop_terminal`,
    `unrecoverable_loop_identity`, `foreign_request`, the last two terminated) are subsets of it, so `sum(...)` across reasons double-counts; the
    signal is `missing_waiter` minus the settle reasons (the counter's section, rewritten with task 2.1).

  Size the section against the keys the semspec watchers read, in one read-only pass.
- [ ] 7.3 Keep the `agentic-loop` delta in step with what shipped (variant A or B from 1.6). Then run
  `openspec validate agentic-loop-restart-l4b --strict` and `task spec:properties`.

## 8. Verification

- [ ] 8.1 `task check:push`: build, lint, tagged vet, schema drift, contract, race unit, integration.
- [ ] 8.2 `task e2e:agentic` green, including 6.1. This is the BREAKING gate for 3.1's order change.
- [ ] 8.3 The PR body carries:
  - `Closes #1362` and `Closes #1155`
  - `implemented-by:`
  - the branch 1.6 took, and why
  - the breaker count, if any rider rode along
