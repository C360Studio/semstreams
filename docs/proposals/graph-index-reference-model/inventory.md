# Graph-index reconciliation proof inventory

base: 316170b1cf26e19a329626857819d582bcf30721

Issue #1292; draft PR #1393; branch `codex/gh1292-graph-index-properties`.
Read-only inventory by `semstreams-architect`, materialized by the coordinating session.
No target state or implementation recommendation is recorded here.

## Scope and record

Issue #1292 requests an independent model over create/update/replacement, deletion, admitted replay,
failure/repair and fresh-owner hydration. No runtime, API, storage-owner, configuration or broker-contract
change is authorized. Abrupt process/broker recovery is outside the proof's claim.

- `docs/contributing/06-openspec-change-discipline.md:10` — `- Use a GitHub issue for sequencing, investigation, proof-only work, release`
- `docs/contributing/09-property-testing.md:66` — `[#1292](https://github.com/C360Studio/semstreams/issues/1292) owns the planned graph-index reference model. These are`

The issue/PR owns acceptance and progress. This inventory introduces no OpenSpec behavioral delta.

## Claimed gap

Existing deterministic evidence includes replacement, deletion, stale work, repair and fresh ownership:

- `processor/graph-index/owner_reconcile_test.go:24` — `func TestProcessEntityUpdate_ReplacesPublicIndexResults(t *testing.T) {`
- `processor/graph-index/owner_reconcile_test.go:112` — `func TestDeleteFromIndexes_RetractsOwnedRowsWithoutDeletingLiveSourceAssertions(t *testing.T) {`
- `processor/graph-index/ordered_dispatch_test.go:133` — `func TestAuthoritativeReconcilePreventsLateOlderWatcherClobber(t *testing.T) {`
- `processor/graph-index/ordered_dispatch_test.go:173` — `func TestAuthoritativeReconcileReplacesOutgoingWithExplicitEmptyArray(t *testing.T) {`
- `processor/graph-index/ordered_dispatch_test.go:208` — `func TestRepairRefetchesAtOrderedExecution(t *testing.T) {`
- `processor/graph-index/replacement_reconcile_integration_test.go:29` — `func TestIntegration_ReplacementWatcherWatermarkPublicParityAndRestart(t *testing.T) {`
- `processor/graph-index/replacement_reconcile_integration_test.go:140` — `func TestIntegration_ReplacementPartialFailureWithholdsUntilOrderedRepair(t *testing.T) {`

The scoped model-gap search found no generated graph-index property/model declaration. Reconciliation and its
deterministic examples already exist; the claim concerns generated-history evidence.

## Spellings of the fact

### Authority and owner projections

Canonical ENTITY_STATES supplies authority. PREDICATE and NAME belong to an entity, INCOMING to a source
assertion, and OUTGOING to its entity key.

- `processor/graph-index/component.go:1339` — `entry, err := c.entityStatesBucket.Get(ctx, entityID)`
- `processor/graph-index/owner_reconcile.go:18` — `// reconcileOwnedRows replaces one entity owner's complete membership set. The`
- `processor/graph-index/owner_reconcile.go:47` — `existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)`
- `processor/graph-index/owner_reconcile.go:76` — `delErr := bucket.Delete(ctx, key)`
- `processor/graph-index/owner_reconcile.go:95` — `_, putErr := bucket.Put(ctx, key, desired[key])`
- `processor/graph-index/owner_reconcile.go:151` — `incomingIndexSourceFilter(sourceID), desired, false)`
- `processor/graph-index/component.go:1407` — `func (c *Component) buildEntityIndexPlan(state graph.EntityState, entityID string) (entityIndexPlan, error) {`
- `processor/graph-index/component.go:1491` — `func (c *Component) applyEntityIndexPlan(ctx context.Context, plan entityIndexPlan) error {`
- `processor/graph-index/component.go:1502` — `// ALIAS_INDEX has no owner-complete axis and is intentionally outside`
- `processor/graph-index/component.go:1503` — `// replacement reconciliation. Skipping an unsafe candidate does not retract a`

ALIAS is outside replacement reconciliation. Current specs retire CONTEXT_INDEX and PREDICATE_CATALOG;
historical ADR owner matrices do not expand this proof to those retired buckets.

### Replacement and deletion

- `openspec/specs/graph-index/spec.md:88` — `- **Authoritative OUTGOING replacement.** Every successful reconciliation of a present authoritative`
- `openspec/specs/graph-index/spec.md:90` — `relationship array, including an explicit empty array when it has no relationships. Only`
- `openspec/specs/graph-index/spec.md:91` — `authoritative `ENTITY_STATES` absence MUST delete the owner key. Explicit empty values are bounded`
- `openspec/specs/graph-index/spec.md:285` — `- **THEN** the source-owned INCOMING assertion remains available to retention/query policy`
- `openspec/specs/graph-index/spec.md:292` — `- **THEN** every row owned by that source is retracted through the selected bounded source-owned mechanism`
- `openspec/specs/graph-index/spec.md:293` — `- **AND** unrelated source assertions to the same targets remain`
- `processor/graph-index/component.go:2068` — `incomingFilter := incomingIndexSourceFilter(entityID)`
- `processor/graph-index/component.go:2069` — `predicateFilter := predicateIndexEntityFilter(entityID)`
- `processor/graph-index/component.go:2070` — `nameFilter := nameIndexEntityFilter(entityID)`

Deleting a target differs from deleting a live source assertion. A present empty entity and an absent entity
also have different OUTGOING storage obligations.

### Ordering and repair

- `openspec/specs/graph-index/spec.md:110` — `SAME hash-keyed FIFO dispatch per entity, with concurrency permitted across entities. Every work`
- `openspec/specs/graph-index/spec.md:111` — `item MUST reconcile authoritative `ENTITY_STATES` when it executes, so stale queued work cannot`
- `processor/graph-index/component.go:1136` — `// processEntityWork is the only mutation entry point used by the watcher,`
- `processor/graph-index/component.go:1144` — `completionAllowed := c.reconcileEntity(ctx, work.entityID)`
- `processor/graph-index/component.go:1306` — `func (c *Component) repairFailedEntities(ctx context.Context) {`
- `processor/graph-index/component.go:1318` — `if err := c.submitEntityWork(ctx, entityIndexWork{`
- `processor/graph-index/component.go:1368` — `// processEntityUpdateResult applies one captured watcher snapshot. Ordered`
- `processor/graph-index/component.go:1369` — `// production work completes its watermark in processEntityWork; the wrapper above`

References to reconcileEntity identify processEntityWork as its production caller. References to processEntityWork
identify the dispatcher callback and synchronous fallback. The captured-snapshot processing wrapper alone cannot
establish execution-time authority refetch.

### Revisions, coalescing and bootstrap

- `openspec/specs/graph-index/spec.md:115` — `- **Exact watermark completion.** Coalescing MUST retain the greatest delivered revision for each`
- `openspec/specs/graph-index/spec.md:116` — `pending entity and MUST complete the watermark for the exact revision represented by a detached`
- `processor/graph-index/revision_coalescer.go:42` — `if revision > c.pending[key] {`
- `processor/graph-index/revision_coalescer.go:86` — `batch = append(batch, coalescedEntity{entityID: key, revision: revision})`
- `processor/graph-index/component.go:1031` — `c.watermark.Observe(entry.Revision(), entry.Key(), entry.Created())`
- `processor/graph-index/component.go:1146` — `c.watermark.Complete(work.entityID, work.completionRevision)`
- `pkg/revlag/watermark.go:104` — `// Complete drains every in-flight revision for key with revision <= rev — the single`
- `pkg/revlag/watermark.go:109` — `// (never global-<=-rev) so one key's completion cannot drop a different key's pending`
- `processor/graph-index/component.go:1017` — `c.bootstrapTarget.Store(c.watermark.Observed())`
- `processor/graph-index/component.go:1020` — `c.initialEnumerationComplete.Store(true)`
- `processor/graph-index/watermark.go:148` — `return c.initialEnumerationComplete.Load() && indexed >= c.bootstrapTarget.Load()`

The component supplies the detached work revision; the watermark drains that key's pending revisions through it.
ADR-066 assumes ascending broker delivery and sparse latest-per-key replay under history 1. Stale queued work is
admitted separately; arbitrary descending Observe histories are not authorized by that premise.
Enumeration completion and reconciliation completion are distinct.

### Failure and readiness

- `openspec/specs/graph-index/spec.md:84` — `required delete that ultimately fails after bounded retry MUST return failure to the entity-work`
- `openspec/specs/graph-index/spec.md:95` — `(`GRAPH_STATUS` KV, ADR-083) MUST report not-ready, and the `INCOMING`, `OUTGOING`, `byName`,`
- `openspec/specs/graph-index/spec.md:107` — `- **Durable recovery.** A failed entity MUST be retried by a background repair loop (not only on the`
- `processor/graph-index/component.go:1178` — `if _, loaded := c.failedEntities.LoadOrStore(entityID, struct{}{}); !loaded {`
- `processor/graph-index/query.go:213` — `if c.failedCount.Load() > 0 {`
- `processor/graph-index/query.go:217` — `if c.indexBootstrapped.Load() {`
- `openspec/specs/graph-index-readiness/spec.md:40` — `answers no question the health gate asks. No read path SHALL defer on`
- `openspec/specs/graph-index-readiness/spec.md:41` — ``!Ready` alone — with one carve-out:`
- `openspec/specs/graph-index-readiness/spec.md:47` — ``IndexedRevision >= myRev` remains the caller-supplied read-your-writes`
- `openspec/specs/graph-index-readiness/spec.md:174` — `- **THEN** it is served (no transient), with staleness observable on the`

Failed work can complete revision accounting while failed-entity state continues to refuse queries. The existing
failure integration example checks both a failure and advanced watermark. Later readiness contracts/ADR-084/085
distinguish producer health from coverage. Exact results after a declared catch-up boundary do not imply ordinary
healthy post-bootstrap queries wait for live-head equality.

## Consumers and existing test seams

No new exported symbols, ports, subjects, buckets or configuration fields are proposed. Consumer-at-birth additions
are empty by scope. Existing production seams and fixture observations include:

- `processor/graph-index/component_test.go:1183` — `func createTestComponentWithMockKV(t *testing.T) *Component {`
- `processor/graph-index/component_test.go:1186` — `// Create unconnected NATS client`
- `processor/graph-index/component_test.go:1201` — `// Create mock buckets and wrap them in KVStore so the component field types match.`
- `processor/graph-index/component_test.go:27` — `putFunc          func(ctx context.Context, key string, value []byte) (uint64, error)`
- `processor/graph-index/component_test.go:28` — `getFunc          func(ctx context.Context, key string) (jetstream.KeyValueEntry, error)`
- `processor/graph-index/component_test.go:29` — `deleteFunc       func(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error`
- `processor/graph-index/component_test.go:30` — `listFilteredFunc func(ctx context.Context, filters ...string) (jetstream.KeyLister, error)`
- `processor/graph-index/component_test.go:46` — `return 1, nil`
- `processor/graph-index/ordered_dispatch_test.go:46` — `func startOrderedTestPool(t *testing.T, comp *Component, workers int) (context.Context, context.CancelFunc) {`
- `processor/graph-index/ordered_dispatch_test.go:50` — `require.NoError(t, comp.startIndexPool(ctx))`
- `processor/graph-index/ordered_dispatch_test.go:54` — `case <-comp.indexPool.done:`

Mock hooks admit failure injection but default writes lack real revision progression. Existing result helpers cover
names, predicates, incoming edges and predicate lists. Existing NATS examples cover watcher dispatch, watermark
publication, refusal, ordered repair and fresh component replay; none was executed during inventory.

## Adjacent claims

- #1393 — this claim, #1292 graph-index reconciliation proof.
- #1395 — #1394 smoke-test cleanup correction and lifecycle audit; the smoke test is excluded here.
- #1392 — prospective PBT guidance, without new obligations on existing claims.
- #1390 — service composition.
- #1387/#1388 — agentic accepted input and terminal recovery.
- #1219 — shutdown model's missing failure branch.
- #1287 — performance calibration.
- #759/#1146/#1155 — settlement/process replacement evidence.

Snapshot: September 26 GitHub issue/claim reads. The non-archive tracked OpenSpec search found no active graph-index
change at this baseline; it does not describe all remote branch content.

## Problem shape

Stateful history conformance with fault injection and retry has existing test precedents:

- `service/service_manager_prop_test.go:35` — `failNext    error // armed genuine failure, consumed by the next visit`
- `processor/agentic-tools/executors/graph_query_prop_test.go:20` — `// Both generators are written from the stated grammar and the stated`
- `processor/agentic-tools/executors/graph_query_prop_test.go:21` — `// contract — never from the executor's code — and each hugs the boundary the`
- `docs/contributing/09-property-testing.md:60` — `obligations. Sequential model testing does not establish concurrent interleavings, abrupt process replacement, or`

These precedents do not establish graph-index proof. No new runtime coordination/durable primitive or establishing
pattern is proposed. Adoption choice is deferred until independent inventory review.

## Adopter seam inventory

No outward contract changes. Existing observations under assessment:

| Seam question | Existing contract |
| --- | --- |
| What must an adopter know? | Healthy built indexes may lag; revision comparison provides read-your-writes. Empty results do not establish authoritative absence. |
| What happens without special handling? | Healthy post-bootstrap reads can return while lagging; incomplete bootstrap and failed operations refuse; reset-required is fatal. |
| Where is it observed? | Classified query errors and readiness fields. Correct interpretation of empty results remains a semantic obligation. |
| What new knowledge does this issue demand? | None: no deployment knob, wire, ordering promise or consumer procedure changes. |

Pins above identify the query gate and readiness contract. Sister repositories were not searched: no outward change
is proposed and no claim about their adoption correctness is made.

## Limits


No tests, Docker, integration, E2E, CI, runner or shared NATS helper operation was run by the architect.

Protected smoke cleanup and sister-owned runtime/CI claims are excluded.

No broker behavior, performance bound, filter maximum or activation gate was re-proven.

Fresh component hydration does not establish abrupt process or broker recovery.

Descending broker delivery is outside the stated premise; stale queued work is a distinct allowed case.

Readiness assertions must identify the producer/seam: graph-index refuses failed derived state even though generic
  readiness can distinguish complete coverage from health.

No implementation option or property budget is selected. Independent INVENTORY PASS is outstanding.

## Contract inconsistency recorded for review

The current spec retains a stale legacy deletion paragraph that conflicts with its later accepted source-ownership
and replacement requirements. Independent inventory review traced the settled authority to accepted ADR-077 section 5;
this is documentation inconsistency, not an unresolved choice of new behavior. The proof must cite the current
source-owned requirement. This inventory does not amend the spec or introduce a new guarantee.

History checked by the independent reviewer: legacy paragraph from `6b51d21a` (July 14); accepted ADR-077 dated
July 17; source-owned current requirement promoted by `6d02cdab` (#831, August 1); production source-filter deletion
introduced by `5113036a`. Existing source-owned requirements and production behavior agree.

- `openspec/specs/graph-index/spec.md:519` — `target). Source-owned retraction is deferred to the retention increment (gh#527).`
- `openspec/specs/graph-index/spec.md:292` — `- **THEN** every row owned by that source is retracted through the selected bounded source-owned mechanism`
- `openspec/specs/graph-index/spec.md:543` — `delete stale rows, and put missing rows.`
- `processor/graph-index/owner_reconcile_test.go:134` — `assert.Empty(t, queryIncomingEntries(t, comp, target), "retired source assertions must be retracted")`

- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:5` — `**Accepted (2026-07-17).**`
- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:86` — `A source fact replacement retracts the former INCOMING row. Source removal retracts all rows discovered on the`
- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:87` — `source axis across every target. Target retirement, removal, or tombstoning does not delete assertions still owned`
- `docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:88` — `by live sources, so target-prefix hard-delete is retired.`

## Searches

Commands ran at the declared baseline. Counts are stdout lines (grep matches, file paths or gopls declaration/member
lines). Broad search counts were remeasured with captured output; this does not imply every match was inspected.
Table escapes around alternation are Markdown syntax: executed regular expressions used ordinary vertical bars.

| Exact command | Result |
| --- | --- |
| `rg --files processor/graph-index openspec/specs openspec/changes` | 793 paths; initial preview `head -160` |
| `git grep -n -E 'rapid\|propert\|reconcil\|revision\|hydrat' -- processor/graph-index ':!processor/graph-index/predicate_layout_smoke_integration_test.go'` | 194 matches; initial preview `head -110` |
| `git grep -n -E 'graph-index\|index.*reconcil' -- docs/adr` | 107 matches; initial preview `head -45` |
| `git grep -n -E 'rapid.Check\|MakeCheck\|StateMachine' -- '*prop*test.go'` | 15 matches; all returned |
| `git grep -l -E 'owner.*reconcil\|replacement.*reconcil' -- docs/adr` | 2 files: ADR-056 and ADR-057; narrow spelling did not locate ADR-077 |
| `git grep -n -E 'graph-index\|1292' -- openspec/changes ':!openspec/changes/archive/**'` | 0, exit 1 without stderr |
| `git grep -n -E '1292\|graph-index reconciliation\|replacement reconciliation' -- docs/contributing docs/adr openspec/changes ':!openspec/changes/archive/**'` | 1, guide reference |
| `git grep -n -E 'rapid\|property\|model' -- docs/contributing/01-testing.md docs/contributing/09-property-testing.md` | 36 matches |
| `git grep -n -E 'rapid\|Fuzz\|TestProp\|reference.model' -- processor/graph-index ':!processor/graph-index/predicate_layout_smoke_integration_test.go'` | 1 unrelated comment; no property/model declaration |
| `git grep -n -E '^func createTestComponentWithMockKV\|^func Test.*(Repair\|Coalesc\|Bootstrap\|Enumeration\|Watermark)' -- processor/graph-index ':!processor/graph-index/predicate_layout_smoke_integration_test.go'` | 13 matches |
| `gopls workspace_symbol -matcher=fuzzy reconcile` | 0, exit 0; not absence evidence |
| `gopls workspace_symbol -matcher=fuzzy graphindex.reconcileEntity` | 0, exit 0; not absence evidence |
| `gopls references processor/graph-index/component.go:1328:21` | 1, component.go:1144 |
| `gopls references processor/graph-index/component.go:1138:21` | 2, component.go:1122 and :1130 |
| `gopls symbols processor/graph-index/component.go` | 133 |
| `gopls symbols processor/graph-index/owner_reconcile_test.go` | 23 |
| `gopls symbols processor/graph-index/replacement_reconcile_integration_test.go` | 26 |
| `gopls symbols processor/graph-index/ordered_dispatch_test.go` | 30 |
| `gopls symbols processor/graph-index/failure_honesty_test.go` | 5 |
| `gopls symbols processor/graph-index/mock_helpers_test.go` | 14 |
| `gopls symbols pkg/revlag/watermark.go` | 18 |
| `rg --files /tmp/semstreams-pbt-audit \| head -40` | 34 cached artifacts |
| `rg --files /tmp/semstreams-pbt-audit \| rg '1292\|issues\|claims'` | 0 |
| `command -v gopls` | `/Users/coby/go/bin/gopls` |
| `rg --files docs/adr \| rg '066\|078\|079\|083\|084\|085'` | 6 paths |
| `rg --files docs/adr \| rg '077'` | 1 path |
| `gh issue view 1292 --json title,body,comments` | network failure; no issue evidence |

Parent supplied current issue and claim JSON snapshots, subsequently read by the architect.
All 628 graph-index spec lines and 667 graph-index-readiness spec lines were read in bounded ranges. Potentially
truncated reads were closed with `sed -n '1,79p'`, `sed -n '130,284p'`, and `sed -n '471,628p'` on graph-index/spec.md,
and `sed -n '231,450p'` on graph-index-readiness/spec.md. These returned 79, 155, 158 and 220 lines without truncation.
ADR-066 was read through ranges 1-235 and 236-479. ADR-077, ADR-078, ADR-084 and ADR-085 were read in full.
`cat docs/adr/083-readiness-as-distributed-state.md` returned all 179 lines to close a combined-output gap.
`git rev-parse HEAD` and `git status --short` established the baseline and initially clean worktree.

- NOT READ: protected predicate_layout_smoke_integration_test.go, owned by #1395.
- NOT RUN: sister-repository searches; no outward contract change is proposed.
- NOT RUN: repository-wide implementer/caller sweep for every exported graph-index helper; structure is bounded above.
- NOT READ: every broad-search match; the 194-line implementation and 107-line ADR previews were followed by targeted reads.
- NOT READ: graph-query, graph-ingest, graph-state-contract and keyed-dispatch specs in full; no full conformance claim.
- NOT RUN: git log -S; no deletion/dead-code premise is asserted.
- NOT RUN: tests, mutations, Docker/integration/E2E, CI gates, runner inspection, or shared NATS helper operations.
- NOT RUN: current broker/filter/performance/activation experiments.
- NOT RUN by the architect: inventory verification/hashing; the coordinator owns artifact materialization.

Independent reviewer authority-history supplement: `git log -S` traced the quoted legacy and source-owned paragraphs;
the reviewer inspected accepted ADR-077 section 5. Coordinator confirmation:
`git show -s --format='%h %ad %s' --date=short 6b51d21a 6d02cdab 5113036a` returned the three commits and dates above;
`nl -ba docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md` ranges 1-22 and 79-101 checked the four ADR pins.
The earlier NOT RUN for git log -S describes the architect's inventory pass, not this independent review supplement.
