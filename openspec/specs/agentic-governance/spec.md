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

### Requirement: Governance publications are durably at-least-once

Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required
correlation and receive PubAck before source ACK. PubAck uncertainty MAY repeat a publication. `Nats-Msg-Id` MAY
provide bounded duplicate suppression but SHALL NOT be treated as permanent publication identity.

The exact retained-verdict read exists only at the governance waiter-loss boundary. Ordinary validated outputs and
proposals require no exact committed-output lookup. Absence outside admitted retention SHALL remain unknown. No
general stream scan or new verdict authority is introduced.

Verdict dispositions SHALL follow what the arriving message can be, not what the operator would prefer. A verdict
missing its decision or its execution identity SHALL terminate — redelivery cannot supply either. A SECOND verdict
under one execution identity SHALL quarantine, because only one of the two can have been acted on and which one is
not knowable from the message.

A verdict naming no active waiter SHALL be settled against the LOOP it names rather than retried by default, and
the execution identity cannot supply that loop — it is an opaque digest — so the loop comes from the verdict's own
`loop_id`, or from the RequestID grammar when a rule echoes only that. Three dispositions follow, and each is
required for a different reason. A verdict whose loop is finished, or belongs to another process's tree on a shared
stream, SHALL be acknowledged: that is the documented-normal case the missing-waiter counter exists for, and
retrying it is a hot redelivery loop no record can ever end. A verdict whose loop is LIVE but held by no waiter in
this process SHALL remain retryable, because the loop is still owed its answer and a replacement process may
register the waiter. A verdict carrying no recoverable loop identity at all SHALL terminate as malformed and be
counted, because acknowledging it is indistinguishable from "that loop finished" — which is how a rule echoing a
non-canonical identity would lose every verdict it publishes with no signal naming why.

`ProposalFingerprint` SHALL be carried, not verified: agentic-loop mints it onto the proposal, the rule engine
echoes it onto the verdict, and agentic-loop decodes it as audit context — audit mode SHALL record it on the
observed-verdict log line, which is the whole of what "audit context" means here. Routing SHALL use the execution
identity alone, so a verdict whose fingerprint disagrees with its proposal SHALL still reach its waiter. Enforcing
the comparison requires the proposal's fingerprint to outlive the process that registered the waiter — durable
per-call governance state that NO layer of this stack owns: L4 (#1330) carries durable loop state, not durable
per-call proposal state. Absent a new issue claiming it, the fingerprint is an audit token only, and no layer
verifies it.

The verdict handed to the dispatcher SHALL be the DECODED one, and every field read off it SHALL tolerate both
published shapes — top level, and nested under `properties`. The rule engine's approve action publishes a
`core.json.v1` envelope and the canonical reject pattern publishes a raw map whose fields all sit under
`properties`, so a dispatcher that unmarshals the wire bytes itself reads the empty string for every field it takes
from the top level: all of them for the envelope shape, and the audit fingerprint and rule id for the raw-map shape.
Normalization SHALL happen in one place, not once per dispatcher.

#### Scenario: Validated output may repeat

- **WHEN** validation input redelivers after its validated output was published
- **THEN** governance may publish the correlated validated output again
- **AND** acknowledges only after the required publication receives PubAck

#### Scenario: Validated output publication is retried

- **WHEN** the first validated-output PubAck is uncertain
- **THEN** retry may repeat the correlated validated output
- **AND** source ACK still waits for PubAck

#### Scenario: A verdict arrives without routing identity

- **WHEN** a verdict carries no decision or no execution identity
- **THEN** it is terminated rather than retried
- **AND** no waiter is consulted

#### Scenario: A verdict names no waiter and its loop is finished or foreign

- **WHEN** a verdict's execution identity has no registered waiter and the loop it names is terminal, absent, or
  another process's
- **THEN** the delivery is acknowledged
- **AND** the missing-waiter observation is recorded rather than the delivery being retried

#### Scenario: A verdict names no waiter and its loop is still live

- **WHEN** a verdict's execution identity has no registered waiter and the loop it names is non-terminal
- **THEN** the delivery remains retryable, because that loop is still owed its answer
- **AND** the missing-waiter observation is recorded

#### Scenario: A verdict names no waiter and no recoverable loop

- **WHEN** a verdict carries neither a canonical `loop_id` nor a `request_id` in the `<loopID>:req:<iteration>:<retry>`
  grammar
- **THEN** the delivery terminates as malformed rather than being acknowledged as if its loop had settled
- **AND** the unrecoverable-identity observation is recorded, so a rule echoing a non-canonical identity is visible

#### Scenario: Two verdicts name one execution identity

- **WHEN** a second verdict arrives for an execution identity whose waiter already holds one
- **THEN** the delivery quarantines
- **AND** neither verdict silently replaces the other

#### Scenario: A verdict arrives in either published shape

- **WHEN** a verdict arrives as an approve-action envelope, or as a publish-action map whose fields are nested
  under `properties`
- **THEN** the audit line records the decision, execution identity, rule, reason and fingerprint the rule actually
  echoed, for either shape
- **AND** an enforce-mode waiter receives that same reason, so the refusal the model reads is never reasonless

#### Scenario: A verdict's fingerprint disagrees with its proposal

- **WHEN** an echoed `proposal_fingerprint` does not match the proposal that minted it
- **THEN** the verdict still reaches the waiter named by its execution identity
- **AND** the disagreement is carried as audit context rather than refused

