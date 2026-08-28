# Conformance — entity-id-segment-semantics (revision 8 — slice A implementation columns filled; slice B rows marked NOT IN THIS PR; implementation-review rounds 1 and 2 applied)

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
| R2 | `platform` = minting deployment authority; source → `system`; `domain` = delegated taxonomy (ruling 2) | `pkg/types/entity_domain_authority.go` (`EntityDomainDelegation`, `NewEntityDomainAuthority`, `Authorize`, reserved set — **no exclusivity check**, see the O-5 DEVIATION row); builders compose from `deps.Platform` — `graph/events.go` `NewAlertEvent(org, platform, …)`, `processor/rule/graph_event_identity.go` + `rule.Dependencies.Platform` / `(*Processor).SetPlatform`, `cmd/e2e-semstreams/mission/command.go` (`deps.Platform`, wire authority ignored), `config.GetPlatform()` = `platform.id` (slice A); agentic builders on the merged tree (5.1). `Authorize` has no production caller and is not on the ingest path; overlap is reported offline by `composition.Validate` | same requirement; ADDED "Entity-domain authority is delegated on the predicate-namespace pattern", amended 2026-08-28 so it no longer asserts a composition rejection | `TestEntityDomainAuthorityMirrorsPredicateAuthority`, `TestEntityDomainAuthorityReservedPassesForEveryProducer`, `TestEntityDomainAuthorityPermitsSharedDomains` |
| R3 | `org.platform` enforced at graph boundaries on the candidate subject unless via an import lane with provenance (ruling 3) | `pkg/types/entity_id_authority.go` `ValidateEntityIDAuthority` (slice A: the validator); the graph-ingest gate and import lane are slice B — NOT IN THIS PR | `specs/graph-ingest/spec.md` ADDED "Every graph boundary enforces the deployment's own authority…" | `TestAuthorityGateRejectsForeignOnFactLane`, `TestAuthorityGateRejectsForeignOnMutationLane`, `TestImportLaneAcceptsForeignRejectsLocalClaim`, `TestAuthorityGateAllowsForeignReferenceObject` |
| R4 | Rule read-back is a bug (#1096); on an imported firing loop nothing is written to the import and the linkage lives on the local run (`agent.run.origin-entity-id`) | slice A plumbs `rule.Dependencies.Platform` / `Processor.SetPlatform` (`processor/rule/factory.go`, `rule_loader.go`, `config_validation.go`); the run-scope mint, anchor skip, and origin predicate are slice B — NOT IN THIS PR | `specs/graph-ingest/spec.md` ADDED "Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity" | `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`, `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`; omissions M6a–M6c |
| R5 | semsource re-slots inside the wave (ruling 5) | N/A in-tree — downstream migration guidance in the PR body and design §D | — | communicate only |
| K1 | Reordering allowed (constraint) | `pkg/types/entity_id.go` `Key()`/`ParseEntityID`; repository sweep (`scratchpad sweep.py`, 132 files; `sweep2.py`, 5 files) | `specs/entity-id-contract/spec.md` MODIFIED "Every entity ID has one canonical six-segment ASCII form" | `TestEntityIDKeyOrderIsSystemBeforeDomain` |
| K2 | Arity stays six (constraint) | `pkg/types/entity_id.go` validators unchanged (`canonicalEntityIDParts = 6`) | same (unchanged arity clause) | existing `pkg/types` arity tests |
| K3 | Second- and third-order impacts of the six-part shape made explicit (constraint) | `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md` §1 (rows S/K/X/W/C/H/L/R/M/D/F/T/P + census) | — | independent inventory pass (PASS WITH DIVERGENCES, r2 corrections) |
| D1 | Order `org.platform.system.domain.type.instance`; instance last (ADR-102 d1, d6) | `pkg/types/entity_id.go` `DeploymentPrefix`/`SourcePrefix`/`TaxonomyPrefix`/`TypePrefix`/`PrefixLevel`, `IsSameSource`; retired helpers deleted | MODIFIED canonical form; ADDED "Prefix lengths have fixed meanings and the instance position is last" | `TestPrefixLevelsAreNamed`, `TestTaxonomyAcrossSourcesIsPatternNotPrefix` |
| D2 | `platform` from the single identity field (conditional on O-2); ADR-076 d1 retired, d2 amended (d2) | `config/config.go` `GetPlatform`, `rejectRemovedPlatformFields`, `validateAuthorityPair`; `pkg/platform.Config.InstanceID` deleted; `pkg/types/framework_identity_families.go` (`MaxAuthorityPairBytes()` = 170 derived); `graph/events.go` + `processor/rule/graph_event_identity.go` compose from the family table — no `semstreams.framework` literal remains | ADDED position-meaning requirement; ADDED "The authority pair is bounded at configuration load" | `TestConfigRejectsOversizedAuthorityPair`; `NewAlertEvent`/`ruleTriggerEntityID` rows in `entity_ids_semantics_test.go` |
| D3 | Product names are provenance (d3) | `internal/entityidaudit/segment_rules.go` `authority_literal` over `go-format-prefix`, `go-dotted-constant`, declaration patterns (production Go + `configs/`) | same | `TestAuditFlagsAuthorityLiteral` |
| D4 | Domain delegation + reserved set `{agent, ops, graph}` (d4); overlap between producers PERMITTED per the 2026-08-28 ruling | `pkg/types/entity_domain_authority.go` (exclusivity removed); `composition/entity_domains.go` `entityDomainOverlaps` → `TypeEntityDomainOverlap` at warning severity, emitted by `Validate` only; `processor/gated-dag/participant.go` `*.*.gated-dag.agent.fanout.*` (re-slotted under `agent`); `segment_rules.go` `domain_unregistered` with `collectRegisteredDomains`, over a corpus that includes the projection-contract declaration surface (`audit.go` `languageForName` `entitypattern`/`Contract` arm and `projection_contracts[].entity_pattern`); delegations declared by `examples/processors/{document,iot_sensor,weather_station}/entity_domains.go` and `cmd/e2e-semstreams/mission/state.go`, retained as the audit's registered set | ADDED delegation requirement, amended for the ruling with two new scenarios | `TestAuditFlagsUnregisteredDomain`, `TestValidateReportsSharedEntityDomainAsNonBlockingFinding`, `TestBootAnalysisCannotSeeEntityDomainOverlap` |
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
| DEVIATION | **O-5 (boot-time composition rejection for a duplicate delegation) is SUPERSEDED, owner ruling 2026-08-28** — https://github.com/C360Studio/semstreams/issues/1095#issuecomment-5454766422. Raised by Codex's BLOCKING at `328b4181`: this change's own delta asserted the rejection and no composition root performed it. The owner ruled the act mis-specified, not missing: domain overlap between producers is permitted | exclusivity check removed from `pkg/types/entity_domain_authority.go`; overlap reported by `composition.Validate` as `entity_domain_overlap` at warning severity | delta amended: the composition-rejection sentence replaced by the ruled model, two scenarios added | `TestEntityDomainAuthorityPermitsSharedDomains`, `TestValidateReportsSharedEntityDomainAsNonBlockingFinding`, `TestBootAnalysisCannotSeeEntityDomainOverlap` |

## Implementation-review round (verdict CHANGES REQUESTED at `5f66ce37`; 0 BLOCKING, 3 HIGH, 7 MEDIUM, 4 NIT)

| Finding | Disposition | Evidence |
|---|---|---|
| HIGH-1 — the audit could not see the projection-contract declaration surface (`contract.Contract.EntityPattern` normalizes to `entitypattern`, matched by no arm; `projection_contracts[].entity_pattern` never extracted) | FIXED, scoped (see deviation below) | `internal/entityidaudit/audit.go` `languageForName` `entitypattern`+`contractTypeName` arm and the `projection_contracts` walk; `TestAuditSeesProjectionContractEntityPatterns`; golden row `testdata/corpus/entities.go:11`. Corpus 1289 → 1304 candidates, 0 findings; the 14 new rows include `agentic/loop_execution_entity.go:224`, `agentic/agent_lesson_entity.go:400`, `configs/rules/lessons/lesson-lifecycle-rulepack.json:39`. Round 2 widened it again to 1317 (MEDIUM-2) |
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
| NIT — `tasks.md` cited `ops/scenario.go:715` (a closing brace) | FIXED — **four** sites, not the two this row first claimed (round 2 MEDIUM-3) | all four re-anchored to `:607` and `:718-719` (`tasks.md` `:37`, `:619`, `:694`, `:708`); `:686`'s `:604`/`:712` are legitimate pre-change targets and stay |
| NIT — `graph/clustering/semantic_edge_provider_test.go:366` comment named the retired order | FIXED | `o.p.d.sys.t` → `o.p.sys.d.t`; the values were already canonical |
| NIT — `test/e2e/scenarios/research-graph/scenario.go:115` field kept the retired concept's name | FIXED | `PlatformInstance`/`platform_instance` → `PlatformID`/`platform_id`; three in-package sites, no JSON supplies this struct (`DefaultConfig` is the only producer) |

### HIGH-1 mutation transcript (RED before / GREEN after, `cp` backups, restore verified by md5)

Baseline both sides: unmutated tree passes — pre-fix `1289 structured candidates`, post-fix `1304` (and `1317` after
round 2's MEDIUM-2), zero findings in each. Each mutation was applied with a `cp` backup, an explicit `[applied]` marker printed between mutating and
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

## Re-review round 2 (`5f66ce37` → `897476cf`; verdict CHANGES REQUESTED, 0 BLOCKING, 1 HIGH, 3 MEDIUM, 1 NIT)

Round 1's HIGH-1 was independently reproduced and confirmed closed: same baselines, all three mutations
NOT KILLED → KILLED, plus three reviewer-added mutations the developer had not run — the Go × `authority_literal`
combination, a newly-visible row set to a 5-part value, and reverting `audit.go` against the new test — all three
discriminating. The 11 false positives were reproduced, so the round-1 deviation was correct.

| Finding | Disposition | Evidence |
|---|---|---|
| **HIGH — `migration…md:442-444` told an adopter a config projection contract is domain-checked.** A defect round 1 *introduced*: the sentence "Every declaration-pattern surface counts, including … a config's `projection_contracts[].entity_pattern`" was attached to the `domain_unregistered` claim. `segment_rules.go:57-59` returns before the domain rule for anything outside production Go. Round 1 measured this boundary and recorded it here, then published the opposite — the correction loop failing outward, one section above the place the same document states it correctly | FIXED | obligation 4 now reads "in production Go only" and carries an explicit paragraph: a config is extracted, judged lexically and for `authority_literal`, and **never** domain-checked, so a rulepack in the retired order passes silently. Re-proven at this head: `lesson-lifecycle-rulepack.json:39` → `*.*.agent.lesson.record.*` → `entity ID audit passed: 1317` |
| MEDIUM-2 — the `container` binding under-covered: `[]Contract{{EntityPattern: …}}` elides its element type, so `strings.Contains("", "contract")` failed. 13 real declarations missed. Round 1 deferred this citing the generic-generalization blast radius; that obstacle does not bind a `Contract`-scoped fix | FIXED | `audit.go:392-402` clones the `*ast.ArrayType` element-type idiom already at `segment_rules.go:174-185`, keyed on the new `contractTypeName` constant (`audit.go:724`) that `languageForName:737` also reads, so the two cannot drift. Corpus 1304 → 1317; **corpus diff: 0 rows removed, 14 added**, all 317 existing `go-field:.<field>` surfaces intact, so `pkg/fusion/engine_test.go:211`'s line-pinned annotation is untouched. Zero new findings |
| MEDIUM-3 — the `:715` NIT was half closed; the round-1 row claimed "two sites" when four cite the brace | FIXED, and the row above corrected | `tasks.md:694` and `:708` re-anchored to `:607`/`:718-719`. Both sit inside "Done." evidence where a current anchor is load-bearing |
| MEDIUM-4 — the eight/three provenance split had no in-tree artifact, and on the only checkable reading the grouping differs | FIXED | split dropped; the paragraph now keeps only what was verified and is all an adopter needs — all eleven SHAs resolve to real commits and each equals that sister's current `HEAD` |
| NIT-1 — `audit.go` cited `graph/query/classifier.go:80` for `Options["entity_pattern"]`; that line is the comment above an unrelated token regexp and `entity_pattern` appears in zero production Go under `graph/query/` | FIXED | the comment now cites `configs/domains/iot.json:8` and `graph/query/examples_test.go:52` |

### MEDIUM-2 mutation transcript

| Mutation | Pre-fix (`audit.go` md5 `f8c963b34c4d…`) | Post-fix (md5 `240103495e73…`) |
|---|---|---|
| `processor/rule/projection_derivation_test.go:66` (an elided `[]projection.Contract{{…}}` element) → 5-part `acme.ops.test.system.record` | `entity ID audit passed: 1304` — **NOT KILLED** | `:66: declaration-pattern go-field:Contract.EntityPattern: "acme.ops.test.system.record": entity_id_pattern_invalid:arity` — **KILLED** |
| revert `audit.go` with the extended test in place | `candidates = []string{…3 rows…}`, want 4 — **FAILS** | passes |

Restored by `cp` and md5: `audit.go` `240103495e73666a7571cd8a5d58c7ba`,
`projection_derivation_test.go` `3a8410fec4d1d50634913b4db454e514`,
`lesson-lifecycle-rulepack.json` `65a66ebf606768fc45ec2048bc0a1e46` — each equal to its pre-mutation value.

The 13 recovered declarations are all `_test.go` in this tree (`processor/rule/projection_derivation_test.go:{66,78,86,177,207,281,395}`, `config_projection_test.go:{28,97,165}`, `actions_reconcile_test.go:49`, `projection_bindings_test.go:24`, `pkg/projection/mutation_client_test.go:428`), so the recovery is lexical here; the audit's purpose is running over a sister's tree, where `processor/rule/config.go:78` `ProjectionContracts []projection.Contract` invites exactly that spelling in production. A map-keyed `Contract` literal was searched for and does not exist in this tree, so the walk stays slice-scoped rather than speculatively general.

## Codex owner round at `328b4181` (1 BLOCKING, 1 HIGH, 1 MEDIUM) — all three premises independently re-verified

| Finding | Disposition | Evidence |
|---|---|---|
| **BLOCKING — the change shipped a spec requirement it did not implement.** The delta asserted "A duplicate delegation of one domain by two producers in one composition MUST be a composition rejection before binding"; no composition root called `NewEntityDomainAuthority`. Rounds 1-2 treated the unwired constructor as a design choice when O-5 had already decided it | **RESOLVED BY OWNER RULING 2026-08-28 — the act was mis-specified, not missing.** See the DEVIATION row | exclusivity removed at `pkg/types/entity_domain_authority.go:83`; delta amended; overlap reported by `composition.Validate` |
| **HIGH — `authority_literal` skipped a literal minting builder.** `segment_rules.go` covered declaration patterns, format builders and prefix constants but not `LanguageLiteral`, so a production `EntityID{Org: "acme", Platform: "fixed-product", …}` passed | FIXED, scoped to minting surfaces | `segment_rules.go:57` adds the `LanguageLiteral` arm gated on `isMintingSurface`; `mintingSurfaces` at `:86` is the closed set `{go-constructor:EntityID, go-return:EntityID}`. A triple subject, typed reference, or state ID may legitimately name a foreign authority and stays unjudged. `TestAuditFlagsAuthorityLiteralInAMintingLiteral` pins both surfaces and that negative. Full audit after the fix: **1317 candidates, 0 findings — no false positives** (the tree already mints from `deps.Platform`; all 22 literal-valued minting candidates are `_test.go`) |
| **MEDIUM — the breaking-field sweep was incomplete and task 5.5 claimed otherwise** | FIXED, and 5.5 corrected | `config/README.md:50` (documented `instance_id`, a load-time error), `natsclient/kv.go:521` and `processor/graph-ingest/component.go:2685` (retired order in live doc comments). `git log origin/main..HEAD -- natsclient/kv.go` returned **no commits**: 5.5's "Done" list named a file this PR never touched. Sixteen of the seventeen named files were genuinely touched; that one was the single false entry. The unreproducible "132 files" figure is withdrawn rather than restated |

### The seam chosen for the overlap report, and why

`composition.Validate(catalog, cfg, delegations ...semtypes.EntityDomainDelegation)` — `validate.go:28-30`. Neither
the catalog nor the configuration carries entity-domain delegations; they are `[]EntityDomainDelegation` functions in
product packages, which only a composition root holds, so a new input was unavoidable. A **variadic on the existing
signature** adds zero new types and zero new functions, and every one of the nine existing call sites compiles
unchanged; an options type was rejected as speculative widening for a second input that does not exist. Supplying no
delegations simply omits the report.

**The finding cannot refuse a boot, structurally and not only by severity.** The boot refusal is
`service/component_manager.go:396` `analyzeBootComposition`, which runs `composition.Analyze(declarations, streams)`
— a function with no delegation parameter. The overlap finding is added in `Validate` only (`validate.go:79`), so
`Analyze` cannot observe it. `TestBootAnalysisCannotSeeEntityDomainOverlap` pins that. Belt and braces,
`severityOf` maps the type to `SeverityWarning` (`findings.go:77`).

**Grammar-collision audit on the new token.** `entity_domain_overlap` / `EntityDomainOverlap` /
`TypeEntityDomainOverlap`: **zero pre-existing hits** across `*.go`, `*.json` and `*.md`. It collides with none of
the thirteen existing `Type*` constants, none of the audit reasons (`authority_literal`, `domain_unregistered`,
`instance_reserved`), and none of the entity-ID codes (`entity_id_authority_invalid`, `domain_undelegated`). Noted
as a near-miss the audit surfaced: `component.Registration.Domain` already exists and means a **business** domain
("robotics", "semantic", "network") — an unrelated vocabulary that must not be overloaded for entity domains.

### Round-3 mutation transcripts

| Mutation | Result | Restore |
|---|---|---|
| `severityOf`: drop `TypeEntityDomainOverlap` from the warning arm | `Severity:"error"` → test fails with "an error trips the boot refusal on the intended case" — **KILLED** | `findings.go` md5 `80072982a31707b5332a1e99dc386c20` |
| `Validate`: delete the `entityDomainOverlaps` CALL (wiring, not primitive) | both overlap tests fail — **KILLED** | `validate.go` md5 `80ef8f46a8be53423e3eb0bc20102154` |
| production `OpsDiagnosisEntity.EntityID()` returns `"acme.fixed-product.diagnosis.ops.finding.d1"` | pre-fix `entity ID audit passed: 1318` — **NOT KILLED**; post-fix `:92: literal go-return:EntityID: …: authority_literal` — **KILLED** | `segment_rules.go` `d382cf4b28df75e05f7aa64165fe3036`, `ops_diagnosis_entity.go` `569dbba82fee6bbcb8da9f5371149b1e` |
| revert `segment_rules.go` with the new audit test in place | test **FAILS** — it discriminates | as above |
| restore `"instance_id": "west-1"` to `config/README.md`'s documented block | `TestREADMEPlatformExampleLoads` fails with the production loader's own removal error — **KILLED** | `README.md` md5 `e919f4215f6debf29e05592727ac04bf` |

Codex suggested a config decode test for the documented platform block; it was taken.
`config/readme_example_test.go:62` `TestREADMEPlatformExampleLoads` parses `config/README.md`'s own fenced example,
strips its `//` annotations without touching `nats://` inside strings, and loads the platform block through
`NewLoader().EnableValidation(true)`. The example can no longer drift from the removed-field guard.

### Raised, not decided — for the owner

ADR-102 decision 4 also said an **undelegated** value "is a composition rejection at boot". That is the same
shape the BLOCKING found and is equally unimplemented: `Authorize` has zero production callers, and slice A's real
enforcement is the corpus audit's `domain_unregistered` rule. The 2026-08-28 ruling covers the overlap clause only,
so decision 4 now records what ships in a marked note rather than being re-decided here.
