# jetstream-consumer-policy Delta

> Written against the spec as it stands AFTER `settle-after-durable-effect` (L1) archives: the second and third
> MODIFIED blocks restate L1's modified and added text and change exactly one `**AND**` clause each. L1 has since
> archived, so the caveat this note carried during the design phase is discharged: `openspec validate
> delivery-lane-admission-package --strict` is green at the implementation base `3faca84f` and on every commit of
> this change. No import path appears in any SHALL; the package path lives in `design.md` § 5 and the package doc
> comment.

## MODIFIED Requirements

### Requirement: control loss shuts down through the existing exact owner

Each migrated physical binding SHALL create private admission before acquisition and buffer its first
OwnerStopRequired result. Closed admission SHALL perform no work, heartbeat, or terminal method. Already-admitted work
MAY finish. The callback SHALL NOT drain or wait on its handle; the existing owner SHALL stop the exact committed
handle outside callback and join the observer during Stop.

Refusal by closed admission is a declared event, not a silent drop. Each refused delivery SHALL emit a substrate log
line and increment a lane-labelled counter naming the refusal and why continuing is safe (ADR-098). The buffered
deliveries a drained handle flushes reach this path, so the first fatal result alone SHALL NOT be the only signal.

Within this module the owner-side reaction has exactly one home: one shared package that provides the per-lane
admission latch, the drain-once binding around the exact committed handle, and the observer that runs the owner's
reaction and drains that handle on the first buffered fatal. That package SHALL own no lifecycle authority — no
Stop, no restart, no reconstruction, no registry of lanes: the owner constructs the admission before acquisition,
constructs and retains the binding after it, decides Stop, awaits the exact handle's Closed, and joins the observer
through the binding. The owner's health writer SHALL run synchronously inside the latch before the result is
buffered. A closed lane SHALL read nothing from a refused delivery except its subject, and that only to declare the
refusal. No migrated binding SHALL declare its own admission latch.

#### Scenario: control loss precedes handle return

- **WHEN** a callback reports OwnerStopRequired before acquisition returns
- **THEN** the result remains buffered until the exact handle is committed
- **AND** owner-side shutdown occurs outside callback

#### Scenario: closed admission refuses a buffered delivery

- **WHEN** a delivery reaches a binding whose admission is already closed
- **THEN** the binding performs no work, heartbeat, or terminal method
- **AND** it emits a log line and increments its lane-labelled refusal counter

#### Scenario: the owner-side reaction has one home

- **WHEN** a durable-consumer owner in this module reacts to OwnerStopRequired
- **THEN** it consumes each delivery through the shared lane package under an admission it constructed before
  acquisition, and retains the shared binding it constructed after acquisition
- **AND** the observer runs on the owner's Start-derived context, runs the owner's reaction, drains the exact handle
  once, and is joined by the owner's Stop through the binding, which is joinable whether or not an observer ran
- **AND** the shared package holds no registry of bindings, stops nothing on its own authority, and exposes no
  status, metric, or durable state

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
- **AND** no shared settlement helper owns admission, a native handle, health, shutdown, or restart; the owner-side
  reaction lives in the one shared lane package, which owns no lifecycle authority

#### Scenario: the exported settlement operation carries no work half

- **WHEN** a caller reaches either exported settlement entry point
- **THEN** that caller has already invoked and joined its own delivery work
- **AND** the entry point attempts at most one terminal method and returns
- **AND** the interpreter it delegates to remains unexported

### Requirement: settlement-only delivery decisions use one shared interpreter

`SettleDelivery` SHALL validate the existing closed decision/error tuple and attempt at most one local terminal
method. Ack with nil cause SHALL call Ack; Retry with non-nil cause SHALL call Nak under the operation's retry
policy, immediate for `SettleDelivery` and delayed by the caller's interval for `SettleDeliveryWithRetry`;
Terminate with non-nil cause SHALL call Term. Quarantine with non-nil cause, invalid tuples, and nil message
SHALL attempt no terminal method and SHALL return quarantined owner-stop evidence. Terminal-method errors SHALL
remain local, unconfirmed evidence.

The function SHALL NOT invoke work, read payload or metadata, create context, derive a deadline from consumer policy,
send heartbeat, or own consumer lifecycle. Its caller SHALL invoke and join delivery work before settlement and SHALL
react to `OwnerStopRequired` through the existing exact owner.

#### Scenario: terminal execution is shared without work ownership

- **WHEN** typed heartbeat or settlement-only handling attempts a terminal JetStream method
- **THEN** it calls the private terminal-method executor
- **AND** no shared settlement helper owns admission, a native handle, health, shutdown, or restart; the owner-side
  reaction lives in the one shared lane package, which owns no lifecycle authority

#### Scenario: an invalid tuple or absent message settles nothing

- **WHEN** `SettleDelivery` observes Quarantine, a decision/cause combination outside the closed set, or a nil message
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true

#### Scenario: an unset retry policy settles nothing

- **WHEN** `SettleDeliveryWithRetry` observes a retry policy that is not valid
- **THEN** it attempts no terminal method
- **AND** it returns a quarantined result whose `OwnerStopRequired` is true, rather than silently settling as immediate
