# nats-streaming Delta

## ADDED Requirements

### Requirement: JetStream remains durable restart authority

Semantic settlement SHALL use existing JetStream consumer position and redelivery. Quarantine SHALL attempt no
terminal method. The existing component owner SHALL stop its exact lane and ordinary reconstruction SHALL reacquire
durable ownership. #759 SHALL add no recovery ledger or durable quarantine state.

#### Scenario: quarantined delivery

- **WHEN** a delivery quarantines or loses heartbeat control
- **THEN** later admission closes and the exact existing owner stops the lane
- **AND** reconstructed ownership uses the existing durable consumer state

### Requirement: no-settlement BackOff and explicit semantic retry remain distinct

Consumer AckWait/BackOff SHALL govern lease renewal and server redelivery after missing settlement. Delivery retry
policy SHALL govern explicit Nak or NakWithDelay after semantic Retry. Preserving one SHALL NOT rewrite the other.

#### Scenario: process loss follows BackOff

- **WHEN** a tools process stops renewing or settling a delivery
- **THEN** server redelivery follows the configured 15-second first BackOff class
- **AND** it does not wait for the 300-second AckWait or use the 30-second semantic retry delay

### Requirement: local settlement does not imply server confirmation

Ack, Nak, delayed Nak, and Term returning nil SHALL report local method success only. Any method error SHALL remain
unknown/not-confirmed and SHALL NOT prove settlement or redelivery.

#### Scenario: method error after required effect

- **WHEN** the required effect may have completed and a terminal method errors
- **THEN** the semantic and method errors remain observable
- **AND** the framework makes no claim whether the server retained or settled the delivery

### Requirement: replay-unsafe bindings remain unchanged

A direct binding whose external effect cannot safely replay SHALL remain on characterized legacy behavior until its
named evidence gate is reviewed and accepted. JetStream redelivery alone SHALL NOT be claimed as effect idempotency.

#### Scenario: paid model effect is ambiguous

- **WHEN** no accepted provider-outcome or provider-idempotency design exists
- **THEN** the model binding remains non-authorizing and unchanged
- **AND** Stage A does not claim paid-execution restart safety
