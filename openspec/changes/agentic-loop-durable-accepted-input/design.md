# Design — agentic-loop-durable-accepted-input (#1365 + #1345): the accepted input is a fact of the record

> Change id `agentic-loop-durable-accepted-input`, claiming #1365 and #1345 as one design (ruling 3, #1146
> issuecomment-5828511934; draft PR #1387). Base `9e5d8455`; the inventory is `inventory.md` (149 pins, `base:`
> `9e5d8455`), HEAD `d52d726c` = base + proposal + inventory, so every code pin reads the same at either. Every premise
> below cites an inventory pin or a pin generated here the same way (`sed -n "${n}p"`, § 9).
>
> Rulings applied, none reopened: #1146 issuecomment-5828511934 rulings 2/3/5 (rows of the transition-result table,
> one MODIFIED requirement, one Tier 1 review, one migration section, one or two PRs); #1330 Q2/Q8 (the marker-only
> limitation was L4a's honest sentence; the field is this change); the 2026-09-22 standing rule (the documented
> alternative is the first row of every docket entry; nothing beyond the record's existing shape is designed, it is
> asked); ADR-106 (`agentic` is Tier 1: additive `LoopEntity` fields only; `task api:compat` reads additions); #857
> (a text field on a KV record carries a stated bound and a named over-bound behaviour); owner 2026-08-30 (sister
> inventories size the migration note, never gate design).
>
> **Shape in one paragraph.** Two additive string fields on `agentic.LoopEntity` — `task_prompt` and
> `pending_continuation_prompt` — and no other durable surface. The task prompt's in-process cache (`taskPrompts`)
> becomes the entity field, so birth, every carrier write and the wholesale rebuild seat carry it with zero new
> lines; the deferred turn's text rides the same compare-and-swap that already writes its marker, is replayed by the
> rebuild after the retained conversation when the marker is uncarried, and clears where the marker clears; identity
> adoption names the carrier so a request minted after the deferral is never both retained and replayed. Task intake
> converts by the lane's existing class policy: a malformed task is terminated (the sibling lanes' scenario), an
> invalid one is terminated, every other handler error is retried, and a birth that fails after registering its loop
> releases it so the retry is a birth and not a silent duplicate. No resumable-intake record is added: nothing
> durable precedes the failures that still acknowledge, and the record-before-publish already written at birth is
> the resumable fact once one exists. **Rows whose disposition changes: 3** (decode, wrong type, handler error), all
> on the task lane; every other row of the table is unchanged.
>
> Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ST `state.go`, LE `loop_evidence.go`, LC
> `loop_classification.go`, TW `trajectory_handler_wiring.go`, A `agentic/state.go`, UT `agentic/user_types.go`, S
> `openspec/specs/agentic-loop/spec.md`, M `docs/operations/migration-beta162-to-beta163.md`, D
> `processor/agentic-loop/doc.go`; tests LRT `loop_rebuild_test.go`, DCRIT
> `deferred_continuation_record_integration_test.go`, TRIT `task_redelivery_integration_test.go`, CDT
> `continuation_deferral_test.go`, AFPT `applied_facts_property_test.go`, TRT `transition_result_test.go`, SA
> `test/e2e/scenarios/agentic/stage_a_process_replacement.go`. `I:` = inventory pin.

## 0. Owner questions — first

Each names the cheaper alternative first, the recommendation, and the exact clause of the delta it would change. The
delta as written assumes the recommendation.

### OQ0 — the delta's composition: one MODIFIED plus one REMOVED requirement. Recommendation: confirm

- Ruling 2 says "one MODIFIED requirement, no new requirements". The internal inconsistency this pass was told to
  resolve (S:1076-1077 "a refused create or a failed publish releases the loop and is retried" vs S:1104-1111 "a
  failed first publication or loop-state write SHALL keep their pre-existing log-and-acknowledge settlement", I:
  § Fact 4) lives in a SECOND requirement, `Task intake is the one loop input class this layer does not convert`
  (S:1104). It cannot be resolved inside the MODIFIED block alone.
- **(a) REMOVED** (the delta as written): that requirement's only job was to name the exemption ("SHALL be named as
  an exemption rather than left to the absence of a scenario", S:1106-1107); when the lane converts, it has no content.
  Its three scenarios that were never about the exemption (S:1123, S:1131, S:1138) move verbatim into the MODIFIED
  block, so no scenario is lost and no `// spec:` citation breaks (no test cites any heading under S:1104; 26 tests
  cite the S:886 heading, which is unchanged — § 9 searches).
- **(b) a second MODIFIED block** rewriting S:1104 into "task intake converts as follows" — a requirement whose content
  is then a duplicate of the MODIFIED S:886 rows. Recommended against: two homes for one interpreted fact.
- Neither is a new requirement. If the owner reads ruling 2 as forbidding a REMOVED block, (b) is the fallback and
  its text is the four added scenarios of the delta moved under the S:1104 heading.

### OQ1 — the deferred-turn field's cardinality: one string or every uncarried turn. Recommendation: (a)

- **(a) one string, `pending_continuation_prompt`** — the latest uncarried turn. Matches the marker's existing shape:
  `PendingContinuation` is a bool and `PendingContinuationRequestID` one carrier (A:125, A:141), neither counts turns.
  The honest sentence it carries (delta, task-lane scenario): a second turn deferred behind the SAME outstanding
  request replaces the first as the record's uncarried turn; both are in the live loop's context and both are carried
  when no replacement intervenes (CDT:336 `TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns` is the live
  proof), but a replacement in that state replays only the latest. Two turns inside one model round trip AND a
  replacement inside the same window is the loss.
- **(b) `[]string`, every uncarried turn** — append on a deferral whose marker is already uncarried, reset on one that
  uncarries a carried marker, clear with the marker, replay in order. About five more lines and one more conditional
  in `attachContinuation`; the record's shape gains a list where it had a flag.
- Recommendation (a) by the 2026-09-22 rule: the record's shape is not extended past the fact its marker already
  models, and the loss needs two rare events to coincide. (b) is the change if the owner wants "every accepted turn"
  to be literal; it changes one AND-clause of the deferred-turn scenario.

### OQ2 — the size bound and the over-bound behaviour (#857). Recommendation: (a)

- **(a) the observed ceiling, no number of ours.** The record's text fields are bounded by the NATS server's
  `max_payload` (1 MiB default; no configuration in this repository sets it — § 9 search), which the client checks
  before sending (`nats.go@v1.53.1:4585-4589`, `ErrMaxPayload`; `KeyValue.Create`/`Update` reach it through
  `js.PublishMsg` → `nc.publish`). The `natsclient` 1 MB `MaxValueSize` guard does not apply to this bucket (I: § Fact
  5, `loopsBucket` is the raw `jetstream.KeyValue`). Over-bound behaviour, alternative-first:
  - doc sentence: "a task whose prompt pushes its loop record past the server's payload ceiling is not supported: the
    birth fails; a deferred turn that does not fit is carried in process memory only". Today a too-large birth write
    returns `WrapTransient` (C:1716) → Retry (C:1321) twice (config MaxDeliver default 2, C:1193 `DelayedDeliveryRetry(30s)`)
    then the consumer stops redelivering — a permanent refusal spent as two retries.
  - smallest code (chosen): the birth arm classifies `errors.Is(err, nats.ErrMaxPayload)` as permanent —
    `natsclient.TerminateDelivery` after the release, the same shape as the preflight refusal in the same function
    (C:1494) — with the loop id and `len(data)` in the cause; precedent `graph/clustering/storage.go:138`. The marker
    write keeps today's row: "any other write failure is best-effort and the task is acknowledged" (S:1083-1084), with
    the log line naming the refusal. Accounting the delta states honestly: the prompt already rode `agent.task` and the
    first request already carries the whole context, so a prompt that fit its own message fits the record unless the
    record's other fields (`PendingToolResults` with tool outputs, `Metadata`) fill it.
- **(b) a fixed per-field cap (e.g. 64 KiB) enforced at `preflightDecodedTask`** with a Terminate — a number the
  adopter must learn and we must pick; a prediction where the framework can observe (contract § Prefer observation to
  prediction). Recommended against unless the owner wants a number on the record; it would add one sentence to the
  payload-ceiling scenario and one config-free constant.

### OQ3 — a continuation refused because its loop has work in flight (`ErrLoopBusy`). Recommendation: (b)

- Producers: `attachContinuation` refuses with `WrapTransient(… ErrLoopBusy)` when tool calls are outstanding or the
  loop awaits approval (ST:307-316). Today the lane special-cases it: `Warn` + `return nil` = Ack (C:1541-1544), by
  #1227's shape ("ordinary user behaviour — someone typed while the agent was thinking", C:1535-1540).
- **(a) keep the acknowledged refusal** — zero diff; the honest sentence is today's: "a turn typed while the loop has
  tool calls in flight or awaits approval is refused, acknowledged, and must be re-sent". It is a "defined refusal"
  under the requirement's ACK definition (S:903-904). The delta's task-lane scenario would gain the clause "a
  continuation refused because its loop has work in flight is acknowledged as a defined refusal".
- **(b) class-derived Retry** — the row the accepted transition-result design already recorded for A5 ("when the lane
  converts, those rows take the class-derived disposition", archived design § 2 O3; S:1084-1085): the `ErrLoopBusy`
  branch keeps its `Warn` level and returns the error. On the default policy the turn is redelivered once, 30 s later
  (C:1193; MaxDeliver 2, config.go:170), which is enough for the tool-batch case — the loop is usually idle or
  waiting on the model by then, so the turn attaches or defers instead of vanishing — and not for a loop parked on a
  human approval, where the redelivery budget is spent and the turn is dropped without an ACK (a JetStream
  max-deliveries advisory, no log of ours). Fewer lines than (a) (the carve-out goes).
- Recommendation (b): it is the disposition on record, not a carve-out, and it recovers the common case. (a) if the
  owner wants #1227's refusal shape preserved as a defined refusal.

## 1. The docket

Columns: the fact · the alternative-first rows (doc sentence / smallest code) · the chosen shape · pins.

| # | Fact | Alternative-first rows | Chosen | Pins |
|---|---|---|---|---|
| F1 | **The deferred turn's text** is in the predecessor's context manager and `taskPrompts` only (I: § Fact 1 Admission); the record carries a marker and an empty carrier; the rebuild clears it with a warning (ST:435-442). | (a) doc sentence — "the turn must be re-sent" (D:280-294, M:1873-1883): L4a's accepted sentence, ruled past by #1330 Q2 → #1365. (b) **field**: `PendingContinuationPrompt string \`json:"pending_continuation_prompt,omitempty"\`` beside A:141. | (b). Set in memory where the marker is set (ST:327-333, `attachContinuation` takes the prompt); written by the marker's own compare-and-swap (C:3140-3144 + the text); replayed by the rebuild after the retained conversation (after ST:475) when uncarried; cleared where the marker clears (ST:1301-1303); adoption names the carrier (LE:503-507 + 1 line). Cardinality: OQ1. | I: A:125, A:141, ST:327-333, ST:435-442, C:3140-3144, ST:1301-1303, H:1014-1016, H:1097; § 9 LE:503, LE:507, ST:463, ST:475 |
| F2 | **The task prompt** is `taskPrompts[loopID]`, one writer (H:1016), three readers (H:2529, H:3336, H:3302-3303), the one cache the rebuild does not restore (I: § Fact 2 Mirror). | (a) doc sentence — "a consumer tolerates an empty `Prompt`; recovery uses the placeholder" (D:295-303, M:1885-1892): L4a's sentence, ruled past by Q8. (b) derive it from the retained request — rejected, § 6.2. (c) **field** `TaskPrompt string \`json:"task_prompt,omitempty"\``, and the map goes. | (c), consolidated: `CacheTaskPrompt` sets the in-memory entity's field, `GetTaskPrompt` reads it, `taskPrompts` and its `DeleteLoop` line (ST:940) are deleted. Birth renders it (C:2907 → `marshalLoopRecord` C:3167 renders the in-memory entity), a continuation's next carrier write renders it, the wholesale seat restores it (ST:444) — zero new lines on any of the three. The readers are unchanged; `recoverEmptyContext`'s literal stays as the empty-field branch (H:3303-3305). | I: ST:90, ST:1062-1065, ST:1069-1072, H:2529, H:3336, H:3302-3303, ST:481-501, UT:324; § 9 ST:444, ST:940, C:2907, C:3167 |
| F3a | **Undecodable envelope / wrong payload type** (C:1473-1482) log and Ack. | (a) doc sentence + smallest code are the same row: "a malformed task is terminated, never acknowledged as done" — the scenario the requirement already states for heartbeat lanes (S:941-946) and the response and tool lanes already implement (C:1978, C:1985, C:2456, C:2463). No record can resume bytes that never decode. | (a): two `return natsclient.TerminateDelivery(fmt.Errorf(…))`. No counter — the sibling lanes count none; `structural-invalid` (C:36) is preflight's reason for a DECODED task. | I: C:1473-1482; § 9 C:1978, C:1985, C:2456, C:2463, C:1494, S:941 |
| F3b | **`HandleTask` failure** (C:1533-1547) logs and Acks; producers H:869 (`ctx.Err()`), H:874 (`WrapInvalid`, depth), H:918-924 (`attachContinuation`: `ErrLoopTerminal` Invalid, `ErrLoopBusy` Transient), H:934/939 (`CreateLoop*`), H:968 (`startTrajectory`), H:980 (`GetLoop`), H:1102 (`buildTaskRequest`). | (a) doc sentence — "a task whose handler fails is acknowledged and lost; re-send" (today; the L1 residual, archived `settle-after-durable-effect` design:238). (b) **class-derived**: `return err` (C:1547) so the lane's policy decides (C:1314-1321: Fatal → Quarantine, `PermanentDeliveryError` → Terminate, else Retry), with `errs.IsInvalid(err)` wrapped as `TerminateDelivery` in the same function, mirroring C:1494 — the heartbeat policy does not read the Invalid class (C:1318-1319), so without the wrap an invalid task would be retried to exhaustion. Retry is honest only if the failed BIRTH released what it registered: a warm loop sends the redelivery into `HasActiveLoopForTask` (ST:642-652) and the "deduplicated" Ack (C:1577-1583) — the exact silent loss #1345 names and the birth arms already guard against (C:1711-1712). The release belongs INSIDE `HandleTask`, which knows `continuation`; the component must not release an attached loop (TW:56-62). | (b): three lines at the site, one `if errs.IsInvalid`, and a deferred `DeleteLoop` in `HandleTask` on a birth error after `CreateLoop`/`CreateLoopWithID`. `ErrLoopBusy`: OQ3. | I: C:1533-1547, H:896, C:1577-1583, ST:642; § 9 C:1316, C:1318-1319, C:1541-1544, TW:63, TW:71, ST:927, H:1162 |
| F3c | **The resumable intake record.** `pendingTaskResult` is an in-process map for the transient lineage NAK only (I: § Fact 3, C:1757-1778); the record is created before the first publish (C:1699) and a publish failure retries with the loop released (C:1741-1745); the cold fork republishes R1 from the record (LC:102 `taskRepublishFirstRequest`, C:1658-1698). | (a) **nothing new** — the honest sentence: "a task that fails before its record exists is redelivered into a fresh birth; one that fails after is redelivered into the cold fork, which republishes the request the record names". (b) promote `pendingTaskResult` to a KV record / a new durable intake record — rejected, § 6.3. | (a): the durable fact a redelivery resumes from is the record already written before the first publication; before it exists there is nothing to resume and nothing to lose once F3b releases the in-memory loop. `pendingTaskResult` is untouched. | I: C:1699-1716, C:1741-1745, C:1757-1778, C:1577-1583; § 9 LC:93, LC:102, LC:111, LC:121, TRIT:408 |
| F4 | **The spec's inconsistency** (S:1076-1077 vs S:1104-1111). | (a) REMOVED S:1104 with its non-exemption scenarios moved. (b) second MODIFIED. | (a), OQ0. | I: S:1076-1077, S:1104-1111, S:1120; § 9 S:1104, S:1115, S:1123 |
| F5 | **Size** (I: § Fact 5): no guard on this bucket; the wire's ceiling is the only bound. | OQ2 (a) observed ceiling / (b) a number. | (a). | I: acquire.go:20, C:109, C:870-874, kv.go:29-40, kv.go:358; § 9 storage.go:138, client.go:214 |
| F6 | **Sisters** (I: § Fact 6): zero readers of `pending_continuation*` or a prompt key; semsage decodes a narrow struct. | one migration line. | § 5. | I: § Fact 6 table |

Decision skills: `kv-or-stream` — not triggered (no new path; both facts ride the existing `AGENT_LOOPS` record, which
already carries the marker); `entity-or-bucket` — the facts are per-loop operational state on the loop's own record,
not graph triples: `LoopEntity` is not `Graphable` (I: § Fact 1 Graph projection) and the graph-facing
`LoopExecutionEntity` projection is untouched; `orchestration-check` — no multi-step behaviour added (replay is one
`AddMessage` inside the existing rebuild); `new-payload` — none; `query-pattern` — none.

## 2. The table rows

The MODIFIED requirement's changed and added scenarios, verbatim as they appear in `specs/agentic-loop/spec.md`
(the delta keeps all twenty-two existing headings; openspec 1.7.0 refuses a MODIFIED block that omits one). Row keys
follow the archived transition-result design § 2.

| Row | Before (`9e5d8455`) | After | Delta scenario |
|---|---|---|---|
| A1-task | `ctx.Err()` at H:869 → log + **Ack** | **Retry** (joins model/tool) | `A refusal before any mutation is retried; a refusal naming invalid input is terminated` (body changed) |
| A2-task | `WrapInvalid` depth at H:874 → log + **Ack** | **Terminate** | same |
| A5 | `attachContinuation` refusals, `CreateLoop*`, `startTrajectory`, `GetLoop`, `buildTaskRequest` errors → log + **Ack** | `ErrLoopTerminal` → **Terminate**; `ErrLoopBusy` → **Retry** (OQ3 (b)); the rest → **Retry**, a failed birth released first | `The task lane's results settle on their own owner, and its errors stay exempt` (body changed; the heading's exemption is recorded as ended) |
| — | undecodable / wrong type → log + **Ack** (C:1476, C:1482) | **Terminate** | `A malformed task is terminated, never acknowledged as done` (added) |
| B1 | unchanged (record by create-once, then publish; refused create / failed publish → released, Retry) | unchanged; the redelivery's cold fork is named | task-lane scenario, first THEN |
| B3 | marker write; text not durable; rebuild clears with a warning | marker + text in one CAS; rebuild replays; adoption names the carrier | task-lane scenario third THEN; `The deferred continuation's replacement behaviour is owed to #1365` (body changed) |
| — | `Prompt` empty on a rebuilt loop's terminal event; placeholder on recovery | the record's prompt | `A rebuilt loop's terminal event carries the prompt its record accepted` (added) |
| — | a birth whose handler failed after registering → Ack; would dedup on retry | released; Retry; the retry is a birth | `A task that fails before its record exists is redelivered into a fresh birth` (added) |
| — | a record write over the payload ceiling: birth → Retry ×2 then dropped; marker → best-effort Ack | birth → **Terminate**; marker → best-effort Ack, logged | `A loop record the payload ceiling refuses is not retried` (added) |
| O2 | obligation row | closed by this change | — |
| O3 | obligation row | closed by this change | — |

**Counts.** Disposition changes on production paths: 3 rows (decode, wrong type, handler error — with A1/A2/A5 as its
sub-rows). Under OQ3 (a) the `ErrLoopBusy` sub-row keeps Ack. Under OQ2 (b) one added scenario gains a number. No
other row of the table moves (ruling 3's "B3's replacement column changes, nothing else").

## 3. Write, replay and clear points

### 3.1 `pending_continuation_prompt`

| Point | Where | What |
|---|---|---|
| Admit (memory) | `attachContinuation(loopID, taskID, prompt)` — ST:292, inside the `outstanding` branch ST:327-333 | `entity.PendingContinuationPrompt = prompt` beside `PendingContinuation = true` and `PendingContinuationRequestID = ""`. One fact, one site, one lock. The text is `task.Prompt` (UT:324, `Validate` refuses an empty one — UT:422-423), the same string H:1014 appends to the context. |
| Write (record) | `persistDeferredContinuationMarker` — C:3105, the overlay at C:3140-3144, the CAS `loopsBucket.Update(ctx, loopID, data, revision)` at C:3150 | the overlay writes THREE fields onto the record it read: marker `true`, carrier `""`, prompt `<text>`. Same write, same revision, same lost-CAS → Retry / other → best-effort rows (C:1564-1573). The text travels from the admitted task to this write on the `HandlerResult` (an unexported field set by `deferredContinuationResult`, H:1097 → H:1119), so the lane writes the fields it owns from the value it admitted; it does not re-read the live entity (§ 6.9). |
| Render (every carrier write) | `marshalLoopRecord` C:3167 renders the in-memory entity | no change: after `TrackRequest` names a carrier (ST:1239-1240) the record says carried and the text is inert until settle. |
| Adopt | `adoptNewerRetainedRequest` — LE:503-507 | one line after LE:507: `if adopted.PendingContinuation && adopted.PendingContinuationRequestID == "" { adopted.PendingContinuationRequestID = retained.RequestID }`. Every request minted after a deferral carries the loop's whole context (H:1047, a continuation sends `cm.GetContext()`; `buildRetryRequest`/`handleToolsComplete` send `cm.GetContext()`; CDT:237 `TestTruncationRetryCarriesTheDeferredTurn`), so a retained request newer than the one the record named IS the carrier. This is what the field's own comment already claims (A:139-140 "closed by identity adoption") and what the code does not yet do. |
| Replay | `restoreLoopFromRequest` — after the conversation loop ST:463-474 and `RepairToolPairs` ST:475, before the caches ST:481 | `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" && entity.PendingContinuationPrompt != "" { cm.AddMessage(RegionRecentHistory, {user, text}); Info }`. The marker is KEPT: `HasPendingContinuation` (ST:633-637) then reads true on the rebuilt loop, the next completion advances instead of settling (H:1564, H:2764) with a context that now holds the turn once, `TrackRequest` names the carrier, `SettleRequest` clears. The existing clear-with-warning (ST:435-442) narrows to the text-less case — a record that claims a turn it does not carry — and stays where it is (before the seat). |
| Clear | `SettleRequest` ST:1301-1303 | `entity.PendingContinuationPrompt = ""` beside the two clears. `DeleteLoop` (ST:927) drops the entity. A terminal record may carry an uncarried turn's text (CDT:563 already keeps the marker there): the honest record of an accepted, unanswered turn; the bucket's 24 h TTL (acquire.go:20) is its retention. |

**Exactly-once, stated as the four windows a replacement can fall in** (the test plan's four named examples, § 4.1):

| Window | Record | Retained newest | Rebuild does | Next request carries the turn |
|---|---|---|---|---|
| W-a before the marker write landed | no marker | R | nothing (today's sentence: the turn was in memory only; the marker write's own Retry/best-effort rows) | 0 — the honest loss, unchanged |
| W-b marker + text landed, no carrier | marker, "", text; names R | R | replays the text after R's conversation, keeps the marker | 1 (the completion of R advances) |
| W-c carrier R(N+1) PubAck'd, record not updated (W4) | marker, "", text; names R | R(N+1) | adoption names R(N+1) as carrier; no replay | 1 (inside R(N+1)) |
| W-d carrier's record write landed | marker, R(N+1), text | R(N+1) | leaves it (as today) | 1 (inside R(N+1)); settles on R(N+1)'s response |

### 3.2 `task_prompt`

| Point | Where | What |
|---|---|---|
| Set (memory) | `CacheTaskPrompt` ST:1062-1065 | writes `entity.TaskPrompt` on the in-memory entity instead of `m.taskPrompts[loopID]`; called once per task delivery from H:1016 (birth and continuation alike, before the `deferred` branch). |
| Read | `GetTaskPrompt` ST:1069-1072 | reads the field. Callers H:2529, H:3336, H:3302 unchanged. H:3303-3305's literal stays as the empty-field branch. |
| Write (record) | birth: `createLoopState` C:2907 → `marshalLoopRecord` C:3167 (renders the in-memory entity); continuation: the next carrier write (`persistLoopState` C:3025) | zero new lines. The deferred lane's marker write does NOT overlay it (§ 6.7). |
| Restore | the wholesale seat `m.loops[record.ID] = &entity` ST:444 | zero lines. The cold R1 arm (`taskRepublishFirstRequest`) runs the ordinary `HandleTask` and caches the redelivered task's prompt, as today (D:300-301). |
| Delete | `DeleteLoop` ST:927 | the `delete(m.taskPrompts, loopID)` line (ST:940) and the map (ST:90) go. |

### 3.3 Task intake (`handleTaskMessage` C:1472)

| Site | Today | After |
|---|---|---|
| C:1473-1476 decode | `Error` + `return nil` | `Error` + `return natsclient.TerminateDelivery(fmt.Errorf("decode task BaseMessage: %w", err))` (C:1978's shape) |
| C:1479-1482 payload type | same | `TerminateDelivery(fmt.Errorf("task payload is %T, not *agentic.TaskMessage", …))` (C:1985's shape) |
| C:1533-1547 `HandleTask` error | `ErrLoopBusy` → `Warn` + `return nil`; else `Error` + `return nil` | `ErrLoopBusy` → `Warn` + `return err` (OQ3 (b)); `errs.IsInvalid(err)` → `Error` + `return natsclient.TerminateDelivery(err)` (C:1494's shape); else `Error` + `return err` |
| H:869-1102 `HandleTask` birth failure | the loop registered by `CreateLoop`/`CreateLoopWithID` stays in `m.loops` | a deferred release on `err != nil && !continuation && loopID != ""`: `h.loopManager.DeleteLoop(loopID)` beside the existing trajectory discard (H:971-975). A continuation is never released (TW:56-62). |
| C:1699-1716 birth record write | any error → `WrapTransient` → Retry | `errors.Is(err, nats.ErrMaxPayload)` → release + `TerminateDelivery` (OQ2 (a)); every other error as today |

The policy that reads these is unchanged: C:1314-1321.

## 4. Counterexample plan (tests, not code)

Every counterexample is today's behaviour asserted by an existing test or reproducible against `9e5d8455`; mutation
evidence per site is `cp` backup + `md5 -q` before/after, deleting the CALL, not the primitive.

| # | Counterexample on a production path | Harness | Extends | Mutation evidence |
|---|---|---|---|---|
| 4.1 | **Deferred intake → durable acceptance → replacement → cold response → the next request carries the turn exactly once.** Today: `TestARebuiltLoopDoesNotReAskForATurnItCannotRecover` (LRT:429) asserts the marker is cleared, no request is minted and `Prompt` is empty (LRT:474). | unit, `restoreLoopFromRequest` direct + `HandleModelResponse` (LRT:429's shape) | a new `TestARebuiltLoopCarriesTheTurnItsRecordAccepted` beside LRT:429, four subtests = the four windows of § 3.1: W-b (record with marker, "", text; retained R → after the completion of R a request IS minted, its messages contain the text exactly once — count, not contains — and the marker names it); W-c (record names R, retained R(N+1) carrying the text → `adoptNewerRetainedRequest` then restore → no replay, carrier = R(N+1), count 1); W-d (carrier named → left alone, count 1); W-a (no marker → nothing, count 0). LRT:429 keeps its text-less-marker case (the clear-with-warning branch). | delete the replay `AddMessage` → W-b red (count 0, no request minted); delete adoption's carrier line → W-c red (count 2); delete the `SettleRequest` clear → the settle subtest red (marker survives its carrier's response) |
| 4.2 | **The same through the production intake and record write** (integration): `TestADeferredContinuationWritesOnlyTheMarkerItOwns` (DCRIT:178) asserts the record after `deferContinuation` carries the marker and an empty carrier. | integration, `startLoopProcess` + `deferContinuation` + a replacement `startLoopProcess` (DCRIT:178's harness) | DCRIT:178: its record assertions gain `pending_continuation_prompt == <the turn>`; its "crash in the interval" arm (DCRIT:211) gains: the replacement's next minted request on `agent.request.<loopID>` contains the turn exactly once. | delete the overlay's text line at C:3140-3144 → the record assertion red; the W-c arm is 4.1's |
| 4.3 | **A rebuilt loop's completion/failure event carries the task prompt.** Today LRT:474 asserts it empty; D:295-303 documents it. | unit, LRT:429's shape | 4.1's W-b subtest asserts `CompletionState.Prompt == record.TaskPrompt` after the loop settles; a sibling asserts `recoverEmptyContext` on a rebuilt loop with an emptied context injects the record's prompt and not the literal. | delete the `CacheTaskPrompt` → field write → the birth-record assertion red; delete nothing else — the restore is the wholesale seat, so the mutation that would break it (`entity := record`) breaks every rebuild test |
| 4.4 | **A redelivered accepted task resumes rather than dedups.** Today: a `HandleTask` failure after `CreateLoop` leaves the loop warm; a second delivery is acknowledged as "deduplicated" (C:1577-1583) with no record and no request. | unit, through the production heartbeat callback like TRT:119 `TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition` | a new `TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition`: rows undecodable bytes → Terminate; wrong type → Terminate; depth → Terminate; `ctx` cancelled → Retry; `ErrLoopBusy` → Retry (OQ3 (b)) / Ack (a); `ErrLoopTerminal` → Terminate; a birth whose `startTrajectory` (or an injected `buildTaskRequest`) failure fires after registration → Retry AND `HasActiveLoopForTask` false AND the redelivery returns `Created`. Today's row for all of them is Ack (C:1476, C:1482, C:1547), which is the counterexample. | delete the `DeleteLoop` in `HandleTask`'s birth-failure path → the last row red ("deduplicated"); replace one `TerminateDelivery` with `return nil` → its row red |
| 4.5 | **Over the ceiling** (OQ2 (a)). | unit with a fake `jetstream.KeyValue` returning `nats.ErrMaxPayload` from `Create` (the loop-carrier tests' fake, `loop_carrier_test.go:352`'s shape) | one row in 4.4's table: Terminate, loop released. | replace the `errors.Is` branch → Retry |
| 4.6 | **Successful controls, unchanged**: CDT:115 `TestDeferredContinuationIsCarriedByTheCompletionResponse`, CDT:336 `TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns`, CDT:563 `TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord`, TRIT:78 `TestTaskRedeliveredToAReplacementLeavesOneFirstRequest`, TRIT:408 `TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry`, TRIT:718 `TestAColdContinuationForALoopNoProcessHoldsIsRefused`, AFPT:123 `TestPropAppliedFactsHoldAcrossEveryCrashWindow`. | — | run by name, `-race -count=1`; they must stay green with no edits beyond an `attachContinuation` call-site argument. | — |
| 4.7 | **Final validation, `task e2e:agentic`**: `verifyMidFlightLoopAcrossReplacement` (SA:898) pauses the model-request consumer (SA:909), publishes a task, replaces the process, resumes, and expects `agent.complete` (SA:1010). | e2e tier | after R1 is retained and before the replacement, publish a continuation task naming the loop (deferred behind R1); after the resume, the completion of R1 must advance, R2 on `agent.request.<loopID>` must contain the turn exactly once, and `agent.complete` must carry `prompt`. The tier is the proposal's final validation of the intake disposition; it is not the iteration loop. | — |

**PBT decision** (`docs/contributing/01-testing.md` § When to Use Property-Based Testing): the exactly-once obligation
is a property over the four replacement windows of § 3.1, a finite set, so four named examples driving the production
rebuild (4.1) enumerate it and are stronger than a sampled property. `TestPropAppliedFactsHoldAcrossEveryCrashWindow`
(AFPT:123) is NOT extended: its model retains requests and an applied set and has no conversation (AFPT:67-69, "the
tool batch a response carries belongs to the cold rebuild, which no action here drives"); a deferred-turn action would
need a second in-memory conversation model, which is the reconstruction the testing policy forbids. If the owner wants
the property, it is a follow-up on the model, not on this change.

**Invariants and their spec homes** (contract § Design discipline):

| Invariant (every input, every window) | Spec home |
|---|---|
| I-A While a record's marker is uncarried, `pending_continuation_prompt` holds the (latest, OQ1) uncarried turn and no retained request carries it. | delta: requirement paragraph "from the write that sets its marker to the write that clears it"; the deferred-turn scenario's third AND (adoption) |
| I-B A rebuilt loop's next minted request carries an uncarried deferred turn exactly once. | delta: requirement paragraph; the deferred-turn scenario |
| I-C `task_prompt` on a record is the prompt of the task the record's `task_id` names, from the birth write on. | delta: requirement paragraph; the terminal-event scenario |
| I-D No task delivery is positively acknowledged on a failure that left nothing durable behind it; the defined refusals (duplicate, applied, unheld continuation) are the exceptions and are named. | delta: requirement paragraph; the task-lane scenario's last THEN; the malformed-task and fresh-birth scenarios |

## 5. Migration (one section; `docs/operations/migration-beta162-to-beta163.md`, after the #1374 section)

`## Durable accepted input, and task intake settles like every other lane (#1365, #1345)`:

- **Record keys added** (`agentic.LoopEntity`, additive, `omitempty`, `Validate()` does not require them — the shape of
  the L4a section at M:1827-1832): `task_prompt` (the prompt of the task `task_id` names, from the birth write; a
  continuation's next carrier write moves it with `task_id`), `pending_continuation_prompt` (the deferred turn's text
  while `pending_continuation` is true and `pending_continuation_request_id` is empty; cleared with them). A record
  written before this tag decodes with both empty and behaves as the M:1873-1892 paragraphs describe; those two
  paragraphs are marked superseded by this section rather than deleted (the tag range is the same).
- **What a consumer sees**: `LoopCompletedEvent.prompt` / `LoopFailedEvent.prompt` are populated after a process
  replacement (they were empty; a consumer that tolerated empty keeps working); a loop record now carries prompt
  text, so a listing that decodes the whole record pulls it (semsage's `processor/ui-api/types.go:14-27` decodes a
  narrow struct and is unaffected — I: § Fact 6).
- **Intake dispositions on `agent.task`** (observable only through the consumer's redelivery behaviour and metrics;
  no wire or subject change): undecodable or wrong-type → Terminate (was Ack); an over-depth task or a continuation
  of a settled loop → Terminate (was Ack); any other handler failure → Retry, on the configured policy (default one
  redelivery after 30 s, `max_deliver` 2); a continuation refused because its loop has work in flight → Retry (OQ3
  (b); was Ack — under (a) this line says Ack). `tasks_submitted_total` is unchanged (at-least-once, M:1942).
- **Size**: the record's text fields share the server's `max_payload` (1 MiB default); a birth write the client
  refuses for size terminates the task (was: retried to the redelivery budget); a marker write it refuses is logged
  and the turn stays in process memory. Under OQ2 (b) this line names the cap.
- **Sister impact, measured read-only** (I: § Fact 6): zero readers or writers of `pending_continuation*`,
  `task_prompt` or any prompt key in semsource, semboids, semsage, semops, semdragon, semconnect, semmem, semembed,
  seminstruct, semmachina, semteams, semspec, semdev, servicesim — one line: nothing to do. NOT RUN: `agent.task`
  producers per sister (I: § NOT RUN) — the disposition change is invisible to a fire-and-forget producer except as a
  redelivery, so the line stands either way.
- Not BREAKING under ADR-106 § 5 (no removed or changed exported symbol; `task api:compat` must read `agentic` as
  additions only). The commit is `feat(agentic-loop):`; the intake disposition change is behaviour on a failure path
  and lives here, not in a `!`.

## 6. Rejected simpler alternatives, and why

| # | Alternative | Why not |
|---|---|---|
| 6.1 | Doc sentences only (D:280-303, M:1873-1892 stay; the exemption requirement stays). | The accepted L4a sentence for its window; #1330 Q2/Q8 ruled the field is this change; #1345's residual is open since L1. First row of F1/F2/F3b by the rule, not chosen. |
| 6.2 | Derive the task prompt from the retained request instead of a field. | The request's messages are unlabelled `user`-role entries — the task's turn, `recoverEmptyContext`'s synthetic one (H:3312-3315), a product-injected one are indistinguishable; the conversation is GC/eviction-subject (recovery exists because it can be EMPTY, H:3301); `taskPrompts`' semantics is last-task-only, which no message position expresses; and the `agent.task` stream is not addressable by task id. A linkage resting on position or content is the naming-coincidence tell (contract § Design discipline). |
| 6.3 | A durable resumable-intake record (promote `pendingTaskResult`, or a new KV key). | No durable effect precedes the failures that still acknowledge, so there is nothing for a record to resume; once the record exists it IS the resumable fact (LC:102). `pendingTaskResult` bridges one transient NAK in-process and needs no promotion. A second durable home would be a same-class collision with the record. |
| 6.4 | `[]string` for the deferred turn. | OQ1 (b): beyond the marker's shape. |
| 6.5 | A fixed byte cap at preflight. | OQ2 (b): predicting a value the framework observes. |
| 6.6 | Quarantine for the task lane's post-mutation errors, applying S:907-909 literally. | The only producers after H:1014's append are `GetLoop` on a released loop (a race; the redelivery meets the cold fork and is refused with a counted reason) and `buildTaskRequest` (marshal / `ResolveSubject`, no production trigger — archived transition-result design § 0 OQ2). A lane latch for an unreachable path. Recorded as § 7.3. |
| 6.7 | Overlay `task_id`/`task_prompt` at the marker write too. | The marker write owns the marker (DCRIT:178's invariant: "a lane writes only the fields it OWNS"); moving `task_id` there changes a classification input (`taskContinuationUnheld` refuses on it, LC:121) on a write that is not the carrier's. The carrier's next write moves both, as today. § 7.2 records the window. |
| 6.8 | Count decode/type failures on `task_intake_rejections_total`. | The response and tool lanes count none (C:1978, C:2456); `structural-invalid` is preflight's. A new label value for parity with nothing. |
| 6.9 | The marker write reads the LIVE carrier instead of writing `""`. | Reintroduces the hole L4a closed: `TrackRequest` names a carrier at mint, before PubAck (A:134-140); a record naming a carrier nothing retains leaves the turn unrecoverable. `""` is the honest claim at admission. |
| 6.10 | Replay the turn inside `HandleModelResponse` (at carry time) instead of at rebuild. | Two homes for "seat the record's facts into memory"; the rebuild already restores five caches at one point (ST:481-501) and the problem shape (I: § Problem shape) is exactly that mirror. |

## 7. Residuals (doc comments in the code, not issues — owner 2026-09-02)

1. **The marker write landing after the carrier's own record write.** If, between `attachContinuation` and
   `persistDeferredContinuationMarker` (C:1533 → C:1564), the response lane received R's answer, minted R(N+1)
   carrying the turn, got its PubAck and wrote the record, the marker write then overlays "" onto a record whose
   carrier was R(N+1); a replacement in that state replays a turn R(N+1) already carries (once more, not lost). Two
   NATS round trips inside the task lane's own delivery; recorded at C:3140-3144, not coded.
2. **A replacement between the marker write and the next carrier write** rebuilds a loop whose `task_id` and
   `task_prompt` are the previous task's (the marker write does not move them, § 6.7): the terminal event's `prompt`
   is the birth's, not the deferred turn's, and `task_id` was already so in L4a (D:290-292). Recorded at ST:444.
3. **A continuation's post-mutation handler failure** retries into `HasActiveLoopForTask` and is acknowledged as a
   duplicate with its turn in memory (§ 6.6): no production producer. Recorded at H:1097.
4. **Inert text on a carried record**: `pending_continuation_prompt` stays until settle after the carrier is named;
   a terminal record at the iteration ceiling keeps an uncarried turn's text (CDT:563). Retention is the bucket's TTL.
5. `Validate()` (A:164) checks neither field; a fixture that writes `AGENT_LOOPS` directly may set a prompt with no
   marker (harmless) or a marker with no prompt (the clear-with-warning branch).
6. Under OQ3 (b), a busy refusal's exhausted redelivery budget is visible only as a JetStream max-deliveries
   advisory; the loop logs each attempt's `Warn` and nothing at the drop.

## 8. Adopter seam (contract § The adopter seam inventory)

Surfaces reached from outside: (i) two JSON keys on `AGENT_LOOPS` records; (ii) `prompt` on the completion/failure
events after a replacement; (iii) the `agent.task` delivery disposition; (iv) the Go surface — `agentic.LoopEntity`
gains two exported fields (Tier 1, additive), `processor/agentic-loop` changes no exported symbol (`attachContinuation`
is unexported; `CacheTaskPrompt`/`GetTaskPrompt` keep their signatures).

1. **What must they know?** A record reader: nothing — unknown keys are ignored by every sister decoder found (I:
   § Fact 6). An event consumer: `prompt` is now populated where it was empty; one that special-cased empty keeps
   working. An `agent.task` producer: a malformed task is no longer acknowledged (the same as every other lane); a
   refused-busy turn is redelivered once (OQ3 (b)). A fixture that writes records directly: the two keys' meaning
   (§ 7.5). One item per surface, none load-bearing for correctness.
2. **If they do nothing?** Same observations as today, plus a populated `prompt`. The intake change is invisible to a
   fire-and-forget producer except as a redelivery or a JetStream advisory.
3. **Where do they find out?** The record itself (typed JSON, runtime) and the event; the migration section for the
   disposition — "doc" for a non-correctness fact, which is acceptable.
4. **What SHOULD they know?** Nothing. The gap between 1 and 4 is empty for readers; for producers the one honest new
   fact (a malformed task is terminated) is the framework observing its own outcome rather than the producer
   predicting anything.

**Prefer observation to prediction**: the size bound is the one place a knob could have appeared; OQ2 (a) deletes
it by observing the client's refusal. No adopter is asked to compute a size, a subject, a bucket or a deadline.

## 9. Pins generated for this design (`sed -n "${n}p"` at `9e5d8455`; every other pin is the inventory's)

- `processor/agentic-loop/loop_evidence.go:503` — `adopted.PublishedRequestID = retained.RequestID`
- `processor/agentic-loop/loop_evidence.go:507` — `adopted.PendingToolResults = nil`
- `processor/agentic-loop/loop_evidence.go:609` — `if err := c.handler.loopManager.restoreLoopFromRequest(ctx, record.entity, request); err != nil {`
- `processor/agentic-loop/state.go:318` — `entity.TaskID = taskID`
- `processor/agentic-loop/state.go:444` — `m.loops[record.ID] = &entity`
- `processor/agentic-loop/state.go:445` — `m.pendingTools[record.ID] = make(map[string]bool)`
- `processor/agentic-loop/state.go:463` — `for _, msg := range conversation {`
- `processor/agentic-loop/state.go:475` — `cm.RepairToolPairs()`
- `processor/agentic-loop/state.go:642` — `func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {`
- `processor/agentic-loop/state.go:927` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-loop/state.go:940` — `delete(m.taskPrompts, loopID)`
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:71` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/handlers.go:1162` — `func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {`
- `processor/agentic-loop/component.go:35` — `taskIntakeRejectionLane   = "decoded-task"`
- `processor/agentic-loop/component.go:36` — `taskIntakeRejectionReason = "structural-invalid"`
- `processor/agentic-loop/component.go:1193` — `settleRetry, retryErr := natsclient.DelayedDeliveryRetry(30 * time.Second)`
- `processor/agentic-loop/component.go:1316` — `return natsclient.DeliveryDecisionQuarantine, handlerErr`
- `processor/agentic-loop/component.go:1318` — `var permanent *natsclient.PermanentDeliveryError`
- `processor/agentic-loop/component.go:1319` — `if errors.As(handlerErr, &permanent) {`
- `processor/agentic-loop/component.go:1494` — `return natsclient.TerminateDelivery(err)`
- `processor/agentic-loop/component.go:1541` — `if errors.Is(err, ErrLoopBusy) {`
- `processor/agentic-loop/component.go:1542` — `c.logger.Warn("Task refused — the loop it names still has work in flight",`
- `processor/agentic-loop/component.go:1564` — `if err := c.persistDeferredContinuationMarker(ctx, result.LoopID); errors.Is(err, natsclient.ErrKVRevisionMismatch) {`
- `processor/agentic-loop/component.go:1978` — `return nil, "", natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:1985` — `return nil, "", natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2456` — `return natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2463` — `return natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2907` — `func (c *Component) createLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:3167` — `func (c *Component) marshalLoopRecord(loopID string) ([]byte, error) {`
- `processor/agentic-loop/loop_classification.go:93` — `taskBirth taskDisposition = iota`
- `processor/agentic-loop/loop_classification.go:102` — `taskRepublishFirstRequest`
- `processor/agentic-loop/loop_classification.go:111` — `taskApplied`
- `processor/agentic-loop/loop_classification.go:121` — `taskContinuationUnheld`
- `processor/agentic-loop/config.go:170` — `MaxDeliver        int    `json:"max_deliver,omitempty" schema:"type:int,description:Maximum redelivery attempts for long-running consumers. Must cover the fixed two-entry BackOff,default:2,min:2,max:10,category:advanced"``
- `processor/agentic-loop/approval_response_handler.go:330` — `const continuationUnavailableReason = "continuation_unavailable"`
- `agentic/state.go:140` — `// closed by identity adoption rather than by this field.`
- `agentic/state.go:164` — `func (e *LoopEntity) Validate() error {`
- `agentic/user_types.go:423` — `return fmt.Errorf("prompt required")`
- `graph/clustering/storage.go:138` — `if stderrors.Is(err, nats.ErrMaxPayload) {`
- `natsclient/client.go:214` — `func (m *Client) MaxPayload() (int64, error) {`
- `openspec/specs/agentic-loop/spec.md:888` — `Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict`
- `openspec/specs/agentic-loop/spec.md:941` — `#### Scenario: A malformed heartbeat-lane input is terminated, never acknowledged as done`
- `openspec/specs/agentic-loop/spec.md:1040` — `#### Scenario: A refusal before any mutation is retried; a refusal naming invalid input is terminated`
- `openspec/specs/agentic-loop/spec.md:1073` — `#### Scenario: The task lane's results settle on their own owner, and its errors stay exempt`
- `openspec/specs/agentic-loop/spec.md:1087` — `#### Scenario: The deferred continuation's replacement behaviour is owed to #1365`
- `openspec/specs/agentic-loop/spec.md:1104` — `### Requirement: Task intake is the one loop input class this layer does not convert`
- `openspec/specs/agentic-loop/spec.md:1115` — `#### Scenario: A task delivery fails after its loop exists`
- `openspec/specs/agentic-loop/spec.md:1123` — `#### Scenario: A tool result is cancelled after the loop has advanced`
- `docs/operations/migration-beta162-to-beta163.md:1827` — `### One field is added to `agentic.LoopEntity``
- `docs/operations/migration-beta162-to-beta163.md:1873` — `**A deferred turn is durable as a MARKER only, and so is nothing about the task prompt.** A continuation admitted`
- `docs/operations/migration-beta162-to-beta163.md:1885` — `The loop's task prompt is the same limitation one field over. A loop rebuilt from its record and a retained request —`
- `processor/agentic-loop/loop_rebuild_test.go:429` — `func TestARebuiltLoopDoesNotReAskForATurnItCannotRecover(t *testing.T) {`
- `processor/agentic-loop/loop_rebuild_test.go:474` — `require.Empty(t, completion.CompletionState.Prompt,`
- `processor/agentic-loop/deferred_continuation_record_integration_test.go:178` — `func TestADeferredContinuationWritesOnlyTheMarkerItOwns(t *testing.T) {`
- `processor/agentic-loop/task_redelivery_integration_test.go:408` — `func TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry(t *testing.T) {`
- `processor/agentic-loop/task_redelivery_integration_test.go:718` — `func TestAColdContinuationForALoopNoProcessHoldsIsRefused(t *testing.T) {`
- `processor/agentic-loop/continuation_deferral_test.go:115` — `func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {`
- `processor/agentic-loop/continuation_deferral_test.go:336` — `func TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns(t *testing.T) {`
- `processor/agentic-loop/applied_facts_property_test.go:123` — `func TestPropAppliedFactsHoldAcrossEveryCrashWindow(t *testing.T) {`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:898` — `func (s *Scenario) verifyMidFlightLoopAcrossReplacement(`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:909` — `if _, err := agentStream.PauseConsumer(ctx, modelRequestConsumerName, time.Now().Add(2*time.Minute)); err != nil {`

Searches run for this design (beyond the inventory's): `git grep -n "spec: agentic-loop / <heading>" -- '*_test.go'`
for each heading the delta rewrites or removes → 0 for every scenario heading, 26 for the S:886 requirement heading
(unchanged); `git grep -n "max_payload" -- '*.conf' '*.yml' '*.yaml' '*.json' 'test/*'` → 0; `git grep -n
"ErrMaxPayload\|MaxPayload" -- '*.go'` → 10 (the two pinned); `grep -n "PendingContinuation"
processor/agentic-loop/component.go` → 4 (all inside the marker write; the cold arms do not touch the marker);
`grep -rn "deferred\|continuation\|prompt" test/e2e/scenarios/agentic/{process_replacement,stage_a_process_replacement}.go`
→ 0 (the tier does not cover the turn today); nats.go `v1.53.1` `jetstream/kv.go:1049,1146,1199` (`Create`/`Update`
→ `js.PublishMsg`) and `nats.go:4585-4589` (`ErrMaxPayload`); `openspec` 1.7.0 `dist/core/specs-apply.js:287` (a
MODIFIED block must restate every current scenario by name).
