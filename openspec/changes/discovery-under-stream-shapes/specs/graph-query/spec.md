## ADDED Requirements

### Requirement: The subjects a component answers on MUST be reachable by a consumer

A component that registers request/reply handlers MUST expose the set of
subjects it answers on, as a value a composing consumer can read at build or
test time. Registration from an unexported literal is not reachable, and
configured input ports do not substitute for it: in a consumer deployment the
port configuration is supplied by the consumer, so it answers "what did I
configure", not "what does the framework claim".

Without this, a consumer composing framework components into its own process
cannot detect that one of its own components subscribes to a subject the
framework already answers. NATS request/reply with two subscribers is not load
balancing — both handlers receive the request and both publish to the reply
inbox, and the requester keeps whichever arrives first and discards the other.
When the two payload shapes differ, the subject has no usable contract and the
result is nondeterministic by construction.

The exported set MUST be derived from the same declaration the handlers are
registered from, so it cannot drift from what is actually served.

#### Scenario: a consumer checks its own subjects against the framework's

- **GIVEN** a consumer that registers its own request/reply handlers
- **WHEN** it compares its subjects against the exported set
- **THEN** a subject it shares with the framework is identified
- **AND** the comparison did not rely on a hand-maintained copy of the framework's surface

#### Scenario: a newly served subject appears in the exported set

- **GIVEN** a request/reply subject newly served by the component
- **WHEN** the exported set is read
- **THEN** it includes the new subject
- **AND** no separate declaration had to be updated for it to appear

#### Scenario: the exported set matches what is registered

- **GIVEN** the component's registered request/reply handlers
- **WHEN** the exported set is compared against them
- **THEN** the two agree exactly
- **AND** neither carries a subject the other omits
