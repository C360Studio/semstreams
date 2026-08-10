# Tasks — post-Foundation-B graph query contract closure

No implementation task was complete at promotion time. Each slice receives independent `semstreams-reviewer` approval
before the next slice lands. A later slice MUST NOT reopen the fourteen owner rulings without implementation evidence
of an internal contradiction and a new owner ruling.

## P. Promotion and frozen evidence

- [x] P.1 Retain the accepted inventory and reviewed roadmap with their SHA-256 provenance.
- [x] P.2 Record all fourteen owner rulings and the `semanticSearch` clean-cutover clarification.
- [x] P.3 Validate every target-state delta strictly before production implementation begins.
- [x] P.4 Obtain independent review of the promoted OpenSpec change.

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

- [ ] D.1 Add explicit-synchronization failing tests for absent and late buckets, replay staging and empty caught-up,
  update/delete/purge, typed decode/poison, nonblocking loss signaling, failed-Start cleanup, loss/reopen/replacement,
  ghost removal, single-pointer publication, and orderly cancellation.
- [ ] D.2 Replace the bucket-presence watcher, once guard, raw KV handle, shared summary map, and bespoke watcher with
  one component-owned supervisor and catalog-backed `pkg/graphview.View[clustering.CommunitySummaryRecord]` projection.
- [ ] D.3 Make subsequent point reads fail closed after view loss. The sole supervisor receives a nonblocking loss
  signal, clears and stops the exact failed view, reopens the catalog reader, then constructs/starts one replacement
  using the existing recheck interval. Stop failed initial Starts and the current view on cancellation; use no summary
  generation ID, request lease, or final-response validation.
- [ ] D.4 Preserve statistical fallback for absent, late, staging, empty, failed, stopped, poisoned, and not-found
  summaries without `index_not_ready`, readiness/`GRAPH_STATUS`, degradation metadata, metric contract, config, or new
  infrastructure.
- [ ] D.5 Run focused race and real-NATS integration tests with no arbitrary sleeps and obtain independent review.

## E. Embedded decoding, result truth, and fusion preservation

- [ ] E.1 Migrate every admitted research and fusion adapter to exactly one `graph.UnwrapQueryResponse` pass before
  operation decoding; accept equivalent bare and enveloped production fixtures.
- [ ] E.2 Preserve full entities, entity digests, community summaries, synthesized answer, and degradation metadata;
  do not collapse a successful representation into count-only or invented empty success.
- [ ] E.3 Populate a non-empty canonical terminal strategy on every successful global-search outcome, including empty
  and fallback success; report the strategy that produced the returned result.
- [ ] E.4 Preserve `pkg/fusion/fusionnats.Client`, `New(requester, timeout)`, optional Close, lazy graph-index
  readiness, downstream role, and exactly six operations.
- [ ] E.5 Exercise all six fusion operations plus readiness with real producer shapes; decode entity as
  `graph.ExactEntity`, preserving revision, ordering, similarity, missing reasons, and relationship direction.
- [ ] E.6 Keep libraries free of component ports; the actual embedding component owns six required outputs and its
  `GRAPH_STATUS` KV-read declaration.
- [ ] E.7 Run focused race/integration and semantic/research E2E gates and obtain independent review.

## F. Complexity deletion

- [ ] F.1 Delete only the provisional `graph/query.Client` cohort: client-only configuration/defaults,
  constructors, direct bucket/RPC state, cache/watch/readiness/poison, query methods, client-only path/cache types,
  tests, and examples.
- [ ] F.2 Prove `graph.ExactEntityReader`, `pkg/projection.MutationClient`, classifier/search-option code,
  component-local research adapters, and `pkg/fusion/fusionnats.Client` remain.
- [ ] F.3 Delete `search_graph` and `summarize_graph` from shared/local discovery, `RegisterBuiltins`,
  `BuiltinGroupKeys`, accepted `SkipBuiltins`, implementations, registration functions, complete exported
  type/option/constructor/querier surfaces, tests, schemas, docs, and expectations.
- [ ] F.4 Prove stale skip values fail existing closed-set validation and non-reserved local executor registration,
  discovery, and dispatch precedence remain unchanged.
- [ ] F.5 Prove deletion does not reach GraphQL `searchGraph`/`graphSummary`, graph-query responders, research
  consumers, fusion, exact reads, projection, or classifier code.
- [ ] F.6 Run focused race, schema/no-drift, agentic and research E2E gates and obtain independent review.

## G. Spec correction, release evidence, and archive

- [ ] G.1 Confirm the graph-index predicate correction documents the shipped raw nine-token `PREDICATE_INDEX`, absent
  `PREDICATE_CATALOG`, NAME hashing, and INCOMING-only reversible predicate hex without runtime index change.
- [ ] G.2 On archive, update the current `graph-query` Purpose to name the admitted operation family, versioned port
  contract, stable responders, generation-safe optional-view caches, success decoding, and representation
  preservation, while explicitly excluding a public subject catalog and general embedded client.
- [ ] G.3 Publish one downstream migration notice covering removed GraphQL fields/spellings, deleted Go/tool surfaces,
  `graph.query/v1` port declarations, and retryable `localSearch`; perform no downstream implementation or audit.
- [ ] G.4 Produce a fourteen-row ruling-conformance table mapping every ruling to final `file:line` evidence or an
  owner-approved deviation; no sentence-only conformance claim is sufficient.
- [ ] G.5 Run the correction-propagation sweep over every change artifact, task, migration note, schema, fixture, and
  cited mechanism. Record measured evidence only from reproducible in-tree or CI artifacts.
- [ ] G.6 Run `task lint`, `go test -race ./...`, integration tests, schema generation/no drift, contract tests, strict
  OpenSpec validation, and relevant statistical, semantic, agentic, and research E2E tiers with active monitoring.
- [ ] G.7 Obtain final independent review, merge all implementation slices, verify merged-tree negative searches and
  gates, conservatively update task truth, and archive the OpenSpec change.
