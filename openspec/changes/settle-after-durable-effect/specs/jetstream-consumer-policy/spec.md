# jetstream-consumer-policy Delta

## ADDED Requirements

### Requirement: settlement-only delivery decisions use one shared interpreter

`SettleDelivery` SHALL validate the existing closed decision/error tuple and attempt at most one local terminal
method. Ack with nil cause SHALL call Ack; Retry with non-nil cause SHALL call Nak under the operation's retry
policy, immediate for `SettleDelivery` and delayed by the caller's interval for `SettleDeliveryWithRetry`;
Terminate with non-nil cause SHALL call Term. Quarantine with non-nil cause, invalid tuples, and nil message SHALL attempt no terminal method
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

#### Scenario: an unset retry policy settles nothing

- **WHEN** `SettleDeliveryWithRetry` observes a retry policy that is not valid
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true, rather than silently settling as immediate

## MODIFIED Requirements

### Requirement: shared settlement remains stateless and heartbeat-specific

The typed path SHALL use only a private terminal-method executor. The no-heartbeat interpreter SHALL remain private:
the decision interpreter and the terminal-method executor SHALL NOT be exported. #759 SHALL add no exported pull
settlement operation. #1327 SHALL add exactly one exported settlement operation — settlement of one already-joined
outcome, with no work half — reachable through two entry points that differ only in the retry policy they apply:
`SettleDelivery` fixes it at immediate and `SettleDeliveryWithRetry` takes it from the caller. Neither entry point
SHALL invoke work, read payload or metadata, create a context, derive a deadline from consumer policy, send a
heartbeat, or own consumer lifecycle, and neither SHALL modify OTEL production settlement.

#### Scenario: terminal execution is shared privately

- **WHEN** typed heartbeat settlement attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared helper owns admission, a native handle, health, shutdown, or restart

#### Scenario: the exported settlement operation carries no work half

- **WHEN** a caller reaches either exported settlement entry point
- **THEN** that caller has already invoked and joined its own delivery work
- **AND** the entry point attempts at most one terminal method and returns
- **AND** the interpreter it delegates to remains unexported
