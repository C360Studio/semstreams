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
`AGENT_LOOPS` facts after replacement and SHALL perform exact read-through for explicit LoopID operations.
AutoContinue SHALL route only after an initial snapshot completes and is installed atomically. An interrupted or
partial projection SHALL NOT be treated as empty or authoritative.

#### Scenario: Approval HTTP request follows replacement

- **WHEN** dispatch has an empty process cache
- **AND** `AGENT_LOOPS` contains the matching pending approval
- **THEN** the approval endpoint resolves the exact call from durable state
- **AND** does not return 404 or 409 solely because the cache is empty

#### Scenario: Complete snapshot is empty

- **WHEN** the initial snapshot completes with no nonterminal matching loops
- **THEN** dispatch treats the projection as authoritatively empty
- **AND** may create a new loop

#### Scenario: Complete snapshot has one candidate

- **WHEN** the initial snapshot completes
- **AND** exactly one current nonterminal loop matches user and channel
- **THEN** dispatch routes to that loop
- **AND** uses stable task and output identities

#### Scenario: Complete snapshot is ambiguous

- **WHEN** more than one current loop matches the user and channel
- **THEN** dispatch returns a deterministic clarification or error
- **AND** does not guess a loop

#### Scenario: Snapshot is interrupted

- **WHEN** enumeration or watch hydration fails before the completion boundary
- **THEN** AutoContinue is unavailable
- **AND** a bus delivery retries or an HTTP request reports service unavailable
- **AND** dispatch does not treat the partial cache as empty or authoritative

#### Scenario: Explicit LoopID is supplied during incomplete hydration

- **WHEN** an operation supplies an explicit LoopID
- **THEN** dispatch performs an exact `AGENT_LOOPS` read
- **AND** may continue without relying on AutoContinue projection completeness

#### Scenario: Matching loop becomes terminal during hydration

- **WHEN** the ordered snapshot and update sequence records that a candidate is terminal
- **THEN** the installed projection excludes it from AutoContinue
- **AND** stale process-cache state cannot route work to it
