## Why

SemStreams documents predicates as `domain.category.property`, but production never enforced that contract.
Framework constants, rule packs, dynamic writers, reference deployments, sister products, and persisted
ENTITY_STATES now contain incompatible predicate shapes. Enabling the current fail-closed implementation
would reject first-party traffic, leave bypassing write lanes, and can delete a noncanonical value during a
remove-then-rejected-add update.

Beta is the clean-break window. The grammar, all owned producers/reference designs, and fail-closed
enforcement must land together before v1. SemStreams will not carry compatibility aliases, permissive runtime
  modes, dual reads, or deprecated predicate handling into the codebase.

## What Changes

- Define one canonical predicate parser for exactly three lowercase kebab-case, bounded, NATS-KV-safe
  segments. Each segment uses `[a-z][a-z0-9]*(-[a-z0-9]+)*` and is at most 64 bytes.
- Separate structural validity, vocabulary declaration, namespace authority, ownership authority, and
  physical index encoding so one layer cannot silently stand in for another.
- Validate declarative authoring surfaces at registration/configuration time and validate the complete final
  entity candidate at the single ENTITY_STATES persistence seam.
- Add build-time/source/configuration audits and an offline cutover check that reports reset/reingest needs.
  Runtime uses the ordinary canonical replay validator, not a dedicated compatibility scanner.
- Rename all first-party and owned sister-product predicates, rules, schemas, tools, and reference designs in
  lockstep with enforcement.
- Make fail-closed validation unconditional at the final persistence seam, with no compatibility escape hatch.
- Require beta operators to export if needed, clear incompatible graph state, and reingest from canonical
  sources. Existing derived indexes are rebuilt from the clean authoritative state.
- Validate replacements before destructive removal so any rejected update leaves prior valid state intact.
- Constrain agent/tool predicate authoring to declared vocabulary or explicitly delegated namespaces.

**BREAKING:** graph writes outside the canonical grammar are rejected as soon as the new binary runs. Exact
predicate identities change, and incompatible beta ENTITY_STATES data must be reset/reingested before the
deployment becomes ready.

## Non-goals

- Selecting the final PREDICATE_INDEX raw-versus-hashed key format; that belongs to the coordinated
  `graph-index-fixed-arity-reconciliation` change.
- Re-keying NAME, CONTEXT, INCOMING, or OUTGOING solely because predicates become canonical.
- Centralizing every product ontology in SemStreams. Products retain domain semantics while declaring the
  vocabulary or namespace authority they use.
- Treating vocabulary registration as graph-fact ownership authorization.
- Folding entity-ID validator unification from gh#531 into this change unless its scope is explicitly revised.
- Solving gh#519 scalar rule substitution, physical semantic GC, cascade policy, or ObjectStore reachability.
- Runtime compatibility aliases, deprecation shims, permissive modes, dual reads/writes, or in-place migration
  of malformed beta state.

## Capabilities

### New Capabilities

- `predicate-contract`: syntax, semantic identity, vocabulary/namespace declaration, authoring validation,
  clean beta cutover, and unconditional enforcement for graph predicates.

### Modified Capabilities

- `graph-ingest`: every final ENTITY_STATES candidate is structurally validated through one unconditional
  authoritative gate, and incompatible stored beta state blocks readiness until reset/reingest.

## Dependencies

- `graph-index-fixed-arity-reconciliation` consumes clean canonical state for any physical key cutover. Its
  benchmark does not block source/config audits or the canonical parser.

## Impact

- **Framework code:** `vocabulary`, payload/tool schema registration, `processor/graph-ingest`, mutation
  APIs, Graphable/foreign-edge ingestion, rule/gated-DAG/lifecycle configuration validation, schema
  generation, metrics, corpus-audit tooling, graph-index replay, and query/traversal/clustering readiness.
- **Stored data:** incompatible beta ENTITY_STATES is exported if required, cleared, and reingested from
  canonical sources. Derived indexes are rebuilt; no runtime state rewriter or dual format is retained.
- **Consumers:** SemSource, SemOps, SemConnect, SemTeams, SemSpec, SemDragon, and other products that emit
  Graphables, rule actions, projection contracts, or direct mutation requests.
- **Operations:** offline cutover check, explicit reset/reingest instructions, typed rejection metrics, and a
  readiness error naming incompatible stored state.
- **Verification:** unit and real-NATS integration coverage for every write lane, restart/replay query parity,
  invalid-preexisting-state readiness, rule replacement atomicity, cross-repository contracts, and e2e tiers.
