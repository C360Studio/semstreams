## ADDED Requirements

### Requirement: A message type is a type of the deployment only if it is registered in the binary's payload registry

The payload registry MUST be the single authority for which `message.Type` keys (`domain.category.version`) exist in a
deployment. `Register` MUST reject a nil registration, a nil factory, an empty domain, category, or version, a factory whose
payload `Schema()` disagrees with the registration, and a key already registered; there MUST be no second catalogue of
types, and no global registry — each binary constructs its own and injects it through `Dependencies.PayloadRegistry`. A type
registered in one binary is not thereby a type of another: the attributes registered with it (floor, contracts) exist only
where the type is registered.

A production-target e2e tier stamps only what the production binary registers (owner ruling on #1100, 2026-08-27):
`cmd/semstreams` registers no test type, so a scenario that births a synthetic type runs against `cmd/e2e-semstreams`, whose
composition root registers them through `cmd/e2e-semstreams/fixtures.RegisterPayloads`.

| Tier (`task e2e:<tier>`) | App target / binary | Synthetic types stamped on `entity.create` |
|---|---|---|
| core — phase 1 (`core-health`, `core-dataflow`) | `production` / `cmd/semstreams` (`docker/compose/e2e.yml` `semstreams`) | none |
| core — phase 2 (`core-graph-roundtrip`) | `e2e` / `cmd/e2e-semstreams` (`e2e.yml` `semstreams-fixtures`, profile `fixtures`) | `test.fixture.v1` |
| lessons | `e2e` (`e2e.yml` `semstreams-fixtures`) | `test.fixture.v1` (evidence fixture) |
| research-graph | `e2e` (`research-graph.yml`) | `research.e2e_search_seed.v1` |
| structural / statistical / semantic | `e2e` (`tiered.yml`) | `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1` (structural) |
| lifecycle | `e2e` (`lifecycle.yml`) | none (`lifecycle.harness.v1` is a framework type) |
| ops | `e2e` (`ops.yml`) | none (its seed is the framework type `agentic.loop_completed.v1`, written by direct `PutKV` — `ops/scenario.go:464,472`) |
| agentic | `production` (`agentic.yml`) | none |
| crud-tools | `production` (`crud-tools.yml`) | none on create (`e2e.probe.v1` is a direct `PutKV`) |
| deep-research | `production` (`deep-research.yml`) | none |
| slow-consumer | `production`-derived (`e2e-slow-consumer.yml`) | none |

#### Scenario: a colliding key is refused at registration

- **GIVEN** a registry holding `agentic.agent_lesson.v1`
- **WHEN** a second registration with the same domain, category, and version is registered
- **THEN** `Register` returns an error naming the key
- **AND** the first registration is unchanged
- **AND** the test that verifies this is `TestRegistry_RegisterPayload_DuplicateError`

#### Scenario: a type is known only where it is registered

- **GIVEN** a binary that does not select graph research
- **WHEN** `IndexingProfileFor("research.result.v1")` is read from its registry
- **THEN** it reports the type as unregistered with no floor
- **AND** the test that verifies this is `TestIndexingProfileFor`

#### Scenario: a component holding the key separator is refused at registration

- **WHEN** a registration declares `Domain: "bad.domain"` (or a category or version containing `.`)
- **THEN** `Register` returns an error naming the separator and stores nothing — the key could never round-trip through
  `Key()`, so the error belongs at boot, not at the first `Create`
- **AND** `types.Type.Validate` is the one owner of that component grammar
- **AND** the test that verifies this is `TestRegisterRejectsMalformedComponent` (and `TestTypeValidateOwnsComponentGrammar`)

#### Scenario: a factory that disagrees with its registration is refused

- **WHEN** a registration's factory produces a payload whose `Schema()` returns a different domain, category, or version
- **THEN** `Register` returns an error naming both tuples
- **AND** the test that verifies this is `TestRegisterRejectsSchemaMismatch`

### Requirement: A registration carries the indexing-profile floor and the projection contracts bound to the type

`Registration` MUST carry an optional `IndexingProfile` (the ADR-054 channel-(c) floor for entities born with the type) and an
optional list of `Contracts` (the projection contracts bound to the type). `Register` MUST reject an `IndexingProfile` outside the
vocabulary's profile set; MUST fill an empty contract `MessageType` with the registration's key and reject a contract naming a
different key; MUST reject duplicate contract names within one registration; and MUST validate each contract's shape (name,
entity pattern, groups, birth predicates, profile). Predicate declaration is not checked at registration. A registered type
with an empty floor is admitted; graph-ingest meters it. Copies returned by lookups MUST include both attributes with
independent contract copies.

#### Scenario: a contract registered with a type inherits the type's key

- **WHEN** `agentic.agent_lesson.v1` is registered with a contract whose `MessageType` is empty
- **THEN** the stored contract's `MessageType` is `agentic.agent_lesson.v1`
- **AND** the test that verifies this is `TestRegisterFillsAndChecksContractMessageType`

#### Scenario: a contract naming another key is refused

- **WHEN** `agentic.agent_lesson.v1` is registered with a contract whose `MessageType` is `agentic.loop_execution.v1`
- **THEN** `Register` returns an error naming both keys
- **AND** the test that verifies this is `TestRegisterFillsAndChecksContractMessageType`

#### Scenario: an invalid floor is refused

- **WHEN** a registration declares `IndexingProfile: "prose"`
- **THEN** `Register` returns an error naming the value
- **AND** the test that verifies this is `TestRegisterRejectsInvalidIndexingProfile`

#### Scenario: a registered type may declare no floor

- **WHEN** a registration declares no `IndexingProfile`
- **THEN** `Register` succeeds
- **AND** `IndexingProfileFor(key)` reports the type as registered with an empty floor
- **AND** the test that verifies this is `TestIndexingProfileFor`

### Requirement: The registry exposes floor and contract lookups

The registry MUST expose `IndexingProfileFor(key) (profile string, registered bool)` and `Contracts() []contract.Contract`
returning fresh copies ordered by key then contract name. graph-ingest MUST obtain the floor through the registry it already
holds, and the composition root MUST derive its projection-contract set from `Contracts()`; no other table of floors or of
framework contracts MAY exist.

#### Scenario: the composition root's contract set is the registry's

- **GIVEN** the framework builtin set is registered
- **WHEN** `Contracts()` is read
- **THEN** it contains exactly one contract per registered contract name, including the loop-execution and lesson-record contracts
- **AND** mutating a returned copy does not change a later read
- **AND** the test that verifies this is `TestContractsReturnsIndependentSortedCopies`

### Requirement: Framework entity types born on the mutation lane are registered Graphable payloads

Every framework type stamped on `entity.create` MUST be registered by the framework builtin set with a factory producing a
payload that implements `EntityID()` and `Triples()`, round-trips through `BaseMessage`, and declares its floor:
`agentic.loop_execution.v1` (`control`), `agentic.agent_lesson.v1` (`content`), `agentic.ops_diagnosis.v1` (`content`),
`agentic.model_endpoint.v1` (`control`), `agentic.web_observation.v1` (`content`), `lifecycle.harness.v1` (`control`). The types that
hold a projection contract today (`agentic.loop_execution.v1`, `agentic.agent_lesson.v1`) MUST register it with the type;
whether `ops_diagnosis`, `model_endpoint`, and `web_observation` gain a birth contract in this change is owner item O-4
(unruled: no contract; #818's lane). The type's `Triples()` MUST be the only builder of its triples and MUST reproduce the
former writer's triples byte-for-byte except `Timestamp`, and for every registered contract the relation birth ⊆
predicates(`Triples()` of a fully populated entity) ⊆ birth ∪ groups MUST hold — a group predicate (a todo record, the lesson
lifecycle) may be absent at birth and a birth-time value of a group predicate (`agent.lesson.status`) is admitted. Under owner
item O-16 (a) graph-ingest's hierarchy container type `graph.hierarchy_container.v1` (`control`, verbatim carrier) joins the
builtin set. No framework type MAY be documented as "mutation-only, not registered".

#### Scenario: a lesson round-trips through the production decoder

- **GIVEN** a fully populated `AgentLessonEntity`
- **WHEN** it is marshalled and decoded through `message.NewDecoder(reg)` with the builtin set registered
- **THEN** the decoded payload is an `*AgentLessonEntity` with equal fields
- **AND** its `EntityID()` and the predicate set of `Triples()` equal the original's
- **AND** the test that verifies this is `TestAgentLessonEntity_RoundTrip`

#### Scenario: the builtin set registers every mutation-lane type with a floor

- **WHEN** the builtin set is registered into a fresh registry
- **THEN** each of the six keys (seven under O-16 (a)) is registered with a non-empty floor
- **AND** `agentic.loop_execution.v1` and `agentic.agent_lesson.v1` carry a contract whose `MessageType` equals the key (the
  other three only under O-4 = mint)
- **AND** the test that verifies this is `TestPayloadRegistryIsTheSingleTypeAuthority`

#### Scenario: a contract that drifts from its builder is caught

- **WHEN** a birth predicate is removed from a type's `Triples()` builder but not from its registered contract
- **THEN** the conformance test for that type fails naming the predicate
- **AND** the test that verifies this is `TestRegisteredContractMatchesTriples`

#### Scenario: moved builders are byte-identical to the writers they replace

- **GIVEN** a golden literal captured from each former builder for a fully populated entity and for one with every optional
  field zero
- **WHEN** the registered type's `Triples()` runs on the same inputs
- **THEN** predicate, object (type and value), `Source`, and `Confidence` match triple-for-triple, and only `Timestamp` differs
- **AND** the test that verifies this is `TestModelEndpointEntityMatchesBuilder` (also `TestOpsDiagnosisEntityMatchesBuilder`,
  `TestWebObservationEntityMatchesToolBuilders`, `TestEmitLessonBuildsEntityTriples`)


### Requirement: A registered payload's `Validate()` is the writer's full contract

A registered framework entity type MUST carry the complete contract its writer used to enforce — every required field,
closed vocabulary, numeric range, byte bound, control-byte rule, and entity-ID grammar — in ONE validator that both the
writer's argument parser and `Validate()` use, because registration makes a type publishable: `BaseMessage.MarshalJSON`
uses `Payload.Validate()` as the publication gate. The parser MAY normalise (clamp a severity) and MUST check only wire shape; it MUST
NOT duplicate or weaken the contract. A payload that fails `Validate()` MUST fail to marshal through `BaseMessage`.

Boundary (fact lane): graph-ingest's fact-lane consumer decodes through `message.NewDecoder` WITHOUT calling `Validate()`
(`processor/graph-ingest/component.go` `extractEntityFromMessage`), so wire bytes that bypass `BaseMessage.MarshalJSON` are
not gated by this requirement; that lane's missing validation is #1112's, not this change's. The decoded payload still
carries the contract.

#### Scenario: a malformed registered payload is unpublishable

- **WHEN** an `OpsDiagnosisEntity` with no finding, recommendation, evidence, severity, or executor and a confidence of 2
  (the Codex repro) — or any one of the lesson, model-endpoint, loop-execution, or web-observation contract violations —
  is validated and marshalled through `message.NewBaseMessage`
- **THEN** `Validate()` returns an error naming the fault and `json.Marshal` fails
- **AND** the tests that verify this are `TestAgentLessonEntityRejectsMalformed`, `TestOpsDiagnosisEntityRejectsMalformed`,
  `TestModelEndpointEntityRejectsMalformed`, `TestLoopExecutionEntityRejectsMalformed`, `TestWebObservationEntityRejectsMalformed`

#### Scenario: a malformed finding never reaches the graph

- **GIVEN** a real graph-ingest holding the builtin set
- **WHEN** `emit_diagnosis` is invoked with the Codex repro shape
- **THEN** the tool returns an invalid-arguments result, no `ops.diagnosis.finding` key is born, and the same shape cannot be
  marshalled through `BaseMessage`
- **AND** the test that verifies this is `TestMalformedDiagnosisNeverReachesTheGraph`
