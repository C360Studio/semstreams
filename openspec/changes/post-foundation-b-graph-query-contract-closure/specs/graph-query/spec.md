## ADDED Requirements

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
