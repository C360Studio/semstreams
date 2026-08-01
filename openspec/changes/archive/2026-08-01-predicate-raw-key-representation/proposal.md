## Why

PREDICATE_INDEX keys hash the predicate (`hash(predicate).entity6`) and recover names through a second
PREDICATE_CATALOG bucket. That design was a defense against the pre-contract era in which graph-ingest accepted any
non-empty predicate: arbitrary bytes were unsafe as NATS tokens and variable arity broke fixed-position parsing.
PR #532 killed that rationale — predicates are now exactly three lower-kebab, subject-safe tokens, enforced
fail-closed at every authoritative write and revalidated on replay — and the `entity-id-contract` bounds the other
key axis. The recorded intent of the predicate contract was precisely this payoff: raw structured keys and direct
NATS wildcard queries, retiring the hash indirection and its catalog appendage.

The hash's remaining costs are permanent: a second bucket, catalog-consistency repair semantics, a join for every
human-readable or namespace lookup, and operator-opaque keys. The pre-v1 clean wipe/reseed — already mandated by
the contract changes and consumed by `graph-index-replacement-semantics` — is the one window in which changing the
physical layout is nearly free. After v1 it becomes a migration with compatibility obligations SemStreams has
declared it will not provide.

The absolute representation gates passed, so this change adopts raw keys. The accepted decision and pinned evidence
are recorded in [ADR-078](../../../docs/adr/078-raw-canonical-predicate-membership-keys.md). Hash-plus-catalog
measurements remain comparison evidence, not a threshold tournament whose null hypothesis is the legacy layout.

## What Changes

- Adopt the fixed-nine-token PREDICATE_INDEX layout `domain.category.property.org.platform.domain.system.type.instance`
  and retire PREDICATE_CATALOG after cutover.
- Namespace and exact-predicate queries become direct fixed-arity filters (`domain.*.*.…`, `domain.category.*.…`,
  exact `predicate3.*.*.*.*.*.*`); the owner filter is `*.*.*.entity6`.
- The 451-byte maximum and every filter passed the shared budgets and pinned real-NATS conformance. The 5k CI guard
  and supervised 21k raw run passed absolute latency, convergence, restart, and resource gates; the exact results
  and reproduction commands are in
  [operations/32](../../../docs/operations/32-predicate-layout-smoke-harness.md).
- The hash-plus-catalog companion run is retained only as comparative evidence. It is not a selection threshold.
- Cutover rides the SAME announced pre-v1 wipe/reseed as `graph-index-replacement-semantics` activation: fresh raw
  buckets initialize behind typed not-ready responses from reseeded canonical ENTITY_STATES. No dual format, no
  old-format reader, no migration, no rollback.
- NAME/CONTEXT keep `hash(name)`/`hash(context)` (open product content — still motivated) and NAME/CONTEXT/INCOMING
  keep the reversible `hex(predicate)` single-token codec (fixed arity without re-keying three stores); both
  decisions are recorded with their rationale and revisited only on demonstrated query or operational need.
- Supersede the affected ADR-065 clauses with ADR-078.

**BREAKING:** pre-v1 clean cutover only. **Schedule rule:** if this change cannot land inside the announced pre-v1
wipe window, it MUST NOT silently slip — it converts to an explicit post-v1 migration proposal with the costs
stated honestly, and the ADR records that the window was missed.

## Non-goals

- Re-keying NAME, CONTEXT, or INCOMING.
- ALIAS (separately owned).
- A public predicate-membership watch API (add/remove watch semantics are defined only if a current consumer is
  identified).
- Retention, TTL, cascade, or GC policy.

## Capabilities

### Modified Capabilities

- `graph-index`: PREDICATE_INDEX physical layout, catalog retirement, direct namespace filters.
- `graph-query`: unchanged wire semantics on the new representation (the exact-vs-namespace contract is defined in
  `graph-index-replacement-semantics` and MUST hold before and after cutover).

## Dependencies

- `graph-index-replacement-semantics`: its ownership matrix, `[A] -> [B] -> []` fixtures, and reconciliation
  mechanism are reused verbatim on the raw layout.
- Canonical predicate contract (PR #532) and bounded entity-ID contract enforced; `nats-kv-keys` baseline archived.
- The announced pre-v1 wipe window still open.

## Impact

- **Framework code:** predicate key codec, catalog retirement, query handler filters, graph-clustering readers,
  fixtures.
- **Stored data:** covered by the same announced wipe/reseed.
- **Operators:** human-readable predicate keys; one fewer bucket; documented direct filters.
- **Architecture:** ADR-078 supersedes ADR-065's hash-plus-catalog representation clauses and selects raw keys.
