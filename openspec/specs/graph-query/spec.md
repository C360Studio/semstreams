# graph-query Specification

## Purpose

The semantic (embedding-backed) query strategy in `processor/graph-query`. Seeded
lazily by gh#463 (ADR-071): scope is the single ID-scoping responsibility on the
semantic path. Other graph-query strategies are added to this spec when touched.

## Requirements

### Requirement: The semantic path has a single, source-level ID-scoping responsibility

The semantic query strategy MUST NOT carry a post-retrieval ID filter that duplicates
the source-level `Scope` on `graph.embedding.query.search`. Where the semantic path
constrains results by entity ID, it MUST pass that constraint to the embedding search
as `Scope` (applied at the candidate source) rather than filtering the returned IDs
after the fact — so a small domain is not first out-ranked and then filtered from an
already-truncated window. Any ID-matching MUST go through the shared
`graph.MatchesAnyIDPrefix` matcher so the prefix semantics cannot drift between the
scope filter and the prefix query.

A distinct filtering axis that is genuinely not expressible as an ID prefix (e.g. the
type segment matched by the prior `filterEntityIDsByType`) MAY remain, but MUST be
documented as a separate, intentional axis layered atop scope — never a second,
silently-overlapping ID-prefix filter.

#### Scenario: an ID constraint on the semantic path is applied at the source

- **GIVEN** a semantic query that constrains results to an entity-ID prefix
- **WHEN** it runs
- **THEN** the constraint is passed to the embedding search as `Scope`
- **AND** there is no redundant post-retrieval ID-prefix filter on the returned set
