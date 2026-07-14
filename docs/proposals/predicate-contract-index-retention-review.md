# Predicate Contract, Index, and Retention Review

## Status

Research complete. This proposal records the contract reconstruction and the decisions that must precede
semantic-retention implementation. It authorizes planning a clean beta contract cutover, but not selecting a
new index key format before the real-NATS decision gate.

## Executive verdict

SemStreams was designed around predicates with exactly three semantic parts:
`domain.category.property`. That structure was documented and a validator existed from the initial code,
but production graph-write paths never enforced it. Framework and product producers consequently shipped
one-, two-, three-, and four-part predicates.

PR #524 correctly hardened the physical index representation against the malformed live corpus. Its
per-membership sharding, ordered authoritative reconciliation, explicit empty OUTGOING projection,
readiness contract, and failure handling remain sound. Restoring the three-part contract does, however,
reopen the PREDICATE_INDEX raw-versus-hashed key decision because ADR-065's prefix-collision argument
depends on variable predicate arity.

The same fixed-token reasoning also changes the retention design favorably. NATS filtered key listing
accepts leading and interior token wildcards, so a bare entity ID may be sufficient to enumerate most
PR #524 graph-index memberships. A per-entity reverse manifest or payload-rich tombstone is therefore not
yet proven necessary for PREDICATE, NAME, source-side INCOMING, or CONTEXT cleanup. This must be confirmed
under real NATS load before ADR-068/073 or gh#527 prescribe new durable structures.

The current enforcement spike is not the implementation baseline. It misses authoritative write lanes and
creates a remove-then-reject data-loss path. The clean gate lands only after all owned producers,
configurations, and reference designs are corrected in lockstep.

Beta policy is explicit: v1 supports only the canonical predicate contract. Existing graph/index buckets are
reset and authoritative sources reingested. SemStreams carries no runtime compatibility mode, alias table,
in-place transformer, dual read/write path, downgrade path, or deprecated predicate acceptance.

## Reconstructed contract

### Predicate syntax

A predicate has exactly three non-empty segments:

```text
domain.category.property
```

The original contract uses the positions as semantic structure:

- `domain` identifies the vocabulary or authority namespace;
- `category` groups related facts inside the domain;
- `property` identifies the specific fact;
- the complete three-part string is the predicate's exact semantic identity.

The canonical v1 grammar is exactly three lower-kebab segments. Each segment matches
`[a-z][a-z0-9]*(-[a-z0-9]+)*`, is at most 64 ASCII bytes, and is NATS-KV-safe. Uppercase, underscore,
wildcard tokens, whitespace, slash, empty segments, and control characters are invalid.

### Structure is not authority

Three valid segments do not authorize an agent or product to mint arbitrary semantics. These are separate
layers:

1. structural validity: the predicate has the canonical safe shape;
2. vocabulary declaration: metadata and tooling know the predicate;
3. namespace authority: a writer is allowed to use that domain/category;
4. ownership authority: a writer may mutate that predicate group on a particular entity;
5. storage encoding: an index chooses raw, reversible, or hashed representation for its access pattern.

The framework must validate structure for every graph write. Registration remains an explicit semantic and
tooling contract; it cannot make malformed syntax valid. Agent-facing tools should select declared
predicates or authorized namespaces rather than accept unrestricted strings.

## Production violation classes

The violation inventory must cover more than vocabulary constants. Confirmed first-party classes include:

- framework constants and literals such as `agent.run`, `rule.spawned_task`, `inferred.related_to`, and
  four-part network traffic predicates;
- gated-DAG defaults and dynamically generated lineage predicates;
- rule action predicates, rule condition fields, and substitution references;
- mission, research, alert, and GitHub workflow reference configurations;
- lifecycle tags, ownership claims, projection contracts, schema defaults, and tool schemas;
- sister-repository Graphable implementations and rule packs;
- existing values already persisted in ENTITY_STATES.

A lint that only scans `$entity.triple.*` references is not a corpus audit. CI must inventory every owned
authoring surface; an offline cutover check reports whether stored state requires reset/reingest. At runtime,
ordinary canonical replay validation permanently withholds readiness from any invalid state.

## Enforcement boundary

The correctness gate belongs immediately before every ENTITY_STATES commit, after normalization, merge,
foreign-edge routing, hierarchy/profile injection, and final candidate construction. Handler validation is
useful for early classified errors but is not authoritative.

Enforcement has one mode: fail closed. CI/static audits first correct every owned framework/product producer,
configuration, schema, tool, and exact-query consumer. The new binary then rejects malformed final candidates
unconditionally at the persistence seam. Startup refuses incompatible stored state and directs the operator
to export if required, clear graph/index buckets, and reingest canonical sources.

Replacement must validate before destructive mutation. In particular, `update_triple` must never remove a
valid old value and then expose a rejected or failed replacement.

## PR #524 impact matrix

- **PREDICATE — `hash(predicate).entityID`:** three-part arity removes the cited raw prefix collision.
  Benchmark raw fixed-arity versus hash/catalog before v1.
- **INCOMING — `targetID.sourceID.hex(predicate)`:** hex remains a stable one-token codec. Keep it unless a
  real predicate-axis query requires raw tokens.
- **NAME — `hash(name).entityID.hex(predicate)`:** names remain arbitrary content. Keep the hashed name and
  sharded memberships.
- **CONTEXT — `entityID.hash(context).hex(predicate)`:** context remains arbitrary content and the entity
  prefix aids reconciliation. Keep it.
- **OUTGOING — `entityID -> complete edge array`:** it is independent of predicate key encoding. Keep
  authoritative full replacement, including `[]`.
- **ALIAS — `alias -> entityID`:** entity ownership exists only in the value. It still needs a reverse
  structure or value scan for cleanup.

With a strict three-part predicate and six-part entity ID, a raw PREDICATE membership key has exactly nine
tokens:

```text
domain.category.property.org.platform.domain.system.type.instance
```

It could support exact predicate listing, namespace filters, direct watches, entity-position cleanup, and
human-readable operations without PREDICATE_CATALOG. Hashing may still be retained as grammar-independent
defense against malformed input. The choice is a measured v1 decision, not an automatic rollback of PR #524.

### Current catalog contradiction

PR #524's graph-index specification says predicates may contain arbitrary KV-unsafe characters because
reverse-index predicate axes are hex encoded. PREDICATE_CATALOG nevertheless writes the raw predicate as a
NATS KV key. A structure-only validator can therefore accept a predicate that commits to ENTITY_STATES and
hashed membership storage but fails catalog insertion, withholding graph-index readiness and predicate-list
visibility. Either the grammar must be KV-safe or the catalog must be encoded; the latter gives up much of
the direct namespace property and adds another join.

## Retention correction

`natsclient.FilteredKeys` delegates to NATS `ListKeysFiltered`, whose subject filters can place `*` in fixed
token positions. PR #524's key shapes can potentially be enumerated from a bare six-part entity ID:

| Store ownership to reconcile | Filter shape |
|---|---|
| PREDICATE membership | `*.<entityID>` |
| NAME membership | `*.<entityID>.*` |
| INCOMING membership owned by the source | `*.*.*.*.*.*.<entityID>.*` |
| CONTEXT membership | `<entityID>.>` |

This can support both delete cleanup and stale-membership retraction during ordinary re-indexing. Returned
keys must be deduplicated if concurrent writes can produce repeated observations.

The capability is not yet a performance result. A real-NATS spike must prove:

1. exact matching for every current key shape, including freshly recreated empty buckets;
2. behavior under concurrent Put/Delete and duplicate delivery;
3. leading-wildcard cost at a gh#430-sized and production-shaped cardinality;
4. selected buckets are deleted/recreated empty before replay, with no old-format reader;
5. bounded reconciliation latency and resource usage;
6. comparative write/read cost versus maintaining an owner manifest on every mutation.

Semantic ownership remains decisive. Retiring a source retracts source-owned INCOMING evidence. Retiring a
target does not by itself authorize deletion of every live source assertion pointing to that target.

This correction does not solve every retention surface. ALIAS, spatial/geohash structures, embedding
deduplication, shared ObjectStore references, cascade/refuse policy, and tombstone purge still need explicit
authority and lifecycle contracts.

## Governed work sequence

Two coordinated OpenSpec changes should follow this proposal:

1. `predicate-contract-enforcement`
   - freeze syntax, namespace, registration, and authoring contracts;
   - audit and rename every owned local/sister-repo/reference-design producer;
   - place one unconditional fail-closed gate at the final persistence seam;
   - require incompatible beta state to reset/reingest with no runtime compatibility machinery.
2. `graph-index-fixed-arity-reconciliation`
   - correct and archive the merged `graph-index-hardening` change;
   - run the raw-versus-hash PREDICATE_INDEX benchmark;
   - run the real-NATS wildcard reconciliation spike;
   - amend graph-query and graph-retention contracts from evidence;
   - supersede the affected ADR-065/068/073 decisions without rewriting their history.

The predicate contract must settle before the final index-key decision. The wildcard reconciliation spike
can run in parallel because PR #524's current hashed/encoded keys already have fixed token positions.

## Decision gates

No enforcement or index cutover may land until all applicable gates pass:

- one canonical parser and typed error taxonomy;
- zero invalid owned source/configuration/contract fixtures across participating repositories;
- an exact breaking rename ledger used only for coordinated source changes and release documentation;
- validate-before-remove replacement semantics;
- a successful representative reset/reingest drill and clean-state restart/query parity;
- real-NATS key-safety and wildcard-filter evidence;
- structural e2e and every affected product contract/e2e suite;
- operator preflight, export-if-needed, reset, and reingest instructions;
- corrected OpenSpec current truth and superseding ADR decisions.

## Documentation and issue ledger

The implementation changes must correct, supersede, or cross-link:

- ADR-065: raw predicate collision rationale, wildcard trade-off, and catalog necessity;
- ADR-068 D3: claim that suffix/middle-token cleanup requires a reverse index;
- ADR-073 section 4: tombstone-payload/reverse-manifest premise and participant ledger;
- gh#527: graph-index cleanup assumptions and remaining non-index retention scope;
- gh#531: entity-ID validator unification remains separate unless deliberately pulled in;
- gh#519: predicate arity does not itself solve scalar field substitution;
- gh#433: narrow remaining reciprocal/non-filterable cleanup after the real-NATS filter proof;
- `graph-index-hardening`: unsafe-predicate acceptance scenario and retention non-goals;
- `openspec/specs/graph-retention/spec.md`: forecast that a per-entity reverse index is the next increment;
- `docs/proposals/graph-retention-10-product-audit.md`: owner-reverse versus tombstone-payload premise;
- graph-query: exact predicate identity versus namespace-prefix query semantics;
- graph-retention: stores discoverable by fixed-position filters versus stores needing other authority;
- `docs/concepts/02-kv-twofer.md`: raw monolithic PREDICATE_INDEX and direct predicate-watch claims;
- `docs/concepts/04-knowledge-graphs.md`: claim that the predicate itself is the physical index key;
- `docs/advanced/05-index-reference.md`: pre-PR #524 layouts and raw-bucket debug commands;
- vocabulary guides and `vocabulary/README.md`: unresolved lexical style and direct NATS predicate claims.

## Non-goals

- Reverting PR #524's sharding, ordered reconciliation, or readiness hardening wholesale.
- Choosing raw PREDICATE_INDEX keys without benchmark and clean-cutover evidence.
- Treating three-part syntax as centralized ownership of product vocabularies.
- Adding a general-purpose database query planner or a new event bus.
- Solving cascade deletion, ObjectStore reachability, or global mark/sweep inside predicate enforcement.
- Runtime compatibility modes, aliases, deprecated predicate tables, in-place state rewriting, or dual formats.
