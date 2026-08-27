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
lesson has a serializable form as itself and one builder of its triples. `AgentLessonEntity.Validate()` IS the ADR-080
writer contract (required fields, control-byte hygiene, the injection-form byte bound, the polarity / severity / status
vocabularies, at least one well-formed evidence entity ID, at least one typed scope key, a well-formed back-link);
`emit_lesson` parses wire shape, clamps severity, and delegates every other gate to it, counting the wrapped
`ErrLessonEvidence` / `ErrLessonBound` / `ErrLessonGrammar` sentinels as the ADR-080 rejection reasons.

#### Scenario: Evidence-cited lesson is created
- **WHEN** `emit_lesson` is called with summary, detail, injection form, category, polarity,
  severity, at least one typed `applies_to` key, and at least one well-formed evidence entity ID
- **THEN** an `agent.lesson.record` entity is created via the canonical `entity.create` operation carrying
  `agent.lesson.*` triples, the evidence references, attribution to the emitting loop, and
  `agent.lesson.status` of `proposed`
- **AND** the entity's `message_type` is the registered `agentic.agent_lesson.v1`
- **AND** the test that verifies this is `TestEmitLessonExecutor_CreatesLesson`

#### Scenario: The lesson contract is the entity's

- **WHEN** an `AgentLessonEntity` violates any gate the emit_lesson parser used to own
- **THEN** `Validate()` names the fault, `BaseMessage` refuses to marshal it, and `emit_lesson` returns the same rejection
- **AND** the test that verifies this is `TestAgentLessonEntityRejectsMalformed` (with `TestEmitLessonExecutor_EvidenceRejects`)

#### Scenario: Re-emitting an identical lesson is idempotent
- **WHEN** `emit_lesson` is called twice with identical category, scope keys, summary, and
  evidence set
- **THEN** both calls derive the same entity ID and only one lesson entity exists
- **AND** the test that verifies this is `TestEmitLessonExecutor_IdempotentReEmit`

#### Scenario: Evidence-free lesson is rejected
- **WHEN** `emit_lesson` is called with an empty `evidence_entity_ids` list or an entry that
  is not a well-formed 6-part entity ID
- **THEN** the call fails with an error naming the evidence contract and no graph mutation is
  published
- **AND** the test that verifies this is `TestEmitLessonExecutor_EvidenceRejects`

#### Scenario: A lesson decodes as itself on the fact lane
- **GIVEN** the framework builtin payload set is registered
- **WHEN** a marshalled `AgentLessonEntity` arrives on a Graphable input
- **THEN** it decodes to `*AgentLessonEntity` and ingests with the same entity ID and predicate set its birth produced
- **AND** the test that verifies this is `TestAgentLessonEntity_RoundTrip`

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
- **AND** the test that verifies this is `TestLessonProjectionContractMatchesCanonicalAndReturnsIndependentSnapshots`

#### Scenario: Contract snapshots are independent

- **WHEN** a caller modifies the contract or nested predicate slices returned by `LessonProjectionContract()`
- **THEN** a later call returns the unchanged canonical lesson contract
- **AND** the test that verifies this is `TestLessonProjectionContractMatchesCanonicalAndReturnsIndependentSnapshots`

#### Scenario: The snapshot is the registered contract

- **WHEN** `LessonProjectionContract()` is compared with the contract the builtin registry holds for `agentic.agent_lesson.v1`
- **THEN** they are equal
- **AND** the test that verifies this is `TestLessonProjectionContractIsTheRegisteredContract`

#### Scenario: Canonical lifecycle transition preserves birth facts

- **GIVEN** a lesson record contains every framework-declared birth predicate and a valid lifecycle state
- **WHEN** a curator composed from `LessonProjectionContract()` promotes, retires, or supersedes the lesson
- **THEN** every birth predicate retains its prior object set
- **AND** the lifecycle predicate group equals the complete desired state for that transition
- **AND** the test that verifies this is `TestLessonCurator_Promote_HappyPath`

#### Scenario: Retired NATS helper remains absent

- **WHEN** standard lesson composition is inspected
- **THEN** the mutation client is constructed at the product composition root
- **AND** `LessonCurator` receives only its narrow reconciler and authoritative-reader capabilities
- **AND** no `NewNATSLessonCurator` production helper is exposed
- **AND** the check that verifies this is `grep -rn 'NewNATSLessonCurator' --include='*.go' .` → 0 (tasks 7.2)

