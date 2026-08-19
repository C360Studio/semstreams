# Remaining lifecycle-owner family-wave design

> **Status:** Target design only. **NOT owner-approved; pending independent design review.** No implementation, owner,
> gate, proof, release, archive, or tag credit is granted.

## Evidence identity

Accepted inventory: `remaining-owner-archetype-inventory.md`, baseline
`269e0ac94b28c6f6162d8f5d144ca545b393df85`, reviewed artifact SHA
`9d51ef0de6d069554a9340a98e214685e96ac936dd35bff30b925ea008d50780`, independent `INVENTORY PASS`. This design
preserves its 36-owner membership and exact census.

## Options considered

1. Do nothing. Cost: no API break and no migration work, but ADR-095/tasks 2.1-2.3 remain unimplemented; Client/name
   catalogs and running-result rejoin remain lifecycle authority. Reject.
2. One atomic 36-owner rewrite. Cost: few commits, but it mixes 25 core-sub owners, 17 helper JS owners, six HTTP
   surfaces, three pool/Operation protocols, context removal, and unique managers. Failure attribution and review are
   not bounded. Reject.
3. Thirty-six per-owner slices. Cost: strongest isolation, but repeats the same fixed-subscription and startDone
   mechanics 36 times and recreates the ceremony the user explicitly changed. Reject.
4. One “core-only” wave combining Research and graph-read. Cost: ten owners in one review, but Research is M-primary
   (Start unlocks before acquisition and requires startDone/failed-Start authority) while graph-read is serialized Q/F
   with worker/watcher exceptions. Shared Subscription tests do not make their Start contracts equivalent. Do not
   combine; run them concurrently after F0 with one shared proof matrix.
5. Recommended: frozen contract-equivalent family batches plus eight singleton protocol exceptions, one stateless
   prerequisite, one atomic non-port acquisition/bootstrap wave, and one final NATS retirement wave. This reduces 36
   owner reviews to 15: seven multi-owner batches cover 28 owners; only the eight genuinely unique owners remain
   singletons.

## Recommended rulings for owner adoption

D1. Each of the 36 owners appears in exactly one frozen owner wave below. Moving a member requires an explicit split
ruling; implementation does not silently reshuffle membership.

D2. F0 is the next prerequisite slice, but the helpers land immediately in their final narrow home: package
`internal/lifecyclecleanup`, symbols `Wait` and `RollbackFailedStart`. Migrated waves import only that package.
`internal/lifecyclejoin.RunPartialStartRollback` becomes a temporary forwarding compatibility symbol for unmigrated
owners; no migrated wave calls it. N1 deletes `internal/lifecyclejoin` in full, so both the package-import and
old-symbol zero gates are reachable.

D3. Non-port consumption cuts over atomically in I1 because its complete production call-site set can be migrated
together. Port-backed consumption uses a temporary, target-shaped handle-return bridge born with immediate in-repo
consumers in S1, then the bridge is removed during N1’s canonical signature cutover. The branch must not tag/release
the temporary bridge.

D4. Existing core `Subscription.Drain(ctx)` remains mechanically callable during owner migration, but every migrated
owner calls it exactly once. Its stored once/result/rejoin state retires only in N1 after all 25 remaining owners have
left resumable running Stop. Changing it early would break current retry/rejoin paths rather than unlock them.

D5. HTTP has no new generic provider. Each native server stays with its measured owner. `metric.Server` is repaired
only inside M1; pprof remains the explicit process-lifetime exception and is out of owner waves.

D6. Component/service lifecycle interfaces, config, subjects, ports, durable names, and Prometheus endpoint behavior
remain unchanged. Normal Stop never deletes durable topology.

D7. Only final `lifecyclecleanup.RollbackFailedStart` may remain. Every old
`lifecyclejoin.RunPartialStartRollback` call migrates with its owner; old calls and every production lifecyclejoin
import are zero at N1.

D3 and D4 change exported `natsclient`; M1 changes exported `metric.Server`. They require explicit owner API rulings.
The user’s batch-process approval does not approve them.

## Global owner contract

1. Start rejects nil where its interface can return error, validates before acquisition, and derives work only from
   the received Start context.
2. The owner retains only exact native handles, private cancel functions, done channels/WaitGroups, and phase data. No
   production context is retained or recovered through a provider.
3. A serialized owner holds one transition authority across complete Start/Stop selection and needs no startDone. An M
   owner publishes a cleanup record and `startDone` before acquisition can escape; Stop waits for Start finalization
   before choosing running versus cleanupPending.
4. Failed Start attempts one bounded synchronous rollback. Success clears authority. Failure/expiry retains every
   exact acquired handle in cleanupPending, rejects another Start, and lets later manager Stop retry cleanup with its
   caller context. This is not running-generation rejoin.
5. Running Stop rejects nil before state inspection, fences admission, invokes native Drain/Shutdown, awaits exact
   Closed/serveDone while callback/request context remains live, cancels remaining runtime, joins owner work under the
   caller context, then performs terminal cleanup. It holds no owner/manager gate lock across native shutdown,
   callback completion, or child code.
6. A pull-loop owner without native callback admission fences new fetch, cancels the loop, joins it, then shuts down
   the exporter/child under the Stop context.
7. Completed repeated Stop returns nil with no teardown and no retained-result replay. Concurrent Stop and
   same-instance restart are not contracts. A running Stop deadline is non-clean process-exit evidence, not later
   rejoin authority.
8. Owner lifecycle records never retain consumer names as shutdown authority. Identity labels may remain only in
   read-only observation records separated from lifecycle.
9. No Start/Stop path calls Client Stop/Delete by name. Deletion remains fixture/admin-only.

## F0 — final stateless lifecycle-cleanup primitives

Owner membership: none.

Exact production scope:

- final package `internal/lifecyclecleanup`;
- `Wait(ctx context.Context, done <-chan struct{}) error`;
- `RollbackFailedStart(rollback func(context.Context) error) error`;
- tests in same final package;
- compatibility-only `internal/lifecyclejoin.RunPartialStartRollback`, forwarding to
  `lifecyclecleanup.RollbackFailedStart` for unmigrated owners.

`Wait` stores nothing, starts no goroutine, rejects nil context and nil completion, returns nil on exact completion,
returns `ctx.Err()` otherwise, and deterministically prefers already-observable completion over simultaneous
cancellation. `RollbackFailedStart` is synchronous, starts no detached work, invokes callback under accepted fixed
five-second timeout-only cleanup context, accepts nil callback as nil, and returns callback/timeout failure without
retaining it.

F0 does not add `Wait` to legacy lifecyclejoin. It does not move Generation/Operation. Every later owner wave replaces
old rollback with final RollbackFailedStart and uses Wait for exact done. N1 deletes forwarding and lifecyclejoin after
zero imports.

TDD: final-package failures for nil ctx/done, closed completion, cancellation/race, zero goroutine/state, nil rollback,
callback error, timeout, sync return; compatibility equality test. Gate
`go test -race ./internal/lifecyclecleanup ./internal/lifecyclejoin`. F0 census zero; old rollback stays 20 until waves.

## I1 — atomic non-port native ownership + Milestone pair

Frozen owner membership: `service/milestone_service.go`, `agentic/agentrun/agentrun.go`.

Supporting current non-port consumers in the same atomic boundary: `internal/maxdelivery/observer.go`;
zero-present-consumer `component.Registry.SubscribeCapabilities`; `natsclient` internal-consumption
implementation/tests/docs. `ConsumeDurable` is port-backed settlement composition and is explicitly outside I1; N1
alone owns its removal.

Contract: canonical `ConsumeInternalStreamWithConfig` returns exact `jetstream.ConsumeContext`; all fallible
setup/observation precedes `Consumer.Consume`; fixed internal durable duplicates reject; no Client catalog/name stop.
Agentrun owns both completion/failure handles and returns one opaque closure to MilestoneService. Maxdelivery keeps its
root-facing stop closure but captures the exact handle. SemTeams still knows no native handle/name. With refreshed
zero present consumers, `Registry.SubscribeCapabilities` is removed rather than preserved as an error-only phantom
lifecycle surface. That exported removal and the signature change are owner-gated.

Exceptions: missing MaxDeliver stream remains existing no-op Start; second agentrun consumer failure cleans first or
retains it in cleanupPending. Maxdelivery policy observation remains separate.

Proof: NATS integration for two-handle partial failure, exact Drain/Closed, duplicate fixed durable rejection without
incumbent replacement, maxdelivery Stop, no deletion, failed rollback then later Milestone Stop, repeated completed
Stop nil; race packages `./agentic/agentrun ./service ./internal/maxdelivery ./natsclient ./component` plus relevant
integration tests.

Delta: owners -2, NG -1, Stop -1, Operation -1; old lifecyclejoin rollback -1.

## S1 — serialized fixed-port Q/F batch + port bridge birth

Frozen membership (6): document, IoT, weather, json_filter, json_generic, json_map owner files.

S1 introduces temporary migration-only `ConsumeStreamWithConfigHandle` (and split-context counterpart only with first
consumer) returning exact `ConsumeContext`. Fallible setup first, duplicate reject, no bridge-path Client catalog.
Born with five JS members; Weather core-only. Canonical error-only method remains for unmigrated callers until N1. No
release/tag exposes both as supported public contracts.

Shared contract: serialized Start/Stop; fixed subscription set; exact handle per acquired port; partial acquisition
retains/cleans handles; core Drain once; JS Drain/Closed; cancel/join; terminal cleanup. Exceptions: Document/IoT
twins; JSON variants; Weather core-only.

Proof: shared lifecycle matrix in all six plus package tests; blocked callback Drain before cancel; second-sub failure
exact cleanupPending; duplicate integration; race serialization. Delta owners -6, NG -6, Stop -12; old lifecyclejoin
rollback -6.

## R1 — Research core-subscription M family

Membership (5): assess, classify, execute, route, synthesize. Start publishes startDone/cleanup before unlock; exact
core subs; Drain once; cancel/join; LLM closes after callbacks. Execute no LLM; classify optional LLM. Common M proof.
Delta -5 owners/NG/Stop; old lifecyclejoin rollback -5. F0 only; concurrent with G1.

## G1 — graph-read serialized Q/F family

Membership (5): graph-query, graph-clustering, graph-embedding, spatial, temporal plus adjacent query files. Serialized
exact incremental subs; rollback; Drain once before cancel; terminal child close. Exceptions as inventoried. Common
serialized proof. Delta -5 owners/NG/Stop. F0 only; concurrent with R1.

## A1 — agentic M+Q/F family

Membership (5): dispatch, governance, loop, model, tools plus adjacent dispatch/loop files. Existing startDone, exact
bridge handles, no name lifecycle; unique GraphView/observation/sweeper/client/tool-list exceptions. Common M proof +
exceptions + integration/e2e. Delta owners -5, NG -5, Stop -10; old lifecyclejoin rollback -5. F0+S1; loop closes
observation separation.

## O1 — static output sinks

Membership: file + httppost. Serialized exact JS/core handles; partial cleanup; callback drain before sink close. File
flush/close; HTTPPost ACME moved from constructor to Start. Deterministic proof. Delta owners/NG/Stop -2. F0+S1.

## H1 — standalone HTTP component family

Membership: graph gateway, input websocket, output websocket. Synchronous bind; BaseContext exact Start; fence
upgrades/requests; Shutdown while context live; await serveDone/callbacks; then cancel/join. No generic provider.
Unique readiness/connections/output NATS/root exceptions; pprof out. HTTP/NATS proof. Delta owners/NG -3, SWQ -3;
old lifecyclejoin rollback -1. F0; output also S1.

## M1 — Metrics + metric.Server singleton

Membership service/metrics.go with metric/handler.go. Exact listener/server/serveDone; bind before BaseService commit;
BaseContext exact Start; caller-bounded Shutdown outside locks; rollback reachable; repeat nil. Existing metric.Server
signatures become context-bearing, no second surface; owner-gated. Delta owner/NG/Stop/Cancel -1; old lifecyclejoin
rollback -1. F0.

## SM1 — ServiceManager multi-HTTP singleton

Membership: service/service_manager.go. Three server generations owner-local; sync bind/BaseContext/Shutdown/serveDone;
health publisher cancel/done; StopAll reverse aggregation unchanged; same-instance rebind retires. Proof per
listener/join/aggregation/budget/repeat/race. Delta owner -1, NG -3, SWQ -3. F0.

## CM1 — ComponentManager singleton

Membership service/component_manager.go. Fence callback-borrow admission; wait admitted borrows outside locks; child
startDone selects running/cleanupPending; caller context; no rejoin. Proof blocked borrow, typed stopping, overlap,
partial failure, reverse aggregation/race. Delta owner -1, NG -2, Stop -1, SWQ -2. F0.

## ML1 — MessageLogger singleton

Membership service/message_logger.go plus adjacent HTTP/KV watch. Dynamic fence, retry cancel/done, exact core Drain
once, no retained result; KV Keys/Get request context; watcher separate. Proof dynamic/retry/callback/KV/repeat/race.
Delta owner/NG/Stop -1. F0.

## OS1 — ObjectStore singleton

Membership storage/objectstore/component.go. startDone/cleanup, exact JS/core before Store, no name Stop, terminal
Store, cleanupPending. Proof partial/block/order/later Stop/identity/race/integration. Delta owner/NG -1, Stop -2; old
lifecyclejoin rollback -1. F0+S1.

## OT1 — OTEL pull-loop singleton

Membership output/otel/component.go. Retain exact Consumer observation/acquisition; duplicate reject not replacement;
cancel/join fetch; flush/remove observers; context Shutdown; no Operation replay. Does not use ConsumeContext/core
Drain. Proof duplicate/block/cancel/exporter/cleanup/repeat/race. Delta owner/NG/Stop/Cancel/Operation -1. F0+I1
identity, not S1.

## RU1 — Rule package singleton

Owner processor/rule/processor.go, coherent package files from inventory. Exact handles; dynamic fence; remove all
retained contexts/unauthorized roots; classify bounded persistence exception; no lifecycle lookup/schedule roots. Proof
hot reload/watchers/evaluation/cron/persistence/ack/context census/integration/e2e. Delta owner/NG/Stop/Cancel -1.
F0+S1.

## GI1 — graph-ingest singleton

Owner component.go plus keyed_ingest/readiness/pool tests. Exact handles; separate backlog labels; remove stored
contexts/roots; fence delivery, Drain/Closed while submission live, cancel submission, stop pool, remaining
runtime/core cleanup; preserve effect→guard→ACK/poison. Proof callback
fence/pool/redelivery/guard/ACK/backlog/partial failure/deadline/race/integration/e2e. Delta
owner/NG/Stop/Cancel/Operation -1. F0+S1+observation separation.

## ConsumeDurable outward contract and replacement

The accepted zero-adopter premise is false. Current sister-repo production has ten concrete calls:

- SemDragon (1): `semdragon/questdag/component.go:294`.
- SemMachina (8): `internal/stage/loopfailure.go:337`, `internal/stage/runner.go:217`,
  `internal/knowledge/consumer.go:92`, `internal/ledger/writer.go:218`, `internal/accusation/consumer.go:75`,
  `internal/caseflow/consumer.go:65`, `internal/turn/intake.go:216`, `internal/egress/notifier.go:161`.
- SemSpec (1): `semspec/processor/execution-bridge/gated_dag_dispatch.go:36`.

SemMachina also has interface seams at `internal/stage/runner.go:48-57`, `internal/knowledge/consumer.go:28-32`,
`internal/ledger/writer.go:58-67`, `internal/accusation/consumer.go:28-32`,
`internal/caseflow/consumer.go:26-30`, `internal/turn/intake.go:57-69`, `internal/egress/notifier.go:82-91`.

Current ConsumeDurable owns: zero/nonpositive AckWait→30-second server effective default; positive heartbeat <=
effectiveAckWait/2 with overflow-safe comparison; ConsumeWithHeartbeat exclusive
InProgress/Ack/Term/transient-or-cancel Nak/heartbeat failure/work join.

Recommended final stateless exported adapter:
`NewDurableHandler(cfg StreamConsumerConfig, heartbeat time.Duration, work func(context.Context, []byte) error)
(func(context.Context, jetstream.Msg), error)`. It validates before acquisition, rejects nil work, delegates
settlement exclusively to ConsumeWithHeartbeat, and owns no Consumer/ConsumeContext/context/goroutine beyond sync
invocation/identity/catalog/Stop/deletion/replay.

Migration: build handler once and treat validation as config failure; call canonical handle-return
ConsumeStreamWithConfig with explicit PortConsumerContext/config/handler; retain exact ConsumeContext; Stop via
Drain/Closed while handler live, then cancel/join; never name Stop/Delete or Ack/Nak in work. SemSpec retry acquisition
only; SemDragon replace Background with Start context; SemMachina interfaces adopt handle-return and owners store/stop
handle. SemStreams migration doc; sisters read-only.

Adopter seam: know build handler + retain exact handle; do nothing compile-fails; discovery compiler+guide+release; no
arithmetic/settlement/name/catalog/rejoin knowledge.

`NewDurableHandler` plus removal is one explicit owner API ruling; batch approval does not approve it.

## N1 — final NATS/lifecycle retirement and breaking proof gate

Depends all owner waves. No owner membership. Canonical port helpers become exact-handle signatures; bridge callers
mechanically renamed; bridge deleted same commit; error-only APIs/Client lifecycle catalog/replacement/name
Stop/Delete removed; observation retained; Subscription once/error/rejoin stripped while one-shot
Drain/Closed/Unsubscribe preserved; unused Generation/Operation and the complete lifecyclejoin package deleted;
migration guidance covers 27 sister helper callers and external Subscription holders. Canonical callers compile-fail
and retain handle; Subscription semantic change needs explicit migration note. No service/config change.

N1 is the sole ConsumeDurable removal wave. The same breaking boundary adds owner-approved `NewDurableHandler`;
preserves effective AckWait/heartbeat/settlement proof; removes the old file, declaration, and tests only after adapter
equivalence; updates the ten-call/seven-interface migration map; and keeps local zeros truthful. No earlier wave may
delete, alias, or hollow ConsumeDurable.

Proof: table tests cover heartbeat relation, zero AckWait, nonpositive values, equality, and overflow; real NATS tests
cover ACK, Nak redelivery, Term, cancellation Nak, InProgress, heartbeat failure, and work join; adapter proof covers
stateless composition and the canonical exact handle; the guide names all sites. N1 rulings include canonical cutover,
Subscription semantic change, and `NewDurableHandler`/ConsumeDurable removal distinctly. Whole
census/integration/race/lint/schema/contracts plus task e2e:core, agentic, semantic run before breaking commit/tag.
Owner delta zero.

## Exact census movement

| Wave | Owners | NG | Stop | Cancel | SWQ | Operation | Old lifecyclejoin rollback calls |
|---|---:|---:|---:|---:|---:|---:|---:|
| F0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| I1 | -2 | -1 | -1 | 0 | 0 | -1 | -1 |
| S1 | -6 | -6 | -12 | 0 | 0 | 0 | -6 |
| R1 | -5 | -5 | -5 | 0 | 0 | 0 | -5 |
| G1 | -5 | -5 | -5 | 0 | 0 | 0 | 0 |
| A1 | -5 | -5 | -10 | 0 | 0 | 0 | -5 |
| O1 | -2 | -2 | -2 | 0 | 0 | 0 | 0 |
| H1 | -3 | -3 | 0 | 0 | -3 | 0 | -1 |
| M1 | -1 | -1 | -1 | -1 | 0 | 0 | -1 |
| SM1 | -1 | -3 | 0 | 0 | -3 | 0 | 0 |
| CM1 | -1 | -2 | -1 | 0 | -2 | 0 | 0 |
| ML1 | -1 | -1 | -1 | 0 | 0 | 0 | 0 |
| OS1 | -1 | -1 | -2 | 0 | 0 | 0 | -1 |
| OT1 | -1 | -1 | -1 | -1 | 0 | -1 | 0 |
| RU1 | -1 | -1 | -1 | -1 | 0 | 0 | 0 |
| GI1 | -1 | -1 | -1 | -1 | 0 | -1 | 0 |
| N1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| Total | -36 | -38 | -43 | -4 | -8 | -3 | -20 |

Only final `lifecyclecleanup.RollbackFailedStart` may remain. Every old
`lifecyclejoin.RunPartialStartRollback` call migrates with its owner; old calls and every production lifecyclejoin
import are zero at N1. Final lifecyclecleanup helpers are not failures.

## Dependency DAG, concurrency, and review reuse

There is no single global “next wave” after reviewed design lands. A wave is executable when every declared
prerequisite is complete and frozen membership/contract still matches reviewed global artifacts.

Dependencies: F0 none; I1 F0; S1 F0+I1; R1/G1/M1/SM1/CM1/ML1 F0; OT1 F0+I1; A1/O1/H1/OS1/RU1/GI1 F0+S1; N1
every owner wave + API/migration/proof prerequisites.

Failure blocks only that wave/descendants, not independent reviewed waves. Independent work may proceed concurrently
in isolated worktrees; overlapping natsclient files cannot.

The reviewed artifact SHA freezes inventory, membership, contracts, exceptions, API decisions, proof once. Unchanged
waves do not repeat inventory/design review; TDD implementation + independent implementation review. Re-review only
split membership, premise-changing census/source drift, new outward surface/contract, new
protocol/context/observation exception, or prerequisite API-shape change. Ancestor baseline movement already
represented by DAG is not drift.

## Shared TDD/review gate

Every owner wave first adds failing applicable tests: nil/no action; exact order; repeat nil/no replay; partial
rollback; rollback failure→cleanupPending→Start reject→later Stop; M overlap or serialization proof; blocked
callback closes before cancel; deadline non-clean; handle Closed/observation; no name lifecycle; no retained
context/root; race. Channels/listeners, no sleeps. Per-package race, focused integration for NATS/HTTP. Per-member
evidence required.

## Abort and split rules

1. Abort before implementation if exported change lacks explicit owner approval.
2. Split if admission/settlement differs, new exported/config/subject/port surface is needed, or another frozen owner
   lifecycle is touched.
3. Split on stored context, detached goroutine, name lifecycle, deletion knob, or result/rejoin state.
4. Split if exact native closure lacks deterministic proof or failed Start cannot retain every handle.
5. Census drift in 36 owner/27 external/25 core/17 helper pauses for refreshed inventory.
6. Race/integration failure, async bind, or callback after cancel blocks; no family-noise waiver.
7. Split preserves completed members but revised membership needs owner record before continuation.

## Why 17 waves is throughput-oriented

Seventeen review units do not mean seventeen serial inventory/design ceremonies. There is one global inventory review
and one global design review. Thereafter F0/I1/S1 form the shared serial spine, unchanged independent waves reuse that
review and can run concurrently, each wave gets one bounded implementation review, and N1 is the final convergence
review. Seven family batches still cover 28 owners; eight singleton units remain because their protocols are unique.

## Owner rulings required before handoff

1. Accept/reject final helper home `internal/lifecyclecleanup`, symbols `Wait` and `RollbackFailedStart`, and F0 as
   dependency root.
2. Approve I1 atomic non-port signature plus Registry.SubscribeCapabilities zero-consumer disposition; ConsumeDurable
   excluded.
3. Approve temporary no-release port bridge and N1 canonical cutover.
4. Approve same-signature one-shot Subscription semantics in N1.
5. Approve final `NewDurableHandler` and N1-only ConsumeDurable removal with ten-caller migration contract.
6. Approve context-bearing existing metric.Server Start/Stop signatures.
7. Confirm pprof remains out-of-scope process-lifetime exception.
8. Accept dependency-only concurrency and global inventory/design review reuse for unchanged waves.

Do not mark approved until materialized with baseline/hash, independent pre-owner design review, and owner acceptance.
