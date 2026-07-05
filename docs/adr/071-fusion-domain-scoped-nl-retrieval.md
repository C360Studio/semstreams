# ADR-071: Domain-scoped NL retrieval for a fusion Lens (gh#463)

## Status

**Accepted — 2026-07-05.** Mechanism: **Option 1** — a `Scope []string` on
`fusion.Request` plumbed to the embedding search — recorded as an ADR because it
changes a **cross-component NATS RPC contract** (`graph.embedding.query.search`), the
class of decision an ADR exists to hold. A code-grounded adversarial review
(framework-ADR discipline — `feedback_adversarial_review_framework_adr`) found a
**BLOCKING** shape defect (a warm-cache no-op) and two HIGHs (scope must be a list; a
cross-repo compile break), all folded into the Decision/Consequences/Resolved-decisions
below before Accept. **Convergence decided:** the follow-on change unifies the existing
`graphrag.filterEntityIDsByType` post-filter with the new scope onto one shared
`graph.MatchesAnyIDPrefix` helper (question c / MEDIUM). The mechanics now move into an
openspec change against the `fusion` + `graph-embedding` + `graph-query` capability
specs; implementation follows.

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

Add an optional **`Scope []string`** (a list of dot-delimited entity-ID **prefixes**,
OR-matched; empty/nil = no filter) to `fusion.Request`, threaded to the vector scan so
a lens instance constrains seeds to its domain **at the candidate source**.

- **Why a list, not a scalar (pre-Accept review, HIGH):** the code-vs-docs
  discriminator lives in the ID's domain/system segments (positions 3–4 of
  `org.platform.domain.system.type.instance`), and each side spans **multiple**
  prefixes — semsource "all code" = `golang`/`python`/`ts`/`svelte`, "all docs" =
  `source.doc` + `source.chunk`. A single glob/prefix cannot express either side. (The
  earlier draft's `*.web.*.doc.*` example was fictional — no real ID has both `web` and
  `doc` segments.) Prefix, not glob: `strings.HasPrefix(id, p+".")` is cheapest and
  avoids `path.Match`'s dotted-delimiter ambiguity (`*` would span `.`).
- **Correctness:** filters at the candidate source, so the small domain is never
  crowded out of the ranked window — the only option that reliably fixes *severe*
  dilution (the httpx case, where docs fell entirely below `resolveLimit = 40`).
- **Cost — the reason this is an ADR:** the scope is threaded through these hops (one
  of the six the earlier draft listed is a free passthrough — LOW):
  1. `fusion.Request` (`contract.go:36`) — add `Scope []string`
  2. `RetrievalClient.Resolve` (`retrieval.go:22`) — **use a struct param**
     (`Resolve(ctx, ResolveQuery{Query, Mode, Scope, Limit})`) rather than a positional
     add: `Resolve` is shared across symbol/prefix/nl, scope is NL-only, and a
     positional arg forces every mode's callers (incl. the cross-repo fake) to pass an
     ignored value and re-breaks on the next dimension.
  3. `fusionnats.Client.resolveSemantic` body (`client.go:131`) — insert `"scope"`
     **only when non-empty** (byte-parity for the unscoped case).
  4. `graph.query.semantic` handler — **no code change**: `handleQuerySemantic`
     (`processor/graph-query/query.go:569`) forwards raw `[]byte` via
     `RequestClassified`, so a new JSON field rides through untouched.
  5. **`SearchRequest`** (`processor/graph-embedding/query.go:65`) — add `Scope []string`
     **(the cross-component RPC contract change)**.
  6. Apply the filter in **both** `findSimilarEntities` paths (see BLOCKING below).
- **Backward-compat:** the field is optional; empty = no filter = today's behavior.
  graph-embedding uses plain `json.Unmarshal` (no `DisallowUnknownFields`), so an
  un-migrated server silently ignores `scope` (graceful degrade to unscoped), and every
  other caller of the subject sends none. Additive, but a shared wire-shape change —
  hence this ADR.

> **BLOCKING (pre-Accept review) — the filter must live in the WARM path, not only the
> scan fallback.** `findSimilarEntities` (`processor/graph-embedding/query.go:221`)
> serves from an in-memory cache via `FindSimilarFromCache` (`graph/embedding/storage.go:439`,
> loop `:451`) **first**, and only hits the KV-scan loop (`query.go:248`) when the cache
> is cold. Applying the filter only at `query.go:248` (as the earlier draft said) makes
> it a **silent no-op for essentially every real query** in warm production. The scope
> filter MUST be applied in **both** paths via one shared helper, filtering **before**
> `CosineSimilarity` (cache) / before the per-candidate `GetEmbedding` KV round-trip
> (scan) — on the httpx case a docs scope turns ~1334 KV round-trips into ~30.

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

**Option 1**, with `Scope []string` (OR-matched ID prefixes) applied at the candidate
source in **both** `findSimilarEntities` paths, because it is the only option that
fixes the severe-dilution case semsource actually hit, and it keeps engine ownership
intact (the product supplies the scope per lens). Option 2 is **not** shipped alongside
(see resolved question c).

## Consequences

- The `graph.embedding.query.search` (`SearchRequest`) — and the raw-passthrough
  `graph.query.semantic` — request gains an optional `Scope []string` — additive,
  backward-compatible (unknown-field-tolerant decode), but a cross-component contract
  shape semsource depends on (the reason for this ADR).
- **`RetrievalClient.Resolve` change is a cross-repo compile break — full blast radius:**
  the sole production impl `fusionnats.Client`; the in-repo fake `engine_lens_test.go`
  `fakeGraph.Resolve` + 7 call sites (`client_test.go` L119/145/166/180,
  `client_integration_test.go` L87/93/99); and — omitted from the earlier draft —
  the **cross-repo** `../semsource/source/fusion/fusiontest/memgraph.go:73`
  `MemGraph.Resolve`. (`engine_test.go`'s `fakeGraphQuery` implements the *different*
  `GraphQueryClient` interface — unaffected.) The struct-param shape (question a)
  contains this: adding a field to `ResolveQuery` later won't re-break these.
- **Convergence decision (review MEDIUM):** an Option-2-shaped post-filter already
  exists — `graph-query/graphrag.go` `handleStrategySemantic` calls
  `filterEntityIDsByType` (`graphrag.go:907/1237`) **after** the embedding search. The
  follow-on openspec change MUST decide to converge on one scope filter (reuse its
  `[]string` + empty=no-op convention, and extract a shared
  `graph.MatchesAnyIDPrefix(id, []string)` helper) rather than ship a second overlapping
  filter on the same semantic path — "name the responsibility once."
- **Coverage gap (review):** no e2e tier exercises `pkg/fusion` / the semantic NL path;
  the openspec change adds a production-decoder round-trip test (`SearchRequest`-with-scope
  → graph-embedding decode → filter applied) and ideally an integration test through
  `fusionnats.Client → graph-query → graph-embedding`.
- The `did_you_mean` path (`e.miss` → `Names` → `graph.query.byName`) stays **unscoped**
  (LOW): a scoped docs-lens miss can suggest code names. Decide in the openspec change
  whether to scope `Names` too or document the cross-domain suggestion.
- Mechanics (field names, prefix semantics, both-path helper, decode tests) are specified
  in the `fusion` + `graph-embedding` capability specs via the follow-on openspec
  change — NOT in this ADR.
- Product half (choosing/wiring the scope per lens) is semsource's, once the hook lands.

## Resolved decisions (pre-Accept adversarial review, 2026-07-05)

An architect adversarial review verified every line citation, confirmed Option 1's
mechanism is sound, and resolved the four open questions (defects folded above):

a. **Scope shape → `Scope []string` of dot-delimited ID *prefixes*, OR-matched, empty =
   no filter.** Prefix (not glob — cheapest, no `path.Match` dotted-delimiter ambiguity);
   list (not scalar — code/docs each span multiple prefixes, HIGH-2); match on the
   domain/system-segment prefix (not the type segment — the discriminator isn't there).
   Reuse the `graph.PrefixQueryRequest` dot-prefix convention + `filterEntityIDsByType`'s
   `[]string`/empty=no-op convention via a shared matcher helper.
b. **Reuse the prefix path? → Reuse the prefix *matching semantics*, but as a DISTINCT
   `Scope` field, not the `Prefix` field.** `ResolveModePrefix`'s `Prefix` *is* a
   deterministic query; NL `Scope` is a *filter on embedding candidates* (types differ:
   list vs scalar). Overloading one field conflates "what I search for" with "the subset
   I search within." Share only the matcher.
c. **Ship Option 2 alongside? → No, Option 1 only.** Once Option 1 filters at the source,
   a `Lens.SeedFilter` + over-fetch is a strictly-weaker duplicate, and two filter
   surfaces is a silent-weak-scoping footgun. Option 2's shape already exists
   (`graphrag.filterEntityIDsByType`, post-retrieval) and is precisely what fails severe
   dilution. Add a non-prefix filter later only if a real need arises (e.g. on a triple
   value), scoped to that need.
d. **Also folded:** the BLOCKING warm-cache path, the `[]string` requirement, the
   cross-repo `MemGraph.Resolve` blast radius, the struct-param `Resolve` shape, the
   graphrag convergence decision, unscoped `did_you_mean`, and the e2e coverage gap.

**Remaining to Accept:** user sign-off on this folded shape; then the follow-on openspec
change against the `fusion` + `graph-embedding` specs pins the mechanics and
implementation follows.
