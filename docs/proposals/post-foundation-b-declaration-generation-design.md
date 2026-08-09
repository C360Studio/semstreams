# Post-Foundation-B declaration generation: target-state design

**Artifact state:** OWNER-ACCEPTED DESIGN — version-arbitration wording corrected; pending refreshed independent
identity confirmation before OpenSpec or implementation handoff.
**Repository:** `C360Studio/semstreams`.
**Repository baseline:** `ee3b43ce67f3ee6b39547317529da7ce1a783233`.
**Amended design identity:** recorded externally after this revision is materialized for fresh independent review.
**Accepted inventory:** `docs/proposals/post-foundation-b-remap-inventory.md`, 595 lines, 36,227 bytes,
SHA-256 `58e44190937c247a30ae5ce55621da27cddd113da6da858d64a2e9bc51bdd7fb`.
**Inventory review:** `docs/proposals/post-foundation-b-remap-inventory-review.md`, verdict `INVENTORY PASS`.
**Accepted owner ruling:** the complete target is accepted. Services are immutable process-composition units;
`services.*` is durable desired next-boot configuration only; components remain runtime-configurable. This supersedes
the rejected dynamic-route seam and prior no-restart-status non-goal.
**Status:** owner-accepted but not yet an implementation handoff; the wording-only revision needs refreshed independent
identity confirmation.
**Slice C census amendment (2026-08-09):** OWNER-APPROVED. Four shipped files whose enabled factories had no
production registration were retired without aliases, synthetic factories, or substitutes. The production-loader and
real-factory census now covers 21 shipped configurations and is pending Slice C independent review.
**Downstream holdouts:** the ten paused projects remain parity evidence after the framework contract is accepted. They
do not shape or block it.
**Suspended work:** `semantic-tier-split` remains suspended, frozen, and non-executable.

The accepted surface inventory, same-class collision tables, adopter seam inventory, measurements, negative searches,
and open evidence questions are incorporated verbatim as Appendix A when this draft is materialized. That checkpoint
remains historical; the 2026-08-09 amendment in §5.4 supersedes only its shipped message-logger census premise.

## 1. Decision scope

Foundation B already established:

- one strict twelve-kind port grammar;
- one exported resolution path;
- one normalized immutable facts projection;
- component instances that retain resolved effective `[]Port` values; and
- shared classification without concrete-type switches.

The residual defect is narrower:

- a successful component instance generation has no single retained declaration snapshot;
- Registry, ComponentManager, flowgraph, capability publication, and message-logger observe declarations at different
  moments;
- restart and removal do not update one component-plus-declaration record;
- message-logger predicts subjects from construction-time raw configuration; and
- stream planning is a distinct pre-component-construction operation.

The message-logger is a service, so correcting its declaration observation also reaches one already-colliding service
lifecycle seam. Services are process-composition units: desired service configuration may change durably while a
process runs, but the running service set, its HTTP routes, and its OpenAPI surface do not change until restart.
Components remain the runtime-configurable flow units.

This design addresses the declaration/effective-configuration collision class and that bounded service-composition
seam only. It does not combine either class with health/readiness, `GRAPH_STATUS`, indexes, hierarchy, research
ordering, or retention.

## 2. Measured premises

- **Residual gap:** authorship/lifetime plurality, not grammar plurality. Accepted inventory `:23-45`.
- **Component storage:** components already retain resolved effective ports. Accepted inventory `:74-81`.
- **Registry lifetime:** Registry has no instance-generation snapshot and re-reads component ports. Accepted inventory
  `:83-123`; `component/registry.go:101-117`, `:253-300`, `:594-607`, `:938-979`.
- **Logger prediction:** message-logger parses raw config once and can skip invalid rows. Accepted inventory `:125-132`;
  `service/message_logger.go:302-359`.
- **Planning boundary:** stream planning is pure, pre-construction, and config-derived. Accepted inventory `:134-140`;
  `config/stream_bounds.go:180-310`.
- **Boot order:** production boot provisions streams before Registry and component construction.
  `cmd/semstreams/main.go:90-147`.
- **Config synchronization:** `config.Manager.Start` synchronizes KV state into its `SafeConfig` before composition,
  but `cmd/semstreams/main.go` later passes the original file-loaded `cfg` to service setup. `SafeConfig.Mutate`
  replaces the protected pointer rather than mutating that original object. Service construction can therefore use
  stale file state after a successful desired-state sync. `config/manager.go:182-280`, `:927-963`;
  `config/config.go:130-142`; `cmd/semstreams/main.go:138-150`, `:249-252`, `:535-587`.
- **Config authority arbitration:** on subsequent boot, `config.Manager.Start` selects file versus KV by semantic
  version. A newer file version pushes file state; an older or equal file version selects KV. Therefore a file content
  edit with no version bump is superseded by KV on restart. `config/manager.go:234-278`. This is intentional safety:
  file content may overwrite KV only when its version is newer. Operators changing file content must advance version;
  later documentation or diagnostics may make that requirement more visible.
- **Dynamic-service path:** `service.Manager.ConfigureFromServices` subscribes to `services.*`, but its mutation
  consumer starts only from `Manager.Start` when `isHTTPManager` is true. Production sets neither condition; only
  tests do. The dynamic add/enable/disable/remove/apply path is therefore not active in production.
  `service/service_manager.go:76-140`, `:707-871`.
- **Service configuration owner:** `config.Manager` legitimately watches `services.*` and updates `SafeConfig` before
  notifying subscribers. That path remains the durable desired-configuration owner; it is not a running-service
  mutation contract. `config/manager.go:283-328`, `:405-481`, `:509-556`.
- **Runtime service configuration:** `service.RuntimeConfigurable` has exactly two production implementations,
  message-logger and metrics, and both advertise an inner `enabled` update whose apply path is a no-op. Component hot
  configuration uses a separate anonymous method pair and does not depend on this interface.
  `service/configurable.go:19-32`; `service/message_logger.go:705-853`; `service/metrics.go:201-278`;
  `service/component_manager_http.go:729-777`.
- **Dynamic service methods:** exported `StartService`, `StopService`, and `RemoveService` have no production callers
  once the inactive watcher path is removed. Boot uses create/register plus `StartAll`; shutdown uses `StopAll`.
  `service/service_manager.go:507-616`.
- **Composition paths:** configurable services enter through `CreateService`; fixed composition-root services enter
  through `RegisterInstance`; `StartAll` creates the mandatory `component-manager` before starting the set; and the
  framework binary registers fixed `milestone` before configurable services. `service/service_manager.go:163-205`,
  `:254-318`, `:390-412`; `cmd/semstreams/main.go:228-252`, `:535-587`.
- **Unsealed mutation:** `CreateService` and `RegisterInstance` currently remain callable after startup, and
  `RegisterInstance` is void and overwrites duplicate instances. No retained composition seal or complete sorted
  service identity set exists. `service/service_manager.go:163-205`.
- **KV service overlay:** `syncFromKV` applies present KV keys onto the existing file-loaded `SafeConfig`; it does not
  clear `Services` before applying current `services.*` keys. When version arbitration selects KV, a service deleted
  from KV can therefore survive from the file map. `config/manager.go:927-963`.
- **Static HTTP composition:** Manager enumerates the constructed set, binds routes only for services implementing
  `HTTPHandler`, and obtains per-service OpenAPI from that interface's `OpenAPISpec` method. With immutable process
  composition, those subsets cannot drift through a service config mutation during the process lifetime.
  `service/service_manager.go:980-1027`, `:1207-1215`, `:1272-1377`.
- **Current service observability:** `GET /services` already reports the running service name, status, health, and
  count. It is the smallest existing surface on which to report whether desired next-boot composition differs.
  `service/service_manager.go:1487-1504`.
- **Factory contract:** Registry documents factories as I/O-free. `component/registry.go:176-185`.
- **Shared views:** shared consumers must use normalized facts. `openspec/specs/component-discovery/spec.md`,
  “Shared component views consume one normalized port projection.”
- **Provisioning projection:** port-derived stream declarations currently consume canonical normalized facts.
  `openspec/specs/stream-provisioning/spec.md`, “Port-derived stream declarations consume canonical normalized
  facts.”
- **Rejected old shape:** the old `Ports() PortConfig` plus observer shape is not established. Accepted inventory
  `:34-42`.

## 3. Options considered

### Option 0 — Do nothing

Keep component-local effective ports, repeated Registry reads, construction-time message-logger discovery, and
pre-construction stream planning unchanged.

Cost:

- capability publication can observe a different moment from conflict registration;
- runtime add/restart/remove remains invisible to message-logger auto-discovery;
- flowgraph and management views continue re-reading component state;
- declaration drift remains diagnosable only through symptoms.

This preserves the smallest code surface but does not resolve the evidenced lifetime defect.

### Option 1 — Registry-owned resolved generation snapshot, retaining the current component API

Keep `Discoverable.InputPorts()` and `OutputPorts()`. During successful registration, call each exactly once, clone the
returned resolved ports, derive normalized facts, validate conflicts, and retain the component plus declaration as one
Registry instance record.

All post-construction consumers read defensive copies of that record. Message-logger observes complete current
snapshot sets. Stream planning remains a separate config-time provisioning operation and continues to classify
`PortConfig` through the same shared resolver/facts projection.

Cost:

- adds a narrow Registry read/observation surface;
- requires declarations to remain immutable within one instance generation, with declaration-affecting live updates
  rejected before mutation or performed as prepared replacement generations;
- retires ComponentManager's parallel resource tracker so Registry is the sole declaration/resource admission owner;
- conditionally expands explicitly enabled message-logger `"*"` from raw-config-explicit subjects to all effective
  declared subjects, including factory defaults, while retaining raw message bodies;
- removes or internalizes identity-free direct `Registry.RegisterInstance(name, component)` admission;
- requires restart/removal paths to update the instance record atomically;
- leaves stream planning intentionally separate because it runs before a live generation exists;
- does not make the framework police owner-local acquisition or degraded-state policy;
- makes running service composition immutable until process shutdown, while retaining durable desired
  `services.*` updates for the next boot;
- removes the dead service hot-mutation surface and reports a pure current desired-versus-boot comparison through the
  existing `GET /services` response;
- requires service construction to consume the effective desired configuration after `config.Manager.Start`, fixing
  the current stale-file restart defect;
- seals one complete service composition before any service starts or contributes routes/OpenAPI; and
- resolves only the outer service map while leaving inner config validation/defaulting with each constructor.

Benefit:

- resolves the measured runtime lifetime plurality without migrating 38 component implementations to a second
  declaration shape;
- removes message-logger’s prediction of current runtime subjects;
- preserves existing flow/component runtime configurability while deliberately ending service runtime
  configurability;
- introduces no durable state, recovery subsystem, lease, election, or cross-node authority.

All listed corrections above are necessary to Option 1. Omitting one preserves a second declaration lifetime,
resource authority, undisclosed observation seam, identity-free admission path, or false service lifecycle contract
and therefore refutes the option.

### Option 2 — Replace the two component methods with `Ports() PortConfig`

Apply the old Foundation C clean break, make components return definitions, and have Registry resolve them.

Cost:

- migrates every component and adopter despite components already retaining correct resolved ports;
- converts resolved runtime truth back into configuration-shaped declarations before resolving it again;
- does not solve the pre-construction stream-planning timing mismatch;
- creates a breaking external bill without measured evidence that the replacement improves runtime acquisition
  consistency.

This is not recommended. Foundation B invalidated its original grammar rationale.

### Option 3 — Construct every component before provisioning, then provision from its Registry snapshot

Reorder startup into prepare, provision, initialize, and start phases. Runtime additions would follow the same staged
path.

Cost:

- materially restructures boot and runtime reconciliation;
- requires every dependency needed by factories to exist before stream provisioning;
- expands a declaration correction into lifecycle scheduling;
- creates failure and rollback questions unrelated to the accepted collision class.

This is coherent only if an owner rules that component-default JetStream outputs must become provisioning authority
before activation. It is not the smallest current correction.

## 4. Recommendation

Adopt Option 1.

The target has two deliberately distinct facts using one classifier:

1. **Configured provisioning intent** exists before component construction. The stream planner reads canonical
   `PortConfig`, resolves it through the shared binding, and applies stream-owner policy.
2. **Accepted runtime declaration** exists only after a factory successfully constructs a component generation.
   Registry captures the component’s resolved effective ports once and retains the immutable generation snapshot.

These are not two interpretations of one runtime fact. They are facts at different lifecycle boundaries. They must
share declaration classification, but they must not share owner policy responses.

The stream provisioner decides creation, bounds, drift, and refusal locally. Components decide acquisition,
absent/stale/poison handling, retry, and degradation locally. Registry records declared shape; it does not provision,
poll, repair, or enforce owner policy.

The Registry snapshot is the successfully admitted shape of one component generation. It does not state that the
component is enabled, started, healthy, ready, or part of an atomic group or cohort. It does not encode provider phase,
`Health`, `GRAPH_STATUS`, capability completeness, or orchestration progress. Activation and readiness consumers must
read their lifecycle-, health-, or capability-specific authorities rather than infer them from declaration presence.

Services follow the complementary boundary. They are immutable process-composition units, not hot flow units.
`services.*` remains durable desired configuration, but a running process never adds, enables, disables, removes, or
reconfigures a service from those writes. The next process boot consumes the current desired state. Component
configuration continues to use its existing runtime update contract.

## 5. Target contract

### 5.1 One accepted declaration per instance generation

A successful Registry instance record contains:

- instance name and factory identity;
- component reference;
- cloned effective input ports;
- cloned effective output ports;
- normalized facts derived from those exact clones;
- exclusive-resource facts derived from the same facts; and
- a process-local generation identifier sufficient to distinguish replacement.

The record is immutable for the lifetime of that generation. Registration shall:

1. construct the component through the existing factory;
2. call `InputPorts()` and `OutputPorts()` exactly once;
3. deep-clone and validate both results;
4. derive normalized facts and resource conflicts;
5. publish no instance record if any step fails; and
6. store the component, declaration, and derived resource state together.

An absent or disabled component configuration constructs and admits no generation. An admission failure publishes no
generation. If admission succeeds but component start later fails, Registry may retain the honest admitted shape for
inspection; its presence does not mean ready. Readiness consumers must use the component lifecycle/health authority,
not declaration presence.

An in-place `UpdateConfig` or `ValidateConfigUpdate` plus `ApplyConfigUpdate` path may proceed only after the proposed
configuration's normalized port facts are proven exactly equal to the retained generation snapshot, before any
component or retained-configuration mutation. A declaration-affecting update must instead either:

- fail with a typed `declaration_change_requires_replacement` error before mutating the component or retained
  configuration; or
- prepare a complete replacement component and declaration off-Registry, then use the replacement operation in
  §5.3.

The runtime must never mutate a live component and then recapture its declaration. That sequence creates a window in
which successful management state and the retained generation disagree.

The snapshot is process-local runtime state. It is not graph authority, NATS state, an audit log, a recovery record, or
a cross-node ownership claim.

### 5.2 One runtime read and resource-admission path

Registry conflict reporting, local capability publication, ComponentManager management responses, and flowgraph
construction consume Registry snapshots. They do not call component port methods or resolve definitions again.

Registry is the sole owner of declaration-derived exclusive-resource admission. ComponentManager's parallel
`resources` map and its `checkPortConflicts`, `registerPorts`, `unregisterPorts`, and port re-read bookkeeping are
retired. ComponentManager asks Registry to admit, replace, or remove an instance and reads defensive Registry clones;
it does not retain or derive a second resource projection.

Capability publication captures a defensive snapshot before starting asynchronous publication. The goroutine must not
re-read a component that could already have been replaced or removed.

Runtime helpers inside a component continue using the component’s stored effective configuration. Registry does not
inspect live subscriptions, bucket handles, or Store bindings.

### 5.3 Restart and removal

A declaration-neutral live update retains the current generation. A port-affecting update is rejected before mutation
or prepared as a complete replacement generation.

A failed replacement preparation publishes no new declaration snapshot, leaves the old component and retained
configuration untouched, and exposes no partially derived resource state.

A successful replacement assigns a new process-local generation and atomically replaces the Registry-visible
component, declaration, factory identity, and derived resource facts as one mutation.

Removal deletes the component record and its declaration/resource projections together.

This atomicity is about local inspection truth. It does not promise zero service interruption, distributed
transactions, rollback recovery, or simultaneous resource acquisition.

### 5.4 Message-logger auto-discovery

Message-logger is an optional diagnostic. It is chatty, retains raw message bodies, expands observation, and has
security and resource consequences similar to an operator inspection tool such as the NATS CLI. It is not a
production/default service.

#### Configuration findings and dispositions

- The global application log level controls only `slog` emission. It does not activate message-logger.
- The inner message-logger `log_level` field is unused and dead. Remove it from accepted config, schema, update, and
  runtime-helper paths; strict configuration rejects it. No alias, deprecated field, or compatibility shim remains.
- The inner message-logger and metrics runtime `enabled` fields are currently advertised but succeed as no-ops.
  Remove them from accepted config, schema, update, and runtime-helper paths; strict configuration rejects them. No
  shim remains.
- For optional services, outer `types.ServiceConfig.Enabled` is the sole next-boot activation input. Mandatory
  framework services are not optional activation choices.
- The loader-injected default enabled message-logger entry is removed. Omission means off.
- Retained message-logger fields `monitor_subjects`, `max_entries`, `output_to_stdout`, and `sample_rate` are
  next-boot configuration only. Retained metrics fields `port` and `path` are next-boot configuration only. Their
  schema remains useful for discovery and validation, but carries no runtime marker and promises no hot application.

When the outer service is enabled and its actual `Start` executes, the logger attaches its Registry observer and
logger-owned subscriptions independently of the global application log level. Only a started logger in `"*"` mode
subscribes lazily to Registry declaration snapshots. Stop detaches the observer and every logger-owned subscription.
Component declaration add, replace, and remove remain dynamically observed while the logger is started.

When the service is omitted or disabled before activation, there is no logger instance, buffer, Registry observer,
NATS/message subscription, runtime HTTP/KV/SSE route, or logger delivery work/backpressure. Constructor and static
schema metadata may remain registered because they are not runtime activation or capture surfaces.

For a started logger in `"*"` mode, discovery changes from scanning raw configured port rows to all effective declared
subjects, including factory-default subjects omitted from component config. Because the logger retains raw message
bodies, this is an intentional data-capture expansion, not merely a metadata-source correction, and requires owner
acceptance.

The former static-mux/OpenAPI mismatch is resolved by the service-composition contract in §5.5. The constructed
service set cannot change while the process runs, so route binding and OpenAPI generation observe the same immutable
composition. No dynamic mux or route-removal machinery is required.

Conditional on the outer service being enabled and started in `"*"` mode, the complete logger census covers all 21
production-constructible shipped component configurations, every enabled component, both port directions, and the
`nats`, `nats-request`, and `jetstream` kinds:

- raw configuration contains 385 subject rows, 243 summed per-configuration exact keys, and 51 distinct global
  strings;
- effective generation declarations contain 561 subject rows, 378 summed per-configuration exact keys, and 66
  distinct global strings;
- the delta is 176 factory-default rows across nine configurations, 135 net-new per-configuration exact keys, 15
  net-new global strings, and zero removals;
- the 176 added rows comprise 60 JetStream inputs, 61 JetStream outputs, 18 NATS inputs, 28 NATS outputs, and 9
  NATS-request inputs across 16 default families;
- five loop/dispatch default families duplicate exact strings in eight configurations, so 40 added rows collapse
  during exact-key deduplication;
- one governance `user.response.*` row overlaps an existing raw exact key, producing one further exact-key collapse;
- each of the nine affected configurations gains 15 exact keys, while the other 12 configurations are unchanged; and
- no added declaration is bare `*`, bare `>`, `_INBOX`, a reply subject, or an undeclared subject. NATS-request adds
  only its request subject.

`configs/agentic.json` also has three wildcard-containment overlaps that exact-key deduplication cannot collapse:

- new `agent.toolcall.proposed.*` is contained by raw `agent.toolcall.proposed.>`;
- raw `agent.toolcall.approved.*` is contained by new `agent.toolcall.approved.>`; and
- raw `agent.toolcall.rejected.*` is contained by new `agent.toolcall.rejected.>`.

The started logger must handle those overlaps deliberately so the same message is not captured twice. Runtime subject
inspection must expose the resolved union and its overlap handling rather than presenting only the pre-deduplicated
declarations.

The 21-config scope is the owner-approved correction to the earlier 25-config premise. Exactly these four configs were
retired because their enabled `graph`/`graph-processor` factories have no production registration:

- `configs/http-gateway-semantic-search.json`;
- `configs/semantic-basic.json`;
- `configs/examples/bm25-semantic-search.json`; and
- `configs/examples/pathrag-graph-traversal.json`.

No alias, synthetic factory, substitute config, or census exclusion replaces them. The production-loader and
real-factory completeness pass also bound three owner-approved prerequisite repairs: documented WebSocket duration
decoding and explicit `ws_control` in `configs/cloud-federation.json@1.0.2`; explicit `ALIAS_INDEX` in
`configs/hello-world.json@1.1.1`; and core-NATS declarations matching the mission-command implementation in
`configs/lifecycle-flow.json@1.1.1`. The frozen reproducible evidence is
`service/testdata/message_logger_subject_census.json` at baseline
`f2b7c4506ae78b1b8ace9fbc581994a2d14f1d55`.

Subscription behavior:

- initial subscription receives one complete clone of the current snapshot set, including an empty set;
- later successful add, replace, and removal mutations publish the newest complete set;
- delivery is latest-state and coalescing, not an event log;
- Registry mutation never blocks on message-logger;
- message-logger reconciles only declared `nats`, `nats-request`, and `jetstream` subjects;
- explicit operator subjects remain unioned with discovered subjects;
- undeclared product traffic, inboxes, and replies are not captured implicitly; and
- cancellation releases the observer.

Conditional on the outer service being enabled and started in `"*"` mode, the migration contract compares the exact
old and effective discovered subject sets for every shipped configuration, including exact-key deduplication and
wildcard containment. Replacement and removal must reconcile the complete current set. This 176-row logger
observation census spans both directions and three NATS kinds. It is distinct from the 61-output-row,
61-covered/zero-uncovered stream-provisioning census in §5.7.

### 5.5 Immutable service composition and next-boot truth

Services are process-composition units. `services.*` remains durable desired configuration, and `config.Manager`
continues accepting and synchronizing whole-service entries. A write changes only what the next process boot should
construct. It never mutates the running service set or a running service instance.

#### Outer desired-service resolution

Boot first starts `config.Manager` and completes its existing file/KV authority selection. It then reads `Services`
from the resulting `SafeConfig`, never from the original file-loaded `cfg`, and passes that map through one pure
**outer** `ServiceConfigs` resolver.

The outer resolver:

- treats the `ServiceConfigs` map key as the sole service identity;
- removes redundant `types.ServiceConfig.Name` from the type and every config/schema/test/documentation surface;
- deep-clones the map and canonicalizes each structurally valid raw inner JSON value for stable equality;
- when absent, materializes mandatory configurable `component-manager` and the already-required `service-manager`
  manager-configuration entry with `Enabled: true`, while preserving an explicitly present `Enabled: false` for boot
  validation and desired-state comparison;
- applies any other existing outer default policy, including the current optional metrics default, without making that
  service mandatory;
- never injects message-logger; and
- returns both enabled and disabled entries without decoding, defaulting, or semantically validating service-specific
  inner fields.

For optional services only, `types.ServiceConfig.Enabled` is the sole activation owner. The exact current mandatory
managed-service set contains `component-manager`. The `service-manager` entry configures always-present manager/process
infrastructure; it is not an optional managed-service activation. A configured `component-manager` or
`service-manager` entry with `Enabled: false` is invalid at boot. A successful boot always has active manager
infrastructure and an active `component-manager`; optional services are constructed exactly when their resolved outer
entry is enabled.

Materialization is identical for the boot desired map and every current desired read. Therefore omission at boot
produces an enabled mandatory entry in the retained boot map; a later explicit `component-manager` false remains false
and compares as `disable`; deleting that override materializes true again and clears the comparison. The same
absent-versus-explicit-false rule applies to the required `service-manager` outer entry.

Constructors remain the sole owners of service-specific inner validation and defaults. Each enabled configurable
service constructor receives the exact canonical raw config emitted by the outer resolver. There is no per-service
codec registry, normalizer callback, schema-driven default engine, or second inner-config interpretation layer.
Disabled entries are never sent to a constructor.

The manager retains an immutable clone of the exact resolved configurable-service map used by the composition root.
That desired-at-boot map is separate from the full sealed runtime identity set: it includes disabled configurable
entries and the manager's own outer configuration, but not fixed or mandatory services that have no configurable
entry.

The current binary violates this boundary: after `config.Manager.Start` has synchronized desired KV state into
`SafeConfig`, service setup still consumes the stale original file-loaded `cfg`. The implementation must instead
resolve, construct, and retain the map selected after Start. It does not copy the pre-sync file object or later desired
state.

This guarantee begins only after `config.Manager` has selected and synchronized effective desired state under its
current file/KV authority rules. It does not mean every file edit applies on restart: with equal versions, KV still
wins, and a file-content change requires advancing the file version. That arbitration is accepted intentional safety
and remains outside this implementation change. Later non-foundational work may improve documentation or diagnostics;
this increment fixes stale post-selection consumption without changing which source wins.

When that existing arbitration selects KV, the selected **Services map** is authoritative replacement state. Config
Manager starts service synchronization from an empty Services map and loads only current `services.*` keys. It does
not overlay them onto file-loaded services. Thus a service absent from KV remains absent even if the file still names
it. This is a bounded correction to service desired-state synchronization, not a claim that KV already replaces every
other top-level config section or a redesign of global file/KV authority.

| Owner-noted finding | Disposition in this increment |
|---|---|
| Stale post-sync `cfg` read | Fix: construct and snapshot from post-Start `SafeConfig`. |
| Pretend service hot mutation with static routes | Fix: immutable composition and next-boot desired state. |
| Equal versions select KV over file edits | Preserve accepted safety policy; improve clarity later if useful. |
| KV-selected services overlay file services | Fix: replace only the Services map from current `services.*` keys. |

#### Pre-start composition seal

Service composition has one bounded mutation phase before startup:

1. the composition root resolves the desired configurable-service map;
2. it calls `CreateService` for each enabled configurable service;
3. it calls `RegisterInstance` for fixed pre-built services such as `milestone`;
4. `StartAll` ensures every enabled configured optional service was constructed and the mandatory
   `component-manager` is present, creating it during this same pre-start phase if absent;
5. `StartAll` seals composition and retains the sorted complete service identity set; and
6. only after the seal may any service `Start`, route binding, or OpenAPI exposure occur.

`CreateService` and `RegisterInstance` remain exported only as composition-root APIs. Both reject duplicates and fail
with typed errors after the seal; `RegisterInstance` becomes error-returning. The clean break updates every caller and
retains no void wrapper, overwrite behavior, alias, deprecated method, or compatibility shim. No service instance or
identity may be added, replaced, or removed after sealing.

The composition root—not Manager inference—is responsible for registering whatever fixed services its binary
requires before the seal. For the current production framework root, that includes `milestone`, and production-root
tests prove the registration occurs. Manager introduces no fixed-service manifest, group, inferred requirement, or
generic completeness primitive; if a composition root omits one of its own fixed services, the seal freezes the actual
admitted set rather than discovering the omission.

`StartAll` fails before starting any service when an enabled configured optional service was not constructed, a
configured mandatory entry is disabled, or `component-manager` cannot be present. A successful seal retains two
distinct immutable facts:

- the resolved configurable-service map actually used by the composition root; and
- the sorted full identity set of every optional, mandatory, and fixed runtime service actually admitted before the
  seal.

The existing `GET /services` runtime rows match the full sealed identity set. Bound service routes match exactly the
sealed subset implementing `service.HTTPHandler`. Under the current interface, `HTTPHandler` also exposes
`OpenAPISpec`; generated per-service OpenAPI contributors therefore match that OpenAPI-capable sealed subset. Static
manager-owned framework endpoints are not service identities, and no global spec entry may invent an unsealed service
contributor. These views do not re-enumerate a mutable manager map or infer configured identities from the desired map.
Start failure after the seal may change lifecycle/health, but never composition identity.

The service manager removes its `services.*` subscription, config-update channel, watcher goroutine, old/new diff,
dynamic add/enable/disable/remove logic, and runtime apply helpers. Exported `StartService`, `StopService`, and
`RemoveService` are removed. Boot remains create/register followed by `StartAll`; shutdown remains `StopAll`. No
internal alias, deprecated method, or compatibility shim remains.

`service.RuntimeConfigurable` is removed together with the message-logger and metrics implementations and their
dynamic tests/docs. `service.Configurable` and service schemas may remain for next-boot discovery and validation.
The service-only `PropertySchema.Runtime` marker is removed: every retained inner service knob is next-boot only.
Message-logger rejects inner `enabled` and `log_level`; metrics rejects inner `enabled`. Message-logger retains only
its actual next-boot knobs, and metrics `port` and `path` remain next-boot schema fields. Component Manager's separate
anonymous `ValidateConfigUpdate`/`ApplyConfigUpdate` method-pair contract is preserved unchanged for components.

`GET /services` remains the operator read surface and gains only two top-level fields:

- `restart_required`: true exactly when a restart is required to attempt to consume a current desired difference; and
- `pending_service_changes`: a sorted array of `{name, change}` entries, where `change` is exactly one of `add`,
  `enable`, `disable`, `remove`, or `reconfigure`.

The handler computes both fields on every read by passing the current `configManager.GetConfig().Get().Services`
through the same pure outer resolver and comparing it with the immutable boot desired map. It installs no watcher,
retains no change history, invokes no constructor, and does not infer from service health or lifecycle events.
Comparison emits at most one entry per service map key with these precedence rules:

- absent at boot and desired enabled: `add`;
- disabled at boot and desired enabled: `enable`;
- enabled at boot and desired disabled: `disable`;
- enabled at boot and absent from desired: `remove`; and
- enabled in both with different canonical raw inner JSON: `reconfigure`.

Absent-to-disabled, disabled-to-absent, and disabled-to-disabled edits do not affect running composition and therefore
emit nothing and require no inner-config validation. A simultaneous activation-state and config change is classified
by the activation transition, not as a second reconfiguration. Reverting desired state to the boot-effective value
clears the result immediately. A successful restart that consumes valid current desired state captures it as the new
boot map and starts with an empty pending set. The existing `services` array and the pending array are both sorted by
service name for deterministic output.

`restart_required` is not a restart-success prediction. An enabled unknown service or semantically invalid inner
config is still reported by its structural `add`, `enable`, or `reconfigure` classification. A later restart may fail
through existing registry/constructor validation. The read surface does not add `restart_blocked`, config-error
classification, speculative inner validation, or any promise that restart will succeed.

A desired `Enabled: false` for configured `component-manager` or `service-manager` is likewise reported structurally
as a pending `disable` restart attempt, but boot validation rejects it until corrected. There is no successful-consume
or clear-on-restart promise for that invalid desired state. Reverting it clears the pending comparison; every successful
boot has active manager infrastructure and an active `component-manager`.

This observation does not create a second service status system. Existing service status, health, `/services/health`,
readiness, and `GRAPH_STATUS` meanings are unchanged. No bucket, stream, new service, group, scheduler, or readiness
gate is added. `restart_required` reports only a difference between the immutable boot desired map and current desired
next-boot configuration.

### 5.6 Communication primitive ruling

The `kv-or-stream` heuristic was applied to declaration observation.

The data is a current local fact, fan-out capable, cheap, and idempotent, which would point toward KV if it needed
cross-process durability. It does not: the Registry is the source, the only present observer is in the same process,
and restart reconstructs the current set from configured components.

Therefore the target uses a bounded in-process replaying observer, not a KV bucket or JetStream stream. Adding either
would create unnecessary durable catalog, status, recovery, and operator surfaces.

### 5.7 Stream planning remains a separate bounded owner family

Stream planning continues to run from configuration before component construction and NATS provisioning. It consumes
only canonical `PortConfig` and normalized facts; it does not consume Registry generation snapshots and does not
become a runtime declaration observer.

The planner owns physical stream policy. It does not classify component readiness, lifecycle, index freshness, or
runtime acquisition.

No second port grammar, concrete-type switch, component-local stream-name derivation, alias, deprecated path, or
compatibility shim is permitted.

The shipped-flow census contains exactly 61 effective factory-default JetStream output rows absent from raw component
output configuration. All 61 derive stream `AGENT`; all 61 are covered by the explicit `config.streams.AGENT` subject
`agent.>`; zero are uncovered. `config/stream_bounds.go:223-243` admits explicit stream declarations before port
derivation at `:245-299`.

- **`agentic-loop`:** five default-only subjects across nine enabled shipped configurations, producing 45 rows.
  All are covered by explicit `AGENT` / `agent.>`:
  - `agent.created`;
  - `agent.failed`;
  - `agent.context.compaction`;
  - `agent.approval_pending`; and
  - `agent.toolcall.proposed`.
- **`agentic-dispatch`:** two default-only subjects across eight enabled shipped configurations, producing 16 rows.
  All are covered by explicit `AGENT` / `agent.>`:
  - `agent.signal`; and
  - `agent.approval_response`.
- **Total:** seven distinct subjects, 61 rows, 61 covered, zero uncovered.

The nine `agentic-loop` configurations are:

- `configs/agentic.json`;
- `configs/examples/research-graph-pipeline.json`;
- `configs/research-graph-e2e.json`;
- `configs/flows/crud-tools-test.json`;
- `configs/flows/deep-research-test.json`;
- `configs/flows/deep-research.json`;
- `configs/flows/lesson-example.json`;
- `configs/flows/ops-agent-test.json`; and
- `configs/flows/ops-agent.json`.

The eight `agentic-dispatch` configurations are the same set except `configs/agentic.json`.

For completeness, the census also found factory-default JetStream output rows that are fully explicit in shipped raw
component output configuration: 11 `udp` `nats_output` rows, 9 `agentic-model` `agent.response` rows, 9
`agentic-tools` `tool.result` rows, and 1 `agentic-governance` row containing all four governance outputs. No shipped
configuration enables `gated-dag`.

The 61-covered/zero-uncovered result is an invariant, not permission for the planner to predict future component
defaults. A future default-only JetStream output without explicit preconstruction coverage fails validation and the
structural census; the planner never guesses or constructs that default.

### 5.8 Factory identity is mandatory at admission

The exported identity-free `Registry.RegisterInstance(name, component)` path is removed or internalized in the clean
break. `CreateComponent` is the sole production admission path.

Any internal test or prepared-replacement helper must require a validated factory identity and perform the same
single-capture, validation, conflict, and atomic admission contract as `CreateComponent`. It must not infer identity
from a component, provide an alias, or retain a compatibility shim.

### 5.9 Admission is group-neutral

Registry admits and snapshots individual component generations only. It introduces no generic group primitive and
does not infer or promise atomic cohorts, group readiness, provider phase, all-or-nothing activation, or declaration
withholding until related components exist. Independently valid graph subsets remain valid and visible.

Capability-specific complete-set validators, such as `graphresearch`, stay at their composition boundary and decide
whether their own required members are present and ready. Rules and lifecycle mechanisms orchestrate component order
and progress; Registry declaration admission does not.

Any future generic grouping or cohort contract requires a separate measured design. It must not be inferred from
snapshot-set completeness, shared subjects, configuration adjacency, component type, or naming.

## 6. Adopter seam inventory for the target

- **External component author**
  - Must know: effective input/output ports are immutable within a generation; declaration changes require replacement.
  - If they do nothing: declaration-neutral updates continue; a port-changing live update fails typed before mutation
    unless the framework prepares a replacement.
  - Discovery: compile-time types and typed management/boot error.
  - Ideal bill: only the semantic interfaces/resources consumed and provided.
- **Registry/flowgraph consumer**
  - Must know: read Registry snapshots as admitted per-generation shape; do not call component port methods or infer
    activation/readiness.
  - If they do nothing: direct re-reads fail structural/contract checks.
  - Discovery: compile/test failure.
  - Ideal bill: only that Registry exposes accepted current declarations, not lifecycle or health.
- **Message-logger operator**
  - Must know: as an optional service, message-logger uses outer `types.ServiceConfig.Enabled` as its sole next-boot
    activation decision, and any desired edit requires restart. A started logger in `"*"` observes all effective
    declared subjects, retains raw message bodies, and incurs the measured expansion independently of global log level.
  - If they do nothing: message-logger is absent, with no instance, buffer, observer, subscription, runtime route,
    delivery work, capture, or backpressure.
  - Discovery: service configuration plus runtime inspection that exposes the resolved union and overlap handling.
  - Ideal bill: only whether to pay for this optional diagnostic and its raw-body capture.
- **Service-config operator**
  - Must know: the map key is the service identity; `services.*` writes are durable desired next-boot configuration;
    running composition is immutable; and pending means restart is needed to attempt consumption, not that boot will
    succeed. `Enabled` controls optional services only; disabling `component-manager` or `service-manager` is invalid.
  - If they do nothing: the current process continues unchanged, and `GET /services` reports any pending composition
    difference until it is reverted or valid desired state is successfully consumed. A mandatory disable cannot be
    consumed successfully.
  - Discovery: deterministic `restart_required` and `pending_service_changes` on the existing service list response.
  - Ideal bill: only that service edits require process restart; no subjects, generations, health keys, or diff rules.
- **File-config author**
  - Must know: current Config Manager arbitration requires the file version to exceed KV before a file edit wins;
    equal versions select KV.
  - If they do nothing: an unversioned file content edit is ignored on restart even though service construction now
    correctly consumes the selected effective state.
  - Discovery: only startup logging and existing version guidance; equal-version selection is not a typed authoring
    error. Documentation or diagnostics may make the intentional rule clearer later.
  - Ideal bill: understand directly that a file overwrite requires a newer version.
- **Service composition-root author**
  - Must know: create enabled configured services and register every fixed service their binary requires before
    `StartAll`; both APIs return errors and reject calls after the composition seal.
  - If they do nothing: the `RegisterInstance` signature break is a compile error. Manager validates configured and
    mandatory services but does not infer an omitted fixed service; the current production-root test proves
    `milestone` is registered.
  - Discovery: compile error, typed configured/mandatory pre-start error, and binary-root wiring test.
  - Ideal bill: name the intended pre-start services and let the manager seal a coherent runtime identity set.
- **Capability composer**
  - Must know: use the capability-specific complete-set validator and lifecycle/health facts, not a generic Registry
    group or snapshot-set completeness.
  - If they do nothing: independently valid graph subsets remain admitted; no inferred cohort withholds them.
  - Discovery: the capability's typed composition contract.
  - Ideal bill: only the members and readiness conditions required by that capability.
- **Flow-config author**
  - Must know: the canonical component port grammar and operator-owned stream policy.
  - If they do nothing: invalid config fails boot; current pre-construction planner behavior remains.
  - Discovery: typed boot error.
  - Ideal bill: semantic wiring and operator-chosen retention, not factory internals.
- **Stream-planning operator**
  - Must know: planning is desired provisioning intent, not proof of a running generation.
  - If they do nothing: a declaration may provision successfully while its component later fails construction/start.
  - Discovery: provisioning and component startup report separately.
  - Ideal bill: desired physical stream policy.

The shipped-flow seam is measured rather than deferred: 61 default-only JetStream output rows are covered by explicit
preconstruction declarations and zero are uncovered. That 61-covered/zero-uncovered invariant is enforced before
implementation and for future shipped-flow changes; the planner does not silently guess component defaults.

## 7. Collision-class disposition

- **Declaration:** changed by this design through one Registry generation snapshot and one shared classifier. The
  snapshot means admitted shape only; it carries no activation, readiness, group, or orchestration semantics.
- **Status:** no second status authority is added. `GET /services` observes only desired-versus-boot composition;
  no `GRAPH_STATUS` key, readiness envelope, component status bucket, or clustering status is added or inferred from
  declaration presence.
- **Lifecycle:** component lifecycle changes only through atomic local Registry record replacement. Service lifecycle
  is simplified to one pre-start composition phase, an immutable seal, and `StartAll`/`StopAll`; desired `services.*`
  changes cannot mutate a running service. Service health, provider phase, and domain lifecycle remain distinct from
  both the sealed identity set and desired-state comparison.
- **Orchestration:** unchanged. Rules/lifecycle coordinate order and progress; Registry does not infer cohorts or
  withhold independently valid declarations.
- **Indexes:** unchanged. Catalog acquisition stays shared; reconciliation, freshness, readiness, and degradation stay
  local to each projection owner.
- **Hierarchy:** unchanged. Graphable birth and request/reply mutation retain their current distinct semantics.
- **Research:** unchanged. Create-before-append and hierarchy-free RPC birth remain as inventoried.
- **Retention:** unchanged. No TTL, expiry, reclamation, archival, repair, or evidence-recovery policy is introduced.

No evidence supports extending either the declaration snapshot or service restart comparison into another class.

## 8. Framework identity preserved

The target preserves SemStreams as an offline-first, edge-capable, tiered semantic graph framework:

- component and flow configuration remains runtime-configurable;
- services remain explicit process-composition choices, with durable next-boot configuration and observable restart
  need rather than hidden hot mutation;
- NATS KV remains the fact/watch/history substrate where durable shared facts require it;
- queued work remains JetStream;
- graph mutations remain canonical request/reply against graph-ingest authority;
- derived state remains eventually consistent with owner-local failure policy;
- process-local declaration inspection stays process-local and admission-only;
- optional raw-body diagnostics remain explicit, default-off operator choices;
- capability composition remains capability-specific rather than an inferred generic cohort; and
- no ownership service, CQRS layer, recovery coordinator, migration reader, or compatibility shim is added.

## 9. Draft OpenSpec deltas

### component-discovery

Add requirements that:

- Registry captures each successful instance's effective resolved ports exactly once;
- component, declaration, and resource projections form one local instance-generation record;
- Registry is the sole declaration-derived resource admission owner and ComponentManager retains no parallel resource
  tracker or port re-read path;
- every admitted instance has validated factory identity and no identity-free production admission path exists;
- absent or disabled component configuration and failed admission create no generation;
- a retained generation describes admitted shape only, including after a later start failure, and exposes no enabled,
  started, healthy, ready, provider-phase, group, cohort, or orchestration field;
- a complete Registry snapshot set is an admission census, not an inferred or atomic cohort;
- independently valid graph subsets remain admitted, while capability-specific complete-set validation stays at the
  composition boundary;
- Registry readers receive defensive clones;
- capability, flowgraph, and management consumers use that record without re-reading the component;
- enabled message-logger auto-discovery reconciles complete current declaration sets; and
- when the outer service is enabled and started, the `"*"` migration expands from raw-config-explicit subjects to
  effective defaulted declarations under the bounded subject-set contract in §5.4.

### component-runtime-config

Add requirements that:

- declarations are immutable within a generation;
- declaration-neutral live updates prove normalized-fact equality before mutation;
- declaration-affecting live updates fail typed before mutation or use a prepared replacement generation;
- failed replacement exposes no partial generation;
- successful replacement assigns a new local generation and swaps the component plus declaration/resource projections
  together; and
- removal deletes the same record together.

### message-logger

Add requirements that:

- message-logger is a default-off optional diagnostic independent of global application log level;
- as an optional service, message-logger uses outer `types.ServiceConfig.Enabled` as its sole activation owner, and the
  loader injects no default logger entry;
- the inner `log_level` and `enabled` fields are removed from config, schema, update, and runtime-helper paths and are
  rejected strictly with no aliases or compatibility shims;
- omission or an outer disabled service creates no runtime instance, buffer, observer, subscription, route, delivery
  work, or backpressure before activation;
- actual `Start` attaches the observer and subscriptions, while Stop detaches them;
- only a started logger in `"*"` mode subscribes lazily to Registry snapshots;
- started logger observation continues to reconcile component declaration add, replace, and remove dynamically;
- runtime inspection exposes the resolved subject union and wildcard-overlap handling; and
- retained logger configuration is next-boot only, with no service runtime-config interface.

### service composition and configuration

Add requirements that:

- services are immutable process-composition units while components remain runtime-configurable flow units;
- `services.*` is durable desired next-boot configuration and never mutates running service composition;
- boot constructs services from the effective `SafeConfig` after `config.Manager.Start`, not from the stale original
  file-loaded object;
- that guarantee starts after existing version-based file/KV arbitration; equal versions continue selecting KV, and
  accepted arbitration remains unchanged;
- when arbitration selects KV, current `services.*` keys replace only the file-loaded Services map rather than
  overlaying it;
- one pure outer resolver removes `ServiceConfig.Name`, uses map keys as identity, materializes absent
  `component-manager` and required `service-manager` entries as enabled, preserves explicit false, applies existing
  optional outer defaults, omits message-logger, deep-clones/canonicalizes raw JSON, and performs no service-specific
  inner interpretation;
- constructors remain sole inner config validation/default owners and receive exact resolved raw config;
- the manager separately retains the exact resolved configurable-service map and sorted full sealed runtime identity
  set;
- `CreateService` and error-returning `RegisterInstance` are pre-seal composition-root APIs that fail typed after
  sealing;
- `StartAll` validates enabled configured optional services and mandatory `component-manager`, then seals the actual
  admitted set before any Start/routes/OpenAPI; composition roots own fixed-service requirements, with no Manager
  manifest or inference;
- outer `types.ServiceConfig.Enabled` is the sole next-boot activation input for optional services; configured
  `component-manager` or manager-infrastructure `service-manager` cannot be disabled, and every successful boot has
  both active;
- `service.RuntimeConfigurable`, its message-logger/metrics implementations, the service-only runtime schema marker,
  dynamic service manager mutation path, and exported per-service start/stop/remove methods do not exist;
- retained service schemas describe next-boot configuration only, and component hot updates remain unchanged;
- `GET /services` adds only deterministic `restart_required` and sorted `pending_service_changes` fields computed on
  demand from the boot desired map and current outer-resolved desired state using the §5.5 classifications;
- pending structural differences do not predict successful restart or trigger speculative inner validation; a
  mandatory disable is pending but boot-invalid until corrected;
- deleting or reverting an explicit mandatory false materializes enabled again and clears the pending comparison;
- reverted desired state clears the comparison, and only a successful boot consuming valid desired state starts clear;
- `/services` runtime rows match the full sealed set, routes match its `HTTPHandler` subset, and per-service OpenAPI
  contributors match its OpenAPI-capable subset; and
- health, readiness, and `GRAPH_STATUS` contracts remain unchanged.

### stream-provisioning

Clarify that:

- config-time port-derived stream declarations are provisioning intent;
- runtime Registry snapshots are accepted live declaration state;
- both consume the canonical resolver and normalized facts;
- neither imports the other owner's policy; and
- component declaration snapshots do not independently create, reconcile, or repair streams;
- all 61 shipped default-only JetStream output rows remain covered by explicit preconstruction stream declarations,
  with zero uncovered rows; and
- structural validation rejects any future uncovered default-only row rather than guessing its stream.

No ADR is proposed unless the owner decides the Registry snapshot observer is a supported cross-repo contract rather
than an internal framework API.

## 10. Verification obligations

A later implementation proposal must prove:

- Registry calls each component port method once per successful generation;
- every admitted record carries validated factory identity, and negative production searches find no identity-free
  direct-admission path, alias, or compatibility shim;
- invalid declarations and conflicts publish no record;
- absent/disabled component configuration and admission failure publish no generation;
- every Registry read is defensively cloned;
- a successfully admitted component whose start fails remains honestly inspectable as admitted but is not reported
  ready by lifecycle/health consumers;
- capability publication cannot re-read a superseded component;
- flowgraph and management views match the Registry snapshot for every port kind;
- complete Registry snapshot sets remain admission censuses: independent graph subsets are not withheld, and
  capability-specific validators decide their own completeness;
- negative searches find no generic group/cohort field, type, readiness state, provider phase, or orchestration state
  in Registry generation records or observer payloads;
- declaration-neutral live updates prove exact normalized-fact equality before component/config mutation;
- port-affecting live updates either return typed `declaration_change_requires_replacement` before mutation or prepare
  a complete replacement off-Registry;
- no path mutates a live component and then recaptures its declaration;
- failed replacement leaves the old component, retained configuration, and record untouched;
- successful replacement and removal update component, factory identity, declaration, and resource views together;
- negative production searches find no ComponentManager resource tracker, conflict/registration bookkeeping, or
  component port re-read;
- characterization proves the accepted version-arbitration rule remains unchanged: newer file version pushes file state,
  while older or equal file version selects KV; an equal-version file content edit alone does not apply on restart;
- when arbitration selects KV, service synchronization starts from an empty Services map and loads only current
  `services.*` keys, so a file-backed service deleted from KV remains absent; other top-level config synchronization
  retains its current behavior;
- after `config.Manager.Start` completes that arbitration and synchronization, service construction reads the
  resulting `SafeConfig` and never the stale original file-loaded object;
- negative searches find no `ServiceConfig.Name` field or dependent config, schema, test, documentation, log, or
  comparison path; map key is the sole identity;
- one pure outer `ServiceConfigs` resolver is deterministic and non-mutating: absent `component-manager` and required
  `service-manager` entries materialize with `Enabled: true`, explicit false is preserved, existing optional outer
  defaults remain optional, message-logger is never injected, and raw JSON is structurally canonicalized without
  per-service decoding, semantic validation, defaults, schema interpretation, or codec callbacks;
- constructors receive exact resolved raw config and remain the only inner validation/default owners; disabled-only
  resolution and comparison invoke no constructor or inner validator;
- the retained boot desired map is an immutable deep clone of the exact resolved configurable-service map used by the
  composition root, including disabled entries, and remains distinct from the sealed full runtime identity set;
- `CreateService` and error-returning `RegisterInstance` reject duplicates and return a typed sealed-composition error
  after `StartAll` seals; all composition-root callers handle their errors and no void/overwrite shim remains;
- `StartAll` proves every enabled configured optional service and mandatory `component-manager` is present, creates
  `component-manager` before sealing if needed, then seals and retains a sorted full identity set before any service
  Start, route binding, or OpenAPI exposure;
- current production-root tests prove `milestone` registration occurs before the seal; Manager does not infer its
  omission, and negative searches find no fixed-service manifest, group, or generic completeness primitive;
- configured/mandatory composition failure leaves zero services started and no routes/OpenAPI exposed, while
  post-seal Start failure does not mutate the sealed identity set;
- desired `services.*` add, enable, disable, remove, and reconfigure writes do not create, start, stop, remove, or
  apply configuration to a running service;
- negative production searches find no service-manager `services.*` subscription/config-update channel, service
  watcher goroutine, old/new diff or apply helper, `RuntimeConfigurable`, service-only runtime schema marker, or
  exported `StartService`, `StopService`, and `RemoveService`;
- component hot-update tests remain green through the separate anonymous validation/application method pair;
- the loader injects no default message-logger entry, and omission or an outer disabled service creates no logger
  instance, buffer, Registry observer, NATS/message subscription, runtime HTTP/KV/SSE route, delivery work, or
  backpressure before activation;
- outer `types.ServiceConfig.Enabled` remains the sole next-boot activation input for optional services, and actual
  Start attaches optional message-logger at every global application log level;
- configured `component-manager` and manager-infrastructure `service-manager` reject `Enabled: false` at boot; every
  successful boot has both active;
- message-logger inner `log_level`/`enabled` and metrics inner `enabled` plus their schema/update/runtime-helper paths
  are absent, and strict config rejects them without aliases, deprecated fields, or compatibility shims;
- retained message-logger and metrics inner knobs are exposed only as next-boot schema with no runtime marker or hot
  apply path;
- only a started logger in `"*"` mode subscribes lazily; it observes later declaration add, replace, and remove, and
  Stop cancels the observer and every logger-owned subscription;
- `GET /services` sorts its existing service rows and emits sorted pending rows with exact add/enable/disable/remove/
  reconfigure classification, at most one row per service map key;
- absent-to-disabled, disabled-to-absent, and disabled-to-disabled churn emits no pending row; activation transitions
  take precedence over simultaneous inner-config changes, and disabled-only churn performs no inner validation;
- enabled invalid/unknown desired config is still classified structurally and may fail on the next boot through its
  constructor/registry path; `restart_required` promises only that restart is needed to attempt consumption;
- negative searches find no `restart_blocked`, config-error outcome, speculative inner validation, or restart-success
  claim on the comparison surface;
- desired mandatory disable emits the structural pending `disable`, a restart attempt fails until corrected, and no
  test promises successful consumption or clearing by restart for that invalid state;
- boot omission of a mandatory outer entry resolves to enabled in the retained boot desired map; later explicit false
  emits `disable` with `restart_required: true`; reverting or deleting it resolves to enabled and clears immediately;
- direct external `services.*` KV writes become observable after Config Manager applies them, reversion to the boot
  desired map clears immediately, and only a successful boot consuming valid effective desired state starts clear;
- `/services` runtime rows equal the sorted sealed full set; bound routes equal its `HTTPHandler` subset; generated
  per-service OpenAPI contributors equal its OpenAPI-capable subset; and none drift during desired-state edits;
- service health, `/services/health`, readiness, and `GRAPH_STATUS` behavior remain unchanged;
- negative searches find no dynamic mux lifecycle, generic service group, scheduler, new status key, readiness gate,
  service-state bucket, or service-state stream;
- message-logger covers empty initial state, add, replacement, removal, coalescing, explicit-subject union, and
  cancellation;
- message-logger performs no raw component-config port parsing;
- for an outer-enabled, started logger in `"*"` mode, an exact migration test constructs effective generations through
  registered factories for every enabled component in all 21 shipped configurations and proves 385 raw rows/243
  per-config keys/51 global strings become 561 effective rows/378 keys/66 strings; the 176-row/135-key/15-string delta
  has zero removals or forbidden broadening; exact-key deduplication accounts for the 40 loop/dispatch and one
  governance collapses; and the three wildcard-containment overlaps are detected, resolved without duplicate capture,
  and exposed through runtime inspection;
- the structural shipped-flow census proves exactly 61 default-only JetStream output rows, 61 explicitly covered, and
  zero uncovered;
- stream planning continues using only canonical resolution/facts and does not inspect concrete port configurations;
- a future uncovered default-only JetStream output fails validation instead of being guessed;
- no NATS declaration-snapshot bucket or stream appears;
- no new readiness authority, lifecycle scheduler, index, hierarchy, research, or retention primitive appears; and
- relevant race, integration, contract, schema, and E2E gates are selected before any breaking implementation lands.

## 11. Non-goals

- No `Ports() PortConfig` migration.
- No `Registration.DefaultPorts`, static declaration factory, or second component-config normalization hook.
- No durable declaration log, KV bucket, JetStream stream, ownership claim, repair worker, or recovery protocol.
- No global component lifecycle scheduler.
- No generic group/cohort primitive, readiness scheduler, provider-phase field, or atomic-completeness inference.
- No fixed-service manifest or Manager inference that a composition root omitted a binary-specific fixed service.
- No snapshot activation/readiness coupling or declaration withholding for inferred cohorts.
- No log-level-inferred message-logger activation or default production logger.
- No service hot configuration, dynamic routing, route removal, or mux lifecycle machinery.
- No per-service normalizer registry, codec callback, schema-default engine, duplicate inner validator, or prediction of
  constructor success in the outer service resolver.
- No `restart_blocked`, config-error status, or promise that a pending desired difference can boot successfully.
- No durable restart-required key, service-status bucket/stream, health coupling, readiness gate, or `GRAPH_STATUS`
  change; the existing service-list response performs only an on-demand boot-versus-desired comparison.
- No change to Config Manager's version-based file/KV authority arbitration. Manual file-version advancement is a
  required part of intentionally authorizing file content to overwrite KV. Documentation or diagnostic clarity may be
  improved later, but no authority redesign is promised.
- No claim that KV selection replaces the whole file config. This increment replaces only the Services map from
  current `services.*` keys when existing arbitration selects KV.
- No readiness-key consolidation or owner-policy centralization.
- No index reconciliation framework.
- No hierarchy or research mutation redesign.
- No retention decision.
- No downstream implementation while the ten holdouts are paused.
- No activation, archival, or implementation of `semantic-tier-split`.
- No issue mutation.

## 12. Owner ruling state

The owner accepts the complete design and every ruling below.

1. Option 1 replaces the stale old Foundation C shape.
2. Declarations are immutable within a generation: port-affecting live updates fail before mutation or use a
   prepared replacement generation.
3. Registry is the sole declaration-derived resource authority; ComponentManager's parallel tracker is retired.
4. The measured shipped-flow invariant is accepted: 61 default-only JetStream output rows, all explicitly covered, zero
   uncovered, with future uncovered rows rejected structurally.
5. Conditional on the outer service being enabled and started, message-logger `"*"` expands by 176
   default-only rows and 135 net-new per-configuration exact keys, with zero removals; bounded exclusion of bare
   wildcards, inboxes, replies, and undeclared subjects; continued raw-body retention; and deliberate, inspectable
   handling of the three `configs/agentic.json` wildcard overlaps.
6. Identity-free `Registry.RegisterInstance(name, component)` is removed or internalized with no alias or compatibility
   shim.
7. Pre-construction provisioning intent and accepted runtime declaration are distinct facts sharing one
   classifier, rather than requiring one snapshot to serve both.
8. The Registry snapshot/observer is an internal framework API, not a cross-repo contract requiring an ADR.
9. Generation semantics are process-local and coalescing, with no durable replay or recovery claim.
10. Owner policy responses remain local even though declaration classification is shared.
11. Services are immutable process-composition units; `services.*` is durable desired next-boot
    configuration only; components remain runtime-configurable. Construct from the post-`config.Manager.Start`
    effective desired state through one pure outer-map resolver: remove `ServiceConfig.Name`, use map keys as identity,
    materialize absent `component-manager` and required `service-manager` outer entries as enabled, preserve explicit
    false for pending comparison and boot rejection, retain existing optional outer defaults as optional, canonicalize
    raw JSON structurally, and leave all inner validation/defaults to constructors. When existing arbitration selects
    KV, replace only the Services map from current `services.*` keys.
    Compose configured, fixed, and mandatory services in one pre-start phase; optional services alone use `Enabled`
    as activation, configured `component-manager` or manager-infrastructure `service-manager` cannot be disabled, and
    every successful boot has both active. Composition roots own their binary-specific fixed services; Manager adds no
    manifest or omission inference. Make `CreateService` and error-returning `RegisterInstance` fail typed after
    `StartAll` seals and retain the exact boot desired map separately from the sorted full sealed identity set.
    `/services` rows match the full set, routes its `HTTPHandler` subset, and per-service OpenAPI contributors its
    OpenAPI-capable subset. Report only deterministic `restart_required` plus sorted pending changes through existing
    `GET /services`, where pending means restart is required to attempt consumption, not that restart will succeed. A
    mandatory disable is pending but boot-invalid until corrected, with no successful-clear promise; deletion or
    reversion materializes enabled and clears the comparison. Remove the service mutation watcher/apply path,
    `RuntimeConfigurable`, service runtime schema marker, dynamic per-service lifecycle
    methods, message-logger loader default, and dead inner message-logger `enabled`/`log_level` and metrics `enabled`
    fields with no shims. Keep retained service knobs next-boot only; preserve component hot updates and existing
    health/readiness/`GRAPH_STATUS`. Existing version arbitration is accepted intentional safety and remains unchanged;
    later non-foundational work may improve its documentation or diagnostics.
12. Registry snapshots are admission-only, group-neutral per-generation shape: no readiness inference, generic
    cohort, declaration withholding, or orchestration semantics.

The wording-only revision requires refreshed independent identity confirmation before an OpenSpec change or
implementation handoff is created.
