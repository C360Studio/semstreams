## MODIFIED Requirements

### Requirement: Every exported port-backed consumption operation requires policy context

`ConsumeStreamWithConfig` and `ConsumeStreamWithConfigContexts` SHALL require nonempty component and port context,
complete every fallible stream, consumer, policy, and observation setup step before delivery, and then return the exact
native `jetstream.ConsumeContext` created at the delivery commit point. No fallible setup step SHALL follow successful
`Consumer.Consume`. Former error-only signatures and a stateful SemStreams managed-consumer wrapper SHALL NOT remain.

`ConsumeDurable` SHALL NOT exist. Retained durable owners use the exact native handle plus existing heartbeat and
settlement primitives.

#### Scenario: setup fails before commit
- **GIVEN** any setup or observation step fails
- **WHEN** the operation returns
- **THEN** delivery has not begun and no lifecycle handle is published

#### Scenario: split-context setup returns ownership
- **WHEN** split-context setup succeeds
- **THEN** setup observation used setup context and handlers use handler context
- **AND** the owner receives the exact native handle for Drain and Closed

#### Scenario: Missing owner context fails before I/O
- **WHEN** a port-backed operation receives empty component or port context
- **THEN** it returns typed invalid configuration before consumer creation

#### Scenario: Split-context consumption remains observed
- **GIVEN** setup and handler lifetimes differ
- **WHEN** `ConsumeStreamWithConfigContexts` creates the consumer
- **THEN** setup observation uses setup context and delivered handlers use handler context
- **AND** the owner receives the exact native handle

### Requirement: Non-port consumption is explicit and bounded

Consumers with no `JetStreamPort` contract MAY use `ConsumeInternalStreamWithConfig`; port-backed consumers SHALL NOT.
The operation SHALL complete every fallible setup/observation step before `Consumer.Consume`, return the exact native
`jetstream.ConsumeContext`, and require the caller to retain it through exact Closed. No internal split-context or
durable convenience operation SHALL exist without a new consumer inventory and owner review.

#### Scenario: internal consumption returns exact ownership
- **GIVEN** a non-port framework consumer
- **WHEN** `ConsumeInternalStreamWithConfig` commits delivery
- **THEN** its caller receives and retains the exact native handle through Closed

#### Scenario: Production call-site census remains separated
- **WHEN** production consumer call sites are enumerated
- **THEN** every `GetConsumerConfig` caller avoids the internal operation
- **AND** internal callers equal the named framework census and retain their exact handles

### Requirement: Consumer policy metrics never retain stale effective truth

The framework SHALL retain the three existing consumer-policy metrics and their labels/source semantics. Refresh
failure SHALL remove stale effective truth and set observation availability to zero while retaining requested truth.
Exact observation MAY retain a concurrency-guarded `Consumer.Info` handle, but SHALL NOT own Drain, Stop, deletion, or
Client child cleanup. Observation records SHALL be removed when the resource owner reports exact native Closed. No
replacement, Stop-by-name, delete-by-name, or Client Close path SHALL masquerade as lifecycle observation cleanup.

#### Scenario: owner closes exact consumer
- **WHEN** the owner observes exact native Closed
- **THEN** all policy series and private observation state for that consumer are removed

#### Scenario: Refresh failure removes stale effective truth
- **GIVEN** a tracked consumer previously reported an effective value
- **WHEN** refresh fails
- **THEN** requested truth remains, effective truth is removed, and observation availability becomes zero

#### Scenario: Lifecycle cleanup removes all policy series
- **WHEN** the exact resource owner reports native Closed
- **THEN** all three series for its private observation record are removed
- **AND** no replacement, name-routed lifecycle, or Client Close path performs that cleanup

## ADDED Requirements

### Requirement: Duplicate local durable identity fails rather than replaces

Within one sealed process composition, two owners SHALL NOT acquire the same `(stream,durable)` identity. Every final
identity derivable from sealed configuration SHALL be validated before parallel Start with one canonical derivation.
An identity not knowable before acquisition SHALL use a minimal active claim containing only identity and opaque owner
token. Duplicate acquisition SHALL fail boot naming both owners and SHALL NOT Stop, Drain, delete, or replace the
incumbent. The fallback claim SHALL NOT become a child-handle catalog.

#### Scenario: duplicate identity is rejected
- **GIVEN** two local owners resolve to one stream and durable identity
- **WHEN** sealed validation or the fallback claim observes the duplicate
- **THEN** boot fails naming both owners without replacing the incumbent
