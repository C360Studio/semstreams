# Change: Give the six entity-ID positions meanings, an authority, and a boundary

## Why

`entity-id-contract` fixes arity, alphabet, bound, and validator authority and assigns meaning to no position.
Four incompatible meanings occupy `platform` today (deployment authority, product name, a fixed framework literal,
wire-supplied), the rule engine mints local runtime state under a firing entity's authority (#1096), hierarchy
containers pad the arity with reserved tokens that have no contract, and no graph boundary knows whose authority a
write carries. The owner ruled on #1095 (2026-08-26): positions have meanings; `platform` is the minting deployment
authority; source belongs in `system`; `domain` is a delegated taxonomy; `org.platform` is enforced at graph
boundaries unless the write arrives through an import lane with provenance; reordering is allowed; arity stays six.
The pre-v1 clean break is the only window in which the order can change (ADR-076 d6; ADR-068).

## What Changes

- **BREAKING:** the canonical order becomes `org.platform.system.domain.type.instance` (owner decision O-1); every
  builder, pattern, prefix helper, index-position reader, config literal, fixture, and document follows.
- `pkg/types` names each position, exports the prefix-level vocabulary (deployment = 2, source = 3, taxonomy = 4,
  type = 5), and adds `EntityDomainDelegation` — a declaration on the predicate-namespace pattern, with a
  framework-reserved set. An authorization policy over it was built and then deleted by the owner ruling of
  2026-08-28: domain overlap between producers is permitted, so there was nothing to authorize. The declaration's
  only consumer is the entity-ID corpus audit's registered set.
- Every framework builder declares its domain and takes authority only from `deps.Platform`; ADR-076's fixed
  `semstreams.framework` namespace is retired in favour of the deployment's own `org.platform`.
- `pkg/types` exports a coded authority rejection distinct from structural rejection.
- graph-ingest enforces the deployment's own authority on every lane before KV I/O; an input port may be declared an
  import lane; #1096 is fixed by minting from `deps.Platform`.
- `platform.instance_id` leaves identity; `platform.id` is the single authority field (owner decision O-2).
- `cmd/entity-id-audit` gains segment rules, its 30 unclassified corpus findings are classified, and
  `task entity-id:audit` joins the CI lint job.
- Spec deltas: `entity-id-contract` (MODIFIED canonical form; ADDED semantics, authority, prefix levels, coded
  rejection, corpus rules), `graph-ingest` (ADDED authority gate, import lane, own-authority minting),
  `graph-clustering` (MODIFIED type-prefix requirement), `agentic-lessons` (MODIFIED scope-key specificity),
  `rule-engine` (ADDED name-resolved segment substitution).

## Non-goals

- Implementing ADR-099 (gh606 lands on the new cut points as its own change in the same wave).
- Retiring hierarchy containers (gh606 scope; this change declares the padding tokens as reserved).
- Typed or signed provenance on the import lane (ADR-057 remains withdrawn).
- Any migration, alias table, rename ledger, dual contract, or in-place rewrite of stored identity.
- Editing any sister repository (communicate only; per-sister list in
  `docs/proposals/gh1095-entity-id-segment-semantics-design.md` §D).
- Fixing `docs/concepts/16-federation.md` (#1097).
