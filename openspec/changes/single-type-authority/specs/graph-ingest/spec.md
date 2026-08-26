## ADDED Requirements

### Requirement: A birth MUST carry a registered message type

graph-ingest MUST reject a birth whose `message_type` is not registered in the payload registry it holds, on both of its
create paths — the `entity.create` RPC, after the structural `IsValid` check and before any clone, profile, or KV work, and
its own in-process `CreateEntity` (the hierarchy container birth), before the state-contract check — through one shared
check that writes nothing and yields one classified error (class invalid, closed code `message_type_unregistered`, the key in
detail `message_type`). On the RPC lane that error MUST be the reply, MUST be metered once as
`mutation_rejections_total{reason="message_type_unregistered"}`, and MUST be accompanied by a loud log naming the key. On the
in-process lane the same error MUST be returned to the caller and MUST NOT be metered (the counter is labelled by RPC subject
and an in-process birth has none); the caller's existing WARN is the observable. The fact lane is unchanged: an unregistered
type is refused at decode, and the Graphable merge birth is therefore registered by construction and needs no check. When
hierarchy is enabled, graph-ingest MUST refuse to construct if its registry lacks `graph.hierarchy_container.v1` (O-16 (a)). `entity.reconcile`, `triple.append`, and `entity.delete` carry no type
and are not affected. graph-ingest MUST refuse to construct without a payload registry, and a create reaching either seam
of a component that nonetheless holds no registry MUST be refused with code `internal` and an ERROR log — never admitted.
graph-ingest's own hierarchy container births MUST carry the registered framework type `graph.hierarchy_container.v1`
(owner item O-16 (a); under (b) this sentence is replaced by an explicit exception naming the empty stamp and the `unknown`
metric label).

#### Scenario: an unregistered stamp never reaches ENTITY_STATES

- **GIVEN** a registry without `test.unknown.v1`
- **WHEN** an `entity.create` request stamps `test.unknown.v1`
- **THEN** the reply carries code `message_type_unregistered` and `detail.message_type` = `test.unknown.v1`
- **AND** no `ENTITY_STATES` key is created
- **AND** `mutation_rejections_total{reason="message_type_unregistered"}` increments exactly once
- **AND** the test that verifies this is `TestCreateRejectsUnregisteredMessageType`

#### Scenario: a registered stamp is born unchanged

- **GIVEN** `agentic.agent_lesson.v1` is registered
- **WHEN** an `entity.create` request stamps it
- **THEN** the entity is created with that `message_type` persisted verbatim
- **AND** the test that verifies this is `TestCreateAcceptsRegisteredMessageType`

#### Scenario: a hierarchy container is born with a registered type

- **GIVEN** `enable_hierarchy: true` and the builtin payload set registered
- **WHEN** a Graphable arrival causes graph-ingest to birth a container
- **THEN** the container's `message_type` is `graph.hierarchy_container.v1`
- **AND** `indexing_profile_default_total{message_type="unknown"}` does not increment
- **AND** the test that verifies this is `TestHierarchyContainerBirthCarriesRegisteredType`

#### Scenario: an in-process birth with an unregistered type is refused

- **WHEN** `Component.CreateEntity` is called with an entity whose type is not registered
- **THEN** it returns the classified `message_type_unregistered` error naming the key
- **AND** nothing is written and `mutation_rejections_total` does not increment
- **AND** the test that verifies this is `TestInProcessCreateRejectsUnregisteredType`

#### Scenario: hierarchy enabled without the container type is a construction error

- **GIVEN** `enable_hierarchy: true` and a registry that does not hold `graph.hierarchy_container.v1`
- **WHEN** graph-ingest is constructed
- **THEN** construction fails naming the type
- **AND** no subscription is installed and no container birth is ever attempted
- **AND** the test that verifies this is `TestFactoryRejectsHierarchyWithoutContainerType`

#### Scenario: a create with no registry configured is refused

- **GIVEN** a graph-ingest component constructed without a payload registry
- **WHEN** an `entity.create` request reaches the create seam
- **THEN** the reply carries code `internal`
- **AND** nothing is written and the process does not panic
- **AND** the test that verifies this is `TestCreateSeamRejectsWhenRegistryMissing`

#### Scenario: a missing registry is a construction error

- **WHEN** graph-ingest is constructed with a nil `PayloadRegistry`
- **THEN** construction fails naming the dependency
- **AND** no subscription is installed
- **AND** the test that verifies this is `TestFactoryRejectsNilPayloadRegistry`

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
- **AND** the test that verifies this is `TestFloorComesFromRegistration`

#### Scenario: a registered type with no floor is metered

- **GIVEN** `test.nofloor.v1` is registered with an empty floor
- **WHEN** an entity of that type is created with no declared profile
- **THEN** `entity.indexing.profile` is `control`
- **AND** `indexing_profile_default_total{message_type="test.nofloor.v1"}` increments exactly once
- **AND** the test that verifies this is `TestFloorComesFromRegistration`

