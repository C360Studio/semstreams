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
