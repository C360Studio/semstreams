# Tasks — fusion-graph-projection-facet

## 1. Contract + engine

- [x] 1.1 `WantGraph` + `GraphProjection`/`GraphNode`/`GraphFact`/`GraphEdge`/`GraphEvidence`/
  `ViewRevision` types per design D2 (verbatim predicates, `json.RawMessage` values,
  pointer-confidence, omitempty discipline)
- [x] 1.2 Engine facet build: facts from seed triples (declaration-driven split per D3 — no
  `Triple.IsRelationship`); edges from the relations walk reusing Neighbors + counterpart
  fetches; evidence per D4 (outgoing from seed triples, incoming from counterpart triples,
  same-(s,p,t) merge into one edge with multiple evidence entries)
- [x] 1.3 Caps + truncation metadata per D5 (independent of v1 budgeter and role cap)
- [x] 1.4 ViewRevision sampling per D6 (pre-resolve reuse + post-fetch re-sample; failed
  re-sample → End=0, Coherent=false)

## 2. Tests (map gh#533 acceptance 1–8)

- [x] 2.1 ID-shaped literal with empty datatype and undeclared predicate stays a fact (1)
- [x] 2.2 Parallel predicates → two edges (2); opposite directions → distinct edges with
  swapped source/target (3)
- [x] 2.3 Two evidence contributions on one semantic edge stay inspectable (4); absent
  confidence/context omitted, never zero-filled (5)
- [x] 2.4 Fact truncation observable + independent of v1 truncation (6)
- [x] 2.5 ViewRevision coherent/spanning both ways incl. failed re-sample (7)
- [x] 2.6 v1 request without the want → no graph field, v1 shape byte-identical (8);
  `@id`-datatyped undeclared-predicate triple projects as edge with handle-only target
- [x] 2.7 fusionnats pass-through verification (response field transports unmodified —
  verified fusionnats never (de)serializes `fusion.Response` (it is the RetrievalClient);
  Response-JSON round-trip test proves the facet survives opaque transport)

## 3. Gates + close-out

- [x] 3.1 `go test -race ./pkg/fusion/...` unit + integration; vet; gofmt; schema no-drift;
  `task lint`
- [x] 3.2 semstreams-reviewer pre-merge pass
- [x] 3.3 gh#533 reply: shape summary + acceptance mapping; rides the next tag
