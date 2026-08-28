# Tasks — entity-id-segment-semantics

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
in any OPEN task line as a live caveat. They appear only in the RED-capture task 2.9 once it is CLOSED. Everywhere
else say "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (measured at `5cc0c7fb`, re-measured at `3226c220`): `pkg/types/entity_id.go:82-134` (struct, `Key`, `Parse`
by index), `:248-348` (prefix helpers, zero production callers), `:11-67` (coded contract); `agentic/entity_ids.go:29,82,138`,
`agent_lesson_entity.go:68,92`, `web_observation_entity.go:79`, `ops_diagnosis_entity.go:56` (builders);
`graph/events.go:19-20,171-175,290-301` (`NewAlertEvent` takes no authority; no sister caller) and
`processor/rule/graph_event_identity.go:12-37` (ADR-076 families; fixed lengths `docs/adr/076:27-28`);
`examples/processors/document/payload_document.go:50`, `payload_sensor.go:40`, `payload_maintenance.go:41`,
`payload_observation.go:41`, `examples/processors/weather_station/payload.go:100` (example builders; `document` wired
at `cmd/e2e-semstreams/main.go:25`); `examples/processors/iot_sensor/processor.go:278-288` (silent position reader,
caller `component.go:597`); `processor/rule/actions.go:1575-1583,1697-1712` (#1096 and the run anchor on the firing
entity); the rule processor holds no Platform (`processor/rule/*.go` non-test → one comment);
`processor/rule/entity_substitution.go:55-57,73-83`; `processor/rule/actions.go:881,1865` (config-authored subject
lane, `$entity.id` per `docs/concepts/18-rule-driven-artifacts.md:72,118`); `processor/agentic-tools/emit_lesson.go:55-57,862-885`;
`processor/agentic-loop/handlers.go:721` (lesson reader scans the local prefix only); `graph/inference/hierarchy.go:129-141,170-172,257-276,315-333,440`
(containers and inverse sibling edges minted from the ingested entity's prefix; `GetHierarchyTriples` on every fact
arrival at `processor/graph-ingest/component.go:1961-1977,2105-2116`); `processor/graph-ingest/component.go:1888`
(syntax-only gate), `:1464-1507` (fact lane), `:2614-2626` (suffix index), no `deps.Platform` read in graph-ingest;
`config/config.go:225-241` (bounds neither `org` nor `id`), `:772-778` (precedence); `types/component.go:134-137`
(`PlatformMeta` in the root package); `Taskfile.yml:96-99` (audit not in `.github/workflows/ci.yml`); `task
entity-id:audit` → 30 `entity_id_invalid:arity` findings; audit surfaces today: `go-assignment`, `go-call`,
`go-constructor`, `go-declaration`, `go-field`, `go-return`, `go-triple-reference`, `go-triple-subject` (no format-string
or dotted-constant surface); `vocabulary/namespace_authority.go:28-124` (donor) with one consumer `agentic/tools.go:369-382`;
`message/base_message.go:234-238` (wire meta = created_at, received_at, source); `deps.Platform` = 18 non-test lines
and `platform.{Org,Platform}` = 62 lines; `vocabulary/export/export.go:123-126` (export IRI in wire order);
`processor/graph-query/summary.go:198-202` → GraphQL `EntityTypeSummary.type` (`gateway/graph-gateway/component.go:1870`),
asserted only by `test/e2e/scenarios/tiered.go:350` (statistical, semantic); `graph/clustering/entityid_provider.go:231-236`
(live via `processor/graph-clustering/component.go:1331`) and `graph/clustering/summarizer.go:719-731`;
`test/e2e/scenarios/ops/scenario.go:604,712` (now `:607` and `:718-719` after the slice-A rewrite),
`tiered_structural.go:428-434`, `research-graph/scenario.go:201-203`,
`cmd/e2e-semstreams/mission/command.go:59-66,324-328` (e2e position literals and wire authority);
`configs/graph-backend.json` composes graph-ingest (`grep -c '"graph-ingest"'` → 2), as do 11 of the 14 shipped
configs; only `configs/gemini-example.json` and `configs/prompts.json` do not — re-measured 2026-08-27 after PR #1130
deleted `configs/cloud-federation.json` and `configs/edge-federation.json`, which this line originally cited as the
graph-ingest-free pair; `agentic/agentrun/agentrun.go:114-124` (run entity carries only `agent.run.phase`
and `agent.run.parent-entity-id`; no origin predicate), `:224-233` (`Mint(ctx, mgr, org, platform, rootLoopID)`),
`vocabulary/agentic/predicates.go:489,502` (`agent.loop.run`, `agent.run.entity-id`).

## 1. Claim

- [x] 1.1 Slice A claimed 2026-08-27: worktree `../semstreams-wt/claude/gh1095-entity-id-slice-a`, branch
      `claude/gh1095-entity-id-slice-a` from `origin/main` `78fe095c`; draft PR (number recorded on the PR itself and in
      7.6) whose body starts `Part of #1095 (slice A of two — slice B closes it)`, then `Closes #1097`, then
      `implemented-by: fable`. The owner split the design's one-PR landing (design §D row 2) into two PRs after the
      design package merged as `7e7ea76e` (#1099): **slice A** = §2 tests 2.1–2.4, 2.6 (substitution + lesson-scope
      rows only), 2.7, 2.8, 2.9 for those; §3 except 3.8; §4 omissions M1, M2, M5, M7–M12, M14, M15; §5; §7 gates for
      what slice A does. **Slice B** (a later PR carrying `Closes #1095`, `Closes #1096`) = §2.5, the two run-scope
      tests of 2.6, 3.8 (run-origin linkage is the #1096 mechanism), M3/M4/M6/M13, and all of §6. Slice A's PR body is
      the published layer for the per-sister migration list (design §D), the two values that leave the graph (export
      IRI path, `graphSummary` `entity_types[].type`), and the owner ruling of 2026-08-26 as applied (O-1–O-11, O-13,
      O-14 as recommended; O-12 = read-only mirror); slice B's PR body restates it for the boundary. #1097 (two
      defects in `docs/concepts/16-federation.md`) is folded into slice A's §5.5 doc sweep by owner direction.

## 2. Baseline capture — write the named tests first

- [x] 2.1 `pkg/types/entity_id_semantics_test.go`: `TestEntityIDKeyOrderIsSystemBeforeDomain` — build
      `EntityID{Org:"acme",Platform:"dep1",System:"src",Domain:"git",Type:"commit",Instance:"a1"}`; assert
      `Key() == "acme.dep1.src.git.commit.a1"`; assert `ParseEntityID` of that string round-trips every field.
      `TestPrefixLevelsAreNamed` — assert `DeploymentPrefix()=="acme.dep1"`, `SourcePrefix()=="acme.dep1.src"`,
      `TaxonomyPrefix()=="acme.dep1.src.git"`, `TypePrefix()=="acme.dep1.src.git.commit"`, and that
      `PrefixLevel(n)` for n∈{2,3,4,5} returns the same strings. `TestTaxonomyAcrossSourcesIsPatternNotPrefix` —
      `ValidateEntityIDPattern("acme.dep1.*.git.*.*")` is nil and `ValidateEntityIDPrefix("acme.dep1.*.git")` returns
      `entity_id_prefix_invalid`. Does not compile at baseline (new symbols).
      **Done (slice A, `a0e2a2cb`+):** `pkg/types/entity_id_semantics_test.go` — the three named tests plus `TestMaxAuthorityPairBytesDerivesFromLongestFamily` (pins that the 170-byte budget is derived from the family table, never hand-copied). Did not compile at baseline (see 2.9).

- [x] 2.2 `pkg/types/entity_domain_authority_test.go`: `TestEntityDomainAuthorityMirrorsPredicateAuthority` — an
      undelegated domain with an empty producer is rejected; an exact `domain.type` delegation admits only that type;
      the returned error carries code `entity_id_authority_invalid` and reason `domain_undelegated`.
      `TestEntityDomainAuthorityReservedPassesForEveryProducer` — every reserved domain passes for an empty and for
      an arbitrary producer. Does not compile at baseline.
      **Done:** `pkg/types/entity_domain_authority_test.go` — both named tests plus `TestEntityDomainAuthorityRejectsDuplicateAndReservedDelegations` (one domain by two producers, a reserved domain, an empty producer, a non-canonical segment all refuse to build). Did not compile at baseline.

- [x] 2.3 `pkg/types/entity_id_authority_test.go`: `TestAuthorityRejectionIsCodedAndIdentityFree` — table over
      (candidate, org, platform, importLane) → reason `foreign_authority` / nil; assert `errs.Code ==
      "entity_id_authority_invalid"`, detail keys are exactly `reason`, `segment_index`, `lane`, and no detail value
      contains a dot-joined identity. `TestAuthorityRejectionLocalClaimOnImportLane` — a candidate equal to the local
      pair on an import lane returns `local_authority_claimed`. Does not compile at baseline.
      **Done:** `pkg/types/entity_id_authority_test.go` — both named tests plus `TestAuthorityValidationRunsStructuralFirst` (arity masks nothing; an empty local pair is `foreign_authority`, never a wildcard). Did not compile at baseline.

- [x] 2.4 `agentic/entity_ids_semantics_test.go`: for each of the nine framework builders assert the produced ID's
      positions 3–4 are `<component>.<reserved-domain>` in the new order (e.g. loop execution →
      `agentic-loop.agent.execution`), and that `graph.NewAlertEvent(org, platform, …)` / `ruleTriggerEntityID(org,
      platform, …)` carry the supplied pair rather than `semstreams.framework`. MUST fail at baseline on every row
      (the two constructors do not compile with the new parameters).
      **Done:** `agentic/entity_ids_semantics_test.go` (nine builder rows, the `graph.NewAlertEvent(org, platform, …)` row, the gated-DAG pattern, `LoopIDFromExecutionEntityID` rejecting the retired order, and `TestAlertIdentityCarriesTheDeploymentAuthority`); the trigger row lives in `processor/rule/graph_event_identity_semantics_test.go` because `ruleTriggerEntityID` is unexported; the e2e mission row in `cmd/e2e-semstreams/mission/entity_id_semantics_test.go`. Did not compile at baseline (see 2.9). GREEN for the agentic rows only after 5.1 lands on the merged tree.

- [ ] 2.5 `processor/graph-ingest/authority_gate_integration_test.go` (`//go:build integration`; real NATS via the
      package's existing test client): `TestAuthorityGateRejectsForeignOnFactLane` — deployment `acme.dep1`; publish a
      Graphable whose ID is `acme.dep2.src.git.commit.a1` on a non-import port; assert no `ENTITY_STATES` key is
      created and `mutation_rejections{reason="authority_foreign"}` == 1. `TestAuthorityGateRejectsForeignOnMutationLane`
      — an `entity.create` request over `graph.mutation.>` for the same foreign ID; decode the reply into a fresh value
      and assert code `entity_id_authority_invalid`, reason `foreign_authority`, no `entity_id_invalid`.
      `TestAuthorityGateAllowsForeignReferenceObject` — after importing `acme.dep2.src.git.commit.a1` on an import
      port, create `acme.dep1.agentic-loop.agent.execution.<uuid>` with an `@id` triple to it; assert created and the
      reference persisted unchanged. `TestImportLaneAcceptsForeignRejectsLocalClaim` — the foreign entity on a port
      declared `"import": true` is created unchanged; an entity claiming `acme.dep1` on that port is rejected with
      `local_authority_claimed`. `TestHierarchySkipsForeignAuthority` — with `enable_hierarchy: true`, the imported
      entity is persisted with no `hierarchy.*` triple and no `…group` container exists.
      `TestAuthorityGateRejectsAnnotationOfImportedSubject` — after the import, a `triple.append` from a non-import
      lane targeting the imported subject is rejected `foreign_authority` and the import's revision is unchanged.
      MUST fail at baseline (no gate exists: the foreign write lands, the annotation lands, and the container is
      minted under `acme.dep2`).
      **Slice B — not done in this PR** (graph-ingest boundary tests belong with the gate they exercise).

- [ ] 2.6 `processor/rule/actions_run_scope_integration_test.go` (`//go:build integration`; a recording
      `tripleMutator` that captures every `AddTriple` subject): `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`
      — deployment `acme.dep1`; a rule with `run_scope=new` fires on `foreign.dep9.agentic-loop.agent.execution.<uuid>`
      (a peer deployment's own loop execution, because `LoopIDFromExecutionEntityID` (`actions.go:1554`,
      `entity_ids.go:167`) admits only that family and any other entity takes the warn-and-inherit fallback
      (`:1555-1570`)); assert the run entity `acme.dep1.chain.agent.execution.<uuid>` exists with
      `agvocab.RunOriginEntityID` = the imported ID, that ZERO captured `AddTriple` calls have the imported ID as
      subject, that the import's `ENTITY_STATES` revision is unchanged, and that
      `rule_run_anchor_skipped_total{reason="foreign_authority"}` == 1 while `mutation_rejections` is unchanged.
      `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin` — the same rule on `acme.dep1.agentic-loop.agent.execution.<uuid>`;
      assert the run carries `agvocab.RunOriginEntityID` = the local loop AND the loop received `agvocab.LoopRun` and
      `agvocab.LoopRunEntityID`. Both MUST fail at baseline (#1096: the mint reads `foreign.dep9`, the origin predicate
      does not exist, and the anchor write targets the import).
      `processor/rule/entity_substitution_test.go`: `TestSegmentTokensResolveByName` — `$entity.system` and
      `$entity.domain` resolve to positions 3 and 4 of the NEW order; `TestSegmentTokensUnresolvedOnInvalidID` — a
      five-position value leaves every token unresolved and the warning fires. MUST fail at baseline (first) /
      GREEN at baseline (second; forced omission 4.5 covers it).
      `processor/agentic-tools/emit_lesson_test.go`: `TestAppliesToThreeSegmentsIsSourceScope` — a lesson with
      `id:acme.dep1.src` matches a loop scoped to `acme.dep1.src.git.commit.a1` and not `acme.dep1.other.git.commit.a1`.
      GREEN at baseline (segment-boundary matching is order-agnostic, inventory L2): a documenting test, outside the
      2.9 baseline capture; it pins the meaning the spec delta assigns to three positions.
      **Partly done (slice A rows):** `TestSegmentTokensResolveByName` / `TestSegmentTokensUnresolvedOnInvalidID` in `processor/rule/entity_substitution_test.go` (the first did not pass at baseline, the second did — see 2.9); `TestAppliesToThreeSegmentsIsSourceScope` in `processor/agentic-loop/lessonmatch/lessonmatch_scope_test.go` — the matcher's home rather than `emit_lesson_test.go`, which PR #1109 owns during this window (GREEN at baseline, documenting). The two `actions_run_scope_integration_test.go` rows are slice B and are not done in this PR.

- [x] 2.7 `processor/graph-query/summary_test.go`: `TestGraphSummaryTypeKeyFollowsCanonicalOrder` — the
      `EntityTypeSummary.Type` for `acme.dep1.src.git.commit.a1` is `src.git.commit`, built from named fields.
      `graph/clustering/entityid_provider_test.go`: `TestEntityIDEdgesReadPositionsByName` — sibling prefix and
      source-peer affinity for the new order. `graph/clustering/summarizer_test.go`: `TestSummaryGroupsByNamedDomain`.
      `vocabulary/export/export_test.go`: `TestSubjectToIRIFollowsCanonicalOrder` — the IRI path is
      `…/entities/acme/dep1/src/git/commit/a1`. `examples/processors/iot_sensor/processor_test.go`:
      `TestParseZoneEntityIDReadsNamedPositions`. All MUST fail at baseline.
      **Done, with a measured premise correction:** `TestGraphSummaryTypeKeyFollowsCanonicalOrder` (`processor/graph-query/summary_test.go`) and `TestSubjectToIRIFollowsCanonicalOrder` (`vocabulary/export/export_test.go`) cannot go RED *by value* at baseline — both readers emitted the wire order through the old field mapping, so `src.git.commit` and `/src/git/` come out identical before and after the struct reorder. The summary test discriminates on the by-name parser skipping a seven-position and a first-byte value (RED at baseline: `Count = 4, want 2`); the export test on the named-field composition (RED at baseline through `ParseEntityID`'s old mapping); omissions M10/M11 prove both post-change. `TestEntityIDEdgesReadPositionsByName`, `TestSummaryGroupsByNamedDomain` (`graph/clustering/`), `TestParseZoneEntityIDReadsNamedPositions` (`examples/processors/iot_sensor/processor_test.go`) RED at baseline as predicted.

- [x] 2.8 `internal/entityidaudit/audit_test.go`: `TestAuditFlagsAuthorityLiteral` (a `Sprintf` builder with a
      product-name platform literal) and `TestAuditFlagsFormatPrefixAuthorityLiteral` (a trailing-dot constant and a
      `semstreams.framework.%s…` format) report `authority_literal`; `TestAuditFlagsUnregisteredDomain` reports
      `domain_unregistered`. `config/config_test.go`: `TestConfigRejectsOversizedAuthorityPair` — a 171-byte
      `org`+`id` pair does not load and the error names the trigger family and 170. All MUST fail at baseline.
      **Done:** `internal/entityidaudit/audit_test.go` — the three named tests plus `TestAuditFlagsReservedInstanceToken` and `TestAuditSegmentRulesSkipTestFilesAndSeeConfigPatterns` (rule `entity.pattern` and `entity_watch_buckets.ENTITY_STATES` are declaration patterns; `_test.go` is lexical-only); `config/config_test.go` — `TestConfigRejectsOversizedAuthorityPair` plus `TestConfigRejectsRemovedInstanceID` (O-2). All RED at baseline.

- [x] 2.9 RED capture on baseline code (§2 tests only), recorded here verbatim (package + test name + failing
      assertion or build error):

  ```
  go test -race -count=1 -run 'TestEntityIDKeyOrderIsSystemBeforeDomain|TestPrefixLevelsAreNamed|TestTaxonomyAcrossSourcesIsPatternNotPrefix|TestEntityDomainAuthority|TestAuthorityRejection' ./pkg/types/
  go test -race -count=1 -run 'Semantics' ./agentic/ ./graph/ ./processor/rule/
  go test -race -tags=integration -count=1 -run 'TestAuthorityGate|TestImportLane|TestHierarchySkipsForeignAuthority' ./processor/graph-ingest/
  go test -race -tags=integration -count=1 -run 'TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite|TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin' ./processor/rule/
  go test -race -count=1 -run 'TestSegmentTokens' ./processor/rule/
  go test -race -count=1 -run 'TestGraphSummaryTypeKeyFollowsCanonicalOrder' ./processor/graph-query/
  go test -race -count=1 -run 'TestEntityIDEdgesReadPositionsByName|TestSummaryGroupsByNamedDomain' ./graph/clustering/
  go test -race -count=1 -run 'TestSubjectToIRIFollowsCanonicalOrder' ./vocabulary/export/
  go test -race -count=1 -run 'TestParseZoneEntityIDReadsNamedPositions' ./examples/processors/iot_sensor/
  go test -race -count=1 -run 'TestAuditFlags' ./internal/entityidaudit/
  go test -race -count=1 -run 'TestConfigRejectsOversizedAuthorityPair' ./config/
  ```
      RED capture at `c73bb6ab` (baseline = `origin/main` `78fe095c` + the claim commit), filtered to build errors and `--- FAIL` lines, verbatim:

  ```
  $ go test -race -count=1 -run TestEntityIDKeyOrderIsSystemBeforeDomain|TestPrefixLevelsAreNamed|TestTaxonomyAcrossSourcesIsPatternNotPrefix|TestEntityDomainAuthority|TestAuthorityRejection|TestAuthorityValidationRunsStructuralFirst|TestMaxAuthorityPairBytes ./pkg/types/
  # github.com/c360studio/semstreams/pkg/types [github.com/c360studio/semstreams/pkg/types.test]
  pkg/types/entity_domain_authority_test.go:17:18: undefined: ErrorCodeEntityIDAuthorityInvalid
  pkg/types/entity_domain_authority_test.go:18:18: undefined: EntityIDReasonDomainUndelegated
  pkg/types/entity_domain_authority_test.go:28:20: undefined: NewEntityDomainAuthority
  pkg/types/entity_domain_authority_test.go:29:3: undefined: EntityDomainDelegation
  pkg/types/entity_domain_authority_test.go:30:3: undefined: EntityDomainDelegation
  pkg/types/entity_domain_authority_test.go:43:12: undefined: EntityDomainAuthority
  pkg/types/entity_domain_authority_test.go:53:61: undefined: FrameworkEntityDomains
  pkg/types/entity_domain_authority_test.go:54:16: undefined: NewEntityDomainAuthority
  pkg/types/entity_domain_authority_test.go:56:12: undefined: EntityDomainAuthority
  pkg/types/entity_domain_authority_test.go:57:25: undefined: FrameworkEntityDomains
  pkg/types/entity_domain_authority_test.go:57:25: too many errors
  FAIL	github.com/c360studio/semstreams/pkg/types [build failed]
  FAIL
  
  $ go test -race -count=1 -run FrameworkBuildersMint|FrameworkPrefixesAndPatterns|AlertIdentityCarries ./agentic/
  # github.com/c360studio/semstreams/agentic_test [github.com/c360studio/semstreams/agentic.test]
  agentic/entity_ids_semantics_test.go:60:76: too many arguments in call to graph.NewAlertEvent
  	want (string, string, map[string]any, graph.EventMetadata)
  agentic/entity_ids_semantics_test.go:75:28: undefined: semtypes.IsFrameworkEntityDomain
  agentic/entity_ids_semantics_test.go:114:73: too many arguments in call to graph.NewAlertEvent
  	want (string, string, map[string]any, graph.EventMetadata)
  agentic/entity_ids_semantics_test.go:116:74: too many arguments in call to graph.NewAlertEvent
  	want (string, string, map[string]any, graph.EventMetadata)
  agentic/entity_ids_semantics_test.go:121:45: undefined: semtypes.LongestFrameworkIdentityFamily
  agentic/entity_ids_semantics_test.go:123:64: too many arguments in call to graph.NewAlertEvent
  	want (string, string, map[string]any, graph.EventMetadata)
  agentic/entity_ids_semantics_test.go:125:69: too many arguments in call to graph.NewAlertEvent
  	want (string, string, map[string]any, graph.EventMetadata)
  FAIL	github.com/c360studio/semstreams/agentic [build failed]
  FAIL
  
  $ go test -race -count=1 -run TestRuleTriggerIdentityCarriesTheDeploymentAuthority|TestSegmentTokens ./processor/rule/
  # github.com/c360studio/semstreams/processor/rule [github.com/c360studio/semstreams/processor/rule.test]
  processor/rule/graph_event_identity_semantics_test.go:18:52: too many arguments in call to ruleTriggerEntityID
  	want (string, string)
  processor/rule/graph_event_identity_semantics_test.go:22:54: too many arguments in call to ruleTriggerEntityID
  	want (string, string)
  processor/rule/graph_event_identity_semantics_test.go:26:52: too many arguments in call to ruleTriggerEntityID
  	want (string, string)
  processor/rule/graph_event_identity_semantics_test.go:45:18: undefined: types.LongestFrameworkIdentityFamily
  processor/rule/graph_event_identity_semantics_test.go:50:53: too many arguments in call to ruleTriggerEntityID
  	want (string, string)
  FAIL	github.com/c360studio/semstreams/processor/rule [build failed]
  FAIL
  
  $ go test -race -count=1 -run TestAppliesToThreeSegmentsIsSourceScope ./processor/agentic-loop/lessonmatch/
  ok  	github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch	1.226s
  
  $ go test -race -count=1 -run TestGraphSummaryTypeKeyFollowsCanonicalOrder ./processor/graph-query/
  --- FAIL: TestGraphSummaryTypeKeyFollowsCanonicalOrder (0.00s)
      summary_test.go:398: Count = 4, want 2 (non-canonical values are skipped, never bucketed by index)
  FAIL
  FAIL	github.com/c360studio/semstreams/processor/graph-query	0.382s
  FAIL
  
  $ go test -race -count=1 -run TestEntityIDEdgesReadPositionsByName|TestSummaryGroupsByNamedDomain ./graph/clustering/
  --- FAIL: TestEntityIDEdgesReadPositionsByName (0.00s)
      entityid_provider_test.go:614: neighbors = [acme.dep1.src.git.commit.a2 acme.dep1.other.git.commit.b1], want the source peer v1 (named System = src)
      entityid_provider_test.go:620: getSystem = "git", want src (position 3 by name)
  --- FAIL: TestSummaryGroupsByNamedDomain (0.00s)
          	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1095-entity-id-slice-a/graph/clustering/summarizer_test.go:354
          	Error:      	Not equal: 
          	            	expected: "git"
          	            	actual  : "src"
          	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1095-entity-id-slice-a/graph/clustering/summarizer_test.go:361
          	Error:      	map[string]llm.DomainGroup{"feed":llm.DomainGroup{Domain:"feed", Count:1, SystemTypes:[]llm.SystemType{llm.SystemType{Name:"media.video", Count:1}}}, "src":llm.DomainGroup{Domain:"src", Count:3, SystemTypes:[]llm.SystemType{llm.SystemType{Name:"git.repo", Count:1}, llm.SystemType{Name:"git.commit", Count:2}}}} does not contain "git"
  FAIL
  FAIL	github.com/c360studio/semstreams/graph/clustering	0.327s
  FAIL
  
  $ go test -race -count=1 -run TestSubjectToIRIFollowsCanonicalOrder ./vocabulary/export/
  --- FAIL: TestSubjectToIRIFollowsCanonicalOrder (0.00s)
      export_test.go:306: subjectToIRI() = "https://semstreams.semanticstream.ing/entities/acme/dep1/src/git/commit/a1", want the named-field composition "https://semstreams.semanticstream.ing/entities/acme/dep1/git/src/commit/a1"
  FAIL
  FAIL	github.com/c360studio/semstreams/vocabulary/export	0.172s
  FAIL
  
  $ go test -race -count=1 -run TestParseZoneEntityIDReadsNamedPositions ./examples/processors/iot_sensor/
  --- FAIL: TestParseZoneEntityIDReadsNamedPositions (0.00s)
      processor_test.go:346: ZoneEntityID = "acme.logistics.facility.zone.area.cold-storage-1", want acme.logistics.zone.facility.area.cold-storage-1
  FAIL
  FAIL	github.com/c360studio/semstreams/examples/processors/iot_sensor	0.270s
  FAIL
  
  $ go test -race -count=1 -run TestAuditFlags|TestAuditSegmentRules ./internal/entityidaudit/
  --- FAIL: TestAuditFlagsAuthorityLiteral (0.00s)
      audit_test.go:660: findings = []entityidaudit.Finding{}, want one authority_literal on the go-format-prefix surface
  --- FAIL: TestAuditFlagsFormatPrefixAuthorityLiteral (0.00s)
      audit_test.go:685: findings = []entityidaudit.Finding{}, want two authority_literal findings
  --- FAIL: TestAuditSegmentRulesSkipTestFilesAndSeeConfigPatterns (0.00s)
      audit_test.go:768: findings = []entityidaudit.Finding{}, want the two config patterns with a literal org
  --- FAIL: TestAuditFlagsReservedInstanceToken (0.00s)
      audit_test.go:743: findings = []entityidaudit.Finding{}, want one instance_reserved finding for the group token
  --- FAIL: TestAuditFlagsUnregisteredDomain (0.00s)
      audit_test.go:716: findings = []entityidaudit.Finding{}, want media (pattern) and game (format builder) as the only unregistered domains
  FAIL
  FAIL	github.com/c360studio/semstreams/internal/entityidaudit	0.214s
  FAIL
  
  $ go test -race -count=1 -run TestConfigRejectsOversizedAuthorityPair|TestConfigRejectsRemovedInstanceID ./config/
  --- FAIL: TestConfigRejectsOversizedAuthorityPair (0.00s)
          	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1095-entity-id-slice-a/config/config_test.go:409
          	Error:      	An error is expected but got nil.
  --- FAIL: TestConfigRejectsRemovedInstanceID (0.00s)
          	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1095-entity-id-slice-a/config/config_test.go:424
          	Error:      	An error is expected but got nil.
  FAIL
  FAIL	github.com/c360studio/semstreams/config	0.286s
  FAIL
  
  $ go test -race -count=1 -run TestMissionIdentityFollowsCanonicalOrder ./cmd/e2e-semstreams/mission/
  # github.com/c360studio/semstreams/cmd/e2e-semstreams/mission [github.com/c360studio/semstreams/cmd/e2e-semstreams/mission.test]
  cmd/e2e-semstreams/mission/entity_id_semantics_test.go:26:29: undefined: semtypes.NewEntityDomainAuthority
  cmd/e2e-semstreams/mission/entity_id_semantics_test.go:26:54: undefined: EntityDomainDelegations
  cmd/e2e-semstreams/mission/entity_id_semantics_test.go:34:32: undefined: Producer
  FAIL	github.com/c360studio/semstreams/cmd/e2e-semstreams/mission [build failed]
  FAIL
  ```

      Two rows did not go RED: `TestAppliesToThreeSegmentsIsSourceScope` (`ok`, a documenting test as the task predicts) and, by value, the export/summary pair (2.7).

## 3. Contract — `pkg/types` and `config`

- [x] 3.1 Reorder `EntityID` fields to `Org, Platform, System, Domain, Type, Instance`; `Key()`/`ParseEntityID` follow;
      keep `EntityType()` = `{Domain, Type}`; update the struct comment to the position table in the spec delta.
      **Done:** `pkg/types/entity_id.go` — struct reordered with the position table in its doc comment, `Key()` and `ParseEntityID` by named position, `EntityType()` unchanged; `internal/entityidaudit` constructor join follows (`audit.go` `entityIDConstructorValue`).

- [x] 3.2 Replace `TypePrefix/SystemPrefix/DomainPrefix/PlatformPrefix` with the named levels `DeploymentPrefix` (2),
      `SourcePrefix` (3), `TaxonomyPrefix` (4), `TypePrefix` (5), plus `PrefixLevel(n)`; `IsSameSystem` → `IsSameSource`;
      `IsSameDomain` removed (not a prefix under the new order; `grep -rn IsSameDomain --include='*.go'` → tests only).
      **Done:** `DeploymentPrefix`/`SourcePrefix`/`TaxonomyPrefix`/`TypePrefix`, `PrefixLevel(n)` (coded prefix rejection outside 2–5), `PrefixLevelDeployment..PrefixLevelType` constants, `IsSameSource`; `SystemPrefix`/`DomainPrefix`/`PlatformPrefix`/`IsSameSystem`/`IsSameDomain` deleted (`message/parse_entity_id_test.go` rewritten to the named levels).

- [x] 3.3 Add `EntityDomainDelegation`, `EntityDomainAuthority`, `NewEntityDomainAuthority`, `Authorize(producer,
      domain, entityType)`, and the reserved set `FrameworkEntityDomains = {agent, ops, graph}` (ruled O-9: the gated-DAG
      family re-slots under `agent`); mirror `vocabulary/namespace_authority.go` shape-for-shape
      (`Producer` from the trusted boundary, exact matches only).
      **Done:** `pkg/types/entity_domain_authority.go` — `EntityDomainDelegation`, `EntityDomainAuthority`, `NewEntityDomainAuthority` (composition rejections: empty producer, non-canonical segment, reserved domain delegated; the two-producer rejection was implemented here and REMOVED 2026-08-28 when the owner superseded O-5 — overlap is permitted and reported by `composition.Validate` instead), `Authorize(producer, domain, entityType)` nil-safe with reserved passing for every producer, `FrameworkEntityDomains()`/`IsFrameworkEntityDomain` = `{agent, ops, graph}`. Consumers at birth: framework builders (reserved), the e2e mission harness and the three example packages declare delegations (`EntityDomainDelegations()`), the audit reads them as the registered set.

- [x] 3.4 Export `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`, reasons
      `EntityIDReasonForeignAuthority = "foreign_authority"`, `EntityIDReasonLocalAuthorityClaimed =
      "local_authority_claimed"`, `EntityIDReasonDomainUndelegated = "domain_undelegated"`, detail key
      `EntityIDDetailLane = "lane"`; add `ValidateEntityIDAuthority(candidate, org, platform string, importLane bool)
      error` (strings — `types.PlatformMeta` lives in the root `types` package and `pkg/types` must not import it).
      Details never carry identity bytes.
      **Done:** `pkg/types/entity_id_authority.go` — the code, three reasons, `EntityIDDetailLane` with `EntityIDLaneLocal`/`EntityIDLaneImport`, and `ValidateEntityIDAuthority(candidate, org, platform string, importLane bool)`; details are exactly `reason`, `segment_index`, `lane`. Unwired until slice B (graph-ingest).

- [x] 3.5 Reserve the container padding tokens: `ReservedInstanceTokens = {group, container, level}`;
      `ValidateEntityID` unchanged (lexical); the audit (5.6) and `graph/inference/hierarchy.go` consume the constant.
      **Done:** `ReservedInstanceTokens()`/`IsReservedInstanceToken` in `pkg/types/entity_domain_authority.go`; the audit reports `instance_reserved` (5.6). `graph/inference/hierarchy.go` consumes the constant on the merged tree (5.2 — PR #1109 owns that file during this window).

- [x] 3.6 `internal/semantictest.EntityID` positional args follow the new order; `message.ParseEntityID` delegator
      unchanged (alias).
      **Done:** `internal/semantictest/fixtures.go` positional parameters renamed `(org, platform, system, domain, type, instance)`; the join is unchanged so call sites keep their strings; call sites naming a family had their two middle arguments swapped (scratchpad `sweep2.py`, 5 files). `message.ParseEntityID` delegator unchanged.

- [x] 3.7 Authority-pair bound at config load (`config/config.go:225-241`): declare the framework identity family
      table in `pkg/types` (`FrameworkIdentityFamilies`, each family's fixed suffix; the rule trigger family = 86 bytes
      today) and `MaxAuthorityPairBytes = 256 − longest = 170` there — `config` imports neither `graph` nor
      `processor/rule` and the trigger prefix is unexported (`graph_event_identity.go:14`); `graph/events.go:20` and
      `ruleTriggerEntityID` build their prefixes from the `pkg/types` family entries so the number is never
      hand-copied; `Validate` rejects `len(org)+len(id) > MaxAuthorityPairBytes` naming the binding family. Amends
      ADR-076 d2.
      **Done:** `pkg/types/framework_identity_families.go` — `FrameworkIdentityFamily{Name, System, Domain, Type, InstanceBytes}` with `FixedBytes()` and `EntityID(org, platform, instance)` (fail-closed compose), the table (`rule-alert` 84, `rule-trigger` 86, `web-observation` 40 fixed bytes), `RuleAlertIdentityFamily()`/`RuleTriggerIdentityFamily()`/`WebObservationIdentityFamily()`, `LongestFrameworkIdentityFamily()`, `MaxAuthorityPairBytes()` = 256 − 86 = 170 computed, never a literal. `graph/events.go` and `processor/rule/graph_event_identity.go` compose from the table; `config/config.go` `validateAuthorityPair` rejects over-budget pairs naming `rule-trigger` and 170, then composes the binding family's identity under the pair by observation (which also refuses a dotted or leading-`-` org/id). Amends ADR-076 d2.

- [ ] 3.8 Run origin linkage: declare `agvocab.RunOriginEntityID = "agent.run.origin-entity-id"` (`@id`) beside
      `LoopRunEntityID` (`vocabulary/agentic/predicates.go:502`); add `AgentRun.OriginEntityID` with lifecycle tag
      `predicate=agent.run.origin-entity-id` (`agentic/agentrun/agentrun.go:114-124`) as a birth predicate of the run
      contract; `agentrun.Mint` gains `originEntityID` and sets it at creation (`:224-233`); update the run projection
      contract and schema (`task schema:generate`).
      **Slice B — not done in this PR** (the run-origin predicate is the #1096 mechanism and lands with 6.3).

## 4. Forced omissions — one per new parser/builder/mapper (commit GREEN first; restore by `cp` + `shasum`)

Each row: copy the file aside, delete the CALL (not the error check), run the named test, record the verbatim
`--- FAIL`, restore with `cp`, and record `shasum` equality of source and backup.

- [x] 4.1 M1 `ParseEntityID`: swap the two index assignments back to `Domain: parts[2], System: parts[3]` →
      `TestEntityIDKeyOrderIsSystemBeforeDomain` MUST fail.
      **Done** (`pkg/types/entity_id.go`; `cp` backup, restore verified by sha256):

      ```text
      ===== M1: pkg/types/entity_id.go =====
      BEFORE sha256 690ab162c909db961c0b9c4d7df60fce205c3b036ee69e92239fa641678e3711
      [applied] pkg/types/entity_id.go
      --- FAIL: TestEntityIDKeyOrderIsSystemBeforeDomain (0.00s)
          entity_id_semantics_test.go:28: 
          entity_id_semantics_test.go:29: 
          entity_id_semantics_test.go:32: 
          entity_id_semantics_test.go:33: 
      FAIL
      FAIL	github.com/c360studio/semstreams/pkg/types	0.290s
      FAIL
      AFTER  sha256 690ab162c909db961c0b9c4d7df60fce205c3b036ee69e92239fa641678e3711  restored=yes
      ```

- [x] 4.2 M2 `EntityDomainAuthority.Authorize`: return nil unconditionally → `TestEntityDomainAuthorityMirrorsPredicateAuthority` MUST fail.
      **Done** (`Authorize` short-circuits to nil for every domain):

      ```text
      ===== M2: pkg/types/entity_domain_authority.go =====
      BEFORE sha256 046239ae979aafbde12425d7aea0609641b7b658c65c6abe4f368a887c5f9381
      [applied] pkg/types/entity_domain_authority.go
      --- FAIL: TestEntityDomainAuthorityMirrorsPredicateAuthority (0.00s)
          entity_domain_authority_test.go:34: 
              	            				/Users/coby/Code/c360/semstreams-wt/claude/gh1095-entity-id-slice-a/pkg/types/entity_domain_authority_test.go:34
      FAIL
      FAIL	github.com/c360studio/semstreams/pkg/types	0.242s
      FAIL
      AFTER  sha256 046239ae979aafbde12425d7aea0609641b7b658c65c6abe4f368a887c5f9381  restored=yes
      ```

- [ ] 4.3 M3 graph-ingest gate: delete the `ValidateEntityIDAuthority` call on the fact lane →
      `TestAuthorityGateRejectsForeignOnFactLane` MUST fail.
      **Slice B — not done in this PR.**

- [ ] 4.4 M4 import lane: ignore the port's `import` flag → `TestImportLaneAcceptsForeignRejectsLocalClaim` MUST fail.
      **Slice B — not done in this PR.**

- [x] 4.5 M5 `entityPartNames`: swap the two names back → `TestSegmentTokensResolveByName` MUST fail; delete the
      `IsValidEntityID` guard in `applyEntityPartsSubstitutions` → `TestSegmentTokensUnresolvedOnInvalidID` MUST fail.
      **Done** — M5a swaps the two token names; M5b deletes the by-name parser guard:

      ```text
      ===== M5a: processor/rule/entity_substitution.go =====
      BEFORE sha256 27bfa301d8ec4943333d44f1f68175a42ecb7cd66f18d256b74ee932738f9707
      [applied] processor/rule/entity_substitution.go
      --- FAIL: TestSegmentTokensResolveByName (0.00s)
          entity_substitution_test.go:220: applyEntityPartsSubstitutions() = "src=git dom=src id=a1", want "src=src dom=git id=a1"
      FAIL
      FAIL	github.com/c360studio/semstreams/processor/rule	0.402s
      FAIL
      AFTER  sha256 27bfa301d8ec4943333d44f1f68175a42ecb7cd66f18d256b74ee932738f9707  restored=yes
      
      ===== M5b: processor/rule/entity_substitution.go =====
      BEFORE sha256 27bfa301d8ec4943333d44f1f68175a42ecb7cd66f18d256b74ee932738f9707
      [applied] processor/rule/entity_substitution.go
      --- FAIL: TestSegmentTokensUnresolvedOnInvalidID (0.00s)
          entity_substitution_test.go:237: five-position value resolved tokens: "src= dom= id="
      FAIL
      FAIL	github.com/c360studio/semstreams/processor/rule	0.414s
      FAIL
      AFTER  sha256 27bfa301d8ec4943333d44f1f68175a42ecb7cd66f18d256b74ee932738f9707  restored=yes
      ```

- [ ] 4.6 M6a `actions.go` run-scope mint: restore `idParts[0], idParts[1]` → both 2.6 tests MUST fail (run minted under
      `foreign.dep9`). M6b: delete the foreign-authority skip before `stampRun` → `…WithoutForeignWrite` MUST fail (a
      captured `AddTriple` targets the imported subject). M6c: delete the `OriginEntityID` assignment in `Mint` → both
      2.6 tests MUST fail (the local linkage is missing).
      **Slice B — not done in this PR.**

- [x] 4.7 M7 audit `authority_literal` rule: skip the `go-format-prefix` and `go-dotted-constant` surfaces →
      `TestAuditFlagsAuthorityLiteral` and `TestAuditFlagsFormatPrefixAuthorityLiteral` MUST fail.
      **Done** (`segmentFinding` narrowed to declaration patterns — both new surfaces skipped):

      ```text
      ===== M7: internal/entityidaudit/segment_rules.go =====
      BEFORE sha256 ea1483350d7eb8f7c6b34c65488935b1c7720cd91aae69acb99f070ac82297a2
      [applied] internal/entityidaudit/segment_rules.go
      --- FAIL: TestAuditFlagsAuthorityLiteral (0.00s)
          audit_test.go:660: findings = []entityidaudit.Finding{}, want one authority_literal on the go-format-prefix surface
      --- FAIL: TestAuditFlagsFormatPrefixAuthorityLiteral (0.00s)
          audit_test.go:685: findings = []entityidaudit.Finding{entityidaudit.Finding{Candidate:entityidaudit.Candidate{File:"/var/folders/db/qk1n2zs935q9ld3p2sn98yzr0000gn/T/TestAuditFlagsFormatPrefixAuthorityLiteral2565723234/001/prefix.go", Line:3, Column:27, Language:"prefix-constant", Value:"semstreams.framework.graph.rules.alert.", Surface:"go-dotted-constant:alertEntityPrefix", Status:"finding", Reason:"domain_unregistered", Classification:"", ClassificationReason:""}, Reason:"domain_unregistered"}}, want two authority_literal findings
      FAIL
      FAIL	github.com/c360studio/semstreams/internal/entityidaudit	0.304s
      FAIL
      AFTER  sha256 ea1483350d7eb8f7c6b34c65488935b1c7720cd91aae69acb99f070ac82297a2  restored=yes
      ```

- [x] 4.8 M8 `SourcePrefix`/`PrefixLevel(3)`: return four positions → `TestPrefixLevelsAreNamed` MUST fail.
      **Done** (`SourcePrefix` returns four positions):

      ```text
      ===== M8: pkg/types/entity_id.go =====
      BEFORE sha256 690ab162c909db961c0b9c4d7df60fce205c3b036ee69e92239fa641678e3711
      [applied] pkg/types/entity_id.go
      --- FAIL: TestPrefixLevelsAreNamed (0.00s)
          entity_id_semantics_test.go:43: 
          entity_id_semantics_test.go:44: 
          entity_id_semantics_test.go:45: 
          entity_id_semantics_test.go:55: 
          entity_id_semantics_test.go:55: 
          entity_id_semantics_test.go:55: 
          entity_id_semantics_test.go:64: 
      FAIL
      FAIL	github.com/c360studio/semstreams/pkg/types	0.242s
      FAIL
      AFTER  sha256 690ab162c909db961c0b9c4d7df60fce205c3b036ee69e92239fa641678e3711  restored=yes
      ```

- [x] 4.9 M9 audit `domain_unregistered` rule: return no finding → `TestAuditFlagsUnregisteredDomain` MUST fail.
      **Done** (`domain_unregistered` never returned):

      ```text
      ===== M9: internal/entityidaudit/segment_rules.go =====
      BEFORE sha256 ea1483350d7eb8f7c6b34c65488935b1c7720cd91aae69acb99f070ac82297a2
      [applied] internal/entityidaudit/segment_rules.go
      --- FAIL: TestAuditFlagsUnregisteredDomain (0.00s)
          audit_test.go:718: findings = []entityidaudit.Finding{}, want media (pattern) and game (format builder) as the only unregistered domains
      FAIL
      FAIL	github.com/c360studio/semstreams/internal/entityidaudit	0.315s
      FAIL
      AFTER  sha256 ea1483350d7eb8f7c6b34c65488935b1c7720cd91aae69acb99f070ac82297a2  restored=yes
      ```

- [x] 4.10 M10 `summary.go` typeKey builder: rebuild from `segs[2..4]` → `TestGraphSummaryTypeKeyFollowsCanonicalOrder` MUST fail.
      **Done on the third attempt.** The first two mutants did not compile (`strings.Split` reintroduced without its import; then `semtypes` left unused) — recorded as invalid mutants, not kills. The third drops the parser import and reads `segs[2..4]` raw, which compiles and is killed:

      ```text
      ===== M10: processor/graph-query/summary.go =====
      BEFORE sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9
      [applied] processor/graph-query/summary.go
      FAIL	github.com/c360studio/semstreams/processor/graph-query [build failed]
      FAIL
      AFTER  sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9  restored=yes
      
      ===== M10 (re-run, compiling mutant): processor/graph-query/summary.go =====
      BEFORE sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9
      [applied] processor/graph-query/summary.go
      FAIL	github.com/c360studio/semstreams/processor/graph-query [build failed]
      FAIL
      AFTER  sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9  restored=yes
      
      ===== M10 (re-run 2, compiling mutant: raw segs[2..4] reader, parser import dropped): processor/graph-query/summary.go =====
      BEFORE sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9
      [applied] processor/graph-query/summary.go
      --- FAIL: TestGraphSummaryTypeKeyFollowsCanonicalOrder (0.00s)
          summary_test.go:398: Count = 4, want 2 (non-canonical values are skipped, never bucketed by index)
      FAIL
      FAIL	github.com/c360studio/semstreams/processor/graph-query	0.421s
      FAIL
      AFTER  sha256 d43df4ba99f77e86aba32380c5d2639b3708c4a913418b5a280f47aee2c831d9  restored=yes
      ```

- [x] 4.11 M11 `subjectToIRI`: emit `{domain}/{system}` in the old order → `TestSubjectToIRIFollowsCanonicalOrder` MUST fail.
      **Done** (`{domain}/{system}` emitted in the retired order):

      ```text
      ===== M11: vocabulary/export/export.go =====
      BEFORE sha256 f42f2a1f14875096b4f5950a7a779ae435f3bc7aaf5662c676f3aa893d1be840
      [applied] vocabulary/export/export.go
      --- FAIL: TestSubjectToIRIFollowsCanonicalOrder (0.00s)
          export_test.go:298: subjectToIRI() = "https://semstreams.semanticstream.ing/entities/acme/dep1/git/src/commit/a1", want "https://semstreams.semanticstream.ing/entities/acme/dep1/src/git/commit/a1"
          export_test.go:306: subjectToIRI() = "https://semstreams.semanticstream.ing/entities/acme/dep1/git/src/commit/a1", want the named-field composition "https://semstreams.semanticstream.ing/entities/acme/dep1/src/git/commit/a1"
      FAIL
      FAIL	github.com/c360studio/semstreams/vocabulary/export	0.223s
      FAIL
      AFTER  sha256 f42f2a1f14875096b4f5950a7a779ae435f3bc7aaf5662c676f3aa893d1be840  restored=yes
      ```

- [x] 4.12 M12 `getSystem`/`parseEntityID` rewrites: restore `parts[3]`/index reads → `TestEntityIDEdgesReadPositionsByName`
      and `TestSummaryGroupsByNamedDomain` MUST fail.
      **Done.** M12a needed a second, compiling attempt (the first reintroduced `strings.Split` without its import); M12b swaps the two named fields in the summarizer mapper:

      ```text
      ===== M12a: graph/clustering/entityid_provider.go =====
      BEFORE sha256 f5a3683dd120e7021684df15b521f9e04ee4a6bd81653d187c52fdc72502f2db
      [applied] graph/clustering/entityid_provider.go
      FAIL	github.com/c360studio/semstreams/graph/clustering [build failed]
      FAIL
      AFTER  sha256 f5a3683dd120e7021684df15b521f9e04ee4a6bd81653d187c52fdc72502f2db  restored=yes
      
      ===== M12a (re-run, compiling mutant): graph/clustering/entityid_provider.go =====
      BEFORE sha256 f5a3683dd120e7021684df15b521f9e04ee4a6bd81653d187c52fdc72502f2db
      [applied] graph/clustering/entityid_provider.go
      --- FAIL: TestEntityIDEdgesReadPositionsByName (0.00s)
          entityid_provider_test.go:616: neighbors = [acme.dep1.src.git.commit.a2 acme.dep1.other.git.commit.b1], want the source peer v1 (named System = src)
          entityid_provider_test.go:619: neighbors = [acme.dep1.src.git.commit.a2 acme.dep1.other.git.commit.b1], b1 shares the taxonomy git but not the source and must not be a peer
          entityid_provider_test.go:622: getSystem = "git", want src (position 3 by name)
      FAIL
      FAIL	github.com/c360studio/semstreams/graph/clustering	0.372s
      FAIL
      AFTER  sha256 f5a3683dd120e7021684df15b521f9e04ee4a6bd81653d187c52fdc72502f2db  restored=yes
      
      ===== M12b: graph/clustering/summarizer.go =====
      BEFORE sha256 dbf56af410378dd642b93c6231bb96ac50e754680f855f1690f7e5719d8816a8
      [applied] graph/clustering/summarizer.go
      --- FAIL: TestSummaryGroupsByNamedDomain (0.00s)
          summarizer_test.go:354: 
          summarizer_test.go:361: 
      FAIL
      FAIL	github.com/c360studio/semstreams/graph/clustering	0.353s
      FAIL
      AFTER  sha256 dbf56af410378dd642b93c6231bb96ac50e754680f855f1690f7e5719d8816a8  restored=yes
      ```

- [ ] 4.13 M13 hierarchy foreign-skip: delete the authority check in `GetHierarchyTriples` → `TestHierarchySkipsForeignAuthority` MUST fail.
      **Slice B — not done in this PR.**

- [x] 4.14 M14 config-load bound: delete the `MaxAuthorityPairBytes` check → `TestConfigRejectsOversizedAuthorityPair` MUST fail.
      **Done.** M14 deletes the `validateAuthorityPair` call. M14b (bonus, O-2) needed a second attempt: the first deleted the `instance_id` probe at only ONE of the two raw-JSON loaders and the file-loading test still passed — an incomplete mutant, recorded; deleting the CALL at both sites kills it:

      ```text
      ===== M14: config/config.go =====
      BEFORE sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a
      [applied] config/config.go
      --- FAIL: TestConfigRejectsOversizedAuthorityPair (0.00s)
          config_test.go:409: 
      FAIL
      FAIL	github.com/c360studio/semstreams/config	0.304s
      FAIL
      AFTER  sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a  restored=yes
      
      ===== M14b: config/config.go =====
      BEFORE sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a
      [applied] config/config.go
      ok  	github.com/c360studio/semstreams/config	1.312s
      AFTER  sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a  restored=yes
      
      ===== M14b (re-run, compiling mutant): config/config.go =====
      BEFORE sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a
      [applied] config/config.go
      --- FAIL: TestConfigRejectsRemovedInstanceID (0.00s)
          config_test.go:424: 
      FAIL
      FAIL	github.com/c360studio/semstreams/config	0.303s
      FAIL
      AFTER  sha256 ec9b3918d265c2bae4a78da79588cb1af77386f06c93402bd4b2d2c059a1619a  restored=yes
      ```

- [x] 4.15 M15 `ParseZoneEntityID`: restore `parts[2] != "facility" || parts[3] != "zone"` → `TestParseZoneEntityIDReadsNamedPositions` MUST fail.
      **Done** (the check reverted to the retired positions):

      ```text
      ===== M15: examples/processors/iot_sensor/processor.go =====
      BEFORE sha256 2eee45ec4bec3f97b75734b5a5f79bb3852fbf5ad5fe2a1e812f86da511df21d
      [applied] examples/processors/iot_sensor/processor.go
      --- FAIL: TestParseZoneEntityIDReadsNamedPositions (0.00s)
          processor_test.go:343: ParseZoneEntityID(canonical) = ("", ""), want (area, cold-storage-1)
      FAIL
      FAIL	github.com/c360studio/semstreams/examples/processors/iot_sensor	0.310s
      FAIL
      AFTER  sha256 2eee45ec4bec3f97b75734b5a5f79bb3852fbf5ad5fe2a1e812f86da511df21d  restored=yes
      ```

## 5. Sweep — builders, patterns, configs, docs, examples, audit

**Rows in files PR #1116 (#1093) deletes: none.** Measured before the deletion, not assumed — while `flowstore/`,
`flowtemplate/`, `engine/`, and `service/flow_*.go` still existed,
`grep -rn 'org\.platform|\*\.\*\.[a-z]|semstreams\.framework'` over them returned one hit,
`service/flow_runtime_messages_test.go:128` `"*.*.data"`, a NATS subject-match fixture and not an entity-ID
declaration pattern. #1116 has since merged and all four paths are gone, so the conclusion now holds trivially:
no inventory row was skipped for the flow retirement, and none is owed to the spec delta.

**Rows formerly paused behind PR #1109 (#1100): RELEASED.** #1109 merged as `62f56d7e`; this branch merged
`origin/main` (also carrying #1116 `b0a3f0b9` and #1130 `ad153b20`) and finished 5.1–5.3. Two anchors had moved
and are re-anchored in place: `internal/builtinprojection/contracts.go:26,56` no longer exists (#1109 deleted the
package; both declarations are now on the payload registrations) and
`test/e2e/scenarios/ops/scenario.go:604,712` are now `:607` and `:718-719`.


- [x] 5.1 Framework builders (`agentic/entity_ids.go`, `agent_lesson_entity.go`, `web_observation_entity.go`,
      `ops_diagnosis_entity.go`, `graph/events.go`, `processor/rule/graph_event_identity.go`, gated-dag participant,
      e2e mission, `examples/processors/iot_sensor/payload.go`) emit `org.platform.<component>.<domain>.<type>.<instance>`
      and authorize their domain; ADR-076 families drop the `semstreams.framework` literal and `graph.NewAlertEvent`
      / `ruleTriggerEntityID` gain `org, platform string` parameters (exported-surface change on `graph/`; no sister
      caller). Example builders `examples/processors/document/payload_document.go:50`, `payload_sensor.go:40`,
      `payload_maintenance.go:41`, `payload_observation.go:41`, `examples/processors/weather_station/payload.go:100`
      swap positions 3–4.
      **Partly done — the five framework-builder files are PAUSED behind PR #1109** (`agentic/entity_ids.go`,
      `agent_lesson_entity.go`, `web_observation_entity.go`, `ops_diagnosis_entity.go`, `graph/inference/hierarchy.go`
      are all in that PR's file list; it rewrites them for ADR-103 and is open with green checks at the time of
      writing). Editing them before it merges would collide on every hunk, so this row finishes with
      `git merge origin/main` after #1109 lands.
      **Done in this PR:** `graph/events.go` (`NewAlertEvent(org, platform, …)` composing from
      `pkg/types.RuleAlertIdentityFamily()`), `processor/rule/graph_event_identity.go` (`ruleTriggerEntityID(org,
      platform, packID, ruleID)` from `RuleTriggerIdentityFamily()`, `semstreams.framework` retired),
      `processor/gated-dag/participant.go` (`*.*.gated-dag.agent.fanout.*`, ruled O-9),
      `cmd/e2e-semstreams/mission/{state,command}.go`, and the five example builders
      (`examples/processors/document/payload_{document,sensor,maintenance,observation}.go`,
      `weather_station/payload.go`, `iot_sensor/payload.go`) which also declare their domain delegations.
      **Barrier evidence, then and now:** before the #1109 merge `task entity-id:audit` reported exactly seven
      `domain_unregistered` findings, one per un-migrated builder. **After this row: zero** —
      `go run ./cmd/entity-id-audit .` → `entity ID audit passed: 1289 structured candidates across 1 roots` when
      this row was written; 1317 after review findings HIGH-1 and MEDIUM-2 widened extraction (`conformance.md`,
      "Implementation-review round"), still zero findings.
      The zero is load-bearing, proven by mutation: reverting `agentic/ops_diagnosis_entity.go`'s format to the
      retired order re-fires `agentic/ops_diagnosis_entity.go:225: format-builder
      go-format-prefix:tryOpsDiagnosisEntityID: "%s.%s.ops.diagnosis.finding.%s": domain_unregistered`; restore
      verified by md5 (`569dbba82fee6bbcb8da9f5371149b1e` before and after).
      **Completed after the merge:** the five builder files re-slotted (`agentic/entity_ids.go`,
      `agent_lesson_entity.go`, `web_observation_entity.go`, `ops_diagnosis_entity.go`, plus
      `loop_execution_entity.go`, which #1109 created and which carries the sixth family pattern).
      `TryWebObservationEntityID` now composes from `pkg/types.WebObservationIdentityFamily()` rather than
      re-spelling `web.agent.observation` in a `Sprintf`: the family table is the declared single home for those
      prefixes and had no consumer, and `webObservationInstanceLen` is now read from the same entry that binds
      the authority-pair budget.

- [x] 5.2 Index-position readers (inventory W3, W9, C3): `graph/inference/hierarchy.go`,
      `graph/clustering/entityid_provider.go:231-236` (`getSystem`, live through `NewEntityIDProvider` at
      `processor/graph-clustering/component.go:1331` — stays correct until gh606 deletes it),
      `graph/clustering/summarizer.go:719-731` (domain grouping of the summary prompt),
      `processor/graph-query/summary.go:198-202` (`EntityTypeSummary.Type` = `system.domain.type` by named field — an
      API value change named in the PR body), `graphrag.go`, `agentic/entity_ids.go:161-171`,
      `agentic/agentrun/agentrun.go:158-170`, `examples/processors/iot_sensor/processor.go:278-288` (the silent
      `facility.zone` reader) read by named field via `ParseEntityID`, never by raw index.
      **Done.** After the merge: `graph/inference/hierarchy.go` builds every container from the NAMED prefix
      levels (`TypePrefix`/`TaxonomyPrefix`/`SourcePrefix`) instead of `strings.Join(parts[:N])`, and
      `isContainerEntity` reads `pkg/types.IsReservedInstanceToken` instead of re-spelling the padding set — the
      audit rule and the runtime check now share one home. The H7 naming debt is recorded at the declaration:
      `CreateSystemEdges`/`CreateDomainEdges` and `hierarchy.{system,domain}.member` name the retired order and
      are ruled (O-6) to retire with gh606, not to be renamed here. `agentic.LoopIDFromExecutionEntityID` reads
      `ParseEntityID`'s System/Domain/Type. Earlier in this PR: `graph/clustering/entityid_provider.go`
      (`getSystem`, `getTypePrefix`),
      `graph/clustering/summarizer.go` (`parseEntityID`, prompt grouping), `processor/graph-query/summary.go`
      (`aggregateEntityTypes`), `processor/graph-query/graphrag.go` (`extractEntityType`, `extractEntityInstance`),
      `agentic/agentrun/agentrun.go` (`runIDFromChainEntityID`), `examples/processors/iot_sensor/processor.go`
      (`ParseZoneEntityID`), `graph/llm/prompt_types.go` field comments. PAUSED: `graph/inference/hierarchy.go` and
      `agentic/entity_ids.go:161-171` (`LoopIDFromExecutionEntityID`) — both in PR #1109.

- [x] 5.3 Declaration patterns, config literals, and e2e literals (inventory W5–W6, W8, W10): `agentic/agentrun/agentrun.go:100`,
      `internal/builtinprojection/contracts.go:26,56` (RE-ANCHORED: PR #1109 deleted that package and moved both
      declarations onto the payload registrations — now `agentic/loop_execution_entity.go:224` and
      `agentic/agent_lesson_entity.go:400`), `processor/gated-dag/participant.go:17`,
      `cmd/e2e-semstreams/mission/state.go:28`, `configs/*` three literal patterns; lesson record prefix
      (`agent_lesson_entity.go:85-93`); `entityPartNames` resolves by name; `test/e2e/scenarios/ops/scenario.go:604`
      and `:712`, `test/e2e/scenarios/tiered_structural.go:428-434` (rename the `domain`/`system` variables with the
      swap), `test/e2e/scenarios/research-graph/scenario.go:201-203`, `test/e2e/scenarios/tiered.go:350` value
      expectations, and `cmd/e2e-semstreams/mission/command.go:59-66,324-328` (mission minted from `deps.Platform`,
      never from the wire) rewritten in the same commit so no tier reports a position-literal mismatch that reads as
      a regression (`test/e2e/client/nats.go:965-974` is arity-only and stays).
      **Done.** After the merge: the two projection `EntityPattern` declarations re-slotted at their new home
      (`agentic/loop_execution_entity.go:224`, `agentic/agent_lesson_entity.go:400`), the lesson record prefix
      (`agent_lesson_entity.go:482-489`), and `test/e2e/scenarios/ops/scenario.go` `:607` and `:718-719` — both
      assertions now read positions by name through `ParseEntityID`. The three-part PREDICATE `ops.diagnosis.finding` that the same
      scenario queries is a separate vocabulary name and is deliberately unchanged.
      Earlier in this PR: `agentic/agentrun/agentrun.go:100` (`*.*.chain.agent.execution.*`),
      `processor/gated-dag/participant.go:17`, `cmd/e2e-semstreams/mission/state.go:28`, every `configs/*` literal
      pattern (`entity_watch_buckets.ENTITY_STATES` and the lesson rule pack now carry no literal authority),
      `test/e2e/scenarios/tiered_structural.go:428-434` (variables renamed `source`/`domain`),
      `research-graph/scenario.go`, `lifecycle/scenario.go`, `agentic/scenario.go:248`,
      `cmd/e2e-semstreams/mission/command.go` (minted from `deps.Platform`; the config knob and the wire authority are
      gone), `processor/rule/entity_substitution.go` (`entityPartNames` resolves by name).
      **Done after the #1109 merge:** the two projection `EntityPattern` declarations that were
      `internal/builtinprojection/contracts.go:26,56` — that package is deleted and they now live at
      `agentic/loop_execution_entity.go:224` (`*.*.agentic-loop.agent.execution.*`) and
      `agentic/agent_lesson_entity.go:400` (`*.*.lesson.agent.record.*`), with the lesson record prefix at
      `agent_lesson_entity.go:482-489`; and `test/e2e/scenarios/ops/scenario.go` (re-measured `:607` and `:718-719`,
      both now reading positions by name through `ParseEntityID`).

- [x] 5.4 `config/config.go`: `GetPlatform()` returns `Platform.ID`; `instance_id` present in a loaded config fails
      load with guidance naming `platform.id` (`removedConfigFields` precedent); `cmd/semstreams/main.go:477-484` and
      `cmd/e2e-semstreams/main.go:628-634` drop the precedence; every `configs/*.json` drops `instance_id`. Ruled (O-2).
      **Done:** `config.GetPlatform()` returns `Platform.ID`; `pkg/platform.Config.InstanceID` deleted; `rejectRemovedPlatformFields` fails load naming `platform.id` at both raw-JSON loaders; `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` `extractPlatformMeta` read `GetOrg()`/`GetPlatform()`; `instance_id` removed from `configs/**/*.json` — 18 files carried it at the merge base `78fe095c`; 16 were edited in place and the other 2 (`configs/cloud-federation.json`, `configs/edge-federation.json`) were deleted by PR #1130, so 16 edits land here. `git grep -l instance_id HEAD -- configs/` → 0.

- [x] 5.5 Docs: `docs/concepts/*`, `docs/basics/*`, `CLAUDE.md`, `AGENTS.md`, `openspec/project.md:91`,
      `openspec/specs/structural-identity/spec.md:6-13` name the new order (29 files, inventory §1.14 list);
      `docs/concepts/18-rule-driven-artifacts.md:72,118` (whole-ID subject examples) state that a `$entity.id` subject
      carries the canonical order and that position-literal subscriptions must follow it;
      `docs/proposals/gh606-derived-communities-design.md` is RESTATED at `:24-25`, `:29`, `:65-71`, `:76-79`, `:90`,
      `:126`, `:271`, `:334-335` per design §D (level 1 = source, served by default and summaries gate there — ruled
      O-11; symbol names after 3.2).
      **Done.** Order restated in `CLAUDE.md`, `AGENTS.md`, `openspec/project.md:91`,
      `openspec/specs/{structural-identity,entity-id-contract,graph-clustering,graph-index}/spec.md`,
      `docs/basics/{01,03}`, `docs/concepts/{02,14,16,18}`, `graph/README.md`, `pkg/types/README.md`,
      `processor/graph-ingest/README.md`, and the package docs (`doc.go`, `message/doc.go`, `message/types.go`,
      `message/triple.go`, `pkg/types/doc.go`, `graph/graphable.go`, `graph/types.go`, `pkg/lifecycle/participant.go`,
      `pkg/platform/platform.go`, `vocabulary/predicates.go`, …) — the explained examples were hand-edited so the
      prose matches the new meanings, not just the token order.
      **Correction (Codex round, 2026-08-28): this list named `natsclient/kv.go`, and the claim was false.**
      `git log origin/main..HEAD -- natsclient/kv.go` returned no commits: the file was never edited by this PR, and
      `natsclient/kv.go:521` still documented the retired order. Re-measured: of the seventeen files named here,
      sixteen were genuinely touched and `natsclient/kv.go` was the single false entry; it is edited now, together
      with `processor/graph-ingest/component.go:2685` and `config/README.md:50`. Current-surface census after those
      edits — retired order outside history: one hit, `agentic/entity_ids_semantics_test.go:102`, which names it AS
      retired; `platform.instance_id` as a live field: zero (the remaining `fan_out_instance_id` is the unrelated
      gated-DAG config key, and `process_instance_id` is ADR-056's). The "132 files" figure is withdrawn as
      unreproducible rather than restated.
      `docs/concepts/18-rule-driven-artifacts.md` states that a `$entity.id` subject carries the canonical order and
      that position-literal subscriptions must follow it. `docs/proposals/gh606-derived-communities-design.md` is
      RESTATED at P4, P6, the level table, §3.1, §3.2, the record example, the GraphQL note, and Q8 (level 1 = source,
      served by default, summaries gate there — ruled O-11).
      **#1097 (both defects) fixed** in `docs/concepts/16-federation.md`: the "ID format handles isolation by design"
      claim is replaced by what the boundary actually enforces today (lexical only; the authority gate is slice B) with
      the positions and their owners named, and the nonexistent `federation.RegisterPayload(domain)` instruction is
      replaced by the real payload-registry obligation, naming semsource's `graph/event_payload.go` as the shipped
      example. Deferred with #1109: `docs/concepts/15-payload-registry.md` and `docs/concepts/32-agent-memory.md`
      carry lesson/loop family literals in files that PR touches.

- [x] 5.6 `internal/entityidaudit`: add the surfaces `go-format-prefix` (a `fmt.Sprintf` format whose dot-separated
      tokens are read as positions, `%s` = template position) and `go-dotted-constant` (a string constant of ≥2 dotted
      tokens ending in `.`); add rules `authority_literal` (literal non-`*`, non-template value in positions 1–2 of a
      production builder, declaration, or prefix constant) and `domain_unregistered` (literal position-4 value outside
      the reserved set in production Go); classify the 30 existing arity findings at their exact occurrence; fixture
      tests per rule (2.8); `task entity-id:audit` added to the CI `Lint` job in `.github/workflows/ci.yml`.
      **Done.** `internal/entityidaudit/segment_rules.go` adds the surfaces `go-format-prefix` (a `fmt.Sprintf`
      format whose dotted tokens are positions, `%s` = template position) and `go-dotted-constant` (a trailing-dot
      dotted constant under an entity-named declaration), and the rules `authority_literal`, `domain_unregistered`
      (against `pkg/types.FrameworkEntityDomains()` plus `EntityDomainDelegation` literals collected from production
      Go), and `instance_reserved` (`pkg/types.ReservedInstanceTokens`). `audit.go` also extracts rule
      `entity.pattern` and `entity_watch_buckets.ENTITY_STATES` values from configs as declaration patterns (the
      bucket name comes from `graph.BucketEntityStates`, not a second literal). Fixture tests per rule: 2.8.
      `.github/workflows/ci.yml` Lint job gained the step "Entity-ID corpus audit (lexical + segment rules)" running
      the same command as `task entity-id:audit`.
      **The 30 baseline arity findings, each dispositioned at its exact occurrence** — no blanket allowlist:
      (a) 22 CONVERTED to canonical identities because the test never asserted malformedness, only used an opaque
      placeholder — `graph/embedding/{review_findings,storage_scan_failed}_test.go` (8),
      `agentic/agentrun/agentrun_integration_test.go` (6) and `test/compat/semteams/agentrun_terminal_compat_test.go`
      (3) via `agentic.ChainExecutionEntityID(...)` so the "absent run" sentinel is a well-formed ID that is absent,
      `processor/graph-query/operation_inventory_test.go` (4), `processor/graph-index/lifecycle_order_test.go` (1),
      `processor/rule/matches_test.go` (1), `service/message_logger_http_kv_query_test.go` (2),
      `pkg/fusion/engine_graph_test.go` (1);
      (b) 3 CLASSIFIED as deliberate negatives at their exact line/column/surface —
      `pkg/fusion/fusionnats/slice_e_contract_test.go:42` (poisoned wire entity),
      `processor/graph-clustering/outgoing_reader_contract_test.go:31` (malformed relationship target),
      `processor/graph-ingest/entity_state_preio_contract_test.go:42` (malformed root rejected before I/O);
      (c) 1 CANONICALIZED in production e2e — `test/e2e/scenarios/agentic/scenario.go:248`, a five-part
      `query_entity` argument on the live agentic tier (the stage asserts durable redelivery, not the entity).
      Plus one new deliberate negative from this change classified in place
      (`pkg/types/entity_id_semantics_test.go:76`, the taxonomy-across-sources prefix).
      **Barrier reading before 5.1:** `task entity-id:audit` reported 7 findings, all `domain_unregistered` on
      the seven un-migrated agentic builders that task 5.1 owns — zero lexical findings, and every one of the
      original 30 resolved. 5.1 cleared those seven; the reading after 5.1 is recorded on that row.

- [x] 5.7 `task schema:generate`; `git diff --exit-code schemas/ specs/` → commit any regenerated output.
      **Done:** `task schema:generate` produced no diff; `task schema:check-changes` exits 0 (no drift).

- [x] 5.8 Values that leave the graph (inventory P5–P6; owner item O-10): `vocabulary/export/export.go:123-126` emits
      the IRI path in the canonical order from named fields; the PR body announces the export IRI path and the
      `graphSummary` `entity_types[].type` value as published-artifact breaks that fresh state does not re-mint.
      **Done:** `vocabulary/export/export.go` `subjectToIRI` emits `{org}/{platform}/{system}/{domain}/{type}/{instance}` from the named fields (`TestSubjectToIRIFollowsCanonicalOrder`, M11); `processor/graph-query/summary.go` builds `EntityTypeSummary.Type` = `system.domain.type` by named field (`TestGraphSummaryTypeKeyFollowsCanonicalOrder`, M10). Both announced in the PR body as published-artifact breaks.

## 6. Boundary enforcement, hierarchy, and #1096

- [ ] 6.1 graph-ingest reads `deps.Platform` at construction (`CreateGraphIngest`, `component.go:644`); the structural
      gate calls `ValidateEntityIDAuthority` for the candidate SUBJECT identity only — never for `@id` objects, which
      keep structural validation; no stub is created and an absent object is permitted (`graph-ingest/spec.md:776-780`) — on the fact lane, every `graph.mutation.>` operation, and
      direct persistence, before KV I/O; metered once as `mutation_rejections{reason="authority_foreign"|"authority_claimed"}`;
      loud log names lane and segment index, never the identity. Mutation of an existing foreign subject from any
      non-import lane is rejected `foreign_authority` — an import is a read-only mirror (ruled O-12(a)); local facts
      about an import live on a local subject that references it.
- [ ] 6.2 `JetStreamPort` gains `Import bool` (`"import"`); the port schema and `configs/graph-backend.json` (the
      reference graph-backend composition; it composes graph-ingest) carry one declared import lane as the reference.
      The two federation configs this row once contrasted against were deleted by PR #1130 (#1129).
- [ ] 6.3 `processor/rule`: add a `platform` field to the action executor plumbed from `deps.Platform` at construction
      (the processor holds none today); `actions.go:1575-1583` mints from it; the firing entity remains the parent
      reference; delete the `SplitN` read-back. Before `stampRun` (`:1697-1700`) the action evaluates
      `semtypes.ValidateEntityIDAuthority(entityID, org, platform, false)`: when the firing loop is local the anchor pair
      is stamped as today; when it is a foreign-authority import BOTH anchor writes are skipped deliberately — no
      mutation request targets the foreign subject — and the skip is recorded as
      `rule_run_anchor_skipped_total{reason="foreign_authority"}` with an Info log naming the rule; `agentrun.Mint`
      receives the firing entity as `originEntityID` (3.8) in both cases. #1096 is complete only when 2.6 is GREEN and
      M6a–M6c are recorded.
- [ ] 6.4 `graph/inference/hierarchy.go`: `GetHierarchyTriples` returns `nil, nil` for an entity whose positions 1–2
      differ from the deployment authority (no container, no membership, no inverse sibling edge, no warning) — the
      pair reaches `NewHierarchyInference` (which carries none today, `hierarchy.go:109-114`;
      `processor/graph-ingest/component.go:1371-1376`) through `HierarchyConfig` from the `deps.Platform` read 6.1
      adds; the skip is accepted by ruling on every lane;
      containers use `ReservedInstanceTokens`; a container whose ID would exceed 256 bytes returns the coded
      structural error instead of a padded overflow.

## 7. Gates and landing (AGENTS.md:63-68 order)

- [ ] 7.1 Focused gates, results recorded verbatim: `task lint`; `go test -race -count=1 ./...`;
      `scripts/run-integration-tests.sh` (what CI runs); `go test ./test/contract/...`; `task entity-id:audit`;
      `task schema:generate && git diff --exit-code schemas/ specs/`;
      `openspec validate entity-id-segment-semantics --strict --no-interactive`.
- [ ] 7.2 Covering e2e tiers on the landing branch, one at a time on the shared host, results recorded verbatim:
      `task e2e:core`; `task e2e:structural`; `task e2e:statistical`; `task e2e:semantic`; `task e2e:agentic`;
      `task e2e:lessons`; `task e2e:lifecycle`; `task e2e:ops`; `task e2e:crud-tools`; `task e2e:research-graph`.
      Excluded with reason recorded: `slow-consumer`, `throughput`, `openai-responses`, `deep-research` (no position
      literal). Cold-start proof: each tier starts on newly provisioned NATS storage with readiness fail-closed
      through initial replay.
      **SLICE A RUN — 2026-08-27, all ten GREEN, one at a time on a host with no competing gate.** The row stays open
      because slice B changes the same paths and must re-run it; this records what slice A proved.

      | Tier | exit | evidence |
      |---|---|---|
      | `e2e:core` | 0 | `graph_roundtrip_trace_entries:2`, health+dataflow on the production binary, round-trip on the e2e twin |
      | `e2e:structural` | 0 | `hierarchy_container_count:46` (min 32), `inverse_symmetry_valid:1`, `rule_firings:6`, `authority_hierarchy_provenance_triples:836`, `validation_errors:0` |
      | `e2e:statistical` | 0 | `hierarchy_container_count:46`; gateway response-shape probe green |
      | `e2e:semantic` | 0 | 11m24s, 48 stages; `graphrag_local_community_id:c360.logistics.document.content.group.container` (a level-4 taxonomy container + two padding tokens, i.e. the rewritten builder on the wire), `gateway_shape_probes_checked:3`, `communities_total:17`, `embedding_resolved_total:111`, `validation_errors:0` |
      | `e2e:agentic` | 0 | `graph_loop_triples:10`, `graph_model_triples:6` — the loop-execution and model-endpoint entities found in the graph at the new order AND under `platform.id`, which is the discriminating half: the scenario now asks for `semstreams-agentic`, so the retired `instance_id` precedence would return nothing |
      | `e2e:lessons` | 0 | `assertions_run=3` |
      | `e2e:lifecycle` | 0 | `rule-driven-transition_duration_ms:157` — UDP → graph-ingest → entity-watcher → rule → `Manager.Transition` → MISSIONS KV → gateway, with positions 1-2 stamped from `deps.Platform` |
      | `e2e:ops` | 0 | `assertions_run=9`, including both wire subject-shape checks (`*.*.diagnosis.ops.finding.*`, `*.*.lesson.agent.record.*`) read by name off subjects the running binary produced |
      | `e2e:crud-tools` | 0 | `tool_executions:4`, `hotreload_pickup_latency_ms:213` |
      | `e2e:research-graph` | 0 | both fixture modes; `loops_completed_total:2`, `orchestration_triples_total:17`/`21` |

      **Two tiers were RED first and the failures were real, not fixtures drifting.** Both are the same defect class,
      and it is the one the adopter-seam rule names — a caller predicting a value the framework owns:
      - `e2e:lifecycle`: `docker/compose/lifecycle.yml` hardcoded the seed `c360.test.gcs.lifecycle.mission.m001`
        while `configs/lifecycle-flow.json`'s `platform.id` is `semstreams-lifecycle`. Since the mission-command
        processor now stamps its own authority, the seeded and commanded entities were different keys and the tier
        failed 5s later as "rule did not transition mission to flying", naming the symptom. Fixed by aligning the
        seed, the scenario constant, and — so the next such mismatch is loud — a boot guard in `seedMission`
        (`cmd/e2e-semstreams/main.go`) that rejects a `--lifecycle-seed` whose authority pair is not this
        deployment's, naming both pairs.
      - `e2e:research-graph`: `test/e2e/scenarios/research-graph/scenario.go` carried `PlatformInstance:
        "rg-e2e-001"`, the retired `instance_id`, against a config whose `platform.id` is `research-graph-e2e`.

      Hole class enumerated rather than fixed one-by-one: **every one of the 16 configs that carried `instance_id`
      had a value different from its `platform.id`**, so any hardcoded copy is now wrong. Grepping all 16 retired
      values across `*.go`/`*.json`/`*.yml`/`*.md` (excluding `docs/` and `openspec/`) returns exactly three live
      sites — the two above plus `test/e2e/scenarios/agentic/scenario.go`, already fixed — and two deliberate
      non-sites: `config/config_test.go:419` (the O-2 rejection fixture, which must keep `instance_id`) and
      `processor/agentic-tools/decide_test.go` (an arbitrary self-consistent unit-test platform value).
- [ ] 7.3 Implementation review by `semstreams-reviewer`; verdict and every finding's disposition recorded in
      `conformance.md`.
      **Round 1:** verdict CHANGES REQUESTED at `5f66ce37` (0 BLOCKING, 3 HIGH, 7 MEDIUM, 4 NIT); every finding's
      disposition, the one scoped deviation, and the stated residue are in `conformance.md` §"Implementation-review
      round".
      **Round 2:** narrow re-review `5f66ce37` → `897476cf`, CHANGES REQUESTED (0 BLOCKING, 1 HIGH, 3 MEDIUM, 1 NIT);
      HIGH-1 confirmed closed by independent reproduction. The HIGH was a defect round 1 introduced in the published
      layer, not a survivor. Dispositions in the same `conformance.md` section. Re-review is outstanding.
- [ ] 7.4 Owner-run cross-agent round where the owner asks for it; fixes and re-review recorded in `conformance.md`.
- [ ] 7.5 `openspec archive entity-id-segment-semantics` + spec sync as the final content commit; narrow reviewer
      check of the archive/spec sync recorded.
- [ ] 7.6 Undraft; PR body carries `implemented-by`, the per-sister migration list, the two values that leave the
      graph, the owner ruling of 2026-08-26 as applied, and the e2e evidence pointers. No task asserts CI state.
