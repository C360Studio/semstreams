# Remaining owner archetype inventory

> **Status:** Complete inventory-only draft. Baseline
> `269e0ac94b28c6f6162d8f5d144ca545b393df85`; read-only; **NOT INVENTORY PASS**; no target recommendation.

## Problem statement / reconciliation

- Repository-first `rg -l 'internal/lifecyclejoin' --glob '*.go'` reconciles exactly 36 production owner files. Current
  census: 38 NewGeneration, 43 Generation.Stop, 4 external Generation.Cancel, 0 Signal, 8 StopWithQuiesce, 3
  NewOperation/3 Operation.Run, 20 RunPartialStartRollback. Family contributions below sum exactly to every census.
- HEAD is the clean durable BaseService commit named above. Shared worktree has one unrelated untracked
  `openspec/changes/simplify-one-shot-lifecycle-ownership/metrics-http-owner-inventory.md`; no inventory agent edited
  it.
- Constraints already claimed by current target: ADR-095 exact native order and separate failed-Start authority
  (`docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:24-55`); exact
  order/failed Start
  (`openspec/changes/simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md:3-19`); exact
  ConsumeContext/identity (`.../specs/jetstream-consumer-policy/spec.md:3-48,74-85`); service one-shot
  (`.../specs/service-shutdown/spec.md:44-69`); tasks 2.1-2.3 (`tasks.md:17-21`); ledger gates
  (`recovery-ledger.md:701-732,790-824`, recheck after materialization).

## Mandatory surface inventory — current shared spellings/collisions

- Stateful generic ownership: `internal/lifecyclejoin/generation.go:96-153` cancels before owner cleanup and retains
  result/rejoin semantics; `Operation` supplies the same rejected result-sharing shape. Every owner below delegates at
  least one terminal fact to it.
- JetStream callback ownership: exported helpers are still error-only at `natsclient/stream.go:275-317`; same-name
  incumbent replacement occurs at `:359-372`; native `jetstream.ConsumeContext` commits at `:391-403` and is hidden in
  Client catalog at `:416-425`; name-routed stop is `:689-723`. Seventeen owners use these helpers and therefore cannot
  retain exact native callback authority.
- Core NATS callback ownership: exact native subscription is wrapped at `natsclient/client.go:864-885`; wrapper
  `Drain(ctx)` at `:896-940` retains once/error/completion and explicitly promises later rejoin. Twenty-five of the 36
  remaining lifecyclejoin owners retain that wrapper, directly or behind MessageLogger’s local interface: document,
  IoT, weather, file, HTTPPost, output websocket, agentic-loop, agentic-tools, graph-ingest, the five graph-read owners,
  the three JSON owners, the five research owners, Rule, MessageLogger, and ObjectStore. The three owners omitted by
  the prior count are `processor/agentic-loop/component.go:72-73`, `processor/agentic-tools/component.go:71`, and
  `processor/graph-ingest/component.go:618`. MessageLogger stores the return behind `messageLoggerSubscription` at
  `service/message_logger.go:166-171,188-198,456-466`.

  The collision is broader than the remaining-owner census. Four already-migrated/root/E2E holders also retain the
  exported wrapper: migrated `processor/graph-index/component.go:361,789`; process-root
  `cmd/e2e-semstreams/main.go:267`; mission root `cmd/e2e-semstreams/mission/command.go:154,257`; and research E2E
  `test/e2e/scenarios/research-graph/scenario.go:84`. Thus the repository surface has 29 holder files: 25 remaining
  owners plus those four. The helper cannot be redesigned as though only the 36-owner ledger consumes it.
- HTTP process-lifetime collision inventory has six distinct surfaces. Five are lifecyclejoin owner files: graph
  gateway (`gateway/graph-gateway/component.go:699-724`), input websocket
  (`input/websocket/websocket_input.go:625-681`), output websocket (`output/websocket/websocket.go:675-735`), Metrics
  through `metric.Server` (`service/metrics.go:111-167`; `metric/handler.go:19-28,100-166`), and ServiceManager
  (`service/service_manager.go:740-1005`). The sixth is the explicit process-lifetime pprof exception:
  `service/pprof.go:8-38` fire-and-forgets `http.ListenAndServe` with the default request root and no Stop/join; both
  composition roots invoke it before NATS at `cmd/semstreams/main.go:12,90-93` and
  `cmd/e2e-semstreams/main.go:12,88-91`. Metric.Server and input websocket omit BaseContext; pprof intentionally uses
  the default root; the other three lifecycle owners capture Start context. These six are collision evidence, not one
  equivalent family.
- Observation/lifecycle collisions: agentic-loop `consumerInfo` is both name-routed lifecycle and OutstandingWork
  source (`processor/agentic-loop/component.go:167-175,612-657`; `inflight.go:135-167`); graph-ingest `boundConsumers`
  is backlog observation while actual ConsumeContext remains hidden (`processor/graph-ingest/component.go:512-517,
  1334-1451`; `readiness.go:21-42,131,175`). Observation must remain separate from lifecycle authority.
- No new communication, payload, orchestration, or query surface is introduced by this inventory; canonical decision
  skills do not trigger.

## Per-owner file:line inventory and reclassified archetype (36/36)

1. `processor/agentic-dispatch/component.go`: Generation/startDone and name labels `:92-137`; publishes
   authority/rollback `:307-347`; five error-only consumers `:445-584`; name Stop/Delete `:406-435`; exact GraphView
   child `:107-118`, stopped in `http_activity.go:174-190`. M-primary + Q/F; unique GraphView child; context/root
   facet: GraphView Background `http_activity.go:166`.
2. `processor/agentic-governance/component.go`: Generation + config labels `:46-53`; startDone/rollback `:208-245`;
   error-only consume `:405-464`; name cleanup `:537-553`. M-primary + Q/F; simplest agentic member.
3. `processor/agentic-loop/component.go`: Generation, exact core subs, sweeper cancel/done, name observer records
   `:49-80,167-175`; startDone/rollback `:431-475`; exact core acquisition `:488-506`; hidden JS handles `:805-931`;
   core Drain/name Stop/Delete `:612-657`. M-primary + Q/F; unique sweeper and observation collision.
4. `processor/agentic-model/component.go`: Generation, name records, cached clients `:46-68,94-98`;
   startDone/rollback `:251-300`; hidden handle `:380-417`; name cleanup then client close `:477-540`. M-primary + Q/F;
   unique model-client close.
5. `processor/agentic-tools/component.go`: Generation, exact toolListSub, name records `:53-81`; startDone/rollback
   `:159-208`; core sub `:222-229`; hidden JS handle `:353-400`; core Drain/name cleanup `:494-535`. M-primary + Q/F.
6. `processor/graph-query/component.go`: WG/Generation/exact query subs `:171-194`; whole Start serialized `:453-520`;
   incremental acquisition `query.go:72-89`; cancellation-first plain Unsubscribe + child close `component.go:529-568`;
   restart overwrites retained Generation. Q-primary + F, historical S false.
7. `processor/graph-clustering/component.go`: query subs/readiness watchers/workers/clients/Generation `:590-656`;
   serialized partial-acquisition Start `:918-1055`; incremental core subs `query.go:17-45`; cancellation-first child
   teardown `component.go:1068-1123`; watcher partial acquisition `:1344-1373`. Q-primary + F; protocol-child
   exception.
8. `processor/graph-embedding/component.go`: query subs/worker/embedder/coalescer/Generation `:240-322`; serialized
   partial Start `:613-706`; incremental subs `query.go:17-39`; cancellation-first teardown `component.go:721-767`;
   goroutine-local watcher closes `:1293-1329`. Q-primary + F; worker/coalescer exception.
9. `processor/graph-index-spatial/component.go`: Generation/exact query subs `:180-199`; serialized Start with late
   bucket failure `:445-503`; incremental subs `query.go:20-34`; cancellation-first Unsubscribe `component.go:520-549`;
   local watcher `:557-596`. Q-primary + F.
10. `processor/graph-index-temporal/component.go`: Generation/exact query subs `:189-208`; serialized Start with late
    bucket failure `:454-524`; acquisition `query.go:15-27`; cancellation-first Unsubscribe `component.go:540-569`;
    local watcher `:577-612`. Q-primary + F.
11. `processor/graph-ingest/component.go`: Generation/Operation `:500-507`; observation labels `:512-517`; exact core
    subs `:617-618`; prohibited retained contexts `:595-606` and invented roots in `keyed_ingest.go:71-90`;
    serialized Start publishes authority only after partial acquisition `component.go:858-970`; hidden JS handles
    `:1334-1451`; pre-cancel then Operation pool stop, later core cleanup `:984-1050`. P-primary + Q/F + context/root
    debt; unique pool/backlog protocol.
**12–13.** `examples/processors/document/component.go` and `examples/processors/iot_sensor/component.go`: same
fields/native core subs/name consumer bindings `:83-103`; serialized published Generation + rollback `:196-236`;
mixed acquisition `:238-277`; Stop `:301-345`; hidden JS handle `:433-439`. Genuine clone family, serialized Q/F.
Their complete Start and Stop transitions share `lifecycleMu`; current Start/Stop method bodies cannot overlap, so M
is false even though failed-Start rollback is required.
14. `examples/processors/weather_station/component.go`: Generation/exact core subs `:55-73`; serialized Start/rollback
    `:160-200`; Stop `:216-252`. Q/F core-only; separate from mixed clones.
15. `processor/json_filter/json_filter.go`: lifecycle state `:70-105`; Start `:198-255`; hidden JS at `:349`; Stop
    `:416-460`.
16. `processor/json_generic/json_generic.go`: lifecycle state `:76-77`; Start `:176-214`; hidden JS `:326`; Stop
    `:373-414`.
17. `processor/json_map/json_map.go`: lifecycle state `:95-96`; Start `:216-255`; hidden JS `:371`; Stop `:418-459`.
    These three are a genuine mixed core/JS serialized Q/F transform family. Complete Start and Stop transitions share
    the lifecycle lock; M is false. They retain failed-Start rollback obligations and have no measured
    context-retention debt in these owner files.
18. `processor/research-graph-assess/component.go`: exact core subs + LLM + Generation `:50-64`; Start publishes then
    unlocks before acquisition/rollback `:139-172`; subscription `:243-262`; Drain/LLM close `:267-307`.
19. `processor/research-graph-classify/component.go`: state `:72-75`; Start `:202-224`; sub `:314-329`; Stop
    `:340-376`.
20. `processor/research-graph-execute/component.go`: state `:55-57`; Start `:178-199`; subscription around `:246-260`;
    Stop `:269-300`; no LLM client child.
21. `processor/research-graph-route/component.go`: state `:61-64`; Start `:145-167`; sub `:263-278`; Stop `:289-325`.
22. `processor/research-graph-synthesize/component.go`: state `:51-53`; Start `:123-145`; sub `:222-236`; Stop
    `:245-281`. Genuine shared Start/rollback/core-sub skeleton, M+Q/F; execute is the no-LLM-close exception. Start
    unlocks before acquisition, so explicit startDone/admission is required.
23. `processor/rule/processor.go`: Generation, exact core subs, hidden JS consumer, KV KeyWatchers, dynamic watcher
    cancels and prohibited retained watcherCtx `:97-164`; Start readiness/failure `:918-983`; mixed setup
    `:1033-1144`; Stop fences dynamic admission and tears down watchers/queue/subs/cache `:1181-1269`. Q/M/F + context
    debt; unique dynamic-admission protocol; Rule package-wide context/root debt.
24. `gateway/graph-gateway/component.go`: HTTP server/readiness/WG/Generation `:279-289`; correct BaseContext but async
    bind `:699-724`; StopWithQuiesce/Shutdown/WG/readiness `:740-777`; no failed-Start rollback, restart overwrites
    generation. HTTP Q; unique readiness listener.
25. `input/websocket/websocket_input.go`: HTTP server/connections/TLS/WG/Generation plus redundant once/channels
    `:50-78`; starts processMessages before fallible server setup `:535-554`; StopWithQuiesce `:562-591,686-717`; no
    BaseContext and async bind `:625-681`. HTTP Q/F + root gap; unique input/client connection protocol.
26. `output/file/file.go`: exact core subs/file/WG/Generation `:106-119`; partial Start + restart support `:236-267`;
    hidden JS handle `:367-379`; Unsubscribe/join/flush/close `:411-454`. Q/F, historical S false.
27. `output/httppost/httppost.go`: exact core subs/TLS cleanup `:90-113`; constructor ACME Background root `:186-195`;
    partial Start `:261-268`; hidden JS `:349-372`; Unsubscribe `:409-439`. Q/F + root debt, historical S false. Same
    static-output skeleton as file, but root/client exception.
28. `output/otel/component.go`: exact `jetstream.Consumer` objects/observer cleanup/exporter/WG plus
    Generation/Operation `:30-76`; directly derives durable and calls CreateOrUpdateConsumer `:263-280`; staged
    rollback and pull-loop start `:232-350`; Cancel/join/flush/observer/exporter shutdown via Operation `:495-530`.
    P-primary; no task2.1 dependency, but task2.2 identity collision applies.
29. `output/websocket/websocket.go`: HTTP server/core subs/clients/TLS/WG/Generation `:130-156`; publishes start
    barrier/rollback `:590-624`; BaseContext/async bind `:675-735`; hidden JS `:888-943`; StopWithQuiesce `:758-815`;
    failed cleanup/rejoin state `:829-843`; continuing send/ACK roots `:1395,1480`. HTTP Q/F + root debt; unique
    client-broadcast protocol.
30. `service/component_manager.go`: manager Generation plus per-child Generation/startDone/Start result/mode
    `:83-124`; publishes child authority before concurrent Start `:492-580`; `withComponents` has no borrow admission
    fence/join `:635-650`; failed/graceful child selection `:697-742`. M-primary + Q/F; unique manager/borrow protocol.
31. `service/message_logger.go`: exact dynamic subscription map, cancels/retryDone, Generation, once/retained result
    `:166-172,188-198,227-238`; retry admission `:578-616`; Stop fence/cancel/join/Unsubscribe `:627-709`; tests retain
    rejected replay/rejoin `message_logger_registry_test.go:80-129`. Dynamic Q; no F because subscription failure
    degrades into retry. MessageLogger HTTP KV query context-discard debt `message_logger_http.go:463-494,524`.
32. `service/metrics.go`: abstract metricsServer + Generation/once/retained error `:18-35`; BaseService starts before
    listener bind `:111-138`; bind/rollback failure can leave BaseService cleanup authority unreachable, Stop may
    return at `:162-167`. Concrete `metric.Server` owns listener/server/serveDone `metric/handler.go:19-28,116-133`,
    has no BaseContext, Stop context, graceful Shutdown, or retained failure authority and waits under mutex
    `:100-166`. HTTP Q/F, NOT P. Coherent surface necessarily includes `metric/handler.go`; no sister direct NewServer
    consumer found.
33. `service/milestone_service.go`: opaque stop closure + Operation `:43-52`; failed Start stop remains installed
    `:91-101`; Stop Operation clears it `:110-133`. Wrapper P/F, but coherent native owner is paired
    `agentic/agentrun/agentrun.go` below.
34. `service/service_manager.go`: two exact HTTP servers, three Generations `:41-56,740-905`; three StopWithQuiesce
    Shutdown paths `:909-1005`; StopAll reverse aggregation `:502-578`; health publisher lacks retained cancel/done
    `:360-361,419-430`; same-instance health rebind advertised `:857-859`; async binds. Multi-HTTP Q + composition F;
    no measured StartAll/StopAll overlap proving M.
35. `storage/objectstore/component.go`: Generation/startDone before Store/sub acquisition `:385-423`; Store/exact core
    subs but name-only JS `:41-82`; cleanup name StopConsumer/core Drain/Store close `:535-577`; hidden JS `:764-835`;
    rollback retains generation but no explicit cleanupPending `:514-532`. M-primary + Q/F.
36. `agentic/agentrun/agentrun.go`: validates and can no-op if stream absent `:601-630`; Generation and name-based stop
    `:642-662`; two hidden internal consumers `:691-706`; second failure bounded rollback `:707-714`. Q/F. Coherent
    with MilestoneService; unique two-consumer leaf.

## Family/census reconciliation (current contribution; exact sums)

- Agentic five: owners 5, NG5, Stop10, Cancel0, SWQ0, Op0, rollback5.
- Graph read five: 5, NG5, Stop5, rollback0.
- Graph-ingest: 1, NG1, Stop1, Cancel1, Op1, rollback0.
- Document+IoT serialized Q/F clone: 2, NG2, Stop4, rollback2.
- JSON serialized Q/F trio: 3, NG3, Stop6, rollback3.
- Research quintet: 5, NG5, Stop5, rollback5.
- Weather: 1, NG1, Stop2, rollback1.
- Rule: 1, NG1, Stop1, Cancel1.
- File+HTTPPost: 2, NG2, Stop2.
- OTEL: 1, NG1, Stop1, Cancel1, Op1.
- Graph gateway: 1, NG1, SWQ1.
- Input websocket: 1, NG1, SWQ1.
- Output websocket: 1, NG1, SWQ1, rollback1.
- ComponentManager: 1, NG2, Stop1, SWQ2.
- MessageLogger: 1, NG1, Stop1.
- Metrics: 1, NG1, Stop1, Cancel1, rollback1.
- ServiceManager: 1, NG3, SWQ3.
- ObjectStore: 1, NG1, Stop2, rollback1.
- MilestoneService+agentrun: 2, NG1, Stop1, Op1, rollback1.
- Totals: owners36, NG38, Stop43, Cancel4, SWQ8, Op3, rollback20.

## Shared prerequisites and owner unlock matrix

- Task 2.1 exact native ConsumeContext return directly unlocks 17: agentrun; document; IoT; file; HTTPPost; output
  websocket; all five agentic; graph-ingest; JSON trio; rule; ObjectStore. Exported natsclient change requires owner
  design review.
- Task 2.2 duplicate durable identity applies to those 17 plus OTEL direct CreateOrUpdateConsumer = 18. It must reject
  rather than replace/catalog; exact claim shape remains owner-ruling territory.
- Stateless/exact core Subscription terminal semantics unlock 25 owner files: document, IoT, weather, file, HTTPPost,
  output websocket, agentic-loop, agentic-tools, graph-ingest, five graph-read, JSON trio, research quintet, rule,
  MessageLogger, ObjectStore. Existing exported wrapper is used outside repo; semantic-only change will not
  compile-fail.
- Stateless context wait/rollback helpers unlock every Generation/Operation owner; owner-local failed-Start records
  still must retain exact handles. Old rollback helper reaches 20 paths but may remain only if it is truly
  stateless/bounded and no running result/rejoin authority survives.
- Context/root inventory must cover owner-adjacent package files, not only the 36 import sites:
  - graph-ingest retains `ingestPoolCtx` and `ingestSubmitCtx` at `processor/graph-ingest/component.go:595-606`;
    `processor/graph-ingest/keyed_ingest.go:71-90` invents both pool roots with `context.Background`.
  - Rule is package-wide debt, not only `Processor.watcherCtx`. Stored contexts are
    `processor/rule/processor.go:162-164` (`watcherCtx`), `processor/rule/kv_config_integration.go:42-49`
    (`ConfigManager.ctx`), and `processor/rule/cron_scheduler.go:55-65` (`CronScheduler.parentCtx`).
    Unauthorized/fallback roots occur at `actions_lifecycle.go:189`, `kv_config_integration.go:65,416`,
    `cron_scheduler.go:352,393`, `expression_factory.go:181`, `lifecycle_substitution.go:229`,
    `runtime_config.go:261-270`, and `stateful_evaluator.go:282`; `stateful_evaluator.go:247` also uses bounded
    `WithTimeout(WithoutCancel(ctx))` and must be classified against the accepted durability exception rather than
    silently copied. `kv_test_helpers.go:27,261` is test-helper-only evidence, not production authority.
  - agentic-dispatch’s exact GraphView is not Start-context-owned:
    `processor/agentic-dispatch/http_activity.go:131-174` calls `view.Start(context.Background())` at `:166`. Its Stop
    remains at `:174-190`.
  - MessageLogger’s HTTP KV query accepts the request context and uses it for bucket lookup, then discards it for
    blocking KV operations: `service/message_logger_http.go:463-494` calls `kv.Keys(context.Background())` at `:494`,
    and its entry loop calls `kv.Get(context.Background())` at `:524`. The request-owned KV watch path separately uses
    `r.Context` and native watcher Stop at `service/message_logger_kv_watch.go:106-140,195-240`; it is not evidence
    that the query path is context-correct.
  - HTTPPost creates ACME authority in its constructor at `output/httppost/httppost.go:186-195`.
  - output websocket creates continuing send/ACK timeout roots at `output/websocket/websocket.go:1395,1480`.
  - input websocket and Metrics/metric.Server omit `http.Server.BaseContext` at
    `input/websocket/websocket_input.go:625-628` and `metric/handler.go:100-104`.
  - pprof is the separately declared process-lifetime default-root/no-join exception at `service/pprof.go:8-38`.
  These findings belong to restore-go context/root authority; none is precedent.
- Metrics prerequisite is specifically provider-aware Shutdown/BaseContext/serve completion in the coherent
  `service/metrics.go` + `metric/handler.go` unit. This does not justify a new generic provider or exported surface.
- Milestone prerequisite is coherent inclusion of `service/milestone_service.go` + `agentic/agentrun/agentrun.go`,
  after task2.1.
- Observation separation task 3.1 gates agentic-loop and graph-ingest.

## Same-class collision table (condensed)

- Semantic job: one-shot admission fence, exact native callback/listener close, remaining-runtime cancel/join,
  failed-Start cleanup.
- Owners: generic Generation/Operation; Client JS child catalog; exported core Subscription wrapper; five
  lifecycle-owned HTTP protocols plus the explicit pprof process-lifetime exception; 36 component/service records.
- Catalogs/identity: Client consumerBinding catalog `natsclient/client.go:619-649`; component
  configs/PortConsumerContext; OTEL direct durable derivation.
- Status/readers: graph readiness/backlog, agentic OutstandingWork, service/BaseService status, HTTP readiness.
- Lifecycle/writers: helper consumption and native Subscribe/HTTP Serve/CreateOrUpdate; Client name Stop/Delete; owner
  Stop paths.
- Recovery: 20 bounded rollback calls; only M-family members publish startDone today; graph-read/static outputs can
  leak partial exact handles; generic helpers replay results/rejoin.
- Ownership collision: same-name JS replacement, local duplicate durable identities, wrapper-vs-owner terminal
  authority, HTTP provider-vs-service authority.
- Empty new catalog/status/primitive cells: no new communication primitive, durable store, status key, payload, port,
  subject, or query operation is proposed.

## Mandatory adopter seam inventory

Specific adopter A: an external component author calling exported `natsclient.ConsumeStreamWithConfig*`.

- Must know today: component+port policy context and stream config; they cannot obtain lifecycle authority. Accepted
  target would add one fact: retain the exact returned native handle through Drain/Closed. They should not know
  durable-name derivation, Client catalogs, deletion, backlog math, Generation, or rejoin.
- If they do nothing after task2.1: return-arity use produces a compile error (best discovery rank), rather than
  silently retaining Client ownership.
- Present external source census is 27 direct helper-call files across five sister repositories: semdev 2, semdragon
  1, semspec 6, semspec-ui-bmad 9, semspec-ui-run-visibility 9. SemSpec’s sixth caller is
  `semspec/cmd/sandbox/qa_subscriber.go:70`; representative others remain `semdev/internal/intake/component.go:377`,
  `semdragon/processor/questtools/handler.go:32`, and `semspec/processor/researcher-manager/component.go:182`. Sister
  repositories remain read-only and migrate under their own owners.

Specific adopter B: external component retaining `*natsclient.Subscription`.

- Must know today: wrapper `Unsubscribe`; no external Drain call was found, but wrapper type is embedded in
  interfaces/fields, e.g. `semdev/internal/station/station.go:243`,
  `semdragon/processor/questbridge/component.go:118`, `semsource/processor/source-manifest/component.go:65-96`,
  `semsage/tools/spawn/executor.go:34`.
- If semantics change under the same signature: no compile error; discovery is tests/migration note at best, so this
  is an adopter finding requiring owner-reviewed guidance or a typed boundary. They SHOULD know only exact
  subscription closure, not wrapper replay state.

Specific adopter C: product composition roots using Manager StartAll/StopAll and MilestoneSubscriber.

- `service.Manager` is used by SemDev/SemDragon/SemSource/SemSpec/SemTeams; lifecycle signatures/config should remain
  unchanged, so doing nothing preserves composition.
- SemTeams directly composes MilestoneSubscriber at `semteams/cmd/semteams/main.go:939-941`; it should not retain two
  native consumer handles or names—the SemStreams subscriber/service unit must absorb them.

Specific adopter D: metrics user/config author.

- No sister use of `metric.NewServer` or `service.NewMetrics` was found; current exported NewServer consumer is in-repo
  Metrics. Adding another provider surface would have zero consumer at birth. Prometheus endpoint/config behavior
  should remain unchanged and adopters should know nothing about BaseContext/serveDone.

## Evidence questions and closed measurements for independent inventory review

1. Core Subscription owner census is closed: 25/36 remaining owners; broader in-repo holder census is 29 files after
   adding migrated graph-index and the three process/E2E holders.
2. Confirm task2.2 scope includes OTEL direct replacement and no additional direct durable creator among the 36.
3. External helper census is closed at 27 files/5 sister repositories after including SemSpec’s sandbox caller and
   excluding SemStreams worktrees, vendor, and generated files.
4. Decide only after INVENTORY PASS whether any family grouping is sufficiently equivalent for one implementation
   wave; unique exceptions above must not be erased.

This draft contains no target-state wave recommendation or approval. It must be materialized with baseline+hash,
independently reviewed to INVENTORY PASS, then returned for options/recommendation. Both child inventory agents are
complete/closed.
