## MODIFIED Requirements

### Requirement: A lesson is an evidence-cited first-class graph entity with content-derived identity
The framework SHALL persist each lesson as a first-class graph entity
(`{org}.{platform}.agent.lesson.record.{id}`) minted through the canonical graph mutation API
with a semantic envelope, where `{id}` MUST be derived deterministically from the lesson's
content (category, scope keys, summary, evidence set) so that re-emitting an identical lesson
cannot create a second entity, and the writer MUST reject any lesson carrying zero evidence
citations. The lesson type `agentic.agent_lesson.v1` MUST be a registered Graphable payload
(`agentic.AgentLessonEntity`: factory, floor `content`, birth contract registered with the type);
`emit_lesson` MUST construct that entity and birth it through the entity's own `Triples()`, so the
lesson has a serializable form as itself and one builder of its triples.

#### Scenario: Evidence-cited lesson is created
- **WHEN** `emit_lesson` is called with summary, detail, injection form, category, polarity,
  severity, at least one typed `applies_to` key, and at least one well-formed evidence entity ID
- **THEN** an `agent.lesson.record` entity is created via the canonical `entity.create` operation carrying
  `agent.lesson.*` triples, the evidence references, attribution to the emitting loop, and
  `agent.lesson.status` of `proposed`
- **AND** the entity's `message_type` is the registered `agentic.agent_lesson.v1`

#### Scenario: Re-emitting an identical lesson is idempotent
- **WHEN** `emit_lesson` is called twice with identical category, scope keys, summary, and
  evidence set
- **THEN** both calls derive the same entity ID and only one lesson entity exists

#### Scenario: Evidence-free lesson is rejected
- **WHEN** `emit_lesson` is called with an empty `evidence_entity_ids` list or an entry that
  is not a well-formed 6-part entity ID
- **THEN** the call fails with an error naming the evidence contract and no graph mutation is
  published

#### Scenario: A lesson decodes as itself on the fact lane
- **GIVEN** the framework builtin payload set is registered
- **WHEN** a marshalled `AgentLessonEntity` arrives on a Graphable input
- **THEN** it decodes to `*AgentLessonEntity` and ingests with the same entity ID and predicate set its birth produced

### Requirement: External lesson composition uses the framework-owned contract snapshot

The framework MUST expose a purpose-scoped function returning an independent copy of the canonical lesson-record
projection contract — the contract registered with `agentic.agent_lesson.v1` in the payload registry. A product composition
root MUST be able to include that snapshot in its local projection mutation client without reproducing the contract name,
lifecycle group name, entity pattern, predicate membership, or birth-versus-mutable classification, and MUST NOT need any
framework-internal package to do so.

`LessonCurator` MUST continue to depend on the narrow `PredicateReconciler` and `AuthoritativeReader`
capabilities. The framework MUST NOT reintroduce the retired `NewNATSLessonCurator` helper.

The snapshot path MUST NOT introduce a bespoke agent, LLM persona, prompt role, or framework agent type.

#### Scenario: External composition uses the canonical lesson contract

- **GIVEN** first-party vocabulary is registered and a connected NATS client is available
- **WHEN** a product includes `LessonProjectionContract()` in its local mutation-client contract set
- **THEN** construction validates the framework-owned canonical lesson contract
- **AND** the product supplies no copied lesson-contract literals
- **AND** the product injects only reconciler and authoritative-reader capabilities into `LessonCurator`

#### Scenario: Contract snapshots are independent

- **WHEN** a caller modifies the contract or nested predicate slices returned by `LessonProjectionContract()`
- **THEN** a later call returns the unchanged canonical lesson contract

#### Scenario: The snapshot is the registered contract

- **WHEN** `LessonProjectionContract()` is compared with the contract the builtin registry holds for `agentic.agent_lesson.v1`
- **THEN** they are equal

#### Scenario: Canonical lifecycle transition preserves birth facts

- **GIVEN** a lesson record contains every framework-declared birth predicate and a valid lifecycle state
- **WHEN** a curator composed from `LessonProjectionContract()` promotes, retires, or supersedes the lesson
- **THEN** every birth predicate retains its prior object set
- **AND** the lifecycle predicate group equals the complete desired state for that transition

#### Scenario: Retired NATS helper remains absent

- **WHEN** standard lesson composition is inspected
- **THEN** the mutation client is constructed at the product composition root
- **AND** `LessonCurator` receives only its narrow reconciler and authoritative-reader capabilities
- **AND** no `NewNATSLessonCurator` production helper is exposed
