# Graph-Index Pre-Tag Evidence — 0a7af288

This appendix preserves the literal focused-run evidence behind the acceptance tables in
[32 — predicate layout evidence and reproduction runbook](../32-predicate-layout-smoke-harness.md). It is an
audit record, not a new operating contract or a substitute for the remaining product and cutover gates.

## Provenance

| Field | Recorded value |
|---|---|
| SemStreams revision | `0a7af2889a7ca3010924832e6c8c5d7896e2aa97` |
| Worktree | Clean before all three runs |
| Run window | 2026-07-17 15:56:10–15:58:42 CDT |
| Host | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,744 MB |
| Docker client/server | 29.6.1/29.6.1; testcontainers API 1.51; CLI API 1.55 |
| NATS server | `nats:2.12.4-alpine` |
| NATS image digest | `sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea` |
| Go SDK | `github.com/nats-io/nats.go v1.48.0` |

## Exact commands

These are the literal commands used. They remain unwrapped so their flag and environment ordering is auditable.

```bash
env TESTCONTAINERS_RYUK_DISABLED=true go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
env TESTCONTAINERS_RYUK_DISABLED=true GRAPH_INDEX_OWNER_FILTER_FULL=1 go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
env TESTCONTAINERS_RYUK_DISABLED=true PREDICATE_SMOKE_FULL=1 go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_PredicateLayoutSmoke$' -count=1 -v -timeout=25m
```

## Owner-filter 5k CI profile

The focused test passed in 2.45 seconds; its package completed in 4.369 seconds. The lines below are the literal
evidence emitted by the harness. Long log lines are intentionally not wrapped.

```text
phase=setup profile=ci entities=5000 name_context=5000 spread=20 reps=5 workers=[4] server=2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea sdk=v1.48.0
phase=seed rows=20020 elapsed=515.482834ms throughput=38837.4_rows_per_second
phase=seed-complete cpu=0.00 rss=43667456 subscriptions=77 rss_delta=26759168
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 context=710 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
phase=latency filter=predicate-owner reps=5 p50=729µs p95=741.459µs p99=741.459µs max=761.417µs
phase=latency filter=predicate-forward reps=5 p50=60.265958ms p95=61.619583ms p99=61.619583ms max=62.818792ms
phase=latency filter=name-owner reps=5 p50=1.729959ms p95=1.74875ms p99=1.74875ms max=1.999541ms
phase=latency filter=name-forward reps=5 p50=74.30675ms p95=76.846417ms p99=76.846417ms max=76.887042ms
phase=latency filter=incoming-owner reps=5 p50=808.75µs p95=845.5µs p99=845.5µs max=991.208µs
phase=latency filter=incoming-forward reps=5 p50=69.603ms p95=69.857292ms p99=69.857292ms max=91.852167ms
phase=latency filter=context-owner reps=5 p50=805.25µs p95=876.125µs p99=876.125µs max=1.154667ms
phase=latency filter=predicate reps=5 p50=1.857667ms p95=2.664542ms p99=2.664542ms max=3.10025ms
phase=latency filter=name reps=5 p50=1.732667ms p95=1.789667ms p99=1.789667ms max=1.802125ms
phase=latency filter=incoming reps=5 p50=2.351834ms p95=2.376125ms p99=2.376125ms max=2.958458ms
phase=latency filter=context reps=5 p50=1.835041ms p95=1.879583ms p99=1.879583ms max=1.975709ms
phase=concurrent workers=4 operations=20 catch_up=10.453292ms throughput=1913.3_ops_per_second queue_high_water=16
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=3 aggregate_after=0 predicate_baseline=0 predicate_high=2 predicate_after=0 name_baseline=0 name_high=0 name_after=0 incoming_baseline=0 incoming_high=1 incoming_after=0 context_baseline=0 context_high=1 context_after=0
phase=resource workers=4 cpu_before=0.00 cpu_after=19.00 rss_before=43667456 rss_after=65789952 subscriptions_before=83 subscriptions_after=83 slow_consumers=0
```

```text
--- PASS: TestIntegration_OwnerFilterLoadHarness (2.45s)
    --- PASS: TestIntegration_OwnerFilterLoadHarness/workers-4 (1.32s)
PASS
ok  github.com/c360studio/semstreams/processor/graph-index  4.369s
```

## Owner-filter 21k full profile

The full test passed in 43.90 seconds; its package completed in 45.764 seconds. Setup, seed, and lifecycle evidence
is shared by its four- and 16-worker subtests.

```text
phase=setup profile=full entities=21000 name_context=5000 spread=20 reps=30 workers=[4 16] server=2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea sdk=v1.48.0
phase=seed rows=52020 elapsed=1.184951042s throughput=43900.5_rows_per_second
phase=seed-complete cpu=77.00 rss=71589888 subscriptions=77 rss_delta=54710272
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 context=710 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
```

### Workers 4

```text
phase=latency filter=predicate-owner reps=30 p50=2.465334ms p95=3.423333ms p99=4.208458ms max=5.117333ms
phase=latency filter=predicate-forward reps=30 p50=262.236792ms p95=266.626584ms p99=267.830167ms max=285.212833ms
phase=latency filter=name-owner reps=30 p50=838.5µs p95=6.223208ms p99=6.726917ms max=7.909875ms
phase=latency filter=name-forward reps=30 p50=82.671ms p95=84.519208ms p99=85.487083ms max=86.012875ms
phase=latency filter=incoming-owner reps=30 p50=3.132417ms p95=4.9265ms p99=5.443ms max=14.397917ms
phase=latency filter=incoming-forward reps=30 p50=310.054333ms p95=315.656833ms p99=316.274542ms max=330.208375ms
phase=latency filter=context-owner reps=30 p50=867.083µs p95=2.537416ms p99=6.981417ms max=12.199208ms
phase=latency filter=predicate reps=30 p50=4.328875ms p95=9.863375ms p99=11.532875ms max=13.06825ms
phase=latency filter=name reps=30 p50=1.212792ms p95=2.099708ms p99=2.680291ms max=2.935375ms
phase=latency filter=incoming reps=30 p50=7.027291ms p95=12.10675ms p99=16.291458ms max=19.293542ms
phase=latency filter=context reps=30 p50=1.402959ms p95=2.484541ms p99=2.493125ms max=2.736333ms
phase=concurrent workers=4 operations=120 catch_up=122.427167ms throughput=980.2_ops_per_second queue_high_water=116
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=6 aggregate_after=0 predicate_baseline=0 predicate_high=2 predicate_after=0 name_baseline=0 name_high=1 name_after=0 incoming_baseline=0 incoming_high=3 incoming_after=0 context_baseline=0 context_high=2 context_after=0
phase=resource workers=4 cpu_before=77.00 cpu_after=61.00 rss_before=76013568 rss_after=93159424 subscriptions_before=83 subscriptions_after=83 slow_consumers=0
```

### Workers 16

```text
phase=latency filter=predicate-owner reps=30 p50=2.890291ms p95=5.007291ms p99=5.842958ms max=8.552ms
phase=latency filter=predicate-forward reps=30 p50=263.627458ms p95=266.836458ms p99=267.261625ms max=269.249958ms
phase=latency filter=name-owner reps=30 p50=910.208µs p95=1.965916ms p99=7.487625ms max=8.440333ms
phase=latency filter=name-forward reps=30 p50=81.534916ms p95=83.267583ms p99=83.726667ms max=93.41325ms
phase=latency filter=incoming-owner reps=30 p50=3.374542ms p95=5.4905ms p99=7.592292ms max=16.400291ms
phase=latency filter=incoming-forward reps=30 p50=310.731875ms p95=315.363583ms p99=315.506333ms max=316.191417ms
phase=latency filter=context-owner reps=30 p50=852.541µs p95=1.154208ms p99=1.258417ms max=20.803125ms
phase=latency filter=predicate reps=30 p50=10.855917ms p95=19.568375ms p99=23.123792ms max=23.422042ms
phase=latency filter=name reps=30 p50=3.971959ms p95=7.266209ms p99=7.294167ms max=7.694ms
phase=latency filter=incoming reps=30 p50=33.343792ms p95=57.322959ms p99=66.022958ms max=69.496584ms
phase=latency filter=context reps=30 p50=3.941916ms p95=7.602083ms p99=7.732625ms max=7.779083ms
phase=concurrent workers=16 operations=120 catch_up=114.719333ms throughput=1046.0_ops_per_second queue_high_water=104
phase=consumers workers=16 aggregate_baseline=0 aggregate_high=14 aggregate_after=0 predicate_baseline=0 predicate_high=5 predicate_after=0 name_baseline=0 name_high=4 name_after=0 incoming_baseline=0 incoming_high=11 incoming_after=0 context_baseline=0 context_high=4 context_after=0
phase=resource workers=16 cpu_before=61.00 cpu_after=72.00 rss_before=93159424 rss_after=92635136 subscriptions_before=83 subscriptions_after=83 slow_consumers=0
```

```text
--- PASS: TestIntegration_OwnerFilterLoadHarness (43.90s)
    --- PASS: TestIntegration_OwnerFilterLoadHarness/workers-4 (20.80s)
    --- PASS: TestIntegration_OwnerFilterLoadHarness/workers-16 (21.15s)
PASS
ok  github.com/c360studio/semstreams/processor/graph-index  45.764s
```

## Predicate 21k comparison

The comparative test passed in 73.70 seconds; its package completed in 75.623 seconds. Both candidates used 30
repetitions and four churn writers.

```text
phase=setup profile=full entities=21000 spread=20 reps=30 churn_writers=4 server=2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea sdk=v1.48.0
phase=seed-throughput codec=hash-catalog membership_rows=21021 catalog_rows=22 elapsed=500.248416ms throughput=42065.1_rows_per_second maximum_key_bytes=321
phase=seed codec=hash-catalog rss_before=16482304 rss_after=37306368 rss_delta=20824064 subscriptions=71
phase=latency operation=hash-catalog-exact reps=30 p50=301.054875ms p95=316.41975ms p99=323.088708ms max=323.088708ms
phase=latency operation=hash-catalog-owner reps=30 p50=2.015875ms p95=5.82075ms p99=17.336167ms max=17.336167ms
phase=latency operation=hash-catalog-maximum-owner reps=30 p50=908.875µs p95=1.581375ms p99=1.706167ms max=1.706167ms
phase=latency operation=hash-catalog-namespace-catalog-join reps=30 p50=326.39925ms p95=333.6415ms p99=336.952084ms max=336.952084ms
phase=latency operation=hash-catalog-exact-under-churn reps=30 p50=300.727667ms p95=307.110334ms p99=321.958125ms max=321.958125ms
phase=churn codec=hash-catalog writers=4 mutations=2000 convergence=exact
phase=restart codec=hash-catalog fresh_bucket_handles=parity
phase=resource codec=hash-catalog cpu_before=0.00 cpu_after=27.00 rss_before=16482304 rss_after=43134976 subscriptions_before=71 subscriptions_after=71 slow_consumers=0 membership_consumer_baseline=0 membership_consumer_high_water=1 membership_consumer_after=0 catalog_consumer_baseline=0 catalog_consumer_high_water=1 catalog_consumer_after=0
phase=seed-throughput codec=raw-nine-token membership_rows=21021 catalog_rows=0 elapsed=462.533333ms throughput=45447.5_rows_per_second maximum_key_bytes=451
phase=seed codec=raw-nine-token rss_before=18055168 rss_after=39448576 rss_delta=21393408 subscriptions=68
phase=latency operation=raw-nine-token-exact reps=30 p50=263.801709ms p95=268.287333ms p99=269.328542ms max=269.328542ms
phase=latency operation=raw-nine-token-owner reps=30 p50=1.816125ms p95=7.901ms p99=9.793208ms max=9.793208ms
phase=latency operation=raw-nine-token-maximum-owner reps=30 p50=879.125µs p95=1.736042ms p99=2.261292ms max=2.261292ms
phase=latency operation=raw-nine-token-namespace-category reps=30 p50=266.9915ms p95=274.753333ms p99=286.406875ms max=286.406875ms
phase=latency operation=raw-nine-token-namespace-domain reps=30 p50=267.354375ms p95=278.815417ms p99=292.620875ms max=292.620875ms
phase=latency operation=raw-nine-token-exact-under-churn reps=30 p50=264.068542ms p95=270.7705ms p99=284.744875ms max=284.744875ms
phase=churn codec=raw-nine-token writers=4 mutations=2000 convergence=exact
phase=restart codec=raw-nine-token fresh_bucket_handles=parity
phase=resource codec=raw-nine-token cpu_before=0.00 cpu_after=27.00 rss_before=18055168 rss_after=51933184 subscriptions_before=68 subscriptions_after=68 slow_consumers=0 membership_consumer_baseline=0 membership_consumer_high_water=1 membership_consumer_after=0 catalog_consumer_baseline=0 catalog_consumer_high_water=0 catalog_consumer_after=0
```

```text
--- PASS: TestIntegration_PredicateLayoutSmoke (73.70s)
    --- PASS: TestIntegration_PredicateLayoutSmoke/hash-catalog (34.90s)
    --- PASS: TestIntegration_PredicateLayoutSmoke/raw-nine-token (38.80s)
PASS
ok  github.com/c360studio/semstreams/processor/graph-index  75.623s
```

All three commands returned PASS. The raw lines above are the values transcribed into operations guide 32; no
comparative ratio is an acceptance threshold.
