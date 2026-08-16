## ADDED Requirements

### Requirement: Controlled restart quiesces before canceling runtime authority

SIGTERM, SIGINT, and an operator-requested configuration restart SHALL initiate bounded graceful shutdown without
first canceling the Start contexts owned by running services and components. The composition root SHALL provide a live
shutdown context with an explicit deadline and SHALL keep accepted-work contexts live through their drain phase.

Lifecycle owners SHALL stop admitting new work before canceling and joining remaining Start-owned work. A component
with no admission or accepted-work drain MAY proceed directly to cancellation and join. The public lifecycle surface
SHALL NOT add a separate quiesce method unless implementation inventory proves `Stop(ctx)` cannot own the phases.

#### Scenario: SIGTERM starts quiesce with live work authority

- **GIVEN** a running process with accepted NATS work
- **WHEN** the process receives SIGTERM
- **THEN** lifecycle Stop begins under a live bounded shutdown context
- **AND** the runtime parent is not pre-canceled before accepted work can drain

#### Scenario: Deadline is visible failure

- **GIVEN** accepted work cannot settle before the shutdown deadline
- **WHEN** the deadline expires
- **THEN** shutdown returns a typed error naming the incomplete phase and owner
- **AND** the process does not report a clean restart boundary

### Requirement: Graceful NATS teardown uses authoritative native drain

Every managed JetStream consumer SHALL stop new delivery with native `ConsumeContext.Drain` and SHALL wait for its
`Closed` channel. Every core NATS subscription SHALL use native subscription Drain and SHALL wait for authoritative
closure. Graceful shutdown SHALL NOT use `ConsumeContext.Stop` or `Subscription.Unsubscribe`, because those operations
may discard buffered delivery.

Client-wide close SHALL rejoin every remaining consumer and subscription drain before draining and closing the NATS
connection. Caller deadline MAY force transport close, but that path SHALL preserve and report the drain failure and
SHALL NOT launch detached cleanup.

#### Scenario: JetStream callback buffer drains

- **GIVEN** a managed JetStream consumer has delivered callbacks in its local buffer
- **WHEN** its owner stops gracefully
- **THEN** native Drain stops new delivery and every buffered callback finishes before `Closed`
- **AND** owner Stop waits for `Closed` under its caller context

#### Scenario: Core subscription drains instead of unsubscribing

- **GIVEN** a core NATS subscription has queued callbacks
- **WHEN** its owner stops gracefully
- **THEN** native Drain removes interest and completes the queued callbacks
- **AND** graceful Stop does not call `Unsubscribe`

#### Scenario: Repeated Stop rejoins one drain

- **GIVEN** a first Stop initiated native drain but its caller deadline expired
- **WHEN** a later authorized Stop is called
- **THEN** it rejoins the same drain operation
- **AND** no second drain or detached cleanup starts

### Requirement: Durable settlement distinguishes completed from unfinished work

A durable message handler SHALL acknowledge only after its required effects and publications commit. During graceful
shutdown, an already-delivered handler MAY complete and acknowledge while its live work context and Stop deadline
permit. If it cannot complete, it SHALL remain unacknowledged or be negatively acknowledged according to its existing
redelivery policy. Cancellation SHALL NOT fabricate acknowledgement or suppress a settlement error.

The NATS connection SHALL flush and drain accepted outbound publications before clean close. A clean shutdown result
SHALL mean that accepted callbacks either committed and settled or remain recoverable through their durable primitive.

#### Scenario: Completed work settles before exit

- **GIVEN** an in-flight durable handler commits its effect and required publication during drain
- **WHEN** shutdown waits for the callback
- **THEN** the handler acknowledges after commit
- **AND** clean exit retains the committed semantic result

#### Scenario: Unfinished work survives restart

- **GIVEN** an in-flight durable handler cannot commit before the shutdown deadline
- **WHEN** forced termination follows the failed graceful attempt
- **THEN** no success acknowledgement is emitted
- **AND** the durable consumer can redeliver the work after restart

#### Scenario: Outbound publication is flushed

- **GIVEN** a completed callback has accepted an outbound NATS publication
- **WHEN** the process reports clean shutdown
- **THEN** connection drain has flushed the publication to NATS
- **AND** transport close does not silently discard it

### Requirement: Restart safety is proven across a real process boundary

The breaking boot-activation change SHALL NOT land until a real-process test starts SemStreams against retained NATS
state, admits both in-flight and pending work, sends SIGTERM, observes a clean exit, and starts a new process with
changed desired configuration.

The proof SHALL show that acknowledged work has its committed semantic result, unfinished durable work is recovered,
already-accepted callbacks are not silently lost, the new process alone owns listeners and consumers, and next-boot
configuration becomes effective. Repeated controlled restarts SHALL be included under the race detector where
in-process coverage is possible and in an E2E loop for process boundaries.

#### Scenario: Desired configuration activates after clean restart

- **GIVEN** runtime generation G and validated desired next-boot configuration C'
- **WHEN** G exits through the complete graceful protocol and a new process boots
- **THEN** the new process uses C'
- **AND** no runtime from G remains active

#### Scenario: Restart proof fails closed

- **GIVEN** any drain, settlement, join, flush, listener-release, or next-boot assertion fails
- **WHEN** the breaking release gate is evaluated
- **THEN** the gate fails
- **AND** boot-only composition is not considered releasable

### Requirement: Dirty restart correctness does not depend on shutdown hooks

Work or state whose loss would violate restart correctness SHALL use durable JetStream or KV according to the
framework's communication decision contract. Core NATS alone SHALL NOT carry crash-critical work or authoritative
state. Power loss SHALL require no Stop, Drain, deferred function, finalizer, or detached cleanup to make committed
state recoverable.

Every crash-critical stream and KV bucket SHALL use file-backed storage. Boot SHALL verify the live resource's storage
and declared replica policy rather than accept an existing incompatible resource. Memory-backed state SHALL fail
crash-safety admission. The guarantee assumes the declared NATS persistence failure domain survives; destruction of
all persistent NATS copies is data loss and SHALL NOT be described as application-level restart recovery.

A durable handler SHALL commit its required durable effect before acknowledging delivery. Because a crash may occur
after effect commit and before ACK, redelivery SHALL converge through an idempotent effect or stable deduplication key.
SemStreams SHALL NOT claim exactly-once external side effects across systems without a transactional boundary; the
output contract SHALL expose at-least-once behavior and stable idempotency evidence.

#### Scenario: Crash before ACK redelivers safely

- **GIVEN** a durable handler committed its effect but power is lost before ACK
- **WHEN** the process restarts against retained NATS state
- **THEN** the durable consumer may redeliver the message
- **AND** repeated handling converges without an invalid semantic duplicate

#### Scenario: Core NATS is not crash-critical storage

- **GIVEN** a communication path carries work or authoritative state required after restart
- **WHEN** its transport contract is selected
- **THEN** the path uses durable JetStream or KV
- **AND** core NATS may be used only where loss across power failure is an explicit non-critical semantic

#### Scenario: Memory-backed authoritative state fails admission

- **GIVEN** a desired-config KV bucket or crash-critical work stream is memory backed
- **WHEN** restart-safety validation inspects the live NATS resource
- **THEN** validation fails before the process claims crash safety
- **AND** the operator receives the exact incompatible resource and required persistence posture

#### Scenario: External effect contract is honest

- **GIVEN** an output calls a system outside the durable NATS transaction boundary
- **WHEN** crash timing can repeat the call
- **THEN** the output exposes at-least-once behavior and a stable idempotency key where supported
- **AND** SemStreams does not label the effect exactly once

### Requirement: Dirty restart is proven at settlement boundaries

The breaking boot-activation change SHALL NOT land until a real-process test kills SemStreams without graceful
shutdown after delivery, after durable effect, after publication, and before ACK, then restarts it against retained
NATS state. A second test SHALL kill both SemStreams and its isolated NATS server, restart NATS from the same file
store, and then restart SemStreams.

The proof SHALL show no silent loss of crash-critical work, expected redelivery, idempotent semantic convergence,
recovery of durable desired configuration, and honest behavior for any external effect that cannot be transactional.
Every successful boot SHALL consume the latest committed desired state regardless of whether the previous process
exited cleanly. Clean-exit evidence SHALL NOT be an activation prerequisite.

#### Scenario: Kill-point matrix recovers

- **GIVEN** deterministic kill points around effect and settlement boundaries
- **WHEN** each kill point terminates the process and a new process boots
- **THEN** retained durable state drives the expected recovery
- **AND** no test relies on graceful cleanup having run

#### Scenario: NATS process loss recovers from file storage

- **GIVEN** crash-critical resources are file backed and their persistent store remains intact
- **WHEN** SemStreams and NATS both terminate without drain and NATS restarts from that store
- **THEN** desired facts and unacknowledged work remain available to the new SemStreams process
- **AND** recovery makes no claim if every declared persistent copy was destroyed

#### Scenario: Power loss does not pin the previous boot configuration

- **GIVEN** desired configuration C' committed while boot incarnation B still runs C
- **WHEN** power loss prevents every shutdown hook and a new incarnation B' successfully boots
- **THEN** B' selects C' from retained desired state
- **AND** the absence of a clean-exit record for B does not suppress C'
- **AND** stale observations from B are never current evidence for B'
