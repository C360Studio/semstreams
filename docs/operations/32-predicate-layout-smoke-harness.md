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
