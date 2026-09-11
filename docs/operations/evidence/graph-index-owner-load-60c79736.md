# Graph-Index Owner-Load Evidence — 60c79736

This appendix preserves the literal harness evidence behind the owner-filter acceptance tables in
[32 — predicate layout evidence and reproduction runbook](../32-predicate-layout-smoke-harness.md). It is an audit
record, not a new operating contract.

It is the **supervised record that satisfies ADR-077 § 8 condition 4** under the #1284 amendment: the
continuously-running CI guard is a regression guard and carries no per-operation wall-clock budget, so condition 4's
evidence comes from a supervised run recorded against the current server and SDK pin. It supersedes the
owner-filter rows of [the 0a7af288 pre-tag record](graph-index-pre-tag-0a7af288.md), which were taken on
`nats:2.12.4-alpine` with SDK `v1.48.0` and are retained there as history.

## Provenance

| Field | Recorded value |
|---|---|
| SemStreams revision | `60c79736` |
| Worktree | Clean before both runs |
| Run window | 2026-09-11 15:44:56–15:46:50 CEST |
| Host | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,742 MB |
| Docker server / API | 29.7.2; API 1.51; testcontainers-go v0.40.0 |
| NATS server | `nats:2.14.4-alpine` |
| NATS image digest | `sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` |
| Go SDK | `github.com/nats-io/nats.go v1.52.0` |
| Box condition | Quiet — no other test process running. The same subtest measures 8.64 s on a shared CI runner against 2.12 s here; treat every number below as the uncontended floor, not a CI expectation. |

## Exact commands

These are the literal commands used. They remain unwrapped so their flag and environment ordering is auditable.

```bash
env TESTCONTAINERS_RYUK_DISABLED=true go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
env TESTCONTAINERS_RYUK_DISABLED=true GRAPH_INDEX_OWNER_FILTER_FULL=1 go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
```

Every duration the harness emits carries its own Go unit suffix (`ns`, `µs`, `ms`, `s`). Read the suffix on each
value: a parser that assumed `ms` silently dropped two `µs` rows while preparing this record.

## Owner-filter 5k CI profile

`TestIntegration_OwnerFilterLoadHarness` passed in **2.12 s** (subtest `workers-4` 1.36 s); its package completed in
3.931 s. The lines below are the literal evidence emitted by the harness. Long log lines are intentionally not
wrapped.

```text
phase=setup profile=ci entities=5000 name_context=5000 spread=20 reps=5 workers=[4] server=2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66 sdk=v1.52.0
phase=seed rows=15020 elapsed=347.565625ms throughput=43214.9_rows_per_second
phase=seed-complete cpu=0.00 rss=35729408 subscriptions=74 rss_delta=18161664
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
```

### Workers 4

```text
phase=latency filter=predicate-owner reps=5 p50=722.625µs p95=771.792µs p99=771.792µs max=853.667µs
phase=latency filter=predicate-forward reps=5 p50=61.934625ms p95=62.481042ms p99=62.481042ms max=63.941708ms
phase=latency filter=name-owner reps=5 p50=1.798167ms p95=2.907666ms p99=2.907666ms max=3.329583ms
phase=latency filter=name-forward reps=5 p50=77.363292ms p95=77.86075ms p99=77.86075ms max=80.06825ms
phase=latency filter=incoming-owner reps=5 p50=1.919416ms p95=2.511958ms p99=2.511958ms max=2.801333ms
phase=latency filter=incoming-forward reps=5 p50=73.377584ms p95=74.621667ms p99=74.621667ms max=74.957958ms
phase=latency filter=incoming reps=5 p50=2.384042ms p95=2.793375ms p99=2.793375ms max=3.952291ms
phase=latency filter=predicate reps=5 p50=3.311417ms p95=3.341458ms p99=3.341458ms max=4.36ms
phase=latency filter=name reps=5 p50=2.717ms p95=4.397375ms p99=4.397375ms max=4.815625ms
phase=concurrent workers=4 operations=15 catch_up=14.282792ms throughput=1050.2_ops_per_second queue_high_water=11
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=2 aggregate_after=0 predicate_baseline=0 predicate_high=0 predicate_after=0 name_baseline=0 name_high=2 name_after=0 incoming_baseline=0 incoming_high=2 incoming_after=0
phase=resource workers=4 cpu_before=0.00 cpu_after=54.00 rss_before=35860480 rss_after=47923200 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

## Owner-filter 21k full profile

`TestIntegration_OwnerFilterLoadHarness` passed in **43.17 s** (subtests `workers-4` 20.50 s, `workers-16` 21.01 s);
its package completed in 45.212 s.

```text
phase=setup profile=full entities=21000 name_context=5000 spread=20 reps=30 workers=[4 16] server=2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66 sdk=v1.52.0
phase=seed rows=47020 elapsed=1.005461042s throughput=46764.6_rows_per_second
phase=seed-complete cpu=77.00 rss=61468672 subscriptions=74 rss_delta=43745280
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
```

### Workers 4

```text
phase=latency filter=predicate-owner reps=30 p50=2.392541ms p95=2.771167ms p99=3.261125ms max=3.417958ms
phase=latency filter=predicate-forward reps=30 p50=262.270125ms p95=265.5305ms p99=265.934958ms max=291.337833ms
phase=latency filter=name-owner reps=30 p50=876.833µs p95=2.036667ms p99=2.130459ms max=2.57375ms
phase=latency filter=name-forward reps=30 p50=77.966917ms p95=81.49025ms p99=81.941917ms max=83.001792ms
phase=latency filter=incoming-owner reps=30 p50=3.408333ms p95=5.342291ms p99=6.588416ms max=14.493625ms
phase=latency filter=incoming-forward reps=30 p50=305.528791ms p95=310.302583ms p99=310.509291ms max=313.09825ms
phase=latency filter=incoming reps=30 p50=7.89225ms p95=14.180917ms p99=15.652625ms max=18.779917ms
phase=latency filter=predicate reps=30 p50=4.891417ms p95=9.244708ms p99=17.37825ms max=19.599875ms
phase=latency filter=name reps=30 p50=1.226083ms p95=1.720709ms p99=1.798167ms max=1.891833ms
phase=concurrent workers=4 operations=90 catch_up=123.470541ms throughput=728.9_ops_per_second queue_high_water=86
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=4 aggregate_after=0 predicate_baseline=0 predicate_high=3 predicate_after=0 name_baseline=0 name_high=1 name_after=0 incoming_baseline=0 incoming_high=3 incoming_after=0
phase=resource workers=4 cpu_before=77.00 cpu_after=43.00 rss_before=61599744 rss_after=74203136 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

### Workers 16

```text
phase=latency filter=predicate-owner reps=30 p50=3.076333ms p95=5.646542ms p99=5.815125ms max=6.011875ms
phase=latency filter=predicate-forward reps=30 p50=261.956667ms p95=284.592166ms p99=320.157208ms max=396.719209ms
phase=latency filter=name-owner reps=30 p50=875.833µs p95=1.838583ms p99=2.746ms max=2.749625ms
phase=latency filter=name-forward reps=30 p50=78.166375ms p95=81.476291ms p99=81.974625ms max=82.375667ms
phase=latency filter=incoming-owner reps=30 p50=3.62375ms p95=5.073334ms p99=8.166542ms max=14.274ms
phase=latency filter=incoming-forward reps=30 p50=304.682833ms p95=311.448708ms p99=316.089958ms max=320.544833ms
phase=latency filter=name reps=30 p50=3.739958ms p95=16.374625ms p99=16.699209ms max=17.39625ms
phase=latency filter=predicate reps=30 p50=14.998042ms p95=26.782958ms p99=26.836292ms max=29.631042ms
phase=latency filter=incoming reps=30 p50=27.481375ms p95=55.977542ms p99=56.781542ms max=58.916417ms
phase=concurrent workers=16 operations=90 catch_up=122.906625ms throughput=732.3_ops_per_second queue_high_water=74
phase=consumers workers=16 aggregate_baseline=0 aggregate_high=13 aggregate_after=0 predicate_baseline=0 predicate_high=8 predicate_after=0 name_baseline=0 name_high=4 name_after=0 incoming_baseline=0 incoming_high=12 incoming_after=0
phase=resource workers=16 cpu_before=43.00 cpu_after=59.00 rss_before=74203136 rss_after=74637312 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

## What this record establishes, and what it does not

It establishes condition 4's supervised evidence on the current pin, and it is the source for the acceptance tables
in the runbook. It does not complete the component-level replacement, readiness, public-query, or clustering gates
in ADR-077, and it is not a CI expectation: these are quiet-box numbers.

The absolute ceiling on a directly measured key listing is the framework-enforced `natsclient` KV deadline
(`DefaultKVOptions().Timeout`), observed as the operation's own typed error. No per-operation wall-clock budget is
asserted here or in the harness.
