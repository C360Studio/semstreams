# agentic-lessons Specification

## Purpose

`agentic-lessons` governs **procedural memory**: how completed agent work is distilled into
durable, reusable guidance, and how that guidance reaches later loops. A lesson is a
first-class graph entity (`{org}.{platform}.lesson.agent.record.{id}`), not a document — it
lives in the same substrate as every other agent artifact, under the same mutation and
projection contracts.

One interface decision binds the whole capability: **memory is pushed, never queried.** The
framework registers no memory search tools; active lessons are assembled into a loop's brief by
the substrate before the agent runs, and the only memory-specific agent read is dereferencing a
reference it was already handed. This is the irreversible lesson of the prior attempt, where
memory exposed as agent-invoked query tools was ignored in favour of training-corpus habits and
then removed as friction (ADR-080).

Three invariants carry the weight:

- **Evidence or nothing.** A lesson cannot be created without at least one well-formed evidence
  citation, and cannot be promoted until every cited entity resolves in the graph. Resolving
  evidence is what makes a promotion honest; refusal leaves the lesson `proposed`.
- **Injectability is earned.** Lessons are born `proposed`, and only `active` lessons reach a
  brief. The lifecycle gate is operator/product review — the framework ships no agent-facing
  promotion tool.
- **Delivery is bounded and replay-stable.** Selection is a pure function of the candidate set
  and the loop's scope, ordered on the lesson's immutable birth timestamp rather than any
  revision or re-stamped update time, and truncated to a ranked prefix under explicit count and
  byte bounds. The same graph yields the same brief, including across an ADR-073 from-zero
  reingest.

Bounds are contract, not hygiene: an oversized injection form is rejected with an error naming
the bound rather than silently truncated, because that form is rendered verbatim into every
future brief. Relatedly, the authored-text predicates (`summary`, `detail`, `injection-form`)
register rule-opaque, so rules cannot predicate on model-sampled prose.
## Requirements
### Requirement: A lesson is an evidence-cited first-class graph entity with content-derived identity
The framework SHALL persist each lesson as a first-class graph entity
(`{org}.{platform}.lesson.agent.record.{id}`) minted through the canonical graph mutation API
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

### Requirement: Lesson content separates stored detail from a bounded injection form
The writer SHALL store rich lesson detail separately from a compressed injection form, and
MUST reject an injection form exceeding the configured byte bound with an error that names the
bound, rather than truncating silently.

#### Scenario: Injection form within bound is accepted
- **WHEN** `emit_lesson` is called with an injection form at or under the bound
- **THEN** the lesson persists both `agent.lesson.detail` (unbounded prose) and
  `agent.lesson.injection-form` (bounded) as distinct predicates

#### Scenario: Oversized injection form is rejected instructively
- **WHEN** `emit_lesson` is called with an injection form over the bound
- **THEN** the call fails with an error stating the byte bound so the agent can rewrite, and
  no graph mutation is published

### Requirement: Scope keys use a typed grammar with minimum specificity
The writer SHALL require at least one `applies_to` scope key and MUST accept only typed keys —
`id:<entity-ID prefix>` with at least three segments, or `tag:<token>` — and scope matching
MUST compare id-prefixes on entity-ID segment boundaries only.

#### Scenario: Untyped or over-broad scope key is rejected
- **WHEN** `emit_lesson` is called with `applies_to` of `["c360"]` or `["id:c360"]`
- **THEN** the call fails naming the typed-grammar and minimum-specificity rules (an id-prefix
  of fewer than three segments would match an entire org)

#### Scenario: Prefix matching respects segment boundaries
- **WHEN** a lesson carries `applies_to: ["id:c360.ops.robotics"]` and a loop's scope contains
  the entity `c360.ops-agent.robotics.gcs.drone.001`
- **THEN** the lesson does not match (the prefix `c360.ops` is not the segment `c360.ops-agent`)

### Requirement: emit_lesson is the only agent-facing lesson creation path, on the ops seam
The framework SHALL expose lesson creation to agents solely through the `emit_lesson` tool
registered beside `emit_diagnosis` on the ADR-027 ops observation seam; the tool MUST NOT stop
the calling loop, and the executor MUST enforce a per-loop emission cap.

#### Scenario: Multiple lessons from one ops loop
- **WHEN** an ops-role loop calls `emit_lesson` three times across its iterations
- **THEN** three distinct lesson entities exist and the loop continues to its own terminal

#### Scenario: Per-loop cap bounds runaway emission
- **WHEN** a loop exceeds the configured per-loop lesson cap
- **THEN** further `emit_lesson` calls in that loop fail with an error naming the cap and no
  entities are created

#### Scenario: Attribution is derived, not supplied
- **WHEN** `emit_lesson` executes inside a loop with execution context
- **THEN** the lesson carries `agent.lesson.observed-role` and an `agent.action.executed-by`
  backlink derived from the loop context, without the caller passing identity parameters

### Requirement: Lessons carry a gated lifecycle and only active lessons are injectable
Lessons SHALL be created with `agent.lesson.status` of `proposed`, and the framework MUST exclude
any lesson whose status is not `active` from brief injection. Promotion and retirement SHALL
be single-valued reconcile-not-append writes through the canonical `entity.reconcile` operation
(rule `reconcile_predicates` or a product curation writer), promotion MUST resolve that every cited
evidence entity exists, and retired or superseded lessons MUST remain durable in the graph for
audit.

#### Scenario: Proposed lesson is not injected
- **WHEN** a freshly emitted lesson matches a loop's scope but its status is `proposed`
- **THEN** brief assembly does not include it

#### Scenario: Promotion resolves evidence existence
- **WHEN** a promotion write targets a lesson citing an evidence entity that does not exist in
  the graph
- **THEN** the promotion is refused and the lesson remains `proposed`

#### Scenario: Retired lesson leaves the brief, not the graph
- **WHEN** an active lesson's `agent.lesson.status` is replaced with `retired`
- **THEN** subsequent brief assembly excludes it and the entity remains queryable through the
  graph gateway

### Requirement: Lesson predicates split rule-matchable fields from rule-opaque authored text
The vocabulary SHALL register `agent.lesson.polarity`, `agent.lesson.severity`, and `agent.lesson.status` as
closed-enum rule-matchable predicates and `agent.lesson.category` as an OPEN rule-matchable
predicate carrying no framework-defined value set, and MUST register the LLM-authored text
predicates (`agent.lesson.summary`, `agent.lesson.detail`, `agent.lesson.injection-form`) with
`WithRuleOpaque(true)` per house convention.

#### Scenario: A rule routes on lesson enums
- **WHEN** a rule condition matches `agent.lesson.severity == "critical"`
- **THEN** the rule engine evaluates it against lesson entities like any rule-visible predicate

#### Scenario: Category is open
- **WHEN** a product emits a lesson with a category value the framework has never seen
- **THEN** the write succeeds; no framework-side closed set constrains `agent.lesson.category`

#### Scenario: Authored text is opaque to rules
- **WHEN** the registry metadata for `agent.lesson.summary`, `agent.lesson.detail`, and
  `agent.lesson.injection-form` is inspected
- **THEN** each is registered rule-opaque

### Requirement: Standards grounding is annotation-only PROV-O alignment
Lesson predicates that are semantically equivalent to W3C PROV-O terms SHALL carry
`StandardIRI` annotations using the existing constants in `vocabulary/standards.go` (at
minimum `agent.lesson.evidence` → `ProvWasDerivedFrom`), and the framework MUST NOT introduce RDF or
external-schema machinery beyond these annotations.

#### Scenario: Evidence predicate carries its PROV-O equivalence
- **WHEN** the predicate registration for `agent.lesson.evidence` is inspected
- **THEN** its metadata carries the `ProvWasDerivedFrom` StandardIRI

### Requirement: Active lessons reach agents by bounded deterministic push at brief assembly
The framework SHALL deliver lessons to agents exclusively through substrate-side brief
assembly: at loop dispatch it SHALL select active lessons whose typed scope keys match the
loop's scope, order them by severity then stored emit-timestamp (replay-stable) then entity
ID, bound the result by both a count ceiling (K ≤ 25, default 10) and a total-byte budget,
and render injection forms with their entity IDs; the framework MUST NOT register any
dedicated agent-invoked lesson search, list, or query tool.

#### Scenario: Matching active lessons arrive in the brief
- **WHEN** a loop dispatches with a scope matching two active lessons
- **THEN** the loop's brief contains both injection forms with their lesson entity IDs, with
  no agent tool call involved

#### Scenario: Ordering is replay-stable
- **WHEN** the same loop scope is dispatched twice against unchanged graph state, including
  after an ADR-073 from-zero reingest
- **THEN** the injected lessons and their order are identical (ordering derives from stored
  emit-time triples, never KV revisions or re-stamped update times)

#### Scenario: Delivery is bounded and observable
- **WHEN** more lessons match than the count ceiling or byte budget allow
- **THEN** the brief carries the bounded selection and the injection block states
  matched-versus-included counts

#### Scenario: No dedicated lesson search tool exists
- **WHEN** the built-in tool registry is enumerated
- **THEN** it contains `emit_lesson` (write) and no dedicated lesson search, list, or query
  tool (generic graph-read tools remain governed by per-role allowlists)

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

