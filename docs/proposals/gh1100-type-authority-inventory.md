# gh#1100 — `message.Type` authority: surface inventory and adopter seam inventory

Baseline: `origin/main` `c3a17741bd8e56520307b68a3ef6fe8d2d159472` (2026-08-26); revision 3 re-premised at `7e7ea76e`
(#1099 and #1104 merged; every line cited below re-read there — none moved). Read-only architect pass; every claim below is
a `file:line` at that head or a search that returned what is stated. Sisters were read at their checked-out heads
(`semsource 4093d3c`, `semteams 8a70b7e7`, `semmachina 841c45e` 16 dirty, `semmem b909cbf`, `semdev ca3956a`,
`semdragon 07f4de9`, `semconnect d0d06e0`). Status: **INVENTORY PASS WITH DIVERGENCES** (blind, Fable, 2026-08-26); D1–D3 corrected and L1–L5 added in revision 2.
Pre-owner design review round 1 (Fable, adversarial, 2026-08-26): REQUEST CHANGES — revision 3 adds the third birth lane
(B-1, §2.2/§2.4) and the builder facts behind F-1/F-2 (§2.7); dispositions in the design §18. Nothing below is a target state.

## 0. Problem statement (as measured)

Three tables are keyed by the `message.Type` namespace (`domain.category.version`, `pkg/types/type.go:20-38`; `message.Type`
is an alias, `message/types.go:16`) and none checks another:

| Authority | Where | Keyed by | Knows the framework's mutation-only stamps? |
|---|---|---|---|
| Payload registry | `payloadregistry/registry.go:43-52` (`Registration`), `:78-132` (`Register`: nil/factory/domain/category/version checks, `validateSchemaConsistency` `:261-300`, duplicate rejection `:121-128`), lookup `Create :138`, `Build :157`, `GetRegistration :189` (copy without Factory), `List :211`, `ListByDomain :234` | `Registration.MessageType()` string `:56-58` | **0 of 6** (§2) |
| Projection contracts | `internal/builtinprojection/contracts.go:21-49` (`Contracts()`: loop-execution `:23-46`, lesson `:47`), `LessonContract() :52-80`; `pkg/projection/contract.go:36-43` (`MessageType` `omitempty` `:38`, `IndexingProfile` `:42`), `Validate :46-98` (name, pattern, groups, birth predicates, profile against a **local** `validIndexingProfiles` map `:12-14`; never the type) | `Contract.MessageType` string, optional | **2 of 6** (`loop_execution`, `agent_lesson`) |
| Indexing-profile floors | `processor/graph-ingest/indexing_profile_registry.go:30-65` (`indexingProfileDefaults`, 22 keys), `indexingProfileFloorFor :71-76`; consumer `component.go:1864-1868`; metric `component.go:113-117` `indexing_profile_default_total{message_type}` | `message.Type.Key()` string | **0 of 6** |

Why the floor is string-keyed: `indexing_profile_registry.go:16-22` — "Deliberately keyed by STRING, not by importing the
agentic/research domain packages into the generic graph-ingest layer". Measured consequence the comment omits: graph-ingest
already imports `payloadregistry` through `message.NewDecoder(deps.PayloadRegistry)` (`component.go:692`), and every one of the
22 floor keys is a registered type today (agentic 15 `agentic/payload_registry.go:20-36`; `agentic.signal_message.v1`
`processor/agentic-dispatch/payload_registry.go:42`; research 6 `agentic/research/register.go:16-58`) — **D1:** the six
`research.*` keys are registered only in a binary that selects graph research (`cmd/semstreams/main.go:766-770`,
`agentic/research/register.go:10-14`: "intentionally absent from the unconditional payloadbuiltins registry"); a floor derived
from registrations is therefore per-binary for those six.

`EntityState.MessageType` (`graph/types.go:38-40`): "records the original message type … Provides provenance and enables
filtering by message source." Persisted verbatim by the canonical codec; copied by `Clone` `:105`.

## 1. The claimed gap — measured

| Claim in #1100 | Measurement | Verdict |
|---|---|---|
| Five mutation-only stamps, all unregistered | `agentic/{agent_lesson:18,31-37, loop_execution:16,167-173, model_endpoint:9,25-31, ops_diagnosis:13,25-31, web_observation:21,25-27}_entity.go`; none appears in any `payloadregistry.Registration{` literal (12 sites, §2.1) | holds — and undercounts (§2.2) |
| Registry rejects duplicates | `payloadregistry/registry.go:121-128` | holds |
| Contracts' `MessageType` optional | `pkg/projection/contract.go:38`; but `pkg/projection/mutation_client.go:322-327` requires a valid entity type and equality when the contract sets one | holds for the binding, not for the stamp |
| `IsValid` is the only ingest check | `processor/graph-ingest/canonical_mutations.go:207-209` (`IsValid` → `invalid_request`) | holds |
| `_Distinct` tests hand-compare strings | `agentic/{model_endpoint:27, agent_lesson:111, ops_diagnosis:111, loop_execution:337}_entity_test.go` — each compares `mt.Category` to a hand list of 5–8 constants | holds |
| StoredMessage is the counter-example | `storage/objectstore/stored_message.go:92-103` (registered `storage.stored.v1` with Factory + Builder), `:137-145` (`EntityID`, `Triples`) | holds |
| Fact lane decodes "gracefully" to a generic payload | `message/base_message.go:301-307` **rejects** an unregistered type (`unregistered payload type: …`); the fallback described at `payloadregistry/registry.go:134-137` is a stale comment | **PREMISE FAILED** (P3) |
| ADR-076 families are "registered — compare" | `graph/events.go` has no `message.Type` (grep → 0); ADR-076 names entity-ID families (`graph/events.go:19-20`, `processor/rule/graph_event_identity.go:12-15`); `graph.events.entity.create` has no consumer (grep → only `gateway/graph-gateway/component.go:1054` for `relationship.create`) | **PREMISE FAILED** (P2) |
| `emit_lesson.go:236` is the only semantic use of the stamp | also `pkg/projection/mutation_client.go:322-327`, semdragon `questdag/unit.go:599` (reader) | **PREMISE FAILED** (P5) |
| ADR-056 :281-284 premise, ADR-091 superseded in full | `docs/adr/056-*.md:283` ("producer identity for the gate **IS the registered `MessageType`**"), `:1170`; `docs/adr/091-*.md:9` ("supersedes ADR-056 in full"); ADR-091 body mentions `MessageType` 0 times | holds |

## 2. Every current spelling of "what type is this entity"

### 2.1 Registration sites (the writers of the registry)

`payloadregistry.Registration{` literals, non-test: `agentic/payload_registry.go:20` (15), `agentic/research/register.go:16` (6),
`processor/agentic-dispatch/payload_registry.go:42` (1), `processor/gated-dag/payload.go:109` (2), `governance/verdict.go:133` (1),
`message/generic_json.go:29` (1), `storage/objectstore/stored_message.go:93` (1), `cmd/e2e-semstreams/mission/command.go:115` (1),
`examples/processors/{document/payload.go:240 (4), iot_sensor/payload.go:103,123 (2), weather_station/payload.go:61 (1)}`,
`frameworkcapabilities/graphresearch/register.go:473`. Aggregator: `payloadbuiltins/register.go:33-49` (message, agentic,
agentic-dispatch, gated-dag, objectstore, governance). Composition roots: `cmd/semstreams/main.go:214,761-772`
(`registerPayloads`: builtins + selected graphresearch), `cmd/e2e-semstreams/main.go:147,358-378` (builtins + iot_sensor +
document + mission + graphresearch). Vocabulary builtins register first (`cmd/semstreams/main.go:80` `builtins.Register()`).

### 2.2 Every `message.Type` minted in-tree and NOT registered (the census the issue asked for)

| Key | Minted at | Stamped at (mutation lane) | Registered? |
|---|---|---|---|
| `agentic.agent_lesson.v1` | `agentic/agent_lesson_entity.go:31-37` | `processor/agentic-tools/emit_lesson.go:527` → `:204-216` (`CreateEntityRequest.Entity.MessageType`) | no |
| `agentic.loop_execution.v1` | `agentic/loop_execution_entity.go:167-173` | `processor/agentic-loop/graph_writer.go:474`; `frameworkcapabilities/graphresearch/executor.go:387` → `processor/research-graph-llmwrap/triplepub.go:94-100,167` | no |
| `agentic.ops_diagnosis.v1` | `agentic/ops_diagnosis_entity.go:25-31` | `processor/agentic-tools/emit_diagnosis.go:204` → `decide.go:672-684` | no |
| `agentic.model_endpoint.v1` | `agentic/model_endpoint_entity.go:25-31` | `processor/agentic-loop/graph_writer.go:254` | no |
| `agentic.web_observation.v1` | `agentic/web_observation_entity.go:25-27` | `processor/agentic-tools/executors/web_emit.go:61` (callers `httprequest.go:267`, `websearch.go:264`) | no |
| **`lifecycle.harness.v1`** | `pkg/lifecycle/manager.go:24-28` | `pkg/lifecycle/manager.go:400-407` (every `Manager` birth) | **no** — the issue's "five" is six in-tree; `grep -rn '"harness"' --include='*.go'` → the definition only |
| `entity.state.v1` | `processor/rule/message_handler.go:367-371` | none — an in-memory wrapper for `ExecuteEvents`, never persisted or published | no (not a stamp) |
| **(empty)** — hierarchy containers | `graph/inference/hierarchy.go:427-437` builds `&gtypes.EntityState{ID, Triples}` with **no `MessageType`** | `hierarchy.go:440` `entityManager.CreateEntity` → adapter `processor/graph-ingest/component.go:451-456` → `Component.CreateEntity` `:1893-1896` → `createEntityWithReceipt` `:2081` (`ValidateEntityStateContract :2093`, `reconcileIndexingProfile :2121`, `entityBucket.Create :2132`) — **never `handleCanonicalCreate`**; every deployment with `enable_hierarchy: true` (`configs/e2e-structural.json:480`, `configs/agentic.json:182`) persists `message_type` `""`, and `indexing_profile_default_total{message_type="unknown"}` fires for each (`component.go:1876-1880`) | **B-1**: a third birth lane, in-process, unstamped |

Test-only stamps (**D3**, re-measured: `grep -rE 'MessageType: *message\.Type\{' --include='*_test.go'` → **42 sites, ≥14 distinct
keys**): `test.entity.v1` ×26, `test.fixture.v1`, `test.poison.v1`, `boid.telemetry.v1`, `attack.test.v1`, `attack.stress.v1`,
`workflow.task-unit.v1`, `test.sensor.v1`, `test.seed.v1`, `test.mutation.v1`, `test.decode.v1`, `test.container.v1`,
`logistics.sensor.v1`, `test.noop.v1`, `fs.unit.v1`, `fs.holder.v1`, `test.revision.v1`, `test.revision-claim.v1`, `e2e.probe.v1`,
plus `test.widget.v1` via a builder (`processor/graph-ingest/indexing_profile_test.go:29,75-263`). Many are fact-lane or
direct-state fixtures that never pass the create seam; 13 test files construct `CreateEntityRequest{` (the seam that will
reject). On the real e2e wire: `test.fixture.v1` (`test/e2e/scenarios/graph_roundtrip.go:207,233`; `lessons/scenario.go:374,391`),
`e2e.probe.v1` (`crud-tools/scenario.go:684`), `e2e.eventtime.v1` (`tiered_structural.go:1273`), `e2e.canonical_create_contract.v1`
(`:444`), `e2e.relationship_contract.v1` (`:502`), `research.e2e_search_seed.v1` (`research-graph/scenario.go:350-352`) — rejected
unless the e2e binary registers them (**L4**). Direct-KV seed with a **mis-spelled** key
`agentic.loop-completed.1` (`test/e2e/scenarios/ops/scenario.go:459-470`, `PutKV` into `ENTITY_STATES` — bypasses the sole
writer, `openspec/specs/graph-ingest/spec.md:4`).

Registered types that are also stamped on the mutation lane by the framework: none (the registered set is fact-lane only, §2.1).

### 2.3 Every consumer of `EntityState.MessageType` (non-test, non-worktree; `grep -rnE '\.MessageType\b'`)

| Reader | Line | What it does with the stamp |
|---|---|---|
| graph-ingest floor | `processor/graph-ingest/component.go:1864-1868` | `indexingProfileFloorFor(entity.MessageType)`; metric label `indexingProfileMetricLabel :1875-1880` |
| graph-ingest merge | `component.go:2036` | `existing.MessageType = entity.MessageType` (fact-lane arrival wins) |
| graph-ingest create | `canonical_mutations.go:207` | `IsValid()` only |
| fact-lane extraction | `component.go:1704-1760` (`:1732` `MessageType: msg.Type()`) | the decoded, therefore registered, type |
| projection client | `pkg/projection/mutation_client.go:322-327` | requires valid; equality with `contract.MessageType` when set |
| emit_lesson guard | `processor/agentic-tools/emit_lesson.go:236-239` | collision check on create conflict |
| codec / boot sweep | `graph/entity_predicate_contract.go:134-175` (`ValidateEntityStateContract`: ID, subjects, references — **not** the type); `component.go:1264-1290` (`UnmarshalEntityState`) | none |
| graph-query / gateway | `grep -rn 'message_type\|MessageType' processor/graph-query gateway` → 0 | **no reader filters by message source** (the `graph/types.go:39` promise has no consumer) |
| e2e | `test/e2e/scenarios/lessons/scenario.go:440,549-550` | asserts the stamp round-trips |
| sister reader | `semdragon/questdag/unit.go:599` | rejects a unit entity whose type is not `questdag.unit.v1` |

### 2.4 The two lanes and the seam

- Fact lane: `component.go:1597-1608` `decodeEntity` → `c.decoder.Decode` (`:1599`; decoder built at `:692` from `deps.PayloadRegistry`)
  → `message/base_message.go:301-307` rejects an unregistered type → `extractEntityFromMessage :1704` stamps `msg.Type()` `:1732`.
  **The fact lane already enforces "registered or rejected".**
- Mutation lane: `canonical_mutations.go:199-240` `handleCanonicalCreate`: decode `:201`, nil entity `:204`, `IsValid` `:207`, no
  entity.triples `:210`, profile validity `:214`, clone + subject check `:219-228`, version/updated `:229-234`,
  `stampExplicitIndexingProfile` `:234`, `reconcileIndexingProfile` `:235`, `ValidateEntityStateContract` `:236`. The registry is
  never consulted. `reconcile`/`append`/`delete` requests carry no `Entity` and no type (`graph/mutation_requests.go:17-41`).
- Rejection vocabulary: `graph/mutation_responses.go:10-52` closed set (`invalid_request :32`, `structural_invalid :39`, …; "adding a
  value requires updating both this declaration and the graph-ingest handler"); helpers `processor/graph-ingest/mutation_runtime.go:198-204`
  (`rejectInvalid`, `rejectInvalidDetail` → `errs.ClassifiedCodeDetail`); metric `mutation_rejections_total{subject,reason}`
  `component.go:134-138`.
- Ingest factory: only `deps.NATSClient` is nil-checked (`component.go:646`); a nil `PayloadRegistry` surfaces at the first
  fact-lane message (`message/decoder.go:39-44`), not at boot.
- **L1 — who actually writes births.** `pkg/projection.MutationClient.Create` has **zero production callers in-tree**
  (`grep -rn 'CreateMutation{' --include='*.go'` non-test → only `test/e2e/scenarios/graph_roundtrip.go:105`,
  `lessons/scenario.go:388`). All six framework stampers call `internal/graphmutation/client.go:89` directly
  (`emit_lesson.go:204-216`, `decide.go:672-684`, `graph_writer.go`, `triplepub.go:94-100`, `pkg/lifecycle/graph_emit.go`). The
  client-side check at `mutation_client.go:322-326` therefore protects only sisters using `CreateMutation` (semmachina, semdev)
  and e2e; an ingest-side gate is the only check that covers the framework's own writers.
- **L2 — constructions that bypass the factory.** 23 `&Component{` literals across six `processor/graph-ingest/*_test.go`
  files (`readiness_test.go` 14, `lifecycle_owner_test.go` 4, `keyed_ingest_test.go` 2, `batch_unit_test.go`, `component_test.go`,
  `query_contract_guard_test.go` 1 each); none sets `decoder` or a registry (`grep -l 'decoder:\|PayloadRegistry'` → 0 files).
- **B-1 — two disjoint create paths, one of them in-process.** Births reach `ENTITY_STATES` by exactly two code paths:
  the RPC lane `handleCanonicalCreate` (`canonical_mutations.go:199-243`, writes `c.entityBucket.Create` at `:243`) and the
  in-process lane `Component.CreateEntity` (`component.go:1893-1896`) → `createEntityWithReceipt` (`:2081-2132`, writes at
  `:2132`). They share no gate. Births and mutations reach `ENTITY_STATES` through **six** `entityBucket` writers (`git grep -nE 'entityBucket\.[A-Z]\w*\('`, non-test — the earlier `Create|Put|Update` filter could not match `UpdateWithRetry`): `canonical_mutations.go:243` (RPC create), `:306` (RPC reconcile, must-exist), `component.go:1985` (`MergeEntity`, **birth-capable** through the `len(current)==0` branch `:1993-2000`), `:2132` (in-process create), `:2311` and `:2495` (`AddTriple`/batch, must-exist); plus `:2174` `DeleteAtRevision`. **Four are birth-capable, and one of those is decode-gated:** `MergeEntity`'s sole caller is `ingestEntity` `:1633`, reached only through `c.decoder.Decode` `:1599` → `extractEntityFromMessage` `:1704` (`MessageType: msg.Type()` `:1732`), so a fact-lane birth carries a registered key by construction (ADR-103 d3) and `:1985` needs no helper. The two births that need the helper are `canonical_mutations.go:243` and `component.go:2132`.
  Every `CreateEntity(` caller: `graph/inference/hierarchy.go:440` and the adapter `component.go:452` — the hierarchy container
  is the only in-process birth, and it carries an empty type. `hierarchy.go:440-451` returns the create error without logging;
  both graph-ingest callers WARN and continue without the container (`component.go:1971`, `:2108`).
- **L5 — the import edge a registry-held floor adds.** `go list -deps ./message | grep -c vocabulary` → 0 and the same for
  `./payloadregistry` → 0: neither imports `vocabulary` today; `payloadregistry/registry.go:4` documents "imports only stdlib +
  pkg/errs + pkg/types".

### 2.5 `_Distinct` tests

Four functions (§1). Each asserts `mt.Category` differs from a hand-maintained list; none covers `web_observation` or
`lifecycle.harness`; the lists disagree with each other (model_endpoint's omits `ops_diagnosis`/`agent_lesson`). The
registry's duplicate check (`registry.go:121-128`) plus `payloadbuiltins/register_test.go:10-13` (registers the full builtin set)
already is the collision detector for every registered type.

### 2.6 The pattern to copy

`storage/objectstore/stored_message.go:89-103` (`RegisterPayloads` with Factory + Builder), `:113-124` (fields: entityID,
triples, storageRef, storedAt, messageType), `:137-160` (`EntityID`, `Triples`, `StorageRef`, `MessageType`), `:162-` (MarshalJSON /
UnmarshalJSON at `:194`). Existing half-pattern in the five: `agentic.LoopExecutionEntity` (`loop_execution_entity.go:68-151`) is
already Graphable (`EntityID :76`, `Triples :91`) — it lacks only `Schema`/`Validate`/`MarshalJSON`/factory/registration.

### 2.7 Other spellings of the same facts

- Profile vocabulary: `vocabulary/predicates.go:325-332` (`IsValidIndexingProfile`) **and** `pkg/projection/contract.go:12-14`
  (`validIndexingProfiles`) — two homes.
- Triple builders for the five live in the writer packages, not beside the type: `emit_lesson.go:693-741`
  (`buildEmitLessonTriples`, source `ops-emit-lesson` `:34`; emits `LessonStatus` at birth, never `LessonSupersededBy`/`LessonRetiredAt`),
  `emit_diagnosis.go:249-291` (source `ops-emit-diagnosis` `:26`; **`Confidence: args.Confidence` on every triple** `:259-265`),
  `graph_writer.go:511-548` (`buildModelEndpointTriples`, source `agentic-loop` `:24`; **five zero-gates** `:529-542` — `MaxTokens`,
  `InputPricePer1MTokens`, `OutputPricePer1MTokens`, `URL`, `RequestsPerMinute` — and `bool`/`int`/`float64` objects; the diagnosis
  confidence object is `fmt.Sprintf("%g", …)` at `emit_diagnosis.go:262`), `loop_execution_entity.go:91-151` (beside
  its type; never emits `TodoRecord`), and **two** web builders with **two sources** and two unconditional sets:
  `executors/httprequest.go:28` (`agent-http-request`) `:257-266` always emits `WebURL, WebFetchedAt, WebFetchedBy, WebContentType,
  WebStatusCode, WebText, WebTruncated` (zero values included); `websearch.go:31` (`agent-web-search`) `:255-262` always emits
  `WebURL, WebTitle, WebSnippet, WebSourceQuery, WebObservedAt, WebObservedBy`.
- Contract names/groups: `internal/builtinprojection/contracts.go:12-17`; consumers `processor/agentic-tools/lesson_promotion.go:52,170-171`,
  `write_todos.go:196-197`; composition `cmd/semstreams/main.go:221`, `cmd/e2e-semstreams/main.go:154` → `service.WireGraphRuntime`
  (`service/graph_runtime.go:16-21`, variadic `projection.Contract`).

## 3. Adjacent claims on the territory

- ADR-054 §4(c) (`docs/adr/054-*.md:185-194`) defines the floor and the metric's meaning ("an unclassified registry GAP"); §5
  (`:196-233`) the stamp seam; §7 (`:244-256`) `control` = embed yes, `content` = retrieval corpus.
- ADR-056 `:283`, `:1170` — the premise; ADR-091 `:9` supersedes it in full and is silent on the stamp.
- ADR-080 `:33-41` — lesson entity under `agent.*`; ADR-027 — ops diagnosis.
- Specs: `graph-ingest/spec.md:1-24` (Purpose names the two lanes and the create-time profile), `:67-80` (profile immutable
  across merge), `:232-256` (coded structural rejection — the template for a coded type rejection), `:489-512` (boot sweep uses
  the canonical decoder only), `:741-762` (four operations); `agentic-lessons/spec.md:37-62` (lesson birth via `entity.create`
  with a semantic envelope), `:193-226` (`LessonProjectionContract()` snapshot; products supply no copied literals);
  `projection-mutation-client/spec.md:9-21` (contracts are local; optional message type and profile); `rule-projection-mutations/spec.md:46-56`
  (non-inferable metadata explicit); `graph-state-contract/spec.md:105-124` (one canonical codec). **No spec exists for the
  payload registry** (`ls openspec/specs` → none; seeded lazily by the change that first touches it).
- Active/claimed work (re-premised at `7e7ea76e`): **PR #1099 is MERGED** as a design package (ADR-102 Accepted; change
  `entity-id-segment-semantics` 0/51 open). Its change has **no lesson-import scenario**; its implementation tasks 5.1 (`:210-218`, the builder
  files: `agent_lesson_entity.go:68,92`, `web_observation_entity.go:79`, `ops_diagnosis_entity.go:56`) and 5.3 (`:223-229`,
  declaration patterns: `internal/builtinprojection/contracts.go:26,56`, which this change deletes, and the lesson prefix
  `:85-93`) edit the same five `agentic/*_entity.go` files; its inventory row W5 lists the two patterns `*.*.agent.agentic-loop.execution.*` and `*.*.agent.lesson.record.*`
  as rewrites under the ADR-102 order (`acme.dep1.agentic-loop.agent.execution.<uuid>` in its graph-ingest delta). Its O-6
  rules hierarchy containers "retire with gh606" (design `:143,365`). **PR #1104 is MERGED**: `.agents/skills/new-payload/SKILL.md`,
  `docs/concepts/15-payload-registry.md`, `CLAUDE.md`, `AGENTS.md` now teach `RegisterPayloads(reg)` (15/15/2/2 mentions) and
  mention `IndexingProfile`/`Contracts` 0 times; `.claude/skills/new-payload/SKILL.md` is a thin adapter, not a copy.
  Milestone `v1.0.0-beta.163` exists and holds #1100. `AGENTS.md` Land bullet is at `:68-73`. PR #1101 (#1092) touches
  `component.Registration` (a different registry) and no payload surface. #1093 edits `cmd/semstreams/main.go`. #818 (birth
  discipline) would consume a contract-per-type.
- Skill/doc drift: closed by #1104 (`b0d65ff0`); what remains is that the rewritten checklist knows no floor, contract, or
  test-type helper (0 mentions in all four files).

## 4. The consumer at birth (for every new symbol the design will introduce)

`Registration.IndexingProfile` → `reconcileIndexingProfile` (`component.go:1864`). `Registration.Contracts` → `WireGraphRuntime`
(`main.go:221`) via a registry-derived contract list, `LessonProjectionContract()` (`lesson_promotion.go:52`), the contract-vs-`Triples()`
conformance test. `graph.ErrorCodeMessageTypeUnregistered` → `handleCanonicalCreate` and the `mutation_rejections_total{reason}`
label. The six payload structs → their existing writers (§2.2) and the fact-lane/import decode (`base_message.go:301`). A
test-registration helper → the 13 test files constructing `CreateEntityRequest{` (§7 of the design) and the e2e binary.

## 5. Same-class collision table

| Dimension | Evidence |
|---|---|
| Semantic class | "What type is this entity; can it be decoded; what floor and what birth contract apply" |
| Owners | `payloadregistry` (decode/collision), `internal/builtinprojection` + `pkg/projection` (contract), `graph-ingest` floor table, the six `*MessageType()` builders, sisters' own namespace homes (`semmachina/internal/payload/constants.go:60-147`) |
| Catalogs | `payloadbuiltins.Register` set; `builtinprojection.Contracts()`; `indexingProfileDefaults`; rule packs' `projection_contracts` (`rule-projection-mutations/spec.md:34-56`) |
| Status | `indexing_profile_default_total{message_type}`; `mutation_rejections_total{subject,reason}` |
| Lifecycle | registrations boot-time, immutable; contracts static across hot reload (`rule-projection-mutations/spec.md:22-32`) |
| Ownership | one registry per binary via `component.Dependencies.PayloadRegistry` (`component/dependencies.go:74`); no global |
| Readers | §2.3 |
| Writers | §2.1; sisters §6 |
| Recovery | none needed — a registration defect is a boot error (`payloadbuiltins.Register` aggregates via `errors.Join`) |

## 6. Sister census (read-only) — every mutation-lane stamp and whether the sister registers it

| Sister (semstreams pin) | Stamps on `entity.create` | Registered in that sister? | Obligation under a registry gate |
|---|---|---|---|
| semsource (beta.160) | none — births on the fact lane as `semsource.entity.v1` (`graph/event_payload.go:22-31`); mutation lane = `Reconcile` only (`processor/supersession/lifecycle.go:303,325`); contract `graph/contract.go:69-72` names the registered key | yes (`event_payload.go:22`; 7 more in `processor/source-manifest/payload_registry.go:13-57`) | none |
| semteams (beta.160) | framework types only — contracts re-declare the framework's loop-execution and lesson contract structure using the framework's `*MessageType().Key()` builders (`cmd/semteams/main.go:971,998`), not copied strings; lifecycle births via `Manager` (`flowtemplates/loader.go:200`) | own types registered (`research/artifact.go:253`, `devviaspec/plan.go:148`, `semsource/payload.go:39-69`) | none at ingest; the copied literals conflict with `agentic-lessons/spec.md:193-206` |
| semmachina (beta.160) | 4 birth types: `semmachina.campaign_entity.v1` (`internal/campaign/gate.go:32-36,395`), `semmachina.turn_state.v1` (`internal/turn/recorder.go:32-36,344`), `…knowledge_grant_entity.v1`, `…revelation_receipt_entity.v1` (`internal/projectioncontract/contracts.go:106-109`; stamped `internal/knowledge/granter.go:282-284`); 7 birth contracts total (`contracts.go` `BirthPredicates` ×7) | **no** — `constants.go:63-147` records each as "deliberately NOT registered: no message of this type is ever published"; `TestSubmitAction_IsDeliberatelyUnregistered` (`submitaction_test.go:364`) pins one; 10 other categories registered (`internal/payload/registry.go:27-72`). **D2:** also births `lifecycle.harness.v1` through `lifecycle.NewManager` (`internal/boot/components.go:51`, `engine.go:121`) | register 4; invert the deliberate-unregistered tests; the rationale is the framework's own retired one (`loop_execution_entity.go:157-166`); the harness type is covered once `payloadbuiltins.Register` carries it (`cmd/semmachina/main.go:99` calls it) |
| semmem (`go.mod:1,6,10`: module `github.com/c360/semmem`, `replace github.com/c360/semstreams => ../semstreams`) | 9 `semmem.entity.*.v1` strings on a pre-rename `EntityState` shape (`entity/types.go:180-192`: `Edges`, `ObjectRef`, `MessageType string`) | **no** (`grep payloadregistry` → 0) | the tree does not build against `github.com/c360studio/semstreams`; the federation MVP that motivated #1100 is not in any local tree (`find … federation-mvp.md` → 0); obligation applies when it rejoins |
| semdev (beta.160) | `semdev.intake_event.v1` (`internal/intake/record.go:63,91`), `semdev.standards_source.v1` (`internal/standards/sync.go:143,186`) via `graphown.Creator.Create` (`internal/graphown/create.go:60-86`); lessons via the framework type (`contracts.go:444`) | **no** — only `payloadbuiltins.Register` (`internal/boot/runtime.go:623-624`). **D2:** also births `lifecycle.harness.v1` through `lifecycle.NewManager` (`internal/boot/runtime.go:537,656`, `boot.go:361`) | register 2; the harness type is covered by `payloadbuiltins.Register` (`runtime.go:624`) |
| semdragon (beta.135) | `questdag.unit.v1` (`questdag/unit.go:72,673` on the **pre-ADR-091** subject `graphingest.SubjectEntityCreateWithTriples`); dynamic `semdragons.<event>.v1` written **directly** to `ENTITY_STATES` (`graphclient.go:24,79-95,108`) | **no** (registers `semsource.*` only, `semsource/payload.go:29-50`) | already off the current mutation surface; notice only |
| semconnect (beta.160) | 11 `c360.csapi-*.v1` types (`gateway/cs-api/projection_contracts.go:29-39`) stamped at `graph_mutations.go:159`; contracts declare `IndexingProfile: "content"` (`:44-64`) | **no registry at all** — `cmd/cs-api-server` never calls `payloadbuiltins.Register` or `payloadregistry.New` (grep → 0); the OMS `RegisterPayloads` (`message/oms/register.go:16-22`) is exported for a host; the 11 stamps reach the **host's** graph-ingest | export a `RegisterPayloads` from `gateway/cs-api` for the 11 (floor `content` from the contracts) and have the host composition root call it |

## 7. Adopter seam inventory

Asked for a developer outside this repo who has never opened `payloadregistry/registry.go`.

### 7.1 A product author birthing an entity on the mutation lane

- **Must know today.** (a) `Entity.MessageType` needs three non-empty parts (`canonical_mutations.go:207`); (b) *any* string is
  accepted and persisted; (c) if they want their entities embedded under the right profile they must know that a string-keyed
  table exists in graph-ingest, that a miss means `control`, and that a metric in a namespace they do not scrape fires
  (`component.go:1864-1868`); (d) if they want a birth contract they must know `projection.Contract`, that its `MessageType` is
  optional, and that it is checked only client-side. Four debts — a design finding, not a doc task.
- **If they do nothing.** The entity persists under an unresolvable stamp; embedded anyway (`processor/graph-embedding/component.go:1748-1757`
  returns `true`); the metric increments; no reader ever notices (§2.3). Silent.
- **Where they find out.** A Debug log and a counter — effectively *nowhere*.
- **Should know.** One thing: register the type — the same act they already perform for every fact-lane payload — and the
  framework must tell them at the first write if they did not (typed runtime error naming the key). The gap between (a)–(d)
  and this sentence is the design.
- **Prediction → observation.** Today the caller predicts a fact the framework owns (whether the key is known and what it
  implies). After: the caller declares the type once; ingest observes registration and answers with a coded rejection.

### 7.2 A product author adding a type today (the `/new-payload` checklist)

- **Must know.** The skill's Step 3 (`SKILL.md:51-73`) and Debugging (`:129-134`) name `payloadregistry.Register(&…)` in `init()`
  and `payloadregistry.Global()` — neither exists; the live idiom is `RegisterPayloads(reg *payloadregistry.Registry)` wired into
  `payloadbuiltins.Register` or the binary's composition root (§2.1). Floor and contract are not on the checklist at all.
- **If they do nothing.** Compile error (loud) — but the doc sent them to it. After the design the checklist is the ONE place:
  `Registration{Domain, Category, Version, Description, Factory, IndexingProfile, Contracts}`.
- **Where they find out.** Compile error > (after) boot error from `Register` for an invalid profile or a contract naming a
  different key.

### 7.3 An operator reading `indexing_profile_default_total{message_type}`

- **Today.** The label names a key found in no registration; the constant they grep to says "MUTATION-ONLY … NOT registered"
  (`loop_execution_entity.go:157-166`) — a dead end. **After.** The label names a `Registration` whose `IndexingProfile` is
  empty — the literal to edit.

### 7.4 A federation peer importing a lesson (#1095 slice B)

- **Today.** A lesson has no wire form: no factory, so `base_message.go:301-307` rejects `agentic.agent_lesson.v1` at decode;
  the peer would have to re-wrap as a generic carrier and lose the type. **After.** `AgentLessonEntity` round-trips through
  `BaseMessage`; the serializable form is the struct's fields (every triple **object** is a field, including the immutable
  `created-at`); `Triple.Timestamp` is regenerated at decode, as for every Graphable today.

## 8. Searches that closed empty

- `grep -rn 'message_type\|MessageType' processor/graph-query gateway graph/query*` → 0 (no query-side filter).
- `grep -rn '"graph.events\|EventEntityCreate\b' --include='*.go' .` (non-test) → `gateway/graph-gateway/component.go:1054` only.
- `grep -rn '"harness"' --include='*.go' .` (non-test) → `pkg/lifecycle/manager.go:26` only.
- `grep -n '^func Register\|^func Global' payloadregistry/*.go` → 0.
- `ls openspec/specs | grep -i payload` → 0.
- `grep -rn 'IndexingProfile' processor/agentic-tools/emit_lesson.go agentic/*_entity.go` → 0.
- `find /Users/coby/Code/c360 -name federation-mvp.md` → 0.

## 9. Open evidence questions for the inventory reviewer

1. `web_observation` births have no e2e tier (`grep -rln 'web_search\|http_request' test/e2e/scenarios` → 0; only
   `configs/rules/deep-research/*` reference the tools). Confirm before naming the covering tier.
2. semmachina's `campaign_entity` "deliberately unregistered" test named at `constants.go:145` was not found by name
   (`grep IsDeliberatelyUnregistered` → `submitaction_test.go:364` only) — one exists, the named one may not.
3. Resolved after drafting: `go list -deps ./payloadbuiltins | grep -c processor/graph-ingest` → 0 — `package graphingest`
   tests may import `payloadbuiltins`; the fixture shape is `payloadbuiltins.Register` + a stub-type helper.
4. Revision 2 (after the blind pass): D1 (research floors per-binary), D2 (sister lifecycle births), D3 (42-site test census)
   corrected above; L1, L2, L5 added to §2.4; L3/L4 are design statements (design §6, §8, §11).
6. Revision 4 (narrow re-review, APPROVE WITH CHANGES): the writer census corrected to six writers / four birth-capable / one
   decode-gated (§2.4); model-endpoint gates and the diagnosis `%g` object added (§2.7); #1095 pointer is its 5.3 (§3); the
   hierarchy callers' WARN-and-continue behaviour recorded (§2.4).
5. Revision 3 (after design review round 1): B-1 (the in-process container birth lane) added to §2.2 and §2.4; F-2 builder
   facts (two web sources, diagnosis confidence) added to §2.7; §3 re-premised at `7e7ea76e`; semconnect row reworded (N-2).
