# WebSocket Output

The `output/websocket` component broadcasts inbound NATS messages to connected
WebSocket clients. This spec covers how inbound payloads are transformed before
broadcast.

> Seeded lazily by the gh#471 pass-through change. Requirements distilled from
> `output/websocket/websocket.go` (`handleNATSMessageData`, `handleNATSMessage`) and
> verified against code. Scope is the **inbound-payload → broadcast** transform;
> connection lifecycle, delivery modes, and TLS are separate concerns, seeded when
> a change first touches them.

## ADDED Requirements

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
`false`). When enabled, a payload that is valid JSON (`json.Valid`) MUST be
broadcast as its **original bytes**, unchanged: no decode/re-encode, so JSON object
key order and numeric precision are preserved, and neither `subject` nor
`timestamp` is injected. Enabling pass-through is the producer's assertion that it
emits an envelope-complete payload; the component does not add envelope fields on
this path. A payload that is not valid JSON MUST still fall back to the `raw_data`
wrapper, so pass-through is safe on a subject carrying mixed content.

Pass-through MUST apply on every inbound handler path, so the behavior does not
depend on which NATS subscription entrypoint delivered the message.

#### Scenario: valid JSON is broadcast byte-for-byte

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives a valid, envelope-complete JSON payload
- **THEN** the broadcast bytes are identical to the received bytes
- **AND** no `subject` or `timestamp` field is injected

#### Scenario: pass-through does not inject missing envelope fields

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives valid JSON that lacks `subject` and `timestamp`
- **THEN** the broadcast bytes are still identical to the received bytes (not injected)

#### Scenario: pass-through still wraps non-JSON as raw_data

- **GIVEN** a websocket output with `passthrough: true`
- **WHEN** it receives bytes that are not valid JSON
- **THEN** the broadcast payload is a `raw_data` envelope carrying the original bytes
