## ADDED Requirements

### Requirement: Offloaded-body resolution is instance-exact

A `StorageReference` SHALL be resolved only through the live store registered under its exact non-empty
`StorageInstance`. A registry miss SHALL NOT be served from a default, configured bucket, owned fallback, or any store
registered under another name.

An unresolved instance is an explicit content exclusion, not a resolved-store failure. The unresolved body SHALL be
counted through the existing content-unresolved observable. Existing inline identity text MAY continue through
embedding; if no usable inline text remains, the entity SHALL reach the existing skipped/no-text outcome. The miss
alone SHALL NOT create a failed embedding, increment a failure reason, or make embedding readiness degraded.

Resolution SHALL remain lazy per fetch. If an instance deregisters after the entity is admitted but before the worker
fetches, the worker SHALL apply the same unresolved/excluded behavior.

Once the exact instance resolves, an Open or Read failure from that store remains a real content failure and SHALL
retain existing failed/degraded accounting.

#### Scenario: The exact registered instance serves the body

- **GIVEN** a reference naming instance A and live stores registered as A and B
- **WHEN** graph-embedding resolves the reference
- **THEN** it reads only store A
- **AND** store B is never consulted

#### Scenario: A foreign instance never falls back

- **GIVEN** a reference naming unregistered instance B while another store A is available
- **WHEN** graph-embedding processes the entity
- **THEN** store A is never opened for the reference
- **AND** the body is counted as unresolved/excluded
- **AND** the miss does not enter failed/degraded accounting

#### Scenario: Inline identity continues after an unresolved body

- **GIVEN** an entity with an unresolved storage reference and inline identity text
- **WHEN** graph-embedding processes it
- **THEN** the inline identity text remains eligible for embedding
- **AND** no body bytes are guessed from another store

#### Scenario: An unresolved body with no inline text skips

- **GIVEN** an entity with an unresolved storage reference and no usable inline text
- **WHEN** graph-embedding processes it
- **THEN** it reaches the existing skipped/no-text terminal outcome
- **AND** any stale vector is removed through the existing no-text behavior
- **AND** the entity is not recorded as failed

#### Scenario: Deregistration between admission and fetch remains non-degrading

- **GIVEN** the exact instance exists when the component admits the entity
- **AND** that instance deregisters before the worker fetches
- **WHEN** the worker resolves lazily
- **THEN** it applies unresolved/excluded behavior
- **AND** it does not record a content failure solely because the name is now absent

#### Scenario: A resolved-store read failure remains a failure

- **GIVEN** the exact named instance resolves successfully
- **WHEN** that store fails to Open or Read the referenced key
- **THEN** graph-embedding records the existing bounded content-failure outcome
- **AND** failed/degraded observability remains intact
