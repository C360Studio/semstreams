## ADDED Requirements

### Requirement: Heartbeat consumption SHALL expose settlement failure

`ConsumeWithHeartbeat` SHALL return ACK, delayed NAK, and Term settlement errors to its caller while preserving the
existing heartbeat and shutdown delays. It SHALL not discard a settlement error after work has returned.

#### Scenario: transient work fails and delayed NAK fails

- **WHEN** work returns a transient error
- **AND** `NakWithDelay` also fails
- **THEN** the returned error chain contains both failures

#### Scenario: shutdown NAK fails

- **WHEN** context cancellation owns the delivery outcome
- **AND** the five-second delayed NAK fails
- **THEN** the returned error chain contains context cancellation and the settlement failure
