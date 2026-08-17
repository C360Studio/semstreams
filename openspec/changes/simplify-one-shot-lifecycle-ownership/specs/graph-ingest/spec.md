## MODIFIED Requirements

### Requirement: Concurrent ingest MUST bound in-flight work and preserve at-least-once ack

Graph-ingest MUST bound dispatched-but-unacknowledged work by bounded per-lane queues and effective
`max_ack_pending`. Missing JetStream metadata and decode/extract poison occur before keyed-pool admission and MUST be
counted and ACK-dropped under the existing poison policy; they MUST NOT claim keyed durable-guard protection. Pool
admission failure and lane panic MUST NAK for redelivery.

After admission, validation, durable-guard read, effect, durable-guard stamp, in-memory guard update, and ACK MAY run
out of order across lanes but MUST remain ordered within one entity lane. Transient guard/effect failure MUST NAK;
structurally permanent candidate failure MAY Term.

`Lag==0` MUST continue to mean no currently pending or unacknowledged deliverable work, never semantic completeness.
Pre-pool poison and MaxDeliver-parked work MUST NOT license authoritative absence.

#### Scenario: decode poison bypasses keyed guard
- **GIVEN** input cannot become a keyed candidate
- **WHEN** pre-pool poison policy ACK-drops it
- **THEN** no keyed durable guard is written
- **AND** later zero backlog is not semantic-completion evidence

#### Scenario: backpressure caps in-flight work
- **GIVEN** a producer offers entities faster than ingest can process them
- **WHEN** lane queues and `max_ack_pending` are reached
- **THEN** the consumer stops fetching until capacity frees
- **AND** in-memory queued work remains bounded

### Requirement: A redelivered stale message MUST NOT overwrite a newer write

The stale guard MUST remain keyed by `(entity,input stream)`, serialize every stream's updates for one entity on one
lane, and survive restart/eviction in graph-ingest-owned durable state. It MUST be written only after all required graph
effects and before source ACK. A crash after effect and before guard MUST converge through graph merge/CAS identity; a
crash after durable guard and before ACK MUST converge through the stale-redelivery branch. Guard read/write failure
MUST NAK and MUST NOT acknowledge past unpersisted progress.

#### Scenario: crash follows durable guard but precedes ACK
- **GIVEN** effects and durable guard committed
- **WHEN** the process dies before source ACK
- **THEN** redelivery observes stale progress
- **AND** produces no second semantic graph effect

#### Scenario: a late redelivery of an older update is ignored
- **GIVEN** entity state reflects stream S sequence N
- **WHEN** an older sequence from S is redelivered
- **THEN** the durable guard prevents it from overwriting the newer write

#### Scenario: a valid message from another stream is not dropped
- **GIVEN** one entity was updated from stream A
- **WHEN** a later-arriving update comes from stream B's independent sequence space
- **THEN** it remains eligible and serializes on the same entity lane

#### Scenario: a redelivery after restart is still ignored
- **GIVEN** the durable guard records stream S sequence N before restart
- **WHEN** an older S sequence redelivers after restart
- **THEN** durable state still prevents the stale overwrite
