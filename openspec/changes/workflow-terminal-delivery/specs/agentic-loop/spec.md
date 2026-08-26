## ADDED Requirements

### Requirement: A decide terminal SHALL be carried as a typed decision on the completion event

When a loop completes because a `StopLoop` tool result arrived from the tool whose tracked name is the framework
decide tool, the loop SHALL populate `LoopCompletedEvent.Decision` with the decision's `Action` and `Reason` taken from
the tool result's typed metadata, and SHALL leave `Result` unchanged. When the terminal tool is any other tool, or the
loop completes on model text, `Decision` SHALL be nil. The loop SHALL NOT infer a decision from the shape of `Result`.

#### Scenario: decide terminal carries its decision

- **GIVEN** a loop whose pending tool call is tracked under the decide tool name
- **AND** its tool result has `StopLoop=true` and metadata `action` and `reason`
- **WHEN** the loop completes
- **THEN** the published completion event decodes with `Decision.Action` and `Decision.Reason` equal to that metadata
- **AND** `Result` equals the tool result content

#### Scenario: non-decide terminal carries no decision

- **GIVEN** a loop whose terminal `StopLoop` tool is tracked under any other name
- **WHEN** the loop completes
- **THEN** the published completion event decodes with a nil `Decision`

#### Scenario: synthesized decision does not populate the field

- **GIVEN** a loop that completes on model text with `decide` in its tool set
- **WHEN** the framework synthesizes a `needs_clarification` decision triple after completion
- **THEN** the published completion event still decodes with a nil `Decision`

#### Scenario: additive wire field round-trips

- **GIVEN** a marshalled `agentic.loop_completed.v1` envelope carrying `decision`
- **WHEN** the production decoder decodes it into a fresh value
- **THEN** the concrete payload is `*agentic.LoopCompletedEvent` with `Decision` populated
- **AND** an envelope without `decision` decodes with a nil `Decision`
