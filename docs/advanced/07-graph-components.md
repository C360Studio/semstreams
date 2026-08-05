# Graph Components Reference

This page is a bounded current-state reference. It does not invent portable
configuration recipes or universal consistency guarantees. For exact config,
use each package's generated schema, `DefaultConfig`, and current capability
spec. The frozen
[graph-state inventory](../proposals/graph-state-read-write-inventory.md) owns the
commit-pinned bucket and caller census.

## Current roles

| Component | Current role | Declared durable ownership |
|---|---|---|
| `graph-ingest` | Accepts graph facts/mutations and serves entity reads | Authority, suffix index, ingest guard |
| `graph-index` | Builds topology, identity, and predicate indexes | Graph-index bucket family |
| `graph-index-spatial` | Builds/query spatial geohash view | Spatial bucket family |
| `graph-index-temporal` | Builds/query current observed-time view | Temporal bucket family |
| `graph-embedding` | Builds optional statistical/neural embeddings | Embedding and dedup family |
| `graph-clustering` | Periodically computes communities/anomalies | Community/anomaly family |
| `graph-query` | Routes an enumerated hand-written operation set | None |
| `graph-gateway` | Provisional query-only HTTP facade over NATS requests | None; anomaly access is GS-10 debt |

One declared owner does not make multiple runtime instances safe. ADR-090 makes
durable owners single-active until an owner proves active/active convergence.
Query-only, queue-group-safe responders may scale only under a proven
request/reply contract.

## Configuration truth

Do not copy bucket names or port shapes from prose. Current port validators use
package-specific `subject` fields, and not every component uses the same trigger
or storage mechanism. Temporal configuration uses the package's enumerated
`time_resolution`, not an invented duration bucket size.

Before deploying a component:

1. inspect its generated schema and `DefaultConfig`;
2. verify the exact subjects and dependencies in that package;
3. keep durable ownership single-active unless the owner has accepted
   active/active proof; and
4. use the applicable OpenSpec capability as the behavior contract.

## Query surfaces

`graph-query` handles an enumerated hand-written operation set; its registration
table in `processor/graph-query/query.go` is the runtime source of truth. The HTTP
gateway is a hand-written, query-only,
GraphQL-shaped router; it is not a general GraphQL executor, has no mutation type,
does not read graph KV through a `QueryManager`, and exposes no implemented MCP
graph tools.

The gateway advertises `graph.query.capabilities`, but no component subscribes.
There is no `QueryCapabilityProvider` contract and no served general
`*.capabilities` discovery family. Treat those advertised routes as GS-12
read-front debt, not runtime capability discovery.

See [Query Access Patterns](../concepts/11-query-access.md) for admitted caller
guidance and [graph-gateway README](../../gateway/graph-gateway/README.md) for the
enumerated provisional facade operations.

## Current versus target consistency

Current evidence is incomplete. The GS program adds proof one owner at a time.

| Component | Current evidence | Scheduled target |
|---|---|---|
| `graph-ingest` | Entity value without revision in query reply | GS-01: value plus revision |
| `graph-index` | Implementation-specific readiness | GS-04: declared authority coverage |
| Spatial | No uniform lifecycle/status contract | GS-06: owner status |
| Temporal | No uniform lifecycle/status contract | GS-07: owner status |
| Embedding | Operation-specific capability behavior | GS-08: work/capability state |
| Clustering | Periodic recompute behavior | GS-09: cycle/staleness evidence |
| Query/gateway | Operation-specific routing | GS-12: declared answer source |

Protocol alone does not determine consistency. A KV-watch bootstrap hydrates
current matching inputs; it does not prove derived outputs are repaired. Each
durable owner must provide explicit repair/redrive, reset, readiness, and failure
evidence through its bounded GS slice.

## Recovery and failure boundary

| Failure | Current safe interpretation |
|---|---|
| `graph-ingest` unavailable | New authority acceptance and entity reads fail |
| Derived owner unavailable | Its view may be stale or unavailable; do not infer empty truth |
| Query responder unavailable | The named operation fails or times out |
| Gateway unavailable | Remote facade is unavailable; authority is unchanged |

Bootstrap from current inputs repairs a derived view; it is not deployment
backup or restore. Connected deployments use JetStream replication, while
edge/offline operators maintain infrastructure backups as checkpoints.
SemStreams adds no recovery runtime beside those deployment operations. Derived
rebuild remains capability-scoped and effect-free; authorized inference
application is a separate operation.

## Monitoring

Use each component's health/status and Prometheus metrics as operational
evidence. Metrics and service logs are not a query audit contract. Until the
applicable GS slice lands, do not translate generic health into a fabricated
revision watermark or complete-view guarantee.

## Related documentation

- [Architecture Overview](../basics/02-architecture.md)
- [Query Access Patterns](../concepts/11-query-access.md)
- [Spatial-Temporal Queries](../concepts/30-spatial-temporal-queries.md)
- [ADR-090](../adr/090-authoritative-current-state-and-materialized-views.md)
- [Canonical graph-state program](../proposals/graph-state-read-write-program.md)
