# Post-R1c foundation remap roadmap

**Artifact state:** DESIGN DRAFT — pending independent pre-owner review.
**Repository baseline:** `c38e3e82d5a0b1deec598ad1bf8bb21a6bf0b3fa`.
**Accepted inventory:** `docs/proposals/post-r1c-foundation-remap-inventory.md`, 447 lines, 25,852 bytes,
SHA-256 `d347b99935e9d9a8f3ddf1e97b6e3595d187e51087829ea96e06aa25321de953`.
**Inventory verdict:** `INVENTORY PASS`.
**Authority:** none until independent design review passes and the owner accepts the resulting exact artifact.

The accepted inventory is a companion part of this design handoff and is incorporated by the exact identity above. It
is not copied here because duplicating a 447-line current-state record would create another drift surface.

## 1. Program objective

Restore one comprehensible foundation for component declarations and current graph facts before touching indexes,
queries, GraphQL, or queued graph issues. The desired framework remains:

- pragmatic and easy for an external component author to understand;
- offline-first and edge-capable;
- tiered, so deployments may select only the graph capabilities they need;
- eventually consistent, with missing, stale, or malformed derived facts surfaced rather than converted into global
  startup failure;
- component-and-flow based, with ports as the component API contract; and
- built on NATS primitives according to the fact/request distinction, not a CQRS or semantic-ownership model.

This roadmap does not create recovery tooling, checkpoint orchestration, NATS-CLI dependencies, writer ownership,
exactly-once claims, or a new internal event stream.

## 2. Binding design constraints

1. Pre-v1 breaks are clean. No compatibility shim, deprecated method, accepted alias, dual declaration, or legacy
   decoder remains after its owning slice.
2. One semantic fact has one interpreter. A consumer may select local response policy, but it may not invent another
   spelling of port kind, resource identity, readiness outcome, or bucket acquisition.
3. Port declarations describe dependencies and provisions. They do not inject NATS handles or absorb component-owned
   retry, degraded-mode, or lifecycle policy.
4. `StoreReadPort` remains backend-neutral referenced-content federation. Exact current KV access is a separate
   `kv-read` semantic kind.
5. The ten named downstream repositories are later API/feature-parity holdouts. They neither block nor shape this
   foundation.
6. Each slice lands independently green. A stop condition ends the slice rather than authorizing a workaround or new
   abstraction.
7. After the three slices, the program stops and re-inventories. No old `R1d`, `R1e`, `R2`, or later issue sequence is
   inherited.

## 3. Decision-skill outcome

The `kv-or-stream` four-test heuristic gives the following result:

- Current entity, index, readiness, and diagnostic facts use KV exact read or KV watch.
- Mutation and tool requests use NATS request/reply, or JetStream where durable queued work is already required.
- Referenced large-content acquisition uses store federation/`StoreRegistry`; it is outside the KV-versus-stream
  choice.

No inventoried path warrants a new stream. The write remains the event for current facts. Port metadata records which
fact or request a component uses; it does not build a parallel communication channel.

## 4. Options considered

### Option A — Do nothing

Cost: no immediate migration.

Consequence: seven configurations continue to contain silently ignored `kv_read`; the special `KVWrite` lane remains
mostly inert; unknown kinds can become NATS ports; store ports do not round-trip; registry capabilities,
ComponentManager, flowgraph, message-logger, and components keep interpreting different subsets. Every new port kind
continues to require a repository-wide hunt.

### Option B — Patch local readers on current `Discoverable`

Shape: add `KVReadPort`, teach each existing switch about it, and repair graph-clustering declarations in place.

Cost: smaller first diff.

Consequence: preserves component-authored runtime `Port` rendering and the independent switches in registry,
ComponentManager, flowgraph, serialization, and message-logger. It fixes today's missing token by extending the defect
class that produced it.

### Option C — Foundation-first consolidation

Shape: remove the redundant diagnostic plane, establish one strict port language, then atomically replace
component-authored runtime ports with component declarations and one framework-owned effective snapshot.

Cost: three deliberate breaking slices and one repository-wide mechanical component migration.

Consequence: deletes cross-cutting code before the port cutover, makes invalid declarations fail at boot, and leaves a
single interpreter for every management and flow consumer. Component runtime acquisition and eventual-consistency
policy remain local.

### Option D — Broad graph/framework rewrite

Shape: change ports, component lifecycle, readiness, indexes, query clients, gateways, GraphQL, and issue-queue behavior
together.

Cost: largest diff, longest feedback loop, and no meaningful attribution when an E2E result moves.

Consequence: combines unrelated owners and encourages abstractions before present consumers prove them. It recreates
the planning churn this remap exists to stop.

## 5. Recommendation

Choose Option C: foundation-first consolidation, implemented as Foundation A, B, and C below, followed by a mandatory
stop/remap checkpoint.

The ordering is intentional:

1. remove an expensive diagnostic plane that has no dedicated framework consumer;
2. make the port language strict and singular without changing component authorship yet; then
3. perform one atomic exported component-declaration cutover against that proven language.

No slice contains index behavior, query results, GraphQL, message-logger security/scaling, or a readiness policy
rewrite.

## 6. Premises and measurements

- `COMPONENT_STATUS` is neither graph correctness nor component health. Accepted inventory section 4.5 distinguishes
  it from `GRAPH_STATUS`, `Health()`, and `pkg/lifecycle`.
- The diagnostic plane has broad write cost: 25 reporter constructor sites across 26 files and approximately 93
  stage/cycle calls.
- It has a real but generic consumer: default-enabled message-logger accepts any existing bucket and provides
  query/watch replay, while no dedicated production reader exists.
- Exact KV-read declaration is absent: no `KVReadPort`, `PortConfig.KVRead`, or `kv-read` decoder branch exists.
- Ignored declarations already ship in seven configurations with nine `kv_read` rows.
- Interpretation is plural. Accepted inventory sections 4.1-4.2 enumerate component, serializer, builder, flowgraph,
  registry, manager, and logger owners.
- Store read is not KV read. `StoreReadPort` federates referenced content through `StoreRegistry` and all-to-all
  provider matching.
- Shared readiness already exists. `graph/readiness` owns publisher, watcher, set, outcome state, freshness, and
  explicit producer keys.
- The downstream census should not bias the target. The later holdout boundary is recorded at
  `docs/operations/36-graph-foundation-breaking-cutover.md:8-10,62-87`.

## 7. Foundation A — delete the redundant component-status plane

### 7.1 Boundary

Delete framework production and test surfaces whose only job is writing or directly reading `COMPONENT_STATUS`:

- `component.Status` and `component.LifecycleReporter` diagnostic types in `component/lifecycle.go`;
- `component/lifecycle_reporter.go` and `component/lifecycle_reporter_catalog.go`;
- every component reporter field, constructor call, stage report, and cycle report;
- `graph.BucketComponentStatus` and its `KVCatalog` descriptor;
- `COMPONENT_STATUS`-specific E2E client helpers; and
- comments and docs that describe this bucket as a supported framework status surface.

Do not rename or replace the reporter. A new name would retain the same 93-call maintenance bill without adding a
consumer or framework decision.

### 7.2 Preserved boundaries

- `component.State`, `LifecycleComponent`, and ComponentManager lifecycle control remain.
- `Discoverable.Health()` and ComponentManager HTTP component health remain the component-health front door.
- `graph/readiness` and `GRAPH_STATUS` remain the graph-derived readiness front door.
- `pkg/lifecycle` remains the domain/workflow phase convention over entity state.
- Message-logger remains a generic, default-enabled, caller-selected, must-exist KV query/watch service. It is not
  narrowed to the graph catalog. A request for a nonexistent `COMPONENT_STATUS` receives the existing bucket-not-found
  result; no replacement bucket is predicted or created.
- Product/deployment middleware remains responsible for message-logger authorization.

Correct the stale `GRAPH_STATUS` catalog owner prose to name the four present producers: graph-index,
graph-embedding, graph-ingest, and rule. Do not change publisher keys, envelopes, freshness, policy, or required
producer selection.

### 7.3 Exported break and adopter path

This slice deletes exported diagnostic types and constructors. It adds no alias or no-op replacement.

An external component author does nothing: component start, stop, health, ports, and data flow continue without a
diagnostic reporter. If the adopter directly imported the reporter API, the compiler identifies every required
deletion. If a deployment queried `COMPONENT_STATUS`, it receives ordinary bucket absence and should use the component
health endpoint for component health or `GRAPH_STATUS` for graph readiness.

### 7.4 Verification

- Exact searches for `BucketComponentStatus`, `COMPONENT_STATUS`, `LifecycleReporter`, `ReportStage`, and
  `ReportCycle` reach zero outside historical artifacts.
- ComponentManager lifecycle and HTTP health unit/integration tests remain green.
- Message-logger tests prove caller-selected existing framework and product KV buckets still query/watch and that reads
  never create a bucket.
- `graph/readiness` unit tests remain unchanged and green.
- `task e2e:core` is green before the breaking slice lands.

### 7.5 Stop condition

Stop if a dedicated production consumer is found whose correctness depends on stage/cycle records. Generic
message-logger reachability alone is already accounted for and is not such a consumer. Do not replace the plane during
this slice; return the consumer evidence for an owner ruling.

## 8. Foundation B — one canonical port language

### 8.1 Boundary

Centralize declaration parsing, runtime rendering, serialization, classification, and normalized inspection in the
`component` package before breaking `Discoverable`.

Primary seams:

- `component/port.go`, `component/ports.go`, and concrete `component/port_*.go`;
- `component/flowgraph`;
- `component/registry.go` capability and resource-conflict code;
- `service/component_manager.go` port reporting.

Message-logger's constructor-time raw-config interpreter remains one explicit temporary exception at the Foundation B
boundary. It cannot consume an unexported resolver or a registry snapshot that does not exist yet. Foundation C removes
that last interpreter through the snapshot observer in section 9.5; #859 cannot close before then.

### 8.2 Canonical vocabulary

Add exported `PortKind` constants for the present semantic kinds and make them the only discriminators used by
definition JSON, runtime `Port` JSON, resolver logic, flow pattern classification, capability announcements, and
management reporting.

The canonical set includes the present supported classes plus `kv-read`:

- `timer`
- `network`
- `file`
- `http-client`
- `nats`
- `nats-request`
- `jetstream`
- `kv-watch`
- `kv-read`
- `kv-write`
- `store-read`
- `store-provide`

Protocol is data on a `network` declaration; `http`, `grpc`, and `websocket-server` are not alternate kinds. Migrate
all owned declarations to `network` plus explicit protocol data in the same slice.

Delete accepted aliases such as `kv`, `kvwatch`, and `kvwrite`, migrating all owned configurations to canonical
spellings in the same slice. Delete the dead `NATSStreamPortConfig` and `NATSRequestPortConfig` types. This is a clean
break, not a compatibility decoder.

Use one JSON and Go shape for definitions and resolved ports. `PortDefinition` retains only common declaration fields
(`name`, `required`, `description`) plus a typed `Portable` config. Delete the duplicated flat `type`, `subject`,
`interface`, `timeout`, `stream_name`, and `bucket` fields and delete `Config any`. The wire shape is:

```json
{
  "name": "graph_mutations",
  "required": true,
  "description": "canonical mutation request",
  "config": {
    "kind": "nats-request",
    "subject": "graph.mutation.>",
    "timeout": "1s",
    "interface": {"type": "semstreams.graph.mutation", "version": "v1"}
  }
}
```

`Portable.Type() string` becomes `Portable.Kind() PortKind`. The same `config.kind` decoder is used whether the outer
value is a declaration or a resolved `Port`; there is no second `{"type": ..., "data": ...}` envelope.

### 8.3 Binding kind matrix

- `timer`: input; requires `interval`; optional interface; shared resource identity is the interval.
- `network`: input or output; requires protocol and positive port; host defaults to `0.0.0.0`; protocol/host/port is
  exclusive.
- `file`: input or output; requires path; optional pattern; path is shared.
- `http-client`: input; requires URL pattern; method defaults to `GET`; trigger port, auth ref, contact policy, and
  interface are optional; method/URL is shared.
- `nats`: input or output; requires subject; queue and interface are optional; subject is shared.
- `nats-request`: input or output; requires subject; timeout defaults to `1s`; retries and interface are optional;
  subject is shared.
- `jetstream`: input or output; requires stream name or at least one subject; current stream/consumer fields and
  interface are optional; resource identity is the stream or first subject and is shared.
- `kv-watch`: input; requires bucket; keys, history, and interface are optional; bucket is shared.
- `kv-read`: input; requires bucket; interface is optional; bucket is shared.
- `kv-write`: output; requires bucket; interface is optional; bucket is shared.
- `store-read`: input; requires advisory bucket; interface is optional; federation is shared.
- `store-provide`: output; requires instance; ownership conflicts remain checked by `StoreRegistry`.

Host defaulting and the existing request timeout are the only resolver defaults. All other required data is explicit.
Wrong direction, duplicate name, unknown field, missing data, malformed duration, and invalid port are typed boot
errors. Kind-specific fields live only on the named concrete config type; there is no field-precedence problem to
reconcile.

### 8.4 Exact KV read

Add `KVReadPort` as declaration/runtime metadata for exact or list access to the current value of one named KV bucket.
It is non-exclusive and carries bucket identity and optional interface metadata. It does not open a bucket, create a
bucket, inject a handle, select retry, or imply watch/replay.

Keep these classes separate:

- `kv-read`: exact/list current KV values;
- `kv-watch`: observe current values and subsequent changes;
- `kv-write`: mutate current facts, normally with owner-local CAS behavior; and
- `store-read`: resolve referenced large content through store federation.

### 8.5 One resolver and one normalized fact

Delete exported `BuildPortFromDefinition`. The framework-only signature is:

```go
func resolvePort(def PortDefinition, direction Direction) (Port, error)
```

Its output is the single normalized runtime fact consumed by framework interpreters. Both `PortDefinition` and `Port`
JSON use the same kind decoder/encoder.

Unknown or malformed kinds return a typed, component/port-named configuration error before component start. There is
no default-to-NATS branch and no `unknown` capability result for a declaration accepted at boot.

Flowgraph, registry capabilities/resource conflicts, and ComponentManager port reporting consume the normalized result
rather than independent type switches. Store federation remains a distinct flowgraph pattern derived from the same
normalized fact. The documented message-logger exception is removed in Foundation C, not papered over here.

### 8.6 Configuration and first-consumer migration in this slice

- Migrate every owned alias to the canonical kind and required explicit data.
- Move all ignored top-level `kv_read` rows into ordinary direction-bearing inputs and delete the top-level rows. Do
  not add `PortConfig.KVRead`.
- Make the new exact-read kind real at birth: graph-clustering declares `KVReadPort` inputs for `ENTITY_STATES`,
  `OUTGOING_INDEX`, and `INCOMING_INDEX`; agentic-tools and graph-query declare their current exact reads. These are
  still returned through the current `InputPorts()` method until Foundation C.
- Retain the current `PortConfig.KVWrite` field only until Foundation C, without adding new uses. It is removed in the
  atomic declaration cutover rather than broken before its current component migration.

Keeping the current field across one green boundary is sequencing of an existing surface, not a shim or second target.

### 8.7 Exported break and adopter path

`PortKind`, the typed concrete configs, and `KVReadPort` are exported because component declarations consume them. The
resolver remains unexported because only the framework resolves declarations. The old builder signature and kind
aliases are deleted.

An adopter declares a canonical kind and its semantic resource data. Correct declarations resolve identically for all
framework consumers. Incorrect declarations fail boot with component, port, kind, and field context; they never
silently disappear or change semantic class.

### 8.8 OpenSpec contract delta

The `component-runtime-config` and component-discovery contracts gain these requirements:

- A port declaration SHALL use the common envelope and exactly one canonical `config.kind` from the binding matrix.
- Configuration loading SHALL reject unknown kinds, unknown fields, invalid directions, duplicates, and missing
  required kind data before component initialization.
- A NATS request declaration SHALL preserve subject, timeout, retries, and interface through JSON decode and runtime
  resolution.
- A JetStream declaration SHALL preserve every current consumer/stream field through that same path.
- `kv-read`, `kv-watch`, `kv-write`, and `store-read` SHALL remain distinct kinds with the direction and resource
  semantics in the matrix.

These requirements replace, rather than sit beside, current alias and flat-field behavior.

### 8.9 Verification

- Table-driven parse/resolve/JSON round-trip tests cover every canonical kind.
- Tests prove every unknown kind and missing required field fails before component start.
- Contract tests prove flowgraph, capability, resource conflict, and ComponentManager projections are derived from the
  same normalized fact.
- A structural assertion records message-logger as the sole remaining raw port interpreter and prevents another one
  from appearing before Foundation C deletes it.
- Store-read/provide round-trip and federation tests remain green.
- Exact searches prove deleted aliases and dead config types are absent from production/configuration.
- `task e2e:all` and `task e2e:research-graph` are green because the common port wire shape changes across tier,
  agentic, store-federation, and research-graph configurations.

### 8.10 Stop condition

Stop if a present port cannot be represented without embedding component-owned runtime policy, or if a proposed
extension mechanism lacks two current consumers. Do not add a custom-kind escape hatch to make the slice pass.

## 9. Foundation C — framework-owned effective declarations

### 9.1 Exact outward contract

Replace the two effective-runtime methods on `Discoverable`:

```go
InputPorts() []Port
OutputPorts() []Port
```

with one declaration method:

```go
Ports() PortConfig
```

At the end of this slice `PortConfig` contains only `Inputs` and `Outputs`, both `[]PortDefinition`. Delete
`PortConfig.KVWrite`; write declarations are ordinary outputs. Do not add `Registration.DefaultPorts`, a `KVRead`
side lane, or another declaration container.

Components return effective semantic declarations after applying their component config with the one merge contract
below. They do not construct runtime `Port` values. Factory construction necessarily happens first; the registry then
resolves and validates the returned declarations before it registers, initializes, or starts the component.

### 9.2 Exact merge contract

Foundation B replaces the old slice merge helper with:

```go
func MergePortConfig(defaults, overrides PortConfig) (PortConfig, error)
```

The contract is deliberately small:

- input and output groups are merged independently by non-empty port name;
- defaults and overrides must each have unique names in their direction;
- an override is a complete replacement for an existing named definition, not a field patch;
- the override kind must equal the default kind and its direction cannot move;
- overrides for unknown names fail instead of inventing component behavior;
- omission retains the default; there is no deletion sentinel;
- default ordering is preserved; and
- the result is defensively cloned before return.

Components whose optional ports depend on non-port configuration select their default declaration set before calling
the helper. Each component stores that effective `PortConfig`; `Ports()` returns a clone, and runtime subject/resource
helpers read the same stored config. There is no second precedence rule in the registry.

### 9.3 Snapshot owner and lifecycle

`component.Registry` owns the resolved snapshot for each instance generation:

1. `Registration.Factory` constructs the component from config.
2. Before registry insertion or any `Initialize`/`Start` call, the registry calls `Ports()` exactly once, deep-clones
   the declarations, validates them, and resolves each through `resolvePort`.
3. The registry atomically stores the component and its immutable input/output `[]Port` snapshot as one instance
   record. Capability and resource-conflict facts are derived during that store.
4. Registry read APIs return defensive deep clones; no consumer receives the retained slice or mutable interface
   pointers.
5. Flowgraph construction and ComponentManager management output read the registry snapshot, never call `Ports()` and
   never re-resolve.
6. On config restart, the replacement factory/declaration/resolution pass completes before the registry atomically
   replaces the old instance record. Failure publishes no partial snapshot. Removal removes component and snapshot
   together.
7. The registry exposes a replaying snapshot observer for message-logger. Subscription atomically delivers one complete
   clone of current instance snapshots, including an empty set, followed by complete generation-stamped replacements
   after every successful add, restart, or removal. Delivery is latest-state, not an event log: a slow observer may
   skip intermediate generations but always receives the newest complete state. Registry mutation never blocks on an
   observer, and observer cancellation releases its channel.

“Before allocation” in earlier drafts is withdrawn. The enforceable boundary is after factory construction and
before registration, initialization, start, subscription, bucket acquisition, or other component I/O.

### 9.4 Atomic migration

Because no compatibility interface is permitted, migrate every production component, mock, test, registry consumer,
flowgraph consumer, ComponentManager consumer, documentation example, and schema in one breaking slice. Go interface
conformance is the migration census.

Truthful durable dependencies include:

- graph-clustering `kv-read` inputs for `ENTITY_STATES`, `OUTGOING_INDEX`, and `INCOMING_INDEX`;
- gated-DAG's optional `ENTITY_STATES` watch/read dependency with optionality preserved;
- agentic-tools and graph-query exact reads made truthful in Foundation B; and
- agentic-loop and other proven KV writes moved into ordinary outputs.

The implementation inventory must enumerate the remaining component-specific declarations before editing. It may add
only dependencies proven by runtime acquisition; declarations do not become provisioning authority.

### 9.5 Shared consumers and message-logger snapshot observation

Registry resource tracking/capabilities, flowgraph construction, and ComponentManager management output read the same
retained snapshot. Component runtime helpers use the same effective `PortConfig` that produced it.

Delete message-logger's raw component-config parser. When its existing `"*"` auto-discovery sentinel is configured,
message-logger subscribes to the registry observer during `Start`:

- the initial replay makes service construction order irrelevant;
- an empty registry means no auto-discovered NATS subscriptions yet, not startup failure;
- each complete generation is projected from normalized ports into only declared `nats`, `nats-request`, and
  `jetstream` subjects plus their port metadata;
- explicit configured subjects are unioned with, and remain independent of, the discovered set;
- additions are subscribed before obsolete discovered subscriptions are drained; and
- removal/restart reconciles the set without reading component config or calling `Ports()` again.

No NATS `>` account-wide subscription is implied by `"*"`. Undeclared product traffic and inbox/reply subjects remain
outside the default unless an operator explicitly names a matching subject. The observer carries current declaration
state, not message payloads or component lifecycle policy.

Other message-logger boundaries remain:

- generic caller-selected must-exist KV query/watch remains;
- product/application buckets remain readable;
- `COMPONENT_STATUS` is not recreated;
- product middleware remains the authorization owner; and
- #472 filtering and #587 watcher/shared-view scaling remain separate.

The snapshot is declaration truth only. Component code continues to acquire `OpenCatalogReader`, watches, writers,
and stores through owner-local runtime paths and applies local degraded/retry policy. The snapshot validates declared
shape; it does not claim to observe or police later resource acquisition.

### 9.6 Exported break and adopter path

External components implement one declaration method instead of two runtime-rendering methods. If they do nothing,
they fail to compile. The new method returns configuration-shaped semantic declarations; the shared merge helper owns
override rules, and the registry owns kind resolution, conflicts, serialization, snapshot lifetime, and inspection.

At boot, a declaration/config error is a typed component/port error before initialization or I/O. Missing derived data
after boot remains an eventual-consistency outcome handled by the owning component; the port layer does not turn it
into global failure.

### 9.7 OpenSpec contract delta

The component-discovery/runtime contracts gain these requirements:

- A component SHALL expose one effective `PortConfig` through `Ports()` and SHALL use that same stored config for its
  runtime subject/resource helpers.
- The framework SHALL resolve declarations after factory construction and before registration, initialization, start,
  or component I/O.
- The registry SHALL own one immutable snapshot per instance generation and SHALL replace/remove component and
  snapshot atomically.
- Flowgraph, capability/resource reporting, and ComponentManager SHALL consume registry snapshots without re-resolving.
- Port overrides SHALL follow the exact complete-replacement merge contract in section 9.2.
- The registry snapshot observer SHALL replay one complete current generation and then deliver latest complete
  generations without blocking registry mutation.
- Message-logger auto mode SHALL reconcile only declared normalized NATS/JetStream subjects from that observer and
  SHALL NOT infer subjects from component configuration or subscribe account-wide.

### 9.8 Verification

- Production searches for `InputPorts()` and `OutputPorts()` reach zero.
- Production/config searches for the removed `KVWrite` and ignored `kv_read` lanes reach zero.
- Cold-boot contract tests prove invalid declarations fail after factory construction but before registry insertion,
  initialization, start, subscription, bucket acquisition, or other I/O.
- Config-restart tests prove component and snapshot replace atomically, failed replacement exposes no partial snapshot,
  and removal deletes both.
- Clone tests prove mutation by a registry caller cannot change retained declaration/config data.
- Contract tests compare registry capability/resource facts, flowgraph, and ComponentManager views for every kind.
- Message-logger tests cover construction before and after components, empty initial replay, add/restart/remove,
  generation coalescing, explicit-subject union, metadata, and shutdown cancellation.
- Default-boundary tests prove undeclared product traffic and inbox/reply subjects are not captured, while raw component
  configs are never parsed.
- Graph-clustering declarations exactly match its three current-state readers.
- Store federation remains provider/reader federation rather than exact KV identity matching.
- `task e2e:all` and `task e2e:research-graph` are green before the breaking slice lands.

### 9.9 Stop condition

Stop if framework resolution must predict runtime-selected store providers, provision a component-owned resource, or
choose a component's absent/stale/poison response. Those are evidence that the declaration boundary is absorbing
runtime policy. Return for an owner ruling; do not add a second path.

## 10. Readiness and status disposition after the slices

The fresh inventory changes the old readiness premise: `graph/readiness` already owns shared acquisition, explicit
producer keys, last-known/current outcome state, freshness, malformed/transport errors, rebind, publishers, and sets.
This roadmap therefore adds no new readiness primitive.

The durable boundary remains:

- shared: bucket acquisition, envelope decoding, absent/unknown/fresh/stale/malformed/transport classification;
- owner-local: blast radius, retry/defer/degraded posture, and gateway presentation; and
- deployment-local: which producer keys are required.

Do not fold component health or domain lifecycle into `GRAPH_STATUS`. Do not add a global mandatory-producer list.
Issues #795, #820, and #868 must be re-evaluated after Foundation C rather than assumed into these slices.

## 11. Issue effects and exclusions

- #859 port interpretation drift spans Foundations B and C. Foundation B centralizes the language; close #859 only
  after Foundation C deletes message-logger's last raw-config interpreter.
- #862 sealing `Discoverable` is Foundation C's target, using the merged tree rather than the absent
  `Registration.DefaultPorts` premise.
- #620's `kv_read`/`KVWrite` portion is addressed. Re-inventory its remaining claims at the stop gate.
- #810 discovery under stream coverage is re-evaluated against the effective snapshot after Foundation C. No behavior
  is assumed here.
- #842's `tool.list` subject move remains deferred until the #810 re-evaluation.
- #717's superseded component-status plane is deleted. Do not reopen retention/ownership machinery.
- #795, #820, and #868 are not implemented. Preserve and later re-inventory the current `graph/readiness` foundation.
- #472, #587, and message-logger authorization remain separate; no filter, watcher model, shared view, or framework
  authorization change is included.
- #688-#690, #725, #736, #765, and #882-#886 remain outside these slices with no inferred disposition.

Explicitly excluded from all three slices:

- index result/data-structure refactors;
- graph query DTOs, absence, pagination, aliases, or subject changes;
- GraphQL, MCP, or gateway query behavior;
- mutation semantics, retries, ownership, auto-stubs, or a new stream;
- recovery, checkpoint, snapshot, backup, restore, or NATS-CLI work;
- message-logger authorization, filtering, or scaling; and
- downstream repository migration.

## 12. Mandatory stop/remap checkpoint

After Foundation C merges:

1. re-inventory exported port APIs, every checked configuration, effective-snapshot consumers, graph readiness,
   message-logger, and live issue premises from the merged tree;
2. record deletion counts and remaining interpreter counts rather than using completed-task labels as evidence;
3. run the ten downstream repositories only as the approved API/feature-parity holdout set, then plan their clean
   migration against the stable target;
4. reassess #620, #795, #810, #820, #842, #859, #862, and #868; and
5. require a new accepted inventory and owner-approved roadmap before index, query-client, GraphQL, gateway, or the
   #688-#690/#882-#886 clusters begin.

The checkpoint is a hard program stop, not a documentation task.

## 13. Evidence gates per implementation slice

Each slice requires:

1. a SemStreams developer implementation from the exact owner-approved slice;
2. focused tests written against behavior and explicit synchronization;
3. `task lint`, `go test -race ./...`, schema generation/diff, and contract tests;
4. the named relevant E2E tiers because each slice is breaking;
5. an independent SemStreams reviewer pass; and
6. an implementation report recording deviations and the actual merged baseline before the next slice begins.

No slice begins merely because the prior roadmap paragraph exists. The prior slice must be merged and its assumptions
checked against the resulting tree.

## 14. Owner rulings required

1. Accept Option C, the three-slice foundation-first sequence, and the mandatory post-C stop.
2. Approve clean deletion of `COMPONENT_STATUS` and all reporter APIs/calls while preserving generic message-logger KV
   access, ComponentManager health, `GRAPH_STATUS`, and domain lifecycle.
3. Approve exported `PortKind`, typed concrete configs including `KVReadPort`, the common declaration envelope,
   canonical-only spellings, typed boot failure, the unexported resolver, and deletion of the old builder signature and
   dead NATS config types.
4. Approve the clean `Discoverable` break to `Ports() PortConfig`, complete-replacement merge rules, deletion of
   `PortConfig.KVWrite`, and registry ownership of one immutable snapshot per instance generation.
5. Approve deletion of message-logger raw-config prediction through the replaying registry snapshot observer. Existing
   `"*"` auto mode remains limited to declared normalized NATS/JetStream subjects; it does not become account-wide.
6. Confirm that current `graph/readiness` already satisfies the shared acquisition/outcome-classification boundary and
   that response policy remains owner-local; no readiness implementation is authorized here.
7. Confirm that #810/#842, indexes, queries, GraphQL, other message-logger behavior issues, and downstream migration
   wait for the mandatory remap.

Until those rulings follow an independent `DESIGN REVIEW PASS`, this document is not implementation authority.
