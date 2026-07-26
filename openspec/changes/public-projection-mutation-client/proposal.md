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
  authoritative create and replace-owned operations. Append-only and foreign-edge-only registrations are
  persistently heartbeat-free.
- Expose four narrow capabilities:
  - atomic entity creation with primary-subject initial triples;
  - schema-derived replacement of owned predicate groups;
  - append-only evidence with duplicate-resistant ambiguity handling;
  - authoritative entity read-back.
- Preserve the existing graph mutation and query subjects, request/response envelopes, and error codes.
- Return a typed mutation error and commit state without hiding the existing classified-error chain.
- Make `CreateMutation.Triples` the sole birth-fact source and reject a populated `Entity.Triples`.
- Add optional stable predicate-group names so replacement targets one declared atomic group without breaking
  existing single-group contracts.
- Add create-only `Contract.BirthPredicates` for initial facts that must not become ownership claims.
- Define operation-specific retry and equality behavior. In particular, blind append is not treated as idempotent.
- Document the remaining late-commit append race. A timeout, absent read-back, and retry can still double-apply if
  the original request commits late. Generic outer retries are prohibited; strict no-retry deployments set
  `Retry.MaxRetries=0` until the server provides the idempotency primitive tracked by
  [#697](https://github.com/C360Studio/semstreams/issues/697).
- Make owner binding a Registry-wide invariant: the first successful ownership registration consumes that owner
  identity for the Registry lifetime. Direct `RegisterOwner`, `projection.Bind`, `BindAndHeartbeat`, and
  `BindMutationClient` all reject a second same-owner attempt with `ErrOwnerAlreadyBound` before heartbeat or claim
  mutation, including when the registration is identical.
- Require composition roots to aggregate every static built-in contract for an owner and bind the complete set
  once. A failed first registration releases the identity for correction; after success, correction or revival
  requires a new Registry and incarnation. A birth-only client that derives no claim skips registration and does
  not consume the identity.
- Amend registration and lease semantics under
  [#700](https://github.com/C360Studio/semstreams/issues/700): owner presence represents lease liveness, not
  registration identity. Birth-only clients create no registration, presence, token, or heartbeat enrollment.
  Foreign-edge-only, append-only, and combined foreign-edge/append owners register persistently with a zero token
  and no presence or enrollment. If any replace-owned or CAS claim exists, the complete atomic owner entry is
  liveness-managed with a non-zero token and heartbeater enrollment.
- Treat valid append-only and combined foreign-edge/append registrations as first-class persistent postures. Their
  lack of owning claims MUST NOT be logged as a misconfiguration warning.
- Preserve the same-Registry one-registration guard independently of presence. Defer permanent foreign-edge
  cross-type conflict policy to a separate follow-up rather than adding implied expiry to persistent registrations.
- Require every serving graph-ingest instance to enable owner-lease enforcement before liveness-managed clients send
  token-fenced create or replace traffic.
- Provide narrow interfaces so consumers depend only on the mutation mode they are authorized to use.
- Migrate duplicated in-repository mutation orchestration after the public API is proven.
- Make the first internal migration a bounded agentic-tools slice: preserve owner
  `agentic-loop-graph-writer`; bind all enabled built-in static contracts once; fail boot on binding, bootstrap, or
  overlap errors; atomically replace todos; inject narrow mutation interfaces into `LessonCurator`; and remove
  `OwnedFactWriter` only after parity evidence.

## Impact

### Framework

- New public API and implementation in `pkg/projection`.
- Existing `pkg/ownership`, `pkg/natsclient`, graph wire types, and graph-ingest handlers remain authoritative.
- `pkg/ownership.Registry` rejects every second successful-registration attempt for one owner and Registry through
  the inspectable `ErrOwnerAlreadyBound` sentinel.
- Owner-presence records are lease-liveness evidence only. Persistent non-owning registrations remain registered
  without presence records, tokens, or heartbeater enrollment.
- Operators MUST NOT receive a warning solely because a valid append-only or foreign-edge/append registration has no
  replace-owned or CAS claim.
- Rule internals may delegate to the new client, but no rule type becomes part of the public API.
- PR1 does not migrate raw create/append publishers, rules, `graph_writer`, research, or `agentrun`. Those lanes
  remain tracked by [#688](https://github.com/C360Studio/semstreams/issues/688),
  [#689](https://github.com/C360Studio/semstreams/issues/689), and
  [#690](https://github.com/C360Studio/semstreams/issues/690).

### Consumers

- Semdragon can replace its bespoke create-or-verify, owner-token, replacement, and read-back orchestration only
  after the enforcement rollout gate in this change passes.
- Issue #683 can use this client if it selects canonical child entities, while retaining responsibility for the
  child model and query semantics.
- Consumers migrate incrementally through narrow interfaces; no downstream migration is required in this change.

### Compatibility

- No new NATS subject, envelope, handler, or persisted representation.
- Predicate-group names, birth predicates, and the replace group selector are local contract/client fields and do
  not change graph mutation JSON.
- Existing callers and rule actions remain source- and wire-compatible during migration.
- Built-in contracts register as one aggregate set, fixing the partial-registration hazard tracked by
  [#691](https://github.com/C360Studio/semstreams/issues/691).
- Create rejects cross-subject triples, including `ForeignEdgeClaim` writes; those remain on the existing
  reconciliation path.
- No change to lifecycle CAS transitions, entity deletion, or foreign-edge reconciliation.
- #700 changes registration lifecycle semantics without adding a new wire envelope, subject, or persisted shape.
- The #700 public-contract amendment requires Fable re-review before implementation acceptance.
- This change does not perform the later internal adoption tracked by PR #696 or the Semdragon adoption tracked by
  [#313](https://github.com/C360Studio/semstreams/issues/313).
