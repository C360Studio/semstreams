# Cleanup remediation plan

## Completion boundary

#1064 establishes the source-accounted census, reviewed exact baseline, regression guard, and package repair plan.
It does not claim existing debt is repaired or a finite supplied context makes synchronous Stop return.
Existing causal evidence takes precedence over population size when selecting the next repair.
Each package repair keeps its own review, deterministic failure proof, and rollback boundary.

## Evidence that sets priority

#1062 observed Rule readiness cleanup waiting after the test body finished: unbounded Stop reached
`awaitEntityBorrowSettlement` and parked sibling tests behind the package timeout. Its immediate correction is
already merged. Preserve that regression evidence; do not reopen or duplicate its four-site repair.

The shared suite's `TestErrorInjection` contains two distinct Background calls: the operation under assertion and
an explicit finalizer after the assertion (`component/lifecycle_test_suite.go`, currently lines 445 and 455).
The ordinary finalizer is remediation evidence even though the defer/Cleanup guard does not classify ordinary
syntax as terminal ownership by itself. Do not relabel both calls as deliberate contract tests.

The independently reviewed baseline contains 334 exact known-debt entries across 109 source files. These are
existing unbounded cleanup sites, not measured hangs. Eighty-six exact resolutions classify reviewed uncertainty;
641 ordinary-only unknown records remain visible and do not establish cleanup safety.

## Proposed package sequence

| Order | Boundary | Evidence priority |
|---|---|---|
| 1 | Shared test support | Distinguish contract operations from finalizers; check substrate order |
| 2 | `processor/graph-ingest` | Review each cleanup family and its test-owned substrate |
| 3 | `processor/rule` | Preserve #1062 proof; select remaining debt from the final census |
| 4a | `processor/graph-query` | Package-specific ordering and cancellation proof |
| 4b | `gateway/graph-gateway` | Package-specific ordering and cancellation proof |
| 4c | `processor/gated-dag` | Package-specific ordering and cancellation proof |
| 4d | `processor/graph-index` | Package-specific ordering and cancellation proof |
| 5 | WebSocket output; remaining packages | Prioritize measured failures, then review applicable patterns |

This ordering is a planning default, not measured runtime risk. Attach exact census identities and accepted counts
before claiming a package batch; a new causal failure may take priority. Do not combine these boundaries merely to
make the baseline smaller.

## Required evidence for each batch

- Identify the exact cleanup owner, caller and test-owned substrate. Components must finish cleanup before the
  NATS/TestClient owner tears its substrate down; registration order alone is not proof without the actual call path.
- Preserve deliberate API-contract operations. Give terminal cleanup fresh finite authority and assert Stop errors.
  Do not mechanically substitute one timeout everywhere or reuse an already-canceled operation context.
- Observe a representative bounded failure or omission at the production boundary. If Stop ignores cancellation,
  finite context supply alone does not satisfy hang containment or authorize production changes without separate proof.
- Reconcile removed debt entries in the same patch; new/stale entries must be rejected before the expensive gate.
- Run the narrowest causal checks during iteration, then the existing required preflight through the canonical runner.
  Preserve the host lock and other sessions' containers/processes.

## Adjacent test debt

#1293 owns measured common-gate parity and duplication, citation/fuzz execution, and coverage artifacts. Record the
new guard's cold/warm and composed-path cost here before handing any deduplication consideration to that issue.
No bypass flag or optimistic load result is justified by overhead. Its rc.1 placement is unchanged.

#1411 owns the broader shared lifecycle ownership pattern. #1412 owns the separately tracked nil-context behavior.
This plan introduces no new production lifecycle policy or follow-up issue merely to restate those boundaries.

## Independently reviewed known-debt population

The exact installed identities are in `test/testinfra/cleanup_baseline.json`; reconciliation and source review
are retained under this change's `review/`. This table summarizes that set and grants no package-wide exemption.

| Package | Reviewed entries |
|---|---:|
| `agentic/agentrun` | 4 |
| `examples/processors/document` | 1 |
| `examples/processors/iot_sensor` | 1 |
| `gateway/graph-gateway` | 32 |
| `gateway/lifecycle-gateway` | 1 |
| `input/udp` | 7 |
| `output/file` | 5 |
| `output/httppost` | 5 |
| `output/websocket` | 22 |
| `pkg/dispatch` | 4 |
| `pkg/lifecycle` | 1 |
| `processor/agentic-dispatch` | 2 |
| `processor/agentic-loop` | 18 |
| `processor/agentic-model` | 14 |
| `processor/agentic-tools` | 12 |
| `processor/agentic-tools/executors` | 1 |
| `processor/gated-dag` | 6 |
| `processor/graph-clustering` | 16 |
| `processor/graph-embedding` | 13 |
| `processor/graph-index` | 27 |
| `processor/graph-index-spatial` | 5 |
| `processor/graph-index-temporal` | 5 |
| `processor/graph-ingest` | 32 |
| `processor/graph-query` | 39 |
| `processor/json_filter` | 4 |
| `processor/json_map` | 4 |
| `processor/rule` | 29 |
| `service` | 20 |
| `storage/objectstore` | 4 |

Total: 334. The plan above sets the provisional order; counts alone do not establish runtime risk.

## Contextless cleanup observed during the audit

Source inspection also found contextless cancel-and-join operations behind test Cleanup registrations:
`fusionnats.Client.Close` joins its readiness watcher, and `Component.stopActivityView` joins activity work.
Their receive operations carry no supplied context budget. These are outside this guard's context-taking lifecycle
Stop contract; their exclusion does not establish bounded return or an observed hang. Keep them in the source review
packet for a later cleanup-ownership repair decision, without widening #1064 into a generic Close/join analyzer.

The existing common-gate duplication in #1293 applies to the new testinfra proof too: local check:push runs unit race
and then the additive integration selection. Preserve both existing gates in this change, and attach measured final
cost when considering their consolidation in #1293. The admitted syntax/dependency-loading probe is cost evidence,
not a new test-gap issue or a reason to weaken source accounting.

## Measured composed-gate cost

The final local check:push passed in 851.30s. Its two standalone cleanup admissions took 7.569s and 7.719s;
testinfra took 133.504s in unit race and 130.688s in additive integration race. The package therefore ran for
264.192s across both phases. These are complete-package durations, not a claim that all of that cost is new.
The isolated final guard measured 29.80s with a private empty Go build/export cache and 7.34s warm. Retain these
measurements with #1293's existing duplication work; preserve new-debt freshness, race, and additive integration
coverage during any consolidation. No runtime budget or gate bypass is inferred from these local samples.
