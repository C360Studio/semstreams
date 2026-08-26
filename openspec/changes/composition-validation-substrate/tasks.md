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

- [x] 1.1 Claimed 2026-08-26 on branch `claude/gh1092-composition-validation-substrate` (worktree
      `../semstreams-wt/claude/gh1092-composition-validation-substrate`, base `origin/main` `c3a17741`); draft PR
      #1101 is open with `Closes #1092` in its body; this change directory's claim tick was its first commit. The
      earlier wording of this line (`Closes #1089`, branch `claude/gh1089-composition-validation`, PR #1088 closure)
      predated the owner's two-PR split (design §7 item 5): #1092 is the substrate PR (P1–P7, this PR #1101) and
      #1093 is the retirement PR. Every task below that belongs to the retirement is annotated `(#1093)` and stays
      open here until that PR lands; the REMOVED deltas (`specs/flow-authoring`, `specs/component-runtime-config`)
      and the archive land with #1093.
- [x] 1.2 Owner ruling on #1089 (2026-08-26), verbatim: "C, ADR-100 accepted." The nine §7 defaults stood without
      override (ADR-100 Status; restated by the owner at the #1092 claim): (1) no next-boot write verb; (2) a factory
      without a declarer is REJECTED at `RegisterFactory`; (3) boot REFUSES on an error-severity finding, only after
      every shipped config has been measured (P3 before P5); (4) tools named `validate_composition` /
      `composition_graph`, `list_components` kept; (5) TWO landing PRs — #1092 substrate (this PR #1101), #1093
      retirement; (6) `--validate` stays as an alias; (7) `empty_composition` is a WARNING; (8) the new exported
      surface is reviewed by the owner inside this PR; (9) PR #1088 closed unmerged. No answer departs from the
      delta, so no delta edit was needed before 2.x.

## 2. Baseline capture — write the named tests first

- [x] 2.1 `component/registry_test.go`: `TestRegisterFactoryRejectsNilPortDeclarer` (does not compile until 3.1 adds
      the field — record the compile error verbatim as the baseline); `TestAdmissionRefusesPortDeclarationMismatch`
      with a package-local fake factory whose declarer returns one output and whose component returns two.
- [x] 2.2 `componentregistry/register_integration_test.go` (`//go:build integration`):
      `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory` — `natsclient.NewTestClient(t,
      natsclient.WithJetStream(), natsclient.WithKV())`, the full registry (core + `graphresearch.RegisterComponents` +
      `optionalotel.Register`), a table of 33 rows each carrying the smallest configuration the factory admits
      (the inventory's nil-deps column names the five that reject `{}`), construct through
      `Registry.CreateComponent` with real deps, evaluate the declarer, compare resolved ports port-for-port. Assert
      `len(rows) == len(registry.ListFactories())` so a new factory cannot be skipped.
- [x] 2.3 `composition/validate_test.go`: the eight named P2 tests (`TestValidateFindingsVocabularyIsClosed`,
      `TestValidateReportsUnknownComponent`, `TestValidateReportsRequiredStreamInputWithoutPublisher`,
      `TestValidateReportsInterfaceMismatch`, `TestValidateReportsStreamRequirement`,
      `TestValidateReportsConnectionPatternConflict`, `TestValidateReportsExclusiveResourceConflict`,
      `TestValidateIsDeterministic`) over hand-built registries of package-local fake factories with declarers only
      (no construction). Decode every result from its JSON into a fresh `composition.Result` before asserting.
- [x] 2.4 `composition/shipped_configs_test.go`: `TestValidateShippedConfigsHaveNoErrorFindings` walking
      `configs/**/*.json`, `docker/**/*.json`, `test/e2e/**/*.json` (skip files that are not `config.Config`
      documents by decoding and checking `platform.org`). Record the initial finding counts per file here.
- [x] 2.5 `composition/engine_parity_integration_test.go` (`//go:build integration`; DELETED in 3.8 with the engine):
      `TestValidateMatchesEngineFindingsForShippedConfigs` — for every shipped config, `flowstore.FromComponentConfigs`
      + `engine.ValidateFlowDefinition` against a real NATS client vs `composition.Validate`; assert equal sets of
      `(type, component, port)` after mapping `empty_flow`→`empty_composition`, `graph_build_error`→
      `port_declaration_error`. This is the dropped-step detector for the move in 3.2; a difference is a finding to
      resolve, not to map away.
- [x] 2.6 `composition/cli/main_test.go`: `TestCLIValidateExitsNonZeroOnErrorFindings`,
      `TestCLICatalogPrintsEveryRegisteredFactory` (asserts 33 against `len(registry.ListFactories())`),
      `TestCLIGraphMermaidRendersEveryEdge`. `cmd/semstreams/main_test.go`:
      `TestValidateFlagReportsCompositionFindings`.
- [x] 2.7 `composition/assert_test.go`: `TestAssertValidFailsOnErrorFinding` with a recording `testing.TB`.
- [x] 2.8 `service/component_manager_boot_findings_integration_test.go` (`//go:build integration`):
      `TestComponentManagerRefusesBootOnErrorFinding` (a config with a JetStream input fed only by a core-NATS output),
      `TestComponentManagerExposesBootFindings`, `TestGraphProjectionMatchesAdmittedComposition`;
      `service/component_manager_http_test.go`: `TestFlowValidationHandlerProjectsLibraryResult` (decode into a fresh
      `composition.Result`; assert equality with the retained result). `composition/mermaid_test.go`:
      `TestMermaidIsDeterministic`.
- [x] 2.9 `processor/agentic-tools/executors/composition_tools_test.go`: `TestValidateCompositionToolReturnsFindings`,
      `TestCompositionGraphToolReturnsMermaid`, `TestListComponentsCarriesPorts` — through `RegisterBuiltins` with
      `ToolDependencies{ComponentRegistry: reg, SkipBuiltins: skipAllBut("component_catalog")}` so the production
      wire is driven.
- [ ] 2.10 (#1093 for the four removal guards; the two catalog tests are #1092 — see 2.10a) Removal guards: `service/register_test.go` `TestServiceRegistryHasNoFlowBuilder`;
      `processor/agentic-tools/executors/register_test.go` `TestToolRegistryHasNoFlowTools` (asserts each of the
      eleven names is absent after `RegisterBuiltins` with every dependency non-nil);
      `test/contract/openapi_no_flow_routes_test.go` `TestOpenAPIHasNoFlowRoutes`;
      `service/stream_override_expiry_test.go` `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`.
- [x] 2.10a `test/contract/schema_export_test.go` `TestSchemaExportCarriesDefaultPorts`;
      `composition/catalog_test.go` `TestCatalogCarriesDefaultPortsOrRequiresConfig`.
- [x] 2.11 RED capture: run each §2 file with `-run` and record the verbatim `--- FAIL` / compile-error lines here
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
      RED captured 2026-08-26 at the claim head (`4f7e678e` + §2 test files), verbatim first lines (log:
      scratchpad `red.log`; the #1093 test names in the commands above were not run — they land with the retirement PR):
      1. `component/registry_test.go:262:3: unknown field Ports in struct literal of type RegistrationConfig` /
         `FAIL	github.com/c360studio/semstreams/component [build failed]`
      2. `componentregistry/register_integration_test.go:126:30: registry.Declare undefined (type *component.Registry has no field or method Declare)` /
         `FAIL	github.com/c360studio/semstreams/componentregistry [build failed]`
      3. `github.com/c360studio/semstreams/composition: build constraints exclude all Go files in .../composition` /
         `github.com/c360studio/semstreams/composition/cli: no non-test Go files in .../composition/cli` /
         `FAIL	github.com/c360studio/semstreams/composition [build failed]` / `FAIL	github.com/c360studio/semstreams/composition/cli [build failed]`
      4. `github.com/c360studio/semstreams/composition: no non-test Go files in .../composition` / `FAIL	github.com/c360studio/semstreams/composition [build failed]`
      5. `github.com/c360studio/semstreams/composition: no non-test Go files in .../composition` / `FAIL	github.com/c360studio/semstreams/service [build failed]`
      6. `github.com/c360studio/semstreams/composition: build constraints exclude all Go files in .../composition` / `FAIL	github.com/c360studio/semstreams/service [build failed]`
      7. `... composition: build constraints exclude all Go files ...` / `FAIL	github.com/c360studio/semstreams/processor/agentic-tools/executors [build failed]`
      8. `... composition: build constraints exclude all Go files ...` / `FAIL	github.com/c360studio/semstreams/cmd/semstreams [build failed]`
      9. `schema_export_test.go:52: udp.v1.json: default_ports=false ports_require_config=false ports_error="" — want exactly one shape` (one line per
         committed component schema) / `--- FAIL: TestSchemaExportCarriesDefaultPorts (0.01s)` / `FAIL	github.com/c360studio/semstreams/test/contract	0.475s`
      Committed as `test(composition): baseline for #1092` (the issue number in the task's suggested subject predates the split).

## 3. GREEN — implement in dependency order

- [x] 3.1 **P1.** `component.PortDeclarer`, `Registration.Ports`, `RegistrationConfig.Ports`, nil rejection in
      `RegisterFactory`; exported `component.Declaration` value type (the existing `componentDeclaration` shape,
      `registry.go:76-84`); `Registry.Declare(factory, cfg types.ComponentConfig, instance) (Declaration, error)`
      resolving through `resolveAndProjectPort`; parity compare in `prepareComponent` after
      `captureComponentDeclaration`. Then all 33 factories: expose the existing derivation as `Ports` and call it
      from the constructor (one home; the constructor MUST NOT keep a second derivation). Record the 33 file:line
      pairs here. `examples/processors/*` and `cmd/e2e-semstreams/mission` components get declarers too (they
      register through the same seam).
      DONE (head after `0333cde3`): `component/registry.go:40` (`PortDeclarer`), `:64` (`Registration.Ports`), `:74`
      (`RegistrationConfig.Ports`), `:88` (`Declaration`, the exported `componentDeclaration` shape + `ComponentType`),
      `:134` (`declarationSnapshot.Declaration()`), `:173` (nil declarer rejected at `RegisterFactory`, after the Type
      check so the existing validation table keeps its messages), `:318` (`Registry.Declare(instanceName, config)` —
      one parameter fewer than the design's `Declare(factory, cfg, instance)`: `cfg.Name` IS the factory), `:300`
      + `:394` (parity compare in `prepareComponent` after `captureComponentDeclaration`: name, direction, required,
      kind, resource id, subjects, interface, in order; first differing port named). `component/ports.go:157`
      (`PortConfigFrom` — the one helper the 38 declarers use to expose resolved ports as definitions; added beside
      the design's list, owner review). Declarers (`DeclarePorts`, one per package; the constructor calls the same
      `resolveConfig`/`resolvePorts` and keeps no second derivation): agentic-dispatch `processor/agentic-dispatch/component.go:200`
      · agentic-governance `processor/agentic-governance/component.go:76` · agentic-loop `processor/agentic-loop/component.go:197`
      · agentic-model `processor/agentic-model/component.go:113` · agentic-tools `processor/agentic-tools/component.go:103`
      · file `output/file/file.go:146` · file_input `input/file/file.go:630` · gated-dag `processor/gated-dag/component.go:82`
      · graph-clustering `processor/graph-clustering/component.go:669` · graph-embedding `processor/graph-embedding/component.go:381`
      · graph-gateway `gateway/graph-gateway/component.go:360` · graph-index `processor/graph-index/component.go:376`
      · graph-index-spatial `processor/graph-index-spatial/component.go:212` · graph-index-temporal `processor/graph-index-temporal/component.go:221`
      · graph-ingest `processor/graph-ingest/component.go:645` · graph-query `processor/graph-query/component.go:214`
      · http `gateway/http/http.go:75` (static: no ports; config still validated) · httppost `output/httppost/httppost.go:146`
      · json_filter `processor/json_filter/json_filter.go:122` · json_generic `processor/json_generic/json_generic.go:106`
      · json_map `processor/json_map/json_map.go:128` · lifecycle-gateway `gateway/lifecycle-gateway/component.go:302`
      · objectstore `storage/objectstore/component.go:154` (see 3.1a) · otel-exporter `output/otel/component.go:115`
      · research-graph-assess `processor/research-graph-assess/component.go:84` · research-graph-classify `processor/research-graph-classify/component.go:98`
      · research-graph-execute `processor/research-graph-execute/component.go:78` · research-graph-route `processor/research-graph-route/component.go:87`
      · research-graph-synthesize `processor/research-graph-synthesize/component.go:74` · rule-processor `processor/rule/factory.go:31`
      · udp `input/udp/udp.go:739` · websocket `output/websocket/websocket.go:1842` · websocket_input `input/websocket/register.go:17`;
      registered through the same seam: http_input `input/http/register.go:14`, document_processor
      `examples/processors/document/component.go:119`, iot_sensor `examples/processors/iot_sensor/component.go:118`,
      weather_station `examples/processors/weather_station/component.go:91`, mission-command `cmd/e2e-semstreams/mission/command.go:175`.
      Parity: `go test -race -tags=integration ./componentregistry/ -run TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory -v`
      → `--- PASS: TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory (0.47s)` / `ok github.com/c360studio/semstreams/componentregistry 1.971s`
      (33/33 rows, `len(rows) == len(ListFactories())`). Two rows needed the smallest admitted configuration rather than `{}`:
      `http` (`routes[0].nats_subject`) and `lifecycle-gateway` (`path_prefix` + `ports.outputs`: `SafeUnmarshal` runs `Validate`
      BEFORE `ApplyDefaults`, so any non-empty document must carry both — pre-existing, not changed here). Catalog for `{}`
      (`go run ./cmd/semstreams catalog`, 33 entries): 23 declare `default_ports`; 10 `ports_require_config`: file, file_input,
      gated-dag, graph-gateway, graph-ingest, graph-query, http, httppost, lifecycle-gateway, rule-processor (file, httppost,
      http, lifecycle-gateway because `SafeUnmarshal` validates before defaults on `{}` — pre-existing).
- [~] 3.1a **objectstore instance name — measured, not codified.** `storage/objectstore/component.go` passes the literal
      `"objectstore"` to `resolveObjectStorePorts` (now the named constant `constructedInstanceName`, `:143`); the factory
      signature carries no instance name and `component.Dependencies` has no such field, so the ADMITTED declaration NEVER
      carries the real instance name — its `store-provide` port is `store:objectstore` for every instance. Every shipped
      instance is named `objectstore` (17 configs, `grep -rn '"name": "objectstore"' configs/`), so shipped compositions
      cannot observe the difference. The declarer (`:154`) therefore ignores its `instanceName` parameter and mirrors the
      constructor, so parity holds and nothing changes at runtime; the spec's "pure function of ... the instance name"
      is NOT honoured for this one factory. Threading the real instance name into the constructor would change the
      store-provide resource identity (`store:<instance>`) at runtime for any adopter naming the instance differently —
      an owner ruling (FILE: objectstore constant instance name, inventory §12.2). Neither side is codified in a test.
- [x] 3.2 **P2 + P6.** `composition/` package: constants, `Finding`, `Result`, `Graph`, `Validate`, `Analyze`,
      `Mermaid`; `flowgraph.BuildFromDeclarations`. Move `engine/validator.go:300-623` logic in (severity table in
      one function; interface compatibility exact-match preserved). `TestValidateMatchesEngineFindingsForShippedConfigs`
      MUST be green before 3.8; every difference it surfaces is recorded here with its disposition.
      DONE: `composition/findings.go` (13 `Type*` constants, `Finding`, `Result`, `severityOf` `:67` — the one severity
      table), `composition/graph.go` (`Graph`/`Node`/`PortView`/`Edge`), `composition/analyze.go:16` (`Analyze`),
      `composition/validate.go:19` (`Validate`; mirrors `prepareComponent`'s pre-factory checks as findings; an
      exclusive-resource loser is excluded from the graph exactly as admission would refuse it),
      `composition/mermaid.go:12`, `composition/catalog.go:51`; `component/flowgraph/flowgraph.go:144`
      (`BuildFromDeclarations`; `BuildFromRegistry` is now a wrapper over it). `flowgraph` edge derivation, orphan and
      disconnected-node walks, KV-writer and network-conflict checks, and stream-requirement dedupe now iterate sorted
      keys (`:163` `sortedNodeNames` and `slices.Sorted(maps.Keys(...))` at each site) — without this the connection
      ID chosen for a pair reachable under both a stream name and a subject differed run to run, and
      `TestValidateIsDeterministic`/`TestMermaidIsDeterministic` could not hold. Engine parity (dropped-step detector):
      `go test -race -tags=integration ./composition/ -run TestValidateMatchesEngineFindingsForShippedConfigs -v` →
      `--- PASS: TestValidateMatchesEngineFindingsForShippedConfigs (1.29s)` / `ok github.com/c360studio/semstreams/composition 2.737s`
      over all 22 shipped configs, two-way on the engine's vocabulary. Differences surfaced and their dispositions:
      (1) with only a NATS client the engine could not construct `agentic-dispatch`/`agentic-model` (ModelRegistry) and
      `lifecycle-gateway` (LifecycleManager), and `engine/validator.go:120-133` returns build errors only and SKIPS
      connectivity when any node fails — an oracle that bails is no oracle; disposition: added
      `engine.NewValidatorWithDependencies` (`engine/validator.go:37`, additive; `NewValidator` delegates to it) so the
      test constructs every node with real deps and the comparison is complete; a `graph_build_error` from the engine
      is now a test failure, never a skip. (2) `stream_requirement`, `config_invalid`, `component_type_mismatch`,
      `component_config_invalid`, `exclusive_resource_conflict`, `connection_pattern_error` are composition-only: the
      engine never emitted them (stream requirements lived in the HTTP handler, the rest in the Registry); they are
      excluded from the engine comparison by vocabulary, not mapped. No remaining difference.
- [x] 3.3 **P4.** `composition.AssertValid` — `composition/assert.go:14`; `TestAssertValidFailsOnErrorFinding` PASS.
- [x] 3.4 **P3.** `composition/cli.Main`; `cmd/semstreams`: verb dispatch before `parseCLI` (`main.go:86`), and
      `--validate` (`main.go:112-115`) prints the same findings and exits non-zero on errors. `cmd/e2e-semstreams`
      wires the same. Update `docs/concepts/32-agent-memory.md:226` (`--validate` example) if its output changes.
      DONE: `composition/cli/main.go:54` (`Main`) and `:37` (`Dispatch(args, registry, stdout, stderr) (code, handled)`
      — the design's adopter-seam three-liner; added beside `Main`, owner review); `cmd/semstreams/main.go:86`
      (`dispatchCompositionVerb`, before `run()` so no banner precedes the JSON), `:108` (`fullComponentRegistry`: the
      verbs and `--validate` judge against the FULL catalog — core + graph-research + OTEL — while boot gates the two
      capabilities on `Selected(cfg)`; recorded as a prediction/observation gap for the owner), `:165` (`--validate`
      calls `compositioncli.Main` with the `validate` verb: same findings, exit non-zero on errors); `cmd/e2e-semstreams/main.go:85,104,157`
      the same plus the bundled examples; help text in `cmd/semstreams/flags.go` and the e2e `printHelp`.
      `docs/concepts/32-agent-memory.md:226` updated (output is the `composition.Result` JSON; exit 1 on error findings).
      Tests: `TestCLIValidateExitsNonZeroOnErrorFindings`, `TestCLICatalogPrintsEveryRegisteredFactory` (33),
      `TestCLIGraphMermaidRendersEveryEdge`, `TestValidateFlagReportsCompositionFindings` (drives `main()` in a child
      process via `TestMain`) all PASS.
- [~] 3.5 **Measure shipped compositions.** Run `go run ./cmd/semstreams validate <path>` over every file 2.4 walks;
      paste the error findings here. Fix each shipped configuration or record it as FILED #n with the owner's
      disposition. 2.4 MUST be green before 3.6 flips the boot refuse.
      MEASURED (`TestValidateShippedConfigsHaveNoErrorFindings` over the union registry core + graph-research + OTEL +
      the e2e examples, 22 `config.Config` documents under `configs/`; `docker/` and `test/e2e/` hold no JSON configs),
      verbatim per file:
      ../configs/agentic.json: status=errors errors=1 warnings=11 nodes=7 edges=117
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/cloud-federation.json: status=warnings errors=0 warnings=1 nodes=2 edges=1
      ../configs/e2e-structural.json: status=warnings errors=0 warnings=10 nodes=17 edges=16
      ../configs/edge-federation.json: status=valid errors=0 warnings=0 nodes=3 edges=2
      ../configs/examples/research-graph-pipeline.json: status=errors errors=6 warnings=10 nodes=13 edges=95
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
        error finding orphaned_port on research-graph-assess/assess_trigger: input port 'assess_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-classify/classify_trigger: input port 'classify_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-execute/execute_trigger: input port 'execute_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-route/route_trigger: input port 'route_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-synthesize/synthesize_trigger: input port 'synthesize_trigger' (stream): no_publishers
      ../configs/flows/crud-tools-test.json: status=errors errors=1 warnings=9 nodes=7 edges=85
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/flows/deep-research-test.json: status=errors errors=1 warnings=9 nodes=7 edges=86
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/flows/deep-research.json: status=errors errors=1 warnings=9 nodes=8 edges=166
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/flows/lesson-example.json: status=errors errors=1 warnings=9 nodes=6 edges=72
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/flows/ops-agent-test.json: status=errors errors=1 warnings=9 nodes=6 edges=72
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/flows/ops-agent.json: status=errors errors=1 warnings=9 nodes=6 edges=72
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
      ../configs/gemini-example.json: status=warnings errors=0 warnings=1 nodes=0 edges=0
      ../configs/graph-backend.json: status=warnings errors=0 warnings=9 nodes=5 edges=3
      ../configs/hello-world.json: status=warnings errors=0 warnings=7 nodes=6 edges=4
      ../configs/lifecycle-flow.json: status=errors errors=1 warnings=1 nodes=5 edges=4
        error finding stream_requirement on graph-ingest/mission_in: JetStream subscriber expects a stream for subjects [mission.processed.entity] but its publishers [mission-command] use core NATS (no stream will be created)
      ../configs/protocol-flow.json: status=warnings errors=0 warnings=11 nodes=12 edges=9
      ../configs/research-graph-e2e.json: status=errors errors=6 warnings=10 nodes=13 edges=95
        error finding orphaned_port on agentic-dispatch/user.message: input port 'user.message' (stream): no_publishers
        error finding orphaned_port on research-graph-assess/assess_trigger: input port 'assess_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-classify/classify_trigger: input port 'classify_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-execute/execute_trigger: input port 'execute_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-route/route_trigger: input port 'route_trigger' (stream): no_publishers
        error finding orphaned_port on research-graph-synthesize/synthesize_trigger: input port 'synthesize_trigger' (stream): no_publishers
      ../configs/semantic-8b.json: status=warnings errors=0 warnings=12 nodes=19 edges=20
      ../configs/semantic-frontier.json: status=warnings errors=0 warnings=12 nodes=19 edges=20
      ../configs/semantic.json: status=warnings errors=0 warnings=12 nodes=19 edges=20
      ../configs/statistical.json: status=warnings errors=0 warnings=12 nodes=19 edges=20
      ../configs/structural.json: status=warnings errors=0 warnings=10 nodes=17 edges=17
      Twelve configs carry error findings, ALL from two validator classes the composition cannot observe — none is a
      configuration defect a config edit should absorb:
      (a) `orphaned_port` error = required stream input with no publisher IN THE COMPOSITION, where the publisher is
          external by design: `agentic-dispatch/user.message` (the UI publishes `user.message.>`; 9 configs) and the
          five research-graph `*_trigger` inputs (rule-processor `publish_message` actions publish
          `component.<stage>.<loop>`; rules are not ports; 2 configs). The severity rule is the engine's, moved
          verbatim as the delta specifies; the engine judged saved diagrams, never a boot gate.
      (b) `stream_requirement` on `lifecycle-flow.json` `graph-ingest/mission_in`: the config declares an explicit
          `streams.MISSION` on `mission.>`, so the core-NATS publish lands in that stream and the JetStream subscriber
          is fed; `flowgraph.ValidateStreamRequirements` (moved from `component_manager_http.go`) cannot see explicit
          `streams` declarations. The lifecycle e2e tier does not call `CheckFlowHealth`, which is why the pre-existing
          "critical" verdict never bit. `mission-command` requires core-NATS kinds, so the config cannot be "fixed" to
          jetstream.
      Disposition needed from the owner (FILE: both): rule (a) → warning, or a port-grammar way to say "fed externally";
      rule (b) → explicit-streams-aware. Neither the severity table nor the factory defaults were changed here (the
      delta binds both); the test stays as the target state and is `t.Skip`ped naming this task
      (`composition/shipped_configs_test.go:69`).
- [~] 3.6 **P5.** `ComponentManager.Initialize`: `Analyze(registry.Snapshots)` before `SealComposition`; log; refuse
      on error (per the 1.2 ruling); retain the result; `handleFlowValidation`/`handleFlowGraph` become projections of
      the retained result (delete `component_manager_http.go:677-683` status logic). Update
      `test/e2e/client/observability.go:330-400` to decode `composition.Result`.
      DONE except the refuse: `service/component_manager.go:365` (`analyzeBootComposition`, called at `:249` for the
      nil-config path and `:342` before `SealComposition`; logs every finding, retains the result at `:80`
      `bootFindings`), `service/component_manager_http.go:601` (`handleFlowValidation` serves the retained result
      verbatim), `:578` (`handleFlowGraph` serves `result.Graph`, Mermaid on `format=mermaid`), OpenAPI rows for
      `/validate` and `/flowgraph` now carry `SchemaRef`s to `Result`/`Graph`; the handler's private status logic is
      deleted. `test/e2e/client/observability.go` decodes `composition.Result`; `CheckFlowHealth` fails on error
      findings and keeps the tier's gateway filter over `disconnected_node` warnings. Tests PASS:
      `TestComponentManagerExposesBootFindings`, `TestGraphProjectionMatchesAdmittedComposition`,
      `TestFlowValidationHandlerProjectsLibraryResult`.
      NOT DONE — the REFUSE (`Initialize` returning an error on an error-severity finding): 3.5's measurement is red for
      12 of 22 shipped configs; flipping the refuse would make every agentic configuration unbootable. The owner's
      default (3) makes the measurement the precondition, so the refuse waits for the 3.5 ruling; it is one error
      return in `analyzeBootComposition` (`component_manager.go:365`, comment names this task).
      `TestComponentManagerRefusesBootOnErrorFinding` stays as the target state and is `t.Skip`ped naming this task
      (`service/component_manager_boot_findings_integration_test.go:102`). The delta's scenario "an error finding refuses
      boot" carries the same `[~]` note.
- [x] 3.7 **P7.** `list_components` gains `default_ports`; `validate_composition` and `composition_graph` executors
      under the `component_catalog` gate. `docs/operations/adopter-tool-effect-metadata.md:130` rows updated.
      DONE: `processor/agentic-tools/executors/composition_tools.go` (`validate_composition`, `composition_graph`;
      `ToolEffectReadOnly`; config decoded through `config.Loader.LoadFromBytes` so the tool judges what a file would),
      registered under the `component_catalog` gate at `register_component_catalog.go:30`; `list_components` gains
      `default_ports` through `service.BuildComponentTypeCatalog` → `composition.Catalog` (`component_manager_http.go:387`;
      `/types/{id}` serves the same entry). Doc row updated. Tests PASS: `TestValidateCompositionToolReturnsFindings`,
      `TestCompositionGraphToolReturnsMermaid`, `TestListComponentsCarriesPorts` (through `RegisterBuiltins` with
      `SkipBuiltins` = every group but `component_catalog`).
- [ ] 3.8 (#1093) **Removal.** Rehome `service/stream_override_expiry.go` (constructor + `RegisterMetrics`) onto
      ComponentManager or the metrics service — decided and recorded here — THEN delete: `flowstore/`,
      `flowtemplate/`, `engine/` (and 2.5's parity test), `service/flow_service.go`, `service/flow_runtime_*.go` and
      their tests, the four executor files and their tests, `service/register.go:15`, `configs/protocol-flow.json:39-42`,
      `cmd/semstreams/main.go:24-25,245,247,707-760`, `cmd/e2e-semstreams/main.go:27-28,185,187,418-460`,
      `test/e2e/client/observability.go:80-114`, `ToolDependencies.FlowManager`/`FlowTemplateManager` and the two
      gates (`register.go:51,53,114,116,201,203`), `docs/concepts/12-flow-architecture.md`,
      `docs/operations/migration-boot-only-flow-activation.md`. `grep -rn "flowstore\|flowtemplate\|flowengine\|flow-builder\|flowbuilder" --include='*.go' --include='*.json' --include='*.yml' --include='*.md' .` (main tree,
      `docs/adr` and `openspec/changes/archive` excluded) → 0; paste the command and count here.
- [ ] 3.9 (#1093) Write `docs/operations/migration-composition-validation-adr100.md`: removed routes, tools, packages,
      buckets; per-repo instructions for semstreams-ui and semteams from inventory §9; what the projection and the
      verbs give back. Set ADR-100 Status to Accepted with the ruling date only after 1.2 records the ruling.
- [x] 3.10 Commit GREEN (`feat(composition)!: …` with a BREAKING footer) before §4. Gates run at the GREEN head before
      the commit: `go build ./...`; `go vet ./...`; `go vet -tags=integration ./...`; `task lint` (revive 0 warnings
      after two `empty-block` fixes); `go test -race ./...` → every package `ok` (two fixes on the way: the
      port-grammar guard rejected a `FilePort` type assertion in `output/file/file.go`, replaced by carrying the
      path; `TestComponentManagerFlowReportingUsesRetainedPortsAfterComponentMutation` now computes the retained boot
      result before hitting the projections); `go test ./test/contract/...` `ok` with the regenerated schemas (the
      `default_ports` rows land in this commit so it is green; §5 verifies the second regeneration is clean).

## 4. Forced omissions — each guard must be load-bearing

Each: apply the omission, run the named command, record the verbatim failure, restore with `cp` from a copy taken
before the omission, and record `shasum -a 256` equality of the restored file.

- [x] 4.1 Delete the parity compare in `prepareComponent` → `go test -race ./component/ -run
      TestAdmissionRefusesPortDeclarationMismatch -v` MUST fail.
      DONE: `[applied]` → `--- FAIL: TestAdmissionRefusesPortDeclarationMismatch (0.00s)`; `component/registry.go` restored by `cp`, sha256 `620dc74f…a3dd` before and after.
- [x] 4.2 Delete the nil check on `Ports` in `RegisterFactory` → `TestRegisterFactoryRejectsNilPortDeclarer` MUST fail.
      DONE: `[applied]` → `--- FAIL: TestRegisterFactoryRejectsNilPortDeclarer (0.00s)`; `component/registry.go` restored, sha256 `620dc74f…a3dd` before and after.
- [x] 4.3 Replace one factory's declarer body with its defaults only (udp: drop the merge) →
      `go test -race -tags=integration ./componentregistry/ -run TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory -v`
      MUST fail on the udp row with an overridden port.
      DONE (the udp row overrides `udp_socket` to port 14551, commit `c57f5994`, so a defaults-only declarer disagrees with the constructed 14551): `[applied]` → `--- FAIL: TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory (0.48s)`; `input/udp/udp.go` restored, sha256 `8fd1db37…a647` before and after.
- [x] 4.4 Delete the `interface_mismatch` branch in `composition` → `TestValidateReportsInterfaceMismatch` and
      `TestValidateFindingsVocabularyIsClosed` MUST fail.
      DONE: `[applied]` → `--- FAIL: TestValidateFindingsVocabularyIsClosed (0.01s)` and `--- FAIL: TestValidateReportsInterfaceMismatch (0.00s)`; `composition/analyze.go` restored, sha256 `b7262f3f…b6c0` before and after.
- [~] 4.5 Delete the boot refuse (keep the log) → `go test -race -tags=integration ./service/ -run
      TestComponentManagerRefusesBootOnErrorFinding -v` MUST fail.
      NOT RUN: the refuse is not flipped (3.6 `[~]`); there is nothing to omit until the owner rules. 4.12 below covers the boot analysis wiring instead.
- [x] 4.6 Reintroduce a local status computation in `handleFlowValidation` → `TestFlowValidationHandlerProjectsLibraryResult`
      MUST fail.
      DONE: `[applied]` (a local errors→warnings→valid derivation in `handleFlowValidation`) → `--- FAIL: TestFlowValidationHandlerProjectsLibraryResult (0.00s)`; `service/component_manager_http.go` restored, sha256 `86551daa…563c` before and after.
- [ ] 4.7 (#1093) Delete the rehomed reporter registration → `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`
      MUST fail.
- [x] 4.8 Delete edge rendering in `Mermaid` → `TestCLIGraphMermaidRendersEveryEdge` MUST fail.
      DONE: `[applied]` → `--- FAIL: TestCLIGraphMermaidRendersEveryEdge (0.01s)`; `composition/mermaid.go` restored, sha256 `28ed7fa2…2ad2` before and after.
- [ ] 4.9 (#1093) Re-add `"flow-builder"` to `service/register.go` → `TestServiceRegistryHasNoFlowBuilder` MUST fail.
- [x] 4.10 Delete the composition-tools registration under the `component_catalog` gate
      (`register_component_catalog.go:30`) → `[applied]` → `--- FAIL: TestValidateCompositionToolReturnsFindings (0.00s)`,
      `--- FAIL: TestCompositionGraphToolReturnsMermaid (0.00s)`; restored, sha256 `82377807…3c4f` before and after.
- [x] 4.11 Delete the `default_ports` assignment in `composition.Catalog` → `[applied]` →
      `--- FAIL: TestCatalogCarriesDefaultPortsOrRequiresConfig (0.06s)`, `--- FAIL: TestListComponentsCarriesPorts (0.00s)`;
      `composition/catalog.go` restored, sha256 `a4e8d681…764d` before and after.
- [x] 4.12 Delete the `analyzeBootComposition()` call before `SealComposition` (`component_manager.go:342`) →
      `[applied]` → `--- FAIL: TestComponentManagerExposesBootFindings (0.26s)` (integration); `service/component_manager.go`
      restored, sha256 `1f23b5f9…4017` before and after.
- [x] 4.13 Not claimed: the sorted iteration in `flowgraph` is exercised by `TestValidateIsDeterministic` and
      `TestMermaidIsDeterministic` over five runs; a map-iteration mutation can pass those by chance, so no omission is
      recorded for it (a subtest that can pass under the mutation proves nothing).
- [x] 4.14 `--validate` prints the old "✓ Configuration is valid" and returns nil → `[applied]` →
      `--- FAIL: TestValidateFlagReportsCompositionFindings (1.05s)`; `cmd/semstreams/main.go` restored, sha256
      `e3addbe7…eca0` before and after.
      All omissions: full log in the session scratchpad `mutations.log`; `git status --porcelain` → 0 lines and
      `go build ./...` OK after the sequence.

## 5. Schema regeneration

- [x] 5.1 `task schema:generate`; commit the `schemas/*.v1.json` `default_ports` rows, the removed `/flows*` rows and
      `Flow*` schemas, and the changed `/flowgraph` and `/validate` response schemas; delete
      `schemas/workflow-definition.v1.json` (stale: no factory, `cmd/openapi-generator/main.go:94`) and record it.
      Second `task schema:generate` → `git diff --exit-code schemas/ specs/openapi.v3.yaml` clean.
      DONE (#1092 half): the regenerated `schemas/*.v1.json` (`x-component-metadata.default_ports` for 23 factories,
      `ports_require_config` + `ports_error` for 10) and `specs/openapi.v3.yaml` (`/validate` → `#/components/schemas/Result`,
      `/flowgraph` → `#/components/schemas/Graph`, both schemas emitted from `composition.Result`/`composition.Graph` by
      reflection; `format` query parameter on `/flowgraph`) landed in the GREEN commit `aa70317c` (34 files,
      +1416/−41) so that commit's contract tests are green. Second `task schema:generate` on the §4 head →
      `git diff --exit-code --stat schemas/ specs/openapi.v3.yaml` → NO-DRIFT; `task schema:check-changes` → clean;
      `go test ./test/contract/...` → `ok github.com/c360studio/semstreams/test/contract 2.866s`.
      NOT this PR (#1093, the removal surface): the removed `/flows*` rows and `Flow*` schemas;
      `schemas/workflow-definition.v1.json` (stale, no factory) stays — `test/contract` keeps it in `nonComponentSchemas`
      and `TestSchemaExportCarriesDefaultPorts` skips it by that map.

## 6. Standard gates — record each command and its result

- [x] 6.1 `task lint`.
      DONE on `38155919`+§6 head: `task lint` → exit 0, revive `0 problems` (two `empty-block` warnings fixed at the GREEN head; log: scratchpad `lint_final.log`).
- [x] 6.2 `go test -race ./...`.
      DONE at the GREEN head (`aa70317c`, code unchanged since): every package `ok`, `grep -E '^FAIL'` → no FAIL lines (log: scratchpad `unit1.log`, 153 `ok` lines after the two fixes recorded in 3.10 re-ran green).
- [x] 6.3 `go test -race -tags=integration -p 2 ./...`.
      DONE on `38155919` (`-count=1`): 155 packages `ok`, `EXIT=0`, `grep -E '^(FAIL|--- FAIL|panic:)'` → no FAIL lines (log: scratchpad `integration_full.log`). The two `[~]` target-state tests report `--- SKIP` naming their tasks.
- [x] 6.4 `task build`.
      DONE: `Built bin/semstreams`; CI line `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s -X main.version=local" -o semstreams-linux-amd64 ./cmd/semstreams` → exit 0 (29876386 bytes). `go vet -tags=integration ./...` → clean. `openspec validate composition-validation-substrate --strict` → `Change 'composition-validation-substrate' is valid`.
- [x] 6.5 `go test ./test/contract/...`.
      DONE: `ok github.com/c360studio/semstreams/test/contract 2.866s` on the regenerated schemas (5.1).
- [x] 6.6 `task e2e:core` for this PR (#1092: step 1 of the design's per-step table is BREAKING for adopter
      components; `task e2e:core` is the covering tier it names). `task e2e:crud-tools` and `task e2e:agentic` are
      the retirement PR's (#1093) gate on the head that carries 3.8; paste each tier summary here when it runs.
      DONE for #1092 on `38155919`: `task e2e:core` → `[OK] Readiness and heartbeat report 12/12 healthy components` ·
      `msg="Scenario PASSED" name=core-health` · `msg="Scenario PASSED" name=core-dataflow` (duration 36.3s) ·
      `msg="Scenario PASSED" name=core-graph-roundtrip` · `[OK] SIGTERM exited 0, released listeners, completed shutdown,
      and left NATS healthy` · `[OK] Early SIGTERM canceled blocked NATS boot, exited 1, and fenced service startup` ·
      `EXIT=0` (log: scratchpad `e2e_core.log`). Every shipped factory in `configs/protocol-flow.json` passed the P1
      parity check at boot admission and every tiered `Setup` pre-flight (`CheckFlowHealth`) decoded the new
      `composition.Result` shape.
- [ ] 6.7 (#1093) Downstream measurement (read-only): `cd ~/Code/c360/semteams && go vet ./cmd/semteams/` against a
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
- [ ] 7.5 (#1093) `openspec archive composition-validation-substrate` with the spec sync as the final content commit; the
      narrow reviewer check of the archive/spec sync follows as a PR comment; then undraft. The PR body is a
      published layer: re-read it at undraft and correct any claim the branch no longer supports.

## 8. Not in scope (recorded so the archiver does not infer completion)

- A next-boot component-configuration write verb (design §7 item 1).
- `POST <components>/validate` with a draft body.
- Unifying merge-vs-replace port-override policies across factories.
- The e2e client's gateway filter (`observability.go:378-392`).
- semstreams-ui and semteams migrations (owners' work; instructions in the migration document).
- #1087's four scenarios (their routes no longer exist).
