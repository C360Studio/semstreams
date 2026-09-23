# Reconciliation — accepted L4 package (`68c14c8e`) against `main` (`b7ce8727`)

Verdict: **SEMANTIC.** The accepted design's positive content (one field, publish-then-CAS, applied set = `PendingToolResults` keys, identity adoption, terminal adoption by marker) stands unchanged; what changes is *where it attaches* and *what it replaces*. `main` never carried the Codex recovery layer, so the design's § 6 inverts: its "survives" list is a build list and its "deleted" list has one target (`IncrementTruncationRetry`/`ResetTruncationRetry`). Three main-only facts change the meaning of a ruling's premise rather than its pin: (1) no lane holds a KV revision — every writer is `Put` and discards the revision it is returned; (2) birth and the sweeper publish before they write, every other lane writes before it publishes, and the three terminal paths are three `Put`s with no Create-once marker; (3) the advance already drains the applied set before minting. Nothing below redesigns; every addition is an owner question with a recommendation first.

Counts over the 49 predecessor decisions + 7 predecessor KV write sites (56 rows): **unchanged 9 · mechanical 7 · semantic 11 · no target 29.**

Abbreviations as in `inventory.md`. `SR:` = `settlement_recovery.go`, exists only at `68c14c8e`. Every `main` line is at `b7ce8727` and appears as a pin in `inventory.md` § 7 (verifier `EXIT=0`).

## A. One row per recovery decision and per KV write site

Effect legend — **unchanged**: the site exists on `main` and the design's treatment applies verbatim; **mechanical**: same site, new line or name, treatment applies with a pin swap; **semantic**: the treatment's meaning changes on `main` (replacement sentence given); **no target**: nothing on `main`; the cell names what § 5 attaches to instead.

| Row | `68c14c8e` site | `main` equivalent | Effect | Replacement sentence / attach site |
|---|---|---|---|---|
| D1 | `C:1310-1311` | NONE — `C:1396` `HandleTask` creates the loop in memory; `C:1345` reads the process map only | no target | § 5.1 attaches a cold fork before `C:1396`: read `loopsBucket` by loop ID; present → § 5.1 steps 2-4; absent → birth |
| D2 | `SR:384-397` | NONE | no target | the same fork reads `Iterations` (`AG:55`) and `PendingToolResults` (`AG:57`) |
| D3 | `SR:398-400` | NONE (pattern: `LP:91-92`) | no target | the same fork; terminal → ACK with the Q7 audit line |
| D4 | `SR:402-422` | `H:1120-1174` `buildTaskRequest` (mint `:1122`, MsgID `:1174`) | mechanical | the R1 rebuild is the birth path itself; republish rides `publishResults` `C:2318` |
| D5 | `SR:424`, `ST:349-430` | NONE (L2 residual `stable-request-identity/design.md:186`) | no target | build `restoreLoopFromRequest` in `state.go` beside `attachContinuation` (`ST:298`); entered from the cold arms `C:1700`, `C:2292`, and the approval lane's new cold branch (D30) after step 0 |
| D6 | `C:1339-1350`, `C:1406-1409` | `C:1429-1437`, `C:1510` | unchanged | — |
| D7 | `C:1532-1541` | NONE — a birth failure goes `C:1596` → `C:1752` (`Put`) | no target | the terminal owner (D41); whether birth uses `Create` is owner question OQ3 |
| D8 | `SR:457-476` | `C:1679`, `ST:1380` | unchanged | — |
| D9 | `SR:478-492` | `C:1698-1700` → `LP:66-94` | mechanical | live → Retry today (`C:1700`); § 5.2's cold path replaces that arm after step 0 |
| D10 | `SR:494-513` | NONE (no warm KV re-read) | no target | the CAS `Update` in the carrier (`C:2483` → `KV:231`) detects the moved record |
| D11 | `SR:517-535` | `H:1253` `CurrentRequest` (process map, empty after replacement — L2 residual `ST:955-961`) | **semantic** | "The superseded-response guard at `H:1253` compares `response.RequestID` with the record's `PublishedRequestID`; an empty process map after replacement is no longer a let-through." |
| D12 | `C:1569-1586` | `H:1322` (warm), `C:1700` stale arm (cold) | mechanical | Q7's metric + audit line added at both |
| D13 | `H:1249-1264` | `H:1322-1337` | unchanged | — |
| D14 | `SR:131-195` | NONE; `GD:727` publishes proposals without a MsgID | no target | recommended NOT built (OQ: none — the declared residual "duplicate proposed/verdict pair" covers it; building it adds a read the design does not need) |
| D15 | `SR:197-235` | NONE | no target | with D14 |
| D16 | `GD:547-560`, `C:2585-2595` | `GD:608-629` (`ErrNoGovernanceWaiter` `GD:337`); `C:2734` → `C:2759-2780` (stale → Ack `:2773`, live → Retry `:2780`) | **semantic** | "On a missing waiter the verdict lane reads the record: `verdict.RequestID` older than `PublishedRequestID`, or `ExecutionID ∈ PendingToolResults`, or terminal → Ack with a reason on the missing-waiter counter (`M:393`); current and unseen → Retry (`C:2780` unchanged)." |
| D17 | `C:2146-2148` | `C:2173` | unchanged | — |
| D18 | `SR:556-569`, `C:2269-2306` | NONE at the cold path; `EI:31` derives | no target | the live arm of `settleToolResultWithoutLoop` (`C:2287-2292`) after step 0; `agentic/tools.go:637-641` carries the identity |
| D19 | `SR:570-576` | `C:2287-2292` | mechanical | — |
| D20 | `SR:577-621` | NONE (pattern `PS:28-49`, `TR:51`) | no target | new retained-response reader (identity only) beside `execution_identity.go`; used by D5/D24/D25 |
| D21 | `SR:622-632` | NONE | no target | dropped (design § 6 deletes it) |
| D22 | `SR:651-665`, `SR:686-740` | NONE; warm path `H:2546` stores by key before any check | **semantic** | "A redelivered tool result is classified against `PublishedRequestID` at component entry (`C:2195`, before `HandleToolResult`) and in the cold live arm (`C:2292`); `HandleToolResult` never sees an older or unknown result." |
| D23 | `SR:634-636` | NONE | no target | nothing to delete; `H:2029` normal truncation unchanged |
| D24 | `ST:473-521`, `ST:486-488` | NONE | no target | build `restoreToolBatch` inside D5 without a decrement (on `main` the advance is written after the PubAck, so a retained R(N+1) proves the advance) |
| D25 | `ST:435-468` | NONE | no target | inside D24: stored keys ⊆ the retained response's tool calls (D20), else Quarantine; `requirePreceding` never existed |
| D26 | `SR:666-667`, `SR:821-852` | cold: `C:2296-2303` (`tool_results_dropped_total{stale_execution}`); warm: NONE before `C:2195` | **semantic** | "The cold terminal drop at `C:2296-2303` is already Q7's effect-free ACK; the warm half is inserted before `HandleToolResult` at `C:2195`, because `StopLoop` (`H:2580`) and `StoreToolResult` (`H:2546`) both precede the lane's only terminal guard (`H:2652`)." |
| D27 | `SR:637-650`, `SR:763-817` | `H:2573` → `H:2667-2702` (gate path); `H:2704-2723` writes the gate | mechanical | the gate-consumed case is answered by D22's older classification (design § 5.4 step order holds) |
| D28 | `SR:669-670`, `SR:743-758` | `H:2674` absorbs silently | no target | § 5.4's re-echo: in the awaiting branch at `H:2674`, when `PendingApproval.ExecutionID` matches, re-emit the `ApprovalPendingEvent` (gate keeps write → publish) |
| D29 | `ARH:176-181` | `ARH:54-58` (`ResolveApprovalIfPending`, `ST:499`) | unchanged | warm path |
| D30 | `ARH:182-215` | NONE — `!ok` at `ARH:58` → `staleDrop` (`ARH:78`) → Ack (`ARH:194-199`) | no target | § 5.5 attaches a cold branch at `ARH:58`: on `ErrLoopNotFound` read the record (+ revision), step 0, then classify by gate identity |
| D31 | `SR:857-866`, `SR:1015-1032` | NONE | no target | D30's branch: I4 + key presence |
| D32 | `SR:867-899`, `SR:1034-1040` | NONE | no target | D30's branch: I4 |
| D33 | `SR:921-981` | NONE (`continuation_unavailable` has 0 hits outside this change) | no target | **OQ1** (Q8 has no referent) |
| D34 | `SR:904-916` | NONE | no target | D30's branch → D24 rebuild |
| D35 | `ARH:238-241` | `ARH:194-199` | **semantic** | "An in-memory loop that is not awaiting stays an Ack (W3); `ErrLoopNotFound` no longer stale-drops — it enters D30's cold branch, and only a record that is absent or terminal is acknowledged." |
| D36 | `ARH:255-279` | `ARH:205` → carrier `C:1947` (write) → `C:1959` (publish) | mechanical | the order is the carrier's (P3); the reject-minted W4 exists on `main` only after task 2.1 reorders the carrier (today the crash window is the inverse: record advanced, R(N+1) unpublished) |
| D37 | `ARH:130-149` | `ARH:139-158` (`RequestID: pending.RequestID` at `:149`) | unchanged | — |
| D38 | `AS:21-86` | NONE — `AS:69` reads memory only | no target | **OQ2** (startup hydration of pending approvals) |
| D39 | `AS:134-169` | `AS:65-129`: publish `:100` → `Put` `:101` (error ignored) → wire `:130` | **semantic** | "The timeout sweeper's own publish-then-`Put` pair (`AS:100-101`) is replaced by the carrier (`persistHandlerResult`, `C:1923`) so the auto-reject takes the same publish → `Update` order and the same CAS as an operator rejection." |
| D40 | `C:2508-2526` | `C:2593-2612` | unchanged | — |
| D41 | `C:1833-1845`, `C:1959-1964`, `C:1917-1927` | three paths, all `Put`, no marker identity: carrier `C:1974-2023` (entity `:1975` → marker `:1982`/`:2002` → stamps → publish `:1959`); failure `C:1752` → `:1809` → `:1831`; cancel `C:2631` → `:2668` → `:2683` | **semantic** | "The three terminal write paths become one owner: `COMPLETE_` marker by `Create` (`KV:211`; `ErrKVKeyExists` `KV:218` → read it back and adopt), graph stamps, publish, then the entity by `Update(revision)` (`KV:231`); the marker's Create-once is the identity the beta.57 ordering (`C:1798-1803`) keyed on." |
| D42 | `TR:65-110` | `TR:64`, `:89`, `:147` | unchanged | — |
| D43 | `PS:76-122`, model `:616-627` | `PS:76-89`, model `:633-643` | unchanged | — |
| D44 | `C:1833-1843` | NONE | **semantic** | "Q7(a) sits at `C:2195` (warm) and `C:2296` (cold), not inside a terminal owner; the TaskID-versus-marker Quarantine is dropped because no warm read exists to feed it." |
| D45 | `C:2162-2170` | NONE | no target | dropped; the process-retained revision (S3) is the CAS input |
| D46 | `C:2171-2177` | NONE | no target | dropped |
| D47 | `C:2178-2181` | NONE | no target | → Q7 at `C:2195` |
| D48 | `C:2182-2190` | NONE; identity written at `H:2710-2713`, resolved at `ST:499` | no target | I4 is new; checked in D30's branch |
| D49 | `C:1846-1870` | NONE; markers are `Put` (`C:2401`, `:2432`, `:2456`) | no target | D41's owner: § 5.7(b) reads the marker `Create` refused |
| P1 | `C:1405 → C:2396-2415` (birth: `Put` then publish) | `C:1496` publish → `C:1499` `Put` via `persistLoopState` (`C:2468`, `:2483`), error ignored | **semantic** | "Birth becomes `Put` → publish (Q1): `persistLoopState` moves ahead of `publishResults` at `C:1496-1499` and its error returns Retry — converting the first of #1345's five task-intake branches by necessity; the other four stay #1345's." |
| P2 | `C:1532` (`Create` on graph-birth failure) | NONE | no target | D41 |
| P3 | `C:1795 → C:2411` inside `persistHandlerResult` (`Put` → publish) | `C:1947` → `C:1959` (`Put` at `:2483`) | mechanical | task 2.1's sentence applies verbatim with new lines: non-terminal results publish first, then `Update(revision)` |
| P4 | `C:2319` `persistApprovalGate` (`Update` → publish gate event) | NONE — the gate is a carrier result (`H:2573` sets awaiting; `C:1947` → `:1959`) | no target | § 5.4 attaches to a carrier branch keyed on `result.State == awaiting_approval` that keeps write → publish; uniform order is **OQ8** |
| P5 | `ARH:275` (`Update` after publish `:269`) | `ARH:205` → carrier (`Put` `:1947` → publish `:1959`) | **semantic** | "The approval lane's order is inverted on `main` (write, then publish); the carrier reorder in task 2.1 gives it the accepted publish → `Update` order, which is what creates the reject-minted W4 that § 5.5 and task 4.2 handle." |
| P6 | `C:1927` `persistTerminalOutcome` (`Update`; marker `Create` `:1960`; publish `:1917`) | three `Put` paths (D41) | **semantic** | as D41 |
| P7 | `SR:961-976` `settleAbsentApprovalEvidence` | NONE | no target | OQ1 |

Main-only writers with no predecessor row: `C:1425` (deferred continuation, `Put`, error ignored; a CAS loss here is placed comment #2's window — § D 2); `AS:101` (D39). **Amended 2026-09-23 (round 3, finding 2):** the deferred continuation does NOT ride the carrier's `Update` — it renders the live entity, which at that instant carries the tool lane's unpublished advance — so it gets its own record-overlay writer (`persistDeferredContinuationMarker`), keeping the CAS and the OQ3 fence.

## B. Design § 6 rows on `main`

Deleted list: `toolResultProvenInLaterRequest`, `approvalRequiredResultSuperseded`, `proveTerminalToolResultApplied`, the `ensureResponseLoop`/`recoverToolResult` compares, `validatePendingApprovalRequest`, compare-only truncation, `Iterations--`, `requirePreceding`, the terminal compares and terminal-at-revision Retry — **never built on `main`; nothing to delete.** The one deletion with a target: `IncrementTruncationRetry` (`ST:466`) / `ResetTruncationRetry` (`ST:477`) and their callers `H:2037`, `H:1389`, `H:1409`, plus the map cleared at `ST:588` — delete stands (task 2.4). Tests named as "deleted or rewritten": `settlement_recovery_test.go`, `tool_result_recovery_test.go`, `terminal_tool_recovery_test.go`, `tool_result_redelivery_integration_test.go`, `terminal_tool_redelivery_integration_test.go`, `settlement_recovery_integration_test.go` — all ABSENT; "rewrite" becomes "write".

Survives list, disposition on `main`:

| Predecessor item | `main` | Disposition |
|---|---|---|
| evidence reader + addresses (`SR:20-127`) | ABSENT | build (one interface, two reads: retained request, retained response; pattern `PS:21-49`, `TR:51`) |
| `recoverGovernance` + `readRetainedGovernanceVerdict` | ABSENT | recommend not built (D14) |
| `readLoopEntity[Revision]` | `LP:75` reads the entity and discards `entry.Revision()` | build a revision-returning read; `classifyMissingLoop` keeps its signature |
| `readRetainedAgentRequest/Response` | ABSENT | build (task 2.3 / D20) |
| `loopIDFromRequestID` | TWO prefix-only copies: `ST:1360-1367` and `LP:50-60` | task 1.0's `Parse` replaces both readers (touches L3's file — mechanical) |
| `recoverTaskDelivery` | ABSENT | build as the cold fork before `C:1396` (D1-D4) |
| `republishPendingApproval` | ABSENT | build inside D30's branch |
| `settleAbsentApprovalEvidence` | ABSENT | OQ1 |
| `validatePendingApprovalResult/Evidence` | ABSENT | I4 check, new, inside D30's branch |
| `loopSettlementDecision` | ABSENT; decisions are `natsclient.DeliveryDecision` returns in the handlers (`C:2780` shape) | no helper needed; each lane returns its decision in place |
| `restoreLoopFromRequest` (`ST:349-430`) | ABSENT | build (D5) |
| `restoreToolBatch` minus the decrement | ABSENT | build (D24) |
| `execution_identity.go` | PRESENT (`EI:24-36`) | survives |
| `delivery_owner.go` | ABSENT — replaced by `internal/deliverylane` (`DL:27-225`; `C:1108`, `:1130`) | drop from the list per the archived #1341 design (`:377-381`); L4 adds no latch spelling |
| `approval_sweeper.go` | PRESENT, memory-only snapshot (`AS:69`) | survives; its write pair rides the carrier (D39); hydration is OQ2 |
| `agentic-dispatch/task_recovery.go`, `agentic-model/provider_settlement.go`, shared handlers | PRESENT | survive |
| the single terminal owner (`C:1802-1931`) | ABSENT (three paths) | build (D41) |
| e2e harness: `processbarrier`, `stage_a_process_replacement.go` | PRESENT | survive; assertions re-pointed at KV facts |
| e2e `approval_restart.go` / `approval_restart_test.go` | ABSENT | write new (task 6.2) — depends on OQ1/OQ2 |

## C. Rulings' premises re-verified on `main`

- **Q1 (birth Put → publish; unconditional R1 republish at iteration 0).** Premise "birth already keeps Put → publish" is FALSE: `C:1496` publishes, `C:1499` writes, the write error is ignored. Ruling stands; the implementation reorders and honors the error (P1). The task lane has no cold fork at all today (D1), so § 5.1 steps 2-4 are built, not re-pointed.
- **Q2 (`Update(observedRevision)`).** Premise "both lanes already hold the revision" is FALSE: zero `Update`/`Create` calls under `processor/agentic-loop`; the four writers are `Put` (`C:2401`, `:2432`, `:2456`, `:2483`) and discard the returned revision (`KV:194` returns it); the one reader `LP:75` discards `entry.Revision()`. Which method: `KVStore.Update(ctx, key, data, revision)` at `KV:231`, `ErrKVRevisionMismatch` at `KV:238`. Which revision: the process retains, per loop, the revision returned by its own last write (seeded at birth from `Put`/`Create`'s return; on a cold read from `entry.Revision()`). On mismatch the lane returns Retry AND releases the loop's process state (`ST:574` `DeleteLoop` / `C:1964` `releaseLoopTransientState`) so the redelivery re-enters the cold path against the record that won — without the release the loser keeps a stale in-memory loop forever (the amendment in § D 2).
- **Q3 (applied set = `PendingToolResults` keys).** Holds: `AG:57`, written `ST:1075-1082`, read at `H:2546`. New on `main`: the advance drains the set (`H:2850` → `H:2870` → `ST:1107-1121`, `nil` at `:1121`) BEFORE the mint at `H:2927`, so the normal path writes an empty set at the advance. Pass 3's MEDIUM (rationale for `PendingToolResults = nil` in step 0) reverses: on `main` the design's original sentence ("the shape the normal path leaves at the advance") is TRUE. Mechanical doc fix in § 3.6; the mechanism is unchanged and every reader stays gated behind the older-RequestID classification.
- **Q4 (grammar; no parser).** Confirmed: `ST:1347` `%s:req:%d:%d`; iteration part = `Iterations + 1` (`ST:1345`), retry part process-local (`ST:466-482`); readers split only the loop-ID prefix (`ST:1360`, `LP:50`). Consequence for § 3.6/task 2.5: the adopt writes `Iterations = parsed iteration − 1`, not "N+1" in the record's own units; the design's R(N)/R(N+1) notation is relative and stays.
- **Q5 (`PublishToStreamWithMsgID`).** SHIPPED by L2: `C:2328` routes every published message through it; `MsgID = RequestID` at all three mints (`H:1174`, `:2194`, `:2958`); `natsclient/client.go:963`. Other stamping sites for the record: tools plane `TC:780`, `TC:1224`; dispatch `terminal_settlement.go:272`. Task 2.2 is complete before L4 begins.
- **Q6 (verdict ACK path).** Attach point exists: `settleVerdictWithoutWaiter` `C:2759` — stale → Ack `:2773`, live → Retry `:2780`. The live arm gains § 5.6's classification (D16). Metric home: `RecordGovernanceVerdictMissingWaiter` `M:393`.
- **Q7 (terminal + unproven → effect-free ACK, metric, audit).** Cold half exists (`C:2296-2303`, `stale_execution`). Warm half must precede `HandleToolResult` at `C:2195`: `StoreToolResult` (`H:2546`) and `StopLoop` (`H:2580`) both run before the guard at `H:2652`, which is exactly L2's placed comment #3. Cancel already has the pattern (`C:2593-2612`).
- **Q8 (`settleAbsentApprovalEvidence` unchanged).** NO REFERENT on `main`; #1146's acceptance line 83 ("confirmed missing required evidence durably fails `continuation_unavailable`, including before the approval deadline") is the behaviour the e2e stage in task 6.2 asserts. → OQ1.
- **Terminal-outcome adoption (owner "confirmed").** Attaches to D41's owner; the marker must be `Create` for (b) to have anything to read back. Also lands L3's deferred item: clear `PendingApproval` on the terminal transition (archived L3 design `:50`).

## D. The three placed inventory comments (OWNER QUESTIONS; recommendation first)

1. **Task-lane Quarantine → Retry precondition on `tasks_submitted_total`.** Recommendation: **own placement, dispatch-scoped, not absorbed into L4.** On `main` the counter is agentic-dispatch's (`processor/agentic-dispatch/metrics.go:112`, `recordTaskSubmitted` `:322`) and L4's task lane has no Quarantine arm to convert (a birth failure is a terminal via `C:1596` → `C:1752`). Absorbing it would make L4 edit a second component's metric semantics for a condition L4 does not create. Alternative: absorb as a one-line precondition in task 3.4 — only if the owner wants the counter's meaning fixed in the same PR.
2. **Two-consumer `…:req:N:0` window, closable by `PublishedRequestID` under `Update(revision)`.** Recommendation: **absorb with amendment** — "A CAS failure on the carrier releases the loop's process state (`ST:574`, `C:1964`) and returns Retry; the redelivery re-enters the cold path against the winning record." Without the release, the losing consumer keeps an in-memory loop whose `PublishedRequestID` the record never named. Sub-question: birth by `Create` (`KV:211`) rather than `Put`, so the second consumer's birth is refused with `ErrKVKeyExists` and takes the cold fork — recommended, one line, and it is the only way the window closes at iteration 0 where no revision exists yet. Alternative the owner may prefer: per-loop serialization at admission (a latch spelling), which #1341's design reserves for the lane package and L4 must not introduce.
3. **`complete → complete` replay of a duplicate StopLoop result.** Recommendation: **absorb with amendment** — the check lives at component entry before `HandleToolResult` (`C:2195`; Q7 warm half, D26), and `TransitionTo`'s same-state `nil` at `AG:181` stays untouched (it is a legitimate no-op for other callers). Alternative: guard inside `HandleToolResult` ahead of `H:2546` — same effect, but it leaves the store-before-check shape in place for the next lane.

## E. The 2026-09-21 metering ruling

Where it lands: `activeLoop` (`processor/agentic-dispatch/http_activity.go:321`) builds the `loop_route_ambiguous` refusal at `:334`; it is called from both lanes (`agentic-dispatch/component.go:893`, `:1120` and `http.go:289`, `:389`). Task line (new **3.9**): "Meter `loop_route_ambiguous` on `loop_admission_refusals_total` (`agentic-dispatch/metrics.go:180`, labels `seam`,`reason`) inside `activeLoop` at `http_activity.go:334`; extend the counter's Help; update the `commands.go:71` comment that names the pinning test; rename `TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate` (`command_target_resolution_test.go:313`) and retire its two post-refusal absence assertions — `:335` (delivery lane) and `:349` (HTTP lane) — replacing each with a positive count of one; the HTTP 409 assertion at `:346-348` and the pre-refusal isolation `require.Zero` at `:319` stay." Which half retires: the *absence* half is the assertion that `loopAdmissionRefusals` has no series after the refusal (`:335`, and `:349` because the meter sits in the shared resolver). Sub-question (OQ6): the counter's `seam` label — recommend a single value naming the resolver (e.g. `route`) set inside `activeLoop`, matching the ruling's placement; the alternative (meter at the four callers with each lane's seam) doubles the sites and is not what the ruling says.

## F. Spec delta MODIFIED block vs live `openspec/specs/agentic-loop/spec.md:201-226`

`diff live_req.txt delta_req.txt` (scratchpad): the requirement title and first paragraph are byte-identical; the ADR-088 paragraph is byte-identical; scenario "A poison-exhausted message does not freeze the in-flight answer" and scenario "A crashed process does not read as work in flight" are restated verbatim. The delta adds exactly one paragraph (delta `:113-116`, the new SHALL NOT naming `published_request_id` and `pending_tool_results`) and one scenario (delta `:130-136`). openspec 1.7.0's "restate every scenario" rule is satisfied. ADDED-requirement scenarios re-read against `main`: "W4, tool lane" and "cold replacement adopts" describe the post-2.1 order (target state, not today's); "task redelivered at iteration zero" holds under Q1 with `Iterations = 0` ↔ `…:req:1:0` (§ C Q4); "pending_approval names request_id = published_request_id" holds by construction on `main` (`H:2713` stamps the current request's ID; `AG:215` sets none). No scenario needs a wording change; the metric named "the inapplicable-result metric" is unnamed in the spec, so the § A/task 3.5 substitution (reason values on `tool_results_dropped_total`) does not touch the delta.

## G. The L1 breaker, restated

#1327 body line 17: "PR opened, reviewed and merged within the 7-day / ~100-file breaker; over either limit is a stop-and-split ruling, not a waiver." Sizing on `main`: about 30-40 files (seven agentic-loop sources, `agentic/state.go`, the new `looprequest` package, one reader file, roughly nine new or extended test files, two e2e stage files, four agentic-dispatch files for task 3.9, two docs, the spec sync) — inside the file limit; the 7-day limit is the binding one because nine "survives" items are builds, not edits. Natural split line if it trips: **L4a** = tasks 1.x, 2.1, 2.3-2.5, 3.1, 3.2, 3.4, 3.5, 3.8, 4.1, 4.3(a-c,e) (field, carrier order + CAS, cold rebuild, task/response/tool lanes); **L4b** = 3.3, 3.6, 3.7, 3.9, OQ1/OQ2, 4.2's approval and terminal cases, 4.3(d,f), 6.2's approval stage (approval lane, verdict, terminal owner, hydration). → OQ7.

## H. Codex-branch facts with no home on `main` (dropped from the inventory; listed here so nothing is silently lost)

`settlement_recovery.go` entire (evidence reader, `readLoopEntity[Revision]`, `readRetainedAgentRequest/Response`, `recoverTaskDelivery`, `ensureResponseLoop`, `recoverToolResult`, `recoverApprovalResponse`, `republishPendingApproval`, `settleAbsentApprovalEvidence`, `validatePendingApproval*`, `loopSettlementDecision`, `recoverGovernance`, `readRetainedGovernanceVerdict`, the three layout proofs, the compare-only truncation); `state.go` `restoreLoopFromRequest`, `restoreToolBatch`, `requirePreceding`, `validatedToolBatchResults`, `Iterations--`; `component.go` `persistTerminalOutcome`, `selectTerminalOutcome`, `persistApprovalGate`, the warm KV checks `C:2162-2190`, the birth `Create` `C:1532`; `approval_sweeper.go` `restoreApprovalDeadlines`; `metrics.go` `approvalDecisionsInapplicable`; `delivery_owner.go` (superseded by `internal/deliverylane`); the six test files named in § B; e2e `approval_restart.go`/`_test.go`.

Pre-existing residual observed, not L4's (record only): a rule-published task with an empty `LoopID` births a second loop after process replacement (`H:824` → `C:1396`); the cold fork of D1 keys on the task's loop ID and cannot see it.
