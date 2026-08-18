# Design: Boot-only composition with bounded rule activation

## Context

SemStreams configuration currently conflates three different facts:

1. desired configuration persisted for a future boot;
2. effective configuration of the running process;
3. commands to mutate that process.

This design separates them. Desired state remains writable. Effective component and service composition is sealed at
boot. Live rule-definition activation remains as a dedicated capability because it has a demonstrated UX consumer and
does not require component generation replacement.

Approval provenance: D10 and its delivery order derive from the owner-approved minimal lifecycle design artifact with
SHA-256 `0047789823bd8a6b3f772bb598fae90ca6060a8799cd828a95487589e0a7a11e`. The measured reset inventory is
`openspec/changes/require-restart-for-config-activation/inventory.md`; the exact native-surface companion is
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md` with approved SHA-256
`d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`.

## Goals

- Preserve flow/rule authoring, validation, persistence, and boot-time assembly.
- Remove general in-process component/service/topology mutation.
- Preserve live expression and cron rule-definition changes inside a fixed Rule processor envelope.
- Make activation claims revision-bound and observable.
- Make clean, loss-aware shutdown and restart the required activation mechanism for next-boot state.
- Remove lifecycle machinery whose only consumer is hot component replacement.

## Non-goals

- Automatic process restart.
- Live model-registry, credential, port, dependency, watch-bucket, integration-mode, or projection-binding changes.
- A generic component configuration hook.
- Compatibility aliases for retired beta APIs.

## Decisions

### D1. Successful boot seals service and component composition

ComponentManager constructs and starts the validated boot set. After Start succeeds, config changes cannot create,
remove, restart, or replace a running component. Registry admits declarations during boot and exposes defensive value
views; it has no live replacement protocol.

Registry, flow graph, dependency records, lifecycle records, and observation DTOs expose no runtime handles.
ComponentManager owns concrete handles and permits only callback-scoped access where a remaining in-process consumer
requires it. Terminal Stop fences and drains those borrows; there are no replacement/removal transition gates.

Terminal Stop fences runtime access and invokes each fully started component's `Stop(ctx)` while its Start authority
remains live so the component can quiesce and drain. It then cancels and joins remaining Start-owned work and records
the exact boot generation's terminal result. An in-flight Start that never reached admission uses the separate
partial-start rollback path: cancel, join Start finalization, then clean partial acquisitions. Removing live replacement
does not weaken shutdown ownership or allow Start and Stop method bodies to overlap.

### D2. Config KV remains durable desired state

Config KV remains authoritative for next-boot configuration after existing version arbitration. FlowService and other
authoring paths may validate and persist desired changes while a process is running. They must not mutate the sealed
runtime.

An accepted flow mutation returns an honest pending-next-boot result: desired state changed, current runtime unchanged,
restart required. SemStreams does not automatically exit or restart; process supervision is deployment policy.

Every successful boot consumes the latest committed desired state regardless of how the prior process exited. Planned
activation uses the graceful-shutdown protocol in D9; a dirty restart uses crash recovery. A prior clean-exit record is
useful operational evidence but never a gate that can suppress committed desired configuration.

### D3. Retire generic live component configuration

ComponentManager PUT and its anonymous interface probes are removed. No current production `UpdateConfig` implementer
is lost. A future live operational control requires a separately named contract, a current consumer, explicit
durability, and observable success/failure. It cannot re-enter through generic component config.

### D4. Rule definitions are the only live configuration exception

The fixed Rule processor may activate create/update/delete changes to rule definitions, including expression and cron
definitions. Its component configuration remains boot-only: ports, dependencies, entity-watch buckets, graph
integration, producer identity, and projection contract/index bindings cannot hot change.

The Rule processor owns a Start-context supervisor. KV watcher and application goroutines receive that context as a
goroutine parameter. No context is stored on a struct or recovered from a retained closure. A candidate rule set is
validated and built before the active rule generation changes; rejection leaves the active generation unchanged.

### D5. Desired rules and activation outcomes use KV Watch

The four storage tests all select KV:

| Test | Desired rules | Activation outcome |
|---|---|---|
| Restart | replay current definitions | replay current outcome |
| Delivery | all Rule processors react | tools/operators may all observe |
| Work | fast, idempotent reconciliation | fast fact publication |
| Nature | desired-state fact | activation-state fact |

Rule authoring targets one boot-composed `pack_id`. Desired keys are pack-scoped, and their typed values represent a
present definition or deletion tombstone. Create, update, and delete therefore all produce an exact committed KV
revision without post-delete inference. The caller receives an opaque `(pack_id, rule_id, revision)` receipt.

The owning Rule processor publishes a terminal activation outcome for each observed desired revision: `applied`,
`rejected`, `superseded`, or `canceled_shutdown`. A status also identifies the active rule-set generation. `pending`
belongs to the writer-side response before a terminal processor outcome exists; write success alone never produces
`applied`.

Activation status is processor-instance scoped by a freshly generated `boot_id` plus stable process-slot and Rule
component identity. A `boot_id` is never reused after a restart, including a dirty restart, so multiple Rule processors
and successive boots cannot overwrite one another's truth. Status records repeat `boot_id`; an old incarnation is
never current activation evidence for a new process.

Rule's existing readiness envelope in `GRAPH_STATUS` is the one liveness home. `process_slot` is the validated,
non-empty `platform.instance_id` sealed into the boot snapshot. Rule hot-reload admission fails if that identity is
absent. Its framework-owned key is stable per `(process_slot, component_id, pack_id)` and preserves the bucket's
History 3 contract. The envelope adds the unique `boot_id` and repeats the stable identities. A new boot overwrites the
stable slot; it does not create an accumulating per-boot key. Non-Rule readiness producers retain their existing fixed
keys and consumers unchanged.

Rule claims a missing or expired stable slot with KV compare-and-set. A fresh slot owned by another `boot_id` is a
typed `readiness_slot_collision`; the new processor does not overwrite it or claim hot reload ready. Heartbeats update
the claimed revision with compare-and-set, so losing slot ownership degrades Rule readiness rather than creating two
live owners. Gateway/readiness consumers derive Rule keys from sealed composition; existing raw `readiness_keys`
remain only for unchanged non-Rule producers.

The activation reader joins activation facts with the exact `boot_id` carried by the fresh Rule readiness envelope and
uses the existing consumer-local three-heartbeat freshness rule. A fresh ready/degraded Rule incarnation is live; an
explicit stopping/tombstoned or expired incarnation is historical. If the readiness fact cannot be read or freshness
is indeterminate, current activation is `unknown`; the reader does not promote history. Clean Stop publishes terminal
activation truth before making the readiness instance not current. Dirty shutdown relies on heartbeat expiry.

Status uses one current record per `(boot_id, component_id, pack_id, rule_id)` with KV history fixed at five revisions.
Framework GC purges status keys for expired boot incarnations after the `GRAPH_STATUS` freshness grace period and
retains at most the five most recent boot incarnations per stable process/component slot. A receipt outside retained
history returns typed
`history_expired` unless its newer desired record proves `superseded`; it is never guessed applied or rejected. This
bounded policy and its constants are framework-owned, not adopter-provided knobs.

A typed activation reader is part of the capability, not a follow-up. In-process rule tool executors call its admitted,
operation-specific Go interface directly. A remote web UI uses a schema-defined operation on the existing
GraphQL-shaped HTTP facade, backed by the same reader. Neither path exposes bucket grammar. MCP is reserved for an
admitted external agent client; the existing in-process executors do not add a network hop. NATS Direct may carry an
internal service request if a process boundary is later required, but raw operational KV is not a supported query
surface.

Rule mutation responses, `get_rule`, `list_rules`, and a dedicated activation-status operation consume the reader.
Watcher or status-publication loss degrades Rule readiness and metrics. A Start-owned supervisor repairs transport
loss with bounded framework backoff and full-snapshot reconciliation; it never silently falls back to file-only rules
while claiming hot reload is available.

Current-active facts join to the existing framework-owned Rule `GRAPH_STATUS` heartbeat. The typed reader treats an
expired readiness incarnation as stale history, never as a running processor. The existing heartbeat/expiry policy is
framework-owned, not an adopter knob.

### D6. Flow diagrams are authoring artifacts, not activation owners

Flowstore persists diagram metadata, nodes, connections, audit fields, and a CAS version. It persists no desired or
effective lifecycle state, provenance, component bundle, or restart claim. Creating, updating, or deleting a diagram
cannot change configuration or the running process.

Engine validates diagrams and compiles nodes into detached enabled component-configuration candidates. Compilation
rejects duplicate instance names and has no persistence or runtime effect. An explicit
`POST /flows/{id}/publish-component-configs` operation loads the saved diagram, compiles it, sorts instance names, and
upserts each candidate through Config Manager. Diagram omissions never imply deletion. A partial failure returns the
exact persisted names and failed name so retry is safe.

Config Manager seals the post-arbitration component map at successful Start. The current ComponentManager reads that
defensive snapshot once; later desired writes do not mutate it. Restart-required truth compares exact component-map
membership and canonical component values against the sealed boot map. The publish response says only that persistence
occurred, the current runtime is unchanged, and whether a restart is required.

The deployment routes, flow-status WebSocket, flow-associated log stream, lifecycle agent fields, and lifecycle agent
operations are removed without aliases. Health, metrics, and message endpoints may remain only as best-effort
observations keyed by component names declared in a saved diagram; they do not assert ownership or activation.
Completed workflow-run aggregation is named `monitor_workflow_runs` and filters AGENT_LOOPS by `workflow_slug`; it has
no dependency on flowstore.

### D7. Simplify the pending lifecycle protocol

In `restore-go-lifecycle-ownership`, replacement tasks 2.5 and 2.7 through 2.10 disappear, as do replacement/removal
cases in 2.11. Registry raw-handle retirement, boot admission ownership, terminal manager Stop, context debt, and
terminal race tests remain. `ReplaceComponent` is deleted rather than redesigned.

Rule hot reload has its own bounded lifecycle design and does not use ComponentManager replacement, runtime borrows,
or request-owned component generations.

### D8. Pre-v1 breaking migration is clean

There are no deprecated aliases, dual live paths, or compatibility shims. Migration documentation names retired APIs
and response changes. Sister repositories are read-only; downstream teams update their own code. Relevant E2E must be
green before the breaking commit lands.

### D9. Restart-safe shutdown is a prerequisite

Boot-only activation makes process restart a normal configuration operation. SemStreams therefore cannot rely on
process death as its activation mechanism until controlled shutdown is proven loss-aware.

Receipt of SIGTERM or SIGINT initiates bounded shutdown; it does not pre-cancel the Start contexts passed to services
and components. Lifecycle owners first stop admitting new work. The owner that starts a managed JetStream consumer or
core NATS subscription retains its exact returned handle, invokes native Drain during `Stop(ctx)`, and waits for
authoritative closure before Start-owned cancellation. Abrupt consumer Stop and subscription Unsubscribe are
forced-termination operations, not graceful-shutdown aliases, and cannot contribute to a clean shutdown result.

Already-delivered callbacks keep a live work context while draining. Successful durable work acknowledges only after
its effects and required publications commit. Work that cannot complete before the shutdown deadline remains unacked
or is negatively acknowledged for durable redelivery; shutdown never fabricates success. After admissions and
accepted work settle, owners cancel and join remaining Start-owned goroutines. Composition aggregates every owner
Stop result, then calls terminal transport-only Client Close to cancel and join only Client-owned workers, drain the
native connection's outbound buffer, observe CLOSED, and close transport.

This ordering is framework lifecycle mechanics, not a reactive rule or workflow. No public Quiesce phase is added
without implementation inventory proving it necessary; components may implement the phases behind their existing
`Stop(ctx)` contract. ComponentManager must not cancel every generation before NATS-owning components have had the
opportunity to drain.

The controlled-restart proof starts the real binary against durable NATS state, admits work, sends SIGTERM with work
both in flight and pending, waits for a clean exit, starts a new process with desired configuration, and proves that
redelivery converges without an invalid semantic duplicate, unfinished durable work is recovered, no accepted work is
lost, and no old listener, consumer, callback, or goroutine competes with the new process.

Dirty shutdown is a separate proof because power loss runs none of the graceful protocol. Crash-critical work uses
durable JetStream or KV, never core NATS alone. A durable handler commits its effect before ACK; a crash before ACK may
redeliver and therefore the effect must be idempotent or use a stable deduplication key. Exactly-once external side
effects are not fabricated across NATS and another system: an adopter-facing output contract must state its
at-least-once behavior and stable idempotency evidence.

Crash-critical JetStream and KV resources are file backed, and boot verifies the live resource rather than trusting a
desired declaration that an existing bucket or stream may not satisfy. Replica policy matches the declared deployment
failure domain. If every persistent NATS copy is destroyed, the data is gone; SemStreams reports that boundary rather
than calling it restart recovery.

The dirty-restart proof kills the real process at deterministic boundaries after delivery, after durable effect, after
publication, and before ACK, then boots against retained NATS state. It also kills NATS and restarts it from the same
file store. The proof covers no silent loss, expected redelivery, semantic convergence, durable desired-config
recovery, and honest failure where an external system cannot provide idempotency.

Every successful new boot selects the latest committed desired state. Crash recovery must not require a preceding
clean-shutdown record, and stale boot-incarnation facts must not suppress or impersonate the new boot's applied state.

### D10. Resource owners drain; Client closes terminal transport

> **Superseded lifecycle mechanics.** ADR-095 and
> `openspec/changes/simplify-one-shot-lifecycle-ownership/` supersede D10's `ManagedConsumer`,
> `DrainAndDelete`, handle-local backlog, later running-generation rejoin, and retained repeated Client Close result.
> D10 remains decision provenance inside this active design, but it is not the lifecycle implementation target.
> PR #984 retains boot-only composition, rule hot reload, and flow activation truth. Raw-root retirement and
> restart-safe guarantees are delegated dependencies owned by the superseding change.

ADR-095 and `simplify-one-shot-lifecycle-ownership` supersede PR #984's managed-consumer, lifecycle deletion,
concurrent/rejoin, and retained-result mechanics and own the complete `restart-safe-shutdown` and
`jetstream-consumer-policy` lifecycle target. PR #984 retains boot-only composition, rule hot reload, and flow
activation truth; it depends on `simplify-one-shot-lifecycle-ownership` for broad-root retirement and restart-safe
settlement/outbound-flush, controlled-process proof, dirty-recovery, durable-communication, live-storage/replica
validation, NATS restart, clean-marker independence, and latest-desired-state guarantees. No runtime or proof task is
completed by delegation.

Composition owns one synchronous Connect and one terminal Close. Concurrent Connect/Close is outside the supported
contract. Every retained managed-consumer constructor and subscription setup returns an exact owner handle; the
component that starts the resource retains it and drains it during Stop. `ConsumeStreamWithConfig`,
`ConsumeStreamWithConfigContexts`, and `ConsumeInternalStreamWithConfig` return `*ManagedConsumer`.
`ConsumeDurable` has no production consumer and is retired rather than widened. Client does not catalog or rediscover
child resources, and name-routed Stop/Delete, setup/delete reservations, generations, admission gates, readiness
latches, publisher settlement, and forced child convergence are not part of the lifecycle contract.

`ManagedConsumer.DrainAndDelete(ctx)` is the only graceful durable-deletion shape. Construction binds a private
deletion closure to the exact stream/durable identity; callers receive no deletion capability separate from the
handle. Drain must complete before deletion begins. An incomplete drain prevents deletion and permits a later caller
to rejoin the same drain. Concurrent or repeated deletion calls issue at most one exact deletion and observe one
retained result. Consumer-not-found after drain is benign success; every other failure, including an ambiguous
deadline, is retained as non-clean and is not retried. Partial setup never publishes a handle, and its exact acquired
resources are cleaned before return. Exact handle identity, not a rediscovered name, fences partial, duplicate, and
stale ownership.

`ManagedConsumer.OutstandingWork(ctx)` is the only outstanding-work query for a managed consumer. It reads the exact
handle's bound consumer under the caller context; it does not rediscover a mutable stream/name identity through
Client. The two callers in `processor/graph-ingest/readiness.go` and the caller in
`processor/agentic-loop/inflight.go` migrate to their retained handles. `Client.OutstandingWork(stream,name)` retires
without an alias.

Client Close rejects new Client work, cancels and exactly joins only Client-owned health and metrics workers,
initiates native connection Drain, observes native CLOSED, and returns one retained terminal result to repeated
callers. An installed transport already closed before Close is failed even when `LastError()` is nil. Any historical
or terminal non-nil `LastError()` conservatively makes the result non-clean; no drain-window callback state infers it
away. Caller-budget expiry force-closes transport but remains failed. Close never enumerates, drains, aborts, deletes,
or compensates for child resources and never launches detached cleanup. Subscription exposes Drain only: no exported
Abort or Unsubscribe lifecycle escape exists.

Synchronous Connect installs a private framework-owned `nats.FlusherTimeout(5*time.Second)` with no option, config, or
environment knob. The ceiling makes a blocked native write/flush fail visibly so owner Stop and controlled shutdown
can terminate rather than predict an adopter-provided timeout.

Every controlled shutdown uses one fresh bounded shutdown context, stops admission owners, drains every exact owner
handle while accepted-work authority remains live, cancels and joins remaining owner work, aggregates all Stop
results, and only then invokes Client Close. Both clean and failed controlled shutdowns exit the current process.
Clean versus failed controls exit status and observability only; neither authorizes Client reuse or in-process restart.
Supervision starts a fresh process with a newly constructed Client and the latest committed desired state.

Before the breaking tag, Client and framework constructors retire broad mutable native roots (`*nats.Conn`,
`jetstream.JetStream`, `jetstream.Stream`, `jetstream.KeyValue`, `jetstream.ObjectStore`, or equivalent capabilities).
Broad injected roots narrow to measured local interfaces. Reviewed message, value, watcher, lister, and future seams
remain only with explicit caller context and local Stop/completion ownership. There is no `Unsafe*` compatibility
alias, and sister repositories remain read-only to this change.

## Approved-ruling conformance

This contract-only slice claims no runtime implementation or proof. Each binding ruling maps to durable target-state
text; every runtime task cited below remains unchecked.

| Approved ruling | Contract evidence |
|---|---|
| Boot-only topology and dedicated rule-definition hot reload remain | `design.md:39`, `design.md:74` |
| Dirty power and settlement stay Close-independent | `design.md:216`, `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md:74`, `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md:137` |
| Resource owners retain exact native handles | ADR-095; `../simplify-one-shot-lifecycle-ownership/specs/jetstream-consumer-policy/spec.md` |
| Quiesce and exact Closed precede runtime cancellation | ADR-095; `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md` |
| Normal lifecycle does not delete durable topology | ADR-095; `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md` |
| OutstandingWork remains an independent exact observer | `../simplify-one-shot-lifecycle-ownership/design.md` |
| Compiler errors direct callers to retain the exact native handle | `docs/operations/migration-restart-safe-nats-client.md` |
| Abrupt Stop cannot become clean; no lifecycle deletion escape exists | ADR-095; `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md` |
| Client Close is terminal transport-only | ADR-095; `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md` |
| Preclosed, LastError, and deadline truth remains non-clean; completed repeat is nil | `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md` |
| Connect owns a private five-second flusher ceiling | `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md:51` |
| Every controlled result exits to a supervisor-started fresh process | `../simplify-one-shot-lifecycle-ownership/specs/restart-safe-shutdown/spec.md:107` |
| ConsumeDurable retires | `../simplify-one-shot-lifecycle-ownership/specs/jetstream-consumer-policy/spec.md:10` |
| Historical ADR-070 remains unchanged | `094-boot-only-composition-and-observable-rule-activation.md:163` |
| Broad roots retire or narrow to locally owned seams | `native-surface-inventory.md:134` |
| Superseding six-PR order binds; runtime and proof tasks remain unchecked | `../simplify-one-shot-lifecycle-ownership/tasks.md` |
| Reset inventory approval artifact and SHA are durable | `inventory.md:5` |
| Minimal lifecycle design approval artifact and SHA are durable | `design.md:15` |
| Native inventory is byte-identical to its approved SHA | `native-surface-inventory.md:1`, `inventory.md:113` |

## Risks and mitigations

- **Operator expects flow change to be immediate.** Typed responses state runtime unchanged and restart required.
- **Rule write succeeds but activation fails.** Revision-bound terminal status records rejection; active generation
  remains unchanged.
- **Rapid rule writes coalesce.** Every observed revision reaches `applied`, `rejected`, `superseded`, or
  `canceled_shutdown`; coalescing cannot silently relabel an intermediate write as applied.
- **Rule reload recreates general component config.** The accepted payload is Definition-only. Component envelope
  fields are rejected as restart-required config, not interpreted by the hot path.
- **Deletion removes lifecycle tests that protected Stop.** Terminal shutdown and Start/Stop race tests remain and are
  strengthened around the smaller boot-owned manager.
- **Restart becomes frequent and exposes latent teardown debt.** Boot-only activation does not land until the
  controlled signal, drain, settlement, clean-exit, dirty-crash, and next-boot known-answer proofs are green.
