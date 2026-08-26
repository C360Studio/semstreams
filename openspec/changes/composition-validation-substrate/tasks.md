# Tasks — composition-validation-substrate

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
in any OPEN task line as a live caveat. They appear only in the RED-capture task (2.11) and in CLOSED tasks.
Everywhere else say "MUST fail", "does not compile", "abort", "barrier".

Premises (measured at `5cc0c7fb`; re-measure at the claim head and amend here): `component/registry.go:52-62`
(`Registration` carries `Schema`, no ports), `:139-147` (nil-`Factory` rejection precedent), `:209-273`
(`prepareComponent`; nil-NATS guard `:228`), `:564-590` (`captureComponentDeclaration`), `:667-678` (`Snapshots`);
`component/ports.go:53-113,153-165` (`PortDefinition`/`PortConfig`/`MergePortConfig`); `component/port_resolver.go:11`
(`Resolve`); `config/streams.go:435` (`extractPortsFromConfig`); `config/stream_bounds.go:259-320` (pure port lane);
`component/flowgraph/flowgraph.go:127-143` (`BuildFromRegistry`), `:216` (`ConnectComponentsByPatterns`), `:714`
(`AnalyzeConnectivity`), `:748-770` (status derivation), `:955` (`ValidateStreamRequirements`);
`engine/validator.go:300-388,389-457,459-489,491-610,612-623` (the logic that moves); `service/component_manager.go:229-335`
(`Initialize`; seal `:330`), `:1008` (cache invalidation), `:1430-1500`; `service/component_manager_http.go:74-77,618-716`;
`cmd/semstreams/flags.go:22,71` and `main.go:102-115` (`--validate`); `cmd/openapi-generator/main.go:54-90`;
`processor/agentic-tools/executors/register.go:51-54,114-117,201-204` (tool gates);
`processor/agentic-tools/component_catalog_executor.go:15-60`; `service/register.go:15` (`flow-builder`);
`service/flow_service.go:560-585` (override-expiry reporter host); `configs/protocol-flow.json:39-42`;
`test/e2e/client/observability.go:80-114,330-400`; `test/e2e/scenarios/tiered.go:187`; the 33-factory table in
`docs/proposals/gh1089-flow-boundary-inventory.md` §2.3.

## 1. Claim

- [ ] 1.1 Branch `claude/gh1089-composition-validation` in `../semstreams-wt/<branch>`; pushed; draft PR open with
      `Closes #1089`, the ADR-100 status line, and `implemented-by: <persona>` in the body; this change directory is
      its first commit. Draft PR #1088 (Slice C) is closed unmerged with a comment pointing here once the owner rules.
- [ ] 1.2 Owner rulings recorded here verbatim (design §7): nil-declarer rejection vs warning; boot refuse vs report;
      next-boot write verb; tool naming; one or two landing PRs; `--validate` flag retention. Each answer that departs
      from the delta is written into the delta before 2.x starts.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `component/registry_test.go`: `TestRegisterFactoryRejectsNilPortDeclarer` (does not compile until 3.1 adds
      the field — record the compile error verbatim as the baseline); `TestAdmissionRefusesPortDeclarationMismatch`
      with a package-local fake factory whose declarer returns one output and whose component returns two.
- [ ] 2.2 `componentregistry/register_integration_test.go` (`//go:build integration`):
      `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory` — `natsclient.NewTestClient(t,
      natsclient.WithJetStream(), natsclient.WithKV())`, the full registry (core + `graphresearch.RegisterComponents` +
      `optionalotel.Register`), a table of 33 rows each carrying the smallest configuration the factory admits
      (the inventory's nil-deps column names the five that reject `{}`), construct through
      `Registry.CreateComponent` with real deps, evaluate the declarer, compare resolved ports port-for-port. Assert
      `len(rows) == len(registry.ListFactories())` so a new factory cannot be skipped.
- [ ] 2.3 `composition/validate_test.go`: the eight named P2 tests (`TestValidateFindingsVocabularyIsClosed`,
      `TestValidateReportsUnknownComponent`, `TestValidateReportsRequiredStreamInputWithoutPublisher`,
      `TestValidateReportsInterfaceMismatch`, `TestValidateReportsStreamRequirement`,
      `TestValidateReportsConnectionPatternConflict`, `TestValidateReportsExclusiveResourceConflict`,
      `TestValidateIsDeterministic`) over hand-built registries of package-local fake factories with declarers only
      (no construction). Decode every result from its JSON into a fresh `composition.Result` before asserting.
- [ ] 2.4 `composition/shipped_configs_test.go`: `TestValidateShippedConfigsHaveNoErrorFindings` walking
      `configs/**/*.json`, `docker/**/*.json`, `test/e2e/**/*.json` (skip files that are not `config.Config`
      documents by decoding and checking `platform.org`). Record the initial finding counts per file here.
- [ ] 2.5 `composition/engine_parity_integration_test.go` (`//go:build integration`; DELETED in 3.8 with the engine):
      `TestValidateMatchesEngineFindingsForShippedConfigs` — for every shipped config, `flowstore.FromComponentConfigs`
      + `engine.ValidateFlowDefinition` against a real NATS client vs `composition.Validate`; assert equal sets of
      `(type, component, port)` after mapping `empty_flow`→`empty_composition`, `graph_build_error`→
      `port_declaration_error`. This is the dropped-step detector for the move in 3.2; a difference is a finding to
      resolve, not to map away.
- [ ] 2.6 `composition/cli/main_test.go`: `TestCLIValidateExitsNonZeroOnErrorFindings`,
      `TestCLICatalogPrintsEveryRegisteredFactory` (asserts 33 against `len(registry.ListFactories())`),
      `TestCLIGraphMermaidRendersEveryEdge`. `cmd/semstreams/main_test.go`:
      `TestValidateFlagReportsCompositionFindings`.
- [ ] 2.7 `composition/assert_test.go`: `TestAssertValidFailsOnErrorFinding` with a recording `testing.TB`.
- [ ] 2.8 `service/component_manager_boot_findings_integration_test.go` (`//go:build integration`):
      `TestComponentManagerRefusesBootOnErrorFinding` (a config with a JetStream input fed only by a core-NATS output),
      `TestComponentManagerExposesBootFindings`, `TestGraphProjectionMatchesAdmittedComposition`;
      `service/component_manager_http_test.go`: `TestFlowValidationHandlerProjectsLibraryResult` (decode into a fresh
      `composition.Result`; assert equality with the retained result). `composition/mermaid_test.go`:
      `TestMermaidIsDeterministic`.
- [ ] 2.9 `processor/agentic-tools/executors/composition_tools_test.go`: `TestValidateCompositionToolReturnsFindings`,
      `TestCompositionGraphToolReturnsMermaid`, `TestListComponentsCarriesPorts` — through `RegisterBuiltins` with
      `ToolDependencies{ComponentRegistry: reg, SkipBuiltins: skipAllBut("component_catalog")}` so the production
      wire is driven.
- [ ] 2.10 Removal guards: `service/register_test.go` `TestServiceRegistryHasNoFlowBuilder`;
      `processor/agentic-tools/executors/register_test.go` `TestToolRegistryHasNoFlowTools` (asserts each of the
      eleven names is absent after `RegisterBuiltins` with every dependency non-nil);
      `test/contract/openapi_no_flow_routes_test.go` `TestOpenAPIHasNoFlowRoutes`;
      `service/stream_override_expiry_test.go` `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`.
      `test/contract/schema_export_test.go` `TestSchemaExportCarriesDefaultPorts`;
      `composition/catalog_test.go` `TestCatalogCarriesDefaultPortsOrRequiresConfig`.
- [ ] 2.11 RED capture: run each §2 file with `-run` and record the verbatim `--- FAIL` / compile-error lines here
      (this is the one task where the words red / failed may appear). Commands:
      `go test -race ./component/ -run 'TestRegisterFactoryRejectsNilPortDeclarer|TestAdmissionRefusesPortDeclarationMismatch' -v`;
      `go test -race -tags=integration ./componentregistry/ -run TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory -v`;
      `go test -race ./composition/... -run 'TestValidate|TestCatalog|TestMermaid|TestAssertValid|TestCLI' -v`;
      `go test -race -tags=integration ./composition/ -run TestValidateMatchesEngineFindingsForShippedConfigs -v`;
      `go test -race -tags=integration ./service/ -run 'TestComponentManagerRefusesBootOnErrorFinding|TestComponentManagerExposesBootFindings|TestGraphProjectionMatchesAdmittedComposition' -v`;
      `go test -race ./service/ -run 'TestFlowValidationHandlerProjectsLibraryResult|TestServiceRegistryHasNoFlowBuilder|TestStreamOverrideExpiryReporterRegistersWithoutFlowService' -v`;
      `go test -race ./processor/agentic-tools/executors/ -run 'TestValidateCompositionToolReturnsFindings|TestCompositionGraphToolReturnsMermaid|TestListComponentsCarriesPorts|TestToolRegistryHasNoFlowTools' -v`;
      `go test -race ./cmd/semstreams/ -run TestValidateFlagReportsCompositionFindings -v`;
      `go test ./test/contract/ -run 'TestOpenAPIHasNoFlowRoutes|TestSchemaExportCarriesDefaultPorts' -v`.
      Commit the tests before any implementation (`test(composition): baseline for #1089`).

## 3. GREEN — implement in dependency order

- [ ] 3.1 **P1.** `component.PortDeclarer`, `Registration.Ports`, `RegistrationConfig.Ports`, nil rejection in
      `RegisterFactory`; exported `component.Declaration` value type (the existing `componentDeclaration` shape,
      `registry.go:76-84`); `Registry.Declare(factory, cfg types.ComponentConfig, instance) (Declaration, error)`
      resolving through `resolveAndProjectPort`; parity compare in `prepareComponent` after
      `captureComponentDeclaration`. Then all 33 factories: expose the existing derivation as `Ports` and call it
      from the constructor (one home; the constructor MUST NOT keep a second derivation). Record the 33 file:line
      pairs here. `examples/processors/*` and `cmd/e2e-semstreams/mission` components get declarers too (they
      register through the same seam).
- [ ] 3.2 **P2 + P6.** `composition/` package: constants, `Finding`, `Result`, `Graph`, `Validate`, `Analyze`,
      `Mermaid`; `flowgraph.BuildFromDeclarations`. Move `engine/validator.go:300-623` logic in (severity table in
      one function; interface compatibility exact-match preserved). `TestValidateMatchesEngineFindingsForShippedConfigs`
      MUST be green before 3.8; every difference it surfaces is recorded here with its disposition.
- [ ] 3.3 **P4.** `composition.AssertValid`.
- [ ] 3.4 **P3.** `composition/cli.Main`; `cmd/semstreams`: verb dispatch before `parseCLI` (`main.go:86`), and
      `--validate` (`main.go:112-115`) prints the same findings and exits non-zero on errors. `cmd/e2e-semstreams`
      wires the same. Update `docs/concepts/32-agent-memory.md:226` (`--validate` example) if its output changes.
- [ ] 3.5 **Measure shipped compositions.** Run `go run ./cmd/semstreams validate <path>` over every file 2.4 walks;
      paste the error findings here. Fix each shipped configuration or record it as FILED #n with the owner's
      disposition. 2.4 MUST be green before 3.6 flips the boot refuse.
- [ ] 3.6 **P5.** `ComponentManager.Initialize`: `Analyze(registry.Snapshots)` before `SealComposition`; log; refuse
      on error (per the 1.2 ruling); retain the result; `handleFlowValidation`/`handleFlowGraph` become projections of
      the retained result (delete `component_manager_http.go:677-683` status logic). Update
      `test/e2e/client/observability.go:330-400` to decode `composition.Result`.
- [ ] 3.7 **P7.** `list_components` gains `default_ports`; `validate_composition` and `composition_graph` executors
      under the `component_catalog` gate. `docs/operations/adopter-tool-effect-metadata.md:130` rows updated.
- [ ] 3.8 **Removal.** Rehome `service/stream_override_expiry.go` (constructor + `RegisterMetrics`) onto
      ComponentManager or the metrics service — decided and recorded here — THEN delete: `flowstore/`,
      `flowtemplate/`, `engine/` (and 2.5's parity test), `service/flow_service.go`, `service/flow_runtime_*.go` and
      their tests, the four executor files and their tests, `service/register.go:15`, `configs/protocol-flow.json:39-42`,
      `cmd/semstreams/main.go:24-25,245,247,707-760`, `cmd/e2e-semstreams/main.go:27-28,185,187,418-460`,
      `test/e2e/client/observability.go:80-114`, `ToolDependencies.FlowManager`/`FlowTemplateManager` and the two
      gates (`register.go:51,53,114,116,201,203`), `docs/concepts/12-flow-architecture.md`,
      `docs/operations/migration-boot-only-flow-activation.md`. `grep -rn "flowstore\|flowtemplate\|flowengine\|flow-builder\|flowbuilder" --include='*.go' --include='*.json' --include='*.yml' --include='*.md' .` (main tree,
      `docs/adr` and `openspec/changes/archive` excluded) → 0; paste the command and count here.
- [ ] 3.9 Write `docs/operations/migration-composition-validation-adr100.md`: removed routes, tools, packages,
      buckets; per-repo instructions for semstreams-ui and semteams from inventory §9; what the projection and the
      verbs give back. Set ADR-100 Status to Accepted with the ruling date only after 1.2 records the ruling.
- [ ] 3.10 Commit GREEN (`feat(composition)!: …` with a BREAKING footer) before §4.

## 4. Forced omissions — each guard must be load-bearing

Each: apply the omission, run the named command, record the verbatim failure, restore with `cp` from a copy taken
before the omission, and record `shasum -a 256` equality of the restored file.

- [ ] 4.1 Delete the parity compare in `prepareComponent` → `go test -race ./component/ -run
      TestAdmissionRefusesPortDeclarationMismatch -v` MUST fail.
- [ ] 4.2 Delete the nil check on `Ports` in `RegisterFactory` → `TestRegisterFactoryRejectsNilPortDeclarer` MUST fail.
- [ ] 4.3 Replace one factory's declarer body with its defaults only (udp: drop the merge) →
      `go test -race -tags=integration ./componentregistry/ -run TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory -v`
      MUST fail on the udp row with an overridden port.
- [ ] 4.4 Delete the `interface_mismatch` branch in `composition` → `TestValidateReportsInterfaceMismatch` and
      `TestValidateFindingsVocabularyIsClosed` MUST fail.
- [ ] 4.5 Delete the boot refuse (keep the log) → `go test -race -tags=integration ./service/ -run
      TestComponentManagerRefusesBootOnErrorFinding -v` MUST fail.
- [ ] 4.6 Reintroduce a local status computation in `handleFlowValidation` → `TestFlowValidationHandlerProjectsLibraryResult`
      MUST fail.
- [ ] 4.7 Delete the rehomed reporter registration → `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`
      MUST fail.
- [ ] 4.8 Delete edge rendering in `Mermaid` → `TestCLIGraphMermaidRendersEveryEdge` MUST fail.
- [ ] 4.9 Re-add `"flow-builder"` to `service/register.go` → `TestServiceRegistryHasNoFlowBuilder` MUST fail.

## 5. Schema regeneration

- [ ] 5.1 `task schema:generate`; commit the `schemas/*.v1.json` `default_ports` rows, the removed `/flows*` rows and
      `Flow*` schemas, and the changed `/flowgraph` and `/validate` response schemas; delete
      `schemas/workflow-definition.v1.json` (stale: no factory, `cmd/openapi-generator/main.go:94`) and record it.
      Second `task schema:generate` → `git diff --exit-code schemas/ specs/openapi.v3.yaml` clean.

## 6. Standard gates — record each command and its result

- [ ] 6.1 `task lint`.
- [ ] 6.2 `go test -race ./...`.
- [ ] 6.3 `go test -race -tags=integration -p 2 ./...`.
- [ ] 6.4 `task build`.
- [ ] 6.5 `go test ./test/contract/...`.
- [ ] 6.6 `task e2e:core`, `task e2e:crud-tools`, `task e2e:agentic` — BREAKING commit; all three green on the exact
      head that carries 3.8; paste the tier summaries here.
- [ ] 6.7 Downstream measurement (read-only): `cd ~/Code/c360/semteams && go vet ./cmd/semteams/` against a
      `replace` to this branch in a scratch module (never edit semteams); record the compile errors as the migration
      document's semteams section. semstreams-ui: record the 15 call sites from inventory §9 in the migration
      document; the owner runs its suite.

## 7. Review and archive (inside the landing PR; the `AGENTS.md:68-73` Land order)

- [ ] 7.1 `semstreams-reviewer` on the GREEN + §4 + §5 head: verdict, every finding and its disposition (FIXED /
      FILED #n / ruling) recorded here. Findings on unused paths are FILED, not fixed.
- [ ] 7.2 Owner-run Codex round where the owner asks for it: verdict and dispositions recorded here; each fix
      re-enters 7.1 and re-runs the focused commands of 2.11 with `-v`.
- [ ] 7.3 `conformance.md`: replace every `__` placeholder with the measured `file:line` at the head that carries the
      last `.go` or delta change. Maintained as part of every commit that moves a line, not at the end.
- [ ] 7.4 Reconcile: every scenario in `specs/composition-validation/spec.md` names a test that exists and is green in
      6.2/6.3/6.5; every REMOVED requirement in `specs/flow-authoring/spec.md` and
      `specs/component-runtime-config/spec.md` names tests that no longer exist; table recorded here. Any `[~]` in
      this file is ALSO written into the delta before archiving.
- [ ] 7.5 `openspec archive composition-validation-substrate` with the spec sync as the final content commit; the
      narrow reviewer check of the archive/spec sync follows as a PR comment; then undraft. The PR body is a
      published layer: re-read it at undraft and correct any claim the branch no longer supports.

## 8. Not in scope (recorded so the archiver does not infer completion)

- A next-boot component-configuration write verb (design §7 item 1).
- `POST <components>/validate` with a draft body.
- Unifying merge-vs-replace port-override policies across factories.
- The e2e client's gateway filter (`observability.go:378-392`).
- semstreams-ui and semteams migrations (owners' work; instructions in the migration document).
- #1087's four scenarios (their routes no longer exist).
