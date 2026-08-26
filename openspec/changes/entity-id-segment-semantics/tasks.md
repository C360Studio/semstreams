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
`test/e2e/scenarios/ops/scenario.go:604,712`, `tiered_structural.go:428-434`, `research-graph/scenario.go:201-203`,
`cmd/e2e-semstreams/mission/command.go:59-66,324-328` (e2e position literals and wire authority);
`configs/cloud-federation.json` and `configs/edge-federation.json` compose no graph-ingest (`grep -c` → 0);
`configs/graph-backend.json` does.

## 1. Claim

- [ ] 1.1 Worktree `../semstreams-wt/claude/gh1095-entity-id-segment-semantics` from `origin/main`; draft PR #1099 open
      with `Closes #1095`, `Closes #1096`, and `implemented-by: <persona>` in the body; this change directory,
      `docs/adr/102-entity-id-segment-semantics.md`, and the two `docs/proposals/gh1095-*` documents are its first
      commit. Slices A (contract + sweep) and B (boundary + import lane + #1096) land in this ONE PR; the PR body is
      a published layer carrying the per-sister migration list (design §D), the two values that leave the graph
      (export IRI path, `graphSummary` `entity_types[].type`), and the owner rulings on O-2, O-6, O-9, O-11–O-14.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `pkg/types/entity_id_semantics_test.go`: `TestEntityIDKeyOrderIsSystemBeforeDomain` — build
      `EntityID{Org:"acme",Platform:"dep1",System:"src",Domain:"git",Type:"commit",Instance:"a1"}`; assert
      `Key() == "acme.dep1.src.git.commit.a1"`; assert `ParseEntityID` of that string round-trips every field.
      `TestPrefixLevelsAreNamed` — assert `DeploymentPrefix()=="acme.dep1"`, `SourcePrefix()=="acme.dep1.src"`,
      `TaxonomyPrefix()=="acme.dep1.src.git"`, `TypePrefix()=="acme.dep1.src.git.commit"`, and that
      `PrefixLevel(n)` for n∈{2,3,4,5} returns the same strings. `TestTaxonomyAcrossSourcesIsPatternNotPrefix` —
      `ValidateEntityIDPattern("acme.dep1.*.git.*.*")` is nil and `ValidateEntityIDPrefix("acme.dep1.*.git")` returns
      `entity_id_prefix_invalid`. Does not compile at baseline (new symbols).
- [ ] 2.2 `pkg/types/entity_domain_authority_test.go`: `TestEntityDomainAuthorityMirrorsPredicateAuthority` — an
      undelegated domain with an empty producer is rejected; an exact `domain.type` delegation admits only that type;
      the returned error carries code `entity_id_authority_invalid` and reason `domain_undelegated`.
      `TestEntityDomainAuthorityReservedPassesForEveryProducer` — every reserved domain passes for an empty and for
      an arbitrary producer. Does not compile at baseline.
- [ ] 2.3 `pkg/types/entity_id_authority_test.go`: `TestAuthorityRejectionIsCodedAndIdentityFree` — table over
      (candidate, org, platform, importLane) → reason `foreign_authority` / nil; assert `errs.Code ==
      "entity_id_authority_invalid"`, detail keys are exactly `reason`, `segment_index`, `lane`, and no detail value
      contains a dot-joined identity. `TestAuthorityRejectionLocalClaimOnImportLane` — a candidate equal to the local
      pair on an import lane returns `local_authority_claimed`. Does not compile at baseline.
- [ ] 2.4 `agentic/entity_ids_semantics_test.go`: for each of the nine framework builders assert the produced ID's
      positions 3–4 are `<component>.<reserved-domain>` in the new order (e.g. loop execution →
      `agentic-loop.agent.execution`), and that `graph.NewAlertEvent(org, platform, …)` / `ruleTriggerEntityID(org,
      platform, …)` carry the supplied pair rather than `semstreams.framework`. MUST fail at baseline on every row
      (the two constructors do not compile with the new parameters).
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
      entity is persisted with no `hierarchy.*` triple and no `…group` container exists. MUST fail at baseline (no
      gate exists: the foreign write lands and the container is minted under `acme.dep2`).
- [ ] 2.6 `processor/rule/actions_run_scope_integration_test.go` (`//go:build integration`):
      `TestRunScopeNewMintsUnderDeploymentAuthority` — deployment `acme.dep1`; a rule with `run_scope=new` fires on
      `foreign.dep9.agentic-loop.agent.execution.<uuid>` — a peer deployment's own loop execution, because
      `LoopIDFromExecutionEntityID` (`actions.go:1554`, `entity_ids.go:167`) admits only that family and any other
      entity takes the warn-and-inherit fallback (`:1555-1570`); assert the stamped `agvocab.LoopRunEntityID` begins with
      `acme.dep1.` and the firing entity is referenced as parent. MUST fail at baseline (#1096).
      `processor/rule/entity_substitution_test.go`: `TestSegmentTokensResolveByName` — `$entity.system` and
      `$entity.domain` resolve to positions 3 and 4 of the NEW order; `TestSegmentTokensUnresolvedOnInvalidID` — a
      five-position value leaves every token unresolved and the warning fires. MUST fail at baseline (first) /
      GREEN at baseline (second; forced omission 4.5 covers it).
      `processor/agentic-tools/emit_lesson_test.go`: `TestAppliesToThreeSegmentsIsSourceScope` — a lesson with
      `id:acme.dep1.src` matches a loop scoped to `acme.dep1.src.git.commit.a1` and not `acme.dep1.other.git.commit.a1`.
      GREEN at baseline (segment-boundary matching is order-agnostic, inventory L2): a documenting test, outside the
      2.9 baseline capture; it pins the meaning the spec delta assigns to three positions.
- [ ] 2.7 `processor/graph-query/summary_test.go`: `TestGraphSummaryTypeKeyFollowsCanonicalOrder` — the
      `EntityTypeSummary.Type` for `acme.dep1.src.git.commit.a1` is `src.git.commit`, built from named fields.
      `graph/clustering/entityid_provider_test.go`: `TestEntityIDEdgesReadPositionsByName` — sibling prefix and
      source-peer affinity for the new order. `graph/clustering/summarizer_test.go`: `TestSummaryGroupsByNamedDomain`.
      `vocabulary/export/export_test.go`: `TestSubjectToIRIFollowsCanonicalOrder` — the IRI path is
      `…/entities/acme/dep1/src/git/commit/a1`. `examples/processors/iot_sensor/processor_test.go`:
      `TestParseZoneEntityIDReadsNamedPositions`. All MUST fail at baseline.
- [ ] 2.8 `internal/entityidaudit/audit_test.go`: `TestAuditFlagsAuthorityLiteral` (a `Sprintf` builder with a
      product-name platform literal) and `TestAuditFlagsFormatPrefixAuthorityLiteral` (a trailing-dot constant and a
      `semstreams.framework.%s…` format) report `authority_literal`; `TestAuditFlagsUnregisteredDomain` reports
      `domain_unregistered`. `config/config_test.go`: `TestConfigRejectsOversizedAuthorityPair` — a 171-byte
      `org`+`id` pair does not load and the error names the trigger family and 170. All MUST fail at baseline.
- [ ] 2.9 RED capture on baseline code (§2 tests only), recorded here verbatim (package + test name + failing
      assertion or build error):

  ```
  go test -race -count=1 -run 'TestEntityIDKeyOrderIsSystemBeforeDomain|TestPrefixLevelsAreNamed|TestTaxonomyAcrossSourcesIsPatternNotPrefix|TestEntityDomainAuthority|TestAuthorityRejection' ./pkg/types/
  go test -race -count=1 -run 'Semantics' ./agentic/ ./graph/ ./processor/rule/
  go test -race -tags=integration -count=1 -run 'TestAuthorityGate|TestImportLane|TestHierarchySkipsForeignAuthority' ./processor/graph-ingest/
  go test -race -tags=integration -count=1 -run 'TestRunScopeNewMintsUnderDeploymentAuthority' ./processor/rule/
  go test -race -count=1 -run 'TestSegmentTokens' ./processor/rule/
  go test -race -count=1 -run 'TestGraphSummaryTypeKeyFollowsCanonicalOrder' ./processor/graph-query/
  go test -race -count=1 -run 'TestEntityIDEdgesReadPositionsByName|TestSummaryGroupsByNamedDomain' ./graph/clustering/
  go test -race -count=1 -run 'TestSubjectToIRIFollowsCanonicalOrder' ./vocabulary/export/
  go test -race -count=1 -run 'TestParseZoneEntityIDReadsNamedPositions' ./examples/processors/iot_sensor/
  go test -race -count=1 -run 'TestAuditFlags' ./internal/entityidaudit/
  go test -race -count=1 -run 'TestConfigRejectsOversizedAuthorityPair' ./config/
  ```

## 3. Contract — `pkg/types` and `config`

- [ ] 3.1 Reorder `EntityID` fields to `Org, Platform, System, Domain, Type, Instance`; `Key()`/`ParseEntityID` follow;
      keep `EntityType()` = `{Domain, Type}`; update the struct comment to the position table in the spec delta.
- [ ] 3.2 Replace `TypePrefix/SystemPrefix/DomainPrefix/PlatformPrefix` with the named levels `DeploymentPrefix` (2),
      `SourcePrefix` (3), `TaxonomyPrefix` (4), `TypePrefix` (5), plus `PrefixLevel(n)`; `IsSameSystem` → `IsSameSource`;
      `IsSameDomain` removed (not a prefix under the new order; `grep -rn IsSameDomain --include='*.go'` → tests only).
- [ ] 3.3 Add `EntityDomainDelegation`, `EntityDomainAuthority`, `NewEntityDomainAuthority`, `Authorize(producer,
      domain, entityType)`, and the reserved set `FrameworkEntityDomains = {agent, ops, graph}` — plus `gateddag` only if
      owner item O-9 declines the gated-DAG re-slot; mirror `vocabulary/namespace_authority.go` shape-for-shape
      (`Producer` from the trusted boundary, exact matches only).
- [ ] 3.4 Export `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`, reasons
      `EntityIDReasonForeignAuthority = "foreign_authority"`, `EntityIDReasonLocalAuthorityClaimed =
      "local_authority_claimed"`, `EntityIDReasonDomainUndelegated = "domain_undelegated"`, detail key
      `EntityIDDetailLane = "lane"`; add `ValidateEntityIDAuthority(candidate, org, platform string, importLane bool)
      error` (strings — `types.PlatformMeta` lives in the root `types` package and `pkg/types` must not import it).
      Details never carry identity bytes.
- [ ] 3.5 Reserve the container padding tokens: `ReservedInstanceTokens = {group, container, level}`;
      `ValidateEntityID` unchanged (lexical); the audit (5.6) and `graph/inference/hierarchy.go` consume the constant.
- [ ] 3.6 `internal/semantictest.EntityID` positional args follow the new order; `message.ParseEntityID` delegator
      unchanged (alias).
- [ ] 3.7 Authority-pair bound at config load (`config/config.go:225-241`): declare the framework identity family
      table in `pkg/types` (`FrameworkIdentityFamilies`, each family's fixed suffix; the rule trigger family = 86 bytes
      today) and `MaxAuthorityPairBytes = 256 − longest = 170` there — `config` imports neither `graph` nor
      `processor/rule` and the trigger prefix is unexported (`graph_event_identity.go:14`); `graph/events.go:20` and
      `ruleTriggerEntityID` build their prefixes from the `pkg/types` family entries so the number is never
      hand-copied; `Validate` rejects `len(org)+len(id) > MaxAuthorityPairBytes` naming the binding family. Amends
      ADR-076 d2.

## 4. Forced omissions — one per new parser/builder/mapper (commit GREEN first; restore by `cp` + `shasum`)

Each row: copy the file aside, delete the CALL (not the error check), run the named test, record the verbatim
`--- FAIL`, restore with `cp`, and record `shasum` equality of source and backup.

- [ ] 4.1 M1 `ParseEntityID`: swap the two index assignments back to `Domain: parts[2], System: parts[3]` →
      `TestEntityIDKeyOrderIsSystemBeforeDomain` MUST fail.
- [ ] 4.2 M2 `EntityDomainAuthority.Authorize`: return nil unconditionally → `TestEntityDomainAuthorityMirrorsPredicateAuthority` MUST fail.
- [ ] 4.3 M3 graph-ingest gate: delete the `ValidateEntityIDAuthority` call on the fact lane →
      `TestAuthorityGateRejectsForeignOnFactLane` MUST fail.
- [ ] 4.4 M4 import lane: ignore the port's `import` flag → `TestImportLaneAcceptsForeignRejectsLocalClaim` MUST fail.
- [ ] 4.5 M5 `entityPartNames`: swap the two names back → `TestSegmentTokensResolveByName` MUST fail; delete the
      `IsValidEntityID` guard in `applyEntityPartsSubstitutions` → `TestSegmentTokensUnresolvedOnInvalidID` MUST fail.
- [ ] 4.6 M6 `actions.go` run-scope mint: restore `idParts[0], idParts[1]` → `TestRunScopeNewMintsUnderDeploymentAuthority` MUST fail.
- [ ] 4.7 M7 audit `authority_literal` rule: skip the `go-format-prefix` and `go-dotted-constant` surfaces →
      `TestAuditFlagsAuthorityLiteral` and `TestAuditFlagsFormatPrefixAuthorityLiteral` MUST fail.
- [ ] 4.8 M8 `SourcePrefix`/`PrefixLevel(3)`: return four positions → `TestPrefixLevelsAreNamed` MUST fail.
- [ ] 4.9 M9 audit `domain_unregistered` rule: return no finding → `TestAuditFlagsUnregisteredDomain` MUST fail.
- [ ] 4.10 M10 `summary.go` typeKey builder: rebuild from `segs[2..4]` → `TestGraphSummaryTypeKeyFollowsCanonicalOrder` MUST fail.
- [ ] 4.11 M11 `subjectToIRI`: emit `{domain}/{system}` in the old order → `TestSubjectToIRIFollowsCanonicalOrder` MUST fail.
- [ ] 4.12 M12 `getSystem`/`parseEntityID` rewrites: restore `parts[3]`/index reads → `TestEntityIDEdgesReadPositionsByName`
      and `TestSummaryGroupsByNamedDomain` MUST fail.
- [ ] 4.13 M13 hierarchy foreign-skip: delete the authority check in `GetHierarchyTriples` → `TestHierarchySkipsForeignAuthority` MUST fail.
- [ ] 4.14 M14 config-load bound: delete the `MaxAuthorityPairBytes` check → `TestConfigRejectsOversizedAuthorityPair` MUST fail.
- [ ] 4.15 M15 `ParseZoneEntityID`: restore `parts[2] != "facility" || parts[3] != "zone"` → `TestParseZoneEntityIDReadsNamedPositions` MUST fail.

## 5. Sweep — builders, patterns, configs, docs, examples, audit

- [ ] 5.1 Framework builders (`agentic/entity_ids.go`, `agent_lesson_entity.go`, `web_observation_entity.go`,
      `ops_diagnosis_entity.go`, `graph/events.go`, `processor/rule/graph_event_identity.go`, gated-dag participant,
      e2e mission, `examples/processors/iot_sensor/payload.go`) emit `org.platform.<component>.<domain>.<type>.<instance>`
      and authorize their domain; ADR-076 families drop the `semstreams.framework` literal and `graph.NewAlertEvent`
      / `ruleTriggerEntityID` gain `org, platform string` parameters (exported-surface change on `graph/`; no sister
      caller). Example builders `examples/processors/document/payload_document.go:50`, `payload_sensor.go:40`,
      `payload_maintenance.go:41`, `payload_observation.go:41`, `examples/processors/weather_station/payload.go:100`
      swap positions 3–4.
- [ ] 5.2 Index-position readers (inventory W3, W9, C3): `graph/inference/hierarchy.go`,
      `graph/clustering/entityid_provider.go:231-236` (`getSystem`, live through `NewEntityIDProvider` at
      `processor/graph-clustering/component.go:1331` — stays correct until gh606 deletes it),
      `graph/clustering/summarizer.go:719-731` (domain grouping of the summary prompt),
      `processor/graph-query/summary.go:198-202` (`EntityTypeSummary.Type` = `system.domain.type` by named field — an
      API value change named in the PR body), `graphrag.go`, `agentic/entity_ids.go:161-171`,
      `agentic/agentrun/agentrun.go:158-170`, `examples/processors/iot_sensor/processor.go:278-288` (the silent
      `facility.zone` reader) read by named field via `ParseEntityID`, never by raw index.
- [ ] 5.3 Declaration patterns, config literals, and e2e literals (inventory W5–W6, W8, W10): `agentic/agentrun/agentrun.go:100`,
      `internal/builtinprojection/contracts.go:26,56`, `processor/gated-dag/participant.go:17`,
      `cmd/e2e-semstreams/mission/state.go:28`, `configs/*` three literal patterns; lesson record prefix
      (`agent_lesson_entity.go:85-93`); `entityPartNames` resolves by name; `test/e2e/scenarios/ops/scenario.go:604`
      and `:712`, `test/e2e/scenarios/tiered_structural.go:428-434` (rename the `domain`/`system` variables with the
      swap), `test/e2e/scenarios/research-graph/scenario.go:201-203`, `test/e2e/scenarios/tiered.go:350` value
      expectations, and `cmd/e2e-semstreams/mission/command.go:59-66,324-328` (mission minted from `deps.Platform`,
      never from the wire) rewritten in the same commit so no tier reports a position-literal mismatch that reads as
      a regression (`test/e2e/client/nats.go:965-974` is arity-only and stays).
- [ ] 5.4 `config/config.go`: `GetPlatform()` returns `Platform.ID`; `instance_id` present in a loaded config fails
      load with guidance naming `platform.id` (`removedConfigFields` precedent); `cmd/semstreams/main.go:477-484` and
      `cmd/e2e-semstreams/main.go:628-634` drop the precedence; every `configs/*.json` drops `instance_id`. Subject to
      owner item O-2.
- [ ] 5.5 Docs: `docs/concepts/*`, `docs/basics/*`, `CLAUDE.md`, `AGENTS.md`, `openspec/project.md:91`,
      `openspec/specs/structural-identity/spec.md:6-13` name the new order (29 files, inventory §1.14 list);
      `docs/concepts/18-rule-driven-artifacts.md:72,118` (whole-ID subject examples) state that a `$entity.id` subject
      carries the canonical order and that position-literal subscriptions must follow it;
      `docs/proposals/gh606-derived-communities-design.md` is RESTATED at `:24-25`, `:29`, `:65-71`, `:76-79`, `:90`,
      `:126`, `:271`, `:334-335` per design §D (level 1 = source; served level per O-11; symbol names after 3.2).
- [ ] 5.6 `internal/entityidaudit`: add the surfaces `go-format-prefix` (a `fmt.Sprintf` format whose dot-separated
      tokens are read as positions, `%s` = template position) and `go-dotted-constant` (a string constant of ≥2 dotted
      tokens ending in `.`); add rules `authority_literal` (literal non-`*`, non-template value in positions 1–2 of a
      production builder, declaration, or prefix constant) and `domain_unregistered` (literal position-4 value outside
      the reserved set in production Go); classify the 30 existing arity findings at their exact occurrence; fixture
      tests per rule (2.8); `task entity-id:audit` added to the CI `Lint` job in `.github/workflows/ci.yml`.
- [ ] 5.7 `task schema:generate`; `git diff --exit-code schemas/ specs/` → commit any regenerated output.
- [ ] 5.8 Values that leave the graph (inventory P5–P6; owner item O-10): `vocabulary/export/export.go:123-126` emits
      the IRI path in the canonical order from named fields; the PR body announces the export IRI path and the
      `graphSummary` `entity_types[].type` value as published-artifact breaks that fresh state does not re-mint.

## 6. Boundary enforcement, hierarchy, and #1096

- [ ] 6.1 graph-ingest reads `deps.Platform` at construction (`CreateGraphIngest`, `component.go:644`); the structural
      gate calls `ValidateEntityIDAuthority` for the candidate SUBJECT identity only — never for `@id` objects, which
      keep structural validation; no stub is created and an absent object is permitted (`graph-ingest/spec.md:776-780`) — on the fact lane, every `graph.mutation.>` operation, and
      direct persistence, before KV I/O; metered once as `mutation_rejections{reason="authority_foreign"|"authority_claimed"}`;
      loud log names lane and segment index, never the identity. Mutation of an existing foreign subject: the default
      rejects until owner item O-12 is ruled; the ruling is applied here and in the delta.
- [ ] 6.2 `JetStreamPort` gains `Import bool` (`"import"`); the port schema and `configs/graph-backend.json` (which
      composes graph-ingest — `cloud-federation.json`/`edge-federation.json` do not) carry one declared import lane as
      the reference.
- [ ] 6.3 `processor/rule`: add a `platform` field to the action executor plumbed from `deps.Platform` at construction
      (the processor holds none today); `actions.go:1575-1583` mints from it; the firing entity remains the parent
      reference; delete the `SplitN` read-back. The run anchor at `:1697-1700` keeps writing to the firing entity;
      for an imported firing entity it is rejected by the gate, metered `mutation_rejections{reason="authority_foreign"}`,
      and logged until O-12 is ruled — O-12 is pre-landing (it gates `Closes #1096` for imported firing entities), so
      the ruling is applied here before 7.1 and the reject-and-meter state never ships.
- [ ] 6.4 `graph/inference/hierarchy.go`: `GetHierarchyTriples` returns `nil, nil` for an entity whose positions 1–2
      differ from the deployment authority (no container, no membership, no inverse sibling edge, no warning) — the
      pair reaches `NewHierarchyInference` (which carries none today, `hierarchy.go:109-114`;
      `processor/graph-ingest/component.go:1371-1376`) through `HierarchyConfig` from the `deps.Platform` read 6.1
      adds; the skip stands regardless of O-12;
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
- [ ] 7.3 Implementation review by `semstreams-reviewer`; verdict and every finding's disposition recorded in
      `conformance.md`.
- [ ] 7.4 Owner-run cross-agent round where the owner asks for it; fixes and re-review recorded in `conformance.md`.
- [ ] 7.5 `openspec archive entity-id-segment-semantics` + spec sync as the final content commit; narrow reviewer
      check of the archive/spec sync recorded.
- [ ] 7.6 Undraft; PR body carries `implemented-by`, the per-sister migration list, the two values that leave the
      graph, the owner rulings applied (O-2, O-6, O-9, O-11–O-14), the tag-split ruling (O-7), and the e2e evidence
      pointers. No task asserts CI state.
