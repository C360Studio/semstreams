## ADDED Requirements

### Requirement: Model request settlement is bound to a durable response

Agentic-model SHALL not positively acknowledge an `AgentRequest` until a matching `AgentResponse` has received
synchronous JetStream PubAck. It SHALL use RequestID as the stable provider-work correlation and SHALL read an
already committed matching response before invoking a provider.

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

- **WHEN** a request is redelivered and an exact committed response with matching RequestID and fingerprint exists
- **THEN** agentic-model does not invoke the provider
- **AND** positively acknowledges the source request

Exact response lookup SHALL occur only after the model owner's local AGENT replay-admission gate succeeds. Absence
outside an admitted retention horizon is unknown and SHALL NOT prove that a provider invocation did not occur.

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
- **AND** performs no heartbeat or settlement call
- **AND** drains the exact consume handle
- **AND** component health becomes negative with the exact cause and one error-count increment

#### Scenario: Delivery attempt observation is immutable and bounded

- **WHEN** agentic-model receives delivery-attempt observation
- **THEN** it may observe only `Number`, `MetadataAvailable`, and `IsRedelivery`
- **AND** it cannot access a native message, settlement method, sequence, consumer identity, header, or mutable state

### Requirement: Provider commit-unknown behavior is explicit

Agentic-model `Config` SHALL add exact field
`ProviderAmbiguityPolicy string` with JSON tag `provider_ambiguity_policy,omitempty` and schema enum
`fail_commit_unknown|at_least_once|provider_reconcile`, default `fail_commit_unknown`, category `advanced`, and a
description that names paid/effectful duplicate risk. Its closed string values
SHALL be `fail_commit_unknown`, `at_least_once`, and `provider_reconcile`. Omission or the empty string SHALL default
to `fail_commit_unknown`; any other value SHALL fail configuration validation before consumer allocation. Generated
schema, shipped fixtures, and model operator documentation SHALL carry the same enum and default.

`provider_reconcile` SHALL be admitted only when every endpoint reachable by the component's current model registry
(direct endpoint, capability chain, and default) supplies the package-private `providerCommitReconciler` capability.
Its exact method SHALL be
`ReconcileProviderCommit(context.Context, agentic.AgentRequest) (providerReconcileResult, error)`, where the closed
result kind is `exact_match`, `proven_not_invoked`, `unresolved`, or `collision` and an exact-match result carries the
validated `AgentResponse`. The method SHALL observe by stable RequestID without invoking the provider. Component
setup SHALL enumerate `RegistryReader.ListEndpoints`, resolve
each configured route, construct its internal execution client, and refuse readiness with typed
`provider_reconcile_unsupported` before allocating the request consumer when any reachable endpoint lacks the
capability. No shipped provider at checkpoint P declares this capability; therefore selecting `provider_reconcile`
is a setup refusal until an independently tested backend implements it. Formatting-only `ProviderAdapter` and
`ResponsesAdapter` SHALL NOT imply reconciliation support.

#### Scenario: Default policy sees unresolved redelivery

- **WHEN** a request is redelivered
- **AND** no matching durable response exists
- **AND** the framework cannot prove whether the prior process invoked the provider
- **THEN** agentic-model does not invoke the provider again
- **AND** publishes a typed commit-unknown `AgentResponse`
- **AND** acknowledges only after that response receives PubAck

#### Scenario: Default unresolved redelivery performs no second provider call

- **WHEN** a redelivered request has no matching committed response
- **AND** policy is `fail_commit_unknown`
- **THEN** provider invocation count for that delivery is zero
- **AND** the typed commit-unknown response receives PubAck before source ACK

#### Scenario: Response publication fails after provider return

- **WHEN** a provider returns and response publication receives no PubAck
- **THEN** the source is not positively acknowledged
- **AND** replacement applies configured ambiguity policy without assuming the provider did not run

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

- **WHEN** every endpoint reachable by configured direct/default/capability routing declares the internal
  reconciliation capability keyed by RequestID
- **THEN** agentic-model admits `provider_reconcile` before consumer allocation
- **AND** on delivery it reconciles before invoking and publishes the reconciled or proven-safe new result with the
  same stable identity

#### Scenario: Provider reconciliation is unsupported at setup

- **WHEN** `provider_ambiguity_policy` is `provider_reconcile`
- **AND** any reachable endpoint lacks the internal reconciliation capability
- **THEN** setup refuses with `provider_reconcile_unsupported` naming that endpoint
- **AND** no request consumer or provider call is created

#### Scenario: Provider ambiguity policy is omitted

- **WHEN** `provider_ambiguity_policy` is omitted or empty
- **THEN** validation installs `fail_commit_unknown`

#### Scenario: Provider ambiguity policy is unknown

- **WHEN** `provider_ambiguity_policy` contains any other string
- **THEN** configuration validation fails before consumer allocation

### Requirement: Provider commit-unknown is machine-readable

`AgentResponse` SHALL expose the exact optional JSON field `failure_kind`. Its Go field SHALL be
`FailureKind AgentResponseFailureKind` with `json:"failure_kind,omitempty"`. `AgentResponseFailureKind` SHALL encode
as a JSON string whose only non-empty admitted value in this change is `provider_commit_unknown`. Empty is omitted
and remains valid for existing ordinary responses. A non-empty failure kind SHALL be valid only when `status` is
`error`; unknown strings or a non-empty failure kind on another status SHALL fail validation. Consumers SHALL NOT
infer commit-unknown from free-text `error` content.

#### Scenario: Commit-unknown response validates

- **WHEN** status is error
- **AND** failure kind is `provider_commit_unknown`
- **THEN** validation succeeds
- **AND** consumers classify the outcome without parsing error text

#### Scenario: Unknown failure kind is received

- **WHEN** failure kind is non-empty and outside the closed enumeration
- **THEN** validation fails permanently

#### Scenario: Failure kind appears on a successful response

- **WHEN** `failure_kind` is non-empty and `status` is not `error`
- **THEN** validation fails permanently

#### Scenario: Ordinary response omits failure kind

- **WHEN** an ordinary response has an empty failure kind
- **THEN** JSON omits `failure_kind` and existing response semantics remain unchanged

### Requirement: Started markers do not claim invocation certainty

Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once
mechanism.

#### Scenario: Process stops after a started marker

- **WHEN** a process records a pre-call marker and stops before provider invocation
- **THEN** replacement does not classify the marker as proof of invocation
- **AND** provider ambiguity follows the configured policy

### Requirement: Model heartbeat policy is valid before acquisition

Agentic-model SHALL default to AckWait 120s and heartbeat 60s. It SHALL validate the exact acquisition config before
allocating a consumer. Heartbeat SHALL be no greater than half the shortest positive BackOff when BackOff exists,
otherwise no greater than half positive AckWait or the effective 30s server default.

#### Scenario: Legacy model default is refused before allocation

- **WHEN** setup observes heartbeat 90s and AckWait 120s
- **THEN** setup returns a typed policy error naming the observed values and 60s ceiling
- **AND** allocates no consumer

### Requirement: Model response publication is durably at-least-once

Every required `AgentResponse`, including success, provider error, and commit-unknown, SHALL carry the source
RequestID and receive PubAck before source ACK. PubAck uncertainty MAY repeat the response. `Nats-Msg-Id` MAY provide
bounded duplicate suppression but SHALL NOT be treated as permanent publication identity.

The operation-specific exact committed-response read exists only at the provider-invocation boundary. A matching
RequestID and content protects against repeating provider work; a conflict SHALL quarantine; absence outside admitted
retention SHALL remain unknown. No general stream scan or exact-read requirement for ordinary response publication
is admitted.

#### Scenario: Commit-unknown publication repeats

- **WHEN** a commit-unknown response publication is retried after an uncertain PubAck
- **THEN** the response may repeat with the same RequestID
- **AND** source ACK still waits for one response PubAck

#### Scenario: Matching retained response protects provider work

- **WHEN** exact retained response lookup finds matching RequestID and content before provider invocation
- **THEN** agentic-model does not invoke the provider again
- **AND** positively acknowledges the redelivered request

#### Scenario: Existing response content conflicts

- **WHEN** exact lookup finds the expected RequestID with conflicting response content
- **THEN** agentic-model quarantines before provider invocation

### Requirement: Model shutdown closes its delivery owner

Agentic-model shutdown SHALL stop admission, drain its exact request consume handle, await exact `Closed`, then
cancel and join its owner-stop observer and all delivery work. Shutdown SHALL NOT return while provider or
publication work can later settle the source.

#### Scenario: Shutdown races provider work

- **WHEN** model Stop begins during a request callback
- **THEN** admission stops, the request handle drains and closes, and callback work joins
- **AND** Stop returns only after no later ACK or response publication is possible
