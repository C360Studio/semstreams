# Design: Complete top-level EntityDigest labels

## Accepted evidence and rulings

- Accepted inventory: `docs/proposals/gh958-entity-digest-label-inventory.md`, body SHA-256
  `892d44fa1d8b8f412b32d62c79026dc4d82e3176c9f42c4cc3efd506933b3d17`.
- Accepted design: `docs/proposals/gh958-entity-digest-label-design.md`, body SHA-256
  `1840aede76ed9006acc61f27344def6c22bc53444027095a3772733cdf5475e7`.
- Owner approved the reviewed design on 2026-08-13.

The binding rulings are:

1. Keep and contract the fixed `resolveLabel` predicate order plus its legacy arbitrary-string heuristic. Registry
   alignment or heuristic retirement requires separate inventory and owner review.
2. Preserve compact search on ordinary hydration failure with instance fallback; authoritative graph-state contract
   failures remain fatal.
3. Select the semantic E2E title fixture by measurement, use explicit synchronization, and do not activate the frozen
   `semantic-tier-split` change.

## Decision

Reuse `resolveEntityLabels` for the final bounded ID set in both deficient compact-result branches. Auto-summary passes
that ID-keyed label map to the existing ID-ordered builder. Direct fallback enriches the adapter's existing digest rows
in place, changing only type and label so row order, multiplicity, tags, ID, and per-row relevance remain intact.

Keep the current fixed label resolution order. Do not use `NAME_INDEX` or dynamic vocabulary discovery. Community
representative enrichment remains independent because its PageRank backfills need not be top-level result IDs and its
community count is caller-sized.

## Composition

Auto-summary resolves display text for its final post-filter `entityIDs`, then uses `buildEntityDigests`. This retains
that branch's existing ID-keyed relevance semantics; label hydration does not recompute relevance.

The direct semantic adapter continues to preserve one row per raw semantic hit. A private row-enrichment helper fills
only type and label, first with instance fallbacks and then with the resolved ID-keyed display-text map. The existing
ID-keyed score map remains solely for community work and never reconstructs top-level rows.

Graph-ingest batch response order is never treated as ranked order. Labels join by entity ID while response projection
iterates the original producer-owned row or ID sequence.

## Failure and freshness model

Successful partial batches use resolved display text where present and instance fallback elsewhere. Ordinary
transport, timeout, malformed-response, no-responder, or observed carrier refusal preserves compact results with
instance fallback. Authoritative graph-state contract failure propagates and no partially validated success escapes.

No retry, arbitrary sleep, `Client.MaxPayload` prediction, guessed byte chunking, or caller size knob is introduced.
Carrier limits remain governed by actual publish observation in the shared response-bounds contract.

No graph-query label cache is added. Admitted canonical mutations invalidate graph-ingest's entity cache after commit,
so the next successful hydration observes the correction. Out-of-band authority writes retain the existing 30-second
TTL behavior and are not legitimized here.

## Label resolution

For each returned authoritative entity, graph-query resolves display text in this order:

1. the first stored `dc.terms.title` triple, when its object is a non-empty string;
2. the first stored `agent.identity.display-name` triple under the same condition;
3. the first stored `agent.capability.name` triple under the same condition;
4. the first stored `agent.model.name` triple under the same condition;
5. as a legacy compatibility heuristic, the first triple in stored order whose object is a non-empty string and is
   not a valid entity ID;
6. otherwise, the entity-ID instance segment.

Steps 1-4 are recognized-label resolution. Step 5 is heuristic display text, not recognized label authority. Step 6
does not assert that canonical state supplied human-readable text.

## Compatibility and performance

No wire field or operation changes. Direct fallback begins populating existing optional type/label fields;
auto-summary label values become human-readable when authority supplies display text. ID, count, ranking, branch-owned
relevance, strategy, summarization, source, community, and degradation behavior remain unchanged.

Each affected compact response adds one existing batch request: at most 100 IDs for auto-summary or 8 for direct
fallback. Community representative hydration stays separate. Request-count tests lock this bound. No latency claim is
made before implementation measurement.

No ADR is warranted. Property projection, representative tags on top-level rows, registry-driven labels,
`NAME_INDEX` inversion, new caching, adaptive batch splitting, and frozen `semantic-tier-split` work are out of scope.

## Binding ruling conformance

Implementation must complete this table with exact `file:line` evidence before review.

| Ruling | Implementation and test evidence | Deviation |
|---|---|---|
| R1 — fixed predicate order and legacy heuristic | Existing owner retained at `processor/graph-query/graphrag.go:1911-1927`; exact order/unusable-first-match coverage at `graphrag_test.go:52-138` | None |
| R2 — ordinary fallback; authoritative poison fatal | Both branches at `processor/graph-query/entity_digest_labels_test.go:142-269`; resolver split at `graphrag.go:1883-1908` | None |
| R3 — measured E2E fixture, explicit synchronization, frozen change untouched | Exact fixture and label-only assertion at `test/e2e/scenarios/tiered_semantic_known_answer.go:25-46,162-180,323-338`; guard at `tiered_semantic_known_answer_test.go:8-43`; no arbitrary sleep or `semantic-tier-split` edit | None |
| Preserve direct-fallback rows and per-row relevance | In-place enrichment at `processor/graph-query/searchgraph.go:131-144,187-212`; duplicate/reordered proof at `entity_digest_labels_test.go:77-140` | None |
| One bounded top-level batch per affected branch | Auto-summary at `processor/graph-query/graphrag.go:892-904`; direct fallback at `searchgraph.go:131-144,187-212`; request-count and LocalSearch control at `entity_digest_labels_test.go:18-75,77-129,272-300` | None |
| No new public/runtime surface or payload prediction | Production diff adds one private helper and calls the existing resolver/batch seam; no config, index, subject, payload, readiness, retry, preflight, or exported symbol | None |

There are no owner-authorized deviations.
