# agent-run-milestones Specification

## Purpose
AgentRun's milestone subscriber is the framework's consumer of loop terminals (`agent.complete.*`, `agent.failed.*`)
on behalf of the product handlers a workflow registers. This capability owns how one stored terminal delivery reaches
every handler as one replay-safe unit: the terminal's wire identity travels to the handlers so they can be idempotent
across redelivery, "handler done" means a committed durable consequence for that identity, the fanout settles on the
aggregate of every handler's outcome so a partial fanout is never acknowledged, run-resolution failures are classified
on the AgentRun side, and a latched lane is visible on `/health` and `/metrics`. It is a settlement contract over the
typed heartbeat surface, not a workflow engine: what a handler does with a terminal belongs to the product.
## Requirements
### Requirement: milestone fanout settles as one replay-safe unit

`MilestoneSubscriber` SHALL settle each `agent.complete.*` and `agent.failed.*` delivery through the typed heartbeat
settlement surface with one `DeliveryWork` per delivery, and SHALL treat the registered handler set as one unit: every
attempt invokes every registered handler in registration order under a per-handler recover, and the delivery is
acknowledged only on an attempt where every handler returned nil. The aggregate decision SHALL be a pure function of
the ordered outcome list: any fatal outcome (panic, `errs` Fatal, or an unclassified handler error) → Quarantine; else
any transient outcome → Retry; else any invalid outcome → Terminate; else Ack. Every attempt of one stored delivery
SHALL present the same `LoopTerminalEvent.SourceMessageID`, the terminal's wire message identity, to every handler. A
handler SHALL return nil only after its durable consequence for that identity is committed, or it has nothing to do
for it; the framework does not verify this obligation and documents it on the handler type.

Resolution failures SHALL be classified on the AgentRun side, never by editing `pkg/lifecycle`: `ErrEntityNotFound` →
continue with a nil run; `ErrEntityNotLifecycleManaged` → Retry (the ADR-049 forward-reference case, resolved by a
later `Manager.Create`); `ErrWorkflowNotRegistered` → Quarantine (a process-wide composition defect); a decoded
terminal naming no run and no loop, a non-`*AgentRun` result, and every `ResolveRun` entity-ID grammar, parent-type,
hop-bound, or non-string-value failure → Terminate with a non-nil `errs` Invalid cause; every other error by its `errs`
class — Fatal → Quarantine, unknown → Retry. Retry is bounded by the lane's finite `MaxDeliver`. Every non-Ack decision
SHALL emit exactly one log line carrying the source identity and exactly one increment of
`semstreams_agentrun_milestone_decisions_total{lane,decision,reason}`.

The `reason` label set SHALL be closed and SHALL be exactly these ten, one per decision row: `decode` (bytes that are
not a readable terminal); `not_managed` (`ErrEntityNotLifecycleManaged`); `composition` (`ErrWorkflowNotRegistered`);
`resolution_type` (the lifecycle participant is not an `*AgentRun`); `resolution_invalid` (an entity-ID grammar,
parent-type, hop-bound or non-string-value failure, and a terminal naming no run and no loop); `resolution_fatal` (a
Fatal-classified resolution failure); `resolution_transient` (a resolution failure no `errs` class places);
`handler_invalid` (every handler that spoke rejected the input); `handler_transient` (a handler is not ready yet);
`handler_fatal` (a handler panicked or failed unplaceably). Adding, renaming or retiring a label is a change to this
requirement, never an implementation detail.

#### Scenario: a partial fanout is not acknowledged

- **GIVEN** two registered handlers
- **WHEN** the first returns nil and the second returns a transient error
- **THEN** the delivery is Naked with the lane's retry delay, not Acked
- **AND** the next attempt invokes both handlers with the same `SourceMessageID`

#### Scenario: the aggregate is pure over the ordered outcomes

- **WHEN** an attempt's outcome list contains any fatal outcome
- **THEN** the decision is Quarantine regardless of position or of the other outcomes
- **AND** transient outranks invalid, and invalid outranks done, in the same way

#### Scenario: a quarantined delivery is left to JetStream

- **WHEN** a handler panics
- **THEN** no Ack, Nak, or Term is attempted for that delivery
- **AND** that lane admits no further local work and `DeliveryFatal()` is non-nil

#### Scenario: a not-yet-managed entity retries and then succeeds

- **GIVEN** `Manager.Get` returns `ErrEntityNotLifecycleManaged`
- **WHEN** the delivery is attempted
- **THEN** the decision is Retry with reason `not_managed`
- **AND** after `Manager.Create` the next attempt resolves the run and can Ack

#### Scenario: an unregistered workflow quarantines rather than dropping every milestone

- **WHEN** `Manager.Get` returns `ErrWorkflowNotRegistered`
- **THEN** the decision is Quarantine with reason `composition`
- **AND** the milestone service reports unhealthy with that cause

#### Scenario: a deterministic resolution defect terminates with a cause

- **WHEN** the terminal decodes but resolution fails on entity-ID grammar, a parent that is not a loop entity, the
  hop bound, a non-string predicate value, or a non-`*AgentRun` result
- **THEN** the decision is Terminate with a non-nil `errs` Invalid cause

#### Scenario: every non-Ack decision is observable

- **WHEN** any decision other than Ack is reached
- **THEN** exactly one log line carries `source_message_id`, `loop_id`, `category`, `lane`, and `reason`
- **AND** the decisions counter increments exactly once with matching labels

### Requirement: milestone lanes are finite, observed, and report fatal ownership loss

Both milestone durables (`agentrun-milestone-complete`, `agentrun-milestone-failed`) SHALL declare a finite `MaxDeliver`
(5) with `AckWait` 30s and a `HeartbeatDeliveryPolicy` validated before acquisition, and SHALL settle Retry through
`DelayedDeliveryRetry(30s)`. Exhaustion is observed by `max-delivery-observability`'s
`semstreams_nats_max_delivery_exhaustions_total{consumer}`; no other exhaustion signal is introduced.
`MilestoneSubscriber` SHALL expose `DeliveryFatal() error` and `RegisterMetrics(metric.MetricsRegistrar) error`; the
milestone service's `Health()` SHALL report unhealthy with the fatal cause once any lane's owner is stopped (a fatal
work outcome, an `InProgress` failure, or unavailable delivery metadata), and each composition root SHALL call
`RegisterMetrics` after construction. `milestoneConsumerOwner` SHALL remain the sole owner of both consume handles: a
fatal on one lane drains only that lane's exact handle, and `stop()` drains each binding at most once, waits for each
handle's `Closed`, and joins each lane's observer.

#### Scenario: exhaustion is counted, not silent

- **WHEN** a delivery reaches its fifth attempt and is not acknowledged
- **THEN** JetStream emits the MAX_DELIVERIES advisory for that consumer
- **AND** `semstreams_nats_max_delivery_exhaustions_total` increments for `agentrun-milestone-complete` or `-failed`

#### Scenario: a fatal on one lane leaves the other consuming

- **WHEN** the complete lane quarantines
- **THEN** its exact handle is drained once through its `deliverylane.Binding`, and `Health()` reports the cause
- **AND** the failed lane continues to consume, and `stop()` performs no second drain of the complete lane

#### Scenario: both lanes are declared finite

- **WHEN** the subscriber builds its two consumer configurations
- **THEN** each declares `MaxDeliver` 5 and `AckWait` 30s
- **AND** a heartbeat above half of `AckWait` is refused before any consumer is acquired
