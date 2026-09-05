# Change: Direct graph-read tools signal absence, serve the type segment, and observe their bounds

Closes #1261. Claim: PR on `claude/gh1261-graph-read-tools`, own worktree. Premises pinned at `main@797d294a` in
`inventory-verification.md`. **Design status: DRAFT, conditional on the owner's INVENTORY PASS over
`inventory-verification.md` and on the eleven owner questions below. Nothing here is approved.** Milestone: none yet —
owner places (architect recommendation: `v1.0.0-beta.165`, beside #1212, the same ADR-102 type-segment territory; not
beta.163, the exit-criteria lane).

**Sequencing:** implementation is on HOLD behind Codex's #759/#1146 stack (PRs #1156/#1159/#1141) until owner question 11
is ruled; the implementation's file set intersects none of the 176 unique paths those PRs hold (54 + 133 + 7, paginated — `gh pr view
--json files` caps at 100 and PR #1159 has 133), and the delta is ADDED-only because Codex's pending `agentic-tools`
delta MODIFIES `openspec/specs/agentic-tools/spec.md:435`, `:467`, and `:487`. The owner's 2026-09-05 note (on #1261)
reads this as a break wanted sooner; `design.md` § Break classification answers it.

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

1. **`query_relationships`:** a malformed filter → `ToolErrorInvalidArgs` before any read; an empty result →
   `ResultHint: HintEmpty` plus fields `filter_registered` and `predicates_present` (every predicate on the entity with
   `kind: relationship|property` and the registry's `description`/`role`/`inverse_of` when registered). Relationships
   are selected by `message.Triple.IsRelationship()`; the dead `relationships` reader branch is deleted.
2. **`query_by_type`:** served, not retired — identities listed by the ADR-102 type segment through
   `natsclient.FilteredKeys` on the existing catalog-reader seam (inventory addition 1); `entity_type` is one token
   (`*.*.*.*.<type>.*`) or `domain.type` (`*.*.*.<domain>.<type>.*`), validated by `pkg/types.ValidateEntityIDPattern`;
   sorted; `limit` honored; `HintTooLarge` when truncated, `HintEmpty` when none. No new index, no new bucket.
3. **Schema read:** no new tool. `predicates_present` on `query_relationships` is the experiment's `describe_edges`
   scoped to the entity in hand; the graph-wide predicate catalog already has an owner — `graph.index.query.predicateList`
   (`processor/graph-index/query.go:59,507-545`; consumed by the gateway's `predicates` field and by `graph.query.summary`) —
   and open questions are `research_graph`'s job (`frameworkcapabilities/graphresearch/executor.go:151-156`). ADR-036
   call and the case against it are in `design.md`.
4. **`query_neighbors`:** a content byte budget observed while assembling; `truncated`, `frontier_remaining`,
   `HintTooLarge`; missing targets in `unresolved` (inherits `openspec/specs/graph-query/spec.md:255-263`); transient
   fetch errors fail the call as `query_entity` does; `filter_type` matches the identity's type segment with the same
   grammar as `entity_type`.

No new hint value is needed; all four findings fit `HintEmpty`/`HintTooLarge` plus result fields. `agentic/tools.go`
and `processor/agentic-loop/result_hint.go` are untouched.

## Adopter seam inventory

The adopter is a component or persona author outside this repo, and the model itself as the reader of these results.

- **What must they know?** Nothing new to call. Model-facing: the `entity_type` grammar (one or two tokens) and the
  `relationship_type` grammar (`domain.category.property`) — both stated in the parameter descriptions and both
  enforced with a typed `invalid_args` rather than a silent zero. Two facts; the debt is named in the descriptions and
  enforced at runtime.
- **What happens if they do nothing?** Today a filtered zero is indistinguishable from a typo, `filter_type` is
  ignored, and `query_by_type` returns nothing forever. After: every empty result names why, every truncation names
  itself, every listing shows the pattern it matched.
- **Where do they find out?** Typed runtime error (`invalid_args`) > result field (`filter_registered`,
  `predicates_present`, `pattern`, `truncated`) > the hint preamble the framework renders. Nothing lands at "doc only".
- **What SHOULD they have to know?** Nothing about which predicate names or type tokens exist — the result shows them.
  Observation over prediction: the model observes `predicates_present` instead of predicting a name; the tool observes
  the real key set and the real byte count instead of a knob. The one knob kept is the existing `limit`.

## Non-goals

- No new tool; no new index or bucket; no `PredicateMetadata` fields; no MCP.
- No change to `query_entities` (#839 owns its bound).
- `direction=incoming` is not re-homed (owner question 4).
- Graph-shape guidance for adopters is #1260.
- The #1117 small-model `e2e:semantic` variant is NOT a standing proof for these tools: nothing in that tier calls
  them (0 hits). Recorded as a residual, not filed.

## Consumers at birth

`KVKeyLister` (new exported optional interface in a Tier 1 package): `graphQueryKVAdapter` implements it,
`queryByType` consumes it. `predicates_present`/`filter_registered`/`unresolved`/`truncated`/`pattern`: the model via
`buildToolMessages`; the agentic e2e approval walk consumes the served `query_by_type` (inventory addition 4). No sister
repo calls the executor directly (addition 8); semdev and semteams reach it through `RegisterBuiltins`.

## Owner questions (numbered; each with the recommendation and the strongest case against)

Renumbered 2026-09-05 after the independent inventory review and the owner note on #1261. Rulings are recorded on
#1261; the docket comment there mirrors this list.

1. **Neighbors budget value.** Recommend 100KB (`executors/bash.go:36`, the package precedent) with a comment naming the
   sibling. Against: `docs/concepts/24`'s failure case was 102KB; 64KB is defensible. Value only — the mechanism (a
   model-facing content cap, distinct from the transport bound spec `:467` governs) is stated in `design.md`.
2. **`query_by_type` returns identities only** (hydrate via `query_entities`). Recommend yes. Against: one extra call
   for a small model; but hydrating 100 records is the #839 payload class.
3. **Fold in the `filter_type` and `IsRelationship` fixes** (inventory additions, not in the issue's four). Recommend
   yes: both are required for `unresolved` to be truthful. Against: scope beyond the issue's four.
4. **`direction=incoming|both` is structurally empty from the record** (records are own-subject only). Recommend:
   truthful description now, residual recorded, enum narrowing the owner's own `class:advertised-absent` issue.
   Against: shipping a knowingly empty enum value.
5. **Migration doc home.** Recommend `docs/operations/migration-graph-read-tools.md` (precedent
   `migration-gated-dag-semantic-settlement.md`); `migration-beta162-to-beta163.md` is in PR #1159's file list.
   Against: one more topic doc.
6. **Accept three right-anchored tokens** (`*.*.<system>.<domain>.<type>.*`) so `graph.query.summary`'s
   `system.domain.type` bucket key pastes into `entity_type` as-is. Recommend yes (one grammar, one more arity; the
   delta's R2 gains one sentence and one scenario if accepted — not pre-applied). Against: a third arity for the model
   to hold; summary is unreachable in tiers without graph-query.
7. **"Which types exist" entry** (omitted `entity_type` → a type listing). Two owners exist already, both on
   graph-query and both absent from the agentic tier: `summary` (flat `system.domain.type` buckets with example IDs) and
   `hierarchyStats`/`entityIdHierarchy` (level-by-level drill, `processor/graph-query/query.go:55,518-528`). Recommend
   **no** in this change: either is a NATS dependency on graph-query and needs a typed adapter. Against: cold-start type
   discovery then has no direct-surface home; `research_graph` is async and heavier.
8. **A `find_nodes`-shaped substring tool.** Recommend **no**: re-opens `agentic-tools/spec.md:267-291`, ADR-036 width,
   an O(N) value scan; app-local registration is the sanctioned route (semsource's `graph_search`). Against: the cited
   experiment's whole condition rested on it.
9. **Commit label.** Recommend `feat(agentic-tools):` without `!`, with `task e2e:agentic` green before merge regardless.
   Against: the owner reads it as a break; the `filter_type` behaviour flip could shrink a deployment's neighbor set.
10. **Milestone.** Recommend `v1.0.0-beta.165` ("close declaration-to-admission-to-runtime parity"), landed first in it.
    Against: 29 open there; an own-break tag ships the shapes to semteams' ops role and semsource sooner.
11. **Relax the HOLD** on the #759/#1146 stack to archive-order coordination on the shared spec file. Recommend yes:
    the implementation's file set intersects none of the 176 unique Codex-held paths (paginated). Against: two ADDED deltas
    landing on one spec and one shared e2e tier in the same window.

Stated, not asked: no new hint value is needed; all four findings fit `HintEmpty`/`HintTooLarge` plus result fields.
