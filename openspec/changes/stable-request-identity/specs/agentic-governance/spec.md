# agentic-governance Delta

## ADDED Requirements

### Requirement: Governance publications are durably at-least-once

Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required
correlation and receive PubAck before source ACK. PubAck uncertainty MAY repeat a publication. `Nats-Msg-Id` MAY
provide bounded duplicate suppression but SHALL NOT be treated as permanent publication identity.

The exact retained-verdict read exists only at the governance waiter-loss boundary. Ordinary validated outputs and
proposals require no exact committed-output lookup. Conflicting proposal or verdict correlation SHALL quarantine;
absence outside admitted retention SHALL remain unknown. No general stream scan or new verdict authority is
introduced.

#### Scenario: Validated output may repeat

- **WHEN** validation input redelivers after its validated output was published
- **THEN** governance may publish the correlated validated output again
- **AND** acknowledges only after the required publication receives PubAck

#### Scenario: Validated output publication is retried

- **WHEN** the first validated-output PubAck is uncertain
- **THEN** retry may repeat the correlated validated output
- **AND** source ACK still waits for PubAck

