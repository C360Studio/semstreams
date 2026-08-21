# ADR-094: Boot-Only Composition and Observable Rule Activation

## Status

**Accepted (2026-08-16).** Breaking pre-v1 lifecycle simplification. This ADR partially supersedes ADR-026's live
flow-activation decision while preserving its coordinator judgment role and durable flow authoring.

## Context

SemStreams currently lets configuration serve three incompatible roles:

1. durable desired state for a future process;
2. effective state of the running process; and
3. a command to alter that process.

The third role grew a broad runtime-composition protocol. ComponentManager can watch component and model-registry
configuration, reconcile additions and removals, restart dependants, and replace Registry generations. A generic HTTP
PUT also probes optional component interfaces and may change transient process state without persisting it.

That generality has no demonstrated adopter. `watch_config` defaults false and no shipped configuration enables it.
No production component implements the generic `UpdateConfig(ctx, json.RawMessage)` contract. Shipped binaries do not
wire the flow-lifecycle manager needed by agent flow lifecycle tools.

Rule authoring is different. Both shipped binaries wire rule CRUD, and the Rule processor already observes desired
rule definitions. Immediate expression and cron editing is a high-value operator and flow-builder interaction. The
useful behavior is changing a rule set inside one fixed Rule processor, not replacing the component that owns it.

Keeping generalized hot composition solely to support that narrow interaction imposes replacement reservations,
borrow gates, transition states, failed-candidate policy, request-to-supervisor lifetime transfer, and a large race
matrix on the entire framework. It also makes an adopter predict whether a successful config write is durable,
effective, or merely accepted.

## Decision

### Boot seals service and component composition

Successful boot fixes service and component identity, declaration, dependency, port, and configuration state for the
process lifetime. Post-boot changes to `services.*`, `components.*`, `platform`, `nats`, or `model_registry` are durable
desired state for the next successful boot. They do not create, start, stop, remove, reconfigure, restart, or replace a
running service or component.

ComponentManager's config subscribers, generic live-config PUT, hidden update interfaces, runtime reconcile, restart,
and replacement paths are retired. Registry admits the boot set and exposes defensive values; it has no live
replacement protocol. Terminal shutdown remains Start-owned and must cancel, join, and stop the exact boot generation.

Flow create, update, validation, and persistence remain supported. Deploy, start, stop, and undeploy operations change
desired state only while the process is running. Their typed result states that desired state changed, runtime did not,
and restart is required. SemStreams does not restart itself.

Persisted flow `runtime_state` is retired because Engine currently writes `deployed_stopped` and `running` after
desired config mutations without observing runtime. Flowstore instead records desired `absent`, `disabled`, or
`enabled` activation. Flow reads and `monitor_flow` report desired state, independently observed effective state, and
`restart_required`; without an authoritative runtime observer, effective state is `unknown`. Every successful boot
seals a unique boot incarnation and a canonical digest of the desired configuration it actually applied. Reads compare
current desired provenance with that boot-applied provenance; health is reported separately and cannot prove that a
configuration revision became effective.

### Rule definitions are the one live exception

A running Rule processor may hot activate expression and cron rule-definition create, update, and delete operations.
This is a dedicated capability, not generic component reconfiguration. The component envelope remains fixed at boot,
including ports, dependencies, entity-watch buckets, integration mode, producer identity, and projection bindings.

The Rule processor builds and validates a complete candidate rule set before changing the active generation. A
rejected candidate leaves the previous generation unchanged. Watch, reconciliation, activation, and status publication
descend from the Rule processor's Start context; production structs and retained callbacks do not store context.

### Desired rules and activation truth use KV Watch

Desired rule definitions and activation outcomes are facts. Both require restart replay and fan-out, and both are fast,
idempotent observations. They therefore use KV Watch rather than a work stream.

A rule write targets one boot-composed `pack_id`, commits a typed present/deleted desired record, and returns an opaque
pack/rule/revision receipt with activation pending. Write success is not evidence that the running Rule processor
accepted the revision. Each processor instance publishes a terminal outcome for every observed revision: `applied`,
`rejected`, `superseded`, or `canceled_shutdown`. The status identifies the active rule-set generation and a typed
failure or superseding revision where applicable.

Activation facts are processor-instance scoped, including a unique boot incarnation and Rule component identity, so
multiple Rule processors and successive boots cannot overwrite one another. They live in a framework-cataloged,
bounded operational KV store.
Bucket identity, retention, and key grammar are framework-owned implementation details, not adopter-provided tuning
knobs.

Typed rule reads and an activation-status operation consume those facts. An operator does not provide a bucket name,
key grammar, process identity, boot incarnation, or watcher timing. Rule's existing `GRAPH_STATUS` readiness heartbeat
is the sole liveness fact. Its framework-owned key is stable per process slot, Rule component, and pack; its envelope
carries the unique `boot_id`. Only activation facts joined to the exact fresh envelope incarnation are current; older
facts are bounded history. Unknown readiness freshness yields unknown current activation, never promotion of stale
history. Watcher bootstrap/closure, reconcile, and status-publication failures degrade Rule readiness and metrics; a
Start-owned supervisor retries transport loss and performs full-snapshot repair.

### Restart-safe shutdown and crash recovery are activation prerequisites

Because component and flow changes activate at boot, controlled restart becomes a normal configuration operation.
Boot-only activation does not land until shutdown is loss-aware and proven end to end.

SIGTERM and SIGINT initiate bounded quiesce; they do not cancel the runtime Start context before lifecycle owners can
stop admission and drain. The owner that starts a managed JetStream consumer or core NATS subscription retains its
exact returned handle, invokes native Drain during Stop, and waits for authoritative closure before canceling
Start-owned work. Graceful paths do not substitute `ConsumeContext.Stop` or `Unsubscribe`, both of which can discard
queued delivery. `ConsumeDurable` is retired because its production consumer census is zero; the three retained
consume constructors return exact managed-consumer handles instead.

Already-delivered callbacks retain a live work context during drain. Durable work acknowledges only after its effects
and required publications commit. Work that misses the shutdown deadline remains unacknowledged or is negatively
acknowledged for redelivery. After accepted work settles, owners cancel and join remaining Start-owned goroutines and
composition aggregates every Stop result. Only then does terminal transport-only Client Close cancel and join its own
health/metrics workers, native-drain the connection, observe CLOSED, and report conservative transport history. Client
does not catalog, rediscover, or compensate for children. A preclosed installed transport, any historical or terminal
`LastError`, or a deadline-forced close is an observable failed shutdown, never a clean restart result. Connect owns a
private five-second native flusher ceiling with no adopter-facing knob.

Every controlled shutdown exits the current process. A clean all-owner Stop plus Client Close result produces clean
observability and successful exit; an incomplete owner or transport boundary produces failed observability and
nonzero exit. Neither result authorizes in-process restart or Client reuse. Supervision starts the fresh process that
consumes the latest committed desired state.

The required proof runs the real process across SIGTERM and a new boot against retained NATS state, with both in-flight
and pending work. It proves semantic completion for acknowledged work, recovery of unfinished durable work, no loss of
accepted callbacks, clean reuse of listeners and durable consumers, and activation of the new desired configuration.

Power loss is a separate mandatory proof because it runs no shutdown code. Crash-critical work uses durable JetStream
or KV rather than core NATS alone. A handler commits its durable effect before ACK; a crash before ACK can redeliver, so
the effect must be idempotent or use a stable deduplication key. SemStreams does not claim exactly-once effects across
NATS and an external system without a transactional boundary; output contracts expose at-least-once behavior and
stable idempotency evidence.

Crash-critical streams and KV buckets are file backed, and boot verifies the live storage and declared replica policy.
The recovery contract assumes the declared NATS persistence failure domain survives. Destruction of every persistent
copy is data loss, not an application lifecycle condition.

The dirty-restart gate kills the real process after delivery, after durable effect, after publication, and before ACK,
then restarts against retained NATS state. It also kills NATS and restarts it from the same file store. The gate proves
no silent loss, expected redelivery, semantic convergence, and desired-config recovery without relying on Stop, Drain,
or deferred cleanup.

Every successful boot consumes the latest committed desired state regardless of how the previous process exited.
Planned activation uses the clean-shutdown protocol; dirty restart uses the crash-recovery contract. Neither a missing
clean-exit record nor stale status from a dead boot incarnation may suppress committed desired configuration.

## Consequences

The runtime lifecycle model becomes one boot generation plus restart-safe terminal shutdown. The pending lifecycle
restoration no longer needs live component replacement, removal, reservation, borrow, or candidate-failure protocols.
NATS lifetime follows exact owner handles rather than a Client-wide child ledger. Broad mutable Conn, JetStream,
Stream, KV, and ObjectStore roots returned by Client/framework constructors retire before release; retained narrow
watcher, lister, future, message, and value seams carry caller context and local Stop/completion ownership. No
`Unsafe*` compatibility alias survives. Terminal Stop behavior, context cleanup, and deterministic race proofs remain
required.

Operators may author flows without stopping the process, but their activation boundary is the next successful boot. The
response makes that boundary explicit. Operators retain immediate rule editing and gain revision-bound evidence of
whether each processor instance applied the edit.

This is a breaking pre-v1 change. Restart-safe shutdown is a prerequisite, not deferred hardening. Removed APIs receive
no compatibility shims or parallel live paths. Sister repositories are read-only to this work; migration documentation
records downstream changes for their owners. Controlled- and dirty-restart plus relevant core, structural, CRUD,
agentic, and semantic E2E evidence is required before the breaking commit lands.

Delivery is dependency ordered: contract reset; owner-handle adoption while temporary catalogs remain; minimal Client
and catalog removal; broad raw-root retirement/narrowing; controlled always-exit process proof; then dirty-power proof.
ADR-070 remains unchanged historical context for durable gated-DAG dispatch even though its unused `ConsumeDurable`
helper is retired by the current capability contract.

## Superseded ADR-026 scope

ADR-026 remains authoritative for the coordinator as the judgment layer, its structured decisions, and its ability to
author durable flow and rule definitions. This ADR supersedes only these activation claims:

- a flow config write causes ComponentManager to instantiate or replace components in the running process;
- `manage_flow` changes running topology without a reboot; and
- all coordinator-authored configuration has immediate runtime effect.

Coordinator-authored flow changes are now pending-next-boot desired state. Coordinator-authored rule definitions may
hot activate only through the bounded, observable rule capability defined here.

## References

- [ADR-026: Coordinator Agent](026-coordinator-agent-dynamic-flow-composition.md)
- [ADR-028: Orchestration Architecture](028-orchestration-architecture.md)
- `openspec/changes/archive/2026-08-21-require-restart-for-config-activation/`
