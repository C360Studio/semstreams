# Proposal: agent-memory-lesson-substrate

## Why

The three-layer agent-memory model (episodic / semantic / procedural) already exists in this
ecosystem except for one edge: nothing distills completed work into durable, reusable guidance.
That edge has been built twice at product level (semdragon guild lessons, off by default;
semspec lesson-decomposer/curator, stranded) — the classic unnamed-framework-responsibility
signal — while the framework's episodic raw material expires on 24h/7d retention cliffs before
anything learns from it. ADR-080 records the decisions (push-based delivery, evidence-cited
lessons with a gated lifecycle, ops as debrief officer, align-by-annotation vocabulary
posture); this change carries the mechanics. Now, because the contracts being set (predicate
vocabulary, identity scheme, lifecycle, injection bounds) are cheap in beta and expensive
after 1.0, and semdragon's in-flight rules migration will otherwise produce a third
per-product reinvention. The 5-lens adversarial review has run; all findings are folded
(`adversarial-review.md`).

## What Changes

- New `lesson.*` predicate family in `vocabulary/agentic` (beside `ops.diagnosis.*`):
  evidence citations, `detail`/`injection_form` split, typed `applies_to` scope keys
  (`id:`/`tag:` grammar), open `category`, enums for polarity/severity/status, lifecycle
  fields. `lesson.evidence` registers with `WithIRI(vocabulary.ProvWasDerivedFrom)` — the
  PROV-O constants already exist in `vocabulary/standards.go`; this is annotation only.
- New `emit_lesson` tool executor in `processor/agentic-tools` — sibling of `emit_diagnosis`
  on the ADR-027 ops seam. Writer gates: ≥1 well-formed evidence entity ID, injection-form
  byte bound (reject, never truncate), ≥1 typed `applies_to` key, per-loop emission cap.
  **Content-derived entity identity** (UUIDv5) makes re-emission idempotent. Lessons mint
  `{org}.{platform}.agent.lesson.record.{id}` via the canonical mutation API, born
  `status=proposed`.
- **Gated lifecycle**: only `active` lessons are injectable. Promotion (with evidence-existence
  resolution) and retirement ride the canonical replace lane (`update_with_triples` via rule
  `replace_owned` or a product curation writer); auto-promotion is explicit product config.
- **v1 delivery = brief-assembly injection** (framework-owned, deterministic, bounded): at
  loop dispatch, active lessons matching the loop's scope are rendered into the brief
  (severity → stored emit-timestamp → ID ordering; K ≤ 25 default 10; total-byte bound;
  entity IDs included for governed dereference). The `want:[lessons]` fusion facet is
  **deferred** to the change that gives fusion a serving surface (the lens Engine currently
  has no production consumer); it will reuse this matcher.
- Ops reference flow (`configs/flows/ops-agent.json` + `-test.json`) and the ops e2e extend
  to the full loop: emit → proposed entity → promote → next loop's brief carries the
  injection form (hard-fail assertions; first in-repo consumer).
- Removal: `processor/agentic-memory` (verified orphan — factory never registered; no
  loadable config carries a live block; sweep enumeration corrected per review) plus the dead
  context-event publish leg in `processor/agentic-loop/component.go` that fed it, and the
  stale component-table/doc references. Not **BREAKING**: nothing can instantiate it.
- Episodic retention interaction made explicit: debrief fires event-driven per loop
  completion (chain-terminal triggering is product rule-pack work), so distillation happens
  while full loop content (AGENT_LOOPS 24h, `agent.complete.*` 7d) still exists.
  Retention-tier changes themselves stay in the gh#527 (ADR-073) track.

## Capabilities

### New Capabilities

- `agentic-lessons`: the lesson artifact contract — vocabulary and entity shape,
  evidence-gated idempotent write via `emit_lesson` on the ops seam, the proposed→active→
  retired lifecycle and its curation lane, and push-only bounded delivery at brief assembly
  (no dedicated memory search tools).

### Modified Capabilities

(none — the fusion facet delta was removed from this change by the adversarial review; it
returns with the change that makes fusion servable)

## Impact

- Code: `vocabulary/agentic` (predicates + registration), `processor/agentic-tools` (new
  executor + registration), `processor/agentic-loop` (brief-assembly injection step; prune
  dead context-event publish leg), `configs/flows/ops-agent*.json`,
  `test/e2e/scenarios/ops`, removal of `processor/agentic-memory`, docs/component tables.
- Consumers: **semteams** first (re-wires its dormant ops pack; chain-terminal trigger rules;
  a tracked upstream issue is filed by this change); **semdragon** migrates guild-lessons
  during its rules migration; **semsource** is a candidate later producer (extraction-
  correction lessons). semspec/semmem are mined for design and retired, never revived.
- Relationship to in-flight work: complements gh#527 retention Increment-0; no overlap with
  the gh#161/#576 breaking wave; the deferred fusion facet lands with future fusion-serving
  work.

## Non-goals

- No dedicated agent-invoked memory search/query tools (ADR-080 decision 2; generic
  graph-read tools remain per-role-allowlisted, worker roles excluded by convention).
- No separate memory store or subsystem — the graph + KV twofer is the memory store.
- No `want:[lessons]` fusion facet in this change (deferred with the fusion serving surface;
  the matcher ships here and is reused there).
- No ungated injection: no lesson reaches a brief without passing the promotion gate.
- No automated pattern-mining beyond evidence-cited lessons through the ops quality gate.
- No framework-owned category taxonomy (`lesson.category` is open; taxonomies are product
  vocabulary), no product curation policies, no chain-terminal trigger rules (product
  rule-pack work), no semteams/semdragon repo changes.
- No revival of `processor/agentic-memory` or its LLM extraction path.
- No third-party memory schema adoption; no RDF/PROV machinery beyond `StandardIRI`
  annotations.
