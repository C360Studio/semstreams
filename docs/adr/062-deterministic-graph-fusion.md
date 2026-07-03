# ADR-062: Deterministic graph fusion — extract & unify the engine

## Status

**Accepted — 2026-06-29.** Scopes gh#376 (semsource deterministic-fusion ask). Co-authored
with semsource, whose reference implementation (`source/fusion/`, their ADR-0004) validated the
approach across two domain lenses. The first increment — `graph.query.byName` (resolve-side
name→ranked-IDs, gh#376 ask #5) — shipped in #380.

## Decision

Extract the deterministic fusion engine that **already exists in embryonic form** inside
`processor/research-graph-execute` (the `executeAll` pure function + `GraphQueryClient` interface +
evidence types) into a standalone **`pkg/fusion`**, framed as the **deterministic sibling of
`research_graph`** (ADR-045): same `resolve → expand → hydrate → assemble` shape, no LLM in the path.

`execute_subqueries` becomes a thin adapter over `pkg/fusion`; semsource converges `source/fusion`
onto the same package rather than maintaining a parallel surface. The engine is parameterized by a
product-supplied **Lens SPI** (the only domain-specific surface), registered via the explicit
factory-aggregation pattern (a `lensregistry.Register`), **not** the `init()`-global vocabulary
pattern. Verbatim content is hydrated through a **`Lens.Hydrate`-returns-a-handle,
engine-dereferences-via-the-backend-agnostic-`storage.Store`-interface** contract — no lens reads the
filesystem and **no concrete store is baked in** (NATS ObjectStore, a filestore, or S3 all plug in via
the handle's `StorageInstance`) — making fusion deployment-independent (headless/remote).

This is an **extraction and unification of three near-identical pipelines** (semsource's code lens,
semsource's docs lens, our `research-graph-execute`), not a new sibling primitive.

## Context

**The problem.** Agents handed *triples by 6-part entity ID* tend not to use the graph — they bail to
`grep`. The data is already there (semstreams parses, embeds, relates), but the **exposure** is wrong:
learn IDs per call, compose a query, hop N times, then re-read the file anyway. The fix is a **fused
response shape + content hydration**, assembled server-side, with entity IDs demoted to opaque handles
and an honest readiness/provenance envelope. The need is not code-specific — it recurs for docs, specs,
and any domain with content + structure.

**`research_graph` already offloads the reasoning half.** ADR-045's `research-graph-execute` is a
pure-code (no-LLM) component that does materialize → resolve → multi-tier expand → dedup/rank/budget →
provenance-stamp over a `GraphQueryClient` interface (NATS isolated in `adapters.go`), emitting a
registered `ExecutionOutput{Evidence[]}` payload that the LLM stages (assess/synthesize) consume across
a clean KV seam. So *"fusion is the substrate research_graph reasons over"* is **literally true in our
code today** — the LLM stages route *through* this deterministic layer, never driving graph tools
directly. That is exactly the missing primitive, buried inside one chain-specific component and typed to
a Phase-1-minimal, deliberately-internal sub-query set.

**semsource proved generality.** Their `fusion.Engine` (≈1,100 LOC incl. tests) runs the same pipeline
behind a 6-method `GraphQueryClient` and a 7-method `Lens` SPI; the engine code is byte-identical across
their **code** and **docs** lenses (the docs lens declares zero edges and the engine degrades
generically). Rule-of-two, validated — and with our `research-graph-execute`, rule-of-three.

## The engine (`pkg/fusion`)

Lift, near-verbatim, from `research-graph-execute`:

- **`executeAll(ctx, GraphQueryClient, queries, intent, …) → *ExecutionOutput`** — the pure
  orchestration function (no NATS), already factored for exactly this (its doc: "no NATS plumbing leaks
  here so the test matrix can exercise the orchestration logic with deterministic inputs").
- **`GraphQueryClient`** — the interface the engine needs. Production impl wraps `graph.query.*` /
  `graph.index.query.*` via `RequestClassified` (ADR-060); tests inject a fake. **Today's
  `research-graph-execute` client is 4 methods** (`EntityState`→`graph.query.batch`,
  `PredicateWalk`→`graph.query.relationships`, `TemporalRange`→`graph.query.temporal`,
  `BM25`→`graph.query.searchGraph`). The *target* `pkg/fusion` surface widens it toward semsource's
  6-method client — adding readiness (`status`, to build), resolve (prefix / **byName** / semantic), and
  incoming/`pathSearch` expand. This widening is part of the extraction, not existing substrate.
- **Evidence types** — `Evidence{Tier, Source, EntityID, Score, SnippetText, ObjectStoreRef, …}`
  (`agentic/research/result.go`), the fused node shape. semsource's richer `Node{Name, Kind, Path, Body,
  Relations, Class, Handle}` and `Paths` / `Impact` / `Misses` facets are merged in where they generalize.

The generalization work left is exactly ADR-045's Phase-2 list: Tier-2 neural, cross-tier score
normalization, multi-source-per-entity provenance — not net-new substrate.

## The Lens SPI

The lens is the **only** domain-specific surface. From semsource's validated interface (7 methods):

```go
type Lens interface {
    Name() string
    ResolveMode(query string) ResolveMode      // nl | symbol | prefix
    Edges() []EdgeSpec                          // predicate sets + direction to walk
    Label(e *Entity) string
    Kind(e *Entity) string
    Location(e *Entity) Locator
    Hydrate(ctx, e *Entity) (Handle, error)     // see hydration contract
}
```

**Registration follows the component/payload factory-aggregation pattern** — a `lensregistry.Register`
called from an explicit bootstrap aggregator — **not** the vocabulary `init()`-global pattern. Rationale:
lenses are factories that need deps/config like components and payloads, not stateless metadata like
predicates; and the vocabulary registry's last-write-wins override semantics would be exactly wrong for
lenses (you want explicit per-product registration, not silent override). Products supply a lens; the
framework owns resolve/expand/rank/budget/envelope.

## The hydration contract (the crux — resolved by convergence)

gh#376 flagged verbatim-body hydration as *the* gating decision. The key constraint, surfaced in review:
**the fusion engine must NOT be coded to a concrete store.** The framework already has the right seam — a
backend-agnostic **`storage.Store` interface** (`storage/storage.go`: `Put`/`Get([]byte)`/`List`/`Delete`,
its own doc distinguishing "immutable stores (NATS ObjectStore)" from "mutable stores (S3, SQL)") — and
**`StorageReference` already carries a `StorageInstance` backend selector** (`message/storable.go`:
"identifies which storage component holds the data … enables federation across multiple storage
instances"). NATS ObjectStore is **one** `storage.Store` implementation, and a poor fit for large
binaries; semsource already runs a **filestore** for media, having deliberately migrated *off* objectstore
(`../semsource/.../run.go`: "binaries (media) deliberately stay on the local filestore, not here"). The
contract must preserve that freedom.

**Contract:** `Lens.Hydrate` returns a **handle (`StorageReference`)**. The **engine** resolves the
handle's `StorageInstance` to a registered `storage.Store` and reads the body with **`Get(key) ([]byte,
error)`** — byte-exact by construction (`[]byte` in, `[]byte` out), no envelope or role gymnastics, no
filesystem in the lens. The producer chooses the backend appropriate to the content: NATS ObjectStore for
small/immutable text, a filestore / S3 / blob store for large binaries. The fusion contract is **the
`storage.Store` interface + the `StorageReference` handle — never a concrete backend.** semsource's code
lens's `os.ReadFile(root, relPath)` collapses to "return the entity's StorageRef," and their filestore
plugs in as the `StorageInstance` it resolves to.

What the framework owns: a small **handle-resolution helper** — given a `StorageReference`, look up the
`storage.Store` for its `StorageInstance` and `Get` the bytes (the existing `graph/llm.ContentFetcher`
interface, "implementations may use NATS request/reply, direct store access, or mocks," is the natural
shape to generalize). What it does NOT need: routing verbatim bodies through the text `StoredContent`
envelope — `StoredContent.Fields` is `map[string]string` through `json.Marshal`, so non-UTF-8 corrupts to
U+FFFD and the embedding worker already warns against it for body text (`worker.go:418-425`). Bodies ride
`storage.Store.Get`, not a content-role accessor.

The producer side (offload verbatim bodies + stamp the `StorageReference`) is semsource's parallel
ast-source/filestore work; the ref is lifted onto `EntityState` at ingest already (gh#264).

## Resolve / expand / hydrate substrate (status)

- **Resolve — adequate.** prefix (`graph.query.prefix`), suffix (`graph.ingest.query.suffix`),
  predicate(+value), alias (stable IDs), and **`graph.query.byName`** (deterministic name/title → ranked
  IDs; shipped #380, gh#376 ask #5, closes the gap where exact-name lookup silently fell back to semantic
  search). Semantic (`globalSearch`/`searchGraph`) covers NL.
- **Expand — complete.** `graph.index.query.{outgoing,incoming,predicate,predicateCompound}` + `pathSearch`.
- **Hydrate — complete.** `graph.query.batch` (bounded-concurrent, partial-success; chunk above ~1 MB).
- **Readiness — MISSING (to build).** The honesty envelope's `ready ≠ not-found` needs a queryable index-
  readiness surface; there is **no `graph.*.query.status` subject today** (the only `IndexStatus`-shaped
  type is `graph/datamanager`'s internal cache struct, not queryable). semsource's engine reads a
  `Status` today against a subject that does not yet exist framework-side — a readiness query is net-new
  work (folded into increment 2's `GraphQueryClient`).

## Honesty envelope

Carried on every response (from semsource's validated shape):

- **`ready ≠ not-found`** — an `IndexStatus{ready, state}`; when not ready the caller must fall back
  (e.g. to grep) rather than treat empty as absent. Only `ready` permits a not-found conclusion. (The
  backing index-readiness query is net-new — see Substrate › Readiness.)
- **`provenance ∈ {deterministic, embedding, llm}`** — `deterministic` for exact lookup + structural
  walk, `embedding` when seeds came from semantic search, `llm` reserved for the `research_graph` sibling.
  (The byName primitive is what lets a symbol resolution claim `deterministic` honestly instead of
  optimistically.)

## Increments

1. **`graph.query.byName` name index** — DONE (#380). Resolve-side determinism.
2. **Extract `pkg/fusion`** — `executeAll` + `GraphQueryClient` + evidence types; `execute_subqueries`
   becomes a thin adapter.
3. **Lens SPI + `lensregistry`** — the 7-method interface + factory-aggregation registration.
4. **Hydration contract** — the engine-side handle-resolution helper (`StorageReference` →
   `storage.Store` for its `StorageInstance` → `Get([]byte)`), backend-pluggable (NATS ObjectStore /
   filestore / S3); the verbatim-body producer (semsource ast-source/filestore, parallel).
5. **Ontology ranking inputs** — gh#376 sub-asks #1 (`vocabulary/bfo|cco` `Parents()`/`IsA()` subclass
   helper) and #2 (predicate salience: `WithRole`/`WithWeight` on `vocabulary.Register`).
6. **Convergence** — semsource ports `source/fusion` onto `pkg/fusion`; contributes the code + docs
   lenses (rule-of-three inputs) + ontology ranking.
7. **Signed salience (down-rank)** — gh#441. `WithWeight` accepts NEGATIVE weights; the ranker folds an
   entity's strongest boost (max positive) and strongest demotion (min negative) predicate weights
   together (`entitySalience`), so a consumer can push structurally-identifiable noise — tests
   (`*_test.go`), generated code (`*.pb.go`), mocks — BELOW the real thing even when it carries the
   SAME salient predicates (a test's doc-comment has the same salience as its impl's). Presence-predicate
   based (emit e.g. `code.artifact.test` and weight it negative); no new SPI surface (the existing signed
   `PredicateSalience` starts meaning what its sign says). Demotion is `max`+`min` over predicates, NOT a
   sum — fact count still cannot inflate rank — and stays a bounded secondary reordering, never an
   exclusion. All-positive configs (every config before this) are unchanged (min-negative stays 0 → old
   `max`).

## Division of labor

- **semstreams (us):** this ADR; `pkg/fusion` extraction; the Lens SPI + registry; the hydration
  contract (`body` role + engine dereference); the ontology helpers (#1/#2); the name index (done).
- **semsource (them):** the verbatim-body producer (ast-source/handlers → ObjectStore + StorageRef);
  converging `source/fusion`; the code + docs lenses; ontology ranking inputs.

## Alternatives considered

- **A new sibling primitive beside `research-graph-execute`.** Rejected — duplicates a third copy of the
  same pipeline; the unification (rule-of-three onto one package) is the stronger move.
- **Leave fusion buried in `research-graph-execute`.** Rejected — it is reusable substrate trapped behind
  a chain-specific, deliberately-internal sub-query type set; products can't consume it.
- **`Lens.Hydrate` reads the filesystem (semsource's current standalone mode).** Rejected — not
  headless/distributed; a remote deployment can't read another process's worktree. The handle +
  store-dereference contract is what makes fusion location-independent.
- **Couple hydration to NATS ObjectStore as the content substrate.** Rejected — NATS ObjectStore is a
  poor fit for large binaries, and a backend-agnostic `storage.Store` interface already exists with
  `StorageReference.StorageInstance` selecting the backend. semsource deliberately migrated media off
  objectstore to a filestore. The fusion contract binds to the `storage.Store` *interface* + the handle,
  so each deployment/content-type picks its own store (ObjectStore, filestore, S3) — fusion never
  assumes one.

## Consequences

- Unifies two parallel engines (ours + semsource's) onto one substrate; renderers stay per-product.
- `research_graph`'s deterministic retrieval half becomes a first-class, independently-consumable
  primitive — and `research_graph` keeps composing over it (fusion is the substrate it reasons over).
- Enables headless/distributed fusion (the contract that retires semsource's worktree coupling).
- New surface to build/maintain: `pkg/fusion`, the Lens SPI, `lensregistry`, an index-readiness query
  subject, and the engine-side handle-resolution helper over the `storage.Store` interface
  (backend-agnostic — does not couple fusion to any concrete store).

## References

- gh#376 (this ask); semsource ADR-0004 (deterministic fusion gateway), ADR-0005 (ontology-aware
  ranking), ADR-0006 (standalone-external-service).
- ADR-045 (`research_graph` chain) — the LLM sibling; ADR-046 (gated-DAG) — orchestration neighbor.
- ADR-055/056 (ContentStorable / authoritative state), gh#264 (StorageRef lift at ingest).
- #380 (`graph.query.byName`, increment 1); #381 (index value-growth follow-up).
- gh#376 sub-asks: #1 (BFO/CCO subclass helper), #2 (predicate salience), #5 (name→ranked-IDs, shipped).
- gh#441 (signed salience — down-rank tests/generated/mocks; semsource ask #14), increment 7.
