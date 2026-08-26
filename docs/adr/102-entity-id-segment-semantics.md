# ADR-102: Entity-ID Positions Have Meanings; `platform` Is the Minting Deployment Authority

## Status

**Proposed — awaiting owner ruling on #1095** (order choice, enforcement strictness, tag split). Supersedes
ADR-076 decision 1 (the fixed `semstreams.framework` namespace for rule-derived entities) if accepted; ADR-076
decisions 2–6 stand. Amends ADR-099's level-1 phrase "1 = domain (3)" to "1 = source (3)". Mechanics live in
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
2. **`platform` is the composition root's own `platform.id` and nothing else.** It is never taken from a payload, a
   constant, a product name, or a firing entity. Framework-derived families (rule alerts and triggers, loop and
   chain executions, lessons, observations, diagnoses, gated-DAG fan-outs, hierarchy groupings while they exist)
   carry the deployment's own `org.platform`; ADR-076's fixed `semstreams.framework` namespace is retired because a
   trigger digest over `(packID, ruleID)` under a fixed authority is the same entity in every deployment.
3. **Product names are provenance, not identity.** `system` names the source (repo, feed, world, board, API,
   framework component); the producing product rides `Triple.Source` and the envelope `source`.
4. **`domain` and `type` are delegated on the predicate-namespace pattern.** The framework reserves `agent`,
   `ops`, `gateddag`, `graph`; a product registers exact `domain` or `domain.type` delegations at its composition
   root; an undelegated value in a builder or declaration is a composition rejection at boot, never a runtime
   rewrite. `system` and `instance` are not registered.
5. **Every graph boundary enforces the deployment's own authority.** A candidate whose `org.platform` differs from
   the deployment's is rejected with a coded, metered, identity-free error unless it arrives on an input port the
   operator declared as an import lane; on an import lane a candidate claiming the local authority is rejected.
   Provenance on an import is the declared lane plus the unauthenticated `source` strings that exist today; typed
   origin is a separate decision.
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
- The rule engine mints runtime state under its own authority (#1096); an imported entity's rules still fire, but
  the runtime state they create is local.
- Hierarchy containers are a second spelling of the ADR-099 partition and are retired with it; until then the
  padding tokens are contract-reserved.

## Cross-repo contract

A sister conforms when: `platform` = its composition root's `platform.id`; `system` = its source; its domains are
registered delegations; no builder carries a literal authority; exported entities keep their own `org.platform` and
are accepted elsewhere only on an import lane.

## References

- Owner rulings: gh#1095 comment 2026-08-26; #1093 (wave), #1096 (bug), #1097 (docs), #606/ADR-099.
- ADR-076 (superseded d1), ADR-099 (amended level 1), ADR-068, ADR-072, ADR-065/078, ADR-032, ADR-087.
- `docs/proposals/gh1095-entity-id-segment-semantics-{inventory,design}.md`.
