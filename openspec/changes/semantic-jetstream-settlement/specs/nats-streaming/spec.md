# nats-streaming Delta

## ADDED Requirements

### Requirement: JetStream remains durable restart authority

Semantic settlement SHALL use existing JetStream consumer position and redelivery. Quarantine SHALL attempt no
terminal method. The existing component owner SHALL stop its exact lane and ordinary reconstruction SHALL reacquire
durable ownership. #759 SHALL add no recovery ledger or durable quarantine state.

`DeliveryAttempt` SHALL be an invocation-scoped projection of JetStream `NumDelivered`, not a checkpoint or replay
authority. Missing metadata SHALL add no replacement state; it SHALL fail closed and leave JetStream plus the exact
existing owner responsible for redelivery and reconstruction.

#### Scenario: quarantined delivery

- **WHEN** a delivery quarantines or loses heartbeat control
- **THEN** later admission closes and the exact existing owner stops the lane
- **AND** reconstructed ownership uses the existing durable consumer state

#### Scenario: redelivery is not execution proof

- **WHEN** JetStream reports a second delivery after the prior process stopped before invoking work
- **THEN** `DeliveryAttempt` reports redelivery
- **AND** no framework state claims that the prior work or effect occurred

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

### Requirement: replay follows the binding's durable authority

Every migrated binding SHALL define the durable consequence that permits positive settlement and the evidence checked
before repeating work on redelivery. JetStream delivery number and redelivery alone SHALL NOT be claimed as proof that
a prior invocation ran, that an external effect committed, or that replay is idempotent.

#### Scenario: external effect has an ambiguous prior outcome

- **WHEN** redelivery follows an external effect whose prior commit cannot be proved or disproved
- **THEN** the binding follows its accepted ambiguity decision rather than mechanically ACKing or retrying
- **AND** JetStream redelivery is not treated as provider-outcome authority

### Requirement: no staged compatibility surface becomes current truth

Temporary coexistence of typed and legacy settlement on a non-default integration branch SHALL NOT be archived as
current capability truth or merged to the default branch. Final current truth SHALL contain only the permanent typed
surface and its migrated production bindings.

JetStream remains the delivery and redelivery authority. This staging rule adds no supervisor, checkpoint, outbox,
receipt ledger, state-machine runtime, or new durable primitive.

#### Scenario: default branch receives one settlement surface

- **GIVEN** typed and legacy settlement coexist temporarily on the non-default integration branch
- **WHEN** the semantic-settlement change reaches the default branch
- **THEN** only the permanent typed settlement surface and migrated bindings are present
- **AND** no temporary compatibility state is archived as current truth

## REMOVED Requirements

### Requirement: Heartbeat consumption SHALL expose settlement failure

**Reason**: This requirement names the removed `ConsumeWithHeartbeat` export and its inferred nil/error settlement
contract. Final current truth has one typed semantic-settlement surface and preserves semantic, heartbeat-control, and
terminal-method evidence through `DeliveryResult`.

**Migration**: Define an owner-specific `DeliveryWork` decision matrix, validate `HeartbeatDeliveryPolicy` from the
exact acquisition configuration, call `ConsumeDeliveryWithHeartbeat`, inspect every `DeliveryResult`, and stop the
exact retained consumer owner outside the callback when `OwnerStopRequired` is true. ACK, Retry, Terminate, and
Quarantine replace inferred success/error handling; the removed helper has no alias.
