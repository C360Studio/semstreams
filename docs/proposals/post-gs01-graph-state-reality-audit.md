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
