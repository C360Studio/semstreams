## ADDED Requirements

### Requirement: The framework catalog SHALL own durable tool-call outcomes

The catalog SHALL declare `TOOL_CALL_OUTCOMES` as operational, owner-only state owned and created by `agentic-tools`.
It SHALL have History 1, Replicas 1, MaxAge zero, MaxBytes unlimited, and no lifecycle reclamation. `agentic-tools`
SHALL acquire it through `EnsureFrameworkBucket` before subscriptions or consumers. No adopter-facing bucket or
retention configuration SHALL be added.

#### Scenario: generic KV mutation targets the outcome ledger

- **WHEN** a generic framework KV writer targets `TOOL_CALL_OUTCOMES`
- **THEN** the catalog-derived owner-only guard rejects the write
- **AND** identifies `agentic-tools` as owner
