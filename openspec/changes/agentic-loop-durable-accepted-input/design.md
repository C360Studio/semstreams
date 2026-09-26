# Design — agentic-loop-durable-accepted-input (#1365 + #1345): the accepted input is a fact of the record

> Change id `agentic-loop-durable-accepted-input`, claiming #1365 and #1345 as one design (ruling 3, #1146
> issuecomment-5828511934; draft PR #1387). Base `9e5d8455`; the inventory is `inventory.md` (149 pins, `base:`
> `9e5d8455`); every commit on the branch since is docs-only, so every code pin reads the same at base and HEAD. Every
> premise below cites an inventory pin or a pin generated here the same way (`sed -n "${n}p"`, § 9).
>
> **Amended after design review round 1 (FAIL, small amendments; this revision).** Blocking 1: the adoption line is
> withdrawn — a turn can defer behind a request that is tracked but not yet on the record, and adoption then names a
> carrier minted BEFORE the turn (silent loss where today there is a warning); the rebuild now replays on every
> uncarried marker and accepts a logged duplicate in the one window the record cannot tell apart (§ 3.1 W-c/W-e, OQ4).
> High 2: `ErrLoopBusy` keeps its acknowledged refusal — Retry parks the whole task lane (MaxAckPending 1) and the repo
> already rejected it there (OQ3 flipped to (a)). High 3: the bound is over the WHOLE record, the carrier-write refusal
> has its row, and `task_prompt` is pre-selected birth-only so the deferred text is stored once (OQ2 re-weighed, OQ5).
> Medium 4–8 and the nits: the transient-lineage sentence is carried into the MODIFIED block; the two headings that read
> false after archive are named in OQ0 as ruling 2's cost; the `HandleTask` release code is dropped for a doc sentence;
> § 7.3 is corrected; the evidence embedding is an adopter-seam row; C:3107; graphresearch's writer noted; § 10 lists
> the unproven items. Nothing the reviewer confirmed is reopened.
>
> Rulings applied, none reopened: #1146 issuecomment-5828511934 rulings 2/3/5 (rows of the transition-result table,
> one MODIFIED requirement, one Tier 1 review, one migration section, one or two PRs); #1330 Q2/Q8 (the marker-only
> limitation was L4a's honest sentence; the field is this change); the 2026-09-22 standing rule (the documented
> alternative is the first row of every docket entry; nothing beyond the record's existing shape is designed, it is
> asked); ADR-106 (`agentic` is Tier 1: additive `LoopEntity` fields only; `task api:compat` reads additions); #857
> (a text field on a KV record carries a stated bound and a named over-bound behaviour); owner 2026-08-30 (sister
> inventories size the migration note, never gate design).
>
> **Shape in one paragraph.** Two additive string fields on `agentic.LoopEntity` — `task_prompt` (the prompt of the
> task that bore the loop, written once at birth) and `pending_continuation_prompt` (a deferred turn's text, written
> by the same compare-and-swap as its marker, replayed by the rebuild after the retained conversation whenever the
> marker is uncarried, cleared where the marker clears) — and no other durable surface. The in-process cache
> `taskPrompts` becomes the entity field, so birth, every carrier write and the wholesale rebuild seat carry it with
> zero new lines. Task intake converts by the lane's existing class policy: a malformed task is terminated (the
> sibling lanes' scenario), an invalid one is terminated, a transient handler error is retried, and the defined
> refusals — duplicate, applied, unheld continuation, busy loop — stay acknowledged and named. No resumable-intake
> record is added: no durable effect precedes the failures that still acknowledge, and the record-before-publish
> already written at birth is the resumable fact once one exists. **Rows whose disposition changes: 3** (decode,
> wrong type, handler error), all on the task lane; every other row of the table is unchanged.
>
> Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ST `state.go`, LE `loop_evidence.go`, LC
> `loop_classification.go`, TW `trajectory_handler_wiring.go`, TJ `trajectory.go`, A `agentic/state.go`, UT
> `agentic/user_types.go`, S `openspec/specs/agentic-loop/spec.md`, M `docs/operations/migration-beta162-to-beta163.md`,
> D `processor/agentic-loop/doc.go`, TS `processor/agentic-dispatch/terminal_settlement.go`; tests LRT
> `loop_rebuild_test.go`, DCRIT `deferred_continuation_record_integration_test.go`, TRIT
> `task_redelivery_integration_test.go`, CDT `continuation_deferral_test.go`, AFPT `applied_facts_property_test.go`,
> TRT `transition_result_test.go`, SA `test/e2e/scenarios/agentic/stage_a_process_replacement.go`. `I:` = inventory
> pin.

## 0. Owner questions — first

Each names the cheaper row first, the pre-selected answer, and the exact clause of the delta it would change. The
delta as written assumes the pre-selection.

### OQ0 — the delta's composition, and ruling 2's cost: two headings read false after archive. Pre-selected: REMOVED, and the headings stay

- Ruling 2 says "one MODIFIED requirement, no new requirements". The inconsistency this pass was told to resolve
  (S:1076-1077 vs S:1104-1111, I: § Fact 4) lives in a SECOND requirement, `Task intake is the one loop input class
  this layer does not convert` (S:1104); it cannot be resolved inside the MODIFIED block alone. **(a) REMOVED** (the
  delta as written): that requirement's only job was to name the exemption (S:1106-1107); when the lane converts it
  has no content. Its three scenarios that were never about the exemption (S:1123, S:1131, S:1138) and its one
  sentence that still states a fact — "the birth-failure and transient-lineage paths of the same lane are NOT exempt"
  (S:1112-1113, the `pendingTaskResult` resume) — move verbatim into the MODIFIED block. No `// spec:` citation breaks
  (§ 9 searches). **(b)** a second MODIFIED block rewriting S:1104 — a duplicate home; recommended against. Neither is
  a new requirement.
- **The cost of ruling 2, called out in the first round (the "call out a ruling" rule).** Two existing scenario
  headings read false once this change archives: `The task lane's results settle on their own owner, and its errors
  stay exempt` (S:1073) and `The deferred continuation's replacement behaviour is owed to #1365` (S:1087). openspec
  1.7.0 cannot rename a scenario inside a MODIFIED block — `specs-apply.js:287` refuses a MODIFIED block that omits
  any current scenario by name — so the only rename route is REMOVED + ADDED of the whole S:886 requirement, which
  ruling 2's letter forbids as a new requirement. The delta therefore keeps both headings and corrects their bodies
  (each body's last clause records that the heading's claim ended with this change). **Alternative, named:** the
  owner waives ruling 2's letter for a REMOVED + ADDED pair of the same requirement heading in this change (the 26
  `// spec:` citations resolve to the heading, which comes back identical), or a one-line spec-hygiene change after
  archive does the same. Pre-selected: keep the headings; the cheaper row is zero extra blocks.

### OQ1 — the deferred-turn field's cardinality: one string or every uncarried turn. Pre-selected: (a)

- **(a) one string, `pending_continuation_prompt`** — the latest uncarried turn. Matches the marker's existing shape:
  `PendingContinuation` is a bool and `PendingContinuationRequestID` one carrier (A:125, A:141), neither counts turns.
  The honest sentence it carries (delta, deferred-turn scenario): a second turn deferred behind the SAME outstanding
  request replaces the first as the record's uncarried turn; both are in the live loop's context and both are carried
  when no replacement intervenes (CDT:336 is the live proof), but a replacement in that state replays only the
  latest. Two turns inside one model round trip AND a replacement inside the same window is the loss.
- **(b) `[]string`, every uncarried turn** — append on a deferral whose marker is already uncarried, reset on one that
  uncarries a carried marker, clear with the marker, replay in order. About five more lines and one more conditional
  in `attachContinuation`; the record's shape gains a list where it had a flag.
- (a) by the 2026-09-22 rule. (b) changes one AND-clause of the deferred-turn scenario.

### OQ2 — the size bound and the over-bound behaviour (#857), re-weighed over the whole record. Pre-selected: (a)

- **The bound is over the whole record.** `AGENT_LOOPS` has no per-value guard (I: § Fact 5); the NATS client refuses
  a message larger than the server's `max_payload` (1 MiB default; nothing in this repository sets it, § 9) before
  sending it (`nats.go@v1.52.0:4461-4463`, the version `go.mod` pins, `ErrMaxPayload`; `KeyValue.Create`/`Update` reach it through `js.PublishMsg`
  → `nc.publish`). What is bounded is `len(json.Marshal(entity))` — every field summed: `PendingToolResults` with tool
  outputs, `Metadata`, and now `task_prompt` plus, while a turn is deferred, `pending_continuation_prompt`. Under
  OQ5 (a) the deferred turn's text is on the record ONCE; under OQ5 (b) it is on it twice until settle, so a turn
  above roughly half the ceiling less the record's other fields does not fit although it fit `agent.task`.
- **Three writes can meet the refusal, each with its row (delta, payload-ceiling scenario):**
  - the birth write (`createLoopState` C:2907): today `WrapTransient` (C:1716) → Retry (C:1321) for MaxDeliver attempts
    (config.go:170, default 2; C:1193 `DelayedDeliveryRetry(30s)`) on a message no redelivery can fix — and the task
    lane runs at MaxAckPending 1 (C:1260-1263), so those attempts park every other task. Chosen: the birth arm
    classifies `errors.Is(err, nats.ErrMaxPayload)` as permanent — release, then `natsclient.TerminateDelivery` in the
    shape of C:1494, with the loop id and `len(data)` in the cause (precedent `graph/clustering/storage.go:138`).
  - the marker write (`persistDeferredContinuationMarker` C:3107): today "any other write failure is best-effort and
    the task is acknowledged" (S:1083-1084). The first draft's "held in process memory only" was untrue: the
    in-memory entity would keep the text and every later carrier write would render it into the same refusal. Chosen:
    on `ErrMaxPayload` the lane drops the text from the in-memory entity (`PendingContinuationPrompt = ""`), logs a
    `Warn` naming the loop and the size, and returns as best-effort; the turn stays in the context manager and is
    carried by the next request exactly as before this change, and it is not durable — the pre-change sentence for
    this one case. No second write: the marker does not land either, which is today's W-a state.
  - a carrier write (`persistLoopState` C:3025, the CAS at C:3071): a non-conflict error is returned plain (C:3079)
    and the carrier's ordinary row quarantines it ("other write → Quarantine", archived transition-result design § 2
    B5; C:2271-2281, C:2319-2329). The row is unchanged by this change and is stated in the delta because the text fields now
    count toward it; under OQ5 (a) the only text a carrier write can add beyond the birth prompt (which fit at birth
    or was terminated) is a deferred turn that fit its own marker write, so this refusal needs the record's other
    fields to have grown since — the pre-existing #857 class, not a new one.
- **(a) observe** the client's refusal at the three writes — pre-selected. It is the cheaper honest row: the error is
  typed and authoritative, no extra call, no seam beyond the KV fake returning it. **(b) predict** with
  `natsclient.Client.MaxPayload()` (`natsclient/client.go:214`; the component already calls it for trajectory queries,
  C:3180-3185): compare `len(data)` before the CAS and refuse in-process. Not cheaper — the same clear-and-log is
  needed, one more call and one more fake, and the method's own doc says the publish result is authoritative
  ("diagnostic and may change"). Its one use, reporting the ceiling in the log line, is not worth the seam. A fixed
  per-field cap at preflight remains rejected as a predicted number the adopter must learn.

### OQ3 — a continuation refused because its loop has work in flight (`ErrLoopBusy`). Pre-selected: (a) keep the acknowledged refusal

- Producers: `attachContinuation` refuses with `WrapTransient(… ErrLoopBusy)` when tool calls are outstanding or the
  loop awaits approval (ST:307-316). The lane special-cases it: `Warn` + `return nil` = Ack (C:1541-1544), by #1227's
  shape ("ordinary user behaviour — someone typed while the agent was thinking", C:1535-1540).
- **(a) keep it** — zero diff. It is a defined refusal under the requirement's ACK definition (S:903-904) and the
  delta names it as one; the honest sentence is today's: "a turn typed while the loop has tool calls in flight or
  awaits approval is refused, acknowledged, and must be re-sent".
- **(b) class-derived Retry — rejected.** The task lane runs at MaxAckPending 1 (C:1260-1263), so a Retry parks ALL
  intake for the redelivery budget, and the repo already rejected exactly this on this lane: "Retry parks the whole
  task lane, which runs at MaxAckPending 1, for MaxDeliver attempts on a message no redelivery can fix" (C:1440-1442,
  the unheld-continuation skip; that quote states the unheld case's reason — a busy loop can clear, so for the busy refusal the only reason is that a Retry parks the lane). On the default policy the turn is redelivered once, 30 s later (C:1193; MaxDeliver 2),
  and on the second miss it is dropped with no settlement of ours. The archived transition-result design's O3 row
  ("the error rows take the class-derived disposition") is amended by this design for this one refusal, and the
  delta's requirement paragraph says so.

### OQ4 — the window the record cannot tell apart: a duplicated turn, or a third field. Pre-selected: (a) duplicate and log

- **The finding (design review round 1, blocking).** A turn can defer behind a request R(N) that is tracked as
  outstanding but not yet on the record: "by the time a continuation can defer, the request it is deferring behind
  has been tracked as outstanding … while the request that justifies both is still unpublished" (C:3091-3095), and
  the deferred lane takes `loopRecordMu` (C:3113) before the carrier's `persistLoopState` does (C:3047). The record then
  names R(N-1) with the uncarried marker and the text. A crash after R(N)'s PubAck and before its record write leaves
  the record naming R(N-1) while the stream retains R(N); on rebuild `adoptNewerRetainedRequest` adopts R(N)
  (LE:487-503). The first draft's "adoption names the carrier" line would have named R(N) — built BEFORE the turn
  existed — so nothing replayed and R(N)'s completion settled the marker (ST:1301-1303): the turn lost SILENTLY where
  today it is lost with a warning. The same holds under OQ1 (a) for a second turn behind R(N+1). The record alone
  cannot distinguish this window (W-e below) from W-c (a carrier PubAck'd after the deferral, record not yet updated).
- **(a) drop the adoption line; replay on every uncarried marker; accept a duplicate in W-c, logged** —
  pre-selected. In W-e the replay is exactly once; in W-c the retained request already carries the turn and the
  replay appends it a second time (the model sees the same user turn twice; degraded, never lost — a duplicate is
  better than a loss). The rebuild logs the replay at Info naming the loop and the retained request; adoption's own
  Info line (LE:540) is the other half an operator reads. Zero new fields. The delta's exactly-once sentence is
  stated with this one exception.
- **(b) a third field recording which request the turn deferred behind** (`pending_continuation_behind`, set with
  the marker, cleared with it): the rebuild replays iff the retained request is the one the turn deferred behind and
  skips iff it is newer — exact in every window, no duplicate. It adds a field beyond the marker's existing shape,
  which is the complexity ratchet the 2026-09-22 rule reserves for the owner; the cheaper row is (a).
- Either way the field's existing comment (A:139-140, "the opposite window … is closed by identity adoption rather
  than by this field") over-claims: adoption names no carrier and the window is not closed by it. The comment is
  rewritten (tasks 2.5) to say what is true.

### OQ5 — `task_prompt`'s meaning: the prompt that bore the loop, or the latest task's. Pre-selected: (a) birth-only

- Today `taskPrompts[loopID]` is written on EVERY task delivery (H:1016, before the `deferred` branch), so on a continued
  loop it holds the latest turn — incidentally: its own doc says "the original task prompt" (ST:1059, H:3299), and the
  readers name it `Prompt` on the terminal events and "Original task" in `recoverEmptyContext` (H:3313-3315).
- **(a) birth-only** — `CacheTaskPrompt` runs only when `!continuation`; `task_prompt` is the prompt of the task that
  bore the loop, written once by the birth write and never rewritten. Cheaper: one condition, the deferred turn's
  text is stored ONCE (a continuation's turn is the record's pending text or its retained request, never its prompt),
  and the marker write's over-bound handling touches one field (OQ2). It matches the documented intent. **Visible
  change:** on a loop that took a continuation, `LoopCompletedEvent.Prompt`/`LoopFailedEvent.Prompt` carry the birth
  prompt where today they carry the latest turn's; named in the migration section.
- **(b) latest task** — today's incidental semantics, no visible change on `Prompt`; the deferred turn's text is on
  the record twice until settle, the record's headroom for a turn halves, and the over-bound clear must revert two
  fields (to a previous value the lane no longer has). More lines, less honest bound.
- Pre-selected (a); changes the terminal-event scenario's AND-clause and one migration line.
- **Ruled (a′), 2026-09-26 (owner, transcribed on #1365, answering implementation finding F1).** (a) alone lost a
  deferred turn: `TestTruncationRetryCarriesTheDeferredTurn` (`continuation_deferral_test.go:237`) went red because an
  emptied context re-injected the BIRTH prompt, the retry was named the turn's carrier, and its completion settled.
  (a′) keeps `task_prompt` birth-only and makes `recoverEmptyContext` re-inject the birth prompt and then, when the
  marker is uncarried (marker set, no carrier — the replay's predicate), `pending_continuation_prompt` after it. No
  double store: the turn is read from the one field that holds it.
- **Ruled F3 → (b), 2026-09-26 (owner, transcribed on #1365, issuecomment-5843437754; the finding is
  issuecomment-5843409525).** (a′)'s "uncarried" predicate composed with `SettleRequest` settling the carrier on every
  response status lost a CARRIED turn: R2 carries the turn, R2 comes back `length_truncated`, the settle at the top of
  `HandleModelResponse` cleared marker, carrier and text, and a compaction that emptied the context re-injected only
  the birth prompt. Now a `length_truncated` answer settles only the outstanding mark (`settleTruncatedRequest`) and
  the deferral survives into the compaction retry; `recoverEmptyContext` re-injects the turn whenever the marker is
  set and its text is non-empty, carried or not (`deferredContinuationPrompt`); the retry's `TrackRequest` names it
  the carrier and its answer settles the deferral. The rebuild's replay keeps the "uncarried" predicate: a restart
  inside the truncation window reads a carried marker, and the retained request already holds the turn.
  **Why empty-context recovery cannot inject the turn twice:** it runs only when the context holds no user or
  assistant message (`hasUserOrAssistantMessage`, at both callers `emitRetryRequest` and `publishIterationRequest`),
  and the turn is a user message, so the context it re-injects into cannot already hold the turn. A truncation the
  loop cannot retry fails the loop with the deferral kept — the terminal record keeps the unanswered turn, marker,
  carrier and text, as a loop cancelled or timed out while its carrier is outstanding already does (`SettleRequest`
  is the only clear site); the iteration ceiling (CDT:563) keeps an UNCARRIED marker, a different shape. Owner
  2026-09-26 (#1365, conditional ruling, applied on the re-review's finding): an existing failure shape reached by a
  new path, not a new one. The marker-write size drop stays a documented loss (§ 7.7): a turn whose text the record
  refused is not recovered when a later compaction empties the context.

## 1. The docket

Columns: the fact · the alternative-first rows (doc sentence / smallest code) · the chosen shape · pins.

| # | Fact | Alternative-first rows | Chosen | Pins |
|---|---|---|---|---|
| F1 | **The deferred turn's text** is in the predecessor's context manager and `taskPrompts` only (I: § Fact 1 Admission); the record carries a marker and an empty carrier; the rebuild clears it with a warning (ST:435-442). | (a) doc sentence — "the turn must be re-sent" (D:280-294, M:1873-1883): L4a's accepted sentence, ruled past by #1330 Q2 → #1365. (b) **field**: `PendingContinuationPrompt string \`json:"pending_continuation_prompt,omitempty"\`` beside A:141. | (b). Set in memory where the marker is set (ST:327-333, `attachContinuation` takes the prompt); written by the marker's own compare-and-swap (C:3140-3144 + the text); replayed by the rebuild after the retained conversation (after ST:475) on EVERY uncarried marker; cleared where the marker clears (ST:1301-1303). Adoption is NOT touched (OQ4). Cardinality: OQ1. | I: A:125, A:141, ST:327-333, ST:435-442, C:3140-3144, ST:1301-1303, H:1014-1016, H:1097; § 9 LE:503, LE:507, ST:463, ST:475 |
| F2 | **The task prompt** is `taskPrompts[loopID]`, one writer (H:1016), three readers (H:2529, H:3336, H:3302-3303), the one cache the rebuild does not restore (I: § Fact 2 Mirror). | (a) doc sentence — "a consumer tolerates an empty `Prompt`; recovery uses the placeholder" (D:295-303, M:1885-1892): L4a's sentence, ruled past by Q8. (b) derive it from the retained request — rejected, § 6.2. (c) **field** `TaskPrompt string \`json:"task_prompt,omitempty"\``, and the map goes. | (c), consolidated and birth-only (OQ5 (a)): `CacheTaskPrompt` sets the in-memory entity's field when `!continuation`, `GetTaskPrompt` reads it, `taskPrompts` and its `DeleteLoop` line (ST:940) are deleted. Birth renders it (C:2907 → `marshalLoopRecord` C:3167 renders the in-memory entity); the wholesale seat restores it (ST:444) — zero new lines on either. The readers are unchanged; `recoverEmptyContext`'s literal stays as the empty-field branch (H:3303-3305). | I: ST:90, ST:1062-1065, ST:1069-1072, H:2529, H:3336, H:3302-3303, ST:481-501, UT:324; § 9 ST:444, ST:940, C:2907, C:3167 |
| F3a | **Undecodable envelope / wrong payload type** (C:1473-1482) log and Ack. | (a) doc sentence + smallest code are the same row: "a malformed task is terminated, never acknowledged as done" — the scenario the requirement already states for heartbeat lanes (S:941-946) and the response and tool lanes already implement (C:1978, C:1985, C:2456, C:2463). No record can resume bytes that never decode. | (a): two `return natsclient.TerminateDelivery(fmt.Errorf(…))`. No counter — the sibling lanes count none; `structural-invalid` (C:36) is preflight's reason for a DECODED task. | I: C:1473-1482; § 9 C:1978, C:1985, C:2456, C:2463, C:1494, S:941 |
| F3b | **`HandleTask` failure** (C:1533-1547) logs and Acks; producers H:869 (`ctx.Err()`), H:874 (`WrapInvalid`, depth), H:918-924 (`attachContinuation`: `ErrLoopTerminal` Invalid, `ErrLoopBusy` Transient), H:934/939 (`CreateLoop*`), H:968 (`startTrajectory`), H:980 (`GetLoop`), H:1102 (`buildTaskRequest`). | (a) doc sentence — "a task whose handler fails is acknowledged and lost; re-send" (today; the L1 residual, archived `settle-after-durable-effect` design:238). (b) **class-derived**: `return err` (C:1547) so the lane's policy decides (C:1314-1321: Fatal → Quarantine, `PermanentDeliveryError` → Terminate, else Retry), with `errs.IsInvalid(err)` wrapped as `TerminateDelivery` in the same function, mirroring C:1494 — the heartbeat policy does not read the Invalid class (C:1318-1319), so without the wrap an invalid task would be retried to exhaustion; `ErrLoopBusy` keeps its acknowledged refusal (OQ3). **The failures after a birth registered its loop** (H:968, H:980, H:1102) have no production producer: `startTrajectory` always returns nil (TJ:24-32), `GetLoop` fails only on a release race, `buildTaskRequest`'s marshal/`ResolveSubject` never fail (archived transition-result design § 0 OQ2). Simple over edge-case: no release code; one doc sentence at H:1102 says a Retry there would meet the warm loop's dedup (ST:642-652, C:1577-1583) and be acknowledged. | (b): three lines at the site and one `if errs.IsInvalid`. | I: C:1533-1547, H:896, C:1577-1583, ST:642; § 9 C:1316, C:1318-1319, C:1541-1544, TJ:24, H:1162 |
| F3c | **The resumable intake record.** `pendingTaskResult` is an in-process map for the transient lineage NAK only (I: § Fact 3, C:1757-1778); the record is created before the first publish (C:1699) and a publish failure retries with the loop released (C:1741-1745); the cold fork republishes R1 from the record (LC:102 `taskRepublishFirstRequest`, C:1658-1698). | (a) **nothing new** — the honest sentence: "a task that fails before anything is registered is redelivered into a fresh birth; one that fails after its record exists is redelivered into the cold fork, which republishes the request the record names". (b) promote `pendingTaskResult` to a KV record / a new durable intake record — rejected, § 6.3. | (a): the durable fact a redelivery resumes from is the record already written before the first publication; before it exists there is nothing to resume. `pendingTaskResult` is untouched and its sentence (S:1112-1113) is carried into the MODIFIED block. | I: C:1699-1716, C:1741-1745, C:1757-1778, C:1577-1583; § 9 LC:93, LC:102, LC:111, LC:121, TRIT:408 |
| F4 | **The spec's inconsistency** (S:1076-1077 vs S:1104-1111). | (a) REMOVED S:1104 with its non-exemption scenarios and sentence moved. (b) second MODIFIED. | (a), OQ0. | I: S:1076-1077, S:1104-1111, S:1120; § 9 S:1104, S:1112, S:1115, S:1123 |
| F5 | **Size** (I: § Fact 5): no guard on this bucket; the wire's ceiling is the only bound. | OQ2 (a) observed refusal at the three writes / (b) predicted through `MaxPayload()`. | (a). | I: acquire.go:20, C:109, C:870-874, kv.go:29-40, kv.go:358; § 9 storage.go:138, client.go:214, C:3071, C:3079 |
| F6 | **Sisters** (I: § Fact 6): zero readers of `pending_continuation*` or a prompt key; semsage decodes a narrow struct. | one migration line. | § 5. | I: § Fact 6 table |

Decision skills: `kv-or-stream` — not triggered (no new path; both facts ride the existing `AGENT_LOOPS` record, which
already carries the marker); `entity-or-bucket` — the facts are per-loop operational state on the loop's own record,
not graph triples: `LoopEntity` is not `Graphable` (I: § Fact 1 Graph projection) and the graph-facing
`LoopExecutionEntity` projection is untouched (the evidence embedding is § 8); `orchestration-check` — no multi-step
behaviour added (replay is one `AddMessage` inside the existing rebuild); `new-payload` — none; `query-pattern` —
none.

## 2. The table rows

The MODIFIED requirement's changed and added scenarios, as they appear in `specs/agentic-loop/spec.md` (the delta
keeps all twenty-two existing headings; openspec 1.7.0 refuses a MODIFIED block that omits one). Row keys follow the
archived transition-result design § 2.

| Row | Before (`9e5d8455`) | After | Delta scenario |
|---|---|---|---|
| A1-task | `ctx.Err()` at H:869 → log + **Ack** | **Retry** (joins model/tool) | `A refusal before any mutation is retried; a refusal naming invalid input is terminated` (body changed) |
| A2-task | `WrapInvalid` depth at H:874 → log + **Ack** | **Terminate** | same |
| A5 | `attachContinuation` refusals, `CreateLoop*`, `startTrajectory`, `GetLoop`, `buildTaskRequest` errors → log + **Ack** | `ErrLoopTerminal` → **Terminate**; `ErrLoopBusy` → **Ack**, a defined refusal (OQ3 (a)); the rest → **Retry** (no producer after registration) | `The task lane's results settle on their own owner, and its errors stay exempt` (body changed; the heading's exemption is recorded as ended) |
| — | undecodable / wrong type → log + **Ack** (C:1476, C:1482) | **Terminate** | `A malformed task is terminated, never acknowledged as done` (added) |
| B1 | unchanged (record by create-once, then publish; refused create / failed publish → released, Retry) | unchanged; the redelivery's cold fork is named; the transient-lineage resume (S:1112) is carried in | task-lane scenario, first and last THEN |
| B3 | marker write; text not durable; rebuild clears with a warning | marker + text in one CAS; rebuild replays on every uncarried marker; W-c duplicates, logged | task-lane scenario third THEN; `The deferred continuation's replacement behaviour is owed to #1365` (body changed) |
| — | `Prompt` empty on a rebuilt loop's terminal event; placeholder on recovery | the record's birth prompt | `A rebuilt loop's terminal event carries the prompt its record accepted` (added) |
| — | a task refused before anything is registered → Ack; on retry it would be a fresh birth | Retry/Terminate by class; the retry IS a fresh birth (nothing was registered) | `A task that fails before anything is registered is redelivered into a fresh birth` (added) |
| — | a record write over the payload ceiling: birth → Retry ×2 then dropped, lane parked; marker → best-effort Ack, text kept in memory; carrier → Quarantine | birth → **Terminate**; marker → best-effort Ack, text dropped from memory, logged; carrier → **Quarantine** (unchanged row, now stated) | `A loop record the payload ceiling refuses is not retried` (added) |
| O2 | obligation row | closed by this change | — |
| O3 | obligation row | closed except `ErrLoopBusy`, which keeps Ack as a defined refusal (OQ3) | — |

**Second MODIFIED block (ruling 2026-09-26 on implementation finding F2).** `### Requirement: The loop record names
its outstanding request` (S:1587) stated the L4a limitation as a SHALL. The delta restates it with all 22 scenarios
byte-identical but one; changed: the paragraph at S:1649-1654 now reads "a rebuild SHALL replay the
`pending_continuation_prompt` the record carries after the retained conversation as the user's turn and SHALL keep
the marker; it SHALL clear the marker and warn only when the record carries no text (a record written before this
tag)", and the body of `A rebuilt loop clears a deferred turn whose text it cannot recover` (heading kept) says the
same.

**Counts.** Disposition changes on production paths: 3 rows (decode, wrong type, handler error — with A1/A2/A5 as its
sub-rows). Under OQ2 (b) one added scenario gains a predicted check. No other row of the table moves (ruling 3's
"B3's replacement column changes, nothing else").

## 3. Write, replay and clear points

### 3.1 `pending_continuation_prompt`

| Point | Where | What |
|---|---|---|
| Admit (memory) | `attachContinuation(loopID, taskID, prompt)` — ST:292, inside the `outstanding` branch ST:327-333 | `entity.PendingContinuationPrompt = prompt` beside `PendingContinuation = true` and `PendingContinuationRequestID = ""`. One fact, one site, one lock. The text is `task.Prompt` (UT:324; `Validate` refuses an empty one, UT:422-423), the same string H:1014 appends to the context. Under OQ5 (a) `CacheTaskPrompt` does not run on this branch, so the text is on the entity once. |
| Write (record) | `persistDeferredContinuationMarker` — C:3107, the overlay at C:3140-3144, the CAS `loopsBucket.Update(ctx, loopID, data, revision)` at C:3150 | the overlay writes THREE fields onto the record it read: marker `true`, carrier `""`, prompt `<text>`. Same write, same revision, same lost-CAS → Retry / other → best-effort rows (C:1564-1573). The text travels from the admitted task to this write on the `HandlerResult` (an unexported field set by `deferredContinuationResult`, H:1097 → H:1119), so the lane writes the fields it owns from the value it admitted; it does not re-read the live entity (§ 6.9). On `nats.ErrMaxPayload`: drop the in-memory text, `Warn`, best-effort (OQ2). |
| Render (every carrier write) | `marshalLoopRecord` C:3167 renders the in-memory entity | no change: after `TrackRequest` names a carrier (ST:1239-1240) the record says carried and the text is inert until settle. A gate write (awaiting_approval, no request minted) renders the uncarried marker and its text unchanged — which is why the entity must hold the text and a carrier write cannot be asked to preserve a field it does not own. |
| Adopt | `adoptNewerRetainedRequest` — LE:487-507 | **not touched** (OQ4). Adoption moves the record's name to the newest retained request and leaves the marker as it found it; the rebuild that follows cannot tell whether that request was minted before or after the turn. |
| Replay | `restoreLoopFromRequest` — after the conversation loop ST:463-474 and `RepairToolPairs` ST:475, before the caches ST:481 | `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" && entity.PendingContinuationPrompt != "" { cm.AddMessage(RegionRecentHistory, {user, text}); Info naming the loop and request.RequestID }`. On EVERY uncarried marker, whichever request is retained. The marker is KEPT: `HasPendingContinuation` (ST:633-637) then reads true on the rebuilt loop, the next completion advances instead of settling (H:1564, H:2764) with a context that holds the turn, `TrackRequest` names the carrier, `SettleRequest` clears. The existing clear-with-warning (ST:435-442) narrows to the text-less case — a record that claims a turn it does not carry — and stays where it is (before the seat). |
| Clear | `SettleRequest` ST:1301-1303 | `entity.PendingContinuationPrompt = ""` beside the two clears. Not on a `length_truncated` answer (F3 (b), OQ5): that status settles only the outstanding mark, and the compaction retry becomes the carrier. `DeleteLoop` (ST:927) drops the entity. A terminal record may carry an uncarried turn's text (CDT:563 already keeps the marker there): the honest record of an accepted, unanswered turn; the bucket's 24 h TTL (acquire.go:20) is its retention. |

**The five windows a replacement can fall in** (the test plan's five named examples, § 4.1). "Record" is what the
marker write left; "retained newest" is what adoption (LE:487-503) moves the record's name to before the rebuild.

| Window | Record after the marker write | Retained newest | Rebuild does | Next request carries the turn |
|---|---|---|---|---|
| W-a before the marker write landed | no marker | R | nothing (today's sentence: the turn was in memory only; the marker write's own Retry/best-effort rows) | 0 — the honest loss, unchanged |
| W-b marker + text landed; names R, the request the turn deferred behind | marker, "", text; names R | R | replays the text after R's conversation, keeps the marker | 1 (the completion of R advances) |
| W-c carrier R(N+1) minted after the turn, PubAck'd, record not updated (W4) | marker, "", text; names R | R(N+1) (carries the turn) | adoption names R(N+1); replays the text after R(N+1)'s conversation, which already holds it | **2, logged — the accepted duplicate (OQ4 (a)); 1 under OQ4 (b)** |
| W-d carrier's record write landed | marker, R(N+1), text | R(N+1) | leaves it (as today) | 1 (inside R(N+1)); settles on R(N+1)'s response |
| W-e the turn deferred behind R(N) while R(N) was tracked but not yet on the record (C:3091-3095, C:3113 before C:3047); crash after R(N)'s PubAck, before its record write | marker, "", text; names R(N-1) | R(N) (built before the turn; does NOT carry it) | adoption names R(N); replays the text after R(N)'s conversation | 1 — and the first draft's adoption line would have made it **0, silently** |

W-c and W-e leave the same record and differ only in whether the retained request was minted before or after the
turn — a fact the record does not hold. (a) errs to the duplicate; (b) would hold the fact (OQ4).

### 3.2 `task_prompt`

| Point | Where | What |
|---|---|---|
| Set (memory) | `CacheTaskPrompt` ST:1062-1065, called from H:1016 | writes `entity.TaskPrompt` on the in-memory entity instead of `m.taskPrompts[loopID]`; under OQ5 (a) the call at H:1016 runs only when `!continuation` (the birth), so the field is written once. Under OQ5 (b) it runs as today, on every delivery. |
| Read | `GetTaskPrompt` ST:1069-1072 | reads the field. Callers H:2529, H:3336, H:3302 unchanged. H:3303-3305's literal stays as the empty-field branch. |
| Recover (OQ5 (a′), F3 (b)) | `recoverEmptyContext` — `processor/agentic-loop/handlers.go:3342` — `if pending := h.loopManager.uncarriedContinuationPrompt(loopID); pending != "" {`; `processor/agentic-loop/state.go:1366` — `func (m *LoopManager) uncarriedContinuationPrompt(loopID string) string {` (pins at `31775e25`, before F3) | after the synthetic "Original task" message (the birth prompt), the deferred turn as a user message. Under F3 (b) the predicate is "marker set and text non-empty", carried or not (`deferredContinuationPrompt` replaces `uncarriedContinuationPrompt`, whose only caller this was). Its callers are `emitRetryRequest` (truncation retry) and `publishIterationRequest` (advance); no other reader synthesises context. |
| Write (record) | birth: `createLoopState` C:2907 → `marshalLoopRecord` C:3167 (renders the in-memory entity); every later write renders the same value | zero new lines. The deferred lane's marker write does NOT overlay it (§ 6.7). |
| Restore | the wholesale seat `m.loops[record.ID] = &entity` ST:444 | zero lines. The cold R1 arm (`taskRepublishFirstRequest`) runs the ordinary `HandleTask` and caches the redelivered task's prompt, as today (D:300-301). |
| Delete | `DeleteLoop` ST:927 | the `delete(m.taskPrompts, loopID)` line (ST:940) and the map (ST:90) go. |

### 3.3 Task intake (`handleTaskMessage` C:1472)

| Site | Today | After |
|---|---|---|
| C:1473-1476 decode | `Error` + `return nil` | `Error` + `return natsclient.TerminateDelivery(fmt.Errorf("decode task BaseMessage: %w", err))` (C:1978's shape) |
| C:1479-1482 payload type | same | `TerminateDelivery(fmt.Errorf("task payload is %T, not *agentic.TaskMessage", …))` (C:1985's shape) |
| C:1533-1547 `HandleTask` error | `ErrLoopBusy` → `Warn` + `return nil`; else `Error` + `return nil` | `ErrLoopBusy` → unchanged (OQ3 (a)), its comment names it a defined refusal and why Retry is rejected (C:1440-1442); `errs.IsInvalid(err)` → `Error` + `return natsclient.TerminateDelivery(err)` (C:1494's shape); else `Error` + `return err` |
| H:968 / H:980 / H:1102 a birth failure after registration | the loop stays in `m.loops` | **no code** (no producer): one doc sentence at H:1102 — "a failure here leaves the loop registered; a Retry meets `HasActiveLoopForTask` and is acknowledged as a duplicate; no production path fails here (`startTrajectory` TJ:24-32, `GetLoop` only on a release race, `buildTaskRequest` never)" — § 7.3 |
| C:1699-1716 birth record write | any error → `WrapTransient` → Retry | `errors.Is(err, nats.ErrMaxPayload)` → release + `TerminateDelivery` (OQ2 (a)); the loop-execution entity, born by `WriteSpawnIdentity` before this write, is stamped failed with reason `record_exceeds_payload_ceiling` and the refusal counts on `task_intake_rejections_total{lane="birth"}` (PR #1387 review MEDIUM 1; not through the terminal owner — COMPLETE_, the event and the record all carry the refused prompt); every other error as today |
| C:3150 marker write | any non-conflict error → best-effort | `errors.Is(err, nats.ErrMaxPayload)` → drop the in-memory text, `Warn`, then the same best-effort return (OQ2 (a)) |

The policy that reads these is unchanged: C:1314-1321.

## 4. Counterexample plan (tests, not code)

Every counterexample is today's behaviour asserted by an existing test or reproducible against `9e5d8455`; mutation
evidence per site is `cp` backup + `md5 -q` before/after, deleting the CALL, not the primitive.

| # | Counterexample on a production path | Harness | Extends | Mutation evidence |
|---|---|---|---|---|
| 4.1 | **Deferred intake → durable acceptance → replacement → cold response → the next request carries the turn once (twice only in W-c, never zero).** Today: `TestARebuiltLoopDoesNotReAskForATurnItCannotRecover` (LRT:429) asserts the marker is cleared, no request is minted and `Prompt` is empty (LRT:474). | unit, `restoreLoopFromRequest` direct + `HandleModelResponse` (LRT:429's shape); W-c/W-e drive `adoptNewerRetainedRequest` first through a fake bucket and a retained-request seam as `loop_carrier_test.go`'s adoption tests do | a new `TestARebuiltLoopCarriesTheTurnItsRecordAccepted` beside LRT:429, **five** subtests = § 3.1's windows, counting the turn's occurrences in the next minted request: W-a 0; W-b 1 and the marker names the minted request; W-c 2 and the replay log line names the loop and R(N+1) (under OQ4 (b): 1); W-d 1; W-e 1 — this subtest is the one that pins the blocking finding: a rebuild over an adopted R(N) built before the turn must still replay it. W-b also asserts `CompletionState.Prompt == record.TaskPrompt` after the loop settles. LRT:429 keeps its text-less-marker case (the clear-with-warning branch). `// spec: agentic-loop / Loop input classes settle after owner-specific durable done`. | delete the replay `AddMessage` → W-b, W-c, W-e red (0); re-add the withdrawn adoption line → W-e red (0, silent); delete the `SettleRequest` clear → the settle subtest red (marker survives its carrier's response) |
| 4.2 | **The same through the production intake and record write** (integration): `TestADeferredContinuationWritesOnlyTheMarkerItOwns` (DCRIT:178) asserts the record after `deferContinuation` carries the marker and an empty carrier. | integration, `startLoopProcess` + `deferContinuation` + a replacement `startLoopProcess` (DCRIT:178's harness) | DCRIT:178: its record assertions gain `pending_continuation_prompt == <the turn>`; its "crash in the interval" arm (DCRIT:211) gains: the replacement's next minted request on `agent.request.<loopID>` contains the turn exactly once (that arm is W-b: R2's PubAck never lands). | delete the overlay's text line at C:3140-3144 → the record assertion red |
| 4.3 | **A rebuilt loop's completion/failure event carries the task prompt.** Today LRT:474 asserts it empty; D:295-303 documents it. | unit, LRT:429's shape | 4.1's W-b subtest (`CompletionState.Prompt`); a sibling asserts `recoverEmptyContext` on a rebuilt loop with an emptied context injects the record's prompt and not the literal; under OQ5 (a) a third asserts a continued loop's completion still carries the BIRTH prompt. | delete the `CacheTaskPrompt` → field write → the birth-record assertion red; the restore is the wholesale seat, so the mutation that would break it (`entity := record`) breaks every rebuild test |
| 4.4 | **A redelivered accepted task resumes rather than dedups.** Today: decode, wrong type and every `HandleTask` error are Ack (C:1476, C:1482, C:1547). | unit, through the production heartbeat callback like TRT:120 `TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition` | a new `TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition`: undecodable bytes → Terminate; wrong type → Terminate; depth → Terminate; `ctx` cancelled → Retry and `HasActiveLoopForTask` false (nothing registered); `ErrLoopBusy` → Ack; `ErrLoopTerminal` → Terminate. No injected post-registration fault (no producer, F3b). | replace one `TerminateDelivery` with `return nil` → its row red; replace the `errs.IsInvalid` branch with `return err` → the depth row red (Retry) |
| 4.5 | **Over the ceiling** (OQ2 (a)). | unit with a fake `jetstream.KeyValue` returning `nats.ErrMaxPayload` (the loop-carrier tests' fake, `loop_carrier_test.go:352`'s shape) | three rows: birth `Create` refused → Terminate, loop released; marker `Update` refused → Ack, the in-memory entity's `PendingContinuationPrompt` empty afterwards, the `Warn` line names the size, and a following carrier write on the same loop is NOT refused; carrier `Update` refused → the existing Quarantine row (unchanged; asserted so the sentence in the delta is proved, not read). | replace the birth `errors.Is` branch → Retry; delete the marker write's clear → the following carrier write red (refused again) |
| 4.6 | **Successful controls, unchanged**: CDT:115, CDT:336, CDT:563, TRIT:78, TRIT:408, TRIT:718, AFPT:123. | — | run by name, `-race -count=1`; they must stay green with no edits beyond an `attachContinuation` call-site argument. | — |
| 4.7 | **Final validation, `task e2e:agentic`**: `verifyMidFlightLoopAcrossReplacement` (SA:898) pauses the model-request consumer (SA:909), publishes a task, replaces the process, resumes, and expects `agent.complete` (SA:1010). | e2e tier | after R1 is retained and before the replacement, publish a continuation task naming the loop (deferred behind R1 — W-b, the only window the tier reaches, § 10); after the resume, the completion of R1 must advance, R2 must contain the turn exactly once, and `agent.complete` must carry `prompt`. The tier is the proposal's final validation of the intake disposition; it is not the iteration loop. | — |

**PBT decision** (`docs/contributing/01-testing.md` § When to Use Property-Based Testing): the exactly-once obligation
is a property over the five replacement windows of § 3.1, a finite set, so five named examples driving the production
rebuild (4.1) enumerate it and are stronger than a sampled property. `TestPropAppliedFactsHoldAcrossEveryCrashWindow`
(AFPT:123) is NOT extended: its model retains requests and an applied set and has no conversation (AFPT:67-69, "the
tool batch a response carries belongs to the cold rebuild, which no action here drives"); a deferred-turn action would
need a second in-memory conversation model, which is the reconstruction the testing policy forbids. If the owner wants
the property, it is a follow-up on the model, not on this change.

**Invariants and their spec homes** (contract § Design discipline):

| Invariant (every input, every window) | Spec home |
|---|---|
| I-A While a record's marker is uncarried, `pending_continuation_prompt` holds the (latest, OQ1) uncarried turn's text. | delta: requirement paragraph "from the write that sets its marker to the write that clears it"; the deferred-turn scenario |
| I-B A rebuilt loop's next minted request carries an uncarried deferred turn at least once and at most twice; twice only when the retained request was minted after the turn and the record was not updated before the replacement (W-c), and then it is logged. Never zero. | delta: requirement paragraph; the deferred-turn scenario's third AND |
| I-C `task_prompt` on a record is the prompt of the task that bore the loop, from the birth write on, never rewritten (OQ5 (a)). | delta: requirement paragraph; the terminal-event scenario |
| I-D No task delivery is positively acknowledged on a failure that left nothing durable behind it, except the defined refusals, each named: a duplicate, an applied task, an unheld continuation, a continuation of a loop with work in flight. | delta: requirement paragraph; the task-lane scenario's last THEN; the malformed-task and fresh-birth scenarios |

## 5. Migration (one section; `docs/operations/migration-beta162-to-beta163.md`, after the #1374 section)

`## Durable accepted input, and task intake settles like every other lane (#1365, #1345)`:

- **Record keys added** (`agentic.LoopEntity`, additive, `omitempty`, `Validate()` does not require them — the shape of
  the L4a section at M:1827-1832): `task_prompt` (the prompt of the task that bore the loop, written once by the birth
  write; OQ5 (a)), `pending_continuation_prompt` (the deferred turn's text while `pending_continuation` is true and
  `pending_continuation_request_id` is empty; cleared with them). A record written before this tag decodes with both
  empty and behaves as the M:1873-1892 paragraphs describe; those two paragraphs are marked superseded by this section
  rather than deleted (the tag range is the same).
- **What a consumer sees**: `LoopCompletedEvent.prompt` / `LoopFailedEvent.prompt` are populated after a process
  replacement (they were empty; a consumer that tolerated empty keeps working) and, on a loop that took a
  continuation, carry the BIRTH prompt where they carried the latest turn's (OQ5 (a); under (b) this line goes), and `recoverEmptyContext` on a continued loop re-injects the birth prompt as its "Original task" and then the uncarried deferred turn, where it re-injected the latest turn alone (H:3313-3315; OQ5 (a′)); a loop
  record now carries prompt text, so a listing that decodes the whole record pulls it (semsage's
  `processor/ui-api/types.go:14-27` decodes a narrow struct and is unaffected — I: § Fact 6); the terminal trajectory
  evidence embeds the whole record and therefore carries both keys (§ 8).
- **Intake dispositions on `agent.task`** (observable only through the consumer's redelivery behaviour and metrics;
  no wire or subject change): undecodable or wrong-type → Terminate (was Ack); an over-depth task or a continuation
  of a settled loop → Terminate (was Ack); a transient handler failure → Retry, on the configured policy (default one
  redelivery after 30 s, `max_deliver` 2; the lane runs at MaxAckPending 1, so a Retry parks intake for that budget —
  no production path produces one today); a continuation refused because its loop has work in flight → Ack, as
  before (OQ3 (a)). `tasks_submitted_total` is unchanged (at-least-once, M:1942).
- **Size**: the WHOLE record shares the server's `max_payload` (1 MiB default) — every field summed, the prompt and
  a deferred turn's text included; a birth write the client refuses for size terminates the task (was: retried to
  the redelivery budget with the lane parked); a marker write it refuses is logged and the turn stays in process
  memory, not durable; a carrier write it refuses quarantines the delivery, as any other carrier write failure
  (unchanged).
- **A replacement may replay a deferred turn twice** in one window (a carrier minted after the turn, PubAck'd, record
  not yet updated — OQ4 (a)); the replay is logged. Never zero, where before this tag it was zero with a warning.
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
| 6.2 | Derive the task prompt from the retained request instead of a field. | The request's messages are unlabelled `user`-role entries — the task's turn, `recoverEmptyContext`'s synthetic one (H:3312-3315), a product-injected one are indistinguishable; the conversation is GC/eviction-subject (recovery exists because it can be EMPTY, H:3301); and the `agent.task` stream is not addressable by task id. A linkage resting on position or content is the naming-coincidence tell (contract § Design discipline). |
| 6.3 | A durable resumable-intake record (promote `pendingTaskResult`, or a new KV key). | No durable effect precedes the failures that still acknowledge, so there is nothing for a record to resume; once the record exists it IS the resumable fact (LC:102). `pendingTaskResult` bridges one transient NAK in-process and needs no promotion. A second durable home would be a same-class collision with the record. |
| 6.4 | `[]string` for the deferred turn. | OQ1 (b): beyond the marker's shape. |
| 6.5 | A fixed byte cap at preflight, or a predicted check through `MaxPayload()`. | OQ2 (b): predicting a value the framework observes, one more seam for the same clear-and-log. |
| 6.6 | Quarantine for the task lane's post-mutation errors, applying S:907-909 literally. | The only producers after H:1014's append are `buildTaskRequest`'s (marshal / `ResolveSubject`, no production trigger — archived transition-result design § 0 OQ2). A lane latch for an unreachable path. Recorded as § 7.3. |
| 6.7 | Overlay `task_id`/`task_prompt` at the marker write too. | The marker write owns the marker (DCRIT:178's invariant: "a lane writes only the fields it OWNS"); moving `task_id` there changes a classification input (`taskContinuationUnheld` refuses on it, LC:121) on a write that is not the carrier's. The carrier's next write moves `task_id`, as today; `task_prompt` is birth-only (OQ5). § 7.2 records the window. |
| 6.8 | Count decode/type failures on `task_intake_rejections_total`. | The response and tool lanes count none (C:1978, C:2456); `structural-invalid` is preflight's. A new label value for parity with nothing. |
| 6.9 | The marker write reads the LIVE carrier instead of writing `""`. | Reintroduces the hole L4a closed: `TrackRequest` names a carrier at mint, before PubAck (A:134-140); a record naming a carrier nothing retains leaves the turn unrecoverable. `""` is the honest claim at admission. |
| 6.10 | Replay the turn inside `HandleModelResponse` (at carry time) instead of at rebuild. | Two homes for "seat the record's facts into memory"; the rebuild already restores five caches at one point (ST:481-501) and the problem shape (I: § Problem shape) is exactly that mirror. |
| 6.11 | **Adoption names the newest retained request as the turn's carrier** (the first draft's line at LE:507). | Withdrawn on the reviewer's probe: in W-e the adopted request was minted BEFORE the turn (C:3091-3095, C:3113/C:3047), so naming it carrier skips the replay and lets its completion settle the marker — silent loss where today there is a warning. The record cannot tell W-e from W-c; OQ4. |
| 6.12 | **`HandleTask` releases the loop it registered when a birth fails** (the first draft's F3b code and its injected-fault test row). | Guards paths with no production producer: `startTrajectory` always returns nil (TJ:24-32), `GetLoop` fails only on a release race, `buildTaskRequest` never. Simple over edge-case: one doc sentence at H:1102 (§ 7.3). |
| 6.13 | Re-write the marker without its text after an over-bound refusal, so the marker still lands. | A second write for a case whose honest sentence already exists (the marker write is best-effort; today's W-a state). The clear-and-log is the whole code. |
| 6.14 | Blank `task_prompt`/`pending_continuation_prompt` in the terminal trajectory evidence. | § 8: evidence is a first-class capability; the record is the evidence. |

## 7. Residuals (doc comments in the code, not issues — owner 2026-09-02)

1. **The marker write landing after the carrier's own record write.** If, between `attachContinuation` and
   `persistDeferredContinuationMarker` (C:1533 → C:1564), the response lane received R's answer, minted R(N+1)
   carrying the turn, got its PubAck and wrote the record, the marker write then overlays "" onto a record whose
   carrier was R(N+1); a replacement in that state is W-c (a logged duplicate, never a loss). Two NATS round trips
   inside the task lane's own delivery. **Unproven, § 10:** the "already carried" premise assumes the turn was appended
   to the context (H:1014) before that advance read `cm.GetContext()` — the lanes are not serialized per loop
   (H:1862-1866), so the advance can also read the context BEFORE the append, in which case R(N+1) does not carry the
   turn and the state is W-e (a correct single replay). Either way not a loss. Recorded at C:3140-3144, not coded.
2. **A replacement between the marker write and the next carrier write** rebuilds a loop whose `task_id` is the
   previous task's (the marker write does not move it, § 6.7; already so in L4a, D:290-292). `task_prompt` is unaffected
   under OQ5 (a). Recorded at ST:444.
3. **A `HandleTask` failure after `attachContinuation` rebound `TaskID`** (ST:318) and before the record is touched — no
   production producer (TJ:24-32; `GetLoop` H:980 only on a release race; `buildTaskRequest` H:1102 never). What a Retry
   would do differs by site: `startTrajectory` (H:968) and `GetLoop` (H:980) fail BEFORE the append at H:1014, so the
   redelivery meets `HasActiveLoopForTask` on the rebound `TaskID` (ST:642-652), is acknowledged as a duplicate
   (C:1577-1583), and the turn is nowhere — not in the context, not on the record; `buildTaskRequest` (H:1102) fails
   AFTER the append, so the turn is in the live context only and the same duplicate acknowledgement follows. A birth
   (`!continuation`) failing at the same sites leaves a registered loop with no record and is acknowledged the same way
   on retry. One doc sentence at H:1102 says all of this; no release code (§ 6.12).
4. **Inert text on a carried record**: `pending_continuation_prompt` stays until settle after the carrier is named;
   a terminal record at the iteration ceiling keeps an uncarried turn's text (CDT:563). Retention is the bucket's TTL.
5. `Validate()` (A:164) checks neither field; a fixture that writes `AGENT_LOOPS` directly may set a prompt with no
   marker (harmless) or a marker with no prompt (the clear-with-warning branch). graphresearch's `CreateLoopEntity`
   (`frameworkcapabilities/graphresearch/register_tool.go:91`, the second bare-key writer TS:195-203 names) writes a
   record with both fields empty — the pre-change shape, harmless.
6. **The W-c duplicate** (OQ4 (a)): logged at the replay and at adoption; never counted. A counter would be an owner
   question, as the L4a clear's was.
7. **The over-bound marker write** drops the text from memory and does not land the marker: a replacement then
   rebuilds without knowing a turn was deferred (today's W-a). The `Warn` names the loop and the size. With the text
   gone, a later compaction that empties the context cannot re-inject the turn either (F3 (b) re-injects from the
   text): the recovery carries the birth prompt alone. The ceiling, documented in the delta and the migration section.
8. **A failed carrier's turn: its fate depends on the status** (re-review at `783a18b7`; it follows from ruling (b)'s
   text, not a developer choice). A carrier that fails `length_truncated` keeps its deferral — marker, carrier and
   text — on the terminal record; a carrier that fails `model_error`, or the timeout and max-iterations early returns
   under any other status, clears it through `SettleRequest`. Recorded here, not coded.

## 8. Adopter seam (contract § The adopter seam inventory)

Surfaces reached from outside: (i) two JSON keys on `AGENT_LOOPS` records; (ii) `prompt` on the completion/failure
events after a replacement, and on a continued loop (OQ5); (iii) the `agent.task` delivery disposition; (iv) the Go
surface — `agentic.LoopEntity` gains two exported fields (Tier 1, additive), `processor/agentic-loop` changes no
exported symbol (`attachContinuation` is unexported; `CacheTaskPrompt`/`GetTaskPrompt` keep their signatures); (v)
**the terminal trajectory evidence**: `trajectoryTerminalEvidence` embeds the whole `agentic.LoopEntity` (TW:24-29;
populated from `GetLoop` at TW:98-100), so agent execution evidence now carries `task_prompt` and, on a loop that
ended with an uncarried turn, `pending_continuation_prompt`, beside `Completion.Prompt`.

1. **What must they know?** A record reader: nothing — unknown keys are ignored by every sister decoder found (I:
   § Fact 6). An event consumer: `prompt` is now populated where it was empty; one that special-cased empty keeps
   working; on a continued loop it is the birth prompt (OQ5 (a)). An `agent.task` producer: a malformed task is no
   longer acknowledged (the same as every other lane). A fixture that writes records directly: the two keys' meaning
   (§ 7.5). An evidence reader: the record inside the evidence now carries the prompt (duplicating
   `Completion.Prompt` in bytes) and, when present, an accepted turn that was never answered. One item per surface,
   none load-bearing for correctness.
2. **If they do nothing?** Same observations as today, plus a populated `prompt` and a larger evidence object. The
   intake change is invisible to a fire-and-forget producer except as a redelivery or a JetStream advisory.
3. **Where do they find out?** The record itself (typed JSON, runtime) and the event; the migration section for the
   disposition — "doc" for a non-correctness fact, which is acceptable.
4. **What SHOULD they know?** Nothing. The gap between 1 and 4 is empty for readers; for producers the one honest new
   fact (a malformed task is terminated) is the framework observing its own outcome rather than the producer
   predicting anything.

**The evidence row, decided (medium 8).** Cheaper row first: **(a) leave the embedding as it is** — zero code; agent
execution evidence is a first-class capability (`openspec/project.md` § Purpose: "every step an agent takes is
recorded … an agentic harness you cannot audit is the black hole"), the record IS the evidence of the loop's state at
its terminal, and an uncarried turn's text on a terminal record is exactly the auditable fact "this turn was accepted
and never answered" (the `continuation_unavailable` class, D:329). The prompt duplicates `Completion.Prompt` by a few
bytes. **(b) blank the two fields in the evidence copy** — code, plus a rule for why evidence hides an accepted input,
which runs against the purpose statement. Chosen (a).

**Prefer observation to prediction**: the size bound is the one place a knob could have appeared; OQ2 (a) deletes it
by observing the client's refusal. No adopter is asked to compute a size, a subject, a bucket or a deadline.

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
- `processor/agentic-loop/trajectory_handler_wiring.go:24` — `type trajectoryTerminalEvidence struct {`
- `processor/agentic-loop/trajectory_handler_wiring.go:25` — `Loop       agentic.LoopEntity          `json:"loop"``
- `processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:71` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/trajectory_handler_wiring.go:99` — `terminal := trajectoryTerminalEvidence{`
- `processor/agentic-loop/trajectory.go:24` — `func (m *trajectoryManager) startTrajectory(loopID string) (agentic.Trajectory, error) {`
- `processor/agentic-loop/handlers.go:1162` — `func (h *MessageHandler) buildTaskRequest(loopID string, task TaskMessage, entity agentic.LoopEntity, messages []agentic.ChatMessage, tools []agentic.ToolDefinition) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:1862` — `// serialized — agent.task, agent.response and tool.result are separate`
- `processor/agentic-loop/handlers.go:1863` — `// consumers and nothing in this package serializes them per loop (design.md`
- `processor/agentic-loop/component.go:35` — `taskIntakeRejectionLane   = "decoded-task"`
- `processor/agentic-loop/component.go:36` — `taskIntakeRejectionReason = "structural-invalid"`
- `processor/agentic-loop/component.go:1193` — `settleRetry, retryErr := natsclient.DelayedDeliveryRetry(30 * time.Second)`
- `processor/agentic-loop/component.go:1261` — `if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {`
- `processor/agentic-loop/component.go:1262` — `fixed = 1`
- `processor/agentic-loop/component.go:1316` — `return natsclient.DeliveryDecisionQuarantine, handlerErr`
- `processor/agentic-loop/component.go:1318` — `var permanent *natsclient.PermanentDeliveryError`
- `processor/agentic-loop/component.go:1319` — `if errors.As(handlerErr, &permanent) {`
- `processor/agentic-loop/component.go:1440` — `// The other two settlements were rejected on this lane: Retry parks the whole`
- `processor/agentic-loop/component.go:1441` — `// task lane, which runs at MaxAckPending 1, for MaxDeliver attempts on a`
- `processor/agentic-loop/component.go:1494` — `return natsclient.TerminateDelivery(err)`
- `processor/agentic-loop/component.go:1541` — `if errors.Is(err, ErrLoopBusy) {`
- `processor/agentic-loop/component.go:1542` — `c.logger.Warn("Task refused — the loop it names still has work in flight",`
- `processor/agentic-loop/component.go:1564` — `if err := c.persistDeferredContinuationMarker(ctx, result.LoopID); errors.Is(err, natsclient.ErrKVRevisionMismatch) {`
- `processor/agentic-loop/component.go:1978` — `return nil, "", natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:1985` — `return nil, "", natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2456` — `return natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2463` — `return natsclient.TerminateDelivery(`
- `processor/agentic-loop/component.go:2907` — `func (c *Component) createLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:3047` — `c.loopRecordMu.Lock()`
- `processor/agentic-loop/component.go:3071` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/component.go:3079` — `return fmt.Errorf("persist loop state %s: %w", loopID, err)`
- `processor/agentic-loop/component.go:3091` — `// time a continuation can defer, the request it is deferring behind has been`
- `processor/agentic-loop/component.go:3107` — `func (c *Component) persistDeferredContinuationMarker(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:3113` — `defer c.loopRecordMu.Unlock()`
- `processor/agentic-loop/component.go:3150` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/component.go:3167` — `func (c *Component) marshalLoopRecord(loopID string) ([]byte, error) {`
- `processor/agentic-loop/loop_classification.go:93` — `taskBirth taskDisposition = iota`
- `processor/agentic-loop/loop_classification.go:102` — `taskRepublishFirstRequest`
- `processor/agentic-loop/loop_classification.go:111` — `taskApplied`
- `processor/agentic-loop/loop_classification.go:121` — `taskContinuationUnheld`
- `processor/agentic-loop/config.go:170` — `MaxDeliver        int    `json:"max_deliver,omitempty" schema:"type:int,description:Maximum redelivery attempts for long-running consumers. Must cover the fixed two-entry BackOff,default:2,min:2,max:10,category:advanced"``
- `processor/agentic-loop/approval_response_handler.go:330` — `const continuationUnavailableReason = "continuation_unavailable"`
- `processor/agentic-dispatch/terminal_settlement.go:199` — `//`
- `frameworkcapabilities/graphresearch/register_tool.go:91` — `func (w *natsResearchKVWriter) CreateLoopEntity(ctx context.Context, loopID string, value []byte) error {`
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
- `openspec/specs/agentic-loop/spec.md:1112` — `The birth-failure and transient-lineage paths of the same lane are NOT exempt — they settle on their durable effect`
- `openspec/specs/agentic-loop/spec.md:1115` — `#### Scenario: A task delivery fails after its loop exists`
- `openspec/specs/agentic-loop/spec.md:1123` — `#### Scenario: A tool result is cancelled after the loop has advanced`
- `docs/operations/migration-beta162-to-beta163.md:1827` — `### One field is added to `agentic.LoopEntity``
- `docs/operations/migration-beta162-to-beta163.md:1873` — `**A deferred turn is durable as a MARKER only, and so is nothing about the task prompt.** A continuation admitted`
- `docs/operations/migration-beta162-to-beta163.md:1885` — `The loop's task prompt is the same limitation one field over. A loop rebuilt from its record and a retained request —`
- `processor/agentic-loop/loop_rebuild_test.go:429` — `func TestARebuiltLoopDoesNotReAskForATurnItCannotRecover(t *testing.T) {`
- `processor/agentic-loop/loop_rebuild_test.go:474` — `require.Empty(t, completion.CompletionState.Prompt,`
- `processor/agentic-loop/deferred_continuation_record_integration_test.go:178` — `func TestADeferredContinuationWritesOnlyTheMarkerItOwns(t *testing.T) {`
- `processor/agentic-loop/deferred_continuation_record_integration_test.go:211` — `t.Run("a crash in the interval leaves a record the batch can be replayed against", func(t *testing.T) {`
- `processor/agentic-loop/task_redelivery_integration_test.go:408` — `func TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry(t *testing.T) {`
- `processor/agentic-loop/task_redelivery_integration_test.go:718` — `func TestAColdContinuationForALoopNoProcessHoldsIsRefused(t *testing.T) {`
- `processor/agentic-loop/continuation_deferral_test.go:115` — `func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {`
- `processor/agentic-loop/continuation_deferral_test.go:336` — `func TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns(t *testing.T) {`
- `processor/agentic-loop/continuation_deferral_test.go:563` — `func TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord(t *testing.T) {`
- `processor/agentic-loop/applied_facts_property_test.go:123` — `func TestPropAppliedFactsHoldAcrossEveryCrashWindow(t *testing.T) {`
- `processor/agentic-loop/transition_result_test.go:120` — `func TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition(t *testing.T) {`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:898` — `func (s *Scenario) verifyMidFlightLoopAcrossReplacement(`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:909` — `if _, err := agentStream.PauseConsumer(ctx, modelRequestConsumerName, time.Now().Add(2*time.Minute)); err != nil {`

Inventory corrections (recorded here, the inventory file is not re-pinned): the `C:3105` pin in the first draft was
a comment line — the function starts at C:3107; the inventory's writer census for `AGENT_LOOPS` omits graphresearch's
`CreateLoopEntity` (`frameworkcapabilities/graphresearch/register_tool.go:91`, named as the second bare-key writer at
TS:199-203) — it writes a `NewLoopEntity` with both new fields empty, harmless.

Searches run for this design (beyond the inventory's): `git grep -n "spec: agentic-loop / <heading>" -- '*_test.go'`
for each heading the delta rewrites or removes → 0 for every scenario heading, 26 for the S:886 requirement heading
(unchanged); `git grep -n "max_payload" -- '*.conf' '*.yml' '*.yaml' '*.json' 'test/*'` → 0; `git grep -n
"ErrMaxPayload\|MaxPayload" -- '*.go'` → 10 (the two pinned); `grep -n "PendingContinuation"
processor/agentic-loop/component.go` → 4 (all inside the marker write; the cold arms do not touch the marker);
`grep -rn "deferred\|continuation\|prompt" test/e2e/scenarios/agentic/{process_replacement,stage_a_process_replacement}.go`
→ 0 (the tier does not cover the turn today); nats.go `v1.52.0` (the `go.mod` pin; an earlier draft cited v1.53.1's lines)
`jetstream/kv.go:1059,1089` (`Create`/`Update` → `updateRevision` → `js.PublishMsg` at `:1117`) and `nats.go:4461-4463`
(`ErrMaxPayload`); `openspec` 1.7.0 `dist/core/specs-apply.js:287` (a
MODIFIED block must restate every current scenario by name).

## 10. Unproven, NOT RUN

- **Recovery ordering on a rebuilt tool batch.** OQ5 (a′)'s re-injection and the rebuild's replay are both proved at
  unit level (`TestARebuiltLoopCarriesTheTurnItsRecordAccepted`, `TestTruncationRetryCarriesTheDeferredTurn`,
  `TestTheCarriersTruncationRetryCarriesTheDeferredTurn`). On a cold tool-result rebuild the replayed turn precedes
  the restored batch's assistant turn (replay runs before `restoreToolBatch`), and that IS the order it was typed in:
  a turn defers only while a request is outstanding (`attachContinuation` refuses with `ErrLoopBusy` while tools are
  in flight), so the live context was already [conversation, user(turn), assistant(tool_calls), tool…], and the
  rebuild (the retained conversation, then the replay at `state.go:503-521`, then `restoreToolBatch`'s assistant message at `:609`,
  both at `31775e25`) reproduces it. Not a loss and not a reorder. Not measured against a model.

- **The attach-vs-append race.** § 7.1's "R(N+1) already carries the turn" assumes the advance's `cm.GetContext()` ran
  after H:1014's append; the lanes are not serialized per loop (H:1862-1866: "nothing in this package serializes them
  per loop … there is no per-loop mutex"), so the opposite order is possible and turns W-c into W-e. Not measured;
  either order is covered by "replay on every uncarried marker" (a duplicate or a single replay, never zero).
- **`task e2e:agentic` reaches only W-b** (4.7): the tier pauses the model-request consumer, so no carrier is minted
  after the deferral before the replacement; W-c, W-d and W-e are unit-level (4.1) only.
- **A live measurement of a marshalled record's size** (I: § Fact 5 NOT RUN) is still not run; OQ2's "roughly half the
  ceiling" is arithmetic on two copies of one string, not a measurement.
- **`agent.task` producers per sister** (I: § NOT RUN) — unchanged.
