# ADR-080: Push-Based Agent Memory and Evidence-Cited Lesson Artifacts

## Status

**Accepted (2026-07-19).** The 5-lens adversarial review ran against the
`agent-memory-lesson-substrate` change (verdicts and resolutions in its
`adversarial-review.md`: 5× READY-WITH-CHANGES, all findings folded), and the owner accepted
the three review-driven pivots: (1) v1 delivery at the brief-assembly seam with the fusion
facet deferred, (2) the proposed→active lesson lifecycle, (3) the `agent.lesson.*` namespace.
Companion OpenSpec change: `agent-memory-lesson-substrate`.

## Context

The ecosystem converged on the three-layer agent-memory model (episodic / semantic /
procedural, per CoALA and the 2025–26 industry consensus) once already, and the first attempt
failed — not because the model was wrong, but because the interface was: memory was exposed as
agent-invoked query tools (`search_graph`, `query_*`), agents fell back to training-corpus
habits (grep and friends) instead of calling them, and the tools were removed as friction.
That failure drove the fusion work. Meanwhile the third layer's write path — distilling
completed work into reusable guidance — was built independently twice at product level
(semdragon guild lessons, off by default; semspec lesson-decomposer/curator, stranded) and
once in the framework as the now-orphaned `processor/agentic-memory`. Two product reinventions
of one convention is the Lifecycle-harness signal: an unnamed framework responsibility.

A 2026-07-19 inventory (six repos, file:line-verified) established: episodic substrate alive
(ADR-053 run entities; full loop content expires on 24h KV / 7d stream cliffs), semantic
substrate alive (graph + fusion), the ops observation seam alive (ADR-027 Phase 1,
`emit_diagnosis` → `ops.diagnosis.*`), and no lesson primitive anywhere. Standards research
(2026-07): no adopted agent-memory schema exists (the three-tier taxonomy is converged;
schemas are per-framework, 21 counted by Mem0's survey). The mature adjacent standard is W3C
PROV(-O); `vocabulary/standards.go` already carries a full PROV-O constant section.

## Decision

1. **The graph is the memory store; the three-layer taxonomy is the model.** Episodic,
   semantic, and procedural memory all live in the existing graph + KV substrate under the KV
   twofer. No dedicated memory store, database, or subsystem is introduced — ever. A memory
   layer is a *view over authoritative state*, not a sibling store with a dual-write problem.
   Memory artifacts share the `agent.*` entity-ID domain: episodic under `agent.loop.*` /
   `agent.chain.*` (existing), procedural lessons under
   `{org}.{platform}.agent.lesson.record.{id}`. (Diagnosis stays `ops.*` — it is an
   observability artifact, not memory.)
2. **Memory is push, not pull — stated honestly.** Memory reaches agents through
   substrate-side assembly: deterministic brief construction at dispatch and, later, fusion
   facets. The framework registers **no dedicated memory search tools**; the sole
   memory-specific agent read is dereferencing a handed reference (e.g. `read_loop_result`, or
   `query_entity` on a lesson ID a brief handed over). Generic graph-read tools continue to
   exist and are governed per-role by tool allowlists — worker roles should not carry them;
   observation roles (ops) legitimately do. This is the irreversible interface lesson of the
   first circle and it binds all future memory work.
3. **Lessons are evidence-cited graph artifacts with a gated lifecycle.** The debrief edge
   produces typed `lesson.*` entities (vocabulary in `vocabulary/agentic`, beside
   `ops.diagnosis.*`). Contract essentials: at least one evidence citation
   (well-formed-entity-ID-gated at emit; **existence-resolved at promotion**); rich
   `lesson.detail` stored separately from a byte-bounded `lesson.injection_form` (over-bound
   is rejected instructively, never truncated); typed `applies_to` scope keys
   (`id:<prefix ≥3 segments, segment-boundary matched>` / `tag:<token>`, ≥1 required);
   **content-derived identity** (UUIDv5 over category + scope + summary + evidence — re-emitting
   the identical lesson cannot mint a second entity); and lifecycle status. **Lessons are born
   `proposed`; only `active` lessons are injectable.** Promotion and retirement ride the
   canonical replace lane (`update_with_triples` via rule `replace_owned` or a product
   curation writer, ADR-056); the default promotion gate is operator/product review, with
   auto-promotion available only as explicit product config (ADR-027 Phase-2 philosophy). This
   one lifecycle answers three review findings at once: nothing LLM-authored shapes another
   agent's behavior without a gate (ADR-026/027 posture preserved), the retirement writer is
   named, and durable LLM text is reviewed before it recirculates.
4. **The debrief officer is the ops role; triggering is honest about what ships.** Lesson
   emission extends the ADR-027 observation seam — an `emit_lesson` sibling of
   `emit_diagnosis` (StopLoop:false, per-loop emission cap). The framework's reference ops
   flow fires per loop completion (`agent.complete.*`); **chain-terminal triggering is product
   rule-pack work** (semteams' ops pack), made safe at per-completion granularity by
   content-derived identity (duplicates collapse) and the proposed-status gate. Distillation
   still happens while full episodic content exists — that is why debrief is event-driven, not
   batch.
5. **v1 delivery is the brief-assembly seam; the fusion facet is deferred, not dropped.**
   The lens-fusion Engine currently has no production consumer or serving surface (verified:
   constructed in tests only; even `want:[graph]` is undeployed), and its retrieval model is
   query-seed-driven — the wrong shape for scope-keyed lesson retrieval. So v1 ships a
   framework-owned, deterministic **lesson injection step at loop brief assembly**: match
   active lessons' `applies_to` against the loop's scope, order by severity → stored emit
   timestamp (replay-stable) → ID, cap at K (≤25, default 10) and a total-bytes bound, render
   injection forms with their entity IDs. A `want:[lessons]` facet lands in the change that
   makes fusion servable, reusing the same matcher.
6. **Vocabulary posture: align by annotation, adopt no third-party schema.** Internal
   predicates stay dotted house-style under the namespace authority and predicate contract.
   `lesson.evidence` registers with `WithIRI(vocabulary.ProvWasDerivedFrom)` — the PROV-O
   constants **already exist** in `vocabulary/standards.go`; the `StandardIRI` field/option
   live in `vocabulary/predicates.go`/`registry.go`. LLM-authored text predicates are
   registered rule-opaque by house convention (explicit `WithRuleOpaque(true)`); enumerable
   fields stay rule-visible. **`lesson.category` is an open predicate** — the framework ships
   no closed category set (Product Boundary: categories are product vocabulary). No memory
   framework's schema (Mem0, Letta, Zep, POLE+O) is adopted; OASF stays tracked only for
   capability descriptors. Revisit if a genuine memory-interop standard reaches adoption.
7. **Framework owns the contract; products own the semantics.** semstreams owns the lesson
   vocabulary and writer, the ops emission seam, the brief-assembly injection step, and the
   episodic-retention interaction (folded into the ADR-073/gh#527 track). Products own
   category taxonomies, promotion/curation policy, scope conventions, and chain-terminal
   trigger rules. `processor/agentic-memory` is retired as code — its event-driven hydration
   *shape* was right and returns as the brief-assembly injection step; its dead context-event
   publish leg in `processor/agentic-loop/component.go` is pruned with the removal.

## Consequences

- Rejected permanently: dedicated agent-invoked memory search tools (tried; failed on
  friction), a separate memory store (KV-twofer violation), automated pattern-mining beyond
  evidence-cited distillation (unproven industry-wide), and ungated injection of LLM-authored
  guidance into briefs (ADR-026/027 posture).
- semteams' ops pack becomes the first product consumer (tracked via an upstream issue filed
  by the change); semdragon's guild-lessons loop migrates onto the primitive during its rules
  migration; semspec/semmem are mined for design and retired.
- The 24h/7d episodic retention cliffs become an explicit contract: what must outlive them is
  exactly what debrief distills into durable `agent.lesson.*` artifacts.
- The lessons fusion facet, chain-terminal trigger rules, and any relevance-ranked (vs
  deterministic) selection are explicitly future work with named owners, not silent scope.
