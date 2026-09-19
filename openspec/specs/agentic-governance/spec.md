# agentic-governance Specification

## Purpose

`agentic-governance` governs **the policy gate an agentic message passes before it is allowed to become work**:
content policy, PII redaction, injection detection, and rate limiting over the task, request, and response
subscriptions. This spec was seeded when `settle-after-durable-effect` first touched the capability, so it
currently states one thing only — that each of the three validation subscriptions settles its delivery after the
consequence it declared has completed, through its own binding owner, with native messages and settlement methods
kept out of filter business logic. The rest of the capability's behavior is not yet specified here and is seeded
lazily by the change that next touches it.

## Requirements
### Requirement: Governance validation settles after its declared consequence

The task, request, and response validation subscriptions SHALL return classified outcomes through their three
existing private binding owners, and SHALL NOT settle before the declared consequence has completed. Native messages
and settlement methods SHALL NOT enter filter business logic or an exported work-owning no-heartbeat adapter.

Each physical subscription SHALL invoke its typed business handler using the callback installed by its production
setup branch. All delivery-derived work SHALL join before the private callback passes its decision and cause to
`natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; governance SHALL NOT
derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout.

For an allowed message, done SHALL require publication through the declared JetStream output and synchronous
PubAck. For a blocked message, done SHALL be the completed policy decision and deliberate
non-forwarding. The existing audit contract remains nonblocking, but decode, filter, output-subject, marshal, and
required publication failures SHALL NOT become ACK.

The first owner-fatal result across governance validation owners SHALL synchronously latch before exact-handle drain.
Existing Health SHALL report `Healthy=false`, status `delivery ownership lost`, and the exact first cause in
`LastError`. The existing cumulative error count SHALL increase exactly once for owner loss, independently of prior
business-error counts; later owner-fatal results SHALL not increment it again. No new metric family, public state,
durable state, or communication path is added.

#### Scenario: Allowed output publication fails

- **WHEN** policy allows a message
- **AND** its declared validated output does not receive PubAck
- **THEN** the delivery quarantines and the exact owner stops, because the publication's durable state is unknown and
  the validated output carries no identity a redelivery could republish against
- **AND** no core-NATS fallback authorizes ACK

#### Scenario: Policy blocks a message

- **WHEN** policy completes and refuses forwarding
- **THEN** source may be acknowledged because non-forwarding is the terminal consequence
- **AND** audit failure remains observable without reversing the policy decision

#### Scenario: A malformed validation input is terminated, never acknowledged as done

- **WHEN** a production governance callback receives an input it cannot decode
- **THEN** it returns Terminate with a non-nil cause
- **AND** no log-only return becomes ACK

#### Scenario: Governance handler panics

- **WHEN** validation panics or observes a semantic identity collision
- **THEN** the delivery quarantines and the exact owner stops
- **AND** the first fatal cause latches into health before the exact handle drains

#### Scenario: Governance business work reaches its own deadline

- **WHEN** a delivery-owned governance operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

