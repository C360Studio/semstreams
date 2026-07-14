## Context

Predicate structure was intended to be semantic schema, not free-form text. The dormant validator checked
only dot count and was never placed at the persistence boundary. Meanwhile the graph accumulated malformed
first-party names and APIs accepted unconstrained dynamic predicates.

The current enforcement spike demonstrates useful audit and test ideas but is unsafe as a rollout baseline:
it defaults to rejection before the corpus is clean, validates selected handlers rather than the final
authoritative state, misses foreign triples and direct mutation paths, and does not rewrite existing state.
Its structure-only grammar also conflicts with PREDICATE_CATALOG, which stores raw predicate names as NATS KV
keys.

## Goals / Non-Goals

**Goals:**

- freeze one unambiguous predicate syntax and semantic identity contract;
- expose and fix every owned producer before the breaking cutover;
- validate at both authoring time and the authoritative persistence seam;
- change exact predicate identities deterministically across owned repositories and reference designs;
- require incompatible beta state to reset/reingest instead of preserving deprecated runtime behavior;
- give agents useful predicates without allowing arbitrary semantic namespace minting.

**Non-Goals:**

- make the vocabulary registry an ontology reasoner;
- change graph-index physical keys in this change;
- infer rename mappings automatically from similarly spelled predicates;
- add compatibility aliases, permissive modes, dual reads/writes, or an in-process state migrator;
- conflate predicate validity with write ownership or indexing eligibility.

## Decisions

### 1. Parse predicates into a typed three-segment value

One parser returns `{Domain, Category, Property}` or a typed reason. Boolean helpers wrap the parser; they do
not implement separate rules. Each of the three segments matches
`[a-z][a-z0-9]*(-[a-z0-9]+)*`; each segment is at most 64 ASCII bytes and the complete predicate is at
most 194 bytes including dots. This lower-kebab grammar is both semantic authoring style and v1 validity.

Wildcard tokens are query syntax, never valid stored predicate segments. Raw `*`, `>`, whitespace, slash,
empty segments, control characters, and unbounded names are rejected.

### 2. Keep syntax, declaration, authority, ownership, and encoding separate

- Syntax answers whether a string can be a predicate.
- Vocabulary declaration supplies metadata and stable constants.
- Namespace authority delegates which domains/categories a producer may author.
- Ownership decides who may mutate a predicate group on an entity.
- Index encoding is an implementation choice for a particular query axis.

Every registered predicate must be syntactically valid, but not every product predicate must be compiled
into SemStreams. Product startup may register vocabulary packages or declare an exact domain or
`domain.category` namespace. Agent tools receive an allowlisted vocabulary/namespace view; they do not mint
unrestricted strings.

### 3. Validate declarative surfaces before runtime traffic

Startup/configuration validation covers vocabulary registration, rule conditions and actions, gated-DAG
defaults, lifecycle tags, ownership/projection contracts, schema defaults, generated tool schemas, and
reference deployments. The scanner parses these surfaces structurally rather than grepping only
`$entity.triple.*` substitutions.

This gives producers precise startup failures while the persistence gate remains the final defense.

### 4. Validate the complete candidate at the single persistence seam

Every lane that can create or mutate ENTITY_STATES constructs its final candidate first. Validation runs
after merge, normalization, foreign-edge routing, hierarchy/profile injection, and ownership processing but
before the KV create/update/CAS. No partial candidate or derived-index side effect commits after a structural
rejection.

Graphable, mutation RPC, direct Go adapter, inference, rule, and repair paths call the same commit primitive.
Handler checks may return earlier errors but cannot replace this gate.

### 5. Enforce one contract unconditionally

The canonical parser is fail-closed at startup/configuration and at the final ENTITY_STATES persistence seam.
There is no runtime report mode, migration mode, `allow_nonconforming` escape hatch, compatibility alias, or
dual-read path. Source/config audit tools run in CI and an offline cutover check; they do not weaken
acceptance. Runtime uses ordinary canonical replay validation as a permanent invariant.

One structured rejection contains every unique invalid predicate/reason in the candidate. Metrics use one
recording layer and bounded labels such as lane/reason; they increment once per unique reason and never place
entity or predicate values in labels.

### 6. Make the beta cutover reset and reingest

The audit produces the exact source/config/reference-design rename ledger. All owned repositories update in
lockstep with the breaking SemStreams version. On startup, the new binary scans existing ENTITY_STATES before
graph-index replay. Any noncanonical predicate blocks readiness with a diagnostic requiring export if needed,
bucket reset, and reingest from canonical sources.

Startup ordering is not trusted for correctness. Every component that replays ENTITY_STATES or serves a
derived graph view validates replayed entities with the canonical parser. graph-index marks any violating
entity failed, never advertises ready, and makes predicate/incoming/outgoing queries, traversal, and clustering
return the typed reset/reingest requirement. No consumer may briefly serve a partial index while graph-ingest
preflight runs independently.

SemStreams does not rewrite malformed beta state in place. The operator deletes/recreates the incompatible
graph and derived-index buckets, then the ordinary authoritative ingest and graph-index replay paths rebuild
clean state. This clean cutover has no dependency on the broader bounded-storage epic.

### 7. Validate replacement before removal

Any replace/update operation validates the intended final candidate before deleting the old triple. If
validation or persistence fails, the prior valid state remains unchanged. This is ordinary mutation
atomicity, not deprecated compatibility behavior.

### 8. Make the fail-closed gate evidence-based

Enforcement requires:

- zero unmapped violations in framework source, generated schemas, reference configurations, and tests that
  model production contracts;
- clean reports from every participating sister repository;
- a startup preflight that refuses incompatible beta state and a tested reset/reingest runbook;
- cross-component proof that invalid preexisting state can never produce a ready graph-index/query surface;
- successful clean-state restart/re-index query parity tests;
- structural e2e plus affected product contract/e2e suites;
- operator diagnostics and reset/reingest rehearsal.

## Risks / Trade-offs

- **Predicate renames can break rules and queries:** require one mapping shared across producers, configs,
  reference designs, and exact-query consumers.
- **Central vocabulary registration can violate product boundaries:** delegate namespaces while enforcing a
  universal structural grammar.
- **Reset/reingest discards unexported beta state:** make the break explicit and provide preflight/export
  guidance; do not hide it behind permanent compatibility code.

## Cutover Plan

1. Land the grammar decision, parser, typed reasons, and complete source/config audit in the breaking branch.
2. Rename all owned framework/product/reference-design predicates and exact-query consumers in lockstep.
3. Validate every declarative surface and constrain agent/tool authoring.
4. Put unconditional validation at the final persistence seam and make replacement validate-before-remove.
5. Add startup incompatible-state preflight plus operator export/reset/reingest instructions.
6. Rebuild indexes from clean reingested state and prove restart/query parity.
7. Run the breaking-change e2e gates and ship without compatibility code.
