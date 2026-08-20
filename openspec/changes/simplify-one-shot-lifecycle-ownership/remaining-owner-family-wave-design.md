# Remaining lifecycle-owner family-wave design

> **Status:** Independent corrected-design review returned `DESIGN APPROVE`. The owner then stated
> “agree - continue with recommendation,” explicitly accepting rejection of Wait/zero-owner F0, the parent-aware
> helper born with R1, and R1 as the selected first wave. Independent R1 implementation review subsequently returned
> `APPROVE`; owner-migrated credit is limited to the five frozen research owners. This does not approve unrelated
> exported API rulings or complete any task or gate. Independent review of the narrow SM1 correction returned
> `DESIGN APPROVE`, and independent SM1 implementation review returned `APPROVE`; owner-migrated credit is limited to
> `service/service_manager.go`. Independent G1 implementation review returned `APPROVE`; owner-migrated credit is
> limited to its five frozen graph-read `component.go` owners. Independent CM1 implementation review returned
> `APPROVE`; owner-migrated credit is limited to `service/component_manager.go`.
> Independent I1 implementation review and both required breaking-change E2E tiers returned green; owner-migrated
> credit is limited to `agentic/agentrun/agentrun.go` and `service/milestone_service.go`.
> Independent OT1 implementation review returned `APPROVE`; owner-migrated credit is limited to
> `output/otel/component.go`.
> The owner explicitly approved the temporary no-release/no-tag S1 standard port-handle bridge. Independent S1
> implementation review returned `APPROVE`; owner-migrated credit is limited to the six frozen S1 owner files.
> S1's conditional approval permits the split-context bridge when its first real caller arrives. It is born with A1
> Loop under the same no-release/no-tag invariant. Independent A1 implementation review returned `APPROVE`;
> owner-migrated credit is limited to the five frozen A1 owner files.
> Independent H1 implementation review returned `APPROVE`; owner-migrated credit is limited to the graph gateway,
> input WebSocket, and output WebSocket owner files.
> Independent O1 implementation review returned `APPROVE`; owner-migrated credit is limited to the file and HTTP POST
> output owner files.
> Independent OS1 implementation review returned `APPROVE`; owner-migrated credit is limited to
> `storage/objectstore/component.go`.
> Independent GI1 implementation review returned `APPROVE`; owner-migrated credit is limited to
> `processor/graph-ingest/component.go`.
> The owner approved RU1 rulings R1-R5 only. Independent implementation review and a fresh isolated structural E2E
> returned green; owner-migrated credit is limited to `processor/rule/processor.go`.

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
   combine; G1 depends on R1’s final rollback-helper contract.
5. Recommended: frozen contract-equivalent family batches plus eight singleton protocol exceptions, one atomic
   non-port acquisition/bootstrap wave, and one final NATS retirement wave. Sixteen review units contain 15 owner
   waves: seven multi-owner batches cover 28 owners; only the eight genuinely unique owners remain singletons.

## Recommended rulings for owner adoption

D1. Each of the 36 owners appears in exactly one frozen owner wave below. Moving a member requires an explicit split
ruling; implementation does not silently reshuffle membership.

D2. There is no zero-owner helper wave and no shared Wait helper. Exact completion waits remain owner-local `select`
operations over exact done/Closed channels. The only final shared helper is
`internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`, born with the first real owner-family wave R1. It
synchronously invokes failed-Start cleanup under `context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)`.
Legacy `internal/lifecyclejoin.RunPartialStartRollback(rollback)` remains byte-for-byte unchanged for unmigrated owners
because its signature cannot receive the parent; it does not forward to the final helper. Every migrated owner calls
the final parent-aware helper. N1 deletes the legacy package and old symbol after their call/import censuses reach
zero.

Exact API: `func RollbackFailedStart(parent context.Context, rollback func(context.Context) error) error`.

Contract: reject nil parent before callback; valid parent+nil rollback nil; only
`WithTimeout(WithoutCancel(parent), 5s)`; preserve values not cancellation/deadline; synchronous caller goroutine; no
goroutine/state/cache/rejoin/detached work; rollback honors bound/joins; callback+expiry errors joined. No Wait,
compatibility forwarder, or lifecyclecleanup package exists before R1.

Adopter seam: the helper is internal, so it creates no adopter bill; a component author knows nothing new and sees no
outside effect. Discovery is in-repo compile failure only. The five-second budget is fixed, framework-owned, and
observes completion; it exposes no prediction knob. External NATS and ConsumeDurable surfaces remain unchanged.

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

D7. Only final parent-aware `lifecyclecleanup.RollbackFailedStart` may remain. Every old
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

Exceptions: only agentrun treats a missing optional `AGENT` stream as a no-op. MaxDeliver observation requires its
provisioned capture stream and fails loud when it is absent, returning a nil stop closure and an error satisfying
`errors.Is(err, jetstream.ErrStreamNotFound)`. Second agentrun consumer failure cleans first or retains it in
cleanupPending. Maxdelivery policy observation remains separate.

Proof: NATS integration for two-handle partial failure, exact Drain/Closed, duplicate fixed durable rejection without
incumbent replacement, maxdelivery Stop, no deletion, failed rollback then later Milestone Stop, repeated completed
Stop nil; race packages `./agentic/agentrun ./service ./internal/maxdelivery ./natsclient ./component` plus relevant
integration tests.

Delta: owners -2, NG -1, Stop -1, Operation -1; old lifecyclejoin rollback -1. I1 depends on R1.

Owner ruling on 2026-08-19: APPROVE only the I1 breaking surface described above. The canonical internal-consumption
method returns its exact native handle, rejects duplicate live durable ownership, and
`Registry.SubscribeCapabilities` is removed because it has no present repository or known sister-repository
consumer. This approval does not include `ConsumeDurable`, either port consumption method, `natsclient.Subscription`,
Metrics APIs, or any later N1 retirement.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE`, `task e2e:agentic` exit 0,
and `task e2e:core` exit 0 grant owner-migrated credit only to `agentic/agentrun/agentrun.go` and
`service/milestone_service.go`. Supporting natsclient, component, and MaxDeliver files receive no owner credit. I1 is
committed at `07c37f7319a65c5109fe31bc36136661bc6e9243`. Task 2.3, Gate A/B/C, runtime migration, proof, release,
archive, and tag readiness remain incomplete.

## S1 — serialized fixed-port Q/F batch + port bridge birth

Frozen membership (6): document, IoT, weather, json_filter, json_generic, json_map owner files.

S1 introduces only temporary migration-only `ConsumeStreamWithConfigHandle`, returning exact `ConsumeContext`.
Fallible observation and context checks precede native Consume; duplicate identity rejects; the exact handle returns
without Client cataloging or another fallible post-commit branch. Its claim and metrics release only after exact
Closed. The split-context bridge is deferred to A1, its first caller. Born with five JS members; Weather core-only.
Canonical error-only and split-context methods remain for unmigrated callers until later reviewed waves. No release or
tag exposes the temporary bridge as a supported public contract.

Shared contract: serialized Start/Stop; fixed subscription set; exact handle per acquired port; partial acquisition
retains/cleans handles; core Drain once; JS Drain/Closed; cancel/join; terminal cleanup. Exceptions: Document/IoT
twins; JSON variants; Weather core-only.

Proof: shared lifecycle matrix in all six plus package tests; blocked callback Drain before cancel; second-sub failure
exact cleanupPending; duplicate integration; race serialization. Delta owners -6, NG -6, Stop -12; old lifecyclejoin
rollback -6. S1 depends on I1.

Owner ruling on 2026-08-20: APPROVE only this temporary standard port-handle bridge and its branch-local
no-release/no-tag use by the five S1 JetStream owners. The canonical method, split-context bridge or method,
`ConsumeDurable`, `natsclient.Subscription`, Metrics APIs, and N1 retirements remain excluded.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to the frozen document, IoT, Weather, JSON filter, JSON generic, and JSON map owner files. Supporting natsclient
and test files receive no owner credit. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag
readiness remain incomplete.

## R1 — Research-five owner family and final rollback-helper birth (selected first wave)

Frozen owners: assess/classify/execute/route/synthesize component.go files. Supporting scope: new
internal/lifecyclecleanup containing only parent-aware RollbackFailedStart and tests; no lifecyclejoin changes.

Why first: five real consumers at package birth; one M+Q/F skeleton; five of twenty old rollback calls; no exported
API/port bridge.

Shared contract: Start retains exact received parent only for deferred call (not state), derives run authority,
publishes cleanup/startDone before acquisition escapes, drains exact core subs once while callback live, cancels/joins,
closes LLM. Failed Start calls final helper with exact Start ctx/ownerCleanup. Success clears; failure/expiry retains
handles cleanupPending, rejects Start, later Stop caller context. Execute no LLM; Classify optional.

TDD: helper nil parent/no callback, nil rollback, canceled/expired parent yields live bounded context with value,
unexported test budget seam avoids 5s sleep/no production knob, callback+deadline aggregate, sync/no state; every owner
tests Stop vs blocked Start/startDone, partial sub/LLM failure exact rollback, expiry cleanupPending/reject/later Stop,
blocked callback drain before cancel, LLM order, repeat nil, restart reject, race. Gates final helper+five package race
and focused NATS callback integration. Delta owners -5, NG -5, Stop -5, old rollback -5, final helper calls +5; no
exported surface.

Implementation status on 2026-08-19: independent `semstreams-reviewer` verdict `APPROVE`. Owner-migrated credit is
limited to the frozen assess, classify, execute, route, and synthesize owner files. The final parent-aware helper was
born with those five real consumers and receives no separate owner credit. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain incomplete.

## G1 — graph-read serialized Q/F family

Membership (5): graph-query, graph-clustering, graph-embedding, spatial, temporal plus adjacent query files. Serialized
exact incremental subs; rollback; Drain once before cancel; terminal child close. Exceptions as inventoried. Common
serialized proof. All five have partial acquisition failures requiring bounded cancellation-independent rollback; old
rollback delta remains zero; final helper call count is determined by implementation. G1 depends on R1. Do not combine
R1/G1. Delta -5 owners/NG/Stop.

Implementation status on 2026-08-19: independent `semstreams-reviewer` verdict `APPROVE`. Owner-migrated credit is
limited to the frozen graph-query, graph-clustering, graph-embedding, graph-index-spatial, and graph-index-temporal
`component.go` owner files. Adjacent query and test files are supporting evidence and receive no separate owner credit.
Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## A1 — agentic M+Q/F family

Membership (5): dispatch, governance, loop, model, tools owner `component.go` files. Supporting adjacent files and
tests receive no owner credit. Existing startDone; exact standard handles; Loop is the first real caller of temporary
`ConsumeStreamWithConfigContextsHandle`, which preserves exact setup and handler contexts and retains claim/metrics
until exact Closed. No name-routed lifecycle or deletion. Dispatch moves its GraphView from an invented root onto the
Start-derived controller lifetime. Loop separates read-only consumer observation from lifecycle authority. Owner
exceptions preserve sweeper, Model-client, and Tools tool-list shutdown ordering. Common M proof plus exceptions,
integration, and agentic E2E. Delta owners -5, NG -5, Stop -10; old lifecyclejoin rollback -5. A1 depends on S1.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to the five frozen dispatch, governance, loop, model, and tools owner `component.go` files. Supporting natsclient,
`http_activity`, `inflight`, and test files receive no owner credit. The split bridge remains temporary and branch-only;
no release or tag may expose it as a supported contract. Task 2.3, Gate A/B/C, runtime migration, proof, release,
archive, and tag readiness remain incomplete.

## O1 — static output sinks

Membership: file + httppost. Serialized exact JS/core handles; partial cleanup; callback drain before sink close. File
flush/close; HTTPPost ACME moved from constructor to Start, joined before exact-once idle-connection close.
Component instances are one-shot; fresh construction is the reuse boundary. Lifecycle transitions are serialized and
Stop is caller-bounded; there is no concurrent-Stop coordination contract. Public signatures and configuration remain
unchanged. Deterministic proof. Delta owners/NG/Stop -2. O1 depends on S1.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to `output/file/file.go` and `output/httppost/httppost.go`. Tests and package documentation are supporting
evidence/adopter surfaces and receive no owner credit. M1, OS1, RU1, GI1, N1, and unrelated APIs remain excluded. The
temporary port bridges keep the branch ineligible for release or tag. Task 2.3, Gate A/B/C, runtime migration, proof,
release, archive, and tag readiness remain incomplete.

## H1 — standalone HTTP component family

Membership: graph gateway, input websocket, output websocket. Synchronous bind; BaseContext exact Start; fence
upgrades/requests; Shutdown while context live; await serveDone/callbacks; then cancel/join. No generic provider.
Unique readiness/connections/output NATS/root exceptions; pprof out. HTTP/NATS proof. Delta owners/NG -3, SWQ -3;
old lifecyclejoin rollback -1. H1 depends on S1.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to `gateway/graph-gateway/component.go`, `input/websocket/websocket_input.go`, and
`output/websocket/websocket.go`. Readiness and test files are supporting evidence and receive no owner credit. M1,
ServiceManager HTTP, and pprof remain excluded. The branch remains ineligible for release or tag while temporary port
bridges exist. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## M1 — Metrics + metric.Server singleton

Membership service/metrics.go with metric/handler.go. Exact listener/server/serveDone; bind before BaseService commit;
BaseContext exact Start; caller-bounded Shutdown outside locks; rollback reachable; repeat nil. Existing metric.Server
signatures become context-bearing, no second surface; owner-gated. Delta owner/NG/Stop/Cancel -1; old lifecyclejoin
rollback -1. M1 depends on R1.

## SM1 — ServiceManager multi-HTTP singleton

Membership: service/service_manager.go. Three server generations owner-local; sync bind/BaseContext/Shutdown/serveDone;
health publisher cancel/done; StopAll reverse aggregation unchanged; same-instance rebind retires.

Contract correction: SM1 depends on R1 because every migrated owner with a post-acquisition Start failure uses
`lifecyclecleanup.RollbackFailedStart(parent, rollback)`. `Manager.StartAll` must locally attempt bounded synchronous
rollback before returning a child Start, main listener bind, or publisher failure. Process-root `StopAll` is
defense-in-depth, not a substitute for Manager-owned failed-Start cleanup.

Proof per listener/join/aggregation/budget/repeat/race, plus local bounded rollback for each post-acquisition failure.
Delta owner -1, NG -3, SWQ -3; old lifecyclejoin rollback unchanged; final helper calls 5→6. SM1 depends on R1. No
membership or exported surface changes.

Implementation status on 2026-08-19: independent narrow corrected-design verdict `DESIGN APPROVE`, followed by
independent implementation verdict `APPROVE` after all corrections. Owner-migrated credit is limited to
`service/service_manager.go`; adjacent tests and the process-root comment receive no owner credit. Task 2.3, Gate
A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## CM1 — ComponentManager singleton

Membership service/component_manager.go. Fence callback-borrow admission; wait admitted borrows outside locks; child
startDone selects running/cleanupPending; caller context; no rejoin. Proof blocked borrow, typed stopping, overlap,
partial failure, reverse aggregation/race. Delta owner -1, NG -2, Stop -1, SWQ -2. CM1 depends on R1.

Implementation status on 2026-08-19: independent `semstreams-reviewer` verdict `APPROVE`. Owner-migrated credit is
limited to `service/component_manager.go`; supporting test files receive no separate owner credit. Task 2.3, Gate
A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## ML1 — MessageLogger singleton

Membership service/message_logger.go plus adjacent HTTP/KV watch. Dynamic fence, retry cancel/done, exact core Drain
once, no retained result; KV Keys/Get request context; watcher separate. Proof dynamic/retry/callback/KV/repeat/race.
Delta owner/NG/Stop -1. ML1 is an independent root.

Implementation status on 2026-08-19: independent `semstreams-reviewer` verdict `APPROVE`. Owner-migrated credit is
limited to `service/message_logger.go`; adjacent `service/message_logger_http.go` and test files are supporting
implementation/evidence surfaces and receive no separate owner credit. The request-owned SSE watcher remains
unchanged. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## OS1 — ObjectStore singleton

Membership storage/objectstore/component.go. startDone/cleanup, exact JS/core before Store, no name Stop, terminal
Store, cleanupPending. Proof partial/block/order/later Stop/identity/race/integration. Delta owner/NG -1, Stop -2; old
lifecyclejoin rollback -1. OS1 depends on S1.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to `storage/objectstore/component.go`. Tests and the narrowly corrected package concurrency documentation are
supporting surfaces and receive no owner credit. Durable topology, StoreProvider availability during drain, and
effect-before-ACK settlement remain unchanged. RU1, GI1, M1, N1, and unrelated API/configuration surfaces remain
excluded. Temporary bridges keep the branch ineligible for release or tag. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain incomplete.

## OT1 — OTEL pull-loop singleton

Membership `output/otel/component.go`. Retain exact Consumer observation/acquisition; a process-global opaque
`(stream, durable)` claim rejects duplicate local ownership without replacing the incumbent. Fence new fetch, cancel
and join pull loops, flush the exporter, remove observers, run context-bound exporter Shutdown, then release the exact
claims; completed repeated Stop is nil with no Operation replay. Does not use ConsumeContext/core Drain. Proof
duplicate/block/cancel/exporter/cleanup/repeat/race. Delta owner/NG/Stop/Cancel/Operation -1. OT1 depends on I1
identity, not S1.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to `output/otel/component.go`. `output/otel/component_test.go` and
`output/otel/component_lifecycle_integration_test.go` are supporting evidence and receive no owner credit. Task 2.3,
Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## RU1 — Rule package singleton

Owner processor/rule/processor.go, coherent package files from inventory. Exact handles; dynamic fence; remove all
retained contexts/unauthorized roots; classify bounded persistence exception; no lifecycle lookup/schedule roots. Proof
hot reload/watchers/evaluation/cron/persistence/ack/context census/integration/e2e. Delta owner/NG/Stop/Cancel -1. RU1
depends on S1.

Owner approval on 2026-08-20 is limited to the coherent R1-R5 source contract: context-first Rule APIs, immediate
context-bearing KV initialization, an internal cron dispatcher, nil-context rejection by `Matches`, and shutdown
barriers ordered before snapshots or native teardown. Independent implementation review returned `APPROVE`, and the
reviewer ran a fresh isolated structural E2E from the final production identity with 38/38 passing. Owner-migrated
credit is limited to `processor/rule/processor.go`. Supporting Rule package files, composition callers, package docs,
migration guidance, and tests receive no owner credit. Temporary bridges keep the branch ineligible for release or
tag. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain incomplete.

## GI1 — graph-ingest singleton

Owner component.go plus keyed_ingest/readiness/pool tests. Exact handles; separate backlog labels; remove stored
contexts/roots; fence delivery, Drain/Closed while submission live, cancel submission, stop pool, remaining
runtime/core cleanup; preserve effect→guard→ACK/poison. Proof callback
fence/pool/redelivery/guard/ACK/backlog/partial failure/deadline/race/integration/e2e. Delta
owner/NG/Stop/Cancel/Operation -1. GI1 depends on S1 and observation separation.

Implementation status on 2026-08-20: independent `semstreams-reviewer` verdict `APPROVE` grants owner-migrated credit
only to `processor/graph-ingest/component.go`. `keyed_ingest.go`, `readiness.go`, and tests are supporting surfaces and
receive no owner credit. Stored production contexts and unauthorized roots are removed while settlement, readiness,
subjects, configuration, and schema remain unchanged. RU1, M1, N1, and unrelated outward surfaces remain excluded.
Temporary bridges keep the branch ineligible for release or tag. Task 2.3, Gate A/B/C, runtime migration, proof,
release, archive, and tag readiness remain incomplete.

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

Only final parent-aware `lifecyclecleanup.RollbackFailedStart` may remain. Every old
`lifecyclejoin.RunPartialStartRollback` call migrates with its owner; old calls and every production lifecyclejoin
import are zero at N1. The final lifecyclecleanup helper is not a failure.

Final-helper call progression is 0→5 in R1 and 5→6 in SM1. SM1 removes no old rollback call; its owner, NG, and SWQ
deltas remain exactly those in the table.

## Dependency DAG, concurrency, and review reuse

There is no single global “next wave” after reviewed design lands. A wave is executable when every declared
prerequisite is complete and frozen membership/contract still matches reviewed global artifacts.

Dependencies: R1/ML1 none; I1/G1/M1/SM1/CM1 R1; S1 I1; OT1 I1; A1/O1/H1/OS1/RU1/GI1 S1; N1 every owner wave +
API/migration/proof prerequisites. R1 is selected first; ML1 may run independently after corrected global design
acceptance. The shared serial spine is R1 → I1 → S1 → N1, and N1 also waits for all owner waves.

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

## Why 16 waves is throughput-oriented

Sixteen review units do not mean sixteen serial inventory/design ceremonies: one global inventory and corrected
global design are reused. Seven family batches cover 28 owners; eight inventory-proven unique owners remain singleton
implementation reviews; N1 is convergence. R1/ML1 can begin as independent roots; R1 then unlocks SM1 and I1, and the
I1/S1 spine unlocks the port families. Expected no-split range: approximately 31–49 implementation-review cycles,
7–10 single-lane working weeks, or 5–8 elapsed weeks with two isolated implementation lanes and one reviewer.

## Owner rulings required before handoff

1. Confirm final helper home `internal/lifecyclecleanup`, sole symbol
   `RollbackFailedStart(parent, rollback)`, and R1 as the selected first helper-birth family.
2. Approve I1 atomic non-port signature plus Registry.SubscribeCapabilities zero-consumer disposition; ConsumeDurable
   excluded.
3. Approve temporary no-release port bridge and N1 canonical cutover.
4. Approve same-signature one-shot Subscription semantics in N1.
5. Approve final `NewDurableHandler` and N1-only ConsumeDurable removal with ten-caller migration contract.
6. Approve context-bearing existing metric.Server Start/Stop signatures.
7. Confirm pprof remains out-of-scope process-lifetime exception.
8. Accept dependency-only concurrency and global inventory/design review reuse for unchanged waves.

Independent corrected-design review returned `DESIGN APPROVE`. The owner’s “agree - continue with recommendation”
accepts only rejection of Wait/zero-owner F0, the parent-aware `RollbackFailedStart` born with R1, and R1 as the
selected first wave. The unrelated exported API rulings above remain unapproved. Independent R1 implementation review
later returned `APPROVE`, granting owner-migrated credit only to the five frozen research owner files; no broader task
or gate credit follows. Independent narrow SM1 design re-review returned `DESIGN APPROVE`, and independent SM1
implementation review returned `APPROVE`, granting owner-migrated credit only to `service/service_manager.go`. The
unrelated exported API rulings remain unapproved, and no broader task or gate credit follows. Independent G1
implementation review returned `APPROVE`, granting owner-migrated credit only to its five frozen graph-read
`component.go` owners; query/test support receives no owner credit and no broader task or gate credit follows.
Independent CM1 implementation review returned `APPROVE`, granting owner-migrated credit only to
`service/component_manager.go`; supporting tests receive no owner credit and no broader task or gate credit follows.
Independent ML1 implementation review returned `APPROVE`, granting owner-migrated credit only to
`service/message_logger.go`; adjacent HTTP and test support receives no owner credit and no broader task or gate credit
follows. Independent I1 implementation review returned `APPROVE`, and both required breaking-change E2E tiers passed,
granting owner-migrated credit only to `agentic/agentrun/agentrun.go` and `service/milestone_service.go`; supporting
natsclient, component, and MaxDeliver files receive no owner credit. Other exported API rulings remain unapproved, and
no broader task or gate credit follows. Independent OT1 implementation review returned `APPROVE`, granting
owner-migrated credit only to `output/otel/component.go`; supporting tests receive no owner credit and no broader task
or gate credit follows. The owner explicitly approved only the temporary standard S1 port-handle bridge under the
branch no-release/no-tag invariant. Independent S1 implementation review returned `APPROVE`, granting owner-migrated
credit only to its six frozen owner files; natsclient and test support receive no owner credit, and no broader task or
gate credit follows. S1's conditional split-bridge approval is exercised by A1 Loop, its first real caller, under the
same no-release/no-tag invariant. Independent A1 implementation review returned `APPROVE`, granting owner-migrated
credit only to its five frozen owner files; natsclient, adjacent implementation, and test support receive no owner
credit, and no broader task or gate credit follows. Independent H1 implementation review returned `APPROVE`, granting
owner-migrated credit only to its three frozen standalone HTTP owner files; readiness and test support receive no owner
credit, and no broader task or gate credit follows. Independent O1 implementation review returned `APPROVE`, granting
owner-migrated credit only to its two frozen output owner files; tests and package documentation receive no owner
credit, and no broader task or gate credit follows. Independent OS1 implementation review returned `APPROVE`,
granting owner-migrated credit only to `storage/objectstore/component.go`; tests and package documentation receive no
owner credit, and no broader task or gate credit follows. Independent GI1 implementation review returned `APPROVE`,
granting owner-migrated credit only to `processor/graph-ingest/component.go`; adjacent implementation and test support
receive no owner credit, and no broader task or gate credit follows. RU1 owner approval is limited to R1-R5;
independent implementation review and the fresh isolated 38/38 structural E2E grant owner-migrated credit only to
`processor/rule/processor.go`, while supporting surfaces receive none and no broader task or gate credit follows.
