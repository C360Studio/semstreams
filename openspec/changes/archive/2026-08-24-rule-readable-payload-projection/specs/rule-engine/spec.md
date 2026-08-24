## ADDED Requirements

### Requirement: Rule evaluation MUST read any payload that declares its rule-readable fields

The rule engine SHALL obtain `$message.*` condition and substitution data from any payload
implementing the `message.RuleReadable` behavior, and SHALL NOT require a payload to be a
particular concrete type in order to be rule-readable. A payload implementing `RuleReadable`
supplies its own projection; `message.GenericJSONPayload` remains readable through its data map so
every rule valid before this requirement stays valid after it.

Projection SHALL have exactly one implementation in the rule engine. A payload's rule-readable
fields SHALL be whatever that payload declares and nothing more — the engine SHALL NOT derive
fields reflectively from a payload's structure, so a field reaches a rule only because its author
exposed it.

#### Scenario: a typed payload declaring rule-readable fields is matchable

- **GIVEN** a registered payload type that implements `message.RuleReadable`
- **WHEN** a message-path rule evaluates a condition on one of its declared fields
- **THEN** the condition resolves against the declared value
- **AND** the same value is available to `$message.*` substitution in the rule's actions

#### Scenario: a generic JSON payload keeps working unchanged

- **GIVEN** a `core.json.v1` payload
- **WHEN** a message-path rule evaluates a `$message.*` condition against it
- **THEN** the condition resolves exactly as it did before this requirement

#### Scenario: an undeclared field is not reachable

- **GIVEN** a payload implementing `message.RuleReadable` that omits a struct field from its
  declared projection
- **WHEN** a rule conditions on that field
- **THEN** the field is not present, and the engine does not fall back to the payload's structure

### Requirement: A payload that cannot supply rule-readable fields MUST be observable, never silently false

When a rule carries `$message.*` conditions and its message payload implements neither
`message.RuleReadable` nor the generic data surface, the engine SHALL surface that the payload was
unreadable, distinctly from a condition that evaluated false. The signal SHALL identify the rule and
the payload type, and SHALL be bounded — reported per rule and payload type rather than per message,
so a high-rate subject cannot flood the surface.

Evaluation SHALL still proceed without firing the rule: an unreadable payload SHALL NOT halt the
engine, fail other rules, or change any other rule's verdict.

#### Scenario: an unreadable payload reports rather than silently failing

- **GIVEN** a rule with `$message.*` conditions
- **AND** a message whose payload supplies no rule-readable fields
- **WHEN** the rule is evaluated
- **THEN** the engine surfaces the unreadable pairing, identifying the rule and the payload type
- **AND** the rule does not fire
- **AND** other rules evaluating the same message are unaffected

#### Scenario: repeated unreadable messages do not flood the signal

- **GIVEN** a rule and payload type that have already been reported unreadable
- **WHEN** further messages of that type are evaluated against that rule
- **THEN** the pairing is not reported again
