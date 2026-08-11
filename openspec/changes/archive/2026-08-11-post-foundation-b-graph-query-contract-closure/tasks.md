# Tasks — post-Foundation-B graph query contract closure

No implementation task was complete at promotion time. Each slice receives independent `semstreams-reviewer` approval
before the next slice lands. A later slice MUST NOT reopen the fourteen owner rulings without implementation evidence
of an internal contradiction and a new owner ruling.

## P. Promotion and frozen evidence

- [x] P.1 Retain the accepted inventory and reviewed roadmap with their SHA-256 provenance.
- [x] P.2 Record all fourteen owner rulings and the `semanticSearch` clean-cutover clarification.
- [x] P.3 Validate every target-state delta strictly before production implementation begins.
- [x] P.4 Obtain independent review of the promoted OpenSpec change.

### Slice E promotion review evidence (2026-08-10)

- Independent review approved the Slice E promotion after task truth was corrected, archive language was bounded to
  research projection/result truth, and the exported `graph.QueryResponse.RequestID` break was added to adopter and
  downstream migration guidance.

## A. Gateway surface and error truth

- [x] A.1 Add failing conformance tests for exactly nineteen introspected root fields, the exact fourteen graph-query
  subset, one production route/response field per root, and absence of `capabilities`.
- [x] A.2 Remove the `capabilities` field, type, route, response mapping, fixtures, and documentation with no responder,
  alias, stub, or deprecated field.
- [x] A.3 Make `semanticSearch` the sole semantic-search root and response key; migrate the owned E2E search executor,
  fixtures, and adopter documentation from hidden `similaritySearch` atomically, leaving no gateway alias.
- [x] A.4 Pass the original `error` to graph-gateway error projection and copy only existing classified class and
  non-empty code into `errors[].extensions`; expose no detail and infer nothing.
- [x] A.5 Prove gateway-local invalid input retains HTTP 400, handler-side classified failure retains HTTP 200,
  transport behavior is unchanged, plain errors have no extensions, and uncoded classified errors expose class only.
- [x] A.6 Run focused race tests, strict OpenSpec validation, schema/no-drift checks, contract tests, and the
  statistical E2E gateway/search path before the breaking slice lands.
- [x] A.7 Obtain independent review for Slice A.

## B. Operation inventory and atomic port cutover

- [x] B.1 Add failing tests for one internal inventory containing exactly the current sixteen operations, with stable
  subjects/request/success types, GraphQL exposure, consumers, and availability outcomes.
- [x] B.2 Install all sixteen responders on every successful graph-query Start and return classified
  `index_not_ready` from `localSearch` until a usable community generation exists.
- [x] B.3 Replace graph-query's effective wildcard declaration with one required `graph_queries` `nats-request` input
  for `graph.query.*`, interface `graph.query/v1`, and derive all responder subjects from that resolved family.
- [x] B.4 Retain graph-gateway's three required outputs and version `graph_queries` as `graph.query/v1`.
- [x] B.5 Add one required `searchGraph` output to research classify and four required
  `batch`/`relationships`/`temporal`/`searchGraph` outputs to research execute; add no agentic query outputs.
- [x] B.6 Load all twenty-one shipped configs through production factories and Registry. Prove eleven query, eight
  gateway, two classify, two execute, and nine agentic-tools instances; raw `395/243/54`, effective `571/378/69`, and
  unchanged delta `176/135/15`.
- [x] B.7 Reject unknown operations, out-of-family declarations, missing/mismatched interface facts, and any effective
  adapter request without its exact declared output.
- [x] B.8 Run focused race/integration, schema/no-drift, contract, strict OpenSpec, and relevant E2E gates.
- [x] B.9 Obtain independent review for Slice B.

## C. Community generation supervisor

- [x] C.1 Add explicit-synchronization failing tests for absent bucket, staging, sentinel publication, usable state,
  update/delete, unexpected watch close while the bucket remains, replacement, and orderly cancellation.
- [x] C.2 Replace bucket-presence recovery with component-lifetime open/`WatchAll` retry and monotonically identified
  fresh private generations. Never seed, copy, retain, or serve an old generation map.
- [x] C.3 Publish only after the initial sentinel, unpublish the exact generation before retry on unexpected loss, and
  prevent late generation-N updates/exits from affecting N+1.
- [x] C.4 Make every community-backed access lease and finally validate one generation. Exercise `localSearch`,
  `globalSearch`, and `searchGraph` across all lifecycle states.
- [x] C.5 Require a usable generation for `localSearch`; let lower-tier global/searchGraph results serve with
  `degraded=true`, `degraded_reason=community_cache_not_ready` when requested enrichment is unavailable.
- [x] C.6 Add no readiness producer/key, service, bucket, stream, metric contract, or retry/configuration surface.
- [x] C.7 Run focused race and real-NATS integration tests with no arbitrary sleeps and obtain independent review.

### Slice C gate evidence (2026-08-10)

- Explicitly synchronized generation and query-behavior tests cover staging, sentinel publication including an empty
  generation, update/delete, watch loss with the bucket still present, fresh replacement, stale-generation fencing,
  final lease validation, degradation, and orderly cancellation.
- `task lint` and `go test -race ./...` passed.
- `scripts/run-integration-tests.sh` passed, including the real-NATS graph-query lifecycle, orderly-cancellation, and
  response-enrichment paths.
- `task schema:generate` produced no schema or spec drift; `go test ./test/contract/...` passed.
- `task openspec:validate` and the breaking-change gate `task e2e:statistical` passed.
- Independent `semstreams-reviewer` review approved Slice C after its blocking and high findings were remediated and
  the affected focused race and real-NATS tests were rerun.

## D. Optional-summary serving view

- [x] D.1 Add explicit-synchronization failing tests for absent and late buckets, replay staging and empty caught-up,
  update/delete/purge, typed decode/poison, nonblocking loss signaling, failed-Start cleanup, loss/reopen/replacement,
  ghost removal, single-pointer publication, and orderly cancellation.
- [x] D.2 Replace the bucket-presence watcher, once guard, raw KV handle, shared summary map, and bespoke watcher with
  one component-owned supervisor and catalog-backed `pkg/graphview.View[clustering.CommunitySummaryRecord]` projection.
- [x] D.3 Make subsequent point reads fail closed after view loss. The sole supervisor receives a nonblocking loss
  signal, clears and stops the exact failed view, reopens the catalog reader, then constructs/starts one replacement
  using the existing recheck interval. Stop failed initial Starts and the current view on cancellation; use no summary
  generation ID, request lease, or final-response validation.
- [x] D.4 Preserve statistical fallback for absent, late, staging, empty, failed, stopped, poisoned, and not-found
  summaries without `index_not_ready`, readiness/`GRAPH_STATUS`, degradation metadata, metric contract, config, or new
  infrastructure.
- [x] D.5 Run focused race and real-NATS integration tests with no arbitrary sleeps and obtain independent review.

### Slice D gate evidence (2026-08-10)

- Explicitly synchronized tests cover absent and late buckets, replay staging and empty caught-up views,
  update/delete/purge, typed decode and per-key poison, nonblocking coalesced loss and poison signals, failed-Start
  cleanup, loss/reopen/replacement, ghost removal, single-pointer publication, and orderly cancellation. A controlled
  blocking logger proves poison observability cannot stall later view updates or deletes.
- `go test -race ./processor/graph-query -count=1`, `task lint`, and `go test -race ./...` passed.
- `scripts/run-integration-tests.sh` passed, including the real-NATS late-attach and loss/reopen/no-ghost paths.
- `task schema:generate` produced no schema or spec drift; `go test ./test/contract/...` passed.
- `task openspec:validate` and the breaking-change gate `task e2e:statistical` passed all 41 steps.
- Independent `semstreams-reviewer` review approved Slice D after its blocking finding about poison observability was
  remediated and the affected focused race and real-NATS tests were rerun.
- Slice D changes only the optional-summary consumer serving view. It does not close #609's producer cold-start
  remainder, #608/#829 producer and content-quality work, or #710's future measurement-gated retention design.

## E. Bounded embedded decoding and truthful query outcomes

- [x] E.1 Add failing focused tests showing: full-entity-only searchGraph success becomes zero research candidates;
  fusion Entity rejects the real ExactEntity shape; successful global-search strategies are blank; and temporal/spatial
  `id` rows become zero IDs.
- [x] E.2 Pass each success through `graph.UnwrapQueryResponse` exactly once at the research-classify,
  research-execute, and fusionnats request/reply boundaries. Prove current bare and equivalent standard-enveloped
  fixtures agree without changing producer declarations.
- [x] E.3 Project validated full entities into the existing research Candidate contract only when digests are absent.
  Preserve order, limit, digest behavior, and degradation. Do not widen CandidateSet or Evidence.
- [x] E.4 Decode fusion Entity replies as `graph.ExactEntity`; validate matching entity and nonzero revision, then
  project only ID and triples into the existing `fusion.Entity`. Preserve the six-method interface, six request-subject
  mapping, constructor, lazy readiness, ordering, similarity, missing reasons, and relationship direction.
- [x] E.5 Decode temporal and spatial result IDs from canonical `id`; populate the existing terminal strategy on every
  successful global-search path, including empty and fallthrough success.
- [x] E.6 Delete the unreachable private `similaritySearch` wrapper decoder and tests. Delete the unused
  `QueryResponse.RequestID`, discriminator key, and request-ID envelope fixtures. Add no alias or compatibility path.
- [x] E.7 Remove every claim that a nonexistent embedding/fusion-host component owns six fusion outputs or readiness.
  Make no Slice E component, configuration, Registry-count, schema, service, bucket, stream, or readiness change.
- [x] E.8 Within the existing research-graph stages, seed one canonical entity through `graphmutation.Client.Create`,
  register and lifecycle-manage a test-owned `graph.embedding.query.search` responder returning that entity ID, and
  require `graph-query` to be healthy. Add no new stage. Then read `research.classify.candidate-count` from the
  production classify result and fail unless it is a parsed integer greater than zero. In the existing
  `test-http-gateway` stage, add `strategy` to the controlled `globalSearch("robot warehouse", level:0)`
  selection/response, require exactly `graphrag`, and make every earlier marshal/request/transport/status/read/parse/
  GraphQL failure return a hard error so the assertion cannot false-green. Run `task e2e:research-graph`,
  `task e2e:statistical`, focused package tests under race, the focused real-NATS fusion integration test, and
  independent SemStreams review; add no stage or tier.

## F1. Provisional aggregate query-client deletion

- [x] F1.1 Add failing source-surface checks for absence of `Client`, client `Config`, `NewClient`,
  `NewClientWithMetrics`, `PathQuery`, `PathResult`, and `CacheStats`, while proving classifier/search-option symbols
  remain.
- [x] F1.2 Migrate the graph-index activation/tombstone integration test to the production
  `graph.query.pathSearch` responder with a canonical test-owned exact-read provider; retain the existing public
  incoming and clustering assertions.
- [x] F1.3 Delete only the client implementation, interface/types, prefix extension, client-only tests/benchmarks,
  package client documentation, and current operational claims.
- [x] F1.4 Prove `graph.ExactEntityReader`, `pkg/projection.MutationClient`, graph-query responders,
  classifier/search-option code, research adapters, GraphQL routing, and `pkg/fusion/fusionnats.Client` remain.
- [x] F1.5 Run focused race and integration tests, full race tests, lint, schema/no-drift, contract tests, strict
  OpenSpec validation, `task e2e:statistical`, and independent SemStreams review.

### Slice F1 gate evidence (2026-08-10)

- RED caught all seven retired exported names and an alias mutation before implementation.
- The exact ten approved client files were deleted: `client.go`, `interface.go`, `prefix.go`, `client_test.go`,
  `incoming_shard_integration_test.go`, `readiness_gate_test.go`, `prefix_test.go`, `path_benchmark_test.go`, `doc.go`,
  and `README.md` under `graph/query`.
- The graph-index integration test now uses production `graph.query.pathSearch` with a test-owned exact responder and
  retains its incoming and clustering assertions.
- Preservation checks found no F2 reach, replacement general client, compatibility shim, deprecated alias, or copied
  traversal client.
- Focused race and targeted integration tests passed, followed by full `go test -race ./...` and Docker
  `scripts/run-integration-tests.sh`.
- Lint, schema generation with no drift, contract tests, strict OpenSpec validation (39/39), and statistical E2E
  (41/41) passed.
- Independent `semstreams-reviewer` gave final **APPROVE** after two medium documentation and AST-guard fixes.

## F2. Unadmitted agentic wrapper deletion

- [x] F2.1 Add failing source and behavior guards for the complete exported,
  registration, skip-key, category-alias, discovery, dispatch, permissive-
  allowlist, and operation-consumer surfaces.
- [x] F2.2 Delete exactly the six framework wrapper implementation,
  registration, and test files; remove both shared builtin keys/gates, all nine
  exported symbols, and stale `graph_search`/`graph_summary` category entries.
  Do not prohibit application-local reuse of either former name. Add no shared
  replacement, reserved name, alias, shim, port, subject, client, MCP surface,
  or config field.
- [x] F2.3 Make stale deleted `SkipBuiltins` values fail through existing
  closed-set validation. Preserve open-vocabulary
  allow/default/approval/retry fields, nil/empty `AllowedTools` semantics for
  registered tools, admission-before-approval ordering, ApprovalFilter-before-
  registry ordering on the wire path, application-local registration,
  local-over-shared discovery, and local-first dispatch.
- [x] F2.4 Update the graph-query operation consumer inventory, temporary F1
  preservation guard, current docs, research adapter comments, and generated
  research-classify description. Preserve historical ADR/archive evidence and
  the independent research provenance spelling `Source: "search_graph"`.
- [x] F2.5 Prove GraphQL fields, all sixteen responders, research adapters,
  fusionnats, exact reads, projection, classifier/search options, five direct
  `query_*` tools, and selected `research_graph` remain unchanged.
- [x] F2.6 Run focused and full race tests, lint, schema/no-drift review,
  contract tests, strict OpenSpec validation, `task e2e:agentic`, and
  `task e2e:research-graph`; obtain independent SemStreams review before the
  breaking F2 commit lands.

### Slice F2 gate evidence (2026-08-10)

- RED caught the exact six wrapper files, nine exports, two registration functions, two builtin/skip keys, stale
  category aliases, skip behavior, shared discovery, and operation-consumer claims.
- Negative mutation runs reintroduced the `search_graph` key and removed `ResearchGraphToolName`; both corresponding
  guards failed, and the tree was checksum-restored after each run.
- Implementation deleted exactly the approved wrapper cohort and added no replacement, alias, reserved name, shim,
  port, subject, client, MCP surface, or configuration field.
- Real-NATS behavior proved ApprovalFilter ordering before registry miss and ordinary application-local reuse of both
  former names through existing admission, discovery, approval, and dispatch rules.
- Preservation checks retained GraphQL fields, all sixteen responders, research adapters and provenance, fusionnats,
  exact reads, projection, classifier/search options, five direct `query_*` tools, and selected `research_graph`.
- Focused and full race tests, full Docker integration, lint, contract tests, and strict OpenSpec validation (39/39)
  passed. Schema generation changed only the expected research-classify description.
- Agentic and research-graph E2E tiers passed. Independent `semstreams-reviewer` gave final **APPROVE** after the
  checkpoint-identity correction.

## G. Spec correction, release evidence, and archive

- [x] G.1 Confirm the graph-index predicate correction documents the shipped raw nine-token `PREDICATE_INDEX`, absent
  `PREDICATE_CATALOG`, NAME hashing on the name axis, NAME and INCOMING reversible predicate hex, and no runtime index
  migration.
- [x] G.2 On archive, update the current `graph-query` Purpose to name the admitted operation family, versioned port
  contract, stable responders, generation-safe optional-view caches, success decoding, bounded research projection,
  and truthful query outcomes, while explicitly excluding a public subject catalog and general embedded client.
- [x] G.3 Publish one downstream migration notice covering every accepted adopter break and consequence, including
  GraphQL spellings, retryable `localSearch`, deleted Go/tool surfaces, open-vocabulary F2 configuration behavior,
  category fallback, `graph.QueryResponse.RequestID`, and `graph.query/v1` declarations; perform no downstream audit.
- [x] G.4 Produce fourteen primary ruling-conformance rows plus affected-row addenda mapping every later binding Slice
  D, E, F1, and F2 clarification or approval condition to final `file:line` evidence or an owner-approved deviation.
- [x] G.5 Run the correction-propagation sweep over every mutable active artifact, task, migration note, schema,
  fixture, and cited mechanism. Preserve hash-pinned capture-time artifacts verbatim and disposition stale claims in
  new evidence. Record only reproducible in-tree or CI evidence.
- [x] G.6 Run `task lint`, `go test -race ./...`, integration tests, schema generation/no drift, contract tests, strict
  OpenSpec validation, and relevant statistical, semantic, agentic, and research E2E tiers with active monitoring.
- [ ] G.7 Obtain final independent review, merge all implementation slices, verify merged-tree negative searches and
  gates, conservatively update task truth, and archive the OpenSpec change.

### Checkpoint 1 evidence (2026-08-11)

- G.1 now records shipped code truth: NAME hashes only its name axis; NAME and INCOMING retain reversible predicate
  hex; PREDICATE uses the canonical raw nine-token key; no predicate catalog exists. This is a documentation
  correction, not a runtime index migration.
- G.3 is published at `docs/operations/migration-post-foundation-b-graph-query-contract-closure.md`. It covers every
  accepted adopter break and consequence, names the surface-specific discovery path, and leaves downstream audit and
  migration downstream-owned.
- G.4 is recorded at `docs/proposals/post-foundation-b-g-ruling-conformance.md`: fourteen primary ruling rows plus the
  binding Slice D, E, F1, and F2 addenda, each mapped to final in-tree evidence or an explicit disposition.
- G.5 corrected mutable active design, approval, task, specification, and migration layers. Hash-pinned and
  baseline-identified capture artifacts remain preserved; stale capture-time claims are dispositioned in the new
  conformance evidence rather than rewritten.
- The approved plan freezes the exact archive-time Purpose. The current `openspec/specs/graph-query/spec.md` is
  untouched, so G.2 remains incomplete.
- Checkpoint 1 changes no Go source, generated schema, fixture, runtime behavior, or downstream repository. G.6 and
  G.7 remain incomplete pending the merged-tree gate, final review, merge verification, and archive transaction.

### Checkpoint 2 archive-candidate evidence (2026-08-11)

- The exact corrected implementation tree is commit `cbbc907e`. `task lint`, `go test -race ./...`,
  `task test:integration` (including `natsclient` in 102.483s), `task schema:generate` with no generated schema/spec
  drift, `go test ./test/contract/...`, and strict `task openspec:validate` (39/39) passed on that tree.
- The required E2E tiers passed with active monitoring: statistical (41/41, about 29.18s), semantic (48/48, about
  11m17s, exact `graphrag`, 7/7 known answers), agentic, research-graph with a positive candidate count, and
  deep-research.
- The first semantic run exposed a test-only mismatch: the HTTP gateway caller allowed 10s while server-side answer
  synthesis allowed 15s. Bounded correction `cbbc907e` changed only that caller to the existing 60s helper and made
  the helper comments truthful. Independent review approved the correction; the rerun passed with the gateway
  response arriving in 43.6s.
- All nine named frozen-artifact checksum sidecars passed. Focused Slice F1, Slice F2, gateway-query closure, and
  sixteen-operation inventory guards passed. Production-source searches found no retired GraphQL alias or
  capabilities field, F2 wrapper declaration, `QueryResponse.RequestID`, or graph-index `PREDICATE_CATALOG`.
- `openspec archive -y post-foundation-b-graph-query-contract-closure` materialized this archive, promoted all deltas,
  and the current `graph-query` Purpose now contains the exact owner-approved capability statement. G.2 and G.6 are
  therefore complete on this archive candidate.
- Strict post-archive `task openspec:validate` passed all 40 materialized specs/changes, `git diff --check` passed, and
  both moved Slice F2 checksum sidecars still verify in the archive directory.
- G.7 remains unchecked. Final independent review must be tied to the exact archive commit; every GitHub CI check for
  that unchanged commit must pass before merge. The review and unchanged merge are completion evidence and cannot be
  claimed by the diff still awaiting them.
