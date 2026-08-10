<!-- markdownlint-disable MD041 -->

## Why

Foundation work made `ENTITY_STATES` authority, typed mutations, exact reads, component ports, and Registry admission
coherent. The remaining graph-query surface still has multiple competing contracts: sixteen operations are registered
from two sites, `localSearch` can disappear as a responder, optional KV watches can serve stale generations after
loss, GraphQL advertises an unserved field and discards existing error classifications, embedded consumers decode
copied reply shapes, and an unused general client retains direct buckets and process-lifetime cache state.

This change closes that bounded contract. It does not redesign graph authority, indexes, hierarchy, retention,
research orchestration, or readiness.

## What Changes

- Define one internal sixteen-operation graph-query inventory and one required `graph.query/v1` provider input.
- Keep all sixteen responders present for every successful graph-query Start; optional view availability becomes a
  classified query outcome rather than transport-level no-responder.
- Add exact declared request outputs for graph-gateway and the existing research consumers. Libraries own no ports.
- Replace the partition watch lifecycle with a fresh-map generation supervisor. Consolidate the content-addressed,
  optional-summary reader on the existing catalog-backed `pkg/graphview.View`, with statistical fallback for every
  unavailable outcome.
- **BREAKING** Remove GraphQL `capabilities`, leaving exactly fourteen graph-query-backed and nineteen total served
  root fields.
- **BREAKING** Make `semanticSearch` the sole advertised and projected semantic-search field; remove the hidden
  `similaritySearch` gateway spelling with no alias.
- Preserve existing classified error class and non-empty code in GraphQL error extensions without changing status or
  inventing authority.
- Standardize framework-owned embedded consumers on one `graph.UnwrapQueryResponse` pass and preserve every admitted
  successful representation.
- Require every successful global-search response to report its actual terminal strategy.
- Preserve `pkg/fusion/fusionnats.Client`, its constructor, six operations, lazy readiness, and downstream role.
- **BREAKING** Delete only the unused mixed direct-KV `graph/query.Client` cohort.
- **BREAKING** Delete the unadmitted agentic `search_graph` and `summarize_graph` wrappers completely.
- Correct stale graph-index predicate-layout text to the already-shipped raw canonical representation; runtime index
  layout does not change.

## Capabilities

### New Capabilities

- `gateway-query-routing`: Owns the exact GraphQL root-field inventory and production route obligation.
- `gateway-error-projection`: Projects existing classified error authority into GraphQL error extensions.

### Modified Capabilities

- `component-discovery`: Adds the exact versioned graph-query provider/consumer port topology.
- `graph-query`: Adds the admitted operation family, stable responder, generation-safe partition cache, shared
  serving-view summary reader, canonical success decoding and representation preservation, terminal strategy, and
  embedded-client boundary.
- `agentic-tools`: Removes two unadmitted query wrappers and their complete exported/configuration surfaces.
- `fusion`: Preserves the operation-specific NATS adapter while converging reply decoding on production shapes.
- `graph-index`: Corrects stale predicate representation text only.

## Impact

The implementation touches graph-query lifecycle and handlers, graph-gateway routing/error projection, research and
fusion adapters, component declarations and shipped configurations, agentic tool registrations, tests, generated
schemas, and adopter documentation. The sixteen existing graph-query subjects and producer success payloads remain
wire-stable except that successful global search fills its existing `strategy` field truthfully.

Remote applications use the admitted GraphQL fields. Embedded framework consumers use named operation-specific
adapters over declared request/reply ports. Downstream projects receive the break list and own their dependency bump,
compilation fixes, configuration migration, flow validation, and product E2E; they neither shape nor block this change.

## Non-goals

- No new query operation, public subject catalog, general embedded client, MCP graph surface, raw-KV fallback, or
  producer wire envelope.
- No bucket, stream, consumer, service, retry knob, status key, clustering readiness producer, or durable recovery
  mechanism.
- No BM25 persistence, payload chunking, blob GC, hierarchy redesign, alias-index repair, anomaly repair, or research
  orchestration change.
- No `MergePortConfig` redesign, executor-dependency metadata, discovery redesign, or false library port ownership.
- No compatibility shim, deprecated alias, dual route, accepted no-op skip key, or downstream implementation audit.
- No activation or modification of the suspended `semantic-tier-split` change.
