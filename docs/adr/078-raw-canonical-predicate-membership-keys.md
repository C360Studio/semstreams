# ADR-078: Raw Canonical Predicate Membership Keys

## Status

**Accepted (2026-07-17).** The fixed-nine-token raw layout passed the supervised absolute representation gates.
This decision selects raw canonical predicate membership keys. It does not waive the remaining production lifecycle
and activation gates in ADR-077 and the governing OpenSpec changes.

## Context

ADR-065 selected `hash(predicate).entity6` plus PREDICATE_CATALOG when predicates could contain arbitrary bytes and
vary in arity. ADR-074 now requires exactly three bounded, lower-kebab, NATS-safe tokens at every authoritative
write and on replay. The entity-ID contract likewise fixes the other six tokens. Hashing therefore no longer
protects an accepted input, while it permanently requires a second bucket, a recovery join, repair semantics, and
operator-opaque keys.

The announced pre-v1 clean cutover is the only window in which SemStreams can replace this physical representation
without assuming post-v1 compatibility obligations.

## Decision

### 1. PREDICATE_INDEX uses the raw nine-token layout

Each membership key is `predicate3.entity6`, expanded as
`domain.category.property.org.platform.domain.system.type.instance`. It remains one membership per key with O(E)
writes and is owned by the entity under ADR-077.

| Query | Exact nine-token filter |
|---|---|
| Exact predicate | `domain.category.property.*.*.*.*.*.*` |
| Category namespace | `domain.category.*.*.*.*.*.*.*` |
| Domain namespace | `domain.*.*.*.*.*.*.*.*` |
| Entity owner | `*.*.*.org.platform.domain.system.type.instance` |

PREDICATE_CATALOG is retired. Predicate identity is recoverable from the first three tokens, so production must not
create, repair, join, or read a catalog after cutover.

### 2. The cutover is clean and deployment-scoped

There is no runtime layout flag, dual reader or writer, mixed-format mode, old-key compatibility path, export,
record-by-record migration, or rollback. During the combined pre-v1 cutover, operators stop writers, resolve the
deployment's configured derived-bucket names, remove the old PREDICATE_INDEX and PREDICATE_CATALOG state, create a
fresh raw PREDICATE_INDEX, and rebuild it from canonical ENTITY_STATES behind typed not-ready responses. This is
not permission to delete unrelated resources or to wildcard a shared NATS account.

If the combined pre-v1 wipe/reseed window closes before activation, this decision does not authorize a second wipe.
The work must halt and return as an explicit post-v1 migration proposal.

### 3. Absolute representation gates passed

The supervised runs used `nats:2.12.4-alpine` at
`sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea` and
`github.com/nats-io/nats.go v1.48.0`. A change to either pin requires the maximum, correctness, latency, churn, and
resource evidence to be rerun before release.

The 5,000-member CI profile passed its 3-second per-operation budget, exact match-set assertions, restart parity,
churn convergence, and temporary-consumer return-to-baseline checks.

The supervised 21,000-entity raw run recorded:

| Evidence | Raw result |
|---|---:|
| Seed throughput | 83,557 rows/s |
| Maximum membership key | 451 bytes |
| Exact predicate | p95 31.920 ms; p99 47.825 ms |
| Entity owner | p95 2.465 ms |
| Maximum-length entity owner | p95 1.154 ms |
| Category namespace | p95 29.474 ms |
| Domain namespace | p95 27.705 ms |
| Exact predicate under churn | p95 31.245 ms |
| Churn convergence | 2,000 mutations; exact final set |
| NATS RSS | 18.2 MB to 46.9 MB |
| NATS subscriptions | 68 to 68 |
| Slow consumers | 0 |
| Membership consumers | baseline/high-water/after = 0/1/0 |
| Catalog consumers | baseline/high-water/after = 0/0/0 |

All raw measurements passed the absolute 3-second p95, 5-second p99, 10-second per-operation, correctness, and
resource-leak gates. Hash-plus-catalog results from the companion run are comparative evidence only. They are not
selection thresholds and do not weaken or reverse the raw decision.

The executable evidence and reproduction commands are maintained in the
[Predicate Layout Evidence Runbook](../operations/32-predicate-layout-smoke-harness.md).

### 4. Other codecs do not change

NAME and CONTEXT retain hashing for their open-content axes. NAME, CONTEXT, and INCOMING retain reversible
`hex(predicate)` tokens because fixed arity is useful and no query need justifies re-keying those stores. Encoding
never replaces canonical predicate validation.

## Consequences

- Exact, category, domain, and entity-owner discovery operate directly on one bounded membership bucket.
- PREDICATE_CATALOG consistency, write amplification, repair, and query joins disappear after cutover.
- Storage is intentionally coupled to the canonical predicate grammar; changing that grammar is a breaking change.
- Production activation remains fail-closed on ADR-077 replacement, readiness, restart, query, and clustering gates.
- A new server or SDK pin cannot inherit this evidence silently.

## Supersession and References

- Supersedes ADR-065 only for predicate hashing, PREDICATE_CATALOG, and their query/repair consequences. ADR-065's
  one-membership-per-key sharding and absolute operating budgets remain in force.
- Completes the physical-representation decision left open by ADR-077.
- Implements the decision in
  [`predicate-raw-key-representation`](../../openspec/changes/predicate-raw-key-representation/proposal.md).
- Reuses the ownership, replacement, and activation boundaries in
  [`graph-index-replacement-semantics`](../../openspec/changes/graph-index-replacement-semantics/proposal.md).
