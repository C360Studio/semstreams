# Change: Make boot the component-composition activation boundary

## Why

SemStreams currently treats broad configuration writes as commands against the running process. ComponentManager can
watch `components.*` and `model_registry`, create and remove components, restart dependants, and replace Registry
generations. Its generic HTTP config endpoint also probes hidden component interfaces and may mutate a running
component without persisting the change.

That flexibility has not become a product requirement. `watch_config` defaults false and no shipped configuration
enables it. No production component implements the generic `UpdateConfig(ctx, json.RawMessage)` hook. Flow lifecycle
tools exist but neither shipped binary wires their runtime manager. The only shipped live-authoring path with clear UX
value is rule create/update/delete followed by Rule-processor activation.

The unused generality is expensive: it is the reason the pending lifecycle design needs replacement reservations,
borrow gates, transition states, request-to-supervisor lifetime handoff, failed-candidate policy, and exact
same-generation teardown. Those mechanisms make the framework harder to reason about and give future contributors a
large non-idiomatic surface to copy.

## What changes

- Treat service and component composition as immutable after successful boot.
- Keep config KV and rule storage as durable desired state. Keep flow storage, schemas, validation, and authoring APIs
  as diagram artifacts with no lifecycle meaning.
- Retire flow deploy/start/stop/undeploy operations. Diagram CRUD cannot change configuration. One explicit publish
  operation compiles a saved diagram and upserts desired component candidates for a later boot; it reports the current
  runtime unchanged and whether the sealed boot map differs.
- Retire ComponentManager config watching, generic live config PUT, runtime create/remove/restart/replace, and
  model-registry-triggered component restart.
- Make Registry and observation value-only. ComponentManager remains the sole runtime-handle owner and exposes only
  callback-scoped borrows for concrete in-process consumers, fenced by terminal Stop rather than live transitions.
- Retire the generic component `UpdateConfig` capability. A future live operational control must be separately named,
  typed, observable, and justified by a current consumer.
- Preserve one bounded exception: rule definitions may hot reload inside an already-running Rule processor. The Rule
  processor's ports, dependencies, watch-bucket set, integration mode, and projection bindings remain boot-only.
- Scope rule authoring to an already-composed `pack_id`; retire the ambiguous global rule namespace. Deletes use typed
  desired tombstones so every mutation has an exact receipt and deterministic restart replay.
- Make rule activation revision-bound and observable. A durable rule write is not evidence that the rule became
  active; the writer reports `pending`, and the owning Rule processor records `applied`, `rejected`, `superseded`, or
  `canceled_shutdown` for the exact desired revision.
- Use KV Watch for desired rule facts and rule-activation facts. Restart replay and fan-out are correct, application is
  fast and idempotent, and both records describe current facts rather than queued work.
- Make restart-safe shutdown a prerequisite for relying on boot activation. A controlled restart must quiesce new
  intake, drain already-accepted NATS callbacks, settle ACK/NAK and publications, cancel and join Start-owned work, and
  flush and close the NATS connection before exit.
- Preserve owner-local shutdown as a prerequisite, but supersede this change's managed-consumer lifecycle mechanics
  with ADR-095 and `simplify-one-shot-lifecycle-ownership`: retained consume constructors return the exact native
  `jetstream.ConsumeContext`, failed-Start cleanup authority is retained, and Client does not rediscover children.
- Make Client Close terminal and transport-only: it joins only Client-owned health/metrics workers, native-drains the
  connection, observes CLOSED and conservative error history, and never compensates for missing owner cleanup.
- Remove abrupt NATS consumer `Stop` and subscription `Unsubscribe` from graceful shutdown paths. Deadline-forced
  termination remains an observable failed shutdown; it cannot be reported as a clean restart boundary.
- Retire broad mutable NATS roots returned by Client/framework constructors and narrow broad injected roots before the
  breaking tag. Preserve only reviewed message/value/watcher/lister/future seams with explicit caller context and
  local Stop/completion ownership; add no `Unsafe*` compatibility alias.
- Require every controlled shutdown, clean or failed, to exit the current process. Exit status and observability name
  the result; supervision always starts the next process with a fresh Client.
- Make dirty restart correctness independent of shutdown hooks. Crash-critical work uses durable JetStream or KV,
  acknowledges only after durable effects commit, and converges safely when a crash causes redelivery.
- Make every successful boot consume the latest committed desired state regardless of whether the previous process
  drained cleanly or lost power. A clean-exit marker is observability, never an activation precondition.
- Simplify `restore-go-lifecycle-ownership`: delete its live replacement protocol and retain boot ownership, raw-handle
  retirement, terminal shutdown, context cleanup, and their race proofs.

ADR-095 and `simplify-one-shot-lifecycle-ownership` supersede PR #984's managed-consumer, lifecycle deletion,
concurrent/rejoin, and retained-result mechanics and own the complete `restart-safe-shutdown` and
`jetstream-consumer-policy` lifecycle target. PR #984 retains boot-only composition, rule hot reload, and diagram-only
flow authoring; it depends on `simplify-one-shot-lifecycle-ownership` for broad-root retirement and restart-safe
settlement/outbound-flush, controlled-process proof, dirty-recovery, durable-communication, live-storage/replica
validation, NATS restart, clean-marker independence, and latest-desired-state guarantees. No runtime or proof task is
completed by delegation.

## Capabilities

### New capability

- `rule-hot-reload`: bounded live rule-definition activation and revision-bound outcome truth.
- `flow-diagram-authoring`: durable diagram CRUD, validation, compilation, explicit candidate publication, and
  name-keyed observations without lifecycle authority.

### Delegated dependency capability

- `restart-safe-shutdown`: ADR-095 and `simplify-one-shot-lifecycle-ownership` own the raw-root and restart-safe
  guarantees that PR #984 consumes as prerequisites for its boot, rule, and flow scope.

### Modified capabilities

- `component-runtime-config`: desired component configuration is next-boot state; generic live apply and generation
  replacement are retired.
- `service-composition`: all service and component composition is sealed at boot, with the dedicated Rule exception.
- `component-discovery`: Registry admission is boot-owned and has no live replacement/removal protocol.
- `framework-composition`: the component-start barrier consumes one boot snapshot and has no late boot-drain or
  post-boot dynamic Start path.
- `graph-index-readiness`: Rule readiness gains boot-incarnation identity and remains the sole Rule liveness fact;
  configured entity-watch membership becomes boot-only.
- `rule-entity-watching`: watcher generation replacement repairs the same boot-authoritative watcher after transport
  loss and cannot apply a configured pattern-set change.

## Impact

- **Breaking API and behavior:** ComponentManager live mutation methods, Registry replacement, generic config PUT, and
  every flow lifecycle route, field, stream, and tool are retired without compatibility shims.
- **Preserved authoring:** flow diagrams and rule definitions remain durable and validated. Diagram CRUD has no config
  effect; explicit diagram publication writes upsert-only desired component candidates for a later boot.
- **Flow truth:** persisted flows are diagrams, not desired or effective activation records. Retained observations use
  diagram component names only and never assert ownership or activation.
- **Lifecycle:** ComponentManager owns one boot generation per admitted component and terminal shutdown. It no longer
  coordinates incumbent/candidate replacement.
- **Restart safety:** the process signal path must preserve runtime authority until lifecycle owners quiesce and drain.
  A graceful shutdown never silently converts native NATS drain into abrupt stop/unsubscribe, and Client Close alone
  never certifies owner callback settlement.
- **Crash safety:** power loss runs no cleanup. Durable work remains recoverable and at-least-once redelivery converges;
  core NATS is not used for work whose loss would violate restart correctness.
- **Observability:** rule writers receive a desired revision and can observe the terminal activation outcome for that
  revision through typed rule reads, without knowing operational KV grammar. Watcher/status failure degrades Rule
  readiness. Diagram publishers receive exact persistence progress, runtime-unchanged truth, and a boot-map comparison.
- **Deployment identity:** Rule hot reload requires a validated, stable `platform.instance_id`. Concurrent ownership of
  one process-slot/component/pack readiness key fails admission through compare-and-set rather than overwriting truth.
- **Migration:** sister repositories remain read-only. Migration documentation names removed APIs and new response
  semantics, exact owner-handle adoption, `ConsumeDurable` retirement, and broad-root narrowing; downstream teams
  update their own repositories. ADR-070 remains historical decision context rather than being rewritten.
- **Release:** restart-safe shutdown is a prerequisite, not a follow-up. This pre-v1 breaking work requires controlled
  SIGTERM and SIGKILL restart evidence plus relevant core, structural, agentic, CRUD, and semantic E2E before the
  breaking commit lands.
