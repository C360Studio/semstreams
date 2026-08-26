# Conformance — entity-id-segment-semantics (skeleton)

Per-ruling map from the owner rulings on #1095 (2026-08-26) and ADR-102 decisions to the code, spec delta, and
test that carry each. Every `file:line` is to be measured at the head that holds the last change to any `.go` file
or spec delta on the branch; `tasks.md` rows cite section numbers. Fill the three right-hand columns at
implementation time; a row with an empty Implementation column at review time is a deviation to record, not a
gap to hide.

| # | Ruling / decision | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| R1 | Positions have defined meanings (ruling 1) | | `specs/entity-id-contract/spec.md` ADDED "Each entity-ID position has one defined meaning and one owner" | |
| R2 | `platform` = minting deployment authority; source → `system`; `domain` = delegated taxonomy (ruling 2) | | same requirement; ADDED "Entity-domain authority is delegated on the predicate-namespace pattern" | |
| R3 | `org.platform` enforced at graph boundaries unless via an import lane with provenance (ruling 3) | | `specs/graph-ingest/spec.md` ADDED "Every graph boundary enforces the deployment's own authority" | |
| R4 | Rule read-back is a bug (#1096) | | `specs/graph-ingest/spec.md` ADDED "Framework-minted runtime state carries the deployment's own authority" | |
| R5 | semsource re-slots inside the wave (ruling 5) | N/A in-tree — downstream migration guidance in the PR body and design §D | — | communicate only |
| C1 | Reordering allowed (constraint) | | `specs/entity-id-contract/spec.md` MODIFIED "Every entity ID has one canonical six-segment ASCII form" | |
| C2 | Arity stays six (constraint) | | same (unchanged arity clause) | |
| D1 | Order `org.platform.system.domain.type.instance`; instance last (ADR-102 d1, d6) | | MODIFIED canonical form; ADDED "Prefix lengths have fixed meanings and the instance position is last" | |
| D2 | `platform` from `platform.id` only; ADR-076 d1 retired (d2) | | ADDED position-meaning requirement (framework families clause) | |
| D3 | Product names are provenance (d3) | | same | audit rule `authority_literal` |
| D4 | Domain delegation + reserved set (d4) | | ADDED delegation requirement | |
| D5 | Coded authority rejection; import lane (d5) | | ADDED "Authority mismatch is a coded rejection distinct from structural rejection"; graph-ingest ADDED | |
| D6 | Prefix-level meanings; ADR-099 levels (d6) | | ADDED prefix-level requirement; `specs/graph-clustering/spec.md` MODIFIED | |
| D7 | Never rewrite; fresh-state break (d7) | | existing "clean owned-source break" requirement (unchanged) | cold-start proof in tasks §7 |
| A1 | Audit gains segment rules and becomes a CI gate | | ADDED "Segment semantics are enforced by the entity-ID corpus audit" | |
| L1 | Lesson `id:` three-segment minimum means source scope | | `specs/agentic-lessons/spec.md` MODIFIED | |
| S1 | `$entity.<name>` resolves by name | | `specs/rule-engine/spec.md` ADDED | |
| P5 | Export IRI path follows the canonical order (O-11) | | — (published artifact; announced in the PR body) | |
| P6 | `EntityTypeSummary.type` built from named fields in canonical order | | — (API value; announced in the PR body) | `TestGraphSummaryTypeKeyFollowsCanonicalOrder` |
| C3 | LPA provider and summarizer read positions by name in slice A; tag holds until gh606 (O-7) | | — | `TestGetSystemReadsNamedField`, `TestSummaryGroupsByNamedDomain` |
| W8 | e2e position-literal assertions rewritten with slice A | | — | `task e2e:ops`, `task e2e:lessons` results in `tasks.md` 7.2 |
| DEVIATION | (record any owner-signed deviation here with its comment URL) | | | |
