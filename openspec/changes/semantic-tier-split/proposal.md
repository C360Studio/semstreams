## Why

**The `semantic` e2e tier tests two unrelated capabilities, so its red light cannot
tell you which one broke.** gh#830 is the proof: a `globalSearch` probe failed
intermittently, and it took five full tier runs and two disproved hypotheses to
establish that the retrieval path was fine (the batch load it blamed takes
**2.7ms** against a 5s budget) and the host was simply saturated by three
co-located LLM services. A tier that had separated the two would have answered
that on run one.

The confusion is not incidental — it is baked into the tier's name. "Semantic"
in this repo means two different things depending on who is speaking:

- **Inference Tier 2** (the ladder in `CLAUDE.md`): neural embeddings, which
  decide *what is related to what*.
- **Answer synthesis** (GraphRAG): an LLM writing prose about what was
  retrieved.

The e2e tier tests both under one name, gates on both, and saturates its host
doing the second.

### What Tier 2 actually buys you, stated plainly

This has been a recurring source of confusion and the split is the moment to fix
it in writing. The tiers are a **retrieval** ladder. None of them generate text.

| Tier | You can find an entity by… | Concretely |
|------|---------------------------|-----------|
| **0** | only what you explicitly asserted | you said `A relates-to B`, so the graph knows it. Nothing is inferred from text. |
| **1** | the **words** its text contains (BM25, pure Go) | searching `forklift` finds documents containing the token *forklift*. Vocabulary must match. |
| **2** | the **meaning** of its text (neural embeddings, external service) | searching `forklift` also finds *lift truck* and *pallet jack*. The docs' own example: **"Machine" matches "equipment" and "device"**. |

So **Tier 2 buys exactly one thing: matches across different vocabulary.** That
is the entire delta over Tier 1. It is not smarter, it does not reason, and it
does not write answers — it maps text to a vector where "means the same thing"
is close, so retrieval stops depending on the searcher guessing the author's
words.

Two consequences that keep getting lost:

- **Tiers only affect entities with text.** Telemetry-only deployments behave
  identically at all three tiers, because there is nothing to embed. Tier 2 on a
  sensor-only graph buys nothing and costs an embedding service.
- **The LLM is not on this ladder at all.** Answer synthesis is a separate axis:
  retrieval decides *what is relevant*, the LLM decides *how to say it*. You can
  have excellent Tier 2 retrieval and bad answers, or vice versa — which is
  precisely why one tier gating both produces an unattributable failure.

### Why CI cannot run it today

Measured: the tier runs `semembed` plus **three** `seminstruct` LLM services
(0.6b, 1.7b ×2) and saturates a 12-vCPU host — one run logged **18** LLM
answer-synthesis timeouts. A free GitHub runner on a public repo has **4 vCPU**.
Disk is not the constraint (~3.6GB of images against ~14GB); CPU is.

The consequence is gh#769: the tier is manual-only, so its last known green was
the beta.159 tag and a regression could sit undetected for weeks. gh#830's
failure rate is **2 in 5** — a rate that is invisible without automation and
untrustworthy on any single hand-run observation.

## What Changes

- **Split the compose profile by capability.** The scenario code is *already*
  split (`tiered_semantic.go` for embeddings and community enhancement,
  `tiered_semantic_known_answer.go` for answer synthesis); only the single
  `semantic` compose profile lumps all four ML services together.
  - `semantic` — `semembed` + the smallest LLM needed for community summary
    enhancement (`tiered_semantic.go:543` is the sole LLM dependency in the core
    scenario). Target: runnable on 4 vCPU.
  - `semantic-rag` — adds the 1.7b answer/summary services and the known-answer
    probes. Stays local/nightly; its own code comment records that an
    answer-synthesis round trip "legitimately takes tens of seconds".
- **Give each tier a written diagnostic contract** — what a red light means, and
  what it rules out. This is the deliverable, not a side effect: the reason
  gh#830 cost five runs is that neither tier had one.
- **Document the tier ladder in plain terms** where a reader will hit it, and
  state explicitly that the LLM is not a tier.
- **Enable `semantic` in CI** if and only if the measurement below supports it.

## Open question this change must answer before wiring CI

**Can 4 vCPU run `semembed` + the 0.6b model within the tier's timeouts?**
Nobody has measured it. The proposal does not assume the answer — it is settled
by running the proposed CI stack locally under a `--cpus=4` constraint, which is
the same empirical method that root-caused gh#830.

If the answer is no, the fallback is `semembed`-only in CI with the community
enhancement assertion moved to `semantic-rag`, and that trade — losing LLM
community-enhancement coverage per PR — gets recorded rather than absorbed
silently.

## Impact

- **Affected specs:** new `e2e-tiers` capability (diagnostic contracts, tier
  composition).
- **Affected code:** `docker/compose/tiered.yml` (profiles), `taskfiles/e2e/semantic.yml`
  (variants), `.github/workflows/` (CI wiring), `docs/concepts/00-real-time-inference.md`
  (plain-language tiers + the LLM-is-not-a-tier statement).
- **Not affected:** any production inference behavior. This is test-topology and
  documentation; no tier's runtime semantics change.
- **Closes/advances:** gh#769 (nightly + CI for semantic), gh#830 (removes the
  saturation that causes it), gh#811 (a tier that can actually gate).
