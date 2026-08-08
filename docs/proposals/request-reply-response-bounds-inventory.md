# Request/reply response bounds and ObjectStore surface inventory

## Checkpoint identity

This is an inventory-only artifact. It records repository state and collisions; it does not select a target state,
recommend an implementation, or authorize a new exported surface.

- Committed baseline: `4d3ea2ff5db69b40840c51ef76a3e2f730edef62`.
- The working tree also contains the uncommitted Foundation B trajectory-read cutover. Its
  `agentic.query.trajectory` responder is included below and is identified as uncommitted.
- Dependency inspected: `github.com/nats-io/nats.go v1.52.0` (`go.mod:11`).
- Enumeration command:

  ```sh
  rg -n "SubscribeForRequests|\.Subscribe\(|\.Respond\(" \
    --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' --glob '!cmd/**'
  ```

## Problem statement

The uncommitted trajectory reader can hydrate every visible evidence body into one NATS request/reply response. A
count limit does not bound the encoded bytes. The shared responder logs a failed successful reply publication and
returns no classified error to the requester (`natsclient/request.go:342-397`), so an oversized response becomes a
requester timeout.

The same mechanism serves graph, index, clustering, embedding, temporal, spatial, tool, and mutation responders. The
claimed gap is therefore framework-wide response-carrier behavior, not a trajectory-only pagination gap.

## Production responder census

The current working tree has 46 production request/reply subscription endpoints representing 48 operations. Forty-five
endpoints use `natsclient.SubscribeForRequests`; one ObjectStore endpoint subscribes directly and multiplexes three
actions. The committed baseline lacks only the uncommitted trajectory endpoint.

| Owner | Count | Acquisition |
|---|---:|---|
| graph-index | 8 | `processor/graph-index/query.go:23-82` |
| graph-query | 16 | `processor/graph-query/query.go:18-62`; `graphrag.go:216-224` |
| graph-ingest reads | 4 | `processor/graph-ingest/query.go:24-56` |
| graph-ingest mutations | 4 | `processor/graph-ingest/mutation_runtime.go:17-39` |
| graph-clustering | 4 | `processor/graph-clustering/query.go:17-49` |
| graph-embedding | 3 | `processor/graph-embedding/query.go:17-43` |
| graph-index-spatial | 2 | `processor/graph-index-spatial/query.go:20-38` |
| graph-index-temporal | 1 | `processor/graph-index-temporal/query.go:15-27` |
| agentic-loop | 2 | `processor/agentic-loop/component.go:474-489` |
| agentic-tools | 1 | `processor/agentic-tools/component.go:185-201` |
| ObjectStore API | 3 | `storage/objectstore/component.go:304-318,402-477` |
| **Total operations** | **48** | **46 subscription endpoints** |

Operations by owner:

- graph-index: outgoing, incoming, alias, predicate, predicateList, predicateStats, predicateCompound, and byName;
- graph-query: entity, entityByAlias, batch, relationships, pathSearch, hierarchyStats, prefix, spatial, temporal,
  semantic, similar, globalSearch, summary, searchGraph, byName, and localSearch;
- graph-ingest reads: entity, batch, prefix, and suffix;
- graph-ingest mutations: create, replace, append, and delete;
- graph-clustering: community, members, entity, and level;
- graph-embedding: similar, search, and status;
- graph-index-spatial: bounds and polygon;
- graph-index-temporal: range;
- agentic-loop: in-flight and the uncommitted trajectory endpoint;
- agentic-tools: tool list; and
- ObjectStore API: get, store, and list on one multiplexed endpoint.

The census excludes tests, commands, ordinary core-NATS publications, JetStream consumers, and fire-and-forget
subscriptions. Those are adjacent payload-size territory but are not successful request/reply response carriers.

## Shared carrier behavior

`natsclient.SubscribeForRequests` calls the handler, converts handler errors into the shared classified error reply,
and sends successful bytes with `msg.Respond` (`natsclient/request.go:342-397`). If successful response publication
fails, it logs the subject and error and returns. It does not send a smaller classified reply, and the handler cannot
observe that publication failure because it already returned.

The ObjectStore API does not use this helper. Its local `respond` marshals a `Response`, calls `msg.Respond`, and logs
publication failure (`storage/objectstore/component.go:639-660`). Its `get` action calls the materializing
`Store.Get`, embeds the complete body in JSON, and returns one message (`storage/objectstore/component.go:402-430`).

One production package implements a public client for that full-body action:
`graph/llm.NATSContentFetcher` defaults to `storage.objectstore.api`, requests action `get`, materializes the response,
and extracts title, abstract, or a short body fallback (`graph/llm/nats_content_fetcher.go:17-220`). It is dormant:

```sh
rg -n "NewNATSContentFetcher|WithContentSubject|WithContentFetcher\(" \
  --glob '*.go' --glob '!graph/llm/nats_content_fetcher.go'
```

finds only the `WithContentFetcher` option definition in `graph/clustering/summarizer.go`; no production or test call
constructs `NATSContentFetcher` or injects any `ContentFetcher`. The clustering summarizer conditionally calls an
injected fetcher for representative entities (`graph/clustering/summarizer.go:508-533,566-618`), but production
construction omits that option (`processor/graph-clustering/component.go:2168-2175`). The active semantic-tier change
records the same missing wiring as #829 (`openspec/changes/semantic-tier-split/proposal.md:74-91`).

This is an exported, production-capable adopter surface with no current in-repository runtime birth. The ObjectStore
README and default port configuration advertise it (`storage/objectstore/README.md:20-100`;
`storage/objectstore/config.go:78-86`). `WithContentSubject` also permits adopters to rename the subject. The fetcher
uses `StorageReference.Key` but ignores `StorageReference.StorageInstance`, choosing a separately configured subject
instead (`graph/llm/nats_content_fetcher.go:117-176`).

The package GoDoc advertises a conflicting subject, `storage.api`, in its client example
(`storage/objectstore/doc.go:135-153`). Repository-wide search finds that spelling only in generic port and flowgraph
fixtures; no ObjectStore configuration or client uses it. It is a stale adopter-visible catalog entry, not a second
runtime endpoint.

ObjectStore API acquisition depends on the effective port set. Configurations that inherit default ObjectStore ports
receive the optional `api` input; configurations that completely replace the ports with `write` and `stored` do not
subscribe to it (`storage/objectstore/component.go:300-315`). No shipped JSON configuration declares an `api` override
or alternate ObjectStore API subject.

SemSource configures the SemStreams ObjectStore with default ports and comments that `StorageReference` content is
dereferenced through `storage.objectstore.api`
(`/Users/coby/Code/c360/semsource/cmd/semsource/run.go:878-894`). It has no executable action-`get` caller and currently
hydrates through StoreRegistry and base `Store.Get`, but the comment and inherited default are a downstream adopter
expectation, not proof of a runtime client.

No production responder calls `nats.Conn.MaxPayload()`. The search

```sh
rg -n "MaxPayload\(" natsclient graph processor storage gateway agentic pkg --glob '*.go'
```

returns no production match. Static byte values therefore predict a server/account limit rather than observing the
connected server's negotiated value.

## Response-shape inventory

The response population contains both individually unbounded bodies and collections whose size grows with stored
state.

- **Exact or batch authority reads:** graph-ingest entity and batch return full `EntityState` values. One entity can
  exceed a static page budget; batch size does not bound encoded bytes.
- **Mutations:** create, replace, and append can return full entities. Batch-shaped results grow with input
  cardinality.
- **Derived indexes:** relationship arrays, predicate results, name matches, communities, spatial, temporal,
  embedding, and tool lists have independent limits and no shared encoded-response bound.
- **Composite graph queries:** relationship, GraphRAG, summary, path, hierarchy, spatial, temporal, semantic, and
  similar operations compose or proxy other responders. The lower producer does not know the final wrapping size.
- **Stored bodies:** ObjectStore `get` materializes and JSON-wraps the complete object. The uncommitted trajectory
  reader can similarly hydrate all evidence bodies into one response.

The dormant NATS content-fetch path materializes the object repeatedly: native `GetBytes`, ObjectStore `Response.Data`,
the complete Core NATS reply, the decoded outer response, and decoded `StoredContent`. Only after full transfer does it
select title/abstract or truncate a body fallback to 250 characters (`graph/llm/nats_content_fetcher.go:153-218`). That
250-character setting bounds prompt content, not storage I/O or the NATS response. Oversize or fetch failure is logged
at Debug, skipped per entity, and returned as a partial aggregate with nil error
(`graph/llm/nats_content_fetcher.go:117-147`).

## Existing bounds and continuation spellings

The repository already has several non-equivalent mechanisms.

1. `graph.query.prefix` has an opaque keyset cursor and a static 800 KiB response budget
   (`graph/query_prefix_types.go:11-94`; `processor/graph-ingest/query.go:237-425`). It still admits the first entity
   when that entity alone exceeds the budget. NATS KV has no ranged scan, so every page first lists and sorts the
   matching key population.
2. Graph-gateway accepts no prefix cursor and projects the internal `{entities,next_cursor}` response to a bare
   `[Entity]`, discarding continuation (`gateway/graph-gateway/component.go:1082-1094,1855-1910`; schema at
   `gateway/graph-gateway/component.go:1652`). This is the live mechanism behind open issue #884.
3. The production `graph/query.QueryPrefixAll` helper follows those cursors, accumulates entities up to a required
   caller-supplied `maxEntities`, and separately returns `truncated`. It has no unbounded mode and no page-at-a-time
   callback (`graph/query/prefix.go:67-138`).
4. Path search carries `truncated`; globalSearch and searchGraph carry `entities_truncated`; graph summary carries
   `entity_sample_truncated` (`processor/graph-query/pathrag.go:35-40`;
   `processor/graph-query/graphrag.go:127-157`; `graph/query_summary_types.go:76-85`). Graph-gateway's raw object
   response forwarding preserves these fields on the wire, but introspection advertises the summary marker only:
   `PathSearchResult` omits `truncated`, and `GlobalSearchResult` omits `entities_truncated`
   (`gateway/graph-gateway/component.go:1329-1347,1684-1698`). This differs from prefix, whose projection deletes
   `next_cursor` from the response itself.
5. Spatial and temporal reads use count limits but expose no continuation. Spatial scope/pagination remains open as
   #885.
6. Agent tools expose `has_more`, `next_offset`, and `next_cursor` metadata, with caller-specific offset or cursor
   meanings (`agentic/tools.go:479-536`; `processor/agentic-loop/result_hint.go:66-85`).
7. `read_loop_result` uses caller-visible byte offset and `max_bytes`, slicing an already materialized result
   (`processor/agentic-tools/loop_result.go:24-155`).
8. Fusion accepts caller-provided node and byte limits (`pkg/fusion/contract.go:38,314`). The current implementation
   can admit one individually oversized node before stopping.
9. Lifecycle gateway uses HTTP offset/count pagination (`pkg/lifecycle/list_options.go:52`;
   `gateway/lifecycle-gateway/handlers.go:234-238`).
10. Generic HTTP gateway caps request bodies, not responses (`gateway/http/http.go:204`;
   `gateway/types.go:102`). Graph-gateway decodes its GraphQL body without an explicit size-limited reader
   (`gateway/graph-gateway/component.go:1912-1938`).

The older issue #176 contains a proposal to use a new JetStream subscription for bulk reads. That proposal is
adjacent historical territory, not a current runtime primitive. The `kv-or-stream` heuristic classifies a read as
neither a durable work request nor an unacknowledged-work queue; no production bulk-read stream was found by:

```sh
rg -n "bulk.*subscribe|subscribe.*entities|graph\.bulk" --glob '*.go' --glob '*.json' --glob '*.md'
```

outside the issue/design discussion.

## Native NATS ObjectStore inventory

The pinned `nats.go` ObjectStore already supplies sequential chunked blob transfer:

- `ObjectStore.Put(ctx, ObjectMeta, io.Reader)` streams writes; the default chunk size is 128 KiB
  (`.../nats.go@v1.52.0/jetstream/object.go:111,486,638-705`).
- `ObjectStore.Get` returns `ObjectResult`, an `io.ReadCloser`
  (`.../nats.go@v1.52.0/jetstream/object.go:134-143,836`). Full digest verification completes only after the reader
  reaches EOF; closing early does not establish whole-object verification.
- The API also exposes info, list, watch, delete, links, bucket links, seal, and status
  (`.../nats.go@v1.52.0/jetstream/object.go:172-244`). Delete writes deletion metadata and purges the object's chunk
  subject (`.../nats.go@v1.52.0/jetstream/object.go:955-1005`).
- Bucket configuration includes TTL, MaxBytes, storage, replicas, placement, compression, and metadata. Default TTL
  is no expiry (`.../nats.go@v1.52.0/jetstream/object.go:255-293`). Chunk size is instead per-object writer metadata
  in `ObjectMetaOptions`; `Put` supplies the 128 KiB default when that option is absent
  (`.../nats.go@v1.52.0/jetstream/object.go:350-381,479-486,638-650`).
- `GetObjectOpt` exposes show-deleted behavior only. No offset, byte-range, seek, or partial-get option exists in
  v1.52.0 (`.../nats.go@v1.52.0/jetstream/object.go:435-450`). Native partial PUT/GET remains requested in nats.go
  issue #1021.

ObjectStore's internal chunking therefore does not make a one-message request/reply response paginated, and it does
not provide an application-level resume offset for an opened object.

## SemStreams storage surface

The base `storage.Store` materializes both directions: `Put([]byte)` and `Get() []byte`
(`storage/storage.go:51-87`). SemStreams already has the optional `storage.StreamableStore.Open`, returning an
`io.ReadCloser` (`storage/storage.go:89-101`). The NATS ObjectStore backend implements it with native `Get`
(`storage/objectstore/store.go:338-354`), while its base `Get` still uses `GetBytes`
(`storage/objectstore/store.go:300-336`).

No backend-neutral streaming-write interface was found:

```sh
rg -n "PutReader|PutStream|io\.Reader" storage --glob '*.go'
```

Only read-side `Open` and backend/native internals match. `Open` begins at byte zero and its `io.ReadCloser` contract
does not promise seek or ranges.

SemSource has downstream field evidence for this exact asymmetry: its local file store added `PutReader`, while its
version-diff hydration resolves full bodies before applying the cumulative response budget. Its SemStreams fusion
resolver calls base `Store.Get`, not optional `StreamableStore.Open` (`pkg/fusion/hydrate.go:88`). SemSource is holdout
evidence; it does not set the framework contract.

## Same-class collision table

| Dimension | Inventory entry |
|---|---|
| Semantic class | Response carriage, continuation, and large-body retrieval overlap but differ. |
| Owners | Shared responder, local responder, handlers, gateways, tools, and storage. |
| Catalogs | Ports name routes; GraphQL/OpenAPI independently name result shapes. |
| Status | Publish failure is a responder log followed by requester timeout. |
| Lifecycle | Component subscription, stateless marker, caller offset, or reader lifetime. |
| Ownership | Transport, semantic handler, public projection, and storage provider differ. |
| Readers | GraphQL, components, agent tools, E2E, and downstream products. |
| Writers | Forty-five shared-helper endpoints and one direct endpoint. |
| Recovery | No reply replay or shared response-resume protocol exists. |

Details attached to the table:

- No canonical response-too-large status or code was found, and component health does not observe the per-request
  loss.
- Request subscriptions start and stop with components. Prefix cursors are stateless key markers, tool offsets are
  caller state, and object readers live until EOF or Close.
- Transport owns final publication; handlers own semantic results; gateways own public projection; storage providers
  own object handles. Call sites currently cross these boundaries by predicting carrier size.
- Native ObjectStore writers and `storage.Store.Put` write evidence separately from reply publication.
- Stateless continuation reissues reads. ObjectStore sequential reads can reopen only from byte zero.
- The local ObjectStore responder overlaps the registered `storage.Store`/`StreamableStore` access plane. Its sole
  in-repository client implementation is exported and production-capable but has no current runtime construction.
- ObjectStore's README and default port advertise `storage.objectstore.api`; package GoDoc instead tells adopters to
  call nonexistent `storage.api`.
- The ObjectStore API appears or disappears through complete port replacement, and its dormant fetcher selects by an
  independently configured subject rather than the `StorageReference.StorageInstance` owner.

## Adjacent claims and issue territory

- #857 inventories payload-size failures across KV, publish, request/reply, and storage paths. Its request/reply row is
  the same silent-success-publication mechanism recorded here; its write-side rows are adjacent but outside this
  response inventory.
- #839 directly owns the unbounded graph-ingest batch response and requires it to share any eventual ceiling
  mechanism with graph-size-growing community membership. It is direct response-bound territory, while its community
  storage requirement is adjacent write-side territory.
- #833 owns caller/responder deadline propagation in `natsclient.SubscribeForRequests`. It operates at the same shared
  responder and externally indistinguishable timeout seam, but concerns cancellation/deadline attribution rather than
  response byte size.
- #829 and the active `semantic-tier-split` change own the missing clustering content-enrichment behavior. The current
  `NATSContentFetcher` is not wired; any later design must distinguish that behavioral need from preserving its
  full-body NATS transport.
- #884 records graph-gateway loss of prefix continuation.
- #885 records spatial scoping and pagination requirements.
- #306 records prefix's static byte budget, individually oversized first entity, and full-scan-per-page backend cost.
- #176 is historical bulk-read/pagination discussion; parts of its premise predate shipped prefix paging and its
  proposed new bulk stream has no production owner.
- The accepted Foundation B trajectory contract requires full evidence in ObjectStore, observed-only reads, no
  automatic expiry, and no completeness claim. This inventory does not change those rulings.
- The later retention program owns coordinated fact expiry and reference-aware evidence reclamation. ObjectStore TTL
  and delete capability exist but do not by themselves express reference reachability.

## Adopter seam inventory

| Adopter | Current default outcome | Desired burden |
|---|---|---|
| Request handler author | Oversized valid success is logged and the caller times out. | No carrier-size prediction. |
| Collection API author | Results can lose or fail to advertise truncation truth. | One end-to-end result contract. |
| Large-evidence producer | Base `Store.Put` materializes the body. | Declared sequential write capability. |
| Large-evidence consumer | Base `Get` materializes; optional `Open` starts at zero. | No NATS internals. |
| Public GraphQL caller | Prefix continuation is discarded. | Preserve required result truth. |
| Operator | Several failure classes appear as the same timeout. | Stable classified failure. |

Current knowledge and discovery burden:

- A request handler author must predict the server ceiling plus JSON/envelope overhead; this is discoverable only
  through a responder log, source, or issue.
- A collection author must learn whether cursor, marker, count, byte offset, or `has_more` applies; whether the
  gateway preserves it; and whether public discovery advertises it. This is scattered across source and docs.
- A producer learns at compile time that base `Store.Put` needs complete bytes even when the backend streams.
- A consumer must type-assert `StreamableStore`; complete native digest verification requires reading through EOF.
- The dormant LLM content fetcher instead knows the ObjectStore API subject, request action, response envelope, and
  body shape. Doing nothing leaves it unwired, so clustering receives no enrichment from this code path.
- A SemSource maintainer sees a comment promising dereference over `storage.objectstore.api`, even though executable
  hydration currently uses StoreRegistry. Removing the API is therefore a documented clean break.
- An adopter following ObjectStore package GoDoc calls stale `storage.api` and receives no responder; the actual
  default is discoverable only in README, configuration, or source.
- A GraphQL caller has nowhere in the response to discover discarded prefix continuation.
- An operator needs responder logs to distinguish oversized success from handler latency, no responder, or network
  failure.

## Closed searches and open evidence questions

Closed by repository search:

- no production `MaxPayload()` observation;
- no backend-neutral streaming-write storage capability;
- no native/SemStreams range or seek read contract;
- no shared response-too-large classification;
- no production graph bulk-read JetStream subscription;
- no second direct production `msg.Respond` responder outside the shared helper and ObjectStore API.
- no in-repository production or test call to `NewNATSContentFetcher` or `WithContentFetcher`;
- no ObjectStore configuration or client using the package GoDoc's stale `storage.api` spelling.

Open evidence questions for the post-inventory design phase:

- Which of the 48 operations can produce one individually oversized semantic item, versus only an oversized
  collection?
- Which downstream consumers, if any, rely on the advertised ObjectStore request API despite the absence of an
  in-repository runtime consumer?
- Which admitted future content-enrichment consumer requires sequential whole-body streaming, and which requires
  resumable random access?
- What exact final encoded size does the NATS client reject for messages with reply/error headers on the connected
  server versions used in CI and supported deployments?
