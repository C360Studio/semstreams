# Tasks — single-type-authority

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing / deliberate /
"not done" / "still open" in any OPEN task line as a live caveat. They appear only in the RED-capture task 2.9, which the
queue flags while OPEN by design. Everywhere else say "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (measured at `c3a17741`, re-read unchanged at `7e7ea76e` — #1099 and #1104 merged; milestone `v1.0.0-beta.163` holds #1100; `AGENTS.md` Land bullet `:68-73`):
`graph/inference/hierarchy.go:427-440` (container birth, empty `MessageType`) → `processor/graph-ingest/component.go:451-456` (adapter), `:1893-1896` (`CreateEntity`),
`:2081-2132` (`createEntityWithReceipt`: `ValidateEntityStateContract :2093`, `reconcileIndexingProfile :2121`, `entityBucket.Create :2132`) — disjoint from
`canonical_mutations.go:243`; direct bucket writers are exactly `canonical_mutations.go:243,306` and `component.go:2132` (B-1); `configs/e2e-structural.json:480`,
`configs/agentic.json:182` (`enable_hierarchy: true`); `executors/httprequest.go:28,257-266` and `websearch.go:31,255-262` (two sources, two unconditional sets);
`emit_diagnosis.go:259-265` (`Confidence: args.Confidence` on every triple); `test/e2e/scenarios/agentic/scenario.go:786-800,838-848` (loop asserted; model endpoint a warning);
`openspec/changes/entity-id-segment-semantics/tasks.md` 5.1 (edits the five entity files, the lesson prefix `:85-93`, `internal/builtinprojection/contracts.go:26,56`);
`go list -deps ./graph/inference | grep -c 'payloadbuiltins\|processor/graph-ingest'` → 0 and the reverse → 0; `semconnect/cmd/cs-api-server` calls no `payloadbuiltins.Register`/`payloadregistry.New`;
`pkg/projection/contract.go:102-104` (`ValidateContracts` rejects an empty set → a composition root without the spread cannot boot); `payloadregistry/registry.go:43-52,78-132,189-253` (Registration, Register, copies);
`pkg/projection/contract.go:12-14,36-43,46-98` and `errors.go:6`; `go list -deps ./payloadregistry` → `pkg/retry pkg/errs pkg/types`;
`go list -deps ./vocabulary` → `pkg/platform`; `go list -deps ./pkg/projection` includes `graph message natsclient internal/graphmutation`;
`go list -deps ./payloadbuiltins | grep -c processor/graph-ingest` → 0; `internal/builtinprojection/contracts.go:12-80` with consumers
`processor/agentic-tools/lesson_promotion.go:52,170-171`, `write_todos.go:196-197`, `cmd/semstreams/main.go:221`, `cmd/e2e-semstreams/main.go:154`
(`service.WireGraphRuntime`, `service/graph_runtime.go:16-21`); `processor/graph-ingest/indexing_profile_registry.go:30-76` (22 keys, all
registered: `agentic/payload_registry.go:20-36`, `processor/agentic-dispatch/payload_registry.go:42`, `agentic/research/register.go:16-58`);
`component.go:113-117,134-138,646,692,1264-1290,1704-1760,1864-1868,2036`; `canonical_mutations.go:199-240` (`:207` `IsValid`);
`mutation_runtime.go:198-204`; `graph/mutation_responses.go:10-52`; `graph/entity_predicate_contract.go:134-175`; `message/base_message.go:301-307`;
`message/decoder.go:39-44`; the six stamps `agentic/{agent_lesson:31-37,loop_execution:68-73,167-173,model_endpoint:25-31,ops_diagnosis:25-31,web_observation:25-27}_entity.go`
and `pkg/lifecycle/manager.go:24-28,400-407`; writers `processor/agentic-tools/emit_lesson.go:34,204-216,518,527,693-741`,
`emit_diagnosis.go:26,203-204,249-291`, `executors/web_emit.go:55-73`, `executors/httprequest.go:267`, `executors/websearch.go:264`,
`processor/agentic-loop/graph_writer.go:24,254,474,511-548`, `frameworkcapabilities/graphresearch/executor.go:387`,
`processor/research-graph-llmwrap/triplepub.go:94-100,167`; `_Distinct` tests `agentic/{model_endpoint:27,agent_lesson:111,ops_diagnosis:111,loop_execution:337}_entity_test.go`;
test stamps `test/e2e/scenarios/{graph_roundtrip.go:207,233, lessons/scenario.go:374,391, crud-tools/scenario.go:684, tiered_structural.go:444,502,1273,
research-graph/scenario.go:350-352, ops/scenario.go:459-470}`, `processor/graph-ingest/{indexing_profile_test.go:29,75-263, canonical_mutations_test.go:653}`,
13 test files with `CreateEntityRequest{`; `processor/graph-ingest/metrics_test.go:147-156` (`newTestDependencies`, no registry);
`payloadbuiltins/register.go:33-49`, `register_test.go:10-13`; `payloadregistry/testing.go:16-40`; `grep -rn 'CreateMutation{' --include='*.go'` non-test → only
`test/e2e/scenarios/graph_roundtrip.go:105`, `lessons/scenario.go:388` (L1); 23 `&Component{` literals in six `processor/graph-ingest/*_test.go` files
(`readiness_test.go` 14, `lifecycle_owner_test.go` 4, `keyed_ingest_test.go` 2, `batch_unit_test.go`, `component_test.go`, `query_contract_guard_test.go`) with no registry (L2);
42 `_test.go` stamp sites, ≥14 distinct keys (D3); `cmd/semstreams/main.go:766-770` + `agentic/research/register.go:10-14` (research registered only when selected, D1);
`go list -deps ./message | grep -c vocabulary` → 0, `./payloadregistry` → 0 (L5); `cmd/semstreams/main.go:80,214,761-772`,
`cmd/e2e-semstreams/main.go:147,358-378`; `vocabulary/predicates.go:325-332`; predicates `vocabulary/agentic/predicates.go:253,352-387,756-790,837-928,1034-1115`.

## 1. Claim

- [ ] 1.1 Worktree `../semstreams-wt/claude/gh1100-single-type-authority` from `origin/main`; draft PR #1102 open with
      `Closes #1100` and `implemented-by: <persona>` in the body; this change directory,
      `docs/adr/103-payload-registry-is-the-single-type-authority.md`, and the two `docs/proposals/gh1100-*` documents are
      its first commit (`9899d71d`). Rebase onto `7e7ea76e` or later before implementation begins. The PR body is a published
      layer: it carries the sister migration list (design §11), the new outcome code and its detail key, the metric's new
      meaning, and the owner rulings on O-1…O-18 as they land.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `payloadregistry/attributes_test.go`: `TestRegisterRejectsInvalidIndexingProfile` (`"prose"` → error naming the value);
      `TestRegisterFillsAndChecksContractMessageType` (empty → filled with the key; a different key → error naming both);
      `TestGetRegistrationCopiesAttributes` (profile present; mutating the returned contract slice does not change a later read);
      `TestContractsReturnsIndependentSortedCopies`; `TestIndexingProfileFor` (registered+floor, registered+empty, unregistered);
      `TestRegisterRejectsSchemaMismatch` (a factory whose `Schema()` disagrees — GREEN at baseline, names the existing check at
      `registry.go:261-300` for the delta). Does not compile at baseline (new fields and methods).
- [ ] 2.2 `pkg/projection/contract/contract_test.go`: `TestContractValidateUsesVocabularyProfiles` — every value in
      `vocabulary.{Content,Control,Signal,Trace}` validates and `"prose"` does not; `pkg/projection/contract_test.go` unchanged
      and GREEN at baseline, plus two documenting tests there: `TestContractLiteralCompilesAgainstAliases` (a literal using
      `projection.Contract`, `projection.PredicateGroup`, `projection.ModeReconcile` validates) and
      `TestOverlappingLocalContractsConstruct` (two clients with overlapping contracts both construct). Does not compile at
      baseline (new package).
- [ ] 2.3 `agentic/entity_payloads_test.go`: `TestAgentLessonEntity_RoundTrip`, `TestOpsDiagnosisEntity_RoundTrip`,
      `TestModelEndpointEntity_RoundTrip`, `TestWebObservationEntity_RoundTrip`, `TestLoopExecutionEntity_RoundTrip` — marshal
      a fully populated entity, decode through `message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads))`
      into a fresh value, assert concrete type, field equality, `EntityID()` equality, and predicate-set equality of `Triples()`.
      `TestRegisteredContractMatchesTriples` — table over every registered contract (loop execution and lesson; the three others
      only under O-4 = mint): birth(C) ⊆ predicates(`Triples()` of a fully populated entity) ⊆ birth(C) ∪ groups(C), and a
      predicate removed from the builder but not from the contract fails naming it. `TestWebObservationEntityMatchesToolBuilders`
      — for `Tool = http_request` and `Tool = web_search`, the triple set (predicate, object, source, confidence) equals the
      baseline builder's output captured as a golden literal from `httprequest.go:257-266` / `websearch.go:255-262`, zero-valued
      triples included. `TestOpsDiagnosisEntityStampsArgsConfidence` — every triple carries the entity's `Confidence`. Does not
      compile at baseline.
- [ ] 2.4 `pkg/lifecycle/harness_entity_test.go`: `TestHarnessEntity_RoundTrip` (verbatim triples survive decode).
      `payloadbuiltins/single_type_authority_test.go`: `TestPayloadRegistryIsTheSingleTypeAuthority` — six keys registered with
      non-empty floors; loop execution and lesson carry a contract whose `MessageType` equals the key (the three others only under
      O-4 = mint); `Contracts()` names unique and equal to `{agentic.loop-execution, agentic.lesson-record}` (plus the three
      under O-4 = mint); under O-16 (a) `graph.hierarchy_container.v1` registered with floor `control`; every registration's
      profile empty or valid. `processor/agentic-tools/lesson_promotion_test.go`: `TestLessonProjectionContractIsTheRegisteredContract`
      — `LessonProjectionContract()` equals the contract the builtin registry holds for `agentic.agent_lesson.v1`. Does not
      compile at baseline.
- [ ] 2.5 `processor/graph-ingest/registered_type_gate_integration_test.go` (`//go:build integration`; the package's real NATS
      test client): `TestCreateRejectsUnregisteredMessageType` — registry from `payloadbuiltins.Register`; send `entity.create`
      stamping `test.unknown.v1`; decode the reply into a fresh value; assert code `message_type_unregistered`,
      `detail.message_type == "test.unknown.v1"`, no `ENTITY_STATES` key, `mutation_rejections_total{reason="message_type_unregistered"}`
      == 1. `TestCreateAcceptsRegisteredMessageType` — stamp `agentic.agent_lesson.v1`; entity created; stamp persisted verbatim.
      `TestFloorComesFromRegistration` — `agentic.request.v1` → `trace` with no metric; `test.nofloor.v1` registered via
      `RegisterTestType` → `control` and metric +1. `TestHierarchyContainerBirthCarriesRegisteredType` — `enable_hierarchy: true`;
      ingest a Graphable whose ID implies a container; assert the container exists with `message_type`
      `graph.hierarchy_container.v1` and `indexing_profile_default_total{message_type="unknown"}` unchanged (under O-16 (b): the
      empty stamp and the `unknown` label instead). MUST fail at baseline (the gate does not exist; the floor is table-driven;
      the container has no stamp).
- [ ] 2.6 `processor/graph-ingest/factory_registry_test.go`: `TestFactoryRejectsNilPayloadRegistry` — construction with
      `PayloadRegistry: nil` returns an error naming the dependency. MUST fail at baseline (only `NATSClient` is checked).
      `TestCreateSeamRejectsWhenRegistryMissing` — a `&Component{}` literal with no registry receives an `entity.create`; the
      reply decodes into a fresh value with code `internal`, nothing is written, no panic. `TestInProcessCreateRejectsUnregisteredType`
      — `Component.CreateEntity` with a registry lacking the entity's type returns an invalid error and writes nothing. MUST fail
      at baseline (no guard exists on either path).
      `processor/graph-ingest/resident_stamp_integration_test.go` (`//go:build integration`): `TestResidentUnregisteredStampIsNotPoison`
      — put an entity with `message_type` `legacy.gone.v1` directly into the test bucket; boot; assert no poison inventory
      entry, exact read returns the stamp, and a `triple.append` to it reports `applied`. GREEN at baseline (documents §10 of
      the design; a barrier against a later registry-consulting codec).
- [ ] 2.7 `processor/agentic-tools/emit_lesson_entity_test.go`: `TestEmitLessonBuildsEntityTriples` — for the same args,
      the predicate/object multiset from `AgentLessonEntity.Triples()` equals the multiset the baseline builder produces
      (capture the baseline output as a golden literal in the test before 4.2 moves the builder). Does not compile at baseline.
      `processor/agentic-tools/executors/web_observation_integration_test.go` (`//go:build integration`):
      `TestWebObservationBirthIsRegistered` — `publishWebObservation` against a graph-ingest with the builtin set; assert the
      entity exists with `message_type` `agentic.web_observation.v1`. MUST fail at baseline once 5.2 lands without 4.4; GREEN
      before 5.2 (records the coverage the missing e2e tier would give — O-10).
- [ ] 2.8 `cmd/e2e-semstreams/fixtures/register_test.go`: `TestFixturesRegisterEveryE2EStamp` — the six e2e keys register into
      a fresh registry with floor `control` and round-trip as verbatim carriers. Does not compile at baseline.
- [ ] 2.9 RED capture on baseline code (§2 tests only), recorded here verbatim (package + test name + failing assertion or
      build error):

  ```
  go test -race -count=1 -run 'TestRegisterRejectsInvalidIndexingProfile|TestRegisterFillsAndChecksContractMessageType|TestGetRegistrationCopiesAttributes|TestContractsReturnsIndependentSortedCopies|TestIndexingProfileFor' ./payloadregistry/
  go test -race -count=1 -run 'TestContractValidateUsesVocabularyProfiles' ./pkg/projection/contract/
  go test -race -count=1 -run '_RoundTrip|TestRegisteredContractMatchesTriples|TestWebObservationEntityMatchesToolBuilders|TestOpsDiagnosisEntityStampsArgsConfidence' ./agentic/ ./pkg/lifecycle/
  go test -race -count=1 -run 'TestPayloadRegistryIsTheSingleTypeAuthority' ./payloadbuiltins/
  go test -race -tags=integration -count=1 -p 2 -run 'TestCreateRejectsUnregisteredMessageType|TestCreateAcceptsRegisteredMessageType|TestFloorComesFromRegistration|TestResidentUnregisteredStampIsNotPoison|TestHierarchyContainerBirthCarriesRegisteredType' ./processor/graph-ingest/
  go test -race -count=1 -run 'TestFactoryRejectsNilPayloadRegistry|TestCreateSeamRejectsWhenRegistryMissing|TestInProcessCreateRejectsUnregisteredType' ./processor/graph-ingest/
  go test -race -count=1 -run 'TestLessonProjectionContractIsTheRegisteredContract' ./processor/agentic-tools/
  go test -race -count=1 -run 'TestEmitLessonBuildsEntityTriples' ./processor/agentic-tools/
  go test -race -tags=integration -count=1 -run 'TestWebObservationBirthIsRegistered' ./processor/agentic-tools/executors/
  go test -race -count=1 -run 'TestFixturesRegisterEveryE2EStamp' ./cmd/e2e-semstreams/fixtures/
  ```

## 3. Registry — attributes registered with the type

- [ ] 3.1 Create `pkg/projection/contract` (package `contract`): move `Contract`, `PredicateGroup`, `WriteMode`, `ModeReconcile`,
      `ModeAppend`, `ErrInvalidContract`, `Validate`, `ValidateContracts`, `validateGroupName` from `pkg/projection/contract.go`
      and `errors.go:6`; replace `validIndexingProfiles` with `vocabulary.IsValidIndexingProfile`; add `ValidateShape()` (everything
      `Validate` does except `vocabulary.RequireDeclaredPredicate`). In `pkg/projection` keep `type Contract = contract.Contract`,
      `type PredicateGroup = contract.PredicateGroup`, `type WriteMode = contract.WriteMode`, `const ModeReconcile = contract.ModeReconcile`,
      `ModeAppend`, `var ErrInvalidContract = contract.ErrInvalidContract`, `func ValidateContracts(...) = contract.ValidateContracts`.
      `go build ./... && go vet ./...` clean; `grep -rn 'validIndexingProfiles' --include='*.go' .` → 0.
- [ ] 3.2 `payloadregistry.Registration` gains `IndexingProfile string` and `Contracts []contract.Contract`; `Register` validates
      per the payload-registry delta (profile via `vocabulary.IsValidIndexingProfile`; contract key fill/check; unique contract
      names; `ValidateShape()`); `GetRegistration`/`List`/`ListByDomain` copy both (deep-copy contracts); add
      `IndexingProfileFor(key) (string, bool)` and `Contracts() []contract.Contract` (fresh copies, sorted by key then name).
      `payloadregistry` imports `pkg/projection/contract` and `vocabulary` only; re-measure `go list -deps ./payloadregistry`
      and record it here. Rewrite the package comment at `registry.go:1-16`: the genuinely new transitive dependency is
      `vocabulary` itself (five `init()`s — `hierarchy.go:17`, `labels.go:16`, `lifecycle.go:16`, `relationships.go:7`,
      `rulepacks/predicates.go:37` — and a global predicate registry); `pkg/platform` is already reached through `message`. Name
      the edge `payloadregistry → pkg/projection/contract → vocabulary` and that `message` inherits it.
- [ ] 3.3 `payloadregistry/testing.go`: `RegisterTestType(tb testing.TB, reg *Registry, key string)` — parses the key, registers a
      schema-less stub factory with no floor; `tb.Fatalf` on error. 2.1 GREEN.

## 4. The six framework types

- [ ] 4.1 `agentic/loop_execution_entity.go`: JSON tags on `LoopExecutionEntity`, `Schema()`, `Validate()`, `MarshalJSON` (alias
      idiom); move `LoopExecutionContract()` and the constants `LoopExecutionContractName`, `TodoGroupName` from
      `internal/builtinprojection/contracts.go:12-46` beside the type.
- [ ] 4.2 `agentic/agent_lesson_entity.go`: `AgentLessonEntity` (design §7 fields; `CreatedAt time.Time`; `Status` born
      `proposed`), `EntityID()` via `AgentLessonEntityID`, `Triples()` = the builder from `emit_lesson.go:693-741` with source
      constant `LessonSource = "ops-emit-lesson"` beside the type; `LessonContract()` + `LessonRecordContractName`,
      `LessonLifecycleGroupName` moved from `contracts.go:52-80`. `emit_lesson.go:518,527` constructs the entity and passes
      `entity.Triples()`; `buildEmitLessonTriples` deleted. 2.7 `TestEmitLessonBuildsEntityTriples` GREEN.
- [ ] 4.3 `agentic/ops_diagnosis_entity.go`: `OpsDiagnosisEntity`, `Triples()` from `emit_diagnosis.go:249-291` (source
      `"ops-emit-diagnosis"`; `Confidence` = the entity's field on every triple, `:259-265`); `OpsDiagnosisContract()` only if O-4
      = mint. `emit_diagnosis.go:203-204` uses the entity.
      `agentic/model_endpoint_entity.go`: `ModelEndpointEntity` with plain fields, `Triples()` from `graph_writer.go:511-548`
      (source `"agentic-loop"`), `ModelEndpointContract()`; `graph_writer.go:245-258` constructs the entity; `buildModelEndpointTriples` deleted.
- [ ] 4.4 `agentic/web_observation_entity.go`: `WebObservationEntity` with a `Tool` discriminator (`WebObservationTool`:
      `http_request` \| `web_search`) that selects the source constant (`agent-http-request` from `httprequest.go:28`,
      `agent-web-search` from `websearch.go:31`) and the unconditional emitted set (`:257-266` / `:255-262`, zero values
      included); `EntityID()` via `TryWebObservationEntityID` returning `""` on error; `Validate()` requires a known `Tool`.
      `httprequest.go:267` and `websearch.go:264` construct the entity and pass `entity.Triples()`; the two inline builders are
      deleted. `WebObservationContract()` only if O-4 = mint. `web_emit.go:55-73` unchanged in shape (create, then append on exists).
- [ ] 4.5 `agentic/payload_registry.go`: add the five registrations with `IndexingProfile` (loop_execution `control`, agent_lesson
      `content`, ops_diagnosis `content`, model_endpoint `control`, web_observation `content`) and `Contracts` (loop execution
      and lesson; the three others only if O-4 = mint); add `IndexingProfile:`
      to the existing 15 rows with the values from `indexing_profile_registry.go:32-60` verbatim; same for
      `processor/agentic-dispatch/payload_registry.go:42` (`signal`) and `agentic/research/register.go:16-58` (6 rows — these
      floors exist only in a binary that selects graph research, `cmd/semstreams/main.go:766-770`; design §5, O-14). Delete the
      `MUTATION-ONLY … NOT registered` paragraphs in the five entity files.
- [ ] 4.6 `pkg/lifecycle/harness_entity.go`: `HarnessEntity{ID string; Facts []message.Triple}` (Graphable, Payload, MarshalJSON),
      `RegisterPayloads(reg)` registering `lifecycle.harness.v1` with floor `control`; `lifecycleMessageType` (`manager.go:24-28`)
      becomes `HarnessMessageType()` beside it; `payloadbuiltins.Register` calls `lifecycle.RegisterPayloads`. 2.3, 2.4 GREEN.
- [ ] 4.7 Delete `internal/builtinprojection/` (both files); re-point all seven consumer files: `lesson_promotion.go:52` returns
      `agentic.LessonContract()`; `lesson_promotion.go:170-171`, `write_todos.go:196-197` use the `agentic` constants;
      `cmd/semstreams/main.go:220-222` and `cmd/e2e-semstreams/main.go:153-155` pass `payloadReg.Contracts()...` to
      `service.WireGraphRuntime` (the registry is built at `:214` / `:147`, before the call); `test/e2e/scenarios/ops/scenario.go`,
      `processor/agentic-tools/graph_mutation_integration_helpers_test.go`, `lesson_promotion_test.go` use the `agentic` symbols.
      Delete the four `_Distinct` tests. `grep -rn builtinprojection --include='*.go' .` → 0.
- [ ] 4.8 (O-16 (a); under (b) this task becomes the explicit exemption on the in-process lane and a spec-delta note)
      `graph/inference/container_entity.go`: `ContainerEntity{ID string; Facts []message.Triple}` (Graphable, Payload, MarshalJSON),
      `HierarchyContainerMessageType()` = `graph.hierarchy_container.v1`, `RegisterPayloads(reg)` with floor `control`;
      `payloadbuiltins.Register` calls it; `hierarchy.go:428` stamps `MessageType: HierarchyContainerMessageType()`.

## 5. graph-ingest — floor from the type, gate at the seam

- [ ] 5.1 Retain `payloadRegistry *payloadregistry.Registry` on the component beside `decoder` (`component.go:487,692`); the
      factory (`:646`) returns `errs.WrapInvalid(..., "payload registry is required")` on nil. Delete
      `indexing_profile_registry.go`; `reconcileIndexingProfile` (`:1864`) calls `c.payloadRegistry.IndexingProfileFor(mt.Key())`;
      update the comment block at `:1836-1842` and the metric help at `:113-117` to the new meaning. Rewrite
      `indexing_profile_registry_test.go` against a registry from `payloadregistry.NewWithSubset(t, agentic.RegisterPayloads,
      research.RegisterPayloads, agenticdispatch.RegisterPayloads)` keeping all 22 expectations. 2.6 unit test GREEN.
- [ ] 5.2 `graph/mutation_responses.go`: `ErrorCodeMessageTypeUnregistered = "message_type_unregistered"` with the closed-set
      comment. `canonical_mutations.go:207`: after `IsValid`, `GetRegistration(key)` miss → `rejectInvalidDetail(code,
      {"message_type": key}, err)`, metered through the existing rejection path as `reason="message_type_unregistered"`, WARN log
      naming the key. Implement the lookup as one helper `requireRegisteredMessageType(entity) error` and call it on BOTH create
      paths: `canonical_mutations.go:207` (wrapped as the coded rejection) and the top of `createEntityWithReceipt`
      (`component.go:2081`, before `ValidateEntityStateContract`; returned as `errs.WrapInvalid`; `hierarchy.go:440`'s caller
      logs and does not cache). A nil `c.payloadRegistry` at either seam → `rejectInternal` (`mutation_runtime.go:206-208`) with
      an ERROR log, never a pass-through. 2.5 GREEN; 2.6 GREEN; 2.7 integration GREEN.
- [ ] 5.3 Unit fixtures: `metrics_test.go:147-156` `newTestDependencies` sets `PayloadRegistry` from `payloadbuiltins.Register`
      plus `RegisterTestType` for `test.widget.v1` and `test.fixture.v1`; sweep the other 12 files listed in the premises
      (`grep -rln 'CreateEntityRequest{' --include='*_test.go' .`) so every stamped key is registered in that test's registry.
      Sweep the 42 `_test.go` stamp sites (≥14 distinct keys, inventory §2.2) so every key that reaches the create seam is
      registered in that test's registry.
      `go test -race -count=1 ./processor/graph-ingest/ ./graph/ ./internal/graphmutation/ ./pkg/lifecycle/ ./processor/graph-index/ ./processor/rule/`
      and the same with `-tags=integration -p 2` GREEN.
- [ ] 5.4 `processor/graph-ingest/component_fixture_test.go`: `withTestRegistry(t, c *Component) *Component` sets
      `payloadRegistry` (builtins + `RegisterTestType` for the keys that file stamps) and `decoder`; apply it to the 23 `&Component{`
      literals in the six files named in the premises. `go test -race -count=1 ./processor/graph-ingest/` GREEN with the
      fail-closed seam in place.

## 6. e2e fixtures and the composition roots

- [ ] 6.1 `cmd/e2e-semstreams/fixtures/register.go`: `RegisterPayloads(reg)` for `test.fixture.v1`, `e2e.probe.v1`,
      `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1`, `research.e2e_search_seed.v1`
      (verbatim carriers, floor `control`); called from `buildPayloadRegistry` (`main.go:358-378`). Tier → keys: `core` and
      `lessons` → `test.fixture.v1`; `crud-tools` → `e2e.probe.v1`; `structural` → `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`,
      `e2e.relationship_contract.v1`; `research-graph` → `research.e2e_search_seed.v1`. 2.8 GREEN.
- [ ] 6.2 `test/e2e/scenarios/ops/scenario.go:462`: stamp the registered `agentic.loop_completed.v1` instead of
      `agentic.loop-completed.1` (the direct `PutKV` seed stays; O-9 files the write-path hygiene separately).
- [ ] 6.3 Docs owned by this change (#1104 already rewrote both to `RegisterPayloads`): add floor (`IndexingProfile`),
      `Contracts`, and `RegisterTestType` to the rewritten checklist in BOTH `.agents/skills/new-payload/SKILL.md` and
      `docs/concepts/15-payload-registry.md`, the checklist block byte-identical between them (`diff <(sed -n '/^## Step/,/^## Verification/p' …)`
      → empty); `.claude/skills/new-payload/SKILL.md` is a thin adapter and is untouched (technical writer; O-8).
- [ ] 6.4 `test/e2e/scenarios/agentic/scenario.go:838-848`: the missing-model-endpoint branch becomes a failure, not a
      warning, so `e2e:agentic` covers `model_endpoint` (N-1).

## 7. Gates — in the `AGENTS.md:63-68` Land order

- [ ] 7.1 Commit GREEN before any mutation check. Then, each with `cp <file> <file>.pre && sha256sum <file>` before and a
      restore + equal checksum after, one at a time: (a) delete the lesson row in `agentic.RegisterPayloads` →
      `TestPayloadRegistryIsTheSingleTypeAuthority` MUST fail; (b) delete `lifecycle.RegisterPayloads` from
      `payloadbuiltins.Register` → same test MUST fail; (c) delete the `GetRegistration` lookup at the create seam →
      `TestCreateRejectsUnregisteredMessageType` MUST fail; (d) delete `IndexingProfile:` on the lesson row →
      `TestFloorComesFromRegistration` MUST fail; (e) delete one predicate line in `AgentLessonEntity.Triples()` →
      `TestRegisteredContractMatchesTriples` and `TestEmitLessonBuildsEntityTriples` MUST fail; (f) delete the nil-registry guard →
      `TestFactoryRejectsNilPayloadRegistry` MUST fail; (g) delete the fixtures call in `buildPayloadRegistry` →
      `TestFixturesRegisterEveryE2EStamp` MUST fail; (h) delete the nil-registry guard at the seam →
      `TestCreateSeamRejectsWhenRegistryMissing` MUST fail; (i) remove the `payloadReg.Contracts()...` spread from
      `service.WireGraphRuntime` in `cmd/semstreams/main.go` → `go build ./...` passes and the binary MUST fail to boot with
      `projection: invalid contract: no contracts` (`task e2e:core` MUST fail) — the wiring, not the primitive; (j) delete the
      helper call on the in-process lane → `TestInProcessCreateRejectsUnregisteredType` MUST fail; (k) delete
      `inference.RegisterPayloads` from `payloadbuiltins.Register` → `TestHierarchyContainerBirthCarriesRegisteredType` MUST
      fail. Record each command and its output line here.
- [ ] 7.2 `task lint` (revive warnings = failure); `go test -race -count=1 ./...`; `go test -race -tags=integration -count=1 -p 2 ./...`;
      `task schema:generate && git diff --exit-code schemas/ specs/`; `go test ./test/contract/...`. Record outputs.
- [ ] 7.3 BREAKING tiers, one agent at a time on the host, results recorded verbatim: `task e2e:agentic` (loop execution,
      model endpoint after 6.4, containers), `task e2e:lessons` (minimum before the breaking commit lands on main), then
      `task e2e:structural` (containers, three `e2e.*` keys), `task e2e:ops`, `task e2e:research-graph`, `task e2e:lifecycle`,
      `task e2e:crud-tools`, `task e2e:core`.
- [ ] 7.4 Fill `conformance.md` Implementation and Test columns with `file:line` at the head that carries the last change to
      any `.go` file or spec delta on the branch; an empty cell at review time is a deviation to record.
- [ ] 7.5 Sister notices (communicate-only, no sister edits): semmachina (4 types, `internal/payload/constants.go:60-147`,
      `internal/projectioncontract/contracts.go:77-109`), semdev (`internal/intake/record.go:63`, `internal/standards/sync.go:143`),
      semconnect (no registry of its own — `cmd/cs-api-server` calls neither `payloadbuiltins.Register` nor `payloadregistry.New`;
      the obligation is "export a `RegisterPayloads` from `gateway/cs-api` for the 11 `c360.csapi-*.v1` types, floor `content`,
      and have the host composition root call it", `gateway/cs-api/projection_contracts.go:29-64`), plus the informational notes
      for semteams (`cmd/semteams/main.go:971,998`) and semmem — each as an issue in that repo referencing #1100 and ADR-103;
      links recorded in the PR body.
- [ ] 7.6 Implementation review by `semstreams-reviewer`, the owner-run cross-agent round where asked, fixes and re-review;
      `openspec archive single-type-authority` + spec sync as the final content commit; narrow reviewer check of the archive;
      undraft. The merge gate owns CI.
