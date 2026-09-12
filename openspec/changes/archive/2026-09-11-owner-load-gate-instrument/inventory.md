# Inventory — owner-load-gate-instrument (#1284)

> **Post-archive corrections (2026-09-12).** This is a design-time record and two classes of figure in it were
> corrected after it was archived. **(1)** Every cross-kind latency comparison — a ratio between two measurements
> that do not do the same work — is repudiated; the like-for-like figures are tabulated in `design.md` § 11.11.
> **(2)** The `60c79736` supervised record cited throughout predates the submission-order instrument and is
> superseded by `b10671ed`, whose worst p95/p99 are 580.383 ms / 591.050 ms, making the full profile's 3s/5s
> **5.2x/8.5x** rather than the 9.6x/15.6x recorded here (`design.md` § 11.12). Current truth for both lives in
> `docs/operations/32-predicate-layout-smoke-harness.md`, § "Owner-filter acceptance record". No ruling changes.


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

- `git grep -nF <pin text> -- <path>` uniqueness check for every pin added in sections 6-7 (each returned exactly 1 hit)
- `grep -rn "NewKVStore(" processor/graph-index/*.go` (how production graph-index builds its KV stores)
- `gh run view 34475316237 --log | grep -E "phase=latency|ok .*graph-index"` (a GREEN main run: no `phase=` line is printed)
- `for d in /Users/coby/Code/c360/*/; do git -C "$d" grep -rn -iE "adr-?077|OwnerFilterLoad|owner.?filter.*(budget|3s)"; done` (read-only sister sweep, 27 checkouts)
- `git log --all --oneline --diff-filter=A -- 'docs/adr/107*'` (is the next ADR number free)
- `git grep -ln "operationBudget"` -> 3 code files; the third is `processor/graph-index/predicate_layout_smoke_integration_test.go`
- `gh issue view 750 --json body` (the original distribution the 3s budget was argued against)
- `gh run list --workflow=ci.yml --status=failure --created '>=2026-08-26' --limit 60` -> 42 failures, every log fetched, 0 fetch errors
- `gh run list --workflow=ci.yml --status=failure --limit 40` -> a DIFFERENT 40-run slice spanning 2026-05-30..2026-08-26 (38 fetchable, 2 HTTP 410 expired); the bare `--limit` form is not "the most recent failures"
- `gh run view <id> --log-failed | grep -c "PredicateLayoutSmoke"` and `| grep -c "OwnerFilterLoadHarness"` over all 80 fetched failures, stderr captured to a file rather than discarded
- `sed -n '470,486p' processor/graph-index/predicate_layout_smoke_integration_test.go` (the sibling's assertion ORDER, which voids the A/B)
- `grep -n -i "milliseconds" docs/operations/32-predicate-layout-smoke-harness.md` -> `:133`, the unit statement over the owner-filter latency tables
- `gh api repos/C360Studio/semstreams/issues/comments/5635299542` (the owner ruling, read verbatim)

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

## 6. The measurement environment

- `.github/workflows/ci.yml:132` — `timeout-minutes: 25`
- `.github/workflows/ci.yml:144` — `run: scripts/run-integration-tests.sh`
- `scripts/run-integration-tests.sh:304` — `uncapped package parallelism`
- `scripts/run-integration-tests.sh:311` — `go test -race -failfast -tags=integration -timeout=20m -count=1`
- `processor/graph-index/owner_filter_load_integration_test.go:125` — `ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)`

The gate measures wall-clock inside one `go test ./...` invocation that caps neither package parallelism nor
concurrent testcontainer count, under `-race`, on a single shared `ubuntu-latest` runner. The contention is therefore
in-run and self-inflicted as well as external: up to `GOMAXPROCS` Docker-backed integration packages execute while
this one measures latency. The harness's own context is 15 minutes and never bounds a call; `natsclient`'s 5s default
does.

## 7. The other per-repetition gate, the anti-relaxation pins, and production's identical bound

- `processor/graph-index/owner_filter_load_integration_test.go:375` — `require.Equal(t, 1, result.count, result.label)`
- `processor/graph-index/owner_filter_load_integration_test.go:376` — `require.Less(t, result.duration, profile.operationBudget, result.label)`
- `processor/graph-index/owner_filter_load_integration_test.go:400` — `assertOwnerLoadLatency(t, label, samples, profile)`
- `processor/graph-index/owner_filter_budget_contract_test.go:29` — `require.Equal(t, 3*time.Second, ci.operationBudget,`
- `processor/graph-index/owner_filter_budget_contract_test.go:32` — `require.Equal(t, 3*time.Second, ci.p95Budget`
- `processor/graph-index/owner_filter_budget_contract_test.go:55` — `require.Len(t, durations, ci.repetitions`
- `processor/graph-index/owner_filter_budget_contract_test.go:66` — `so the per-rep gate MUST remain`
- `natsclient/kv.go:70` — `return context.WithTimeout(ctx, kv.options.Timeout)`
- `processor/graph-index/component.go:927` — `c.nameBucket = c.natsClient.NewKVStore(nameBucket)`
- `processor/graph-index/component.go:953` — `kvStore := c.natsClient.NewKVStore(bucket)`
- `processor/graph-index/owner_filter_load_integration_test.go:408` — `require.Eventually(t, func() bool {`
- `natsclient/kv.go:37` — `MaxRetries:            10, // Increased for high-contention scenarios`
- `natsclient/kv.go:41` — `UseExponentialBackoff: true,`

`:489` is not the only per-repetition budget assertion. The concurrent phase asserts the same `operationBudget` at
`:376` over the results channel, and unlike `:489` it does not `FailNow` before an aggregate: `:400` runs after the
whole channel drains. That gate measures owner filters only (`:375` requires exactly 1 key), which run 2-6ms, and no
firing in the window came from it. Any change to the per-repetition rule has two homes, not one.

`owner_filter_budget_contract_test.go` is the anti-relaxation pin and is itself a constraint on the target state.
`:29`/`:32` fail if `operationBudget` or `p95Budget` moves off 3s. `:55` fails if `repetitions` moves off 5, because
its fixture is a hardcoded five-sample slice. `:66` asserts in prose that the per-repetition gate must remain.

Production graph-index builds its KV stores through the same `NewKVStore(bucket)` call with no option override
(`:927`, `:953`), so the 5s bound at `natsclient/kv.go:70` is the production bound too, not a test artifact.

The repository's established answer to "a transient makes one observation unreliable" is bounded re-observation, not
a wider threshold: `natsclient/kv.go:37`/`:41` retry ten times with exponential backoff, and this very harness already
decides its consumer-return-to-baseline assertion by re-observation at `:408` before the equality check at `:414`.
The budget gate is the only assertion in the file that decides on a single sample.

## 8. The same-class instance in the same package: the predicate-layout smoke harness

- `processor/graph-index/predicate_layout_smoke_integration_test.go:89` — `// CI budgets carry ≥3× headroom over observed healthy latencies per the`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:90` — `// wall-clock-assertion discipline (gh#220): shared-runner contention put a`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:94` — `// budgets belong to the opt-in "full" profile on a quiet box.`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:96` — `name: "ci", entities: 5_000, spread: 20, churnWriters: 2, churnPerWriter: 100,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:98` — `p95Budget: 8 * time.Second, p99Budget: 9 * time.Second, maxServerRSSBytes: 1 << 30,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:84` — `name: "full", entities: 21_000, spread: 20, churnWriters: 4, churnPerWriter: 500,`
- `service/service_manager_health_listener_test.go:114` — `// gh#209 / gh#220 — budget widened from 3s to 10s. The 3s budget`
- `service/service_manager_health_listener_test.go:253` — `// gh#209/gh#220: 3s → 10s for the same reason as the sister test.`
- `docs/operations/32-predicate-layout-smoke-harness.md:77` — `The CI profile is a regression guard, not a source for comparative layout selection.`
- `docs/operations/32-predicate-layout-smoke-harness.md:80` — `## Pre-tag owner-filter acceptance record`
- `docs/operations/32-predicate-layout-smoke-harness.md:43` — `recorded below under the OLD pin are historical`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:208` — `membershipRaw: membershipRaw, membership: nc.NewKVStore(membershipRaw), membershipStream: membershipStream,`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:479` — `require.NoError(t, err, label)`
- `processor/graph-index/predicate_layout_smoke_integration_test.go:480` — `require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)`
- `docs/operations/32-predicate-layout-smoke-harness.md:120` — `| 5k CI | 4 | PREDICATE | 1.857667 | 2.664542 | 2.664542 | 3.100250 | PASS |`
- `docs/operations/32-predicate-layout-smoke-harness.md:133` — `All latency values above are milliseconds.`
- `docs/operations/32-predicate-layout-smoke-harness.md:160` — `| Candidate | Operation | p95 ms | p99 ms |`
- `docs/operations/32-predicate-layout-smoke-harness.md:168` — `| Hash plus catalog | Namespace catalog join | 333.641500 | 336.952084 |`

`git grep -ln operationBudget` returns three code files, not two. The third is the predicate-layout smoke harness:
same package, same profile-struct shape, same three budget fields — and it hit this identical flake and resolved it
on 2026-07-18 by widening the CI profile to 10s/8s/9s and demoting it to an order-of-magnitude regression guard,
citing a standing repository discipline, **gh#220**, that the owner-filter harness never cites. gh#220 is applied in
a third place as well (`service/service_manager_health_listener_test.go:114`, `:253`, both 3s -> 10s).

The runbook already carries the doctrine half at `:77`. Note what `:43` does to the alternative evidence home: the
owner-filter acceptance record at `:80`+ was measured on `nats:2.12.4-alpine` at revision `0a7af288`, and `:43`
declares every performance row recorded under that old pin historical.

**The sibling's budget is unreachable dead code, so its silence proves nothing.** `:208` builds its measured store
with `nc.NewKVStore(membershipRaw)` and no timeout override, so it carries the same 5s deadline. Its loop asserts
`require.NoError(t, err, label)` at `:479` BEFORE `require.Less(..., profile.operationBudget)` at `:480`, and its
`operationBudget` is 10s (`:85`, `:97`). A stalled call therefore fails as `context deadline exceeded` at `:479` and
never reaches the 10-second comparison — structurally the same defect as `owner_filter_load_integration_test.go:85`.

**And the widening that produced it rests on a unit error.** `:90`-`:92` justifies 3s -> 10s/8s/9s with "healthy p95
already 2.65s". The smoke's own latency table is headed `p95 ms` / `p99 ms` (`docs/operations/32-...:160`) with a
worst row of 333.641500 ms (`:168`). The value `2.664542` is the **owner-filter** harness's 5k CI PREDICATE p95
(`:120`) under an explicit `All latency values above are milliseconds` (`:133`) — and it is a single-key owner
lookup, not a forward filter. That comment read another harness's millisecond value as seconds. Filed as #1286; not
repaired here.

## 9. Where the framework bound is actually enforced, and where ordering is destroyed

- `natsclient/kv.go:69` — `if kv.options.Timeout > 0 {`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`
- `natsclient/kv.go:592` — `if err := ctx.Err(); err != nil {`
- `processor/graph-index/owner_filter_load_integration_test.go:497` — `sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })`

The 5s deadline is not applied unconditionally at `:70`: `:69` guards it on `Timeout > 0`, and the application site
for the measured operation is `applyTimeout` called at `:538`. The refusal to return a partial key set is enforced in
`collectFilteredKeys` at `:589` (drain-time cancellation) and `:592` (post-closure re-check), not in the doc comment
at `:533`-`:536`.

`:497` sorts the duration slice before any percentile is computed, so submission order is destroyed before anything
is recorded. A summary of p50/p95/p99/max cannot answer whether an inflated sample was isolated or adjacent to
another — which is the discriminator between a single stall and a sustained one.

## 10. What the CI profile asserts besides the per-operation budget

- `processor/graph-index/owner_filter_load_integration_test.go:374` — `require.NoError(t, result.err, result.label)`
- `processor/graph-index/owner_filter_load_integration_test.go:402` — `require.LessOrEqual(t, queueHighWater, 1000, "dispatcher queue must remain bounded")`
- `processor/graph-index/owner_filter_load_integration_test.go:443` — `require.Len(t, keys, fixture.wantForward, "%s did not converge", fixture.name)`
- `docs/operations/evidence/graph-index-pre-tag-0a7af288.md:27` — `GRAPH_INDEX_OWNER_FILTER_FULL=1 go test -race -tags=integration ./processor/graph-index`

Removing the per-repetition budget leaves the guard with: exact match-set correctness (`:488`, `:375`), post-churn
convergence (`:443`), the typed-error ceiling (`:487`, `:374`), the percentile gates (`:492`, `:400`), bounded
dispatcher queue (`:402`), temporary consumers returning to every baseline (`:408`, `:414`), released subscriptions
(`:448`), zero slow consumers (`:450`), and the NATS RSS bound. `:27` of the evidence file records the exact
supervised command the 2026-07-17 acceptance record was produced with.

## Adjacent claims

Analysis and unresolved questions. Not pins.

- #1284 — the issue this change answers; #750 — closed as COMPLETED 2026-07-30 having shipped only the comment at `:73`.
- **Citation defect A.** `openspec/specs/graph-index/spec.md:184` attributes the 3s CI guard to ADR-065. ADR-065
  contains no 3-second budget; its stated absolute bound for this operation class is the 10s handler timeout
  (`docs/adr/065-...:49`). The 3s figure is ADR-077 condition 4's tightening.
- **Citation defect B, and the repair target is wrong too.** The contract comment at `:60`-`:75` cites
  `docs/operations/32-predicate-layout-smoke-harness.md:49-50` for the 3s/10s profile assignment. Those lines are
  "Run the CI profile:" (the `TestIntegration_PredicateLayoutSmoke` reproduction command) and a blank line. The
  budget table at `:70`-`:71` is **also the wrong target**: it describes the SMOKE harness, decided by the churn
  column — `:70`'s `2 writers x 100` is `predicate_layout_smoke...:96`, while the owner harness runs 4 workers x
  `churnPerWriter: 50` (`:59`); `:71`'s `4 writers x 500` is `predicate_layout_smoke...:84`. The owner harness has
  its own section at `:80`+.
- **Citation defect C (new).** `docs/operations/32-...:70` asserts the CI profile gates "every operation <3s;
  p95/p99 <=3s". Its own harness has read 10s/8s/9s since 2026-07-18 (`predicate_layout_smoke...:97`-`:98`). The row
  has been stale for ~7 weeks and is a separate filing, not this change's repair.
- **The full profile's absolute ceiling is unreachable.** `:85` sets `operationBudget: 10 * time.Second`, but every
  measured call is a `KeysByFilter` bounded at 5s (`natsclient/kv.go:39`, `:69`-`:70`, applied at `:538`), and an
  expiry fails at `:487` before `:489` is reached. A 10-second per-operation budget on this operation can never fire.
  The 10-second figure is the **query handler's** bound (`docs/adr/065-...:49`), imported onto an operation the KV
  client bounds at 5s.
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
- **RESOLVED, and a second number must not be confused with it.** Two "2.2-2.7 second" figures are in play and only
  one is genuine. gh#750's **2.237s is real seconds**, verified from its run log. The smoke comment's **2.65 is
  milliseconds misread as seconds** (section 8, #1286) — and it is the owner-filter harness's number, not the
  smoke's. Revision 2 falsified the millisecond-misread hypothesis on the strength of #750 alone and over-generalized
  it; the hypothesis was right about the other number.
- **The gh#750 comment's "same-run max of 2.24s" was a real measurement of a distribution that no longer exists.**
  #750's body carries the line verbatim:
  `phase=latency filter=predicate-forward reps=5 p50=99.784608ms p95=697.726516ms max=2.23697341s`, and #750 itself
  reads it as "a long tail an order of magnitude past the median" — so it is genuine seconds, not a millisecond
  misread. Three fully-logged runs in the current window show that distribution has collapsed:

  | run | date | predicate-forward p50 | p95 | max | max/p50 |
  |---|---|---|---|---|---|
  | (gh#750, 2026-07-30) | pre-pin-move | 99.8ms | 697.7ms | 2.237s | 22.4x |
  | 33208133273 | 2026-08-28 | 149.4ms | 175.4ms | 389.0ms | 2.60x |
  | 33260659637 | 2026-08-29 | 156.1ms | 157.5ms | 165.9ms | 1.06x |
  | 34367949188 | 2026-09-09 | 153.0ms | 169.9ms | 189.8ms | 1.24x |

  p95 collapsed ~698ms -> ~170ms; max 2.237s -> 166-389ms. The comment at `:73` is therefore **stale evidence, not a
  wrong measurement**, and `docs/operations/32-...:43`'s historical-rows warning is exactly the mechanism.
- **gh#220's >=3x rule was satisfied and was never the problem.** 3s / 389ms = **7.7x**. A stall adds seconds rather
  than multiplying them, so the load-bearing quantity is **absolute headroom (2.61s)**, not the ratio. A 3x ratio
  buys a slow assertion many absolute seconds and a fast one almost none — which is why the same discipline protects
  a harness whose healthy latency is large and leaves this one exposed. That generalizes beyond this harness.
  (The smoke's own "1.13x" figure is void: its 2.65 is milliseconds misread as seconds — section 8, #1286.)
- **The firings are excursions far outside the distribution, not its tail.** Against the same run's own
  `predicate-forward` p50: 3.356s is 21.9x (run 34367949188), 4.760s is 30.5x (run 33260659637); against those runs'
  forward maxima, 17.7x and 28.7x. In gh#750's era a 2.24s sample was INSIDE a 22x-spread distribution; today a
  3.4-4.8s sample is far outside a 1.06-2.6x one. #750's diagnosis was right for 2026-07-30 and does not describe
  today.
- **A FIFTH firing, outside the inventory's original window.** Run 32872635700, 2026-08-25T16:38Z, branch
  `claude/gh1010-flowstore-list-current-state`: `:489`, `"4.80475016s" is not less than "3s"`, `name-forward rep 2`,
  package `FAIL ... 34.465s`. It is the largest budget firing observed and it makes `name-forward` a second
  twice-firing label.
- **VOID as evidence: the sibling A/B.** The sweep itself is sound — 80 CI failures fetched with stderr captured,
  42 created on or after 2026-08-26 (0 fetch errors) plus a 40-run slice spanning 2026-05-30..2026-08-26 (38
  fetchable, 2 HTTP 410); `PredicateLayoutSmoke` appears in **zero**, `OwnerFilterLoadHarness` in **five**. But the
  inference drawn from it does not hold: section 8 shows the sibling's budget cannot fire at all, because its
  `require.NoError` at `:479` precedes its `require.Less` at `:480` and its own 5s deadline expires first. **Zero
  firings is what an unreachable assertion looks like, not what a well-chosen budget looks like.** The only claim the
  sweep still supports is the count of five owner-harness events. Do not cite the A/B in support of any option.
- **No budget choice reduces the residual below one event.** Healthy forward max is 389ms; the five observed events
  imply stalls of ~3.2s, ~3.3s, ~4.6s, ~4.6s and >=4.7s. Absorbing the largest needs ~5s of budget, which is where
  `KeysByFilter` fails as a typed error instead — and event 4 (run 33208133273) already did exactly that.
- **A bare `--limit` failure list is not "the most recent failures".** `gh run list --workflow=ci.yml
  --status=failure --limit 40` returned a slice spanning 2026-05-30..2026-08-26 and contained none of the four
  firings the original sweep found. The date-bounded form (`--created '>=2026-08-26'`) returned all four. Any count
  taken with the bare form is over an unstated window.
- **PARTLY RESOLVED: whether the 3s-5s band contains a real tail of the 5,000-key drain.** No observed forward
  distribution reaches it: the three fully-logged runs cap at 389ms, 166ms and 190ms. What stays undecided is
  whether a firing's own neighbouring repetitions were also inflated, because section 1's discard means no firing has
  ever recorded its reps 0..n-1 — and section 9's `:497` sort means even a recorded set would lose the ordering that
  answers it.

- **The activation evidence is invisible on a green run.** `t.Logf` output is emitted only for failing tests unless
  `-v` is passed, and `scripts/run-integration-tests.sh:311` does not pass it. Measured on run 34475316237 (`main`,
  green): the only graph-index line is `ok github.com/c360studio/semstreams/processor/graph-index 60.725s`; no
  `phase=` line appears. ADR-077's Status requires evidence "recorded against the exact implementation revision"
  (`:8`) and `docs/operations/32-...:75` calls silence a failed evidence run. Today a green CI run records nothing.
- **The filter that fires most is the one whose distribution has never been logged.** `incoming-forward` accounts for
  2 of the 4 firings and its `phase=latency` line has never been printed in the window, because it fails before
  `:492` and a passing run prints nothing.
- **Cost of an added repetition (arithmetic over section-5 measurements, not a pin).** The sequential measure phase
  per repetition is the sum of three forward p50s plus three owner p50s: 153ms + 256ms + [`incoming-forward`
  unmeasured; bracketed 150-390ms by the window's forward range] + ~11ms = **~0.6-0.8s**. The concurrent phase adds
  three owner-filter operations (~12ms). Raising `repetitions` 5 -> 21 therefore costs **~10-13s** on a harness that
  ran 35.06s inside a 60.7s package, inside a `Test` job capped at `timeout-minutes: 25`.
- **Sister repos do not cite the 3s budget.** Read-only sweep of the 27 sibling checkouts under
  `/Users/coby/Code/c360/`: semboids cites ADR-077's key shape and its LIST traffic
  (`docs/perf/beta149-migration-2026-07-18.md:52`, `:189`; `internal/boidgraph/neighbor_empty_verify_test.go:159`),
  semsource cites ADR-077 in an audit (`docs/upstream/semstreams-pre-v1-core-audit.md:299`). No sister cites
  condition 4, the 3-second figure, or the harness. No sister repository was modified.
- **ADR-107 is already taken.** `git log --all --diff-filter=A -- 'docs/adr/107*'` -> `bc7d79cc` on the unmerged
  `claude/gh1267-honor-predicate-datatype` branch (semweb boundary rule). A new ADR filed by this change would be 108
  and would race that branch's number; the in-place amendment precedent is `docs/adr/046-...:14`.

- **Supervised baselines at the current pin (new, 2026-09-11).** Revision `60c79736`, worktree clean, same host as
  the `0a7af288` record (Apple M3 Pro; 12 CPU; 38,654,705,664 bytes); pin moved to `nats:2.14.4-alpine`, SDK
  `v1.52.0`, Docker 29.7.2. **21k full: PASS 43.17s, exit 0** — worst measurement p95 311.449ms
  (`incoming-forward`, 16 workers), worst p99 320.157ms and worst max 396.719ms (`predicate-forward`, 16 workers),
  worst concurrent-phase p95 55.978ms. **5k CI: PASS 2.12s, exit 0** — worst measurement p95 77.861ms and worst max
  80.068ms (`name-forward`), worst concurrent-phase p95 4.397ms, fastest filter p95 771.792µs (`predicate-owner`).
  Raw logs are in this session's scratchpad and must be published in-tree (`tasks.md` 3.3); they are not pinned here
  because a scratchpad path is not repository content.
- **The 5k CI budgets cannot be derived from the 21k run.** Worst forward p95 is 311.449ms at 21k against 77.861ms
  at 5k — 4x apart. Both profiles had to be recorded, which is why `tasks.md` 3.2 exists as its own task.
- **The contention tax, measured.** Same profile, same workload: subtest 2.12s on the quiet box against 8.64s on the
  shared runner (run 34367949188, `workers-4`; whole test 11.02s) = 4.1x; forward-filter p95 78ms quiet against
  157-175ms shared = 2.0-2.2x. This is steady-state contention and is a different quantity from the 3.2-4.8s stalls. **[REPUDIATED — see `design.md` § 11.11]**
- **One CI budget spans a 108x range of healthy values.** 771.792µs (`predicate-owner`) to 77.861ms
  (`name-forward`) under a single `p95Budget` — the absolute-headroom failure mode reproduced inside one profile.
- **METHOD: a duration regex that silently dropped two rows.** Parsing the baseline logs with `p50=[0-9.]+m?s`
  returned 16 of 18 `phase=latency` lines and dropped both `name-owner` rows (`p50=876.833µs`), because `m?s` cannot
  match `µs`. The absence read as "that filter did not run". Go emits `ns`, `µs`, `ms` and `s`; match the unit as a
  set and check the parsed count against `grep -c` first. Re-parsed: 18 of 18 and 9 of 9.

### Observed firings

| # | run | date | branch | assertion | value | label |
|---|-----|------|--------|-----------|-------|-------|
| 1 | 34372644231 | 2026-09-09 | `claude/gh1267-honor-predicate-datatype` | `:489` budget | 3.515s | `predicate-forward` rep 2 |
| 2 | 34367949188 | 2026-09-09 | `claude/gh1261-graph-read-tools` | `:489` budget | 3.356s | `incoming-forward` rep 3 |
| 3 | 33260659637 | 2026-08-29 | `main` | `:489` budget | 4.760s | `incoming-forward` rep 2 |
| 4 | 33208133273 | 2026-08-28 | `claude/gh1095-entity-id-slice-b` | `:487` error | `context deadline exceeded` | `name-forward` |
| 5 | 32872635700 | 2026-08-25 | `claude/gh1010-flowstore-list-current-state` | `:489` budget | 4.805s | `name-forward` rep 2 |

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
