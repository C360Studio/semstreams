# Tasks — domain-scoped NL retrieval for a fusion Lens (gh#463, ADR-071)

> Scoping change (Proposed). Tasks unchecked; implementation follows scope approval
> (`/opsx:apply` on a `feat/` branch). Ordering note: land AFTER gh#475 (which seeds
> the `fusion` capability spec) so this change's `fusion` delta ADDs cleanly.

## 1. Shared prefix matcher

- [x] 1.1 Add `graph.MatchesAnyIDPrefix(id string, prefixes []string) bool`: empty/nil
      prefixes → true (no filter); else true iff `id == p` or `strings.HasPrefix(id, p+".")`
      for some `p` (dot-boundary so `c360.semspec.source.doc` doesn't match
      `c360.semspec.source.docker...`). Table tests incl. empty, exact, boundary,
      multi-prefix OR.
- [x] 1.2 Confirm the `graph.PrefixQueryRequest` dot-prefix convention
      (`graph/query_prefix_types.go`) and reuse/align, so the prefix query and scope
      filter share one matcher.

## 2. fusion contract + resolve threading

- [x] 2.1 Add `Scope []string` to `fusion.Request` (`contract.go:36`), `json:"scope,omitempty"`.
- [x] 2.2 Change `RetrievalClient.Resolve` to a struct param:
      `Resolve(ctx, ResolveQuery{Query string, Mode ResolveMode, Scope []string, Limit int}) ([]string, error)`.
      Update `Engine.Fuse` (`engine_lens.go:88`) to pass `req.Scope`.
- [x] 2.3 Update ALL implementers/fakes/call sites (compile break — enumerate up front,
      do NOT whack-a-mole): prod `fusionnats.Client.Resolve`; in-repo fake
      `engine_lens_test.go fakeGraph.Resolve` + call sites `client_test.go`
      L119/145/166/180, `client_integration_test.go` L87/93/99; **cross-repo**
      `../semsource/source/fusion/fusiontest/memgraph.go:73 MemGraph.Resolve` (coordinate
      on the framework tag — this is why it is a struct param, not positional).
      DONE for all in-repo sites; the semsource `MemGraph.Resolve` bump is deferred to
      the framework tag (task 7.5) — semsource pins semstreams by tag (beta.132), so
      editing it now would break its build against the current tag.

## 3. fusionnats — carry scope on the NL request

- [x] 3.1 `resolveSemantic` (`fusionnats/client.go:130`): insert `"scope"` into the
      request body **only when non-empty** (byte-parity for the unscoped case). Symbol
      and prefix resolve paths carry NO scope (NL-only).

## 4. graph-embedding — the source filter (BOTH paths)

- [x] 4.1 Add `Scope []string` to `SearchRequest` (`processor/graph-embedding/query.go:65`),
      `json:"scope,omitempty"`; decode stays `json.Unmarshal` (unknown-field-tolerant →
      un-migrated server degrades to unscoped).
- [x] 4.2 **BLOCKING correctness:** apply the scope in BOTH `findSimilarEntities` paths
      — `FindSimilarFromCache` (`graph/embedding/storage.go:451`, filter before
      `CosineSimilarity`) AND the KV-scan fallback (`query.go:248`, filter the candidate
      ID set before the per-candidate `GetEmbedding`). One shared code path calling
      `graph.MatchesAnyIDPrefix`. A cache-path miss = silent warm-production no-op.
- [x] 4.3 Perf check: scope filter runs before the expensive op on both paths (cosine /
      KV round-trip); on the httpx case a docs scope turns ~1334 KV reads into ~30.

## 5. graph-query — converge the overlapping filter

- [x] 5.1 Converge the overlapping filter on the semantic path to ONE ID-scoping
      responsibility. Axis wrinkle resolved: `Scope` = leading-prefix (domain/system);
      `filterEntityIDsByType` = type segment (position 5, exact equality). These are
      genuinely distinct axes — routing the type filter through `MatchesAnyIDPrefix`
      (leading prefix) would be a BUG (a type `drone` is not a leading prefix `drone.`),
      so `filterEntityIDsByType` is KEPT and now carries a doc comment naming it the
      distinct type-segment axis (`graphrag.go:1237`). No second post-retrieval
      ID-prefix filter is added. The actual gh#463 fusion NL path is
      `fusionnats.resolveSemantic → graph.query.semantic → handleQuerySemantic` (raw
      passthrough), so leading-prefix `Scope` reaches `SearchRequest.Scope` at the
      source WITHOUT touching `handleStrategySemantic`; the GraphRAG global-search path
      (`handleStrategySemantic`) has no leading-prefix scope input today (none is asked
      for) — no dead nil-plumbing added. semstreams-reviewer confirmed "one
      responsibility," spec-conformant (not an under-implementation).

## 6. Tests

- [x] 6.1 Unit: `MatchesAnyIDPrefix` table (task 1.1).
- [x] 6.2 Production-decoder round-trip: a `SearchRequest` with `Scope` JSON-decodes in
      graph-embedding and the filter is applied (close the warning-not-fail gap;
      `feedback_production_decoder_round_trip_required`).
- [x] 6.3 Both-path coverage: a scoped search over a mixed corpus returns only in-scope
      entities via BOTH the warm cache and the cold KV-scan (drive each path explicitly;
      warm-cache omission is the BLOCKING regression to guard).
- [x] 6.4 fusion engine: `Fuse` with `req.Scope` set threads scope to `Resolve`; empty
      scope is a byte-identical no-op (assert the unscoped request body is unchanged).
- [x] 6.5 Integration (if feasible): `fusionnats.Client → graph-query → graph-embedding`
      with a scoped NL query over a code+docs corpus reproduces the httpx fix (docs no
      longer drowned). Closes the flagged `pkg/fusion` e2e coverage gap.
      DONE at the graph-embedding integration level (`scope_integration_test.go`, in-process
      NATS KV): drives BOTH real similarity paths over a mixed code+docs corpus and
      reproduces the dilution shape (unscoped = whole corpus; scoped = docs-only). The full
      `fusionnats → graph-query → graph-embedding` chain over live NATS remains an e2e-tier
      gap (no fusion e2e tier exists — the ADR flagged this); the source filter that fixes
      the dilution is exercised end-to-end through the real handler code here.
- [x] 6.6 Backward-compat: every existing caller of `graph.embedding.query.search` /
      `RetrievalClient.Resolve` with no scope behaves identically.

## 7. Spec + gates + close

- [x] 7.1 `openspec validate --strict`.
- [x] 7.2 Gates: `go test -race` (fusion, graph-embedding, graph-query), `task lint`,
      schema no-drift (SearchRequest is an RPC type — confirm no generated-schema drift),
      `go vet -tags=integration`.
- [x] 7.3 semstreams-reviewer pre-merge (RPC error-contract on the scoped search; the
      BOTH-paths filter; the cross-repo `Resolve` blast radius incl. semsource; the
      convergence really is one responsibility; empty-scope byte-parity).
- [x] 7.4 Archive → promote `graph-embedding` + `graph-query` into `openspec/specs/`;
      ADD to `fusion`.
- [ ] 7.5 Breaking-change check: `RetrievalClient.Resolve` signature change is a
      cross-repo break (semsource `MemGraph.Resolve`) — coordinate the semsource bump on
      the framework tag (see [[feedback_e2e_required_for_breaking_changes]] /
      [[feedback_greenfield_cross_product_break_now]]). PR; CI; merge; tag.
- [ ] 7.6 Confirm back to semsource on gh#463 / upstream-asks #16 (code + docs lenses
      set their `Scope` prefixes; docs no longer diluted). File the semsource
      `MemGraph.Resolve` bump.
