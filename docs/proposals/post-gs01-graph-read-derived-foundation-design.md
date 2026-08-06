<!--
Complete pre-owner handoff.
Part I is the accepted inventory embedded byte-for-byte.
Part II is the unapproved design.
-->

## Part I — Accepted post-GS-01 inventory

# Post-GS-01 graph-state reality audit

## 1. Audit frame

- Baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`
- Branch inspected: `codex/post-gs01-reality-audit`
- Worktree state at inspection: clean
- Mode: inventory only
- Downstream repositories: not inspected; they remain an unmeasured holdout set
- GitHub queue: 177 open issues on 2026-08-05
- Verification run: `go test ./pkg/lifecycle -run 'TestManager_(ConcurrentCreateOnlyOneWins|CreateOnExistingPhaseTripleErrAlreadyExists)$' -count=1` — green
- Parent verification runs:
  - `go test -race ./pkg/lifecycle -run 'TestManager_ConcurrentCreateOnlyOneWins|TestCreate_UnrelatedConcurrentUpdateIsNotADuplicateBirth' -count=10` — green
  - `go test -race ./processor/graph-ingest -run 'Exact|Competing|Revision|Mutation' -count=1` — green
  - `go test -race ./processor/gated-dag -run 'Claim|Dispatch' -count=1` — green
- Full integration and E2E suites were not run.

ADR-090 and ADR-091 are the current architectural records. ADR-090 retains current-state authority, eventual
consistency, no general CQRS runtime, and no recovery subsystem. ADR-091 supersedes semantic ownership with typed
request/reply mutation plus CAS. The `discovery-under-stream-shapes` and `semantic-tier-split` changes remain
explicitly suspended and frozen; they are historical hypotheses, not current executable plans.

## 2. Current authoritative-state surface

### 2.1 Authority and storage

`ENTITY_STATES` is the sole authoritative graph bucket in the catalog:

- Owner: `graph-ingest`
- Class: authoritative
- History: 1
- Replicas: 1
- Retention: no lifecycle

Evidence: `graph/kvcatalog.go:37-68`, `graph/kvcatalog.go:120-127`.

The catalog is descriptive and controls bucket acquisition and generic write protection. Its owner strings are not
runtime writer identities: `graph/kvcatalog.go:174-241`.

Production searches found no direct `ENTITY_STATES` write outside graph-ingest. Direct readers remain in several
shapes:

- exact request/reply adapter: `graph/exact_entity.go:15-98`
- aggregate mixed KV/RPC client: `graph/query/client.go:153-278`
- lifecycle direct KV reader and watcher: `pkg/lifecycle/manager.go:222-264`, `pkg/lifecycle/manager_query.go`
- graph-ingest query responders: `processor/graph-ingest/query.go`
- always-registered service-manager `GET /graph/triples` direct KV scan:
  `service/graph_triples_http.go:162-221`, `service/service_manager.go:1408-1409`
- operation-specific readers in agentrun, rule, gated-DAG, agentic-loop, agentic-tools and PathRAG
- operator/E2E and diagnostic reads

`pkg/lifecycle.Manager` opens `ENTITY_STATES` through `GetKeyValueBucket`, while newer readers use
`OpenCatalogBucket` or the exact adapter. This is a current same-class acquisition collision, not a second authority.

### 2.2 Canonical mutation port

The current graph mutation protocol is one typed component-port family:

- interface: `semstreams.graph.mutation`
- version: `v1`
- family: `graph.mutation.>`
- leaves:
  - `entity.create`
  - `entity.reconcile`
  - `triple.append`
  - `entity.delete`

Evidence: `internal/graphmutation/protocol.go:10-45`.

Graph-ingest’s default component definition declares:

- Graphable JetStream input `entity.>`
- required typed `nats-request` mutation provider
- `ENTITY_STATES` KV-write output

Evidence: `processor/graph-ingest/component.go:387-418`.

Graph-ingest validates that exactly one required provider matches the interface, version and family:
`processor/graph-ingest/canonical_mutations.go:128-189`.

The one in-repository wire adapter makes exactly one request and never retries an ambiguous delivery. No responders
maps to unavailable; timeout, malformed response and other transport failures map to commit-unknown:
`internal/graphmutation/client.go:70-175`.

### 2.3 Operation algebra and outcomes

The request types are:

| Operation | Required authority evidence | Missing entity | Mutation |
|---|---:|---|---|
| Create | none | creates atomically | entity plus initial triples |
| Reconcile | nonzero expected KV revision | not found | exact selected-predicate replacement |
| Append | none | per-subject not found | exact-tuple append, deduplicated |
| Delete | nonzero expected KV revision | not found | revision-fenced deletion |

Evidence: `graph/mutation_requests.go:8-41`, `processor/graph-ingest/canonical_mutations.go:210-451`.

The response algebra includes applied, unchanged, not-found, already-exists, revision-mismatch, invalid and failed
outcomes, with nonzero revisions on verified applied/unchanged state: `graph/mutation_responses.go:90-173`.

`RequestID` and `TraceID` are echoed correlation fields. No request-claim bucket, ledger or idempotency record was
found. Append tuple deduplication and Graphable merge convergence are state semantics, not request-scoped
idempotency.

### 2.4 Physical write behavior

Canonical create uses KV `Create`: `processor/graph-ingest/canonical_mutations.go:214-250`.

Canonical reconcile:

1. reads entity plus current revision,
2. compares the caller’s expected revision,
3. computes selected-predicate equality,
4. writes with KV CAS,
5. returns the committed revision.

Evidence: `processor/graph-ingest/canonical_mutations.go:255-320`.

Canonical append groups by subject, reports one outcome per distinct subject, and does not create missing entities:
`processor/graph-ingest/canonical_mutations.go:322-409`.

Canonical delete validates the expected revision and calls revision-fenced delete:
`processor/graph-ingest/canonical_mutations.go:412-451`, `processor/graph-ingest/component.go:2139-2158`.

Graphable ingest remains a distinct semantic lane. It uses `UpdateWithRetry`, creating on an empty value and
predicate-merging on an existing value: `processor/graph-ingest/component.go:1875-2039`.

Graphable merge still provides semantics not present in canonical RPC create:

- latest-wins metadata refresh
- monotonic logical `EntityState.Version`
- predicate-level merge
- optional hierarchy projection on first Graphable birth

Canonical RPC create has no hierarchy side effects. Graphable hierarchy behavior is documented at
`processor/graph-ingest/component.go:1928-1953`; strict create remains at
`processor/graph-ingest/component.go:2056-2136`.

### 2.5 Missing references

Production searches found no stub, pending-edge, delayed-drain or foreign-edge implementation. The following
categories are absent from production Go code:

- `StubMessageType`
- `IsStub`
- `PENDING_EDGES`
- owner claim/presence buckets
- owner tokens or leases
- `create_with_triples`
- `update_with_triples`
- old triple add/remove subjects

One stale comment still claims graph-ingest creates referential stubs: `processor/gated-dag/reader.go:147-154`. The
code beneath it no longer tests or excludes a stub type.

Missing relationship objects are therefore represented only by source triples whose object cannot currently be
resolved.

## 3. Current read surfaces

### 3.1 Exact authority read

`graph.ExactEntity` contains a validated `EntityState` and the nonzero KV revision from the same entry.
`EntityState.Version` is explicitly not a replacement for `KVRevision`: `graph/exact_entity.go:17-27`.

The embedded adapter hides the literal subject `graph.ingest.query.entity`, validates the identifier, decodes the exact
envelope and rejects a zero revision: `graph/exact_entity.go:30-98`.

Graph-ingest’s responder reads one KV entry and returns the value and `entry.Revision()`:
`processor/graph-ingest/query.go:60-123`.

Graph-query validates that proxied entity responses contain both a valid entity and nonzero revision:
`processor/graph-query/query.go:271-289`.

GraphQL advertises exact entity results with `entity` and `kvRevision`:
`gateway/graph-gateway/component.go:1561-1603`. The core E2E scenario queries that shape:
`test/e2e/scenarios/graph_roundtrip.go:28`.

### 3.2 Graph-ingest query surface

Graph-ingest subscribes to four untyped query subjects:

- `graph.ingest.query.entity`
- `graph.ingest.query.batch`
- `graph.ingest.query.prefix`
- `graph.ingest.query.suffix`

Evidence: `processor/graph-ingest/query.go:27-55`.

Only the exact entity subject is hidden behind a narrow exported adapter. Batch, prefix and suffix remain directly
known by several internal callers.

### 3.3 Aggregate embedded client

`graph/query.Client` still exists as a general mixed direct-KV/RPC client.

It:

- directly opens `ENTITY_STATES`, `SPATIAL_INDEX` and `INCOMING_INDEX`
- opens one lifetime `ENTITY_STATES` `WatchAll`
- preflights the full initial snapshot
- invalidates its cache on every live authority update
- permanently latches poison or watcher loss for that client instance
- lazily opens a `GRAPH_STATUS/graph-index` watcher
- gates some direct derived reads on graph-index health

Evidence: `graph/query/client.go:153-278`, `graph/query/client.go:384-545`.

The watcher-loss latch is permanent; the code itself says callers must construct a new client:
`graph/query/client.go:261-277`.

### 3.4 Graph-query coordinator

Graph-query runtime registers 15 `graph.query.*` responders: `processor/graph-query/query.go:18-65`.

Its default declared input ports list only four:

- entity
- batch
- relationships
- pathSearch

Evidence: `processor/graph-query/component.go:89-103`.

No exported request-subject list exists. Searches found no `QuerySubjects`, `RequestSubjects` or equivalent symbol.

The static downstream router maps query types to graph-ingest, graph-index, spatial, temporal, embedding and clustering
subjects: `processor/graph-query/router.go:7-60`.

### 3.5 GraphQL gateway

The graph gateway is a hand-written GraphQL-shaped facade, not a general GraphQL executor:

- it routes by lowercased substring inspection of the complete query text
- it does not apply selection-set projection
- it advertises a bounded hand-written introspection schema
- it has no mutation root
- it declares one wildcard `graph.query.*` output port
- its `/mcp` route returns only `{"message":"MCP endpoint"}`

Evidence:

- `gateway/graph-gateway/doc.go:1-39`
- `gateway/graph-gateway/component.go:822-941`
- `gateway/graph-gateway/component.go:1561-1618`
- `gateway/graph-gateway/component.go:1895-1925`

The wildcard port declaration does not enumerate the real request subjects.

### 3.6 Always-registered service-manager triple scan

Every service-manager mux registers `GET /graph/triples`, including flows that do not wire graph-gateway:
`service/service_manager.go:1397-1409`.

The endpoint is a separate direct-authority read implementation rather than a graph-query or graph-ingest client. For
each request it opens `ENTITY_STATES` through the catalog, lists all keys, fetches them one at a time, canonically
decodes each entity, filters triples in process, and stops once the response limit is reached:
`service/graph_triples_http.go:162-221`.

Its current observable semantics differ from the other read fronts:

- a manager with no NATS client returns HTTP success with an empty triple list
- a key that disappears or fails fetch after enumeration is logged and skipped
- a stored entity decode failure aborts the whole request
- the limit applies while traversing keys, so the endpoint does not prove a complete matching result set
- its authentication is inherited from whatever protects the service-manager mux

The route is intended for low-throughput E2E, operator-dashboard and debugging use:
`service/graph_triples_http.go:3-13`, `service/graph_triples_http.go:168-170`.

## 4. Derived-state and index inventory

| Owner | Source/read model | Durable outputs | Update/rebuild behavior | Query/readiness surface |
|---|---|---|---|---|
| graph-ingest | Graphable stream plus mutation requests | `ENTITY_STATES`, `ENTITY_SUFFIX_INDEX`, `GRAPH_INGEST_APPLIED_SEQ` | keyed CAS merge; startup authority validation; suffix maintained as side effect | ingest entity/batch/prefix/suffix; `GRAPH_STATUS/graph-ingest` |
| graph-index | `ENTITY_STATES` `WatchAll` | outgoing, incoming, alias, predicate, name | initial replay plus live watch; keyed current-authority reconciliation; bounded write retry; failed-entity repair loop | eight index subjects; `GRAPH_STATUS/graph-index` |
| graph-index-spatial | `ENTITY_STATES` `WatchAll` | `SPATIAL_INDEX` | incremental initial replay plus same live watcher; query gated until bootstrap sentinel | bounds and polygon subjects; in-process bootstrap health only |
| graph-index-temporal | `ENTITY_STATES` `WatchAll` | `TEMPORAL_INDEX`, reverse map | incremental initial replay plus live watcher; reverse map retracts stale time rows | range subject; in-process bootstrap health only |
| graph-embedding | `ENTITY_STATES` `WatchAll`, optional content stores | `EMBEDDING_INDEX`, `EMBEDDING_DEDUP` | two-hop pending/terminal pipeline, live watcher, repair/stranding state and readiness watermark | similar/search/status; `GRAPH_STATUS/graph-embedding` |
| graph-clustering | periodic authority/index reads; readiness watches | community index, summaries, anomaly index | whole periodic community derivation; readiness-gated; summary enhancement and anomaly workers | four clustering subjects; consumes graph-index/embedding status but publishes no graph-status key |

Catalog evidence: `graph/kvcatalog.go:120-158`.

Graph-index evidence:

- startup, output buckets and watcher: `processor/graph-index/component.go:570-682`,
  `processor/graph-index/component.go:790-920`
- current-authority reconciliation: `processor/graph-index/component.go:1015-1026`
- retry and repair loop: `processor/graph-index/component.go:1029-1091`
- required-write failure blocks readiness: `processor/graph-index/component.go:1395-1465`

Spatial evidence: `processor/graph-index-spatial/component.go:442-505`,
`processor/graph-index-spatial/component.go:579-650`.

Temporal evidence: `processor/graph-index-temporal/component.go:451-515`,
`processor/graph-index-temporal/component.go:599-653`.

Embedding evidence: `processor/graph-embedding/component.go:620-710`,
`processor/graph-embedding/component.go:1423-1495`.

Clustering evidence: `processor/graph-clustering/component.go:878-990`,
`processor/graph-clustering/component.go:1302-1390`.

The derived owners share the authority source and catalog seam, but not one lifecycle contract:

- graph-index has a durable failed-entity repair loop and publishes distributed readiness
- spatial and temporal retain a continuous watcher and local bootstrap/failure gates but publish no `GRAPH_STATUS` key
- embedding persists intermediate state and publishes distributed readiness
- clustering periodically recomputes a whole result, consumes readiness, and publishes no readiness envelope

### 4.1 Existing shared read/convergence primitives

`pkg/graphview` is a specified, domain-agnostic, in-process current-state projection over one injected KV bucket. One
`WatchAll` decodes once and fans an atomic snapshot plus coalesced deltas to local subscribers. It has explicit initial
replay, watermark, tombstone, poison, watcher-loss, restart and backpressure semantics:
`pkg/graphview/doc.go:1-60`, `openspec/specs/graph-view-subscription/spec.md:3-28`.

Its current production consumer is agentic-dispatch’s shared `AGENT_LOOPS` activity view:
`processor/agentic-dispatch/http_activity.go:131-171`, `processor/agentic-dispatch/http_activity.go:193-215`.
No production `pkg/graphview` consumer over `ENTITY_STATES` was found. The primitive therefore exists in the same
read/convergence class but does not currently unify the graph-derived owners listed above.

`pkg/revlag.Watermark` is a shared low-water-of-pending tracker for ordered, possibly sparse KV revisions. It records
observed revisions, key-scoped terminal completion, the highest fully covered revision, and the covered KV commit time:
`pkg/revlag/watermark.go:1-16`, `pkg/revlag/watermark.go:23-64`.

Current production consumers are graph-index and graph-embedding:
`processor/graph-index/component.go:265-268`, `processor/graph-index/component.go:626-628`,
`processor/graph-embedding/component.go:328-333`, `processor/graph-embedding/component.go:680-682`. Spatial, temporal
and clustering do not use `pkg/revlag`. This means caught-up watermark mechanics are shared by two derived owners while
their surrounding repair and readiness lifecycles remain different.

## 5. GRAPH_STATUS, component health and lifecycle status

### 5.1 GRAPH_STATUS

Current producer keys are:

- `graph-index`
- `graph-embedding`
- `graph-ingest`
- `rule`

Evidence: `graph/readiness/watcher.go:39-70`.

The producer set is explicitly deployment-dependent. Consumers declare their required keys.

Current consumers include:

- `graph/query.Client` watching `graph-index`
- graph-clustering watching `graph-index` and optionally `graph-embedding`
- graph-gateway exposing a configured set through `/readiness`

Evidence:

- `graph/query/client.go:390-525`
- `processor/graph-clustering/component.go:1350-1390`
- `gateway/graph-gateway/readiness_surface.go:35-105`

Catalog collision: `graph/kvcatalog.go:69-74` describes the owner as `graph-index/graph-embedding`, while current
producers also include graph-ingest and rule.

No `GRAPH_STATUS` key exists for graph-clustering, spatial or temporal.

### 5.2 COMPONENT_STATUS

`COMPONENT_STATUS` is a separate diagnostic bucket:

- shared write-open producer model
- History 1
- unmanaged retention
- catalog comment records 24 production writers and zero production readers, excluding E2E

Evidence: `graph/kvcatalog.go:103-118`.

Components acquire it through `component.NewCatalogLifecycleReporter`:
`component/lifecycle_reporter_catalog.go:11-37`.

Service-manager lifecycle/health HTTP state is another separate in-process surface:
`service/component_manager.go:2222-2249`.

### 5.3 Lifecycle graph state

Lifecycle is a schema-and-discipline layer over `ENTITY_STATES`, not another authority.

Create behavior:

- absent entity → atomic canonical create
- existing entity without lifecycle phase → exact-read revision-fenced reconcile
- existing phase → already exists
- attach revision mismatch → re-read; phase present means concurrent attach, phase absent means unrelated contention

Evidence: `pkg/lifecycle/manager.go:456-523`.

Transition and operator update use bounded component-level CAS retries only after definite revision mismatch:
`pkg/lifecycle/manager.go:568-721`, `pkg/lifecycle/manager.go:780-834`.

History is no longer KV revision history. Each current entity carries a bounded window of 64 occurrence-discriminated
transition records: `pkg/lifecycle/transition_records.go:13-18`, `pkg/lifecycle/manager_query.go:516-543`.

Despawn uses revision-fenced canonical delete. Derived-index cleanup remains asynchronous through index owners:
`pkg/lifecycle/manager.go:922-1022`.

## 6. Projection and component mutation consumers

`pkg/projection.MutationClient` is the reusable local projection-contract client.

It provides:

- strict create with local entity and birth-predicate validation
- exact-read plus one revision-fenced reconcile
- one append request
- caller-revision-fenced delete
- exact authority read

Evidence: `pkg/projection/mutation_client.go:132-259`.

Create and append require stable request ID/source metadata and canonicalize triple context/timestamp:
`pkg/projection/mutation_client.go:334-374`.

The framework does not enforce projection contracts globally. Direct callers can use
`internal/graphmutation.Client` without a local contract.

Current examples:

- rule’s raw-named `AddTriple` and `RemoveTriple` methods now use canonical append and exact-read/reconcile, but not
  `pkg/projection`: `processor/rule/triple_mutator.go:36-90`
- contract-bound rule actions use `pkg/projection`: `processor/rule/actions.go:1032-1144`
- gated-DAG claims use exact-read/reconcile directly: `processor/gated-dag/claim.go:21-62`
- inference applies relationships through canonical append, not a projection contract:
  `graph/inference/applier.go:258-267`
- agentic-loop graph writes use canonical append
- web observations have a typed message origin and explicit create-then-append behavior, but no located projection
  contract: `agentic/web_observation_entity.go:19-81`, `processor/agentic-tools/executors/web_emit.go:51-72`

### 6.1 Current rule-reconcile contract collision

Runtime truth for `reconcile_predicates` is one call from the action executor into the bound projection reconciler;
that client performs one exact read followed by one reconcile request, with no revision-mismatch retry:
`processor/rule/actions.go:1108-1144`, `pkg/projection/mutation_client.go:177-200`. The active GS-01 delta requires
that one-attempt behavior and says the component owns any future operation-specific retry policy:
`openspec/changes/establish-graph-read-write-foundation/specs/rule-projection-mutations/spec.md:81-93`.

The current canonical `rule-projection-mutations` spec still requires one automatic fresh-read/recompute/retry after a
definite revision mismatch:
`openspec/specs/rule-projection-mutations/spec.md:82-94`.

Those two current contract sources contradict each other. The active GS-01 delta and merged runtime agree; the
canonical spec has not yet incorporated that delta. This is contract drift, not evidence that runtime performs a
retry.

## 7. Same-class collisions

| Semantic class | Current implementations | Observable collision |
|---|---|---|
| Authority read | exact RPC adapter; graph-ingest query handlers; direct KV lifecycle reads; aggregate `graph/query.Client`; always-on `/graph/triples` scan; direct operator/E2E reads | different acquisition, watcher, revision, completeness and poison behavior |
| Mutation caller | `internal/graphmutation.Client`; `pkg/projection.MutationClient`; lifecycle emitter; direct component wrappers | one wire protocol, but contract/provenance/retry policy remains caller-specific |
| Rule reconcile contract | canonical current spec; active GS-01 delta; merged runtime | canonical spec requires one conflict retry; active delta and runtime require one mutation attempt |
| Graph query declaration | graph-query default ports; graph-query runtime handler table; static router; graph-gateway wildcard port; GraphQL introspection | declarations enumerate different surfaces |
| Derived lifecycle | `pkg/graphview` shared current-state projection; `pkg/revlag` watermark; continuous WatchAll; WatchAll plus repair; two-hop durable pipeline; periodic whole recomputation | shared primitives cover only part of the current graph owner set and no single convergence/readiness contract spans all owners |
| Readiness | GRAPH_STATUS producer envelopes; local component health; COMPONENT_STATUS; service-manager status | same word “status” spans separate storage and semantics |
| Embedded query | narrow exact reader; aggregate graph/query client; package-specific direct readers; `pkg/graphview` with zero current `ENTITY_STATES` consumers | ADR-090’s narrow-adapter state coexists with the provisional aggregate client and an unused-for-graph shared-view primitive |
| Storage owner description | catalog owner string; actual producer key set | GRAPH_STATUS catalog text omits two present producers |

## 8. Adopter seam inventory

Specific adopter: a developer outside this repository writing a SemStreams component without reading graph-ingest
internals.

| Current outward surface | What the adopter must know | Do-nothing behavior | Where discoverable | Knowledge the framework already absorbs |
|---|---|---|---|---|
| Mutation component port | interface `semstreams.graph.mutation`, v1, family `graph.mutation.>` | invalid or missing typed port fails composition/start for validated components | component config/schema and protocol constants | leaf subjects, response validation and transport classification |
| Create | entity ID, complete initial entity/triples, conflict is not success | existing entity returns classified conflict | exported graph request/response types | atomic KV Create |
| Reconcile | exact current KV revision and complete desired set for selected predicates | stale revision returns conflict; missing entity returns not found | `ExactEntity`, graph request types | CAS and selected-predicate replacement |
| Append | entity must already exist; duplicate exact tuple is unchanged | absent subject returns per-subject not-found | graph request/response types | grouping, tuple dedup and per-subject accounting |
| Delete | nonzero exact KV revision | stale/missing state is rejected | graph request types | revision-fenced KV delete |
| Commit ambiguity | unavailable differs from commit-unknown | automatic retry is not performed | `internal/graphmutation` is internal; exported projection errors expose commit state | one-attempt transport mapping |
| Projection contract | contract/group names, allowed predicates, stable metadata | local client rejects invalid mutation; direct wire callers bypass this layer | `pkg/projection` | local validation and metadata canonicalization |
| Exact remote read | GraphQL `entity { entity kvRevision }` | ordinary entity read exposes revision | GraphQL introspection and gateway docs | raw authority subject and KV decoding |
| Query capability | exact root-operation names supported by hand-written gateway | unsupported or misrouted query can reach unknown/unserved subject | gateway docs/introspection | NATS proxying and response classification |
| Service-manager triple scan | optional exact subject/predicate/object filters and a limit; low-throughput intent | route is always mounted; no NATS returns an empty success, fetch races are skipped, decode poison fails the request, and limits can stop before a complete scan | service-manager HTTP route and source comments; no graph-query capability declaration | catalog acquisition, entity decode and JSON response |
| Shared local view | caller supplies a bucket, validating decoder, lifecycle owner and subscriber policy | no `ENTITY_STATES` view exists merely because `pkg/graphview` is linked; current graph readers continue using their own watchers | `pkg/graphview` package docs and graph-view-subscription spec | single watcher, atomic snapshot/delta seam, poison, restart and backpressure |
| Revision-lag watermark | watcher owner must call Observe and Complete at the correct terminal seams | components that do nothing do not gain caught-up evidence; current sharing is graph-index/embedding only | `pkg/revlag` docs and derived-owner code | sparse-revision low-water calculation and commit-time pairing |
| Readiness | configured producer keys when using gateway readiness | empty key list disables readiness surface | graph-gateway config | watch lifecycle, freshness and envelope folding |
| Graphable merge | omission preserves existing predicates; RPC reconcile expresses now-zero | stream republish cannot remove an omitted predicate | graph-ingest docs/code; not represented in `Graphable` itself | CAS merge and logical version increment |

## 9. Pre-GS-01 assumption reconciliation

| Pre-GS-01 claim | Current state | Evidence classification |
|---|---|---|
| `ENTITY_STATES` is current authority with History 1 | true | catalog |
| graph-ingest is the sole production physical writer | true in current production search | code/search |
| eight mutation subjects exist | resolved/changed to four | protocol |
| ownership claims, presence, tokens, leases and service are active primitives | resolved by deletion | exact searches |
| referential stubs and pending-edge repair exist | resolved by deletion; one stale comment remains | exact searches |
| exact authority reads omit storage revision | resolved | `ExactEntity` |
| non-create mutation may auto-create | false in current code | reconcile/append/delete handlers |
| create conflict may be accepted through matching content | false in current projection/lifecycle code | code/search |
| mutation clients retry no-responders loops | retired from the old sites; current low-level client makes one request | code/search |
| Graphable and RPC existing-key writes can unconditionally overwrite | changed to CAS-based write paths | graph-ingest |
| Graphable and RPC create have identical semantics | false; hierarchy remains Graphable-only | graph-ingest |
| lifecycle history depends on KV History >1 | resolved/superseded by 64 current-value records | lifecycle/catalog |
| one general embedded client is canonical | false as policy, but aggregate client still exists provisionally | ADR-090/code |
| GraphQL is a conformant general graph gateway | false; current gateway is hand-written and query-only | gateway docs/code |
| MCP graph reads exist | false; current route is a placeholder | gateway docs/code |
| every derived owner shares one rebuild/readiness method | false | derived inventory |
| GRAPH_STATUS is the only component-health surface | false | readiness/diagnostic inventory |
| a SemStreams recovery subsystem is required | false | ADR-090/091 and code inventory |
| downstream parity is established | unknown; downstream repositories not inspected | audit boundary |

## 10. Evidence-checked issue reconciliation

| Issue | Current-code result |
|---|---|
| #892 `update_with_triples` revision metadata | old operation absent; premise superseded by removal |
| #888 unresolvable StorageInstance E2E coverage | confirmed: no intentional unresolvable-instance scenario found |
| #887 embedding persisted/runtime state asymmetry | confirmed: persisted statuses are pending/generated/failed; runtime terminal outcomes also include skipped/deleted |
| #886 relationship schema/runtime alignment | advertised schema confirmed; complete runtime-field comparison not completed |
| #885 spatialSearch scope/pagination | issue body not independently adjudicated beyond current facade inventory |
| #884 prefix pagination/truncation | confirmed: GraphQL advertises `[Entity]`; transport pagination/truncation is not exposed in that type |
| #883 GraphQL selection sets | confirmed by gateway package contract |
| #875 instance-blind fallback | confirmed: unresolved registry instance falls back to one configured `contentStore` |
| #874 no-responder retry loops | old loops absent; remaining `IsNoResponders` sites classify errors rather than retry ambiguous mutations |
| #872 concurrent lifecycle create E2E | confirmed absent; only unit coverage found |
| #871 content equality as ownership | named production sites no longer compare content to infer ownership |
| #870 lifecycle attach false 409 | named mechanism changed: revision mismatch now re-reads and distinguishes phase from unrelated contention |
| #869 request-scoped idempotency | absent by current accepted contract; RequestID is correlation |
| #868 general readiness type | type remains `IndexStatusResponse`, but non-index producers graph-ingest and rule already publish it |
| #861 concurrent lifecycle create | resolved in current unit behavior; targeted test green |
| #859 port interpretation drift | confirmed on graph-query and graph-gateway declarations |
| #851 authoritative revision | resolved through `ExactEntity` and GraphQL |
| #843 lifecycle history | premise changed; current E2E checks create plus two transitions from current-value records |
| #827 coordinated tag/wipe/reseed | operational event not verifiable from current code |
| #822 query-subject export | confirmed absent |
| #820 clustering GRAPH_STATUS producer | confirmed absent |
| #818 immutable birth predicates globally | confirmed absent globally; indexing profile and local projection contracts cover narrower cases |
| #802 principal-bearing envelope | absent and explicitly deferred; trigger conditions not audited |
| #798, #799, #800 | ownership-specific premises removed with ownership subsystem |
| #736 Docker package parallelism | runner remains uncapped; TestClient now uses log readiness, monitoring is opt-in, and cleanup/retry diagnostics exist |
| #703, #700 | ownership-specific premises removed |
| #695 web observation origin/contract | typed origin exists; no web-observation projection contract located |
| #694 agentrun projection contracts | exact reader exists; named product projection contract not located |
| #693 indexed predicate-family contracts | static local predicate groups exist; indexed-family derivation not located |
| #692 stable mutation provenance | enforced by `pkg/projection`; not enforced for direct canonical-client callers |
| #690 inference contract-bound mutation | raw old subject retired; inference uses canonical append, not `pkg/projection` |
| #689 gated-DAG claim CAS | exact-read plus revision-fenced reconcile exists; local projection contract absent |
| #688 rule add/remove | old subjects retired; direct `TripleMutator` path is canonical but not contract-bound |
| #681 `ENTITY_STATES` history | superseded by History 1 plus bounded occurrence records |
| #578 Graphable now-zero | confirmed: omission preserves prior predicate; explicit reconcile is a separate request lane |
| #571 aggregate query client watcher/latch | confirmed |
| #422 unused query API | no in-repo production consumers found for the named exports; downstream state unknown |
| #313 reusable owned-write helper | ownership premise obsolete; reusable local `projection.MutationClient` now exists |
| #260 metadata clear through `update_with_triples` | superseded by operation removal |
| #178 probe after already-exists | current contract deliberately returns conflict; no content-probe path exists |

## 11. Exhaustive open-issue title census

The 177 open issues divide into:

- 43 evidence-checked foundation candidates listed above
- 105 foundation-adjacent titles not adjudicated from their full bodies
- 29 non-foundation titles marked out of scope and unexamined

### 11.1 Foundation-adjacent, title-only, unexamined

#882 graph-gateway authentication/prefix authorization; #881 unreachable offloaded-body count; #862 seal
Discoverable; #860 crud-tools rule counters; #855 oversized community state loss; #854 lifecycle-gateway
authorization; #848 raw storage E2E lane; #844 ops false green; #839 graph payload ceilings; #837 oversized community
pass abort; #833 request-handler deadline propagation; #830 semantic E2E; #829 clustering ContentFetcher; #824
advertised lifecycle workflows; #821 lifecycle fresh-volume E2E; #819 globalSearch strategy wire; #811 false-green
E2E tasks; #810 stream/request subject collision; #795 readiness consumer front door; #786 phantom request ID; #785
canonical query decoder adoption; #784 unserved capabilities subject; #769 nightly E2E; #767 append replay coverage;
#766 storage-observability E2E; #759 JetStream ack disposition; #751 hierarchy inference asymmetry; #746 first-wins
companion predicate; #742 parked ObjectStore messages; #741 ObjectStore key collision; #739 offline
storage-observability proof; #738 cluster-aware ceiling; #735 stream-capacity circuit breaker; #725 hello-world
graph-index config; #710 community-summary GC; #701 multi-community expansion; #683 repeated structured values; #682
lifecycle-gateway 413; #680 OpenAPI security; #679 resolved gateway routes; #678 lifecycle allowlists; #677 atomic
operator field updates; #676 durable rule deadlines; #675 query-prefix matcher; #673 modified predicate; #672 clustering
cache invalidation; #671 predicate-audit classifications; #670 staged gated-DAG fan-out; #669 typed prefix
relationships; #668 gated-DAG predicate docs; #667 predicate-audit scanner limit; #661 unchanged community rewrites;
#652 LLM builder drift; #643 semantic cache-control E2E; #621 fusion/PathRAG truncation; #620 graph-core phantom
signals; #618 clustering readiness asymmetry; #609 GraphRAG readiness latch; #608 clustering LLM retry/level scan; #606
community hierarchy levels; #589 dead inference watch; #588 shared community/embedding views; #587 message-logger
graphview; #586 watcher limits; #579 shared graph view; #560 rule deletion cleanup; #527 composite-key retention; #526
graph-index resource isolation; #525 graph-index change detection; #498 lifecycle batch round-trips; #486 stale fan-out
docs; #472 message-logger filter ordering; #469 integration WebSocket flake; #465 clustering adaptive synthesis; #436
gated-DAG hierarchy entities; #424 rule watcher shutdown; #421 readiness-aware clients; #391 fusion E2E; #382
gated-DAG reset/requeue; #376 deterministic fusion; #367 projection provenance docs; #348 GraphRAG error collapse; #347
research graph phase 2; #340 raw/current projection guidance; #330 predicate value-filter coverage; #323
agentic-dispatch dead KV mapper; #315 capabilities route alignment; #309 port backpressure metrics; #306 prefix response
bounds; #301 crud-tools failure; #236 structural ranking; #219 SHACL fixture; #211 MCP graph server; #176 bulk-read
design; #145 chain URL cache; #867 component-start barrier; #857 payload-sized framework writes; #828 stale ADR-068
reasoning; #823 globalSearch defaults; #765 stale rule WatchAll docs; #753 sister cutover tracking; #633 orphaned blob
GC; #619 process-local BM25; #603 impact facet; #576 zero confidence representation.

### 11.2 Out of scope and unexamined

#877 trajectory contract; #876 trajectory retention; #873 unreachable trajectory objects; #866 cancellation payload
mismatch; #865 successful-loop outcome mismatch; #842 discovery subject move; #808 approval policy; #807 TaskID claims;
#764 dispatcher stats; #734 schema type fallback; #659 number schema generation; #582 lesson promotion endpoint; #553
empty tool set; #546 vocabulary TryRegister; #477 agent-loop completion watcher; #457 retired reactive-engine docs; #453
heartbeat/ack validation; #384 loop timeout; #383 rule iteration naming; #320 approval-response publish; #230
dependency-coordinate skill; #223 `@latest` task references; #212 CCO alignment; #194 Gemini thought signature; #144
sandbox egress policy; #143 bash output pagination; #142 warm sandbox docs; #141 chain worktree; #26 ADR-032 program.

## 12. Inventory summary

GS-01 materially changed the current system:

- semantic ownership, stubs and the eight-subject mutation surface are gone
- one four-operation typed mutation port and exact authority revision now exist
- existing-key authority writes are CAS-shaped
- lifecycle history no longer depends on KV history
- GraphQL exposes the exact entity revision
- no recovery or CQRS runtime is present

The remaining current complexity is concentrated in read and derived-state plurality:

- exact, direct-KV, aggregate and coordinator read paths coexist
- the always-mounted service-manager triple scan adds a separately bounded direct-authority read
- port declarations, runtime subscriptions, routers and gateway schemas describe different query surfaces
- `pkg/graphview` and `pkg/revlag` share selected mechanics, but derived owners still use different rebuild, repair and
  readiness contracts
- GRAPH_STATUS, COMPONENT_STATUS and in-process health remain distinct but overlapping operational concepts
- local projection contracts coexist with direct canonical mutation callers
- the aggregate embedded graph client remains present beside the narrow exact adapter
- the canonical rule reconcile spec still contradicts the active GS-01 delta and merged one-attempt runtime

These are current-state observations only; no successor increment, ordering or target state is implied.


<!-- End accepted inventory. -->

## Part II — Pre-owner design

# Post-GS-01 graph read and derived-state foundation

## Status

**PRE-OWNER DESIGN — NOT APPROVED**

This design is based on:

- merged baseline `d1570ef81b23096021af0d7bf3321b4c08c7e54b`;
- the accepted 629-line inventory
  `docs/proposals/post-gs01-graph-state-reality-audit.md`, SHA-256
  `869be8fdfaef9c141dd7697071da0ff9fb5ffa1c4e3fbb5863837b25fb3be4ba`;
- its independent `INVENTORY PASS` review; and
- the accepted architecture boundaries in ADR-090 and ADR-091.

The reviewed inventory is Part I of this artifact and is inserted byte-for-byte above this design. Its surface inventory
and §8 adopter-seam inventory are accepted verbatim; this draft does not amend or summarize them into different factual
claims.

The prior GS-03 through GS-14 roadmap is invalid. Nothing below inherits its order, increment names, issue assignment,
or implementation assumptions.

## 1. Problem statement

GS-01 removed the system’s largest false abstractions:

- semantic ownership;
- claims, leases, presence, tokens, and arbitration;
- referential stubs and pending-edge repair;
- eight overlapping mutation subjects;
- value-only exact reads; and
- unconditional existing-key writes.

The remaining foundation problem is narrower.

SemStreams still has several competing ways to read current graph state, declare graph queries, maintain derived views,
report graph capability health, and validate component-originated mutations. Those implementations do not share one
explicit contract, even when they model the same fact.

The target is not stronger consistency. SemStreams remains an offline-first, edge-capable, tiered semantic graph
framework with eventual consistency:

- `ENTITY_STATES` is current authority;
- graph-ingest remains its physical writer;
- authority mutations use the existing NATS request/reply port and CAS;
- valid unresolved references remain ordinary graph state;
- derived views may lag;
- healthy stale results may serve with honest currency;
- unsound partial results must not present themselves as complete;
- transport loss, poison, and ordinary lag remain distinct;
- no event store, CQRS runtime, exactly-once subsystem, ownership system, recovery product, autostub workflow, or
  compatibility layer is introduced.

The objective is fewer concepts, fewer front doors, and predictable behavior.

## 2. Measured premises

| Premise | Measurement |
|---|---|
| `ENTITY_STATES` is the sole authoritative graph bucket, History 1, physically written by graph-ingest | Accepted inventory §§2.1, 12 |
| One four-operation typed request/reply mutation port exists and its client sends once | §§2.2–2.4 |
| Exact reads expose entity and same-entry KV revision | §3.1 |
| General embedded, direct-KV, exact-adapter, coordinator, GraphQL-shaped, and service-manager read fronts coexist | §§3.2–3.6, 7 |
| Graph-query runtime, declared ports, router, gateway wildcard port, and gateway schema enumerate different capabilities | §§3.4–3.5, 7 |
| The current gateway is not a conformant GraphQL executor | §3.5 |
| `/mcp` is a placeholder, not a graph tool contract | §3.5 |
| `/graph/triples` is always mounted and independently scans authority | §3.6 |
| `graph/query.Client` is a provisional general mixed KV/RPC client | §3.3 |
| Derived owners have materially different lifecycle shapes | §4 |
| `pkg/graphview` has one current non-graph consumer and no `ENTITY_STATES` consumer | §4.1 |
| `pkg/revlag` is shared by graph-index and graph-embedding only | §4.1 |
| `GRAPH_STATUS` has four current producer keys; catalog prose names only two | §5.1 |
| `COMPONENT_STATUS` has 24 production writers and zero production readers, excluding E2E | §5.2 |
| Projection-bound and direct canonical mutation callers coexist | §6 |
| The canonical rule spec contradicts the merged one-attempt runtime and accepted GS-01 delta | §6.1 |
| Downstream usage is unmeasured | §§1, 9 |
| The issue queue contains evidence of these collisions but does not establish a program order | §§10–11 |

These premises are sufficient for design. Remaining uncertainty is expressed as proof gates, not filled with assumptions.

## 3. Decision-skill outcomes

### 3.1 Query-pattern

Applied to the graph read fronts:

- external applications and web clients use a conformant GraphQL gateway;
- internal framework services use NATS request/reply only behind operation-specific typed adapters and component ports;
- raw KV remains an owner/operator seam;
- there is no general embedded graph client;
- MCP is absent until a real bounded graph-tool consumer and auditable tool contract exist.

All paths observe the same eventually consistent graph. Access protocol does not promise stronger consistency.

The skill’s references to the invalidated GS-03 through GS-12 sequence are stale guidance and do not establish order.

### 3.2 KV-or-stream

Applied to current state, status, and derived maintenance:

- `ENTITY_STATES` is a fact and remains KV;
- current derived rows and capability status are facts and remain KV where durability or cross-component observation is
  justified;
- authority-change fan-out remains KV Watch;
- mutation remains the accepted core NATS request/reply command path because it needs an immediate classified outcome
  and caller-controlled retry; no mutation stream is added;
- failed derived work is re-derived from current authority by the owning component. No general repair stream, event
  ledger, or replay service is introduced.

`pkg/revlag` applies only where one ordered authority-revision space and terminal per-revision work exist.
`pkg/graphview` applies only where several in-process readers genuinely benefit from one validated current-state
watcher.

## 4. Options considered

### Option A — Preserve the current plurality

Keep every reader, status store, declaration, and derived-owner lifecycle as-is. Address individual defects locally.

Cost:

- lowest immediate implementation cost;
- continued port/schema/router drift;
- continued confusion about which read surface is authoritative;
- every index owner continues inventing lifecycle behavior;
- unused and placeholder surfaces remain adopter-visible;
- issue-by-issue patches continue to increase framework knowledge.

This does not satisfy the stated foundation goal.

### Option B — Selective normalization and deletion

Retain the successful GS-01 authority substrate. Normalize only facts currently represented more than once, delete
unjustified front doors, and specify behavioral obligations without building a general read-model runtime.

Cost:

- breaking removal of several current surfaces;
- substantial GraphQL gateway work;
- migration of internal callers to narrow adapters;
- conformance work across every derived query owner;
- careful status cleanup.

Benefit:

- one external graph API;
- component ports remain the internal API contract;
- one mutation substrate;
- fewer concepts and packages;
- owner-specific algorithms remain possible;
- no new bucket, stream, service, general runtime, or adopter knob.

### Option C — Build a general derived-view and query runtime

Extend `pkg/graphview`, `pkg/revlag`, query routing, repair, status, and persistence into one substrate used by every
graph owner.

Cost:

- spatial, temporal, embedding, clustering, and graph-index do not currently share the same work or revision model;
- `graphview` is in-process while several owners materialize durable outputs;
- clustering is periodic and whole-result based;
- embedding is multi-stage and can strand work;
- the abstraction would need hooks for almost every current exception;
- this violates ADR-090’s three-owner/reduced-code gate until measured otherwise.

This is rejected unless the proof gates falsify Option B.

### Option D — Collapse derived queries onto authority-time computation

Delete most durable indexes and compute queries from `ENTITY_STATES` snapshots or in-memory views.

Cost:

- conceptually small;
- potentially suitable for very small edge deployments;
- unbounded scans and repeated computation threaten latency, memory, and tiered semantic capabilities;
- spatial, semantic, traversal, and community operations would lose their current serving characteristics;
- the reviewed inventory contains no performance evidence proving this is viable as the framework default.

This remains a possible deployment profile or later simplification, not the recommended general foundation.

## 5. Recommendation

Adopt **Option B: selective normalization and deletion**.

The recommendation is falsifiable. Option C becomes justified only if at least three surviving owners require the same
lifecycle mechanics and a prototype measurably reduces total production code and adopter knowledge. Option D becomes
viable only if representative edge and connected workloads meet explicit latency and resource bounds without the
durable views.

## 6. Target read and acquisition contract

### 6.1 Catalog acquisition contract

Do not introduce a graph-source injector, source capability, registry, or runtime abstraction. Current component
dependencies cannot carry one: port declarations are composition metadata, and flowgraph edges do not supply bucket
handles.

Use the existing catalog seams directly:

- only the declared bucket owner calls `graph.EnsureCatalogBucket`;
- every reader calls `graph.OpenCatalogBucket` inside its own `Start` or lazy-open lifecycle;
- `OpenCatalogBucket` never creates or reconciles the bucket and reports an absent owner bucket as classified
  not-ready: `graph/kvcatalog.go:214-241`;
- no reader calls generic `GetKeyValueBucket`, `js.KeyValue(name)`, or an owner ensure/create helper;
- each consumer stores the returned handle behind a package-local minimal read interface exposing only the `Get`,
  `ListKeys`, `Watch`, or `WatchAll` methods that consumer needs. This is local compile-time narrowing, not a shared
  framework abstraction;
- the full `jetstream.KeyValue` handle does not escape that package's acquisition function;
- the existing owner-only catalog classification remains the write-policy guard: `graph/kvcatalog.go:174-200`;
- production searches and tests must continue to prove that graph-ingest is the only `ENTITY_STATES` writer.

`kv-watch` port metadata declares that a component consumes authority. It does not acquire or inject the source.

### 6.2 Surviving consumer disposition

| Consumer | Target acquisition and lifetime |
|---|---|
| graph-ingest | Calls `EnsureCatalogBucket` as the sole owner and retains the writable handle for component lifetime. |
| graph-index | Declares its `ENTITY_STATES` `kv-watch` input; calls `OpenCatalogBucket` during Start; owns and stops its watcher. |
| graph-index-spatial | Same declared reactive-consumer shape; package-local catalog open and watcher lifetime. |
| graph-index-temporal | Same declared reactive-consumer shape. |
| graph-embedding | Same declared reactive-consumer shape. |
| graph-clustering | Declares snapshot/poll consumption; calls `OpenCatalogBucket` for its owner-specific periodic enumeration. It does not claim an active watcher. |
| rule | Declares reactive authority consumption for configured entity patterns; opens through `OpenCatalogBucket`. Zero patterns means no bucket open or watch. |
| lifecycle | Remains a declared reactive authority consumer. Exact point reads stay on `graph.ExactEntityReader`; List/Watch/WatchEvents open `ENTITY_STATES` through `OpenCatalogBucket`. Replace the generic call at `pkg/lifecycle/manager.go:222-239`; do not convert enumeration/watch into request/reply loops. |
| lifecycle dedicated full-graph guard | Delete and deliberately localize poison. Lifecycle performs no whole-graph admission scan. Exact operations validate the entity they touch; List validates its requested workflow scope; a malformed watched entity is not acted upon and degrades only that subscription/workflow observation while unrelated lifecycle work continues. Current Manager-wide fail-closed guard: `pkg/lifecycle/manager_query.go:208-285`. |
| gated-DAG | Remains a declared reactive authority consumer. Its arbitrary `UnitEntityPrefix` is not a lifecycle workflow pattern, so it retains its own prefix watch. Declare the `ENTITY_STATES` dependency as an input port, open with `OpenCatalogBucket`, watch `UnitEntityPrefix + ".>"`, and own the watcher until component stop. Current distinct prefix semantics: `processor/gated-dag/config.go:75-79`; current watch: `processor/gated-dag/executor.go:355-386`; current missing declaration: `processor/gated-dag/component.go:272-274`. Do not create a shared prefix-watch adapter for this one consumer. |
| agentic graph tools | Delete their direct KV acquisition and use admitted typed query adapters. |
| `graph/query.Client` | Delete. |
| service-manager `/graph/triples` | Delete. |
| message logger/operator diagnostic | Explicit operator-only catalog open with a package-local read/watch interface. Never an application graph API. |
| E2E storage-contract probe | May catalog-open explicitly when the assertion is about storage. Behavioral tests use GraphQL or typed operations. |

Deleting lifecycle's full-graph guard is an observable poison-policy change, not acquisition cleanup. Today one
malformed entity anywhere latches the entire Manager fail-closed. In the target, a malformed unrelated entity does not
block a valid exact operation, transition, list, or watch outside the affected scope. Touching malformed state retains
the existing typed poison/reset-required failure. A matching watch never emits or mutates from the malformed entity;
it reports the entity/revision through bounded structured metrics and logs and terminates or degrades that
subscription without poisoning unrelated workflows. Graph-index and other whole-authority derived owners retain their
own global poison observations. These diagnostics are observations, never a new global lifecycle gate.

### 6.3 External and internal fronts

Remote graph reads use a conformant GraphQL component:

- real parsing and execution;
- schema validation and selection projection;
- conformant introspection and errors;
- explicit query-only scope unless a later external mutation contract is independently approved.

The executor is `github.com/99designs/gqlgen`: schema-first SDL, reproducible generated type-safe execution code, and
thin authored resolvers that delegate only to canonical typed graph operations. SemStreams does not hand-write a
GraphQL parser, executor, introspection engine, selection projector, or response engine. Mutations, subscriptions,
playground routes, and generated application policy remain absent. Replacing gqlgen or adding handwritten execution
requires a new owner ruling; it is not an implementation-level substitution.

Internal components use NATS request/reply only through the typed operation-specific adapters below. Raw subjects are
provider transport, not component-author contracts.

There is no general embedded graph client, always-mounted service graph endpoint, or placeholder MCP route.

### 6.4 One canonical operation catalog

The canonical coordinator catalog contains exactly the following 20 operations. The original 15 registrations are at
`processor/graph-query/query.go:30-51`; `localSearch` is separately registered at
`processor/graph-query/graphrag.go:219-226`; the four predicate operations currently bypass the coordinator at
`gateway/graph-gateway/component.go:922-934`.

The catalog is internal typed declaration data consumed by handler registration, component-port declaration, provider
routing, and GraphQL resolver registration. It is not a general client or public subject registry.

| Canonical operation | Request family | Response family | Visibility | Provider binding | GraphQL binding |
|---|---|---|---|---|---|
| `graph.query.entity` | validated canonical six-part entity ID | `graph.ExactEntity` | Public + typed internal | ingest exact | `entity` |
| `graph.query.entityByAlias` | typed exact `{alias}` | singular `graph.AliasedExactEntity`; typed `entity_not_found` or `alias_ambiguous` otherwise; every semantic outcome carries alias-view coverage | Public | index exact-alias resolution + ingest exact composite | `entityByAlias` |
| `graph.query.batch` | entity ID list | entities plus complete missing/fault report | Internal typed only | ingest batch | none |
| `graph.query.relationships` | entity ID, direction, predicate filter | incoming/outgoing relationship set | Public + typed internal | index incoming/outgoing composite | `relationships` |
| `graph.query.pathSearch` | start ID, direction, predicates, bounds | bounded path result | Public | internal PathRAG composite | `pathSearch` |
| `graph.query.hierarchyStats` | canonical entity prefix | `HierarchyStatsResult` complete observed traversal | Public | cursor-exhausted ingest-prefix composite | `entityIdHierarchy` |
| `graph.query.prefix` | ID prefix, page request | canonical entity page | Public + typed internal | ingest prefix | `entitiesByPrefix` |
| `graph.query.spatial` | bounds and limit | spatial entity result | Public + typed internal | spatial bounds | `spatialSearch` |
| `graph.query.temporal` | time range and limit | temporal entity result | Public + typed internal | temporal range | `temporalSearch` |
| `graph.query.semantic` | text, scope, limit | scored semantic result | Public + typed internal | embedding search | semantic/text/similarity search roots |
| `graph.query.similar` | entity ID and limit | scored similar entities | Public | embedding similar | `findSimilar` |
| `graph.query.globalSearch` | query and bounded search options | global GraphRAG result | Public | graph-query composite | `globalSearch` |
| `graph.query.summary` | bounded graph scope | graph summary result | Public + typed internal | prefix + predicate-list composite | `graphSummary` |
| `graph.query.searchGraph` | query and bounded search options | search answer/evidence result | Public + typed internal | global search + semantic fallback | `searchGraph` |
| `graph.query.byName` | name and limit | ranked entity IDs | Internal typed only | index by-name | none |
| `graph.query.localSearch` | anchor entity, query, level | local GraphRAG result | Public | graph-query composite | `localSearch` |
| `graph.query.predicate` | exact canonical predicate, filters, limit | current predicate memberships | Public | index predicate | `entitiesByPredicate` |
| `graph.query.predicateList` | namespace/list request | current predicate identities | Public + typed internal | index predicate-list | `predicates` |
| `graph.query.predicateStats` | exact predicate/stat request | bounded predicate statistics | Public | index predicate-stats | `predicateStats` |
| `graph.query.predicateCompound` | bounded compound predicate expression | matching entity memberships | Public | index predicate-compound | `compoundPredicateQuery` |

Binding rules:

- gateway resolvers reference only canonical operations;
- graph-query registers every catalog entry and declares matching typed ports;
- internal-only operations have current typed consumers: batch—fusion/research-graph; by-name—fusion;
- provider subjects remain internal transport and are not separately admitted;
- direct gateway routing to `graph.index.query.predicate*` is deleted;
- `graph.query.capabilities` and `graph.query.unknown` are deleted;
- GraphQL introspection replaces the phantom capabilities route;
- GraphQL validation replaces the unknown-subject fallback;
- `agentic.query.trajectory` remains outside the graph catalog as an agentic operation.

#### Hierarchy statistics are complete-or-error

Delete the ignored public `limit` argument. The caller supplies only a prefix; the framework owns page size and the
aggregate resource budget.

```go
type HierarchyStatsRequest struct {
    Prefix string `json:"prefix"`
}

type HierarchyStatsResult struct {
    Prefix              string           `json:"prefix"`
    ObservedEntityCount int              `json:"observedEntityCount"`
    Children            []HierarchyChild `json:"children"`
}

type HierarchyChild struct {
    Prefix              string `json:"prefix"`
    Name                string `json:"name"`
    ObservedEntityCount int    `json:"observedEntityCount"`
}
```

GraphQL exposes `entityIdHierarchy(prefix: String!): HierarchyResult!`, with `prefix`, `observedEntityCount`, and
`children`; each child exposes `prefix`, `name`, and `observedEntityCount`. Delete `totalEntities`, child `count`, and
the ignored `limit` with no shim. A result exists only after cursor exhaustion, so no `scanComplete` field is needed.
Pages scanned are telemetry, not an adopter-facing field.

The graph-query coordinator:

1. validates the prefix once, starts with an empty cursor, and requests pages at no more than
   `graph.MaxPrefixQueryLimit`;
2. uses `RequestClassified`, decodes `graph.PrefixQueryResponse`, and validates every entity before accepting a page;
3. counts each canonical entity ID at most once using a defensive `seenIDs` set and aggregates children only from newly
   observed IDs;
4. succeeds only when `NextCursor` is empty;
5. fails transient/internal, without partial data, on a repeated or non-advancing cursor or an empty page carrying a
   continuation cursor;
6. preserves classified page failures and returns no partial result on decode, validation, cancellation, or timeout;
7. enforces a framework-owned `MaxHierarchyScanEntities = 10_000` unique-ID budget. Exactly 10,000 with an exhausted
   cursor succeeds; 10,000 with a continuation returns invalid/resource-exhausted and no result. The caller narrows the
   prefix rather than predicting a page size; and
8. uses the incoming operation context/deadline for the entire loop, never a fresh full timeout per page. #833 remains
   the separate cross-cutting deadline-propagation issue.

A successful hierarchy result is a complete traversal of the pages the provider exposed until cursor exhaustion. It
is not an atomic multi-key snapshot. `ENTITY_STATES` remains eventually consistent during the scan: a concurrent
insertion before the cursor may be absent, a concurrent deletion may disappear, and duplicates are discarded. The
result reports the unique entity IDs actually observed, hence `observedEntityCount`.

#### Entity identity is canonical and explicit

`graph.query.entity` accepts one validated six-part entity ID and performs one exact authority read. It never treats a
malformed or shorter value as an alias, suffix, prefix, or search term. `entityByAlias` accepts only typed `{alias}`. It
never accepts `aliasOrID`, forwards alias text as an entity ID, or falls back to canonical-ID interpretation. The
internal alias provider returns exactly one of `absent`, `singular`, or `ambiguous`. The composite hydrates only
`singular`; it maps `absent` to `entity_not_found` and `ambiguous` to `alias_ambiguous`, with no entity payload.
Every one of those three semantic outcomes carries the alias view's captured coverage. Provider transport, decode, and
readiness failures propagate and never become absence or search fallback.

Discovery belongs to prefix, name, semantic/global search, and exact alias operations. Discovery returns candidates and
never silently chooses among suffix collisions.

A GraphRAG/path classifier reference is used only when it is already a canonical ID or resolves to a `singular` exact
alias. `absent` reports not-handled and may fall through to discovery. `ambiguous` and provider failure are classified
failures: neither permits candidate selection or search fallback. The classifier never invents identity from partial ID
segments.

### 6.5 Direct provider operation disposition

Provider registrations are measured at graph-ingest `processor/graph-ingest/query.go:27-55`, graph-index
`processor/graph-index/query.go:20-101`, spatial `processor/graph-index-spatial/query.go:18-40`, temporal
`processor/graph-index-temporal/query.go:16-25`, embedding `processor/graph-embedding/query.go:18-42`, and clustering
`processor/graph-clustering/query.go:18-53`.

Every retained provider row binds only to a canonical operation in §6.4:

| Current provider subject | Present production consumer | Target disposition |
|---|---|---|
| `graph.ingest.query.entity` | exact adapter; graph-query | Keep internal provider binding for `graph.query.entity`. |
| `graph.ingest.query.batch` | graph-query | Keep internal provider binding for `graph.query.batch`. |
| `graph.ingest.query.prefix` | graph-query; agentic-loop; gated-DAG | Keep internal provider binding for `graph.query.prefix`; raw consumers move to the typed canonical operation. |
| `graph.ingest.query.suffix` | graph-query partial-ID resolver only | Delete with `ENTITY_SUFFIX_INDEX`; suffix fragments are not identities. |
| `graph.index.query.outgoing` | graph-query relationships/PathRAG | Keep internal provider binding for `graph.query.relationships` and `graph.query.pathSearch`. |
| `graph.index.query.incoming` | graph-query relationships/PathRAG | Keep internal provider binding for `graph.query.relationships` and `graph.query.pathSearch`. |
| `graph.index.query.alias` | graph-query alias resolution | Keep as the internal collision-safe exact-alias provider for `graph.query.entityByAlias`; it returns absent/singular/ambiguous and never chooses an arbitrary entity. |
| `graph.index.query.predicate` | gateway direct route; provisional aggregate client | Keep internal provider binding for `graph.query.predicate`; delete direct gateway and aggregate-client use. |
| `graph.index.query.predicateList` | gateway; graph-query summary; aggregate client | Keep internal provider binding for `graph.query.predicateList` and `graph.query.summary`. |
| `graph.index.query.predicateStats` | gateway; aggregate client | Keep internal provider binding for `graph.query.predicateStats`. |
| `graph.index.query.predicateCompound` | gateway; aggregate client | Keep internal provider binding for `graph.query.predicateCompound`. |
| `graph.index.query.byName` | graph-query | Keep internal provider binding for `graph.query.byName`. |
| `graph.spatial.query.bounds` | graph-query | Keep internal provider binding for `graph.query.spatial`. |
| `graph.spatial.query.polygon` | no in-repository production consumer found | Delete. Downstream unknown remains a holdout migration finding, not a reason to preserve the subject. |
| `graph.temporal.query.range` | graph-query | Keep internal provider binding for `graph.query.temporal`. |
| `graph.embedding.query.search` | graph-query/GraphRAG | Keep internal provider binding for `graph.query.semantic` and graph-query composites. |
| `graph.embedding.query.similar` | graph-query; clustering | Keep internal provider binding for `graph.query.similar`; clustering uses a narrow semantic-similarity adapter. |
| `graph.embedding.query.status` | no production consumer; `GRAPH_STATUS` already carries status | Delete. |
| `graph.clustering.query.entity` | graph-query GraphRAG | Keep internal provider binding for graph-query composites. |
| `graph.clustering.query.community` | router declaration only; no production request found | Delete. |
| `graph.clustering.query.members` | no production consumer found | Delete. |
| `graph.clustering.query.level` | no production consumer found | Delete. |
| `graph.anomalies.query.detect` | static router entry; no registered producer found | Delete. |

### 6.6 Gateway-only and phantom disposition

Gateway direct routing is at `gateway/graph-gateway/component.go:835-941`; current tests identify unserved subjects at
`gateway/graph-gateway/response_shape_test.go:219-231`.

| Current gateway route | Target |
|---|---|
| direct `graph.index.query.predicate*` routes | Replace with the four canonical predicate operations in §6.4. |
| `graph.query.capabilities` | Delete. No producer exists. GraphQL introspection is remote capability discovery. |
| `graph.query.unknown` | Delete as a wire subject. GraphQL validation returns an ordinary unknown-field error before NATS. |
| `agentic.query.trajectory` | Keep outside the graph-operation catalog as an explicitly typed agentic resolver. This design does not turn it into a graph query. |
| `/mcp` placeholder | Delete. |

## 7. Derived-owner contract

ADR-090’s role-specific classification remains controlling. The framework does not impose one implementation.

Every component that serves or persists a derived graph capability must nevertheless satisfy these behavioral
obligations:

1. **Declare the role.** Required query view, optional enrichment, internal accelerator, deduplication/reverse
   bookkeeping, reactive consumer, or serving cache.
2. **Name current authority.** Identify the current fact set from which the output can be reconstructed.
3. **Define desired state.** Reprocessing the same authoritative state must converge without accumulating duplicate
   semantic output.
4. **Handle retraction.** Source deletion and predicate removal have an explicit cleanup outcome.
5. **Define bootstrap completion.** Partial initial replay cannot be presented as a complete current view.
6. **Define live-update behavior.** Watch loss, periodic recomputation, or other feed interruption is distinguishable
   from ordinary lag.
7. **Define failed-work behavior.** A failed required update remains visible and can be redriven from current authority.
   Replay of an old command log is not required.
8. **Define poison scope.** Canonical authority poison and transport failure retain their existing distinct
   classifications.
9. **Define serving honesty.** A healthy stale view may serve with currency. A known-incomplete or poisoned view cannot
   claim completeness.
10. **Define observability.** The owner exposes only the readiness, failure, and currency facts required by a present
    consumer.

These are specification and conformance obligations, not a new Go interface or shared runtime.

### 7.1 `pkg/graphview`

Use it when:

- one KV bucket is current state;
- several in-process consumers need the same validated snapshot and live tail;
- coalesced latest-value deltas are correct;
- an in-memory projection is sufficient.

Do not use it as:

- a durable projection writer;
- a work queue;
- a multi-source causal join;
- a historical per-revision delivery mechanism; or
- a universal index runtime.

No graph owner migrates merely to create a consumer for the package.

### 7.2 `pkg/revlag`

Use it when:

- work is observed in one comparable authority-revision space;
- every observed revision reaches a terminal applied/failed outcome; and
- a low-water coverage statement is meaningful.

Do not force it onto periodic whole recomputation, multi-source work, or backlog units that are not KV revisions.

### 7.3 Owner-specific repair remains valid

Graph-index’s repair loop, embedding’s multi-stage repair, spatial and temporal bootstrap gates, and clustering’s
periodic recomputation need not become identical.

Shared mechanics are extracted only when the three-owner/reduced-code gate is proven. Similar vocabulary alone is not
sufficient.

### 7.4 Exact-alias membership remains owner-specific

`entityByAlias` remains a graph-index-owned exact derived view. It does not become a universal alias subsystem.

The current raw `alias -> entity ID` layout is replaced inside the existing `ALIAS_INDEX` bucket by two versioned
membership axes:

- lookup: `a2.<sha256(alias UTF-8)>.<sha256(canonical entity ID)>`; and
- owner: `e2.<sha256(canonical entity ID)>.<sha256(alias UTF-8)>`.

Both rows retain the original exact alias and canonical entity ID. Hashes are internal storage encoding only: callers
never validate or predict NATS key grammar. Reads and writes recompute both hashes and compare the retained originals.
A conflicting value at the same hashed key is a required derived-view failure and is never overwritten or skipped.
Aliases are exact Unicode strings; the framework does no trimming, case folding, normalization, suffix matching, or
other identity guessing.

For each authoritative entity revision, graph-index derives the complete deduplicated exact-alias set and reconciles it
against that entity's `e2` owner rows. Desired memberships write or verify the owner row before the lookup row. Removed
memberships delete the lookup row before the owner row. Entity deletion reconciles to the empty set. Cold bootstrap
reconciles both axes against the complete current-authority alias projection: it removes stale or orphaned rows,
restores missing pairs, and verifies every retained pair before declaring completion.

This ordering makes interruption repairable. During ordinary healthy lag, an eventually consistent read may still see
the prior singular membership, temporary absence, or both old and new memberships as ambiguity. It may not choose one
of multiple memberships. A failed write, retraction, validation, or bootstrap repair keeps the work item failed,
prevents the graph-index coverage watermark from passing that authority revision, and leaves `RevisionViewStatus`
building or degraded. Alias queries use the normal index-not-ready failure when required work is known incomplete;
repair always re-derives from current authority.

Exact lookup scans only the `a2.<alias-hash>.` membership prefix, validates each retained alias and entity ID, and
deduplicates canonical IDs. It returns:

- `absent` when an exhausted lookup observes no exact membership;
- `singular` with one `canonical_id` when the exhausted lookup observes exactly one distinct entity; or
- `ambiguous` with no `canonical_id` as soon as it observes two distinct exact memberships.

The provider response therefore needs only state plus the optional singular ID:

```go
type AliasResolutionState string

const (
    AliasAbsent    AliasResolutionState = "absent"
    AliasSingular  AliasResolutionState = "singular"
    AliasAmbiguous AliasResolutionState = "ambiguous"
)

type AliasData struct {
    State                       AliasResolutionState `json:"state"`
    CanonicalID                 *string              `json:"canonical_id,omitempty"`
    AliasCoveredThroughRevision uint64               `json:"alias_covered_through_revision"`
}
```

After the normal fail-closed readiness check, graph-index captures its greatest contiguous completed `ENTITY_STATES`
revision immediately before beginning the membership scan. It returns that value unchanged as
`alias_covered_through_revision` for absent, singular, and ambiguous outcomes. The value means that every required alias
reconciliation through that authority revision completed before the scan began. It is a lower-bound currency statement,
not a point-in-time snapshot, an upper bound, or the hydrated entity's revision; the scan may observe memberships from
later revisions. Capturing coverage after the scan is prohibited because that could claim work the scan did not observe.
If graph-index cannot obtain valid contiguous coverage, it returns typed `index_not_ready` and does not invent zero.

On singular resolution, the coordinator preserves that captured value through exact authority hydration and returns an
operation-specific public result:

```go
type AliasedExactEntity struct {
    Entity                      *EntityState `json:"entity"`
    KVRevision                  uint64       `json:"kvRevision"`
    AliasCoveredThroughRevision uint64       `json:"aliasCoveredThroughRevision"`
}
```

`KVRevision` is the hydrated current authority entry's revision;
`AliasCoveredThroughRevision` is the alias view's lower-bound coverage. They are deliberately distinct. Absent and
ambiguous mappings carry `alias_covered_through_revision` in classified error detail, and GraphQL preserves it as
`extensions.aliasCoveredThroughRevision`. Provider, decode, readiness, and hydration failures keep their original
classification and do not fabricate alias currency.

Candidate lists, counts, and collision-selection policy are deliberately not public surface. A singular result describes
the current derived view with the captured lower bound; it is not a linearizable assertion about a concurrently changing
authority. This metadata is specific to alias resolution, not a general response envelope or metadata framework.

## 8. `GRAPH_STATUS` remains, but its values become role-typed

Process health remains separate: service-manager and component `Health` surfaces describe process-local lifecycle,
liveness, and the ability to accept work. They do not promise graph coverage or freshness.

ADR-088 records two real non-read consumers that GS-01 does not make obsolete: semdragon must not take its parity
snapshot while graph-ingest still has accepted asynchronous work, and SemMachina must not assume the rule processor's
runtime-mutable watcher set has replayed. This repository's E2E entity stage exercises both through `FullyCovered`.
The error is the shared `IndexStatusResponse`, not the producer keys.

Keep one producer-owned KV key and the existing 5-second heartbeat and 15-second freshness window:

| Key | Concrete value | Named decisions |
|---|---|---|
| `graph-index` | `RevisionViewStatus` (`kind=revision_view`) | graph query/fusion/clustering health and revision coverage; E2E snapshot settlement |
| `graph-embedding` | `RevisionViewStatus` (`kind=revision_view`) | semantic clustering health and revision coverage |
| `graph-ingest` | `IngestBacklogStatus` (`kind=ingest_backlog`) | semdragon parity snapshot; E2E snapshot settlement |
| `rule` | `RuleReplayStatus` (`kind=rule_replay`) | SemMachina watcher-replay wait; E2E snapshot settlement |

Spatial, temporal, and clustering receive no key without a named cross-component decision. Their query handlers return
typed local availability.

The key fixes the allowed concrete type. Every value also carries its required constant `kind`; a key/kind mismatch is
a decode failure, not a zero-valued status. There is no stored `StatusEnvelope` union and no concrete type contains
fields belonging to another role.

### 8.1 Revision-view status

```go
type RevisionViewStatus struct {
    Kind              StatusKind      `json:"kind"` // revision_view
    State             ViewState       `json:"state"` // building|serving|degraded|reset_required
    BootstrapComplete bool            `json:"bootstrap_complete"`
    IndexedRevision   *uint64         `json:"indexed_revision"`
    TargetRevision    *uint64         `json:"target_revision"`
    CoveredAt         *time.Time      `json:"covered_at,omitempty"`
    ObservedAt        time.Time       `json:"observed_at"`
    Problem           *StatusProblem  `json:"problem,omitempty"`
    Failure           *FailureSummary `json:"failure,omitempty"`
}
```

Zero is a valid revision, so unknown revisions are `nil`, not zero. `CoveredAt` is read atomically with
`IndexedRevision` from `pkg/revlag.Watermark.IndexedAt()`; it is evidence, never a gate. It is never derived from
`LastSynced`, status-publish time, delivery-arrival time, or local watermark-advance time.

`serving` means sound to read after bootstrap, even while behind. Revision-settled is one typed reading satisfying:
known, fresh, `state=serving`, bootstrap complete, both revisions known, and indexed greater than or equal to target.
The ordinary read-health predicate omits the last comparison.

### 8.2 Ingest-backlog status

```go
type IngestBacklogStatus struct {
    Kind                  StatusKind     `json:"kind"` // ingest_backlog
    State                 WorkState      `json:"state"` // building|serving|degraded
    BindingsComplete      bool           `json:"bindings_complete"`
    EntitySweepComplete   bool           `json:"entity_sweep_complete"`
    InitialBacklogDrained bool           `json:"initial_backlog_drained"`
    BoundConsumers        uint32         `json:"bound_consumers"`
    OutstandingMessages   *uint64        `json:"outstanding_messages"`
    ObservedAt            time.Time      `json:"observed_at"`
    Problem               *StatusProblem `json:"problem,omitempty"`
}
```

The producer computes `OutstandingMessages` from one successful observation of every bound durable consumer: pending
plus delivered-but-unacknowledged, summed across consumers. If any observation fails, `OutstandingMessages=nil`,
`state=degraded`, and problem code `outstanding_observation_failed`; a partial total is not presented as a backlog. A
zero bound-consumer set is valid and observes zero.

`BindingsComplete` latches only after every declared durable input port has either bound successfully or caused Start
to fail; configuration-driven reconstruction begins a new binding lifecycle. `InitialBacklogDrained` remains a latch.
`EntitySweepComplete` is true when the boot `ENTITY_STATES` sweep completed or no sweep was applicable. Ingest-settled
is one typed reading satisfying: known, fresh, `state=serving`, bindings complete, entity sweep complete, initial
backlog drained, and outstanding known equal to zero. Later accepted work makes the outstanding count nonzero and
reopens settlement without clearing the initial-drain latch.

Do not add parked, stranded, or completeness fields: the current counters cannot observe MaxDeliver-exhausted work.
Settled means no observable outstanding accepted work, not that every published message applied. #742 remains the
separately named operator-observability lane.

### 8.3 Rule-replay status

```go
type RuleReplayStatus struct {
    Kind               StatusKind     `json:"kind"` // rule_replay
    State              ReplayState    `json:"state"` // building|serving|degraded|reset_required
    WatchSetRevision   uint64         `json:"watch_set_revision"`
    ConfiguredWatchers uint32         `json:"configured_watchers"`
    CompletedWatchers  uint32         `json:"completed_watchers"`
    ReplayComplete     bool           `json:"replay_complete"`
    ObservedAt         time.Time      `json:"observed_at"`
    Problem            *StatusProblem `json:"problem,omitempty"`
}
```

`WatchSetRevision` is a process-local producer epoch and increments for every authoritative watcher-set change,
including removals. It is not compared across producer restarts. All fields are captured under the existing dispatch
gate, so `ReplayComplete` applies to that exact set revision. Adding or replacing a watcher makes it false until every
currently authoritative generation sees its replay sentinel. Zero configured watchers is vacuously complete.
Rule-settled is one typed reading satisfying: known, fresh, `state=serving`, replay complete, and no degraded/reset
condition.

### 8.4 Deleted shared fields and bounded common details

`bootstrap_scope`, generalized `Ready`, generalized `Lag`, `StalenessMs`, `Phase`, string `Revision`, and `LastSynced`
are deleted. No named consumer uses scope. `BoundConsumers` and watcher counts are operator evidence, never verdict
inputs.

The role types may use these bounded common details without becoming a shared status envelope:

```go
type StatusProblem struct {
    Code   string `json:"code"`
    Reason string `json:"reason"`
}

type FailureSummary struct {
    Count   uint64            `json:"count"`
    Reasons map[string]uint64 `json:"reasons,omitempty"`
    FirstAt *time.Time        `json:"first_at,omitempty"`
}
```

`StatusProblem.Reason` and operator `watch_error` are capped at 256 UTF-8 bytes. `FailureSummary.Reasons` contains at
most eight stable reason codes; additional codes fold into `other`. These are diagnostics, not unbounded error logs.

### 8.5 Watch transport and heterogeneous settlement

Use key-specific typed watcher and publisher constructors, or one private generic implementation beneath them. Do not
expose a watcher returning a common status struct. Freshness remains consumer-local: each producer writes every 5
seconds; a successfully decoded delivery is fresh for 15 seconds. Initial delivery of an old KV value still consults
KV commit time so a dead producer's last value is not re-stamped fresh. `observed_at` is operator evidence and is not
compared across process clocks. Tombstone, malformed JSON, or key/kind mismatch fails closed and does not refresh
arrival time; the next valid heartbeat recovers it.

`FullyCovered` is replaced, not adapted to a new field soup. Each concrete watcher performs exactly one `Read`,
validates freshness, and evaluates its own settled predicate, yielding only this post-evaluation decision:

```go
type SettlementDecision struct {
    Key     StatusKey
    Settled bool
    Reason  SettlementReason
}

type SettlementRequirement interface {
    SettlementDecision() SettlementDecision
}
```

`AllSettled(requirements...)` deterministically folds those decisions. The E2E stage explicitly constructs an ingest
requirement, a graph-index revision requirement, and a rule-replay requirement. The common object exists only after the
typed status has been interpreted; it carries no producer data and therefore does not recreate the generalized wire
envelope. One read per requirement preserves the current anti-torn-snapshot invariant.

Ordinary read paths use their role-specific health predicate and never `AllSettled`. A healthy revision view may serve
while behind.

### 8.6 Operator readiness front door

Keep configured HTTP `GET {prefix}{readiness_path}` with the default `/readiness`; do not add readiness to GraphQL. The
route is disabled when `readiness_keys` is empty, and configured keys must be unique members of the closed four-key
set. Unknown keys fail configuration validation. The route accepts GET only and returns no aggregate verdict.

Its heterogeneous array is the one place a closed tagged representation is needed:

```json
{
  "producers": [
    {
      "key": "graph-ingest",
      "kind": "ingest_backlog",
      "known": true,
      "fresh": true,
      "received_age_ms": 82,
      "status": {
        "kind": "ingest_backlog",
        "state": "serving",
        "bindings_complete": true,
        "entity_sweep_complete": true,
        "initial_backlog_drained": true,
        "bound_consumers": 2,
        "outstanding_messages": 0,
        "observed_at": "2026-08-05T18:42:11.251Z"
      },
      "watch_error": null
    }
  ]
}
```

The gateway's closed key/type registry selects the concrete decoder and renderer. One row appears per configured key
in deterministic configured-key order. `known` and `fresh` are false with null status when no value has ever been
observed; a stale value remains visible with `known=true`, `fresh=false`, and its local `received_age_ms`. Watch/bind
failure before a value produces unknown status plus a bounded `watch_error` string. HTTP `200` means the observation
was served, not that producers are usable.

This remains an HTTP control-plane surface, not GraphQL graph data, and requires no NATS CLI. It cannot read entities,
relationships, predicates, or search results.

### 8.7 Clean migration

In one SemStreams increment, update all four producers, index/fusion/clustering/query consumers, gateway rendering, and
E2E; then delete `IndexStatusResponse`, `ComputeBacklogStatus`, the generalized watcher, `Set`, `FullyCovered`, gate,
and their deprecated fields. There are no shims.

semdragon adopts the typed ingest requirement; SemMachina adopts the typed rule-replay requirement. Direct downstream
KV readers decode only the concrete type for the key and validate `kind`. A direct sibling-repository grep found no
current `graph/readiness`, `GRAPH_STATUS`, `FullyCovered`, or bootstrap-field code consumer in semdragon or SemMachina;
these are adoption seams recorded by ADR-088, not current code that requires a compatibility bridge.

`COMPONENT_STATUS` is still deleted. The reviewed baseline has 24 production writers and zero production readers,
excluding E2E. Process lifecycle assertions move to service health; graph role assertions use the typed status above;
flow/message evidence uses the appropriate flow or message-logger surface.

## 9. Mutation caller contract

The accepted GS-01 mutation port and authority behavior remain unchanged.

`internal/graphmutation.Client` is transport machinery. It is not a component-author API.

Adopter-facing components mutate through:

- a narrow `pkg/projection` capability backed by a validated local predicate contract; or
- an operation-specific typed framework adapter where projection semantics genuinely do not fit.

Direct framework callers are not granted semantic authority. They must still state:

- which admitted mutation operation they perform;
- required provenance;
- whether the operation is additive or complete desired-state reconciliation;
- which definite outcomes may be retried by that component; and
- how `commit_unknown` is surfaced.

Projection contracts remain local validation, not ownership or authorization. Overlap is allowed; CAS makes contention
observable.

No missing entity is auto-created for reconcile, append, or delete. Components decide whether a definite not-found
warrants a later create or retry.

The rule component's one-attempt reconcile policy remains the explicit owner-approved GS-01 contract at design
SHA-256 `9c6913ad558205b89c4197bb813228f40133432364698e1413666df8fe11f161` and
`openspec/changes/establish-graph-read-write-foundation/approval.md`. Lifecycle `Transition` is not a retry-symmetry
precedent. Each lifecycle CAS attempt rereads authority and revalidates the current phase, permitted transition, audit
chain, and optional mutator before applying the still-valid imperative intent. A rule action was triggered and evaluated
from an older `ExecutionContext`; rereading only to obtain a new revision would replay that old action without
re-evaluating the predicate and trigger conditions that authorized it.

Therefore rule reconcile performs one exact read and one mutation attempt and returns a definite revision mismatch to
the component. A future rule retry is permitted only through a separately approved operation-specific contract that
re-evaluates both the rule predicate and mutation intent. There is no generic retry-symmetry requirement and no shared
CAS-retry helper.

## 10. Collision dispositions

| Collision | Disposition |
|---|---|
| Direct authority acquisition | Owner ensure seam for graph-ingest; package-local `OpenCatalogBucket` plus minimal read interfaces for declared reactive/snapshot consumers; typed adapters for operation consumers; no generic bucket bypass or injector. |
| Lifecycle exact/list/watch acquisition | Exact remains typed; lifecycle remains a declared reactive authority consumer for List/Watch using its package-local catalog-open seam; generic `GetKeyValueBucket` and dedicated full-graph guard are deleted. |
| Lifecycle Manager-wide poison vs affected-scope poison | Delete the global admission latch; malformed state fails the exact/list/watch scope that touches it and is never acted upon, while unrelated lifecycle work continues with typed errors or observable watch closure plus bounded structured log/metric diagnostics. |
| Query registrations, provider subjects, gateway routes | Target sets are exactly §§6.4–6.6; anything not kept there is deleted. |
| Hierarchy first-page totals vs complete statistics | Coordinator exhausts provider cursors internally and returns a complete observed traversal or a typed error with no partial result. |
| Suffix fragment vs canonical identity | Delete suffix resolution and `ENTITY_SUFFIX_INDEX`; exact ID, exact alias, and explicit discovery remain. This supersedes the archived current graph-ingest-owned GS-05 disposition; rejected r35 future graph-index ownership never became current truth. |
| Exact alias shared by multiple entities | Preserve every owner-complete membership and return `ambiguous`; never use last-writer-wins or select a candidate. |
| GraphQL-shaped facade vs conformant GraphQL | Replace facade mechanics with gqlgen's generated executor plus thin canonical-operation resolvers; no hand-written execution stack. |
| Service-manager `/graph/triples` vs graph gateway | delete always-mounted route |
| Placeholder MCP route vs no MCP contract | delete route |
| Shared `graphview`/`revlag` vs differing owner lifecycles | retain as bounded mechanics; do not generalize without three-owner proof |
| `GRAPH_STATUS` vs process health vs `COMPONENT_STATUS` | preserve first two with distinct semantics; delete `COMPONENT_STATUS` |
| Status producer set | Keep graph-index, graph-embedding, graph-ingest, and rule because each has a named decision; add no spatial, temporal, or clustering key. |
| Status wire | Key-fixed role types replace `IndexStatusResponse`; no stored common union and no fields with cross-role meanings. |
| Catalog owner description vs actual producers | describe shared producer model; present consumers justify keys |
| Projection client vs direct transport callers | projection/narrow adapter is public seam; raw client remains internal |
| Canonical rule spec vs active delta/runtime | canonical spec adopts one exact read and one mutation attempt |
| ADR/program/live guidance | Correction set is exactly §12; no old sequencing remains current. |

## 11. Rule reconcile correction

The canonical rule-projection-mutations contract must match merged runtime, the accepted GS-01 delta, and ADR-091:

- one exact authority read;
- one reconcile request;
- no automatic revision-mismatch retry;
- no retry of `commit_unknown`;
- the component may define a future operation-specific retry only through a separately reviewed contract.

The current “one bounded conflict retry” canonical requirement is stale contract drift, not intended target behavior.
The explicit GS-01 approval superseded that earlier requirement. Lifecycle's bounded loop is intentionally different:
it rereads and revalidates the complete transition intent on every attempt. Rule reconcile has only the previously
evaluated action and does not re-evaluate its triggering predicate after a mismatch. A fresh revision alone cannot make
stale evaluated intent safe to replay.

## 12. Durable record and current-artifact correction

ADR-090's architectural decisions remain accepted: current-state authority, eventual consistency, role-specific
materialized views, no general CQRS runtime, no recovery subsystem, GraphQL for remote reads, narrow typed embedded
adapters, no raw-KV application fallback, and the three-owner gate for shared runtime mechanics.

Old GS sequencing may remain only inside paths explicitly classified as historical archives. The post-GS-01 design and
reality audit may mention it solely as invalidated context.

### 12.1 Live source dispositions

| Live artifact | Stale evidence | Binding disposition |
|---|---|---|
| `.agents/skills/query-pattern/SKILL.md` | lines 17, 41-42, 69-71 | Replace future GS claims and provisional facade guidance with the canonical operation catalog, conformant GraphQL, and narrow typed adapters. |
| `gateway/graph-gateway/README.md` | lines 4, 13, 77 | Replace GS-12/GS-10 sequencing and provisional-gateway claims with current target operation truth and the gqlgen/thin-resolver boundary. |
| `gateway/graph-gateway/doc.go` | package contract describes the hand-written GraphQL-shaped facade | Replace with the gqlgen schema-first query-only contract and canonical-operation resolvers. |
| `docs/concepts/10-pathrag-pattern.md` | lines 115, 128, 133 call `pathSearch` provisional facade access | Describe `pathSearch` as an admitted conformant GraphQL operation. |
| `docs/concepts/11-query-access.md` | lines 43-55, 130-133, 188-206 | Replace facade and GS sequence with current read contracts. |
| `docs/concepts/30-spatial-temporal-queries.md` | lines 143, 153 | Replace provisional facade wording and remove GS-12 future-admission claim; point to canonical spatial/temporal operations. |
| `docs/advanced/07-graph-components.md` | lines 20-21, 43-78, 103-109 | Replace scheduled GS table and canonical-program link with current operation/status/owner contracts. |
| `docs/basics/02-architecture.md` | lines 70-75, 148-150, 288-295 | Replace provisional/GS target material with current architecture. |
| `pkg/lifecycle/doc.go` and `openspec/specs/lifecycle/spec.md` | current whole-Manager guard implies unrelated poison blocks every lifecycle operation | Record affected-scope poison, no whole-graph lifecycle admission scan, and bounded diagnostics while unrelated work continues. |
| `openspec/specs/rule-projection-mutations/spec.md` | still requires one bounded conflict retry after the owner-approved GS-01 one-attempt amendment | Require one exact read and one mutation attempt; explain that any later retry must re-evaluate rule predicate and intent, not merely fetch a new revision. |
| ADR-090 | lines 9-13, 42-47, 70-76 | Preserve architecture decisions; remove GS-12 and canonical-program ordering. |
| `docs/proposals/graph-state-read-write-program.md` | obsolete GS-03..GS-14 program | Move to historical archive and mark superseded by the accepted post-GS-01 inventory/design. It is not live routing. |
| `docs/proposals/graph-state-read-write-ruling-conformance.md` | lines 8, 18, 21, 23, 36 | Move with the invalidated program into historical archive. |
| `docs/proposals/graph-state-read-write-decision.md` | lines 12, 19 | Move into historical archive or rewrite its header as superseded history with no canonical-program link. |
| `docs/proposals/prev1-program.md` | lines 10, 20 | Remove old program as a live prerequisite; archive if retained for history. |
| `docs/proposals/graph-state-read-write-inventory-review.md` | lines 40, 47 | Archive beside the inventory it reviewed; it is evidence for the invalidated program, not current target truth. |
| `docs/proposals/graph-state-read-write-inventory.md` | lines 20, 30 | Archive as the pre-GS-01 inventory. The reviewed post-GS-01 audit is current evidence. |
| `openspec/changes/discovery-under-stream-shapes/proposal.md` | line 5 | Keep the change suspended only if its prerequisite is rewritten against the approved current foundation; otherwise archive/supersede it. No canonical-program dependency survives. |
| `openspec/changes/discovery-under-stream-shapes/tasks.md` | line 5 | Same disposition as its proposal. |
| `openspec/changes/semantic-tier-split/proposal.md` | line 5 | Keep suspended only with a prerequisite stated against current foundation; otherwise archive/supersede. |
| `openspec/changes/semantic-tier-split/tasks.md` | line 5 | Same disposition as its proposal. |
| completed `establish-graph-read-write-foundation` change | task truth remains 44/45 despite merged #898 | Record completed truth and archive the change. Completed GS-01 artifacts then become historical and are excluded from live-guidance checks. |
| `openspec/specs/graph-index-readiness/spec.md` | binding spec still requires one generalized envelope/gate/fold | Split into explicit revision-view, ingest-backlog, rule-replay, settlement-fold, and operator-surface requirements. Preserve all four named producer capabilities while deleting shared field-soup semantics. |
| `docs/operations/adopter-caught-up-readiness.md` | generalized `graph/readiness.Set`, every producer's `lag == 0`, and `bootstrap_scope` guidance | Replace with named typed requirements and their exact predicates; keep snapshot settlement separate from read health. |
| `docs/operations/migration-readiness-distribution-adr083.md` | historical `Ready`, `Lag`, `staleness_ms`, and old KV wire migration | Archive as historical migration guidance. No compatibility migration survives. |
| `docs/operations/adopter-gateway-response-shape.md` | direct `graph.index.query.predicate*` backing subjects at lines 42-45 | Replace with canonical `graph.query.predicate*` coordinator operations and ordinary conformant GraphQL response projection. Provider subjects remain internal. |
| ADR-088 | accurate rationale but generalized Set/scope/fold mechanics are superseded | Preserve the ADR verbatim as history. Add a new ADR that partially supersedes only its generalized Set/gate, `bootstrap_scope`, common `Lag == 0` fold, and shared `IndexStatusResponse` implications. |
| `test/e2e/scenarios/stages/entities.go` | generic `FullyCovered` over ingest/index/rule | Replace with heterogeneous `AllSettled` over explicit ingest-backlog, graph-index revision, and rule-replay requirements; keep one-read-per-requirement and post-settlement entity assertions. |
| `processor/graph-query/README.md` | partial/suffix resolution, alias-or-ID fallback, and old hierarchy response guidance | Document canonical exact identity, three-state exact alias lookup, no suffix guessing, and complete-or-error observed hierarchy counts. |
| `openspec/specs/graph-query/spec.md` | lacks canonical exact-read, three-state explicit-alias/no-guessing, and hierarchy completeness contracts | Add those target contracts and their typed failures, including `alias_ambiguous`. |
| `openspec/specs/graph-index/spec.md` | raw alias-to-single-ID storage permits last-writer-wins collisions and does not require owner-complete retraction | Replace it with the paired `a2`/`e2` membership contract, exact absent/singular/ambiguous resolution, bootstrap repair, and readiness withholding on failed required work. |
| `docs/advanced/05-index-reference.md` | teaches raw `ALIAS_INDEX` keys and singular alias values | Document exact-alias membership semantics and keep hashes, paired axes, and NATS key grammar internal. |
| `docs/operations/26-nats-kv-key-migration-ledger.md` | records the old raw alias layout as current | Record the clean `a2`/`e2` cutover and the inert old rows; do not add a dual reader or migration helper. |
| `openspec/specs/graph-ingest/spec.md` | suffix-index maintenance and suffix-poison scenarios | Delete suffix requirements; retain the general post-commit-side-effect rule for side effects that survive. |
| `openspec/specs/graph-retention/spec.md` | treats `ENTITY_SUFFIX_INDEX` as a live framework bucket | Remove the retired bucket from current retention behavior. |
| `docs/operations/framework-bucket-catalog.md` | lists `ENTITY_SUFFIX_INDEX` | Remove it from the current catalog; note cleanup only in the breaking migration record. |
| `docs/operations/17-predicate-cutover-clean-wipe.md` and `29-entity-id-contract-clean-cutover.md` | current wipe instructions name the suffix bucket | Remove it from live runtime instructions; an old deployed bucket is inert orphan data handled by ordinary operator administration. |

Suffix retirement explicitly supersedes the graph-ingest-owned GS-05 disposition recorded in
`openspec/changes/archive/2026-08-05-establish-authority-read-and-recovery/scope-audit-r36.md:206-221,280-286`.
That same accepted audit records revision 35's proposed future graph-index ownership as rejected and unapproved. PR
#898 did not establish graph-index suffix ownership. Historical artifacts remain verbatim, but no current instruction
may cite either graph-ingest GS-05 or rejected r35 graph-index ownership as a live future disposition.

The new ADR retains ADR-088 decisions 1, 3, 4, and 5: producer-owned keys, consumer-declared dependencies, no framework
mandatory list or published aggregate, backlog is not completeness, and rule replay describes the current watcher
generation. It also retains the consequence that snapshot settlement is separate from read health and must never gate
graph reads.

### 12.2 Status rationale and issue disposition

- gh#712 remains satisfied by `IngestBacklogStatus` plus `AllSettled`; it is not evidence for a generic envelope.
- gh#732 remains satisfied by `RuleReplayStatus` over the current authoritative watcher-set revision; it is not
  evidence for process-lifetime bootstrap or a generic envelope.
- gh#742 remains separate. No field claims parked, stranded, or complete processing until a producer can make that
  observation honestly.
- gh#590's watched-KV distribution and consumer-local freshness rationale remains intact.
- The merged GS-01 read/write contract is not reopened; `GRAPH_STATUS` is operational state, not graph authority.

Historical ADRs and archived OpenSpec changes remain verbatim. They are evidence of the previous contract, not live
instructions. Current guidance corrections are proved against the explicit file inventory in §12.1 and the structural
and behavioral gates in §17, not repository-wide word searches. Broad greps for words such as `ready`, `lag`,
`status`, `owner`, `query`, or `canonical order` are prohibited as acceptance gates because they match valid history
and unrelated code.

## 13. Target adopter seam

Specific adopter: a developer outside this repository writing a component without reading graph internals.

| Target surface | What they must know | If they do nothing | Discovery | What they should know |
|---|---|---|---|---|
| GraphQL graph reads | schema field, arguments, result/error shape | no graph gateway exists unless composed; no hidden service-manager read front appears | GraphQL schema and boot-time component composition | only the GraphQL contract |
| Mutation port | requested operation and typed outcome; exact revision for reconcile/delete | undeclared or unwired mutation capability fails composition/start | typed port schema, compile/boot error | domain intent and classified result, not subjects or KV |
| Projection mutation | local contract/group and complete-vs-additive semantics | invalid operation fails locally before send | typed construction/preflight error | the component’s own predicate model |
| Narrow internal reader | only the method required by the component | dependency absence fails construction/composition | compile/boot error | operation semantics, not provider subject |
| Lifecycle poison | only the typed failure on an affected exact/list operation or observable closure of an affected watch | valid unrelated lifecycle work continues; malformed state is never acted upon | operation error plus bounded structured logs/metrics naming entity and revision | no whole-graph poison scan, status product, or reset gate |
| Derived query | result, currency, and typed unavailable/reset state | healthy stale results remain usable; known-unsound results fail visibly | typed response and GraphQL error/extensions | no readiness key names or bucket knowledge |
| Exact alias lookup | exact alias and typed not-found/ambiguous outcomes | unique aliases continue to resolve; collisions fail visibly instead of selecting an entity | GraphQL schema and typed internal response | alias semantics only, never bucket keys, hashes, NATS grammar, or reconciliation |
| Alias result currency | `kvRevision` describes the entity row; `aliasCoveredThroughRevision` describes alias-view coverage | ordinary reads still work; freshness-sensitive code may compare the explicit lower bound | typed response and GraphQL schema/error extensions | the two operation-level meanings, never graph-index status keys or watermark mechanics |
| Snapshot settlement | the named role requirements it depends on | old generic helper fails to compile; no silent remap | typed settlement helpers and migration note | role intent, not producer counters or generic lag arithmetic |
| Rule replay wait | rule-replay requirement | old common-envelope decode fails | typed rule helper and migration note | replay completion, not watcher counters or sentinel mechanics |
| Operator status | configured key from the closed four-key set | empty keys disable the route; invalid keys fail startup | configured gateway HTTP readiness route | which producer roles this deployment actually composed |

The framework absorbs subjects, bucket names, routing, status folding, transport classification, and CAS mechanics.

## 14. Explicit deletions and non-goals

Recommended deletions:

- exported/provisional general `graph/query.Client` surface;
- always-mounted service-manager `/graph/triples`;
- placeholder `/mcp`;
- `graph.ingest.query.suffix`, `ENTITY_SUFFIX_INDEX`, its cache/maintenance/fallback scan, and all partial-ID guessing;
- `COMPONENT_STATUS` and its write-only reporters;
- generalized `IndexStatusResponse`, shared readiness gate/watcher/Set/FullyCovered, and cross-role status fields;
- duplicated query-operation declarations;
- stale rule retry requirement;
- stale ADR-090 roadmap references;
- obsolete catalog owner wording;
- superseded docs that teach deleted surfaces.

An existing deployed `ENTITY_SUFFIX_INDEX` bucket becomes inert orphan data. SemStreams neither opens nor auto-deletes
it and adds no NATS CLI dependency; operators may remove it with their normal NATS administration during migration.

Raw legacy keys inside an existing `ALIAS_INDEX` bucket are likewise inert. New code reads and writes only the
versioned `a2`/`e2` membership axes. There is no dual read, compatibility shim, migration helper, or deprecated alias
API. Because the bucket is wholly derived, operators may discard and rebuild it through ordinary administration at the
breaking cutover; SemStreams does not require the NATS CLI.

Non-goals:

- no ownership, claims, leases, tokens, presence, or writer authorization;
- no CQRS framework or event-sourced authority;
- no mutation stream, command ledger, outbox, or exactly-once claim;
- no SemStreams backup, checkpoint, restore, recovery, or attestation product;
- no stubs or missing-reference repair workflow;
- no automatic create on non-create mutation;
- no general derived-view runtime;
- no universal requirement to use `graphview` or `revlag`;
- no MCP graph contract without real tools;
- no hand-written GraphQL parser, executor, introspection, selection, or response engine;
- no compatibility aliases, deprecated paths, dual readers, or shims;
- no issue-driven ordering;
- no downstream product policy in SemStreams.

This artifact defines target state only. It does not prescribe roadmap slices, issue ordering, or implementation
sequencing. Dependency-ordered slices are derived only after the owner approves the complete target and may not amend
that target implicitly.

## 15. Issue evidence mapping

Issues are evidence and test prompts, not the implementation program.

| Design territory | Confirmed issue evidence | Title-only hypotheses requiring independent adjudication |
|---|---|---|
| Query declarations and external reads | #571, #422, #822, #859, #883, #884 | #785, #784, #315, #306, #176, #211 |
| Derived lifecycle and convergence | #887, #875, #820 | #618, #672, #710, #525, #526, #586, #579, #588 |
| Mutation/projection boundary | #692, #690, #689, #688, #695, #694, #818 | #367, #340 |
| Status semantics | #868, #820 | #795, #421, #618 |
| Proof coverage | #888, #872 | #821, #829, #830, #811, #769 |

No issue is closed, scheduled, or accepted merely because it appears here. Old-operation issues whose premises were
removed remain superseded rather than recreated.

#622 remains correctly closed because GS-01 classified the suffix bucket; the target now retires the feature for
collision, retraction, and serving-honesty failures rather than reopening the ownership finding. This retirement
supersedes graph-ingest-owned GS-05; the archived r35 graph-index proposal was never accepted ownership. #176's stale
claim that prefix pagination was not shipped is corrected separately; hierarchy aggregation consumes the already-present
cursor primitive. #833 remains the cross-cutting request-deadline propagation lane and is not absorbed here.

## 16. Downstream holdout contract

The ten downstream repositories remain an unmeasured holdout set:

- semdev
- semmachina
- semsource
- semboids
- semdragon
- semstreams-ui
- semteams
- semconnect
- semlink
- semops

They do not constrain the target and are not used to design around current anti-patterns.

The proof is feature parity, not API-shape preservation:

- every currently used graph capability is classified;
- legitimate behavior is available through the target GraphQL, port, or narrow adapter surface;
- anti-pattern usage is migrated;
- removed surfaces receive no shim;
- downstream projects may break until they migrate;
- a hidden dependency can falsify an implementation assumption but cannot revive the rejected general surface without
  owner review.

## 17. Proof gates

This design is rejected or revised if any gate fails.

### Read contract

- Runtime subscriptions, default ports, routing, and GraphQL resolvers enumerate the same admitted operation set.
- Every admitted operation has a present typed consumer.
- A clean checkout reproducibly generates gqlgen code from the committed SDL and `gqlgen.yml` without a diff.
- GraphQL conformance tests cover parsing, variables, operation names, aliases, fragments, selection projection,
  introspection, custom scalars, and error shape through gqlgen.
- Authored resolvers are thin canonical-operation delegates. No production source implements a GraphQL parser,
  executor, introspection engine, selection projector, response engine, mutation, subscription, or playground route.
- No production application caller uses raw graph subjects or direct authority KV.
- No exported general embedded graph client remains.
- `/graph/triples` and placeholder MCP are absent.

### Lifecycle poison localization

1. A malformed entity A outside workflow B does not block an exact read, list, watch, or valid transition on B.
2. Exact access, list scope, or workflow work that touches A returns the existing typed poison/reset-required outcome;
   no mutation is attempted from A.
3. A malformed matching watch event is not emitted as a participant and causes no action; the affected subscription
   closes or degrades observably, bounded structured logs/metrics identify its entity and revision, and unrelated
   subscriptions continue. No new lifecycle status surface is introduced.
4. No lifecycle start or operation opens `WatchAll` or performs a whole-authority preflight, and no Manager-wide poison
   latch remains.

### Mutation retry boundary

1. Rule reconcile performs one exact read and one mutation request. A definite revision mismatch returns visibly and
   produces no second read or mutation request.
2. Rule tests prove a newer revision alone cannot replay an action from the old `ExecutionContext`; any future retry
   contract must re-evaluate the predicate and desired mutation intent.
3. Lifecycle contention tests prove every retry rereads and revalidates current phase, permitted transition, audit
   chain, and optional mutator before issuing another mutation.
4. No shared generic CAS-retry helper or framework retry knob is introduced.

### Authority-composite completeness

Hierarchy-statistics behavior proves:

1. 1,001 or more unique entities across multiple pages contribute to root and child observed counts;
2. each request stays within `MaxPrefixQueryLimit`, and a provider that byte-trims pages still exhausts correctly;
3. duplicate IDs across pages count once;
4. exactly 10,000 IDs with an exhausted cursor succeeds, while a continuation at the ceiling returns typed
   resource-exhausted with zero response bytes;
5. repeated/non-advancing cursor and empty-page-plus-cursor each fail without looping;
6. any later-page transport/handler failure, poisoned decode, cancellation, or timeout returns error and no partial
   hierarchy result;
7. GraphQL pins the new argument/result/child fields and rejects old `limit`, `totalEntities`, and child `count`; and
8. a scripted concurrent-change case pins defensive deduplication and documents that insertion before the cursor is
   outside snapshot guarantees.

### Identity and suffix-retirement proof

1. Public and provider exact-entity handlers reject empty, 1–5-part, wildcard, malformed, and over-part IDs with typed
   `entity_id_invalid`; a valid six-part ID performs exactly one authority read.
2. `entityByAlias` sends typed `{alias}`, hydrates only a singular exact match, returns typed not-found for absence and
   `alias_ambiguous` for collision, and never forwards alias text as an entity ID.
3. GraphRAG/path tests prove canonical ID and singular exact alias work, absence may fall through to discovery, and
   ambiguity/provider failure does not; a suffix-only token makes no suffix request.
4. Graph-ingest registers exactly entity, batch, and prefix query providers; no suffix provider remains.
5. Catalog structure contains no `BucketEntitySuffixIndex` or `ENTITY_SUFFIX_INDEX`; graph-ingest startup creates no
   suffix bucket or cache.
6. Create, merge, and delete make no suffix-index writes or deletes. Suffix-specific tests are removed rather than
   translated into compatibility tests.
7. A targeted current-source check covers only the retired identifiers `graph.ingest.query.suffix`,
   `BucketEntitySuffixIndex`, `ENTITY_SUFFIX_INDEX`, `resolveViaSuffix`, `resolvePartialEntityID`, `suffixBucket`, and
   `suffixCache`; historical ADRs and archived OpenSpec are excluded.
8. Current OpenSpec strict validation and graph-query/gateway integration cover the public behavior. No extra E2E tier
   is justified solely for an unexposed deleted provider.

### Exact-alias proof

1. Exact aliases containing dots, spaces, and other non-NATS-key characters resolve through the internal hash layout;
   callers never validate or construct storage keys.
2. Zero exact memberships returns `absent`; one returns `singular`; two entities sharing an alias deterministically
   return `ambiguous` and never hydrate either entity.
3. Replacement and predicate removal retract only the changed entity's old memberships; entity deletion retracts all
   of that entity's memberships without disturbing a colliding entity.
4. Cold replay, restart, bootstrap cleanup, and interrupted add/delete ordering converge both `a2` and `e2` axes from
   current authority.
5. Required membership write, retraction, validation, or repair failure withholds the graph-index watermark and serving
   readiness until current-authority redrive succeeds.
6. Malformed rows, retained-value/hash mismatch, and inconsistent axis pairs fail closed; raw legacy alias keys are
   ignored rather than read through a compatibility path.
7. Public GraphQL and canonical NATS mapping return an entity only for `singular`, with typed not-found or ambiguity for
   the other states. Provider transport/decode/readiness failure is never collapsed into absence.
8. GraphRAG/path classification never swallows ambiguity or provider failure and never chooses among candidates.
9. Absent, singular, and ambiguous provider outcomes all carry the coverage captured immediately before their scan; a
   watermark advance during the scan does not advance the returned value.
10. Singular hydration preserves alias coverage while reporting the independent exact-entity `KVRevision`; tests prove
    neither revision is interpreted as the other.
11. Absent and ambiguous classified error detail preserves alias coverage through the NATS wire and GraphQL
    `extensions.aliasCoveredThroughRevision`.
12. Unavailable or invalid contiguous coverage returns `index_not_ready` rather than fabricated currency; a completed
    empty-authority bootstrap may honestly report known coverage zero.
13. GraphQL schema exposes `entityByAlias` as `AliasedExactEntity` with `entity`, `kvRevision`, and
    `aliasCoveredThroughRevision`, and rejects the old `ExactEntity` result contract for that field.

### Derived-owner behavior

Each surviving derived query owner proves, as applicable:

- cold bootstrap;
- live update;
- source delete and predicate retraction;
- restart;
- required-write failure;
- redrive from current authority;
- watcher loss or periodic-run failure;
- poison;
- honest readiness/currency;
- no ready partial result.

A single test harness may share scenarios; it must not force one runtime implementation.

### Shared-primitives gate

A broader substrate is allowed only when:

- at least three owners require the same mechanic;
- the mechanic has identical semantics, not merely similar names;
- a prototype reduces non-generated production code;
- it reduces adopter knowledge;
- it introduces no owner-specific hook maze.

### Role-typed status proof

Proof is targeted, structural, and behavioral. Repository-wide greps for overloaded words are not proof.

1. Closed key/type mapping tests in `graph/readiness` prove each of the four keys accepts exactly its declared `kind`
   and concrete decoder. Wrong kind, malformed JSON, tombstone, and unknown key fail closed; a later valid heartbeat
   recovers.
2. Exact wire-key tests for each concrete status marshal and assert the allowed JSON key set. Retired keys (`ready`,
   `lag`, `staleness_ms`, `bootstrap_scope`, `phase`, string `revision`, `last_synced`) are absent without false-positive
   text searches.
3. Predicate tables prove:
   - revision-view health is independent of lag; settlement requires known revisions and indexed greater than or equal
     to target, including valid 0/0;
   - ingest requires an all-consumer observation; partial observation is unknown/degraded; delivered-but-unacknowledged
     work counts; zero consumers is settled; later work reopens settlement; entity sweep and initial drain remain
     required;
   - rule watcher-set change reopens replay; an old generation cannot close it; degraded/reset fail; zero patterns is
     settled.
4. The E2E three-requirement fold reads each typed requirement exactly once. `AllSettled` returns the deterministic
   first failed key/reason and cannot inspect producer fields itself.
5. The operator HTTP contract proves ordered tagged rows, known/fresh/received age/status/watch error, closed key
   validation, and no aggregate verdict.
6. Producer integration tests prove each producer publishes the correct kind to its fixed key on first compute and
   heartbeat; graph-index/embedding use the atomic watermark pair; graph-ingest publishes null outstanding after any
   observation failure; rule publishes one dispatch-gate-consistent watcher-set revision.
7. The compiler is the retired-symbol proof. Delete `IndexStatusResponse`, `ComputeBacklogStatus`,
   `BacklogStatusInputs`, generalized `StatusReading`/`EvaluateReadinessGate`, generic `Watcher`/`Set`, and
   `FullyCovered`, then compile/test the touched packages. Any live reference is a compile failure.
8. Current-doc proof reviews and validates only the explicit §12.1 target files and the new ADR. Historical ADRs and
   archived OpenSpec changes are excluded; ADR-088 must remain byte-identical.

`COMPONENT_STATUS` deletion remains blocked only by a verified production reader with unique semantics. No
process-liveness endpoint gates on data-plane coverage.

### Complexity gate

- No new bucket, stream, service, coordinator, compatibility path, general client, or MCP surface.
- Production code is net-negative after generated artifacts. Generated gqlgen output is excluded; authored SDL,
  `gqlgen.yml`, resolvers, typed adapters, and glue count normally.
- Superseded concepts and documentation are removed, not layered.
- The number of graph front doors decreases.
- No adopter must know a raw subject, bucket, consumer name, or readiness-key spelling for ordinary use.

### Consistency gate

- CAS remains the existing-key authority race boundary.
- Missing references remain visible and non-fatal.
- Derived currency or unsettled work is reported by its role type, not confused with poison or transport loss.
- No component claims exactly-once behavior.
- No new design requires global availability or synchronous derived convergence.

### Verification gate

A breaking implementation requires focused unit and race tests first, the relevant integration packages once, and a
final structural E2E plus semantic E2E because typed revision status reaches both graph-index and graph-embedding. Do
not run the full E2E ladder iteratively.

The explicit compiler/test surface is:

- types/gate/transport/fold: `graph/index_status.go`, `graph/readiness_gate.go`, and
  `graph/readiness/{watcher,publisher,set,gauges}.go`;
- revision producers/consumers: `processor/graph-index/{watermark,component,query,metrics}.go`,
  `processor/graph-embedding/{readiness,component,metrics}.go`, `graph/query/client.go`, and
  `processor/graph-clustering/component.go`;
- graph query/identity/catalog: `processor/graph-query/{query,entity_resolver}.go`,
  `processor/graph-ingest/{query,component}.go`,
  `graph/{constants,kvcatalog,query_prefix_types,query_index_types,exact_entity}.go`, and gateway gqlgen SDL,
  configuration, generated executor, and thin resolvers, including the dedicated `AliasedExactEntity` wire shape;
- lifecycle localization and retry asymmetry: `pkg/lifecycle/{manager,manager_query,doc}.go`,
  `processor/rule/{actions,triple_mutator}.go`, and `pkg/projection/mutation_client.go`;
- other producers: `processor/graph-ingest/readiness.go`, `processor/rule/readiness.go`, and rule watcher-set revision
  maintenance in `processor/rule/{processor,entity_watcher}.go`;
- outward and in-repository consumers:
  `gateway/graph-gateway/{component,readiness_surface}.go` and `test/e2e/scenarios/stages/entities.go`.

Integration also covers typed ports, query routing, watcher behavior, and status. Removed alternate front doors receive
deterministic E2E assertions. Downstream holdout results are recorded without compatibility concessions.

## 18. Risks

- A conformant GraphQL executor may expose differences hidden by the current substring facade. That is expected breaking
  behavior; schema and E2E evidence must define the intended result.
- gqlgen adds generated source and a maintained library dependency. The SDL and thin resolvers remain the reviewable
  contract; generated output is reproducible and is never hand-edited or counted as authored complexity.
- Localized lifecycle poison allows known malformed authority state to coexist with unrelated valid lifecycle work.
  The affected scope fails visibly and diagnostics identify it; this is deliberate pragmatic/eventual-consistency
  behavior, not silent acceptance or permission to mutate poison.
- Query-operation consolidation can become a dynamic registry abstraction. It must remain an internal typed declaration
  with present consumers.
- Exhaustive hierarchy traversal can be expensive on a broad prefix because the provider performs keyset pages over
  current authority. The framework ceiling and complete-or-error contract bound cost without publishing false totals.
- Removing suffix guessing may surface callers that used short fragments as identity. They migrate to canonical IDs,
  explicit aliases, or search; the nondeterministic behavior is not reproduced.
- Exact alias collisions that previously appeared to work will now return `alias_ambiguous`. That is intentional: the
  old answer depended on write order. Healthy eventual-consistency lag may still expose a prior singular membership,
  temporary absence, or temporary ambiguity, with `aliasCoveredThroughRevision` exposing the view's lower-bound
  authority coverage independently from the hydrated entity's `kvRevision`.
- Paired alias membership rows add repair ordering inside graph-index. They remain owner-specific derived mechanics;
  they must not become a transaction protocol or a general secondary-index framework.
- A “derived-owner contract” can accidentally become a universal runtime. The three-owner/reduced-code gate prevents
  that.
- Deleting `COMPONENT_STATUS` may reveal a direct downstream reader. The response is a clean migration to the correct
  semantic surface.
- Removing the aggregate client may expose consumers that combined unrelated capabilities for convenience. They receive
  narrow interfaces, not a reconstructed general client.
- `GRAPH_STATUS` can sprawl if every component publishes defensively. The present-consumer rule applies to every key.
- Typed status transport can drift back into a stored union or a generic field bag. Key-specific concrete decoding and
  wire-key tests prohibit that regression.
- Snapshot settlement can be misused as read admission. `AllSettled` remains a snapshot/workflow helper; ordinary
  graph reads use only role health and may serve healthy stale results.
- Healthy stale serving can be mistaken for careless consistency. Results must expose honest currency where it is
  meaningful, while known-incomplete state remains a typed failure.
- Rule one-attempt behavior may surface revision mismatch under write load. Automatically replaying an action evaluated
  against an older trigger would be less honest; a future retry must re-evaluate rule intent, not merely fetch a newer
  revision.

## 19. Owner rulings requested

The complete Option B target asks the owner to approve or reject these decisions as one design:

1. Lifecycle remains a declared reactive authority consumer using package-local catalog acquisition; its generic
   acquisition and full-graph guard are deleted, and no graph-source injector is introduced. This deliberately
   localizes poison: affected exact/list/watch scope fails and never acts on malformed state, bounded diagnostics name
   it, and unrelated lifecycle work continues without a Manager-wide admission latch.
2. The operation and provider dispositions in §§6.4–6.6 are the complete target set; conformant GraphQL is the sole
   external graph-read API; `/graph/triples`, placeholder MCP, and the general embedded client are deleted.
3. Hierarchy statistics internally exhaust provider cursors and return a complete observed traversal or error, with a
   framework-owned 10,000-ID ceiling, eventual-consistency wording, renamed observed counts, and no partial result or
   compatibility fields.
4. Suffix resolution and `ENTITY_SUFFIX_INDEX` are retired completely; entity reads require canonical IDs,
   GraphRAG accepts canonical-or-exact-alias references only, and no cleanup shim or NATS CLI dependency is introduced.
   This supersedes archived graph-ingest-owned GS-05; rejected r35 future graph-index ownership never became current.
5. `entityByAlias` remains graph-index-owned collision-safe, owner-complete exact membership. It returns
   absent/singular/ambiguous, hydrates only singular, never chooses among collisions, hides NATS key grammar,
   reconciles replacement/deletion, withholds readiness on failed required work, and has no legacy raw-layout reader or
   compatibility shim. Every semantic outcome exposes the pre-scan alias coverage lower bound; singular success uses
   dedicated `AliasedExactEntity`, while absent/ambiguous errors preserve the same currency distinctly from entity
   `KVRevision`, without a general metadata framework.
6. Derived owners share behavioral obligations without a general runtime; projection/narrow typed adapters remain the
   mutation seam; the rule canonical spec is one exact read plus one mutation attempt. Lifecycle retry is not a
   symmetry precedent because it rereads and revalidates transition intent on every attempt; a rule retry requires a
   separately approved contract that re-evaluates its predicate and intent rather than replaying an old
   `ExecutionContext`.
7. All four `GRAPH_STATUS` keys remain with the fixed type map in §8; graph-ingest and rule status are not deleted.
8. The generalized envelope, health gate, watcher, Set, FullyCovered, and deprecated shared fields are deleted without
   a shim.
9. `WatchSetRevision` is a producer-owned epoch incremented on every authoritative rule watcher-set change, so replay
   completion names the exact set it describes.
10. `bootstrap_scope` is deleted because no surviving named decision consumes it; counts remain diagnostics only.
11. Ingest backlog is honest: any consumer-observation failure yields null outstanding and degraded status; zero bound
   consumers is valid zero; this increment makes no parked, stranded, or completeness claim.
12. Snapshot settlement uses consumer-side `AllSettled` over post-evaluation typed decisions, never a stored/common
   status union and never an ordinary read gate.
13. The operator surface remains configured HTTP with tagged role-specific rows, closed key validation, and no
   aggregate verdict; there is no NATS CLI dependency or GraphQL readiness alias.
14. ADR-088 remains immutable history and a new ADR records the exact partial supersession in §12.
15. All current-artifact correction dispositions in §12 are required target truth; historical ADRs and archives remain
   unchanged.
16. Verification runs focused tests first and final structural plus semantic E2E, not repeated full-ladder runs.
17. The conformant GraphQL component uses gqlgen with committed schema-first SDL, reproducible generated type-safe
   execution, and thin canonical-operation resolvers. SemStreams does not hand-write GraphQL execution or expose
   mutations, subscriptions, or playground routes; replacing gqlgen or adding handwritten execution requires a new
   owner ruling.

Approval also deletes `COMPONENT_STATUS` and retains the fixed no-CQRS, no-recovery, eventual-consistency boundaries.

No acquisition, operation, status-key, status-field, or stale-guidance decision remains deferred to implementation.

No binding target state exists until these rulings receive owner approval and the design passes an independent pre-owner
review.
