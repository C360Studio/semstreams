# Foundation B mandatory inventory checkpoint

Inventory only. No target-state selection, implementation plan, recommendation, task list, file edit, or Git mutation was performed.

## 1. Checkpoint identity and Foundation A premise

- Worktree: `/private/tmp/semstreams-gs00`
- Branch: `codex/foundation-b-port-grammar`
- HEAD: `61022ae1b4da0309e93ce49ec00c9c64679d09d8`
- Tracked baseline status before materialization: clean.
- Checkpoint artifact status: untracked `docs/proposals/foundation-b-port-language-inventory.md` exists above the tracked
  baseline.
- Prior artifact identity verified before this correction: 719 lines, 38,847 bytes, SHA256
  `7762ae3e0d8ef5059e1beee0d399c98d04ab50575c3a335ca60d0c73dcfa0882`.
- Foundation A range inspected: `c38e3e82..61022ae1`
- Range size: 65 changed files.
- No files under `component/port*.go`, `component/ports.go`, `component/registry.go`, `component/flowgraph/`, `config/`, or `schemas/` changed in that range.
- Two message-logger KV support files changed as part of component-status retirement, but the raw port interpreter at `service/message_logger.go:289-340` did not.
- A zero-context diff search for `InputPorts`, `OutputPorts`, `PortDefinition`, `PortConfig`, `BuildPort`, `KVRead`, `KVWrite`, or `PortKind` additions/removals under `component/**` returned empty.
- The production retirement search for `BucketComponentStatus|COMPONENT_STATUS|LifecycleReporter|ReportStage|ReportCycle`, excluding historical documents and the retirement contract test, returned empty.
- Foundation A removed the component-status plane and its E2E exact-read helper cluster. It did not change the Foundation B port grammar, decoders, builders, flowgraph interpreters, registry/manager projections, configuration lanes, or claimed exact-read consumers.

Result: Foundation A does not invalidate the Foundation B inventory premise. It does reduce the adjacent exact-KV-reader population by removing the retired `COMPONENT_STATUS` reader path.

## 2. Claimed-gap verification

| Roadmap claim | Merged-tree evidence | Inventory result |
|---|---|---|
| Port semantics are spelled independently across definition JSON, runtime JSON, resolver, flowgraph, capability reporting, and management reporting | `component/port.go:42-179`; `component/ports.go:35-152,192-371`; `component/flowgraph/flowgraph.go:165-320`; `component/registry.go:1008-1033`; `service/component_manager.go:2291-2343` | Verified |
| The present canonical semantic classes are timer, network, file, http-client, nats, nats-request, jetstream, kv-watch, kv-write, store-read, store-provide, plus the proposed kv-read | Concrete types exist for all except KVRead, but several cannot survive all existing paths | Altered: concrete type presence is broader than end-to-end support |
| `PortKind` does not yet exist | Exact production search for `\\bPortKind\\b` returned empty | Verified |
| `KVReadPort` does not yet exist | Exact production search for `type KVRead|KVReadPort` returned empty | Verified |
| `kv_read` is an ignored top-level lane | `PortConfig` has only `Inputs`, `Outputs`, and `KVWrite` at `component/ports.go:155-160`; shipped configurations contain `kv_read`, which unmarshalling drops | Verified |
| `PortConfig.KVWrite` is an existing special lane | Field exists at `component/ports.go:159`; code and shipped configs populate it | Verified, but the lane has no runtime consumer |
| Aliases and flat-field precedence exist | Decoder/builder accept `kv`, `kvwatch`, `kvwrite`, `http`, `grpc`, and `websocket-server`; `PortDefinition` has flat fields plus `Config any` | Verified |
| Unknown definitions can silently become NATS | `BuildPortFromDefinition` defaults every unhandled kind to `NATSPort` at `component/ports.go:357-368` | Verified |
| Registry capability reporting yields unknown for ordinary runtime ports | Only pointer `*NATSStreamPortConfig` and `*NATSRequestPortConfig` are recognized at `component/registry.go:1017-1028` | Verified |
| Raw component configuration has two interpreter-owner families | Message-logger discovers flat subjects at `service/message_logger.go:289-340`; stream planning decodes ports at `config/streams.go:430-441` and selects/derives JetStream outputs at `config/stream_bounds.go:243-275,326-346` | Verified; their accepted rows and projections differ |
| Graph-clustering has three real exact/list KV reads | Runtime opens and reads `ENTITY_STATES`, `OUTGOING_INDEX`, and `INCOMING_INDEX` | Verified |
| Agentic-tools has current exact KV reads | `ENTITY_STATES` and `AGENT_LOOPS` exact/list consumers exist | Verified, with distinct acquisition behavior |
| Graph-query has a current exact `ENTITY_STATES` read matching shipped `kv_read` rows | No production `ENTITY_STATES` KV acquisition or read exists in `processor/graph-query` | Not verified; current rows are dead/mismatched |
| Store federation is distinct from exact KV identity matching | Store readers connect to every provider, with per-reference runtime selection | Verified at `component/flowgraph/flowgraph.go:597-630` |
| An extension mechanism currently has consumers | No production custom-kind registry or registration mechanism was found; only permissive unknown decoding comments/behavior exist | No present extension surface or consumers found |

## 3. Current outward port surface

### 3.1 Runtime surface

`component.Port` is exported and contains:

- `Name`
- `Direction`
- `Required`
- `Description`
- `Config Portable`

Evidence: `component/port.go:10-32`.

`Portable` requires:

- `ResourceID() string`
- `IsExclusive() bool`
- `Type() string`

Runtime `Port` JSON is:

```json
{
  "name": "...",
  "direction": "...",
  "required": true,
  "description": "...",
  "config": {
    "type": "...",
    "data": {}
  }
}
```

Evidence: `component/port.go:42-73`.

Runtime `Port.UnmarshalJSON` recognizes:

- `timer`
- `network`
- `nats`
- `nats-request`
- `file`
- `jetstream`
- `kvwatch`
- `kv-watch`
- `kvwrite`
- `kv-write`
- `kv`
- `http-client`

It does not recognize `store-read`, `store-provide`, `http`, `grpc`, `websocket-server`, `stream`, `request`, or `kv-read`. Unknown runtime kinds fail with a typed invalid error. Evidence: `component/port.go:76-179`.

### 3.2 Definition/configuration surface

`PortDefinition` exports these flat fields at `component/ports.go:35-49`:

- `name`
- `type`
- `subject`
- `interface`
- `required`
- `description`
- `timeout`
- `stream_name`
- `bucket`
- `config any`

Its wire shape uses top-level `type` as the discriminator and the direct `config` object as typed payload. This differs from runtime `Port`’s nested `{"type","data"}` envelope. The distinction is explicitly documented at `component/ports.go:51-69`.

`PortDefinition.UnmarshalJSON` recognizes:

- `jetstream`
- `nats`
- `nats-request`
- `kvwatch`
- `kv-watch`
- `kv`
- `kv-write`
- `kvwrite`
- `timer`
- `network`
- `http`
- `grpc`
- `websocket-server`
- `file`
- `http-client`

It does not have typed cases for `store-read`, `store-provide`, or `kv-read`. Unknown kinds retain `Config` as `map[string]any`, allowing forward decoding but not semantic resolution. Evidence: `component/ports.go:70-152`.

`PortConfig` has:

```go
Inputs  []PortDefinition
Outputs []PortDefinition
KVWrite []PortDefinition
```

There is no `KVRead` field. Evidence: `component/ports.go:155-160`.

### 3.3 Effective runtime methods

Every external component implementing `Discoverable` must provide:

- `InputPorts() []Port`
- `OutputPorts() []Port`

Evidence: `component/discovery.go:17-35`.

These methods are read independently by:

- flowgraph node construction;
- registry conflict tracking;
- registry capability announcements;
- ComponentManager conflict tracking;
- ComponentManager management reporting.

### 3.4 Builder and merge surface

`MergePortConfigs`:

- merges one direction at a time;
- indexes overrides by name;
- silently keeps the last duplicate override;
- permits unknown additional names;
- uses `BuildPortFromDefinition`;
- appends remaining map entries in nondeterministic map order.

Evidence: `component/ports.go:162-190`.

`BuildPortFromDefinition` is exported, returns no error, and resolves:

- `timer`
- `jetstream`
- `nats-request`
- `kv-watch`, `kvwatch`
- `kv`, `kv-write`, `kvwrite`
- `store-read`
- `http-client`
- `http`, `grpc`, `websocket-server`

Everything else, including explicit `nats`, `network`, `file`, `store-provide`, unknown kinds, and empty type, reaches the default `NATSPort` branch. Evidence: `component/ports.go:192-371`.

## 4. Concrete kind and alias inventory

| Semantic class | Concrete `Type()` | Definition decoder | Runtime decoder | Builder | Current production literal construction |
|---|---|---|---|---|---:|
| timer | `timer` | `timer` | `timer` | Timer; interval taken from flat `subject` | 1, builder only |
| network | `network` | `network`, `http`, `grpc`, `websocket-server` | `network` | Only `http/grpc/websocket-server` build Network; explicit `network` falls to NATS | 3 |
| file | `file` | `file` | `file` | No `file` case; falls to NATS | 1 |
| http-client | `http-client` | `http-client` | `http-client` | Typed merge supported | 1, builder only |
| nats | `nats` | `nats` | `nats` | No explicit case; default branch happens to construct NATS | 35 |
| nats-request | `nats-request` | `nats-request` | `nats-request` | Typed merge with 1s default | 7 |
| jetstream | `jetstream` | `jetstream` | `jetstream` | Partial typed-field merge | 10 |
| kv-watch | concrete returns `kvwatch` | `kv-watch`, `kvwatch` | `kv-watch`, `kvwatch` | both | 2 |
| kv-read | absent | absent | absent | absent | 0 |
| kv-write | concrete returns `kvwrite` | `kv`, `kv-write`, `kvwrite` | same three | same three | 2, documentation/builder only |
| store-read | `store-read` | generic map only | rejected | typed flat-field builder exists | 2 |
| store-provide | `store-provide` | generic map only | rejected | falls to NATS | 1 |
| dead stream config | `stream` | absent | rejected | absent | 0 |
| dead request config | `request` | absent | rejected | absent | 0 |

The production literal-construction census excluded tests and searched `*.go` for concrete struct literals. Counts include builder construction where applicable.

The two dead types are `NATSStreamPortConfig` and `NATSRequestPortConfig` at `component/port_nats.go:12-53`. Production construction count for both is zero. Registry capability reporting is their only production type switch.

### 4.1 Concrete fields and identities

- `TimerPort`: interval and optional interface; resource `timer:<interval>`; nonexclusive. `component/port_timer.go:5-24`.
- `NetworkPort`: protocol, host, port; resource `<protocol>:<host>:<port>`; exclusive. `component/port_network.go:5-25`.
- `FilePort`: path and pattern; resource `file:<path>`; nonexclusive. `component/port_file.go:5-24`.
- `HTTPClientPort`: method, URL pattern, trigger port, auth ref, contact policy, interface; default GET only in resource identity; nonexclusive. `component/port_httpclient.go:5-44`.
- `NATSPort`: subject, queue, interface; resource `nats:<subject>`; nonexclusive. `component/port_nats.go:5-10,56-69`.
- `NATSRequestPort`: subject, timeout, retries, interface; resource `nats-request:<subject>`; nonexclusive. `component/port_nats.go:71-92`.
- `JetStreamPort`: stream name, subjects, storage, retention, size, replicas, consumer name, delivery/ack policies, max deliver, ack wait, heartbeat, max ack pending, interface; resource stream name, else first subject, else `unknown`; nonexclusive. `component/port_jetstream.go:9-74`.
- `KVWatchPort`: bucket, keys, history, interface; resource `kvwatch:<bucket>`; nonexclusive. `component/port_kv.go:5-26`.
- `KVWritePort`: bucket and interface; resource `kvwrite:<bucket>`; nonexclusive. `component/port_kv.go:28-47`.
- `StoreReadPort`: bucket and interface; resource `store-read:<bucket>`; nonexclusive. `component/port_store.go:5-27`.
- `StoreProvidePort`: instance; resource `store-provide:<instance>`; nonexclusive because collision ownership is delegated to StoreRegistry. `component/port_provide.go:5-35`.

### 4.2 Builder field preservation

- Timer ignores typed `TimerPort`; flat `Subject` becomes interval.
- JetStream seeds `StreamName` and one-element `Subjects`, then copies only:
  - `DeliverPolicy`
  - `AckPolicy`
  - `MaxDeliver`
  - `ConsumerName`
  - `AckWait`
  - `HeartbeatInterval`
- JetStream builder does not preserve typed:
  - `Subjects`
  - `Storage`
  - `RetentionPolicy`
  - `RetentionDays`
  - `MaxSizeGB`
  - `Replicas`
  - `MaxAckPending`
  - `Interface`
- NATS request preserves typed subject, timeout, retries, and interface.
- KV watch preserves only bucket. Typed keys, history, and interface are dropped.
- KV write preserves bucket and flat interface. Typed config is ignored.
- Store read preserves bucket and flat interface, although its decoder does not create typed `StoreReadPort`.
- HTTP client merges its typed fields.
- `network` and `file` typed decoder outputs are ignored by the builder because those cases are absent.
- `store-provide` is not buildable.
- No builder direction validation exists; direction is copied from the caller argument.

Evidence: `component/ports.go:192-371`.

## 5. Production definition/configuration census

### 5.1 Executable Go `PortDefinition` constructions

A syntax-aware `go/ast` plus `go/types` census found 125 non-test `PortDefinition` composite literals. One is the
reflection sentinel `PortDefinition{}` at `component/schema_tags.go:705`; it generates UI metadata and is not an
executable port declaration. Excluding that sentinel leaves 124 executable declaration constructions:

| Spelling | Count |
|---|---:|
| `file` | 1 |
| `http` | 2 |
| `jetstream` | 39 |
| `kv-watch` | 11 |
| `kv-write` | 17 |
| `nats` | 31 |
| `nats-request` | 20 |
| `network` | 2 |
| `store-read` | 1 |

Total: 124.

Empty production-literal categories:

- `timer`
- `http-client`
- `grpc`
- `websocket-server`
- `kvwatch`
- `kvwrite`
- `kv-read`
- `store-provide`

Construction sites, grouped by source file and kind, are:

- `examples/processors/document/component.go:39,49` — `nats`.
- `examples/processors/iot_sensor/component.go:40,50` — `nats`.
- `examples/processors/weather_station/component.go:34,43` — `nats`.
- `gateway/graph-gateway/component.go:128,172` — `http`; `:137,156,179` — `nats-request`.
- `gateway/lifecycle-gateway/component.go:158` — `nats-request`.
- `input/udp/udp.go:209` — `network`; `:218` — `nats`.
- `input/websocket/config.go:97,104` — `nats`.
- `output/file/file.go:57` — `nats`; `:67` — `file`.
- `output/httppost/httppost.go:67` — `nats`.
- `output/otel/component.go:198` — `jetstream`.
- `output/otel/config.go:59,67` — `jetstream`.
- `output/websocket/websocket.go:88` — `nats`; `:100` — `network`; `:806` — `nats`.
- `processor/agentic-dispatch/config.go:64,72,80,88,96,106,113,120,127` — `jetstream`.
- `processor/agentic-governance/config.go:189,197,205,216,224,232,240` — `jetstream`; `:248` — `nats`.
- `processor/agentic-loop/config.go:393,401,409,417,425,433,441,458,465,472,479,486,493,500,507` —
  `jetstream`; `:451` — `nats-request`; `:516` — `kv-write`.
- `processor/agentic-model/config.go:129,140` — `jetstream`; `:148` — `nats`.
- `processor/agentic-tools/config.go:138,162` — `jetstream`; `:146` — `nats`; `:155` — `nats-request`.
- `processor/graph-clustering/component.go:439,472,501` — `kv-write`; `:456,487` — `kv-watch`;
  `:465,494` — `nats-request`.
- `processor/graph-embedding/component.go:191,205` — `kv-watch`; `:210` — `store-read`.
- `processor/graph-index-spatial/component.go:113,137` — `kv-watch`; `:122,144` — `kv-write`.
- `processor/graph-index-temporal/component.go:115,139` — `kv-watch`; `:124,146` — `kv-write`.
- `processor/graph-index/component.go:140,179` — `kv-watch`; `:149,154,159,164,186,191,196,201` —
  `kv-write`.
- `processor/graph-ingest/component.go:392` — `jetstream`; `:400` — `nats-request`; `:409` — `kv-write`.
- `processor/graph-query/component.go:94,95,96,97` — `nats-request`.
- `processor/json_filter/json_filter.go:39,50` — `nats`.
- `processor/json_generic/json_generic.go:31,41` — `nats`.
- `processor/json_map/json_map.go:41,52` — `nats`.
- `processor/research-graph-assess/config.go:111` — `nats`; `:119` — `nats-request`.
- `processor/research-graph-classify/config.go:79` — `nats`; `:87` — `nats-request`.
- `processor/research-graph-execute/config.go:99` — `nats`; `:107` — `nats-request`.
- `processor/research-graph-route/config.go:95` — `nats`; `:103` — `nats-request`.
- `processor/research-graph-synthesize/config.go:112` — `nats`; `:120` — `nats-request`.
- `processor/rule/config.go:223` — `kv-watch`; `:231` — `nats-request`; `:238` — `nats`.
- `storage/objectstore/config.go:77,96,104` — `nats`; `:85` — `nats-request`.

The syntax analyzer loaded `./...` with `packages.NeedSyntax|NeedTypes|NeedTypesInfo`, disabled test variants, identified
composite literals whose resolved type was `component.PortDefinition`, and read the constant value of each keyed
`Type` element. Comments and documentation examples are absent from the AST. The exact command was:

```text
go run /private/tmp/foundation_b_port_ast.go
```

The analyzer additionally reported the one `<missing>` reflection sentinel separately; it was not folded into any kind
or into the executable total.

### 5.2 Shipped JSON configuration rows

A recursive `jq` census over every `configs/**/*.json` array named `inputs`, `outputs`, `kv_read`, or `kv_write` found 522 rows:

| Type spelling | Rows | Files |
|---|---:|---:|
| `http` | 8 | 8 |
| `jetstream` | 268 | 22 |
| `kv` | 57 | 9 |
| `kv-read` | 9 | 7 |
| `kv-watch` | 40 | 15 |
| `kv-write` | 33 | 15 |
| `nats` | 23 | 9 |
| `nats-request` | 83 | 20 |
| `network` | 1 | 1 |

Lane totals:

| Lane | Rows |
|---|---:|
| `inputs` | 218 |
| `outputs` | 279 |
| `kv_read` | 9 |
| `kv_write` | 16 |

The ordinary `inputs`/`outputs` population already contains both 57 `kv` aliases and 33 canonical `kv-write` rows.

### 5.3 Exact top-level `kv_read` rows

All nine are `kv-read` for `ENTITY_STATES`:

| File/component | Rows | Runtime consumer |
|---|---:|---|
| `configs/agentic.json` / agentic-tools | 1 | Real |
| `configs/examples/research-graph-pipeline.json` / agentic-tools | 1 | Real |
| same file / graph-query | 1 | No matching consumer |
| `configs/flows/deep-research-test.json` / agentic-tools | 1 | Real |
| `configs/flows/deep-research.json` / agentic-tools | 1 | Real |
| `configs/flows/lesson-example.json` / agentic-tools | 1 | Real |
| `configs/flows/ops-agent.json` / agentic-tools | 1 | Real |
| `configs/research-graph-e2e.json` / agentic-tools | 1 | Real |
| same file / graph-query | 1 | No matching consumer |

Because `PortConfig` has no `KVRead`, all nine rows are ignored during canonical port-config unmarshal.

### 5.4 Exact top-level `kv_write` rows

Sixteen rows occur in nine files. Every row belongs to agentic-loop:

- nine `AGENT_LOOPS` rows;
- seven `AGENT_TRAJECTORIES` rows.

Agentic-loop’s code default also populates `PortConfig.KVWrite` with `AGENT_LOOPS` at `processor/agentic-loop/config.go:515-522`.

The constructor builds only `Ports.Inputs` and `Ports.Outputs` at `processor/agentic-loop/component.go:147-169`; `InputPorts` and `OutputPorts` return only those stored slices at `processor/agentic-loop/component.go:290-298`.

Exact production search for `.Ports.KVWrite`, `Ports.KVWrite`, or `.KVWrite` consumers returned empty. The field and rows decode but do not contribute runtime port declarations.

### 5.5 Syntax-aware interpreter and renderer census

The same type-checked AST pass enumerated three production categories. It excluded `*_test.go`, `test/**`, and the
test-only production-named helper `component/test_helpers.go`. E2E and example binaries remain included because they are
compiled non-test Go packages and exercise the public component surface.

#### `PortDefinition.Type` interpretations

There are 93 resolved selector occurrences at 78 unique file:line sites. A multiplier records multiple selector
occurrences on one source line:

- `component/ports.go:86,148,202,353`.
- `config/stream_bounds.go:260`.
- `examples/processors/document/component.go:136` ×2, `:200,202,230,305`.
- `examples/processors/iot_sensor/component.go:137` ×2, `:202,204,232,307,577`.
- `examples/processors/weather_station/component.go:110`.
- `gateway/lifecycle-gateway/component.go:105`.
- `input/file/file.go:158` ×2, `:193` ×2, `:610`.
- `input/http/config.go:227` ×2, `:318` ×2, `:320`.
- `input/udp/udp.go:168,240,759`; `:193,257` ×2 each.
- `input/websocket/websocket_input.go:1182`.
- `output/file/file.go:145` ×2, `:267,292`.
- `output/httppost/httppost.go:138` ×2, `:276,301`.
- `output/otel/component.go:192,483`.
- `output/websocket/websocket.go:819`.
- `processor/agentic-governance/component.go:491,519`.
- `processor/agentic-tools/component.go:148,909`.
- `processor/graph-embedding/component.go:1128`.
- `processor/graph-ingest/component.go:1337,1338`.
- `processor/json_filter/json_filter.go:122,129` ×2 each, `:219,246,423`.
- `processor/json_generic/json_generic.go:106` ×2, `:188,214,376`.
- `processor/json_map/json_map.go:127` ×2, `:231,258,416`.
- `processor/research-graph-assess/component.go:211,214`.
- `processor/research-graph-classify/component.go:240,243`.
- `processor/research-graph-execute/component.go:161,164`.
- `processor/research-graph-route/component.go:233,236`.
- `processor/research-graph-synthesize/component.go:191,194`.
- `processor/rule/processor.go:974` ×2, `:1034,1054`.
- `processor/rule/publisher.go:71`.
- `service/message_logger.go:315,328`.
- `storage/objectstore/component.go:625,638,982`.

These sites include definition decoding/building, validation, runtime subscription setup, stream derivation, component
rendering, and raw-config observation. The count is selector occurrences, not a count of semantic owners.

#### Runtime `Port.Config` type switches and assertions

There are 16 syntax/type-checked assertions whose asserted expression resolves to `component.Portable`:

- `component/flowgraph/flowgraph.go:190,218,252` — type switches for interface, pattern, and connection ID.
- `component/flowgraph/flowgraph.go:416,459,1059,1082` — JetStream assertions.
- `component/port_jetstream.go:105` — JetStream consumer-config assertion.
- `component/registry.go:584` — Network assertion for port validation.
- `component/registry.go:1018` — capability type switch.
- `gateway/lifecycle-gateway/component.go:104` — NATS request assertion.
- `processor/agentic-loop/component.go:704` — subscription type switch.
- `processor/agentic-loop/component.go:767` — JetStream stream-name assertion.
- `processor/graph-embedding/component.go:477` — StoreRead assertion.
- `processor/graph-ingest/canonical_mutations.go:165` — NATS request assertion.
- `service/component_manager.go:2330` — management-detail type switch.

#### `InputPorts`/`OutputPorts` renderers

There are 76 production methods: six return an already-stored slice, while 70 hand-roll, delegate, merge, or reconstruct
the effective view.

Stored-slice methods:

- `processor/agentic-dispatch/component.go:214,219`.
- `processor/agentic-loop/component.go:291,296`.
- `processor/rule/processor.go:381,386`.

Hand-rolled renderer methods, exhaustively:

- `cmd/e2e-semstreams/mission/command.go:204,212`.
- `examples/processors/document/component.go:606,622`.
- `examples/processors/iot_sensor/component.go:547,563`.
- `examples/processors/weather_station/component.go:275,289`.
- `gateway/graph-gateway/component.go:402,419`.
- `gateway/http/http.go:399,404`.
- `gateway/lifecycle-gateway/component.go:320,333`.
- `input/file/file.go:263,279`.
- `input/http/http.go:150,162`.
- `input/udp/udp.go:365,382`.
- `input/websocket/websocket_input.go:401,406`.
- `output/file/file.go:601,615`.
- `output/httppost/httppost.go:553,567`.
- `output/otel/component.go:470,499`.
- `output/websocket/websocket.go:404,421`.
- `processor/agentic-governance/component.go:457,470`.
- `processor/agentic-model/component.go:928,950`.
- `processor/agentic-tools/component.go:895,925`.
- `processor/gated-dag/component.go:274,277`.
- `processor/graph-clustering/component.go:758,773`.
- `processor/graph-embedding/component.go:468,501`.
- `processor/graph-index-spatial/component.go:269,284`.
- `processor/graph-index-temporal/component.go:279,294`.
- `processor/graph-index/component.go:445,462`.
- `processor/graph-ingest/component.go:727,744`.
- `processor/graph-query/component.go:237,256`.
- `processor/json_filter/json_filter.go:647,667`.
- `processor/json_generic/json_generic.go:475,491`.
- `processor/json_map/json_map.go:650,670`.
- `processor/research-graph-assess/component.go:445,461`.
- `processor/research-graph-classify/component.go:428,444`.
- `processor/research-graph-execute/component.go:502,518`.
- `processor/research-graph-route/component.go:436,452`.
- `processor/research-graph-synthesize/component.go:435,451`.
- `storage/objectstore/component.go:974,1010`.

The renderer classification is structural: a method is “stored-slice” only when its body is one return statement whose
result is the receiver’s `inputPorts` or `outputPorts` field. Every other production method is recorded as hand-rolled;
the classification does not infer whether a short method’s result is semantically correct.

## 6. Consumer-at-birth inventory for exact KV reads

The accepted Foundation B claim is at `docs/proposals/post-r1c-foundation-remap-roadmap.md:311-320`. The proposed exact-read semantic boundary is current-value exact/list access without watch, replay, creation, handle injection, or retry policy at `:280-291`.

### 6.1 Graph-clustering: `ENTITY_STATES`

Present consumer: yes.

- Default declaration currently calls this `entity_watch` and types it `kv-watch`: `processor/graph-clustering/component.go:482-507`.
- Runtime opens a must-exist catalog reader: `processor/graph-clustering/component.go:1135-1143`.
- It lists all entity IDs with `Keys`: `:1835-1845`.
- It point-reads entities with `Get`: `:2392-2417`.
- No ENTITY_STATES watch was found in graph-clustering.

Consumer class: exact/list current state.

Current declaration truth: mismatched; it says watch.

### 6.2 Graph-clustering: `OUTGOING_INDEX`

Present consumer: yes.

- Must-exist catalog acquisition: `processor/graph-clustering/component.go:1145-1153`.
- Point read by entity ID: `:1980-2016`.

Consumer class: exact current state.

Current declaration truth: absent.

### 6.3 Graph-clustering: `INCOMING_INDEX`

Present consumer: yes.

- Must-exist catalog acquisition: `processor/graph-clustering/component.go:1155-1163`.
- Filtered key listing: `:2019-2055`.

Consumer class: list current state.

Current declaration truth: absent.

Graph-clustering’s separate `GRAPH_STATUS` readiness paths are actual watches through `graph/readiness`, beginning at `processor/graph-clustering/component.go:1335-1358`. They are not the same semantic class as the three exact/list readers.

### 6.4 Agentic-tools: `ENTITY_STATES`

Present consumer: yes.

- Graph-query tool registration is unconditional and exposes five tools.
- It lazily binds `graph.OpenCatalogReader` and never creates the bucket.
- It point-reads entries through `Get`.

Evidence: `processor/agentic-tools/executors/register_graph_query.go:14-45,48-105`.

Consumer class: exact current state.

Current default component declaration: absent at `processor/agentic-tools/config.go:135-181`.

Current shipped declaration: seven ignored top-level `kv_read` rows, all truthful to this consumer.

### 6.5 Agentic-tools: `AGENT_LOOPS`

Present consumers: yes.

- `read_loop_result` point-reads completion entries.
- `monitor_flow` lists all keys and point-reads `COMPLETE_*` entries at `processor/agentic-tools/flow_monitor_executor.go:234-270`.

Consumer class: exact/list current state.

Acquisition collision: `registerReadLoopResult` calls `CreateKeyValueBucket` with history and TTL, explicitly allowing the reader side to win creation at boot. Evidence: `processor/agentic-tools/executors/register_read_loop_result.go:16-70`.

Current component declaration: absent.

Current shipped top-level `kv_read` declaration: absent.

The runtime consumer is real, but its acquisition includes provisioning behavior that differs from the accepted exact-read metadata statement that `kv-read` “does not open a bucket, create a bucket, inject a handle, select retry, or imply watch/replay.”

### 6.6 Graph-query: shipped `ENTITY_STATES` rows

Present consumer: no.

- Default ports contain only four `nats-request` query inputs: `processor/graph-query/component.go:89-103`.
- Exact production search under `processor/graph-query` found no `OpenCatalogReader`, `ENTITY_STATES` acquisition, or ENTITY_STATES `Get`/`Keys`.
- Graph-query acquires `COMMUNITY_INDEX` and `COMMUNITY_SUMMARIES`, then starts watch/sync loops: `processor/graph-query/component.go:459-497,579-610,613-689`.

Those are watch/replay-style cache dependencies, not exact/list `kv-read`.

The two shipped graph-query `kv_read`/`ENTITY_STATES` rows are therefore ignored configuration with no matching runtime consumer.

Consumer-at-birth result: the roadmap phrase “graph-query declare their current exact reads” has no present exact-read resource identifiable in the merged runtime. The only graph-query KV dependencies found are community watches.

## 7. Flowgraph inventory

Flowgraph reads `InputPorts()` and `OutputPorts()` directly at `component/flowgraph/flowgraph.go:150-185`.

It independently derives three facts from type switches:

1. interface contract: `:188-214`;
2. interaction pattern: `:216-244`;
3. connection ID: `:246-320`.

Coverage differences:

- Interface extraction handles NATS, request, JetStream, KV watch/write, HTTP client, and timer.
- `StoreReadPort` carries an interface but interface extraction omits it.
- Pattern classification includes StoreRead and StoreProvide as store federation.
- Unknown runtime types default to stream pattern.
- Connection IDs have separate sentinel values such as `nats_missing_subject`, `jetstream_unknown`, `kv_missing_bucket`, and `unknown_type_%T`.

Connection behavior:

- stream, request, watch, and store each have separate connection logic;
- network conflict checking is separate;
- graph mutation validation is an additional request-port interpretation at `:354-389`;
- store federation connects every provider to every reader at `:597-630`;
- orphan handling separately exempts network, HTTP client, store, and timer classes at `:891-987`;
- JetStream stream requirements later re-assert concrete type assumptions.

Thus flowgraph has multiple in-file interpretations after the builder has already interpreted the definition.

## 8. Registry and ComponentManager inventory

### 8.1 Registry conflict ownership

Registry:

- calls both effective port methods;
- checks only `IsExclusive`;
- validates port number only for concrete value `NetworkPort`;
- tracks only exclusive resource IDs.

Evidence: `component/registry.go:574-628`.

Capability announcements expose name, subject, type, interface, and description at `component/registry.go:79-99`.

Capability projection recognizes only:

- `*NATSStreamPortConfig` → `stream`;
- `*NATSRequestPortConfig` → `request`.

All normal value-typed runtime ports become `type:"unknown"` with empty subject. Interface is never populated. Evidence: `component/registry.go:1008-1033`.

Neither dead pointer type has a production constructor.

### 8.2 ComponentManager conflict ownership

ComponentManager independently:

- calls both effective port methods;
- conflicts only on `IsExclusive`;
- tracks every resource ID, including nonexclusive resources, in its own map.

Evidence: `service/component_manager.go:1080-1147`.

This tracker is separate from Registry’s tracker and has different retention behavior.

Management port reporting independently recognizes only:

- `NATSPort`;
- `NATSRequestPort`.

Every other runtime port is omitted from management port output. Evidence: `service/component_manager.go:2291-2343`.

### 8.3 Store ownership

Store provider lifecycle is not enforced through `IsExclusive`.

- `StoreProvider` maps storage-instance names to live stores: `component/dependencies.go:29-40`.
- ComponentManager receives StoreRegistry through dependencies: `service/component_manager.go:2088-2103`.
- After start, duplicate store ownership is logged and skipped rather than replacing the incumbent or failing component start: `service/component_manager.go:2108-2159`.
- `StoreProvidePort` is deliberately nonexclusive because this is owned by ADR-063’s StoreRegistry lifecycle: `component/port_provide.go:5-35`.

## 9. Schema/config-loader inventory

- `ValidateConfig` explicitly permits unknown fields: `component/schema.go:26-31`.
- Schema tags know the broad property type `ports`, not individual port kinds: `component/schema_tags.go:300-312`.
- A `ports` property gets reflected `PortFields`: `component/schema_tags.go:413-416`.
- `GeneratePortFieldSchema` reflects the flat `PortDefinition` fields and editability only; it has no kind-specific matrix: `component/schema_tags.go:687-746`.
- `config.PortsConfig` and `config.PortDefinition` are aliases of the component types: `config/streams.go:411-428`.
- Raw component configuration extraction reads only `ports`: `config/streams.go:430-441`.
- Stream provisioning interprets only `ports.outputs` with exact `type == "jetstream"`: `config/stream_bounds.go:243-275`.
- Stream identity is independently derived from `stream_name` or flat `subject`: `config/stream_bounds.go:326-346`.

Thirty-one generated `schemas/*.json` files contain a `ports` property. Their shapes are not uniform; examples include `graph-query.v1.json` exposing ports as an object while `agentic-loop.v1.json` exposes ports as a string. No generated kind-specific binding matrix exists.

## 10. Raw component-config interpreter owners

There are two raw component-config owner families, plus the component-local definition interpreters recorded in the AST
census. The two raw owners consume `config.Component.Config` without first obtaining an effective runtime `[]Port`.

### 10.1 Message-logger subject discovery

Message-logger defaults `monitor_subjects` to `["*"]`, then at construction reads enabled raw component configurations from the manager and discovers subjects. Evidence: `service/message_logger.go:19-77`.

Its interpreter at `service/message_logger.go:289-340`:

- locally defines only `ports.inputs` and `ports.outputs`;
- unmarshals each row as `PortDefinition`;
- subscribes to every nonempty flat `Subject`, regardless of semantic kind;
- copies flat type and interface into metadata;
- silently skips component configs it cannot parse;
- ignores typed-only subject data;
- ignores top-level `kv_read`;
- ignores top-level `kv_write`;
- does not use the component runtime ports, flowgraph facts, registry facts, or management facts.

### 10.2 Stream declaration extraction and planning

The config package owns a separate raw-config interpretation path:

- `config/streams.go:411-428` aliases the component package’s `PortConfig` and `PortDefinition` types.
- `config/streams.go:430-441` unmarshals each raw component config’s `ports` object.
- `config/stream_bounds.go:243-275` walks enabled components but examines only `ports.outputs` whose flat type is exactly
  `jetstream`.
- `config/stream_bounds.go:326-346` derives one stream declaration from flat `stream_name` or flat `subject`.

The two raw owner families interpret different projections:

| Owner | Accepted rows | Extracted fact | Ignored data |
|---|---|---|---|
| Message-logger | Inputs and outputs with any type and nonempty flat `subject` | NATS subscription subject plus flat type/interface metadata | Typed-only subjects, `kv_read`, `kv_write`, semantic class |
| Stream planner | Enabled-component outputs with exact flat `type == "jetstream"` | Ordinary stream name and subjects from flat `stream_name`/`subject` | Inputs, non-JetStream kinds, runtime `Port`, top-level side lanes |

Neither path consumes the component’s effective runtime ports. Message-logger can subscribe to a flat subject for a
non-NATS semantic kind, while stream planning ignores the same row unless its type is exactly `jetstream`.

## 11. Same-class collision inventory

| Same-class concepts | Current owners | Collision observed |
|---|---|---|
| Definition vs runtime port JSON | `component/ports.go`; `component/port.go` | Different discriminators and envelopes; supported-kind sets differ |
| Flat definition fields vs typed `Config any` | `PortDefinition`, decoder, builder | Per-kind precedence differs; several typed fields are discarded |
| String `Type` vs concrete `Portable.Type()` | definition literals/config vs concrete types | Canonical dashed KV spellings become concatenated runtime spellings |
| Definition decoder vs builder | both in `component/ports.go` | Decoder-recognized `network` and `file` become NATS; store types are asymmetrical |
| Runtime decoder vs concrete types | `component/port.go` vs `port_store.go`/`port_provide.go` | Store ports serialize but cannot runtime-round-trip |
| Unknown definition behavior vs unknown runtime behavior | two decoders | Definition preserves generic map; runtime rejects; builder then defaults definitions to NATS |
| NATS real types vs dead capability types | `NATSPort`/`NATSRequestPort` vs `NATS*PortConfig` | Capabilities understand only dead pointer types |
| `kv-watch` vs exact/list access | graph-clustering declaration vs runtime | ENTITY_STATES is declared watch but only Keys/Get are used |
| `kv-read` rows vs `PortConfig` | shipped configs vs type | Nine rows are silently ignored |
| Ordinary KV outputs vs `PortConfig.KVWrite` | inputs/outputs plus special lane | Same write semantic class has two containers; special lane has no runtime reader |
| `kv`, `kv-write`, `kvwrite` | configs/decoders/builder/concrete Type | Three accepted definition spellings, one concatenated runtime spelling |
| `kv-watch`, `kvwatch` | configs/decoders/concrete Type | Two accepted definition spellings, concatenated runtime spelling |
| `http`, `grpc`, `websocket-server`, `network` | decoder/builder | Protocol aliases act as kinds; explicit network follows a different builder path |
| Store identity vs exact KV identity | ADR-063 types/flowgraph/StoreRegistry | Store-read is advisory federation; store-provide collision is runtime registry ownership, not exact resource matching |
| Registry resource tracker vs ComponentManager resource tracker | two independent owners | Registry tracks exclusive only; manager records all and checks exclusive |
| Registry capability view vs flowgraph view vs management view | three interpreters | Same runtime port can be classified correctly, reported unknown, or omitted |
| Static flowgraph resource facts vs live StoreRegistry | flowgraph vs ComponentManager | Store edges are advisory all-to-all; live provider ownership is dynamic |
| JetStream port resolution vs stream provisioning | builder vs `config/stream_bounds.go` | Both independently derive stream/subjects; builder drops some stream fields |
| Message-logger raw subject discovery vs stream-planner raw declarations | `service/message_logger.go` vs `config/streams.go` and `config/stream_bounds.go` | The former accepts any input/output flat subject as NATS; the latter accepts only exact JetStream outputs and derives streams |
| Both raw-config owner families vs runtime views | service/config vs registry, flowgraph, and ComponentManager | Neither begins from effective runtime ports, so each can disagree independently with runtime projections |
| `nats-request` kind vs frozen `RequestReply bool` proposal | current specs/ADR-091 vs suspended discovery change | Two possible representations of request/reply semantics occupy the same class |
| Exact-read metadata vs AGENT_LOOPS acquisition | roadmap semantic statement vs agentic-tools registration | Consumer reads exact/list values but also creates the bucket with policy |
| Graph-query `kv_read` config vs graph-query runtime | shipped configs vs component | Declared ENTITY_STATES dependency has no runtime consumer |
| Graph-query community KV access vs exact-read claim | graph-query runtime vs roadmap wording | Runtime dependencies are watches, not exact/list reads |

## 12. Adjacent ownership and collision territory

- `component-runtime-config` already binds request-port identity through decode, `BuildPortFromDefinition`, runtime config, and flowgraph, including typed-interface precedence: `openspec/specs/component-runtime-config/spec.md:213-227`.
- `framework-composition` already binds typed request ports and exactly one graph-mutation provider: `openspec/specs/framework-composition/spec.md:231-244`.
- `graph-ingest` already requires one declared typed mutation input and prohibits hidden fallback subjects: `openspec/specs/graph-ingest/spec.md:681-693`.
- `stream-provisioning` owns ordinary JetStream provisioning and interprets component output port declarations; it explicitly excludes KV/ObjectStore backing streams and independently enforces stream bounds.
- ADR-063 owns store-read/provide federation, StoreRegistry population, duplicate ownership, and lifecycle.
- ADR-083 and ADR-088 own `GRAPH_STATUS` readiness watching; this is separate from exact KV read metadata.
- ADR-090 owns raw KV access boundaries and catalog/operator acquisition.
- ADR-091 owns typed graph-mutation request-port identity.
- `openspec/changes/discovery-under-stream-shapes` is suspended/frozen. Its draft `RequestReply bool` field on `PortDefinition` collides with the existing canonical `nats-request` semantic class. Its work is held pending port-handling issues and has no merged implementation.
- `establish-graph-read-write-foundation` remains active and contains the already-current typed request-port preservation contract.
- `semantic-tier-split` is active but does not own the port grammar.

Issue territory recorded by the accepted roadmap:

- #859 spans Foundations B and C. The accepted roadmap names message-logger as the last raw-config interpreter, but the
  merged inventory also finds the config stream extraction/planning family. Its close premise is therefore not proven by
  deleting only message-logger’s parser.
- #862 is Foundation C’s `Discoverable` cutover territory.
- #620’s `kv_read`/`KVWrite` claims are in Foundation B/C territory, with remaining claims reserved for re-inventory.
- #810 is held for the effective-snapshot re-evaluation.
- #842 `tool.list` subject movement is held with #810.
- #795, #820, and #868 remain readiness follow-ups.
- #717’s component-status plane is closed by Foundation A and is not present.
- Query DTOs, GraphQL, MCP, gateway behavior, mutation semantics, recovery, backup, and downstream migration are explicitly excluded by the accepted roadmap.

## 13. Adopter seam inventory

Specific adopter: a developer outside this repository implementing a SemStreams component without opening the framework’s port implementation files.

| Outward seam | What they must know now | If they do nothing | Where current truth is discoverable | What they should ideally have to know |
|---|---|---|---|---|
| `Discoverable` | They must implement both `InputPorts()` and `OutputPorts()` and construct resolved `Port` values | Their component does not compile | `component/discovery.go:17-35` | Their component’s semantic dependencies and directions |
| `Portable` | They must select a concrete config, understand `ResourceID`, exclusivity, and exact `Type()` spelling | Their custom type may compile but be unknown/defaulted/omitted by framework consumers | Concrete `component/port_*.go` files plus every consumer type switch | The semantic resource, not every framework interpreter |
| `PortDefinition` | They must understand flat fields, typed `Config any`, per-kind precedence, aliases, and direction supplied elsewhere | A declaration can boot as the wrong semantic class or lose fields | `component/ports.go:35-371` | One class and its own resource data |
| Exported builder | They must know which decoder-supported kinds the builder actually handles and that it never returns an error | Unknown, `network`, `file`, and `store-provide` definitions can become NATS | `component/ports.go:192-371` | They should not have to predict silent fallback behavior |
| Runtime JSON | They must use `config.type` plus `config.data`, with a kind set different from definition JSON | Store ports and aliases may fail round-trip | `component/port.go:42-179` | One documented wire envelope |
| Definition JSON | They must use top-level `type`, flat fields, and direct typed `config` | Unknown kinds decode but later become NATS; top-level `kv_read` disappears | `component/ports.go:35-152`; shipped configs | One documented semantic declaration |
| KV read dependency | They must currently choose between a false `kv-watch`, an ignored `kv_read`, or no declaration | Framework inspection cannot truthfully report the exact-read dependency | Graph-clustering, agentic-tools, and config evidence above | Bucket identity and exact/list semantic class only |
| KV write dependency | They must know ordinary outputs work while special `kv_write` rows do not reach runtime ports | Their write dependency may be invisible to flow/reporting | `PortConfig`, component constructors, shipped configs | One ordinary direction-bearing declaration |
| Capability inspection | They must know capability announcements do not understand ordinary port values | Their component publishes `unknown`/empty port capabilities | `component/registry.go:1008-1033` | No extra knowledge beyond their declaration |
| ComponentManager inspection | They must know only NATS and NATS request ports appear | Other dependencies disappear from management output | `service/component_manager.go:2291-2343` | No knowledge of management-specific type switches |
| Message-logger auto mode | They must keep a nonempty flat `subject` even for kinds whose semantic resource is elsewhere | Traffic may be missed, or non-NATS resource strings may become subscriptions | `service/message_logger.go:289-340` | No message-logger-specific declaration convention |
| Stream planning | They must put an exact `jetstream` spelling on an output and expose flat `stream_name`/`subject` fields | The ordinary stream declaration is omitted or independently derived from a different fact than runtime ports | `config/streams.go:430-441`; `config/stream_bounds.go:243-275,326-346` | The semantic stream declaration, without a second raw-config projection rule |
| Store provider | They must implement `StoreProvider`, emit `StoreProvidePort`, and understand duplicate ownership is handled after start | Provider may be invisible or duplicate registration may be logged/skipped | ADR-063; `component/dependencies.go`; `component/port_provide.go`; manager lifecycle code | Storage instance identity and ownership only |

The present exported API makes the adopter predict facts owned by framework internals: accepted spellings, builder fallbacks, field precedence, which observer has which type switch, and which configuration lane is live. Failures are split among compile errors, boot-time decode errors, silent NATS fallback, dropped fields, ignored rows, unknown capabilities, and omitted management output.

## 14. Exact searches and empty categories

Commands/results recorded against HEAD:

```text
git status --short --branch
## codex/foundation-b-port-grammar
?? docs/proposals/foundation-b-port-language-inventory.md

git rev-parse HEAD
61022ae1b4da0309e93ce49ec00c9c64679d09d8
```

Foundation A port-surface diff search:

```text
git diff --name-only c38e3e82..HEAD |
  rg '^(component/(port|ports|registry|flowgraph)|config/|schemas/|service/message_logger)'
```

Only the two status-retirement message-logger KV support files matched; no port grammar/config/schema files matched.

Production retirement search:

```text
rg -n -S \
  'BucketComponentStatus|COMPONENT_STATUS|LifecycleReporter|ReportStage|ReportCycle' \
  --glob '!docs/adr/**' \
  --glob '!docs/proposals/**' \
  --glob '!openspec/changes/archive/**' \
  --glob '!test/contract/component_status_retirement_contract_test.go' .
```

Result: empty.

New-symbol/category search:

```text
rg -n '\bPortKind\b|type KVRead|KVReadPort' \
  component config service processor input output storage gateway \
  --glob '*.go' --glob '!**/*_test.go'
```

Result: empty.

Production `PortConfig.KVWrite` consumer search:

```text
rg -n '\.Ports\.KVWrite|Ports\.KVWrite|\.KVWrite\b' \
  --glob '*.go' --glob '!**/*_test.go' .
```

Result: empty.

Production custom extension search:

```text
rg -n 'custom.*port|port.*custom|Register.*Port|Port.*Register' \
  component config service --glob '*.go' --glob '!**/*_test.go'
```

Result: only comments describing permissive unknown `PortDefinition` decoding; no registry, registration API, or production custom kind.

Runtime exact-read search under graph-query:

```text
rg -n 'ENTITY_STATES|OpenCatalogReader|GetKeyValueBucket|\.Watch\(|\.Get\(|\.Keys\(' \
  processor/graph-query --glob '*.go' --glob '!**/*_test.go'
```

Result: only `COMMUNITY_INDEX` and `COMMUNITY_SUMMARIES` bucket acquisition/watch paths; no ENTITY_STATES reader.

Raw component-config owner-family search:

```text
rg -n 'discoverSubjectsFromComponents|extractPortsFromConfig|resolveStreamDeclarations|derivePortStream' \
  service/message_logger.go config/streams.go config/stream_bounds.go
```

Result: message-logger discovery at `service/message_logger.go:64,289`; config port extraction at
`config/streams.go:434`; and stream resolution/derivation at `config/stream_bounds.go:215,252,264,326,337,362`.

Syntax-aware census command and category results:

```text
go run /private/tmp/foundation_b_port_ast.go

PortDefinition composite literals: 125 AST nodes
Executable declarations after excluding component/schema_tags.go:705: 124
PortDefinition.Type selector occurrences: 93 at 78 unique file:line sites
Portable assertions/type switches: 16
InputPorts/OutputPorts methods: 76 production methods
Hand-rolled methods: 70
Stored-slice methods: 6
```

The analyzer used resolved Go types rather than identifier text: `component.PortDefinition` composite literals and
selectors were matched by package path plus named type; assertions were included only when the asserted expression’s
static type resolved to `component.Portable`; renderer methods were selected from method declarations, not grep hits.
`component/test_helpers.go` and test variants were excluded from production method counts.

Schema search:

```text
rg -l '"ports"' schemas --glob '*.json' | wc -l
31
```

Shipped lane census:

```text
total rows: 522
inputs: 218
outputs: 279
kv_read: 9
kv_write: 16
```

## 15. Inventory stop risks requiring owner interpretation

These are evidence conflicts, not rulings:

1. Graph-query has no current exact-read consumer matching either the accepted wording or its two shipped `ENTITY_STATES` `kv_read` rows.
2. Agentic-tools’ AGENT_LOOPS exact/list consumer also creates the bucket with retention policy, while the accepted KVRead semantic statement excludes creation and runtime policy.
3. StoreProvide is described as a present supported class but cannot pass either current JSON decoder/builder route.
4. StoreRead has a builder case but cannot typed-definition decode or runtime-Port round-trip.
5. Explicit `network` and `file` definitions are typed by the decoder but become NATS through the exported builder.
6. JetStream’s current typed surface contains fields that the builder drops, including `MaxAckPending`, stream policy fields, subjects, and interface.
7. `PortConfig.KVWrite` has 16 shipped rows and a code default but zero runtime consumers.
8. The nine shipped `kv_read` rows divide into seven truthful-but-ignored agentic-tools rows and two ignored graph-query rows with no consumer.
9. The suspended `RequestReply bool` artifact occupies the same semantic territory as the current `nats-request` kind.
10. Registry, flowgraph, ComponentManager, schema generation, message-logger, and config stream planning currently
    disagree on the same declaration population.
11. The interpreter population is larger than the central consumers alone: 93 typed `PortDefinition.Type` reads at 78
    file:line sites, 16 runtime `Portable` assertions, and 70 hand-rolled effective-port renderers.
12. The accepted “message-logger is the sole remaining raw interpreter” premise is false in the merged tree because
    config stream extraction/planning is a second raw component-config owner family.
13. No present custom-kind extension mechanism or two current custom-kind consumers were found.
14. Foundation A changed none of these port premises; the conflicts exist above tracked baseline
    `61022ae1b4da0309e93ce49ec00c9c64679d09d8` with only this inventory artifact untracked.

Binding interpretation of these stop risks remains with the owner.
