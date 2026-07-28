# ADR-087: Community Summary Store Ownership (Content-Addressed, Worker-Exclusive)

## Status

Accepted — 2026-07-27. Decision record for the `community-summary-ownership-split`
change (Epic B / B3). Sits alongside ADR-085 (write-then-prune keeps
`COMMUNITY_INDEX` never-transiently-empty) and ADR-068 (the live graph never uses
TTL/MaxBytes/MaxAge for lifecycle eviction), both of which this decision respects.
Mechanics live in the `graph-clustering` and `graph-query` capability specs; this
ADR records only the ownership decision and its irreversible trade.

## Context

The LLM community-summary enhancement worker corrupted the community partition. It
read a `Community` from a `COMMUNITY_INDEX` KV-watch snapshot, spent 5–300s in
`SummarizeCommunity`, then blind-`Put` the snapshot back into the shared
`COMMUNITY_INDEX` keyspace with no revision CAS. One shared write key, two failure
modes:

- **Lost update (#607):** the worker wrote a STALE membership over the detector's
  fresher partition and re-pointed `entity.{level}.{id}` mappings to superseded
  communities. Because a community ID is its seed entity ID, stale IDs survived
  `Prune`'s keep-set and the corrupted record persisted.
- **Zombie resurrection (#617):** a lagging worker wrote a snapshot for a community
  `Prune` had already deleted, resurrecting it; graph-query's community cache
  (watching the same bucket) then served it. The backlog was chronic — detection is
  interval-driven (30s), enhancement was blocking KV-watch-driven — so the
  stale-snapshot window widened without bound.

This was live, not dormant: `enable_llm: true` in every `configs/semantic*.json`
on `main` (the planned B1 "disable LLM" interim never landed; B1's determinism goal
was met via #658 instead), so the vulnerable worker ran in every semantic-tier
deployment with a reachable summary endpoint.

The root cause is a shared write key across two owners with different currency: the
detector owns the partition and rebuilds it on an interval; the worker produces a
slow, best-effort enrichment. Coordinating them with a revision CAS, a
membership-similarity (Jaccard) transfer, and an archive step was the machinery
that had accreted — and it was still racy. The correct fix removes the shared key,
not the race around it.

## Decision

1. **LLM summaries live in a separate, worker-exclusive `COMMUNITY_SUMMARIES` KV
   bucket, keyed by `{level}.{membership_hash}`** — content-addressed, where
   `membership_hash` is `clustering.MembershipHash(members)` (sha256 over the
   lexically-sorted, newline-joined member IDs, hex). The worker is the SOLE writer
   of this bucket.

2. **`COMMUNITY_INDEX` becomes detector-exclusive** (partition, keywords, and the
   statistical summary). The enhancement worker stops writing it entirely; it
   watches it ONLY as a trigger. The worker holds no `CommunityStorage` handle, so
   it cannot write the partition bucket — the single-writer invariant is structural,
   not merely conventional.

3. **The write is correct-for-that-membership regardless of currency.** Because the
   key is derived from the content the worker summarized (the sorted member set), a
   summary written for a membership is correct for that membership whether or not
   the membership is still current. A lagging or slow worker therefore CANNOT
   overwrite a fresher partition (there is no shared key) and CANNOT resurrect a
   `Prune`-deleted community (the read path joins by the *current* community's
   membership hash, so an orphaned summary is served only when a current community
   has that exact member set). This removes the need for a revision CAS, a Jaccard
   transfer, and the archive step — all deleted. A same-membership double-write by
   two workers is idempotent, not an error (wasteful, not incorrect); no `kv.Create`
   claim is added, because that would reintroduce the coordination this design
   exists to delete.

4. **The membership hash has ONE shared definition** (`clustering.MembershipHash`).
   The worker, the graph-query read-join, and the B0 thematic eval all derive the
   hash through that one helper so the definition cannot drift into two subtly
   different hashes that never join.

5. **Readiness is gated on `COMMUNITY_INDEX` only.** The graph-query community cache
   watches BOTH buckets, but a summary miss is a graceful statistical fallback, not
   an unready state. Coupling readiness to the summary bucket would reintroduce the
   LLM-pipeline dependency the split removes; an empty `COMMUNITY_SUMMARIES`
   completes its (empty) initial sync immediately and never blocks GraphRAG
   availability. The read path resolves each community's summary through a single
   helper that joins the summary store and falls back to the community's statistical
   summary — the tiered floor lives in one place and a summary-less partition
   degrades to a non-empty statistical answer, never an empty one.

6. **The store is ADR-068 compliant** — bare `KeyValueConfig{Bucket, Description}`,
   no TTL/MaxBytes/MaxAge, History 1. It is regenerable derived data whose keys are
   content-addressed, so it never carries reachability-blind eviction.

### The staleness trade (decision, not emergent)

This design accepts one irreversible trade, recorded here rather than left to be
discovered:

> membership change is the SOLE refresh trigger. A member-set that stays constant
> while a member's *content* drifts keeps its cached prose — accepted, and
> materially softened by #702: the fresh per-entity digests (labels+tags) ride
> `.Entities` from live ENTITY_STATES reads; only the LLM narrative rides the
> hash-keyed cache.

In other words: a community whose membership is unchanged but whose members' text
content has drifted keeps its previously generated LLM narrative until the
membership itself changes. This is deliberate. The alternative — invalidating the
cache on any member content change — reintroduces per-content coordination and
unbounded re-summarization churn, which is exactly the cost the content-addressed
key exists to eliminate. #702 softens the trade because the query-relevant,
tag-enriched representative context (`CommunitySummary.Entities`) is read fresh
from ENTITY_STATES on every query; only the LLM prose narrative
(`CommunitySummary.Summary`) rides the hash-keyed cache. B3 owns `.Summary`; #702
owns `.Entities`; the two are disjoint struct fields and compose.

## Consequences

- **#607 and #617 are structurally closed**, not mitigated: there is no shared
  write key to race on and no path by which the worker can mutate the partition.
- **LLM cost drops on a steady graph.** A trigger for an unchanged membership is a
  microsecond cache hit that performs no LLM call. Only a NEW distinct membership
  costs a call — which is precisely the #617 unbounded-backlog math eliminated. This
  also plausibly makes #661 (idempotent `COMMUNITY_INDEX` writes) unnecessary:
  spurious churn becomes a cache hit, strictly better than idempotent writes would
  achieve. #661 is reframed to "re-measure necessity after B3," not built.
- **The store accumulates one entry per distinct membership ever seen and never
  prunes in this increment.** This is a reuse cache (a recurring membership is a
  free hit; ~1KB/summary, 10k ≈ 10MB), NOT a leak. B3 ships with a
  `community_summaries_size` gauge so "does it matter" is a metric read, and files a
  worker-owned bounded-GC follow-up (the #703 shape). GC must stay worker-owned to
  preserve the single-writer invariant — the detector must never prune this bucket.
- **BREAKING.** New bucket, changed write topology. Both `cmd/semstreams` and
  `cmd/e2e-semstreams` register the graph-clustering (writer) and graph-query
  (reader) components via the shared `componentregistry.Register`, so both binaries
  open the bucket. Per the clean-beta policy, `task e2e:semantic` must be green
  before the breaking commit lands.
- **The phantom `Inc/DecQueueDepth` gauge is removed.** It bracketed
  already-dequeued work; content-addressing makes backlog benign, so there is no
  real queue to gauge. The three summary counters (`_cache_hits_total`,
  `_generated_total`, `_failed_total`) and the size gauge replace it.
