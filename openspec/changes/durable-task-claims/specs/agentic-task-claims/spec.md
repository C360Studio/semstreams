# agentic-task-claims — delta (durable-task-claims)

## Purpose

Durable, atomic idempotency for agentic task acceptance: a write-once claim keyed by TaskID
that binds the task to its LoopID and initial RequestID independently of retained message
bytes, so redelivery, restart, eviction, and operator purges cannot cause a second execution
or a second provider charge for the same logical task.

## ADDED Requirements

### Requirement: A TaskID claim MUST have exactly one winner

The system MUST accept a given TaskID at most once, via an atomic create of a write-once
claim record; concurrent claimers of the same TaskID MUST resolve to exactly one winner, and
every loser MUST be able to read the winning claim.

#### Scenario: Two concurrent claimers

- **WHEN** two handlers attempt to claim the same TaskID concurrently
- **THEN** exactly one claim create succeeds, the other receives the key-exists conflict, and
  the loser's subsequent read returns the winner's claim record

### Requirement: The claim MUST bind the full identity chain at claim time

A claim record MUST carry the TaskID, the LoopID, and the initial RequestID, all fixed before
any side effect of task execution, and the record MUST be immutable after creation.

#### Scenario: Identity fixed before side effects

- **WHEN** a task is accepted
- **THEN** the claim record containing its LoopID and initial RequestID is durably committed
  before any loop entity is persisted or any initial request is published

#### Scenario: No update path

- **WHEN** any component attempts to modify an existing claim record
- **THEN** no API exists to do so; execution state is read from loop state, never from the
  claim

### Requirement: A claim MUST survive independently of retained message bytes

A committed claim MUST remain readable after eviction of the task message from its stream,
after a NATS restart, and after an operator purge of the AGENT stream, for at least the
configured claim retention horizon; claim retention MUST exceed the AGENT stream's retention.

#### Scenario: Redelivery after eviction and restart

- **WHEN** a task message is evicted under the stream's byte ceiling, NATS restarts, and an
  upstream trigger republishes the same TaskID with identical canonical bytes
- **THEN** the claim is found and no second execution or provider call results

#### Scenario: Operator purge of the AGENT stream

- **WHEN** an operator purges the AGENT stream and a claimed TaskID is republished
- **THEN** the claim, stored outside that stream, still resolves the TaskID to its original
  LoopID

### Requirement: Identical replay MUST be idempotent and divergent bytes MUST be rejected

A claimed TaskID republished with identical canonical task content MUST resolve
idempotently to the existing claim; the same TaskID with different canonical content MUST be
refused with a stable classified error. The canonical content basis MUST exclude volatile
envelope fields (timestamps, trace metadata) and MUST be pinned by a round-trip test.

#### Scenario: Exact replay

- **WHEN** the same TaskID arrives twice with identical canonical content
- **THEN** the second arrival returns the existing LoopID and performs no new work

#### Scenario: Divergent bytes under a claimed TaskID

- **WHEN** a claimed TaskID arrives carrying different canonical content
- **THEN** the task is rejected with a stable classified error naming the TaskID and the
  conflict, and no work is executed under either identity

### Requirement: A redelivered task MUST resume an interrupted acceptance from its claim

When a claim exists but no loop state exists (the claimant crashed between claim and
persist), a redelivered task MUST resume using the claim's LoopID and initial RequestID
rather than minting new identity; the resumed initial request publication MUST carry the same
message-deduplication identity as the original would have.

#### Scenario: Crash between claim and loop persist

- **WHEN** a process crashes after committing a claim and before persisting the loop, and the
  task is redelivered
- **THEN** the redelivery creates the loop under the claimed LoopID and publishes the initial
  request under the claimed RequestID, and a still-in-flight original publication collapses
  with it under one deduplication identity

#### Scenario: Redelivery after completion

- **WHEN** a claimed TaskID is redelivered after its loop reached a terminal state
- **THEN** the task is acknowledged without re-execution and the response names the existing
  LoopID

### Requirement: Task and initial-request publications MUST carry deduplication identity

Every `agent.task` publication MUST stamp its TaskID as the JetStream message-deduplication
ID, every initial `agent.request` publication MUST stamp its claimed initial RequestID, and
the AGENT stream declaration MUST set an explicit duplicates window rather than inheriting
the server default.

#### Scenario: Publisher stamps identity

- **WHEN** any in-repo producer publishes an `agent.task` message
- **THEN** the message carries its TaskID as the deduplication ID and the AGENT stream's
  declared duplicates window governs collapse

#### Scenario: Duplicate initial request inside the window

- **WHEN** an initial request is republished by recovery while the original is retained
  within the duplicates window
- **THEN** the stream stores exactly one copy
