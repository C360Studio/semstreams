## MODIFIED Requirements

### Requirement: Every exported port-backed consumption operation requires policy context

`ConsumeStreamWithConfig` and `ConsumeStreamWithConfigContexts` SHALL require nonempty component and port context,
SHALL observe acknowledgement-admission policy before delivery, and SHALL return the exact `*ManagedConsumer` handle
created for the caller. Their former error-only signatures SHALL NOT remain as compatibility paths.

`ConsumeDurable` SHALL NOT exist. Its zero-production-consumer convenience surface SHALL be retired instead of
propagating a second acknowledgement/heartbeat lifecycle wrapper. ADR-070 remains historical decision context for
durable gated-DAG dispatch and ack-after-terminal-marker semantics; retirement of the unused helper does not rewrite
that accepted history.

#### Scenario: Missing owner context fails before I/O

- **WHEN** a port-backed operation receives empty component or port context
- **THEN** it returns typed invalid configuration before consumer creation
- **AND** no managed-consumer handle is published

#### Scenario: Split-context consumption returns exact ownership

- **GIVEN** setup and handler lifetimes differ
- **WHEN** `ConsumeStreamWithConfigContexts` creates the consumer
- **THEN** setup observation uses the setup context
- **AND** delivered handlers use the declared handler context
- **AND** the caller receives and retains the exact handle for graceful Stop

#### Scenario: Durable convenience surface is absent

- **WHEN** exported natsclient consumption methods are enumerated
- **THEN** `ConsumeDurable` and equivalent convenience aliases are absent
- **AND** durable owners use a retained exact handle plus the existing heartbeat and settlement primitives

### Requirement: Non-port consumption is explicit and bounded

Consumers with no `JetStreamPort` contract MAY use `ConsumeInternalStreamWithConfig`. Port-backed consumers SHALL NOT
use it. The operation SHALL return the exact `*ManagedConsumer` handle, and its caller SHALL retain that handle and
Drain it during Stop. No internal split-context or durable convenience operation SHALL exist without a new consumer
inventory and owner review.

#### Scenario: Production call-site census remains separated

- **WHEN** production consumer call sites are enumerated
- **THEN** every `GetConsumerConfig` caller avoids the internal operation
- **AND** internal callers equal the named framework census
- **AND** every retained caller owns the returned handle through authoritative drain completion
