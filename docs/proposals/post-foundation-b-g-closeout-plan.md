# Post-Foundation-B G closeout plan

**Status:** Owner approved Option 2 and authorized Checkpoint 1 on 2026-08-11 after design review. Checkpoint 2 remains
gated on the merged Checkpoint 1 proof tree and separate owner direction; this plan grants no runtime, merge, archive,
or issue-execution authority.

**Merged baseline:** `08c03b4d48414b2daba1ca443c29202c0152e4f6`

**Accepted inventory:** `docs/proposals/post-foundation-b-g-closeout-inventory.md`, SHA-256
`ce7b27274f75b91be72c1c24c9b8780226094d021d37b0f432142fd001e670ae`

**Inventory review:** `INVENTORY PASS`

**Owner-accepted inventory after status promotion:** SHA-256
`bde4c51db3044e575be04b384d4e6941f829331b598a59058072959b69fe5645`

**Owner-approved plan checkpoint:** SHA-256
`4220173426389576c37ad4656133cc2ceaa03deb85fe46d15dd5be806423f797`

## Options considered

| Option | Coordination cost | Benefit | Residual risk |
|---|---:|---|---|
| 0. Do nothing | None now | Avoids immediate closeout work. | Leaves G.1 false, G.2 unpublished, adopter breaks undocumented, stale active layers unresolved, and the program unarchived. Future work inherits ambiguous authority. |
| 1. One combined correction/proof/archive checkpoint | One pull request, but a long-running review and gate window | Fewer merge boundaries. | The tree under review changes while gates, archive materialization, Purpose publication, and task truth are still being assembled. G.2 can be claimed before current Purpose exists, and final review can miss the exact archive diff. |
| 2. Two bounded checkpoints | Two pull requests and one explicit handoff | Separates truth/evidence correction from merged-tree gates and archive mechanics; every proof runs against a stable prerequisite tree. | Moderate coordination overhead and a deliberate stop between checkpoints; gate failures still require separately attributed bounded fixes. |
| 3. Broaden closeout into runtime or queued-issue work | Unbounded design, implementation, and downstream coordination | Could address adjacent known defects in one campaign. | Reopens foundation assumptions, mixes unrelated owners and tests, defeats archive attribution, and recreates the complexity ratchet this program is closing. |

**Recommendation:** Option 2. Its extra merge boundary buys a stable proof tree and an independently reviewable final
archive diff. Options 0, 1, and 3 leave materially higher truth, attribution, or scope risk.

## Recommended G.1-G.7 dispositions

| Task | Recommended disposition | Bounded treatment |
|---|---|---|
| G.1 | **BLOCKED pending owner wording correction** | Replace the false “INCOMING-only” claim with: NAME hashes the name axis; NAME and INCOMING use reversible predicate hex; PREDICATE is raw; catalog absent; no runtime migration. The conflict is at active `specs/graph-index/spec.md:19-24` and `tasks.md:222-223`. |
| G.2 | **ARCHIVE-TIME ONLY** | The active change contains no Purpose delta, so archive cannot publish G.2 implicitly. Checkpoint 1 freezes the exact replacement text but does not edit current Purpose or mark G.2 complete. Checkpoint 2 directly replaces `openspec/specs/graph-query/spec.md:3-7` in the archive transaction, verifies it on the materialized archive tree, and only then records G.2 complete. |
| G.3 | **READY, artifact missing** | Publish one downstream migration notice with the task's exact breaks and communicate-only boundary: active `tasks.md:227-229`. |
| G.4 | **READY, artifact missing** | Produce fourteen primary ruling-to-final-`file:line` rows, plus affected-row addenda mapping every later binding Slice D, E, F1, and F2 clarification or approval condition to final evidence or an owner-approved deviation. The primary-row requirement is active `tasks.md:230-231`; addenda prevent a later ruling from disappearing behind the original row. |
| G.5 | **REQUIRED** | Disposition stale capture-time claims in new final evidence and sweep every current change artifact and cited mechanism under active `tasks.md:232-233`. Preserve only hash-pinned or baseline-identified capture artifacts verbatim. Correct mutable active `design.md` status, `approval.md` E-pending text, specs, tasks, and migration guidance before archive. |
| G.6 | **NOT DONE** | Run every final corrected merged-tree gate with active monitoring; no earlier slice run substitutes for active `tasks.md:234-235`. |
| G.7 | **NOT READY** | Begins only after Checkpoint 1 merges and G.6 is green. Archive verification, final review, conservative task truth, and completion remain under active `tasks.md:236-237`. |

Paths shortened to `specs/...`, `tasks.md`, `design.md`, and `approval.md` in the table are relative to
`openspec/changes/post-foundation-b-graph-query-contract-closure/`.

## Adopter seam

The specific adopter is an external developer using SemStreams without opening internal implementation files.

| Adopter surface | Concrete migration | If they do nothing | Primary discovery |
|---|---|---|---|
| GraphQL `capabilities` or `similaritySearch` caller | Remove `capabilities`; replace `similaritySearch` with the surviving exact `semanticSearch` spelling. | GraphQL validation fails; no alias is provided. | Schema introspection and GraphQL error, then migration notice. |
| GraphQL `localSearch` caller | Treat classified `index_not_ready` as retryable eventual availability. | The caller receives the typed transient result rather than a transport no-responder. | GraphQL error extensions and migration notice. |
| Go importer of `graph/query.Client` | Move to an admitted GraphQL operation or named operation-specific adapter. | Compilation fails at the deleted symbol; no shim exists. | Compiler first, then migration notice. |
| Go importer of `graph.QueryResponse.RequestID` | Remove field selection or keyed-literal use; query success contains `Data` and `Timestamp`. | Compilation fails; there is no compatibility field. | Compiler first, then query-success spec and migration notice. |
| Go importer of deleted agentic wrapper symbols | Remove the executor, option, constructor, or querier use. | Compilation fails because the framework-owned surface is deleted. | Compiler first, then migration notice. |
| Config author retaining deleted wrapper `SkipBuiltins` keys | Remove the keys. | Existing closed-set boot validation fails visibly. | Boot error first, then migration notice. |
| Config author retaining former shared-wrapper names in `allowed_tools`, `default_tools`, `approval_required`, or `tool_retries` | Remove stale framework references unless an application-local executor intentionally owns the same name. The fields remain open vocabulary. | `default_tools` warns and drops an unresolved name; allow/retry policy creates no executor; `approval_required` can pause an otherwise admitted call before registry miss. | Config review, discovery warning, approval pause, or typed not-found, then migration notice. |
| Application intentionally reusing a former name for a local executor | Keep only the local tool and its matching allow/default/approval/retry policy; no shared compatibility executor participates. | Existing local discovery, admission, approval, retry, and dispatch behavior applies. | Local registration and ordinary tool discovery. |
| Exported category-API consumer querying `graph_search` or `graph_summary` | Stop treating the deleted alternate aliases as framework `CategoryKnowledge`; accept the existing unknown-name `CategoryCore` result or explicitly categorize an application-local tool through the existing API. | `GetToolCategory` now returns `CategoryCore`; exported category functions and `ReadOnlyCategories` otherwise remain. | Go tests/compiler-visible API behavior and migration notice. |
| Component author consuming graph query | Declare the versioned `graph.query/v1` interface and required named outputs. | Missing or stale declarations fail Registry validation. | Registry validation and generated schema, then migration notice. |

The adopter-seam questions resolve as follows:

- **What must they know?** Only which concrete surfaces they use and the corresponding migration above.
  They do not need internal bucket names, responder registration sites, cache mechanics, or archive mechanics.
- **What happens if they do nothing?** Outcomes are surface-specific: compilation failure, boot/Registry rejection,
  GraphQL validation failure, a typed transient query result, default-tool warning/drop, approval pause before registry
  miss, typed not-found, or the existing silent unknown-name `CategoryCore` fallback. The table above identifies the
  exact outcome for each migration.
- **Where do they find out?** Use each row's Primary discovery column; there is no truthful single global rank across
  compile-time failures, boot/Registry checks, GraphQL/transient errors, warnings, approval pauses, typed not-found,
  and silent category fallback. The consolidated migration notice is the human-readable map across them.
- **What should they have to know?** Only their concrete migration. They should not learn a new client, subject catalog,
  port family, status model, or compatibility mechanism.

The closeout adds no external surface. It documents already-landed breaks and the existing versioned port contract;
downstream implementation, audit, and product E2E remain downstream-owned.

## Two-checkpoint closeout sequence

### Checkpoint 1: truth, active-delta, and release-evidence correction

One bounded documentation/specification pull request should contain only:

1. the owner-approved G.1 wording correction;
2. corrected active graph-index delta and conservative G.1 task truth;
3. the exact archive-time graph-query Purpose draft below, frozen as evidence without editing current Purpose or
   marking G.2 complete;
4. the standalone G.3 downstream migration notice, including every adopter migration above;
5. the G.4 fourteen primary ruling-conformance rows plus affected-row addenda for every later binding Slice D, E, F1,
   and F2 clarification or approval condition;
6. correction of mutable active layers, including `design.md` status, `approval.md` stale E-pending text, active
   specs/tasks, and migration guidance;
7. the G.5 correction-propagation sweep and measured evidence; and
8. independent review of that exact documentation/specification checkpoint.

The exact Purpose replacement frozen by Checkpoint 1 is:

```markdown
## Purpose

The admitted graph query capability in `processor/graph-query`: the versioned
`graph.query/v1` operation family, stable responders, generation-safe
optional-view caches, shared success decoding, bounded research projection, and
truthful query outcomes. Remote applications use admitted GraphQL operations;
embedded framework consumers use named operation-specific adapters declared
through component ports. This capability exposes neither a public subject
catalog nor a general embedded client.
```

The active change contains no Purpose delta. Checkpoint 1 therefore prepares this exact replacement but does not
change `openspec/specs/graph-query/spec.md:3-7`; G.2 remains unchecked.

Checkpoint 1 must not:

- mark G.2, G.6, or G.7 complete;
- archive the active change;
- rewrite hash-pinned or baseline-identified capture-time inventory/design artifacts;
- preserve stale text merely because it is in a mutable active layer; or
- absorb a runtime fix or unrelated issue.

Hash-pinned or baseline-identified capture artifacts remain verbatim as provenance. New final evidence dispositions
their stale claims. Mutable active `design.md`, `approval.md`, delta specs, tasks, and migration guidance are corrected
before archive.

### Checkpoint 2: merged-tree proof and exact archive transaction

Only after Checkpoint 1 merges:

1. run the exact non-E2E G.6 gates on the merged Checkpoint 1 tree with active monitoring:
   - `task lint`;
   - `go test -race ./...`;
   - `task test:integration`;
   - `task schema:generate`, followed by a clean generated-schema/spec diff;
   - `go test ./test/contract/...`; and
   - strict `task openspec:validate`;
2. run the exact E2E gates with active monitoring:
   - `task e2e:statistical`;
   - `task e2e:semantic`;
   - `task e2e:agentic`;
   - `task e2e:research-graph`; and
   - `task e2e:deep-research`;
3. run final negative searches, checksum verification, and correction-propagation checks on that same merged tree;
4. stop and create a separately attributed bounded correction if any gate or negative search fails;
5. materialize the complete archive tree from the proven Checkpoint 1 baseline;
6. archive the active OpenSpec change and directly replace
   `openspec/specs/graph-query/spec.md:3-7` with the frozen Purpose in the same transaction;
7. write conservative archived task truth and final evidence on that materialized tree—G.2 may now be complete because
   current Purpose exists, and G.6 may be complete only from the measured gates;
8. rerun strict OpenSpec validation plus archive-tree structural, checksum, generated-drift, and diff checks;
9. create the exact final archive commit, record its commit SHA, and confirm the worktree is clean;
10. hand that exact commit SHA and final diff to an independent `semstreams-reviewer`;
11. after approval tied to that SHA, push the unchanged commit and require every GitHub CI check for that exact archive
    commit to be green; and
12. merge that exact reviewed, CI-green commit without any subsequent mutation.

G.7 includes the final independent review and merge, so it cannot be truthfully pre-checked in the diff that is itself
under review. The archived task record remains conservative; the final review plus unchanged merge commit is the
completion evidence for G.7. No file changes, checkbox edits, formatting changes, or generated drift may occur after
the final reviewer approves the exact diff.

Any local-gate, review, or exact-commit CI failure returns to the appropriate earlier step. A fix creates a new commit
SHA and requires fresh archive-tree checks, reviewer SHA handoff, approval, and exact-SHA CI. It does not expand either
closeout checkpoint into unrelated runtime or issue work.

## Hard post-archive stop

After the archive pull request and archive-tree verification complete, stop this program. Do not select or implement
another queued issue from the old roadmap. Begin a fresh inventory against the then-current merged tree and issue
queue before proposing the next foundation program. This prevents old assumptions, already-closed tasks, and
issue-queue drift from silently becoming new implementation authority.

## Explicitly out of scope

The following do not enter either G closeout checkpoint:

- runtime graph-index representation or layout migration;
- hierarchy, clustering, alias, or anomaly redesign;
- BM25 persistence or restart-order work;
- record chunking or payload-ceiling work;
- retention or ObjectStore/KV garbage collection;
- research orchestration or hierarchy inference changes;
- `GRAPH_STATUS`, generic readiness, or new status ownership;
- new clients, catalogs, subjects, ports, services, buckets, or streams;
- downstream implementation, compilation audit, configuration migration, or product E2E;
- the suspended `semantic-tier-split` change; and
- unrelated issue-queue work.

These exclusions follow the accepted foundation boundary at
`docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:509-531` and the stop gates against runtime
representation migration, new infrastructure, broader mechanisms, and downstream work at
`docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md:645-654`.

## Plan conclusion

The owner-approved safe closeout is two checkpoints: first correct and independently review truth/evidence without
claiming archive-time completion; then, only after the Checkpoint 1 proof tree and separate direction, prove the
corrected merged tree, archive, verify current Purpose and strict OpenSpec on the archive tree, and stop.
