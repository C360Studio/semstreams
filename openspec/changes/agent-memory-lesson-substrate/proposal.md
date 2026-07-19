# Proposal: agent-memory-lesson-substrate

## Why

The three-layer agent-memory model (episodic / semantic / procedural) already exists in this
ecosystem except for one edge: nothing distills completed work into durable, reusable guidance.
That edge has been built twice at product level (semdragon guild lessons, off by default;
semspec lesson-decomposer/curator, stranded) — the classic unnamed-framework-responsibility
signal — while the framework's episodic raw material expires on 24h/7d retention cliffs before
anything learns from it. ADR-080 records the decisions (push-based delivery, evidence-cited
lessons, ops as debrief officer, align-by-annotation vocabulary posture); this change carries
the mechanics. Now, because the contracts being set (predicate vocabulary, facet shape,
retention interaction) are cheap in beta and expensive after 1.0, and semdragon's in-flight
rules migration will otherwise produce a third per-product reinvention.

## What Changes

- New `lesson.*` predicate family in `vocabulary/agentic` (beside `ops.diagnosis.*`):
  evidence-citation, storage/injection split (rich detail + bounded injection form),
  provenance, and retirement metadata. PROV-O `StandardIRI` annotations where terms align;
  PROV-O constants added to `vocabulary/standards.go` (annotation only, no RDF machinery).
- New `emit_lesson` tool executor in `processor/agentic-tools` — sibling of `emit_diagnosis`
  on the ADR-027 ops seam. Evidence-free lessons are rejected at the writer. Lessons mint
  first-class graph entities via the canonical mutation API (graph-ingest remains sole
  ENTITY_STATES writer; semantic envelope per ADR-055).
- Lessons delivery via push only: a fusion facet (`want:[lessons]`, relevance-filtered,
  bounded top-K of injection forms) consumable at prompt/brief-assembly seams. No
  agent-invoked lesson search tool exists or will.
- Ops reference flow (`configs/flows/ops-agent.json`) and the ops e2e scenario extend to
  exercise emit → store → facet-retrieve round-trip (first in-repo consumer; complete-system
  PR discipline).
- Removal: `processor/agentic-memory` (orphaned — `Register()` never called, no loadable
  config references it, extraction path dormant per gh#317). Not **BREAKING**: no binary can
  currently instantiate it. Its post-compaction hydration seam is recorded as a future fusion
  consumer, not carried by this change.
- Episodic retention interaction made explicit: debrief fires event-driven at chain-terminal
  so distillation happens while full loop content (AGENT_LOOPS 24h, `agent.complete.*` 7d)
  still exists. Retention-tier changes themselves stay in the gh#527 (ADR-073) track.

## Capabilities

### New Capabilities

- `agentic-lessons`: the lesson artifact contract — `lesson.*` vocabulary and entity shape,
  evidence-required write semantics via `emit_lesson` on the ops seam, injection-form bounds,
  retirement metadata, and the push-only delivery rule (facet/brief injection; no agent
  memory-search tools).

### Modified Capabilities

- `fusion`: adds the lessons facet — declaration-driven retrieval of bounded, relevance-ranked
  lesson injection forms as a governed facet (same admission discipline as the graph facet,
  gh#533: declaration-driven classification, absent-not-fabricated evidence).

## Impact

- Code: `vocabulary/agentic` (+`vocabulary/standards.go` PROV-O constants),
  `processor/agentic-tools` (new executor + registration), `pkg/fusion` (lessons facet),
  `configs/flows/ops-agent.json` + `test/e2e/scenarios/ops`, removal of
  `processor/agentic-memory`.
- Schema/registry: new payload/predicate registrations → `task schema:generate` no-drift gate.
- Consumers: **semteams** first (re-wires its dormant ops pack against the primitive; fixes
  its stale `reviewer-qa` trigger on adoption); **semdragon** migrates guild-lessons onto it
  during its rules migration; **semsource** is a candidate second producer/consumer
  (extraction-correction lessons) but is not required by this change; semspec/semmem are
  mined for design and retired, never revived.
- Relationship to in-flight work: complements gh#527 retention Increment-0 (this change
  defines *what must outlive* the episodic cliffs; #527 defines the tiers); no overlap with
  the gh#161/#576 breaking wave.

## Non-goals

- No agent-invoked memory query/search tools (the retired `search_graph`/`query_*` friction
  path; ADR-080 decision 2 binds).
- No separate memory store or subsystem — the graph + KV twofer is the memory store.
- No automated pattern-mining/distillation beyond evidence-cited lessons through the ops
  quality gate (unproven industry-wide; revisit on evidence).
- No product semantics: lesson categories, curation policy, and which briefs receive which
  lessons belong to products (semteams/semdragon/semsource), not the framework.
- No revival of `processor/agentic-memory` or its LLM extraction path; no re-implementation
  of its hydration component (seam noted for a future fusion consumer).
- No third-party memory schema adoption (Mem0/Letta/Zep/POLE+O — none is a standard); no
  full RDF/PROV machinery — `StandardIRI` annotations only.
- No semteams/semdragon repo changes in this change (their adoption is product-side work).
