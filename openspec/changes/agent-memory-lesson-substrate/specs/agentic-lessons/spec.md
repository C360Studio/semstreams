# agentic-lessons — delta

## ADDED Requirements

### Requirement: A lesson is an evidence-cited first-class graph entity with content-derived identity
The framework SHALL persist each lesson as a first-class graph entity
(`{org}.{platform}.agent.lesson.record.{id}`) minted through the canonical graph mutation API
with a semantic envelope, where `{id}` MUST be derived deterministically from the lesson's
content (category, scope keys, summary, evidence set) so that re-emitting an identical lesson
cannot create a second entity, and the writer MUST reject any lesson carrying zero evidence
citations.

#### Scenario: Evidence-cited lesson is created
- **WHEN** `emit_lesson` is called with summary, detail, injection form, category, polarity,
  severity, at least one typed `applies_to` key, and at least one well-formed evidence entity ID
- **THEN** an `agent.lesson.record` entity is created via `create_with_triples` carrying
  `lesson.*` triples, the evidence references, attribution to the emitting loop, and
  `lesson.status` of `proposed`

#### Scenario: Re-emitting an identical lesson is idempotent
- **WHEN** `emit_lesson` is called twice with identical category, scope keys, summary, and
  evidence set
- **THEN** both calls derive the same entity ID and only one lesson entity exists

#### Scenario: Evidence-free lesson is rejected
- **WHEN** `emit_lesson` is called with an empty `evidence_entity_ids` list or an entry that
  is not a well-formed 6-part entity ID
- **THEN** the call fails with an error naming the evidence contract and no graph mutation is
  published

### Requirement: Lesson content separates stored detail from a bounded injection form
The writer SHALL store rich lesson detail separately from a compressed injection form, and
MUST reject an injection form exceeding the configured byte bound with an error that names the
bound, rather than truncating silently.

#### Scenario: Injection form within bound is accepted
- **WHEN** `emit_lesson` is called with an injection form at or under the bound
- **THEN** the lesson persists both `lesson.detail` (unbounded prose) and
  `lesson.injection_form` (bounded) as distinct predicates

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
- **THEN** the lesson carries `lesson.observed_role` and an `agent.action.executed-by`
  backlink derived from the loop context, without the caller passing identity parameters

### Requirement: Lessons carry a gated lifecycle and only active lessons are injectable
Lessons SHALL be created with `lesson.status` of `proposed`, and the framework MUST exclude
any lesson whose status is not `active` from brief injection. Promotion and retirement SHALL
be single-valued replace-not-append writes through the canonical `update_with_triples` lane
(rule `replace_owned` or a product curation writer), promotion MUST resolve that every cited
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
- **WHEN** an active lesson's `lesson.status` is replaced with `retired`
- **THEN** subsequent brief assembly excludes it and the entity remains queryable through the
  graph gateway

### Requirement: Lesson predicates split rule-matchable fields from rule-opaque authored text
The vocabulary SHALL register `lesson.polarity`, `lesson.severity`, and `lesson.status` as
closed-enum rule-matchable predicates and `lesson.category` as an OPEN rule-matchable
predicate carrying no framework-defined value set, and MUST register the LLM-authored text
predicates (`lesson.summary`, `lesson.detail`, `lesson.injection_form`) with
`WithRuleOpaque(true)` per house convention.

#### Scenario: A rule routes on lesson enums
- **WHEN** a rule condition matches `lesson.severity == "critical"`
- **THEN** the rule engine evaluates it against lesson entities like any rule-visible predicate

#### Scenario: Category is open
- **WHEN** a product emits a lesson with a category value the framework has never seen
- **THEN** the write succeeds; no framework-side closed set constrains `lesson.category`

#### Scenario: Authored text is opaque to rules
- **WHEN** the registry metadata for `lesson.summary`, `lesson.detail`, and
  `lesson.injection_form` is inspected
- **THEN** each is registered rule-opaque

### Requirement: Standards grounding is annotation-only PROV-O alignment
Lesson predicates that are semantically equivalent to W3C PROV-O terms SHALL carry
`StandardIRI` annotations using the existing constants in `vocabulary/standards.go` (at
minimum `lesson.evidence` → `ProvWasDerivedFrom`), and the framework MUST NOT introduce RDF or
external-schema machinery beyond these annotations.

#### Scenario: Evidence predicate carries its PROV-O equivalence
- **WHEN** the predicate registration for `lesson.evidence` is inspected
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
