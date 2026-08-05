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

### Requirement: Batch entity reads report unreturned IDs

The batch entity read (`graph.query.batch`) SHALL report every requested ID it
does not return, with a reason from the closed set `not_found` / `error`,
while preserving partial success for the IDs it can serve. Silent omission is
prohibited: a caller SHALL always be able to partition its request into
returned, not-found, and faulted — and not-found remains a statement about a
single KV read at one instant, never an authoritative absence claim. Under
the preserved first-error contract (a non-not-found fault fails the call),
`error` entries are reserved wire vocabulary; every consumer of the batch
response (the fusion client, the research-graph adapter, the graph-query
passthrough's validator) SHALL tolerate and surface the report rather than
drop it.

#### Scenario: A not-found ID is reported, not omitted

- **GIVEN** a batch request where one ID's ENTITY_STATES read returns not-found
- **WHEN** the handler responds
- **THEN** the response carries the other entities AND lists the missing ID
  with reason not-found

#### Scenario: A faulted read does not masquerade as not-found

- **GIVEN** a batch request where one ID's read fails with a non-not-found error
- **WHEN** the handler responds
- **THEN** that ID is reported with a fault reason (or the call errors,
  per the existing first-error contract), never dropped or conflated with
  not-found

#### Scenario: Every requested ID missing is still a report, not an error

- **GIVEN** a batch request where every ID's read returns not-found
- **WHEN** the handler responds
- **THEN** the response carries an empty entity list and a missing entry per
  requested ID — a complete miss is partial success at n=0, not a failure

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

### Requirement: Exact predicate lookup and namespace enumeration have distinct semantics

Graph query MUST treat a complete canonical `domain.category.property` as an exact predicate identity.
Namespace enumeration MUST be an explicit operation over `domain` or `domain.category`; it MUST NOT be
implemented by ambiguous string-prefix matching. Query wildcard syntax MUST be validated separately from stored
predicate syntax. The wire contract MUST remain independent of the physical PREDICATE_INDEX key representation.

#### Scenario: exact lookup excludes a longer or neighboring name

- **GIVEN** entities using two distinct canonical predicates in the same namespace
- **WHEN** a caller requests one complete predicate identity
- **THEN** only memberships for that exact three-part predicate are returned

#### Scenario: namespace enumeration is explicit

- **GIVEN** several predicates under one `domain.category` namespace
- **WHEN** a caller performs namespace enumeration for that two-part namespace
- **THEN** all and only canonical predicate identities in that namespace are returned
- **AND** the two-part namespace is never accepted as a stored predicate identity

### Requirement: Query-visible memberships reflect the complete current projection

Graph-index queries MUST observe the complete current ENTITY_STATES projection after the published
readiness envelope (`GRAPH_STATUS` KV, ADR-083; formerly the removed `graph.index.query.status`
subject) reports the authoritative entity revision reached. This applies to exact predicate, predicate-list,
predicate-stats, compound-predicate, by-name, incoming, and traversal queries. Superseded and empty memberships
MUST NOT remain query-visible. The contract does not imply synchronous indexing before that watermark or freshness
of independently scheduled downstream processors such as graph-clustering.

#### Scenario: a replacement retracts the former result

- **GIVEN** an entity is discoverable through membership A
- **WHEN** its authoritative projection changes from A to B and then to empty
- **THEN** queries return only B after the first watermark
- **AND** neither A nor B returns the entity after the empty-projection watermark

#### Scenario: restart and repair preserve replacement truth

- **GIVEN** a membership replacement is interrupted by a required index-operation failure
- **WHEN** readiness is withheld and repair or restart replays the current entity state
- **THEN** the public query surface converges to the complete current projection
- **AND** it never reports a ready partial mixture of old and new memberships

### Requirement: Limited query results are deterministic

Graph-index query handlers MUST deduplicate and sort the complete candidate or result set before applying a limit
or sample. Exact, value-filtered, compound, and stats-sample results use entity ID ascending; predicate-list and
namespace-list use predicate identity ascending; INCOMING retains `(sourceID, predicate)` order; NAME retains its
documented ranking tuple with entity ID as the final tie-breaker. Value-filter hydration MUST consume sorted IDs.
Limits or samples are applied after ordering only on wire surfaces that expose them.

#### Scenario: repeated limited queries return the same entities

- **GIVEN** an unchanged index contains more matches than a request limit
- **WHEN** the same limited exact, value-filtered, compound, stats-sample, or by-name query is repeated
- **THEN** every response contains the same ordered limited result

#### Scenario: predicate listing is ordered without inventing a limit

- **GIVEN** predicate-list or namespace-list returns several current predicates
- **WHEN** the same query is repeated after shuffled replay
- **THEN** every response contains all matching predicates in predicate-identity order

#### Scenario: restart does not reshuffle a limited result

- **GIVEN** an unchanged authoritative entity set and a limited query
- **WHEN** graph-index restarts and rebuilds the selected derived buckets
- **THEN** the result identities and order match the pre-restart response

### Requirement: Predicate listing reports current materialized membership

Predicate-list and namespace-list MUST derive from the selected raw PREDICATE_INDEX representation and include only
predicates with at least one current membership. PREDICATE_CATALOG MUST NOT be consulted or recreated. Vocabulary
declaration and historical-use discovery MUST remain vocabulary-registry concerns, not graph-index listing
semantics.

#### Scenario: last member removal retracts the predicate from listings

- **GIVEN** one entity is the final current member of a predicate
- **WHEN** that entity retracts the predicate and graph-index reaches its revision
- **THEN** predicate-list and namespace-list no longer return the predicate
- **AND** the predicate may remain declared in the vocabulary registry

### Requirement: Exact entity reads carry same-entry authority revision

The admitted exact entity operation MUST return the validated canonical entity and nonzero KV revision from one
`ENTITY_STATES` entry. Remote applications consume it through GraphQL as `{entity, kvRevision}`. Embedded framework
consumers receive one operation-specific typed adapter. Raw KV, MCP, literal-colon HTTP routes, provider JSON, and the
aggregate `graph/query.Client` MUST NOT become alternate application contracts.

#### Scenario: GraphQL and embedded exact reads agree

- **GIVEN** entity A is resident at KV revision R
- **WHEN** GraphQL and the embedded adapter exact-read A without an intervening write
- **THEN** both return the same canonical entity and R
- **AND** neither substitutes logical `EntityState.Version`

### Requirement: Dereference reports unresolved object IDs without hiding source edges

Exact dereference, batch hydration, and traversal MUST preserve every valid source relationship and report unresolved
object IDs through their existing missing/unknown shapes. Missing objects MUST NOT be silently omitted, fabricated as
stubs, treated as source poison, or interpreted as permission to delete the source edge.

#### Scenario: Later birth resolves without replay

- **GIVEN** source A references absent B and dereference reports B missing
- **WHEN** a real producer later creates B
- **THEN** the next dereference resolves B
- **AND** A is not replayed or rewritten
