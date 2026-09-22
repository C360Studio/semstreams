# Design — agentic-loop-durable-applied-facts (#1330, restart-safety L4)

> Every `file:line` is at `68c14c8e` unless prefixed `main:`. Owner rulings: #1330 comment, 2026-09-18, "as recommended
> on all eight" (Q1–Q8) and, in its own comment the same day, terminal outcome adoption (§ 1). Premise correction
> (recorded): RequestIDs are UUID-minted at `68c14c8e` (`state.go:1364-1365`) and `af829616` (`state.go:1131-1132`);
> deterministic IDs are L2's deliverable (#1328). Abbreviations as in `inventory.md`.

## 1. Rulings applied (2026-09-18, #1330)

| Was | Decision |
|---|---|
| Q1 birth order | Birth keeps Put → publish (`C:1405` → `:1410`). A task redelivered while the record is at iteration 0 republishes R1 unconditionally (no evidence read). |
| Q2 carrier form | The non-terminal carrier becomes `Update(observedRevision)`; in L4 scope. |
| Q3 applied set | `PendingToolResults` keys ARE the applied execution IDs; no separate field. |
| Q4 ID grammar (lands in #1328) | `<loopID>:req:<iteration>:<retry>`; the truncation-retry ordinal is derived from `PublishedRequestID`; recovery ADOPTS an already-published next request by identity (`readExact` = `GetLastMsgForSubject` on `agent.request.<loopID>`, `SR:56-71`) instead of republishing. Applied to the cold read in § 3.6 (coordinator, 2026-09-18, on the design review's BLOCKING). |
| Q5 duplicates | Request publishes stamp RequestID as `Nats-Msg-Id` via the existing `PublishToStreamWithMsgID` (`main:natsclient/client.go:963`); the window is a bonus, not the guarantee. `agent.created` duplicates accepted. |
| Q6 verdict ACK | L4 owns a minimal ACK path for a verdict with no waiter, using the tool lane's classification. |
| Q7 terminal + unproven | Effect-free ACK with a metric and an audit line. |
| Q8 retention proof | `settleAbsentApprovalEvidence` (`SR:921-981`) stays unchanged. |
| Terminal outcome adoption | #1330, owner ruling 2026-09-18 (terminal outcome adoption), same standing as Q1–Q8: a redelivered terminal adopts the loop's durable terminal by identity (loop ID + terminal kind); content differences are logged, never a disposition (§ 5.7). |

## 2. The field

```go
// agentic/state.go, LoopEntity — beside PendingToolResults (:54)
// PublishedRequestID names the AgentRequest outstanding for this loop: the
// RequestID whose PubAck preceded the KV update that wrote this record (I1).
// Set at birth (R1) and by every request-minting transition; never cleared.
PublishedRequestID string `json:"published_request_id,omitempty"`
```

Applied execution IDs: `PendingToolResults` keys (`AG:54`, written at `ST:1107-1119`, kept through the advance at
`H:2612` with `clearResults=false`, superseded by the next batch's first result at `ST:1100-1106`). Gate identity:
`PendingApproval.RequestID` (`agentic/state.go:158`) already exists; I4 binds it to the new field.
`LoopEntity` is in no generated schema or OpenAPI and is not a registered payload (measured, `inventory.md` § 0), so
`task schema:generate` is untouched.

## 3. The carrier: order, form, identity adoption

Carrier: `persistLoopState` (`C:2396-2415`) reached from `persistHandlerResult` (`C:1782-1799`) and birth (`C:1405`).

1. **Order (non-terminal results):** `publishResults` (`C:1798`) moves before the write (`C:1795`), so
   `PublishedRequestID` is written only after the request's PubAck. The terminal owner already publishes before its
   Update (`C:1917` → `:1927`); the approval owner already does (`ARH:269` → `:275`); the gate keeps
   Update → publish (`C:2319` → `:2323`) because a redelivered `approval_required` result re-echoes the event (§ 5.4).
   Birth keeps Put → publish (Q1).
2. **Form:** `Update(observedRevision)` replaces `Put` at `C:2411`. The tool lane reads the revision at `C:2163`, the
   response lane at `C:1564`; both currently discard it. A lost race (cancel at `C:1927`, gate clear at `ARH:275`)
   now fails the write → Retry → re-read → the classification below answers. `MaxAckPending=1` (`C:1162-1163`)
   serializes one port only.
3. **Identity adoption (Q4):** before publishing a minted next request R' the carrier calls
   `readRetainedAgentRequest` (`SR:273-323`, identity checks only; the newest message on `agent.request.<loopID>`) and
   orders the retained RequestID against R' (Q4): == R' → adopt (skip publish; the retained body is authoritative);
   absent or == current R → publish with `Nats-Msg-Id = R'` (Q5); anything else (older than R, beyond R', unparseable)
   → Quarantine as a conflict (`errs.WrapFatal` → `loopSettlementDecision`, `SR:1042-1051`). No content compare.
4. **Setting the field:** `LoopManager.SetPublishedRequest(loopID, requestID)` at the three minting sites (`H:1083`
   via `buildTaskResultFromRequest`, `H:1981`, `H:2654`); the marshal at `C:2406` carries it.
5. **Retry ordinal:** `IncrementTruncationRetry` (`ST:601-606`, process-local) is replaced by parsing the `<retry>`
   part of `PublishedRequestID` (`looprequest`, added in THIS change — L2 declined to export a parser with no reader, deviation accepted by the coordinator 2026-09-19 on #1328; task 1.0).
6. **Cold read, step 0 — one rule for every lane but task (Q4 applied to the cold path; coordinator, 2026-09-18):** a
   process with no memory of the loop reads the entity + revision, then the newest retained request on
   `agent.request.<loopID>` (`SR:63`, the only reader; no RequestID-addressed form exists), and orders its RequestID
   against `PublishedRequestID`. Equal → current, continue. Newer (higher `(iteration, retry)`) → **adopt first**:
   `Update(revision)` the record to that request — `PublishedRequestID = R(N+1)`, `Iterations` = its parsed iteration
   (I3), `PendingToolResults = nil` — one eviction ahead of the normal path, which retains R(N)'s batch at the advance
   (`toolResults(loopID, false)` at `H:2612`; doc `ST:1144-1146`) and evicts it at the next batch's first result
   (`StoreToolResult`, `ST:1100-1105`). Harmless (review pass 3): every surviving reader classifies a redelivered input
   older-by-request before consulting membership (§ 5.3 step 2, § 5.6), Q7's terminal-unproven check reads absence,
   and `validatedToolBatchResults` no-ops on an empty map; no entry is synthesized, so nothing is derived from any message body and no surviving validator
   (`validatedToolBatchResults`, `ST:441-443`) ever sees a synthetic entry — and, when `PendingApproval != nil`,
   `PendingApproval = nil` with `State = running`: by I4 the gate's RequestID equals `PublishedRequestID`, so it is always
   older than the adopted request, and R(N+1) can only have been minted after that gate's result completed the batch
   (`H:2654` runs on the batch's last result). The adopt writes every field the advance implies, so I1–I4 hold on the
   written record by construction — then classify the redelivered input against the UPDATED record. Older or unparseable → Quarantine (conflict). The rebuild source
   (`restoreLoopFromRequest`, `ST:349-430`) is always that newest retained request, never "the request for R", so it has
   no `PublishedRequestID` mismatch to refuse. Task lane: no step 0 — Q1 rules iteration 0 (no evidence read; a duplicate
   R1 is answered by the MsgId window or L2 reuse) and an advanced record already answers "applied" (§ 5.1.4).

## 4. Invariants (spec home: the ADDED requirement in `specs/agentic-loop/spec.md`)

For every AGENT_LOOPS record of a non-terminal loop L at revision r:
- I1. `PublishedRequestID = R` ⇒ while the record exists, an `AgentRequest{RequestID: R, LoopID: L}` is durably
  retained on `agent.request.L`. I1 is scoped to records that exist: AGENT_LOOPS is `History: 10, TTL: 24h` and refuses
  any other policy at startup (`internal/loopbucket/acquire.go:20,42-43`, measured 2026-09-18); an expired record is a
  gone loop (pre-existing property) and a redelivery after it reads "not observable" → Retry (`SR:488-490`, `:574-575`).
- I2. Every `PendingToolResults[e]` with `RequestID == R` names an ExecutionID derived from a tool call of the retained
  response for R (`deriveToolExecutionID`, `execution_identity.go:31-40`) — membership only. That its conversation
  effect is the tool message the successor request carries is a consequence the normal handler's tests already prove;
  recovery never checks it, and neither does the property below. An adopted record (§ 3.6) carries an empty set; I2 holds vacuously until the next batch.
- I3. `Iterations` changes only in an update whose `PublishedRequestID` also changes.
- I4. `PendingApproval != nil` ⇒ `PendingApproval.RequestID == PublishedRequestID`. Step 0 (§ 3.6) preserves I4 by clearing the gate in the same update that advances `PublishedRequestID`.

PBT decision (`docs/contributing/01-testing.md`): I1–I4 hold over action sequences (deliver / crash at W1–W4 /
redeliver), so one bounded Rapid state-machine property over an in-memory KV plus a fake retained-stream reader (the
`loopSettlementEvidenceReader` seam, `SR:28-32`) covers the tool and response lanes and checks I2 as membership, never by
rendering; the approval lane has three shapes and uses named examples.

## 5. Per lane: algorithm and crash windows

R = current `PublishedRequestID`; W1 = crash before effect; W2 = after effect, before update; W3 = after update, before
ACK; W4 = next request PubAck'd, crash before the update. "Classify" = terminal → effect-free ACK (Q7); `RequestID == R`
→ current; older ordinal → applied, ACK; unknown/future ordinal → quarantine (`loopSettlementDecision`, `SR:1042-1051`).
"Cold" = a process with no memory of the loop: § 3.6 step 0 runs first on every lane but task.

### 5.1 Task (`agent.task`)
1. Warm map hit → `HandleTask` dedup (`H:877-890`), unchanged.
2. Cold: read entity by `task.LoopID`. Absent → normal birth (Put → publish, R1 with MsgId). Present: verify
   task/role/model (`SR:391-397`); terminal → ACK (`SR:398-400`).
3. Present, `Iterations == 0`, `PendingToolResults` empty: rebuild R1 from the TaskMessage (`SR:414-421`), publish it
   unconditionally with `Nats-Msg-Id = R1` (Q1, Q5), rebuild ContextManager (`SR:424`), ACK.
4. Present and advanced: applied → ACK.
Windows: W1 redo. W2 (record written, R1 unpublished) → step 3 publishes. W3 → step 3 republishes; the window dedups
or L2 reuse answers. No W4 at birth (Put precedes publish by ruling).

### 5.2 Model response (`agent.response.<requestID>`)
1. Loop from the RequestID grammar (`SR:369-375`); read entity + revision (`SR:484`).
2. Cold → step 0 (§ 3.6). Classify on `response.RequestID` vs R. Current: cold → rebuild ContextManager from the newest
   retained request, which after step 0 is R (`ST:349-430`); `handleModelResponse` with `recoverGovernance` (`SR:131-195`).
   Older → ACK with metric. `RequestID` newer than R (its update not yet landed) → Retry "not yet observable"
   (the shape at `SR:522`).
3. Effects: proposals (`GD:665`), `tool.execute` publishes, or the terminal owner. Truncation retry mints R' (`H:1981`,
   retry ordinal from R): identity-adopt or publish (§ 3.3), then Update `PublishedRequestID = R'`.
4. Update(revision) after PubAck; a tool_call response leaves `PublishedRequestID = R`.
Windows: W1 redo. W2 → re-proposal re-fires the rule (no MsgID either side, measured); duplicate `tool.execute`
replays via TOOL_CALL_OUTCOMES (`TC:710-713`). W3 → step 2 sees R unchanged, re-runs; same idempotency. W4 (retry
lane only): R' retained, record says R → warm: re-runs, finds R' by identity, adopts, Update, ACK; cold: step 0 adopts
R' first, then the response for R classifies older → ACK.

### 5.3 Tool result (`tool.result`)
1. Validate identity (`C:2277-2304`); read entity + revision (`C:2163`). Warm route (inventory D45-D48, all four now
   consume the revision § 3 makes the CAS input): not-yet-observable → Retry (stays); process-vs-KV identity conflict →
   Quarantine (stays); `C:2178-2180` under the CAS re-read: terminal → Q7 effect-free ACK, gate mismatch → re-classify
   per § 5.4 (not Retry "authority changed"); the six-field gate-ownership compare (`C:2185-2189`) is identity, not
   content — Retry until the gate's own CAS lands is correct and short-lived. Cold → step 0 (§ 3.6). Classify on
   `result.RequestID` vs R.
2. Older → applied (the iteration cannot advance before `AllToolsComplete`, `H:2410-2414`) → ACK.
3. Current: cold → rebuild from the newest retained request (after step 0 it is R) and retained response R
   (`ST:473-521` without `:486-488`).
   `HandleToolResult` stores the result (`H:2346`; idempotent key overwrite at `ST:1119`), dispatches the next queued
   sibling, or on the last result mints R(N+1) (`H:2654`).
4. Publish `tool.execute`, or identity-adopt / publish R(N+1) (§ 3.3); then Update(revision) with `Iterations`,
   `PendingToolResults`, `PublishedRequestID = R(N+1)`; ACK.
Windows: W1 redo. W2 mid-batch → re-store, re-dispatch; duplicate `tool.execute` replays (`TC:710-713`). **W4**:
R(N+1) is retained, the record still says R. Warm redelivery: current → re-store (no-op) → `AllToolsComplete` →
`handleToolsComplete` mints R(N+1) → `readRetainedAgentRequest` returns R(N+1) → adopted, **not republished** → Update
→ ACK. Cold redelivery (process replaced): step 0 reads R(N+1) > R → adopts first (Update: `PublishedRequestID =
R(N+1)`, `Iterations = N+1`, applied set by identity) → the result for R now classifies older → ACK, nothing published;
no rebuild of R is attempted, so `restoreLoopFromRequest` has nothing to refuse (the design review's BLOCKING). A response
for R(N+1) arriving before that update → Retry until observable. W3 → step 2: `R(N+1) ≠ result.RequestID`, older → ACK.

### 5.4 Approval-required tool result (gate)
Steps 1–2 as § 5.3. Current and `State == awaiting_approval && PendingApproval.ExecutionID == result.ExecutionID` →
re-echo `ApprovalPendingEvent` (`SR:743-758` minus its retained-request validation) → ACK. Current, running,
`PendingToolResults[e]` holds this gate status and `PendingApproval == nil` → gate consumed → ACK (`SR:793-795`).
Else `gateForApproval` (`H:2459-2482`) → `persistApprovalGate` Update → publish → ACK. Windows: W1 redo; W2/W3 →
re-echo; no W4 (the gate's Update precedes its publish). Cold → step 0 first, as § 5.3.

### 5.5 Approval response (`agent.approval_response.<loopID>`)
1. Step 0 (§ 3.6): read entity + revision, adopt a newer retained request (which clears the gate); then require
   `awaiting_approval` with matching `ExecutionID`/`CallID` (`ARH:182-215`); else ACK inapplicable / quarantine, unchanged.
2. Require I4 and the gated result in `PendingToolResults` (`SR:1015-1032`). Cold → rebuild from retained R and its
   response (`SR:904-916`). Retained R or its response absent → `settleAbsentApprovalEvidence` (`SR:921-981`, Q8).
3. Approve/modify: publish `tool.execute`, then Update clearing the gate (`ARH:269-278`, unchanged). Reject:
   synthetic result through § 5.3.
Windows: W1 (crash before the `tool.execute` publish) → redo, record unchanged. W2 (published, gate not cleared) →
redelivery re-publishes `tool.execute`; the duplicate execution replays by ExecutionID (`TC:710-713`); Update clears; ACK.
W3 (gate cleared, no ACK) → step 1: no longer `awaiting_approval` → ACK inapplicable (`ARH:182-215`, existing metric at
`ARH:204`). W4 is real on the reject/timeout branch (the sweeper's auto-rejects land here too, `AS:156`): `handleRejectedApproval` feeds a
synthetic result to `HandleToolResult` (`ARH:130-149`), which on the batch's last result mints R(N+1) (`H:2654`);
`publishResults` (`ARH:269`) precedes the single `Update` (`ARH:275`) that clears the gate and records the advance, so a
crash between them leaves R(N+1) retained with the record `awaiting_approval` at R. Redelivery → step 0 adopts R(N+1)
and clears the gate in the same CAS write (§ 3.6) → step 1 finds the record no longer `awaiting_approval` → ACK
inapplicable, nothing republished: the W3 path. The approve/modify branch mints no request (it publishes `tool.execute`)
and has no W4.

### 5.6 Governance verdict (Q6), timer, startup, cancel
- Verdict: `HandleVerdict` returns no-waiter (`GD:557-560`). The component (`C:2585-2596`) then reads the entity by
  `verdict.LoopID` (`GD:133`) and classifies: terminal → ACK; `verdict.RequestID ≠ R` and older → ACK with metric;
  `verdict.ExecutionID ∈ PendingToolResults` → consumed → ACK; else Retry (the response redelivery re-runs governance
  and reads this verdict by identity, `SR:197-235`).
- Timer/startup: `restoreApprovalDeadlines` (`AS:21-86`) and the sweeper unchanged; they feed § 5.5.
- Cancel: unchanged (`C:2506-2572`); its terminal Update is one of the writers § 3.2 protects.

### 5.7 Terminal (all lanes; the single owner `persistTerminalOutcome`, `C:1802-1931`)
**#1330, owner ruling 2026-09-18 (terminal outcome adoption)**, same standing as Q1–Q8 (applies Q7 + the Q4 identity
principle; inventory D44, D49). On a redelivered terminal, after the TaskID check (`C:1841-1842`, Quarantine, unchanged):
- (a) record terminal at the observed revision → effect-free ACK with metric and audit line (Q7); today `C:1838-1839`
  retries. A revision conflict alone → Retry (CAS re-read, short-lived).
- (b) record not terminal and the loop's durable terminal exists — `selectTerminalOutcome` (`C:1846`) does a
  `COMPLETE_<loopID>` Create-once (`C:1959-1960`) and on `ErrKeyExists` reads the saved payload back; at 68c14c8e this
  KV marker, not a stream read, is the "retained published terminal" the ruling names → adopt it by identity (loop ID +
  terminal kind): the saved payload replaces the candidate (`C:1857-1858`, `:1867-1869`, unchanged), publish proceeds
  with it (`C:1917`; a terminal republish is an accepted duplicate, `main:spec.md:430-446`), the entity is written to
  match under `Update(revision)` (`C:1927`), ACK. Content differences between this delivery's candidate and the saved
  payload are logged at the audit line, never a disposition; the compares at `C:1852-1856`/`:1861-1866` are deleted.
- (c) no durable terminal → normal path: Create, publish, `Update`, ACK.
Windows: crash after the Create or the publish, before `Update` → (b) on redelivery; after `Update`, before ACK → (a).

## 6. Deleted from the Codex layer / survives

Deleted (`settlement_recovery.go` 1,051 → ≈ 430): `toolResultProvenInLaterRequest` (686-740),
`approvalRequiredResultSuperseded` (763-817), `proveTerminalToolResultApplied` (821-852), the retained-request compares
in `ensureResponseLoop` (517-535) and `recoverToolResult` (622-665), `validatePendingApprovalRequest` (1034-1040), the
compare-only truncation (634-636); `state.go`: `Iterations--` (486-488), `requirePreceding` (435-444),
`IncrementTruncationRetry`/`ResetTruncationRetry` (601-616); `component.go`: the terminal content compares (1852-1856,
1861-1866) and the terminal-at-revision Retry (1838-1839), § 5.7. Tests deleted or rewritten: `settlement_recovery_test.go`
(818), `tool_result_recovery_test.go` (179), `terminal_tool_recovery_test.go` (175), and the layout assertions in
`tool_result_redelivery_integration_test.go` (255), `terminal_tool_redelivery_integration_test.go` (188),
`settlement_recovery_integration_test.go` (161).

Survives: evidence reader and addresses (`SR:20-127`), `recoverGovernance` + `readRetainedGovernanceVerdict`
(131-235), `readLoopEntity[Revision]` (237-271), `readRetainedAgentRequest/Response` (273-367; identity + rebuild),
`loopIDFromRequestID`, `recoverTaskDelivery` (§ 5.1), `republishPendingApproval` (simplified),
`settleAbsentApprovalEvidence` (Q8), `validatePendingApprovalResult`, `validatePendingApprovalEvidence` (`SR:984-1013`,
reduced to I4 + `validatePendingApprovalResult`), `loopSettlementDecision`; `restoreLoopFromRequest` (`ST:349-430`, fed the
adopted record, § 3.6); `restoreToolBatch` minus the decrement; `execution_identity.go`;
`delivery_owner.go`; `approval_sweeper.go`; `agentic-dispatch/task_recovery.go`; `agentic-model/provider_settlement.go`
(L2); the shared handlers; the single terminal owner (`C:1802-1931`) minus the compares § 5.7 deletes; the e2e harness
(`test/e2e/harness/processbarrier`, `test/e2e/scenarios/agentic/stage_a_process_replacement.go`,
`approval_restart.go`) with assertions re-pointed at KV facts.

## 7. Strongest case against, each marked

1. KV record growth — **answered**: one string; content unchanged; residual bound in `proposal.md` § Declared cost.
2. Batch-size bound on the applied set — **answered**: `PendingToolResults` keys, bounded by the model's `tool_calls`.
3. Approval-required results — **answered**: the gate is already CAS-committed; L4 adds I4 and the re-echo; the
   reject-minted W4 (`ARH:269` before `:275`) is adopted by step 0, which clears the gate with the advance (§ 5.5).
4. Terminal loop + unproven result — **answered by ruling Q7**: effect-free ACK with metric and audit line (as the
   cancel lane does at `C:2518-2526`); Codex's retry-to-`MaxDeliver` (`SR:851`) is removed.
5. Governance verdict retention — **answered by measurement** (re-proposal re-fires the rule) and **by ruling Q6** for
   the verdict lane's ACK; **residual**: duplicate proposed/verdict pairs (declared cost).
6. Stale vs conflict — **answered by ruling Q4**: the ordinal grammar orders RequestIDs; unknown/future quarantines.
7. Two lanes, one record — **answered by ruling Q2**: `Update(observedRevision)`.
8. Retention-absence proof — **answered by ruling Q8**: unchanged.
9. Deterministic retry IDs — **answered by ruling Q4**: retry ordinal derived from `PublishedRequestID`.
10. Adopted-request drift — **residual**, declared in `proposal.md`.
11. Terminal owner's content compare (`C:1852-1866`, the twin of D26) — **answered by owner ruling 2026-09-18 (terminal
    outcome adoption)** (§ 5.7); **residual**: adopted-terminal drift is logged, never a disposition.
12. Divergent duplicate for a stored ExecutionID in the current batch overwrites by key instead of the Codex quarantine
    at `SR:799-802` — **residual**, declared; the framework producer cannot diverge (`TC:710-713`, one outcome per ID).
13. Cold W4 rebuild source (one reader, `GetLastMsgForSubject`, serving two purposes) — **answered** (§ 3.6): the newest
    retained request is adopted into the record before any classification; "the request for R" is never fetched.
14. AGENT_LOOPS 24h TTL vs stream retention — **residual**, measured (`acquire.go:20,42-43`); I1 is scoped to records
    that exist; an expired record is a gone loop; a post-expiry redelivery takes the existing not-observable Retry path.

## 8. Other capabilities, skills, adopter seam

- `agentic-model`: no behaviour change; the MsgId stamp (Q5) is on the loop's publish side; its retained-response
  reuse (`processor/agentic-model/component.go:616-627`) is consumed as-is. **No `specs/agentic-model` delta.**
- Skills: `entity-or-bucket` applied — existing bucket AGENT_LOOPS, ground 1 (CAS atomicity with `Iterations` /
  `PendingToolResults`); no new bucket, no ADR. `kv-or-stream`, `orchestration-check`, `new-payload`, `query-pattern`
  not triggered.
- Adopter seam (`inventory.md` § 4): one optional JSON field; sisters decode into their own structs (semsage
  `processor/ui-api/types.go:12`). Two semspec control planes watch this record (`execution-bridge/completion.go:22-54`,
  `review_completion.go:28-51`) and one treats presence as liveness (`recovery-consumer/backstop.go:40-247`); write
  cadence is unchanged (one write per settled input; a CAS failure writes nothing), so semsage's one-SSE-per-change
  (`ui-api/sse.go:15`) sees no new event class. L2's grammar must keep the loop-id half colon-free and the literal
  `:req:` separator (semspec `agent_response_walk.go:119-127` splits on the first colon, so the UUID-suffix worry is
  refuted; `SR:369-375`); the suffix shape is free. L3 (#1329) migration-note item, not L4: semspec
  `configs/e2e-claude.json:311,818` and `e2e-gemini.json:345,874` set the `loops_bucket` key the port-owned bucket
  retires. Nothing asks a caller to predict a value.
