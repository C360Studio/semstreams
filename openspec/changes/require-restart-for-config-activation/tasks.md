## 1. Contract and prerequisite truth

- [x] 1.1 Obtain SemStreams architect and owner approval for boot-only composition plus the dedicated rule-definition
  exception.
- [x] 1.2 Add ADR-094 and mark only ADR-026's live flow-activation decision superseded; retain coordinator judgment.
- [x] 1.3 Add current-truth deltas for component runtime config, service composition, component discovery, rule hot
  reload, graph index readiness, and rule entity watching. Replace stale Purpose text that advertises live component
  or configured watcher-set mutation.
- [x] 1.4 Revise `restore-go-lifecycle-ownership` P2/P3 target and tasks after this prerequisite is approved.
- [x] 1.5 Run strict OpenSpec validation after every contract edit.

## 2. Restart-safe shutdown reset and ordered delivery

The six PRs below are dependency ordered. A later PR SHALL NOT land before its predecessor. Only the contract reset is
complete; every runtime migration and proof remains unchecked.

ADR-095 and `simplify-one-shot-lifecycle-ownership` supersede PR #984's managed-consumer, lifecycle deletion,
concurrent/rejoin, and retained-result mechanics and own the complete `restart-safe-shutdown` and
`jetstream-consumer-policy` lifecycle target. This change retains boot-only composition and depends on the new change's
broad-root retirement, settlement/outbound-flush, controlled-process proof, dirty-recovery, durable-communication,
live-storage/replica validation, NATS restart, clean-marker independence, and latest-desired-state guarantees. No
runtime or proof task is completed by delegation.

- [x] 2.1 **PR2-reset-contract.** Replace the Client-wide child-ledger contract with the approved owner-local exact
  handle design; record the reset inventory, native-root disposition, `ConsumeDurable` retirement, terminal
  transport-only Close, always-exit controlled restart, and migration sequence. Preserve the exact approved native
  census at `openspec/changes/require-restart-for-config-activation/native-surface-inventory.md` with SHA-256
  `d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`. Run strict OpenSpec validation only.
- [ ] 2.2 **SUPERSEDED — tracked by `simplify-one-shot-lifecycle-ownership`.** The former PR2-owner-handles task would
  return `*ManagedConsumer` from
  `ConsumeStreamWithConfig`, `ConsumeStreamWithConfigContexts`, and `ConsumeInternalStreamWithConfig`; update every
  interface, mock, test, and caller; retire zero-production-consumer `ConsumeDurable`; simplify core Subscription to
  exact-handle Drain; and migrate every in-repo owner to quiesce, Drain/DrainAndDelete, wait authoritative closure,
  settle accepted callbacks, then cancel and join Start-owned work. Keep catalogs only as temporary migration
  scaffolding so partial acquisition, duplicate cleanup, or stale ownership cannot lose an owner. Add handle-local
  `ManagedConsumer.OutstandingWork(ctx)`; migrate exactly the two callsites in
  `processor/graph-ingest/readiness.go` and the one callsite in `processor/agentic-loop/inflight.go` to the handles
  their owners retain; retire `Client.OutstandingWork(stream,name)` without an alias.
- [ ] 2.3 **SUPERSEDED — tracked by `simplify-one-shot-lifecycle-ownership`.** The former PR2-client-minimal task would
  remove Client child catalogs,
  name-routed Stop/Delete, setup/delete reservations, generations, admission gates, readiness latches, publisher
  convergence, and forced child cleanup. Make Connect synchronous with private `nats.FlusherTimeout(5s)` and no knob.
  Make Close terminal transport-only: reject later work; cancel/join health and metrics; native-drain and observe
  CLOSED; classify preclosed transport and any historical/terminal `LastError` as non-clean; force close on caller
  expiry without reporting clean; retain one result for repeated Close. Expose no Subscription Abort or Unsubscribe.
- [ ] 2.4 **SUPERSEDED — tracked by `simplify-one-shot-lifecycle-ownership`.** Execute every RETIRE/NARROW row in the
  approved native-surface inventory. Remove
  broad mutable roots returned by Client/framework constructors, narrow broad injected roots to measured local method
  sets, and preserve only reviewed message/value/watcher/lister/future seams with caller context and local
  Stop/completion ownership. Add no `Unsafe*` alias and edit no sister repository.
- [ ] 2.5 **SUPERSEDED — tracked by `simplify-one-shot-lifecycle-ownership`.** The former PR2-composition-proof task
  would separate controlled signal receipt from Start-context cancellation. Use one fresh
  bounded shutdown context; stop admission owners; drain exact handles while accepted-work authority remains live;
  aggregate every owner Stop result; then call Client Close. Prove ACK-after-commit, unfinished-work redelivery,
  publisher flush, repeated Stop/Close, and signal races. Add a real-process SIGTERM known answer proving that clean
  shutdown exits zero, failed owner/transport shutdown exits nonzero, both processes terminate, and supervision boots
  a fresh process and Client with latest desired configuration. A clean marker remains observability, never an
  activation gate.
- [ ] 2.6 **SUPERSEDED — tracked by `simplify-one-shot-lifecycle-ownership`.** The former PR2-dirty-proof task would
  inventory every crash-critical communication path and move any core-NATS-only critical
  work/fact to durable JetStream or KV using the canonical four-test decision. Require file-backed live resources and
  verify declared replica policy. Add deterministic real-process SIGKILL tests after delivery, durable effect,
  publication, and before ACK; kill SemStreams and its isolated NATS server without drain; restart from the same file
  store; and prove retained-state redelivery, idempotent convergence, latest desired-state recovery, and honest
  external-effect limits. Do not touch Docker resources outside the test project's namespace.

## 3. Retire generic runtime composition mutation

- [ ] 3.1 Remove ComponentManager config/model-registry subscribers and all live reconcile/restart/replace/remove paths.
- [ ] 3.2 Remove generic ComponentManager config PUT and anonymous live-update probes; retain value-only observation.
- [ ] 3.3 Delete Registry replacement/reservation APIs and keep boot admission plus defensive value views.
- [ ] 3.4 Remove `watch_config` and other operator surfaces that imply live component mutation.
- [ ] 3.5 Keep config KV desired-state synchronization but prove post-boot writes cannot mutate running services or
  components.
- [ ] 3.6 Preserve terminal Start-owned cancellation, exact Start joins, same-generation Stop, and idempotent shutdown.
- [ ] 3.7 Classify every production post-boot `config.Manager.GetConfig()` read. Desired-state reporting may read the
  mutable desired view; boot construction uses the selected snapshot; runtime behavior and resource recovery use only
  the sealed boot snapshot.
- [ ] 3.8 Document and test that a callback borrow cannot synchronously request terminal Stop for its own instance.
  Inventory production callbacks for reentrant lifecycle calls and keep manager locks out of borrow drain.

## 4. Preserve flow authoring without flow lifecycle

- [ ] 4.1 Retain diagram CRUD, audit, CAS update, import from component configs, validation, validator metrics, and
  compilation. Remove every persisted or returned flow lifecycle, provenance, component-bundle, and restart field.
- [ ] 4.2 Add explicit deterministic upsert-only `publish-component-configs`. Return exact partial progress, unchanged
  runtime truth, and component-map restart comparison; never infer deletion from a diagram omission.
- [ ] 4.3 Seal a defensive post-arbitration component map at Config Manager Start. Prove six rapid writes converge in
  KV and SafeConfig, cannot mutate the current component registry/start count, and are selected once by a fresh boot.
- [ ] 4.4 Remove deployment, status-stream, flow-log, and lifecycle-tool surfaces without aliases. Rename retained
  saved-diagram observations truthfully and replace flow monitoring with workflow-run aggregation by workflow slug.

## 5. Dedicated rule-definition hot reload

- [ ] 5.1 Replace global `rules.<rule_id>` values with pack-scoped typed desired records. Create/update write `present`;
  delete writes a revision-returning `deleted` tombstone. Return an opaque pack/rule/revision receipt.
- [ ] 5.2 Restrict live payloads to rule definitions. Reject component envelope, watch-bucket, integration, port,
  dependency, producer-identity, and projection-binding changes from the hot path.
- [ ] 5.3 Move the watcher and reconciler under a Start-context supervisor with contexts passed as goroutine parameters;
  retain only private cancellation and join state.
- [ ] 5.4 Build and validate a complete candidate rule generation before commit; rejection leaves the active generation
  unchanged.
- [ ] 5.5 Publish revision-bound `applied`, `rejected`, `superseded`, or `canceled_shutdown` activation facts and an
  active-generation fact scoped by unique boot incarnation in a cataloged bounded operational KV bucket.
- [ ] 5.6 Return the exact desired revision from create/update/delete. Report `pending` until the owning processor
  proves a terminal outcome; never infer activation from write success.
- [ ] 5.7 Prove expression and cron add/update/delete, invalid-set rejection, burst supersession, restart replay,
  multiple processor instances, and cancellation/Stop races deterministically.
- [ ] 5.8 Add a typed activation reader used by rule mutation responses, `get_rule`, `list_rules`, and a dedicated
  status operation; callers never provide storage grammar.
- [ ] 5.9 Make watcher/reconcile/status-publication failures degrade readiness and metrics, then repair through
  Start-owned bounded retry plus full-snapshot reconciliation.
- [ ] 5.10 Join active-generation facts to Rule's existing `GRAPH_STATUS` liveness by exact envelope `boot_id`; typed
  reads label expired or different boot incarnations stale and never report a crashed generation currently active.
- [ ] 5.11 Use one status record per boot/component/pack/rule with KV history five; GC expired boot status after the
  `GRAPH_STATUS` freshness grace, retain at most five boot incarnations per stable process/component slot, and return
  typed
  unknown/history-expired results instead of promoting stale evidence.
- [ ] 5.12 Back in-process tool executors with an admitted typed Go activation reader. Expose remote web reads through
  a schema-defined operation on the existing GraphQL-shaped HTTP facade. Do not expose operational KV grammar or add
  an MCP hop for in-process tools.
- [ ] 5.13 Use one stable framework-owned Rule `GRAPH_STATUS` key per process slot/component/pack with History 3. Put
  unique `boot_id` in the envelope and derive process slot from validated boot-sealed `platform.instance_id`. Claim the
  slot with KV compare-and-set, discover Rule keys from sealed composition, and leave every non-Rule producer key
  unchanged. Do not create a second liveness catalog or accumulating per-boot readiness keys.

## 6. Migration and verification

- [ ] 6.1 Update SemStreams migration docs with exact removed APIs and response changes; make sister-repository notices
  read-only. Document required stable `platform.instance_id` for Rule hot reload and typed collision failure.
- [ ] 6.2 Remove tests that assert retired live component replacement and replace them with sealed-runtime tests.
- [ ] 6.3 Run `task lint`, `go test -race ./...`, integration tests, contract tests, schema generation/no drift, and
  strict OpenSpec validation.
- [ ] 6.4 Run relevant `task e2e:core`, `task e2e:structural`, CRUD, agentic, and semantic tiers before the breaking
  commit lands.
- [ ] 6.5 Record exact commit, test, controlled/dirty restart, and E2E artifacts before tagging the breaking release.
