# application-logging Delta

## ADDED Requirements

### Requirement: Client observability dependencies exist before connection

Each primary binary SHALL create its metrics registry and configured local logging handler before client construction.
It SHALL pass a client logger and the registry through the existing `WithLogger` and `WithMetrics` options before
`Connect`.

The client logger SHALL reuse the same configured local handler and common base attributes as the process logger and
SHALL add `component=natsclient`. Shared composition SHALL NOT silently create or default to an independent logger or
handler instance.

#### Scenario: production client warning uses configured local output once

- **GIVEN** the production Phase-A composition
- **WHEN** the client emits a WARN record
- **THEN** configured local output receives the record once
- **AND** `semstreams_log_entries_total{component="natsclient",level="warn"}` increments once

#### Scenario: E2E client uses the same configured local output

- **GIVEN** the E2E Phase-A composition
- **WHEN** the client emits a record at an enabled level
- **THEN** its configured local output matches application-local formatting and base attributes
- **AND** no counter handler or NATS log handler receives the record

### Requirement: Client and configuration logging cannot forward through their own client

The client and config-manager logger graphs SHALL NOT contain a NATS log handler backed by that client. This invariant
SHALL hold structurally for every publish-failure path without relying on source exclusions or runtime failure checks.

#### Scenario: forwarded publish failure cannot recurse

- **GIVEN** production NATS forwarding is enabled
- **WHEN** a forwarded application record fails on a path that reaches client failure accounting
- **THEN** any resulting client diagnostic is emitted only to its non-forwarding local/counter graph
- **AND** it does not create another forwarded record

### Requirement: Boot failures remain locally visible

Configured local output SHALL be installed before client construction. Client construction, connection, configuration
arbitration, validation, limit verification, and stream-provisioning failures SHALL remain visible locally and through
the returned boot error.

#### Scenario: connection fails before forwarding exists

- **GIVEN** the configured broker is unavailable
- **WHEN** a primary binary attempts to connect
- **THEN** the failure is visible through configured local output and the returned boot error
- **AND** no NATS forwarding handler is required for visibility
