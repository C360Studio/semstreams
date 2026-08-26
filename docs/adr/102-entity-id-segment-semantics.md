# ADR-102: Entity-ID Positions Have Meanings; `platform` Is the Minting Deployment Authority

## Status

**Proposed — awaiting owner ruling on #1095** (order choice, tag split, and owner items O-2, O-6, O-9, O-10, O-11
through O-14 in the design). Supersedes ADR-076 decision 1 (the fixed `semstreams.framework` namespace for rule-derived
entities) if accepted; amends ADR-076 decision 2 (identity lengths are bounded through a configuration-load budget on
the authority pair, no longer fixed constants); ADR-076 decisions 3–6 stand. Amends ADR-099's level phrases: "level 0
= system (4 parts)" becomes source×taxonomy and "1 = domain (3)" becomes source (3). Mechanics live in
`openspec/specs/entity-id-contract/spec.md` and `graph-ingest/spec.md` via the `entity-id-segment-semantics`
change; this page records only the decision.

## Context

The six-position entity ID had a rigorous lexical contract and no semantic one. The positions were labels in a
doc comment; the shipped dogfooder put the producing product's name in `platform`, the framework put a fixed
literal there for rule-derived entities, and the rule engine read a firing entity's `org.platform` back as the
authority under which to mint local runtime state. Federation (semmem PR #2) made the drift visible: peer exchange
is the first case where two independently chosen values must mean something to each other. The full dependency
inventory is `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md`.

## Decision

1. **Positions have names, meanings, and owners; the order is
   `org.platform.system.domain.type.instance`.** `org` is the organization namespace; `platform` is the minting
   deployment authority; `system` is the source that produced the entity; `domain.type` is a delegated taxonomy;
   `instance` is the leaf and stays last because it is the only unbounded-cardinality position. Arity stays six.
2. **`platform` is the composition root's own identity field — `platform.id`, subject to owner item O-2 — and
   nothing else.** It is never taken from a payload, a constant, a product name, or a firing entity. Framework-derived families (rule alerts and triggers, loop and
   chain executions, lessons, observations, diagnoses, gated-DAG fan-outs, hierarchy groupings while they exist)
   carry the deployment's own `org.platform`; ADR-076's fixed `semstreams.framework` namespace is retired because a
   trigger digest over `(packID, ruleID)` under a fixed authority is the same entity in every deployment.
3. **Product names are provenance, not identity.** `system` names the source (repo, feed, world, board, API,
   framework component); the producing product rides `Triple.Source` and the envelope `source`.
4. **`domain` and `type` are delegated on the predicate-namespace pattern.** The framework reserves `agent`,
   `ops`, `graph` (and `gateddag` only if owner item O-9 declines the gated-DAG re-slot); a product registers exact
   `domain` or `domain.type` delegations at its composition root; an undelegated value in a builder or declaration
   is a composition rejection at boot, never a runtime rewrite. `system` and `instance` are not registered.
5. **Every graph boundary enforces the deployment's own authority on the candidate subject identity.** A subject
   whose `org.platform` differs from the deployment's is rejected with a coded error unless it arrives on an input
   port the operator declared as an import lane; on an import lane a subject claiming the local authority is
   rejected. `@id` objects are not authority-checked: they keep structural validation; no stub is created and an absent object is permitted
   (`graph-ingest/spec.md` "Relationship target absence creates no entity"), so local entities may cite imported ones. Whether a local lane may annotate an existing foreign subject is owner
   item O-12. Provenance on an import is the declared lane plus the unauthenticated `source` strings that exist
   today; typed origin is a separate decision. Rejection mechanics (codes, metering, logging) live in the spec.
6. **Prefix lengths have fixed meanings:** 2 = deployment, 3 = deployment+source (the federation triple),
   4 = +taxonomy, 5 = +type. ADR-099 levels are 0 = 4 parts, 1 = 3 parts (source), 2 = 2 parts.
7. **Identity is never rewritten.** The cutover is the one-time pre-v1 clean owned-source break of
   `entity-id-contract` §"clean owned-source break": fresh storage, no alias, ledger, migration, or dual contract.
   Content-derived families (lessons, digests) make rewrite transitive and infeasible; evidence is not regenerable
   (ADR-068).

## Consequences

- BREAKING, one wave (beta.163) with #1093 and #1096: every minted identity changes; downstreams start on newly
  provisioned NATS storage after every owned source, configuration, schema, fixture, and query is updated.
- Every sister moves the product name out of `platform` and swaps positions 3–4 in its builders; the framework's
  `entity-id-audit` gains segment rules and becomes a CI gate.
- "Everything this deployment holds from source S" becomes a prefix scan; "taxonomy D across sources" becomes a
  wildcard filter or a `tag:` scope, no longer a prefix.
- The rule engine mints runtime state under its own authority (#1096, live today for any semsource-fed deployment);
  an imported entity's rules still fire, but the runtime state they create is local. The run anchor the rule stamps
  on the firing entity is a mutation of that entity: for an imported firing entity it is rejected by the gate and
  metered `mutation_rejections{reason="authority_foreign"}`, and logged until O-12 is ruled; under the recommended O-12
  option the run-anchor pair lands as a framework annotation. O-12 therefore gates whether `Closes #1096` is complete
  for imported firing entities and is pre-landing: the reject-and-meter state never ships.
- Hierarchy inference skips foreign-authority entities: no container birth, membership triple, or inverse sibling
  edge is minted for an imported entity, because the framework never mints under a foreign authority. The skip
  stands regardless of O-12, which governs the run anchor and product annotations only.
- The authority pair is bounded at configuration load by the longest fixed-suffix framework family (170 bytes for
  `org` + `platform` today); `graph.NewAlertEvent` and the trigger identity gain authority parameters.
- Hierarchy containers are a second spelling of the ADR-099 partition and are retired with it; until then the
  padding tokens are contract-reserved.
- Two values leave the graph in wire order and are not re-minted by fresh state: the GraphQL `EntityTypeSummary.type`
  value and the vocabulary export IRI path (`vocabulary/export/export.go:123-126`); both follow the canonical order
  (owner item O-10) and are announced as published-artifact breaks.

## Cross-repo contract

A sister conforms when: `platform` = its composition root's `platform.id`; `system` = its source; its domains are
registered delegations; no builder carries a literal authority; exported entities keep their own `org.platform` and
are accepted elsewhere only on an import lane.

## References

- Owner rulings: gh#1095 comment 2026-08-26; #1093 (wave), #1096 (bug), #1097 (docs), #606/ADR-099.
- ADR-076 (superseded d1, amended d2), ADR-099 (both level phrases amended), ADR-068, ADR-072, ADR-065/078, ADR-032, ADR-087.
- `docs/proposals/gh1095-entity-id-segment-semantics-{inventory,design}.md`.
