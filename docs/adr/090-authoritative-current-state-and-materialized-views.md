# ADR-090: Authoritative Current Semantic State with Role-Specific Materialized Views

## Status

**Accepted** — 2026-08-03. The owner approved the graph-state decision proposal and authorized
breaking pre-v1 implementation without compatibility layers.

The evidence baseline is the frozen
[graph-state inventory](../proposals/graph-state-read-write-inventory.md). The
canonical living implementation state is the
[graph-state program](../proposals/graph-state-read-write-program.md). This ADR
remains the decision record and is not a program tracker.

## Context

SemStreams has one authoritative current-state store, `ENTITY_STATES`, and independently
implemented derived views. `ENTITY_STATES` has history 1; mutation commands and retained
Graphable facts do not form an authority-recovery ledger. The recurring failure pattern is
therefore not missing event-sourced CQRS machinery. It is derived capabilities without
role-appropriate convergence contracts, including durable projections with no semantic consumer.

## Decision

1. `ENTITY_STATES` remains canonical current shared semantic state. Graphable replay is bounded
   catch-up, not disaster recovery. Authority recovery uses snapshot/restore of `ENTITY_STATES`,
   referenced ObjectStore content, and explicitly coordinated ingest-guard state.
2. SemStreams does not adopt event sourcing or a general CQRS/read-model runtime.
3. Every derived capability is classified as a required query view, optional enrichment,
   internal accelerator or deduplication store, reverse bookkeeping, reactive consumer, or
   serving cache. Obligations follow the role rather than `ClassDerived` as a whole.
4. A durable view without a present semantic consumer is deleted before convergence machinery
   is added. `CONTEXT_INDEX` and durable `STRUCTURAL_INDEX` are the first accepted retirements;
   the modeled context fact and in-memory structural computation remain.
5. Surviving durable owners default to one active runtime instance until active/active
   convergence is explicitly proven.
6. Remote application reads use the required GraphQL gateway's admitted operations; GS-12 makes
   that gateway conformant. No MCP graph-read contract is claimed until tools exist. There is no
   canonical general embedded client; embedded
   services use an operation-specific typed adapter only when admitted. The aggregate
   `graph/query.Client` is provisional mixed direct-KV/RPC and GS-12 retires or internalizes it.
   Raw KV is an owner/operator seam, not an application fallback.
7. Authoritative writes are expressed as typed intents. Raw subjects are transport details.
   Command correlation and idempotency remain separate from projection visibility.
8. Derived rebuild is side-effect-free. Effectful inference application is a separately
   authorized, idempotent, bounded component operation with authoritative mutation evidence.
   Authority restore is a distinct runbook.
9. Shared runtime mechanics are introduced only after at least three surviving owners need the
   same behavior and a prototype reduces total code and adopter knowledge.

## Alternatives rejected

- Package-local repair without a shared architectural identity.
- Event-sourced CQRS.
- Extending `pkg/graphview` into a general durable-projection runtime now.
- Imposing one convergence contract on every derived bucket.

## Consequences

The framework carries fewer durable stores and public paths. Required views acquire explicit
convergence and readiness obligations incrementally. Breaking removals use the pre-v1 clean
wipe/reseed policy with no aliases, dual readers, online migrations, or compatibility layers.

This decision supersedes the continuing durable-context consequence of ADR-065 while preserving
ADR-065 as the historical record of the retired storage layout. It also supersedes ADR-049's
claim that `ENTITY_STATES` revision history supplies lifecycle audit history. ADR-049's lifecycle
convention remains; repair and audit retention require an explicit owner design outside authority
history 1. Mechanics are implemented through
separate, archivable OpenSpec changes. The first two retirements landed in #894
and #895; subsequent order and gate state live only in the canonical program.
