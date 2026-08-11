# Post-Foundation-B G closeout inventory

**Status:** Owner accepted as Checkpoint 1 evidence on 2026-08-11 after `INVENTORY PASS`. Inventory only; no
implementation, task-completion, merge, archive, or issue-execution authority.

**Merged baseline:** `08c03b4d48414b2daba1ca443c29202c0152e4f6`

**Scope:** The remaining G.1-G.7 closeout surface for
`post-foundation-b-graph-query-contract-closure`, including current truth conflicts, missing release evidence, and
adopter outcomes. This artifact records no target state, sequencing recommendation, new framework design, or owner
ruling.

## Evidence boundary

- All file and line references describe the merged baseline above.
- Negative searches report only repository-local evidence. Downstream repositories were not audited.
- A `READY` disposition means the evidence needed to begin that bounded documentation or proof step is present. It
  does not mark the task complete or authorize implementation.
- A `BLOCKED` or `NOT READY` disposition preserves the corresponding unchecked task state.

## Surface inventory

### G.1 currently states a false representation boundary

The shipped layouts are:

- `PREDICATE_INDEX` stores the raw canonical three-token predicate followed by the six-token entity ID. The key
  builder returns `predicate + "." + entityID`: `processor/graph-index/predicate_index.go:12-16`.
- Production Go contains no `PREDICATE_CATALOG` reference at this baseline. The current graph-index specification's
  hash/catalog paragraph is therefore stale: `openspec/specs/graph-index/spec.md:387-391`.
- NAME hashes its open-content name axis, then appends the entity ID and a reversible hex-encoded predicate:
  `processor/graph-index/name_index.go:49-62`. NAME decodes that predicate on reads:
  `processor/graph-index/name_index.go:114-143`.
- INCOMING likewise writes and reads a reversible hex-encoded predicate:
  `processor/graph-index/incoming_index.go:20-42,58-99`.
- The shared codec explicitly names INCOMING, NAME, and CONTEXT reverse-index consumers and describes reversible hex
  as a physical, non-authoritative codec: `graph/predicate_codec.go:5-22`.

The current specification already states the general NAME-and-INCOMING truth at
`openspec/specs/graph-index/spec.md:314-319`. The active delta narrows it incorrectly: its prose permits predicate hex
only for INCOMING at
`openspec/changes/post-foundation-b-graph-query-contract-closure/specs/graph-index/spec.md:3-11`, and its scenario says
“INCOMING alone” at
`openspec/changes/post-foundation-b-graph-query-contract-closure/specs/graph-index/spec.md:19-24`. G.1 repeats that false
claim at `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:222-223`.

The correction required before G.1 can proceed is documentation-only:

- NAME hashes the name axis.
- NAME and INCOMING use reversible predicate hex in their composite reverse-index layouts.
- PREDICATE uses the raw canonical predicate in its nine-token membership key.
- `PREDICATE_CATALOG` is absent.
- None of those physical representations is predicate acceptance authority.
- No runtime index migration is authorized.

That boundary matches the accepted roadmap's documentation-only intent at
`docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:501-507` and its stop condition against a
runtime representation migration at
`docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:645-647`.

### G.2-G.5 have identifiable documentation and proof gaps

- G.2 is not yet satisfied. The current graph-query Purpose still describes only the semantic strategy and defers
  other strategies until touched: `openspec/specs/graph-query/spec.md:3-7`. The active task requires the admitted
  operation family, versioned port contract, stable responders, optional-view cache behavior, success decoding,
  bounded research projection, truthful outcomes, and explicit exclusion of a public subject catalog and general
  embedded client: `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:224-226`.
- G.3 is specified but its standalone downstream migration notice is missing. The task names the required breaks and
  communicate-only boundary at
  `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:227-229`. The accepted roadmap already
  records compiler, boot-validation, Registry, and query-error consequences at
  `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:638-656`.
- G.4 is specified but its fourteen-row ruling-conformance artifact is missing. The sole active requirement is the
  task at `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:230-231`; no standalone table exists
  at this baseline.
- G.5 remains necessary because active artifacts retain stale completion text. The design still says Slice E
  implementation remains pending at
  `openspec/changes/post-foundation-b-graph-query-contract-closure/design.md:3-7`, and approval still says E.1-E.8
  remain unchecked at
  `openspec/changes/post-foundation-b-graph-query-contract-closure/approval.md:95-96`. The sweep obligation is recorded
  at `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:232-233`.

### G.5 also has outer-layer capture and migration conflicts

The accepted active inventory is a capture-time artifact, not current merged-tree truth. It identifies its exact
baseline and checkpoint provenance at
`openspec/changes/post-foundation-b-graph-query-contract-closure/inventory.md:5-16`. Several classifications at
`openspec/changes/post-foundation-b-graph-query-contract-closure/inventory.md:463-474` have since been refuted by the
implemented program:

- #822 says sixteen subjects remain split across two registration sites with no complete list at `inventory.md:463`.
  The merged tree now has one internal sixteen-operation conformance and registration catalog at
  `processor/graph-query/query.go:45-66`.
- #421, #422, and #571 retain aggregate-client descriptions at `inventory.md:464,468,474`. The approved F1 task truth
  records the complete client-cohort deletion at
  `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:143-172`, and the retained structural guard
  rejects every former client symbol at `graph/query/slice_f1_surface_contract_test.go:14-31`.
- #785 says research consumers still decode divergent bare shapes at `inventory.md:469`. The current boundaries call
  `graph.UnwrapQueryResponse` at `processor/research-graph-classify/adapters.go:146`,
  `processor/research-graph-execute/adapters.go:342`, and `pkg/fusion/fusionnats/client.go:594`.
- #819/#823 say normal global-search results omit Strategy and the agentic wrapper performs count-only projection at
  `inventory.md:470`. Current GraphRAG success paths populate `Strategy: "graphrag"`, including
  `processor/graph-query/graphrag.go:900-935`; the framework wrapper cohort is absent and protected by
  `processor/agentic-tools/executors/slice_f2_contract_test.go:24-65`.

Those capture-time rows must not be rewritten: changing them would falsify their baseline provenance. G.5 final
evidence must explicitly disposition them as superseded by the merged implementation.

The active design is a mutable current layer and its migration-notice draft remains incomplete. At
`openspec/changes/post-foundation-b-graph-query-contract-closure/design.md:648-659`, it omits both:

- the GraphQL `similaritySearch` to exact `semanticSearch` spelling break; and
- the explicit component-author action to declare the versioned `graph.query/v1` interface and named outputs.

The second action exists in the adopter table immediately above at active `design.md:646` but is absent from the
quoted migration notice. The final G.3 notice and G.5 sweep must carry both breaks.

The preservation rule is therefore layered:

- hash-pinned or baseline-identified capture-time inventories and reviewed designs retain their original text;
- new final evidence records which capture-time claims were superseded and by what current `file:line` evidence; and
- mutable active layers—current delta specs, tasks, approval/status text, migration guidance, and the archive result—
  are corrected before closeout.

### G.6-G.7 are final merged-tree gates, not documentation assumptions

- G.6 requires lint, full race tests, integration tests, schema generation/no drift, contract tests, strict OpenSpec,
  and relevant statistical, semantic, agentic, and research E2E tiers with active monitoring:
  `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:234-235`.
- G.7 requires final independent review, merged-tree negative searches and gates, conservative task truth, and
  archive: `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:236-237`.
- Neither task can be inferred green from earlier slice evidence. They apply to the final corrected merged tree.

## Adopter seam outcomes

The specific adopter is an external developer using SemStreams without reading internal implementation files.

| Adopter surface | Required action | If they do nothing | Primary discovery |
|---|---|---|---|
| GraphQL `capabilities` or `similaritySearch` caller | Remove the phantom field or use the surviving exact GraphQL spelling. | GraphQL validation fails; no alias is provided. | Schema introspection and GraphQL error, then migration notice. |
| GraphQL `localSearch` caller | Treat classified `index_not_ready` as retryable eventual availability. | The caller receives the typed transient result rather than a transport no-responder. | GraphQL error extensions and migration notice. |
| Go importer of `graph/query.Client` | Move to an admitted GraphQL operation or named operation-specific adapter. | Compilation fails at the deleted symbol; no shim exists. | Compiler first, then migration notice. |
| Go importer of `graph.QueryResponse.RequestID` | Remove field selection or keyed-literal use; query success contains `Data` and `Timestamp`. | Compilation fails; there is no compatibility field. | Compiler first, then query-success spec and migration notice. |
| Go importer of the deleted agentic wrapper symbols | Remove the executor, option, constructor, or querier use. | Compilation fails because the complete framework-owned surface is deleted. | Compiler first, then migration notice. |
| Config author retaining deleted wrapper `SkipBuiltins` keys | Remove the keys. | Existing closed-set boot validation fails visibly. | Boot error first, then migration notice. |
| Component author consuming graph query | Declare the versioned `graph.query/v1` provider/consumer outputs required by the port contract. | Missing or stale declarations fail Registry validation. | Registry validation and generated schema, then migration notice. |

The accepted roadmap records the underlying break/discovery rows at
`docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:638-656`. The discovery order is therefore
compiler, then boot/Registry validation, then schema introspection or classified query error, then the consolidated
migration notice. Downstream compilation, configuration migration, and product E2E remain downstream-owned; this
program performs no downstream audit or implementation.

## Inventory conclusion

At the merged baseline, G.1 conflicts with shipped NAME behavior; G.2 still depends on archive-time current-Purpose
publication; G.3 and G.4 artifacts are absent; G.5 has stale capture-time claims to disposition and mutable migration
guidance to correct; and no final merged-tree G.6 or G.7 evidence exists. This inventory records those facts and
grants no authority to correct, sequence, merge, archive, or begin unrelated work.
