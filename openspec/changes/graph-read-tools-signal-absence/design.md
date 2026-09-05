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
when the model holds no entity. Why a tool is still not the answer: the graph-wide predicate catalog already has an
owner — `graph.index.query.predicateList` over `PREDICATE_INDEX` (`processor/graph-index/query.go:59,507-545`), consumed
today by the gateway's `predicates` field and by `graph.query.summary` (`processor/graph-query/summary.go:100`). A
`describe_predicates` tool would be a second home for that fact; the admitted route, if the model ever needs the
catalog, is a typed adapter over that subject — the owner-question-7 shape (a NATS dependency on a component absent
from the agentic tier). What the catalog does not carry is kind, description, role, and inverse — that is the
process-local registry's content, and `predicates_present` adds exactly that, scoped to the entity in hand. The
cold-start gap is filled by `query_by_type` (typed entry) and `research_graph`.

## `query_by_type`: serve via the filtered key listing

`natsclient.FilteredKeys(ctx, reader, pattern)` over `ENTITY_STATES` through `graph.CatalogReader.ListKeysFiltered`
(`graph/kvcatalog.go:261`). Case against: an unbounded key listing for a huge type — the same cost the `prefix`
responder already accepts (lists, then caps at 1000). Retire was rejected: it collides with the agentic tier's approval
proof and with Codex's e2e files (inventory addition 4).

**The matcher on the type axis is `pkg/types.MatchEntityIDPattern`** (`pkg/types/entity_id.go:166-186`; exact
six-position, byte-exact, both inputs validated) — for `entity_type` on every key the NATS filter returns (R2) and for
`filter_type` on every neighbor identity (R3). The one/two-token grammar is a pattern BUILDER (`temperature` →
`*.*.*.*.temperature.*`; `environmental.temperature` → `*.*.*.environmental.temperature.*`) validated by
`ValidateEntityIDPattern`; no new extractor joins `graphrag.go:258` and `graph/clustering/summarizer.go:18`. The
existing selector on this axis, `graphrag.filterEntityIDsByType` (`processor/graph-query/graphrag.go:1570-1592`,
ADR-071), is deliberately NOT reused and the divergence is intentional: it is package-private to graph-query, it
folds case (admitting a token the canonical alphabet, `entity_id.go:243-249`, would reject), and it widens to the
unfiltered input when a non-empty set filters to empty (`:1589-1592`) — the right recall choice for classifier guesses
over a semantic hit list, and exactly the silent substitution a model-requested selection must never make (an absent
type answers empty + `HintEmpty`, never the whole bucket). Same grammar underneath (`ParseEntityID`), two matchers
with two stated semantics; `graph/id_prefix.go:19-21`'s one-matcher rule is honoured on the axis it names
(leading prefix), which this change does not touch.

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
  `IsRelationship()` triple on a visited entity; `filter_type` keeps exactly the identities for which
  `MatchEntityIDPattern(pattern, id)` is true; `truncated ⇔ frontier_remaining > 0 ⇔ HintTooLarge`;
  Σ neighbor record bytes ≤ budget.

## Residual (recorded, owner question 4)

`direction=incoming|both` is structurally empty when read from the record (records are own-subject only;
inventory addition 6). The incoming home is `graph.query.relationships` over INCOMING_INDEX. This design corrects the
description; enum narrowing is the owner's call.

## Budget: a model-facing content cap, not the transport bound

Two bound classes exist on this component and the delta names which one `query_neighbors` joins:

| Class | Where | Who owns the number | How it is observed |
|---|---|---|---|
| Transport bound | `openspec/specs/agentic-tools/spec.md:467-475` — the component attempts the full record, and a typed oversize rejection yields one compact `too_large` authority; `:473` "SHALL NOT inspect configured payload limits" | NATS (`max_payload`); the framework never reads it | by attempting the real Create |
| Model-facing content cap | `executors/bash.go:36` `bashMaxOutputBytes = 100 * 1024`; `executors/httprequest.go:23` `httpMaxTextSize = 20000` | the executor, as a constant | by measuring the real bytes of real content while assembling |

The neighbors budget is the second class. It is not a read of a framework-owned limit and does not predict the
transport outcome: assembly fetches real records, counts their real bytes, and stops before the next one would cross
the cap — `truncated`, `frontier_remaining`, `HintTooLarge` report what was observed. A result under the executor cap
that still trips the transport bound takes spec `:467`'s path unchanged; the two compose, they do not overlap. The
origin case in `docs/concepts/24-tool-result-hints-and-pagination.md:8-9` (a 102KB graph result retried three times by
mid-tier models) is this class: the failure was model-facing, not transport. Neither existing cap is specced; the
delta's `query_neighbors` requirement is the first to state the class. External support for the class: the Cekikj
restatement's evidence review (Part 2 § 2.5) cites Microsoft Research's tool-space interference work — over-long tool
responses cut performance by up to 91% even inside the context window. Adopter seam: nothing to configure; owner
question 1 is the value only.

## Break classification and sequencing (owner obligation 1, #1261 note 2026-09-05)

**Go surface — additive.** `processor/agentic-tools/executors` is Tier 1 (`release/tier1-packages.txt:79`). The change
adds one exported optional interface (`KVKeyLister`) and changes no existing exported symbol; `KVGetter`,
`NewGraphQueryExecutor`, and `ListTools` are unchanged. ADR-106 (`docs/adr/106-…md:81-83`): a compatible addition to
Tier 1 does not reset RC-4 **but must pass the walked-path guard (RC-6) before the tag that ships it** — "a brand-new
surface with one adopter walking it under time pressure, which is the dominant defect shape being minted live". For
`KVKeyLister` the walked path is NOT the approval walk as it stands: that walk asserts a counter delta on
`{tool_name=query_by_type, status=success}` (`test/e2e/scenarios/agentic/approval_signal.go:139-144,186-190`) and never
reads the result, and a zero-key listing is also `status=success` — so it passes identically over a working listing, an
empty one, and the stub. The walked path this change supplies is two-part:

1. **Integration** (`tasks.md` 4.2): `TestIntegration_QueryByType_ListsFromEntityStates` against real NATS through the
   catalog-reader adapter — the first positional-wildcard call of `ListKeysFiltered` in the tree (every existing
   `FilteredKeys` caller passes `prefix+">"`: `graph/inference/storage.go:312,560`, `graph/clustering/storage.go:243`).
2. **Booted binary** (`tasks.md` 4.5): the mock's pinned args move from `temperature` (matches nothing in the tier) to
   `{"entity_type":"agent.execution","limit":5}` (`test/e2e/mock/cmd/main.go:38`; not Codex-held), and
   `walkApprovalPath` gains one assertion after the success metric: read `tool.result.<pending.CallID>` from the stream
   (the read `scenario.go:411-452` already performs), decode `agentic.ToolResult.Content`, and assert `pattern ==
   "*.*.*.agent.execution.*"`, `matched >= 1`, and `entity_ids` contains
   `agentic.LoopExecutionEntityID(org, platform, <primary loop_id>)` — the entity `verifyGraphTriples` proved present
   five stages earlier (`scenario.go:238,243`). Lives in `approval_signal.go` (not Codex-held; `scenario.go` is).

Until 4.5 lands, RC-6 is satisfied by the spec scenario plus the integration test only. `task api:compat:report` is in
the gates (`tasks.md` 6.1), so compatibility is measured, not read from a `!` marker (`:114-115`). No payload, schema, subject, or KV key changes.

**Model-facing result shapes — three tools change what the model reads.** The consumer is the model via
`buildToolMessages`; the shapes are JSON inside `ToolResult.Result`, not a Go type.

| Tool | Additive | Behaviour that flips | Who reads the old shape today |
|---|---|---|---|
| `query_by_type` | `entity_ids`, `pattern`, `matched`, `truncated`, hints | stub `{entities:[], note, suggested_ids}` → served listing; a non-segment `entity_type` → `invalid_args` where the stub accepted anything | nothing pins the stub shape (0 hits for `suggested_ids` outside `graph_query.go`); the agentic e2e approval walk asserts `status="success"` on the executions metric with pinned args `{"entity_type":"temperature","limit":5}` (`test/e2e/mock/cmd/main.go:38`; `test/e2e/scenarios/agentic/approval_signal.go:36-40,77-88`) — a canonical one-token segment the served tool accepts, and one that matches NOTHING in this tier: the agentic tier writes only loop-execution and model-endpoint entities to ENTITY_STATES (`scenario.go:884-887`), the sensor ID is a `query_entity` argument (`scenario.go:362`), so the served tool answers `matched: 0` + `HintEmpty` with `status=success` and the walk stays green either way |
| `query_relationships` | `filter_registered`, `predicates_present`, `HintEmpty` | rows are `IsRelationship()` triples only — literal-object triples reported as relationships today (`:591-617`) disappear; a malformed filter → `invalid_args` where today `count: 0` | the model; prompt text names the tool, never a result key (`processor/agentic-loop/prompt/assembler.go:142`; `configs/personas/fragments/ops/00-identity.md:9`; `configs/flows/ops-agent.json:316`, an `allowed_tools` entry) |
| `query_neighbors` | `unresolved`, `truncated`, `frontier_remaining`, hints | `filter_type` filters (today ignored, `:442`) — a caller passing it gets a smaller set, possibly empty; the budget truncates where today unbounded; a transient fetch error fails the call where today it is skipped (`:428-431`) | the model; same prompt-text finding |
| `query_entity`, `query_entities` | — | none | — |

No sister calls the executor directly (inventory addition 8); semteams routes all seven graph tools to ops roles only
(`semteams/docs/adr/041-mvp-role-compression-and-graph-as-substrate.md:874-890`, read-only); semsource registers
none of them (`semsource/processor/mcp-gateway/component.go:119-122`, read-only). The served `query_by_type` is
reachable in the agentic tier through the existing adapter: `graphQueryKVAdapter.bind` returns a
`graph.CatalogReader` (`register_graph_query.go:65-71`), which carries `ListKeysFiltered` (`graph/kvcatalog.go:261`),
so the loud no-lister path (`TestQueryByType_WithoutKeyListerIsLoud`) cannot fire there.

**Classification (owner question 9).** Recommend `feat(agentic-tools):` without `!`: the Go surface is compatible,
nothing on the wire changes, and every flipped behaviour is a tool answering truthfully where it answered wrong or
nothing. Strongest case against: the three flips (relationship rows filtered, `filter_type` honoured, the budget)
each shrink a result set a deployment may be reading today, and the owner's note reads the change as a break. The
decision changes the label and the migration doc's prominence, not the gate: `task e2e:agentic` is green before merge
either way (`tasks.md` 6.2), and the migration doc (owner question 5) carries before/after JSON for all three flips.

**Sequencing (owner question 11).** Measured with pagination (`gh api repos/:owner/:repo/pulls/N/files --paginate`;
`gh pr view --json files` caps at 100 and PR #1159 has 133): #1156 holds 54 paths, #1159 133, #1141 7 — 176 unique.
The implementation's file set (`executors/graph_query.go`, `executors/register_graph_query.go`, their `_test.go`
siblings, `docs/operations/migration-graph-read-tools.md`, this change directory) intersects none of them. Two
shared things remain, neither a file conflict:

- `openspec/specs/agentic-tools/spec.md` — Codex's `agentic-loop-restart-safety` delta MODIFIES `:435/:467/:487`;
  this delta is ADDED-only. Whichever archives second rebases its delta on the other's spec text: archive-order
  coordination.
- the agentic e2e tier — #1156 holds `test/e2e/scenarios/agentic/scenario.go` and `approval_signal_test.go`; this
  change edits no file there but changes what the approval walk observes (stub success → served success). Whichever
  lands second runs `task e2e:agentic` on its rebase; that is the same gate both already carry.

Recommend relaxing the HOLD to that coordination: the hold was file-list based and the measured lists do not
intersect. Against: two ADDED deltas landing on one spec and one shared e2e tier in the same window. Milestone is
owner question 10 (`v1.0.0-beta.165` recommended, landed first in it; an own tag ships the shapes sooner).

## Tool-preference premise (owner obligation 2, #1261 note 2026-09-05)

**The failure, recorded.** Agents preferred `grep`, `bash`, and other training-corpus tools over bespoke graph tools
(owner note on #1261, verbatim in the docket comment); the framework's own record of the related shape is
ADR-036 `docs/adr/036-agent-private-observable-state.md:236-244` — semteams smoke #7, "small models drown when the
tool surface widens", persona-level opt-out as the lever. This design changes what a called tool returns, not whether
the model calls it. Stated plainly: enriched results do not fix tool preference; they remove the second failure (a
called tool that answers wrong) so the first (a tool not called) is the only one left to measure. `research_graph`
exists because of the first failure; the served `query_by_type` gives a restricted agent a second entry beside it.

**Where surface restriction lives today (measured; three seams, none touched by this change).**

| Seam | Pins | Semantics |
|---|---|---|
| Component allowlist | `processor/agentic-tools/config.go:19` `AllowedTools` (nil/empty allows all); `component.go:988-994` `isToolAllowed` → `not_allowed`; `metrics.go:156` `recordToolFiltered` | deployment-wide ceiling |
| Per-loop advertised set | `processor/agentic-dispatch/config.go:28` `default_tools` → `task.Tools` → `processor/agentic-loop/handlers.go:990-1001` (`task.Tools != nil` wins, an explicit empty slice means no tools; nil falls back to discovery) → `CacheTools` → stamped as `agent.tools.advertised` (`agentic/exec_policy.go:63`; `handlers.go:1684-1689`) → enforced at `component.go:974-984` `admitToolCall` (key present-but-empty fails closed) | per-role set, the lever ADR-036 names |
| Rule-level governance | `docs/operations/17-tool-call-governance.md:161-170` (`auto-approve-readonly-tools` over `agent.toolcall.proposed.>`), `:261` (role-based allowlist with caller context, ADR-041 `when`) | per-call verdicts |

In tree, `configs/agentic.json:468-469` already runs the cited experiment's condition — `allowed_tools:
["query_entity","query_by_type"]`, graph-read tools only — and `configs/flows/ops-agent.json:313-328` is the ops
role's fourteen-tool allowlist with no `bash`. Reproducing the experiment's restriction needs no framework change; it
is flow and persona configuration today.

**External evidence the owner's restatement collected (Part 2 / Part 3 of the Cekikj evidence review, 2026-09-04),
and where each lands in this design.**

| Finding (restatement §) | Bears on | Where it lands |
|---|---|---|
| § 2.5 — large tool spaces cut performance by up to 85%; flattening a parameter schema improved tool-calling by 47% (Microsoft Research); LiveMCPBench: cutting retrieved tools from five to one dropped success from 78.95% to 64.21% | tool count in both directions: width costs, and over-restriction costs | ADR-036 call above: enrich five, add none; parameters stay flat strings. The LiveMCPBench number is the caution against reading "graph-read tools only" as free — it is a configured restriction with a measured cost, not a default |
| § 2.5 — over-long tool responses cut performance by up to 91% inside the window; Chroma's context-rot result across 18 models | result size | § Budget (model-facing cap); `HintTooLarge` + `truncated` + `frontier_remaining` on `query_neighbors`; identities-only `query_by_type` (owner question 2) |
| § 2.3 — tool names, not descriptions, are the primary routing signal (Agent4Science review); SNAILS: identifier naturalness correlates with accuracy | what the model routes on | the five names are unchanged; descriptions are rewritten for truth, never relied on for routing; `predicates_present` shows registry names instead of asking the model to guess them |
| § 3.3 — the article's ranking is a property of its traversal-only tool surface; distance is cheap when the model sees the whole schema and expensive when it discovers the path one call at a time | which surface the finding applies to | this change is the traversal surface; the whole-schema route stays `research_graph` and the graph-query/gateway operations (`summary`, `predicateList`, `hierarchyStats`) — none re-homed here |
| § 3.4 — the tool layer is the cheaper lever: a single `get_schema` or one query-writing tool would have removed most of the flat graph's 18 false refusals; GitHub Copilot cut its tool count from 40 to 13 with measurable improvement | schema exposure vs ontology change; tool count | `predicates_present` is the entity-scoped `get_schema`; the global catalog has an owner already (ADR-036 call); no tool added |
| § 3.8 — a fan of typed edges lost to one readable property on aggregation (0.17 vs 0.76) | graph shape, not tool shape | #1260 (`edge-or-property` heuristic), out of scope here |

These are cited as the restatement reports them; none was re-run here, and the restatement's own Part 3 § 3.6 caution
(small synthetic benchmarks) applies to every number in the table.

**Is the experiment's `find_nodes` a gap in the direct surface?** Its roster is four tools (`code/graph.py`, read
from the cited repository: `find_nodes` `:47-65`, `get_node` `:67`, `traverse` `:73`, `describe_edges` `:98`).
`find_nodes` is a case-insensitive substring over `json.dumps(props)` and the node id, optional exact-type filter,
sorted by id, capped — a grep WITHOUT regex. Mapping to the direct surface after this change:

| Experiment | Direct surface | Status |
|---|---|---|
| `get_node` | `query_entity` | covered |
| `traverse` | `query_neighbors`, `query_relationships` | covered; truthful after this change |
| `describe_edges` | `predicates_present` on `query_relationships` (entity-scoped) | covered by this change |
| `find_nodes`, id half (`needle in node_id.lower()`) | served `query_by_type` (type segment); adjacent owner `graph.ingest.query.suffix` over `ENTITY_SUFFIX_INDEX` (trailing segments; graph-ingest IS in the agentic tier) | partly covered by this change; the suffix responder is a candidate for the rest, not adopted (owner question 8) |
| `find_nodes`, props half (substring over property values) | none — `byName` is exact over NAME_INDEX and fusionnats-only (`processor/graph-query/query.go:64`); `searchGraph`/`localSearch` are semantic/statistical and tier-dependent (`:63,65`); `prefix` needs leading segments (`:56`); `summary` is a type distribution (`:62`) | **gap** |

What governs filling it: `openspec/specs/agentic-tools/spec.md:267-291` — the framework SHALL NOT supply
`search_graph`/`summarize_graph`; an application MAY register a component-local executor through the general
extension seam, subject to the allowlist, per-loop advertised set, and approval; the framework adds no alias or
special behaviour. The live precedent is semsource's `graph_search` (`mcp-gateway/component.go:112-125`, read-only).
Owner question 8 recommends no framework substring tool (an O(N) scan over every record's values; ADR-036's width
cost; re-opens `:267-291`); the strongest case against is that the experiment's whole condition rested on it. A
framework-owned fill re-opens the spec requirement, which is an owner ruling, not a design choice.

**What `research_graph` remains for.** `frameworkcapabilities/graphresearch/executor.go:145-156`: `Mutating` effect
(it spawns a loop and writes the trigger key), asynchronous, "classifier → route → multi-tier subqueries →
sufficiency → synthesis", advertised "for non-trivial questions where you don't already know the entities or
predicates to query". After this change the split is: the direct tools answer "what is here, and what is absent, by
ID or by type" synchronously and truthfully; `research_graph` answers "what is this about" when the model holds
neither an ID nor a type. Its description's "for direct lookups by ID, use query_entity" stays true and could gain
"by type, use query_by_type"; that file is outside this change's file set — recorded as a residual, not a task.

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
