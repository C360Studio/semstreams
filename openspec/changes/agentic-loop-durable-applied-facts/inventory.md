# L4 inventory — #1146 restart recovery on `origin/codex/gh1146-agentic-loop-restart`

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f
merge-base with origin/main: 461b6902f0746d4fbc5e5c22911aa34ab4fe121c
role: semstreams-architect, inventory-only phase (read-only; no worktree, branch, or GitHub mutation)
sources: every `file:line` below is at `68c14c8e` unless prefixed `main:`; files were read via `git show <branch>:<path>`.

Abbreviations: SR = `processor/agentic-loop/settlement_recovery.go`, ST = `processor/agentic-loop/state.go`,
C = `processor/agentic-loop/component.go`, H = `processor/agentic-loop/handlers.go`,
ARH = `processor/agentic-loop/approval_response_handler.go`, AS = `processor/agentic-loop/approval_sweeper.go`,
GD = `processor/agentic-loop/governance_dispatcher.go`, EI = `processor/agentic-loop/execution_identity.go`,
TR = `processor/agentic-dispatch/task_recovery.go`, PS = `processor/agentic-model/provider_settlement.go`,
TC = `processor/agentic-tools/component.go`, AG = `agentic/state.go`.

## 0. Premises measured before anything else

| Premise (from the brief) | Measurement | Result |
|---|---|---|
| "RequestIDs are deterministic (L2)" | `ST:1364-1365` `GenerateRequestID` returns `loopID:req:<uuid.NewString()>`; minted at `H:1083` (task), `H:1981` (truncation retry), `H:2654` (tools-complete) | **NOT true at 68c14c8e.** L4 inherits it as a dependency on L2; the three minting sites are L2's. |
| "agentic-model reuses a matching retained response (L2)" | `processor/agentic-model/component.go:616-627` reads `agent.response.<requestID>` before the provider call; `PS:76-122` | True at 68c14c8e; keyed by RequestID, so it only helps once L2 makes IDs stable. |
| "LoopEntity already persists PendingToolResults, Iterations, PendingApproval, five states" | `AG:46-93`, `AG:18-24`, `AG:54` (`PendingToolResults map[string]ToolResult` keyed by ExecutionID) | True. `main:agentic/state.go:53` has `PendingToolResults` keyed by call ID; the branch re-keyed it to ExecutionID. |
| "no applied-ID / published-request field exists under any spelling" | `git grep -n -i -E 'applied_execution\|AppliedExecution\|applied_ids\|current_request_id\|CurrentRequestID\|published_request\|PublishedRequest\|active_request_id\|LastRequestID\|iteration_request' <ref> -- agentic processor schemas openspec/specs docs/adr` on both refs | 0 hits on both. **But** the current batch's applied execution IDs already have a durable home: the keys of `PendingToolResults` (`ST:1084-1119`, Put via `C:1795 → C:2411`). See § 2. |
| "ExecutionID is deterministic" | `EI:24-26`, `EI:31-40`: `sha256(requestID, callID, ordinal)` | True, given a stable RequestID. |
| "LoopEntity appears in generated schemas / OpenAPI" | scan of every `schemas/*.json` + `specs/openapi.v3.yaml` for `pending_tool_results` | 0 hits; the `max_iterations` hits are component config. Adding a field does not touch `task schema:generate`. `agentic/payload_registry.go` does not register `LoopEntity`. |
| "proposals/verdicts carry a Nats-Msg-Id" | `GD:665` `publisher.PublishToStream(ctx, subject, data)`; `git show <branch>:processor/rule/publisher.go \| grep -n 'MsgID\|Nats-Msg-Id'` → 0 | Neither side dedups; a re-proposal re-fires the rule and yields a fresh verdict. |
| "AGENT_LOOPS retention is the bucket's, unmeasured" (my § 3 Lifecycle, first draft) | `processor/agentic-loop/internal/loopbucket/acquire.go:20` creates `History: 10, TTL: 24 * time.Hour`; `:42-43` refuse any other policy at startup (`require History=10 TTL=24h MaxAge=24h MaxBytes<=0`) | **Measured:** a record expires 24h after its last write; the `agent.request` stream's retention is separate and is the only retention D33 observes (`SR:938-940`). I1 is scoped to records that exist: an expired record is a gone loop (pre-existing property); a redelivery after expiry reads "not observable" → Retry (`SR:488-490`, `:574-575`) to `MaxDeliver`. semspec's 24h claim (`recovery-consumer/backstop.go:247`) is correct |
| "one outstanding delivery per lane" | `C:1162-1163` `MaxAckPending` fixed at 1 for `agent.task`, `agent.response`, `tool.result`; 10 elsewhere | Tool/response/task lanes are serialized per consumer; approval/signal/verdict lanes are not. |

## 1. Recovery decisions (one row per decision, not per function)

Legend for the last column: **AF** = applied-fact answers it; **DF(x)** = existing durable fact x answers it; **NEED(x)** = still needs x; **GONE** = decision disappears; **KEEP** = not a proof (rebuild / warm-cold fork / validation), survives unchanged.

| # | Where | Input | Question the decision answers | Facts read today | Verdict |
|---|---|---|---|---|---|
| D1 | `C:1310-1311` | task | Is the process task→loop map empty (cold)? | process map `HasActiveLoopForTask` | KEEP (warm/cold fork) |
| D2 | `SR:384-397` | task | Does AGENT_LOOPS hold `task.LoopID` and agree on task/role/model? | KV entity | DF(`LoopEntity.ID/TaskID/Role/Model`) |
| D3 | `SR:398-400` | task | Is the loop terminal → ACK without effects? | KV entity `State` | DF(`State`) |
| D4 | `SR:402-422` | task | Was the initial AgentRequest already published, or must it be rebuilt from the TaskMessage? | retained request on `agent.request.<loopID>` (`SR:63`) | AF (`PublishedRequestID` set ⇒ published; rebuild + republish otherwise; with L2 IDs a rebuilt R1 == retained R1, republish is dup-safe) |
| D5 | `SR:424`, `ST:349-430` | task/response/tool/approval | Rebuild ContextManager, tool cache, timeout, format from the retained request | retained request | KEEP (rebuild, not proof; brief keeps it) |
| D6 | `C:1339-1350`, `C:1406-1409` | task | Same-process retry after transient lineage NAK: reuse the unpublished spawn result? | process map `pendingTaskResults` | KEEP (same-process only; cold path is D2/D4) |
| D7 | `C:1532-1541` | task (birth failure) | Was a birth record already committed by another owner? | KV `Create` → `ErrKeyExists` → `DeepEqual` | DF(Create-once) |
| D8 | `SR:457-476` | model response | Does the warm map's loop agree with the loop encoded in RequestID? | process map + `loopID:req:` grammar (`SR:369-375`) | KEEP (correlation) |
| D9 | `SR:478-492` | model response | Cold: does AGENT_LOOPS hold the encoded loop? | KV entity | DF(entity presence) |
| D10 | `SR:494-513` | model response | Did durable authority move under the warm copy (terminal / PendingApproval drift)? | KV entity vs process entity | DF(`State`, `PendingApproval`, revision) |
| D11 | `SR:517-535` | model response | Is this response's RequestID the loop's CURRENT request? | retained request (`GetLastMsgForSubject`) `SR:524` | **AF, conditional on ruling Q4** (`PublishedRequestID == response.RequestID` ⇒ current; "older" ⇒ lower `(iteration, retry)` under the L2 grammar `<loopID>:req:<iteration>:<retry>`, #1328; at 68c14c8e IDs are UUID-suffixed, `ST:1364-1365`, and the classification is not decidable); retained read survives only as D5 source. `SR:524-529` quarantines a merely stale response; under Q4 older → ACK, newer → Retry, unparseable → Quarantine |
| D12 | `C:1569-1586` | model response | Process says terminal: is the durable marker terminal → ACK? | KV entity `State` | DF(`State`) |
| D13 | `H:1249-1254`, `H:1257-1264` | model response | Terminal-in-process ignore; `Iterations >= MaxIterations` fail | process entity (rebuilt from KV) | KEEP; with AF a redelivered response after the advancing Put is caught by D11 first |
| D14 | `SR:131-195` | model response (governance) | For each proposed call, was a verdict already retained on `agent.toolcall.{approved,rejected}.<executionID>`? | retained verdict (`SR:211`) | DF(retained verdict). Not correctness-critical: `GD:665` and the rule publisher stamp no MsgID, so a re-proposal re-fires the rule; the retained read saves a re-evaluation and duplicate audit pairs |
| D15 | `SR:197-235` | governance | Does the retained verdict correlate to the proposal (fingerprint, subject)? | retained verdict + `matchVerdictProposal` | DF (part of D14) |
| D16 | `GD:547-560`, `C:2585-2595` | governance verdict (redelivered) | Is there a live waiter for `ExecutionID`? No → Retry forever | process `waiters` map | **NEED**: a verdict redelivered after restart has no ACK path (retries to `MaxDeliver`); `VerdictPayload.RequestID`/`ExecutionID` (`GD:131-137`) vs `PublishedRequestID`/`PendingToolResults` answers it — **ruled Q6** (#1330, 2026-09-18): L4 owns a minimal ACK path with the tool lane's classification |
| D17 | `C:2146-2148` | tool result | Warm route for `ExecutionID` (and not approval-required)? | process map `toolCallToLoop` | KEEP (warm/cold fork) |
| D18 | `SR:556-569`, `C:2269-2306` | tool result | Does the result carry request_id/execution_id/ordinal and encode the loop; does ExecutionID re-derive? | payload identity + `EI:31` | KEEP (validation) |
| D19 | `SR:570-576` | tool result | Does AGENT_LOOPS hold the loop? | KV entity | DF(entity presence) |
| D20 | `SR:577-621` | tool result | Did the originating AgentResponse contain this execution at this ordinal/callID/name? What are its siblings? | retained response on `agent.response.<requestID>` | DF(retained response) — needed to rebuild the batch (queued siblings are not in KV); identity compare only |
| D21 | `SR:622-632` | tool result | Does the current retained request agree on role/model? | retained request | AF replaces (D22); role/model already in KV entity |
| D22 | `SR:651-665`, `SR:686-740` | tool result | `request.RequestID != result.RequestID`: was this result already applied into a later request? | retained request `Messages` + `buildToolMessages` rendering (`SR:687`, `SR:723`, `SR:735`) — **the hazard** | **AF, conditional on L2 (same caveat as D11):** a *known-older* `result.RequestID` ⇒ applied (the iteration cannot advance before `AllToolsComplete`, `H:2410-2414`); `== PublishedRequestID` ⇒ current ⇒ normal handler, idempotent via `PendingToolResults` key overwrite (`ST:1119`); a cold read first adopts a newer retained request into the record (design § 3.6), so the cold rebuild source is always the newest retained request. "Older" means lower `(iteration, retry)` under ruling Q4's grammar `<loopID>:req:<iteration>:<retry>` (lands in L2, #1328); at 68c14c8e IDs are UUID-suffixed and unordered (`ST:1364-1365`), so without Q4 the classification is not decidable — an unparseable ID quarantines, never falls to a content compare. **Semantics change:** deleting `SR:763-817` also deletes the `!reflect.DeepEqual(stored, result)` → Fatal quarantine at `SR:799-802`; a divergent duplicate for an already-stored ExecutionID in the current batch overwrites by key instead of quarantining — declared residual |
| D23 | `SR:634-636` | tool result | Truncate content to `ToolResultMaxBytes` so the rendering compare matches | config + content | GONE (only served D22) |
| D24 | `ST:473-521`, `ST:486-488` | tool result | Rebuild batch state; **decrement `Iterations`** because the branch Puts the advanced iteration before publishing the next request | KV entity `Iterations`, `PendingToolResults`, retained response | Rebuild KEEP; the `Iterations--` at `ST:486-488` is GONE once the advancing Put follows the request PubAck (design § 2) |
| D25 | `ST:435-468` | tool result / approval | Every stored result of this batch correlates; a preceding ordinal missing ⇒ retry | KV `PendingToolResults` | DF; `requirePreceding` GONE (serial dispatch means a later ordinal implies earlier ones are stored, but redelivery order is not needed as proof) |
| D26 | `SR:666-667`, `SR:821-852` | tool result | Loop terminal: was THIS result the one that produced the terminal (`entity.Result == result.Content`, `SR:843`; or max-iteration failure with full batch, `SR:846-848`)? | KV entity terminal fields + `PendingToolResults` + content equality | DF(`State` terminal) suffices for an effect-free ACK, as the cancel lane already does at `C:2518-2526`; the content-equality proof is GONE. Semantics change (Codex retries unproven terminal results forever, `SR:851`) — **ruled Q7** (#1330, 2026-09-18): effect-free ACK with a metric and an audit line, no retry-to-`MaxDeliver` |
| D27 | `SR:637-650`, `SR:763-817` | approval-required tool result | Is this gate status stale (a later phase consumed the gate)? | KV `State`, `PendingApproval`, `PendingToolResults`; falls to D22 for older RequestIDs (`SR:810`) | AF for the older-request branch; `SR:793-798` already answers the same-request branch from KV alone |
| D28 | `SR:669-670`, `SR:743-758` | approval-required tool result | Loop awaiting this exact call → re-echo `ApprovalPendingEvent` | KV `PendingApproval`; `validatePendingApprovalEvidence` reads retained request + response (`SR:984-1013`) | DF(`PendingApproval`); the request/response validation reduces to `PendingApproval.RequestID == PublishedRequestID` |
| D29 | `ARH:176-181` | approval response | Process holds the loop with a matching gate route? | process map | KEEP (warm/cold fork) |
| D30 | `ARH:182-215` | approval response | Durable loop awaiting approval with matching `ExecutionID`/`CallID`; else ACK inapplicable (`ARH:200-206`) or quarantine | KV entity | DF(`State`, `PendingApproval`) |
| D31 | `SR:857-866`, `SR:1015-1032` | approval response | Is the gated result stored and coherent with the gate? | KV `PendingToolResults[pending.ExecutionID]` | DF |
| D32 | `SR:867-899`, `SR:1034-1040` | approval response | Retained request == `PendingApproval.RequestID`; retained response present and tool_call | retained request + response | AF for the ID compare (`PendingApproval.RequestID == PublishedRequestID`); retained reads survive only as D5/D20 rebuild sources |
| D33 | `SR:921-981` | approval response | Required retained evidence absent: is absence proven by stream retention config and unchanged revision → fail `continuation_unavailable` | `stream.Info().Config` (`SR:938-940`), KV revision (`SR:948`) | NEED(retention observation) — required verbatim by #1146 Acceptance ("Confirmed missing required evidence durably fails continuation_unavailable"); independent of the applied fact |
| D34 | `SR:904-916` | approval response | Rebuild batch, drop queued siblings | retained response + KV | KEEP (rebuild) |
| D35 | `ARH:238-241` | approval response | Handler found no local gate (staleDrop) → Retry | process | KEEP |
| D36 | `ARH:255-279` | approval response (approve) | Publish `tool.execute` then CAS-clear the gate (`ARH:269`, `ARH:275`) | PubAck, KV revision | DF(TOOL_CALL_OUTCOMES replays a duplicate dispatch: `TC:710-713`, key `outcomes.go:82`) — publish-then-Update already closes the crash window |
| D37 | `ARH:130-149` | approval response (reject) | Synthetic result through `HandleToolResult` | same as tool result | AF via D22/D25 |
| D38 | `AS:21-86` | startup | Which loops await approval with a timeout → hydrate timer candidates | KV `WatchAll(MetaOnly)` + `Get` (`AS:69-73`) | DF(`State`, `PendingApproval.Timeout`) |
| D39 | `AS:134-169` | timer | Expired gate → publish `ApprovalResponse` to the wire (`AS:156`), consumed by D29-D37 | process snapshot of hydrated entities | KEEP |
| D40 | `C:2508-2526` | cancel signal | Terminal → ACK inapplicable; else hydrate and cancel through the terminal owner | KV entity | DF(`State`) |
| D41 | `C:1833-1845`, `C:1959-1964`, `C:1917-1927` | terminal (all lanes) | Authority unchanged (revision CAS); which terminal payload won (`COMPLETE_<loopID>` Create-once); publish then `Update(revision)` | KV revision, Create-once, PubAck | DF; the single terminal-persistence owner stays |
| D42 | `TR:65-110` | dispatch (UserMessage) | Is there a retained dispatch task for this stable task ID? | retained task on `agent.task.<taskID>` (hash `TR:65-74`) | DF — agentic-dispatch layer, outside L4, unchanged |
| D43 | `PS:76-122`, `agentic-model/component.go:616-627` | agent request (model side) | Was this RequestID already answered → ACK | retained response | DF — layer L2, stays |
| D44 | `C:1833-1843` (`:1838-1839`, `:1841-1842`) | terminal (all lanes, `persistTerminalOutcome`) | Is the record observable at this delivery's revision and still non-terminal, else Retry "terminal authority changed or is not observable"; does its TaskID match the marker, else Quarantine? | KV revision + `State` + `TaskID` | DF; **ruled 2026-09-18 (Q7 applied):** record terminal at the observed revision → effect-free ACK with metric and audit line, never Retry; revision conflict alone → Retry (CAS re-read, short-lived); TaskID conflict → Quarantine unchanged |
| D49 | `C:1846-1870` (`:1846`, `:1852-1856`, `:1861-1866`) | terminal (all lanes, redelivered) | Does this delivery's candidate terminal payload equal the saved `COMPLETE_<loopID>` payload that `selectTerminalOutcome` reads back on `ErrKeyExists` (Create-once at `C:1959-1960`; Result/Decision; Reason/Error), else Retry "lacks this delivery's compatible applied proof"? | KV Create-once terminal payload vs re-derived candidate — **content equality on the terminal owner, the twin of D26** | **DELETE the compare; ruled 2026-09-18 (Q7 + the Q4 identity principle):** (b) record not terminal and the loop's durable terminal exists → ADOPT it by identity (loop ID + terminal kind): the saved payload replaces the candidate (`:1857-1858`, `:1867-1869`, unchanged), publish proceeds with it (a terminal republish is an accepted duplicate, `main:spec.md:430-446`), the entity is written to match under `Update(revision)` (`C:1927`), ACK; content differences are logged at the audit line, never a disposition; (c) no durable terminal → Create, publish, `Update`, ACK. At 68c14c8e the "retained published terminal" the ruling names is this KV marker, not a stream read |
| D45 | `C:2162-2170` | tool result (warm route) | Warm route with no observed revision: is the loop observable in KV, else Retry? | KV revision (`:2163`, `:2168`) | DF(entity presence); Retry stays; the revision now feeds the CAS `Update` (design § 3) instead of being discarded |
| D46 | `C:2171-2177` | tool result (warm route) | Do the durable and process copies agree on TaskID/Role/Model, else Quarantine? | KV vs process entity | DF; Quarantine stays; consumes the same revision |
| D47 | `C:2178-2181` | tool result (warm route) | Has authority moved — durable terminal, or `PendingApproval` drift — → release process state, Retry | KV `State`, `PendingApproval` | DF; under the CAS re-read: terminal → Q7 effect-free ACK (as D26); gate mismatch → re-classify per the approval-lane rule (design § 5.4), not Retry "authority changed" |
| D48 | `C:2182-2190` | tool result (warm route) | Does this execution own the current gate (six-field `PendingApproval` compare), else Retry? | KV `PendingApproval` identity | DF; survives — an identity compare, not content; Retry until the gate's own CAS lands is correct and short-lived (one redelivery); I4 adds `PendingApproval.RequestID == PublishedRequestID` |

## 2. Every non-recovery Put of `LoopEntity` to KV (the applied fact must ride one of these)

| Site | Form | Order relative to the outputs it implies | Lane |
|---|---|---|---|
| `C:1405 → C:2396-2415` (`persistLoopState`, `Put` at `C:2411`) | plain `Put` | **Put, then** `publishResults` (`C:1410`) — birth: `agent.created` + R1 | task |
| `C:1532` | `Create` (birth on graph-birth failure) | before the failure publication | task |
| `C:1795 → C:2411` inside `persistHandlerResult` (`C:1782-1799`) | plain `Put` | **Put (`C:1795`), then** `publishResults` (`C:1798`) — covers: response→tool dispatch (`C:1618`), tool result mid-batch and tools-complete R(N+1) (`C:2257`), truncation retry R' (`C:1618`) | response, tool |
| `C:2319` (`persistApprovalGate`) | `Update(revision)` | Update, then publish `ApprovalPendingEvent` (`C:2323`) | tool (gate) |
| `ARH:275` | `Update(revision)` | publish `tool.execute` (`ARH:269`), then Update | approval |
| `C:1927` (`persistTerminalOutcome`) | `Update(revision)` | `COMPLETE_` Create (`C:1960`), publish (`C:1917`), then Update | all terminal lanes |
| `SR:961-976` (`settleAbsentApprovalEvidence`) | via terminal owner | as above | approval |

Observation (not a design): the only non-CAS writer is `C:2411`, reached from lanes that read `observedRevision` at `C:2163` / `C:1564` and then discard it. The entity written by `C:2411` is the process copy (`C:2401`), so a lane that wins the race between `C:2163` and `C:2411` (cancel signal at `C:1927`, approval at `ARH:275`) is overwritten. `MaxAckPending=1` (`C:1162-1163`) serializes only within one port's consumer.

## 3. Same-class collision table (durable primitive: "current published request + applied set on the loop record")

| Dimension | Evidence |
|---|---|
| Semantic class | "Which model request is outstanding for loop L, and which tool executions of that request have been applied" |
| Owners | agentic-loop only. Applied set: `LoopEntity.PendingToolResults` keys (`AG:54`, written at `ST:1119`, retained across the advance by `ST:1146-1160` with `clearResults=false` at `H:2612`, superseded by the next batch's first result at `ST:1100-1106`). Current request: **no durable owner** — today reconstructed from `GetLastMsgForSubject(agent.request.<loopID>)` (`SR:63`, `SR:276`) and process map `requestToLoop` (`ST:976-989`). |
| Catalogs | AGENT_LOOPS declared as `component.KVWritePort{Bucket: "AGENT_LOOPS"}` at `config.go:426`; ADR-028:61/141 (COMPLETE_ records); `openspec/specs/framework-bucket-catalog/spec.md` — `git grep -n AGENT_LOOPS origin/main -- openspec/specs/framework-bucket-catalog/spec.md` → 0 (no catalog descriptor). |
| Status | `LoopEntity.State` (five values, `AG:18-24`); no readiness key involved. |
| Lifecycle | Written at birth (`C:1405`), every handler result (`C:1795`), gate (`C:2319`), gate clear (`ARH:275`), terminal (`C:1927`); `COMPLETE_` Create-once (`C:1960`). Bucket policy at `internal/loopbucket/acquire.go:20,42-43`: `History: 10`, `TTL: 24h` (age eviction per key since last write), `MaxBytes<=0`, refused otherwise at startup; `COMPLETE_` markers share it. An expired record is a gone loop. |
| Ownership | Single writer component; per-port consumers with `MaxAckPending` 1/10 (`C:1162-1163`); no lease. |
| Readers (in-repo) | recovery paths D2-D41; trajectory query reads AGENT_TRAJECTORIES not loops (`C:2417-2419`). |
| Readers (sisters, read-only inventory) | **Control planes (Watch):** semspec `processor/execution-bridge/completion.go:22,25,39,54` and `review_completion.go:28,51` watch AGENT_LOOPS for terminal loops and translate them into `exec_produced` / `review_verdict_signal`; `processor/lesson-decomposer/component.go:901,924` and `processor/qa-reviewer/component.go:302,323` watch for completions. **Liveness:** semspec `processor/recovery-consumer/backstop.go:40,45,167,247` treats entry presence as liveness for orphan detection and asserts the 24h TTL at `:247`; `config.go:32,35,70` exposes `loops_bucket` (default `AGENT_LOOPS`) as an operator key. **Key-space parsers:** `pkg/health/orchestrate.go:14,73,92`, `detector_repeattoolfailure.go:230` (know both `<uuid>` and `COMPLETE_` keys). **Mirrors:** semsage `processor/ui-api/component.go:2,50,160,217`, `sse.go:15` (one SSE event per KV change), `http.go:72,137,297`, own struct `types.go:12`; semteams `cmd/semteams/main.go:492`, `cmd/semteams/approvalpause/doc.go:40` (reimplement against `LoopEntity.State`); semspec `cmd/semspec/watch_live.go:193-238`, `pkg/health/capture.go:75`; semmachina `internal/resume/pending.go:88` (records AGENT_LOOPS cannot answer its question). **Config coupling (L3 #1329 migration-note item, not L4 scope):** semspec `configs/e2e-claude.json:311,818` and `configs/e2e-gemini.json:345,874` set the agentic-loop `loops_bucket` key that the port-owned bucket retires. semsource, semconnect, semboids, semmem: 0 hits. |
| Writers | agentic-loop only (`git grep -n 'AGENT_LOOPS' origin/main -- processor | grep -v agentic-loop` not run; sister scan above shows readers only). |
| Recovery | This inventory. Closest same-shape instances: `TOOL_CALL_OUTCOMES` post-effect Create-once keyed by ExecutionID (`outcomes.go:82`, `TC:773`), `COMPLETE_<loopID>` Create-once (`C:1959-1964`), `PendingApproval` committed in the same Update as the state transition (`C:2319`). All three are "applied fact written atomically with the state it describes" — the problem shape L4 adopts; none is a pre-call marker. |

## 4. Adopter seam inventory (surfaces reached from outside this repo)

Surface A — `LoopEntity` JSON in AGENT_LOOPS gains one optional field.
1. What must they know: nothing to keep working (Go `encoding/json` ignores unknown fields; semsage decodes into a mirror struct). To *use* it: that `published_request_id` names the outstanding request and is not cleared at terminal.
2. If they do nothing: unchanged behaviour. No silent loss. Write cadence is unchanged — one KV write per settled input; a CAS failure writes nothing and re-handles on redelivery; identity adoption writes once — so semsage's one-SSE-event-per-change (`ui-api/sse.go:15`) sees no new event class, and the semspec watchers/liveness scan (Readers row) see `Update` exactly as they saw `Put`.
3. Where they find out: doc only (`LoopEntity` is in no generated schema; measured § 0). Acceptable because no correctness fact is at stake for a reader.
4. Should have to know: nothing. Gap: none for readers. Finding, sharpened: semspec `pkg/health/agent_response_walk.go:119-127` splits the subject on the **first colon** — the loop id is the UUID half, so the UUID-suffix worry is refuted, so L2's grammar must keep the loop-id half colon-free and the literal `:req:` separator (`SR:369-375`, `ST:1378-1386` parse it too); the suffix is unconstrained by any sister.

Surface B — tool authors / agentic-tools: no change; `ToolResult` identity fields (`agentic/tools.go` `request_id`, `execution_id`, `call_ordinal`) are already required by `C:2277-2282`.

Surface C — approval UIs publishing `ApprovalResponse`: no change; D29-D37 unchanged in shape.

Surface D — the framework-owned prediction check: nothing in L4 asks a caller to predict a value. The one prediction-shaped input in the Codex layer is the *rendering* compare (`SR:687`): recovery predicts what `buildToolMessages` will produce. Deleting it is the design.

## Adjacent claims on the territory (§ 5)

- `openspec/specs/agentic-loop/spec.md:212-213` (main): "A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler transitions a loop out of `state=running`" — scoped to the in-flight query; L4 does not derive in-flight from the record, but the spec delta must say so explicitly.
- `openspec/specs/agentic-tools/spec.md:452-537` (main): completed-outcome replay is the existing authority for duplicate `tool.execute` (D36).
- `openspec/specs/agentic-loop/spec.md:430-446` (main): terminal redelivery creates another terminal observation — duplicate terminal publication is accepted today.
- `docs/concepts/17-approval-flow.md:65-68` (main): "Restart-safe. `LoopEntity.PendingApproval` lives in the AGENT_LOOPS KV bucket" — the claim #1146 Acceptance calls false; must be corrected with L4.
- No active `openspec/changes/` entry touches agentic-loop (`ls openspec/changes` → only `archive`).
- ADRs: none mention #1146/#759/settlement (`git grep -l -i -E '1146|#759|restart-safe|settlement' origin/main -- docs/adr` hits are unrelated: 045, 046, 068, 094, 095, 098, 101).

## 6. Commands run (verbatim, in order)

```
git rev-parse origin/codex/gh1146-agentic-loop-restart; git merge-base <branch> origin/main
git diff --stat 461b6902 68c14c8e | tail -60
for f in $(git ls-tree -r --name-only <branch> -- processor/agentic-loop processor/agentic-model processor/agentic-dispatch processor/agentic-tools agentic | grep -v _test.go); do git show <branch>:$f > scratch/src/$f; done
gh issue view 1146 --json body -q .body; sed -n '43,97p'
grep -n '^func ' on: settlement_recovery.go state.go approval_response_handler.go approval_sweeper.go delivery_owner.go execution_identity.go inflight.go task_recovery.go provider_settlement.go handlers.go component.go governance_dispatcher.go
grep -n '\.Put(\|\.Update(\|\.Create(\|UpdateLoop(\|persistLoop' component.go handlers.go state.go approval_response_handler.go approval_sweeper.go settlement_recovery.go governance_dispatcher.go
grep -n 'recoverGovernance\|recoverTaskDelivery\|ensureResponseLoop\|recoverToolResult(\|recoverApprovalResponse\|restoreApprovalDeadlines\|releaseLoopTransientState\|GetLoopFor.*WithRecovery\|settlementEvidence\|pendingTaskResult' agentic-loop/*.go
grep -n 'GenerateRequestID\|:req:\|RequestID: \|RequestID = ' agentic-loop/*.go (non-test)
grep -n 'completedOutcome\|outcomeStore\|toolCallOutcomeKey\|publishStream(\|toolResultMessageID(' agentic-tools/component.go outcomes.go
git grep -n 'PublishToStreamWithMsgID' origin/main -- natsclient   # hits client.go:946,963,968,1056 (receiver is (m *Client), decl at :963)
git grep -n -i -E 'applied_execution|AppliedExecution|applied_ids|AppliedIDs|applied_results|AppliedResults|current_request_id|CurrentRequestID|published_request|PublishedRequest|active_request_id|ActiveRequestID|LastRequestID|last_request_id|iteration_request' <branch|origin/main> -- agentic processor schemas openspec/specs docs/adr   → 0 / 0
git show origin/main:agentic/state.go | grep -n 'PendingToolResults\|ExecutionID\|CallOrdinal\|RequestID'   → only :53 (call-ID keyed)
for f in $(git ls-tree -r --name-only <branch> -- schemas specs); do git show <branch>:$f | grep -l 'pending_tool_results\|max_iterations'; done   → 4 files, all max_iterations config; git show <branch>:specs/openapi.v3.yaml | grep -c pending_tool_results → 0
git grep -n 'AGENT_LOOPS' origin/main -- openspec/specs/framework-bucket-catalog/spec.md docs/adr
git grep -n -i 'restart\|redeliver\|settlement\|applied' origin/main -- 'openspec/specs/agentic-loop*/spec.md' 'openspec/specs/agentic-tool*/spec.md'
git grep -l -i -E '1146|#759|restart-safe|restart safety|settlement' origin/main -- docs/adr; ls openspec/changes
git show <branch>:processor/rule/publisher.go | grep -n 'MsgID\|Nats-Msg-Id\|fingerprint'   → 0
grep -n 'LoopEntity' scratch/src/agentic/payload_registry.go   → 0
sister scan (read-only, tracked files, non-test): git ls-files | grep -E '\.(go|ts)$' | xargs grep -n -E 'agentic\.LoopEntity|AGENT_LOOPS'  per sister dir
grep -rn --include='*.go' ':req:' /Users/coby/Code/c360 (excluding semstreams*, tests) | head -5
grep -n 'agentic.LoopEntity\|json.Unmarshal\|DisallowUnknownFields' semsage/processor/ui-api/{component,sse,types}.go
# after INVENTORY FAIL (inventory-pass.md), 2026-09-18:
git grep -n -E 'AGENT_LOOPS|loopbucket|TTL' 68c14c8e -- processor/agentic-dispatch/*.go processor/agentic-loop/internal natsclient/*.go service/*.go config/*.go; git show 68c14c8e:processor/agentic-loop/internal/loopbucket/acquire.go
per cited semspec/semteams/semsage pin: git -C <sister> show HEAD:<file> | sed -n '<n>p'   (all confirmed at the quoted lines)
sed -n '36,76p' scripts/inventory-verify.sh; scripts/inventory-verify.sh <this file> from /Users/coby/Code/c360/semstreams-wt/verify-68c14c8e
# design review round (2026-09-18): sed -n '20p;113p;304p;336p' processor/agentic-loop/metrics.go; sed -n '984,990p' processor/agentic-loop/settlement_recovery.go; grep -n 'type ToolResult struct' -A 14 agentic/tools.go; sed -n '24,40p' processor/agentic-loop/execution_identity.go; sed -n '349,356p;1100,1119p' processor/agentic-loop/state.go; git show origin/main:natsclient/client.go | sed -n '960,970p'
# coordinator rulings 2026-09-18 (terminal owner, warm lane, D11/D22, seam, lifecycle): sed -n '1833,1870p;1934,1995p' processor/agentic-loop/component.go; sed -n '18,21p;40,44p' processor/agentic-loop/internal/loopbucket/acquire.go  (pins 19/41 were mis-lined: 20 creates, 42-43 refuse)
```
Skills applied: `entity-or-bucket` (loaded; outcome: existing bucket AGENT_LOOPS, ground 1 — CAS atomicity with `Iterations`/`PendingToolResults`; no new bucket, no ADR trigger). `kv-or-stream`, `orchestration-check`, `new-payload`, `query-pattern`: not triggered (no new path, orchestration, payload type, or query access).

## 7. Pins (`task inventory:verify` grammar; generated with `sed -n "${n}p"` from `git show 68c14c8e:<path>`)

- `processor/agentic-loop/settlement_recovery.go:63` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:131` — `func (c *Component) recoverGovernance(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {`
- `processor/agentic-loop/settlement_recovery.go:384` — `entity, found, err := c.readLoopEntity(ctx, task.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:402` — `request, retained, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:424` — `if err := c.handler.loopManager.restoreLoopFromRequest(entity, request, nil); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:457` — `mappedLoopID, _ := c.handler.loopManager.GetLoopForRequest(response.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:484` — `entity, revision, err = c.readLoopEntityRevision(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:517` — `request, found, err := c.readRetainedAgentRequest(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:524` — `if request.RequestID != response.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:553` — `func (c *Component) recoverToolResult(`
- `processor/agentic-loop/settlement_recovery.go:577` — `response, found, err := c.readRetainedAgentResponse(ctx, result.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:622` — `request, found, err := c.readRetainedAgentRequest(ctx, result.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:634` — `if c.config.ToolResultMaxBytes > 0 && len(result.Content) > c.config.ToolResultMaxBytes {`
- `processor/agentic-loop/settlement_recovery.go:637` — `if agentic.IsApprovalRequired(result.Error) {`
- `processor/agentic-loop/settlement_recovery.go:651` — `if request.RequestID != result.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:656` — `applied, err := c.toolResultProvenInLaterRequest(request, calls, result)`
- `processor/agentic-loop/settlement_recovery.go:666` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:669` — `if entity.State == agentic.LoopStateAwaitingApproval {`
- `processor/agentic-loop/settlement_recovery.go:672` — `if err := c.handler.loopManager.restoreToolBatch(entity, request, response, result); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:686` — `func (c *Component) toolResultProvenInLaterRequest(request agentic.AgentRequest, calls []agentic.ToolCall, result agentic.ToolResult) (bool, error) {`
- `processor/agentic-loop/settlement_recovery.go:687` — `want := c.handler.buildToolMessages([]agentic.ToolResult{result})[0]`
- `processor/agentic-loop/settlement_recovery.go:723` — `resultIndex := index + int(result.CallOrdinal)`
- `processor/agentic-loop/settlement_recovery.go:735` — `} else if reflect.DeepEqual(stored, want) {`
- `processor/agentic-loop/settlement_recovery.go:743` — `func (c *Component) republishPendingApproval(`
- `processor/agentic-loop/settlement_recovery.go:763` — `func (c *Component) approvalRequiredResultSuperseded(entity agentic.LoopEntity, request agentic.AgentRequest, calls []agentic.ToolCall, result agentic.ToolResult) (bool, error) {`
- `processor/agentic-loop/settlement_recovery.go:793` — `if pending == nil && reflect.DeepEqual(stored, result) {`
- `processor/agentic-loop/settlement_recovery.go:821` — `func proveTerminalToolResultApplied(entity agentic.LoopEntity, requestID string, calls []agentic.ToolCall, result agentic.ToolResult) error {`
- `processor/agentic-loop/settlement_recovery.go:843` — `entity.Outcome == agentic.OutcomeSuccess && entity.Result == result.Content {`
- `processor/agentic-loop/settlement_recovery.go:857` — `func (c *Component) recoverApprovalResponse(ctx context.Context, approval agentic.ApprovalResponse, entity agentic.LoopEntity, revision uint64) (bool, natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/settlement_recovery.go:867` — `request, found, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:881` — `response, found, err := c.readRetainedAgentResponse(ctx, request.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:904` — `if err := c.handler.loopManager.restoreToolBatch(entity, request, response, result); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:921` — `func (c *Component) settleAbsentApprovalEvidence(ctx context.Context, entity agentic.LoopEntity, streamName, subject string, revision uint64) (bool, natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/settlement_recovery.go:938` — `if retention.Retention != jetstream.LimitsPolicy || retention.Discard != jetstream.DiscardNew ||`
- `processor/agentic-loop/settlement_recovery.go:975` — `decision, err := c.persistTerminalOutcome(ctx, HandlerResult{LoopID: entity.ID, State: agentic.LoopStateFailed,`
- `processor/agentic-loop/state.go:349` — `func (m *LoopManager) restoreLoopFromRequest(entity agentic.LoopEntity, request agentic.AgentRequest, batch *agentic.ChatMessage) error {`
- `processor/agentic-loop/state.go:435` — `func validatedToolBatchResults(entity agentic.LoopEntity, requestID string, calls []agentic.ToolCall, incoming agentic.ToolResult, requirePreceding bool) (map[string]agentic.ToolResult, bool, error) {`
- `processor/agentic-loop/state.go:473` — `func (m *LoopManager) restoreToolBatch(entity agentic.LoopEntity, request agentic.AgentRequest, response agentic.AgentResponse, incoming agentic.ToolResult) error {`
- `processor/agentic-loop/state.go:486` — `if len(results) == len(calls) && ordinaryBatch && entity.PendingApproval == nil && entity.Iterations > 0 {`
- `processor/agentic-loop/state.go:487` — `entity.Iterations--`
- `processor/agentic-loop/state.go:601` — `func (m *LoopManager) IncrementTruncationRetry(loopID string) int {`
- `processor/agentic-loop/state.go:1084` — `func (m *LoopManager) StoreToolResult(loopID string, result agentic.ToolResult) error {`
- `processor/agentic-loop/state.go:1100` — `if result.RequestID != "" {`
- `processor/agentic-loop/state.go:1139` — `func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {`
- `processor/agentic-loop/state.go:1146` — `func (m *LoopManager) toolResults(loopID string, clearResults bool) []agentic.ToolResult {`
- `processor/agentic-loop/state.go:1364` — `func (m *LoopManager) GenerateRequestID(loopID string) string {`
- `processor/agentic-loop/state.go:1365` — `return fmt.Sprintf("%s:req:%s", loopID, uuid.NewString())`
- `processor/agentic-loop/component.go:563` — `if err := c.restoreApprovalDeadlines(runCtx); err != nil {`
- `processor/agentic-loop/component.go:1162` — `if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {`
- `processor/agentic-loop/component.go:1163` — `fixed = 1`
- `processor/agentic-loop/component.go:1310` — `if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {`
- `processor/agentic-loop/component.go:1311` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/component.go:1405` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1410` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1532` — `revision, createErr = c.loopsBucket.Create(ctx, loopID, data)`
- `processor/agentic-loop/component.go:1564` — `entity, revision, err := c.ensureResponseLoop(ctx, *response)`
- `processor/agentic-loop/component.go:1569` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1588` — `result, err := c.handler.handleModelResponse(ctx, loopID, *response, c.recoverGovernance)`
- `processor/agentic-loop/component.go:1618` — `if err := c.persistHandlerResult(ctx, result, revision); err != nil {`
- `processor/agentic-loop/component.go:1795` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1798` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1917` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:1927` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/component.go:2147` — `loopID = c.findLoopIDForToolCall(toolResult.ExecutionID)`
- `processor/agentic-loop/component.go:2150` — `loopID, observedRevision, err = c.recoverToolResult(ctx, toolResult)`
- `processor/agentic-loop/component.go:2163` — `current, observed, readErr := c.readLoopEntityRevision(ctx, loopID)`
- `processor/agentic-loop/component.go:2178` — `if current.State.IsTerminal() || (process.PendingApproval != nil && !reflect.DeepEqual(process.PendingApproval, current.PendingApproval)) {`
- `processor/agentic-loop/component.go:2231` — `if err := c.persistApprovalGate(ctx, result, observedRevision); err != nil {`
- `processor/agentic-loop/component.go:2257` — `if err := c.persistHandlerResult(ctx, result, observedRevision); err != nil {`
- `processor/agentic-loop/component.go:2319` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/component.go:2323` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:2337` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:2411` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/component.go:2518` — `if current.State.IsTerminal() {`
- `processor/agentic-loop/handlers.go:1083` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:1257` — `if entity.Iterations >= entity.MaxIterations {`
- `processor/agentic-loop/handlers.go:1318` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls, propose); err != nil {`
- `processor/agentic-loop/handlers.go:1981` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2346` — `err = h.loopManager.StoreToolResult(loopID, toolResult)`
- `processor/agentic-loop/handlers.go:2379` — `if gated, err := h.checkApprovalGate(loopID, &entity, toolResult, &result); gated || err != nil {`
- `processor/agentic-loop/handlers.go:2465` — `if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {`
- `processor/agentic-loop/handlers.go:2478` — `if err := h.loopManager.UpdateLoop(*entity); err != nil {`
- `processor/agentic-loop/handlers.go:2566` — `err := h.loopManager.IncrementIteration(loopID)`
- `processor/agentic-loop/handlers.go:2612` — `allResults := h.loopManager.toolResults(loopID, false)`
- `processor/agentic-loop/handlers.go:2654` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:2730` — `func (h *MessageHandler) buildToolMessages(results []agentic.ToolResult) []agentic.ChatMessage {`
- `processor/agentic-loop/approval_response_handler.go:176` — `entity, getErr := c.handler.GetLoop(response.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:182` — `persisted, revision, err := c.readLoopEntityRevision(ctx, response.LoopID)`
- `processor/agentic-loop/approval_response_handler.go:200` — `if persisted.State != agentic.LoopStateAwaitingApproval || pending.ExecutionID != response.ExecutionID {`
- `processor/agentic-loop/approval_response_handler.go:206` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-loop/approval_response_handler.go:217` — `settled, decision, err := c.recoverApprovalResponse(ctx, response, persisted, revision)`
- `processor/agentic-loop/approval_response_handler.go:238` — `if result.staleDrop {`
- `processor/agentic-loop/approval_response_handler.go:269` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:275` — `if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {`
- `processor/agentic-loop/approval_sweeper.go:21` — `func (c *Component) restoreApprovalDeadlines(ctx context.Context) (restoreErr error) {`
- `processor/agentic-loop/approval_sweeper.go:69` — `entity, found, err := c.readLoopEntity(ctx, key)`
- `processor/agentic-loop/approval_sweeper.go:156` — `if err := c.publishApprovalResponseToWire(ctx, response); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:131` — `type VerdictPayload struct {`
- `processor/agentic-loop/governance_dispatcher.go:557` — `// source for redelivery, including after the waiter has been recreated.`
- `processor/agentic-loop/governance_dispatcher.go:560` — `slog.String("decision", decision))`
- `processor/agentic-loop/governance_dispatcher.go:665` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-dispatch/task_recovery.go:65` — `func stableDispatchTaskID(msg agentic.UserMessage) string {`
- `processor/agentic-dispatch/task_recovery.go:90` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-model/provider_settlement.go:76` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:626` — `c.requestsProcessed++`
- `processor/agentic-tools/component.go:710` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:713` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/component.go:773` — `if err := c.persistAndPublishOutcome(ctx, call, result, outcomePathNew, true); err != nil {`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-tools/outcomes.go:82` — `func toolCallOutcomeKey(executionID string) string {`
- `processor/agentic-tools/outcomes.go:86` — `func toolResultMessageID(executionID string) string {`
- `agentic/state.go:46` — `type LoopEntity struct {`
- `agentic/state.go:52` — `Iterations         int                   `json:"iterations"``
- `agentic/state.go:54` — `PendingToolResults map[string]ToolResult `json:"pending_tool_results,omitempty"` // ExecutionID; synthetic failures use CallID`
- `agentic/state.go:74` — `PendingApproval *PendingApprovalState `json:"pending_approval,omitempty"``
- `agentic/state.go:148` — `e.PendingApproval = nil`
- `agentic/state.go:226` — `e.Iterations++`
- `processor/agentic-loop/config.go:426` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/component.go:1854` — `if !ok || marker.State != agentic.LoopStateComplete || marker.Outcome != agentic.OutcomeSuccess || marker.Result != saved.Result ||`
- `processor/agentic-loop/component.go:1855` — `prepared.Result != saved.Result || !reflect.DeepEqual(prepared.Decision, saved.Decision) {`
- `processor/agentic-loop/component.go:1856` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("selected success for loop %q lacks this delivery's compatible applied proof", result.LoopID)`
- `processor/agentic-loop/component.go:1863` — `(marker.Outcome != agentic.OutcomeFailed && marker.Outcome != agentic.OutcomeTruncated) ||`
- `processor/agentic-loop/component.go:1865` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("selected failure for loop %q lacks this delivery's compatible applied proof", result.LoopID)`
- `processor/agentic-loop/component.go:2168` — `if observedRevision == 0 {`
- `processor/agentic-loop/component.go:2175` — `if current.TaskID != process.TaskID || current.Role != process.Role || current.Model != process.Model {`
- `processor/agentic-loop/component.go:2178` — `if current.State.IsTerminal() || (process.PendingApproval != nil && !reflect.DeepEqual(process.PendingApproval, current.PendingApproval)) {`
- `processor/agentic-loop/component.go:2185` — `(pending.RequestID != toolResult.RequestID || pending.ExecutionID != toolResult.ExecutionID ||`
- `processor/agentic-loop/component.go:2189` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("tool execution %q does not own the current gate", toolResult.ExecutionID)`
- `processor/agentic-loop/settlement_recovery.go:799` — `if !reflect.DeepEqual(stored, result) {`
- `processor/agentic-loop/settlement_recovery.go:802` — `}`
- `processor/agentic-loop/component.go:1838` — `if revision == 0 || observed != revision || current.State.IsTerminal() {`
- `processor/agentic-loop/component.go:1839` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("terminal authority for loop %q changed or is not observable", result.LoopID)`
- `processor/agentic-loop/component.go:1841` — `if current.TaskID != marker.TaskID {`
- `processor/agentic-loop/component.go:1846` — `selected, err := c.selectTerminalOutcome(ctx, result.LoopID, marker.TaskID, candidate)`
- `processor/agentic-loop/component.go:1852` — `case *agentic.LoopCompletedEvent:`
- `processor/agentic-loop/component.go:1861` — `prepared, ok := candidate.(*agentic.LoopFailedEvent)`
- `processor/agentic-loop/component.go:1959` — `key := "COMPLETE_" + loopID`
- `processor/agentic-loop/component.go:1960` — `if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:20` — `bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:43` — `return nil, fmt.Errorf("loop bucket %q policy: observed History=%d TTL=%s MaxAge=%s MaxBytes=%d; require History=10 TTL=24h MaxAge=24h MaxBytes<=0 (no reconciliation)", name, status.History(), status.TTL(), info.Config.MaxAge, info.Config.MaxBytes)`
- `processor/agentic-loop/execution_identity.go:31` — `func deriveToolExecutionID(requestID, callID string, ordinal uint32) string {`
- `processor/agentic-loop/execution_identity.go:36` — `binary.BigEndian.PutUint32(ordinalBytes[:], ordinal)`
- `processor/agentic-loop/metrics.go:20` — `approvalDecisionsInapplicable  prometheus.Counter`
- `processor/agentic-loop/metrics.go:113` — `approvalDecisionsInapplicable: prometheus.NewCounter(prometheus.CounterOpts{`
- `processor/agentic-loop/metrics.go:304` — `_ = registry.RegisterCounter("agentic-loop", "approval_decisions_inapplicable_total", metrics.approvalDecisionsInapplicable)`
- `processor/agentic-loop/metrics.go:336` — `_ = prometheus.DefaultRegisterer.Register(metrics.approvalDecisionsInapplicable)`
- `processor/agentic-loop/settlement_recovery.go:984` — `func validatePendingApprovalEvidence(entity agentic.LoopEntity, request agentic.AgentRequest, calls []agentic.ToolCall) (agentic.ToolResult, error) {`
- `processor/agentic-loop/state.go:356` — `if entity.ID != request.LoopID || entity.Role != request.Role || entity.Model != request.Model {`
- `agentic/tools.go:629` — `type ToolResult struct {`
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `processor/agentic-loop/state.go:441` — `return nil, false, fmt.Errorf("result for preceding execution %q is not yet observable", call.ExecutionID)`
- `processor/agentic-loop/state.go:1100` — `if result.RequestID != "" {`
- `processor/agentic-loop/state.go:1161` — `entity.PendingToolResults = nil`
- `processor/agentic-loop/approval_response_handler.go:130` — `func (h *MessageHandler) handleRejectedApproval(ctx context.Context, loopID string, pending agentic.PendingApprovalState, response agentic.ApprovalResponse) (HandlerResult, error) {`
- `processor/agentic-loop/state.go:1144` — `// transition retains its durable evidence until the new request's first result`
