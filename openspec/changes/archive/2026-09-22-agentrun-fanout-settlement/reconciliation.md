# #1249 reconciliation — accepted design (draft 4 + 2026-09-19 § 2.6 amendment) against `main` `b7ce8727`

Old base `0053183d` (L1 branch at round 1; six pins attributed to L1 head `c2a9cef6`). New base
`b7ce8727a770c9f880049def24abe887545bccbe` (= `origin/main`, #1357 merged; HEAD `8ddc8974` adds only the change directory).
Every "new pin" below is verified in `inventory.md` (`pins=214 ok=214`, exit 0). Effect vocabulary: `none` — the premise
holds, at most a number moves inside prose; `mechanical` — pin or citation only; `semantic` — the design/proposal/spec/task
text must change, replacement given in § B; `owner` — an owner question, recommendation in `report.md`.

## A. Rows

| # | Premise (design §, old pin) | On `main` at `b7ce8727` (new pin) | Effect |
|---|---|---|---|
| R1 | Header: three files differ at L1 head; L0 head `759bd596`; "Blocked by #1341" (round 5) | 18 pinned files changed since `0053183d`, three `delivery_owner.go` deleted; L0 `f4d66934`, L1 `94cd8e4c`, #1357 `b7ce8727` all merged; `agentic/agentrun/`, `internal/agentterminal/`, `service/`, `cmd/`, `pkg/lifecycle`, `pkg/errs`, `metric`, `config/streams.go`, `internal/maxdelivery` byte-identical | mechanical |
| R2 | § 2.3 `DeliveryWork` "at L0 head is `func(context.Context, []byte) (DeliveryDecision, error)`" | identical, live at `natsclient/delivery_settlement.go:35` | none |
| R3 | § 2.3 rows: InProgress `:361/:366` (L1 `:383/:388`); metadata `:379`, checked `:329` (L1 `:401/:351`); `interpretDeliveryWork` `:392/:396` (L1 `:414/:418`); `heartbeat.go:113` | `:372/:377`; `:390`, `:341`; `:403/:407`; `heartbeat.go:118` — same text, mechanisms intact | mechanical |
| R4 | § 2.3 handler-transient row "as dispatch `component.go:530`" | `processor/agentic-dispatch/component.go:551` (`DelayedDeliveryRetry(30 * time.Second)`) | mechanical |
| R5 | § 2.6 "The closure at `:810` becomes `deliverylane.Consume(ctx, msg, policy, lane.admission)`" | `Consume` returns `(natsclient.DeliveryResult, bool)` (`deliverylane.go:105-118`); a refusal is the zero result whose `Err()` is non-nil by construction (`:126-132`); every consumer guards on `admitted` (loop `:1110-1111`, `:1136`; dispatch `:612-613`; governance `:496`) | semantic → B1 |
| R6 | § 2.6 "`react` records the fatal and drains that lane's exact handle" | `onFatal` (`NewAdmission`'s first arg) runs synchronously inside `Latch` before the result is buffered (`:78-81`) — that is the recorder; `Observe` runs `react` then drains the handle itself (`:240-241`); `react` is required non-nil (`:231-232`); all five consumers pass a log-only `react` (loop `:1161`, governance `:508`, model `:419`) | semantic → B2 |
| R7 | § 2.6 "`Done()` is never nil, so `stop()` awaits `Closed()` (`:727`) and both-drain-first (`:718`) holds" — no join of `Done()` stated | consumers' Stop = `Drain()` each binding → await each `Closed()` → cancel → `<-binding.Done()` each (loop `:761`, `:781`); #1357 archive `design.md:363-375` writes exactly this for agentrun: "cancels `runCtx`, joins both `Done()` — a join draft-4 never specified"; `runCtx`/`cancel` at `agentrun.go:806-807`, `o.cancel()` at `:743` | semantic → B3 |
| R8 | § 2.6 `milestoneConsumerOwner` "stays the SOLE owner of both `ConsumeContext`s"; its running-Stop force `Stop()` fallback (`:734-737`) | `Binding` exposes `Drain`/`Closed`/`Done` only (`deliverylane.go:207-216`), no `Stop`; none of the five consumers keeps a `.Stop()` fallback (grep → 0); no agentrun test exercises `:734-737`; the ruling text and #1357 § 6 are silent on it | owner → OQ2 |
| R9 | § 2.6 "`NewAdmission(onFatal, nil)` — the milestone lanes declare no refusal today; wiring `onRefused` is #1342's scope" | live SHALL `jetstream-consumer-policy/spec.md:606-608` + scenario `:625-629`: every refused delivery is declared; #1342's lane table (at `c58c65bd`) lists the eight existing lanes, none agentrun; issuecomment-5763070246 puts the per-lane wiring test on "the change that wires the declarer" | owner → OQ1 |
| R10 | § 2.6 "Sequencing: #1329 → #1341 → #1249; the implementation task list re-pins against the merged #1341 commit" | done: this file + `tasks-pins.md`; #1341 = `b7ce8727` | mechanical |
| R11 | § 2.7 dispatch metrics-at-construction `component.go:185` | `:196` (`metrics:       getMetrics(deps.MetricsRegistry),`) | mechanical |
| R12 | § 2.8 heartbeat ceiling `delivery_settlement.go:191`; loop `validateLoopRetryPolicy` base `:1129` (L1 `:1154`); dispatch `:575`, `:684`, `:688` and the quoted "unlimited, retention-bounded settlement" | `:181`; loop `:1203`; dispatch terminal lanes `:595`, `:636` (`MaxDeliver:    0,`); the stated choice now reads "Both terminal lanes run MaxDeliver=0, so a blind NAK here is an unbounded retry of an effect whose commit state is unproven" (`:755-756`); approval-pending lane and its `MaxDeliver 10` deleted (`:725`); the contrast still rests on `agentic-terminal-events/spec.md:242` (unchanged) | mechanical (quote swap) |
| R13 | § 2.9 legacy 5s NAK `heartbeat.go:122` | `:127` | mechanical |
| R14 | § 2d (inventory) / § 2.5: the loop publishes terminals without `Nats-Msg-Id`; no duplicates window | still true: `agent.failed` via `PublishToStream` (`:1831`); `publishResults` uses `PublishToStreamWithMsgID` (`:2328`) but only `agent.request` messages set `MsgID` (`handlers.go:1174`, `:2194`, `:2958`); no tracked config sets `duplicates` | none |
| R15 | § 4 bullet 1: expected `agentrun.go:812` only "(true at L1 head; six on `origin/main` incl. `consume_durable.go:39`)" | task 1.2 grep on `main`, stderr visible: `agentic/agentrun/agentrun.go:812` and the declaration `natsclient/heartbeat.go:84` only; `consume_durable.go` absent | mechanical (drop the parenthetical) |
| R16 | § 4 bullet 2: delete `heartbeat.go:79`, `:37`; "delete the `Deprecated:` notice (`:75`) IF present" | `:84`, `:37`; no `Deprecated:` marker exists — `:75-83` is a doc comment naming #1249 as the deleting PR and goes with the function; `heartbeat.go` 156 lines | mechanical |
| R17 | § 4 ratchet `:426`, `:444`, `:395`, `:423` | `:428`, `:446`, `:395`, `:425` | mechanical |
| R18 | § 4 Tier 1 "a removed export is one 'Incompatible changes' package"; § 7 / tasks 7.4 "one incompatible change in `natsclient`" | `task api:compat:report` at HEAD (51 s, base `v1.0.0-beta.162`, 62 packages): 15 incompatible, exit 0; `natsclient` already listed (`NewDurableHandler: removed`), `agentic/agentrun` already listed (`EntityIDPattern`, `Mint`) | semantic → B4 |
| R19 | § 4 spec delta (1): MODIFIED consumer-policy "one permanent exported surface" against L0 head `:73-103`, "targeting L0's HEAD text as L1 does (D8)" | live `openspec/specs/jetstream-consumer-policy/spec.md:380-413`; scenario names identical to the delta's three (`public surface at this layer` `:396`, `binding migration requires semantic authority`, `fast lane lacks an admitted settlement route`), bullet counts equal; the live third paragraph (`:387-390`, "model, loop, and AgentRun … keep the legacy helper") is stale since L1 and is replaced whole by the block — no delta text change | mechanical |
| R20 | § 4 spec delta (2) + inventory § 2i/Adjacent: "L0's delta REMOVES 'Heartbeat consumption SHALL expose settlement failure'"; the package carries one nats-streaming REMOVED ("shrinking remainder") | L0 landed it as a MODIFIED block (archive `2026-09-19-semantic-jetstream-settlement/specs/nats-streaming/spec.md:79-81`, commit `f4d66934`); live at `openspec/specs/nats-streaming/spec.md:158-181` with `:169` "This requirement is deleted together with the helper by the PR that migrates its last caller (#1249)"; "shrinking remainder" live `:239-255`, header text identical to the delta's | semantic → B5 |
| R21 | § 4 docs: `migration-beta162-to-beta163.md:1050-1065`; `migration-restart-safe-nats-client.md:95`; `33-semantic-settlement.md:99` | `:1216-1231` (`:1226` "still exported at this tag"); `:104-108` (now "#1327 takes model and loop, #1249 takes AgentRun"); `:107-108` | mechanical |
| R22 | § 5 / tasks 8.4: O5 recorded on #1155 by the coordinator | not yet — latest #1155 comment is 2026-09-14; boot roots unchanged (`cmd/e2e-semstreams/main.go:261`, `:272`; `cmd/semstreams/main.go:334`, `:347`); no handler registered in either root | none (coordinator action outstanding) |
| R23 | § 6.4 "A sixth copy: declared, ≈75 lines" | superseded by the amendment (round 5 already says so) | mechanical (strike) |
| R24 | § 2.7 metric `semstreams_agentrun_milestone_decisions_total` | no collision: `agentrun_milestone|milestone_decisions` → change docs only; no `Subsystem: "agentrun"`; no `RegisterCounterVec("agentrun", …)` | none |
| R25 | § 1, 2.1–2.3, 2.7 pins in `agentrun.go`, `terminal.go`, `manager.go`, `errors.go`, `projection.go`, `errs.go`, `service/*`, `cmd/*`, `metric/registry.go`, `observer.go`, `tier1-packages.txt`, `api-compat.sh`, `ci.yml:238` | byte-identical / same line | none |
| R26 | `proposal.md` "A sixth private copy of the admission latch…", Affected code "new `agentic/agentrun/delivery_owner.go`", Non-goals "No consolidation of the six admission-latch copies (L1's declared residual)" | stale against the 2026-09-19 amendment and #1357 | semantic → B6 |
| R27 | `specs/agent-run-milestones/spec.md` scenario "its exact handle is drained and marked drained"; requirement "`stop()` waits for each still-running handle's `Closed` without a second drain" | the drained flags are gone; the binding drains once; `stop()` also joins the observer | semantic → B7 |
| R28 | `tasks.md` 1.1 (PR against the L1 branch), 3.1 (`consumeAdmittedDelivery`), 4.1 (copy from the loop `delivery_owner.go`), 4.2 (drained flags), 4.3/6.1/7.x/8.x pins | see `tasks-pins.md` | mechanical + B1–B3 carried into 3.1/4.1/4.2 |

## B. Replacement sentences (exact text)

B1 — design § 2.6, replace "The closure at `:810` becomes `deliverylane.Consume(ctx, msg, policy, lane.admission)`." with:
"The closure at `:810` becomes `result, admitted := deliverylane.Consume(msgCtx, msg, policy, lane.admission)` followed by
`if !admitted { return }` — a refusal returns the zero `DeliveryResult`, whose `Err()` is non-nil by construction
(`internal/deliverylane/deliverylane.go:126-132`), so an unguarded `result.Err() != nil` branch would log a refused
delivery as a settlement failure. Every § 2.3 log line and § 2.7 increment is emitted inside the `DeliveryWork`, keyed on
the decision it returns, never on `result`; the only branch after `Consume` logs a settlement-method error
(`admitted && result.Err() != nil && !result.OwnerStopRequired()`, as loop `component.go:1111`)."

B2 — design § 2.6, replace "runs `deliverylane.Observe(ctx, binding, admission, react)` whose `react` records the fatal
and drains that lane's exact handle" with:
"runs `deliverylane.Observe(runCtx, lane.binding, lane.admission, react)` where `react` only logs the lost lane (`lane`,
cause). Recording is `onFatal` — `s.recordDeliveryOwnerFatal`, passed to `NewAdmission` and run synchronously inside
`Latch` before the result is buffered (`deliverylane.go:78-81`), which is what `DeliveryFatal()` reads — and `Observe`
itself drains the exact handle after `react` returns (`:240-241`); `react` is required non-nil (`:231-232`)."

B3 — design § 2.6, replace "the hand-rolled drained flags (`:712`) go — `Binding` owns drain-once and `Done()` is never
nil, so `stop()` awaits `Closed()` (`:727`) and both-drain-first (`:718`) holds through the package's contract." with:
"`milestoneConsumerOwner` replaces `complete`/`failed` (`:681-682`) and the drained flags (`:683-684`) with two
`*deliverylane.Binding`; `stop()` calls `Drain()` on both (both-drain-first, `:718`, holds; `Drain` is once-only, so a lane
the observer already drained is a no-op), awaits both `Closed()` (`:727`, `:730`), calls `o.cancel()` (`:743`, ending the
observers' `runCtx`), then joins both `Done()`, which is never nil (`deliverylane.go:216`) — the join #1357's design § 6
names as the one draft 4 never specified."

B4 — design § 4 Tier 1 bullet and § 7, and tasks 7.4, replace "one incompatible package" / "one incompatible change in
`natsclient`" with:
"`task api:compat:report` at `b7ce8727` lists 15 incompatible Tier 1 packages against `v1.0.0-beta.162`, `natsclient`
(`NewDurableHandler: removed`) and `agentic/agentrun` (`EntityIDPattern`, `Mint`) among them; this layer adds one line,
`ConsumeWithHeartbeat: removed`, under the already-counted `natsclient` and only compatible additions under
`agentic/agentrun`, so the package count does not move. The commit is still `!`; the posture is still ADR-106's
pre-RC descending count."

B5 — design § 4 spec-delta list, add after item (2):
"(2b) `nats-streaming` REMOVED "Heartbeat consumption SHALL expose settlement failure" (live `:158-181`, with its two
scenarios "transient work fails and delayed NAK fails" and "shutdown NAK fails"). Reason: the requirement's own last
paragraph (`:169`) assigns its deletion to this PR; L0 landed it MODIFIED, not REMOVED, so the removal is owed here."
And in `openspec/changes/agentrun-fanout-settlement/specs/nats-streaming/spec.md`, add under `## REMOVED Requirements`:
"### Requirement: Heartbeat consumption SHALL expose settlement failure
**Reason**: `ConsumeWithHeartbeat`, the only function this requirement binds, is deleted by this change with its last
production caller; the requirement's own text ("This requirement is deleted together with the helper by the PR that
migrates its last caller (#1249)") named this PR. Its scenarios ("transient work fails and delayed NAK fails",
"shutdown NAK fails") describe the deleted helper's NAK paths and go with it. Migrated bindings are governed by
`jetstream-consumer-policy` ("semantic heartbeat settlement has one permanent exported surface") and by
"replay follows the binding's durable authority" in this capability."

B6 — `proposal.md`: replace "**Fatal ownership loss latches health.** A sixth private copy of the admission latch stops a
lane after a fatal outcome, an `InProgress` failure or unavailable delivery metadata; `milestoneConsumerOwner` stays the
sole owner of both consume handles and drains only the failed lane; `MilestoneService.Health()` reports the cause." with
"**Fatal ownership loss latches health.** Each milestone lane is a `internal/deliverylane` consumer (#1341): its
`Admission` closes on a fatal outcome, an `InProgress` failure or unavailable delivery metadata, `Observe` drains only
that lane's exact handle, and `MilestoneService.Health()` reports the cause through `DeliveryFatal()`;
`milestoneConsumerOwner` retains both bindings and stays the sole owner." Replace "new `agentic/agentrun/delivery_owner.go`,"
in Impact with "(no new file: `agentrun` imports `internal/deliverylane`),". Replace the non-goal "No consolidation of the
six admission-latch copies (L1's declared residual)." with "No change to `internal/deliverylane` (#1341 owns it); no
refusal declarer on the two milestone lanes (#1342's sweep, see OQ1)."

B7 — `specs/agent-run-milestones/spec.md`: replace "- **THEN** its exact handle is drained and marked drained, and
`Health()` reports the cause" with "- **THEN** its exact handle is drained once through its `deliverylane.Binding`, and
`Health()` reports the cause"; in the requirement replace "and `stop()` waits for each still-running handle's `Closed`
without a second drain." with "and `stop()` drains each binding at most once, waits for each handle's `Closed`, and joins
each lane's observer."

## C. Checks the brief asked for, answered

- § 2.6 signatures vs `internal/deliverylane/deliverylane.go`: `NewAdmission(onFatal, nil)` matches (`:45-48`);
  `Consume(ctx, msg, policy, admission)` matches but returns `(DeliveryResult, bool)` (R5); `Observe(ctx, binding,
  admission, react)` matches, `react` non-nil and does not drain (R6); `Binding` wraps the exact `ConsumeContext`
  (`:194-196`); `Done()` never nil (`:216`); `stop()` awaits `Closed()` (`:211`) and must join `Done()` (R7); no `Stop()`
  on `Binding` (R8). `Settle` is not used by this design (heartbeat lanes).
- `ConsumeWithHeartbeat` callers on `main` (stderr visible): `agentic/agentrun/agentrun.go:812` + declaration `:84`. R15.
- Spec deltas vs live specs: consumer-policy MODIFIED block restates the live requirement's three scenarios by name (R19);
  nats-streaming REMOVED "shrinking remainder" header matches live `:239`; a second REMOVED is owed (R20);
  `agent-run-milestones` is new (`ls openspec/specs | grep -i -E 'agent-run|milestone'` → 0).
- Tier 1: `release/tier1-packages.txt:40` `agentic/agentrun`, `:57` `natsclient` (also `:69` `pkg/lifecycle`, `:89`
  `service`); report count 15 at HEAD (R18).
- #1155 stage D / Q5: boot roots unchanged, no handler registered, O5 not yet recorded on #1155 (R22).
- Metric name: no collision (R24).
