# Agentic trajectory contract inventory

This inventory is measured at branch HEAD `9b5a3eee`. Historical comparison points are `427faca3`, `074b471f`, and
`5d97b58a`. It covers trajectory capture, durability, reads, configuration, documentation, and tests. Hierarchy,
research, and index redesign are outside this inventory.

This document records existing and historical behavior only. It contains no target state, option selection,
recommendation, or implementation task list.

## Historical capability

| Snapshot | Measured write and storage behavior | Measured read and restart behavior |
|---|---|---|
| Before `074b471f`, represented by `427faca3` | Agentic-loop configuration owned `trajectories_bucket`, `trajectory_ttl`, and `trajectory_history`, plus a default `trajectories` KV-write output (`427faca3:processor/agentic-loop/config.go:23`, `:28-29`, `:168`, `:173-174`, `:249-261`). Startup opened or created that bucket with default history 10 and TTL 24 hours (`427faca3:processor/agentic-loop/component.go:408-433`). After each handler, the component read and unmarshaled the whole aggregate, recalculated totals, appended steps, marshaled the whole aggregate, and called `Put`; terminal handling wrote end time and outcome (`427faca3:processor/agentic-loop/component.go:848-867`, `:1035-1123`). | The NATS trajectory handler read the KV value directly (`427faca3:processor/agentic-loop/component.go:1126-1159`). The last successful per-handler write survived a process restart or crash. KV supplied current state, up to ten retained historical revisions, and watch behavior. |
| `074b471f` | The KV aggregate remained. Terminal handling read the finalized KV value and added graph/ObjectStore emission: every step body was stored, metadata step entities were born, and `LoopHasStep` was appended (`074b471f:processor/agentic-loop/component.go:1052-1124`; `074b471f:processor/agentic-loop/graph_writer.go:150-176`, `:395-426`). | KV remained the direct query source (`074b471f:processor/agentic-loop/component.go:1127-1192`). Graph/ObjectStore emission produced a second representation only after terminal handling. |
| `5d97b58a` | The trajectory KV, its configuration, and its port were removed. The commit states that whole JSON aggregates could contain megabyte-scale tool results and calls the graph/ObjectStore copy durable. Per-handler persistence calls were removed; active capture moved to `TrajectoryManager`, and terminal serving moved to a process-local TTL cache (`5d97b58a:processor/agentic-loop/component.go`). | The E2E client changed from a direct KV read to bare NATS request/reply (`5d97b58a:test/e2e/client/nats.go`). Restart-readable KV behavior ended at this comparison point. |

The old KV path rewrote a potentially bulky whole-trajectory blob on each handler result. Updates used unconditional
`Put`, not compare-and-set. Marshal, read, and write failures were logged and returned from the helper without a
durable failure state. Consequently, the latest handler result or terminal update could be missing even though the last
successful aggregate remained restart-readable (`427faca3:processor/agentic-loop/component.go:1035-1123`).

## Current writers and stores

### Published aggregate shape

`agentic.Trajectory` is the aggregate, full-text response shape. Its steps can include prompts, responses, tool
arguments, tool results, full messages, and tool calls. `AddStep` appends a step and adds its duration and token totals;
`Complete` replaces aggregate duration with wall-clock elapsed time (`agentic/trajectory.go:8-84`).

### Shaping and cache configuration

Default configuration sets `trajectory_detail` to `summary` and `tool_result_max_bytes` to 32,768
(`processor/agentic-loop/config.go:47-58`, `:378-388`). In summary mode, the task step omits full messages and task
model, and the response step omits tool calls (`processor/agentic-loop/handlers.go:520-532`, `:1051-1067`). Full mode
populates those fields. Configuration and handler tests cover accepted detail values and the full-versus-summary
omissions (`processor/agentic-loop/config_test.go:380-400`;
`processor/agentic-loop/handlers_test.go:2803-2918`).

Oversized tool-result content is truncated before the tool result enters context and before the trajectory step copies
that content (`processor/agentic-loop/handlers.go:1975-1984`, `:2161-2188`). An invalid non-empty
`trajectory_cache_ttl` parse is ignored, leaving the four-hour default without an error or warning
(`processor/agentic-loop/component.go:608-619`). The working-list system-message trajectory claim at
`docs/operations/15-agent-private-state.md:154-160` therefore describes full-detail capture, not the default summary
shape.

### In-process capture

`TrajectoryManager` owns a mutex-protected process-local map. `StartTrajectory`, `AddStep`, and `GetTrajectory` operate
only on that map (`processor/agentic-loop/trajectory.go:12-76`). `DeleteTrajectory` exists but has no production caller.
`SaveTrajectory` says that it saves to KV but is a no-op (`processor/agentic-loop/trajectory.go:79-91`). The manager has
no TTL, terminal removal, or size bound.

Current step-append paths enter the manager at:

- loop start: `processor/agentic-loop/handlers.go:829-830`;
- task/model handling: `processor/agentic-loop/handlers.go:945-958` and `:1070-1075`;
- routine context compaction: `processor/agentic-loop/handlers.go:351-367`;
- retry-driven context compaction: `processor/agentic-loop/handlers.go:1645-1671`;
- tool handling: `processor/agentic-loop/handlers.go:2012-2021`.

`HandlerResult.TrajectorySteps` is documented as unconsumed at
`processor/agentic-loop/handlers.go:947-952`. There is no durable write for active steps. A process crash before
terminal handling loses the active trajectory.

### Terminal and cancellation paths

Normal terminal handling calls `finalizeTrajectory`, then `writeTrajectoryToGraph`, only for complete or failed results
(`processor/agentic-loop/component.go:1369-1392`). Cancellation separately finalizes and writes the trajectory
(`processor/agentic-loop/component.go:1949-1958`). `writeTrajectoryToGraph` reads only the in-process manager.
`CompleteTrajectory` has no production caller. `finalizeTrajectory` receives a value copy from `GetTrajectory`, calls
`Complete` on that copy, caches the copy, and classifies every state other than complete, including cancelled, as
failed (`processor/agentic-loop/trajectory.go:51-76`; `processor/agentic-loop/component.go:1748-1780`). The retained
manager value remains unfinalized. After cache expiry, the fallback at `processor/agentic-loop/component.go:1808-1817`
can therefore serve a completed trajectory with empty outcome and end time.

Completion construction reads the manager for token totals and the step set used by terminal-tool-less synthesis
(`processor/agentic-loop/handlers.go:1880-1894`). Failure construction separately reads it for token totals
(`processor/agentic-loop/handlers.go:2436-2444`). These are internal reads of the process-local aggregate, not durable
trajectory reads.

Terminal paths do not share one persistence order:

- Ordinary success reaches `persistHandlerResult`, which persists loop state, finalizes/caches the trajectory, persists
  terminal state, stamps graph state, writes trajectory steps, and only then publishes results
  (`processor/agentic-loop/component.go:1369-1391`).
- `handleLoopFailure` persists loop state and a failure event, and stamps loop-failure graph facts, but does not call
  `finalizeTrajectory` or `writeTrajectoryToGraph`; that path has no finalized cache copy or terminal trajectory-step
  graph write (`processor/agentic-loop/component.go:1212-1229`, `:1247-1270`).
- A tool-result timeout builds a failed `HandlerResult` and returns it with an error
  (`processor/agentic-loop/handlers.go:1955-1972`). The caller discards that result on the error return, before
  loop/failure persistence, trajectory finalization/cache, graph writes, or publication
  (`processor/agentic-loop/component.go:1549-1554`).
- Cancellation publishes `agent.complete.*` first, then writes cancellation graph/KV state, finalizes/caches the
  trajectory, and writes trajectory steps (`processor/agentic-loop/component.go:1913-1958`). A consumer reacting to the
  event can read before those later side effects, unlike the ordinary success ordering.

The graph writer first stores step bodies using loop-local entities, then discards the returned storage references. It
rebuilds fresh metadata entities, writes loop links and step entities independently, and warns and continues on
individual failures (`processor/agentic-loop/graph_writer.go:314-389`). `TrajectoryStepEntity.storageRef` is unexported,
and its graph triples omit that reference (`agentic/trajectory_entity.go:36-47`, `:55-142`). There is no write receipt,
retry/redrive path, or readiness/degradation state. A terminal graph/ObjectStore write can therefore be partial.

### Store and retention inventory

- `AGENT_LOOPS` is a KV bucket with history 10 and TTL 24 hours
  (`processor/agentic-loop/component.go:593-606`). It owns loop current state and `COMPLETE_<loopID>` terminal records.
  A completion record includes result text, prompt, token totals, model/iteration/workflow/run metadata, and routing
  metadata (`processor/agentic-loop/handlers.go:1855-1894`). It does not contain trajectory steps or step bodies.
- `processor/research-graph-synthesize` is a fourth production writer to `AGENT_LOOPS/COMPLETE_<loopID>`. It writes a
  BaseMessage-wrapped `research.SearchResult`, not an agentic terminal event
  (`processor/research-graph-synthesize/component.go:407-417`;
  `processor/research-graph-synthesize/adapters.go:156-171`). This existing research writer is inventoried here without
  bringing research redesign into scope.
- `agent.complete.*` publishes terminal events to the durable `AGENT` stream for acknowledged consumers. Successful
  handling publishes a `LoopCompletedEvent` (`processor/agentic-loop/handlers.go:1927-1943`), while cancellation
  publishes a distinct `LoopCancelledEvent` on the same output (`processor/agentic-loop/component.go:1913-1947`). It is
  an event-delivery surface, not the trajectory detail query source
  (`processor/agentic-loop/config.go:430-437`).
- The trajectory serving cache is process-local, defaults to four hours, and is configurable
  (`processor/agentic-loop/config.go:58`; `processor/agentic-loop/component.go:608-623`).
- Optional `AGENT_CONTENT` ObjectStore is opened during startup. A fatal retention mismatch fails startup, while other
  unavailability disables body storage (`processor/agentic-loop/config.go:57`, `:385`;
  `processor/agentic-loop/component.go:625-656`). No production trajectory reader is registered against it, and the
  body references are not persisted.
- `ENTITY_STATES` is the canonical current shared semantic state under ADR-090, with history 1 rather than audit-ledger
  retention. Trajectory step facts arrive only at terminal handling and contain metadata, not resolvable body
  references. ADR-054 classifies trajectory steps as graph-visible trace excluded from semantic embedding
  (`docs/adr/054-semantic-indexing-eligibility.md:90`, `:134`, `:272`). Retention and indexing are separate properties.

## Current readers and published APIs

The direct NATS subscriber hardcodes `agentic.query.trajectory`
(`processor/agentic-loop/component.go:386-392`). Its handler reads the TTL cache and then `TrajectoryManager`; a
positive limit returns a prefix of steps. A restart or process miss is classified `ErrorInvalid`, with a historical
“trajectory not found” message, rather than a not-found wire class. It performs no graph reconstruction or
ObjectStore body fetch (`processor/agentic-loop/component.go:1783-1825`). Cache expiry can fall back to the retained,
unfinalized manager copy while the process remains alive (`processor/agentic-loop/component.go:1808-1817`).

Every agentic-loop instance subscribes to that same plain NATS request/reply subject. In an account with multiple
agentic-loop deployments, each instance receives the request and can reply; the requester accepts whichever response
arrives first. The selected trajectory can therefore come from an arbitrary deployment
(`processor/agentic-loop/component.go:386-392`). The repository already records this exact permissive failure class for
the adjacent in-flight query contract (`openspec/specs/agentic-loop/spec.md:83-90`).

The HTTP surface registers list and detail routes (`processor/agentic-loop/http.go:27-44`). List enumerates
`AGENT_LOOPS`, then enriches from cache or manager. When both miss, token counts and duration remain zero without an
unknown marker (`processor/agentic-loop/http.go:107-212`). Detail reads only cache or manager and otherwise returns 404
(`processor/agentic-loop/http.go:214-255`). The generated OpenAPI document promises list and full-detail trajectory
responses (`specs/openapi.v3.yaml:791-869`, `:2383`).

GraphQL maps `trajectory` to the same NATS subject and declares a narrower, ad hoc trajectory type
(`gateway/graph-gateway/component.go:897-900`, `:1053-1054`, `:1114-1115`, `:1657`, `:1691-1692`). It does not query or
reconstruct graph state. NATS, HTTP, and GraphQL therefore all terminate at the process-local cache/manager read path.

`read_loop_result` is a separate restart-readable terminal-result reader. It fetches `COMPLETE_<loopID>` from
`AGENT_LOOPS`, decodes `LoopCompletedEvent`, and pages the `Result` text with role, outcome, completion, task, and byte
metadata (`processor/agentic-tools/loop_result.go:18-50`, `:101-161`). It does not return trajectory steps or bodies.

The `COMPLETE_<loopID>` key is polymorphic: success, failure, and cancellation write `LoopCompletedEvent`,
`LoopFailedEvent`, and `LoopCancelledEvent` shapes respectively (`processor/agentic-loop/component.go:1649-1717`).
`read_loop_result` always decodes the value as `LoopCompletedEvent` (`processor/agentic-tools/loop_result.go:132-161`).
Because missing fields decode to zero values, a failed or cancelled loop can appear as a successful empty result while
failure or cancellation detail is discarded.

The research-graph synthesizer adds a fourth shape at the same key: a BaseMessage envelope whose `research.SearchResult`
fields live under its payload. `read_loop_result` still decodes the top level as `LoopCompletedEvent`, yielding an empty
result and zero-value metadata (`processor/agentic-tools/loop_result.go:132-161`). Agentic-dispatch also expects a
top-level terminal loop wire; missing top-level `loop_id` makes the shared activity view poison the key
(`processor/agentic-dispatch/http_activity.go:42-56`; `processor/agentic-dispatch/loop_wire.go:122-131`). `flow_monitor`
peeks for top-level `workflow_slug` and skips the research envelope before its terminal decoder runs
(`processor/agentic-tools/flow_monitor_executor.go:252-277`).

`agentic-dispatch` consumes `agent.complete.*` with an explicit-ack durable consumer, invokes `handleAgentComplete`, and
then acknowledges regardless of whether that handler accepted the payload
(`processor/agentic-dispatch/component.go:412-426`). The handler asserts `LoopCompletedEvent`; a cancellation payload is
logged as unexpected and then acknowledged, so it is dropped from this consumer
(`processor/agentic-dispatch/component.go:807-821`). Successful completion has a second mismatch: the publisher sets
outcome to `success`, while dispatch's result branch matches `complete`, so the success result falls through to a status
response (`agentic/constants.go:37-42`; `processor/agentic-loop/handlers.go:1855-1863`;
`processor/agentic-dispatch/component.go:856-875`).

`agentrun.MilestoneSubscriber` is a separate concrete reader of `agent.complete.*` and `agent.failed.*`. It
demultiplexes the BaseMessage payload category into completion, cancellation, or failure and fans the normalized
terminal event out to registered handlers (`agentic/agentrun/agentrun.go:405-407`, `:460-533`). Its stable durable
consumers preserve offsets across subscriber restart, acknowledge accepted events, and negatively acknowledge
decode/infrastructure errors for redelivery (`agentic/agentrun/agentrun.go:625-693`).

`output/otel` is a third production reader of `agent.complete.*` through its required `agent.>` input on the `AGENT`
stream (`output/otel/config.go:54-65`). Its consumer invokes `SpanCollector.ProcessMessage` and acknowledges even when
processing returns an error (`output/otel/component.go:285-303`). The collector dispatches by payload category and has
separate completion and cancellation decoders (`output/otel/span_collector.go:182-223`). Its focused tests cover
completion, but not cancellation (`output/otel/span_collector_test.go:497-550`).

The production rule evaluator is not a current `COMPLETE_<loopID>` reader. Its typed watcher rejects every bucket other
than `ENTITY_STATES`, and no operational-KV adapter for `AGENT_LOOPS` exists
(`processor/rule/entity_pattern_contract.go:13-18`, `:39-64`). The default rule input declares only the
`ENTITY_STATES` watch (`processor/rule/config.go:220-227`).

## Port and configuration drift

Current runtime defaults expose `loops`, `graph_mutations`, `agent.request`, `tool.execute`, `agent.complete`, and other
event outputs, but no `trajectories` port (`processor/agentic-loop/config.go:378-455`). Construction strictly merges
overrides by declared name and rejects an unknown override (`processor/agentic-loop/component.go:128-155`;
`component/ports.go:153-206`).

Exactly seven shipped flow configurations still declare an output named `trajectories` targeting
`AGENT_TRAJECTORIES`:

1. `configs/agentic.json:391-397`;
2. `configs/flows/deep-research.json:240-245`;
3. `configs/flows/lesson-example.json:244-249`;
4. `configs/flows/ops-agent-test.json:247-252`;
5. `configs/flows/deep-research-test.json:262-267`;
6. `configs/flows/ops-agent.json:241-246`;
7. `configs/flows/crud-tools-test.json:247-252`.

Port migration commit `19ce5f7c` mechanically retained or reintroduced these rows. They claim a KV-write capability the
component cannot bind, so strict merge fails startup. Foundation B task 5.6 records the observed unknown
`trajectories` override failure.

The generated schema advertises `content_bucket`, `trajectory_cache_ttl`, and `trajectory_detail`. The cache TTL text
says older trajectories are available through graph queries (`schemas/agentic-loop.v1.json:45-49`, `:161-171`), but no
trajectory API reconstructs from graph state.

There is also a request-port asymmetry. Graph-gateway requires the `agentic_queries` request output for
`agentic.query.*` (`gateway/graph-gateway/component.go:134-157`), while agentic-loop's hardcoded trajectory responder is
outside its declared default input ports (`processor/agentic-loop/component.go:386-392`;
`processor/agentic-loop/config.go:378-421`).

## Conflicting durable documentation

The following current documentation still describes trajectory KV storage, restart behavior, or a trajectory storage
contract that differs from the measured runtime:

- `agentic/doc.go:274-279`, `:300`;
- `agentic/README.md:223`;
- `processor/agentic-loop/doc.go:203-248`, `:250-284`, `:397`;
- `processor/agentic-loop/README.md:51-54`, `:91-94`, `:130-133`, `:276` and later;
- `docs/adr/036-agent-private-observable-state.md:18`;
- `docs/adr/051-openai-responses-wire-support.md:162`, `:388`;
- `docs/concepts/13-agentic-systems.md:427-429`;
- `docs/concepts/17-approval-flow.md:155`;
- `docs/concepts/25-phased-agentic-chains.md:113`;
- `docs/concepts/27-frontier-harness-mapping.md:46`;
- `docs/concepts/32-agent-memory.md:47`;
- `docs/operations/02-troubleshooting.md:92-98`;
- `docs/operations/migration-beta19.md:173`, `:264`;
- `docs/advanced/08-agentic-components.md:47-50`, `:84-124`, `:412-415`, `:703-709`, `:732-735`, `:1145-1148`;
- `docs/advanced/11-jetstream-tuning.md:314`;
- `docs/basics/07-agentic-quickstart.md:93-96`, `:206-209`.

ADR-051 is internally contradictory: it places semstreams-owned trajectory state in graph/ObjectStore at lines 162-168,
then calls saved trajectories non-durable operator debug artifacts at lines 388-392
(`docs/adr/051-openai-responses-wire-support.md:162-168`, `:388-392`).

ADR-073 is Proposed and design-only. Lines 197-202 describe `AGENT_TRAJECTORIES` as an existing windowed firehose and
the graph copy as redundant (`docs/adr/073-graph-ingestion-retention-contract.md:197-202`). That premise is stale at
HEAD and conflicts with the owner's later rejection of the obsolete “stop storing” premise on issue #873.

Current code comments and guides also claim that rules or downstream watchers consume `COMPLETE_<loopID>` directly,
despite the production rule evaluator rejecting `AGENT_LOOPS`:

- `processor/agentic-loop/component.go:1232-1259`, `:1646-1661`;
- `processor/agentic-loop/handlers.go:1855-1857`, `:1941-1943`;
- `docs/concepts/13-agentic-systems.md:288-289`, `:425`, `:461-473`;
- `docs/concepts/14-orchestration-layers.md:124-147`;
- `docs/concepts/23-parallel-agents.md:145-170`, `:257-275`, `:445-459`, `:693-707`;
- `docs/advanced/12-coordinator-pattern.md:106-108`.

`docs/concepts/14-orchestration-layers.md:345-348` contradicts its earlier example by correctly stating that the rule
entity evaluator does not decode operational buckets and needs a separately designed typed adapter.

ADR cleanup text also claims trajectory or loop/trajectory records retire with `COMPLETE_*` cleanup, which is not the
measured unbounded-manager or current store behavior
(`docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md:435-441`;
`docs/adr/055-graph-write-intent-taxonomy.md:568-579`).

Code and schema comments also call ObjectStore plus graph durable and promise post-cache graph-query availability
(`processor/agentic-loop/component.go:58`, `:608`; `processor/agentic-loop/config.go:58`;
`schemas/agentic-loop.v1.json:161-165`). Current APIs cannot reconstruct that representation, and the writer discards
the stored body references.

## Tests and coverage gaps

Current tests cover manager behavior, including the no-op `SaveTrajectory`, and gateway routing or payload
pass-through. The trajectory E2E client explicitly describes its source as the in-memory cache
(`test/e2e/client/nats.go:260-280`). Its scenario queries only a same-process completed loop
(`test/e2e/scenarios/agentic/scenario.go:348-402`), and the integration test queries before component stop or restart
(`processor/agentic-loop/loop_integration_test.go:618-745`).

The graph-writer integration test does not prove that the writer's stored reference is reachable. It stores and fetches
its own second copy instead (`processor/agentic-loop/graph_writer_integration_test.go:825-852`).

The research-graph E2E verifies only that `COMPLETE_<loopID>` contains a non-empty BaseMessage with the expected payload
category (`test/e2e/scenarios/research-graph/scenario.go:455-481`). It does not pass that value through
`read_loop_result`, agentic-dispatch activity decoding, or `flow_monitor`.

The ops E2E writes synthetic success and failure `COMPLETE_<loopID>` values directly, bypassing the production terminal
writers (`test/e2e/scenarios/ops/scenario.go:331-419`). Its completion wait succeeds when any non-seed `COMPLETE_*` key
appears and does not decode or validate that value (`test/e2e/scenarios/ops/scenario.go:508-547`).

There is no E2E coverage for:

- restart-readable detail;
- a crash before terminal handling;
- graph reconstruction or ObjectStore body retrieval;
- reachability of the writer-created storage reference;
- partial terminal graph/ObjectStore writes;
- arbitrary first-responder selection with multiple agentic-loop deployments on `agentic.query.trajectory`;
- failed and cancelled `COMPLETE_<loopID>` records decoded through the success-only `read_loop_result` shape;
- research `SearchResult` envelopes at `COMPLETE_<loopID>` consumed by any of the three shared readers;
- cancellation delivery through agentic-dispatch's `agent.complete.*` durable consumer;
- successful result delivery through agentic-dispatch's outcome switch;
- failed and cancelled trajectory queries across their distinct terminal paths;
- cancellation read-after-event ordering against KV, cache, graph, and trajectory-step side effects;
- `output/otel` cancellation span handling;
- production terminal writers and shared-reader decoding in the ops scenario;
- repair/redrive or readiness/degradation behavior.

## Issue evidence

- [#873](https://github.com/C360Studio/semstreams/issues/873) is open for discarded stored-body references. The owner
  withdrew “stop storing” and re-ruled the condition as a broken audit trail: SemStreams owns the evidence
  primitive, repair has to retain resolvable references while trace stays non-embedded, and store registration/honesty
  is a prerequisite.
- [#876](https://github.com/C360Studio/semstreams/issues/876) is open for the manager retaining every completed
  full-text trajectory without a bound.
- [#877](https://github.com/C360Studio/semstreams/issues/877) is open for the unversioned published `Trajectory`,
  ambiguous `Duration`, treatment of cache as durable, undeclared bare query subject, no-op `SaveTrajectory`, and list
  summaries that report unknown metrics as zero.
- [#888](https://github.com/C360Studio/semstreams/issues/888) is open because no E2E tier covers an unresolvable
  `StorageInstance` or restart repair.
- [#633](https://github.com/C360Studio/semstreams/issues/633) is open for reference-aware reclamation of orphaned
  content-addressed blobs. With lifecycle TTL removed, unreclaimed bodies accumulate without a reference count,
  mark-and-sweep owner, or size/object-count gauge.
- [#857](https://github.com/C360Studio/semstreams/issues/857) is open for payload-size failures across framework writes.
  Its ledger includes log-and-drop `COMPLETE_<loopID>` writes and warn-and-continue trajectory body writes.
- [#875](https://github.com/C360Studio/semstreams/issues/875) is open for instance-blind `StorageRef` fallback in graph
  embedding. Persisting the references currently discarded by agentic-loop would make that failure path reachable.
- [#881](https://github.com/C360Studio/semstreams/issues/881) is open because no current gauge or durable per-entity
  record counts graph entities whose offloaded body is unreachable.
- [#865](https://github.com/C360Studio/semstreams/issues/865) is open because agentic-dispatch matches `complete`
  against a successful event whose outcome is `success`, so the result is not returned through its result branch.
- [#866](https://github.com/C360Studio/semstreams/issues/866) is open because agentic-dispatch rejects cancellation's
  distinct payload type, then acknowledges and drops the message.

The post-GS01 reality audit also lists #865 and #866 as unexamined successful-outcome and cancellation-payload
mismatches (`docs/proposals/post-gs01-graph-state-reality-audit.md:598-599`).

## Same-class collision inventory

| Surface | Measured ownership and retention | Readers | Recovery, convergence, or status |
|---|---|---|---|
| `TrajectoryManager` map | Active capture; process-local; no TTL or terminal removal | HTTP and NATS paths | None |
| TTL cache | Terminal serving accelerator; process-local; configurable, default four hours | HTTP and NATS paths | None |
| `AGENT_LOOPS` loop entries | Loop current state; TTL 24 hours; history 10; watchable | HTTP list and agentic-dispatch activity | No steps or bodies; rule evaluator rejects this bucket |
| `AGENT_LOOPS` `COMPLETE_<loopID>` | Four production shapes share one key: success, failure, cancellation, and BaseMessage-wrapped research `SearchResult`; TTL 24 hours; history 10 | `read_loop_result`, agentic-dispatch activity, and `flow_monitor`; not the rule evaluator | Success-only read yields empty failure/cancel/research results; activity poisons research values; monitor skips research values |
| `agent.complete.*` on `AGENT` | `LoopCompletedEvent` and `LoopCancelledEvent` share one durable subject; cancellation publishes before its other terminal side effects | Agentic-dispatch, `agentrun.MilestoneSubscriber`, and `output/otel` | Dispatch acks/drops cancellation and misses `success` result branch; agentrun demuxes categories; OTel decodes both but lacks a cancellation test |
| Plain `agentic.query.trajectory` | Every agentic-loop deployment subscribes to the same request subject | NATS, GraphQL, and gateway callers | First arbitrary response wins; no deployment selector |
| Historical `AGENT_TRAJECTORIES` KV | Removed current-state/read model; TTL 24 hours; history 10; watchable; direct read | Historical NATS/direct readers; current configs and docs still claim it | Removed from runtime |
| `ENTITY_STATES` | Canonical current semantic state; history 1; terminal step metadata only | Graph queries can enumerate metadata; trajectory APIs do not | No audit reconstruction or repair status |
| `AGENT_CONTENT` ObjectStore | Intended body evidence; terminal best-effort writer | No production trajectory reader | No persisted reference or reclamation owner |
| Ports, config, and schema | Seven claimed trajectory KV writes, a claimed graph-query fallback, and asymmetric query declarations | Adopter configuration and generated schema | Runtime lacks the KV write and graph fallback; responder is undeclared |

No current surface owns a complete, restart-readable trajectory contract. No current surface owns convergence,
readiness, or degradation for the combined metadata-and-body representation.

## Adopter seam inventory

The specific adopter is an external developer operating a configured agentic-loop and reading trajectories through
HTTP, GraphQL, or NATS without opening this repository.

- **What must they know today?** Full detail is scoped to manager/cache residence. Cache expiry can fall back to the
  retained unfinalized manager copy, while process loss or restart produces an `ErrorInvalid`-class “trajectory not
  found” response despite graph metadata. In multi-deployment accounts, the shared NATS subject can return an
  arbitrary deployment's answer. Zero list metrics can mean unknown. `read_loop_result` can present a failed or
  cancelled record or a research `SearchResult` as an empty success shape. The same research value poisons
  agentic-dispatch activity and is skipped by `flow_monitor`. On `agent.complete.*`, agentic-dispatch acknowledges and
  drops cancellation, and its success-outcome mismatch does not return successful result content through the result
  branch; the separate agentrun and OTel readers do distinguish terminal categories. Summary capture omits full
  messages, task model, and response tool calls; tool content can be truncated. Failure and tool-timeout terminal paths
  do not produce the same cache/graph/publication state as success, and cancellation publishes before its
  KV/cache/step writes. The configured `trajectories` output is rejected, graph step bodies are not retrievable, and
  terminal writes can be partial. Despite several code and guide claims, the rule evaluator does not read
  `AGENT_LOOPS`.
- **What happens if they do nothing?** The seven shipped configurations fail strict merge. Removing the row allows
  startup, but a crash or restart loses trajectory detail from the published read APIs. Multiple deployments can answer
  the same query nondeterministically. Failure, cancellation, and research-result detail can be lost in shared-reader
  decoding; the research value can also poison or disappear from adjacent activity and flow views. Dispatch users can
  lose cancellation delivery and successful result content even though its durable consumer acknowledges the events.
  Default summary/truncation shaping silently limits captured detail, an invalid cache TTL silently falls back to four
  hours, failed or timed-out paths can lack finalized trajectory state, and cancellation consumers can race later
  terminal writes.
- **Where do they find out?** Nowhere coherently. OpenAPI, schema, durable docs, shipped configuration, runtime code,
  and open issues disagree.
- **What should they have to know?** Only stable API semantics and explicit availability or degradation. They should not
  need bucket names, subjects, cache TTLs, graph reconstruction rules, storage-reference resolution, or repair
  choreography.

The current seam makes the adopter predict capture mode, truncation, cache residence, terminal path and ordering,
responder deployment, terminal event shape, graph completeness, body reachability, rule-adapter existence, and which
internal store answers. Those are framework-owned observations. The measured prediction burden is the absence of a
response contract that reports actual completeness, provenance, and degradation from observed writes and reads.

## KV-or-stream inventory classification

Applying the repository's four-test heuristic to a trajectory current/execution record yields the following measured
classification:

| Test | Classification evidence |
|---|---|
| Restart | A reader needs current trajectory facts rehydrated, not unacknowledged work resumed. |
| Fan-out or queue | Current trajectory state is observable by multiple readers and watchers, not consumed by one worker. |
| Processing time | Applying a current-state update is fast and can be idempotent; the trajectory record is not the LLM or tool side effect itself. |
| Nature | A recorded execution step or aggregate is a fact about completed or current execution, not a request to perform execution. |

All four tests classify the record as fact/current state, corresponding to KV/watch or another materialized-state
owner, not a queued-work stream. A graph-derived owner is in the same fact/materialized-view class. Under the mandatory
derived-owner rule, that class carries idempotent desired-state apply, explicit failed-work repair/redrive, and visible
readiness/degradation; bootstrap hydration is not retry. The current terminal best-effort graph write has none of those
measured properties.

This classification does not choose a target state. It records the communication class and the owner obligations that
would accompany a graph-derived materialized view. Hierarchy, research, and index redesign remain explicitly outside
scope.
