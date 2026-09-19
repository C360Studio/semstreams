# nats-streaming Delta

## ADDED Requirements

### Requirement: JetStream remains durable restart authority

Semantic settlement SHALL use existing JetStream consumer position and redelivery. Quarantine SHALL attempt no
terminal method. The existing component owner SHALL stop its exact lane and ordinary reconstruction SHALL reacquire
durable ownership. #759 SHALL add no recovery ledger or durable quarantine state.

JetStream `NumDelivered` SHALL remain the server's own count and SHALL NOT become a framework-exposed checkpoint or
replay authority. Missing metadata SHALL add no replacement state; it SHALL fail closed and leave JetStream plus the
exact existing owner responsible for redelivery and reconstruction.

#### Scenario: quarantined delivery

- **WHEN** a delivery quarantines or loses heartbeat control
- **THEN** later admission closes and the exact existing owner stops the lane
- **AND** reconstructed ownership uses the existing durable consumer state

#### Scenario: redelivery is not execution proof

- **WHEN** JetStream reports a second delivery after the prior process stopped before invoking work
- **THEN** the delivery is admitted on its own metadata
- **AND** no framework state or work-visible value claims that the prior work or effect occurred

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

### Requirement: the legacy helper is a shrinking remainder, never a compatibility surface

While `ConsumeWithHeartbeat` still has production callers, its coexistence with the typed surface SHALL NOT be
described as a compatibility period. It SHALL remain unadvertised, SHALL admit no new production caller — enforced
by an AST ratchet over the exact remaining set, which only shrinks — and SHALL carry no deprecation window, alias,
or shim. It is deleted by the PR that migrates its last caller. Adopters get the removal from
`docs/operations/migration-beta162-to-beta163.md`, not from a deprecation marker. (Owner ruling 2026-09-18.)

JetStream remains the delivery and redelivery authority. This rule adds no supervisor, checkpoint, outbox, receipt
ledger, state-machine runtime, or new durable primitive.

#### Scenario: a new caller is refused while the remainder shrinks

- **GIVEN** the typed surface and the unremoved legacy helper are both present
- **WHEN** any production file adds a call to the legacy helper
- **THEN** the AST ratchet fails
- **AND** the recorded caller set is never widened, only reduced by the migrating PRs

## MODIFIED Requirements

### Requirement: Heartbeat consumption SHALL expose settlement failure

`ConsumeWithHeartbeat` SHALL return ACK, delayed NAK, and Term settlement errors to its caller while preserving the
existing heartbeat and shutdown delays. It SHALL not discard a settlement error after work has returned.

This contract SHALL bind only the helper's remaining ratcheted callers. It is not the framework's settlement
contract: a migrated binding defines an owner-specific `DeliveryWork` decision matrix, validates
`HeartbeatDeliveryPolicy` from the exact acquisition configuration, calls `ConsumeDeliveryWithHeartbeat`, inspects
every `DeliveryResult`, and stops the exact retained consumer owner outside the callback when `OwnerStopRequired` is
true. ACK, Retry, Terminate, and Quarantine replace inferred success/error handling there.

This requirement is deleted together with the helper by the PR that migrates its last caller (#1249).

#### Scenario: transient work fails and delayed NAK fails

- **WHEN** work returns a transient error
- **AND** `NakWithDelay` also fails
- **THEN** the returned error chain contains both failures

#### Scenario: shutdown NAK fails

- **WHEN** context cancellation owns the delivery outcome
- **AND** the five-second delayed NAK fails
- **THEN** the returned error chain contains context cancellation and the settlement failure
