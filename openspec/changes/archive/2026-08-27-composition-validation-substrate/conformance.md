# Conformance — composition-validation-substrate

Per-decision map from ADR-100 (`docs/adr/100-compositions-are-validated-diagrams-are-projections.md`, decisions
D1–D5) and the design's primitives P1–P7 (`docs/proposals/gh1089-flow-boundary-design.md`) to the code, spec delta,
and test that carry them. Every `__` is replaced with the measured `file:line` at the head that carries the last
change to any `.go` file or spec delta on the branch (tasks 7.3). `tasks.md` rows cite section numbers.

| # | Decision / primitive | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| D1 | The unit of composition is `config.Components` + the catalog; connections are derived; a diagram is a projection | `composition/validate.go:19` (`Validate`), `composition/graph.go:13` (`Graph`), `composition/analyze.go:16` (`Analyze`) | `specs/composition-validation/spec.md` "Composition validation is a pure function…", "…projection…" scenarios | `TestValidateIsDeterministic`, `TestGraphProjectionMatchesAdmittedComposition` — both PASS |
| D2 | Port declarations are static facts of a factory; boot verifies them | `component/registry.go:40` (`PortDeclarer`), `:64` (`Registration.Ports`), `:173` (nil rejection), `:300` + `:394` (parity compare in `prepareComponent`); 33 factory declarers + 5 same-seam registrations (`tasks.md` 3.1 list); objectstore instance name `[~]` (`tasks.md` 3.1a, FILED #1106; noted in the delta under this requirement) | "Port declarations are static facts of a registration" | `TestRegisterFactoryRejectsNilPortDeclarer`, `TestAdmissionRefusesPortDeclarationMismatch`, `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory` (33/33) — all PASS |
| D3 | One validator, one vocabulary, two evidence classes; boot refuses on error | `composition/findings.go:15-41` (13 constants), `:67` (`severityOf`, the one severity table), `composition/analyze.go` (`Analyze(declarations, streams)`; explicit-stream coverage `explicitStreamCovers` keyed on the subscriber's declared stream name and `flowgraph.SubjectCovers` — cover, not overlap); `service/component_manager.go` (`bootStreams` captured from the boot configuration and passed to `Analyze`; `analyzeBootComposition` refuses `Initialize` on an error finding); external-boundary marker `component/ports.go` (`PortDefinition.External`), `composition/analyze.go` (`externalInputs`); `service/component_manager.go` `analyzeBootComposition` (called before the seal on both Initialize paths; retains `bootFindings`; refuses with every finding and its remedy in the error text); `service/component_manager_http.go` `handleFlowValidation` (projection only) | "…one closed findings vocabulary", "Boot validates the admitted composition at the real boundary" | `TestValidateFindingsVocabularyIsClosed`, `TestComponentManagerRefusesBootOnErrorFinding`, `TestFlowValidationHandlerProjectsLibraryResult`, `TestValidateSuppressesOrphanOnlyForExternallyFedInput` — all PASS |
| D4 | Products get `catalog`/`validate`/`graph`; no authoring store or write verb | `composition/cli/main.go:54` (`Main`), `:37` (`Dispatch`); `cmd/semstreams/main.go:86,108,165`; `composition/assert.go:14`; `processor/agentic-tools/executors/composition_tools.go` + `register_component_catalog.go:30` | "Every binary can expose the composition verbs…", "Product CI can assert…", "Agents read the catalog…" | `TestCLIValidateExitsNonZeroOnErrorFindings`, `TestCLICatalogPrintsEveryRegisteredFactory`, `TestCLIGraphMermaidRendersEveryEdge`, `TestValidateFlagReportsCompositionFindings`, `TestAssertValidFailsOnErrorFinding`, `TestValidateCompositionToolReturnsFindings`, `TestCompositionGraphToolReturnsMermaid`, `TestListComponentsCarriesPorts` — all PASS |
| D3b | The framework serves ONE composition judgment: `<components>/gaps` — a second interpreter of the same analysis with its own severity table, which called an `external` input a critical orphan — is removed with its route, handler, OpenAPI row, generated-document entry, and the Go surface only it reached (`ComponentManager.ValidateFlowConnectivity`, `DetectObjectStoreGaps`, `ComponentGap`, `isStorageComponent`, `hasIncomingEdges`, `flowGraphCache.lastAnalysis`) | `service/component_manager_http.go:59` (`RegisterHTTPHandlers`; no `gaps` route), `:216`/`:253` (the OpenAPI path map runs `/flowgraph` straight to `/paths`), `service/component_manager.go:1495` (`flowGraphCache`, no `lastAnalysis`), `:1551` (`GetFlowPaths`, the last caller of `GetFlowGraph`) | `specs/composition-validation/spec.md` "The framework serves one composition judgment and no second gap analysis" (with the `[~]` naming the ONE second judgment that survives at this head — the served `POST <flowbuilder>/flows/{id}/validate` → `engine/validator.go:309` `convertAnalysisToResult`, its own severity table, no `External` check, deleted by `flow-authoring-retirement` 3.2 with the surface it serves — and retaining `AnalyzeConnectivity`, `GetFlowGraph`, and `/paths` for #1093) | `TestComponentGapsOperationIsAbsent`, `TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation` — both PASS; restore-mutation recorded in `tasks.md` 9.1 |
| D5 | Retirement without aliases; the override-expiry metric survives | NOT this change — the whole of D5 moved to `openspec/changes/flow-authoring-retirement/` (#1093) with its deltas, tasks, and conformance table | `flow-authoring-retirement/specs/{flow-authoring,component-runtime-config,composition-validation}/spec.md` | `flow-authoring-retirement/conformance.md` rows D5.a–D5.g |
| P1 | Catalog export carries default ports | `cmd/openapi-generator/main.go:82` + `ComponentMetadata.DefaultPorts/PortsRequireConfig/PortsError` (under `x-component-metadata`); `composition/catalog.go:51` | "…the catalog carries default ports…" scenario | `TestCatalogCarriesDefaultPortsOrRequiresConfig` (23 declare / 10 require on the 33-factory registry), `TestSchemaExportCarriesDefaultPorts` — both PASS |
| P2 | The move from `engine` is complete (dropped-step detector) | `composition/engine_parity_integration_test.go` (exists until `flow-authoring-retirement` 3.2 deletes the engine; oracle given full deps via `engine/validator.go:37` `NewValidatorWithDependencies`; last green run recorded in `tasks.md` 3.2) | — | `TestValidateMatchesEngineFindingsForShippedConfigs` PASS over 22 shipped configs, two-way (deleted with the engine; last green output pasted in 3.2) |
| P5 | Shipped compositions validate clean before the refuse flips | `tasks.md` 3.5 measurement: 22/22 clean after H1 (config fix), H2 (explicit streams), and the H4 ruling (external-boundary marker); refuse flipped in `service/component_manager.go` `analyzeBootComposition` | "Shipped compositions carry no error finding" | `TestValidateShippedConfigsHaveNoErrorFindings` PASS (measurement verbatim in 3.5) |
| P6 | Mermaid | `composition/mermaid.go:12`; `service/component_manager_http.go:578` (`format=mermaid`) | "…projection…" scenarios | `TestMermaidIsDeterministic`, `TestCLIGraphMermaidRendersEveryEdge` — both PASS |
| E2E | BREAKING gate | `tasks.md` 6.6 | — | `task e2e:core` for #1092 (step 1 of the design's per-step table); `task e2e:crud-tools`, `task e2e:agentic` are #1093's |
| R3 | Review round 3 (Fable, `e67901b9`): APPROVE WITH CHANGES, 0 BLOCKING / 0 HIGH / 2 MEDIUM / 3 NIT | M1 `docs/operations/migration-beta162-to-beta163.md:130,166` (served prefix corrected to `/components/gaps` per `service/service_manager.go:1683`); M2 the `[~]` reworded in the delta and `tasks.md` 9.1; NIT-1 `service/component_manager_gaps_removed_test.go` `declaredMethods`; NIT-2 three intersection rows in `component/flowgraph/subject_cover_test.go`; NIT-3 recorded in `flow-authoring-retirement` 3.3 | `specs/composition-validation/spec.md` `[~]` under the one-judgment requirement | `TestComponentGapsOperationIsAbsent`, `TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation`, `TestSubjectCoversIsDirectionalCover` — all PASS (`tasks.md` 9.7) |
| DEVIATION | (1) objectstore declarer ignores its instance-name parameter (`tasks.md` 3.1a; `[~]` note under the P1 requirement in the delta) — FILED #1106, awaiting the owner's ruling, not executed. The former (2) — refuse withheld / shipped configs unmet — was resolved by the H4 ruling (external-boundary marker) and no longer exists. | | | `TestValidateShippedConfigsHaveNoErrorFindings`, `TestComponentManagerRefusesBootOnErrorFinding` PASS |

## Review record

| Round | Reviewer / head | Verdict | Dispositions |
|---|---|---|---|
| 1 | `semstreams-reviewer` (Fable), `0b00749d` | REQUEST CHANGES — 0 BLOCKING, 4 HIGH, 6 MEDIUM | H1 FIXED (research-graph configs `component.*` → `component.>`, grammar control amended); H2 FIXED (`Analyze(declarations, streams)`; explicit streams reach both evidence classes); H3 FIXED (objectstore `[~]` written into the delta) → FILED #1106; H4 RULED BY OWNER (external-boundary marker, option ii) then IMPLEMENTED; M1, M2, M5, Q8, NITs FIXED; M4 FILED #1107 (verbs judge the full catalog vs `Selected(cfg)` boot gating), owner ruled no registry-builder parameter |
| 2 | `semstreams-reviewer` (Fable), `0b00749d..be38605a` | APPROVE WITH CHANGES — 5 MEDIUM | M1 FIXED (stale "pending ruling" text purged); M2 FIXED (remedy suggestion on the required no-publisher orphan and in the refusal text); M3 FIXED (`SubjectCovers` — cover, not overlap — keyed on the subscriber's declared stream name; omissions 4.19/4.20); M4 FIXED (`external` on a non-input refused at resolution; omission 4.21); M5 FIXED (3.5c wording) |
| 3 | `semstreams-reviewer` (Fable), `e67901b9` | APPROVE WITH CHANGES — 0 BLOCKING, 0 HIGH, 2 MEDIUM, 3 NIT | M1 FIXED (served path `/components/gaps` per `service/service_manager.go:1683`, generated key `/gaps`); M2 FIXED (the `[~]` names the surviving SERVED second judgment); NIT-1 FIXED (`declaredMethods`); NIT-2 FIXED (wildcard-intersection rows); NIT-3 RECORDED in `flow-authoring-retirement` 3.3 (dead `ValidationStatus`) |
| Codex 1 | owner-run, `bad4a1af` | REQUEST CHANGES — 1 BLOCKING, 1 MEDIUM | BLOCKING FIXED by RETIREMENT (`/gaps` bypassed the canonical judgment: boot `status=warnings errors=0` vs `/gaps` `no_publishers, required=true, critical_port_count=1, has_issues=true` for one required external input) — owner ruling "we do not need to maintain legacy paths — we break it and document it for migration by downstream at this stage"; MEDIUM FIXED by REMOVAL (`flowgraph.SubjectMatches` phantom export) |
| Codex 2 | owner-run, `c851d0be` | **APPROVE** — no actionable findings | Independently rechecked every round-1 disposition and verified: `go test -race -count=1 ./service ./component/flowgraph ./composition/...` PASS · `go test -race -count=1 -tags=integration ./service ./componentregistry ./composition` PASS · `go test -count=1 ./test/contract/...` PASS · `openspec validate --all --strict` 55 passed, 0 failed · "Hosted CI: all seven reported checks green". Recorded verbatim: "The remaining 7.4 reconciliation, archive-as-final-content-commit, and narrow archive/spec-sync check are normal landing steps, not review findings." |

## Task 7.4 reconciliation — every scenario names a test that exists and is green

`specs/composition-validation/spec.md` (ADDED), 26 scenarios. Unit unless the tag column says otherwise.

| # | Scenario | Test | Package | Tag |
|---|---|---|---|---|
| 1 | a registration without a port declarer is rejected at registration | `TestRegisterFactoryRejectsNilPortDeclarer` | `component` | — |
| 2 | every shipped factory's declaration equals its constructed ports | `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory` | `componentregistry` | `integration` |
| 3 | a lying declarer fails admission | `TestAdmissionRefusesPortDeclarationMismatch` | `component` | — |
| 4 | the catalog carries default ports or says the factory needs configuration | `TestCatalogCarriesDefaultPortsOrRequiresConfig`, `TestSchemaExportCarriesDefaultPorts` | `composition`, `test/contract` | — |
| 5 | the vocabulary is closed | `TestValidateFindingsVocabularyIsClosed` | `composition` | — |
| 6 | an unknown factory is a finding, not an error return | `TestValidateReportsUnknownComponent` | `composition` | — |
| 7 | a required stream input with no publisher is an error | `TestValidateReportsRequiredStreamInputWithoutPublisher` | `composition` | — |
| 8 | an externally fed required input is not an orphan | `TestValidateSuppressesOrphanOnlyForExternallyFedInput` | `composition` | — |
| 9 | interface contracts are checked on every derived edge | `TestValidateReportsInterfaceMismatch` | `composition` | — |
| 10 | a JetStream subscriber fed only by core-NATS publishers is an error | `TestValidateReportsStreamRequirement` | `composition` | — |
| 11 | an explicit stream declaration satisfies a JetStream subscriber | `TestValidateStreamRequirementSatisfiedByExplicitStream`, `TestValidateStreamRequirementNeedsTheNamedStream`, `TestValidateStreamRequirementNeedsCoverNotOverlap`, `TestComponentManagerBootFindingsHonourExplicitStreams` | `composition`, `service` | last is `integration` |
| 12 | pattern conflicts and exclusive resources are findings | `TestValidateReportsConnectionPatternConflict`, `TestValidateReportsExclusiveResourceConflict` | `composition` | — |
| 13 | validation is deterministic | `TestValidateIsDeterministic` | `composition` | — |
| 14 | validate exits non-zero on an error finding | `TestCLIValidateExitsNonZeroOnErrorFindings` | `composition/cli` | — |
| 15 | catalog lists every registered factory | `TestCLICatalogPrintsEveryRegisteredFactory` | `composition/cli` | — |
| 16 | graph renders every derived edge in Mermaid | `TestCLIGraphMermaidRendersEveryEdge` | `composition/cli` | — |
| 17 | the legacy flag reports the same findings | `TestValidateFlagReportsCompositionFindings` | `cmd/semstreams` | — |
| 18 | the helper fails on an error finding and passes on warnings | `TestAssertValidFailsOnErrorFinding` | `composition` | — |
| 19 | an error finding refuses boot | `TestComponentManagerRefusesBootOnErrorFinding` | `service` | `integration` |
| 20 | the HTTP validate operation projects the boot result | `TestComponentManagerExposesBootFindings`, `TestFlowValidationHandlerProjectsLibraryResult` | `service` | first is `integration` |
| 21 | the projection of the running composition matches the admitted declarations | `TestGraphProjectionMatchesAdmittedComposition`, `TestMermaidIsDeterministic` | `service`, `composition` | first is `integration` |
| 22 | the gap operation is absent from the routed and advertised surface | `TestComponentGapsOperationIsAbsent` | `service` | — |
| 23 | an externally fed input is never a critical orphan on any component operation | `TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation` | `service` | — |
| 24 | validate_composition returns the same findings as the library | `TestValidateCompositionToolReturnsFindings` | `processor/agentic-tools/executors` | — |
| 25 | composition_graph renders Mermaid and list_components carries ports | `TestCompositionGraphToolReturnsMermaid`, `TestListComponentsCarriesPorts` | `processor/agentic-tools/executors` | — |
| 26 | the shipped configurations validate clean | `TestValidateShippedConfigsHaveNoErrorFindings` | `composition` | — |

`specs/component-runtime-config/spec.md`, the MODIFIED requirement "Component ports have one strict canonical
grammar", 9 scenarios. Only scenario 1's marker clause is new here; the other eight are pre-existing current truth
restated verbatim (see the RECONCILIATION FINDING below), and their tests predate this change.

| # | Scenario | Test | Package |
|---|---|---|---|
| 1 | Canonical definition and runtime views agree | `TestPortDefinitionExternalRoundTrip` (the marker clause, named in the scenario), `TestPortDefinitionAndPortUseOneStrictWire`, `TestFactsForPortPreservesStreamAndInterfaceFacts` | `component` |
| 2 | Legacy declaration fails without repair | `TestPortCodecRejectsUnknownAndLegacyShapes` | `component` |
| 3 | Named merge is complete replacement | `TestMergePortConfigCompleteReplacementStableOrderAndClone` | `component` |
| 4 | Invalid named merge is rejected | `TestMergePortConfigRejectsInvalidOverrides`, `TestPortConfigJSONRejectsDuplicateNamesWithinEachLane` | `component` |
| 5 | JetStream fields survive canonical round-trip | `TestPortDefinitionAndPortUseOneStrictWire` (drives `completeJetStreamPort`: subjects, storage, retention, days, size, replicas, consumer, deliver/ack policy, max deliver, ack wait, heartbeat, max ack pending, interface), `TestFactsForPortPreservesStreamAndInterfaceFacts` | `component` |
| 6 | Subject-only JetStream input is rejected | `TestPortConfigJSONResolvesJetStreamDefinitionsByLane/subject-only_input_fails_with_typed_stream_identity_context`, `TestResolvePortRejectsInvalidDeclarations` (row "jetstream input missing stream name") | `component` |
| 7 | JetStream input without subjects is rejected | `TestResolvePortRejectsInvalidDeclarations` (row "jetstream input missing subjects") | `component` |
| 8 | Subject-only JetStream output remains valid | `TestPortConfigJSONResolvesJetStreamDefinitionsByLane/subject-only_output_remains_valid`, `TestResolveJetStreamOutputAllowsProvisionerOwnedName` | `component` |
| 9 | Retired agentic-model stream default is absent | `TestShippedAgenticModelConfigDoesNotExposeLegacyStreamName`, `TestShippedJetStreamInputsDeclareBackingStreamAndSubjects` | `test/contract` |

**No scenario in either delta lacks a test.** Every test above was run at the reconciliation head and passed; the
exact commands and results are `tasks.md` 7.4.

**RECONCILIATION FINDING (found by 7.4, fixed before the archive).** The MODIFIED block for
`component-runtime-config` carried only 2 of the requirement's 9 scenarios. An OpenSpec MODIFIED requirement REPLACES
the whole requirement, scenarios included — the archived precedent
`openspec/changes/archive/2026-08-08-foundation-b-port-language/specs/component-runtime-config/spec.md` restates all 9,
which is exactly why `openspec/specs/component-runtime-config/spec.md` has 9 today. (Correction from the narrow archive
check: openspec 1.7.0 REFUSES a MODIFIED block that omits or renames a current scenario — `specs-apply.js:285-288`,
verified on a scratch copy of the 2-of-9 state: `Aborted. No files were changed.`, exit 1 — so the restoration was
required for the archive to run, not to prevent a silent drop; a silent drop is reachable only via `--skip-specs` or
a hand sync.) Archiving as written would have been refused for seven scenarios of permanent current truth: "Named merge is complete replacement", "Invalid named
merge is rejected", "JetStream fields survive canonical round-trip", "Subject-only JetStream input is rejected",
"JetStream input without subjects is rejected", "Subject-only JetStream output remains valid", and "Retired
agentic-model stream default is absent". All seven were restored verbatim from the current spec before archiving; the
header sets and their order are now identical, and the block's only remaining difference from current truth is the
intended `external` grammar.

## Archive / spec-sync check — `semstreams-reviewer` (Fable) at `671a92d7`: ARCHIVE OK

Scope: `3ad2f008` (restoration) + `671a92d7` (archive). `3ad2f008` judged a pure restoration (3 files under `openspec/`; delta
diff = 50 appended lines, 0 removed; each restored scenario byte-identical to main's spec) — no re-entry to full review.
`component-runtime-config` header sets 47/47, diff empty, dupes 0; body change = the `external` grammar paragraph, the
scenario-1 marker clause, one archiver-added trailing blank line. `composition-validation` synced spec == archived delta
(8 requirements, 26 scenarios, real Purpose, both `[~]` blocks present). Archive dir carries proposal/tasks/conformance +
both deltas (no design.md ever existed; the design is `docs/proposals/gh1089-flow-boundary-design.md`);
`flow-authoring-retirement` untouched and open; `openspec validate --all --strict` 55/55; no ticked task asserts a
post-merge fact; 7.1/7.2/7.4 records match the PR comments verbatim. 7.4 spot-check 6/35 rows: every named test exists
and asserts its scenario's THEN; focused unit + integration commands re-run green. Findings: MEDIUM — the "silently
deleted" mechanism claim (corrected above); NIT — 7.5 mixed wording (disclosed as NOT DONE at the time); NIT — archiver
trailing blank line; NIT — `ports_merge_test.go` exercises the kind-specific-fields AND clause through a common field
(pre-existing test and scenario). Observation: `assertPortsEqual` in the registry parity test does not compare
`External`; the marker's parity is covered by `component/registry_test.go:302`.
