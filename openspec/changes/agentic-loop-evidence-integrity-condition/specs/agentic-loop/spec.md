## MODIFIED Requirements

### Requirement: Audit loss degrades loudly and never fails agent work

Every trajectory audit failure SHALL emit `ERROR` with loop ID, attempt ID, bounded kind/stage/reason, and no evidence
body; increment `semstreams_agentic_loop_trajectory_audit_failures_total{stage,kind,reason}` using closed label sets;
and latch the existing component Health degraded with `ErrorCount` and bounded `LastError`. Stages SHALL be exactly
`provider_resolve`, `evidence_get`, `evidence_put`, `evidence_verify`, `fact_encode`, `fact_create`, and `fact_verify`.
Raw backend errors SHALL NOT become metric labels.

Missing configured provider SHALL NOT fail agentic-loop Start. Start SHALL record provider-resolve degradation,
install subscriptions, and continue work. Health SHALL check current provider presence on each call; restoration MAY
clear the live dependency condition, but any prior audit-loss latch SHALL remain degraded for the process lifetime.

If required evidence cannot be resolved, stored, or verified while KV remains usable, agentic-loop SHALL attempt an
ordinary fact with `evidence_capture="missing"`, the computed digest/size when available, a bounded failure reason, and
no fabricated reference. If encoding or immutable fact Create/verification fails, no durable fact or reconstructed gap
claim is required. Logs, metrics, and Health remain the operational evidence.

The prohibition on durable records is a prohibition on FABRICATION, not on observation. A record that reconstructs lost
evidence, names what is missing, asserts a repair, or claims the trajectory is complete SHALL NOT be manufactured. A
classification of a failure the component itself observed is not such a record, and the loop-level evidence-integrity
condition below is REQUIRED rather than forbidden. Nothing in this requirement licenses a durable claim that evidence
IS complete.

No audit failure SHALL reject, NAK, cancel, or fail the agent work. The existing state transition, downstream publish,
and source ACK SHALL proceed with their original work result.

#### Scenario: evidence failure records an honest observation when KV is usable

- **GIVEN** Store resolution, Get, Put, or verification fails for required evidence
- **WHEN** the fact bucket remains usable
- **THEN** agentic-loop attempts a fact with `evidence_capture="missing"` and no fabricated reference
- **AND** the failure logs, increments bounded metrics, degrades Health, and does not block work publication or ACK

#### Scenario: fact failure leaves no invented durable gap

- **GIVEN** fact encoding, size validation, Create, or verification ultimately fails
- **WHEN** the work handler continues
- **THEN** ERROR, bounded metric, and degraded Health report the audit loss
- **AND** no counter, seal, gap fact, repair record, or reconstruction of the lost evidence is manufactured
- **AND** no durable claim that the trajectory IS complete is written
- **AND** the existing work transition, publication, and ACK still occur

#### Scenario: missing provider starts degraded and continues work

- **GIVEN** agentic-loop's configured evidence provider is absent after provider startup
- **WHEN** agentic-loop starts
- **THEN** it installs subscriptions with Health degraded and provider-resolve telemetry emitted
- **AND** later work still publishes and ACKs despite failed evidence capture

## ADDED Requirements

### Requirement: Observed audit loss MUST be readable from the loop entity as a classified condition

A loop for which at least one trajectory audit failure was observed SHALL carry
`agent.loop.evidence-integrity` with the value `incomplete` on its loop execution entity, stamped on the same terminal
graph write that carries `agent.loop.outcome`. The predicate SHALL be absent on every other loop, and its absence SHALL
mean only that no audit loss was observed — never that evidence is complete. The predicate SHALL NOT carry a stage,
kind, reason, attempt, or any reconstruction of the lost evidence; those remain in the `ERROR` log and the bounded
counter.

The condition SHALL be derived from the same observed failure value that already feeds the Health latch, the metric,
and the log, and SHALL NOT be derived by re-evaluating any predicate or by reading the counter.

#### Scenario: a loop with observed audit loss is machine-readable as incomplete

- **GIVEN** a loop for which at least one trajectory audit failure was observed at any stage
- **WHEN** the loop reaches its terminal graph write
- **THEN** the loop execution entity carries `agent.loop.evidence-integrity` with value `incomplete`
- **AND** the triple is written on the same mutation that carries `agent.loop.outcome`, not a separate write

#### Scenario: a loop with no observed audit loss carries no claim

- **GIVEN** a loop for which no trajectory audit failure was observed
- **WHEN** the loop reaches its terminal graph write
- **THEN** the loop execution entity carries no `agent.loop.evidence-integrity` triple
- **AND** no predicate asserts that the loop's evidence is complete

#### Scenario: repeated failures at several stages yield one unqualified condition

- **GIVEN** a loop that observed audit failures at more than one stage
- **WHEN** the loop reaches its terminal graph write
- **THEN** exactly one `agent.loop.evidence-integrity` triple with value `incomplete` is written
- **AND** no stage or reason is elected onto the triple

#### Scenario: a failed condition write does not fail agent work

- **GIVEN** the terminal graph write carrying the evidence-integrity condition fails
- **WHEN** the work handler continues
- **THEN** the existing state transition, downstream publish, and source ACK still proceed
- **AND** the absence of the triple is not readable as complete evidence
