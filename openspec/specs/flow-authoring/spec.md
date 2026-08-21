# flow-authoring Specification

## Purpose
Defines saved Flow diagrams as authoring artifacts and explicit, next-boot-only publication of compiled component
configuration.
## Requirements
### Requirement: Saved Flow mutations are authoring-only

Flow create, read, update, and delete SHALL operate on saved authoring data in flowstore. Create and update SHALL
retain the existing validation behavior. Saving or updating a Flow SHALL NOT publish component configuration or mutate
the running process.

Flowstore SHALL NOT persist or claim current component lifecycle state, runtime activation, or current runtime
membership.

#### Scenario: Save does not publish

- **GIVEN** a valid Flow diagram
- **WHEN** an author creates or updates the saved Flow
- **THEN** flowstore contains the authoring change
- **AND** no component configuration write occurs
- **AND** the running process remains unchanged

#### Scenario: Invalid Flow is rejected before persistence

- **WHEN** a Flow fails the existing validation contract
- **THEN** the author receives the validation failure
- **AND** neither flowstore nor component configuration changes

### Requirement: Component configuration publication is explicit and next-boot-only

`POST /flows/{id}/publish-component-configs` SHALL load the saved Flow, apply the existing validator and compiler,
sort compiled component instance names, and call the existing Config Manager component write operation sequentially.

Publication SHALL be upsert-only. A component omitted from the compiled Flow SHALL NOT cause deletion of an existing
component configuration. Publication SHALL NOT mutate the running component set or automatically restart the process.

#### Scenario: Successful publication reports observed outcome

- **GIVEN** a valid saved Flow that compiles to component instances B and A
- **WHEN** the author explicitly publishes component configuration
- **THEN** Config Manager receives upserts for A and then B
- **AND** the response reports A and B as persisted
- **AND** the response reports the running process unchanged and reboot required

#### Scenario: Saving alone never publishes

- **GIVEN** a valid saved Flow that has not been explicitly published
- **WHEN** the process continues running or later reads the Flow
- **THEN** no component configuration is inferred from the saved authoring record

#### Scenario: Omission does not delete

- **GIVEN** an existing component configuration for A
- **AND** a saved Flow compiles without A
- **WHEN** the Flow is explicitly published
- **THEN** publication does not delete A
- **AND** any desired removal is handled outside this upsert-only operation

### Requirement: Partial publication reports exact retry-safe progress

If a sequential component write fails, publication SHALL stop and report the exact sorted prefix already persisted and
the component instance whose write failed. It SHALL NOT report unattempted instances as persisted. Retrying the same
publication SHALL be safe because every attempted operation is an upsert of the same compiled configuration.

#### Scenario: Middle write fails

- **GIVEN** a valid Flow compiling to sorted instances A, B, and C
- **AND** Config Manager accepts A and rejects B
- **WHEN** publication runs
- **THEN** the response reports persisted instances `[A]`
- **AND** it reports B as the failed instance
- **AND** it does not report C as persisted
- **AND** retry may safely upsert A again before retrying B

### Requirement: Flow lifecycle surfaces are absent

Flow runtime lifecycle state, operations, agent tools, metrics, timestamps, logs, and streams SHALL NOT exist. Retired
surfaces SHALL have no compatibility aliases and no replacement monitor.

Name-keyed Flow health, metrics, or message observations MAY remain when they report current component observations.
Their presence SHALL NOT claim Flow ownership of component lifecycle, runtime activation, or authoring publication.

#### Scenario: Lifecycle operation is not routed

- **WHEN** a caller attempts a retired Flow runtime lifecycle operation
- **THEN** no supported HTTP or agent-tool route provides that operation
- **AND** the caller uses authoring CRUD, optional explicit publication, and process supervision instead

#### Scenario: Observation does not imply lifecycle ownership

- **GIVEN** a saved Flow whose component names match current runtime observations
- **WHEN** a caller reads retained Flow observation data
- **THEN** the response reports only those observations
- **AND** it does not claim the Flow authoring record activated or owns the components
