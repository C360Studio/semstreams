# websocket-output Delta

## ADDED Requirements

### Requirement: WebSocket output owns one configurable path-only route pattern

WebSocket output SHALL expose a component-root `path` configuration field and SHALL default an omitted field to
`/ws`. The field SHALL remain separate from `NetworkPort`; changing it SHALL NOT change listener facts, resource
identity, or exclusivity.

Every configured value SHALL be validated before live mux registration. It MUST be nonempty, begin with `/`, contain
no ASCII whitespace or control character, and be a valid path-only pattern for the running Go version's
`http.ServeMux`. Root, trailing-slash, percent-escaped, and valid wildcard patterns SHALL remain supported. Method or
host patterns, full URLs, and syntactically invalid patterns SHALL fail component construction with invalid-config
context rather than panic during startup.

The retired root `endpoint` spelling SHALL fail construction and SHALL NOT act as an alias. URL-in-port subjects,
root `url` or `websocket_path`, and nested network `path` SHALL NOT be accepted as compatibility routes.

#### Scenario: omission preserves the default route

- **GIVEN** WebSocket output JSON omits `path`
- **WHEN** the production factory constructs the component
- **THEN** its effective route is `/ws`

#### Scenario: raw JSON selects a custom upgrade route

- **GIVEN** production factory JSON declares `path: /graph`
- **WHEN** the production HTTP handler serves requests
- **THEN** a WebSocket upgrade to `/graph` succeeds
- **AND** `/ws` does not upgrade

#### Scenario: valid path-pattern affordances are preserved

- **GIVEN** JSON or direct Go construction supplies `/`, a trailing-slash pattern, a percent-escaped segment, or a
  valid ServeMux wildcard path
- **WHEN** the component is constructed
- **THEN** the path is accepted

#### Scenario: invalid or non-path patterns fail before startup

- **GIVEN** an empty path, missing-leading-slash value, method/host pattern, full URL, whitespace/control character,
  or syntactically invalid ServeMux pattern
- **WHEN** JSON or direct Go construction is attempted
- **THEN** construction returns invalid-config context
- **AND** no live mux registration or listener allocation occurs

#### Scenario: retired endpoint spelling is not a compatibility path

- **GIVEN** WebSocket output JSON declares root `endpoint`
- **WHEN** the production factory parses it
- **THEN** construction fails with field context
- **AND** no route is inferred from the value

#### Scenario: route selection does not redefine listener identity

- **GIVEN** two otherwise identical output configurations with different valid paths
- **WHEN** their network port facts and resource IDs are projected
- **THEN** those listener identities remain equal

#### Scenario: core E2E proves the configured route

- **GIVEN** the shipped protocol-flow fixture selects a non-default WebSocket output path
- **WHEN** the core-dataflow E2E scenario runs
- **THEN** it receives a successful WebSocket upgrade at that exact path
