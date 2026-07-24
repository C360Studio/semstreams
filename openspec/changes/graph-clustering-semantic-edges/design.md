# Design — Semantic co-location edges in community detection (Epic B, B2)

## The decision

Weighted-LPA (rebalance edge weights on the existing `LPADetector`), confirmed by the owner. Leiden is a
**gated future fork**: if weighted-LPA's `colocation_mean` gains prove insufficient once measured against the
compound gate (see below), Leiden is the recorded next lever — not built speculatively now. See ADR-086.

## Provider chain

```
kvProvider (explicit edges only)
  -> EntityIDProvider (adds sibling + system-peer virtual edges, gh#461-configurable)
    -> SemanticEdgeProvider (adds mutual-kNN semantic virtual edges, NEW, this change)
```

`SemanticEdgeProvider` implements `graph.Provider` (`GetAllEntityIDs`, `GetNeighbors`, `GetEdgeWeight`) and
wraps `EntityIDProvider` the same way `EntityIDProvider` wraps `kvProvider` today
(`graph/clustering/entityid_provider.go:146` for the `GetNeighbors` shape, `:389` for `GetEdgeWeight`). It is
inserted in `initProviderAndDetector` (`processor/graph-clustering/component.go:1027`) only when
`enable_semantic_edges` is true; when false, the chain is exactly what it is today (two providers, not
three) — no behavior change for an unopted deployment.

This is a fresh build, not a revival of ADR-061's removed `semantic_provider.go`. That implementation
predated the readiness contract (ADR-083/084/085), the current `EntityIDProviderConfig` shape (gh#461), and
the multi-tier weight resolution this change introduces; reviving the old commit would mean immediately
rewriting most of it.

## Mutual-kNN: the precise membership test

A semantic virtual edge between entities A and B is synthesized **only when both directions qualify**:

- B appears in A's `FindSimilar(A, threshold, k)` result (via `graph.embedding.query.similar`), **and**
- A appears in B's `FindSimilar(B, threshold, k)` result.

"Mutual" is the load-bearing word: a one-directional high-similarity outlier (A is in B's top-k because B has
few close neighbors, but B is nowhere near A's top-k because A has many closer ones) does not synthesize an
edge. This keeps per-entity semantic degree bounded by `k` in both directions and avoids exactly the kind of
lopsided vote-mass injection that made the unconditional sibling/system-peer edges dominate LPA in the first
place — the defect this change exists to fix, so the fix must not reintroduce the same failure mode with a
different edge type.

Starting values (empirical starting point, tuned against `colocation_mean` on the theme-spanning fixture
queries in `validate_partition_colocation.go` — record as measured, not asserted a priori): `k≈8`,
`similarity_threshold≈0.75`, `semantic_edge_weight≈0.9`.

## Weight resolution — one testable place, explicit dominant, max not sum

Today's `EntityIDProvider.GetEdgeWeight` (`entityid_provider.go:389-415`) is a first-match cascade: try
explicit, if `>0` return it; else try sibling; else try system-peer. Adding a fourth tier (semantic) the same
way would silently drop information — e.g. a pair that is *both* a sibling (0.7) and a mutual-kNN semantic
match (0.9) would resolve to whichever tier the cascade checks first, not to the max of the two.

The resolved `WeightConfig` this change introduces is a single, unit-testable function of the *set* of
qualifying tiers for a pair (`{explicit?, sibling?, systemPeer?, semantic?}`) rather than a first-match
cascade:

1. If an explicit edge exists (weight `> 0`), that weight is returned outright — explicit edges are
   **strictly dominant** over every virtual-edge tier, unconditionally.
2. Otherwise, the resolved weight is the **max** of the weights of every virtual-edge tier the pair
   qualifies under (sibling, system-peer, semantic) — never a sum. A pair that is both a sibling and a
   mutual-kNN match gets `max(sibling_weight, semantic_weight)`, not their total.

This requires `SemanticEdgeProvider.GetEdgeWeight` to evaluate sibling/system-peer/semantic membership
itself (or receive them from `EntityIDProvider` as a structured result rather than a bare `float64`) so the
max can be computed across tiers instead of losing tier identity the moment `EntityIDProvider.GetEdgeWeight`
collapses everything into one number. The exact seam (`EntityIDProvider` exposing per-tier queries, or
`SemanticEdgeProvider` re-deriving sibling/system-peer membership independently) is an implementation
decision for the build; the requirement fixed here is the **resolved value**, not the call shape.

### Starting weights and caps (this feature's profile only)

| Tier | Weight (today) | Weight (starting, semantic-enabled) | Cap (today) | Cap (starting, semantic-enabled) |
|------|------|------|------|------|
| Explicit | 1.0 | 1.0 (unchanged, always dominant) | — | — |
| Sibling | 0.7 | 0.7 (unchanged) | 10 | 5 |
| System-peer | 0.3 | 0.2 (or off) | 15 | 8 (or off) |
| Semantic (new) | — | ≈0.9 | — | k≈8 |

**These starting values apply only when `enable_semantic_edges` is true.** Omitting the config MUST
reproduce today's sibling/system-peer weights and caps exactly (0.7/10, 0.3/15) — the invariant the gh#461
change (`openspec/changes/archive/2026-07-04-graph-clustering-edge-config/`) established for that config
surface, and semboids' flock-coloring use of LPA over `flock.neighbor` edges depends on it holding. This
change does not touch the gh#461 defaults for an unopted deployment; it only changes what the *sibling/
system-peer* tiers resolve to *when a third voting tier (semantic) is added to the same vote*, so the total
non-explicit vote mass stays competitive rather than semantic edges getting drowned out by a now-even-larger
structural total.

## Embedding-readiness gate and the structural-floor guarantee

A second `readiness.Watcher` binds `readiness.KeyGraphEmbedding` in `startStatusWatcher`
(`processor/graph-clustering/component.go:1122`, mirroring the existing `KeyGraphIndex` watcher at `:1127`),
started only when `enable_semantic_edges` is true (no wasted subscription otherwise). `evaluateReadiness`
(`:1177`) gains a second axis:

| Index (graph-index) | Embedding (graph-embedding) | Outcome |
|---|---|---|
| not ready | (any) | defer the whole cycle (unchanged from today) |
| ready | not ready, semantic enabled | run structural-only this cycle; `semantic_edges_applied=false` |
| ready | ready (or semantic disabled) | run the full cycle; `semantic_edges_applied=true` (or n/a when disabled) |

This is the concrete shape of the tiered-graceful-fallback tenet this program has held since B0: structural
partition + statistical summaries are the correct Tier-0/1 floor; semantic edges are additive Tier-1/2;
detection **never fails or empties** for a cold embedding index, it degrades and reports. `pruneToPartition`
and the write-then-prune non-destructive rebuild (`openspec/specs/graph-clustering/spec.md`, "The community
index is rebuilt non-destructively") are unaffected — a structural-only cycle is a complete, valid partition
in its own right, not a partial one.

## Fail-open fix — scoped narrowly

`querySimilarityFinder.FindSimilar` (`processor/graph-clustering/similarity.go:93-99`) today does:

```go
respData, err := f.natsClient.RequestClassified(ctx, similarQuerySubject, reqData, similarQueryTimeout)
if err != nil {
    f.logger.Debug("similarity query failed (transport or handler)", ...)
    return nil, nil // fail-open: treated identically to "no similar neighbors"
}
```

`graph.embedding.query.similar`'s handler (`processor/graph-embedding/query.go:96`) calls
`ensureBootstrapReady()` first, which returns a classified transient `ErrorCodeIndexNotReady`
(`processor/graph-embedding/component.go:1481-1495`) when the embedding index is mid-bootstrap or its watcher
is unavailable — verified in code, not speculative (`processor/graph-embedding/predicate_contract_test.go`
pins this classification today). The blanket `return nil, nil` above cannot distinguish that transient from a
genuine "this entity has no close neighbors," so clustering silently commits communities computed with zero
semantic input whenever embedding happens to be cold — indistinguishable, until this diagnostic, from the
graph simply lacking semantic structure (#618).

The fix is scoped to the **new edge-consumer path only**: a readiness-aware wrapper (consulting
`errs.IsTransient` + the `ErrorCodeIndexNotReady` code, not message-text matching, per the existing
`graph-index-readiness` contract's classification discipline) that treats the not-ready transient as "could
not ask this tick" — which the embedding-readiness gate above already handles by falling back to
structural-only — and treats every other outcome (including a genuinely empty result set) as "asked, got
nothing." `SemanticGapDetector`'s existing call through the *same* `FindSimilar` method for anomaly detection
is **unchanged** — it keeps its opportunistic fail-open, because an anomaly-detection miss has a different
(lower) cost profile than silently committing a semantically-blind partition. The wrapper lives at the
clustering-edge call site, not inside `querySimilarityFinder` itself, so the two consumers do not have to
agree on one error-handling policy.

## Determinism fixes

Two independent non-determinism sources make `DetectCommunities` non-reproducible run-to-run on identical
input, which is required for the compound co-location gate (below) to mean anything:

1. `lpa.go:284`'s `rand.Shuffle` uses the unseeded global `math/rand` source. Fix: a per-`DetectCommunities`-
   call seeded `*rand.Rand`, so a fixed edge set is deterministic across runs (repeatable measurement) while
   still varying the shuffle order between hierarchical levels within one run (the shuffle's actual purpose —
   reducing LPA oscillation — is preserved).
2. `lpa.go:398-405`'s vote tally picks the winning label via unordered map iteration; a tie between two
   labels' vote totals resolves to whichever the map happens to yield first, which Go deliberately
   randomizes. Fix: on an exact tie, pick the lexicographically smallest label — deterministic, and no
   different in kind from the colocation diagnostic's own tie-break convention (`gradeColocation`'s
   sorted-first community assignment).

This folds in the B1/#606 detection-determinism piece; it is a prerequisite for this change's own gate
(below) rather than a separate future increment, because a gate compared against a non-reproducible baseline
cannot tell a real `colocation_mean` improvement from run-to-run noise.

## Bounded, cached edge build

`SemanticEdgeProvider.ensureCache` mirrors `EntityIDProvider.ensureTypePrefixCache`'s lazy-build-once-per-
`DetectCommunities`-call pattern, but keyed additionally by each entity's embedding revision: if unchanged
since the previous cycle, its cached mutual-kNN neighbor set is reused rather than re-queried. Without this,
a ~175-entity corpus issues ~175 `FindSimilar` calls every detection cycle regardless of whether any
embedding changed. `semantic_edge_build_ms` (wall time) and a query-count metric make the actual cost
observable rather than assumed.

## The compound colocation gate

`validate_partition_colocation.go` moves from a pure recorder to a pass condition requiring **all** of:

- `partition_colocation_mean` **increases** relative to the pre-change baseline (0.60) on the theme-spanning
  fixture queries (forklift-maintenance, fire-emergency, dock-equipment, conveyor-systems — cold-chain is
  same-type and already saturated at 1.0, so it is not the discriminating signal);
- `partition_entities_not_in_community` stays ~0 (coverage does not regress — a partition that co-locates by
  simply losing entities is not a win); and
- `partition_level0_communities > 1` (rejects the degenerate case where "all entities collapse into one
  community" trivially scores `colocation_mean=1.0` while being the worst possible partition).

The third condition exists because a naive pass/fail on `colocation_mean` alone is gameable by exactly the
failure mode this change must not reintroduce in the other direction — over-weighting semantic edges into a
monolithic blob. Cross-reference the B0 thematic-recall dimension (`validate_thematic_eval.go`) from the same
run: a rising `colocation_mean` should track a rising thematic recall on the same queries, not diverge from
it.

## Non-goals carried from the proposal

See `proposal.md`'s Non-goals for the full list (B3 ownership split, Leiden implementation, `EnableLLM`
re-enable, the frozen-partition paired-run follow-up, two-LLM-clients consolidation). Not repeated here.
