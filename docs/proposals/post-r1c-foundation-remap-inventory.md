# Post-R1c foundation remap: surface inventory

**Artifact state:** INVENTORY DRAFT — pending independent inventory review.
**Repository baseline:** `c38e3e82d5a0b1deec598ad1bf8bb21a6bf0b3fa`.
**Contains:** current-state evidence, collision inventories, adopter seams, issue dispositions, and open evidence
questions.
**Does not contain:** options, recommendations, target state, implementation tasks, approval, or binding rulings.

## 1. Purpose and control boundary

This inventory re-derives the SemStreams foundation after GS-01, R1a, R1b, and R1c. It does not inherit the former
mechanical `R1c -> R1d -> R1e -> R2` sequence. It asks which remaining declarations, reads, status surfaces, and
diagnostic paths exist now that R1c is merged.

The product boundary remains the one in `openspec/project.md`: SemStreams owns the substrate and framework primitives,
not product-domain semantics. The framework remains offline-first, edge-capable, tiered, and eventually consistent.
This inventory introduces no stronger consistency, recovery, ownership, or CQRS premise.

The pre-v1 program permits clean breaks. That policy is recorded here only as a constraint on any later design: no
compatibility shims, deprecated alternatives, aliases, or dual paths.

## 2. Evidence method

The inventory was enumerated from production types, constructors, configuration, interpreters, graph builders,
services, tests, specifications, program records, and the live issue queue. Issue text and earlier roadmaps were treated
as hypotheses and compared with the merged tree.

The `kv-or-stream` decision skill was applied to each communication class in section 6. No orchestration, payload, or
query-access decision skill is triggered by this inventory because it creates no new behavior, payload, or gateway.

## 3. Claimed gaps checked against the tree

### 3.1 `kv_read` exists in configuration but not in the Go model

`component.PortConfig` at `component/ports.go:155-160` contains `Inputs`, `Outputs`, and `KVWrite`; it has no KV-read
field. Seven checked-in configurations nevertheless contain `kv_read` arrays with entries whose type is `kv-read`:

| Configuration | Declaring components |
|---|---|
| `configs/agentic.json` | `agentic-tools`, `graph-query` |
| `configs/research-graph-e2e.json` | `graph-query` |
| `configs/flows/deep-research.json` | `agentic-tools` |
| `configs/flows/lesson-example.json` | `agentic-tools` |
| `configs/flows/deep-research-test.json` | `agentic-tools` |
| `configs/flows/ops-agent.json` | `agentic-tools` |
| `configs/examples/research-graph-pipeline.json` | `graph-query` |

There are nine rows in total. Normal `encoding/json` unmarshalling ignores these unknown object fields. The checked-in
configuration therefore declares a fact that the current Go configuration model does not retain or validate.

### 3.2 `KVWrite` exists in the Go model but is not a shared runtime lane

Nine checked-in configurations use `kv_write`. The only production construction found for `PortConfig.KVWrite` is in
`processor/agentic-loop/config.go:515`. Agentic-loop port construction renders `Ports.Inputs` and `Ports.Outputs`, while
`initializeKVBuckets` independently opens or creates `AGENT_LOOPS`. No production read of an already populated
`.KVWrite` declaration was found.

Exact closing search:

```text
rg -n '\.KVWrite|KVWrite:' --glob '*.go' --glob '!**/*_test.go'
=> processor/agentic-loop/config.go:515
```

### 3.3 `Registration.DefaultPorts` is absent

Issue #862 describes `component.Registration.DefaultPorts`; the current `component.Registration` definition has no
such field. No current `DefaultPorts` symbol was found.

```text
rg -n 'DefaultPorts' --glob '*.go'
=> no output
```

### 3.4 No central port-kind/declaration vocabulary exists

No current `PortKind`, `DeclaredPorts`, `InputPortsOf`, or `OutputPortsOf` symbol was found.

```text
rg -n 'type PortKind|DeclaredPorts|InputPortsOf|OutputPortsOf' --glob '*.go'
=> no output
```

The absence of those proposed spellings does not mean the underlying facts are absent. Their current spellings are
enumerated next.

## 4. Every current spelling of the modeled facts

### 4.1 Public port and discovery types

| Surface | Current job |
|---|---|
| `component.Port` | Runtime port interface and JSON envelope |
| `component.PortDefinition` | Generic configured declaration and typed/untyped JSON decoding |
| `component.PortConfig` | Component config groups for inputs, outputs, and separate KV writes |
| `component.Discoverable` | Component-authored `InputPorts`, `OutputPorts`, and `Health` |
| `component.NATSPort` | Plain NATS/JetStream subject port |
| `component.NATSRequestPort` | Request/reply subject port |
| `component.KVWatchPort` | KV watch dependency |
| `component.KVWritePort` | KV write dependency in runtime port form |
| `component.StoreReadPort` | Referenced large-content federation consumer |
| `component.StoreProvidePort` | Referenced large-content federation provider |

`Discoverable` is exported. A component author must implement `InputPorts()` and `OutputPorts()` and construct runtime
`Port` values rather than declaring only semantic dependencies.

### 4.2 Independent interpreters of the port fact

The same port declaration is parsed, rendered, classified, or reported in these production locations:

| Interpreter | Evidence and interpretation |
|---|---|
| `PortDefinition.UnmarshalJSON` | `component/ports.go`; recognizes a subset of typed config forms and otherwise retains a generic map |
| `Port.UnmarshalJSON` | `component/ports.go`; switches on runtime port type but has no `store-read` or `store-provide` case |
| `BuildPortFromDefinition` | `component/ports.go`; constructs runtime values; an unknown type falls through to `NATSPort` |
| Component `InputPorts`/`OutputPorts` methods | Per-component rendering and defaults |
| Flow graph builder | Classifies communication edges, including store federation |
| Component manager | Reports a subset of runtime ports |
| Registry capability announcements | Reclassifies a subset of configured types |
| Message logger | Reparses raw input/output configuration to derive subjects |

The issue #859 inventory measured approximately 55 interpretation sites using two broad idioms. This inventory confirms
the class remains plural after R1c; the table above identifies the load-bearing production owners rather than adopting
the issue's desired solution.

Specific divergences:

- `BuildPortFromDefinition` maps an unknown type into the NATS-port semantic class.
- Store ports cannot round-trip through `Port.UnmarshalJSON`.
- `PortDefinition.UnmarshalJSON` has no typed store branches.
- `component/registry.go:1008-1033` recognizes only pointer `*NATSStreamPortConfig` and
  `*NATSRequestPortConfig` for capability announcements. No production constructor of those pointer forms was found;
  value-form declarations can therefore announce an empty subject and `unknown` type.
- `service/component_manager.go:2291-2343` reports only `NATSPort` and `NATSRequestPort`; other runtime port kinds are
  omitted.
- `service/message_logger.go:289-340` reparses raw `Inputs` and `Outputs`, so it cannot see ignored `kv_read`, separate
  `KVWrite`, synthesized defaults, or runtime-only declarations.

### 4.3 Graph-clustering declarations and runtime acquisition

Graph-clustering declares one `kv-watch` input for `ENTITY_STATES`. At runtime it opens must-exist catalog readers for
three buckets: `ENTITY_STATES`, `OUTGOING_INDEX`, and `INCOMING_INDEX`. Its watchers are for `GRAPH_STATUS` readiness;
it does not watch entity authority data for its clustering work.

The declared method therefore differs from runtime use for `ENTITY_STATES`, and two runtime dependencies have no
corresponding declaration.

### 4.4 `StoreReadPort` is a distinct semantic class

`StoreReadPort` represents backend-neutral federation of referenced large content. The flow graph connects all store
providers to all store readers; its bucket value is advisory rather than the exact identity contract used by KV
catalog access. Graph embedding resolves content through `StoreRegistry` at runtime. Store-read is therefore adjacent
to but not another spelling of exact KV read.

### 4.5 Status, readiness, health, and lifecycle

Four current classes share operator-facing vocabulary but have different storage and consumers:

| Class | Current fact | Writers | Readers |
|---|---|---|---|
| `GRAPH_STATUS` | Current readiness of graph-derived services | graph-index, graph-embedding, graph-ingest, rule | Components that explicitly select relevant producer keys |
| `COMPONENT_STATUS` | Component stage/cycle diagnostic reports | Lifecycle reporters constructed across components | Generic message-logger query/watch; no dedicated reader found |
| `Discoverable.Health()` | Current in-process component health | Each component | ComponentManager and its HTTP health surface |
| `pkg/lifecycle` | Domain/workflow phase convention over entity state | Lifecycle-aware domain flows | Domain lifecycle consumers over `ENTITY_STATES` |

`GRAPH_STATUS` is a KV current fact with history 3. The graph catalog owner prose names graph-index and graph-embedding,
but the merged tree also writes graph-ingest and rule producer keys. Consumers select keys explicitly; there is no
global mandatory-producer list.

`COMPONENT_STATUS` has 25 constructor call sites across 26 files with lifecycle-reporter fields, and approximately 93
`ReportStage`/`ReportCycle` call sites. The following search shows that there is no statically named, dedicated
production reader while excluding tests and the E2E client:

```text
rg -n -S \
  'OpenCatalogReader\([^\n]*BucketComponentStatus|GetKeyValueBucket\([^\n]*BucketComponentStatus|OpenFrameworkBucket\([^\n]*BucketComponentStatus|BucketComponentStatus[^\n]*\.(Get|Keys|Watch|WatchAll)|COMPONENT_STATUS[^\n]*(Get|Keys|Watch|WatchAll)' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/e2e/**'
=> no output
```

A broader production occurrence search returns only the constant/catalog, writer acquisition, writer implementation,
and comments:

```text
rg -n -S 'BucketComponentStatus|COMPONENT_STATUS' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/e2e/**'
=> graph/constants.go
   graph/kvcatalog.go
   natsclient/kvspec.go comments
   component/lifecycle_reporter.go
   component/lifecycle_reporter_catalog.go
   research-graph component comments
```

That literal-name search does not close the semantic reader class. The default-enabled message-logger accepts a bucket
selected from the request route at `service/message_logger_http.go:390-440`, opens it and reads keys/values at
`:459-556`, and opens/watches it at `service/message_logger_kv_watch.go:195-232`. It is therefore a production
diagnostic reader of `COMPONENT_STATUS` whenever a caller selects that bucket. The E2E client also contains a helper
cluster at `test/e2e/client/nats.go:1463-1605`; no calls to that cluster were found outside that file. Actual E2E health
checks use ComponentManager HTTP `GetComponents`, not `COMPONENT_STATUS`.

### 4.6 Message-logger acquisition and interpretation

Message-logger is a service, not a component. Its KV query/watch routes accept a caller-selected existing bucket and
call lookup-only `GetKeyValueBucket`; they do not create or reconcile a bucket. This preserves reads of both framework
catalog buckets and product/application buckets such as `AGENT_LOOPS`. The graph KV catalog intentionally lists only
framework-guaranteed buckets.

Message-logger uses a separate configuration parser at `service/message_logger.go:289-340` to infer monitored NATS
subjects from configured inputs and outputs. That is a port-interpretation spelling; the generic must-exist KV read
path is a separate acquisition concern.

#### Default and checked-tier enablement

Message-logger is enabled by framework configuration default, not merely by documentation. `Loader.getDefaults`
installs the `message-logger` service with `Enabled: true` and wildcard subject monitoring at
`config/config.go:485-504`.

Every tier/E2E configuration checked for this inventory also explicitly enables it:

| Configuration | Evidence |
|---|---|
| `configs/hello-world.json` | `43-48`, enabled at `:45` |
| `configs/e2e-structural.json` | `43-48`, enabled at `:45` |
| `configs/statistical.json` | `43-48`, enabled at `:45` |
| `configs/semantic.json` | `80-85`, enabled at `:82` |
| `configs/agentic.json` | `93-98`, enabled at `:95` |
| `configs/lifecycle-flow.json` | `43-48`, enabled at `:45` |
| `configs/research-graph-e2e.json` | `71-76`, enabled at `:73` |

The handler comment describing message-logger as off by default at `service/message_logger_http.go:431-436` is stale
relative to the loader default and checked configurations. Enablement does not establish authorization; the framework
ships no authorization middleware, and product/deployment middleware owns access control.

Current message-logger issues remain distinct observations:

- #472: entry limit is applied before filtering.
- #587: per-client watchers and the absence of a shared view create scaling pressure.
- Authorization belongs to product/deployment middleware.
- Documentation and comments disagree with default enablement.

Closed #611 fixed reader-created buckets; the current lookup-only behavior contains that correction.

## 5. Same-class collision inventories

### 5.1 Durable/declaration class

| Dimension | Current evidence |
|---|---|
| Semantic class | Declare durable state dependencies and provisions for component/flow inspection |
| Owners | `PortConfig`, per-component `Discoverable` methods, runtime bucket-open code, `StoreRegistry` |
| Catalogs | Graph KV catalog, raw component configuration, registry capability announcements |
| Status | `GRAPH_STATUS` gates some graph bucket use; most declarations carry no readiness state |
| Lifecycle | Component start/stop owns handles; KV watch replay restores current facts; store federation is flow-scoped |
| Ownership | Catalog owns framework bucket specifications; components own runtime acquisition; no declaration injects a handle |
| Readers | Flow graph, registry, ComponentManager, message-logger, components, tests |
| Writers | Config authors, component defaults/methods, KV provisioners, component runtime code |
| Recovery | KV current-state replay/reopen for facts; store content via `StoreRegistry`; no declaration-layer recovery owner |

### 5.2 Communication/declaration class

| Dimension | Current evidence |
|---|---|
| Semantic class | Describe component message/request reachability and connect flow edges |
| Owners | NATS ports, NATS request ports, per-component renderers, flow graph, registry capability code |
| Catalogs | Raw component configs and component registrations |
| Status | Component `Health`; no communication-wide readiness registry |
| Lifecycle | Components subscribe during start and drain/close during stop |
| Ownership | Components own handlers; stream coverage and request/reply subjects can overlap independently |
| Readers | Flow builder, discovery/tool listing, ComponentManager, message-logger, E2E |
| Writers | Config authors and component default/rendering methods |
| Recovery | KV facts replay through watches; requests use request/reply or JetStream according to their semantic class |

### 5.3 Coordination/status class

| Dimension | `GRAPH_STATUS` | `COMPONENT_STATUS` | `Health()` | `pkg/lifecycle` |
|---|---|---|---|---|
| Semantic class | Graph producer readiness | Diagnostic stage/cycle telemetry | In-process health | Domain phase convention |
| Owners | Four producer components | Lifecycle reporters in many components | Each component/ComponentManager | Domain workflows |
| Catalogs | Graph KV catalog | Graph KV catalog | Component registry/manager | Entity vocabulary/state |
| Status | The stored fact itself | The stored report itself | Method result | Phase triples/state |
| Lifecycle | Rebuilt/updated by producers | Written across stage/cycle transitions | Observed while process runs | Persisted in entity state |
| Ownership | Producer-key scoped | Component/report key scoped | Component scoped | Domain entity scoped |
| Readers | Explicit graph dependants | Generic message-logger query/watch; unused E2E helper cluster; no dedicated production reader | ComponentManager HTTP/E2E | Domain lifecycle consumers |
| Writers | index, embedding, ingest, rule | About 25 constructors/~93 report calls | Components | Lifecycle-aware flows |
| Recovery | KV current value/history | Generic message-logger replays current values and signals initial sync; no dedicated framework state-recovery consumer found | Recomputed in process | ENTITY_STATES replay |

These tables identify overlaps; they do not assert that any class should be merged, retained, or removed.

## 6. `kv-or-stream` classification

Applying the four-test heuristic to the existing paths yields:

| Path | Semantic answer | Existing primitive |
|---|---|---|
| Entity state, graph indexes, current readiness | Fact; latest state matters; watchers need current values on restart | KV and KV watch |
| Message-logger KV query/watch | Diagnostic observation of an existing current-fact store | Lookup-only KV read/watch |
| Mutation and tool calls | Request to perform work; every request may matter | NATS request/reply or JetStream where durable resume is required |
| Store federation | Referenced content acquisition, not a state-vs-request choice | Store ports and `StoreRegistry` |
| `COMPONENT_STATUS` | Current diagnostic fact in shape | KV writers plus generic message-logger query/watch; no dedicated production consumer found |

No evidence in this inventory creates a need for a new JetStream stream or a parallel event channel. This is a
classification of current semantics, not a future design ruling.

## 7. Adopter seam inventory

### 7.1 External component author

- **Must know now:** which config type strings each interpreter recognizes; whether to construct value or pointer config
  forms; how to hand-render runtime input/output ports; which durable dependencies are declared versus opened only at
  runtime.
- **If they do nothing:** a dependency may remain invisible, be reported as `unknown`, be omitted, or an unknown type
  may become a NATS port.
- **Where they find out:** repository examples and implementation details; some failures are silent rather than boot
  errors.
- **Should have to know:** the semantic dependency and its application-level values, not each framework interpreter.

### 7.2 Configuration author

- **Must know now:** `inputs`, `outputs`, and `kv_write` are modeled differently; checked-in `kv_read` is ignored;
  unknown port types can fall through during runtime construction.
- **If they do nothing:** valid JSON can boot without preserving the intended declaration.
- **Where they find out:** source inspection or downstream behavioral absence.
- **Should have to know:** one canonical declaration grammar with boot-time rejection of invalid values.

### 7.3 Store-content consumer

- **Must know now:** store-read is federation through `StoreRegistry`, while exact KV read is a bucket identity and
  current-state operation.
- **If they do nothing:** treating the two as equivalent can create incorrect flow edges or acquisition assumptions.
- **Where they find out:** port implementation and flow-graph code.
- **Should have to know:** only that the payload references external content; the framework should own backend lookup.

### 7.4 `GRAPH_STATUS` consumer

- **Must know now:** which producer keys are relevant and what ready/degraded states mean for its own blast radius.
- **If they do nothing:** eventual graph data may be absent or stale while the component continues.
- **Where they find out:** component code and graph status vocabulary; catalog owner prose is incomplete.
- **Should have to know:** its actual prerequisites and local response policy, while shared acquisition and outcome
  classification remain canonical.

### 7.5 Lifecycle reporter adopter

- **Must know now:** where to instantiate a reporter and which stage/cycle transitions to emit.
- **If they do nothing:** production behavior continues; the generic message-logger can expose fewer stage/cycle
  observations if a caller explicitly queries this bucket.
- **Where they find out:** copied component patterns and reporter APIs.
- **Should have to know:** nothing unless a named production consumer requires the diagnostic fact.

### 7.6 Message-logger integrator

- **Must know now:** the service is enabled by default; KV reads require an existing caller-selected bucket; monitoring
  subjects are inferred from only part of the port model; authorization is deployment middleware's job.
- **If they do nothing:** the diagnostic service is exposed according to surrounding deployment routing/middleware and
  may omit traffic described through unrecognized declaration paths.
- **Where they find out:** loader defaults, service implementation, and stale/conflicting comments.
- **Should have to know:** only deployment authorization policy and the diagnostic resource requested; the framework
  should report its effective monitored surface truthfully.

## 8. Adjacent issue and program territory

### 8.1 Directly adjacent live issues

- **#620 OPEN** — phantom signals and inert configuration.
- **#795 OPEN** — readiness front door.
- **#820 OPEN** — graph-clustering lacks a `GRAPH_STATUS` envelope.
- **#859 OPEN** — port interpretation drift.
- **#862 OPEN** — seal `Discoverable`; its current issue-body `Registration.DefaultPorts` premise is absent from the
  merged tree.
- **#868 OPEN** — generalize readiness vocabulary.
- **#753 OPEN** — sister-project breaking-cutover tracking.
- **#810 OPEN** — `agentic-tools: tool.list discovery is silently swallowed when a JetStream stream covers tool.>`.
  This is a port-model-dependent communication collision: request/reply reachability is interpreted separately from
  stream coverage. Current program control holds it behind #859/#862; this inventory does not infer its disposition.
- **#842 OPEN** — move the default discovery subject off `tool.list`, deferred from #810 to a breaking wave. It remains
  dependent on #810 and, under current control, behind #859/#862. Its proposed subject move is issue evidence, not an
  adopted target.

### 8.2 Live but separate issue classes

- #422: unused query API.
- #472, #571, #579, #587: message-logger behavior, security, and scaling.
- #688, #689, #690: graph/index concerns requiring a later post-foundation inventory.
- #725, #736, #765: other bounded graph/query concerns.
- #882 through #886: issue cluster recorded by the earlier graph-state investigation.

### 8.3 Closed territory not reopened by this inventory

- #611: reader-created message-logger bucket; current code is lookup-only.
- #717: `COMPONENT_STATUS` disposition record.
- #861, #869, #870, #871, #874: closed program increments from the superseded sequence.

### 8.4 Suspended OpenSpec work

The `discovery-under-stream-shapes` change for #810 is suspended/frozen. Its task record says port handling must be fixed
first. Its absent `Registration.DefaultPorts` premise cannot be treated as merged framework truth.

### 8.5 Deliberate downstream holdout set

The owner-approved downstream set is fixed at ten repositories:

- `semdev`
- `semmachina`
- `semsource`
- `semboids`
- `semdragon`
- `semstreams-ui`
- `semteams`
- `semconnect`
- `semlink`
- `semops`

The boundary at `docs/operations/36-graph-foundation-breaking-cutover.md:8-10,62-87` treats these repositories as
future feature/API-parity holdouts. Their current implementation choices do not constrain the SemStreams foundation,
block this inventory, or supply design input. Findings there belong to the later approved migration stocktake after a
stable framework target exists. No further downstream census is an open evidence requirement here.

## 9. Consumer-at-birth checks

This inventory introduces no exported symbol, port, subject, bucket, or config field. Current surfaces with no present
dedicated production consumer or constructor were measured as follows. The literal `COMPONENT_STATUS` search does not
exclude the generic message-logger reader documented in section 4.6:

```text
rg -n 'DefaultPorts|type PortKind|DeclaredPorts|InputPortsOf|OutputPortsOf' --glob '*.go'
=> no output

rg -n 'NATSStreamPortConfig|NATSRequestPortConfig' --glob '*.go' --glob '!**/*_test.go'
=> definitions and registry type checks; no production constructors found

rg -n 'GetComponentStatus|WatchComponentStatus' --glob '*.go' --glob '!**/*_test.go'
=> ComponentManager HTTP status method plus the E2E client helper cluster; no E2E-cluster call sites outside its file
```

## 10. Open evidence questions for a later design phase

1. Which existing declaration is authoritative when raw config, component defaults, and runtime acquisition disagree?
2. Which current component methods synthesize ports from facts unavailable in static configuration?
3. Which exported port/config types have external consumers not represented by the deliberate downstream holdout set?
4. Which exact E2E tier observes each port semantic class, especially store federation and request/reply discovery?
5. Does any deployment have a dedicated `COMPONENT_STATUS` consumer outside this repository, beyond callers using the
   generic message-logger? That would be downstream migration evidence, not a dedicated reader in this baseline.
6. Which message-logger defaults and comments are contractual versus accidental? This does not alter its current
   lookup-only generic KV acquisition class.

Those questions bound evidence still needed for design. They do not authorize implementation or revive the superseded
post-R1c sequence.
