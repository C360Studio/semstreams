# ADR-074: Canonical Predicate Contract and Clean Beta Cutover

**Status:** Accepted

**Date:** 2026-07-14

## Context

SemStreams intended predicates to be semantic names with three positions, but the runtime accepted arbitrary
strings. First-party producers consequently emitted two-part, four-part, underscored, camel-case, and dynamic
predicate names. That ambiguity weakened namespace ownership, made NATS wildcard reasoning unreliable, and
allowed graph-index layouts to evolve without a stable semantic input contract.

This is still a beta system. We own the production reference designs and can coordinate breaking producer
updates. Carrying aliases, permissive modes, or dual formats into v1 would make every later retention and index
decision reason about historical predicate dialects.

## Decision

### Canonical syntax

Every stored predicate is exactly `domain.category.property`. Each segment matches
`[a-z][a-z0-9]*(-[a-z0-9]+)*`, is at most 64 ASCII bytes, and the complete value is at most 194 bytes. Wildcards
are query syntax and are never stored predicate values.

`vocabulary.ParsePredicate` is the sole parser. Boolean checks, registration, authoring validation, persistence,
replay, and index code delegate to it.

### Separate syntax, declaration, authority, ownership, and encoding

- Syntax determines whether a value can be a predicate.
- Vocabulary registration supplies stable names and metadata.
- Namespace delegation permits a product authoring surface to define names in one domain or one
  `domain.category` pair.
- Graph ownership determines who may mutate facts on an entity.
- Index encoding determines how a query axis is stored.

These checks do not imply one another.

Namespace delegation is a declaration-time authoring boundary, not a bearer credential. A vocabulary package,
configuration, rule pack, schema, or generated tool may expose an undeclared canonical predicate only when that
artifact is bound to an exact delegated domain or `domain.category` namespace. ENTITY_STATES persistence always
enforces canonical syntax, but does not infer namespace authority from `Triple.Source`, `EntityState.MessageType`,
context, subjects, or other caller data. Raw mutation lanes are syntax-only at persistence; endpoint authentication
and graph ownership remain separate. Runtime namespace authorization requires a future principal-bearing mutation
envelope and is out of scope.

### One authoritative state codec

All in-process ENTITY_STATES writers use `graph.MarshalEntityState`, which validates the complete final candidate
after merge and framework injection and before Create, Put, or CAS. All authoritative readers use
`graph.UnmarshalEntityState`, which rejects unreadable or noncanonical state.

Graphable ingestion preflights both primary and foreign triples before the first write. CAS callbacks validate the
candidate reconstructed from the current revision on every retry. Generic rule `update_kv` actions cannot target
framework-owned graph buckets.

### Stored-state poison is permanent for the process lifetime

If graph-index observes unreadable ENTITY_STATES or a noncanonical predicate, it enters sticky
`graph_state_reset_required`. The repair loop cannot clear this state. Readiness stays false and query consumers
receive a fatal typed error with a bounded reason.

Only a complete incompatible-resource wipe followed by process restart and canonical-source reseed clears the poison
state. The pre-v1 contract does not define beta-state export, inspection, preservation, or rollback.

ENTITY_STATES watch handling distinguishes state poison from transport and deletion. PUT/CREATE values pass
through the canonical decoder. DEL and PURGE entries are valid empty-payload tombstones that drive the same
entity-removal cleanup and count as terminal replay work; they are never decoded as entity JSON. Watch closure or
another transport failure withholds or degrades readiness through the ordinary retry/health path, but does not
latch `graph_state_reset_required`. Transport recovery does not require an operator graph reset.

### Clean beta cutover

Owned producers, configurations, exact queries, schemas, and sister repositories update in lockstep. The rename
ledger is release documentation only; runtime code does not load it. Existing incompatible graph and derived-index
buckets are not rewritten or inspected in place. Operators stop all writers, delete the complete incompatible
resource set, restart, and reseed canonical source data.

## Consequences

- Predicate identity changes are breaking and must land with all owned producers.
- Invalid candidates fail before persistence and invalid stored state cannot yield a ready graph.
- Product vocabularies remain product-owned without weakening the universal structural grammar.
- Index representation remains hash-plus-catalog until the fixed-arity real-NATS benchmark selects a successor.
- Operators get a simple, honest beta recovery procedure instead of permanent compatibility machinery.

## Rejected Alternatives

- Permissive/report-only runtime mode.
- Deprecated aliases or automatic predicate rewriting.
- Dual index reads or writes.
- Treating `Triple.Source` or message metadata as authenticated namespace authority.
- In-place migration of malformed beta ENTITY_STATES.

## References

- `openspec/changes/predicate-contract-enforcement/`
- `openspec/changes/graph-index-fixed-arity-reconciliation/`
- ADR-065, ADR-068, ADR-073
