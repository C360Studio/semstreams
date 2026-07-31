# agentic-loop Specification

## Purpose

`agentic-loop` governs what an agentic loop **owes an outside observer** about its own work: how much
of it a spawn is permitted to do, and whether any of it is currently in flight.

Two question classes live here. **Budget** — a spawn may narrow its iteration allowance within the
operator ceiling, and exhaustion is reported under one uniform reason rather than a per-call-site
spelling. **In-flight visibility** — whether this deployment currently has outstanding loop work for
a task subject is answerable over the component's NATS request/reply surface, without the caller
knowing, deriving, or supplying the loop's JetStream consumer or stream name.

One invariant binds the second class and is the reason it is specified at all: **an absent
measurement must never render as a measurement of absence.** A missing consumer, a component that is
not answering, and a consumer state that could not be read this attempt are three instances of one
rule — each is *unknown*, and none of them is zero. Mapping unknown onto policy belongs to the
caller, which is the only party that knows the cost of guessing in each direction.

**What it does NOT cover.** Whether the loop's answer is trustworthy *yet* is readiness, and belongs
to the ADR-066 envelope — this capability answers *what* the state is, readiness answers *whether the
answer can be believed*, and a consumer that asks the second without the first has made a cold-start
read. Trajectory content, tool dispatch, and model invocation belong to their own components. The
consumer-name derivation is deliberately private and is specified here only as something callers MUST
NOT need.
## Requirements
### Requirement: A spawn may narrow its loop iteration budget

`agentic.TaskMessage` MUST accept an optional per-spawn `max_iterations`; a nil value uses the component
default, a value below 1 fails task validation, and the effective budget MUST be the minimum of the spawn
value and the component `MaxIterations` ceiling. The `publish_agent` rule action MUST expose this as
`loop_max_iterations` with variable substitution, and a substituted value that is not a positive integer MUST
fail the action with a classified, observable error.

#### Scenario: spawn narrows the budget

- **GIVEN** a component configured with MaxIterations 20
- **WHEN** a task is spawned with max_iterations 2
- **THEN** the loop fails with reason "max_iterations" after 2 iterations

#### Scenario: spawn cannot widen past the operator ceiling

- **GIVEN** a component configured with MaxIterations 5
- **WHEN** a task is spawned with max_iterations 50
- **THEN** the effective budget is 5

#### Scenario: substituted budget from an entity triple

- **GIVEN** a publish_agent action with loop_max_iterations "$entity.triple.task.spec.budget"
- **WHEN** the rule fires on an entity carrying that predicate with value "3"
- **THEN** the spawned task carries max_iterations 3

#### Scenario: non-integer substitution fails loudly

- **GIVEN** a publish_agent action whose loop_max_iterations substitutes to "unbounded"
- **WHEN** the rule fires
- **THEN** the action fails with a classified error and a bounded rejection metric, and no task is published

### Requirement: Iteration exhaustion publishes one uniform reason

Every path that detects iteration-budget exhaustion MUST publish the loop-terminal failure reason
`"max_iterations"`. Internal detection MUST use a typed sentinel error mapped via errors.Is; consumers MUST
NOT need to match error text to distinguish budget exhaustion from other handler failures.

#### Scenario: model-response guard at the cap

- **GIVEN** a loop whose iteration count has reached its budget
- **WHEN** the next model response arrives
- **THEN** the published failure reason is "max_iterations"

#### Scenario: tool drain at the cap

- **GIVEN** a loop at its budget with tool calls still in flight
- **WHEN** the pending tools are drained with synthetic failures
- **THEN** the published failure reason is "max_iterations"

### Requirement: Whether a loop task is in flight MUST be readable without reconstructing the consumer name
The agentic-loop component SHALL answer the in-flight question — "does this deployment currently have
outstanding agentic-loop work for this task subject" — over its NATS request/reply surface, and the
caller SHALL NOT need to know, derive, or supply the loop's JetStream consumer name or its stream
name.

The request subject SHALL carry **deployment identity**. Request/reply subscription is plain
subject subscription, so a single shared subject means every agentic-loop in the NATS account
receives the request and replies, and the requester keeps whichever reply arrives first — an
arbitrary deployment's answer delivered with full confidence, which is the precise permissive
failure this capability exists to remove. The deployment token SHALL be the loop's consumer-name
suffix rather than a separately invented identifier, because that suffix already determines which
durable consumer exists: two loops sharing it bind the SAME consumer and therefore necessarily
report the same count, while two loops with different suffixes are different deployments. The
addressing thereby matches the thing being measured. Supplying that token is a SELECTOR — the
caller states which deployment it is asking about, which is inherent to the question — and is not
the consumer-name reconstruction this capability forbids.

The consumer name and its subject-sanitizing derivation remain **private to the component**. A caller
that must reconstruct a name has taken on a contract the framework never promised: when the derivation
changes, the copy does not fail to compile, it fails to find a consumer, and a not-found consumer is
indistinguishable from an idle one.

The component SHALL derive the name internally, from the same helper its own consumer setup uses, so
the query cannot address a different consumer than the component binds. Serving the answer on the wire
rather than through an in-process call is what makes the derivation *deleted* from callers rather than
relocated into their parameter lists: no name, no configuration, and no component handle crosses the
boundary, and a caller in another process is served identically.

#### Scenario: A caller asks about in-flight work by subject

- **GIVEN** a deployment running an agentic-loop bound to a task subject
- **WHEN** a caller issues the in-flight request for that subject
- **THEN** it receives the answer without supplying a consumer name, stream name, or suffix
- **AND** no exported symbol reveals the consumer-name derivation

#### Scenario: An out-of-process caller is served identically

- **GIVEN** a caller in a different process from the agentic-loop component
- **WHEN** it issues the in-flight request over NATS
- **THEN** it receives the same answer an in-process caller would
- **AND** it requires no component handle to do so

#### Scenario: Two deployments in one account are addressed separately

- **GIVEN** two agentic-loop deployments on one NATS account with distinct consumer-name suffixes
- **AND** one holding outstanding work while the other is idle
- **WHEN** a caller addresses each deployment's subject in turn
- **THEN** each answer reflects that deployment's own consumer, deterministically and repeatably
- **AND** asking one deployment about a task subject it does not bind is unknown, never the other
  deployment's count

#### Scenario: A request subscription installed before a failing one is not leaked

- **GIVEN** component start installs more than one request subscription in sequence
- **WHEN** a later one fails and start is abandoned
- **THEN** every already-installed request subscription is unsubscribed during start-failure cleanup
- **AND** a subsequent start attempt leaves exactly one responder per subject

#### Scenario: Outstanding work is visible while a task is being worked

- **GIVEN** a task has been delivered to the loop and is not yet acked
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer reports work in flight
- **AND** it continues to do so across the task's heartbeat renewals until the task is acked

### Requirement: An unknown in-flight state MUST be an error, never a report of no work
An unobserved in-flight state MUST be reported as **unknown** and MUST NOT be reported as zero
outstanding work, on every path that can fail to observe it.

**An absent measurement must never render as a measurement of absence.** This capability has three
instances of that one invariant, and they SHALL be implemented as one rule rather than three
coincidences:

| Condition | Means | Must NOT mean |
|---|---|---|
| `jetstream.ErrConsumerNotFound` | this deployment has no agentic-loop | nothing in flight |
| No responders on the request subject | the loop component is not answering | nothing in flight |
| Consumer state unreadable this attempt | not observed | nothing in flight |

The no-responders case is the most dangerous of the three and is the one introduced by serving the
answer on the wire: a down loop component does not mean the work is gone. Messages may be sitting in
the stream with nobody to answer for them — which is exactly the situation in which a recovery pass is
most likely to be running, and most likely to do harm by concluding a turn is stranded.

Mapping unknown onto policy — defer, retry, treat as busy — belongs to the caller, the only party that
knows the cost of each direction. The requirement is that the caller can tell the cases apart without
string-matching an error message.

**Composition note (normative for consumers, not for this component):** a consumer SHALL gate on the
loop's ADR-066 readiness envelope before treating an in-flight answer as authoritative. Readiness
answers "is this component's answer trustworthy yet"; the in-flight query answers "what is it". Asking
the second without the first is a cold-start read, and it fails closed.

#### Scenario: A deployment with no agentic-loop reports unknown rather than idle

- **GIVEN** a deployment that runs no agentic-loop component
- **WHEN** a caller issues the in-flight request for a task subject
- **THEN** the result is unknown, distinguishable from "consumer exists, nothing outstanding"
- **AND** no zero-valued count is returned alongside it

#### Scenario: A down loop component reports unknown, not idle

- **GIVEN** the agentic-loop component is not running, while task messages remain on the stream
- **WHEN** a caller issues the in-flight request
- **THEN** the no-responders condition surfaces as unknown
- **AND** the caller can distinguish it from an answered "nothing in flight"

#### Scenario: A transient lookup failure does not read as idle

- **GIVEN** the consumer exists but its state cannot be read on this attempt
- **WHEN** a caller issues the in-flight request
- **THEN** the result is unknown rather than a zero count

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

