# Inventory — owner-load-gate-instrument (#1284)

base: 29187077e202784fcd51071f1e467361c8dc958d

Enumeration only, no judgment. Every source claim is a verified pin; every CI claim names its run id.
Analysis and unresolved questions are kept out of the pinned sections, in "Adjacent claims" below.

## Searches

- `gh run list --workflow=ci.yml --limit 400` filtered to 2026-08-26T18:30Z..2026-09-09T22:14Z (denominator)
- `gh run list --workflow=ci.yml --status=failure --limit 40` (the complete failure set for that window)
- `gh run view <id> --log-failed | grep OwnerFilterLoadHarness` over all 40 failures
- `gh run view 34367949188 --log` (full phase=latency output of the run #1284 cites)
- `grep -nE "second|budget|3s" docs/adr/065-predicate-index-composite-key-sharding.md`
- `grep -nE "3 second|10 second|budget|Decision profile" docs/operations/32-predicate-layout-smoke-harness.md`
- `grep -rn "func.*KeysByFilter" natsclient/` and `grep -rnE "Timeout:\s*[0-9]+\s*\*\s*time\." natsclient/*.go`

## 1. The assertion that fires

- `processor/graph-index/owner_filter_load_integration_test.go:473` — `func measureOwnerLoadFilter(`
- `processor/graph-index/owner_filter_load_integration_test.go:484` — `started := time.Now()`
- `processor/graph-index/owner_filter_load_integration_test.go:485` — `keys, err := store.KeysByFilter(ctx, filter)`
- `processor/graph-index/owner_filter_load_integration_test.go:486` — `duration := time.Since(started)`
- `processor/graph-index/owner_filter_load_integration_test.go:487` — `require.NoError(t, err, label)`
- `processor/graph-index/owner_filter_load_integration_test.go:488` — `require.Len(t, keys, want, label)`
- `processor/graph-index/owner_filter_load_integration_test.go:489` — `require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)`
- `processor/graph-index/owner_filter_load_integration_test.go:490` — `durations = append(durations, duration)`
- `processor/graph-index/owner_filter_load_integration_test.go:492` — `assertOwnerLoadLatency(t, label, durations, profile)`
- `processor/graph-index/owner_filter_load_integration_test.go:495` — `func assertOwnerLoadLatency(t *testing.T, label string, durations []time.Duration, profile ownerLoadProfile) {`
- `processor/graph-index/owner_filter_load_integration_test.go:498` — `p95 := durations[(len(durations)-1)*95/100]`
- `processor/graph-index/owner_filter_load_integration_test.go:499` — `p99 := durations[(len(durations)-1)*99/100]`
- `processor/graph-index/owner_filter_load_integration_test.go:502` — `t.Logf("phase=latency filter=%s reps=%d p50=%s p95=%s p99=%s max=%s",`

`require.*` calls `t.FailNow()`, so a failure at `:489` returns before `:492`. The per-repetition durations already
collected are discarded and never logged. A firing therefore cannot be classified as stall vs. regression from its own
output: reps 0..n-1 are unrecoverable. At `repetitions: 5`, `(5-1)*95/100 == (5-1)*99/100 == 3`, so p95 == p99 ==
`durations[3]`, the second-largest of five; neither percentile ever examines the max.

## 2. The two profiles

- `processor/graph-index/owner_filter_load_integration_test.go:56` — `func ownerLoadCIProfile() ownerLoadProfile {`
- `processor/graph-index/owner_filter_load_integration_test.go:59` — `repetitions: 5, churnPerWriter: 50, workerShapes: []int{4},`
- `processor/graph-index/owner_filter_load_integration_test.go:60` — `// operationBudget 3s is a CONTRACTED ACTIVATION GATE, not a tunable test detail. Production`
- `processor/graph-index/owner_filter_load_integration_test.go:68` — `// This per-repetition gate is also the ONLY tail coverage here: at repetitions=5 the`
- `processor/graph-index/owner_filter_load_integration_test.go:73` — `// gh#750 records that this budget flakes under CI runner contention (observed 3.30s against a`
- `processor/graph-index/owner_filter_load_integration_test.go:76` — `operationBudget: 3 * time.Second, p95Budget: 3 * time.Second, p99Budget: 3 * time.Second,`
- `processor/graph-index/owner_filter_load_integration_test.go:81` — `func ownerLoadFullProfile() ownerLoadProfile {`
- `processor/graph-index/owner_filter_load_integration_test.go:84` — `repetitions: 30, churnPerWriter: 200, workerShapes: []int{4, maxGraphIndexWorkers},`
- `processor/graph-index/owner_filter_load_integration_test.go:85` — `operationBudget: 10 * time.Second, p95Budget: 3 * time.Second, p99Budget: 5 * time.Second,`

The two profiles differ in kind, not only in magnitude. The full profile separates an absolute per-operation ceiling
(10s, the handler bound) from the latency contract (p95 3s / p99 5s). The CI profile collapses all three onto one
number, so a single stalled repetition trips what is structurally the absolute ceiling at the value of the typical
contract.

## 3. What the measured operation actually does

- `natsclient/kv.go:537` — `func (kv *KVStore) KeysByFilter(ctx context.Context, pattern string) ([]string, error) {`
- `natsclient/kv.go:538` — `ctx, cancel := kv.applyTimeout(ctx)`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:68` — `func (kv *KVStore) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {`
- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `natsclient/kv.go:54` — `func (c *Client) NewKVStore(bucket jetstream.KeyValue, opts ...func(*KVOptions)) *KVStore {`
- `natsclient/kv.go:55` — `options := DefaultKVOptions()`
- `processor/graph-index/owner_filter_load_integration_test.go:168` — `stores[bucketName] = nc.NewKVStore(raw)`

`NewKVStore` starts from `DefaultKVOptions()`, whose `Timeout` is 5s, and the harness at `:168` passes no option
override — so every measured call is bounded at 5s, and `KeysByFilter` can never return later than that.

The 3s test gate therefore sits below a 5s hard bound the framework itself enforces. The band 3s-5s is a zone where the
production path still succeeds and the test fails. All three observed budget firings land inside it.

The harness independently asserts that the temporary consumers created by each call return to baseline:

- `processor/graph-index/owner_filter_load_integration_test.go:414` — `require.Equal(t, baselines, afterConsumers, "temporary consumers must return to every per-store baseline")`
- `processor/graph-index/owner_filter_load_integration_test.go:448` — `require.LessOrEqual(t, phaseAfter.Subscriptions, phaseBefore.Subscriptions+2,`
- `processor/graph-index/owner_filter_load_integration_test.go:450` — `require.Zero(t, phaseAfter.SlowConsumers, "load gate must not create slow consumers")`

## 4. The fixtures, and whether incoming-forward is exercised

- `processor/graph-index/owner_filter_load_integration_test.go:184` — `{name: "incoming", bucket: incomingBucket, store: stores[incomingBucket],`
- `processor/graph-index/owner_filter_load_integration_test.go:186` — `wantForward: profile.entities, stream: streams[incomingBucket]},`
- `processor/graph-index/owner_filter_load_integration_test.go:274` — `measureOwnerLoadFilter(t, ctx, fixture.store, fixture.name+"-owner", fixture.ownerFilter,`
- `processor/graph-index/owner_filter_load_integration_test.go:278` — `profile, fixture.wantForward)`

All three fixtures define a non-empty `forwardFilter`, and `:274`/`:278` measure `<name>-owner` and `<name>-forward`
for each, so the CI profile runs six measurements per worker shape. #1284's premise that `incoming-forward` "does not
appear in the profile definitions" is correct only in that the label is composed at `:274`/`:278` rather than written
literally; the measurement does run.

## 5. The contract surfaces that pin 3s

- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:139` — `4. the 5,000-hot-member plus 20-predicate CI guard, with each operation below 3 seconds;`
- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:140` — `5. one 21,000-entity sustained-churn run at the configured worker shape and one stress shape, with p95 at most`
- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:141` — `3 seconds, p99 at most 5 seconds, no operation reaching the 10-second handler bound, bounded queue growth, and`
- `openspec/specs/graph-index/spec.md:184` — `Performance MUST be gated by absolute budgets, not comparison: the ADR-065 CI guard (5,000 hot members, each`
- `openspec/specs/graph-index/spec.md:185` — `operation under 3 seconds) and one sustained-churn run on the 21,000-entity profile at the configured worker shape`
- `openspec/specs/graph-index/spec.md:186` — `and one stress shape, achieving p95 at most 3 seconds, p99 at most 5 seconds, no operation at the 10-second`
- `docs/operations/32-predicate-layout-smoke-harness.md:70` — `| CI | 5,000 | 20 | 2 writers x 100 | 5 | every operation <3s; p95/p99 <=3s |`
- `docs/operations/32-predicate-layout-smoke-harness.md:71` — `| Decision | 21,000 | 20 | 4 writers x 500 | 30 | every operation <10s; p95 <=3s; p99 <=5s |`
- `docs/adr/065-predicate-index-composite-key-sharding.md:46` — `predicate holding 5,000 — the shape that triggers GH #430) seeded in`
- `docs/adr/065-predicate-index-composite-key-sharding.md:49` — `of magnitude inside the handler's 10s timeout, confirming the "2 round`

## Adjacent claims

Analysis and unresolved questions. Not pins.

- #1284 — the issue this change answers; #750 — closed as COMPLETED 2026-07-30 having shipped only the comment at `:73`.
- **Citation defect A.** `openspec/specs/graph-index/spec.md:184` attributes the 3s CI guard to ADR-065. ADR-065
  contains no 3-second budget; its stated absolute bound for this operation class is the 10s handler timeout
  (`docs/adr/065-...:49`). The 3s figure is ADR-077 condition 4's tightening.
- **Citation defect B.** The contract comment at `:60`-`:75` cites `docs/operations/32-predicate-layout-smoke-harness.md:49-50`
  for the 3s/10s profile assignment. Those lines are "Run the CI profile:" and a blank line; the budget table is at
  `:70`-`:71`.
- **Measured firing rate (new — #1284 cites one instance).** Window 2026-08-26T18:30Z..2026-09-09T22:14Z, 15 days:
  314 CI runs, 273 success, 40 failure. 4 of the 40 failures are this harness — 10% of all CI failures, 1.3% of all runs.
- **It is not filter-specific.** Three distinct labels fired across the four: `incoming-forward` (x2),
  `predicate-forward`, `name-forward`.
- **Only `*-forward` filters ever fire.** All four are forward filters, which return 5,000 keys. No `*-owner` firing
  appears in the window; owner filters return 1 key and measure 2-6ms.
- **#1284's magnitude claim overstates by ~50x.** It reads "three orders of magnitude off the same run's own
  distribution" (3.356s / 2.9ms ~ 1150x), which compares a 5,000-key drain against a 1-key owner lookup. Against the
  comparator of the same kind - forward filters, same key count - it is 3.356s / 153ms ~ 22x, or ~11.5x against the
  worst forward max in the window (389ms, run 33208133273). The direction holds; the magnitude does not.
- **UNRESOLVED: what the gh#750 comment's "same-run max of 2.24s" refers to.** No forward filter in this window
  measured anything near it (worst 389ms). Different filter, different server pin, or different era is not determined.
  `docs/operations/32-...:44` warns that pre-pin performance rows are historical. Current pin: NATS
  `2.14.4-alpine@sha256:f2123f...`, SDK `v1.52.0`.
- **UNRESOLVED: whether the 3s-5s band is stall-only or contains a real tail of the 5,000-key drain.** Not decided by
  this data, because section 1's discard means no firing has ever recorded its own reps 0..n-1.

### Observed firings

| # | run | date | branch | assertion | value | label |
|---|-----|------|--------|-----------|-------|-------|
| 1 | 34372644231 | 2026-09-09 | `claude/gh1267-honor-predicate-datatype` | `:489` budget | 3.515s | `predicate-forward` rep 2 |
| 2 | 34367949188 | 2026-09-09 | `claude/gh1261-graph-read-tools` | `:489` budget | 3.356s | `incoming-forward` rep 3 |
| 3 | 33260659637 | 2026-08-29 | `main` | `:489` budget | 4.760s | `incoming-forward` rep 2 |
| 4 | 33208133273 | 2026-08-28 | `claude/gh1095-entity-id-slice-b` | `:487` error | `context deadline exceeded` | `name-forward` |

### Full latency output of run 34367949188

#1284 quotes three of the five lines that completed, omitting `name-forward` and `incoming-owner`.

```
predicate-owner    reps=5 p50=2.901784ms   p95=3.633598ms   p99=3.633598ms   max=3.735528ms
predicate-forward  reps=5 p50=152.981877ms p95=169.852498ms p99=169.852498ms max=189.778227ms
name-owner         reps=5 p50=4.060837ms   p95=4.390145ms   p99=4.390145ms   max=6.320733ms
name-forward       reps=5 p50=256.215943ms p95=258.039091ms p99=258.039091ms max=291.975562ms
incoming-owner     reps=5 p50=4.227452ms   p95=4.273866ms   p99=4.273866ms   max=4.573105ms
incoming-forward   -- never logged; failed at rep 3
```
