# Pre-v1 core hardening — program entry point

**This file is the baton.** Read it first, do the Next Action, update it last.
It is the only file that carries program state across sessions.

Opened 2026-07-21 · baseline `v1.0.0-beta.157`

---

## Next action

> **EPIC C OPENED + REDRAWN (2026-07-28, owner-decided) — the next epic-level WIP-1 item. Epic C is now
> "operational / derived-KV-plane state-ownership," NOT the original 2026-07-21 leftover-issues bucket.**
> RATIONALE (owner): the split is by STATE CATEGORY, not by file. The Codex projection-contract arc
> (#686→#687→#696→#704→#708) governs the AUTHORITATIVE entity-fact plane (contract-bound mutation client in
> `pkg/projection` over ENTITY_STATES triples — verified: it has ZERO refs to `update_kv`/`ENTITY_SUFFIX`/
> `FrameworkOwnedBucket`, a disjoint seam). NOBODY governs the operational/derived-KV plane — and #622 is the
> indictment (retention guard covers 2 of 17 buckets). B3 proved the pattern on ONE instance (single-writer +
> content-addressing killed a resurrection class structurally); Epic C GENERALIZES it. That's a real epic
> identity.
>
> **SCOPE (verified against HEAD 2026-07-28):**
> - **#622 = the PRIMITIVE, increment-0, START NOW.** Generic `update_kv` write-owner guard + FULL bucket
>   coverage. Live bug confirmed: `ENTITY_SUFFIX_INDEX` created at `component.go:1154`, ABSENT from
>   `FrameworkOwnedBuckets()`, so a rule can legally `update_kv` into it TODAY. **ARCHITECT SCOPE (verified HEAD):**
>   inc-0 = asks 1 (write-ownership: add the bucket constant + list entry) + 2 (coverage). **Ask 3 (ObjectStore
>   analogue) is ALREADY DONE** — `storage/objectstore/retention.go` (PR #632/#636) + `graph-retention` spec
>   Requirement `:47` cover every store incl. AGENT_CONTENT/embedding-content; DROP from scope, verify+close.
>   **Ask 4 already satisfied** — the KV guard already reads *existing* server config via `bucket.Status()`, not
>   requested defaults. **Design = reconcile-then-assert** (mirror the ObjectStore precedent: strip a foreign TTL
>   → self-heal+WARN → re-read → fail-closed on the shared `CheckNoLifecycleRetention` predicate) — NOT pure-assert,
>   which would multiply graph-ingest's process-lifetime-sticky takedown blast radius 17×. Additive, not breaking
>   (no shipped config writes the bucket). Exclude `EMBEDDINGS_CACHE` from the retention sweep (keep it
>   write-protected) so #622 doesn't pre-empt the storage-limits epic's cache-bounding. Spec homes: `graph-retention`
>   (broaden the ENTITY_STATES-specific requirement to the full owned set) + seed write-ownership into `nats-kv-keys`.
> - **#625 = embedding cleanup repair loop, START NOW (pairs with #629).** graph-embedding plane, zero contact
>   with any in-flight change (architect grepped all 7). Port graph-index's `repairLoop` precedent. Consumes the
>   #622 primitive. Dependency: `#622 → #625`.
> - **#629 = coalescer resurrection. IMPL-HOLD FALSIFIED (architect, verified HEAD) — owner: ship with #625.**
>   The resurrection lane is **graph-embedding** (`cache.CoalescingSet` → `processEntityBatch` discards revisions →
>   `SavePending` unconditional Put defeats the gh#614 drop-guard), NOT `graph-index/revision_coalescer.go` (which
>   RETAINS greatest-revision by design → not vulnerable; the issue/baton conflated the two). NO active change
>   touches graph-embedding, so under the mechanical rule #629 needs no hold. Design the ordering protocol (durable
>   revisioned delete-marker + CAS-create, resting on #622's owned+retention-free `EMBEDDING_INDEX`) then implement
>   with the #625 pair. Dependency: `#622 → #629`. Low urgency: opt-in-only (`coalesce_ms > 0`, no shipped config).
> - **~~#527~~ DROPPED from Epic C** — it IS `graph-index-replacement-semantics` (15/19, in-flight sister/Codex:
>   composite-key contract freeze + durable NAME/PREDICATE/source-owned-INCOMING reverse projection, co-activates
>   with `predicate-raw-key-representation`/ADR-078). Pointer hygiene DONE: baton + MEMORY.md idx + rollout hub +
>   retention hub + gh474 hub all updated so a fresh session can't resurrect #527 as orphaned "NEXT" work.
>
> **MECHANICAL COLLISION RULE (owner, replaces "hold until quiesce"): Epic C builds ONLY against merged main.**
> The moment an item needs an in-flight surface, that item HOLDS automatically (the beta.135-pin move that made
> the semdragon stack safe). **Predicate/vocabulary surface is OUT OF SCOPE BY WRITTEN CONSTRAINT** — the change
> proposal must state Epic C touches no predicate/vocab surface, which makes the in-flight BREAKING
> `predicate-contract-enforcement` (38/44) deterministically irrelevant to it.
>
> **CROSS-CHANGE COLLISION to coordinate (architect D3): `bounded-storage-operability` (0/35, BREAKING,
> not-started) is a proposed retention epic that edits the SAME `graph-retention` spec file AND wants a
> `DiscardNew` `MaxBytes` emergency ceiling, which #622's `CheckNoLifecycleRetention` predicate rejects
> (`MaxBytes>0` forbidden). The "build vs merged main" rule doesn't catch it (it's a proposed SPEC, not merged
> code). OWNER-DECIDED: #622 inc-0 lands the strict status-quo invariant ONLY (extends coverage, touches no
> `MaxBytes` policy), sequences FIRST on the `graph-retention` spec; the `DiscardNew` carve-out is deferred
> entirely to `bounded-storage-operability`, which rebases its delta onto #622's broadened requirement.**
>
> **inc-0 (#622) MERGED + ARCHIVED (PR #716 `d03c49f7`) — then REOPENED same day on a Codex retrospective
> (2 P0s: the "post-start" sweep raced async component start; component Start failures never failed
> boot/health) and RE-CLOSED via `reopen-framework-owned-bucket-guards` (PR #719 `a4287869`, MERGED +
> ARCHIVED 2026-07-28, THREE Codex rounds): component-start barrier + fail-closed boot + health StateFailed;
> boot-boundary config drain (mid-boot adds/edits/registry changes join the boot transaction; cutoff
> honesty: post-drain changes = dynamic class, durable closure = acquisition seam); EMBEDDINGS_CACHE +
> cache_ttl + 4 unconsumed lifecycle hooks DELETED (loud rejection of stale configs; adopter note
> `docs/operations/embeddings-cache-removal.md` = the sole sister migration channel — we do NOT edit
> sister repos). The primitive is now REAL.**
>
> **SHIPPED 2026-07-28 (all three of the previously-sequenced items):
> (1) cache-CLASS deletion — PR #721 `f7965f0e` (Cache iface/NATSCache/HTTPConfig.Cache + branches +
> doc example gone; cache.go→dedup.go preserving the LIVE dedup-identity surface; `ContentHash` now
> caller-less, retained pending owner word).
> (2) #625 + #629 — change `embedding-derived-state-convergence`, PR #722 `2b532d76` (archived
> 2026-07-28): hop-1 single-writer seam (`hop1Mu`) + reconcile-at-execution closes #629 structurally
> (semsource defaults `coalesce_ms=200` — the issue's "dormant" framing was FALSIFIED); #625 =
> markStranded into the #613 failed map + dedicated 30s repairLoop, zero durable evidence. ONE Codex
> round: floor-0 marks were clearable by superseded hop-2 terminals → replaced by the CAUSAL
> strandedAt invariant (design.md records the falsification); `SavePendingGuarded` (CAS decision
> table) closes the stale-downgrade write side; coalescer now publishes before the watcher.
> Net durable-state delta: ZERO. `configs/statistical.json` carries `coalesce_ms:100` — first-ever
> e2e coverage of the coalesced lane. Lesson memo: convergence marks clear causally, never by
> "any later event."**
>
> **EPIC C STRUCTURAL LEG COMPLETE (2026-07-29): `framework-bucket-catalog` MERGED `eff3927f`
> (PR #724, archived 2026-07-29).** The 22-row descriptor catalog (graph/kvcatalog.go) is the ONE
> source; FrameworkOwnedBuckets() is a derived view (owned set 19→21: +OWNER_CLAIMS/+OWNER_PRESENCE);
> Ensure/OpenFrameworkBucket is the acquisition seam (Kind+params retention, fail-closed on every
> discriminated field); the post-start sweep pass is DELETED, the pre-start pass demoted to the
> owner-absent-from-composition backstop; an AST contract test bans catalog-bucket literals outside
> the catalog. Two live bugs died red-first (F1 ENTITY_STATES History boot-race, owner-decided
> History=1; F2 operator-named framework buckets now fail boot off-catalog). #714 + #717 CLOSED by
> design. ONE Codex round (clean-boot tool loss → lazy execution-time bind; cross-owner seam →
> exact-four Validate+belt; fail-open enums → explicit arms). FOUR e2e tiers green incl. agentic.
> BREAKING: graph/query.Config bucket fields deleted; readers fail not-ready naming the owner;
> adopter note `docs/operations/framework-bucket-catalog.md`. Follow-ups: #725 (hello-world config,
> pre-existing); sister-sweep before deleting caller-less `natsclient.AssertNoLifecycleRetention`;
> graph-inference RetentionDays/CleanupInterval phantom pair; census-exempt constant-based readers →
> Open (mechanical).
>
> **(1) RESOLVED 2026-07-29 — NOT a rebase. `bounded-storage-operability` is RETIRED (deleted, 0/35,
> never started) and SUPERSEDED by `storage-capacity-observability` (4/4 artifacts, validates
> `--strict`, 0/35 tasks).** The planned "add a `RetentionDiscardNewCeiling` Kind + fill rows" was
> FALSIFIED by direct measurement, not by staleness: against real NATS 2.12 (KV `MaxBytes` 128 KiB,
> `History` 1), at the ceiling NATS **rejects replacing an existing key** (`code=503 err_code=10077`)
> while still accepting deletes and purges — a same-size replacement is net-zero bytes under History 1,
> but the append is checked against `MaxBytes` BEFORE the superseded revision compacts away. So (a)
> "reserve replacement headroom" is not expressible against a single append-checked limit — replacement
> is the FIRST thing to fail; (b) graph-ingest writes one entity across many independently-ceilinged
> buckets with no cross-bucket transaction, so a ceiling doesn't stop growth, it TEARS authoritative
> state against derived indexes at a nondeterministic point; (c) it inverts ADR-068 — delete (the
> semantic op we reserve) still works while update is denied by storage policy. **ADR-073:77-79's ban
> on all `MaxBytes>0` on the identity tier STANDS, now independently confirmed; the retired change's
> task to "update ADR-068/073 wording" would have re-opened it.** Owner steer that shaped the
> replacement: *observability with prior warning first — "we do not want to turn off new writes unless
> we have given them the chance to correct resources."* New change = inventory + growth/headroom/
> time-to-threshold + pressure states, REPORT-ONLY; explicit bounds on ordinary time-shaped streams
> ONLY; a **provisioner prefix guard refusing `KV_*`/`OBJ_*`** (the load-bearing safety boundary —
> `createStream` has no name filter today and extending the reconciler to retention fields is what
> would make an operator typo stamp age eviction onto graph state); expiring migration overrides.
> Application-level admission in graph-ingest (entity-atomic, cross-bucket-coherent — the thing NATS
> `MaxBytes` structurally cannot be) is DEFERRED behind a checkable 3-condition gate. Architect
> adversarial review returned 3 BLOCKERS (all on the exclusion boundary) + 2 HIGH + 8 MEDIUM — all
> folded in before validate. Probe preserved at scratchpad `ceiling_probe_test.go.txt`. Also filed
> **#727** (objectstore JetStream handler acks unconditionally after a store call that cannot report
> failure → transient failure = silent permanent loss; independent of capacity, split out so it
> wasn't lost with the retired change). **(2) #712 projection quiescence (semdragon ask) —
> third consumer of the readiness substrate; extend the ADR-083 producer pattern to inference stages
> + aggregate over GRAPH_STATUS, NOT a new readiness system. (3) Complexity-pivot remainder:
> adopter module contract (one Register bundling payloads/vocab/factories/projections) + `--validate`
> doing real registry composition + tutorial configs compiled in CI (#725 is the motivating case) +
> docs rewrite LAST against the simplified surface.**

> **EPIC B COMPLETE (2026-07-28) — B3 MERGED (PR #709 `857988ef`, archive #711). B0/B1/B2/B3 ALL CLOSED.
> This is the newest state; the recall-ceiling note below is now history.** B3 = the community-summary
> ownership split: LLM summaries moved from the shared `COMMUNITY_INDEX` record to a worker-exclusive,
> content-addressed `COMMUNITY_SUMMARIES` store keyed `{level}.{membership_hash}` — structurally closing
> the #607 clobber + #617 resurrection classes (no CAS: a content-keyed write can't touch the
> detector-exclusive partition; an unchanged membership is a cache-hit skip). graph-query joins
> partition+summary by membership hash with a statistical floor; readiness gated on `COMMUNITY_INDEX` only.
> ADR-087 (ownership decision + the membership-vs-content staleness trade). BREAKING; frontier e2e gate
> GREEN on the FINAL code (`llm_enhanced=14/pending=0`, determinism 1.00, known-answer 7/7, recall 0.95,
> `validation_errors:0`). **Codex found 3 real issues the internal reviewer missed (2 HIGH):** late
> summary-bucket attach on a rolling upgrade → stuck on the statistical floor forever (fixed: independent
> retrying resource watcher); a lagging `llm-failed` write clobbering an `llm-enhanced` summary (fixed:
> CAS `PutFailedUnlessEnhanced`); +MEDIUM gauge-init on restart — all fixed + integration-proven + a
> confirming frontier re-run. Two frontier runs surfaced two observability PHANTOMS (an e2e stage + a
> metric writer still reading the emptied old `COMMUNITY_INDEX` field) — both migrated to the new store.
> Ship-time follow-ups filed: **#710** (worker-owned bounded-GC for the content-addressed store, gated on
> the size gauge) + **#661 reframed** to re-measure-after-B3. Lesson (reinforced twice this arc): a same-run
> reviewer APPROVE is NOT the last word — Codex caught real HIGH correctness bugs on both #702 and #709;
> the once-through Codex gate earns its keep. **NEXT (fresh session): Epic B fully closed — pick the next
> epic-level WIP-1 item: Epic C (derived-state ownership, #622/#527/#625/#629) vs deferred Epic A
> (#619/#633/#643) vs #701 (multi-community query expansion) vs #710 (summary-store GC). Reconcile at
> start: git HEAD + `gh issue list` + `openspec list`.**

> **RECALL-CEILING FIX MERGED + ARCHIVED (2026-07-27, PR #702 `f9833ace` + archive #705 `669790d6`) — front CLOSED (history). Superseded the "next front"
> framing in the B2 note below.** The owner's question "why don't communities resolve as expected?" is
> answered: the 0.85 thematic-recall ceiling was NEVER a partition/community problem — it was
> synthesis-projection lossiness. GraphRAG answer synthesis fed the LLM only {community summary + up to 5
> PageRank (query-AGNOSTIC) rep titles + 5 keywords}; entity bodies never reach the prompt, and a theme
> term on a relevant NON-rep member's title/tags could not appear. OpenSpec change
> `thematic-synthesis-context` fixes it with two query-side levers: (A) select digest reps by QUERY
> RELEVANCE (`semanticScores` handleStrategyGraphRAG already computes), full PageRank fallback preserved;
> (B) a capped `Tags` channel from `content.classification.tag` triples in both the LLM prompt and the
> template floor. Architect-designed, developer-built, reviewer-APPROVE (2 MEDIUM + 2 NIT fixed; MEDIUM-2
> promoted the tag predicate to framework constant `vocabulary.ContentClassificationTag`). **Frontier e2e:
> `thematic_theme_recall_mean` 0.85 → 0.95, `battery`+`door` recovered, ZERO regressions (known-answer
> 7/7, partition determinism 1.0).** Additive/non-breaking. `evacuation` (fire) stays missing — its only
> tag-bearing entity sits in a document-type community that doesn't reach the fire query's top synthesis
> clusters (cross-community coverage, NOT a tags defect); owner chose LAND-THE-WIN + filed **#701**
> (retrieval-side multi-community query expansion). Codex reviewed #702 once (3 findings: P1 type-filter
> leak via the score map, P2 fallback cap, P2 baton wording) — ALL addressed (score map now keyed by
> surviving `entityIDs`; fallback capped-copy; wording fixed) + CI green, then MERGED + ARCHIVED (#705).
> **NEXT (fresh session): recall-ceiling front is DONE — pick the next epic-level WIP-1 item: B3 (ownership
> split, Tier-2, #607/#617) vs #701 (multi-community query expansion) vs Epic C vs deferred Epic A
> (#619/#633/#643).** Codex/sister-session own the separate projection-contract + predicate-audit stack
> (#686←#687←#696←#704, #685) — merged bottom-up after sister review; NOT this thread. Full detail:
> memory `epic-b-recall-ceiling-synthesis-context.md`.

> **B2 CLOSED (2026-07-26, honest negative) — SUPERSEDES the "B2 IS DECIDED ... HAS ROI" framing
> immediately below.** The weight-tuning lever is exhausted: enabling the merged semantic-edge mechanism
> raised `colocation_mean` 0.60→0.83, but a paired frontier decider (Gemini 2.5 Flash held constant,
> partition the only variable) found thematic recall FLAT — 0.85 both arms, per-query byte-identical,
> known-answer 7/7 both — because the colocation rise was a mega-community merge artifact
> (`distinct_plurality_communities=2/5`, one 47-member community absorbing 4/5 themes), not genuine
> theme coherence, and the partition sits off the recall path entirely (a context-diff showed the same
> missed theme terms present in-corpus under both partitions; the 0.85 ceiling is upstream, in synthesis
> compression / eval literal-term matching). This falsifies B2's founding premise. The mechanism (§1–§7,
> §8.0 of the change) ships MERGED + DEFAULT-OFF as a future lever for a different corpus; the compound
> colocation gate was measured and deliberately NOT adopted — the recorder stays record-only, hardened
> by two trust instruments landed via PR #698 (`304368d9`). Two levers recorded out of B2 scope, future
> work: retrieval-side multi-community query expansion, and embedding-input shaping (the absorbing
> community's membership hints the embedding space organizes by document genre at least as strongly as
> by theme). Full detail: ADR-086 "Outcome (2026-07-26)",
> `openspec/changes/graph-clustering-semantic-edges/tasks.md` §8, and memory
> `b2-partition-clusters-by-type-not-theme.md`. **The historical trace below (sessions 8–11) stands
> as-is and is not re-derived — read it for how B2 was scoped and built, not for its "HAS ROI"
> conclusion, which this note corrects.**

> **B2 IS DECIDED — partition rebuild HAS ROI. The confounded frontier "low-ROI" hypothesis is REVERSED
> by a white-box, LLM-independent measurement (session 11, 2026-07-24).** The owner course-corrected off the
> frontier comparison (a black-box proxy; #654 re-scoped): with a KNOWN corpus we don't need a frontier model
> to define "good" — we trace where each known-expected entity lands in the partition. New diagnostic
> `test/e2e/scenarios/validate_partition_colocation.go` (stage `validate-partition-colocation`, built + reviewed
> + unit-green; RECORDER, cheap 1.7b `task e2e:semantic`, no 8B/frontier). It reads `GetAllCommunities` and, per
> B0 thematic query, measures whether the known-expected entities are co-located in one level-0 community.
>
> **The finding (decisive): the partition clusters by ENTITY TYPE/STATUS, not by THEME.** All completed
> `maint-*` → one blob; all `doc-*` → one blob; all `obs-*` → one blob; all sensor-docs → one blob (driven by
> entity-id sibling edges dominating the LPA). Coverage is perfect (`entities_not_in_community=0`, all 18
> present; trust guard clean — 5 communities, not collapsed). So any theme that spans entity types SCATTERS:
> forklift 0.40 (maint+obs+doc across 3 comms), conveyor 0.33, fire 0.50, dock 0.75; cold-chain is the ONLY
> co-located query (1.00) because both its entities are the same type (sensor-docs). `colocation_mean=0.60`.
> This EXPLAINS the frontier "caps at 3/4" — the missing entity is a different type in a different community,
> so retrieving "the relevant community" structurally cannot reach it; a better synthesizer can't fix a
> theme-blind partition. **⇒ B2 (ADR-061 semantic-kNN co-location edges that cross type boundaries) is the
> right lever, un-confounded.**
>
> **NEXT (sequencing):**
> 1. **Land the B2 diagnostic PR** — `validate_partition_colocation.go` + `_test.go` + `tiered.go` registration
>    (+ the `enhancement_workers: 2` pin in `configs/semantic-8b.json`, #652 quick-win, bundled). Test-instrumentation
>    + config only; unit-green + lint-clean. Merge gate = Codex → addressed → CI green (owner-run Codex).
> 2. **Scope the B2 build (architect-owned)** — re-add ADR-061 semantic-kNN edges + #618 readiness gate; the
>    open design question is how to weight semantic edges against the entity-id sibling edges that currently
>    dominate (theme vs type). Success metric already instrumented: `colocation_mean` should rise on
>    theme-spanning queries. Write **ADR-08x** (semantic partition + readiness; promote ADR-061) now that B2 is scoped.
> 3. **B1 determinism** — this run measured `1.00` on the ~74-entity corpus (run1=5/run2=5); the 0.83 was the
>    ~175-entity corpus. So B1's determinism piece is a LARGER-CORPUS phenomenon, not universal — verify on the
>    bigger corpus before treating as a live gap.
> 4. **#652 de-prioritized but CORROBORATED.** This 1.7b run took stage-20=2m25s / stage-21=4m28s with LLM
>    `pending=9`, and `validate-globalsearch-known-answer` (stage 42) then TIMED OUT on graph-query `loadEntities`
>    under that sustained load — the SAME enhancement-oversubscription (5 workers vs 2 slots) biting the 1.7b tier,
>    not just 8B. The standard `configs/semantic.json` also defaults `enhancement_workers=5` (unpinned). Extending
>    the pin to the standard config is a candidate #652 follow-up. Full admission-gate consolidation stays architect-owned.
>
> Default `task e2e:semantic` stays 1.7b (fast CI gate); 8b/frontier are optional quality reads. The B2 decision
> needs NEITHER — the co-location signal is LLM-independent, so it's read on the cheap tier.
>
> **THE RE-MEASURE (the whole point — done; measure-before-building paid off AGAIN).** The #645 fix
> RESOLVED the dominant thematic-retrieval defect:
> - **dim1 `empty_answer_count=0/5` (was 4/5)** at BOTH tiers — every question now retrieves
>   (count=68) and reaches synthesis. First clean thematic read achieved.
> - **8B synthesis quality confirmed GOOD once retrieval works:** all 5 grounded, no fabrication;
>   recall 0.50–1.00 (cold-chain/conveyor 1.00; dock-equipment **0.50** weakest).
> - **1.7b `task e2e:semantic` full 48-step gate GREEN (exit 0)** on the branch, incl. the
>   known-answer step — proves the fix does NOT regress the standard CI tier.
>
> **Two prior-claim CORRECTIONS the re-measure forced (do not carry the old ones forward):**
> 1. **Partition determinism is 0.83, NOT 1.0** (run1=6 comms / run2=5, level0=5, 175-entity corpus).
>    The session-9 "already 1.0" was on the ~74-entity corpus. **B1's determinism piece is a LIVE gap**,
>    not low-leverage.
> 2. **Recall spread 0.50–1.00 is now MEASURABLE** (was invisible behind the zeroing) — that spread,
>    esp. dock-equipment 0.50, is the concrete **B2 coherence signal**. Scope B2 against it.
>
> **8B harness caveat (capacity, not correctness).** `task e2e:semantic:8b` itself ran RED end-to-end:
> two qwen3-8b on a 23.2 GiB box SATURATE — `validate-llm-enhancement` left `pending=10` after its
> 10-min budget, total run 33m (vs ~10m), and `validate-globalsearch-known-answer` (step 41) failed
> with a server-side **`EOF`** on the graphql POST (2/3 probes). The B0 eval (step 21) passed and the
> SAME step passes at 1.7b → capacity artifact, NOT a regression. Container logs were LOST to the
> `defer docker compose down -v` before OOM-vs-server-timeout could be confirmed — next 8B run must
> stream container logs or use `task e2e:semantic:debug` (no teardown). Owner call needed on 8B-harness
> viability (one shared 8b? more RAM?). See [[reference_seminstruct_8b_for_graphrag_measurement]].
>
> **The B2 DECISION — now armed with a FRONTIER-CEILING probe (session 10, `task e2e:semantic:frontier`,
> Gemini 2.5 Flash routing answer_synthesis + community_summary; cloud YARDSTICK, not the offline
> product path).** Full 48-step scenario GREEN in 3m42s (remote synthesis → no 8b saturation; known-answer
> passed → reconfirms the 8b EOF was pure capacity). **Result: Gemini MATCHED local qwen3-8b on 4/5 thematic
> queries; it only lifted the weakest (dock-equipment 0.50→0.75, one entity). Every 4-expected-entity query
> (forklift/fire/dock) caps at exactly 3/4 EVEN at the frontier.** Reading: (1) **synthesis quality is NOT
> the bottleneck — the edge 8b appears within ~1 entity of a frontier model** on this corpus, consistent with
> the tiered-fallback story (cloud not required); (2) the "misses exactly one" that survives a frontier model
> *looks like* a NARROW retrieval/partition residual. **BUT the comparison is CONFOUNDED (Codex, PR #653): the
> frontier and local runs were SEPARATE runs, each re-clustered from clean at level-0 determinism ~0.83, so
> the recall delta conflates model quality with a possibly-different realized partition.** So **"B2 rebuild is
> low-ROI" is a HYPOTHESIS, not a finding** — a rigorous answer needs a FROZEN `COMMUNITY_INDEX` evaluated by
> both synthesizers in ONE paired run (filed as a follow-up). B1 (determinism 0.83) is still a live gap. Also
> single run, ~74-entity corpus, 5 queries, recall coarse at N=4. (Gotcha that made the probe valid: `gemini-2.5-flash` is a thinking model; the hardcoded 200-tok
> community_summary cap truncates under thinking → needs `reasoning_effort: none`, which exposed a graph/llm
> gap — see log.)
>
> **Original B0–B3 plan (stands, but now GATED by the #645 re-measure):**
> - **B1** — `WithLevels(1)`, `EnableLLM=false` (interim, re-enabled B3), deterministic partition,
>   fix dead `GetEdgeWeight`, cold-start detection, ready-latch + phantom-`IsAvailable` fix,
>   non-empty degraded floor. #607 #608 #609 #617.
> - **B2 (may be unnecessary — the re-measure decides)** — re-add ADR-061 semantic-kNN edges +
>   #618 readiness gate. Engine checkpoint weighted-LPA vs Leiden. #606 #618.
> - **B3** — ownership split: `COMMUNITY_INDEX`=partition (detector-exclusive) + new
>   `COMMUNITY_SUMMARIES` keyed `{level}.{membership_hash}`; re-enable `EnableLLM`→seminstruct. #607/#617.
> - **B0 converts to gates:** the instrument RECORDS today; B1/B2/B3 each convert their dimension
>   (stability/floor→B1, grounding/theme-recall→B2, Tier-2→B3) to a HARD gate — report-only past its
>   increment is the drift.
> - **Deferred (not built):** B0 Layer-2 (semsource scorecard `thematic` band + `global_search` MCP
>   proxy); **ADR-08x** (semantic partition + split store + readiness; promote ADR-061) — write when B2 scoped.
>
> **Run the valid measurement:** `task e2e:semantic:8b` (HEAVY local: ~20GiB Docker RAM, two qwen3-8b
> for answer_synthesis + community_summary; ~10min). Instrument: `test/e2e/scenarios/validate_thematic_eval.go`.
>
> **TENET (owner, hold throughout):** semstreams is TIERED with GRACEFUL FALLBACK — structural
> partition + statistical summaries are the correct Tier-0/1 output; semantic edges + LLM narratives
> are additive Tier-1/2; never fail/empty, degrade + report. Graded by B0.
>
> (Epic C / deferred Epic A #619 #633 #643 remain queued behind Epic B.)

---

## How to read this file

Status words lie. That is the central finding of the audit that created this
program, and it applies to project tracking too. **Every row's state is a command
you run, not an adjective someone typed.** If the command disagrees with the
table, the command is right — fix the table.

```bash
gh issue list --state open --limit 100        # what is actually open
openspec list                                 # what is actually in flight
gh run list --workflow=sister-validation.yml  # whether the gate is actually running
git status --porcelain                        # whether the tree is actually clean
```

## Session protocol

1. **Start** — read this file, then run the four commands above and reconcile.
2. **Work** — the Next Action. One thing.
3. **End** — update the table from checkable state, then rewrite Next Action.

**WIP = 1 at the epic level.** Eight OpenSpec changes are already stalled in the
80–95% band; the last increment of each is disproportionately the observability
and guardrail work this program is about. Adding parallel epics is how this
becomes change number twelve through sixteen. Finish, then start.

---

## Issue flow (measured — update when touching the queue, at least weekly)

Purpose: keep mint-vs-close **measured, not smelled**. Discovery during a hardening
program is the program working (filing makes latent defects visible — the alternative
is the same defects, invisible); divergence is only real if the watch conditions below
trip. Regenerate the table with:
`gh issue list --state all --limit 300 --json number,createdAt,closedAt` bucketed by ISO week.

| Week | Opened | Closed | Net | Note |
|---|---|---|---|---|
| 2026-W26 | 24 | 33 | −9 | |
| 2026-W27 | 57 | 41 | +16 | audit prep wave |
| 2026-W28 | 15 | 16 | −1 | steady state |
| 2026-W29 | 25 | 23 | +2 | steady state |
| 2026-W30 | 73 | 23 | +50 | deliberate: pre-v1 audit filing + Codex projection-arc asks |
| 2026-W31* | 24 | 11 | +13 | partial week; #737 merge closes 2 more |

Composition 2026-07-30 (post-triage, post-confirm-close): 113 open = **32 bug / 77
enhancement / 4 docs-class** (56 previously unlabeled triaged; owner CONFIRM-CLOSE executed
same day: #622/#615/#617/#666/#654 closed with evidence comments against the merged epic
ledgers, #729/#730 auto-closed by the #737 merge, and 6 partially-fixed issues retitled to
their verified remainders — #621/#609/#608/#606/#618/#619; #199 folded into #736). Closure speed: days-to-close
median **1d**, p75 5d; open-backlog median age 10d.

**Dry criterion (program-exit gate):** two consecutive hardening increments surfacing
zero new P1+ defect-class finds ⇒ discovery has converged; the remaining queue is
asks/design work, sequenced post-v1 or by product need.

**Watch conditions (either one makes the divergence concern real):**
1. A closed class reopens (a second #719-shaped retrospective on the same guarantee).
2. Per-increment defect finds stop trending down in severity or count.

---

## Evidence (stable — do not re-derive)

| Doc | Holds |
|---|---|
| `prev1-audit-synthesis.md` | reconciliation of two independent audits; convergent findings, disagreements resolved, the merged plan |
| `prev1-graph-core-audit.md` | Claude-side detail: the 13 phantom signals, per-subsystem findings with file:line |

Findings in those docs are **settled evidence**. If a session re-derives them, that
is wasted budget. Cite and move on.

---

## Track 0 — MERGED (PR #624 → `5e80f676`, 2026-07-21)

One PR. No OpenSpec change; ceremony on mechanical fixes is how they reach 90% and
stop. Eight fixes shipped; three reviews (two internal + Codex) folded in; the
`!`-marked BREAKING change had all three e2e tiers green before merge. Follow-ups
filed instead of scope-creeping the PR: **#623** (Epic A) and **#625** (Epic C).

| # | Fix | Issue | Done when |
|---|---|---|---|
| 1 | rule processor: delete the ENTITY_STATES create path | #610 | issue closed |
| 2 | message-logger: `GetKeyValueBucket`, return 404 | #611 | issue closed |
| 3 | dedup key: fold in embedder type + model + cap | #612 | issue closed |
| 4 | two phantom e2e metrics | #615 | issue closed |
| 5 | `WithWorkers(max(1, …))` or explicit `workers` field | #620 | issue closed |
| 6 | tombstone branch → `DeleteEmbedding` | #614 | issue closed |
| 7 | sort before truncating at `graphrag.go:1176` | #621 | issue closed |
| 8 | `GenerateQuery` read-only (stop corpus pollution) | #619 | issue closed |

State: `gh issue view 610 611 612 614 615 619 620 621 --json number,state`

Rows 5–8 are deliberately **slices**: #619 keeps its index-vs-stateless-TF
decision, #620 keeps the ~400–500 LOC phantom-deletion bundle, #621 keeps items
2–5, #614 keeps part 2 (revision CAS). Those remainders belong to the epics.

### Track 0 implementation state (2026-07-21, uncommitted)

All eight implemented. `semstreams-reviewer` returned CHANGES REQUESTED; all five
findings are fixed:

| Finding | Where | Resolution |
|---|---|---|
| BLOCKING — tombstone delete made a nil-deref **reachable**; panic killed a worker permanently | `storage.go:206`/`:237`, `worker.go:277` | drop-not-resurrect via `ErrRecordGone`; `recover()` moved inside the loop |
| BLOCKING — `Model == ""` escape hatch reopened #612 on the upgrade path | `worker.go:441` | inverted to unusable; also fixed a second-order `SaveDedup` overwrite |
| HIGH — `HTTPEmbedder.dimensions` race + placeholder split the dedup keyspace | `http_embedder.go:203` | `atomic.Int64`, CAS-once, `DedupKey` withholds a key when unresolved |
| HIGH — rule startup budget was 10 attempts vs every sibling reader's 30 | `entity_watcher.go` | `startup_attempts`/`startup_interval_ms` config, sibling default 30×500ms |
| HIGH — warn→fail e2e conversions need tiers green | — | structural, semantic, statistical all green |

Gate: `go build`, `go vet` (plain + `integration` + `live_llm`), `task lint`,
`go test -race ./...` (0 FAIL), contract tests, tagged integration on all touched
packages, `task schema:generate` (additive only: `workers`, `startup_attempts`).
`task entity-id:audit` is red on 3 candidates — **verified identical at HEAD** in
a clean worktree (1173 extracted both sides), so pre-existing, not ours.

### Track 0 open questions — BLOCKING the PR

Measured against a HEAD baseline built in a clean worktree (two runs, stable):

| statistical tier | HEAD | Track 0 |
|---|---|---|
| `embedding_dedup_hits` | 188 / 190 | **53** |
| `embedding_generated_total` | 256 / 258 | 241 |
| `known_answer_tests_passed` | **7/7** | **6/7** |
| `communities_total` | **15** | **4** |

- **The dedup collapse is explained and correct.** Those ~137 hits came from the
  offloaded lane, which keyed on the ObjectStore *key* — an address, not content.
  They were precisely the stale-vector hits #612 exists to remove. Dedup is now
  disabled for that lane (`message.StorageReference` carries no digest, and
  hashing the body at hop 1 means a second full read on the watcher's hot path).
  **Cost is real and unmeasured: offloaded entities now re-embed on every update,
  each a remote call on the neural tier.** The clean follow-up the implementer
  named — derive the key inside hop 2 where `getSourceText` already holds the
  fetched, truncated body, which also subsumes the inline lane — is the remaining
  part of #612 and should be filed. A `dedup_skipped_total{reason}` counter is
  the minimum; without it this is invisible.
- **Settled by 4 runs per side** (statistical tier, HEAD in a clean worktree):

  | metric | HEAD (n=4) | Track 0 (n=4) | verdict |
  |---|---|---|---|
  | `known_answer_tests_passed` | 6, 6, 7, 7 | 7, 5, 6, 6 | **noise — no regression.** HEAD is not stably 7/7; the single 7/7 that raised the alarm was the top of HEAD's own range |
  | `communities_total` | 15, 15, 15, 15 | 4, 5, 5, 10 | real change, but see next row |
  | `community_ground_truth_passed` | 0, 0, 0, 0 | 0, 1, 1, 1 | **improvement.** HEAD's stable 15 communities match ground truth 0/3 every run |
  | `embedding_generated_total` | 241–257 | 253–256 | unchanged — we are not doing more embedding work |
  | `embedding_dedup_hits` | 173–189 | 60–65 | explained (stale object-key hits removed) |
  | `search_quality_score` | 0.2399–0.2437 | 0.2255–0.2287 | **real, consistent −6.0%. Ranges do not overlap.** |

  So the two alarms resolved opposite ways. `known_answer` was noise. The
  community drop is accompanied by a *consistent ground-truth improvement*
  (0/3 → 1/3), which reads as #619 working — BM25 vectors no longer shift under
  query traffic — rather than as damage; it is also consistent with the audit's
  own finding that community membership is non-deterministic and low-quality.

- **TRACKED, not blocking** (owner call 2026-07-21): `search_quality_score` drops
  6.0% with **non-overlapping ranges** across 4 runs per side. Not noise, but it
  measures a BM25 tier the audit already recommends shrinking, and #619's parent
  decision will move it again. Carry it forward rather than gate on it.

  **Baseline for the next comparison — use these numbers, do not re-quote a single
  run.** Statistical tier, `search_quality_score`, n=4 per side:

  | | runs | range |
  |---|---|---|
  | HEAD (pre-Track-0) | 0.2421, 0.2399, 0.2437, 0.2415 | 0.2399–0.2437 |
  | Track 0 | 0.2255, 0.2261, 0.2287, 0.2283 | 0.2255–0.2287 |

  Re-measure when Epic A touches BM25 (#619's index-vs-stateless-TF decision) or
  when #623 restores dedup on the offloaded lane. If a change claims to improve
  search quality, it has to clear 0.2287 to be distinguishable from Track 0 at all.
  Recorded on #619 as well so it surfaces when that work starts.

---

## Epics

Sequential, not parallel. Each is a candidate OpenSpec change **only if it carries
genuine spec deltas** — A, B, and C qualify; D and E do not.

**Epic A increment 1 is already scoped** (filed 2026-07-21 as #623): derive the
dedup key at content resolution in hop 2, bundling **#623 + #602 + #614 part 2**.
They share one seam — `graph/embedding/storage.go` + `worker.go` — and the key
derivation subsumes #602's cap half, so splitting them means touching the same
two files three times. Start Epic A here.

| Epic | Scope | Issues | State |
|---|---|---|---|
| **A** — evidence cannot silently expire | body TTL, hydration signal, dedup identity, vector reconciliation, BM25 contract, readiness truth | ~~#612 #623 #602 #614pt2~~ (inc.1 ✓) · ~~#600 #616~~ (inc.2 ✓) · ~~#601~~ (inc.3 ✓) · ~~#613 #627 #630~~ (inc.4 ✓) · ~~#599 #597~~ (test-debt ✓ #642) · #619 #633 (deferred owner-decisions) · #643 (spun off) | **COMPLETE (inc.1–4 + test-debt merged/closed); only deferred owner-decisions #619 #633 remain** |
| **B** — communities DELIVER GraphRAG (re-scoped: NL→thematic answers, not shrink) | B0 instrument → B1 stabilize/deterministic → B2 semantic-informed coherence → B3 ownership-split Tier-2 | ~~#606–#618~~ · #607/#617 closed by B3 · follow-ups #701 #710 | **✅ COMPLETE — B0/B1/B2/B3 ALL CLOSED. Recall-ceiling fix MERGED (PR #702, 0.85→0.95); B3 ownership split MERGED (PR #709 `857988ef`, archive #711; closes #607/#617). Follow-ups: #701 (multi-community expansion), #710 (summary-store GC), #661 reframed** |
| **C** — operational / derived-KV-plane state-ownership (REDRAWN 2026-07-28) | generalize B3's single-writer + content-addressing + guard-coverage pattern across the plane the projection-contract arc leaves ungoverned; extend the boot retention/write-owner guard to full bucket coverage (the primitive), then repair loops that CONSUME it | ~~#622~~ (primitive, inc-0) DONE #625 #629 · ~~#527~~ folded into `graph-index-replacement-semantics` | **inc-0 (#622) MERGED + ARCHIVED 2026-07-28 (PR #716 `d03c49f7`, archive `2026-07-28-framework-owned-bucket-guards`). Two-pass boot-time retention guard over full `FrameworkOwnedBuckets()` (reconcile-then-assert, ObjectStore-symmetric) + write-ownership registration of ENTITY_SUFFIX_INDEX/GRAPH_INGEST_APPLIED_SEQ/GRAPH_STATUS. Codex found 3 BLOCKING past a same-run reviewer APPROVE (create-race coverage hole; 2 more forge/drop write holes) — ALL fixed + re-reviewed APPROVE + CI green, once-through Codex gate held. #715 closed (F2). Follow-ups: #714 (reader-creates), #717 (COMPONENT_STATUS classify), F2 blind-decode hardening (optional). NEXT: #625+#629 graph-embedding pair (consume the primitive).** |
| **D** — consumer-path release gates | see prerequisite below | #615 + CI | **in progress** |
| **E** — semsource clean cut | GRAPH stream posture, dead bucket wiring | semsource#110 | not started |

### Epic D has a prerequisite that was not obvious

`ci.yml` runs **no e2e tier at all** — jobs are lint, test, build,
schema-validation, status-check. Building a verification matrix on top of a suite
no machine runs buys nothing. Order: automate a tier first, then strengthen
assertions, then extend coverage.

Shipped 2026-07-21: `.github/workflows/sister-validation.yml` — semsource e2e per
PR, semsource + semboids + `e2e:core` nightly, all against **this checkout** via
`go mod edit -replace`.

**Status on D — first CI run happened 2026-07-21 (PR #626):**

- **The gate fired for the first time** (run 29871032706, `on: pull_request`). It
  works: `go mod edit -replace` built semsource@main against semstreams@main and
  ran semsource's full e2e suite. The nightly-only jobs (semboids, `e2e:core`)
  correctly skipped on a PR.
- **The local prediction was WRONG and is corrected here.** The baton claimed
  semsource@beta.156 "does not compile against main" (fusion API drift:
  `Resolve`→`[]fusion.Seed`, `Entities`→`fusion.Hydration`). In CI, semsource@main
  **compiled and ran fine** against semstreams@main — no compile break. The
  framework integration is healthy: **28,379 entities ingested end-to-end**
  (java 27,875 / web 83 / git 3 / config 35) across all four domains.
- **One narrow failure**, and it is product-domain, not substrate: semsource's docs
  source emits entities typed `"chunk"` where semsource's own e2e test expects
  `'doc'` (`semsource test/e2e/e2e_test.go:1426`, in `TestE2E_OSH_JavaMavenIngest`).
  This is **main-vs-main drift the gate surfaced** — independent of Track 0 (which
  never touched doc/chunk typing) and of this PR (which only added the gate). Per
  the Product Boundary the doc-vs-chunk type is semsource-owned; most likely
  semsource's docs source started chunking while its e2e assertion stayed on the
  old label. **Owner action: confirm on the semsource side and either fix the
  emitter or update the assertion; file a semsource-asks entry if it is actually a
  semstreams contract change.** Do not carry the disproven compile-drift story
  forward.
- **Main has no required checks**, so this gate is advisory until made required.
  A green check that cannot block a merge is a notification, not a gate.

Also note `semspec-validation.yml` has **zero runs, ever** — itself an instance of
the phantom class. Fold into `sister-validation.yml` or delete; do not leave a
workflow that looks like coverage and has never executed.

### Decisions deliberately not made by the audit

These need an owner, not an implementer:

- **Community scope at v1. — DECIDED, REVERSED (owner, 2026-07-23).** The audit's
  shrink recommendation (level-0-only + disable LLM + defer the split) was
  **rejected** once the intent was examined: communities are the GraphRAG thematic
  layer, semstreams exists for PathRAG+GraphRAG, semsource (lead v1 product) wires
  community + `global_search` + Tier-2 (seminstruct), and "NL→thematic answer" is a
  v1 expectation. The audit's "low quality" metrics are a *garbage-in* symptom of an
  unweighted, semantic-blind, non-deterministic partition — not a reason to shrink.
  **Resolution: INVEST** (Epic B B0–B3, see Next Action). Level-0-only survives but
  for the honest reason (today's 3 "levels" are 3 identical LPA runs, `ParentID`
  always nil — fake hierarchy); LLM is disabled only as a B1 *interim*, re-enabled
  via the B3 ownership split; the split is **in scope**, not deferred.
- **Embedding readiness semantics.** "Terminal includes failed" is a decision, not
  a patch. Fits the ADR-084 health-gates frame.
- **BM25 tier contract.** Lexical index over an immutable snapshot, or stateless
  hashed TF. Do not add locks to the current hybrid and call it fixed.
- **ADR-061 is shipped but still marked `Proposed`.** Promote it, and note it never
  claimed the broader "the semantic tier does not affect the partition" contract —
  if that is what we want, say so explicitly.

---

## Timing — DECIDED

**Owner-approved 2026-07-21: do the breaking half now, in one wave.**

Not speculative. Sister lockstep against .157 is already due and verified —
semsource at beta.156 does not compile against main (`fusion.RetrievalClient.Resolve`
now returns `[]fusion.Seed` not `[]string`; `Entities` returns `fusion.Hydration`).
That cost is being paid regardless, so folding the dedup key change, the community
collapse, the config deletions, and the TTL fixes into the same lockstep is close
to free. Post-v1 every one of them becomes a compat shim maintained forever.

Practical consequence for whoever picks this up: **do not defer a breaking fix to
"after v1" on compatibility grounds.** That trade is already resolved. Breaking
changes still owe the house rule — a relevant e2e tier green before the tag
(CLAUDE.md), which is now partly automatable via `sister-validation.yml`.

## Explicitly not doing

Coverage targets (the mechanism that produced this) · more tests (ratios are
1.2–2.1 and the unit tests are good — the rot was in e2e) · more reference designs
(diminishing returns; they are structurally blind to the phantom class) · a twelfth
parallel OpenSpec change.

---

## Log

Append one line per session. Newest last.

- **2026-07-21** — Program opened. Two audits (Codex + Claude) reconciled into
  `prev1-audit-synthesis.md`. 13 issues filed (#610–#622) + semsource#110.
  `sister-validation.yml` written and locally validated; not yet run in CI.
  **Timing decided by owner: breaking wave now, one shot.** Next: Track 0.
- **2026-07-21 (session 2)** — Track 0 implemented (all 8) + reviewed + 5 review
  findings fixed. Uncommitted. Full local gate green; structural, semantic and
  statistical tiers all green. Two corrections to the plan worth carrying:
  (1) **#610's issue text was wrong** — it prescribed "return a transient error,
  the watcher already retries." Nothing retries: `processor.go:481` calls
  `watchEntityStates` once and swallows the error with a Warn, and the degraded
  latch is process-lifetime sticky. Deleting the create path alone would have
  traded a nondeterministic graph-ingest outage for nondeterministic *permanent
  rule-evaluation disablement*. The fix needed a bounded wait too.
  (2) `sister-validation.yml` has zero runs because **the branch was never
  pushed** — the workflow does not exist on GitHub at all (`gh run list` returns
  404, not an empty list). Epic D's "unproven in CI" is really "not yet on main."
  Ran 4 statistical-tier runs per side against a HEAD worktree baseline to settle
  two suspected regressions: `known_answer` was **noise** (HEAD's own range is
  6–7), and the community-count drop came with a consistent ground-truth
  *improvement* (0/3 → 1/3). One real finding survives: search quality −6.0% with
  non-overlapping ranges. **Method note worth keeping:** the first comparison used
  the audit's quoted 43% dedup figure as the baseline and looked like a large
  regression; building an actual HEAD baseline in a clean worktree is what
  reframed it. Quoted numbers from a prior run are not a baseline.
- **2026-07-21 (session 3)** — Track 0 committed, pushed as PR #624 (off `main`,
  not stacked), and **merged** (squash `5e80f676`). Codex reviewed and found 7
  issues beyond the two internal reviewers; 6 real (1 refuted), all fixed in the
  PR. Highlights: the rule startup knobs were a self-inflicted phantom (accepted,
  validated, schema-published, silently dropped by the factory overlay); the KV
  circuit-breaker exemption was missing (mirrors `GetStream` gh#248); and
  `resolved_total` double-counted dedup hits, which had made me report embedding
  volume as "unchanged" when real fresh work went 68→191 (2.81x) — the callback
  that increments `embeddings_generated_total` fires on the dedup-hit path too.
  Follow-ups #623 (Epic A) + #625 (Epic C repair loop). Branch hygiene: the
  duplicate Track 0 code commit was dropped from this branch via
  `rebase --onto`, leaving only docs + `sister-validation.yml`. Next: land this
  branch (first CI run of the sister gate), then Epic A increment 1.
- **2026-07-22 (session 4)** — Track 0 docs+CI branch merged (#626); sister-gate
  fired its first-ever run and **disproved the baton's own prediction** —
  semsource@main compiles/runs fine against main (28k entities ingested); the one
  red is a product-domain `chunk`/`doc` main-vs-main drift, semsource-owned. Then
  **Epic A increment 1 shipped end-to-end**: spec-driven (`/opsx:new` →
  proposal/design/specs/tasks), implemented, two review rounds (semstreams-reviewer
  + PR review), **MERGED as #628 (`a6ea9979`)**, OpenSpec archived. Offloaded-lane
  dedup restored (fresh embedding work 191→68 vs Track 0). Three lessons worth the
  ink: (1) the **superseded-drop-skips-onGenerated** fix regressed the e2e metric
  invariant (`dedup_hits` counted eagerly at lookup, but a dropped hit skips
  `generated_total`) — the semantic tier's invariant gate caught `dedup_hits 166 >
  generated 69`; **the statistical *smoke* had too little same-entity churn to trip
  it, so re-run the tier that exercises the path, not the fastest one.** (2) A
  **security alert on the reviewer agent** was benign fails-without-fix methodology
  the scanner couldn't distinguish from sabotage — verified the guard intact by
  neutralizing it myself. (3) `#623`'s ContentHash producer→consumer move
  *simplified* `#614 part 2` (removed the read-to-copy). Follow-ups filed: #627,
  #629, #630. Next: Epic A increment 2 (#600 + #616).
- **2026-07-22 (session 5)** — Inc 2 (#632), inc 3 (#635), and both retrospective
  rounds (#636, #638) all shipped since session 4; main tip `08d7c5c2`. **Racked and
  stacked the remaining Epic A surface** and found the baton's "4 remaining"
  (#613/#619/#599/#633) undercounted it: increments 1–3 spun off **four more issues on
  the same embedding seam** (#625/#627/#629/#630) that were never folded back into the
  epic. Real remaining surface = 8, plus #597 to reconcile. Owner decisions:
  (1) **inc 4 = #613 + #627 + #630** — one worker.go hop-2 seam, one e2e tier; #613's
  owner-call settled ("terminal" excludes "failed"; track a `failed` gauge; a
  *configurable* failure-ratio drives `degraded`). (2) **#625 + #629 → Epic C** (both
  cross-bucket/ownership-shaped; #629 dormant). (3) **#619** (interim shipped, bleeding
  stopped) and **#633** (owner-accepted growth) → deferred owner-decisions.
  (4) **#599 + #597** → test-debt/reconcile. Also: **new merge-gate** owner directive
  recorded — a code PR merges only after Codex has posted a review, it's addressed, and
  CI is green (in that order). Next: `/opsx:new` for inc 4.
- **2026-07-22 (session 6)** — **Epic A inc 4 SHIPPED end-to-end and archived** — change
  `embedding-readiness-and-dedup-efficiency`, spec-driven (proposal/design/2 spec deltas/
  tasks, validates `--strict`). Implemented by semstreams-developer, APPROVED by
  semstreams-reviewer (mutation-tested), `task e2e:semantic` GREEN (breaking gate) with the
  degraded path integration-covered, merged as **PR #639 (`fa1041a8`)**. The Codex gate did
  its job: it found **2 P1 correctness bugs the reviewer missed** — (1) the failure map was
  not revision-aware (an older completion could clear a newer failure), (2) a persistence
  miss (SaveFailed/SavePending error) was mis-classified as terminal-skipped, clearing the map
  + advancing readiness — plus 3 P2s (snapshot/live race, cold-start singleflight bypass,
  unbounded scanned reason labels). All 5 fixed with failing-first tests + a second reviewer
  pass (`b7a29f58`) verifying finding 2 does NOT reintroduce the watermark deadlock. #613 +
  #630 closed. Lessons: (a) my initial #613 framing ("must not advance the watermark") was
  WRONG — the watermark advances by design (deadlock avoidance); the fix is a `FailedCount`
  input to the shared projection. (b) #627 was already fixed by inc 1 — verified before
  building; closed as verify-only, no phantom follow-up. (c) the owner ran no bespoke
  degraded-e2e because integration coverage is genuine — no follow-up filed (anti-proliferation).
  Then **TAGGED + PUSHED `v1.0.0-beta.158`** (`d6d3e57e`) — owner-decided to release the
  4-BREAKING wave now (owns the sisters; wants a tight cadence so migration stays small; sister
  tests are the signal). Pre-tag gates: build-tag sweep (integration+live_llm) clean +
  `e2e:semantic` GREEN at HEAD on the FINAL code incl the 5 Codex fixes (the earlier e2e ran on
  pre-fix `037af8f3` — re-ran at HEAD to close the beta.18-style gap). Annotated house-format
  tag names each breaking change + migration doc; sisters not touched (owner manages them).
  **Next: OWNER DECISION on the next unit — Epic B, Epic C (now carrying #625/#629), or the
  deferred Epic A owner-decisions/test-debt (see Next Action).**
- **2026-07-23 (session 7)** — **Epic A test-debt (`#599 + #597`) SHIPPED + CLOSED; Epic A now
  fully complete.** New `validate-batch-read-reconciliation` e2e:semantic stage (test-only, PR
  **#642 `db64d2c6`**) drives `graph.query.batch` + the production `fusionnats.Client.Entities`
  over the live wire — gh#604 missing→unhydrated reconciliation + exactly-once per reply +
  load-bearing `batch_query_missing_total`. Pipeline: semstreams-developer → semstreams-reviewer
  (1 HIGH: verdict overclaimed "resolved" — fixed) → Codex gate (**5 real holes, 3 P1, two
  reviewers missed**: production-client reconciliation untested, counter presence-only,
  count-only `assertAllHydrate`, pre-warmed reorder, non-evicting soak) → all addressed. Lessons:
  (1) **my own over-tightening** — I directed an exact `==` counter invariant; the counter is
  process-global, so exact/upper-bound would flake red on unrelated traffic. Changed to a
  **lower-bound** gate (proves increment / catches silent-stop + reset); present-gap detection
  stays where it's deterministic + attributed (the hydration guards). (2) **Honest downscope beats
  a faked signal** — the gh#604 reorder-under-cache-miss and a real gh#597 cache-residency soak
  are unreachable (entity cache hard-coded 5000/30s over ~74 entities never evicts); filed **#643**
  (cache-control seam) rather than pre-warm an all-hit request into a false pass. Fuse envelope →
  **#391** (re-home note posted). (3) **The out-of-band Codex flow**: no GitHub-integrated bot —
  the owner runs it and relays findings under their account; I cannot trigger it. #599 + #597
  CLOSED (owner: #597 guarded + countable, NOT proven closed). Also reconciled the tracker — **7
  merged-but-open issues (#600 #601 #602 #611 #612 #614 #616) closed**. Merged CLEAN, second Codex
  pass waived. **Next: the SAME owner decision — Epic B / Epic C / deferred Epic A (#619 #633 #643).**
- **2026-07-23 (session 8)** — **Epic B opened and RE-SCOPED.** Started to scope Epic B as the
  audit's "shrink communities" (drafted an OpenSpec `community-single-truth` proposal to go
  level-0-only + disable LLM). **The owner stepped back to intent** — why does community exist, who
  consumes it — which flipped the epic: communities are the GraphRAG thematic layer, semstreams is
  a PathRAG+GraphRAG edge SKG, semsource (LEAD v1 product) wires `global_search`+Tier-2 via
  seminstruct, and NL→thematic answers is a v1 bar. **Retired the shrink proposal.** Grabbed a
  baseline (thematic quality is UNMEASURED; the thematic *layer* is broken while retrieval works;
  explicit-edge LPA is weakest exactly on the doc corpus). Architect designed the INVEST plan
  (B0 instrument → B1 stabilize → B2 semantic coherence → B3 ownership-split Tier-2 + one ADR-08x,
  promote ADR-061). Owner approved + added the **tiered-graceful-fallback tenet** (structural+
  statistical = correct Tier-0/1 floor; semantic/LLM additive; never empty). Lessons: (1) **ask
  "what job does this do" before "how do we fix the tech"** — the shrink was technically clean and
  practically wrong; the owner's intent question saved a wrong build. (2) **the audit's "low
  quality → shrink" was garbage-in**: the fix is partition quality (semantic edges — never built),
  not deletion. (3) verify consumers in the SISTER repo (semsource wires it intentionally), not
  just the framework. **Next: build B0 (the instrument) + record the broken-state baseline; then
  the ADR + B1.**
- **2026-07-23 (session 9)** — **Epic B B0 SHIPPED (#650) + the plan RE-ORDERED by measurement.** Built
  the B0 thematic instrument (`validate_thematic_eval.go`, 5 deterministic dims) + a repeatable 8B
  harness (`task e2e:semantic:8b`). Measured at 1.7b (noise — `answer_synthesis` timed out at the 15s
  default → metadata-template floor), then on the owner's steer (Microsoft: <~7B community summarization
  is noise; seminstruct has a `qwen3-8b`) re-measured at **8B** — which forced a natsclient fix (**#646**:
  the hardcoded 30s handler timeout truncated 8B synthesis below its budget). **KEY FINDING: the dominant
  thematic defect is a RETRIEVAL bug — the classifier type-filter hard-zeroes good semantic results
  (#645) — NOT partition coherence as the audit predicted.** 4/5 thematic queries → count=0; 8B synthesis
  WORKS when entities are retrieved; partition determinism already 1.0. So the plan re-ordered: fix #645
  first → re-measure on the 8B harness → THEN decide if B2 (partition) is even needed. Also owner-directed:
  **CI redesign (#647)** — replaced per-PR sister validation with our own `e2e:statistical` ladder (Epic D
  "automate a tier first"), sister OFF until v1/RC; **graphview flake fixed (#648/#649)** (a real
  observation race — verified 5/5-local-PASS = flake); the #650 golden-pack-id test caught a real
  config-dup bug (fixed). Lessons: (1) **"what job does this do" (s8) then MEASURE before building (s9)**
  killed the wrong build TWICE (shrink→invest; rebuild-partition→fix-retrieval). (2) **measure with an
  ADEQUATE model** — a sub-threshold model reads as "communities are bad" when the MODEL is the problem;
  seminstruct `qwen3-8b` is the recommended summary tier, `1.7b` is an "expect degradation" fallback.
  (3) **investigate every red before calling flake** — 5/5-local-FAIL is deterministic (golden test caught
  a real dup), 5/5-local-PASS is the flake (graphview). (4) **the Codex gate is out-of-band** — no GitHub
  bot; owner runs it + relays under their account; agent can't trigger it (owner waived it for the
  low-risk PRs this session). **Next: fix #645 (+ the #650 NIT), re-measure on `task e2e:semantic:8b`,
  scope B2 against real numbers.**
- **2026-07-23 (session 10)** — **#645 FIXED + VALIDATED; re-measure done.** Zero-fallback in
  `filterEntityIDsByType` (`(filtered, fellBack)`; non-empty→empty falls back to unfiltered +
  `classifier_garbage{type=type_filter_zeroed}`) + the #650 NIT folded in. semstreams-developer built
  it, **semstreams-reviewer APPROVE-WITH-NITS** (mutation-proven the tests catch the old hard-zero),
  the one substantive NIT taken (a stale operator-triage comment the fix made misleading). Full local
  gate green. Branch `fix/645-type-filter-zero-fallback`, **UNCOMMITTED** (awaiting owner go for
  commit→PR→Codex→CI→merge). **Re-measure: retrieval defect RESOLVED — `empty_answer_count=0/5` (was
  4/5) at both tiers; 8B all 5 count=68 grounded+no-fab (recall 0.50–1.00); 1.7b full 48-step gate
  GREEN (exit 0).** Three findings that move the plan: (1) **partition determinism is 0.83 NOT 1.0**
  (175-entity corpus; session-9's 1.0 was the ~74-entity corpus) → B1 determinism is a LIVE gap;
  (2) **recall spread 0.50–1.00 now measurable** (dock-equipment 0.50) = the B2 coherence signal;
  (3) **`e2e:semantic:8b` is a capacity artifact RED** — two qwen3-8b saturate 23.2 GiB (enhancement
  `pending=10` after 10min, 33m total, known-answer step 41 `EOF`); SAME step green at 1.7b → not a
  regression. Lessons: (a) **measure-before-building paid off a SECOND time** — the audit predicted
  "partition coherence," the 8B measure said "retrieval bug"; fixing the retrieval bug (not rebuilding
  the partition) cleared 4/5 of the defect. (b) **validate a capacity-suspect e2e RED at the cheaper
  tier** — the 1.7b gate green + same-step attribution disambiguated the 8B EOF as capacity, not
  correctness, without a 33m re-run on main. (c) **the auto-teardown `defer down -v` ate the container
  logs** before I could confirm OOM-vs-timeout — next 8B run streams logs or uses the debug (no-teardown)
  target. **Next: owner go on commit→PR→Codex→merge for #645; then owner+architect scope B1 (determinism
  0.83) / B2 (recall spread), and decide 8B-harness viability.** THEN (same session, owner steer) ran a
  **FRONTIER-CEILING probe** to decide B2 against a real upper bound: new `task e2e:semantic:frontier`
  (`configs/semantic-frontier.json` + `docker/compose/tiered.frontier.yml`) routes answer_synthesis +
  community_summary to **Gemini 2.5 Flash** (cloud yardstick, NOT the offline product path). Full 48-step
  GREEN in 3m42s. **Gemini ≈ local 8b on 4/5 queries, +1 entity on dock (0.50→0.75); 4-entity queries cap
  at 3/4 even at the frontier → synthesis is NOT the bottleneck (edge 8b is within ~1 entity of frontier;
  tiered-fallback holds), residual is a NARROW retrieval/partition miss → B2 REBUILD looks low-ROI (targeted
  retrieval fix instead).** Two keeper follow-ups: (1) **graph/llm `reasoning_effort` gap-fill** — the
  validated `EndpointConfig.ReasoningEffort` setting was honored by agentic-model but SILENTLY DROPPED by the
  graph/llm synthesis client (`OpenAIConfig` lacked the field; `doWireChat` omitted it) — needed because
  gemini-2.5-flash is a thinking model that truncates the 200-tok community_summary without
  `reasoning_effort: none`. (2) **owner-raised architecture finding → filed:** TWO independent LLM
  chat-request builders (agentic-model + graph/llm) DRIFT on endpoint settings (reasoning_effort was
  instance #1); consolidate onto one shared endpoint-applier (transport already unified via
  `model.NewHTTPClient`; it's the request-semantics layer that's doubled). Deferred, architect-owned. Lessons:
  (a) **MEASURE THE CEILING before building B2** (third measure-before-build win this arc) — a frontier model
  proved the partition rebuild is low-ROI without building it; (b) **verify a "we already support X" claim
  from code** — the owner was right that `reasoning_effort` exists (registry + agentic), I was wrong it needed
  new code; the real bug was one client dropping it. **Bundling #645 + reasoning_effort + frontier harness +
  this baton into ONE PR (**#653**, owner: run counts as review for the simple reasoning_effort fix); filed the
  two-LLM-clients issue (**#652**). **Codex round on #653 (6 findings):** #1 pack_id reuse (had already self-
  caught + fixed — my pre-commit gate ran only touched packages, not `./...`, so CI caught it first; lesson
  re-learned); addressed #5 (reasoning_effort test bypassed the `OpenAIConfigFromEndpoint` seam it guards —
  rewrote through the translator + `io.ReadAll`, mutation-proven) and #6 (fallback mislabeled as
  `classifier_garbage` — moved to a neutral `type_filter_fallback_total`; a zero-match only means no candidate
  in the window, not proven-invented). **#3 was the sharp one — the frontier-vs-local comparison did NOT hold
  the partition fixed, so the "B2 low-ROI" read is a HYPOTHESIS not a finding; walked it back here + in memory.**
  Per owner ("keep the harness lean, note honest findings") #2/#4 are DOCUMENTED as harness limitations
  (record-only, not gated; no quota-safe retry) + `enhancement_workers:1`, not built into gates; the rigorous
  frozen-partition paired run is a filed follow-up. Next: owner re-review/CI/merge; then B1/B2 (via the paired
  run) + client-consolidation.** **Also incorporated (owner saturation analysis, verified):** the 8B endpoint
  saturation (`pending=10`/EOF) is the SAME two-clients drift — `graph/llm` honors neither `max_concurrent`
  nor `requests_per_minute` (agentic-model does, via `EndpointThrottle`), and clustering defaults to 5
  enhancement workers vs 2 llama.cpp slots (2.5× oversubscription). Fix = concurrency-admission (not RPM): a
  shared `model`-layer admission gate keyed by endpoint URL/model that both clients honor + narrow retries
  (429/503 only) + the SemStreams/SemInstruct ownership boundary; immediate quick-win is aligning
  `enhancement_workers` ↔ `MODEL_PARALLEL` per tier. Folded into **#652** (concrete deliverable); subsumes
  #654's retry note. So "decide 8B-harness viability" now has a path, not just a question.
- **2026-07-26 (session 12, docs closure)** — **B2 CLOSED, honest negative.** §8 (weight-tuning) is DONE:
  a paired frontier decider (Gemini 2.5 Flash held constant across both arms, partition the only
  variable) found the colocation_mean rise (0.60→0.83) is a mega-community merge artifact
  (`distinct_plurality_communities=2/5`, one 47-member community absorbing 4/5 themes) that bought ZERO
  thematic recall (flat 0.85 both arms, per-query byte-identical, known-answer 7/7 both, summaries not
  truncated). A context-diff confirmed the missed theme terms are identical across partitions and
  present in-corpus under both — the recall ceiling is upstream (synthesis compression / eval
  literal-term matching), falsifying B2's founding premise that the missing entity is structurally
  unreachable in another community. The mechanism (§1–§7, §8.0 of `graph-clustering-semantic-edges`)
  ships MERGED + DEFAULT-OFF as a future lever; the compound colocation gate (§8.1/§8.2) was measured
  and deliberately NOT adopted — `validate_partition_colocation.go` stays record-only, hardened by two
  trust instruments (mega-community discriminators, per-query dilution channel) landed via PR #698
  (`304368d9`). ADR-086 promoted `Proposed` → `Accepted` with an `Outcome (2026-07-26)` section
  recording this — the measurement refines expected ROI, it does not reverse the mechanism decision.
  `tasks.md` §9 updated: validate/gates/reviewer-approval marked done, 9.3's e2e gate item annotated
  (no gate condition exists since 8.1 was not adopted; the tier itself is green), 9.5 (archive) left
  pending. Two levers recorded out of B2 scope, future work: retrieval-side multi-community query
  expansion, embedding-input shaping (themes collapse by document genre in the absorbing community's
  membership). Docs-only change; the mechanism spec deltas were left untouched (already accurate).
  **Next:** program manager reviews this closure, runs `openspec validate --strict` and `openspec
  archive`, then decides B3 (ownership split, Tier-2) vs. the next epic.
- **2026-07-27 (session 13)** — **RECALL-CEILING FIX BUILT + VALIDATED; PR #702 OPENED (ready to merge, not yet merged); the owner's "why don't
  communities resolve as expected?" is answered.** Cheapest-win static trace (free, no e2e) first: the
  0.85 ceiling is synthesis-projection lossiness, NOT partition. GraphRAG answer synthesis
  (`answer.go:buildAnswerPrompt`) feeds only {community summary + ≤5 PageRank query-AGNOSTIC rep titles +
  ≤5 keywords}; entity bodies never reach the prompt. The 3 missed terms split TWO ways (corrected the
  standing memory, which said "descriptions not titles"): `battery`/`door` are in TITLES of non-rep
  entities; `evacuation` is tag/description-only. Key unlock (architect): **tags ARE triples**
  (`content.classification.tag`), bodies are ObjectStore-only — so a tags channel recovers all three with
  zero fetches. Spec-driven change `thematic-synthesis-context`: Lever A (query-relevant rep selection via
  `semanticScores`, PageRank fallback) + Lever B (capped tags in prompt + template floor). architect →
  semstreams-developer → semstreams-reviewer APPROVE (2 MEDIUM + 2 NIT fixed; MEDIUM-2 = a 2nd architect
  call promoting the predicate to `vocabulary.ContentClassificationTag`, fixing a product-boundary smell
  via the dc.terms.title precedent). **Frontier e2e (single approved run, doubled as confirm-trace):
  recall 0.85→0.95, `battery`+`door` recovered, ZERO regressions (known-answer 7/7, determinism 1.0,
  validation_errors:0).** `evacuation` stays missing = cross-community coverage (tag-bearing doc in a
  document community that doesn't reach the fire query's maintenance-dominated top clusters), NOT a tags
  defect → owner chose LAND-THE-WIN + filed **#701** (multi-community query expansion). PR #702 pushed,
  CI pending at hand-off; owner-gated Codex + merge, then archive. Lessons: (1) **cheapest-win static
  trace before any paid e2e** paid off — it partially corrected the recorded diagnosis (terms in titles,
  not just bodies) for free and reshaped the fix. (2) **fold confirm-trace into fix-validation** — one
  frontier run instead of a confirm-only run + a validation run. (3) two stale gopls diagnostics
  (undefined symbol, scratch-file) both dissolved on independent `go build` — status words lie, run the
  command. **Then Codex reviewed #702 (3 findings): [P1] pre-type-filter score map could promote a
  type-EXCLUDED entity into a shared community's digest (fixed — new `relevanceScoresFor` helper keys the
  score map by the surviving `entityIDs` at all 3 call sites; the original reviewer had under-weighted
  this as "acceptable"); [P2] no-score fallback bypassed `MaxQueryFocusedReps` (fixed — capped copy);
  [P2] baton said "landed" on an open PR (fixed). All addressed + fail-without tests + CI green →
  MERGED (`f9833ace`) + ARCHIVED (#705 `669790d6`, graph-query spec requirement promoted). Owner-authorized
  the merge once CI green. Also declined to touch the separate codex projection-contract/predicate-audit
  stack (owner + sister session merge it bottom-up). Lesson (4): a same-run reviewer APPROVE is not the
  last word — Codex caught a real P1 the internal reviewer waved through; the once-through Codex gate earns
  its keep. Recall-ceiling front CLOSED. Next: epic-level WIP-1 pick (B3 / #701 / Epic C / deferred Epic A).**
- **2026-07-28 (session 14)** — **EPIC B COMPLETE — B3 (community-summary ownership split) BUILT + MERGED
  (PR #709 `857988ef`, archive #711).** Same session as the recall fix. The enhancement worker's blind
  `Put` into the shared `COMMUNITY_INDEX` (clobber #607 + resurrection #617) is closed STRUCTURALLY by a
  worker-exclusive, content-addressed `COMMUNITY_SUMMARIES` store keyed `{level}.{membership_hash}` — a
  content-keyed write can't touch the detector-exclusive partition, so no CAS on the happy path; unchanged
  membership = cache-hit skip. graph-query joins by membership hash with a statistical floor. ADR-087.
  Architect-scoped → semstreams-developer built → semstreams-reviewer APPROVE. BREAKING gate: the 1.7b
  `e2e:semantic` FAILED on the known LLM-saturation capacity artifact (NOT a defect — determinism still
  1.00); frontier tier (Gemini) is the reliable gate → GREEN. Owner steered "belt-and-suspenders," which
  PAID OFF: the confirming frontier runs surfaced **two observability PHANTOMS** (the `validate-llm-enhancement`
  e2e stage + a second metric writer both still reading the emptied old `COMMUNITY_INDEX.SummaryStatus`
  field → reported `enhanced=0` while the worker genuinely enhanced) — a $0 real-NATS wire integration test
  gave the definitive measurement-gap-not-defect answer, both stages migrated to the new store. **Codex
  then found 3 MORE real issues the internal reviewer missed (2 HIGH): late summary-bucket attach on
  rolling upgrade → statistical floor forever (independent retrying watcher); a lagging `llm-failed` write
  clobbering `llm-enhanced` (CAS `PutFailedUnlessEnhanced` — the "no CAS" premise held only success-vs-
  success, not success-vs-late-failure); +MEDIUM gauge-init on restart — all fixed + integration-proven + a
  final confirming frontier (self-consistent `llm_enhanced=14`). Follow-ups filed: **#710** (worker-owned
  bounded-GC, gated on the size gauge B3 ships), **#661 reframed** to re-measure-after-B3 (its churn is now
  a µs cache-hit skip). Lessons: (1) **the once-through Codex gate earned its keep TWICE this arc** — real
  HIGH correctness bugs on both #702 and #709 past a same-run reviewer APPROVE; treat internal APPROVE as
  necessary-not-sufficient on concurrency/startup code. (2) **a BREAKING migration must migrate its
  OBSERVABILITY too** — moving a store while leaving the e2e/metric readers on the old field yields a
  green-but-blind gate (the "warn-not-fail masks drift" class); the frontier runs caught it precisely
  because we read the numbers, not the exit code. (3) **1.7b e2e is capacity-flaky for the LLM path; frontier
  is the reliable BREAKING gate** — don't chase a 1.7b saturation failure as a code defect. **Next: Epic B
  fully closed — pick Epic C / deferred Epic A / #701 / #710.**
- **2026-07-29 (session 16)** — **Baton item (1) resolved by FALSIFICATION, not by the planned rebase.**
  Picked up `bounded-storage-operability` (0/35, BREAKING, 11 days old) to rebase onto the
  `framework-bucket-catalog` seam. Two parallel agents (architect ruling + staleness sweep) found the
  delta would REVERT the catalog requirement (12 buckets of coverage, per-descriptor policy —
  `OWNER_PRESENCE`'s declared TTL would become a violation — seam-primary enforcement, and strip-and-warn
  self-heal reverted to hard boot failure), plus a cross-capability contradiction `openspec validate
  --strict` structurally cannot see (its `object-storage` delta mandates a `windowed` TTL knob that merged
  `graph-retention` forbids outright). **Then the owner asked the question that killed the rebase: "are we
  really in a position to say we can safely handle a ttl or maxbytes in storage?"** — the ban exists because
  NATS has no context for atomic, consistent deletes in our system. Ran a real-NATS probe instead of
  arguing: at a `DiscardNew` ceiling, **replacing an existing key is REJECTED** while deletes and purges
  succeed. My own "one-way trap" hypothesis was FALSIFIED (deletes get in — tombstones are small), but the
  finding that matters is worse for the change's premise: reserve-replacement-headroom is not expressible,
  per-bucket ceilings tear cross-bucket consistency with no transaction to make them atomic, and the
  ceiling inverts ADR-068 by denying update while permitting delete. Retired the change; wrote
  `storage-capacity-observability` (proposal/design/2 deltas/tasks, valid `--strict`) scoped to the
  operationally useful half per the owner's steer. Architect adversarial review: **3 BLOCKERS, all on the
  KV/OBJ exclusion boundary** — the delta said those streams were "not *required* to declare bounds"
  (a permission, satisfiable while still letting the reconciler WRITE retention onto them), "ordinary
  stream" had zero representation in `config/streams.go`, and the three downstream safety nets each have a
  hole (`ReconcileNoLifecycleRetention` never clears a discard policy; `RetentionUnmanaged` reconciles
  nothing; non-catalog buckets have no seam). Rewrote as a MUST-NOT prefix guard at the provisioner with a
  POSITIVE guard test. Also folded: restored the dropped migration override (H4 — without it the bounds
  rule is a flag day for every component-derived stream), `SpecFor(b).Owner` doesn't compile → `OwnerOf`,
  per-storage-tier account limits (memory vs file must not be summed), restart-surviving growth rate,
  named report transport, and re-homed provisioning OUT of `nats-streaming` (a publish-path capability)
  into its own `stream-provisioning`. Filed **#727**. Lessons: (1) **a spec can be wrong in premise, not
  just stale in detail** — the ledger-driven rebase would have produced a well-formed change built on a
  falsified foundation; the owner's intent question caught what two agents' file-level analysis did not
  (same shape as session 8's shrink→invest reversal). (2) **Measure the primitive, don't reason about it** —
  a 40-line testcontainer probe settled in one run what the spec, two ADRs, and three agents had been
  arguing from prose; it also falsified MY hypothesis, which is why it was worth running. (3) **"not
  required to X" is not "must not X"** — a negative permission is satisfiable by an implementation that
  still does the dangerous thing. **Next: owner sequences (2) #712 projection quiescence or (3) the
  complexity-pivot remainder; `storage-capacity-observability` is scoped and unstarted.**
