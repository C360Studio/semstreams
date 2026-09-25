## ADDED Requirements

### Requirement: Metrics listeners have explicit ownership

Metric Server SHALL admit a caller-bound raw TCP listener through `StartWithListener` without acquiring another
socket. Successful return SHALL transfer listener-release responsibility to Server's existing lifecycle. Every
returned error SHALL leave the supplied listener under caller responsibility.

Nil or ended context, nil or unusable listener, and used-instance rejection SHALL precede transfer. Nil or unusable
listener rejection SHALL precede consumption of the instance's one-shot opportunity. Registry and TLS-preparation
failures SHALL preserve the existing consumed-instance semantics without taking listener ownership. Configured TLS,
request context ancestry, one-shot admission, and terminal Stop behavior SHALL match `Start`.

While Server retains its accepted listener, `Address` SHALL report that listener's actual assigned endpoint, using
localhost for an unspecified bind host and preserving configured TLS scheme and path. Before successful acquisition
and after terminal cleanup releases ownership, `Address` SHALL report its configured localhost endpoint. A rejected
second Start SHALL preserve any already-owned listener and its observed address. `Address` SHALL NOT imply readiness.
Existing port-zero configuration meanings SHALL remain unchanged.

#### Scenario: Native startup owns its acquired listener

- **GIVEN** a fresh Server with a valid registry and an available bind address
- **WHEN** `Start` succeeds
- **THEN** the Server SHALL own the listener acquired by that Start
- **AND** real requests through `Address` SHALL reach that Server
- **AND** Stop SHALL close the listener and join its serving operation

#### Scenario: Caller-bound listener transfers on successful startup

- **GIVEN** a fresh Server and a caller-owned raw TCP listener on an assigned port
- **WHEN** `StartWithListener` succeeds
- **THEN** the Server SHALL serve the same listener without releasing and reacquiring its port
- **AND** requests SHALL inherit the supplied startup context
- **AND** Stop SHALL close that listener and join its serving operation

#### Scenario: Address observes a listener whose port differs from configuration

- **GIVEN** a Server configured for one port and a caller-owned listener bound to another port
- **WHEN** `StartWithListener` succeeds
- **THEN** `Address` SHALL identify the accepted listener's assigned endpoint and configured path
- **AND** real requests through `Address` SHALL reach that Server
- **AND** before acquisition and after terminal Stop its address SHALL use the configured localhost fallback

#### Scenario: Configured TLS applies to the supplied raw listener

- **GIVEN** a fresh Server with valid TLS configuration and a caller-owned raw TCP listener
- **WHEN** `StartWithListener` succeeds
- **THEN** Server SHALL apply TLS once to the supplied listener
- **AND** `Address` SHALL use HTTPS and the accepted endpoint
- **AND** real HTTPS requests SHALL reach the metrics handler

#### Scenario: Admission rejection preserves caller ownership

- **GIVEN** a nil or ended context, a nil or unusable listener, or an already-used Server
- **WHEN** listener startup is attempted
- **THEN** the attempt SHALL return an error without transferring or closing a supplied listener
- **AND** nil or unusable listener rejection SHALL not consume a fresh Server instance
- **AND** a rejected second Start SHALL preserve any already-owned listener and its observed address

#### Scenario: Preparation failure preserves caller ownership and consumes the instance

- **GIVEN** a fresh Server with an invalid registry or invalid TLS configuration and a caller-owned raw TCP listener
- **WHEN** `StartWithListener` attempts startup
- **THEN** the preparation failure SHALL return synchronously without transferring or closing the listener
- **AND** the instance's one-shot opportunity SHALL be consumed, matching native `Start`

#### Scenario: Terminal Stop does not admit another startup

- **GIVEN** a Server whose listener startup succeeded and whose Stop completed
- **WHEN** Stop is repeated or startup is attempted again
- **THEN** repeated Stop SHALL preserve the existing terminal-repeat behavior
- **AND** startup SHALL be refused without taking ownership of another supplied listener
