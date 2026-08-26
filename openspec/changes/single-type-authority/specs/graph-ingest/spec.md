## ADDED Requirements

### Requirement: A mutation-lane birth MUST carry a registered message type

graph-ingest MUST reject an `entity.create` whose `entity.message_type` is not registered in the payload registry it holds,
after the structural `IsValid` check and before any clone, profile, or KV work. The rejection MUST use the closed code
`message_type_unregistered`, class invalid, with the key in detail `message_type`; MUST write nothing; MUST be metered once as
`mutation_rejections_total{reason="message_type_unregistered"}`; and MUST emit a loud log naming the key. The fact lane is
unchanged: an unregistered type is refused at decode. `entity.reconcile`, `triple.append`, and `entity.delete` carry no type
and are not affected. graph-ingest MUST refuse to construct without a payload registry.

#### Scenario: an unregistered stamp never reaches ENTITY_STATES

- **GIVEN** a registry without `test.unknown.v1`
- **WHEN** an `entity.create` request stamps `test.unknown.v1`
- **THEN** the reply carries code `message_type_unregistered` and `detail.message_type` = `test.unknown.v1`
- **AND** no `ENTITY_STATES` key is created
- **AND** `mutation_rejections_total{reason="message_type_unregistered"}` increments exactly once

#### Scenario: a registered stamp is born unchanged

- **GIVEN** `agentic.agent_lesson.v1` is registered
- **WHEN** an `entity.create` request stamps it
- **THEN** the entity is created with that `message_type` persisted verbatim

#### Scenario: a missing registry is a construction error

- **WHEN** graph-ingest is constructed with a nil `PayloadRegistry`
- **THEN** construction fails naming the dependency
- **AND** no subscription is installed

### Requirement: The indexing-profile floor is read from the registered type

When neither the Graphable payload nor the mutation envelope declares a profile, graph-ingest MUST take the floor from the
registered type's `IndexingProfile`; an empty floor MUST fall to `control` and increment
`indexing_profile_default_total{message_type}`, which now means "a registered type declares no floor". No string-keyed floor
table MAY exist in graph-ingest.

#### Scenario: a registered floor is stamped without a metric

- **GIVEN** `agentic.request.v1` is registered with floor `trace`
- **WHEN** an entity of that type arrives with no declared profile
- **THEN** `entity.indexing.profile` is `trace`
- **AND** `indexing_profile_default_total{message_type="agentic.request.v1"}` does not increment

#### Scenario: a registered type with no floor is metered

- **GIVEN** `test.nofloor.v1` is registered with an empty floor
- **WHEN** an entity of that type is created with no declared profile
- **THEN** `entity.indexing.profile` is `control`
- **AND** `indexing_profile_default_total{message_type="test.nofloor.v1"}` increments exactly once
