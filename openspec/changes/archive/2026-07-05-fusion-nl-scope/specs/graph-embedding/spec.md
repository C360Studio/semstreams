# Graph Embedding

The `graph.embedding.query.search` semantic-search RPC and its similarity scan.

> Seeded lazily by gh#463 (ADR-071). Scope is the **scoped semantic search** contract;
> other graph-embedding behavior is seeded when a change first touches it. Distilled
> from `processor/graph-embedding/query.go` + `graph/embedding/storage.go`, verified
> against code.

## ADDED Requirements

### Requirement: Semantic search MAY be scoped to entity-ID prefixes, applied at the source

The `graph.embedding.query.search` request (`SearchRequest`) MUST support an optional
`Scope` — a list of entity-ID prefixes. When non-empty, the search MUST return only
candidates whose entity ID matches at least one prefix (via the shared
`graph.MatchesAnyIDPrefix` matcher), and MUST apply that filter **at the candidate
source in every similarity path** — both the warm in-memory cache path
(`FindSimilarFromCache`) and the cold KV-scan fallback — filtering **before** the
expensive per-candidate operation (cosine similarity / the embedding KV fetch). An
empty/absent `Scope` MUST behave exactly as today. The request MUST decode with
unknown-field tolerance, so a producer sending `Scope` to an un-migrated server
degrades gracefully to an unscoped search rather than erroring.

Applying the filter to only one similarity path (e.g. the cold fallback) is
non-conformant: the warm cache is the steady-state path, so a cache-path omission
makes the scope a silent no-op in production.

#### Scenario: a scoped search returns only in-scope candidates (warm and cold)

- **GIVEN** an index holding entities under two ID prefixes
- **WHEN** `graph.embedding.query.search` runs with `Scope` naming one prefix
- **THEN** only entities under that prefix are returned
- **AND** this holds whether the similarity is served from the warm cache or the cold
      KV scan

#### Scenario: an unscoped search is unchanged

- **GIVEN** a `SearchRequest` with an empty/absent `Scope`
- **WHEN** it runs
- **THEN** results are identical to the pre-scope behavior

#### Scenario: an un-migrated server ignores an unknown scope

- **GIVEN** a server that predates the `Scope` field
- **WHEN** it receives a `SearchRequest` carrying `Scope`
- **THEN** it decodes successfully and runs an unscoped search (graceful degrade)
