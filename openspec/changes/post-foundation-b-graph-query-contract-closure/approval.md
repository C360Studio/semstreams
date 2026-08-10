# Owner approval — post-Foundation-B graph query contract closure

## Evidence accepted

- Frozen inventory:
  `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
  `c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.
- Reviewed roadmap:
  `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md`, SHA-256
  `ff23db51ce7bf6e3d45da09a1706bf70ee548ae5e6aa2b12201ceeae64c4f343`.
- Independent inventory and design review passed before owner approval.

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
6. Give optional summaries their own fresh-map generation supervisor, with sentinel publication, immediate
   unpublish-on-loss, retry while the bucket remains, and statistical fallback without readiness/degradation coupling.
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

## Approved implementation clarification

The exact-field ruling makes introspected `semanticSearch` the sole GraphQL spelling. The current hidden
`similaritySearch` request/response spelling and its in-repo E2E consumer migrate atomically and are deleted without an
alias. This is a same-class correction required by rulings 11 and 14, not a new operation or producer wire change.
