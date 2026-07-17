# Predicate Layout Evidence and Reproduction Runbook

**Status:** Executable integration harness; supervised 5k and 21k decision runs recorded 2026-07-17.

[ADR-078](../adr/078-raw-canonical-predicate-membership-keys.md) selects the fixed-nine-token raw
`predicate3.entity6` layout and retires PREDICATE_CATALOG.
[ADR-077](../adr/077-bounded-owner-discovery-and-incoming-ownership.md) defines owner discovery, replacement,
readiness, and the remaining production activation gates. The governing OpenSpec change is
[`predicate-raw-key-representation`](../../openspec/changes/predicate-raw-key-representation/proposal.md).

The executable source is `processor/graph-index/predicate_layout_smoke_integration_test.go`. Keep the proof in that
test; do not copy its codecs, workload generator, percentile calculation, or resource assertions into this runbook.

## Pinned evidence environment

| Dependency | Decision pin |
|---|---|
| NATS server | `nats:2.12.4-alpine` |
| NATS image digest | `sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea` |
| Go SDK | `github.com/nats-io/nats.go v1.48.0` |

Changing the server digest or SDK invalidates inherited conformance and performance evidence. A predicate grammar,
entity-ID bound, filter implementation, storage layout, or test profile change requires the affected gates to be
rerun as well.

## Reproduction commands

Run the CI profile:

```bash
go test -race -tags=integration ./processor/graph-index \
  -run '^TestIntegration_PredicateLayoutSmoke$' -count=1 -v
```

Run the supervised decision profile:

```bash
PREDICATE_SMOKE_FULL=1 go test -tags=integration ./processor/graph-index \
  -run '^TestIntegration_PredicateLayoutSmoke$' -count=1 -timeout=25m -v
```

Archive the complete verbose log with the repository commit, host CPU/memory, Docker version, and run timestamp.
Do not promote output from a dirty or different revision as release evidence.

## Profiles and absolute gates

| Profile | Hot members | Spread predicates | Churn | Repetitions | Absolute gates |
|---|---:|---:|---:|---:|---|
| CI | 5,000 | 20 | 2 writers x 100 | 5 | every operation <3s; p95/p99 <=3s |
| Decision | 21,000 | 20 | 4 writers x 500 | 30 | every operation <10s; p95 <=3s; p99 <=5s |

Both profiles require exact match sets, the 451-byte maximum raw key, exact final convergence after churn, fresh
bucket-handle restart parity, zero slow consumers, bounded NATS RSS, and temporary filtered consumers returning to
their baseline. Silence or an omitted resource scrape is a failed evidence run.

The CI profile is a regression guard, not a source for comparative layout selection. The revision-pinned acceptance
results follow.

## Pre-tag owner-filter acceptance record

Both owner-filter profiles ran from the clean follow-up revision. Every row passed its absolute budget and exact
match-set assertion.

| Provenance | Required value |
|---|---|
| SemStreams revision | `0a7af2889a7ca3010924832e6c8c5d7896e2aa97` |
| Worktree state | Clean before both commands |
| Run timestamp and timezone | 2026-07-17 15:56:10–15:58:42 CDT |
| Host CPU and memory | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,744 MB |
| Docker client/server | 29.6.1/29.6.1; testcontainers API 1.51; CLI API 1.55 |
| NATS server and image digest | `nats:2.12.4-alpine` and the pinned digest above |
| Go SDK | `github.com/nats-io/nats.go v1.48.0` |
| Evidence capture | [In-tree raw phase appendix](evidence/graph-index-pre-tag-0a7af288.md) |

Owner-filter latency rows use the exact output from `TestIntegration_OwnerFilterLoadHarness`. The CI shape is
5,000 entities at four workers. The full shape is 21,000 entities at both the configured four-worker shape and the
selected maximum of 16 workers. OUTGOING uses its exact entity key rather than a filtered lister, so its maximum
proof appears in the maximum-key table rather than the per-worker latency table.

| Store | Maximum key bytes | Owner discovery | Exact real-NATS match | Result |
|---|---:|---|---|---|
| PREDICATE | 451 | `*.*.*.entity6` | Exact one-row match | PASS |
| NAME | 710 | `*.entity6.*` | Exact one-row match | PASS |
| INCOMING | 902 | `*.*.*.*.*.*.source6.*` | Exact one-row match | PASS |
| CONTEXT | 710 | `entity6.*.*` | Exact one-row match | PASS |
| OUTGOING | 256 | Exact `entity6` key | Put/Get value parity | PASS |

The owner harness emits one `phase=seed` record before exercising either worker shape. Preserve it as the ingest
baseline for task 2.2; it is distinct from the raw-versus-hash predicate candidate seed measurement below.

| Profile | Entities | Seed rows | Elapsed | Throughput | Result |
|---|---:|---:|---:|---:|---|
| 5k CI | 5,000 | 20,020 | 515.482834 ms | 38,837.4 rows/s | PASS |
| 21k full | 21,000 | 52,020 | 1.184951042 s | 43,900.5 rows/s | PASS |

| Profile | Workers | Store | p50 | p95 | p99 | Max | Result |
|---|---:|---|---:|---:|---:|---:|---|
| 5k CI | 4 | PREDICATE | 1.857667 | 2.664542 | 2.664542 | 3.100250 | PASS |
| 5k CI | 4 | NAME | 1.732667 | 1.789667 | 1.789667 | 1.802125 | PASS |
| 5k CI | 4 | INCOMING | 2.351834 | 2.376125 | 2.376125 | 2.958458 | PASS |
| 5k CI | 4 | CONTEXT | 1.835041 | 1.879583 | 1.879583 | 1.975709 | PASS |
| 21k full | 4 | PREDICATE | 4.328875 | 9.863375 | 11.532875 | 13.068250 | PASS |
| 21k full | 4 | NAME | 1.212792 | 2.099708 | 2.680291 | 2.935375 | PASS |
| 21k full | 4 | INCOMING | 7.027291 | 12.106750 | 16.291458 | 19.293542 | PASS |
| 21k full | 4 | CONTEXT | 1.402959 | 2.484541 | 2.493125 | 2.736333 | PASS |
| 21k full | 16 | PREDICATE | 10.855917 | 19.568375 | 23.123792 | 23.422042 | PASS |
| 21k full | 16 | NAME | 3.971959 | 7.266209 | 7.294167 | 7.694000 | PASS |
| 21k full | 16 | INCOMING | 33.343792 | 57.322959 | 66.022958 | 69.496584 | PASS |
| 21k full | 16 | CONTEXT | 3.941916 | 7.602083 | 7.732625 | 7.779083 | PASS |

All latency values above are milliseconds. The full owner-filter harness passed in 45.764 seconds.

Record the concurrent phase separately because its catch-up, queue, and consumer evidence is per worker shape, not
per store.

| Profile | Workers | Operations | Catch-up | Throughput | Queue high-water | Consumers base/high/after | Result |
|---|---:|---:|---:|---:|---:|---|---|
| 5k CI | 4 | 20 | 10.453292 ms | 1,913.3 ops/s | 16 | 0/3/0 | PASS |
| 21k full | 4 | 120 | 122.427167 ms | 980.2 ops/s | 116 | 0/6/0 | PASS |
| 21k full | 16 | 120 | 114.719333 ms | 1,046.0 ops/s | 104 | 0/14/0 | PASS |

| Profile | Workers | NATS RSS before/after | Subscriptions before/after | Slow consumers | Result |
|---|---:|---|---|---:|---|
| 5k CI | 4 | 43,667,456/65,789,952 | 83/83 | 0 | PASS |
| 21k full | 4 | 76,013,568/93,159,424 | 83/83 | 0 | PASS |
| 21k full | 16 | 93,159,424/92,635,136 | 83/83 | 0 | PASS |

## Pre-tag predicate comparison

`TestIntegration_PredicateLayoutSmoke` ran on the same clean follow-up revision and passed in 75.623 seconds. These
values are descriptive comparison evidence only; each candidate was evaluated against the absolute budget.

| Candidate | Membership/catalog rows | Seed elapsed | Throughput | Maximum key | Result |
|---|---:|---:|---:|---:|---|
| Hash plus catalog | 21,021/22 | 500.248416 ms | 42,065.1 rows/s | 321 bytes | PASS |
| Raw nine-token | 21,021/0 | 462.533333 ms | 45,447.5 rows/s | 451 bytes | PASS |

| Candidate | Operation | p95 ms | p99 ms |
|---|---|---:|---:|
| Hash plus catalog | Exact predicate | 316.419750 | 323.088708 |
| Raw nine-token | Exact predicate | 268.287333 | 269.328542 |
| Hash plus catalog | Entity owner | 5.820750 | 17.336167 |
| Raw nine-token | Entity owner | 7.901000 | 9.793208 |
| Hash plus catalog | Maximum owner | 1.581375 | 1.706167 |
| Raw nine-token | Maximum owner | 1.736042 | 2.261292 |
| Hash plus catalog | Namespace catalog join | 333.641500 | 336.952084 |
| Raw nine-token | Category namespace | 274.753333 | 286.406875 |
| Raw nine-token | Domain namespace | 278.815417 | 292.620875 |
| Hash plus catalog | Exact under churn | 307.110334 | 321.958125 |
| Raw nine-token | Exact under churn | 270.770500 | 284.744875 |

| Candidate | Membership consumers | Catalog consumers | NATS RSS before/after | Slow consumers |
|---|---|---|---|---:|
| Hash plus catalog | 0/1/0 | 0/1/0 | 16,482,304/43,134,976 | 0 |
| Raw nine-token | 0/1/0 | 0/0/0 | 18,055,168/51,933,184 | 0 |

Both candidates converged exactly after four writers and 2,000 mutations, released temporary consumers to their
baselines, and passed fresh-bucket-handle restart parity. The raw layout remains selected by ADR-078; no comparison
ratio is an acceptance threshold.

This measured owner-discovery matrix is the bounded mechanism and resource input for the
[graph-retention epic (gh#527)](https://github.com/C360Studio/semstreams/issues/527). It does not select retention,
TTL, cascade, or global GC policy.

## Interpreting a run

A green smoke run proves the bounded physical key/filter usage, real-NATS match sets, the declared operating
profile, restart parity, churn convergence, and resource cleanup. It does not by itself activate production.
Activation still requires ADR-077's component-level `[A] -> [B] -> []`, readiness watermark, repair, shuffled
replay, affected public-query, clustering, and deployment cutover gates.

If a rerun fails an absolute gate, stop activation and preserve the full log. Do not silently restore hash-plus-
catalog, weaken the budget, add a dual-format mode, or start a second wipe. Any post-window change requires a new
migration proposal.

## Clean cutover boundary

The selected layout has no dual reader/writer, compatibility reader, mixed-format mode, export, in-place migration,
or rollback. In the combined pre-v1 maintenance window, stop writers, resolve the deployment's configured bucket
names, remove the old derived PREDICATE_INDEX and PREDICATE_CATALOG state, create a fresh raw PREDICATE_INDEX, and
rebuild from canonical ENTITY_STATES behind typed not-ready responses. Never use a copied default bucket list or a
wildcard deletion against a shared NATS account.
