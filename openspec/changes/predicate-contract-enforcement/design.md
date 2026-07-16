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
`domain.category` namespace. Namespace delegation is declaration-time authoring governance, not a bearer
credential. Agent tools receive an allowlisted vocabulary/namespace view; they do not mint unrestricted strings.
ENTITY_STATES persistence enforces syntax but does not infer namespace authority from caller-controlled triple or
message fields. Runtime namespace authorization requires a future principal-bearing mutation envelope.

### 3. Validate declarative surfaces before runtime traffic

Startup/configuration validation covers vocabulary registration, rule conditions and actions, gated-DAG
defaults, lifecycle tags, ownership/projection contracts, schema defaults, generated tool schemas, and
reference deployments. The scanner parses these surfaces structurally rather than grepping only
`$entity.triple.*` substitutions.

This gives producers precise startup failures while the persistence gate remains the final defense.

The completed initial corpus is bounded production evidence: it covers non-test Go producers and the declared
configuration, schema, tool, and reference surfaces, but deliberately excludes `*_test.go` and `testdata`. A separate
tracked test-fixture corpus is therefore required before the clean beta cutover can claim local zero violations.

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

Startup ordering is not trusted for correctness. Every component that interprets ENTITY_STATES uses the shared
canonical decoder. Projection owners enter sticky reset-required state, never advance readiness across poisoned
state, and return the typed reset/reingest requirement. Action/evaluation consumers emit no derived output.
graph-index makes predicate/incoming/outgoing queries, traversal, and clustering return the same fatal code. No
consumer may briefly serve a partial view while another component's preflight runs independently.

Watchers classify the event before decoding: CREATE/PUT values use the canonical decoder, DEL and PURGE are
equivalent valid tombstones that drive cleanup and replay completion, and transport failure follows ordinary
degraded/not-ready recovery without latching stored-state poison.

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

Positive test fixtures use a grammar-only `internal/semantictest` predicate builder where runtime construction is
appropriate. It accepts the three explicit semantic positions, joins them without normalization, delegates to
`vocabulary.ParsePredicate`, and returns the validated string. It provides no default namespace, alias, graph entity,
triple, or Graphable factory. Production Go files may not import the helper, and vocabulary grammar-authority tests
retain raw fixtures to avoid a delegation cycle. Literal constants and commentless fixture data remain checked by the
source auditors.

Intentional invalid predicates are classified per exact occurrence, value, contract kind, and authoritative reason.
Source comments bind only to the candidate on that source occurrence; strict JSON, JSONL, and other commentless
structured fixtures use a checked file-plus-structural-location manifest. Missing, stale, duplicate, broad, unmatched,
or reason-mismatched classifications fail. The existing production scan and the complementary `*_test.go`/`testdata`
scan must both be clean before local zero-violation evidence is complete.

## Risks / Trade-offs

- **Predicate renames can break rules and queries:** require one mapping shared across producers, configs,
  reference designs, and exact-query consumers.
- **Central vocabulary registration can violate product boundaries:** delegate namespaces while enforcing a
  universal structural grammar.
- **Reset/reingest discards unexported beta state:** make the break explicit and provide preflight/export
  guidance; do not hide it behind permanent compatibility code.
- **Shared fixtures can erase semantic test intent:** keep the helper at the grammar-string layer and require tests to
  construct their own graph state, subjects, and references.
- **File-wide invalid allowances can hide stale predicates:** bind every negative classification to one occurrence and
  its authoritative reason, and fail on stale or ambiguous entries.
- **Authoring delegation is not runtime namespace authorization:** configuration-time checks prevent rules and
  dispatch from inventing `agent.lineage.*`, but any holder of a raw graph-mutation lane or graph-writing tool can
  still mint syntactically valid `agent.*` triples because the ENTITY_STATES seam authenticates no principal. This
  is an explicit threat-model gap, not an implied trust guarantee. The named follow-up is a principal-bearing
  mutation envelope plus seam-level denial of undeclared `agent.*` writes on every non-delegated lane.

## Cutover Plan

1. Land the grammar decision, parser, typed reasons, bounded production audit, and complementary test-fixture audit
   in the breaking branch.
2. Rename all owned framework/product/reference-design predicates and exact-query consumers in lockstep.
3. Validate every declarative surface and constrain agent/tool authoring.
4. Put unconditional validation at the final persistence seam and make replacement validate-before-remove.
5. Add startup incompatible-state preflight plus operator export/reset/reingest instructions.
6. Rebuild indexes from clean reingested state and prove restart/query parity.
7. Run the breaking-change e2e gates and ship without compatibility code.
