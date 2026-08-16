## 1. Contract and prerequisite truth

- [x] 1.1 Obtain SemStreams architect and owner approval for boot-only composition plus the dedicated rule-definition
  exception.
- [x] 1.2 Add ADR-094 and mark only ADR-026's live flow-activation decision superseded; retain coordinator judgment.
- [x] 1.3 Add current-truth deltas for component runtime config, service composition, component discovery, rule hot
  reload, graph index readiness, and rule entity watching. Replace stale Purpose text that advertises live component
  or configured watcher-set mutation.
- [x] 1.4 Revise `restore-go-lifecycle-ownership` P2/P3 target and tasks after this prerequisite is approved.
- [x] 1.5 Run strict OpenSpec validation after every contract edit.

## 2. Prove restart-safe shutdown

- [ ] 2.1 Inventory every production NATS consumer, core subscription, watcher, publisher, and connection owner; name
  its graceful primitive, accepted-work boundary, settlement rule, and join proof.
- [ ] 2.2 Separate controlled signal receipt from runtime Start-context cancellation so SIGTERM/SIGINT initiates
  bounded quiesce before cancellation.
- [ ] 2.3 Replace graceful JetStream `ConsumeContext.Stop` and core-NATS `Unsubscribe` with native drain plus
  authoritative closure waiting. Preserve abrupt operations only for typed deadline-forced failure.
- [ ] 2.4 Prove component Stop closes admission, drains accepted callbacks with a live work context, settles durable
  ACK/NAK and required publications, then cancels and joins remaining Start-owned work.
- [ ] 2.5 Make `natsclient.Client.Close(ctx)` rejoin all remaining consumer/subscription drains before connection drain
  and close; surface every incomplete phase without detached cleanup.
- [ ] 2.6 Add deterministic unit and integration tests for drain completion, deadline/rejoin, ACK-after-commit,
  unfinished-work redelivery, publisher flush, repeated Stop, and signal/Stop races.
- [ ] 2.7 Add a real-process SIGTERM/restart E2E known answer against retained NATS state. Require its clean-shutdown
  proof as a release gate, but never use a clean-exit marker as a runtime gate on committed desired configuration.
- [ ] 2.8 Inventory every crash-critical communication path and move any core-NATS-only work/fact path to durable
  JetStream or KV according to the canonical four-test decision.
- [ ] 2.9 Require file-backed live storage and declared replica-policy verification for desired config, activation
  status, authoritative facts, and crash-critical work; fail boot on incompatible memory-backed resources.
- [ ] 2.10 Add deterministic real-process SIGKILL tests after delivery, durable effect, publication, and before ACK;
  prove retained-state redelivery, idempotent convergence, desired-config recovery, and honest external-effect limits.
- [ ] 2.11 Kill SemStreams and its isolated NATS server without drain, restart NATS from the same file store, and prove
  recovery. Do not touch Docker resources outside the test project's namespace.
- [ ] 2.12 Prove every successful boot consumes the latest committed desired state after both clean and dirty exits;
  clean-exit evidence is observability and never an activation gate.

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

## 4. Preserve desired flow authoring

- [ ] 4.1 Keep flow create/update/validation and desired component-config persistence.
- [ ] 4.2 Make deploy/start/stop/undeploy responses state desired-state outcome, runtime unchanged, and restart
  required.
- [ ] 4.3 Add restart-known-answer tests: a desired flow change does not affect the current runtime and does affect the
  next successful boot after both graceful exit and power loss.
- [ ] 4.4 Remove or keep unwired any runtime-lifecycle tool surface with no present consumer; do not advertise immediate
  activation.
- [ ] 4.5 Replace persisted `runtime_state` with desired `absent`/`disabled`/`enabled` activation and migrate the
  current not-deployed/deployed-stopped/running API without aliases.
- [ ] 4.6 Remove or rename timestamps and metrics that currently claim a desired write deployed, started, stopped, or
  ran a flow.
- [ ] 4.7 Make flow reads and `monitor_flow` return desired state, independently observed effective state, and
  `restart_required`; effective state never comes from flowstore and reports `unknown` without an observer.
- [ ] 4.8 Seal unique boot identity plus canonical boot-applied configuration digests. Derive `restart_required` by
  comparing current desired provenance with boot-applied provenance, independently of activation labels and health.

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
