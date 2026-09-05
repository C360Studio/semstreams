# Design — graph-read-tools-signal-absence (#1261)

**Status: DRAFT, conditional on the owner's INVENTORY PASS over `inventory-verification.md` and on the owner questions
in `proposal.md`.** Base `main@797d294a`.

## Decision skills

- `query-pattern`: the tools are the spec-preserved direct `query_*` surface (`openspec/specs/agentic-tools/spec.md:285-287`)
  reading authority through the catalog-reader seam; no new front door, no MCP, no embedded client. The type listing
  uses the primitive the admitted `prefix` operation is built on (inventory addition 1).
- `entity-or-bucket`: not triggered — no durable state.
- `kv-or-stream`, `orchestration-check`, `new-payload`: not triggered.

## ADR-036 call: enrich, do not add

A `describe_predicates` tool would JOIN the surface (the stub is being served, so a new tool cannot replace it) and
hand small models a ~200-name list per call. `predicates_present` on `query_relationships` is the experiment's
`describe_edges` scoped to the entity in hand. Case against the recommendation: an entity-scoped schema gives no answer
when the model holds no entity; that gap is filled by `query_by_type` (typed entry) and `research_graph`, and the
registry is process-local anyway, so a global list would still not be "the graph's" vocabulary.

## `query_by_type`: serve via the filtered key listing

`natsclient.FilteredKeys(ctx, reader, pattern)` over `ENTITY_STATES` through `graph.CatalogReader.ListKeysFiltered`
(`graph/kvcatalog.go:261`). Case against: an unbounded key listing for a huge type — the same cost the `prefix`
responder already accepts (lists, then caps at 1000). Retire was rejected: it collides with the agentic tier's approval
proof and with Codex's e2e files (inventory addition 4).

## Tier 1 shape

`KVGetter` and `NewGraphQueryExecutor` are unchanged. New exported optional interface
`KVKeyLister { KeysByPattern(ctx context.Context, pattern string) ([]string, error) }`; the adapter in
`register_graph_query.go` implements it via `natsclient.FilteredKeys`; `queryByType` type-asserts and returns
`ToolErrorInternal` naming the binding when absent — loud, never silent. `ListTools` stays five.

## Result shapes

### `query_relationships`

Before: `{entity_id, relationships:[{type,source,target}], count, direction, filter_type?}`. After:

```json
{"entity_id":"…","direction":"both","filter_type":"agent.lineage.parent","filter_registered":false,
 "relationships":[],"count":0,
 "predicates_present":{
   "agent.lineage.parents":{"kind":"relationship","registered":true,"description":"…","role":"unspecified","inverse_of":""},
   "sensor.temperature.celsius":{"kind":"property","registered":true,"description":"…"}}}
```

plus `ResultHint: "empty"`. The absence-vs-nonexistent path:

| Case | Answer |
|---|---|
| filter not `domain.category.property` | `invalid_args`, no read |
| present as a relationship | rows |
| present only as a property | empty + `kind: property` visible in `predicates_present` |
| registered and absent | empty, `filter_registered: true` |
| unregistered and absent | empty, `filter_registered: false` (process-local vocabulary; may still exist from another producer — the description says so) |
| entity missing | `not_found` (unchanged) |

Read filter vs `openspec/specs/predicate-contract/spec.md:160-162`: that requirement binds what a tool WRITES; this
tool writes nothing (0 `Publish`/`Put`/`graph.mutation` sites in the file), declares `read_only`, and its parameter
name stays outside the predicate-authority audit's substring set (inventory addition 5).

### `query_by_type`

Before: `{entity_type, limit, entities:[], count:0, note, suggested_ids:[]}`. After:

```json
{"entity_type":"temperature","pattern":"*.*.*.*.temperature.*","limit":5,"matched":12,
 "entity_ids":["…","…","…","…","…"],"count":5,"truncated":true}
```

plus `ResultHint: "too_large"` when `matched > limit`, `"empty"` when `matched == 0`. Tokens are validated as
canonical segments and the pattern by `ValidateEntityIDPattern` before any listing.

### `query_neighbors`

Before: `{source_entity, neighbors:{id:record}, count, depth, filter_type?}`. After adds `unresolved:[…]`,
`truncated`, `frontier_remaining`, and sets `HintTooLarge` on truncation / `HintEmpty` on zero. Expansion stops when
the next record would exceed the byte budget; the frontier is drained only through `IsRelationship()` targets.

## Invariants (each cited to the delta requirement)

- **R1** (`query_relationships`): `count == len(relationships)`; every relationship predicate appears in
  `predicates_present` with `kind: relationship`; `ResultHint == empty ⇔ count == 0 ∧ Error == ""`; a non-canonical
  filter never reaches the scan; `filter_registered == (GetPredicateMetadata(f) != nil)`.
- **R2** (`query_by_type`): every returned ID satisfies `MatchEntityIDPattern(pattern, id)`; output sorted;
  `truncated ⇔ matched > limit ⇔ HintTooLarge`; `HintEmpty ⇔ matched == 0`; a token that is not a canonical segment
  yields `invalid_args` and zero lister calls.
- **R3** (`query_neighbors`): `unresolved ∩ keys(neighbors) = ∅`; every neighbor is the object of an
  `IsRelationship()` triple on a visited entity; `truncated ⇔ frontier_remaining > 0 ⇔ HintTooLarge`;
  Σ neighbor record bytes ≤ budget.

## Residual (recorded, owner question 4)

`direction=incoming|both` is structurally empty when read from the record (records are own-subject only;
inventory addition 6). The incoming home is `graph.query.relationships` over INCOMING_INDEX. This design corrects the
description; enum narrowing is the owner's call.

## Test plan

Untagged unless marked; fixtures built with `graph.MarshalEntityState`, never hand-written maps (the existing
`query_entity` fixture at `graph_query_test.go:93-97` is non-canonical — a test that reconstructs).

- `TestQueryRelationships_FilteredAbsenceIsClassified` — table: registered-absent, unregistered-absent,
  present-as-property, malformed.
- `TestQueryRelationships_PredicatesPresentCarriesRegistryMetadata` — uses `vocabulary.SnapshotRegistry`.
- `TestQueryRelationships_LiteralObjectsAreNotRelationships`.
- `TestQueryByType_ListsIDsByTypeSegment` — one-token, two-token, sorted, truncated+hint, empty+hint.
- `TestQueryByType_RejectsNonSegmentTokens` — `*`, `>`, empty, three tokens; asserts the mock lister was never called.
- `TestQueryByType_WithoutKeyListerIsLoud`.
- `TestQueryNeighbors_FilterTypeReadsIDSegment`.
- `TestQueryNeighbors_BudgetTruncatesWithHint`.
- `TestQueryNeighbors_UnresolvedTargetsAreReported` — not-found → `unresolved`; transient → `ToolErrorNetwork`.
- Property (rapid): pattern construction from any two canonical segments validates; any injected wildcard is refused (R2).
- `-tags=integration`: `TestIntegration_QueryByType_ListsFromEntityStates` against real NATS via the catalog-reader
  adapter, in `register_graph_query_integration_test.go`.
- Fails-without-fix: revert the `IsRelationship` filter and the segment match separately; each reds its named test.
- `predicate_authority_contract_test.go` stays green unchanged.
- E2E: `task e2e:agentic` is the standing proof for the served `query_by_type` (inventory addition 4). The #1117
  small-model `e2e:semantic` variant is NOT a standing proof — nothing in that tier calls these tools.
