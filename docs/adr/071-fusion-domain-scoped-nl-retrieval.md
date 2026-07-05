# ADR-071: Domain-scoped NL retrieval for a fusion Lens (gh#463)

## Status

**Proposed — Option 1 selected (2026-07-05); pending pre-Accept adversarial review
and open-question resolution.** The mechanism decision is made: **Option 1** — a
`Scope` on `fusion.Request` plumbed to the embedding search. It is recorded as an ADR
because it changes a **cross-component NATS RPC contract** (`graph.query.semantic` +
`graph.embedding.query.search`), the class of decision an ADR exists to hold. Before
Accept, the four open questions below are resolved and the mechanism gets a
code-grounded adversarial review (framework-ADR discipline —
`feedback_adversarial_review_framework_adr`); then the mechanics move into an openspec
change against the `fusion` + `graph-embedding` capability specs and implementation
follows.

Scopes gh#463 / semsource upstream-asks #16.

## Context

`pkg/fusion`'s `Engine.Fuse` resolves NL seeds over the **whole shared embedding
index** with no per-lens or per-request domain filter:

- `Engine.Fuse` calls `e.graph.Resolve(ctx, query, mode, resolveLimit)`
  (`engine_lens.go:88`) with `mode ∈ {symbol, prefix, nl}`.
- The `Lens` interface exposes no scope hook (`Name`/`ResolveMode`/`Edges`/`Label`/
  `Kind`/`Location`/`Hydrate`).
- `fusion.Request` carries only `Query`, `Want`, `Budget` (`contract.go:36`).
- The production NL path is `fusionnats.Client.resolveSemantic` (`client.go:130`) →
  `graph.query.semantic` → `graph.embedding.query.search` → `findSimilarEntities`
  (`processor/graph-embedding/query.go:221`), which scans **all** generated entity
  IDs, scores cosine, and truncates. **No scope/type/prefix filter exists at any hop**
  — `SearchRequest` is `{Query, Limit}` only (`query.go:65`).

Consequence for a product running multiple lens instances over one embedding index:
NL retrieval for the smaller domain is **diluted by the larger one**. semsource runs
a `code` lens and a `docs` lens over one index; measured on httpx (1304 Python code
entities + 30 markdown docs):

- `doc_context "what exceptions can be raised on a failed request"` → returned Python
  **test functions** ranked above `docs/exceptions.md`.
- The same query on a docs-dominant index → **100% documents, top-1 correct**.

The doc content is embedded and retrievable — it is a **scope/ranking** problem, not
hydration. Not MVP-blocking (a broad `code_search` retrieves docs well; `doc_context`
is accurate when domains are balanced), but it blocks accurate small-domain NL
retrieval on mixed corpora.

## Decision

**Option 1 is selected** (2026-07-05). The engine keeps ownership of
resolve/rank/budget; the product supplies the scope per lens. Options 2 and 3 are
retained below as the considered-and-rejected alternatives (Option 2 may still ship
*alongside* as a cheap in-engine convenience — open question #3).

### Option 1 — `Scope` on `fusion.Request`, plumbed to the embedding search *(SELECTED)*

Add an optional scope (an entity-ID glob/prefix, e.g. `*.web.*.doc.*`, matching the
6-part federated entity ID) to `fusion.Request`, threaded to the vector scan so a
lens instance constrains seeds to its domain at the source.

- **Correctness:** filters at the candidate source, so the small domain is never
  crowded out of the ranked window — the only option that reliably fixes *severe*
  dilution (the httpx case, where docs fell entirely below `resolveLimit = 40`).
- **Cost — the reason this is an ADR:** the scope must be threaded through every hop,
  two of which are **cross-component NATS RPC contracts** consumed by semsource:
  1. `fusion.Request` (`contract.go:36`) — add `Scope`
  2. `RetrievalClient.Resolve` signature (`retrieval.go:22`) — ripples to all impls + fakes
  3. `fusionnats.Client.resolveSemantic` body (`client.go:131`)
  4. `graph.query.semantic` handler passthrough (`processor/graph-query/query.go:562`)
  5. **`SearchRequest`** (`processor/graph-embedding/query.go:65`) — add scope field **(RPC contract change)**
  6. `findSimilarEntities` scan loop (`query.go:248`) — apply the filter
- **Backward-compat:** the field is optional; empty = no filter = today's behavior.
  Additive to the RPC contract, but it is still a shared wire-shape change — hence
  this ADR.

### Option 2 — `Lens.SeedFilter(entity) bool` + engine over-fetch *(self-contained, cheaper, weaker)*

Add an optional post-retrieval predicate the engine applies over an over-fetched
candidate set (resolve `N × limit`, filter, trim).

- **Cost:** self-contained in `pkg/fusion` — **no RPC contract change, no ADR** if
  chosen alone.
- **Weakness:** post-filtering cannot recover a domain that falls *entirely* below
  the over-fetch window. In the httpx case the docs were out-ranked at limit 40; a
  post-filter would need a large, wasteful over-fetch and still risks returning too
  few. Directionally helpful for mild dilution, unreliable for severe.

### Option 3 — per-lens embedding namespace *(deferred)*

A separate searchable embedding space per domain. Heaviest (index/topology change);
deferred unless Options 1/2 prove insufficient.

### Recommendation

**Option 1**, with the scope as an optional entity-ID glob/prefix applied in
`findSimilarEntities` (`query.go:248`), because it is the only option that fixes the
severe-dilution case semsource actually hit, and it keeps engine ownership intact
(the product supplies the scope per lens). Option 2 could ship *additionally* as a
cheap in-engine convenience, but is not sufficient on its own.

## Consequences

- The `graph.embedding.query.search` (and passthrough `graph.query.semantic`) RPC
  request gains an optional scope field — additive and backward-compatible, but a
  cross-component contract shape semsource depends on (the reason for this ADR).
- `RetrievalClient.Resolve` signature changes; all impls and test fakes (`engine_lens_test.go`
  `fakeGraph.Resolve`, `fusionnats.Client`) update.
- Mechanics (field names, glob semantics, scan-loop filter, over-fetch interplay) are
  specified in the `fusion` + `graph-embedding` capability specs via the follow-on
  openspec change — NOT in this ADR.
- Product half (choosing/wiring the scope per lens) is semsource's, once the hook lands.

## Open questions (resolve before Accept)

1. **Scope shape:** entity-ID glob (`*.web.*.doc.*`) vs bare prefix vs an
   entity-type/domain enum? The 6-part ID favors a glob/prefix; confirm the scan-loop
   match is cheap at index scale.
2. **Reuse the deterministic prefix path?** `ResolveModePrefix` → `graph.query.prefix`
   already carries a `Prefix`. Should NL scope reuse that field name/semantics for
   consistency, or is a distinct `Scope` clearer (NL scope filters *candidates*, the
   prefix mode *is* the query)?
3. **Ship Option 2 alongside** as a cheap in-engine `SeedFilter` for mild dilution,
   or Option 1 only?
4. **Pre-Accept adversarial review** (framework-ADR discipline): verify the scan-loop
   filter cost, that empty-scope is a true no-op on every hop, and that no other
   `Resolve` caller breaks on the signature change.
