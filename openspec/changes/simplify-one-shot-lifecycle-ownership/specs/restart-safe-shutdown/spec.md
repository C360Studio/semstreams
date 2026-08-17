## ADDED Requirements

### Requirement: Running owner shutdown has one exact order

A successfully running owner SHALL fence new admission, initiate concrete native Drain or Shutdown, await exact native
Closed while callback authority remains live, cancel remaining runtime, await owner done/WaitGroup under the shutdown
context, and then perform terminal cleanup. A simple owner with no native callback resource MAY omit Drain/Closed but
SHALL cancel ctx-driven work before awaiting its done/WaitGroup. An M-class owner SHALL observe `startDone` before
selecting the running path or failed-Start `cleanupPending` path.

Deadline expiry SHALL be a failed result. After recording the failed native-close boundary, the owner SHALL still issue
runtime cancellation and continue bounded best-effort join/cleanup where safe; it SHALL NOT wait on ctx-driven owner
done before cancellation or promise later running-generation rejoin.

#### Scenario: native owner closes before runtime cancellation
- **GIVEN** a native callback resource has admitted work
- **WHEN** controlled Stop executes
- **THEN** exact native Closed is observed while callback authority remains live
- **AND** remaining runtime cancellation and owner join follow native closure

### Requirement: Normal lifecycle never deletes durable topology

Owner Stop and Client Close SHALL NOT delete durable consumers. The five `DeleteConsumerOnStop` fields,
`Client.StopConsumer`, `Client.StopAndDeleteConsumer`, and `Client.StopAllConsumers` SHALL retire without aliases.
Namespace-scoped fixture/admin teardown SHALL record and delete only test-created durable identities after owners drain.

#### Scenario: normal Stop preserves durable position
- **GIVEN** a component owns a durable consumer
- **WHEN** its owner stops and Client closes
- **THEN** durable topology is not deleted by lifecycle cleanup

### Requirement: Backlog observation is not lifecycle authority

Exact outstanding-work observation SHALL remain read-only and separate from native lifecycle handles. It SHALL expose
no Stop, Drain, deletion, or cleanup authority. Unknown observation SHALL remain error, never zero.
`NumPending + NumAckPending == 0` SHALL mean no currently outstanding deliverable work and SHALL NOT prove semantic
completion or absence of MaxDeliver-parked work.

#### Scenario: zero backlog is not semantic completion
- **GIVEN** exact observation reports no pending or ack-pending work
- **WHEN** a caller reads outstanding work
- **THEN** the result does not claim semantic completion or absence of parked work

### Requirement: Client Close is terminal transport-only

Client Close SHALL reject new Client work, initiate native connection Drain, observe exact CLOSED, cancel remaining
Client-owned health/metrics runtime, await those workers, perform credential/transport cleanup, and return the observed
aggregate. It SHALL NOT enumerate, rediscover, drain, stop, delete, or wait for component children. Completed repeated
Close SHALL return nil without repeating teardown; concurrent Close and retained-result replay are not contracts.
Preclosed transport, native LastError, and deadline-forced close SHALL remain non-clean. Connect SHALL retain PR #984's
private five-second native flusher ceiling with no adopter knob.

#### Scenario: completed Close is repeated
- **GIVEN** Client Close completed
- **WHEN** Close is called again
- **THEN** it returns nil without repeating teardown

### Requirement: Broad NATS ownership roots retire before release

Client and framework constructors SHALL NOT return raw `*nats.Conn`, `jetstream.JetStream`, `jetstream.Stream`,
`jetstream.KeyValue`, `jetstream.ObjectStore`, or equivalent broad mutable ownership roots. Broad injected roots SHALL
narrow to the measured local method set unless separately approved inventory proves a named adapter boundary where the
caller already owns the root and the callee owns neither close nor rediscovery; the approved inventory has no exception.

Reviewed native message, config, value, watcher, lister, and future seams MAY remain only with caller-bounded operation
or acquisition and explicit local Stop/completion ownership. No `Unsafe*` alias SHALL preserve a retired root. Sister
repositories remain read-only and migrate in their own work.

#### Scenario: constructor inventory has no broad root
- **WHEN** exported Client/framework constructors are enumerated before the breaking tag
- **THEN** no broad mutable native ownership root is returned
- **AND** every retained narrow seam names context and Stop/completion ownership

### Requirement: Durable settlement distinguishes completed from unfinished work

A durable handler SHALL ACK only after required effects and publications commit. During graceful drain an admitted
handler MAY complete and ACK while callback authority and Stop deadline permit. Otherwise it SHALL remain unacknowledged
or NAK under existing redelivery policy. Cancellation SHALL NOT fabricate ACK or suppress settlement error.

The NATS connection SHALL flush and drain accepted outbound publications before clean close. Clean shutdown SHALL mean
accepted callbacks either committed and settled or remain recoverable through their durable primitive.

Every external-effect lane SHALL declare stable idempotency, durable progress/outbox, or explicit at-most-once
semantics. A lane claiming server-confirmed source settlement SHALL use `DoubleAck(ctx)` under a declared latency and
failure SLO; plain ACK SHALL NOT claim synchronous server confirmation, and confirmation failure remains replay-safe.

#### Scenario: completed work settles before exit
- **GIVEN** an admitted handler commits its effect and required publication during drain
- **WHEN** shutdown awaits exact native Closed
- **THEN** the handler ACKs only after commit
- **AND** clean exit retains the semantic result

#### Scenario: unfinished work survives restart
- **GIVEN** an admitted handler cannot commit before the deadline
- **WHEN** failed graceful shutdown exits
- **THEN** no success ACK is fabricated
- **AND** durable consumption can redeliver after restart

#### Scenario: outbound publication is flushed
- **GIVEN** a completed callback accepted outbound NATS publication
- **WHEN** clean shutdown is reported
- **THEN** connection drain flushed it to NATS
- **AND** transport close did not silently discard it

### Requirement: Restart safety is proven across a real process boundary

Every controlled shutdown SHALL terminate the current process. Composition SHALL use one fresh bounded shutdown
context, run every owner through exact ordering, aggregate every Stop result, and call terminal Client Close only after
every owner Stop returns. Nil aggregate produces clean observability and zero exit; any owner/transport failure produces
failed observability and nonzero exit. Neither authorizes Client reuse or in-process restart; supervision creates a
fresh process and Client.

The breaking boot-activation change SHALL NOT land until real-process tests start SemStreams against retained NATS,
admit in-flight and pending work, send SIGTERM, observe clean and failed exits, and start a new process with changed
desired configuration. Proof SHALL show committed semantic results for acknowledged work, unfinished-work recovery,
no silent loss of accepted callbacks, exclusive new-process listener/consumer ownership, and next-boot configuration.
Repeated controlled restarts SHALL be race-tested in-process where possible and in an E2E process loop.

#### Scenario: desired configuration activates after clean restart
- **GIVEN** generation G and validated desired configuration C-prime
- **WHEN** G exits through controlled shutdown and a new process boots
- **THEN** the new process uses C-prime
- **AND** no runtime from G remains active

#### Scenario: controlled shutdown always exits
- **GIVEN** clean or failed all-owner Stop plus Client Close aggregate
- **WHEN** controlled shutdown completes
- **THEN** process exit status and observability reflect the aggregate
- **AND** supervision starts the next Client/runtime

#### Scenario: proof fails closed
- **GIVEN** any drain, settlement, join, flush, listener release, or next-boot assertion fails
- **WHEN** the release gate evaluates
- **THEN** the gate fails
- **AND** boot-only composition is not releasable

### Requirement: Dirty restart correctness does not depend on shutdown hooks

Work/state whose loss violates restart correctness SHALL use durable JetStream or KV under the communication decision
contract. Core NATS alone SHALL NOT carry crash-critical work or authoritative state; it MAY carry only work whose loss
across power failure is explicit noncritical semantics. Power loss SHALL require no Stop, Drain, defer, finalizer, or
detached cleanup to make committed state recoverable.

Every crash-critical stream and KV bucket SHALL be file backed. Boot SHALL observe and validate each live resource's
storage and declared replica policy, rejecting incompatible or memory-backed resources before claiming crash safety.
The guarantee assumes the declared persistence failure domain survives; loss of every persistent copy is data loss, not
application recovery.

A durable handler SHALL commit required effect before ACK. Crash after effect and before ACK SHALL converge through an
idempotent effect or stable deduplication key. SemStreams SHALL NOT claim exactly-once cross-system effects without a
transactional boundary; output contracts SHALL expose at-least-once behavior and stable idempotency evidence where
supported.

#### Scenario: crash before ACK redelivers safely
- **GIVEN** durable effect committed and power fails before ACK
- **WHEN** restart uses retained NATS state
- **THEN** delivery may redeliver
- **AND** handling converges without an invalid semantic duplicate

#### Scenario: core NATS is excluded from crash-critical storage
- **GIVEN** communication carries required post-restart work or authority
- **WHEN** its primitive is selected
- **THEN** it uses durable JetStream or KV
- **AND** core NATS is allowed only for explicitly loss-tolerant semantics

#### Scenario: incompatible live resource fails admission
- **GIVEN** a crash-critical stream/KV is memory backed or violates declared replica policy
- **WHEN** boot observes the live resource
- **THEN** admission fails naming actual and required persistence posture

#### Scenario: external effect contract is honest
- **GIVEN** output calls beyond the durable NATS transaction boundary
- **WHEN** crash timing can repeat it
- **THEN** output exposes at-least-once and stable idempotency where supported
- **AND** does not claim exactly once

### Requirement: Dirty restart is proven at settlement boundaries

The breaking change SHALL NOT land until a real-process test kills SemStreams without graceful shutdown after delivery,
after durable effect, after required publication, and before ACK, then restarts against retained NATS. A second test
SHALL kill both SemStreams and its isolated NATS server without drain, restart NATS from the same file store, and then
restart SemStreams.

Proof SHALL show no silent loss of crash-critical work, expected redelivery, idempotent semantic convergence, recovery
of durable desired configuration, and honest nontransactional external-effect limits. Every successful boot SHALL
consume latest committed desired state regardless of prior process exit. Clean-exit evidence SHALL NOT be activation
prerequisite, and stale observations from a prior boot SHALL never be current evidence for the new boot.

#### Scenario: kill-point matrix recovers
- **GIVEN** deterministic kill points around delivery, effect, publication, guard/ledger, and ACK
- **WHEN** each kills the process and a new process boots
- **THEN** retained durable state drives expected recovery
- **AND** no test relies on graceful cleanup

#### Scenario: NATS process loss recovers from file storage
- **GIVEN** crash-critical resources are file backed and persistent store remains intact
- **WHEN** SemStreams and NATS terminate without drain and NATS restarts from that store
- **THEN** desired facts and unacknowledged work remain available
- **AND** no recovery claim is made if every declared persistent copy was destroyed

#### Scenario: power loss does not pin old configuration
- **GIVEN** desired C-prime committed while boot B runs C
- **WHEN** power loss prevents every shutdown hook and boot B-prime succeeds
- **THEN** B-prime selects C-prime from retained desired state
- **AND** missing clean-exit evidence for B does not suppress C-prime
- **AND** stale B observations are never current evidence for B-prime
