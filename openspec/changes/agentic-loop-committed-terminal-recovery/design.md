# Design — agentic-loop-committed-terminal-recovery (#1377): the per-window docket

> Change id `agentic-loop-committed-terminal-recovery`, claiming #1377 (draft PR #1388). Base main `9e5d8455`; the
> inventory is pinned at `597072c4` (= `9e5d8455` + the proposal + the probe) and re-verified at HEAD `6876fe51`:
> `task inventory:verify` reports `pins=121 ok=121 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. Every
> `file:line` below is at `6876fe51`; production files are byte-identical to `9e5d8455` (inventory header).
>
> **Rulings applied, none reopened.** #1146 issuecomment-5828511934 ruling 4 (docket order (a) → (b) → (c); a second
> design round or any new durable authority returns to the owner with (a)); ruling 5 (implementation last in Lane A,
> after PR #1387); #1377 issuecomment-5828356994 (the two #1366 residuals are inputs, not retroactive defects; #1155
> keeps its scope); the Codex caution (#1377 issuecomment-5838471641) and the coordinating read
> (issuecomment-5838780631): an (a) row is an accepted **limitation** and an explicit owner amendment of the epic's
> first exit clause, never a quiet pass — § 1 and § 8 flag every one; #1362 issuecomment-5808903072 ruling 1 (a
> benign cancel race must not latch the lane) and ruling 2 (non-terminal lanes reading `COMPLETE_` "would need its own
> design"); the 2026-09-22 standing rule (the documented alternative is the first entry of every row); #1372 (one
> round, or the smallest fix).
>
> **Bound.** No new durable fact, no new coordination, no new exported API, `agentic` (Tier 1) untouched. One
> production change is recommended (§ 3, W3+W4), one optional (§ 0 OQ2 (b), W2), one test-only hook (§ 0 OQ5). The
> delta (§ 2) assumes (a) on OQ1 and OQ2, so no scenario depends on an owner answer; the (b) text for OQ2 is named.
>
> Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ARH `approval_response_handler.go`, AS
> `approval_sweeper.go`, TO `terminal_owner.go`, ST `state.go`, LE `loop_evidence.go`, LP `loop_presence.go`, TW
> `trajectory_handler_wiring.go`, LC `loop_classification.go`, M `metrics.go`, AST `agentic/state.go`, DS
> `natsclient/delivery_settlement.go`, S `openspec/specs/agentic-loop/spec.md`, MIG
> `docs/operations/migration-beta162-to-beta163.md`, PROBE
> `processor/agentic-loop/held_loop_cancel_approval_race_probe_integration_test.go`, ALD
> `approval_loop_deadline_test.go`, ARO `approval_restore_order_test.go`, TRI `task_redelivery_integration_test.go`,
> PPF `publish_phase_fatal_test.go`.

## 0. Owner questions — first

Each question names the cheaper row first. The recommendation follows it.

### OQ1 — W1, lost record CAS: (a) documented bound, or (b)/(c). Recommendation: **(a)**

- **(a) — the bound, no code (recommended).** A terminal that lost its record write leaves `COMPLETE_<loopID>` and the
  event over a live record. That loss needs a writer in **another process** (P2): the loop runs on there, and its
  record converges at that loop's next terminal commit, through a seam that already exists — `createTerminalMarker`'s
  refused `Create` reads the saved terminal back and adopts it by loop ID and kind (TO:262-313), and `kind()` is
  reason-agnostic (TO:42-52), so a `timeout` failure adopts a `max_iterations` marker and vice versa. A terminal of a
  different kind is refused (TO:296-298) and quarantined — the ruled "first terminal wins" (S:1612-1614). **Bound:**
  the holding loop's remaining budget (its iteration cap, or `timeout_at`; a loop with `timeout_at` zero has no time
  bound, ST:1550-1552). **Guaranteed:** the durable terminal is never overwritten (create-once), a same-kind terminal
  converges the record and republishes the saved event, and a different kind never silently replaces it. **Not
  guaranteed:** the epic's first clause — the loop resumes ordinary work under a durable terminal until its own
  terminal. That is an amendment (§ 8).
- **(b)/(c) — any lane consulting the marker before advancing.** The only existing marker readers are the cancel
  lane's cold arm (TO:389) and the owner's step 1; a read on the warm model/tool/approval paths is the shape ruling 2
  of #1362 issuecomment-5808903072 deferred to "its own design", and it would add a KV read to every advancing
  delivery for a window whose precondition is a second process writing one loop's record — the active/active shape
  #1377's non-goals exclude. Under ruling 4 that is (c): returned here with (a).

### OQ2 — W2, sweeper terminal whose publication failed: (a) documented bound, or (b) cold-branch adoption. Recommendation: **(a)**, with (b) fully specified so it needs no second round

- **(a) — the bound, no code (recommended).** The sweeper is memory-only (AS:41-47); a failed commit releases the loop
  (TO:151-163, H1 of #1362), so no later tick re-finds it (inventory § 4). The record converges on the **next answer to
  that gate, or a cancel**: the cold branch rebuilds the gated loop (ARH:360-410) and the answer runs warm. A reject,
  or any answer to a loop past its own deadline, dispatches nothing — `IsTimedOut` (ARH:101) or the batch's
  `max_iterations` re-derivation fails the loop and `commitTerminal` adopts the marker (`TestASweepTimeoutWhosePublish
  FailedSettlesOnTheNextAnswer`, ALD:219-241, proves the timeout case). An **approve** of a loop at its iteration cap
  dispatches the approved call once (ARH:107 → H:2035); when its result completes the batch, `handleToolsComplete`
  re-derives `max_iterations` (H:3005-3037) and the terminal owner adopts the saved terminal. **Bound:** until the next
  answer or cancel (no time bound: the record is parked `awaiting_approval`); at most one approved call executes after
  the durable terminal. **Not guaranteed:** the first clause, for that one call. Amendment (§ 8).
- **(b) — the approval lane's cold branch adopts a durable failed terminal it finds under the gated record** (ruling
  4's named shape: the existing seam that meets the gated record, adopting an existing durable fact, on the accepted
  trigger). After ARH:375-380 establishes "live record awaiting this gate", read `COMPLETE_<loopID>` (the `Get` at
  TO:389); a marker that decodes to a **failed** terminal naming this loop is adopted through the path that already
  exists for a gated record that cannot continue: `seatRecordToFail` + `handleLoopFailure(ctx, loopID,
  saved.failed.Reason, errors.New(saved.failed.Error))` (the `failContinuationUnavailable` shape, ARH:421-431) →
  `commitTerminal` → `createTerminalMarker` adopts by kind, republishes the saved event, writes the record terminal and
  counts it once with the saved reason (TO:168-208, TO:225). The answer is acknowledged and counted inapplicable
  (`recordApprovalInapplicable`, ARH:291). A marker of another kind falls through to today's rebuild (a cancel marker
  is the cancel lane's own to adopt, TO:385; a success marker under a gated record is W1-shaped). ~20 lines at one
  seam, one 10-line read helper, two tests (§ 4). **Gained:** no call is dispatched on a loop with a durable failed
  terminal, on the accepted trigger. **Cost:** touches a #1362-reviewed cold path for a window that needs a sweeper
  terminal, a publish failure between marker and event, and a later approve of a loop at its cap; the standing rule
  (doc sentence first) and #1372 both point at (a). If taken: § 2 names the one sentence and one scenario that change.

### OQ3 — W3 fix site: the carrier's entry check alone, or entry plus a post-publish check. Recommendation: **entry only**

- **The fix (§ 3.1).** `persistHandlerResult` reads the loop it is about to publish for and write from, once, at entry,
  for a non-terminal result: terminal in memory or not held → publish nothing, write nothing, and settle by the record
  through the existing guard settlement (`settleTerminalGuard`, TO:481-503). This is the seam every lane's
  non-terminal result passes (7 production callers, inventory § 1 searches) and the one place that already reads memory
  at write time (`marshalLoopRecord`, C:3167-3168). It flips both probe tests (§ 4).
- **What it leaves (the sub-window).** A cancel landing between that check and the publish (C:2261 →
  `publishThenPersistResultState`'s `publishResults`) lets one publication out and the write that follows renders the
  loop's terminal state from memory: the carrier writes `cancelled` once, outside the owner, before `COMPLETE_`; the
  owner's own write follows with the same content (the carrier refreshed the observed revision under the shared lock,
  C:3081, so the owner's compare-and-swap succeeds). Width: one publish latency. Observable only to a watcher ordering
  the record against the marker.
- **Alternative — a second read after the publish**, before the write (inside `publishThenPersistResultState`, before
  C:3025's caller), refusing the write when memory went terminal: closes the second-writer half of the sub-window; the
  published call is out either way. One more map read under `RLock`; a second site for one condition. Recommended
  against under simple-over-edge-case; the sub-window is stated in the delta as a bound.
- **Drop metric on the carrier's Ack branch.** The guard settlement counts through its callback; the carrier does not
  know the lane, so it passes the existing `terminal_unproven` recorder (TO:508-511). An approval answer settled there
  counts under `terminal_unproven` rather than `approval_inapplicable` (§ 7 residual; the Retry branch — the common
  case for W3 — leaves counting to the lane's cold branch, ARH:370/378).

### OQ4 — W4 classification: the doc sentence, or the same carrier check. Recommendation: **the check** (it comes free)

- **(a) — the sentence, no code:** "a cancel that releases a loop while an approval for it is mid-dispatch quarantines
  that delivery and latches the approval lane; a restart clears it." An availability defect stated as a bound.
- **The check (recommended).** With § 3.1 in place, the released loop fails the carrier's `GetLoop` (ST:655-666) and the
  record — already `cancelled`, an existing durable fact — settles it: acknowledged without effect, nothing published,
  nothing written, nothing latched. This is #1362 issuecomment-5808903072 ruling 1's reasoning applied one lane over:
  a benign cancel race must not latch health. No separate code: OQ3's fix covers it because the check tests "not
  held" as well as "terminal in memory".
- **The literal pre-`AddPendingTool` point** (a cancel that released the loop between ARH:83 and H:2036) is unforced;
  from code it is `AddPendingTool`'s "loop not found" (ST:1118-1120), wrapped at ARH:139, unclassified → Retry
  (ARH:240) → redelivery → cold branch → inapplicable Ack (ARH:368-380). Residual row (§ 7), not a change.

### OQ5 — the seam the landed tests use: a test-only hook, or the probe's misconfiguration Warn. Recommendation: **the hook**

- **The hook.** `testApprovedDispatchHook func(loopID string)` on `MessageHandler`, nil in production, called in
  `dispatchApprovedCall` after `dispatchToolCall` returns nil (ARH:138-141): after `AddPendingTool` (H:2036), before
  the carrier — exactly the probe's pause point. Precedent: `testPublishHook` and `testLineageWriteHook` on `Component`
  (C:153-160). Cost: one unexported field, one nil check, approval lane only. `dispatchToolCall` has no log line after
  `AddPendingTool` (`awk` over H:2035-2165 → 0 `logger.` hits), so there is no legitimate log seam to ride.
- **The probe's seam** (the `resolveRunEntityID` Warn, H:632): zero production change, but the pause exists only in a
  component with no platform identity — a degraded configuration — and any fix to that Warn silently unpins the test.
  Recorded on #1377 as a caveat; not recommended for landed tests.

## 1. The per-window docket

Columns per ruling 4 and the coordinating read: window · path (a/b/c) · existing durable fact used · what is guaranteed ·
what is only bounded · smallest fix at which seam · amendment flag. The documented alternative is the first entry of
every row (2026-09-22 rule).

| Window | Path | Existing durable fact | Guaranteed | Only bounded | Smallest fix, seam | (a) = epic amendment? |
|---|---|---|---|---|---|---|
| **W1** lost record CAS after marker + event (C:2242-2250; S:1617-1619) | **(a)** documented. (b)/(c): warm lanes reading the marker → own design (#1362 ruling 2) → returned with (a) | `COMPLETE_<loopID>` (TO:84), adopted by the owner's step 1 (TO:262-313) | the marker is never overwritten; a same-kind later terminal adopts it, republishes the saved event, writes the record terminal (TO:171-208); a different kind is refused (TO:296) and quarantined, first terminal wins (S:1612-1614); the redelivered terminal input is acknowledged as older (S:1623-1624) | the loop resumes ordinary work **in the other process** until its own terminal: its remaining iteration cap or `timeout_at` (none if zero, ST:1550-1552); precondition is a foreign writer (P2) | none | **YES** — the first clause is not met between the lost write and the loop's next terminal |
| **W2** sweeper terminal commits marker, publish fails (AS:152-163; S:1620-1623) | **(a)** documented, recommended. **(b)** the approval cold branch adopts a failed marker under the gated record (OQ2) | `COMPLETE_<loopID>` and the gated record (`awaiting_approval`, `pending_approval`) read at ARH:364-380 | (a): the record converges on the next answer or cancel; reject, and any answer past `timeout_at`, dispatch nothing (ARH:101; ALD:219-241); an approve at the cap dispatches once and the terminal is adopted when its result completes the batch (H:3005-3037 → TO:262). (b): nothing is dispatched; the answer adopts the terminal and is acknowledged inapplicable | until the next answer or cancel (no time bound: parked `awaiting_approval`); (a) admits one approved call after the durable terminal | (a) none. (b) ~20 lines after ARH:380 + a marker-read helper, via `seatRecordToFail` + `handleLoopFailure` (ARH:421-431) | (a) **YES**, for the one approved call. (b) no |
| **W3** cancel lands while the loop is held, after `AddPendingTool` (PROBE:373; ARH:107 → H:2036 → C:2261) | **fix** (no (a): the coordinating read rules "not supported" unavailable — the first clause broken on a live path) | the loop's in-memory terminal (the cancel lane's `CancelLoop`, ST:1925-1959) and its record (`readLoopRecord`, LE:282) | a non-terminal result reaching the carrier after the cancel committed in memory publishes nothing and writes nothing; the delivery is retried and its redelivery is acknowledged inapplicable by the lane's cold branch after the cancel's record lands (ARH:368-380); one terminal-record writer (TO:225-230) | a cancel between the carrier's check and its publish lets one publication out; its result is acknowledged without effect on the terminal loop (LC:284-297 warm, LE:282 cold) and the carrier's write renders `cancelled` once before the owner's identical write (OQ3); the executor never stops a published call (inventory § 6) — loop-side only | one read of the held loop at the top of `persistHandlerResult`'s non-terminal path (C:2232), settled through `settleTerminalGuard` (TO:481) | not an (a) row |
| **W4** cancel completes and releases the loop while the approval is mid-dispatch (PROBE:467; C:3168 Fatal → ARH:280-281 → latch C:1071) | **(a)** the sentence ("latches the lane; restart clears it"), or **the same check** (recommended, OQ4) | the record, already `cancelled` (an existing durable fact the probe measured at rev 8) | with the check: the delivery is acknowledged without effect, nothing published, nothing written, health untouched, the lane keeps consuming — the next valid answer dispatches | the pre-`AddPendingTool` variant is unforced (OQ4): Retry → cold → inapplicable Ack, from code | the same read: `GetLoop` fails → the record decides | not an (a) row under the recommendation; (a) would be a bound on availability, not on the first clause |

## 2. The MODIFIED requirement — what changes and the scenarios, verbatim

The delta modifies ONE requirement, `The loop record names its outstanding request` (S:1587-1861: the residual sentences
at S:1617-1623 live here, and so does the terminal-order sentence a second writer violates, S:1609-1611). All 22
existing scenarios are restated unchanged (openspec 1.7.0 refuses omissions): W4 tool lane · A cold replacement adopts
the newest retained request · A rebuilt loop keeps its record's deadline · An approval answer does not outlive its
loop's deadline · A task rebuilt at iteration zero keeps its record's deadline · A replaced process re-arms no approval
deadline · An approval timeout whose rejection could not be published leaves the record as it was · A stale tool result
is acknowledged without effect · A response that outruns the record update retries · A second delivery of an applied
response is acknowledged without effect · A terminal loop receives a result it cannot prove it applied · A task
redelivered at iteration zero publishes the first request nothing retains · … over a first request the stream retains
· … over a first batch that already applied something · A continuation naming a loop no process holds is refused · A
tool result the record already applied is replayed · A continuation deferred while a request is unpublished writes only
its marker · A rebuilt loop clears a deferred turn whose text it cannot recover · A cold replacement adopts past a
rejection-minted request … · A governance verdict redelivered after its waiter is gone · A redelivered terminal adopts
the published terminal by identity · A governance verdict naming a request of another loop is terminated ….

The settlement requirement (S:886) is **not** modified: its scenario "A durable terminal with a non-terminal record is
owed to #1377" (S:1095-1100) describes W1 and W2, whose `(result, error)` dispositions do not change under (a)/(b);
the carrier check widens the condition of the existing terminal-guard row (S:1033, B4 of the archived table) rather
than adding a row (§ 7).

**Requirement text replaced** (S:1617-1623, the sentences from "A terminal whose record update loses" to "so the loop
settles on that answer."):

> A terminal whose record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves
> a durable terminal and a published event over a live record; that loss needs a writer in another process, because
> every record writer of one process serializes under one lock and refreshes the revision it observed. The record
> converges at the loop's next terminal commit in whichever process holds it: a terminal of the same kind adopts the
> durable terminal, republishes it and writes the record from it; a terminal of a different kind is refused and
> quarantined, the first terminal wins. Until then the loop runs on under a durable terminal, bounded only by its own
> remaining iteration budget and `timeout_at`. An approval-timeout sweep terminal (its `max_iterations` auto-reject,
> or the loop's own timeout) that commits `COMPLETE_<loopID>` and then fails to publish leaves a durable failed
> terminal under a record that stays `awaiting_approval`: a timer is never redelivered, and the record converges on the
> next answer to that gate or on a cancel. A reject, and any answer to a loop past its own deadline, dispatches nothing:
> the rebuilt loop re-derives the failure and adopts the durable terminal. An approve of a loop at its iteration cap
> dispatches the approved call once, and the durable terminal is adopted when that call's result completes the batch.
> A non-terminal result that reaches the carrier after its loop went terminal in memory, or after the loop was
> released, SHALL publish nothing and write nothing: the loop's record decides the delivery exactly as it decides a
> terminal-guard result — a terminal or absent record is acknowledged without effect, a live one is retried, and the
> redelivery is classified by its lane against the record. A terminal that lands between that check and the carrier's
> publication lets that one publication out; its record write then renders the loop's terminal state and the terminal
> owner's own write follows with the same content, and the published call's result is acknowledged without effect on
> the terminal loop.

Under OQ2 (b) the sentence "A reject, and any answer … completes the batch." becomes: "The next answer to that gate
adopts the durable failed terminal before any rebuild — nothing is dispatched, the saved event is republished, the
record is written terminal, and the answer is acknowledged as inapplicable."

**Scenarios added** (verbatim as in the delta):

#### Scenario: A terminal whose record write was lost converges at the loop's next terminal of the same kind

- **GIVEN** a loop held by process A whose terminal committed `COMPLETE_<loopID>` and published its event, and whose
  record write lost its compare-and-swap because process B had rebuilt the loop from a redelivered input and advanced
  the record to a later request
- **WHEN** process B's loop reaches its own terminal of the same kind
- **THEN** the terminal owner in B adopts the durable terminal by loop ID and kind, republishes the saved event, writes
  the record terminal under compare-and-swap and counts the terminal once; the input that produced A's terminal, when
  redelivered, is acknowledged as older; and between the lost write and B's terminal the loop ran ordinary work in B
  under a durable terminal, bounded by its remaining iteration budget and `timeout_at`

#### Scenario: A terminal of a different kind meeting a durable terminal is refused

- **GIVEN** the same loop, with `COMPLETE_<loopID>` holding a completion
- **WHEN** process B's loop fails instead
- **THEN** the failure is refused rather than adopted — the marker is not overwritten, no event is published, the record
  is not written — and the delivery is quarantined: the first terminal wins

#### Scenario: A sweeper terminal whose publication failed is adopted on the next answer to its gate

- **GIVEN** a loop gated on a human approval, at its iteration cap or past its own deadline, whose approval-timeout sweep
  committed `COMPLETE_<loopID>` and then failed to publish, so the record stays `awaiting_approval` and no process
  holds the loop
- **WHEN** a reject, or any answer to the loop past its own deadline, is delivered to a process with no memory of the
  loop
- **THEN** the loop is rebuilt from its record, nothing is dispatched, the rebuilt loop re-derives the failure and the
  terminal owner adopts the durable terminal, republishes it and writes the record terminal with the gate cleared, and
  the answer is acknowledged
- **WHEN** an approve is delivered instead, for a loop at its iteration cap
- **THEN** the approved call is dispatched once; when its result completes the batch the loop re-derives the
  `max_iterations` failure, the terminal owner adopts the durable terminal, and the record is written terminal

#### Scenario: A cancel that lands while a non-terminal result is on its way to the carrier publishes and writes nothing

- **GIVEN** a loop held by this process, gated on a human approval, whose approve was resolved and whose call was
  registered as pending but not yet published
- **WHEN** a cancel signal cancels the loop in memory before the approval's result reaches the carrier, and the cancel
  lane's terminal commit is still in flight
- **THEN** the carrier publishes no `tool.execute` and writes no record; the approval delivery is retried; once the
  cancel lane has written the cancelled record and released the loop, the redelivered answer reads that record, is
  acknowledged as inapplicable and counted, and the record has exactly one terminal writer

#### Scenario: A loop released while a non-terminal result is on its way to the carrier is settled by its record

- **GIVEN** the same gated loop with its approve mid-dispatch
- **WHEN** the cancel lane commits its terminal, writes the cancelled record and releases the loop before the approval's
  result reaches the carrier
- **THEN** the carrier finds no held loop, reads the record, and acknowledges the approval without effect — no
  `tool.execute` is published, the record's revision is unchanged, the delivery is neither quarantined nor terminated,
  loop health stays healthy and the approval lane keeps consuming — and the next valid answer on that lane dispatches
  its call

#### Scenario: A cancel that lands after the carrier's check lets one publication out

- **GIVEN** a held loop whose non-terminal result passed the carrier's check
- **WHEN** a cancel cancels the loop in memory after that check and before the carrier's record write
- **THEN** the publication that was in flight is retained on the stream, the carrier's write renders the loop cancelled
  and the terminal owner's write follows with the same content once `COMPLETE_<loopID>` and the cancellation event have
  landed; the published call's result, when it arrives, is acknowledged without effect on the terminal loop

Under OQ2 (b) the third scenario's THEN clauses become: "the cold branch reads `COMPLETE_<loopID>` under the gated
record before rebuilding, adopts the failed terminal through the terminal owner — the saved event is republished, the
record is written terminal with the gate cleared, the terminal is counted once with the saved reason — and the answer
is acknowledged as inapplicable and counted; nothing is dispatched for an approve or a reject alike."

## 3. Premises (measured) and change points

| # | Premise | Measurement |
|---|---|---|
| P1 | The probed W3 result is **not** `cancelled`-shaped; the carrier's `terminal` predicate (C:2219) is not where W3 is decided. | `ResolveApproval` restores `StateBeforeApproval` (AST:281-289: `e.State = restore`); the approval lane reads the entity at ARH:83 and sets `State: entity.State` at ARH:90 before `dispatchApprovedCall` (ARH:107); the probe pauses inside `dispatchToolCall` (PROBE:393-395) and the cancel lands after. The record's `cancelled` at rev 4 came from `marshalLoopRecord`'s `GetLoop` at write time (C:3167-3168), not from the result. Widening C:2219 to Cancelled flips nothing; the inventory § 1 sentence "a Cancelled-state HandlerResult (the shape dispatchApprovedCall … produce …)" describes the ARH:59-85 producer (Retry today, A8/D3-cancelled of the archived table), not the probed window. Correction recorded in § 7. |
| P2 | A lost compare-and-swap on a **held** loop's record needs a writer in another process. | Every in-process record write holds `loopRecordMu` and either refreshes the held revision or writes a loop this process does not hold: `createLoopState` C:2915/2923/2930; `persistLoopState` C:3047/3071/3081; `persistDeferredContinuationMarker` C:3112/3150/3161; step 0's adopt LE:426/528 (cold, `record.revision`); `writeRecordCancelled` TO:444/468 (cold). Rationale in the lock's own comment, C:3031-3036. `git grep -n "loopsBucket.Update(\|loopsBucket.Put(\|loopsBucket.Create(" -- 'processor/agentic-loop/*.go' ':!*_test.go'` → 6 sites, all listed. |
| P3 | Adoption is by kind, and kind is reason-agnostic. | TO:42-52 (`kind()` returns success/failed/cancelled); the identity check at TO:296 compares `saved.kind() != candidate.kind()`. |
| P4 | The sweeper never re-finds a loop whose commit failed. | `sweepExpiredApprovals` snapshots memory only (AS:81; ST:729-747); `commitTerminal` releases the loop on every failure but a lost CAS (TO:151-163); `releaseLoopTransientState` deletes it from the manager (TW:63-72). Inventory § 4; ALD:219-241 observes it. |
| P5 | The executor cannot stop a published call for a terminal loop; its result is dropped loop-side. | `handleToolCall` reads no loop state before executing (inventory § 6, `processor/agentic-tools/component.go:703-803`; `git grep` for `AGENT_LOOPS\|loopsBucket\|IsTerminal` in `processor/agentic-tools/*.go` non-test → only the `read_loop_result` tool's bucket binding, no dispatch-time read). Warm: `classifyRedeliveredToolResult` acks a terminal loop's result under `terminal_unproven` (LC:284-297); cold: `readLoopRecord` reports a terminal record stale (LE:282-310) and the tool cold arm acks it. |
| P6 | A non-terminal result at the carrier with no held loop is always "released mid-delivery". | Non-terminal producers all hold the loop when they return: the task lane writes birth by create-once outside the carrier (C:1674-1716, table B1) and the marker path (B3); every cold arm seats the loop before applying (ARH:403, `restoreLoopFromEvidence`); the sweeper's candidates come from memory (AS:81). Today that case fails at C:3168 ("get loop … for persistence") → Fatal → Quarantine: the probed W4 (PROBE:517-520). The one unit test that constructs it, PPF:31-59 (`loop-publish-phase` is never created in the manager), is named in tasks. |
| P7 | `settleTerminalGuard` already settles "memory says terminal, the record decides" for a result the handler did not act on, from any lane. | TO:481-503; callers C:1933-1940, C:2557-2560, ARH:262-271, and the carrier's own backstop C:2224-2230. Stale → Ack + the lane's drop; live or unreadable → transient (`WrapTransient`) → Retry on every lane (ARH:276-278; heartbeat lanes by class; sweeper logs). |
| P8 | The approval lane's Retry is a NAK, and the redelivery of a cancelled loop's answer takes the cold branch and is acknowledged inapplicable. | DS:432/463 (`DeliveryDecisionRetry` → `msg.Nak()`), the fake counts it (`loopSettlementMsg.Nak`, delivery_owner_test.go:628); `ResolveApprovalIfPending` on a released loop → `ErrLoopNotFound` (ST:777-786) → ARH:194-204 → `settleApprovalResponseWithoutLoop` → terminal record → `loopPresenceStale` → `recordApprovalInapplicable` (ARH:368-371). |
| P9 | There is no legitimate log seam after `AddPendingTool` on the approval lane. | `awk 'NR>=2035 && NR<=2165 && /logger\./' processor/agentic-loop/handlers.go` → 0 lines; the probe's Warn is H:632 inside `resolveRunEntityID`, a misconfiguration branch. |
| P10 | PR #1387 (durable accepted input) and this design touch different lines and one shared requirement. | `gh pr diff 1387 --name-only` → `openspec/changes/agentic-loop-durable-accepted-input/{inventory,proposal}.md` only at its head (design phase). Its stated target is `agentic.LoopEntity` fields (task prompt, deferred turn, intake record) and the S:1587 requirement's "Neither the turn nor the loop's task prompt is carried by the record" sentences (S:1649-1655) and their scenarios. This design reads only `State` and writes no new field; the carrier check and the (b) adoption render whatever fields the record carries. **Interaction:** both changes carry a MODIFIED block on the same requirement; ruling 5 orders #1387 first, so this change's restated text is re-based on the synced spec after #1387 archives, before its own archive (tasks 0.2). |
| P11 | W3/W4 are not approval-specific. | The tool lane's dispatch (`HandleToolResult` → `dispatchToolCall`) and the model lane's dispatch reach the same carrier with the same non-terminal shape; their handler-entry guards (H:2664, H:1433) run before the handler moves the loop, and a cancel can land between the handler's return and the carrier on either. The carrier is therefore the one home for the check (contract: one home per interpreted fact). |

### 3.1 Change point — the carrier reads the loop it publishes for (W3, W4)

`persistHandlerResult` (C:2218). After the `terminalOwnedElsewhere` backstop (C:2224-2230) and before
`recordHandlerResultTrajectory` (C:2232), for a non-terminal result:

```go
	if !terminal {
		// A non-terminal result whose loop went terminal in memory, or is no
		// longer held, meets a terminal commit in flight on another lane — the
		// cancel lane, or a lost compare-and-swap that released it (#1377 W3,
		// W4). This delivery owns no terminal and must publish and write
		// nothing: rendering the record from that entity would commit the
		// cancel outside its owner, and the call it would publish is work
		// after a cancel. The record decides, as it does for a guard result.
		if held, err := c.handler.GetLoop(result.LoopID); err != nil || held.State.IsTerminal() {
			return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)
		}
	}
```

Nine lines. `terminal` is the predicate already computed at C:2219; `settleTerminalGuard` is TO:481; the recorder is
TO:508. Nothing else moves: the terminal branch (C:2234-2258), the gated write-then-publish tail and
`publishThenPersistResultState` (C:2306) are unchanged. The `terminal` predicate itself is deliberately NOT widened to
Cancelled (P1): the only producer of a `State: cancelled` result with no error and a publication would be a handler
that observed the cancel — none does (ARH:90 reads the restored state) — and the check above covers the loop-in-memory
fact the record write actually reads. Under OQ3's alternative the same two-line condition is repeated inside
`publishThenPersistResultState` after `publishResults`, returning the same guard settlement without writing.

### 3.2 Change point — the test-only hook (OQ5)

`MessageHandler` gains `testApprovedDispatchHook func(loopID string)` (unexported; doc comment names #1377 and the
precedent C:153-160). `dispatchApprovedCall` (ARH:128-142) calls it after `dispatchToolCall` returns nil, before
`return nil`. Production never sets it.

### 3.3 Change point — OQ2 (b) only: the cold branch adopts a failed marker

`settleApprovalResponseWithoutLoop` (ARH:360), between the gate identity check (ARH:375-380) and the I4 check
(ARH:382): read `COMPLETE_<loopID>` (`c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))`, the TO:389 shape; not-found
→ continue; a read error → `WrapTransient`; a decode error or a marker naming another loop → `WrapFatal`, as TO:398-408).
A marker with `saved.failed != nil` → `seatRecordToFail(record.entity)`, `rememberLoopRevision`, start the trajectory
(the three lines of ARH:423-430), then `handleLoopFailure(ctx, loopID, saved.failed.Reason, errors.New(saved.failed.Error))`;
on nil, `recordApprovalInapplicable(response)` and `return false, nil`. The adoption itself is `createTerminalMarker`'s
existing conflict path (TO:276-313) reached through `commitTerminal` (C:2102). The `cancelled`/`completed` kinds fall
through to the rebuild. Not touched: `adoptDurableCancel` (a cancel marker stays the cancel lane's).

### 3.4 Not changed, and why

- `AddPendingTool` (ST:1114) keeps admitting a held terminal loop: refusing there closes only the cancel-before-`A4`
  order, not the probed one, and the drain at C:3336 already answers a pending call on a cancelling loop.
- `persistLoopState` (C:3025) keeps rendering from memory: it is the owner's step 4 too (TO:198) and cannot know its
  caller's shape without a signature change across three callers.
- The `Latch` (`internal/deliverylane/deliverylane.go:70`) gains no `Unlatch`: the fix removes the benign cause of the
  latch; a latch on a real invariant failure stays a restart matter (S:984-991).
- `adoptDurableCancel` (TO:385) is not generalized: under (a) nothing reads the marker anywhere new; under (b) the
  approval cold branch adopts through `handleLoopFailure`, not through a shared adopter (§ 6 row 5).

## 4. Counterexamples, controls, mutations

All at the component seam through the production callbacks `setupSubscriptions` wires (the probe's `startProbeLane`,
PROBE:181-219: `NewComponent`, `initializeKVBuckets`, real JetStream), `-race -count=20` as the probe ran. Harness
names are the ones on this tree.

| # | Counterexample (epic wording) | Harness | Asserts on the fixed tree | Control | Mutation that must turn it red |
|---|---|---|---|---|---|
| T1 | **Terminal commitment + concurrent request advancement (W1)** | two `Component`s over one bucket — the `predecessor`/replacement pattern (TRI:170-200) with `newLoopNATS`, `retainModelResponse`, `deliverToolResult`, `loopRecordOf`, `messagesOn`: A holds the loop at `R(N)`; B rebuilds it cold from a redelivered result and advances the record to `R(N+1)`; A's completion response for `R(N)` is delivered to A | A: `COMPLETE_` created, one `agent.complete` retained, A's record write lost (`ErrKVRevisionMismatch` → Retry, loop released); record live at `R(N+1)`. Then B completes (same kind): marker unchanged (`content_differs` logged), a second `agent.complete` retained (the saved one), record terminal, `loops_completed` counted once in B. The redelivery of A's response to B is acknowledged as older. | B's completion on a loop with **no** marker: normal commit (the existing terminal-owner tests, `terminal_owner_test.go:72-410`). | none — an (a) row; the test asserts the documented bound. Second arm: B fails instead of completing → `createTerminalMarker` refuses (TO:296), Quarantine, marker still the completion. |
| T2 | **Publication failure after commitment (W2)** | `newColdApproval` (ARO:67) + `unpublishableClient` (loop_carrier_test.go:28) + `sweepExpiredApprovals`, exactly ALD:219-241, plus a second shape: `e.Iterations` at the cap with `TimeoutAt` zero so the sweep's reject re-derives `max_iterations` | (a): timeout arm = ALD:219-241 as it stands; cap arm: an approve rebuilds and dispatches ONE `tool.execute` (counted via `testPublishHook`/`approvedToolCallsOn`); its result delivered → `loops_failed_total{max_iterations}` +1, record `failed`, marker unchanged, the saved event republished. (b): the approve dispatches nothing; `a.bucket.written()` gains the record, `state=failed` with the saved reason; `approval_inapplicable` +1; Ack. | the same approve with no marker rebuilds and dispatches (ARO:316, `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt`). | (a): none. (b): delete the marker read in the cold branch → the cap arm dispatches a call (red on "no tool.execute"). |
| T3 | **W3 flipped** | PROBE:373-461 with the pause moved from the Warn to `testApprovedDispatchHook`; `raceProbeBucket` (PROBE:84-151) keeps the cancel lane's marker pause | at approval return: acks=0, naks=1, terms=0; approved `tool.execute` unchanged; `bucket.recorded()` empty (no carrier write); no `COMPLETE_` yet. After the cancel lane releases: one write, `cancel-lane`, `cancelled`; `loops_failed_total{cancelled}` +1, `active_loops` −1. Redeliver the same approval: acks=1, `tool_results_dropped_total{approval_inapplicable}` +1, nothing published. | the same approval with no cancel dispatches and acks (existing warm approval tests; the probe's `gatedRunLoop` + a plain approve). | delete the § 3.1 block → red on "approved tool.execute unchanged" and on "no carrier write" (the probe's original observations return). `cp` backup + `md5 -q` per the testing policy. |
| T4 | **W4 flipped** | PROBE:467-542 with the same hook | acks=1, naks=0, terms=0, drains=0; `c.Health().Status` not `delivery ownership lost`; approved `tool.execute` unchanged; `afterApproval.revision == cancelledRecord.revision`; `tool_results_dropped_total{terminal_unproven}` +1; the second loop's approval: acks=1, its `tool.execute` +1, record `executing` with the gate cleared. | the second-loop approval IS the control (the lane keeps working). | delete the § 3.1 block → red on acks (0) and on health (`delivery ownership lost`). |
| T5 | **Process replacement** | T1's two-component harness is replacement at the component seam; the e2e `verifyApprovalAcrossReplacement` stage (`test/e2e/scenarios/agentic/approval_restart.go:188`) walks the cold approval branch across a real kill/start and is the tier's successful control. | unchanged stage; `task e2e:agentic` green on the final diff (proposal § Impact: the BREAKING gate walks the approval path; #1238's stages are on main). | the stage's approve/reject both settle on the replacement. | n/a (no change on that path). |
| T6 | **Successful controls** | the 14 order tests the archived design § 5 lists as unchanged; `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt` (ARO:316); `TestApprovalLanePublishesBeforeItWrites`; `TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind` (loop_carrier_test.go:71, its loop is held: `carrierLoop` creates it, :40) | all green: a held, non-terminal loop passes the check and takes the same order as today | — | — |

**Tests the check changes.** `TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified` (PPF:31-59) drives a
non-terminal result for `loop-publish-phase`, never created in the manager, with `loopsBucket` nil: today Fatal on the
publish; with the check, `GetLoop` fails → `readLoopRecord` → nil bucket → `loopPresenceUnknown` → transient. The test
keeps its purpose by creating the loop in the handler first (`CreateLoopWithID`, as `trajectory_eviction_internal_test.go:39`
does). `partial_publish_settlement_integration_test.go:28` seeds its loop (archived task 2.3) — verify, not change.
`trajectory_eviction_internal_test.go` drives only terminal states through the carrier (`grep -n "State:"` → Complete
at :30/:196, failed/cancelled arms create the loop) — unchanged. The developer runs the 28 `persistHandlerResult` test
call sites (`git grep -c`) and names any other that drives a non-terminal result on an unheld loop.

**The probe.** PROBE is retired by T3/T4: its two tests assert today's behaviour and go red on the fix by design; the
harness types (`raceProbeBucket`, `probeLane`, `gatedRunLoop`, metric snapshots) move into the landed test file;
`pauseOnLog` (PROBE:48-71) is deleted with the Warn seam.

**Invariants and the PBT decision.** I1: after the carrier's check, a non-terminal delivery publishes and writes only
for a loop this process holds non-terminal (delta sentence; T3/T4). I2: `COMPLETE_<loopID>` is written once and never
overwritten (S:1609-1610, create-once; T1). I3: a terminal record has one writer per terminal (TO:225-230; T3's
`bucket.recorded()`). I4: a same-kind later terminal converges the record; a different kind never replaces the marker
(S:1612-1614; T1's two arms). The inputs are two lanes' interleavings at three named points — a finite set of
orderings each test forces explicitly — not a grammar or a history, so named counterexamples with forced interleaving
are stronger than a sampled property (`docs/contributing/01-testing.md` § When to Use Property-Based Testing); no
Rapid property. Mutation evidence: T3/T4 on the one fix site; T2 on the (b) site if taken.

## 5. Migration text (replaces MIG:2015-2025, "Two residuals, recorded and not reconciled")

> **Two residuals, now bounded** (#1362 issuecomment-5808903072 and issuecomment-5809906669; #1377):
>
> - A terminal whose record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves
>   a durable terminal and a published event over a live record. That loss needs a second process writing the same
>   loop's record; within one process every record writer serializes and refreshes the revision it observed. The
>   record converges at the loop's next terminal commit in whichever process holds it: a terminal of the same kind
>   adopts the durable terminal, republishes the saved event and writes the record terminal; a terminal of a different
>   kind is refused and quarantined — the first terminal wins. Until then the loop runs on under a durable terminal,
>   bounded only by its own iteration budget and `timeout_at`. A watcher keyed on `COMPLETE_<loopID>` counts it as
>   finished while it runs; one keyed on the record's terminal `state` sees it at that next terminal.
> - An approval-timeout sweep terminal (its `max_iterations` auto-reject, or the loop's own timeout) that commits
>   `COMPLETE_<loopID>` and then fails to publish leaves a durable failed terminal under a record that stays
>   `awaiting_approval`; a timer is never redelivered. The record converges on the next answer to that gate or on a
>   cancel: a reject, and any answer to a loop past its own deadline, dispatches nothing and adopts the durable
>   terminal; an approve of a loop at its iteration cap dispatches the approved call once, and the terminal is adopted
>   when that call's result completes the batch. [(b): The next answer to that gate adopts the durable failed terminal
>   before any rebuild and is acknowledged as inapplicable; nothing is dispatched.]
>
> **A cancel racing a result on its way to the record.** A non-terminal result — an approved call, a model response's
> tool batch, a tool result's next request — that reaches the loop-record carrier after a cancel moved the loop
> terminal in memory, or after the loop was released, now publishes nothing and writes nothing: the record decides it,
> and the redelivered input is acknowledged as inapplicable once the cancel's record has landed. Before this, the
> carrier published the call for the cancelled loop and wrote a cancelled record outside the terminal owner, and a
> cancel that released the loop mid-dispatch quarantined the approval delivery and latched the approval lane until
> restart. A cancel that lands between the carrier's check and its publication still lets that one publication out;
> the executed call's result is acknowledged without effect on the terminal loop. **Action:** none for a consumer of
> `agent.complete` / `AGENT_LOOPS`. A consumer that counted `tool_results_dropped_total{reason="terminal_unproven"}`
> sees an approval answer settled at the carrier counted there.

## 6. Costs and rejected simpler alternatives (docket order)

| # | Alternative | Cost / why not |
|---|---|---|
| 1 | **All four windows documented, no code.** | W1 and W2: taken (OQ1, OQ2). W3: unavailable — the coordinating read rules it a defect, not an edge case; the Codex caution and the epic's first clause forbid a quiet pass. W4: the sentence is an availability bound the same nine lines remove. |
| 2 | **The carrier reads the loop once at entry (§ 3.1) — recommended.** | Nine lines at the one seam every lane's non-terminal result passes; adopts `settleTerminalGuard`; flips both probes; leaves the publish sub-window as a stated bound. |
| 3 | Widen `persistHandlerResult`'s `terminal` predicate to Cancelled (the brief's candidate (i)). | Does not flip the probe: the result is not cancelled-shaped (P1). Would only re-route the ARH:59-85 producer, which publishes nothing and already Retries. |
| 4 | Refuse `AddPendingTool` on a terminal loop (candidate (ii)). | Closes only a cancel that precedes `A4`; the probed order (cancel after `A4`) still publishes. A second site for half the window. |
| 5 | Re-check under the `LoopManager` lock at the publish (candidate (iii)). | The publish is I/O; nothing holds the manager's mutex across it, so the check narrows the window to the same width as row 2 while sitting in the shared dispatch path. |
| 6 | Entry check plus a post-publish check (OQ3 alternative). | Closes the second-writer half of the sub-window at the cost of a second site; the published call is out either way. Owner's call; recommended against. |
| 7 | Generalize `adoptDurableCancel` to every kind and every cold arm. | A shared adopter over a #1362-ruled cancel path for W2 alone; under (b) the cold branch reaches the same adoption through `handleLoopFailure` with no new helper beyond a marker read. |
| 8 | A startup or per-tick scan of `AGENT_LOOPS` for gated records with a marker (the sweeper "finding" W2 itself). | New read path over the bucket, deferred by the owner as OQ2 of #1330 (AS:49-54, issuecomment-5812283590); (c)-class. |
| 9 | Warm lanes read `COMPLETE_` before advancing (W1 reconciliation). | Ruled to need its own design (#1362 issuecomment-5808903072 ruling 2); a KV read on every delivery for an active/active window. |
| 10 | Keep the probe's Warn seam for landed tests. | Depends on a misconfigured component (P9); the hook is one field (OQ5). |
| 11 | A blocking `json.Marshaler` planted in the gated call's arguments as a no-hook pause. | Works on this tree (the warm dispatch marshals the in-memory arguments, H:2127) but is opaque and breaks on any argument normalization. |

## 7. Residuals (doc comments, not issues)

- **Inventory correction.** Inventory § 1's "a Cancelled-state `HandlerResult` (the shape `dispatchApprovedCall`/
  `checkApprovalGate` produce when they observe the loop cancelled in memory mid-dispatch) takes the non-terminal
  branch" conflates two producers: `dispatchApprovedCall` never reads state (ARH:128-142); the probed record content
  comes from `marshalLoopRecord` (P1). The pins are correct; the sentence is not. Recorded here, inventory untouched
  (pins are pre-change evidence).
- **Metric label at the carrier's Ack branch** (OQ3): an approval answer settled by the carrier counts under
  `terminal_unproven`, not `approval_inapplicable` (M:167 help text covers both readings). Doc comment on
  `recordTerminalToolResultDropped` (TO:506-511) and the migration sentence in § 5.
- **The publish sub-window** (OQ3): one publication may leave for a loop cancelled a moment later; the carrier writes
  `cancelled` once before the owner does. Stated in the delta; comment at the check.
- **The pre-`AddPendingTool` release** (OQ4): unforced; Retry → cold → inapplicable Ack from code. Comment at ARH:139.
- **W1's other-process precondition** (P2) and the different-kind quarantine remain the "first terminal wins" rule's
  cost; an unbounded-budget loop (`timeout_at` zero) has no time bound. Delta text.
- **A cancel marker under a gated record** is adopted by the cancel signal's own redelivery (TO:385), not by the
  approval lane; if an answer rebuilds the loop first, the redelivered cancel cancels the held loop and adopts. Not this
  change's window; comment under (b) if taken.
- **S:1095 "owed to #1377"**: after this lands the scenario's "owed" framing is historical; its claim "adding no row and
  changing no disposition" holds for W1/W2 (the pairs it names). Left as-is; the archive's spec sync carries the new
  scenarios in S:1587's requirement.
- **Approval-lane Retry policy latency on W3**: the redelivery's delay is the lane's `settleRetry` policy (C:1202);
  the answer is acknowledged one redelivery later. Bounded by the lane's `MaxDeliver`/backoff, as any Retry.

## 8. The #1146 per-window table, as it will be posted before the epic closes

| Window | Path taken | What is guaranteed | What is only bounded | Owner amendment of the first exit clause? |
|---|---|---|---|---|
| W1 — lost record CAS after marker and event | (a) documented bound | the durable terminal is never overwritten; a same-kind later terminal converges the record and republishes the saved event; a different kind is refused (first terminal wins); the redelivered terminal input is acknowledged as older | the loop resumes ordinary work in the other process until its own terminal — its iteration cap or `timeout_at` (no time bound if zero); needs a writer in another process | **YES** — requested |
| W2 — sweeper terminal committed, publication failed | (a) documented bound [(b) if taken: cold-branch adoption on the next answer] | the record converges on the next answer or cancel; reject and past-deadline answers dispatch nothing [(b): no answer dispatches anything] | parked `awaiting_approval` with no time bound; (a) admits one approved call after the durable terminal | (a) **YES** for that one call — requested [(b): no] |
| W3 — cancel lands while the loop is held, mid-dispatch | fix: the carrier reads the loop it publishes for | after a cancel commits in memory, a non-terminal result publishes and writes nothing; the redelivery is acknowledged inapplicable; one terminal-record writer | a cancel inside one publish latency after the carrier's check lets one publication out; its result is dropped on the terminal loop; the executor never stops a published call | no |
| W4 — cancel releases the loop mid-dispatch | the same check | the answer is acknowledged without effect from the cancelled record; nothing published, nothing written, no quarantine, no latch; the lane keeps consuming | the pre-`AddPendingTool` release: Retry → cold → inapplicable (from code) | no |

## 9. Problem shape, decision skills, adopter seam

**Shape (contract § 5):** *a non-owner meets a terminal in flight and must defer to the record* — the terminal-guard
shape. Closest existing instance, same plane: `settleTerminalGuard` (TO:481-503) with its four callers and the carrier's
backstop (C:2224-2230); on another plane, `authority_gate.go:38-59`'s structural-check-first refusal. **Adopted**: the
check routes to the existing guard settlement and adds no interpreter. No pattern is established (nothing new is named
for reuse), so no adoption sweep is owed. Decision skills: `kv-or-stream` — not triggered (no new communication path);
`orchestration-check` — not triggered (no multi-step behaviour added; (b) reuses the owner's four steps); `new-payload`
— none; `query-pattern` — none.

**Adopter seam.** The surfaces reached from outside are the wire and the KV record, not the Go pair (the archived
design's P7: zero sister callers of `HandlerResult`/`Handle*` at the pinned SHAs; not re-measured here — no exported
symbol changes). 1. *What must they know?* Under (a)/(a): the two bounds in § 5 — a durable terminal may precede the
record's terminal by the loop's remaining budget (W1) or until the next answer (W2). Under the fix: nothing new — an
approval answer or tool result racing a cancel is acknowledged rather than quarantined. 2. *If they do nothing?* Same
observations as today for W1/W2; for W3/W4 they stop seeing a `tool.execute` for a cancelled loop and a latched
approval lane. 3. *Where do they find out?* The runtime line `Delivery acknowledged without effect — the loop's record
is terminal` (TO:490) and the drop metric; the bounds only in docs (S, MIG) — a finding the (a) rows carry by
construction. 4. *What should they know?* Nothing; the gap is exactly the two amended clauses in § 8.
