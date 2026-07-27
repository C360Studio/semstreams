## ADDED Requirements

### Requirement: Thematic answer synthesis resolves the community summary from the summary store with a statistical floor

Thematic (global-search) answer synthesis SHALL resolve each community's summary text by joining
the partition record (`COMMUNITY_INDEX`) with the LLM summary store (`COMMUNITY_SUMMARIES`) on the
community's membership hash, and SHALL fall back to the community's statistical summary whenever no
`llm-enhanced` summary is present for that membership. The resolution SHALL be applied through a
single helper at every summary read site, so the tiered fallback lives in one place and a community
without an LLM summary yet degrades to a non-empty statistical answer, never an empty one.

The community cache SHALL watch BOTH the partition bucket and the summary bucket. Cache readiness
SHALL be gated on the partition bucket only: a summary miss is a graceful statistical fallback, not
an unready state, so GraphRAG availability is decoupled from the LLM summary pipeline. This
requirement composes with — and does not alter — the query-relevant, tag-enriched representative
context (which is sourced from ENTITY_STATES on `CommunitySummary.Entities`); this requirement
governs only `CommunitySummary.Summary`.

#### Scenario: An enhanced summary reaches synthesis via the join

- **GIVEN** a matched community whose membership hash has an `llm-enhanced` summary record
- **WHEN** its `CommunitySummary` is built for synthesis
- **THEN** the summary text is the stored LLM summary joined by membership hash

#### Scenario: A community with no LLM summary degrades to the statistical floor

- **GIVEN** a matched community with no `llm-enhanced` summary for its current membership
- **WHEN** its `CommunitySummary` is built for synthesis
- **THEN** the summary text is the community's statistical summary
- **AND** the synthesized answer is non-empty

#### Scenario: An empty summary store does not block GraphRAG availability

- **GIVEN** a populated partition and an empty `COMMUNITY_SUMMARIES` bucket
- **WHEN** the community cache reports readiness
- **THEN** readiness is satisfied once the partition bucket's initial sync completes
- **AND** thematic answers are served from the statistical floor
