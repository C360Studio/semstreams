# ADR-100: Compositions Are Validated; Diagrams Are Projections

## Status

**Accepted (2026-08-26).** Owner ruling on #1089: "C, ADR-100 accepted." The nine design defaults in
`docs/proposals/gh1089-flow-boundary-design.md` §7 stood without override. Drafted 2026-08-26 from the architect package
(`docs/proposals/gh1089-flow-boundary-inventory.md`, `docs/proposals/gh1089-flow-boundary-design.md`). If accepted it
supersedes the named clauses of ADR-096 and ADR-026 below; ADR-094 and ADR-095 remain the authority for boot-only
composition and lifecycle mechanics. Mechanics live in `openspec/specs/composition-validation/spec.md` (new) and the
`flow-authoring` capability is retired; this page records only the decision.

## Context

Three passes (ADR-026 → ADR-094 → ADR-096) each shrank the visual Flow surface: live activation went, then lifecycle
authority went, leaving "save and validate a diagram, compile it, publish explicitly, reboot". The remaining
milestone items (#1008, #1060, #1087) are polish on the HTTP layer of a canvas editor whose premise has weakened, and
the one capability that matters — connection-level validation — exists twice: once behind a saved Flow
(`engine`, constructing every node with a live NATS client because port values are read from constructed components,
`component/registry.go:564-571`) and once over the running composition (`GET /components/validate`), with different
interpretations of the same analysis. Nothing runs either at boot, and nothing runs offline.

The inventory measured the chicken/egg: every one of the 33 registered factories computes its ports as a pure
function of its raw configuration (32 config-derived, 1 static, 0 runtime-only); 25 refuse construction without
dependencies and the Registry refuses first. Ports are already config-shaped (`component.PortConfig`, Foundation B)
and the framework already derives stream declarations from configured ports without constructing anything
(`config/stream_bounds.go:259-320`). What is missing is small: factory **default** ports are not static facts on
`Registration`, and no exported pure validator exists.

## Decision

1. **The unit of composition is the boot configuration plus the binary's catalog.** `config.Components` (with
   `platform` and `services`) composed with the factories the binary registers is the only authored artifact the
   framework recognizes. Connections are never authored; they are derived from port declarations. A diagram is a
   read-only projection of a composition — of a config file (offline) or of the admitted composition (running) —
   and is never stored by the framework.

2. **Port declarations are static facts of a factory.** A registration declares its ports as a pure function of raw
   configuration and instance name, the same way it declares `Schema`. The framework verifies that declaration
   against the constructed component at boot admission; a disagreement fails admission. Adopter components carry
   this declaration; the framework carries the check.

3. **One validator, one vocabulary, two evidence classes.** Composition validation is a pure Go library over the
   catalog and a configuration, with a stable findings vocabulary. The same findings are produced (a) offline, from
   declarations — a prediction of the next boot — and (b) at boot, from the admitted composition — an observation at
   the real boundary. The framework refuses to boot a composition whose observed findings include an error. The
   `engine`, the HTTP handler, and the e2e client stop re-interpreting the analysis; the library is the one home.

4. **Cross-repo contract: products get `catalog`, `validate`, and `graph`.** Every binary can expose the three verbs
   through one exported framework entry point (CLI), the ComponentManager service exposes the projection and the
   admitted-composition findings over HTTP, and the agentic substrate exposes them as read-only tools. The framework
   owns no authoring store, no diagram CRUD, no template store, and no next-boot write verb; writing a composition is
   writing the product's configuration.

5. **Retirement without aliases.** `flowstore`, `flowtemplate`, `engine`, the `flow-builder` service and its HTTP
   routes and observation routes, the eleven flow and flow-template agent tools, the `semstreams_flows` and
   `FLOW_TEMPLATES` buckets, and the `flow-authoring` capability leave the tree. Pre-v1 fresh-state policy applies:
   no migration, no legacy reader, no compatibility Flow view. Recovery of the removed mechanism is by git history
   plus this record.

## Consequences

Composition correctness moves from "after you saved a diagram and asked" to "before you boot, and at boot". A product
author validates a config in CI with one call and never predicts a subject, a bucket, or a connection. The framework
loses about 4.1k production lines (5.6k test lines) of diagram handling and keeps the 1.2k that analyze a graph.

Breaking, pre-v1: semstreams-ui's canvas editor, publish panel, and runtime-viz panels lose their backend; they get
back a read-only projection (JSON and Mermaid) of what is running and a validator for what would run. semteams loses
its `engine`/`flowstore`/`flowtemplate` imports and its Flow-JSON template seeding; its admin inventory page gets the
projection. Owners of sister repositories perform their own migrations from a SemStreams-owned migration document.
`task e2e:core`, `task e2e:crud-tools`, and `task e2e:agentic` must be green before the removal commit lands.

Milestone consequence: #1008, #1060, and #1087 are ruled out (their surfaces are removed); PR #1088 closes unmerged;
the eight graph finding types and the non-null `ValidationIssue` shape from its Slice C1 vocabulary survive into the
composition validator.

## Superseded clauses

- ADR-096: Decision paragraphs one to three and five ("`flowstore.Flow` contains…", "Engine validates and compiles…",
  "The only diagram-to-config write is explicit `POST /flows/{id}/publish-component-configs`…", "Retained Flow
  health, metrics, and message endpoints are saved-diagram observations…") and Consequences paragraph one ("The Flow
  builder remains useful for authoring…"). Paragraph four (ComponentManager reads configuration once; Registry seals)
  stands as ADR-094/095 substrate.
- ADR-026: "Can author durable Flow and Rule definitions" as it applies to Flow; §"Flow-composition tool executors"
  (`manage_flow`, `list_flow_templates`, and the historical `create_flow`/`update_flow`/`delete_flow`/`list_flows`/
  `get_flow` and flow-template tools); §"Composition model: flows, not composite components" insofar as it names
  `Flow` entities and `FlowEngine`. Coordinator judgment, `decide`, `read_loop_result`, rule authoring, and
  `list_components` stand.
- ADR-094 `:47-48` ("Flow create, update, validation, and persistence remain supported") and `:50-53` (flowstore
  activation record). Everything else in ADR-094 stands.
- ADR-029: the Pattern-B rows and plan for `FlowManager`/`FlowTemplateManager`. Rules and personas stand.
- ADR-027's reuse of "the same runtime composition tools" narrows to the read-only `catalog`/`validate`/`graph` set.

## References

- #1089 (owner direction, 2026-08-26); the architect package named in Status.
- ADR-026, ADR-029, ADR-094, ADR-095, ADR-096; ADR-061/ADR-099 (removal-and-recoverability pattern).
- `docs/proposals/foundation-b-port-language-design.md` (the config-shaped port grammar this decision relies on).
- `openspec/specs/component-discovery/spec.md`, `openspec/specs/stream-provisioning/spec.md:451-501` (the
  port-derived pure lane precedent).
