# Proposal — flow-authoring-retirement

Target state for #1093, the retirement half of the owner's two-PR split on #1089 (design §7 item 5). ADR-100 is
Accepted (`docs/adr/100-compositions-are-validated-diagrams-are-projections.md`); its decision D5 ("Retirement without
aliases") is what this change performs. The substrate half — static port declarations, the `composition` package, the
verbs, `AssertValid`, the boot check, the projection, the tools — landed as `composition-validation-substrate` in
PR #1101 (#1092). Every `file:line` below was measured at `5cc0c7fb` in
`docs/proposals/gh1089-flow-boundary-inventory.md` and re-measured against the substrate PR's head where the substrate
moved a line; re-measure at the claim head before coding.

## Why

The substrate PR built the one validator and left the diagram surface standing beside it. Until it leaves, the
repository ships two answers to "is this composition wired": the canonical `composition` library, and the saved-diagram
path (`engine/validator.go`) that constructs every node through the Registry with a live NATS client because port
values are read from constructed components. The second answer is what ADR-100 D3 refuses, and the second answer is
also what pays the 4.1k production / 5.6k test lines, two KV buckets, eight OpenAPI operations, and eleven agent tools
the owner has written off.

`GET <components>/gaps` — ComponentManager's own second judgment, which reported an input declared `external: true` as
a critical orphan while the canonical validator raised nothing — was already removed in PR #1101, because it
contradicted the substrate the same PR shipped. This change removes the rest.

## What Changes

- **Rehome first.** `service/stream_override_expiry.go` (constructor + `RegisterMetrics`) is hosted by
  `service/flow_service.go:560-585` and by nothing else. It moves to a retained service before `FlowService` is
  deleted, so the override-expiry metric survives the removal.
- **Delete, without aliases**: `flowstore/`, `flowtemplate/`, `engine/` (and the substrate PR's
  `composition/engine_parity_integration_test.go`, whose oracle is the engine), `service/flow_service.go`,
  `service/flow_runtime_{health,messages,metrics}.go` and their tests,
  `processor/agentic-tools/executors/{flows,flow_templates,register_flows,register_flow_templates}.go` and their
  tests, the `flow-builder` registration (`service/register.go:15`), `ToolDependencies.FlowManager` /
  `FlowTemplateManager` and their two tool gates, `configs/protocol-flow.json:39-42`, the `cmd/semstreams` and
  `cmd/e2e-semstreams` wiring, `test/e2e/client/observability.go:80-114`, the `/flowbuilder/*` OpenAPI rows and the
  `Flow*` schemas, `docs/concepts/12-flow-architecture.md`, and
  `docs/operations/migration-boot-only-flow-activation.md`.
- **Buckets**: the framework creates no `semstreams_flows` or `FLOW_TEMPLATES` bucket. Retained deployed buckets are
  inert; pre-v1 fresh-state policy means no migration, no legacy reader, no compatibility Flow view.
- **Spec deltas**: `flow-authoring` loses all eleven requirements (the capability is retired);
  `component-runtime-config` loses its Flow-publication requirement; `composition-validation` gains "The framework owns
  no composition authoring store" with the four absence guards.
- **Migration**: `docs/operations/migration-composition-validation-adr100.md` — removed routes, tools, packages,
  buckets; per-repo instructions for semstreams-ui and semteams from inventory §9. The `/gaps` removal already has its
  section in `docs/operations/migration-beta162-to-beta163.md`; this document is the wider surface.

## Non-goals

- No next-boot component-configuration write verb (HTTP or tool). After the removal `config.Manager.PutComponentToKV`
  (`config/manager.go:684`) has zero production callers; whether a write verb returns is an owner ruling (design §7
  item 1), not this change.
- No compatibility Flow view, no legacy reader, no bucket migration, no alias for any removed route or tool.
- No change to `persona/`, rule CRUD tools, `read_loop_result`, `decide`, or the coordinator persona.
- semstreams-ui and semteams migrations are their owners' work; sister repositories are read-only to SemStreams agents
  (owner ruling on #1100). Obligations are recorded in the migration document only.

## Impact

- **BREAKING**: semstreams-ui (`/flowbuilder/*` at 15 call sites in 8 files; its e2e global setup reaps flows through
  the API), semteams (`cmd/semteams/main.go:24-26,595` imports; flow-template seeding `:627-640`; admin inventory and
  MCP validate), generated TS types in semspec/semdragon (residue).
- **E2E before the removal commit lands** (ADR-100 Consequences, and the repository's BREAKING rule): `task e2e:core`,
  `task e2e:crud-tools` (the tool registry boots without the flow gates), and `task e2e:agentic` (the largest shipped
  composition through the boot check).
- **Retained by the substrate PR and re-judged here**: `flowgraph.AnalyzeConnectivity` keeps one production caller
  after `engine/` leaves — `composition.Analyze` — which is correct and stays. `ComponentManager.GetFlowGraph` and
  `GET <components>/paths` rebuild a graph from the admitted registry rather than serving the retained
  `composition.Result.Graph`; `/paths` derives no severity, so it is a projection and not a second judgment, but the
  duplicate build is this change's to resolve or to record.
