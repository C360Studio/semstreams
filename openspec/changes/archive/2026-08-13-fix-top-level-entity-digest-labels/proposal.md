# Change: Complete human labels on compact top-level entity digests

## Why

GlobalSearch auto-summary and searchGraph semantic fallback return compact `EntityDigest` rows without consistently
projecting display text already present in canonical entity state. Non-representative auto-summary rows use an entity
ID fragment, while direct fallback rows omit label and type.

## What changes

- Batch-resolve display text for the bounded final top-level ID set in both deficient compact-result branches.
- Preserve retrieval order, IDs, counts, and each branch's existing relevance semantics.
- Populate direct-fallback type through the existing entity-ID parser without rebuilding its rows.
- Retain instance fallback for missing, ordinarily unavailable, or unresolved entities.
- Propagate authoritative graph-state contract failures.
- Add deterministic unit, integration, and semantic E2E coverage without arbitrary sleeps.

## What does not change

No subject, operation, payload, configuration, index, bucket, response field, property projection, label registry,
readiness protocol, or durable state is added. Community representative behavior and LocalSearch remain unchanged.

## Impact

Affected production code is confined to graph-query compact response composition. Existing optional label/type fields
become populated more consistently. One existing graph-ingest batch request is added per affected compact response,
bounded at 100 IDs for auto-summary and 8 IDs for direct fallback. Actual carrier limits remain outcome-observed by
the shared request/reply layer.
