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

- **Owner-filter acceptance record — current.** Re-measured at `b10671ed` on 2026-09-12 against this pin (#1284), with per-repetition
  durations in submission order.
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

## Owner-filter acceptance record — revision `b10671ed`

This is the **supervised record that satisfies ADR-077 § 8 condition 4** under the #1284 amendment, taken at the
revision that ships the submission-order instrument. The **owner-filter** CI profile —
`TestIntegration_OwnerFilterLoadHarness`, not the smoke harness governed by the table in § "Profiles and absolute
gates" — is a regression guard and asserts no per-operation wall-clock budget.

The numbers below come from a quiet supervised box and are the uncontended floor, not a CI expectation. Compare wall
clock at the same level: subtest `workers-4` took **8.64 s on a shared CI runner against 1.42 s here**, and the whole
test **11.02 s against 2.24 s** (run 34367949188). The shared subtest is a floor rather than a completed run — it
aborted at `incoming-forward` repetition 3, having measured 5 of 9 filters — so the real gap exceeds the 6.1x those
two numbers give.

Compare latency like for like too — worst forward filter against worst forward filter, never one filter's p95
against another's. On that basis the tax is **~3.1×**: shared-runner `name-forward` p95 reached **258.039 ms** (run
34367949188) against **83.153 ms** here. Taking `predicate-forward` alone it is ~2.5–2.7× (**157.495 ms** in run
33260659637 and **175.414 ms** in run 33208133273, against **64.090 ms** here; the same filter measured 169.852 ms in
run 34367949188).

It replaces the owner-filter rows previously recorded at `0a7af288` (2026-07-17, `nats:2.12.4-alpine`, SDK `v1.48.0`),
retained as history in [the 0a7af288 pre-tag appendix](evidence/graph-index-pre-tag-0a7af288.md). **The old record's
CONTEXT rows have no counterpart here: that store was retired after `0a7af288`** — itself evidence of how far the old
record had drifted from the harness it claimed to describe.

| Provenance | Recorded value |
|---|---|
| SemStreams revision | `b10671ed` |
| Worktree state | Clean before both commands |
| Run timestamp and timezone | 2026-09-12 09:59:26–10:00:22 CEST |
| Host CPU and memory | Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM |
| Docker allocation | 23,742 MB |
| Docker server / API | 29.7.2; API 1.51; testcontainers-go v0.40.0 (the run log records no CLI version) |
| NATS server and image digest | `nats:2.14.4-alpine` @ `sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` |
| Go SDK | `github.com/nats-io/nats.go v1.52.0` |
| Evidence capture | [In-tree raw phase appendix](evidence/graph-index-owner-load-b10671ed.md), with every filter's per-repetition durations in submission order |

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
| 5k CI | 5,000 | 15,020 | 362.402 | 41,445.7 | PASS |
| 21k full | 21,000 | 47,020 | 1,012.698 | 46,430.4 | PASS |

Each filter is measured in three forms — the bare filter, its `-owner` single-key lookup, and its `-forward` drain
across the full hot-member set. **Every value in this table is milliseconds.** The owner and forward classes differ
by roughly two orders of magnitude, so comparing a value against one from the other class is meaningless. The raw
appendix carries each row's per-repetition durations in submission order; the percentiles here are sorted summaries
of those sequences.

| Profile | Workers | Filter | Reps | p50 (ms) | p95 (ms) | p99 (ms) | Max (ms) | Result |
|---|---:|---|---:|---:|---:|---:|---:|---|
| 5k CI | 4 | `predicate-owner` | 5 | 0.780 | 0.785 | 0.785 | 1.299 | PASS |
| 5k CI | 4 | `predicate-forward` | 5 | 63.429 | 64.090 | 64.090 | 65.950 | PASS |
| 5k CI | 4 | `name-owner` | 5 | 2.851 | 2.951 | 2.951 | 4.253 | PASS |
| 5k CI | 4 | `name-forward` | 5 | 83.036 | 83.153 | 83.153 | 85.520 | PASS |
| 5k CI | 4 | `incoming-owner` | 5 | 2.665 | 4.111 | 4.111 | 5.297 | PASS |
| 5k CI | 4 | `incoming-forward` | 5 | 77.346 | 77.618 | 77.618 | 77.987 | PASS |
| 5k CI | 4 | `predicate` | 5 | 2.816 | 5.123 | 5.123 | 5.193 | PASS |
| 5k CI | 4 | `name` | 5 | 3.739 | 3.784 | 3.784 | 3.968 | PASS |
| 5k CI | 4 | `incoming` | 5 | 3.711 | 5.280 | 5.280 | 6.317 | PASS |
| 21k full | 4 | `predicate-owner` | 30 | 2.530 | 2.666 | 3.458 | 3.575 | PASS |
| 21k full | 4 | `predicate-forward` | 30 | 265.393 | 269.423 | 269.501 | 270.865 | PASS |
| 21k full | 4 | `name-owner` | 30 | 0.902 | 2.238 | 2.597 | 2.692 | PASS |
| 21k full | 4 | `name-forward` | 30 | 81.616 | 84.231 | 85.242 | 85.711 | PASS |
| 21k full | 4 | `incoming-owner` | 30 | 3.458 | 4.744 | 7.061 | 15.982 | PASS |
| 21k full | 4 | `incoming-forward` | 30 | 312.240 | 318.445 | 320.467 | 329.644 | PASS |
| 21k full | 4 | `name` | 30 | 1.392 | 3.190 | 3.346 | 3.711 | PASS |
| 21k full | 4 | `incoming` | 30 | 7.536 | 12.815 | 21.120 | 27.730 | PASS |
| 21k full | 4 | `predicate` | 30 | 5.663 | 14.545 | 15.756 | 19.429 | PASS |
| 21k full | 16 | `predicate-owner` | 30 | 2.795 | 3.541 | 4.170 | 4.650 | PASS |
| 21k full | 16 | `predicate-forward` | 30 | 266.871 | 479.509 | 483.741 | 508.696 | PASS |
| 21k full | 16 | `name-owner` | 30 | 4.231 | 7.768 | 8.492 | 8.934 | PASS |
| 21k full | 16 | `name-forward` | 30 | 151.065 | 173.079 | 174.461 | 176.406 | PASS |
| 21k full | 16 | `incoming-owner` | 30 | 7.648 | 15.176 | 18.972 | 22.057 | PASS |
| 21k full | 16 | `incoming-forward` | 30 | 328.092 | 580.383 | 591.050 | 598.767 | PASS |
| 21k full | 16 | `predicate` | 30 | 18.698 | 30.950 | 30.991 | 31.123 | PASS |
| 21k full | 16 | `name` | 30 | 4.880 | 18.209 | 18.259 | 19.719 | PASS |
| 21k full | 16 | `incoming` | 30 | 33.769 | 64.176 | 64.176 | 64.629 | PASS |

Worst measurement across every row: **p95 580.383 ms** and **p99 591.050 ms** (both `incoming-forward`, 21k/16
workers), **max 598.767 ms** (same row). The full profile's `p95Budget` of 3 s and `p99Budget` of 5 s therefore sit at
**5.2× and 8.5×** over what it measures. That looseness is accepted deliberately (#1284 owner ruling, Q7(b)): the
supervised run has never fired, is not a flake source, and tightening a gate that does not fire can only introduce
one. Re-deriving the budgets for **both** profiles is tracked as **#1287**, once enough submission-order runs exist
to separate a stalled repetition from a shifted distribution.

The absolute ceiling on a directly measured key listing is the framework-enforced `natsclient` KV deadline
(`DefaultKVOptions().Timeout`), observed as the operation's own typed error rather than restated as a predicted
budget.

Record the concurrent phase separately because its catch-up, queue, and consumer evidence is per worker shape, not
per store.

| Profile | Workers | Operations | Catch-up (ms) | Throughput (ops/s) | Queue high-water | Consumers base/high/after | Result |
|---|---:|---:|---:|---:|---:|---|---|
| 5k CI | 4 | 15 | 16.763 | 894.8 | 11 | 0/3/0 | PASS |
| 21k full | 4 | 90 | 124.452 | 723.2 | 86 | 0/5/0 | PASS |
| 21k full | 16 | 90 | 133.845 | 672.4 | 74 | 0/14/0 | PASS |

| Profile | Workers | NATS RSS before/after (bytes) | Subscriptions before/after | Slow consumers | Result |
|---|---:|---|---|---:|---|
| 5k CI | 4 | 35,852,288/45,580,288 | 80/80 | 0 | PASS |
| 21k full | 4 | 63,696,896/72,425,472 | 80/80 | 0 | PASS |
| 21k full | 16 | 72,425,472/75,530,240 | 80/80 | 0 | PASS |


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
