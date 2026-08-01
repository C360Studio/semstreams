## ADDED Requirements

### Requirement: A request/reply subject captured by a stream MUST NOT fail silently

A deployment whose stream subject filters cover a subject the framework serves
request/reply on MUST surface that collision at provisioning time, naming the
capturing stream and the captured subject. It MUST NOT be discoverable only by
observing that a response is empty.

The failure this closes is not a missing signal but a **misleading positive**
one: the core-NATS subscription succeeds, so health, logs and metrics all report
normal while the responder is unreachable. A successful subscribe is evidence
that the process registered a handler, never that a caller can reach it — the
framework's standing rule that a positive signal must not be read as a
precondition it does not establish.

The guard MUST be derived from the declared request/reply subjects and the
provisioned stream filters, not from a hand-maintained list of known-bad pairs,
so a stream shape or a request/reply subject added later is covered without
either side being updated.

#### Scenario: a stream filter covers a declared discovery subject

- **GIVEN** a declared request/reply subject and a stream whose subject filters cover it
- **WHEN** streams are provisioned
- **THEN** the collision is reported, naming the capturing stream and the captured subject
- **AND** the deployment does not reach a state where discovery returns an empty result without warning

#### Scenario: a stream that covers no request/reply subject

- **GIVEN** a stream whose subject filters cover no declared request/reply subject
- **WHEN** streams are provisioned
- **THEN** provisioning proceeds without a collision report

#### Scenario: a request/reply subject added after the stream

- **GIVEN** an existing stream whose filters would cover a newly declared request/reply subject
- **WHEN** streams are provisioned
- **THEN** the collision is reported
- **AND** detection did not require the stream declaration to be updated

### Requirement: A publish ack MUST NOT decode as a query reply

The canonical query-reply decoder MUST refuse a JetStream publish acknowledgement
rather than decoding it as a reply body. A publish ack is never a valid reply on
the query plane, and decoding one yields a structurally valid but empty result —
a zero-item catalog indistinguishable from "nothing is registered".

This is deliberately redundant with the provisioning guard above. The guard
protects deployments the framework provisions; the decoder protects every caller
regardless of who provisioned the streams, and converts a silent empty answer
into a typed error naming what was actually received.

An empty result and a captured request are different facts, and a consumer that
cannot distinguish them will report the wrong one.

#### Scenario: a publish ack arrives where a reply was expected

- **GIVEN** a reply body that is a JetStream publish acknowledgement
- **WHEN** it is decoded by the canonical reply decoder
- **THEN** decoding fails with an error identifying the body as a publish ack
- **AND** it does not yield an empty result

#### Scenario: a genuine empty result is still an empty result

- **GIVEN** a valid reply carrying no items
- **WHEN** it is decoded
- **THEN** it decodes successfully as an empty result
- **AND** it is not reported as a publish ack

#### Scenario: discovery served over an uncaptured subject

- **GIVEN** a discovery subject no stream covers
- **WHEN** a discovery request is served
- **THEN** the response is the tool catalog
- **AND** it carries the registered tools rather than an acknowledgement
