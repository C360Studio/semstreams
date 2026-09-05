# Inventory verification — graph-read-tools-signal-absence (#1261)

Architect verification pass over the explorer inventory (the collapsed comment on #1261, explorer base `5b7c3db3`).
Design base `main@797d294a`. **Status: awaiting independent inventory review and the owner's INVENTORY PASS.** The
proposal, design, tasks, and delta beside this file are drafts conditional on that pass; nothing is approved.

**Drift check.** `git diff --stat 5b7c3db3..797d294a` touches only `processor/rule/*`, `openspec/specs/graph-ingest/spec.md`,
and the archived change `2026-09-04-foreign-firing-skip-reason` — no explorer-pinned file changed, so every pin holds
by construction. The spot-checks below re-read the line text the design rests on.

## Spot-checks

| Explorer claim | `file:line` @797d294a | Verdict |
|---|---|---|
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
6. **Records are own-subject only** (`graph/entity_predicate_contract.go:147-150`;
   `processor/graph-ingest/canonical_mutations.go:242`; `processor/graph-ingest/component.go:2585`) →
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

## Strikes

- The "coexist on disk" claim (spot-check row 4).
- Explorer §4's `filter_type` row (spot-check row 5).
