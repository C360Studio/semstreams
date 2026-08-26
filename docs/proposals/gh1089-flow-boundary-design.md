# Flow authoring boundary — design for #1089 (option C)

## Checkpoint and status

- Baseline: `5cc0c7fbe569c6398fc534025218639b4c7e0345` (`main`).
- Inventory: `docs/proposals/gh1089-flow-boundary-inventory.md`, SHA-256
  `1a2c1d8e45b43a4247421e7301bf270e9c77f46e9ae368ca4def23d9a9e3178d`. **Review state: `INVENTORY PASS WITH DIVERGENCES`
  (independent blind re-derivation, Fable, 2026-08-26; table of record linked from PR #1091). Every load-bearing number
  reproduced; the two divergences (ten OpenAPI operations, not twelve; semteams already broken on `main` for non-flow
  reasons) and the line-number mis-cites are corrected in this revision.** The design was drafted in the same pass as
  the inventory at the caller's direction.
- Status: **DRAFT — awaiting independent pre-owner design review and the owner's ruling on #1089.** Nothing here is
  approved. Binding rulings stay with the owner.
- Companion artifacts: `docs/adr/100-compositions-are-validated-diagrams-are-projections.md` (Proposed),
  `openspec/changes/composition-validation-substrate/` (proposal, tasks, conformance, three spec deltas;
  `openspec validate composition-validation-substrate --strict` output is in the handoff).

## 1. The composition model

The unit of composition is the boot configuration composed with the binary's catalog: `config.Config{Platform,
Services, Components}` (`config/config.go:46-52`) plus the factories the binary registers
(`cmd/semstreams/main.go:489-516`; semsource `cmd/semsource/run.go:148,264`). Connections are never authored: they are
derived from port declarations by subject/bucket/address overlap (`flowgraph.ConnectComponentsByPatterns`,
`component/flowgraph/flowgraph.go:216`). A diagram is a projection of a composition — from a file (offline) or from
the admitted Registry declarations (running, `flowgraph.BuildFromRegistry`, `:127-143`) — never a stored thing.

Two evidence classes, one vocabulary:

| | Offline (`validate <config>`, `AssertValid`, `validate_composition`) | At boot (`ComponentManager.Initialize`) and `<components>/validate` |
|---|---|---|
| Input | catalog + `config.Config` | admitted `component.Declaration`s (`Registry.Snapshots`) |
| Ports come from | each factory's pure declarer (P1) | the constructed component, verified equal to the declarer at admission (P1 parity) |
| What it is | a **prediction** of the next boot's shape | an **observation** at the real boundary |
| Can be wrong about | a declarer that drifts from its constructor (caught at boot by parity), a config the boot arbitration will override from KV desired state (ADR-094) | nothing the composition itself controls |
| Interpreter | `composition.Validate` → `composition.Analyze` | `composition.Analyze` |

"Prefer observation to prediction" applied: the offline path exists for CI and agents, but the framework does not
trust it — boot re-derives the same findings from what was actually admitted and refuses on error (P5). The parity
check turns the one thing the offline path predicts (ports) into something the framework observes.

## 2. Primitives

Adopter seam rows are answered for a developer outside this repo writing a component or a product binary who has
never opened `component/registry.go`.

### P1 — static port declarations on the registration

- **Contract.** `type PortDeclarer func(rawConfig json.RawMessage, instanceName string) (component.PortConfig, error)`;
  `Registration.Ports PortDeclarer` and `RegistrationConfig.Ports`; `RegisterFactory` rejects nil `Ports`
  (`component/registry.go:139-147` rejects nil `Factory` the same way). `Registry.Declare(factory, cfg, instance)
  (component.Declaration, error)` resolves the definitions through `resolveAndProjectPort`
  (`component/port_resolver.go:16`) — the single interpreter of a port declaration — and returns the value type the
  Registry already admits (`componentDeclaration`, `registry.go:76-84`, exported as `Declaration`). At admission,
  `prepareComponent` (`:209-273`) compares `Declare`'s output with `captureComponentDeclaration`'s (`:564-590`) port
  by port (name, direction, required, kind, resource id, subjects, interface); a difference fails admission naming
  factory, instance, and port.
- **Seam.** `component/registry.go:52-62` (the field), `:139-147` (nil rejection), `:268-272` (after capture);
  the 33 factories (inventory §2.3, file:line per row) each expose their existing derivation as the declarer and
  call it from the constructor; `cmd/openapi-generator/main.go:76-90` exports `default_ports`;
  `service/component_manager_http.go:417-500` and `component_catalog_executor.go:30-60` read it.
- **Replaces.** The engine's construct-to-discover (`engine/validator.go:236-275`) and its throwaway registry
  (`:277-298`).
- **Why a function, not a constant.** Measured: 32/33 factories' ports depend on config (6 merge, 21 replace, 5 derive
  from non-port fields or the instance name); 1/33 is constant. A constant `DefaultPorts` on `Registration` plus a
  framework merge would cover the 27 merge/replace factories only if their override policy were unified (it is not:
  `MergePortConfig` rejects unknown override names, `ports.go:190-193`, while the replace factories accept any set),
  and would fail the 5 derived ones (file paths, gated-dag stream fields, lifecycle-gateway's canonical append,
  objectstore's `store-provide`). The function absorbs all of that per factory with no new policy.
- **Why not construct with null dependencies.** 25/33 factories refuse nil deps and the Registry refuses first
  (`registry.go:228`); those guards are deliberate fail-fast boot checks; relaxing them would make offline validation
  construct real components (buffers, metrics registration) — side effects in a "pure" verb.
- **Adopter seam.** *Must know:* write one function returning the ports your `DefaultConfig` already declares, and
  call it from your constructor. *Do nothing:* `RegisterFactory` fails at the product's first boot with the factory
  name and the one-line fix — loud, early, mechanical (boot error, rank 2). *Find out:* the boot error; the
  `component` package doc. *Should know:* nothing beyond the declaration they already write; the parity check means
  they cannot get the declaration silently wrong. The remaining bill — a second function — is recorded as the gap; it
  disappears only if `Discoverable.InputPorts/OutputPorts` are retired in favor of the declarer (Foundation-C-shaped,
  out of scope).
- **Tests.** `TestRegisterFactoryRejectsNilPortDeclarer`, `TestAdmissionRefusesPortDeclarationMismatch`,
  `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory` (integration, 33 rows asserted against
  `len(ListFactories())`), `TestCatalogCarriesDefaultPortsOrRequiresConfig`, `TestSchemaExportCarriesDefaultPorts`.

### P2 — `composition` package: pure validator, one vocabulary

- **Contract.** `composition.Validate(catalog *component.Registry, cfg *config.Config) (*Result, error)` (error only
  for nil arguments) and `composition.Analyze(decls []component.Declaration) *Result`. Steps of `Validate`:
  `cfg.Validate()` → `config_invalid`; per enabled component in instance order: factory lookup → `unknown_component`;
  type mismatch (`registry.go:249-253`) → `component_type_mismatch`; `ComponentConfig.Validate`
  (`types/component.go:98-123`) → `component_config_invalid`; declarer → `port_declaration_error`; exclusive-resource
  collision (`registry.go:547`) → `exclusive_resource_conflict`; then `Analyze` over the declarations:
  `flowgraph.BuildFromDeclarations` (new, beside `BuildFromRegistry`) → `connection_pattern_error` (one finding, the
  Slice C ruling-6 shape); `AnalyzeConnectivity` (`flowgraph.go:714`) → `disconnected_node` (warning),
  `orphaned_port` (error iff required stream input with no publishers, else warning — the rule now at
  `engine/validator.go:313-361`, moved); `ValidateStreamRequirements` (`:955`) → `stream_requirement` (error — today
  "critical", `component_manager_http.go:677-683`); interface contracts over derived edges → `interface_mismatch`
  (error), `missing_interface` (warning) (moved from `validator.go:491-610`; exact-match rule `:612-623` preserved);
  zero enabled components → `empty_composition` (warning — a services-only process boots today,
  `component_manager.go:241-245`; the engine's `empty_flow` error was a diagram rule). `Result{Status, Errors,
  Warnings, Graph}`; `Finding{Type, Severity, Component, Port, Message, Suggestions}`; all arrays non-nil; JSON
  byte-stable.
- **Seam.** New package `composition/`; `component/flowgraph/flowgraph.go:127-143` gains `BuildFromDeclarations`;
  `flowgraph.go:748-770` (status derivation inside `AnalyzeConnectivity`) is left as-is but no consumer reads it
  after P5 — file its retirement.
- **Replaces.** `engine/validator.go:300-623` (moved), the HTTP handler's own status logic
  (`component_manager_http.go:677-716`), the e2e client's re-interpretation (`observability.go:366-400`, follow-up).
- **Slice C1 survivors.** Of the twenty types in PR #1088's delta, the eight graph types survive with two renames
  (`empty_flow`→`empty_composition`, `graph_build_error`→`port_declaration_error`) and five additions
  (`config_invalid`, `component_type_mismatch`, `component_config_invalid`, `exclusive_resource_conflict`,
  `stream_requirement`); the twelve structural Flow types die with the Flow (a `components` map cannot duplicate a
  key; `node_component_required`/`node_type_required` become `component_config_invalid`). The `ValidationIssue`
  shape (stable `type`, non-empty identity, non-nil `suggestions`) and the non-null-arrays rule carry over unchanged.
- **Adopter seam.** *Must know:* nothing — call one function. *Do nothing:* boot (P5) runs it anyway. *Find out:*
  the returned findings (typed, rank 3). *Should know:* nothing. The one judgment an adopter could disagree with —
  severity — lives in one table they can read.
- **Decision skills.** `orchestration-check`: this is neither a rule nor a lifecycle participant — a pure library
  function called by a component-manager service at boot and by verbs; no orchestration primitive is added.
  `kv-or-stream` / `entity-or-bucket`: no new durable state (two buckets are removed). `new-payload`: no new message
  type. `query-pattern`: not a graph query; the HTTP faces are admitted operations on the component-manager service,
  as `<components>/validate` already is.
- **Tests.** The eight named in the spec; plus `TestValidateShippedConfigsHaveNoErrorFindings` (over every
  `configs/**/*.json`, docker and e2e configs) and the dropped-step detector
  `TestValidateMatchesEngineFindingsForShippedConfigs` (engine vs `composition` on the same configs, integration,
  deleted with the engine).

### P3 — verbs shipped with every binary

- **Contract.** `composition/cli.Main(args []string, registry *component.Registry, stdout, stderr io.Writer) int`
  serving `catalog`, `validate <config-path>`, `graph <config-path> [--mermaid]`; `validate` prints findings and
  returns non-zero on errors; no NATS. `cmd/semstreams` dispatches to it when `os.Args[1]` is a verb (before
  `parseCLI`, `main.go:86`) and makes `--validate` (`flags.go:71`; `main.go:112-115`) print the same findings.
- **Seam.** `cmd/semstreams/main.go:85-115`; `cmd/e2e-semstreams/main.go`; products' `main` (semsource already owns
  a `validate` verb, `cmd/semsource/main.go:177-185`, which can delegate).
- **Why exported, not a `cmd/semstreams` feature.** `internal/bootstrapobservability` is not importable
  (`main.go:29`); products replicate boot; "shipped with every binary" therefore means one exported call.
- **Adopter seam.** *Must know:* add three lines to `main` (`if code, ok := cli.Dispatch(os.Args[1:], registry,
  os.Stdout, os.Stderr); ok { os.Exit(code) }`). *Do nothing:* the product has no verbs, and boot (P5) still validates.
  *Find out:* the migration document and `composition` package doc (doc rank — acceptable because doing nothing is
  safe). *Should know:* nothing; the gap is the three lines.
- **Tests.** `TestCLIValidateExitsNonZeroOnErrorFindings`, `TestCLICatalogPrintsEveryRegisteredFactory`,
  `TestCLIGraphMermaidRendersEveryEdge`, `TestValidateFlagReportsCompositionFindings`.

### P4 — `composition.AssertValid(t testing.TB, catalog, cfg)`

- **Contract.** Fails with every error finding printed; passes on warnings. Precedent for a `testing.TB`-taking
  framework helper: `natsclient.NewTestClient(t, ...)`.
- **Adopter seam.** *Must know:* one call in a product test over each shipped config. *Do nothing:* boot still
  refuses (P5); CI does not. *Find out:* doc. *Should know:* nothing.
- **Test.** `TestAssertValidFailsOnErrorFinding`.

### P5 — boot-time validation at the real boundary

- **Contract.** After the fixed set is created and before `SealComposition` (`service/component_manager.go:330`),
  `Initialize` runs `composition.Analyze(registry.Snapshots(access))`, logs each finding, retains the result, and
  returns an error when the result has errors (fail-closed, consistent with
  `openspec/specs/framework-composition/spec.md:152`). `<components>/validate` (`component_manager_http.go:657-716`)
  serves the retained result verbatim; `<components>/flowgraph` (`:618-655`) serves `result.Graph` (JSON, or Mermaid
  with `format=mermaid`).
- **Seam.** `component_manager.go:229-335` (`Initialize`), `:1423-1500` (cache — becomes the retained result),
  `component_manager_http.go:618-716`.
- **Replaces.** On-request-only analysis; the handler's private status logic.
- **Observation vs prediction.** This is the observation. The parity check in P1 is what lets P2's prediction be
  trusted between boots; P5 is what makes a wrong prediction impossible to ship.
- **Risk measured.** Whether any shipped composition carries an error-severity finding today is unmeasured
  (inventory §12.1) — no offline validator exists to measure it. Every tiered e2e Setup already fails on stream
  warnings and on disconnected non-gateway nodes (`observability.go:366-400`), so the e2e compose configs are
  known-clean for that class; `configs/*.json` are not. Sequencing (§5) lands P3 before P5 so the measurement is a
  command, and the refuse flips only after `TestValidateShippedConfigsHaveNoErrorFindings` is green.
- **Adopter seam.** *Must know:* nothing. *Do nothing:* a broken composition refuses to boot with a typed finding
  (boot error, rank 2) instead of running with a silently unfed consumer. *Find out:* the boot log and
  `<components>/validate`. *Should know:* nothing.
- **Tests.** `TestComponentManagerRefusesBootOnErrorFinding` (integration),
  `TestComponentManagerExposesBootFindings`, `TestFlowValidationHandlerProjectsLibraryResult`.

### P6 — graph projection (JSON + Mermaid)

- **Contract.** `composition.Graph{Nodes []Node{Instance, Factory, Type, Inputs, Outputs []PortView}, Edges
  []Edge{From, To, Pattern, ConnectionID}}` is `Result.Graph`; `composition.Mermaid(Graph) string` renders
  `flowchart LR` with one node per component and one edge per derived connection, sorted. `PortView` is today's
  `ValidatedPort` (`engine/validator.go:56-64`) plus `kind`.
- **Seam.** `composition/graph.go`, `mermaid.go`; `component_manager_http.go:618-655` (`?format=mermaid`); CLI
  `graph`.
- **Replaces.** `engine.ValidationResult.Nodes/DiscoveredConnections` (`validator.go:389-489`),
  `flowstore.FromComponentConfigs` (`flowstore/converter.go:26-83`; the grid layout `:85-101` dies — layout is the
  viewer's), the saved `Flow` (`flowstore/flow.go:12-55`), and the current `/flowgraph` shape (a map of nodes plus
  edges; consumers measured: none).
- **What it gives back.** semstreams-ui: a read-only picture of what is running (nodes with ports, edges) and, via
  the CLI or the tool, of any config file; semteams' admin inventory: the running composition instead of a list of
  saved diagrams.
- **Tests.** `TestGraphProjectionMatchesAdmittedComposition` (integration), `TestMermaidIsDeterministic`,
  `TestCLIGraphMermaidRendersEveryEdge`.

### P7 — agent tools

- **Contract.** Under the existing `component_catalog` gate (`processor/agentic-tools/executors/register.go:204`,
  requires only `deps.ComponentRegistry`): `list_components` (kept name; gains `default_ports`), new
  `validate_composition` (input: a configuration document as a JSON object; output: `composition.Result`), new
  `composition_graph` (input: document + `format` `json|mermaid`). All `ToolEffectReadOnly`; no NATS; no new payload.
- **Seam.** `component_catalog_executor.go:30-60` (extend), new `executors/composition_tools.go`,
  `register.go:204`; `docs/operations/adopter-tool-effect-metadata.md:130` rows.
- **Replaces.** The eleven flow and flow-template tools (`executors/flows.go`, `flow_templates.go`).
- **Writes.** PREMISE PARTIALLY FAILED (inventory §10.10): after the publication route goes,
  `config.Manager.PutComponentToKV` (`config/manager.go:684`) has zero callers. This design adds no write tool: the
  composition is the product's configuration, and an agent that wants to change it edits that document through the
  product's own tools and restarts. Whether the framework should offer a next-boot upsert verb is owner item §7.1.
- **Adopter seam.** *Must know:* nothing (the gate already exists; personas allow-list by name). *Do nothing:*
  personas that allow-listed `create_flow` etc. get "unknown tool" from the registry — loud (typed runtime error,
  rank 3); the migration document lists the names. *Should know:* nothing.
- **Tests.** `TestValidateCompositionToolReturnsFindings`, `TestCompositionGraphToolReturnsMermaid`,
  `TestListComponentsCarriesPorts`, `TestToolRegistryHasNoFlowTools`.

### Removal

- Packages `flowstore/`, `flowtemplate/`, `engine/`; `service/flow_service.go`, `service/flow_runtime_*.go`;
  four executor files; `service/register.go:15`; `configs/protocol-flow.json:39-42`; `cmd/*` wiring;
  `test/e2e/client/observability.go:80-114`; OpenAPI rows (regenerated); `openspec/specs/flow-authoring` (11 REMOVED),
  `component-runtime-config:350-368` (REMOVED); docs `concepts/12-flow-architecture.md`,
  `operations/migration-boot-only-flow-activation.md` (superseded by
  `operations/migration-composition-validation-adr100.md`); `adopter-tool-effect-metadata.md:130-136` rows;
  `schemas/workflow-definition.v1.json` (stale). Buckets `semstreams_flows`, `FLOW_TEMPLATES` no longer created;
  retained ones are inert (fresh-state policy; no migration).
- **What the removed guard was holding.** `service/stream_override_expiry.go` (130 lines, stream-provisioning's
  override-expiry metric) is constructed and registered only by `FlowService` (`flow_service.go:560-585`). It is
  rehomed (ComponentManager or the metrics service — recorded in tasks 3.8) *before* the service is deleted, with
  `TestStreamOverrideExpiryReporterRegistersWithoutFlowService` as the dropped-step detector.
- Totals: REMOVED ≈ 4,060 production (3,916 across flowstore, flowtemplate, engine, service/flow_*, executors; ≈145 wiring, e2e client, config) / 5,550 test lines; KEPT `component/flowgraph` 1,245 / 1,932 and the
  seams listed in inventory §4; RESHAPED ≈ 330 lines of `engine/validator.go` logic, `ensureDefaultFlowFromConfig`'s
  derivation, the catalog tool, and the two ComponentManager HTTP handlers.

## 3. Options

| Option | What it is | Cost | Outcome |
|---|---|---|---|
| **A/B — baseline** (owner already rejected) | Finish Slices C, D, #1087 on the CRUD/HTTP layer; keep flowstore, engine, tools | ≈3–4k more lines on the diagram surface; `--validate` and boot still check no connections; two interpreters remain; semstreams-ui candidate gate stays a tag blocker | polished authoring of a document the framework compiles for a product to reboot into |
| **C — as stated** | P1–P7 substrate; retire the diagram surface; no store, no write verb, no alias | 33 factory declarers (mechanical); one new package; BREAKING for semstreams-ui; marginal for semteams, which already fails to compile against `main` for two non-flow reasons (inventory §9); two buckets orphaned | one validator, two evidence classes, boot refuses broken compositions; products validate in CI with one call |
| **C-minus** | C plus a read-only `GET /flows` that serves the running composition as a `flowstore.Flow` document (nodes, connections, positions) so semstreams-ui's list/detail pages keep loading | keeps `flowstore.Flow` alive as a wire type and the grid layout; a legacy-reader shape the pre-v1 policy forbids; the UI's save/publish/observations still break, so its owner rewrites anyway | delays the UI's rewrite by one page |
| **C-plus** | C plus `POST <components>/validate` accepting a draft configuration body | one handler over `composition.Validate`; zero present consumers once the UI loses the Flow shape (phantom-surface rule) | an HTTP draft validator nobody calls yet |
| **Do nothing** | keep the surface as-is, pause #1008/#1060/#1087 | the boot composition stays unvalidated; the diagram surface keeps its bill | — |

## 4. Recommendation

**C as stated.** Grounding sentence: every one of the 33 factories already computes its ports as a pure function of
its configuration and the framework already derives stream declarations from configured ports without constructing
anything, so the only thing standing between today and an offline validator is putting the factory's default
declaration on `Registration` — after which the saved-diagram store, its compiler, and its eleven tools are a second
home for a fact (the composition) that the product's configuration file already owns. C-minus and C-plus each add a
surface with no measured consumer; A/B polishes a surface with a weakened premise.

## 5. Sequencing and BREAKING assessment

| Step | Content | BREAKING? | E2E before landing |
|---|---|---|---|
| 1 | P1 declarers on all 33 factories + parity at admission + catalog export | exported-surface addition on `component` (owner design review); adopter components must add a declarer (nil rejected) — BREAKING for custom components | `task e2e:core` (boot path admits every shipped factory) |
| 2 | P2 `composition` + P6 projection + P4 helper + engine-parity detector | additive | — |
| 3 | P3 verbs (`cli.Main`, `--validate` extended) | additive | — |
| 4 | Measure every shipped config with the verb; fix or file | — | — |
| 5 | P5 boot check, refuse on error; HTTP handlers become projections | BREAKING for any deployed config with an error finding (measured in step 4) and for consumers of the old `/flowgraph` shape (none measured) | `task e2e:core`, `task e2e:agentic` |
| 6 | P7 tools | additive | `task e2e:crud-tools` (registry boots with the gate) |
| 7 | Rehome the override-expiry reporter, then removal + migration doc + spec sync | BREAKING (routes, tools, packages, buckets) | `task e2e:core`, `task e2e:crud-tools`, `task e2e:agentic` |

Removal goes **last**: the engine is the only oracle for "did the move drop a step" (the parity detector in step 2
needs both alive), and the boot check must exist before the only other validator disappears. One OpenSpec change
carries the whole target state as asked; owner item §7.5 asks whether to land it as one PR or as two (substrate,
then retirement) — if two, the REMOVED deltas move to the second change and the first is not BREAKING except for
step 1's declarer requirement.

## 6. Milestone consequence

- **#1008** (Slice C, invalid handling on flow CRUD) — ruled out: every route it fixes is removed. PR #1088 closes
  unmerged. Its C1 vocabulary survives as described in P2.
- **#1060** (Slice D, six Get consumers → 404) — ruled out: all six consumers are removed.
- **#1087** (four combined-candidate e2e scenarios + crud-tools empty extension + the semstreams-ui downstream
  gate) — ruled out with **no surviving target**: `flow-authoring-http-contract`, `flow-list-current-state`, and
  `flow-get-corrupt-projection` assert on removed routes; `flow-crud-tools-empty` asserts `list_flows`; the downstream
  gate becomes a migration document, not a tag gate. The e2e obligation this change carries instead is step 7's three
  tiers.
- Slices A (#1009) and B (#1010) landed code that is deleted with `flowstore` (their CAS and List semantics); the
  discipline they established (typed absence, never message text; revision-fenced writes) stays in `natsclient` and
  `nats-kv-keys`.
- The milestone's remaining membership is this change (claims #1089). Whether beta.162 waits for it is the owner's
  membership ruling.

## 7. Open items for the owner (decisions only the owner can make)

1. **Next-boot write verb.** After removal, `config.Manager.PutComponentToKV` has zero callers. Options: (a) none —
   the composition is the product's config file (recommended: the framework owns no authoring store, and a KV write
   lane is a second home for the composition); (b) a minimal `PUT <components>/config/{name}` next-boot upsert (what
   ADR-094 retired as "generic live PUT", reintroduced as explicitly next-boot). Recommend (a); file (b) if a product
   asks.
2. **Nil declarer: reject or warn.** Reject at `RegisterFactory` (recommended: a silently undeclared factory makes
   every `disconnected_node` finding for its instances wrong — a silent-loss shape; the fix is one function) vs admit
   with `ports_require_config` and a `port_declaration_error` finding per instance.
3. **Boot refuse vs report.** Refuse on error (recommended; the e2e tiers already fail on the same class, and step 4
   measures the shipped configs first) vs report-only for one tag.
4. **Tool naming.** Keep `list_components` (recommended: personas allow-list by name; the issue's `catalog` would
   break every allow-list for a rename) vs rename to `catalog`. New names `validate_composition` and
   `composition_graph` are proposed; `graph` alone collides with graph-query vocabulary.
5. **One PR or two.** One change as drafted (removal last) vs substrate PR then retirement PR. Recommend two if the
   owner wants beta.162 to ship the substrate before the UI owners have migrated; one otherwise.
6. **`--validate` flag.** Keep as an alias of `validate <config>` (recommended; `docs/concepts/32-agent-memory.md:226`
   uses it) vs retire for one spelling.
7. **`empty_composition` severity.** Warning as drafted (a services-only process boots today) vs error (the engine's
   `empty_flow` rule).
8. **Exported surface review.** `component.Registration.Ports`, `component.Declaration`, `Registry.Declare`,
   `flowgraph.BuildFromDeclarations`, and the `composition` package are new exported framework surface — the architect
   contract requires owner design review before implementation.
9. **PR #1088 disposition** and the `status:needs-decision` labels on #1008; the milestone membership of #1060 and
   #1087.

### PREMISE FAILED (against #1089's framing; each measured in the inventory §10)

34 schema files → 33 factories (`workflow-definition.v1.json` is stale) · "instance-only" ports → pure functions of
config for 33/33 (0 runtime-only) · "nothing validates the boot composition" → `config.Validate` already validates
components, port-derived streams, and the model registry; boot runs rule-pack and capability gates; `/components/validate`
analyzes the admitted composition on request · "connection validation exists only behind a saved Flow" → the
`flowgraph` analysis exists for the running composition; only interface and unknown-component findings are
engine-only · "131 test lines against 1,273 production" → engine 837/131; `flowgraph` 1,245/1,932 · "five `*_flow`
executors" → eleven tools across two executors · "writes go through the existing Config Manager path" → that path's
only caller is the removed publication · "semteams' already-broken `engine` import" → compiles at beta.160 and
beta.161; broken only on unreleased main — and there for two non-flow reasons as well (`InitializeKVStore`/`StopAll`
now take `ctx`), so C's cost to semteams is marginal · "exported by the product's own binary" → needs an exported entry point;
`internal/bootstrapobservability` is not importable.

## 8. Decision skills applied

- `orchestration-check` — P5 is a boot-time library call inside the component-manager service; no rule, lifecycle
  participant, or workflow primitive is added or bypassed.
- `kv-or-stream`, `entity-or-bucket` — not triggered: no new durable state or communication path; two KV buckets are
  removed.
- `new-payload` — not triggered: tool results are `agentic.ToolResult` content; no new message type.
- `query-pattern` — not a graph query; the HTTP faces are admitted operations on the component-manager service,
  consistent with the existing `<components>/validate`.
