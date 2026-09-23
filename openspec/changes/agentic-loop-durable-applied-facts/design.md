# Design — agentic-loop-durable-applied-facts (#1330, restart-safety L4)

> **Scope: PR #1361 implements L4a** — the field, the carrier order + CAS, the cold rebuild, and the task, response and
> tool lanes. The approval lane, the verdict after waiter loss, the single terminal owner and route-ambiguity metering
> are **L4b = #1362**; their design text stays below, and every section carrying it is marked "(L4b, #1362)" so no
> reader mistakes it for this PR's scope.
>
> Pins: sentences amended on 2026-09-22 are at `b7ce8727` and use the `inventory.md` abbreviations; surviving original
> text is at `68c14c8e` (PR #1159's never-merged branch), and every one of its sites maps through `reconciliation.md`
> § A, which names the `main` equivalent or records that there is none. Owner rulings: #1330, 2026-09-18, "as
> recommended on all eight" (Q1–Q8) and, in its own comment the same day, terminal outcome adoption (§ 1); #1330,
> 2026-09-22, the reconciliation docket and its simplicity re-read (§ 1). Premise correction (recorded): RequestIDs were
> UUID-minted at `68c14c8e` (`state.go:1364-1365`) and `af829616` (`state.go:1131-1132`); deterministic IDs shipped with
> L2 (#1328) and are a fact on `main` (`ST:1339-1347`).

## 1. Rulings applied (2026-09-18, #1330)

| Was | Decision |
|---|---|
| Q1 birth order | Birth keeps Put → publish. A task redelivered while the record still names R1 at iteration 0 with an empty applied set rebuilds R1 and hands it to the publish path, which adopts an R1 the stream already retains and otherwise publishes it ([amended 2026-09-22](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078)). On `main` birth publishes first (`C:1496`) and writes after (`C:1499`, error ignored), so task 2.1 reorders it — P1, § 3.1. |
| Q2 carrier form | The non-terminal carrier becomes `Update(observedRevision)`; in L4 scope. |
| Q3 applied set | `PendingToolResults` keys ARE the applied execution IDs; no separate field. |
| Q4 ID grammar (lands in #1328) | `<loopID>:req:<iteration>:<retry>`; the truncation-retry ordinal is derived from `PublishedRequestID`; recovery ADOPTS an already-published next request by identity (`readExact` = `GetLastMsgForSubject` on `agent.request.<loopID>`; the reader is built here, task 2.3) instead of republishing. Applied to the cold read in § 3.6 (coordinator, 2026-09-18, on the design review's BLOCKING). |
| Q5 duplicates | Request publishes stamp RequestID as `Nats-Msg-Id` via the existing `PublishToStreamWithMsgID` (`natsclient/client.go:963`); the window is a bonus, not the guarantee. `agent.created` duplicates accepted. **Shipped by L2 on `main`:** `C:2328` routes every publish through it and all three mints stamp `MsgID` (`H:1174`, `H:2194`, `H:2958`), so task 2.2 is done before L4a begins. |
| Q6 verdict ACK | L4 owns a minimal ACK path for a verdict with no waiter, using the tool lane's classification. |
| Q7 terminal + unproven | Effect-free ACK with a metric and an audit line. |
| Q8 retention proof | No referent on `main` (`git grep -n continuation_unavailable -- .` returns only this change's own files), so "stays unchanged" has nothing to leave alone: the ruled outcome is one branch that fails the loop `continuation_unavailable`. Docket **OQ1**; it ships with the approval lane in **L4b (#1362)**. |
| Terminal outcome adoption | #1330, owner ruling 2026-09-18 (terminal outcome adoption), same standing as Q1–Q8: a redelivered terminal adopts the loop's durable terminal by identity (loop ID + terminal kind); content differences are logged, never a disposition (§ 5.7). |

### Rulings applied — 2026-09-22 (#1330, [ruling comment](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773604066))

Verbatim: "as recommeded.  and i approve the simplificatino plan on L4 as long sa we are not just gaming a quicker
finish." Applied to the reconciliation docket and its simplicity re-read
([issuecomment-5773199445](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773199445)); the re-read
table governs where the two differ.

- **OQ1** build `continuation_unavailable` with the approval lane → L4b.
- **OQ2** no hydration; the sentence "a replaced process re-arms no approval deadline at startup; the loop stays
  `awaiting_approval` until answered or cancelled; a loop it later rebuilds for a redelivered input carries its
  record's own deadline" becomes a scenario in this change's `agentic-loop` delta and a migration-note line (the
  "at startup" scope and the rebuild clause were ratified by the owner 2026-09-22, issuecomment-5781101792).
- **OQ3** absorb: a CAS loss releases the loop's process state and Retries; birth by `Create`.
- **OQ4** document `tasks_submitted_total` as at-least-once under redelivery; no new issue, no arm change.
- **OQ5** absorb: classify the redelivered tool result at component entry; `TransitionTo` untouched.
- **OQ6** meter inside `activeLoop` with one reason value; retire the two absence asserts → L4b.
- **OQ7 SPLIT.** #1330 is narrowed to **L4a**: the field, carrier order + CAS, cold rebuild, and the task/response/tool
  lanes (tasks 1.x, 2.1, 2.3–2.5, 3.1, 3.2, 3.4, 3.5, 3.8, 4.1, 4.3a–c,e). **L4b is #1362** (beta.163), carrying the
  approval lane, verdict, terminal owner, and 3.9 with their tests and the 6.2 approval stage.
- **OQ8** uniform publish → `Update`, conditional on a test showing the response lane Retries an answer that beats its
  gate; if that test cannot be written, the accepted write → publish branch stands.
- **3.5** reason values on the existing `tool_results_dropped_total`.

**Condition recorded from the ruling — not a quicker finish.** Guards applied: every "document instead of build" row
lands as a spec scenario with a test that asserts the documented behaviour (OQ2's sentence, OQ4's counter note); L4b
keeps the beta.163 milestone, the #1146 acceptance line, and its place on the epic; and this change's tasks name what
moved and where ("Moved to L4b (#1362)" in `tasks.md`).

**Coordinator scoping under OQ7/OQ8 (2026-09-22).** L4a flips the carrier's order to publish → CAS `Update` only on
the lanes it owns — task, model response, tool result. The writer itself becomes `Update` for every caller (one
function, `C:2468`); the order flip does not reach the approval lane's call site (`ARH:205`) or the approval-timeout
sweeper (`AS:100-101`), because the reject-minted W4 that reorder opens is closed only by the approval lane's cold
branch, which is #1362's. OQ8's gate-order decision and its conditional test (task 2.7) move to #1362 with that lane;
until then the gate keeps write → publish and the spec delta scopes the uniform order to L4a's three lanes.

Standing simplicity rule (owner, 2026-09-22): keep complexity as low as possible — an edge case that a doc sentence or
a plain "not supported" can carry does not earn a code branch. Every section below is read against it.

## 2. The field

```go
// agentic/state.go, LoopEntity — beside PendingToolResults (:54)
// PublishedRequestID names the AgentRequest outstanding for this loop: the
// RequestID whose PubAck preceded the KV update that wrote this record (I1).
// Set at birth (R1) and by every request-minting transition; never cleared.
PublishedRequestID string `json:"published_request_id,omitempty"`
```

Applied execution IDs: `PendingToolResults` keys (`AG:57`, written at `ST:1075-1082`, read at `H:2546`, and drained by
the advance itself — `absorbToolResultsIntoContext` (`H:2850`, draining `GetAndClearToolResults`, `ST:1107`, `nil` at
`ST:1121`) runs BEFORE `publishIterationRequest` (`H:2852`, which mints at `H:2927`), so the normal path leaves an
empty set at every advance). Gate identity: `PendingApprovalState.RequestID`
(`AG:197`) already exists; I4 binds it to the new field.
`LoopEntity` is in no generated schema or OpenAPI and is not a registered payload (measured, `inventory.md` § 0), so
`task schema:generate` is untouched.

## 3. The carrier: order, form, identity adoption

Carrier on `main`: `persistLoopState` (`C:2468`, write at `C:2483`), reached from `persistHandlerResult` (`C:1923`;
write `C:1947`, then publish `C:1959`) and from birth (`C:1496` publish, then `C:1499` `Put` with its error ignored).
Two more callers ride the same write: the deferred-continuation marker (`C:1425`) and the approval sweeper (`AS:101`).

1. **Order (L4a's three lanes — docket OQ8, coordinator scoping 2026-09-22):** on the model-response and tool-result
   lanes `publishResults` runs before the write, so `PublishedRequestID` is written only after the request's PubAck.
   The design's premise that three owners already had that order is FALSE on `main`: the carrier writes and then
   publishes for every result, terminal included (`C:1947` → `C:1959`). The flip is scoped by call site, not by the
   carrier: the approval lane reaches `persistHandlerResult` only through `ARH:205` and keeps today's write → publish
   order, and the approval-timeout sweeper keeps its own publish-then-`Put` pair (`AS:100-101`) rather than moving
   onto the carrier (D39). Reordering either one opens the reject-minted W4 whose only handler is the approval lane's
   cold branch, and that branch is #1362's (§ 5.5). OQ8's gate-order decision and its conditional test move to #1362
   with the lane; until then the gate keeps write → publish and the spec delta scopes the uniform order to L4a's
   three lanes. Birth is the write-first lane of the three: birth becomes record → publish (Q1) — the record write
   moves ahead of `publishResults` at `C:1496-1499`, and BOTH errors return rather than one. The write's, because no
   request may go out for a loop with no record. The publish's, because a record naming a request the stream does not
   retain is the state I1 declares impossible, and every later cold read of that loop answers it with Quarantine
   (§ 3.6's I1 arm) instead of recovering it — so a discarded publish error here does not merely lose a request, it
   strands the loop. Two of #1345's five task-intake branches convert by necessity; the other three stay #1345's (P1).
   Until task 3.4's cold fork lands, the Retry a returned birth publish produces meets birth's own `Create`, is
   refused with `ErrKVKeyExists`, and Retries again to the lane's `MaxDeliver`; 3.4 is what turns that refusal into
   the unconditional R1 republish, so it is a merge precondition for this change and not a later slice.
2. **Form (Q2):** `Update(observedRevision)` replaces `Put` at `C:2483`. The `KV:` pins in this section are the
   PATTERN, never the call: `natsclient.KVStore.Update` (`KV:231`, `ErrKVRevisionMismatch` at `KV:238`) and
   `KVStore.Create` (`KV:211`, `ErrKVKeyExists` at `KV:218`) are what the component mirrors, but `c.loopsBucket` is a
   raw `jetstream.KeyValue` and not a `KVStore`, so the component classifies each conflict itself with the shared
   `natsclient.IsKVConflictError` and names it by call site — a refused `Create` is "the key exists", a refused
   `Update` is "the revision moved". That is unambiguous because the two calls cannot return each other's case, and
   it mirrors `natsclient/kv.go:217` and `:237` line for line. One consequence to state rather than discover: an
   `Update` against a record the bucket's 24h TTL has already expired classifies as revision-mismatch, not as
   absence, so it settles on the gone-loop path — release the loop, return Retry, and let the redelivery read a
   bucket that now has no record for this loop at all (`loopPresenceStale`). The ruling's premise that both lanes already hold the revision is FALSE on
   `main`: no production loop-record writer calls `Update` or `Create` — all four are `Put` (`C:2401`,
   `:2432`, `:2456`, `:2483`) and discard the revision `Put` returns, and the one reader `LP:75` discards
   `entry.Revision()`. The revision is therefore process-retained per loop — seeded at birth from `Create`'s return,
   and on a cold read from `entry.Revision()`. A lost race now fails the write → Retry → re-read → the classification
   below answers, **and the loser releases the loop's process state** (`ST:574` `DeleteLoop`, `C:1964`
   `releaseLoopTransientState`) before that Retry, so the redelivery re-enters the cold path against the record that
   won; without the release the loser keeps a stale in-memory loop forever (docket OQ3). Birth is by `Create`, so a
   second consumer's birth is refused with `ErrKVKeyExists` and takes the cold fork — the
   only way the `…:req:N:0` window closes at iteration 0, where no revision exists yet (OQ3).
   `MaxAckPending=1` (`C:1191-1192`) is set per port, for the three ports named at `C:1191`; it serializes deliveries
   within a port and never across them.
3. **Identity adoption (Q4):** before publishing a minted next request R' the carrier calls the retained-request
   reader built in task 2.3 (identity checks only; the newest message on `agent.request.<loopID>`) and orders the
   retained RequestID against R' (Q4): == R' → adopt (skip publish; the retained body is authoritative); absent or ==
   current R → publish with `Nats-Msg-Id = R'` (Q5); anything else (older than R, beyond R', unparseable) → Quarantine
   as a conflict (`errs.WrapFatal`; on `main` each lane returns its own `natsclient.DeliveryDecision` in place, the
   `C:2780` shape — there is no `loopSettlementDecision` helper and none is built). No content compare.
4. **Setting the field** (amended 2026-09-23 by the owner ruling on the Codex round's finding 3, #1330 Q1):
   `LoopManager.SetPublishedRequest(loopID, requestID)` at BIRTH only (`buildTaskRequest`), where Q1 writes the record
   before the first publish. The two iteration mint sites (`emitRetryRequest`, `publishIterationRequest`) do not set
   it; the CARRIER does, in `Component.stampPublishedRequest` under `loopRecordMu`, after `publishResults` PubAcks and
   before `persistResultState`. The entity is shared with every other lane that writes this loop — a deferred
   continuation on the task lane, a tool lane's CAS — so a name set at the mint is one a SIBLING can commit while the
   request is still in flight, writing a record that names a request the stream does not retain. `TrackRequest` stays
   at the mint: route, outstanding and the deferred turn's carrier are attach-order facts. The marshal inside
   `persistLoopState` carries the field as before.
5. **Retry ordinal:** `IncrementTruncationRetry` (`ST:466`) and `ResetTruncationRetry` (`ST:477`), both process-local,
   are replaced by parsing the `<retry>` part of `PublishedRequestID` (`looprequest`, added in THIS change — L2 declined
   to export a parser with no reader, deviation accepted by the coordinator 2026-09-19 on #1328; task 1.0). Their
   callers (`H:2037`, `H:1389`, `H:1409`) and the map cleared at `ST:588` go with them: this is the only deletion
   target the accepted § 6 has on `main`.
6. **Cold read, step 0 — one rule for every lane but task (Q4 applied to the cold path; coordinator, 2026-09-18):** a
   process with no memory of the loop reads the entity + revision (`LP:75`, made revision-returning by task 2.3), then
   the newest retained request on `agent.request.<loopID>` (task 2.3's reader; no RequestID-addressed form exists), and
   orders its RequestID against `PublishedRequestID`. Equal → current, continue. Newer (higher `(iteration, retry)`) →
   **adopt first**: `Update(revision)` the record to that request — `PublishedRequestID = R(N+1)`,
   `Iterations = parsed iteration − 1` (`ST:1345` mints `iteration = entity.Iterations + 1`, so birth writes
   `Iterations = 0` beside `…:req:1:0`; I3), `PendingToolResults = nil` — which is exactly the shape the normal path
   itself leaves at the advance: on `main` `absorbToolResultsIntoContext` (`H:2850`, draining `ST:1107-1121`, nil at
   `ST:1121`) runs BEFORE `publishIterationRequest` (`H:2852`, minting at `H:2927`), so the adopt evicts nothing the
   normal path would have kept. (Review pass 3
   read this as an eviction one step ahead and asked for a rationale; on `main` the design's original sentence is true
   and that MEDIUM reverses — `reconciliation.md` § C, Q3.) Every reader still classifies a redelivered input
   older-by-request before consulting membership (§ 5.3 step 2, § 5.6), Q7's terminal-unproven check reads absence, and
   no entry is synthesized, so nothing is derived from any message body. When `PendingApproval != nil`, the same update
   sets `PendingApproval = nil` with `State = running`: by I4 the gate's RequestID equals `PublishedRequestID`, so it is
   always older than the adopted request, and R(N+1) can only have been minted after that gate's result completed the
   batch (`H:2927` runs on the batch's last result). The adopt writes every field the advance implies, so I1–I4 hold on
   the written record by construction — then classify the redelivered input against the UPDATED record. Older or
   unparseable → Quarantine (conflict). The rebuild source (`restoreLoopFromRequest`, built here — § 6) is always that
   newest retained request, never "the request for R", so it has no `PublishedRequestID` mismatch to refuse. Task lane:
   no step 0 — Q1 rules iteration 0 (no step-0 adopt; a duplicate R1 is answered by the publish path's own identity
   check — Q1 as amended 2026-09-22, https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078 — or by L2's retained
   response reuse) and a record that has advanced, retried its first iteration under a later ordinal, or already
   applied something answers "applied" (§ 5.1.4).

## 4. Invariants (spec home: the ADDED requirement in `specs/agentic-loop/spec.md`)

For every AGENT_LOOPS record of a non-terminal loop L at revision r:
- I1. `PublishedRequestID = R` ⇒ while the record exists, an `AgentRequest{RequestID: R, LoopID: L}` is durably
  retained on `agent.request.L`. I1 is scoped to records that exist: AGENT_LOOPS is `History: 10, TTL: 24h` and refuses
  any other policy at startup (`internal/loopbucket/acquire.go:20,42-43`, measured 2026-09-18); an expired record is a
  gone loop (pre-existing property) and a redelivery after it reads "not observable" → Retry (on `main` the cold arms
  `C:1700` and `C:2292`, via `LP:66-94`).
- I2. Every `PendingToolResults[e]` with `RequestID == R` names an ExecutionID derived from a tool call of the retained
  response for R (`deriveToolExecutionID`, `execution_identity.go:31-40`) — membership only. That its conversation
  effect is the tool message the successor request carries is a consequence the normal handler's tests already prove;
  recovery never checks it, and neither does the property below. An adopted record (§ 3.6) carries an empty set; I2 holds vacuously until the next batch.
- I3. `Iterations` changes only in an update whose `PublishedRequestID` also changes.
- I4. `PendingApproval != nil` ⇒ `PendingApproval.RequestID == PublishedRequestID`. Step 0 (§ 3.6) preserves I4 by clearing the gate in the same update that advances `PublishedRequestID`.

**No hydration (docket OQ2, owner ruling 2026-09-22).** An approval deadline is a process-local convenience, not a
durable fact: **a replaced process re-arms no approval deadline at startup; the loop stays `awaiting_approval` until
answered or cancelled; a loop it later rebuilds for a redelivered input carries its record's own deadline.** That is
the standard (scope ratified 2026-09-22, issuecomment-5781101792), written as a scenario in this change's `agentic-loop` delta and as a migration-note
line — no startup hydration is built, and `approval_sweeper.go` keeps its memory-only snapshot (`AS:69`).

> **Correction owed on the sentence's SCOPE (task 5.3, checkpoint 4, 2026-09-22).** The ruled sentence reads without
> qualification, and measured against this tree it is too broad. The cold rebuild of task 1.2 seats the record
> WHOLESALE (`state.go:387-388`), `State = awaiting_approval` and `PendingApproval` included, so a replacement that
> rebuilds a gated loop for some OTHER reason — a redelivered model response or tool result naming the request the
> record names — does hold that deadline again, at the record's own `RequestedAt + Timeout`. Probe, run and
> discarded: predecessor snapshot 1, replacement at start 0, replacement after redelivering the gated result 1.
> The delta SCENARIO (`specs/agentic-loop/spec.md:58-63`) is scoped to startup and is exactly true, and that is what
> task 5.3's test asserts; only the free-text sentence over-reaches. It is verbatim ruling text, so it is NOT edited
> here — the doc comments and the migration note state the narrow truth, and the narrowing is the owner's to ratify.

PBT decision (`docs/contributing/01-testing.md`): I1–I4 hold over action sequences (deliver / crash at W1–W4 /
redeliver), so one bounded Rapid state-machine property over an in-memory KV plus a fake retained-stream reader (the
evidence-reader seam built in task 2.3) covers the tool and response lanes and checks I2 as membership, never by
rendering; the approval lane has three shapes, uses named examples, and lands with L4b (#1362).

## 5. Per lane: algorithm and crash windows

R = current `PublishedRequestID`; W1 = crash before effect; W2 = after effect, before update; W3 = after update, before
ACK; W4 = next request PubAck'd, crash before the update. "Classify" = terminal → effect-free ACK (Q7); `RequestID == R`
→ current; older ordinal → applied, ACK; unknown/future ordinal → quarantine (each lane returns its own
`natsclient.DeliveryDecision` in place, the `C:2780` shape).
"Cold" = a process with no memory of the loop: § 3.6 step 0 runs first on every lane but task.

### 5.1 Task (`agent.task`)
1. Warm map hit → `HandleTask` dedup (`H:843-855`, `HasActiveLoopForTask`), unchanged.
2. Cold: read entity by `task.LoopID`. Absent → normal birth (Put → publish, R1 with MsgId). Present: verify
   task/role/model (`SR:391-397`); terminal → ACK (`SR:398-400`).
3. Present, `PublishedRequestID == R1`, `Iterations == 0`, `PendingToolResults` empty: rebuild R1 from the
   TaskMessage (`SR:414-421`) and hand it to the publish path, which adopts an R1 the stream already retains and
   otherwise publishes it with
   `Nats-Msg-Id = R1` (Q1 as amended 2026-09-22 — https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078 — Q5),
   rebuild ContextManager (`SR:424`), ACK.
4. Present and advanced, OR present at `Iterations == 0` with a non-empty `PendingToolResults`, OR present naming
   any request other than R1: applied → ACK. The whole first batch runs at iteration 0 and a length-truncated first
   response re-asks it under the next retry ordinal, so neither the ordinal nor the applied set separates an untouched
   birth from a progressed one on its own; the batch is rebuilt by its next tool result and the request the record
   does name is answered by its own response, each on the lane that owns it.
Windows: W1 redo. W2 (record written, R1 unpublished) → step 3 publishes. W3 (R1 already retained) → step 3 adopts it
and publishes nothing. No W4 at birth (Put precedes publish by ruling).
Counter semantics (docket OQ4, owner ruling 2026-09-22): a redelivered task submission counts again on
`tasks_submitted_total` (agentic-dispatch `metrics.go:112`; `recordTaskSubmitted` `:321`, increment `:322`). The counter is **at-least-once under
redelivery**; that is documented in the migration note and pinned by a test, not armed away — no Quarantine arm, no new
issue.

### 5.2 Model response (`agent.response.<requestID>`)
1. Loop from the RequestID grammar (`SR:369-375`); read entity + revision (`SR:484`).
2. Cold → step 0 (§ 3.6). Classify on `response.RequestID` vs R. The superseded-response guard at `H:1253` compares
   `response.RequestID` with the record's `PublishedRequestID`; an empty process map after replacement is no longer a
   let-through (D11; L2's declared residual `ST:955-961`). Current: cold → rebuild ContextManager from the newest
   retained request, which after step 0 is R — no rebuild path exists on `main`, so `restoreLoopFromRequest` is built
   here (task 1.2, beside `attachContinuation` at `ST:298`); `handleModelResponse` with `recoverGovernance`
   (`SR:131-195`).
   Older → ACK with metric. `RequestID` newer than R (its update not yet landed) → Retry "not yet observable"
   (the shape at `SR:522`).
3. Effects: proposals (`publishProposed`, `GD:667`), `tool.execute` publishes, or the terminal owner. Truncation
   retry mints R' (`emitRetryRequest`, `H:2141`, minting at `H:2168`, retry ordinal from R): identity-adopt or publish (§ 3.3), then Update `PublishedRequestID = R'`.
4. Update(revision) after PubAck; a tool_call response leaves `PublishedRequestID = R`.
Windows: W1 redo. W2 → re-proposal re-fires the rule (no MsgID either side, measured); duplicate `tool.execute`
replays via TOOL_CALL_OUTCOMES (`loadCompletedOutcome` → `publishCompletedResult`, `TC:740-743`). W3 → step 2 sees R unchanged, re-runs; same idempotency. W4 (retry
lane only): R' retained, record says R → warm: re-runs, finds R' by identity, adopts, Update, ACK; cold: step 0 adopts
R' first, then the response for R classifies older → ACK.

### 5.3 Tool result (`tool.result`)
1. A redelivered tool result is classified against `PublishedRequestID` at component entry (`C:2195`, before
   `HandleToolResult`) and in the cold live arm (`C:2292`); `HandleToolResult` never sees an older or unknown result
   (D22). Entry is the placement the owner ruled for the duplicate-`StopLoop` replay as well: classify the redelivered
   tool result at component entry and leave `TransitionTo` untouched — its same-state `nil` at `AG:181` is a legitimate
   no-op for other callers (docket OQ5, 2026-09-22). The cold terminal drop at `C:2293-2302` is already Q7's
   effect-free ACK; the warm half is inserted before `HandleToolResult` at `C:2195`, because `StopLoop` (`H:2580`) and
   `StoreToolResult` (`H:2546`) both precede the lane's only terminal guard (`H:2652`) (D26).
   Identity is the routing lookup at `C:2173` (`findLoopIDForToolCall`); a miss goes to `settleToolResultWithoutLoop`
   (`C:2287-2311`), which derives the loop from the payload at `C:2288-2290`. The warm lane on `main` reads no loop
   record at all — the predecessor's four warm KV checks (`C@68c:2162-2190`, D45–D48) were never written, so there is
   nothing to keep — and `C:2178-2192` is only the error flag, the Debug line and the received/truncated metrics.
   Task 3.1 therefore BUILDS the warm route at `C:2195`: read entity + revision, then terminal → Q7 effect-free ACK,
   older → ACK, `RequestID` newer than R → Retry (not yet observable), unknown → Quarantine. Cold → step 0 (§ 3.6).
   Classify on `result.RequestID` vs R.
2. Older → applied (the iteration cannot advance before `AllToolsComplete`, `ST:810`) → ACK.
3. Current: cold → rebuild from the newest retained request (after step 0 it is R) and retained response R. No
   rebuild path exists on `main`: `ST:473-521` there is `ResetTruncationRetry` (`ST:473-481`, which task 2.4 deletes)
   followed by `ResolveApprovalIfPending` (`ST:483`) — so `restoreLoopFromRequest` and `restoreToolBatch` are built by
   task 1.2.
   `HandleToolResult` stores the result (`H:2546`; idempotent key overwrite), dispatches the next queued
   sibling, or on the last result mints R(N+1) (`H:2927`).
4. Publish `tool.execute`, or identity-adopt / publish R(N+1) (§ 3.3); then Update(revision) with `Iterations`,
   `PendingToolResults`, `PublishedRequestID = R(N+1)`; ACK.
Windows: W1 redo. W2 mid-batch → re-store, re-dispatch; duplicate `tool.execute` replays (`TC:740-743`). **W4**:
R(N+1) is retained, the record still says R. Warm redelivery: current → re-store (no-op) → `AllToolsComplete` →
`handleToolsComplete` mints R(N+1) → `readRetainedAgentRequest` returns R(N+1) → adopted, **not republished** → Update
→ ACK. Cold redelivery (process replaced): step 0 reads R(N+1) > R → adopts first (Update: `PublishedRequestID =
R(N+1)`, `Iterations = N+1`, applied set by identity) → the result for R now classifies older → ACK, nothing published;
no rebuild of R is attempted, so `restoreLoopFromRequest` has nothing to refuse (the design review's BLOCKING). A response
for R(N+1) arriving before that update → Retry until observable. W3 → step 2: `R(N+1) ≠ result.RequestID`, older → ACK.

### 5.4 Approval-required tool result (gate) — L4a, except the warm re-echo (L4b, #1362)
Steps 1–2 as § 5.3. Current and `State == awaiting_approval && PendingApproval.ExecutionID == result.ExecutionID` →
re-echo `ApprovalPendingEvent` (`SR:743-758` minus its retained-request validation) → ACK. Current, running,
`PendingToolResults[e]` holds this gate status and `PendingApproval == nil` → gate consumed → ACK (`SR:793-795`).
Else `gateForApproval` (`H:2704-2723`) → the carrier's gate branch (on `main` there is no `persistApprovalGate`: the
gate is a carrier result keyed on `result.State == awaiting_approval`, `H:2573` → the tool lane's carrier call at
`C:2220` → `C:1947` → `C:1959`).
Windows: W1 redo; W2/W3 → re-echo. W4 would exist only if the gate took the uniform publish → `Update` order; in L4a
it does not (OQ8's gate-order decision moved to #1362), so the gate keeps write → publish and there is no W4 here.
The warm re-echo itself (`H:2674`, D28) rides the approval lane and lands with L4b (#1362). Cold → step 0 first, as
§ 5.3.

### 5.5 Approval response (`agent.approval_response.<loopID>`) (L4b, #1362)
1. Step 0 (§ 3.6): read entity + revision, adopt a newer retained request (which clears the gate); then require
   `awaiting_approval` with matching `ExecutionID`/`CallID`; else ACK inapplicable / quarantine. On `main` this lane has
   no cold branch at all: an in-memory loop that is not awaiting stays an Ack (W3); `ErrLoopNotFound` no longer
   stale-drops — it enters the new cold branch at `ARH:58`, and only a record that is absent or terminal is
   acknowledged (D35, today `ARH:78` → `ARH:194-199`).
2. Require I4 and the gated result in `PendingToolResults`. Cold → rebuild from retained R and its response. Retained R
   or its response absent → the loop fails `continuation_unavailable` (the 2026-09-13 ruling on #1146; docket OQ1 — one
   branch, built here, nothing on `main` to leave unchanged).
3. Approve/modify: `dispatchApprovedCall` (`ARH:117-130`) builds the call, and the carrier publishes it and writes
   the record (`ARH:205` → `C:1947` → `C:1959`); the gate itself was already cleared atomically by
   `ResolveApprovalIfPending` (`ARH:54`). Reject: synthetic result through § 5.3.
Windows: W1 (crash before the `tool.execute` publish) → redo, record unchanged. W2 (published, gate not cleared) →
redelivery re-publishes `tool.execute`; the duplicate execution replays by ExecutionID (`TC:740-743`); Update clears; ACK.
W3 (gate cleared, no ACK) → step 1: no longer `awaiting_approval` → ACK inapplicable — on `main` the stale-drop at
`ARH:58-78` returns through `ARH:194-199`, logging at `ARH:73` and recording no metric. W4 becomes real on the reject/timeout branch once #1362 flips this lane's order (the sweeper's auto-rejects land here
too, `AS:92`): `handleRejectedApproval` feeds a synthetic result to `HandleToolResult` (`ARH:139-158`), which on the
batch's last result mints R(N+1) (`H:2927`); with the flipped order the carrier's `publishResults` (`C:1959`) would
precede the single `Update` (`C:1947`) that clears the gate and records the advance, so a crash between them leaves
R(N+1) retained with the record `awaiting_approval` at R. In L4a this lane keeps write → publish (`ARH:205` →
`C:1947` → `C:1959`) and the window stays closed. Redelivery → step 0 adopts R(N+1)
and clears the gate in the same CAS write (§ 3.6) → step 1 finds the record no longer `awaiting_approval` → ACK
inapplicable, nothing republished: the W3 path. The approve/modify branch mints no request (it publishes `tool.execute`)
and has no W4.

### 5.6 Governance verdict (Q6 — L4b, #1362), timer, startup, cancel
- Verdict: on a missing waiter the verdict lane reads the record: `verdict.RequestID` older than `PublishedRequestID`,
  or `ExecutionID ∈ PendingToolResults`, or terminal → Ack with a reason on the missing-waiter counter (`M:393`);
  current and unseen → Retry (`C:2780` unchanged) (D16). The skeleton it attaches to exists on `main`:
  `ErrNoGovernanceWaiter` (`GD:337`, returned at `GD:608-629`) → `C:2734` → `settleVerdictWithoutWaiter` (`C:2759`,
  stale → Ack `C:2773`, live → Retry `C:2780`). No retained-verdict reader is built (D14/D15): the re-proposal re-fires
  the rule, and the duplicate proposed/verdict pair is the declared residual.
- Timer/startup: the sweeper keeps its memory-only snapshot (`AS:69`) and **no startup hydration is built** — a replaced
  process re-arms no approval deadline at startup; the loop stays `awaiting_approval` until answered or cancelled
  (docket OQ2, owner ruling 2026-09-22, scope ratified 2026-09-22 issuecomment-5781101792; § 4). Its write pair moves onto the carrier **with the approval lane in #1362**: the timeout
  sweeper's own publish-then-`Put` pair (`AS:100-101`) is replaced by the carrier (`persistHandlerResult`, `C:1923`)
  so the auto-reject takes the same publish → `Update` order and the same CAS as an operator rejection (D39). In L4a
  the pair is untouched; only its `persistLoopState` call (`AS:101`) rides the writer's change to `Update`.
  **Residual for #1362, recorded in L4a:** both halves of that pair fail log-only. A timer has no delivery to
  classify, so neither the publish nor the record write can settle anything — L4a names each in its own `Warn`
  line ("did not publish its results", "did not commit the loop record") and adds no counter. No existing loop-side counter's subject is "a write this process meant to
  make did not commit": `tool_results_dropped_total`, `model_responses_dropped_total` and `signals_dropped_total`
  each count an INPUT acknowledged without effect, and reporting a sweeper write loss on one of them would put two
  different events under one reason vocabulary. Whether the auto-reject owes a counted signal is decided where the
  lane moves onto the carrier and acquires a delivery to classify, which is #1362's.
- Cancel: unchanged (`settleUncancellableLoop`, `C:2593-2612`, classifying by `State` only; `handleCancelSignal`,
  `C:2614`); its terminal writes (`C:2631`, `C:2683`) are among the writers § 3.2 protects.

### 5.7 Terminal (all lanes; one owner replacing the three `Put` paths) (L4b, #1362)
**#1330, owner ruling 2026-09-18 (terminal outcome adoption)**, same standing as Q1–Q8 (applies Q7 + the Q4 identity
principle; inventory D44, D49). On `main` there is no single owner and no marker identity: three paths each `Put` — the
carrier (`C:1974` → marker `:2401`/`:2432` → stamps → publish `C:1959`), the loop-failure path
(`handleLoopFailure`, `C:1752` → `publishFailureEvents`, `:1809` → `:1831`) and cancel (`C:2631` → `:2668` →
`:2683`). The three terminal write paths become one owner: `COMPLETE_` marker
by `Create` (`KV:211`; `ErrKVKeyExists` `KV:218` → read it back and adopt), graph stamps, publish, then the entity by
`Update(revision)` (`KV:231`); the marker's Create-once is the identity the beta.57 ordering (`C:1805-1812`, KV marker before publish) keyed on
(D41/P6). Q7(a) sits at `C:2195` (warm) and `C:2293` (cold), not inside this owner, and the TaskID-versus-marker
Quarantine is dropped because no warm read exists to feed it (D44). It also lands L3's deferred item: `PendingApproval`
is cleared on the terminal transition. On a redelivered terminal:
- (a) record terminal at the observed revision → effect-free ACK with metric and audit line (Q7). A revision conflict
  alone → Retry (CAS re-read, short-lived).
- (b) record not terminal and the loop's durable terminal exists — the owner's marker `Create` is refused with
  `ErrKVKeyExists` and the saved payload is read back; this KV marker, not a stream read, is the "retained published
  terminal" the ruling names → adopt it by identity (loop ID + terminal kind): the saved payload replaces the
  candidate, publish proceeds with it (a terminal republish is an accepted duplicate,
  `openspec/specs/agentic-loop/spec.md:430-446`), the entity is written to match under `Update(revision)`, ACK. Content
  differences between this delivery's candidate and the saved payload are logged at the audit line, never a
  disposition. There are no content compares on `main` to delete and none is built.
- (c) no durable terminal → normal path: Create, publish, `Update`, ACK.
Windows: crash after the Create or the publish, before `Update` → (b) on redelivery; after `Update`, before ACK → (a).

## 6. What exists on `main` at `b7ce8727` / what this change builds

The accepted design was written as a pruning of PR #1159's recovery layer (`settlement_recovery.go`, 1,051 lines). That
layer never landed on `main`, so this section inverts: one deletion target, nine of the predecessor's "survives" items
are builds, and the rest of its vocabulary has no home here at all (rows: `reconciliation.md` § B and § H).

**Deleted — the one target:** `IncrementTruncationRetry` (`ST:466`) and `ResetTruncationRetry` (`ST:477`), their callers
`H:2037`, `H:1389`, `H:1409`, and the map cleared at `ST:588` (task 2.4).

**Built here (L4a):**

| Item | `main` today | What lands |
|---|---|---|
| evidence reader + addresses | absent (pattern: the reader `PS:20-48`, the address helper `PS:50`, `TR:51`) | one interface, two reads: newest retained request, retained response (task 2.3) |
| revision-returning entity read | `LP:75` reads the entity and discards `entry.Revision()` | a read that returns the revision; `classifyMissingLoop` keeps its signature |
| one reader for the ID grammar | two prefix-only readers, `ST:1360-1366` and `LP:50-59` | `looprequest.Parse/Next/Compare` (task 1.0) |
| cold task fork | none — `C:1396` `HandleTask` creates the loop in memory | the fork before `C:1396` (§ 5.1, task 3.4) |
| `restoreLoopFromRequest` | absent | rebuild from the adopted record + newest retained request (task 1.2) |
| `restoreToolBatch` | absent | membership against the retained response; no `Iterations--`, no `requirePreceding` |
| step 0 (adopt, then classify) | absent | the cold arms `C:1700` and `C:2292` (task 2.5) |
| `PublishedRequestID` + CAS carrier | four `Put` writers discarding the revision (`C:2401`, `:2432`, `:2456`, `:2483`) | the field, `Update(revision)`, publish → write, birth by `Create` (tasks 1.1, 2.1, 2.6) |
| Q7 warm half | cold half exists (`C:2293-2302`); the warm lane has none | the check before `HandleToolResult` at `C:2195` (task 3.5) |
| the six predecessor test files | absent | written, not rewritten (tasks 3.x, 4.x) |

**Built with L4b (#1362):** the approval lane's cold branch at `ARH:58` with `republishPendingApproval` and the I4
evidence check, `continuation_unavailable` (OQ1), the verdict classification at `C:2759-2780` (D16), the single terminal
owner (D41/P6), and the route-ambiguity metering inside `activeLoop` (OQ6).

**Survives untouched:** `execution_identity.go` (`EI:24-36`), `approval_sweeper.go` (memory-only snapshot at `AS:69`; no
hydration — OQ2), `agentic-dispatch/task_recovery.go`, `agentic-model/provider_settlement.go`, the shared handlers, and
the e2e harness (`test/e2e/harness/processbarrier`, `test/e2e/scenarios/agentic/stage_a_process_replacement.go`) with
its assertions re-pointed at KV facts. `delivery_owner.go` leaves the list entirely: `internal/deliverylane`
(`DL:27-225`) owns the latch since #1341, and L4 adds no second spelling.

**Codex-only, with no home here (nothing to delete):** `settlement_recovery.go` entire — `recoverTaskDelivery`,
`ensureResponseLoop`, `recoverToolResult`, `recoverApprovalResponse`, `recoverGovernance`,
`readRetainedGovernanceVerdict`, `loopSettlementDecision`, `validatePendingApproval*`, the three layout proofs
(`toolResultProvenInLaterRequest`, `approvalRequiredResultSuperseded`, `proveTerminalToolResultApplied`) and the
compare-only truncation; `state.go`'s `requirePreceding`, `validatedToolBatchResults` and `Iterations--`;
`component.go`'s `persistTerminalOutcome`, `selectTerminalOutcome`, `persistApprovalGate`, the warm KV checks
`C@68c:2162-2190` and the birth `Create` at `C@68c:1532`; `approval_sweeper.go`'s `restoreApprovalDeadlines`; `metrics.go`'s
`approvalDecisionsInapplicable`. They are never written rather than deleted, and § H of `reconciliation.md` keeps the
full list so nothing is silently lost.

## 7. Strongest case against, each marked

1. KV record growth — **answered**: one string; content unchanged; residual bound in `proposal.md` § Declared cost.
2. Batch-size bound on the applied set — **answered**: `PendingToolResults` keys, bounded by the model's `tool_calls`.
3. Approval-required results — **answered**: the gate is already CAS-committed; L4 adds I4 and the re-echo. The
   reject-minted W4 is not opened in L4a — this lane keeps write → publish (`ARH:205` → `C:1947` → `C:1959`) — and
   when #1362 flips it, step 0 adopts and clears the gate with the advance (§ 5.5).
4. Terminal loop + unproven result — **answered by ruling Q7**: effect-free ACK with metric and audit line (as the
   cancel lane already does at `C:2593-2612`); the cold half exists (`C:2293-2302`) and the warm half is built at
   `C:2195`. The predecessor's retry-to-`MaxDeliver` was never on `main`, so there is nothing to remove.
5. Governance verdict retention — **answered by measurement** (re-proposal re-fires the rule) and **by ruling Q6** for
   the verdict lane's ACK; **residual**: duplicate proposed/verdict pairs (declared cost).
6. Stale vs conflict — **answered by ruling Q4**: the ordinal grammar orders RequestIDs; unknown/future quarantines.
7. Two lanes, one record — **answered by ruling Q2**: `Update(observedRevision)`.
8. Retention-absence proof — **answered by ruling Q8, re-read 2026-09-22**: no referent exists on `main`, so the ruled
   outcome is built as one `continuation_unavailable` branch (docket OQ1) and ships with the approval lane in L4b
   (#1362).
9. Deterministic retry IDs — **answered by ruling Q4**: retry ordinal derived from `PublishedRequestID`.
10. Adopted-request drift — **residual**, declared in `proposal.md`.
11. Terminal owner's content compare (the twin of D26) — **answered by owner ruling 2026-09-18 (terminal outcome
    adoption)** (§ 5.7). No compare exists on `main` to delete; the owner is built with marker identity in L4b (#1362).
    **Residual**: adopted-terminal drift is logged, never a disposition.
12. Divergent duplicate for a stored ExecutionID in the current batch overwrites by key; the predecessor's quarantine at
    `SR:799-802` is not built — **residual**, declared; the framework producer cannot diverge (`TC:740-743`, one outcome
    per ID).
13. Cold W4 rebuild source (one reader, `GetLastMsgForSubject`, serving two purposes) — **answered** (§ 3.6): the newest
    retained request is adopted into the record before any classification; "the request for R" is never fetched.
14. AGENT_LOOPS 24h TTL vs stream retention — **residual**, measured (`acquire.go:20,42-43`); I1 is scoped to records
    that exist; an expired record is a gone loop; a post-expiry redelivery takes the existing not-observable Retry path.

## 8. Other capabilities, skills, adopter seam

- `agentic-model`: no behaviour change; the MsgId stamp (Q5) is on the loop's publish side; its retained-response
  reuse (`processor/agentic-model/component.go:633-645`) is consumed as-is. **No `specs/agentic-model` delta.**
- `agentic-dispatch`: no behaviour change and no code change — but OQ4 states what `tasks_submitted_total`
  (`processor/agentic-dispatch/metrics.go:112`) already means under redelivery, and that belongs to the capability
  that owns the counter. **One ADDED requirement in `specs/agentic-dispatch/spec.md`**, pinned by task 5.4's test.
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

## 9. Conformance — every ruling, where it landed

One row per binding ruling on #1330: the eight design questions Q1–Q8, terminal-outcome adoption, the eight docket
questions OQ1–OQ8, the 2026-09-21 route-ambiguity metering ruling, the two coordinator-applied rulings that reached
this change after the docket — the standing no-deprecation ruling and the scoping of the carrier reorder — and the
ten findings of the owner Codex round, whose own eight docket questions the owner ruled on 2026-09-23
([issuecomment-5790258247](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5790258247)), and the
three findings of the owner's SECOND Codex round
([issuecomment-5790425046](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5790425046)) that were
not gated on an owner ruling.
Every row points at a line in this tree or names the change that carries it. One row is a **DEVIATION**; one is a
**SIMPLIFICATION**. Line numbers are at `c0a31dff`, the last content commit of the owner's SECOND Codex round, regenerated with `sed -n "${n}p"` rather than transcribed (checkpoint-4 review, MEDIUM-3). Every pin the round's own commits moved was re-derived from the symbol it names, not by arithmetic. Unlike `tasks.md`'s task descriptions, nothing here is baseline evidence: every row names where a ruling landed in THIS tree, so every pin moves with the tree.

| Ruling | Landed as | Where |
|---|---|---|
| **Q1** birth order — Put → publish; a task redelivered at iteration 0 republishes R1 | Birth writes the record by `Create` and publishes after; the cold task fork rebuilds R1 through the ordinary birth path. **DEVIATION — see the row below.** | `component.go:3010` (`createLoopState`, `Create` at `:3026`), `component.go:1436` → `loop_classification.go:119` (`classifyRedeliveredTask`), republish arm `component.go:1579` |
| **Q1 DEVIATION** — "republish it unconditionally with the MsgId, no retained read" | The rebuilt R1 goes out through `publishResults`, which consults `adoptRetainedRequest` first, so an R1 the stream already retains is **adopted, not republished**. The birth is never blocked behind a read (Q1's intent); the one divergent state — R1 retained, dedup window expired — yields no duplicate instead of a second copy under the same name, which is what task 2.3 exists to prevent. **Owner-ratified 2026-09-22**, verbatim "1330 agree with recommendation": [issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078). Q1 is amended to the adopt-not-republish reading. | `component.go:2816` (the `msg.MsgID != ""` arm of `publishResults`) → `loop_evidence.go:340` (`adoptRetainedRequest`); evidence `task_redelivery_integration_test.go` |
| **Q2** carrier form — non-terminal carrier becomes `Update(observedRevision)` | `persistLoopState` compare-and-swaps against the revision this process observed, for every caller; the ORDER flip is scoped to the task, model-response and tool-result lanes (coordinator scoping under OQ7/OQ8) | `component.go:3125` (`Update` at `:3171`), order at `component.go:2315` (`publishThenPersistResultState`) |
| **Q3** applied set — `PendingToolResults` keys ARE the applied execution IDs; no separate field | No field added; the map's keys are the applied identities and the rebuild reads them as such | `agentic/state.go:57`, read at `state.go:512` (`restoreToolBatch`) |
| **Q4** ID grammar `<loopID>:req:<iteration>:<retry>`; recovery ADOPTS by identity | Shipped by L2 (#1328) as `internal/looprequest`; L4a's step 0 adopts the newest retained request before any classification | `processor/agentic-loop/internal/looprequest`, `loop_evidence.go:425` (`adoptNewerRetainedRequest`) |
| **Q5** duplicates — RequestID as `Nats-Msg-Id`, the window is a bonus | Every publish carrying a MsgID goes through `PublishToStreamWithMsgID`; identity adoption, not the window, is the guarantee | `component.go:2830` |
| **Q6** verdict ACK for a verdict with no waiter | **L4b (#1362).** Out of L4a scope by OQ7 | `tasks.md` § "Moved to L4b (#1362)", bullet 3.6 |
| **Q7** terminal + unproven — effect-free ACK with a metric and an audit line | Component-entry classification acks without effect and counts `terminal_unproven` / `older_request` on the existing `tool_results_dropped_total`; each drop carries a `WarnContext` naming both request names | `loop_classification.go:201` (`classifyRedeliveredToolResult`), counters `:214` and `:230`, warm call site `component.go:2590` |
| **Q7 recorded deviation** — the terminal arm does not check `PendingToolResults` membership first | Membership would only tell "already applied" from "never applied" for a loop that can apply neither, and Q7 rules both to the same effect-free Ack; the branch's two sides would do the same thing. Widened in the delta's terminal scenario so the GIVEN covers both | `specs/agentic-loop/spec.md:107-115`, `tasks.md` 3.5 |
| **Q8** retention proof → `continuation_unavailable` | **L4b (#1362)** as OQ1; it ships with the approval lane | `tasks.md` § "Moved to L4b (#1362)", bullet OQ1 |
| **Terminal outcome adoption** (owner ruling 2026-09-18) — a redelivered terminal adopts the loop's durable terminal by identity; content differences are logged, never a disposition | **L4b (#1362).** The single terminal owner that replaces the three `Put` paths is task 3.7, moved out under OQ7 | `design.md` § 5.7, `tasks.md` § "Moved to L4b (#1362)", bullet 3.7 |
| **OQ1** build `continuation_unavailable` with the approval lane | **L4b (#1362)** | `tasks.md` § "Moved to L4b (#1362)", bullet OQ1 |
| **OQ2** no approval-deadline hydration; the sentence becomes a spec scenario and a migration line | **CONFORMS.** No hydration is built: `SnapshotExpiredApprovals` reads `m.loops`, and no startup path reads the bucket. The scenario, the migration section and the measured-delta test all landed. The requirement's free-text sentence originally said "a replaced process SHALL re-arm no approval deadline" without qualification, which was measurably too broad — a replacement that rebuilds a loop for another reason (task 1.2) seats its `PendingApproval` with it. The owner ratified the narrowing 2026-09-22 (issuecomment-5781101792) and the sentence now reads "at startup" with the rebuild clause, matching the scenario, which was always scoped to startup | scenario `specs/agentic-loop/spec.md:87-92`; test `approval_deadline_hydration_integration_test.go:41` (`TestAReplacementReArmsNoApprovalDeadline`); migration `docs/operations/migration-beta162-to-beta163.md:1801`; sweeper `state.go:680`, doc `approval_sweeper.go:40-46`; the narrowed sentence `specs/agentic-loop/spec.md:46-49` |
| **OQ3** a CAS loss releases the loop's process state and Retries; birth by `Create` | Birth is `Create`; a lost CAS releases this loop's in-process state and returns the sentinel, which the lane reads as Retry | `component.go:3026` (`Create`), `component.go:3174-3177` (release + `ErrKVRevisionMismatch`), lane reads at `component.go:2280`, `:2334` |
| **OQ4** document `tasks_submitted_total` as at-least-once under redelivery; no arm change | **CONFORMS.** No arm changed: `recordTaskSubmitted` is still called unconditionally after the task publication, on both submission lanes. The delta landed `0e327a51`; the migration section and the test carrying the citation landed with task 5.4 | delta `specs/agentic-dispatch/spec.md:9-22`; counter `processor/agentic-dispatch/metrics.go:109-114`, recorder `:321`, unconditional call sites `component.go:1168` and `http.go:428`; test `task_submission_counter_integration_test.go:41`; migration `docs/operations/migration-beta162-to-beta163.md:1836` |
| **OQ5** classify the redelivered tool result at component entry; `TransitionTo` untouched | The terminal and ordering checks sit before `HandleToolResult`; `LoopEntity.TransitionTo`'s same-state `nil` is left alone | `component.go:2590`, `agentic/state.go` `TransitionTo` unchanged |
| **OQ6** meter `loop_route_ambiguous` inside `activeLoop` with one reason value; retire the two absence asserts | **L4b (#1362)** as task 3.9 | `tasks.md` § "Moved to L4b (#1362)", bullet 3.9 |
| **OQ7 SPLIT** — #1330 narrowed to L4a; L4b is #1362 | This change carries tasks 1.x, 2.1, 2.3–2.5, 3.1, 3.2, 3.4, 3.5, 3.8, 3.10, 4.1–4.3; everything else is listed by name under "Moved to L4b" | `tasks.md` § "Moved to L4b (#1362)" |
| **OQ8** uniform publish → `Update`, conditional on a test showing the response lane Retries an answer that beats its gate | The conditional test and the gate-order decision move to #1362 with the approval lane; until then the gate keeps write → publish and the delta scopes the uniform order to L4a's three lanes | `tasks.md` § "Moved to L4b (#1362)", bullet 2.7; scoping `design.md` § 1 |
| **2026-09-21 metering ruling** (route ambiguity) | **L4b (#1362)** as task 3.9; never opened here | `reconciliation.md` § E, `tasks.md` § "Moved to L4b (#1362)", bullet 3.9 |
| **No deprecation** ([issuecomment-5774793150](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5774793150), coordinator-applied under the standing #759 ruling) — the two truncation-retry helpers are DELETED in this PR, not marked `Deprecated:` | **CONFORMS.** `LoopManager.IncrementTruncationRetry` and `ResetTruncationRetry` are gone from `state.go` together with the `truncationRetryAttempts` map; the budget is read back off the retry ordinal of `PublishedRequestID`. The change is a `fix(agentic-loop)!:` with a migration-note section, and `task api:compat:report` lists both removals under `processor/agentic-loop`, joining the three entries earlier layers left | `git grep -n 'IncrementTruncationRetry\|ResetTruncationRetry' -- '*.go'` is empty; migration note `docs/operations/migration-beta162-to-beta163.md:1729`; `handlers.go` `handleLengthTruncation` reads the ordinal |
| **Carrier-reorder scoping** ([issuecomment-5773929421](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773929421), coordinator-applied under OQ7/OQ8) — L4a reorders publish → CAS `Update` only on the task, model-response and tool-result lanes; the writer still becomes `Update` for every caller | **CONFORMS.** One writer for everyone; the order flip is lane-scoped. The approval lane's call site, the approval-timeout sweeper, and any result that CREATES an approval gate keep write → publish, because the reject-minted W4 that reorder opens is closed only by the approval lane's cold branch (#1362, task 3.3). The gate arrives on the TOOL-RESULT lane, which asks for publish-first, so the carve-out is in the carrier rather than at a call site — round 2, finding 2 | writer `component.go:3125`; flip `component.go:2315` (`publishThenPersistResultState`), reached from `component.go:1860` and `:2640` only, and skipped for a gate by `component.go:2241`; approval lane `approval_response_handler.go` and `approval_sweeper.go` unchanged; scoping `design.md` § 1, deferral `tasks.md` § "Moved to L4b (#1362)", bullets 2.1 and 2.7 |
| **Rebuild inherits the deadline** (checkpoint-4 review, 2026-09-22 — a residual recorded, not a ruling) | **DOCUMENTED, behaviour unchanged on the response and tool lanes; the task lane was FIXED in round 2 — see finding 4 below.** `TimeoutAt` is written at birth and lives on the record, so `restoreLoopFromRequest` seats it with everything else and the warm apply's `IsTimedOut` fails the loop on the FIRST delivery whenever the replacement gap outran `timeout`. The loop is rebuilt and then settles on `agent.failed` with `loop timeout exceeded`, and the delivery is ACKNOWLEDGED — a shape invisible to settlement and consumer-health checks. Refreshing the deadline on rebuild, or excluding downtime from it, would let a loop outlive the budget its caller set: that is an owner ruling, not a recovery decision, so it is recorded as a residual on #1330 and the adopter-facing layers state it instead | seat `state.go` `restoreLoopFromRequest`, check `handlers.go` `IsTimedOut` (response and tool lanes), reader `state.go` `IsTimedOut`; scenario `specs/agentic-loop/spec.md` "A rebuilt loop keeps its record's deadline"; test `tool_result_redelivery_integration_test.go` (`TestToolResultRedeliveredToAReplacementProcess`, arm "the replacement gap outran the loop's deadline"); `doc.go` § Recovery across a process replacement; migration note § "A rebuilt loop's conversation is one region" |
| **Standing simplicity rule** (owner, 2026-09-22) — an edge case a doc sentence or a plain "not supported" can carry does not earn a code branch | **SIMPLIFICATION, no ruling deviates.** The cold rebuild replays the retained conversation into ONE region — system prompt to `RegionSystemPrompt`, everything else to `RegionRecentHistory` in retained order, then `RepairToolPairs()` — rather than reconstructing the predecessor's compaction attribution. No conversation CONTENT is lost, but it is not true that nothing moves: the request's per-iteration framing is stripped on replay, more than one system message is re-seated together at the front rather than where the request interleaved them, and per-region attribution resets — which the migration note carries as its own section. **Owner: "the one-region replay stands", no objection recorded**, [issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078) | `state.go:433-449`, migration note § "A rebuilt loop's conversation is one region" |
| **Owner Codex round, finding 1** (2026-09-22) — a rebuilt loop dispatches without its task enforcement metadata | **FIXED.** `restoreLoopFromRequest` now restores `cachedMetadata` from the RECORD (the request never carried it), defensively copied, beside the four request-side caches. Dispatch stamps `DispatchEnforcedMetadataKeys` from that cache onto every outgoing call, and both consumers read an absent key as permissive, so a recovered read-only task was silently writable | `state.go:475`; test `rebuild_enforcement_metadata_integration_test.go` (`TestARebuiltLoopDispatchesWithItsTaskEnforcementMetadata`, both recovery entries), commit `754dca63` |
| **Owner Codex round, finding 2** (2026-09-22) — a refused birth write leaves a warm loop and the retry ACKs without R1 | **FIXED.** The non-conflict `Create` failure releases the loop it built, as the key-exists arm above and the publish-failure arm below already did. Left warm, the redelivery met `HandleTask`'s task-id dedup instead of the record and acknowledged a task that never issued a request | `component.go:1635`; test `task_redelivery_integration_test.go` (`TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry`), commit `680ccbff` |
| **Owner Codex round, finding 5** (2026-09-22) — a replay of an applied tool result quarantines the lane | **FIXED.** `settleToolResultWithoutLoop`'s `requestOrderCurrent` arm checks `PendingToolResults` membership BEFORE any rebuild and settles the replay with a third reason value `already_applied`. Ordering cannot decide it (the batch is the current request's) and the rebuild leaves applied executions unrouted, so the lane Terminated a routine lost-ACK redelivery. The terminal arm stays membership-free per the Q7 deviation row above | `component.go:2771` (check) and `:2776` (counter), `metrics.go:173` (Help) and `:538` (recorder doc); delta scenario "A tool result the record already applied is replayed"; test `tool_result_redelivery_integration_test.go` (`TestAReplayedAppliedToolResultDoesNotQuarantineItsLane`), commit `58317c26` |
| **Owner Codex round, finding 6** (2026-09-22) — `iterations = 0` alone is not an untouched birth | **FIXED.** `classifyRedeliveredTask` requires the delta's other half — an empty `PendingToolResults` — before taking the republish arm. The whole first batch runs at iteration 0, so the ordinal alone sent a progressed batch down the republish path, seating a fresh loop over a record that carries one | `loop_classification.go:176`; delta scenario "A task redelivered over a first batch that already applied something is acknowledged without effect"; test `task_redelivery_integration_test.go` (`TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished`), commit `732d0d0e` |
| **Owner Codex round, finding 7** (2026-09-22) — prefix stripping also removes a configured system prompt that looks like framing | **DOCUMENTED, behaviour unchanged**, under the standing simplicity rule: the reach is one system message, in the framing slot, on the cold path only. `[Iteration Budget]` and `[Working list` are named as RESERVED prefixes an adopter must not start a system prompt with | `doc.go:247` § Recovery across a process replacement; migration note `docs/operations/migration-beta162-to-beta163.md:1791`; existing coverage `loop_rebuild_test.go` ("a message that only looks like the prefix is still the conversation"), commit `1f1cd870` |
| **Owner Codex round, finding 8** (2026-09-22) — active deltas contradicted the Q1 amendment and their own test | **FIXED (documentation).** The loop scenario is "adopts or publishes"; § 1, § 5.1 step 3 and the windows line no longer say "unconditionally / no evidence read" and cite the amendment; the dispatch delta says no second LOGICAL task is minted, matching `task_submission_counter_integration_test.go:86`. No runtime behaviour changed | `specs/agentic-loop/spec.md:117`, `specs/agentic-dispatch/spec.md:12` and `:22`, `design.md` § 1 Q1 row, § 3.6 and § 5.1, commit `66ac92f2` |
| **Owner Codex round, finding 9** (2026-09-22) — the migration guide's recommended actions cannot settle a cold parked loop | **FIXED (documentation).** A cold `ApprovalResponse` is stale-dropped and acknowledged without touching KV; a cold `cancel` against a live record retries to `MaxDeliver`. The note now states that both need the loop in process memory, that a cold parked loop is not settleable in beta.163, and that the cold approval branch is #1362 | `docs/operations/migration-beta162-to-beta163.md:1819`, commit `0c2700c2` |
| **Owner Codex round, finding 10** (2026-09-22) — the E2E stage could kill before the task's ACK and pass warm | **FIXED (test-only).** The mid-flight check waits for this task delivery's settlement on agentic-loop's `agent.task` consumer — against the floor observed before the task was published — while the model consumer is still paused. Without it a task redelivery could reach the replacement first and rebuild the loop warm, and the recorded no-rebuild mutant would go green on that schedule | `test/e2e/scenarios/agentic/stage_a_process_replacement.go:980` (the wait) and `:773` (`taskLaneConsumerName`), commit `3f76e16b`; tier and mutant re-run recorded in `tasks.md` § 7 |
| **Owner Codex round, finding 3** (2026-09-22) — a sibling lane can commit a request ID before that request is published | **FIXED.** The memory stamp of `PublishedRequestID` moved from the two ITERATION mint sites to the carrier: `stampPublishedRequest` runs under `loopRecordMu` after `publishResults` has PubAck'd (or adopted) the minted request and before `persistResultState` reads the entity. Stamped at the mint, the name was on the SHARED entity from the moment the request was built, so a deferred continuation's write on the task lane or a tool lane's CAS could commit a record naming R2 over a stream retaining only R1 — and a crash there leaves a loop no replacement can adopt and no operator can settle. **I1 is now true by construction** rather than by every mint site remembering the order: the field is only ever written by a step that runs after the request is retained. `TrackRequest` stays at the mint (route, outstanding, the deferred turn's carrier are attach-order facts); birth keeps its mint-site call, because Q1 writes its record before the first publish. The stamp is at the carrier for EVERY lane, including `writeThenPublish` ones — the approval-rejection path reaches `publishIterationRequest` through `HandleToolResult` and would otherwise never name its minted request — and that order is unchanged for them | stamp `component.go:3098` (`stampPublishedRequest`), selector `component.go:3051` (`mintedRequestID`), call sites `component.go:2324` (publish-first) and `:2271` (write-first); mint sites `handlers.go` `publishIterationRequest` and `emitRetryRequest` no longer call it, birth `handlers.go:1168` does; doc `state.go:1116` (`SetPublishedRequest`), field doc `agentic/state.go:68`; design § 3 item 4; test `loop_record_writer_test.go` (`TestARecordNeverNamesARequestBeforeItsPubAck`), commit `99cef493` |
| **Owner Codex round, finding 4** (2026-09-22) — recovery loses an acknowledged deferred user turn | **DOCUMENTED (owner ruling Q2/Q3) plus the two-line honesty fix.** The turn's text lived in the predecessor's context manager and is not recoverable; `PendingContinuationRequestID` is empty precisely because no request carried it. `restoreLoopFromRequest` now clears `PendingContinuation` in that case and warns, so a rebuilt loop does not spend an iteration re-asking the model with a context that gained nothing — a turn already inside a retained request is untouched, because the replay carries it. The loss is the documented limitation in one sentence family at three homes, and **Q8 rides it**: `taskPrompts` is the one per-loop cache the rebuild does not restore, so `LoopCompletedEvent.Prompt`, `LoopFailedEvent.Prompt` and `recoverEmptyContext`'s fallback all read empty after a replacement. The durable-turn and durable-prompt fields are an exported-surface addition to a Tier 1 package and are their own issue, #1365. Anti-gaming condition met: the limitation is a delta scenario AND the regression asserts both halves of it | clear `state.go:409`; `doc.go:257` § Recovery across a process replacement; migration `docs/operations/migration-beta162-to-beta163.md:1777`; delta scenario `specs/agentic-loop/spec.md:143` and normative sentence `:41-44`; test `loop_rebuild_test.go` (`TestARebuiltLoopDoesNotReAskForATurnItCannotRecover`), commit `1709421e` |
| **Owner Codex round 2, finding 3** (2026-09-23) — a first-iteration truncation retry still reads as an untouched birth | **FIXED.** A length-truncated first response self-heals by re-asking the same iteration under the next retry ordinal: it publishes `:req:1:1`, leaves `Iterations` at zero and runs no tool, so the record it leaves is byte-for-byte the shape the republish arm called an untouched birth. Redelivering the original task therefore minted `:req:1:0` under a NEWER retained request and the cold adopt refused the backward name as Fatal — a routine at-least-once redelivery quarantining the `agent.task` lane, which runs at `MaxAckPending` 1, for every task behind it. The republish arm now also requires the record to NAME the loop's first request, which is what the delta's GIVEN has always said; the code did not read it. A record naming anything else — a retry, a later iteration, or nothing — is a loop that moved past its task, and the request it does name is answered on the lane that owns it. No exported surface | `loop_classification.go:175` (the `PublishedRequestID == firstRequest` clause, name minted at `:174`), audit line `component.go:1450`; design § 5.1 steps 3-4, § 1 Q1 row, § 3 item 6; delta scenario `specs/agentic-loop/spec.md:117` unchanged — it already stated the condition; test `task_redelivery_integration_test.go:340` (`TestATaskRedeliveredAfterItsFirstIterationRetriedIsNotRepublished`), commit `122606ed` |
| **Owner Codex round 2, finding 4** (2026-09-23) — cold R1 reconstruction refreshed the original deadline | **FIXED.** This is the only reconstruction that runs the ORDINARY `HandleTask`, whose `configureLoopMetadata` calls `SetTimeout` (`state.go:1356`), which stamps `StartedAt = now` and `TimeoutAt = now + budget`; only the revision was restored afterwards. An expired record whose task redelivered before its response therefore resumed on a full fresh budget — a loop outliving the budget its caller set, and a deviation from the owner's explicit ruling ([issuecomment-5781101792](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5781101792)), blocking at any label. The arm now overlays the record's two timing fields through the existing `GetLoop`/`UpdateLoop` seam; a record with no deadline overlays zero onto zero, which is what the wholesale seat gives, and a failure of either call refuses the delivery rather than republishing on a forbidden deadline. **Class sweep:** the only writers of either field in production code are `SetTimeout`, reached only from `configureLoopMetadata`, reached from `HandleTask` on three paths — birth (intended), this arm (fixed), and a WARM continuation (`handlers.go:932` runs for `continuation == true`), which refreshes a live loop's deadline and is pre-existing on `main`, **recorded as a residual, not built and not filed**. **Second residual:** the R1 arm republishes `agent.created` on every redelivery — the loop-created event carries no `Nats-Msg-Id`, so the server cannot collapse it — recorded in the migration note beside the `tasks_submitted_total` at-least-once rule. **Third residual:** this arm drops a `PendingContinuation` marker without the warning the ruled clear-on-rebuild gives, because it builds a fresh entity rather than seating the record; the turn is lost either way and the ruled limitation sentence already covers the loss. No exported surface | overlay `component.go:1608` calling `component.go:1678` (`restoreRecordedLoopDeadline`); delta scenario `specs/agentic-loop/spec.md:78` (the existing one at `:69` is scoped by its WHEN to an input naming the request, and a task names the loop); migration `docs/operations/migration-beta162-to-beta163.md:1850`; test `task_redelivery_integration_test.go:446` (`TestAColdR1ReconstructionKeepsTheRecordsDeadline`), commit `73370641` |
| **Owner Codex round 2, finding 2** (2026-09-23) — approval-gate creation took the publication order deferred to L4b | **FIXED.** An `awaiting_approval` result is not terminal, so the carrier's non-terminal branch took the tool-result lane's `publishThenWrite` and published the `ApprovalPendingEvent` before writing the gate. A crash between them leaves a human an approval request with no durable gate, and the replacement's approval-response handler stale-drops the answer and acknowledges it; the branch that would recover it is the approval lane's cold arm, which is #1362's. § 5.4's "the gate keeps write → publish and there is no W4 here" was false as shipped and is true now. The carve-out is one clause in the CARRIER rather than at the call site, because the `carrierOrder` contract is where the reader looks for which result takes which order and the tool lane asks for publish-first for everything else it produces. **Hole class:** `checkApprovalGate` is the only producer of an `awaiting_approval` result in production code and is reachable only from `HandleToolResult`; the response lane cannot produce one. **Interaction with the stamp reorder (finding 3 of round 1):** a gate result mints no request — its only publication is the `ApprovalPendingEvent`, built with no `MsgID` — so `mintedRequestID` returns `""` for it and `stampPublishedRequest` is a no-op on this path under either order, which is stated in the carrier's doc. No exported surface | clause `component.go:2241` (`gated` at `:2237`), order contract `component.go:2222-2232` and the `carrierOrder` const doc `component.go:2184-2195`; delta carve-out `specs/agentic-loop/spec.md:28-30`; test `loop_carrier_test.go:148` (`TestAnApprovalGateIsWrittenBeforeItsEventIsPublished`), commit `c0a31dff` |
