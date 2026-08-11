# Post-Foundation-B graph query ruling conformance

**Status:** Checkpoint 2 archive candidate derived from verified corrected implementation commit `cbbc907e`. G.2 and
G.6 are complete on the materialized archive tree. G.7 remains pending exact-commit independent review, exact-SHA
GitHub CI, and unchanged merge; this artifact claims neither G.7 nor downstream completion.

## Fourteen primary rulings

| Ruling | Binding outcome | Final file:line evidence | Disposition |
|---|---|---|---|
| 1 | Bounded query closure, not broad graph convergence | Approved scope: `openspec/changes/archive/2026-08-11-post-foundation-b-graph-query-contract-closure/approval.md:26-32`; bounded slices and exclusions: `proposal.md:7-10,17-38` | Conforms. No authority, hierarchy, retention, or issue-queue redesign entered A-F2. |
| 2 | Generation supervisor replaces bucket-presence watching | Generation state/watch/publication: `processor/graph-query/community_cache.go:23-68,130-166`; measured task evidence: `tasks.md:55-80` | Conforms. |
| 3 | `localSearch` requires usable generation; lower tiers degrade instead of disappearing | Exact availability catalog: `processor/graph-query/query.go:61-65`; implementation/gate truth: `tasks.md:63-79` | Conforms. All responders remain installed; optional view loss is classified behavior. |
| 4 | One `graph.query/v1` port family with exact consumers | Interface and one operation catalog: `processor/graph-query/query.go:22-23,45-69`; gateway declared family: `gateway/graph-gateway/component.go:142-167`; task truth: `tasks.md:38-53` | Conforms. |
| 5 | Delete shared agentic graph wrappers; preserve unrelated local extension | Complete absence/local-behavior guard: `processor/agentic-tools/executors/slice_f2_contract_test.go:24-120`; measured truth: `tasks.md:174-218` | Conforms. No shared replacement or reserved former name. |
| 6 | One catalog-backed optional-summary view with statistical fallback | Typed view owner: `processor/graph-query/component.go:162-168`; lifecycle publication/clear: `processor/graph-query/summary_view.go:241-277`; measured truth: `tasks.md:83-112` | Conforms under the later reduced Slice D ruling; see addenda. |
| 7 | Preserve existing classified error authority in GraphQL extensions | Projection code: `gateway/graph-gateway/component.go:1320-1324`; coded/uncoded/plain/status tests: `gateway/graph-gateway/query_contract_closure_test.go:83-139` | Conforms. No new class/code authority. |
| 8 | Libraries receive no component ports; fusion gets no invented host/config/readiness | Slice E boundary truth: `tasks.md:131-132`; retained operation-specific constructor/client surface: `graph/query/slice_f1_surface_contract_test.go:53-73` | Conforms. |
| 9 | Delete only provisional aggregate `graph/query.Client` | Deleted-symbol guard: `graph/query/slice_f1_surface_contract_test.go:14-31`; exact deletion/gates: `tasks.md:143-172` | Conforms. No shim or replacement general client. |
| 10 | Preserve `fusionnats.Client`; converge only reply decoding/fixtures | Exact-entity validation/projection: `pkg/fusion/fusionnats/client.go:370-399`; single unwrap boundary: `pkg/fusion/fusionnats/client.go:584-595`; contract fixtures: `pkg/fusion/fusionnats/slice_e_contract_test.go:16-70` | Conforms. |
| 11 | Remove `capabilities`; retain fourteen graph-query-backed and nineteen total fields | Exact inventory/routes/count and absence test: `gateway/graph-gateway/query_contract_closure_test.go:20-67`; semantic spelling test: `query_contract_closure_test.go:69-80` | Conforms, including the later exact-field clarification. |
| 12 | Seed routing/error capabilities and publish complete graph-query Purpose | Routing/error implementation evidence: `gateway/graph-gateway/query_contract_closure_test.go:20-139`; archived capability deltas: `openspec/changes/archive/2026-08-11-post-foundation-b-graph-query-contract-closure/specs/gateway-query-routing/spec.md:1-30` and `specs/gateway-error-projection/spec.md:1-34`; current capability Purpose: `openspec/specs/graph-query/spec.md:3-11` | Conforms. The materialized current Purpose publishes the admitted operation family, versioned port contract, serving and decoding behavior, and explicit exclusions. |
| 13 | Correct predicate layout documentation without runtime migration | Corrected archived delta: `openspec/changes/archive/2026-08-11-post-foundation-b-graph-query-contract-closure/specs/graph-index/spec.md:3-25,27-69`; shipped code: `processor/graph-index/predicate_index.go:12-16`, `name_index.go:49-62,114-143`, `incoming_index.go:20-42,58-99` | Conforms after Checkpoint 1 correction: NAME hashes name; NAME+INCOMING hex predicates; PREDICATE raw; catalog absent. |
| 14 | Clean break; communicate without downstream audit/fixes | Structural deletion guards: `graph/query/slice_f1_surface_contract_test.go:14-31` and `processor/agentic-tools/executors/slice_f2_contract_test.go:24-65`; canonical notice: `docs/operations/migration-post-foundation-b-graph-query-contract-closure.md:1-45` | Conforms. Downstream implementation remains outside the program. |

G.4 requires a complete ruling-to-evidence mapping, not a claim that every mapped condition is complete. The fourteen
primary rows and binding addenda are complete; archive-time Purpose publication makes ruling 12 conforming.

## Later binding-condition addenda

These addenda prevent later owner clarifications from disappearing behind their original primary row.

| Binding condition | Affected primary ruling(s) | Final file:line evidence | Disposition |
|---|---|---|---|
| Slice D uses existing `pkg/graphview.View`; no summary generation/lease/final validation/readiness/degradation/config/infrastructure | 6 | Approval condition: `approval.md:59-64`; implementation truth: `tasks.md:83-97`; typed view: `processor/graph-query/component.go:162-168` | Conforms. |
| Slice D supervisor stops/clears failed view, reopens, publishes one pointer, and classifies decoder poison | 6 | Approval condition: `approval.md:66-71`; lifecycle code: `processor/graph-query/summary_view.go:241-277`; measured tests: `tasks.md:99-110` | Conforms. |
| Slice E remains bounded to measured decoder/result truth | 1, 8, 10, 12 | Approval E.1: `approval.md:73-79`; completed scope: `tasks.md:114-141` | Conforms. |
| Slice E unwraps exactly once at research-classify, research-execute, and fusion request boundaries; fusion Status remains KV | 10, 12 | `processor/research-graph-classify/adapters.go:142-150`; `processor/research-graph-execute/adapters.go:338-343`; `pkg/fusion/fusionnats/client.go:584-595` | Conforms. |
| Slice E validates `ExactEntity.KVRevision` but does not widen `fusion.Entity` | 10 | `pkg/fusion/fusionnats/client.go:382-399`; `pkg/fusion/fusionnats/slice_e_contract_test.go:16-48` | Conforms. |
| Slice E adds no fusion-host port/config/Registry/readiness owner | 8 | `tasks.md:131-132`; preserved library surfaces: `graph/query/slice_f1_surface_contract_test.go:53-73` | Conforms. |
| Slice E does not widen receiver-less CandidateSet/Evidence; full entities project through existing candidates | 1, 12 | `processor/research-graph-classify/adapters.go:153-188`; task boundary: `tasks.md:122-126` | Conforms. |
| Slice E deletes private `similaritySearch` wrapper without compatibility | 11, 14 | Gateway negative: `gateway/graph-gateway/query_contract_closure_test.go:69-80`; task truth: `tasks.md:129-130` | Conforms. |
| Slice E deletes query-success `RequestID` and discriminator key | 12, 14 | Closed two-field type/key set: `graph/query_contracts.go:17-37`; task truth: `tasks.md:129-130` | Conforms. |
| Slice E proves nonzero research candidates and exact live `graphrag` strategy without a new tier | 3, 12 | E2E obligation/truth: `tasks.md:133-141`; terminal strategy implementation: `processor/graph-query/graphrag.go:900-935` | Conforms. |
| Exact-field clarification makes `semanticSearch` sole GraphQL spelling | 11, 14 | Approval clarification: `approval.md:98-102`; route/negative test: `gateway/graph-gateway/query_contract_closure_test.go:69-80` | Conforms. |
| F sequencing keeps aggregate-client deletion separate and reviewed before wrapper deletion | 5, 9, 14 | Approval sequencing: `approval.md:104-114`; separate F1/F2 task evidence: `tasks.md:143-218` | Conforms. |
| F1 uses production `pathSearch`, deletes exactly the client cohort, preserves named adapters, and adds no replacement | 4, 9, 10, 14 | Exact deletion/preservation/gates: `tasks.md:145-172`; guard: `graph/query/slice_f1_surface_contract_test.go:14-101` | Conforms. |
| F2 deletes shared wrappers/categories while preserving former-name local reuse and open-vocabulary policy | 5, 14 | Approval: `approval.md:116-129`; shared-deletion/local-behavior tests: `processor/agentic-tools/executors/slice_f2_contract_test.go:24-120`; task evidence: `tasks.md:174-218` | Conforms. |
| F2 preserves admission-before-approval and ApprovalFilter-before-registry behavior | 5, 14 | Approval condition: `approval.md:125-129`; real-NATS measured evidence: `tasks.md:205-218` | Conforms. |

Paths shortened to `approval.md`, `tasks.md`, `proposal.md`, and `specs/...` are relative to
`openspec/changes/archive/2026-08-11-post-foundation-b-graph-query-contract-closure/`.
`query_contract_closure_test.go` without a prefix is relative to `gateway/graph-gateway/`.

## G.5 correction-propagation evidence

### Mutable layers corrected in Checkpoint 1 and retained in the archive

- Graph-index delta and G.1 task wording now match shipped NAME, INCOMING, PREDICATE, and catalog behavior:
  `specs/graph-index/spec.md:3-69` and `tasks.md:222-224`.
- Archived design, corrected while active, no longer says Slice E implementation is pending: `design.md:3-6`.
- Archived approval, corrected while active, no longer says E.1-E.8 are unchecked: `approval.md:95-97`.
- The archived adopter table and migration draft include semantic spelling, versioned port declarations, F2
  open-vocabulary policy/local reuse, and category fallback: `design.md:631-659`.
- The standalone migration notice covers every accepted adopter row:
  `docs/operations/migration-post-foundation-b-graph-query-contract-closure.md:1-45`.
- Archived G task wording distinguishes archive-time Purpose publication and final merged-tree proof:
  `tasks.md:220-240`.

### Capture-time evidence preserved, not rewritten

The accepted baseline inventory remains a historical checkpoint. Its stale #822, #421/#422/#571, #785, and #819/#823
rows at `inventory.md:463-474` are dispositioned by current catalog, deletion, unwrap, and strategy evidence in
`docs/proposals/post-foundation-b-g-closeout-inventory.md` rather than rewritten. Hash-pinned Slice D/E/F inventory and
design checkpoints likewise retain their capture-time text.

### Archive-time and final-gate boundaries preserved

- The exact owner-approved Purpose is now published at `openspec/specs/graph-query/spec.md:3-11`; G.2 is complete.
- Checkpoint 2 gates passed on corrected implementation commit `cbbc907e`: lint, full race tests, integration tests
  (`natsclient` 102.483s), schema generation with no drift, contract tests, and strict OpenSpec validation (39/39).
- Statistical (41/41), semantic (48/48, exact `graphrag`, 7/7 known answers), agentic, research-graph with a positive
  candidate count, and deep-research E2E tiers passed. G.6 is complete.
- The first semantic attempt exposed a test-only 10s caller versus 15s server synthesis timeout. Correction
  `cbbc907e` reused the existing 60s helper; independent review approved it, and semantic passed with a 43.6s gateway
  response.
- All nine named frozen-artifact checksum sidecars and focused F1, F2, gateway-query, and operation-inventory guards
  passed. Production-source negatives found no retired GraphQL alias/capabilities, F2 wrapper declarations,
  `QueryResponse.RequestID`, or graph-index `PREDICATE_CATALOG`.
- The materialized archive passes strict OpenSpec validation (40/40) and `git diff --check`; both moved Slice F2
  checksum sidecars still verify from the archive directory.
- G.7 remains unchecked until an independent reviewer approves the exact archive commit, every GitHub CI check for
  that exact SHA is green, and the commit merges unchanged. This candidate does not pre-claim those later facts.
