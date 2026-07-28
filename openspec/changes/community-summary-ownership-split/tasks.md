## 0. Kickoff re-verification (premise guard)

- [x] 0.1 Confirm ground truth at HEAD before coding: `grep -rn enable_llm configs/` (expect semantic* = true,
  statistical = false), `WithLevels` in `component.go`, and the four `COMMUNITY_INDEX` writers still present.
  If the premise drifted, reconcile the design before proceeding.

## 1. Shared membership hash

- [x] 1.1 Add `clustering.MembershipHash(members []string) string` (sha256 over sorted `\n`-joined members, hex)
  in `graph/clustering/storage.go`.
- [x] 1.2 Refactor `test/e2e/scenarios/validate_thematic_eval.go:level0MembershipHashes` to call it; add a
  parity unit test asserting the two paths produce identical hashes for a fixed member set.

## 2. COMMUNITY_SUMMARIES store

- [x] 2.1 `BucketCommunitySummaries = "COMMUNITY_SUMMARIES"` in `graph/constants.go:33` + its bucket list `:48-61`.
- [x] 2.2 `CommunitySummaryRecord` type (membership_hash, level, llm_summary, model, status, truncated,
  member_count, generated_at). Summary CRUD in storage keyed `{level}.{hash}`; bucket config bare (no
  TTL/MaxBytes — ADR-068), History 1.

## 3. Enhancement-worker rewrite (`enhancement_worker.go:296-390`)

- [x] 3.1 Trigger-only flow: parse community → `MembershipHash` → read summary store → skip on `llm-enhanced`
  hit / backoff-retry on `llm-failed` / summarize+write on miss. Worker never writes `COMMUNITY_INDEX`;
  `markFailed` writes an `llm-failed` record to the summary store.
- [x] 3.2 Wire `EnhancementWorkerConfig.SummaryBucket` (new handle); open it in `startEnhancementWorker`.
- [x] 3.3 Delete `transferSummary`/`jaccardIndex` (`lpa.go:809-876`), `SummaryTransferThreshold` (`lpa.go:30`),
  Phase-1 archive / Phase-2 transfer (`lpa.go:182-196, 259-282`). Confirm the detector persist path
  (`lpa.go:410`) is the sole `COMMUNITY_INDEX` writer after removal.
- [x] 3.4 Metrics: drop `Inc/DecQueueDepth` (phantom); add `summary_cache_hits_total`,
  `summary_generated_total`, `summary_failed_total`, keep latency histogram, add **`community_summaries_size`
  gauge** (add-3). Unit-test skip-on-hit (no LLM call), failed-retry-backoff, and that a stale-snapshot write
  never targets `COMMUNITY_INDEX`.

## 4. graph-query read-path join

- [x] 4.1 `community_cache.go`: second watcher on `COMMUNITY_SUMMARIES`; parallel `summaries` map keyed
  `{level}.{hash}` with update/delete handlers; `SummaryFor(comm) (string, bool)`.
- [x] 4.2 `resolveCommunitySummary(comm)` helper (SummaryFor else StatisticalSummary); apply at the five read
  sites (`graphrag.go:298-300, 1276-1278, 1518-1520, 2228-2230`; thread `summaryOf` into
  `scoreCommunitySummaries:2213`). Readiness stays gated on `COMMUNITY_INDEX` only.
- [x] 4.3 Open `COMMUNITY_SUMMARIES` in the graph-query component (mirror `component.go:479-512`).
- [x] 4.4 Unit/integration: enhanced summary surfaces via the join; miss → statistical floor (non-empty);
  empty summary bucket does not block readiness. Confirm #702's rep/tag digest path (`.Entities`) is unchanged.

## 5. ADR-087

- [x] 5.1 Write `docs/adr/087-community-summary-store-ownership.md` (Accepted): content-addressed
  worker-exclusive summary store; `COMMUNITY_INDEX` detector-exclusive; readiness partition-gated; **the add-1
  staleness trade stated as a decision** (membership = sole refresh trigger; content drift in prose accepted;
  #702 softens). Update `docs/adr/README.md` index.

## 6. Two-binary wiring (BREAKING guard)

- [x] 6.1 `cmd/semstreams/main.go` AND `cmd/e2e-semstreams/main.go` both open `COMMUNITY_SUMMARIES`
  (`grep -rn COMMUNITY_SUMMARIES cmd/` — both present, beta.18 half-migration lesson).

## 7. Gate — local, mirrors CI

- [x] 7.1 `go build ./...`; `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`; `task lint`;
  `go test -race ./...`; `task schema:generate` + `git diff schemas/ specs/` (commit any deltas);
  tagged integration on `graph/clustering`, `processor/graph-clustering`, `processor/graph-query`.

## 8. Review

- [x] 8.1 `semstreams-reviewer` pass — APPROVE, no BLOCKING/HIGH; 2 NITs fixed (stale metrics doc block,
  level-source comment). Verified single-writer (structural), content-addressing closes #607/#617, metrics
  wiring idempotent, cache second-watcher race-clean, deletions complete, startup ordering genuine.
- [ ] 8.2 (owner, out-of-band) Codex; address before merge.

## 9. E2E (BREAKING gate — before the breaking commit lands)

- [x] 9.1 `task e2e:semantic:frontier` GREEN (BREAKING gate). The 1.7b `task e2e:semantic` FAILED on the
  known capacity artifact (enhancement saturates → step-42 `loadEntities` timeout), NOT a B3 defect —
  partition determinism was still 1.00 there. The frontier confirming run (Gemini) is GREEN:
  `validation_errors:0`, known-answer 7/7, determinism 1.00, no orphans, recall 0.95, non-degraded, and
  `llm_enhanced=14 / pending=0` with the pending-wait resolving in 6ms — proving the full worker →
  `COMMUNITY_SUMMARIES` → join → synthesis path end-to-end. Reinforced by the §11 real-NATS wire
  integration test (production trigger path, PASS not skip).

## 10. Land + ship-time follow-ups

- [ ] 10.1 Merge per house gate (CI green, `gh pr checks`, `mergeStateStatus`; Codex addressed).
- [ ] 10.2 **File the worker-owned bounded-GC follow-up issue (add-3)** — decay-window sweep on
  `COMMUNITY_SUMMARIES`, worker-owned to preserve single-writer; gated on the size gauge; reference #703.
- [ ] 10.3 **Reframe #661 (add-2)** — comment: "re-measure necessity after B3; B3's cache-hit skip likely
  makes idempotent `COMMUNITY_INDEX` writes unnecessary — do not build until re-measured."
- [ ] 10.4 `openspec archive community-summary-ownership-split`.

## 11. Enhancement-observability migration (post-e2e finding)

The frontier e2e gate passed but surfaced that the `validate-llm-enhancement` stage still
read enhancement status from the OLD `COMMUNITY_INDEX` location (`comm.SummaryStatus` /
`comm.LLMSummary`), which the worker no longer writes after the split — so it was blind to
the new `COMMUNITY_SUMMARIES` store and reported `enhanced=0` while always burning its full
pending-wait ceiling. Test-harness only; no production code changed.

- [x] 11.1 Integration test proving the production wire end-to-end (real NATS, mock LLM):
  `processor/graph-query/community_summary_wire_integration_test.go` drives the real
  COMMUNITY_INDEX watch trigger → worker enhances → `llm-enhanced` `CommunitySummaryRecord`
  lands in the real `COMMUNITY_SUMMARIES` bucket at `{level}.{hash}` → graph-query's cache
  (via its real `WatchSummaries` over NATS) surfaces it through `SummaryFor` /
  `resolveCommunitySummary` (not the statistical floor). Closes the reviewer's MEDIUM
  (WatchSummaries-over-real-NATS seam). **PASSES** — the e2e `enhanced=0` was a measurement
  gap, not a worker/join defect.
- [x] 11.2 Migrated the blind e2e stage to the new store: `validate-llm-enhancement` and its
  reporting/comparison/`validateLLMSummaryQuality` now JOIN each community to
  `COMMUNITY_SUMMARIES` on `clustering.MembershipHash(comm.Members)`; the 300s pending-wait
  resolves on store enhanced records (new `client.GetCommunitySummaries` +
  `WaitForCommunitySummaryEnhancement`, replacing the dead COMMUNITY_INDEX poll). Confirmed
  `tiered_statistical.go:420` stays correct (detector still writes `SummaryStatus:statistical`
  on COMMUNITY_INDEX; `enable_llm:false` → 0 enhanced, unchanged, no mis-read).
