# Pre-v1 core audit — synthesis of two independent audits

Date: 2026-07-21 · Baseline `main` @ `a16a1b42` (v1.0.0-beta.157)

Two audits ran independently against the same baseline with no shared working
notes: a Codex pass (issue triage + architecture + config-use + focused race
tests) and a Claude pass (four parallel subsystem audits: embedding risk,
retention/config sweep, test quality, community cost/shape). This document
reconciles them.

Companion: `prev1-graph-core-audit.md` (the Claude-side detail).

**Why the convergence matters more than either report.** The two passes used
different methods and found the same defects in embedding dedup identity,
out-of-order revision commits, stale vectors after tombstone, BM25 corpus
instability, community ownership, and inert config. Independent convergence on a
finding is the strongest evidence available short of a reproduction. Those items
should be treated as settled and not re-litigated.

---

## 1. Convergent findings — settled, do not re-derive

Both audits found these independently. High confidence.

| # | Finding | Disposition |
|---|---|---|
| 1 | `EMBEDDINGS_CACHE` created, validation-required, never written | **delete** (+ `NATSCache`, `cache_ttl`, the required output port) |
| 2 | Body 24h TTL vs permanent vectors — green signals over empty bodies (gh#600) | **P0** |
| 3 | Dedup identity incomplete: no model/extraction fingerprint; StorageRef lane hashes the **key**, not bytes | **P0** — see §4.1 for the one disagreement |
| 4 | Tombstone and no-text updates leave stale vectors searchable | **P0** |
| 5 | Out-of-order revision commits — old vector overwrites newer under a newer hash | **P0** |
| 6 | BM25 mutates corpus state while embedding; queries pollute the document corpus; restart forks the space | **P0 decision** — pick lexical-index or stateless; do not add locks and call it fixed |
| 7 | Community: partition / statistical summary / LLM enhancement share records with different writers, no CAS | **P0** |
| 8 | "Hierarchy" is 3 identical LPA runs; IDs collide across levels; nothing consumes level > 0 | **collapse to level 0** |
| 9 | Exact membership hash retires the 0.8 transfer threshold wholesale | adopt |
| 10 | First detection waits a full interval — cold-start authoritative-empty window | **run detection immediately** |
| 11 | Cache `ready` is a one-way latch | model as lifecycle state |
| 12 | LLM-down-at-startup permanently disables enhancement (gh#608) | **P0** |
| 13 | Inert config: `cache_ttl`, `batch_size` ×3, `min_community_size`, sibling/system weights | **delete** |
| 14 | `InferRelationshipsFromCommunities` dead (172 LOC) | delete *after* sister-repo check |
| 15 | Tests: warnings-as-failures, `>= 0` assertions, level-1-not-level-0, direct-KV bypass of the cache under test | **P0** |
| 16 | Extend the no-lifecycle-retention boot guard to ObjectStores | **P0** (gh#600 ask #2) |
| 17 | Semantic tier does not affect the partition; docs claim otherwise | delete the claims |

---

## 2. Found by Codex alone — adopt

### 2.1 The exact swallow site for gh#600 (verified)

I described the symptom; Codex found the mechanism. `pkg/fusion/engine_lens.go:349-355`:

```go
if wants[WantBody] && e.body != nil {
    if ref, err := lens.Hydrate(ctx, ent); err == nil && ref != nil {
        if body, derr := e.body.ResolveBody(ctx, ref); derr == nil {
            node.Body = string(body)
        }
    }
}
```

Both the `Hydrate` error and the `ResolveBody` error are discarded; the node ships
with an empty `Body`. The doc comment names the policy — "degrade-don't-fail."
And `Response.Unhydrated` is scoped to **seeds**, not bodies (`contract.go:239`:
"requested seeds that did not load"). So when a body expires there is no signal at
any layer. This is the precise fix site for the hydration-outcome ask, and it is
better than "add a metric somewhere."

### 2.2 SemSource is configured for silent ingest loss (verified)

I swept only semstreams and missed a product-side footgun that is arguably worse
than anything in the framework. `cmd/semsource/run.go:942-944`:

```go
Storage:  "memory",
MaxBytes: 256 * 1024 * 1024,
MaxAge:   "1h",
```

The `GRAPH` ingest stream is **in-memory, 256 MiB, 1-hour age**. It is a buffer
rather than the live graph, but facts can evict before graph-ingest persists them,
and a NATS restart drops everything in flight. `config.Streams` exposes raw
`storage`/`max_age`/`max_bytes`/`replicas` as product configuration, which is
exactly the opaque-lifecycle-knob class we are removing from the framework.
`run.go:840` also still declares `EMBEDDINGS_CACHE` as the graph-embedding output.

Codex's Epic E is correct and I had no equivalent. Raw JetStream retention
mechanics should not be product configuration.

### 2.3 Three-way retention contradiction blocks further machinery

ADR-068 (central reverse index + sweeper), ADR-073 (per-owner reverse knowledge,
sweeper demoted to backstop), and `openspec/specs/graph-retention/spec.md` disagree
— and **both ADRs are still Proposed/design-only**. Accepting one ownership model
and syncing current-truth must precede implementing more retention machinery.
Codex's preference for ADR-073's owner-local model is well argued: literal owner
keys, idempotent replacement, bounded cleanup, no central scan as the primary
correctness mechanism.

### 2.4 `ALIAS_INDEX` has no owner-complete axis

Explicitly outside replacement and deletion today, so alias rename or entity
retirement retains stale aliases. I missed this entirely.

### 2.5 Queue hygiene I did not check

`pkg/graphview` shipped in PR #585, so gh#579 can close with the two proven
migrations tracked under gh#588. gh#527 is partly obsolete after ADR-077. gh#607
can close as subsumed if enhancement is disabled or ownership split. These narrow
the queue rather than adding to it.

### 2.6 Two smaller items

Malformed duration strings can silently default rather than being rejected;
graph-index outputs create unknown buckets that are then ignored.

---

## 3. Found by Claude alone — add to the program

### 3.1 A live blocking bug neither the queue nor Codex names

`processor/rule/entity_watcher.go:97` creates `ENTITY_STATES` with **`TTL: 7 * 24 * time.Hour`**.
The rule processor is a *reader*, but `getOrCreateBucket` creates on a `Get` miss.
`service/component_manager.go:366` iterates a Go **map** and `:380` starts every
component in its own goroutine — no ordering, winner re-rolled every boot. Either
graph-ingest's boot guard fires and **the pipeline fails to start
nondeterministically, looking like a flake**, or on a split deploy no guard runs
and **the live graph silently expires on a rolling 7-day window**.

It is a missed emitter of the completed gh#484 sweep, which set `TTL: 0` elsewhere
and documents this exact failure mode in a comment three files away.

Codex's Finding 6 remedy ("one create/open-and-assert helper for all live graph KV
buckets") *would* catch it, but the violating site was never located. **This jumps
the queue: it is a one-line fix for a live nondeterministic outage.**

Same class: `service/message_logger_http.go:430` — a **GET** endpoint auto-creates
any caller-named bucket with a 7-day TTL, no allowlist. `GET /message-logger/kv/SPATIAL_INDEX`
creates that index with an expiry. Enabled in all three shipped graph configs.

### 3.2 Codex's Epic D has an unstated prerequisite that does not hold

Epic D asks for "the verification matrix in CI." **`ci.yml` runs no e2e tier at
all** — jobs are lint, test, build, schema-validation, status-check, and the only
e2e workflow is `workflow_dispatch`. Every tier runs only when a human types the
command. Combined with main having no required checks and the HARD RULE requiring
e2e before breaking changes, the whole net is unautomated discipline; beta.18 is
the precedent for the cost.

**Automating one tier is a prerequisite for Epic D, not a deliverable inside it.**

### 3.3 Two e2e stages are structurally incapable of failing

Stronger than "tests are weak" — two Prometheus metrics referenced by e2e gates
**do not exist in production code**:

- `semstreams_clustering_runs_total` → `executeValidateZeroClusters`
  (`tiered_structural.go:47-69`) computes `0 <= ExpectedClusters`, permanently
  true. **The structural tier's core constraint has never once been evaluated.**
- Same metric → `waitForCommunities` (`tiered_semantic.go:42-81`) never executes
  its fetch, burns the full 90 s every call; its success log has never printed.
- `semstreams_graph_embedding_queued_total` → reported as real data in every
  result JSON.

Codex's Finding 4 correctly diagnoses weak oracles but treats the tests as
executing-and-lenient. Several are **inoperative**. That changes the fix from
"strengthen assertions" to "these gates have never run."

### 3.4 `embedding.ready` conflates coverage with usability — the exact mechanism

Codex says readiness often means "a watcher once caught up." Sharper: **failure is
a terminal outcome that advances the watermark.** Every error routes `markFailed`
→ `SaveFailed` → deferred `onTerminal` → `completeEmbedding`
(`worker.go:331-339`, `readiness.go:33-43`). semembed down at cold start ⇒ every
entity `failed` ⇒ watermark hits target ⇒ green ADR-083 envelope, lag 0,
`bootstrap_complete: true`, **zero usable vectors**.

### 3.5 A cross-component asymmetry that makes communities silently semantic-free

No `readiness.NewWatcher` call site watches `KeyGraphEmbedding` — all three watch
`KeyGraphIndex`. Clustering gates on graph-index, goes ready, calls
`graph.embedding.query.similar`, receives a classified `ErrorCodeIndexNotReady`,
and `FindSimilar` **swallows it and returns `nil, nil`**
(`graph-clustering/similarity.go:93-99`). Clustering then commits communities with
zero semantic input, indistinguishable from a graph with no semantic neighbors.

### 3.6 Post-.157 regression: the enhancement worker resurrects pruned communities

.157 replaced `Clear` with write-then-`Prune`. The enhancement worker still blind-`Put`s
a stale snapshot (`enhancement_worker.go:355`). If `Prune` deleted that key, the
write **resurrects a dead community plus its entity mappings**, and graph-query's
cache serves it. `markFailed` has the same shape. No guard on either path.

### 3.7 Smaller additions

`batch_size` 1–9 yields **zero** embedding workers while `Start` returns nil and
logs `workers=0` at Info — reachable from an in-range value, not just a negative ·
`graphrag.go:1176` truncates the search corpus at 10 000 after ranging a **map**
with no sort, so the same query on an unchanged graph returns a different arbitrary
subset every call · `ENTITY_SUFFIX_INDEX` is missing from `FrameworkOwnedBuckets()`,
so it is invisible to both the retention guard and the `update_kv` ownership guard ·
`communityCache.IsAvailable()` is cited in a comment as a guard and **does not
exist**, while `IsReady()` has zero production callers · `CommunityCache.WatchAndSync`
has zero test coverage and lacks an `ok` check on `<-watcher.Updates()` (hot spin
on close) · the enhancement queue-depth gauge is bounded by the worker count and
cannot show a backlog · **measured 43 % dedup hit rate** (201/470) in the latest
statistical e2e run · ADR-061 already retired the semantic-clustering intent under
3-lens review and is **still marked Proposed despite being shipped**.

---

## 4. Genuine disagreements

### 4.1 `EMBEDDING_DEDUP` — fix the key, don't remove the bucket

Codex: remove dedup unless its identity is made exact; "written ≠ safe."
Claude: fold embedder type + model + cap into `ContentHash` — one line at two call
sites, no migration (old keys simply never match again).

**Recommendation: fix, don't remove.** The measured hit rate is 43 % of embed
operations. On the neural tier every hit is a saved remote call to semembed, which
is the expensive part; removing dedup imposes that cost permanently to avoid a
hazard that a hash-input change eliminates. Codex's underlying point stands and
should be honored in the fix: the key must identify the *final normalized input*
plus a stable model/extraction fingerprint — which also closes the StorageRef
hash-the-key bug in the same change.

If the fingerprint cannot be made exact in one pass, Codex's removal is the right
fallback. Decide by attempting the fix first; it is a day of work.

### 4.2 Community — Codex's third option is better than mine

I recommended level-0-only now and deferring the ownership split as ADR-scale
(~500 production LOC, storage-key contract change, pre-v1 state wipe).

Codex offers a middle path I did not consider: **level-0-only now, and disable LLM
enhancement until the ownership split lands.** That is cheaper than the split and
strictly safer than the status quo — it removes the clobber, the resurrection bug,
the unbounded LLM spend, and gh#607/#608 in one config-shaped move, without
committing to the ADR before the keep/redesign decision is made.

**Adopt Codex's version.** Given ADR-061 already established community is post-hoc
decoration on the primary search path, spending on enhancement before that decision
is made is hard to justify.

### 4.3 Scope of the "delete BM25" option

Codex lists "stateful query-mutating BM25" under Delete-instead-of-build. Worth
separating: making `GenerateQuery` read-only is a **two-line** change that stops
queries polluting the corpus immediately, independent of the larger
lexical-vs-stateless decision. Do the two-liner now; take the contract decision on
its own timeline.

---

## 5. Merged program

Codex's epic structure is sound. Three amendments:

**Track 0 — jumps every queue (days, one-to-three-line fixes)**

1. `entity_watcher.go:97` — delete the create path (§3.1)
2. `message_logger_http.go:430` — `GetKeyValueBucket`, return the existing 404
3. Dedup key → fold in embedder type + model + cap (§4.1)
4. The two phantom e2e metrics (§3.3)
5. `WithWorkers(max(1, …))` or an explicit `workers` field
6. Tombstone branch → `DeleteEmbedding`
7. Sort before truncating at `graphrag.go:1176`
8. `GenerateQuery` read-only (§4.3)

**Epic A (evidence cannot silently expire)** — adopt as written; add §2.1's exact
swallow site as the hydration-outcome fix location, and §3.4's readiness mechanism.

**Epic B (one community truth)** — adopt with §4.2 (disable enhancement until the
split) and add §3.6 (resurrection) and §3.5 (semantic-free communities).

**Epic C (derived-state ownership)** — adopt; §2.3's ADR contradiction is the first
deliverable, and §2.4's `ALIAS_INDEX` gap plus §3.7's `ENTITY_SUFFIX_INDEX`
omission belong in the ledger.

**Epic D (consumer-path release gates)** — **prerequisite added**: automate one e2e
tier in CI before building the matrix (§3.2), and treat §3.3's inoperative stages as
"never ran" rather than "too lenient."

**Epic E (SemSource clean cut)** — adopt as written (§2.2).

**Track Z — deletion PR**: ~400–500 LOC across `EMBEDDINGS_CACHE` + `NATSCache` +
`Cache` iface + dead `http_embedder` branches + `SetPending` + `max_hop_distance` +
`min_community_size` + `batch_size` ×2 + `graph/query.Config` TTL fields +
`HTTPEmbedder.dimensions` + (pending sister check) `InferRelationshipsFromCommunities`.

---

## 6. What the two audits agree the sweep was missing

Both reports arrive at the same meta-conclusion by different routes. Codex: "state
that looks healthy while its meaning has already diverged." Claude: thirteen
phantom signals — metrics, guards, and knobs that read as load-bearing and are
wired to nothing.

Stated jointly: **the subsystems were audited; the instruments were not.**
Test-to-prod ratios are healthy (1.2–2.1), unit tests are genuinely adversarial,
and the graph core still shipped this defect class — because the apparatus that
was supposed to report problems (metrics, boot guards, readiness flags, config
surfaces, e2e gates) was itself never verified against a consumer.

The cheap durable check, and the one that would have caught nearly everything here
years earlier: **when a signal is load-bearing, grep for its consumer.**
