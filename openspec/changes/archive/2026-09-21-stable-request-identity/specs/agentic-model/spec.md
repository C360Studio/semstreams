# agentic-model Delta

## ADDED Requirements

### Requirement: Request delivery settles only on its own response

Agentic-model SHALL NOT positively acknowledge an `agent.request` delivery before the response that answers it has
received PubAck, and SHALL NOT settle any delivery through a log-only path. Every request callback SHALL return a
classified `DeliveryDecision` naming what it observed: no void handler, no acknowledgement that a log line is the
only record of.

A redelivered request whose RequestID already names a committed, validated `AgentResponse` SHALL be acknowledged
from that response without a second provider call. This rule, not duplicate-window suppression, is what bounds
provider work under at-least-once delivery (owner ruling 2026-09-05 on #1146).

#### Scenario: The request cannot be parsed

- **WHEN** a delivery's bytes do not decode into an `AgentRequest`
- **THEN** agentic-model terminates the delivery with the classified decode error
- **AND** performs no retained-response lookup, because the delivery names no RequestID to correlate

#### Scenario: Endpoint resolution fails before any provider call

- **WHEN** the request names a model or endpoint the registry cannot resolve
- **THEN** agentic-model publishes an error response carrying the source RequestID
- **AND** acknowledges the source only after that response receives PubAck
- **AND** invokes no provider
- **AND** retries the delivery instead if the error response cannot be published

#### Scenario: The provider call fails

- **WHEN** the provider returns an error or the call itself fails
- **THEN** agentic-model publishes an error response carrying the source RequestID
- **AND** acknowledges the source only after that response receives PubAck
- **AND** retries the delivery instead if the error response cannot be published

#### Scenario: The response cannot be published after a provider result

- **WHEN** a provider result exists but its response publication produces no observed PubAck
- **THEN** agentic-model retries the delivery and the source remains unacknowledged
- **AND** the replacement repeats the retained-response check, whose typed absence may invoke the provider again
  under the accepted at-least-once contract

#### Scenario: A retained response disagrees with the request it claims to answer

- **WHEN** a retained response is found whose correlation does not match the request that looked it up
- **THEN** agentic-model quarantines the delivery before any provider call
- **AND** invokes no provider, because a conflicting answer is not evidence about this request either way

#### Scenario: The retained-response lookup itself fails

- **WHEN** the retained-response read fails rather than reporting a typed absence
- **THEN** agentic-model retries the delivery before any provider call
- **AND** invokes no provider, because an unread ledger is not an absent answer

#### Scenario: A republished request is answered from its retained response

- **WHEN** the same RequestID is delivered again as a new stream message, with no duplicate-window suppression
  available
- **THEN** agentic-model finds the committed response and acknowledges without calling the provider
- **AND** the provider call count across both deliveries is one

### Requirement: Started markers do not claim invocation certainty

Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once
mechanism.

#### Scenario: Process stops after a started marker

- **WHEN** a process records a pre-call marker and stops before provider invocation
- **THEN** replacement does not classify the marker as proof of invocation
- **AND** the ordinary retained-response rule applies
- **AND** typed absence permits another provider invocation

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
