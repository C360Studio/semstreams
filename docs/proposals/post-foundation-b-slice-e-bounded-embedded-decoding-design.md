# Slice E design draft — bounded post-D closure

**Status:** Draft for independent pre-owner design review. No ruling is treated as approved.

## Accepted inventory

The accepted inventory is
`docs/proposals/post-foundation-b-slice-e-embedded-decoding-result-truth-inventory.md`, SHA-256:

`2033480aa58b9cb8906d4efd08e57d5e19fa71f0050c2cd98cd873c2f67bcf5e`

It is incorporated by reference without modification.

No canonical decision skill triggers: this adds no communication path, query-access mode, orchestration behavior, or
payload type.

## Options considered

| Option | Scope | Cost | Residual problem |
|---|---|---:|---|
| 0. Do nothing | Leave Slice E unchanged | None | All four proven defects remain; current Slice E text still claims nonexistent receivers and a nonexistent fusion-host component |
| 1. Proven defects only | Fix the four measured behavioral bugs; retain direct decoders, `RequestID`, and private `similaritySearch` tolerance | Small | In-slice adapters still independently interpret the same envelope class; dead compatibility surfaces remain |
| 2. Bounded Slice E closure | Option 1, plus one canonical unwrap at the three in-slice adapter boundaries, correct fusion projection, and deletion of the two dead compatibility claims | Medium-small | Out-of-slice interpreters remain deliberately separate |
| 3. Broad decoder closure | Convert every interpreter in the accepted census, including graph-query composition, clustering, exact readers, lessons, gated DAG, and surfaces scheduled for F deletion | Large | Reopens unrelated owners, overlaps F, and recreates the scope churn this reassessment is intended to stop |

## Recommendation

Adopt Option 2.

It is the smallest coherent post-D slice because it:

- fixes every measured behavioral defect;
- uses the existing shared envelope discriminator only at the three adapter boundaries Slice E already owns;
- does not change any producer payload shape except populating the existing `strategy` field;
- does not widen `fusion.Entity`, `CandidateSet`, or `Evidence`;
- removes two dead compatibility claims instead of maintaining them;
- creates no component, port, status key, bucket, stream, or service.

## Explicit adjudications

### Four proven defects

1. Full-entity `searchGraph` success becoming zero candidates: fix.

   When digests are absent and validated full entities are present, research classify projects those entities into its
   existing `research.Candidate` contract, preserving entity order and limit. It sets only facts the receiver can
   carry honestly: entity ID, optional type derived from the validated ID, tier, and source. It does not invent a
   label, relevance score, or snippet.

   Existing digest projection remains the preferred path when digests are present. This slice does not union
   unrelated response representations or widen `CandidateSet`.

2. Fusion decoding entity as `graph.EntityState`: fix.

   `fusionnats.Client.Entity` decodes the actual `graph.ExactEntity` wire shape, requires a non-nil valid entity,
   matching requested ID, and nonzero KV revision, then projects ID and triples into the existing `fusion.Entity`.

3. Blank terminal strategy: fix.

   Every successful `GlobalSearchResponse`, including empty success, reports the handler that produced the returned
   result:

   - `entity_lookup`
   - `pathrag`
   - `semantic`
   - `temporal`
   - `spatial`
   - `graphrag`
   - `semantic_fallback` for the separate `searchGraph` fallback

   A route that falls through reports `graphrag`, not the abandoned route. Internal semantic-to-text behavior inside
   GraphRAG remains `graphrag`.

4. Temporal/spatial `entity_id` versus `id`: fix.

   Graph-query decodes the producers' canonical `id` field. It does not change either producer's wire shape or retain
   unused temporal type/spatial coordinates in the global-search intermediate.

### Canonical envelope decoder

Yes: every in-slice research/fusion request/reply adapter should call `graph.UnwrapQueryResponse` exactly once at its
request boundary:

- research classify `searchGraphRetriever`;
- research execute `graphQueryAdapter`;
- `fusionnats.Client` request/reply path.

`fusionnats.Status` is KV state, not request/reply, and is excluded.

This is consumer tolerance, not permission to change producer envelopes. Each current operation retains its catalogued
bare or standard envelope. Out-of-slice interpreters are unchanged.

### Fusion revision

Do not expand `fusion.Entity`.

The KV revision is authority-read evidence used to validate the `ExactEntity` response, but the six-method retrieval
interface and lens engine have no revision consumer. After successful validation, the adapter deliberately projects
the entity into the existing ID/triples shape.

Adding revision would create a downstream API obligation with no present receiver.

### Fusion component ports

Delete the requirement that an “embedding component” owns fusion ports.

No in-repo component constructs `fusionnats.Client`; graph-embedding is unrelated and rejects output ports. Slice E
changes no component declarations or configuration for fusion. The library remains port-free.

### Narrow research projections

Leave them unchanged.

`CandidateSet` and `fusion.Evidence` do not receive full triples, relationship direction, temporal type, community
summaries, answer text, or KV revision. Their omission is not a proven behavioral defect. Slice E does not widen those
contracts.

### Private `similaritySearch` wrapper

Delete it.

The gateway has already removed and rejects `similaritySearch`; the only supported `searchGraph` fallback receives the
raw graph-embedding NATS `SearchResponse`. The private GraphQL-wrapper decoder and its tests are stale compatibility
code with no caller.

### Phantom `QueryResponse.RequestID`

Delete it in the clean cutover.

Production search finds no graph-query producer assignment or consumer read. Remove:

- the exported field;
- `queryResponseRequestIDKey`;
- envelope tests that preserve it as an accepted discriminator key.

The success envelope's closed key set becomes `data` plus `timestamp`. Mutation request IDs are unrelated and remain
unchanged.

This is a breaking Go surface deletion and requires an owner ruling.

## Measurable premises

| Premise | Measurement |
|---|---|
| Canonical unwrapping already exists | `graph/query_contracts.go:31-117` |
| Only gateway currently adopts it | `rg -n "UnwrapQueryResponse" . -g '*.go' -g '!**/*_test.go' -g '!**/doc.go'` returns the definition and gateway call |
| Research full-entity success collapses | Producer full-entity path at `processor/graph-query/graphrag.go:899-949`; consumer iterates only digests at `processor/research-graph-classify/adapters.go:144-170` |
| Exact entity wire differs from fusion decoder | `graph/exact_entity.go:14-24`; `processor/graph-ingest/query.go:60-122`; `pkg/fusion/fusionnats/client.go:370-390` |
| Fusion has no revision receiver | `pkg/fusion/lens.go:26-29` |
| Fusion interface has six total methods | `pkg/fusion/retrieval.go:17-51` |
| Fusion's six subjects are a different mapping | `pkg/fusion/fusionnats/client.go:25-32,249-263,531-573` |
| Global-search strategy is generally blank | Only response assignment found at `processor/graph-query/searchgraph.go:289-295` |
| Temporal/spatial producers emit `id` | `processor/graph-index-temporal/query.go:30-34`; `processor/graph-index-spatial/query.go:45-55` |
| Consumer expects `entity_id` | `processor/graph-query/graphrag.go:1319-1335` |
| Private wrapper has no supported caller | `similaritySearch` production hits are confined to `processor/graph-query/searchgraph.go`; gateway rejects it at `gateway/graph-gateway/query_contract_closure_test.go:72-75` |
| Query `RequestID` is unused | Focused production search finds zero assignment/read; remaining `RequestID` fields belong to mutation/agentic contracts |
| No fusion-host component exists | Production search for `fusionnats.New`, `fusionnats.Client`, or `fusion.RetrievalClient` finds definitions/comments only |
| Existing research E2E reaches production classify but does not assert its result | `test/e2e/scenarios/research-graph/scenario.go:350-449` asserts the classify-complete timestamp but does not inspect `research.classify.candidate-count`; that count is stamped from `len(output.Candidates)` at `processor/research-graph-classify/component.go:457-470` |
| Existing statistical E2E has one admitted controlled gateway probe but can currently false-green | `test/e2e/scenarios/tiered.go:341-350` admits `test-http-gateway` to statistical and semantic variants. Its `globalSearch("robot warehouse", level:0)` request at `test/e2e/scenarios/validate_infra.go:363-450` omits `strategy`, and every marshal/request/transport/status/read/parse/GraphQL failure currently appends a warning and returns nil before any assertion. The separate `executeTestGraphRAGGlobal` path is not the statistical proof seam. |

## Replacement design text

### Slice E: bounded embedded decoding and truthful query outcomes

Slice E owns only the research classify adapter, research execute adapter, `fusionnats.Client`, and the graph-query
outcome defects proven by the accepted inventory. It does not normalize every reply interpreter in the repository.

Each of the three in-slice request/reply adapter boundaries removes at most one recognized `graph.QueryResponse`
envelope through `graph.UnwrapQueryResponse` before decoding the operation payload. Current producer envelope
declarations remain unchanged. Bare and equivalent standard-enveloped fixtures prove consumer tolerance; they do not
authorize producers to switch formats.

Research classify retains its existing `CandidateSet` contract. Digest-bearing responses retain current behavior.
When a successful `searchGraph` reply contains full entities but no digests, the adapter validates the entities and
projects them, in response order and under the existing limit, into candidates. It does not invent facts unavailable
from the entity.

Research execute retains its current Evidence projection. Batch triples, relationship direction, temporal type,
summaries, answer text, and other fields without an Evidence receiver are not added.

`fusionnats.Client` preserves its constructor, optional `Close`, lazy graph-index readiness, and six-method
`RetrievalClient` implementation. Its six request subjects remain a separate transport mapping. Request/reply
successes pass through one unwrap. `Status` remains a `GRAPH_STATUS` KV read. Entity replies decode and validate
`graph.ExactEntity`, then project into the existing ID/triples `fusion.Entity`; KV revision is validation evidence,
not a new fusion field.

Graph-query decodes temporal and spatial result IDs using the producers' canonical `id` spelling. Every successful
global-search response reports the terminal strategy that returned it, including empty success and fallthrough. No
new strategy type or exported enum is introduced.

The unsupported private GraphQL `similaritySearch` fallback shape is deleted; `searchGraph` fallback accepts only the
actual raw graph-embedding response. The unused `QueryResponse.RequestID` field and discriminator key are deleted.
Mutation correlation remains unchanged.

No component is assigned ownership of fusion ports because no current component constructs the client. No component
configuration, port declaration, readiness producer, or Registry count changes in Slice E.

The existing research-graph E2E SHALL prove more than pipeline completion: for its seeded `drone hover anomalies`
query, it SHALL read the production classify result already stamped on the loop entity and require
`research.classify.candidate-count` to parse as an integer greater than zero. Focused adapter fixtures, not this E2E,
prove that the candidates specifically came from the full-entity-only response representation.

The existing `test-http-gateway` stage SHALL provide the live strategy proof because it is already admitted to the
statistical and semantic variants at `test/e2e/scenarios/tiered.go:343`. Its controlled
`globalSearch("robot warehouse", level:0)` request SHALL select `strategy`, decode it, and require exactly `graphrag`.
Missing or different strategy is a hard stage failure.

To prevent a false-green before that assertion, this stage's existing query marshal, request construction, transport,
non-200 status, body read, JSON decode, and GraphQL-error branches SHALL return errors rather than warnings followed by
nil. This prices one intentional tightening: the existing gateway probe becomes a contract gate in every variant where
it already runs. It adds no query, stage, tier, latency threshold, hit-count minimum, answer-quality assertion, or
semantic-only dependency. Focused graph-query table tests remain responsible for every other direct, empty, and
fallthrough strategy branch.

## Replacement graph-query spec text

```markdown
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
```

## Replacement fusion spec text

```markdown
### Requirement: The operation-specific NATS fusion adapter remains stable

`pkg/fusion/fusionnats.Client` SHALL remain the NATS implementation of `fusion.RetrievalClient`. It SHALL preserve
`New(requester, timeout)`, optional Close, lazy `GRAPH_STATUS/graph-index` readiness, and the six interface methods
`Status`, `Resolve`, `Entity`, `Entities`, `Neighbors`, and `Names`.

The transport SHALL retain six request subjects: by-name, prefix, semantic, entity, batch, and relationships. These
subjects are not a one-to-one restatement of the interface: Status uses KV, Resolve selects among three subjects, and
Names reuses by-name.

Every request/reply success SHALL pass through `graph.UnwrapQueryResponse` exactly once before operation decoding.
Status SHALL remain outside this rule because it reads KV state.

Entity SHALL decode the producer's `graph.ExactEntity`, require a valid matching entity and nonzero KV revision, and
project its ID and triples into the existing `fusion.Entity`. The revision SHALL NOT expand `fusion.Entity` or
`RetrievalClient` without a present consumer.

The fusion library SHALL claim no component ports. This change SHALL NOT invent a component or configuration owner for
the client.

#### Scenario: entity uses the producer representation

- **GIVEN** a valid `graph.ExactEntity` reply
- **WHEN** `fusionnats.Client.Entity` reads it
- **THEN** the exact entity and revision are validated
- **AND** the existing fusion entity contains its ID and triples
- **AND** no obsolete bare `EntityState` fixture remains

#### Scenario: request subjects accept one envelope

- **GIVEN** equivalent bare and standard-enveloped fixtures for each request subject
- **WHEN** fusion decodes them
- **THEN** each pair produces the same existing retrieval result
- **AND** no payload is unwrapped twice

#### Scenario: fusion preservation creates no port owner

- **WHEN** Slice E component and configuration changes are inspected
- **THEN** no fusion-host component or fusion port declaration was added
- **AND** the client constructor and interface remain unchanged
```

## Component-discovery correction

Replace the false fusion-host paragraph with:

```markdown
Libraries and E2E harnesses SHALL NOT synthesize component ports. Slice E adds no port or configuration requirement for
`pkg/fusion/fusionnats.Client` because no current in-repo component constructs it. Research classify and execute retain
their already-admitted exact graph-query outputs.
```

## Revised Slice E tasks

```markdown
## E. Bounded embedded decoding and truthful query outcomes

- [ ] E.1 Add failing focused tests showing: full-entity-only searchGraph success becomes zero research candidates;
  fusion Entity rejects the real ExactEntity shape; successful global-search strategies are blank; and temporal/spatial
  `id` rows become zero IDs.
- [ ] E.2 Pass each success through `graph.UnwrapQueryResponse` exactly once at the research-classify,
  research-execute, and fusionnats request/reply boundaries. Prove current bare and equivalent standard-enveloped
  fixtures agree without changing producer declarations.
- [ ] E.3 Project validated full entities into the existing research Candidate contract only when digests are absent.
  Preserve order, limit, digest behavior, and degradation. Do not widen CandidateSet or Evidence.
- [ ] E.4 Decode fusion Entity replies as `graph.ExactEntity`; validate matching entity and nonzero revision, then
  project only ID and triples into the existing `fusion.Entity`. Preserve the six-method interface, six request-subject
  mapping, constructor, lazy readiness, ordering, similarity, missing reasons, and relationship direction.
- [ ] E.5 Decode temporal and spatial result IDs from canonical `id`; populate the existing terminal strategy on every
  successful global-search path, including empty and fallthrough success.
- [ ] E.6 Delete the unreachable private `similaritySearch` wrapper decoder and tests. Delete the unused
  `QueryResponse.RequestID`, discriminator key, and request-ID envelope fixtures. Add no alias or compatibility path.
- [ ] E.7 Remove every claim that a nonexistent embedding/fusion-host component owns six fusion outputs or readiness.
  Make no Slice E component, configuration, Registry-count, schema, service, bucket, stream, or readiness change.
- [ ] E.8 Extend the existing research-graph E2E to read `research.classify.candidate-count` from the seeded loop's
  production classify result and fail unless it is a parsed integer greater than zero. In the existing
  `test-http-gateway` stage, add `strategy` to the controlled `globalSearch("robot warehouse", level:0)`
  selection/response, require exactly `graphrag`, and make every earlier marshal/request/transport/status/read/parse/
  GraphQL failure return a hard error so the assertion cannot false-green. Run `task e2e:research-graph`,
  `task e2e:statistical`, focused package tests under race, the focused real-NATS fusion integration test, and
  independent SemStreams review; add no stage or tier.
```

## Verification

Fast gates:

```text
go test -race ./graph
go test -race ./processor/research-graph-classify
go test -race ./processor/research-graph-execute
go test -race ./processor/graph-query
go test -race ./pkg/fusion/...
go test -race -tags=integration ./pkg/fusion/fusionnats
```

Required behavioral fixtures:

- focused research-classify fixtures: bare and enveloped full-entity-only success produces candidates; these fixtures,
  not pipeline completion, prove the representation path;
- research execute: all four operation decoders agree for bare/enveloped fixtures;
- fusion: all six subject decoders agree, with actual `ExactEntity`;
- ExactEntity zero revision, nil entity, poisoned entity, and requested-ID mismatch fail;
- focused terminal-strategy table: every direct, empty, and fallthrough branch reports its truthful terminal strategy;
- temporal/spatial tests use actual producer row types or exact canonical shapes;
- negative searches close `similaritySearch`, query `RequestID`, and invented fusion ports.

Existing E2E assertions:

- `task e2e:research-graph` SHALL inspect the seeded loop entity after production classify, parse
  `research.classify.candidate-count`, require it to be greater than zero, and record the value. A classify-complete
  timestamp or terminal pipeline completion alone is not evidence for candidate preservation.
- `task e2e:statistical` SHALL exercise the already-admitted `test-http-gateway` stage. That stage SHALL select and
  decode `strategy` from its controlled `globalSearch("robot warehouse", level:0)` request, require exactly `graphrag`,
  and fail hard on query marshal, request construction, transport, non-200 status, body read, JSON decode, GraphQL
  errors, missing strategy, or wrong strategy. Merely reaching globalSearch, returning entities, or appending a warning
  is not evidence for terminal-strategy truth.

The research E2E proves a nonzero live classify result; focused adapter fixtures prove the full-entity-only
representation. The statistical `test-http-gateway` stage proves one controlled live strategy; focused table tests
prove all direct, empty, and fallthrough branches. Tightening that existing stage changes its gateway-contract failures
from warnings to test failures in both variants where it already runs, but adds no unrelated result-quality
requirement.

Do not invent another tier. Do not require the unrelated-red semantic tier to prove this slice. Fusion entity remains
covered by focused real-NATS integration because no current E2E composition constructs that path.

## Explicit non-goals

- No broad migration of graph-query compositor, clustering, gated-DAG, lessons, exact-reader, or other out-of-slice
  interpreters.
- No widening of `Candidate`, `CandidateSet`, `Evidence`, `fusion.Entity`, or `RetrievalClient`.
- No producer envelope change or dual producer format.
- No fusion-host component, ports, configuration, Registry facts, or readiness declaration.
- No new subject, query operation, exported decoder, general client, MCP surface, bucket, stream, service, status key,
  or retry knob.
- No semantic-search quality work, research orchestration change, hierarchy work, index refactor, storage work, or
  downstream audit.
- No compatibility shim for `similaritySearch` or query `RequestID`.
- No reliance on semantic E2E.
- No claim that research pipeline completion alone proves the classify adapter preserved candidates.
- No claim that a successful globalSearch call alone proves terminal strategy; the live response must be selected and
  asserted.
- No use of the separate `executeTestGraphRAGGlobal` path as the strategy gate; it is not the admitted statistical
  proof seam.
- No new gateway stage or tier, and no minimum hit count, answer quality, latency, semantic-model, or other unrelated
  assertion in `test-http-gateway`.

## Owner rulings required

1. Approve Option 2 rather than defect-only patches or broad decoder closure.
2. Approve exactly-once `UnwrapQueryResponse` use at the three in-slice adapter boundaries only.
3. Approve validating `ExactEntity.KVRevision` but not adding it to `fusion.Entity`.
4. Remove the nonexistent fusion-host component/port requirement.
5. Keep receiver-less research projections unchanged.
6. Delete the private `similaritySearch` wrapper without compatibility.
7. Delete the exported but unused query `RequestID` field without compatibility.
8. Accept focused race/real-NATS tests plus two strengthened existing E2E seams as the Slice E gate: research-graph
   requires a nonzero production classify candidate count, and statistical `test-http-gateway` requires exact live
   strategy `graphrag` with all pre-assertion gateway failures made hard. Exhaustive representation and strategy
   branches remain in focused tests; add no stage, tier, or semantic E2E requirement.
