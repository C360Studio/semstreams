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
- Accepted Slice E inventory:
  `docs/proposals/post-foundation-b-slice-e-embedded-decoding-result-truth-inventory.md`, SHA-256
  `2033480aa58b9cb8906d4efd08e57d5e19fa71f0050c2cd98cd873c2f67bcf5e`.
- Reviewed Slice E design:
  `docs/proposals/post-foundation-b-slice-e-bounded-embedded-decoding-design.md`, SHA-256
  `49e838133a2eb785f66314bf4a625b8e6f4888783f51a5d7ee6e3ea72292cc42`.
- Independent review passed for the original inventory and design before the 2026-08-09 owner approval. The reduced
  Slice D reassessment passed independent planning review, and its implementation passed independent
  `semstreams-reviewer` review on 2026-08-10. The bounded Slice E design passed independent pre-owner review, and the
  owner approved all eight Slice E rulings on 2026-08-10. The hash-pinned reassessments retain their capture-time status
  text.

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
8. Give libraries no component ports. Slice E assigns no component port or configuration ownership to fusion because
   no current in-repo component constructs `fusionnats.Client`.
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

## Approved Slice E reassessment

On 2026-08-10 the owner approved all eight rulings in the independently reviewed bounded Slice E design. These rulings
supersede only the original roadmap's broader Slice E decoder/representation scope, false fusion-host port premise,
stale private `similaritySearch` compatibility, phantom query `RequestID`, and E2E gate claims:

1. Adopt bounded Slice E closure rather than defect-only patches or broad decoder closure.
2. Use `graph.UnwrapQueryResponse` exactly once at only the three in-slice request/reply adapter boundaries:
   research-classify, research-execute, and fusionnats. Fusion Status remains KV state.
3. Validate `graph.ExactEntity.KVRevision` without adding it to `fusion.Entity`; project only the existing ID/triples
   result after validation.
4. Remove the nonexistent fusion-host component/port requirement. Slice E adds no component, configuration, Registry,
   or readiness declaration.
5. Keep receiver-less research projections unchanged. Do not widen CandidateSet or Evidence for facts they do not
   currently model.
6. Delete the private `similaritySearch` wrapper without compatibility.
7. Delete the exported but unused query `RequestID` field and discriminator key without compatibility.
8. Use focused race/real-NATS tests plus two strengthened existing E2E seams: research-graph requires a nonzero
   production classify candidate count, and statistical `test-http-gateway` requires exact live strategy `graphrag`
   with all pre-assertion gateway failures made hard. Exhaustive representation and strategy branches remain in
   focused tests; add no stage, tier, or semantic E2E requirement.

Implementation tasks E.1-E.8 remain unchecked until their evidence exists and independent implementation review
passes.

## Approved implementation clarification

The exact-field ruling makes introspected `semanticSearch` the sole GraphQL spelling. The current hidden
`similaritySearch` request/response spelling and its in-repo E2E consumer migrate atomically and are deleted without an
alias. This is a same-class correction required by rulings 11 and 14, not a new operation or producer wire change.

## Approved Slice F sequencing clarification

On 2026-08-10 the owner approved splitting complexity deletion into two independently implemented and reviewed slices.

- F1 deletes only the provisional mixed direct-KV `graph/query.Client` cohort under rulings 9 and 14. It preserves all
  admitted query responders and operation-specific adapters. The graph-index activation/tombstone integration test
  migrates from the retired client to the existing production `graph.query.pathSearch` operation; no replacement
  general client is added.
- F2 separately deletes the unadmitted agentic `search_graph` and `summarize_graph` wrappers under rulings 5 and 14.

F1 lands and is independently reviewed before F2 begins. Neither slice may absorb the other or unrelated issue work.
