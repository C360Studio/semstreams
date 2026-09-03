## ADDED Requirements

### Requirement: Every dispatch durable input settles through its owner

Dispatch SHALL classify `user.message`, `agent.created`, `agent.approval_pending`, `agent.complete`, and
`agent.failed` through their binding owner. Business handlers SHALL receive only an immutable owner-supplied work
view and SHALL return a typed semantic outcome. Native message and settlement methods SHALL NOT escape the owner.

A `UserMessage` SHALL not be positively acknowledged until every required task, cancel signal, approval response,
and user-response publication has synchronous JetStream PubAck. Created and approval-pending events SHALL settle
after projection update or exact proof from `AGENT_LOOPS`. Terminal events SHALL retain their typed ancestry,
read-through, and deterministic response contract. No void, log-only, or core-NATS publication failure SHALL become
ACK.

The `user.message`, `agent.created`, and `agent.approval_pending` subscriptions SHALL invoke their typed business
handlers using the callback installed by each production setup branch. All delivery-derived work SHALL join before
the private callback passes its decision and cause to `natsclient.SettleDelivery`. JetStream consumer configuration
owns AckWait and redelivery; dispatch SHALL NOT derive a universal work deadline from AckWait. An operation MAY use
an ordinary business timeout. A physical subscription SHALL move to the existing heartbeat owner only after measured
legitimate work can exceed its configured acknowledgement interval.

The first owner-fatal result from any dispatch delivery owner SHALL synchronously latch before the exact handle is
drained. Existing Health SHALL report `Healthy=false`, status `delivery ownership lost`, the exact first cause in
`LastError`, and exactly one owner-loss error count. Later owner-fatal results SHALL neither overwrite nor recount
the first cause. This replaces per-lane fatal aggregation and adds no metric family, public state, durable state, or
communication path.

#### Scenario: Task publication succeeds but user response fails

- **WHEN** the deterministic TaskMessage receives PubAck
- **AND** the required user response does not receive PubAck
- **THEN** dispatch retries the UserMessage
- **AND** republishes the same task and response identities

#### Scenario: Invalid user input receives its negative consequence

- **WHEN** a user message is permanently invalid or unauthorized
- **THEN** its deterministic typed user error receives PubAck before termination
- **AND** tracker and gauge state remain unchanged

#### Scenario: Created event arrives after replacement

- **WHEN** dispatch has no process tracker entry
- **AND** the exact `AGENT_LOOPS` record proves the same loop
- **THEN** dispatch updates or reconstructs its projection
- **AND** acknowledges without treating cache absence as not-found

#### Scenario: Terminal publication is uncertain

- **WHEN** a deterministic user response does not receive PubAck
- **THEN** its terminal source is not positively acknowledged
- **AND** replay uses the same source-derived response identity

#### Scenario: Dispatch business work reaches its own deadline

- **WHEN** a delivery-owned dispatch operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

### Requirement: Dispatch process state is a reconstructable projection

`LoopTracker` and pending-approval caches SHALL NOT be authority. Dispatch SHALL reconstruct them from current
`AGENT_LOOPS` facts after replacement and SHALL perform exact read-through for explicit LoopID operations.
AutoContinue SHALL route only after an initial snapshot completes and is installed atomically. An interrupted or
partial projection SHALL NOT be treated as empty or authoritative.

#### Scenario: Approval HTTP request follows replacement

- **WHEN** dispatch has an empty process cache
- **AND** `AGENT_LOOPS` contains the matching pending approval
- **THEN** the endpoint resolves the exact call from durable state
- **AND** does not return 404 or 409 solely because the cache is empty

#### Scenario: Complete snapshot is empty

- **WHEN** the initial snapshot completes with no nonterminal matching loops
- **THEN** dispatch treats the projection as authoritatively empty
- **AND** may create a new loop

#### Scenario: Complete snapshot has one candidate

- **WHEN** the initial snapshot completes with exactly one current nonterminal loop matching user and channel
- **THEN** dispatch routes to that loop with stable task and output identities

#### Scenario: Complete snapshot is ambiguous

- **WHEN** more than one current loop matches user and channel
- **THEN** dispatch returns a deterministic clarification or error
- **AND** does not guess a loop

#### Scenario: Snapshot is interrupted

- **WHEN** enumeration or watch hydration fails before completion
- **THEN** AutoContinue is unavailable
- **AND** bus delivery retries or HTTP reports service unavailable
- **AND** the partial cache is not treated as empty or authoritative

#### Scenario: Explicit LoopID is supplied during incomplete hydration

- **WHEN** an operation supplies an explicit LoopID
- **THEN** dispatch performs an exact `AGENT_LOOPS` read
- **AND** may continue without projection completeness

#### Scenario: Matching loop becomes terminal during hydration

- **WHEN** ordered snapshot/update state records that a candidate is terminal
- **THEN** the installed projection excludes it from AutoContinue

### Requirement: Dispatch publication identity is deterministic and reconcilable

Dispatch SHALL derive TaskID and a new LoopID deterministically from validated `UserMessage` identity. Every task,
cancel signal, approval response, refusal, terminal user response, and other required output SHALL have a stable
source-derived identity and deterministic `Nats-Msg-Id`. Before repeating a required publication after redelivery,
dispatch SHALL perform its operation-specific exact committed-output lookup and validate canonical content. A match
proves the publication; absence remains safe only inside admitted source and destination retention; an identity with
different content SHALL quarantine. No general stream scan or query front door is introduced.

#### Scenario: User delivery repeats after task commit

- **WHEN** a `UserMessage` is redelivered after its exact task output committed
- **THEN** dispatch finds and validates that committed task by deterministic identity
- **AND** any required republish uses the same `Nats-Msg-Id`

#### Scenario: Deterministic identity carries different content

- **WHEN** exact lookup finds the expected output identity with non-matching canonical content
- **THEN** dispatch quarantines the source delivery
- **AND** does not overwrite or select either value

### Requirement: Dispatch shutdown closes every delivery owner

Dispatch shutdown SHALL stop admission, drain every exact durable-input consume handle, await each handle's exact
`Closed` signal, then cancel and join its projection observer and delivery work. Shutdown SHALL NOT return while a
delivery callback or hydration goroutine can still settle or mutate projection state.

#### Scenario: Shutdown races active dispatch work

- **WHEN** dispatch Stop begins while a durable callback and projection observer are active
- **THEN** no new delivery is admitted, every consume handle drains and closes, and both work paths join
- **AND** Stop returns only after no later ACK, publication, or projection mutation is possible

## MODIFIED Requirements

### Requirement: Loop existence and ownership are merged facts, never process memory alone

The gate MUST decide existence, ownership, route, and state from the exact durable `AGENT_LOOPS` record. The
process-local `LoopTracker` is a reconstructable projection of that authority: it MAY accelerate an answer after a
complete hydration, but it MUST NOT admit a loop, establish ownership, or supply a state when the durable record is
absent or unreadable. When cache and authority are both observed, a conflicting non-empty owner, route, or state is
an invariant collision and MUST be refused rather than merged by silent preference.

The durable bucket name MUST be OBSERVED from the component's declared KV read port through the existing port
projection. No reader may carry a bucket-name default of its own.

Degradation is explicit. An exact read failure other than key absence MUST refuse as transient even when the tracker
has an entry. Key absence is the not-found refusal and a tracker-only entry MUST NOT change it. A complete projection
MAY answer AutoContinue candidate selection, but explicit LoopID operations MUST read the exact durable record.

The durable facts MUST carry the loop's recorded STATE. The valid state vocabulary excludes `paused`; a record that
carries `paused`, no state, or an unknown state MUST be refused as invalid/unknown rather than reported as a valid
running state. A settled durable observation remains authoritative over a stale nonterminal cache entry.

#### Scenario: a continuation after a process replacement is admitted from the durable record

- **GIVEN** a loop created before dispatch was replaced, whose `AGENT_LOOPS` record names its owner
- **AND** an empty loop tracker in the replacement process
- **WHEN** that loop's owner continues it by `reply_to`
- **THEN** the request is admitted, and the loop is continued rather than silently forked under the same token
- **AND** the test that verifies this is `TestContinuationAfterReplacementIsAdmittedFromDurableRecord`

#### Scenario: a tracker-only loop is not admitted as durable fact

- **GIVEN** a loop appears only in the process tracker and the exact `AGENT_LOOPS` key is absent
- **WHEN** its apparent owner attempts to continue it
- **THEN** the request is refused as not found rather than admitted from process memory
- **AND** the test that verifies this is `TestTrackerOnlyLoopDoesNotEstablishDurableExistence`

#### Scenario: an unreadable durable record refuses as transient

- **GIVEN** an `AGENT_LOOPS` read that fails with anything other than key absence
- **WHEN** a request names a loop, whether or not the tracker contains an entry
- **THEN** the refusal is classified transient, not not-found, and no loop is created for the token
- **AND** the test that verifies this is `TestUnreadableDurableRecordRefusesTransient`

#### Scenario: a status read after a process replacement reports the recorded state

- **GIVEN** an empty loop tracker and an `AGENT_LOOPS` record whose loop is `awaiting_approval`
- **WHEN** its owner asks for that loop's status
- **THEN** the answer names `awaiting_approval` rather than a fixed "running"
- **AND** a record carrying no state, `paused`, or an unknown state is refused rather than fabricated
- **AND** the tests that verify this are `TestStatusReportsTheRecordedStateNotAFabricatedRunning` and
  `TestMergeLoopStatePrefersSettledThenTheTracker`

#### Scenario: conflicting owners across cache and authority are refused

- **GIVEN** a tracker entry and an `AGENT_LOOPS` record for the same token whose recorded owners differ
- **WHEN** the gate admits a request naming it
- **THEN** the request is refused with the conflict reason rather than one source being silently preferred
- **AND** the test that verifies this is `TestConflictingOwnersAcrossSourcesAreRefused`
