# agentic-loop — delta (durable-task-claims)

## ADDED Requirements

### Requirement: Task acceptance MUST be gated by the durable claim, not process memory

The loop processor MUST consult the durable TaskID claim as the authority for whether a task
has been accepted; process-local state MAY serve as a fast path but MUST NOT be the deciding
authority for acceptance, resumption, or rejection.

#### Scenario: Redelivery to a restarted process

- **WHEN** a process restarts, losing all in-memory loop state, and a previously accepted
  task is redelivered
- **THEN** the claim resolves it to the existing LoopID and no second loop or initial request
  is created

#### Scenario: Redelivery to a different replica

- **WHEN** a task already claimed by one instance is delivered to another instance
- **THEN** the second instance resolves the claim identically to the first, with no second
  execution

#### Scenario: Terminal loop redelivery does not re-execute

- **WHEN** a task whose loop reached a terminal state is redelivered to any instance
- **THEN** acceptance short-circuits to the terminal loop's identity and no new loop, request,
  or provider call results

### Requirement: The initial request publication MUST be idempotent by claimed identity

The loop processor MUST publish the initial agent request under the claim's initial
RequestID with that identity stamped as the JetStream deduplication ID, so recovery
republication and the original publication collapse rather than duplicate.

#### Scenario: Recovery republication collapses

- **WHEN** recovery republishes an initial request whose original was published within the
  duplicates window
- **THEN** downstream consumers observe exactly one initial request for that RequestID
