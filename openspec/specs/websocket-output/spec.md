# websocket-output Specification

## Purpose
TBD - created by archiving change websocket-output-passthrough. Update Purpose after archive.
## Requirements
### Requirement: Default broadcast injects subject and timestamp metadata

By default (pass-through disabled) the component MUST parse each inbound JSON
payload, inject a `subject` and a `timestamp` field when they are absent, and
re-encode before broadcasting. A payload that is not valid JSON MUST be wrapped in
a `raw_data` envelope (`type`, `subject`, `data`, `timestamp`) rather than dropped.
This is the backward-compatible default behavior.

#### Scenario: a JSON payload without metadata gets subject and timestamp injected

- **GIVEN** a websocket output with pass-through disabled (the default)
- **WHEN** it receives a valid JSON object carrying neither `subject` nor `timestamp`
- **THEN** the broadcast payload includes an injected `subject` (the NATS subject)
- **AND** an injected `timestamp`

#### Scenario: a non-JSON payload is wrapped as raw_data

- **GIVEN** a websocket output (pass-through in either state)
- **WHEN** it receives bytes that are not valid JSON
- **THEN** the broadcast payload is a `raw_data` envelope carrying the original bytes
      as a string

### Requirement: Pass-through mode broadcasts pre-validated JSON unchanged

The component MUST support an opt-in `passthrough` configuration flag (default
`false`). When enabled, a payload that is valid JSON (`json.Valid`) MUST be handed
to the broadcast path as its **original bytes** — not decoded and re-encoded — so
that JSON object **key order and numeric precision are preserved**, and neither
`subject` nor `timestamp` is injected. Enabling pass-through is the producer's
assertion that it emits an envelope-complete payload; the component does not add
envelope fields on this path. A payload that is not valid JSON MUST still fall back
to the `raw_data` wrapper, so pass-through is safe on a subject carrying mixed
content.

The preserved guarantee is **key order and numeric precision, not literal
byte-identity.** All broadcasts (pass-through and default alike) are wrapped in the
shared message envelope via `json.Marshal`, which compacts insignificant whitespace
and HTML-escapes `<`, `>`, `&`. Pass-through eliminates the two perturbations the
default path adds — map-driven key reordering and float re-formatting — but a
pretty-printed or `<`/`>`/`&`-bearing producer payload is still compacted/escaped by
the envelope marshal (the result remains semantically-equal JSON).

Pass-through MUST apply on every inbound handler path, so the behavior does not
depend on which NATS subscription entrypoint delivered the message.

`json.Valid` accepts any valid JSON value, including bare scalars and arrays
(`123`, `"x"`, `[1,2]`, `null`); pass-through broadcasts these unchanged, whereas
the default path — which requires a JSON object to inject into — wraps a non-object
as `raw_data`. This divergence is intentional: a valid JSON value is passed through,
a non-JSON payload is wrapped.

#### Scenario: valid JSON preserves key order and injects nothing

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives a valid, envelope-complete JSON object whose keys are not in
      sorted order
- **THEN** the broadcast payload has the producer's key order (not re-sorted)
- **AND** its numeric values are not re-formatted
- **AND** no `subject` or `timestamp` field is injected

#### Scenario: pass-through does not inject missing envelope fields

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives valid JSON that lacks `subject` and `timestamp`
- **THEN** neither `subject` nor `timestamp` is added to the broadcast payload

#### Scenario: pass-through still wraps non-JSON as raw_data

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives bytes that are not valid JSON
- **THEN** the broadcast payload is a `raw_data` envelope carrying the original bytes

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

