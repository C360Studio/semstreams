## ADDED Requirements

### Requirement: GraphQL errors preserve existing classified authority

When a graph-gateway query failure resolves through `errors.As` to `*errs.ClassifiedError`, graph-gateway SHALL return
the clean `err.Error()` message and copy the existing error class and non-empty code into that GraphQL error object's
`extensions.class` and `extensions.code`. The gateway SHALL NOT classify errors, parse message text, infer from HTTP
status, subject, or field, create a new code, or expose `ClassifiedError.Detail` in this change.

A plain error SHALL remain message-only. A classified error with an empty code SHALL expose class but no code. Existing
HTTP behavior SHALL remain unchanged: gateway-local invalid input keeps its current 400-class status, handler-side
classified failures keep GraphQL HTTP 200, and transport timeout/unavailability keeps its current gateway status.

#### Scenario: invalid input preserves its existing code

- **GIVEN** prefix validation returns class `invalid` and code `entity_id_prefix_invalid`
- **WHEN** graph-gateway returns the error
- **THEN** `extensions.class` is `invalid` and `extensions.code` is `entity_id_prefix_invalid`
- **AND** the existing gateway-local HTTP status is unchanged

#### Scenario: index-not-ready remains machine readable

- **GIVEN** `RequestClassified` returns class `transient` and code `index_not_ready`
- **WHEN** graph-gateway returns the handler error
- **THEN** HTTP status remains 200
- **AND** `extensions.class` is `transient` and `extensions.code` is `index_not_ready`

#### Scenario: the gateway invents no authority

- **GIVEN** a plain error, or a classified error with no code
- **WHEN** graph-gateway projects it
- **THEN** a plain error has no class or code extensions
- **AND** an uncoded classified error exposes class only
- **AND** neither response exposes classified detail
