## ADDED Requirements

### Requirement: Model request settlement is bound to a durable response

Agentic-model SHALL treat provider invocation as durably at-least-once. It SHALL not positively acknowledge an
`AgentRequest` until a matching `AgentResponse` has received synchronous JetStream PubAck. It SHALL use RequestID as
the stable provider-work correlation and SHALL perform an operation-specific exact retained-response read before each
provider invocation.

A validated matching retained response SHALL prevent provider invocation and SHALL satisfy the durable-response
precondition for source ACK. Conflicting retained correlation SHALL quarantine before invocation. Typed retained
absence SHALL permit provider invocation with the same RequestID, including on redelivery after ambiguous process
replacement. A lookup error SHALL NOT be treated as absence.

Agentic-model SHALL receive immutable delivery-attempt observation from the accepted settlement adapter. It SHALL
NOT receive or retain native message or settlement authority.

The first fatal result from the model delivery owner SHALL synchronously latch into the component's existing health
surface before owner-stop observation can drain the handle. Health SHALL report `Healthy=false`, status
`delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the existing error count.
Later fatal results SHALL neither overwrite the first cause nor increment the count again. This adds no metric family,
public state, durable state, or communication path.

#### Scenario: Response publication succeeds

- **WHEN** a provider returns an `AgentResponse` for a valid `AgentRequest`
- **THEN** agentic-model publishes the response correlated by RequestID
- **AND** waits for PubAck
- **AND** only then positively acknowledges the source request

#### Scenario: Matching response already exists

- **WHEN** exact retained lookup finds a validated response matching RequestID and expected response correlation
- **THEN** agentic-model does not invoke the provider
- **AND** positively acknowledges the source request because the required response is already committed

#### Scenario: No matching response exists

- **WHEN** exact retained lookup returns typed not-found
- **THEN** agentic-model invokes the provider with the same stable RequestID
- **AND** this remains true on a redelivered request after ambiguous process replacement

#### Scenario: Retained response lookup fails

- **WHEN** exact retained lookup fails without proving typed absence
- **THEN** agentic-model retries without invoking the provider

#### Scenario: Response identity collides

- **WHEN** a committed response's subject RequestID, payload RequestID, and source request RequestID do not agree
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
- **AND** performs no heartbeat or settlement call
- **AND** drains the exact consume handle
- **AND** component health becomes negative with the exact cause and one error-count increment

#### Scenario: Delivery attempt observation is immutable and bounded

- **WHEN** agentic-model receives delivery-attempt observation
- **THEN** it may observe only `Number`, `MetadataAvailable`, and `IsRedelivery`
- **AND** it cannot access a native message, settlement method, sequence, consumer identity, header, or mutable state

### Requirement: Started markers do not claim invocation certainty

Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once
mechanism.

#### Scenario: Process stops after a started marker

- **WHEN** a process records a pre-call marker and stops before provider invocation
- **THEN** replacement does not classify the marker as proof of invocation
- **AND** the ordinary retained-response rule applies
- **AND** typed absence permits another provider invocation

### Requirement: Model heartbeat policy is valid before acquisition

Agentic-model SHALL default to AckWait 120s and heartbeat 60s. It SHALL validate the exact acquisition config before
allocating a consumer. Heartbeat SHALL be no greater than half the shortest positive BackOff when BackOff exists,
otherwise no greater than half positive AckWait or the effective 30s server default.

#### Scenario: Legacy model default is refused before allocation

- **WHEN** setup observes heartbeat 90s and AckWait 120s
- **THEN** setup returns a typed policy error naming the observed values and 60s ceiling
- **AND** allocates no consumer

### Requirement: Model response publication is durably at-least-once

Every required `AgentResponse`, including success and provider error, SHALL carry the source RequestID and receive
PubAck before source ACK. PubAck uncertainty MAY repeat the response. `Nats-Msg-Id` MAY provide bounded duplicate
suppression but SHALL NOT be treated as permanent publication identity.

The operation-specific exact committed-response read exists only at the provider-invocation boundary. A matching
validated response prevents repeated provider work; conflicting correlation SHALL quarantine; typed absence SHALL
permit another provider call with the same RequestID. No general stream scan, provider reconciliation capability,
ambiguity policy, or replay-admission prerequisite is admitted.

#### Scenario: Response publication is uncertain

- **WHEN** a provider returns but response publication does not produce an observed PubAck
- **THEN** the source remains unacknowledged
- **AND** replacement repeats the retained-response check
- **AND** typed absence may lead to another provider invocation

#### Scenario: Matching retained response protects provider work

- **WHEN** exact retained-response lookup finds matching validated correlation before provider invocation
- **THEN** agentic-model does not invoke the provider again
- **AND** positively acknowledges the source request

#### Scenario: Existing response correlation conflicts

- **WHEN** exact lookup finds a response whose subject RequestID, payload RequestID, and source request RequestID do
  not agree
- **THEN** agentic-model quarantines before provider invocation

### Requirement: Model shutdown closes its delivery owner

Agentic-model shutdown SHALL stop admission, drain its exact request consume handle, await exact `Closed`, then
cancel and join its owner-stop observer and all delivery work. Shutdown SHALL NOT return while provider or
publication work can later settle the source.

#### Scenario: Shutdown races provider work

- **WHEN** model Stop begins during a request callback
- **THEN** admission stops, the request handle drains and closes, and callback work joins
- **AND** Stop returns only after no later ACK or response publication is possible
