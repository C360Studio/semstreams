# agentic-model Delta

## ADDED Requirements

### Requirement: Model request settlement is bound to a durable response

Agentic-model SHALL NOT positively acknowledge an `AgentRequest` until a matching `AgentResponse` has received
synchronous JetStream PubAck. It SHALL use RequestID as the stable logical response identity.

Agentic-model SHALL receive immutable delivery-attempt observation from the accepted settlement adapter. It SHALL
NOT receive or retain native message or settlement authority.

The first fatal result from the model delivery owner SHALL synchronously latch into the component's existing health
surface before owner-stop observation can drain the handle. Health SHALL report `Healthy=false`, status
`delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the existing error count.
Later fatal results SHALL neither overwrite the first cause nor increment the count again. This adds no metric family,
public state, durable state, or communication path.

#### Scenario: Response publication succeeds

- **WHEN** a provider returns an `AgentResponse` for a valid `AgentRequest`
- **THEN** agentic-model publishes the response with deterministic identity
- **AND** waits for PubAck
- **AND** only then positively acknowledges the source request

#### Scenario: Delivery metadata is unavailable

- **WHEN** the settlement adapter cannot observe native delivery metadata
- **THEN** it does not invoke agentic-model work
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** stops the exact delivery owner
- **AND** performs no heartbeat or settlement call
- **AND** drains the exact consume handle
- **AND** component health becomes negative with the exact cause and one error-count increment

#### Scenario: Delivery attempt observation is immutable and bounded

- **WHEN** agentic-model receives delivery-attempt observation
- **THEN** it may observe only `Number`, `MetadataAvailable`, and `IsRedelivery`
- **AND** it cannot access a native message, settlement method, sequence, consumer identity, header, or mutable state

### Requirement: Model heartbeat policy is valid before acquisition

Agentic-model SHALL default to AckWait 120s and heartbeat 60s, and SHALL validate the exact acquisition config
before allocating a consumer. Heartbeat SHALL be no greater than half the shortest positive BackOff when BackOff
exists, otherwise no greater than half positive AckWait or the effective 30s server default.

#### Scenario: Legacy model default is refused before allocation

- **WHEN** setup observes heartbeat 90s and AckWait 120s
- **THEN** setup returns a typed policy error naming the observed values and 60s ceiling
- **AND** allocates no consumer

#### Scenario: The declared default port resolves a valid policy

- **WHEN** the shipped `agent.request` port definition resolves its acquisition config
- **THEN** its declared AckWait and heartbeat satisfy the ceiling
- **AND** every shipped model configuration fixture satisfies it as well
