# agentic-dispatch Delta

## ADDED Requirements

### Requirement: Dispatch is exclusively an edge gateway

Dispatch SHALL only admit external requests and publish task, cancel, and approval work; expose exact LoopID reads
and one caught-up current-state view; and bridge terminal complete/failed events to user responses when validated
authority carries a user route. Agentic-loop SHALL exclusively own loop birth, pending approval, every intermediate
transition, and terminal state.

Dispatch SHALL NOT create, advance, repair, infer, persist, or cache intermediate loop state and SHALL NOT consume
`agent.created` or `agent.approval_pending` as correctness inputs. Those publications remain available to external
subscribers.

A validated terminal for a system-lane loop with no user route SHALL settle without `user.response`. Conflicting or
temporarily unreadable route evidence SHALL not be treated as routeless.

#### Scenario: System-lane terminal has no user route

- **GIVEN** validated loop authority identifies a terminal system-lane loop with no user route
- **WHEN** dispatch receives its complete or failed event
- **THEN** dispatch publishes no `user.response`
- **AND** settles the terminal source after required validation

#### Scenario: AutoContinue observes the loop-birth gap

- **GIVEN** a new task has received PubAck but its first `LoopEntity` is not yet visible
- **WHEN** another route-only message uses the same `(UserID, ChannelType, ChannelID)`
- **THEN** dispatch observes zero current matches and may mint another task and random LoopID
- **AND** it does not invent a route claim from process memory
- **AND** a caller requiring continuity must supply the first minted LoopID

### Requirement: Dispatch uses one authority-backed current-state projection

Dispatch SHALL use one caught-up graph view over `AGENT_LOOPS` for `/activity`, `/loops`, `/debug/state`, and
AutoContinue. `LoopTracker` and pending-approval process caches SHALL NOT exist. `/loops` and `/debug/state` SHALL
preserve the existing immutable `LoopInfo` JSON schema, including `execution_id` on the existing nested
`PendingApprovalInfo`, which SHALL come from observed pending authority. The existing optional
`context_request_id` field SHALL remain empty in the authority-backed projection, as accepted by the edge-gateway
design; no historical notification lookup SHALL reconstruct it. All other unrelated DTO fields and projection
contracts SHALL remain unchanged.
`/debug/state` SHALL expose the view's caught-up readiness
and current poison diagnostics rather than reporting a false empty state.

Explicit LoopID approval, read, continuation, cancellation, terminal-route, and command-owner operations SHALL
exact-read and validate `AGENT_LOOPS/<LoopID>`. A partial, stale, watcher-lost, or relevant-poisoned projection SHALL
never be treated as empty.

An approval SHALL read the current durable record on every decision rather than a process-local pending cache, and
SHALL obtain its CallID from validated `PendingApproval` state. An unreadable, absent, or incoherent record — a
pending approval outside `awaiting_approval`, or an `awaiting_approval` record whose pending CallID or ExecutionID is
empty — SHALL refuse as unavailable. A record that is readable but no longer awaiting approval SHALL refuse as
conflict. Admission and publication SHALL NOT mutate loop authority, so a failed publish leaves the decision
retryable.

#### Scenario: Approval follows replacement

- **GIVEN** exact current state is awaiting approval
- **WHEN** an authorized approval names its canonical LoopID
- **THEN** dispatch obtains CallID from validated `PendingApproval` state read at decision time
- **AND** requires no earlier approval-pending event

#### Scenario: Pending output carries observed identity after replacement

- **GIVEN** exact validated current authority contains a pending approval
- **WHEN** the existing pending HTTP projection is read after dispatch replacement
- **THEN** it carries the pending record's ExecutionID as `execution_id`
- **AND** the caller need not compute identity or have received the original event

#### Scenario: Current authority refuses a decision the memory cache would have accepted

- **GIVEN** the durable record is executing, terminal, or incoherent
- **WHEN** an approval decision arrives
- **THEN** dispatch refuses from the record alone, as conflict or unavailable
- **AND** no process-local cache can override or stand in for that record

#### Scenario: A failed publish leaves the decision retryable

- **GIVEN** a validated pending approval
- **WHEN** publishing the decision fails
- **THEN** the durable record is byte-identical to what it was before the request
- **AND** the caller may submit the same decision again

#### Scenario: Projection endpoint is unavailable

- **WHEN** the shared view is not caught up or has current-loop poison
- **THEN** listing and debug return service unavailable
- **AND** debug diagnostics identify not-caught-up readiness or the current poison condition
- **AND** AutoContinue remains retryable
- **AND** no path assumes zero loops

#### Scenario: Loop DTO shape is preserved

- **WHEN** `/loops` or `/debug/state` reports a valid view-derived loop
- **THEN** it uses the existing immutable `LoopInfo` JSON schema with only the declared nested pending
  `execution_id` addition
- **AND** `context_request_id` retains its optional schema but is empty, as accepted for the authority-backed view
- **AND** JSON/OpenAPI verification preserves every other unrelated field and mapping
- **AND** no mutable loop entity, tracker state, or projection internals enter the response

#### Scenario: Exact AutoContinue tuple has one match

- **GIVEN** exactly one nonterminal record matches `(UserID, ChannelType, ChannelID)`
- **WHEN** AutoContinue resolves the message
- **THEN** dispatch continues that LoopID

#### Scenario: Partial route does not match

- **WHEN** only UserID, ChannelType, or ChannelID agrees
- **THEN** the record is not an AutoContinue candidate

#### Scenario: AutoContinue is ambiguous

- **GIVEN** more than one exact nonterminal match
- **WHEN** AutoContinue resolves the message
- **THEN** dispatch refuses with typed ambiguity
- **AND** does not guess

### Requirement: The shared view separates current authority from activity

Bare canonical LoopID keys SHALL validate as `LoopEntity` with key/ID equality. Invalid values under those keys
SHALL poison authoritative listing and AutoContinue until a greater-revision valid write or tombstone heals them.
Other non-completion keys SHALL be excluded from current-loop authority without optional-producer classification.

Existing ordinary `COMPLETE_` activity SHALL retain its field mappings and canonical suffix/payload identity checks.
Completion records SHALL remain activity-only. Unsupported or malformed completion records SHALL produce observable
activity errors and SHALL NOT fabricate current state or block current-loop authority.

This change SHALL NOT introduce research completion rendering, a payload behavior, or a shared namespace package.
Registered stream terminal decoding and validation SHALL remain unchanged by this activity-scope reduction.
Withdrawal of research rendering SHALL NOT withdraw registered ordinary-terminal support. Both private raw
terminal records and registered ordinary-terminal envelopes SHALL retain their current validation and field mappings.

#### Scenario: Canonical current-loop corruption heals

- **GIVEN** an invalid value under a canonical LoopID has poisoned current-loop authority
- **WHEN** a greater-revision valid value with matching ID or a tombstone lands
- **THEN** the poison clears
- **AND** authoritative listing and AutoContinue may resume after that revision is applied

#### Scenario: Non-authority record is present

- **WHEN** a key is neither a canonical LoopID nor a completion key
- **THEN** it is excluded without interpreting its value or optional producer's namespace
- **AND** it does not become a loop or poison current-loop authority

#### Scenario: Ordinary completion remains activity-only

- **GIVEN** an ordinary raw or registered completion with a canonical key and matching payload LoopID
- **WHEN** the shared view decodes it
- **THEN** it preserves the existing activity fields
- **AND** it does not supply a current-loop record or an AutoContinue candidate

#### Scenario: Unsupported completion does not block current authority

- **GIVEN** an unsupported or malformed completion value
- **WHEN** the shared view observes it
- **THEN** activity reports the existing observable error
- **AND** no result or current-loop state is fabricated
- **AND** current-loop authority remains available if its own records and watcher are healthy

### Requirement: The shared loop view classifies the mixed bucket

Bare canonical LoopID keys SHALL validate as `LoopEntity` with key/ID equality. `COMPLETE_<canonical LoopID>` SHALL
validate by completion family and remain activity-only. Known research namespaces SHALL be ignored as non-loop
records. Every other key SHALL poison as malformed would-be loop state.

A typed terminal payload's LoopID SHALL equal the suffix. A registered `SearchResult` has no payload LoopID; the
suffix supplies its activity identity. Its aggregate `TokensUsed` SHALL NOT populate directional Loop token fields.

Current-loop and unknown-key poison SHALL disable AutoContinue and authoritative listing until a greater-revision
clean write or tombstone heals it.

#### Scenario: SearchResult completion is projected

- **GIVEN** a valid registered `SearchResult` at `COMPLETE_<canonical LoopID>`
- **WHEN** the view decodes it
- **THEN** the suffix supplies LoopID
- **AND** synthesis, success, complete state, and iterations project through the existing Loop activity shape
- **AND** TokensIn and TokensOut remain zero

#### Scenario: Research intermediate record is present

- **WHEN** a known research namespace is observed
- **THEN** it is excluded without becoming loop poison

#### Scenario: Malformed would-be loop heals

- **GIVEN** an unknown or malformed current-loop key has poisoned the view
- **WHEN** a greater-revision clean value or tombstone lands
- **THEN** the poison clears
- **AND** readiness may return after that revision is applied
