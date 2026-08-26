## ADDED Requirements

### Requirement: A message type is a type of the deployment only if it is registered in the binary's payload registry

The payload registry MUST be the single authority for which `message.Type` keys (`domain.category.version`) exist in a
deployment. `Register` MUST reject a nil registration, a nil factory, an empty domain, category, or version, a factory whose
payload `Schema()` disagrees with the registration, and a key already registered; there MUST be no second catalogue of
types, and no global registry — each binary constructs its own and injects it through `Dependencies.PayloadRegistry`. A type
registered in one binary is not thereby a type of another: the attributes registered with it (floor, contracts) exist only
where the type is registered.

#### Scenario: a colliding key is refused at registration

- **GIVEN** a registry holding `agentic.agent_lesson.v1`
- **WHEN** a second registration with the same domain, category, and version is registered
- **THEN** `Register` returns an error naming the key
- **AND** the first registration is unchanged

#### Scenario: a type is known only where it is registered

- **GIVEN** a binary that does not select graph research
- **WHEN** `IndexingProfileFor("research.result.v1")` is read from its registry
- **THEN** it reports the type as unregistered with no floor

#### Scenario: a factory that disagrees with its registration is refused

- **WHEN** a registration's factory produces a payload whose `Schema()` returns a different domain, category, or version
- **THEN** `Register` returns an error naming both tuples

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

#### Scenario: a contract naming another key is refused

- **WHEN** `agentic.agent_lesson.v1` is registered with a contract whose `MessageType` is `agentic.loop_execution.v1`
- **THEN** `Register` returns an error naming both keys

#### Scenario: an invalid floor is refused

- **WHEN** a registration declares `IndexingProfile: "prose"`
- **THEN** `Register` returns an error naming the value

#### Scenario: a registered type may declare no floor

- **WHEN** a registration declares no `IndexingProfile`
- **THEN** `Register` succeeds
- **AND** `IndexingProfileFor(key)` reports the type as registered with an empty floor

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

### Requirement: Framework entity types born on the mutation lane are registered Graphable payloads

Every framework type stamped on `entity.create` MUST be registered by the framework builtin set with a factory producing a
payload that implements `EntityID()` and `Triples()`, round-trips through `BaseMessage`, and declares its floor:
`agentic.loop_execution.v1` (`control`), `agentic.agent_lesson.v1` (`content`), `agentic.ops_diagnosis.v1` (`content`),
`agentic.model_endpoint.v1` (`control`), `agentic.web_observation.v1` (`content`), `lifecycle.harness.v1` (`control`). The five
`agentic` types MUST register their birth contract with the type; the type's `Triples()` MUST be the only builder of its
triples, and the registered contract's birth and group predicates MUST equal the predicate set `Triples()` emits for a fully
populated entity. No framework type MAY be documented as "mutation-only, not registered".

#### Scenario: a lesson round-trips through the production decoder

- **GIVEN** a fully populated `AgentLessonEntity`
- **WHEN** it is marshalled and decoded through `message.NewDecoder(reg)` with the builtin set registered
- **THEN** the decoded payload is an `*AgentLessonEntity` with equal fields
- **AND** its `EntityID()` and the predicate set of `Triples()` equal the original's

#### Scenario: the builtin set registers every mutation-lane type with a floor

- **WHEN** the builtin set is registered into a fresh registry
- **THEN** each of the six keys is registered with a non-empty floor
- **AND** each of the five `agentic` keys carries at least one contract whose `MessageType` equals the key

#### Scenario: a contract that drifts from its builder is caught

- **WHEN** a predicate is removed from a type's `Triples()` builder but not from its registered contract
- **THEN** the conformance test for that type fails naming the predicate
