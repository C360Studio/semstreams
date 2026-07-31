# agentic-loop delta — task in-flight visibility

## ADDED Requirements

### Requirement: Whether a loop task is in flight MUST be readable without reconstructing the consumer name
The framework SHALL expose the in-flight question directly — "does this deployment currently have
outstanding agentic-loop work for this task subject" — and SHALL NOT require the caller to know, derive,
or supply the loop's JetStream consumer name to ask it.

The consumer name and its subject-sanitizing derivation remain **private**. A caller that must
reconstruct a name has taken on a contract the framework never promised: when the derivation changes,
the copy does not fail to compile, it fails to find a consumer, and a not-found consumer is
indistinguishable from an idle one. Exposing the name would make a naming detail a public contract and
freeze it; exposing the answer leaves the derivation free to change.

#### Scenario: A caller asks about in-flight work by subject

- **GIVEN** a deployment running an agentic-loop bound to a task subject
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** it receives the answer without supplying a consumer name
- **AND** no exported symbol reveals the consumer-name derivation

#### Scenario: Outstanding work is visible while a task is being worked

- **GIVEN** a task has been delivered to the loop and is not yet acked
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer reports work in flight
- **AND** it continues to do so across the task's heartbeat renewals until the task is acked

### Requirement: An unknown in-flight state MUST be an error, never a report of no work
The query SHALL return an error, and SHALL NOT report zero outstanding work, whenever the in-flight
state cannot be determined — most importantly when no agentic-loop consumer exists in this deployment
at all.

`jetstream.ErrConsumerNotFound` means "this deployment has no agentic-loop", which is a different fact
from "this agentic-loop has nothing in flight". Collapsing them hands the caller the exact inverse of
the truth it asked for, as a confident answer rather than a failure it can handle. **Unknown work MUST
NOT be representable as absent work.** Mapping the error onto caller policy — defer, retry, treat as
busy — belongs to the caller, which is the only party that knows the cost of each direction.

#### Scenario: A deployment with no agentic-loop errors rather than reporting idle

- **GIVEN** a deployment that runs no agentic-loop component
- **WHEN** a caller asks whether work is outstanding for a task subject
- **THEN** an error is returned distinguishing "no such consumer" from "no outstanding work"
- **AND** no zero-valued count is returned alongside it

#### Scenario: A transient lookup failure does not read as idle

- **GIVEN** the consumer exists but its state cannot be read on this attempt
- **WHEN** a caller asks whether work is outstanding
- **THEN** an error is returned rather than a zero count

### Requirement: In-flight state MUST NOT be derived from the acknowledgement floor
The in-flight answer SHALL be sourced from the consumer's outstanding-work bookkeeping
(`NumPending + NumAckPending`) and SHALL NOT be computed from `AckFloor`.

`AckFloor` was measured against both deployed NATS versions and found to misreport in **both**
directions: it does not advance past a `MaxDeliver`-exhausted message, so it sits behind that message
while the consumer is idle; and on the next unrelated ack it leaps *past* the never-applied message.
It therefore never means "everything at or below this is durably handled". The rejection and its
measurement are recorded in ADR-088. This requirement exists so the disproven approach cannot be
reintroduced as an optimization.

A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler
transitions a loop out of `state=running`, so a crashed process leaves a stale `running` entry
indistinguishable from live work.

#### Scenario: A poison-exhausted message does not freeze the in-flight answer

- **GIVEN** a task message that has exhausted `MaxDeliver` and was never applied
- **WHEN** a caller asks whether work is outstanding
- **THEN** the answer reflects genuine outstanding work, not a floor stalled behind that message

#### Scenario: A crashed process does not read as work in flight

- **GIVEN** a loop record left at `state=running` by a process that crashed mid-task
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, not from the stale record
