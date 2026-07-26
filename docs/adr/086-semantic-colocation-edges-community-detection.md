# ADR-086: Semantic co-location edges in community detection

## Status

**Accepted — 2026-07-26.** Decision record for the OpenSpec change
`graph-clustering-semantic-edges` (Epic B, increment B2). Reverses the "no consumer" premise of
[ADR-061](061-community-semantic-virtual-edges.md); its "would not move primary search" premise is
unaffected and stands. See [Outcome](#outcome-2026-07-26) for the §8 measurement that followed
acceptance — it refines the expected ROI of the mechanism this ADR ships; it does not reverse the
decision below.

## Decision

Community detection incorporates an ephemeral **semantic mutual-kNN virtual-edge tier**, weighted to
compete with — not be dominated by — the entity-id structural edges (sibling, system-peer) it already
synthesizes, gated on embedding readiness, and strictly additive over an always-committing structural
floor. The detection engine for this increment is **weighted-LPA**: rebalance edge weights on the existing
`LPADetector`/`EntityIDProvider` chain rather than adopting a new algorithm. **Leiden is a gated future
fork** — recorded here as the next lever if weighted-LPA's gains prove insufficient, not built now.

## The pivotal why

ADR-061 removed the prior `SemanticProvider` decorator on two independent findings:

- **(a) No consumer.** Nothing read community structure for retrieval; `NewSemanticProvider` had zero
  non-test callers.
- **(b) Would not move primary search even if wired.** An independent trace of every graph-query strategy
  handler found the primary semantic search path ranks entities entirely from the embedding index;
  community membership was post-hoc decoration for summaries, load-bearing only on the text-fallback and
  local-search paths.

**(b) remains true and is not contradicted by this decision.** Wiring semantic edges still does not change
how `searchEntitiesSemantic` ranks entities for point queries. What has changed is **(a) has expired**:
community-level thematic/global GraphRAG search is now a real, measured consumer, and Epic B's arc — B0's
thematic-recall instrument, the #645 retrieval fix, and finally the white-box
`validate_partition_colocation.go` diagnostic (#656, merged) — is the recorded evidence admitting the
capability, not a speculative "might be useful" the way the original wiring proposal was.

The diagnostic measured, on the cheap 1.7b `task e2e:semantic` tier, with no LLM and no frontier model
involved in the signal itself: `partition_colocation_mean=0.60` across theme-spanning queries, with the
partition clustering cleanly by entity type/status instead — every completed `maint-*` entity in one
community, every `doc-*` in another, every `obs-*` in another, coverage clean (no entity missing from the
partition, five level-0 communities, not a degenerate collapse). The one query that co-locates perfectly
(cold-chain, 1.00) does so only because both its entities happen to be the same type. Quantitatively: a
maxed-out entity's sibling vote mass (`10 × 0.7 = 7.0`) plus system-peer vote mass (`15 × 0.3 = 4.5`)
structurally dominates typical explicit vote mass (`1–3 × 1.0`) in these corpora, so LPA's label vote is won
by type/status before a theme signal built from unlinked-but-related entities ever gets to compete. This is
the same reasoning ADR-061 itself anticipated as the "future trigger" condition it found absent at the
time ("There is no planned consumer that would [reach primary ranking]") — a consumer has since arrived, at
the *community* layer ADR-061 explicitly scoped its finding (b) to, not the primary-ranking layer.

A frontier-model probe (Gemini 2.5 Flash, `task e2e:semantic:frontier`) initially read as evidence *against*
this rebuild — matching local qwen3-8b on 4/5 thematic queries and still capping every 4-expected-entity
query at exactly 3/4 — but that comparison was **confounded** (PR #653 Codex review, finding #3): the
frontier and local runs each re-clustered independently at the pre-fix partition determinism of ~0.83, so
the observed recall delta conflated model quality with a possibly-different realized partition. The
white-box diagnostic needs no model and no re-clustering to be trusted; it traces where each *known* corpus
entity actually lands, directly. It reverses the frontier probe's apparent conclusion: the missing entity
on every capped query is structurally unreachable — a different type, in a different community — which no
improvement in synthesis quality can retrieve. This decision is grounded in that reversal.

## Consequences

- **New operator-facing config** (`enable_semantic_edges` + flat threshold/k/weight fields), defaulting off
  and, when off, byte-identical to today's sibling/system-peer edge behavior (the gh#461 default-
  preservation invariant extends unchanged to this new tier).
- **A second readiness watcher** (`readiness.KeyGraphEmbedding`) alongside the existing graph-index one,
  and a structural-floor fallback: a cold embedding index degrades semantic-edge synthesis for that cycle,
  never the detection run itself. Community detection remains the framework's tiered-graceful-fallback
  example — Tier-0/1 structural partition always commits; semantic edges are additive Tier-1/2; never fail,
  never empty, degrade and report.
- **A tiered-fallback contract on the embedding side**: `graph.embedding.query.similar`'s existing
  not-ready classification (`ErrorCodeIndexNotReady`, already true in code) becomes load-bearing for a
  second consumer beyond anomaly detection, and is seeded as current truth in the `graph-embedding` spec
  for the first time as a result.
- **A determinism fix** (seeded shuffle, deterministic vote tie-break) becomes a prerequisite of this
  change rather than a separately schedulable increment, because the colocation gate this decision arms is
  meaningless against a non-reproducible baseline.
- **Bounded, cached cost**: the semantic-edge build reuses a neighbor set across cycles when an entity's
  embedding revision is unchanged, keeping the added `graph.embedding.query.similar` traffic from scaling
  linearly with detection frequency on an otherwise-static corpus.
- **Cross-repo**: community membership becomes load-bearing for downstream global/thematic search for the
  first time. **SemSource** (the lead v1 product wiring `global_search` and Tier-2 seminstruct
  summarization) is notified — its retrieval quality is now directly a function of this partition's
  quality, where previously it was a function of the summary text alone.
- **What this decision explicitly leaves alone (B3, not this change)**: `COMMUNITY_INDEX`'s shared-mutable
  ownership problem (#606's ownership half) and the `EnhancementWorker`'s CAS/clobber/resurrection bugs
  (#607, #608, #617) are unaffected — this change builds and measures with `EnableLLM=false` (the B1
  interim), so that surface stays dormant.

## Outcome (2026-07-26)

The mechanism this ADR decided on shipped in full (§1–§7 of the change's `tasks.md`, plus the
§8.0 explicit-edge weight-correctness prereq, #665/#666) and **ships default-off**, exactly as
consequenced above. §8 then ran the compound-gate measurement the decision anticipated, and the
result is an **honest negative on the weight-tuning lever**, not a reversal of the decision itself:

- Enabling `enable_semantic_edges` and reweighting semantic edges above the structural tiers
  (semantic 2.5, sibling 0.35) DID raise `partition_colocation_mean` from the 0.60 type-partition
  baseline to 0.83. Two trust instruments added to harden the recorder
  (`distinct_plurality_communities`, `max_plurality_community_size`) then showed that rise was a
  **mega-community merge artifact** — 4 of 5 theme-spanning queries' expected entities were
  absorbed into one 47-member community, not genuinely co-located by theme.
- A **paired frontier decider** (Gemini 2.5 Flash held constant across both arms — the type
  baseline and the 2.5/0.35 semantic reweight — so partition was the only variable) measured
  thematic recall directly instead of trusting the co-location proxy. Recall was **flat**: 0.85 in
  both arms, per-query byte-identical, known-answer 7/7 in both, community summaries not
  truncated (286–339 chars, well under the cap, ruling out a summary-dilution artifact).
- A context-diff of the two arms' retrieval artifacts found the exact theme terms every capped
  query missed (`battery`, `evacuation`, `door`) are **identical across both partitions** and are
  present in theme-relevant corpus entities in both. The partition is off the recall path; the
  0.85 ceiling is upstream, in synthesis compression and/or the eval's literal-term matching. This
  **falsifies B2's founding premise** — that the missing entity is structurally unreachable
  because it lands in a different community than the query's other expected entities.

**Conclusion:** on this ~74-entity corpus, the mutual-kNN mechanism has no thematic-recall ROI via
weight-tuning. The compound colocation gate (`tasks.md` §8.1/§8.2) was measured and deliberately
**not adopted** — `validate_partition_colocation.go` stays a recorder, not a pass condition — and
the two trust instruments that exposed the merge artifact, plus a per-query dilution channel
(`summary_len`/`truncated`), landed as permanent recorder hardening via PR #698 (`304368d9`). The
mechanism itself is unchanged and stays merged, default-off, as a future lever: a different corpus
whose semantic neighborhoods do not collapse into one document-genre mega-community could still
realize the colocation gain this ADR targeted. Two levers are recorded as **out of B2 scope,
future work**: retrieval-side multi-community query expansion, and embedding-input shaping (the
absorbing mega-community's membership hints the embedding space organizes by document genre at
least as strongly as by theme).

This measurement refines the mechanism's *expected ROI* on the corpus tested; it does not
invalidate the Decision above, which was and remains about building a tiered, gated, additive
mechanism at the point the "no consumer" premise expired. See
`docs/proposals/prev1-program.md` and the change's `tasks.md` §8 for the full trace.

## Alternatives considered

1. **Leiden (rejected for v1, recorded as a gated future fork).** Leiden generally produces higher-quality,
   more stable partitions than LPA and would be a reasonable target if weighted-LPA's colocation gains
   prove insufficient once measured. Deferred rather than built now: it is a new detection engine (new
   dependency or implementation, new hierarchical-level semantics) layered onto a component whose edge set,
   config surface, and readiness contract are already changing in this increment; stacking an engine change
   on top would confound the measurement this change exists to produce. If weighted-LPA's compound gate
   (`colocation_mean` rise + coverage + non-degenerate cardinality) does not clear a bar the owner sets after
   this change lands, Leiden is the next lever, evaluated on its own increment against a known baseline.

2. **A single merged `VirtualEdgeProvider`** collapsing `EntityIDProvider` and the new `SemanticEdgeProvider`
   into one decorator, rather than a third link in the provider chain. Deferred as a follow-up refactor: the
   decorator-chain shape (`kvProvider -> EntityIDProvider -> SemanticEdgeProvider`) mirrors the existing
   gh#461 chain exactly, is independently testable per tier, and does not block this change. Worth revisiting
   once the weight-resolution seam (see the change's `design.md`) settles, since a merged provider is the
   natural place to hold the single resolved `WeightConfig` if the multi-tier max-not-sum resolution proves
   awkward split across two decorators.

3. **Do nothing (accept the measured miss).** Rejected: the white-box measurement is not ambiguous —
   `colocation_mean=0.60` with clean coverage on a healthy, non-degenerate partition is a structural
   defect, not noise, and it directly explains the frontier-ceiling cap this Epic B arc measured
   independently. Declining to act would mean the framework's community-detection primitive cannot serve
   its now-real consumer's thematic-retrieval use case, contradicting the Product Boundary's premise that
   SemStreams provides the graph substrate SemSource's GraphRAG retrieval depends on.

4. **Revive ADR-061's exact removed commit (`a60ef433`) rather than build fresh.** Rejected: that
   implementation predates the readiness contract (ADR-083/084/085), the current
   `EntityIDProviderConfig`/gh#461 config shape, and the multi-tier weight-resolution problem this change
   introduces (competing sibling/system-peer/semantic tiers did not exist when it was written — it only had
   explicit vs. sibling to reconcile). Reviving it would mean rewriting most of it immediately; ADR-061's
   own recoverability section already anticipated a fresh build "should a primary-path consumer ever land."

## References

- [ADR-061](061-community-semantic-virtual-edges.md) — the decision this reverses in part; premise (b) is
  unaffected, premise (a) has expired.
- `openspec/changes/graph-clustering-semantic-edges/` — the OpenSpec change this ADR is the decision record
  for (proposal, design, tasks, spec deltas).
- `test/e2e/scenarios/validate_partition_colocation.go` (#656) — the white-box measurement that reverses the
  confounded frontier-probe reading and grounds this decision.
- PR #653 (Codex finding #3) — the confound in the frontier-vs-local comparison that this decision's
  reasoning explicitly walks back.
- gh#606 (partition-quality/weighting half only), gh#618 (clustering-edge-consumer scope only) — subsumed by
  the linked change; gh#607/#608/#617 and #606's ownership half remain open, deferred to B3.
- PR #698 (`304368d9`) — the recorder-hardening trust instruments (`distinct_plurality_communities`,
  `max_plurality_community_size`, per-query `summary_len`/`truncated`) that exposed and evidence the
  [Outcome](#outcome-2026-07-26)'s mega-community merge finding.
- `docs/proposals/prev1-program.md` — the Epic B baton this increment reports back onto.
