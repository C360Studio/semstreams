## 1. Digest shape + tag extraction (graphrag.go)

- [x] 1.1 Add `Tags []string \`json:"tags,omitempty"\`` to `EntityDigest`.
- [x] 1.2 Add consts/vars near `labelPredicates`: `MaxQueryFocusedReps = 5`, `MaxDigestTags = 6`,
  `digestTagPredicates = []string{vocabulary.ContentClassificationTag}`. (MEDIUM-2: the tag predicate
  is now a framework vocab constant — new `vocabulary.ContentClassificationTag` in
  `vocabulary/predicates.go` "Content Domain Predicates" group — instead of a bare string literal;
  the example package's `PredicateContentTag` string converges on it, unmigrated, like
  `DCTermsTitle`.)
- [x] 1.3 Add `collectDigestTags(entity *gtypes.EntityState, maxTags int) []string` — iterate
  `entity.Triples`, collect string `Object`s whose `Predicate ∈ digestTagPredicates`, dedupe,
  cap at `maxTags`. Unit-test multi-valued / dedupe / cap. (Param renamed from `max` → `maxTags`:
  `max` shadows the Go builtin and trips revive/CI.)

## 2. Query-relevant representative selection (graphrag.go)

- [x] 2.1 Add `selectDigestEntities(comm, semanticScores map[string]float64) []string` —
  members present in `semanticScores` sorted by score desc, top `MaxQueryFocusedReps`,
  backfilled from `comm.RepEntities` to the cap; pure `RepEntities` when scores nil/empty.
  Deterministic tie-break (score then ID). Unit-test ordering, backfill, full fallback, and
  "no rep slot lost vs today".
- [x] 2.2 Change `enrichCommunitySummaries` signature to take `semanticScores`; select via 2.1,
  **batch-load selected IDs once and keep `[]*EntityState`** (via `loadDigestEntities`, keyed by
  ID for lookup), set `Label = resolveLabel(e)` and `Tags = collectDigestTags(e, MaxDigestTags)`
  in one pass (`buildRepDigest`).
- [x] 2.3 `handleStrategyGraphRAG`: pass `semanticScores` into `enrichCommunitySummaries`.
- [x] 2.4 `enrichGlobalResponse`: gains a `semanticScores` param; the tier-1 full-load caller
  builds a score map from `semanticHits` and passes it; the pure-semantic-strategy caller
  (`handleStrategySemantic`) builds a score map from its threshold-filtered `[]SemanticHit` and
  passes it too (MEDIUM-1 — Lever A now applies on the semantic strategy, not just auto-summarize);
  path/entity-lookup/temporal/spatial callers pass `nil` (PageRank behavior unchanged there).
  `globalSearchTextBased` passes `nil`.

## 3. Prompt + template rendering (answer.go + graphrag.go)

- [x] 3.1 `buildAnswerPrompt` (answer.go): render each rep as `Label [Type] {tags: t1, t2, …}`
  when `len(e.Tags) > 0` (via shared `formatRepDigest`). Unit-test present/absent.
- [x] 3.2 `synthesizeAnswer` template floor: mirror the same tags rendering (owner tenet — floor
  mirrors the LLM prompt's context) via the same `formatRepDigest`. Unit-test.

## 4. Gate — local, mirrors CI

- [x] 4.1 `go build ./...`, `go vet ./...` (plain), `task lint` (revive clean), `go test -race ./...`.
  (build ok; vet ok; lint clean; race suite: 133 pkg ok, 0 fail.)
- [x] 4.2 `go vet -tags=integration ./...` and `-tags=live_llm ./...` (pre-tag build-tag sweep). (both clean.)
- [x] 4.3 `task schema:generate` + `git diff schemas/ specs/` shows no drift (EntityDigest is not
  schema-bound — confirmed: zero diff).
- [x] 4.4 Tagged integration tests on `processor/graph-query` (`-race -tags=integration`).
  (ok, 38.7s; `TestIntegration_EnrichGlobalResponse` + enrichment path all PASS, not skipped.)

## 5. Review

- [x] 5.1 `semstreams-reviewer` pass on the full diff; address findings.
  - Reviewer returned APPROVE with 2 MEDIUM + 2 NIT; architect resolved MEDIUM-2's boundary
    (framework owns the tag predicate as a vocab constant). Consolidated fix pass applied all four:
    - MEDIUM-1: `handleStrategySemantic` threads its threshold-filtered scores into
      `enrichGlobalResponse` (was `nil`). New unit-level seam test
      `TestEnrichCommunitySummaries_ScoresSteerRepDigests` proves the scores map is consumed by the
      enrichment path (no natsClient, mirrors the colliding-IDs test); driving `handleStrategySemantic`
      end-to-end needs a semantic embedding searcher, so it stays covered by mirror-symmetry with the
      reviewed tier-1 caller + `TestSelectDigestEntities`.
    - MEDIUM-2: `vocabulary.ContentClassificationTag` framework constant; `digestTagPredicates`
      references it. Vocab-registry check: no completeness test/lint requires registration; adding the
      const passed all `vocabulary/...` tests and `TestCollectDigestTags`' `semantictest.Predicate`
      cross-check. Example package left unmigrated per architect.
    - NIT-1: `gateway/graph-gateway/component.go` introspection `EntityDigest` typedef gains `"tags"`.
    - NIT-2: `graphrag_enrich_test.go` tie-break fixture `Members` reordered to non-ascending;
      mutation-verified (removing the `id < id` tie-break fails the subtest; restored by checksum).
- [ ] 5.2 (owner, out-of-band) Codex review; address findings before merge.

## 6. E2E validation (single run = confirm-trace)

- [x] 6.1 `task e2e:semantic:frontier` — ran green in 3m37s (`validation_errors:0`,
  `known_answer 7/7`, partition determinism 1.0). GEMINI_API_KEY sourced from `../semdev/.env`.
- [x] 6.2 `thematic_theme_recall_mean` **0.85 → 0.95** (pass_rate 1.0). `battery` (forklift 0.75→1.00)
  and `door` (dock 0.75→1.00) recovered; NO fixture gained a newly-missing term (regression guard
  clean); all other dims perfect (nonempty/grounding/stability 1.0, fabrication 0, degraded-floor 0).
  **`evacuation` (fire) still missing** — its only tag-bearing entity `doc-emergency-001` sits in the
  document-type community, which doesn't reach the fire query's top synthesis clusters (dominated by
  maintenance work orders). Cross-community-coverage limit, NOT a tags-mechanism defect (unit-proven).
  Deferred to a filed follow-up (retrieval-side multi-community query expansion).
- [ ] 6.3 Non-regression smoke `task e2e:semantic` (1.7b) — NOT run; the frontier run is the single
  owner-approved validation, and its own regression guard (recall scored across all 5 fixtures) is
  clean. Optional, skip.

## 7. Land

- [ ] 7.1 Merge per house gate (CI green, `gh pr checks`, `mergeStateStatus`; Codex addressed).
- [ ] 7.2 `openspec archive thematic-synthesis-context` (promote the graph-query requirement
  into `openspec/specs/graph-query/spec.md`).
