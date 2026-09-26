# Design — agentic-loop-committed-terminal-recovery (#1377): the per-window docket

> Change id `agentic-loop-committed-terminal-recovery`, claiming #1377 (draft PR #1388). Base main `9e5d8455`; the
> inventory is pinned at `597072c4` (= `9e5d8455` + the proposal + the probe) and re-verified at `6876fe51`:
> `task inventory:verify` reports `pins=121 ok=121 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. Every
> `file:line` below is at `6876fe51`; production files are byte-identical to `9e5d8455` (inventory header).
> **Amended after independent design review** (PASS WITH AMENDMENTS, 2026-09-25, on `40bb373f`): the W3/W4 carrier
> check and the premise correction (P1) hold; the amendments are the three sub-window orderings (OQ3), the second
> MODIFIED block (OQ6), the cancel-over-a-timer-terminal composition (OQ7), the withdrawal of P2's other-process
> clause, and the Unproven list (§ 10).
>
> **Rulings applied, none reopened.** #1146 issuecomment-5828511934 ruling 4 (docket order (a) → (b) → (c); a second
> design round or any new durable authority returns to the owner with (a)); ruling 5 (implementation last in Lane A,
> after PR #1387); ruling 2 (obligations are rows of #1376's table, S:886 — its cost is called out in OQ6); #1377
> issuecomment-5828356994 (the two #1366 residuals are inputs, not retroactive defects; #1155 keeps its scope); the
> Codex caution (#1377 issuecomment-5838471641) and the coordinating read (issuecomment-5838780631): an (a) row is an
> accepted **limitation** and an explicit owner amendment of the epic's first exit clause, never a quiet pass — § 1 and
> § 8 flag every one, the W3 sub-window included; #1362 issuecomment-5808903072 ruling 1 (a benign cancel race must not
> latch the lane; its premise is questioned in OQ7) and ruling 2 (non-terminal lanes reading `COMPLETE_` "would need
> its own design"; the spawn-path source it names is kept); the 2026-09-22 standing rule (the documented alternative
> is the first entry of every row); #1372 (one round, or the smallest fix).
>
> **Bound.** No new durable fact, no new coordination, no new exported API, `agentic` (Tier 1) untouched. One
> production change is recommended (§ 3.1 + § 3.2, W3/W4), one optional (OQ2 (b), W2), test-only hooks (OQ5). The
> delta (§ 2) carries the recommended answers (OQ1 (a), OQ2 (a), OQ3 (ii), OQ4 the check, OQ6 two blocks, OQ7 (a));
> the alternative texts are named in § 2.
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
  event over a live record. The record converges at the loop's next terminal commit in whichever process holds it
  next, through a seam that already exists — `createTerminalMarker`'s refused `Create` reads the saved terminal back
  and adopts it by loop ID and kind (TO:262-313), and `kind()` is reason-agnostic (TO:42-52), so a `timeout` failure
  adopts a `max_iterations` marker and vice versa. A terminal of a different kind is refused (TO:296-298) and
  quarantined — the ruled "first terminal wins" (S:1612-1614). **Sources of the loss (P2, amended):** a second process
  writing the record; this process's own step-0 adopt writing a loop it still holds (LE:416-423, the tool lane's cold
  arm when the execution's routing entry was drained — a stated residual that releases the loop by design); a
  spawn-path birth failure under a producer-supplied loop ID (MIG:2019, #1362 ruling 2). **Bound:** the holding loop's
  remaining budget — its iteration cap, or `timeout_at`; no time bound when `timeout_at` is zero (ST:1550-1552) or
  while the loop is gated. **Guaranteed:** the durable terminal is never overwritten (create-once), a same-kind terminal
  converges the record and republishes the saved event, and a different kind never silently replaces it. **Not
  guaranteed:** the epic's first clause — the loop resumes ordinary work under a durable terminal until its own
  terminal. That is an amendment (§ 8).
- **(b)/(c) — any lane consulting the marker before advancing.** The only existing marker readers are the cancel
  lane's cold arm (TO:389) and the owner's step 1; a read on the warm model/tool/approval paths is the shape ruling 2
  of #1362 issuecomment-5808903072 deferred to "its own design", and it would add a KV read to every advancing
  delivery. Under ruling 4 that is (c): returned here with (a).

### OQ2 — W2, sweeper terminal whose publication failed: (a) documented bound, or (b) cold-branch adoption. Recommendation: **(a)**, with (b) specified so it needs no second round

- **(a) — the bound, no code (recommended).** The sweeper is memory-only (AS:41-47); a failed commit releases the loop
  (TO:151-163, H1 of #1362), so no later tick re-finds it (P4). The record converges on the **next answer to that
  gate** — and on nothing else: a cancel of that loop is retried to exhaustion (OQ7). The cold branch rebuilds the gated
  loop (ARH:360-410) and the answer runs warm. A reject, or any answer to a loop past its own deadline, dispatches
  nothing — `IsTimedOut` (ARH:101) or the batch's `max_iterations` re-derivation fails the loop and `commitTerminal`
  adopts the marker (`TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer`, ALD:219-241, proves the timeout
  case; the reject-at-cap arm is unproven, § 10). An **approve** of a loop at its iteration cap dispatches the approved
  call once (ARH:107 → H:2035); when its result completes the batch, `handleToolsComplete` re-derives `max_iterations`
  (H:3005-3037) and the terminal owner adopts the saved terminal. **Bound:** until the next answer (no time bound: the
  record is parked `awaiting_approval`); at most one approved call executes after the durable terminal. **Not
  guaranteed:** the first clause, for that one call. Amendment (§ 8).
- **(b) — the approval lane's cold branch adopts a durable failed terminal it finds under the gated record.** This is a
  **reinterpretation** of ruling 4's (b), not its literal shape: the sweeper itself cannot find a released loop (P4);
  the literal reading — the sweeper scanning `AGENT_LOOPS` for gated records with a marker — is § 6 row 8, a new read
  path the owner deferred as OQ2 of #1330, i.e. (c). The reinterpretation keeps the accepted trigger (the later answer,
  ruling 4) and the existing seam that meets the gated record. After ARH:375-380 establishes "live record awaiting this
  gate", read `COMPLETE_<loopID>` (the `Get` at TO:389); a marker that decodes to a **failed** terminal naming this
  loop is adopted through the path that already exists for a gated record that cannot continue: `seatRecordToFail` +
  `handleLoopFailure(ctx, loopID, saved.failed.Reason, errors.New(saved.failed.Error))` (the
  `failContinuationUnavailable` shape, ARH:421-431) → `commitTerminal` → `createTerminalMarker` adopts by kind,
  republishes the saved event, writes the record terminal and counts it once with the saved reason (TO:168-208,
  TO:225). The answer is acknowledged and counted inapplicable (ARH:291). A marker of another kind falls through to
  today's rebuild. ~20 lines at one seam, one 10-line read helper, two tests (§ 4). **Gained:** no call is dispatched
  on a loop with a durable failed terminal, on the accepted trigger. **Cost:** touches a #1362-reviewed cold path for a
  triple-conditioned window; the standing rule and #1372 both point at (a). If taken: § 2 names the one sentence and
  one scenario that change.

### OQ3 — W3 fix: (i) the carrier's entry check alone, (ii) entry check plus a render-time refusal in the carrier's own write, (iii) a post-publish read. Recommendation: **(ii)**, cost stated

The entry check (§ 3.1) guarantees "nothing published, nothing written" only for a cancel that lands **before** it. A
cancel inside the window between the check and the carrier's write has **three orderings** the first draft did not
separate; each is a defect the carrier itself creates, and the owner's marker `Create` (TO:274) can land before the
carrier's PubAck, so in every ordering a call may be published after the **durable** terminal commit:

- **(A)** the carrier's write lands before the owner's marker: the carrier renders the loop `cancelled` from memory
  (C:3167-3168) and writes it — a terminal record with no `COMPLETE_` and no event. If the process dies before the
  marker, the record is stranded: the redelivered cancel finds no held loop, reads a terminal record and is
  acknowledged `stale_loop_id` (C:3303-3309); the cancellation is never published.
- **(B)** the owner commits and releases before the carrier's write: `marshalLoopRecord` cannot read the loop
  (C:3168) → Fatal → Quarantine → the lane latches (C:1071) — the probed W4 text, one ordering later than the probe
  forced, so the first draft's "nothing latched" (OQ4) did not hold.
- **(C)** the owner writes, then the carrier's `stampPublishedRequest` and compare-and-swap succeed against the
  revision the owner refreshed (C:3081): a second terminal-record writer, from a different snapshot.

- **(i) — entry check only, the sub-window documented (cheapest).** Nine lines (§ 3.1). Flips T3/T4 as probed. The
  three orderings become stated bounds in the delta and an explicit amendment in § 8 (publication after the durable
  terminal; a cancelled record that may precede or outlive its marker; the latch in ordering B). Recommended against:
  it would document the carrier stranding a cancelled record with no marker and no event — a state no reader can
  settle and the one-terminal-owner invariant (TO:225-230) broken by the change meant to establish it — and leave W4's
  latch alive in one ordering.
- **(ii) — entry check + render-time refusal in the carrier's own write + not-found mapping (recommended).** The
  carrier's write already renders the loop under `loopRecordMu` (C:3047-3050): refuse there, on the rendered entity,
  to write a terminal snapshot for a non-terminal result, and treat "loop not found" at that render as the same
  refusal (§ 3.2). Closes A (nothing written before the marker; the record stays live and the cancel's own commit or
  redelivery lands it), B (no Fatal, no latch: the record decides), C (one terminal writer). **What remains:** a
  publication already out before the refusal — the executor runs it (P5) and its result is acknowledged without
  effect; because the owner's marker may land before that call's PubAck, this is still work published after the
  durable terminal, and § 8 flags it as the amendment, narrower than under (i). **Cost:** ~25 lines — a
  `terminalWriter` bool on the shared write, two carrier call sites mapping the refusal to the guard settlement, and
  `LoopManager. GetLoop` wrapping `ErrLoopNotFound` at ST:664 as `CancelLoop` (ST:1936-1937) and
  `ResolveApprovalIfPending` (ST:785-786) already do (an additive wrap of an unexported sentinel; the alternative is a
  private `(entity, held bool)` accessor beside `GetLoop`); and, in `publishThenPersistResultState`, a
  `stampPublishedRequest` error that is `ErrLoopNotFound` (`SetPublishedRequest`, ST:1265-1268, via C:2996-3007)
  mapped to `errTerminalOwnedElsewhere`: for a result that mints the next request — a model response, or a tool result
  completing its batch — the stamp at C:2315 runs BEFORE the render, so without the mapping a release in ordering B is
  wrapped Fatal (C:2315-2317), quarantined and latched before the refusal is reached; an approved `tool.execute` mints
  nothing, so T8's approval arm cannot see it and T8 gains a tool-lane arm. Consequence of the wrap, named: the
  approval lane's A8 producer (ARH:85, a loop released between the resolve and its re-read) reaches the cold branch on
  its **first** delivery (ARH:194) instead of after one Retry — same outcome one redelivery earlier; S:886's scenario
  "An approval answer whose loop was released after its gate resolved is recovered cold" is updated in the second
  MODIFIED block (OQ6). No other `errors.Is(…, ErrLoopNotFound)` site reads a `GetLoop` error (`git grep` → ARH:194,
  AS:105, C:3303, C:3311; the tool lane's H:2649 and the model lane's H:1316 do not test the sentinel, so rows A3 keep
  their dispositions).
- **(iii) — a post-publish `GetLoop` outside the lock (rejected).** Narrows the write-side window to the same shape as
  (i); the render under the lock is the only point that sees the state the write will carry.
- **Drop metric on the carrier's Ack branch (all lanes).** The guard settlement counts through its callback and the
  carrier does not know the lane, so it passes the existing `terminal_unproven` recorder (TO:508-511):
  `tool_results_dropped_total{reason="terminal_unproven"}` for an approval answer, a model response's batch, a tool
  result's next request and a sweeper auto-reject alike, whereas each lane's own handler-entry guard counts under its
  own family (the model lane: `model_responses_dropped_total{stale_request_id}`, C:1935-1939; the approval lane's cold branch:
  `approval_inapplicable`, ARH:370/378). Doc comments name it (§ 7); the Retry branch — the common case — leaves
  counting to the lane's cold branch.

### OQ4 — W4 classification: the doc sentence, or the carrier check. Recommendation: **the check, under OQ3 (ii)**

- **(a) — the sentence, no code:** "a cancel that releases a loop while an approval for it is mid-dispatch quarantines
  that delivery and latches the approval lane; a restart clears it." An availability defect stated as a bound.
- **The check (recommended).** With § 3.1 the released loop fails the carrier's entry read and the record — already
  `cancelled` (the probe measured it at rev 8) — settles it: acknowledged without effect, nothing published, nothing
  written, nothing latched. That covers a release **before** the check; a release between the carrier's publish and
  its write (ordering B) is closed only by the render-time refusal of OQ3 (ii) — under (i) it still quarantines and
  latches. This is #1362 issuecomment-5808903072 ruling 1's reasoning applied one lane over: a benign cancel race must
  not latch health.
- **The literal pre-`AddPendingTool` point** (a cancel that released the loop between ARH:83 and H:2036) is unforced;
  from code it is `AddPendingTool`'s "loop not found" (ST:1118-1120), wrapped at ARH:139, unclassified → Retry
  (ARH:240) → redelivery → cold branch → inapplicable Ack (ARH:368-380). Unproven (§ 10); the `before_dispatch` stage
  of OQ5 forces it if the owner wants it proved.

### OQ5 — the seams the landed tests use: one dispatch hook, or staged hooks on the approval lane and the carrier. Recommendation: **the staged hooks**

- **Cheaper row — one call site.** `testApprovedDispatchHook func(loopID string)` on `MessageHandler`, nil in
  production, called in `dispatchApprovedCall` after `dispatchToolCall` returns nil (ARH:138-141): after
  `AddPendingTool`, before the carrier — the probe's pause point. Forces T3/T4 only; T7–T9 (the three orderings) stay
  unforced and the sub-window scenario rests on code reading.
- **Staged hooks (recommended).** The same field with a stage argument, `testApprovedDispatchHook func(loopID, stage
  string)`, called at `before_dispatch` (after the `IsTimedOut` block, ARH:101-103, before the decision switch — the
  pre-`AddPendingTool` point) and `dispatched` (as above); plus `testCarrierHook func(loopID, stage string)` on
  `Component`, called at `checked` (after the entry check, before `recordHandlerResultTrajectory`, C:2232) and
  `published` (in `publishThenPersistResultState` after `publishResults`, before `stampPublishedRequest`, C:2307-2316).
  Precedent: `testPublishHook` (C:153-157, read at AS:215) and `testLineageWriteHook` (C:158-160). Cost: two unexported
  fields, four nil checks. `dispatchToolCall` has no log line after `AddPendingTool` (P9), so no legitimate log seam
  exists.
- **The probe's seam** (the `resolveRunEntityID` Warn, H:632): zero production change, but the pause exists only in a
  component with no platform identity, and any fix to that Warn silently unpins the test. Not recommended.

### OQ6 — one MODIFIED block or two. Recommendation: **two** (S:886 and S:1587) — and this is ruling 2's cost, called out

- **Cheaper row — one block, on S:886 only.** Ruling 2 made #1376's table (S:886, "Loop input classes settle after
  owner-specific durable done") the home of obligation rows; it already holds "A durable terminal with a non-terminal
  record is owed to #1377" (S:1095-1100). Discharging the row there alone (~215 lines restated) leaves S:1587's two
  "is not reconciled" sentences (S:1617-1623) contradicting the new row after archive. Rejected.
- **Two blocks (recommended).** S:886 restated with four scenario changes and one text sentence (§ 2), S:1587 restated
  with the residual sentences replaced and six scenarios added. One block cannot reach both: the residuals live in
  S:1587 and the row and the order scenarios ("The same result shape takes the same order on every lane", S:1022-1031
  — every non-terminal result "publishes every output first" — and the guard scenario S:1033-1038, which covers only
  effect-free results) live in S:886. **Ruling 2's cost, per the call-out rule:** every child that discharges a table
  row must restate S:886's 22 scenarios, and PR #1387 at `d2b6a20e` MODIFIES the same S:886 (and REMOVES "Task intake
  is the one loop input class this layer does not convert") and lands first (ruling 5), so this block is re-based on
  the synced spec before archive (task 0.2). The owner may prefer a lighter mechanism for later children; this change
  pays the cost as ruled. One more cost of the same ruling: the kept title "A durable terminal with a non-terminal
  record is owed to #1377" reads as history after archive (openspec refuses renames) — the same class as #1387's two
  headings under ruling 2.

### OQ7 — a cancel of a W2 loop retries to exhaustion: a ruling composition. Recommendation: **(a)** document, and ask

- **The composition.** #1362 issuecomment-5808903072 ruling 1: on the cold cancel arm a completion or failure marker
  over a live record is **Retried**, "because the loop's own terminal redelivery writes the record". For a **timer**
  terminal there is no redelivery: a W2 loop (failed marker, record `awaiting_approval`, held by no process) that is
  cancelled reaches `settleUncancellableLoop` → live record → `adoptDurableCancel` finds a failed marker and returns
  false (TO:397-399) → the cancel is returned as an error → Retry (C:3311-3323, C:3268-3273) until the signal
  consumer's `MaxDeliver`, then it is recorded in the framework's MaxDeliver ledger (`internal/maxdelivery`; C:2065-2068)
  — no dead-letter subject, exhaustion observed, not re-published. The loop stays parked until an answer; the cancel
  never lands. The first draft's "converges on the next answer or a cancel" was false and is withdrawn everywhere.
- **(a) — document (recommended):** the delta and § 5 say a cancel of such a loop is retried to exhaustion and observed
  there, and the record converges only on an answer. The owner is asked whether ruling 1's premise should be
  narrowed to redeliverable terminals.
- **(b) — the cold cancel arm adopts a failed marker under a gated record:** seat-and-fail with the saved reason (the
  OQ2 (b) mechanism reached from the cancel lane) and acknowledge the cancel as `already_terminal`. Requires the owner
  to revise ruling 1 for the gated-record case; ~15 lines if OQ2 (b) lands, ~30 otherwise. Not designed further here.

## 1. The per-window docket

Columns per ruling 4 and the coordinating read: window · path (a/b/c) · existing durable fact used · what is guaranteed ·
what is only bounded · smallest fix at which seam · amendment flag. The documented alternative is the first entry of
every row (2026-09-22 rule).

| Window | Path | Existing durable fact | Guaranteed | Only bounded | Smallest fix, seam | (a) = epic amendment? |
|---|---|---|---|---|---|---|
| **W1** lost record CAS after marker + event (C:2242-2250; S:1617-1619) | **(a)** documented. (b)/(c): warm lanes reading the marker → own design (#1362 ruling 2) → returned with (a) | `COMPLETE_<loopID>` (TO:84), adopted by the owner's step 1 (TO:262-313) | the marker is never overwritten; a same-kind later terminal adopts it, republishes the saved event, writes the record terminal (TO:171-208); a different kind is refused (TO:296) and quarantined, first terminal wins (S:1612-1614); the redelivered terminal input is acknowledged as older (S:1623-1624) | the loop resumes ordinary work in whichever process holds it next, until its own terminal: its remaining iteration cap or `timeout_at` (no time bound if zero, or while gated); sources: a second process, this process's step-0 adopt on a loop it still held (LE:416-423), a spawn-path birth failure under a producer-supplied loop ID (MIG:2019) | none | **YES** — the first clause is not met between the lost write and the loop's next terminal |
| **W2** sweeper terminal commits marker, publish fails (AS:152-163; S:1620-1623) | **(a)** documented, recommended. **(b)** the approval cold branch adopts a failed marker under the gated record (OQ2, a reinterpretation of ruling 4's (b)) | `COMPLETE_<loopID>` and the gated record (`awaiting_approval`, `pending_approval`) read at ARH:364-380 | (a): the record converges on the next answer to the gate; reject, and any answer past `timeout_at`, dispatch nothing (ARH:101; ALD:219-241); an approve at the cap dispatches once and the terminal is adopted when its result completes the batch (H:3005-3037 → TO:262). (b): nothing is dispatched; the answer adopts the terminal and is acknowledged inapplicable | until the next answer (no time bound: parked `awaiting_approval`); a cancel of the loop is retried to `MaxDeliver` exhaustion and observed in the MaxDeliver ledger, never applied (OQ7); (a) admits one approved call after the durable terminal | (a) none. (b) ~20 lines after ARH:380 + a marker-read helper, via `seatRecordToFail` + `handleLoopFailure` (ARH:421-431) | (a) **YES**, for the one approved call and for the cancel that cannot land. (b): the cancel half stays (OQ7) |
| **W3** cancel lands while the loop is held, after `AddPendingTool` (PROBE:373; ARH:107 → H:2036 → C:2261) | **fix** (no (a): the coordinating read rules "not supported" unavailable). OQ3: (i) entry check, (ii) entry check + render-time refusal (recommended) | the loop's in-memory terminal (the cancel lane's `CancelLoop`, ST:1925-1959) and its record (`readLoopRecord`, LE:282) | (ii): a non-terminal result reaching the carrier after the cancel committed in memory publishes nothing and writes nothing; one that passed the check writes nothing once the loop is terminal or released at its render; the delivery is retried or acknowledged by the record and its redelivery is settled by the lane's cold branch (ARH:368-380); exactly one terminal-record writer (TO:225-230). (i): the first sentence only | a cancel inside one publish latency after the carrier's check lets one publication out, and the owner's marker may land before that publication's PubAck: work published after the durable terminal; the executor never stops it (P5) and its result is acknowledged without effect (LC:284-297 warm, LE:282 cold). Under (i) additionally orderings A, B, C (OQ3) | (ii): one read of the held loop at the top of the non-terminal path (C:2232) + the refusal on the rendered entity in the carrier's write (C:3047-3050) + `ErrLoopNotFound` wrapped at ST:664 | **YES** — for the publication in the sub-window (narrow under (ii); under (i) also the record hazards). Requested explicitly |
| **W4** cancel completes and releases the loop while the approval is mid-dispatch (PROBE:467; C:3168 Fatal → ARH:280-281 → latch C:1071) | **(a)** the sentence ("latches the lane; restart clears it"), or **the check** (recommended, OQ4) | the record, already `cancelled` | with (ii): the delivery is acknowledged without effect from the record whether the release preceded the carrier's check or fell between its publish and its write; nothing published after the check, nothing written, health untouched, the lane keeps consuming — the next valid answer dispatches. With (i): only a release before the check | the pre-`AddPendingTool` variant is unforced (OQ4): Retry → cold → inapplicable Ack, from code; under (i) ordering B still latches | the same read at entry; under (ii) the same refusal at the render | not an (a) row under the recommendation; (a) would be a bound on availability |

## 2. The two MODIFIED requirements — what changes and the scenarios, verbatim

**Block 1 — `Loop input classes settle after owner-specific durable done` (S:886-1100, 22 scenarios restated).** One
sentence appended to the partial-effect paragraph (S:907-911): "A non-terminal result whose loop is terminal in
memory, or no longer held, when the carrier is entered, when it names the request it published, or when it renders the
loop's record is not this delivery's partial effect: the carrier publishes nothing further, writes nothing, and the
loop's record decides the delivery." Four scenarios change (titles unchanged — openspec refuses renames):

- *The same result shape takes the same order on every lane* — new AND: "a non-terminal result whose loop is terminal
  in memory or no longer held when the carrier is entered, when it names the request it published, or when it renders
  the loop's record, publishes nothing further and writes nothing on every lane: the record decides it".
- *A terminal-guard result is settled by the record, whichever lane produced it* — new AND: "the carrier settles a
  non-terminal result the same way when it finds the loop terminal in memory or no longer held — a case the handler's
  guard could not see because the loop moved after the handler returned — counting an acknowledged drop under the
  tool-result family's terminal reason on every lane, and retrying a live record".
- *An approval answer whose loop was released after its gate resolved is recovered cold* — THEN becomes: "the delivery
  takes the cold branch on that first delivery: it reads the still-gated record, rebuilds the loop and applies the
  answer exactly as the process that gated the loop would have; a released loop whose record is already terminal is
  acknowledged as inapplicable" (OQ3 (ii)'s `ErrLoopNotFound` wrap; under (i) the scenario is unchanged).
- *A durable terminal with a non-terminal record is owed to #1377* — the last AND becomes: "the recovery path is
  declared under `The loop record names its outstanding request`: a same-kind later terminal adopts the marker and
  converges the record, and a sweeper terminal is adopted on the next answer to its gate; a cancel of that gated loop
  is retried to exhaustion; neither adds a row nor changes a row's disposition here — the carrier's refusal widens the
  terminal-guard row's condition".

**Block 2 — `The loop record names its outstanding request` (S:1587-1861, 22 scenarios restated).** The residual
sentences (S:1617-1623) are replaced by:

> A terminal whose record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves
> a durable terminal and a published event over a live record — whether the record moved under a second process, under
> this process's own adoption of a newer retained request for a loop it still held, or under a spawn-path birth
> failure with a producer-supplied loop ID. The record converges at the loop's next terminal commit in whichever
> process holds it next: a terminal of the same kind adopts the durable terminal, republishes it and writes the record
> from it; a terminal of a different kind is refused and quarantined, the first terminal wins. Until then the loop
> runs on under a durable terminal, bounded only by its own remaining iteration budget and `timeout_at`, with no time
> bound while `timeout_at` is zero or the loop is gated. An approval-timeout sweep terminal (its `max_iterations`
> auto-reject, or the loop's own timeout) that commits `COMPLETE_<loopID>` and then fails to publish leaves a durable
> failed terminal under a record that stays `awaiting_approval`: a timer is never redelivered, and the record
> converges only on the next answer to that gate. A reject, and any answer to a loop past its own deadline, dispatches
> nothing: the rebuilt loop re-derives the failure and adopts the durable terminal. An approve of a loop at its
> iteration cap dispatches the approved call once, and the durable terminal is adopted when that call's result
> completes the batch. A cancel of that loop is retried until the signal consumer's redelivery budget is exhausted and
> is observed there, never applied, because the cold cancel arm adopts only a cancel marker. A non-terminal result
> that reaches the carrier after its loop went terminal in memory, or after the loop was released, SHALL publish
> nothing and write nothing, and a non-terminal result whose loop is terminal in memory or no longer held when the
> carrier names the request it published or renders its record SHALL NOT be written: the loop's record decides the
> delivery exactly as it decides a terminal-guard result — a terminal or absent record is acknowledged without effect,
> a live one is retried, and the redelivery is classified by its lane against the record. A terminal that lands
> between the carrier's check and its publication lets that one publication out, and the durable terminal may be
> created before that publication's PubAck; the published call's result is acknowledged without effect on the terminal
> loop.

Under OQ2 (b) the sentence "A reject, and any answer … completes the batch." becomes: "The next answer to that gate
adopts the durable failed terminal before any rebuild — nothing is dispatched, the saved event is republished, the
record is written terminal, and the answer is acknowledged as inapplicable." Under OQ3 (i) the "SHALL NOT be written"
clause is dropped and the last sentence reads: "…lets that one publication out; its record write then renders the
loop's terminal state before or after the terminal owner's own write, and a process that dies between the two may
leave a cancelled record with no marker and no event".

**Scenarios added to block 2** (verbatim in the delta): *A terminal whose record write was lost converges at the loop's
next terminal of the same kind* · *A terminal of a different kind meeting a durable terminal is refused* · *A sweeper
terminal whose publication failed is adopted on the next answer to its gate* (with a third WHEN/THEN for the cancel
that is retried to exhaustion) · *A cancel that lands while a non-terminal result is on its way to the carrier
publishes and writes nothing* · *A loop released while a non-terminal result is on its way to the carrier is settled
by its record* (both orderings: before the check, and between publish and write) · *A cancel that lands after the
carrier's check lets one publication out and its record write is refused* (orderings A and C by name). Under OQ2 (b)
the third scenario's first THEN reads as the (b) sentence above.

## 3. Premises (measured) and change points

| # | Premise | Measurement |
|---|---|---|
| P1 | The probed W3 result is **not** `cancelled`-shaped; the carrier's `terminal` predicate (C:2219) is not where W3 is decided. | `ResolveApproval` restores `StateBeforeApproval` (AST:281-289: `e.State = restore`); the approval lane reads the entity at ARH:83 and sets `State: entity.State` at ARH:90 before `dispatchApprovedCall` (ARH:107); the probe pauses inside `dispatchToolCall` (PROBE:393-395) and the cancel lands after. The record's `cancelled` at rev 4 came from `marshalLoopRecord`'s `GetLoop` at write time (C:3167-3168). Widening C:2219 to Cancelled flips nothing. Inventory § 1 corrected (its prose at :62-68 and :216-221; pins untouched). Confirmed by the design review. |
| P2 | **Withdrawn as stated** ("needs a writer in another process"). A lost compare-and-swap on a held loop's record has three sources, two of them in-process. | (1) A second process. (2) Step 0's adopt writes a loop this process still holds without refreshing its revision — a stated residual (LE:416-423: "When it happens to hold it anyway — the tool lane reaches the cold arm whenever the execution's routing entry has been drained — the warm lane's next compare-and-swap is refused and the loop is released"); callers ARH:364, C:2017, C:2644. (3) A spawn-path birth failure under a producer-supplied loop ID (MIG:2017-2019; #1362 ruling 2). The lock's rationale (C:3031-3036) covers the carrier and birth, not step 0. Neither in-process source is tested (§ 10). |
| P3 | Adoption is by kind, and kind is reason-agnostic. | TO:42-52 (`kind()` returns success/failed/cancelled); the identity check at TO:296 compares `saved.kind() != candidate.kind()`. |
| P4 | The sweeper never re-finds a loop whose commit failed. | `sweepExpiredApprovals` snapshots memory only (AS:81; ST:729-747); `commitTerminal` releases the loop on every failure but a lost CAS (TO:151-163); `releaseLoopTransientState` deletes it from the manager (TW:63-72). Inventory § 4; ALD:219-241 observes it. |
| P5 | The executor cannot stop a published call for a terminal loop; its result is dropped loop-side. | `handleToolCall` reads no loop state before executing (inventory § 6, `processor/agentic-tools/component.go:703-803`; `git grep` for `AGENT_LOOPS\|loopsBucket\|IsTerminal` in `processor/agentic-tools/*.go` non-test → only the `read_loop_result` tool's bucket binding). Warm: LC:284-297 acks a terminal loop's result under `terminal_unproven`; cold: LE:282-310 reports a terminal record stale and the tool cold arm acks it. |
| P6 | A non-terminal result at the carrier with no held loop is always "released mid-delivery". **Argued, not tested** (§ 10). | Non-terminal producers all hold the loop when they return: the task lane writes birth by create-once outside the carrier (C:1674-1716, table B1) and the marker path (B3); every cold arm seats the loop before applying (ARH:403); the sweeper's candidates come from memory (AS:81). Today that case fails at C:3168 → Fatal → Quarantine: the probed W4 (PROBE:517-520). The one unit test that constructs it, PPF:31-59 (`loop-publish-phase` is never created in the manager), is named in tasks; its changed assertion is the right contract (review). |
| P7 | `settleTerminalGuard` already settles "memory says terminal, the record decides" from any lane; its text assumes a held loop. | TO:481-503; callers C:1933-1940, C:2557-2560, ARH:262-271, and the carrier's own backstop C:2224-2230. Stale → Ack + the lane's drop; live or unreadable → transient → Retry on every lane (ARH:276-278; heartbeat lanes by class; sweeper logs). Its transient cause "is terminal in memory and its record is not" (TO:499-500) and its Warn's `state` (TO:494, the result's restored state) are wrong for a released loop: the new call sites pass their own cause (§ 3.1). |
| P8 | The approval lane's Retry is a NAK, and the redelivery of a cancelled loop's answer takes the cold branch and is acknowledged inapplicable. | DS:432/463 (`DeliveryDecisionRetry` → `msg.Nak()`), the fake counts it (delivery_owner_test.go:628); `ResolveApprovalIfPending` on a released loop → `ErrLoopNotFound` (ST:777-786) → ARH:194-204 → `settleApprovalResponseWithoutLoop` → terminal record → `loopPresenceStale` → `recordApprovalInapplicable` (ARH:368-371). |
| P9 | There is no legitimate log seam after `AddPendingTool` on the approval lane. | `awk 'NR>=2035 && NR<=2165 && /logger\./' processor/agentic-loop/handlers.go` → 0 lines; the probe's Warn is H:632 inside `resolveRunEntityID`, a misconfiguration branch. |
| P10 | PR #1387 collides with this design on **S:886**, not S:1587. | `gh pr diff 1387 --name-only` at `d2b6a20e` → its `specs/agentic-loop/spec.md` carries `## MODIFIED Requirements` → `### Requirement: Loop input classes settle after owner-specific durable done` and `## REMOVED Requirements` → `### Requirement: Task intake is the one loop input class this layer does not convert`. Ruling 5 orders #1387 first; this change's block 1 is re-based on the synced spec after #1387 archives (task 0.2). This design reads only `State` and writes no new field. |
| P11 | W3/W4 are not approval-specific — and "every lane" holds only with § 3.2's stamp mapping. | The tool lane's dispatch (`HandleToolResult` → `dispatchToolCall`) and the model lane's reach the same carrier with the same non-terminal shape; their handler-entry guards (H:2664, H:1433) run before the handler moves the loop, and a cancel can land between the handler's return and the carrier on either. The carrier is the one home for the check. Unlike an approved `tool.execute`, a model response or a batch-completing tool result mints the next request and stamps it at C:2315 before the render (`SetPublishedRequest`, ST:1265-1268, refuses a released loop with `ErrLoopNotFound`), so on those lanes ordering B reaches the stamp first: the render-time refusal alone would not stop the Fatal wrap (C:2315-2317). |
| P12 | `GetLoop` returns exactly two error classes and neither carries the sentinel. | ST:659-666: `WrapInvalid` for an empty ID; `errs.Wrap(fmt.Errorf("loop %s not found"))` otherwise. Hence the entry check scopes "not held" by the sentinel once ST:664 wraps it (OQ3 (ii)); an Invalid error is returned as it is, never acknowledged as stale. |

### 3.1 Change point — the carrier reads the loop it publishes for (W3, W4; OQ3 (i) and (ii))

`persistHandlerResult` (C:2218). After the `terminalOwnedElsewhere` backstop (C:2224-2230) and before
`recordHandlerResultTrajectory` (C:2232), for a non-terminal result:

```go
	if !terminal {
		// A non-terminal result whose loop went terminal in memory, or is no
		// longer held, meets a terminal commit in flight on another lane — the
		// cancel lane, or a lost compare-and-swap that released it (#1377 W3,
		// W4). This delivery owns no terminal and must publish and write
		// nothing: the record decides, as it does for a guard result.
		held, err := c.handler.GetLoop(result.LoopID)
		switch {
		case errors.Is(err, ErrLoopNotFound), err == nil && held.State.IsTerminal():
			return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)
		case err != nil:
			return err // an invalid loop ID is the caller's error, never a stale loop
		}
	}
```

`terminal` is the predicate already computed at C:2219 and is NOT widened (P1); `settleTerminalGuard` is TO:481; the
recorder is TO:508. The guard's Warn and transient cause are reworded at TO:494-501 to "terminal in memory or no
longer held; the record decides", logging the record's state beside the result's (P7). Under OQ3 (i) this is the whole
change; the OQ5 `checked` stage follows it.

### 3.2 Change point — the carrier's own write refuses a terminal snapshot (OQ3 (ii))

`persistLoopState` (C:3025) keeps its signature for the owner (TO:198) and delegates to
`writeLoopRecord(ctx, loopID, terminalWriter bool)` — the existing body — with `true`; the carrier's two sites,
C:2276 (gated, write-then-publish) and C:2325 (ordinary, after the publish), call it with `false`. Inside the
critical section (C:3047-3050), the render's `GetLoop` result is checked before marshalling:

```go
	held, err := c.handler.GetLoop(loopID)
	if !terminalWriter && (errors.Is(err, ErrLoopNotFound) || err == nil && held.State.IsTerminal()) {
		return errTerminalOwnedElsewhere // the carrier maps it to settleTerminalGuard
	}
```

`errTerminalOwnedElsewhere` is an unexported sentinel beside `errCancelledBeforeMutation` (H:2625). The two carrier
sites test it first and return `settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`; everything else
keeps its mapping (lost CAS → transient; other → Fatal). In `publishThenPersistResultState` the stamp's error is
tested the same way first: `errors.Is(err, ErrLoopNotFound)` from `SetPublishedRequest` (ST:1265-1268, reached through
`stampPublishedRequest`, C:2996-3007, at C:2315 before the render) maps to the same guard settlement; any other stamp
error keeps its Fatal wrap (C:2315-2317). The `GetLoop` wrap also reaches the sweeper: a loop released between the
sweeper's snapshot and `HandleApprovalResponse`'s re-read (ARH:85) now matches AS:105 and takes the "already released"
Warn (AS:108-110) instead of the "auto-reject failed" Error (AS:131-135) — benign, § 7. `LoopManager.GetLoop` (ST:664)
wraps `ErrLoopNotFound` (`fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound)`), as ST:785-786 and ST:1936-1937 do;
A8's consequence is in OQ3. The OQ5 `published` stage sits between `publishResults` and this write. Unchanged: the
owner's step 4 (`terminalWriter` true renders and writes the terminal as today); `persistDeferredContinuationMarker`
(C:3112, the task lane's marker — same shape, a different window; residual § 7).

### 3.3 Change point — the test-only hooks (OQ5)

`MessageHandler.testApprovedDispatchHook func(loopID, stage string)` called at `before_dispatch` (ARH:103, after the
`IsTimedOut` block) and `dispatched` (ARH:141, after `dispatchToolCall` returns nil);
`Component.testCarrierHook func(loopID, stage string)` called at `checked` (after § 3.1) and `published` (C:2311, after
`publishResults` in `publishThenPersistResultState`). Nil in production; doc comments cite C:153-160.

### 3.4 Change point — OQ2 (b) only: the cold branch adopts a failed marker

`settleApprovalResponseWithoutLoop` (ARH:360), between the gate identity check (ARH:375-380) and the I4 check
(ARH:382): read `COMPLETE_<loopID>` (`c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))`, the TO:389 shape; not-found
→ continue; a read error → `WrapTransient`; a decode error or a marker naming another loop → `WrapFatal`, as TO:398-408).
A marker with `saved.failed != nil` → `seatRecordToFail(record.entity)`, `rememberLoopRevision`, start the trajectory
(ARH:423-430), then `handleLoopFailure(ctx, loopID, saved.failed.Reason, errors.New(saved.failed.Error))`; on nil,
`recordApprovalInapplicable(response)` and `return false, nil`. The adoption itself is `createTerminalMarker`'s
existing conflict path (TO:276-313) reached through `commitTerminal` (C:2102). Other kinds fall through.

### 3.5 Not changed, and why

- `AddPendingTool` (ST:1114) keeps admitting a held terminal loop: refusing there closes only the cancel-before-`A4`
  order, and the drain at C:3336 already answers a pending call on a cancelling loop.
- The `Latch` (`internal/deliverylane/deliverylane.go:70`) gains no `Unlatch`: (ii) removes the benign causes of the
  latch; a latch on a real invariant failure stays a restart matter (S:984-991).
- `adoptDurableCancel` (TO:385) is not generalized (OQ7 (b) would, on a revised ruling 1).
- The `terminal` predicate (C:2219) is not widened to Cancelled (P1).

## 4. Counterexamples, controls, mutations

All at the component seam through the production callbacks `setupSubscriptions` wires (the probe's `startProbeLane`,
PROBE:181-219: `NewComponent`, `initializeKVBuckets`, real JetStream), `-race -count=20` as the probe ran. Harness
names are the ones on this tree; `raceProbeBucket` (PROBE:84-151) supplies the cancel lane's pauses (marker `Create`
entry; and, for T9, after its own record `Update` returns for `laneOf() == "cancel-lane"`).

| # | Counterexample (epic wording) | Harness and seam | Asserts on the fixed tree | Control | Mutation that must turn it red |
|---|---|---|---|---|---|
| T1 | **Terminal commitment + concurrent request advancement (W1)** | two `Component`s over one bucket — the `predecessor`/replacement pattern (TRI:170-200): A holds the loop at `R(N)`; B rebuilds it cold from a redelivered result and advances the record to `R(N+1)`; A's completion response for `R(N)` is delivered to A. Arm (c), in-process source: B is A — the tool lane's cold arm on a held loop whose routing entry was drained (LE:416-423); seam to be found by the developer, else § 10 | A: `COMPLETE_` created, one `agent.complete` retained, A's record write lost (`ErrKVRevisionMismatch` → Retry, loop released); record live at `R(N+1)`. Then B completes (same kind): marker unchanged (`content_differs` logged), the saved event republished, record terminal, `loops_completed` counted once. A's response redelivered to B is acknowledged as older. Arm (b): B fails instead → refused (TO:296), Quarantine, marker still the completion. | B's completion on a loop with **no** marker (the terminal-owner tests, `terminal_owner_test.go:72-410`). | none — an (a) row; the test asserts the documented bound. |
| T2 | **Publication failure after commitment (W2)** | `newColdApproval` (ARO:67) + `unpublishableClient` (loop_carrier_test.go:28) + `sweepExpiredApprovals`, as ALD:219-241, plus the cap shape (`e.Iterations` at the cap, `TimeoutAt` zero) and the cancel shape | (a): timeout arm = ALD:219-241; cap arm: a reject dispatches nothing and adopts; an approve dispatches ONE `tool.execute` (via `testPublishHook`/`approvedToolCallsOn`), its delivered result → `loops_failed_total{max_iterations}` +1, record `failed`, marker unchanged, saved event republished. Cancel arm: a cancel signal on the released W2 loop → `adoptDurableCancel` false → Retry (naks), record still gated (OQ7 (a)). (b): the approve dispatches nothing; record `failed` with the saved reason; `approval_inapplicable` +1; Ack. | the same approve with no marker rebuilds and dispatches (ARO:316). | (a): none. (b): delete the marker read → the cap arm dispatches (red on "no tool.execute"). |
| T3 | **W3 flipped** (cancel before the carrier's check) | PROBE:373-461 with the pause moved from the Warn to `testApprovedDispatchHook("dispatched")`; the cancel lane paused at the marker `Create` | at approval return: acks=0, naks=1, terms=0; approved `tool.execute` unchanged; `bucket.recorded()` empty; no `COMPLETE_` yet. After the cancel lane releases: one write, `cancel-lane`, `cancelled`; `loops_failed_total{cancelled}` +1, `active_loops` −1. Redeliver the approval: acks=1, `tool_results_dropped_total{approval_inapplicable}` +1, nothing published. | the same approval with no cancel dispatches and acks. | delete the § 3.1 block → red on "approved tool.execute unchanged" and "no carrier write". |
| T4 | **W4 flipped** (release before the check) | PROBE:467-542 with the same hook | acks=1, naks=0, terms=0, drains=0; health not `delivery ownership lost`; `tool.execute` unchanged; record revision unchanged; `tool_results_dropped_total{terminal_unproven}` +1; the second loop's approval: acks=1, its `tool.execute` +1, record `executing`, gate cleared. | the second-loop approval IS the control. | delete the § 3.1 block → red on acks (0) and health. |
| T7 | **Ordering A** (cancel after the check, carrier write before the owner's marker) | approval paused at `testCarrierHook("published")`; then the cancel lane runs `CancelLoop` and pauses at the marker `Create`; release the carrier | (ii): the write is refused → naks=1, `bucket.recorded()` has no carrier write, record still live; release the cancel lane → marker, event, one `cancel-lane` write. Second half (the process dies): leave the cancel lane paused and deliver the cancel to a second `Component` → record live, no marker → Retry (the honest state), never `stale_loop_id`. (i), recorded as the counterexample: the carrier writes `cancelled` before any marker; the second component's cancel is acknowledged `stale_loop_id` and no `agent.complete` is ever retained. | T3. | delete the § 3.2 refusal → red on "no carrier write" and on the second component's `stale_loop_id`. |
| T8 | **Ordering B** (owner commits and releases between publish and write) | approval paused at `published`; the cancel lane runs to completion; release the carrier. **Tool-lane arm:** a batch-completing tool result (it mints the next `agent.request`) paused at `published`; the cancel lane runs to completion; release the carrier — the stamp meets `ErrLoopNotFound` before the render | (ii): acks=1, no Fatal, health healthy, no drain; `tool.execute` = 1 (the one let out); its delivered result → `terminal_unproven` +1, nothing else. Tool-lane arm: the stamp's not-found is mapped → acks=1, no Fatal, no latch; the minted request is retained once and named by no record. (i): Fatal `get loop … for persistence` → quarantine + latch (PROBE:517-520's text); tool-lane arm: Fatal `name the published request` → quarantine + latch. | the next answer on the lane dispatches; a batch-completing tool result on a held loop mints, stamps and writes as today. | delete § 3.2's refusal → the approval arm red on acks/health (the probed W4 returns); drop the stamp mapping → the tool-lane arm red (quarantine + latch). |
| T9 | **Ordering C** (owner wrote, not yet released) | approval paused at `published`; the cancel lane paused after its own record `Update` returns (raceProbeBucket, lane `cancel-lane`), before `releaseLoopTransientState` (C:3391); release the carrier | (ii): refused → record terminal → acks=1; `bucket.recorded()` = one terminal write (`cancel-lane`). (i): two terminal writes, the carrier's second, from a different snapshot. | T3. | delete § 3.2 → red on the write count. |
| T5 | **Process replacement** | T1's two-component harness and the T7 second component are replacement at the component seam; the e2e `verifyApprovalAcrossReplacement` stage (`test/e2e/scenarios/agentic/approval_restart.go:188`) walks the cold approval branch across a real kill/start | unchanged stage; `task e2e:agentic` green on the final diff (proposal § Impact). | the stage's approve/reject settle on the replacement. | n/a. |
| T6 | **Successful controls** | the 14 order tests the archived design § 5 lists, by name (task 4.6); ARO:316; `TestApprovalLanePublishesBeforeItWrites`; `TestTheResultShapeDecidesWhatAFailedPublishLeavesBehind` (its loop is held: `carrierLoop`, loop_carrier_test.go:40) | all green: a held, non-terminal loop passes both checks and takes the same order as today | — | — |

**Tests the checks change.** `TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified` (PPF:31-59) drives a
non-terminal result for `loop-publish-phase`, never created in the manager, with `loopsBucket` nil: today Fatal on the
publish; with § 3.1, `GetLoop` not-found → `readLoopRecord` → nil bucket → `loopPresenceUnknown` → transient. The test
creates the loop in the handler first (`CreateLoopWithID`, as `trajectory_eviction_internal_test.go:39`); the review
confirms the changed assertion is the right contract. `partial_publish_settlement_integration_test.go:28` seeds its loop
(archived task 2.3) — verify. `trajectory_eviction_internal_test.go` drives only terminal states through the carrier —
unchanged. The developer runs the 28 `persistHandlerResult` test call sites (`git grep -c`) and names any other that
drives a non-terminal result on an unheld loop.

**The probe.** Retired by T3/T4/T7–T9: its two tests assert today's behaviour and go red on the fix by design; the
harness types move into the landed test file; `pauseOnLog` (PROBE:48-71) is deleted with the Warn seam.

**Invariants and the PBT decision.** I1: after the carrier's entry check, a non-terminal delivery publishes only for a
loop this process holds non-terminal (block 2 sentence; T3/T4). I2: a non-terminal delivery never writes a terminal
snapshot (block 2 "SHALL NOT be written"; T7/T9). I3: `COMPLETE_<loopID>` is written once and never overwritten
(S:1609-1610; T1). I4: a terminal record has one writer per terminal (TO:225-230; T3/T9). I5: a same-kind later
terminal converges the record; a different kind never replaces the marker (S:1612-1614; T1). The inputs are two lanes'
interleavings at five named points — a finite set of orderings each test forces explicitly — not a grammar or a
history, so named counterexamples with forced interleaving are stronger than a sampled property
(`docs/contributing/01-testing.md` § When to Use Property-Based Testing); no Rapid property. Mutation evidence: T3/T4
on § 3.1, T7–T9 on § 3.2, T2 on § 3.4 if taken.

## 5. Migration text (replaces MIG:2015-2025, "Two residuals, recorded and not reconciled")

> **Two residuals, now bounded** (#1362 issuecomment-5808903072 and issuecomment-5809906669; #1377):
>
> - A terminal whose record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves
>   a durable terminal and a published event over a live record — whether the record moved under a second process,
>   under the process's own adoption of a newer retained request for a loop it still held, or under a spawn-path
>   birth failure with a producer-supplied loop ID. The record converges at the loop's next terminal commit in
>   whichever process holds it next: a terminal of the same kind adopts the durable terminal, republishes the saved
>   event and writes the record terminal; a terminal of a different kind is refused and quarantined — the first
>   terminal wins. Until then the loop runs on under a durable terminal, bounded only by its own iteration budget and
>   `timeout_at` (no time bound while `timeout_at` is zero or the loop is gated). A watcher keyed on `COMPLETE_<loopID>`
>   counts it as finished while it runs; one keyed on the record's terminal `state` sees it at that next terminal.
> - An approval-timeout sweep terminal (its `max_iterations` auto-reject, or the loop's own timeout) that commits
>   `COMPLETE_<loopID>` and then fails to publish leaves a durable failed terminal under a record that stays
>   `awaiting_approval`; a timer is never redelivered. The record converges only on the next answer to that gate: a
>   reject, and any answer to a loop past its own deadline, dispatches nothing and adopts the durable terminal; an
>   approve of a loop at its iteration cap dispatches the approved call once, and the terminal is adopted when that
>   call's result completes the batch. [(b): The next answer to that gate adopts the durable failed terminal before any
>   rebuild and is acknowledged as inapplicable; nothing is dispatched.] A cancel of that loop does not settle it: the
>   cold cancel arm adopts only a cancel marker, so the cancel is retried until the signal consumer's `MaxDeliver` is
>   exhausted and is recorded in the MaxDeliver ledger, never applied.
>
> **A cancel racing a result on its way to the record.** A non-terminal result — an approved call, a model response's
> tool batch, a tool result's next request, a sweeper auto-reject — that reaches the loop-record carrier after a
> cancel moved the loop terminal in memory, or after the loop was released, now publishes nothing and writes nothing;
> one that passed the carrier's check but finds the loop terminal or released when its record is rendered writes
> nothing. The record decides the delivery, and the redelivered input is acknowledged as inapplicable once the
> cancel's record has landed. Before this, the carrier published the call for the cancelled loop and wrote a cancelled
> record outside the terminal owner — before, after, or beside the owner's own — and a cancel that released the loop
> mid-dispatch quarantined the delivery and latched the approval lane until restart. What remains: a cancel that lands
> inside one publish latency after the carrier's check lets that one publication out, and the durable terminal may be
> created before its PubAck; the executed call's result is acknowledged without effect on the terminal loop. The
> approval-timeout sweeper acts after the carrier returns: when the carrier settles its auto-reject this way, the
> sweeper still publishes its `agent.approval_response` echo of that auto-reject and still logs Info `approval timed
> out; auto-rejected` — the carrier's Warn and the `terminal_unproven` count, not the echo, say what happened to the
> loop. **Action:** none for a consumer of `agent.complete` / `AGENT_LOOPS`. A consumer that reads
> `tool_results_dropped_total` sees a result the carrier settled this way counted under `reason="terminal_unproven"`
> on every lane — approval answer, model response and sweeper auto-reject included — where each lane's own
> handler-entry guard counts under its own family (`model_responses_dropped_total{stale_request_id}`,
> `tool_results_dropped_total{approval_inapplicable}`).

## 6. Costs and rejected simpler alternatives (docket order)

| # | Alternative | Cost / why not |
|---|---|---|
| 1 | **All four windows documented, no code.** | W1 and W2: taken (OQ1, OQ2). W3: unavailable — a defect, not an edge case; the Codex caution and the epic's first clause forbid a quiet pass. W4: the sentence is an availability bound the check removes. |
| 2 | **Entry check only (OQ3 (i)).** | Nine lines; flips both probes; documents orderings A/B/C as bounds — including a stranded cancelled record with no marker. Cheaper row; recommended against. |
| 3 | **Entry check + render-time refusal in the carrier's own write (OQ3 (ii)) — recommended.** | ~25 lines at two existing seams; closes A/B/C on the loop side; leaves the publication in flight; A8 one redelivery earlier. |
| 4 | Widen `persistHandlerResult`'s `terminal` predicate to Cancelled (the brief's candidate (i)). | Does not flip the probe: the result is not cancelled-shaped (P1). |
| 5 | Refuse `AddPendingTool` on a terminal loop (candidate (ii)). | Closes only a cancel that precedes `A4`; the probed order still publishes. |
| 6 | Re-check under the `LoopManager` lock at the publish (candidate (iii)). | The publish is I/O; nothing holds the manager's mutex across it. |
| 7 | A post-publish `GetLoop` outside the lock (OQ3 (iii)). | Narrows only; the render under `loopRecordMu` is the point that sees the state the write carries. |
| 8 | A startup or per-tick scan of `AGENT_LOOPS` for gated records with a marker (ruling 4's literal (b)). | New read path over the bucket, deferred by the owner as OQ2 of #1330 (AS:49-54, issuecomment-5812283590); (c)-class. |
| 9 | Warm lanes read `COMPLETE_` before advancing (W1 reconciliation). | Ruled to need its own design (#1362 ruling 2); a KV read on every delivery. |
| 10 | Generalize `adoptDurableCancel` to every kind and every cold arm. | A shared adopter over a #1362-ruled cancel path; OQ2 (b) and OQ7 (b) reach adoption through `handleLoopFailure` instead. |
| 11 | One MODIFIED block on S:886 only (OQ6). | Leaves S:1587's residual sentences contradicting the row. |
| 12 | Keep the probe's Warn seam for landed tests; a blocking `json.Marshaler` in the gated call's arguments as a no-hook pause. | Misconfiguration dependence (P9); opacity and fragility. |

## 7. Residuals (doc comments, not issues)

- **Inventory correction (applied to the prose, pins untouched).** Inventory § 1's "Writer 3" paragraph and § 5's
  candidate (3) described a Cancelled-state result; the mechanism is the carrier rendering the loop's in-memory state
  at write time (P1).
- **The sentinel wrap reaches the sweeper** (OQ3 (ii)): with `GetLoop` wrapping `ErrLoopNotFound`, a loop released
  between the sweeper's snapshot and `HandleApprovalResponse`'s re-read (ARH:85) now matches AS:105 and logs the
  "already released" Warn (AS:108-110) instead of the "auto-reject failed" Error (AS:131-135) — benign; comment at
  AS:105.
- **Metric label at the carrier's Ack branch** (OQ3): every lane's carrier-settled result counts under
  `tool_results_dropped_total{terminal_unproven}`; the handler-entry guards keep their own families. Doc comment on
  `recordTerminalToolResultDropped` (TO:506-511) and on M:166-167; the § 5 sentence.
- **The publication in flight** (OQ3 (ii)): one call may leave for a loop cancelled a moment later, and the durable
  marker may precede its PubAck; the executor runs it and its result is dropped. Stated in block 2; comment at the
  `published` stage.
- **`persistDeferredContinuationMarker`** (C:3112) is a third memory-rendering write on the task lane (B3); a cancel
  between `attachContinuation` and the marker write has the same shape as W3, outside this docket's windows. Comment
  there; § 10.
- **The pre-`AddPendingTool` release** (OQ4): unforced; Retry → cold → inapplicable Ack from code. Comment at ARH:139.
- **W1's in-process sources** (P2) and the different-kind quarantine remain the "first terminal wins" rule's cost.
- **A cancel marker under a gated record** is adopted by the cancel signal's own redelivery (TO:385), not by the
  approval lane; if an answer rebuilds the loop first, the redelivered cancel cancels the held loop and adopts.
- **S:1095 title "…is owed to #1377"** is kept (openspec refuses renames); its body is discharged in block 1.
- **Approval-lane Retry latency on W3**: the redelivery's delay is the lane's `settleRetry` policy (C:1202); the
  answer is acknowledged one redelivery later, bounded by the lane's `MaxDeliver`/backoff, as any Retry.

## 8. The #1146 per-window table, as it will be posted before the epic closes

| Window | Path taken | What is guaranteed | What is only bounded | Owner amendment of the first exit clause? |
|---|---|---|---|---|
| W1 — lost record CAS after marker and event | (a) documented bound | the durable terminal is never overwritten; a same-kind later terminal converges the record and republishes the saved event; a different kind is refused (first terminal wins); the redelivered terminal input is acknowledged as older | the loop resumes ordinary work in whichever process holds it next until its own terminal — its iteration cap or `timeout_at` (no time bound if zero, or while gated); sources: a second process, the process's own step-0 adopt on a held loop, a spawn-path birth failure | **YES** — requested |
| W2 — sweeper terminal committed, publication failed | (a) documented bound [(b) if taken: cold-branch adoption on the next answer] | the record converges on the next answer to the gate; reject and past-deadline answers dispatch nothing [(b): no answer dispatches anything] | parked `awaiting_approval` with no time bound; (a) admits one approved call after the durable terminal; a cancel of the loop is retried to `MaxDeliver` exhaustion and observed there, never applied (OQ7) | (a) **YES** for that one call and for the cancel — requested [(b): the cancel half remains] |
| W3 — cancel lands while the loop is held, mid-dispatch | fix: the carrier reads the loop it publishes for, and its write refuses a terminal snapshot (OQ3 (ii)) | after a cancel commits in memory before the carrier's check, a non-terminal result publishes and writes nothing; after the check, it writes nothing once the loop is terminal or released at the render; the redelivery is acknowledged inapplicable; one terminal-record writer | a cancel inside one publish latency after the check lets one publication out, and `COMPLETE_<loopID>` may be created before that publication's PubAck — work published after the durable terminal; the executor never stops it; its result is dropped on the terminal loop | **YES** — for that publication; requested explicitly (under (i): also the record hazards A/B/C) |
| W4 — cancel releases the loop mid-dispatch | the same check and refusal | the answer is acknowledged without effect from the cancelled record whether the release preceded the check or fell between publish and write; nothing published after the check, nothing written, no quarantine, no latch; the lane keeps consuming | the pre-`AddPendingTool` release: Retry → cold → inapplicable (from code, unforced) | no (under (i): ordering B still latches — an availability bound) |

## 9. Problem shape, decision skills, adopter seam

**Shape (contract § 5):** *a non-owner meets a terminal in flight and must defer to the record* — the terminal-guard
shape. Closest existing instance, same plane: `settleTerminalGuard` (TO:481-503) with its four callers and the carrier's
backstop (C:2224-2230); on another plane, `authority_gate.go:38-59`'s structural-check-first refusal. **Adopted**: both
checks route to the existing guard settlement and add no interpreter. No pattern is established, so no adoption sweep
is owed. Decision skills: `kv-or-stream` — not triggered; `orchestration-check` — not triggered ((b) reuses the owner's
four steps); `new-payload` — none; `query-pattern` — none.

**Adopter seam.** The surfaces reached from outside are the wire and the KV record, not the Go pair (the archived
design's P7: zero sister callers of `HandlerResult`/`Handle*` at the pinned SHAs; no exported symbol changes here).
1. *What must they know?* Under (a)/(a): the bounds in § 5 — a durable terminal may precede the record's terminal by
the loop's remaining budget (W1) or until the next answer (W2), and a cancel of a W2 loop never lands. Under the fix:
nothing new — an input racing a cancel is acknowledged rather than quarantined; one call may still run. 2. *If they do
nothing?* Same observations as today for W1/W2; for W3/W4 they stop seeing a cancelled record outside the owner and a
latched approval lane. 3. *Where do they find out?* The runtime line `Delivery acknowledged without effect` (TO:490)
and the drop metric; the bounds only in docs (S, MIG) — a finding the (a) rows carry by construction. 4. *What should
they know?* Nothing; the gap is exactly the amended clauses in § 8.

## 10. Unproven (named, not claimed)

1. The three sub-window orderings A, B, C — argued from C:3167-3168, C:3081, C:3303-3309, C:1071; FORCED at
   implementation by T7–T9 and the `checked` variant (`carrier_terminal_race_integration_test.go`). What the
   `checked` variant leaves unasserted, deliberately (review 2 LOW B, not extended): the redelivered approval's Ack
   once the cancel has landed, and the executed call's result acknowledged without effect on the cancelled loop —
   both are asserted only for a RELEASED loop, by T8.
2. W2's reject-at-cap arm (a reject on a loop at its iteration cap re-derives `max_iterations` and adopts) — argued
   from H:3005-3037; FORCED by T2's cap arm (`approval_cap_sweep_integration_test.go`).
3. W2's cancel: retried to exhaustion (OQ7) — argued from C:3311-3323 and TO:397-399; FORCED by T2's cancel arm.
4. P6 (a non-terminal result at the carrier with no held loop is always a release) — argued from the producers, not
   tested; PPF:31-59 is the only unit fixture and it is changed.
5. In-process terminal CAS loss via step 0 (LE:416-423) — stated by the code's own comment, not observed; T1 arm (c).
6. The spawn-path W1 source (MIG:2019) — carried from #1362 ruling 2, not re-derived here.
7. The pre-`AddPendingTool` release path (OQ4) — FORCED by the `before_dispatch` test
   (`TestALoopReleasedBeforeTheApprovedCallIsRegisteredIsRetriedThenInapplicable`).
8. The task lane's deferred-marker write racing a cancel (§ 7) — out of the docket; not observed.
9. The literal A8 re-read race (the approval handler's own `GetLoop` at ARH:83 racing a release) — no seam between the
   resolve and that read; the block-1 scenario is forced one read later, on the reject path's `HandleToolResult` read
   (`TestARejectWhoseLoopWasReleasedAfterItsGateResolvedIsSettledColdOnItsFirstDelivery`), and the still-gated branch
   rests on `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt`.
