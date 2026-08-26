# Conformance — entity-id-segment-semantics (skeleton, revision 5)

Per-ruling map from the owner rulings on #1095 (2026-08-26, including the design-package ruling: O-1–O-11, O-13,
O-14 accepted; O-12 overridden to read-only mirror; hierarchy skip accepted), the design constraints, and ADR-102 decisions to
the code, spec delta, and test that carry each. Every `file:line` is to be measured at the head that holds the last
change to any `.go` file or spec delta on the branch; `tasks.md` rows cite section numbers. Fill the right-hand
columns at implementation time; a row with an empty Implementation column at review time is a deviation to record,
not a gap to hide. Owner-item numbers follow the design §F (review letters O-12/13/14/15 = O-11/12/13/14 here).
Constraint rows are labelled K1–K3 so they do not collide with the inventory's C1–C4 row ids.

| # | Ruling / decision | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| R1 | Positions have defined meanings (ruling 1) | | `specs/entity-id-contract/spec.md` ADDED "Each entity-ID position has one defined meaning and one owner" | `agentic/entity_ids_semantics_test.go` |
| R2 | `platform` = minting deployment authority; source → `system`; `domain` = delegated taxonomy (ruling 2) | | same requirement; ADDED "Entity-domain authority is delegated on the predicate-namespace pattern" | `TestEntityDomainAuthorityMirrorsPredicateAuthority`, `TestEntityDomainAuthorityReservedPassesForEveryProducer` |
| R3 | `org.platform` enforced at graph boundaries on the candidate subject unless via an import lane with provenance (ruling 3) | | `specs/graph-ingest/spec.md` ADDED "Every graph boundary enforces the deployment's own authority…" | `TestAuthorityGateRejectsForeignOnFactLane`, `TestAuthorityGateRejectsForeignOnMutationLane`, `TestImportLaneAcceptsForeignRejectsLocalClaim`, `TestAuthorityGateAllowsForeignReferenceObject` |
| R4 | Rule read-back is a bug (#1096); on an imported firing loop nothing is written to the import and the linkage lives on the local run (`agent.run.origin-entity-id`) | | `specs/graph-ingest/spec.md` ADDED "Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity" | `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`, `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`; omissions M6a–M6c |
| R5 | semsource re-slots inside the wave (ruling 5) | N/A in-tree — downstream migration guidance in the PR body and design §D | — | communicate only |
| K1 | Reordering allowed (constraint) | | `specs/entity-id-contract/spec.md` MODIFIED "Every entity ID has one canonical six-segment ASCII form" | `TestEntityIDKeyOrderIsSystemBeforeDomain` |
| K2 | Arity stays six (constraint) | | same (unchanged arity clause) | existing `pkg/types` arity tests |
| K3 | Second- and third-order impacts of the six-part shape made explicit (constraint) | `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md` §1 (rows S/K/X/W/C/H/L/R/M/D/F/T/P + census) | — | independent inventory pass (PASS WITH DIVERGENCES, r2 corrections) |
| D1 | Order `org.platform.system.domain.type.instance`; instance last (ADR-102 d1, d6) | | MODIFIED canonical form; ADDED "Prefix lengths have fixed meanings and the instance position is last" | `TestPrefixLevelsAreNamed`, `TestTaxonomyAcrossSourcesIsPatternNotPrefix` |
| D2 | `platform` from the single identity field (conditional on O-2); ADR-076 d1 retired, d2 amended (d2) | | ADDED position-meaning requirement; ADDED "The authority pair is bounded at configuration load" | `TestConfigRejectsOversizedAuthorityPair`; `NewAlertEvent`/`ruleTriggerEntityID` rows in `entity_ids_semantics_test.go` |
| D3 | Product names are provenance (d3) | | same | `TestAuditFlagsAuthorityLiteral` |
| D4 | Domain delegation + reserved set `{agent, ops, graph}` (+`gateddag` per O-9) (d4) | | ADDED delegation requirement | `TestAuditFlagsUnregisteredDomain` |
| D5 | Subject-only coded authority rejection; import lane; `@id` objects structural only (no stub, absent object permitted); imports are read-only mirrors (ruled O-12(a)) (d5) | | ADDED "Authority mismatch is a coded rejection distinct from structural rejection"; graph-ingest ADDED | `TestAuthorityRejectionIsCodedAndIdentityFree`, `TestAuthorityRejectionLocalClaimOnImportLane`, `TestAuthorityGateRejectsAnnotationOfImportedSubject` |
| D6 | Prefix-level meanings; ADR-099 levels 0 = source×taxonomy, 1 = source, 2 = deployment (d6) | | ADDED prefix-level requirement; `specs/graph-clustering/spec.md` MODIFIED | `TestEntityIDEdgesReadPositionsByName` |
| D7 | Never rewrite; fresh-state break (d7) | | existing "clean owned-source break" requirement (unchanged) | cold-start proof in tasks §7.2 |
| H | Hierarchy inference skips foreign-authority entities (accepted by ruling on every lane; O-6 ruled: containers retire with gh606) | | `specs/graph-ingest/spec.md` ADDED "Hierarchy inference skips foreign-authority entities" | `TestHierarchySkipsForeignAuthority` |
| A1 | Audit gains two surfaces and two segment rules and becomes a CI gate | | ADDED "Segment semantics are enforced by the entity-ID corpus audit" | `TestAuditFlagsFormatPrefixAuthorityLiteral`; `task entity-id:audit` in tasks 7.1 |
| L1 | Lesson `id:` three-segment minimum means source scope | | `specs/agentic-lessons/spec.md` MODIFIED | `TestAppliesToThreeSegmentsIsSourceScope` |
| S1 | `$entity.<name>` resolves by name; invalid IDs leave tokens unresolved | | `specs/rule-engine/spec.md` ADDED | `TestSegmentTokensResolveByName`, `TestSegmentTokensUnresolvedOnInvalidID` |
| P5 | Export IRI path follows the canonical order (O-10) | | — (published artifact; announced in the PR body) | `TestSubjectToIRIFollowsCanonicalOrder` |
| P6 | `EntityTypeSummary.type` built from named fields in canonical order | | — (API value; announced in the PR body) | `TestGraphSummaryTypeKeyFollowsCanonicalOrder`; `task e2e:statistical` / `e2e:semantic` |
| W9 | `iot_sensor` zone reader reads positions by name | | — | `TestParseZoneEntityIDReadsNamedPositions`; omission M15 |
| R-C | LPA provider and summarizer read positions by name in slice A; tag holds until gh606 (O-7) | | — | `TestEntityIDEdgesReadPositionsByName`, `TestSummaryGroupsByNamedDomain` |
| R-D | e2e position-literal assertions and the wire-minted mission rewritten with slice A | | — | `task e2e:ops`, `e2e:lessons`, `e2e:lifecycle`, `e2e:research-graph` results in `tasks.md` 7.2 |
| O-11 | RULED 2026-08-26: level 1 (source) served by default; summaries gate there (gh606 Q8 re-ruled) | applied in gh606, not here | — | #1095 ruling comment |
| O-12 | RULED 2026-08-26 (overridden): read-only mirror — no local lane mutates a foreign subject | | graph-ingest ADDED requirements (read-only-mirror clause; #1096 requirement) | `TestAuthorityGateRejectsAnnotationOfImportedSubject`, `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite` |
| O-13 | RULED 2026-08-26: imported lessons do not apply locally by default; opt-in by scope | | — | #1095 ruling comment |
| O-14 | RULED 2026-08-26: authority-pair bound at config load (170 bytes today) | | ADDED "The authority pair is bounded at configuration load" | `TestConfigRejectsOversizedAuthorityPair` |
| DEVIATION | (record any owner-signed deviation here with its comment URL) | | | |
