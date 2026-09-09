# Design — graph-read-tools-signal-absence (#1261)

**Status: DRAFT. The owner ruled questions 1–12 and gates G1/G2 on 2026-09-07 (#1261 comment 7, adopting the
recommendations in comments 5 and 6); this revision applies them. Still conditional on the owner's INVENTORY PASS,
which ruling G2 defers until after the closure-only round 5 over `inventory-verification.md`.** Base `main@797d294a`;
the Codex-held path set re-measured 2026-09-07.

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
from the agentic tier), ruled **not in this change** and filed as **#1265** (`v1.0.0-beta.165`). What the catalog does
not carry is kind, description, role, and inverse — that is the
process-local registry's content, and `predicates_present` adds exactly that, scoped to the entity in hand. The
cold-start gap is filled by `query_by_type` (typed entry) and `research_graph`.

## `query_by_type`: serve via the filtered key listing

`natsclient.FilteredKeys(ctx, reader, pattern)` over `ENTITY_STATES` through `graph.CatalogReader.ListKeysFiltered`
(`graph/kvcatalog.go:261`). Case against: an unbounded key listing for a huge type — the same cost the `prefix`
responder already accepts (lists, then caps at 1000). Retire was rejected: it collides with the agentic tier's approval
proof and with Codex's e2e files (inventory addition 4).

**The matcher on the type axis is `pkg/types.MatchEntityIDPattern`** (`pkg/types/entity_id.go:166-186`; exact
six-position, byte-exact, both inputs validated) — for `entity_type` on every key the NATS filter returns (R2) and for
`filter_type` on every neighbor identity (R3). The grammar is a pattern BUILDER over one to three RIGHT-ANCHORED
canonical segments (owner ruling Q6): `temperature` → `*.*.*.*.temperature.*`; `environmental.temperature` →
`*.*.*.environmental.temperature.*`; `gcs.environmental.temperature` → `*.*.gcs.environmental.temperature.*`. One
rule, one validator, one matcher, three arities — and the third is exactly the bucket key `graph.query.summary`
already emits (`processor/graph-query/summary.go:197`, `typeKey := parsed.System + "." + parsed.Domain + "." +
parsed.Type`), so a summary bucket pastes into `entity_type` as-is once #1265's typed adapter exists. Every arity is
validated by
`ValidateEntityIDPattern`; no new extractor joins `graphrag.go:258` and `graph/clustering/summarizer.go:18`. The
existing selector on this axis, `graphrag.filterEntityIDsByType` (`processor/graph-query/graphrag.go:1570-1592`,
ADR-071), is deliberately NOT reused and the divergence is intentional: it is package-private to graph-query, it
folds case (admitting a token the canonical alphabet, `entity_id.go:243-249`, would reject), and it widens to the
unfiltered input when a non-empty set filters to empty (`:1589-1592`) — the right recall choice for classifier guesses
over a semantic hit list, and exactly the silent substitution a model-requested selection must never make (an absent
type answers empty + `HintEmpty`, never the whole bucket). Same grammar underneath (`ParseEntityID`), two matchers
with two stated semantics; `graph/id_prefix.go:19-21`'s one-matcher rule is honoured on the axis it names
(leading prefix), which this change does not touch.

## Continuation: adopt the pagination half of the tool contract (owner ruling Q12)

The framework declares **two** complementary contracts, not one
(`docs/concepts/24-tool-result-hints-and-pagination.md:16-27`): `ResultHint` for refinement, and
`ToolDefinition.Paginated` + `MetadataKey{HasMore,NextOffset,NextCursor}` for continuation. `HintTooLarge`'s own
declaration says they compose (`agentic/tools.go:551-555` — the model gets BOTH "narrow your query" AND "or continue
with cursor=…" in one shot); the contract's MUST is on the executor (`agentic/tools.go:44-59`, `:576-582`: `has_more`
is set on EVERY successful result of a paginated tool, `false` included, and its absence is "a contract violation
worth a Warn log"); the same tool roster already produces it (`processor/agentic-tools/loop_result.go:55-56,65`,
continuation argument advertised at `:78`, metadata set at `:157-158`; one `RegisterBuiltins` registers both —
`executors/register.go:136`, `read_loop_result` at `:182`, this executor at `:188`); and the loop already renders it
(`processor/agentic-loop/result_hint.go:69-88` → `handlers.go:2639`, one line below the hint decorator this design
already relies on). An earlier revision of this design invented `truncated` in the content body: a second spelling of
a fact the contract owns, invisible to `decorateContentWithPagination`, telling the model to narrow a filter that is
already exact.

**`query_by_type` adopts it.** `Paginated: true` on the definition; a `cursor` string parameter beside `limit` (the
`read_loop_result` precedent advertises its own continuation argument, `loop_result.go:78`); `has_more` in
`ToolResult.Metadata` on every successful call; `next_cursor` when matches remain beyond the page. `matched` stays
(the observed total, on every page), `truncated` goes — one spelling. `HintTooLarge` is still set on a page that does
not exhaust the match, which is the composition the hint was written for.

**The cursor is the graph package's existing one, not a new codec.** `graph.EncodeCursor`/`graph.DecodeCursor`
(`graph/query_prefix_types.go:78,84`) already encode an opaque, URL-safe token over a raw `ENTITY_STATES` key for
`PrefixQueryResponse.NextCursor` (`:74`), the cursor contract and keyset caveat are stated at `:19-44`, and
`executors` already imports `graph` (`graph_query.go:10`). Same fact, same encoding, one home. The mechanics are the
ones `handleQueryPrefixNATS` already runs: sort, then advance past the decoded key
(`processor/graph-ingest/query.go:310-327` — "MANDATORY: sort before cursor application — cursor is meaningless
without a deterministic key order"). A decode failure is `invalid_args` before any listing.

**Sorting is ours to do — measured, not assumed.** `natsclient.FilteredKeys` returns keys in KV-scan order:
`collectFilteredKeys` appends in channel arrival order and nothing sorts (`natsclient/kv.go:582-600`, the append at
`:597`). R2's "sorted" clause and the cursor's correctness both rest on the executor's own `sort.Strings`, exactly as
the prefix responder does at `processor/graph-ingest/query.go:312`. This closes round 4's open item ("whether
`FilteredKeys` returns keys already sorted") with a measurement rather than deferring it to implementation review.

**Cost, stated plainly.** Each page is a full filtered key scan: NATS KV has no ranged scan, so the cursor slices a
freshly-listed, freshly-sorted key set rather than seeking (`graph/query_prefix_types.go:41-44`, the backend note on
this same primitive). Paging is O(N) per page — the identical profile `PrefixQueryResponse.NextCursor` accepts today,
here over identities only and bounded by the advertised `limit` maximum of 100.

**`query_neighbors` refuses it, and the reason is recorded.** A BFS frontier has no stable resumable position without
server-held state, and this executor holds none. A `has_more` with no token the model can pass back is worse than
silence: `decorateContentWithPagination` would render "pass the continuation token from this call's metadata to
continue" (`result_hint.go:85`) for a token that does not exist. `query_neighbors` therefore stays unpaginated and
reports width through `truncated` + `frontier_remaining` + `HintTooLarge`, narrowed by `depth` and `filter_type`.
Server-held traversal state is a different design with a different owner; it is not proposed here.

## Tier 1 shape

`KVGetter` and `NewGraphQueryExecutor` are unchanged. New exported optional interface
`KVKeyLister { KeysByPattern(ctx context.Context, pattern string) ([]string, error) }`; the adapter in
`register_graph_query.go` implements it via `natsclient.FilteredKeys`; `queryByType` type-asserts and returns
`ToolErrorInternal` naming the binding when absent — loud, never silent. `ListTools` stays five.

## Result shapes

### `query_relationships`

Before: `{entity_id, relationships:[{type,source,target}], count, direction, filter_type?}`. After:

```json
{"entity_id":"…","direction":"outgoing","filter_type":"agent.lineage.parent","filter_registered":false,
 "relationships":[],"count":0,
 "predicates_present":{
   "agent.lineage.parents":{"kind":"relationship","registered":true,"description":"…","role":"unspecified","inverse_of":""},
   "sensor.temperature.celsius":{"kind":"property","registered":true,"description":"…"}}}
```

plus `ResultHint: "empty"`. The absence-vs-nonexistent path:

| Case | Answer |
|---|---|
| `direction` explicitly `incoming` or `both` | `invalid_args` naming the incoming owner, no read (owner ruling Q4) |
| `direction` omitted | served as `outgoing` and echoed in the result |
| filter not `domain.category.property` | `invalid_args`, no read |
| present as a relationship | rows |
| present only as a property | empty + `kind: property` visible in `predicates_present` |
| registered and absent | empty, `filter_registered: true` |
| unregistered and absent | empty, `filter_registered: false` — a report of THIS process's registry, never an authorization or existence verdict: `PredicateAuthority.Authorize` (`vocabulary/namespace_authority.go:101-118`) legitimately admits an unregistered predicate under an exact `domain`/`domain.category` delegation, so a delegated-namespace predicate reads `false` here and is authoritative on the graph. The description says so |
| entity missing | `not_found` (unchanged) |

Read filter vs `openspec/specs/predicate-contract/spec.md:160-162`: that requirement binds what a tool WRITES; this
tool writes nothing (0 `Publish`/`Put`/`graph.mutation` sites in the file), declares `read_only`, and its parameter
name stays outside the predicate-authority audit's substring set (inventory addition 5).

### `query_by_type`

Before: `{entity_type, limit, entities:[], count:0, note, suggested_ids:[]}`. After:

```json
{"entity_type":"temperature","pattern":"*.*.*.*.temperature.*","limit":5,"matched":12,
 "entity_ids":["…","…","…","…","…"],"count":5}
```

with `Metadata: {"has_more": true, "next_cursor": "<opaque>"}` and `ResultHint: "too_large"` when the page does not
exhaust the match; `Metadata: {"has_more": false}` and `ResultHint: "empty"` when `matched == 0`. There is no
`truncated` field — continuation is the contract's job (§ Continuation). One to three tokens are validated as
canonical segments and the built pattern by `ValidateEntityIDPattern` before any listing, and `cursor` is decoded
before any listing too: a token that does not decode is `invalid_args`.

### `query_neighbors`

Before: `{source_entity, neighbors:{id:record}, count, depth, filter_type?}`. After adds `unresolved:[…]`,
`truncated`, `frontier_remaining`, and sets `HintTooLarge` on truncation / `HintEmpty` on zero. Expansion stops when
the next record would exceed the byte budget; the frontier is drained only through `IsRelationship()` targets. It
sets no `has_more` and does not declare `Paginated` — the refusal and its reason are in § Continuation. `unresolved`
is the traversal spelling of "asked for and not readable"; `query_entities`' existing `not_found`
(`graph_query.go:265,291-292,309`) keeps its name for caller-supplied IDs, and the vocabulary row in
`inventory-verification.md` records the third spelling (`graph.MissingReason`, ADR-084) and why neither is renamed.

## Invariants (each cited to the delta requirement)

- **R1** (`query_relationships`): `count == len(relationships)`; every relationship predicate appears in
  `predicates_present` with `kind: relationship`; `ResultHint == empty ⇔ count == 0 ∧ Error == ""`; a non-canonical
  filter never reaches the scan; `filter_registered == (GetPredicateMetadata(f) != nil)`.
- **R2** (`query_by_type`): every returned ID satisfies `MatchEntityIDPattern(pattern, id)`; the returned IDs are
  sorted and strictly increasing; a token count outside 1–3, a non-canonical segment, or an undecodable `cursor`
  yields `invalid_args` and zero lister calls; `has_more` is present on EVERY successful result;
  `has_more ⇔ next_cursor != "" ⇔ HintTooLarge`; `HintEmpty ⇔ matched == 0 ⇒ ¬has_more`; every ID on a page reached
  through `next_cursor` sorts strictly after every ID on the page that issued it, so over one unchanged key set the
  pages partition the match exactly once; `matched` is the whole match count on every page, never the remainder.
- **R3** (`query_neighbors`): `unresolved ∩ keys(neighbors) = ∅`; every neighbor is the object of an
  `IsRelationship()` triple on a visited entity; `filter_type` keeps exactly the identities for which
  `MatchEntityIDPattern(pattern, id)` is true; `truncated ⇔ frontier_remaining > 0 ⇔ HintTooLarge`;
  Σ neighbor record bytes ≤ budget; the result never carries `has_more` (the delta's neighbors requirement: no result
  announces more without a token the caller can pass back).
- **R4** (`query_relationships` direction, the delta's second requirement): an omitted `direction` answers as
  `outgoing` and echoes it; an explicit `incoming` or `both` yields `invalid_args` naming the incoming owner and
  performs zero reads; no result is ever produced for a direction the record cannot carry.

## `direction` narrows to what the record can carry (owner ruling Q4)

`direction=incoming|both` is structurally empty when read from the record (records are own-subject only; inventory
addition 6). The owner ruled the enum narrowed **in this change**, not deferred: the advertised enum becomes
`["outgoing"]`; an omitted `direction` is served as `outgoing` and echoed (today's default is `both`,
`graph_query.go:325`); an explicit `incoming` or `both` returns `ToolErrorInvalidArgs` whose message names the incoming
owner — the `graph.query.relationships` operation over INCOMING_INDEX (`processor/graph-query/query.go:53`).
`HintEmpty` on `incoming` would have meant "nothing exists" when the truth is "this tool cannot see it".

The framework's own asynchronous reader made the same call on the same fact: `research-graph-execute`'s
`PredicateWalk` sends `Direction: "outgoing"` only and records incoming as a Phase 2 extension
(`processor/research-graph-execute/adapters.go:156-159,183-185`).

Adopter impact, measured: no in-tree caller passes `direction` to this tool. `git grep -n '"direction"' -- configs/
test/` returns `configs/domains/{iot,logistics,robotics}.json:58` (natural-language query examples in a domain pack)
and `test/e2e/scenarios/tiered_structural.go:1405` (the GraphQL gateway's `RelationshipDirection`), neither of which
reaches this executor. The consumer that changes is the model; the migration doc (`tasks.md` 5.1) carries the row the
owner asked for.

## Budget: a model-facing content cap, not the transport bound

Two bound classes exist on this component and the delta names which one `query_neighbors` joins:

| Class | Where | Who owns the number | How it is observed |
|---|---|---|---|
| Transport bound | `openspec/specs/agentic-tools/spec.md:467-475` — the component attempts the full record, and a typed oversize rejection yields one compact `too_large` authority; `:473` "SHALL NOT inspect configured payload limits" | NATS (`max_payload`); the framework never reads it | by attempting the real Create |
| Model-facing content cap | `executors/bash.go:36` `bashMaxOutputBytes = 100 * 1024`; `executors/httprequest.go:23` `httpMaxTextSize = 20000`; this change adds the neighbors cap at **64KB** | the executor, as a constant | by measuring the real bytes of real content while assembling |

The neighbors budget is the second class. It is not a read of a framework-owned limit and does not predict the
transport outcome: assembly fetches real records, counts their real bytes, and stops before the next one would cross
the cap — `truncated`, `frontier_remaining`, `HintTooLarge` report what was observed. A result under the executor cap
that still trips the transport bound takes spec `:467`'s path unchanged; the two compose, they do not overlap. The
origin case in `docs/concepts/24-tool-result-hints-and-pagination.md:8-9` (a 102KB graph result retried three times by
mid-tier models) is this class: the failure was model-facing, not transport. Neither existing cap is specced; the
delta's `query_neighbors` requirement is the first to state the class. External support for the class: the Cekikj
restatement's evidence review (Part 2 § 2.5) cites Microsoft Research's tool-space interference work — over-long tool
responses cut performance by up to 91% even inside the context window. Adopter seam: nothing to configure.

**The value is 64KB (owner ruling Q1).** The recorded failure in `docs/concepts/24:8-9` was 102KB, so a cap 2% under
it leaves no headroom for the hint preamble and the pagination line the loop appends, and a neighbor map is dense JSON
the model must parse rather than stdout it skims. The roster's own practice for graph content reaching a small model
is smaller still — `read_loop_result` pages a STORED result at a 4KB default
(`processor/agentic-tools/loop_result.go:27`, `defaultReadLoopResultChunk`) — but store-and-page is unavailable to a
ReadOnly tool, because a KV write would make it Mutating under ADR-089's worst-effect rule (`agentic/tools.go:61-62`).
An inline cap is therefore the right instrument, and 64KB is the generous end of the roster's practice rather than the
loose end. One comment at the constant names its sibling `bashMaxOutputBytes`, so two caps in one package are
explained where they are read.

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
   catalog-reader adapter. Fixed-position wildcards are not new to the tree — they are `KeysByFilter`'s documented
   purpose (`natsclient/kv.go:528-530`, ADR-102 canonical order), already built by
   `processor/graph-index/predicate_index.go:26-30` and consumed at `processor/graph-index/query.go:428,567` and
   `incoming_index.go:55`, with real-NATS coverage at `processor/graph-index/owner_filter_integration_test.go:139-148`.
   What is new is narrower: the first caller of the package-level `natsclient.FilteredKeys` helper to pass a
   fixed-position pattern — all eight existing non-test callers pass a prefix form (`graph/inference/storage.go:312,560`,
   `graph/clustering/storage.go:244`, `processor/agentic-loop/trajectory_reader.go:80`,
   `processor/graph-clustering/anomaly.go:123`, `component.go:2189`, `query.go:321`,
   `processor/graph-index-temporal/query.go:75`). The precedent to mirror is that test's ctx-expiry boundary, which
   `FilteredKeys` shares (inventory addition 1: rejects partial lists on ctx expiry).
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
| `query_by_type` | `entity_ids`, `pattern`, `matched`, hints; `Paginated: true` with a new `cursor` argument and `has_more`/`next_cursor` in `Metadata` | stub `{entities:[], note, suggested_ids}` → served listing; a non-segment `entity_type` → `invalid_args` where the stub accepted anything | nothing pins the stub shape (0 hits for `suggested_ids` outside `graph_query.go`); the agentic e2e approval walk asserts `status="success"` on the executions metric with pinned args `{"entity_type":"temperature","limit":5}` (`test/e2e/mock/cmd/main.go:38`; `test/e2e/scenarios/agentic/approval_signal.go:36-40,77-88`) — a canonical one-token segment the served tool accepts, and one that matches NOTHING in this tier: the agentic tier writes only loop-execution and model-endpoint entities to ENTITY_STATES (`scenario.go:884-887`), the sensor ID is a `query_entity` argument (`scenario.go:362`), so the served tool answers `matched: 0` + `HintEmpty` with `status=success` and the walk stays green either way |
| `query_relationships` | `filter_registered`, `predicates_present`, `HintEmpty` | rows are `IsRelationship()` triples only — literal-object triples reported as relationships today (`:591-617`) disappear; a malformed filter → `invalid_args` where today `count: 0`; `direction: incoming\|both` → `invalid_args` where today it silently returns `count: 0`, and an omitted `direction` answers `outgoing` where today it answers `both` (owner ruling Q4) | the model; prompt text names the tool, never a result key (`processor/agentic-loop/prompt/assembler.go:142`; `configs/personas/fragments/ops/00-identity.md:9`; `configs/flows/ops-agent.json:316`, an `allowed_tools` entry) |
| `query_neighbors` | `unresolved`, `truncated`, `frontier_remaining`, hints | `filter_type` filters (today ignored, `:442`) — a caller passing it gets a smaller set, possibly empty; the budget truncates where today unbounded; a transient fetch error fails the call where today it is skipped (`:428-431`) | the model; same prompt-text finding |
| `query_entity`, `query_entities` | — | none | — |

No sister calls the executor directly (inventory addition 8); semteams routes all seven graph tools to ops roles only
(`semteams/docs/adr/041-mvp-role-compression-and-graph-as-substrate.md:874-890`, read-only); semsource registers
none of them (`semsource/processor/mcp-gateway/component.go:119-122`, read-only). The served `query_by_type` is
reachable in the agentic tier through the existing adapter: `graphQueryKVAdapter.bind` returns a
`graph.CatalogReader` (`register_graph_query.go:65-71`), which carries `ListKeysFiltered` (`graph/kvcatalog.go:261`),
so the loud no-lister path (`TestQueryByType_WithoutKeyListerIsLoud`) cannot fire there.

**Classification: `feat(agentic-tools)!:` (owner ruling Q9).** FOUR model-facing behaviours flip — relationship rows
filtered to `IsRelationship()` triples, `filter_type` honoured, the neighbors budget, and the `direction` narrowing —
and each can silently shrink or newly refuse a result set a deployment reads today. The `!` costs nothing, because
`task e2e:agentic` green is required either way (`tasks.md` 6.2), and it is what makes the migration doc get read
(owner ruling Q5: `docs/operations/migration-graph-read-tools.md`, before/after JSON for all four). The changelog line
names **model-facing result shapes, not the Go surface**: the Go surface stays additive and `task api:compat` measures
that separately (`tasks.md` 6.1). Recorded cost of the label: it adds to the beta.165 break count the tag-range memo
will report. The architect's earlier recommendation (no `!`) is withdrawn.

**Sequencing (owner ruling Q11: HOLD relaxed to archive-order coordination).** The held set is a MOVING TARGET and
this section states a measurement, never a remembered number — it has now been wrong twice by quoting one. Measure it
(`gh api repos/:owner/:repo/pulls/N/files --paginate`; `gh pr view --json files` caps at 100 and PR #1159 is past
that). Re-measured **2026-09-09** before 3.1: #1156 holds 54 paths, #1159 **181**, #1141 7 — **224 unique**. It read
180 on 2026-09-07 (#1159 at 137) and a stale 176 at round 2; the growth is #1159's alone.
The implementation's file set — `executors/graph_query.go`, `executors/register_graph_query.go`, their `_test.go`
siblings, `docs/operations/migration-graph-read-tools.md`, this change directory, and task 4.5's two files
`test/e2e/mock/cmd/main.go` and `test/e2e/scenarios/agentic/approval_signal.go` — intersects none of them (`comm -12`
over the sorted lists: empty). Two shared things remain, neither a file conflict:

- `openspec/specs/agentic-tools/spec.md` — Codex's `agentic-loop-restart-safety` delta MODIFIES `:435/:467/:487`;
  this delta is ADDED-only. Whichever archives second rebases its delta on the other's spec text: archive-order
  coordination.
- the agentic e2e tier — #1156 holds `test/e2e/scenarios/agentic/scenario.go` and `approval_signal_test.go`, while
  task 4.5 edits `approval_signal.go`, which nothing holds: the same walk in two files, so it is a **same-function,
  not same-file, coordination point** — a rebase resolves textually while the assertions must still agree. The walk
  also changes what it observes (stub success → served success). Whichever lands second runs `task e2e:agentic` on
  its rebase; that is the same gate both already carry.

The owner relaxed the HOLD to exactly that coordination (ruling Q11), with the concrete rule: rebase on `main` after
each Codex stack merge, and `task e2e:agentic` green before this PR's own merge. Recorded cost: two ADDED deltas
landing on one spec and one shared e2e tier in the same window. Milestone is `v1.0.0-beta.165` (owner ruling Q10, on
#1261, #1260 and PR #1262), with the caveat ruled beside it: the **tag range** decides what ships — if this merges
before the beta.163 tag is cut it ships in beta.163 whatever the milestone says, and it is re-homed at tag time.

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
| § 2.5 — over-long tool responses cut performance by up to 91% inside the window; Chroma's context-rot result across 18 models | result size | § Budget (model-facing cap, ruled 64KB); `HintTooLarge` + `truncated` + `frontier_remaining` on `query_neighbors`; identities-only `query_by_type` (owner ruling Q2); the `query_by_type` page bounded by `limit` with continuation instead of a wider single result (owner ruling Q12) |
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
| `find_nodes`, id half (`needle in node_id.lower()`) | served `query_by_type` (type segment); adjacent owner `graph.ingest.query.suffix` over `ENTITY_SUFFIX_INDEX` (trailing segments; graph-ingest IS in the agentic tier) | partly covered by this change; the suffix responder is a candidate for the rest, not adopted (owner ruling Q8: no framework substring tool) |
| `find_nodes`, props half (substring over property values) | none — `byName` is exact over NAME_INDEX and fusionnats-only (`processor/graph-query/query.go:64`); `searchGraph`/`localSearch` are semantic/statistical and tier-dependent (`:63,65`); `prefix` needs leading segments (`:56`); `summary` is a type distribution (`:62`) | **gap** |

What governs filling it: `openspec/specs/agentic-tools/spec.md:267-291` — the framework SHALL NOT supply
`search_graph`/`summarize_graph`; an application MAY register a component-local executor through the general
extension seam, subject to the allowlist, per-loop advertised set, and approval; the framework adds no alias or
special behaviour. The live precedent is semsource's `graph_search` (`mcp-gateway/component.go:112-125`, read-only).
The owner ruled **no** framework substring tool (Q8): an O(N) value scan the framework cannot bound, ADR-036's width
cost, and `:267-291` already names app-local registration as the fill (semsource's `graph_search`). The case against
is recorded rather than dropped: the cited experiment's whole condition rested on `find_nodes`, so its preference
finding is not reproducible on the framework's five tools alone — a product-boundary fact, not a gap. The props-half
row above stays a measured gap with a sanctioned route, not a task.

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
- `TestQueryByType_ListsIDsByTypeSegment` — one-token, two-token, three-token, sorted output, empty+hint.
- `TestQueryByType_SortsUnsortedListerOutput` — the mock lister returns keys in scan order and the result is sorted
  (`natsclient.FilteredKeys` does not sort: `natsclient/kv.go:582-600`).
- `TestQueryByType_PageOneSetsHasMoreAndCursor` — a match wider than `limit` sets `has_more`, `next_cursor`, and
  `HintTooLarge` together, and the content carries no `truncated` field.
- `TestQueryByType_CursorContinuesWithoutRepeats` — page 2 from `next_cursor` returns only IDs sorting after the
  cursor; the last page sets `has_more: false` with no `next_cursor`; the union of pages equals the sorted match set.
- `TestQueryByType_RejectsUndecodableCursor` — asserts the mock lister was never called.
- `TestQueryByType_RejectsNonSegmentTokens` — `*`, `>`, empty, four tokens; asserts the mock lister was never called.
- `TestQueryByType_WithoutKeyListerIsLoud`.
- `TestQueryNeighbors_FilterTypeReadsIDSegment`.
- `TestQueryNeighbors_BudgetTruncatesWithHint`.
- `TestQueryNeighbors_UnresolvedTargetsAreReported` — not-found → `unresolved`; transient → `ToolErrorNetwork`.
- `TestQueryNeighbors_NeverAnnouncesContinuation` — a truncated neighbors result carries no `has_more` key.
- `TestQueryRelationships_UnservedDirectionIsRefused` — `incoming` and `both` are `invalid_args` naming the incoming
  owner and read nothing; an omitted `direction` answers `outgoing` and echoes it.
- Property (rapid): pattern construction from any one, two, or three canonical segments validates; any injected
  wildcard is refused; and for any sorted key set and any page size, cursor-continued pages partition that set exactly
  once (R2).
- `-tags=integration`: `TestIntegration_QueryByType_ListsFromEntityStates` against real NATS via the catalog-reader
  adapter, in `register_graph_query_integration_test.go` — asserting sorted output, one cursor continuation across two
  pages, and the cancelled-context rejection the precedent asserts
  (`processor/graph-index/owner_filter_integration_test.go:139-148`).
- Fails-without-fix: revert the `IsRelationship` filter and the segment match separately; each reds its named test.
- `predicate_authority_contract_test.go` stays green unchanged.
- E2E: `task e2e:agentic` is the standing proof for the served `query_by_type` (inventory addition 4). The #1117
  small-model `e2e:semantic` variant is NOT a standing proof — nothing in that tier calls these tools.
