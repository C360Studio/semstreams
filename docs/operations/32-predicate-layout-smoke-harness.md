# Predicate Layout Evidence and Reproduction Runbook

**Historical cutover evidence — not an active release procedure.**

This document records the 2026 cutover plan. Stable release adoption starts on newly provisioned NATS storage; do not
execute this body as a release gate. Its no-shim conclusions remain evidence. Typed graph-poison recovery is governed
by [operations 17](17-predicate-cutover-clean-wipe.md) and [operations 33](33-graph-poison-response-runbook.md).

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
| NATS server | `nats:2.14.4-alpine` |
| NATS image digest | `sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` |
| Go SDK | `github.com/nats-io/nats.go v1.52.0` |

Changing the server digest or SDK invalidates inherited conformance and performance evidence. A predicate grammar,
entity-ID bound, filter implementation, storage layout, or test profile change requires the affected gates to be
rerun as well.

**Pin moved 2026-07-31 (gh#790), and the clause above was honoured rather than waived.** The repo had three
NATS regimes at once — `2.10-alpine`, `2.12-alpine`, and an unpinned `nats:latest` in CI — so the convergence
onto `2.14.4-alpine` necessarily moved this pin. Both affected gates were **re-run against the new pin before it
was written here**, and both passed:

| Gate | Result on `nats:2.14.4-alpine` + `nats.go v1.52.0` |
|---|---|
| `TestIntegration_OwnerFilterLoadHarness` (workers-4) | PASS, 2.46s |
| `TestIntegration_PredicateLayoutSmoke` (hash-catalog, raw-nine-token) | PASS, 6.18s |

Conformance evidence is therefore re-established, not inherited across the version change. Which performance rows
below are current-pin evidence now differs by section, so check before citing one:

- **Owner-filter acceptance record — current.** Re-measured at `60c79736` on 2026-09-11 against this pin (#1284).
- **Pre-tag predicate comparison — historical.** Still the `2.12.4-alpine` measurements; a truthful record of what
  was measured then, not claimed as current-pin evidence. Re-measure before citing a latency budget from it.

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

## Owner-filter acceptance record — revision `60c79736`

This is the **supervised record that satisfies ADR-077 § 8 condition 4** under the #1284 amendment. The
**owner-filter** CI profile — `TestIntegration_OwnerFilterLoadHarness`, not the smoke harness governed by the table
in § "Profiles and absolute gates" — is a regression guard and asserts no per-operation wall-clock budget. The
numbers below come from a quiet supervised box and are the uncontended floor, not a CI expectation. Compare wall
clock at the same level: subtest `workers-4` took **8.64 s on a shared CI runner against 1.36 s here**, and the whole
test **11.02 s against 2.12 s** (run 34367949188). The shared subtest is a floor rather than a completed run — it
aborted at `incoming-forward` repetition 3, having measured 5 of 9 filters — so the real gap is wider than the 6.35x
those two numbers give.

Compare contention like for like — worst forward filter against worst forward filter, never one filter's p95 against
another's. On that basis the tax is **~3.3×**: shared-runner `name-forward` p95 reached **258.039 ms** (run
34367949188) against **77.861 ms** here. Taking `predicate-forward` alone it is ~2.5–2.8× (**157.495 ms** in run
33260659637 and **175.414 ms** in run 33208133273, against **62.481 ms** here; the same filter measured 169.852 ms in
run 34367949188).

It replaces the owner-filter rows previously recorded at `0a7af288` (2026-07-17, `nats:2.12.4-alpine`, SDK `v1.48.0`),
which are retained as history in
[the 0a7af288 pre-tag appendix](evidence/graph-index-pre-tag-0a7af288.md). **The old record's CONTEXT rows have no
counterpart here: that store was retired after `0a7af288`** — which is itself evidence of how far the old record had
drifted from the harness it claimed to describe.

| Provenance | Recorded value |
|---|---|
| SemStreams revision | `60c79736` |
| Worktree state | Clean before both commands |
| Run timestamp and timezone | 2026-09-11 15:44:56–15:46:50 CEST |
| Host CPU and memory | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,742 MB |
| Docker server / API | 29.7.2; API 1.51; testcontainers-go v0.40.0 (the run log records no CLI version) |
| NATS server and image digest | `nats:2.14.4-alpine` @ `sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` |
| Go SDK | `github.com/nats-io/nats.go v1.52.0` |
| Evidence capture | [In-tree raw phase appendix](evidence/graph-index-owner-load-60c79736.md) |

Latency rows use the exact output from `TestIntegration_OwnerFilterLoadHarness`. The CI shape is 5,000 entities at
four workers; the full shape is 21,000 entities at both the configured four-worker shape and the selected maximum of
16 workers. OUTGOING uses its exact entity key rather than a filtered lister, so its maximum proof appears in the
maximum-key table rather than the latency table.

| Store | Maximum key bytes | Owner discovery | Exact real-NATS match | Result |
|---|---:|---|---|---|
| PREDICATE | 451 | `*.*.*.entity6` | Exact one-row match | PASS |
| NAME | 710 | `*.entity6.*` | Exact one-row match | PASS |
| INCOMING | 902 | `*.*.*.*.*.*.source6.*` | Exact one-row match | PASS |
| OUTGOING | 256 | Exact `entity6` key | Put/Get value parity | PASS |

The harness emits one `phase=seed` record before exercising either worker shape.

| Profile | Entities | Seed rows | Elapsed (ms) | Throughput (rows/s) | Result |
|---|---:|---:|---:|---:|---|
| 5k CI | 5,000 | 15,020 | 347.566 | 43,214.9 | PASS |
| 21k full | 21,000 | 47,020 | 1,005.461 | 46,764.6 | PASS |

Each filter is measured in three forms — the bare filter, its `-owner` single-key lookup, and its `-forward` drain
across the full hot-member set. **Every value in this table is milliseconds.** The owner and forward classes differ
by roughly two orders of magnitude, so comparing a value against one from the other class is meaningless.

| Profile | Workers | Filter | Reps | p50 (ms) | p95 (ms) | p99 (ms) | Max (ms) | Result |
|---|---:|---|---:|---:|---:|---:|---:|---|
| 5k CI | 4 | `predicate-owner` | 5 | 0.723 | 0.772 | 0.772 | 0.854 | PASS |
| 5k CI | 4 | `predicate-forward` | 5 | 61.935 | 62.481 | 62.481 | 63.942 | PASS |
| 5k CI | 4 | `name-owner` | 5 | 1.798 | 2.908 | 2.908 | 3.330 | PASS |
| 5k CI | 4 | `name-forward` | 5 | 77.363 | 77.861 | 77.861 | 80.068 | PASS |
| 5k CI | 4 | `incoming-owner` | 5 | 1.919 | 2.512 | 2.512 | 2.801 | PASS |
| 5k CI | 4 | `incoming-forward` | 5 | 73.378 | 74.622 | 74.622 | 74.958 | PASS |
| 5k CI | 4 | `incoming` | 5 | 2.384 | 2.793 | 2.793 | 3.952 | PASS |
| 5k CI | 4 | `predicate` | 5 | 3.311 | 3.341 | 3.341 | 4.360 | PASS |
| 5k CI | 4 | `name` | 5 | 2.717 | 4.397 | 4.397 | 4.816 | PASS |
| 21k full | 4 | `predicate-owner` | 30 | 2.393 | 2.771 | 3.261 | 3.418 | PASS |
| 21k full | 4 | `predicate-forward` | 30 | 262.270 | 265.531 | 265.935 | 291.338 | PASS |
| 21k full | 4 | `name-owner` | 30 | 0.877 | 2.037 | 2.130 | 2.574 | PASS |
| 21k full | 4 | `name-forward` | 30 | 77.967 | 81.490 | 81.942 | 83.002 | PASS |
| 21k full | 4 | `incoming-owner` | 30 | 3.408 | 5.342 | 6.588 | 14.494 | PASS |
| 21k full | 4 | `incoming-forward` | 30 | 305.529 | 310.303 | 310.509 | 313.098 | PASS |
| 21k full | 4 | `incoming` | 30 | 7.892 | 14.181 | 15.653 | 18.780 | PASS |
| 21k full | 4 | `predicate` | 30 | 4.891 | 9.245 | 17.378 | 19.600 | PASS |
| 21k full | 4 | `name` | 30 | 1.226 | 1.721 | 1.798 | 1.892 | PASS |
| 21k full | 16 | `predicate-owner` | 30 | 3.076 | 5.647 | 5.815 | 6.012 | PASS |
| 21k full | 16 | `predicate-forward` | 30 | 261.957 | 284.592 | 320.157 | 396.719 | PASS |
| 21k full | 16 | `name-owner` | 30 | 0.876 | 1.839 | 2.746 | 2.750 | PASS |
| 21k full | 16 | `name-forward` | 30 | 78.166 | 81.476 | 81.975 | 82.376 | PASS |
| 21k full | 16 | `incoming-owner` | 30 | 3.624 | 5.073 | 8.167 | 14.274 | PASS |
| 21k full | 16 | `incoming-forward` | 30 | 304.683 | 311.449 | 316.090 | 320.545 | PASS |
| 21k full | 16 | `name` | 30 | 3.740 | 16.375 | 16.699 | 17.396 | PASS |
| 21k full | 16 | `predicate` | 30 | 14.998 | 26.783 | 26.836 | 29.631 | PASS |
| 21k full | 16 | `incoming` | 30 | 27.481 | 55.978 | 56.782 | 58.916 | PASS |

Worst measurement across every row: **p95 311.449 ms** (`incoming-forward`, 21k/16 workers), **p99 320.157 ms** and
**max 396.719 ms** (both `predicate-forward`, 21k/16 workers). The full profile's
`p95Budget` of 3 s and `p99Budget` of 5 s therefore sit at **9.6× and 15.6×** over what it measures. That looseness
is accepted deliberately (#1284 owner ruling, Q7(b)): the supervised run has never fired, is not a flake source, and
tightening a gate that does not fire can only introduce one. Re-deriving the budgets for both profiles is tracked as
**#1287**, once the harness's per-repetition recording yields within-filter adjacency data.

The absolute ceiling on a directly measured key listing is the framework-enforced `natsclient` KV deadline
(`DefaultKVOptions().Timeout`), observed as the operation's own typed error rather than restated as a predicted
budget.

Record the concurrent phase separately because its catch-up, queue, and consumer evidence is per worker shape, not
per store.

| Profile | Workers | Operations | Catch-up (ms) | Throughput (ops/s) | Queue high-water | Consumers base/high/after | Result |
|---|---:|---:|---:|---:|---:|---|---|
| 5k CI | 4 | 15 | 14.283 | 1,050.2 | 11 | 0/2/0 | PASS |
| 21k full | 4 | 90 | 123.471 | 728.9 | 86 | 0/4/0 | PASS |
| 21k full | 16 | 90 | 122.907 | 732.3 | 74 | 0/13/0 | PASS |

| Profile | Workers | NATS RSS before/after (bytes) | Subscriptions before/after | Slow consumers | Result |
|---|---:|---|---|---:|---|
| 5k CI | 4 | 35,860,480/47,923,200 | 80/80 | 0 | PASS |
| 21k full | 4 | 61,599,744/74,203,136 | 80/80 | 0 | PASS |
| 21k full | 16 | 74,203,136/74,637,312 | 80/80 | 0 | PASS |

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
