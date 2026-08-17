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

### Requirement: Graceful NATS teardown is owned by exact returned handles

Every owner of a managed JetStream consumer SHALL retain the exact handle returned by consume setup, invoke native
`ConsumeContext.Drain` through that handle during `Stop(ctx)`, and wait for its `Closed` channel before canceling the
Start-owned handler authority. Every owner of a core NATS subscription SHALL retain the exact returned subscription
wrapper, invoke native subscription Drain, and wait for authoritative closure before Start cancellation.

Graceful shutdown SHALL NOT use `ConsumeContext.Stop` or subscription `Unsubscribe`, because those operations may
discard buffered delivery. An abrupt Stop/Unsubscribe path SHALL be non-clean even if later transport Close succeeds.
Client SHALL NOT catalog, rediscover, drain, abort, delete, or compensate for child resources during Close.

Managed-consumer outstanding-work observation SHALL be handle-local. `ManagedConsumer.OutstandingWork(ctx)` SHALL read
the exact consumer bound to that handle under the caller context. `Client.OutstandingWork(stream,name)` and equivalent
name-rediscovery aliases SHALL NOT exist.

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

#### Scenario: Outstanding work uses exact ownership

- **GIVEN** an owner retained the managed-consumer handle returned by setup
- **WHEN** readiness or in-flight accounting queries outstanding work
- **THEN** it calls `OutstandingWork(ctx)` on that exact handle
- **AND** Client does not rediscover a consumer from caller-supplied stream and durable names

#### Scenario: Owner drain precedes Start cancellation

- **GIVEN** an owner has admitted a callback under its Start authority
- **WHEN** controlled Stop begins
- **THEN** the owner closes admission and drains its exact handle while that callback authority remains live
- **AND** it cancels and joins remaining Start-owned work only after authoritative drain completion

#### Scenario: Abrupt child stop cannot become false-clean

- **GIVEN** an owner uses consumer Stop or subscription Unsubscribe during controlled shutdown
- **WHEN** final transport Close later succeeds
- **THEN** the owner result remains failed
- **AND** Client Close does not relabel discarded or unproved callback work as clean

### Requirement: Exact managed-consumer deletion is private, drain-first, and fenced

`ManagedConsumer.DrainAndDelete(ctx)` SHALL be the only graceful durable-consumer deletion surface. Consume setup
SHALL bind private deletion authority to the exact stream and durable identity acquired by that handle. Client SHALL
NOT expose a name-routed administrative graceful delete or rediscover deletion authority from mutable catalog state.

DrainAndDelete SHALL rejoin the handle's one native drain and SHALL NOT begin deletion until exact `Closed` is
observed. A drain deadline SHALL prevent deletion and permit a later caller to rejoin. Concurrent or repeated callers
SHALL issue at most one bound deletion and SHALL observe its one retained terminal result. Consumer-not-found after
drain SHALL be benign success; any other deletion failure, including an ambiguous deadline, SHALL remain non-clean and
SHALL NOT be retried in process.

If consume setup fails after partial acquisition, it SHALL NOT publish a handle. It SHALL clean only the exact partial
resources it acquired and SHALL fence duplicate or stale cleanup from deleting another owner's durable. Exact acquired
identity, not a later name lookup, SHALL be the deletion authority.

#### Scenario: Exact-handle deletion cannot outrun drain

- **GIVEN** a managed-consumer owner requests graceful durable deletion
- **WHEN** `DrainAndDelete(ctx)` runs
- **THEN** it initiates and joins the exact handle's native Drain before durable deletion
- **AND** an incomplete drain prevents deletion

#### Scenario: Repeated deletion has one retained result

- **GIVEN** concurrent or repeated callers request deletion through one exact handle
- **WHEN** drain completes
- **THEN** at most one bound deletion runs
- **AND** every caller observes the same retained terminal result or its own waiting-context error

#### Scenario: Partial acquisition cannot delete foreign state

- **GIVEN** consume setup acquired only part of its exact resource set and then failed
- **WHEN** rollback runs concurrently with another owner or a later setup
- **THEN** no handle is published for the failed setup
- **AND** cleanup is fenced to the exact partial acquisition and cannot delete a duplicate or stale namesake

### Requirement: Client Close is terminal transport-only and conservatively truthful

Client Close SHALL reject new Client work, cancel and exactly join only Client-owned health and metrics workers,
register native CLOSED observation before initiating connection Drain, wait for CLOSED, clear native authority, and
retain one terminal result. Repeated Close calls SHALL serialize and return that retained result; they SHALL NOT start
another drain or cleanup generation.

An installed connection already closed before Client Close SHALL produce retained non-clean preclosed-transport
failure even when native `LastError()` is nil. Any non-nil `LastError()` observed before drain or after CLOSED SHALL
conservatively make the result non-clean; Client SHALL NOT add drain-window callback state to infer historical errors
away. Caller deadline MAY force native Close, but the retained result SHALL name failure and Close SHALL still join its
owned workers and CLOSED observation without detached cleanup.

Connect SHALL install private framework-owned `nats.FlusherTimeout(5*time.Second)`. No option, config, or environment
surface SHALL expose that timeout. A blocked native write or flush SHALL fail within that ceiling so controlled
shutdown can report failure and exit.

The core subscription wrapper SHALL expose Drain as its only graceful lifecycle operation. It SHALL expose no Abort or
Unsubscribe method. Client Close SHALL NOT enumerate, drain, abort, delete, or wait for managed consumers or core
subscriptions and SHALL NOT claim their accepted work settled.

#### Scenario: Preclosed installed transport is not clean

- **GIVEN** Client still owns an installed connection that was closed outside Client
- **AND** native LastError is nil
- **WHEN** Client Close runs
- **THEN** it returns retained non-clean preclosed-transport failure

#### Scenario: Historical transport error is conservatively non-clean

- **GIVEN** LastError is non-nil before native drain begins
- **WHEN** native drain later reaches CLOSED
- **THEN** Close still returns non-clean transport-history failure
- **AND** it creates no drain-window callback state to infer the error away

#### Scenario: Repeated Close returns one terminal result

- **GIVEN** one Close has started or completed terminal transport drain
- **WHEN** another caller invokes Close
- **THEN** it waits for or observes the same terminal operation
- **AND** it returns the retained result without a second drain or detached cleanup

#### Scenario: Blocked native write has a private framework ceiling

- **GIVEN** a native socket write or flush cannot make progress
- **WHEN** the private flusher timeout elapses
- **THEN** the operation fails within five seconds and controlled shutdown can report failure
- **AND** no adopter-facing timeout knob exists

### Requirement: Broad NATS ownership roots retire before release

Client and framework constructors SHALL NOT return raw `*nats.Conn`, `jetstream.JetStream`, `jetstream.Stream`,
`jetstream.KeyValue`, `jetstream.ObjectStore`, or an equivalent broad mutable ownership root. Broad injected native
roots SHALL narrow to the measured local method set unless a separately approved named adapter boundary proves that
the caller already owns the root and the callee owns neither transport close nor rediscovery; the approved inventory
contains no such exception.

Reviewed native message, config, value, watcher, lister, and future seams MAY remain only when caller context bounds
operation or acquisition and local Stop/completion ownership is explicit. No `Unsafe*` compatibility alias SHALL
preserve a retired root. Sister repositories SHALL remain read-only to this change and migrate in their own work.

#### Scenario: Framework constructor does not leak a mutable root

- **WHEN** exported Client and framework constructor signatures are enumerated before the breaking tag
- **THEN** no broad mutable native ownership root is returned
- **AND** every retained narrow watcher, lister, or future seam names caller context and Stop/completion ownership

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

Every controlled shutdown SHALL terminate the current process. Composition SHALL use one fresh bounded shutdown
context, stop admission owners, aggregate every owner Stop result, and call terminal Client Close only after every
owner Stop returns. A nil aggregate SHALL produce clean observability and successful exit. Any owner or transport
failure SHALL produce failed observability and nonzero exit. Neither result SHALL authorize Client reuse or an
in-process runtime restart; supervision SHALL start a fresh process with a newly constructed Client.

The breaking boot-activation change SHALL NOT land until real-process tests start SemStreams against retained NATS
state, admit both in-flight and pending work, send SIGTERM, observe clean and failed exits, and start a new process with
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

#### Scenario: Controlled shutdown always exits

- **GIVEN** either a clean or failed all-owner Stop plus Client Close aggregate
- **WHEN** controlled shutdown completes
- **THEN** the current process exits with corresponding status and observability
- **AND** supervision, not the old process, starts the next Client and runtime

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
