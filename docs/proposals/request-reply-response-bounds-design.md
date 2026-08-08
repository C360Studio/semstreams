# Design: bounded request/reply responses and reference-only large evidence

## Status and checkpoint identity

Draft for pre-owner design review. This artifact recommends a target; it does not approve it.

- Repository baseline: `4d3ea2ff5db69b40840c51ef76a3e2f730edef62`.
- Accepted inventory: `docs/proposals/request-reply-response-bounds-inventory.md`, 344 lines, SHA-256
  `26ea5b020e1f292ee646dfd45115bf753e0ac392493a6d672e5743c2336e182e`.
- The baseline statement includes the uncommitted Foundation B trajectory-read slice as identified by the inventory.
- Compatibility posture: pre-v1 clean break; no alias, shim, dual response, or deprecated path.

The accepted inventory is incorporated verbatim by reference and controls over every summary below.

## Problem boundary

The inventory measured 46 production request/reply subscription endpoints representing 48 operations. Forty-five use
`natsclient.Client.SubscribeForRequests`; the optional ObjectStore API is the sole direct responder and multiplexes
`get`, `store`, and `list`.

A handler success that exceeds the connected NATS server's maximum payload is logged at the responder and becomes a
requester timeout. No production responder observes `nats.Conn.MaxPayload()`.

The graph mutation operations in that population are exactly:

- `graph.mutation.entity.create`;
- `graph.mutation.entity.reconcile`;
- `graph.mutation.triple.append`; and
- `graph.mutation.entity.delete`.

Pagination is not the common missing primitive. Current operations use incompatible ordering, cursor, offset, count,
byte, truncation, and completeness semantics. The common defect is narrower: the carrier silently loses a completed
success that cannot fit, while some result owners and public projections also lose their own continuation truth.

The ObjectStore correction is load-bearing:

- `graph/llm.NATSContentFetcher` is exported and production-capable but has no runtime construction or injection in
  this repository.
- It materializes a full ObjectStore `get` reply before selecting title/abstract or truncating a body fallback.
- The default/README advertise `storage.objectstore.api`; package GoDoc advertises stale `storage.api`.
- The API may disappear when a configuration completely replaces default ObjectStore ports.
- SemSource documents the API as expected framework surface, although its executable hydration uses StoreRegistry.
- Repository evidence cannot prove that no external adopter uses the advertised API.

## Decision skills

### `kv-or-stream`

A synchronous query answer is neither a current-fact watch nor queued work. Retain Core NATS request/reply for admitted
bounded operations. Do not add a JetStream overflow stream, response KV bucket, or generic work consumer.

Large durable bodies remain in ObjectStore. Internal authorized consumers use registered `storage.Store` capabilities;
bounded request/reply messages carry metadata and references.

### `query-pattern`

- External controlled callers use enumerated operations on the existing HTTP graph facade.
- Internal services use operation-specific typed adapters or registered Store handles.
- Projection owners/operators use their declared bucket seam or diagnostics.
- No MCP graph surface, general embedded graph client, raw KV fallback, raw subject fallback, or new gateway is added.

Graph-gateway is GraphQL-shaped routing, not a selection-set executor. Continuation fields must be explicit in the
facade result and introspection; requesting fewer GraphQL fields does not shrink the internal NATS reply.

## Options considered

### A. Do nothing

Completed oversized successes remain timeouts; prefix continuation remains publicly lost; trajectory hydration can
exceed the carrier; and the ObjectStore RPC remains a duplicate, inconsistently acquired access plane.

Cost: callers predict broker limits and operators diagnose permanent size failures as availability failures.

### B. Shared refusal only; retain ObjectStore RPC

Classify `nats.ErrMaxPayload` from `SubscribeForRequests`, and give the direct ObjectStore responder the same helper.

Benefit: the timeout becomes classified.

Cost: large objects still cannot traverse one reply. The instance-blind fetcher, stale subject catalog, full
materialization, optional acquisition, and duplicate storage plane remain. Preserving the bypass also creates an
exported response helper whose only additional consumer is the bypass itself.

### C. Universal pagination or overflow protocol

Add a generic page envelope, cursor registry, chunk protocol, JetStream result stream, response bucket, or automatic
ObjectStore overflow reference.

Cost: ordering, mutation, ranking, restart, authorization, retention, abandonment, and cleanup differ by operation.
This would create another framework beside the typed results and native ObjectStore streaming.

### D. Public streamed trajectory evidence now

Add an HTTP body operation or let graph-gateway borrow StoreRegistry.

Cost: no concrete public consumer currently establishes authorization, content-type, integrity, cancellation, or
resume requirements. StoreRegistry is already injected as a framework dependency, but making graph-gateway dereference
and stream arbitrary storage bodies would add storage authorization, integrity, and lifecycle policy to a query facade
that does not own them.

### E. Shared carrier admission, result-owned continuation, one storage plane

1. Make `natsclient` observe and enforce the active carrier limit.
2. Return classified `response_too_large` rather than an impossible success.
3. Give result owners a narrow observed limit when they need exact page fitting.
4. Preserve existing prefix pages through GraphQL.
5. Make trajectory reads paginated fact metadata/reference only.
6. Keep full evidence in ObjectStore without transporting bodies through trajectory RPC.
7. Cleanly delete the optional ObjectStore RPC and dormant NATS fetcher.
8. Consolidate future #829 enrichment on StoreRegistry and `StreamableStore`.

Recommended.

## Target contract

### Carrier admission belongs to `natsclient.Client`

`Client.SubscribeForRequests` always attempts to publish the handler's final successful bytes first.

The classified error detail is bounded and observed:

```json
{
  "response_bytes": 1234567,
  "max_payload": 1048576
}
```

`invalid` is the existing non-retry class: an identical request and representation cannot fit the same carrier. The
stable code distinguishes this condition from other invalid requests. The numeric detail is diagnostic evidence, not
an adopter byte-budget knob.

If publication returns `nats.ErrMaxPayload`, the client has rejected
the message before queueing it; the responder then attempts the classified `response_too_large` reply using the newly
observed limit. Any other publication error follows the existing bounded log path. This actual publish result is the
authoritative carrier admission outcome.

`MaxPayload()` is used only for result-page fitting and diagnostic detail. It is not copied into configuration, cached
at subscription time, or used to skip the first publication attempt. NATS may deliver asynchronous `INFO` between an
owner's page-fit observation and publication; the actual publish result remains authoritative.

If the small error reply itself cannot publish, the existing bounded publish-failure log remains the outcome. This
slice adds no speculative metric.

No exported success-response helper or generic response wrapper is introduced. Deleting the direct ObjectStore
responder leaves every production endpoint on `SubscribeForRequests`.

### Result owners may observe, but never configure, the carrier limit

Add one narrow exported observation:

```go
func (c *Client) MaxPayload() (int64, error)
```

It returns the active connected server limit or a connection error. It exposes no connection handle and accepts no
override. Its present consumers are graph-ingest prefix page fitting and the trajectory fact page builder. New exported
`natsclient` surface requires the owner approval represented by this design.

`Client.MaxPayload()` supports result-page fitting but never proves a later publication will succeed. The shared
publish-and-classify path remains authoritative.

The shared carrier does not trim, split, store, or reinterpret success bytes. Each collection result independently
defines:

- ordering and cursor meaning;
- page versus wider totals;
- insertion/deletion behavior between reads;
- exhaustion and truncation;
- treatment of one indivisible item that cannot fit.

Unconverted operations receive the shared refusal. No `Page[T]`, pagination registry, or generic overflow envelope is
introduced.

### Prefix GraphQL preserves and exactly fits the existing page

The internal `graph.PrefixQueryResponse` remains `{entities,next_cursor}`. Graph-gateway stops projecting it to a bare
array.

The public facade changes cleanly to:

```text
entitiesByPrefix(prefix: String!, limit: Int, cursor: String): EntityPage

EntityPage:
  entities: [Entity]
  next_cursor: String
```

Graph-ingest replaces the static 800 KiB prediction with exact encoded-page fitting against `Client.MaxPayload()`.
The page builder includes its final cursor in the marshaled candidate. When at least one item fits, it returns that
page and continuation. If the first indivisible entity cannot fit, it returns `response_too_large`.

Graph-gateway forwards `cursor`, validates the complete typed response, preserves the envelope, and advertises the
page type and argument. There is no legacy list alias.

This closes #884 and the first-entity silent carrier failure in #306. It does not change the current full KV scan on
each page.

### Trajectory reads are bounded fact pages, never body transport

The uncommitted request changes from `{loopId,limit,hydrateEvidence}` to `{loopId,limit,cursor}`.
`hydrateEvidence` is deleted and rejected; no ignored legacy field remains.

Limit semantics are fixed:

- omitted or zero uses `64` facts;
- negative is `invalid_request`;
- values `1..256` are accepted; and
- values above `256` are rejected, not silently clamped.

The limit bounds the returned result page, not storage work. Because fact keys are attempt-ID based, every page still
prefix-lists, fetches, validates, and sorts every currently visible fact before cursor selection. No storage scan bound
is claimed.

The response remains observed-only:

```text
schema_version
loop_id
coverage: observed
observed_totals
terminal_observed
facts
next_cursor
```

Facts contain bounded `TrajectoryFactV1` metadata: evidence state, digest, size, and optional `StorageReference`. They
contain no evidence body or read-time hydrated status.

The cursor is base64url without padding over strict canonical JSON:

```text
version: "v1"
loop_digest: sha256(requested loop ID)
iteration
phase_rank
source_ordinal
attempt_ordinal
attempt_id
```

Unknown or missing fields, malformed base64/JSON, unsupported version, invalid tuple values, and a loop digest that
does not match the requested loop return ADR-060 `invalid/invalid_cursor` before KV listing. Callers treat the token as
opaque. The encoded tuple is the exact last returned causal tuple; comparison is lexicographic in the accepted tuple
order.

The operation:

1. prefix-lists and validates visible facts;
2. sorts by the accepted causal tuple;
3. advances strictly after the opaque last-tuple cursor;
4. applies the request's fixed result-page limit;
5. exactly marshals candidate responses against `Client.MaxPayload()`; and
6. returns the largest fitting fact page plus continuation.

`observed_totals` and `terminal_observed` describe only the returned page. An absent `next_cursor` means no later fact
was visible during that read; it is not an audit-completeness claim. Concurrent observations that sort before an
already-issued cursor may not appear on later pages, consistent with documented keyset semantics and
`coverage: observed`.

One bounded fact that cannot fit receives `response_too_large`. There is no caller byte knob.

Graph-gateway exposes the same cursor and page truth and deletes `hydrateEvidence`, `evidence_body`, and
`TrajectoryEvidence` from this query surface. Agentic-loop and gateway perform no Store read during a trajectory query.

Full canonical evidence remains stored and digest-verified under the accepted registered-store contract. Authorized
internal/operator code may resolve the `StorageReference` and use `StreamableStore.Open`. No public streamed retrieval
operation is admitted until a concrete consumer justifies it.

### Amend Foundation B's hydration obligation

The accepted statement "GraphQL production-path body hydration" cannot hold for an ObjectStore body larger than one
NATS reply. Replace it with:

> GraphQL and typed internal trajectory reads return paginated observed fact metadata and durable evidence references
> only. Full evidence remains in the registered Store. No trajectory request/reply operation carries evidence bodies.

Update the Foundation B proposal, design, tasks, approval amendment, agentic-loop delta, and gateway-response-
projection delta. Amend `docs/proposals/agentic-trajectory-target-contract.md` at both the canonical reader step and
the required-test list, or explicitly supersede those hydration clauses from this accepted design. Preserve full
capture, digest verification, registered-store ownership, non-blocking degradation, and no-completeness rulings.

### Delete the optional ObjectStore RPC cleanly

Delete:

- default ObjectStore `api` input and subscription;
- `get`, `store`, and `list` action dispatch plus RPC-only DTOs;
- the local direct `msg.Respond` path;
- `graph/llm.NATSContentFetcher` and its NATS-specific options;
- README/GoDoc instructions for both API subject spellings; and
- tests/schema whose only purpose is the RPC.

Configuration validation rejects every input named `api` and every ObjectStore `nats-request` input at construction.
An unmigrated explicit configuration therefore fails boot; it cannot start with an inert port. Ordinary `nats` and
`jetstream` write inputs remain admitted.

Retain:

- ObjectStore's `StoreProvider` role and ordinary component flow ports;
- `storage.Store`, `storage.StreamableStore`, and StoreRegistry lifecycle;
- backend-neutral `llm.ContentFetcher` and `clustering.WithContentFetcher` for the separately owned #829 behavior.

This is a documented break. SemSource must remove/correct its stale API expectation when it migrates. Its executable
StoreRegistry hydration remains compatible, but repository evidence does not prove unknown adopters absent.

Delete every surviving API claim, including registration/package comments and `StoredMessage` guidance that tells
consumers to retrieve through the ObjectStore API. The remaining guidance names registered Store access only.

### Future #829 enrichment uses the registered Store

This design does not close #829. A later direct content fetcher must:

- lazily resolve `StorageReference.StorageInstance` through StoreRegistry;
- avoid cached/closed borrowed handles;
- prefer `StreamableStore.Open` for sequential reads;
- distinguish provider-unavailable, missing object, and backend failure;
- preserve explicit partial degradation; and
- acquire only the prompt information its operation contract needs.

Native `Open` does not supply range or selective JSON-field retrieval. No remote store RPC is inferred from #829.

## Adopter seam in the target

| Adopter | Does nothing | Must know |
|---|---|---|
| Fixed-shape handler author | Success publishes or receives classified refusal. | Nothing about `max_payload`. |
| Collection owner | Carrier failure is loud; semantics remain local. | Its result ordering/continuation only. |
| Prefix HTTP caller | Receives a page with visible continuation. | Cursor is opaque; empty means exhausted. |
| Trajectory caller | Receives observed fact metadata/reference pages. | Cursor and page-local totals only. |
| Internal content reader | Resolves declared Store instance and streams. | Close/read-to-EOF integrity contract. |
| Operator | Sees classified size failure rather than timeout. | No broker-size diagnosis arithmetic. |

No external adopter calculates a byte limit, NATS subject, ObjectStore chunk size, or storage bucket.

## Implementation slices and verification

### Slice 1: carrier admission

- Add `Client.MaxPayload()` with connected/disconnected tests.
- Attempt every shared success publication and classify only `nats.ErrMaxPayload` as `invalid/response_too_large`.
- Prove below/exact-limit bytes publish unchanged.
- Prove over-limit returns classified error through `RequestClassified`, not timeout.
- Prove the oversized success bytes are never published and ordinary handler errors remain unchanged.
- Prove the small refusal publish failure remains logged.
- Force a page-fit/publish limit change and prove `nats.ErrMaxPayload` triggers the classified refusal without
  publishing the oversized success.

### Slice 2: prefix facade

- Fit the complete typed page against the observed limit.
- Forward and preserve cursor through GraphQL.
- Advertise `EntityPage` and remove the bare-list shape.
- Prove stable multi-page traversal without duplication.
- Prove one indivisible oversized entity returns classified refusal.

### Slice 3: trajectory reference-only paging

- Reject and remove every hydration/body field from config, schema, and runtime.
- Prove deterministic causal paging and invalid-cursor rejection.
- Prove v1 cursor encoding, strict decode, loop binding, foreign-loop refusal, and unsupported-version refusal.
- Prove zero/default, negative, maximum, and above-maximum limit behavior.
- Prove page-local totals/terminal observation and explicit continuation.
- Prove no Store read occurs during query.
- Prove full evidence remains stored/verifiable through registered-store tests.
- Prove restart reads use KV facts, not cache or `TrajectoryManager`.

### Slice 4: ObjectStore RPC deletion

- Prove default ports contain no `api` and no API subscription/DTO/fetcher remains.
- Prove explicit `api` and arbitrary `nats-request` inputs fail component construction.
- Delete both subject spellings from README/GoDoc.
- Delete API claims from registration, package, and `StoredMessage` comments.
- Prove ObjectStore still registers `StreamableStore` and ordinary storage flows work.
- Keep graph embedding, fusion, trajectory evidence, and shipped `AGENT_CONTENT` configurations green.

### Slice 5: #829 later

Only after its own accepted inventory/design: inject lazy StoreRegistry enrichment, select by `StorageInstance`, avoid
cached handles, keep large content off Core NATS, and validate semantic-tier output against corpus content.

## Issue disposition

| Issue | Disposition |
|---|---|
| #857 | Fix only silent request/reply success loss; write-side members remain. |
| #839 | Remains: refusal is not batch/community continuation. |
| #833 | Remains independently owned deadline propagation. |
| #829 | Remains; later implementation uses registered Store, not RPC. |
| #884 | Close after the prefix page lands. |
| #885 | Remains; spatial scope/order/cursor require their own contract. |
| #306 | Partial: observed page fitting/refusal; full-scan cost remains. |
| #176 | Supersede after owner ruling; no generic bulk-result stream. |

## Durable artifact placement

Seed a `request-reply-response-bounds` capability spec for carrier admission and result-owned continuation. Modify
Foundation B's `agentic-loop` and `gateway-response-projection` deltas and its proposal/design/tasks/approval amendment.
No ADR is recommended: carrier classification extends ADR-060, while storage consolidation follows existing registered-
store ownership. Mechanics belong in specs.

## Release gates

The GraphQL prefix shape, trajectory hydration deletion, and ObjectStore RPC deletion are breaking. Required gates:

- focused natsclient, graph-ingest, graph-gateway, agentic-loop, ObjectStore, clustering, and StoreRegistry tests;
- `task lint`, `task build`, and both tagged vet runs;
- `go test -race ./...` and `task test:integration`;
- schema generation with reviewed schema/spec diff;
- contract tests and OpenSpec validation;
- `task e2e:agentic`, `task e2e:semantic`, `task e2e:all`, and `task e2e:research-graph`;
- independent SemStreams implementation review; and
- post-change responder/adopter inventory.

## Owner rulings required

1. Delete the optional ObjectStore RPC and dormant NATS fetcher, accepting the documented SemSource and unknown-adopter
   break.
2. Amend Foundation B from GraphQL evidence hydration to paginated metadata/reference-only trajectory reads.
3. Add narrow observed `Client.MaxPayload()` and classify oversized success as `invalid/response_too_large`.
4. Break GraphQL prefix from `[Entity]` to `EntityPage` with no alias.
5. Supersede #176's generic bulk-stream direction with operation-owned continuation.

Until independent design review and explicit owner acceptance, this remains a recommendation.
