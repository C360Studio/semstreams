# L4 inventory — #1330 restart recovery, re-derived on `main`

base: b7ce8727a770c9f880049def24abe887545bccbe
role: semstreams-architect, reconciliation pass (read-only; written to the scratchpad, never to the repository)
sources: every `file:line` below is at `b7ce8727` (`origin/main`; the seed commit `ed667305` is its child and touches only `openspec/changes/`). The predecessor inventory (163 pins at `68c14c8e`, the head of PR #1159's Codex branch, never merged) is `openspec/changes/agentic-loop-durable-applied-facts/inventory.md`; its rows are carried below by number (D1–D49) so the reconciliation can cite both. Facts of the Codex branch that have no home on `main` carry no pin here — they are listed as non-pin notes in `reconciliation.md` § H, because every section the verifier parses is strict except `## Searches` (skipped) and `## Adjacent claims` (lenient), and a Codex fact is neither a search nor an adjacent claim.

Abbreviations: ST = `processor/agentic-loop/state.go`, C = `processor/agentic-loop/component.go`, H = `processor/agentic-loop/handlers.go`, LP = `processor/agentic-loop/loop_presence.go`, ARH = `processor/agentic-loop/approval_response_handler.go`, AS = `processor/agentic-loop/approval_sweeper.go`, GD = `processor/agentic-loop/governance_dispatcher.go`, EI = `processor/agentic-loop/execution_identity.go`, M = `processor/agentic-loop/metrics.go`, TR = `processor/agentic-dispatch/task_recovery.go`, PS = `processor/agentic-model/provider_settlement.go`, TC = `processor/agentic-tools/component.go`, AG = `agentic/state.go`, DL = `internal/deliverylane/deliverylane.go`, KV = `natsclient/kv.go`. `SR:` (settlement_recovery.go) exists only at `68c14c8e`.

## 0. Premises measured on `main` before anything else

| Premise (from the accepted design) | Measurement at `b7ce8727` | Result |
|---|---|---|
| "RequestIDs are deterministic (L2)" | `ST:1339-1347`: `%s:req:%d:%d`; iteration part = `entity.Iterations + 1` (`ST:1345`); retry part = process-local `truncationRetryAttempts` (`ST:466-482`, cleared at `ST:588`) | TRUE. The design's premise correction is moot. The iteration part of a record's request is `Iterations + 1` (birth writes `Iterations = 0` beside `…:req:1:0`), so § 3.6's "`Iterations` = its parsed iteration" is off by one on `main` (reconciliation S4) |
| "agentic-model reuses a matching retained response (L2)" | `processor/agentic-model/component.go:633-643`; `PS:76-89` | TRUE |
| "L2 shipped no `<iteration>:<retry>` parser" (Q4, L2 archive) | `ST:1360-1367` splits on `:req:` and returns `parts[0]`; `LP:50-60` takes the prefix before the separator; `git grep -n looprequest` → 0 | TRUE; task 1.0 stands |
| "request publishes stamp `Nats-Msg-Id`" (Q5) | `H:55` `MsgID` on `PublishedMessage`; stamped at the three mints `H:1174`, `H:2194`, `H:2958`; `C:2328` routes every published message through `PublishToStreamWithMsgID` (`natsclient/client.go:963`) | SHIPPED by L2. Task 2.2 is complete before L4 starts (`publication_semantics_integration_test.go` exists) |
| "no applied-ID / published-request field exists" | `git grep -n 'PublishedRequestID\|published_request_id' -- ':!openspec'` → only the forward-reference comment `ST:1338`; `AG:49-113` carries two L2 fields the design predates, `PendingContinuation` (`AG:98`) and `PendingContinuationRequestID` (`AG:113`) | TRUE; step 0 must say what it does with the two L2 fields (reconciliation S6) |
| "`LoopEntity` appears in generated schemas / OpenAPI" | `git grep -l 'pending_tool_results\|published_request_id' -- schemas specs` → 0 (stderr visible) | 0 hits; `task schema:generate` untouched |
| "proposals/verdicts carry a MsgID" | `GD:727` `publisher.PublishToStream`; `git grep -n 'MsgID\|Nats-Msg-Id' -- processor/rule/publisher.go` → 0 | Neither side dedups; the re-proposal residual stands |
| AGENT_LOOPS policy | `processor/agentic-loop/internal/loopbucket/acquire.go:20`, `:42-43` | Unchanged: History 10, TTL 24h, refused otherwise |
| "one outstanding delivery per lane" | `C:1191-1192`: `MaxAckPending` fixed 1 for `agent.task`, `agent.response`, `tool.result`; 10 elsewhere | Unchanged |
| "both lanes already hold the revision" (Q2) | `git grep -n -E 'loopsBucket\.(Update\|Create)\(' -- processor/agentic-loop` → 0. Four writers, all `Put`, each discarding the revision `Put` returns: `C:2401`, `:2432`, `:2456`, `:2483`. The one reader, `LP:75`, discards `entry.Revision()` | FALSE on `main` (reconciliation § C Q2, S3) |
| "the terminal owner already publishes before its Update; the approval owner already does; birth keeps Put → publish" (§ 3.1, Q1) | Carrier `C:1947` (write) → `:1959` (publish) for every result, terminal included. Birth `C:1496` (publish) → `:1499` (Put, error ignored). Failure `C:1752` (entity Put) → `:1809` (marker Put) → `:1831` (publish). Cancel `C:2631` (Put) → `:2668` (publish) → `:2683` (marker Put). Sweeper `AS:100` (publish) → `:101` (Put, error ignored) | FALSE on `main`: no lane publishes before its entity write except birth and the sweeper, and those two are the ones the design wants write-first (§ C Q1, S1, S2, S13) |
| "`COMPLETE_<loopID>` is a Create-once marker read back on `ErrKeyExists`" (§ 5.7 b) | `C:2400-2401`, `:2431-2432`, `:2455-2456` — all `Put`; `KV:211` `Create` exists and returns `ErrKVKeyExists` (`KV:218`) | FALSE; § 5.7(b) needs a marker read (S13) |
| "a recovery layer exists to prune (§ 6)" | `ls processor/agentic-loop` → no `settlement_recovery.go`; `git grep -n 'GetLastMsgForSubject\|readRetained' -- processor/agentic-loop` → 0; `git grep -n continuation_unavailable -- .` → only this change's own inventory | FALSE; § 6 inverts — "survives" becomes "build", "deleted" becomes "never built" (reconciliation § B) |
| "the applied set is retained across the advance; the adopt writes one eviction ahead" (§ 3.6) | `H:2850` → `H:2870` `GetAndClearToolResults` (`ST:1107`, nil at `ST:1121`) drains the batch BEFORE the mint at `H:2927` | FALSE on `main`: the normal path already writes an empty set at the advance; pass-3's MEDIUM reverses (S5) |
| "the tool lane classifies a terminal loop before acting" (Q7 warm) | `H:2580` StopLoop branch precedes the lane's only terminal guard `H:2652`; `H:2546` stores by key before any check | FALSE: L2's placed comment #3 (`complete → complete`); the Q7 warm check must precede `HandleToolResult` at `C:2195` (S9) |
| Cold-path entry points | response `C:1623` → `C:1698-1700`; tool `C:2175` → `C:2287-2292`; verdict `C:2734` → `C:2759-2780`; cancel `C:2602`; approval `ARH:54-58` (memory only: `ErrLoopNotFound` → `staleDrop` → Ack at `ARH:194-199`); task: none (`HandleTask` `C:1396` creates the loop anew) | The loop-presence classifier (`LP:66-94`) is the attach point on four lanes; the approval and task lanes have none (S7, S11) |

## 1. Recovery decisions on `main` (one row per predecessor decision; main-only rows M1–M4)

Legend: **DF(x)** = existing durable fact x answers it on `main`; **AF** = the applied fact answers it; **NEED** = the decision has no home on `main` and L4 must build the site it attaches to; **KEEP** = present on `main`, survives unchanged; **GONE** = never built on `main`, nothing to delete.

| # | `68c14c8e` | `main` site | Input | Facts read today on `main` | Verdict on `main` |
|---|---|---|---|---|---|
| D1 | `C:1310-1311` | none; `C:1396` `HandleTask` → `ST:225` `CreateLoopWithID` / `H:876` `attachContinuation`; `C:1345` reads memory only | task | process map | NEED: no cold fork; a task naming a loop this process lost is born again in memory and its record overwritten by `Put` (`C:1499`) |
| D2 | `SR:384-397` | none | task | — | NEED (entity fields exist: `AG:49-57`) |
| D3 | `SR:398-400` | none on this lane; pattern `LP:91-92` | task | — | NEED |
| D4 | `SR:402-422` | `H:1120` `buildTaskRequest` mints `…:req:1:0` with MsgId (`H:1122`, `:1174`) | task | process only | AF; the birth path is the rebuild (S7) |
| D5 | `SR:424`, `ST:349-430` | none | task, response, tool, approval | — | NEED: rebuild ContextManager, caches, routing from the retained request (S8) |
| D6 | `C:1339-1350`, `C:1406-1409` | `C:1429-1437`, `C:1510` | task | `pendingTaskResults` | KEEP |
| D7 | `C:1532-1541` | none; birth failure → `C:1596` → `C:1752` `Put` | task | last-writer `Put` | GONE (no Create-once); a redelivered task after a birth failure meets a terminal record → D3 |
| D8 | `SR:457-476` | `C:1679`, `ST:1380` | response | process map + grammar | KEEP |
| D9 | `SR:478-492` | `C:1698-1700`, `LP:66-94` | response | KV entity | DF(entity presence); live → Retry today (`C:1700` default arm) |
| D10 | `SR:494-513` | none (no warm KV read) | response | — | GONE; the CAS write (Q2) is what detects a moved record |
| D11 | `SR:517-535` | `H:1253` `CurrentRequest` (process-local; empty after replacement, let through — L2's declared residual `ST:955-961`) | response | process map | AF: the guard reads `PublishedRequestID` (S8) |
| D12 | `C:1569-1586` | `H:1322`; cold `C:1700` stale = terminal | response | process entity / KV | DF(`State`); Q7's metric and audit line to add |
| D13 | `H:1249-1264` | `H:1322`, `H:1330-1337` | response | process entity | KEEP |
| D14 | `SR:131-195` | none; `GD:727` no MsgID | response (governance) | — | GONE; not built (S8); duplicate proposed/verdict pair is the declared residual |
| D15 | `SR:197-235` | none | governance | — | GONE (with D14) |
| D16 | `GD:547-560`, `C:2585-2595` | `GD:608-629` (`ErrNoGovernanceWaiter`, `GD:337`), `C:2734` → `C:2759-2780` (stale → Ack `:2773`; live → Retry `:2780`) | verdict | KV entity via `LP` | DF skeleton exists; Q6's classification replaces `:2773` (S12) |
| D17 | `C:2146-2148` | `C:2173` `findLoopIDForToolCall` | tool | process map | KEEP |
| D18 | `SR:556-569`, `C:2269-2306` | none at the wire; `EI:31` re-derives | tool | — | NEED in the cold path; `agentic/tools.go:637-641` fields exist |
| D19 | `SR:570-576` | `C:2287-2292` | tool | KV entity | DF(entity presence) |
| D20 | `SR:577-621` | none; pattern `PS:28-49`, `TR:51` | tool | — | NEED: retained-response reader (identity only) |
| D21 | `SR:622-632` | none | tool | — | GONE |
| D22 | `SR:651-665`, `SR:686-740` | none; warm path `H:2546` stores by key before any check | tool | — | AF (S9): classification before `C:2195` and in `C:2292`'s live arm |
| D23 | `SR:634-636` | none; `H:200` normal truncation | tool | — | GONE |
| D24 | `ST:473-521`, `ST:486-488` | none | tool | — | NEED: `restoreToolBatch` without the decrement (the advance is written after the PubAck, S2) |
| D25 | `ST:435-468` | none | tool, approval | — | NEED inside D24: stored keys ⊆ retained response's calls, else Quarantine; `requirePreceding` GONE |
| D26 | `SR:666-667`, `SR:821-852` | cold `C:2296-2303` (`tool_results_dropped_total{stale_execution}`, Warn); warm: none before `C:2195` | tool | KV `State` (cold) | DF(`State`): cold half is already Q7's effect-free ACK; warm half NEED (S9) |
| D27 | `SR:637-650`, `SR:763-817` | `H:2573` → `H:2667-2702`; store at `H:2546` precedes it | approval-required result | process entity | AF (S10): a resolved gate's redelivered `approval_required` result overwrites the real result by key (`ST:1082`) and re-gates via `H:2704-2723` |
| D28 | `SR:669-670`, `SR:743-758` | `H:2674` absorbs silently (no re-echo) | approval-required result | process `State` | NEED: re-echo in the awaiting branch when `PendingApproval.ExecutionID` matches (accepted gate order kept, S2) |
| D29 | `ARH:176-181` | `ARH:54-58` `ResolveApprovalIfPending` (`ST:499`) | approval response | process map | KEEP for warm; cold `ErrLoopNotFound` → `staleDrop` → Ack `ARH:78`, `:194-199` (S11) |
| D30 | `ARH:182-215` | none (no KV read) | approval response | — | NEED (S11) |
| D31 | `SR:857-866`, `SR:1015-1032` | none | approval response | — | NEED: I4 + key presence |
| D32 | `SR:867-899`, `SR:1034-1040` | none | approval response | — | AF (I4) |
| D33 | `SR:921-981` | none; `continuation_unavailable` → 0 hits outside this change | approval response | — | NEED — OWNER QUESTION (Q8 "unchanged" has no referent; reconciliation § C Q8) |
| D34 | `SR:904-916` | none | approval response | — | NEED (with D24) |
| D35 | `ARH:238-241` | `ARH:194-199` (`staleDrop` → Ack) | approval response | process | AF split: in-memory not-awaiting → Ack (W3); `ErrLoopNotFound` → record (S11) |
| D36 | `ARH:255-279` | `ARH:205` `persistHandlerResult` (Put `C:1947` → publish `:1959`) | approval (approve) | — | DF(TOOL_CALL_OUTCOMES replay `TC:740-743`); the order is the carrier's (S2) |
| D37 | `ARH:130-149` | `ARH:139-158` (`RequestID: pending.RequestID` at `:149`) | approval (reject) | same as tool | KEEP |
| D38 | `AS:21-86` | none; `AS:69` `SnapshotExpiredApprovals` reads memory | startup | — | NEED — OWNER QUESTION (§ C Q8 companion) |
| D39 | `AS:134-169` | `AS:65-129`: in-process `HandleApprovalResponse`, then `publishResults` `:100` → `persistLoopState` `:101` (errors ignored), then `:130` publishes the response to the wire for observers only | timer | process snapshot | AF via D37; the sweeper's own publish → Put pair must ride the carrier (S11) |
| D40 | `C:2508-2526` | `C:2593-2612` (`signals_dropped_total{already_terminal,stale_loop_id}`) | cancel | process refusal + KV | DF(`State`); the Q7 pattern the ruling names |
| D41 | `C:1833-1845`, `C:1959-1964`, `C:1917-1927` | three terminal write paths: carrier `C:1974-2023` (entity `:1975` → marker `:1982`/`:2002` → stamps → publish `:1959`); failure `C:1752` → `:1809` → `:1831`; cancel `C:2631` → `:2668` → `:2683`; all `Put` | terminal | none (no revision, no Create-once) | NEED (S13): no single owner, no CAS, no marker identity, write-before-publish |
| D42 | `TR:65-110` | `TR:64`, `TR:89`, `TR:147` | dispatch | retained task | KEEP (outside L4) |
| D43 | `PS:76-122`, model `:616-627` | `PS:76-89`, model `:633-643` | agent request | retained response | KEEP (L2) |
| D44 | `C:1833-1843` | none | terminal | — | Q7(a) attaches before `C:2195` and inside `C:2296` (S9); the TaskID-vs-marker Quarantine has no warm read to attach to and is dropped |
| D45 | `C:2162-2170` | none | tool (warm) | — | GONE; the process-retained revision is the CAS input (S3) |
| D46 | `C:2171-2177` | none | tool (warm) | — | GONE |
| D47 | `C:2178-2181` | none | tool (warm) | — | GONE; terminal → Q7 at `C:2195` (S9) |
| D48 | `C:2182-2190` | none; the six-field identity lives at `H:2710-2713` (write) and `ST:499` (resolve) | tool (warm) | — | GONE; I4 is new |
| D49 | `C:1846-1870` | none; markers are `Put` (`C:2401`, `:2432`, `:2456`) | terminal (redelivered) | — | NEED (S13): (b) reads the marker `Create` refused |
| M1 | — | `C:1420-1425` deferred continuation: `Put` only, error ignored | task | process entity | main-only writer; rides `Update(revision)` (S3); a CAS loss here is the two-consumer window (§ D 2) |
| M2 | — | `C:1429-1437` `!result.Created` dedup → Ack without publish unless `pendingTaskResult` | task | process map | KEEP warm; cold has no dedup (D1) |
| M3 | — | `C:1108-1110` (`deliverylane.Consume`, heartbeat lanes task/response/tool) and `C:1130-1132` (`deliverylane.Settle`, signal/approval/verdict); `C:980-1024` names the lanes | all | admission latch | KEEP: L4's ACK paths and adoption are decisions inside the handlers (#1341 design § L4); no latch spelling is added |
| M4 | — | `H:1253` empty-`CurrentRequest` let-through after replacement (L2 residual `ST:955-961`); `H:2580` before `H:2652` (L2 comment #3); `ST:333` admission vs `ST:887` mint window (L2 comment #2) | response, tool, task | process maps | the three placed inventory comments; reconciliation § D |

## 2. Every writer of AGENT_LOOPS on `main` (the applied fact must ride one of these)

| Site | Form | Order relative to the outputs it implies | Lane |
|---|---|---|---|
| `C:1499` via `persistLoopState` (`C:2468`, `Put` at `:2483`) | `Put`, error ignored | publish `C:1496` (R1 + `agent.created`) THEN `Put` | task birth |
| `C:1425` via `persistLoopState` | `Put`, error ignored | marker only; nothing published | task, deferred continuation |
| `C:1975` via `persistResultState` (`C:1974`) inside `persistHandlerResult` (`C:1923`) | `Put` | `Put` (`C:1947`) THEN `publishResults` (`C:1959`); callers `C:1638` response, `C:2220` tool, `C:2271` failed terminal tool result, `ARH:205` approval | response, tool, approval, gate (the gate result is `result.State = awaiting_approval` from `H:2573`; there is no separate gate writer) |
| `C:1982` `persistCompletionState` (`Put` `:2401`), `C:2002` `persistFailureState` (`Put` `:2432`) | `Put` | inside `persistResultState`, after the entity `Put`, before graph stamps, before publish | terminal via the carrier |
| `C:1752` (entity) and `C:1809` (marker) in `handleLoopFailure` (`C:1734`) / `publishFailureEvents` (`C:1794`) | `Put` | entity `Put` → marker `Put` → stamp → publish `C:1831` | terminal failure (handler error, spawn-identity failure `C:1596`) |
| `C:2631` (entity), `C:2683` → `:2456` (marker) in `handleCancelSignal` (`C:2614`) | `Put` | entity `Put` → publish `C:2668` (no MsgID) → graph → marker `Put` | cancel |
| `AS:101` via `persistLoopState` | `Put`, error ignored | publish `AS:100` THEN `Put`; the wire publish `AS:130` is for observers only | approval timeout sweeper |

Observation (not a design): seven writer sites, one form, three orders. The predecessor's seven sites had two forms (`Put`, `Update`) and one CAS-protected terminal owner; `main` has neither a revision anywhere nor a Create-once marker. The single-holder model (one process holds a loop; `MaxAckPending` 1 on the three heartbeat lanes) means the process copy written by `C:2483` is the only source on every lane, so a CAS on `main` protects against the two-consumer window (§ 1 M1, M4) and against a replaced process's stale write, not against a warm cross-lane race the Codex branch had.

## 3. Same-class collision table (durable primitive: "current published request + applied set on the loop record")

| Dimension | Evidence on `main` |
|---|---|
| Semantic class | "Which model request is outstanding for loop L, and which tool executions of that request have been applied" |
| Owners | agentic-loop only. Applied set: `LoopEntity.PendingToolResults` keys (`AG:57`; written at `ST:1075-1082`; drained and nilled at the advance by `ST:1107-1121` via `H:2870`, before the mint at `H:2927`). Current request: **no durable owner** — process maps `currentRequests` / `outstandingRequests` (`ST:79`, `ST:887-888`, read at `ST:933`, `ST:955`), the structured-ID route rebuild (`ST:1380`), and the comment reserving the field (`ST:1338`) |
| Catalogs | `processor/agentic-loop/config.go:426` declares AGENT_LOOPS as a `KVWritePort`; `git grep -n AGENT_LOOPS -- openspec/specs/framework-bucket-catalog/spec.md` → 0 |
| Status | `LoopEntity.State` (`AG:49-57`); no readiness key |
| Lifecycle | Written at the seven sites of § 2; bucket policy `acquire.go:20`, `:42-43`; `COMPLETE_` markers share the bucket (`C:2400`, `:2431`, `:2455`) |
| Ownership | Single writer component; one process per loop; `MaxAckPending` 1/10 (`C:1191-1192`); admission latch per lane (`C:1108`, `:1130`; `DL:27-58`); no lease |
| Readers (in-repo) | `LP:75` (presence classification, four lanes); `processor/agentic-dispatch` reads loop authority through its own view (`http_activity.go:321-337`, terminal-skipping conjunct at `:329`); the trajectory query reads AGENT_TRAJECTORIES |
| Readers (sisters) | Not re-swept: the predecessor's Readers row was pinned against sister HEADs, which are outside this base and unchanged by it (semspec control planes watch AGENT_LOOPS; recovery-consumer treats presence as liveness; semsage mirrors one SSE event per change). One new adopter-visible fact is added in § 4 |
| Writers | agentic-loop only (`git grep -n 'loopsBucket\.' -- processor ':!processor/agentic-loop'` → 0) |
| Recovery | This inventory. Closest same-shape instances on `main`: `TOOL_CALL_OUTCOMES` post-effect Create-once keyed by ExecutionID (`TC:808`, `TC:869`, `outcomes.go:100`); the dispatch retained-task read-back (`TR:51`, `:89`); the model plane's retained-response reuse (`PS:37`, model `:633-643`). All three are "read the durable fact by identity before acting" — the shape L4 adopts; none is a pre-call marker |

## 4. Adopter seam inventory (surfaces reached from outside this repo)

Surface A — `LoopEntity` JSON in AGENT_LOOPS gains one optional field. Unchanged from the predecessor: nothing to know to keep working; no silent loss; write cadence unchanged (one write per settled input; a CAS failure writes nothing); doc-level discoverability is acceptable because no reader correctness fact is at stake.

Surface A' — ordering, new on `main`. Today every terminal lane except cancel writes the entity's terminal `state` before publishing the terminal event (`C:1947` → `:1959`; `C:1752` → `:1831`), the beta.57 contract at `C:1798-1803`. Under the reconciled terminal order (marker → stamps → publish → entity `Update`, reconciliation S13) a watcher keyed on the `COMPLETE_` marker sees it before the event, as today; a watcher keyed on the entity's `state` sees terminal AFTER the event. semspec's two control planes and its liveness scan are keyed on AGENT_LOOPS (predecessor Readers row) and which key each reads was not re-verified here. Finding: the terminal reorder is adopter-visible and belongs in the migration note with the key each sister watcher reads.

Surface B — tool authors / agentic-tools: no change; `ToolResult` carries `request_id`, `execution_id`, `call_ordinal` (`agentic/tools.go:637-641`) and the tools plane already stamps `Nats-Msg-Id` per execution (`TC:1224`, `TC:780`).

Surface C — approval UIs publishing `ApprovalResponse`: no wire change; the lane stops acknowledging a decision for a live loop the process lost (`ARH:58-78`, `:194-199` today).

Surface D — the framework-owned prediction check: nothing in L4 asks a caller to predict a value. The Codex rendering compare that motivated the change was never merged; on `main` the prediction-shaped input is absent.

## Adjacent claims on the territory (§ 5)

- `openspec/specs/agentic-loop/spec.md:212` — `A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler`
- `openspec/specs/agentic-loop/spec.md:421` — `### Requirement: Terminal trajectory facts are ordinary observations`
- `openspec/specs/agentic-loop/spec.md:443` — `#### Scenario: terminal redelivery creates another terminal observation`
- `openspec/specs/agentic-tools/spec.md:437` — ``agentic-tools` SHALL own one immutable COMPLETED outcome per framework execution identity, retaining the provider`
- `docs/concepts/17-approval-flow.md:65` — `- **Restart-safe.** `LoopEntity.PendingApproval` lives in the`
- #1345 (beta.163, `class:swallowed-degrade`, placement candidate L4): five task-intake branches ACK on failure; Q1's Put → publish converts the failed-write branch at `C:1499` by necessity, the other four stay #1345's (reconciliation S1).
- #1342 (blocked by #1341): silent-refusal lanes; L4 adds no lane and no latch spelling.
- Archived L1 `openspec/changes/archive/2026-09-19-settle-after-durable-effect/design.md` residuals naming L4: identity-preserving replay at `persistHandlerResult` (`:95-96`, `:136`), task intake (`:238-255`), cancelled tool result after mutation (`:257-283`), `loopPresenceLive` Retry (`:313`).
- Archived L2 `openspec/changes/archive/2026-09-21-stable-request-identity/design.md` residuals naming L4: retry ordinal (`:157-161`), restore-from-KV (`:186`), completing-path queued sibling (`:212`), `complete → complete` (`:230-232`), admission/mint window (`:248-251`), outstanding registry (`:256`), superseded guard after replacement (`:264-265`), parser deviation accepted (`:276`).
- Archived L3 `openspec/changes/archive/2026-09-21-durable-loop-authority/design.md:50`: clearing `PendingApproval` on a terminal transition is deferred to L4 ("the layer that owns terminal and adopt transitions"); `:107-114` the route-ambiguity residual the 2026-09-21 ruling places on L4.
- Archived #1341 `openspec/changes/archive/2026-09-21-delivery-lane-admission-package/design.md:377-381`: L4's Survives list loses `delivery_owner.go`; L4's ACK paths are `DeliveryWork` decisions inside the handlers.
- Sibling change `openspec/changes/agentrun-fanout-settlement/` (#1249, other worktree): `grep -n -i '#1330\|L4\b\|PublishedRequestID\|durable-applied' design.md` → 0 references.
- `ls openspec/changes` on this branch → `archive` and this change only.

## Searches run (verbatim, in order)

```
git rev-parse HEAD b7ce8727 origin/main ; git status --porcelain
sed -n '1,140p' scripts/inventory-verify.sh
gh issue view 1330 --comments ; gh pr view 1361 ; gh issue view 1327 --json body,comments ; gh issue view 1146 --json body ; gh issue view 1146 --json comments | grep -n -i 'continuation_unavailable\|2026-09-13'
grep -n '^func ' processor/agentic-loop/{component,handlers,state,approval_response_handler,approval_sweeper,governance_dispatcher}.go
git grep -n -E 'loopsBucket\.(Put|Update|Create|Get|Delete)\(' -- processor/agentic-loop ':!*_test.go'        # 4 Put + 1 Get; 0 Update, 0 Create
git grep -n -E 'persistLoopState\(|persistHandlerResult\(|persistResultState\(|persist(Completion|Failure|Cancellation)State\(|publishResults\(' -- processor/agentic-loop ':!*_test.go'
git grep -n -E 'GenerateRequestID\(|Nats-Msg-Id|PublishToStreamWithMsgID|MsgID' -- processor/agentic-loop ':!*_test.go'
git grep -n -E 'COMPLETE_' -- processor/agentic-loop agentic ':!*_test.go'
git grep -n -E 'PublishedRequestID|published_request_id|outstandingRequests|attachContinuation\(|OutstandingRequest\(|CurrentRequest\(|SettleRequest\(' -- processor agentic ':!*_test.go'
git grep -n -E 'loopIDFromStructuredID|:req:|ExtractLoopIDFromRequest' -- processor/agentic-loop processor/agentic-dispatch agentic ':!*_test.go'
git grep -n -E 'deliverylane\.' -- processor ':!*_test.go'
git grep -n -E 'loop_route_ambiguous|func .*activeLoop' -- processor/agentic-dispatch ; git grep -n TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate -- processor
git grep -n 'continuation_unavailable' -- .        # only openspec/changes/agentic-loop-durable-applied-facts/inventory.md
git grep -n 'tasks_submitted_total\|tasksSubmitted' -- processor ':!*_test.go'
git grep -n -E 'func \(m \*Client\) (PublishToStream|PublishToStreamWithMsgID|PublishToStreamAsync)' -- natsclient ; git grep -n -E 'func .*\) (Update|Put|Create)\(ctx' -- natsclient
git grep -n 'GetLastMsgForSubject' -- processor natsclient ':!*_test.go'        # dispatch task_recovery.go:51, model provider_settlement.go:37; 0 in agentic-loop
git grep -n -E 'Nats-Msg-Id|WithMsgID\(' -- processor/agentic-tools processor/agentic-model processor/agentic-dispatch processor/agentic-governance processor/rule ':!*_test.go'
git grep -n 'IncrementTruncationRetry\|ResetTruncationRetry\|truncationRetryAttempts' -- processor/agentic-loop ':!*_test.go'
git grep -n 'classifyMissingLoop(' -- processor/agentic-loop ':!*_test.go'        # component.go:1700, 2292, 2602, 2773
git grep -n 'PendingToolResults' -- processor/agentic-loop agentic ':!*_test.go'
git grep -l 'pending_tool_results\|published_request_id' -- schemas specs        # 0
git grep -n 'MsgID\|Nats-Msg-Id' -- processor/rule/publisher.go        # 0
git grep -n 'acknowledgement floor' -- '*_test.go'        # 0
git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'        # openspec only
ls processor/agentic-loop/*_test.go ; ls test/e2e/harness/processbarrier test/e2e/scenarios/agentic
sed -n '201,226p' openspec/specs/agentic-loop/spec.md > live_req.txt ; sed -n '102,136p' <delta> > delta_req.txt ; diff live_req.txt delta_req.txt
grep -n -i '#1330\|L4\b\|PublishedRequestID\|durable-applied' /Users/coby/Code/c360/semstreams-wt/claude/gh1249-agentrun-fanout-settlement/openspec/changes/agentrun-fanout-settlement/design.md        # 0
for ref in $(cat pins.txt); do sed -n "${n}p" "$p"; done        # every pin below generated, none transcribed
scripts/inventory-verify.sh <abs>/arch1330/inventory.md        # from the worktree root; final line in verify.out
```

Skills applied: `entity-or-bucket` (existing bucket AGENT_LOOPS, ground 1 — CAS atomicity with `Iterations` / `PendingToolResults`; unchanged outcome). `kv-or-stream`, `orchestration-check`, `new-payload`, `query-pattern`: not triggered on `main` either (the retained-request read adopts the shape at `PS:28-49` / `TR:51`; no new path, payload, or query access).

## 7. Pins (`task inventory:verify` grammar; each generated with `sed -n "${n}p"` at `b7ce8727`)

- `agentic/state.go:49` — `type LoopEntity struct {`
- `agentic/state.go:55` — `Iterations         int                   `json:"iterations"``
- `agentic/state.go:57` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID`
- `agentic/state.go:79` — `PendingApproval     *PendingApprovalState `json:"pending_approval,omitempty"``
- `agentic/state.go:98` — `PendingContinuation bool `json:"pending_continuation,omitempty"``
- `agentic/state.go:113` — `PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"``
- `agentic/state.go:136` — `func (e *LoopEntity) Validate() error {`
- `agentic/state.go:171` — `func (e *LoopEntity) TransitionTo(newState LoopState) error {`
- `agentic/state.go:181` — `if e.State == newState {`
- `agentic/state.go:185` — `if e.State.IsTerminal() {`
- `agentic/state.go:196` — `type PendingApprovalState struct {`
- `agentic/state.go:197` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/state.go:215` — `func (e *LoopEntity) BeginAwaitingApproval(callID, toolName string, arguments map[string]any, reason string, timeout time.Duration, traceID string) error {`
- `agentic/state.go:246` — `func (e *LoopEntity) ResolveApproval() error {`
- `agentic/state.go:279` — `func (e *LoopEntity) IncrementIteration() error {`
- `agentic/state.go:283` — `e.Iterations++`
- `agentic/tools.go:629` — `type ToolResult struct {`
- `agentic/tools.go:637` — `LoopID      string         `json:"loop_id,omitempty"``
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/state.go:79` — `outstandingRequests map[string]string // loopID -> requestID`
- `processor/agentic-loop/state.go:298` — `func (m *LoopManager) attachContinuation(loopID, taskID string) (agentic.LoopEntity, bool, error) {`
- `processor/agentic-loop/state.go:333` — `if _, outstanding := m.outstandingRequests[loopID]; outstanding {`
- `processor/agentic-loop/state.go:391` — `func (m *LoopManager) UpdateLoop(entity agentic.LoopEntity) error {`
- `processor/agentic-loop/state.go:466` — `func (m *LoopManager) IncrementTruncationRetry(loopID string) int {`
- `processor/agentic-loop/state.go:477` — `func (m *LoopManager) ResetTruncationRetry(loopID string) {`
- `processor/agentic-loop/state.go:574` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-loop/state.go:883` — `func (m *LoopManager) TrackRequest(requestID, loopID string) {`
- `processor/agentic-loop/state.go:887` — `m.outstandingRequests[loopID] = requestID`
- `processor/agentic-loop/state.go:888` — `m.currentRequests[loopID] = requestID`
- `processor/agentic-loop/state.go:915` — `func (m *LoopManager) SettleRequest(loopID, requestID string) {`
- `processor/agentic-loop/state.go:933` — `func (m *LoopManager) OutstandingRequest(loopID string) string {`
- `processor/agentic-loop/state.go:955` — `func (m *LoopManager) CurrentRequest(loopID string) string {`
- `processor/agentic-loop/state.go:962` — `func (m *LoopManager) GetLoopForRequest(requestID string) (string, bool) {`
- `processor/agentic-loop/state.go:1063` — `func (m *LoopManager) StoreToolResult(loopID string, result agentic.ToolResult) error {`
- `processor/agentic-loop/state.go:1075` — `resultKey := result.ExecutionID`
- `processor/agentic-loop/state.go:1082` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/state.go:1107` — `func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {`
- `processor/agentic-loop/state.go:1121` — `entity.PendingToolResults = nil`
- `processor/agentic-loop/state.go:1338` — `// (#1330, LoopEntity.PublishedRequestID).`
- `processor/agentic-loop/state.go:1339` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1345` — `iteration = entity.Iterations + 1`
- `processor/agentic-loop/state.go:1347` — `return fmt.Sprintf("%s:req:%d:%d", loopID, iteration, m.truncationRetryAttempts[loopID])`
- `processor/agentic-loop/state.go:1360` — `func (m *LoopManager) ExtractLoopIDFromRequest(requestID string) string {`
- `processor/agentic-loop/state.go:1361` — `parts := strings.Split(requestID, ":req:")`
- `processor/agentic-loop/state.go:1380` — `func (m *LoopManager) GetLoopForRequestWithRecovery(requestID string) (string, bool) {`
- `processor/agentic-loop/component.go:77` — `consumers     []*deliverylane.Binding`
- `processor/agentic-loop/component.go:980` — `func (c *Component) resolveLoopLaneDelivery(`
- `processor/agentic-loop/component.go:990` — `case "agent.task", "agent.response", "tool.result":`
- `processor/agentic-loop/component.go:999` — `default: // agent.signal, agent.approval_response, agent.toolcall.* — fast`
- `processor/agentic-loop/component.go:1026` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/component.go:1036` — `func (c *Component) setupConsumer(`
- `processor/agentic-loop/component.go:1108` — `admission = deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)`
- `processor/agentic-loop/component.go:1110` — `result, admitted := deliverylane.Consume(msgCtx, msg, policy, admission)`
- `processor/agentic-loop/component.go:1130` — `admission = deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)`
- `processor/agentic-loop/component.go:1132` — `result, admitted := deliverylane.Settle(msgCtx, msg, settleRetry, admission, "loop", settleHandlerFn)`
- `processor/agentic-loop/component.go:1185` — `func agenticLoopConsumerPolicy(port component.Port) (component.ConsumerConfig, int, error) {`
- `processor/agentic-loop/component.go:1191` — `if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {`
- `processor/agentic-loop/component.go:1192` — `fixed = 1`
- `processor/agentic-loop/component.go:1313` — `func (c *Component) taskInputHandler(workTimeout time.Duration) inputHandler {`
- `processor/agentic-loop/component.go:1345` — `func (c *Component) refuseConflictingTaskIdentity(task agentic.TaskMessage, suppliedLoopID string) error {`
- `processor/agentic-loop/component.go:1360` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1396` — `result, err := c.handler.HandleTask(ctx, *task)`
- `processor/agentic-loop/component.go:1420` — `if result.Deferred {`
- `processor/agentic-loop/component.go:1425` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1429` — `if !result.Created {`
- `processor/agentic-loop/component.go:1496` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1499` — `c.persistLoopState(ctx, result.LoopID)`
- `processor/agentic-loop/component.go:1510` — `func (c *Component) rememberPendingTaskResult(taskID string, result HandlerResult) {`
- `processor/agentic-loop/component.go:1596` — `func (c *Component) handleSpawnIdentityFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, err error) error {`
- `processor/agentic-loop/component.go:1617` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1623` — `return c.settleResponseWithoutLoop(ctx, response.RequestID)`
- `processor/agentic-loop/component.go:1628` — `result, err := c.handler.HandleModelResponse(ctx, loopID, *response)`
- `processor/agentic-loop/component.go:1638` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:1664` — `func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, string, error) {`
- `processor/agentic-loop/component.go:1679` — `loopID := c.findLoopIDForRequest(responsePtr.RequestID)`
- `processor/agentic-loop/component.go:1698` — `func (c *Component) settleResponseWithoutLoop(ctx context.Context, requestID string) error {`
- `processor/agentic-loop/component.go:1699` — `loopID := loopIDFromStructuredID(requestID, ":req:")`
- `processor/agentic-loop/component.go:1700` — `switch c.classifyMissingLoop(ctx, loopID) {`
- `processor/agentic-loop/component.go:1734` — `func (c *Component) handleLoopFailure(`
- `processor/agentic-loop/component.go:1752` — `established := c.persistLoopState(ctx, loopID)`
- `processor/agentic-loop/component.go:1794` — `func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string) error {`
- `processor/agentic-loop/component.go:1809` — `if persistErr := c.persistFailureState(errorCtx, loopID, failure); persistErr != nil {`
- `processor/agentic-loop/component.go:1831` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1923` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1947` — `if err := c.persistResultState(ctx, result, terminal); err != nil {`
- `processor/agentic-loop/component.go:1959` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1964` — `c.releaseLoopTransientState(result.LoopID)`
- `processor/agentic-loop/component.go:1974` — `func (c *Component) persistResultState(ctx context.Context, result HandlerResult, terminal bool) error {`
- `processor/agentic-loop/component.go:1975` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1982` — `if err := c.persistCompletionState(ctx, result.LoopID, result.CompletionState); err != nil {`
- `processor/agentic-loop/component.go:2002` — `if err := c.persistFailureState(ctx, result.LoopID, result.FailureState); err != nil {`
- `processor/agentic-loop/component.go:2140` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:2173` — `loopID := c.findLoopIDForToolCall(toolResult.ExecutionID)`
- `processor/agentic-loop/component.go:2175` — `return c.settleToolResultWithoutLoop(ctx, toolResult)`
- `processor/agentic-loop/component.go:2195` — `result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)`
- `processor/agentic-loop/component.go:2220` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:2263` — `func (c *Component) settleFailedToolResult(`
- `processor/agentic-loop/component.go:2271` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:2287` — `func (c *Component) settleToolResultWithoutLoop(ctx context.Context, toolResult agentic.ToolResult) error {`
- `processor/agentic-loop/component.go:2290` — `loopID = loopIDFromStructuredID(toolResult.CallID, ":tool:")`
- `processor/agentic-loop/component.go:2292` — `switch c.classifyMissingLoop(ctx, loopID) {`
- `processor/agentic-loop/component.go:2318` — `func (c *Component) publishResults(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:2328` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, msg.Subject, msg.Data, msg.MsgID); err != nil {`
- `processor/agentic-loop/component.go:2389` — `func (c *Component) persistCompletionState(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {`
- `processor/agentic-loop/component.go:2400` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2401` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2421` — `func (c *Component) persistFailureState(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {`
- `processor/agentic-loop/component.go:2431` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2432` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2445` — `func (c *Component) persistCancellationState(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) error {`
- `processor/agentic-loop/component.go:2455` — `key := fmt.Sprintf("COMPLETE_%s", loopID)`
- `processor/agentic-loop/component.go:2456` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
- `processor/agentic-loop/component.go:2468` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:2483` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/component.go:2529` — `func (c *Component) findLoopIDForRequest(requestID string) string {`
- `processor/agentic-loop/component.go:2540` — `func (c *Component) findLoopIDForToolCall(executionID string) string {`
- `processor/agentic-loop/component.go:2550` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2593` — `func (c *Component) settleUncancellableLoop(ctx context.Context, loopID string, cause error) error {`
- `processor/agentic-loop/component.go:2602` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:2614` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) error {`
- `processor/agentic-loop/component.go:2631` — `if err := c.persistLoopState(ctx, loopID); err != nil {`
- `processor/agentic-loop/component.go:2668` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`
- `processor/agentic-loop/component.go:2683` — `if err := c.persistCancellationState(ctx, loopID, &completion); err != nil {`
- `processor/agentic-loop/component.go:2716` — `func (c *Component) handleToolCallVerdictMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2734` — `settled, err := dispatcher.HandleVerdict(decision, executionID, payload)`
- `processor/agentic-loop/component.go:2759` — `func (c *Component) settleVerdictWithoutWaiter(`
- `processor/agentic-loop/component.go:2773` — `if c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:2780` — `return natsclient.DeliveryDecisionRetry, cause`
- `processor/agentic-loop/loop_presence.go:50` — `func loopIDFromStructuredID(structuredID, separator string) string {`
- `processor/agentic-loop/loop_presence.go:66` — `func (c *Component) classifyMissingLoop(ctx context.Context, loopID string) loopPresence {`
- `processor/agentic-loop/loop_presence.go:70` — `return loopPresenceStale`
- `processor/agentic-loop/loop_presence.go:75` — `entry, err := c.loopsBucket.Get(ctx, loopID)`
- `processor/agentic-loop/loop_presence.go:78` — `return loopPresenceStale`
- `processor/agentic-loop/loop_presence.go:84` — `if err := json.Unmarshal(entry.Value(), &entity); err != nil {`
- `processor/agentic-loop/loop_presence.go:91` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/loop_presence.go:94` — `return loopPresenceLive`
- `processor/agentic-loop/handlers.go:55` — `MsgID string`
- `processor/agentic-loop/handlers.go:824` — `func (h *MessageHandler) HandleTask(ctx context.Context, task TaskMessage) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:876` — `entity, deferred, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)`
- `processor/agentic-loop/handlers.go:1077` — `func (h *MessageHandler) deferredContinuationResult(loopID, taskID string, entity agentic.LoopEntity) HandlerResult {`
- `processor/agentic-loop/handlers.go:1120` — `func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:1122` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:1174` — `MsgID:   request.RequestID,`
- `processor/agentic-loop/handlers.go:1212` — `func (h *MessageHandler) HandleModelResponse(ctx context.Context, loopID string, response agentic.AgentResponse) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:1253` — `if current := h.loopManager.CurrentRequest(loopID); current != "" && current != response.RequestID {`
- `processor/agentic-loop/handlers.go:1275` — `h.loopManager.SettleRequest(loopID, response.RequestID)`
- `processor/agentic-loop/handlers.go:1322` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/handlers.go:1389` — `h.loopManager.ResetTruncationRetry(loopID)`
- `processor/agentic-loop/handlers.go:1409` — `h.loopManager.ResetTruncationRetry(loopID)`
- `processor/agentic-loop/handlers.go:1424` — `carried, err := h.carryDeferredContinuation(ctx, loopID, entity, cm, &result)`
- `processor/agentic-loop/handlers.go:2029` — `func (h *MessageHandler) handleLengthTruncation(ctx context.Context, loopID string, entity agentic.LoopEntity, cm *ContextManager, response agentic.AgentResponse, result *HandlerResult) error {`
- `processor/agentic-loop/handlers.go:2037` — `retryCount := h.loopManager.IncrementTruncationRetry(loopID)`
- `processor/agentic-loop/handlers.go:2141` — `func (h *MessageHandler) emitRetryRequest(ctx context.Context, loopID string, entity agentic.LoopEntity, cm *ContextManager, result *HandlerResult, postUtilization float64) error {`
- `processor/agentic-loop/handlers.go:2168` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2194` — `MsgID:   request.RequestID,`
- `processor/agentic-loop/handlers.go:2480` — `func (h *MessageHandler) HandleToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:2546` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`
- `processor/agentic-loop/handlers.go:2552` — `err = h.loopManager.RemovePendingTool(loopID, toolResult.CallID)`
- `processor/agentic-loop/handlers.go:2573` — `if h.checkApprovalGate(loopID, &entity, toolResult, &result) {`
- `processor/agentic-loop/handlers.go:2580` — `if toolResult.StopLoop {`
- `processor/agentic-loop/handlers.go:2619` — `h.absorbToolResultsIntoContext(loopID, cm)`
- `processor/agentic-loop/handlers.go:2620` — `carried, err := h.carryDeferredContinuation(ctx, loopID, entity, cm, &result)`
- `processor/agentic-loop/handlers.go:2651` — `if h.loopManager.AllToolsComplete(loopID) {`
- `processor/agentic-loop/handlers.go:2652` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/handlers.go:2653` — `return result, nil`
- `processor/agentic-loop/handlers.go:2667` — `func (h *MessageHandler) checkApprovalGate(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult, result *HandlerResult) bool {`
- `processor/agentic-loop/handlers.go:2674` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/handlers.go:2704` — `func (h *MessageHandler) gateForApproval(loopID string, entity *agentic.LoopEntity, toolResult agentic.ToolResult) (*PublishedMessage, error) {`
- `processor/agentic-loop/handlers.go:2710` — `if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {`
- `processor/agentic-loop/handlers.go:2713` — `entity.PendingApproval.RequestID = toolResult.RequestID`
- `processor/agentic-loop/handlers.go:2723` — `if err := h.loopManager.UpdateLoop(*entity); err != nil {`
- `processor/agentic-loop/handlers.go:2792` — `func (h *MessageHandler) handleToolsComplete(`
- `processor/agentic-loop/handlers.go:2805` — `err := h.loopManager.IncrementIteration(loopID)`
- `processor/agentic-loop/handlers.go:2850` — `h.absorbToolResultsIntoContext(loopID, cm)`
- `processor/agentic-loop/handlers.go:2852` — `if err := h.publishIterationRequest(ctx, loopID, entity, cm, result, newIteration); err != nil {`
- `processor/agentic-loop/handlers.go:2869` — `func (h *MessageHandler) absorbToolResultsIntoContext(loopID string, cm *ContextManager) {`
- `processor/agentic-loop/handlers.go:2870` — `for _, tm := range h.buildToolMessages(h.loopManager.GetAndClearToolResults(loopID)) {`
- `processor/agentic-loop/handlers.go:2886` — `func (h *MessageHandler) publishIterationRequest(`
- `processor/agentic-loop/handlers.go:2927` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2958` — `MsgID:   request.RequestID,`
- `processor/agentic-loop/handlers.go:3031` — `if err := h.publishIterationRequest(ctx, loopID, entity, cm, result, newIteration); err != nil {`
- `processor/agentic-loop/handlers.go:3066` — `func (h *MessageHandler) buildToolMessages(results []agentic.ToolResult) []agentic.ChatMessage {`
- `processor/agentic-loop/approval_response_handler.go:32` — `func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (result HandlerResult, err error) {`
- `processor/agentic-loop/approval_response_handler.go:54` — `pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID, response.ExecutionID)`
- `processor/agentic-loop/approval_response_handler.go:58` — `if !ok {`
- `processor/agentic-loop/approval_response_handler.go:78` — `return HandlerResult{LoopID: loopID, State: state, staleDrop: true}, nil`
- `processor/agentic-loop/approval_response_handler.go:117` — `func (h *MessageHandler) dispatchApprovedCall(loopID string, pending agentic.PendingApprovalState, args map[string]any, approvedBy string, result *HandlerResult) error {`
- `processor/agentic-loop/approval_response_handler.go:122` — `RequestID:   pending.RequestID,`
- `processor/agentic-loop/approval_response_handler.go:139` — `func (h *MessageHandler) handleRejectedApproval(ctx context.Context, loopID string, pending agentic.PendingApprovalState, response agentic.ApprovalResponse) (HandlerResult, error) {`
- `processor/agentic-loop/approval_response_handler.go:149` — `RequestID:   pending.RequestID,`
- `processor/agentic-loop/approval_response_handler.go:158` — `return h.HandleToolResult(ctx, loopID, synthetic)`
- `processor/agentic-loop/approval_response_handler.go:164` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/approval_response_handler.go:182` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_response_handler.go:194` — `if result.staleDrop {`
- `processor/agentic-loop/approval_response_handler.go:199` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/approval_response_handler.go:205` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:209` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/approval_sweeper.go:43` — `func (c *Component) runApprovalTimeoutSweeper(ctx context.Context) {`
- `processor/agentic-loop/approval_sweeper.go:65` — `func (c *Component) sweepExpiredApprovals(ctx context.Context) {`
- `processor/agentic-loop/approval_sweeper.go:69` — `candidates := c.handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())`
- `processor/agentic-loop/approval_sweeper.go:100` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/approval_sweeper.go:101` — `c.persistLoopState(ctx, cand.LoopID)`
- `processor/agentic-loop/approval_sweeper.go:130` — `func (c *Component) publishApprovalResponseToWire(ctx context.Context, response agentic.ApprovalResponse) {`
- `processor/agentic-loop/governance_dispatcher.go:207` — `func (v VerdictPayload) effectiveLoopID() string {`
- `processor/agentic-loop/governance_dispatcher.go:337` — `var ErrNoGovernanceWaiter = errors.New("no active governance waiter")`
- `processor/agentic-loop/governance_dispatcher.go:600` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, verdict VerdictPayload) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:608` — `ch, ok := d.lookupWaiter(executionID)`
- `processor/agentic-loop/governance_dispatcher.go:629` — `fmt.Errorf("%w for execution_id %q", ErrNoGovernanceWaiter, executionID)`
- `processor/agentic-loop/governance_dispatcher.go:667` — `func publishProposed(ctx context.Context, publisher VerdictPublisher, loopID, parentLoopID string, call agentic.ToolCall, logger *slog.Logger) error {`
- `processor/agentic-loop/governance_dispatcher.go:727` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-loop/execution_identity.go:31` — `func deriveToolExecutionID(requestID, callID string, ordinal uint32) string {`
- `processor/agentic-loop/execution_identity.go:36` — `binary.BigEndian.PutUint32(ordinalBytes[:], ordinal)`
- `processor/agentic-loop/metrics.go:35` — `toolResultsDropped  *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:38` — `modelResponsesDropped *prometheus.CounterVec`
- `processor/agentic-loop/metrics.go:169` — `Name:      "tool_results_dropped_total",`
- `processor/agentic-loop/metrics.go:176` — `Name:      "model_responses_dropped_total",`
- `processor/agentic-loop/metrics.go:275` — `Name:      "tool_call_governance_subscribe_before_publish_failures_total",`
- `processor/agentic-loop/metrics.go:301` — `_ = registry.RegisterCounterVec("agentic-loop", "tool_results_dropped_total", metrics.toolResultsDropped)`
- `processor/agentic-loop/metrics.go:302` — `_ = registry.RegisterCounterVec("agentic-loop", "model_responses_dropped_total", metrics.modelResponsesDropped)`
- `processor/agentic-loop/metrics.go:303` — `_ = registry.RegisterCounterVec("agentic-loop", "signals_dropped_total", metrics.signalsDropped)`
- `processor/agentic-loop/metrics.go:382` — `verdictDropMissingWaiter         = "missing_waiter"`
- `processor/agentic-loop/metrics.go:383` — `verdictDropUnrecoverableIdentity = "unrecoverable_loop_identity"`
- `processor/agentic-loop/metrics.go:393` — `func (m *loopMetrics) RecordGovernanceVerdictMissingWaiter() {`
- `processor/agentic-loop/metrics.go:404` — `func (m *loopMetrics) recordVerdictIdentityUnrecoverable() {`
- `processor/agentic-loop/metrics.go:527` — `func (m *loopMetrics) recordToolResultDropped(reason string) {`
- `processor/agentic-loop/metrics.go:532` — `func (m *loopMetrics) recordSignalDropped(reason string) {`
- `processor/agentic-loop/metrics.go:544` — `func (m *loopMetrics) recordModelResponseDropped(reason string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:20` — `bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:43` — `return nil, fmt.Errorf("loop bucket %q policy: observed History=%d TTL=%s MaxAge=%s MaxBytes=%d; require History=10 TTL=24h MaxAge=24h MaxBytes<=0 (no reconciliation)", name, status.History(), status.TTL(), info.Config.MaxAge, info.Config.MaxBytes)`
- `processor/agentic-loop/config.go:426` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `internal/deliverylane/deliverylane.go:27` — `type Admission struct {`
- `internal/deliverylane/deliverylane.go:45` — `func NewAdmission(`
- `internal/deliverylane/deliverylane.go:58` — `func (a *Admission) Admit() bool {`
- `internal/deliverylane/deliverylane.go:105` — `func Consume(`
- `internal/deliverylane/deliverylane.go:136` — `func Settle(`
- `internal/deliverylane/deliverylane.go:225` — `func Observe(`
- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:963` — `func (m *Client) PublishToStreamWithMsgID(ctx context.Context, subject string, data []byte, msgID string) error {`
- `natsclient/kv.go:194` — `func (kv *KVStore) Put(ctx context.Context, key string, value []byte) (uint64, error) {`
- `natsclient/kv.go:211` — `func (kv *KVStore) Create(ctx context.Context, key string, value []byte) (uint64, error) {`
- `natsclient/kv.go:218` — `return 0, ErrKVKeyExists`
- `natsclient/kv.go:231` — `func (kv *KVStore) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {`
- `natsclient/kv.go:238` — `return 0, ErrKVRevisionMismatch`
- `processor/agentic-model/component.go:633` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:643` — `c.requestsProcessed++`
- `processor/agentic-model/provider_settlement.go:21` — `ReadRetainedResponse(context.Context, string, string) (retainedResponseEvidence, bool, error)`
- `processor/agentic-model/provider_settlement.go:28` — `func (r natsRetainedResponseEvidenceReader) ReadRetainedResponse(`
- `processor/agentic-model/provider_settlement.go:37` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-model/provider_settlement.go:76` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-dispatch/task_recovery.go:64` — `func stableDispatchTaskID(msg agentic.UserMessage) string {`
- `processor/agentic-dispatch/task_recovery.go:89` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:147` — `func (c *Component) readRetainedDispatchTask(`
- `processor/agentic-dispatch/http_activity.go:321` — `func (c *Component) activeLoop(ctx context.Context, msg agentic.UserMessage) (string, error) {`
- `processor/agentic-dispatch/http_activity.go:334` — `return "", &errs.ClassifiedError{Class: errs.ErrorInvalid, Code: "loop_route_ambiguous", Err: fmt.Errorf("multiple current loops match the user/channel route")}`
- `processor/agentic-dispatch/command_target_resolution_test.go:313` — `func TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate(t *testing.T) {`
- `processor/agentic-dispatch/commands.go:71` — `// residuals; TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate pins it.`
- `processor/agentic-dispatch/component.go:896` — `// retry. `loop_route_ambiguous` (http_activity.go:334) is`
- `processor/agentic-dispatch/component.go:1168` — `c.metrics.recordTaskSubmitted()`
- `processor/agentic-dispatch/component.go:1189` — `// survives is the counter at :1118 — tasks_submitted_total moves twice`
- `processor/agentic-dispatch/metrics.go:112` — `Name:      "tasks_submitted_total",`
- `processor/agentic-dispatch/metrics.go:180` — `Name:      "loop_admission_refusals_total",`
- `processor/agentic-dispatch/metrics.go:322` — `m.tasksSubmitted.Inc()`
- `processor/agentic-dispatch/terminal_settlement.go:272` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {`
- `processor/agentic-tools/component.go:740` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:743` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/component.go:780` — `err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ExecutionID))`
- `processor/agentic-tools/component.go:808` — `if err := c.persistAndPublishOutcome(ctx, call, result, outcomePathNew, true); err != nil {`
- `processor/agentic-tools/component.go:837` — `func (c *Component) loadCompletedOutcome(`
- `processor/agentic-tools/component.go:869` — `func (c *Component) persistAndPublishOutcome(`
- `processor/agentic-tools/component.go:1224` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-tools/component.go:1227` — `func (c *Component) publishResultWithMsgID(ctx context.Context, result agentic.ToolResult, msgID string) error {`
- `processor/agentic-tools/outcomes.go:100` — `func toolCallOutcomeKey(executionID string) string {`
- `processor/agentic-tools/outcomes.go:104` — `func toolResultMessageID(executionID string) string {`
- `docs/concepts/17-approval-flow.md:65` — `- **Restart-safe.** `LoopEntity.PendingApproval` lives in the`
- `openspec/specs/agentic-loop/spec.md:201` — `### Requirement: In-flight state MUST NOT be derived from the acknowledgement floor`
- `openspec/specs/agentic-loop/spec.md:212` — `A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler`
- `openspec/specs/agentic-loop/spec.md:421` — `### Requirement: Terminal trajectory facts are ordinary observations`
- `openspec/specs/agentic-loop/spec.md:443` — `#### Scenario: terminal redelivery creates another terminal observation`
- `openspec/specs/agentic-tools/spec.md:437` — ``agentic-tools` SHALL own one immutable COMPLETED outcome per framework execution identity, retaining the provider`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:26` — `func (s *Scenario) verifyStageAProcessReplacement(`
