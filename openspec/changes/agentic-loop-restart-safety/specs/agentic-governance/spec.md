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

For an allowed message, done SHALL require durable at-least-once publication through the declared JetStream output
and synchronous PubAck. For a blocked message, done SHALL be the completed policy decision and deliberate
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
- **THEN** the source retries and the validated output may repeat
- **AND** no core-NATS fallback authorizes ACK

#### Scenario: Policy blocks a message

- **WHEN** policy completes and refuses forwarding
- **THEN** source may be acknowledged because non-forwarding is the terminal consequence
- **AND** audit failure remains observable without reversing the policy decision

#### Scenario: Filter dependency fails

- **WHEN** a transient dependency prevents the filter chain from completing
- **THEN** source retries and no log-only return becomes ACK

#### Scenario: Governance handler panics or correlation conflicts

- **WHEN** validation panics or observes conflicting required proposal/verdict correlation
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

At this boundary, a matching validated retained verdict SHALL be reused. A successful exact lookup returning
typed absence SHALL permit republication of the same exactly correlated proposal for current-policy evaluation,
without proving a finite verdict-retention horizon. The resulting decision MAY differ from an expired earlier
verdict. Absence SHALL NOT authorize approval or establish that no prior decision existed.
Failed or unresolved reads SHALL Retry without re-proposal; required-correlation conflicts SHALL Quarantine.
Recovery SHALL read both exact approved and rejected verdict subjects for the execution. If both contain
validated opposing verdicts that each match the same originating proposal, recovery SHALL Quarantine,
positively settle neither the response nor those verdicts on that basis, and stop the affected response
delivery owner. It SHALL NOT choose by decision, arrival order, read order or timestamp.
Existing invalid-input classifications, disabled/audit behavior, required PubAck, source-settlement obligations
and separate durable protection against repeated non-repeatable tool effects SHALL remain unchanged.

#### Scenario: Verdict arrives after waiter loss

- **WHEN** an exact valid verdict arrives after replacement with no process waiter
- **THEN** while retained, it remains recoverable by redelivered response work
- **AND** source settlement does not depend on process-channel presence

#### Scenario: Verdict identity conflicts

- **WHEN** retained verdict identity or fingerprint conflicts with the proposal
- **THEN** the delivery quarantines rather than selecting one value

#### Scenario: Matching retained verdict is reused

- **WHEN** exact lookup returns a validated verdict matching the originating proposal
- **THEN** response recovery reuses that verdict without another policy evaluation

#### Scenario: Both retained decisions match the proposal

- **GIVEN** the exact approved and rejected subjects each retain a validated verdict
- **AND** both verdicts match the same originating proposal
- **WHEN** response recovery reads that evidence
- **THEN** the response delivery quarantines and its affected consumer stops without positive settlement
- **AND** neither decision is selected and no proposal or tool work is published

#### Scenario: Exact absence permits current-policy evaluation

- **GIVEN** response recovery has the same exactly correlated proposal and no matching live waiter
- **WHEN** successful exact retained-verdict lookup returns typed absence
- **THEN** it may republish that proposal without a finite verdict-retention horizon prerequisite
- **AND** current policy may decide differently from an expired earlier verdict
- **AND** absence itself supplies neither approval nor historical-decision proof

#### Scenario: Failed lookup is not absence

- **WHEN** the exact retained-verdict read fails or remains unresolved
- **THEN** response recovery retries without republishing or assuming approval

### Requirement: Governance publications are durably at-least-once

Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required
correlation and receive PubAck before source ACK. PubAck uncertainty MAY repeat a publication. `Nats-Msg-Id` MAY
provide bounded duplicate suppression but SHALL NOT be treated as permanent publication identity.

The exact retained-verdict read exists only at the governance waiter-loss boundary. Ordinary validated outputs and
proposals require no general exact committed-output lookup. Conflicting proposal or verdict correlation SHALL
Quarantine. Absence outside admitted retention SHALL leave the historical decision unknown; it SHALL NOT prohibit
the current-policy re-evaluation permitted after successful exact typed absence by
`Governance verdict correlation survives process replacement`. No general stream scan or new verdict authority
is introduced.

#### Scenario: Validated output may repeat

- **WHEN** validation input redelivers after its validated output was published
- **THEN** governance may publish the correlated validated output again
- **AND** acknowledges only after the required publication receives PubAck

#### Scenario: Validated output publication is retried

- **WHEN** the first validated-output PubAck is uncertain
- **THEN** retry may repeat the correlated validated output
- **AND** source ACK still waits for PubAck

### Requirement: Governance shutdown closes every delivery owner

Governance shutdown SHALL stop admission, drain all validation and verdict consume handles, await every handle's
exact `Closed` signal, then cancel and join owner-stop observers, filter work, and verdict-correlation work. Shutdown
SHALL NOT return while a callback can publish, settle, or write process correlation.

#### Scenario: Shutdown races validation and verdict work

- **WHEN** governance Stop begins with validation and verdict callbacks active
- **THEN** admission stops, every exact handle drains and closes, and all work joins
- **AND** Stop returns only after no later ACK, publication, or waiter mutation is possible

### Requirement: Governance verdicts use one registered wire and typed handoff

Framework rule publications within `agent.toolcall.approved.>` and `agent.toolcall.rejected.>` SHALL use the existing
registered `core.json.v1` BaseMessage carrier. The publish action SHALL preserve its complete existing inner wrapper
and properties. Approve, publish and deny authoring and audit semantics SHALL remain as specified by their existing
owners.

Verdict intake SHALL decode through its configured registry, explicitly validate the decoded message and require
GenericJSON. It SHALL NOT accept a raw-map fallback or reserialize/redecode a verdict between intake and the existing
GovernanceDispatcher.

The existing dispatcher SHALL accept VerdictPayload directly. One private implementation SHALL normalize and
validate both wire-derived and direct typed inputs. No additional public payload, normalization API or serializer
SHALL be introduced.

Decision, LoopID, RequestID, ExecutionID and proposal fingerprint SHALL be nonempty and correctly typed.
Decision SHALL be approved or rejected; ExecutionID SHALL be one concrete NATS subject token.
Conflicting supplied correlation SHALL Quarantine; missing or malformed required input SHALL Terminate.
Optional CallID and diagnostic context SHALL NOT become required. Missing correlation SHALL NOT be inferred
from routing or process state.

Reason and RuleID SHALL each use the nonempty top-level string, otherwise the corresponding string in
Properties, otherwise empty. Diagnostic disagreement SHALL follow that precedence and SHALL NOT be classified
as a correlation conflict. Normalization SHALL NOT modify the supplied Properties map. For every valid verdict
represented equivalently as registered wire input and direct VerdictPayload input, normalization SHALL produce
the same decision, correlation and optional diagnostic context. Refused input SHALL NOT mutate a waiter.

Actual delivered subject SHALL equal the normalized decision/execution subject. A conflicting subject SHALL
Quarantine before dispatcher effects. Valid verdict context SHALL reach the dispatcher without carrier-dependent
loss. These rules do not replace the separately required proposal-match and retained-verdict proofs.

#### Scenario: Both rule authoring paths survive the production codec

- **WHEN** approve or publish emits a valid verdict in either protocol family
- **THEN** production registry decoding exposes the expected inner fields
- **AND** publish retains its entire wrapper/properties and dispatcher context survives.

#### Scenario: Invalid or conflicting input cannot reach a waiter

- **WHEN** wire or direct typed input lacks required correlation, has malformed required fields, or contains
  conflicting correlation
- **THEN** it receives the specified Terminate or Quarantine disposition before waiter mutation
- **AND** omitted optional context alone does not refuse it.

#### Scenario: Transport identity cannot repair or override payload identity

- **WHEN** a verdict payload lacks required identity or disagrees with the actual subject
- **THEN** intake refuses with the specified disposition
- **AND** neither subject nor process state supplies a replacement value.
