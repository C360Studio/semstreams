# gateway-response-projection delta — envelope detection

## ADDED Requirements

### Requirement: The gateway MUST decide envelope unwrapping from the response, never from the subject
The GraphQL gateway MUST determine whether a NATS query response carries the `graph.QueryResponse`
envelope by examining the RESPONSE, and MUST NOT decide it by matching the subject against a prefix or
an enumerated subject list.

Subject enumeration is not merely fragile here, it is unsound. Query handlers proxy: the graph-query
component's semantic and spatial handlers forward to downstream subjects and return the downstream
response verbatim, so an envelope produced under `graph.index.query.*` can surface under a
`graph.query.*` subject. Whether a given response carries the envelope is therefore a property of the
response and not of the subject that produced it, and any subject-keyed rule is wrong for some
response no matter how the list is maintained.

The consequence of the enumerated form is that a new query family inherits the WRONG shape by
default, silently, and is discovered by a consumer rather than by a test.

#### Scenario: A newly added query family is projected correctly with no gateway change

- **GIVEN** a new query subject whose handler returns a `QueryResponse` envelope
- **AND** the gateway has not been edited to name that subject or its prefix
- **WHEN** a caller reads the corresponding GraphQL field
- **THEN** the payload is projected unwrapped, with the envelope's fields at the top level
- **AND** no `data.<field>.data.*` nesting is present

#### Scenario: A proxied response is projected by its own shape

- **GIVEN** a handler that forwards to a downstream subject and returns that response verbatim
- **WHEN** the downstream response carries the envelope while the outer subject is in a different
  family
- **THEN** the envelope is unwrapped on the basis of the response
- **AND** the outer subject's name does not affect the decision

### Requirement: Envelope detection MUST be conservative, admitting only the exact envelope shape
A response MUST be treated as the envelope only when its top-level JSON object carries both `data` and
`timestamp` and every one of its keys is drawn from the envelope's own field set
(`data`, `request_id`, `timestamp`); any other response MUST be passed through byte-for-byte
unchanged.

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

### Requirement: The envelope MUST be removed exactly once, never iteratively
The gateway MUST unwrap at most one envelope layer per response, and MUST NOT re-test the unwrapped
payload in order to unwrap again.

One envelope is applied by the producer, so exactly one is removed at the projection boundary.
Unwrapping while the payload continues to look like an envelope would make the number of layers
removed depend on user data: a payload whose own contents happened to match the discriminator would be
silently flattened, and the same query would project differently for different entities.

#### Scenario: Envelope-shaped payload data survives projection

- **GIVEN** a `QueryResponse` whose `data` is itself an object carrying `data` and `timestamp`
- **WHEN** the gateway projects the response
- **THEN** exactly one layer is removed
- **AND** the inner object is delivered to the caller intact

### Requirement: The projected shape MUST be asserted against raw wire bytes, not a decoded struct
The projected response shape MUST be verified by assertions over the raw JSON keys of the served
payload, and MUST NOT be verified solely by decoding into a struct.

The wrapped and unwrapped shapes both unmarshal cleanly into any permissive target, so a struct-level
assertion passes under either one. A test that decodes before asserting cannot distinguish the defect
from the fix, and reports green in both states.

Verification MUST cover representative subjects from EVERY query family routed through the gateway's
response path, not only the family in which the defect was first observed, and MUST assert the
ABSENCE of a repeated `data` hop rather than only the presence of expected fields.

#### Scenario: The shape gate fails against the unfixed gateway

- **GIVEN** the response-shape assertions and a gateway that does not detect the envelope
- **WHEN** the gate runs
- **THEN** it fails
- **AND** its falsifiability is therefore demonstrated rather than assumed

#### Scenario: A repeated data hop is caught rather than read through

- **GIVEN** a projected response carrying `data.<field>.data.*`
- **WHEN** the shape assertions run
- **THEN** the repeated hop is reported as a failure
- **AND** the assertion does not pass merely because the expected leaf values are reachable
