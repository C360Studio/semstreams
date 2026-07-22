## ADDED Requirements

### Requirement: Body hydration failure is reported per node, never silent

The projection MUST report a requested-but-unloadable body as an empty body AND a
bounded reason naming why it is absent, never an empty body carrying no signal. This
applies whenever a node's verbatim body is requested (`WantBody`) but cannot be loaded.
A missing body is a partial result, not an absence: the node exists and ranks, so the
reason rides on the node itself. This is DISTINCT from `Unhydrated`, which reports seeds
that produced no node at all — a body-hydration failure concerns a node that is present.

The reason set is closed and mirrors the seed-hydration vocabulary: `not_found` when the
body reference resolves to no stored object (the object is absent — e.g. expired or
not-yet-written), and `error` for a genuine hydration fault (the body handle could not be
produced, or the stored-object read faulted for a reason other than absence). The reason
field is omitted entirely when the body hydrates, so a fully-hydrated response is
wire-unchanged. A failed body hydration MUST NOT cause the engine to defer or to
synthesize a `Miss`.

An entity that simply has no verbatim body is NOT a failure: it produces no body
reference, and its node MUST ship with an empty body, no reason, and no counter
increment. Only a body that was referenced-or-attempted but could not be loaded is
reported.

#### Scenario: a resolve failure reports a reason on the node

- **GIVEN** a request with `WantBody` and a node whose stored body read faults
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and a body reason of `error`

#### Scenario: a missing body object is distinguished from a fault

- **GIVEN** a request with `WantBody` and a node whose body reference does not resolve
  to a stored object
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and a body reason of `not_found`

#### Scenario: a missing body is a partial result, not a defer or a miss

- **GIVEN** a request with `WantBody` and one or more nodes whose bodies fail to load
- **WHEN** the response is assembled
- **THEN** the affected nodes are present with their body reasons set
- **AND** the engine does not defer and synthesizes no `Miss` for them

#### Scenario: a hydrated body carries no reason and is wire-unchanged

- **GIVEN** a request with `WantBody` and a node whose body loads successfully
- **WHEN** the node is projected
- **THEN** the node carries its body and the body reason field is omitted from the wire

#### Scenario: an entity with no verbatim body reports nothing

- **GIVEN** a request with `WantBody` and a node for an entity that has no verbatim body
  (its lens produces no body reference)
- **WHEN** the node is projected
- **THEN** the node is returned with an empty body and no body reason
- **AND** no body-hydration-failure counter is incremented

#### Scenario: body hydration failures are observable

- **GIVEN** one or more nodes whose bodies fail to load during a request
- **WHEN** the response is assembled
- **THEN** a body-hydration-failure counter is incremented, labelled by reason
