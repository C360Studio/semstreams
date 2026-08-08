<!-- markdownlint-disable MD041 -->

## Why

Foundation B has completed its grammar, owned migration, shared-consumer, and renderer/runtime checkpoints, but its
approved breaking behavior is not yet durable OpenSpec truth. This change records that exact target before release
validation, so adopters see one strict port language instead of stale flat fields, aliases, and divergent projections.

The accepted trajectory inventory at `8c6997a6` exposed a second release blocker: trajectory authority remained a
process-local aggregate with a private ObjectStore and an undeclared query subscription. The accepted contract at
`139b8b1c` resolves it as a best-effort append-only KV audit with registered-store evidence and observed-only reads.

The accepted request/reply inventory and design identified in `approval.md` exposed a third closed-boundary defect:
successful responses could exceed the connected server's payload limit and become requester timeouts, while paging,
trajectory hydration, and the optional ObjectStore RPC each made different assumptions about response size. The
approved correction observes the actual transport result, gives each unbounded operation its own continuation, and
removes the redundant ObjectStore request surface.

## What Changes

- **BREAKING** Replace flat port fields and the second runtime envelope with one typed `config.kind` envelope using
  the twelve canonical kinds. Unknown kinds or fields, wrong directions, duplicate names, malformed values, and
  missing required data fail before component initialization; there is no alias or fallback decoder.
- Resolve each declaration once into immutable normalized facts consumed by Registry, flowgraph, ComponentManager,
  schema generation, and ordinary stream provisioning.
- Keep `kv-read`, `kv-watch`, `kv-write`, and `store-read` observably distinct while normalizing the three KV resource
  and connection identities to `kv:<bucket>`.
- Preserve one narrow gated-DAG physical-provisioning specialization for policies the generic stream declaration
  cannot express.
- Preserve graph mutation as one required canonical `nats-request` provider and compatible requester outputs.
- **BREAKING** Make graph-gateway declare no composition inputs and exactly three required query-family
  `nats-request` outputs: `graph_queries`, `graph_index_queries`, and `agentic_queries`. Existing configurations with
  an input, the legacy `queries` name, or a missing/extra/optional/wrong-family output fail startup until migrated.
- **BREAKING** Replace durable/public aggregate trajectories with immutable `TrajectoryFactV1` observations created
  in `AGENT_TRAJECTORIES`. Each processing invocation owns a new attempt identity; within-invocation retries reuse
  exact bytes, while redelivery appends another visible observation.
- Capture full canonical trajectory evidence before operational truncation in the configured registered
  `storage.Store`, defaulting to `objectstore` backed by `AGENT_CONTENT` in the seven shipped agentic assemblies.
- Add a narrow provider-first lifecycle phase, propagate duplicate Store registration as startup failure, and let a
  missing configured evidence provider start agentic-loop degraded while work continues.
- Route graph-gateway's existing `agentic.query.*` output to agentic-loop's declared
  `agentic.query.trajectory` input. GraphQL is the sole public trajectory API; all responses report observed coverage
  and totals without completeness claims.
- **BREAKING** Make trajectory reads strict cursor-paged metadata/reference queries. Delete evidence hydration from
  that request/response; authorized evidence retrieval uses the registered Store named by the reference.
- **BREAKING** Replace GraphQL prefix `[Entity]` with `EntityPage { entities, next_cursor }` and preserve continuation
  through every adapter. No list alias remains.
- Attempt NATS request/reply success responses before classification. Translate only an observed
  `nats.ErrMaxPayload` to `invalid/response_too_large`; use the connected server's current maximum only to fit exact
  operation-owned pages and provide diagnostics.
- **BREAKING** Delete the ObjectStore request/reply API and dormant NATS content fetcher. Registered
  `Store`/`StreamableStore` is the sole internal ObjectStore access path; old `api`/`nats-request` inputs fail boot.

## Capabilities

### New Capabilities

- `component-discovery`: Defines canonical normalized port facts, consistent management/flow projections, exact-read
  semantics, and graph-gateway's strict three-family discovery contract.
- `request-reply-response-bounds`: Defines observed oversize refusal, operation-owned continuation, and removal of
  the redundant ObjectStore request/reply surface.

### Modified Capabilities

- `component-runtime-config`: Replaces flat/builder precedence with the strict common declaration/runtime envelope
  and complete-replacement merge behavior.
- `stream-provisioning`: Derives ordinary stream declarations from normalized JetStream facts and bounds the
  gated-DAG specialization; explicitly keeps trajectory KV and evidence ObjectStore backing streams outside ordinary
  stream provisioning.
- `framework-composition`: Makes canonical `nats-request` facts the only static graph-mutation topology contract and
  adds the narrow StoreProvider-first startup barrier.
- `graph-ingest`: Requires the sole mutation provider to use the strict canonical request-port declaration.
- `agentic-loop`: Defines immutable attempt observations, digest-addressed full evidence, non-blocking degradation,
  causal reads, ordinary terminal observations, and observed-only coverage.
- `gateway-response-projection`: Adds GraphQL-only reference-preserving trajectory pages and end-to-end graph-prefix
  continuation through existing typed internal query families.

## Impact

The implementation spans the exported component port API, shipped JSON configurations, component declarations,
runtime renderers, registry/flow/management views, schema generation, stream provisioning, StoreProvider lifecycle,
agentic-loop trajectory recording, registered evidence storage, and GraphQL/NATS query projection. SemStreams is the
direct consumer. The later migration holdout set is `semdev`, `semmachina`, `semsource`, `semboids`, `semdragon`,
`semstreams-ui`, `semteams`, `semconnect`, `semlink`, and `semops`; those downstream repositories do not shape or
block this framework contract. SemSource's former ObjectStore RPC expectation is intentionally broken and must
migrate to registered Store access at the release boundary.

## Non-goals

- No graph-query semantic change or graph-query spec delta.
- No Foundation C `Discoverable`/snapshot cutover.
- No payload-registry type, orchestration mechanism, custom port kind, compatibility shim, or migration window.
- No trajectory cache authority, aggregate summary, terminal seal, completeness proof, graph projection, repair
  worker, or direct trajectory HTTP/OpenAPI surface.
- No generic response stream, overflow KV bucket, response envelope, public evidence-body endpoint, payload-size
  configuration knob, deprecated ObjectStore RPC, or compatibility shim.
- No automatic trajectory/evidence expiry until retention is separately bound; Foundation B uses history 1, no TTL,
  and no automatic evidence expiry.
- No hierarchy, research, `COMPLETE_`, or terminal-event redesign.
- No claim that checkpoints 1-4 satisfy the checkpoint-5 release, E2E, reviewer, or post-B inventory gates.
