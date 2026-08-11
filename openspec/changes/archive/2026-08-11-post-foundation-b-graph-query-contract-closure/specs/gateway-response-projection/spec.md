## MODIFIED Requirements

### Requirement: Envelope detection MUST be conservative, admitting only the exact envelope shape

A response MUST be treated as the envelope only when its top-level JSON object carries both `data` and
`timestamp` and every one of its keys is drawn from the envelope's own field set
(`data`, `timestamp`); any other response MUST be passed through byte-for-byte unchanged.

Detecting on the presence of `data` alone is prohibited. A payload that legitimately carries a
top-level `data` field would be stripped of a nesting level, which converts this capability's
cosmetic defect into silent data loss — a strictly worse failure than the one being fixed. `timestamp`
carries no `omitempty` tag and is therefore always present on a real envelope, so the conjunction
costs nothing to require. Requiring the key set to be CLOSED additionally means a payload bearing
`data` and `timestamp` alongside other fields is not an envelope and is left alone.

A response that fails detection MUST NOT be reported as an error on that basis; failing to be an
envelope is the ordinary case for the families that do not use one.

#### Scenario: A payload with a legitimate top-level data field is not unwrapped

- **GIVEN** a response whose own payload carries a top-level `data` field and no `timestamp`
- **WHEN** the gateway projects it
- **THEN** the payload is passed through with its nesting intact
- **AND** no field is removed from the caller's view

#### Scenario: A collection response with its own envelope is untouched

- **GIVEN** a response shaped `{entities, next_cursor}`, which is a distinct type and not the query
  envelope
- **WHEN** the gateway projects it
- **THEN** envelope detection does not claim it
- **AND** its own established handling applies unchanged

#### Scenario: An envelope-shaped object bearing extra keys is not an envelope

- **GIVEN** a response carrying `data`, `timestamp`, and at least one key outside the envelope's field
  set
- **WHEN** the gateway projects it
- **THEN** it is passed through unchanged

### Requirement: The envelope's key set MUST be reserved against collision by other response types

A response type served through the gateway's projection path MUST NOT consist solely of keys drawn
from the envelope field set (`data`, `timestamp`) unless it IS the envelope. The envelope's shape is
reserved, and a new or modified response type that would occupy it MUST be given a distinguishing
field or a different field name instead.

Detection on a closed key set is exact for every response type that exists when it is written, and
carries exactly one residual risk forward: a response type introduced LATER that happens to consist
only of envelope keys would be detected as an envelope and unwrapped, silently removing a nesting
level from a payload nobody intended to wrap. Detection cannot defend against this on its own, because
by construction such a response is indistinguishable from the envelope on the wire.

Stating the reservation converts that residual from a latent runtime defect into a **reviewable
contract violation** — visible when the offending type is written, in review, rather than when a
consumer loses a field in production. This is also what earns this capability its own home: an
unowned contract is one nobody checks, and that observation applies forward to types not yet written,
not only backward to the defect that prompted the capability.

#### Scenario: A new response type may not occupy the envelope's shape

- **GIVEN** a proposed gateway response type whose marshalled form consists only of keys drawn from
  `{data, timestamp}`
- **AND** that type is not the query-response envelope
- **WHEN** it is reviewed
- **THEN** it is rejected as a contract violation
- **AND** the remedy is a distinguishing field or a different field name, not a detection exception

#### Scenario: A reserved-shape collision is caught before it ships

- **GIVEN** a change introducing such a type
- **WHEN** the projection path's response types are checked against the reservation
- **THEN** the collision is reported against the new type
- **AND** it is not left to surface as a missing field in a consumer
