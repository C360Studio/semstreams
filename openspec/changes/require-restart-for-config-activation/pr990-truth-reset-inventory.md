# PR #990 truth-reset inventory

## Checkpoint identity

- Status: `INVENTORY PENDING`
- Inventory date: 2026-08-18
- Repository: `C360Studio/semstreams`
- Current-main baseline: `eb1f6d7758f75a2ff5598e2ca92af92e8c21d753`
- Historical PR #990 head: `8f19ef3678a549913385b090e4de1766a7a43a27`
- Historical merge base: `9fcc841ee792a080a7b9998bfb51400cd81b24fe`
- Branch: `codex/gh986-boot-only-flow-activation`
- Content hash: recorded externally in
  `openspec/changes/simplify-one-shot-lifecycle-ownership/recovery-ledger.md`
  after this exact artifact is materialized.
- This is an inventory-only checkpoint. It grants no implementation,
  lifecycle, proof, or release credit.

## Problem statement

Draft PR #990 contains useful boot-only composition and flow-authoring work,
but it was built while lifecycle authority and flow semantics were changing.
A normal merge or commit-by-commit rebase can therefore resolve five textual
conflicts while silently accepting incompatible semantics elsewhere.

This inventory measures the complete historical patch against current main.
It separates:

1. boot-only topology work actually present in the patch;
2. flow authoring/validation work ratified by the owner;
3. incomplete Rule hot-reload work;
4. lifecycle machinery merely carried forward;
5. current-main authority that the historical branch must not overwrite.

## Measured patch surface

The historical patch contains nine commits:

1. `78f95b2e` `refactor(runtime): seal component activation at boot`
2. `166a9cde` `docs(runtime): document boot-only activation`
3. `f5ba7a61` `refactor(runtime): enforce immutable boot composition`
4. `81176053` `docs(runtime): align boot activation contract`
5. `7d32530d` `refactor(flow): make diagrams boot-only authoring`
6. `2471ddf3` `docs(flow): document diagram-only activation`
7. `7a33998b` `fix(lifecycle): close boot-only composition review gaps`
8. `1d420592` `fix(flow): close final authoring review gaps`
9. `8f19ef36` `test(flow): tighten monitor fail-closed proof`

Measurement:

```text
git diff --shortstat \
  9fcc841ee792a080a7b9998bfb51400cd81b24fe..\
8f19ef3678a549913385b090e4de1766a7a43a27

173 files changed, 4729 insertions(+), 24265 deletions(-)
```

Top-level file count:

| Surface | Files |
|---|---:|
| `cmd` | 4 |
| `component` | 13 |
| `config` | 8 |
| `configs` | 3 |
| `docs` | 12 |
| `engine` | 8 |
| `flowstore` | 8 |
| `flowtemplate` | 1 |
| `internal` | 4 |
| `message` | 1 |
| `model` | 2 |
| `natsclient` | 2 |
| `openspec` | 7 |
| `pkg` | 2 |
| `processor` | 37 |
| `service` | 54 |
| `specs` | 1 |
| `test` | 5 |
| `types` | 1 |

The complete immutable path manifest is reproduced by:

```text
git diff --name-status \
  9fcc841ee792a080a7b9998bfb51400cd81b24fe..\
8f19ef3678a549913385b090e4de1766a7a43a27
```

Current main changed 24 files from the same merge base:

```text
24 files changed, 1386 insertions(+), 836 deletions(-)
```

Only five paths overlap textually:

```text
docs/operations/migration-restore-go-lifecycle-ownership.md
openspec/changes/require-restart-for-config-activation/design.md
openspec/changes/require-restart-for-config-activation/proposal.md
openspec/changes/require-restart-for-config-activation/specs/component-runtime-config/spec.md
openspec/changes/require-restart-for-config-activation/tasks.md
```

This small textual overlap is misleading. The branch also deletes
`specs/flow-activation-truth/spec.md` without a merge conflict and retains
lifecycle machinery contradicted by current-main authority.

# Surface inventory

## 1. Claimed gaps

### Boot-only component topology

The historical branch implements a distinct sealed boot snapshot:

- `8f19ef36:config/manager.go:21-46` stores desired configuration separately
  from a private boot snapshot.
- `8f19ef36:config/manager.go:253-280` seals the post-arbitration boot
  configuration before live desired-state watchers run and returns defensive
  copies through `BootConfig`.
- `8f19ef36:config/manager.go:283-304` compares current desired component
  configuration with the actual sealed boot map through
  `ComponentRestartRequired`.
- `8f19ef36:config/manager.go:533-586` documents component writes and deletes
  as desired next-boot mutations.
- `8f19ef36:service/component_manager.go:154-166` reads the sealed boot
  configuration once during construction.
- `8f19ef36:service/component_manager.go:239-341` constructs the boot component
  set and seals Registry admission.
- `8f19ef36:service/component_manager.go:344-413` has no desired-config drain,
  reconciliation, or post-boot component-start lane.
- `8f19ef36:component/registry.go:100-170` retains immutable declaration values,
  not live component handles.
- `8f19ef36:component/registry.go:229-270` restricts component creation to an
  internal boot-admission token.
- `8f19ef36:component/registry.go:369-376` seals composition.

The generic component mutation search was:

```text
git grep -n -E \
  'ReplaceComponent|RemoveComponent|watchConfigUpdates|reconcileComponents|UpdateConfig' \
  8f19ef36 -- '*.go'
```

It found only an unrelated graph-inference `UpdateConfig` and historical
comments in `natsclient/storage_report*`. No general post-boot component
replacement, removal, reconciliation, or generic configuration hook remains.

### Flow diagrams as authoring, validation, and compilation

The historical branch implements the owner-ratified authoring-only boundary:

- `8f19ef36:engine/engine.go:18-46` states that Engine is a
  validator/compiler and owns no component or service lifecycle.
- `8f19ef36:engine/engine.go:49-113` validates diagrams and compiles detached
  component configuration candidates.
- `8f19ef36:flowstore/flow.go:11-30` retains diagram identity, metadata, nodes,
  connections, and audit data; runtime lifecycle fields are removed.
- `8f19ef36:service/flow_service.go:72-86` states that FlowService is not a
  lifecycle owner.
- `8f19ef36:service/flow_service.go:195-210` exposes CRUD, validation, explicit
  publication, and name-keyed observations; deploy/start/stop/undeploy routes
  are absent.
- `8f19ef36:service/flow_service.go:354-408` makes publication explicit,
  upsert-only, retryable, and honest about `runtime_unchanged` and
  `restart_required`.
- `8f19ef36:processor/agentic-tools/executors/flows.go:16-127` retains
  authoring CRUD tools and removes flow lifecycle tools.
- `8f19ef36:docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md:19-48`
  records the authoring-only decision.

The lifecycle-name search was:

```text
git grep -n -E \
  'DeployFlow|StartFlow|StopFlow|UndeployFlow|monitor_flow|flow_lifecycle' \
  8f19ef36 -- '*.go'
```

It returned no production or test matches.

### Rules as the only intended live configuration exception

The branch narrows the generic update boundary:

- `8f19ef36:processor/rule/runtime_config.go:13-38` requires a non-nil context
  and rejects every runtime update envelope except one `rules` object.
- `8f19ef36:processor/rule/kv_config_integration.go:39-56` retains a private
  cancel function and join state, not a context.
- `8f19ef36:processor/rule/kv_config_integration.go:81-121` derives the watcher
  lifetime from `Start(ctx)`.
- The dynamic `entity_watch_buckets` update path is removed; the watcher set is
  boot configuration.

This is a boundary implementation, not completion of the current
`rule-hot-reload` target:

- `8f19ef36:processor/rule/kv_config_integration.go:106-114` still watches
  global `rules.*` and returns success after watcher creation fails, silently
  disabling hot reload.
- `8f19ef36:processor/rule/kv_config_integration.go:177-193` treats unexpected
  watcher closure as terminal and only logs reconcile failures.
- `8f19ef36:processor/rule/runtime_config.go:40-89` mutates rules sequentially;
  a later failure can leave earlier rules changed.
- It has no pack-scoped desired records, typed tombstones, revision receipt,
  candidate-set atomic swap, activation facts, `boot_id` readiness join,
  admitted activation reader, repair supervisor, or revision-bound terminal
  outcome.

Current main still tracks these items as unchecked work at
`openspec/changes/require-restart-for-config-activation/tasks.md:40-65`.

## 2. Every current spelling of the modeled facts

### Composition authority

The patch contains four distinct representations whose roles and limits are:

| Representation | Evidence | Observed role and limit |
|---|---|---|
| Mutable desired projection | `8f19ef36:config/manager.go:96-99,372-419,533-683` | `GetConfig` exposes the manager’s current in-memory authoring projection. Local component writes update it synchronously; external writes update it only through successfully created watchers. It can therefore be stale or partial. |
| Sealed boot snapshot | `8f19ef36:config/manager.go:101-280` | `BootConfig` is a defensive immutable clone of the in-memory configuration present when `Start` seals it. It is process-composition input, but it is not proof that KV arbitration completed successfully or completely because several arbitration failures are logged and ignored. |
| Registry declaration snapshot | `8f19ef36:component/registry.go:100-170` | Immutable factory, port, and resource declaration values; no retained live component handle. |
| Runtime handles | `8f19ef36:service/component_manager.go:204-220` | Private ComponentManager-owned live handles. |

`BootConfig` and `GetConfig` have separate production reader sets:

| Reader | Read | Classification and use |
|---|---|---|
| `config.Manager.ComponentRestartRequired` | `8f19ef36:config/manager.go:283-304` reads `BootConfig` and the current SafeConfig | Compares sealed boot component truth with the current in-memory desired projection; its answer inherits any incompleteness in either snapshot. |
| `bootstrapobservability.StartConfigManager` | `8f19ef36:internal/bootstrapobservability/bootstrap.go:168-215` reads `BootConfig` | Exports the sealed snapshot to the process composition root as effective boot input. |
| `ComponentManager` constructor | `8f19ef36:service/component_manager.go:154-166` reads `BootConfig` | Consumes sealed boot Components, Security, and ModelRegistry for runtime construction. |
| `FlowService` constructor | `8f19ef36:service/flow_service.go:111-131` reads and retains `BootConfig` | Uses sealed boot Components for first-run default-diagram import at `service/flow_service.go:151-184`. |
| Metrics service constructor | `8f19ef36:service/metrics.go:76-84` reads `BootConfig` | Consumes sealed boot Security configuration. |
| FlowService override-expiry reporter | `8f19ef36:service/flow_service.go:460-478` reads `GetConfig` | Reads mutable desired configuration on each report cycle. |
| ServiceManager service-status handler | `8f19ef36:service/service_manager.go:1355-1388` reads `GetConfig` | Compares sealed boot service configuration with the mutable desired projection to report pending service changes and `restart_required`. |

`component/flowgraph/flowgraph.go` and `component/dependencies.go` remove live component handles from observation and dependency values. ComponentManager retains only private callback-scoped borrowing at
`8f19ef36:service/component_manager.go:637-666`.

### Config watch partial success and boot-snapshot limits

`config.Manager.Start` has a per-pattern partial-success path:

- `8f19ef36:config/manager.go:205-213` declares five independent desired-state watches:
  `services.*`, `components.*`, `platform`, `nats`, and `model_registry`.
- `8f19ef36:config/manager.go:228-238` logs watcher-creation failure at Debug and continues.
- `8f19ef36:config/manager.go:240-244` fails Start only when all five watchers fail. One successful watcher makes Start successful even when the other four desired-state classes are unwatched.
- `8f19ef36:config/manager.go:246-251` then clears every pending local write while claiming every local write has an active watcher. That claim is false after partial watcher creation.
- `8f19ef36:config/manager.go:253-263` seals BootConfig and launches only the successful watchers. The stored watcher slice does not retain pattern identity.
- `8f19ef36:config/manager.go:348-370` reads `watcher.Updates()` without checking the channel’s `ok` value. Unexpected channel closure can therefore repeatedly select a nil entry until context cancellation or shutdown.
- No `config/*_test.go` at the historical head fault-injects failure of one watch pattern while another succeeds.

Observed consequences are pattern-specific:

| Failed watch | Desired/read impact |
|---|---|
| `components.*` | External component writes do not enter `GetConfig`; `ComponentRestartRequired` can report no restart against a stale desired map. Local `PutComponentToKV` and `DeleteComponentFromKV` still mutate the in-memory desired map synchronously at `config/manager.go:533-602`. |
| `services.*` | ServiceManager’s pending-service list and `restart_required` response can omit externally authored service changes. |
| `platform`, `nats`, or `model_registry` | `GetConfig` remains stale for that section, including any reporter reading it as current desired truth. |
| Any subset | There is no typed readiness or degraded-state record identifying the inactive patterns; the creation failure is a Debug log only. |

The sealed snapshot has independent completeness limits:

- `8f19ef36:config/manager.go:109-122` treats failure to check KV existence as first boot and continues after an initial PushToKV failure.
- `8f19ef36:config/manager.go:156-201` logs and continues after version-read, version-compare, KV-sync, or file-to-KV push failures.
- `8f19ef36:config/manager.go:751-798` resets Services before KV overlay but does not reset Components. Absent KV component keys can therefore leave file-configured components in the sealed map.
- `8f19ef36:config/manager.go:779-794` skips individual failed KV reads or applies and still returns success after processing the remaining keys.

Accordingly, BootConfig is exact as a clone of the resulting in-memory state, but the branch does not establish that the resulting state is a complete durable arbitration result. Restart can reload durable state, but the running process has no repair path for a missing watcher and no typed indication that desired/restart reporting is partial.

### Flow facts

The historical patch has:

- durable authoring diagrams in `flowstore`;
- validation and compilation in `engine`;
- explicit candidate publication in `service/flow_service.go`;
- current desired component configuration in `config.Manager`;
- sealed effective boot configuration in `config.Manager`;
- component health/metrics/messages observed by component names.

It removes flowstore lifecycle state, deployment timestamps, flow lifecycle
tools, flow status streaming, flow-associated logs, and flow runtime streams.

The current-main `flow-activation-truth` capability is a conflicting spelling:

- `specs/flow-activation-truth/spec.md:3-29` defines
  deploy/start/stop/undeploy desired-state transitions.
- `specs/flow-activation-truth/spec.md:31-71` defines desired/effective
  provenance and boot digests.
- `specs/flow-activation-truth/spec.md:73-84` defines lifecycle-shaped mutation
  responses.

The owner has since ruled that Flow Engine exists to validate construction and
must not participate in lifecycle. That ruling makes the historical branch’s
ADR-096 direction authoritative, but current-main flow truth must be explicitly
superseded and rewritten. Silent deletion is not reconciliation.

### Rule activation facts

The patch currently has:

- desired Rule JSON under global `rules.*`;
- in-memory expression and cron maps;
- scheduler registrations;
- logs and metrics for application failures.

Current-main target truth additionally claims:

- pack-scoped desired facts;
- exact committed revisions;
- typed terminal activation facts;
- current boot incarnation and Rule readiness;
- bounded repair and typed activation reads.

Those target facts are not implemented by historical PR #990.

### Lifecycle coordination

The prior “42 production owners” statement was unsupported subtraction. The exact historical-head census is:

```text
git grep -l \
  '"github.com/c360studio/semstreams/internal/lifecyclejoin"' \
  8f19ef36 -- '*.go' |
  sed 's/^[^:]*://' |
  grep -v '_test\.go$'
```

This returns 41 non-test production importer/owner files. Forty use
`Generation`; three use `Operation`, with `output/otel` and `graph-ingest`
present in both sets. The union remains 41.

Generation owner locations:

- `8f19ef36:agentic/agentrun/agentrun.go:643`
- `8f19ef36:examples/processors/document/component.go:88`
- `8f19ef36:examples/processors/iot_sensor/component.go:88`
- `8f19ef36:examples/processors/weather_station/component.go:71`
- `8f19ef36:gateway/graph-gateway/component.go:289`
- `8f19ef36:input/file/file.go:113`
- `8f19ef36:input/http/http.go:86`
- `8f19ef36:input/websocket/websocket_input.go:76`
- `8f19ef36:output/file/file.go:117`
- `8f19ef36:output/httppost/httppost.go:111`
- `8f19ef36:output/otel/component.go:62`
- `8f19ef36:output/websocket/websocket.go:154`
- `8f19ef36:processor/agentic-dispatch/component.go:95`
- `8f19ef36:processor/agentic-governance/component.go:52`
- `8f19ef36:processor/agentic-loop/component.go:52`
- `8f19ef36:processor/agentic-model/component.go:58`
- `8f19ef36:processor/agentic-tools/component.go:58`
- `8f19ef36:processor/gated-dag/executor.go:72`
- `8f19ef36:processor/graph-clustering/component.go:645`
- `8f19ef36:processor/graph-embedding/component.go:297`
- `8f19ef36:processor/graph-index-spatial/component.go:186`
- `8f19ef36:processor/graph-index-temporal/component.go:195`
- `8f19ef36:processor/graph-index/keyed_dispatcher.go:19`
- `8f19ef36:processor/graph-ingest/component.go:506`
- `8f19ef36:processor/graph-query/component.go:176`
- `8f19ef36:processor/json_filter/json_filter.go:88`
- `8f19ef36:processor/json_generic/json_generic.go:76`
- `8f19ef36:processor/json_map/json_map.go:95`
- `8f19ef36:processor/research-graph-assess/component.go:62`
- `8f19ef36:processor/research-graph-classify/component.go:72`
- `8f19ef36:processor/research-graph-execute/component.go:55`
- `8f19ef36:processor/research-graph-route/component.go:61`
- `8f19ef36:processor/research-graph-synthesize/component.go:51`
- `8f19ef36:processor/rule/processor.go:99`
- `8f19ef36:service/base.go:112`
- `8f19ef36:service/component_manager.go:90`
- `8f19ef36:service/message_logger.go:233`
- `8f19ef36:service/metrics.go:27`
- `8f19ef36:service/service_manager.go:55`
- `8f19ef36:storage/objectstore/component.go:49`

Operation owner locations:

- `8f19ef36:output/otel/component.go:63`
- `8f19ef36:processor/graph-ingest/component.go:507`
- `8f19ef36:service/milestone_service.go:51`

The two non-test implementation files previously folded into the arithmetic are:

- `8f19ef36:internal/lifecyclejoin/generation.go:13-24`, defining
  `Generation` with shared quiesce and stop Operations;
- `8f19ef36:internal/lifecyclejoin/operation.go:11-32`, defining concurrent
  caller joining, retained results, and later-caller resumption.

Thus the 43 non-test files returned by the broader symbol search are exactly
41 importer/owner files plus those two implementation files. They are not
42 owners plus one undifferentiated helper package.

PR #990 also introduces a separate same-class coordination owner that does
not import `internal/lifecyclejoin`:

- `8f19ef36:processor/rule/cron_scheduler.go:39-64` adds `dispatch`,
  `dispatchDone`, `started`, `stopping`, and per-request `done`.
- `8f19ef36:processor/rule/cron_scheduler.go:221-268` creates a Start-owned
  dispatcher, spawns one goroutine per admitted fire, waits for the fire
  WaitGroup, and closes shared `dispatchDone`.
- `8f19ef36:processor/rule/cron_scheduler.go:376-408` makes concurrent Stop
  callers join the same `dispatchDone`; the first Stop joins robfig callbacks,
  closes dispatch, and joins all admitted work.
- `8f19ef36:processor/rule/cron_scheduler.go:479-505` uses the scheduler mutex
  as an admission/Stop fence and blocks each robfig callback on its request’s
  completion.
- `8f19ef36:processor/rule/cron_scheduler_test.go:439-490` explicitly launches
  two concurrent Stop calls and requires them to join concurrent register,
  fire, deregister, and cancellation activity.
- `8f19ef36:processor/rule/cron_scheduler_test.go:733-798` inspects the private
  `stopping` state and requires Stop to remain blocked through both action and
  tracker completion.
- `8f19ef36:processor/rule/cron_scheduler.go:401-405` clears `started`,
  `dispatch`, and `dispatchDone` but does not clear `stopping`; the same
  scheduler instance therefore rejects every later Start.

This produces an exact 41-file lifecyclejoin owner census plus one separately
identified CronScheduler coordination owner. It is not a claim that every
hand-written native lifecycle mechanism elsewhere in the repository has been
enumerated.

Current-main authority remains:

- `simplify-one-shot-lifecycle-ownership/recovery-ledger.md:14-18` assigns
  draft PR #990 zero lifecycle-migration credit.
- `recovery-ledger.md:25-35` makes simplify the sole lifecycle tracker.
- `recovery-ledger.md:157-174` retains runtime migration and proof as unchecked.
- `authority-reconciliation-inventory.md:48-90` and
  `authority-reconciliation-design.md:75-134` retain historical lifecycle
  machinery as unresolved debt.

### Monitoring and tool surfaces

The patch removes `monitor_flow` and adds
`monitor_workflow_runs(workflow_slug)`. The replacement reads existing
agent-loop records and does not depend on flowstore. It introduces no new
durable primitive, but it is an outward model-tool contract and needs its own
review rather than being credited as boot topology work.

## 3. Adjacent claims on the territory

| Claim | Evidence | Relationship |
|---|---|---|
| Simplify owns all generic lifecycle mechanics and proof | ADR-095; `simplify-one-shot-lifecycle-ownership/recovery-ledger.md:25-35` | Binding current-main authority |
| Restore owns context signature/root debt only | `authority-reconciliation-design.md:46-73` | Must not regain lifecycle mechanics |
| Require-restart owns boot composition and rules-only reload | `recovery-ledger.md:34-35` | Proper home for #990 topology work |
| Flow Engine is authoring/validation only | Owner ruling and `8f19ef36:docs/adr/096-...:19-48` | Ratified direction, not yet propagated into current-main spec truth |
| Current-main flow desired/effective lifecycle truth | `require.../design.md:150-170`; `specs/flow-activation-truth/spec.md:3-84` | Explicitly conflicts with the later owner ruling |
| Rule activation is revision-bound and observable | `require.../design.md:83-148`; `tasks.md:40-65` | Target remains substantially unimplemented |
| Sister repositories are read-only | `AGENTS.md` repository ownership rule | Downstream breaks become SemStreams migration notes only |

Historical PR #990 must not restore:

- `ManagedConsumer`;
- `DrainAndDelete`;
- `OutstandingWork` as lifecycle authority;
- Client child-consumer catalogs;
- name-routed Stop/Delete;
- automatic same-name consumer replacement;
- `Generation`/`Operation` teaching;
- concurrent/rejoin/result replay;
- repeated/concurrent Stop requirements;
- generic runtime component replacement.

## 4. Consumer at birth

No new NATS subject, KV bucket, component port, or payload kind is introduced.

Outward surfaces and present consumers:

| Surface | Present consumer |
|---|---|
| `Manager.BootConfig` | bootstrap composition root, ComponentManager, FlowService, Metrics service, and `ComponentRestartRequired`; immutable sealed boot input, but not proof of complete KV arbitration |
| `Manager.GetConfig` | FlowService override-expiry reporting and ServiceManager pending-service/restart reporting; mutable desired projection that can be stale per failed watch pattern |
| Flow validation | flow-builder/API and tool callers; validation executes real factories and can observe validation-host filesystem state or reject omitted production dependencies |
| `Manager.ComponentRestartRequired` | explicit diagram publication response |
| `POST /flows/{id}/publish-component-configs` | flow-builder/API clients intentionally publishing candidates |
| Removed flow lifecycle routes/tools | existing adopters receive compile/route/tool absence, with migration guidance |
| `monitor_workflow_runs(workflow_slug)` | model tool registry and workflow-run callers |
| Rules-only `ApplyConfigUpdate(ctx, ...)` | Rule ConfigManager reconciliation |
| Registry internal admission token | SemStreams composition and validator only; unavailable downstream |

There is no present consumer for another generic runtime topology protocol or
another lifecycle state machine.

# Same-class collision table

| Dimension | Inventory evidence |
|---|---|
| Semantic class | Composition activation, diagram authoring, Rule activation, and runtime lifecycle coordination are separate jobs currently mixed across the patch history |
| Owners | Config Manager owns desired and sealed boot config; ComponentManager owns runtime handles; Registry owns declarations; flowstore owns diagrams; Engine validates/compiles; Rule owns live Rule definitions; simplify owns generic lifecycle |
| Catalogs | Config component map, Registry factories/declarations, flow diagrams, Rule desired records, tool registry |
| Status | Component health/metrics/messages; `ComponentRestartRequired`; Rule logs/metrics in the patch; current-main target Rule activation/readiness facts |
| Lifecycle | Historical branch still uses lifecyclejoin Generation/Operation and StopWithQuiesce; simplify owns their removal and replacement proof |
| Ownership | Topology fixed at boot; runtime handles private to ComponentManager; Rule is the single bounded live-config exception; flow diagrams own no runtime |
| Readers | ComponentManager, FlowService, Engine validator, agentic flow tools, HTTP clients, model tools, operators, tests, downstream Go component/config authors |
| Writers | Config Manager APIs, flow explicit publication, Rule CRUD/KV writer, boot composition |
| Recovery | Desired configuration survives dirty shutdown; next boot re-arbitrates it; flow diagrams carry no recovery state; Rule repair/terminal truth remains target work; generic restart proof remains simplify work |

# Semantic conflict table

| Conflict | Historical branch | Current main / owner authority | Inventory finding |
|---|---|---|---|
| Generic lifecycle | Retains Generation, Operation, StopWithQuiesce, rejoin branches | ADR-095 and simplify own native one-shot lifecycle | Zero lifecycle credit; current-main lifecycle artifacts remain authoritative |
| Flow activation | ADR-096 and code make diagrams authoring-only | Current-main spec still defines deploy/start/stop/undeploy desired/effective lifecycle truth | Owner ratified ADR-096; contract must be explicitly superseded, never silently deleted |
| Rule hot reload | Global watcher, incremental mutation, log-only failure | Pack-scoped, atomic, revision-bound, observable and repairable target | Boundary work only; target tasks remain unchecked |
| Registry “generation” | Immutable declaration version named generation | Runtime Generation is removal debt | Different semantics, but naming collision can mislead reviewers and future agents |
| Flow validation | `engine/validator.go:201-297` clones live registrations and calls the real Registry creation path for every node | `component/registry.go:31-36` says factories perform no I/O, but known factories do filesystem reads, emit operational logs, allocate runtime clients/metrics, or reject missing production dependencies | “Validation-only” does not mean construction-effect-free; validation outcome can depend on validator-host files and its deliberately incomplete dependency set |
| Config shutdown | `Manager.Stop(timeout)` uses waiter goroutine and `time.After` | Lifecycle migration requires ordinary caller-owned Stop context | Existing debt carried by touched file; no credit |
| ComponentManager shutdown | Borrow fence plus lifecyclejoin quiesce/rejoin machinery | Simplify owns terminal ordering and proof | Boot topology can be assessed independently; lifecycle remains incomplete |
| Workflow monitor replacement | Replaces flow monitor inside the same broad patch | Separate tool-contract concern | Review independently; do not hide it inside topology acceptance |

### Validation-time factory effects

`8f19ef36:engine/validator.go:201-297` creates an ephemeral Registry, copies
every live registration, and calls `CreateComponent` for each diagram node with
only `NATSClient` populated in `component.Dependencies`.
`8f19ef36:component/registry.go:274-337` executes the actual registered factory
and captures its declaration. No component `Start` is called, but factory
construction is real.

Known construction effects and dependency mismatches are:

| Factory | Evidence | Validation-time effect |
|---|---|---|
| Graph Gateway | `8f19ef36:gateway/graph-gateway/component.go:299-330,425-513`; `graph/query/examples.go:24-44` | When embedding classification is enabled, factory construction performs `os.Stat`, directory globbing, and JSON file reads. It emits Warn or Info logs for missing, invalid, empty, or successfully loaded examples. Validation therefore observes the validator process filesystem and logger. |
| Agentic Governance | `8f19ef36:processor/agentic-governance/component.go:61-113`; `filter_chain.go:227-274`; `injection_classifier.go:51-101`; `injection_corpus/loader.go:129-157` | A configured `injection_classifier` causes corpus files to be opened and parsed during factory construction. Missing or invalid validation-host files make construction fail. |
| Lifecycle Gateway | `8f19ef36:gateway/lifecycle-gateway/component.go:266-320` | Factory rejects nil `LifecycleManager`. The validator supplies none, so a node that is valid under the production composition dependency set is rejected in validation. |
| Agentic Dispatch | `8f19ef36:processor/agentic-dispatch/component.go:155-228` | Factory rejects nil `ModelRegistry`. The validator supplies none. |
| Agentic Model | `8f19ef36:processor/agentic-model/component.go:101-205` | Factory rejects nil `ModelRegistry`. The validator supplies none. |
| Graph Ingest | `8f19ef36:processor/graph-ingest/component.go:621-698` | Factory constructs metric handles, readiness gauges, decoder state, maps, and timestamps. Because the validator supplies no MetricsRegistry, construction differs from production metric registration. |
| Graph Clustering | `8f19ef36:processor/graph-clustering/component.go:659-767` | Factory constructs metrics and conditionally registers semantic-tier metrics when a MetricsRegistry is present. The validator supplies none, so validation does not exercise the production constructor effect. |
| HTTP POST output | `8f19ef36:output/httppost/httppost.go:122-232` | Factory allocates an `http.Client`. The validator’s zero Security dependency avoids the optional TLS/ACME construction branch; no request is sent. |

Bounded inventory statement: this table enumerates concrete non-pure effects
and dependency collisions found by the path-directed factory inspection. It is
not proof that every other registered factory is pure. Because Validator calls
the real factory contract for every node, an unenumerated constructor effect
would also execute during validation.

# Adopter seam inventory

## Downstream component/config author

What must they know?

1. Component/service topology is fixed after boot.
2. Component configuration writes are desired next-boot state.
3. Only Rule definition content may activate live.

What happens if they do nothing?

- Removed Registry and ComponentManager mutation APIs fail compilation.
- Existing config writes can succeed while the running process remains
  unchanged; callers need typed restart-required truth where an authoring
  operation promises activation feedback.

Where do they find out?

- Compile error for removed Go APIs.
- Boot validation and typed publication response.
- Migration guide and API schema after the executable contract.

What should they have to know?

- Only that topology changes require restart and Rule definitions are the
  named exception.
- They should not know lifecyclejoin types, generations, consumer names,
  catalogs, quiesce phases, rejoin behavior, or storage grammar.

Observed seam qualification:

- An external KV author receives no typed indication that this process failed
  to watch the relevant pattern.
- If they do nothing after such a partial Start, `GetConfig`,
  `ComponentRestartRequired`, or service `restart_required` can omit their
  external write.
- Discovery is limited to a Debug watcher-creation log; no outward readiness
  or degraded record names the missing pattern.
- They should not have to know which internal watch pattern backs their config
  class, but the historical branch currently makes that hidden detail affect
  reported desired truth.

## Flow author/tool caller

What must they know?

1. A flow is a saved diagram, not a running generation.
2. CRUD does not publish component configuration.
3. Publication is explicit, upsert-only, and omission never deletes.
4. A successful publication leaves the current runtime unchanged.

What happens if they do nothing?

- Saving a diagram changes no configuration and no runtime.
- Calling removed lifecycle routes/tools fails visibly.
- Explicit publication returns persisted names, any failed name,
  `runtime_unchanged`, and `restart_required`.

Where do they find out?

- Removed routes/tool names fail immediately.
- Typed publication response and OpenAPI/tool schema.
- Migration guide.

What should they have to know?

- Save, validate, optionally publish, then restart.
- They should not calculate boot digests, infer running state, or predict
  whether an omitted node should delete configuration.

Observed seam qualification:

- Validation of Graph Gateway and injection-classifier nodes can read files
  from the validator host.
- Lifecycle Gateway, Agentic Dispatch, and Agentic Model nodes can be rejected
  because validation supplies fewer dependencies than production composition.
- If callers do nothing, otherwise identical diagrams can validate differently
  across hosts or composition contexts.
- The returned validation error is where they discover failure; the surface
  does not distinguish diagram invalidity from validation-environment
  dependency or filesystem absence.
- A flow author should not need to know validator-host paths or the validator’s
  reduced Dependency value, but those facts presently influence validation.

## Rule author/operator

What must they know in the historical patch?

- Writes under the current Rule path may be picked up by `rules.*`.
- Watcher/reconcile failure is observable only through logs.

What happens if they do nothing?

- A write can persist while live activation is silently unavailable.
- Partial application can occur before a later rule fails.
- The writer receives no exact terminal activation outcome.

Where do they find out?

- Currently logs; this is below the required correctness threshold.

What should they have to know?

- Only an opaque write receipt and its typed terminal outcome.
- They should not know KV patterns, boot identities, watcher health, or
  activation storage grammar.

Prediction check:

- `ComponentRestartRequired` observes sealed boot versus current desired
  configuration; it does not ask callers to predict.
- Flow publication reports actual persisted progress and never infers deletion.
- The current Rule surface still asks callers to predict activation from write
  success. That is an unresolved design/implementation gap.

# Context and lifecycle-directed inspection

No new production struct in the inventoried topology/flow/Rule additions stores
`context.Context`.

- Rule ConfigManager retains only a private `context.CancelFunc` and WaitGroup
  at `8f19ef36:processor/rule/kv_config_integration.go:39-56`.
- Start derives and directly passes its operation context at
  `8f19ef36:processor/rule/kv_config_integration.go:81-121`.
- Config Manager retains watcher/shutdown machinery but not a context at
  `8f19ef36:config/manager.go:21-46`.

This does not clear generic lifecycle debt. The numerous retained
lifecyclejoin owners remain governed by simplify’s repository-wide zero gates.

# Findings requiring independent inventory review

1. `BLOCKING`: A normal Git rebase can silently delete current-main
   `flow-activation-truth/spec.md` without recording the owner’s later ruling.
2. `BLOCKING`: Historical lifecycle documents and code cannot receive any
   lifecycle completion credit.
3. `BLOCKING`: Rule hot reload cannot be called complete from this branch.
4. `BLOCKING`: Validation executes real component factories. Graph Gateway and
   Agentic Governance perform validation-time filesystem reads; Graph Gateway
   also emits operational logs; Lifecycle Gateway, Agentic Dispatch, and
   Agentic Model reject dependencies that Validator does not supply.
5. `BLOCKING`: Config Manager accepts partial watcher creation without
   identifying the missing patterns. `GetConfig`, component restart reporting,
   and service restart reporting can therefore be stale while Start reports
   success.
6. `BLOCKING`: BootConfig is immutable but not demonstrated complete:
   arbitration and per-key synchronization failures are logged and ignored,
   and KV synchronization resets Services but overlays Components.
7. `BLOCKING`: The lifecyclejoin census is 41 production owner/importer files
   plus two helper implementation files. PR #990 additionally introduces
   CronScheduler `dispatchDone`/`stopping` coordination and an explicit
   concurrent-Stop join contract outside lifecyclejoin.
8. `INVENTORY LIMIT`: The factory-effect table records verified non-pure
   constructors and dependency collisions, not proof that every unlisted
   factory is pure. Validator executes any unlisted constructor effect through
   the same real factory path.
9. `REVIEW`: The workflow-run monitor replacement and E2E WebSocket client
   removal are adjacent surfaces and need explicit relevance review.
10. `REVIEW`: `componentGeneration` is declaration-version terminology, not
   runtime lifecycle, but its name collides with prohibited runtime-generation
   teaching.
11. `REVIEW`: Config Manager and ComponentManager lifecycle code remains old
   debt in heavily changed files. Tests must not convert that proximity into
   lifecycle proof credit.

# Review gate

The technical writer shall materialize this exact inventory, record:

- baseline `eb1f6d7758f75a2ff5598e2ca92af92e8c21d753`;
- historical head `8f19ef3678a549913385b090e4de1766a7a43a27`;
- the artifact SHA-256;
- independent reviewer identity and verdict;

in `simplify-one-shot-lifecycle-ownership/recovery-ledger.md`.

No target-state patch selection, rebase, conflict resolution, implementation,
or lifecycle task completion proceeds until an independent
`semstreams-reviewer` returns `INVENTORY PASS` against that exact hash.
