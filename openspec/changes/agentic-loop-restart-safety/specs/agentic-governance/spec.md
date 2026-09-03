## ADDED Requirements

### Requirement: Governance validation settles after its declared consequence

The task, request, and response validation subscriptions SHALL return classified outcomes through their three
existing private binding owners. Native messages and settlement methods SHALL NOT enter filter business logic or an
exported work-owning no-heartbeat adapter.

Each physical subscription SHALL invoke its typed business handler using the callback installed by its production
setup branch. All delivery-derived work SHALL join before the private callback passes its decision and cause to
`natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; governance SHALL NOT
derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout. A physical
subscription SHALL move to the existing heartbeat owner only after measured legitimate work can exceed its
configured acknowledgement interval. Cancellation-ignoring or non-returning work SHALL fail lifecycle review.

For an allowed message, done SHALL require deterministic publication through the declared JetStream output and
synchronous PubAck. For a blocked message, done SHALL be the completed policy decision and deliberate
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
- **THEN** the source retries with the same semantic output identity
- **AND** no core-NATS fallback authorizes ACK

#### Scenario: Policy blocks a message

- **WHEN** policy completes and refuses forwarding
- **THEN** source may be acknowledged because non-forwarding is the terminal consequence
- **AND** audit failure remains observable without reversing the policy decision

#### Scenario: Filter dependency fails

- **WHEN** a transient dependency prevents the filter chain from completing
- **THEN** source retries and no log-only return becomes ACK

#### Scenario: Governance handler panics

- **WHEN** validation panics or observes a semantic identity collision
- **THEN** the delivery quarantines and the exact owner stops

#### Scenario: Governance business work reaches its own deadline

- **WHEN** a delivery-owned governance operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

### Requirement: Governance verdict correlation survives process replacement

Every proposal SHALL carry LoopID, RequestID, execution identity, and proposal fingerprint. Verdict subjects SHALL
use the NATS-safe execution identity. A response handler without a process waiter SHALL validate and read the exact
retained verdict before republishing a proposal. Missing or full waiter channels SHALL NOT authorize completed
log-and-drop. No governance bucket SHALL be added unless a named replacement failpoint proves retained verdict and
response redelivery insufficient.

#### Scenario: Verdict arrives after waiter loss

- **WHEN** an exact valid verdict arrives after replacement with no process waiter
- **THEN** it remains recoverable by redelivered response work
- **AND** source settlement does not depend on process-channel presence

#### Scenario: Verdict identity conflicts

- **WHEN** retained verdict identity or fingerprint conflicts with the proposal
- **THEN** the delivery quarantines rather than selecting one value

### Requirement: Governance publication identity is deterministic and reconcilable

Every validated task, request, response, proposal, and verdict publication SHALL derive a stable identity and
canonical fingerprint from its source identity and SHALL use deterministic `Nats-Msg-Id`. Before repeating an
allowed validated output or proposal after replacement, governance SHALL perform an operation-specific exact
committed-output lookup and validate its content. A matching output proves publication; a conflicting identity SHALL
quarantine; absence outside admitted retention SHALL remain unknown. No general stream scan or new verdict authority
is introduced.

#### Scenario: Validated output already committed

- **WHEN** validation input redelivers after its exact validated output committed
- **THEN** governance verifies the retained identity and canonical fingerprint
- **AND** acknowledges without producing a differently identified output

#### Scenario: Validated output publication is retried

- **WHEN** the first validated-output PubAck is uncertain
- **THEN** retry uses the same deterministic `Nats-Msg-Id`
- **AND** content mismatch quarantines instead of overwriting

### Requirement: Governance shutdown closes every delivery owner

Governance shutdown SHALL stop admission, drain all validation and verdict consume handles, await every handle's
exact `Closed` signal, then cancel and join owner-stop observers, filter work, and verdict-correlation work. Shutdown
SHALL NOT return while a callback can publish, settle, or write process correlation.

#### Scenario: Shutdown races validation and verdict work

- **WHEN** governance Stop begins with validation and verdict callbacks active
- **THEN** admission stops, every exact handle drains and closes, and all work joins
- **AND** Stop returns only after no later ACK, publication, or waiter mutation is possible
