# GitHub #958 EntityDigest human-label design

Baseline: `baa59cf1147d4ea8e3ea41000e477995a6d2044f`

Phase: `pre-owner-design-review`

Accepted inventory: `docs/proposals/gh958-entity-digest-label-inventory.md`

Accepted inventory body SHA-256: `892d44fa1d8b8f412b32d62c79026dc4d82e3176c9f42c4cc3efd506933b3d17`

Design body SHA-256: `1840aede76ed9006acc61f27344def6c22bc53444027095a3772733cdf5475e7`

Hash method: `sed -n '/^## Design body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Design body

The accepted inventory is the immutable companion artifact to this design and is included verbatim in this two-file
handoff. Its decisive findings are:

- `EntityDigest.Label` and `Type` already exist.
- LocalSearch already resolves top-level labels from loaded entities.
- GlobalSearch auto-summary resolves only representative labels; other rows use the entity-ID instance.
- Direct `searchGraph` semantic fallback emits ID and relevance only.
- The unused `resolveEntityLabels` already performs one canonical batch read under the current label convention.
- `buildEntityDigests` preserves caller ID order, fills type, joins labels by ID, and applies instance fallback, but
  its ID-keyed score input cannot preserve distinct per-row relevance for duplicate IDs.
- `NAME_INDEX` is name to IDs, not ID to display label.
- The affected compact result sets are bounded at 100 and 8 IDs.
- Graph-ingest batch responses can differ from request order; ranking must remain driven by the original ID slice.

## Adopter seam

Specific adopter: a SemSource MCP-gateway developer who has never opened graph-query internals.

Today they must know that `summarize_threshold` changes the response shape, only representative compact rows
reliably receive authoritative labels, and other compact rows can expose an ID fragment or no label. Doing nothing
produces successful but unusable labels even when canonical state contains `dc.terms.title`. This is documented only
in implementation, GitHub #958, and downstream issue notes.

After the correction they should need to know only that a compact result uses graph-query's display-text projection
when canonical state supplies either a recognized label or the retained legacy string heuristic. An absent or
ordinarily unreadable entity, or one for which neither step yields text, uses its ID instance. They need not know
representative selection, `NAME_INDEX`, predicate spelling, batch ordering, cache mechanics, or NATS payload limits.
No adopter knob is added.

## Options considered

| Option | Cost and consequence |
|---|---|
| Do nothing | No implementation cost; every adopter keeps paying for local repair or unusable labels. SemSource rejected that workaround. |
| Hydrate auto-summary only | Small diff, but direct semantic fallback remains empty and the reported top-level surface stays inconsistent. |
| Hydrate both deficient producers through the existing batch path | One added existing request per compact response, bounded at 100 or 8 IDs; fixes the full reported surface without a new contract. |
| Combine top-level and representative hydration | May save a request but couples independent projections; backfill representatives need not be result IDs and caller-sized community selection makes the union less tightly bounded. |
| Split/retry after `response_too_large` | Observation-based, but needs a new exported error discriminator or private string coupling; separate shared-surface design. |
| Resolve through `NAME_INDEX` | Wrong direction and freshness model; would add an inverse durable projection for facts already in authority. |
| Adopt registry-driven label aliases | Repairs a real divergence but changes priority, startup timing, and application vocabulary behavior beyond #958. |
| Add digest properties/full entities | Changes wire intent and compactness; adjacent downstream scope, not the label defect. |

## Recommendation

Hydrate both deficient top-level producers through the existing `graph.ingest.query.batch` path.

1. Leave LocalSearch unchanged.
2. In auto-summary, resolve labels for the final post-filter `entityIDs`, independent of representative enrichment.
3. In direct fallback, keep the adapter's existing rows and per-row relevance. First enrich those rows with
   ID-derived type and instance fallback; then resolve the final IDs and enrich type/label in place before optional
   community work. Do not rebuild relevance from the existing ID-keyed score map.
4. Join hydrated labels by entity ID and iterate the original ranked ID slice. Never use batch-response order.
5. Add no tags, properties, subjects, payloads, configuration, indexes, persistence, or readiness state.

## Measured premises and request cost

- Auto-summary is capped at 100 IDs: `processor/graph-query/graphrag.go:814-833`.
- Direct fallback is capped at 8 IDs: `processor/graph-query/searchgraph.go:13-20,100-109`.
- `resolveEntityLabels` issues one batch and maps returned states by ID:
  `processor/graph-query/graphrag.go:1888-1910`.
- `buildEntityDigests` preserves original ID order and derives type from the ID:
  `processor/graph-query/graphrag.go:1931-1947`.
- Direct fallback preserves each row's similarity in the adapter, then separately collapses scores by ID for
  community work: `processor/graph-query/searchgraph.go:135-142,244-252`. No contract guarantees unique semantic IDs,
  so label enrichment cannot reconstruct direct-fallback rows from that map.
- Graph-ingest can return cache hits before misses, not request order:
  `processor/graph-ingest/query.go:616-700`.
- The entity cache is bounded at 5,000 entries with a 30-second TTL:
  `processor/graph-ingest/component.go:1072-1083`.
- Admitted mutations invalidate after commit, with a generation guard against stale repopulation:
  `processor/graph-ingest/component.go:1985-1991,2067-2178,2294-2299,2489-2493`.
- The issue records 513,859 response bytes for 100 entities. This must not become a predicted-success premise.
- The shared responder attempts the actual publish first and classifies only observed `nats.ErrMaxPayload`:
  `natsclient/request.go:394-411`.

| Branch | Current hydration requests | Proposed hydration requests |
|---|---:|---:|
| Auto-summary without representatives | 0 | 1 top-level batch |
| Auto-summary with representatives | 1 representative batch | 2 total |
| Direct fallback without summaries | 0 | 1 top-level batch |
| Direct fallback with representatives | 1 representative batch | 2 total |
| LocalSearch | Existing load | Unchanged |

A warm cache reduces KV work but not the NATS request or JSON encoding. No latency claim is made before measurement;
tests lock the request-count invariant.

## Target behavior

For every top-level compact digest:

- Preserve ID, count, ranked position, and the relevance semantics already computed by the producing branch; label
  hydration itself does not recompute relevance.
- Populate type from the canonical six-part ID via `extractEntityType`.
- Use `resolveLabel` when the batch returns an authoritative state with a recognized predicate value or the retained
  legacy heuristic yields display text.
- Otherwise use `extractEntityInstance`.
- Do not add property fields or representative tags. The existing heuristic may source the `Label` value from an
  arbitrary eligible string triple, but that does not project the triple as a property.

Direct fallback changes from omitted optional type/label values to populated values. This corrects existing fields; it
does not add a schema field. A row-enrichment helper fills only type and label on the adapter-owned rows; it must not
recompute ID, relevance, tags, multiplicity, or row order.

### Failure and freshness model

- Reordered batch: join by `EntityState.ID`; preserve retrieval order.
- Missing state in a successful partial batch: retain the row with instance fallback.
- Loaded state without a recognized label but with eligible legacy heuristic text: use that text; use instance
  fallback only when neither resolution step yields text.
- Direct-fallback duplicate IDs, including rows with distinct relevance: hydrate the ID set once where practical,
  preserve every row, and preserve each row's original relevance. Auto-summary retains its existing ID-keyed score
  semantics; #958 changes labels, not that pre-existing computation.
- Authoritative graph-state contract error: propagate; emit no successful projection.
- Ordinary transport, timeout, malformed response, no responder, or observed `response_too_large`: use the existing
  resolver's logged best-effort omission and return instance fallbacks.
- Do not retry, sleep, inspect `Client.MaxPayload`, pre-chunk by guessed bytes, or expose a size knob.

No graph-query label cache is added. An admitted title mutation invalidates graph-ingest's cache after commit, so the
next successful hydration observes the correction. Out-of-band writes retain the existing TTL behavior and are not
legitimized here.

### Label authority

Keep and explicitly contract the fixed resolution order for #958:

1. first stored `dc.terms.title` triple, when its object is a non-empty string
2. first stored `agent.identity.display-name` triple, under the same condition
3. first stored `agent.capability.name` triple, under the same condition
4. first stored `agent.model.name` triple, under the same condition
5. legacy heuristic: first non-empty string object in stored triple order that is not a valid entity ID
6. ID-instance fallback outside the entity-label resolver

Steps 1-4 are recognized-label resolution. Step 5 is compatibility display text, not recognized label authority;
removing it here would change labels for existing entities and widen #958. Step 6 does not assert that canonical state
supplied human-readable text. Registry alignment or heuristic retirement is a separate owner decision because either
would alter behavior unrelated to SemSource's partial-hydration failure.

## Compatibility and scope

This is a non-breaking value correction. No operation, subject, response field, config, bucket, payload, generated
schema, or persistence identity changes. Existing optional type/label fields become populated consistently. Ranking,
relevance, count, strategy, summarization, source, community, and degradation behavior remain unchanged.

No ADR is warranted: the change extends an existing reversible response projection and introduces no cross-repo
primitive. No shared decision skill triggers because there is no new query access, communication path, payload, or
orchestration boundary.

Explicitly rejected scope: property projection, representative tags on top-level rows, registry-driven labels,
`NAME_INDEX` inversion, new caching, adaptive batch splitting, and activation of frozen `semantic-tier-split` work.

## Proposed OpenSpec delta

Change name: `fix-top-level-entity-digest-labels`.

### Requirement: Compact top-level entity digests project canonical human labels

Every top-level `EntityDigest` returned by `globalSearch` or `searchGraph` SHALL preserve the ranked result ID, count,
position, and relevance semantics already produced by its search branch. Label hydration SHALL NOT recompute
relevance. Its type SHALL be derived from the canonical entity ID.

For bounded compact-result branches, graph-query SHALL batch-read the final IDs through the admitted graph-ingest
batch surface. For each returned entity it SHALL inspect these predicates in order: `dc.terms.title`,
`agent.identity.display-name`, `agent.capability.name`, then `agent.model.name`. For each predicate, it SHALL inspect
the first matching stored triple and use its object only when it is a non-empty string; otherwise it SHALL advance to
the next predicate. These four steps are recognized-label resolution.

If no recognized label resolves, graph-query SHALL retain the legacy compatibility heuristic: the first triple in
stored order whose object is a non-empty string and is not a valid entity ID. This is heuristic display text, not a
recognized label predicate. If the entity is missing, hydration fails ordinarily, or neither recognized-label
resolution nor the heuristic yields text, the digest SHALL retain its row and use the entity-ID instance. That
fallback does not assert that canonical state supplied human-readable text. An authoritative graph-state contract
failure SHALL stop the response.

Batch response order SHALL NOT determine digest order. This requirement adds no property projection, label index,
caller byte budget, or payload-size prediction. Actual carrier refusal remains governed by the shared response-bounds
contract.

Required scenarios:

1. Auto-summary labels a titled non-representative without changing rank or relevance.
2. Direct fallback completes type and label while retaining strategy, degradation reason, row order, and per-row
   relevance, including duplicate IDs with different relevance values.
3. Reordered batch results join labels by ID and preserve semantic-hit order.
4. Missing rows, ordinary hydration failures, and rows yielding neither recognized nor legacy heuristic text remain
   present with instance fallback; recognized or heuristic-resolved siblings retain their display text.
5. Ordinary hydration failure preserves compact results without caller prediction or retry knobs.
6. Authoritative graph-state poison propagates and no partially validated success escapes.
7. Two direct semantic rows with the same ID and different relevance retain both rows, positions, and scores while
   receiving the same hydrated type and label from one batch.

## TDD and verification plan

1. Owner approves the fixed-convention and ordinary-failure rulings.
2. Add failing unit tests for ID-joined labels, reordered batch responses, partial batches, ordinary lookup failure,
   and authoritative poison.
3. Add failing auto-summary coverage proving a titled non-representative gets its title without rank/relevance drift.
4. Add failing direct-fallback coverage proving label/type while preserving strategy/degradation and a duplicate-ID
   fixture whose two rows retain distinct relevance values.
5. Implement auto-summary using final IDs through `resolveEntityLabels` and `buildEntityDigests`.
6. Implement direct fallback by enriching existing digest rows with type/label only; do not rebuild relevance through
   the ID-keyed score map.
7. Assert exactly one added `entityBatch` request per affected branch and none for already-correct LocalSearch.
8. Use explicit callbacks/channels in integration tests; no arbitrary sleeps.
9. Strengthen the existing semantic known-answer E2E with a measured deterministic title fixture, force compact
   results with `summarizeThreshold=1`, and require the term in `EntityDigest.Label` rather than accepting the ID.
   Do not edit or activate `semantic-tier-split`.
10. Run `go test ./processor/graph-query`, `go test -race ./processor/graph-query`,
    `go test ./test/contract/...`, and `task e2e:semantic`.
11. Run schema generation and confirm there is no response-schema delta.

## Owner rulings required

1. Keep and contract the fixed `resolveLabel` predicate order plus its legacy arbitrary-string heuristic for #958;
   inventory registry alignment or heuristic retirement separately.
2. Preserve compact search on ordinary hydration failure with instance fallback; keep authoritative contract poison
   fatal.
3. Select a deterministic E2E title fixture only after measuring which titled row reliably enters the compact result.
