# Runtime configuration and activation inventory

Inventory baseline: exact `v1.0.0-beta.161` candidate
`9d0ff67f377ea3dd82dca2f3bf614871c0100766`.

## Surface inventory

### Current mutation entry points

- `config.Manager` watches `services.*`, `components.*`, `platform`, `nats`, and `model_registry` in
  `config/manager.go:283-328`. Its exported desired-state writes are at `config/manager.go:629-816`, and external KV
  revisions are accepted at `config/manager.go:405-478`.
- Only `components.*` and `model_registry` have production ComponentManager subscribers
  (`service/component_manager.go:203-221`). There is no production `services.*` subscriber.
- ComponentManager live update, reconcile, restart, replacement, and removal occupy
  `service/component_manager.go:1240-1504` and `service/component_manager.go:1627-1995`.
- Registry exports construction, replacement, and unregister surfaces at `component/registry.go:256-276`,
  `component/registry.go:370-402`, and `component/registry.go:530-550`. `ReplaceComponent` has one production caller,
  the ComponentManager live recreate path at `service/component_manager.go:1846`.
- ComponentManager registers GET/PUT component config at `service/component_manager_http.go:70-75` and implements it
  at `service/component_manager_http.go:559-822`. PUT is transient and explicitly does not persist across restart
  (`service/component_manager_http.go:729-756`).
- No production component implements the probed `UpdateConfig(ctx, json.RawMessage)` hook. The only production match
  for the alternate validation/application pair is Rule processor
  (`processor/rule/config_validation.go:16-77`, `processor/rule/runtime_config.go:13-252`).
- Rule tools are wired in both binaries (`cmd/semstreams/main.go:214-226`, `cmd/e2e-semstreams/main.go:184-196`). They
  write `rules.*` through `processor/agentic-tools/executors/rules.go:165-245`; Rule processor watches and applies
  through `processor/rule/processor.go:971-1028`.
- Rule desired keys are global `rules.<rule_id>` rather than `pack_id` scoped
  (`processor/rule/kv_config_integration.go:286-313`), even though boot composition requires distinct enabled pack IDs
  (`service/rule_pack_bind.go:70-99`). The same rule ID can therefore address multiple replicas/packs ambiguously.
- `SaveRule` discards the revision returned by KV Put and `DeleteRule` calls the error-only KV Delete API
  (`processor/rule/kv_config_integration.go:286-313`). Exact delete receipts do not exist on the current surface.
- Hot reload can silently retire itself: initialization/start failure logs and returns
  (`processor/rule/processor.go:1010-1028`), watcher creation failure returns nil as file-only operation, and unexpected
  watcher closure ends the goroutine without readiness repair (`processor/rule/kv_config_integration.go:76-174`).
- No typed activation reader exists. `get_rule` and `list_rules` read desired definitions only
  (`processor/agentic-tools/executors/rules.go:73-112,165-245`); no shipped API consumes a terminal activation record.
- FlowService exposes runtime deployment operations at `service/flow_service.go:269-280` and
  `service/flow_service.go:748-763`. Engine persists the corresponding desired component configurations at
  `engine/engine.go:89-235` and `engine/engine.go:306-468`.
- Flow lifecycle agent tools exist, but neither shipped binary supplies `FlowEngineManager`; they have no shipped
  runtime consumer.
- Engine persists `RuntimeState=deployed_stopped/running` immediately after desired config writes
  (`engine/engine.go:89-235`). Flowstore names that field runtime truth (`flowstore/flow.go:20-29,63-75`), and
  `monitor_flow` repeats it as runtime state (`processor/agentic-tools/flow_monitor_executor.go:200-247`) without a
  running-process observer.
- `watch_config` defaults false (`service/component_manager_config.go:8-31`), and the shipped configuration census
  contains no true value.

### Current shutdown and dirty-restart boundary

- The binary passes a signal-cancelled context into `Manager.StartAll` (`cmd/semstreams/main.go:512-548`). SIGTERM or
  SIGINT therefore cancels runtime work before `StopAll` receives its fresh bounded shutdown context.
- ComponentManager terminal Stop cancels every component generation before calling per-component Stop
  (`service/component_manager.go:867-956`). A NATS callback whose context descends from Start can be cancelled before
  its owner invokes native drain.
- The managed JetStream primitive is correct in isolation: `natsclient.Client.StopConsumer` calls
  `ConsumeContext.Drain`, waits for `Closed`, and lets later callers rejoin the same drain
  (`natsclient/stream.go:689-723`; `natsclient/consumer_stop_test.go:38-139`).
- Client-wide Close bypasses that primitive. It calls `ConsumeContext.Stop` for remaining consumers, calls core NATS
  `Unsubscribe`, and only then drains the connection (`natsclient/client.go:562-603,765-841`). NATS documents Stop and
  Unsubscribe as discarding buffered delivery; Drain finishes buffered callbacks.
- Production graceful paths still call `Unsubscribe` in natsclient, message logger, file/HTTP outputs, graph ingest,
  graph query, and graph index/embedding/clustering processors. A complete callsite/ownership table is an implementation
  prerequisite, not a search-and-replace assumption.
- Power loss executes none of these cleanup paths. Crash correctness therefore depends on durable JetStream/KV,
  effect-before-ACK ordering, and idempotent or deduplicated redelivery. Core NATS alone cannot carry work or state
  whose loss violates restart correctness.

### Production `GetConfig()` reader classification

The literal production callsites divide into three contracts. Post-boot runtime behavior must never re-read mutable
desired configuration and accidentally activate it:

| Caller | Classification | Required target |
|---|---|---|
| `internal/bootstrapobservability/bootstrap.go:191` | boot-only construction | return the boot snapshot |
| `service/metrics.go:79` | boot-only construction | read security from the selected boot snapshot |
| `service/component_manager.go:209,314,759,2193` | boot construction | use sealed snapshot; remove reconcile |
| `service/flow_service.go:177` | boot-only construction | seed the default flow from the boot snapshot |
| `service/service_manager.go:1378` | desired reporting | compare mutable desired with sealed boot services |
| `service/flow_service.go:997` | desired reporting | report expiry of mutable desired stream overrides only |
| `engine/engine.go:310,353,379,417,453` | desired authoring | atomically mutate and persist desired state only |
| `service/flow_runtime_stream.go:760` | runtime resource recovery | use the sealed boot snapshot |

`service/flow_runtime_stream.go:760` is the concrete failure example: stream auto-create recovery currently re-reads
the latest desired `cfg.streams` declaration. A post-boot edit could therefore change a resource-recovery decision in
the sealed process even though composition is declared boot-only. The implementation inventory must also follow
aliases/helpers that receive `SafeConfig` or `*config.Config`; this literal census is the minimum, not permission for a
mutable desired view to leak through an indirect caller.

### Existing claims on the territory

- `openspec/specs/component-runtime-config/spec.md` currently requires generic hot apply, runtime add/remove,
  reconcile, restart, replacement, and removal.
- `openspec/specs/service-composition/spec.md` already makes service composition next-boot-only but explicitly leaves
  component hot update enabled.
- `openspec/specs/graph-index-readiness/spec.md` already owns Rule readiness/liveness in `GRAPH_STATUS`, including the
  three-heartbeat freshness/unknown contract, but currently assumes a runtime configuration edit may add a watcher.
- `openspec/specs/rule-entity-watching/spec.md` currently requires atomic dynamic replacement of a requested watcher
  set. ADR-094 narrows replacement to transport repair of the same boot-authoritative watcher identity.
- `docs/adr/026-coordinator-agent-dynamic-flow-composition.md` treats dynamic flow/rule activation as coordinator
  reach. The coordinator judgment role remains valid; generalized live flow activation is superseded by ADR-094.
- `openspec/changes/restore-go-lifecycle-ownership/tasks.md:31-53` assumes the live replacement protocol that this
  change removes.

### Current consumers at birth

- Durable flow authoring is consumed by FlowService and its HTTP clients.
- Durable rule authoring and live rule activation are consumed by shipped rule tools and the CRUD E2E path.
- Generic `UpdateConfig` has zero production implementers.
- Registry replacement has one production caller and exists only for ComponentManager live recreate.
- Runtime flow lifecycle tools have no shipped binary wiring.

## Adopter seam inventory

### Rule author or in-process tool caller

- **Must know today:** global rule IDs can ambiguously address multiple packs, and write success does not prove
  activation.
- **If they do nothing:** a tool can report success while the watcher is degraded or a different processor owns the
  rule.
- **Where they find out today:** Rule processor source, config-key grammar, and logs.
- **Target knowledge:** choose the already-composed `pack_id`, retain the opaque receipt, and read typed activation
  truth. The framework owns storage grammar, boot identity, liveness, and supersession.

### Flow UI operator

- **Must know today:** deploy/start/stop responses and `runtime_state` can describe desired writes as running process
  transitions.
- **If they do nothing:** the UI can display a flow as running although the sealed runtime never changed.
- **Where they find out today:** HTTP responses, flowstore fields, and historical ADRs disagree.
- **Target knowledge:** authoring remains live, component/topology activation occurs on the next successful boot, and a
  typed read exposes desired provenance, effective provenance, health, and `restart_required` separately.

### Deployment supervisor

- **Must know today:** SIGTERM pre-cancels Start work, several owners unsubscribe rather than drain, and power loss runs
  no cleanup.
- **If they do nothing:** a planned restart may lose accepted callbacks, while a dirty restart may redeliver effects or
  lose core-NATS-only work.
- **Where they find out today:** scattered Stop implementations and transport-specific behavior.
- **Target knowledge:** a clean exit is an explicit bounded result; a dirty restart consumes retained desired state and
  durable work without needing prior cleanup. The supervisor does not decide activation from an exit marker. A
  deployment enabling Rule hot reload supplies one stable, unique `platform.instance_id`; collision fails admission
  rather than silently sharing readiness truth.

### External-output author

- **Must know today:** crash timing can repeat an effect after it commits but before its NATS ACK.
- **If they do nothing:** the external system can receive a duplicate that SemStreams cannot transactionally erase.
- **Where they find out today:** individual component behavior, if documented.
- **Target knowledge:** the output contract states at-least-once behavior and supplies stable idempotency evidence where
  supported. The framework never advertises cross-system exactly once without a transactional boundary.

Across all four seams, the framework owns the desired/effective distinction and boot/incarnation provenance. An
adopter never predicts whether a field is declaration-neutral, whether KV propagated, or whether stale status is live.

## Options considered

### A. Boot-only composition with dedicated rule hot reload — selected

This deletes unused component-generation replacement while preserving the demonstrated rule UX. The Rule exception is
bounded by fixed component topology and revision-bound activation truth.

### B. Keep generic component-local UpdateConfig

Rejected. It has zero production implementers, leaves persistence and declaration-classification ambiguity, and makes
the adopter predict whether a hidden hook exists.

### C. Keep full component add/remove/replace

Rejected. It preserves dormant behavior at the cost of replacement reservations, borrow gates, transition states,
request/supervisor lifetime transfer, failed-candidate policy, and a large race matrix.

### D. Do nothing

Rejected. Current APIs continue conflating persisted intent, running truth, and lifecycle commands. Restart becomes an
untrustworthy activation mechanism, and power-loss recovery remains unproved.

### E. Make rule definitions boot-only too

Rejected for now. It yields the smallest lifecycle surface but removes the demonstrated high-value expression/cron
editing UX. The selected exception remains acceptable only while it stays Definition-only, atomic, revision-bound,
boot-incarnation-scoped, and observable. If those constraints require generalized component lifecycle machinery, this
option becomes the fallback.
