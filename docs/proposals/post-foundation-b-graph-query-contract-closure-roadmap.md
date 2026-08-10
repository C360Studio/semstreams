# Post-Foundation-B graph query contract-closure roadmap

**Status:** Owner-ready design draft; non-binding until independent design review and owner acceptance.

**Baseline:** `967b75b6ebcb0f1b0eee9157e76c39da982aa640`.

**Accepted inventory:** `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
`c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.

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

Libraries and E2E harnesses do not implement `component.Discoverable` and synthesize no Registry ports.
`pkg/fusion/fusionnats.Client` owns adapter behavior, while a component embedding it owns six required outputs
(`byName`, `prefix`, `semantic`, `entity`, `batch`, `relationships`) plus a `GRAPH_STATUS` KV-read declaration.

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

`COMMUNITY_SUMMARIES` uses its own optional generation supervisor, independent of the partition supervisor and its
generation IDs. Remove the bucket-presence watcher, `summaryWatchStarted` once guard, and shared always-published map.
For the component lifetime, the summary supervisor:

1. retries must-exist bucket open and `WatchAll` using the existing `RecheckInterval`, including after closure while
   the bucket remains present;
2. allocates a fresh private map and monotonic token for each watch;
3. distinguishes initial sentinel from loss with `entry, ok := <-watcher.Updates()`;
4. applies pre-sentinel updates/deletes to staging only;
5. publishes that fresh generation atomically on the sentinel, including an empty generation;
6. applies post-sentinel changes only while its token is current;
7. unpublishes its exact generation on unexpected close/error, makes the map unreachable, and retries without waiting
   for bucket disappearance; and
8. treats component cancellation as orderly shutdown, without retry or loss classification.

Summary generations never copy, seed, or inherit partition state or an older summary map. `SummaryFor` consults only
the currently published summary generation and finally validates it before return. With none published, or no matching
record, the existing resolver uses `Community.StatisticalSummary`. Summary loss never gates partition readiness,
returns `index_not_ready`, or sets query degradation: the statistical floor is the already-admitted successful tier.
Loss remains visible through bounded component logging; no new status key, metric contract, or configuration surface
is introduced.

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

### One reply-envelope rule for embedded consumers

Every admitted embedded adapter invokes `graph.UnwrapQueryResponse` exactly once before operation payload decoding.
Existing bare and enveloped successes remain accepted; producers are not forced into a new envelope.

Research classify/execute and fusion migrate together. Request/result projection may remain local; envelope detection
may not. Each adapter exposes only its named operation. The two unadmitted agentic search/summary wrappers are deleted,
not migrated. Private components may compose narrow interfaces, but SemStreams adds no replacement aggregate client.

Named embedded adapters declare their operation through the component port contract and use NATS request/reply as the
transport. Raw KV and free-standing subject literals are not fallback application APIs. Remote callers use admitted
GraphQL operations. No MCP surface is added.

### Query outcomes report what happened

Every successful `GlobalSearchResponse` carries a non-empty canonical `strategy` naming the terminal strategy that
produced the response, including fallback and empty success. Existing vocabulary is retained. Errors remain errors;
adapters do not invent empty successes.

Search consumers preserve every successful representation they claim:

- full `Entities`;
- `EntityDigests`;
- `CommunitySummaries`;
- synthesized `Answer`; and
- degradation metadata.

Research adapters test full-entity-only, digest-only, summary-only, empty, bare, enveloped, and degraded fixtures.

### Retire only the provisional mixed direct-KV aggregate client

Delete only the provisional `graph/query.Client` cohort: exported Client, client-only Config/defaults, constructors,
private mixed direct-KV/RPC implementation, cache/watchers/readiness/poison, query methods, client-only path/cache types,
and obsolete examples. Retain classifier/search-option code.

Preserve `graph.ExactEntityReader`, `pkg/projection.MutationClient`, component-local research adapters, unrelated
agentic tools, and `pkg/fusion/fusionnats.Client`. There is no deprecated symbol, wrapper, replacement general client,
or compatibility period.

`fusionnats.Client` remains the admitted NATS implementation of `fusion.RetrievalClient`, with existing
`New(requester, timeout)`, optional Close, lazy GRAPH_STATUS graph-index readiness, downstream role, and six operations:
byName, prefix, semantic, entity, batch, relationships. Every reply passes through `graph.UnwrapQueryResponse` once.
Producer-shape fixtures preserve rank/order, similarity presence, ExactEntity validation, batch missing reasons/order,
relationship direction, and raw readiness. The old entity fixture expecting bare EntityState becomes production
`graph.ExactEntity`. A library claims no component ports; its embedding component owns declarations.

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

The optional `COMMUNITY_SUMMARIES` cache SHALL use a separate fresh-map generation supervisor. It SHALL distinguish a
closed updates channel from the initial sentinel, unpublish its current generation on loss, and retry `WatchAll` while
the bucket remains. Missing/lost enhanced summaries SHALL use the admitted statistical fallback without changing
partition readiness or query degradation.

#### Scenario: summary watch loss falls back and rebinds

- **GIVEN** an enhanced summary generation is published and its bucket remains
- **WHEN** that watch closes unexpectedly
- **THEN** the generation is unpublished and a new `WatchAll` is attempted
- **AND** queries use the statistical summary until a fresh generation reaches its sentinel

#### Scenario: summary deletion during the gap cannot survive

- **GIVEN** a summary existed in generation N
- **WHEN** N is lost and that summary is deleted before N+1 enumeration
- **THEN** N+1 publishes without the deleted summary
- **AND** no N map is retained, copied, or served

### Add `graph-query` success decoding and representation preservation

Every framework-owned embedded consumer SHALL remove at most one recognized `graph.QueryResponse` envelope via
`graph.UnwrapQueryResponse`, then decode its operation payload. It SHALL preserve all successful representations the
operation contract admits: full entities, entity digests, community summaries, synthesized answer, and degradation
metadata. It SHALL not turn a non-empty successful representation into count-only text or an invented empty success.

#### Scenario: adapter accepts both current success forms

- **GIVEN** equivalent valid bare and enveloped fixtures
- **WHEN** the adapter decodes each
- **THEN** both yield the same typed result
- **AND** an envelope-shaped inner payload loses no second layer

#### Scenario: full entities need no digest fallback

- **GIVEN** a successful search result containing full entities but no digests
- **WHEN** a framework adapter projects the result
- **THEN** the full entities remain available to its caller
- **AND** the adapter does not replace them with only a result count

### Add `graph-query` terminal-strategy requirement

Every successful global-search result SHALL carry the non-empty canonical terminal strategy, including empty success.
Fallback SHALL report the strategy that produced the returned result, not the abandoned initial choice.

#### Scenario: classifier choice falls through

- **GIVEN** an initial strategy cannot produce a result
- **WHEN** a lower-tier fallback succeeds
- **THEN** `strategy` names the fallback
- **AND** it is neither blank nor the abandoned choice

### Add `graph-query` fusion adapter preservation

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`, preserving its
constructor and six-operation surface. It SHALL decode each success through `graph.UnwrapQueryResponse` once and SHALL
validate entity replies as `graph.ExactEntity`. The embedding component, not the library, owns port declarations.

#### Scenario: fusion entity uses the producer representation

- **GIVEN** the graph-query entity producer returns a bare or enveloped `graph.ExactEntity`
- **WHEN** `fusionnats.Client` reads it
- **THEN** the adapter preserves the exact entity and revision
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
| #820/#609 cache lifecycle subset | Availability contract | Consumer observation only; no producer status |
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
| #633/#710 GC | Retention candidate | Separate owner-specific reachability/bounds design |
| #829 content summaries | Optional semantic enhancement | Separate |
| #606/#672/#436/#751 | Hierarchy/clustering model/cache | Separate remap |
| #391/#376/#347 | Research verification/enhancement | Separate |
| #810/#842 | Agentic tool collision | Outside graph query |
| #868 | Generic readiness | No generic change without three proven owners |
| #875 | Storage reference defect | Separate storage contract |

## Decision-skill outcomes

- `query-pattern`: remote callers use admitted GraphQL-shaped operations; embedded services use named typed adapters;
  NATS request/reply remains the declared port transport; no general client, MCP, raw KV, or unowned subject fallback.
- `orchestration-check`: the partition and optional-summary supervisors are private execution mechanics owned by the
  graph-query component lifecycle. They add no rule, workflow, lifecycle entity, or operator-visible phase state.
- `new-payload`: not applicable; no registry payload type is added.
- `kv-or-stream`: no new communication path in Option 3. Cache availability is observed through the existing KV watch.
  Option 4 would be a KV current-state fact, but would also require explicit repair/degradation obligations.

## Migration and adopter impact

| Adopter | Required action | If they do nothing | Discovery |
|---|---|---|---|
| Ordinary GraphQL caller | None | Existing 14 fields retain wire shape | Introspection/spec |
| GraphQL localSearch caller | Treat `index_not_ready` as retryable | Classified transient replaces no-responder until usable | Error contract/release note |
| GraphQL capabilities caller | Remove query; select known admitted operation | Query validation fails | Introspection/break notice |
| Embedded framework consumer | Use named port-declared adapter | In-repo consumers migrate atomically | Adapter/compiler/spec |
| Agentic config/default-tools caller naming a deleted wrapper | Remove the name; use GraphQL or a separately owned custom tool | Closed-set validation fails or discovery drops the name | Config/dispatch error/break notice |
| `SkipBuiltins` caller naming a deleted key | Remove the key | Existing validation fails; no no-op compatibility value | Boot error/break notice |
| Importer of any deleted executor symbol | Remove it or own a distinct downstream tool/component | Compilation fails; full surface is deleted | Compiler/break notice |
| Aggregate client importer | Replace with GraphQL or named adapter | Compilation fails; no shim | Compiler/migration notice |
| Direct external NATS caller | No wire change in this program | Existing wire continues, but copied literal gains no separate API promise | Migration notice |
| Readiness/config author | None | No clustering key/config introduced | Existing docs |
| Component/port author | Declare `graph.query/v1` on graph-query ports and use named outputs | Old/missing interface or consumer declarations fail Registry validation | Port contract, generated schema, break notice |

Migration notice draft:

> Query contract closure removes the unserved GraphQL `capabilities` field and exported `graph/query.Client`. No
> aliases or deprecated wrappers are provided. The 16 existing request/reply subjects and 14 remaining GraphQL
> operations are unchanged. `localSearch` always responds and reports `index_not_ready` while the optional community
> view is unavailable or synchronizing. Remote applications use GraphQL; embedded framework services use the named
> port-declared adapter for their operation. The unadmitted agentic `search_graph` and `summarize_graph` wrappers are
> removed completely, including shared registrations, builtin skip keys, and exported executor symbols; GraphQL and
> graph-query operations remain. Remove stale config/skip entries or own a distinct downstream tool. Downstream teams
> own compilation, migration, flow validation, and E2E.

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
- Close a published summary watch while its bucket remains. Prove immediate unpublish, statistical fallback without
  `index_not_ready` or degradation, and a new `WatchAll` attempt.
- Exercise summary update/delete staging, close-before-sentinel, empty enumeration, deletion during the gap, late old-
  generation events, replacement publication, and orderly cancellation without sleeps.
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
- Feed bare and enveloped production-shape fixtures through gateway, research, and fusion consumers.
- Exercise all six fusion operations plus readiness; preserve the existing constructor and use `graph.ExactEntity` for
  entity replies.
- Prove only the provisional `graph/query.Client` cohort is removed; fusion, exact reader, projection, and local adapters
  remain.
- Exercise every global-search terminal/fallback and require truthful strategy, including empty success.
- Feed full-entity, digest, summary, answer, empty, and degraded fixtures to research consumers and prove no
  admitted representation collapses to count-only output.
- Prove the sixteen producer subjects and payload shapes remain unchanged.

Concurrent tests use explicit synchronization, never sleeps.

### Gates

- touched package tests under race;
- focused graph-query, graph-gateway, research, agentic-tools, and NATS integration tests;
- `go test -race ./...`;
- `task lint`;
- schema generation plus clean generated diff;
- contract tests and strict OpenSpec validation; and
- breaking tiers: semantic, agentic, and research where migrated adapters are exercised.

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
17. Summary watch closure cannot be distinguished with the updates-channel `ok` value, or loss cannot unpublish and
    retry while the bucket remains.
18. A replacement summary generation would copy, retain, serve, or mutate an older generation's map.
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

## Owner rulings requested

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
6. Give optional summaries their own fresh-map generation supervisor, with sentinel publication, immediate
   unpublish-on-loss, retry while the bucket remains, and statistical fallback without readiness/degradation coupling.
7. Seed `gateway-error-projection` and copy existing classified class/non-empty code into GraphQL extensions without
   creating new classification authority.
8. Give libraries no component ports; the component embedding fusion owns its six outputs and readiness declaration.
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
