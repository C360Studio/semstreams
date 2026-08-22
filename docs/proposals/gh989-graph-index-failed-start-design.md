# GH #989 graph-index failed-Start inventory

Baseline: `b56df74c430d2822239446197554aa4b81059caa` (`main`, including merged PR #1037 and #1038).

## Problem statement

Issue #989 alleges that failure while registering query subscription N can leave earlier graph-index responders alive and let a manager retry Start on a dirty instance. Repository truth has advanced since filing: PR #999 / commit `c84a9de7` added graph-index failed-Start ownership, and issue #986 was implemented by merged PR #997 / commit `81178583`, removing reactive component-Start retry in favor of one-shot cold-boot barriers. This inventory measures what remains unproved or nonconforming; it selects no target.

## Surface inventory

### 1. Claimed gap

The original five-step mechanism is no longer current in full.

- `Component.Start` rejects nil/canceled context, serializes lifecycle under `c.mu`, rejects `cleanupPending`, and rejects reuse after successful Stop at `processor/graph-index/component.go:575-606`.
- Before any bucket, goroutine, coalescer, watcher, or subscription can escape, Start derives the run context and publishes private `runCancel`, `runDone`, and `cleanupPending` at `processor/graph-index/component.go:608-626`.
- On any Start error, the deferred path seals the child join, attempts synchronous bounded owner-local cleanup, clears exact handles only on complete success, otherwise retains them, and joins cleanup failure to the Start error at `processor/graph-index/component.go:627-651`.
- `stopOwnedRuntime` drains every retained query subscription while the Start callback context is live, then cancels, joins the watcher/repair/status children, coalescer, and keyed dispatcher at `processor/graph-index/component.go:781-831`.
- A later Stop snapshots retained failed-Start handles, retries with its caller context, and clears them only after success at `processor/graph-index/component.go:722-750`; another Start is rejected while cleanup is pending at `:592-595`.
- The remaining production-context defect is exact: failed-Start cleanup derives `WithTimeout(context.Background(), 5s)` at `processor/graph-index/component.go:36-39,632-633`, while the sole accepted shared policy derives bounded cleanup from `context.WithoutCancel(parent)` and preserves parent values at `internal/lifecyclecleanup/lifecyclecleanup.go:12-37`. The current graph-index path bypasses that helper.
- The remaining proof gap is exact: `setupQueryHandlers` has no injectable acquisition seam and appends successful subscriptions incrementally at `processor/graph-index/query.go:23-79`. Existing graph-index lifecycle tests simulate `cleanupPending` and a blocked dispatcher, but never make subscription N fail after subscription 1 succeeds (`processor/graph-index/lifecycle_order_test.go:127-182`). Searches `rg -n "failed Start|failed-Start|query subscription|subscription N|duplicate responder" processor/graph-index/*_test.go` and `rg -n "subscribeForRequests" processor/graph-index --glob '*.go'` found no deterministic partial-subscription proof or seam.
- The issue premise that `startComponentWithRetry` may retry the same instance is stale. `rg -n "startComponentWithRetry|StartWithRetry|component.*retry|retry.*component.*Start" --glob '*.go' .` found only comments/unrelated retry lanes and no component Start retry. `ComponentManager` is one-shot (`lifecycleUsed`) and launches cold-boot barriers once at `service/component_manager.go:330-417,431-570`; framework boot fails closed on any component Start error at `openspec/specs/framework-composition/spec.md:152-182`.

### 2. Current spellings of the lifecycle fact

The modeled fact is exact ownership of resources acquired before Start commits, especially request/reply subscriptions whose callback authority is the Start context.

Acquisition order in graph-index is:

1. Private cleanup authority: derived run context/cancel, `runDone`, `cleanupPending` (`processor/graph-index/component.go:608-626`).
2. Cached vocabulary alias/name maps; no external resource (`:653-656`).
3. Config validation requires the four graph-index output bucket identities to be present but does not reject duplicate configured output entries. Start acquires sequentially once per configured output entry, assigning one Component field per identity, so a duplicate overwrites the prior handle to the same persistent catalog bucket; it then acquires the internal `NAME_INDEX` handle (`processor/graph-index/component.go:65-112,834-904`; catalog ownership at `graph/kvcatalog.go:115-121`). These are persistent derived topology and are not deleted on lifecycle rollback.
4. `GRAPH_STATUS` bucket handle and publisher (`processor/graph-index/component.go:668-677,907-922`).
5. In-memory revision watermark (`:679-681`).
6. Keyed dispatcher lanes and exact done join (`:683-686,1091-1100`; `processor/graph-index/keyed_dispatcher.go:12-74`).
7. Optional revision coalescer ticker/goroutine and done join (`processor/graph-index/component.go:688-697`; `processor/graph-index/revision_coalescer.go:18-71`).
8. Bounded synchronous ENTITY_STATES availability watcher (no background check is started), two catalog reader handles, then three Start-owned goroutines: entity watcher, repair loop, readiness metrics loop (`processor/graph-index/component.go:699-702,1041-1088`; `pkg/resource/watcher.go:106-136`). The entity watcher acquires its local `KeyWatcher` inside the child goroutine and owns `watcher.Stop` on every exit (`processor/graph-index/component.go:945-978`).
9. Eight request/reply subscriptions, acquired and appended in this exact order: outgoing, incoming, alias, predicate, predicateList, predicateStats, predicateCompound, byName (`processor/graph-index/query.go:23-79`).
10. Seal `runDone`, then commit `cleanupPending=false`, `running=true`, `startTime` (`processor/graph-index/component.go:708-719`).

Failure behavior by boundary:

- Bucket/dependency failure invokes the same deferred cleanup with whatever runtime handles exist. Persistent catalog buckets/handles require no deletion; derived state remains rebuildable.
- Query subscription failure retains every earlier appended exact `*natsclient.Subscription` until cleanup succeeds (`processor/graph-index/query.go:23-79`; `component.go:627-650`).
- `SubscribeForRequests` captures the passed Start context for every callback and derives each message context from it (`natsclient/request.go:341-428`), which is why drain must precede cancellation.
- `Subscription.Drain(ctx)` starts native Drain once, waits exact `SubscriptionClosed`, and lets a later caller rejoin the same native drain after deadline (`natsclient/client.go:746-823`; tests `natsclient/subscription_test.go:63-159`).
- Graph-index normal Stop is intentionally one-shot; retained retry authority applies only to incomplete failed-Start cleanup (`processor/graph-index/component.go:751-779`; ADR-095 lines 24-36).
- ComponentManager wraps each child with its own cancel/startDone/cleanupPending record, waits Start completion, and on failed boot later invokes the child `Stop(ctx)` through retained component authority (`service/component_manager.go:470-570,573-624,745-869`).

Context ownership inventory: production graph-index structs retain no `context.Context`; the only lifecycle authority field is private `context.CancelFunc` at `processor/graph-index/component.go:243-253`. Production searches for `context.Background|TODO|WithoutCancel` found the single graph-index hit at `component.go:632`; `TODO` and `WithoutCancel` were empty. The Start context is passed directly to dispatcher, coalescer, watcher, repair/status goroutines, and subscription callbacks.

### 3. Adjacent claims and boundaries

- ADR-095 requires exact native drain/Closed while callback authority remains live, then cancel/join; failed Start retains every exact handle on incomplete rollback and permits manager Stop retry (`docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:24-55`). It forbids a generalized lifecycle wrapper or public deletion knob.
- The archived lifecycle inventory historically classified graph-index owner #23 as P-only at `openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/inventory.md:62-109`; its immutable checkpoint later records that tasks 2.3/3.3 were outside an earlier subset (`:191-207`). The recovery ledger subsequently records the graph-index failed-Start correction and focused dispatcher proof at `.../recovery-ledger.md:2188-2246`, but it does not supply deterministic subscription-N failure proof.
- Current `service-shutdown` truth requires ComponentManager retained failed-Start authority and later caller-context Stop (`openspec/specs/service-shutdown/spec.md:114-126`). Current runtime-context truth forbids stored contexts and invented replacement contexts (`openspec/specs/runtime-context-ownership/spec.md:6-59`).
- #977 concerned the successful-running Stop race caused by clearing coalescer state before watcher join. Current Stop retains immutable resources and joins before terminal state (`processor/graph-index/lifecycle_order_test.go:15-105`). #989 must not reopen successful-running Stop semantics.
- Issue #986, implemented by merged PR #997 / commit `81178583`, established boot-only composition and removed reactive Start retry. #989 must not restore runtime component activation or manager retry.
- PR #1037 / merge `271a33ec` touches only agentic-loop evidence integrity/vocabulary/OpenSpec files; it creates no graph-index overlap. It is included in the baseline and does not expand #989.
- The current graph-index spec has readiness/indexing requirements but no failed-Start subscription-acquisition scenario (`openspec/specs/graph-index/spec.md:1-525`).
- Searches for `LLM|llm|persona|agent role|agentic|prompt|model` in `processor/graph-index` and the graph-index spec returned no runtime-agent surface. `service/component_manager.go` contains only the unrelated boot model registry dependency. No LLM persona, role, prompt, model call, or runtime agent is part of this lifecycle work.

### 4. Consumer at birth

No exported symbol, subject, bucket, config field, port, payload, or public knob is presently justified. The only measured missing testability surface is package-private subscription acquisition. Five sibling graph owners use the same concrete private `subscribeForRequests` field returning `*natsclient.Subscription`, consumed by production fallback to `natsClient.SubscribeForRequests` and package lifecycle tests: graph-index-spatial (`component.go:205-208`, `query.go:20-38`, `lifecycle_owner_test.go:82-157`), graph-index-temporal (`component.go:215-216`), graph-embedding (`component.go:329-330`), graph-query (`component.go:201-202`), and graph-clustering (`component.go:663-664`). Two additional same-class agentic owners use package-local `requestSubscription` interfaces and private seams: agentic-loop (`processor/agentic-loop/component.go:83,448-536,671-700`) and agentic-tools (`processor/agentic-tools/component.go:81,169-251,553-570,599-603`). There is no present consumer for a public lifecycle state machine, retry knob, failure index, subscription count, or cleanup API; each is excluded by the inventory.

## Same-class collision table

Semantic class: partial-Start request/responder acquisition and retained cleanup authority.

| Owner | Catalog/status | Lifecycle/ownership | Readers/writers | Recovery |
|---|---|---|---|---|
| graph-index | Eight fixed query subjects; GRAPH_STATUS health; private exact subscription slice (`query.go:23-104`; `component.go:360-361`) | Serialized Start, private cancel/done/cleanupPending; drains before cancel (`component.go:575-831`) | Graph-query, gateway, PathRAG and tests request these subjects; graph-index alone subscribes/writes responses | Deferred bounded rollback; incomplete handles retained; later Stop retries |
| `natsclient.Subscription` | No catalog; wraps one native subscription (`natsclient/client.go:746-767`) | Native Drain once plus exact Closed; Client does not own child cleanup (`:778-823`; ADR-095:38-42) | Component owners call Drain; NATS dispatch writes callbacks | Later Drain rejoins the same native completion after caller deadline |
| `internal/lifecyclecleanup` | No status/catalog | Stateless, fixed 5s parent-aware failed-Start policy (`internal/lifecyclecleanup/lifecyclecleanup.go:12-37`) | Component/manager failed-Start owners call it | Synchronous only; returns joined rollback/expiry error and retains no authority |
| ComponentManager / service Manager | Component state and fail-closed boot (`service/component_manager.go:548-569`; framework spec:152-182) | One-shot manager and per-child startDone/cleanupPending records (`component_manager.go:330-417,470-570`) | Composition root starts/stops; health reads managed state | Manager rollback/Stop calls exact child Stop under cleanup/caller contexts; no Start retry |
| graph-index-spatial | Two fixed query subjects; private subscription slice/seam (`component.go:205-208`; `query.go:20-38`) | One-shot F/Q owner with startDone and unresolved-handle retention (`component.go:452-664`) | Spatial query callers/tests | Parent-aware shared rollback; subscription-N causal tests (`lifecycle_owner_test.go:82-157`) |
| graph-index-temporal | One query subject; private seam (`component.go:215-216`; `query.go:16-26`) | Same one-shot F/Q family (`component.go:464-683`) | Temporal callers/tests | Parent-aware rollback and lifecycle-owner tests |
| graph-embedding | Three query subjects; private seam (`component.go:329-330`; `query.go:18-43`) | Same one-shot F/Q family (`component.go:623-878`) | Embedding callers/tests | Parent-aware rollback and lifecycle-owner tests |
| graph-query | Operation-derived query subjects; private seam (`component.go:201-202`; `query.go:72-91`) | Same one-shot F/Q family (`component.go:463-686`) | Gateway/research/fusion callers | Parent-aware rollback and lifecycle-owner tests |
| graph-clustering | Four query subjects; private seam (`component.go:663-664`; `query.go:18-49`) | Same one-shot F/Q family (`component.go:928-1252`) | Clustering callers/tests | Parent-aware rollback and lifecycle-owner tests |
| processor/agentic-loop | Request responders acquired through a package-local `requestSubscription` interface and private seam (`processor/agentic-loop/component.go:83,448-536`) | Retains exact handles and drains them during cleanup (`:671-700`) | Agentic dispatch/loop callers | Parent-aware failed-Start rollback; out of #989 scope |
| processor/agentic-tools | Request responders acquired through a package-local `requestSubscription` interface and private seam (`processor/agentic-tools/component.go:81,169-251`) | Retains exact handles and drains them during cleanup (`:553-570,599-603`) | Agentic tool callers | Parent-aware failed-Start rollback; out of #989 scope |
| graph-ingest | Canonical query responders plus ingest runtime (`component.go:880-1064`; `query.go:24+`) | Distinct consumer/pool Q/F owner; no proposed reuse | Mutation/query callers | Owner-local exact cleanup; out of #989 scope |

No new durable primitive or communication path is proposed by the inventory; KV-vs-stream does not trigger. No new orchestration path, payload, or remote query operation is proposed.

## Adopter seam inventory

Specific adopter: a developer in a sister product composing the standard graph-index component without opening its implementation.

1. What must they know? No new fact. Existing framework facts remain: provide valid graph-index config, call lifecycle boundaries with nonnil contexts, and let ComponentManager own boot/Stop. They do not need subscription order, subject cleanup, cleanup timeout, or responder identity.
2. What happens if they do nothing? The normal path is unchanged. A partial subscription failure makes boot fail closed; framework-owned rollback removes admitted responders before cancellation. If cleanup remains incomplete, the same instance rejects another Start and manager Stop can retry. There is no silent dirty retry.
3. Where do they find out? Start failure is a typed/runtime boot error naming graph-index and prevents HTTP/healthy boot (`framework-composition` spec:152-182). A direct caller attempting reuse while cleanup is pending receives an immediate Start error (`component.go:592-595`). No correctness fact is left only in docs/logs.
4. What should they have to know? Nothing beyond the existing LifecycleComponent contract. The framework can observe which subscriptions were acquired and their actual drain result; asking the adopter to predict failure position, enumerate subjects, set a rollback timeout, or manually unsubscribe would be a design defect.

The seam gap is therefore internal proof/ownership, not adopter education. The candidate work must preserve zero schema/config/API changes and must not ask callers to predict subscription count, callback completion, or cleanup timing.

## Open evidence questions for inventory review

- Does the accepted inventory agree that #989 cannot close on current evidence because no test makes subscription N fail after a prior success?
- Does the single production `context.Background()` cleanup root require correction to the accepted parent-aware helper in the same atomic change?
- Is the existing package-private `subscribeForRequests` seam used by five sibling graph owners the smallest non-public test seam, without expanding their scope?

# Design appendix

## Inventory review

Independent SemStreams inventory review: `INVENTORY PASS` on SHA-256 `00de531276a2e13e5509bae8168d83794b36947c2a0211829481ad6cbe1f2b1f`.

## Options considered

1. **Close #989 on current code/evidence.** Lowest diff, but leaves no deterministic subscription-N proof and preserves the one graph-index production `context.Background()` rollback root contrary to the accepted parent-aware helper. Cost: the exact incident class remains unproved and context provenance remains divergent.
2. **Add only a private subscription-acquisition seam and tests.** Proves partial acquisition without API/schema change, but leaves the production rollback-root defect. Cost: tests bless a path that still bypasses the accepted lifecycle policy.
3. **Minimal owner-local correction plus protocol proof (recommended).** Reuse the existing private seam shape from five sibling graph owners, route graph-index failed-Start cleanup through `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`, and add deterministic real-NATS causal tests at subscription 2. Cost: one private function field, a small setup fallback, one helper import/call, focused tests, and a graph-index spec delta.
4. **General lifecycle state machine, public failure knob/count, or Client subscription catalog.** Would centralize more mechanics, but directly conflicts with ADR-095, duplicates existing native authority, and bills adopters for internal facts. Cost: new public/stateful surface and broader migration; rejected.
5. **No seam; induce connection loss in integration.** Avoids a production private field but cannot deterministically fail exactly at subscription N without timing/race coupling. Cost: flaky proof that cannot distinguish the causal boundary; rejected.

## Recommendation and exact target

Recommend option 3. Owner acceptance is required before implementation.

1. Scope is only graph-index failed-Start query-subscription proof and its rollback-context correction. Do not alter successful-running Stop semantics, sibling owners, ComponentManager behavior, query subjects/handlers, KV topology, readiness, configuration, schemas, ports, payloads, or public APIs.
2. In `processor/graph-index/component.go`, remove the graph-index-local `failedStartCleanupTimeout` and save `parent := ctx` before deriving the Start runtime context. In the deferred failed-Start path, invoke `lifecyclecleanup.RollbackFailedStart(parent, func(cleanupCtx context.Context) error { return c.stopOwnedRuntime(cleanupCtx, exact captured handles...) })`. Preserve synchronous execution, joined Start/cleanup errors, and handle clearing only when rollback returns nil.
3. Preserve `stopOwnedRuntime` order: attempt `Drain` on every acquired query subscription while the Start callback context remains live; only after all drain attempts call the private cancel; then await `runDone`, coalescer done, and pool done under the cleanup context. Do not delete persistent catalog buckets or detach cleanup.
4. Add to graph-index Component the same package-private test seam shape already used by graph-index-spatial/temporal/embedding/query/clustering:
   `subscribeForRequests func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (*natsclient.Subscription, error)`.
   In `setupQueryHandlers`, select this field when nonnil, otherwise call `c.natsClient.SubscribeForRequests`. Do not initialize it in configuration or expose it from the factory.
5. Failure injection is fixed at subscription 2 (`graph.index.query.incoming`) after subscription 1 (`graph.index.query.outgoing`) succeeds. The existing eight-subject order remains byte-for-byte and is asserted by the test rather than refactored into a new registry.
6. Successful bounded rollback clears `runCancel`, `runDone`, pool, coalescer, subscription slice, and `cleanupPending`; a direct same-instance Start is then cleanly eligible under existing behavior. Incomplete/expired rollback retains the exact handles and `cleanupPending`, rejects another Start, and later `Stop(callerCtx)` retries with that caller context. ComponentManager/Manager remain one-shot and never retry component Start.
7. No LLM persona, role, prompt, model call, runtime agent, ops agent, or scenario is required. This is deterministic lifecycle/resource ownership.

## Measurable premises

- Eight sequential subscription acquisitions and incremental append: `processor/graph-index/query.go:23-79`.
- Start-owned authority precedes acquisition and existing cleanup order is drain/cancel/join: `processor/graph-index/component.go:608-650,781-831`.
- Only production graph-index root invention is `component.go:632`; canonical parent-aware helper is `internal/lifecyclecleanup/lifecyclecleanup.go:12-37`.
- Five sibling graph owners already carry the exact private concrete seam; two agentic owners use interface variants. The recommendation extends an admitted package-private testability idiom and exports nothing.
- Manager Start retry search is empty; current manager/component manager are boot-only/fail-closed (`service/component_manager.go:330-417,431-570`; `openspec/specs/framework-composition/spec.md:152-182`).
- Existing manager proof already covers retained failed-Start cleanup and later Stop (`service/lifecycle_context_contract_test.go:413-445`); #989 needs graph-index-specific exact-subscription proof, not another manager mechanism.

## Exact causal tests

Place graph-index-specific tests in a new `processor/graph-index/failed_start_subscription_test.go` (or, if the developer can keep `lifecycle_order_test.go` cohesive, append there; one file only).

1. `TestComponentFailedStartSecondQuerySubscriptionRollsBackBeforeCancel`
   - Run a real embedded JetStream NATS, create ENTITY_STATES, initialize graph-index.
   - Private seam delegates outgoing acquisition to the real client; incoming acquisition returns a sentinel.
   - Before returning the sentinel, publish one outgoing request whose wrapped callback is admitted and channel-blocked.
   - Start runs in a goroutine. While the callback is blocked, assert Start has not returned and callback context is not canceled. Release it; assert callback returns before Start returns, Start contains the subscription sentinel, rollback succeeds, exact lifecycle handles/slice are cleared, and child joins completed. Use channels, not sleeps.
2. `TestComponentFailedStartSubscriptionRollbackExpiryRetainsAuthorityForCallerStop`
   - Same deterministic failure at incoming, but keep the admitted outgoing callback blocked through the fixed 5s cleanup deadline.
   - Assert Start joins sentinel plus deadline, retains exactly the acquired outgoing subscription and cleanup authority, and a second Start is rejected as cleanup pending.
   - Release the callback. Call `Stop` with an observed caller context; prove Drain observes that caller authority, cleanup clears only after native close/child joins, and repeated Stop is nil/no-op. This combines with existing ComponentManager retained-cleanup proof at `service/lifecycle_context_contract_test.go:413-445`; do not edit service manager code/tests.
3. `TestComponentRetryAfterSuccessfulFailedStartHasOneResponderPerSubject`
   - After the successful rollback case, disable the failure seam and Start the same clean instance.
   - Assert the seam observed the exact eight subjects once in canonical order for the committed run.
   - Send a raw outgoing request with a unique reply inbox; receive exactly one response and prove no second response before a bounded negative deadline. This is responder-count proof, not payload semantics.
   - Stop and prove no responder remains. Do not use arbitrary sleeps.
4. Run focused tests with `-race`. Existing `natsclient.Subscription` tests continue to own generic native Drain/rejoin semantics; do not duplicate or modify natsclient.

## Spec delta draft

Modify `openspec/specs/graph-index/spec.md` through an OpenSpec change with:

### Requirement: Failed graph-index Start owns partial query responders

Graph-index MUST publish private failed-Start cleanup authority before any Start-owned resource can escape. If query subscription acquisition fails after one or more prior subscriptions succeeded, Start MUST attempt one bounded synchronous rollback derived from the Start parent through the canonical failed-Start helper. It MUST attempt native Drain for every acquired query subscription while callback authority remains live, then cancel and join every Start-owned child. It MUST clear exact handles only after complete rollback.

If rollback fails or expires, graph-index MUST retain every exact cleanup handle, reject another Start on that instance, and permit later manager Stop to retry cleanup with the Stop caller context. A clean rollback MAY permit the existing direct same-instance Start behavior, but no manager Start retry is introduced. Neither path may leave a duplicate responder. No public lifecycle state, cleanup knob, subscription count, subject, schema, or configuration is added.

#### Scenario: second subscription failure rolls back the first responder

- **GIVEN** outgoing subscription acquisition succeeded with an admitted callback and incoming acquisition fails
- **WHEN** failed-Start rollback runs
- **THEN** outgoing Drain is attempted and its callback completes while Start callback authority is live
- **AND** only then are runtime children canceled and joined
- **AND** Start returns only after bounded rollback resolves or retains exact cleanup authority

#### Scenario: incomplete rollback rejects reuse and later Stop completes cleanup

- **GIVEN** second-subscription failure and outgoing Drain cannot complete within the failed-Start budget
- **WHEN** Start returns and another Start is attempted
- **THEN** exact cleanup authority remains retained and the second Start is rejected
- **AND** later manager Stop retries with its caller context and clears authority only after cleanup succeeds

#### Scenario: clean retry has no duplicate responders

- **GIVEN** partial query acquisition failed and bounded rollback completed successfully
- **WHEN** the existing direct caller starts the clean instance again
- **THEN** each canonical graph-index query subject has exactly one responder
- **AND** no responder from the failed attempt remains

## OpenSpec/task draft

Create change `close-graph-index-partial-start-subscriptions` with proposal, tasks, and graph-index spec delta only. Tasks: materialize reviewed inventory/design; RED deterministic N=2 causal tests; implement private seam and parent-aware helper; GREEN focused `go test -race ./processor/graph-index`; run full lint/unit-race/integration/schema/strict OpenSpec gates; independent SemStreams review; merge and close #989. No ADR is required because ADR-095 already owns the decision.

## Skill disposition

No canonical decision skill triggers: no new communication path (`kv-or-stream`), orchestration (`orchestration-check`), payload (`new-payload`), or remote query operation (`query-pattern`) is introduced. Existing request subjects and handlers are unchanged.

## Owner rulings requested

1. Accept that current code is not sufficient to close #989 because deterministic partial-subscription proof is absent and the rollback root bypasses the accepted helper.
2. Accept option 3 and the exact subscription-2 failure boundary.
3. Accept the private concrete `subscribeForRequests` seam and zero exported/adopter surface.
4. Accept graph-index-only production ownership: `component.go`, `query.go`; one focused graph-index test file; OpenSpec/docs owned by technical writer. No service/sibling/natsclient production edits.
5. Accept no LLM/persona/runtime-agent requirement.
