## ADDED Requirements

### Requirement: User intake settles after durable routing

Dispatch SHALL not positively acknowledge a `UserMessage` until every required task, signal, approval, and
user-response publication has received synchronous PubAck.

#### Scenario: Task publication succeeds but user acknowledgement fails

- **WHEN** the deterministic `TaskMessage` receives PubAck
- **AND** the user acknowledgement publication fails
- **THEN** dispatch retries the `UserMessage`
- **AND** republishes the same task and user response identities

### Requirement: Dispatch process state is a reconstructable projection

`LoopTracker` and pending-approval caches SHALL NOT be authority. Dispatch SHALL reconstruct them from current
`AGENT_LOOPS` facts after replacement and SHALL perform exact read-through on cache miss.

#### Scenario: Approval HTTP request follows replacement

- **WHEN** dispatch has an empty process cache
- **AND** `AGENT_LOOPS` contains the matching pending approval
- **THEN** the approval endpoint resolves the exact call from durable state
- **AND** does not return 404 or 409 solely because the cache is empty

#### Scenario: AutoContinue follows replacement

- **WHEN** a user submits a message without an explicit LoopID after replacement
- **AND** current `AGENT_LOOPS` facts identify one unambiguous active loop for that user and channel
- **THEN** dispatch routes to that loop
- **AND** uses stable task and output identities

#### Scenario: AutoContinue is ambiguous

- **WHEN** more than one current loop matches the user and channel
- **THEN** dispatch returns a deterministic clarification or error
- **AND** does not guess a loop
