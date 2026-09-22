# agentic-dispatch — delta

> Delta for #1330 (restart-safety **L4a**), docket OQ4, owner ruling 2026-09-22: the submission counter's
> redelivery semantics are documented rather than armed away. No dispatch code changes; the requirement states
> what `tasks_submitted_total` already means so a reader of the metric cannot take it for a submission count.

## ADDED Requirements

### Requirement: The task submission counter is at-least-once under redelivery
`tasks_submitted_total` SHALL be treated as an at-least-once count of task submissions: a redelivered
`UserMessage` whose task already committed SHALL increment the counter again, and SHALL remain otherwise
idempotent — it reuses the retained LoopID and publishes no second task.

The counter is a submission-attempt signal, not a distinct-task count. Nothing in dispatch or in the loop
suppresses the second increment, and no arm is added to make it exactly-once.

#### Scenario: A redelivered task submission counts again

- **GIVEN** a `UserMessage` whose task committed with a retained LoopID
- **WHEN** the source redelivers that `UserMessage`
- **THEN** `tasks_submitted_total` increments a second time
- **AND** the retained LoopID is reused, no second task is published, and no loop is created
