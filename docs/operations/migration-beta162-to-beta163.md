# Migration notes — v1.0.0-beta.162 → v1.0.0-beta.163

SemStreams-owned record of what each downstream product must do to adopt the beta.163 wave. Sister repositories are
**read-only** to SemStreams agents (owner ruling on #1100, 2026-08-26, O-11/O-12): no issues, comments, or edits are made
there — every obligation is recorded here, at the sister's pinned SHA, and linked from the landing PR. One `##` section per
landing; later wave items (#1095 re-slot and reorder, gh606) append their own sections below.

Every `file:line` below was read at the named SHA on 2026-08-26 and re-read at SemStreams `origin/main` `7e7ea76e`.

## Single type authority (ADR-103)

### What changes on the wire

After this landing, graph-ingest rejects any `entity.create` whose `entity.message_type` is not registered in the receiving
binary's payload registry, with the closed outcome code `message_type_unregistered` (class invalid; detail `message_type` = the
key). Reads, `entity.reconcile`, `triple.append`, `entity.delete`, and entities already stored are unaffected; only new births
of an unknown type are refused. `projection.Contract` literals keep compiling (the contract types move to
`pkg/projection/contract` with aliases). A product that creates through `pkg/projection.MutationClient.Create` may omit
`entity.MessageType`: the client fills it from the bound contract (owner ruling O-17); a non-empty stamp that conflicts with
the contract is rejected. `projection.Contract.MessageType` is now the structured `message.Type` — on the wire
`{"domain":…,"category":…,"version":…}`, the same shape `EntityState.message_type` has — so a dotted-string literal
(`MessageType: "semmachina.campaign_entity.v1"` or `agentic.LoopExecutionMessageType().Key()`) no longer compiles and a
rule pack's `"message_type": "…"` no longer loads: write `MessageType: YourMessageType()` (the `message.Type` builder every
product already has) and `"message_type": {"domain": …, "category": …, "version": …}`. `Register` refuses a domain,
category, or version containing `.`. Registered framework entity types now validate their writer's full contract at
publication (`Validate()` gates `BaseMessage.MarshalJSON`). Two boot-time consequences for a composition root that builds its own graph-ingest: the
graph-ingest factory refuses to construct without a payload registry (`Dependencies.PayloadRegistry`), and with
`enable_hierarchy: true` it refuses to construct unless that registry holds `graph.hierarchy_container.v1` — both are
registered by `payloadbuiltins.Register`, so a root that calls it (semmachina, semdev do) sees no change. Full mechanics:
`openspec/specs/payload-registry/spec.md`, `openspec/specs/graph-ingest/spec.md`.

Two exported constants are removed: `agentic.LessonPolarityAvoid` and `agentic.LessonPolarityBestPractice` (the lesson
polarity vocabulary is now unexported, matching the sibling severity and status vocabularies; a producer writes the
literal `"avoid"` / `"best_practice"`, and `Validate`'s error names both values). No adopter impact, verified: a grep
across all nine sister repositories found zero references.

### The one obligation

Register every `message.Type` you stamp on `entity.create`, in the binary that hosts graph-ingest, with its ADR-054 floor and —
where you hold a birth contract — that contract. The pattern is the framework's own `storage/objectstore/stored_message.go:88-103`:

```go
// RegisterPayloads registers this product's graph-born types with the supplied
// registry. Called from the composition root after payloadbuiltins.Register.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Factory:         func() any { return &CampaignEntity{} }, // a Graphable payload: EntityID(), Triples(), Schema(), Validate(), MarshalJSON
		Domain:          Domain,                                  // e.g. "semmachina"
		Category:        CategoryCampaignEntity,                  // e.g. "campaign_entity"
		Version:         SchemaVersion,                           // e.g. "v1"
		Description:     "Campaign entity born by the campaign gate",
		IndexingProfile: vocabulary.IndexingProfileControl,       // ADR-054 floor; "" = control + metered gap
		Contracts:       []contract.Contract{CampaignBirthContract()}, // the birth contract you hold today, if any
	})
}
```

Verification, in your own tree: a production-decoder round-trip test per type (marshal a fully populated entity, decode
through `message.NewDecoder(reg)`, assert the concrete type, `EntityID()`, and the predicate set of `Triples()`), plus one
`entity.create` against a beta.163 graph-ingest that returns `applied`.

### semmachina — pinned `841c45e`

- **Types stamped on `entity.create` (4):** `semmachina.campaign_entity.v1` (`internal/campaign/gate.go:32-36`, stamped `:395`),
  `semmachina.turn_state.v1` (`internal/turn/recorder.go:32-36`, stamped `:344`), `semmachina.knowledge_grant_entity.v1` and
  `semmachina.revelation_receipt_entity.v1` (`internal/projectioncontract/contracts.go:106-109`; stamped through
  `internal/knowledge/granter.go:282-284`). None is registered: `internal/payload/constants.go:63-147` records each as
  "deliberately NOT registered: no message of this type is ever published"; `internal/payload/registry.go:27-72` registers ten
  other categories. `internal/boot/components.go:51` also births lifecycle participants (`lifecycle.harness.v1`) — covered by
  `payloadbuiltins.Register`, which `cmd/semmachina/main.go:99` already calls.
- **Day one after upgrade:** every campaign, turn, knowledge-grant, and revelation birth is rejected `message_type_unregistered`;
  existing entities keep reading; lifecycle births keep working.
- **Obligation:** add the four to `payload.RegisterPayloads` (`internal/payload/registry.go`) with factories (Graphable payloads
  carrying the fields your triple builders read); floors: `campaign_entity` and `turn_state` `control` (machinery),
  `knowledge_grant_entity` and `revelation_receipt_entity` `content` if their text is retrieval-worthy, else `control`;
  contracts: the seven birth contracts in `internal/projectioncontract/contracts.go:77-109` bound to their types (a contract's
  `MessageType` may be left zero — `Register` fills it; where set, it is now `message.Type{…}`, not the dotted string at
  `contracts.go:106-109`). Invert the "deliberately unregistered" tests
  (`submitaction_test.go:364` and any sibling) — the rationale they pin is the one ADR-103 retires.
- **Verification:** four round-trip tests; `TestCategoryCampaignEntity_*` and friends assert registration instead of its absence.

### semdev — pinned `ca3956a`

- **Types stamped (2):** `semdev.intake_event.v1` (`internal/intake/record.go:63`, stamped through `graphown.Creator.Create` at
  `:91`), `semdev.standards_source.v1` (`internal/standards/sync.go:143`, stamped `:186`). Lessons use the framework's
  `agentic.agent_lesson.v1` (`internal/graphown/contracts.go:444`) — registered by the framework. The registry is built at
  `internal/boot/runtime.go:623-624` from `payloadbuiltins.Register` only. `runtime.go:537,656` and `boot.go:361` birth lifecycle
  participants — covered.
- **Day one:** intake records and standards sources are rejected at birth; lessons and lifecycle births keep working.
- **Obligation:** a `RegisterPayloads` in `internal/intake` and `internal/standards` (or one in `internal/graphown`) called from
  `runtime.go:624` after the builtins; floors: `intake_event` `control`, `standards_source` `content`; contracts: the graphown
  contracts for those two (`internal/graphown/contracts.go`) bound to the types. The structured contract type also breaks
  compilation where a contract literal sets a dotted string: `internal/graphown/contracts.go:444` and
  `test/conformance/standards_contracts_test.go:102-103` — use the `message.Type` builder instead of `.Key()`.
- **Verification:** two round-trip tests; `graphown.Creator.Create` against beta.163 returns `applied`.

### semconnect — pinned `d0d06e0`

- **Types stamped (11):** `c360.csapi-{system,datastream,procedure,deployment,sampling-feature,property,control-stream,command,system-event,feasibility,schema-artifact}.v1`
  (`gateway/cs-api/projection_contracts.go:29-39` — those vars are already structured `message.Type`s and are fine; the
  compile breaks are the contract literals that flatten them, `projection_contracts.go:45,60` and
  `projection_contracts_test.go:26` — carry the `message.Type` value instead of `.Key()`), stamped at
  `gateway/cs-api/graph_mutations.go:159`. semconnect holds **no
  registry**: `cmd/cs-api-server` calls neither `payloadbuiltins.Register` nor `payloadregistry.New`; its one registration
  (`message/oms/register.go:16-22`, OMS observation) is exported for a host. The 11 stamps reach the **host's** graph-ingest.
- **Day one:** every CS-API resource birth is rejected by the host's graph-ingest.
- **Obligation:** export `RegisterPayloads` from `gateway/cs-api` for the 11 types (floor `content` — the value its contracts
  already declare at `projection_contracts.go:44-64`; contracts: `representationContract`/`birthOnlyContract` bound to each
  type), and have the **host composition root** call it after `payloadbuiltins.Register`. `message/oms.RegisterPayloads` is the
  in-tree model for the shape.
- **Verification:** round-trip tests in `gateway/cs-api`; one host-side boot that registers both and creates a system resource.
- **Ruled (#1114, 2026-08-27, option (a)):** semconnect builds its own composition root — the pattern semmachina
  (`cmd/semmachina/main.go:98-99`), semdev (`internal/boot/runtime.go:623-624`), semteams (`cmd/semteams/main.go:872-873`)
  and semsource (`cmd/semsource/run.go:279-280`) already follow: a `cmd/<binary>/main.go` that builds
  `payloadregistry.New()`, calls `payloadbuiltins.Register(reg)`, then `csapi.RegisterPayloads(reg)` (the export above),
  injects `reg` through `component.Dependencies.PayloadRegistry`, and hosts graph-ingest itself. The framework binary gains
  no registration seam; the unmodified `cmd/semstreams` image in `semconnect/deploy/compose.yml:19-49` and
  `conformance/compose.yml:55-73` cannot host CS-API births after this tag. Sisters stay read-only: this is the
  instruction, not a change made for semconnect.

### semteams — pinned `8a70b7e7`

- **Types stamped:** none of its own on `entity.create`. Its contracts re-declare the framework's loop-execution and lesson
  contract structure with the framework's key builders (`cmd/semteams/main.go:971,998`); lifecycle births through `Manager`
  (`cmd/semteams/flowtemplates/loader.go:200`) carry `lifecycle.harness.v1` — registered by `payloadbuiltins.Register`. Own
  registered types: `research/artifact.go:253`, `devviaspec/plan.go:148`, `semsource/payload.go:39-69`.
- **Day one:** nothing is rejected.
- **Obligation:** the two re-declared contracts set `MessageType` from `.Key()` (`cmd/semteams/main.go:971,998`), which no
  longer compiles against the structured field — replace them with `agentic.LoopExecutionContract()` and
  `agentic.LessonContract()` (the contracts now live beside their types; `openspec/specs/agentic-lessons/spec.md` "External
  lesson composition uses the framework-owned contract snapshot").
- **Verification:** boot; the loop-execution and lesson contracts validate unchanged.

### semmem — pinned `b909cbf` — for downstream-owner validation

The local tree is pre-rename and stale: `go.mod:1` declares module `github.com/c360/semmem` and `go.mod:6,10` replace
`github.com/c360/semstreams` (the old import path) with `../semstreams`; it does not build against
`github.com/c360studio/semstreams`. Its `EntityState` shape (`entity/types.go:180-192`: `Edges`, `ObjectRef`, `MessageType string`)
predates ADR-091. It stamps nine `semmem.entity.{spec,doc,issue,discussion,discussion_comment,pr,pr_review,code,decision}.v1`
strings (`entity/types.go:187-365`, `processor/{spec,docs,decision}/processor.go:240,247,332,357`) and registers none.

- **The finding that opened #1100** (a lesson had no payload type at all) is closed by this landing: `agentic.agent_lesson.v1` is
  a registered Graphable payload with a factory, so a lesson can arrive on a fact-lane or import-lane input as itself.
- **Obligation when semmem rejoins the current module path:** register its nine types with floors (`content` for prose-bearing
  spec/doc/discussion/decision, `control` for the rest unless retrieval-worthy) and any birth contracts it adopts. The federation
  MVP document this issue cites is not in any local tree; the downstream owner validates this section.

### Not affected

- **semsource** (`4093d3c`): births on the fact lane as the registered `semsource.entity.v1` (`graph/event_payload.go:22-31`);
  mutation-lane use is `Reconcile` only (`processor/supersession/lifecycle.go:303,325`) — no registration obligation.
  ONE compile fix from the structured contract type: `graph/contract.go:71` sets `MessageType: EntityType.Key()` in a
  `projection.Contract` literal; write `MessageType: EntityType` (it is already a `message.Type`).
- **semdragon** (`07f4de9`, pinned to beta.135): writes through the pre-ADR-091 subject and directly to KV
  (`questdag/unit.go:673`, `graphclient.go:79-95`); already off the current mutation surface. `questdag.unit.v1` and the dynamic
  `semdragons.<event>.v1` keys become obligations only if it returns to the typed mutation API.

## Composition validation substrate (ADR-100) — `GET <components>/gaps` is removed

### What changes on the wire

`GET <components>/gaps` — the operation key `/gaps` in the generated document, served at `/components/gaps` because
ComponentManager mounts under the `components` prefix (`service/service_manager.go:1683-1686` maps the service name
`component-manager` to the URL prefix `components`) — is **removed without an alias**,
together with its response body (`disconnected_nodes`, `orphaned_ports`, `objectstore_gaps`, and the `summary` object
carrying `total_gaps` / `critical_gaps` / `optional_gaps` / `critical_port_count` / `has_issues`). The route 404s and the
operation is gone from `specs/openapi.v3.yaml`. Its Go surface goes with it:
`ComponentManager.ValidateFlowConnectivity`, `ComponentManager.DetectObjectStoreGaps`, and the `ComponentGap` type.

The operation was a **second composition judgment**. It re-derived the connection graph on demand and applied its own
severity table, which disagreed with the canonical one: an input declared `"external": true` — fed from outside the
composition, an operator statement introduced in this same landing — is not an orphan to the canonical validator, while
`/gaps` reported it as `no_publishers`, `required=true`, `critical_port_count=1`, `has_issues=true`. ADR-100 D3 admits one
validator and one findings vocabulary, so the second judgment is retired rather than re-projected.

### What replaces it

| You wanted | Use |
|---|---|
| The findings for the composition that is running | `GET <components>/validate` — the `composition.Result` ComponentManager retained at boot, verbatim (`status`, `errors`, `warnings`, `graph`). Boot now **refuses** on an error finding, so a running process has none. |
| The connection graph of the composition that is running | `GET <components>/flowgraph` (JSON, or `?format=mermaid`) |
| The findings for a composition before you boot it | The `validate <config-path>` verb (`composition/cli.Main`, wired into `semstreams`; `--validate` reports the same findings), or `composition.Validate(catalog, cfg)` in Go |
| The same judgment in a test | `composition.AssertValid(t, catalog, cfg)` |
| The same judgment from an agent | The read-only tools `validate_composition` and `composition_graph`, under the existing `component_catalog` gate |

The finding types `disconnected_node` and `orphaned_port` survive by name in the canonical vocabulary; a required stream
input with no publisher is an `orphaned_port` **error** there (severity, not a separate "critical" count), and an input
declared `"external": true` raises no such finding. `GET <components>/paths` (reachability from input components) is
unchanged **by this landing** — it is a projection, not a judgment. The flow-authoring retirement below does change it:
same response shape, but it is now derived from the retained composition result and answers 503 rather than 500 before
that result exists. See "`/paths` now serves the retained graph".

### Downstream action

Measured across the local sister checkouts on 2026-08-26 (`grep -rn "gaps"` over `*.ts`, `*.tsx`, `*.svelte`, `*.go`,
`*.py`, excluding generated files): **no repository calls the operation from hand-written code.** Every hit is a
generated OpenAPI type — `semstreams-ui/src/lib/types/api.generated.ts:703`, `semteams/ui/src/lib/types/api.generated.ts:715`,
`semspec/ui/src/lib/types/{api,semstreams}.generated.ts:600,715`, `semdragon/ui/src/lib/api/generated.d.ts:91` and
`semdragon/ui/static/openapi.json:58`.

- **Every UI repository**: regenerate types from the new `specs/openapi.v3.yaml`. The `/gaps`
  operation key disappears; nothing else in the component surface changes shape except `/flowgraph`
  and `/validate`, which gain typed schemas (`Graph`, `Result`).
- **Anyone who added a call since that measurement**: replace a `has_issues` check with `result.status !== "valid"` from
  `<components>/validate`, and a `critical_port_count` check with `result.errors.length`. Do not reimplement the severity
  rule client-side — that is the defect this landing removes.
- **Anyone whose component has an input fed from outside the composition** (a UI, a peer process, a rule action): declare
  `"external": true` on that input port, and restate it in any named override (a named merge is a complete replacement).
  Without it, boot refuses with `orphaned_port … no_publishers` and prints the one-line remedy.

## Flow-authoring retirement (ADR-100 D5) — the saved-diagram surface is removed

This is the second half of ADR-100, landed by #1093. The first half (above) built the one validator; this one removes
the diagram-authoring surface that stood beside it. **Nothing here has an alias, a compatibility route, or a legacy
reader** — pre-v1 fresh-state policy (ADR-100 D5).

### What changes on the wire

Every route below was served under the `flowbuilder` prefix — the service name `flow-builder` with its hyphens removed
by `Manager.serviceNameToPrefix` (`service/service_manager.go:1682-1697`, default branch) — and appears in the generated
document under its unprefixed key. All of them 404 now: the service that owned them is no longer registered, so nothing
constructs a handler for them.

| Removed route | What it did | Replacement | Downstream action |
|---|---|---|---|
| `GET /flowbuilder/flows` | List saved diagrams | `GET <components>/flowgraph` — the projection of what is running | Delete the call; read the projection, or read your own configuration file |
| `POST /flowbuilder/flows` | Save a diagram | none — a composition is the product's configuration | Author the config file; there is no framework authoring store |
| `GET /flowbuilder/flows/{id}` | Read a saved diagram | `GET <components>/flowgraph` | Delete the call |
| `PUT /flowbuilder/flows/{id}` | Update a saved diagram | none | Edit the config file and restart |
| `DELETE /flowbuilder/flows/{id}` | Delete a saved diagram | none | Delete the call; the `semstreams_flows` bucket may be deleted |
| `POST /flowbuilder/flows/{id}/validate` | Validate a saved diagram or a request-body draft | `GET <components>/validate` (running), the `validate <config>` verb / `composition.Validate` (offline) | Replace with one of the two. **This route applied its own severity table with no `External` check** — the second-judgment defect the `/gaps` section above describes; do not reimplement it client-side |
| `POST /flowbuilder/flows/{id}/publish-component-configs` | Compile a diagram into `components.*` desired state | none — the framework exposes no next-boot component-configuration write verb (ADR-100 D4) | Edit the product's configuration and restart |
| `GET /flowbuilder/flows/{id}/observations/health` | Component health keyed by diagram names | `GET <components>/health`, `GET <components>/status/{name}` | Re-key by component name instead of flow id |
| `GET /flowbuilder/flows/{id}/observations/metrics` | Component metrics keyed by diagram names | `/metrics` (Prometheus) | Re-key by component name |
| `GET /flowbuilder/flows/{id}/observations/messages` | Message observations filtered by diagram names | the message-logger service's own routes | Re-key by component name |

Generated schemas removed with them: `Flow`, `FlowCreateRequest`, `FlowUpdateRequest`, `FlowListResponse`,
`RuntimeHealthResponse`, `RuntimeMetricsResponse`, `RuntimeMessagesResponse`, `publishComponentConfigsResponse`.
The `FlowGraph` **tag** stays — it labels the retained `<components>/flowgraph`, `<components>/paths`, and
`<components>/validate` operations, which are unchanged in shape.

### Agent tools removed

Eleven tools leave the built-in registry, and the two `SkipBuiltins` group keys that skipped them (`flows`,
`flow_templates`) leave `BuiltinGroupKeys`:

`create_flow`, `update_flow`, `delete_flow`, `list_flows`, `get_flow`, `create_flow_template`, `update_flow_template`,
`delete_flow_template`, `list_flow_templates`, `get_flow_template`, `instantiate_flow_template`.

An agent that needs to know about a composition uses the read-only trio instead: `list_components` (catalog),
`validate_composition`, and `composition_graph`. A role config that still names a removed tool in `allowed_tools`
keeps booting — an allowlist entry for a tool nobody registers is inert — but the agent will never see the tool.
The shipped `configs/flows/ops-agent*.json` were migrated this way in this landing.

### Go packages, services, and symbols removed

| Removed | Notes |
|---|---|
| package `flowstore` | the `semstreams_flows` KV bucket and `Flow` / `FlowConnection` types |
| package `flowtemplate` | the `FLOW_TEMPLATES` KV bucket and the template renderer |
| package `engine` | diagram validation and compilation; it constructed every node through the Registry with a live NATS client to read ports |
| service `flow-builder` (`service.NewFlowServiceFromConfig`, `service.FlowService`) | with `FlowListResponse`, `FlowCreateRequest`, `FlowUpdateRequest`, `RuntimeHealthResponse`, `RuntimeMetricsResponse`, `RuntimeMessagesResponse` |
| `executors.FlowManager`, `executors.FlowTemplateManager`, `executors.NewFlowExecutor`, `executors.NewFlowTemplateExecutor` | with the two `ToolDependencies` fields of the same names |
| `service.FlowServiceConfig` (`flow_service.go:28`) | none — it configured the removed service (`prometheus_url`, `fallback_to_raw`); a `flow-builder` block in a `services` config is now an unknown service and refuses at boot |
| `service.OverallHealth`, `service.ComponentHealth` (`flow_runtime_health.go:42,50`) | none — response types of `/observations/health`. Read component health at `GET <components>/health` and `GET <components>/status/{name}`, whose shapes are unrelated |
| `service.ComponentMetric` (`flow_runtime_metrics.go:52`) | none — response type of `/observations/metrics`. Scrape `/metrics` |
| `service.RuntimeMessage` (`flow_runtime_messages.go:16`) | none — response type of `/observations/messages`. Use the message-logger service's own routes |
| `executors.FlowExecutor`, `executors.FlowTemplateExecutor` (`flows.go:27`, `flow_templates.go:29`) | none — the executors behind the eleven tools |
| `flowgraph.BuildFromRegistry` | no production caller once `engine` and the ComponentManager rebuild are gone; `flowgraph.BuildFromDeclarations` is the one construction seam, and `composition.Analyze` is the one caller that matters |
| `flowgraph.FlowAnalysisResult.ValidationStatus` | a `"healthy"`/`"warnings"` string with **no production reader**, computed by a walk that (like the retired `/gaps`) treated every required stream port with `no_publishers` as critical without checking `External`. Severity belongs to `composition.Result.Status` |
| `service.ComponentManager.GetFlowGraph` | see "`/paths` now serves the retained graph" below |
| `service.ComponentPortInfo`, `service.ComponentPortDetail`, `service.FlowConnection`, `service.ComponentPortReference`, `service.FlowGap` | residue of the retired `/gaps` analysis: a second, exact-subject-match connection interpreter with no caller |

### `schemas/workflow-definition.v1.json` is deleted

Owner ruling on #1122 (2026-08-27): the file belongs to this retirement and goes with it. It was a **generated
artifact with no generator** — no registered factory produces it, the generator stopped emitting it when the old
workflow processor was retired, and `test/contract` carried a `nonComponentSchemas` exemption so the
orphaned-schema, schema-drift, JSON-Schema-shape and `default_ports` guards all skipped it. That exemption is removed
too, so every file in `schemas/` is now a component schema subject to all four guards with no exceptions. `schemas/`
goes from 34 files to **33 — one per registered factory**, which is what the ADR-100 inventory said the number should
have been all along.

Nothing in this repository read it, and no product reads it through an API: it is a checked-in file that downstream
repositories **vendor by copying**. Two carry a copy today, and both should drop it:

| Repository | What to delete | Measured |
|---|---|---|
| semstreams-ui @ `39f5f04` | `contracts/semstreams/schemas/workflow-definition.v1.json` (4,595 bytes) | Vendored artifact only — `grep -rn "workflow-definition"` over `src/`, `e2e/`, `scripts/`, `package.json` (excluding `node_modules`) returns **no hand-written reference**, so deleting the file is the whole change |
| semteams @ `8a70b7e7` | `schemas/workflow-definition.v1.json` (4,595 bytes) **and** the exemption that hides it: `test/contract/schema_contract_test.go:14-18` (the `nonComponentSchemas` declaration and its `"workflow-definition": true` entry at `:17`) plus its three skip sites at `:57`, `:106`, and `:188` | semteams mirrors this repository's contract test; with the entry gone its own orphaned-schema guard fails on the vendored file, so the file and the exemption must be deleted together |

There is no replacement. A product that still wants a JSON-Schema for a workflow definition owns it in its own
repository; the framework generates schemas only for registered component factories.

**Two KV buckets are no longer created**: `semstreams_flows` and `FLOW_TEMPLATES`. A bucket retained from an earlier
deployment is inert — nothing reads it, nothing writes it, and no migration, legacy reader, or compatibility Flow view
exists. Delete it when convenient.

### `/paths` now serves the retained graph

`GET <components>/paths` is **unchanged in shape** (the same `paths` map and `statistics` object) but is now derived from
the `composition.Result.Graph` ComponentManager retains at boot, instead of rebuilding a second graph from the Registry
on demand. One consequence for a caller: before the composition result exists the route answers `503` rather than `500`,
matching its sibling projections `<components>/flowgraph` and `<components>/validate`. It still derives no severity — it
is a projection, not a judgment.

### The stream-override expiry metric moved hosts

`semstreams_streams_migration_override_expired` was hosted by the flow-builder service. It is now owned by the
component-manager, the one service the framework refuses to compose without, so a deployment that declares a
`stream_migration_overrides` bridge cannot lose the report by not enabling a service. **The metric name, labels
(`stream`, `owner`), and semantics are unchanged.** It is now registered against the registry `/metrics` scrapes at
composition time and evaluated once immediately, so the series exists from boot rather than from the first tick.

**Expect output you have never seen before.** "Unchanged" describes the metric's contract, not its reach. Before
beta.163 this report was effectively unreachable: the WARN fired only in a process that enabled the `flow-builder`
service, and the gauge reached `/metrics` in **no** process at all, because it was registered only through
`Service.RegisterMetrics` — a method nothing in the framework calls. Both now run in every process, in every
composition. So if you hold a `stream_migration_overrides` bridge whose expiry has already passed, the first boot on
beta.163 starts logging a WARN per lapsed bridge per minute and exporting
`semstreams_streams_migration_override_expired{stream=...,owner=...} 1`. Nothing regressed — the signal is working for
the first time. Bound the stream (`max_age`, `max_bytes`, `discard`), or move it to `archival_streams` with an owner
and a reason if permanence is genuinely its contract.

### Downstream action

Measured on 2026-08-27 at each sister's pinned SHA, read-only.

- **semstreams-ui — pinned `39f5f04`. The largest break in this wave.** 17 hand-written files under `src/` reference the
  removed surface across 19 call sites: `src/hooks.server.ts` (the proxy path gate on `/flowbuilder`),
  `src/lib/api/flows.ts`, `src/lib/services/{flowApi,publishApi,observationsApi,messagesApi,opsSummaryApi}.ts`,
  `src/lib/server/mcp/tools.ts`, `src/lib/server/ai/toolExecutors.ts`,
  `src/lib/components/{OpsConsoleShell,runtime/OpsReadinessMatrix,runtime/LogsTab}.svelte`, `src/lib/types/flow.ts`, and
  the four files under `src/routes/flows/`. Counted precisely on 2026-08-27: **17 hand-written `src/` files**
  (19 call sites), **16 `e2e/` files naming `flowbuilder`**, and **4 further `e2e/` files that drive the `/flows` UI
  routes without naming the proxy path** — `e2e/flow-crud.spec.ts`, `e2e/flow-management.spec.ts`,
  `e2e/navigation.spec.ts`, `e2e/pages/FlowListPage.ts` — which lose their backend all the same: **20 e2e files in
  total**. Among the first group, `e2e/helpers/backend-helpers.ts`'s `reapOrphanedTestFlows` runs from
  `e2e/global-setup.ts` on **every** run and will fail against a backend with no `/flowbuilder/flows`. The canvas editor and publish panel lose their backend;
  what comes back is a read-only projection (`<components>/flowgraph`, JSON or `?format=mermaid`) of what is running and
  a validator (`<components>/validate`) for what would run. Regenerate `src/lib/types/api.generated.ts`
  (`npm run generate-types`) — the `Flow*` schemas and every `/flows*` operation key disappear.
- **semteams — pinned `8a70b7e7`.** Compiled read-only against this branch in a scratch copy
  (`go vet ./cmd/semteams/`); three errors, all import-resolution:
  ```
  cmd/semteams/main.go:24:2: module github.com/c360studio/semstreams ... does not contain package .../engine
  cmd/semteams/main.go:25:2: module github.com/c360studio/semstreams ... does not contain package .../flowstore
  cmd/semteams/main.go:26:2: module github.com/c360studio/semstreams ... does not contain package .../flowtemplate
  ```
  Behind those imports: `buildFlowManager` (`main.go:570`), `buildFlowEngine` (`:590`, calls `flowengine.NewEngine`
  at `:595`), `buildFlowTemplateManager` (`:608`), `loadFlowTemplates` (`:627`), `seedKVCorpora` (`:886`), the
  `FlowManager` / `FlowTemplateManager` / `FlowEngineManager` fields at `:297-300`, the whole
  `cmd/semteams/flowtemplates/` loader package, the three `configs/flow-templates/*.json` fixtures, and its
  `test/contract` seed test. `ui/src/lib/api/flows.ts`, `ui/src/lib/services/flowApi.ts`,
  `ui/src/routes/admin/flows/+page.ts`, `ui/src/lib/server/mcp/tools.ts` and
  `ui/e2e/agentic/admin-flows-inventory.spec.ts` call the removed routes. Note semteams was **already** failing to
  compile against `main` before this landing (`FlowEngineManager` unknown field, `NewEngine` arity, plus two non-flow
  breaks: `InitializeKVStore` and `StopAll` now take a context); this landing adds the three imports to a migration it
  already owed. The admin inventory page's replacement is the projection.
- **semspec `5a9496ee`, semdragon `07f4de9`**: generated TypeScript/OpenAPI artifacts only — regenerate. No
  hand-written call site, no Go import.
- **semsource, semconnect, semboids, semmachina, semops, semmem, semdev**: no call site and no import. Two prose
  mentions in semsource docs.

## core-federation e2e scenario removed (gh#1129)

Added 2026-08-27, after the beta.163 landings above; verified against this repository's own `origin/main` HEAD at
the time of writing, not the pinned sister SHAs the provenance sentence at the top of this document covers — this
section carries no sister-repo obligation, so no sister pin applies.

**Nothing ran it.** The `core-federation` e2e scenario (`test/e2e/scenarios/core_federation.go`), its dispatch case
and menu entry in `cmd/e2e/main.go`, its two configs (`configs/cloud-federation.json`, `configs/edge-federation.json`),
and its doc (`test/e2e/docs/core-federation.md`) are deleted with no replacement. It was dispatchable but unreachable:
no `task e2e:federation` wrapper existed, no compose file defined an `edge` service, and `cloud-federation.json`
dialed the literal hostname `ws://edge:8082/stream` that this repo never created. Owner ruling 2026-08-27 (gh#1129,
option (b)): delete under the greenfield principle — a menu entry that fails for anyone who selects it is legacy
cruft, not a capability. No in-tree consumer (verified by grep across this repository); the e2e scenario set is not
part of the supported framework surface, so no downstream-obligation claim is made about the read-only sister repos
— nobody read them for this.

## Entity-ID segment semantics, slice A (ADR-102, #1095) — the canonical order is `org.platform.system.domain.type.instance`

Added 2026-08-27. The per-sister table below was measured read-only on 2026-08-26 at these SHAs: semsource
`4093d3ce`, semmachina `841c45e8`, semdev `ca3956af`, semdragon `07f4de9b`, semteams `8a70b7e7`, semconnect
`d0d06e00`, semspec `5a9496ee`, semmem `b909cbf1`, semboids `8c03cc53`, semops `602c619a`, semsage `4d28b4dc`. All
eleven were re-verified read-only when this section was written: each resolves to a real commit and each equals that
sister's current `HEAD`, so every row diffs against something. Every SemStreams-side claim below was re-verified
against this branch after merging `#1116`, `#1109`, and `#1130`.

### What changes on the wire

Every minted identity changes. Positions 3 and 4 swap and gain meanings: position 3 is **`system` — the source** that
produced the entity (a repository, feed, world, board, API, or framework component); position 4 is **`domain` — a
delegated taxonomy**. Positions 1-2 are the **minting deployment authority**: the composition root's `platform.org` /
`platform.id`, carried to components as `deps.Platform`, and never a payload value, a constant, or a product name.
`instance` stays last. Arity stays six. There is no accept-both-orders parser, no alias for the retired order, and no
compatibility knob: a downstream starts on newly provisioned NATS storage after every owned builder, pattern, config,
fixture, and query is updated (`openspec/specs/entity-id-contract/spec.md` "clean owned-source break").

**Doing nothing here fails silently — this is the one obligation in this section with no loud path.** A sister that
keeps minting `org.platform.domain.system.type.instance` still emits six lexically valid segments.
`pkg/types.ValidateEntityID` is arity and alphabet only (`pkg/types/entity_id.go:149`); it has no position semantics,
so the ID passes, graph-ingest accepts it, and nothing logs. The reinterpretation happens downstream and everywhere at
once: `graph/inference/hierarchy.go:26-29` cuts containers at the named prefix levels, so the "system" container is
now the taxonomy and the "domain" container the source; `graph/clustering/entityid_provider.go:224` `getSystem`
returns the taxonomy as the source; `processor/graph-query/summary.go:197` composes `entityTypes[].type` as
`System + "." + Domain + "." + Type`; and `vocabulary/export/export.go:129-130` writes the IRI path
`{org}/{platform}/{system}/{domain}/{type}/{instance}` inverted. Every entity, every query, no error. The only
detector is running `cmd/entity-id-audit` over your own tree — and it reads statically resolvable values, so a builder
whose positions are all `%s` verbs carries no literal to judge and is invisible to it. Re-read the two middle
positions of every builder by hand before trusting a green audit.

Framework families re-slot (`<system>.<domain>.<type>`): loop execution `agentic-loop.agent.execution`, chain
`chain.agent.execution`, model endpoint `model-registry.agent.endpoint`, lesson `lesson.agent.record` (record prefix
`org.platform.lesson.agent.record`), web observation `web.agent.observation`, ops diagnosis `diagnosis.ops.finding`,
gated-DAG fan-out `gated-dag.agent.fanout` (ruled O-9), and — under the deployment's own authority instead of ADR-076's
retired `semstreams.framework` literal — rule alerts `org.platform.rules.graph.alert.<digest>` and rule triggers
`org.platform.rules.graph.trigger.<digest>`. Two deployments running one pack no longer converge on one trigger entity.

Two values that **leave the graph** follow the new order and are not re-minted by fresh state (owner item O-10): the
vocabulary export IRI path `<base>/entities/{org}/{platform}/{system}/{domain}/{type}/{instance}` and the `graphSummary`
GraphQL value `entityTypes[].type`, now `system.domain.type`.

### The obligations

1. **`platform.instance_id` is gone (ruled O-2).** `platform.id` is the single authority field; a config that still
   carries `instance_id` does not load (`config field platform.instance_id was removed (ADR-102, BREAKING) …`). Every
   sister `extractPlatformMeta` that preferred `InstanceID` drops that precedence.
2. **The authority pair is bounded at load (ruled O-14).** `len(platform.org)+len(platform.id) <= 170` bytes while the
   rule-trigger family (86 fixed bytes) binds; the budget is `pkg/types.MaxAuthorityPairBytes()`, derived from
   `pkg/types.LongestFrameworkIdentityFamily()`, never configured. An `org` or `id` that is not one canonical entity-ID
   segment (a dot, a leading `-`/`_`) is rejected at load for the same reason.
3. **Builders swap positions 3-4 and take authority only from `deps.Platform`.** A product name in `platform`, a fixed
   literal authority in a builder, a `Sprintf` template whose org/platform are literals, or a trailing-dot prefix
   constant is an `authority_literal` finding once the sister runs `cmd/entity-id-audit` over its tree.
4. **Domains are delegated (ruled O-3/O-5) — and the two halves of that have different enforcement.** A product
   declares `[]pkg/types.EntityDomainDelegation{{Producer, Domain[, Type]}}`. The framework reserves `agent`, `ops`,
   `graph`; `system` and `instance` values are never registered.
   *Declared-or-reserved is enforced by the corpus audit — in production Go only.* A literal position-4 value in
   production Go that is neither reserved nor declared is a `domain_unregistered` finding from `cmd/entity-id-audit`,
   which reads your `EntityDomainDelegation` literals as the registered set
   (`internal/entityidaudit/segment_rules.go:152-155`). Every production-Go declaration-pattern surface is judged,
   including a projection contract's `EntityPattern` field in both spellings — the named literal and the elided
   `[]Contract{{…}}` element.
   **A config is extracted but never domain-checked.** `segment_rules.go:57-59` returns before the domain rule for
   anything outside production Go, so a rulepack's `projection_contracts[].entity_pattern` is judged only lexically
   and for `authority_literal`. A rulepack left in the RETIRED ORDER passes the audit silently — verified by
   mutation. Mirror the Go contract by hand and re-read positions 3-4 yourself; on that surface the audit will not
   tell you.
   *Sharing a domain across producers is PERMITTED, and nothing reports it* — owner ruling 2026-08-28, superseding
   O-5. Two products may both declare `web`; the taxonomy vocabulary is shared and the overlap is sometimes the
   point. It collides nothing: `system` is position 3, so `acme.prod.semsource.web.page.001` and
   `acme.prod.semdragon.web.doc.001` are distinct IDs, and ADR-099 level 0 is source x taxonomy, so the two are
   distinct communities. The cross-source wildcard `org.platform.*.<domain>.*.*` then returns every producer's
   entities in that taxonomy, which is the query sharing exists to serve; a `system` prefix narrows it back to one
   source.
   There is **nothing to call and nothing to configure**. `EntityDomainAuthority`, `NewEntityDomainAuthority`, and
   `Authorize` are deleted — a composition root that once passed delegations to them now passes them nowhere, and
   `EntityDomainDelegation` remains only as the declaration the corpus audit AST-scans. Two products meaning
   different things by one token is a vocabulary problem — someone picked the wrong token — and the framework does
   not detect it at composition time.
5. **Prefix levels have fixed meanings (ADR-102 d6):** 2 = deployment (`DeploymentPrefix`), 3 = source
   (`SourcePrefix`, the federation triple; ADR-099 level 1), 4 = taxonomy (`TaxonomyPrefix`; ADR-099 level 0), 5 = type
   (`TypePrefix`). `SystemPrefix`, `DomainPrefix`, `PlatformPrefix`, `IsSameSystem`, `IsSameDomain`
   no longer exist, and neither does `IsSameSource` — compare `SourcePrefix()` directly. "A taxonomy across sources" is the wildcard pattern
   `org.platform.*.D.*.*` or a `tag:` lesson scope, never a prefix; a three-position `id:` lesson scope key now means one
   source within one deployment.
6. **Exported signatures.** `graph.NewAlertEvent(org, platform, alertType, sourceEntityID, properties, metadata)`;
   `rule.NewExpressionRule(platform types.PlatformMeta, packID, def)`, `rule.NewTestRule(platform, packID, …)`,
   `rule.Dependencies.Platform`, `(*rule.Processor).SetPlatform` (installed by `CreateRuleProcessor` from
   `deps.Platform`); `internal/semantictest.EntityID(t, org, platform, system, domain, type, instance)` — positional,
   so call sites keep their strings and only the two middle arguments change meaning; swap them where the fixture named
   a family.
7. **Rule substitution tokens keep their names**: `$entity.system` / `$entity.domain` (and `$related.*`) resolve by the
   named position, so a template written against the names is unchanged; a config-authored subject carrying `$entity.id`
   emits the new token order, and a subscriber pinning position literals follows it.
8. **Reference configs**: `entity_watch_buckets.ENTITY_STATES`, rule `entity.pattern`, and
   `projection_contracts[].entity_pattern` values carry no literal authority (`*.*.…`); a literal org/platform in any
   of those three in a shipped config is an `authority_literal` audit finding.

### Per-sister list (values → after; measured read-only 2026-08-26, at the eleven SHAs pinned at the top of this section)

| Sister | Change |
|---|---|
| semsource | `PlatformSemsource` constant → `deps.Platform.Platform`; order swap at every `entityid.Build` call; register domains `web, media, config, git, golang, svelte`; `MaxOrgLen` arithmetic re-checked against the 170-byte pair bound; `handler/entity_state_test.go` fixtures |
| semmachina | per-world composed `platform.id` is already the authority; order swap |
| semboids | delete the `"semboids"` fallback literal; order swap in two builders; register `sim` |
| semdev | order swap (`forge.intake`, `repo.standards`, `agent.chain.execution` prefix → `chain.agent.execution`); register `forge`, `repo`; drop `instance_id` precedence |
| semdragon | replace `Org "default"`/`Platform "local"` defaults with config; order swap (`game.<board>`, `web.agent.doc`); register `game`, `web` — `web` is also semsource's domain, which is PERMITTED (ruling 2026-08-28): the two are told apart by `system` at position 3 and land in distinct ADR-099 level-0 communities, and nothing reports the overlap |
| semteams | one literal (`attestation_runner.go:124`); drop precedence; e2e configs |
| semops | `Platform: "edge"` literal → config; order swap (`cop.fusion`); register `cop` |
| semconnect | `semconnect` platform → config; `SystemEventIDPrefix` shape; register `systems` |
| semspec | order swap in `agentgraph/entities.go:54-60`; the `rule.NewExpressionRule(def)` call in `workflow/intakerules/rulepack_test.go:124` was already one argument behind main; 10k fixtures importing semsource IDs re-generate after semsource re-slots |
| semsage | `OrgDefault`/`PlatformDefault` constants → config; `_` placeholder fixture |
| semmem | 5-part fixtures → six-part in the new order; imported lessons opt-in by scope (O-13); curation status on an imported lesson lives on a local overlay entity (O-12) — both land with slice B |

### Slice B (this PR) — the boundary is now enforced

Slice A shipped `pkg/types.ValidateEntityIDAuthority` and left it unwired. Slice B wires it. What follows is the
whole adopter-facing surface, and for each one what happens if you do nothing.

**What must an adopter know.** Four things, no more:

1. **Your composition root must carry `platform.org` and `platform.id`.** graph-ingest and the rule processor now
   REFUSE to construct without them (`deps.Platform`). Config load has always required both, so a deployment that
   loads a config already satisfies this; a binary that hand-builds `component.Dependencies` does not.
2. **graph-ingest accepts only entities your deployment minted** — positions 1-2 of every candidate SUBJECT must
   equal your `org`/`platform`, on every lane, before any KV I/O. Rejection is coded
   `entity_id_authority_invalid` with reason `foreign_authority`, metered
   `mutation_rejections{reason="authority_foreign"}`, and logged WARN with the lane and segment index (never the
   identity).
3. **To hold a peer's entities, declare an import lane** — `"import": true` on a `jetstream` INPUT port. On that
   lane a foreign pair is persisted byte-for-byte and a subject claiming YOUR pair is refused
   (`local_authority_claimed` / `mutation_rejections{reason="authority_claimed"}`). It is an operator statement of
   trust and nothing is authenticated: the recorded provenance is the port declaration plus the envelope `source`
   string.

   **No shipped config declares one, deliberately.** A lane is a decision to trust a peer, so the default
   composition must import nothing; a reference config carrying an enabled lane would hand that decision to whoever
   copied the file. Add it yourself, to the graph-ingest instance that should hold the mirror:

   ```json
   {
     "name": "peer_import",
     "config": {
       "kind": "jetstream",
       "stream_name": "PEER_ENTITY",
       "subjects": ["peer.entity.>"],
       "deliver_policy": "all",
       "import": true
     }
   }
   ```

   Declare the backing stream too (`streams.PEER_ENTITY` with `max_age`, `max_bytes` and `discard`), or graph-ingest
   waits for a stream nothing provisions and `Start` fails. `import` is INPUT-only: on an output port it is refused
   at config resolution rather than ignored.
4. **An import is a READ-ONLY mirror** (ruled O-12(a)). No local lane mutates a foreign subject — not `entity.create`,
   not `triple.append`, not `entity.reconcile`, not `entity.delete`, and not the framework's own writes. Every local
   fact about an imported entity lives on a LOCAL subject that references it through an `@id` triple; `@id` OBJECTS
   are never authority-checked, no stub is created for them, and an absent target is permitted, which is what makes
   that pattern work.

**What SHOULD an adopter have to know? Ideally only (3).** The framework already holds your `org`/`platform`; it does
not ask you to restate the pair anywhere, to predict which writes are local, or to compute a lane. `import` is the one
knob, and it exists because trusting a peer is a decision only an operator can make. Everything else is observed:
the boundary compares against the pair it already has and reports the real outcome.

**What happens if you do nothing — three loud paths and one silent one.**

- *No `platform.org` / `platform.id`* → **LOUD.** Boot fails at the factory:
  `deps.Platform must carry the deployment authority (platform.org and platform.id)`.
- *A peer's entities arriving on an ordinary lane* → **LOUD.** Each is refused with the coded error, counted under
  `mutation_rejections{reason="authority_foreign"}`, and logged. Nothing is written; nothing is silently dropped.
- *A direct `inference.NewHierarchyInference` consumer that does not set `HierarchyConfig.Org`/`Platform`* →
  **LOUD.** With `Enabled: true` and no pair, `GetHierarchyTriples` returns a classified error rather than deciding
  every entity is foreign and minting nothing forever.
- *A rule chained off the run anchors of an IMPORTED firing entity* → **SILENT, and this is the one to read twice.**
  When a rule with `run_scope=new` fires on an imported loop, the framework writes NOTHING to that loop: neither
  `agent.loop.run`, nor `agent.run.entity-id`, nor the `rule.task.spawned` back-reference. A chained rule whose
  condition reads `$entity.triple.agent.run.entity-id` or `$entity.triple.rule.spawned_task` off that entity simply
  never fires. **A rule that does not fire logs nothing and fails nothing** — the only signal is
  `rule_foreign_firing_writes_skipped_total{reason="foreign_authority"}` rising and one Info line per dispatch naming
  which writes were skipped. **It counts DISPATCHES, not entities.** One increment is one `publish_agent` dispatch —
  one (firing entity x `for_each` item) — whose framework writes were all declined. The firing entity does not vary
  across a `for_each` fan-out, so an action fanning out over N items on a single imported entity reports N. Do not
  read the counter as "distinct peer entities we declined to write to"; over a fanning rule pack it exceeds that
  number by the fan-out factor. If your rule packs chain off either predicate and any of your firing entities are imported, re-point
  those rules at the LOCAL run entity (`org.platform.chain.agent.execution.<loopID>`) or its local children before
  you upgrade. Hierarchy is the same shape and the same ruling: an imported entity is persisted with no
  `hierarchy.*` triple, no container, and no sibling edge, so structural-tier queries return fewer edges over
  imported data and nothing says so.

**The linkage that replaces those writes.** Every agent run — local origin or imported — now carries
`agent.run.origin-entity-id` on the LOCAL run entity, naming the loop it was minted from, set at birth by
`agentrun.Mint`. It is the one home for the run→loop pointer that never depends on writing the loop. Walk it from the
run side: run → `agent.run.origin-entity-id` → the mirrored loop → its `agent.loop.parent` chain.

**Exported signature changes:** `agentrun.Mint(ctx, mgr, org, platform, rootLoopID, originEntityID)` gains the
origin; `AgentRun` gains `OriginEntityID`; `component.JetStreamPort` gains `Import`; `component.StreamFacts` gains
`Import()`; `inference.HierarchyConfig` gains `Org`/`Platform` (both `json:"-"` — framework-owned, never operator
config). `agvocab.RunOriginEntityID = "agent.run.origin-entity-id"` is a new declared predicate.

**Stated limit on the origin predicate.** The design calls `agent.run.origin-entity-id` an `@id` predicate, and its
object IS a canonical entity ID — but the stored triple carries no `@id` datatype marker, because the Lifecycle
harness has no write-side datatype channel: `pkg/lifecycle` emits every projected triple through one helper that sets
Subject, Predicate, Object, Timestamp and Confidence and nothing else (`pkg/lifecycle/graph_emit.go`; the package
contains no `Datatype` reference at all). Its sibling `agent.run.parent-entity-id` and the run anchor
`agent.run.entity-id` behave identically today, so this is the family's existing convention rather than a new gap.
The consequence is narrow and worth knowing: `pkg/fusion`'s graph facet projects an edge only for a lens-declared
predicate or an explicit `@id`, so the origin link reads as a property fact there unless your lens declares it.

### Interaction with the ADR-103 section above

`internal/builtinprojection` no longer exists (#1109 deleted it); the two projection contracts that declared entity
patterns now live on their payload registrations — `agentic.LessonContract()` (`agentic/agent_lesson_entity.go`) and
`agentic.LoopExecutionContract()` (`agentic/loop_execution_entity.go`). A sister that copied either `entity_pattern`
literal re-slots it there, not in a projection package. The framework's own `configs/rules/lessons/lesson-lifecycle-rulepack.json`
carries the re-slotted `*.*.lesson.agent.record.*` beside ADR-103's structured `message_type` object; a product rulepack
mirroring that contract changes both in one edit. Both spellings are inside the audit corpus — the Go field as
`go-field:Contract.EntityPattern` and the config key as `config:projection_contracts.entity_pattern` — so a
contract left in the retired order is a `domain_unregistered` finding in production Go, and a contract given a literal
org/platform is an `authority_literal` finding in either.

## Entity-ID segment semantics, slice C (ADR-102 d2, #1149) — `org_id` / `platform` leave the example processors

Added 2026-08-28. Slice A moved the *meaning* of positions 1-2 to the composition root's `platform.org` /
`platform.id`. Its corpus sweep did not reach `examples/processors/`, where three shipped example components still
declared `org_id` and `platform` as their own operator config keys — two of them `required:true` — and minted from
them, while the payload types additionally carried `OrgID` / `Platform` on the wire. Every shipped config that
composed them declared one authority at the top level and a different one on the component:
`configs/e2e-structural.json` said `c360` / `semstreams-e2e-structural` at the top and `c360` / `logistics` on
`iot_sensor`. This slice retires both meanings.

### What changes

1. **`org_id` and `platform` are removed from `iot_sensor`, `document_processor`, and `weather_station`, and a
   config that still carries either FAILS TO LOAD** with a coded error naming ADR-102 d2:

   ```text
   IoTSensorComponent.rejectRemovedConfigKeys: config field "org_id" was removed (ADR-102 d2, BREAKING):
   positions 1-2 of every minted entity ID are the composition root's platform.org / platform.id and nothing
   else — never an operator knob on a component. Delete the field; set platform.org at the top level of the
   config
   ```

   The refusal fires on both entry paths that read the raw component config — `DeclarePorts` (offline composition
   validation) and `NewComponent` (boot) — because both share one `resolveConfig`. Same shape as slice A's
   `platform.instance_id` rejection (`config/config.go` `removedPlatformFields`) and graph-clustering's
   `removedConfigFields`.

2. **`OrgID` and `Platform` leave the wire shape of all seven example payload types** — `SensorReading`, `Zone`,
   `Document`, `Maintenance`, `Observation`, `SensorDocument`, `WeatherReading`. Each now carries one
   `entity_id` field holding the identity its processor minted, and `EntityID()` returns that value rather than
   recomputing it. A minting function sits beside each type, each taking `types.PlatformMeta` —
   `component.Dependencies.Platform`, verbatim:

   | Payload type | Package | Minting function |
   |---|---|---|
   | `SensorReading` | `iot_sensor` | `SensorReadingEntityID` |
   | `Zone` | `iot_sensor` | `ZoneEntityID` |
   | `Document` | `document` | `MintDocumentEntityID` |
   | `Maintenance` | `document` | `MintMaintenanceEntityID` |
   | `Observation` | `document` | `MintObservationEntityID` |
   | `SensorDocument` | `document` | `MintSensorDocumentEntityID` |
   | `WeatherReading` | `weather_station` | `WeatherReadingEntityID` |

   The `document` package's four carry a `Mint` prefix because `document.DocumentEntityID` stutters under revive.
   The prefix went on all four for within-package consistency, not on the other three packages, whose names do not
   stutter. If you rename these in your own copy, scope the edit to the package: `ObservationEntityID` is a
   substring of the unrelated `agentic.TryWebObservationEntityID`, and a tree-wide substitution rewrites it.

3. **`NewProcessor` takes the deployment authority, not a config struct.** The `Config` struct that held
   `OrgID` / `Platform` in each of the three example packages is deleted; `NewProcessor(deps.Platform)` replaces
   `NewProcessor(Config{OrgID: …, Platform: …})`.

4. **Every entity the example processors mint changes identity.** `c360.logistics.*` becomes the deployment's own
   authority: `c360.semstreams-e2e-structural.*` under `configs/e2e-structural.json`,
   `c360.semstreams-statistical.*` under `configs/statistical.json`, `c360.semstreams-kitchen-sink-ml.*` under
   `configs/semantic.json` (and its `-8b` / `-frontier` overlays), `c360.semstreams-structural.*` under
   `configs/structural.json`, `demo.hello-world.*` under `configs/hello-world.json`.

### Doing nothing — LOUD on the config, and this is deliberate

An operator who upgrades and changes nothing gets a **boot-time refusal**, not a silent re-mint. That is the whole
point of obligation 1. `encoding/json` DROPS an unknown key without complaint, so removing the struct field alone
would have left the operator's `"platform": "logistics"` in place, ignored, while every entity ID moved to
`platform.id` — a silent authority change discoverable only by diffing entity IDs. The rejection converts that into
an error naming the field and its replacement.

This is the opposite of slice A's own position-3/4 swap, which the section above records as silent. Where a break
CAN be made loud, it is.

### Adopter action

1. Delete `org_id` and `platform` from every `iot_sensor` / `document_processor` / `weather_station` component block
   in your configs. If the values differed from your top-level `platform.org` / `platform.id`, decide which one you
   meant: the top-level pair is now the only authority, so move the value you want there.
2. If you copied an example processor into your own repo (the README tells you to), apply the same three edits:
   delete the config keys, add the rejection probe, and take the authority from `deps.Platform`.
3. Re-provision NATS storage. Every entity ID these processors mint changes; there is no alias, migration, or
   dual-read (`openspec/specs/entity-id-contract/spec.md` "clean owned-source break").
4. Re-point any query, fixture, dashboard, or rule that names `c360.logistics.*` at your deployment's own authority.

### Not affected

Products that do not compose the bundled example processors are unaffected: no framework component ever had an
`org_id` or `platform` config key. `cmd/e2e-semstreams/mission` already took its authority from `deps.Platform` and
refuses a wire value that disagrees, so it needed no change.
