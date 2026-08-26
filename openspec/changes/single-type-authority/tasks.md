# Tasks — single-type-authority

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing / deliberate /
"not done" / "still open" in any OPEN task line as a live caveat. They appear only in the RED-capture task 2.9 once it is
CLOSED. Everywhere else say "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (measured at `c3a17741`): `payloadregistry/registry.go:43-52,78-132,189-253` (Registration, Register, copies);
`pkg/projection/contract.go:12-14,36-43,46-98` and `errors.go:6`; `go list -deps ./payloadregistry` → `pkg/retry pkg/errs pkg/types`;
`go list -deps ./vocabulary` → `pkg/platform`; `go list -deps ./pkg/projection` includes `graph message natsclient internal/graphmutation`;
`go list -deps ./payloadbuiltins | grep -c processor/graph-ingest` → 0; `internal/builtinprojection/contracts.go:12-80` with consumers
`processor/agentic-tools/lesson_promotion.go:52,170-171`, `write_todos.go:196-197`, `cmd/semstreams/main.go:221`, `cmd/e2e-semstreams/main.go:154`
(`service.WireGraphRuntime`, `service/graph_runtime.go:16-21`); `processor/graph-ingest/indexing_profile_registry.go:30-76` (22 keys, all
registered: `agentic/payload_registry.go:20-36`, `processor/agentic-dispatch/payload_registry.go:42`, `agentic/research/register.go:16-58`);
`component.go:113-117,134-138,646,692,1264-1290,1704-1760,1864-1868,2036`; `canonical_mutations.go:199-240` (`:207` `IsValid`);
`mutation_runtime.go:198-204`; `graph/mutation_responses.go:10-52`; `graph/entity_predicate_contract.go:134-175`; `message/base_message.go:301-306`;
`message/decoder.go:39-44`; the six stamps `agentic/{agent_lesson:31-37,loop_execution:68-73,167-173,model_endpoint:25-31,ops_diagnosis:25-31,web_observation:25-27}_entity.go`
and `pkg/lifecycle/manager.go:24-28,400-407`; writers `processor/agentic-tools/emit_lesson.go:34,204-216,518,527,693-741`,
`emit_diagnosis.go:26,203-204,249-291`, `executors/web_emit.go:55-73`, `executors/httprequest.go:267`, `executors/websearch.go:264`,
`processor/agentic-loop/graph_writer.go:24,254,474,511-548`, `frameworkcapabilities/graphresearch/executor.go:387`,
`processor/research-graph-llmwrap/triplepub.go:94-100,167`; `_Distinct` tests `agentic/{model_endpoint:27,agent_lesson:111,ops_diagnosis:111,loop_execution:337}_entity_test.go`;
test stamps `test/e2e/scenarios/{graph_roundtrip.go:207,233, lessons/scenario.go:374,391, crud-tools/scenario.go:684, tiered_structural.go:444,502,1273,
research-graph/scenario.go:350-352, ops/scenario.go:459-470}`, `processor/graph-ingest/{indexing_profile_test.go:29,75-263, canonical_mutations_test.go:653}`,
13 test files with `CreateEntityRequest{`; `processor/graph-ingest/metrics_test.go:147-156` (`newTestDependencies`, no registry);
`payloadbuiltins/register.go:33-49`, `register_test.go:10-13`; `payloadregistry/testing.go:16-40`; `cmd/semstreams/main.go:80,214,761-772`,
`cmd/e2e-semstreams/main.go:147,358-378`; `vocabulary/predicates.go:325-332`; predicates `vocabulary/agentic/predicates.go:253,352-387,756-790,837-928,1034-1115`.

## 1. Claim

- [ ] 1.1 Worktree `../semstreams-wt/claude/gh1100-single-type-authority` from `origin/main`; draft PR open with
      `Closes #1100` and `implemented-by: <persona>` in the body; this change directory,
      `docs/adr/103-payload-registry-is-the-single-type-authority.md`, and the two `docs/proposals/gh1100-*` documents are
      its first commit. The PR body is a published layer: it carries the sister migration list (design §11), the new
      outcome code and its detail key, the metric's new meaning, and the owner rulings on O-1…O-13 as they land.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `payloadregistry/attributes_test.go`: `TestRegisterRejectsInvalidIndexingProfile` (`"prose"` → error naming the value);
      `TestRegisterFillsAndChecksContractMessageType` (empty → filled with the key; a different key → error naming both);
      `TestGetRegistrationCopiesAttributes` (profile present; mutating the returned contract slice does not change a later read);
      `TestContractsReturnsIndependentSortedCopies`; `TestIndexingProfileFor` (registered+floor, registered+empty, unregistered).
      Does not compile at baseline (new fields and methods).
- [ ] 2.2 `pkg/projection/contract/contract_test.go`: `TestContractValidateUsesVocabularyProfiles` — every value in
      `vocabulary.{Content,Control,Signal,Trace}` validates and `"prose"` does not; `pkg/projection/contract_test.go` unchanged
      and GREEN at baseline (documents the alias). Does not compile at baseline (new package).
- [ ] 2.3 `agentic/entity_payloads_test.go`: `TestAgentLessonEntity_RoundTrip`, `TestOpsDiagnosisEntity_RoundTrip`,
      `TestModelEndpointEntity_RoundTrip`, `TestWebObservationEntity_RoundTrip`, `TestLoopExecutionEntity_RoundTrip` — marshal
      a fully populated entity, decode through `message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads))`
      into a fresh value, assert concrete type, field equality, `EntityID()` equality, and predicate-set equality of `Triples()`.
      `TestRegisteredContractMatchesTriples` — table over the five: predicate set of `Triples()` for a fully populated entity
      equals the registered contract's birth ∪ group predicates. Does not compile at baseline.
- [ ] 2.4 `pkg/lifecycle/harness_entity_test.go`: `TestHarnessEntity_RoundTrip` (verbatim triples survive decode).
      `payloadbuiltins/single_type_authority_test.go`: `TestPayloadRegistryIsTheSingleTypeAuthority` — six keys registered with
      non-empty floors; five carry a contract whose `MessageType` equals the key; `Contracts()` names unique and equal to
      `{agentic.loop-execution, agentic.lesson-record, agentic.ops-diagnosis, agentic.model-endpoint, agentic.web-observation}`;
      every registration's profile empty or valid. Does not compile at baseline.
- [ ] 2.5 `processor/graph-ingest/registered_type_gate_integration_test.go` (`//go:build integration`; the package's real NATS
      test client): `TestCreateRejectsUnregisteredMessageType` — registry from `payloadbuiltins.Register`; send `entity.create`
      stamping `test.unknown.v1`; decode the reply into a fresh value; assert code `message_type_unregistered`,
      `detail.message_type == "test.unknown.v1"`, no `ENTITY_STATES` key, `mutation_rejections_total{reason="message_type_unregistered"}`
      == 1. `TestCreateAcceptsRegisteredMessageType` — stamp `agentic.agent_lesson.v1`; entity created; stamp persisted verbatim.
      `TestFloorComesFromRegistration` — `agentic.request.v1` → `trace` with no metric; `test.nofloor.v1` registered via
      `RegisterTestType` → `control` and metric +1. MUST fail at baseline (the gate does not exist; the floor is table-driven).
- [ ] 2.6 `processor/graph-ingest/factory_registry_test.go`: `TestFactoryRejectsNilPayloadRegistry` — construction with
      `PayloadRegistry: nil` returns an error naming the dependency. MUST fail at baseline (only `NATSClient` is checked).
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
  go test -race -count=1 -run '_RoundTrip|TestRegisteredContractMatchesTriples' ./agentic/ ./pkg/lifecycle/
  go test -race -count=1 -run 'TestPayloadRegistryIsTheSingleTypeAuthority' ./payloadbuiltins/
  go test -race -tags=integration -count=1 -p 2 -run 'TestCreateRejectsUnregisteredMessageType|TestCreateAcceptsRegisteredMessageType|TestFloorComesFromRegistration|TestResidentUnregisteredStampIsNotPoison' ./processor/graph-ingest/
  go test -race -count=1 -run 'TestFactoryRejectsNilPayloadRegistry' ./processor/graph-ingest/
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
      and record it here.
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
      `"ops-emit-diagnosis"`), `OpsDiagnosisContract()` (birth predicates per design §7). `emit_diagnosis.go:203-204` uses the entity.
      `agentic/model_endpoint_entity.go`: `ModelEndpointEntity` with plain fields, `Triples()` from `graph_writer.go:511-548`
      (source `"agentic-loop"`), `ModelEndpointContract()`; `graph_writer.go:245-258` constructs the entity; `buildModelEndpointTriples` deleted.
- [ ] 4.4 `agentic/web_observation_entity.go`: `WebObservationEntity` (one struct for both tools), `EntityID()` via
      `TryWebObservationEntityID` returning `""` on error, `Triples()` replacing the two builders in `executors/httprequest.go`
      and `websearch.go`, `WebObservationContract()` (birth `WebURL`; append group `observation`). `web_emit.go:55-73` unchanged
      in shape (create, then append on exists).
- [ ] 4.5 `agentic/payload_registry.go`: add the five registrations with `IndexingProfile` (loop_execution `control`, agent_lesson
      `content`, ops_diagnosis `content`, model_endpoint `control`, web_observation `content`) and `Contracts`; add `IndexingProfile:`
      to the existing 15 rows with the values from `indexing_profile_registry.go:32-60` verbatim; same for
      `processor/agentic-dispatch/payload_registry.go:42` (`signal`) and `agentic/research/register.go:16-58` (6 rows). Delete the
      `MUTATION-ONLY … NOT registered` paragraphs in the five entity files.
- [ ] 4.6 `pkg/lifecycle/harness_entity.go`: `HarnessEntity{ID string; Facts []message.Triple}` (Graphable, Payload, MarshalJSON),
      `RegisterPayloads(reg)` registering `lifecycle.harness.v1` with floor `control`; `lifecycleMessageType` (`manager.go:24-28`)
      becomes `HarnessMessageType()` beside it; `payloadbuiltins.Register` calls `lifecycle.RegisterPayloads`. 2.3, 2.4 GREEN.
- [ ] 4.7 Delete `internal/builtinprojection/` (both files); `lesson_promotion.go:52` returns `agentic.LessonContract()`;
      `lesson_promotion.go:170-171`, `write_todos.go:196-197` use the `agentic` constants; `cmd/semstreams/main.go:220-222` and
      `cmd/e2e-semstreams/main.go:153-155` pass `payloadReg.Contracts()...` to `service.WireGraphRuntime` (the registry is
      built at `:214` / `:147`, before the call). Delete the four `_Distinct` tests. `grep -rn builtinprojection --include='*.go' .` → 0.

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
      naming the key. 2.5 GREEN; 2.7 integration GREEN.
- [ ] 5.3 Unit fixtures: `metrics_test.go:147-156` `newTestDependencies` sets `PayloadRegistry` from `payloadbuiltins.Register`
      plus `RegisterTestType` for `test.widget.v1` and `test.fixture.v1`; sweep the other 12 files listed in the premises
      (`grep -rln 'CreateEntityRequest{' --include='*_test.go' .`) so every stamped key is registered in that test's registry.
      `go test -race -count=1 ./processor/graph-ingest/ ./graph/ ./internal/graphmutation/ ./pkg/lifecycle/ ./processor/graph-index/ ./processor/rule/`
      and the same with `-tags=integration -p 2` GREEN.

## 6. e2e fixtures and the composition roots

- [ ] 6.1 `cmd/e2e-semstreams/fixtures/register.go`: `RegisterPayloads(reg)` for `test.fixture.v1`, `e2e.probe.v1`,
      `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1`, `research.e2e_search_seed.v1`
      (verbatim carriers, floor `control`); called from `buildPayloadRegistry` (`main.go:358-378`). 2.8 GREEN.
- [ ] 6.2 `test/e2e/scenarios/ops/scenario.go:462`: stamp the registered `agentic.loop_completed.v1` instead of
      `agentic.loop-completed.1` (the direct `PutKV` seed stays; O-9 files the write-path hygiene separately).
- [ ] 6.3 Docs owned by this change: `docs/concepts/15-payload-registry.md` §"Registering a New Payload Type" (`:158`) and
      `.agents/skills/new-payload/SKILL.md` steps 3–5 and Debugging rewritten to the live idiom (`RegisterPayloads(reg)`,
      `payloadbuiltins`, `IndexingProfile`, `Contracts`, `RegisterTestType`); `CLAUDE.md:418-424` "Payload Registry" list updated
      (technical writer; O-8).

## 7. Gates — in the `AGENTS.md:63-68` Land order

- [ ] 7.1 Commit GREEN before any mutation check. Then, each with `cp <file> <file>.pre && sha256sum <file>` before and a
      restore + equal checksum after, one at a time: (a) delete the lesson row in `agentic.RegisterPayloads` →
      `TestPayloadRegistryIsTheSingleTypeAuthority` MUST fail; (b) delete `lifecycle.RegisterPayloads` from
      `payloadbuiltins.Register` → same test MUST fail; (c) delete the `GetRegistration` lookup at the create seam →
      `TestCreateRejectsUnregisteredMessageType` MUST fail; (d) delete `IndexingProfile:` on the lesson row →
      `TestFloorComesFromRegistration` MUST fail; (e) delete one predicate line in `AgentLessonEntity.Triples()` →
      `TestRegisteredContractMatchesTriples` and `TestEmitLessonBuildsEntityTriples` MUST fail; (f) delete the nil-registry guard →
      `TestFactoryRejectsNilPayloadRegistry` MUST fail; (g) delete the fixtures call in `buildPayloadRegistry` →
      `TestFixturesRegisterEveryE2EStamp` MUST fail. Record each command and its output line here.
- [ ] 7.2 `task lint` (revive warnings = failure); `go test -race -count=1 ./...`; `go test -race -tags=integration -count=1 -p 2 ./...`;
      `task schema:generate && git diff --exit-code schemas/ specs/`; `go test ./test/contract/...`. Record outputs.
- [ ] 7.3 BREAKING tiers, one agent at a time on the host, results recorded verbatim: `task e2e:agentic`, `task e2e:lessons`
      (minimum before the breaking commit lands on main), then `task e2e:ops`, `task e2e:research-graph`, `task e2e:lifecycle`,
      `task e2e:crud-tools`, `task e2e:core`, `task e2e:structural`.
- [ ] 7.4 Fill `conformance.md` Implementation and Test columns with `file:line` at the head that carries the last change to
      any `.go` file or spec delta on the branch; an empty cell at review time is a deviation to record.
- [ ] 7.5 Sister notices (communicate-only, no sister edits): semmachina (4 types, `internal/payload/constants.go:60-147`,
      `internal/projectioncontract/contracts.go:77-109`), semdev (`internal/intake/record.go:63`, `internal/standards/sync.go:143`),
      semconnect (`gateway/cs-api/projection_contracts.go:29-64`), plus the informational notes for semteams (`cmd/semteams/main.go:971,998`)
      and semmem — each as an issue in that repo referencing #1100 and ADR-103; links recorded in the PR body.
- [ ] 7.6 Implementation review by `semstreams-reviewer`, the owner-run cross-agent round where asked, fixes and re-review;
      `openspec archive single-type-authority` + spec sync as the final content commit; narrow reviewer check of the archive;
      undraft. The merge gate owns CI.
