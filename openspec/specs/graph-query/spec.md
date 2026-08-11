# graph-query Specification

## Purpose

The admitted graph query capability in `processor/graph-query`: the versioned
`graph.query/v1` operation family, stable responders, generation-safe
optional-view caches, shared success decoding, bounded research projection, and
truthful query outcomes. Remote applications use admitted GraphQL operations;
embedded framework consumers use named operation-specific adapters declared
through component ports. This capability exposes neither a public subject
catalog nor a general embedded client.

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

### Requirement: Graph-query owns one admitted operation family with stable responders

Graph-query SHALL own one internal inventory containing exactly these sixteen operations:
`entity`, `entityByAlias`, `batch`, `relationships`, `pathSearch`, `hierarchyStats`, `prefix`, `spatial`, `temporal`,
`semantic`, `similar`, `globalSearch`, `summary`, `searchGraph`, `byName`, and `localSearch`. The inventory SHALL bind
operation, one-token subject suffix, request type, success type, envelope shape, GraphQL exposure, in-repo consumers,
and availability/error behavior. It is internal conformance data, not a new exported subject registry.

All sixteen responders SHALL install during every successful graph-query Start through the one resolved
`graph.query/v1` provider family. Optional backing-view availability SHALL affect the handler's classified result, not
whether the responder exists. The sixteen existing exact subjects and success payloads SHALL remain wire-stable.
`batch` and `byName` remain NATS-only. A seventeenth operation requires an explicit query-contract delta.

After removal of the unadmitted agentic wrappers, the operation inventory SHALL
record `summary` with graph-gateway as its sole in-repo consumer and
`searchGraph` with graph-gateway, research-graph-classify, and
research-graph-execute as its exact in-repo consumers. Subjects, handlers,
success shapes, availability behavior, and GraphQL fields SHALL remain unchanged.

#### Scenario: local search before the community bucket exists

- **GIVEN** graph-query started without `COMMUNITY_INDEX`
- **WHEN** `graph.query.localSearch` is requested
- **THEN** a responder returns transient `index_not_ready`
- **AND** the result is not transport no-responder

#### Scenario: a seventeenth handler cannot appear silently

- **WHEN** a handler is added outside the admitted inventory
- **THEN** operation, registration, subject, and port conformance fail
- **AND** an explicit query-contract delta is required

### Requirement: Community-backed results serve one fully enumerated watch generation

Each `COMMUNITY_INDEX` watch attempt SHALL allocate fresh private community and membership maps under a monotonically
identified generation. Pre-sentinel updates and deletes SHALL change staging only. Only the initial-enumeration
sentinel may atomically publish the fresh generation, including a valid empty generation. A watch that closes, errors,
or is cancelled before its sentinel SHALL publish nothing.

After publication, updates and deletes SHALL apply only while that generation remains current. Unexpected watch exit
SHALL make that exact generation unusable and old maps unreachable before the component retries must-exist bucket open
and `WatchAll`, even when the bucket remains present. A late generation-N update or exit SHALL NOT mutate or invalidate
N+1. Component cancellation SHALL stop the supervisor without classifying orderly shutdown as loss.

Every internal community-backed read SHALL acquire a generation ID and cache pointer and SHALL validate that same
generation immediately before returning. Failure to acquire or finally validate SHALL serve no community data.
Exactly `localSearch`, `globalSearch`, and `searchGraph` may reach community state. `localSearch` requires a usable
generation. Lower-tier global/searchGraph results MAY serve without one, but requested unavailable community
enrichment SHALL be omitted only with `degraded=true` and
`degraded_reason=community_cache_not_ready`. A community-required path that loses its generation SHALL return the
classified transient `index_not_ready`, never stale data or invented empty success.

#### Scenario: a usable watch closes while its bucket remains

- **GIVEN** generation N is usable and `COMMUNITY_INDEX` remains present
- **WHEN** its watch closes unexpectedly
- **THEN** generation N becomes unusable before another community-backed response
- **AND** the supervisor obtains a fresh `WatchAll` without waiting for bucket disappearance

#### Scenario: deletion during the watch gap stays deleted

- **GIVEN** generation N is lost
- **WHEN** a key is deleted before generation N+1 enumerates
- **THEN** published N+1 does not contain that key
- **AND** no N map was copied, retained, or used as a seed

#### Scenario: partial staging is never served

- **GIVEN** generation N+1 received entries but not its initial sentinel
- **WHEN** a query executes or the watch terminates
- **THEN** no N+1 entry is visible to a response
- **AND** the incomplete generation is discarded

#### Scenario: lower tiers remain available honestly

- **GIVEN** no usable community generation and a global or searchGraph request whose lower tier can answer
- **WHEN** the lower tier succeeds
- **THEN** its result is returned with explicit community-cache degradation
- **AND** no absent community data is represented as complete enrichment

### Requirement: Optional enhanced summaries use the shared serving view

`COMMUNITY_SUMMARIES` SHALL use one component-owned supervisor and one catalog-backed
`pkg/graphview.View[clustering.CommunitySummaryRecord]`. The supervisor SHALL be the only owner allowed to open,
publish, clear, stop, or replace the view. It SHALL retry must-exist catalog acquisition using the existing recheck
interval. Its watcher-loss hook SHALL only perform a nonblocking send to a capacity-one control channel.

After a successful Start, one mutex SHALL guard the single published view pointer. Bootstrap MAY be published because
point reads fail `ErrNotReady` until caught-up. If initial Start fails, the supervisor SHALL Stop the unpublished view
before retry. On loss it SHALL clear the exact pointer, Stop the failed view, reopen the catalog reader, and only then
construct and Start a fresh view. At most one view SHALL be published or unstopped. Component cancellation SHALL clear
and Stop the exact current view, create no replacement, and exit from every acquisition, retry, Start, or loss state.

The typed decoder SHALL parse a canonical key containing one non-negative base-10 level and one 64-character lowercase
hexadecimal SHA-256 membership hash. It SHALL decode JSON exactly once, require a canonical record hash, require exact
`key == clustering.SummaryKey(record.Level, record.MembershipHash)`, and accept only the closed
`SummaryStatusEnhanced` and `SummaryStatusFailed` status vocabulary. Enhanced with a non-empty `LLMSummary` SHALL be
servable. Failed SHALL map to absence. Malformed keys, JSON, or hashes; key-record mismatch; unknown status; and
enhanced with an empty summary SHALL be poison. Unknown JSON fields MAY be tolerated.

After watcher loss, subsequent point reads SHALL fail closed while the supervisor replaces the stopped view through a
fresh catalog reader. A successful point read SHALL NOT require a summary generation ID, request lease, or
final-response validation because the exact record is content-addressed by level and membership hash.

When an enhanced summary is absent, late, staging, empty, failed, stopped, poisoned, or not found, resolution SHALL use
the community's statistical summary. Summary availability SHALL NOT gate partition readiness, return `index_not_ready`,
or set query degradation. It SHALL remain visible through bounded component logging and SHALL add no status key,
metric contract, configuration, or infrastructure.

#### Scenario: summary watch loss falls back and rebinds

- **GIVEN** an enhanced summary is readable and its bucket remains present
- **WHEN** that watch closes unexpectedly
- **THEN** subsequent point reads fail closed and the failed view is stopped
- **AND** the catalog reader is reopened before a fresh view is constructed and started
- **AND** queries use the statistical summary until the view is caught up again

#### Scenario: summary deletion during the gap cannot survive

- **GIVEN** a summary existed before watcher loss
- **WHEN** it is deleted before the replacement replay
- **THEN** the fresh replay excludes the ghost key
- **AND** the deleted summary is not served after the view is caught up

#### Scenario: initial view Start failure is cleaned up

- **GIVEN** the catalog reader opens but the initial `WatchAll` fails
- **WHEN** `View.Start` returns its error
- **THEN** the supervisor stops that unpublished view before retry
- **AND** no ticker, watcher, view pointer, or second concurrent view remains

#### Scenario: poisoned summary does not hide the statistical floor

- **GIVEN** one summary key contains an invalid record
- **WHEN** graph-query resolves that community summary
- **THEN** the view classifies the key as poison and graph-query uses the statistical summary
- **AND** unrelated valid enhanced summaries remain readable

### Requirement: Slice E research adapters decode one success envelope

The research-classify `searchGraph` adapter and research-execute graph-query adapter SHALL pass each successful
request/reply body through `graph.UnwrapQueryResponse` exactly once at the adapter request boundary before decoding the
operation payload. This is consumer-side tolerance only; current producer payload and envelope declarations SHALL
remain unchanged.

An envelope-shaped inner payload SHALL lose no second layer. Classified and transport errors SHALL remain errors.

#### Scenario: current bare and equivalent enveloped replies agree

- **GIVEN** a current production payload represented bare and inside one valid `graph.QueryResponse`
- **WHEN** either research adapter decodes it
- **THEN** both forms produce the same typed projection
- **AND** exactly one envelope is removed

### Requirement: Full-entity search success remains non-empty through research classification

A successful `searchGraph` reply containing validated full entities and no entity digests SHALL project those entities
to the existing `research.Candidate` contract in response order and under the existing caller limit. The projection
SHALL carry only facts supported by the entity and existing Candidate fields. It SHALL NOT invent labels, relevance,
snippets, or other unavailable values.

Digest-bearing behavior SHALL remain unchanged. CandidateSet and Evidence SHALL NOT gain fields merely to retain
representations for which they have no current receiver.

#### Scenario: full entities do not become zero candidates

- **GIVEN** a successful response with non-empty full entities and no digests
- **WHEN** research classify projects candidates
- **THEN** every retained valid entity contributes one candidate
- **AND** the result is not replaced by an empty candidate set or count-only claim

### Requirement: Successful global search reports the terminal strategy

Every successful `GlobalSearchResponse`, including empty success, SHALL carry a non-empty strategy naming the handler
that produced the returned result. An entity, path, temporal, or spatial route that falls through to GraphRAG SHALL
report `graphrag`; a successful searchGraph semantic fallback SHALL report `semantic_fallback`. Errors SHALL remain
errors.

#### Scenario: classifier route falls through

- **GIVEN** the initially selected route cannot produce a result
- **WHEN** GraphRAG returns the successful result
- **THEN** strategy is `graphrag`
- **AND** it is not the abandoned route

#### Scenario: empty success names its executor

- **GIVEN** a strategy executes successfully and finds no results
- **WHEN** graph-query returns the empty success
- **THEN** strategy identifies that strategy
- **AND** it is not blank

### Requirement: Temporal and spatial composition consumes canonical result IDs

Graph-query temporal and spatial global-search strategies SHALL read entity IDs from the producers' existing `id`
field. They SHALL NOT require an `entity_id` alias or change either producer's wire response.

#### Scenario: specialized index result hydrates its entity

- **GIVEN** a valid non-empty temporal or spatial producer response using `id`
- **WHEN** the corresponding global-search strategy consumes it
- **THEN** the ID is retained for entity hydration
- **AND** the response is not interpreted as an empty match set

### Requirement: The standard query success envelope has no phantom correlation field

`graph.QueryResponse` SHALL contain only `data` and `timestamp`. `graph.UnwrapQueryResponse` SHALL use that closed key
set. Query success correlation SHALL NOT expose an unused `RequestID`; mutation request correlation is unchanged.

#### Scenario: envelope discriminator follows the real envelope

- **WHEN** the exported query success type and discriminator keys are inspected
- **THEN** neither contains `RequestID` or `request_id`
- **AND** data-plus-timestamp envelopes still unwrap exactly once

### Requirement: SearchGraph fallback accepts only its reachable semantic reply

The private searchGraph semantic fallback SHALL decode the raw graph-embedding NATS search response. It SHALL NOT
retain a `similaritySearch` GraphQL wrapper decoder or compatibility test.

#### Scenario: removed gateway spelling has no private decoder

- **WHEN** gateway and graph-query production code are searched
- **THEN** `similaritySearch` is absent
- **AND** raw semantic fallback remains covered

### Requirement: Embedded graph access uses operation-specific adapters, not a general client

SemStreams SHALL expose no provisional mixed direct-KV `graph/query.Client`. Embedded framework services SHALL use
named operation-specific typed adapters over declared request/reply ports; remote applications SHALL use admitted
GraphQL operations. Raw KV and copied free-standing subject literals SHALL NOT become fallback application APIs.

Removal SHALL include only the client cohort: exported Client, client-only configuration/defaults and constructors,
private mixed direct-KV/RPC state, cache/watch/readiness/poison behavior, query methods, client-only path/cache types,
tests, and obsolete examples. It SHALL NOT remove `graph.ExactEntityReader`, `pkg/projection.MutationClient`,
classifier/search-option code, component-local research adapters, or `pkg/fusion/fusionnats.Client`. No deprecated
symbol, forwarding wrapper, replacement general client, or compatibility period SHALL remain.

#### Scenario: provisional client is gone without deleting admitted adapters

- **WHEN** exported constructors and interfaces are inspected after cutover
- **THEN** none returns the retired mixed direct-KV/RPC client
- **AND** the exact reader, mutation client, operation-specific local adapters, classifier, and fusion adapter remain
