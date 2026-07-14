## ADDED Requirements

### Requirement: ENTITY_STATES has one canonical codec and typed poison contract

Every in-process ENTITY_STATES writer MUST use the canonical complete-candidate encoder and every component that
interprets ENTITY_STATES MUST use the canonical decoder. The decoder MUST distinguish unreadable entity JSON from
a noncanonical predicate with bounded reasons and the shared `graph_state_reset_required` code. Ordinary JSON in
other KV buckets is outside this contract.

#### Scenario: unreadable authoritative state is typed poison

- **GIVEN** an ENTITY_STATES value cannot decode as EntityState
- **WHEN** any authoritative reader interprets it
- **THEN** the reader returns `graph_state_reset_required`
- **AND** the bounded reason is `unreadable_entity_state`

#### Scenario: noncanonical authoritative state is typed poison

- **GIVEN** an ENTITY_STATES value contains a predicate outside the canonical grammar
- **WHEN** any authoritative reader interprets it
- **THEN** the reader returns `graph_state_reset_required`
- **AND** the bounded reason is `noncanonical_predicate`

### Requirement: Watch transport, tombstones, and state poison are distinct

Every ENTITY_STATES watch consumer MUST classify CREATE/PUT values, DEL/PURGE tombstones, and watch transport
failures before decoding. CREATE and PUT values MUST use the canonical decoder. DEL and PURGE MUST be treated as
equivalent valid entity-removal tombstones, MUST drive the consumer's cleanup path, and MUST count as terminal
replay work without attempting to decode their empty payload. A watch transport failure MUST follow the ordinary
retry, degraded-health, or not-ready path and MUST NOT latch `graph_state_reset_required`.

#### Scenario: delete and purge are valid tombstones

- **GIVEN** an ENTITY_STATES watch delivers DEL or PURGE for an entity key
- **WHEN** an authoritative consumer handles the entry
- **THEN** it performs the same entity-removal cleanup for either operation
- **AND** it does not classify the empty payload as unreadable entity state
- **AND** replay progress may advance after that tombstone's cleanup completes

#### Scenario: transport loss is not stored-state poison

- **GIVEN** an ENTITY_STATES watch closes or returns a transport error
- **WHEN** a consumer has not observed malformed authoritative state
- **THEN** the consumer remains degraded or not-ready according to its ordinary recovery contract
- **AND** it does not latch `graph_state_reset_required`
- **AND** transport recovery does not require an operator graph reset

### Requirement: Projection poison is sticky until operator reset and restart

A projection owner that observes typed graph-state poison MUST enter sticky reset-required state, MUST NOT treat the
poisoned revision as successfully terminal, and MUST NOT serve its derived view. Repair or a later valid update MUST
NOT clear the process-lifetime poison. Action/evaluation consumers MUST emit no derived output from poisoned state.

#### Scenario: a later valid event cannot hide poison

- **GIVEN** a projection owner has observed incompatible ENTITY_STATES
- **WHEN** a later valid entity event arrives
- **THEN** readiness remains reset-required
- **AND** no query returns a partial derived view

#### Scenario: clean reset starts a new readiness lifetime

- **GIVEN** the operator exported if needed and deleted incompatible graph/index buckets
- **WHEN** the process restarts and canonical sources are reingested
- **THEN** the new process has no inherited poison flag
- **AND** ordinary replay watermarks decide readiness

### Requirement: Generic KV actions cannot bypass graph ownership

The graph package MUST publish the complete set of framework-owned authoritative and derived graph buckets.
Generic rule `update_kv` actions MUST reject those buckets both during literal configuration validation and after
runtime variable substitution. Domain application KV buckets remain valid `update_kv` targets.

#### Scenario: substituted graph bucket is rejected

- **GIVEN** an update_kv action resolves its bucket from message data
- **WHEN** the result is a framework-owned graph bucket
- **THEN** the action fails before any KV write
- **AND** the caller is directed to the graph mutation API
