# Conformance — entity-id-segment-semantics (revision 7 — slice A implementation columns filled; slice B rows marked NOT IN THIS PR; implementation-review round applied)

Per-ruling map from the owner rulings on #1095 (2026-08-26, including the design-package ruling: O-1–O-11, O-13,
O-14 accepted; O-12 overridden to read-only mirror; hierarchy skip accepted), the design constraints, and ADR-102 decisions to
the code, spec delta, and test that carry each. Every `file:line` is to be measured at the head that holds the last
change to any `.go` file or spec delta on the branch; `tasks.md` rows cite section numbers. Fill the right-hand
columns at implementation time; a row with an empty Implementation column at review time is a deviation to record,
not a gap to hide. Owner-item numbers follow the design §F (review letters O-12/13/14/15 = O-11/12/13/14 here).
Constraint rows are labelled K1–K3 so they do not collide with the inventory's C1–C4 row ids.

| # | Ruling / decision | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| R1 | Positions have defined meanings (ruling 1) | `pkg/types/entity_id.go` `EntityID` doc table + struct order; `ParseEntityID` by named position (slice A) | `specs/entity-id-contract/spec.md` ADDED "Each entity-ID position has one defined meaning and one owner" | `agentic/entity_ids_semantics_test.go` |
| R2 | `platform` = minting deployment authority; source → `system`; `domain` = delegated taxonomy (ruling 2) | `pkg/types/entity_domain_authority.go` (`EntityDomainDelegation`, `NewEntityDomainAuthority`, `Authorize`, reserved set); builders compose from `deps.Platform` — `graph/events.go` `NewAlertEvent(org, platform, …)`, `processor/rule/graph_event_identity.go` + `rule.Dependencies.Platform` / `(*Processor).SetPlatform`, `cmd/e2e-semstreams/mission/command.go` (`deps.Platform`, wire authority ignored), `config.GetPlatform()` = `platform.id` (slice A); agentic builders on the merged tree (5.1) | same requirement; ADDED "Entity-domain authority is delegated on the predicate-namespace pattern" | `TestEntityDomainAuthorityMirrorsPredicateAuthority`, `TestEntityDomainAuthorityReservedPassesForEveryProducer` |
| R3 | `org.platform` enforced at graph boundaries on the candidate subject unless via an import lane with provenance (ruling 3) | `pkg/types/entity_id_authority.go` `ValidateEntityIDAuthority` (slice A: the validator); the graph-ingest gate and import lane are slice B — NOT IN THIS PR | `specs/graph-ingest/spec.md` ADDED "Every graph boundary enforces the deployment's own authority…" | `TestAuthorityGateRejectsForeignOnFactLane`, `TestAuthorityGateRejectsForeignOnMutationLane`, `TestImportLaneAcceptsForeignRejectsLocalClaim`, `TestAuthorityGateAllowsForeignReferenceObject` |
| R4 | Rule read-back is a bug (#1096); on an imported firing loop nothing is written to the import and the linkage lives on the local run (`agent.run.origin-entity-id`) | slice A plumbs `rule.Dependencies.Platform` / `Processor.SetPlatform` (`processor/rule/factory.go`, `rule_loader.go`, `config_validation.go`); the run-scope mint, anchor skip, and origin predicate are slice B — NOT IN THIS PR | `specs/graph-ingest/spec.md` ADDED "Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity" | `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`, `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`; omissions M6a–M6c |
| R5 | semsource re-slots inside the wave (ruling 5) | N/A in-tree — downstream migration guidance in the PR body and design §D | — | communicate only |
| K1 | Reordering allowed (constraint) | `pkg/types/entity_id.go` `Key()`/`ParseEntityID`; repository sweep (`scratchpad sweep.py`, 132 files; `sweep2.py`, 5 files) | `specs/entity-id-contract/spec.md` MODIFIED "Every entity ID has one canonical six-segment ASCII form" | `TestEntityIDKeyOrderIsSystemBeforeDomain` |
| K2 | Arity stays six (constraint) | `pkg/types/entity_id.go` validators unchanged (`canonicalEntityIDParts = 6`) | same (unchanged arity clause) | existing `pkg/types` arity tests |
| K3 | Second- and third-order impacts of the six-part shape made explicit (constraint) | `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md` §1 (rows S/K/X/W/C/H/L/R/M/D/F/T/P + census) | — | independent inventory pass (PASS WITH DIVERGENCES, r2 corrections) |
| D1 | Order `org.platform.system.domain.type.instance`; instance last (ADR-102 d1, d6) | `pkg/types/entity_id.go` `DeploymentPrefix`/`SourcePrefix`/`TaxonomyPrefix`/`TypePrefix`/`PrefixLevel`, `IsSameSource`; retired helpers deleted | MODIFIED canonical form; ADDED "Prefix lengths have fixed meanings and the instance position is last" | `TestPrefixLevelsAreNamed`, `TestTaxonomyAcrossSourcesIsPatternNotPrefix` |
| D2 | `platform` from the single identity field (conditional on O-2); ADR-076 d1 retired, d2 amended (d2) | `config/config.go` `GetPlatform`, `rejectRemovedPlatformFields`, `validateAuthorityPair`; `pkg/platform.Config.InstanceID` deleted; `pkg/types/framework_identity_families.go` (`MaxAuthorityPairBytes()` = 170 derived); `graph/events.go` + `processor/rule/graph_event_identity.go` compose from the family table — no `semstreams.framework` literal remains | ADDED position-meaning requirement; ADDED "The authority pair is bounded at configuration load" | `TestConfigRejectsOversizedAuthorityPair`; `NewAlertEvent`/`ruleTriggerEntityID` rows in `entity_ids_semantics_test.go` |
| D3 | Product names are provenance (d3) | `internal/entityidaudit/segment_rules.go` `authority_literal` over `go-format-prefix`, `go-dotted-constant`, declaration patterns (production Go + `configs/`) | same | `TestAuditFlagsAuthorityLiteral` |
| D4 | Domain delegation + reserved set `{agent, ops, graph}` (d4) | `pkg/types/entity_domain_authority.go`; `processor/gated-dag/participant.go` `*.*.gated-dag.agent.fanout.*` (re-slotted under `agent`); `segment_rules.go` `domain_unregistered` with `collectRegisteredDomains`, over a corpus that includes the projection-contract declaration surface (`audit.go` `languageForName` `entitypattern`/`Contract` arm and `projection_contracts[].entity_pattern`); delegations declared by `examples/processors/{document,iot_sensor,weather_station}/entity_domains.go` and `cmd/e2e-semstreams/mission/state.go` | ADDED delegation requirement | `TestAuditFlagsUnregisteredDomain` |
| D5 | Subject-only coded authority rejection; import lane; `@id` objects structural only (no stub, absent object permitted); imports are read-only mirrors (ruled O-12(a)) (d5) | `pkg/types/entity_id_authority.go` (codes, reasons, lane detail, identity-free details) — slice A; the boundary and mirror rules are slice B — NOT IN THIS PR | ADDED "Authority mismatch is a coded rejection distinct from structural rejection"; graph-ingest ADDED | `TestAuthorityRejectionIsCodedAndIdentityFree`, `TestAuthorityRejectionLocalClaimOnImportLane`, `TestAuthorityGateRejectsAnnotationOfImportedSubject` |
| D6 | Prefix-level meanings; ADR-099 levels 0 = source×taxonomy, 1 = source, 2 = deployment (d6) | `pkg/types/entity_id.go` `PrefixLevel*` constants and helpers; `graph/clustering/entityid_provider.go` `getSystem`/`getTypePrefix` by name; `docs/proposals/gh606-derived-communities-design.md` restated (level 1 = source, served by default) | ADDED prefix-level requirement; `specs/graph-clustering/spec.md` MODIFIED | `TestEntityIDEdgesReadPositionsByName` |
| D7 | Never rewrite; fresh-state break (d7) | no alias, ledger, or dual parser anywhere in the diff; `docs/operations/migration-beta162-to-beta163.md` slice-A section (on the merged tree) | existing "clean owned-source break" requirement (unchanged) | cold-start proof in tasks §7.2 |
| H | Hierarchy inference skips foreign-authority entities (accepted by ruling on every lane; O-6 ruled: containers retire with gh606) | slice B — NOT IN THIS PR; slice A reserves the padding tokens (`pkg/types.ReservedInstanceTokens`) | `specs/graph-ingest/spec.md` ADDED "Hierarchy inference skips foreign-authority entities" | `TestHierarchySkipsForeignAuthority` |
| A1 | Audit gains two surfaces and two segment rules and becomes a CI gate | `internal/entityidaudit/audit.go` (`go-format-prefix`, `go-dotted-constant`, config `entity.pattern` / `entity_watch_buckets.ENTITY_STATES`), `segment_rules.go` (`authority_literal`, `domain_unregistered`, `instance_reserved`); `.github/workflows/ci.yml` Lint job step "Entity-ID corpus audit" | ADDED "Segment semantics are enforced by the entity-ID corpus audit" | `TestAuditFlagsFormatPrefixAuthorityLiteral`; `task entity-id:audit` in tasks 7.1 |
| L1 | Lesson `id:` three-segment minimum means source scope | matcher unchanged (`processor/agentic-loop/lessonmatch/lessonmatch.go` is order-agnostic); meaning pinned by `lessonmatch_scope_test.go` | `specs/agentic-lessons/spec.md` MODIFIED | `TestAppliesToThreeSegmentsIsSourceScope` |
| S1 | `$entity.<name>` resolves by name; invalid IDs leave tokens unresolved | `processor/rule/entity_substitution.go` `entityPartNames` + `entityPartValues` over `semtypes.ParseEntityID` | `specs/rule-engine/spec.md` ADDED | `TestSegmentTokensResolveByName`, `TestSegmentTokensUnresolvedOnInvalidID` |
| P5 | Export IRI path follows the canonical order (O-10) | `vocabulary/export/export.go` `subjectToIRI` by named field | — (published artifact; announced in the PR body) | `TestSubjectToIRIFollowsCanonicalOrder` |
| P6 | `EntityTypeSummary.type` built from named fields in canonical order | `processor/graph-query/summary.go` `aggregateEntityTypes` via `semtypes.ParseEntityID`; `graph/query_summary_types.go` doc | — (API value; announced in the PR body) | `TestGraphSummaryTypeKeyFollowsCanonicalOrder`; `task e2e:statistical` / `e2e:semantic` |
| W9 | `iot_sensor` zone reader reads positions by name | `examples/processors/iot_sensor/processor.go` `ParseZoneEntityID` via `semtypes.ParseEntityID` (+ `entity_domains.go`) | — | `TestParseZoneEntityIDReadsNamedPositions`; omission M15 |
| R-C | LPA provider and summarizer read positions by name in slice A; tag holds until gh606 (O-7) | `graph/clustering/entityid_provider.go` `getSystem`/`getTypePrefix`; `graph/clustering/summarizer.go` `parseEntityID` | — | `TestEntityIDEdgesReadPositionsByName`, `TestSummaryGroupsByNamedDomain` |
| R-D | e2e position-literal assertions and the wire-minted mission rewritten with slice A | `cmd/e2e-semstreams/mission/command.go` mints from `deps.Platform` (config knob and wire authority removed), `configs/lifecycle-flow.json`, `test/e2e/scenarios/lifecycle/scenario.go`, `tiered_structural.go` (variables renamed `source`/`domain`), `research-graph/scenario.go`; `ops/scenario.go` on the merged tree (5.3) | — | `task e2e:ops`, `e2e:lessons`, `e2e:lifecycle`, `e2e:research-graph` results in `tasks.md` 7.2 |
| O-11 | RULED 2026-08-26: level 1 (source) served by default; summaries gate there (gh606 Q8 re-ruled) | applied in gh606, not here | — | #1095 ruling comment |
| O-12 | RULED 2026-08-26 (overridden): read-only mirror — no local lane mutates a foreign subject | slice B — NOT IN THIS PR | graph-ingest ADDED requirements (read-only-mirror clause; #1096 requirement) | `TestAuthorityGateRejectsAnnotationOfImportedSubject`, `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite` |
| O-13 | RULED 2026-08-26: imported lessons do not apply locally by default; opt-in by scope | slice B — NOT IN THIS PR | — | #1095 ruling comment |
| O-14 | RULED 2026-08-26: authority-pair bound at config load (170 bytes today) | `config/config.go` `validateAuthorityPair`; `pkg/types.MaxAuthorityPairBytes()` derived from `LongestFrameworkIdentityFamily()` | ADDED "The authority pair is bounded at configuration load" | `TestConfigRejectsOversizedAuthorityPair` |
| DEVIATION | (record any owner-signed deviation here with its comment URL) | | | |

## Implementation-review round (verdict CHANGES REQUESTED at `5f66ce37`; 0 BLOCKING, 3 HIGH, 7 MEDIUM, 4 NIT)

| Finding | Disposition | Evidence |
|---|---|---|
| HIGH-1 — the audit could not see the projection-contract declaration surface (`contract.Contract.EntityPattern` normalizes to `entitypattern`, matched by no arm; `projection_contracts[].entity_pattern` never extracted) | FIXED, scoped (see deviation below) | `internal/entityidaudit/audit.go` `languageForName` `entitypattern`+`Contract` arm and the `projection_contracts` walk; `TestAuditSeesProjectionContractEntityPatterns`; golden row `testdata/corpus/entities.go:11`. Corpus 1289 → 1304 candidates, 0 findings; the 14 new rows include `agentic/loop_execution_entity.go:224`, `agentic/agent_lesson_entity.go:400`, `configs/rules/lessons/lesson-lifecycle-rulepack.json:39` |
| HIGH-2 — `migration…md` pointed provenance at "the SHAs in design §D", which holds none | FIXED | pointer re-aimed at the eleven SHAs pinned at the top of the section; the misleading **UNPINNED** label replaced with "SHA recorded at application rather than at the reading". All 11 re-verified read-only (`git cat-file -t` = commit, dated, `rev-parse HEAD` equal) |
| HIGH-3(a) — the do-nothing path is silent and nothing said so | FIXED | new paragraph after the wire section: `ValidateEntityID` is arity/alphabet only (`pkg/types/entity_id.go:149`), and the four downstream reinterpretations are cited at `graph/inference/hierarchy.go:26-29`, `graph/clustering/entityid_provider.go:224`, `processor/graph-query/summary.go:197`, `vocabulary/export/export.go:129-130`; the audit's blind spot for fully templated builders is stated |
| HIGH-3(b) — obligation 4 instructs a composition-root call no composition root in this repo makes | FIXED by option (2) — obligation restated | Option (1) was rejected on evidence: `cmd/e2e-semstreams/main.go` composes `document` (content, sensor, maintenance, observation), `iot_sensor` (environmental, facility) and `mission` (lifecycle) — no pair collides, so a boot check placed there could never fail, and `examples/processors/weather_station` has no consumer outside its own package. The falsifiable proof already exists at `pkg/types/entity_domain_authority_test.go:72-79` on the semsource/semdragon `web` pair. Obligation 4 now separates *declared-or-reserved* (enforced by the audit) from *cross-product collision* (only `NewEntityDomainAuthority` detects it; the audit's registered set is flat — `segment_rules.go:206`), and states that skipping the call reports a collision nowhere |
| MEDIUM — `tasks.md` 5.6 asserted a live "reports 7 findings" | FIXED | restated as the pre-5.1 barrier reading |
| MEDIUM — `conformance.md` D4 named `gateddag` as reserved | FIXED | reserved is `{agent, ops, graph}` (`pkg/types/entity_domain_authority.go:17`; `entity_domain_authority_test.go:64` asserts `gateddag` is not) |
| MEDIUM — O-13 Implementation column blank | FIXED | marked "slice B — NOT IN THIS PR" like its siblings |
| MEDIUM — `tasks.md` 5 preamble stated a present-tense grep over four paths #1116 deleted | FIXED | past-tensed; the conclusion survives and now holds trivially |
| MEDIUM — `pkg/types/entity_domain_authority.go:77` `NewEntityDomainAuthority` returns a handle the documented adopter pattern does not consume | NOT CHANGED — owner call at merge | the signature is on the owner's merge list with the eight phantom exports; changing it unilaterally is out of this round's scope |
| MEDIUM — eight exported symbols with no consumer (`FrameworkEntityDomains`, `ReservedInstanceTokens`, `FrameworkIdentityFamilies`, `PrefixLevel(n)`, four `PrefixLevel*`) | NOT CHANGED — owner call at merge | keep as the ADR-099/gh606 vocabulary, or delete until a consumer exists |
| MEDIUM — `agentic/web_observation_entity.go:24` `const` became a package-level `var` | FIXED with a falsifiable pin | a compile-time array-index assertion is impossible (the value comes from a function call, not a constant); `agentic/web_observation_entity_test.go` now pins the literal 16. Mutation-proven: `framework_identity_families.go:28` `InstanceBytes: 32` → `webObservationInstanceLen = 32, want 16`. The pre-existing check compared the segment against the same var and did not fire |
| NIT — `tasks.md` cited `agentic/agent_lesson_entity.go:399` (that line is `MessageType`) | FIXED | three sites re-anchored to `:400` |
| NIT — `tasks.md` cited `ops/scenario.go:715` (a closing brace) | FIXED | two sites re-anchored to `:607` and `:718-719` |
| NIT — `graph/clustering/semantic_edge_provider_test.go:366` comment named the retired order | FIXED | `o.p.d.sys.t` → `o.p.sys.d.t`; the values were already canonical |
| NIT — `test/e2e/scenarios/research-graph/scenario.go:115` field kept the retired concept's name | FIXED | `PlatformInstance`/`platform_instance` → `PlatformID`/`platform_id`; three in-package sites, no JSON supplies this struct (`DefaultConfig` is the only producer) |

### HIGH-1 mutation transcript (RED before / GREEN after, `cp` backups, restore verified by md5)

Baseline both sides: unmutated tree passes — pre-fix `1289 structured candidates`, post-fix `1304`, zero findings in
each. Each mutation was applied with a `cp` backup, an explicit `[applied]` marker printed between mutating and
testing, and restoration confirmed by matching md5 (never `git checkout`/`restore`/`stash`).

| Mutation | Pre-fix (`audit.go` md5 `e01f724f5591…`) | Post-fix (md5 `f8c963b34c4d…`) |
|---|---|---|
| `agentic/loop_execution_entity.go:224` → `*.*.agent.agentic-loop.execution.*` | `entity ID audit passed: 1289 …` — **NOT KILLED** | `:224: declaration-pattern go-field:Contract.EntityPattern: "*.*.agent.agentic-loop.execution.*": domain_unregistered` — **KILLED** |
| `agentic/agent_lesson_entity.go:400` → `*.*.agent.lesson.record.*` | `entity ID audit passed: 1289 …` — **NOT KILLED** | `:400: … "*.*.agent.lesson.record.*": domain_unregistered` — **KILLED** |
| `configs/rules/lessons/lesson-lifecycle-rulepack.json:39` → `acme.ops.lesson.agent.record.*` | `entity ID audit passed: 1289 …` — **NOT KILLED** | `:39: declaration-pattern config:projection_contracts.entity_pattern: "acme.ops.lesson.agent.record.*": authority_literal` — **KILLED** |
| same config line → `*.*.agent.lesson.record.*` (retired order) | passes | passes — **surviving by design**, `segment_rules.go:57-59` gates the domain rule to production Go (see boundary note below) |

Restored md5s: `998e7303f3a7daec33dd731f8a9ba9d3` (loop execution), `681ae240fbf98d782d78d72c5582f268` (lesson),
`e0281f57402a1bc094d8b0996a93cda3` (rulepack), `090141635473dd55b11e51f845f6dd0c`
(`pkg/types/framework_identity_families.go`, mutated 16→32 for the web-observation pin) — each equal to its
pre-mutation value.

**No newly-visible declaration produced a finding.** The 14 rows extraction gained are the three named sites plus ten
`_test.go` contract fixtures (`payloadregistry/attributes_test.go`, `pkg/projection/{,contract/}contract_test.go`,
`pkg/projection/mutation_client_test.go`) and `test/e2e/scenarios/lessons/scenario.go:375`; all `status: valid`, and
the last two corpora are lexical-only by the segment rules' design.

### Deviation from the review text — HIGH-1 extraction is scoped, not a blanket key match

The finding asked for `entitypattern` in the generic `languageForName` arm and in the generic config-key switch. Both
were implemented that way first and **measured**: `go run ./cmd/entity-id-audit .` reported **11 findings, all false
positives**, because `entity_pattern` also spells the natural-language query classifier's option key — an entity *type*
token, not an entity-ID pattern (`graph/query/classifier.go:80`, asserted at `graph/query/examples_test.go:52`). Nine
came from `configs/domains/{iot,logistics,robotics}.json` (`"sensor"`, `"drone"`, `"shipment"`, …) and two from
`graph/query/classifier_{chain,embedding}_test.go` map literals. The shipped fix therefore binds the Go arm to the
owning type (`normalized == "entitypattern" && strings.Contains(container, "contract")` — the same idiom
`languageForName` already uses for `id` under `entitystate`/`entitymutation`) and scopes the config extraction to
`projection_contracts[]` (the same shape as the existing `entity.pattern` walk). Coverage of the finding's three named
sites is unchanged; the eleven false positives do not appear.

**Residue, stated not hidden.** An elided element literal — `[]contract.Contract{{EntityPattern: …}}` — has no type
name at its own AST node and stays outside the corpus. Every such site in this tree is a `_test.go` (lexical-only by
design); both production registrations go through `agentic.LoopExecutionContract()` / `agentic.LessonContract()`, which
are covered. Generalizing the walk to name elided element types was implemented and measured too: it renames existing
surfaces (`go-field:.EntityID` → `go-field:<Type>.EntityID`) and breaks line-pinned classification annotations
(`pkg/fusion/engine_test.go:211` failed immediately), so it is a separate change, not this round's.

**Config corpus boundary.** `segment_rules.go:57-59` gates `domain_unregistered` and `instance_reserved` to production
Go, so a config carrying a retired-order pattern is extracted and lexically validated but not segment-judged. Verified
by mutation: `configs/rules/lessons/lesson-lifecycle-rulepack.json:39` set to `*.*.agent.lesson.record.*` still passes;
the same line set to `acme.ops.lesson.agent.record.*` fires `authority_literal`. That boundary is the file's documented
design (an adopter config may legitimately name a domain no Go delegation in this tree declares), not a hole this
round closes.
