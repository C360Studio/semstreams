# Post-GS-01 graph read and derived-state foundation roadmap

## Status

**PRE-IMPLEMENTATION ROADMAP — TARGET FROZEN, ROADMAP NOT YET OWNER-ACCEPTED**

This roadmap orders the owner-approved target. It does not amend it.

## Frozen authority

- Approved design SHA-256:
  `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- Design baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b` (merged PR #898)
- Approval: `post-gs01-graph-read-derived-foundation-design-approval.md`
- Independent review: `post-gs01-graph-read-derived-foundation-design-review.md`
- Accepted inventory SHA-256:
  `869be8fdfaef9c141dd7697071da0ff9fb5ffa1c4e3fbb5863837b25fb3be4ba`

Issue numbers may annotate evidence and tests. They never determine order.

## Execution model

Each slice is one coherent, independently reviewed PR to `main`. Downstream projects are paused and clean breaks are
approved, so a long-lived integration branch would add final-merge risk without protecting an active adopter.
Sequential merges provide:

- CI on every cutover;
- small reviewable diffs;
- durable commit boundaries and simple bisection;
- one current truth for every new handoff; and
- no final mega-merge in which target drift can hide.

A slice may leave a still-current surface for a later slice, but it must never add a compatibility surface. Storage and
wire changes are atomic inside their owning PR. Every PR deletes its superseded implementation, tests, documentation,
and symbols in the same change. No shim, deprecation, dual reader/writer, fallback, or cleanup-later bridge is allowed.

R5 owner-local work may be developed concurrently after its prerequisites, but shared-file reservations determine
merge order. No other slices overlap in implementation.

## Dependency order

```text
R0 roadmap and baton
  -> R1 catalog acquisition, lifecycle poison, and retry truth
  -> R2 COMPONENT_STATUS deletion
  -> R3 atomic GRAPH_STATUS retyping
  -> R4 atomic alias and suffix cutover
  -> R5 derived-owner conformance
  -> R6 canonical query catalog, hierarchy, and alternate-front deletion
  -> R7 gqlgen external front-door cutover
  -> R8 live-guidance and archive closure
  -> R9 release candidate, downstream holdouts, and final tag
```

Status precedes aliases because alias currency depends on honest graph-index contiguous coverage. Alias semantics must
exist before the catalog declares `entityByAlias`. Derived providers must prove their serving contracts before the
consolidated query surface depends on them. gqlgen lands only after all canonical operations and response types exist.

## R0 — Durable roadmap and baton

**PR:** documentation/program record only.

Land the complete eight-file authority/evidence set together so no later handoff points at a local-only record:

1. accepted post-GS-01 inventory;
2. inventory review;
3. frozen design;
4. design review;
5. owner approval;
6. this roadmap; and
7. roadmap review; and
8. the active baton.

The baton copies the approval identity packet verbatim and records the dependency graph, shared-file reservations,
proof budget, and first slice state.

Completion:

- design, review, and approval hashes independently verified;
- all eight records are tracked on `main` in the same documentation-only merge;
- no runtime or target semantic change;
- later PR templates link the approval and active baton; and
- independent reviewer confirms this roadmap changes order only, never target semantics.

## R1 — Catalog acquisition, lifecycle poison localization, and rule retry truth

**Prerequisite:** R0.

Primary surfaces:

- `graph/kvcatalog.go`
- `pkg/lifecycle/{manager,manager_query,doc}.go`
- `processor/gated-dag/{component,config,executor}.go`
- authority-open paths in graph-index, spatial, temporal, embedding, clustering, and rule
- operator-only message-logger catalog access
- `processor/rule/{actions,triple_mutator}.go`
- `pkg/projection/mutation_client.go`

Atomic outcome:

- graph-ingest alone ensures `ENTITY_STATES`;
- readers use package-local `OpenCatalogBucket` and minimal interfaces;
- no graph-source injector or generic application bucket acquisition;
- lifecycle's Manager-wide full-graph guard and poison latch are deleted;
- poison affects only touched exact/list/watch/workflow scope;
- malformed watched values never cause mutation and close/degrade observably;
- bounded logs/metrics name entity and revision; unrelated lifecycle work continues;
- rule reconcile remains one exact read and one mutation attempt; and
- lifecycle CAS retry retains full reread and transition-intent revalidation.

Owning truth:

- `pkg/lifecycle/doc.go`
- `openspec/specs/lifecycle/spec.md`
- `openspec/specs/rule-projection-mutations/spec.md`

Proof:

- malformed A does not block valid B;
- touching A fails typed and performs no mutation;
- malformed matching watch emits no participant and produces bounded diagnostics;
- no lifecycle `WatchAll`, whole-authority preflight, Manager poison latch, or new status surface;
- rule mismatch issues no second request;
- lifecycle retry revalidates phase, transition, audit chain, and mutator; and
- gated-DAG declares and owns its distinct prefix watch.

Run focused unit/race and lifecycle/gated-DAG integration. Run E2E only if a present scenario exercises this exact
path; otherwise record the coverage gap rather than running an irrelevant ladder.

## R2 — Delete `COMPONENT_STATUS`

**Prerequisite:** R1. Keep separate from `GRAPH_STATUS`: it has a different reason for deletion and no production
readers.

Primary surfaces:

- component-status definitions and all production writers under component, input, processor, output, storage,
  gateway, and service packages;
- `graph/{constants,kvcatalog}.go`;
- service/operator status plumbing; and
- E2E component-status helpers and assertions.

Atomic outcome:

- delete bucket constant, catalog classification, reporters, writers, tests, and E2E reads;
- process lifecycle remains on component/service health;
- graph role state remains on `GRAPH_STATUS`; and
- no replacement bucket, deprecated reporter, or generic status abstraction.

Proof:

- repeat the production-reader census immediately before deletion;
- compiler finds no live reporter/reference afterward;
- process-health tests remain green; and
- no current-source `COMPONENT_STATUS` reference remains outside approved history.

Run focused unit/race. If a present E2E assertion is migrated, run only its relevant tier once.

## R3 — Atomic role-typed `GRAPH_STATUS` cutover

**Prerequisite:** R2. All four producers and every consumer move together in one PR. Mixed formats are unsupported.

Primary surfaces:

- `graph/index_status.go`
- `graph/kvcatalog.go` catalog description for the shared four-producer `GRAPH_STATUS` model
- `graph/readiness_gate.go`
- `graph/readiness/{watcher,publisher,set,gauges}.go`
- graph-index, graph-embedding, graph-ingest, and rule readiness producers
- fusion and clustering consumers
- gateway readiness rendering
- `test/e2e/scenarios/stages/entities.go`

Atomic outcome:

- four fixed key/type pairs only;
- catalog truth names the shared graph-index, graph-embedding, graph-ingest, and rule producers rather than one owner;
- `RevisionViewStatus`, `IngestBacklogStatus`, and `RuleReplayStatus` concrete wires;
- key/kind mismatch fails closed;
- typed watchers and typed predicates;
- `AllSettled` folds post-evaluation decisions only;
- ordinary reads never use settlement;
- configured HTTP operator rows remain heterogeneous with no aggregate verdict; and
- generalized envelope, gate, watcher, `Set`, `FullyCovered`, and retired fields are deleted.

Owning truth:

- `openspec/specs/graph-index-readiness/spec.md`
- `docs/operations/adopter-caught-up-readiness.md`
- archive `docs/operations/migration-readiness-distribution-adr083.md`
- new ADR partially superseding generalized mechanics in ADR-088; ADR-088 remains byte-identical

Proof is design §17's complete role-typed status suite: exact wire keys, known 0/0, failed consumer observations,
watcher-set revisions, one-read requirements, first publish, heartbeat/freshness, malformed recovery, and operator
rendering.

Run focused unit/race and producer/consumer integration once. Run structural and semantic E2E once on the completed PR,
not during iteration.

## R4 — Atomic exact-alias and suffix cutover

**Prerequisite:** R3.

Primary surfaces:

- graph-index component/query/watermark/metrics
- graph-ingest component/query
- graph-query query/entity-resolver/GraphRAG
- graph constants, catalog, index/query response types, and exact entity types
- current gateway alias schema, result mapping, and contract tests before the later gqlgen replacement
- suffix ownership and retention tests

Atomic outcome:

- delete suffix provider, bucket, cache, writes, fallback scan, and partial-ID guessing;
- replace raw alias rows with paired `a2` lookup and `e2` owner memberships;
- owner-before-lookup add and lookup-before-owner delete ordering;
- cold bootstrap repairs both axes;
- exact resolution is absent, singular, or ambiguous with no collision winner;
- coverage is captured before scan;
- singular returns `AliasedExactEntity`;
- absent/ambiguous preserve coverage in classified detail; and
- no raw-key path, alias-or-ID fallback, shim, or migration helper.

Old alias and suffix storage is inert. Operators may discard/rebuild derived buckets through ordinary NATS
administration; SemStreams adds no NATS CLI dependency.

Owning truth:

- graph-index, graph-query, graph-ingest, and graph-retention OpenSpec
- graph-query README
- index reference, KV-key migration ledger, bucket catalog, and named clean-wipe documents

Proof is all thirteen alias gates and every suffix-retirement gate in design §17. Run focused unit/race and
graph-ingest/index/query integration once, then one structural E2E covering canonical ID, singular alias, ambiguous
alias, and suffix non-resolution.

Rollback is whole-slice only: stop affected binaries, revert, discard/rebuild derived buckets from `ENTITY_STATES`, and
restart one coherent version. Never add a dual reader for downgrade convenience.

## R5 — Derived-owner conformance

**Prerequisites:** R3 and R4. This milestone is owner-local work, never a shared-runtime project.

Recommended PRs:

- **R5a:** graph-index outgoing, incoming, predicate, name, and alias integration.
- **R5b:** spatial and temporal.
- **R5c:** embedding, then clustering after embedding's contract settles.

Each owner proves, as applicable:

- cold bootstrap and live update;
- source deletion and predicate retraction;
- restart;
- required-write failure;
- current-authority redrive;
- watcher loss or periodic failure;
- poison;
- honest readiness/currency; and
- no ready partial result.

Fix failures locally. Shared extraction is prohibited unless three owners prove identical semantics, less authored
production code, less adopter knowledge, and no hook maze. Such an extraction requires architect review and owner
change control before implementation.

R5b may develop beside R5a after R4. Embedding may develop beside R5b after R3, but clustering waits for embedding.
Shared-file reservations control merge order. Run focused race and integration tests; save broad E2E for R7.

## R6 — Canonical query catalog, hierarchy, and alternate-front deletion

**Prerequisite:** R4 and required R5 provider proofs.

Primary surfaces:

- graph-query query/entity-resolver/GraphRAG
- one internal typed catalog declaration
- graph-ingest query providers and graph query types
- typed operation/port declarations
- current gateway routing
- agentic tools, fusion/research, and other raw-provider consumers
- provider registrations in spatial, embedding, clustering, and anomalies
- `graph/query/client.go`
- service-manager `/graph/triples` and E2E callers
- retained `agentic.query.trajectory` typed resolver path, explicitly outside the graph catalog

Atomic outcome:

- exactly twenty canonical operations;
- `agentic.query.trajectory` remains a typed agentic resolver outside those twenty operations;
- registrations, ports, routing, and gateway bindings derive from the same declaration;
- four predicate operations route through graph-query;
- hierarchy exhausts cursors, deduplicates, enforces the 10,000-ID framework budget, and returns complete observed
  counts or no result;
- exact identity never guesses;
- legitimate internal callers move to narrow typed adapters;
- delete general `graph/query.Client`, `/graph/triples`, and unused provider operations; and
- no dynamic public subject registry.

The existing external gateway remains the current front until R7 but delegates only through canonical operations. This
is not a compatibility layer, and gqlgen is not partially introduced here.

Owning truth:

- graph-query OpenSpec and README
- PathRAG, query-access, spatial/temporal, and gateway-response documentation

Proof:

- exact operation-set equality across catalog, subscriptions, ports, routing, and gateway binding;
- every operation has a present typed consumer;
- trajectory remains reachable through its typed agentic resolver without entering the graph-operation catalog;
- all hierarchy completeness cases;
- no production raw graph subject or authority-KV application caller;
- removed service/provider routes are absent; and
- E2E operational behavior uses canonical operations.

Run focused unit/race and query-routing integration. R7 owns final external semantic E2E.

## R7 — gqlgen external front-door cutover

**Prerequisite:** R6.

Primary surfaces:

- `gateway/graph-gateway/`
- committed schema SDL and `gqlgen.yml`
- generated executor/models and thin resolver files
- gateway configuration/composition
- `go.mod`, `go.sum`, and GraphQL client/E2E helpers
- placeholder `/mcp` owner if it lives outside the gateway

Atomic outcome:

- replace the hand-written facade with gqlgen;
- graph resolvers call canonical typed graph operations only; trajectory delegates to its separate typed agentic
  operation and is not inserted into the twenty-operation graph catalog;
- conformant parsing, variables, operation names, aliases, fragments, selection, introspection, scalars, and errors;
- query-only schema;
- delete hand-written parser/executor/introspection/projection/response machinery;
- delete placeholder `/mcp`, capabilities fallback, and unknown-subject route; and
- no playground, mutation, or subscription surface.

Owning truth:

- graph-gateway README and package documentation
- query-pattern skill
- remaining GraphQL sections of query-access and architecture documentation

Proof:

- clean generation produces no diff;
- schema and resolver contract tests;
- trajectory GraphQL schema/resolver behavior remains present and typed through the cutover;
- no hand-written GraphQL engine symbols;
- `entityByAlias` exposes `AliasedExactEntity` and error currency;
- old hierarchy fields/arguments are rejected; and
- removed routes produce ordinary absence or GraphQL validation errors.

Run focused tests, gateway integration once, then structural and semantic E2E once. This is the final development-stage
expensive E2E milestone.

## R8 — Live-guidance and archive closure

**Prerequisite:** R7. Runtime semantics do not change. Owning live docs already moved with their code slices.

Complete remaining design §12.1 dispositions:

- correct ADR-090's live roadmap wording without changing accepted architecture;
- archive obsolete graph-state programs, decisions, inventory, and reviews;
- archive completed GS-01 with truthful completion;
- rewrite or archive suspended OpenSpec prerequisites;
- update cross-cutting graph-component and architecture docs;
- preserve historical ADRs and archived OpenSpec verbatim;
- preserve ADR-088 byte-for-byte; and
- retain accurate graph-ingest GS-05/rejected-r35 suffix history.

Run strict OpenSpec validation and targeted current-file checks. Historical artifacts are excluded from live-guidance
checks. No broad word-grep acceptance gate and no new concept document unless it replaces/removes stale guidance.

## R9 — Release candidate, downstream holdouts, and final tag

**Prerequisite:** R8.

Create an immutable SemStreams release-candidate tag from the fully verified program commit. Then migrate the ten
holdouts against that candidate: semdev, semmachina, semsource, semboids, semdragon, semstreams-ui, semteams,
semconnect, semlink, and semops.

For each repository record:

- used graph capabilities and old surface encountered;
- approved replacement and source migration;
- build/unit/integration result;
- feature-parity evidence and anti-pattern removed; and
- not-assessable capabilities due to project maturity.

Holdouts never reshape the target. Raw subject/bucket use, suffix identity, generic readiness, old facade, or aggregate
client use is fixed downstream. A legitimate capability absent from the approved target blocks the final tag and
triggers formal amendment—not a shim. An immature/non-buildable project is recorded as not assessable and does not by
itself block SemStreams; feature-parity falsification does.

Final candidate gate:

- lint, schema generation, and contract tests;
- focused race suites and integration once;
- structural and semantic E2E once;
- final complexity/deletion ledger;
- clean working tree and exact artifact identities.

Promote the candidate unchanged or fix forward and repeat affected focused proof plus final structural/semantic E2E.

## Shared-file reservations

These surfaces do not permit overlapping unmerged work:

- `graph/{constants,kvcatalog}.go`: R1 -> R2 -> R3 -> R4
- graph-index component/query/watermark: R3 -> R4 -> R5a
- graph-query query/entity-resolver: R4 -> R6 -> R7 resolver integration
- graph-gateway component/schema: R3 -> R4 -> R6 -> R7
- E2E entity stages: R3 before later query/front-door E2E
- readiness docs/specs: R3 owns until merge
- graph-query/index specs: R4, then R6/R7 admitted additions
- architecture/program-history docs: reserved for R8 after owning slices

R5b spatial/temporal may overlap development with R5c embedding after R3; clustering waits for embedding. No parallel
work may touch a file reserved by an earlier unmerged slice.

## Required handoff gates

### Architect

- link frozen approval and rulings;
- produce a file:line surface inventory and adopter-seam inventory;
- identify exact wire/storage cutover and delete list;
- declare shared-file ownership and prerequisites;
- confirm no target amendment; and
- define falsifiable proof before code.

### Developer

- begin with failing behavioral tests;
- implement only the slice and delete old paths in the same change;
- run focused unit/race and integration proof;
- update baton current truth and complexity delta; and
- add no shim, deprecated symbol, dual path, or fallback.

### Reviewer

- read frozen target and slice inventory;
- verify code/tests against exact rulings;
- inspect deletion proof and wire/storage atomicity;
- verify adopter knowledge decreased;
- reject speculative abstractions or semantic drift; and
- issue explicit approve/request-changes disposition.

### Technical writer

- update owning OpenSpec, docs, ADRs, and migration truth in the same PR;
- preserve historical artifacts where required;
- remove stale guidance instead of layering concepts; and
- run strict documentation/spec validation.

Any semantic mismatch returns to the architect. Any writer code change returns through reviewer. Merge requires all
four gates and a complete baton.

## Durable baton template

```markdown
# Post-GS-01 foundation baton — <slice>

## Identity
- approval:
- approved design SHA-256:
- design review SHA-256:
- baseline:
- slice PR/commit:
- rulings implemented:

## SemStreams identity packet
<verbatim ten-point packet from approval.md>

## Current truth
- merged prerequisite:
- implemented:
- not implemented:
- current wire/storage format:
- current test state:

## Surface inventory
| file:line | present behavior | target disposition |

## Adopter seam
| surface | must know | do-nothing behavior | discovery | should know |

## Atomic contract
- additions:
- replacements:
- deletions:
- prohibited shims/dual paths:

## Delete proof
- exact retired identifiers:
- allowed historical occurrences:
- current-source result:

## Verification
| command/scenario | result | duration | evidence |
- race:
- integration:
- designated E2E:

## Complexity ledger
- authored production lines added/removed:
- generated lines excluded:
- front doors before/after:
- buckets/streams/services before/after:
- adopter-visible concepts before/after:

## Risks and rollback
- known risk:
- clean revert/forward-fix posture:
- derived/operational state rebuild:
- mixed-version deployment prohibited:

## Blockers/falsifications
- target contradiction:
- missing legitimate capability:
- proof failure:
- owner amendment required:

## Shared-file ownership
- reserved files:
- conflicting live slices:

## Next slice
- prerequisite satisfied:
- first read-only inventory:
- inherited delete list:
- evidence still needed:

## Gates
- architect:
- developer:
- reviewer:
- technical writer:
```

## Rollback posture

There is no compatibility rollback protocol.

- Before merge, abandon or revert the slice branch.
- After merge, prefer a clean forward fix.
- For a storage/wire revert, stop affected binaries, revert the whole slice, discard/rebuild changed derived or
  operational state from current authority, and restart one coherent version.
- Never run mixed status, alias-layout, or GraphQL-contract binaries.
- Never add dual readers/writers, export/import helpers, recovery services, or a NATS CLI dependency.
- `ENTITY_STATES` remains authority. Operators retain ordinary NATS backup/checkpoint responsibility.

## Falsifiable final complexity gate

The program does not complete unless all are true:

- authored production code is net-negative against baseline `d1570ef8`; gqlgen output is excluded while SDL, config,
  resolvers, adapters, and glue count;
- no new bucket, stream, service, coordinator, general client, MCP surface, or universal derived-view runtime;
- exactly twenty canonical operations exist across declarations, ports, routing, and resolvers;
- graph-ingest remains sole `ENTITY_STATES` physical writer;
- conformant gqlgen GraphQL is the one external graph-read front door;
- ordinary internal callers use narrow typed operations/ports;
- retired identifiers have zero current-source references outside approved history;
- no shim, deprecated bridge, compatibility alias, fallback, or dual path;
- current guidance contains no live GS sequencing;
- all target proof gates pass;
- final structural and semantic E2E pass on the exact release candidate; and
- downstream findings resolve by migration, explicit non-assessability, or formal amendment—never compatibility.
