# jetstream-consumer-policy Delta

## ADDED Requirements

### Requirement: settlement-only delivery decisions use one shared interpreter

`SettleDelivery` SHALL validate the existing closed decision/error tuple and attempt at most one local terminal
method. Ack with nil cause SHALL call Ack; Retry with non-nil cause SHALL call immediate Nak; Terminate with non-nil
cause SHALL call Term. Quarantine with non-nil cause, invalid tuples, and nil message SHALL attempt no terminal method
and SHALL return quarantined owner-stop evidence. Terminal-method errors SHALL remain local, unconfirmed evidence.

The function SHALL NOT invoke work, read payload or metadata, create context, derive a deadline from consumer policy,
send heartbeat, or own consumer lifecycle. Its caller SHALL invoke and join delivery work before settlement and SHALL
react to `OwnerStopRequired` through the existing exact owner.

#### Scenario: terminal execution is shared without work ownership

- **WHEN** typed heartbeat or settlement-only handling attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared helper owns admission, a native handle, health, shutdown, or restart

#### Scenario: an invalid tuple or absent message settles nothing

- **WHEN** `SettleDelivery` observes Quarantine, a decision/cause combination outside the closed set, or a nil message
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true
