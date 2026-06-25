# ADR-061: Remove the orphaned community `SemanticProvider` (semantic virtual edges)

## Status

**Proposed — 2026-06-24.** Resolves gh#238.

## Decision

**Remove** `graph/clustering/semantic_provider.go` (the `SemanticProvider` decorator, the
operator-facing `semantic_edges` concept) and its phantom documentation, rather than wire it.
The capability was implemented in the Jan-2026 monolith split but never integrated, has **no
production consumer**, and — verified against the code — would not move end-user search results even
if wired. Keeping ~600 LOC of tested-but-dead code plus docs that teach a non-existent config is a
liability, not an asset. Git preserves the implementation; this ADR preserves the recipe for a
deliberate re-add if a real consumer ever appears.

## Context

`SemanticProvider` (337 LOC + 259 LOC test) wraps the LPA community-detection provider and injects
**ephemeral** embedding-similarity "virtual" neighbors, so semantically-similar-but-unlinked entities
can cluster together. It was added in commit `a60ef433` (2026-01-05); integration fell out of scope
and was never tracked until gh#238.

Three findings — each grounded in code, the last two from a 3-lens adversarial review — drove the
decision:

1. **No consumer, never wired.** `NewSemanticProvider` has zero non-test callers;
   `processor/graph-clustering/component.go:initProviderAndDetector` builds the provider chain and
   never wraps it. The `clustering.SimilarityFinder` interface and `graph.SimilarityHit` type it
   needs are used only by it. The operator `semantic_edges` config block has no struct binding, so
   `json.Unmarshal` silently drops it.

2. **It would not move primary search even if wired (the crux).** An independent trace of all seven
   graph-query strategy handlers and all community-cache read sites confirmed: the primary semantic
   path ranks entities **entirely from the embedding index** (`searchEntitiesSemantic`); community
   membership is **post-hoc decoration** (`findCommunitiesForEntities` maps already-ranked entities
   to communities for summaries). `enrichGlobalResponse` provably never touches `resp.Entities`.
   Community structure is load-bearing for *retrieval* only on the text-based **fallback** path
   (fires only when semantic search returns zero/errors) and in local search. So wiring
   `semantic_edges` would improve community-detection quality, community *summaries*, and
   fallback/local retrieval — **not** the primary ranked result set.

3. **No future trigger.** The natural "wire it when X lands" candidate, gh#236 (k-core/pivot into
   ranking), is **orthogonal and dormant**: it wires the **structural** index (node centrality/
   distance), not **community** membership, into ranking — and `semantic_edges` are ephemeral, so
   they never affect the structural index anyway. gh#236 landing would not make `semantic_edges`
   reach primary ranking. There is no planned consumer that would.

Reinforcing the marginality: neural embeddings are only generated for entities with **text content**
(`graph-embedding` skips entities whose extracted text is empty — `component.go:967-969`; ADR-054
formalizes indexing eligibility). So `semantic_edges` could only ever influence clustering of *text*
entities — exactly the entities where semantic **search** already provides the value. The feature was
doubly redundant.

## Why remove (Option C) rather than keep dormant (B) or wire (A)

- **Wire it (A) — rejected.** No consumer, no future trigger, no primary-search effect. An adversarial
  right-sizing review found the wiring plan over-engineered ~3× for an off-by-default community-quality
  nicety. Per "keep only if confirmed helping" ([ADR-054](054-semantic-indexing-eligibility.md)
  decomposition discipline), this is not confirmed helping.
- **Doc-correct + keep dormant (B) — rejected as a half-measure.** It still carries ~600 LOC that must
  compile, pass tests, and confuse readers ("is this used?") indefinitely, for a capability nothing
  consumes. "We might want it later" is fully covered by git history + this ADR — keeping live dead
  code is not the only way to preserve it.
- **Remove (C) — chosen.** Eliminates the dead code, the phantom docs, and the silent-drop config
  footgun in one move. Honest about what the system does.

## What is removed

- `graph/clustering/semantic_provider.go` + `graph/clustering/semantic_provider_test.go`.
- `graph.SimilarityHit` (`graph/types.go`) and the `clustering.SimilarityFinder` interface — orphaned
  once the provider goes (their "used by semantic search" doc comment was stale; semantic search uses
  `SemanticHit` / `inference.SimilarityResult`).
- The stale `from SemanticProvider` comment on `LPADetector.computeCommunityTightness` (the function
  computes explicit-edge density and never depended on the provider's cache).
- The `semantic_edges` / virtual-edge documentation across `docs/advanced/01-clustering.md`,
  `docs/concepts/07-community-detection.md`, `docs/concepts/05-embeddings.md`,
  `docs/concepts/00-real-time-inference.md`, `docs/advanced/05-index-reference.md`,
  `docs/basics/04-vocabulary.md`, `docs/ROADMAP.md`, `docs/concepts/06-similarity-metrics.md` —
  including the non-compiling `NewSemanticGraphProvider` example.

## What is NOT changing

- **Semantic search** — the neural tier's actual value — is untouched. `searchEntitiesSemantic` /
  `graph.embedding.query.search` / FindSimilar continue to power GraphRAG retrieval, the
  `graph.query.semantic`/`graph.query.similar` APIs, and the `search_graph`/`research_graph` tools.
- **Community detection** still runs (LPA over **explicit** relationship edges) — exactly as it does
  in production today.
- **The anomaly engine** (`inferred.semantic.*` edges) is a separate, already-disabled feature
  (#237); this ADR does not touch it.

## Recoverability (if a real consumer ever appears)

The implementation is recoverable from git at commit `a60ef433` (and the removal commit's parent).
A deliberate re-add — should a primary-path consumer of community structure ever land — is small and
documented here so it need not be reverse-engineered:

- Restore `semantic_provider.go` (and `graph.SimilarityHit` / `clustering.SimilarityFinder`).
- Gate with **one** `enable_semantic_edges` bool + two flat fields (`semantic_similarity_threshold`,
  `semantic_max_virtual_neighbors`), matching the component's `EnableStructural` convention (not a
  nested block).
- The component **already builds** a production similarity finder (`initQuerySimilarityFinder` →
  `c.similarityFinder`, `inference.SimilarityFinder` shape). Bridge it to the clustering interface
  with a ~6-line adapter (the result types share `EntityID`/`Similarity`; the removed `SimilarityHit`
  additionally carried an optional `EntityType` the adapter would default).
- Wrap the provider in `initProviderAndDetector` when the flag is on; off by default.
- Re-add the silent-drop guard (extend `RejectUnknownKeys` to the top-level config) regardless.

## References

- gh#238 (resolved by this ADR), gh#236 (orthogonal: structural-index ranking — dormant),
  PR #237 (ADR-054 Move 1 — disabled the anomaly virtual-edge producer).
- Commit `a60ef433` (2026-01-05) — the monolith split that introduced the orphan.
- [ADR-054](054-semantic-indexing-eligibility.md) — indexing eligibility + the
  "keep only if confirmed helping" decomposition discipline.
- `processor/graph-query/graphrag.go` — the crux trace (embedding-ranked primary path; community as
  decoration; community-driven retrieval only on the text fallback + local search).
- `processor/graph-embedding/component.go:967-969` — text-content eligibility (no text → no embedding).
