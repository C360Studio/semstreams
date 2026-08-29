## ADDED Requirements

### Requirement: Model request settlement is bound to a durable response

Agentic-model SHALL not positively acknowledge an `AgentRequest` until a matching `AgentResponse` has received
synchronous JetStream PubAck. It SHALL use RequestID as the stable logical response identity and SHALL reconcile an
already committed matching response before invoking a provider.

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

#### Scenario: At-least-once is selected

- **WHEN** the operator explicitly selects `at_least_once`
- **AND** an unresolved request is redelivered
- **THEN** agentic-model may invoke the provider again with the same RequestID
- **AND** records the repeated-attempt classification

#### Scenario: Provider reconciliation is supported

- **WHEN** the selected adapter demonstrates reconciliation keyed by RequestID
- **THEN** agentic-model reconciles before invoking
- **AND** publishes the reconciled or newly idempotent result with the same stable identity

### Requirement: Started markers do not claim invocation certainty

Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once
mechanism.

#### Scenario: Process stops after a started marker

- **WHEN** a process records a pre-call marker and stops before provider invocation
- **THEN** replacement does not classify the marker as proof of invocation
- **AND** provider ambiguity follows the configured policy
