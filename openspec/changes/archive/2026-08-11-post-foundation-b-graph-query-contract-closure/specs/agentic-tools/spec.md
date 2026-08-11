## ADDED Requirements

### Requirement: Framework-owned shared builtins exclude the unowned graph-query wrappers

The framework SHALL NOT supply shared builtin tools named `search_graph` or
`summarize_graph`. Their framework-owned shared registrations,
`BuiltinGroupKeys`, accepted `SkipBuiltins` keys, registration functions,
implementations, exported executor/option/constructor/querier symbols, tests,
schemas, documentation, discovery defaults, operation-consumer claims, and
alternate framework category entries `graph_search`/`graph_summary` SHALL be
absent.

This requirement does not reserve either former name or prohibit an application
from registering its own component-local executor under that name through the
existing general extension seam. An application-local executor SHALL remain
subject to the existing allowlist, per-loop advertised set, approval, retry,
local-over-shared discovery, and local-first dispatch behavior. SemStreams SHALL
add no shared alias, compatibility executor, reserved-name rule, dependency
inference, or special configuration behavior for such a local tool.

GraphQL `searchGraph` and `graphSummary`, their graph-query responders, research
consumers, exact reads, fusion, projection, classifier/search options, direct
`query_*` tools, and selected `research_graph` SHALL remain.

Open-vocabulary `allowed_tools`, `default_tools`, `approval_required`, and
`tool_retries` SHALL NOT become a closed framework-tool enum. Nil or empty
`AllowedTools` SHALL remain permissive for surviving or application-local
registered tools, but SHALL NOT create an absent executor. Stale deleted
`SkipBuiltins` values SHALL fail through existing closed-set validation.

#### Scenario: framework shared discovery excludes the deleted wrappers

- **WHEN** framework shared builtin registration and discovery run
- **THEN** neither former name has a framework-supplied definition or executor
- **AND** neither shared registration, skip key, exported implementation, or
  alternate category entry is present

#### Scenario: permissive allowlist does not create a deleted executor

- **GIVEN** nil or empty `AllowedTools`
- **AND** no application-local executor uses the former name
- **WHEN** shared discovery runs
- **THEN** the former name is absent
- **AND** an admitted direct call that is not intercepted for approval reaches
  the registries and returns the existing typed not-found outcome

#### Scenario: approval interception precedes registry miss

- **GIVEN** a former name remains in `approval_required`
- **AND** the wire call passes global and per-loop admission
- **AND** no executor is registered under that name
- **WHEN** the unapproved call is handled
- **THEN** ApprovalFilter produces the existing approval-required permission and
  pause behavior before registry dispatch
- **AND** a later approved or bypassed dispatch returns typed not-found if no
  local executor exists

#### Scenario: application-local reuse remains ordinary local extension

- **GIVEN** an application registers a local executor under a former name
- **WHEN** discovery and dispatch run
- **THEN** the local definition is discovered through existing local precedence
- **AND** existing admission, approval, retry, and dispatch rules apply
- **AND** no shared alias, reservation, or compatibility executor participates

#### Scenario: stale skip configuration fails visibly

- **GIVEN** `SkipBuiltins` contains either deleted key
- **WHEN** builtin configuration is validated
- **THEN** existing closed-set validation rejects it
- **AND** the framework does not silently accept a compatibility no-op
