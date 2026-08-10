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

### Requirement: Optional enhanced summaries use an independent generation supervisor

`COMMUNITY_SUMMARIES` SHALL use a separate fresh-map generation supervisor with independent generation IDs. It SHALL
retry must-exist bucket open and `WatchAll` for the component lifetime using the existing recheck interval, including
after watch closure while the bucket remains present. It SHALL distinguish a closed updates channel from the initial
sentinel.

Each attempt SHALL stage updates and deletes in a fresh private map and publish only on its sentinel, including empty
enumeration. Unexpected loss SHALL unpublish that exact generation, make its map unreachable, and retry. Late old-
generation events SHALL have no effect. Component cancellation SHALL be orderly. `SummaryFor` SHALL consult and finally
validate only the current published summary generation.

When no enhanced summary generation or matching record is usable, resolution SHALL use the community's statistical
summary. Summary loss SHALL NOT gate partition readiness, return `index_not_ready`, or set query degradation. It SHALL
remain visible through bounded component logging and SHALL add no status key, metric contract, or configuration.

#### Scenario: summary watch loss falls back and rebinds

- **GIVEN** an enhanced summary generation is published and its bucket remains present
- **WHEN** that watch closes unexpectedly
- **THEN** the generation is unpublished and a new `WatchAll` is attempted
- **AND** queries use the statistical summary until a fresh generation reaches its sentinel

#### Scenario: summary deletion during the gap cannot survive

- **GIVEN** a summary existed in generation N
- **WHEN** N is lost and that summary is deleted before N+1 enumeration
- **THEN** N+1 publishes without the deleted summary
- **AND** no N map is retained, copied, served, or mutated

### Requirement: Framework-owned embedded consumers decode one success envelope and preserve representations

Every framework-owned embedded graph-query consumer SHALL invoke `graph.UnwrapQueryResponse` exactly once before
decoding its operation payload. Equivalent valid bare and enveloped successes SHALL decode to the same typed result.
An envelope-shaped inner payload SHALL not lose a second layer.

Each consumer SHALL preserve every successful representation its operation admits: full entities, entity digests,
community summaries, synthesized answer, and degradation metadata. It SHALL NOT turn a non-empty successful
representation into count-only text or invented empty success. Errors remain errors.

#### Scenario: adapter accepts both current success forms

- **GIVEN** equivalent valid bare and enveloped producer-shape fixtures
- **WHEN** an admitted adapter decodes each
- **THEN** both yield the same typed result
- **AND** an envelope-shaped inner payload loses no second layer

#### Scenario: full entities need no digest fallback

- **GIVEN** a successful search result containing full entities but no digests
- **WHEN** a framework adapter projects the result
- **THEN** the full entities remain available to its caller
- **AND** the adapter does not replace them with only a result count

### Requirement: Every successful global search reports its terminal strategy

Every successful `GlobalSearchResponse` SHALL carry a non-empty canonical `strategy`, including empty success. A
fallback SHALL report the strategy that produced the returned result, not the abandoned initial choice. Errors SHALL
remain errors rather than acquiring an invented strategy-bearing empty success.

#### Scenario: classifier choice falls through

- **GIVEN** an initial strategy cannot produce a result
- **WHEN** a lower-tier fallback succeeds
- **THEN** `strategy` names the fallback
- **AND** it is neither blank nor the abandoned choice

#### Scenario: empty success remains truthful

- **GIVEN** the terminal strategy executes successfully and finds no result
- **WHEN** graph-query returns the successful empty response
- **THEN** `strategy` names that terminal strategy
- **AND** emptiness is not represented by a blank strategy

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
