# application-logging Delta

## MODIFIED Requirements

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

- **GIVEN** the E2E binary, which boots through the same composition function as the production binary and therefore
  composes the production Phase-A
- **WHEN** the client emits a WARN record
- **THEN** configured local output receives the record once, with application-local formatting and base attributes
- **AND** `semstreams_log_entries_total{component="natsclient",level="warn"}` increments once
- **AND** no E2E-specific Phase-A constructor exists for a binary to select
