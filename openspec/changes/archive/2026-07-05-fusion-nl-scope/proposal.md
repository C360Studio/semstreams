# Domain-scoped NL retrieval for a fusion Lens (gh#463, ADR-071)

## Why

`pkg/fusion`'s `Engine.Fuse` resolves NL seeds over the **whole shared embedding
index** with no per-lens domain filter (`engine_lens.go:88` →
`RetrievalClient.Resolve`). When multiple lens instances share one index (semsource's
`code` + `docs` lenses, ADR-0004), NL retrieval for the smaller domain is **diluted**:
on httpx (1304 Python code entities + 30 docs), `doc_context "what exceptions can be
raised"` returns Python test functions above `docs/exceptions.md`; the same query with
docs not drowned returns 100% docs, top-1 correct. It is a scope/ranking problem, not
hydration.

ADR-071 (Accepted) records the decision: **Option 1** — an optional `Scope []string`
on `fusion.Request` plumbed to the embedding search, filtering candidates **at the
source** so the small domain is never crowded out of the ranked window. This is the
only option that fixes *severe* dilution (docs fell entirely below `resolveLimit = 40`).
Scope is a cross-component NATS RPC contract change (`graph.embedding.query.search`),
hence the ADR.

The pre-Accept adversarial review reshaped the implementation (all folded into ADR-071):
a **BLOCKING** warm-cache no-op, a `[]string` requirement, a cross-repo compile break,
and a convergence decision with an existing overlapping filter.

## What Changes

- **`Scope []string` on `fusion.Request`** — a list of dot-delimited entity-ID
  **prefixes**, OR-matched; empty/nil = no filter (backward-compatible). Prefix, not
  glob (cheapest match, no `path.Match` dotted-delimiter ambiguity); list, not scalar
  (semsource "all code" spans `golang`/`python`/`ts`/`svelte`, "all docs" spans
  `source.doc` + `source.chunk` — one prefix cannot express either).

- **Thread scope via a struct param, not a positional add.** `RetrievalClient.Resolve`
  becomes `Resolve(ctx, ResolveQuery{Query, Mode, Scope, Limit})`. Scope is NL-only, but
  `Resolve` is shared across symbol/prefix/nl; a struct param contains the cross-repo
  blast radius (the sole prod impl `fusionnats.Client`, the in-repo fake + 7 call sites,
  and the **cross-repo** `../semsource/.../fusiontest/memgraph.go MemGraph.Resolve`) and
  won't re-break on the next dimension.

- **Add `Scope []string` to `graph-embedding`'s `SearchRequest`** (the cross-component
  RPC contract) and apply it in **BOTH** `findSimilarEntities` paths — the warm
  `FindSimilarFromCache` (steady-state) and the cold KV-scan fallback — via one shared
  helper, filtering **before** `CosineSimilarity` / before the per-candidate
  `GetEmbedding` KV round-trip. Applying it only to the cold path (as the ADR's first
  draft did) would be a **silent no-op for essentially every warm-production query** —
  the single must-not-miss. `graph.query.semantic`'s handler forwards raw bytes, so it
  needs no code change.

- **Extract a shared `graph.MatchesAnyIDPrefix(id string, prefixes []string) bool`
  helper** (dot-prefix, trailing-dot boundary, empty = match-all) and route the new
  scope filter through it. Reuse the `[]string` + empty=no-op convention already proven
  in `graphrag.filterEntityIDsByType` and the dot-prefix convention on
  `graph.PrefixQueryRequest`.

- **Converge the existing overlapping filter (`graph-query`).**
  `graphrag.handleStrategySemantic` already post-filters the semantic path via
  `filterEntityIDsByType` (an Option-2-shaped weak post-filter — exactly what fails
  severe dilution). This change routes graph-query's semantic path through the
  source-level `Scope` so there is **one** ID-scoping responsibility, not two drifting
  ones. Note the axis wrinkle to resolve in implementation: graphrag filters by the
  **type segment** (mid-ID, position 5) while `Scope` matches a **leading prefix**
  (domain/system, positions 3–4) — so `MatchesAnyIDPrefix` covers the scope axis, and
  the type-segment filter is either re-expressed against the shared helper or kept as a
  documented-distinct axis layered on top. The invariant: no second post-retrieval ID
  filter that silently duplicates source-level scope.

- **Keep `Scope` distinct from the prefix-mode field.** `ResolveModePrefix`'s `Prefix`
  *is* a deterministic query; NL `Scope` is a *filter on embedding candidates*. Share
  the matcher helper, not the field.

## Impact

- **Affected specs:** `fusion` (ADDED: Scope on Request + resolve threading — the
  capability seeded by gh#475), `graph-embedding` (seeded: SearchRequest scope + the
  both-path filter contract), `graph-query` (seeded: the single-scope-responsibility
  convergence).
- **Affected code:** `pkg/fusion/contract.go` (`Scope`), `pkg/fusion/retrieval.go`
  (`ResolveQuery` struct param), `pkg/fusion/engine_lens.go` (pass scope),
  `pkg/fusion/fusionnats/client.go` (`resolveSemantic` body, conditional insert),
  `processor/graph-embedding/query.go` (`SearchRequest.Scope`, both-path filter),
  `graph/embedding/storage.go` (`FindSimilarFromCache` filter), a new
  `graph.MatchesAnyIDPrefix` helper, and `processor/graph-query/graphrag.go`
  (convergence). Cross-repo: `../semsource/.../fusiontest/memgraph.go` `MemGraph.Resolve`
  updates to the struct param (coordinated on tag).
- **Cross-repo contract:** additive `Scope []string` on `graph.embedding.query.search`;
  unknown-field-tolerant decode means an un-migrated server degrades to unscoped.
- **Coverage gap (flagged):** no e2e tier exercises `pkg/fusion` / the semantic NL path
  — the change adds a production-decoder round-trip test (SearchRequest-with-scope →
  graph-embedding decode → filter applied) and ideally an integration test through
  `fusionnats.Client → graph-query → graph-embedding`.
- **No `did_you_mean` scope** in this change: `e.miss` → `Names` stays unscoped (a
  scoped docs-lens miss can suggest code names) — documented, deferred.
