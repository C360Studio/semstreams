# Proposal — composition-validation-substrate

Target state for #1089 under option C (owner direction 2026-08-26), conditional on the owner accepting ADR-100
(`docs/adr/100-compositions-are-validated-diagrams-are-projections.md`, Proposed). Every `file:line` was measured at
`5cc0c7fbe569c6398fc534025218639b4c7e0345`; the inventory is
`docs/proposals/gh1089-flow-boundary-inventory.md` and the design is `docs/proposals/gh1089-flow-boundary-design.md`.

## Why

Connection-level validation exists twice and runs at neither boundary that matters. The saved-diagram path
(`engine/validator.go:87-200`) constructs every node through the Registry with a live NATS client
(`:244-246`; the Registry refuses a nil client first, `component/registry.go:228`) because port *values* are read
from constructed components (`registry.go:564-571`). The running-composition path (`GET <components>/validate`,
`service/component_manager_http.go:657-716`) analyzes the admitted declarations but only when asked, and interprets
the same `flowgraph` analysis with different severities than the engine (`engine/validator.go:313-361` vs
`component_manager_http.go:677-683`). Nothing runs at boot: `grep -rn "AnalyzeConnectivity\|ValidateFlowConnectivity"
--include='*.go' service/` finds only the cache method and the HTTP handlers. Nothing runs offline: `semstreams
--validate` (`cmd/semstreams/main.go:102-115`) checks config, port-derived streams, rule packs, and capabilities, not
connections.

The chicken/egg is narrower than #1089 framed it. All 33 registered factories compute their ports as a pure function
of raw configuration (32 config-derived, 1 static, 0 runtime-only — inventory §2.3); 31 declare them through the
config-shaped `component.PortConfig` lane (`component/ports.go:53-113`); and `config.Validate` already derives stream
declarations from configured ports without constructing anything (`config/stream_bounds.go:259-320`). What is
missing: factory **default** ports are not on `Registration` (`registry.go:52-62` carries `Schema`, not ports), the
derivation is not exported, and the two interpreters are not one.

Meanwhile the diagram surface carries about 4.1k production and 5.6k test lines (`flowstore` 547/1,099, `flowtemplate`
258/314, `engine` 837/131, `service/flow_*` 1,657/3,371, executors 617/635, wiring ≈105), two KV buckets, ten
OpenAPI operations (`specs/openapi.v3.yaml:113-305`; `/flowgraph` and `/gaps` are ComponentManager's), eleven agent tools, and three open milestone items (#1008, #1060, #1087) — all polish on the HTTP
layer of a canvas editor the owner has written off. No e2e tier drives any of it (inventory §11).

## What Changes

- **P1 — static port declarations.** `component.Registration` gains a pure `Ports` declarer
  (`func(rawConfig json.RawMessage, instanceName string) (component.PortConfig, error)`), rejected when nil at
  `RegisterFactory` (`registry.go:139-147` precedent for nil `Factory`). Each of the 33 shipped factories exposes its
  existing port derivation as that function and calls it from its constructor (one home). Boot admission evaluates the
  declarer and refuses a component whose constructed ports differ (`registry.go:209-273`, after
  `captureComponentDeclaration`). The catalog (`schemas/*.v1.json` via `cmd/openapi-generator/main.go:76-90`,
  `<components>/types`, `list_components`) carries `default_ports` or `ports_require_config`.
- **P2 — `composition` package.** `Validate(catalog, cfg)` and `Analyze(declarations)`: pure, deterministic, no I/O.
  Thirteen exported finding types; severities in one place. Consolidates `engine/validator.go:300-610`
  (`convertAnalysisToResult`, `extractNodePorts`, `extractDiscoveredConnections`, `validateInterfaceContracts`,
  `areInterfacesCompatible`) and the HTTP handler's stream-warning status (`component_manager_http.go:677-683`) into
  the one interpreter. `flowgraph` gains `BuildFromDeclarations` beside `BuildFromRegistry`
  (`component/flowgraph/flowgraph.go:127-143`) so the offline and the admitted paths build the same graph.
- **P6 — projection.** `composition.Graph` (nodes with resolved ports, edges) is the result's `graph`;
  `composition.Mermaid` renders it. `<components>/flowgraph` serves it (JSON, `format=mermaid`).
- **P3 — verbs.** Exported `composition/cli.Main(args, registry, stdout, stderr) int` serving `catalog`,
  `validate <config>`, `graph <config> [--mermaid]`; `cmd/semstreams` wires it before flag parsing and makes
  `--validate` call the same function. Products call `cli.Main` from their own `main` (semsource precedent:
  `~/Code/c360/semsource/cmd/semsource/main.go:177-185` owns a `validate` verb; `internal/bootstrapobservability`
  is not importable, `cmd/semstreams/main.go:29`).
- **P4 — `composition.AssertValid(t, catalog, cfg)`** for product CI.
- **P5 — boot check.** `ComponentManager.Initialize` (`service/component_manager.go:229-335`) runs `Analyze` over
  the admitted declarations before `SealComposition` (`:330`), logs every finding, refuses boot on an error, retains
  the result; `<components>/validate` serves it verbatim.
- **P7 — tools.** Under the existing `component_catalog` gate (`processor/agentic-tools/executors/register.go:204`):
  `list_components` gains ports; new read-only `validate_composition` and `composition_graph`. No NATS, no payload.
- **`GET <components>/gaps` removed (owner round, 2026-08-26).** ComponentManager's own second judgment — it
  re-derived the connection graph through `ValidateFlowConnectivity` and applied its own severity table, reporting an
  input declared `external: true` as `no_publishers, required=true, critical_port_count=1, has_issues=true` while the
  canonical validator raised nothing. Removed without an alias, together with the Go surface only it reached
  (`ValidateFlowConnectivity`, `DetectObjectStoreGaps`, `ComponentGap`) and its OpenAPI row. Owner ruling: "we do not
  need to maintain legacy paths — we break it and document it for migration by downstream at this stage."
  `GET <components>/paths` is retained: it derives no severity, so it is a projection and not a judgment.
- **Removal (last) — NOT this change.** The owner's two-PR split (design §7 item 5) puts the diagram surface in #1093;
  its target state is `openspec/changes/flow-authoring-retirement/`. The list below is what that change performs:
  `flowstore/`, `flowtemplate/`, `engine/`, `service/flow_service.go`,
  `service/flow_runtime_{health,messages,metrics}.go`, `executors/{flows,flow_templates,register_flows,register_flow_templates}.go`,
  the `flow-builder` registration (`service/register.go:15`), `configs/protocol-flow.json:39-42`, the
  `cmd/semstreams` and `cmd/e2e-semstreams` wiring, `test/e2e/client/observability.go:80-114`, the OpenAPI rows,
  `openspec/specs/flow-authoring` (11 requirements REMOVED), `component-runtime-config`'s publication requirement
  (REMOVED), `docs/concepts/12-flow-architecture.md` and `docs/operations/migration-boot-only-flow-activation.md`
  (superseded by a new migration document). `service/stream_override_expiry.go` is rehomed to a retained service
  before `FlowService` is deleted (`service/flow_service.go:560-585` is its only host). The `flow-authoring` and
  `component-runtime-config` REMOVED deltas moved to that change with these tasks.

## Non-goals

- No next-boot component-configuration write verb (HTTP or tool). `config.Manager.PutComponentToKV`
  (`config/manager.go:684`) keeps zero production callers after removal; whether a write verb returns is an owner
  ruling (design §7, item 1), not this change.
- No `POST <components>/validate` accepting a draft body. `validate_composition` and the CLI cover drafts; an HTTP
  draft-validate has zero present consumers after the UI loses the Flow shape.
- No unification of the two port-override policies (merge vs replace, inventory §2.3): the declarer reproduces each
  factory's current policy; changing a factory's policy changes its config contract and is separate work.
- No compatibility Flow view, no legacy reader, no bucket migration (pre-v1 fresh-state policy). Retained
  `semstreams_flows` / `FLOW_TEMPLATES` buckets are inert.
- No change to `persona/`, rule CRUD tools, `read_loop_result`, `decide`, or the coordinator persona.
- No canvas positions: layout is the viewer's job.
- The e2e client's gateway filter in `CheckFlowHealth` (`test/e2e/client/observability.go:378-392`) is test code
  interpreting findings; it may collapse onto the library's severities in a follow-up.

## Impact

- **Consumers of the capability**: every product binary (semsource, semteams, semconnect, semdev, semboids,
  semmachina, semops, semdragon) gains `catalog`/`validate`/`graph` through `cli.Main` and `AssertValid`; none
  consumes them today (inventory §6).
- **BREAKING, this change**: `GET <components>/gaps` is gone with no alias, and `component.Registration` requires a
  `Ports` declarer, so an out-of-tree factory does not register until it declares. `/gaps` had no hand-written consumer
  in any local sister checkout on 2026-08-26 (measured: every hit is a generated OpenAPI type); the migration entry is
  the "`GET <components>/gaps` is removed" section of `docs/operations/migration-beta162-to-beta163.md`. Also removed:
  the exported `flowgraph.SubjectMatches`, which had no consumer at all.
- **BREAKING, #1093** (recorded here because ADR-100 is one decision): semstreams-ui (`/flowbuilder/*` at 15 call
  sites in 8 files; its e2e global setup reaps flows through the API), semteams (`cmd/semteams/main.go:24-26,595`
  imports; flow-template seeding `:627-640`; admin inventory and MCP validate), generated TS types in
  semspec/semdragon (residue). Migration document `docs/operations/migration-composition-validation-adr100.md` is part
  of that removal commit.
- **E2E**: this change's covering tiers are `task e2e:core` (boot path; every tiered `Setup` calls
  `<components>/validate`, `test/e2e/scenarios/tiered.go:187` — and every ComponentManager route with it) and
  `task e2e:agentic` (the only tier that boots an `External: true` port through a real boot with the refuse live).
  `task e2e:crud-tools` gates #1093, where the tool registry loses the flow gates.
- **Exported surface** added on `component` (`Registration.Ports`, `Declaration`), `component/flowgraph`
  (`BuildFromDeclarations`), and the new `composition` package — flagged for owner design review per the architect
  contract.
- **Milestone**: #1008, #1060, #1087 are ruled out; PR #1088 closes unmerged; this change claims #1089.
