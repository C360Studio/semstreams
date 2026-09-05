# Inventory verification — graph-read-tools-signal-absence (#1261)

Architect verification pass over the explorer inventory (the collapsed comment on #1261, explorer base `5b7c3db3`).
Design base `main@797d294a`. **Status: independent review returned INVENTORY CHANGES REQUESTED (PR #1262 comment, 2026-09-05); the rows below close its findings; re-review and the owner's INVENTORY PASS are owed.** The
proposal, design, tasks, and delta beside this file are drafts conditional on that pass; nothing is approved.

**Drift check.** `git diff --stat 5b7c3db3..797d294a` touches only `processor/rule/*`, `openspec/specs/graph-ingest/spec.md`,
and the archived change `2026-09-04-foreign-firing-skip-reason` — no explorer-pinned file changed, so line numbers cannot
have moved; that proves nothing about whether a pin was ever right. The rows below were re-read at `797d294a`. Sampled:
14 hold, 1 wrong (`IsRelationship`, fixed below). The explorer inventory is materialized beside this file as
`inventory.md`; `task inventory:verify` on it mostly cannot parse its table/range format (#1256), but its five MOVED
rows in `message/triple.go` are real pin errors at the explorer's own base.

## Spot-checks

| Explorer claim | `file:line` @797d294a | Verdict |
|---|---|---|
| `IsRelationship` | `message/triple.go:133` — `func (t Triple) IsRelationship() bool` | **FIX**: the explorer pinned `:135` |
| Five tools, `ReadOnly` effect | `processor/agentic-tools/executors/graph_query.go:45,60,76,100,125`; `:47,62,78,102,127` | holds |
| `depth` clamped 1–3; no frontier cap; fetch failures `continue`d | `:405-407`, `:419-460`, `:428-431` | holds |
| `query_by_type` stub returns `entities: []`, `note`, `suggested_ids` | `:531-540` | holds; nothing else pins that shape (0 hits for `suggested_ids` outside the file) |
| `extractRelationships` reads `relationships` AND `triples` — "two on-disk representations coexist" | `:566-588`, `:591-617` | **STRIKE the "on-disk" half**: `graph.EntityState` has no `relationships`/`type` field (`graph/types.go:24-47`), the write gate marshals that struct (`graph/entity_predicate_contract.go:188-193`), graph-ingest has 0 hits for `"relationships"`/`"type"` keys. The branch is a dead reader path |
| `filter_type` "non-match silently skips" | `:441-445` | **DRIFTED**: it compares `entityData["type"]`, a key the authority never writes → the filter never skips; `filter_type` is a fifth `class:advertised-absent` |
| Registry: `Description`, `InverseOf`, `IsSymmetric`, `Role`, `GetPredicateMetadata`, `ListRegisteredPredicates` | `vocabulary/predicates.go:357,404,412,432`; `vocabulary/registry.go:418,433` | holds |
| `rules.go` is the only registry→LLM schema path | `processor/agentic-tools/executors/rules.go:119-133` | holds |
| No `ENTITY_TYPE_INDEX`; agent surface reads no derived index | explorer §3 | holds — but see addition 1 |
| ADR-036 `:237-244`, ADR-102 `:54-55`, spec pins, `docs/concepts/24:233-245`, `/25:29` | re-read | hold |

## Additions (what the explorer's 60 searches did not reach)

1. **The type segment is reachable with no index.** `natsclient.KVStore.KeysByFilter` supports fixed-position wildcards
   and names the ADR-102 pattern in its doc (`natsclient/kv.go:522-547`); `graph.query.prefix` is built on the same
   primitive (`KeysByPrefix` → `KeysByFilter(prefix+">")`, `kv.go:518-520`; `processor/graph-ingest/query.go:290`); the
   seam the tool already binds exposes it (`graph.CatalogReader.ListKeysFiltered`, `graph/kvcatalog.go:261`; helper
   `natsclient.FilteredKeys` `kv.go:558-598`, rejects partial lists on ctx expiry); 13 production callers.
   `pkg/types.ValidateEntityIDPattern` (`pkg/types/entity_id.go:160-164`) validates a six-position literal-or-`*` pattern —
   the guard against a model widening a filter.
2. **`HintEmpty`/`HintTooLarge`/`HintSyntaxError` have zero production producers** (`git grep` over `*.go` minus tests:
   only the declaration `agentic/tools.go:555-573` and `processor/agentic-loop/result_hint.go:26-28`). Consumers:
   `processor/agentic-loop/handlers.go:2638`, `agentic/rule_fields.go:396`. This change is the contract's first adopter,
   not a new pattern — no adoption sweep owed.
3. **Codex's pending `agentic-tools` delta** (`origin/codex/gh1146-agentic-loop-restart`,
   `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md`) MODIFIES **three** requirements —
   `openspec/specs/agentic-tools/spec.md:435`, `:467`, `:487` — not only the durability one. Any delta here must be
   ADDED-only.
4. **`query_by_type` is the agentic e2e tier's approval-gated tool**, asserted with `status="success"` and one-token
   `entity_type: "temperature"` pinned (`test/e2e/scenarios/agentic/approval_signal.go:36-40,77-88,136-140`;
   `test/e2e/mock/cmd/main.go:33-37`). Retiring it reaches `approval_signal_test.go`/`scenario.go`, both in PR #1156's
   file list.
5. **Predicate-authority audit is name-substring based** (`processor/agentic-tools/executors/predicate_authority_contract_test.go:31-36,96-105`);
   `relationship_type` passes; renaming it to anything containing `predicate`/`triple` turns the audit red.
6. **Records are own-subject only** — `processor/graph-ingest/component.go:1752-1757` (`if triple.Subject != entity.ID`
   → `errs.WrapInvalid(... "does not match Graphable entity")`) and `:2604-2612` (`bySubject := make(map[string][]message.Triple`,
   the batch split by subject before commit). An earlier revision cited `normalizeProjection`, which does not exist. →
   `direction=incoming|both` read from the record is structurally empty. The incoming home is `graph.query.relationships`
   over INCOMING_INDEX (`processor/graph-query/query.go:53`) — a second home for the "relationships" fact, preserved by
   `openspec/specs/agentic-tools/spec.md:285-287`.
7. **Tests:** only `query_entity` is unit-tested (`graph_query_test.go:46-244`); its fixture is non-canonical (`:93-97`) —
   a test that reconstructs. Zero e2e hits for `query_relationships`/`query_neighbors` under `test/`.
8. **Tier 1:** `processor/agentic-tools/executors` is frozen (`release/tier1-packages.txt:79`); sisters reach the executor
   only via `RegisterBuiltins` (semdev `internal/boot/boot.go:142`, semteams `cmd/semteams/main.go:291`) — read-only
   observation, no sister mutated.
9. **Sibling caps on this plane:** `bashMaxOutputBytes = 100KB` (`processor/agentic-tools/executors/bash.go:36`),
   `httpMaxTextSize = 20000` (`httprequest.go:23`); both append "[truncated]" strings, neither sets a hint.
10. **Description density:** 217 `WithDescription` across 201 `Register` calls; `WithRole` 0 and `WithInverseOf` 2 in
    `vocabulary/agentic/register.go` — a schema read will be description-rich, role/inverse-poor.
11. Explorer §2 readers missed `agentic/rule_fields.go:396` (`result_hint` reaches rules).

## Rows added after the independent inventory review (2026-09-05; closes both BLOCKING findings)

| Row | Pin @797d294a | Verdict |
|---|---|---|
| **Same-class owner: "IDs by type"** | `graph/query_summary_types.go:6-11` — "overview a caller (LLM agent or external dashboard) gets without knowing any entity IDs up-front"; `:22-44` — `SummaryRequest{IncludePredicates, EntitySampleLimit (default 2000), ExamplesPerType (default 2)}`; `:58-71` — `EntityTypeSummary{Type, Count, Examples}`; `processor/graph-query/query.go:62` — operation `summary`, consumers `graph-gateway`; `processor/graph-query/summary.go:52-70` — served by `graph.PrefixQueryRequest{Prefix: "", Limit: req.EntitySampleLimit}` over NATS to graph-ingest; `:196` — `typeKey := parsed.System + "." + parsed.Domain + "." + parsed.Type` | **ADD** (BLOCKING closed). A *sampled type distribution with example IDs*, bucketed on three segments — not a per-type listing. Complement, not substitute (semsource `processor/mcp-gateway/component.go:119-122`, read-only: "summarize_graph works because its roster has query_by_type") |
| **Same-class: bounds** | `openspec/specs/agentic-tools/spec.md:467-475` — the transport bound; `:470` "The component SHALL NOT inspect configured payload limits or match error text"; `executors/bash.go:36` — `bashMaxOutputBytes = 100 * 1024`; `executors/httprequest.go:23` — `httpMaxTextSize = 20000`; `git grep` of both constants and "content budget" over `openspec/specs/` → 0 | **ADD** (BLOCKING closed). Two classes: the transport bound (specced; observed by attempting) and model-facing content caps (two instances, unspecced). The neighbors budget joins the second — `design.md` § Budget |
| Codex-held files, complete | PR #1159 is **133 files** — `gh pr view --json files` caps at 100; `gh api repos/:owner/:repo/pulls/N/files --paginate` is the full list. Intersection with this change's candidate set: `configs/flows/ops-agent.json` (#1159), `docs/operations/17-tool-call-governance.md` (#1159), `docs/operations/adopter-tool-effect-metadata.md` (#1141), `processor/agentic-tools/README.md` (#1141, #1159) | **ADD** (MEDIUM closed). None of the four needs a change (next row); the files the implementation edits intersect nothing |
| Tool names in model-facing text | `configs/personas/fragments/ops/00-identity.md:9`; `processor/agentic-loop/prompt/assembler.go:142` — "Search the knowledge graph ... (query_entity, query_entities, query_relationships)"; `configs/flows/ops-agent.json:316`; `docs/basics/07-agentic-quickstart.md:141`; `docs/operations/17-tool-call-governance.md:169`; `README.md`/`docs/.../08-agentic-components.md` → 0 hits for the five names | **ADD**. Names only; no line depends on a result key; tool names are unchanged, so none must change. No sister doc shows a tool result (`semdocs .../04-first-flow.md:312` is the gateway `/graph/stats` endpoint) |
| Restriction seams | `processor/agentic-tools/config.go:19` — `AllowedTools []string` (nil/empty allows all); `:94-95`; `metrics.go:156` — `recordToolFiltered`; `agentic/exec_policy.go:63` — `MetadataKeyAdvertisedTools = "agent.tools.advertised"`; `processor/agentic-loop/handlers.go:1684-1689` (stamped from `GetCachedTools`); `processor/agentic-tools/component.go:976-990` (global allowlist, then per-loop set; key present-but-empty fails closed); `docs/operations/17-tool-call-governance.md:161,261` (ADR-039 rules) | **ADD**. Three seams; per-loop = the loop's cached (advertised) definitions, so a persona/flow that lists only graph tools reproduces the cited experiment's condition today |
| Agentic tier composition | `configs/agentic.json` components: agentic-dispatch, objectstore, graph-ingest, rule-processor, agentic-loop, agentic-model, agentic-tools — **no graph-query, no graph-index** | **ADD**. A `query_by_type` served through `graph.query.summary` has no responder in the tier whose approval proof asserts `status="success"` |
| Substring-entry candidates | `processor/graph-query/query.go:64` — `byName` (exact, NAME_INDEX `graph/constants.go:16-19`, consumer fusionnats only); `:63,65` — `searchGraph`/`localSearch` (`graphrag.go:160,126`; semantic/statistical, tier-dependent); `:56` — `prefix` (needs leading segments); app-local precedent semsource `graph_search` (`mcp-gateway/component.go:112-125`, read-only) | **ADD**. None is a case-insensitive substring over properties — the cited experiment's `find_nodes` (`code/graph.py:48-55`: `needle in json.dumps(props).lower() or needle in node_id.lower()`; a grep WITHOUT regex) |
| semteams role split (read-only) | `semteams/docs/adr/041-...md:874-890` — all seven graph tools "Forbidden for chain / Allowed for ops" | **ADD**. In the main adopter the direct tools are an ops-role surface today |

## Strikes

- The "coexist on disk" claim (spot-check row 4).
- Explorer §4's `filter_type` row (spot-check row 5).
