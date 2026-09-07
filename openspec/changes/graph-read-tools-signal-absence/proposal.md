# Change: Direct graph-read tools signal absence, serve the type segment, and observe their bounds

Closes #1261. Claim: PR on `claude/gh1261-graph-read-tools`, own worktree. Premises pinned at `main@797d294a` in
`inventory-verification.md`. **Design status: DRAFT. The owner ruled questions 1–12 and gates G1/G2 on 2026-09-07
(#1261 comment 7) and this revision applies them; the INVENTORY PASS itself is NOT given — ruling G2 defers it until
after the closure-only round 5.** Milestone: `v1.0.0-beta.165` (owner ruling Q10), placed on #1261, #1260 and
PR #1262; the tag range decides what actually ships.

**Sequencing:** the HOLD is relaxed to archive-order coordination (owner ruling Q11): rebase on `main` after each
Codex stack merge, `task e2e:agentic` green before this PR's own merge. Sections 3–6 still wait on the INVENTORY PASS
(`tasks.md` 1.6). The implementation's file set — including task 4.5's two e2e files — intersects none of the **180**
unique paths Codex's #759/#1146 stack (PRs #1156/#1159/#1141) holds (54 + 137 + 7, re-measured 2026-09-07 with
`gh api ... --paginate`; `gh pr view --json files` caps at 100), and the delta is ADDED-only because Codex's pending
`agentic-tools` delta MODIFIES `openspec/specs/agentic-tools/spec.md:435`, `:467`, and `:487`. The owner's 2026-09-05
note (on #1261) reads this as a break wanted sooner, and ruling Q9 makes it `feat(agentic-tools)!:`; `design.md`
§ Break classification carries the analysis.

## Why

The five direct graph-read tools in `processor/agentic-tools/executors/graph_query.go` predate the vocabulary
registry, the `ResultHint` contract (`docs/concepts/24-tool-result-hints-and-pagination.md`), and ADR-102's type
segment, and never rejoined them. Measured at `797d294a`:

- a filtered `query_relationships` returns `count: 0` for unregistered, absent, typo'd, and present-as-property alike
  (`:595`) — the blind spot the Cekikj experiment counted 112 wasted calls on;
- every triple is reported as a relationship regardless of `message.Triple.IsRelationship()` (`:591-617`;
  `message/triple.go:133-147` unused by this file);
- `query_by_type` is a stub (`:531-540`) — advertised, never served (`class:advertised-absent`, the #1239/#1255 shape);
- `filter_type` on `query_neighbors` never filters: it compares a `type` key the authority never writes (`:442`) — a
  fifth advertised-absent found by the verification pass, not in the issue's four;
- `query_neighbors` has no width bound and drops missing targets silently (`:428-431`).

`HintEmpty`/`HintTooLarge` have zero production producers today (inventory addition 2): this change is the hint
contract's first adopter.

## What changes (per finding)

1. **`query_relationships`:** `direction` narrows to the one the record can carry (owner ruling Q4) — an omitted
   `direction` answers `outgoing` and echoes it, an explicit `incoming` or `both` is `ToolErrorInvalidArgs` naming
   `graph.query.relationships` over INCOMING_INDEX as the incoming owner; a malformed filter → `ToolErrorInvalidArgs` before any read; an empty result →
   `ResultHint: HintEmpty` plus fields `filter_registered` and `predicates_present` (every predicate on the entity with
   `kind: relationship|property` and the registry's `description`/`role`/`inverse_of` when registered). Relationships
   are selected by `message.Triple.IsRelationship()`; the dead `relationships` reader branch is deleted.
2. **`query_by_type`:** served, not retired — identities listed by the ADR-102 type segment through
   `natsclient.FilteredKeys` on the existing catalog-reader seam (inventory addition 1); `entity_type` is one to
   three right-anchored tokens (`*.*.*.*.<type>.*`, `*.*.*.<domain>.<type>.*`, `*.*.<system>.<domain>.<type>.*` —
   owner ruling Q6), validated by `pkg/types.ValidateEntityIDPattern` and matched
   per key by `pkg/types.MatchEntityIDPattern` (the existing exact-six-position matcher; no new type extractor —
   `graphrag.filterEntityIDsByType`'s case folding and empty-set widening are deliberately not reused, `design.md`);
   sorted by the executor, because `FilteredKeys` returns KV-scan order (`natsclient/kv.go:582-600`); `limit`
   honored; `HintEmpty` when none. Continuation uses the framework's pagination contract rather than a bespoke flag
   (owner ruling Q12): `Paginated: true`, a `cursor` argument, `has_more` on every successful result, an opaque
   `next_cursor` in `Metadata` through the graph package's existing cursor codec, and `HintTooLarge` still set on a
   page that leaves matches behind. No new index, no new bucket.
3. **Schema read:** no new tool. `predicates_present` on `query_relationships` is the experiment's `describe_edges`
   scoped to the entity in hand; the graph-wide predicate catalog already has an owner — `graph.index.query.predicateList`
   (`processor/graph-index/query.go:59,507-545`; consumed by the gateway's `predicates` field and by `graph.query.summary`) —
   and open questions are `research_graph`'s job (`frameworkcapabilities/graphresearch/executor.go:151-156`). ADR-036
   call and the case against it are in `design.md`.
4. **`query_neighbors`:** a 64KB content byte budget observed while assembling (owner ruling Q1); `truncated`,
   `frontier_remaining`, `HintTooLarge` — and deliberately NO `has_more`, because a traversal frontier is not a
   resumable position (owner ruling Q12, refusal reason recorded in `design.md` § Continuation);
   missing targets in `unresolved` (inherits `openspec/specs/graph-query/spec.md:255-263`); transient
   fetch errors fail the call as `query_entity` does; `filter_type` matches the identity's type segment with the same
   grammar as `entity_type`, through `MatchEntityIDPattern`.

No new hint value is needed; all four findings fit `HintEmpty`/`HintTooLarge` plus result fields. The **pagination
half** of the same contract is adopted rather than re-spelled: `query_by_type` sets the existing
`agentic.MetadataKey{HasMore,NextCursor}` and declares `ToolDefinition.Paginated`, which the loop already renders
(`processor/agentic-loop/result_hint.go:69-88`, called at `handlers.go:2639`). `agentic/tools.go` and
`processor/agentic-loop/result_hint.go` are still untouched — consumed as they stand, and now considered rather than
merely unmentioned (round-4 BLOCKING).

## Adopter seam inventory

The adopter is a component or persona author outside this repo, and the model itself as the reader of these results.

- **What must they know?** Nothing new to call. Model-facing: the `entity_type` grammar (one to three right-anchored
  tokens) and the `relationship_type` grammar (`domain.category.property`) — both stated in the parameter descriptions
  and both enforced with a typed `invalid_args` rather than a silent zero. Two facts; the debt is named in the
  descriptions and enforced at runtime. The cursor is deliberately NOT a third fact: it is opaque, the framework
  renders "pass cursor=… to continue" into the model's next message, and the model echoes the token back without ever
  computing it.
- **What happens if they do nothing?** Today a filtered zero is indistinguishable from a typo, `filter_type` is
  ignored, and `query_by_type` returns nothing forever. After: every empty result names why, every truncation names
  itself, every listing shows the pattern it matched.
- **Where do they find out?** Typed runtime error (`invalid_args`) > result field (`filter_registered`,
  `predicates_present`, `pattern`, `truncated`) > the hint preamble the framework renders. Nothing lands at "doc only".
- **What SHOULD they have to know?** Nothing about which predicate names or type tokens exist — the result shows them.
  Observation over prediction: the model observes `predicates_present` instead of predicting a name; the tool observes
  the real key set and the real byte count instead of a knob; and the continuation token is OBSERVED from the previous
  page rather than predicted from a position the model would have to count. The one knob kept is the existing `limit`;
  the budget and the cursor format belong to the framework, not the caller.

## Non-goals

- No new tool; no new index or bucket; no `PredicateMetadata` fields; no MCP.
- No change to `query_entities` (#839 owns its bound).
- `direction=incoming` is not re-homed — it is refused, naming its owner (owner ruling Q4).
- No type-listing entry ("which types exist"): filed as #1265 (owner ruling Q7). No substring `find_nodes` tool
  (owner ruling Q8).
- No new `ToolResultHint` value and no edit to `agentic/tools.go` or `processor/agentic-loop/result_hint.go`; the
  pagination contract is consumed exactly as it stands (owner ruling Q12).
- The three prose-absence executors enumerated by the round-4 adoption sweep (`web_search`, `list_personas`,
  `list_rules`) are NOT migrated here — enumeration only.
- Graph-shape guidance for adopters is #1260.
- The #1117 small-model `e2e:semantic` variant is NOT a standing proof for these tools: nothing in that tier calls
  them (0 hits). Recorded as a residual, not filed.

## Consumers at birth

`KVKeyLister` (new exported optional interface in a Tier 1 package): `graphQueryKVAdapter` implements it,
`queryByType` consumes it. `predicates_present`/`filter_registered`/`unresolved`/`frontier_remaining`/`pattern`: the
model via `buildToolMessages`; the agentic e2e approval walk consumes the served `query_by_type` (inventory addition 4
and `tasks.md` 4.5). `has_more`/`next_cursor`: `decorateContentWithPagination`
(`processor/agentic-loop/result_hint.go:69-88`, called at `handlers.go:2639`) reads them today for `read_loop_result`
and reads ours the moment they are set — a present consumer, not a future one. No sister
repo calls the executor directly (addition 8); semdev and semteams reach it through `RegisterBuiltins`.

## Owner rulings (recorded 2026-09-07 on #1261, comment 7, adopting the recommendations in comments 5 and 6)

Questions 1–11 were numbered on 2026-09-05; round 4 added Q12. The owner ruled all twelve plus the two gates in one
sitting. This section is the record, not a request — nothing in it is re-opened by this change.

| # | Ruling | Where it lands |
|---|---|---|
| G1 | Round-4 amendment (task 1.3h) from an Opus session, then ONE closure-only round 5; a new axis found in round 5 is ruled on the inventory as it stands — no round 6 | `tasks.md` 1.3h, 1.3i |
| G2 | INVENTORY PASS is asked for after G1 completes | `tasks.md` 1.4 |
| 1 | Neighbors budget **64KB** | `design.md` § Budget |
| 2 | `query_by_type` returns identities only | § What changes 2; `design.md` § Result shapes |
| 3 | Fold in the `filter_type` and `IsRelationship` fixes | § What changes 1 and 4 |
| 4 | Narrow `direction` to `outgoing` in this change; explicit `incoming` or `both` → `invalid_args` naming the incoming owner; one migration row | `design.md` § `direction`; delta requirement 2; `tasks.md` 5.1 |
| 5 | Migration doc home `docs/operations/migration-graph-read-tools.md` | `tasks.md` 5.1 |
| 6 | Accept three right-anchored tokens | § What changes 2; delta requirement 3 |
| 7 | No type listing here; the typed adapter to `graph.query.summary` filed as **#1265** (`v1.0.0-beta.165`) | `design.md` § ADR-036 call |
| 8 | No substring `find_nodes` tool | `design.md` § Tool-preference premise |
| 9 | `feat(agentic-tools)!:`; the changelog line names model-facing result shapes, not the Go surface | `design.md` § Break classification |
| 10 | Milestone `v1.0.0-beta.165` for #1261 and #1260; the tag range decides what ships | header above |
| 11 | HOLD relaxed to archive-order coordination: rebase after each Codex stack merge, `task e2e:agentic` green before merge | `tasks.md` 1.6 |
| 12 | Adopt the pagination contract on `query_by_type` (`Paginated: true`, `has_more` + opaque `next_cursor` in Metadata, `HintTooLarge` kept, `matched` stays, `truncated` goes); refuse on `query_neighbors` with the reason recorded; no new hint value | `design.md` § Continuation; delta requirements 3 and 4 |

Filed in the same sitting from the same reading, and NOT part of this change: **#1266** (tool-catalog audit — 28
registered tool names, and discovery advertising the whole roster to any loop without `default_tools`).
