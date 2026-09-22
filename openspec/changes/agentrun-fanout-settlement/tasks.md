# Tasks: AgentRun milestone fanout settlement

> **Re-pin (2026-09-22): DONE.** The architect re-derived every pin against `main` at `b7ce8727` (L0–L3 and #1341/#1357
> all merged), generating each one from `sed -n "${n}p"` rather than transcribing it: 162 pins at `0053183d` became 214.
> `scripts/inventory-verify.sh openspec/changes/agentrun-fanout-settlement/inventory.md` reads
> `pins=214 ok=214 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. The reconciliation of the accepted design
> against that base — 28 rows, seven text corrections (B1–B7), two owner questions — is `reconciliation.md`; the
> per-task old to new pin map is `tasks-pins.md`. From here `scripts/inventory-verify.sh` is EXPECTED to go RED as the
> implementation lands: pins are pre-change evidence, and a landed change is never re-pinned. **Owner rulings
> 2026-09-22 on #1249 (issuecomment-5773598763, "as recommended"):** OQ1 the milestone lanes keep `nil` `onRefused`
> and are added to #1342's table; OQ2 the forced `handle.Stop()` fallback is dropped. Standing rule from the same day:
> keep complexity as low as possible — an edge case goes to a doc sentence or "not supported" before it gets code.

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof. Landing choreography
(review, archive, merge) lives on the PR checklist (#1230), not here. Pins: every pin is a `main` line at `b7ce8727`;
the L1 attributions are retired. The developer re-derives with `sed -n` any pin the rebase moves.

## 1. Claim and the rebase-base check

- [ ] 1.1 The claim exists: draft PR #1360 against `main` (base `b7ce8727`) with `Closes #1249`, `Closes #759`, and
      `implemented-by: pending (design-phase claim)`. Before the first implementation push, set
      `implemented-by: <persona>` in the body and keep the Tier 1 declaration (§ 7, wording per reconciliation B4: one
      added incompatible line under the already-counted `natsclient`, package count unchanged at 15). There is no L1
      branch to target: L1 is `94cd8e4c` on `main`.
- [x] 1.2 Before any deletion, run `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` at the base; on
      `main` at `b7ce8727` that is two hits, the call site `agentic/agentrun/agentrun.go:812` and the declaration
      `natsclient/heartbeat.go:84` (the grep matches `func ConsumeWithHeartbeat(`). Anything else: stop and report; do
      not migrate it here.
      Evidence 2026-09-22 on the claim branch at `580f531d`, stderr visible, exit 0: exactly the two expected hits,
      `agentic/agentrun/agentrun.go:812` and `natsclient/heartbeat.go:84`. Nothing else matched.

## 2. Identity and the handler contract (O3; #759 ruling item 3)

- [x] 2.1 Add `LoopTerminalEvent.SourceMessageID string`, copied from `agentterminal.Event.SourceMessageID`
      (`internal/agentterminal/terminal.go:67`) at `agentic/agentrun/agentrun.go:586`;
      `test/compat/semteams/agentrun_terminal_compat_test.go:75` stays green.
      Evidence: field is the first member of `LoopTerminalEvent`; copy at the `ev := LoopTerminalEvent{...}` literal in
      `HandleEvent`. `go test -race ./test/compat/...` → `ok github.com/c360studio/semstreams/test/compat/semteams`.
- [x] 2.2 Document handler done on `OnLoopTerminal` (`agentrun.go:490`): return nil only after the durable consequence
      for this identity is committed; every replay presents the same identity.
      Test: `TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt`.
      Evidence: the "Handler done" paragraph on the `MilestoneHandler` doc comment; test in
      `agentic/agentrun/milestone_identity_test.go` replays the same stored bytes twice and pins both attempts to the
      envelope's own `ID()`. Mutation evidence is recorded with 3.8.

## 3. Decision matrix and settlement (design § 2.3–2.4; R1, R2, O1)

- [x] 3.1 Replace the `ConsumeWithHeartbeat` closure (`agentrun.go:810-819`) with `ValidateHeartbeatDeliveryPolicy` per
      lane (`natsclient/delivery_settlement.go:155`) and `deliverylane.Consume`
      (`internal/deliverylane/deliverylane.go:105`) under the `admitted` guard (reconciliation B1) over one
      `DeliveryWork` (`natsclient/delivery_settlement.go:35`, signature unchanged:
      `func(context.Context, []byte) (DeliveryDecision, error)`); decode failure → Terminate (`decode`).
      Evidence: `Start` now validates one `HeartbeatDeliveryPolicy` per lane before acquisition
      (`ValidateHeartbeatDeliveryPolicy(ctx, cfg, milestoneHeartbeatInterval, retry, s.deliveryWork(lane))`) and the NATS
      callback is `consumeLane` in `agentic/agentrun/milestone_settlement.go`: `deliverylane.Consume` then
      `if !admitted { return }` (B1). Decode Terminates with reason `decode`, observed through the lane by
      `TestMilestoneDecodeFailureTerminatesAndCounts` (undecodable bytes Term once, are neither Acked nor Naked, reach
      no handler, and produce exactly one `terminate`/`decode` increment and one decision log line). The checkpoint-1
      review found this row ticked with NO test behind it; the test is the correction. `git grep -n -E
      'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` now returns the declaration only.
- [x] 3.2 Classify resolution on the AgentRun side: `errors.Is` against `ErrEntityNotFound`
      (`pkg/lifecycle/manager.go:205`) → nil run; `ErrEntityNotLifecycleManaged` (`:244`) → Retry (R2);
      `ErrWorkflowNotRegistered` (`:169`) → Quarantine (R1); otherwise `errs.Classify` (`pkg/errs/errs.go:280`):
      Invalid → Terminate, Fatal → Quarantine, else Retry. Covers `agentrun.go:637`, `:641` (Terminate
      `resolution_type`), `:653`, `:657`; the nil-reader error (`manager.go:198`) and projection failures (`:250`)
      settle as bounded Retry. `pkg/lifecycle` is not edited.
      Evidence: `classifyResolutionFailure` in `milestone_settlement.go` — sentinels first, then `semerrs.Classify`.
      `resolveRunForEvent` answers `(nil, nil)` only for `ErrEntityNotFound` and returns every other error.
      `pkg/lifecycle` is untouched (`git diff --stat pkg/lifecycle` empty).
- [x] 3.3 Wrap `errs.WrapInvalid` (`errs.go:435`) at the AgentRun-side origins: `agentrun.go:393`, `:405`, `:436`,
      `:455`, `:462`, `nats_reader.go:68` (chains kept, no signature change).
      Evidence: the five `ResolveRun` origins were wrapped in the first pass; `nats_reader.go`'s non-string value was
      NOT, and the checkpoint-1 review caught it (fixed in `f6ca4ca5`). Unwrapped it reached `errs.Classify`'s unknown
      default and settled Retry / `resolution_transient` — against `design.md:68`, the delta
      (`specs/agent-run-milestones/spec.md:20-22`) and I7 — and because `errs.IsTransient` places an unclassified error
      by SUBSTRING first, the disposition also turned on whether the interpolated entity ID happened to contain
      "network" or "timeout".
      The class sweep, every error origin that can reach `classifyResolutionFailure`, as `site → class it carries →
      design row` (line numbers on this branch after the fix):
      `nats_reader.go:50` nil exact reader → unclassified, `Classify` default Transient → the unmatchable row
      (`design.md:63`): deterministic but carrying no sentinel and no class, and unreachable through
      `NewNATSLoopTripleReader`, which always supplies a reader. Left unwrapped: I7 Terminates only failures that are
      deterministic AND matchable, and this one is unmatchable.
      `nats_reader.go:59` exact-read failure → `%w`, forwards the graph reader's own class → the forwarding row
      (`design.md:64`): Invalid→Terminate, Fatal→Quarantine, else Retry.
      `nats_reader.go:75` non-string value → `errs` Invalid (WRAPPED HERE) → `design.md:68` Terminate
      `resolution_invalid`.
      `agentrun.go:399`, `:410`, `:438` entity-ID grammar → Invalid (wrapped) → `design.md:68`.
      `agentrun.go:452` parent is not a loop entity, `:461` hop bound → Invalid (wrapped) → `design.md:68`.
      `agentrun.go:405`, `:432` triple reads → `%w` forward → `design.md:64`.
      `agentrun.go:414`, `:443`, `:751` `Manager.Get` → `%w` forward or returned as-is, carrying the lifecycle
      sentinels → the `ErrEntityNotFound` / `ErrEntityNotLifecycleManaged` / `ErrWorkflowNotRegistered` / projection
      rows (`design.md:60`, `:61`, `:62`, `:65`).
      `agentrun.go:760` terminal names no run and no loop → Invalid (wrapped) → `design.md:67`.
      `milestone_settlement.go:139` `asAgentRun` → Invalid over the `errUnexpectedRunType` sentinel →
      `design.md:66` Terminate `resolution_type`.
      So after the fix every origin a design row classifies Invalid is wrapped, and the only unwrapped origins are the
      `%w` forwards the design requires to forward plus the one unmatchable nil-reader guard. Chains kept via `%w`; no
      signature changed.
      Mutation evidence for the wrap (`cp` backup + md5, `[applied]` printed between mutating and testing, md5
      re-checked after restore): `agentic/agentrun/nats_reader.go` md5 `9a1b3740172b4a7c9eadb8ccf4dd61ce` before and
      after. Mutant G, the `semerrs.WrapInvalid` removed so the site returns the bare `fmt.Errorf` again:
      `TestMilestoneResolutionInvalidTerminates/non-string_predicate_value` went red with
      `expected: "resolution_invalid" / actual: "resolution_transient"` and `terms = 0`, while every other case in that
      table and `TestMilestoneResolutionFatalQuarantines` stayed green — so the test kills exactly this defect and
      reaches the production site.
- [x] 3.4 The dead identity guard (`agentrun.go:646-647`) Terminates with cause
      `errs.WrapInvalid(errors.New("terminal names no run and no loop"), "agentrun", "HandleEvent", "resolve")`, never
      nil (`interpretDeliveryWork`, `natsclient/delivery_settlement.go:403`/`:407`).
      Evidence: the `ev.LoopID == ""` branch of `resolveRunForEvent` returns the ruled cause verbatim; its decision is
      `resolution_invalid` through the same classifier, so no dedicated branch exists for it.
- [x] 3.5 Every attempt runs every handler in registration order under the per-handler recover (`agentrun.go:605`,
      `:612`); collect outcomes; aggregate fatal > transient > invalid > Ack (O1). Tests:
      `TestMilestoneAggregateIsPureOverOrderedOutcomes` (rapid property, I4),
      `TestMilestoneFanoutAcksOnlyWhenEveryHandlerReturnsNil` (I1),
      `TestMilestoneFanoutRetriesOnTransientHandlerError`, `TestMilestoneFanoutTerminatesOnAllInvalid` (O1),
      `TestMilestoneFanoutQuarantinesOnHandlerPanic` (I3).
      Evidence: `aggregateMilestoneOutcomes` + `classifyHandlerOutcome` in `milestone_settlement.go`; the fanout loop is
      in `decide`, each handler under `invokeHandler`'s recover. Tests in
      `agentic/agentrun/milestone_settlement_internal_test.go` and `milestone_aggregate_prop_test.go`, all green under
      `-race`; the property carries `// spec: agent-run-milestones / milestone fanout settles as one replay-safe unit`
      and `task spec:properties` moved 287/287 to 288/288.
- [x] 3.6 Resolution tests: `TestMilestoneNotManagedEntityRetriesThenAcksAfterCreate` (I7/R2: Retry on attempt 1,
      `Manager.Create`, Ack on attempt 2), `TestMilestoneUnregisteredWorkflowQuarantinesAndLatches` (R1),
      `TestMilestoneResolutionInvalidTerminates` (grammar, non-string value, non-`*AgentRun`),
      `TestMilestoneNilReaderAndProjectionFailuresRetry`.
      Evidence: all four tests green. `TestMilestoneUnregisteredWorkflowQuarantinesAndLatches` and the nil-reader half of
      `TestMilestoneNilReaderAndProjectionFailuresRetry` drive the REAL `lifecycle.NewManager`, so their errors are
      production values, not strings a test invented. Each asserts the decision REASON as well as the settlement method,
      because four rows terminate and three retry.
      Corrected by the checkpoint-1 review: `TestMilestoneResolutionInvalidTerminates`'s non-string case handed
      `stubTripleReader` an error the TEST had pre-wrapped Invalid, under a comment claiming it was the class
      `NATSLoopTripleReader` assigns — a reconstruction that could not fail. It now builds the REAL
      `NATSLoopTripleReader` over the REAL `graph.ExactEntityReader`, faking only the NATS request, and reads a real
      authority reply whose `agent.loop.run` object is a number.
      Added: `TestMilestoneResolutionFatalQuarantines` for the `resolution_fatal` row, which no test reached. Its class
      comes from the production seam — an exact authority reply with no entity is `errs` Fatal
      (`graph/exact_entity.go:97`) and `getStringTriple` forwards the class — and the row Quarantines, settles nothing,
      and latches the lane.
- [x] 3.7 Log and counter on every non-Ack decision (I5): one line with `source_message_id`, `loop_id`, `category`,
      `lane`, `reason`; one increment. Test: `TestMilestoneNonAckDecisionsLogOnceAndCountOnce`.
      Evidence: `observeDecision` emits one `slog.Warn` with the five fields and one
      `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}` increment, both inside the `DeliveryWork`
      (B1). The vec is built in `NewMilestoneSubscriberWithRunStateReader` so an unregistered increment is a local
      no-op; `RegisterMetrics` and the root wiring stay with task 5.2.
- [x] 3.8 Mutation evidence for the wiring, not the primitive: `cp` `agentrun.go` aside; delete the aggregate call so
      the closure returns Ack unconditionally; run 3.5's tests and record the failing names; restore and `shasum` the
      restored file against the backup. Then delete the `SourceMessageID` copy at `:586` and record 2.2's failure the
      same way.
      Evidence (`cp` backup + md5, `[applied]` printed between mutating and testing, md5 re-checked after restore):
      `agentic/agentrun/agentrun.go` md5 `9a09f96f5f67f05270a393abe3126aa1` before and after BOTH mutants. Every md5 in
      3.8 and 4.2 is the file at the commit its mutant ran on — A/B/C at `47bf9c83`, D/E1 at `79c2c0bc`, E2/F at
      `6bbbb7f7` — so `git show <rev>:<file> | md5` reproduces each one from the branch.
      Mutant A, `switch aggregateMilestoneOutcomes(outcomes)` to `switch outcomeDone` (the aggregate call deleted, so the
      work always returns Ack): `TestMilestoneFanoutAcksOnlyWhenEveryHandlerReturnsNil`,
      `TestMilestoneFanoutRetriesOnTransientHandlerError`, `TestMilestoneFanoutTerminatesOnAllInvalid`,
      `TestMilestoneFanoutQuarantinesOnHandlerPanic`, `TestMilestoneNonAckDecisionsLogOnceAndCountOnce` and
      `TestSubscriber_PanicGuard_SecondHandlerRunsAfterFirstPanics` all went red.
      Mutant B, the `SourceMessageID: normalized.SourceMessageID,` copy deleted from the `ev` literal:
      `TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt` and
      `TestMilestoneNonAckDecisionsLogOnceAndCountOnce` went red (2.2's evidence).
      Mutant C checks the check: swapping `outcomeTransient` and `outcomeInvalid` in the ordinal (md5
      `8a1613dfb62ae4a70dd2cc68d66f6c3e` on `milestone_settlement.go` before and after) killed the rapid property after
      one test, `aggregate([2 1]) = 2, want 1 by the requirement's precedence` — so the property can fail.

## 4. Admission latch and handle owner (design § 2.6)

- [x] 4.1 Per lane, consume `internal/deliverylane` instead of copying the latch (there is no
      `agentic/agentrun/delivery_owner.go`; `processor/agentic-loop/delivery_owner.go` was deleted at `b7ce8727` by
      #1357): `deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)` (`internal/deliverylane/deliverylane.go:45`),
      `deliverylane.Consume` (`:105`), `deliverylane.NewBinding(handle)` (`:194`),
      `deliverylane.Observe(runCtx, binding, admission, react)` (`:225`; `react` log-only, non-nil).
      `recordDeliveryOwnerFatal` keeps its `*MilestoneSubscriber` receiver as the `onFatal` feeding `DeliveryFatal()`.
      Evidence: `agentic/agentrun/milestone_settlement.go` — `observeLane` is the one place a raw
      `jetstream.ConsumeContext` becomes a `deliverylane.Binding`, and it starts the lane's observer with a log-only
      non-nil `react`. `Start` builds `deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)` per lane (OQ1: `nil`
      `onRefused` as ruled). `recordDeliveryOwnerFatal` and `DeliveryFatal()` are on `*MilestoneSubscriber`. No
      `agentic/agentrun/delivery_owner.go` exists.
- [x] 4.2 Replace the hand-rolled handle state in `milestoneConsumerOwner` (`agentrun.go:679-689`) with two
      `*deliverylane.Binding`: the two `jetstream.ConsumeContext` fields (`:681-682`) and the two drained flags
      (`:683-684`) go, and no observer goroutine is hand-rolled — `Observe` owns it. `stop()` = `Drain()` both
      (both-drain-first, `:718`, holds) → await both `Closed()` (`:727`, `:730`) → `o.cancel()` (`:743`) → join both
      `Done()` (`internal/deliverylane/deliverylane.go:216`); reconciliation B3. The force `Stop()` fallback
      (`:734-737`) is removed and no raw handle is kept beside the binding — owner ruling 2026-09-22 on #1249
      (OQ2, as recommended). Tests (`-race`):
      `TestMilestoneFatalDrainsOnlyTheFailedLane`, `TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain`.
      Evidence: `milestoneConsumerOwner` now holds two `*deliverylane.Binding` and nothing else per lane — the two
      `jetstream.ConsumeContext` fields and both drained flags are gone, and no observer goroutine is hand-rolled.
      `stop()` drains both, awaits both `Closed()` through `waitMilestoneLane`, calls `o.cancel()`, then joins both
      `Done()`. The forced `handle.Stop()` fallback is gone (OQ2). Tests green under `-race`:
      `TestMilestoneFatalDrainsOnlyTheFailedLane`, `TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain`; the
      fake handle's `Stop()` panics, so a reintroduced force-stop fails loudly.
      Per-lane wiring mutation evidence (`cp` backup + md5 before, `[applied]` printed, md5 re-checked after restore;
      `milestone_settlement.go` md5 `9edc1ff9f4c1fc648f45e39af88ef99e` before and after E2 and F,
      `87a11e48dd0b326b9c2d24b12e195c67` before and after D and E1):
      D, the `deliverylane.Observe` call deleted from `observeLane` — `TestMilestoneFatalDrainsOnlyTheFailedLane`,
      `TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain` and
      `TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner` all hang on the drain that never comes
      and the run panics on its 30s deadline naming exactly those three.
      E1, `recordDeliveryOwnerFatal` records nothing — `TestMilestoneFanoutQuarantinesOnHandlerPanic`,
      `TestMilestoneUnregisteredWorkflowQuarantinesAndLatches`, `TestMilestoneFatalDrainsOnlyTheFailedLane`,
      `TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner` went red.
      E2, `newLaneAdmission` passes a nil `onFatal` — the same four went red. E2 only became detectable after both
      lanes and the owner test were moved onto the one `newLaneAdmission` seam: before that the tests built their own
      admission, so `Start` could have passed nil and nothing would have noticed.
      F, the `if !admitted { return }` guard dropped from `consumeLane` —
      `TestMilestoneRefusedDeliveryIsNotLoggedAsASettlementFailure` went red. That test is an addition beyond the
      task's named list; without it the B1 guard had no coverage at all, because nothing drove `consumeLane`.
      `task api:compat:report` at this head (base `v1.0.0-beta.162`, 62 compared, 15 incompatible, exit 0):
      `agentic/agentrun` incompatible set is unchanged (`EntityIDPattern`, `Mint`) and this layer adds only
      `(*MilestoneSubscriber).DeliveryFatal: added` and `LoopTerminalEvent.SourceMessageID: added` under Compatible
      changes; `natsclient` still reads `NewDurableHandler: removed` alone, with `ConsumeWithHeartbeat: removed` owed
      by section 7. The package count does not move, exactly as reconciliation B4 declares.
- [x] 4.3 Control-plane rows: an `InProgress` failure (`natsclient/delivery_settlement.go:372`/`:377`) and unavailable
      metadata (`:390`) latch the lane and surface in health:
      `TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner`.
      Evidence: the metadata row is proven —
      `TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner` green under `-race`: nothing is settled,
      no handler runs, only the exact lane drains, that lane stops admitting, and `DeliveryFatal()` carries
      `delivery_metadata_unavailable`.
      The `InProgress` row is NOT proven, and the checkpoint-1 review corrected this line: it claimed
      `natsclient/delivery_settlement_test.go` owns the InProgress-to-owner-stop mapping, and
      `grep -n InProgress natsclient/delivery_settlement_test.go` returns nothing (exit 1, stderr visible). The only
      unit test that fails `InProgress` is `natsclient/heartbeat_test.go` (`inProgressErr` at `:40`/`:103`, used by
      `TestConsumeWithHeartbeat_ReturnsErrorOnInProgressFailure`, `..._CancelsWorkOnInProgressFailure`,
      `..._JoinsCleanupErrorOnInProgressFailure`) and it exercises the LEGACY `ConsumeWithHeartbeat`, which task 7.3
      deletes — so `natsclient/delivery_settlement.go:372-378` (`ownerStopNeeded = true` on a failed lease renewal) has
      no coverage on the typed path today. What holds by construction is only that the row reaches the owner through
      the same `DeliveryResult.OwnerStopRequired` seam the admission latches on, which the metadata test does exercise.
      7.3 now carries the port item that closes it. `MilestoneService.Health()` itself stays with task 5.1.

## 5. Health and metrics (O3; design § 2.7)

- [x] 5.1 `MilestoneSubscriber.DeliveryFatal() error` LANDED in checkpoint 1 (task 4.1/4.2, on `*MilestoneSubscriber`
      in `milestone_settlement.go`, and listed by `task api:compat:report` under Compatible changes); only the
      `Health()` override below remains. `MilestoneService.Health()` override (the
      `service/base.go:209` pattern) type-asserts `interface{ DeliveryFatal() error }` on the `milestoneStarter`
      (`service/milestone_service.go:21-22`, unchanged) and returns `health.NewUnhealthy("milestone", …)`.
      Test: `TestMilestoneServiceHealthReportsDeliveryFatal`, observed through `/health`
      (`service/service_manager.go:1302` → `:1722` → `:1736`).
      Evidence: `(*MilestoneService).Health()` in `service/milestone_service.go` asserts the named
      `deliveryFatalReporter` interface on `s.subscriber` and answers `health.NewUnhealthy(s.Name(), …)` carrying the
      latched cause; `milestoneStarter` is unchanged, so a lifecycle-only double is still a valid starter.
      `TestMilestoneServiceHealthReportsDeliveryFatal` in `service/milestone_health_test.go` reads the REAL aggregate —
      `Manager.handleSystemHealth` over a registered `MilestoneService` — and asserts both halves: an owned lane is
      healthy at HTTP 200, and a latched lane is not healthy, answers 503, and carries the cause string.
      `TestMilestoneServiceHealthReadsTheProductionSubscriber` pins the assertion against a real
      `agentrun.NewMilestoneSubscriber`, because the `!ok` arm falls back to the base status: a renamed or resigned
      `DeliveryFatal` would otherwise turn the whole signal off silently and no other test would notice.
      **Flake found and fixed (2026-09-22).** `TestMilestoneServiceHealthReportsDeliveryFatal` was red on CI run
      35719826852 (Test job), `a latched lane reports unhealthy with its cause` at
      `service/milestone_health_test.go:88` ("precondition: healthy before the latch"). Reproduced on this branch:
      green at `-count=1`, red inside `go test -count=60 -run 'TestMilestoneServiceHealthReportsDeliveryFatal$'
      ./service/`, both subtests, message `Service is unhealthy (failed checks: 0)`.
      The cause is a deliberate substrate contract, not a substrate defect: a running `BaseService` is unhealthy
      until its monitor's FIRST check completes — `Start` leaves `healthy` at its atomic zero value
      (`service/base.go:266`) and `performHealthCheck` stores the first `true` on the monitor goroutine
      (`service/base.go:446`) — and `/readyz` deliberately reports NOT READY until then
      (`service/service_manager.go:1838` reads `IsHealthy()`). The test was reading `/health` before that edge.
      The "store `healthy = true` in `Start`" remedy was MEASURED and REFUTED before the test fix was chosen, which
      is the evidence for why the fix belongs in the test: applied as a mutant (`service/base.go` md5
      `caba89a154827cfca534d9b924c4709d` before and after), `go test ./service/` went from green to three failures —
      `TestReadinessWaitsForInitialServiceHealthObservation` (`startup_observability_test.go:305`, `/readyz` answered
      200 where it requires 503 while the first check is blocked), `TestReadinessIncludesHealthyNonLifecycleDiscoverables`
      (`:223`) and `TestStartAllBindsSharedAndMetricsBeforeBlockedService` (`:570`), the last two because the store
      removes the false->true edge their callbacks fire on. Coordinator ruling 2026-09-22: the substrate
      contract stays; `service/base.go` is untouched. (No owner artifact stands behind it; the substrate decision
      was the coordinator's, and it is recorded as that.)
      The fix is `awaitFirstHealthObservation` in `service/milestone_health_test.go`: both subtests register
      `svc.OnHealthChange` BEFORE `Start` — the same exported seam
      `TestReadinessWaitsForInitialServiceHealthObservation` uses — and block on that edge, with a bounded failsafe
      that calls `t.Fatal` so a monitor that never observes health is loud rather than a pass. No `Eventually`, no
      `time.Sleep`, no poll.
      Evidence: `go test -race -count=200 -run 'TestMilestoneServiceHealthReportsDeliveryFatal$' ./service/` ->
      `ok github.com/c360studio/semstreams/service 1.844s`, exit 0.
      Mutation evidence K: both `awaitHealthObserved()` calls deleted, the `OnHealthChange` registration left in
      place (`service/milestone_health_test.go` md5 `42455974ad3bb1fac89228f142d581a0` before and after) — the same
      `-race -count=200` command exited 1, with 10 of 200 iterations failing `a latched lane reports unhealthy with
      its cause` ("precondition: healthy before the latch") and 7 failing `owned lanes stay healthy` ("an owned lane
      must not report a delivery fatal: Service is unhealthy (failed checks: 0)").
      The same mutant on two other machines, recorded beside that one because they do not agree: the reviewer
      measured 53 of 200 iterations red at `-race -count=100 -cpu 2,4`; the coordinator measured 0 of 200 at the
      default CPU count and 0 of 200 at `-cpu 2,4`. The kill rate is a property of the host, not of the test, so
      the wait is judged on the EDGE it synchronizes on — `service/base.go:448` stores `healthy` and `:451` fires
      the callback after it, which is the store the `/health` read depends on — and never on a rate. A 0/200 arm
      is not evidence the mutant is harmless; it is evidence that machine never lost the race.
- [x] 5.2 Add `MilestoneSubscriber.RegisterMetrics(r metric.MetricsRegistrar) error` registering
      `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}` via `RegisterCounterVec`
      (`metric/registry.go:216`); the vec is built in `NewMilestoneSubscriber` (`agentrun.go:521`). Wire one call after
      construction in `cmd/semstreams/main.go:347` and `cmd/e2e-semstreams/main.go:272` (`metricsRegistry` in scope:
      `:170`, `:159`). Not `Service.RegisterMetrics` (`service/base.go:389`; nothing calls it).
      Test: `TestMilestoneDecisionsCounterIsRegisteredOnce`.
      Evidence: `(*MilestoneSubscriber).RegisterMetrics` in `agentic/agentrun/agentrun.go` calls
      `r.RegisterCounterVec("agentrun", "milestone_decisions_total", s.decisions)` and REFUSES a nil registrar with an
      `errs` Invalid — accepting nil would answer "registered" to a root holding no registry and leave exactly the
      silence the method removes (`TestMilestoneRegisterMetricsRefusesANilRegistrar`).
      `TestMilestoneDecisionsCounterIsRegisteredOnce` (`agentic/agentrun/milestone_metrics_test.go`) drives real
      deliveries through the production lane fixture and reads the counts back through
      `registry.PrometheusRegistry().Gather()` — where a `/metrics` scrape reads — not off the vec the subscriber
      holds; it also asserts a repeat registration is a no-op that keeps counting on the SAME series rather than
      forking a second one. `TestMilestoneDecisionsCounterCarriesTheRuledIdentity` pins the ruled name and the
      `{lane,decision,reason}` label set (Q3, 2026-09-18).
      Root wiring: both roots call one `registerMilestoneService(manager, svcDeps, natsClient, metricsRegistry,
      platform, logger)`, which constructs the subscriber, registers the counter, and registers the service. The
      previous inline block pushed `cmd/e2e-semstreams/main.go`'s `run()` to 82 statements against revive's 80
      (`task lint` red); extracting the function both fixes that and gives the per-root wiring a test —
      `TestRegisterMilestoneServicePublishesTheDecisionsCounter`, present in BOTH `cmd/semstreams` and
      `cmd/e2e-semstreams`, which asserts the service reached the `ServiceManager` and that
      `metricsRegistry.Unregister("agentrun", "milestone_decisions_total")` answers true.
- [x] 5.3 Mutation evidence: delete the `RegisterMetrics` call in one root; record the failure; restore and
      checksum. It ticks on mutant J below, NOT on the e2e stage the task named.
      **The task's own detector could not fail.** As written it says "run the e2e `verify-streaming-metrics`
      stage", and that stage reads `semstreams_agentic_model_stream_chunks_total` and
      `..._stream_ttft_seconds_count` — neither of them a series `RegisterMetrics` publishes. The refuting
      measurement: `git grep -n milestone_decisions_total 884b8600 -- test/e2e/` returns nothing, exit 1. No e2e
      assertion read the counter at all, so deleting the call would have left the tier green and the mutant would
      have been recorded as "survived" against a test that was never looking.
      Mutant J is the same fault — a composition root wired without its metrics registration — observed where it IS
      detectable: the e2e root's `subscriber.RegisterMetrics(metricsRegistry)` call deleted, that root's
      `TestRegisterMilestoneServicePublishesTheDecisionsCounter` red and `cmd/semstreams`' green. Existing evidence
      covering the same fault is what the discipline asks for here rather than a repeated experiment
      (`docs/contributing/01-testing.md:109-111`).
      After review M1 the tier DOES read the counter: `verify-milestone-exhaustion` waits for
      `semstreams_agentrun_milestone_decisions_total{lane="complete",decision="retry",reason="handler_transient"}`
      = 5 (`test/e2e/scenarios/agentic/stage_d_milestone_settlement.go`). That assertion is the e2e detector this
      task originally wanted, and it is the target for any future e2e run of this mutant. It was NOT run as a
      mutant: § 9's budget is one tier run, spent on the green re-run recorded in 10.2.
      All three in-tree mutants used a `cp` backup, printed `[applied]` between mutating and testing, and re-checked
      the md5 after restoring.
      H, `MilestoneService.Health()` reduced to `return s.BaseService.Health()` so the override never consults the
      latch (`service/milestone_service.go` md5 `d4e05bdffc4d975c22dd58d5b1be22bc` before and after) —
      `TestMilestoneServiceHealthReportsDeliveryFatal/a_latched_lane_reports_unhealthy_with_its_cause` went red on
      both assertions: `/health` answered 200 instead of 503, and the message read "Service operating normally"
      instead of the cause. A first attempt at this mutant (nil the read, keep the branch) did not compile and was
      DISCARDED, not recorded: a mutant that does not build tests nothing.
      I, `RegisterMetrics` returning nil without calling `RegisterCounterVec`, the primitive
      (`agentic/agentrun/agentrun.go` md5 `88a3e888bfa6eaaf65f052b536888d08` before and after) —
      `TestMilestoneDecisionsCounterIsRegisteredOnce` went red with the gathered series map EMPTY where it expects
      one `decision=retry lane=complete reason=handler_transient` series, and BOTH roots'
      `TestRegisterMilestoneServicePublishesTheDecisionsCounter` went red too.
      J, the wiring rather than the primitive: the `subscriber.RegisterMetrics(metricsRegistry)` call deleted from
      `registerMilestoneService` in the e2e root ONLY (`cmd/e2e-semstreams/main.go` md5
      `57e421afc9b34a7e2cdf83eaf4aed130` before and after) — `cmd/e2e-semstreams`'s wiring test went red and
      `cmd/semstreams`'s stayed green, so the guard is per-root and a half-copied root cannot hide behind the other.

## 6. Consumer policy (O2; design § 2.8)

- [x] 6.1 Keep `MaxDeliver 5` (`agentrun.go:832`) and `AckWait 30s` (`:833`) on both lanes; heartbeat 10s under the
      `AckWait/2` ceiling (`natsclient/delivery_settlement.go:181`); semantic retry
      `DelayedDeliveryRetry(30*time.Second)`. Test: `TestMilestoneLanesDeclareFiniteMaxDeliver` (I6, reads both consumer
      configs; fails on 0).
      Evidence: nothing in the policy changed — both `StreamConsumerConfig` literals in `Start` still declare
      `MaxDeliver: 5` and `AckWait: 30 * time.Second`, `milestoneHeartbeatInterval` is still 10s, and
      `milestoneRetryDelay` is still 30s. What was missing was the guard, and checkpoint 1's coverage did not supply
      it: `milestonePolicyFor` asserts `MaxDeliver 5` / `AckWait 30s` on a config the TEST writes, so `Start` could
      have declared 0 on both lanes with every unit test green.
      The guard is `TestIntegration_MilestoneLanesDeclareFiniteMaxDeliver` in
      `agentic/agentrun/milestone_policy_integration_test.go`. It starts the real subscriber against a testcontainer
      NATS, then reads BOTH lanes' `ConsumerInfo.Config` back out of JetStream and asserts `MaxDeliver` is positive
      (the "fails on 0" half, spelled as its own assertion because 0 means unlimited, not zero), that it is exactly 5,
      that `AckWait` is 30s, and that `BackOff` is empty — the retry delay is semantic, not a consumer ladder. The
      name carries the file's `TestIntegration_` prefix; the task names it `TestMilestoneLanesDeclareFiniteMaxDeliver`.
      The heartbeat ceiling needs no separate assertion: `Start` runs `ValidateHeartbeatDeliveryPolicy` for each lane
      BEFORE acquiring its consumer, so a heartbeat above `AckWait/2` fails Start, and the test's successful Start is
      that check passing on both lanes.
      The `DelayedDeliveryRetry(30s)` half was proven in checkpoint 1 and stands:
      `TestMilestoneFanoutRetriesOnTransientHandlerError` asserts the Nak carries `milestoneRetryDelay` and not a
      line-rate redelivery.
      Mutation evidence (`cp` backup + md5, `[applied]` printed between mutating and testing, md5 re-checked after
      restore; `agentic/agentrun/agentrun.go` md5 `88a3e888bfa6eaaf65f052b536888d08` before and after). K, the drift
      I6 exists to catch: `MaxDeliver: 5` changed to `0` in BOTH `Start` literals — the new test went red on both
      lanes, on both the positivity assertion and the exact-value one, reading `-1` because that is what JetStream
      stores for unlimited. The whole unit suite (`go test -race ./agentic/agentrun/`) stayed GREEN under the same
      mutant, which is the measurement that justifies the test: the pre-existing coverage could not see this.
- [x] 6.2 No new exhaustion signal: the existing advisory counter (`internal/maxdelivery/observer.go:148`) is asserted
      by 9.2.
      Evidence: this layer adds no exhaustion signal of its own. `git grep -n 'max_delivery_exhaustions'` finds the
      counter only under `internal/maxdelivery/` and its own tests plus the e2e stage, and both roots already start
      the observer (`maxdelivery.Start` at `cmd/semstreams/main.go:232`, `cmd/e2e-semstreams/main.go:184`) before the
      milestone service is registered, so the lanes' MAX_DELIVERIES advisories are observed by the existing seam. The
      assertion on the series value belongs to 9.2 with the § 9 proof.

## 7. Removal of `ConsumeWithHeartbeat` (Tier 1; `Closes #759`)

- [x] 7.1 Delete the function (`natsclient/heartbeat.go:84`) and `nonCancellationWorkError` (`:37`); the doc comment at
      `:75-83`, which names #1249 as the deleting PR, goes with the function (there is no `Deprecated:` marker on
      `main`). Keep `ErrHeartbeatFailed`, `PermanentDeliveryError`, `TerminateDelivery`; rewrite the
      `PermanentDeliveryError` doc (`:18`) to "a binding maps it to `DeliveryDecisionTerminate`".
      Evidence: `natsclient/heartbeat.go` is now 28 lines — `ErrHeartbeatFailed`, `PermanentDeliveryError`,
      `TerminateDelivery`, and nothing else. `ConsumeWithHeartbeat`, its doc comment and `nonCancellationWorkError`
      are gone, and the import block collapsed to `import "errors"` (the file no longer touches `context`, `fmt`,
      `log/slog`, `time`, or `jetstream`). The `PermanentDeliveryError` doc now reads "a binding maps it to
      DeliveryDecisionTerminate rather than retrying a message no redelivery can fix"; it named the deleted helper
      before. No `Deprecated:` marker existed to remove, matching #759's no-deprecation ruling.
- [x] 7.2 Invert the ratchet: exact-declaration (`natsclient/consumer_policy_callsite_test.go:428`) and exact-caller-set
      (`:446`) assert absence in every package, in the `NewDurableHandler` retirement shape (`:395`); keep the surface
      guard (`:425`).
      Evidence: `TestLegacyHeartbeatProductionCallZeroGrowthStagingGuard` — which pinned the EXACT declaration
      signature at `natsclient/heartbeat.go` and an exact caller set — is replaced by
      `TestConsumeWithHeartbeatHasNoDeclarationOrProductionCalls`, which asserts zero violations and zero direct calls
      across every production package, in the `NewDurableHandler` retirement shape. The exemption that made the
      inversion necessary was inside the scanner, not the test: `scanLegacyHeartbeatReferences` recorded a violation
      for a `ConsumeWithHeartbeat` `FuncDecl` only when `declaration.Recv != nil || parsed.rel !=
      "natsclient/heartbeat.go"`. That clause is deleted, so any declaration anywhere is now a violation, and a
      "function declaration" case was added to `TestLegacyHeartbeatGuardRejectsAlternateExportedSurface` to prove the
      scanner catches the plain re-addition and not only the alias forms. The surface guard is kept intact:
      `TestLegacyHeartbeatGuardRejectsTakingOrAliasingSymbol`, `...RejectsAlternateExportedSurface`,
      `...CountsDotImportAsDirectCall`, `...IgnoresUnrelatedSelector` all still run and pass.
      Repo-wide reference sweep, stderr visible: `git grep -n 'ConsumeWithHeartbeat' -- '*.go'` — the pathspec goes
      AFTER the pattern, because `--include=` is git-grep-fatal (`option '--include=*.go' must come before
      non-option arguments`, exit 128), and a recorded command nobody can re-run is not evidence. Re-run, exit 0:
      20 hits in 3 files, none a declaration and none a call. 18 are the ratchet
      `natsclient/consumer_policy_callsite_test.go` — its scanner comparison literals at `:96`-`:164`, its
      failure-message format strings at `:431`/`:434`, the ratchet test's OWN name at `:417`/`:428`, and the
      synthetic `source:` fixtures at `:442`-`:516` the scanner must reject. The other two are the ported test's
      provenance note at `natsclient/delivery_settlement_test.go:326` and `natsclient/mockmsg_test.go:17`'s.
      Every one is a statement ABOUT the removal, not a use. Outside Go: `docs/operations/` and
      `openspec/specs/` are 8.1/8.3's work; `docs/adr/070-gated-dag-durable-dispatch.md` (3),
      `docs/proposals/` (4) and `openspec/changes/archive/` are historical records of decisions taken when the helper
      existed and are deliberately left as written.
- [x] 7.3 Delete `natsclient/heartbeat_test.go` (407 lines) and `heartbeat_integration_test.go` (133) after porting any
      claim without a twin in `delivery_settlement_test.go` / `delivery_settlement_integration_test.go`; list the
      ported claims in the PR body. One port is already known and is NOT optional: the InProgress-failure → owner-stop
      case has no twin. Before deleting `heartbeat_test.go`, port
      `TestConsumeWithHeartbeat_ReturnsErrorOnInProgressFailure`, `..._CancelsWorkOnInProgressFailure` and
      `..._JoinsCleanupErrorOnInProgressFailure` (`heartbeat_test.go:283`, `:304`, `:354`; the `inProgressErr` mock at
      `:40`/`:103`) to the typed path as a `ConsumeDeliveryWithHeartbeat` test — proposed name
      `TestConsumeDeliveryWithHeartbeatInProgressFailureRequiresOwnerStop` — asserting that a failed lease renewal
      cancels the work, joins `ErrHeartbeatFailed`, and returns a result with `OwnerStopRequired()` true
      (`delivery_settlement.go:372-378`). Task 4.3's matrix row depends on it.
      Evidence: the port landed FIRST, as `TestConsumeDeliveryWithHeartbeatInProgressFailureRequiresOwnerStop` in
      `natsclient/delivery_settlement_test.go`, and carries all three deleted claims in one test because each alone
      permits the defect the other two catch: the work is cancelled by the renewal failure (nothing cancels the
      owner's context), `ControlError()` carries `ErrHeartbeatFailed`, "failed to send InProgress" and the renewal
      cause, `Err()` retains the work's cleanup error, `OwnerStopRequired()` is true, and no terminal method is
      attempted. Before it, `ErrHeartbeatFailed` was ASSERTED ONLY by the three deleted tests and one deleted
      integration test, but the sweep behind that claim was miscounted:
      `git -C . grep -n ErrHeartbeatFailed 3a1ede7a^ -- '*_test.go'` returns FIVE hits on the pre-deletion tree,
      not four. Four assert it and are in the two files this task removes — `heartbeat_test.go:346`, `:372`, `:387`
      and `heartbeat_integration_test.go:118`, each a `require.ErrorIs(t, err, ErrHeartbeatFailed)`. The fifth,
      `processor/agentic-tools/outcome_metrics_test.go:79`, CONSTRUCTS the sentinel as input
      (`errors.Join(natsclient.ErrHeartbeatFailed, errors.New("lost"))` fed to `recordHandlerError`) and asserts
      nothing about it: its assertions are on the `cause=heartbeat` metric label and the log line. It keeps the
      symbol compiling, never pinned, so the conclusion stands — deleting the two files without the port would have
      left `ErrHeartbeatFailed` with no assertion anywhere.
      Twin analysis for the rest of the two files, so the deletion is not a silent coverage drop. `SurfacesSettlementErrors`,
      `_AcksOnSuccess`, `_NaksWithDelayOnWorkError`, `TermsPermanentWorkError`, `_NaksOnContextCancel` → the typed
      truth tables `TestConsumeDeliveryWithHeartbeatValidDecisionTruthTable` and `TestSettleDeliveryDecisionTruthTable`,
      which cover every decision AND every settlement-method failure. `_SendsInProgressBeforeAckWait`,
      `_FastWorkNoHeartbeat` → `TestIntegrationConsumeDeliveryWithHeartbeatHealthyRenewalPreventsOverlap`.
      `_RetainsCleanupErrorJoinedWithCancellation` → `TestConsumeDeliveryWithHeartbeatControlLossPreservesJoinedMeaning`
      and `...OwnerCancellationJoinsThenSettles`. Integration:
      `AckFailureLeavesDeliveryForRedelivery` and `FailureLeavesDeliveryUnsettled` →
      `TestIntegrationConsumeDeliveryWithHeartbeatStoppedRenewalUsesBackOff`, which closes the delivery owner's
      connection and proves the server redelivered a delivery this process could not settle;
      `ShutdownDelayedNAKRedelivers` → `TestIntegrationSemanticRetryProducesDurableRedelivery` (the legacy 5s
      shutdown NAK is deliberately gone, design § 2.9, so that claim is superseded rather than ported).
      One thing the task's "delete the file" does not describe: `mockMsg` — the package's in-memory `jetstream.Msg`,
      113 lines of declaration and methods — was DECLARED in `heartbeat_test.go` and is used throughout
      `delivery_settlement_test.go`. Deleting the file as written breaks the typed path's own tests. It moved
      verbatim to `natsclient/mockmsg_test.go` with a doc comment recording why it outlived the file.
      Mutation evidence for section 7 (`cp` backup + md5, `[applied]` printed between mutating and testing, md5
      re-checked after restore).
      L, the retirement the ratchet exists for: `func ConsumeWithHeartbeat() error { return nil }` appended to
      `natsclient/heartbeat.go` (md5 `ea356d643386ca39e23def1baac3d92c` before and after) —
      `TestConsumeWithHeartbeatHasNoDeclarationOrProductionCalls` went red with `retired ConsumeWithHeartbeat surface
      remains: [natsclient/heartbeat.go: function or receiver method]`. Under the PRE-inversion scanner this exact
      mutant was legal, which is what 7.2 had to change.
      M and N both on `natsclient/delivery_settlement.go` (md5 `f1cbfd26bf13eea3d03f8367d7b66709` before and after
      each). M, `result.ownerStopNeeded = true` deleted from the renewal-failure branch — the ported test and
      `TestConsumeDeliveryWithHeartbeatControlLossPreservesJoinedMeaning` both went red. N,
      `errors.Join(ErrHeartbeatFailed, ...)` reduced to the bare `fmt.Errorf` so the sentinel is no longer joined —
      `TestConsumeDeliveryWithHeartbeatInProgressFailureRequiresOwnerStop` was the ONLY test in the whole `natsclient`
      package that went red. That is the measurement behind "port before deleting": without it, the sentinel could
      have been dropped from the typed path with the suite green.
- [x] 7.4 Commit as `refactor(natsclient)!: remove ConsumeWithHeartbeat`. Record in the PR body that
      `task api:compat:report` at `b7ce8727` lists 15 incompatible Tier 1 packages against `v1.0.0-beta.162`,
      `natsclient` (`NewDurableHandler: removed`) and `agentic/agentrun` (`EntityIDPattern`, `Mint`) among them; this
      layer adds one line, `ConsumeWithHeartbeat: removed`, under the already-counted `natsclient` and only compatible
      additions under `agentic/agentrun`, so the package count does not move. The commit is still `!`; the posture is
      still ADR-106's pre-RC descending count (`scripts/api-compat.sh:174`; no allowlist or waiver exists).
      Evidence: the commit is `refactor(natsclient)!: remove ConsumeWithHeartbeat with its last caller`, with a
      `BREAKING CHANGE:` footer naming `docs/operations/migration-beta162-to-beta163.md`.
      `task api:compat:report` at that head, exit 0, base `v1.0.0-beta.162`: `compared: 62 / clean: 47 /
      incompatible: 15`. The `natsclient` block reads exactly two incompatible lines — `ConsumeWithHeartbeat:
      removed` and `NewDurableHandler: removed` — beside the twenty compatible additions the typed API brought; the
      `agentic/agentrun` block still reads `EntityIDPattern` and `Mint` incompatible, with
      `(*MilestoneSubscriber).DeliveryFatal`, `(*MilestoneSubscriber).RegisterMetrics`, `AgentRun.OriginEntityID` and
      `LoopTerminalEvent.SourceMessageID` under Compatible changes. The package count did not move: 15 before this
      layer and 15 after, exactly as reconciliation B4 declares. `MilestoneService.Health()` produces NO line under
      `service` — it overrides a method `MilestoneService` already promoted from `BaseService`, so apidiff sees no
      change; it is a behaviour change behind an unchanged signature, like the two § 7 contract changes.
- [x] 7.5 Reword the two foreign test comments naming the helper
      (`storage/objectstore/component_ack_integration_test.go:41`,
      `processor/agentic-tools/outcomes_integration_test.go:216`).
      Evidence: objectstore's delivery-timing note now sizes its 90s poll deadline on "the framework's 30s
      semantic-retry constant (`natsclient.DelayedDeliveryRetry`)" — the live constant that supplies the same 30s —
      instead of the deleted helper. agentic-tools' now severs the connection "before the delivery's settlement
      contract can ACK the request". Neither test's behaviour changed; both packages are green under `-race`.

## 8. Specs and docs

- [x] 8.1 Copy `specs/` verbatim into the change: ADDED `agent-run-milestones`; MODIFIED `jetstream-consumer-policy`
      "semantic heartbeat settlement has one permanent exported surface" against the live text
      (`openspec/specs/jetstream-consumer-policy/spec.md:380-413`), all three scenarios restated; REMOVED
      `nats-streaming` "the legacy helper is a shrinking remainder, never a compatibility surface" (live
      `openspec/specs/nats-streaming/spec.md:239-255`) and, per reconciliation B5, a second REMOVED "Heartbeat
      consumption SHALL expose settlement failure" (live `:158-181`, whose `:169` names this PR), each with a Reason.
      L0's blocks are in the tree, so `openspec validate agentrun-fanout-settlement --strict` passes at seed.
      Evidence: the three delta files were seeded at design time; this checkpoint VERIFIED them against the live
      specs at this head rather than rewriting them, and found nothing to change.
      The MODIFIED block restates every scenario of the live requirement, by exact title and in the live order —
      live `openspec/specs/jetstream-consumer-policy/spec.md` "public surface at this layer" (`:396`), "binding
      migration requires semantic authority" (`:403`), "fast lane lacks an admitted settlement route" (`:409`)
      against delta `specs/jetstream-consumer-policy/spec.md` "public surface at this layer" (`:17`), "binding
      migration requires semantic authority" (`:24`), "fast lane lacks an admitted settlement route" (`:30`). Three
      live, three restated, no omission and no rename — openspec 1.7.0 refuses either. Only the first scenario's
      text moves, its fourth bullet going from "`ConsumeWithHeartbeat` carries only its ratcheted remaining callers
      and no alias" to "`ConsumeWithHeartbeat` is absent: no declaration, alias, or production caller", which is
      exactly what 7.2's inverted ratchet now asserts.
      Both `nats-streaming` REMOVED blocks carry a Reason and match the live headings verbatim: "Heartbeat
      consumption SHALL expose settlement failure" (live `:158`, whose `:169` assigns its own deletion to this PR)
      and "the legacy helper is a shrinking remainder, never a compatibility surface" (live `:239`).
      The ADDED `agent-run-milestones` reason set was verified against the implementation, not just read: the ten
      labels in the delta are the ten `reason*` constants in `agentic/agentrun/milestone_settlement.go`, name for
      name, and the requirement states that adding, renaming, or retiring one is a change to it.
      `openspec validate agentrun-fanout-settlement --strict` → `Change 'agentrun-fanout-settlement' is valid`,
      exit 0. `openspec validate --all --strict` → `Totals: 56 passed, 0 failed (56 items)`.
- [ ] 8.2 Seed the `agent-run-milestones` Purpose from `proposal.md` at spec sync.
- [x] 8.3 Rewrite `docs/operations/migration-beta162-to-beta163.md:1216-1231` (from `:1226`, "still exported at this
      tag") with the migration text in `proposal.md`; replace
      `docs/operations/migration-restart-safe-nats-client.md:104-108`; update
      `docs/concepts/33-semantic-settlement.md:107-108`.
      Evidence: `migration-beta162-to-beta163.md`'s "still exported at this tag" paragraph is replaced by "gone at
      this tag" plus three named subsections — what a caller does instead (validate the policy from the same config,
      `ConsumeDeliveryWithHeartbeat` or `SettleDelivery*`, nil-means-ACK is gone, inspect the `DeliveryResult`, stop
      the exact handle on `OwnerStopRequired()`); the 30s NAK budget, now asked for explicitly as
      `DelayedDeliveryRetry(30 * time.Second)`, with the `max_deliver 10` ≈ 4.5 minutes arithmetic adopters sized
      against the old constant; and the measured direct-caller list.
      The sister measurement, read-only with `git grep`/`git show` only and no `go` command, SemDev at `ca3956a`:
      TWO direct call sites, `internal/conversationchannel/component.go:476` and `internal/intake/component.go:378`,
      both `natsclient.ConsumeWithHeartbeat(msgCtx, msg, 20*time.Second, func(workCtx) error { return
      c.handleEvent(...) })`. FOUR comment-only sites, not three: `internal/conversationchannel/apply.go:113`,
      `internal/conversationchannel/component.go:435` and `internal/intake/component.go:355` size `max_deliver 10`
      on the helper's fixed 30s NAK, as the design predicted, and a fourth,
      `internal/conversationchannel/apply.go:202`, explains that the per-message context is cancelled when the
      helper's `InProgress` fails. That fourth is a different claim and needed its own sentence: the typed path
      cancels the work context the same way, and additionally reports `OwnerStopRequired()`. No other sister
      references the helper. SemDev's working tree carries pre-existing modifications under `.agents/` and
      `.claude/` that are NOT this session's; nothing was written there.
      THREE contract changes `api-compat.sh` cannot see, not the two design § 7 named, are their own subsection of
      the note, since the Tier 1 report shows nothing for any of them: (1) `ResolveRun`'s errors now carry the
      `errs` Invalid class with chains preserved, so `errors.Is` still matches and `errs.Classify` places them
      deterministically instead of by substring — which is what lets a poison identity Terminate on first sight;
      (2) `MilestoneSubscriber.HandleEvent` returns nil exactly when the attempt would be acknowledged, where it
      previously returned an error only for decode and NATS failures and logged handler errors without propagating
      them. A caller reading nil as "processed" is unaffected; a caller reading non-nil as "the transport broke"
      now also sees handler and resolution failures. (3) `(*MilestoneService).Health()` is a NEW OVERRIDE of a
      method promoted from `BaseService`, which does not change the type's exported method set, so
      `API_COMPAT_MODE=report task api:compat:report` (run on this branch, base `v1.0.0-beta.162`, 62 compared /
      15 incompatible, exit 0) prints nothing for it — its `service` section lists only the `FlowService` removals
      and two signature re-spellings and never names `MilestoneService`. The behaviour is process-wide:
      `service/service_manager.go:1757` turns one unhealthy sub-status into a whole-process 503 on `/health`, so a
      latched milestone lane makes `/health` return 503 until restart, while `/readyz` is unaffected
      (`handleReadiness`, `service/service_manager.go:1778`, reads the startup snapshot). Measured consumers that
      gate on `/health` as a binary: the shipped image's `HEALTHCHECK` in both stages, `docker/Dockerfile:104-105`
      (`AS production`, stage opens at `:65`) and `docker/Dockerfile:155-156` (`AS e2e`, stage opens at `:117`),
      both `wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1` at
      `--interval=30s --retries=3` so a container flips to `unhealthy` after 3x30s; and the published adopter
      examples `semdocs/examples/production/docker-compose.yml:71` (`interval: 10s`, `retries: 5`) and
      `semdocs/examples/quickstart/docker-compose.yml:55` (`interval: 10s`, `retries: 3`), both read-only.
      `docker/compose/agentic.yml:91-103` already overrides to `/readyz` (URL at `:99`) and is unaffected.
      `migration-restart-safe-nats-client.md` now says the helper is removed and that its guard is INVERTED rather
      than retired, and names `DelayedDeliveryRetry(30 * time.Second)` as where the old fixed delay went.
      `docs/concepts/33-semantic-settlement.md`'s "AgentRun fanout needs its own design" open question is replaced
      by the answer it got, including the trade it makes: whole-fanout settlement buys replay safety by moving from
      at-most-once to at-least-once, and pays for it with the `SourceMessageID` identity rather than with the
      per-handler receipt ledger this capability refuses to invent. Every added line is under 120 columns.
- [x] 8.4 O5 (#1155 amended to re-invocation + idempotent effect count) is recorded on #1155 by the coordinator
      (issuecomment-5775148106, 2026-09-22); this PR's proof in § 9 implements the amended acceptance.

## 9. Proof (#1155 stage D; O4, O5)

- [x] 9.1 Add the env-gated test-only `MilestoneHandler` in the binary the agentic tier boots (registered only when
      the variable is set) that commits a durable effect keyed on `SourceMessageID` and, on its first attempt, exits
      the process before Ack.
      **Was a DEVIATION from O4's ruled root; RATIFIED 2026-09-22 as the Q5 amendment** (owner on #1249, after an
      architect inventory and docket; the rule it established lands in § 11). The handler is in
      `test/e2e/harness/milestoneprobe` and its registration hook is in `cmd/semstreams`, NOT
      `cmd/e2e-semstreams`. The agentic tier does not boot that root:
      `docker/compose/agentic.yml:66-68` builds Dockerfile target `e2e-process-barrier`, which is
      `./cmd/semstreams` with `-tags=e2e_process_barrier` (`docker/Dockerfile:182-193`). `cmd/e2e-semstreams` is the
      `e2e` target, used by `ops.yml`, `lifecycle.yml`, `research-graph.yml` and `tiered.yml`. A probe registered
      there could never run under `task e2e:agentic`, which is the tier task 10.2 requires, so O4's placement and
      10.2's gate cannot both be satisfied as written. Everything O4 ruled ON is kept and strengthened: the handler
      is test-only, env-gated, and no production binary contains it.
      Gating is two independent mechanisms. (1) Build tag: `cmd/semstreams/milestone_probe_e2e.go`
      (`//go:build e2e_process_barrier`) is the only file that imports the harness;
      `cmd/semstreams/milestone_probe_disabled.go` (`//go:build !e2e_process_barrier`) is a no-op with no harness
      import, so the ordinary dependency graph never reaches it. (2) Environment: `milestoneprobe.Register` returns
      nil unless `SEMSTREAMS_E2E_MILESTONE_PROBE` is set, named in the handler's package doc comment, in
      `Register`'s doc comment, and in the compose block that sets it. `docs/contributing/02-e2e-tests.md` had no
      tier env-knob list to add to (no `AGENTIC_LLM_URL`, no `AGENTIC_COMPOSE_FILE`, and the agentic tier was not
      among its Test Tiers sections), so the knob is documented where it is read and where it is set; § 11 then gave
      that page the list of all twelve tiers it was missing, pointing at the spec table for each tier's gate.
      Guards, after § 11 consolidated the tier facts: `TestE2ETierTableMatchesComposeAndDockerfile` pins the tag,
      the target, the binary and BOTH directions of the env gate — `agentic.yml`'s built service sets the variable
      and no other compose FILE so much as mentions it, the second half swept over raw text rather than over parsed
      services, because an overlay with no `build:` block is invisible to the YAML walk and compose still merges its
      `environment:` into the container; an arming leak into another tier would look like a flake, since the probe
      crashes and quarantines on purpose. `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` pins that only a
      tagged file in `cmd/semstreams` may import the harness at all, and that every tagged file imports one; `TestMilestoneProbeIsInertWithoutTag` calls the no-op with
      nils; `TestRegisterIsInertWithoutTheEnvironmentVariable` and `TestRegisterRefusesIncompleteWiringWhenArmed`
      pin the runtime gate's both directions. The first two replaced the four per-hook pin tests, two of which —
      `TestDefaultMilestoneProbeFileDoesNotImportHarness` and `TestAgenticComposeArmsTheMilestoneProbe` — this task
      previously named.
      The registration is one call inside `registerMilestoneService` in `cmd/semstreams/main.go` — an eight-line
      block: five comment lines and one guarded call. That is the one deliberate divergence between the two
      hand-copied bodies (#1301) and BOTH roots' doc comments say so in those words, replacing the previous
      "identical registerMilestoneService body" claim, which would otherwise have become false silently.
      Shape: the probe demuxes on `LoopTerminalEvent.Role` (already on both terminal payloads, so no framework change
      makes it addressable) and returns nil before any IO for every role it does not own — pinned without NATS by
      `TestOrdinaryTerminalIsANoOpBeforeAnyIO`, which holds a nil client so any read or publish would panic.
      Its durable effect is published with `Nats-Msg-Id` = `SourceMessageID`, and its attempts are appended with a
      per-invocation ID; the first-attempt decision reads the DURABLE attempt count, never process memory, because a
      replacement process starts with empty memory and a memory-based decision would crash on every restart forever.
- [x] 9.2 E2E scenario, both lanes: the replacement redelivers; the handler observes the same `SourceMessageID`;
      effect count 1; ack-pending 0. Panic on first attempt: no Ack/Nak/Term; `/health` reports `milestone`
      unhealthy; the failed lane is drained while the other consumes; the replacement succeeds. Five transient
      returns: `semstreams_nats_max_delivery_exhaustions_total{consumer="agentrun-milestone-complete"}` = 1. Record
      every stage's pass/fail verbatim in the PR body.
      Three new stages in the agentic tier, all pinned in order by `TestStagesAreExactlyThisOrderedList`:
      `arm-milestone-exhaustion` (action stage, `asserts:false`, third in the list), `verify-milestone-exhaustion`
      (`asserts:true`, straight after `verify-streaming-metrics`) and `verify-milestone-settlement` (`asserts:true`,
      straight after `verify-stage-a-process-replacement`). `assertions_run` moves 15 -> 17 and the tier's derived
      denominator check moves with it.
      Placement is forced by two facts, both recorded at the stages: exhaustion costs four redeliveries at the lane's
      30s retry delay, so it is armed near the top and asserted ~2 minutes later in `verify-milestone-exhaustion`;
      and it must be asserted BEFORE anything replaces the process, because the exhaustion counter is process-local
      while the advisory feeding it is acknowledged durably — a replacement in between loses the only occurrence.
      The exhaustion assertion is its OWN stage, not the tail of `verify-streaming-metrics` it was first written as
      (review M1). A tail is deleted in one line with every in-tree guard still green — the guard pins stage NAMES,
      so it cannot see a call removed from inside a stage — and that particular tail was also skippable by its host
      stage's early `return nil` on a streaming warning. A named stage fails `TestStagesAreExactlyThisOrderedList`
      in plain `go test` when it goes away.
      Three pre-implementation artifacts name `verify-streaming-metrics` as the exhaustion assertion's home —
      `design.md:199`, `inventory.md:388` (which also names that stage as the decisions counter's consumer at
      birth) and `tasks-pins.md:82`. They are NOT edited: they are pre-change evidence of what was designed and
      measured then, and re-pinning them would destroy exactly that. This task line is where the move is recorded,
      and `inventory.md:388`'s consumer claim becomes true of `verify-milestone-exhaustion` — it was not true of
      `verify-streaming-metrics` at any revision (see 5.3's refuting grep).
      `verify-milestone-exhaustion` reads the exhaustion advisory counter AND
      `semstreams_agentrun_milestone_decisions_total{lane="complete",decision="retry",reason="handler_transient"}`
      = 5 — the first e2e observer of the operator signal section 5.2 added. `observeDecision` returns before the
      increment on an Ack (`agentic/agentrun/milestone_settlement.go:218-219`), so an ordinary milestone contributes
      nothing to that series and the armed probe's five transient returns are what it counts. The wait costs no
      wall clock: the advisory it follows cannot fire before the fifth attempt has already been counted.
      Both counter figures in this task are `waitMetricWithLabels` targets, so each assertion is "reaches N", not
      "equals N" — the exactly-five half is held beside them by the handler attempt count, which IS an equality
      (`attempts != milestoneLaneMaxDeliver` fails).
      The settlement stage sits after stage A so no later replacement resets what it measures, and before the
      approval and signal walks, whose loops publish terminals onto the same two lanes.
      Quarantine is proven on the FAILED lane: nothing else in this tier publishes `agent.failed.*`, so latching it
      cannot perturb the walks that follow, while `agent.complete.*` stays available as the live evidence that one
      lane's fatal leaves the other consuming. The spec scenario "a fatal on one lane leaves the other consuming"
      states its WHEN on the complete lane; the requirement itself is lane-symmetric ("a fatal on one lane drains
      only that lane's exact handle") and the tier proves the failed-lane instance of it.
      The complete-lane instance is proven in-tree, which is why the spec delta needs no amendment for the tier's
      lane choice: `agentic/agentrun/milestone_owner_internal_test.go:86-104`
      (`TestMilestoneFatalDrainsOnlyTheFailedLane`) delivers the panicking terminal on the COMPLETE lane and
      asserts that lane drains its exact handle and admits nothing while the failed lane is untouched and still
      admitting; `:110-140` (`TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain`) pins that a later Stop
      does not order a second drain of the lane the observer already drained. Both lanes are therefore covered,
      one per level: the symmetric requirement in-tree, the deployed instance in the tier.
      Each injected terminal persists a route-LESS `AGENT_LOOPS` record first. Without one, agentic-dispatch answers
      an absent record with an unbounded transient retry (`processor/agentic-dispatch/terminal_settlement.go`), so
      the terminal would stay pending on the dispatch lane forever and break the settled-consumer assertions stage A
      already makes. With it, dispatch settles as `route_less_settled` and publishes nothing, leaving the milestone
      lanes as the only place the terminal acts.
      The quarantine stage also asserts `/readyz` = 200 while `/health` is 503: the agentic compose overrides the
      container healthcheck to `/readyz` (`docker/compose/agentic.yml:91-103`), so the container stays up. That is
      not incidental — a 503 there would replace the proof with a container restart loop. Both halves of that
      sentence are now assertions rather than one assertion and one description (review M3): the stage takes the
      status code from `getHealthBody` and requires exactly 503, and requires the milestone sub-status message to
      contain `delivery ownership lost`. `waitForMilestoneHealth` only requires `healthy=false`, which ANY unhealthy
      cause satisfies, and `milestoneHealth` accepts 200 or 503 so it can be polled across the transition — so
      without those two the stage could pass on an unrelated outage.
      The admits-nothing check carries a positive control (review M2): `NumPending` must GROW across the blocked
      publish while `Delivered` does not move. An unchanged `Delivered` alone also describes a message that never
      arrived, so a broken publish would have read as a working latch.
      Mutation evidence (both at `97703c65`, both `cp` + md5 + `[applied]` + restore + re-checksum;
      `test/e2e/harness/milestoneprobe/milestoneprobe.go` md5 `f1deebec4965f64c51795becfacacc47` before and after
      BOTH). The pair was lettered L and M and is renamed at review, because both letters were already spent:
      § 7 carries L (`tasks.md:464`) and M and N (`:469`) on `natsclient`. The sequence therefore continues at O.
      The review asked for N/O, which would have re-created on N the exact collision it was fixing on L.
      O, non-idempotent effect — `commitEffect` publishing via `PublishToStream` (no `Nats-Msg-Id`) instead of
      `PublishToStreamWithMsgID(..., ev.SourceMessageID)`. `task e2e:agentic` exit 201:
      `verify-milestone-settlement failed: complete lane replacement: durable effects for
      eac98d82-7d95-4f6f-a0b8-a2a9e08ca618 = 2, want exactly 1 across every attempt`, `assertions_run=10`.
      P, Ack before the effect commits — `return nil` inserted at the top of the `BehaviorExitBeforeAck` arm, so the
      handler acknowledges without committing or ending the process. `task e2e:agentic` exit 201:
      `verify-milestone-settlement failed: complete lane replacement: the SemStreams process still answered within
      30s; the probe did not end it`, `assertions_run=10` — the handler is never re-invoked, which is the shape the
      stage exists to refuse.

## 10. Gates

- [x] 10.1 `task lint`, `task test`, `task test:race`, `task schema:generate` with empty `schemas/`/`specs/` drift,
      `task openspec:validate`, `task spec:properties`, `task check:push`; commands and exit codes in the PR body.
      Run 2026-09-22 from the worktree root at `dc9a2f7b` (the last code commit; only this evidence follows it).
      Every gate's exit code is the command's own, captured as `EXIT=$?` immediately after it, never after an echo:

      | Command | Exit | Final line |
      |---|---|---|
      | `task lint` | 0 | `ok  	github.com/c360studio/semstreams/test/natsclient	0.662s` |
      | `task test` | 0 | `?   github.com/c360studio/semstreams/vocabulary/rulepacks   [no test files]` |
      | `task test:race` | 0 | same final line; `ok  github.com/c360studio/semstreams/service  7.861s` |
      | `task schema:generate` | 0 | `Schemas and OpenAPI spec generated` |
      | `git diff --stat schemas/ specs/` | 0 | no output — no drift |
      | `task openspec:validate` | 0 | `Totals: 56 passed, 0 failed (56 items)` |
      | `task spec:properties` | 0 | `spec-properties: 288/288 citations resolve.` |
      | `go run ./cmd/entity-id-audit .` | 0 | `entity ID audit passed: 1333 structured candidates across 1 roots` |
      | `git diff --check b7ce8727..HEAD` | 0 | no output |
      | `task check:push` | 0 | `[INTEGRATION] tests complete`; 0 lines beginning `FAIL` |
      | `git status --porcelain` | 0 | no output — clean |

      The denominator is stated deliberately: `task test` and `task test:race` each produced 177 package result
      lines with 0 lines beginning `FAIL`, so the green is over the whole tree and not over a truncated tail. New
      test files were committed before `task spec:properties` ran, so they are inside its tracked-file denominator
      rather than silently skipped.
      `git log --oneline 647a16f3..HEAD` for this checkpoint:
      `6b002817` probe handler and wiring; `867cb225` the BaseService health-race measurement;
      `287fb8b3` the tier stages; `97703c65` per-proof result keys and the measured tier duration;
      `dc9a2f7b` the milestone health test's wait; `6dde2eed` this evidence.
      **Re-run after the review**, over `b9e3c4d3` (the review's code) and `8f441f8e` (the gate), with only this
      evidence after them. Same rule: each exit code is the command's own, captured as `EXIT=$?` immediately after
      it, never after an echo.

      | Command | Exit | Final line |
      |---|---|---|
      | `task lint` | 0 | `ok  	github.com/c360studio/semstreams/test/natsclient	0.683s` |
      | `go build ./...` | 0 | no output |
      | `go build -tags=e2e_process_barrier ./cmd/semstreams` | 0 | no output |
      | `go vet -tags=e2e_process_barrier ./cmd/semstreams ./test/e2e/...` | 0 | no output |
      | `go test -race -count=1 ./test/e2e/... ./service/... ./cmd/...` | 0 | `ok  	github.com/c360studio/semstreams/cmd/semstreams	3.025s` |
      | `task spec:properties` | 0 | `spec-properties: 288/288 citations resolve.` |
      | `openspec validate --all --strict` | 0 | `Totals: 56 passed, 0 failed (56 items)` |
      | `go run ./cmd/entity-id-audit .` | 0 | `entity ID audit passed: 1333 structured candidates across 1 roots` |
      | `git diff --check b7ce8727..HEAD` | 0 | no output |
      | `task check:push` | 0 | `[INTEGRATION] tests complete` |
      | `task e2e:agentic` | 0 | recorded in 10.2 |

      Denominators again, because a green tail over a truncated list is not a green suite: the race run above
      produced 33 package result lines with 0 beginning `FAIL`, and `task check:push` produced 356 with 0 beginning
      `FAIL`. `check:push` now carries the third tagged vet (`task: [check:push] go vet
      -tags=e2e_process_barrier ...`, line 14 of its log), so the agentic tier's own build is compiled by a local
      gate rather than only by a Docker run. CI itself still runs the untagged `go vet ./...` alone
      (`.github/workflows/ci.yml:44`) — no tagged vet has ever been a CI step — so this gate is pre-push, not CI.
      The tagged build drops a `semstreams` binary at the worktree root; it is gitignored and was deleted, and
      `git status --porcelain` is empty.
- [x] 10.2 `task e2e:agentic` green on the pushed head (the BREAKING rule, `docs/contributing/02-e2e-tests.md:312`),
      every stage's result verbatim in the PR body.
      Ran 2026-09-22 at `6dde2eed`; later commits are evidence and the review fixes. Host state before the
      run, pasted: `pgrep -fl e2e.test` printed nothing (exit 1), `docker compose ls` listed no stacks.
      `task e2e:agentic` exit 0:
      `level=INFO msg="Scenario completed successfully" duration=5m23.017389458s ... assertions_run=16`.
      The runner emits no per-stage PASS line: `Execute` records `<stage>_duration_ms` for each stage that ran to
      completion and returns at the FIRST failing stage with `<stage> failed: <err>`, so a recorded duration IS that
      stage's pass and the absence of any `level=ERROR` line is the absence of a failure. All 19 stages in execution
      order, with the durations the run printed (ms): `verify-components` 2; `capture-baseline` 6;
      `arm-milestone-exhaustion` 8; `inject-task` 0; `wait-for-completion` 512; `verify-terminal-response` 5;
      `validate-trajectory` 5; `verify-graph-triples` 3; `verify-tool-execution` 10; `verify-durable-tool-replay`
      44581; `verify-streaming-metrics` 104959; `verify-tool-call-governance` 12;
      `verify-stage-a-process-replacement` 78781; `verify-milestone-settlement` 93337; `walk-approval-path` 477;
      `refuse-non-canonical-approval` 22; `walk-signal-path` 264; `refuse-non-canonical-signal` 22;
      `validate-results` 0. `assertions_run=16` equals the count `assertingStageCount()` derives from the same list,
      and `TestStagesAreExactlyThisOrderedList` holds the list itself, so the number cannot agree with a list it no
      longer describes.
      The stage-D measurements the run published, one per proof:
      `milestone_exit-before-ack_agentrun-milestone-complete_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_exit-before-ack_agentrun-milestone-failed_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_panic-once_agentrun-milestone-failed_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_exhaustion_attempts:5`. Re-invocation plus an idempotent effect count on
      both lanes, and the finite ceiling read from the handler's side.
      The tier is now 5m23s, up from the ~2m its description carried; `taskfiles/e2e/agentic.yml` records the
      measured figure rather than the stale one. An earlier green run of the same stages at `287fb8b3` took
      5m23.074s, so the cost is the two AckWait expiries, three container replacements and the exhaustion wait, not
      run-to-run noise.
      **Re-run at `b9e3c4d3` after the review**, which is the commit carrying M1 (the exhaustion proof promoted to
      its own stage plus the decisions-counter assertion), M2 (the NumPending positive control) and M3 (the 503 and
      the latched cause). The records above are kept: this is a second measurement of the same tier, not a
      replacement for the first.
      Host state before the run, pasted: `pgrep -fl e2e.test` printed nothing (exit 1), `docker compose ls` listed
      no stacks; both print the same after it, and `task e2e:clean` was not used. `task e2e:agentic` exit 0:
      `level=INFO msg="Scenario completed successfully" duration=5m22.934382917s ... assertions_run=17`, with 0
      `level=ERROR` lines in the whole log.
      All 20 stages in execution order, with the durations this run printed (ms): `verify-components` 3;
      `capture-baseline` 7; `arm-milestone-exhaustion` 9; `inject-task` 0; `wait-for-completion` 511;
      `verify-terminal-response` 5; `validate-trajectory` 6; `verify-graph-triples` 4; `verify-tool-execution` 6;
      `verify-durable-tool-replay` 44742; `verify-streaming-metrics` 15; `verify-milestone-exhaustion` 104989;
      `verify-tool-call-governance` 18; `verify-stage-a-process-replacement` 78681; `verify-milestone-settlement`
      93086; `walk-approval-path` 499; `refuse-non-canonical-approval` 40; `walk-signal-path` 270;
      `refuse-non-canonical-signal` 33; `validate-results` 0. `assertions_run=17` is the count
      `assertingStageCount()` derives from that list, and `TestStagesAreExactlyThisOrderedList` now pins 20 names
      in order.
      The promotion is visible in the numbers and cost no wall clock: `verify-streaming-metrics` drops
      104959 -> 15 ms, the new `verify-milestone-exhaustion` picks up 104989 ms, and the tier moves
      5m23.017 -> 5m22.934 — the wait was always the exhaustion, never the streaming read.
      The stage-D measurements are identical to the pre-review run, which is the point of re-running it:
      `milestone_exit-before-ack_agentrun-milestone-complete_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_exit-before-ack_agentrun-milestone-failed_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_panic-once_agentrun-milestone-failed_handler_attempts:2` /
      `..._durable_effects:1`; `milestone_exhaustion_attempts:5`. The three tightened assertions — the decisions
      counter at 5, `NumPending` growing across the blocked publish, and `/health` = 503 naming `delivery ownership
      lost` — all held on the deployed binary.

## 11. E2E hook placement (Q5 amended, owner 2026-09-22)

The owner reopened Q5 as a design question (verbatim: "1249 smells like we have a deign problem if it's non obvious
where an e2e test lands? we need to figure that out first"), commissioned a read-only architect inventory and a
docket, and then ruled: *an E2E-only hook lands in the binary its tier boots, gated by that tier's build tag and,
where it must stay inert in the tier's other stages, an env var.* Docket option B is ratified — the rule and the
table land WITH the hook they govern, in this PR. PR #1360's probe stays where § 9.1 put it. Options C/D and docket
questions 3 (rename the shared `e2e_process_barrier` tag) and 5 (vet the slow-consumer tag in `check:push`) are
**#1301's**, sequenced
after #1362 and gating the tag; they are deliberately not done here.

- [x] 11.1 The rule and the twelve-tier table, through the change's spec delta.
      `openspec/changes/agentrun-fanout-settlement/specs/payload-registry/spec.md` is a new MODIFIED block against the
      live requirement "A message type is a type of the deployment only if it is registered in the binary's payload
      registry" (`openspec/specs/payload-registry/spec.md:10`). It restates all four live scenarios by exact title and
      in live order, adds two, and keeps the MUST on the requirement's first line.
      `openspec validate agentrun-fanout-settlement --strict` exit 0, `Change 'agentrun-fanout-settlement' is valid`.
      The table was already broader than the docket recorded: the live table at `:23-35` carries **eleven** rows, not
      the six the docket's "six-row table about synthetic types" describes, and it already named agentic, ops,
      crud-tools, deep-research and slow-consumer (`git show 8f441f8e:openspec/specs/payload-registry/spec.md` and
      HEAD are byte-identical here, so this is not rebase drift). What it did not carry is the tier's Dockerfile
      target, its gate, its hooks, or a `throughput` row, and no sentence anywhere said where a hook that must run
      INSIDE the production composition goes. The delta's table is twelve rows with target / binary / gate / hook
      columns beside the synthetic-type column it already had, and each row names exactly one compose service, so the
      rows and the twelve services built from `docker/Dockerfile` are in bijection.
      One restated pin had drifted and is corrected rather than copied: the ops seed's direct `PutKV` calls are
      `test/e2e/scenarios/ops/scenario.go:439` and `:484` (the registered type is at `:476`), not `:464,472`.
      `docker/compose/tiered.yml:246` claimed the structural service boots the e2e root "with reactive workflow
      engine" — that engine was deleted in `e1c66cbd` (2026-03-12); the comment now states the reason the spec row
      gives. The three other survivals of the same deleted founding reason (`docker/Dockerfile:54`, `:115`,
      `taskfiles/build.yml:17`) are left: `test/release/release_smoke_test.go:64,66` slices the Dockerfile on that
      comment's text, and the second root's existence is #1301's question, not this checkpoint's.
- [x] 11.2 One table-driven contract test replaces the four per-hook pin tests.
      `test/contract/e2e_tier_binary_contract_test.go` parses the tier table out of the payload-registry spec — the
      active change's delta first, the live spec after `openspec archive`, so there is no second copy of the table —
      and drives every assertion from its rows: the compose service's `build.target`, the Go package and `-tags=` of
      that target resolved through `docker/Dockerfile`'s stage graph, and the `SEMSTREAMS_E2E_*` variables the service
      sets. It closes the table in both directions (no compose service built from `docker/Dockerfile` may be missing
      from the table, no row may name a service that does not exist), and re-pins the beta.90 hazard that
      `docker/compose/e2e.yml:45-52` records in prose: two targets sharing one `image:` tag. A gate token it cannot
      classify fails the row rather than being ignored — an unread gate is the fail-open shape.
      `TestProductionRootReachesNoE2EHarnessWithoutABuildTag` is the compile-time half. It sweeps every non-test file
      of the package the `production` target builds (read from the Dockerfile, not listed) and fails any that imports
      a `test/e2e/` or `internal/e2e*` package while still building with the overlay tags off — `go/build/constraint`
      evaluation, not a string match — so it covers hook files nobody has written yet. Run: `9 non-test files scanned,
      3 behind an overlay tag`; it refuses a zero denominator in either direction.
      Deleted (subsumed): `TestProcessBarrierE2EBuildDoesNotReplaceProductionTarget`,
      `TestDefaultProcessBarrierFileDoesNotImportHarness`, `TestSlowConsumerE2EBuildDoesNotReplaceProductionTarget`,
      `TestDefaultMilestoneProbeFileDoesNotImportHarness`, `TestAgenticComposeArmsTheMilestoneProbe`, and the compose
      half of `TestOpsComposeUsesE2EBinaryForLessonCurationControl`.
      Kept, because none is a tier→binary→gate fact: `TestMilestoneProbeIsInertWithoutTag` (the untagged stub's
      nil-safe no-op), `TestShippedAgenticConfigDoesNotAdmitProcessBarrier` (config admission),
      `TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration` (boot-order window), and the ops test's persona
      assertion, now `TestE2ECompositionRootLoadsCheckedInPersonaFragments`. Two files are renamed to what they hold
      (`process_barrier_config_contract_test.go`, `slow_consumer_hook_contract_test.go`,
      `test/e2e/scenarios/ops/composition_root_contract_test.go`). `test/e2e/harness/milestoneprobe/protocol_test.go`
      is untouched — the brief listed it as carrying a compose check, and it does not; its env-gate tests in both
      directions (`TestRegisterIsInertWithoutTheEnvironmentVariable`,
      `TestRegisterRefusesIncompleteWiringWhenArmed`) are behaviour, not tier facts.
      Mutation evidence, each by `cp` backup with md5 before / after-mutation / after-restore, `[applied]` printed
      between mutating and testing, and every restore sum equal to its original:

      | Mutant | md5 before → mutated → restored | Result |
      |---|---|---|
      | `docker/compose/agentic.yml` `target: e2e-process-barrier` → `target: e2e` | `decf934c3dea5a9b38c6aef37cab10b1` → `00f1348a574ea978f7273b0da31aaf47` → `decf934c3dea5a9b38c6aef37cab10b1` | RED: `agentic (agentic.yml semstreams): compose target = "e2e", spec table says "e2e-process-barrier"` |
      | `docker/Dockerfile` `-tags=e2e_process_barrier` → `-trimpath` | `bda229324b35a27e311fda6012816447` → `c8bb58cc253142b66d9675158b7bdecf` → `bda229324b35a27e311fda6012816447` | RED in both tests: `builds with tags [], spec table says [e2e_process_barrier]`, and both hook files now `build without any of the overlay tags [e2e_slow_consumer]` |
      | `docker/compose/agentic.yml` drops `SEMSTREAMS_E2E_MILESTONE_PROBE=1` | `decf934c3dea5a9b38c6aef37cab10b1` → `7bc9437bdd069a4a56e8d3b71cde692e` → `decf934c3dea5a9b38c6aef37cab10b1` | RED: `compose sets [], spec table says [SEMSTREAMS_E2E_MILESTONE_PROBE]` |
      | `docker/compose/ops.yml` ADDS `SEMSTREAMS_E2E_MILESTONE_PROBE=1` (the leak direction) | `accbaecb66dd784922f95f4d442fa4a0` → `30bdf6d6b2860eacdceeaa82fcc64e04` → `accbaecb66dd784922f95f4d442fa4a0` | RED: `ops (ops.yml semstreams): compose sets [SEMSTREAMS_E2E_MILESTONE_PROBE], spec table says []` |
      | the spec table loses its `slow-consumer` row | `79b54680914613a3c4f6ad14be6c3ee0` → `09c0557124219cbb9f082576da221edd` → `79b54680914613a3c4f6ad14be6c3ee0` | RED: `11 rows`, `compose service e2e-slow-consumer.yml/semstreams builds docker/Dockerfile but no tier table row names it` |

      The fifth mutant is the one that proves the SPEC is load-bearing rather than the compose files alone: delete a
      row and the guard fails, so the table cannot quietly shrink to whatever still passes.
- [x] 11.3 `docs/contributing/02-e2e-tests.md` corrected. Its Quick Reference called itself "5 E2E tasks - one per
      tier" and listed four; its "Docker Compose Files" table named `structural.yml` and `federation.yml`, neither of
      which exists (`ls docker/compose/`). That section is now "Every tier and the binary it boots": the rule in two
      sentences, a pointer naming `openspec/specs/payload-registry/spec.md` as the source of truth and
      `test/contract/e2e_tier_binary_contract_test.go` as what re-reads it, and all twelve tiers with compose file,
      service, Dockerfile target and binary — gate and hook columns are NOT duplicated here, per the ruling. The
      thirteenth task, `e2e:openai-responses`, is named as the live paid-API test with no container. The
      "Directory Structure" block carried the same class of defect (a `federation.yml` taskfile, four scenario files
      that no longer exist) and is replaced with the layout that is there, including `test/e2e/harness/`. The
      BREAKING rule is unchanged in text and **moves from `:299` to `:312`**: `task inventory:verify` and
      `tasks-pins.md:84,188` pin `:299` as PRE-change evidence and are deliberately not re-pinned, and § 10.2's
      forward-looking citation is re-derived with `sed -n "312p"`.
      The page points at `openspec/specs/payload-registry/spec.md` rather than at the delta, because the archive is
      this PR's last content commit and the live spec is where the table is read from afterwards; until then the
      contract test resolves the delta first, so the two never disagree in a way a gate cannot see.
- [x] 11.4 Gates for this checkpoint, run from the worktree root on the tree that became `6931c6f7` plus § 11's own
      markdown. Every exit code is the command's own, captured as `EXIT=$?` or `${pipestatus[1]}` immediately after
      it, never after an echo:

      | Command | Exit | Final line |
      |---|---|---|
      | `task lint` | 0 | `ok  	github.com/c360studio/semstreams/test/natsclient	0.681s` |
      | `go build ./...` | 0 | no output |
      | `go vet -tags=e2e_process_barrier ./cmd/semstreams ./test/e2e/...` | 0 | no output |
      | `go test -race -count=1 ./test/contract/... ./cmd/... ./test/e2e/...` | 0 | `ok  	github.com/c360studio/semstreams/test/e2e/scenarios/throughput	2.714s` |
      | `task spec:properties` | 0 | `spec-properties: 288/288 citations resolve.` |
      | `openspec validate --all --strict` | 0 | `Totals: 56 passed, 0 failed (56 items)` |
      | `task check:push` | 0 | `[INTEGRATION] tests complete` |
      | `git diff --stat schemas/ specs/` | 0 | no output — no drift |

      Denominators, because a green run that skipped everything reads identically: the race run produced 33 package
      result lines with 0 beginning `FAIL`; `check:push` produced 316 `ok` lines and 40 `[no test files]` with 0
      beginning `FAIL`; `spec:properties` is unchanged at 288 because this checkpoint adds no `// spec:` citation —
      the new contract test is not property-based, and the spec names it in a scenario instead, which is how the rest
      of this capability's tests are cited. The `openspec validate` total is unchanged at 56 items because the
      payload-registry delta joins an existing change rather than adding one.
      `openspec validate --all --strict` and the markdown-walking contract test were re-run after the last
      `tasks.md` re-wrap, since markdown is a gate input here.
      No tier run: nothing this checkpoint touches is compiled or read by a tier. The only non-markdown edits are a
      new `test/contract` file, four `_test.go` files, and one comment line in `docker/compose/tiered.yml`;
      `go vet -tags=e2e_process_barrier` covers the tagged tree the agentic tier's image builds.

### Checkpoint-B review fixes (2026-09-22)

The reviewer verified every one of the twelve rows, the bijection in both directions and the MODIFIED block, and
re-ran the R1/R2 mutants red. Nothing in the rule or the table changed; what changed is the guard's shape and five
text claims.

- [x] 11.5 HIGH-1 — the spec resolver was first-match-wins and therefore fail-open.
      `test/contract/e2e_tier_binary_contract_test.go:59-92` now gathers EVERY candidate (the changes glob plus the
      live spec), and `t.Fatalf`s unless exactly one carries the table header, naming all of them when more than one
      does. openspec's MODIFIED rule makes the ambiguous case ordinary rather than exotic: the next change touching
      this requirement must restate the whole block, table included, so two deltas would both carry it and the old
      resolver would have governed by alphabetical change id. The dead `"/changes/archive/"` skip is removed — a
      single `*` cannot reach `openspec/changes/archive/<date>-<id>/specs/`, one level deeper — and replaced by the
      comment saying so.
      Mutant N6, the reviewer's: a sibling `openspec/changes/aaa-other-change/specs/payload-registry/spec.md` holding
      a correct copy of the table, AND this change's own delta corrupted (agentic row rewritten to
      `production` / no gate). Delta md5 `79b54680914613a3c4f6ad14be6c3ee0` → `c19362b36eb7193ecf8f2942649f18bb` →
      `79b54680914613a3c4f6ad14be6c3ee0`; `[applied]` printed with the sibling path before the run; sibling directory
      removed and `git status --porcelain` showed only the intended test-file edit afterwards. GREEN before this fix
      (the reviewer's finding), now RED: `2 payload-registry specs carry the tier table, so which one governs is
      ambiguous: …/aaa-other-change/…, …/agentrun-fanout-settlement/…`.
- [x] 11.6 HIGH-2 — "every other compose file does not set the variable" was false, and the guard could not see it.
      `composeServices` keeps only services carrying `build.dockerfile: docker/Dockerfile`, so the `semstreams-ml`
      overlays in `docker/compose/tiered.8b.yml:88` and `tiered.frontier.yml:57` — `environment:` and no `build:` —
      were invisible, while `docker compose -f tiered.yml -f tiered.8b.yml` MERGES `environment:` into the container
      that actually runs. `assertNoComposeFileArmsAnUndeclaredHook`
      (`test/contract/e2e_tier_binary_contract_test.go:456-492`) restores what the deleted
      `TestAgenticComposeArmsTheMilestoneProbe` did: a raw-text sweep of every `docker/compose/*.yml` where the set of
      `SEMSTREAMS_E2E_*` names a file mentions must equal the set its table rows declare — comments included, since a
      mention is how a leak gets copied into the next tier. The spec scenario gains the matching `AND` bullet.
      Mutant N2, the reviewer's: `- SEMSTREAMS_E2E_MILESTONE_PROBE=1` added under `tiered.8b.yml`'s `semstreams-ml`.
      md5 `947a1edca55c1eb6f330f3ea4c27aece` → `15cbed9dfcda81169a534e722e326972` →
      `947a1edca55c1eb6f330f3ea4c27aece`. GREEN before this fix, now RED:
      `tiered.8b.yml mentions [SEMSTREAMS_E2E_MILESTONE_PROBE], the tier table declares [] for that file`.
      NIT-5 rides along: the `imagesByTarget` write is guarded so a shared image tag is reported once rather than
      once per later row.
