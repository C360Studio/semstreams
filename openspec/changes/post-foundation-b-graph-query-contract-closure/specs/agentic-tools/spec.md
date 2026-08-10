## ADDED Requirements

### Requirement: The admitted builtin set excludes the unowned graph-query wrappers

The framework SHALL NOT register, advertise, execute, configure, document, or export the agentic tools
`search_graph` or `summarize_graph`. Their shared registrations, component-local registrations, `BuiltinGroupKeys`,
accepted `SkipBuiltins` keys, registration functions, implementations, exported executor/option/constructor/querier
symbols, tests, schemas, documentation, and discovery/default-tool expectations SHALL be absent.

GraphQL `searchGraph` and `graphSummary`, their graph-query responders, and research consumers SHALL remain. The
general component-local extension seam and dispatch precedence SHALL remain unchanged for unrelated tool names. No
replacement tool, reserved name, definition-only executor, dependency-port metadata, discovery redesign, no-op skip
value, alias, or compatibility wrapper SHALL be added.

#### Scenario: deleted wrappers cannot drift from query ports

- **WHEN** shared and component-local tool discovery, builtin keys, executor registrations, and exported symbols are
  inspected
- **THEN** neither deleted name or implementation is present
- **AND** agentic-tools claims no `graph.query.searchGraph` or `graph.query.summary` output

#### Scenario: stale skip configuration fails visibly

- **GIVEN** `SkipBuiltins` contains either deleted key
- **WHEN** builtin configuration is validated
- **THEN** existing closed-set validation rejects it
- **AND** the framework does not silently accept a compatibility no-op

#### Scenario: unrelated local tools keep their existing seam

- **GIVEN** an application registers a non-reserved local executor name
- **WHEN** discovery and dispatch run after the wrapper deletion
- **THEN** registration, discovery, and local-over-shared dispatch precedence remain unchanged
- **AND** no new dependency inference or port mechanism is required
