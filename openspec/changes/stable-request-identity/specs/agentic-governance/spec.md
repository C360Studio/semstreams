# agentic-governance Delta

## ADDED Requirements

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
`properties`, so a dispatcher that unmarshals the wire bytes itself reads the empty string for BOTH: the audit
fingerprint, rule and reason are lost, and in enforce mode the reason the model is told its call was refused with
is lost with them. Normalization SHALL happen in one place, not once per dispatcher.

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
