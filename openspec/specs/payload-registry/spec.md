# payload-registry Specification

## Purpose
The payload registry is the deployment's single type authority (ADR-103): a `message.Type` exists in a binary only by
being registered there, with its indexing profile floor and its graph-state contracts bound at the same registration.
Registration is what makes a type publishable (`BaseMessage.MarshalJSON` gates on the payload's `Validate()`) and what
graph-ingest consults to refuse an unregistered stamp — readers, codecs, and boot sweeps never consult it. Each binary
composes its own registry; there is no global.
## Requirements
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

That choice generalises to one rule for every E2E tier (owner ruling on #1249, 2026-09-22, amending Q5). An E2E tier boots
the binary whose composition it proves: a tier that proves the production composition boots `cmd/semstreams` through a
`production`-derived Dockerfile target, and a tier that needs non-production registrations — examples, fixtures, the mission
workflow, a control responder — boots `cmd/e2e-semstreams` through the `e2e` target. An E2E-only hook that must run INSIDE
the production composition — a probe, a barrier, a fault injector — therefore lands in the binary its tier boots, never in
the E2E root, gated by that tier's build tag so no shipped artifact contains it and, where it must stay inert in the tier's
other stages, by an environment variable that only that tier's compose file sets.

The tier's binary, target and gate are READ from the artifacts that boot it — `build.target` in the tier's compose service,
the Go package and `-tags=` of that target in `docker/Dockerfile` — never predicted from the word "test". This table is the
observation, and a contract test re-reads it against those artifacts:

| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate | E2E-only registrations and hooks | Synthetic types stamped on `entity.create` |
|---|---|---|---|---|---|
| core — phase 1 (`core-health`, `core-dataflow`) | `e2e.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |
| core — phase 2 (`core-graph-roundtrip`), lessons | `e2e.yml` `semstreams-fixtures` (profile fixtures) | `e2e` → `cmd/e2e-semstreams` | root selection | fixture payloads | `test.fixture.v1` (evidence fixture for lessons) |
| structural | `tiered.yml` `semstreams-structural` (profile structural) | `e2e` → `cmd/e2e-semstreams` | root selection | example components, fixture payloads | `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1` |
| statistical, throughput | `tiered.yml` `semstreams` (profile statistical) | `e2e` → `cmd/e2e-semstreams` | root selection | example components | none |
| semantic (and its `:8b` / `:frontier` overlays) | `tiered.yml` `semstreams-ml` (profile semantic) | `e2e` → `cmd/e2e-semstreams` | root selection | example components | none |
| lifecycle | `lifecycle.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | root selection | mission component, `--lifecycle-seed` | none (`lifecycle.harness.v1` is a framework type) |
| ops | `ops.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | root selection | lesson-curation control responder | none (its seed is the framework type `agentic.loop_completed.v1`, written by direct `PutKV` — `ops/scenario.go:439,484`) |
| research-graph | `research-graph.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | root selection | fixture payloads | `research.e2e_search_seed.v1` |
| crud-tools | `crud-tools.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none on create (`e2e.probe.v1` is a direct `PutKV`) |
| deep-research | `deep-research.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |
| agentic | `agentic.yml` `semstreams` | `e2e-process-barrier` → `cmd/semstreams` | `-tags=e2e_process_barrier`, env `SEMSTREAMS_E2E_MILESTONE_PROBE` | process barrier (tool executor), milestone settlement probe (`MilestoneHandler`) | none |
| slow-consumer | `e2e-slow-consumer.yml` `semstreams` | `e2e-slow-consumer` → `cmd/semstreams` | `-tags=e2e_slow_consumer` | slow-consumer boot probe | none |

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

#### Scenario: every tier's target, binary and gate are the ones its artifacts carry

- **GIVEN** the tier table above
- **WHEN** each row is read against `docker/compose/*.yml` and `docker/Dockerfile`
- **THEN** the named service's `build.target` is the row's target, that target's image runs a binary built from the row's Go
  package with exactly the row's `-tags=`, and the service sets exactly the row's `SEMSTREAMS_E2E_*` variables
- **AND** every compose service built from `docker/Dockerfile` appears in the table and every row names a service that exists
- **AND** no compose file mentions a `SEMSTREAMS_E2E_*` variable its rows do not declare — including in an overlay service
  with no `build:` block, since compose merges `environment:` across `-f` files, and including in a comment
- **AND** each declared variable carries a nonempty effective value, an uninterpolated `${VAR}` counting as empty, because
  a hook reading its gate with `os.Getenv` treats `NAME=` exactly as unset and leaves the tier's proof silently unarmed
- **AND** no two Dockerfile targets share an `image:` tag, so a build for one tier cannot leak into another
- **AND** the test that verifies this is `TestE2ETierTableMatchesComposeAndDockerfile`

#### Scenario: an E2E-only hook cannot reach the production build

- **GIVEN** the production root named by the table's `production` rows
- **WHEN** its non-test source files are read
- **THEN** every file importing an E2E harness package carries a build constraint naming one of the tags the Dockerfile
  builds that package with, so an ordinary build of the shipped binary links no harness at all
- **AND** the converse holds per file: a file only an overlay tag can build imports a harness, so a hook cannot be left
  stranded behind a constraint that reaches nothing
- **AND** the test that verifies this is `TestProductionRootReachesNoE2EHarnessWithoutABuildTag`

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

