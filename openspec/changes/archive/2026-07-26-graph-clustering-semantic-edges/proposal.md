# Semantic co-location edges in community detection (Epic B, B2)

## Why

A white-box diagnostic (`test/e2e/scenarios/validate_partition_colocation.go`, merged in #656) measured —
un-confounded, LLM-independent, on the cheap 1.7b `task e2e:semantic` tier — that the community partition
clusters entities by **type/status**, not by **theme**. Every completed `maint-*` entity collapses into one
level-0 community, every `doc-*` into another, every `obs-*` into another (`colocation_mean=0.60`, coverage
clean: `partition_entities_not_in_community=0`, `partition_level0_communities=5` — not a degenerate collapse).
Any thematic query that spans entity types scatters: forklift-maintenance 0.40, conveyor-systems 0.33,
fire-emergency 0.50, dock-equipment 0.75. The one query that co-locates perfectly (cold-chain, 1.00) does so
because both its entities are the *same* type — the coincidence that was masking the defect.

The cause is quantifiable, not merely observed. LPA's label-propagation vote is a weighted sum over a node's
neighbor edges (`graph/clustering/lpa.go:380-395`), and today's edge set is explicit relationships plus two
**EntityID-derived virtual edges** synthesized unconditionally by `EntityIDProvider`
(`graph/clustering/entityid_provider.go`): sibling edges (same 5-part type prefix, weight 0.7, cap 10) and
system-peer edges (same system, weight 0.3, cap 15). A maxed-out entity can carry sibling vote mass of
`10 × 0.7 = 7.0` and system-peer vote mass of `15 × 0.3 = 4.5`, against explicit vote mass typically in the
`1–3 × 1.0` range for these corpora. The type/status signal structurally dominates the vote before a theme
signal ever gets a chance to compete.

This explains the residual gap the Epic B arc has been chasing since B0: fixing retrieval (#645) resolved
4/5 of the thematic-recall defect, and even a frontier synthesizer (Gemini 2.5 Flash, `task
e2e:semantic:frontier`) caps every 4-expected-entity query at exactly 3/4 — the missing entity is a
*different type in a different community*, structurally unreachable via community retrieval no matter how
good the synthesizer is. The frontier probe's apparent "B2 is low-ROI" reading was **confounded** (PR #653
Codex finding #3 — frontier and local runs each re-clustered independently at partition determinism ~0.83,
so the recall delta conflated model quality with a possibly-different realized partition) and is **reversed**
by this white-box measurement, which needs no model at all: it traces where each *known* corpus entity lands
in the partition directly.

## What Changes

Add ephemeral **semantic-similarity (mutual-kNN) virtual edges** to the community-detection edge set, weighted
to *compete with* — not be dominated by — the entity-id structural edges, so entities that are thematically
related but structurally heterogeneous (different type, different system) can still land in the same
community. The owner has confirmed the detection engine for this change: **weighted-LPA** (rebalance edge
weights on the existing `LPADetector`/`EntityIDProvider` chain). Leiden is **deferred as a gated future fork**
— recorded as an alternative in ADR-086, not built now.

- **`SemanticEdgeProvider`** — a new `graph.Provider` decorator wrapping the existing `EntityIDProvider` in
  the `initProviderAndDetector` chain (`kvProvider → EntityIDProvider → SemanticEdgeProvider`), sourcing
  candidate neighbors from the already-built similarity finder (`graph.embedding.query.similar`, the same RPC
  `processor/graph-clustering/similarity.go` already calls for anomaly detection). Built fresh — this does
  **not** revert ADR-061's removed `SemanticProvider` commit; the provider chain, config shape, and readiness
  contract have all changed since.
- **Mutual-kNN synthesis**: a semantic edge between A and B is synthesized only when each appears in the
  other's top-`k` similarity result at or above a similarity threshold (starting point `k≈8`,
  `threshold≈0.75`) — a one-directional high-similarity outlier does not synthesize an edge, and per-entity
  degree stays bounded.
- **One resolved `WeightConfig`** owning every edge-tier weight and cap (explicit, sibling, system-peer,
  semantic) in a single testable place. Resolution rule: explicit edges (weight 1.0) are strictly dominant;
  where a pair qualifies under more than one virtual-edge tier (e.g. sibling *and* semantic), the resolved
  weight is the **max** across qualifying tiers, never a sum. Starting weights (empirical, tuned against
  `colocation_mean` — recorded as a starting point, not fiat): semantic-kNN ≈0.9; sibling weight 0.7 → cap
  10 → 5; system-peer weight 0.3 → 0.2, cap 15 → 8 (or off), **applied only when `enable_semantic_edges` is
  true** — an unopted deployment keeps today's sibling/system-peer defaults byte-for-byte (the invariant the
  gh#461 change established).
- **`enable_semantic_edges` config**: one bool plus flat `semantic_similarity_threshold`,
  `semantic_max_neighbors`, `semantic_edge_weight` fields, strict-decoded (`RejectUnknownKeys`-style guard,
  ADR-054 no-silent-drop pattern) so an operator's typo fails loudly rather than silently leaving the feature
  off.
- **Embedding-readiness gate**: a second `readiness.NewWatcher(..., readiness.KeyGraphEmbedding, ...)`
  alongside the existing graph-index watcher. Per detection tick: index not ready → defer the whole cycle
  (unchanged); index ready + embeddings not ready + semantic edges enabled → run **structural-only** and stamp
  `semantic_edges_applied=false`; both ready → run the full cycle and stamp `semantic_edges_applied=true`.
  Community detection never fails or empties for a cold embedding index — semantic edges are additive Tier-1/2
  over an always-committing structural floor (the tiered-graceful-fallback tenet this program has held
  throughout).
- **Fail-open fix, scoped to the new edge consumer only** (#618): today's `FindSimilar`
  (`processor/graph-clustering/similarity.go:93-99`) collapses *every* error, including the classified
  `ErrorCodeIndexNotReady` transient, into "no similar neighbors" — indistinguishable from a graph that
  genuinely has none. The semantic-edge path gets a readiness-aware wrapper that treats the not-ready
  transient as "couldn't ask this tick" (degrade to structural-only), never as "asked, got nothing."
  Anomaly detection's `SemanticGapDetector` keeps its existing opportunistic fail-open — this fix is scoped
  to the clustering-edge consumer, not a global behavior change.
- **Determinism fixes** (folds in the B1/#606 detection-determinism piece — required for the co-location gate
  to be trustworthy run-to-run): the unseeded global `math/rand.Shuffle` at `lpa.go:284` becomes a per-
  `DetectCommunities`-run seeded `*rand.Rand`, and the map-iteration vote tie-break at `lpa.go:398-405` becomes
  a deterministic tie-break (lexicographically smallest label on equal vote totals).
- **Bounded, cached edge build**: reuse a semantic neighbor set when an entity's embedding revision is
  unchanged since the last cycle (today's ~175 similar-queries/cycle otherwise), plus a
  `semantic_edge_build_ms` duration and query-count metric.
- **Convert the co-location recorder to a compound gate**: `validate_partition_colocation.go` moves from
  record-only to a pass condition requiring `colocation_mean` **rises** on theme-spanning queries **and**
  `partition_entities_not_in_community` stays ~0 **and** `partition_level0_communities > 1` — rejecting the
  degenerate "collapse into one blob" partition that would otherwise score a vacuous 1.0. Cross-reference the
  B0 thematic-recall dimension from the same run.

### Scope boundary

This change **subsumes**:

- **#606**, the "semantic-affects-partition + weighting" half only — the semantic tier now genuinely
  participates in the partition, and the previously-dead `GetEdgeWeight` unweighted-vote defect is fixed by
  the resolved `WeightConfig`.
- **#618**, scoped to the new clustering-edge consumer only.

This change explicitly does **not** build (stays B3):

- **#607, #608, #617** — `EnhancementWorker` CAS/ownership/resurrection bugs and the `COMMUNITY_SUMMARIES`
  split. B2 builds with `EnableLLM=false` (the B1 interim), so the enhancement-worker clobber/resurrection
  race stays dormant during measurement and this change does not touch that surface.
- **#606's shared-mutable / `COMMUNITY_INDEX` ownership half** — the CAS/split-bucket redesign is a B3
  ownership decision, not a partition-quality one.

## Capabilities

### Modified Capabilities

- `graph-clustering`: adds semantic co-location edge synthesis (mutual-kNN, weighted to compete with
  structural edges), the resolved multi-tier `WeightConfig`, the `enable_semantic_edges` config surface, a
  second (embedding) readiness watcher with a structural-floor fallback, and detection determinism (seeded
  shuffle + deterministic tie-break).
- `graph-embedding`: seeds a requirement documenting the `graph.embedding.query.similar` not-ready
  classification (`ErrorCodeIndexNotReady`, already true in code) as current truth now that a second
  consumer — the clustering semantic-edge path, not just anomaly detection — depends on distinguishing it
  from a genuine empty result.

## Impact

- **Framework code**: `processor/graph-clustering/component.go` (new config fields, provider chain, second
  readiness watcher), a new `semantic_edge_provider.go` (mirrors `graph/clustering/entityid_provider.go`'s
  `GetNeighbors`/`GetEdgeWeight` shape), `processor/graph-clustering/similarity.go` (readiness-aware wrapper
  for the edge-consumer path only), `graph/clustering/lpa.go` (seeded shuffle, deterministic tie-break),
  `test/e2e/scenarios/validate_partition_colocation.go` (recorder → compound gate).
- **Schema**: `task schema:generate` gains the new `enable_semantic_edges` block on the graph-clustering
  component schema (expected drift, committed).
- **Consumers**: **semsource** (the lead v1 product wiring `global_search` and Tier-2 seminstruct
  summarization) is the direct beneficiary — community membership becomes load-bearing for its GraphRAG
  thematic/global-search retrieval, which is exactly the consumer whose arrival is what reverses ADR-061's
  "no consumer" premise. No other `sem*` product is known to depend on level-0 community *membership*
  granularity (semboids' flock-coloring use of LPA depends on the EntityID edges from the gh#461 change,
  untouched here).
- **Architecture records**: ADR-086 (new) reverses ADR-061's "no consumer" premise; ADR-061's other premise
  (community structure would not move primary search) is unaffected and stands.

## Non-goals

- **The B3 ownership split** (`COMMUNITY_INDEX` vs `COMMUNITY_SUMMARIES`, CAS-guarded enhancement writes,
  #607/#608/#617, #606's ownership half) — separate change, re-enables `EnableLLM`.
- **Leiden** — recorded in ADR-086 as a gated future fork if weighted-LPA's colocation gains prove
  insufficient; not implemented in this change.
- **Re-enabling `EnableLLM`** — this change builds and measures with `EnableLLM=false` (the B1 interim),
  unchanged.
- **The frozen-partition paired synthesizer comparison** (#654) — a separate measurement-only follow-up;
  this change's own compound gate is the trustworthy signal, not a re-run of the frontier probe.
- **Two-LLM-clients admission-gate consolidation** (#652) — separate, architect-owned, orthogonal to edge
  synthesis.
- **Changing anomaly detection's fail-open behavior** — `SemanticGapDetector` keeps its existing opportunistic
  fail-open; only the new clustering-edge consumer gets the readiness-aware wrapper.
- **Changing the entity-id edge defaults for deployments that do not opt in** — omitting
  `enable_semantic_edges` MUST reproduce today's sibling/system-peer weights and caps exactly (the gh#461
  invariant), not the rebalanced starting values above.

## Consumers

`processor/graph-clustering` (framework component, owns the new synthesis + config). **semsource** (lead v1
product; GraphRAG `global_search` + Tier-2 seminstruct summarization become dependent on community
membership quality — notify on merge). No other `sem*` product is a known consumer of level-0 community
membership at this time.
