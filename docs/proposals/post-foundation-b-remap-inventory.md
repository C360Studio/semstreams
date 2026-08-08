# Post-Foundation-B remap: surface inventory

**Artifact state:** INVENTORY DRAFT — pending independent inventory review.
**Repository:** `C360Studio/semstreams`.
**Repository baseline:** `9d530bf23c97054f38be5a6caf7c25ac20a07e1c` (merge commit for PR #913).
**Scope:** merged SemStreams tree, current specs/ADRs, active changes, and live issue state.
**Contains:** current-state evidence, collision inventories, adopter seams, issue dispositions, exact searches, and open
evidence questions.
**Does not contain:** options, recommendations, target state, roadmap, implementation tasks, spec deltas, approval, or
binding rulings.
**Downstream holdouts:** not inspected; they remain paused parity evidence, not blockers.

The baseline worktree was inspected clean. A focused recheck from #912 to #913 found changes only in `metric/` and
the metrics service and its lifecycle tests; none touched an inventoried declaration, graph, trajectory, hierarchy,
research, or index surface. This is inventory only. Independent inventory review has not yet occurred.

## 1. Problem statement

Foundation B established one strict twelve-kind port grammar and normalized `PortFacts`, but the older roadmap's
“Foundation C” was written before those merged changes. The remap question is whether an unresolved
declaration-foundation problem remains, and whether the old Foundation C shape still describes it accurately.

**Measured disposition: the old Foundation C is partially invalidated.**

The narrower gap remains evidenced:

- components still expose effective runtime ports through `InputPorts()` and `OutputPorts()`;
- no registry-owned immutable instance-generation declaration snapshot exists;
- Registry, ComponentManager, flowgraph, capability reporting, message-logger, and stream planning obtain declaration
  truth at different times and through different paths;
- restart/removal has no single component-plus-declaration generation record;
- message-logger and stream planning still interpret raw component configuration.

The old executable shape is not established from the merged tree:

- Foundation B eliminated the former grammar and concrete-type-classification plurality;
- `PortDefinition.Resolve` is now exported;
- components already retain resolved effective `[]Port` values;
- old roadmap symbols such as `Registration.DefaultPorts`, `DeclaredPorts`, and `BuildPortFromDefinition` do not exist;
- stream planning runs before component construction, unlike Registry/flowgraph/reporting;
- no evidence yet establishes that the old exact `Ports() PortConfig` plus replaying Registry observer contract fits
  every remaining consumer.

The remaining problem is therefore **declaration authorship and declaration lifetime plurality**, not the
pre-Foundation-B “multiple port languages” problem.

## 2. Surface inventory

### 2.1 Claimed gap

#### Existing declaration surface

- `component/discovery.go:17-35` defines `Discoverable`, including:
  - `InputPorts() []Port`
  - `OutputPorts() []Port`
- Repository enumeration finds 38 concrete production implementations of each method, excluding tests, helpers, and
  documentation examples.
- `component/ports.go:52-111` defines the typed `PortDefinition` and `PortConfig`.
- `component/ports.go:113-150` performs strict typed decoding and resolution.
- `component/ports.go:153-206` defines named complete-replacement merging.
- `component/port.go:16-48` defines the closed twelve-kind vocabulary.
- `component/port.go:50-64` defines resolved `Port` and the exported `Portable` interface.
- `component/port_resolver.go:10-49` exports `PortDefinition.Resolve`.
- `component/port_resolver.go:87-95` derives facts from a resolved port and revalidates on each call.
- `component/port_facts.go:3-143` defines the normalized, defensively copied facts projection.
- `component/port_codec.go:43-57` contains the canonical kind/binding table.
- `component/schema_tags.go:359-424` projects a config field tagged `type:ports` into generated schema metadata by
  calling `GeneratePortFieldSchema`.
- `component/schema_tags.go:696-752` derives the common port envelope and each closed kind's fields, requirements,
  directions, and `additionalProperties: false` rule from the same canonical binding table.
- Generated schemas are evaluated when package-level component schema variables initialize and retained as
  `ConfigSchema.Properties[*].PortFields`; component registrations retain those schemas as static metadata.

Representative components apply defaults/configuration at factory time, retain resolved input/output slices, and
return defensive slice copies:

- graph-index: `processor/graph-index/component.go:216-221`, `:453-469`
- graph-index-spatial: `processor/graph-index-spatial/component.go:165-170`, `:288-302`
- graph-index-temporal: `processor/graph-index-temporal/component.go:167-172`, `:298-312`
- graph-embedding: `processor/graph-embedding/component.go:240-245`, `:492-506`
- graph-clustering: `processor/graph-clustering/component.go:553-558`, `:797-811`

#### Registry and runtime snapshot search

- `component/registry.go:51-62` defines `Registration`; it contains factories and metadata but no port declarations,
  effective ports, or instance-generation snapshot.
- `component/registry.go:101-117` stores factories, instances, and resources separately.
- `component/registry.go:176-250` creates and registers an instance.
- `component/registry.go:253-300` derives resources by calling the component's port methods.
- `component/registry.go:594-607` re-calls both port methods to derive exclusive resources.
- `component/registry.go:938-979` re-calls them for capability publication.
- `component/registry.go:303-324` removes the instance/resources/factory without a declaration-generation object.

Exact negative search:

```text
rg -n 'DeclaredPorts\(\)|InputPortsOf\(|OutputPortsOf\(|DefaultPorts' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: zero matches.

```text
rg -n 'PortSnapshot|ComponentSnapshot' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: zero matches.

#### Runtime consumers

- `service/component_manager.go:941-1017` creates through Registry, then independently checks conflicts/registers
  ports, retains effective raw `ComponentConfig`, initializes, and invalidates flowgraph.
- `service/component_manager.go:1109-1144` re-calls the component port methods for conflict checking and registration.
- `service/component_manager.go:2372-2417` re-calls them for management-flow reporting.
- `component/flowgraph/flowgraph.go:126-189` re-calls them when adding a component node and retains only node-local
  derived facts.
- `service/component_manager.go:2535-2556` rebuilds the cached flowgraph from current instances.
- `service/component_manager.go:1020-1073` stops/removes/unregisters resources and invalidates flowgraph.
- `service/component_manager.go:1839-1874` stops, unregisters, and recreates a changed configuration.
- `service/component_manager.go:1940-1972` handles configured removal.

No component-plus-effective-declaration atomic generation is present across these paths.

#### Raw configuration consumers

Message logger:

- `service/message_logger.go:20-112` reads the manager's raw flow configuration at construction.
- `service/message_logger.go:302-359` unmarshals nested `PortConfig`, resolves ports, and extracts NATS subjects.
- Resolve/unmarshal/facts failures in this scan are skipped.
- The scan is construction-time; it does not observe successful runtime add/restart/remove generations.

Stream planning:

- `config/stream_bounds.go:204-310` derives stream requirements from constants, operator maps, and enabled raw
  component configurations.
- `config/streams.go:411-425` unmarshals nested `PortConfig`.
- It handles JetStream output facts and derives omitted stream names/policy before runtime I/O.
- This is a pre-component-construction consumer, unlike Registry and flowgraph.

Component-local declaration policy also exists:

- graph-gateway owns its exact three named query output families in
  `gateway/graph-gateway/component.go:140-167`, with stored effective ports at `:250` and accessors at `:514-529`.
- lifecycle-gateway owns its exact mutation output/defaulting policy in
  `gateway/lifecycle-gateway/component.go:78-184`, factory handling at `:279-320`, and accessors at `:366-375`.

### 2.2 Every current spelling of the modeled facts

#### Declaration/effective-port fact

| Spelling | Owner/site | When evaluated | Retained form |
|---|---|---|---|
| Typed configuration declaration | `component.PortConfig`, `component/ports.go:52-111` | Configuration decode | `[]PortDefinition` |
| Component effective ports | 38 implementations of `InputPorts`/`OutputPorts` | Factory/config construction | Component-local `[]Port` |
| Registry resource declaration | `component/registry.go:253-300`, `:594-607` | Registration/conflict inspection | Resource maps separate from instance |
| Capability declaration | `component/registry.go:938-979` | Capability publication | Published derived capability records |
| ComponentManager port registration | `service/component_manager.go:1109-1144` | Create/restart | Manager registration state |
| Flowgraph declaration | `component/flowgraph/flowgraph.go:126-189` | Cache rebuild | Node-local port/fact copies |
| Management reporting | `service/component_manager.go:2372-2417` | HTTP/report request | Freshly re-derived response |
| Message logging discovery | `service/message_logger.go:302-359` | Logger construction | Subject subscription set |
| Stream provisioning | `config/stream_bounds.go:204-310` | Pre-I/O config planning | Stream requirements |
| Generated port-schema projection | `component/schema_tags.go:359-424`, `:696-752` | Package schema initialization | Cached `ConfigSchema.Properties[*].PortFields`, then `Registration.Schema` |
| Gateway query-family policy | `gateway/graph-gateway/component.go:140-167` | Gateway factory | Gateway-local effective ports |
| Lifecycle mutation-family policy | `gateway/lifecycle-gateway/component.go:78-184` | Gateway factory | Gateway-local effective ports |

Foundation B made these consumers share a grammar and normalized fact vocabulary. It did not give them one shared
declaration-generation owner.

#### Status and lifecycle facts

- `graph/kvcatalog.go:58-74` declares:
  - `ENTITY_STATES` as graph authority;
  - `GRAPH_STATUS` as operational status with graph-index, graph-embedding, graph-ingest, and rule owners.
- `graph/readiness/watcher.go:39-70` defines those four readiness keys.
- `graph/readiness/publisher.go:31-105` ensures and publishes status.
- Current publishers:
  - graph-ingest: `processor/graph-ingest/readiness.go:332-355`
  - graph-index: `processor/graph-index/component.go:814`
  - graph-embedding: `processor/graph-embedding/component.go:898`
  - rule: `processor/rule/readiness.go:201`
- No clustering status key or publisher exists.
- `graph/readiness/watcher.go:231-356` watches configured keys and rebinds when the bucket becomes available.
- `graph/readiness/set.go:11-60`, `:107-179` folds a consumer-chosen key set.
- `gateway/graph-gateway/readiness_surface.go:35-105` exposes per-key query readiness.
- `component/lifecycle.go:10-24`, `:44-95` defines process-local component state and managed configuration.
- `service/service_manager.go:1397-1485` exposes service health/liveness/readiness, not graph projection readiness.
- `service/component_manager_http.go:275-350` exposes component health/state/effective config.
- `pkg/lifecycle/manager.go:35-66` implements domain lifecycle convention as graph state in `ENTITY_STATES`.

These use overlapping lifecycle/readiness language but represent different facts, stores, readers, and restart
behavior.

Exact retired-surface search:

```text
rg -n 'COMPONENT_STATUS|BucketComponentStatus|LifecycleReporter|ReportStage|ReportCycle' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: zero production matches.

#### Index/read facts

Catalog ownership is enumerated in `graph/kvcatalog.go:103-138`:

- suffix index — graph-ingest
- outgoing, incoming, alias, predicate, name — graph-index
- spatial — graph-index-spatial
- temporal and temporal-reverse — graph-index-temporal
- embedding and dedup — graph-embedding
- community, summary, anomaly — graph-clustering
- operational readiness — `GRAPH_STATUS`

Acquisition ownership is normalized by `graph/kvcatalog.go:194-245`: owners ensure; readers open.

Runtime mechanisms remain different:

- graph-index owns multiple buckets, a status publisher, watermarks, authority watch, and query handlers:
  `processor/graph-index/component.go:570-815`.
- graph-index-spatial owns one projection and uses resource wait/open plus its own bootstrap state:
  `processor/graph-index-spatial/component.go:443-516`.
- graph-index-temporal owns two projections with its own bootstrap state:
  `processor/graph-index-temporal/component.go:452-536`.
- graph-embedding owns embedding/dedup state, authority watching, hop-one state, and `GRAPH_STATUS` publication:
  `processor/graph-embedding/component.go:615-721`, `:898`.
- graph-clustering owns community/summary/anomaly state, polls authority/index buckets, consumes other readiness keys,
  and does not publish its own readiness: `processor/graph-clustering/component.go:916-1064`, `:1361-1401`.

One current spec collision exists inside `openspec/specs/graph-index/spec.md`:

- `:29-31` and `:261-276` require raw canonical predicate keys and state that `PREDICATE_CATALOG` is retired.
- `:367-405`, particularly `:387-391`, still describes predicate hashes and a predicate catalog.
- Production code uses raw keys:
  - writer: `processor/graph-index/predicate_index.go:12-45`
  - reader: `processor/graph-index/query.go:698-742`

Exact code search:

```text
rg -n 'PREDICATE_CATALOG|PredicateCatalog' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: zero production matches.

Graph-clustering has an explicit declaration/runtime distinction:

- default configuration declares `entity_watch` as `kv-watch` over `ENTITY_STATES`:
  `processor/graph-clustering/component.go:490`, `:513`.
- README calls it dependency/discovery metadata: `processor/graph-clustering/README.md:129`.
- runtime waits for and opens `ENTITY_STATES` but does not create a watcher:
  `processor/graph-clustering/component.go:1160-1225`.
- current spec requires zero steady-state `ENTITY_STATES` watchers:
  `openspec/specs/graph-clustering/spec.md:353-374`.
- the integration contract explicitly proves zero consumers:
  `processor/graph-clustering/entity_watch_scope_integration_test.go:25-126`.

Thus `kv-watch` currently spells both an actual watch interaction elsewhere and dependency/discovery metadata here.

#### Hierarchy and research facts

Hierarchy:

- `processor/graph-ingest/component.go:1838-1978` performs hierarchy inference only on the birth branch of Graphable
  merge.
- canonical request/reply create writes directly and performs no hierarchy work:
  `processor/graph-ingest/canonical_mutations.go:199-262`.
- this matches `openspec/specs/graph-ingest/spec.md:791-801` and ADR-091 at
  `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:83-84`.
- `graph/inference/hierarchy.go:144-234` computes hierarchy triples while also creating containers and inverse/sibling
  evidence.
- sibling discovery scans same-prefix entities at `graph/inference/hierarchy.go:278-315`.
- `graph/inference/hierarchy.go:237-254` retains `OnEntityCreated` as a compatibility entry point, but no production
  call exists.

Exact search:

```text
rg -n '\.OnEntityCreated\(' --glob '*.go' --glob '!**/*_test.go' .
```

Result: zero matches.

Research:

- `processor/research-graph-llmwrap/triplepub.go:15-37` exposes narrow `Create` and `Append` operations.
- `:41-104` uses the canonical graph-mutation request client.
- `:145-175` creates the loop entity before later appends.
- stage-stamp failure is logged and treated as non-fatal at `:114-142`.
- research configurations enable hierarchy, including:
  - `configs/research-graph-e2e.json:379-395`
  - `configs/flows/deep-research-test.json:447-460`
- those RPC-created research entities do not receive hierarchy inference because RPC create is intentionally
  hierarchy-free.
- `test/e2e/scenarios/research-graph/scenario.go:1-17`, `:426-443` states the scenario exercises
  `synthesize_directly` and asserts execute/assess are absent; issue #391's broader-pipeline coverage gap remains.

Relationship application inventory:

- `MutationRelationshipApplier` uses the canonical graph mutation request/reply client and handles typed mutation
  outcomes: `graph/inference/applier.go:208-285`.
- graph-clustering uses `MutationRelationshipApplier` in production.
- graph-gateway still constructs the legacy NATS stream producer at
  `gateway/graph-gateway/component.go:854-896`.
- no production caller constructs `NewDirectRelationshipApplier`.
- no production subscriber was found for `graph.events.relationship.create`.

Exact searches:

```text
rg -n 'graph\.events\.relationship\.create' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: one producer, `gateway/graph-gateway/component.go:876`.

```text
rg -n 'NewDirectRelationshipApplier\(' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: constructor definition only, `graph/inference/applier.go:152`.

### 2.3 Adjacent claims on the territory

#### Governing merged records

- Foundation B explicitly excluded Foundation C declaration authorship/snapshot lifecycle:
  `openspec/changes/archive/2026-08-08-foundation-b-port-language/proposal.md:91`.
- Its design bounded message-logger and stream planning as the remaining raw-config owner families:
  `openspec/changes/archive/2026-08-08-foundation-b-port-language/design.md:88-91`.
- Release evidence says Foundation C, indexes, hierarchy, research ordering, downstream migration, and retention
  require a fresh inventory: `docs/proposals/foundation-b-release-evidence.md:94-95`.
- The old Foundation C shape is recorded at `docs/proposals/post-r1c-foundation-remap-roadmap.md:367-521`.
- The same roadmap mandates a merged-tree remap before continuing:
  `docs/proposals/post-r1c-foundation-remap-roadmap.md:582-585`.

#### Current architectural constraints

- ADR-090 keeps `ENTITY_STATES` authoritative, derived views reconstructible, and explicitly rejects recovery/CQRS
  escalation.
- ADR-090 also requires evidence across graph-index, embedding, and clustering before a shared index primitive is
  claimed.
- ADR-091 establishes graph-ingest as sole physical authority writer, canonical CAS-fenced request/reply mutation,
  valid unresolved references, and Graphable-only hierarchy.
- ADR-049/ADR-091 leave domain lifecycle as a convention over authority state, distinct from component process state
  and graph projection readiness.
- `openspec/specs/component-discovery/spec.md` requires shared consumers to use normalized facts rather than
  concrete-type reclassification.
- `openspec/specs/component-runtime-config/spec.md` owns the strict port grammar.
- `openspec/specs/stream-provisioning/spec.md` owns pre-I/O stream planning.
- `openspec/specs/graph-index-readiness/spec.md` owns readiness classification while leaving owner-specific policy
  local.

#### Active change

`openspec/changes/semantic-tier-split/proposal.md:3-6` is explicitly **SUSPENDED AND FROZEN** and non-executable. Its
quality sequencing remains blocked on issue #829 at `:97-108`. It is an adjacent diagnostic/coverage record, not an
active foundation implementation increment.

#### PR and issue disposition through #909–#913

Merged:

- #909 → `fbac161f`, Foundation B contracts
- #910 → `44d2a322`, trajectory aggregate release
- #911 → `d3ba7ec7`, resolved dispatch input streams
- #912 → `c5f68261`, Foundation B archive/promotion
- #913 → `9d530bf2`, metric/service startup lifecycle only; the focused inventory recheck found no touched surface

Closed by that work:

- #859 — port interpretation plurality
- #873 — discarded trajectory references
- #877 — trajectory published-contract debt
- #884 — GraphQL prefix page
- #876 — terminal trajectory memory retention

Open issues whose premises changed:

- #862 remains open. Component-authored declaration truth still exists, but its exact proposed symbols and pre-B
  grammar assumptions are stale.
- #795 remains open. `readiness.NewSet` now accepts `*natsclient.Client`, and watcher rebinding is internal; the old
  “every caller hand-rolls bind/retry” premise is substantially reduced. The watcher still opens by raw bucket
  lookup rather than the catalog reader seam.
- #688 remains open. Rule mutation now uses the canonical client and CAS; its raw-subject/ownership premise is
  obsolete, while its unrestricted predicate mutation surface remains.
- #689 remains open. Gated-DAG now uses canonical mutation and revision fencing; owner-token semantics and
  indistinguishable unclaim behavior remain observable questions.
- #875 remains open. Foundation B changed trajectory storage reachability, but graph-embedding still falls back from
  registry lookup to an instance-blind content store:
  `processor/graph-embedding/component.go:1934-1954`,
  `graph/embedding/worker.go:981-1017`.
- #881 remains open with a changed premise. Merged trajectory evidence references live in `TrajectoryFactV1` KV facts
  and do not create trajectory graph writes, so trajectories do not create the entity population the issue predicted.
  Its independently evidenced question is limited to observability for non-trajectory graph entities carrying an
  unresolved `StorageRef`.
- #888 remains open with the same changed trajectory premise. No current trajectory path supplies its proposed E2E
  population. The independently evidenced coverage question is limited to non-trajectory graph entities whose
  `StorageRef.StorageInstance` is unresolved by graph-embedding.
- #690 remains open with a changed premise. `MutationRelationshipApplier` now uses canonical request/reply mutation
  and typed outcomes. The narrower remaining evidence is the gateway's legacy relationship-stream producer with no
  production consumer and the constructor-only `NewDirectRelationshipApplier` path.

Open issues still directly evidenced:

- #820 — clustering has no `GRAPH_STATUS` contract.
- #868 — readiness publisher is typed to `graph.IndexStatusResponse`.
- #829 — clustering does not wire the existing `ContentFetcher` seam:
  `processor/graph-clustering/component.go:2168-2175`,
  `graph/clustering/summarizer.go:508-530`.
- #828 — production predicate membership uses raw canonical keys and no `PREDICATE_CATALOG`, while stale sections in
  `openspec/specs/graph-index/spec.md:367-405` and ADR-068 D3 still reason from hash-plus-catalog layout.
- #391 — research E2E does not cover the full pipeline.
- #436 — hierarchy container entities are consumed as graph entities.
- #746 — research/rule first-wins companion-predicate behavior remains.
- #751 — hierarchy creation behavior remains.
- #784, #785, #786 — query/capability contract debts remain.
- #882, #883, #885, #886 — gateway scope, projection, paging, and relationship-schema debts remain.
- #810 and #842 remain adjacent to declaration/provisioning timing and were not resolved by Foundation B.

### 2.4 Consumer at birth

No new exported symbol, port, subject, bucket, or configuration field is proposed in this inventory, so there is no
proposed surface requiring a new-consumer justification.

Current consumers of the surfaces under review are:

| Existing surface | Present consumers |
|---|---|
| `Discoverable.InputPorts/OutputPorts` | Registry, ComponentManager, flowgraph, capability publication, management HTTP |
| `PortDefinition.Resolve` / `Port.Facts` | factories, Registry, ComponentManager, flowgraph, message-logger, stream planning, gateway validators |
| generated `ConfigSchema` port metadata | component registration/schema discovery, config validation, and configuration-form consumers |
| raw configured `PortConfig` | stream planner and message-logger |
| `GRAPH_STATUS` | graph-gateway readiness surface, clustering readiness gates, diagnostic consumers |
| component state/health | service readiness endpoints and component-manager HTTP |
| domain lifecycle graph state | lifecycle manager and graph query consumers |
| suffix index | graph-ingest writes; query resolution consumes |
| graph indexes | graph query, gateways, clustering, embedding, tests |
| hierarchy facts/entities | graph traversal, gated-DAG, research and structural consumers |
| research `Create`/`Append` | research LLM wrapper/pipeline |
| `graph.events.relationship.create` | graph-gateway producer; no production consumer found |
| `NewDirectRelationshipApplier` | no production consumer found |
| `HierarchyInference.OnEntityCreated` | no production consumer found |

## 3. Same-class collision tables

### 3.1 Declaration and effective-configuration class

| Dimension | Evidence |
|---|---|
| Semantic class | Effective component communication/resource declaration |
| Owners | Component factories/instances, schema generator, Registry, ComponentManager, flowgraph, message-logger, stream planner, component-local gateway validators |
| Catalogs | `PortConfig`, twelve-kind codec table, generated `PortFields`, Registry registrations/resources/capabilities, flowgraph nodes |
| Status | Capability and management projections expose declarations; no dedicated declaration-generation status |
| Lifecycle | Factory construction, registry insertion, manager initialization/start, cache rebuild, stop/unregister/recreate/removal |
| Ownership | Grammar ownership is centralized; effective declaration lifetime has no single retained owner |
| Readers | Registry conflict/capability/schema paths, config validation/form consumers, manager reporting, flowgraph, message-logger, provisioning |
| Writers | Config decoder, component factories, gateway-local default/validation logic |
| Schema projection | Evaluated during package schema initialization; retained in cached component `ConfigSchema` and `Registration.Schema` |
| Recovery | Runtime configuration reconciliation reconstructs components; flowgraph is rebuilt; message-logger uses its construction-time scan; stream planning re-runs from raw config |

### 3.2 Status/lifecycle class

| Semantic owner | Catalog/store | Status surface | Lifecycle/restart | Readers | Writers | Recovery/failure |
|---|---|---|---|---|---|---|
| Graph projection readiness | `GRAPH_STATUS` | Four named keys | Watcher rebind; owner republishes | gateway, clustering, diagnostics | ingest, index, embedding, rule | Missing bucket/key stays unknown; owner policy local |
| Component process lifecycle | memory/managed component | created/initialized/started/stopped/failed plus health | manager start/stop/recreate | service/component HTTP | ComponentManager/components | rebuilt from configured components |
| Service health/readiness | in-memory service state | `/health`, `/healthz`, `/readyz` | service startup/shutdown | operators/orchestrators | ServiceManager | process-local |
| Domain lifecycle | `ENTITY_STATES` triples | entity phase/history convention | CAS mutations and authority replay | lifecycle/query consumers | lifecycle manager through graph mutation | authority state survives process restart |
| Retired component status | none | none | none | none | none | exact production search returned zero |

### 3.3 Derived-index class

| Owner | Cataloged state | Status | Update/rebuild model | Readers | Writers |
|---|---|---|---|---|---|
| graph-ingest | suffix index | ingest readiness | authority-owner update | partial-ID resolution | graph-ingest |
| graph-index | outgoing/incoming/alias/predicate/name | `KeyGraphIndex` | authority watch, watermark/reconciliation | graph query, clustering | graph-index |
| graph-index-spatial | spatial | no `GRAPH_STATUS` key | own bootstrap/watch behavior | spatial query | spatial component |
| graph-index-temporal | temporal/reverse | no `GRAPH_STATUS` key | own bootstrap/watch behavior | temporal query | temporal component |
| graph-embedding | embedding/dedup | `KeyGraphEmbedding` | authority watch, watermark/hop-one state | semantic query/clustering | embedding component |
| graph-clustering | community/summary/anomaly | consumes other status; publishes none | timer-driven polled authority/index reads | community/query/generation | clustering component |

Catalog acquisition is shared; reconciliation, freshness evidence, readiness publication, and lifecycle remain
owner-specific.

### 3.4 Hierarchy/research mutation class

| Dimension | Evidence |
|---|---|
| Semantic class | Entity birth plus explicit/derived relationship evidence |
| Owners | graph-ingest Graphable lane, canonical mutation service, hierarchy inference, research publisher, inference appliers |
| Catalogs | `ENTITY_STATES`, mutation operation catalog, hierarchy vocabulary |
| Status | graph-ingest readiness; research stage observations; no hierarchy-specific readiness |
| Lifecycle | Graphable first birth may infer hierarchy; RPC birth does not; later append is must-exist |
| Ownership | graph-ingest is sole physical writer; callers own mutation policy; hierarchy owns derived companion attempts |
| Readers | graph query/indexes, gated-DAG, research scenarios |
| Writers | Graphable merge, canonical create/reconcile/append/delete, hierarchy Create/CAS, legacy relationship-stream producer |
| Recovery | Authority replays through KV watch; companion failures may remain dangling eventual state; no repair/recovery subsystem is specified |

## 4. Adopter seam inventory

| Adopter | What they must know today | If they do nothing | Where they find out | What they should have to know |
|---|---|---|---|---|
| External component author | Closed port kinds, valid direction, complete-replacement override behavior, and that returned effective ports must match runtime acquisition | Invalid declarations fail boot; a component whose returned declarations diverge from its runtime helpers can produce misleading flow/resource/capability data | Compile error for types; boot error for grammar; divergence may only appear in runtime behavior/logs | The semantic resources/interfaces the component consumes and provides |
| Flow-config author | Exact nested `config.kind` grammar, component-recognized names, kind immutability, required graph mutation/query families | Unknown fields/kinds/names fail startup; omitted required ports fail startup | Boot validation | Component-specific semantic wiring, without internal subject/bucket prediction |
| Message-logger operator | `"*"` means construction-time raw-config discovery; only NATS subjects are discovered; invalid declarations can be skipped by that scan | Runtime component changes may not update subscriptions; skipped rows can become missing logs | Logs or missing observations; not a typed boot failure at this seam | Which declared message surfaces should be observed |
| Stream-planning operator | Stream planning occurs from preconstruction raw config and only sees declared JetStream output facts plus bounded special cases | Required runtime streams can differ if effective component defaults and raw configured outputs differ | Config/provisioning failure or runtime unavailability | Desired retention/policy where operator-owned; not component default reproduction |
| Readiness consumer | Exact readiness keys relevant to its operation and distinction between per-key state and component/service health | Omitted dependencies are not considered; absent selected keys remain unknown | Typed readiness response/runtime gate | The capability it needs, not internal producer keys |
| Graph query/gateway adopter | Query family, response shape, pagination/scope behavior, and which indexes/readiness states cover the operation | Current open gateway issues can yield incomplete projection/scope/paging semantics | Typed runtime response, then docs/issues for behavioral limitations | Query intent and requested result shape |
| Derived-index author | Catalog ownership, authority read contract, poison classification, freshness/readiness contract, and its own rebuild behavior | Index may work locally while exposing no interoperable readiness/freshness evidence | Boot/runtime errors, metrics/status if implemented | The derived fact and transformation; shared acquisition/classification mechanics should not be adopter prediction |
| Research component author | Create-before-append, RPC create is hierarchy-free, and stage-observability failure is non-fatal | Append to absent entity returns not-found; hierarchy-config presence does not add hierarchy to RPC births; stage evidence may be missing | Typed mutation error and warning log | Intended entity facts and explicit relationships |

Prediction-shaped seams currently observed:

- message-logger predicts runtime subjects from initial raw configuration;
- stream planning predicts postconstruction output requirements from preconstruction configuration;
- component authors must keep runtime acquisition consistent with separately exposed ports;
- readiness consumers predict the internal key set representing their dependencies;
- research writers must order birth before append;
- hierarchy behavior depends on knowing which ingest lane created the entity.

## 5. Exact negative searches

```text
rg -n 'DeclaredPorts\(\)|InputPortsOf\(|OutputPortsOf\(|DefaultPorts' \
  --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n 'PortSnapshot|ComponentSnapshot' \
  --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n 'COMPONENT_STATUS|BucketComponentStatus|LifecycleReporter|ReportStage|ReportCycle' \
  --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n 'KeyGraphClustering' --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n 'PREDICATE_CATALOG|PredicateCatalog' \
  --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n '\.OnEntityCreated\(' --glob '*.go' --glob '!**/*_test.go' .
# zero matches
```

```text
rg -n 'graph\.events\.relationship\.create' \
  --glob '*.go' --glob '!**/*_test.go' .
# one producer; no production subscriber found
```

```text
rg -n 'NewDirectRelationshipApplier\(' \
  --glob '*.go' --glob '!**/*_test.go' .
# constructor definition only
```

## 6. Open evidence questions for inventory review

1. Do any production components mutate their stored effective input/output slices after factory construction?
2. Can Registry capability publication race restart/removal and publish facts from a superseded instance generation?
3. Which shipped configurations depend on component-default JetStream outputs that are absent from the raw
   configuration seen by stream planning?
4. Is graph-clustering's `entity_watch` intentionally a metadata-only declaration despite `kv-watch` denoting a watch
   interaction everywhere else?
5. Is issue #875's foreign-`StorageRef` fallback reachable from current post-Foundation-B producers, and what resident
   reference population demonstrates it?
6. Which projection owners are intended to expose interoperable `GRAPH_STATUS` state, particularly clustering,
   spatial, and temporal?
7. Is `graph.events.relationship.create` provisioned in any shipped flow despite having no production consumer?
8. Which operator-facing document distinguishes service readiness, component health/state, graph projection
   readiness, and domain lifecycle?
9. Does any current research path rely on hierarchy configuration for entities born exclusively through RPC create?
10. Does any present consumer call `HierarchyInference.OnEntityCreated` or construct `DirectRelationshipApplier`
    through reflection/generated wiring not visible to the production Go search?
11. Does the current graph-index spec's stale hashed-predicate section affect generated validation or documentation
    despite production code using raw keys?
12. Which parts of #810/#842 depend specifically on declaration timing versus stream-provisioning behavior?

This inventory is ready for independent SemStreams inventory review.
