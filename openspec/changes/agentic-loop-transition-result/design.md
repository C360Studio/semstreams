# Design — agentic-loop-transition-result (#1376): the `(result, error)` table and its one owner

> Change id `agentic-loop-transition-result`, claiming #1376 (draft PR #1381). Base `f15a528e`; the inventory is pinned
> at `7f28a2f3` (= `f15a528e` + the proposal commit), amended at `4f19e119` (H:1316 annotation corrected, S:886 pinned);
> `task inventory:verify` reports `pins=198 ok=198 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`.
>
> **Designed against `main` at `f15a528e` PLUS PR #1380 at its approved head `de1413d7`** (`e2881331` + `0c7ba788` +
> `de1413d7`; ruling 1: #1374 lands first). Every `file:line` below is at `f15a528e` unless suffixed `(de1413d7)`.
> #1380 moves all terminal counting into the terminal owner (`recordCommittedTerminal(entity, outcome)`, TO:225
> (de1413d7)), drops `handleLoopFailure`'s `entity` parameter, adds the `errLoopTimedOut` sentinel that
> `failTimedOutLoop` returns and `failureReasonForHandlerError` maps to `timeout`, deletes `recordTerminalState` and the
> tool lane's `MaxIterationsReached` read, and — in `0c7ba788` — reads the HELD loop in `commitTerminalSteps` before the
> record write and returns Fatal when it is not held (TO:193 (de1413d7)), makes `writeRecordCancelled` return the record
> it wrote and count it (TO:429 (de1413d7)). It does not touch the three guard sites, `persistHandlerResult` or
> `carrierOrder` (PR #1380 body, "Boundary").
>
> Rulings applied, none reopened: #1146 issuecomment-5828511934 rulings 1–5 (2026-09-25); the #1362 L4b rulings and
> their two recorded residuals (#1377's); the 2026-09-22 standing rule (simple over edge-case: the doc-sentence
> alternative is the first docket row). **Bound (ruling 2):** the contract is a table over the combinations the loop
> actually produces, carried as ONE MODIFIED requirement; no new exported API; `agentic` (Tier 1) untouched. This pass
> produced rows, not requirements. **Under the recommendations below no row changes an observable disposition** (0
> changed rows); the four owner questions in § 0 each name the (b) that would, and § 2 counts them.
>
> Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ARH `approval_response_handler.go`, AS
> `approval_sweeper.go`, TO `terminal_owner.go`, ST `state.go`, S `openspec/specs/agentic-loop/spec.md`, DS
> `natsclient/delivery_settlement.go`.

## 0. Owner questions — first

Option **(a)** is always "table only": the row records today's disposition and the spec scenario says so. The design
assumes (a) on all four; no delta scenario depends on an answer. Each (b) names what it would change.

### OQ1 — a terminal-shaped result with no terminal event (row E1). Recommendation: (a)

- **What the row is.** `State ∈ {complete, failed}` with `CompletionState == nil && FailureState == nil`. Producers:
  `handleCompleteResponse` returns an error between `result.State = LoopStateComplete` (H:2513) and
  `result.CompletionState` (H:2609) at H:2517 (`UpdateCompletion`), H:2596 (`json.Marshal`), H:2600 (`ResolveSubject`);
  `failLoop` (H:2402), `handleToolsComplete` (H:3029) and `failTimedOutLoop` (H:3375) set `FailureState` only inside
  `if …; fErr == nil {`, after `State = LoopStateFailed`. `failTimedOutLoop` always returns with `WrapFatal` (H:3379),
  so its shape is with-error; the nil-error producers are H:2402 and H:3029 only.
- **No production trigger found, and why (reviewer's probes, 2026-09-25).** An event build fails for two reasons:
  (i) the loop was released — `buildFailureEvent`'s `GetLoop` (H:3315-3317), `UpdateCompletion`'s not-found. That
  producer never writes the forbidden record: at base the carrier's `persistLoopState` → `marshalLoopRecord`
  (C:3211-3215) fails on the same `GetLoop` and `commitTerminalSteps` returns Fatal "terminal loop record has unknown
  durability" (TO:191-195) with nothing written; at `de1413d7` the held-loop read (TO:193) returns Fatal before the
  write. Quarantine, nothing on the wire or in KV. (ii) the loop is held and the event cannot be marshalled or its
  subject resolved. Subject resolution cannot fail in production: `mergePortDirection` (`component/ports.go:206-245`)
  replaces default output ports by name and refuses an override of another kind, so `agent.complete` / `agent.failed`
  cannot be absent; marshal cannot fail: the same `Metadata` was marshalled into the birth record and the same event
  types are marshalled on every lane. So the Ack-with-silent-terminal-record needs a held loop and an event build
  failure, and no production path produces one.
- **Residual (recorded, not a task).** The three guards drop `fErr` with no log line and no metric (H:2402, H:3029,
  H:3375; each is `if …; fErr == nil { … }` with no else — H:2405, H:3032, H:3378). A future producer that can fail
  the build while holding the loop would reach the carrier's empty-outcome commit silently.
- **(b), if the owner wants the boundary closed anyway.** The check belongs in `commitTerminal` (TO:151 (de1413d7)) —
  the one owner — NOT at the carrier's terminal branch (C:2278): `handleLoopFailure` passes
  `terminalOutcome{failed: nil}` when its build failed (C:2091-2103) and the reviewer's probe confirmed it writes exactly
  the forbidden record before returning Fatal (C:2104-2113). Precedent: the cancel lane's refuse-and-release
  (C:3403-3416: marshal or subject failure after the in-memory transition → `releaseLoopTransientState` + `WrapFatal`,
  nothing committed). The build error is carried into the refusal (the guards must return `fErr`, not drop it). Two arms
  of `recordCommittedTerminal` become dead and are deleted with it: `case entity.State == agentic.LoopStateComplete:`
  (TO:238 (de1413d7), a completion counted from the record with no event) and `default: reason = "unknown"`
  (TO:240-241 (de1413d7)). Observable delta: none on a production path; on the hypothetical held-loop build failure,
  Ack + silent terminal record → Quarantine.

### OQ2 — an approval-lane error after the gate resolved (rows A8, D3). Recommendation: (a)

- **Row A8, corrected.** ARH:85's `GetLoop` fails only for a loop absent from memory (ST:655-666), i.e. released
  between the resolve (ARH:59) and the read. Its error is unclassified → **Retry** (ARH:242). The redelivery's
  `ResolveApprovalIfPending` finds no loop → `ErrLoopNotFound` (ST:785-787) → the cold branch (ARH:195-208) reads the
  still-gated record, rebuilds the loop and applies the answer as the gating process would have —
  `TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt` (`approval_restore_order_test.go:316`) passes under `-race`.
  **A working recovery**, not a wedge; the first attempt's in-memory partial effect died with the release.
- **Row D3, corrected.** With the loop held, `dispatchApprovedCall` (ARH:127-141) fails only inside `dispatchToolCall`:
  `json.Marshal` (H:2127) or `ResolveSubject` (H:2130, `portConfigError` → `WrapInvalid`, `component/port_codec.go:253-256`).
  `errs.Wrap` at ARH:139 preserves the class through `%w` (`pkg/errs/errs.go:394-399`), so the subject failure is
  **Terminate** (ARH:238): the record stays gated and a later answer is stale-dropped. Neither cause is
  production-reachable: the subject, per `mergePortDirection` above; the marshal, because the call's arguments and the
  loop's metadata were already marshalled when the gate's record was written.
- **(b)** would apply S:907-909 literally at the two producers (`WrapFatal` at ARH:85 and ARH:139): A8 Retry →
  Quarantine turns the working cold recovery into a lane latch; D3 Terminate → Quarantine on an unreachable path.
  Recommended against.
- **Owner spec-wording question (no code either way).** S:907-909 reads "A failure that arrives after a handler has
  already moved its loop in memory SHALL be treated as a partial effect and quarantined, never retried." Read literally
  it covers A8's release race (the gate WAS cleared in memory) and would demand the latch, by symmetry with the tool
  lane (OQ3). The design does not propose that; the owner may want the sentence to say "moved its loop in memory and
  still holds it". Recorded here, not in the delta.

### OQ3 — a tool result whose loop was released between routing and handling (row A3, tool lane). Recommendation: (a)

- **Producer.** H:2649: `HandleToolResult`'s `GetLoop` fails after `findLoopIDForToolCall` resolved the loop (C:2515);
  the component's own pre-classification `GetLoop` at C:2541 already tolerates this race (C:2541-2547). The error is
  `errs.Wrap` "loop … not found" (ST:664-666), not the `ErrLoopNotFound` sentinel.
- **Today.** Zero-value result + unclassified error → `settleFailedToolResult` → not terminal, not
  `errCancelledBeforeMutation` → **Quarantine** (C:2671). Nothing was mutated.
- **Spec definitions.** S:903-906: Retry "means stable identity and reconciliation make re-execution safe"; Quarantine
  "means collision, impossible correlation, panic, or invariant failure prevents a safe choice". Re-execution here is
  safe: release clears the routing entry (C:2508-2511), so the redelivery takes the cold path (C:2516) and settles from
  the record (S:1024). (b) would wrap `ErrLoopNotFound` at H:2649 and retry it in `settleFailedToolResult`.
- **(a)** — the window is the lines between C:2541 and H:2646, a release there needs a terminal commit or lost
  compare-and-swap on another lane in that instant, and (b) adds a sentinel match on an exported method's error for
  it. The row keeps the finding; #1377 may pick it up if its recovery path meets it.

### OQ4 — two exported `HandlerResult` fields with no production counterpart (Tier 1). Recommendation: (a)

- `RetryScheduled` (H:66) has zero production writers (inventory § HandlerResult). `MaxIterationsReached` (H:67) has one
  writer (H:3018) and, **after #1380 deletes C:2598**, zero production readers.
- `processor/agentic-loop` is on the Tier 1 list (`release/tier1-packages.txt:75`); `task api:compat` runs `apidiff`
  over it (`scripts/api-compat.sh`), and removing an exported struct field is an incompatible change under ADR-106 § 5.
  Neither is a removal row. (b) hands both to #1314's RC vocabulary pass.

## 1. Premises (measured)

| # | Premise | Measurement |
|---|---|---|
| P1 | The `order` argument of `persistHandlerResult` is dead. | Terminal results return at C:2278-2282 before `order` is read at C:2285; the three `writeThenPublish` callers sit behind `result.State.IsTerminal()` (ARH:210→221, AS:113→123, C:2659→2664). For a non-terminal result `order == publishThenWrite && !gated` (C:2285) ≡ `!gated`. |
| P2 | The failed-terminal decision is copied three times. | `err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere` at ARH:210 and AS:113; `result.State.IsTerminal()` at C:2659 (reached only when `err != nil`, C:2581-2582). |
| P3 | A terminal-guard result is never returned with an error, so the missing `!terminalOwnedElsewhere` conjunct at C:2659 is not a defect. | `terminalGuardResult` (H:2629) is produced at H:1433 (`return …, nil`), H:2660 (`return …, nil`) and H:2877 inside `checkApprovalGate`, whose caller returns it at H:2734 (`return result, nil`); no later return in `HandleToolResult` (H:2782-2815) can carry it because the gate check returned `true` and the function exited. On the approval lane the reject path returns `HandleToolResult`'s pair unchanged (ARH:169). |
| P4 | The model lane does not duplicate the decision; it re-derives the failure from the error. | C:1866-1907: five sentinels (returns at C:1873, C:1881, C:1889, C:1895, C:1900), then `handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)` (C:1907; after #1380 `(ctx, loopID, reason, err)` and `errLoopTimedOut → "timeout"`). The populated result from H:1469 is discarded except for `recordTrajectoryObservations` (C:1903). |
| P5 | `handleLoopFailure` on a loop already failed in memory succeeds. | `TransitionTo` answers the same-state case with nil before the terminal check (`agentic/state.go:207-215`); #1380's `TestAModelLaneTimeoutIsATimeout` asserts Ack and marker reason `timeout` on exactly this path. `Complete → Failed` errors ("cannot transition from terminal state"), so a `handleCompleteResponse` error on the model lane (H:1575) is a plain-error Retry, not a commit. |
| P6 | The heartbeat lanes derive their disposition from the error's class. | C:1281-1298: nil → Ack; `errs.IsFatal` → Quarantine (C:1290); `*natsclient.PermanentDeliveryError` → Terminate (C:1294); else Retry (C:1297). Approval lane: ARH:234-244 (Fatal → Quarantine, Invalid → Terminate, else Retry). Signal lane: C:3303-3306. |
| P7 | `HandlerResult` and the `Handle*` methods are exported from a Tier 1 package with no present outside caller. | `release/tier1-packages.txt:75`; read-only grep of semspec `5a9496ee`, semteams `ce22c961`, semsage `4d28b4d`, semdragon `07f4de9`, semmachina `841c45e`, semstreams-ui `39f5f04`, semsource `4093d3c`, semmem `b909cbf`, semconnect `d0d06e0`, semboids `8c03cc5` for `agenticloop.(HandlerResult\|MessageHandler\|NewMessageHandler)` and `.Handle(ToolResult\|ModelResponse\|ApprovalResponse\|Task)(` → 0 hits in each (stderr visible, 2026-09-25). |
| P8 | `handleToolCallResponse` (H:1598-1762) produces no terminal shape. | `awk 'NR>=1598 && NR<=1762 && /State = \|failLoop\(\|handleCompleteResponse\(\|FailureState\|CompletionState/'` → 0 lines; `failLoop` callers are H:1585, H:2201, H:2221 and `handleCompleteResponse` callers H:1575, H:2789 — all outside the range. Its five `return result, err` lines are one row (D1). |
| P9 | H:1316 is a `GetLoop` failure, not a record-classification failure. | H:1314-1316 is `entity, err := h.loopManager.GetLoop(loopID); if err != nil { return HandlerResult{}, err }`. The inventory's first annotation said otherwise; corrected at `4f19e119`. Row A3. |
| P10 | The settlement requirement is S:886 (pinned at `4f19e119`). | `### Requirement: Loop input classes settle after owner-specific durable done` (S:886); first line carries SHALL (S:888). The inventory's S:1408 pin (bucket acquisition) is not on this territory; S:1467 is the ORDER authority the table cites and is not modified. |
| P11 | A released loop's terminal never reaches the record write, at base or at `de1413d7`. | Base: `marshalLoopRecord` C:3211-3215 → `GetLoop` fails → TO:191-195 Fatal. `de1413d7`: TO:193 `held, err := c.handler.GetLoop(loopID)` → Fatal before `persistLoopState`. |

## 2. The table

Columns, per ruling 2: business outcome · delivery disposition today (Ack / Retry / Terminate / Quarantine, or "by class"
= P6 for the three plain-error lanes) · commitment (known / unknown) · the one owner that commits · the publication-order
obligation the shape implies (birth / gate / ordinary / terminal, S:1486-1491) · replacement behaviour · **what a
watcher sees** (M = `COMPLETE_<loopID>` created, E = the event on the wire, R = the record's state after, D = the
delivery decision; "—" = nothing written or published). "—" in Owner/Order = nothing to commit.

### A. Refusals — an error with no terminal payload (disposition is per lane)

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement | Watcher sees |
|---|---|---|---|---|---|---|---|---|
| A1 | zero-value + pre-mutation cancellation: task H:869 (`ctx.Err()`), model H:1312, tool H:2644 (`errCancelledBeforeMutation`) | none; nothing touched | task: log + **Ack** (S:984 exemption, C:1521-1522); model: **Retry** (C:1895 → P6); tool: **Retry** (C:2668-2669) | known (none) | — | — | redelivery re-runs the handler | — ; D:Ack (task) / Retry |
| A2 | zero-value + invalid input: task H:874 (`WrapInvalid`, depth), approval ARH:52 (`WrapInvalid`, `Validate`) | refused input | task: log + **Ack** (exempt); approval: **Terminate** (ARH:238) | known (none) | — | — | never redelivered (approval); #1345 (task) | — ; D:Ack / Terminate |
| A3 | zero-value/LoopID-only + loop not held: model H:1316 (`GetLoop`, P9), tool H:2649, approval ARH:64 (`ErrLoopNotFound` or other resolve error), sweeper AS:104-111 | the process does not hold the loop | model: falls to `handleLoopFailure`, whose `TransitionLoop` fails → plain error → **Retry** (C:2078-2082; S:960-965); tool: **Quarantine** (C:2671) — **OQ3**; approval not-found: cold branch (ARH:195-208): Ack when the record is absent/terminal/inapplicable, re-run when rebuilt, Retry/Quarantine by class on a cold error; approval other: **Retry** by class; sweeper: log, `continue` | known (none) | — | — | the redelivery reads the record (S:1024) | — ; D:Retry (model) / Quarantine (tool) / cold branch (approval) |
| A4 | model-lane classification: zero-value + `errResponseSuperseded` H:1367, `errResponseAlreadyApplied` H:1419, `errResponseForeign` H:1375; non-empty base result + `errRequestNotYetObservable` H:1382-1389 | the answer is older / already used / not this loop's / newer than the record | **Ack** (C:1881, C:1889); **Quarantine** (C:1900); **Retry** (C:1873) | known (none) | — | — | classified against the record the same way (S:1467) | — ; drop metric; D:Ack / Quarantine / Retry |
| A5 | zero-value + creation failure: task H:924 (`attachContinuation`: `ErrLoopBusy`/`ErrLoopTerminal`), H:934, H:939, H:968, H:982, H:1102 | no loop born / continuation refused | log + **Ack** (C:1516-1522) — the S:984 exemption | known (none) | — | — | **#1345** (row O3) | — ; D:Ack |
| A6 | LoopID-only + recovered panic ARH:45 (`WrapFatal`); zero-value + unknown decision ARH:119 (unreachable after `Validate`) | handler unsafe / impossible | **Quarantine** (ARH:236-237); Retry by class (unreachable — a note, not a row) | unknown | — | — | lane drains (S: "Approval handler panics") | — ; health `delivery ownership lost`; D:Quarantine |
| A7 | zero-value + store failure after routing: tool H:2708 (`StoreToolResult`), H:2714 (`RemovePendingTool`) | partial in-memory effect possible | **Quarantine** (C:2671) | unknown | — | — | lane drains; S:907-909 | — ; health latched; D:Quarantine |
| A8 | zero-value + `GetLoop` failure after the gate resolved: approval ARH:85 (`errs.Wrap`; fails only for a released loop, ST:664-666) | gate cleared in memory; the release discarded it | **Retry** by class (ARH:242); the redelivery meets `ErrLoopNotFound` (ST:785-787) → cold branch → rebuild → the answer is **applied** (`approval_restore_order_test.go:316`) — **OQ2 (a)** | known (none written) | the cold branch, on the redelivery | ordinary (on the redelivery) | as B5 after the rebuild | — on the first attempt; then B5's E+R; D:Retry then Ack |

### B. Applied — a result with no error, committed by the carrier in the order its shape implies

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement | Watcher sees |
|---|---|---|---|---|---|---|---|---|
| B1 | `Created` H:1214 (returned H:1256/H:1105) + nil | loop born: record + `agent.request` + `agent.created` | task lane: record by create-once (C:1674) then `publishResults` (C:1716); refused create → loop released, **Retry** (C:1676-1681); other write → **Retry**; publish failure → loop released, **Retry** (C:1716-1720); success → **Ack** | known at Ack | the task lane itself (`createLoopState` + `publishResults`, not `persistHandlerResult`) | **birth** S:1486-1487 | S:1467 task-redelivery scenarios: republish R1 only when nothing is retained | R:running (before E); E:`agent.request`+`agent.created`; D:Ack |
| B2 | `LoopID`-only H:896 + nil | duplicate task, loop active | **Ack** (C:1550-1556) unless a pending lineage result resumes (C:1557-1561) | known (none) | — | — | dedup against the record's `task_id` (S:1535-1540) | — ; D:Ack |
| B3 | `Deferred` H:1124 (via H:1097) + nil | turn appended in memory; marker owed | marker write (C:1539); lost compare-and-swap → **Retry** (C:1545); other write → best-effort, **Ack** (C:1547) | known only for the marker | the task lane (`persistDeferredContinuationMarker`) | ordinary (marker only, no publication; S:1533-1534) | text not recoverable; rebuild clears the marker with a warning (S:1529-1534) — **#1365** (row O2) | R:`pending_continuation`; no E; D:Ack |
| B4 | `terminalOwnedElsewhere` (`terminalGuardResult` H:2629) from H:1433, H:2660, H:2877 + nil | the loop is terminal in memory; nothing touched | `settleTerminalGuard` (TO:423): record stale → **Ack** + drop metric; live or unreadable → **Retry** (TO:435-437). Callers C:1910-1917, C:2584-2588, ARH:262-271, backstop C:2251-2257 | known (elsewhere) | the terminal owner, on the lane that committed | — | the record decides (S:1024; S: "A terminal loop receives a result it cannot prove it applied") | — ; drop metric on Ack; D:Ack / Retry |
| B5 | non-terminal, non-gated result + nil: model H:1590; tool H:2815, H:3046; approval ARH:107/ARH:113 (nil); sweeper AS:158 | next request or tool call published; record advanced | `publishThenPersistResultState` (C:2285-2286, C:2333-2358): publish failure → **Quarantine** (C:2335); lost compare-and-swap → **Retry** (C:2352); other write → **Quarantine**; success → **Ack**. Sweeper: logged, not counted (AS:158-163) | known at Ack | the carrier (`persistHandlerResult`) | **ordinary** S:1484-1486 (publish, then record by compare-and-swap) | W4 by identity: the redelivery adopts the retained request (S:1467 W4 scenarios) | E:next request / tool call, then R:advanced; D:Ack |
| B6 | `State == awaiting_approval` H:2888 (+ re-echo / sibling absorb H:2843-2861) + nil | loop gated on a human | write-then-publish (C:2249, C:2285, C:2311-2330): lost compare-and-swap → **Retry**; other write → **Quarantine**; publish → **Quarantine**; success → **Ack** | known at Ack | the carrier | **gate** S:1487-1489 | a redelivered `approval_required` result re-publishes the gate from the record (S:1512-1513, C:2816) | R:`awaiting_approval` (before E); E:`ApprovalPendingEvent`; D:Ack |
| B7 | completion event (H:2513 + H:2609, via H:1575 / H:2789) or failure event (`failLoop` H:2393 + H:2404 via H:1585/H:2201/H:2221; max-iterations H:3017-3034) + nil | loop complete / failed | `commitTerminal` (C:2278): lost compare-and-swap → **Retry** with the loop released (TO:146-158, TO:181-190); any other step → **Quarantine**; success → **Ack** | known at Ack | the terminal owner | **terminal** S:1489-1491 (marker → stamp → event → record) | adopt by loop ID and kind; different kind → Quarantine (S:1494-1496) | M, then E, then R:terminal; one terminal metric (#1380); D:Ack |
| B8 | `staleDrop` ARH:78 + nil | answer too late to act on | **Ack** + inapplicable metric (ARH:248-254); sweeper `continue` (AS:135-141) | known (none) | — | — | the cold branch acknowledges an inapplicable answer (S: "cold replacement adopts past a rejection-minted request") | — ; inapplicable metric; D:Ack |

### C. Failed terminal — an error accompanying a populated terminal (the shape `failedTerminal` unifies)

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement | Watcher sees |
|---|---|---|---|---|---|---|---|---|
| C1 | `failTimedOutLoop` (H:3366-3379: `State=failed`, `FailureState`, `PublishedMessages`, error `WrapFatal`; after #1380 wrapping `errLoopTimedOut`) from tool H:2691, approval ARH:102, sweeper via AS:104 | the loop's deadline passed; the failure IS the settlement | tool: C:2659-2664 → `persistHandlerResult` → `commitTerminal`; approval: ARH:210-233; sweeper: AS:113-129. Nil → **Ack**; lost compare-and-swap → **Retry**; other → **Quarantine**; sweeper logs. **The error's class is not read.** | known at Ack | the terminal owner | **terminal** | the redelivered answer re-derives the timeout on the rebuilt loop and adopts the durable terminal (S:1503-1505) | M, E:`agent.failed` reason `timeout`, R:failed with the gate cleared; `loops_failed_total{reason="timeout"}`; D:Ack |
| C2 | the same producer on the model lane, H:1469 | same | C:1866-1907: no sentinel matches → `handleLoopFailure` (C:2066; #1380: `(ctx, loopID, "timeout", err)`) rebuilds the failure from the error and commits it through the terminal owner: nil → **Ack**; lost compare-and-swap → **Retry** (C:2111-2112); other → **Quarantine** (C:2113). The populated result's event is discarded; the committed one has the same kind and, after #1380, the same reason. | known at Ack | the terminal owner (via `handleLoopFailure`) | **terminal** | as C1 | as C1; the event's `error` text is the wrapped error's (C:2088 → `pkg/errs/errs.go:108`), not the bare sentinel's |

**The one answer (asked by the caller): when a populated terminal result is present, the error is the terminal's
CAUSE — logged at the site, carried as the failure event's reason by the producer — and not a disposition. The
disposition is the terminal owner's commit result. This holds on every lane; the model lane reaches it through a
rebuilt event rather than the handed one (C2), which #1380 made carry the same reason.** The max-iterations shape
(H:3017-3034, error nil) is row B7: same content, no error, because the handler did not fail — it exhausted a budget
and says so in the event; the timeout shape carries an error because `failTimedOutLoop`'s callers are inside a handler
that must stop (H:3360-3365 doc comment). Both reach the same owner in the same order.

### D. Non-terminal result with an error (disposition is per lane)

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement | Watcher sees |
|---|---|---|---|---|---|---|---|---|
| D1 | model: base result + `WrapFatal(ErrMaxIterationsReached)` H:1474; accumulated result + dispatch failure H:1536, H:1567, H:1576, H:1581, H:1586 (P8); `failLoop`/`handleLengthTruncation` `TransitionLoop` failure H:1581/H:1586 | the loop cannot continue; it is failed | `handleLoopFailure` with reason `max_iterations` / `handler_error` (C:1929-1937; #1380 adds `timeout`): commit → **Ack** / **Retry** / **Quarantine** as C2; a loop that cannot be transitioned → **Retry** (S:960-965) | known at Ack | the terminal owner (via `handleLoopFailure`) | **terminal** | as B7 | M, E:`agent.failed`, R:failed; D:Ack |
| D2 | tool: accumulated result + error after a mutation: H:2782, H:2790 (non-terminal case), H:2800, H:2991 (`ctx.Err()` after `StoreToolResult`), H:3005, H:3015, H:3043 | partial effect | **Quarantine** (`settleFailedToolResult` C:2666-2672; S:907-909, S: "A tool result is cancelled after the loop has advanced") — regardless of the error's class | unknown | — | — | lane drains | — ; health latched; D:Quarantine |
| D3 | approval: base result + `dispatchApprovedCall` failure ARH:107/ARH:113 (`errs.Wrap` at ARH:139 preserves the class; causes H:2127 marshal, H:2130 subject → `WrapInvalid`) | gate cleared, call not dispatched | subject: **Terminate** (ARH:238), record stays gated, a later answer is stale-dropped; marshal: Terminate (`BaseMessage.MarshalJSON` wraps Invalid; probe). **Neither cause is production-reachable** (OQ2) | unknown (partial) | — | — | none (unreachable) | — ; D:Terminate |

### E. Must not occur — not a produced combination (OQ1)

| Row | `(result, error)` — producers | Business outcome | Disposition today | Commitment | Owner | Order | Replacement | Watcher sees |
|---|---|---|---|---|---|---|---|---|
| E1 | `State ∈ {complete, failed}` with no `CompletionState`/`FailureState`: with error H:2517, H:2596, H:2600 (via H:2789 → C:2659) and H:3375→H:3379; with nil H:2402, H:3029 | a terminal the loop cannot name | **No production trigger found** (OQ1): the released-loop cause is refused Fatal with nothing written (P11 → **Quarantine**); the held-loop causes (marshal, subject) cannot occur. Would-be behaviour on the carrier if one did: record written terminal, no marker, no event, **Ack** (C:2278 → TO:210-212); through `handleLoopFailure`: record written, then **Quarantine** (C:2104-2113) | unknown | the terminal owner, with nothing to commit | terminal — violated | — | released: — ; D:Quarantine. Hypothetical held: R:terminal with no M and no E |

### F. Publication carriers — `HandlerResult` values that are not transitions

| Site | What it is | Owner | Order |
|---|---|---|---|
| C:2102-2103 `handleLoopFailure` | a bare `{LoopID, PublishedMessages}` handed to `commitTerminal` with the rebuilt failure | the terminal owner | terminal |
| C:3422 `handleCancelSignal` | the cancel event's carrier for `commitTerminal`; the lane's decision is the commit's error (C:3427-3431 → C:3303-3306) | the terminal owner | terminal |
| C:2816 `republishPendingApproval` | the gate's re-echo, `publishResults` only; no write (S:1512-1513) | — (publication) | gate already written |
| TO:362 `writeRecordCancelled` (at `de1413d7` it returns the written record and the adoption counts it, TO:429) | the adopted cancel's publication inside the cold cancel adoption | the terminal owner | terminal (marker exists → event → record) |
| verdict lane C:3461 / C:3504 | no `HandlerResult` at all (inventory § Verdict) | — | — |

These need a **note, not a row** and no scenario: no `(result, error)` pair is read at any of them, and the two
terminal ones are already inside the owner. The signal lane's single literal (C:3422) is committed by `commitTerminal`,
whose error the lane classifies exactly as the carrier's terminal branch does.

### O. Obligations — one row each, nothing implemented here

| Row | Obligation | The row's columns | What the owner of the obligation changes |
|---|---|---|---|
| O1 | **#1377 terminal consistency.** A terminal whose marker and event landed and whose record write lost its compare-and-swap (S:1496-1499); a sweep terminal whose marker landed and whose publish failed (S:1499-1502) | outcome: durable terminal exists, record live · disposition: the redelivered input's own lane classification (Retry on the live record via B4, or adoption via B7) · commitment: **known for the marker, unknown for the record** · owner: the terminal owner on the next input · order: terminal · replacement: adopt by identity; a timer is never redelivered · watcher: M (+E) with R live | the record converges on the durable terminal through the declared recovery path (#1377 docket (a)/(b)/(c)); no row here changes disposition |
| O2 | **#1365 durable accepted input (with #1345, ruling 3).** Row B3's replacement column: the deferred turn and the task prompt are not on the record (S:1529-1534) | as B3 | the rebuilt loop recovers the turn and the prompt from durable accepted-input facts on `agentic.LoopEntity` (Tier 1 review there); B3's replacement column changes, nothing else |
| O3 | **#1345 resumable task intake.** Rows A1 (task), A2 (task), A5 keep the S:984 log-and-acknowledge exemption | as listed | when the lane converts, those rows take the class-derived disposition (P6); B1's birth order is unchanged |

**Counts.** 30 entries: 21 produced-combination rows (A 8, B 8, C 2, D 3) + 1 must-not-occur row with no production
trigger (E1) + 3 obligation rows (O) = 25 rows, plus 5 publication-carrier notes (F). **Rows whose disposition would
change:** under (a) on every owner question, **0**; under (b) on every owner question, **3** — E1 (OQ1 (b): H:2517,
H:2596, H:2600, H:3375 with error; H:2402, H:3029 with nil), A8 and D3 (OQ2 (b): ARH:85; ARH:107, ARH:113). OQ3 (b)
would add A3-tool (H:2649) as a fourth; OQ4 changes no disposition.

## 3. Answers to the design questions the inventory raised

1. **Same terminal content, opposite error presence** (`failTimedOutLoop` vs H:3017-3034). One answer, § 2 C: with a
   populated terminal present the error is the cause, not the disposition; the max-iterations shape carries none because
   the handler completed its work. Rows C1/C2 and B7.
2. **`settleFailedToolResult` lacks `!terminalOwnedElsewhere`.** Not a defect — handled earlier: P3. A guard result is
   returned with nil on all three producers (and unchanged through ARH:169), so the conjunct is vacuous on every lane.
   The one decision keeps it so the three sites read identically; the tool lane gains it as a no-op.
3. **`handleLoopFailure` ignores the handler's result.** The consolidated owner does NOT take the handler's result on
   the model lane (row C2, P4). #1380 changed the one thing that made the rebuilt event disagree with the handed one —
   the reason (`handler_error` → `timeout`, `errLoopTimedOut`) — so after #1380 the two events differ only in `Error`
   text (`failTimedOutLoop` writes the bare sentinel text, H:3375; `handleLoopFailure` writes `err.Error()` of the
   `WrapFatal`, C:2088 → `pkg/errs/errs.go:108`) and in the commit context (`handleLoopFailure` detaches to a 5s budget,
   C:2098-2100; the carrier uses the delivery context). Taking the handler's result would change both — a rejected
   alternative (§ 9), not a row.
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
  C:2659 `if failedTerminal(result, cause) {`. Everything after the guard at each site is unchanged: the approval lane
  maps the commit's error to a decision (ARH:222-233), the sweeper logs (AS:123-127), the tool lane returns it. What
  the predicate unifies is exactly the failed-terminal reading; every other refusal keeps its lane's disposition
  (rows A, D) — the model lane keeps its sentinel switch and `handleLoopFailure` (row C2).
- **`persistHandlerResult(ctx, result)`** loses `order` (C:2245). Its body keeps the terminal branch first (C:2278), then
  `if !gated { return c.publishThenPersistResultState(ctx, result) }` and the write-then-publish tail for a gate
  (C:2311-2330). `carrierOrder`, `writeThenPublish`, `publishThenWrite` (C:2193-2206) are deleted; the doc comment's
  order contract (C:2209-2244) is rewritten to say the shape decides. Birth (task lane, C:1674/C:1716), gate, ordinary
  and terminal orders are preserved exactly — P1 shows no non-terminal caller ever passed `writeThenPublish`.
- **Boundary validation: none by default.** No produced row is a must-not-occur combination (E1 has no production
  trigger, OQ1). If the owner takes OQ1 (b), the check lives in `commitTerminal` (TO:151 (de1413d7)), not the carrier —
  § 0 OQ1 names the precedent, the carried error and the two dead counting arms. OQ2 (b) would be two producer-side
  `errs.WrapFatal` (ARH:85, ARH:139), recommended against.
- **No new exported API.** `HandlerResult`, its fields and the four `Handle*` signatures are untouched (Tier 1);
  `agentic` untouched.

**Why a predicate and not a four-way classifier.** The other transitions already have one home each: the guard is
`settleTerminalGuard` (four callers plus the C:2251 backstop), the applied case is `persistHandlerResult`, and the
refusal is each lane's own class map (P6) — deliberately different per lane (the tool lane force-quarantines a
post-mutation error, the model lane commits a failure, the approval lane classifies). An enum would add a type whose
only consumer is the three sites that need one bit, and would have to encode three lane-specific refusal rules it
cannot own.

## 5. Tests

**Keep pinning the orders, unchanged (14, from the inventory § Tests):** `TestAnApprovalGateIsWrittenBeforeItsEventIsPublished`
(loop_carrier_test.go:152), `TestTheApprovalTimeoutSweepNamesTheRequestItPublished` (:226),
`TestBirthRefusesASecondCreateForTheSameLoop` (:347), `TestBirthWhosePublishFailsIsNotAcknowledged` (:378),
`TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks` (persist_handler_result_test.go:208),
`TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged` (:252), `TestTerminalOwnerArms` (terminal_owner_test.go:72),
`TestCancelTakesTheTerminalOwnerAndClearsAPendingApproval` (:186), `TestLoopFailureTakesTheTerminalOwnersOrder` (:226),
`TestAResponseMeetingAnUncommittedTerminalWritesNothing` (:306), `TestAResponseMeetingACommittedTerminalIsAcknowledged`
(:324), `TestAToolResultForATerminalLoopTouchesNothing` (:376), `TestAnApprovalWhoseRecordMovedIsRetried` (:410),
`TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains` (publication_semantics_integration_test.go:125). The
fifteenth pinned test, `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` (loop_carrier_test.go:71), **changes**
(below). The failed-terminal row's lane tests stay: approval_loop_deadline_test.go:61/:82/:110/:143/:219,
approval_timeout_recovery_test.go:50, plus #1380's terminal_metrics_test.go (one per lane through the production entry).

**Change with the parameter (9 call sites in 8 files, inventory § carrierOrder):** loop_carrier_test.go:76/:87,
persist_handler_result_test.go:69/:224, publish_phase_fatal_test.go:48, terminal_owner_test.go:399,
trajectory_eviction_internal_test.go:31, task_redelivery_integration_test.go:187,
tool_result_redelivery_integration_test.go:216. `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` drives both
orders on a NON-terminal result through the parameter; its second subtest ("write then publish commits the record",
:84-92) is only reachable by a gate-shaped result once the parameter is gone, which
`TestAGateIsWrittenBeforeItIsPublishedWhateverItsLaneAsks` already asserts — the subtest is rewritten to a gated result
or folded into that test, and both names lose "order" / "whatever its lane asks".

**New:** a table-driven test of `failedTerminal` over the four axes (state × terminal payload × owned-elsewhere × error
nil/plain/fatal) — exhaustive, every row named. Under OQ1 (b) only: a held loop whose event build fails is quarantined
with no record written, through `commitTerminal` from both the carrier and `handleLoopFailure` (the counterexample is
today's record-then-Fatal on the `handleLoopFailure` path, probe-confirmed).

**Mutation evidence (wiring, not primitive):** delete the `failedTerminal` call at ONE site and run that lane's
failed-terminal test (approval: approval_loop_deadline_test.go:82; sweeper: :143; tool:
terminal_metrics_test.go `TestAToolLaneTimeoutCountsOneTimeoutFailure`) — each must go red on its own site; `cp`
backup + checksum per the testing policy.

## 6. Invariants and the PBT decision

| Invariant (holds for every pair the loop produces) | Spec home |
|---|---|
| I1 A delivery whose handler produced a terminal is acknowledged only after the terminal owner's four steps land. | S:910-911 (existing); S:1489-1491 (existing) |
| I2 The same result shape takes the same carrier order on every lane that produces it (birth, gate, ordinary, terminal). | the MODIFIED requirement's new sentence (delta) |
| I3 An error accompanying a terminal-carrying result is that terminal's cause; its class is never read for the disposition, on any lane. | the MODIFIED requirement's new sentence (delta) |
| I4 (OQ1 (b) only) A terminal state with no terminal event is never written as a terminal record. | would be a sentence in the delta; absent under (a) |

**PBT decision** (docs/contributing/01-testing.md § When to Use Property-Based Testing): the input is a finite product
of four small axes, not a grammar or a history; an exhaustive table (§ 5) enumerates every class and is stronger than a
sampled property. No Rapid property; the order invariants I1/I2 are exercised by the named order tests above, which
drive real publish and write failures at the seam. Targeted mutation evidence: § 5.

## 7. Problem shape (contract § 5) and the precedent adopted

Shape: **interpret a `(value, error)` pair once, at one owner, with the invalid pairs refused loudly.** Closest existing
instance, on the delivery plane: `interpretDeliveryWork` (DS:399-421) — the closed `(decision, cause)` tuple, where Ack
with a cause or Retry/Terminate/Quarantine without one is quarantined as `InvalidDeliveryDecisionError` rather than
coerced. #1376's issue text names it. **Adopted for the one reading this change unifies:** the table is the closed set
of produced pairs and `failedTerminal` is the one reading of the failed-terminal pair. The "refused loudly" half has no
produced invalid pair to apply to (E1, OQ1); it is what OQ1 (b) would add, in `commitTerminal`. No pattern is
established (the predicate is local to the loop owner), so no adoption sweep is owed.

Decision skills: `kv-or-stream` — not triggered (no new communication path); `orchestration-check` — not triggered (no
multi-step behaviour added; the terminal owner's four steps exist); `new-payload` — none; `query-pattern` — none.

## 8. Adopter seam

The surface reached from outside this repo is the wire and the KV record, not the Go pair: P7 measured zero sister
callers of `HandlerResult` / `Handle*` at the SHAs #1380's migration note pins. The per-row **Watcher sees** column in
§ 2 is this seam stated as observation: for every produced pair, which of `COMPLETE_<loopID>`, the event, the record
state and the delivery decision a watcher of `agent.*.<loopID>` / `AGENT_LOOPS` / the loop metrics observes. For a
sister that reads those:

1. **What must they know?** Nothing new: no wire, KV, subject or metric changes under (a) on every question. Under OQ1
   (b) they lose a shape no production path produces; under OQ2 (b) a released-loop approval race latches the lane
   instead of recovering cold.
2. **If they do nothing?** Same observations as today.
3. **Where do they find out?** Not applicable under (a); under a (b), the health line `delivery ownership lost` —
   runtime, not doc.
4. **What SHOULD they know?** Nothing. The gap between 1 and 4 is empty for this change; the wider adopter seam on the
   loop vocabulary is #1314's (RC) and is not touched.

For a Go caller of the exported handlers (none today, P7): the transitions are private (`terminalOwnedElsewhere` is
unexported), so an outside caller cannot tell a guard result from an applied one. That is a pre-existing Tier 1 shape
question for #1314, recorded here, not designed.

## 9. Costs and rejected alternatives (docket order)

| # | Alternative | Cost / why not |
|---|---|---|
| 1 | **Table only, no code change.** The MODIFIED requirement lands; the three guards and `carrierOrder` stay, documented. | Cheapest; leaves the copied decision as three sites a fourth lane can miss (the L4b class) and a dead parameter that reads as a lane choice. First docket row per the 2026-09-22 rule; the predicate and the parameter deletion are still worth their ~40-line diff on their own, with zero behaviour change. |
| 2 | **Predicate + parameter deletion (recommended § 4), (a) on every owner question.** | ~40 lines production, 9 test call sites, 1 test rewrite; behaviour-preserving (0 changed rows). |
| 3 | Four-way `transitionKind` classifier returned by one function, called by all lanes. | A type with one consumer, and it would have to own three lane-specific refusal rules that are deliberately different (§ 4). |
| 4 | The model lane commits the handler's populated result (drop the `handleLoopFailure` re-derivation for C2). | Changes the model lane's `LoopFailedEvent.Error` text and its detached commit budget (§ 3.3); #1366-approved behaviour, ruling 5 says preserve it. Worth an owner question only if the `Error` text difference is found to matter. |
| 5 | Payload-defined terminal test (`FailureState != nil \|\| CompletionState != nil`) in the predicate instead of `State.IsTerminal()`. | Two definitions of "terminal" instead of one (the carrier's C:2246 is state-based); routes the un-produced E1 shape through three lane class maps instead of one owner. |
| 6 | Boundary check at the carrier's terminal branch (C:2278) — this design's first draft. | Rejected on the reviewer's probe: `handleLoopFailure` (C:2102) bypasses the carrier and writes the forbidden record; the one owner is `commitTerminal` (OQ1 (b)). |
| 7 | Remove `RetryScheduled` / `MaxIterationsReached`. | Tier 1 incompatible change — OQ4. |

## 10. Inventory corrections and the NOT RUN items

- P9: H:1316's first annotation (record-classification) was wrong; it is `GetLoop` — corrected in the inventory at
  `4f19e119`. Pin text was always correct.
- S:886 is the settlement requirement; pinned at `4f19e119` (S:1408 is off-territory); the delta modifies S:886 and
  cites S:1467 unchanged.
- The inventory's "Tests that pin the orders" lists 15 tests; 14 are unchanged by this design and one
  (`TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind`, loop_carrier_test.go:71) is rewritten (§ 5).
- NOT RUN 1 (`handleToolCallResponse` body): closed by P8 — no terminal shape, one row (D1).
- NOT RUN 2 (`test/` suites for order-pinning tests): not needed by any row; the pinned tests are in
  `processor/agentic-loop`.
- NOT RUN 5 (sister mentions): closed by P7 — zero hits, read-only, SHAs recorded.
- Design-review round 1 (at `4f19e119`, 2026-09-25) corrected rows A8, D3 and E1 against the reviewer's probes; the
  first draft's OQ1/OQ2 (b) recommendations are withdrawn, and the carrier-branch check is rejected (§ 9 row 6).
