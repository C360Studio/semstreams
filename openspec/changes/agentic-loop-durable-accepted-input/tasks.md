# Tasks — agentic-loop-durable-accepted-input (#1365 + #1345)

Base: `9e5d8455`. Pin keys: C `processor/agentic-loop/component.go`, H `handlers.go`, ST `state.go`, LE
`loop_evidence.go`, A `agentic/state.go`. Pins are generated from the files at base (`sed -n "${n}p"`); an `I:` pin is
the inventory's. Design: `design.md`; rows § 2; owner questions OQ0–OQ3 are § 0. Tasks marked **[OQn (x)]** run only
under that answer. **No task asserts a post-merge fact.** Implementation serializes with other shared loop changes and
precedes #1377 (ruling 5).

## 0. Gates before any code (design phase closes here)

- [ ] 0.1 Independent Tier 1 design review of `design.md` and the delta (contract § Required workflow 7; ADR-106: the
      `agentic` package changes), then owner acceptance and answers to OQ0–OQ3 on #1365. Implementation waits for both.
- [ ] 0.2 `task inventory:verify -- openspec/changes/agentic-loop-durable-accepted-input/inventory.md` reads
      `pins=149 ok=149` at the implementation's starting commit; the pins are pre-change evidence and are not
      re-pinned after the change lands (a red verify on the landed change is the correct reading).

## 1. The record (design § 3.1, § 3.2; Tier 1, additive only)

- [ ] 1.1 Add `TaskPrompt string \`json:"task_prompt,omitempty"\`` to `agentic.LoopEntity` beside
      `agentic/state.go:52` — `State              LoopState             `json:"state"`` (the task block), and `PendingContinuationPrompt string
      \`json:"pending_continuation_prompt,omitempty"\`` immediately after `agentic/state.go:141` — `PendingContinuationRequestID string `json:"pending_continuation_request_id,omitempty"``, each with a doc
      comment stating design I-C / I-A (the write that sets it, the write that clears it, what a rebuild does with
      it). `Validate()` (`agentic/state.go:164` — `func (e *LoopEntity) Validate() error {`) is not changed.
- [ ] 1.2 Delete the cache: `processor/agentic-loop/state.go:90` — `taskPrompts          map[string]string                   // loopID -> original task prompt (for context recovery)`, its `make` in the constructor, and
      `processor/agentic-loop/state.go:940` — `delete(m.taskPrompts, loopID)`. `CacheTaskPrompt` (`processor/agentic-loop/state.go:1062` — `func (m *LoopManager) CacheTaskPrompt(loopID, prompt string) {`) sets
      `entity.TaskPrompt` on `m.loops[loopID]` under `m.mu`; `GetTaskPrompt`
      (`processor/agentic-loop/state.go:1069` — `func (m *LoopManager) GetTaskPrompt(loopID string) string {`) reads it. Callers `processor/agentic-loop/handlers.go:1016` — `h.loopManager.CacheTaskPrompt(loopID, task.Prompt)`,
      `processor/agentic-loop/handlers.go:2529` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`, `processor/agentic-loop/handlers.go:3336` — `Prompt:       h.loopManager.GetTaskPrompt(loopID),`,
      `processor/agentic-loop/handlers.go:3302` — `prompt := h.loopManager.GetTaskPrompt(loopID)` are unchanged; the literal fallback at
      `processor/agentic-loop/handlers.go:3303` — `if prompt == "" {` stays as the empty-field branch.
      `git grep -n "taskPrompts" -- '*.go'` must return 0 lines.
- [ ] 1.3 `attachContinuation` (`processor/agentic-loop/state.go:292` — `func (m *LoopManager) attachContinuation(loopID, taskID string) (agentic.LoopEntity, bool, error) {`) takes the prompt and sets
      `entity.PendingContinuationPrompt = prompt` inside the `outstanding` branch beside
      `processor/agentic-loop/state.go:328` — `entity.PendingContinuation = true` and `processor/agentic-loop/state.go:333` — `entity.PendingContinuationRequestID = ""`. Its one production caller
      `processor/agentic-loop/handlers.go:918` — `entity, deferred, err = h.loopManager.attachContinuation(task.LoopID, task.TaskID)` passes `task.Prompt`; the test callers follow.
      **[OQ1 (b)]** the field is `[]string`: append when the marker is already uncarried, reset when it uncarries a
      carried marker.
- [ ] 1.4 `SettleRequest` (`processor/agentic-loop/state.go:1295` — `func (m *LoopManager) SettleRequest(loopID, requestID string) {`) clears the text beside
      `processor/agentic-loop/state.go:1302` — `entity.PendingContinuation = false` and `processor/agentic-loop/state.go:1303` — `entity.PendingContinuationRequestID = ""`.
- [ ] 1.5 The marker write (`processor/agentic-loop/component.go:3105` — `// sentinel the task lane reads as "redeliver this turn to whoever holds the`) overlays the text as its third owned field
      beside `processor/agentic-loop/component.go:3140` — `entity.PendingContinuation = true` and `processor/agentic-loop/component.go:3144` — `entity.PendingContinuationRequestID = ""`; the text
      reaches it on the `HandlerResult` from `deferredContinuationResult`
      (`processor/agentic-loop/handlers.go:1097` — `return h.deferredContinuationResult(loopID, task.TaskID, entity), nil`) through an unexported field, not by re-reading the live entity
      (design § 6.9). The call at `processor/agentic-loop/component.go:1564` — `if err := c.persistDeferredContinuationMarker(ctx, result.LoopID); errors.Is(err, natsclient.ErrKVRevisionMismatch) {` passes it. Rows at
      `openspec/specs/agentic-loop/spec.md:1083` — `- **WHEN** it returns an error` (lost CAS → Retry, other → best-effort Ack) are unchanged.
- [ ] 1.6 Adoption names the carrier: one line after `processor/agentic-loop/loop_evidence.go:507` — `adopted.PendingToolResults = nil` —
      `if adopted.PendingContinuation && adopted.PendingContinuationRequestID == "" { adopted.PendingContinuationRequestID = retained.RequestID }`
      — with a comment citing `agentic/state.go:140` — `// closed by identity adoption rather than by this field.` (the claim the code now honours) and design § 3.1 W-c.
- [ ] 1.7 Replay in `restoreLoopFromRequest` (`processor/agentic-loop/state.go:383` — `func (m *LoopManager) restoreLoopFromRequest(`): after
      `processor/agentic-loop/state.go:475` — `cm.RepairToolPairs()` and before `processor/agentic-loop/state.go:481` — `m.cachedTools[record.ID] = request.Tools`, when the marker is
      uncarried and the text is non-empty, `cm.AddMessage(RegionRecentHistory, {Role: "user", Content: text})` and an
      Info line naming the loop; the marker is KEPT. The existing clear at `processor/agentic-loop/state.go:435` — `if entity.PendingContinuation && entity.PendingContinuationRequestID == "" {` –
      `processor/agentic-loop/state.go:442` — `entity.PendingContinuation = false` narrows to the text-less marker (`&& entity.PendingContinuationPrompt == ""`)
      and keeps its warning. The doc comment at `processor/agentic-loop/state.go:410` — `// A continuation admitted while a request was outstanding is durable as a` – :434 is rewritten to describe
      the replay and design § 7.1/7.2 as residuals. **[OQ1 (b)]** replay every element in order.

## 2. Task intake (design § 3.3)

- [ ] 2.1 Decode and payload-type failures terminate: `processor/agentic-loop/component.go:1476` — `return nil` and
      `processor/agentic-loop/component.go:1482` — `return nil` return `natsclient.TerminateDelivery(fmt.Errorf(…))` in the shape of
      `processor/agentic-loop/component.go:1978` — `return nil, "", natsclient.TerminateDelivery(` and `processor/agentic-loop/component.go:1985` — `return nil, "", natsclient.TerminateDelivery(`. No counter
      (design § 6.8).
- [ ] 2.2 `HandleTask` errors take their class: at `processor/agentic-loop/component.go:1547` — `return nil` return `err`; before
      it, `errs.IsInvalid(err)` returns `natsclient.TerminateDelivery(err)` in the shape of
      `processor/agentic-loop/component.go:1494` — `return natsclient.TerminateDelivery(err)` (the heartbeat policy at `processor/agentic-loop/component.go:1318` — `var permanent *natsclient.PermanentDeliveryError`
      – :1321 does not read the Invalid class). **[OQ3 (b)]** the `ErrLoopBusy` branch at
      `processor/agentic-loop/component.go:1541` — `if errors.Is(err, ErrLoopBusy) {` keeps its `Warn` and returns `err`; **[OQ3 (a)]** it keeps
      `return nil` and the comment names it a defined refusal.
- [ ] 2.3 A failed birth releases its loop inside `HandleTask` (`processor/agentic-loop/handlers.go:866` — `func (h *MessageHandler) HandleTask(ctx context.Context, task TaskMessage) (HandlerResult, error) {`): a deferred
      `if err != nil && !continuation && loopID != "" { _ = h.loopManager.DeleteLoop(loopID) }` beside the trajectory
      discard at `processor/agentic-loop/handlers.go:971` — `defer func() {`; never on a continuation
      (`processor/agentic-loop/trajectory_handler_wiring.go:60` — `// either — since #1227 that loop may be one it ATTACHED to rather than created,` – :62 states why). The component's own release
      (`processor/agentic-loop/trajectory_handler_wiring.go:63` — `func (c *Component) releaseLoopTransientState(loopID string) {`) is not called from the error path: the revision was
      not seeded and the audit-loss marker not set before `HandleTask` returned.
- [ ] 2.4 **[OQ2 (a)]** In the birth arm at `processor/agentic-loop/component.go:1699` — `} else if err := c.createLoopState(ctx, result.LoopID); err != nil {` – :1716, an
      `errors.Is(err, nats.ErrMaxPayload)` branch releases the loop and returns `natsclient.TerminateDelivery` with the
      loop id and `len(data)` in the cause (precedent `graph/clustering/storage.go:138` — `if stderrors.Is(err, nats.ErrMaxPayload) {`); `createLoopState`
      (`processor/agentic-loop/component.go:2907` — `func (c *Component) createLoopState(ctx context.Context, loopID string) error {`) wraps the client's error with `%w` so the branch can see it. The
      marker write's over-bound case keeps its best-effort row and logs the refusal. **[OQ2 (b)]** instead: a constant
      cap checked in `preflightDecodedTask` (`processor/agentic-loop/component.go:1782` — `func (c *Component) preflightDecodedTask(task *agentic.TaskMessage) (map[string]any, bool, error) {`) with a Terminate.
- [ ] 2.5 Rewrite the comments that state the old limitation: `processor/agentic-loop/doc.go:280` — `// rather than leave a loop that would spend an iteration re-asking the model with nothing` – :303 (the
      re-send paragraphs and the task-prompt paragraph), `processor/agentic-loop/component.go:1550` — `// A deferred continuation is not a dedup and not a spawn: the loop already` – :1558 (the
      deferred branch's "the turn's text does not" sentence), `agentic/state.go:134` — `// It was originally introduced to survive a publish whose durability was` – :140 (the marker's
      replacement paragraph now names the text field and the adoption line).

## 3. Tests (design § 4)

- [ ] 3.1 `TestARebuiltLoopCarriesTheTurnItsRecordAccepted` beside
      `processor/agentic-loop/loop_rebuild_test.go:429` — `func TestARebuiltLoopDoesNotReAskForATurnItCannotRecover(t *testing.T) {`: four subtests = design § 3.1 W-a/W-b/W-c/W-d, counting the
      turn's occurrences in the next minted request (exactly 1 / 0), asserting the carrier the marker names, and in
      W-b `CompletionState.Prompt == record.TaskPrompt` after the loop settles; a fifth subtest drives
      `recoverEmptyContext` on the rebuilt loop with an emptied context and asserts the record's prompt, not the
      literal. `TestARebuiltLoopDoesNotReAskForATurnItCannotRecover` keeps only the text-less-marker case (its
      assertion at `processor/agentic-loop/loop_rebuild_test.go:474` — `require.Empty(t, completion.CompletionState.Prompt,` moves to the new test as its inverse).
      `// spec: agentic-loop / Loop input classes settle after owner-specific durable done`.
- [ ] 3.2 `TestADeferredContinuationWritesOnlyTheMarkerItOwns`
      (`processor/agentic-loop/deferred_continuation_record_integration_test.go:178` — `func TestADeferredContinuationWritesOnlyTheMarkerItOwns(t *testing.T) {`): the record assertions gain
      `pending_continuation_prompt`; the crash arm at
      `processor/agentic-loop/deferred_continuation_record_integration_test.go:211` — `t.Run("a crash in the interval leaves a record the batch can be replayed against", func(t *testing.T) {` asserts the replacement's next
      request on `agent.request.<loopID>` contains the turn exactly once.
- [ ] 3.3 `TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition` in
      `processor/agentic-loop/transition_result_test.go`, driven through the production heartbeat callback like
      `processor/agentic-loop/transition_result_test.go:120` — `func TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition(t *testing.T) {`: undecodable → Terminate; wrong type → Terminate;
      depth → Terminate; `ctx` cancelled → Retry; `ErrLoopTerminal` → Terminate; `ErrLoopBusy` → Retry **[OQ3 (b)]** /
      Ack **[OQ3 (a)]**; a birth failing after registration → Retry, `HasActiveLoopForTask` false, the redelivery
      `Created`; **[OQ2 (a)]** `nats.ErrMaxPayload` from `Create` → Terminate, loop released (a fake `KeyValue` in the
      shape of `loop_carrier_test.go`'s). Every row is today's Ack, which is the counterexample.
- [ ] 3.4 Mutation evidence for the WIRING (`cp` backup, `md5 -q` before and after, one site at a time, recorded in
      the PR body): delete the replay `AddMessage` → 3.1 W-b red only; delete the adoption line → 3.1 W-c red only
      (count 2); delete the `SettleRequest` clear → 3.1's settle assertion red; delete the overlay's text line → 3.2
      red; delete 2.3's `DeleteLoop` → 3.3's birth row red ("deduplicated"); replace one `TerminateDelivery` with
      `return nil` → its 3.3 row red.
- [ ] 3.5 Controls run by name with `-race -count=1`, unchanged beyond an `attachContinuation` argument:
      `processor/agentic-loop/continuation_deferral_test.go:115` — `func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {`,
      `processor/agentic-loop/continuation_deferral_test.go:336` — `func TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns(t *testing.T) {`,
      `processor/agentic-loop/continuation_deferral_test.go:563` — `func TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:78` — `func TestTaskRedeliveredToAReplacementLeavesOneFirstRequest(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:408` — `func TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry(t *testing.T) {`,
      `processor/agentic-loop/task_redelivery_integration_test.go:718` — `func TestAColdContinuationForALoopNoProcessHoldsIsRefused(t *testing.T) {`,
      `processor/agentic-loop/applied_facts_property_test.go:123` — `func TestPropAppliedFactsHoldAcrossEveryCrashWindow(t *testing.T) {`. Paste the `--- PASS` lines, not "green".
- [ ] 3.6 `task e2e:agentic`: extend `verifyMidFlightLoopAcrossReplacement`
      (`test/e2e/scenarios/agentic/stage_a_process_replacement.go:898` — `func (s *Scenario) verifyMidFlightLoopAcrossReplacement(`) — with the model-request consumer paused
      (`test/e2e/scenarios/agentic/stage_a_process_replacement.go:909` — `if _, err := agentStream.PauseConsumer(ctx, modelRequestConsumerName, time.Now().Add(2*time.Minute)); err != nil {`) and R1 retained, publish a continuation task
      naming the loop, replace, resume; assert R2 carries the turn once and `agent.complete` carries `prompt`. Final
      validation of the landed diff (proposal § Impact), not the iteration loop; `pgrep -fl e2e.test` into the tier
      log first.

## 4. Spec, docs, gates

- [ ] 4.1 `openspec validate agentic-loop-durable-accepted-input --strict` green; `task spec:properties` count moves
      only by the new `// spec:` lines (`git add` the new test first; the S:886 heading is unchanged, so the 26 existing
      citations resolve).
- [ ] 4.2 Migration section in `docs/operations/migration-beta162-to-beta163.md` per design § 5, after the #1374
      section; mark `docs/operations/migration-beta162-to-beta163.md:1873` — `**A deferred turn is durable as a MARKER only, and so is nothing about the task prompt.** A continuation admitted` and
      `docs/operations/migration-beta162-to-beta163.md:1885` — `The loop's task prompt is the same limitation one field over. A loop rebuilt from its record and a retained request —` superseded by it.
- [ ] 4.3 `task check:push` green (build, lint, tagged vet, schema drift, contract, race unit, integration); paste the
      summary lines. `task api:compat`: `agentic` reads additions only (two exported fields), `processor/agentic-loop`
      no exported change; paste the per-package lines.
- [ ] 4.4 PR body: `implemented-by: <persona>`, the OQ0–OQ3 answers quoted from #1365, the 3.4 mutation runs, the 3.5
      control runs, the 3.6 tier line.
- [ ] 4.5 Archive: `openspec archive agentic-loop-durable-accepted-input --yes` as the last content commit; the MODIFIED
      block syncs into `openspec/specs/agentic-loop/spec.md` and the REMOVED requirement leaves it (OQ0 (a)).
