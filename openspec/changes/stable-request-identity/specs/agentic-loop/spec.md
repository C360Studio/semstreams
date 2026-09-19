# agentic-loop Delta

## ADDED Requirements

### Requirement: A logical model request has one deterministic identity

Agentic-loop SHALL mint every `agent.request` RequestID as `<loopID>:req:<iteration>:<retry>`, where `iteration` is
the 1-based ordinal of the request within its loop and `retry` is the within-iteration truncation-retry ordinal.
Minting the same logical request twice SHALL yield the same RequestID. The `<loopID>:req:` prefix SHALL be
preserved so loop recovery from a RequestID and the `agent.response.<requestID>` subject are unchanged.

Every `agent.request` publication SHALL stamp its RequestID as `Nats-Msg-Id`. Server-side duplicate suppression
inside the stream's configured window is a bounded convenience and SHALL NOT be treated as the mechanism that
prevents repeated provider work; that mechanism is the retained-response rule in `agentic-model`.

#### Scenario: The same logical request is minted twice

- **WHEN** a task redelivers and agentic-loop mints the request for a loop whose iteration and retry ordinals have
  not moved
- **THEN** the RequestID is byte-identical to the one minted the first time
- **AND** a request minted at a different iteration is a different RequestID

#### Scenario: A truncation retry names its own attempt

- **WHEN** a length-truncated response at iteration N triggers the compaction retry
- **THEN** the retry's RequestID is `<loopID>:req:N:1`
- **AND** forward progress clears the retry ordinal back to 0 for the next iteration

#### Scenario: A consumer reads the loop token from a RequestID

- **WHEN** any consumer recovers the loop token from a RequestID, by the `:req:` separator or by the first colon
- **THEN** it reads the same loop token the framework minted
- **AND** the resolved `agent.response.<requestID>` subject is a single valid NATS token

#### Scenario: A duplicate request publish meets a configured window

- **WHEN** the same RequestID is published twice to a stream that declares a `Duplicates` window, inside that window
- **THEN** the server rejects the second publish and one message is stored on the subject
- **AND** a publication carrying no `Nats-Msg-Id` still repeats, because at-least-once is unchanged

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

### Requirement: Loop task, request, and tool work use only required correlation

For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL
validate their mapping and SHALL reject a conflicting mapping. Provider work SHALL carry a stable RequestID. Tool
work SHALL carry the framework execution identity derived from RequestID, provider CallID, and positive call ordinal.

Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.
Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT
be treated as permanent identity or proof of publication. Exact retained reads SHALL exist only at named boundaries
where they prevent repeating non-repeatable work or prove a lane-specific durable transition already applied.

#### Scenario: Task mapping is stable across redelivery

- **WHEN** a task redelivers after its LoopEntity or initial request committed
- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping
- **AND** any required ordinary publication may repeat and receives PubAck before source ACK

#### Scenario: Request or execution correlation conflicts

- **WHEN** one RequestID or framework execution identity names conflicting required correlation
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either mapping

#### Scenario: Ordinary required publication repeats

- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat
- **THEN** the duplicate is an admitted at-least-once outcome
- **AND** consumers use the lane's required correlation and durable transition rules
