# Design: agent-memory-lesson-substrate

> **Reconciliation (post-adversarial-review — authoritative order: ADR-080 →
> proposal.md → tasks.md → spec delta → this file).** The Decisions below predate
> the 5-lens review fold and are STALE where they conflict with the above. Do not
> re-derive; the corrections are:
> - **Namespace/predicates:** the family is `agent.lesson.*` (ADR-080), realized as
>   contract-valid 3-part predicates (`agent.lesson.status`,
>   `agent.lesson.injection-form`, …) — NOT the flat `lesson.category` /
>   `lesson.injection_form` shorthand in Decision 2 (2-part + underscores fail
>   `ParsePredicate` and panic at registration; PR #532 canonical predicate contract).
> - **Entity ID:** `{org}.{platform}.agent.lesson.record.{uuid5}` — 6-part
>   (`org.platform`/`agent`/`lesson`/`record`/`uuid5`) — NOT the `ops.lesson.record`
>   in Decision 1. Lessons unify under `agent.*`; diagnosis stays `ops.*`.
> - **Delivery:** framework-owned deterministic brief-assembly injection at the loop
>   seam — NOT the `want:[lessons]` fusion facet in Decision 5. The facet is deferred
>   to the change that makes fusion servable; the deterministic matcher ships here and
>   is designed for that facet's later reuse.
> - **No `lesson.confidence`:** Open Question resolved to omit in v1.

## Context

ADR-080 (Proposed) records the decisions; the proposal records the why. Mechanically, this
change composes four existing seams rather than building new machinery: the `vocabulary/agentic`
predicate family + registry (where `ops.diagnosis.*` lives), the ADR-027 ops tool seam in
`processor/agentic-tools` (where `emit_diagnosis` lives), the deterministic fusion engine in
`pkg/fusion` (where the gh#533 graph facet just landed), and the canonical graph mutation API
(graph-ingest sole writer, envelope-on-create). It also deletes one package
(`processor/agentic-memory`, orphaned).

Constraints: fusion is deterministic by contract (no similarity ranking); LLM-authored
predicate text defaults rule-opaque; live-graph lifecycle never uses NATS TTL (ADR-068/073);
lessons must be distilled at chain-terminal because full episodic content expires (24h KV / 7d
stream).

## Goals / Non-Goals

Goals: the lesson artifact contract end-to-end in one PR — vocabulary, evidence-gated writer,
push delivery facet, first in-repo consumer (ops flow + e2e), orphan removal. Non-goals: see
proposal (no query tools, no separate store, no pattern-mining, no product semantics, no
sister-repo edits).

## Decisions

1. **Entity identity mirrors the diagnosis family.** Lessons mint
   `{org}.{platform}.ops.lesson.record.{uuid}` via `create_with_triples` (envelope-on-create),
   the exact shape `emit_diagnosis` uses for `{org}.{platform}.ops.diagnosis.finding.{uuid}`.
   Alternative — a per-product lesson domain segment — rejected: the framework owns the
   artifact, products scope applicability via predicates, not identity.
2. **Predicate family with an explicit rule-visibility split.** Enumerated, rule-matchable:
   `lesson.category` (closed set seeded from the semspec/semdragon convergence), `lesson.polarity`
   (`avoid` | `best_practice`), `lesson.severity` (`info` | `warning` | `critical`),
   `lesson.status` (`active` | `retired` | `superseded`). LLM-authored, registered rule-opaque
   (house default): `lesson.summary`, `lesson.detail`, `lesson.injection_form`. References,
   multi-valued: `lesson.evidence` (entity IDs, ≥1), `lesson.applies_to` (deterministic scope
   keys: entity-ID prefixes or plain tags). Attribution rides the existing
   `agent.action.executed-by` backlink + `lesson.observed_role`, as diagnosis does.
   Single-valued lifecycle fields (`lesson.status`, `lesson.superseded_by`, `lesson.retired_at`)
   are written replace-not-append.
3. **Evidence and injection-form bounds are writer-enforced rejections, not truncations.**
   `emit_lesson` rejects zero-evidence calls (mirroring `emit_diagnosis`'s ≥1-evidence rule and
   semspec's writer) and rejects `injection_form` over a fixed byte bound (default 320 bytes,
   ≈80 tokens) with an error naming the bound so the agent rewrites. Alternative — silent
   truncation — rejected: silent caps masquerade as coverage, and the bound IS the quality
   gate that keeps briefs small.
4. **Tool signature asks for intent; the backend derives structure.** Parameters: `summary`,
   `detail`, `injection_form`, `category`, `polarity`, `severity`, `evidence_entity_ids`,
   `applies_to`. Loop attribution (role, loop entity) derives from loop context via
   `TryLoopExecutionEntityID` (non-panicking; runtime executor discipline). `StopLoop: false`
   — one ops loop emits many lessons. Registered in `RegisterBuiltins` gated on NATSClient,
   beside `emit_diagnosis`.
5. **Delivery is a deterministic fusion facet, `want:[lessons]`.** Matching: a lesson is
   eligible when any `lesson.applies_to` key matches the request's declared scope
   (entity-ID-prefix or tag string match — deterministic, no similarity). Ordering: severity
   desc, then recency desc, then entity-ID tiebreak. Bounded top-K (default 10) of injection
   forms + lesson entity IDs, with matched-vs-returned counts (truncation observable, mirroring
   the graph facet). Retired/superseded lessons excluded by default. No embedding relevance in
   v1 — fusion's determinism contract governs; revisit only with evidence (measure first).
   Alternative — inject at a new framework prompt-assembly hook — rejected for v1: the facet
   is the existing governed delivery surface and products already consume facets; a dedicated
   brief-assembly hook can compose the facet later without contract change.
6. **No new payload type.** Writes ride the existing `graph.mutation.*` API exactly as
   diagnosis does (payload registry untouched); the facet extends the fusion projection schema
   → `task schema:generate` no-drift gate applies. PROV-O constants (`prov:wasDerivedFrom`,
   `prov:wasGeneratedBy`, `prov:wasAttributedTo`) join `vocabulary/standards.go`;
   `lesson.evidence` carries `StandardIRI = prov:wasDerivedFrom`, annotation only.
7. **`processor/agentic-memory` removal is a verified-orphan delete.** Pre-delete sweep: grep
   imports across the repo and every `cmd/` binary (known refs: one doc comment in
   `processor/agentic-loop/doc.go`, one dead config block in `configs/flows/deep-research.json`)
   plus the framework-change branch integration sweep (`go test -race -tags=integration ./...`).
   The AGENT_MEMORY_CHECKPOINTS bucket is created only by the component's own start; since no
   binary can construct it, no live deployment holds one — no data migration.

## Risks / Trade-offs

- [Unbounded lesson accretion (durable entities, no TTL)] → retirement/supersession predicates
  honored at read; facet K-bound caps injection; classification of lessons into the ADR-073
  retention tiers is explicitly on the gh#527 docket; volume curation is product-side by design.
- [Deterministic scope matching too crude → irrelevant lessons briefed] → narrow-by-default:
  no `applies_to` match ⇒ facet absent (never a firehose); closed category enum lets products
  partition; K small. If products later prove a relevance gap, that's measured evidence for a
  ranked upgrade — not a v1 speculation.
- [Writer rejections frustrate emitting agents] → error text is instructive (names the bound /
  the missing evidence) and `detail` carries unbounded prose; the ops persona documents the
  contract.
- [Hidden consumer of agentic-memory] → the Decision-7 sweep is a task gate before delete;
  removal is additive-PR-scoped so revert is a single commit.

## Migration Plan

Single PR, complete-system scope: vocabulary + executor + facet + ops flow/e2e + removal +
schema regen. Not breaking (nothing can instantiate the removed component; facet is opt-in
additive) — standard gates: lint, `-race` unit + integration, schema no-drift, contract tests,
ops e2e scenario green. ADR-080 flips Proposed→Accepted only after the 5-lens adversarial
review runs against this change (verdicts recorded in `adversarial-review.md`, ADR-079
precedent). Sister adoption (semteams ops re-wire, semdragon migration) is product-side,
after tag.

## Open Questions

- Injection-form bound: 320 bytes proposed; confirm against real persona budgets during
  implementation (bound is a writer constant, cheap to tune pre-tag).
- Facet K default 10: confirm with the first semteams consumer; K is request-declarable
  either way.
- Whether `lesson.confidence` (mirroring `ops.diagnosis.confidence`) earns a place in v1 or
  waits for a consumer that reads it. Default: include, optional, rule-opaque.
