# Change: AgentRun milestone fanout settlement

## Why

`MilestoneSubscriber` is the last production caller of `natsclient.ConsumeWithHeartbeat`
(`agentic/agentrun/agentrun.go:812`). That helper acknowledges whatever its callback returns nil for, and `HandleEvent`
returns nil after logging every handler error and every handler panic (`:605`, `:612`, `:621`): a fanout that reached
one of N product handlers is acknowledged as if it reached all N. The terminal's wire identity, normalized at
`internal/agentterminal/terminal.go:123`, is dropped before the handlers see it (`:586`), so a handler that wants to be
idempotent across redelivery has nothing to key on. Nothing on `/health` or `/metrics` shows any of this; the
subscriber has no health or metric surface at all.

The owner ruled on 2026-09-18 (#759) that the helper is removed by the PR that migrates its last caller, without alias
or deprecation window, and on 2026-09-02 (#759 items 3 and 6) that the migration must preserve source identity, define
handler done, design replay and partial failure, and prove complete and failed replacement; a mechanical typed
conversion is forbidden. The five-question design docket was ruled "as recommended on all five" on 2026-09-18 (#1249).

## What changes

- **Identity reaches the handler.** `LoopTerminalEvent` gains `SourceMessageID`, the terminal's wire message id,
  copied from the normalized event. Every attempt of one stored delivery presents the same identity to every handler.
- **Handler done is defined.** `OnLoopTerminal` returns nil only after its durable consequence for that identity is
  committed, or it has nothing to do for it. The framework does not verify the obligation; the #1155 proof handler
  demonstrates it.
- **Whole-fanout idempotent replay.** One `DeliveryWork` per delivery runs every registered handler in registration
  order under a per-handler recover and settles on the aggregate: any fatal → Quarantine, else any transient → Retry,
  else any invalid → Terminate, else Ack. A partial fanout is never acknowledged.
- **Resolution failures are classified on the AgentRun side.** `ErrEntityNotFound` → nil run (unchanged);
  `ErrEntityNotLifecycleManaged` → bounded Retry (the ADR-049 forward-reference case); `ErrWorkflowNotRegistered` →
  Quarantine (a process-wide composition defect); `ResolveRun`'s grammar, parent-type, hop-bound and non-string-value
  failures are wrapped Invalid at their origin and Terminate; everything else settles by its `errs` class, unknown
  defaulting to bounded Retry. `pkg/lifecycle` is not edited.
- **Fatal ownership loss latches health.** Each milestone lane is a `internal/deliverylane` consumer (#1341): its
  `Admission` closes on a fatal outcome, an `InProgress` failure or unavailable delivery metadata, `Observe` drains only
  that lane's exact handle, and `MilestoneService.Health()` reports the cause through `DeliveryFatal()`;
  `milestoneConsumerOwner` retains both bindings and stays the sole owner.
- **Lanes are finite and observed.** Both milestone durables keep `MaxDeliver 5` and `AckWait 30s`, validate their
  heartbeat policy before acquisition, and settle Retry through `DelayedDeliveryRetry(30s)`. Exhaustion is counted by
  the existing `semstreams_nats_max_delivery_exhaustions_total{consumer}`; decisions by a new
  `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}` registered from each composition root.
- **The helper is gone.** `natsclient.ConsumeWithHeartbeat` and `nonCancellationWorkError` are deleted; the caller
  ratchet inverts to assert absence; the two legacy test files are deleted after their claims are ported.

## Impact

- Affected capabilities: `agent-run-milestones` (new, seeded here), `jetstream-consumer-policy` (MODIFIED against L0's
  head text), `nats-streaming` (REMOVED, the L0-added "shrinking remainder" requirement).
- Affected code: `agentic/agentrun/agentrun.go`, (no new file: `agentrun` imports `internal/deliverylane`),
  `agentic/agentrun/nats_reader.go`, `service/milestone_service.go`, `cmd/semstreams/main.go`,
  `cmd/e2e-semstreams/main.go` (one wiring line each, plus the env-gated proof handler), `natsclient/heartbeat.go` and
  its two test files, `natsclient/consumer_policy_callsite_test.go`, two foreign test comments, the e2e agentic
  scenario.
- Exported surface (Tier 1, ADR-106): additions `LoopTerminalEvent.SourceMessageID`, `MilestoneSubscriber.DeliveryFatal()
  error`, `MilestoneSubscriber.RegisterMetrics(metric.MetricsRegistrar) error`; removal `natsclient.ConsumeWithHeartbeat`
  — an incompatible change in a frozen package, declared with a `!` commit; `scripts/api-compat.sh` has no waiver, so
  `task api:compat:report` prints it as one incompatible package, which is ADR-106's expected pre-RC descending count.
  Behavior behind an unchanged signature: `agentrun.ResolveRun` errors gain the `errs` Invalid class (chains kept; one
  in-tree caller, no sister callers).
- New metric: `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}`. No histogram.
- Operator-visible: `/health`, the NATS-published service health and `/services/health` report `milestone` unhealthy
  with the cause after a lane stops; an `InProgress` failure now stops the lane where it was previously a WARN.
- Sister impact: semteams composes the subscriber and registers no handler (`cmd/semteams/main.go:939`); its root has
  an unregistered decisions counter until it adds the `RegisterMetrics` call. SemDev has two direct helper callers and
  four comments sizing `max_deliver 10` on the removed 30s NAK; the migration note below covers them.

## Non-goals

- No supervisor, receipt bucket, checkpoint ledger, or state-machine runtime (#1249 anti-goals); no per-handler
  settlement or receipts — N handlers share one failure domain by design, and the design records that trade.
- No change to `internal/deliverylane` (#1341 owns it); no refusal declarer on the two milestone lanes (#1342's sweep,
  see OQ1).
- No edit to `pkg/lifecycle`; projection failures stay unclassified and retry under the finite ceiling.
- No production handler; no new bucket, stream, subject, or duplicates window.
- No change to any other component's settlement; the five typed owners introduced by #759 and #1327 are untouched.

## Purpose text for the seeded `agent-run-milestones` spec (written at spec sync)

The AgentRun milestone fanout: how a loop terminal on `agent.complete.*` / `agent.failed.*` reaches the product
`MilestoneHandler` set exactly-once-in-effect under JetStream at-least-once delivery — the identity each handler is
handed, what "done" means for a handler, how N handler outcomes settle as one decision, what the framework can and
cannot observe about that, and the two milestone lanes' finite delivery policy and health surface.

## Migration note text (`docs/operations/migration-beta162-to-beta163.md` § #759, replacing "still exported at this tag")

`ConsumeWithHeartbeat` is removed without alias (#1249/#759). Bindings compose `ValidateHeartbeatDeliveryPolicy` +
`ConsumeDeliveryWithHeartbeat` (or `SettleDelivery`/`SettleDeliveryWithRetry`) and return a typed decision from their
own definition of done; nil-means-Ack is gone. `agentrun.ResolveRun` errors now carry the `errs` Invalid class (chains
kept). Known direct callers at beta.160: SemDev `internal/conversationchannel/component.go:476` and
`internal/intake/component.go:378` (heartbeat 20s against AckWait 1m passes the ceiling); three SemDev comments
(`conversationchannel/apply.go:113`, `:202`; `conversationchannel/component.go:435`; `intake/component.go:355`) size
`max_deliver 10` on the removed helper's fixed 30s NAK — `DelayedDeliveryRetry(30*time.Second)` keeps that budget.
