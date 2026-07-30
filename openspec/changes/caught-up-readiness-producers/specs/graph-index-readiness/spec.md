# graph-index-readiness — Delta

## ADDED Requirements

### Requirement: graph-ingest MUST publish a caught-up readiness envelope

graph-ingest MUST publish the readiness envelope to the `graph-ingest` key in `GRAPH_STATUS` on the
shared status heartbeat, reporting outstanding work as `Lag` measured in **messages** — the sum over
every bound durable consumer of pending plus delivered-but-unacknowledged work. Pending alone is
insufficient: an in-process lane queue holds delivered-but-unacked messages that pending does not
count, so a producer reporting only pending under-reports its own backlog.

`Ready` MUST be true only when that total is zero AND bootstrap is complete. `Ready`, `Lag`, and
`StalenessMs` MUST NOT latch — a new backlog after a caught-up period MUST return the producer to
not-ready. `State` MUST be `degraded` when consumer state cannot be read, because an unreadable
consumer is an unknown backlog, not an empty one.

This envelope is sound only because acknowledgement is the terminal step of the ingest success path:
the graph write, its derived writes, and the durable redelivery-guard stamp all complete before the
message is acknowledged, so an acknowledged message's writes are durable.

#### Scenario: an idle stack reports caught up

- **GIVEN** graph-ingest has applied every delivered message and no new work has arrived
- **WHEN** the status tick publishes
- **THEN** `Lag` is zero and `Ready` is true

#### Scenario: a write burst returns the producer to not-ready

- **GIVEN** graph-ingest previously published `Ready` true
- **WHEN** a burst of arrivals is pending or in flight
- **THEN** the next published envelope reports non-zero `Lag` and `Ready` false

#### Scenario: delivered-but-unacknowledged work counts as outstanding

- **GIVEN** messages have been delivered to the component and are queued internally but not yet
  acknowledged
- **WHEN** the status tick publishes
- **THEN** those messages are counted in `Lag`
- **AND** the producer does not report `Ready`

#### Scenario: unreadable consumer state is degraded, not caught up

- **GIVEN** consumer state cannot be read
- **WHEN** the status tick publishes
- **THEN** `State` is `degraded`
- **AND** the envelope does not report `Ready`

#### Scenario: a deployment with no streaming input is honestly caught up

- **GIVEN** a deployment in which graph-ingest binds no durable consumer
- **WHEN** the status tick publishes
- **THEN** `Ready` is true with zero `Lag`
- **AND** the initial-build size is reported as zero

### Requirement: A backlog producer MUST omit the revision-lag fields

A producer whose work does not arrive in a single revision space MUST omit `IndexedRevision` and
`TargetRevision` from its envelope, and no consumer may perform a read-your-writes comparison against
such a producer.

Those two fields are contractually in the entity-state KV revision space — a caller compares its own
committed revision against `IndexedRevision`, and that comparability is pinned by test. graph-ingest
consumes multiple streams whose sequence spaces are independent, so no single scalar revision exists;
publishing a stream sequence in a KV-revision field would silently corrupt every read-your-writes
check in the system. Absence is the honest answer, not a redefinition of the field.

#### Scenario: the revision fields are absent on the wire

- **GIVEN** a backlog producer publishes its envelope
- **WHEN** a consumer decodes it
- **THEN** `IndexedRevision` and `TargetRevision` are absent
- **AND** the consumer does not attempt a revision comparison against this producer

### Requirement: The rule processor MUST report bootstrap replay completion per watcher generation

The rule processor MUST publish the readiness envelope to the `rule` key, with `BootstrapComplete`
true only when every currently-authoritative entity-watcher generation has observed its
end-of-initial-values sentinel. When a new generation is registered — a watcher recreated because the
watched pattern set changed at runtime — `BootstrapComplete` MUST return to false until that
generation has replayed.

A completion signal that latches for the process lifetime MUST NOT be used, because the watched
pattern set is runtime-mutable: a process-lifetime latch would report bootstrapped while a
newly-added pattern was still replaying, which is the defect this requirement exists to prevent. Each
generation latches against its own fixed sentinel, never a moving target.

`Start` returning MUST NOT be treated as any part of this signal — the processor signals ready before
its watchers are created, and watcher creation may additionally block waiting for the entity-state
bucket to exist.

`State` MUST be `degraded` when the entity-watch lane has latched degraded on watch loss, and
`reset_required` when the contract kill switch has fired.

#### Scenario: replay completion becomes observable

- **GIVEN** the rule processor starts with configured watch patterns
- **WHEN** every watcher generation has observed its end-of-initial-values sentinel
- **THEN** the published envelope reports `BootstrapComplete` true

#### Scenario: a runtime pattern addition returns the processor to not-bootstrapped

- **GIVEN** the processor published `BootstrapComplete` true
- **WHEN** a configuration update registers a watcher generation that has not yet replayed
- **THEN** the next published envelope reports `BootstrapComplete` false
- **AND** it returns to true once that generation observes its sentinel

#### Scenario: zero configured patterns report complete with nothing to do

- **GIVEN** the processor is configured with no entity-watch patterns
- **WHEN** the status tick publishes
- **THEN** `BootstrapComplete` is true
- **AND** the initial-build size is reported as zero

#### Scenario: watcher loss is degraded, not merely not-ready

- **GIVEN** the entity-watch lane has latched degraded after an unexpected watch close
- **WHEN** the status tick publishes
- **THEN** `State` is `degraded`

#### Scenario: Start returning licenses nothing

- **GIVEN** the processor's Start has returned
- **WHEN** a consumer reads the envelope
- **THEN** the consumer relies only on `BootstrapComplete`, never on Start having returned

### Requirement: The envelope MUST report the size of the initial build

The readiness envelope MUST carry an additive `bootstrap_scope`: the size of the initial build the
producer latched against, expressed in that producer's own unit. `BootstrapComplete` true together
with `bootstrap_scope` zero MUST mean authoritatively-nothing-to-do, so a caller can distinguish
"replayed everything" from "there was nothing to replay" — a distinction that is otherwise
unrecoverable from the wire, because the target field carries the live target rather than the
bootstrap target.

The readiness gate MUST NOT read this field. It is caller-specific reporting, exactly like the
revision fields, and it MUST NOT acquire a threshold, tolerance, or minimum-scope parameter: a bound
on a quantity the producer only learns at bootstrap is the same unsatisfiable-knob mistake already
retired from this capability. No consumer may treat `bootstrap_scope` as coverage, and it licenses
nothing about absence.

#### Scenario: an empty replay is distinguishable from a completed one

- **GIVEN** one producer bootstrapped with nothing to replay and another replayed a non-empty set
- **WHEN** a consumer decodes both envelopes
- **THEN** both report `BootstrapComplete` true
- **AND** the first reports `bootstrap_scope` zero while the second reports a non-zero size

#### Scenario: the gate verdict ignores scope

- **GIVEN** two otherwise identical envelopes differing only in `bootstrap_scope`
- **WHEN** the readiness gate evaluates each
- **THEN** the verdict and defer reason are identical

### Requirement: Aggregate readiness MUST be folded by the consumer, never published

Readiness spanning multiple producers MUST be computed by the consumer over a key list the consumer
declares, delegating each key to the single readiness gate rather than reimplementing gate semantics.
No aggregate envelope may be published to `GRAPH_STATUS`: an aggregate is itself a producer whose
staleness reports the aggregator's liveness rather than the producers', and a consumer that defers
needs the per-producer detail anyway.

The framework MUST NOT declare which producers are mandatory. The producer set is deployment-
dependent, so a framework-declared list would fail deployments that legitimately run without a given
producer.

A declared key that is absent or whose feed cannot be vouched for MUST defer as status-unknown — fail
closed — and MUST NOT be interpreted as ready.

A consumer needing coverage rather than health, such as one capturing a comparison snapshot, MUST use
a separately named predicate requiring every declared producer to report zero outstanding work. That
predicate MUST NOT gate any read path, so it cannot be mistaken for the health gate.

#### Scenario: an absent declared producer fails closed

- **GIVEN** a consumer declares a producer key that no component publishes in this deployment
- **WHEN** the consumer folds readiness
- **THEN** the fold defers with the status-unknown reason
- **AND** the consumer does not proceed

#### Scenario: the fold reports which producer caused the defer

- **GIVEN** several declared producers of which one is not ready
- **WHEN** the consumer folds readiness
- **THEN** the deferring key and its typed reason are identified deterministically

### Requirement: An operator MUST be able to read every watched readiness envelope

The gateway MUST expose a read-only surface returning, for each watched `GRAPH_STATUS` key, the
envelope plus the consumer-local facts of whether it is known, whether it is fresh, and its age. It
MUST NOT return a computed aggregate verdict, since the key list belongs to the consumer.

Process-liveness endpoints MUST NOT incorporate data-plane coverage: a healthy process serving under
write backlog is live, and folding coverage into liveness makes it flap.

#### Scenario: a quiet feed is distinguishable from a not-ready producer

- **GIVEN** one producer has published a not-ready envelope and another has published nothing recently
- **WHEN** an operator reads the surface
- **THEN** the first is shown not-ready and the second is shown stale or unknown
- **AND** the distinction requires no log correlation

## MODIFIED Requirements

### Requirement: The readiness envelope is exposed as Prometheus metrics
Every ADR-066 envelope producer SHALL expose the envelope as scrapeable Prometheus gauges in addition
to the `GRAPH_STATUS` KV key. At minimum the gauges are `readiness` (1 when Ready else 0), `lag`,
`bootstrap_complete`, and a `state`-labeled gauge distinguishing building / ready / degraded /
reset_required, plus a counter for failed status publishes.

`indexed_revision` and `target_revision` SHALL be exposed by revision-lag producers (graph-index,
graph-embedding). A backlog producer, whose work does not arrive in a single revision space, SHALL
NOT be required to expose them — and SHALL NOT synthesize a value for them, because a fabricated
revision is worse than an absent one. The `lag` gauge's unit is the producer's own outstanding-work
unit: revisions for a revision-lag producer, messages for a backlog producer.

The gauges MUST reflect the same values the producer's status projection returns and stay fresh
independent of query traffic (refreshed on the same periodic tick that publishes the KV key — one
compute feeds both).

#### Scenario: Readiness and lag are scrapeable without a KV read
- **GIVEN** graph-index is running and catching up under continuous write
- **WHEN** Prometheus scrapes the component
- **THEN** the `readiness`, `lag`, `indexed_revision`, and `target_revision`
  gauges are present and reflect the current `computeIndexStatus` values
- **AND** no KV read is required to observe them

#### Scenario: A backlog producer exposes readiness without fabricating revisions
- **GIVEN** a backlog producer is running with outstanding work
- **WHEN** Prometheus scrapes the component
- **THEN** `readiness`, `lag`, `bootstrap_complete`, and the state gauge are present
- **AND** no revision gauge reports a synthesized value

#### Scenario: State distinguishes catching-up from broken
- **GIVEN** the index is `building` with lag, versus `degraded` or `reset_required`
- **WHEN** an operator inspects the `state`-labeled gauge
- **THEN** the current state is identifiable (so "catching up" can be alerted
  differently from "broken"), not collapsed into `readiness=0`

#### Scenario: Metrics and the KV key stay in agreement
- **GIVEN** the periodic status tick
- **WHEN** the envelope is computed
- **THEN** the same struct is written to the gauges and to `GRAPH_STATUS`,
  never two divergent computations
