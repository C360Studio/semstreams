## ADDED Requirements

### Requirement: Model request settlement is bound to a durable response

Agentic-model SHALL not positively acknowledge an `AgentRequest` until a matching `AgentResponse` has received
synchronous JetStream PubAck. It SHALL use RequestID as the stable logical response identity and SHALL reconcile an
already committed matching response before invoking a provider.

Agentic-model SHALL receive immutable delivery-attempt observation from the accepted settlement adapter. It SHALL
NOT receive or retain native message or settlement authority.

#### Scenario: Response publication succeeds

- **WHEN** a provider returns an `AgentResponse` for a valid `AgentRequest`
- **THEN** agentic-model publishes the response with deterministic identity
- **AND** waits for PubAck
- **AND** only then positively acknowledges the source request

#### Scenario: Matching response already exists

- **WHEN** a request is redelivered and an exact committed response with matching RequestID and fingerprint exists
- **THEN** agentic-model does not invoke the provider
- **AND** positively acknowledges the source request

#### Scenario: Response identity collides

- **WHEN** a committed response uses the same RequestID but does not match the request or expected fingerprint
- **THEN** agentic-model quarantines the delivery
- **AND** does not invoke the provider

#### Scenario: First observed delivery

- **WHEN** delivery metadata is available
- **AND** delivery number equals 1
- **THEN** the attempt is classified as first delivery
- **AND** agentic-model may enter provider invocation

#### Scenario: Delivery metadata is unavailable

- **WHEN** the settlement adapter cannot observe native delivery metadata
- **THEN** it does not invoke agentic-model work
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** stops the exact delivery owner

### Requirement: Provider commit-unknown behavior is explicit

Agentic-model SHALL implement an explicit provider ambiguity policy. The default SHALL be
`fail_commit_unknown`. `at_least_once` SHALL require operator opt-in. `provider_reconcile` SHALL be admitted only
for an adapter with demonstrated provider idempotency or result lookup.

#### Scenario: Default policy sees unresolved redelivery

- **WHEN** a request is redelivered
- **AND** no matching durable response exists
- **AND** the framework cannot prove whether the prior process invoked the provider
- **THEN** agentic-model does not invoke the provider again
- **AND** publishes a typed commit-unknown `AgentResponse`
- **AND** acknowledges only after that response receives PubAck

#### Scenario: Replacement occurred before provider invocation

- **WHEN** the first process stops after delivery 1 and before provider invocation
- **AND** delivery 2 has no matching durable response
- **AND** policy is `fail_commit_unknown`
- **THEN** agentic-model publishes `provider_commit_unknown`
- **AND** does not invoke the provider
- **AND** records the conservative false-unknown possibility

#### Scenario: At-least-once is selected

- **WHEN** the operator explicitly selects `at_least_once`
- **AND** an unresolved request is redelivered
- **THEN** agentic-model may invoke the provider again with the same RequestID
- **AND** records the repeated-attempt classification

#### Scenario: Provider reconciliation is supported

- **WHEN** the selected adapter demonstrates reconciliation keyed by RequestID
- **THEN** agentic-model reconciles before invoking
- **AND** publishes the reconciled or newly idempotent result with the same stable identity

### Requirement: Provider commit-unknown is machine-readable

An `AgentResponse` representing unresolved provider invocation SHALL use error status and failure kind
`provider_commit_unknown`. Failure-kind values SHALL form a closed validated enumeration. Consumers SHALL NOT infer
commit-unknown from free-text error content.

#### Scenario: Commit-unknown response validates

- **WHEN** status is error
- **AND** failure kind is `provider_commit_unknown`
- **THEN** validation succeeds
- **AND** consumers classify the outcome without parsing error text

#### Scenario: Unknown failure kind is received

- **WHEN** failure kind is non-empty and outside the closed enumeration
- **THEN** validation fails permanently

### Requirement: Started markers do not claim invocation certainty

Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once
mechanism.

#### Scenario: Process stops after a started marker

- **WHEN** a process records a pre-call marker and stops before provider invocation
- **THEN** replacement does not classify the marker as proof of invocation
- **AND** provider ambiguity follows the configured policy
