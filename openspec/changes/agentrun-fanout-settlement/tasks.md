# Tasks: AgentRun milestone fanout settlement

> **Re-pin (2026-09-22): DONE.** The architect re-derived every pin against `main` at `b7ce8727` (L0–L3 and #1341/#1357
> all merged), generating each one from `sed -n "${n}p"` rather than transcribing it: 162 pins at `0053183d` became 214.
> `scripts/inventory-verify.sh openspec/changes/agentrun-fanout-settlement/inventory.md` reads
> `pins=214 ok=214 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. The reconciliation of the accepted design
> against that base — 28 rows, seven text corrections (B1–B7), two owner questions — is `reconciliation.md`; the
> per-task old to new pin map is `tasks-pins.md`. From here `scripts/inventory-verify.sh` is EXPECTED to go RED as the
> implementation lands: pins are pre-change evidence, and a landed change is never re-pinned.

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof. Landing choreography
(review, archive, merge) lives on the PR checklist (#1230), not here. Pins: every pin is a `main` line at `b7ce8727`;
the L1 attributions are retired. The developer re-derives with `sed -n` any pin the rebase moves.

## 1. Claim and the rebase-base check

- [ ] 1.1 The claim exists: draft PR #1360 against `main` (base `b7ce8727`) with `Closes #1249`, `Closes #759`, and
      `implemented-by: pending (design-phase claim)`. Before the first implementation push, set
      `implemented-by: <persona>` in the body and keep the Tier 1 declaration (§ 7, wording per reconciliation B4: one
      added incompatible line under the already-counted `natsclient`, package count unchanged at 15). There is no L1
      branch to target: L1 is `94cd8e4c` on `main`.
- [ ] 1.2 Before any deletion, run `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` at the base; on
      `main` at `b7ce8727` that is two hits, the call site `agentic/agentrun/agentrun.go:812` and the declaration
      `natsclient/heartbeat.go:84` (the grep matches `func ConsumeWithHeartbeat(`). Anything else: stop and report; do
      not migrate it here.

## 2. Identity and the handler contract (O3; #759 ruling item 3)

- [ ] 2.1 Add `LoopTerminalEvent.SourceMessageID string`, copied from `agentterminal.Event.SourceMessageID`
      (`internal/agentterminal/terminal.go:67`) at `agentic/agentrun/agentrun.go:586`;
      `test/compat/semteams/agentrun_terminal_compat_test.go:75` stays green.
- [ ] 2.2 Document handler done on `OnLoopTerminal` (`agentrun.go:490`): return nil only after the durable consequence
      for this identity is committed; every replay presents the same identity.
      Test: `TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt`.

## 3. Decision matrix and settlement (design § 2.3–2.4; R1, R2, O1)

- [ ] 3.1 Replace the `ConsumeWithHeartbeat` closure (`agentrun.go:810-819`) with `ValidateHeartbeatDeliveryPolicy` per
      lane (`natsclient/delivery_settlement.go:155`) and `deliverylane.Consume`
      (`internal/deliverylane/deliverylane.go:105`) under the `admitted` guard (reconciliation B1) over one
      `DeliveryWork` (`natsclient/delivery_settlement.go:35`, signature unchanged:
      `func(context.Context, []byte) (DeliveryDecision, error)`); decode failure → Terminate (`decode`).
- [ ] 3.2 Classify resolution on the AgentRun side: `errors.Is` against `ErrEntityNotFound`
      (`pkg/lifecycle/manager.go:205`) → nil run; `ErrEntityNotLifecycleManaged` (`:244`) → Retry (R2);
      `ErrWorkflowNotRegistered` (`:169`) → Quarantine (R1); otherwise `errs.Classify` (`pkg/errs/errs.go:280`):
      Invalid → Terminate, Fatal → Quarantine, else Retry. Covers `agentrun.go:637`, `:641` (Terminate
      `resolution_type`), `:653`, `:657`; the nil-reader error (`manager.go:198`) and projection failures (`:250`)
      settle as bounded Retry. `pkg/lifecycle` is not edited.
- [ ] 3.3 Wrap `errs.WrapInvalid` (`errs.go:435`) at the AgentRun-side origins: `agentrun.go:393`, `:405`, `:436`,
      `:455`, `:462`, `nats_reader.go:68` (chains kept, no signature change).
- [ ] 3.4 The dead identity guard (`agentrun.go:646-647`) Terminates with cause
      `errs.WrapInvalid(errors.New("terminal names no run and no loop"), "agentrun", "HandleEvent", "resolve")`, never
      nil (`interpretDeliveryWork`, `natsclient/delivery_settlement.go:403`/`:407`).
- [ ] 3.5 Every attempt runs every handler in registration order under the per-handler recover (`agentrun.go:605`,
      `:612`); collect outcomes; aggregate fatal > transient > invalid > Ack (O1). Tests:
      `TestMilestoneAggregateIsPureOverOrderedOutcomes` (rapid property, I4),
      `TestMilestoneFanoutAcksOnlyWhenEveryHandlerReturnsNil` (I1),
      `TestMilestoneFanoutRetriesOnTransientHandlerError`, `TestMilestoneFanoutTerminatesOnAllInvalid` (O1),
      `TestMilestoneFanoutQuarantinesOnHandlerPanic` (I3).
- [ ] 3.6 Resolution tests: `TestMilestoneNotManagedEntityRetriesThenAcksAfterCreate` (I7/R2: Retry on attempt 1,
      `Manager.Create`, Ack on attempt 2), `TestMilestoneUnregisteredWorkflowQuarantinesAndLatches` (R1),
      `TestMilestoneResolutionInvalidTerminates` (grammar, non-string value, non-`*AgentRun`),
      `TestMilestoneNilReaderAndProjectionFailuresRetry`.
- [ ] 3.7 Log and counter on every non-Ack decision (I5): one line with `source_message_id`, `loop_id`, `category`,
      `lane`, `reason`; one increment. Test: `TestMilestoneNonAckDecisionsLogOnceAndCountOnce`.
- [ ] 3.8 Mutation evidence for the wiring, not the primitive: `cp` `agentrun.go` aside; delete the aggregate call so
      the closure returns Ack unconditionally; run 3.5's tests and record the failing names; restore and `shasum` the
      restored file against the backup. Then delete the `SourceMessageID` copy at `:586` and record 2.2's failure the
      same way.

## 4. Admission latch and handle owner (design § 2.6)

- [ ] 4.1 Per lane, consume `internal/deliverylane` instead of copying the latch (there is no
      `agentic/agentrun/delivery_owner.go`; `processor/agentic-loop/delivery_owner.go` was deleted at `b7ce8727` by
      #1357): `deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)` (`internal/deliverylane/deliverylane.go:45`),
      `deliverylane.Consume` (`:105`), `deliverylane.NewBinding(handle)` (`:194`),
      `deliverylane.Observe(runCtx, binding, admission, react)` (`:225`; `react` log-only, non-nil).
      `recordDeliveryOwnerFatal` keeps its `*MilestoneSubscriber` receiver as the `onFatal` feeding `DeliveryFatal()`.
- [ ] 4.2 Replace the hand-rolled handle state in `milestoneConsumerOwner` (`agentrun.go:679-689`) with two
      `*deliverylane.Binding`: the two `jetstream.ConsumeContext` fields (`:681-682`) and the two drained flags
      (`:683-684`) go, and no observer goroutine is hand-rolled — `Observe` owns it. `stop()` = `Drain()` both
      (both-drain-first, `:718`, holds) → await both `Closed()` (`:727`, `:730`) → `o.cancel()` (`:743`) → join both
      `Done()` (`internal/deliverylane/deliverylane.go:216`); reconciliation B3. The force `Stop()` fallback
      (`:734-737`) is owner question OQ2 in `reconciliation.md`. Tests (`-race`):
      `TestMilestoneFatalDrainsOnlyTheFailedLane`, `TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain`.
- [ ] 4.3 Control-plane rows: an `InProgress` failure (`natsclient/delivery_settlement.go:372`/`:377`) and unavailable
      metadata (`:390`) latch the lane and surface in health:
      `TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner`.

## 5. Health and metrics (O3; design § 2.7)

- [ ] 5.1 Add `MilestoneSubscriber.DeliveryFatal() error`. `MilestoneService.Health()` override (the
      `service/base.go:209` pattern) type-asserts `interface{ DeliveryFatal() error }` on the `milestoneStarter`
      (`service/milestone_service.go:21-22`, unchanged) and returns `health.NewUnhealthy("milestone", …)`.
      Test: `TestMilestoneServiceHealthReportsDeliveryFatal`, observed through `/health`
      (`service/service_manager.go:1302` → `:1722` → `:1736`).
- [ ] 5.2 Add `MilestoneSubscriber.RegisterMetrics(r metric.MetricsRegistrar) error` registering
      `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}` via `RegisterCounterVec`
      (`metric/registry.go:216`); the vec is built in `NewMilestoneSubscriber` (`agentrun.go:521`). Wire one call after
      construction in `cmd/semstreams/main.go:347` and `cmd/e2e-semstreams/main.go:272` (`metricsRegistry` in scope:
      `:170`, `:159`). Not `Service.RegisterMetrics` (`service/base.go:389`; nothing calls it).
      Test: `TestMilestoneDecisionsCounterIsRegisteredOnce`.
- [ ] 5.3 Mutation evidence: delete the `RegisterMetrics` call in one root; run the e2e `verify-streaming-metrics`
      stage; record the failure; restore and checksum.

## 6. Consumer policy (O2; design § 2.8)

- [ ] 6.1 Keep `MaxDeliver 5` (`agentrun.go:832`) and `AckWait 30s` (`:833`) on both lanes; heartbeat 10s under the
      `AckWait/2` ceiling (`natsclient/delivery_settlement.go:181`); semantic retry
      `DelayedDeliveryRetry(30*time.Second)`. Test: `TestMilestoneLanesDeclareFiniteMaxDeliver` (I6, reads both consumer
      configs; fails on 0).
- [ ] 6.2 No new exhaustion signal: the existing advisory counter (`internal/maxdelivery/observer.go:148`) is asserted
      by 9.2.

## 7. Removal of `ConsumeWithHeartbeat` (Tier 1; `Closes #759`)

- [ ] 7.1 Delete the function (`natsclient/heartbeat.go:84`) and `nonCancellationWorkError` (`:37`); the doc comment at
      `:75-83`, which names #1249 as the deleting PR, goes with the function (there is no `Deprecated:` marker on
      `main`). Keep `ErrHeartbeatFailed`, `PermanentDeliveryError`, `TerminateDelivery`; rewrite the
      `PermanentDeliveryError` doc (`:18`) to "a binding maps it to `DeliveryDecisionTerminate`".
- [ ] 7.2 Invert the ratchet: exact-declaration (`natsclient/consumer_policy_callsite_test.go:428`) and exact-caller-set
      (`:446`) assert absence in every package, in the `NewDurableHandler` retirement shape (`:395`); keep the surface
      guard (`:425`).
- [ ] 7.3 Delete `natsclient/heartbeat_test.go` (407 lines) and `heartbeat_integration_test.go` (133) after porting any
      claim without a twin in `delivery_settlement_test.go` / `delivery_settlement_integration_test.go`; list the
      ported claims in the PR body.
- [ ] 7.4 Commit as `refactor(natsclient)!: remove ConsumeWithHeartbeat`. Record in the PR body that
      `task api:compat:report` at `b7ce8727` lists 15 incompatible Tier 1 packages against `v1.0.0-beta.162`,
      `natsclient` (`NewDurableHandler: removed`) and `agentic/agentrun` (`EntityIDPattern`, `Mint`) among them; this
      layer adds one line, `ConsumeWithHeartbeat: removed`, under the already-counted `natsclient` and only compatible
      additions under `agentic/agentrun`, so the package count does not move. The commit is still `!`; the posture is
      still ADR-106's pre-RC descending count (`scripts/api-compat.sh:174`; no allowlist or waiver exists).
- [ ] 7.5 Reword the two foreign test comments naming the helper
      (`storage/objectstore/component_ack_integration_test.go:41`,
      `processor/agentic-tools/outcomes_integration_test.go:216`).

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
