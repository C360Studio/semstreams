# Raw-Lane Plus Current-State Projection

High-rate and binary-ish feeds — MAVLink frames, ADS-B snapshots, TAK/CoT XML, SAPIENT
JSON/protobuf, KLV/MISB media — share one shape: most of the payload should never become a graph
entity, but a small, governed slice of *current state* should. This guide is the recipe for that
split. It composes primitives you already have (`ContentStorable`/`StorageReference`, JetStream
streams, the KV Twofer, indexing profiles, and governed semantic state) — it is **not** a new
framework layer. There is no mandatory raw-payload service and no required object store.

The hard part is not the plumbing. It is **ownership**: once you project "current state" from
several feeds, you are one careless write away from a silently wrong picture. So this guide is
failure-first — it shows the break before it sells the fix.

## 1. The break: why you care about ownership

Two sources report the same aircraft. ADS-B projects its altitude onto a track entity. A beat
later a replay job — or a second feed, or a reconnecting producer with stale data — writes an older
altitude to the same entity with a plain triple write.

Nothing errors. The map now shows the wrong altitude, and no log line said so. That is the failure
mode governed semantic state exists to prevent ([28-governed-semantic-state.md](28-governed-semantic-state.md)),
and it is exactly what a COP (common operating picture) invites by definition: many feeds writing
the current state of shared real-world objects.

You can watch this happen today, because owner leases ship **observe-only by default**
(`enforce_owner_lease` off): a mismatched write still commits, but it is metered and logged. The
owner-lease integration tests are the runnable before/after — no faith required:

```bash
# observe-only (default): the stale write COMMITS; owner_lease_mismatch_total increments.
go test -tags=integration -run TestIntegration_OwnerLease_CreateWithTriples_StaleToken ./processor/graph-ingest/

# enforce on: the same stale write is REJECTED with owner_lease_stale; state stays correct.
go test -tags=integration -run TestIntegration_OwnerLease_Enforce_CreateWithTriples_StaleRejected ./processor/graph-ingest/
```

The rest of this guide earns each primitive by the harm in that scenario it removes.

## 2. Two lanes: what stays off-graph, what becomes state

Split every feed into two lanes:

| Lane | Carries | Lives in |
|------|---------|----------|
| **Raw lane** | the frames/bytes/blobs — the firehose | a bounded JetStream stream (+ replay), and ObjectStore by reference for big/binary |
| **Governed state** | the small slice you query as a *fact* (a track's current position, a detection, a command ACK) | `ENTITY_STATES`, written through graph-ingest under an ownership contract |

The decision rule for "which bytes stay off-graph" (Q1): **if you would never query a *field* of it
as a fact, it does not belong in the graph.** Raw MAVLink frames, KLV media, opaque protobuf — off
to the raw lane. The current track derived from them — into governed state.

This is the boundary [28-governed-semantic-state.md](28-governed-semantic-state.md) already states:
*"Do not use [governed state] for high-volume opaque execution traces, raw telemetry streams, or
one-shot requests. Those belong in JetStream streams, ObjectStore, or component-specific buckets
with graph references."* This guide is how to do that for feeds.

**Bounded raw-lane handling (high-rate / binary):** use a bounded JetStream stream for the record
and replay; offload big or binary payloads to ObjectStore via `ContentStorable` /
`StorageReference`; and put `pkg/buffer.CircularBuffer` in front of slow consumers for in-process
backpressure — it has explicit overflow policies (`DropOldest`, `DropNewest`, `Block`) and built-in
statistics, so a saturated lane drops deliberately instead of blocking the feed or OOMing.

## 3. What crosses to the graph: state plus provenance, never bytes

A projector writes two things onto the current-state entity:

1. **The current-state facts** — the small governed slice (position, status, detection).
2. **A provenance reference** back to the raw record — *not the bytes*.

The reference is the existing `StorageReference` (`message/storable.go`), and it is **storage-backend
agnostic**: `StorageInstance` is just a name — `"message-store"`, `"cache-1"`,
`"objectstore-primary"` — so a small feed can reference a stream sequence or a KV revision and a
large feed can reference an ObjectStore key. ObjectStore is the recommended path for big/binary; it
is **not a required dependency**.

`StorageReference` already carries `ContentType` (MIME) and `Size`. The provenance **convention** is
deliberately *extensible, not a fixed schema*: feeds that need it add — as optional, generic fields —
a content hash, a byte or time range, a packet/frame reference, and codec/container hints. Specify
the minimal core (the reference + content type) now and let the binary/KLV feeds prove which extras
matter before any of them are baked in. Product-specific provenance spelling stays product-local
until a canonical source-reference vocabulary ships; then swap it in. **Do not** upstream
product-specific predicate names.

## 4. Who owns current state — the spine that keeps a COP from becoming a war

This is the section that turns "project current state" from a footgun into a pattern.

**Project per source.** ADS-B writes `…adsb.aircraft.X`; TAK/CoT writes `…cot.track.X`. Each feed
**solely owns its own current-state entity**. No two feeds contend for the same predicate, so there
is no lease war and no silent clobber. A cross-source **fusion** view ("the one true aircraft X") is
a *separate, explicitly-owned projection* with its own producer identity, or a query-time join —
**never** several feeds writing one shared entity. Governance arbitrates `(entity, predicate)`
cells; it is deliberately *not* cross-source identity resolution
([28-governed-semantic-state.md](28-governed-semantic-state.md): "it is not semantic dedupe"). Pick
per-source entities and that whole class of conflict disappears.

With per-source identity in place, each governance primitive earns its keep against §1's failure:

| Primitive | The harm it removes |
|-----------|---------------------|
| **Must-exist** (ADR-055) — a write to a non-existent entity is rejected (`entity_not_found`), no auto-vivify | a typo'd or malformed ID can't silently birth a ghost aircraft |
| **Replace-by-predicate** (`ReplaceOwned`, ADR-056 Decision 3) — current-state predicates are *replaced* by the owner, not appended | altitude is one current value, not an ever-growing pile of stale readings |
| **Owner lease** (`enforce_owner_lease`, ADR-056) — a write whose `OwnerToken` doesn't match the live owner is rejected (`owner_lease_stale`) | the stale replay / reconnect write is rejected instead of overwriting live state |
| **CAS transition** (`ExpectedRevision`, ADR-049) — for state that moves through a machine | a lost update can't skip a transition |

A projector is a component; it stamps an `ownership.OwnerToken` (minted by the ownership Registry,
written verbatim as `ownerToken.Wire()` — the `"<owner>#<incarnation>"` form) onto its
graph-ingest mutation. The token's *incarnation* is what makes a reconnecting projector distinct
from its own stale predecessor — see replay attribution (§7).

Start in observe-only, watch `owner_lease_mismatch_total`, fix the writers it reveals, then flip
`enforce_owner_lease`. The metric is the migration aid; the flip is the guarantee.

## 5. Indexing profiles: pay for the index a feed actually needs

Every projected entity declares an `entity.indexing.profile` (ADR-054, `vocabulary/predicates.go`).
The profile decides how much of the indexing/embedding/community machinery the entity pays for — and
high-rate feeds are exactly where paying for the wrong one hurts. Map by feed shape:

| Profile | Use it for | Cost posture |
|---------|-----------|--------------|
| `signal` | current-state telemetry — tracks, positions, detections | telemetry readings; embed only summarized text, not every reading |
| `control` | command / marker / ACK readback, lifecycle/run machinery | low-cardinality control state |
| `content` | reviewed human text — GeoChat, advisories | full retrieval corpus: embed + community |
| `trace` | mechanically generated debug/replay evidence that lands on-graph | no embedding |

The default for a feed's current-state track is `signal`. Reserve `content` for the genuinely
human-readable, retrievable text — projecting a position firehose as `content` would pay embedding
cost on coordinates nobody searches by phrase.

## 6. Component-flow placement and the component contract

The flow places five responsibilities — keep them as distinct components/ports:

```text
input  →  decoder/processor  →  projector  →  graph-ingest (governed state)
                 │                   │
                 └─ raw capture ─────┴─→ JetStream stream (+ ObjectStore ref)  →  replay
```

- **input** — receives the feed (UDP/WebSocket/file/poller).
- **decoder/processor** — parses bytes into a typed payload (registered in the payload registry).
- **projector** — derives the per-source current-state entity and writes it through graph-ingest
  under an owner token (this is the governed-write step from §4).
- **raw capture** — writes the frame/blob to the bounded stream / ObjectStore.
- **replay** — re-reads the stream/history (§7).

Document these framework surfaces *together* for each feed component (Q5):

| Surface | Where | What to declare |
|---------|-------|-----------------|
| **Ports** | component config | input/output subjects, stream bindings, KV bindings |
| **Payload registry** | `payload_registry.go` `init()` | the decoded type's domain/category/version/factory (see [15-payload-registry.md](15-payload-registry.md) and `/new-payload`) |
| **Buffer / backpressure** | `pkg/buffer` | capacity + overflow policy (`DropOldest`/`DropNewest`/`Block`) for the raw lane |
| **Flow metrics** | `component.ProcessorMetrics` | `events_processed_total`, `events_errors_total`, `kv_operations_total`, `processing_duration_seconds` — the Prometheus convention every component shares |

## 7. Replay and fixtures

**Replay is a composition, not a new primitive.** It is the bounded JetStream stream replayed to a
consumer, plus the KV Twofer's history (replay an entity from any revision —
[02-kv-twofer.md](02-kv-twofer.md)), plus ObjectStore for any referenced bytes. Nothing new to
build.

**Replay attribution is an explicit rule, not a footnote:** a replay writer MUST NOT impersonate
live ownership or re-stamp a stale observation as current state. Concretely — replay under a
*distinct* owner incarnation (or a replay-scoped identity), so the live projector's lease is never
silently overwritten by a re-run of yesterday's frames, and a replayed observation is recorded as
historical, not promoted to current. This is the same lease/incarnation mechanism from §4, applied
to time travel.

**Fixtures and portable demo data** sit in `testdata/` next to the package that consumes them;
end-to-end demo data lives under `test/e2e`. Keep replay fixtures next to the raw-capture format
they exercise so a format change breaks its fixture loudly. Retention policy, privacy, and fixture
licensing are product concerns — out of scope here.

## 8. Framework utilities vs. product-local code

Reach for the framework primitive for the generic concern; keep the feed's identity local:

| Use the framework | For |
|-------------------|-----|
| `pkg/buffer` | bounded raw lane + overflow policy |
| `pkg/cache` (LRU / TTL / hybrid) | dedup / last-seen / short-lived lookups |
| `pkg/worker`, `pkg/dispatch` (ADR-048) | bounded-concurrency fan-out inside the component |
| `natsclient` | all transport (request/reply, streams, KV) |

Keep **local**: the wire decode (MAVLink/CoT/KLV parsing), the domain vocabulary, the per-feed
identity scheme, and retention/privacy policy. The framework gives you the lane and the governance;
the feed gives you its meaning.

## 9. A worked example (feed-agnostic)

A generic `sensor` feed — no product vocabulary — projecting one source's current state:

```text
1. input:    sensor.raw.>            (UDP)            → bounded buffer (DropOldest, cap 4096)
2. capture:  → JetStream "SENSOR_RAW" (+ ObjectStore ref if the frame is large)
3. decode:   raw bytes → SensorReading payload (registered type)
4. project:  per-source entity  acme.platform.sensor.<src>.track.<id>
             - facts:      track.position, track.heading      (current state)
             - profile:    entity.indexing.profile = "signal"
             - provenance: StorageReference → the SENSOR_RAW record
             - owner:      ownerToken.Wire()  (so a stale re-send is rejected, not merged)
             written via graph-ingest update_with_triples (replace-by-predicate)
```

The same write, run twice with a stale token: observe-only commits it and bumps
`owner_lease_mismatch_total`; `enforce_owner_lease` rejects the second with `owner_lease_stale`.
Fusion across sources (`…sensor.fused.track.<id>`) is a separate component with its own owner — never
a second writer on the per-source entities above.

## 10. What this is not

- Not a new framework layer, raw-storage service, or mandatory object store.
- Not a place to upstream product-specific COP/CoT predicates.
- Not a requirement that raw packets become graph entities.
- Not a solution to retention, privacy, or fixture-licensing policy.

It is a recipe for composing what the framework already owns — with the ownership boundary made
explicit so a multi-feed picture stays a picture, not a write war.

## Related documents

- [Governed Semantic State](28-governed-semantic-state.md) — the ownership contract this builds on
- [KV Twofer](02-kv-twofer.md) — current state as queryable fact + history/replay
- [Streams vs KV Watches](03-streams-vs-kv-watches.md) — facts vs requests; what belongs on a stream
- [Payload Registry](15-payload-registry.md) — registering decoded feed types
- [Typed Artifact Entities](26-typed-artifact-entities.md) — content-by-reference via `ContentStorable`
- [ADR-054 — Indexing Profiles](../adr/054-semantic-indexing-eligibility.md)
- [ADR-055 — Graph Write-Intent Taxonomy](../adr/055-graph-write-intent-taxonomy.md)
- [ADR-056 — Authoritative Semantic State](../adr/056-authoritative-semantic-state.md)
