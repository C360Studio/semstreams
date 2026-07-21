# Pre-v1 audit: the graph core (embedding, community, index, cache)

**Status:** DRAFT — audit in progress, 2026-07-21. Companion to the parallel
Codex audit; findings get reconciled before any plan is committed.

**Baseline:** `v1.0.0-beta.157`, clean tree.

**Scope:** the subsystems the product bets on — `graph-embedding`,
`graph-clustering` (community), `graph-index`, `graph-query` caches, and the
retention/config surface underneath them. Retention and operational hardening
were already on the pre-v1 list; this audit tests whether the *core* needs the
same rigor.

**Bias:** simplification over construction. A finding whose fix is "delete the
knob / delete the bucket / delete the feature" ranks above one whose fix adds a
layer. Go and semstreams idioms are non-negotiable.

---

## 0. The unifying defect: phantom signals

The graph core is full of names that read as load-bearing checks and are wired to
nothing. Not drift, not staleness — **things that never worked at all**, in
production code and in the tests that are supposed to catch it. Every one was
found by the same one-line method: grep for the consumer.

| Phantom | Site | What it looks like | What it is |
|---|---|---|---|
| `semstreams_clustering_runs_total` | `test/e2e/scenarios/tiered_semantic.go:54`, `tiered_structural.go:49` | the gate that waits for clustering | **metric does not exist**; reads 0.0 forever |
| `semstreams_graph_embedding_queued_total` | `tiered_semantic.go:609` | queue-depth evidence | **does not exist**; reported as real data in every result JSON |
| `communityCache.IsAvailable()` | `processor/graph-query/component.go:663` | the guard justifying not stopping a watcher | **method does not exist** |
| `IsReady()` | `community_cache.go:302` | cache readiness | exists, latches true, **zero production callers** |
| `SetPending` → `graph_embedding_pending` | `graph/embedding/worker.go:61` | pipeline backlog gauge | registered, **never called**; permanently 0 |
| `EMBEDDINGS_CACHE` | `processor/graph-embedding/component.go:555` | the obvious bucket to check | created, required by validation, **never written** |
| `NATSCache` + `Cache` iface | `graph/embedding/cache.go` | the writer for that bucket | **never wired**; `HTTPEmbedder.cache` always nil, branch dead |
| `min_community_size` | `processor/graph-clustering/component.go:59` | operator knob | validated, defaulted, **never passed anywhere** |
| `min_embedding_coverage` | `docs/advanced/01-clustering.md:290` | operator knob, default 0.5 | **no Go binding exists** |
| `levels` config | `docs/concepts/07-community-detection.md:130` | operator knob, max 10 | **no such field**; `WithLevels(3)` hardcoded |
| edge-weight knobs (gh#461) | — | tunable clustering | `GetEdgeWeight` returns 1.0 unconditionally |
| `transferSummary` 0.8 threshold | `graph/clustering/lpa.go:29` | LLM-cost suppression | **cannot fire** — write ordering defeats it |
| enhancement queue-depth gauge | `enhancement_worker.go:325` | backlog | brackets work already on a goroutine; **bounded by 5** |

Read the table as one claim: **an operator diagnosing this subsystem consults a
dashboard, a config surface, and an e2e result, and a large fraction of what all
three report is fiction.** The semsource debrief's "nasty decoy" instinct about
`EMBEDDINGS_CACHE` was right and understated — it is one of thirteen.

The remediation is mostly *deletion*, which is the good news: a lying gauge is
worse than no gauge, and deleting it is a smaller diff than wiring it up.

## 1. Where signals do exist, they measure activity, never correctness

Every metric the graph core exports counts *work performed*:

```
semstreams_graph_embedding_embeddings_generated_total
semstreams_graph_embedding_queued_total
semstreams_graph_embedding_pending
semstreams_graph_embedding_dedup_hits_total
semstreams_graph_embedding_errors_total
semstreams_graph_embedding_embedder_type
semstreams_clustering_runs_total
semstreams_graph_clustering_staleness_at_detection_ms
```

Not one of them measures whether the *answer* is right. There is no counter for
truncated text (gh#602), no counter for a resolved-but-empty body (gh#600), no
signal that a vector excludes its entity's title (gh#601), no measure of
partition stability across runs, and community ground-truth violations are
recorded as warnings so `validation_errors` stays 0 while the partition is wrong.

This is not a coincidence, and it is the finding that explains all the others.
**Every issue filed in this wave is a silent-degradation bug** — gh#600, #601,
#602, #606, #607, #608, #609, #597 without exception. A subsystem whose entire
observability surface is throughput counters can only fail silently: an operator
watching the dashboard sees green work being done, and every one of these defects
is invisible from there. gh#600 states the endgame precisely — a day after
ingest you get correct recall, correct ranking, `embedding.ready: true`,
`indexed_revision == target_revision`, and empty bodies.

The project already owns the discipline that forbids this. ADR-033 and
`docs/concepts/19-observability-as-operating-curves.md` argue for regime-indexed
curves with named primary metrics and guardrails. The graph core predates that
discipline and was never brought under it. **The remedy is not more metrics in
general — it is one correctness/quality axis per subsystem, treated as a v1 exit
gate.** gh#597's `semstreams_graph_ingest_batch_query_missing_total{reason}` is
the shape to copy: it makes a silent drop countable.

## 2. Test quality — the prior was directionally right but mislocated

Test volume is not the problem, and neither are the unit tests. This is a
correction worth stating plainly, because it redirects the remedy.

| Package | prod LOC | test LOC | ratio |
|---|---|---|---|
| `processor/graph-index` | 4,699 | 10,016 | 2.13 |
| `processor/graph-ingest` | 6,422 | 13,172 | 2.05 |
| `processor/graph-clustering` | 3,132 | 4,671 | 1.49 |
| `graph/query` | 3,253 | 4,660 | 1.43 |
| `pkg/fusion` | 3,364 | 4,744 | 1.41 |
| `processor/graph-embedding` | 2,431 | 2,938 | 1.20 |
| `processor/graph-query` | 5,599 | 5,860 | 1.04 |
| `graph/clustering` | 4,028 | 3,829 | 0.95 |
| `graph/embedding` | 2,130 | 1,156 | **0.54** |

**The unit tests are better than feared.** `query_failure_honesty_test.go`,
`storage_cache_health_test.go` (real channel synchronization, no sleeps, names the
design it regressed), `storage_scope_test.go` (pins the warm path *because* a
cache-path omission would be a silent no-op), and
`TestIntegration_CommunityCacheCrossLevelCollision` (real NATS, production key
format, level-1 half exists precisely because level 0 is the Go zero value) are
genuinely adversarial. Several carry comments naming the mutation they were
written to catch. That is the discipline working.

**The Goodhart traps migrated into `test/e2e`.** Two stages there are
structurally incapable of failing:

- **`executeValidateZeroClusters`** (`tiered_structural.go:47-69`) gates on the
  phantom `clustering_runs_total`, so `clusteringCount` is permanently 0 and
  `0 <= ExpectedClusters` is permanently true. **The structural tier's "clustering
  MUST NOT occur" constraint has never once been evaluated.** Its sibling
  `executeValidateZeroEmbeddings` uses a real metric and is sound — drift in one of
  a matched pair.
- **`waitForCommunities`** (`tiered_semantic.go:42-81`) polls on the same phantom,
  so the loop body never executes a fetch. Every call burns the full 90 seconds
  then does one point-in-time fetch with no retry. The success log at line 59 is
  dead code that has never printed — **only the "after timeout" line can fire,
  which is a visible tell already sitting in every e2e log we have.**

Plus a systemic pattern: `validate*` functions return `error` and return `nil`
unconditionally. `validateEmbeddingQueueHealth` (`tiered_semantic.go:601-656`)
fetches `generated` — the one value proving work happened — records it, and never
asserts on it; zero embeddings generated prints "Health check passed."
`tiered_statistical.go:456` excuses total community-detection failure via
`&& totalCount > 0`. The k-core check asserts `MaxCore < 0` on a quantity that is
non-negative by construction (`tiered_statistical.go:545`).

**And the production wire is untested where it matters most.**
`CommunityCache.WatchAndSync` has zero test coverage — all 11 cache tests call
`handleUpdate`/`handleDelete` directly. Invert or delete the `KeyValueDelete`
dispatch and every one stays green, leaving pruned communities resident forever.
That is the same shape as the bare-ID fixture bug: a wire nothing drives. The same
function also lacks an `ok` check on `<-watcher.Updates()` (`community_cache.go:70`),
so a closed channel means an infinite hot spin plus log flood — independently
corroborated by the embedding audit finding the identical hazard handled
*correctly* two packages away.

So the remedy is not "write more tests." It is: fix the two phantom metrics,
promote warn-to-fail across the e2e validators, and drive the production wire in
the handful of places that don't.

## 2b. The two halves of the safety net have the same hole

| Package | prod LOC | test LOC | ratio |
|---|---|---|---|
| `processor/graph-index` | 4,699 | 10,016 | 2.13 |
| `processor/graph-ingest` | 6,422 | 13,172 | 2.05 |
| `processor/graph-clustering` | 3,132 | 4,671 | 1.49 |
| `graph/query` | 3,253 | 4,660 | 1.43 |
| `pkg/fusion` | 3,364 | 4,744 | 1.41 |
| `processor/graph-embedding` | 2,431 | 2,938 | 1.20 |
| `processor/graph-query` | 5,599 | 5,860 | 1.04 |
| `graph/clustering` | 4,028 | 3,829 | 0.95 |
| `graph/embedding` | 2,130 | 1,156 | **0.54** |

Every subsystem that produced a bug this month is well above 1.0. The community
subsystem — 1.49 and 0.95 — produced **four mutation-green tests across three
review rounds**, and its graph-query integration fixtures seeded bare-ID keys,
testing a wire that did not exist.

So "add tests" is the wrong prescription and would deepen the trap. The correct
prescription is **mutation-testing the load-bearing assertions** of the existing
suites, and treating warn-only validation as a defect class. `graph/embedding` at
0.54 is the one place where volume genuinely is thin, and it is also the least
audited code in the core.

## 3. Corrections to the working brief

### 3.1 "The semantic tier's effect on clustering was never built" — not accurate

It was built, never wired, and then **deliberately deleted** — by
`docs/adr/061-community-semantic-virtual-edges.md` (2026-06-24, resolving
gh#238). Verified: `graph/clustering/semantic_provider.go` is gone, gh#238 is
closed, and the only surviving mention of `semantic_edges` or `SemanticProvider`
anywhere in the repo is ADR-061 itself. The removal was clean.

This matters because it converts an open design question into a **re-litigation
of a month-old decision made under 3-lens adversarial review**. Anyone proposing
to make the semantic tier affect the partition is proposing to revert ADR-061 and
owes the argument it rejected.

### 3.2 ADR-061 already ran the consumer trace that sizes the community question

Its finding #2 is load-bearing for the keep/redesign decision and should not be
re-derived:

> the primary semantic path ranks entities **entirely from the embedding index**
> (`searchEntitiesSemantic`); community membership is **post-hoc decoration**
> (`findCommunitiesForEntities` maps already-ranked entities to communities for
> summaries). `enrichGlobalResponse` provably never touches `resp.Entities`.
> Community structure is load-bearing for *retrieval* only on the text-based
> **fallback** path (fires only when semantic search returns zero/errors) and in
> local search.

Read against this audit's other findings — the partition is non-deterministic,
uniformly unweighted, three redundant runs deep, and passes 0–1 of 3 ground-truth
checks — the community subsystem is **expensive, incorrect, and barely
load-bearing on the path the product sells**. That combination argues for
shrinking scope at v1, not for investing in a redesign.

### 3.3 gh#600 is not newly discovered work — it is designed, unstarted work

The failure mode is already written down in the `bounded-storage-operability`
OpenSpec change, which stands at **0 of 35 tasks**:

> separate lifetime classes (`windowed/ephemeral`, `entity-owned/current`, and
> `retained/audit`) mapped to independently configured store instances or
> ObjectStore buckets; configurable TTL where legal … **no expiring object may be
> advertised through a durable live `StorageReference`**

That last clause is gh#600's exact bug, anticipated in the design and never
implemented. Meanwhile the shipped guardrail's scope is narrow by construction —
`openspec/specs/graph-retention/spec.md` binds the D1 no-lifecycle-retention rule
to `ENTITY_STATES` and its derived indexes only, so `CONTENT` was never covered.

The correct framing for planning: **the retention design predicted this class of
bug; the gap is execution, not discovery.** gh#600's ask #2 (extend the boot
guard to CONTENT) is a spec extension to `graph-retention`; ask #1 (configurable,
default-off TTL) already lives inside `bounded-storage-operability`. Note gh#600's
own coupling warning — that accidental TTL is currently the only thing reclaiming
orphaned blobs, so removing it without an orphan story converts the blob store to
unbounded growth. Both halves are one design.

## 4. No e2e tier runs automatically — the net that would catch this is manual-only

`.github/workflows/ci.yml` runs exactly five jobs: `lint`, `test`, `build`,
`schema-validation`, `status-check`. **No e2e tier is among them.** The only
workflow that runs e2e at all is `semspec-validation.yml`, and its sole trigger is
`workflow_dispatch` — a human clicking a button.

So every graph e2e tier — `core`, `structural`, `statistical`, `semantic`,
`agentic` — runs only when someone types `task e2e:*` on a laptop. Combined with
two facts already on the record — that repo `main` carries no required checks so
`--auto` merges immediately, and that CLAUDE.md declares a **HARD RULE** requiring
a green e2e tier before any BREAKING change lands — the entire e2e safety net is
unautomated human discipline. beta.18 is the proof of what that costs: a
half-migrated registry retirement shipped through three months of beta releases
because nobody ran `task e2e:semantic` on main.

This is the structural explanation for why this whole class of bug survived to
v1. The community and embedding subsystems' only integration-level verification
lives in the `statistical` and `semantic` tiers, which no machine ever runs; the
checks that *do* run on every push measure activity, not correctness (§1). Both
halves of the net have the same hole in the same place.

It also re-scopes the open e2e coverage gaps — gh#599 (no tier exercises fusion
`Fuse`, batch reconciliation, or unhydrated reporting) and gh#391
(`e2e:research-graph` routes around `pkg/fusion` entirely). Closing them buys
nothing while the tiers are unreachable from CI. **Automating one tier is worth
more than writing three new ones.**

## 5. Process signal: eleven OpenSpec changes are open, most partially complete

```
graph-index-replacement-semantics   15/19    rule-event-identity          12/16
rule-evaluation-completeness         8/10    rule-entity-watcher-hardening 11/12
predicate-raw-key-representation     9/14    predicate-contract-enforcement 37/43
loop-iteration-budget                9/11    entity-id-contract            48/54
runtime-lifecycle-idempotency       16/17    bounded-storage-operability    0/35
poison-response-scoping           complete
```

Eight changes sit in the 80–95% band. That pattern — many changes taken to
"working" and none to "closed" — is the same shape as the defects this audit
found: the last increment of each change is disproportionately the observability,
guardrail, and negative-path work, which is exactly what is missing in the code.
**Finishing the open changes and hardening the graph core are substantially the
same work**, and any new epic should be checked against these first to avoid
opening a twelfth front.

---

## 6. The issue queue, re-cut by theme

72 issues are open. Cut by subsystem rather than by age, the graph core accounts
for roughly half, and the clusters are tighter than the queue's flat ordering
suggests.

**Content & embedding (silent loss of indexed text)** — gh#600 (24h CONTENT TTL),
gh#601 (offloaded entities never embed their title; `text_suffixes` inert),
gh#602 (8000-char cap, silent truncation). These three are one bug with three
faces: *text that the operator believes is indexed, is not, and nothing says so.*
They should be planned as a unit, not three tickets. All three are semsource
field reports against beta.156.

**Community (correctness + cost)** — gh#606 (shared-mutable index, tier doesn't
affect partition), gh#607 (enhancement worker clobber + unbounded re-enhancement),
gh#608 (`markFailed` wrong level; LLM-down-at-startup is permanent), gh#609
(query cache; partially fixed in .157), gh#465 (adaptive edge synthesis). Given
§3.2, this cluster's disposition is a **scope decision, not a bug queue**.

**Fusion** — gh#597 (silent drop; cross-store half still open), gh#599 + gh#391
(e2e never exercises `Fuse`), gh#603 (Impact facet names nothing), gh#348
(`WrapTransient` collapses Invalid/Fatal), gh#376 (deterministic fusion
primitive). Note gh#599/#391 are gated behind §4 — the tiers don't run.

**Index & retention** — gh#527 (Increment-0, memory says NEXT UP), gh#525/#526
(gated on measurement), gh#330, gh#306. Plus the unstarted
`bounded-storage-operability` (0/35) that owns gh#600's real fix.

**Read-side fan-out** — gh#579 (design gap), gh#587, gh#588, gh#586, gh#571.
Coherent cluster, ADR-081 shipped the substrate; these are its adopters.

**Hygiene / dead surface** — gh#589 (dead `storage.Watch`), gh#422 (unused
exported query API), gh#323, gh#546, gh#315. Cheap deletions that shrink the v1
surface; they pair naturally with whatever the Class-C inert-config sweep returns.

**Docs drift** — gh#486, gh#457 (docs still teach the retired `processor/reactive`),
gh#367, gh#340, gh#176.

The reading that matters for planning: **the top three clusters are all
silent-degradation families, and they are the clusters that touch what the
product sells.** The hygiene and docs clusters are cheap and unblocked. The
retention cluster is designed and unstarted.

## 7. Findings by subsystem

### 7.1 Community — the evidence supports shrinking, not redesigning

**Cost: the backlog provably never drains at default settings.** Detection is
interval-driven (30s, `processor/graph-clustering/component.go:1075`); enhancement
is KV-watch driven (`graph/clustering/enhancement_worker.go:216`), so the
detector's writes *are* the queue. There is exactly one production LLM call site
(`enhancement_worker.go:340`), bounded by 5 workers (`component.go:279`) each
blocking on a synchronous call with a 60s default timeout (`component.go:48`):

```
calls per cycle ≈ 3C   (C communities × 3 levels)
throughput      = 5 workers × (30s / T)
  T =  5s →  30 calls/cycle → drains iff C ≤ 10
  T = 60s →   2.5/cycle     → drains iff C = 0     ← the default
  T = 300s→   0.5/cycle     → never
```

**Change detection exists and cannot fire.** This is the sharper finding. Two
layers were built — a `SummaryStatus != "statistical"` gate
(`enhancement_worker.go:310`) and the Jaccard `transferSummary`
(`lpa.go:212`, threshold 0.8 at `lpa.go:29`) — and write ordering defeats both.
The detector's statistical summarizer unconditionally stamps
`SummaryStatus = "statistical"` (`summarizer.go:125`) and Puts
(`lpa.go:352`), firing the watcher immediately for every community at every
level. The Jaccard transfer is Phase 2, running only after all three levels
finish — 4.4s–23.7s later per the in-code measurement at `lpa.go:164`. The worker
reads the *queued snapshot* (`enhancement_worker.go:304`), so it tests the stale
status captured at Put time. **A transfer that writes `llm-enhanced` at t+10s
cannot retract the `statistical` entry already sitting in the channel.** The 0.8
threshold buys nothing on the cost axis — it only makes readers see a summary
sooner.

**NEW — unfiled: a lagging worker resurrects pruned communities.** The watcher
channel is buffered at 256 with a blocking send, so the backlog back-pressures
rather than drops, and the worker ends up enhancing partitions many cycles old —
then writes them via a blind `Put` (`enhancement_worker.go:355`,
`storage.go:104-114`). If Phase-3 `Prune` (`lpa.go:237`) already deleted that key,
**the write resurrects a dead community plus its entity mappings**, and
graph-query's cache — watching the same bucket — serves it. `markFailed`
(`enhancement_worker.go:383`) has the same shape. No guard was found on either
path. This is a direct consequence of .157's write-then-prune change interacting
with the pre-existing blind-Put worker; it needs an issue.

**NEW — the queue-depth metric cannot show the backlog.** `IncQueueDepth` /
`DecQueueDepth` (`enhancement_worker.go:325-326`) bracket work *already pulled
onto a worker goroutine*, so the gauge is bounded by 5 and never exceeds it. An
operator watching it sees a healthy pipeline while the channel backs up. There is
no metric on the real queue — a textbook instance of §1.

**Levels 1 and 2 have essentially no consumers.** `GetCommunitiesByLevel` /
`GetEntityCommunity` have exactly two production call sites, both in graph-query
(`graphrag.go:1128`, `:1721`), both reading `req.Level` from a plain `int` with no
default — so absent input means level 0. The gateway forwards `level` only when
the caller supplies it (`gateway/graph-gateway/component.go:1141-1154`). The only
callers anywhere passing level > 0 are **one e2e probe**
(`test/e2e/scenarios/tiered_statistical.go:269`, level 1). **Level 2 has no
consumer at all.** Levels 1–2 are ~2/3 of detection cost and ~2/3 of the LLM bill,
serving one test assertion.

**Dead code and dead knobs found alongside:**

| Item | Site | LOC |
|---|---|---|
| `InferRelationshipsFromCommunities` + `InferenceConfig` + `InferredTriple` + `computeCommunityTightness` + `hasExplicitEdge` — **zero callers, production or test** | `lpa.go:510-681` | 172 |
| `transferSummary` + `jaccardIndex` (retired by the ownership split) | `lpa.go:683-751` | 69 |
| `WithLevels(3)` → 1 collapse (incl. cache level dimension, `{level}.{id}` keys) | multiple | ~120-140 net |
| `min_community_size` — validated, defaulted, **never passed to anything** | `component.go:59, 272-273, 396` | dead knob |

**The ownership split is ADR-scale, ~500 production LOC.** All four
COMMUNITY_INDEX writers marshal and blind-Put the *entire* `Community` struct with
no CAS on any path (`storage.go:89-127`). Writers 3 and 4 — the enhancement worker
— stamp a stale full-record snapshot over whatever the detector has since written.
That is the ownership violation. Moving summaries to a store keyed by
content-hash-of-membership retires gh#607 wholesale (exact-match lookup replaces
the 0.8 heuristic), makes gh#608's wrong-level write structurally impossible (no
level dimension in a membership hash), and closes the resurrection bug above. It
is ADR-scale because it changes a storage key contract, requires a pre-v1 state
wipe, and retires the operator-visible `SummaryStatus` vocabulary. Worth noting:
**`SummaryStatus` has no production reader outside the two writers themselves** —
every other reference is an e2e assertion.

### 7.2 gh#609 residuals — all three still present post-.157

**Latched ready.** `community_cache.go:39`, set true at `:74` on the nil-entry
marker. `grep "ready = false"` across `processor/graph-query/` returns nothing —
there is no unlatch path, so readiness survives bucket loss, watcher death, and an
empty partition.

**The phantom guard is worse than reported.** `component.go:663` justifies not
stopping the watcher on bucket loss with "the `communityCache.IsAvailable()` check
in handlers will prevent queries." `CommunityCache` has no such method. And
**`IsReady()` has zero production callers** — no GraphRAG handler consults cache
readiness before serving. The signal is computed, latched, exported in `Stats`,
and gates nothing. A real decision rests on a method that does not exist.

**Cold-start authoritative-empty.** `runDetectionLoop`'s select has only
`ctx.Done()` and `ticker.C` (`component.go:1084-1107`) — **no pre-loop first
run** — so the first partition lands at t=30s. Meanwhile the cache latches ready
in milliseconds on an empty bucket and logs a successful "initial sync complete"
(`community_cache.go:70-79`). For the first ≥30s after cold start, graph-query
serves confidently empty answers. This is the same failure shape ADR-085 closed on
the *rebuild* path; the cold-start path never got the equivalent treatment because
that fix targeted the detector's write pattern, not the reader's readiness
semantics.

### 7.3 Docs actively teach features that do not exist

Beyond drift, these are false:

| Doc | Claim | Reality |
|---|---|---|
| `docs/advanced/01-clustering.md:354` | co-membership creates edges "where only semantic similarity existed" | LPA never uses semantic similarity; the predicate named is wrong (`inferred.cluster.clustered-with`, `lpa.go:601`); the producing function has zero callers |
| `docs/advanced/01-clustering.md:290,302` | `min_embedding_coverage`, default 0.5 | No Go binding exists anywhere |
| `docs/concepts/07-community-detection.md:112-148` | communities "can nest"; level 1 is coarser | `detectHierarchicalLevel` re-runs LPA over the identical set (`lpa.go:459-466`); `ParentID` is always nil |
| `docs/concepts/07-community-detection.md:130-132,167` | a `levels` config field, max 10, default 2 | No such field; `WithLevels(3)` hardcoded at `component.go:1039`; stated default contradicts code |
| `docs/concepts/07-community-detection.md:148` | "start with level 1 for general queries" | Advice to prefer a duplicate run |

### 7.4 ADR-061 is shipped but still marked Proposed

Its decision was fully implemented and verified removed, yet the status line reads
`Proposed — 2026-06-24`. Under the repo's own ADR discipline that is a
truth-source ambiguity. Two things follow: promote it to Accepted, and note that
**it never claimed the broader statement** — it removed one mechanism (virtual
edges), not "the semantic tier does not affect the partition." If that broader
contract is what we want, it needs saying explicitly.

If the intent were instead **built**, the landing point is confirmed:
`kvProvider.GetEdgeWeight` (`component.go:1591-1594`), which today discards both
arguments and returns 1.0. The similarity primitive is already wired into the
component (`similarity.go:66,130`). Two structural obstacles, both needing an
explicit decision rather than a default: `FindSimilar` is a top-K query from one
entity, not a pair scorer, so `GetEdgeWeight(from, to)` needs either a new
pairwise query or a pre-materialized per-pass map; and embeddings exist only for
text-bearing entities, so weighting would apply to an arbitrary subset while
telemetry entities kept 1.0. `GetEdgeWeight` sits in LPA's inner loop (up to 100
iterations × levels), so any non-local implementation is a per-iteration network
multiplier.

### 7.5 Embedding — two criticals, both "green dashboard, broken index"

**CRITICAL — the dedup bucket is model-blind.** `ContentHash` hashes text and
nothing else (`graph/embedding/cache.go:62`): no model name, no embedder type, no
dimensions, no truncation cap. `DedupRecord` (`storage.go:71-76`) carries no
model field either, so a stale-model vector is not even detectable after the fact.
The dedup check short-circuits generation before the embedder is consulted
(`worker.go:373-386`), and `saveAndNotify` then stamps the returned vector with the
**current** embedder's identity (`worker.go:416-418`).

Failure: an operator runs `configs/statistical.json` (`embedder_type: bm25`) then
switches to `configs/semantic.json` (`http`) against the same NATS state.
`EMBEDDING_DEDUP` is never cleared and has no TTL. Every already-embedded entity
returns its **384-dim BM25 vector** and is written to `EMBEDDING_INDEX` labelled
with the neural model's name. If the neural model is also 384-dim — TEI's
`all-MiniLM-L6-v2` default — `CosineSimilarity` computes a real-looking score
across two unrelated vector spaces. If dimensions differ it returns 0.0 on length
mismatch (`vector.go:17-19`), so the entity ranks last forever rather than
erroring. `errors_total` 0, readiness 1, and `dedup_hits_total` *rising* — which
reads as a cost win.

Compounding: `embedder_type` also flips the truncation cap 8000→4000
(`component.go:844-847`), so **the same switch changes the text that was embedded
while leaving the key identical.** That makes gh#602 a dedup-poisoning vector, not
just a truncation issue. Zero tests touch this path (`SaveDedup` /
`GetByContentHash` appear in no `_test.go`).

Fix is one line at two call sites — fold embedder type, model, and cap into the
hash. No migration: old keys simply never match again.

**CRITICAL — `embedding.ready: true` attests "we stopped trying", not "vectors
exist".** Readiness is pure watermark coverage (`graph/index_status.go:167`), and
**failure is a terminal outcome that advances that watermark**: every error path
routes to `markFailed` → `SaveFailed` → the deferred `onTerminal` fires →
`completeEmbedding` drains the watermark (`worker.go:331-339`,
`readiness.go:33-43`). So if semembed is down during cold start, every entity gets
`Status: failed`, the watermark reaches target, and the component publishes a fully
green ADR-083 envelope to `GRAPH_STATUS` — `Ready: true`, lag 0,
`bootstrap_complete: true` — holding **zero usable vectors**.

This is the `readiness cannot license an absence claim` discipline, but sharper
than the graph-index case: for graph-index "indexed" means the projection was
written; here "terminal" includes *failed*, so coverage and usability are actively
conflated. Nothing gates on `errors_total` and no gauge reports the failed count.

**The readiness envelope has no consumer, and the one real consumer fails open.**
Every `readiness.NewWatcher` call site watches `KeyGraphIndex`, never
`KeyGraphEmbedding` (`graph-clustering/component.go:1127`, `graph/query/client.go:480`,
`pkg/fusion/fusionnats/client.go:139`). The code admits it at
`graph-embedding/query.go:33`. The reachable bad state runs the *opposite*
direction from the one in the brief: clustering gates only on graph-index, goes
ready, calls `graph.embedding.query.similar`, gets a classified
`ErrorCodeIndexNotReady` — and `FindSimilar` swallows it and returns `nil, nil`
(`graph-clustering/similarity.go:93-99`). Clustering then commits communities
computed with **zero semantic input, indistinguishable from a graph that genuinely
has no semantic neighbors.**

**Other HIGH findings:**

| Finding | Evidence | Consequence |
|---|---|---|
| Entity deletion never removes its vector — `DeleteEmbedding` has one caller, the no-text skip | `worker.go:354`; tombstone branch `component.go:1171-1177` | orphaned vectors returned by search forever; `EMBEDDING_INDEX` grows with *historical*, not live, entity count |
| Closed KV watcher permanently halts all generation, logged at **Debug** | `worker.go:285-289` | the component's *own* `ENTITY_STATES` watcher handles this correctly via `watchUnavailable` (`component.go:1104-1108`) — the hazard is recognized and applied inconsistently |
| Vector cache is permanently non-authoritative after one blip; re-Start impossible | `storage.go:381-385, 407-441` | clustering's per-entity `FindSimilar` degrades to O(n²) KV reads until restart |
| Lost-update race on `EMBEDDING_INDEX` — read-modify-write, no CAS, 5 workers on one channel | `storage.go:189-211`, `:223-237` | old text's vector persists as `generated`; `ContentHash` and `Vector` can desynchronize |
| `batch_size` 1–9 yields **zero** worker goroutines; `Start` returns nil and logs `workers=0` at Info | `component.go:850`, `worker.go:222` | watcher with no consumer; every entity pending forever; latent until an operator tunes it down |
| BM25 IDF corpus is in-memory, incrementally mutated, never persisted | `bm25_embedder.go:64-70, 174-178` | vectors are mutually incomparable (first computed at `docCount==0`, IDF hardcoded 1.0); restart forks the space; **search queries pollute the document corpus** via `GenerateQuery`→`Generate`→`updateStats` |
| StorageRef lane hashes the storage **key**, not content | `component.go:1412` | in-place content update serves the old vector forever; masks gh#600's body disappearance from the pipeline entirely |
| Every entity revision re-embeds from scratch; no content-hash short-circuit | `component.go:1336-1343` | **measured 201/470 = 43% dedup hits** in the latest statistical e2e run; a frequently-updated entity can be persistently invisible to search during its pending window |

**Cleanest pure simplification available:** delete `EMBEDDINGS_CACHE` plus
`NATSCache`, the `Cache` interface, the dead cache branches in `http_embedder.go`,
and the `SetPending` gauge — roughly 100+ lines, zero behavioral blast radius, and
it removes two of the three false signals an operator consults while diagnosing
any of the above.

### 7.6 Retention & config — one blocking find, independently verified

**BLOCKING — the rule processor creates `ENTITY_STATES` with a 7-day TTL.**
`processor/rule/entity_watcher.go:97-103`. I verified this directly rather than
relaying it:

```go
return rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
    Bucket:      bucketName,          // == gtypes.BucketEntityStates
    History:     10,
    TTL:         7 * 24 * time.Hour,  // 7 days
    MaxBytes:    -1,
})
```

`getOrCreateBucket` (called from `prepareEntityWatcher:170`, the core rule-engine
watch path) tries `GetKeyValueBucket` first and creates on miss — so it fires on a
cold cluster whenever the rule processor reaches watcher setup before graph-ingest
reaches `initStorage`. And that ordering is a genuine race: I confirmed
`service/component_manager.go:366` seeds the start list by **iterating a Go map**
and `:380-383` starts every component **concurrently in its own goroutine**. There
is no dependency ordering; the winner is re-rolled every boot.

Two outcomes, both bad. Co-deployed: rule processor wins, graph-ingest's
`AssertNoLifecycleRetention` fires, **graph-ingest fails to start
nondeterministically on a fraction of cold boots — and it looks like a flake.**
Split deploy without co-located graph-ingest: no guard runs and **the live graph
silently expires on a rolling 7-day window** with every signal green.

This is a missed emitter of an already-completed migration. The gh#484 sweep set
`TTL: 0` in `graph/query/client.go` and in the agentic-tools registration — where
`register_graph_query.go:26-31` documents this precise failure mode in a comment.
`processor/rule/entity_watcher.go` was missed, and carries 7 days rather than the
24h that was removed elsewhere. Textbook `sweep all emitters of a migrated
write-verb`.

**HIGH — a GET endpoint auto-creates arbitrary buckets with a 7-day TTL.**
`service/message_logger_http.go:425-436`, verified. The bucket name is
caller-supplied and validated only against path traversal — no allowlist. So
`GET /message-logger/kv/SPATIAL_INDEX` on a cluster where that bucket doesn't yet
exist **creates it with a 7-day TTL**, and the index silently expires forever
after. For `ENTITY_STATES` it instead wedges graph-ingest's next restart. A typo'd
bucket name creates a real permanent bucket rather than 404ing — which is why the
handler's own "Bucket not found" branch at `:407` is unreachable. `message-logger`
is enabled in every shipped config, including all three graph-bearing ones. The
correct pattern is in the adjacent file: `message_logger_kv_watch.go:196` uses
`GetKeyValueBucket` and documents "doesn't create if not exists".

**Guard coverage is two buckets out of seventeen.** `AssertNoLifecycleRetention`
has exactly two callers, both in graph-ingest. The repo already contains the
enumeration an extended guard needs — `graph.FrameworkOwnedBuckets()`
(`graph/constants.go:46-65`) — and one gap in it: **`ENTITY_SUFFIX_INDEX` is
missing from that list**, so it is invisible to both the retention guard and the
`update_kv` write-ownership guard. A rule can legally `update_kv` into it today.

**The 24h ObjectStore TTL is universal, not CONTENT-specific.**
`storage/objectstore/store.go:111-115` is the *only* `CreateObjectStore` call in
the repo, so gh#600's TTL also governs `AGENT_CONTENT` (agentic-loop trajectory
content) and graph-embedding's content store. There is no ObjectStore analogue of
the boot guard anywhere.

**Other silent-literal findings:** `graphrag.go:1176` truncates the GlobalSearch
corpus at 10,000 after ranging over a **map** with no intervening sort — so beyond
10k candidates the same query on an unchanged graph returns a different arbitrary
subset every call, unlogged and unflagged, feeding LLM answer synthesis as if
complete. `config/streams.go:430` hardcodes `DiscardOld` with no operator
override, so an operator who sets `max_bytes` to bound footprint gets silent
oldest-message eviction and a successful ack for a message that displaced an
unconsumed one. Fusion's relations facet (`engine_lens.go:401`) and path-set
(`engine_facets.go:117`) truncate silently while every *other* cap in that package
reports — a live inconsistency within one package.

**Four more dead knobs, all deletion candidates:** `max_hop_distance`
(graph-clustering) is consumed **only by a log statement** that echoes it back to
the operator as if effective (`structural.go:35-36`); `min_community_size` is inert
*and* shadows a divergent real default of 2 in `lpa.go:524`; `batch_size` is inert
in graph-clustering and graph-index and silently repurposed as a worker count in
graph-embedding. **No graph processor actually batches** — the field was
copy-propagated across component scaffolds.

*(One item needs sister-repo confirmation before deletion:
`InferRelationshipsFromCommunities` has no in-repo caller but is an exported method
on an exported interface — check semsource/semconnect before removing the
`InferenceConfig` path. The component-level `MinCommunitySize` field is inert
regardless.)*

---

## 8. Proposed shape of the work

Five tracks. Ordered by "what does damage while we deliberate," not by size.

### Track 1 — Fix now, no design needed (days)

These are one-to-three-line changes with verified failure modes and no open
questions. They should not wait for the rest.

| # | Fix | Why it can't wait |
|---|---|---|
| 1 | `entity_watcher.go:97` → delete the create path (rule processor is a reader) | nondeterministic graph-ingest boot failure **or** silent 7-day graph expiry |
| 2 | `message_logger_http.go:430` → `GetKeyValueBucket`, return the existing 404 | a read-shaped HTTP call creates retained state on live-graph buckets |
| 3 | Dedup key → fold in embedder type + model + cap | a supported config switch silently corrupts the vector space |
| 4 | Two phantom e2e metrics | the structural tier's core constraint has never been evaluated |
| 5 | `WithWorkers(max(1, …))` or an explicit `workers` field | in-range config silently starts zero embedding workers |
| 6 | Tombstone branch → `DeleteEmbedding` | makes the already-written cache-delete path live |
| 7 | Sort before truncating at `graphrag.go:1176` | nondeterministic search results on an unchanged graph |

### Track 2 — Delete (one PR, no behavior change)

`EMBEDDINGS_CACHE` + `NATSCache` + `Cache` iface + dead `http_embedder` branches +
`SetPending` gauge (~100 LOC) · `max_hop_distance` · `min_community_size` ·
`batch_size` from graph-index and graph-clustering · the three `TTL` fields on
`graph/query.Config` · `InferRelationshipsFromCommunities` and friends (172 LOC,
pending sister-repo check) · `HTTPEmbedder.dimensions` and `Dimensions()`.

Roughly **400–500 LOC removed**, most of the phantom table in §0 with it. This is
the highest ratio of operator-confusion-removed to risk-taken in the whole audit.

### Track 3 — Make the safety net real (the force multiplier)

Everything else is one-shot; this changes the odds on the next wave.

1. **Automate one e2e tier in CI.** Per §2b, writing new e2e coverage while no
   tier runs and two stages can't fail is motion without progress.
2. **Promote warn-to-fail across the e2e validators** — every `validate*` that
   returns `error` and always returns `nil`.
3. **Extend `AssertNoLifecycleRetention`** to `FrameworkOwnedBuckets()` +
   `ENTITY_SUFFIX_INDEX` + an ObjectStore analogue. This is gh#600 ask #2 and it
   would have caught findings A-1 and A-2 at boot.
4. **Add `ENTITY_SUFFIX_INDEX` to `FrameworkOwnedBuckets()`** — a write-ownership
   gap independent of retention.
5. **One correctness metric per subsystem**, per §1: embedding `failed` count,
   truncation count, real enhancement queue depth. Copy the shape of gh#597's
   `batch_query_missing_total{reason}`.

### Track 4 — Decisions the audit surfaced but must not make alone

- **Community scope at v1.** The evidence (§7.1, §3.2) says: non-deterministic,
  uniformly unweighted, three redundant runs deep, 0–1 of 3 ground truth, level 2
  has no consumer and level 1 has one e2e probe — and ADR-061 already established
  it is post-hoc decoration on the primary search path. `WithLevels(3)→1` alone
  retires ~120–140 LOC and the entire cross-level ID-collision class. **The
  recommendation is to shrink to one level and defer the ownership split**, which
  is ADR-scale (~500 production LOC, a storage-key contract change, a pre-v1 state
  wipe) and only pays off if community stays strategic.
- **Readiness semantics for embedding.** "Terminal includes failed" needs an
  explicit decision, not a patch. It fits ADR-084's existing health-gates frame.
- **BM25 corpus model.** Making vectors corpus-independent *deletes* the mutable
  state problem; persisting the corpus adds machinery. Two-line interim regardless:
  make `GenerateQuery` read-only so searches stop polluting the corpus.
- **gh#600's coupling.** The accidental TTL is the only thing reclaiming orphaned
  blobs. Both halves are one design; `bounded-storage-operability` already owns it.

### Track 5 — File as new issues

Not previously filed, verified this pass: the enhancement worker resurrecting
pruned communities (§7.1) · the six embedding findings in §7.5 not covered by
gh#600–602 · A-1, A-2, A-3 · the silent-literal set (B-2 through B-6) · the inert
knobs (C-1 through C-4) · the e2e phantom metrics.

### What this says about the pre-v1 sweep

The instinct that the sweep needed more rigor was right, and the reason is
structural rather than a matter of effort. Three of these five tracks exist because
**the mechanisms that were supposed to report problems were themselves never
verified** — phantom metrics, inert knobs, a guard covering 2 of 17 buckets, an
e2e suite that no machine runs and that partly cannot fail. The subsystems were
audited; the *instruments* were not.

The cheapest durable lesson: **when a signal is load-bearing, grep for its
consumer.** Every finding in §0 came from that one move, and it is the check that
would have caught all of them years earlier.
