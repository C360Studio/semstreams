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
missing its decision or its execution identity SHALL terminate — redelivery cannot supply either. A verdict naming
no active waiter SHALL remain retryable, since a waiter may be registered by another process or a later attempt. A
SECOND verdict under one execution identity SHALL quarantine, because only one of the two can have been acted on
and which one is not knowable from the message.

`ProposalFingerprint` SHALL be carried, not verified: agentic-loop mints it onto the proposal, the rule engine
echoes it onto the verdict, and agentic-loop decodes it as audit context — audit mode SHALL record it on the
observed-verdict log line, which is the whole of what "audit context" means here. Routing SHALL use the execution
identity alone, so a verdict whose fingerprint disagrees with its proposal SHALL still reach its waiter. Enforcing
the comparison requires the proposal's fingerprint to outlive the process that registered the waiter — durable
per-call governance state that NO layer of this stack owns: L4 (#1330) carries durable loop state, not durable
per-call proposal state. Absent a new issue claiming it, the fingerprint is an audit token only, and no layer
verifies it.

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

#### Scenario: A verdict names no active waiter

- **WHEN** a verdict's execution identity has no registered waiter
- **THEN** the delivery remains retryable
- **AND** the missing-waiter observation is recorded

#### Scenario: Two verdicts name one execution identity

- **WHEN** a second verdict arrives for an execution identity whose waiter already holds one
- **THEN** the delivery quarantines
- **AND** neither verdict silently replaces the other

#### Scenario: A verdict's fingerprint disagrees with its proposal

- **WHEN** an echoed `proposal_fingerprint` does not match the proposal that minted it
- **THEN** the verdict still reaches the waiter named by its execution identity
- **AND** the disagreement is carried as audit context rather than refused

