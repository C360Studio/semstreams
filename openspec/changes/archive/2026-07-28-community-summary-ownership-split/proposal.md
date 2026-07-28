## Why

The LLM community-summary enhancement worker corrupts the community partition. It reads a
`Community` from a `COMMUNITY_INDEX` KV-watch snapshot, spends 5–300s in `SummarizeCommunity`,
then blind-`Put`s the snapshot back into the shared `COMMUNITY_INDEX` keyspace with no revision
CAS (`graph/clustering/enhancement_worker.go:359`, `storage.go:104-114`). Two failure modes from
one cause:

- **Lost update (#607):** the worker writes a STALE membership over the detector's fresher
  partition and re-points `entity.{level}.{id}` mappings to superseded communities. Community ID =
  seed entity ID, so stale IDs survive `Prune`'s keep-set → the corrupted record persists.
- **Zombie resurrection (#617):** a lagging worker writes a snapshot for a community `Prune`
  already deleted, resurrecting it; graph-query's community cache (watching the same bucket) then
  serves it. The backlog is chronic — detection is interval-driven (30s), enhancement is
  blocking KV-watch-driven — so the stale-snapshot window widens without bound.

This is **live, not dormant**: `enable_llm: true` in `configs/semantic*.json` on `main` (the
planned B1 "disable LLM" interim never landed — B1's determinism goal was met via #658's
all-levels-deterministic LPA instead). So the vulnerable worker runs in every semantic-tier
deployment with a reachable summary endpoint, including `task e2e:semantic`.

## What Changes

- **New `COMMUNITY_SUMMARIES` KV bucket, worker-owned, keyed `{level}.{membership_hash}`** —
  `membership_hash` = sha256 over the sorted member list (the *exact* hash the B0 eval already
  computes, lifted to one shared `clustering.MembershipHash` helper). Content-addressing makes the
  write **correct-for-that-membership** regardless of currency, so it structurally **cannot clobber
  the partition or resurrect a community** — no CAS, no Jaccard transfer, no archive dance.
- **`COMMUNITY_INDEX` becomes detector-exclusive** (partition + keywords + statistical summary).
  The enhancement worker stops writing it entirely; it watches it only as a trigger.
- **Worker rewrite:** compute the membership hash, read `COMMUNITY_SUMMARIES`; on an
  `llm-enhanced` hit **skip the LLM call** (fixes #607's "re-enhances every cycle / summaries save
  no LLM cost"); on an `llm-failed` hit retry only after a backoff; on a miss summarize and write
  the summary record. Delete `transferSummary`/`jaccardIndex`/archive+transfer (`lpa.go:182-196,
  259-291, 809-876`). Drop the phantom `IncQueueDepth`/`DecQueueDepth` gauge (#617); add
  `summary_cache_hits_total`, `summary_generated_total`, `summary_failed_total`, and a
  `COMMUNITY_SUMMARIES` **bucket-size gauge** (see add-3 below).
- **graph-query summary-read join:** after the split `Community.LLMSummary` is always empty, so the
  five read sites resolve `CommunitySummary.Summary` through a single `resolveCommunitySummary`
  helper that joins `COMMUNITY_SUMMARIES` by membership hash and **falls back to the statistical
  summary** (tiered floor, in one place, never empty). The community cache adds a **second watcher**
  on `COMMUNITY_SUMMARIES`; readiness stays gated on `COMMUNITY_INDEX` **only** (deliberately
  decoupling GraphRAG availability from the LLM pipeline).
- **ADR-087** records the summary-store ownership decision, including the explicit **staleness
  trade** (add-1): *membership change is the sole refresh trigger; entity-content drift in the prose
  summary is accepted — materially softened because #702's fresh per-entity digests (labels+tags)
  ride in `CommunitySummary.Entities` from live ENTITY_STATES reads while only the LLM prose rides
  the hash-keyed cache.*

**BREAKING** — new bucket, changed write topology, two binaries (`cmd/semstreams`,
`cmd/e2e-semstreams`) must open it. Composes with #702 (thematic-synthesis-context): that change
owns `CommunitySummary.Entities` (ENTITY_STATES-sourced digests + tags); this change owns
`.Summary` (COMMUNITY_SUMMARIES join). Disjoint struct fields — no conflict.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `graph-clustering`: the community-summary ownership split — `COMMUNITY_INDEX` detector-exclusive
  (partition); a new worker-owned, content-addressed `COMMUNITY_SUMMARIES` store; worker skip-on-hit
  + failed-retry-backoff; removal of the CAS-less partition write, transfer/archive/Jaccard, and the
  phantom queue gauge.
- `graph-query`: the community-summary read join — resolve `CommunitySummary.Summary` from
  `COMMUNITY_SUMMARIES` by membership hash with a statistical-summary floor; cache watches both
  buckets; readiness gated on the partition bucket only.

## Impact

- **Code**: `graph/clustering/` (`storage.go` new `MembershipHash` + summary CRUD, `enhancement_worker.go`
  rewrite, `lpa.go` delete transfer/archive/Jaccard), `graph/constants.go` (new bucket constant + list),
  `processor/graph-clustering/component.go` (open + wire `COMMUNITY_SUMMARIES`, worker config),
  `processor/graph-query/community_cache.go` (second watcher + `SummaryFor`), `processor/graph-query/graphrag.go`
  (5 read sites → `resolveCommunitySummary`), `test/e2e/scenarios/validate_thematic_eval.go` (call the shared hash).
- **Data**: new `COMMUNITY_SUMMARIES` bucket (ADR-068 compliant — no TTL/MaxBytes/MaxAge; regenerable
  derived data). `COMMUNITY_INDEX` records no longer carry `LLMSummary`.
- **Consumers**: semsource `global_search` / GraphRAG (reads flow through the join; no caller change).
- **Docs**: ADR-087.
- **Gate**: BREAKING → `task e2e:semantic` green before the breaking commit lands; grep both `cmd/*/main.go`
  for the new bucket wiring (beta.18 half-migration lesson).

## Non-goals

- **No CAS / revision-guard on the summary write.** Content-addressing removes the need; adding a
  `kv.Create` claim would reintroduce the coordination the design exists to delete. The rare
  same-membership double-write is idempotent (wasteful, not incorrect).
- **No summaries GC in this change (add-3).** The content-addressed store is a *reuse cache* —
  orphaned summaries are hits when a membership recurs (~1KB each). Ship with **a bucket-size gauge**
  so "does it matter" is a read, and **file a worker-owned bounded-GC follow-up in this change's PR**
  (the #703 shape: persistent entries need a stated decommission path, even if deferred).
- **#661 is NOT bundled, and is reframed (add-2).** B3 plausibly makes #661 *unnecessary*: spurious
  `COMMUNITY_INDEX` churn becomes a microsecond cache hit — strictly better than idempotent writes
  would achieve. So #661 becomes **"re-measure necessity after B3 lands"** (measure-before-building);
  do not build idempotency machinery for a cost that may no longer exist.
- **No semantic-partition work.** B2 closed that on an honest negative; this is summary-store
  ownership only.
- **No re-enabling flag flip.** `enable_llm` is already `true`; this change makes the already-on path
  safe, it does not turn LLM on.
