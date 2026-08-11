# Post-Foundation-B graph query contract-closure roadmap

**Status:** Archive candidate. Slices A through F2 are implemented and independently reviewed; G.2 and G.6 are
complete. G.7 remains pending exact final independent review, exact-SHA GitHub CI, and unchanged merge.

**Promoted from:** `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md`, SHA-256
`ff23db51ce7bf6e3d45da09a1706bf70ee548ae5e6aa2b12201ceeae64c4f343`.

**Baseline:** `967b75b6ebcb0f1b0eee9157e76c39da982aa640`.

**Accepted inventory:** `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
`c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.

**Slice D reassessment:** `docs/proposals/post-foundation-b-slice-d-optional-summary-serving-view-inventory.md`,
SHA-256 `6a28a0fe9349218baf07bf6d4d79bd89c6bc4ad483fa937e575974d30f499a6b`. The 2026-08-10 owner ruling supersedes only
the original optional-summary generation-supervisor mechanism. Its capture-time status remains in the hash-pinned
artifact; the completed planning and implementation review status is recorded here and in `tasks.md`.

**Accepted Slice E inventory:**
`docs/proposals/post-foundation-b-slice-e-embedded-decoding-result-truth-inventory.md`, SHA-256
`2033480aa58b9cb8906d4efd08e57d5e19fa71f0050c2cd98cd873c2f67bcf5e`.

**Reviewed Slice E design:**
`docs/proposals/post-foundation-b-slice-e-bounded-embedded-decoding-design.md`, SHA-256
`49e838133a2eb785f66314bf4a625b8e6f4888783f51a5d7ee6e3ea72292cc42`. The hash-pinned artifact retains its
capture-time draft status. Independent design review passed, and the owner approved its eight rulings on 2026-08-10.
The approved Slice E target below supersedes only the broader embedded-decoder, receiver-less representation,
fusion-host port, private `similaritySearch`, phantom query `RequestID`, and Slice E gate claims in the original
roadmap.

This roadmap is re-derived from the accepted inventory. It does not resume superseded GS sequencing or the rejected
`Ports() PortConfig` shape.

## Evidence and identity

SemStreams remains pragmatic, easy to comprehend, offline-first, edge-capable, component/flow/port based, eventually
consistent, and tiered so optional semantic capability cannot weaken the lower tiers
(`docs/proposals/graph-state-read-write-program.md:17-32`). It owns product-neutral graph substrate, not product-domain
semantics (`openspec/project.md:14-26`).

The bounded problem is query-contract drift, not missing authority or a missing graph substrate:

- There are 16 query operations across two registration sites. Fifteen always bind; `localSearch` binds only after
  `COMMUNITY_INDEX` appears (accepted inventory lines 161-185).
- GraphQL exposes 14 operations, excludes `batch`/`byName`, and advertises `capabilities` without a responder
  (inventory lines 187-190, 336-350).
- Graph-gateway receives ADR-060 classified errors but writes only their rendered message, discarding stable class/code
  before the GraphQL boundary (`gateway/graph-gateway/component.go:1302-1311,2008-2033`).
- `localSearch` absence is a transport no-responder outcome rather than classified query readiness
  (inventory lines 373-375).
- The optional summary watch cannot distinguish channel close from initial sentinel and cannot rebind while its bucket
  remains, leaving stale summaries reachable (`processor/graph-query/component.go:612-688`;
  `processor/graph-query/community_cache.go:191-280`).
- `graph.UnwrapQueryResponse` exists, but only gateway uses it; research and the two unadmitted agentic wrappers decode
  copied reply shapes (inventory lines 405-412, 600-606; `graph/query_contracts.go:31-116`).
- Normal global-search paths omit `Strategy`. The unadmitted agentic search wrapper also collapses full-entity-only
  results, but deleting that wrapper removes rather than standardizes its unused projection (inventory lines 412-414).
- `graph/query.Client` has zero production constructors but retains three direct buckets, watchers, caches, readiness,
  and whole-client failure state (inventory lines 154-157, 318, 565-598).
- ADR-090 and the query-pattern skill admit a remote HTTP/GraphQL-shaped operation and named operation-specific typed
  adapters for embedded callers; there is no canonical general client, raw-KV fallback, or graph MCP surface
  (`docs/adr/090-authoritative-current-state-and-materialized-views.md:42-47`,
  `.agents/skills/query-pattern/SKILL.md:15-43,64-79`).
- NATS request/reply remains the transport behind declared component ports and named typed adapters. A copied raw
  subject string outside an admitted port/adapter is not a second public application contract.
- Downstream scope is communicate-only; downstream teams own adoption and product E2E (inventory lines 666-678).

## Current adopter pressure

| Adopter | Current bill | Do-nothing outcome | Design pressure |
|---|---|---|---|
| GraphQL app | Schema, message-only errors, conditional localSearch, phantom capabilities | Must parse text; no-responder or impossible field | Preserve existing class/code; advertise only served operations; classify optional-view absence |
| Direct NATS caller | Literal subject/reply convention/no-responder | Reproduces internal transport knowledge | Keep current wire; admit framework use only through declared port/adapter |
| Embedded framework service | Consumer-local subject and decoder | Reply drift accepted silently | Named adapter plus canonical envelope decoder |
| Agentic tool extension | Two unused shared wrappers have undeclared query dependencies; local overrides cannot repair captured ports | Discovery/execution/port truth can disagree | Delete those wrappers; preserve unrelated local names |
| Aggregate-client caller | Direct KV, watcher/cache/readiness/poison | Adds unused process-lifetime state | Delete unused exported aggregate surface |
| External Go importer of `graph.QueryResponse` | May read or set the unused `RequestID` field, including in a keyed struct literal | Field selection or keyed literal fails compilation after the clean break | Remove `RequestID` use; query-success envelopes contain only data and timestamp; compiler and migration notice identify the break |
| Readiness/config author | Producer keys and consumer folding | Clustering has no key | Observe actual query cache locally unless evidence requires producer status |
| Component/port author | Versioned family, exact consumers, tool-conditioned outputs | Registry rejects undeclared/mismatched use; excluded tools claim no dependency | Add only `graph.query/v1`; no config knob, service, bucket, or stream |

The complete adopter inventory remains in the accepted inventory.

## Options

Cost describes coordination/surface cost, not calendar duration.

| Option | Scope | Cost | Benefit | Residual risk |
|---|---|---:|---|---|
| 0. Do nothing | None | None | No migration | Phantom field, no-responder, decoder drift, blank strategy, general client remain |
| 1. Measurement only | Fixtures, payload/restart measurements, issue reconciliation | Small | Improves later evidence | Known adopter failures remain |
| 2. Defect patches | Patch strategy, entity formatting, selected cache bugs | Medium | Quick visible improvements | Leaves duplicate registries, phantom field, conditional responder, copied wire knowledge, general client |
| 3. Bounded query closure | Consolidate operations; stabilize localSearch; remove phantom field; converge decoding/outcomes; retire aggregate client | Medium-large | One remote pattern and one embedded pattern over existing substrate | Breaking removals; broader clustering/storage issues remain separate |
| 4. Query closure plus readiness/capability authorities | Option 3 plus clustering status and real capabilities | Large | Operator-visible producer/capability state | Adds fifth readiness producer and a new truth owner duplicating observed cache state |
| 5. Broad graph convergence | Option 4 plus chunking, BM25 persistence, GC, hierarchy, research | Extra-large | Covers much of issue queue | Conflates separate semantic classes and recreates uncontrolled program |

## Recommendation

Adopt Option 3. Option 1 is the fallback if the owner declines the two breaking removals. Current evidence does not
justify Option 4. Option 5 is rejected as one program.

## Target state

### One internal operation inventory; no new public registry

Graph-query owns one internal handler inventory containing exactly the current 16 operations. `localSearch` enters the
same startup registration path as the other 15. The inventory is implementation/conformance data, not a new exported
subject catalog.

The 16 subjects and success payloads remain wire-stable in this program. `batch` and `byName` remain NATS-only because
no evidence supports adding GraphQL operations. A separately admitted use case is required for a seventeenth operation.

The conformance matrix pins, per operation:

- operation and subject;
- request/success type;
- current envelope shape;
- GraphQL exposure;
- in-repo typed consumers; and
- availability/error behavior.

Gateway's 14-field mapping remains hand-owned, with a conformance test against the admitted matrix.

### Query operations use one versioned port family

The operation inventory and component-port declarations share interface `graph.query/v1`.

Graph-query owns one required input:

```text
name: graph_queries
direction: input
kind: nats-request
subject: graph.query.*
interface: graph.query/v1
required: true
```

All 16 one-token suffixes derive from that resolved family; handlers do not subscribe through independent literals.
Graph-gateway retains its three family outputs. Existing `graph_queries` gains `graph.query/v1` and covers the 14
GraphQL-routed operations without creating 14 gateway ports.

Component-local embedded adapters resolve exact request subjects from their output declarations:

| Operation | Producer input | GraphQL | Other component output |
|---|---|---|---|
| entity/entityByAlias/pathSearch/hierarchyStats/prefix/spatial/semantic/similar/globalSearch/localSearch | graph_queries | graph-gateway.graph_queries | none |
| batch | graph_queries | none | research-graph-execute.graph_query_batch |
| relationships | graph_queries | graph-gateway.graph_queries | research-graph-execute.graph_query_relationships |
| temporal | graph_queries | graph-gateway.graph_queries | research-graph-execute.graph_query_temporal |
| summary | graph_queries | graph-gateway.graph_queries | none |
| searchGraph | graph_queries | graph-gateway.graph_queries | research classify/execute |
| byName | graph_queries | none | fusionnats composition only; no current production component constructor |

Exact unconditional additions are one required `searchGraph` output on research classify and four required
`batch`/`relationships`/`temporal`/`searchGraph` outputs on research execute. All use
`nats-request graph.query/v1`.

Delete the unadmitted agentic wrappers `search_graph` and `summarize_graph` rather than inventing a second discovery or
executor-dependency system. Remove both shared registrations, both `BuiltinGroupKeys`, registration functions,
implementations, shared request-error helper if otherwise unused, tests, and complete exported surfaces:
`SearchGraphExecutor`, `SearchGraphOption`, `NewSearchGraphExecutor`, `SummarizeGraphExecutor`,
`WithSearchGraphTimeout`, `SummarizeGraphOption`, `NewSummarizeGraphExecutor`, `WithSummarizeGraphTimeout`, and
`NATSQuerier`. Remove their docs and allowlist/discovery expectations. There is no local replacement, reserved name,
no-op skip value, alias, or compatibility wrapper.

The general local extension seam remains unchanged for other tool names. Downstream `SkipBuiltins` entries naming
either deleted key become invalid and fail existing closed-set validation until removed. Downstream code needing an
agent-facing graph search owns a distinct tool/component contract; this slice does not add arbitrary dependency ports,
executor metadata, or a model-facing discovery redesign.

Libraries and E2E harnesses do not implement `component.Discoverable` and synthesize no Registry ports. Slice E adds no
port or configuration requirement for `pkg/fusion/fusionnats.Client` because no current in-repo component constructs
it. Research classify and execute retain their already-admitted exact graph-query outputs.

The frozen scope contains eleven graph-query, eight graph-gateway, two research-classify, two research-execute, and
nine agentic-tools instances. None of the nine agentic allowlists admits either query-dependent tool. Raw config gains
ten rows: two classify plus eight execute and zero agentic.

| Measure | Accepted baseline | Target | Change |
|---|---:|---:|---:|
| Raw rows | 385 | 395 | +10 |
| Raw per-config exact keys | 243 | 243 | 0 |
| Raw global distinct strings | 51 | 54 | +3 |
| Effective rows | 561 | 571 | +10 |
| Effective per-config exact keys | 378 | 378 | 0 |
| Effective global distinct strings | 66 | 69 | +3 |

The four exact inputs in graph-query `DefaultConfig` are dormant: the production factory consumes only each shipped
config's one raw `graph.query.>` input. The cutover replaces that one row one-for-one in eleven configs. Eight gateway
configs already contribute `graph.query.*`, so they each lose one per-config key; the two research configs each gain
four distinct exact keys because classify and execute share `searchGraph`. Net per-config key count is unchanged.
Globally, retiring `graph.query.>` and adding four exact research subjects is a net gain of three. Target
raw-to-effective delta remains `176/135/15`. Conformance pins the provider family, retained three gateway outputs,
exact research outputs, the absence of agentic query outputs, required flags, subject containment, interface
version, and the absence of an effective adapter request without a declared output. Fusion remains outside Registry
counts unless an actual component embeds it.

### Community-backed queries use a generation supervisor

Every successful Start binds all 16 responders before optional-view acquisition. The current bucket-presence watcher is
not watch recovery. Replace it for `COMMUNITY_INDEX` with one component-owned generation supervisor:

1. Independently retry bucket open and `WatchAll` for the component lifetime, even if the bucket remains present.
2. Each watch creates a monotonically numbered generation with fresh private community/membership maps.
3. Pre-sentinel updates/deletes touch staging only; close/error/cancel before sentinel cannot publish it.
4. The sentinel atomically swaps the fully enumerated generation into the usable slot; empty enumeration is usable.
5. Post-sentinel updates/deletes apply only to that active generation.
6. Unexpected exit marks that exact generation lost, clears the usable slot, and makes old maps unreachable before
   retry. Late generation-N exit/update cannot affect N+1.
7. Stop cancels supervisor/current watch without classifying orderly cancellation as loss.

A community read leases generation ID plus cache pointer and revalidates the same generation before returning. Failure
to acquire or validate serves no community data.

`COMMUNITY_SUMMARIES` is a different read class. Its key is content-addressed by level and membership hash, so a
summary read for an old membership cannot match a community with new membership. Replace its bucket-presence watcher,
once guard, raw KV handle, shared summary map, and bespoke watcher with one component-owned, catalog-backed
`pkg/graphview.View` serving projection.

One component-owned supervisor is the only code allowed to open, publish, clear, stop, or replace the summary view. It
retries `graph.OpenCatalogReader` with the existing `RecheckInterval`, constructs exactly one
`graphview.View[clustering.CommunitySummaryRecord]`, and installs an `OnWatcherLost` hook that only performs a
nonblocking send to a capacity-one supervisor control channel. The graphview watcher goroutine never performs retry,
stop, or synchronous logging.

The supervisor calls `Start` and publishes the single view pointer under one mutex only after Start succeeds. Bootstrap
publication is safe because point reads return `ErrNotReady` until caught-up. If Start fails, the supervisor stops the
unpublished view before waiting or retrying, cleaning up the ticker Start created before `WatchAll`. On loss, it clears
that exact pointer, stops the failed view, then reopens the catalog reader and constructs/starts a fresh replacement.
There is at most one published or unstopped view. On component cancellation it clears and stops the exact current view,
creates no replacement, and exits from every acquisition, retry, Start, or loss-handling state.

The typed decoder parses one canonical `{level}.{membership_hash}` key: a canonical non-negative decimal level and a
64-character lowercase hexadecimal SHA-256 hash. It decodes one `CommunitySummaryRecord`, requires canonical record
hash and exact `key == SummaryKey(record.Level, record.MembershipHash)`, and treats status as the closed
`SummaryStatusEnhanced`/`SummaryStatusFailed` vocabulary. Enhanced plus non-empty summary is servable; failed is valid
absence. Malformed keys/JSON/hash, key-record mismatch, unknown status, and enhanced with an empty summary are poison.
Unknown JSON fields are tolerated while owned fields are verified.

After watcher loss, subsequent point reads fail closed until the fresh view catches up. A successful point read needs
no summary generation ID, request lease, or final-response validation: the exact content-addressed record remains
truthful after the read completes.

With an absent, late, staging, empty, failed, stopped, poisoned, or not-found summary, the existing resolver uses
`Community.StatisticalSummary`. Summary availability never gates partition readiness, returns `index_not_ready`, or
sets query degradation. Loss and poison remain visible through bounded component logging; no new status key, metric
contract, configuration, or infrastructure is introduced.

Exactly `localSearch`, `globalSearch`, and `searchGraph` can reach community state. Generation gating covers every
internal community access, including fallback, entity-community lookup, text search, summary enrichment, source
building, and direct clustering query fallback. `localSearch` always requires a generation. Lower-tier global/search
results may serve without one; requested unavailable community enrichment is omitted only with
`degraded=true`, `degraded_reason=community_cache_not_ready`. Community-required paths and final generation loss return
transient `index_not_ready`, never stale or invented empty success.

No clustering readiness key, config, producer list, bucket, stream, service, or retry knob is added. The supervisor
uses component lifetime and existing `RecheckInterval`.

### GraphQL advertises only served operations

Remove the unserved GraphQL `capabilities` field and routing branch in one clean cutover. Add no alias, deprecated field,
stub, or synthesized list. Removal leaves exactly 19 served root Query fields. Fourteen are graph-query-backed: entity,
entitiesByPrefix, entityByAlias, relationships, entityIdHierarchy, pathSearch, spatialSearch, temporalSearch,
semanticSearch, findSimilar, localSearch, globalSearch, graphSummary, and searchGraph. Five unrelated fields remain:
trajectory plus four predicate reads. Conformance asserts the exact 14-field subset, exact 19-field total, and a served
route/response fixture for each. Unconditional `localSearch` qualifies with a classified transient.

Implementing capabilities is outside this program because no responder/owner exists, Registry snapshots are internal
declaration facts rather than deployment capability truth, and synthesis would add another interpreter across
declaration, deployment, and runtime state.

The cutover also closes one same-class collision exposed while preparing implementation: introspection advertises
`semanticSearch`, but the current response projection and in-repo E2E search executor use the hidden
`similaritySearch` spelling. `semanticSearch` is the sole target root field and response key. The gateway, owned E2E
executor, fixtures, and adopter documentation migrate atomically. `similaritySearch` is removed from the gateway
surface with no alias. The private `processor/graph-query` compatibility decoder is adjudicated later with the
embedded-consumer decoding slice and is not evidence for a second GraphQL field.

### GraphQL preserves existing classified-error authority

Graph-gateway projects an existing `*errs.ClassifiedError` into one standard GraphQL error object:

- `message` is `err.Error()`;
- `extensions.class` is `ce.Class.String()`; and
- `extensions.code` is `ce.Code` only when the code is non-empty.

The gateway copies these fields. It does not classify errors, parse message text, infer from HTTP status/subject/field,
or translate codes. Plain errors retain message-only objects; an uncoded classified error exposes class but no code.
`ClassifiedError.Detail` is not exposed by this bounded change.

Existing HTTP behavior is unchanged: gateway-local invalid input retains its current 400-class status; handler-side
classified failures retain GraphQL HTTP 200; transport timeout/unavailability retains its current gateway status.
`writeGraphQLError` receives the error value rather than only rendered text so `errors.As` can observe existing machine
authority.

### Bounded embedded decoding and truthful query outcomes

Slice E owns only the research classify adapter, research execute adapter, `fusionnats.Client`, and the graph-query
outcome defects proven by the accepted Slice E inventory. It does not normalize every reply interpreter in the
repository.

Each of the three in-slice request/reply adapter boundaries removes at most one recognized `graph.QueryResponse`
envelope through `graph.UnwrapQueryResponse` before decoding the operation payload. Current producer envelope
declarations remain unchanged. Bare and equivalent standard-enveloped fixtures prove consumer tolerance; they do not
authorize producers to switch formats. `fusionnats.Status` remains a `GRAPH_STATUS` KV read and is outside this rule.

Research classify retains its existing `CandidateSet` contract. Digest-bearing responses retain current behavior.
When a successful `searchGraph` reply contains full entities but no digests, the adapter validates the entities and
projects them, in response order and under the existing limit, into candidates. It carries only facts supported by the
entity and existing Candidate fields. Research execute retains its current Evidence projection. This slice does not
widen `Candidate`, `CandidateSet`, `Evidence`, `fusion.Entity`, or `RetrievalClient` to receive facts they do not
currently model.

Graph-query decodes temporal and spatial result IDs using the producers' canonical `id` spelling. Every successful
global-search response reports the terminal strategy that returned it, including empty success and fallthrough. An
entity, path, temporal, or spatial route that falls through reports `graphrag`; successful searchGraph semantic
fallback reports `semantic_fallback`. No new strategy type or exported enum is introduced.

The unsupported private GraphQL `similaritySearch` fallback shape is deleted; `searchGraph` fallback accepts only the
actual raw graph-embedding response. The unused `QueryResponse.RequestID` field and discriminator key are deleted.
Mutation correlation remains unchanged. No alias, compatibility path, producer envelope change, or new public client
is added.

The existing research-graph E2E seeds one canonical authority entity through the existing graph-mutation client and
installs a test-owned responder on the existing `graph.embedding.query.search` operation. The responder returns that
entity ID above the production relevance threshold. Production graph-query must authoritative-load and return the full
entity, and the production classify result stamped on the loop entity must report
`research.classify.candidate-count` greater than zero. This fixture adds no shipped component, port, configuration,
storage, retry, or readiness behavior and does not claim to validate embedding quality. Focused adapter fixtures retain
exhaustive representation coverage.

The existing `test-http-gateway` stage provides the live strategy proof through its controlled
`globalSearch("robot warehouse", level:0)` request. It selects and decodes `strategy`, requires exactly `graphrag`, and
hard-fails every earlier marshal, request construction, transport, non-200 status, body-read, JSON-decode, and
GraphQL-error branch so the assertion cannot false-green. This intentionally turns the existing gateway smoke probe
into a contract gate in both variants where it already runs. It adds no stage, tier, hit-count minimum, answer-quality,
latency, semantic-model, or other unrelated requirement. Focused graph-query table tests prove all other direct,
empty, and fallthrough strategy branches; the separate `executeTestGraphRAGGlobal` path is not the proof seam.

### Retire only the provisional mixed direct-KV aggregate client

Delete only the provisional `graph/query.Client` cohort: exported Client, client-only Config/defaults, constructors,
private mixed direct-KV/RPC implementation, cache/watchers/readiness/poison, query methods, client-only path/cache types,
and obsolete examples. Retain classifier/search-option code.

Preserve `graph.ExactEntityReader`, `pkg/projection.MutationClient`, component-local research adapters, unrelated
agentic tools, and `pkg/fusion/fusionnats.Client`. There is no deprecated symbol, wrapper, replacement general client,
or compatibility period.

`fusionnats.Client` remains the admitted NATS implementation of `fusion.RetrievalClient`, with existing
`New(requester, timeout)`, optional Close, lazy GRAPH_STATUS graph-index readiness, downstream role, and six operations:
byName, prefix, semantic, entity, batch, relationships. Every request/reply success passes through
`graph.UnwrapQueryResponse` once; Status remains KV state. Entity decodes `graph.ExactEntity`, validates a matching
valid entity and nonzero revision, then projects only ID and triples into the existing `fusion.Entity`. Producer-shape
fixtures preserve rank/order, similarity presence, batch missing reasons/order, relationship direction, and raw
readiness. The old entity fixture expecting bare EntityState becomes production `graph.ExactEntity`. The library claims
no component ports, and Slice E invents no component or configuration owner for it.

### Correct touched current specs

Correct stale predicate hash/catalog reasoning in graph-index spec to the accepted raw-key truth. This is documentation
hygiene only and does not reopen index representation.

## Draft OpenSpec deltas

### Update `graph-query` purpose and admitted-operation contract

Update the capability Purpose so it owns the admitted operation family, `graph.query/v1` port contract, stable
responders, generation-safe optional-view cache, success-envelope decoding, and preservation of successful result
representations. It does not own a public subject catalog or a general embedded client.

Graph-query SHALL install all sixteen admitted responders during every successful Start. Optional backing-view
availability SHALL affect the handler's classified result, not responder existence. The component SHALL register those
operations from one internal inventory and SHALL expose one required `graph_queries` input for `graph.query/v1`.

#### Scenario: local search before community bucket exists

- **GIVEN** graph-query started without `COMMUNITY_INDEX`
- **WHEN** `graph.query.localSearch` is requested
- **THEN** a responder returns transient `index_not_ready`
- **AND** the result is not transport no-responder

#### Scenario: a seventeenth handler cannot appear silently

- **WHEN** a handler is added outside the inventory
- **THEN** operation, registration, and port conformance fail
- **AND** an explicit query-contract delta is required

### Add `graph-query` generation-safe community cache requirements

Each `COMMUNITY_INDEX` watch generation SHALL enumerate into fresh private maps. Only its initial-enumeration sentinel
may publish that generation. Unexpected watch termination SHALL atomically make the published generation unusable and
start a fresh open/watch attempt even when the bucket still exists. Every community-backed result SHALL validate that
the same usable generation remains current immediately before return.

#### Scenario: usable watch closes while bucket remains

- **GIVEN** generation N is usable and the bucket remains present
- **WHEN** its watch closes unexpectedly
- **THEN** generation N becomes unusable before another community-backed response
- **AND** the supervisor obtains a new `WatchAll` without waiting for bucket disappearance

#### Scenario: deletion during the watch gap stays deleted

- **GIVEN** generation N is lost
- **WHEN** a key is deleted before generation N+1 enumerates
- **THEN** the published N+1 maps do not contain that key
- **AND** no N map is copied or retained as a seed

#### Scenario: partial staging is never served

- **GIVEN** generation N+1 has received entries but not its enumeration sentinel
- **WHEN** a query executes or the watch terminates
- **THEN** no N+1 entry is visible to a response
- **AND** the incomplete generation is discarded

The optional `COMMUNITY_SUMMARIES` reader SHALL use one component-owned supervisor and one catalog-backed
`pkg/graphview.View[clustering.CommunitySummaryRecord]`. The supervisor SHALL receive loss through a nonblocking,
capacity-one control channel, clear and stop the exact failed view, reopen the catalog reader, and only then construct
and Start a fresh view. A failed initial Start SHALL be stopped before retry. One mutex SHALL guard the single published
pointer; at most one view may be published or unstopped. Cancellation SHALL clear and stop the exact view without
replacement.

The typed decoder SHALL require canonical key/hash, exact key-record identity, and the closed enhanced/failed status
vocabulary. Enhanced plus non-empty SHALL be servable; failed SHALL map to absence. Malformed, mismatched, unknown, or
empty-enhanced records SHALL be poison. Missing or unavailable enhanced summaries SHALL use the admitted statistical
fallback without changing partition readiness or query degradation.

#### Scenario: summary watch loss falls back and rebinds

- **GIVEN** an enhanced summary is readable and its bucket remains
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

- **GIVEN** the catalog reader opens but the view's initial `WatchAll` fails
- **WHEN** `View.Start` returns its error
- **THEN** the supervisor stops that unpublished view before retry
- **AND** no ticker, watcher, view pointer, or second concurrent view remains

### Add bounded `graph-query` success decoding

The research-classify `searchGraph` adapter and research-execute graph-query adapter SHALL remove at most one
recognized `graph.QueryResponse` envelope through `graph.UnwrapQueryResponse` before operation decoding. Current
producer envelope declarations remain unchanged. An envelope-shaped inner payload SHALL lose no second layer.

When digests are absent and validated full entities are present, research classify SHALL project those entities into
the existing `research.Candidate` contract in response order and under the caller limit. It SHALL invent no unavailable
label, relevance, or snippet. Digest behavior and the narrow CandidateSet/Evidence contracts remain unchanged.

#### Scenario: full entities do not become zero candidates

- **GIVEN** a successful response with non-empty full entities and no digests
- **WHEN** research classify projects candidates
- **THEN** every retained valid entity contributes one candidate
- **AND** the result is not replaced by an empty candidate set or count-only claim

### Add `graph-query` terminal-strategy requirement

Every successful global-search result SHALL carry the non-empty canonical terminal strategy, including empty success.
Fallback SHALL report the strategy that produced the returned result, not the abandoned initial choice. Temporal and
spatial composition SHALL read the producers' existing `id` field.

#### Scenario: classifier choice falls through

- **GIVEN** an initial strategy cannot produce a result
- **WHEN** a lower-tier fallback succeeds
- **THEN** `strategy` names the fallback
- **AND** it is neither blank nor the abandoned choice

### Remove stale private query compatibility surfaces

The standard `graph.QueryResponse` success envelope SHALL contain only `data` and `timestamp`; the unused exported
`RequestID` and its discriminator key are removed without compatibility. The private searchGraph fallback SHALL decode
only the reachable raw graph-embedding reply; the stale `similaritySearch` GraphQL wrapper decoder is removed.

### Add `fusion` adapter preservation

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`, preserving its
constructor, optional Close, lazy graph-index readiness, and six-method surface. Its six request subjects remain a
separate transport mapping. It SHALL decode each request/reply success through `graph.UnwrapQueryResponse` once; Status
remains KV state. Entity SHALL validate `graph.ExactEntity`, including matching ID and nonzero revision, then project
only ID and triples into the existing `fusion.Entity`. The library owns no component ports, and Slice E creates no
component or configuration owner.

#### Scenario: fusion entity uses the producer representation

- **GIVEN** the graph-query entity producer returns a bare or enveloped `graph.ExactEntity`
- **WHEN** `fusionnats.Client` reads it
- **THEN** the adapter validates the exact entity and revision
- **AND** projects only its ID and triples into the existing fusion entity
- **AND** no fixture relies on the obsolete bare `EntityState` shape

### Seed `gateway-query-routing`

Create a routing capability distinct from `gateway-response-projection`. It SHALL own the root Query field inventory
and the requirement that every advertised field has a production route. The root exposes exactly nineteen served
fields: the fourteen graph-query-backed fields named in this roadmap and five unrelated served fields. `capabilities`
SHALL be absent until a separately owned capability contract is admitted.

#### Scenario: introspection has no phantom field

- **WHEN** a caller introspects the root Query schema
- **THEN** the exact graph-query-backed subset has fourteen fields and the total has nineteen
- **AND** `capabilities` is absent and every remaining field has a route/response fixture

### Seed `gateway-error-projection`

Create an error-projection capability separate from routing and from success-envelope projection. It consumes ADR-060
authority and does not create classifications, codes, retry policy, or HTTP mappings.

When a query failure resolves through `errors.As` to `*errs.ClassifiedError`, graph-gateway SHALL return the clean
message and copy the existing class and non-empty code into GraphQL `extensions`. It MUST NOT infer either field from
message text, HTTP status, query subject, or GraphQL field. A plain error has no invented class/code; an uncoded
classified error exposes only its class.

#### Scenario: invalid input preserves its existing code

- **GIVEN** prefix validation returns class `invalid` and code `entity_id_prefix_invalid`
- **WHEN** graph-gateway returns the error
- **THEN** `extensions.class` is `invalid` and `extensions.code` is `entity_id_prefix_invalid`
- **AND** the existing gateway-local HTTP status is unchanged

#### Scenario: index-not-ready remains machine readable

- **GIVEN** `RequestClassified` returns class `transient` and code `index_not_ready`
- **WHEN** graph-gateway returns the handler-error envelope
- **THEN** HTTP status remains 200
- **AND** `extensions.class` is `transient` and `extensions.code` is `index_not_ready`

#### Scenario: the gateway invents no authority

- **GIVEN** a plain error, or a classified error with no code
- **WHEN** graph-gateway projects it
- **THEN** a plain error has no class/code extensions
- **AND** an uncoded classified error exposes class only

### Modify `graph-query` embedded-client boundary

SemStreams SHALL expose no provisional mixed direct-KV `graph/query.Client`. Embedded services SHALL use named
operation-specific typed adapters over declared request/reply ports; remote apps SHALL use admitted GraphQL operations.
This removal SHALL NOT include `graph.ExactEntityReader`, `pkg/projection.MutationClient`, component-local adapters, or
`pkg/fusion/fusionnats.Client`.

#### Scenario: provisional client is gone without deleting admitted adapters

- **WHEN** exported constructors and interfaces are inspected
- **THEN** none returns the retired mixed direct-KV/RPC client
- **AND** the exact reader, mutation client, local adapters, and fusion adapter remain

### Modify `agentic-tools` admitted builtin set

The framework SHALL NOT register, advertise, execute, or export `search_graph` or `summarize_graph`. Their shared
registrations, `BuiltinGroupKeys`, implementations, exported types/options/constructors, tests, and documentation SHALL
be absent. The deleted skip keys SHALL NOT remain as accepted no-ops. GraphQL `searchGraph`/`graphSummary` and their
graph-query operations remain unchanged.

#### Scenario: deleted wrappers cannot drift from query ports

- **WHEN** shared and component-local tool discovery, builtin keys, and exported symbols are inspected
- **THEN** neither deleted tool name or implementation is present
- **AND** agentic-tools claims no `graph.query.searchGraph` or `graph.query.summary` output

#### Scenario: stale skip configuration fails visibly

- **GIVEN** downstream `SkipBuiltins` contains either deleted key
- **WHEN** builtin configuration is validated
- **THEN** existing closed-set validation rejects it
- **AND** the framework does not silently accept a compatibility no-op

### Correct `graph-index` predicate representation text

Replace the stale hash/catalog description with the current contract: `PREDICATE_INDEX` uses the raw canonical
predicate as its nine-token key and there is no `PREDICATE_CATALOG`. `INCOMING_INDEX` retains reversible hex only for
its own storage layout. Malformed predicates or entity identifiers are skipped visibly. Remove paragraphs claiming the
predicate axis must retain hex or that `PREDICATE_INDEX` is `hash(predicate)` plus a catalog. This delta documents
current code only; it does not authorize an index migration.

## Foundation boundary

| Inventory item | Classification here | Treatment |
|---|---|---|
| #822/#785/#819/#823 | Query usability/correctness | Included |
| #609 | Separate remainder | Exact boundary below |
| #820 readiness | Non-goal | No `GRAPH_STATUS` producer/key or generic readiness change |
| #784/#315 | Remote-surface truth | Remove phantom field |
| #421/#422/#571 | Complexity deletion | Remove aggregate client |
| #828 | Touched-spec defect | Documentation-only correction |
| #527 alias cleanup | Owner-local index defect | Separate change |
| #618 anomaly fail-open | Owner-local clustering defect | Separate change |
| #589 anomaly Watch | Dead exported surface | Separate deletion |
| #746 first-wins companion | Mutation semantic defect | Separate owner design |
| #855 destructive pruning | Clustering correctness defect | Separate high-priority fix; no completeness claim here |
| #839 payload ceilings | Capacity-contract candidate | Measure distributions before design |
| #619 BM25 restart order | Evidence-requiring candidate | Measure restart known answers; no persistence here |
| #633/#710 GC | Retention candidate | Separate measurement-gated owner-specific reachability/bounds design |
| #608/#829 content summaries | Producer/content quality | Separate from the consumer serving view |
| #606/#672/#436/#751 | Hierarchy/clustering model/cache | Separate remap |
| #391/#376/#347 | Research verification/enhancement | Separate |
| #810/#842 | Agentic tool collision | Outside graph query |
| #868 | Generic readiness | No generic change without three proven owners |
| #875 | Storage reference defect | Separate storage contract |

For #609 exactly: Slice C addressed the `COMMUNITY_INDEX` consumer subset. The remaining producer cold-start
first-ticker delay is separate, and Slice D does not close it.

## Decision-skill outcomes

- `query-pattern`: remote callers use admitted GraphQL-shaped operations; embedded services use named typed adapters;
  NATS request/reply remains the declared port transport; no general client, MCP, raw KV, or unowned subject fallback.
- `orchestration-check`: the partition supervisor and optional-summary serving view are private execution mechanics
  owned by the graph-query component lifecycle. They add no rule, workflow, lifecycle entity, or operator-visible
  phase state.
- `new-payload`: not applicable; no registry payload type is added.
- `kv-or-stream`: no new communication path in Option 3. Cache availability is observed through the existing KV watch.
  Option 4 would be a KV current-state fact, but would also require explicit repair/degradation obligations.

## Migration and adopter impact

| Adopter | Required action | If they do nothing | Discovery |
|---|---|---|---|
| GraphQL `capabilities` or `similaritySearch` caller | Remove `capabilities`; replace `similaritySearch` with exact `semanticSearch` | GraphQL validation fails; no alias exists | Introspection/error/migration notice |
| GraphQL `localSearch` caller | Treat classified `index_not_ready` as retryable eventual availability | Typed transient replaces transport no-responder until usable | Error extensions/migration notice |
| Aggregate `graph/query.Client` importer | Replace with GraphQL or a named operation-specific adapter | Compilation fails; no shim exists | Compiler/migration notice |
| External Go importer using `graph.QueryResponse.RequestID` | Remove field selection/keyed literal; query success is `Data` plus `Timestamp` | Compilation fails; no compatibility field exists | Compiler/query-success spec/migration notice |
| Importer of deleted agentic wrapper symbols | Remove executor/option/constructor/querier use or own a distinct local tool | Compilation fails; framework surface is absent | Compiler/migration notice |
| `SkipBuiltins` caller naming a deleted wrapper key | Remove the key | Existing closed-set boot validation fails | Boot error/migration notice |
| Config author retaining former shared names in allow/default/approval/retry fields | Remove stale framework references unless an application-local executor owns the name | Default resolution may warn/drop; approval may pause before registry miss; policy creates no executor | Warning, approval pause, typed not-found, migration notice |
| Application intentionally reusing a former name locally | Keep the local executor and matching open-vocabulary policy | Existing local admission/discovery/approval/retry/dispatch applies | Local registration/discovery |
| Category-API consumer querying `graph_search`/`graph_summary` | Accept unknown-name `CategoryCore` or explicitly categorize a local tool | Silent fallback changes from stale `CategoryKnowledge` entry to `CategoryCore` | Go behavior/migration notice |
| Component/port author | Declare `graph.query/v1` and required named outputs | Missing/stale declarations fail Registry validation | Registry/schema/migration notice |
| Direct external NATS caller | No wire change; copied literals gain no public catalog promise | Existing subjects continue without a new API guarantee | Migration notice |

Migration notice draft:

> Query contract closure removes GraphQL `capabilities`, replaces hidden `similaritySearch` with exact
> `semanticSearch`, deletes the aggregate `graph/query.Client`, deletes query-success `RequestID`, and removes the
> framework-owned `search_graph`/`summarize_graph` wrappers and skip keys without aliases or shims. `localSearch`
> remains a stable responder and returns retryable `index_not_ready` while its optional view is unusable. Component
> authors must declare `graph.query/v1` and the required named outputs. Former wrapper names in open-vocabulary
> allow/default/approval/retry fields create no executor; intentional application-local reuse remains supported.
> Removed `graph_search`/`graph_summary` category aliases now use the existing unknown-name `CategoryCore` fallback.
> The canonical, complete notice is `docs/operations/migration-post-foundation-b-graph-query-contract-closure.md`.
> Downstream teams own compilation, migration, flow validation, and product E2E; this program performs no audit.

## Verification

### Failing-first contract evidence

- Assert sixteen operations from one internal inventory and the `graph.query/v1` provider family.
- Start without `COMMUNITY_INDEX`: reproduce no-responder, then require transient `index_not_ready` from the stable
  responder; create the bucket, synchronize on the watch sentinel, and prove service without restart.
- Close a usable community watch while its bucket remains. Prove immediate generation loss and a new `WatchAll`.
- Close a staging watch before its sentinel. Prove the partial generation is never published.
- Delete a key during the generation gap. Prove the replacement enumeration excludes it and did not seed from old maps.
- Deliver a late generation-N update/exit after N+1 publication. Prove it cannot affect or invalidate N+1.
- Exercise `localSearch`, `globalSearch`, and `searchGraph` through absent, staging, usable, lost, and replaced community
  generations. Prove lower-tier results remain available with explicit degradation when community enrichment is absent.
- Close a live summary view while its bucket remains. Prove the nonblocking loss signal, synchronized pointer clear,
  failed-view Stop, statistical fallback without `index_not_ready` or degradation, catalog reopen, and fresh Start.
- Exercise typed decode for canonical enhanced/failed records and poison for malformed key/JSON/hash, key-record
  mismatch, unknown status, and empty enhanced summary.
- Exercise summary replay staging, empty caught-up, update/delete/purge, deletion during the gap, fresh replay without
  ghosts, initial-Start cleanup, one-view ownership, and orderly cancellation without sleeps.
- Load all 21 shipped configs through production factories and Registry validation. Assert eleven query, eight gateway,
  two classify, two execute, and nine agentic-tools instances with their exact allowlists.
- Assert zero shipped effective agentic summary/searchGraph outputs, two required classify outputs, eight required
  execute outputs, target raw census `395/243/54`, target effective census `571/378/69`, and unchanged delta
  `176/135/15`.
- Assert shared/local discovery, `RegisterBuiltins`, `BuiltinGroupKeys`, `SkipBuiltins` validation, allowlist/default-
  tool fixtures, docs, and schemas contain neither deleted wrapper.
- Add compile/AST contract evidence that the complete deleted symbol set and registration functions are absent, with
  no alias, forwarding wrapper, no-op skip key, or replacement agentic query port.
- Prove non-reserved local executor registration/discovery/dispatch remains unchanged.
- Assert the exact sixteen-operation producer inventory and reject unknown/out-of-family declarations.
- Assert the exact fourteen graph-query-backed GraphQL fields, exact nineteen total root fields, absence of
  `capabilities`, and a real route/response fixture for every field.
- Extend the invalid-prefix GraphQL fixture to require `extensions.class=invalid` and
  `extensions.code=entity_id_prefix_invalid`; add a classified `index_not_ready` fixture requiring HTTP 200,
  `class=transient`, and the stable code.
- Assert plain errors expose no class/code, uncoded classified errors expose class only, and this slice exports no
  classified-error detail.
- Feed bare and enveloped production-shape fixtures through the research classify, research execute, and fusion
  request/reply boundaries, with exactly one unwrap. Preserve current producer declarations.
- Exercise all six fusion request subjects plus readiness; preserve the constructor and six-method interface, validate
  `graph.ExactEntity`, and project only ID/triples into the existing fusion entity.
- Prove only the provisional `graph/query.Client` cohort is removed; fusion, exact reader, projection, and local adapters
  remain.
- Exercise every global-search direct, empty, and fallthrough branch and require its truthful terminal strategy.
- Prove focused bare and enveloped full-entity-only research-classify fixtures produce ordered candidates when digests
  are absent, without widening CandidateSet or Evidence.
- Require the existing research-graph E2E to parse `research.classify.candidate-count` from the seeded production
  classify result and fail unless it is greater than zero.
- Make the existing statistical `test-http-gateway` stage select/decode `strategy` from its controlled
  `globalSearch("robot warehouse", level:0)` request and require exactly `graphrag`. Query marshal, request
  construction, transport, non-200 status, body read, JSON decode, GraphQL errors, missing strategy, and wrong strategy
  are hard failures rather than warnings followed by nil.
- Prove production code contains neither the private `similaritySearch` wrapper nor query-success `RequestID`, and that
  Slice E adds no fusion-host component, port, configuration, Registry count, or readiness declaration.
- Prove the sixteen producer subjects and payload shapes remain unchanged.

Concurrent tests use explicit synchronization, never sleeps.

### Gates

- touched package tests under race;
- focused graph-query, graph-gateway, research, agentic-tools, and NATS integration tests;
- `go test -race ./...`;
- `task lint`;
- schema generation plus clean generated diff;
- contract tests and strict OpenSpec validation; and
- breaking tiers relevant to each slice. Slice E uses the strengthened existing research-graph and statistical tiers;
  it adds no stage or tier and does not require semantic E2E.

Long runs actively poll authoritative state and abort when provably wedged.

## Stop and remap gates

Implementation stops for owner ruling if:

1. Watch closure cannot be detected independently of bucket disappearance.
2. Any community-map access cannot acquire and finally validate one generation lease.
3. A replacement generation would serve, copy, or retain an old generation's maps.
4. The exact provider/consumer mapping cannot be expressed through Registry without false ownership or a general
   embedded client.
5. A custom-subject migration appears; identify its real owner and do not add an alias.
6. Deletion reaches `pkg/fusion/fusionnats`, `graph.ExactEntityReader`, `pkg/projection.MutationClient`, or classifier
   code outside the provisional client cohort.
7. Preserving fusion requires changing `fusionnats.New(requester, timeout)` or its six-operation interface.
8. GraphQL cannot converge on the exact fourteen graph-query-backed and nineteen total fields.
9. Routing requirements cannot be owned by a separate `gateway-query-routing` capability rather than response
   projection.
10. The graph-index correction requires a runtime representation migration rather than documentation of current code.
11. A slice proposes a bucket, stream, consumer, service, status key, MCP surface, general client, shim, deprecated
    alias, dual path, or compatibility period.
12. Reply convergence requires a producer wire change beyond truthful strategy population and consumer-side envelope
    unwrapping.
13. Proof requires BM25 persistence, record chunking, GC, hierarchy redesign, research orchestration, or downstream
    implementation/audit.
14. Any binary, provider, gateway, adapter, Registry snapshot, schema, spec, or fixture would land half-migrated.
15. Required unit/race/integration/contract/strict-OpenSpec and relevant semantic/agentic/research E2E gates are not
    green before breaking cutover.
16. GraphQL class/code projection requires message parsing, status inference, a new enum, or a gateway-owned code.
17. `pkg/graphview.View` cannot provide close detection, fail-closed subsequent point reads, and retry/reconciliation
    while the bucket remains without changing its public contract.
18. Correctness requires a summary generation ID, request lease, final-response validation, status/readiness fact,
    degradation reason, metric contract, config knob, or new infrastructure.
19. Preserving the two unadmitted wrappers would require a registry/discovery redesign, definition-only executor, or
    agentic query-port model; delete them instead and return to the owner if deletion is declined.
20. Either deleted name remains in shared/local discovery, `RegisterBuiltins`, `BuiltinGroupKeys`, accepted
    `SkipBuiltins`, schemas/docs, or exported Go symbols, including as a no-op compatibility value.
21. The full 21-config load does not yield nine agentic-tools instances, zero admitted shipped gateway-first tools, or
    raw `395/243/54`, effective `571/378/69`, and unchanged `176/135/15` delta.
22. Census arithmetic subtracts graph-query `DefaultConfig` rows without proving they exist in the production Registry
    snapshot.
23. Wrapper deletion changes non-reserved local executor registration, dispatch precedence, or discovery behavior.
24. Deleting the agentic wrappers reaches GraphQL `searchGraph`/`graphSummary`, their graph-query responders, or
    research consumers.
25. The slice changes `MergePortConfig` globally or invents additive dependency-port/executor metadata.

## Owner rulings

1. Adopt bounded query closure (Option 3), not measurement-only patches or broad graph convergence.
2. Replace the community bucket-presence watcher with the generation supervisor and generation-validated reads.
3. Require a usable generation for `localSearch`; let `globalSearch` and `searchGraph` serve lower-tier results with
   explicit degradation when optional community enrichment is unavailable.
4. Admit one `graph.query/v1` port family: one provider input, the retained gateway family output, and exact named
   research outputs.
5. Delete the unadmitted agentic `search_graph`/`summarize_graph` wrappers completely: shared registrations, both
   builtin group/skip keys, registration functions, implementations, full exported type/option/constructor/querier
   surface, tests, docs, and expectations. Keep GraphQL/query operations and unrelated local-tool extensibility; add no
   no-op key, replacement tool, port system, or discovery redesign.
6. Give optional summaries one catalog-backed `pkg/graphview.View`: fail subsequent point reads closed after loss,
   retry/reconcile with the existing interval, and preserve statistical fallback without generation leases,
   readiness, or degradation coupling.
7. Seed `gateway-error-projection` and copy existing classified class/non-empty code into GraphQL extensions without
   creating new classification authority.
8. Give libraries no component ports. Slice E assigns no fusion port or configuration ownership because no current
   in-repo component constructs `fusionnats.Client`.
9. Remove only the provisional mixed direct-KV `graph/query.Client` cohort, including its client-only config, caches,
   watchers, readiness/poison state, methods, path/cache types, and examples.
10. Preserve `pkg/fusion/fusionnats.Client`, its constructor, six operations, lazy readiness behavior, and downstream
   role; migrate only its reply decoding and production-shape fixtures.
11. Remove GraphQL `capabilities`; retain exactly fourteen graph-query-backed and nineteen total served root fields.
12. Seed `gateway-query-routing`; keep response projection about success projection, seed `gateway-error-projection`,
    and update `graph-query` Purpose and
    normative consumer-representation requirements.
13. Correct stale graph-index hash/catalog text to the current raw `PREDICATE_INDEX` contract without runtime change.
14. Make a clean break: no shims/deprecation/dual paths, and communicate downstream breaks without auditing or fixing
    downstream projects.

The owner approved the original fourteen rulings on 2026-08-09, with the later Slice D and Slice E reassessments
superseding only the mechanisms explicitly identified in the provenance section. On 2026-08-10 the owner approved the
following eight Slice E rulings:

1. Adopt bounded Slice E closure rather than defect-only patches or broad decoder closure.
2. Use exactly one `graph.UnwrapQueryResponse` pass at the research-classify, research-execute, and fusionnats
   request/reply boundaries only.
3. Validate `ExactEntity.KVRevision` without adding it to `fusion.Entity`.
4. Remove the nonexistent fusion-host component/port requirement.
5. Keep receiver-less research projections unchanged.
6. Delete the private `similaritySearch` wrapper without compatibility.
7. Delete the exported but unused query `RequestID` without compatibility.
8. Gate Slice E with focused race/real-NATS tests plus strengthened existing research-graph and statistical
   `test-http-gateway` E2E assertions: nonzero production classify candidate count and exact live strategy `graphrag`
   with every pre-assertion gateway failure hard. Exhaustive representation/strategy branches stay in focused tests;
   add no stage, tier, or semantic E2E requirement.
