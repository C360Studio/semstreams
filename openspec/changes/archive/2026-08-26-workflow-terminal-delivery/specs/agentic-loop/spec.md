## ADDED Requirements

### Requirement: A decide terminal SHALL be carried as a typed decision on the completion event

When a loop completes because a `StopLoop` tool result arrived from the framework decide tool, the loop SHALL populate
`LoopCompletedEvent.Decision` with the decision's `Action` and `Reason` taken from the tool result's typed metadata,
and SHALL leave `Result` unchanged. The loop SHALL identify the tool through its existing name-fallback chain — the
tracked name for the call ID first, then the tool result's own `Name` — so a process restart or cache loss does not
demote a decide terminal. When the terminal tool is any other tool, or the loop completes on model text, `Decision`
SHALL be nil. The loop SHALL NOT infer a decision from the shape of `Result`. `LoopCompletedEvent.Validate` SHALL
reject a present `Decision` whose `Action` or `Reason` is empty; an unknown but nonempty `Action` SHALL remain valid.
When the terminal tool IS the decide tool but its typed metadata cannot supply both a nonempty `Action` and a
nonempty `Reason` — absent, empty, or not a string — the loop SHALL leave `Decision` nil and SHALL warn, rather than
stamp a half-decision: a present `Decision` with an empty field fails validation and is permanently rejected, which
would lose the terminal entirely instead of degrading it to the existing route-ownership behaviour.

#### Scenario: decide terminal carries its decision

- **GIVEN** a loop whose pending tool call is tracked under the decide tool name
- **AND** its tool result has `StopLoop=true` and metadata `action` and `reason`
- **WHEN** the loop completes
- **THEN** the published completion event decodes with `Decision.Action` and `Decision.Reason` equal to that metadata
- **AND** `Result` equals the tool result content

#### Scenario: tracked name absent, result name identifies decide

- **GIVEN** a loop with no tracked tool name for the terminal call ID
- **AND** the tool result's `Name` is the decide tool name with `StopLoop=true` and decision metadata
- **WHEN** the loop completes
- **THEN** the published completion event decodes with `Decision` populated

#### Scenario: non-decide terminal carries no decision

- **GIVEN** a loop whose terminal `StopLoop` tool is tracked under any other name
- **WHEN** the loop completes
- **THEN** the published completion event decodes with a nil `Decision`

#### Scenario: synthesized decision does not populate the field

- **GIVEN** a loop that completes on model text with `decide` in its tool set
- **WHEN** the framework synthesizes a `needs_clarification` decision triple after completion
- **THEN** the published completion event still decodes with a nil `Decision`

#### Scenario: unusable decide metadata leaves the field nil rather than half-stamped

- **GIVEN** a terminal `StopLoop` tool result named for the decide tool
- **AND** its typed metadata has no `action`/`reason`, an empty one, or a non-string one
- **WHEN** the loop completes
- **THEN** the published completion event decodes with a nil `Decision`
- **AND** the completion still validates, so the terminal is delivered under the existing route-ownership behaviour
- **AND** the loop warns that the decide terminal carried no usable typed decision

#### Scenario: present decision with an empty field fails validation

- **GIVEN** a `LoopCompletedEvent` whose `Decision` is present with an empty `Action` or an empty `Reason`
- **WHEN** the payload is validated
- **THEN** validation fails
- **AND** a `Decision` with an unknown but nonempty `Action` and a nonempty `Reason` validates

#### Scenario: additive wire field round-trips

- **GIVEN** a marshalled `agentic.loop_completed.v1` envelope carrying `decision`
- **WHEN** the production decoder decodes it into a fresh value
- **THEN** the concrete payload is `*agentic.LoopCompletedEvent` with `Decision` populated
- **AND** an envelope without `decision` decodes with a nil `Decision`
