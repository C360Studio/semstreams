# pkg/fusion: per-facet edge selection for a Lens (gh#475)

## Why

`fusion.Lens.Edges()` returns **one** `[]EdgeSpec` list that the engine uses for
**three** distinct facets:

- `computeImpact` — incoming (reverse) BFS over the edge predicates
  (`engine_facets.go:47`)
- `computePaths` — outgoing DFS over the edge predicates (`engine_facets.go:100`)
- the `relations` map on each result node (`engine_lens.go:176`)

All three derive their predicate set from the same undifferentiated `[]EdgeSpec`
via a single flattener, `edgePredicates` (`engine_lens.go:219`). A lens therefore
**cannot** say *"walk this edge for `relations` but not for `impact`."* Every
declared edge participates in all three facets.

That is a real fidelity problem for any lens with a **containment** edge. semsource's
code lens (ADR-0004) must declare `CodeContains` (file→symbol) so `code_context` can
show a symbol's container and contents in `relations` — but that same predicate then
pollutes `code_impact`, whose incoming-`contains` walk pulls every dependent's
`file → folder → repo` into the blast radius. The impact **count** then mixes
structural containment ancestry with real reverse-dependents. Measured on httpx:

```
code_impact("BaseClient") = {nodes: 5} = 2 real subclasses + 3 structural containers
```

The containment ancestry dominates a small dependency set, so the count reads as
noisier / larger than the true reverse-dependency closure. This is a **framework
SPI expressiveness gap** — the engine owns resolve/rank/budget/walk; the lens should
be able to annotate which facets each edge feeds.

## What Changes

- **Add an optional per-`EdgeSpec` facet mask.** A new `Facets []Facet` field on
  `EdgeSpec` (`pkg/fusion/lens.go`), where `Facet` names the three edge-walk facets
  — `relations`, `paths`, `impact` (the same three the caller already requests via
  the existing `Want` vocabulary). A spec lists the facets it should feed.

- **Empty `Facets` means all three — fully backward-compatible.** Every existing
  lens (and the `refLens` fixture) declares `EdgeSpec`s with no `Facets` field; the
  zero value (`nil`) MUST mean "participate in every facet," so current behavior is
  byte-for-byte unchanged. The field is a *restriction*, opted into only when a lens
  wants an edge to skip a walk.

- **Make the single choke point facet-aware.** `edgePredicates`
  (`engine_lens.go:219`) gains a facet-filtered variant; the three callers each pass
  their own facet — `relations` (needs the forward+reverse role maps), `computePaths`
  and `computeImpact` (predicates only). An `EdgeSpec` is included for a facet iff
  its `Facets` is empty or contains that facet.

- **No change to `Want`, `Request`, `Budget`, or the resolve/rank/hydrate path.**
  This is a narrow SPI addition on the edge-walk side only.

The product half is semsource's: once the mask exists, the code lens sets
`CodeContains.Facets = {relations}` (and `{relations, paths}` for any containment
edge it also wants in outgoing paths), so containment feeds `relations`/`code_context`
but no longer inflates `code_impact`.

## Impact

- **Affected specs:** new capability `fusion` (seeded lazily by this change — the
  Lens SPI edge model + this per-facet contract, distilled from `pkg/fusion` and
  verified against code). Scope is the **edge-walk / facet** contract; resolve, rank,
  budget, and hydration are separate concerns, seeded when a change first touches them.
- **Affected code:** `pkg/fusion/lens.go` (`EdgeSpec` field + `Facet` type),
  `pkg/fusion/engine_lens.go` (`edgePredicates` facet filter + `relations` caller),
  `pkg/fusion/engine_facets.go` (`computeImpact` / `computePaths` callers), and the
  `lens_test.go` / `engine_facets_test.go` fixtures.
- **No breaking change, no ADR.** `EdgeSpec` is an in-process SPI struct constructed
  by each product's Lens, not a cross-repo wire contract; the addition is a
  backward-compatible field (empty = all). No NATS/RPC contract is touched. (Contrast
  gh#463, whose `Scope`-on-embedding-search fix *does* change a cross-component RPC
  contract and therefore carries an ADR.)
