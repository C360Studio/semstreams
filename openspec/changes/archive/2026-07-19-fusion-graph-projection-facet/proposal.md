# Fusion Graph Projection Facet

## Why

gh#533 (semsource blocker): structured graph consumers (the semsource source-knowledge
workbench's drill-down) cannot safely recover graph facts from the fusion v1 response — relation
`Ref`s carry no target handle, predicates/directions are collapsed into lens role names, typed
property facts and per-fact/per-edge evidence are not exposed, the per-role relationship cap
truncates silently, and `IndexStatus` is sampled before the fetch phase so no coherence contract
exists. Consumers must not reconstruct any of this by shape-sniffing six-part strings or
fabricating provenance — the engine must project it losslessly and honestly.

## What Changes

- **New additive `Want` facet `graph`** on the existing `pkg/fusion` request contract, producing
  an optional `Response.Graph` projection (omitempty). The v1 wire contract is byte-identical for
  requests that don't ask for it — no versioning needed.
- **The projection returns**: typed property facts (original predicate, value, datatype,
  evidence); explicit directed edges with source and target handles, original predicate,
  direction, and stable identity; parallel predicates, opposite-direction edges, and multiple
  evidence contributions preserved distinct; per-fact/per-edge evidence (source, timestamp,
  confidence, context) with absent values omitted, never fabricated; explicit per-node fact/edge
  truncation metadata independent of the node/body budget; and a documented view-revision
  consistency contract (index revision sampled before and after the fetch phase — equal bounds
  mean one coherent view, unequal bounds are the documented weaker contract for consumer
  refresh/rejection).
- **Classification is declaration-driven, never shape-driven**: a triple projects as an edge iff
  its predicate is lens-declared (EdgeSpec) or it carries the explicit `@id` entity-reference
  datatype. A literal that merely looks like a six-part entity ID stays a property
  (`Triple.IsRelationship`'s empty-datatype shape fallback is deliberately NOT used here).
- Lens contract unchanged: lenses keep declaring domain predicates and human roles; the engine
  owns the generic projection and honesty semantics (per the issue's boundary).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `fusion`: ADDED requirements for the graph facet — request/response shape, declaration-driven
  classification, evidence honesty, distinctness guarantees, truncation observability, view
  revision contract, v1 compatibility. Existing requirements untouched.

## Impact

- **Code**: `pkg/fusion` only (contract.go types, engine facet build, tests). NATS/HTTP/MCP
  adapters transport the JSON unchanged; `fusionnats` needs no schema change (verify pass-through).
- **Consumers**: semsource workbench (the blocked consumer — unblocks its canonical graph
  drill-down); existing fusion v1 agents unaffected (facet is opt-in additive).
- Acceptance: the eight contract tests enumerated in gh#533 map 1:1 to spec scenarios.

## Non-goals

Per the issue: no semsource-specific code or lens in semstreams; no new parallel endpoint; no
GraphQL requirement; no change to the entity-ID contract, predicate registration, or
relationship-role contracts; no replacement of v1 search/context/paths/impact behavior; no
snapshot isolation (the view-revision bounds are the honest, cheaper contract).
