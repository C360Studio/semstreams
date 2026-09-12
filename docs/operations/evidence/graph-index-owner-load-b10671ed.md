# Graph-Index Owner-Load Evidence — b10671ed

This appendix preserves the literal harness evidence behind the owner-filter acceptance tables in
[32 — predicate layout evidence and reproduction runbook](../32-predicate-layout-smoke-harness.md). It is an audit
record, not a new operating contract.

It is the **supervised record that satisfies ADR-077 § 8 condition 4** under the #1284 amendment: the
continuously-running CI guard is a regression guard and carries no per-operation wall-clock budget, so condition 4's
evidence comes from a supervised run recorded against the current server and SDK pin.

**Taken at the revision that ships the submission-order instrument.** An earlier supervised pair was measured at
`60c79736`, before `d9582508` added per-repetition recording; those runs carried only p50/p95/p99/max and therefore
could not satisfy this change's own requirement that *"every filter's per-repetition durations are recorded in
submission order"* (`openspec/specs/graph-index/spec.md`, scenario *the measured distribution is recorded on a
passing run*). They are superseded by the runs below and are not cited anywhere as activation evidence. This record
supersedes the owner-filter rows of [the 0a7af288 pre-tag record](graph-index-pre-tag-0a7af288.md), taken on
`nats:2.12.4-alpine` with SDK `v1.48.0` and retained there as history.

## Provenance

| Field | Recorded value |
|---|---|
| SemStreams revision | `b10671ed` |
| Worktree | Clean before both runs |
| Run window | 2026-09-12 09:59:26–10:00:22 CEST |
| Host | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,742 MB |
| Docker server / API | 29.7.2; API 1.51; testcontainers-go v0.40.0 (the run log records no CLI version) |
| NATS server | `nats:2.14.4-alpine` |
| NATS image digest | `sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` |
| Go SDK | `github.com/nats-io/nats.go v1.52.0` |
| Box condition | Quiet — no other test process running. Treat every number below as the uncontended floor, not a CI expectation; the runbook records the measured contention tax. |

## Exact commands

These are the literal commands used. They remain unwrapped so their flag and environment ordering is auditable.

```bash
env TESTCONTAINERS_RYUK_DISABLED=true go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
env TESTCONTAINERS_RYUK_DISABLED=true GRAPH_INDEX_OWNER_FILTER_FULL=1 go test -race -tags=integration ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m
```

Every duration carries its own Go unit suffix (`ns`, `µs`, `ms`, `s`) — including each value inside `submitted=`.
Read the suffix on each one: a parser that assumed `ms` silently dropped two `µs` rows while preparing the earlier
record. Each `submitted=` list is in **submission order**, not sorted, which is what makes a stalled repetition
distinguishable from a shifted distribution.

## Owner-filter 5k CI profile

Passed in **2.24 s** (subtest `workers-4` 1.42 s); package 6.018 s.

```text
phase=setup profile=ci entities=5000 name_context=5000 spread=20 reps=5 workers=[4] server=2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66 sdk=v1.52.0
phase=seed rows=15020 elapsed=362.402291ms throughput=41445.7_rows_per_second
phase=seed-complete cpu=0.00 rss=35590144 subscriptions=74 rss_delta=18128896
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
```

### Workers 4

```text
phase=latency filter=predicate-owner reps=5 p50=780.459µs p95=785.125µs p99=785.125µs max=1.299417ms submitted=1.299417ms,785.125µs,780.459µs,732.667µs,735.208µs
phase=latency filter=predicate-forward reps=5 p50=63.429417ms p95=64.089542ms p99=64.089542ms max=65.950375ms submitted=63.367916ms,63.021458ms,63.429417ms,64.089542ms,65.950375ms
phase=latency filter=name-owner reps=5 p50=2.850542ms p95=2.950792ms p99=2.950792ms max=4.253333ms submitted=2.850542ms,4.253333ms,2.950792ms,2.003958ms,1.786375ms
phase=latency filter=name-forward reps=5 p50=83.035583ms p95=83.153459ms p99=83.153459ms max=85.519583ms submitted=78.509625ms,83.035583ms,85.519583ms,83.153459ms,80.841833ms
phase=latency filter=incoming-owner reps=5 p50=2.664958ms p95=4.1105ms p99=4.1105ms max=5.296625ms submitted=5.296625ms,4.1105ms,2.664958ms,1.724416ms,1.776ms
phase=latency filter=incoming-forward reps=5 p50=77.345875ms p95=77.6185ms p99=77.6185ms max=77.986708ms submitted=73.330833ms,75.428583ms,77.6185ms,77.986708ms,77.345875ms
phase=latency filter=predicate reps=5 p50=2.816375ms p95=5.122958ms p99=5.122958ms max=5.192959ms submitted=5.192959ms,2.669584ms,5.122958ms,2.816375ms,2.284209ms
phase=latency filter=name reps=5 p50=3.738833ms p95=3.783916ms p99=3.783916ms max=3.967958ms submitted=3.967958ms,3.738833ms,2.8415ms,3.783916ms,1.666791ms
phase=latency filter=incoming reps=5 p50=3.71075ms p95=5.279916ms p99=5.279916ms max=6.317458ms submitted=5.279916ms,3.561083ms,6.317458ms,3.71075ms,2.10125ms
phase=concurrent workers=4 operations=15 catch_up=16.763292ms throughput=894.8_ops_per_second queue_high_water=11
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=3 aggregate_after=0 predicate_baseline=0 predicate_high=1 predicate_after=0 name_baseline=0 name_high=2 name_after=0 incoming_baseline=0 incoming_high=2 incoming_after=0
phase=resource workers=4 cpu_before=0.00 cpu_after=55.00 rss_before=35852288 rss_after=45580288 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

## Owner-filter 21k full profile

Passed in **49.95 s** (subtests `workers-4` 20.89 s, `workers-16` 27.53 s); package 52.200 s.

```text
phase=setup profile=full entities=21000 name_context=5000 spread=20 reps=30 workers=[4 16] server=2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66 sdk=v1.52.0
phase=seed rows=47020 elapsed=1.012698208s throughput=46430.4_rows_per_second
phase=seed-complete cpu=87.00 rss=58994688 subscriptions=74 rss_delta=41246720
phase=maxima entity_bytes=256 predicate_bytes=194 key_bytes predicate=451 name=710 incoming=902 outgoing=256
phase=lifecycle cancellation=pass empty=pass clean_recreate=pass
phase=restart fresh_bucket_handles=pass
```

### Workers 4

```text
phase=latency filter=predicate-owner reps=30 p50=2.530292ms p95=2.666083ms p99=3.458208ms max=3.5755ms submitted=2.606625ms,2.532666ms,2.54175ms,2.483708ms,2.480583ms,2.55375ms,2.555334ms,2.426084ms,2.402458ms,2.39125ms,2.587666ms,2.458083ms,2.530292ms,3.458208ms,2.617042ms,2.433875ms,2.666083ms,2.507667ms,2.617542ms,2.54925ms,2.499417ms,2.496667ms,2.443417ms,2.485ms,2.423541ms,2.630541ms,2.578708ms,3.5755ms,2.4925ms,2.527875ms
phase=latency filter=predicate-forward reps=30 p50=265.392958ms p95=269.422541ms p99=269.500791ms max=270.865208ms submitted=261.830583ms,262.757334ms,264.577208ms,266.476292ms,266.153458ms,269.500791ms,267.509167ms,265.294917ms,265.392958ms,265.162667ms,268.37375ms,264.497292ms,266.950625ms,263.314ms,263.830167ms,265.301ms,270.865208ms,267.981375ms,264.245333ms,263.60425ms,255.949875ms,266.108166ms,266.649334ms,265.394709ms,265.50175ms,269.422541ms,264.883625ms,266.222333ms,263.930959ms,263.646291ms
phase=latency filter=name-owner reps=30 p50=902.292µs p95=2.237958ms p99=2.596917ms max=2.691667ms submitted=2.596917ms,2.691667ms,2.237958ms,1.726333ms,1.486917ms,1.456666ms,1.683583ms,1.249791ms,911.375µs,832.833µs,842.084µs,1.532292ms,1.073167ms,902.292µs,928.792µs,934.417µs,846.292µs,824.5µs,804.75µs,959.833µs,771.791µs,775.333µs,766.333µs,850.166µs,847.875µs,821.084µs,810.708µs,881.875µs,814.917µs,798.708µs
phase=latency filter=name-forward reps=30 p50=81.61625ms p95=84.231375ms p99=85.242417ms max=85.711125ms submitted=75.410584ms,78.043667ms,82.35975ms,85.242417ms,81.453625ms,81.38225ms,83.421292ms,81.283625ms,85.711125ms,79.267292ms,80.007333ms,81.141583ms,80.221959ms,80.59525ms,82.51725ms,83.423042ms,75.80875ms,80.422167ms,82.76725ms,83.40575ms,81.61625ms,83.597208ms,81.526459ms,82.644375ms,78.497417ms,78.229875ms,82.025083ms,83.661167ms,84.231375ms,81.960041ms
phase=latency filter=incoming-owner reps=30 p50=3.45825ms p95=4.744208ms p99=7.061125ms max=15.98175ms submitted=15.98175ms,7.061125ms,4.166041ms,3.4055ms,4.455041ms,4.293958ms,3.705833ms,3.925042ms,3.776584ms,4.744208ms,3.571084ms,3.592542ms,3.45825ms,3.36275ms,3.205917ms,3.212042ms,4.18375ms,3.197208ms,3.029416ms,3.027416ms,3.028458ms,3.164625ms,4.003667ms,3.235791ms,3.038ms,3.187958ms,3.091792ms,2.935542ms,4.052667ms,3.117292ms
phase=latency filter=incoming-forward reps=30 p50=312.24ms p95=318.445417ms p99=320.466541ms max=329.643792ms submitted=298.192917ms,311.15975ms,329.643792ms,320.466541ms,316.287041ms,316.71825ms,316.853292ms,306.096042ms,309.279709ms,311.344584ms,302.151375ms,313.579708ms,308.559083ms,312.792625ms,310.390458ms,311.24425ms,309.825375ms,305.980959ms,306.983417ms,316.147042ms,308.250625ms,312.828292ms,314.7885ms,312.24ms,312.534ms,318.445417ms,298.966125ms,314.651167ms,310.475958ms,317.74525ms
phase=latency filter=name reps=30 p50=1.392125ms p95=3.189917ms p99=3.346042ms max=3.71125ms submitted=1.244291ms,1.088542ms,3.71125ms,3.346042ms,1.392125ms,1.488166ms,2.326334ms,1.260125ms,1.767917ms,927.542µs,1.895625ms,1.355125ms,960.916µs,2.099458ms,1.206125ms,1.64325ms,2.505041ms,3.189917ms,843.125µs,1.01775ms,876.791µs,1.520333ms,978µs,1.063125ms,1.612292ms,1.23275ms,1.509375ms,1.936917ms,1.344959ms,1.014208ms
phase=latency filter=incoming reps=30 p50=7.536041ms p95=12.815459ms p99=21.1205ms max=27.730125ms submitted=21.1205ms,10.755583ms,27.730125ms,8.501375ms,5.735791ms,6.881334ms,5.613625ms,3.599084ms,12.815459ms,3.322208ms,8.857917ms,8.267167ms,3.241375ms,3.587458ms,5.593208ms,4.61225ms,7.927583ms,7.819042ms,11.569ms,9.209833ms,8.205542ms,12.077875ms,4.124042ms,7.312834ms,4.42775ms,7.536041ms,6.994042ms,4.763333ms,9.556ms,3.159917ms
phase=latency filter=predicate reps=30 p50=5.66275ms p95=14.545ms p99=15.755584ms max=19.428583ms submitted=19.428583ms,3.355542ms,15.755584ms,6.097625ms,8.690959ms,9.28875ms,6.013334ms,2.774875ms,6.211667ms,2.953625ms,14.545ms,7.901542ms,5.66275ms,6.137833ms,3.119541ms,3.865834ms,7.371125ms,10.83975ms,3.694625ms,2.7565ms,2.82725ms,4.476292ms,5.362125ms,3.632958ms,7.2665ms,5.830792ms,5.39475ms,5.064333ms,5.435583ms,2.725167ms
phase=concurrent workers=4 operations=90 catch_up=124.45225ms throughput=723.2_ops_per_second queue_high_water=86
phase=consumers workers=4 aggregate_baseline=0 aggregate_high=5 aggregate_after=0 predicate_baseline=0 predicate_high=3 predicate_after=0 name_baseline=0 name_high=2 name_after=0 incoming_baseline=0 incoming_high=3 incoming_after=0
phase=resource workers=4 cpu_before=87.00 cpu_after=49.00 rss_before=63696896 rss_after=72425472 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

### Workers 16

```text
phase=latency filter=predicate-owner reps=30 p50=2.795167ms p95=3.540958ms p99=4.170333ms max=4.650125ms submitted=4.650125ms,3.105583ms,3.021458ms,3.540958ms,2.660708ms,3.226709ms,3.336833ms,3.151625ms,4.170333ms,2.982916ms,2.990458ms,2.89325ms,2.806292ms,2.791042ms,2.795167ms,2.730208ms,2.726875ms,2.738542ms,2.633833ms,2.63075ms,2.593375ms,3.057166ms,2.823166ms,2.773625ms,2.654792ms,2.699708ms,2.739542ms,2.643583ms,2.72675ms,2.615375ms
phase=latency filter=predicate-forward reps=30 p50=266.870625ms p95=479.509209ms p99=483.740875ms max=508.696ms submitted=257.435458ms,297.522708ms,255.545875ms,246.991625ms,244.999708ms,240.463417ms,260.731542ms,256.005459ms,239.634375ms,260.116834ms,261.463708ms,248.68575ms,273.921584ms,262.613667ms,365.8985ms,328.078375ms,333.542542ms,286.0925ms,245.261875ms,308.919833ms,236.224041ms,245.290667ms,414.141375ms,266.870625ms,275.505709ms,449.423875ms,406.289167ms,508.696ms,483.740875ms,479.509209ms
phase=latency filter=name-owner reps=30 p50=4.231167ms p95=7.768458ms p99=8.491625ms max=8.934458ms submitted=3.219667ms,2.597167ms,4.394ms,3.22ms,3.782709ms,4.103542ms,4.231167ms,4.381458ms,4.064417ms,3.841792ms,4.141208ms,3.855541ms,4.086792ms,7.54175ms,7.528125ms,5.6875ms,5.632584ms,4.175541ms,5.379667ms,3.887791ms,4.034875ms,3.20825ms,4.688916ms,3.303041ms,5.197958ms,8.934458ms,7.113ms,4.859625ms,8.491625ms,7.768458ms
phase=latency filter=name-forward reps=30 p50=151.065208ms p95=173.078666ms p99=174.461291ms max=176.40575ms submitted=142.133792ms,131.473125ms,148.067292ms,162.040708ms,141.255292ms,167.561917ms,169.938708ms,139.895791ms,130.636125ms,158.544834ms,162.264541ms,157.467125ms,133.162625ms,166.053416ms,176.40575ms,173.078666ms,148.297125ms,174.461291ms,149.126375ms,152.166375ms,133.7015ms,157.0565ms,146.018834ms,144.61025ms,97.877458ms,144.125917ms,164.548291ms,151.611459ms,151.065208ms,140.425542ms
phase=latency filter=incoming-owner reps=30 p50=7.64825ms p95=15.175625ms p99=18.972291ms max=22.057459ms submitted=13.759833ms,22.057459ms,18.972291ms,14.061792ms,4.312042ms,5.180791ms,6.934084ms,7.64825ms,6.004792ms,10.233208ms,7.166375ms,8.589625ms,5.995834ms,7.54025ms,7.9885ms,15.175625ms,5.52175ms,12.505708ms,9.737916ms,14.087209ms,6.059042ms,5.555875ms,5.120958ms,6.465459ms,7.218584ms,10.790167ms,4.1845ms,6.688042ms,9.547292ms,10.391875ms
phase=latency filter=incoming-forward reps=30 p50=328.091834ms p95=580.382709ms p99=591.049875ms max=598.767125ms submitted=554.86275ms,535.640709ms,502.450083ms,575.055542ms,580.382709ms,521.123667ms,458.392792ms,491.498667ms,591.049875ms,598.767125ms,557.03025ms,420.962583ms,399.878291ms,358.846542ms,306.646625ms,305.4685ms,328.091834ms,299.747125ms,309.217125ms,301.078584ms,306.475625ms,299.8275ms,297.118625ms,279.3585ms,307.75525ms,324.759666ms,305.261875ms,312.775833ms,313.156ms,316.291375ms
phase=latency filter=predicate reps=30 p50=18.697875ms p95=30.949541ms p99=30.991125ms max=31.123ms submitted=23.958375ms,30.991125ms,31.123ms,30.949541ms,23.747916ms,7.312333ms,8.535917ms,12.877292ms,13.095917ms,16.852708ms,19.325625ms,22.72525ms,21.777291ms,9.979292ms,11.353875ms,6.722333ms,7.826333ms,19.371ms,3.816209ms,10.860709ms,7.393333ms,19.236417ms,18.697875ms,18.718ms,19.947875ms,15.97625ms,21.863042ms,17.09775ms,20.647916ms,5.553334ms
phase=latency filter=name reps=30 p50=4.879667ms p95=18.209417ms p99=18.259ms max=19.718625ms submitted=18.209417ms,15.004708ms,14.765375ms,15.34575ms,19.718625ms,13.635583ms,13.613916ms,18.259ms,5.013ms,3.618667ms,4.068458ms,2.777875ms,8.215459ms,5.42425ms,5.433334ms,3.807042ms,2.872042ms,5.9195ms,3.162709ms,1.3735ms,2.086042ms,2.216292ms,2.230959ms,2.670208ms,4.879667ms,5.4395ms,1.7455ms,2.071292ms,1.773708ms,3.22375ms
phase=latency filter=incoming reps=30 p50=33.769458ms p95=64.175958ms p99=64.176125ms max=64.629084ms submitted=30.252125ms,48.703959ms,30.059125ms,48.727625ms,64.629084ms,25.224667ms,64.176125ms,64.175958ms,39.77325ms,32.536666ms,35.140916ms,34.231417ms,43.046708ms,42.10275ms,33.769458ms,30.0725ms,34.173041ms,58.859ms,20.802125ms,44.573709ms,5.989667ms,3.60925ms,3.777709ms,17.745417ms,10.369625ms,11.326875ms,41.29575ms,13.972167ms,22.914708ms,25.554042ms
phase=concurrent workers=16 operations=90 catch_up=133.845166ms throughput=672.4_ops_per_second queue_high_water=74
phase=consumers workers=16 aggregate_baseline=0 aggregate_high=14 aggregate_after=0 predicate_baseline=0 predicate_high=6 predicate_after=0 name_baseline=0 name_high=1 name_after=0 incoming_baseline=0 incoming_high=12 incoming_after=0
phase=resource workers=16 cpu_before=49.00 cpu_after=66.00 rss_before=72425472 rss_after=75530240 subscriptions_before=80 subscriptions_after=80 slow_consumers=0
```

## What this record establishes, and what it does not

It establishes condition 4's supervised evidence on the current pin, with the complete per-filter distribution in
submission order, and it is the source for the acceptance tables in the runbook. It does not complete the
component-level replacement, readiness, public-query, or clustering gates in ADR-077, and it is not a CI
expectation: these are quiet-box numbers.

The absolute ceiling on a directly measured key listing is the framework-enforced `natsclient` KV deadline
(`DefaultKVOptions().Timeout`), observed as the operation's own typed error. No per-operation wall-clock budget is
asserted here or in the harness.
