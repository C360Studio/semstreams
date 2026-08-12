# Migrate from beta.159 to beta.160

This is the canonical downstream migration guide for `v1.0.0-beta.160`. It consolidates the breaking changes between
`v1.0.0-beta.159` and `v1.0.0-beta.160`; the linked guides remain the detailed contract references.

The release is intentionally a clean break. SemStreams provides no deprecated APIs, aliases, compatibility shims,
dual formats, or state migration. Downstream repositories can migrate independently and report a genuinely missing
framework capability without reopening ordinary breaking API changes.

## Release target

- Go module tag: `v1.0.0-beta.160`
- Release commit: `8403a2218000e45a31c5132fbfe01af42ed04f14`
- Container image: `ghcr.io/c360studio/semstreams:1.0.0-beta.160`
- Product evidence and accepted limitations:
  [GitHub Release](https://github.com/C360Studio/semstreams/releases/tag/v1.0.0-beta.160)

Start every adopting product on newly provisioned NATS storage. There is no deployed beta.159 state to migrate,
preserve, wipe, or reseed. If retained deployed state is discovered, stop that adoption and request a separate,
owner-reviewed migration or recovery design. Normal operator-managed NATS backups and typed stored-state poison
recovery are separate concerns.

## Recommended migration order

1. Pin the exact tag and start with newly provisioned NATS storage.
2. Compile all binaries to expose deleted Go APIs.
3. Regenerate schemas and API clients, then fix strict configuration and port-validation failures.
4. Migrate graph writes and revision-bearing reads before fixing query callers.
5. Migrate trajectory, tool-discovery, storage-reference, and metrics integrations that the product uses.
6. Validate every shipped flow, then run the product's relevant unit, integration, and end-to-end suites.

Do not copy a downstream census or old bucket-deletion checklist. Compiler, schema, startup, and flow-validation
errors are the current work list for each product.

## Graph state and mutations

The graph has one current-state authority and eventually consistent derived views. Missing relationship targets are
valid observable state; mutating a missing entity returns not-found and never creates a stub.

Required changes:

- Replace ownership registries, tokens, heartbeats, and `pkg/ownership` with local `pkg/projection.Contract` intent.
- Send mutations through the typed `semstreams.graph.mutation/v1` `nats-request` port. The operations are
  `entity.create`, `entity.reconcile`, `triple.append`, and `entity.delete`.
- Replace `ModeReplaceOwned` and `ReplaceOwned` with `projection.ModeReconcile` and `Reconcile`. Use append only for
  genuinely append-only evidence.
- Read the exact entity and KV revision before reconcile or delete. Handle `entity_not_found`, `revision_mismatch`,
  and `commit_unknown` as distinct outcomes; the framework does not retry a mutation for the caller.
- Replace rule action `replace_owned` with `reconcile_predicates` and its local projection contract.
- Replace `OpenCatalogBucket` with `OpenCatalogReader` for readers or `EnsureCatalogBucket` for declared bucket
  owners.
- If consuming graph write todos, use `TodoReader` and `TodoState`; the graph record is now one
  `agent.todo.record` JSON literal.
- Ensure each flow with graph-ingest has exactly one compatible mutation provider and every graph writer declares a
  matching requester output.

`CONTEXT_INDEX`, `STRUCTURAL_INDEX`, the component-status diagnostic bucket and reporter API, and their configuration
knobs are removed without compatibility surfaces. Structural clustering is still computed from the canonical graph
indexes; semantic similarity remains available as `semanticSearch`. `GRAPH_STATUS`, component health, and domain
lifecycle remain distinct supported surfaces.

See [Graph foundation breaking cutover](36-graph-foundation-breaking-cutover.md) for exact mappings and behavior.

## Ports, services, and flow configuration

All component ports now use one strict `PortDefinition` envelope containing `name`, optional `required` and
`description`, and a typed `config` object with `kind`. Old flat fields, side lanes, aliases, custom kinds, and wrong
directions fail validation.

Required changes:

- Construct components through their registered factory and `ComponentManager`; identity-free Registry admission is
  removed.
- Give every JetStream input both `stream_name` and nonempty `subjects`. JetStream outputs require `subjects` and may
  leave naming to the configured stream provisioner.
- Declare graph-query request families as named required `graph.query/v1` outputs. Do not add a graph-gateway input
  port to receive its own queries.
- Treat services as restart-only process composition. Remove calls to `StartService`, `StopService`,
  `RemoveService`, and `RuntimeConfigurable`, and remove `ServiceConfig.Name`.
- Control optional services with outer `services.<name>.enabled`. Remove message-logger inner `enabled` and
  `log_level`, and metrics inner `enabled`. Message logger remains optional and may run at any process log level.
- Keep streams explicitly configuration-owned. A default-only JetStream output without matching configured stream
  intent fails startup.
- Bump the top-level configuration `version` for every changed file. An equal or older file version does not replace
  the configuration already selected from KV.

See [Port and declaration-generation breaking cutover](37-port-and-declaration-generation-cutover.md).

## Graph queries

Required changes:

- Replace GraphQL `similaritySearch` with `semanticSearch` and remove `capabilities` selections.
- Treat `localSearch` `index_not_ready` as retryable eventual availability.
- Consume `entitiesByPrefix` as `EntityPage { entities next_cursor }`, pass its opaque `cursor` argument for the next
  page, and remove code that expects a bare entity array.
- Remove uses of the deleted aggregate `graph/query.Client`, `QueryResponse.RequestID`, `SearchGraph*`,
  `SummarizeGraph*`, and shared `NATSQuerier` wrappers. Use GraphQL for remote access or an existing named,
  operation-specific adapter for embedded framework access.
- Remove deleted shared `search_graph` and `summarize_graph` tool registrations and stale closed-set configuration.
  An application may still own a distinct local executor with either name.
- Remove category aliases `graph_search` and `graph_summary` unless the application explicitly owns their category.

The admitted query operations and GraphQL fields that survived this closure retain their existing wire shapes. See
[Graph query contract closure](migration-post-foundation-b-graph-query-contract-closure.md) for the exact surface
table.

NATS request/reply responders now classify a carrier rejection as `response_too_large`. Treat it as a result-size
failure, not an availability timeout. Result owners that must fit a page may observe the connected server limit with
`natsclient.Client.MaxPayload()`; there is no caller-configured payload budget or generic chunking protocol.

## Agent trajectories and tool discovery

Trajectories are now immutable observed audit facts in `AGENT_TRAJECTORIES`, with history 1 and no bucket TTL. Full
evidence is content-addressed through the registered Store named by `trajectory_evidence_storage_instance` (default
`objectstore`). The GraphQL `trajectory` field is the sole public trajectory query API; it returns bounded facts and
storage references, not evidence bodies.

Required changes:

- Remove trajectory aggregate/cache assumptions and private ObjectStore or HTTP query paths.
- Remove agentic-loop configuration fields `content_bucket`, `trajectory_detail`, and `trajectory_cache_ttl`.
- Send trajectory requests as `{loopId, limit, cursor}`. Remove `hydrateEvidence`; pass `next_cursor` back unchanged
  and retrieve referenced evidence separately through an authorized Store reader.
- Declare the `trajectories` `kv-write` port for `AGENT_TRAJECTORIES` with interface
  `agentic.trajectory.fact/v1`, and the `trajectory_query` `nats-request` port for
  `agentic.query.trajectory` with interface `agentic.query/v1`.
- Configure the named Store that owns full evidence. Failure to record trajectory evidence degrades observability
  loudly; it does not fail the agentic loop.

Tool discovery keeps logical port `tool.list`, but changes from `nats` on `tool.list` to `nats-request` on
`discovery.tool.list`. There is no legacy responder or fallback. Replace broad stream capture `tool.>` with
`tool.execute.>` and `tool.result.>` so the request subject is not captured by a stream. See
[Tool-discovery migration](migration-tool-discovery-default.md).

The complete trajectory contract and GraphQL example live in
[Agentic components](../advanced/08-agentic-components.md#trajectory-analysis).

## Agent-run milestones

The agent-run milestone subscriber now observes terminal loop events and run state without hidden graph or lifecycle
write capability.

Required changes:

- Remove `TriplePublisher` from `MilestoneHandler.OnLoopTerminal` implementations and from
  `NewMilestoneSubscriber` calls.
- Replace `NewMilestoneSubscriberWithManager` with `NewMilestoneSubscriberWithRunStateReader`, and replace test
  doubles for the deleted `MockableManager` with the read-only `RunStateReader` interface.
- Update direct `ResolveRun` callers to pass a `RunStateReader`.
- Move any milestone graph mutation into the consuming component or coordinator through its declared mutation port.
- Explicitly own failed/cancelled root-run lifecycle transitions in the coordinator or component. The subscriber no
  longer changes a dispatched root run to failed or cancelled automatically.

If a downstream does nothing, the deleted signatures fail compilation. Fixing only those compile errors without
assigning lifecycle mutation can leave a failed root run observably in `dispatched`.

## Storage references and metrics servers

`StorageReference.StorageInstance` now resolves only through the live StoreRegistry entry with that exact name.
Remove default-bucket, unnamed-store, and fallback-store assumptions. A missing store is observable and the offloaded
body is excluded for that revision; it is not automatically replayed when a store appears later. See
[Post-G tag-safety migration](migration-post-g-tag-safety-closeout.md).

`metric.Server.Start()` now binds synchronously, returns a bind/startup error directly, and serves in its own managed
goroutine. Call `Start()` directly, not inside another goroutine, and call `Stop()` to close and join the server.

## Product proof

For each downstream product, record:

- the exact SemStreams tag and migration commit;
- successful compilation and generated-schema/client changes;
- successful strict configuration and flow validation;
- removal of every retired surface actually used by the product;
- green unit and integration suites; and
- green end-to-end proof for the product capabilities it advertises.

Products do not need to reproduce SemStreams' full release suite. They do need to prove their own ingest, graph
mutation/read, query, restart, and eventual derived-view behavior as applicable. A downstream failure should be
classified as either an ordinary local migration or a concrete missing framework capability before framework work is
proposed.
