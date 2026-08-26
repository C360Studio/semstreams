# gh#1095 — entity-ID segment semantics: impact inventory (read-only architect pass)

**Baseline:** `origin/main` = `5cc0c7fbe569c6398fc534025218639b4c7e0345` (2026-08-26). Every `file:line` below was read at
that SHA. Sister repositories under `/Users/coby/Code/c360/` were read as checked out on the same day and are
point-in-time; re-verify before treating any sister row as current. Nothing in this document is a target state.

**Status (revision 2):** inventory-only deliverable per `.agents/contracts/semstreams-architect.md` §"Required
workflow" step 2. The independent blind inventory pass on revision 1 (commit `b6b4b024`, draft PR #1099) returned
**INVENTORY PASS WITH DIVERGENCES**; divergences D1–D5 are corrected and rows R-A–R-D added in this revision, each
marked `(r2)`. The design that follows it (`gh1095-entity-id-segment-semantics-design.md`), ADR-102, and the spec
deltas have NOT had a pre-owner design review; that review runs after this revision lands.

## 0. Problem statement (measured)

The lexical contract (`openspec/specs/entity-id-contract/spec.md`) fixes arity, alphabet, the 256-byte bound, the
sole validator authority, exact-arity patterns, and the bounded prefix language. It names the six positions twice
(`:6`, `:24`) and assigns meaning to none. The owner ruled on 2026-08-26 (issue #1095 comment) that positions
have meanings, `platform` is the minting deployment authority, source belongs in `system`, `domain` is a delegated
taxonomy, `org.platform` is enforced at graph boundaries unless the write arrives through an import lane with
provenance, the rule read-back is a bug (#1096), and semsource re-slots inside the pre-v1 wave. Reordering is
allowed; arity stays six.

This inventory enumerates everything that depends on arity, order, or a position's meaning, so that the reorder
decision and the break-wave plan are made against a complete list rather than the issue's directed one.

## 1. Second- and third-order impact rows

Columns: **Assumes** = A (arity six), O (order), P(n) (meaning of position n, 1-based). **Reorder** = what changes
if positions 3 and 4 swap (the only reorder the design considers; `instance` stays last, see §1.4).

### 1.1 NATS subject construction from IDs or segments

| # | Site | Assumes | Reorder |
|---|---|---|---|
| S1 | `processor/agentic-loop/component.go:2144` `ResolveSubject(outputs, "agent.complete", loopID)` — the loop entity's **instance** segment is the subject leaf token | P(6) = dot-free leaf identifier | none while instance stays last |
| S2 | `processor/agentic-loop/component.go:1857` `agent.context.compaction` + `event.LoopID` | P(6) | none |
| S3 | Rule-authored subjects (r2): `processor/rule/actions.go:881` (`executePublish`) and `:1865` (`executeApprove`) resolve `action.Subject` through `ec.SubstituteVariables`, so a config-authored subject may carry the **whole ID** — `$entity.id` at `docs/concepts/18-rule-driven-artifacts.md:72` (`output.drone-snapshot.$entity.id`) and `:118` (`output.entity-md.$entity.id`), pinned by `processor/rule/actions_subject_override_test.go:303` — or one segment (`$entity.instance`: `openspec/specs/user-response-subject-ownership/spec.md:51,127`; semdev `configs/rules/coordinator/04-stamp-issue-ref.json:27`) | whole ID → A + O as subject tokens; `$entity.instance` → P(6) | a whole-ID subject reorders its tokens silently; a subscriber matching with position literals (`output.drone-snapshot.c360.*.robotics.>`) keeps matching nothing and reports nothing — the silent-reinterpretation consumer |
| S4 | AGENT_LOOPS key `COMPLETE_<loopID>` (reader `test/e2e/scenarios/research-graph/scenario.go:647,766`; `read_loop_result` contract in `processor/rule/entity_substitution.go:4-10`) | P(6) = bare loop id | none |
| S5 | Byte bound vs key contract: entity ID ≤ 256 bytes (`pkg/types/entity_id.go:14`); KV literal key ≤ 1,024 bytes / 64 tokens (`openspec/specs/nats-kv-keys/spec.md:26-30,80`); worst INCOMING key `2E+390 = 902` bytes / 13 tokens (`entity-id-contract/spec.md:359-388`). No production subject carries a whole entity ID | A | none (byte totals are order-independent) |

Searches: `grep -rn 'Publish[A-Za-z]*\([^)]*(entityID|EntityID\(\)|entity\.ID)' --include='*.go'` (non-test) → 0 production hits; `grep -rn 'ResolveSubject\([^)]*entity'` → 0. Corrected closure (r2): **no shipped Go site or shipped config puts a whole entity ID on a publish subject, but the lane exists** — any rule config may (S3), and those two greps cannot see it. The shipped ID-derived subject tokens are the instance segment (S1, S2, S4).

### 1.2 KV keys

| # | Site | Assumes | Reorder |
|---|---|---|---|
| K1 | `ENTITY_STATES` key = whole ID: `processor/graph-ingest/component.go:1985` (`UpdateWithRetry`), `:2132` (`Create`), `canonical_mutations.go:243`; History 1 `graph/kvcatalog.go:58-67`; no DiscardNew/TTL on graph buckets (`natsclient/kvspec.go:82-90`, `graph/owned_bucket_retention.go`) | A (token count of the key), O for every prefix scan over the bucket | every `KeysByPrefix`/watch filter's *meaning* changes; whole-key ops unchanged |
| K2 | Keyed ingest (ADR-072): lane = `hash(entityID)`; durable guard key `entityID + "/" + stream` (`processor/graph-ingest/keyed_ingest.go:67-69`) | whole ID | none |
| K3 | `ENTITY_SUFFIX_INDEX` keys `instance` and `type.instance` (`processor/graph-ingest/component.go:2614-2626`, writer `:2632-2650`, reader `query.go:494,572-590`) | P(5)+P(6) are the last two positions (`parts[len-2]`, `parts[len-1]`) | positions 5–6 MUST stay `type.instance` |
| K4 | `OUTGOING_INDEX` key = ID (`processor/graph-index/component.go:1694,1801`); embedding index key = ID (`graph/embedding/storage.go:224,273,415,479`); temporal reverse key = ID (`processor/graph-index-temporal/component.go:1144`); anomaly entity index = hash(ID) (`graph/inference/storage.go:706-710`) | whole ID | none |
| K5 | `COMMUNITY_INDEX` `entity.<level>.<entityID>` (`graph/clustering/storage.go:524-525`; `processor/graph-clustering/query.go:284`) and `<level>.<communityID>` (`storage.go:520-521`) where communityID = seed entity ID today, = prefix under ADR-099 | A (7-token key today; 3–5 tokens per level under ADR-099) | level-1 key set changes meaning (§1.5) |
| K6 | ObjectStore keys `content/Y/M/D/H/<entityID>_<ts>`, `binary/Y/M/D/<entityID>_<field>_<ts>` (`storage/objectstore/store.go:669-696`); ID validated first (`store.go:442`; spec `:245-266`) | whole ID inside a slash path | none |

### 1.3 Predicate-index composite keys and sharding (ADR-065 lineage; current layout per ADR-078)

| # | Site | Assumes | Reorder |
|---|---|---|---|
| X1 | `PREDICATE_INDEX` key `predicate3.entity6` — `processor/graph-index/predicate_index.go:12-16`; reader requires exactly 9 tokens and takes `parts[3:]` as the ID (`query.go:734-745`); forward filters `predicate + "." + wildcardPositions(6)` (`predicate_index.go:18-31`) | A | none |
| X2 | `INCOMING_INDEX` key `target6.source6.hex(predicate)` (`incoming_index.go:41-43`); source-owned filter `*.*.*.*.*.*.<source6>.*` (`:56-58`); readers `SplitN(suffix,".",7)` + `Join(parts[:6])` (`:78-82`; `processor/graph-clustering/component.go:2182-2186`; `anomaly.go:137-141`; `test/e2e/client/nats.go:965-974`) | A | none |
| X3 | `NAME_INDEX` key `hash.entity6.hex(predicate)` (`name_index.go:60-62`, reader `:128-132`) | A | none |
| X4 | ADR-065 §Risks (`docs/adr/065:525-533`) records this exact dependency: "assumes entity IDs are always exactly 6 dot-tokens" | A | none |

### 1.4 Exact-arity patterns and the bounded prefix language

Prefix length meanings **today** (`pkg/types/entity_id.go:248-301`): 1 = org; 2 = `org.platform`; 3 = `+domain`;
4 = `+system`; 5 = `+type`; 6 = exact.

| # | Site | Assumes | Reorder |
|---|---|---|---|
| W1 | `pkg/types/entity_id.go:49-66` regexes; `:147-174` pattern validate/match (positional compare `:166-173`); `:176-180` prefix validate (1..6) | A | none (grammar is order-free) |
| W2 | `pkg/types/entity_id.go:248-348` `TypePrefix`/`SystemPrefix`/`DomainPrefix`/`PlatformPrefix`/`IsSibling`/`IsSameSystem`/`IsSameDomain` — **zero production callers** (`grep -rn '\.(TypePrefix\|SystemPrefix\|DomainPrefix\|PlatformPrefix\|IsSibling\|IsSameSystem\|IsSameDomain)(' --include='*.go' \| grep -v _test \| grep -v pkg/types/entity_id.go` → 0); gh606 design P6 (`docs/proposals/gh606-derived-communities-design.md:29`) plans to consume them | O, P(3), P(4) | the 3-part helper's meaning changes (domain → source) |
| W3 | Cut sites: `graph/inference/hierarchy.go:261,268,275,288` (`parts[:5]`, `[:4]`, `[:3]`, `[:5]`); `graph/clustering/entityid_provider.go:214-219` (`[:5]`), `:232-237` (`parts[3]` = system); `processor/graph-query/summary.go:198-201` (`segs[2].segs[3].segs[4]`); `processor/graph-query/graphrag.go:256-271` (`parts[4]`, `parts[5]`; consumed `:1577,1941,1960,2071`); `graph/clustering/summarizer.go:686-700` (all six by index); `agentic/entity_ids.go:161-171` (`parts[2..5]` literal checks); `agentic/agentrun/agentrun.go:158-170` (same); `processor/rule/actions.go:1575-1583` (`idParts[0..1]`); `processor/rule/entity_substitution.go:55-57,73-83` (index → name) | O + P(n) | every indexed read moves or renames |
| W4 | Order-agnostic prefix consumers: `graph/id_prefix.go:26-50` (`MatchesAnyIDPrefix`); `processor/graph-query/query.go:549-586` (`extractNextLevel`, `hierarchyStats` `:470-521`); `processor/graph-ingest/component.go:459-462` (`ListWithPrefix` = `KeysByPrefix(prefix+".")`); `processor/graph-ingest/query.go:247-273`; `processor/agentic-loop/lessonmatch/lessonmatch.go:219-231` (segment-boundary match) | A | meaning of a given length changes; code does not |
| W5 | Declaration patterns in Go (audit: 17 declaration-pattern candidates): `*.*.agent.chain.execution.*` (`agentic/agentrun/agentrun.go:100`), `*.*.agent.agentic-loop.execution.*` and `*.*.agent.lesson.record.*` (`internal/builtinprojection/contracts.go:26,56`), `*.*.gateddag.fanout.instance.*` (`processor/gated-dag/participant.go:17`), `*.*.lifecycle.gcs.mission.*` (`cmd/e2e-semstreams/mission/state.go:28`) | O, P(3..5) | every literal in positions 3–5 rewrites |
| W6 | Config patterns (`grep -rhoE '"(pattern\|entity_id_pattern)"\s*:\s*"[^"]+"' configs`): 33× `*.*.*.*.*.*`; 1× `*.*.agent.lesson.record.*`; watch buckets 6× `["*.*.*.*.*.*"]`, 4× `["c360.*.*.*.*.*"]`, 1× `["*.*.agent.lesson.record.*"]`, 1× `["c360.test.lifecycle.gcs.mission.*"]` | A; 3 files also O | 3 config literals rewrite; 40+ all-wildcard patterns untouched |
| W7 | `pkg/lifecycle/manager_query.go:59,216,293,523-529` (`matchPattern` over workflow `EntityIDPattern`); `pkg/lifecycle/workflow.go` validates the pattern | A | none |
| W8 (r2) | e2e assertions pin positions 3–5 by literal: `test/e2e/scenarios/ops/scenario.go:604` (`parts[2..4] == ops.diagnosis.finding`) and `:712` (`agent.lesson.record`); `test/e2e/client/nats.go:965-974` is arity-only (`SplitN 7`) and unaffected by order | O, P(3..5) | the `e2e:ops` and `e2e:lessons` tiers report a literal mismatch the moment the order changes — these assertions are in the rewrite list (design §D) so the mismatch is not misread as a regression |

### 1.5 ADR-099 partition cut points (unimplemented)

Evidence that ADR-099 is not in the tree: no `openspec/changes/gh606-*` directory; `graph/clustering/lpa.go` and
`entityid_provider.go` present; `openspec/specs/graph-clustering/spec.md:6-12` still requires LPA over the 5-part
type prefix `org.platform.domain.system.type`.

| # | Site | Assumes | Reorder |
|---|---|---|---|
| C1 | `docs/adr/099:25-27`: level 0 = system (4 parts), 1 = domain (3), 2 = platform (2); design table `gh606-derived-communities-design.md:65-71`, partitioner steps `:84-96`, record key `{level}.{prefix}` `:120-124` | O, P(2..4) | 4-part prefix = the SET {org, platform, position 3, position 4} — identical partition under either order, community-ID string reorders; 2-part unchanged; **3-part changes meaning** (domain-community → source-community) |
| C2 | What the forthcoming work needs from prefixes: community identity = the prefix string (KV key tokens: level 0 → 5 tokens, 1 → 4, 2 → 3 — arity-distinct per level); member enumeration through `graph.ingest.query.prefix` (`graph/query_prefix_types.go:44-58`); write-on-change records; overlay re-entry independent of the base | A + O | the design's level-1 semantics must be restated under any reorder |
| C3 (r2) | Sequencing window between the reorder and gh606: `graph/clustering/entityid_provider.go:231-236` `getSystem` takes `parts[3]` as system (rationale `:225-228`) and is live through `NewEntityIDProvider` at `processor/graph-clustering/component.go:1331`; `graph/clustering/summarizer.go:719-731` groups summary-prompt data by `parsed.Domain` (`parseEntityID` by index, `:686-700`); neither has a position test | O, P(3), P(4) | if the reorder lands before gh606 deletes the provider, LPA affinity silently computes on the taxonomy position and the prompt's "domain" groups become source groups — both are named in slice A's rewrite (design §D) and the tag holds until gh606 lands (O-7) |

### 1.6 Hierarchy containers and their padding

| # | Site | Assumes | Reorder |
|---|---|---|---|
| H1 | `graph/inference/hierarchy.go:257-276`: type container = `parts[:5] + ".group"`, system = `parts[:4] + ".group.container"`, domain = `parts[:3] + ".group.container.level"`; `isContainerEntity :129-141` reads `parts[5] ∈ {group, container, level}`; e2e mirror `test/e2e/client/nats.go:1295-1299` | A, O, P(6) reserved literals | the 3-part container becomes a *source* container while its predicate stays `hierarchy.domain.member` |
| H2 | Reserved-token collision (r2): a real entity whose instance is literally `group`, `container`, or `level` is classified as a container (`:129-141`) and **silently skipped** — `GetHierarchyTriples` returns `nil, nil` at `:170-172`, so it receives no membership edges and no warning; a convention with no contract | P(6) | none |
| H3 | 256-byte overflow (r2): a valid 5-part prefix of up to 255 bytes + `.group` (6 bytes) exceeds 256; the container birth at `hierarchy.go:440` (`CreateEntity`) fails `validateEntityID` (`processor/graph-ingest/component.go:1946`), and graph-ingest **warns and drops the membership triples** — `component.go:1970-1976` (merge path) and `:2107-2111` (create path) log `Failed to get hierarchy triples` and persist the entity without them. Not a hard failure: a silent structural gap. ADR-076 d1 (`docs/adr/076:18-22`) solved the same class for alerts with a digest family | A + byte bound | none |
| H4 | Shipped (r2): `enable_hierarchy: true` in 10 of the 12 `configs/*.json` that declare it, out of 16 config files (`configs/agentic.json:182`, `structural.json:633`, …; false in `protocol-flow.json:381`, `lifecycle-flow.json:171`) | — | every shipped tier is on the path |
| H5 | Duplicate spelling: ADR-099 makes `community(entity, level)` the prefix (never stored); containers store the same groups as entities with membership edges. gh606 design `:191-196` names the overlap and puts hierarchy out of that change's scope | — | two homes for one fact |

### 1.7 Lesson scope keys

| # | Site | Assumes | Reorder |
|---|---|---|---|
| L1 | Writer: `processor/agentic-tools/emit_lesson.go:55-57` (`minAppliesToIDSegments`), `:862-885` (`id:` needs ≥ 3 segments); spec `openspec/specs/agentic-lessons/spec.md:78-92` (scenario `id:c360.ops.robotics`) | O, P(3): "3 segments" = deployment + domain today | 3 segments = deployment + **source** |
| L2 | Reader: `lessonmatch.go:187-231` segment-boundary prefix match | A | none |
| L3 | In use (r2 recount in this repo with `grep -rhoE '"id:[a-zA-Z0-9_.-]+"' --include='*.json' --include='*.go'`; the revision-1 counts double-counted a worktree copy): `id:acme.test.agent`×5, `id:c360.ops.robotics`×4, `id:acme.ops.robotics`×2, `id:acme.ops.agent`×2, plus five negatives — 18 occurrences in 5 files (`processor/agentic-tools/emit_lesson_test.go` 8, `processor/agentic-loop/lessonmatch/lessonmatch_test.go` 6, `processor/agentic-loop/lessons_test.go` 2, `emit_lesson_integration_test.go` 1, `vocabulary/agentic/predicates.go` 1); the same grep over every sister → 0 | — | fixtures rewrite |
| L4 | `agentic/agent_lesson_entity.go:85-93` `AgentLessonRecordPrefix(org, platform)` = `org.platform.agent.lesson.record` (5-part) — callers `processor/agentic-loop/handlers.go:721`, `test/e2e/scenarios/lessons/scenario.go:341` | O, P(3..5) | literal rewrites |

### 1.8 Rule substitution

| # | Site | Assumes | Reorder |
|---|---|---|---|
| R1 | `processor/rule/entity_substitution.go:43-57` `entityPartNames = [6]string{org, platform, domain, system, type, instance}` indexed by position; `:73-83` resolver; docs `processor/rule/execution_context.go:211-218`, `docs/operations/migration-beta35-to-beta36.md:38-43` | O ↔ name | the name→index table flips for positions 3–4; token NAMES survive |
| R2 | Consumers: `$entity.instance` — semdev `configs/rules/coordinator/04-stamp-issue-ref.json:27`; `user.response.$entity.instance` (semteams/semdev rules per `user-response-subject-ownership:51,127`). `$entity.platform` / `$entity.domain` / `$entity.system` / `$related.*`: **zero config consumers** in any repo (`grep -rn '\$(entity\|related)\.(org\|platform\|domain\|system\|type)' --include='*.json'` → 0) | P(6) | none for shipped consumers |

### 1.9 Community summary store keys (ADR-087)

| # | Site | Assumes | Reorder |
|---|---|---|---|
| M1 | `COMMUNITY_SUMMARIES` key `{level}.{membership_hash}`; hash = sha256 over sorted member IDs (`docs/adr/087:44-49`; `openspec/specs/graph-query/spec.md:361-370`) | whole IDs | every re-mint changes every hash (fresh state covers it) |

### 1.10 Retention and deletion (ADR-068)

| # | Site | Assumes | Reorder |
|---|---|---|---|
| D1 | Live graph never uses TTL/MaxBytes/MaxAge (`docs/adr/068:52-57`; `natsclient/kv_retention.go:46-94`; `graph/kvcatalog.go`) | — | none |
| D2 | Cleanup filters are arity-shaped: INCOMING source filter (X2); ADR-065 `*.<entity6>` reverse bonus (`docs/adr/065:222-228`; ADR-068 `:264-272`) | A | none |
| D3 | Evidence is not regenerable (ADR-068; `CLAUDE.md` "Evidence is not regenerable"); ADR-076 d6 never rewrites identity; lesson IDs are UUIDv5 over content that includes loop-execution entity IDs (`emit_lesson.go:500-501,655-674`) → identity rewrite is transitive and infeasible; the wave is fresh-state by contract (`entity-id-contract:319-350`) | — | fresh state only |

### 1.11 The federation import lane as it exists

| # | Site | Assumes | What "import lane with provenance" can mean today |
|---|---|---|---|
| F1 | semsource `graph/event_payload.go:21-72`: `EntityPayload` (payload type `semsource.entity.v1`) implements Graphable; IDs built by `entityid.Build(org, "semsource", <content-type>, SystemSlug(root), type, instance)` (`entityid/entityid.go:22-45`, e.g. `handler/git/entities.go:99`) | P(2) = product name | — |
| F2 | graph-ingest consumes it on the Graphable fact lane (`processor/graph-ingest/component.go:1464-1507`); validation is syntax-only (`:1888-1890`); graph-ingest does not read `deps.Platform` (`grep -n Platform processor/graph-ingest/component.go` → 0) | — | the boundary has no notion of authority |
| F3 | Wire envelope metadata = `created_at`, `received_at`, `source` (`message/base_message.go:234-238`); `DefaultFederationMeta.Platform()` is in-process only (`message/federation.go:50-56,78`); `Triple.Source` is a free string (`message/triple.go:58`; semsource sets `"semsource"`) | — | provenance on the wire = two unauthenticated strings |
| F4 | ADR-057 withdrawn (`docs/adr/057:5-10`, no signing); ADR-059 is the LLM-assertion trust tier, not origin (`docs/adr/059:23-33`) | — | no typed provenance exists |
| F5 | `docs/concepts/16-federation.md:32-40` claims the format isolates by design (→ #1097) | — | — |

### 1.12 `cmd/entity-id-audit` and the validator caller set

| # | Site | Finding |
|---|---|---|
| T1 | `internal/entityidaudit/audit.go:212-228` validates each candidate through `pkg/types` only (lexical); surfaces: go-field, go-triple-subject, go-declaration, go-constructor, json, yaml, structured text (`:286-899`) | no segment-semantics rule exists |
| T2 | `task entity-id:audit` (`Taskfile.yml:96-99`) is **not** invoked by `.github/workflows/ci.yml` or `scripts/` (`grep -rn 'entity-id-audit\|entity-id:audit' .github scripts` → 0) | the corpus gate is unwired |
| T3 | Run at `5cc0c7fb`: **30 `entity_id_invalid:arity` unclassified candidates across 1,189** — all in `*_test.go` except `test/e2e/scenarios/agentic/scenario.go:248` (`c360.agentic.sensor.temperature.temp-sensor-001`, a 5-part `query_entity` argument on the live agentic tier) | the "zero-violation corpus" (`entity-id-contract:321-322`) does not hold on main |
| T4 | Validator callers (`ValidateEntityID`/`IsValidEntityID`/`ParseEntityID`/`ValidateEntityIDPrefix`/`ValidateEntityIDPattern`/`MatchEntityIDPattern`, non-test): 49 files — e.g. `processor/graph-clustering/component.go`×6, `graph/events.go`×3, `pkg/projection/mutation_client.go`×3, `processor/graph-ingest/component.go`, `storage/objectstore/store.go`, `vocabulary/export/export.go` | all lexical; none reads a position |

### 1.13 `pkg/types/entity_id.go`

| # | Site | Assumes |
|---|---|---|
| P1 | `:82-94` struct grouped "Federation hierarchy (3): Org, Platform, System / Domain hierarchy (2): Domain, Type / Instance (1)"; `:98-101` `Key()` emits `org.platform.domain.system.type.instance` — the two declared hierarchies interleave on the wire | O |
| P2 | `:121-134` `ParseEntityID` assigns fields by index; `:108-111` `EntityType()` = `{Domain, Type}`; `message/types.go:24` `type EntityID = types.EntityID` (alias, one home) | O |
| P3 | `internal/semantictest/fixtures.go:20-60` builder takes the six positions as positional args in the current order | O |
| P4 | `vocabulary/iris.go:85-97` `EntityIRI("domain.type", pcfg, localID)`; `vocabulary/export/export.go:123` parses IDs for export | P(3), P(5) |
| P5 (r2) | `vocabulary/export/export.go:123-126` `subjectToIRI` emits the external IRI `<base>/entities/{org}/{platform}/{domain}/{system}/{type}/{instance}` in wire order — a published JSON-LD/RDF artifact **outside the graph that fresh state does not re-mint** | O (path order); the IRI path reorders with O-B, or the exporter pins its own order — owner item O-11 |
| P6 (r2) | `processor/graph-query/summary.go:198-202` builds `EntityTypeSummary.Type = segs[2].segs[3].segs[4]` (`domain.system.type`), exposed as an API VALUE through GraphQL `EntityTypeSummary.type` (`gateway/graph-gateway/component.go:1870`); no graph-query requirement pins the value's shape (`grep -n 'entity_types\|EntityTypeSummary\|graphSummary' openspec/specs/graph-query/spec.md` → 0) | O; the value's token order flips under O-B — an API value change for every `graphSummary` consumer, not only an index edit |

### 1.14 Minting sites — framework and sisters

**Framework (all read authority from `deps.Platform` = `types.PlatformMeta{Org, Platform}`, `types/component.go:134-137`, composed at `cmd/semstreams/main.go:477-484` and `cmd/e2e-semstreams/main.go:628-634` from `cfg.Platform.InstanceID` else `.ID`; same precedence in `config/config.go:772-778`; `Validate` lowercases org and requires `id`, `:225-241`; authority reads by token, non-test lines, re-measured in r2 — the revision-1 "96" came from a receiver-letter regex that mixed tokens and is withdrawn: `deps.Platform` = 18 lines (17 `processor/`, 1 `service/component_manager.go:183`; 0 in `config/` or `cmd/`), `platform.{Org,Platform}` (the copied unexported field, e.g. `e.platform.Org`) = 62 lines, `.Platform.{Org,Platform}` on any receiver outside `config/`/`cmd/` = 14 lines):**

| Family | Builder | Shape today (`org.platform.` +) |
|---|---|---|
| loop execution | `agentic/entity_ids.go:71-89` | `agent.agentic-loop.execution.<loopID>` |
| chain execution | `agentic/entity_ids.go:127-145` | `agent.chain.execution.<chainID>` |
| model endpoint | `agentic/entity_ids.go:18-36` | `agent.model-registry.endpoint.<name>` (conventional name) |
| lesson | `agentic/agent_lesson_entity.go:57-73,85-93` | `agent.lesson.record.<uuidv5(content)>` |
| web observation | `agentic/web_observation_entity.go:60-80` | `agent.web.observation.<sha256-16(url)>` |
| ops diagnosis | `agentic/ops_diagnosis_entity.go:45-60` | `ops.diagnosis.finding.<id>` |
| rule alert (ADR-076) | `graph/events.go:19-20,290-301` | fixed `semstreams.framework.graph.rules.alert.<sha256(source, type, rule, component, ts)>` |
| rule trigger (ADR-076) | `processor/rule/graph_event_identity.go:12-37` | fixed `semstreams.framework.graph.rules.trigger.<sha256(packID, ruleID)>` — **no deployment identity in the digest** |
| gated-dag fan-out | `processor/gated-dag/participant.go:17`, `payload.go:16` | `gateddag.fanout.instance.<id>` |
| hierarchy containers | `graph/inference/hierarchy.go:257-276` | `<prefix>` + padding |
| run-scope mint (bug #1096) | `processor/rule/actions.go:1575-1583,1710-1712` | authority read from the **firing entity** |
| e2e mission | `cmd/e2e-semstreams/mission/command.go:60-61` | `lifecycle.gcs.mission.<id>` (org/platform from the wire, `:59-66,326-327`) |
| example | `examples/processors/iot_sensor/payload.go:350` | `ZoneEntityID(orgID, platform, …)` — wire-supplied authority |

**Sisters (minting site → position values):**

| Repo | Authority source | platform value | domain values | system values | instance family |
|---|---|---|---|---|---|
| semsource | org from config namespace (`config/config.go:323-329`); platform is a **constant** `entityid/entityid.go:23` | `semsource` | web, media, config, git, golang, svelte (`handler/*/entities.go`) | `SystemSlug(root)` = repo/host slug (`entityid.go:99-140`) | sanitized slug / sha / hash-bounded (`:38-74`) |
| semdev | config `InstanceID` else `ID` (`internal/boot/runtime.go:257-264`) | `semdev-001` | agent (framework builders), forge (`intake/record.go:56`), repo (`standards/sync.go:174`) | chain, lesson, agentic-loop, intake, standards | uuid, digest12 |
| semboids | config (`cmd/semboids/main.go:382-391`); fallback literal `"semboids"` (`internal/sim/component.go:199-200`) | `semboids-001` | sim (`boidgraph/payload.go:42`, `zone/payload.go:25`) | flock | integer / zone id |
| semdragon | defaults `Org "default"`, `Platform "local"` (`domain/config.go:100-104`; 18 processor configs default `Platform: "local"`) | prod / local / dev | game (`domain/config.go:110-124`, board = system), web (`processor/executor/tools.go:949` `web.agent.doc`) | `<board>`, agent | quest ids, doc ids |
| semteams | config (`cmd/semteams/main.go:760-769`) | instance_id (`bootstrap-001`, `pathrag-001`, …) | agent (framework builders; literal `agent.chain.execution` at `sandboxruntime/attestation_runner.go:124`) | chain, agentic-loop | uuid |
| semmachina | per-world composed config `ID: "semmachina-"+WorldNS` (`internal/boot/components.go:190,201`; `cmd/bellweather-surface-stack/main.go:288`; `testinfra/harness.go:323`) | `semmachina-<world>` | not measured (audit stopped on `web/tsconfig.json`; re-verify) | — | — |
| semops | literal `Platform: "edge"` (`internal/app/config.go:367-516`); `{Org c360, Platform edge, Source mavlink/tak/adsb/sapient}` (`components/fusion/candidates.go:33-36`) | `edge` | cop (`fusion/association/association.go:172`) | fusion | `<a>-to-<b>` |
| semconnect | fixtures `c360.semconnect.systems.csapi.*` (183); `SystemEventIDPrefix` config (`gateway/cs-api/systemevents.go:329-331`) | `semconnect` | systems | csapi | ids / seeds |
| semspec | `workflow.EntityPrefix()` = config org.platform (`workflow/entity_prefix.go:48-52`); 10,368 fixtures import semsource IDs | semsource / local / semspec-e2e-local | code, agent, wf, exec | workspace, agentic-loop, plan | folder, repo, step ids |
| semsage | constants `OrgDefault`/`PlatformDefault` (`agentgraph/entities.go:83-125`) | `default` | agentic | orchestrator | task/loop ids |
| semmem | config instance_id (`cmd/semmem/main.go:326-334`); entity tests use **5-part** IDs (`entity/github_test.go:14-236`, 38 of 39 corpus candidates) | `semmem-*-001` | github, docs, code | issues, prs, discussions, specs | ids |

**Census totals (distinct literal values per position, valid six-part literals only, `cmd/entity-id-audit -format json`
corpus, filtered by the canonical regex):**

| Repo | literals | org | platform | domain | system | type |
|---|---|---|---|---|---|---|
| semstreams (tracked) | 1,041 | 11 | 39 | 65 | 165 | 61 |
| semsource | 16 | 1 | 1 | 5 | 7 | 7 |
| semdev | 30 | 2 | 3 | 2 | 4 | 3 |
| semboids | 1 | 1 | 1 | 1 | 1 | 1 |
| semdragon | 13 | 4 | 4 | 2 | 3 | 5 |
| semmem | 1 (+38 five-part) | 1 | 1 | 1 | 1 | 1 |
| semspec | 11,221 | 2 | 4 | 4 | 5 | 9 |
| semsage | 0 (+1 `_` placeholder) | 0 | 0 | 0 | 0 | 0 |
| semconnect | 183 | 3 | 3 | 3 | 4 | 15 |
| semteams / semmachina / semops | audit aborted on non-JSON/YAML fixtures (`ui/tsconfig.json`, `web/tsconfig.json`, `tickets/COP-003.yaml`) | — | — | — | — | — |

Token per count (r2): "literals" = candidates of language `literal` whose value matches the canonical six-part regex.
The audit's own totals differ because they include declaration patterns and query prefixes and count findings —
semsource: 21 candidates (19 literal, 2 query-prefix), 3 findings, 16 valid six-part literals; semspec: 11,257
candidates (11,248 literal, 2 declaration-pattern, 7 query-prefix), 29 findings, 11,221 valid six-part literals.
T4's 49-file count reproduces in r2. semteams/semmachina/semops rows stay unmeasured.

Product-name-in-platform, the family ruling 2 retires: semsource (`semsource`), semconnect (`semconnect`), semmachina
(`semmachina-<world>`), semboids fallback (`semboids`), semspec fixtures (`semsource`), and the framework's own
`semstreams.framework` (ADR-076).

## 2. Surface inventory (contract §"The surface inventory")

1. **The claimed gap** — "no authority for segment values." Searches: `grep -rn 'EntityDomain\|DomainDelegation\|RegisterDomain\|domainRegistry\|SegmentAuthority' --include='*.go'` → 0; the only namespace authority is `vocabulary/namespace_authority.go:1-125` (predicates), with one production consumer `agentic/tools.go:369-382`. The gap is real.
2. **Every current spelling of the facts** — *deployment authority*: `config.Platform.{ID,InstanceID}` (`config/config.go:772-778`), `types.PlatformMeta` (`types/component.go:134-137`), `platform.Config` (`pkg/platform/platform.go:27-37`), the first two positions of every minted ID, `DefaultFederationMeta.Platform()` (`message/federation.go:78`), `semspec workflow.EntityPrefix()`. Four spellings of one fact; the ID positions are the only persisted one. *Source*: `Triple.Source` (`message/triple.go:58`), envelope `source` (`base_message.go:238`), `EventMetadata.Source` (`graph/events.go:60`), semsource's platform constant, semops `Source` field, position 4 (`system`) per the struct comment. Five spellings. *Taxonomy*: position 3 + `EntityType{Domain,Type}` (`entity_id.go:108-111`) + `vocabulary.EntityTypeIRI("domain.type")` (`iris.go:33-51`). Three spellings, consistent.
3. **Adjacent claims** — ADR-076 (framework namespace, digest identity, never rewrite); ADR-099 (partition = prefix; gated on this); ADR-072 (lane = hash(ID)); ADR-065/078 (fixed 9/13/8-token composite keys); ADR-068 (no eviction; fresh state); ADR-032 (`org` as tenant boundary, paused #26); ADR-087 (hash-keyed summaries); `entity-id-contract` (lexical; cutover clause `:319-350`); `structural-identity/spec.md:6-13` (six parts, order named); `graph-ingest/spec.md:232-278` (structural gate); `agentic-lessons:78-92`; `user-response-subject-ownership:51,127`; #1093 (same wave); #1096 (bug split); #1097 (docs); #818 (immutable birth predicates); semmem PR #2 (`docs/federation-mvp.md:34-55,78` requires canonical IDs, source authority retention, and rejection of non-canonical IDs).
4. **Consumer at birth** for anything the design would add — recorded in the design document per symbol; the inventory adds none.

### Same-class collision table (semantic job: "who is the authority behind this identity, and who may occupy a position")

| Dimension | Evidence |
|---|---|
| Semantic class | authority of an entity ID's first two positions; ownership of the taxonomy positions |
| Owners | `config.Config.Validate` (`config/config.go:225-241`, org lowercased + required id); every builder in §1.14; `vocabulary.PredicateAuthority` for the *predicate* half; ADR-076 for framework-derived families |
| Catalogs | `payloadregistry` (`message.Type{Domain,Category,Version}` — payload domain, not entity domain); `vocabulary` predicate registry; no entity-domain catalog (search in §2.1) |
| Status | none — no readiness or health surface reports authority |
| Lifecycle | fresh-state cutover only (`entity-id-contract:319-350`); ADR-076 d6 |
| Ownership | single writer `graph-ingest` (`openspec/project.md:93-94`); semantic ownership retired (ADR-091) |
| Readers | every position reader in §1.4 W3; ADR-099 partitioner (planned); lesson scope matcher; rule substitution |
| Writers | §1.14 builders; wire-supplied (`examples/.../payload.go:350`, e2e mission) |
| Recovery | replay from `ENTITY_STATES` rebuilds derived indexes (ADR-065 §Migration); no authority recovery exists |

## 3. Adopter seam inventory (mandatory)

Answered for five people who have never opened `pkg/types/entity_id.go`.

**A. A component author minting an entity (framework or product).**
1. *Must know:* the six positions and their order; that org/platform come from `deps.Platform` and not from a constant or the payload; that `instance_id` silently wins over `id` (`config.go:772-778`); that instance must be dot-free and the whole ID ≤ 256 bytes; that a conventional-name instance will collide across deployments sharing `org.platform`; that `group`/`container`/`level` are reserved instance tokens (H2). Six debts → a design finding, not a doc task.
2. *If they do nothing:* everything works locally; a product-name platform (semsource, semconnect) or a literal (`"local"`, `"edge"`, `"default"`) ships silently; the failure appears only in a third party's graph as merged authority.
3. *Where they find out:* nowhere — no boot check, no typed error, no metric. The struct comment (`entity_id.go:86`) says source is `system`; the shipped dogfooder puts it in `platform`.
4. *Should know:* nothing about authority — the framework holds it. The gap is that the framework does not observe an ID's authority at any boundary (F2).

**B. An operator standing up a deployment.**
1. *Must know:* `platform.org` + `platform.id` are identity-bearing; `instance_id` overrides `id`; the pair must be unique among every deployment they will ever federate with; reference configs ship `org: c360` (11 of 13) — the copy-paste attractor.
2. *If they do nothing:* local success; silent authority collision at a peer.
3. *Where:* `docs/concepts/16-federation.md:40` says the opposite (#1097).
4. *Should know:* one required identity pair; a peer that receives a write claiming its own authority rejects it loudly (observable), so the only unobservable residue is peer-vs-peer.

**C. A sister exporting entities to another deployment (semsource → semmem).**
1. *Must know:* which lane is an import lane; that their `org.platform` will be preserved; that `Triple.Source`/envelope `source` are the only provenance; that nothing authenticates it (F3–F4).
2. *If they do nothing:* today, accepted as local truth under foreign authority; rules fire and mint local runtime state under that foreign authority (#1096).
3. *Where:* nowhere.
4. *Should know:* only that the receiving port is declared as an import lane by its operator.

**D. A rule author using `$entity.<segment>`.**
1. *Must know:* the six token names (`execution_context.go:211-218`); which one is the bare id tools want (`instance`). Names are already meaning-based, so a reorder is invisible to them **only if** the resolver maps by name, not index (R1).
2. *If they do nothing:* unresolved-token warning (`entity_substitution.go:64-68`) — loud. Good.
3. *Where:* log line at fire time.
4. *Should know:* the names. Already the case.

**E. A lesson emitter choosing `applies_to: id:<prefix>`.**
1. *Must know:* three segments minimum; what the third segment means (domain today; source after a reorder).
2. *If they do nothing:* `emit_lesson` rejects with the grammar rule (`emit_lesson.go:841`) — loud.
3. *Where:* tool error.
4. *Should know:* "scope to a source" or "scope to a taxonomy" by name, not by counting segments — the gap a named prefix-level vocabulary closes.

### Prefer observation to prediction — where a producer predicts a framework-owned value

| Prediction today | Observation the framework could make instead |
|---|---|
| Every builder call re-supplies `org, platform` (18 `deps.Platform` lines + 62 `platform.{Org,Platform}` lines, §1.14) — a value the composition root owns | graph-ingest compares positions 1–2 of every candidate against `deps.Platform` (F2) and rejects a mismatch on any lane not declared as import |
| An operator predicts that `platform.id` is globally unique | the importing side observes a write claiming *its own* authority on an import lane and rejects it |
| A product predicts `instance_id` vs `id` precedence | one identity field; config load rejects ambiguity |
| A rule predicts the firing entity's org/platform is the deployment's own (`actions.go:1575-1583`) | mint from `deps.Platform` (#1096) |
| `emit_lesson` authors count segments to hit "domain scope" | a named prefix level |
| semsource predicts the 256-byte budget per segment (`entityid.go:76-97` `MaxOrgLen`) | unchanged by this issue; noted |

## 4. PREMISE FAILED lines (claims in the issue or ruling that did not survive measurement)

- **PF-1** "Predicate-index composite keys (ADR-065) … hash of the predicate": the live layout is raw `predicate3.entity6` (9 tokens) per ADR-078 (`predicate_index.go:12-16`, `query.go:734-745`; ADR-068 correction `:24-33`). Arity dependence stands; the hashing does not.
- **PF-2 (withdrawn in r2).** Revision 1 set 96 `deps.Platform` production reads against the issue's "~29"; the 96 came from a receiver-letter regex that mixed tokens. Re-measured per token: `deps.Platform` 18 non-test lines — consistent with the issue's own token; `platform.{Org,Platform}` 62 lines; `.Platform.{Org,Platform}` 14 lines. The issue's number stands. The class conclusion stands on the corrected numbers: authority reads far exceed the `deps.Platform` token because most builders read it through a copied `platform` field.
- **PF-3** Issue Evidence D "hierarchy containers … coherent with ADR-099 — not a finding": the 3/4/5 cuts coincide, but containers are a second, stored spelling of the derived partition (H5), pad with reserved tokens that have no contract (H2), and overflow the byte bound at the edge (H3). It is a finding.
- **PF-4** `entity-id-contract:321-322` "zero violations" and the issue's "that work is sound": `task entity-id:audit` reports 30 unclassified arity findings at `5cc0c7fb` and is not a CI gate (T2–T3). The lexical contract is sound; its enforcement corpus is not.
- **PF-5** Ruling constraint "NATS subject token counts … depend on fixed arity": no production publish subject carries a whole entity ID (§1.1); the arity dependence is in KV composite keys and wildcard filters (X1–X3, K3), not in publish subjects. The constraint's conclusion (arity six) is unchanged; its stated mechanism is the KV key contract.
- **PF-6** Issue "semmem … is building against it now … requires canonical six-part IDs": semmem's entity fixtures are 5-part and already invalid under the lexical contract (38 of 39 corpus candidates, `entity/github_test.go`); its PR #2 docs require canonical IDs (`docs/federation-mvp.md:46,78`). The consumer is behind the contract, not ahead of it.
- **PF-7** ADR-076 d1's framework namespace is a same-class collision with ruling 2: trigger identity digests only `(packID, ruleID)` (`graph_event_identity.go:28-32`), so two deployments running the same pack mint the same entity under a fixed `semstreams.framework` authority. Ruling 2 cannot hold while ADR-076 d1 stands.
- **PF-8** gh606 design P6 "the canonical prefix functions exist in ONE home" — true, and that home has zero production callers (W2). The forthcoming partitioner would be their first consumer; their 3-part meaning is what a reorder changes.
- **PF-9** Issue Evidence B lists `lifecycle` as a framework domain: it is minted only by the e2e mission harness (`cmd/e2e-semstreams/mission/command.go:61`), not by a framework component; the framework's own domain set is `agent`, `ops`, `gateddag` (+ ADR-076's `graph`).
