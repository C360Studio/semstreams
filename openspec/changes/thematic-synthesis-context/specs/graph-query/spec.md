## ADDED Requirements

### Requirement: Thematic answer-synthesis context is query-relevant and tag-enriched

The thematic (global-search) answer-synthesis context MUST include, per matched community,
representative entities selected by **query relevance** and MUST carry each representative's
classification tags, so that theme vocabulary residing on a relevant member's title or tags
can reach the synthesized answer.

When the auto-summarize branch has per-entity query-relevance scores, the representatives
offered to synthesis SHALL be the community's members ranked by those scores (highest first),
capped at a fixed bound, backfilled from the community's PageRank representative entities to
reach the cap. When query-relevance scores are absent (the text/statistical fallback path),
selection SHALL fall back entirely to the PageRank representative entities — no representative
slot is lost relative to that path. Each representative digest SHALL include up to a fixed cap
of the entity's classification tags (from the entity's already-loaded `content.classification.tag`
triples); entity descriptions and bodies (which are not triples) MUST NOT be fetched or included.
The number of representative digests contributed to the prompt SHALL be bounded independent of
community size. Both the LLM synthesis prompt and the template (LLM-absent) floor SHALL render
the same representative and tag context, so the degraded floor never omits context the LLM path
would have shown.

#### Scenario: A theme term carried only by a member's title surfaces in the answer

- **GIVEN** a global-search query whose theme vocabulary (e.g. "battery") appears in the title
  of a community member that is NOT a top-PageRank representative
- **WHEN** that member ranks highly by query relevance for the query
- **THEN** the member is selected as a representative and its title enters the synthesis prompt
- **AND** the term can appear in the synthesized answer

#### Scenario: A theme term carried only by a member's tags surfaces in the answer

- **GIVEN** a global-search query whose theme vocabulary (e.g. "evacuation") appears only in a
  member's `content.classification.tag` triples and in no title
- **WHEN** that member is selected as a representative
- **THEN** the tag is rendered on the representative's digest in the synthesis prompt
- **AND** the term can appear in the synthesized answer, without any entity body/description fetch

#### Scenario: Absent relevance scores fall back to PageRank representatives

- **GIVEN** a synthesis context built on a path with no per-entity query-relevance scores
- **WHEN** representatives are selected
- **THEN** the community's PageRank representative entities are used unchanged
- **AND** no representative slot is dropped relative to the pre-change behavior

#### Scenario: Representative count is bounded independent of community size

- **GIVEN** a matched community with an arbitrarily large membership
- **WHEN** its digest is built for synthesis
- **THEN** the number of representative digests contributed to the prompt does not exceed the
  fixed representative cap, and each digest's tag list does not exceed the fixed tag cap
