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
- [ ] 5.3 Mutation evidence: delete the `RegisterMetrics` call in one root; run the e2e `verify-streaming-metrics`
      stage; record the failure; restore and checksum.
      The in-tree half is DONE; the `verify-streaming-metrics` stage runs with the § 9 proof, which owns the e2e tier.
      All three mutants used a `cp` backup, printed `[applied]` between mutating and testing, and re-checked the md5
      after restoring.
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
      Repo-wide reference sweep, stderr visible: `git grep -n 'ConsumeWithHeartbeat' --include='*.go'` returns only
      the ratchet's own string literals, the ported test's provenance note, and `mockmsg_test.go`'s provenance note —
      every one of them a statement ABOUT the removal, not a use. Outside Go: `docs/operations/` and
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
      attempted. Before it, `ErrHeartbeatFailed` was asserted ONLY by the three deleted tests and one deleted
      integration test — `git grep -n ErrHeartbeatFailed -- '*_test.go'` on the pre-deletion tree returned four hits,
      all of them in the two files this task removes, so deleting them without the port would have left the sentinel
      with no assertion anywhere.
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

- [ ] 8.1 Copy `specs/` verbatim into the change: ADDED `agent-run-milestones`; MODIFIED `jetstream-consumer-policy`
      "semantic heartbeat settlement has one permanent exported surface" against the live text
      (`openspec/specs/jetstream-consumer-policy/spec.md:380-413`), all three scenarios restated; REMOVED
      `nats-streaming` "the legacy helper is a shrinking remainder, never a compatibility surface" (live
      `openspec/specs/nats-streaming/spec.md:239-255`) and, per reconciliation B5, a second REMOVED "Heartbeat
      consumption SHALL expose settlement failure" (live `:158-181`, whose `:169` names this PR), each with a Reason.
      L0's blocks are in the tree, so `openspec validate agentrun-fanout-settlement --strict` passes at seed.
- [ ] 8.2 Seed the `agent-run-milestones` Purpose from `proposal.md` at spec sync.
- [ ] 8.3 Rewrite `docs/operations/migration-beta162-to-beta163.md:1216-1231` (from `:1226`, "still exported at this
      tag") with the migration text in `proposal.md`; replace
      `docs/operations/migration-restart-safe-nats-client.md:104-108`; update
      `docs/concepts/33-semantic-settlement.md:107-108`.
- [ ] 8.4 O5 (#1155 amended to re-invocation + idempotent effect count) is recorded on #1155 by the coordinator; this
      PR's proof in § 9 implements the amended acceptance.

## 9. Proof (#1155 stage D; O4, O5)

- [ ] 9.1 Add the env-gated test-only `MilestoneHandler` in `cmd/e2e-semstreams` (registered only when the variable is
      set) that commits a durable effect keyed on `SourceMessageID` and, on its first attempt, exits the process
      before Ack.
- [ ] 9.2 E2E scenario, both lanes: the replacement redelivers; the handler observes the same `SourceMessageID`;
      effect count 1; ack-pending 0. Panic on first attempt: no Ack/Nak/Term; `/health` reports `milestone`
      unhealthy; the failed lane is drained while the other consumes; the replacement succeeds. Five transient
      returns: `semstreams_nats_max_delivery_exhaustions_total{consumer="agentrun-milestone-complete"}` = 1. Record
      every stage's pass/fail verbatim in the PR body.

## 10. Gates

- [ ] 10.1 `task lint`, `task test`, `task test:race`, `task schema:generate` with empty `schemas/`/`specs/` drift,
      `task openspec:validate`, `task spec:properties`, `task check:push`; commands and exit codes in the PR body.
- [ ] 10.2 `task e2e:agentic` green on the pushed head (the BREAKING rule, `docs/contributing/02-e2e-tests.md:299`),
      every stage's result verbatim in the PR body.
