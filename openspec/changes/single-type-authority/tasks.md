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
`canonical_mutations.go:243`; six `entityBucket` writers — `canonical_mutations.go:243` (RPC create), `:306` (RPC reconcile), `component.go:1985` (`MergeEntity`, birth-capable
via `len(current)==0` `:1993-2000`, sole caller `ingestEntity :1633` behind `c.decoder.Decode :1599` → registered by construction), `:2132` (in-process create), `:2311`, `:2495`
(`AddTriple`/batch, must-exist) — four birth-capable, one decode-gated, two gated by the helper (B-1/F1); `hierarchy.go:440-451` returns the create error without logging and
both callers WARN and continue (`component.go:1971`, `:2108`); `graph_writer.go:529-542` (five zero-gates); `emit_diagnosis.go:262` (`fmt.Sprintf("%g")` object); `configs/e2e-structural.json:480`,
`configs/agentic.json:182` (`enable_hierarchy: true`); `executors/httprequest.go:28,257-266` and `websearch.go:31,255-262` (two sources, two unconditional sets);
`emit_diagnosis.go:259-265` (`Confidence: args.Confidence` on every triple); `test/e2e/scenarios/agentic/scenario.go:786-800,838-848` (loop asserted; model endpoint a warning);
`openspec/changes/entity-id-segment-semantics/tasks.md` 5.1 `:210-218` (the builder files) and 5.3 `:223-229` (`internal/builtinprojection/contracts.go:26,56`, the lesson prefix `:85-93`);
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

- [x] 1.1 Worktree `../semstreams-wt/claude/gh1100-single-type-authority` from `origin/main`; draft PR #1109 open with
      `Closes #1100` and `implemented-by: <persona>` in the body; this change directory,
      `docs/adr/103-payload-registry-is-the-single-type-authority.md`, and the two `docs/proposals/gh1100-*` documents are
      its first commit (`9899d71d`). Rebase onto `7e7ea76e` or later before implementation begins. The PR body is a published
      layer: it links `docs/operations/migration-beta162-to-beta163.md` (the sister obligations), names the new outcome code
      and its detail key and the metric's new meaning, and carries the owner ruling of 2026-08-26 (O-1…O-18 as recommended;
      overrides O-6, O-11/O-12, O-17; the explicit acceptances).

## 2. Baseline capture — write the named tests first

      Claimed 2026-08-26 on branch `claude/gh1100-single-type-authority-impl` (worktree `../semstreams-wt/…`) off `main` after
      PR #1102 merged; the draft PR carries `Closes #1100`; this tick is its first commit; `implemented-by` in the PR body.
- [x] 2.1 `payloadregistry/attributes_test.go`: `TestRegisterRejectsInvalidIndexingProfile` (`"prose"` → error naming the value);
      `TestRegisterFillsAndChecksContractMessageType` (empty → filled with the key; a different key → error naming both);
      `TestGetRegistrationCopiesAttributes` (profile present; mutating the returned contract slice does not change a later read);
      `TestContractsReturnsIndependentSortedCopies`; `TestIndexingProfileFor` (registered+floor, registered+empty, unregistered);
      `TestRegisterRejectsSchemaMismatch` (a factory whose `Schema()` disagrees — GREEN at baseline, names the existing check at
      `registry.go:261-300` for the delta). Does not compile at baseline (new fields and methods).
- [x] 2.2 `pkg/projection/contract/contract_test.go`: `TestContractValidateUsesVocabularyProfiles` — every value in
      `vocabulary.{Content,Control,Signal,Trace}` validates and `"prose"` does not; `pkg/projection/contract_test.go` unchanged
      and GREEN at baseline, plus two documenting tests there: `TestContractLiteralCompilesAgainstAliases` (a literal using
      `projection.Contract`, `projection.PredicateGroup`, `projection.ModeReconcile` validates) and
      `TestOverlappingLocalContractsConstruct` (two clients with overlapping contracts both construct). Does not compile at
      baseline (new package). `pkg/projection/mutation_client_test.go`: `TestCreateFillsMessageTypeFromContract` — a
      `CreateMutation` whose entity has an empty `MessageType` produces a request carrying the bound contract's key; MUST fail at
      baseline (`validateEntity :322-323` rejects the empty stamp). `TestCreateRejectsConflictingMessageType` — a non-empty
      stamp that differs from the contract is rejected with a classified invalid error naming both keys; GREEN at baseline
      (the existing `:325-326` check), kept as the conflict-branch pin.
- [x] 2.3 `agentic/entity_payloads_test.go`: `TestAgentLessonEntity_RoundTrip`, `TestOpsDiagnosisEntity_RoundTrip`,
      `TestModelEndpointEntity_RoundTrip`, `TestWebObservationEntity_RoundTrip`, `TestLoopExecutionEntity_RoundTrip` — marshal
      a fully populated entity, decode through `message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads))`
      into a fresh value, assert concrete type, field equality, `EntityID()` equality, and predicate-set equality of `Triples()`.
      `TestRegisteredContractMatchesTriples` — table over every registered contract (loop execution and lesson; the three others
      only under O-4 = mint): birth(C) ⊆ predicates(`Triples()` of a fully populated entity) ⊆ birth(C) ∪ groups(C), and a
      predicate removed from the builder but not from the contract fails naming it. `TestWebObservationEntityMatchesToolBuilders`
      — for `Tool = http_request` and `Tool = web_search`, the triple set (predicate, object, source, confidence) equals the
      baseline builder's output captured as a golden literal from `httprequest.go:257-266` / `websearch.go:255-262`, zero-valued
      triples included. `TestModelEndpointEntityMatchesBuilder` — golden literal captured from `graph_writer.go:511-548` for a
      fully populated endpoint AND for one with every optional field zero (the five gates `:529-542`; `bool`/`int`/`float64`
      objects). `TestOpsDiagnosisEntityMatchesBuilder` — golden literal captured from `emit_diagnosis.go:249-291`: the full set,
      the `fmt.Sprintf("%g")` confidence object (`:262`), and the entity's `Confidence` on every triple. Does not compile at
      baseline.
- [x] 2.4 `pkg/lifecycle/harness_entity_test.go`: `TestHarnessEntity_RoundTrip` (verbatim triples survive decode).
      `payloadbuiltins/single_type_authority_test.go`: `TestPayloadRegistryIsTheSingleTypeAuthority` — six keys registered with
      non-empty floors; loop execution and lesson carry a contract whose `MessageType` equals the key (the three others only under
      O-4 = mint); `Contracts()` names unique and equal to `{agentic.loop-execution, agentic.lesson-record}` (plus the three
      under O-4 = mint); under O-16 (a) `graph.hierarchy_container.v1` registered with floor `control`; every registration's
      profile empty or valid. `processor/agentic-tools/lesson_promotion_test.go`: `TestLessonProjectionContractIsTheRegisteredContract`
      — `LessonProjectionContract()` equals the contract the builtin registry holds for `agentic.agent_lesson.v1`. Does not
      compile at baseline.
- [x] 2.5 `processor/graph-ingest/registered_type_gate_integration_test.go` (`//go:build integration`; the package's real NATS
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
- [x] 2.6 `processor/graph-ingest/factory_registry_test.go`: `TestFactoryRejectsNilPayloadRegistry` — construction with
      `PayloadRegistry: nil` returns an error naming the dependency. MUST fail at baseline (only `NATSClient` is checked).
      `TestCreateSeamRejectsWhenRegistryMissing` — a `&Component{}` literal with no registry receives an `entity.create`; the
      reply decodes into a fresh value with code `internal`, nothing is written, no panic. `TestInProcessCreateRejectsUnregisteredType`
      — `Component.CreateEntity` with a registry lacking the entity's type returns the classified `message_type_unregistered`
      error and writes nothing; `mutation_rejections_total` is unchanged. `TestFactoryRejectsHierarchyWithoutContainerType` —
      `EnableHierarchy: true` with `payloadregistry.NewForTest` → construction fails naming `graph.hierarchy_container.v1`; with
      `payloadbuiltins.Register` → constructs (under O-16 (b) this test is dropped). MUST fail at baseline (no guard exists on
      either path; no factory check).
      `processor/graph-ingest/resident_stamp_integration_test.go` (`//go:build integration`): `TestResidentUnregisteredStampIsNotPoison`
      — put an entity with `message_type` `legacy.gone.v1` directly into the test bucket; boot; assert no poison inventory
      entry, exact read returns the stamp, and a `triple.append` to it reports `applied`. GREEN at baseline (documents §10 of
      the design; a barrier against a later registry-consulting codec).
- [x] 2.7 `processor/agentic-tools/emit_lesson_entity_test.go`: `TestEmitLessonBuildsEntityTriples` — for the same args,
      the predicate/object multiset from `AgentLessonEntity.Triples()` equals the multiset the baseline builder produces
      (capture the baseline output as a golden literal in the test before 4.2 moves the builder). Does not compile at baseline.
      `processor/agentic-tools/executors/web_observation_integration_test.go` (`//go:build integration`):
      `TestWebObservationBirthIsRegistered` — `publishWebObservation` against a graph-ingest with the builtin set; assert the
      entity exists with `message_type` `agentic.web_observation.v1`. MUST fail at baseline once 5.2 lands without 4.4; GREEN
      before 5.2 (records the coverage the missing e2e tier would give — O-10).
- [x] 2.8 `cmd/e2e-semstreams/fixtures/register_test.go`: `TestFixturesRegisterEveryE2EStamp` — the six e2e keys register into
      a fresh registry with floor `control` and round-trip as verbatim carriers. Does not compile at baseline.
- [x] 2.9 RED capture on baseline code `29a8779e` (§2 tests only; run 2026-08-26 before any non-test edit), recorded
      verbatim (first compiler line per package, or the failing assertion). Every "does not compile" test failed at build;
      the two `pkg/projection` tests failed at their assertion; the one documenting integration test compiled and passed.
      Deviation from the 2.2 prediction: `TestCreateRejectsConflictingMessageType` is RED at baseline, not GREEN — the
      baseline `:325-326` message names the contract NAME, not the contract's key, and the delta requires "naming both
      keys"; 3.4 rewrites the message. `TestResidentUnregisteredStampIsNotPoison` (GREEN by design) could not run at
      baseline because its package's other new files do not compile; it is exercised at §5.

  ```
  $ go test -race -count=1 -run 'TestRegisterRejectsInvalidIndexingProfile|TestRegisterFillsAndChecksContractMessageType|TestGetRegistrationCopiesAttributes|TestContractsReturnsIndependentSortedCopies|TestIndexingProfileFor' ./payloadregistry/
  github.com/c360studio/semstreams/pkg/projection/contract: no non-test Go files in .../pkg/projection/contract
  FAIL	github.com/c360studio/semstreams/payloadregistry [build failed]
  $ go test -race -count=1 -run 'TestContractValidateUsesVocabularyProfiles' ./pkg/projection/contract/
  pkg/projection/contract/contract_test.go:13:10: undefined: Contract
  pkg/projection/contract/contract_test.go:32:42: undefined: ErrInvalidContract
  FAIL	github.com/c360studio/semstreams/pkg/projection/contract [build failed]
  $ go test -race -count=1 -run 'TestCreateFillsMessageTypeFromContract|TestCreateRejectsConflictingMessageType' ./pkg/projection/
  --- FAIL: TestCreateFillsMessageTypeFromContract (0.00s)
      mutation_client_test.go:350: Create with an empty stamp: projection mutation create failed (invalid, not-committed): entity message type is required
  --- FAIL: TestCreateRejectsConflictingMessageType (0.00s)
      mutation_client_test.go:408: error does not name test.fixture.v1: projection mutation create failed (invalid, not-committed): entity message type "test.other.v1" does not match contract "test"
  FAIL	github.com/c360studio/semstreams/pkg/projection	0.288s
  $ go test -race -count=1 -run '_RoundTrip|TestRegisteredContractMatchesTriples|TestWebObservationEntityMatchesToolBuilders|TestModelEndpointEntityMatchesBuilder|TestOpsDiagnosisEntityMatchesBuilder' ./agentic/ ./pkg/lifecycle/
  pkg/lifecycle/harness_entity_test.go:21:25: undefined: lifecycle.HarnessEntity
  pkg/lifecycle/harness_entity_test.go:30:28: undefined: lifecycle.HarnessMessageType
  pkg/lifecycle/harness_entity_test.go:35:80: undefined: lifecycle.RegisterPayloads
  agentic/entity_payloads_test.go:64:28: undefined: agentic.AgentLessonEntity
  agentic/entity_payloads_test.go:76:31: undefined: agentic.OpsDiagnosisEntity
  agentic/entity_payloads_test.go:85:35: undefined: agentic.ModelEndpointEntity
  agentic/entity_payloads_test.go:94:38: undefined: agentic.WebObservationTool
  agentic/entity_payloads_test.go:94:67: undefined: agentic.WebObservationEntity
  FAIL	github.com/c360studio/semstreams/agentic [build failed]
  FAIL	github.com/c360studio/semstreams/pkg/lifecycle [build failed]
  $ go test -race -count=1 -run 'TestPayloadRegistryIsTheSingleTypeAuthority' ./payloadbuiltins/
  payloadbuiltins/single_type_authority_test.go:46:43: registration.IndexingProfile undefined (type *payloadregistry.Registration has no field or method IndexingProfile)
  payloadbuiltins/single_type_authority_test.go:48:29: reg.IndexingProfileFor undefined (type *payloadregistry.Registry has no field or method IndexingProfileFor)
  payloadbuiltins/single_type_authority_test.go:61:34: registration.Contracts undefined (type *payloadregistry.Registration has no field or method Contracts)
  payloadbuiltins/single_type_authority_test.go:72:31: reg.Contracts undefined (type *payloadregistry.Registry has no field or method Contracts)
  FAIL	github.com/c360studio/semstreams/payloadbuiltins [build failed]
  $ go test -race -tags=integration -count=1 -p 2 -run 'TestCreateRejectsUnregisteredMessageType|TestCreateAcceptsRegisteredMessageType|TestFloorComesFromRegistration|TestResidentUnregisteredStampIsNotPoison|TestHierarchyContainerBirthCarriesRegisteredType' ./processor/graph-ingest/
  processor/graph-ingest/factory_registry_test.go:88:100: undefined: graph.ErrorCodeMessageTypeUnregistered
  processor/graph-ingest/registered_type_gate_integration_test.go:75:87: undefined: graph.ErrorCodeMessageTypeUnregistered
  processor/graph-ingest/registered_type_gate_integration_test.go:115:18: undefined: payloadregistry.RegisterTestType
  processor/graph-ingest/registered_type_gate_integration_test.go:159:28: undefined: inference.HierarchyContainerMessageType
  FAIL	github.com/c360studio/semstreams/processor/graph-ingest [build failed]
  $ go test -race -count=1 -run 'TestFactoryRejectsNilPayloadRegistry|TestCreateSeamRejectsWhenRegistryMissing|TestInProcessCreateRejectsUnregisteredType|TestFactoryRejectsHierarchyWithoutContainerType' ./processor/graph-ingest/
  processor/graph-ingest/factory_registry_test.go:88:100: undefined: graph.ErrorCodeMessageTypeUnregistered
  processor/graph-ingest/factory_registry_test.go:101:24: undefined: graph.ErrorCodeMessageTypeUnregistered
  FAIL	github.com/c360studio/semstreams/processor/graph-ingest [build failed]
  $ go test -race -count=1 -run 'TestLessonProjectionContractIsTheRegisteredContract' ./processor/agentic-tools/
  processor/agentic-tools/emit_lesson_entity_test.go:41:21: undefined: agentic.AgentLessonEntity
  processor/agentic-tools/lesson_promotion_test.go:410:20: registered.Contracts undefined (type *payloadregistry.Registration has no field or method Contracts)
  FAIL	github.com/c360studio/semstreams/processor/agentic-tools [build failed]
  $ go test -race -count=1 -run 'TestEmitLessonBuildsEntityTriples' ./processor/agentic-tools/
  processor/agentic-tools/emit_lesson_entity_test.go:41:21: undefined: agentic.AgentLessonEntity
  FAIL	github.com/c360studio/semstreams/processor/agentic-tools [build failed]
  $ go test -race -tags=integration -count=1 -run 'TestWebObservationBirthIsRegistered' ./processor/agentic-tools/executors/
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/executors	1.915s
  $ go test -race -count=1 -run 'TestFixturesRegisterEveryE2EStamp' ./cmd/e2e-semstreams/fixtures/
  github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures: no non-test Go files in .../cmd/e2e-semstreams/fixtures
  FAIL	github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures [build failed]
  ```

## 3. Registry — attributes registered with the type

- [x] 3.1 Create `pkg/projection/contract` (package `contract`): move `Contract`, `PredicateGroup`, `WriteMode`, `ModeReconcile`,
      `ModeAppend`, `ErrInvalidContract`, `Validate`, `ValidateContracts`, `validateGroupName` from `pkg/projection/contract.go`
      and `errors.go:6`; replace `validIndexingProfiles` with `vocabulary.IsValidIndexingProfile`; add `ValidateShape()` (everything
      `Validate` does except `vocabulary.RequireDeclaredPredicate`). In `pkg/projection` keep `type Contract = contract.Contract`,
      `type PredicateGroup = contract.PredicateGroup`, `type WriteMode = contract.WriteMode`, `const ModeReconcile = contract.ModeReconcile`,
      `ModeAppend`, `var ErrInvalidContract = contract.ErrInvalidContract`, `func ValidateContracts(...) = contract.ValidateContracts`.
      `go build ./... && go vet ./...` clean; `grep -rn 'validIndexingProfiles' --include='*.go' .` → 0.
      Done 2026-08-26: `go build ./...` → ok; `go vet ./payloadregistry/ ./pkg/projection/...` → ok; the grep → 0;
      `pkg/projection/contract_test.go` unchanged and GREEN (`ok  pkg/projection 1.376s`). `pkg/projection/errors.go`
      deleted (the sentinel moved with the types).
- [x] 3.2 `payloadregistry.Registration` gains `IndexingProfile string` and `Contracts []contract.Contract`; `Register` validates
      per the payload-registry delta (profile via `vocabulary.IsValidIndexingProfile`; contract key fill/check; unique contract
      names; `ValidateShape()`); `GetRegistration`/`List`/`ListByDomain` copy both (deep-copy contracts); add
      `IndexingProfileFor(key) (string, bool)` and `Contracts() []contract.Contract` (fresh copies, sorted by key then name).
      `payloadregistry` imports `pkg/projection/contract` and `vocabulary` only; re-measure `go list -deps ./payloadregistry`
      and record it here. Rewrite the package comment at `registry.go:1-16`: the genuinely new transitive dependency is
      `vocabulary` itself (five `init()`s — `hierarchy.go:17`, `labels.go:16`, `lifecycle.go:16`, `relationships.go:7`,
      `rulepacks/predicates.go:37` — and a global predicate registry); `pkg/platform` is already reached through `message`. Name
      the edge `payloadregistry → pkg/projection/contract → vocabulary` and that `message` inherits it.
      Done 2026-08-26: `go list -deps ./payloadregistry | grep semstreams` →
      `pkg/retry pkg/errs pkg/types pkg/platform vocabulary pkg/projection/contract payloadregistry` (the edge exactly as
      named; no `message`, no component package). Package comment rewritten (`registry.go:1-20`). O-13 agreement check
      (contract profile vs floor) implemented in `bindContracts` and pinned by the subtest of
      `TestRegisterRejectsInvalidIndexingProfile`.
- [x] 3.3 `payloadregistry/testing.go`: `RegisterTestType(tb testing.TB, reg *Registry, key string)` — parses the key, registers a
      schema-less stub factory with no floor; `tb.Fatalf` on error. 2.1 GREEN.
      Done 2026-08-26: `go test -race -count=1 -run 'TestRegisterRejectsInvalidIndexingProfile|TestRegisterFillsAndChecksContractMessageType|TestGetRegistrationCopiesAttributes|TestContractsReturnsIndependentSortedCopies|TestIndexingProfileFor|TestRegisterRejectsSchemaMismatch' ./payloadregistry/`
      → six `--- PASS`, `ok  payloadregistry 1.246s`.
- [x] 3.4 (O-17) `pkg/projection/mutation_client.go` `Create` (`:133`): before `validateEntity` (`:146`) and before the request
      is built (`:164`), fill an empty `entity.MessageType` from `binding.contract.MessageType` when the contract has one; keep
      the `:325-326` equality check as the conflict branch (classified invalid error naming both keys); an empty stamp with a
      contract that has no `MessageType` stays rejected (`:322-323`). Do the same in the exact-read validation at `:188` only
      for the conflict branch (a stored entity always carries a stamp). 2.2 fill test GREEN; conflict test stays GREEN.
      Done 2026-08-26: `go test -race -count=1 -run 'TestCreateFillsMessageTypeFromContract|TestCreateRejectsConflictingMessageType|TestContractLiteralCompilesAgainstAliases|TestOverlappingLocalContractsConstruct' ./pkg/projection/`
      → four `--- PASS`; `go test -race -count=1 -run 'TestContractValidateUsesVocabularyProfiles|TestValidateShapeSkipsPredicateDeclaration' ./pkg/projection/contract/`
      → two `--- PASS`. The conflict message now names both keys (see 2.9: it was RED at baseline on that assertion).
      The fill parses the contract key with an unexported three-part splitter in `pkg/projection` (no parser exists in
      `pkg/types`; adding one is a `pkg/*` export outside O-2).

## 4. The six framework types

- [x] 4.1 `agentic/loop_execution_entity.go`: JSON tags on `LoopExecutionEntity`, `Schema()`, `Validate()`, `MarshalJSON` (alias
      idiom); move `LoopExecutionContract()` and the constants `LoopExecutionContractName`, `TodoGroupName` from
      `internal/builtinprojection/contracts.go:12-46` beside the type.
      Done 2026-08-26: JSON tags, `Schema`, `Validate`, alias `MarshalJSON`/`UnmarshalJSON` on `LoopExecutionEntity`; `LoopExecutionContractName`, `TodoGroupName`, `LoopExecutionContract()` in `agentic/loop_execution_entity.go`. `TestLoopExecutionEntity_RoundTrip` PASS.
- [x] 4.2 `agentic/agent_lesson_entity.go`: `AgentLessonEntity` (design §7 fields; `CreatedAt time.Time`; `Status` born
      `proposed`), `EntityID()` via `AgentLessonEntityID`, `Triples()` = the builder from `emit_lesson.go:693-741` with source
      constant `LessonSource = "ops-emit-lesson"` beside the type; `LessonContract()` + `LessonRecordContractName`,
      `LessonLifecycleGroupName` moved from `contracts.go:52-80`. `emit_lesson.go:518,527` constructs the entity and passes
      `entity.Triples()`; `buildEmitLessonTriples` deleted. 2.7 `TestEmitLessonBuildsEntityTriples` GREEN.
      Done 2026-08-26: `AgentLessonEntity` + `Triples()` (builder moved from `emit_lesson.go`), unexported `lessonSource`, `LessonContract()` + names in `agentic/agent_lesson_entity.go`; `emit_lesson.go` constructs the entity and passes `lesson.Triples()`; `buildEmitLessonTriples` deleted. `TestEmitLessonBuildsEntityTriples` PASS, `TestAgentLessonEntity_RoundTrip` PASS.
- [x] 4.3 `agentic/ops_diagnosis_entity.go`: `OpsDiagnosisEntity`, `Triples()` from `emit_diagnosis.go:249-291` (source
      `"ops-emit-diagnosis"`; `Confidence` = the entity's field on every triple, `:259-265`); `OpsDiagnosisContract()` only if O-4
      = mint. `emit_diagnosis.go:203-204` uses the entity.
      `agentic/model_endpoint_entity.go`: `ModelEndpointEntity` with plain fields, `Triples()` from `graph_writer.go:511-548`
      (source `"agentic-loop"`), `ModelEndpointContract()`; `graph_writer.go:245-258` constructs the entity; `buildModelEndpointTriples` deleted.
      Done 2026-08-26: `OpsDiagnosisEntity` (`Confidence` on every triple, `%g` object) and `ModelEndpointEntity` (plain fields, five zero-gates, source `agentic-loop`) written; `emit_diagnosis.go` and `graph_writer.go` construct the entities (`modelEndpointEntity` is the one mapping helper); both builders deleted. O-4 = defer: no diagnosis / model-endpoint contract. `TestOpsDiagnosisEntityMatchesBuilder`, `TestModelEndpointEntityMatchesBuilder` PASS against the goldens captured from the former builders at `08660fc5`.
- [x] 4.4 `agentic/web_observation_entity.go`: `WebObservationEntity` with a `Tool` discriminator (`WebObservationTool`:
      `http_request` \| `web_search`) that selects the source constant (`agent-http-request` from `httprequest.go:28`,
      `agent-web-search` from `websearch.go:31`) and the unconditional emitted set (`:257-266` / `:255-262`, zero values
      included); `EntityID()` via `TryWebObservationEntityID` returning `""` on error; `Validate()` requires a known `Tool`.
      `httprequest.go:267` and `websearch.go:264` construct the entity and pass `entity.Triples()`; the two inline builders are
      deleted. `WebObservationContract()` only if O-4 = mint. `web_emit.go:55-73` unchanged in shape (create, then append on exists).
      Done 2026-08-26: `WebObservationEntity` with `WebObservationTool` discriminator selecting source and set; `httprequest.go`/`websearch.go` construct the entity and pass `observation.Triples()` (the backlink keeps the executors' existing source constants); `web_emit.go` unchanged. `TestWebObservationEntityMatchesToolBuilders` PASS (both tools, zero values included). No web-observation contract (O-4 = defer).
- [x] 4.5 `agentic/payload_registry.go`: add the five registrations with `IndexingProfile` (loop_execution `control`, agent_lesson
      `content`, ops_diagnosis `content`, model_endpoint `control`, web_observation `content`) and `Contracts` (loop execution
      and lesson; the three others only if O-4 = mint); add `IndexingProfile:`
      to the existing 15 rows with the values from `indexing_profile_registry.go:32-60` verbatim; same for
      `processor/agentic-dispatch/payload_registry.go:42` (`signal`) and `agentic/research/register.go:16-58` (6 rows — these
      floors exist only in a binary that selects graph research, `cmd/semstreams/main.go:766-770`; design §5, O-14). Delete the
      `MUTATION-ONLY … NOT registered` paragraphs in the five entity files.
      Done 2026-08-26: five registrations with floors and (loop execution, lesson) contracts added; `IndexingProfile:` on the 15 existing rows, on `agentic-dispatch` (`signal`) and on the 6 research rows, values verbatim from the retired table; the MUTATION-ONLY paragraphs deleted. Forced by #1052's contract (`agentic/rule_fields_test.go` enumerates the registry): the five entity types implement `message.RuleReadable` in `agentic/rule_fields.go` with rows in the projection table (prose withheld: summary/detail/injection-form, finding/recommendation, text/title/snippet/source-query, nested task). `go test -race ./agentic/` → ok.
- [x] 4.6 `pkg/lifecycle/harness_entity.go`: `HarnessEntity{ID string; Facts []message.Triple}` (Graphable, Payload, MarshalJSON),
      `RegisterPayloads(reg)` registering `lifecycle.harness.v1` with floor `control`; `lifecycleMessageType` (`manager.go:24-28`)
      becomes `HarnessMessageType()` beside it; `payloadbuiltins.Register` calls `lifecycle.RegisterPayloads`. 2.3, 2.4 GREEN.
      Done 2026-08-26: `pkg/lifecycle/harness_entity.go` (`HarnessEntity`, `HarnessMessageType`, `RegisterPayloads`, floor control); `manager.go` stamps `HarnessMessageType()`; wired into `payloadbuiltins.Register`. `TestHarnessEntity_RoundTrip` PASS.
- [x] 4.7 Delete `internal/builtinprojection/` (both files); re-point all seven consumer files: `lesson_promotion.go:52` returns
      `agentic.LessonContract()`; `lesson_promotion.go:170-171`, `write_todos.go:196-197` use the `agentic` constants;
      `cmd/semstreams/main.go:220-222` and `cmd/e2e-semstreams/main.go:153-155` pass `payloadReg.Contracts()...` to
      `service.WireGraphRuntime` (the registry is built at `:214` / `:147`, before the call); `test/e2e/scenarios/ops/scenario.go`,
      `processor/agentic-tools/graph_mutation_integration_helpers_test.go`, `lesson_promotion_test.go` use the `agentic` symbols.
      Delete the four `_Distinct` tests. `grep -rn builtinprojection --include='*.go' .` → 0.
      Done 2026-08-26: `internal/builtinprojection/` deleted; `lesson_promotion.go`, `write_todos.go`, both `main.go` (`payloadReg.Contracts()...`), `ops/scenario.go`, `graph_mutation_integration_helpers_test.go`, `lesson_promotion_test.go` re-pointed; four `_Distinct` tests deleted. `grep -rn builtinprojection --include='*.go' .` → 1 hit, the assertion message in `payloadbuiltins/single_type_authority_test.go` naming the retired set. `TestPayloadRegistryIsTheSingleTypeAuthority`, `TestLessonProjectionContractIsTheRegisteredContract` PASS.
- [x] 4.8 (O-16 (a); under (b) this task becomes the explicit exemption on the in-process lane and a spec-delta note)
      `graph/inference/container_entity.go`: `ContainerEntity{ID string; Facts []message.Triple}` (Graphable, Payload, MarshalJSON),
      `HierarchyContainerMessageType()` = `graph.hierarchy_container.v1`, `RegisterPayloads(reg)` with floor `control`;
      `payloadbuiltins.Register` calls it; `hierarchy.go:428` stamps `MessageType: HierarchyContainerMessageType()`. Factory check
      (F7, accepted with O-16 (a)): in graph-ingest's factory, `EnableHierarchy` with a registry that lacks `graph.hierarchy_container.v1` →
      `errs.WrapInvalid` naming the type, before any subscription — the observation-shaped guard for a composition root that did
      not call `payloadbuiltins.Register`. 2.6 factory test GREEN.

      Done 2026-08-26: `graph/inference/container_entity.go` (`ContainerEntity`, `HierarchyContainerMessageType`, `RegisterPayloads`, floor control); `hierarchy.go` stamps the container; wired into `payloadbuiltins.Register`; factory check in `CreateGraphIngest` (`enable_hierarchy` + registry lacking the type → `errs.WrapInvalid` naming `graph.hierarchy_container.v1`, before any subscription). `TestFactoryRejectsHierarchyWithoutContainerType` PASS.
## 5. graph-ingest — floor from the type, gate at the seam

- [x] 5.1 Retain `payloadRegistry *payloadregistry.Registry` on the component beside `decoder` (`component.go:487,692`); the
      factory (`:646`) returns `errs.WrapInvalid(..., "payload registry is required")` on nil. Delete
      `indexing_profile_registry.go`; `reconcileIndexingProfile` (`:1864`) calls `c.payloadRegistry.IndexingProfileFor(mt.Key())`;
      update the comment block at `:1836-1842` and the metric help at `:113-117` to the new meaning. Rewrite
      `indexing_profile_registry_test.go` against a registry from `payloadregistry.NewWithSubset(t, agentic.RegisterPayloads,
      research.RegisterPayloads, agenticdispatch.RegisterPayloads)` keeping all 22 expectations. 2.6 unit test GREEN.
      Done 2026-08-26: `payloadRegistry` field beside `decoder` (`component.go`), set from `deps.PayloadRegistry`; factory rejects nil (`PayloadRegistry required`); `indexing_profile_registry.go` deleted; `reconcileIndexingProfile` reads `c.registeredIndexingProfile` → `IndexingProfileFor(mt.Key())`; comment block and metric help rewritten to the new meaning. `indexing_profile_registry_test.go` rewritten against `NewWithSubset(agentic, research, agenticdispatch)` keeping all 22 expectations (`require.Len(cases, 22)`); `TestIndexingProfile_RegistryFloor_UnregisteredFiresMetric` became `..._RegisteredNoFloorFiresMetric` (an unregistered type is now refused before the floor runs). `TestFactoryRejectsNilPayloadRegistry` PASS.
- [x] 5.2 `graph/mutation_responses.go`: `ErrorCodeMessageTypeUnregistered = "message_type_unregistered"` with the closed-set
      comment. `canonical_mutations.go:207`: after `IsValid`, `GetRegistration(key)` miss → `rejectInvalidDetail(code,
      {"message_type": key}, err)`, metered through the existing rejection path as `reason="message_type_unregistered"`, WARN log
      naming the key. Implement the lookup as one helper `requireRegisteredMessageType(entity) error` and call it on BOTH create
      paths: `canonical_mutations.go:207` (wrapped as the coded rejection) and the top of `createEntityWithReceipt`
      (`component.go:2081`, before `ValidateEntityStateContract`; the same classified error returned to the caller, not
      metered — `hierarchy.go:440-451` returns it without logging and both graph-ingest callers already WARN and continue,
      `component.go:1971`, `:2108`). A nil `c.payloadRegistry` at either seam → `rejectInternal` (`mutation_runtime.go:206-208`) with
      an ERROR log, never a pass-through. 2.5 GREEN; 2.6 GREEN; 2.7 integration GREEN.
      Done 2026-08-26: `graph.ErrorCodeMessageTypeUnregistered` added to the closed set; `requireRegisteredMessageType` in `canonical_mutations.go` called at `handleCanonicalCreate` after `IsValid` and at the top of `createEntityWithReceipt` after the nil check; nil registry → `rejectInternal` + ERROR log; the RPC lane meters through `meteredMutation` (reason = the code) and WARNs with the key in the error text. `go test -race -tags=integration -count=1 -p 2 ./processor/graph-ingest/` → `ok 27.815s` (includes `TestCreateRejectsUnregisteredMessageType`, `TestCreateAcceptsRegisteredMessageType`, `TestFloorComesFromRegistration`, `TestHierarchyContainerBirthCarriesRegisteredType`, `TestResidentUnregisteredStampIsNotPoison`); unit `go test -race -count=1 ./processor/graph-ingest/` → ok (includes `TestCreateSeamRejectsWhenRegistryMissing`, `TestInProcessCreateRejectsUnregisteredType`).
- [x] 5.3 Unit fixtures: `metrics_test.go:147-156` `newTestDependencies` sets `PayloadRegistry` from `payloadbuiltins.Register`
      plus `RegisterTestType` for `test.widget.v1` and `test.fixture.v1`; sweep the other 12 files listed in the premises
      (`grep -rln 'CreateEntityRequest{' --include='*_test.go' .`) so every stamped key is registered in that test's registry.
      Sweep the 42 `_test.go` stamp sites (≥14 distinct keys, inventory §2.2) so every key that reaches the create seam is
      registered in that test's registry.
      `go test -race -count=1 ./processor/graph-ingest/ ./graph/ ./internal/graphmutation/ ./pkg/lifecycle/ ./processor/graph-index/ ./processor/rule/`
      and the same with `-tags=integration -p 2` GREEN.
      Done 2026-08-26: `component_fixture_test.go`: `newTestPayloadRegistry` (builtins + research + 19 test-only stub keys) and `mustTestPayloadRegistry`; `newTestDependencies` and every `component.Dependencies{NATSClient: …}` in the package (15 files) carry the registry; the 7 out-of-package graph-ingest constructions (`agentic-tools` ×2, `agentic-loop`, `graph-index`, `rule`, `gated-dag` ×2) carry builtins plus their own stub keys (`test.revision.v1`, `test.revision-claim.v1`, `test.unit.v1`). Census beyond the 42 stamped sites: 50 entity literals passed to the in-process `CreateEntity` carried NO stamp at all (15 graph-ingest files + 2 gated-dag files) and were stamped `test.entity.v1` / `test.unit.v1`. Suites: see 5.2 and the §7.2 record.
- [x] 5.4 `processor/graph-ingest/component_fixture_test.go`: `withTestRegistry(t, c *Component) *Component` sets
      `payloadRegistry` (builtins + `RegisterTestType` for the keys that file stamps) and `decoder`; apply it to the 23 `&Component{`
      literals in the six files named in the premises. `go test -race -count=1 ./processor/graph-ingest/` GREEN with the
      fail-closed seam in place.

      Done 2026-08-26: `withTestRegistry(tb, c)` applied to the 23 `&Component{` literals (`readiness_test.go` 14, `lifecycle_owner_test.go` 4, `keyed_ingest_test.go` 2, `batch_unit_test.go` 1, `component_test.go` 1; `query_contract_guard_test.go` injects `mustTestPayloadRegistry()` in its no-`t` helper). `go test -race -count=1 ./processor/graph-ingest/` → ok with the fail-closed seam in place.
      2026-08-27 (review MEDIUM-1): the fixture's hand-rolled splitter and `&struct{}{}` factory replaced by
      `payloadregistry.RegisterTestType`; the no-`t` variant passes a panic-shaped `testing.TB` so the one helper is the only
      stub-type spelling.
## 6. e2e fixtures and the composition roots

- [~] 6.1 `cmd/e2e-semstreams/fixtures/register.go`: `RegisterPayloads(reg)` for `test.fixture.v1`, `e2e.probe.v1`,
      `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1`, `research.e2e_search_seed.v1`
      (verbatim carriers, floor `control`); called from `buildPayloadRegistry` (`main.go:358-378`). Tier → keys: `core` and
      `lessons` → `test.fixture.v1`; `crud-tools` → `e2e.probe.v1`; `structural` → `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`,
      `e2e.relationship_contract.v1`; `research-graph` → `research.e2e_search_seed.v1`. 2.8 GREEN.
      Done 2026-08-26: `cmd/e2e-semstreams/fixtures/register.go` (`Carrier` verbatim payload, six keys, floor control) called from `buildPayloadRegistry` after `mission.RegisterPayloads`. `go test -race -count=1 -run TestFixturesRegisterEveryE2EStamp ./cmd/e2e-semstreams/fixtures/` → ok.
      PREMISE MEASURED FALSE 2026-08-26 (`[~]`, escalated — not executed): the tier→keys map assumes `cmd/e2e-semstreams` hosts
      graph-ingest for every tier. Measured from `docker/compose/*.yml`: `target: production` (= `cmd/semstreams`, `docker/Dockerfile:108`)
      for core and lessons (`e2e.yml:56`), agentic (`agentic.yml:68`), crud-tools (`crud-tools.yml:66`), research-graph
      (`research-graph.yml:63`), deep-research (`deep-research.yml:69`); `target: e2e` (= `cmd/e2e-semstreams`,
      `docker/Dockerfile:117-159`) only for structural/statistical/semantic (`tiered.yml`), lifecycle (`lifecycle.yml:56`), ops
      (`ops.yml:76`). The fixtures package cannot reach the production binary's registry. Affected set (corrected at review,
      2026-08-27): core (`test.fixture.v1`, `graph_roundtrip.go:207`, `CreateMutation` over the wire), lessons
      (`test.fixture.v1`, `lessons/scenario.go:391`), research-graph (`research.e2e_search_seed.v1`, `research-graph/scenario.go:343-352`,
      `graphmutation.NewClient(...).Create`). NOT affected: crud-tools — `e2e.probe.v1` (`crud-tools/scenario.go:684`) is written
      by direct `s.nats.PutKV` (`:692`), never through create, and readers never consult the registry; structural's three
      `e2e.*` keys work (e2e target). Options for the owner: (1) re-target the three affected tiers to the e2e image — agentic
      (`agentic.yml:68`), deep-research (`deep-research.yml:69`) and slow-consumer (`Dockerfile:180-191`, `FROM production AS
      e2e-slow-consumer`) still boot the production binary and stamp no synthetic type, so ≥3 production-binary tiers remain;
      (2) register the e2e fixture types in `cmd/semstreams` — test types in the production binary; (3) change what those
      three scenarios stamp to a type the production binary registers (none fits today except `core.json.v1`). None is this
      implementer's call.
- [x] 6.2 `test/e2e/scenarios/ops/scenario.go:462`: stamp the registered `agentic.loop_completed.v1` instead of
      `agentic.loop-completed.1` (the direct `PutKV` seed stays; O-9 files the write-path hygiene separately).
      Done 2026-08-26: `ops/scenario.go` seeds `agentic.loop_completed.v1` from the agentic constants; the direct `PutKV` stays.
- [x] 6.3 Docs owned by this change (#1104 already rewrote both to `RegisterPayloads`): add floor (`IndexingProfile`),
      `Contracts`, and `RegisterTestType` to the rewritten checklist in BOTH `.agents/skills/new-payload/SKILL.md` and
      `docs/concepts/15-payload-registry.md`, the checklist block byte-identical between them (`diff <(sed -n '/^## Step/,/^## Verification/p' …)`
      → empty); `.claude/skills/new-payload/SKILL.md` is a thin adapter and is untouched (technical writer; O-8).
      Done 2026-08-26: Step 3 of both checklists now carries `IndexingProfile`, `Contracts`, and `RegisterTestType`; the Verification Checklist in the skill gained the two ADR-103 items. The Step 3 block is byte-identical between the two files apart from heading level (`diff <(sed -n '/^## Step 3/,/^## Step 4/p' SKILL.md | sed '1d;$d') <(sed -n '/^### Step 3/,/^### Step 4/p' docs/concepts/15 | sed '1d;$d')` → empty). The invocation written in this task (`/^## Step/,/^## Verification/`) matches nothing in `docs/concepts/15-payload-registry.md`, which has always used `### Step 1–4` under `## Registering a New Payload Type` and has no Verification section — recorded, not restructured (technical writer).
- [x] 6.4 `test/e2e/scenarios/agentic/scenario.go:838-848`: the missing-model-endpoint branch becomes a failure, not a
      warning, so `e2e:agentic` covers `model_endpoint` (N-1).
      Done 2026-08-26: `agentic/scenario.go` returns an error when the model endpoint entity is absent.
- [~] 6.5 `docs/operations/migration-beta162-to-beta163.md` (part of this package, section "Single type authority (ADR-103)"):
      keep every sister section's `file:line` pinned to the named SHA; if a sister's obligation changes during implementation
      (e.g. a floor value), amend the section — never a sister repository. `proposal.md` and the PR body link it.

      2026-08-27: every sister section's `file:line` is pinned to the named SHA as written in the design package; one
      paragraph added (`:20`, the two boot-time consequences for a self-built graph-ingest root); no floor changed during
      implementation. `[~]` on the semconnect section (`:83-94`): its obligation is NOT executable as written — semconnect
      has no composition root (`semconnect/cmd/` = `cs-api-server` only; its host is the unmodified framework binary,
      `semconnect/deploy/compose.yml:19-49`, `conformance/compose.yml:55-73`), so "have the host composition root call it"
      names a root that does not exist. Marked OPEN in the document for the owner's ruling; sisters untouched.
## 7. Gates — in the `AGENTS.md:63-68` Land order

- [x] 7.1 Commit GREEN before any mutation check. Then, each with `cp <file> <file>.pre && sha256sum <file>` before and a
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
      fail; (l) delete the hierarchy factory check → `TestFactoryRejectsHierarchyWithoutContainerType` MUST fail; (m) delete the
      `if ep.MaxTokens > 0` gate in `ModelEndpointEntity.Triples()` → `TestModelEndpointEntityMatchesBuilder` MUST fail; (n) drop
      the fill in `MutationClient.Create` → `TestCreateFillsMessageTypeFromContract` MUST fail. Record each command and its output
      line here.
      Done 2026-08-26 at `17324b94` (GREEN: `go test -race -count=1 ./...` 154/154 ok; per-package integration suites ok).
      Harness: `cp` backup + sha256 before, mutation, `[applied]`, the package suite (unit) or the named integration test,
      restore, sha256 equal — every restore matched; tree clean after each. Verbatim `--- FAIL` lines:
      (a) delete the lesson row → `--- FAIL: TestPayloadRegistryIsTheSingleTypeAuthority (0.00s)` (only failure in `./payloadbuiltins/`)
      (b) `track(lifecycle.RegisterPayloads(reg))` → `_ =` → `--- FAIL: TestPayloadRegistryIsTheSingleTypeAuthority (0.00s)`
      (c) delete the `GetRegistration` lookup → `--- FAIL: TestCreateRejectsUnregisteredMessageType (0.34s)` (integration)
      (d) delete `IndexingProfile: content` on the lesson row → `--- FAIL: TestFloorComesFromRegistration (0.34s)` (integration; the
          lesson-floor subtest added in `17324b94` is what observes it — the task's two original subtests do not touch the lesson row)
      (e) delete the `LessonPolarity` line → `--- FAIL: TestRegisteredContractMatchesTriples (0.00s)` (`./agentic/`) and
          `--- FAIL: TestEmitLessonBuildsEntityTriples (0.00s)` + `--- FAIL: TestEmitLessonExecutor_CreatesLesson (0.00s)` (`./processor/agentic-tools/`)
      (f) delete the factory nil-registry guard → `--- FAIL: TestFactoryRejectsNilPayloadRegistry (0.00s)` (only failure in `./processor/graph-ingest/`)
      (g) `fixtures.RegisterPayloads(reg)` → `_ =` in `buildPayloadRegistry` → `--- FAIL: TestBuildPayloadRegistryRegistersEveryE2EStamp (0.00s)`
          (`cmd/e2e-semstreams/registry_wiring_test.go`, added in `17324b94`); `TestFixturesRegisterEveryE2EStamp` stays `ok` — the
          task's named unit test cannot observe `main.go` wiring, so the wiring test is the observer
      (h) delete the seam nil-registry guard → `--- FAIL: TestCreateSeamRejectsWhenRegistryMissing (0.00s)` (only failure)
      (i) remove `payloadReg.Contracts()...` in `cmd/semstreams/main.go` → `go build ./...` exit 0; `task e2e:core` → app container
          `exited (1)` before any scenario; the mutated binary against the e2e NATS: `Error: wire graph runtime: build graph mutation client: projection: invalid contract: no contracts`
      (j) delete the in-process helper call → `--- FAIL: TestInProcessCreateRejectsUnregisteredType (0.00s)` (only failure)
      (k) `track(inference.RegisterPayloads(reg))` → `_ =` → `--- FAIL: TestHierarchyContainerBirthCarriesRegisteredType (0.25s)` (integration)
      (l) delete the hierarchy factory check → `--- FAIL: TestFactoryRejectsHierarchyWithoutContainerType (0.00s)` (only failure)
      (m) make `ModelMaxTokens` unconditional → `--- FAIL: TestModelEndpointEntityMatchesBuilder (0.00s)` (only failure in `./agentic/`)
      (n) drop the O-17 fill → `--- FAIL: TestCreateFillsMessageTypeFromContract (0.00s)` (only failure in `./pkg/projection/`)
      Review HIGH-3 (2026-08-27), same harness: (H3-a) `HarnessMessageType()` category → `harness-unregistered` →
      `--- FAIL: TestHarnessBirthPassesRegisteredTypeGate (0.43s)` (new integration test, `pkg/lifecycle/harness_gate_integration_test.go`,
      real graph-ingest + `payloadbuiltins.NewTestRegistry`); the mutation ALSO fails `--- FAIL: TestHarnessEntity_RoundTrip`,
      `--- FAIL: TestRegisterCoreExcludesCapabilityAndProductPayloads`, `--- FAIL: TestPayloadRegistryIsTheSingleTypeAuthority`
      because `RegisterPayloads` registers literals while `Schema()` returns `HarnessMessageType()` and `validateSchemaConsistency`
      refuses the divergence — the review's "stays green" prediction did not hold for this mutation shape; the integration test
      remains the observer of the ingest gate. (H3-b) `manager.go` stamp → a different literal →
      `--- FAIL: TestManager_RoundTripCreateGetTransition (0.00s)` on the `fakeEmitter`-captured create request. Both restores
      sha256-equal.
- [x] 7.2 `task lint` (revive warnings = failure); `go test -race -count=1 ./...`; `go test -race -tags=integration -count=1 -p 2 ./...`;
      `task schema:generate && git diff --exit-code schemas/ specs/`; `go test ./test/contract/...`;
      `grep -rn 'NewNATSLessonCurator' --include='*.go' .` → 0 (the retired helper stays absent); `grep -rn builtinprojection
      --include='*.go' .` → 0. Record outputs.
      Done 2026-08-26: `task lint` → 0 warnings after `1ec9775b` (one revive indent-error-flow in the 6.4 edit, fixed);
      `go test -race -count=1 ./...` → 154 ok, 0 FAIL (after the census fixture and annotation re-pins in `17324b94`);
      per-package `go test -race -tags=integration -count=1 -p 2`: graph-ingest `ok 27.815s`, agentic-tools `ok 57.566s`,
      executors `ok 4.445s`, runner `ok 1.278s`, agentic-loop `ok 28.110s`, graph-index `ok 38.526s`, rule `ok 40.023s`,
      gated-dag `ok 7.968s`, lifecycle `ok 4.605s`; full `go test -race -tags=integration -count=1 -p 2 ./...` (21:13:06Z–21:22:16Z, background, log in the session
      scratchpad) → exit 0, 0 `FAIL` lines, 0 panics; `task schema:generate` → "OpenAPI generation complete", `git diff --exit-code schemas/ specs/openapi.v3.yaml`
      → empty; `go test ./test/contract/...` → ok; `grep NewNATSLessonCurator` → 0; `grep builtinprojection --include='*.go'` → 1
      (assertion message in `payloadbuiltins/single_type_authority_test.go` naming the retired set); `go vet -tags=integration ./...`
      → clean; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` → ok; `git diff --stat go.sum` → empty;
      `openspec validate single-type-authority --strict` → valid.
- [~] 7.3 Tag gate (owner override O-6): `task e2e:agentic`, `task e2e:lessons`, `task e2e:structural`, `task e2e:ops`, `task e2e:research-graph`, `task e2e:lifecycle`, `task e2e:crud-tools`, `task e2e:core` — all eight green, each as a provenance-complete row (exact command, runner identity, UTC start/end) in the candidate-proof record per `openspec/specs/release-candidate-proof/spec.md`; and, until the web-observation tier exists (O-10), `go test -race -tags=integration -count=1 -run TestWebObservationBirthIsRegistered ./processor/agentic-tools/executors/` recorded as a row of its own. One agent at a time on the host; the BREAKING commit lands on main
      only behind all of these; results recorded here verbatim.
      Run 2026-08-26 on this laptop (runner: Claude Fable 5 in the `claude/gh1100-single-type-authority-impl` worktree):
      `task e2e:agentic` 21:08:26Z–21:09:31Z → "Scenario completed successfully" (production binary; loop execution + model
      endpoint births, the 6.4 promotion held); `task e2e:structural` 21:10:22Z–21:10:42Z → "Scenario completed successfully"
      (e2e binary; `authority_hierarchy_provenance_entities:125`, containers born registered; the three `e2e.*` stamps admitted);
      `task e2e:core` 21:05:20Z–21:06:25Z → core-health PASS, core-dataflow PASS, core-graph-roundtrip FAIL:
      `entity message_type "test.fixture.v1" is not registered in this deployment's payload registry`; `task e2e:lessons`
      21:10:42Z–21:11:10Z → FAIL: `create evidence fixture: … "test.fixture.v1" is not registered in this deployment's payload registry`;
      `go test -race -tags=integration -count=1 -run TestWebObservationBirthIsRegistered ./processor/agentic-tools/executors/` → ok.
      `task e2e:lifecycle` 2026-08-27 11:07:24Z–11:07:48Z → "Scenario completed successfully" (e2e binary; harness births carry
      `lifecycle.harness.v1` through the real stack); `task e2e:ops` 11:08:00Z–11:08:22Z → "Scenario completed successfully"
      (e2e binary; diagnosis births, lesson birth + promotion, the O-9 seed key). Not run: `e2e:research-graph` (production
      target; expected to fail on the same premise as core and lessons), `e2e:crud-tools` (production target; its `e2e.probe.v1`
      is a direct `PutKV`, not a create — expected green, unverified). `[~]`: the union cannot be green until the owner rules the tier-to-binary
      premise recorded under 6.1; the candidate-proof rows are the TAG gate's, not this PR's.
- [x] 7.4 Fill `conformance.md` Implementation and Test columns with `file:line` at the head that carries the last change to
      any `.go` file or spec delta on the branch; an empty cell at review time is a deviation to record.
      Done 2026-08-26: every Implementation and Test column filled with `file:line` at the head carrying the last `.go`
      change; the DEVIATION row records the tier-to-binary premise (owner sign-off pending).
- [~] 7.5 Sister obligations are recorded in `docs/operations/migration-beta162-to-beta163.md` (owner override O-11/O-12:
      sister repositories stay read-only — no sister issues, comments, or edits): semmachina (4 types), semdev (2), semconnect
      (11, host-side registration), semteams (none; recommendation), semmem (for downstream-owner validation), semsource and
      semdragon (not affected). Verify at landing that every sister section's pinned SHA and `file:line` still read as written;
      the PR body links the document.
      2026-08-26/27: no sister file written, no sister `go` command, no sister issue or comment. semmachina, semdev, semteams,
      semmem, semsource, semdragon sections read as written at their pinned SHAs; `:20` added (boot-time consequences).
      NOT verified for semconnect: the section's instruction presumes a composition root semconnect does not have — see 6.5
      (`[~]`, OPEN in the document, owner ruling pending). The PR body link is the owner's (task 1.1).
- [ ] 7.6 Implementation review by `semstreams-reviewer`, the owner-run cross-agent round where asked, fixes and re-review;
      `openspec archive single-type-authority` + spec sync as the final content commit; narrow reviewer check of the archive;
      undraft. The merge gate owns CI.
