# agentic-loop Specification

## Purpose

Define how agentic loops enforce per-spawn iteration budgets within the operator ceiling and report
budget exhaustion consistently to downstream consumers.

## Requirements
### Requirement: A spawn may narrow its loop iteration budget

`agentic.TaskMessage` MUST accept an optional per-spawn `max_iterations`; a nil value uses the component
default, a value below 1 fails task validation, and the effective budget MUST be the minimum of the spawn
value and the component `MaxIterations` ceiling. The `publish_agent` rule action MUST expose this as
`loop_max_iterations` with variable substitution, and a substituted value that is not a positive integer MUST
fail the action with a classified, observable error.

#### Scenario: spawn narrows the budget

- **GIVEN** a component configured with MaxIterations 20
- **WHEN** a task is spawned with max_iterations 2
- **THEN** the loop fails with reason "max_iterations" after 2 iterations

#### Scenario: spawn cannot widen past the operator ceiling

- **GIVEN** a component configured with MaxIterations 5
- **WHEN** a task is spawned with max_iterations 50
- **THEN** the effective budget is 5

#### Scenario: substituted budget from an entity triple

- **GIVEN** a publish_agent action with loop_max_iterations "$entity.triple.task.spec.budget"
- **WHEN** the rule fires on an entity carrying that predicate with value "3"
- **THEN** the spawned task carries max_iterations 3

#### Scenario: non-integer substitution fails loudly

- **GIVEN** a publish_agent action whose loop_max_iterations substitutes to "unbounded"
- **WHEN** the rule fires
- **THEN** the action fails with a classified error and a bounded rejection metric, and no task is published

### Requirement: Iteration exhaustion publishes one uniform reason

Every path that detects iteration-budget exhaustion MUST publish the loop-terminal failure reason
`"max_iterations"`. Internal detection MUST use a typed sentinel error mapped via errors.Is; consumers MUST
NOT need to match error text to distinguish budget exhaustion from other handler failures.

#### Scenario: model-response guard at the cap

- **GIVEN** a loop whose iteration count has reached its budget
- **WHEN** the next model response arrives
- **THEN** the published failure reason is "max_iterations"

#### Scenario: tool drain at the cap

- **GIVEN** a loop at its budget with tool calls still in flight
- **WHEN** the pending tools are drained with synthetic failures
- **THEN** the published failure reason is "max_iterations"
