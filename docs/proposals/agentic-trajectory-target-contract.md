# Agentic trajectory append-only audit contract advisory

This target contract supersedes the aggregate Options A/B/C draft. It is grounded in the accepted surface inventory
at commit `8c6997a6`, whose accepted inventory SHA-256 is
`5a7dcf3591cc643ee93654515763ec69982f36c78c296cf02bb8234b3000dd2a`.

The owner has bound two governing rulings: full-fidelity evidence uses an ObjectStore-capable registered
`storage.Store`, and audit failures degrade loudly but never fail loop work.

## Surface inventory

- Current trajectory authority is process-local: terminal reads use TTL cache then `TrajectoryManager`, and the NATS
  handler marshals that aggregate (`processor/agentic-loop/component.go:1763-1825`).
- Current startup constructs a private ObjectStore directly from `content_bucket`
  (`processor/agentic-loop/config.go:47-58,378-390`; `processor/agentic-loop/component.go:625-656`). That handle is
  outside the registered provider lifecycle.
- Backend-neutral storage already exists: `storage.Store` supplies `Put`/`Get`; `message.StorageReference` carries
  `StorageInstance`, key, content type, and size (`storage/storage.go:51-87`; `message/storable.go:15-36`).
- The ObjectStore component is already the lifecycle owner and `StoreProvider`: its live handle is exposed under
  `store.InstanceName()` (`storage/objectstore/component.go:96-112`), its `store-provide` output is created from the
  logical instance (`storage/objectstore/component.go:222-230`), and Start threads that instance into the store
  (`storage/objectstore/component.go:251-280`).
- The current ObjectStore factory's logical instance is exactly `objectstore`, independent of physical bucket
  (`storage/objectstore/component.go:121-160`; `storage/objectstore/store.go:73-108`).
- `ComponentManager` injects the shared `StoreRegistry` into every component
  (`service/component_manager.go:2077-2110`). The registry provides lazy `Store(instance)` lookup and prohibits handle
  caching or closing by borrowers (`storage/storeregistry/storeregistry.go:83-102`).
- `StoreRegistry.Register` already rejects duplicate ownership
  (`storage/storeregistry/storeregistry.go:60-71`), but `ComponentManager.registerProvidedStores` currently logs and
  skips that error, leaving the rival component reported started (`service/component_manager.go:2113-2164`).
- Components start concurrently behind a barrier, and store registration occurs after each provider's Start
  (`service/component_manager.go:364-397,472-531`). There is no provider-before-consumer order to rely on.
- KV already supplies the required immutable-log operations: `Create`, filtered prefix listing, and replaying watch
  (`natsclient/kv.go:201-219,510-600`).
- Graph-gateway already requires exactly three NATS request outputs; `agentic_queries` is one of them
  (`gateway/graph-gateway/component.go:83-87,134-162`). Its configured family already derives the exact `trajectory`
  subject through `querySubject` (`gateway/graph-gateway/component.go:343-373,890-900`).
- Agentic-loop currently subscribes to the undeclared literal `agentic.query.trajectory`
  (`processor/agentic-loop/component.go:350-392`).
- Seven assembled configs carry redundant `trajectories` full-replacement overrides and none contains an ObjectStore
  provider: `configs/agentic.json`,
  `configs/flows/{ops-agent,ops-agent-test,lesson-example,crud-tools-test,deep-research-test,deep-research}.json`.
- Current agentic-loop health and metrics are established observability surfaces
  (`processor/agentic-loop/component.go:311-332`; `processor/agentic-loop/metrics.go:11-62,260-313`).

## Adopter seam inventory

| Adopter | Must know | If they do nothing | Discovery | Desired burden |
|---|---|---|---|---|
| Flow author using a shipped assembly | Nothing about bucket names, ObjectStore clients, fact keys, or query subjects | The assembly supplies the `objectstore` evidence provider and inherits canonical trajectory ports | Component schema and shipped config | Nothing |
| Flow author replacing storage | The logical registered `StorageInstance`, not its backend or bucket | Audit evidence is reported degraded while agent work continues | Config validation and component health | One logical provider identity |
| External component querying trajectories | `agentic.query` interface `v1` and the configured request family | The canonical route works; isolated deployments need complete paired overrides | Port/interface discovery | One typed request contract |
| Public client | Versioned GraphQL trajectory schema | Direct agentic-loop HTTP endpoints disappear at the breaking cutover | GraphQL schema and migration note | No KV, NATS, Store, or graph knowledge |
| Operator | Audit degradation appears in existing logs, metrics, and component health | Agent execution continues; missing evidence/facts remain explicit operational loss | Existing health/metrics endpoints | No new status service |

## Bound target

Trajectory is a simple append-only audit log of immutable loop facts. It is not a mutable aggregate, summary, cache,
desired-state ledger, CQRS service, or graph projection contract.

The KV write is the fact and the watch event. Large canonical evidence lives behind a registered provider-owned
`storage.Store`. Audit failure degrades observability loudly but never rejects, NAKs, cancels, or fails the agentic
work that produced the fact.

Trajectory is a pragmatic, best-effort audit log. Each successful immutable KV Create proves only that one observation
was recorded. It does not prove that every processing attempt, earlier fact, or evidence body was recorded.

## Physical KV contract

Use one bucket, `AGENT_TRAJECTORIES`, with one immutable key per fact. Bucket history is `1` because keys are never
updated.

Key:

```text
v1.<base32-sha256(loop_id)>.<attempt_id>
```

The loop hash is fixed and bounded. `attempt_id` is one bounded, NATS-safe, framework-generated token, such as a UUID
encoded without punctuation. Raw external IDs never enter the key. Readers compute `v1.<loop_hash>.>` from the
requested loop ID, use native filtered listing or watch, and verify the envelope's loop digest. They never enumerate
the whole bucket.

Each fact-recording invocation is a distinct audit observation. At the beginning of the invocation, agentic-loop
allocates a new framework-owned `attempt_id` and `attempt_ordinal` for that loop. A same-process or cross-process
redelivery is a new invocation and appends a new immutable fact even when request or tool-call correlation is the
same. Repeated delivery is visible audit evidence, not deduplication or an integrity conflict.

The active per-loop manager allocates `attempt_ordinal` monotonically under its existing synchronization, so
concurrent handlers receive distinct ordinals. On restart, it initializes the next ordinal from the maximum visible
ordinal under the loop prefix. The attempt ID is the uniqueness key; the ordinal supplies observed display order.

Writers use KV `Create`, never `Put` or `Update`:

1. Allocate the invocation's attempt ID and ordinal once.
2. Deterministically encode `TrajectoryFactV1` once for that invocation.
3. `Create(key, bytes)`.
4. If Create reports key-exists or its reply is ambiguous, `Get(key)`.
5. Byte-identical content is within-invocation replay success.
6. Different content under that same attempt ID is an integrity audit failure. Retain the original immutable fact,
   report degradation, and continue agent work.

Retries of Store Get/Put, KV Create, and lost-reply verification within one invocation reuse the exact attempt ID,
fact key, and canonical bytes. Only redelivery starts a new processing attempt and receives a new attempt ID.

There is no loop summary key, terminal summary, per-step mutable status, CAS aggregation, membership protocol, or
cache fallback.

## `TrajectoryFactV1`

The envelope is intentionally finite:

```text
schema_version        "v1"
loop_digest           sha256
attempt_id             required framework-generated ID
attempt_ordinal        required uint64
kind                  fixed enum
source_kind            optional fixed enum
source_correlation     optional bounded digest/reference
causal_iteration      uint32
causal_phase          fixed enum/rank
causal_ordinal        uint32
observed_at           RFC3339Nano
elapsed_ms            optional int64
status                optional fixed enum
tokens_in/out          optional uint64
message/tool/url counts optional uint32
model/provider/tool/capability display previews optional bounded strings
error_category         optional fixed enum
evidence_digest        sha256 when a body exists
evidence_size          uint64 when a body exists
evidence               optional message.StorageReference
evidence_capture       none | stored | missing
evidence_failure       optional fixed enum
```

`kind` is a closed v1 set:

- `loop.started`
- `model.requested`
- `model.completed`
- `tool.requested`
- `tool.completed`
- `context.compacted`
- `loop.terminal`

Request and result are separate immutable observations because an append-only record cannot be filled in later.
Source correlation is advisory linkage, not fact identity. Use a request ID, call ID, signal ID, or message ID when the
decoded domain payload supplies one. JetStream stream, consumer, or delivery sequence may be recorded when already
available, but this contract does not require that metadata or add acquisition merely to create trajectory facts.
Missing source correlation never rejects fact creation.

Causal display order is
`(iteration, phase_rank, source_ordinal, attempt_ordinal, attempt_id)`, never arrival timestamp. Tool request/result
source ordinals come from the model response's tool-call order, so concurrent results remain logically ordered.
`observed_at` and `elapsed_ms` describe the processing observation and may differ across redeliveries.

The envelope never embeds prompts, responses, messages, tool-call arrays, arguments, results, URLs, arbitrary metadata
maps, or raw error strings. Those belong only in evidence. It records fixed counters/enums and bounded previews; full
strings are in evidence. Use the repository's existing 8 KiB bounded-text convention as an internal maximum marshaled
fact size and enforce it with a constant plus adversarial shape test. This is a framework invariant, not an adopter
knob: the encoder hashes/truncates previews before marshal, and an oversize fact is an audit implementation failure
reported through the degradation path.

## Full-fidelity evidence

Full fidelity is bound. Delete `trajectory_detail`; audit capture is always full.

Capture the semantic event before operational truncation or compaction:

- model request: all messages, tools, arguments, reasoning carriers, and request parameters;
- model completion: the full decoded completion and usage;
- tool request: full arguments and dispatch metadata;
- tool completion: the original result before `tool_result_max_bytes` truncation;
- compaction: full before/after compaction evidence;
- terminal: the full terminal result/error evidence when one exists.

`tool_result_max_bytes` may remain as an execution/context protection, but it must run after audit evidence capture and
cannot redefine audit fidelity.

Encode a deterministic `TrajectoryEvidenceV1`, SHA-256 the exact canonical bytes, and use the digest-addressed logical
key:

```text
trajectory-evidence/v1/sha256/<hex-digest>
```

For every body:

1. Resolve `deps.StoreRegistry.Store(trajectory_evidence_storage_instance)` lazily for this operation; its shipped
   default is `objectstore`.
2. Never cache or close the borrowed handle.
3. `Get(key)` first. Matching bytes mean replay success; mismatching bytes are an integrity failure.
4. On not-found, `Put(key, bytes)`.
5. On an ambiguous Put reply, `Get` and verify.
6. A stored fact carries
   `StorageReference{StorageInstance:configuredInstance, Key:key,
   ContentType:"application/vnd.semstreams.agentic-trajectory-evidence.v1+json", Size:n}` plus the digest.

Backend-specific versions may exist under one logical key, but replay never creates timestamp-derived logical keys. A
body stored before a fact-write failure is an unreferenced content-addressed object eligible for the eventual
reference-aware retention policy; do not invent a transaction or general CAS service.

If evidence cannot be resolved, stored, or verified, still attempt an immutable fact with
`evidence_capture="missing"`, the computed digest/size when available, and a bounded failure enum, but no fabricated
`StorageReference`. A successful Create makes that missing-evidence observation visible. It does not prove that all
other failures or facts were durably recorded.

## Exact provider representation and assembly

The canonical logical provider is `objectstore`; the canonical physical bucket in the seven agentic assemblies is
`AGENT_CONTENT`.

Add this component to each of the seven configs:

```json
"objectstore": {
  "type": "storage",
  "name": "objectstore",
  "enabled": true,
  "config": {
    "bucket_name": "AGENT_CONTENT"
  }
}
```

This yields `StorageInstance="objectstore"` because the current factory binds that exact instance and automatically
declares `StoreProvidePort{Instance:"objectstore"}`. Agentic-loop replaces `content_bucket` with a logical
`trajectory_evidence_storage_instance` defaulting to `objectstore`; physical bucket configuration exists only on the
storage owner.

Agentic-loop is a borrower/writer, not a `StoreProvider`. Delete its embedded ObjectStore constructor, handle field,
close path, and backend import. Do not misuse `StoreReadPort` as write authorization and do not add a new store-write
port in Foundation B. Store ownership is represented by the provider's existing `StoreProvidePort`; runtime access is
the existing injected `StoreRegistry` substrate.

`ComponentManager` adds one narrow lifecycle phase, not a general dependency scheduler. It partitions the cold-boot
component set using the existing `component.StoreProvider` interface:

1. Start all `StoreProvider` components concurrently in a provider barrier.
2. Register every provided store immediately after its provider Start.
3. Treat invalid or duplicate `StorageInstance` registration as that provider's startup error and fail the cold-boot
   barrier. Never log and skip a rival owner.
4. After the provider barrier completes, start all remaining components concurrently in the existing consumer
   barrier.

This guarantees that agentic-loop observes registered providers before it installs subscriptions, without sleeps,
polling, port-derived dependency graphs, readiness deadlines, or a general topological scheduler. Independent storage
providers may still start concurrently, and non-provider consumers remain concurrent in their phase.

The first provider remains registered if a rival claims the same instance; the rival becomes failed and boot fails
loudly. Dynamic provider starts propagate the same registration error. Provider stop or reconfiguration continues to
deregister before Close through the existing lifecycle hooks. Empty or nil claimed stores are errors; a non-provider
or a provider returning no stores remains a legitimate no-op.

At agentic-loop Start, resolve its configured logical evidence provider once for dependency validation after the
provider phase. A truly absent provider does not fail Start: log `ERROR`, increment the bounded provider-resolve audit
metric, latch existing component Health degraded, install subscriptions, and continue work. Every evidence operation
still resolves lazily through `StoreRegistry`, so later provider addition or reconfiguration is observed. Agentic-loop
never caches or closes a borrowed handle.

The healthy evidence invariant is therefore observable, not predicted:

- zero matching live handles: agentic-loop health is degraded;
- exactly one: the provider dependency is satisfied; component Health is healthy only if no audit-loss latch exists;
- a second claimant: its registration/start fails loudly, so ambiguity never becomes a running state.

## Failure and observability contract

Audit recording is attempted before the work's next publish/ack, but audit failure never changes the work outcome.

For each failure:

- emit `ERROR` with loop ID, attempt ID, bounded kind/stage/reason, and error; never log evidence bodies;
- increment `semstreams_agentic_loop_trajectory_audit_failures_total{stage,kind,reason}` with all three labels drawn
  from closed enums;
- latch the component's existing `HealthStatus` degraded for the process lifetime, populating `ErrorCount` and
  `LastError` with the latest bounded diagnostic;
- continue the existing publish/ack/state transition.

Stages are fixed: `provider_resolve`, `evidence_get`, `evidence_put`, `evidence_verify`, `fact_encode`, `fact_create`,
`fact_verify`. Reasons are fixed error classes, not raw backend strings.

Health also checks current presence of the configured provider on each call. Missing provider reports degraded
immediately; restored provider clears that current dependency condition, but a prior sticky audit-loss latch remains
degraded because a historical gap may already exist. This uses only existing logging, Prometheus, and
`component.HealthStatus`; no KV status bucket, repair queue, readiness service, or general status subsystem is added.

If evidence fails but KV succeeds, the missing-evidence fact is query-visible. If KV fact creation also fails, only
logs, metrics, and Health can report the loss. No durable fact may exist, and the system does not manufacture a
counter, seal, reconstructed gap, or later completeness claim. Agent work continues in every failure case.

## Ordering and restart

Ordinary processing-attempt ordering:

1. Attempt canonical evidence storage before operational truncation.
2. Attempt immutable observation fact Create, using `stored` or honest `missing` evidence state.
3. Regardless of audit outcome, continue the existing state transition, downstream publication, and source ACK.

Terminal processing-attempt ordering:

1. Complete all other audit attempts known to this terminal-processing invocation.
2. Attempt terminal evidence storage if the outcome has a body.
3. Attempt the ordinary immutable `loop.terminal` fact last in this invocation.
4. Regardless of audit outcome, perform the existing `COMPLETE_<loopID>` write.
5. Publish the existing `agent.complete.*` or `agent.failed.*` event.
6. ACK the source delivery.

This defines relative ordering without redesigning adjacent surfaces. `COMPLETE_` polymorphism/collisions and
terminal-event collision behavior remain separate, explicitly out of scope.

Crash/replay behavior:

- retry or lost reply inside one recorder invocation: reuse the same attempt ID, key, and canonical bytes; Get verifies
  idempotent success;
- same attempt ID with different bytes: retain the immutable original, report integrity degradation, and continue;
- redelivery, including after process restart: allocate a new attempt ID and append a new visible fact, correlated to
  the same source when possible;
- multiple attempts may reference the same digest-addressed evidence body without duplicating the logical object;
- crash after a fact but before work ACK: redelivery appends a second attempt fact, which is correct audit history;
- crash before a terminal fact: no terminal observation may exist; a crash after it but before ACK may produce a
  second terminal observation on redelivery;
- restart requires no cache hydration: query readers prefix-list current facts and watches replay current keys.

Neither terminal crash case creates a seal or completeness protocol. `COMPLETE_` and terminal-event correctness or
collisions remain separate and out of scope.

## Read and public API contract

The canonical reader:

1. hashes requested loop ID;
2. lists `v1.<loop_hash>.>`;
3. gets and validates every returned fact;
4. sorts by causal order;
5. derives observed token totals, step durations, counts, and outcomes only from returned facts;
6. hydrates evidence references only when requested;
7. reports visible capture gaps and missing/unverifiable bodies honestly.

Every public and internal trajectory response reports:

```text
coverage: observed
observed_totals: <totals derived only from returned visible facts>
```

Never return or imply `complete`, `fully captured`, `gap free`, or an equivalent guarantee. After restart, readers
expose visible facts without making a durable statement about invisible pre-crash or runtime loss. Process logs,
metrics, and Health remain the operational evidence of audit failures.

If no `loop.terminal` fact is visible, report `terminal_observed: false`. If one or more are visible, report
`terminal_observed: true` and expose every observed terminal fact in causal/attempt order. A terminal fact records only
that one outcome observation; it is not a seal or completeness proof. A terminal redelivery may append another
terminal fact. Do not infer terminal state from `COMPLETE_<loopID>`, terminal events, cache, process memory, or graph
state.

GraphQL through graph-gateway is the sole public application surface. Delete direct agentic-loop trajectory HTTP
handlers and their OpenAPI paths. NATS remains typed internal request/reply. Cache is deleted, not retained as
authority or acceleration in Foundation B. `TrajectoryManager` may remain only for active execution mechanics and
never serves reads.

Graph trajectory entities/projection are outside Foundation B correctness. Delete the terminal batch trajectory
graph-write path from this migration. Any later graph trace is a separate post-foundation index/projection design
consuming the durable fact log; no projector state, graph-pending flag, or repair service belongs here.

## Exact port contracts and routing

Add the canonical required output to agentic-loop:

```text
name: trajectories
kind: KVWritePort
bucket: AGENT_TRAJECTORIES
required: true
interface: {type: agentic.trajectory.fact, version: v1}
```

Add the canonical required request input to agentic-loop:

```text
name: trajectory_query
kind: NATSRequestPort
subject: agentic.query.trajectory
required: true
interface: {type: agentic.query, version: v1}
```

Keep graph-gateway's existing exactly-three-output contract. Do not add a fourth output. Change only its existing
`agentic_queries` definition:

```text
name: agentic_queries
kind: NATSRequestPort
subject: agentic.query.*
required: true
interface: {type: agentic.query, version: v1}
```

`querySubject(family, "trajectory")` already resolves that family to the loop's exact input. Delete the literal
subscription implementation and subscribe through the declared `trajectory_query` input. Do not infer subjects from
platform identity.

Strengthen graph-gateway's existing three-port validator so `agentic_queries` must carry interface `agentic.query`
`v1` in addition to being required `NATSRequestPort` with one trailing-wildcard family. Agentic-loop validates the
exact input uses the same interface/version.

Isolation remains runtime configuration. A deployment requiring isolation supplies complete matching overrides on
both paired components, for example:

```text
graph-gateway agentic_queries: tenant.agentic.query.*
agentic-loop trajectory_query: tenant.agentic.query.trajectory
```

Both overrides repeat the port kind, `Required:true`, and interface type/version. Mismatched pairs fail port/config
validation. There is no platform-derived owner, alias, dual subscription, or compatibility shim.

Delete the seven redundant `trajectories` overrides. They currently act as complete replacements and would erase
`Required` and `Interface`; inheritance from the canonical default is the correct clean cutover. Any genuine custom
binding must repeat the full required versioned definition. No aliases or shims.

## Deletion scope

Delete:

- aggregate `Trajectory` as the durable/public representation;
- terminal trajectory cache and `trajectory_cache_ttl`;
- cache/manager query fallback;
- no-op `SaveTrajectory`;
- `trajectory_detail` and summary capture;
- `content_bucket` and private ObjectStore construction/lifecycle;
- timestamp-derived evidence keys;
- terminal batch trajectory graph emission;
- direct trajectory HTTP/OpenAPI;
- literal query subscription bypassing the declared `trajectory_query` input;
- seven redundant trajectory port overrides;
- stale documentation promising cache expiry falls back to graph reconstruction.

Do not redesign hierarchy, research, index, `COMPLETE_`, or terminal-event contracts.

## Required tests

- Deterministic NATS-safe keys contain the framework attempt ID and no raw external identity.
- Two attempts with the same request/call correlation create two facts with different attempt IDs; their evidence
  digest/reference may be identical.
- Retry and lost replies inside either invocation reuse its exact key and canonical bytes; same-key different bytes
  produce integrity degradation.
- Optional source-correlation absence does not reject fact creation, and no test requires JetStream delivery metadata.
- Parallel tool results sorted by source ordinal rather than arrival.
- Adversarial full metadata encodes below the internal 8 KiB fact bound; no collection/body can enter the fact.
- Full tool result captured before operational truncation; full messages/tool calls/arguments remain retrievable from
  evidence.
- Digest-addressed Store Get/Put/lost-reply verification through `StoreRegistry`, including provider restart and lazy
  re-resolution.
- No agentic-loop ObjectStore construction, cached handle, or Close.
- All seven assembled configs contain the `objectstore`/`AGENT_CONTENT` provider and omit redundant `trajectories`
  overrides.
- StoreProvider Start and registration complete before agentic-loop Start and subscription installation.
- Two independent providers start concurrently in phase one; non-provider consumers start concurrently in phase two.
- Duplicate provider claim fails the provider phase/cold boot or dynamic start without clobbering the incumbent.
- Missing configured provider starts agentic-loop degraded while work still publishes and ACKs.
- No sleep, arbitrary readiness deadline, port-derived dependency graph, or general dependency scheduler is added.
- Missing provider, Store Get/Put failure, fact encode/Create failure, and integrity conflict each log, increment
  bounded metrics, and degrade Health while downstream publish/ACK still occurs.
- Evidence failure plus successful KV produces an honest missing-evidence observation, not a gap-counter input.
- Fact Create failure leaves no durable fact and only ERROR/metric/degraded Health; restart reconstructs no gap claim.
- Every query response labels coverage `observed` and totals `observed_totals`.
- Visible terminal facts set `terminal_observed=true` without changing coverage; none sets it false, with no inference
  from adjacent surfaces.
- Two terminal redelivery attempts create two ordered terminal observations.
- Terminal audit is last in its invocation and precedes `COMPLETE_`, terminal publication, and ACK; audit failure does
  not block them.
- Crash before the terminal fact leaves no observed terminal; crash after it and before ACK may yield another terminal
  observation on redelivery.
- No schema, type, config, or test introduces a terminal seal, audit counters, `counts_known`, manifest, membership
  proof, watermark, checkpoint, or completeness classification.
- Restart query works with empty process memory; watch initial replay returns current facts.
- Prefix reader sorting, observed derived totals, terminal-observation behavior, and missing-body reporting.
- Graph-gateway retains exactly three outputs; canonical `agentic.query.*` resolves `.trajectory`; interface or paired
  override mismatch fails validation.
- GraphQL production-path body hydration; direct HTTP/OpenAPI trajectory surface absent.
- Crash E2E at body-before-fact and fact-before-publish boundaries preserves distinct-attempt observations.

## Remaining owner ruling

No further authority, fidelity, failure-policy, graph, cache, API, routing, or shape ruling remains: the owner has bound
them.

Retention is the only unavoidable product policy before automatic reclamation is enabled:

- KV fact query horizon;
- evidence-body horizon and reference-aware GC;
- treatment of facts whose evidence was deliberately reclaimed;
- legal/privacy deletion requirements.

Retention does not block Foundation B. Until the owner binds it, create `AGENT_TRAJECTORIES` with history 1 and no
TTL, and apply no automatic evidence expiry. Do not guess a horizon or expose a caller-computed TTL.
