## Why

GraphRAG global-search thematic recall is stuck at a **0.85 ceiling** (mean over the
B0 fixture queries; instrument `test/e2e/scenarios/validate_thematic_eval.go`). Epic B
closed the partition hypothesis (B2): co-location is off the recall path. A code-grounded
trace localizes the ceiling to **answer-synthesis context**: the synthesis prompt
(`buildAnswerPrompt`) feeds the LLM only, per community, the ~200-token community summary,
up to 5 **PageRank-selected** representative-entity titles, and up to 5 keywords. PageRank
selection is **query-agnostic**, and entity descriptions/bodies never enter the prompt at
all. So theme vocabulary that lives on a member that is not a top-PageRank representative —
`battery` (title of `maint-008` "Forklift Battery Maintenance"), `door` (`maint-009`
"Overhead Door Motor Replacement"), `evacuation` (only in the tags/description of
`doc-emergency-001`) — cannot appear in the synthesized answer regardless of how good the
partition or the model is. This is a v1 quality bar: semsource (the lead v1 product) wires
`global_search` for natural-language thematic answers.

## What Changes

- **Query-relevant representative selection on the synthesis path.** In the auto-summarize
  branch of `handleStrategyGraphRAG`, the per-community digest entities are selected by
  **query relevance** — the `semanticScores` the handler already computes but currently
  spends only on a flat `EntityDigest.Relevance` field — rather than by query-agnostic
  PageRank. Selection backfills from the community's PageRank `RepEntities` to fill the cap
  when scores are thin, and falls back **entirely** to PageRank when `semanticScores` is
  absent (the text/statistical floor path), so no representative slot is ever lost versus
  today and the tiered graceful-fallback floor is preserved.
- **A capped per-digest tags channel.** `EntityDigest` gains a `Tags` field, populated from
  the entity's `content.classification.tag` triples (already resident on the loaded
  `EntityState` — tags are triples; body/description are ObjectStore-only and stay out of
  the prompt). Rendered on the representatives line in **both** the LLM prompt
  (`buildAnswerPrompt`) and the template floor (`synthesizeAnswer`).
- Prompt growth stays **bounded independent of community size** (`≤ MaxQueryFocusedReps` ×
  `MaxAnswerClusters` × (label + `≤ MaxDigestTags`)); a 10,000-member community still
  contributes exactly the capped number of digests. The 500-token answer response cap is
  untouched.
- Seeds one new requirement in the currently-silent `graph-query` capability spec on what
  the thematic answer-synthesis context MUST include.

Not breaking: `EntityDigest.Tags` is an additive, `omitempty` response field; the digest
shape is not part of any generated schema or contract spec.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `graph-query`: adds a requirement that thematic (global-search) answer-synthesis context
  include query-relevant representatives and their classification tags, with a bounded cap
  and a PageRank fallback that preserves the degraded floor. The spec is currently silent on
  answer synthesis; this is a lazy seed on first touch, verified against code.

## Impact

- **Code**: `processor/graph-query/graphrag.go` (`EntityDigest` gains `Tags`;
  `enrichCommunitySummaries` takes `semanticScores` and selects query-relevant members via a
  single batch entity load that keeps `[]*EntityState` for label + tag extraction in one
  pass; `handleStrategyGraphRAG` and `enrichGlobalResponse` thread scores;
  `synthesizeAnswer` template mirrors tags; new `collectDigestTags` helper and
  `digestTagPredicates`/`MaxQueryFocusedReps`/`MaxDigestTags` consts). `processor/graph-query/answer.go`
  (`buildAnswerPrompt` renders tags).
- **APIs**: `graph.query.globalSearch` response `entity_digests[].tags` added (additive).
- **Consumers**: semsource `global_search`; any `graph.query.globalSearch` caller. No caller
  change required (additive field; richer answer text).
- **Validation**: one `task e2e:semantic:frontier` run — `thematic_theme_recall_mean` must
  clear 0.85 toward ~1.0, with `battery`/`door`/`evacuation` flipping from
  `theme_terms_missing` to present. No ADR (reversible internal synthesis-context
  enrichment; no irreversible choice, no cross-repo contract).

## Non-goals

- **No partition/community-detection change.** B2 is closed; this does not touch clustering,
  edge weighting, or `COMMUNITY_INDEX`. Recall is fixed on the synthesis/read path only.
- **No entity body/description in the prompt.** Bodies are ObjectStore-only; pulling them
  would add per-entity fetches and unbounded tokens. Tags (already-loaded triples) recover
  the missed terms without that cost.
- **No community-summary rewrite.** Summaries are generated query-agnostically at clustering
  time under a 200-token budget; making them query-focused is a different, heavier lever and
  is not attempted here.
- **No eval recalibration.** The missed terms are genuinely in-domain; redefining recall to
  hide the lossiness was considered and rejected.
- **Not converting the B0 recall recorder to a hard gate.** This change moves the number;
  flipping dimension 3 to a gate remains the B2/B3 gate-conversion work.
