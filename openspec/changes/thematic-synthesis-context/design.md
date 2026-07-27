# Design — thematic-synthesis-context

## Context (grounded in code + corpus)

The eval hits the **auto-summarize branch** of `handleStrategyGraphRAG`
(`processor/graph-query/graphrag.go:729-773`; `SummarizeThreshold=50`, `graphrag.go:52`).
That branch computes `semanticScores map[string]float64` (per-entity query relevance,
`graphrag.go:739-742`) but currently spends it only on the flat `EntityDigest.Relevance`
field. The synthesis prompt (`answer.go:buildAnswerPrompt`, 256-299) feeds the model, per
community: `Summary`, up to 5 `Representatives` (`Label [Type]`), up to 5 `Keywords`. The
`.Entities` digests come from `enrichCommunitySummaries` (`graphrag.go:1625`), which pulls
`comm.RepEntities` — **PageRank, query-agnostic**, `MaxRepEntities=5`
(`graph/clustering/summarizer.go:55`).

Decisive corpus fact: **tags are triples** (predicate `content.classification.tag`), while
body/description are ObjectStore-only and absent from the loaded `EntityState`. All three
missed terms are in tags — `maint-008` `[forklift,battery,…]`, `maint-009` `[dock,door,…]`,
`doc-emergency-001` `[emergency,safety,evacuation,…]` — so tags recover them with **zero**
ObjectStore fetches. `EntityDigest`/`GlobalSearchResponse` are not in `schemas/` or `specs/`,
so adding a field does not trip the schema-gen CI gate.

## Two coupled levers (both required)

Neither alone gets all three terms: `evacuation` has no title (needs tags); the tag-bearing
members are not PageRank reps (need query-relevant selection). Lever 3 from the brief (richer
summaries) is rejected — summaries are generated query-agnostically at clustering time under a
200-token budget and cannot be made query-focused on the read path.

### Lever A — select digest entities by query relevance
Source the per-community digest entities from the community's **members ranked by
`semanticScores`** (desc), capped at `MaxQueryFocusedReps`, backfilled from PageRank
`RepEntities` to reach the cap when scored members are thin, and falling back **entirely** to
`RepEntities` when `semanticScores` is nil/empty. This guarantees no rep slot is ever lost
versus today and preserves the tiered graceful-fallback floor.

### Lever B — capped tags line per digest
`EntityDigest` gains `Tags []string`, populated from the entity's `content.classification.tag`
triples (already loaded), capped at `MaxDigestTags`. Rendered on the `Representatives:` line in
both `buildAnswerPrompt` and the `synthesizeAnswer` template floor.

## Precise touch points

`processor/graph-query/graphrag.go`
- `EntityDigest` (line ~187): add `Tags []string \`json:"tags,omitempty"\``.
- New consts/vars near `labelPredicates` (~1531):
  - `MaxQueryFocusedReps = 5` (mirror `MaxRepEntities`; keeps prompt scale identical to today's
    rep channel — no new magic number).
  - `MaxDigestTags = 6` (corpus entities carry 4 tags; 6 gives headroom and bounds a
    pathological entity).
  - `digestTagPredicates = []string{"content.classification.tag"}` (sibling to `labelPredicates`;
    only the tag predicate — it carries all three targets; category/subject adds noise).
- `enrichCommunitySummaries` (1625): new signature
  `enrichCommunitySummaries(ctx, summaries, semanticScores map[string]float64)`. Per summary,
  compute `selectDigestEntities(comm, semanticScores)` (Lever A), **batch-load the selected IDs
  once and keep `[]*EntityState`** (not the label-only `resolveEntityLabels` indirection) so
  `Label = resolveLabel(e)` and `Tags = collectDigestTags(e, MaxDigestTags)` come from one pass.
- `handleStrategyGraphRAG` (~745): pass `semanticScores` into the enrichment call.
- `enrichGlobalResponse` (~1730): thread scores where available (the `else`-branch caller at
  ~791 has `semanticHits` in scope — build the same score map there); pass `nil` on the
  text-fallback path so PageRank behavior is unchanged.
- `synthesizeAnswer` template (~1759-1779): mirror tags on the `Representatives:` line.
- New helper `collectDigestTags(entity *gtypes.EntityState, max int) []string`: iterate
  `entity.Triples`, collect string `Object`s whose `Predicate ∈ digestTagPredicates`, dedupe,
  cap `max`. (`GetPropertyValue` returns only the first match; tags are multi-valued → iterate
  triples directly.)

`processor/graph-query/answer.go`
- `buildAnswerPrompt` (278-284): render per-rep as `Label [Type] {tags: t1, t2, …}` when
  `len(e.Tags) > 0`.

Only new threaded input: `semanticScores` (already built in `handleStrategyGraphRAG`) flowing
into `enrichCommunitySummaries`. Everything else (`comm.Members`, `RepEntities`, loaded
`EntityState.Triples`) is already in hand.

## Scale / token bound

Prompt growth ≤ `MaxQueryFocusedReps (5)` × `MaxAnswerClusters (5)` × (label + `MaxDigestTags (6)`)
— **independent of community size**. A 10,000-member community still contributes exactly 5
digests. ~26 tok/rep → ~130 tok/community → ~650 tok of rep+tag content across 5 clusters, on
top of ~1,000 tok of summaries ≈ 1.7–2K-token prompt, well inside the answer models' context.
The 500-token answer **response** cap (`answerSynthesisMaxTokens`) is untouched.

## Testing

- **Unit**: `answer_test.go` — `buildAnswerPrompt` renders a tags suffix when present, omits it
  when empty. `graphrag_test.go` — `collectDigestTags` (multi-valued, dedupe, cap);
  `selectDigestEntities` (query-relevant ordering; backfill to cap; full PageRank fallback on
  nil scores; no rep slot lost). Template `synthesizeAnswer` mirrors tags.
- **Regression guard (built into the eval)**: recall is scored across all 5 fixtures, so a
  query-relevant selection that dropped a currently-present term (e.g. `hydraulic`) surfaces as a
  new missing term elsewhere. Mitigation already in the design: PageRank backfill keeps rep
  slots, and the community-wide `Keywords` channel independently carries structural terms.
- **E2E validation (single run)**: `task e2e:semantic:frontier` (Gemini 2.5 Flash for
  `answer_synthesis`; `GEMINI_API_KEY` from `../semdev/.env`). Frontier over local 8b: a strong
  answer model echoes the enriched context faithfully AND avoids the local dual-8B macOS
  memory-pressure SIGTERM recorded in prior runs. `validate_thematic_eval.go` prints the
  confirm-trace directly.

## Validation success criteria

- `thematic_theme_recall_mean` rises from **0.85** and clears it decisively toward ~1.0 (three
  questions move 0.75 → 1.0).
- Per-question `theme_terms_missing` loses exactly: `forklift-maintenance` → no longer `battery`
  (title + tag); `dock-equipment` → no longer `door` (title + tag); `fire-emergency` → no longer
  `evacuation` (tag only — the discriminating proof that Lever B works independent of titles).
- No fixture gains a newly-missing term (regression guard).

## Ceremony

Small OpenSpec change on the `graph-query` capability (spec was silent on answer synthesis →
lazy seed on first touch). **No ADR** — reversible internal synthesis-context enrichment, no
irreversible choice, no cross-repo contract. Implementation routes through
`semstreams-developer` → `semstreams-reviewer` before integration; Codex is owner-run,
out-of-band.
