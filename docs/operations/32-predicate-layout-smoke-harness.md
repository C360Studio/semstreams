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

The CI profile passed all absolute checks. Its purpose is regression detection, not a source for comparative layout
selection.

## Supervised 21k raw result

| Phase or operation | Recorded result |
|---|---:|
| Seed throughput | 83,557 rows/s |
| Maximum membership key | 451 bytes |
| Exact predicate | p95 31.920 ms; p99 47.825 ms |
| Entity owner | p95 2.465 ms |
| Maximum-length entity owner | p95 1.154 ms |
| Category namespace | p95 29.474 ms |
| Domain namespace | p95 27.705 ms |
| Exact predicate under churn | p95 31.245 ms |
| Churn completion | 2,000 mutations; exact convergence |

Resource evidence:

| Signal | Before | High-water | After |
|---|---:|---:|---:|
| NATS RSS | 18.2 MB | — | 46.9 MB |
| NATS subscriptions | 68 | — | 68 |
| Slow consumers | 0 | — | 0 |
| Membership consumers | 0 | 1 | 0 |
| Catalog consumers | 0 | 0 | 0 |

The raw layout passed every absolute representation gate. Hash-plus-catalog figures emitted by the companion run
are comparison evidence only; no raw-versus-hash ratio or delta is a selection threshold.

## Pending pre-tag owner-filter acceptance record

The tables below are the required acceptance shape for the follow-up revision. Do not fill them from an uncommitted
worktree or from an earlier decision run. Record the clean revision before executing either profile, and keep task
`graph-index-replacement-semantics` 2.2 open until every row passes.

| Provenance | Required value |
|---|---|
| SemStreams revision | Pending follow-up commit SHA |
| Worktree state | Clean before both commands |
| Run timestamp and timezone | Pending |
| Host CPU and memory | Pending |
| Docker client/server | Pending |
| NATS server and image digest | `nats:2.12.4-alpine` and the pinned digest above |
| Go SDK | `github.com/nats-io/nats.go v1.48.0` |

Owner-filter latency rows use the exact output from `TestIntegration_OwnerFilterLoadHarness`. The CI shape is
5,000 entities at four workers. The full shape is 21,000 entities at both the configured four-worker shape and the
selected maximum of 16 workers. OUTGOING uses its exact entity key rather than a filtered lister, so its maximum
proof appears in the maximum-key table rather than the per-worker latency table.

| Store | Maximum key bytes | Owner discovery | Exact real-NATS match | Result |
|---|---:|---|---|---|
| PREDICATE | 451 | `*.*.*.entity6` | Pending | Pending |
| NAME | 710 | `*.entity6.*` | Pending | Pending |
| INCOMING | 902 | `*.*.*.*.*.*.source6.*` | Pending | Pending |
| CONTEXT | 710 | `entity6.*.*` | Pending | Pending |
| OUTGOING | 256 | Exact `entity6` key | Pending | Pending |

The owner harness emits one `phase=seed` record before exercising either worker shape. Preserve it as the ingest
baseline for task 2.2; it is distinct from the raw-versus-hash predicate candidate seed measurement below.

| Profile | Entities | Seed rows | Elapsed | Throughput | Result |
|---|---:|---:|---:|---:|---|
| 5k CI | 5,000 | Pending | Pending | Pending | Pending |
| 21k full | 21,000 | Pending | Pending | Pending | Pending |

| Profile | Workers | Store | p50 | p95 | p99 | Max | Result |
|---|---:|---|---:|---:|---:|---:|---|
| 5k CI | 4 | PREDICATE | Pending | Pending | Pending | Pending | Pending |
| 5k CI | 4 | NAME | Pending | Pending | Pending | Pending | Pending |
| 5k CI | 4 | INCOMING | Pending | Pending | Pending | Pending | Pending |
| 5k CI | 4 | CONTEXT | Pending | Pending | Pending | Pending | Pending |
| 21k full | 4 | PREDICATE | Pending | Pending | Pending | Pending | Pending |
| 21k full | 4 | NAME | Pending | Pending | Pending | Pending | Pending |
| 21k full | 4 | INCOMING | Pending | Pending | Pending | Pending | Pending |
| 21k full | 4 | CONTEXT | Pending | Pending | Pending | Pending | Pending |
| 21k full | 16 | PREDICATE | Pending | Pending | Pending | Pending | Pending |
| 21k full | 16 | NAME | Pending | Pending | Pending | Pending | Pending |
| 21k full | 16 | INCOMING | Pending | Pending | Pending | Pending | Pending |
| 21k full | 16 | CONTEXT | Pending | Pending | Pending | Pending | Pending |

Record the concurrent phase separately because its catch-up, queue, and consumer evidence is per worker shape, not
per store.

| Profile | Workers | Operations | Catch-up | Throughput | Queue high-water | Consumers base/high/after | Result |
|---|---:|---:|---:|---:|---:|---|---|
| 5k CI | 4 | Pending | Pending | Pending | Pending | Pending | Pending |
| 21k full | 4 | Pending | Pending | Pending | Pending | Pending | Pending |
| 21k full | 16 | Pending | Pending | Pending | Pending | Pending | Pending |

| Profile | Workers | NATS RSS before/after | Subscriptions before/after | Slow consumers | Result |
|---|---:|---|---|---:|---|
| 5k CI | 4 | Pending | Pending | Pending | Pending |
| 21k full | 4 | Pending | Pending | Pending | Pending |
| 21k full | 16 | Pending | Pending | Pending | Pending |

## Pending pre-tag predicate comparison

Run `TestIntegration_PredicateLayoutSmoke` on the same clean follow-up revision. These values are descriptive
comparison evidence only; each candidate is evaluated against the absolute budget.

| Operation | Hash plus catalog | Raw nine-token | Notes |
|---|---:|---:|---|
| Seed throughput | Pending | Pending | Include catalog rows for hash. |
| Maximum key bytes | Pending | Pending | Expected 321 and 451. |
| Exact predicate p95/p99 | Pending | Pending | Same 21,000-member result set. |
| Entity owner p95/p99 | Pending | Pending | One expected member. |
| Maximum owner p95/p99 | Pending | Pending | Maximum canonical entity ID. |
| Namespace p95/p99 | Pending | Pending | Catalog join versus raw category/domain filters. |
| Exact under churn p95/p99 | Pending | Pending | Four writers, 2,000 mutations. |
| Membership consumers | Pending | Pending | Baseline/high-water/after. |
| Catalog consumers | Pending | Pending | Hash only; raw must remain 0/0/0. |
| NATS RSS | Pending | Pending | Before/after bytes. |

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
