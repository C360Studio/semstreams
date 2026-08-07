## Why

Foundation B has completed its grammar, owned migration, shared-consumer, and renderer/runtime checkpoints, but its
approved breaking behavior is not yet durable OpenSpec truth. This change records that exact target before release
validation, so adopters see one strict port language instead of stale flat fields, aliases, and divergent projections.

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

## Capabilities

### New Capabilities

- `component-discovery`: Defines canonical normalized port facts, consistent management/flow projections, exact-read
  semantics, and graph-gateway's strict three-family discovery contract.

### Modified Capabilities

- `component-runtime-config`: Replaces flat/builder precedence with the strict common declaration/runtime envelope
  and complete-replacement merge behavior.
- `stream-provisioning`: Derives ordinary stream declarations from normalized JetStream facts and bounds the
  gated-DAG specialization.
- `framework-composition`: Makes canonical `nats-request` facts the only static graph-mutation topology contract.
- `graph-ingest`: Requires the sole mutation provider to use the strict canonical request-port declaration.

## Impact

The implementation spans the exported component port API, shipped JSON configurations, component declarations,
runtime renderers, registry/flow/management views, schema generation, stream provisioning, and the graph-gateway and
graph-ingest composition contracts. SemStreams is the direct consumer. The later migration holdout set is `semdev`,
`semmachina`, `semsource`, `semboids`, `semdragon`, `semstreams-ui`, `semteams`, `semconnect`, `semlink`, and `semops`;
those downstream repositories do not shape or block this framework contract.

## Non-goals

- No graph-query semantic change or graph-query spec delta.
- No Foundation C `Discoverable`/snapshot cutover.
- No new communication path, query API, payload type, orchestration mechanism, custom kind, compatibility shim, or
  migration window.
- No claim that checkpoints 1-4 satisfy the checkpoint-5 release, E2E, reviewer, or post-B inventory gates.
