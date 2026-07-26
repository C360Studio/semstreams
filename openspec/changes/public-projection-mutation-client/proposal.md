# Public Projection Mutation Client

## Why

SemStreams exposes projection ownership contracts and owner-token binding, but it does not expose the matching
write-side framework boundary. Rule actions can replace owned triples, while non-rule consumers must assemble raw
NATS requests, retry policy, provenance, owner tokens, and authoritative read-back themselves.

Semdragon, agentic tools, and prospective child-entity support in issue #683 demonstrate that this is a framework
gap rather than a domain-specific helper. A replacement-only helper would leave atomic entity creation, append-only
evidence, ambiguous transport outcomes, and degraded success duplicated downstream.

## What Changes

- Add a public, contract-bound mutation client in `pkg/projection`.
- Require owner liveness before binding any owning contract, then apply the resulting opaque owner token to
  authoritative create and replace-owned operations. Append-only clients may be heartbeat-free.
- Expose four narrow capabilities:
  - atomic entity creation with primary-subject initial triples;
  - schema-derived replacement of owned predicate groups;
  - append-only evidence with duplicate-safe ambiguity handling;
  - authoritative entity read-back.
- Preserve the existing graph mutation and query subjects, request/response envelopes, and error codes.
- Return a typed mutation error and commit state without hiding the existing classified-error chain.
- Make `CreateMutation.Triples` the sole birth-fact source and reject a populated `Entity.Triples`.
- Add optional stable predicate-group names so replacement targets one declared atomic group without breaking
  existing single-group contracts.
- Add create-only `Contract.BirthPredicates` for immutable facts that must not become ownership claims.
- Define operation-specific retry and equality behavior. In particular, blind append is not treated as idempotent.
- Provide narrow interfaces so consumers depend only on the mutation mode they are authorized to use.
- Migrate duplicated in-repository mutation orchestration after the public API is proven.

## Impact

### Framework

- New public API and implementation in `pkg/projection`.
- Existing `pkg/ownership`, `pkg/natsclient`, graph wire types, and graph-ingest handlers remain authoritative.
- Rule internals may delegate to the new client, but no rule type becomes part of the public API.

### Consumers

- Semdragon can replace its bespoke create-or-verify, owner-token, replacement, and read-back orchestration with one
  framework client.
- Issue #683 can use this client if it selects canonical child entities, while retaining responsibility for the
  child model and query semantics.
- Consumers migrate incrementally through narrow interfaces; no downstream migration is required in this change.

### Compatibility

- No new NATS subject, envelope, handler, or persisted representation.
- Predicate-group names, birth predicates, and the replace group selector are local contract/client fields and do
  not change graph mutation JSON.
- Existing callers and rule actions remain source- and wire-compatible during migration.
- Create rejects cross-subject triples, including `ForeignEdgeClaim` writes; those remain on the existing
  reconciliation path.
- No change to lifecycle CAS transitions, entity deletion, or foreign-edge reconciliation.
