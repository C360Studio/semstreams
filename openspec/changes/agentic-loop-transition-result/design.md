# Design — agentic-loop-transition-result (#1376): the `(result, error)` table and its one owner

> Change id `agentic-loop-transition-result`, claiming #1376 (draft PR #1381). Base `f15a528e`; the inventory is pinned
> at `7f28a2f3` (= `f15a528e` + the proposal commit) and verified 197/197 by `task inventory:verify`.
>
> **Designed against `main` at `f15a528e` PLUS PR #1380** (`claude/gh1374-terminal-owner-loop-metrics` at `e2881331`,
> ruling 1: #1374 lands first). Every `file:line` below is at `f15a528e`; where #1380 changes a cited line its form
> after #1380 is named beside it. #1380 moves all terminal counting into the terminal owner (`recordCommittedTerminal`,
> `terminal_owner.go`), drops `handleLoopFailure`'s `entity` parameter, adds the `errLoopTimedOut` sentinel that
> `failTimedOutLoop` returns and `failureReasonForHandlerError` maps to `timeout`, and deletes `recordTerminalState` and
> the tool lane's `MaxIterationsReached` read. It does not touch the three guard sites, `persistHandlerResult` or
> `carrierOrder` (PR #1380 body, "Boundary").
>
> Rulings applied, none reopened: #1146 issuecomment-5828511934 rulings 1–5 (2026-09-25); the #1362 L4b rulings and
> their two recorded residuals (#1377's); the 2026-09-22 standing rule (simple over edge-case: the doc-sentence
> alternative is the first docket row). **Bound (ruling 2):** the contract is a table over the combinations the loop
> actually produces, carried as ONE MODIFIED requirement; no new exported API; `agentic` (Tier 1) untouched. This pass
> produced rows, not requirements. Four rows would change an observable disposition; they are owner questions (§ 0),
> not decisions.
>
> Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ARH `approval_response_handler.go`, AS
> `approval_sweeper.go`, TO `terminal_owner.go`, S `openspec/specs/agentic-loop/spec.md`, DS
> `natsclient/delivery_settlement.go`.

## 0. Owner questions — first

Each question's option **(a)** is "table only": the row records today's disposition and the spec scenario says so.
The design below assumes the recommendation; the scenarios and sentences that depend on an answer are marked in the
delta's header note and in `tasks.md`. Nothing in § 4 (the decision shape) changes a disposition on its own.

### OQ1 — a terminal-shaped result with no terminal event (row E1)

- **Producers.** `handleCompleteResponse` sets `result.State = LoopStateComplete` at H:2513 and `result.CompletionState`
  at H:2609; between them it returns an error at H:2517 (`UpdateCompletion`), H:2596 (`json.Marshal`) and H:2600
  (`ResolveSubject`). `failLoop` (H:2402), `failTimedOutLoop` (H:3375) and `handleToolsComplete` (H:3029) set
  `FailureState` only inside `if …; fErr == nil {`, after `result.State = LoopStateFailed` (H:2393, H:3373, H:3017);
  `buildFailureEvent` fails when `GetLoop` fails (H:3315-3317), i.e. when the loop was released concurrently.
- **Today.** On the carrier the result is terminal by state (C:2246) and goes to `commitTerminal` with an empty
  `terminalOutcome` (`terminalOutcomeOf`, TO:32-38): `createTerminalMarker` writes nothing (`event == nil`, TO:210-212),
  `stampTerminal` stamps nothing (TO:279), `publishResults` publishes nothing, `persistLoopState` writes the record
  **terminal** — and the delivery is **acknowledged**. The record is terminal with no `COMPLETE_<loopID>` and no
  terminal event, the state S:1489-1491 forbids and S:910-911 says must not be acknowledged. The same shape through
  `handleLoopFailure` is treated the other way: it commits, then returns Fatal (C:2104-2106, C:2113) — Quarantine — and
  #1380 counts it under `reason="unknown"` (its `recordCommittedTerminal` doc comment).
- **Proposed row.** A terminal state with neither event is a combination the table does not admit: the carrier refuses
  it as fatal **before** the record write, releases the loop from memory (as `commitTerminal` does for a commit that
  did not land, TO:146-158), and the delivery quarantines. One check at the carrier's terminal branch (C:2278) covers
  all six producers.
- **Observable delta.** Ack + silent terminal record → Quarantine (lane latches) on a path reachable only through a
  concurrent release or a marshal/subject failure. `active_loops` is not decremented for a refused terminal (no commit).
- **Options.** (a) table only — the row and scenario say "acknowledged; the record is terminal with no marker";
  (b) refuse at the carrier as above. **Recommendation: (b)** — ruling step 4 already names boundary validation for a
  combination that must not occur, and (a) enshrines a written contradiction of S:1489-1491 in the same spec file.

### OQ2 — an approval-lane error after the gate resolved in memory (rows A8, D3)

- **Producers.** ARH:85 (`GetLoop` fails after `ResolveApprovalIfPending` succeeded at ARH:59) and ARH:107/ARH:113
  (`dispatchApprovedCall` fails; its error is `errs.Wrap`, unclassified, ARH:139). In both, the gate is already cleared
  in memory (`PendingApproval` nil, state restored) and nothing is written.
- **Today.** The lane classifies by error class (ARH:234-244): unclassified → **Retry**. The redelivery re-runs
  `ResolveApprovalIfPending`, finds no gate (`!ok`, ARH:66), returns `staleDrop` and is **acknowledged** (ARH:248-254).
  Net: the human's answer is lost; memory holds the loop un-gated with no outstanding request and no dispatched call;
  the record still says `awaiting_approval`; a later answer is dropped the same way. A silent wedge.
- **Spec rule already written.** S:907-909: "A failure that arrives after a handler has already moved its loop in
  memory SHALL be treated as a partial effect and quarantined, never retried." The tool lane implements it
  (`settleFailedToolResult`, C:2666-2672, force-Fatal); the approval lane does not.
- **Proposed row.** Both producers return `errs.WrapFatal` (ARH:85 wraps `getErr`; ARH:139 `errs.Wrap` → `errs.WrapFatal`),
  so the existing class map at ARH:236 quarantines them. Two producer-side wraps; no consumer change.
- **Observable delta.** Retry-then-silent-Ack → Quarantine (lane latches, loudly) on two race-window paths.
- **Options.** (a) table only — the row says "retried; the redelivery acknowledges the answer as stale";
  (b) fatal at the two producers. **Recommendation: (b)** — it is the spec's own sentence, and the loop is wedged either
  way; (b) makes it visible.

### OQ3 — a tool result whose loop was released between routing and handling (row A3, tool lane)

- **Producer.** H:2649: `HandleToolResult`'s `GetLoop` fails after `findLoopIDForToolCall` resolved the loop (C:2515);
  the component's own pre-classification `GetLoop` at C:2541 already tolerates this race ("Nothing is classified then",
  C:2541-2547). The error is `errs.Wrap` "loop … not found" (state.go:655-666), not the `ErrLoopNotFound` sentinel.
- **Today.** Zero-value result + unclassified error → `settleFailedToolResult` → not terminal, not
  `errCancelledBeforeMutation` → **Quarantine** (C:2671). Nothing was mutated.
- **Spec definitions.** S:903-906: Retry "means stable identity and reconciliation make re-execution safe"; Quarantine
  "means collision, impossible correlation, panic, or invariant failure prevents a safe choice". Re-execution here is
  safe: release clears the routing entry (C:2508-2511), so the redelivery takes the cold path (C:2516) and settles from
  the record (S:1024).
- **Options.** (a) table only — the row says "quarantined; a redelivery would settle from the record" and the scenario
  records it as the lane's answer to a mid-delivery release; (b) H:2649 wraps `ErrLoopNotFound` and
  `settleFailedToolResult` retries it. **Recommendation: (a)** — the window is the lines between C:2541 and H:2646, a
  release there requires a terminal commit or lost compare-and-swap on another lane in that instant, and (b) adds a
  sentinel match on an exported method's error for it. The row keeps the finding; #1377 may pick it up if its recovery
  path meets it.

### OQ4 — two exported `HandlerResult` fields with no production counterpart (Tier 1)

- `RetryScheduled` (H:66) has zero production writers (inventory § HandlerResult; searches recorded there).
  `MaxIterationsReached` (H:67) has one writer (H:3018) and, **after #1380 deletes C:2598**, zero production readers.
- `processor/agentic-loop` is on the Tier 1 list (`release/tier1-packages.txt:75`); `task api:compat` runs `apidiff`
  over it (`scripts/api-compat.sh`), and removing an exported struct field is an incompatible change under ADR-106 § 5.
  Neither is a removal row.
- **Options.** (a) leave both; the table notes them (no cost, default); (b) hand both to #1314's RC vocabulary pass.
  **Recommendation: (a).** The design assumes (a).

## 1. Premises (measured)

| # | Premise | Measurement |
|---|---|---|
| P1 | The `order` argument of `persistHandlerResult` is dead. | Terminal results return at C:2278-2282 before `order` is read at C:2285; the three `writeThenPublish` callers sit behind `result.State.IsTerminal()` (ARH:210→221, AS:113→123, C:2659→2664). For a non-terminal result `order == publishThenWrite && !gated` (C:2285) ≡ `!gated`. |
| P2 | The failed-terminal decision is copied three times. | `err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere` at ARH:210 and AS:113; `result.State.IsTerminal()` at C:2659 (reached only when `err != nil`, C:2581-2582). |
| P3 | A terminal-guard result is never returned with an error, so the missing `!terminalOwnedElsewhere` conjunct at C:2659 is not a defect. | `terminalGuardResult` (H:2629) is produced at H:1433 (`return …, nil`), H:2660 (`return …, nil`) and H:2877 inside `checkApprovalGate`, whose caller returns it at H:2734 (`return result, nil`); no later return in `HandleToolResult` (H:2782-2815) can carry it because the gate check returned `true` and the function exited. On the approval lane the reject path returns `HandleToolResult`'s pair unchanged (ARH:163). |
| P4 | The model lane does not duplicate the decision; it re-derives the failure from the error. | C:1866-1907: five sentinels, then `handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)`; after #1380 `(ctx, loopID, reason, err)` and `errLoopTimedOut → "timeout"`. The populated result from H:1469 is discarded except for `recordTrajectoryObservations` (C:1903). |
| P5 | `handleLoopFailure` on a loop already failed in memory succeeds. | `TransitionTo` answers the same-state case with nil before the terminal check (`agentic/state.go:207-215`); #1380's `TestAModelLaneTimeoutIsATimeout` asserts Ack and marker reason `timeout` on exactly this path. `Complete → Failed` errors ("cannot transition from terminal state"), so a `handleCompleteResponse` error on the model lane (H:1575) is a plain-error Retry, not a commit. |
| P6 | The heartbeat lanes derive their disposition from the error's class. | C:1281-1298: nil → Ack; `errs.IsFatal` → Quarantine; `*natsclient.PermanentDeliveryError` → Terminate; else Retry. Approval lane: ARH:234-244 (Fatal/Invalid/else). Signal lane: C:3303-3306. |
| P7 | `HandlerResult` and the `Handle*` methods are exported from a Tier 1 package with no present outside caller. | `release/tier1-packages.txt:75`; read-only grep of semspec `5a9496ee`, semteams `ce22c961`, semsage `4d28b4d`, semdragon `07f4de9`, semmachina `841c45e`, semstreams-ui `39f5f04`, semsource `4093d3c`, semmem `b909cbf`, semconnect `d0d06e0`, semboids `8c03cc5` for `agenticloop.(HandlerResult\|MessageHandler\|NewMessageHandler)` and `.Handle(ToolResult\|ModelResponse\|ApprovalResponse\|Task)(` → 0 hits in each (stderr visible, 2026-09-25). Closes the inventory's last NOT RUN item. |
| P8 | `handleToolCallResponse` (H:1598-1762) produces no terminal shape. | `awk 'NR>=1598 && NR<=1762 && /State = \|failLoop\(\|handleCompleteResponse\(\|FailureState\|CompletionState/'` → 0 lines; `failLoop` callers are H:1585, H:2201, H:2221 and `handleCompleteResponse` callers H:1575, H:2789 — all outside the range. Its five `return result, err` lines are one row (D1). Closes the inventory's first NOT RUN item. |
| P9 | The inventory's annotation of H:1316 is wrong; its pin is right. | H:1314-1316 is `entity, err := h.loopManager.GetLoop(loopID); if err != nil { return HandlerResult{}, err }` — a `GetLoop` failure, not a record-classification failure. Row A3. |
| P10 | The settlement requirement is S:886, which the inventory did not pin. | `### Requirement: Loop input classes settle after owner-specific durable done` (S:886); first line carries SHALL (S:888). The inventory's S:1408 pin (bucket acquisition) is not on this territory; S:1467 is the ORDER authority the table cites and is not modified. |

## 2. The table

Columns, per ruling 2: business outcome · delivery disposition today (Ack / Retry / Terminate / Quarantine, or "by class"
= P6 for the three plain-error lanes) · commitment (known / unknown) · the one owner that commits · the publication-order
obligation the shape implies (birth / gate / ordinary / terminal, S:1486-1491) · replacement behaviour (what a process
that did not produce the result does with the redelivered input). "—" = nothing to commit.

### A. Refusals — an error with no terminal payload

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement |
|---|---|---|---|---|---|---|---|
| A1 | zero-value + pre-mutation cancellation: task H:869 (`ctx.Err()`), model H:1312, tool H:2644 (`errCancelledBeforeMutation`) | none; nothing touched | task: log + **Ack** (S:984 exemption, C:1521-1522); model: **Retry** (C:1889-1895 → P6); tool: **Retry** (C:2668-2669) | known (none) | — | — | redelivery re-runs the handler |
| A2 | zero-value + invalid input: task H:874 (`WrapInvalid`, depth), approval ARH:52 (`WrapInvalid`, `Validate`) | refused input | task: log + **Ack** (exempt); approval: **Terminate** (ARH:238) | known (none) | — | — | never redelivered (approval); #1345 (task) |
| A3 | zero-value/LoopID-only + loop not held: model H:1316 (`GetLoop`, P9), tool H:2649, approval ARH:64 (`ErrLoopNotFound` or other resolve error), sweeper AS:104-111 | the process does not hold the loop | model: falls to `handleLoopFailure`, whose `TransitionLoop` fails → plain error → **Retry** (C:2078-2082; S:960-965); tool: **Quarantine** (C:2671) — **OQ3**; approval not-found: cold branch (ARH:195-208): Ack when the record is absent/terminal/inapplicable, re-run when rebuilt, Retry/Quarantine by class on a cold error; approval other: **Retry** by class; sweeper: log, `continue` | known (none) | — | — | the redelivery reads the record (S:1024) |
| A4 | model-lane classification: zero-value + `errResponseSuperseded` H:1367, `errResponseAlreadyApplied` H:1419, `errResponseForeign` H:1375; non-empty base result + `errRequestNotYetObservable` H:1382-1389 | the answer is older / already used / not this loop's / newer than the record | **Ack** (C:1877, C:1886); **Quarantine** (C:1899-1901); **Retry** (C:1868-1874) | known (none) | — | — | classified against the record the same way (S:1467) |
| A5 | zero-value + creation failure: task H:924 (`attachContinuation`: `ErrLoopBusy`/`ErrLoopTerminal`), H:934, H:939, H:968, H:982, H:1102 | no loop born / continuation refused | log + **Ack** (C:1516-1522) — the S:984 exemption | known (none) | — | — | **#1345** (row O3) |
| A6 | LoopID-only + recovered panic ARH:45 (`WrapFatal`); zero-value + unknown decision ARH:119 (unreachable after `Validate`) | handler unsafe / impossible | **Quarantine** (ARH:236-237); Retry by class (unreachable — a note, not a row) | unknown | — | — | lane drains (S: "Approval handler panics") |
| A7 | zero-value + store failure after routing: tool H:2708 (`StoreToolResult`), H:2714 (`RemovePendingTool`) | partial in-memory effect possible | **Quarantine** (C:2671) | unknown | — | — | lane drains; S:907-909 |
| A8 | zero-value + `GetLoop` failure after the gate resolved: approval ARH:85 (`errs.Wrap`) | gate cleared in memory, answer not applied | **Retry** by class (ARH:242) → redelivery `staleDrop` → Ack — **OQ2** | unknown (partial) | — | — | the record still holds the gate; a later answer is dropped |

### B. Applied — a result with no error, committed by the carrier in the order its shape implies

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement |
|---|---|---|---|---|---|---|---|
| B1 | `Created` H:1214 (returned H:1256/H:1105) + nil | loop born: record + `agent.request` + `agent.created` | task lane: record by create-once (C:1674) then `publishResults` (C:1716); refused create → loop released, **Retry** (C:1676-1681); other write → **Retry**; publish failure → loop released, **Retry** (C:1716-1720); success → **Ack** | known at Ack | the task lane itself (`createLoopState` + `publishResults`, not `persistHandlerResult`) | **birth** S:1486-1487 | S:1467 task-redelivery scenarios: republish R1 only when nothing is retained |
| B2 | `LoopID`-only H:896 + nil | duplicate task, loop active | **Ack** (C:1550-1556) unless a pending lineage result resumes (C:1557-1561) | known (none) | — | — | dedup against the record's `task_id` (S:1535-1540) |
| B3 | `Deferred` H:1124 (via H:1097) + nil | turn appended in memory; marker owed | marker write (C:1539); lost compare-and-swap → **Retry** (C:1545); other write → best-effort, **Ack** (C:1547) | known only for the marker | the task lane (`persistDeferredContinuationMarker`) | ordinary (marker only, no publication; S:1533-1534) | text not recoverable; rebuild clears the marker with a warning (S:1529-1534) — **#1365** (row O2) |
| B4 | `terminalOwnedElsewhere` (`terminalGuardResult` H:2629) from H:1433, H:2660, H:2877 + nil | the loop is terminal in memory; nothing touched | `settleTerminalGuard` (TO:423): record stale → **Ack** + drop metric; live or unreadable → **Retry** (TO:435-437). Callers C:1910-1917, C:2584-2588, ARH:262-271, backstop C:2251-2257 | known (elsewhere) | the terminal owner, on the lane that committed | — | the record decides (S:1024; S: "A terminal loop receives a result it cannot prove it applied") |
| B5 | non-terminal, non-gated result + nil: model H:1590; tool H:2815, H:3046; approval ARH:107/ARH:113 (nil); sweeper AS:158 | next request or tool call published; record advanced | `publishThenPersistResultState` (C:2285-2286, C:2333-2358): publish failure → **Quarantine** (C:2335); lost compare-and-swap → **Retry** (C:2352); other write → **Quarantine**; success → **Ack**. Sweeper: logged, not counted (AS:158-163) | known at Ack | the carrier (`persistHandlerResult`) | **ordinary** S:1484-1486 (publish, then record by compare-and-swap) | W4 by identity: the redelivery adopts the retained request (S:1467 W4 scenarios) |
| B6 | `State == awaiting_approval` H:2888 (+ re-echo / sibling absorb H:2843-2861) + nil | loop gated on a human | write-then-publish (C:2249, C:2285, C:2311-2330): lost compare-and-swap → **Retry**; other write → **Quarantine**; publish → **Quarantine**; success → **Ack** | known at Ack | the carrier | **gate** S:1487-1489 | a redelivered `approval_required` result re-publishes the gate from the record (S:1512-1513, C:2816) |
| B7 | completion event (H:2513 + H:2609, via H:1575 / H:2789) or failure event (`failLoop` H:2393 + H:2404 via H:1585/H:2201/H:2221; max-iterations H:3017-3034) + nil | loop complete / failed | `commitTerminal` (C:2278): lost compare-and-swap → **Retry** with the loop released (TO:146-158, TO:181-190); any other step → **Quarantine**; success → **Ack** | known at Ack | the terminal owner | **terminal** S:1489-1491 (marker → stamp → event → record) | adopt by loop ID and kind; different kind → Quarantine (S:1494-1496) |
| B8 | `staleDrop` ARH:78 + nil | answer too late to act on | **Ack** + inapplicable metric (ARH:248-254); sweeper `continue` (AS:135-141) | known (none) | — | — | the cold branch acknowledges an inapplicable answer (S: "cold replacement adopts past a rejection-minted request") |

### C. Failed terminal — an error accompanying a populated terminal (the disputed shape)

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement |
|---|---|---|---|---|---|---|---|
| C1 | `failTimedOutLoop` (H:3366-3379: `State=failed`, `FailureState`, `PublishedMessages`, error `WrapFatal`; after #1380 wrapping `errLoopTimedOut`) from tool H:2691, approval ARH:102, sweeper via AS:104 | the loop's deadline passed; the failure IS the settlement | tool: C:2659-2664 → `persistHandlerResult` → `commitTerminal`; approval: ARH:210-233; sweeper: AS:113-129. Nil → **Ack**; lost compare-and-swap → **Retry**; other → **Quarantine**; sweeper logs. **The error's class is not read.** | known at Ack | the terminal owner | **terminal** | the redelivered answer re-derives the timeout on the rebuilt loop and adopts the durable terminal (S:1503-1505) |
| C2 | the same producer on the model lane, H:1469 | same | C:1866-1907: no sentinel matches → `handleLoopFailure` (C:2066; #1380: `(ctx, loopID, "timeout", err)`) rebuilds the failure from the error and commits it through the terminal owner: nil → **Ack**; lost compare-and-swap → **Retry** (C:2111-2112); other → **Quarantine** (C:2113). The populated result's event is discarded; the committed one has the same kind and, after #1380, the same reason. | known at Ack | the terminal owner (via `handleLoopFailure`) | **terminal** | as C1 |

**The one answer (asked by the caller): when a populated terminal result is present, the error is the terminal's
CAUSE — logged at the site, carried as the failure event's reason by the producer — and not a disposition. The
disposition is the terminal owner's commit result. This holds on every lane; the model lane reaches it through a
rebuilt event rather than the handed one (C2), which #1380 made carry the same reason.** The max-iterations shape
(H:3017-3034, error nil) is row B7: same content, no error, because the handler did not fail — it exhausted a budget
and says so in the event; the timeout shape carries an error because `failTimedOutLoop`'s callers are inside a handler
that must stop (H:3360-3365 doc comment). Both reach the same owner in the same order.

### D. Non-terminal result with an error

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement |
|---|---|---|---|---|---|---|---|
| D1 | model: base result + `WrapFatal(ErrMaxIterationsReached)` H:1474; accumulated result + dispatch failure H:1536, H:1567, H:1576, H:1581, H:1586 (P8); `failLoop`/`handleLengthTruncation` `TransitionLoop` failure H:1581/H:1586 | the loop cannot continue; it is failed | `handleLoopFailure` with reason `max_iterations` / `handler_error` (C:1929-1937; #1380 adds `timeout`): commit → **Ack** / **Retry** / **Quarantine** as C2; a loop that cannot be transitioned → **Retry** (S:960-965) | known at Ack | the terminal owner (via `handleLoopFailure`) | **terminal** | as B7 |
| D2 | tool: accumulated result + error after a mutation: H:2782, H:2790 (non-terminal case), H:2800, H:2991 (`ctx.Err()` after `StoreToolResult`), H:3005, H:3015, H:3043 | partial effect | **Quarantine** (`settleFailedToolResult` C:2666-2672; S:907-909, S: "A tool result is cancelled after the loop has advanced") | unknown | — | — | lane drains |
| D3 | approval: base result + `dispatchApprovedCall` failure ARH:107/ARH:113 (`errs.Wrap`, ARH:139) | gate cleared, call not dispatched | **Retry** by class (ARH:242) → redelivery `staleDrop` → Ack — **OQ2** | unknown (partial) | — | — | as A8 |

### E. Must not occur

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement |
|---|---|---|---|---|---|---|---|
| E1 | `State ∈ {complete, failed}` with `CompletionState == nil && FailureState == nil`: with error H:2517, H:2596, H:2600 (via H:2789 → C:2659); with nil H:2402, H:3029, H:3375 (`fErr != nil`, i.e. H:3315-3317) | a terminal the loop cannot name | carrier: **Ack**, record written terminal, no marker, no event (C:2278 → TO:210-212) — **OQ1**; `handleLoopFailure`: commit then **Quarantine** (C:2104-2113) | unknown | the terminal owner, with nothing to commit | terminal — violated | a redelivered input finds a terminal record and is acknowledged (S: "terminal loop receives a result") — the loss is silent |

### F. Publication carriers — `HandlerResult` values that are not transitions

| Site | What it is | Owner | Order |
|---|---|---|---|
| C:2102-2103 `handleLoopFailure` | a bare `{LoopID, PublishedMessages}` handed to `commitTerminal` with the rebuilt failure | the terminal owner | terminal |
| C:3422 `handleCancelSignal` | the cancel event's carrier for `commitTerminal`; the lane's decision is the commit's error (C:3427-3431 → C:3303-3306) | the terminal owner | terminal |
| C:2816 `republishPendingApproval` | the gate's re-echo, `publishResults` only; no write (S:1512-1513) | — (publication) | gate already written |
| TO:362 `writeRecordCancelled` | the adopted cancel's publication inside the cold cancel adoption | the terminal owner | terminal (marker exists → event → record) |
| verdict lane C:3461 / C:3504 | no `HandlerResult` at all (inventory § Verdict) | — | — |

These need a **note, not a row**: no `(result, error)` pair is read at any of them, and the two terminal ones are
already inside the owner. The signal lane's single literal (C:3422) is committed by `commitTerminal`, whose error the
lane classifies exactly as the carrier's terminal branch does.

### O. Obligations — one row each, nothing implemented here

| Row | Obligation | The row's columns | What the owner of the obligation changes |
|---|---|---|---|
| O1 | **#1377 terminal consistency.** A terminal whose marker and event landed and whose record write lost its compare-and-swap (S:1496-1499); a sweep terminal whose marker landed and whose publish failed (S:1499-1502) | outcome: durable terminal exists, record live · disposition: the redelivered input's own lane classification (Retry on the live record via B4, or adoption via B7) · commitment: **known for the marker, unknown for the record** · owner: the terminal owner on the next input · order: terminal · replacement: adopt by identity; a timer is never redelivered | the record converges on the durable terminal through the declared recovery path (#1377 docket (a)/(b)/(c)); no row here changes disposition |
| O2 | **#1365 durable accepted input (with #1345, ruling 3).** Row B3's replacement column: the deferred turn and the task prompt are not on the record (S:1529-1534) | as B3 | the rebuilt loop recovers the turn and the prompt from durable accepted-input facts on `agentic.LoopEntity` (Tier 1 review there); B3's replacement column changes, nothing else |
| O3 | **#1345 resumable task intake.** Rows A1 (task), A2 (task), A5 keep the S:984 log-and-acknowledge exemption | as listed | when the lane converts, those rows take the class-derived disposition (P6); B1's birth order is unchanged |

**Row count.** 27 rows (A 8, B 8, C 2, D 3, E 1, F 5 notes, O 3). **Rows whose disposition today differs from the table
under the recommendations:** 2 — E1 (OQ1: H:2517, H:2596, H:2600 with error; H:2402, H:3029, H:3375 with nil) and the
A8/D3 pair (OQ2: ARH:85, ARH:107, ARH:113). Under (a) everywhere: 0.

## 3. Answers to the design questions the inventory raised

1. **Same terminal content, opposite error presence** (`failTimedOutLoop` vs H:3017-3034). One answer, § 2 C: with a
   populated terminal present the error is the cause, not the disposition; the max-iterations shape carries none because
   the handler completed its work. Rows C1/C2 and B7.
2. **`settleFailedToolResult` lacks `!terminalOwnedElsewhere`.** Not a defect — handled earlier: P3. A guard result is
   returned with nil on all three producers, so the conjunct is vacuous on every lane. The one decision keeps it so the
   three sites read identically; the tool lane gains it as a no-op.
3. **`handleLoopFailure` ignores the handler's result.** The consolidated owner does NOT take the handler's result on
   the model lane (row C2, P4). #1380 changed the one thing that made the rebuilt event disagree with the handed one —
   the reason (`handler_error` → `timeout`, `errLoopTimedOut`) — so after #1380 the two events differ only in `Error`
   text (`failTimedOutLoop` writes the bare sentinel text, H:3375; `handleLoopFailure` writes `err.Error()` of the
   `WrapFatal`, C:2088 → `pkg/errs/errs.go:108`) and in the commit context (`handleLoopFailure` detaches to a 5s budget,
   C:2098-2100; the carrier uses the delivery context). Taking the handler's result would change both — a rejected
   alternative (§ 7), not a row.
4. **`RetryScheduled`.** Inside the Tier 1 apidiff guard (`release/tier1-packages.txt:75`); not a removal — OQ4, with
   `MaxIterationsReached` beside it after #1380.
5. **The three bare constructions and the signal literal.** § 2 F: a note. C:2103 and C:3422 are committed by
   `commitTerminal`; C:2816 and TO:362 are publications. None reads a `(result, error)` pair.

## 4. The decision shape (§ What changes, proposal)

Smallest idiomatic Go that fits the measured rows, given the ruling's verified premise ("one private `(result, err)`
decision helper, three call sites, one deleted parameter"):

```go
// failedTerminal reads a handler's (result, err) pair once: it reports whether
// the error accompanies a populated terminal the loop owner must commit — the
// business failure IS the loop's settlement, and the delivery settles on the
// commit, not on the error's class. A guard result never carries an error
// (P3); the conjunct keeps the three sites identical.
func failedTerminal(result HandlerResult, err error) bool {
	return err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere
}
```

- **Three call sites** replace their inline guards: ARH:210 `if failedTerminal(result, err) {`, AS:113 the same,
  C:2659 `if failedTerminal(result, cause) {`. The model lane keeps its sentinel switch and `handleLoopFailure` (row C2).
- **`persistHandlerResult(ctx, result)`** loses `order` (C:2245). Its body keeps the terminal branch first (C:2278), then
  `if !gated { return c.publishThenPersistResultState(ctx, result) }` and the write-then-publish tail for a gate
  (C:2311-2330). `carrierOrder`, `writeThenPublish`, `publishThenWrite` (C:2193-2206) are deleted; the doc comment's
  order contract (C:2209-2244) is rewritten to say the shape decides. Birth (task lane, C:1674/C:1716), gate, ordinary
  and terminal orders are preserved exactly — P1 shows no non-terminal caller ever passed `writeThenPublish`.
- **Boundary validation, one home (OQ1 (b)):** at the carrier's terminal branch, before `commitTerminal`,
  `if terminalOutcomeOf(result).kind() == "" { c.releaseLoopTransientState(result.LoopID); return errs.WrapFatal(errTerminalWithoutEvent, …) }`
  with a private sentinel `errTerminalWithoutEvent`. Every E1 producer passes through C:2278; `handleLoopFailure` keeps
  its own handling (C:2104-2113). No other typed error is needed: OQ2 (b) is two `errs.WrapFatal` at the producers
  (ARH:85, ARH:139), consumed by the class map that exists.
- **No new exported API.** `HandlerResult`, its fields and the four `Handle*` signatures are untouched (Tier 1);
  `agentic` untouched.

**Why a predicate and not a four-way classifier.** The other three transitions already have one home each: the guard is
`settleTerminalGuard` (four callers plus the C:2251 backstop), the applied case is `persistHandlerResult`, the refusal
is the lane's class map (P6). An enum would add a type whose only consumer is the three sites that need one bit.

## 5. Tests

**Keep pinning the orders (unchanged, from the inventory § Tests):** `TestAnApprovalGateIsWrittenBeforeItsEventIsPublished`
(loop_carrier_test.go:152), `TestTheApprovalTimeoutSweepNamesTheRequestItPublished` (:226),
`TestBirthRefusesASecondCreateForTheSameLoop` (:347), `TestBirthWhosePublishFailsIsNotAcknowledged` (:378),
`TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks` (persist_handler_result_test.go:208),
`TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged` (:252), `TestTerminalOwnerArms` (terminal_owner_test.go:72),
`TestCancelTakesTheTerminalOwnerAndClearsAPendingApproval` (:186), `TestLoopFailureTakesTheTerminalOwnersOrder` (:226),
`TestAResponseMeetingAnUncommittedTerminalWritesNothing` (:306), `TestAResponseMeetingACommittedTerminalIsAcknowledged`
(:324), `TestAToolResultForATerminalLoopTouchesNothing` (:376), `TestAnApprovalWhoseRecordMovedIsRetried` (:410),
`TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains` (publication_semantics_integration_test.go:125); and the
failed-terminal row's lane tests: approval_loop_deadline_test.go:61/:82/:110/:143/:219,
approval_timeout_recovery_test.go:50, plus #1380's terminal_metrics_test.go (one per lane through the production entry).

**Change with the parameter (9 call sites in 8 files, inventory § carrierOrder):** loop_carrier_test.go:76/:87,
persist_handler_result_test.go:69/:224, publish_phase_fatal_test.go:48, terminal_owner_test.go:399,
trajectory_eviction_internal_test.go:31, task_redelivery_integration_test.go:187,
tool_result_redelivery_integration_test.go:216. `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind`
(loop_carrier_test.go:71) drives both orders on a NON-terminal result through the parameter; its second subtest
("write then publish commits the record", :84-92) is only reachable by a gate-shaped result once the parameter is gone,
which `TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks` already asserts — the subtest is rewritten to a gated
result or folded into that test, and both names lose "order"/"whatever its lane asks".

**New:** (i) a table-driven test of `failedTerminal` over the four axes (state × terminal payload × owned-elsewhere ×
error nil/plain/fatal) — exhaustive, ≤ 60 rows; (ii) OQ1 (b): a terminal-shaped result with no event, delivered
through `handleToolResultMessage` and through the approval lane, is quarantined and writes no record (counterexample:
today writes the record and acknowledges); (iii) OQ2 (b): an approval answer whose dispatch fails after the resolve is
quarantined (counterexample: today retries, then acknowledges the redelivery as stale).

**Mutation evidence (wiring, not primitive):** delete the `failedTerminal` call at ONE site and run that lane's
failed-terminal test (approval: approval_loop_deadline_test.go:82; sweeper: :143; tool:
terminal_metrics_test.go `TestAToolLaneTimeoutCountsOneTimeoutFailure`) — each must go red on its own site; `cp`
backup + checksum per the testing policy.

## 6. Invariants and the PBT decision

| Invariant (holds for every pair the loop produces) | Spec home |
|---|---|
| I1 A delivery whose handler produced a terminal is acknowledged only after the terminal owner's four steps land. | S:910-911 (existing); S:1489-1491 (existing) |
| I2 Which carrier order an applied result takes is a function of the result's shape, never of its caller. | the MODIFIED requirement's new sentence (delta) |
| I3 The error accompanying a terminal-carrying result is its cause; its class is never read for the disposition. | the MODIFIED requirement's new sentence (delta) |
| I4 A terminal state with no terminal event is never written as a terminal record. (OQ1 (b)) | the MODIFIED requirement's conditional sentence (delta) |

**PBT decision** (docs/contributing/01-testing.md § When to Use Property-Based Testing): the input is a finite product
of four small axes (P-shape), not a grammar or a history; an exhaustive table (§ 5 (i)) enumerates every class and is
stronger than a sampled property. No Rapid property; the order invariants I1/I2 are exercised by the named order tests
above, which drive real publish and write failures at the seam. Targeted mutation evidence: § 5.

## 7. Problem shape (contract § 5) and the precedent adopted

Shape: **interpret a `(value, error)` pair once, at one owner, with the invalid pairs refused loudly.** Closest existing
instance, on the delivery plane: `interpretDeliveryWork` (DS:399-421) — the closed `(decision, cause)` tuple, where Ack
with a cause or Retry/Terminate/Quarantine without one is quarantined as `InvalidDeliveryDecisionError` rather than
coerced. #1376's issue text names it. **Adopted:** the table is the closed set; `failedTerminal` is the one reading;
E1 is the invalid tuple and is refused, not coerced (OQ1 (b)). No pattern is established (the predicate is local to the
loop owner), so no adoption sweep is owed.

Decision skills: `kv-or-stream` — not triggered (no new communication path); `orchestration-check` — not triggered (no
multi-step behaviour added; the terminal owner's four steps exist); `new-payload` — none; `query-pattern` — none.

## 8. Adopter seam

The surface reached from outside this repo is the wire and the KV record, not the Go pair: P7 measured zero sister
callers of `HandlerResult` / `Handle*` at the SHAs #1380's migration note pins. For a sister that reads
`agent.failed.<loopID>` / `agent.complete.<loopID>` / `AGENT_LOOPS` / `COMPLETE_<loopID>` and the loop metrics:

1. **What must they know?** Nothing new: no wire, KV, subject or metric changes. Under OQ1 (b) they lose a shape they
   could never rely on (a terminal record with no marker/event); under OQ2 (b) a wedged gated loop latches the approval
   lane's health instead of staying silent.
2. **If they do nothing?** Same observations as today, minus the silent cases above.
3. **Where do they find out?** Log line + health (`delivery ownership lost`) for the two latches — runtime, not doc.
4. **What SHOULD they know?** Nothing. The gap between 1 and 4 is empty for this change; the wider adopter seam on the
   loop vocabulary is #1314's (RC) and is not touched.

For a Go caller of the exported handlers (none today, P7): the four transitions are private (`terminalOwnedElsewhere`
is unexported), so an outside caller cannot tell a guard result from an applied one. That is a pre-existing Tier 1 shape
question for #1314, recorded here, not designed.

## 9. Costs and rejected alternatives (docket order)

| # | Alternative | Cost / why not |
|---|---|---|
| 1 | **Table only, no code change.** The MODIFIED requirement lands; the three guards, `carrierOrder` and the E1/OQ2 rows stay as they are, documented. | Cheapest; leaves the copied decision as three sites a fourth lane can miss (the L4b class) and a dead parameter that reads as a lane choice. It is the first docket row per the 2026-09-22 rule and is the fallback if the owner takes (a) on OQ1 and OQ2 — the predicate and the parameter deletion are still worth their ~40-line diff on their own. |
| 2 | **Predicate + parameter deletion (recommended § 4), OQ1/OQ2 per the owner.** | ~40 lines production, 9 test call sites, 1–2 test rewrites; behaviour-preserving except the two owner-ruled rows. |
| 3 | Four-way `transitionKind` classifier returned by one function, called by all lanes. | A type with one consumer; the other three cases already have one home each (§ 4). |
| 4 | The model lane commits the handler's populated result (drop the `handleLoopFailure` re-derivation for C2). | Changes the model lane's `LoopFailedEvent.Error` text and its detached commit budget (§ 3.3); #1366-approved behaviour, ruling 5 says preserve it. Worth an owner question only if the `Error` text difference is found to matter. |
| 5 | Payload-defined terminal test (`FailureState != nil \|\| CompletionState != nil`) in the predicate instead of `State.IsTerminal()`. | Routes E1 through the class maps (three different answers by lane) instead of one refusal at the carrier; two definitions of "terminal" instead of one. |
| 6 | Remove `RetryScheduled` / `MaxIterationsReached`. | Tier 1 incompatible change — OQ4. |

## 10. Inventory corrections and the two NOT RUN items

- P9: H:1316's annotation (record-classification) is wrong; it is `GetLoop`. Pin text is correct.
- S:886 is the settlement requirement and was not pinned (S:1408 was, and is off-territory); the delta modifies S:886
  and cites S:1467 unchanged.
- NOT RUN 1 (`handleToolCallResponse` body): closed by P8 — no terminal shape, one row (D1).
- NOT RUN 2 (`test/` suites for order-pinning tests): not needed by any row; the 15 pinned tests are in
  `processor/agentic-loop`.
- NOT RUN 5 (sister mentions): closed by P7 — zero hits, read-only, SHAs recorded.
