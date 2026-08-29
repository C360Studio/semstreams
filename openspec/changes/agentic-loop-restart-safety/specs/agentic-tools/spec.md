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

### Requirement: Tool replay remains the sole tool-effect recovery authority

Agentic-tools SHALL NOT add a claimed, started, checkpoint, or second outcome ledger for #1146. Post-effect and
pre-completion ambiguity remains governed by the executor's operation-specific idempotency contract.

#### Scenario: Delivery repeats after a completed tool outcome

- **WHEN** a tool delivery repeats and its exact completed outcome exists
- **THEN** agentic-tools replays that outcome
- **AND** does not consult or create another tool-effect ledger
