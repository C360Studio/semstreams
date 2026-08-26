# Tasks — entity-id-segment-semantics

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
in any OPEN task line as a live caveat. They appear only in the RED-capture task 2.7 once it is CLOSED. Everywhere
else say "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (measured at `5cc0c7fb`): `pkg/types/entity_id.go:82-134` (struct, `Key`, `Parse` by index), `:248-348`
(prefix helpers, zero production callers), `:11-67` (coded contract); `agentic/entity_ids.go:29,82,138`,
`agent_lesson_entity.go:68,92`, `web_observation_entity.go:79`, `ops_diagnosis_entity.go:56` (builders);
`graph/events.go:19-20,290-301` and `processor/rule/graph_event_identity.go:12-37` (ADR-076 families);
`processor/rule/actions.go:1575-1583,1710-1712` (#1096); `processor/rule/entity_substitution.go:55-57,73-83`;
`processor/agentic-tools/emit_lesson.go:55-57,862-885`; `graph/inference/hierarchy.go:129-141,257-276`;
`processor/graph-ingest/component.go:1888` (syntax-only gate), `:1464-1507` (fact lane), `:2614-2626` (suffix
index), no `deps.Platform` read in graph-ingest; `config/config.go:772-778` (precedence); `Taskfile.yml:96-99`
(audit not in `.github/workflows/ci.yml`); `task entity-id:audit` → 30 `entity_id_invalid:arity` findings;
`vocabulary/namespace_authority.go:28-124` (donor) with one consumer `agentic/tools.go:369-382`;
`message/base_message.go:234-238` (wire meta = created_at, received_at, source); `deps.Platform` = 18 non-test lines
(17 `processor/`, 1 `service/component_manager.go:183`) and `platform.{Org,Platform}` = 62 lines;
`processor/rule/actions.go:881,1865` (config-authored subject lane, `$entity.id` per
`docs/concepts/18-rule-driven-artifacts.md:72,118`); `vocabulary/export/export.go:123-126` (export IRI in wire order);
`processor/graph-query/summary.go:198-202` → GraphQL `EntityTypeSummary.type` (`gateway/graph-gateway/component.go:1870`);
`graph/clustering/entityid_provider.go:231-236` (live via `processor/graph-clustering/component.go:1331`) and
`graph/clustering/summarizer.go:719-731` (index reads with no position test); `test/e2e/scenarios/ops/scenario.go:604,712`
(literal position assertions).

## 1. Claim

- [ ] 1.1 Worktree `../semstreams-wt/claude/gh1095-entity-id-segment-semantics` from `origin/main`; draft PR open
      with `Closes #1095`, `Closes #1096`, and `implemented-by: <persona>` in the body; this change directory,
      `docs/adr/102-entity-id-segment-semantics.md`, and the two `docs/proposals/gh1095-*` documents are its first
      commit. The PR body is a published layer: it carries the per-sister migration list from design §D verbatim.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `pkg/types/entity_id_semantics_test.go`: `TestEntityIDKeyOrderIsSystemBeforeDomain` — build
      `EntityID{Org:"acme",Platform:"dep1",System:"src",Domain:"git",Type:"commit",Instance:"a1"}`; assert
      `Key() == "acme.dep1.src.git.commit.a1"`; assert `ParseEntityID` of that string round-trips every field.
      `TestPrefixLevelsAreNamed` — assert `DeploymentPrefix()=="acme.dep1"`, `SourcePrefix()=="acme.dep1.src"`,
      `TaxonomyPrefix()=="acme.dep1.src.git"`, `TypePrefix()=="acme.dep1.src.git.commit"`, and that
      `PrefixLevel(n)` for n∈{2,3,4,5} returns the same strings. Does not compile at baseline (new symbols).
- [ ] 2.2 `pkg/types/entity_domain_authority_test.go`: `TestEntityDomainAuthorityMirrorsPredicateAuthority` — a
      reserved domain passes for every producer; an undelegated domain with an empty producer is rejected; an exact
      `domain.type` delegation admits only that type; the returned error carries code
      `entity_id_authority_invalid` and reason `domain_undelegated`. Does not compile at baseline.
- [ ] 2.3 `pkg/types/entity_id_authority_test.go`: `TestAuthorityRejectionIsCodedAndIdentityFree` — table over
      (candidate, local authority, lane) → expected reason `foreign_authority` / `local_authority_claimed` / nil;
      assert `errs.Code == "entity_id_authority_invalid"`, detail keys are exactly `reason`, `segment_index`, `lane`,
      and no detail value contains a dot-joined identity. Does not compile at baseline.
- [ ] 2.4 `agentic/entity_ids_semantics_test.go`: for each of the nine framework builders assert the produced ID's
      positions 3–4 are `<component>.<reserved-domain>` in the new order (e.g. loop execution →
      `agentic-loop.agent.execution`), and that `graph.NewAlertEvent` / `ruleTriggerEntityID` carry the supplied
      `org.platform` rather than `semstreams.framework`. MUST fail at baseline on every row.
- [ ] 2.5 `processor/graph-ingest/authority_gate_integration_test.go` (`//go:build integration`; real NATS via the
      package's existing test client): `TestAuthorityGateRejectsForeignOnFactLane` — deployment `acme.dep1`; publish a
      Graphable whose ID is `acme.dep2.src.git.commit.a1` on a non-import port; assert no `ENTITY_STATES` key is
      created, `mutation_rejections{reason="authority_foreign"}` == 1, and the reply (mutation lane variant) decodes
      into a fresh value carrying code `entity_id_authority_invalid`. `TestImportLaneAcceptsForeignRejectsLocalClaim`
      — same entity on a port declared `"import": true` is created unchanged; an entity claiming `acme.dep1` on that
      port is rejected with `local_authority_claimed`. MUST fail at baseline (no gate exists: the foreign write lands).
- [ ] 2.6 `processor/rule/actions_run_scope_integration_test.go` (`//go:build integration`):
      `TestRunScopeNewMintsUnderDeploymentAuthority` — deployment `acme.dep1`; a rule with `run_scope=new` fires on
      `foreign.dep9.src.agent.execution.<uuid>`; assert the stamped `agvocab.LoopRunEntityID` begins with
      `acme.dep1.` and the firing entity is referenced as parent. MUST fail at baseline (#1096).
      `processor/rule/entity_substitution_test.go`: `TestSegmentTokensResolveByName` — `$entity.system` and
      `$entity.domain` resolve to positions 3 and 4 of the NEW order. MUST fail at baseline.
      `processor/agentic-tools/emit_lesson_test.go`: `TestAppliesToThreeSegmentsIsSourceScope` — a lesson with
      `id:acme.dep1.src` matches a loop scoped to `acme.dep1.src.git.commit.a1` and not `acme.dep1.other.git.commit.a1`.
      `processor/graph-query/summary_test.go`: `TestGraphSummaryTypeKeyFollowsCanonicalOrder` — the
      `EntityTypeSummary.Type` for `acme.dep1.src.git.commit.a1` is `src.git.commit`, built from named fields. MUST
      fail at baseline. `graph/clustering/entityid_provider_test.go`: `TestGetSystemReadsNamedField` and
      `graph/clustering/summarizer_test.go`: `TestSummaryGroupsByNamedDomain` — position reads by name under the new
      order. MUST fail at baseline.
- [ ] 2.7 RED capture on baseline code (§2 tests only), recorded here verbatim (package + test name + failing
      assertion or build error):

  ```
  go test -race -count=1 -run 'TestEntityIDKeyOrderIsSystemBeforeDomain|TestPrefixLevelsAreNamed|TestEntityDomainAuthority|TestAuthorityRejectionIsCodedAndIdentityFree' ./pkg/types/
  go test -race -count=1 -run 'Semantics' ./agentic/ ./graph/ ./processor/rule/
  go test -race -tags=integration -count=1 -run 'TestAuthorityGate|TestImportLane' ./processor/graph-ingest/
  go test -race -tags=integration -count=1 -run 'TestRunScopeNewMintsUnderDeploymentAuthority' ./processor/rule/
  go test -race -count=1 -run 'TestSegmentTokensResolveByName|TestAppliesToThreeSegmentsIsSourceScope' ./processor/rule/ ./processor/agentic-tools/
  go test -race -count=1 -run 'TestGraphSummaryTypeKeyFollowsCanonicalOrder' ./processor/graph-query/
  go test -race -count=1 -run 'TestGetSystemReadsNamedField|TestSummaryGroupsByNamedDomain' ./graph/clustering/
  ```

## 3. Contract — `pkg/types`

- [ ] 3.1 Reorder `EntityID` fields to `Org, Platform, System, Domain, Type, Instance`; `Key()`/`ParseEntityID` follow;
      keep `EntityType()` = `{Domain, Type}`; update the struct comment to the position table in the spec delta.
- [ ] 3.2 Replace `TypePrefix/SystemPrefix/DomainPrefix/PlatformPrefix` with the named levels `DeploymentPrefix` (2),
      `SourcePrefix` (3), `TaxonomyPrefix` (4), `TypePrefix` (5), plus `PrefixLevel(n)`; `IsSameSystem` → `IsSameSource`;
      `IsSameDomain` removed (not a prefix under the new order; `grep -rn IsSameDomain --include='*.go'` → tests only).
- [ ] 3.3 Add `EntityDomainDelegation`, `EntityDomainAuthority`, `NewEntityDomainAuthority`, `Authorize(producer,
      domain, entityType)`, and the reserved set `FrameworkEntityDomains = {agent, ops, gateddag, graph}`; mirror
      `vocabulary/namespace_authority.go` shape-for-shape (`Producer` from the trusted boundary, exact matches only).
- [ ] 3.4 Export `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`, reasons
      `EntityIDReasonForeignAuthority = "foreign_authority"`, `EntityIDReasonLocalAuthorityClaimed =
      "local_authority_claimed"`, `EntityIDReasonDomainUndelegated = "domain_undelegated"`, detail key
      `EntityIDDetailLane = "lane"`; add `ValidateEntityIDAuthority(candidate string, local PlatformMeta, importLane
      bool) error`. Details never carry identity bytes.
- [ ] 3.5 Reserve the container padding tokens: `ReservedInstanceTokens = {group, container, level}`;
      `ValidateEntityID` unchanged (lexical); the audit (5.6) and `graph/inference/hierarchy.go` consume the constant.
- [ ] 3.6 `internal/semantictest.EntityID` positional args follow the new order; `message.ParseEntityID` delegator
      unchanged (alias).

## 4. Forced omissions — one per new parser/builder/mapper (commit GREEN first; restore by `cp` + `shasum`)

Each row: copy the file aside, delete the CALL (not the error check), run the named test, record the verbatim
`--- FAIL`, restore with `cp`, and record `shasum` equality of source and backup.

- [ ] 4.1 M1 `ParseEntityID`: swap the two index assignments back to `Domain: parts[2], System: parts[3]` →
      `TestEntityIDKeyOrderIsSystemBeforeDomain` MUST fail.
- [ ] 4.2 M2 `EntityDomainAuthority.Authorize`: return nil unconditionally → `TestEntityDomainAuthorityMirrorsPredicateAuthority` MUST fail.
- [ ] 4.3 M3 graph-ingest gate: delete the `ValidateEntityIDAuthority` call on the fact lane →
      `TestAuthorityGateRejectsForeignOnFactLane` MUST fail.
- [ ] 4.4 M4 import lane: ignore the port's `import` flag → `TestImportLaneAcceptsForeignRejectsLocalClaim` MUST fail.
- [ ] 4.5 M5 `entityPartNames`: swap the two names back → `TestSegmentTokensResolveByName` MUST fail.
- [ ] 4.6 M6 `actions.go` run-scope mint: restore `idParts[0], idParts[1]` → `TestRunScopeNewMintsUnderDeploymentAuthority` MUST fail.
- [ ] 4.7 M7 audit `authority_literal` rule: skip position 1–2 literal detection → the audit fixture test in 5.6 MUST fail.

## 5. Sweep — builders, patterns, configs, docs, audit

- [ ] 5.1 Nine framework builders (`agentic/entity_ids.go`, `agent_lesson_entity.go`, `web_observation_entity.go`,
      `ops_diagnosis_entity.go`, `graph/events.go`, `processor/rule/graph_event_identity.go`, gated-dag participant,
      e2e mission, `examples/processors/iot_sensor/payload.go`) emit `org.platform.<component>.<domain>.<type>.<instance>`
      and authorize their domain; ADR-076 families drop the `semstreams.framework` literal.
- [ ] 5.2 Index-position readers (inventory W3, C3): `graph/inference/hierarchy.go`,
      `graph/clustering/entityid_provider.go:231-236` (`getSystem`, live through `NewEntityIDProvider` at
      `processor/graph-clustering/component.go:1331` — stays correct until gh606 deletes it),
      `graph/clustering/summarizer.go:719-731` (domain grouping of the summary prompt),
      `processor/graph-query/summary.go:198-202` (`EntityTypeSummary.Type` = `system.domain.type` by named field — an
      API value change named in the PR body), `graphrag.go`, `agentic/entity_ids.go:161-171`,
      `agentic/agentrun/agentrun.go:158-170` read by named field via `ParseEntityID`, never by raw index.
- [ ] 5.3 Declaration patterns and config literals (inventory W5–W6): `agentic/agentrun/agentrun.go:100`,
      `internal/builtinprojection/contracts.go:26,56`, `processor/gated-dag/participant.go:17`,
      `cmd/e2e-semstreams/mission/state.go:28`, `configs/*` three literal patterns; lesson record prefix
      (`agent_lesson_entity.go:85-93`); `entityPartNames` resolves by name; e2e literal assertions
      `test/e2e/scenarios/ops/scenario.go:604` and `:712` rewritten in the same commit so the `e2e:ops` and
      `e2e:lessons` tiers do not report a position-literal mismatch that reads as a regression
      (`test/e2e/client/nats.go:965-974` is arity-only and stays).
- [ ] 5.4 `config/config.go`: `GetPlatform()` returns `Platform.ID`; `instance_id` present in a loaded config fails
      load with guidance naming `platform.id` (`removedConfigFields` precedent); `cmd/semstreams/main.go:477-484` and
      `cmd/e2e-semstreams/main.go:628-634` drop the precedence; every `configs/*.json` drops `instance_id`.
- [ ] 5.5 Docs: `docs/concepts/*`, `docs/basics/*`, `CLAUDE.md`, `AGENTS.md`, `openspec/project.md:91`,
      `openspec/specs/structural-identity/spec.md:6-13` name the new order (29 files, inventory §1.14 list);
      `docs/concepts/18-rule-driven-artifacts.md:72,118` (whole-ID subject examples) state that a `$entity.id` subject
      carries the canonical order and that position-literal subscriptions must follow it;
      `docs/proposals/gh606-derived-communities-design.md:65-71` gains a note that level 1 is source.
- [ ] 5.6 `internal/entityidaudit`: add rules `authority_literal` (literal non-`*`, non-template value in positions
      1–2 of a production builder or declaration) and `domain_unregistered` (literal position-4 value outside the
      reserved set in production Go); classify the 30 existing arity findings at their exact occurrence; fixture test
      for each rule; `task entity-id:audit` added to the CI `Lint` job in `.github/workflows/ci.yml`.
- [ ] 5.7 `task schema:generate`; `git diff --exit-code schemas/ specs/` → commit any regenerated output.
- [ ] 5.8 Values that leave the graph (inventory P5–P6; owner item O-11): `vocabulary/export/export.go:123-126` emits
      the IRI path in the canonical order from named fields; the PR body announces the export IRI path and the
      `graphSummary` `entity_types[].type` value as published-artifact breaks that fresh state does not re-mint.

## 6. Boundary enforcement and #1096

- [ ] 6.1 graph-ingest reads `deps.Platform` at construction (`CreateGraphIngest`, `component.go:644`); the structural
      gate calls `ValidateEntityIDAuthority` for the candidate ID and every `@id` object on the fact lane, every
      `graph.mutation.>` operation, and direct persistence, before KV I/O; metered once as
      `mutation_rejections{reason="authority_foreign"|"authority_claimed"}`; loud log names lane and segment index, never
      the identity.
- [ ] 6.2 `JetStreamPort` gains `Import bool` (`"import"`); the port schema and `configs/edge-federation.json` /
      `cloud-federation.json` carry one declared import lane as the reference.
- [ ] 6.3 `processor/rule/actions.go:1575-1583`: mint from `e.platform` / `deps.Platform`; the firing entity remains
      the parent reference; delete the `SplitN` read-back.
- [ ] 6.4 `graph/inference/hierarchy.go`: containers use `ReservedInstanceTokens`; a container whose ID would exceed
      256 bytes returns the coded structural error instead of a padded overflow.

## 7. Gates and landing (AGENTS.md:63-68 order)

- [ ] 7.1 Focused gates, results recorded verbatim: `task lint`; `go test -race -count=1 ./...`;
      `scripts/run-integration-tests.sh` (what CI runs); `go test ./test/contract/...`; `task entity-id:audit`;
      `task schema:generate && git diff --exit-code schemas/ specs/`;
      `openspec validate entity-id-segment-semantics --strict --no-interactive`.
- [ ] 7.2 Covering e2e tiers on the landing branch, one at a time on the shared host, results recorded verbatim:
      `task e2e:core`; `task e2e:structural`; `task e2e:agentic`; `task e2e:lessons`; `task e2e:lifecycle`;
      `task e2e:ops`. Cold-start proof: each tier starts on newly provisioned NATS storage with readiness fail-closed
      through initial replay.
- [ ] 7.3 Implementation review by `semstreams-reviewer`; verdict and every finding's disposition recorded in
      `conformance.md`.
- [ ] 7.4 Owner-run cross-agent round where the owner asks for it; fixes and re-review recorded in `conformance.md`.
- [ ] 7.5 `openspec archive entity-id-segment-semantics` + spec sync as the final content commit; narrow reviewer
      check of the archive/spec sync recorded.
- [ ] 7.6 Undraft; PR body carries `implemented-by`, the per-sister migration list, the tag-split ruling (O-7), and the
      e2e evidence pointers. No task asserts CI state.
