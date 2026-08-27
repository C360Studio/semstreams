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
the contract is rejected. Full mechanics: `openspec/specs/payload-registry/spec.md`, `openspec/specs/graph-ingest/spec.md`.

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
  `MessageType` may be left empty — `Register` fills it). Invert the "deliberately unregistered" tests
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
  contracts for those two (`internal/graphown/contracts.go`) bound to the types.
- **Verification:** two round-trip tests; `graphown.Creator.Create` against beta.163 returns `applied`.

### semconnect — pinned `d0d06e0`

- **Types stamped (11):** `c360.csapi-{system,datastream,procedure,deployment,sampling-feature,property,control-stream,command,system-event,feasibility,schema-artifact}.v1`
  (`gateway/cs-api/projection_contracts.go:29-39`), stamped at `gateway/cs-api/graph_mutations.go:159`. semconnect holds **no
  registry**: `cmd/cs-api-server` calls neither `payloadbuiltins.Register` nor `payloadregistry.New`; its one registration
  (`message/oms/register.go:16-22`, OMS observation) is exported for a host. The 11 stamps reach the **host's** graph-ingest.
- **Day one:** every CS-API resource birth is rejected by the host's graph-ingest.
- **Obligation:** export `RegisterPayloads` from `gateway/cs-api` for the 11 types (floor `content` — the value its contracts
  already declare at `projection_contracts.go:44-64`; contracts: `representationContract`/`birthOnlyContract` bound to each
  type), and have the **host composition root** call it after `payloadbuiltins.Register`. `message/oms.RegisterPayloads` is the
  in-tree model for the shape.
- **Verification:** round-trip tests in `gateway/cs-api`; one host-side boot that registers both and creates a system resource.

### semteams — pinned `8a70b7e7`

- **Types stamped:** none of its own on `entity.create`. Its contracts re-declare the framework's loop-execution and lesson
  contract structure with the framework's key builders (`cmd/semteams/main.go:971,998`); lifecycle births through `Manager`
  (`cmd/semteams/flowtemplates/loader.go:200`) carry `lifecycle.harness.v1` — registered by `payloadbuiltins.Register`. Own
  registered types: `research/artifact.go:253`, `devviaspec/plan.go:148`, `semsource/payload.go:39-69`.
- **Day one:** nothing is rejected.
- **Obligation:** none at ingest. Recommended: replace the two re-declared contracts with `agentic.LoopExecutionContract()` and
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
  mutation-lane use is `Reconcile` only (`processor/supersession/lifecycle.go:303,325`). Nothing to do.
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
