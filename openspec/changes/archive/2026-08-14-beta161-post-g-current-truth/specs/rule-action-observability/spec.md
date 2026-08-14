<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: FireEveryNEvents admission is independently observable

The rule processor SHALL expose
`semstreams_rule_action_gate_passes_total{rule_name}` as a CounterVec. It SHALL increment exactly once after a match
passes `FireEveryNEvents` admission and before rule-event execution, optional notification publication, or graph-event
delivery. A match rejected by `FireEveryNEvents` SHALL NOT increment the counter.

The counter SHALL report admission independently of later outcomes. An empty action list, malformed action output,
optional notification outcome, or graph-event delivery outcome SHALL NOT fabricate a pass or remove a pass already
counted.

#### Scenario: Every third matching event is admitted

- **GIVEN** a rule named `sampled-rule` with `FireEveryNEvents = 3`
- **WHEN** nine matching events are evaluated
- **THEN** `semstreams_rule_action_gate_passes_total{rule_name="sampled-rule"}` increments exactly three times
- **AND** the six gate-rejected matches do not increment it

#### Scenario: A post-admission failure does not erase the pass

- **GIVEN** a match that passes `FireEveryNEvents`
- **WHEN** rule-event execution or a later delivery step fails
- **THEN** the named rule's gate-pass counter has already incremented exactly once
- **AND** the later failure remains observable through its existing telemetry

### Requirement: Rule-trigger notification is optional

The `rule_events` output SHALL remain an optional rule-trigger notification. When a rule processor has no
`rule_events` port, it SHALL make no notification publish attempt and SHALL emit no missing-port warning or error.
Absence SHALL NOT prevent admitted rule actions from executing or graph events from using their existing delivery
path.

An explicitly configured `rule_events` port remains subject to validation and delivery telemetry. Malformed port
facts or subject declarations and configured publication failures SHALL remain observable through the existing
failure paths; they SHALL NOT be treated as though the port were absent.

#### Scenario: A shipped processor omits optional notification

- **GIVEN** a shipped rule processor with no `rule_events` output
- **WHEN** a match passes `FireEveryNEvents`
- **THEN** no rule-trigger notification publish is attempted
- **AND** no missing-port warning or error is emitted
- **AND** rule execution and graph-event delivery continue

#### Scenario: A configured notification port is malformed

- **GIVEN** a rule processor with an explicit `rule_events` output whose facts or subject declaration is malformed
- **WHEN** an admitted action reaches optional notification
- **THEN** the configuration failure remains observable through the existing error path
- **AND** it is not silently treated as an absent port

#### Scenario: Configured notification publication fails

- **GIVEN** a valid explicit `rule_events` output
- **WHEN** its notification publication fails
- **THEN** the transport failure remains observable through existing warning or error telemetry
- **AND** it is not silently treated as an absent port
