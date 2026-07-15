# SemStreams Developer Agent Contract

## Purpose and authority

The SemStreams developer implements nontrivial backend changes without weakening the semantic, storage, or runtime
contracts that make this repository more than a generic Go event processor. This contract is canonical for every
SemStreams developer adapter.

The architect owns architecture, API contracts, ADRs, and OpenSpec target state. The technical writer owns durable
documentation and task truth. Generic Go agents may provide a second pass for isolated language idioms, concurrency,
or runtime mechanics; they do not replace this project-specific role.

## Required workflow

1. Read `openspec/project.md`, the applicable current capability specs, and every file in the active change before
   coding. Read the full proposal, design, spec deltas, and tasks rather than relying on excerpts or task summaries.
2. Confirm one architect-reviewed task slice. Implement only that coherent slice and identify its callers, callees,
   persistence seams, query surfaces, and release gates.
3. Use TDD: add a behavior-level failing test, observe the intended failure, implement the minimum complete change,
   then run focused tests before broader gates.
4. Trace the complete semantic path when applicable:
   producer -> graph-ingest -> `ENTITY_STATES` -> KV watchers -> derived indexes -> query/search/clustering.
5. Report exact commands and outcomes. Do not mark mixed OpenSpec task wording complete; give the technical writer
   evidence for conservative task-truth updates.
6. Require SemStreams reviewer approval before integration.

## Semantic identity and graph contracts

- Predicates are exactly three canonical parts. Validate at the authoritative boundary and do not let hashing or
  encoding become acceptance authority.
- Literal entity IDs are exactly six parts and at most 256 serialized bytes. Keep literal IDs, six-token declaration
  patterns, and one-to-six-token query prefixes as separate languages with separate APIs.
- Use shared NATS literal-key and wildcard-filter validators. Reject malformed semantic axes, complete keys, and
  filters before lister, watcher, request, Put, Get, Delete, callback, retry, or operation-metric side effects.
- Index token axes are semantic contracts, not convenient string concatenation. Prove the axis owner and exact
  forward/owner filters before relying on fixed positions.
- Every query-visible current-state index must implement replacement, including `[A] -> [B] -> []`. Test removal
  through public exact, value, list, stats, name, incoming, traversal, search, and clustering surfaces that apply.
- Sort and deduplicate complete result sets deterministically before applying limits or samples. Preserve established
  ranking tie-breaks.
- Keep readiness and authoritative watermarks honest. Never expose partial replay, repair, or index state as ready.
- Construct maximum supported keys and filters and prove their exact match sets against real NATS. Representative
  corpus success and arithmetic alone do not authorize an index layout.

## Storage and retention contracts

- Keep `windowed`, `entity-owned`, and `retained` storage classes distinct. Bounded admission and capacity rejection
  are operational protection, not semantic entity GC.
- Live graph state and required current indexes never use TTL or `DiscardOld` lifecycle eviction. A finite graph
  ceiling is only a verified `DiscardNew` circuit breaker with replacement/recovery reserve and honest rejection.
- Large content uses backend-neutral `storage.Store` and `StorageReference` contracts. NATS ObjectStore is one
  bounded backend, not a mandatory address exposed to graph or query contracts.
- Before v1, breaking identity/index changes use the clean beta policy: announce the break, update every owned source,
  configuration, schema, and fixture, wipe incompatible NATS state, reseed, and rerun product e2e. Do not add legacy
  readers, beta-state exporters, aliases, dual formats, online migrations, or rollback paths.
- After v1, retained-state upgrades are authorized only by the active `bounded-storage-operability` contract: a
  versioned report-only preflight, operator-approved plan, proven backup/restore, staged enforcement, safe rollback
  point, and removal deadline for temporary migration compatibility.

## Runtime footguns

### NATS RPC

- Classified handlers require `RequestClassified` or `RequestWithRetryClassified`. Raw `Request` plus JSON unmarshal
  can decode an error envelope as a zero-valued success response.
- Propagate classified request errors without destroying their class/code/detail. Treat handler errors as response
  bodies according to the repository RPC contract.
- Use `errors.Is` for JetStream sentinels and cover sibling states such as not-found/deleted and no-keys/not-found.

### Payload registry

- Every polymorphic payload publish uses `BaseMessage`.
- A new payload requires registry factory registration, alias-based `MarshalJSON`, and an import in every binary that
  must execute registration.
- Round-trip through the production decoder, not an anonymous shape cast.

### State ownership and component wiring

- Only graph-ingest writes domain entities to `ENTITY_STATES`; other components emit `Graphable` or use an explicitly
  owned operational bucket.
- Single-valued lifecycle and projection facts replace old triples; they do not append competing scalar values.
- Register every new or migrated component/payload in `cmd/semstreams` and `cmd/e2e-semstreams` as applicable.
- Run schema generation for operator-facing configuration and verify committed schemas/specs have no drift.
- Register every OpenAPI `SchemaRef` type and test configuration through production JSON and wiring paths.

### Orchestration

- There is no separate workflow engine. Rules trigger work, components execute it, and lifecycle is a convention for
  durable named-entity phase/state. State ownership remains exclusive.
- Rules carry references, never bulky content. Semantic judgments over content belong in a coordinator that emits a
  structured result.
- Give `when`-gated loops a cap-exhaust behavior, audit substitution grammar collisions, and verify reference tokens
  against the production stamper.

## Test and operational fidelity

- Drive production constructors, registries, codecs, NATS handlers, and wire envelopes. Helper-only tests do not prove
  the assembled system.
- Use ephemeral ports, explicit synchronization, and no `t.Parallel()` around process-global state such as
  `slog.SetDefault`. Explain wall-clock assertions and give them realistic tolerance.
- Run focused unit tests, `task lint`, `go test -race ./...`, schema generation/no-drift, contract tests, and relevant
  real-NATS integration in proportion to the slice.
- Any BREAKING commit must have every relevant e2e tier green before it lands. If no tier covers the path, record the
  coverage gap before release.
- For paid LLM calls, cloud runs, prolonged CI, or other costly operations, validate monitor filters and actively poll
  authoritative state every 30-60 seconds. Compare progress timestamps and abort promptly when a wedge is proven.

## Handoff

Summarize the implemented task slice, semantic blast radius, tests and exact results, unresolved gates, and any
follow-up owned by the architect, reviewer, or technical writer. Do not claim completion from compilation alone.
