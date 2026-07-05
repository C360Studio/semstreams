# Tasks — domain-scoped NL retrieval for a fusion Lens (gh#463, ADR-071)

> Scoping change (Proposed). Tasks unchecked; implementation follows scope approval
> (`/opsx:apply` on a `feat/` branch). Ordering note: land AFTER gh#475 (which seeds
> the `fusion` capability spec) so this change's `fusion` delta ADDs cleanly.

## 1. Shared prefix matcher

- [ ] 1.1 Add `graph.MatchesAnyIDPrefix(id string, prefixes []string) bool`: empty/nil
      prefixes → true (no filter); else true iff `id == p` or `strings.HasPrefix(id, p+".")`
      for some `p` (dot-boundary so `c360.semspec.source.doc` doesn't match
      `c360.semspec.source.docker...`). Table tests incl. empty, exact, boundary,
      multi-prefix OR.
- [ ] 1.2 Confirm the `graph.PrefixQueryRequest` dot-prefix convention
      (`graph/query_prefix_types.go`) and reuse/align, so the prefix query and scope
      filter share one matcher.

## 2. fusion contract + resolve threading

- [ ] 2.1 Add `Scope []string` to `fusion.Request` (`contract.go:36`), `json:"scope,omitempty"`.
- [ ] 2.2 Change `RetrievalClient.Resolve` to a struct param:
      `Resolve(ctx, ResolveQuery{Query string, Mode ResolveMode, Scope []string, Limit int}) ([]string, error)`.
      Update `Engine.Fuse` (`engine_lens.go:88`) to pass `req.Scope`.
- [ ] 2.3 Update ALL implementers/fakes/call sites (compile break — enumerate up front,
      do NOT whack-a-mole): prod `fusionnats.Client.Resolve`; in-repo fake
      `engine_lens_test.go fakeGraph.Resolve` + call sites `client_test.go`
      L119/145/166/180, `client_integration_test.go` L87/93/99; **cross-repo**
      `../semsource/source/fusion/fusiontest/memgraph.go:73 MemGraph.Resolve` (coordinate
      on the framework tag — this is why it is a struct param, not positional).

## 3. fusionnats — carry scope on the NL request

- [ ] 3.1 `resolveSemantic` (`fusionnats/client.go:130`): insert `"scope"` into the
      request body **only when non-empty** (byte-parity for the unscoped case). Symbol
      and prefix resolve paths carry NO scope (NL-only).

## 4. graph-embedding — the source filter (BOTH paths)

- [ ] 4.1 Add `Scope []string` to `SearchRequest` (`processor/graph-embedding/query.go:65`),
      `json:"scope,omitempty"`; decode stays `json.Unmarshal` (unknown-field-tolerant →
      un-migrated server degrades to unscoped).
- [ ] 4.2 **BLOCKING correctness:** apply the scope in BOTH `findSimilarEntities` paths
      — `FindSimilarFromCache` (`graph/embedding/storage.go:451`, filter before
      `CosineSimilarity`) AND the KV-scan fallback (`query.go:248`, filter the candidate
      ID set before the per-candidate `GetEmbedding`). One shared code path calling
      `graph.MatchesAnyIDPrefix`. A cache-path miss = silent warm-production no-op.
- [ ] 4.3 Perf check: scope filter runs before the expensive op on both paths (cosine /
      KV round-trip); on the httpx case a docs scope turns ~1334 KV reads into ~30.

## 5. graph-query — converge the overlapping filter

- [ ] 5.1 Route `graphrag.handleStrategySemantic`'s semantic path through the source-level
      `Scope` (pass to `SearchRequest.Scope`) instead of / in addition to the
      post-retrieval `filterEntityIDsByType`. Resolve the axis wrinkle: `Scope` =
      leading-prefix (domain/system), `filterEntityIDsByType` = type segment (position 5).
      Either re-express the type filter via the shared helper or keep it as a documented,
      layered-distinct axis — but eliminate any second post-retrieval ID filter that
      silently duplicates source-level scope. Reviewer confirms "one responsibility."

## 6. Tests

- [ ] 6.1 Unit: `MatchesAnyIDPrefix` table (task 1.1).
- [ ] 6.2 Production-decoder round-trip: a `SearchRequest` with `Scope` JSON-decodes in
      graph-embedding and the filter is applied (close the warning-not-fail gap;
      `feedback_production_decoder_round_trip_required`).
- [ ] 6.3 Both-path coverage: a scoped search over a mixed corpus returns only in-scope
      entities via BOTH the warm cache and the cold KV-scan (drive each path explicitly;
      warm-cache omission is the BLOCKING regression to guard).
- [ ] 6.4 fusion engine: `Fuse` with `req.Scope` set threads scope to `Resolve`; empty
      scope is a byte-identical no-op (assert the unscoped request body is unchanged).
- [ ] 6.5 Integration (if feasible): `fusionnats.Client → graph-query → graph-embedding`
      with a scoped NL query over a code+docs corpus reproduces the httpx fix (docs no
      longer drowned). Closes the flagged `pkg/fusion` e2e coverage gap.
- [ ] 6.6 Backward-compat: every existing caller of `graph.embedding.query.search` /
      `RetrievalClient.Resolve` with no scope behaves identically.

## 7. Spec + gates + close

- [ ] 7.1 `openspec validate --strict`.
- [ ] 7.2 Gates: `go test -race` (fusion, graph-embedding, graph-query), `task lint`,
      schema no-drift (SearchRequest is an RPC type — confirm no generated-schema drift),
      `go vet -tags=integration`.
- [ ] 7.3 semstreams-reviewer pre-merge (RPC error-contract on the scoped search; the
      BOTH-paths filter; the cross-repo `Resolve` blast radius incl. semsource; the
      convergence really is one responsibility; empty-scope byte-parity).
- [ ] 7.4 Archive → promote `graph-embedding` + `graph-query` into `openspec/specs/`;
      ADD to `fusion`.
- [ ] 7.5 Breaking-change check: `RetrievalClient.Resolve` signature change is a
      cross-repo break (semsource `MemGraph.Resolve`) — coordinate the semsource bump on
      the framework tag (see [[feedback_e2e_required_for_breaking_changes]] /
      [[feedback_greenfield_cross_product_break_now]]). PR; CI; merge; tag.
- [ ] 7.6 Confirm back to semsource on gh#463 / upstream-asks #16 (code + docs lenses
      set their `Scope` prefixes; docs no longer diluted). File the semsource
      `MemGraph.Resolve` bump.
