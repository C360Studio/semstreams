# Tasks: AgentRun milestone fanout settlement

> **Seed note (2026-09-22, at the claim PR's opening).** Copied verbatim from the accepted design package (owner
> ruling 2026-09-18 on #1249, "as recommended on all five"; amendment 2026-09-19 on #1341, § 2.6 consumes
> `internal/deliverylane`). The base is `main` at `b7ce8727` (#1341 merged), not the L1 branch named in 1.1: L1–L3 and
> #1341 are all on `main`. Every pin below is pre-change evidence at `0053183d` / L1 `c2a9cef6`; the first
> implementation step is to re-derive them against `b7ce8727` with `sed -n "${n}p"` and bring
> `scripts/inventory-verify.sh inventory.md` to exit 0 at that base before any code. Nothing else in this file was
> edited at seed time.

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof. Landing
choreography (review, archive, merge) lives on the PR checklist (#1230), not here. Pins: an unmarked `file:line` is the
inventory base `0053183d`; the three files that moved at L1 head `c2a9cef6` (`natsclient/delivery_settlement.go`,
`processor/agentic-governance/component.go`, `processor/agentic-loop/component.go`) carry `base :n (L1 :m)`. The
developer stands at the L1 head or above and re-derives with `sed -n` any pin the rebase moves.

## 1. Claim and the rebase-base check

- [ ] 1.1 Open the draft PR against the L1 branch (`claude/gh1327-settle-after-effect`) with `Closes #1249`,
      `Closes #759`, and `implemented-by: <persona>` in the body; declare the Tier 1 incompatible removal (§ 7) there.
- [ ] 1.2 Before any deletion, run `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` at the rebase
      base; expected `agentic/agentrun/agentrun.go:812` only. Anything else: stop and report; do not migrate it here.

## 2. Identity and the handler contract (O3; #759 ruling item 3)

- [ ] 2.1 Add `LoopTerminalEvent.SourceMessageID string`, copied from `agentterminal.Event.SourceMessageID`
      (`internal/agentterminal/terminal.go:67`) at `agentic/agentrun/agentrun.go:586`;
      `test/compat/semteams/agentrun_terminal_compat_test.go:75` stays green.
- [ ] 2.2 Document handler done on `OnLoopTerminal` (`agentrun.go:490`): return nil only after the durable consequence
      for this identity is committed; every replay presents the same identity.
      Test: `TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt`.

## 3. Decision matrix and settlement (design § 2.3–2.4; R1, R2, O1)

- [ ] 3.1 Replace the `ConsumeWithHeartbeat` closure (`agentrun.go:810-819`) with `ValidateHeartbeatDeliveryPolicy` per
      lane and `consumeAdmittedDelivery` over one `DeliveryWork` (L0 head signature
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
      `errs.WrapInvalid(errors.New("terminal names no run and no loop"), "agentrun", "HandleEvent", "resolve")`,
      never nil (`interpretDeliveryWork`, `natsclient/delivery_settlement.go` base `:392`/`:396`, L1 `:414`/`:418`).
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

- [ ] 4.1 Add `agentic/agentrun/delivery_owner.go`: `deliveryLaneAdmission`, its constructor, `admit`, `latch` copied
      verbatim from `processor/agentic-loop/delivery_owner.go:29-63`, and `consumeAdmittedDelivery` from `:74-86`
      (file unchanged at L1 head); `recordDeliveryOwnerFatal` takes a `*MilestoneSubscriber` receiver (loop's `:65`
      has `*Component`). No binding or observer copy (`:88`, `:99`).
- [ ] 4.2 Extend `milestoneConsumerOwner` (`agentrun.go:679-689`): per-lane admission and one observer goroutine per
      lane that records the fatal, drains that lane's exact handle under `o.mu`, and sets
      `completeDrained`/`failedDrained` (`:712-713`) so `stop()` skips the drain and still waits `Closed()` (`:727`);
      both-drain-before-Closed (`:718`) unchanged for any lane still running. Tests (`-race`):
      `TestMilestoneFatalDrainsOnlyTheFailedLane`, `TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain`.
- [ ] 4.3 Control-plane rows: an `InProgress` failure (`delivery_settlement.go` base `:361`/`:366`, L1 `:383`/`:388`)
      and unavailable metadata (base `:379`, L1 `:401`) latch the lane and surface in health:
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
      `AckWait/2` ceiling (`natsclient/delivery_settlement.go:191`, unchanged at L1); semantic retry
      `DelayedDeliveryRetry(30*time.Second)`. Test: `TestMilestoneLanesDeclareFiniteMaxDeliver` (I6, reads both
      consumer configs; fails on 0).
- [ ] 6.2 No new exhaustion signal: the existing advisory counter (`internal/maxdelivery/observer.go:148`) is asserted
      by 9.2.

## 7. Removal of `ConsumeWithHeartbeat` (Tier 1; `Closes #759`)

- [ ] 7.1 Delete the function (`natsclient/heartbeat.go:79`) and `nonCancellationWorkError` (`:37`); delete the
      `Deprecated:` notice (`:75`) if present at the rebase base. Keep `ErrHeartbeatFailed`, `PermanentDeliveryError`,
      `TerminateDelivery`; rewrite the `PermanentDeliveryError` doc (`:18`) to "a binding maps it to
      `DeliveryDecisionTerminate`".
- [ ] 7.2 Invert the ratchet: exact-declaration (`natsclient/consumer_policy_callsite_test.go:426`) and
      exact-caller-set (`:444`) assert absence in every package, in the `NewDurableHandler` retirement shape (`:395`);
      keep the surface guard (`:423`).
- [ ] 7.3 Delete `natsclient/heartbeat_test.go` (407 lines) and `heartbeat_integration_test.go` (133) after porting any
      claim without a twin in `delivery_settlement_test.go` / `delivery_settlement_integration_test.go`; list the
      ported claims in the PR body.
- [ ] 7.4 Commit as `refactor(natsclient)!: remove ConsumeWithHeartbeat`. Record in the PR body that
      `task api:compat:report` shows one incompatible change in `natsclient` (`scripts/api-compat.sh:174`; no
      allowlist or waiver exists) and that this is ADR-106's expected pre-RC descending count, not a red.
- [ ] 7.5 Reword the two foreign test comments naming the helper
      (`storage/objectstore/component_ack_integration_test.go:41`,
      `processor/agentic-tools/outcomes_integration_test.go:202`).

## 8. Specs and docs

- [ ] 8.1 Copy `specs/` verbatim into the change: ADDED `agent-run-milestones`; MODIFIED `jetstream-consumer-policy`
      "semantic heartbeat settlement has one permanent exported surface" against L0's head text (`759bd596`,
      `:73-103`) with all three scenarios restated; REMOVED `nats-streaming` "the legacy helper is a shrinking
      remainder, never a compatibility surface" (L0 `:61-77`) with Reason. `openspec validate
      agentrun-fanout-settlement --strict` is expected red until L0's blocks are in the tree below (protocol § File).
- [ ] 8.2 Seed the `agent-run-milestones` Purpose from `proposal.md` at spec sync.
- [ ] 8.3 Rewrite `docs/operations/migration-beta162-to-beta163.md:1050-1065` (L0 head) with the migration text in
      `proposal.md`; replace `docs/operations/migration-restart-safe-nats-client.md:95-97`; update
      `docs/concepts/33-semantic-settlement.md:99`.
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
