# Design: Boot-only composition with bounded rule activation

## Context

SemStreams configuration currently conflates three different facts:

1. desired configuration persisted for a future boot;
2. effective configuration of the running process;
3. commands to mutate that process.

This design separates them. Desired state remains writable. Effective component and service composition is sealed at
boot. Live rule-definition activation remains as a dedicated capability because it has a demonstrated UX consumer and
does not require component generation replacement.

Historical artifact provenance: the original D10 and delivery order derive from the owner-approved minimal lifecycle
design artifact with SHA-256 `0047789823bd8a6b3f772bb598fae90ca6060a8799cd828a95487589e0a7a11e`;
they are not current authority. The measured reset inventory is
`openspec/changes/require-restart-for-config-activation/inventory.md`; the exact native-surface companion is
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md` with approved SHA-256
`d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`.

## Goals

- Preserve flow/rule authoring, validation, persistence, and boot-time assembly.
- Remove general in-process component/service/topology mutation.
- Preserve live expression and cron rule-definition changes inside a fixed Rule processor envelope.
- Make activation claims revision-bound and observable.
- Keep boot composition free of live replacement/removal machinery.
- Depend on the generic lifecycle ordering and proof owned by `simplify-one-shot-lifecycle-ownership` while retaining
  only Rule-specific activation terminalization under that contract.

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
requires it. The handle is valid only for the callback and cannot be retained. This change defines no terminal fence,
result, Start-finalization, or failed-Start behavior; ADR-095 and `simplify-one-shot-lifecycle-ownership` own those
lifecycle mechanics. There are no replacement/removal transition gates.

### D2. Config KV remains durable desired state

Config KV remains authoritative for next-boot configuration after existing version arbitration. FlowService and other
authoring paths may validate and persist desired changes while a process is running. They must not mutate the sealed
runtime.

An accepted flow mutation returns an honest pending-next-boot result: desired state changed, current runtime unchanged,
restart required. SemStreams does not automatically exit or restart; process supervision is deployment policy.

Every successful boot consumes the latest committed desired state regardless of how the prior process exited. Planned
activation depends on the separately owned lifecycle and proof named in D9; dirty restart correctness is also an
external lifecycle dependency. A prior clean-exit record is useful operational evidence but never a gate that can
suppress committed desired configuration.

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

### D6. Flow authoring remains, live topology activation does not

Deploy/start/stop/undeploy may continue to mutate validated desired component records. While a process is running they
return runtime-unchanged/restart-required truth. Runtime lifecycle agent tools remain unwired unless a future product
proves a consumer for desired-state authoring; they do not regain live activation semantics.

`flowstore.Flow.RuntimeState` cannot retain its current meaning: Engine writes `deployed_stopped` and `running` after
desired config writes, and `monitor_flow` repeats those values as runtime truth. The durable field becomes
`desired_state` with explicit `absent`, `disabled`, or `enabled` values. Historical deployment/start/stop timestamps
that describe unobserved runtime are removed or renamed as desired-state audit facts.

At boot, the framework assigns a unique `boot_id`, canonicalizes the selected desired configuration, and seals a
framework-owned digest for the whole boot snapshot plus the relevant flow/component subsets. Effective observations
carry that immutable boot-applied provenance. Rule/flow reads and monitoring expose current desired provenance,
boot-applied provenance, independently observed effective state, and `restart_required`.

`restart_required` compares the current desired digest/membership with the digest/membership actually sealed by this
boot. It does not compare only `enabled`/`disabled`, because desired and effective state can have equal labels while
their configuration differs. Runtime health is a separate observation and cannot establish activation provenance. If
no authoritative runtime observer is available, effective state and boot-applied provenance report `unknown`; neither
is copied from flowstore or desired state.

### D7. Lifecycle authority is an external prerequisite

ADR-095 and `simplify-one-shot-lifecycle-ownership` exclusively own generic component/service exact Start
finalization, failed-Start cleanup, callback-borrow shutdown, terminal owner sequencing, ACK ordering, settlement, and
controlled/dirty restart proof. This change receives no completion credit from that dependency. It retains only
Rule-specific activation terminalization: fence status publication and cancel/join the Rule-local activation work
under the generic lifecycle contract. Rule hot reload remains bounded to the fixed boot-composed Rule processor and
does not use ComponentManager replacement or request-owned component generations.

### D8. Pre-v1 breaking migration is clean

There are no deprecated aliases, dual live paths, or compatibility shims. Migration documentation names retired APIs
and response changes. Sister repositories are read-only; downstream teams update their own code. Relevant E2E must be
green before the breaking commit lands.

### D9. Lifecycle proof is a release dependency, not owned work

Boot-only activation cannot claim activation or release readiness until the generic runtime, settlement,
controlled-process, dirty-recovery, and E2E proof owned by `simplify-one-shot-lifecycle-ownership` passes. This change
owns no generic component/service lifecycle task, shutdown ordering, ACK rule, recovery mechanism, or proof artifact
and receives no completion credit by cross-reference. Its only lifecycle-adjacent ownership is Rule-specific
activation terminalization under that external contract.

### D10. Superseded lifecycle design tombstone

> **Historical provenance only.** The original owner-approved lifecycle artifact hash
> `0047789823bd8a6b3f772bb598fae90ca6060a8799cd828a95487589e0a7a11e` and Git history preserve the superseded
> design evidence. Its ManagedConsumer, DrainAndDelete, rejoin, and retained-result mechanics are non-normative.
> ADR-095 and `simplify-one-shot-lifecycle-ownership` own the current lifecycle target.

## Approved-ruling conformance

This contract-only slice claims no runtime implementation or proof. Each binding ruling maps to durable target-state
text; every runtime task cited below remains unchecked.

| Approved ruling | Contract evidence |
|---|---|
| Boot-only topology and dedicated rule-definition hot reload remain | `design.md:39`, `design.md:74` |
| Desired/effective flow truth is distinct | D2, D6; `specs/flow-activation-truth/spec.md` |
| Rules-only hot reload and Rule-local activation terminalization remain bounded | D4, D5, D7; `specs/rule-hot-reload/spec.md` |
| Lifecycle proof is a dependency with no completion credit | D7, D9; `../simplify-one-shot-lifecycle-ownership/tasks.md` |
| Historical lifecycle design hash remains provenance | D10 |
| Sister repositories remain read-only | D8 |

## Risks and mitigations

- **Operator expects flow change to be immediate.** Typed responses state runtime unchanged and restart required.
- **Rule write succeeds but activation fails.** Revision-bound terminal status records rejection; active generation
  remains unchanged.
- **Rapid rule writes coalesce.** Every observed revision reaches `applied`, `rejected`, `superseded`, or
  `canceled_shutdown`; coalescing cannot silently relabel an intermediate write as applied.
- **Rule reload recreates general component config.** The accepted payload is Definition-only. Component envelope
  fields are rejected as restart-required config, not interpreted by the hot path.
- **Lifecycle prerequisite is incomplete.** Boot-only activation cannot claim release readiness until the simplify
  change records its runtime and proof gates; this change receives no lifecycle completion credit.
