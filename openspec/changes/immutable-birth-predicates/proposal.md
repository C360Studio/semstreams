# Proposal — immutable-birth-predicates (gh#818)

## Why

Nothing server-side protects a seeded truth predicate. All eight `graph.mutation.*` lanes plus
the Graphable merge path can replace or remove any predicate: the lease check exists on only
two lanes (`create_with_triples`, `update_with_triples`), skips entirely on an empty owner
token (`processor/graph-ingest/component.go:2139-2141` — "the single agreed skip signal"), and
is default-off observe-only; `AddTriplesBatchRequest` carries no owner token at all; the
Graphable merge is newer-wins full replacement per (subject, predicate)
(`graph/helpers.go:108-141`), so a partial re-arrival silently drops omitted sibling values;
and `entity.delete` tombstones the carrier with no protection. `pkg/projection`'s
`BirthPredicates` are, by their own docstring, "a MutationClient convention … writers outside
this client contract can still mutate them" (`contract.go:66-71`) — and the
`projection-mutation-client` spec records that gap as current truth.

One server-enforced immutable predicate already exists and works: the indexing profile is
create-time-immutable via drop-before-merge, hardcoded to that single predicate
(`component.go:2574-2583`, ADR-054). gh#818 asks for that guarantee as a **declarable,
vocabulary-shaped policy**: package-seeded canonical truth predicates (SemMachina's authored
mystery solution and evidence-truth facts) accepted at initial materialization and immutable
afterward, enforced by graph-ingest itself across every mutation lane — not by client-side
convention, local world-rule gates, or a graphio wrapper that cannot reach upstream paths.

## What Changes

- **A vocabulary-level immutable classification**: the vocabulary registry's existing
  per-predicate metadata (the `RuleOpaque bool` + `WithRuleOpaque` pattern,
  `vocabulary/registry.go:190-203`) gains `Immutable` — declared where predicates are
  declared, entity-agnostic, product-neutral. Declaring it grants **no** write authority:
  per the predicate-contract spec, mutation-lane access is the trust boundary; who may seed
  is the host NATS ACL's responsibility, and this split is documented as gh#818 requires.
- **First-write-freezes enforcement in graph-ingest, on every lane**: once an entity carries
  an immutable predicate, replacement, removal, or conflicting append of that predicate is
  refused — request/reply lanes reject the whole request with a new stable classified code;
  the Graphable merge preserves the stored value by generalizing the existing
  indexing-profile drop-before-merge, with the drop metered and logged (that lane cannot
  return an error, and rejecting a whole arrival would discard its unrelated facts).
- **Exact replay is idempotent**: a mutation carrying the identical canonical value of an
  immutable predicate is accepted as a no-op on that predicate, so package re-seeding
  converges.
- **Deletion refuses while protected facts exist**: `entity.delete` of a carrier entity
  returns the same stable code. The privileged teardown path is explicitly deferred to the
  retention/deletion system (ADR-068's lane), recorded here so the reader is not left
  inferring.
- **Enforcement is caller-independent**: it binds every writer including the seeding owner —
  immutability is a property of the fact, not a privilege check — so it composes with, and is
  disjoint from, owner-lease fencing (gh#689/gh#851) and rule contract-binding (gh#688).
- **Audit evidence via the existing rejection machinery**: the closed error-code set gains
  `immutable_predicate`; `mutation_rejections_total` and the graphable-lane drop metric make
  every refused attempt observable; the rejection names entity, predicate, and lane.

## Capabilities

### New Capabilities

_None — this hardens two existing capabilities and their contract._

### Modified Capabilities

- `predicate-contract`: vocabulary declaration gains the immutable classification and its
  authority semantics (declaration grants no write authority).
- `graph-ingest`: every mutation lane and the Graphable merge enforce immutability;
  first-write-freezes; idempotent exact replay; carrier deletion refusal.
- `projection-mutation-client`: the "Create-only birth predicates are not graph-enforced
  immutable facts" requirement gains the cross-reference — that statement remains true of
  `BirthPredicates` as such, and vocabulary-declared immutability is the server-enforced
  mechanism a contract author reaches for when the guarantee must hold against every lane.

## Impact

- `vocabulary/` — classification field + option; `processor/graph-ingest` — enforcement in
  all eight handlers, the merge path, and delete; `graph/mutation_responses.go` — one new
  stable code. No wire-shape changes; no new subjects (nothing for gh#810's registry sweep).
- Consumers: **SemMachina** (mystery-companion-acceptance task 1.5, the named blocker — its
  vocabulary already carries immutable classifications locally, task 1.1), and any sem*
  package seeding canonical truth (SemDragon quest/trust facts are the same shape).
- Behavior change is opt-in per predicate: nothing is enforced until a deployment declares an
  immutable predicate, so existing deployments are untouched.

## Non-goals

- **Owner-lease hardening.** The empty-token skip, the token-less lanes, and fail-closed
  lease rollout remain the ownership/lease workstream (`projection-mutation-client` spec's
  rollout requirement; gh#689). Immutability deliberately does not consult tokens.
- **Privileged teardown of protected entities.** Deferred to the retention/deletion lane
  (ADR-068); this change ships refusal, not the override.
- **Rule-action contract binding** (gh#688) — same enforcement-model family, separate change.
- **Entity-specific or world-specific policy** — the classification is vocabulary-shaped; no
  entity-ID patterns, no product policy in the framework (product boundary).
