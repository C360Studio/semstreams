# agentic-tools Delta

## ADDED Requirements

### Requirement: Tool outcomes preserve framework execution correlation

Agentic-tools SHALL preserve RequestID and framework execution identity from `ToolCall` onto every `ToolResult`,
including approval-required, compacted, panic, failure, and completed-outcome replay results.

#### Scenario: Executor returns a result without correlation fields

- **WHEN** an executor hosted by agentic-tools returns its domain result
- **THEN** agentic-tools stamps the originating RequestID and execution identity
- **AND** the executor author is not required to manage settlement correlation

### Requirement: Completed tool outcome identity is globally unambiguous

`TOOL_CALL_OUTCOMES` SHALL key and fingerprint completed outcomes using framework execution identity while retaining
provider CallID as conversation data.

#### Scenario: Provider CallID repeats across turns

- **WHEN** two calls share provider CallID but have different RequestIDs
- **THEN** they create distinct completed-outcome identities
- **AND** replay returns only the result matching the exact execution identity

### Requirement: Tool-result publication is durably at-least-once

Every required `ToolResult` publication SHALL carry framework execution identity and receive PubAck before source
ACK. PubAck uncertainty MAY repeat a result. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT be
treated as permanent publication identity.

The exact immutable `TOOL_CALL_OUTCOMES` read exists only at the executor-effect boundary. Before executor invocation,
a matching outcome is replayed and a conflicting fingerprint quarantines. Ordinary ToolResult republication requires
no second exact output lookup, general stream scan, or second tool authority.

#### Scenario: Completed result publication repeats

- **WHEN** a completed tool delivery repeats after an uncertain result PubAck
- **THEN** the stored outcome may be published again with the same framework execution identity
- **AND** the executor is not invoked again

#### Scenario: Completed outcome content conflicts

- **WHEN** the expected execution identity names a different canonical result
- **THEN** agentic-tools quarantines without selecting or overwriting either outcome

