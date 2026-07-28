# Design — community-summary-ownership-split (Epic B / B3)

## Ground truth (verified at `main` — re-verify at implementation kickoff)

- `SaveCommunity` is a bare `kv.Put` with no CAS; also re-Puts every `entity.{level}.{id}` mapping —
  `graph/clustering/storage.go:104` + `:109-114`.
- Four blind writers of `COMMUNITY_INDEX`: detector persist `lpa.go:410`, summary transfer `lpa.go:268`,
  worker success `enhancement_worker.go:359`, worker failure `enhancement_worker.go:387`.
- Worker reads a watch snapshot and blocks 5–300s: `enhancement_worker.go:304` → `:340`
  (`SummarizeCommunity`) → `:359`. Trigger channel is a blocking buffered `WatchAll` (`:216`).
- `IncQueueDepth`/`DecQueueDepth` bracket already-dequeued work (`:325-326`) → phantom gauge (#617).
- **`enable_llm: true`** in `configs/semantic.json:599`, `semantic-8b.json:638`, `semantic-frontier.json:647`;
  detector is `WithLevels(3)` (`component.go:1233`). The B1 "disable LLM / WithLevels(1)" interim never
  landed — #658 delivered determinism instead. **The change author MUST re-run `grep enable_llm configs/`
  + confirm git HEAD before trusting this.**

## 1. `COMMUNITY_SUMMARIES` store + key

- New constant `BucketCommunitySummaries = "COMMUNITY_SUMMARIES"` in `graph/constants.go:33` and its bucket
  list `:48-61` (wipe/reseed runbooks).
- Key `{level}.{membership_hash}`. `membership_hash` via a new shared exported helper
  `clustering.MembershipHash(members []string) string` (sha256 over sorted, `\n`-joined member IDs, hex) —
  **byte-identical to `test/e2e/scenarios/validate_thematic_eval.go:520-535`**. Refactor `level0MembershipHashes`
  to call it; add a parity unit test. Hash carries no level (summarization has no level input); level lives
  only in the key prefix, parsed first-dot like `community_cache.go:187`.
- Record `CommunitySummaryRecord{ MembershipHash, Level, LLMSummary, Model, Status ("llm-enhanced"|"llm-failed"),
  Truncated, MemberCount, GeneratedAt }`. NO full member snapshot (the hash is the identity; storing members
  reintroduces a divergence surface). Keywords stay detector-owned in `COMMUNITY_INDEX`.
- Bucket config: bare `KeyValueConfig{Bucket, Description}` — **no TTL/MaxBytes/MaxAge** (ADR-068), History
  default 1 (regenerable derived data).

## 2. Enhancement-worker rewrite (`enhancement_worker.go:296-390`)

`COMMUNITY_INDEX` `WatchAll` stays the trigger only:
1. Parse `Community` (`:303`); drop the `SummaryStatus != "statistical"` gate (`:310`).
2. `hash := clustering.MembershipHash(community.Members)`.
3. Read `COMMUNITY_SUMMARIES[{level}.{hash}]`: **hit+`llm-enhanced`** → skip (`summary_cache_hits_total`);
   **hit+`llm-failed`** → retry only if `now-GeneratedAt > backoff`; **miss** → `fetchEntities` +
   `SummarizeCommunity` + `Put COMMUNITY_SUMMARIES[{level}.{hash}]`.
4. Worker NEVER writes `COMMUNITY_INDEX`. `markFailed` writes an `llm-failed` record to the summary store.

Delete `transferSummary`/`jaccardIndex` (`lpa.go:809-876`), `SummaryTransferThreshold` (`lpa.go:30`), and
Phase-1 archive / Phase-2 transfer (`lpa.go:182-196, 259-282`). Metrics: drop `Inc/DecQueueDepth`; add
`summary_cache_hits_total`, `summary_generated_total` (= misses that did LLM work; fresh-work =
`generated - hits`), `summary_failed_total`, keep the latency histogram, add the **bucket-size gauge**
(add-3). No real queue gauge — content-addressing makes backlog benign (steady graph → all hits → skip in µs;
only a NEW distinct membership costs a call, which is the #617 unbounded-backlog math eliminated).

## 3. graph-query read-path join (the #702 coupling)

After the split `Community.LLMSummary` (from `COMMUNITY_INDEX`) is always empty, so without a join Tier-2 prose
never reaches synthesis. Five read sites today: `graphrag.go:298-300, 1276-1278, 1518-1520, 2228-2230` +
`scoreCommunitySummaries:2213-2230`.

- `community_cache.go`: add a **second watcher** on `COMMUNITY_SUMMARIES` (mirror `WatchAndSync:54`); keep a
  parallel `summaries` map keyed `{level}.{hash}` with `handleUpdate`/`handleDelete`. Add
  `SummaryFor(comm) (string, bool)` = lookup by `MembershipHash(comm.Members)`, ok iff status `llm-enhanced` and
  non-empty.
- One helper `resolveCommunitySummary(comm) string` = `SummaryFor` else `comm.StatisticalSummary`, applied at
  all five sites (thread it as a `summaryOf func(*clustering.Community) string` into `scoreCommunitySummaries`).
  Tiered floor lives in ONE place; a summary-less partition → statistical, never empty.
- **Readiness gated on `COMMUNITY_INDEX` only** (`community_cache.go:70-79`): a summary miss is graceful
  fallback; coupling readiness to the summary bucket would reintroduce the LLM-pipeline dependency the split
  removes. An empty `COMMUNITY_SUMMARIES` completes its (empty) initial sync immediately.
- **#702 composition — disjoint fields.** #702 enriches `.Entities` (rep digests + tags) via
  `enrichCommunitySummaries`/`loadDigestEntities` reading **ENTITY_STATES** (`graphrag.go:1812-1863`), unchanged.
  B3 changes only `.Summary`. Same struct, same `topCommunities` list, different fields → compose; rep/tag path
  needs no B3 edit.

## 4. Re-enable = "make the already-on path safe" (§0)

No flag to flip. Leave `enable_llm: true` in the three semantic configs, `statistical.json` false. Wiring
unchanged and correct: `component.go:991-996` (`startEnhancementWorker` under `!poisoned && EnableLLM`), worker
count `:2212/:397`, `LLMTimeout` `:2172,:2204` (60s default `:48`). `startEnhancementWorker` must ALSO
create/open `COMMUNITY_SUMMARIES` and pass its handle in `EnhancementWorkerConfig{SummaryBucket}`; graph-query
opens the same bucket for the cache's second watcher (mirror `component.go:479-512`). **Both `cmd/semstreams/main.go`
AND `cmd/e2e-semstreams/main.go` must open the new bucket** (beta.18 half-migration). BREAKING → `task e2e:semantic`
green before the breaking commit.

## 5. Adjacencies (confirmed) + the three owner adds

- **Adjacency A:** `COMMUNITY_SUMMARIES` is plain component-owned KV — NOT `projection`/`ownership`-governed.
  graph-clustering authors no ENTITY_STATES facts on the community path (grep: only a dormant, unwired
  `InferRelationshipsFromCommunities` comment `lpa.go:648` and the separate anomaly subsystem). Bucket-ownership
  rubric: derived/regenerable, non-Graphable, query-side cache → private bucket, no `projection.Bind`.
- **Adjacency B:** `graph-index-replacement-semantics` (active) touches the graph-query spec but its delta is
  index-lookup/traversal only — zero community/summary overlap. Expect a **mechanical merge** on
  `openspec/specs/graph-query/spec.md` (both add `### Requirement:` blocks); land whichever archives second on top.

**Add-1 (staleness trade — stated in ADR-087, not emergent):** membership change is the SOLE refresh trigger.
A member-set that stays constant while a member's *content* drifts keeps its cached prose — accepted, and
materially softened by #702: the fresh per-entity digests (labels+tags) ride `.Entities` from live ENTITY_STATES
reads; only the LLM narrative rides the hash-keyed cache.

**Add-2 (#661 reframe):** do NOT bundle, and do NOT build yet. B3 likely makes #661 unnecessary — spurious
`COMMUNITY_INDEX` churn becomes a µs cache hit, strictly better than idempotent writes. Reframe #661 to
"re-measure necessity after B3 lands" (measure-before-building); note on the issue + this change.

**Add-3 (GC-none files its follow-up at ship time):** content-addressed keys accumulate one entry per distinct
membership ever seen and never prune — this is the reuse cache (a recurring membership is a free hit; ~1KB/summary,
10k ≈ 10MB), NOT a leak. Ship B3 with **no GC** but WITH the bucket-size gauge (§2), and **file a worker-owned
bounded-GC issue in this change's PR** (the #703 shape: persistent entries owe a stated decommission path; GC must
stay worker-owned to preserve single-writer on the bucket — the detector must never prune it).

## 6. Ceremony

OpenSpec change (this) + **ADR-087** (summary-store ownership: content-addressed worker-exclusive store; partition
detector-exclusive; readiness partition-gated; the add-1 staleness trade). ADR drafted `Proposed` → `Accepted` in
this PR. Mechanics live in the two capability specs, not the ADR. Route implementation to `semstreams-developer`,
`semstreams-reviewer` pre-merge, `task e2e:semantic` green before the BREAKING commit.
