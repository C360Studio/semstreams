# Design — Fusion Graph Projection Facet

## Context

Grounded against `pkg/fusion` on main (post-beta.152): the request already carries a `Want`
facet vector (`body`, `relations`, `paths`, `impact`) with per-facet response fields; `Entity`
carries the full raw `[]message.Triple` (all property facts + evidence are in hand at projection
time with zero extra I/O); `Edge` from `GraphReader.Neighbors` carries only Predicate+Target,
but `refFor` already fetches the counterpart entity for every edge — so per-edge evidence in
BOTH directions is available without new reads (outgoing: the seed's own triple; incoming: the
fetched counterpart's triple). `Triple.IsRelationship()` shape-sniffs when Datatype is empty —
unusable for this facet (acceptance test 1). `IndexStatus` is sampled once, pre-resolve.

## Goals / Non-Goals

**Goals:** lossless, honest, additive graph projection per gh#533's eight acceptance tests;
zero change to the v1 wire for non-requesting callers; engine-owned generic semantics.
**Non-Goals:** snapshot isolation; GraphQL; any lens API break; evidence synthesis of any kind.

## Decisions

### D1 — `WantGraph Want = "graph"`, `Response.Graph *GraphProjection` (omitempty)

Additive facet exactly like `WantPaths`/`WantImpact`. Default want-set (empty `want:`) does NOT
include it. `ContractVersion` unchanged — the shape is purely additive.

### D2 — Projection shape

```go
type GraphProjection struct {
    Nodes        []GraphNode `json:"nodes"`
    Edges        []GraphEdge `json:"edges,omitempty"`
    ViewRevision ViewRevision `json:"view_revision"`
    Truncated    bool        `json:"truncated"` // any node/edge-level truncation below
}
type GraphNode struct {
    Handle    string      `json:"handle"`          // same opaque handle as Node.Handle
    Facts     []GraphFact `json:"facts,omitempty"`
    FactsTruncated bool   `json:"facts_truncated,omitempty"`
    FactsDropped   int    `json:"facts_dropped,omitempty"`
}
type GraphFact struct {
    Predicate string          `json:"predicate"`          // original, verbatim
    Value     json.RawMessage `json:"value"`              // typed passthrough, no coercion
    Datatype  string          `json:"datatype,omitempty"` // verbatim; absent stays absent
    Evidence  []GraphEvidence `json:"evidence,omitempty"`
}
type GraphEdge struct {
    ID        string          `json:"id"`        // stable: "<source>|<predicate>|<target>"
    Source    string          `json:"source"`    // subject handle (true graph direction)
    Target    string          `json:"target"`    // object handle
    Predicate string          `json:"predicate"` // original, verbatim
    Direction string          `json:"direction"` // "outgoing"|"incoming" relative to the seed node
    Evidence  []GraphEvidence `json:"evidence,omitempty"`
    Truncated bool            `json:"truncated,omitempty"` // evidence list capped
}
type GraphEvidence struct {
    Source     string  `json:"source,omitempty"`
    Timestamp  string  `json:"timestamp,omitempty"`  // RFC3339; omitted when zero
    Confidence *float64 `json:"confidence,omitempty"` // pointer: absent ≠ 0.0 — never fabricate
    Context    string  `json:"context,omitempty"`
}
type ViewRevision struct {
    Start uint64 `json:"start"` // IndexedRevision sampled before resolve/fetch
    End   uint64 `json:"end"`   // re-sampled after the fetch phase
    Coherent bool `json:"coherent"` // Start == End
}
```

Implementation may adjust field mechanics (e.g. evidence dedup keys) but the JSON semantics
above — verbatim predicates, typed values, pointer-confidence, omitted-absent — are the
contract. Edge direction: Source/Target always express the true subject→object direction;
`Direction` says which side the seed is on, so opposite-direction facts between the same pair
are two edges with swapped Source/Target and both retained (acceptance 3).

### D3 — Declaration-driven classification (acceptance 1)

A seed triple projects as an EDGE iff (a) its predicate is declared by the lens's EdgeSpecs
(any facet — reuse `edgePredicates` over all specs), OR (b) the triple carries the explicit
`message.EntityReferenceDatatype` (`"@id"`). Everything else is a FACT, including a string
value with perfect six-part shape and empty datatype. Do NOT call `Triple.IsRelationship()`
(its empty-datatype branch shape-sniffs). Case (b) edges whose predicate has no lens role still
project (the projection is generic); their targets resolve to handles only (no Ref semantics
needed).

### D4 — Evidence sourcing and distinctness (acceptance 2, 3, 4, 5)

Outgoing edges: evidence from the seed's own triple(s). Incoming edges: evidence from the
counterpart entity's matching triple(s) — the counterpart is already fetched. Multiple triples
with the same (source, predicate, target) become ONE edge with MULTIPLE `Evidence` entries
(acceptance 4); different predicates or directions are always distinct edges (acceptance 2, 3).
Absent evidence fields are omitted (`Confidence` is a pointer so absent ≠ zero) — nothing is
defaulted (acceptance 5). Facts likewise: one fact per (predicate, value) with the triple's
evidence; multi-valued predicates produce multiple facts.

### D5 — Truncation observability (acceptance 6)

Per-node caps `maxGraphFactsPerNode` / per-projection `maxGraphEdges` (constants, generous
defaults) with explicit `FactsTruncated`/`FactsDropped` and projection-level `Truncated`,
fully independent of the v1 node/body `budgeter` and the `maxRelationsPerNode` role cap. The
graph facet does not consume the body byte budget (it has its own caps); the existing
`Response.Truncated` top-level bit is NOT overloaded. Review addition (M1): FACT evidence is
capped at 8 like edge evidence, with `GraphFact.Truncated` + projection `Truncated` — a
multi-source assertion cannot turn one fact into an unbounded wire payload.

### D6 — View revision contract (acceptance 7)

Sample `IndexStatus.IndexedRevision` at the existing pre-resolve point (already in the
response) and re-sample after the last graph fetch; report both in `ViewRevision` with
`Coherent = (Start == End)`. Coherent=true ⇒ the projection reflects one indexed revision.
Coherent=false ⇒ documented weaker contract: the response may span revisions; a consumer
needing coherence refreshes (bounded retry is the consumer's policy). No snapshot isolation —
this is the precise weaker contract the issue explicitly allows. If the status re-sample fails,
`End=0, Coherent=false` (degrade honestly, never guess).

## Risks / Trade-offs

- [Projection cost] → facts are free (triples in hand); edges reuse the Neighbors + counterpart
  fetches the relations facet already performs; caps bound the rest. No new I/O class.
- [Evidence for incoming edges when the counterpart fetch failed] → edge still projects with
  empty evidence (absent-not-fabricated); consistent with degrade-don't-fail.
- [`@id`-datatyped triples to absent entities] → edge projects with the target handle; handle
  resolution is the consumer's next call; referential stubs make most targets real anyway.

## Migration Plan

Additive; no config, no data, no adapter changes. Implementation correction: `fusionnats` is
the engine-side RetrievalClient, not a Response transport — no in-repo code (de)serializes
`fusion.Response` (it leaves via product service boundaries as opaque JSON), so pass-through
holds vacuously and the Response-JSON round-trip test proves the facet survives any opaque
boundary losslessly. Rollback = revert.

## Open Questions

- Cap defaults (proposal: 64 facts/node, 256 edges/projection, 8 evidence/edge) — tune at
  implementation review.
