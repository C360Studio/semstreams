# Tasks — Semantic co-location edges in community detection (Epic B, B2)

> Scoping change (Proposed). Tasks unchecked; implementation follows after #656 merges and this change is
> approved. See `design.md` for the mechanics behind each item.

## 1. `SemanticEdgeProvider` decorator

- [x] 1.1 New `graph/clustering/semantic_edge_provider.go` (or `processor/graph-clustering/`, TBD at build
      time) implementing `graph.Provider` (`GetAllEntityIDs`, `GetNeighbors`, `GetEdgeWeight`), wrapping
      `entityIDProvider` in the chain built at `processor/graph-clustering/component.go:1027`
      (`kvProvider -> EntityIDProvider -> SemanticEdgeProvider`). Model the shape on
      `graph/clustering/entityid_provider.go:146` (`GetNeighbors`) and `:389` (`GetEdgeWeight`) — do NOT
      revert ADR-061's removed commit; the chain, config shape, and readiness contract have all changed.
- [x] 1.2 Source candidate neighbors from the existing similarity finder path
      (`graph.embedding.query.similar`, `processor/graph-clustering/similarity.go:66` `FindSimilar`,
      `:130` `initQuerySimilarityFinder`) — do not build a second similarity RPC.
- [x] 1.3 Mutual-kNN membership test: an edge A-B synthesizes only when B is in A's top-`k` (at/above
      `semantic_similarity_threshold`) **and** A is in B's top-`k`. One-directional matches do not
      synthesize an edge.
- [x] 1.4 Wired only when `enable_semantic_edges` is true; the chain is unchanged (two providers, not
      three) when false.

## 2. Resolved `WeightConfig`

- [x] 2.1 A single, unit-testable weight-resolution function of the qualifying-tier set for a pair
      (`{explicit?, sibling?, systemPeer?, semantic?}`) — explicit strictly dominant (returned outright when
      `>0`); otherwise the **max** across qualifying virtual-edge tiers, never a sum. See design.md's
      "Weight resolution" section for why the existing first-match cascade in
      `EntityIDProvider.GetEdgeWeight` cannot be reused unmodified for a 4th tier.
- [x] 2.2 Starting values (empirical, tune against `colocation_mean`, record as measured not asserted):
      explicit 1.0 (unchanged); semantic-kNN weight ≈0.9, mutual-kNN, `k≈8`, similarity threshold ≈0.75;
      sibling weight 0.7 (unchanged) cap 10→5; system-peer weight 0.3→0.2, cap 15→8 (or off in this
      profile).
- [x] 2.3 These starting values apply **only** when `enable_semantic_edges` is true. Omitting the config
      MUST reproduce today's sibling/system-peer weights and caps exactly (0.7/10, 0.3/15) — verify against
      the gh#461 change's existing default-preservation tests
      (`TestEntityIDEdgesConfig_Resolve_NilKeepsDefaults`, `TestApplyDefaults_ResolvesEntityIDEdges`) still
      passing unmodified.

## 3. `enable_semantic_edges` config surface

- [x] 3.1 New `Config` fields: `EnableSemanticEdges bool` + flat `SemanticSimilarityThreshold float64`,
      `SemanticMaxNeighbors int` (the mutual-kNN `k`), `SemanticEdgeWeight float64` — extend
      `component.go:102-108` (the `EntityIDEdgesConfig` struct region) and `:171` (`resolve()`) with the
      equivalent surface for the semantic tier; schema tags per the existing `category:advanced` convention.
      (Built as a strict-decodable `semantic_edges` block `SemanticEdgesConfig` carrying those four fields,
      parallel to `EntityIDEdgesConfig`, rather than four flat top-level `Config` keys — the only shape that
      satisfies §3.2's "unrecognized key under the semantic-edges block fails" without a blanket
      `DisallowUnknownFields` on `Config`, which the component deliberately avoids. Field names verbatim.)
- [x] 3.2 Strict-decode guard mirroring `rejectUnknownEntityIDEdgeKeys` (`component.go:117-129`) /
      `inference.RejectUnknownKeys` (ADR-054): an unrecognized key under the semantic-edges block fails
      config load loudly rather than being silently dropped by `encoding/json`.
- [x] 3.3 JSON round-trip test for every new operator-reachable field (house rule: operator-configurable
      surface needs a round-trip test; no shadow structs).
- [x] 3.4 `task schema:generate`; drift generated (left uncommitted for the reviewer/PR owner per handoff).

## 4. Embedding-readiness gate

- [ ] 4.1 Second `readiness.NewWatcher(c.natsClient, readiness.KeyGraphEmbedding, ...)` alongside the
      existing graph-index watcher in `startStatusWatcher` (`component.go:1122`, existing watcher at
      `:1127`), started only when `enable_semantic_edges` is true.
- [ ] 4.2 `evaluateReadiness` (`component.go:1177`) gains the second axis described in design.md: index not
      ready → defer whole cycle (unchanged); index ready + embeddings not ready + semantic enabled → run
      structural-only, stamp `semantic_edges_applied=false`; both ready → full cycle, stamp
      `semantic_edges_applied=true`.
- [ ] 4.3 A structural-only cycle is a complete, valid partition — verify `pruneToPartition` and the
      write-then-prune non-destructive rebuild (existing `graph-clustering` spec requirement) are unaffected
      by a cycle that skips the semantic tier.

## 5. Fail-open fix (clustering-edge consumer only)

- [ ] 5.1 A readiness-aware wrapper around the semantic-edge path's calls into `FindSimilar`
      (`processor/graph-clustering/similarity.go:93-99` is today's blanket `return nil, nil` on any error)
      that distinguishes the classified `ErrorCodeIndexNotReady` transient (detect via `errs.IsTransient` +
      the code, never message-text matching) from a genuine empty result. Not-ready degrades to
      structural-only for that tick (handled by the readiness gate in section 4); a genuine empty result is
      "no semantic neighbors," not an error.
- [ ] 5.3 Concurrency check surfaced in the §1-3 core review: once enabled, the `SemanticEdgeProvider`
      (`c.graphProvider`) is shared between the detector loop and `startEnhancementWorker`
      (`component.go:962`). `ensureCache`'s double-checked locking keeps the build-once safe, but verify no
      live race when §4/§5 land and (in B3) `EnableLLM` re-enables the enhancement worker — that is the first
      time both readers run concurrently against the provider.
- [ ] 5.2 `SemanticGapDetector`'s existing call through `FindSimilar` for anomaly detection is UNCHANGED —
      it keeps its opportunistic fail-open. The wrapper lives at the new call site, not inside
      `querySimilarityFinder` itself.

## 6. Determinism fixes (folds in B1/#606)

- [x] 6.1 `lpa.go:284`'s unseeded global `math/rand.Shuffle` becomes a per-`DetectCommunities`-call seeded
      `*rand.Rand`. A fixed edge set MUST produce the same partition across repeated runs.
- [x] 6.2 `lpa.go:398-405`'s map-iteration vote tie-break becomes deterministic: on an exact vote-total tie,
      the lexicographically smallest label wins.
- [x] 6.3 A regression test: two `DetectCommunities` runs over an identical fixed edge set (including a
      deliberate vote tie) yield identical partitions.

## 7. Bounded, cached edge build

- [ ] 7.1 Cache each entity's mutual-kNN neighbor set, keyed by its embedding revision; reuse across
      detection cycles when the revision is unchanged (avoids ~175 `FindSimilar` calls/cycle on an
      unchanged corpus).
- [ ] 7.2 `semantic_edge_build_ms` (duration) and a query-count metric for the semantic-edge build phase.

## 8. Compound colocation gate

- [ ] 8.1 Convert `validate_partition_colocation.go` from record-only to a pass condition requiring: (a)
      `partition_colocation_mean` rises on the theme-spanning fixture queries (forklift-maintenance,
      fire-emergency, dock-equipment, conveyor-systems) relative to the 0.60 pre-change baseline; (b)
      `partition_entities_not_in_community` stays ~0 (no coverage regression); (c)
      `partition_level0_communities > 1` (rejects the degenerate single-community collapse that would
      otherwise score a vacuous 1.0).
- [ ] 8.2 Cross-reference the B0 thematic-recall dimension (`validate_thematic_eval.go`) from the same run
      in the printed output / `Details`, so a `colocation_mean` rise that does NOT track a thematic-recall
      rise on the same queries is visible, not just the aggregate pass/fail.

## 9. Spec + close

- [ ] 9.1 `openspec validate --strict` green on this change's spec deltas (`graph-clustering`,
      `graph-embedding`).
- [ ] 9.2 `go vet` (plain + `-tags=integration` + `-tags=live_llm`), `task lint`, `go test -race ./...`,
      `task schema:generate` (no undeclared drift beyond the new fields), contract tests.
- [ ] 9.3 Relevant e2e tier green (`task e2e:semantic`, the compound colocation gate from section 8) before
      any tag — this change's semantic-edge chain wiring is a behavior change to a shipped component
      (breaking-change-adjacent even though `enable_semantic_edges` defaults off; verify the default-off
      path is byte-identical to today via the gh#461 default-preservation tests before relying on default
      isolation to skip the tier).
- [ ] 9.4 `semstreams-reviewer` approval; ADR-086 promoted from `Proposed` to `Accepted` in the same or the
      following PR (do not leave it long-lived `Proposed`).
- [ ] 9.5 Archive this change on completion; promote the durable requirements into `openspec/specs/`.
- [ ] 9.6 Report the measured `colocation_mean` delta (and the cross-referenced B0 thematic-recall delta)
      back onto the Epic B baton (`docs/proposals/prev1-program.md`) and close out #606 (partial — the
      weighting half only) and #618 (partial — the clustering-edge-consumer scope only), leaving the
      ownership/EnhancementWorker halves open for B3.
