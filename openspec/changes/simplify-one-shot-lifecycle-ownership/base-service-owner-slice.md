# BaseService one-shot owner slice

## Purpose and authority

This artifact materializes the architect-selected next owner slice for
`simplify-one-shot-lifecycle-ownership`. It is a line-addressable design handoff, not implementation evidence. All
code citations and census values in this artifact refer to clean merged `main` at
`c5953972bbc56f013bf4665674e99f03c11395f6` unless stated otherwise.

On 2026-08-19 the owner asked to resume the lifecycle goal from the recovery ledger and select the next single
bounded owner slice. Before implementation, the architect selected `service.BaseService` and limited approval to the
surface and rulings below. This artifact records that selection; it does not approve any expansion or claim that the
runtime change or its proof has completed.

The recovery ledger remains execution authority. Implementation Gate A and task 2.3 remain incomplete.

After implementation review, the architect approved one correction-propagation expansion: the BaseService-only test
block at `service/service_manager_stopall_test.go:125-143`. That block invokes no `Manager`; its filename does not
change its ownership. This correction adds no production owner and makes no runtime-completion claim.

The architect also approved a comment-only correction at `service/service_manager_stopall_test.go:27-29`. Its test
body remains unchanged and proves exact `StatusStopped`, not `StatusStopping`. This wording correction adds no Manager
behavior and does not expand any other test scope.

## Baseline checkpoint

The live production census at the clean baseline was:

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 37 |
| `lifecyclejoin.NewGeneration` | 39 |
| Calls on `Generation.Stop` | 44 |
| External `Generation.Cancel` | 4 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| Calls on lifecycle `Operation.Run` | 3 |
| External old-symbol rollback calls | 20 |

These counts are the pre-implementation comparison point for this slice. They do not supersede historical census
checkpoints in `recovery-ledger.md` or `inventory.md`.

## Problem and surface inventory

At the baseline, `BaseService` is an S owner whose local work is expressed through the generic lifecycle framework:

- `service/base.go:83-114` stores a lifecycle mutex, `*lifecyclejoin.Generation`, and a retained `terminalErr`.
- `service/base.go:231-275` validates the Start context, derives the runtime context, constructs a two-member local
  wait group, installs `lifecyclejoin.NewGeneration`, and launches the health and context monitors.
- `service/base.go:284-335` calls `Generation.Stop`, stops the health ticker in its pre-cancel phase, finalizes service
  status, clears the generation after a caller context that remains live, and retains the Stop result.
- `service/base.go:381-398` owns the health-monitor goroutine. It performs the initial health check, then watches the
  runtime context and health ticker until cancellation.
- `service/base.go:442-468` owns the context-monitor goroutine. Parent cancellation drives the same service-status and
  ticker terminalization path.
- `service/base.go:470-485` exposes unchanged `Service.Start(context.Context)` and `Service.Stop(context.Context)`
  signatures. This slice adds no exported API.

The generic generation makes restart, timeout rejoin, and retained-result behavior available even though this owner
needs only one private cancellation authority and one local join. The baseline test surface preserves those broader
semantics: `service/base_lifecycle_test.go:11-34` rejoins after a canceled Stop and restarts the same instance, while
`service/base_test.go:329-361` independently requires same-instance restart. Those expectations conflict with the
approved one-shot target and belong to this slice.

Implementation review also found `service/service_manager_stopall_test.go:125-143`. Despite its file location, that
test constructs only `BaseService` and never invokes a `Manager` or `StopAll`. Its manual `StatusStopping` assignment
preserves the false assumption that stopping status proves owner completion. Only that BaseService block is included
for correction; the actual Manager/StopAll coverage at lines 101-123 and every other test in the file remain outside
this slice.

`BaseService` is embedded by production services, including the two manager surfaces:

| Caller surface | Baseline evidence | Disposition |
|---|---|---|
| `ComponentManager` | `service/component_manager.go:40,184-206,410,594-615` | Caller only; no manager edit. |
| `Manager` service registry | `service/service_manager.go:30,75,557,645-658` | Caller only; no manager edit. |
| Flow service | `service/flow_service.go:40,82-106,158` | Caller only. |
| Storage observability | `service/storage_observability.go:157,256-325` | Caller only. |
| Heartbeat | `service/heartbeat.go:49,108-147,189` | Caller only. |
| Metrics forwarder | `service/metrics_forwarder.go:118,169-249` | Caller only. |
| Log forwarder | `service/log_forwarder.go:65,83-117` | Caller only. |
| Metrics | `service/metrics.go:20,86-182` | Caller only. |
| Milestone service | `service/milestone_service.go:44,62-133` | Caller only. |
| Message logger | `service/message_logger.go:178,252-698` | Caller only. |

The callers continue to compose a fresh `BaseService`, call `Start(ctx)`, and call `Stop(ctx)`. Their own native
handles, failed-Start paths, and terminal ordering are separate owner slices.

No context is retained by `BaseService` at the baseline: Start derives a runtime context and retains only the cancel
authority inside `Generation`. The selected change must preserve that fact while removing the generic wrapper.

## Candidate disposition

The adjacent graph candidates are not substitutes for this bounded S slice. Current repository evidence shows that
each retains exact NATS request-subscription handles and unsubscribes them during Stop. Their earlier S labels are not
implementation authority for this selection; they require Q protocol ordering and Gate B proof.

| Candidate | Native-handle evidence | Selection disposition |
|---|---|---|
| `processor/graph-query` | `component.go:194,543-550`; `query.go:83-87` | Q; defer to Gate B. |
| `processor/graph-clustering` | `component.go:656,1083-1091`; `query.go:19-45` | Q; defer to Gate B. |
| `processor/graph-embedding` | `component.go:322,739-747`; `query.go:19-39` | Q; defer to Gate B. |
| `processor/graph-index-spatial` | `component.go:199,534-541`; `query.go:22-34` | Q; defer to Gate B. |
| `processor/graph-index-temporal` | `component.go:208,554-561`; `query.go:17-22` | Q; defer to Gate B. |
| `service.BaseService` | `service/base.go:103-114,264-273,381-398,442-468` | S; selected for Gate A. |

`BaseService` owns a ticker and two context-driven goroutines but no subscription, consumer, server, WebSocket,
exporter, or worker-pool handle. It therefore does not require the Q ordering of native admission close, callback
drain, exact Closed observation, cancellation, and local join.

The shared decision skills for communication primitives, orchestration, payloads, and queries do not trigger. This
slice adds none of those surfaces.

## Adopter seam inventory

The affected adopter is a developer outside this repository who embeds `BaseService` in a concrete service.

### What must they know?

They must pass non-nil operation contexts, treat a `BaseService` instance as one-shot, and compose a fresh instance
for a new boot after terminal Stop. A caller may bound Stop with cancellation or a deadline; that return describes
the caller's wait, not permission to restart or rejoin the terminal operation.

### What happens if they do nothing?

Existing normal composition remains unchanged: construct once, call `Start(ctx)`, then call `Stop(ctx)`. Completed
repeated Stop is a successful no-op. Only callers that attempt same-instance restart observe a change, and they
receive an explicit error instead of a silent replacement generation.

### Where do they find out?

The unchanged typed `Start` and `Stop` boundary reports nil or canceled contexts and same-instance reuse as runtime
errors. The `Service` contract reports completed repeated Stop as idempotent. No framework-internal name, subject,
bucket, generation, or cleanup budget becomes adopter input.

### What should they have to know?

Only the ordinary one-shot service lifecycle: construct, Start, Stop, and construct fresh for the next boot. They
should not know about `lifecyclejoin`, cancellation storage, wait groups, goroutine counts, ticker sequencing, rejoin,
or retained results. The framework observes owned completion; the adopter predicts no internal value.

## Exact scope

### In scope

- Production implementation in `service/base.go` only.
- Focused BaseService behavior in `service/base_lifecycle_test.go`.
- BaseService-specific lifecycle expectations in `service/base_test.go`.
- BaseService-specific cases in `service/lifecycle_context_contract_test.go`.
- The BaseService-only block at `service/service_manager_stopall_test.go:125-143`; it invokes no `Manager`.
- Comment-only correction at `service/service_manager_stopall_test.go:27-29`; the test body remains unchanged.
- Census measurement and focused/package race proof for this single owner.

### Out of scope

- Concrete services that embed `BaseService`, including their rollback and native-handle behavior.
- `ComponentManager`, service `Manager`, and every other manager or composition owner.
- Manager/StopAll behavior and every test body in `service/service_manager_stopall_test.go`, including lines 101-123.
  Only the comment at lines 27-29 and the BaseService-only block at lines 125-143 are exceptions.
- Shared component lifecycle suites and generic lifecycle-test semantics.
- NATS consumers, request subscriptions, servers, WebSockets, exporters, worker pools, and client catalogs.
- Any other `lifecyclejoin` owner, helper declaration, or helper test.
- Specs, ledger entries, task checkboxes, release evidence, archive state, and tags.
- Every sister repository, which remains read-only under the repository ownership boundary.

## Binding rulings

1. Replace `BaseService`'s `Generation` and retained terminal result with private owner-local cancellation and
   done/WaitGroup ownership. Do not retain a `context.Context`.
2. Start rejects nil or already-canceled context before changing lifecycle state. It publishes cancellation and join
   authority before either owned goroutine can escape Start.
3. The instance is one-shot. Start after any accepted terminal Stop rejects; a fresh composition is the restart
   mechanism.
4. Stop validates its context, makes the terminal transition once, stops local ticker admission, and cancels the run
   context before awaiting owner completion with the exact caller-provided context.
5. A canceled or expired Stop reports that context honestly. It creates no replacement root, detached cleanup,
   retained result, rejoin channel, or later rejoin path.
6. Completed repeated Stop returns nil and performs no terminal work again. It neither replays a prior error nor
   elects or joins a concurrent Stop executor.
7. Parent cancellation follows the same owner-local convergence path: both owned goroutines exit, their join closes,
   final status is stopped, and a later completed Stop is a nil/no-op.
8. Health callbacks do not begin after terminal shutdown starts. A callback already admitted by the health monitor
   remains part of that owned goroutine and must return before a successful join.
9. Add no generic lifecycle abstraction, operation coordinator, native-handle protocol, exported lifecycle surface,
   or change to a concrete service or manager.

## Acceptance checks

1. Focused tests deterministically prove canceled Start rejection, cancel-before-bounded-join, honest canceled and
   deadline Stop returns, no rejoin after timeout, completed repeated Stop, same-instance restart rejection, parent
   cancellation convergence, and owned-goroutine join. Synchronization is explicit; no arbitrary sleep establishes
   an ordering claim.
2. Existing nil-context and health-callback ordering coverage remains green after removal of generic-generation
   expectations. BaseService restart tests require a fresh instance, not same-instance generation replacement. The
   corrected BaseService-only idempotency test must not treat `StatusStopping` alone as proof of owner completion.
   The corrected comment at `service/service_manager_stopall_test.go:27-29` describes the unchanged body's exact
   `StatusStopped` case and makes no broader Manager claim.
3. Focused BaseService tests pass under `go test -race`; the complete `./service` package passes under
   `go test -race` with the integration build mode used by that package where required.
4. `git diff --check` passes for the bounded implementation and test diff.
5. A production-only census against the accepted implementation commit shows exactly this delta:

| Measurement | Before | Expected after | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 37 | 36 | -1 |
| `lifecyclejoin.NewGeneration` | 39 | 38 | -1 |
| Calls on `Generation.Stop` | 44 | 43 | -1 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| Calls on lifecycle `Operation.Run` | 3 | 3 | 0 |
| External old-symbol rollback calls | 20 | 20 | 0 |

Any additional production census movement is outside this approval and requires a new owner-reviewed slice.

Passing these checks can support an `Owner migrated` checkpoint for `service/base.go` at an exact reviewed commit.
It does not complete Gate A, task 2.3, the runtime migration, controlled or dirty proof, archive readiness, or tag
readiness.
