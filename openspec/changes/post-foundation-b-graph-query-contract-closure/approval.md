# Owner approval — post-Foundation-B graph query contract closure

## Evidence accepted

- Frozen inventory:
  `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
  `c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.
- Reviewed roadmap:
  `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md`, SHA-256
  `ff23db51ce7bf6e3d45da09a1706bf70ee548ae5e6aa2b12201ceeae64c4f343`.
- Slice D merged-tree reassessment:
  `docs/proposals/post-foundation-b-slice-d-optional-summary-serving-view-inventory.md`, SHA-256
  `6a28a0fe9349218baf07bf6d4d79bd89c6bc4ad483fa937e575974d30f499a6b`.
- Independent review passed for the original inventory and design before the 2026-08-09 owner approval. The reduced
  Slice D reassessment passed independent planning review, and its implementation passed independent
  `semstreams-reviewer` review on 2026-08-10. The hash-pinned reassessment retains its capture-time status text.

On 2026-08-09 the owner approved the following fourteen rulings. These are binding implementation constraints; a
deviation stops the affected slice for a new owner ruling.

## Binding rulings

1. Adopt bounded query closure (Option 3), not measurement-only patches or broad graph convergence.
2. Replace the community bucket-presence watcher with the generation supervisor and generation-validated reads.
3. Require a usable generation for `localSearch`; let `globalSearch` and `searchGraph` serve lower-tier results with
   explicit degradation when optional community enrichment is unavailable.
4. Admit one `graph.query/v1` port family: one provider input, the retained gateway family output, and exact named
   research outputs.
5. Delete the unadmitted agentic `search_graph`/`summarize_graph` wrappers completely: shared registrations, both
   builtin group/skip keys, registration functions, implementations, full exported type/option/constructor/querier
   surface, tests, docs, and expectations. Keep GraphQL/query operations and unrelated local-tool extensibility; add
   no no-op key, replacement tool, port system, or discovery redesign.
6. Give optional summaries one catalog-backed `pkg/graphview.View`: fail subsequent point reads closed after loss,
   retry/reconcile with the existing interval, and preserve statistical fallback without generation leases,
   readiness, or degradation coupling.
7. Seed `gateway-error-projection` and copy existing classified class/non-empty code into GraphQL extensions without
   creating new classification authority.
8. Give libraries no component ports; the component embedding fusion owns its six outputs and readiness declaration.
9. Remove only the provisional mixed direct-KV `graph/query.Client` cohort, including its client-only config, caches,
   watchers, readiness/poison state, methods, path/cache types, and examples.
10. Preserve `pkg/fusion/fusionnats.Client`, its constructor, six operations, lazy readiness behavior, and downstream
    role; migrate only its reply decoding and production-shape fixtures.
11. Remove GraphQL `capabilities`; retain exactly fourteen graph-query-backed and nineteen total served root fields.
12. Seed `gateway-query-routing`; keep response projection about success projection, seed `gateway-error-projection`,
    and update `graph-query` Purpose and normative consumer-representation requirements.
13. Correct stale graph-index hash/catalog text to the current raw `PREDICATE_INDEX` contract without runtime change.
14. Make a clean break: no shims/deprecation/dual paths, and communicate downstream breaks without auditing or fixing
    downstream projects.

On 2026-08-10 the owner approved a reduced Slice D after reassessing the merged Slice C tree. This supersedes only the
original roadmap's ruling 6 mechanism: optional summaries use the existing catalog-backed `pkg/graphview.View`, whose
subsequent point reads fail closed after watcher loss. Every unavailable summary outcome keeps the statistical
fallback. Because summary keys are content-addressed by membership, Slice D adds no generation ID, request lease,
final-response validation, readiness/`GRAPH_STATUS`, degradation metadata, metric contract, configuration, or
infrastructure.

The approved mechanism is one component-owned supervisor and at most one typed
`graphview.View[clustering.CommunitySummaryRecord]`. Loss notification is a nonblocking control signal. The supervisor
clears and stops the failed view before reopening the catalog reader and constructing/starting a replacement; failed
initial Starts and cancellation stop all owned view state. One mutex guards the published pointer. The decoder verifies
canonical key/hash, exact key-record identity, closed enhanced/failed status, and non-empty enhanced content; failed
records are absence and malformed, mismatched, unknown, or invalid-enhanced records are poison.

## Approved implementation clarification

The exact-field ruling makes introspected `semanticSearch` the sole GraphQL spelling. The current hidden
`similaritySearch` request/response spelling and its in-repo E2E consumer migrate atomically and are deleted without an
alias. This is a same-class correction required by rulings 11 and 14, not a new operation or producer wire change.
