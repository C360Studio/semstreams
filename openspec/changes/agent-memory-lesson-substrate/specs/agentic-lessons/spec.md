# agentic-lessons — delta

## ADDED Requirements

### Requirement: A lesson is an evidence-cited first-class graph entity
The framework SHALL persist each lesson as a first-class graph entity
(`{org}.{platform}.ops.lesson.record.{uuid}`) minted through the canonical graph mutation API
with a semantic envelope, and the writer MUST reject any lesson carrying zero evidence
citations.

#### Scenario: Evidence-cited lesson is created
- **WHEN** `emit_lesson` is called with summary, detail, injection form, category, polarity,
  and at least one evidence entity ID
- **THEN** a `ops.lesson.record` entity is created via `create_with_triples` carrying
  `lesson.*` triples, the evidence references, and attribution to the emitting loop

#### Scenario: Evidence-free lesson is rejected
- **WHEN** `emit_lesson` is called with an empty `evidence_entity_ids` list
- **THEN** the call fails with an error naming the missing-evidence contract and no graph
  mutation is published

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

### Requirement: emit_lesson is the only agent-facing lesson write path, on the ops seam
The framework SHALL expose lesson creation to agents solely through the `emit_lesson` tool
registered beside `emit_diagnosis` on the ADR-027 ops observation seam, and the tool MUST NOT
stop the calling loop, so one observation loop can emit multiple lessons.

#### Scenario: Multiple lessons from one ops loop
- **WHEN** an ops-role loop calls `emit_lesson` three times across its iterations
- **THEN** three distinct lesson entities exist and the loop continues to its own terminal

#### Scenario: Attribution is derived, not supplied
- **WHEN** `emit_lesson` executes inside a loop with execution context
- **THEN** the lesson carries `lesson.observed_role` and an `agent.action.executed-by`
  backlink derived from the loop context, without the caller passing identity parameters

### Requirement: Lesson predicates split rule-matchable enums from rule-opaque authored text
The vocabulary SHALL register `lesson.category`, `lesson.polarity`, `lesson.severity`, and
`lesson.status` as closed-enum rule-matchable predicates, and MUST register the LLM-authored
text predicates (`lesson.summary`, `lesson.detail`, `lesson.injection_form`) rule-opaque.

#### Scenario: A rule routes on lesson enums
- **WHEN** a rule condition matches `lesson.severity == "critical"`
- **THEN** the rule engine evaluates it against lesson entities like any rule-visible predicate

#### Scenario: Authored text is opaque to rules
- **WHEN** the registry metadata for `lesson.summary`, `lesson.detail`, and
  `lesson.injection_form` is inspected
- **THEN** each is registered rule-opaque

### Requirement: Standards grounding is annotation-only PROV-O alignment
Lesson predicates that are semantically equivalent to W3C PROV-O terms SHALL carry
`StandardIRI` annotations (at minimum `lesson.evidence` → `prov:wasDerivedFrom`), and the
framework MUST NOT introduce RDF or external-schema machinery beyond these annotations.

#### Scenario: Evidence predicate carries its PROV-O equivalence
- **WHEN** the predicate registration for `lesson.evidence` is inspected
- **THEN** its metadata carries the `prov:wasDerivedFrom` StandardIRI from
  `vocabulary/standards.go`

### Requirement: Retirement metadata governs delivery, never deletion
Lessons SHALL carry optional single-valued lifecycle predicates (`lesson.status`,
`lesson.retired_at`, `lesson.superseded_by`) written replace-not-append, and retired or
superseded lessons MUST be excluded from default delivery while remaining durable in the graph
for audit.

#### Scenario: Retired lesson leaves the brief, not the graph
- **WHEN** a lesson's `lesson.status` is set to `retired`
- **THEN** default lesson delivery excludes it, and the entity remains queryable through the
  graph gateway

### Requirement: Lessons reach agents by push only
The framework SHALL deliver lessons to agents exclusively through declaration-driven retrieval
(the fusion lessons facet and substrate-side prompt assembly), and MUST NOT register any
agent-invoked lesson search or query tool.

#### Scenario: No lesson search tool exists
- **WHEN** the built-in tool registry is enumerated
- **THEN** it contains `emit_lesson` (write) and no lesson search, list, or query tool

#### Scenario: Lessons arrive via declared facet
- **WHEN** a fusion request declares `want:[lessons]` with a matching scope
- **THEN** bounded lesson injection forms are returned in the projection without any agent
  tool call
