# Tasks — pkg/fusion per-facet edge selection (gh#475)

> Scoping change (Proposed). Tasks unchecked; implementation follows scope approval
> (`/opsx:apply` on a `feat/` branch).

## 1. SPI: the facet mask

- [x] 1.1 Add a `Facet` type (`pkg/fusion/lens.go`) with constants `FacetRelations`,
      `FacetPaths`, `FacetImpact` whose string values match the existing `Want`
      values (`relations`/`paths`/`impact`), so lens authors and callers share one
      vocabulary. Do NOT reuse `Want` directly (it also has `WantBody`, which is not
      an edge-walk facet — a distinct type keeps `EdgeSpec.Facets` type-safe).
- [x] 1.2 Add `Facets []Facet` to `EdgeSpec` (`lens.go:98`). Document the invariant:
      **empty/nil = participates in all three facets** (backward-compatible zero
      value); a non-empty list restricts participation to the named facets.

## 2. Engine: facet-aware predicate selection

- [x] 2.1 Add a facet filter at the single choke point `edgePredicates`
      (`engine_lens.go:219`): a spec is included for facet `f` iff
      `len(spec.Facets) == 0 || slices.Contains(spec.Facets, f)`. Prefer a new
      `edgePredicatesFor(specs, f)` variant over mutating the existing signature, so
      the change is additive.
- [x] 2.2 `relations` (`engine_lens.go:176`) passes `FacetRelations` and keeps using
      the forward + reverse role maps.
- [x] 2.3 `computePaths` (`engine_facets.go:100`) passes `FacetPaths` (predicates only).
- [x] 2.4 `computeImpact` (`engine_facets.go:47`) passes `FacetImpact` (predicates only).
- [x] 2.5 Confirm no other caller of `edgePredicates` exists (grep); if any, route it
      through the appropriate facet.

## 3. Tests

- [x] 3.1 Backward-compat: an `EdgeSpec` with no `Facets` participates in all three
      facets (extend the `refLens` fixture assertions — existing behavior unchanged).
- [x] 3.2 Impact-excluded edge: a spec with `Facets: {relations}` populates the
      `relations` map but is NOT walked by `computeImpact` — mirror the semsource
      containment case (a `contains`-style edge feeds relations; impact returns only
      the real reverse-dependents, not the containment ancestry).
- [x] 3.3 Paths selectivity: a spec limited to `{relations, impact}` is excluded from
      `computePaths`.
- [x] 3.4 Multi-facet: a spec with `{relations, paths, impact}` behaves identically to
      an empty (all) spec.
- [x] 3.5 Extend `engine_facets_test.go` (`facetGraph`/`fuseFacet` builders) and the
      `lens_test.go` `refLens` `Edges()` fixture.

## 4. Spec + gates + close

- [x] 4.1 `openspec validate --strict`.
- [x] 4.2 Gates: `go test -race ./pkg/fusion/...`, `task lint`, schema no-drift
      (pkg/fusion has no generated schema — confirm), `go vet -tags=integration`.
- [x] 4.3 semstreams-reviewer pre-merge — APPROVE-WITH-NITS. Crux verified: the
      production `fusionnats.Client.Neighbors` filters by predicate set (nil=all,
      test-locked), so the facet exclusion is real in production, not a fake-only
      green. Folded: LOW (re-guard impact/paths by predicate so exclusion is robust
      to any RetrievalClient impl, not just the filtering one) + NIT (pin `Facet`
      values to their `Want` twins via constant conversion).
- [x] 4.4 Archive → promote `fusion` into `openspec/specs/`.
- [ ] 4.5 PR; CI green; merge; tag. (No e2e tier directly covers `pkg/fusion` — it is
      consumed by semsource, not the semstreams e2e stack; noted coverage gap.)
- [ ] 4.6 Confirm back to semsource on gh#475 / upstream-asks #17 (code lens sets
      `CodeContains.Facets = {relations}` so containment stops inflating `code_impact`).
