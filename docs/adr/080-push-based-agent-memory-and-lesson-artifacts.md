# ADR-080: Push-Based Agent Memory and Evidence-Cited Lesson Artifacts

## Status

**Proposed (2026-07-19).** Pending the 5-lens adversarial review required for framework ADRs
before Accept. Companion OpenSpec change: `agent-memory-lesson-substrate` (carries the
mechanics; this ADR records only the decisions).

## Context

The ecosystem converged on the three-layer agent-memory model (episodic / semantic /
procedural, per CoALA and the 2025–26 industry consensus) once already, and the first attempt
failed — not because the model was wrong, but because the interface was: memory was exposed as
agent-invoked query tools (`search_graph`, `query_*`), agents fell back to training-corpus
habits (grep and friends) instead of calling them, and the tools were removed as friction.
That failure drove the fusion work (declaration-driven `want:[...]` facets, `code_context`,
graph facet). Meanwhile the third layer's write path — distilling completed work into reusable
guidance — was built independently twice at product level (semdragon guild lessons, off by
default; semspec lesson-decomposer/curator, stranded in a legacy repo) and once in the
framework as the now-orphaned `processor/agentic-memory` (event-driven hydration built
pre-fusion on raw graph queries). Two product reinventions of one convention is the
Lifecycle-harness signal: an unnamed framework responsibility.

A 2026-07-19 inventory (six repos, file:line-verified) established: episodic substrate is
alive (ADR-053 run entities, `AGENT_LOOPS`/trajectory metadata, 24h/7d retention cliffs on
full content), semantic substrate is alive (graph + fusion), the ops observation seam is alive
(ADR-027 Phase 1, `emit_diagnosis` → `ops.diagnosis.*`), and no lesson primitive exists
anywhere in the framework.

Standards research (2026-07): no adopted vocabulary standard for agent memory exists — the
three-tier taxonomy is converged but schemas are per-framework (Mem0's survey counts 21).
The one mature adjacent standard is W3C PROV(-O) (Recommendation since 2013; PROV-AGENT
extends it for agentic workflows, emerging 2025–26). OASF covers agent capability
descriptors, not memory.

## Decision

1. **The graph is the memory store; the three-layer taxonomy is the model.** Episodic
   (run/loop/trajectory facts), semantic (domain triples), and procedural (personas, rules,
   lessons) memory all live in the existing graph + KV substrate under the KV twofer. No
   dedicated memory store, database, or subsystem is introduced — ever. A memory layer is a
   *view over authoritative state*, not a sibling store with a dual-write problem.
2. **Memory is push, not pull.** Memory reaches agents exclusively through substrate-side
   assembly: fusion facets and deterministic prompt/brief construction at dispatch,
   compaction, and wake-up seams. Agents are never given memory *search* tools; the sole
   agent-initiated memory read is dereferencing a handed reference (e.g. `read_loop_result`
   with a rule-supplied loop ID). This is the irreversible interface lesson of the first
   circle and it binds all future memory work.
3. **Lessons are first-class, evidence-cited graph artifacts.** The procedural write-back
   edge (debrief) produces typed `lesson.*` entities in `vocabulary/agentic`, beside
   `ops.diagnosis.*`. Contract essentials, lifted from the semspec/semdragon convergence:
   evidence citations are mandatory (evidence-free lessons are rejected at the writer), rich
   detail is stored separately from a compressed injection form (small, bounded, brief-ready),
   and lessons carry retirement metadata so stale guidance expires instead of accreting.
   Distillation passes through a quality gate; it is never inferred by similarity search alone.
4. **The debrief officer is the ops role.** Lesson emission extends the existing ADR-027
   observation seam (an `emit_lesson` sibling of `emit_diagnosis`), fired event-driven at
   chain-terminal — which is also what beats the episodic retention cliffs: distillation
   happens while full loop content still exists, not as a batch job over expired history.
   Diagnosis (finding about system behavior) and lesson (distilled guidance for future
   briefs) remain distinct artifact types on one seam.
5. **Vocabulary posture: align by annotation, adopt no third-party schema.** Internal
   predicates stay dotted house-style under the namespace authority and predicate contract.
   Where a `lesson.*`/memory predicate is semantically equivalent to a W3C PROV-O term
   (derivation, generation, attribution), it carries the existing `StandardIRI` annotation —
   the established standards-grounding mechanism in `vocabulary/standards.go` — for export
   and interoperability. No memory framework's schema (Mem0, Letta, Zep, POLE+O) is adopted:
   none is a standard and the domain is young. OASF remains tracked only where it already
   applies (capability descriptors). Revisit if a genuine memory-interop standard reaches
   adoption; alignment-by-annotation keeps that migration cheap.
6. **Framework owns the contract; products own the semantics.** semstreams owns the lesson
   vocabulary and writer contract, the ops emission seam, the fusion-facet/brief-assembly
   delivery path, and the episodic retention decision (folded into the ADR-073/gh#527
   retention work). Products own lesson categories, curation policy, and which briefs get
   which lessons. `processor/agentic-memory` is retired as code — its event-driven hydration
   *seam* (post-compaction, pre-task) was the right shape and is re-expressed as a fusion
   consumer, not revived as a component.

## Consequences

- Rejected permanently: agent-invoked memory query tools (tried; failed on friction), a
  separate memory store (KV-twofer violation), and automated pattern-mining beyond
  evidence-cited distillation (unproven industry-wide; revisit on evidence).
- semteams' dormant ops pack becomes the first consumer (its stale `reviewer-qa` trigger gets
  fixed as part of adoption, not before); semdragon's guild-lessons loop migrates onto the
  primitive during its in-flight rules migration; semspec/semmem implementations are mined
  for design and retired.
- The 24h/7d episodic retention cliffs become an explicit, intentional contract: what must
  outlive them is exactly what debrief distills into durable graph artifacts.
- PROV-O constants are added to `vocabulary/standards.go` (annotation-only; no RDF machinery).
