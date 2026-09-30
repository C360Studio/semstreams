## ADDED Requirements

### Requirement: The Graphable lane MUST validate a decoded payload before extraction and MUST fence identity panics

The Graphable lane MUST call the decoded `BaseMessage`'s `Validate()` after decode and before any method of the
payload runs, and MUST treat a validation failure as a poison message: counted in the lane's error count, logged at
WARN with the subject and the reason, acknowledged and dropped, never persisted. A panic raised by the decoded
payload's `EntityID()`, `Triples()`, `StorageRef()` or `IndexingProfile()` MUST be recovered inside the lane and
converted into a classified rejection that names the message type and the recovered value, on the same poison path;
the consumer goroutine MUST survive. This is the consumer half of the `BaseMessage.MarshalJSON` contract: what the
producer refused to emit, the lane refuses to ingest.

#### Scenario: A decoded payload fails its own Validate()
- **WHEN** a Graphable arrival decodes to a registered payload whose `Validate()` returns an error
- **THEN** the lane rejects the message before calling `EntityID()` or `Triples()`
- **AND** nothing is written to `ENTITY_STATES`, the error count increments, a WARN log names the subject and reason, and the message is acknowledged

#### Scenario: A decoded payload's identity method panics
- **WHEN** a Graphable arrival decodes to a registered payload whose `EntityID()` or `Triples()` panics on its content
- **THEN** the panic is recovered inside the lane and the message is rejected with a classified error naming the message type and the panic value
- **AND** the consumer goroutine and the process survive, and the message is acknowledged and dropped

#### Scenario: A conforming payload ingests unchanged
- **WHEN** a Graphable arrival decodes to a payload whose `Validate()` returns nil and whose identity methods return normally
- **THEN** the lane extracts and merges it with the existing predicate-level replacement semantics, unchanged by the validation and the fence
