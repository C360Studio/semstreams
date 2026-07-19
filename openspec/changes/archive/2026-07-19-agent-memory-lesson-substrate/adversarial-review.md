# Adversarial Review — ADR-080 / agent-memory-lesson-substrate

5-lens review (architect, adversarial-breaker, implementation-feasibility, code-accuracy,
completeness), 2026-07-19, run against commit `45447020` (the initial drafts). Every lens read
the code seams the drafts claim to extend. Blocking/high findings re-verified against source
before counting (verify pass at bottom). Lens verdicts: **5× READY-WITH-CHANGES, 0× BROKEN** —
the core thesis (graph as memory store, push-not-pull, evidence-cited lessons, ops seam,
annotation-only vocabulary) held in every lens; the drafts' *mechanisms* did not all survive.

## Upheld findings and their resolutions

| # | Finding (lens) | Sev | Resolution folded into drafts |
|---|---|---|---|
| 1 | Fusion delivery unbuildable as framed: lens Engine is seed-driven (misses before facets on zero seeds), `RetrievalClient` has no by-predicate retrieval, `applies_to` is not an EdgeSpec, K/tags have no request surface — and the Engine is constructed in tests only; no served fusion surface exists (even `want:[graph]` is undeployed); prompt assembler composes static fragments, loop consumes no fusion and no `agent.context.injected.*` (feasibility F1/F5, architect F2) | BLOCKING | **Delivery re-scoped.** v1 delivers via a framework-owned deterministic brief-assembly injection step at the loop seam (new, small, honest). The `want:[lessons]` fusion facet is deferred to the change that makes fusion servable at all; the fusion spec delta is **dropped from this change** |
| 2 | Lessons bypass the ADR-026/027 approval model: behavior-shaping LLM prose lands in future briefs with no human gate, unlike passive diagnosis (architect F3); durable LLM text also bypasses the governance processors (completeness G3); retirement/`lesson.status` has no writer (completeness G1, breaker F5b, architect F6) | HIGH ×3 | **One mechanism resolves all three: lessons are born `proposed` and only `active` lessons are injectable.** Promotion/retirement is the named curation lane (`replace_owned` rule action or product writer via `update_with_triples`, ADR-056); default is operator/product review; auto-promotion is explicit config opt-in (ops Phase-2 philosophy). Evidence existence is validated at promotion |
| 3 | Push-only absolute is false: ops allowlist carries `query_entity`/`query_entities`/`query_relationships` which read any entity; the injection-form "quality gate" is one `query_entity(lessonID)` from unbounded `lesson.detail`; the cited ADR-028 "only ops read the graph" phrase does not exist (architect F1) | HIGH | ADR Decision 2 reworded to what is enforceable: no *dedicated* memory search tools; generic graph reads stay governed by per-role allowlists; worker briefs carry the bounded form, deliberate dereference by read-capable roles is allowed and named |
| 4 | Evidence validated for shape/count only, never existence — dangling/self/fabricated citations satisfy "evidence-cited" while carrying `prov:wasDerivedFrom` (breaker F2) | HIGH | Emit keeps the cheap well-formed-entity-ID gate; **existence resolution moves to the promotion gate** (proposed→active), where trust is actually granted |
| 5 | "Recency" unpinned: `UpdatedAt`/KV revision re-stamp on ADR-073 reingest → identical logical state reorders (breaker F4) | HIGH | Ordering pinned to the stored emit-time triple timestamp (the only replay-stable field); spec names it |
| 6 | `applies_to` grammar broken: empty ⇒ silently undeliverable forever; bare-org prefix ⇒ matches every brief (reverse firehose); prefix vs tag has no discriminator; `HasPrefix` isn't segment-boundary-aware (breaker F3a/b/c) | HIGH | Typed key grammar `id:<prefix>` / `tag:<token>`; ≥1 key required at emit; id-prefixes require ≥3 segments and match on segment boundaries only |
| 7 | No dedup: fresh UUID per call + per-completion ops firing ⇒ duplicate lesson accretion (completeness G2, breaker F1); no chain-terminal trigger exists — ops fires on every `agent.complete.*` hop; e2e closes the loop with a manual poke (completeness G8) | HIGH ×2 | Lesson entity IDs become **content-derived (UUIDv5 over category+applies_to+summary+evidence)** — re-emitting the identical lesson re-derives the same ID, no second entity. ADR language downgraded honestly: framework ships the write primitive + per-completion reference trigger; chain-terminal triggering is product rule-pack work (semteams). Per-loop emission cap added (default 20) |
| 8 | PROV-O constants already exist (`standards.go:226/231/253` + full section 156–399); "add constants" is stale in 4 docs; `StandardIRI` mechanism lives in `predicates.go`/`registry.go`, not standards.go (code-accuracy #4, feasibility F3) | HIGH | All four docs corrected; task recast to "register `lesson.evidence` with `WithIRI(vocabulary.ProvWasDerivedFrom)`" |
| 9 | Orphan-sweep enumeration false: also `agentic-loop/component.go:1575` (live context-event publish leg for the deleted consumer), multiple `doc.go` lines, coincidentally-named `config/rules/agentic-memory/` rulepack (4 files, unrelated — triage note), AGENTS.md/CLAUDE.md tables, docs/basics/07, docs/concepts/13, agentic-loop README, ROADMAP, ADR-043 (all lenses) | HIGH | Decision-7 enumeration corrected; the dead context-event publish leg is pruned with the removal; docs list itemized in tasks |
| 10 | `task schema:generate` claim empty: no fusion projection schema exists in `schemas/`; the lens `Response` is never reflected (feasibility F4) | MED | Claim removed with the facet deferral; contract coverage via unit tests |
| 11 | K request-declarable with no clamp diverges from graph facet's fixed caps; count-only bound (breaker F6) | MED | Fixed framework ceiling (K ≤ 25, default 10) + total injected-bytes bound (8 KiB) on the brief fragment |
| 12 | Closed `lesson.category` enum "seeded from semspec/semdragon" contradicts the Product Boundary and the change's own non-goals (architect F4) | HIGH | `lesson.category` is an **open** rule-visible predicate; framework ships no closed value set; example taxonomies live in docs only |
| 13 | `ops.lesson.*` fragments the memory namespace (episodic under `agent.*`; semsource-authored lessons aren't ops artifacts); envelope Domain=`agentic` vs ID-domain `ops` divergence (architect F5) | MED | Entity stem changed to `{org}.{platform}.agent.lesson.record.{uuid5}` — memory artifacts unify under `agent.*`; diagnosis stays `ops.*` (it is an observability artifact); ops remains the emitting seam |
| 14 | Minor accuracy: `agent.complete.*` 7d MaxAge governed by `config/streams.go:390` (derived AGENT stream default), not `natsclient/stream.go:109`; "defaults rule-opaque" is convention (explicit `WithRuleOpaque(true)`), not registry behavior; "exactly as diagnosis does" holds for create only — lifecycle updates need `update_with_triples`/`replace_owned` which diagnosis never uses (code-accuracy #3/#5/#9) | MED | Citations and wording corrected; the writer's publisher interface scoped to include the update verb |
| 15 | semteams "first consumer" is prose-only — no tracked handoff artifact (completeness G7); no wire-level integration test for the executor (G5); no emission/facet observability counters (G4); empty-`applies_to` footgun (breaker F3a — subsumed by #6) | MED | Tasks added: file the semteams upstream issue; integration-tier test through the production tool wire; rejection/injection counters |

## Refuted / narrowed claims (the verify step working as intended)

- **Breaker F7 (partial):** "`configs/flows/deep-research.json` **is** a loaded config (deep-research e2e tier)" — REFUTED by code-accuracy + compose file: `docker/compose/deep-research.yml:71` loads `deep-research-test.json`, which carries **no** agentic-memory block; the non-test file is referenced only by a unit test. The original "no binary loads a config with the block" claim stands. (The wider sweep-enumeration finding still holds.)
- **Architect F1 (one prong):** the *narrow* spec scenario "no dedicated lesson search/list/query tool" was always satisfiable; what broke was ADR Decision 2's absolute phrasing. Requirement retained, decision reworded.
- **Breaker F5a:** retirement-transition atomicity HOLDS when the `update_with_triples` lane is used (single CAS put) — confirmed, not a defect; the finding correctly narrowed to "the lane must be named," which #2 resolves.

## Verify pass (blocking/high spot-checks against source, this session)

1. `fusion.NewEngine` constructed outside `_test.go`: **zero hits** — Engine unwired in production. CONFIRMED.
2. `agentic-loop` non-test references to `context.injected`/`ContextInjected`: **zero hits** — no existing injection seam; v1 delivery step is genuinely new work. CONFIRMED.
3. `emit_diagnosis.go:318-336`: evidence parse is non-empty `[]string` shape check only. CONFIRMED.
4. `configs/flows/ops-agent.json:75`: dispatch consumes `agent.complete.*` (per-completion, not chain-terminal). CONFIRMED.
5. `vocabulary/standards.go:226/231/253`: `ProvWasAttributedTo`/`ProvWasDerivedFrom`/`ProvWasGeneratedBy` exist verbatim. CONFIRMED.
6. `configs/flows/ops-agent.json:145-147`: `query_entity`/`query_entities`/`query_relationships` in the ops allowlist. CONFIRMED.

## Verdict

**READY-WITH-CHANGES, all changes folded** (this commit). The three structural pivots —
delivery re-scope to the brief-assembly seam with the fusion facet deferred, the
proposed→active promotion lifecycle, and the `agent.lesson.*` namespace — are recorded as
owner-decision items in the ADR; ADR-080 stays **Proposed** until the owner accepts them.
