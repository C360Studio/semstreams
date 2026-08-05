# Blast-Radius Audit: The graph-ingest Mutation API as a Producer API

> Status: audit only (blast-radius sizing) — **RESOLVED by [ADR-055](../adr/055-graph-write-intent-taxonomy.md),
> must-exist flip shipped `v1.0.0-beta.112` (2026-06-19).** Retained as the
> point-in-time evidence base. Fix design was a separate planning step
> (`graphable-fix-plan.md`). Generated 2026-06-11 via a 40-agent fan-out audit (9 producer clusters,
> 55 call sites classified, every anti-pattern finding adversarially refuted —
> 0 of 21 overturned). Motivated by ADR-054's note: *"The motivating
> `agent.agentic-loop.step` entities enter via the mutation API, not the
> Graphable path."*

## 1. Executive Summary

The anti-pattern is **real, concentrated, and high-confidence**. Of 55 classified
mutation-API call sites, **21 are confirmed anti-pattern** writes that originate
first-class domain entities through the graph-ingest mutation API instead of the
canonical `Events → Graphable → graph-ingest` path. Every one survived
adversarial refutation. The blast radius is **not** spread thinly — it collapses
to **seven distinct entity types** across **five producer clusters**:

- **agentic-loop graph_writer** — loop-execution, trajectory-step, model-endpoint
- **agentic-tools terminal executors** — ops-diagnosis, web-observation
- **agentic-memory output port** — operating-model (profile/layer/entry/lesson) + LLM-authored free facts
- **github-pr-workflow example** — workflow-execution

Only **one** of the seven (trajectory-step) already has a Graphable Go type — it
is a pure plumbing fix (register + change emit path). The other six have **no
Graphable producer at all** and need a type defined. A separate **grey zone of 9
lifecycle-harness sites** (`pkg/lifecycle` + `agentrun` milestone publisher) is
Flavor-A-shaped on ownership but is the explicitly-debated ADR-047/049 substrate
decision — it must be resolved by an ADR call, not folded into the fix. The
remaining 26 sites are cleanly legitimate (derived-fact, external-gateway,
operational) and are **out of fix scope**.

**The defect is squarely producer-side** — not query-side, not storage-side.

## 2. The Two Paths and What "Mutation API as Producer" Means

**Path 1 — Canonical Graphable.** A registered payload TYPE implements
`graph.Graphable` (`EntityID() string` + `Triples() []message.Triple`). It is
published as a `BaseMessage` through the payload registry, flows through a
JetStream stream, and graph-ingest's consumer
(`processor/graph-ingest/component.go:857` `extractEntityFromMessage` → `:844`
`MergeEntity`) writes it to `ENTITY_STATES`, **stamping `MessageType` provenance**
(`:885`). A domain Go type with domain expertise OWNS the entity's existence.

**Path 2 — Mutation API.** graph-ingest hosts NATS request/reply subjects
(`graph.mutation.triple.add` / `.add_batch` / `.triple.remove` /
`.entity.create*` / `.entity.update*` / `.entity.delete`) plus in-process write
methods. These write hand-assembled triples directly. `AddTriple`/`AddTriples`
**auto-vivifies** the Subject on first stamp (`mutations.go:28-30`: *"AddTriple's
CAS path creates the entity if it doesn't exist"*) — **no payload type, no
registry envelope, often no MessageType.**

**"Mutation API as producer"** = a component synthesizes a 6-part entity ID (the
primary key of a thing-in-the-world) and stamps hand-assembled triples on it via
Path 2, **creating** an entity that has no backing Graphable type and never
flowed through Path 1. Two flavors:

- **Flavor A (conjured):** no Graphable type exists; the entity exists *only*
  because triples were stamped.
- **Flavor B (bypassed):** a Graphable type EXISTS, but the producer harvests
  its `.Triples()` and ships them through the mutation API instead of publishing
  the payload.

**Critical re-classification trap:** a derived-fact stamp is only legitimate if
the Subject has a **Graphable origin**. A writer that is the first-and-only
toucher of a Subject that never had a Graphable origin is *conjuring*, not
deriving — even if the line looks like a derived edge.

## 3. Blast Radius Table — Confirmed Anti-Pattern Call Sites

| # | Entity Type | Location | Flavor | Severity | Missing Graphable Type |
|---|---|---|---|---|---|
| 1 | loop-execution | `processor/agentic-loop/graph_writer.go:269-293` WriteLoopCompletion | conjured | **high** | `agentic.LoopExecutionEntity` (none) |
| 2 | loop-execution | `graph_writer.go:299-323` WriteLoopFailure | conjured | **high** | same (co-conjuror) |
| 3 | loop-execution | `graph_writer.go:474-496` WriteSpawnIdentity | conjured | **high** | same (spawn-time first toucher) |
| 4 | loop-execution | `graph_writer.go:561-577` WriteLoopCancellation | conjured | **high** | same (terminal co-conjuror) |
| 5 | loop-execution | `graph_writer.go:173-227` WriteSyntheticDecide | conjured | low | same (derived-onto-conjured) |
| 6 | loop-execution | `graph_writer.go:394-417` WriteLineageTriples | conjured | low | same (derived-onto-conjured) |
| 7 | loop↔step edge | `graph_writer.go:759-767` LoopHasStep | bypassed | medium | `agentic.TrajectoryStepEntity` (exists) |
| 8 | model-endpoint | `graph_writer.go:229-258` WriteModelEndpoints | conjured | **high** | `agentic.ModelEndpointEntity` (none) |
| 9 | trajectory-step | `graph_writer.go:328-366` WriteTrajectorySteps | **bypassed** | **high** | `agentic.TrajectoryStepEntity` (EXISTS, unregistered) |
| 10 | ops-diagnosis | `processor/agentic-tools/emit_diagnosis.go:199` | conjured | **high** | `agentic.OpsDiagnosisFinding` (none) |
| 11 | web-observation | `processor/agentic-tools/executors/websearch.go:259` | conjured | **high** | `agentic.WebObservation` (none) |
| 12 | web-observation | `processor/agentic-tools/executors/httprequest.go:270` | conjured | **high** | same (shares ID constructor + predicates) |
| 13 | om-lesson | `processor/agentic-memory/handlers.go:139-157→170` persistCompactionAsLesson | conjured | **high** | `operatingmodel.Lesson` Graphable (none) |
| 14 | om-profile/layer/entry | `processor/agentic-memory/layer_approved_handler.go:54→61` | conjured | **high** | `operatingmodel.{Profile,Layer,Entry}` Graphable (none) |
| 15 | LLM-authored facts | `processor/agentic-memory/handlers.go:208` handleCompactionStarting | conjured | **high** | none (no controlled ID space — purest conjure) |
| 16 | om-transport seam | `processor/agentic-memory/publisher.go:102-149` publishGraphMutations | conjured | low | (shared seam; classified by its 3 callers above) |
| 17 | workflow-execution | `examples/github-pr-workflow/component.go:288-304` handleIssueEvent | conjured | **high** | `WorkflowExecutionEntity` (none) — conjuring ROOT |
| 18 | workflow-execution | `component.go:372-380` handleQualifierComplete | conjured | medium | same (derived-onto-conjured) |
| 19 | workflow-execution | `component.go:397-411` handleDeveloperComplete | conjured | medium | same (derived-onto-conjured) |
| 20 | workflow-execution | `component.go:423-446` handleReviewerComplete | conjured | medium | same (derived-onto-conjured) |
| 21 | workflow-execution | `component.go:561-583` accumulateTokens / incrementRejections | conjured | medium | same (derived-onto-conjured counters) |

Rows 17–21 are one entity family (the workflow-execution Subject) with the
conjuring root at row 17. **21 confirmed call sites → 7 entity types → 8 fixable
units** (row 16 is a transport seam, not an independent entity).

## 4. Entity-Type Catalog — The "What Needs to Become Graphable" List

### 4.1 loop-execution `{org}.{platform}.agent.agentic-loop.execution.{loopID}` — CONJURED (no type)

- **Writers:** `graph_writer.go` — `WriteSpawnIdentity` (:474, usually FIRST
  toucher at spawn), `WriteLoopCompletion` (:269), `WriteLoopFailure` (:299),
  `WriteLoopCancellation` (:561); derived-onto-conjured stampers
  `WriteSyntheticDecide` (:173), `WriteLineageTriples` (:394). Downstream
  second-writers inheriting the conjured Subject: `decide.go:432`,
  `write_todos.go:236/249`, `scratchpad.go:217`, `research-graph-llmwrap/triplepub.go`,
  `actions.go:1212` run-anchor (all currently *legit-derived modulo the missing origin*).
- **Graphable type:** NONE. `agentic.LoopExecutionEntityID` (`entity_ids.go:54`)
  is a bare string formatter. `LoopCompletedEvent`/`LoopFailedEvent`/
  `LoopCancelledEvent` are registered payloads but are fire-and-ack **events**,
  not Graphable.
- **Canonical home:** Define `agentic.LoopExecutionEntity`, register in
  `agentic/payload_registry.go`, publish through the agent loop stream →
  graph-ingest. **Single largest lever:** ADR-054:90 names this exact entity as
  the motivating case; fixing the origin retroactively legitimizes ~8 downstream
  derived-fact stampers.

### 4.2 trajectory-step `{org}.{platform}.agent.agentic-loop.step.{loopID}-{stepIndex}` — BYPASSED (type exists)

- **Writer:** `graph_writer.go:328-366` `WriteTrajectorySteps` harvests
  `entity.Triples()` (:757) and ships via `triple.add`.
- **Graphable type:** **EXISTS** — `agentic.TrajectoryStepEntity`
  (`trajectory_entity.go:15/26/33`; also `ContentStorable`). NOT registered
  (`payload_registry.go:19-44` omits it), no `MarshalJSON`.
- **Canonical home:** Register it (add `MarshalJSON` wrapping `BaseMessage`),
  publish through the existing loop stream. **Pure plumbing fix.** ObjectStore
  content offload at `:348` already works and stays. The `LoopHasStep` edge
  (row 7) rides the same payload's `Triples()`.

### 4.3 model-endpoint `{org}.{platform}.agent.model-registry.endpoint.{endpointName}` — CONJURED (no type)

- **Writer:** `graph_writer.go:229-258` `WriteModelEndpoints` at startup.
- **Graphable type:** NONE. `model.EndpointConfig` (`model/registry.go:195`) has
  no `EntityID()`/`Triples()`.
- **Canonical home:** `agentic.ModelEndpointEntity` wrapping `model.EndpointConfig`;
  `Triples()` reuses `buildModelEndpointTriples`. One `BaseMessage` per endpoint
  at boot. **Low complexity.**

### 4.4 ops-diagnosis `{org}.{platform}.ops.diagnosis.finding.{uuid}` — CONJURED (no type)

- **Writer:** `agentic-tools/emit_diagnosis.go:199` — mints uuid (:178),
  `OpsDiagnosisEntityID` (:179), `AddTriplesBatch`. Entity is **born** at the
  graph-ingest seam.
- **Graphable type:** NONE. `agentic.OpsDiagnosisEntityID`
  (`ops_diagnosis_entity.go:21`) is a pure constructor.
- **Canonical home:** `agentic.OpsDiagnosisFinding`, publish through registry.
  **Self-contained, single call site.**

### 4.5 web-observation `{org}.{platform}.agent.web.observation.{sha256-16(canonicalURL)}` — CONJURED (no type)

- **Writers:** `websearch.go:259` (6 predicates/result), `httprequest.go:270`
  (7 predicates/fetch). Content-addressed for cross-loop dedup; each stamps a
  `LoopObservedWeb`/`LoopFetchedWeb` back-link onto the conjured loop entity.
- **Graphable type:** NONE. `agentic.TryWebObservationEntityID`
  (`web_observation_entity.go:50`) is a pure ID/canonicalizer/hasher.
- **Canonical home:** `agentic.WebObservation` (one type serves both writers,
  shared constructor + vocabulary). Preserve the opportunistic
  log+counter+continue emission pattern.

### 4.6 operating-model family — CONJURED (no types)

Four conjured ID spaces, all written through the `agentic-memory`
`graph_mutations` output port (`publisher.go:138` → `graph.mutation.{loopID}`):

- `user.teams.profile.{userID}`, `user.teams.om-layer.{userID}-{layer}`,
  `user.teams.om-entry.{entryID}` — `layer_approved_handler.go:54→61`, harvesting
  `LayerApproved.Triples(org,platform)` (the payload exists but its method is
  `Triples(org,platform)`, deliberately **not** the no-arg `graph.Graphable.Triples()`).
- `user.teams.lesson.{lessonID}` — `handlers.go:139-157→170`, mints
  `lesson-{uuid}`, calls free function `LessonTriples`.
- **Graphable type:** NONE in `agentic/operating-model/` (zero no-arg
  `Triples() []message.Triple`; all builders are free functions).
- **Canonical home:** Define `operatingmodel.{Profile,Layer,Entry,Lesson}`
  Graphable types, register an operating-model `payload_registry.go`, publish
  through the AGENT stream. The `has_layer`/`has_lesson` edges become
  legit-derived once the profile has a Graphable origin. **Largest single-package
  conversion** (4 types + retire the custom transport seam).

### 4.7 LLM-authored free facts — CONJURED (no type, no controlled ID space)

- **Writer:** `handlers.go:208` `handleCompactionStarting` — ships
  `LLMExtractor.ExtractFacts` (`llm_extractor.go:54`) output verbatim; Subjects
  are **model-chosen**, no constructor, no 6-part validation, no MessageType.
  `LLMAssisted.Enabled` defaults `true` (`config.go:27,244`) — **live by default.**
- **Graphable type:** NONE — and no deterministic ID space at all. The **purest
  conjure** in the catalog. The package's own godoc (`lesson.go:13-17`) flags
  this as the legacy "triples no reader could find."
- **Canonical home:** Less "define a type", more "constrain the ID space" —
  route extraction through a Lesson/Fact Graphable shape with constructor-governed
  6-part Subjects. **Needs a design decision (see §8).**

### 4.8 workflow-execution `{org}.github.repo.{repo}.workflow.{issueNumber}` — CONJURED (no type)

- **Writers (one family):** conjuring root `component.go:288-304`
  `handleIssueEvent`; derived-onto-conjured updaters at :372, :397, :423,
  :561/:573. Package godoc (`entities.go:8-12`) **self-documents the anti-pattern
  by design**: *"does not instantiate these entities directly — it writes
  workflow-phase triples via the graph mutation API instead."*
- **Graphable type:** NONE for `.workflow.{n}`. Three real Graphables
  (`GitHubIssueEntity`, `GitHubPREntity`, `GitHubReviewEntity`) own disjoint spaces.
- **Canonical home:** Either a `WorkflowExecutionEntity` Graphable, **or** —
  because it is workflow-instance-shaped — a `pkg/lifecycle.Participant`
  (ADR-047). Example/demo, low production stakes, but the cleanest teaching case.

## 5. Grey Zone — Lifecycle / Participant Substrate (8 sites, needs explicit decision)

**Sites:** `pkg/lifecycle/graph_emit.go:124` (update) & `:162` (create);
`manager.go:330` (create-fresh), `:357` (attach-to-existing), `:507`
(Transition/Complete/Fail), `:633` (UpdateFromOperator). Entities:
`*.*.agent.chain.execution.*` (`agentrun.AgentRun`)
and `*.*.lifecycle.gcs.mission.*` (`mission.State`).

**The tension.** On pure Subject-ownership these are **Flavor-A-shaped**: they
originate first-class domain entities in `ENTITY_STATES` via
`entity.create_with_triples`/`update_with_triples`, with no `graph.Graphable`
producer — `AgentRun`/`mission.State` implement `lifecycle.Participant`
(`EntityID`/`Workflow`/`Phase`) but have **no `Triples()`**; the harness
reflection-projects struct-tagged fields (`projectStructToTriples`).

**Why it stays grey, not A.** (1) A typed domain owner DOES exist (the
Participant struct owns the shape via tagged fields); (2) every emit stamps
`lifecycleMessageType` (`manager.go:22-26`) — provenance IS present, unlike bare
`triple.add` conjurers; (3) **this exact write surface was the subject of a
dedicated ADR.** ADR-049 ran the bucket-ownership rubric per-field and explicitly
chose mutation-API/ENTITY_STATES emission over both a private bucket *and* the
Graphable path, **rejecting the Graphable path for a CAS-revision reason**. The
`manager.go:357` attach branch is even legit-derived for **mission** (real
Graphable `MissionCommand` origin) but conjuring-substrate for **agent-run** — a
branch legit for one Participant and conjuring for another is the definition of grey.

**Decision needed:** does Participant state ride the Graphable path (define a
generic `lifecycle.ParticipantEntity` adapter exposing projected triples via
`Triples()`) or stay on the deliberately-chosen CAS-on-condition mutation wire?
**This is an ADR-049 re-open, not a call-site patch.** Do not fold into §7.

## 6. Cleared (Legitimate) — Out of Fix Scope (26 sites)

**Legit-derived-fact (computed predicates on a Graphable-origin Subject):**

- Rule engine: `actions.go:600/684/821/1353` — stamp onto the rule's **trigger
  entity** (already exists via ENTITY_STATES watch, ADR-028). (`actions.go:1212`
  run-anchor is derived-onto-the-conjured-loop-entity; its root is §4.1.)
- Inference appliers: `applier.go:270/193/95` (`:95` rides the **entity stream**,
  not the mutation API), `hierarchy.go:239/313/368/423` — derived edges between
  pre-existing Graphable-origin entities. `anomaly.go:190`, `component.go:1256`
  are wiring-only.

**Legit-operational (does NOT target domain ENTITY_STATES):**

- `triple_mutator.go:48/93` (generic legs — verdict inherited from callers),
  `graph_writer.go:92/129` (shared transport).
- `actions.go:1398` executeDeny, `:1448` executeApprove — Subject is the **rule
  ID**, governance audit (ADR-039), never a domain entity.
- `lpa.go:567` `InferRelationshipsFromCommunities` (**no production consumer**),
  `storage.go:104` `SaveCommunity` (own `COMMUNITY_INDEX` bucket).

**Legit-external-gateway / canonical-path citizens (catalog inputs):**

- `federation.EventPayload`, `oms.Observation`, `sensorml.Asset`,
  `objectstore.StoredMessage` — all canonical-path-intended.

**Refuted/reclassified notes:**

- `graph_writer.go:759-767` (LoopHasStep) reclassified conjured→bypassed: its
  Object is the bypassed `TrajectoryStepEntity`; fix rides §4.2, not §4.1.
- The agentic-loop and agentic-tools/research derived stampers
  (`WriteSyntheticDecide`, `WriteLineageTriples`, `decide.go`, `write_todos.go`,
  `scratchpad.go`, `triplepub.go`) are **genuinely derived in shape** — they
  become clean legit-derived-facts **automatically** once §4.1 gives the loop
  entity a Graphable origin. Not independent fix targets; they are *why* §4.1 is
  the highest-leverage fix.

## 7. Fix-Shape Grouping (implementation deliberately omitted)

### Group A — "Type EXISTS, just register + change emit path" (lowest effort)

**Seam:** `agentic/payload_registry.go` + emit at `graph_writer.go:357-365`.

- **trajectory-step** (§4.2). Add `MarshalJSON`, register, publish through the
  existing loop stream. **Effort: S.**

### Group B — "Define + register Graphable payload, route through graph-ingest"

**Seam:** new Graphable type in the owning package + its `payload_registry.go`
`init()` + replace `graph.mutation.*` emit with a `BaseMessage` publish through
the stream graph-ingest consumes.

- **B1 loop-execution** (§4.1) — **highest leverage, M-L.** One type retires 4
  conjuring writers + ~8 downstream stampers in one landing. *Couples to ADR-054.*
- **B2 model-endpoint** (§4.3) — **S.** Static config, `Triples()` already factored.
- **B3 ops-diagnosis** (§4.4) — **S.** Single call site.
- **B4 web-observation** (§4.5) — **S-M.** One type, two writers, shared constructor.
- **B5 operating-model family** (§4.6) — **L.** Four types + retire the
  `agentic-memory` `graph_mutations` custom-envelope port.
- **B6 workflow-execution example** (§4.8) — **S-M**, **decision-gated**:
  Graphable vs `lifecycle.Participant`. Demo code.

### Group C — "Needs a design decision before any fix"

- **LLM-authored free facts** (§4.7) — the conjure is the *uncontrolled ID space*.
  On-by-default, so live blast radius. **Effort: design + M.**
- **Lifecycle grey zone** (§5) — **ADR-049 re-open.** ADR-049 already rejected the
  Graphable path once (CAS-revision concern); reversing has real design cost.

## 8. Open Questions / Risks for Fix-Planning

1. **ADR-054 coupling.** B1 (loop-execution) is ADR-054:90's motivating case.
   Sequence the conversion against ADR-054's indexing-eligibility phases so the
   new payload type carries the right `IndexingProfiler` envelope from day one.
2. **CAS / atomicity on the canonical path.** Several conjured writers rely on
   the mutation API's per-Subject CAS atomicity (`triple.add_batch` atomic
   batches; lifecycle `ExpectedRevision`). The Graphable→stream→graph-ingest path
   uses `MergeEntity` per-predicate merge. Confirm no loss of atomicity/ordering
   (esp. the lifecycle replace-not-append discipline, beta.103).
3. **Downstream derived-fact stampers must not regress.** ~8 sites write to the
   conjured loop Subject. After B1 they become legit-derived **only if** the
   Graphable loop entity exists in ENTITY_STATES **before** the first derived
   stamp fires — else the auto-vivify they implicitly depend on disappears and
   they start failing CAS.
4. **Operating-model dual-source (§4.6 + §4.7).** Lessons have two producers
   (deterministic `persistCompactionAsLesson` and the LLM free-fact path).
   Decide whether C collapses into the B5 Lesson shape or stays distinct.
5. **Web-observation re-population.** Content-addressed dedup means a fetch may
   re-populate an existing vertex. Confirm `MergeEntity` per-predicate merge
   preserves the one-vertex-per-canonical-URL intent.
6. **Lifecycle decision sequencing.** The grey zone blocks nothing, but if the
   ADR-049 re-open lands toward the Graphable path it defines a reusable
   `ParticipantEntity` adapter that B6 could ride — decide the lifecycle call
   before/after B6 to avoid building the projection adapter twice.
7. **Example vs framework priority.** B6 (github-pr-workflow) self-documents the
   anti-pattern (`entities.go:8-12`). Decide fix-first (reference others copy) vs
   fix-last (after the framework types it models exist).
