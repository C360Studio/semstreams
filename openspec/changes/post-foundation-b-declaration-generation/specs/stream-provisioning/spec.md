## ADDED Requirements

### Requirement: Provisioning intent and accepted runtime declaration are distinct facts

The config-time stream planner SHALL run before component construction from canonical `PortConfig`, shared resolution,
and normalized facts. A Registry generation snapshot SHALL represent admitted runtime declaration state.

Neither owner SHALL consume or import the other's policy. Registry snapshots SHALL NOT create, reconcile, or repair
streams. The stream planner SHALL NOT infer component readiness, lifecycle, or admitted runtime state.

#### Scenario: Provisioning may precede later component failure

- **GIVEN** valid configured stream-provisioning intent
- **WHEN** provisioning succeeds and the component later fails construction or Start
- **THEN** the provisioned stream remains truthful configured intent
- **AND** no Registry readiness claim is inferred

#### Scenario: Runtime admission does not provision

- **GIVEN** a component generation is added to Registry
- **WHEN** its snapshot becomes observable
- **THEN** that snapshot does not create, reconcile, or repair a stream

#### Scenario: Both owners use canonical classification

- **GIVEN** one port declaration
- **WHEN** config-time planning and runtime admission classify it
- **THEN** both consume the canonical resolver and normalized facts
- **AND** neither inspects a concrete port-config type or imports the other's policy response

### Requirement: Default-only JetStream outputs require explicit preconstruction coverage

The shipped-flow structural census SHALL contain exactly 61 effective factory-default JetStream output rows absent
from raw component output configuration. All 61 SHALL be explicitly covered by `AGENT` / `agent.>` preconstruction
declarations, and zero SHALL be uncovered.

A future default-only output without explicit preconstruction coverage SHALL fail structural validation. The planner
SHALL NOT guess a stream from a component factory default or runtime Registry snapshot.

#### Scenario: Shipped coverage remains 61 of 61

- **WHEN** all shipped enabled configurations are structurally analyzed
- **THEN** exactly 61 default-only JetStream output rows are found
- **AND** all 61 are covered by `AGENT` / `agent.>`
- **AND** zero are uncovered

#### Scenario: Future uncovered default fails

- **GIVEN** a new factory-default JetStream output omitted from raw config and not covered by explicit
  preconstruction stream policy
- **WHEN** structural validation runs
- **THEN** validation fails
- **AND** the planner does not guess a stream from runtime declarations

