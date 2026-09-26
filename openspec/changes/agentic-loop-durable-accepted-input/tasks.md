# Tasks — agentic-loop-durable-accepted-input (#1365 + #1345)

Base: `9e5d8455`. Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ST `state.go`, LE
`loop_evidence.go`, A `agentic/state.go`. Pins are generated from the files at base (`sed -n "${n}p"`); an `I:` pin is
the inventory's. Design: `design.md` (amended after review round 1); rows § 2; owner questions OQ0–OQ5 are § 0, each
pre-selected. Tasks marked **[OQn (x)]** run only under that answer. **No task asserts a post-merge fact.**
Implementation serializes with other shared loop changes and precedes #1377 (ruling 5).

> **Two design findings from implementation, both owner questions (not improvised):**
> 1. OQ5 (a) × `recoverEmptyContext` drops a deferred turn on an emptied context (task 1.2, held).
> 2. The delta leaves `### Requirement: The loop record names its outstanding request`
>    (`openspec/specs/agentic-loop/spec.md:1587`) untouched, and it states the old limitation as a SHALL: "A deferred
>    continuation is durable as a MARKER only … a rebuild SHALL clear the marker and warn, and SHALL NOT synthesise the
>    turn" (:1649-1654) and the scenario `A rebuilt loop clears a deferred turn whose text it cannot recover`. The
>    replay implemented here contradicts both; archiving the delta as written leaves the spec false. Resolving it is a
>    second MODIFIED block, which ruling 2's "one MODIFIED requirement" does not allow.

## 0. Gates before any code (design phase closes here)

- [x] 0.1 Independent Tier 1 design review of the amended `design.md` and delta (contract § Required workflow 7;
      ADR-106: the `agentic` package changes), then owner acceptance and answers to OQ0–OQ5 on #1365. Implementation
      waits for both.
      Evidence: independent design review PASS after one amendment round (record on PR #1387); owner acceptance 2026-09-26 on #1365, "OQ0–OQ5: option (a) on every question".
- [x] 0.2 `task inventory:verify -- openspec/changes/agentic-loop-durable-accepted-input/inventory.md` reads
      `pins=149 ok=149` at the implementation's starting commit; the pins are pre-change evidence and are not
      re-pinned after the change lands (a red verify on the landed change is the correct reading).
      Evidence: `pins=149 ok=149 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0` at `613815a4`.

## 1. The record (design § 3.1, § 3.2; Tier 1, additive only)

- [x] 1.1 Add `TaskPrompt string \`json:"task_prompt,omitempty"\`` to `agentic.LoopEntity` beside
      `agentic/state.go:52` — `State              LoopState             `json:"state"`` (the task block), and `PendingContinuationPrompt string
      \`json:"pending_continuation_prompt,omitempty"\`` immediately after `agentic/state.go:141` — `PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"``, each with a doc
      comment stating design I-C / I-A (the write that sets it, the write that clears it, what a rebuild does with
      it). `Validate()` (`agentic/state.go:164` — `func (e *LoopEntity) Validate() error {`) is not changed.
      Evidence: `855b920e`; `task api:compat` `agentic` section adds exactly `LoopEntity.PendingContinuationPrompt: added` and `LoopEntity.TaskPrompt: added`.
- [ ] 1.2 Delete the cache: `processor/agentic-loop/state.go:90` — `taskPrompts          map[string]string                   // loopID -> original task prompt (for context recovery)`, its `make` in the constructor, and
      `processor/agentic-loop/state.go:940` — `delete(m.taskPrompts, loopID)`. `CacheTaskPrompt` (`processor/agentic-loop/state.go:1062` — `func (m *LoopManager) CacheTaskPrompt(loopID, prompt string) {`) sets
      `entity.TaskPrompt` on `m.loops[loopID]` under `m.mu`; `GetTaskPrompt`
      (`processor/agentic-loop/state.go:1069` — `func (m *LoopManager) GetTaskPrompt(loopID string) string {`) reads it. **[OQ5 (a)]** the call at
      `processor/agentic-loop/handlers.go:1016` — `h.loopManager.CacheTaskPrompt(loopID, task.Prompt)` runs only when `!continuation` (birth-only; the deferred turn's text
      is then on the entity once); **[OQ5 (b)]** it runs on every delivery as today. Readers
      `processor/agentic-loop/handlers.go:2529` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`, `processor/agentic-loop/handlers.go:3336` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`,
      `processor/agentic-loop/handlers.go:3302` — `prompt := h.loopManager.GetTaskPrompt(loopID)` are unchanged; the literal fallback at
      `processor/agentic-loop/handlers.go:3303` — `if prompt == "" {` stays as the empty-field branch.
      `git grep -n "taskPrompts" -- '*.go'` must return 0 lines.
      **HELD (not done) — owner question.** OQ5 (a)'s birth-only guard at H:1016 turns `TestTruncationRetryCarriesTheDeferredTurn` (`continuation_deferral_test.go:237`, fails at :296: "the retry request does not contain the continuation's turn") red: when compaction empties the context, `recoverEmptyContext` re-injects the BIRTH prompt, the deferred turn is in no request, the retry is named its carrier, and the completion settles — the turn is lost silently. Mutation: removing the guard turns it green. No fix inside the docket rows exists (F2 keeps the readers unchanged); the diff is parked unpushed. `task_prompt` has no writer until this lands.
- [x] 1.3 `attachContinuation` (`processor/agentic-loop/state.go:292` — `func (m *LoopManager) attachContinuation(loopID, taskID string) (agentic.LoopEntity, bool, error) {`) takes the prompt and sets
      `entity.PendingContinuationPrompt = prompt` inside the `outstanding` branch beside
      `processor/agentic-loop/state.go:328` — `entity.PendingContinuation = true` and `processor/agentic-loop/state.go:333` — `entity.PendingContinuationRequestID = ""`. Its one production caller
      `processor/agentic-loop/handlers.go:918` — `entity, deferred, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)` passes `task.Prompt`; the test callers follow.
      **[OQ1 (b)]** the field is `[]string`: append when the marker is already uncarried, reset when it uncarries a
      carried marker.
      Evidence: `9885d4eb`; W-b/W-c/W-e of 3.1 read the text the admission set.
- [x] 1.4 `SettleRequest` (`processor/agentic-loop/state.go:1295` — `func (m *LoopManager) SettleRequest(loopID, requestID string) {`) clears the text beside
      `processor/agentic-loop/state.go:1302` — `entity.PendingContinuation = false` and `processor/agentic-loop/state.go:1303` — `entity.PendingContinuationRequestID = ""`.
      Evidence: `9885d4eb`; mutation M3 (delete the clear) → W-b `Should be empty, but was and also check the second drawer`.
- [x] 1.5 The marker write (`processor/agentic-loop/component.go:3107` — `func (c *Component) persistDeferredContinuationMarker(ctx context.Context, loopID string) error {`) overlays the text as its third owned field
      beside `processor/agentic-loop/component.go:3140` — `entity.PendingContinuation = true` and `processor/agentic-loop/component.go:3144` — `entity.PendingContinuationRequestID = ""`; its doc
      comment's "owns exactly two" (`processor/agentic-loop/component.go:3088` — `// A lane writes only the fields it owns. The deferred turn owns exactly two —`) becomes three. The text reaches it
      on the `HandlerResult` from `deferredContinuationResult` (`processor/agentic-loop/handlers.go:1119` — `func (h *MessageHandler) deferredContinuationResult(loopID, taskID string, entity agentic.LoopEntity) HandlerResult {`) through
      an unexported field, not by re-reading the live entity (design § 6.9). The call at
      `processor/agentic-loop/component.go:1564` — `if err := c.persistDeferredContinuationMarker(ctx, result.LoopID); errors.Is(err, natsclient.ErrKVRevisionMismatch) {` passes it. Rows at `openspec/specs/agentic-loop/spec.md:1083` — `- **WHEN** it returns an error`
      (lost CAS → Retry, other → best-effort Ack) are unchanged.
      Evidence: `9885d4eb`; the text rides `HandlerResult.deferredPrompt`; mutation M4 (delete the overlay line) → DCRIT:190 and :220 `expected: "a turn typed while the agent was thinking" actual: ""`.
- [x] 1.6 **Adoption is NOT changed** (design OQ4, § 6.11): `adoptNewerRetainedRequest` at
      `processor/agentic-loop/loop_evidence.go:503` — `adopted.PublishedRequestID = retained.RequestID` – `processor/agentic-loop/loop_evidence.go:507` — `adopted.PendingToolResults = nil` keeps leaving
      the marker as it found it; the first draft's carrier-naming line is withdrawn and 3.1's W-e subtest pins why.
      **[OQ4 (b)]** instead: a third field `PendingContinuationBehind` set at 1.3 to the outstanding request, cleared
      at 1.4, and the replay in 1.7 runs only when the retained request equals it.
      Evidence: `loop_evidence.go` untouched on the branch; mutation M2 (re-add the carrier-naming line) → W-e `expected: 1 actual: 0` in the context and `actual: -1` (no request minted: the loop settled silently).
- [x] 1.7 Replay in `restoreLoopFromRequest` (`processor/agentic-loop/state.go:383` — `func (m *LoopManager) restoreLoopFromRequest(`): after
      `processor/agentic-loop/state.go:475` — `cm.RepairToolPairs()` and before `processor/agentic-loop/state.go:481` — `m.cachedTools[record.ID] = request.Tools`, on EVERY uncarried
      marker whose text is non-empty, `cm.AddMessage(RegionRecentHistory, {Role: "user", Content: text})` and an Info
      line naming the loop and `request.RequestID` (design § 3.1: once in W-b/W-e, a logged second copy in W-c); the
      marker is KEPT. The existing clear at `processor/agentic-loop/state.go:435` — `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" {` –
      `processor/agentic-loop/state.go:442` — `entity.PendingContinuation = false` narrows to the text-less marker
      (`&& entity.PendingContinuationPrompt == ""`) and keeps its warning. The doc comment at
      `processor/agentic-loop/state.go:410` — `// A continuation admitted while a request was outstanding is durable as a` – :434 is rewritten to describe the replay, the five windows and design
      § 7.1/7.2 as residuals. **[OQ1 (b)]** replay every element in order.
      Evidence: `9885d4eb`; mutation M1 (delete the replay `AddMessage`) → W-b `expected: 1 actual: 0`, W-c `expected: 2 actual: 1`, W-e `expected: 1 actual: 0`, DCRIT:252 `expected: 1 actual: 0`.

## 2. Task intake (design § 3.3)

- [x] 2.1 Decode and payload-type failures terminate: `processor/agentic-loop/component.go:1476` — `return nil` and
      `processor/agentic-loop/component.go:1482` — `return nil` return `natsclient.TerminateDelivery(fmt.Errorf(…))` in the shape of
      `processor/agentic-loop/component.go:1978` — `return nil, "", natsclient.TerminateDelivery(` and `processor/agentic-loop/component.go:1985` — `return nil, "", natsclient.TerminateDelivery(`. No counter
      (design § 6.8).
      Evidence: `42dd961f`; mutations M5/M5b → the undecodable and wrong-type rows `expected: 0x3 actual: 0x1` (Terminate → Ack).
- [x] 2.2 `HandleTask` errors take their class: at `processor/agentic-loop/component.go:1547` — `return nil` return `err`; before
      it, `errs.IsInvalid(err)` returns `natsclient.TerminateDelivery(err)` in the shape of
      `processor/agentic-loop/component.go:1494` — `return natsclient.TerminateDelivery(err)` (the heartbeat policy at `processor/agentic-loop/component.go:1318` — `var permanent *natsclient.PermanentDeliveryError`
      – :1321 does not read the Invalid class). **[OQ3 (a), pre-selected]** the `ErrLoopBusy` branch at
      `processor/agentic-loop/component.go:1541` — `if errors.Is(err, ErrLoopBusy) {` is unchanged (`Warn` + `return nil`); its comment names it a defined
      refusal and cites `processor/agentic-loop/component.go:1440` — `// The other two settlements were rejected on this lane: Retry parks the whole` – :1442 for why Retry is rejected on a lane at
      MaxAckPending 1 (`processor/agentic-loop/component.go:1261` — `if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {`). **[OQ3 (b)]** would return `err` there — rejected
      in design § 0.
      Evidence: `42dd961f`, helper `taskHandlerErrorDisposition` (`d80456d9`, revive function-length); mutation M6 → the depth and settled-loop rows `expected: 0x3 actual: 0x2` (Terminate → Retry).
- [x] 2.3 **No release code in `HandleTask`** (design § 6.12, § 7.3): one doc sentence at
      `processor/agentic-loop/handlers.go:1102` — `return HandlerResult{}, err` — a failure after `CreateLoop`/`CreateLoopWithID`/`attachContinuation`
      registered or rebound the loop (`startTrajectory` `processor/agentic-loop/handlers.go:968` — `return HandlerResult{}, err` always nil,
      `processor/agentic-loop/trajectory.go:24` — `func (m *trajectoryManager) startTrajectory(loopID string) (agentic.Trajectory, error) {`; `GetLoop` `processor/agentic-loop/handlers.go:980` — `entity, err = h.loopManager.GetLoop(loopID)` only on a
      release race; `buildTaskRequest` never) has no production producer, and a Retry there would meet
      `HasActiveLoopForTask` (`processor/agentic-loop/state.go:642` — `func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {`) on the rebound `TaskID`
      (`processor/agentic-loop/state.go:318` — `entity.TaskID = taskID`) and be acknowledged as a duplicate
      (`processor/agentic-loop/component.go:1580` — `c.logger.Debug("Task deduplicated — loop already active",`) with the turn nowhere.
      Evidence: `42dd961f`, the sentence at `buildTaskRequest`'s error return in `HandleTask`.
- [x] 2.4 **[OQ2 (a), pre-selected]** Over the payload ceiling, three sites: in the birth arm at
      `processor/agentic-loop/component.go:1699` — `} else if err := c.createLoopState(ctx, result.LoopID); err != nil {` – :1716, an `errors.Is(err, nats.ErrMaxPayload)` branch releases the
      loop and returns `natsclient.TerminateDelivery` with the loop id and `len(data)` in the cause (precedent
      `graph/clustering/storage.go:138` — `if stderrors.Is(err, nats.ErrMaxPayload) {`); `createLoopState` (`processor/agentic-loop/component.go:2907` — `func (c *Component) createLoopState(ctx context.Context, loopID string) error {`) already
      wraps with `%w`. In the marker write after `processor/agentic-loop/component.go:3150` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`, the same `errors.Is`
      clears `PendingContinuationPrompt` on the in-memory entity (under `m.mu`), logs a `Warn` with the loop id and
      `len(data)`, and returns as best-effort. The carrier write (`processor/agentic-loop/component.go:3071` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)` –
      `processor/agentic-loop/component.go:3079` — `return fmt.Errorf("persist loop state %s: %w", loopID, err)`) is unchanged: its plain error is the carrier's existing Quarantine
      row, now stated in the delta. **[OQ2 (b)]** instead: compare `len(data)` with `c.natsClient.MaxPayload()`
      (`natsclient/client.go:214` — `func (m *Client) MaxPayload() (int64, error) {`) before each CAS — rejected in design § 0.
      Evidence: `42dd961f` (birth, helper `terminateOversizedBirth` in `d80456d9`), `9885d4eb` (marker, clears only while the entity still holds the refused turn); mutations M9 (birth `0x3`→`0x2`), M7 (marker: text kept AND the following carrier write refused), M8 (drop the owned-turn check → a later turn's text cleared).
- [ ] 2.5 Rewrite the comments that state the old limitation or over-claim: `processor/agentic-loop/doc.go:280` — `// rather than leave a loop that would spend an iteration re-asking the model with nothing` –
      :303 (the re-send paragraphs and the task-prompt paragraph), `processor/agentic-loop/component.go:1550` — `// A deferred continuation is not a dedup and not a spawn: the loop already` –
      :1558 (the deferred branch's "the turn's text does not" sentence), `agentic/state.go:134` — `// It was originally introduced to survive a publish whose durability was` – :140 (the marker's
      replacement paragraph: "closed by identity adoption" is not true — adoption leaves the marker as it found it and
      the rebuild replays; name W-c's duplicate and W-e), and the over-bound sentence at the marker write.
      Done except the task-prompt paragraph of `doc.go`, which waits on 1.2 (it still states the L4a limitation, which remains true while `task_prompt` has no writer).

## 3. Tests (design § 4)

- [ ] 3.1 `TestARebuiltLoopCarriesTheTurnItsRecordAccepted` beside
      `processor/agentic-loop/loop_rebuild_test.go:429` — `func TestARebuiltLoopDoesNotReAskForATurnItCannotRecover(t *testing.T) {`: **five** subtests = design § 3.1 W-a/W-b/W-c/W-d/W-e,
      counting the turn's occurrences in the next minted request (W-a 0; W-b 1, marker names the minted request; W-c 2
      and the replay log names the loop and R(N+1) — 1 under **[OQ4 (b)]**; W-d 1; W-e 1 over an adopted R(N) built
      before the turn — the blocking finding's pin). W-c/W-e drive `adoptNewerRetainedRequest` first through the fake
      bucket and retained-request seam `loop_carrier_test.go`'s adoption tests use. W-b also asserts
      `CompletionState.Prompt == record.TaskPrompt` after the loop settles; a sixth subtest drives `recoverEmptyContext`
      on the rebuilt loop with an emptied context and asserts the record's prompt, not the literal; **[OQ5 (a)]** a
      seventh asserts a continued loop's completion carries the BIRTH prompt.
      `TestARebuiltLoopDoesNotReAskForATurnItCannotRecover` keeps only the text-less-marker case (its assertion at
      `processor/agentic-loop/loop_rebuild_test.go:474` — `require.Empty(t, completion.CompletionState.Prompt,` moves to the new test as its inverse).
      `// spec: agentic-loop / Loop input classes settle after owner-specific durable done`.
      Five window subtests landed (`c6f810bd`, `deferred_turn_rebuild_test.go`), all PASS; the W-b `CompletionState.Prompt`, sixth (`recoverEmptyContext`) and seventh (birth prompt on a continued loop) subtests wait on 1.2. LRT:429 keeps its text-less case; its `Prompt` assertion moves when 1.2 lands.
- [x] 3.2 `TestADeferredContinuationWritesOnlyTheMarkerItOwns`
      (`processor/agentic-loop/deferred_continuation_record_integration_test.go:178` — `func TestADeferredContinuationWritesOnlyTheMarkerItOwns(t *testing.T) {`): the record assertions gain
      `pending_continuation_prompt`; the crash arm at
      `processor/agentic-loop/deferred_continuation_record_integration_test.go:211` — `t.Run("a crash in the interval leaves a record the batch can be replayed against", func(t *testing.T) {` (W-b: R2's PubAck never lands)
      asserts the replacement's next request on `agent.request.<loopID>` contains the turn exactly once.
      Evidence: `c6f810bd`; integration `--- PASS: TestADeferredContinuationWritesOnlyTheMarkerItOwns` (both arms).
- [x] 3.3 `TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition` in
      `processor/agentic-loop/transition_result_test.go`, driven through the production heartbeat callback like
      `processor/agentic-loop/transition_result_test.go:120` — `func TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition(t *testing.T) {`: undecodable → Terminate; wrong type → Terminate;
      depth → Terminate; `ctx` cancelled → Retry and `HasActiveLoopForTask` false; `ErrLoopTerminal` → Terminate;
      `ErrLoopBusy` → Ack **[OQ3 (a)]**. No injected post-registration fault (no producer, design § 6.12). Every row is
      today's Ack, which is the counterexample.
      Evidence: `c6f810bd`; six rows PASS. The cancelled row drives `handleTaskMessage` directly: `taskInputHandler` already answered Retry for an expired work context, so the counterexample there is the handler's own `nil`.
- [x] 3.4 **[OQ2 (a)]** Over-the-ceiling rows with a fake `jetstream.KeyValue` returning `nats.ErrMaxPayload` (the
      shape of `loop_carrier_test.go`'s fake, `processor/agentic-loop/loop_carrier_test.go:352` — `func TestBirthRefusesASecondCreateForTheSameLoop(t *testing.T) {`): birth `Create`
      refused → Terminate, loop released; marker `Update` refused → Ack, `PendingContinuationPrompt` empty on the
      in-memory entity afterwards, the `Warn` names the size, and a following carrier write on the same loop is NOT
      refused; carrier `Update` refused → the existing Quarantine row, asserted so the delta's sentence is proved.
      Evidence: `c6f810bd`/`a90c6388`, `payload_ceiling_test.go`, four subtests PASS (the fourth pins the reviewer's owned-turn rule).
- [x] 3.5 Mutation evidence for the WIRING (`cp` backup, `md5 -q` before and after, one site at a time, recorded in
      the PR body): delete the replay `AddMessage` → 3.1 W-b, W-c, W-e red (0); re-add the withdrawn adoption line →
      3.1 W-e red (0, silent); delete the `SettleRequest` clear → 3.1's settle assertion red; delete the overlay's text
      line → 3.2 red; replace one `TerminateDelivery` with `return nil` → its 3.3 row red; replace the `errs.IsInvalid`
      branch with `return err` → the depth row red; delete the marker write's over-bound clear → 3.4's "following
      carrier write" red.
      Evidence: M1–M9 in the PR body; each `cp` backup + `shasum -a 256` restored equal, `git status --porcelain` 0 lines.
- [x] 3.6 Controls run by name with `-race -count=1`, unchanged beyond an `attachContinuation` argument:
      `processor/agentic-loop/continuation_deferral_test.go:115` — `func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {`,
      `processor/agentic-loop/continuation_deferral_test.go:336` — `func TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns(t *testing.T) {`,
      `processor/agentic-loop/continuation_deferral_test.go:563` — `func TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:78` — `func TestTaskRedeliveredToAReplacementLeavesOneFirstRequest(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:408` — `func TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:718` — `func TestAColdContinuationForALoopNoProcessHoldsIsRefused(t *testing.T) {`,
      `processor/agentic-loop/applied_facts_property_test.go:123` — `func TestPropAppliedFactsHoldAcrossEveryCrashWindow(t *testing.T) {`. Paste the `--- PASS` lines, not "green".
      Evidence: `-race -count=1 -tags integration`: `--- PASS` for all seven by name (PR body).
- [ ] 3.7 `task e2e:agentic`: extend `verifyMidFlightLoopAcrossReplacement`
      (`test/e2e/scenarios/agentic/stage_a_process_replacement.go:898` — `func (s *Scenario) verifyMidFlightLoopAcrossReplacement(`) — with the model-request consumer paused
      (`test/e2e/scenarios/agentic/stage_a_process_replacement.go:909` — `if _, err := agentStream.PauseConsumer(ctx, modelRequestConsumerName, time.Now().Add(2*time.Minute)); err != nil {`) and R1 retained, publish a continuation task
      naming the loop, replace, resume; assert R2 carries the turn once and `agent.complete` carries `prompt`. This
      reaches W-b only (design § 10). Final validation of the landed diff (proposal § Impact), not the iteration loop;
      `pgrep -fl e2e.test` into the tier log first.

## 4. Spec and docs

- [x] 4.1 `openspec validate agentic-loop-durable-accepted-input --strict` green; `task spec:properties` count moves
      only by the new `// spec:` lines (`git add` the new test first; the S:886 heading is unchanged, so the 26 existing
      citations resolve).
      Evidence: `Change 'agentic-loop-durable-accepted-input' is valid`; `spec-properties: 407/407 citations resolve` (404 at `9e5d8455` + 3 new `// spec:` lines, all tracked).
- [ ] 4.2 Migration section in `docs/operations/migration-beta162-to-beta163.md` per design § 5, after the #1374
      section; mark `docs/operations/migration-beta162-to-beta163.md:1873` — `**A deferred turn is durable as a MARKER only, and so is nothing about the task prompt.** A continuation admitted` and
      `docs/operations/migration-beta162-to-beta163.md:1885` — `The loop's task prompt is the same limitation one field over. A loop rebuilt from its record and a retained request —` superseded by it; name the birth-prompt change on
      continued loops **[OQ5 (a)]**, the W-c duplicate, and the whole-record bound.
      Section landed (`621807b2`) and the L4a deferred-turn paragraph is marked superseded in part; the birth-prompt line and the task-prompt paragraph's supersession wait on 1.2.

## 5. Gates

- [ ] 5.1 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines, not "green".
- [x] 5.2 `task api:compat`: `agentic` reads additions only (two exported fields), `processor/agentic-loop` no exported
      change; paste the per-package lines.
      Evidence: `FAILING TOTAL: 15` (unchanged from main); `agentic` gains only `LoopEntity.PendingContinuationPrompt: added` and `LoopEntity.TaskPrompt: added`; `processor/agentic-loop` lists nothing from this branch.
- [ ] 5.3 PR body: `implemented-by: <persona>`, the OQ0–OQ5 answers quoted from #1365, the 3.5 mutation runs, the 3.6
      control runs, the 3.7 tier line.
- [ ] 5.4 Archive: `openspec archive agentic-loop-durable-accepted-input --yes` as the last content commit; the MODIFIED
      block syncs into `openspec/specs/agentic-loop/spec.md` and the REMOVED requirement leaves it (OQ0).
