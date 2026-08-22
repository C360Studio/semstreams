# Issue #1030 inventory — product/operator lesson-write E2E coverage

Inventory only. No target state, option, recommendation, artifact delta, or implementation task is included.

## Problem statement

Issue #1030 claims that SemStreams’ only end-to-end lesson proof exercises the model/tool path:

`mock LLM → emit_lesson → LessonStore → graph create → proposed → E2E Promote handler → active → later brief`

The first observed product adopter instead uses:

`product code → construct lesson identity/triples → LessonStore.CreateLesson → LessonCurator → active`

The inventory question is whether SemStreams has a test that exercises that second, product-shaped path against an
assembled SemStreams runtime, and what existing surfaces, claims, consumers, and historical decisions constrain that
territory.

## Baseline and repository state

- SemStreams `HEAD`, local `main`, and `origin/main` all resolve to
  `c5e7255298da0be9dc2823e15e134fe32c77adb3`.
- Current branch: `main`.
- `git status --short`: empty.
- Open SemStreams PR census: `gh pr list --state open ...` returned `[]`.
- One unarchived OpenSpec change exists:
  `openspec/changes/own-lesson-curator-contract`.
- Issue #1030 is open, has no labels or comments, and was created 2026-08-22.
- The known adopter evidence was inspected read-only in sister repository semdev at
  `f54a9432c5bc30713deff30a1da507bb8aa109f5`, branch
  `standards-via-lessons`. Its unrelated dirty files were
  `docs/port-manifest.md` and
  `docs/decisions/0002-disciplined-agentic-engineering-evaluation.md`; neither was touched.

## Surface inventory

### 1. Claimed gap

The core SemStreams-local claim is true, with four qualifications.

1. No SemStreams E2E scenario invokes `LessonStore.CreateLesson` from a product-shaped caller that supplies its own
   entity ID and birth triples.

2. The issue’s quoted search is too narrow as evidence:

   ```text
   grep -rn "LessonStore\|CreateLesson\|NewNATSLessonStore" test/ --include='*.go'
   ```

   It returns zero because SemStreams package tests live beside production packages, not under `test/`. The broader
   census finds:

   - `processor/agentic-tools/emit_lesson_integration_test.go:97-99` constructs
     `NewNATSLessonStore`, but immediately injects it into `EmitLessonExecutor`.
   - `processor/agentic-tools/lesson_promotion_integration_test.go:66-76` also constructs
     the store, but again births through `EmitLessonExecutor`.
   - Neither integration test calls concrete `LessonStore.CreateLesson` from product-authored construction code.
   - `test/e2e/harness/lessoncuration` exposes only promotion.
   - The ops E2E births through the scripted model tool call.

   The covered agent lane does have shipped configured consumers. `configs/flows/lesson-example.json` declares itself
   the smallest runnable agentic flow that both emits and receives lessons, sets `default_role` to `lesson-example`,
   and grants only `emit_lesson` at lines 2-6, 94-101, and 299-361. The production-shaped
   `configs/flows/ops-agent.json` and E2E `configs/flows/ops-agent-test.json` both set `default_role` to `ops` and grant
   `emit_lesson` at lines 98-130 and 305-328, and 97-127 and 300-323, respectively. The gap is not an absence of
   configured agent writers; it is the absence of a SemStreams-owned direct product/operator lesson-store proof.

3. There is no direct concrete-store unit test. Unit tests use the fake
   `recordingLessonStore` at
   `processor/agentic-tools/emit_lesson_test.go:41`, while
   `TestRequireSameLessonIdentity` at
   `processor/agentic-tools/emit_lesson_test.go:854` covers only the private conflict comparison helper.

4. The concrete NATS store is reached through live NATS in two integration tests, but only behind the agent executor.
   Thus the accurate current statement is:

   > SemStreams has unit and integration coverage for the agent writer and indirect live-NATS coverage of the
   > concrete store, but no SemStreams E2E or integration proof whose producer is a product/operator caller
   > constructing lesson birth identity and triples directly.

5. SemStreams E2E does contain direct generic graph creates. The shared graph-roundtrip canary constructs a local
   projection contract and uses the public contract-bound `projection.MutationClient.Create`; structural and research
   scenarios seed entities through the internal raw `graphmutation.Client.Create`. None calls `LessonStore`, uses the
   lesson message type or projection contract, builds the lesson birth predicate set, reads lesson status, or curates
   the created entity. They are important same-class implementation precedents, not existing lesson-write coverage.

6. The claim is SemStreams-local, not ecosystem-wide. The current semdev adopter has its own product E2E:
   `../semdev/test/e2e/standards_journey_test.go:48-118`.
   It boots the real product runtime, provisions standards, observes active
   `agent.lesson.record` entities with real evidence, and re-runs the production sync to prove idempotency. That
   downstream proof does not create a SemStreams-owned regression gate.

### 2. Current spellings of lesson birth and direct writing

#### Entity and semantic identity

- The typed origin is `agentic.agent_lesson.v1`:
  `agentic/agent_lesson_entity.go:20-37`.
- The entity shape is
  `{org}.{platform}.agent.lesson.record.{id}`:
  `agentic/agent_lesson_entity.go:39-75`.
- `AgentLessonEntityID` panics on bad caller-supplied identity parts:
  `agentic/agent_lesson_entity.go:54-71`.
- The prefix used by injection readers is
  `{org}.{platform}.agent.lesson.record`:
  `agentic/agent_lesson_entity.go:77-92`.
- Lesson vocabulary occupies
  `vocabulary/agentic/predicates.go:812-908`.
- The canonical projection declaration classifies eleven birth predicates and three lifecycle predicates:
  `internal/builtinprojection/contracts.go:51-79`.

#### Agent-facing writer

- `emit_lesson` is the sole agent-facing creation name:
  `processor/agentic-tools/emit_lesson.go:26-29`.
- Its public tool schema asks for intent, not identity:
  `processor/agentic-tools/emit_lesson.go:347-410`.
- It derives the UUID from category, sorted scope, summary, and sorted evidence:
  `processor/agentic-tools/emit_lesson.go:495-501`,
  `processor/agentic-tools/emit_lesson.go:642-674`.
- It builds the complete official birth triple set:
  `processor/agentic-tools/emit_lesson.go:676-743`.
- It applies control-byte, byte-bound, evidence, polarity, and scope grammar checks:
  `processor/agentic-tools/emit_lesson.go:746-913`.
- It applies a per-loop cap before writing:
  `processor/agentic-tools/emit_lesson.go:479-492`.
- It finally calls the store:
  `processor/agentic-tools/emit_lesson.go:517-527`.
- Three shipped flow configurations consume the registered agent-facing writer:
  - `configs/flows/lesson-example.json:2-6,94-101,299-361` is the smallest copyable emit-and-receive flow, dispatches
    role `lesson-example`, grants only `emit_lesson`, and documents its graph-mutation output.
  - `configs/flows/ops-agent.json:98-130,305-328` dispatches role `ops` and grants `emit_lesson` among the production
    ops tools.
  - `configs/flows/ops-agent-test.json:97-127,300-323` is the E2E ops configuration with the same role and grant.
- These configurations make manually dispatched model/tool lesson birth runnable through `cmd/semstreams` or the E2E
  binary. They do not create a product/operator direct-store caller or an automatic completion-to-ops trigger.

#### Exported direct store surface

- `LessonStore` is exported at
  `processor/agentic-tools/emit_lesson.go:112-129`.
- Its methods are:
  - `CreateLesson(ctx, entityID, msgType, triples)` at line 124.
  - `ReadLessonStatus(ctx, entityID)` at line 128.
- `NewNATSLessonStore` is exported at
  `processor/agentic-tools/emit_lesson.go:197-202`.
- Its documentation says “Wire this into the emit_lesson executor,” not that it is a fully prepared product birth API.
- The constructor discards the error from `graphmutation.NewClient`:
  `processor/agentic-tools/emit_lesson.go:199-201`.
- A fresh `CreateLesson`:
  - accepts caller-supplied entity ID, message type, and triples;
  - calls the generic strict graph create operation;
  - does not invoke `parseEmitLessonArgs`;
  - does not invoke `buildEmitLessonTriples`;
  - does not derive an ID;
  - does not validate the 320-byte limit, control-byte hygiene, enums, proposed status, or complete birth predicate set.
  Evidence:
  `processor/agentic-tools/emit_lesson.go:204-217`.
- On `EntityExists`, it exact-reads and verifies:
  - the message type;
  - exactly one category and summary;
  - nonempty evidence and applies-to;
  - equality of those four identity fields.
  Evidence:
  `processor/agentic-tools/emit_lesson.go:219-244`,
  `processor/agentic-tools/emit_lesson.go:247-313`.
- Polarity, severity, detail, injection form, status, created-at, observed role, and executed-by are not conflict identity
  fields.
- `ReadLessonStatus` exact-reads and returns the first string status or an empty found status:
  `processor/agentic-tools/emit_lesson.go:328-345`.

#### Generic typed graph-create spelling

- The admitted graph mutation protocol names four operations, including strict
  `entity.create`:
  `internal/graphmutation/protocol.go:10-45`.
- The internal wire client makes one request and does not retry ambiguous delivery:
  `internal/graphmutation/client.go:70-99`,
  `internal/graphmutation/client.go:142-175`.
- The public contract-bound client exposes typed `CreateMutation`:
  `pkg/projection/mutation_types.go:16-40`.
- It validates entity pattern and declared predicates before the wire call:
  `pkg/projection/mutation_client.go:132-173`.
- Graph-ingest’s server admits the generic request, validates message type, subjects, and the generic entity-state
  contract, then performs strict KV create:
  `processor/graph-ingest/canonical_mutations.go:199-257`.
- Graph-ingest does not know the lesson projection contract or lesson writer gates on this raw create lane.

#### Assembled-E2E direct-create spellings

- `GraphRoundTripProbe` is the product-like, public, contract-bound precedent:
  - it constructs an exact fixture contract and `projection.MutationClient` at
    `test/e2e/scenarios/graph_roundtrip.go:230-248`;
  - it calls `Create` with caller-owned entity identity, triples, request/trace metadata, source, and timestamp at
    `test/e2e/scenarios/graph_roundtrip.go:83-120`;
  - it requires `CommitVerified`, reconciles the declared title group, exact-reads authoritative state and KV
    revision, polls two GraphQL views, verifies mutation trace entries, and queries Message Logger KV evidence at
    `test/e2e/scenarios/graph_roundtrip.go:121-180`;
  - it is the standalone core graph canary and is shared by every tier at
    `test/e2e/scenarios/graph_roundtrip_scenario.go:10-66` and `test/e2e/scenarios/tiered.go:258-266,412-420`.
- `tiered_structural.go` contains three internal raw-client fixture/assertion producers:
  - canonical create without hierarchy inference at lines 417-474;
  - relationship create without target-stub birth at lines 477-535;
  - event-time fixture create followed by bounded temporal-query polling at lines 1254-1325.
  They manually supply `graph.CreateEntityRequest` and use `graphmutation.NewClient`; the first two inspect the
  response revision and exact-read absence/presence, while the temporal test ignores the create receipt and observes
  derived query behavior.
- `research-graph/scenario.go:334-368` uses the internal raw client to seed one search fixture before dispatching the
  research parent task. It records the returned ID/revision; the scenario’s deterministic embedding-search responder
  and later research pipeline consume the seeded ID (`research-graph/scenario.go:161-172,370-390`).
- Semantic census across `test/e2e`, `cmd/e2e`, and `cmd/e2e-semstreams` found no other
  `graph.CreateEntityRequest`, `projection.CreateMutation`, direct graph-create subject publisher, or graph mutation
  client `Create` call. `cmd/e2e-semstreams/main.go:390` is `component.Manager.Create`, not graph entity creation.
- The public graph-roundtrip pattern validates generic contract composition and observation. The raw structural and
  research patterns are harness fixture seeding. None is a lesson-specific store, operation, lifecycle owner, status
  reader, idempotent re-emit proof, or recovery path.

#### Current product adopter spelling

The observed semdev product implementation uses the exported store directly:

- Production construction:
  `../semdev/internal/standards/wiring.go:31-100`.
- `NewNATSLessonStore` is assigned to its `Syncer` at
  `../semdev/internal/standards/wiring.go:84-86`.
- `Syncer` retains the public `agentictools.LessonStore` interface:
  `../semdev/internal/standards/sync.go:113-125`.
- It directly calls `CreateLesson` at
  `../semdev/internal/standards/sync.go:193-220`.
- It reads status, then calls its curator at
  `../semdev/internal/standards/sync.go:220-242`.
- It locally constructs proposed status plus the lesson predicates:
  `../semdev/internal/standards/sync.go:253-284`.
- It locally mirrors the canonical identity byte encoding:
  `../semdev/internal/standards/sync.go:463-476`.
- It owns a separate UUID namespace:
  `../semdev/internal/standards/sync.go:134-138`.
- It locally mirrors the 320-byte value:
  `../semdev/internal/standards/standards.go:49-53`.
- It locally checks control bytes:
  `../semdev/internal/standards/standards.go:221-229`.
- It locally enforces the rendered 320-byte bound:
  `../semdev/internal/standards/standards.go:343-353`.

Two semdev comments currently claim “the store rejects” an oversized form or control bytes:
`../semdev/internal/standards/standards.go:49-52` and
`../semdev/internal/standards/standards.go:221-225`.
Current SemStreams `CreateLesson` does not perform those checks. This is a present cross-repository claim collision,
not a hypothetical one.

### 3. Producers, consumers, and composition roots

#### SemStreams production producer census

The non-test SemStreams census for
`LessonStore|CreateLesson|NewNATSLessonStore` has only:

- interface and implementation in `emit_lesson.go`;
- executor call at `emit_lesson.go:527`;
- one constructor use in
  `processor/agentic-tools/executors/register_emit_lesson.go:19-25`.

There is no SemStreams product/operator direct producer outside the agent executor.

`registerEmitLesson` is called through builtin tool registration and is the production store consumer:
`processor/agentic-tools/executors/register_emit_lesson.go:12-32`.

At configured-runtime level, the agent executor has three shipped consumers: the copyable lesson example and the
production/test ops flows listed above. “No direct producer” applies only to product-authored calls to `LessonStore`,
not to configured model/tool use of `emit_lesson`.

#### Production composition root

- `cmd/semstreams/main.go:219-253` wires the shared graph runtime and builtin tools.
- It does not construct a `LessonCurator`.
- It does not subscribe any lesson operator-control request subject.
- The production direct-store surface is therefore a Go library API consumed by external composition, not an
  operation exposed by the SemStreams binary.
- When `cmd/semstreams` is loaded with `configs/flows/lesson-example.json` or `configs/flows/ops-agent.json`, its
  builtin tool registration and configured `allowed_tools` make the agent-facing writer available. This is a real
  production-binary consumer path, but dispatch remains user/model driven in the shipped configuration.

#### E2E composition root

- `cmd/e2e-semstreams/main.go:152-167` wires the graph runtime, constructs
  `NewLessonCurator`, and subscribes the E2E-only promotion handler.
- The subscription is held as a root resource:
  `cmd/e2e-semstreams/main.go:263-269`.
- It is unsubscribed during root shutdown:
  `cmd/e2e-semstreams/main.go:271-287`.
- This curation control is absent from `cmd/semstreams`.

#### Injection consumer

- Every agentic-loop component wires a NATS lesson reader:
  `processor/agentic-loop/component.go:305-308`.
- The reader queries all lesson records by prefix:
  `processor/agentic-loop/lessons.go:17-100`.
- Role becomes the current `tag:<role>` scope:
  `processor/agentic-loop/lessons.go:149-163`.
- Only matched active lessons render:
  `processor/agentic-loop/lessons.go:165-182`.
- Read failure silently omits the lesson block for that dispatch:
  `processor/agentic-loop/lessons.go:22-25`.

### 4. Current unit, integration, and E2E coverage

#### Unit

`processor/agentic-tools/emit_lesson_test.go` covers executor behavior using a fake store:

- create: line 135;
- detail/injection separation: 214;
- created-at: 234;
- idempotent re-emit: 258;
- persisted-status reporting: 294;
- read-back failure: 322;
- identity: 349;
- evidence: 386;
- injection bounds: 421 and 443;
- scope grammar: 465;
- control bytes: 523 and 560;
- cap: 595 and 619;
- attribution: 657;
- severity/polarity: 693 and 717;
- required fields/schema/routing: 742-832;
- store failure budget release: 832;
- private conflict identity: 854.

These tests prove agent-layer preparation. They do not prove raw caller-supplied product triples through the concrete
store.

The E2E promotion adapter has handler unit coverage:
`test/e2e/harness/lessoncuration/handler_test.go:18-62`.

#### Integration

- `processor/agentic-tools/emit_lesson_integration_test.go:3-9` explicitly describes a production tool-wire test.
- It registers the concrete store under the executor:
  `processor/agentic-tools/emit_lesson_integration_test.go:67-110`.
- It publishes an enveloped tool call to
  `tool.execute.emit_lesson`:
  `processor/agentic-tools/emit_lesson_integration_test.go:139-165`.
- It proves typed birth, proposed status, attribution, evidence/scope, and no second KV revision:
  `processor/agentic-tools/emit_lesson_integration_test.go:182-236`.
- The #1029 integration composes the public projection snapshot and curator but births by calling the executor:
  `processor/agentic-tools/lesson_promotion_integration_test.go:38-83`.
- It then proves promotion, supersession, retirement, birth-fact preservation, and re-emit:
  `processor/agentic-tools/lesson_promotion_integration_test.go:85-124`.
- Promotion request subscription lifecycle is tested at
  `test/e2e/harness/lessoncuration/handler_integration_test.go:14-40`.

No integration test calls `NewNATSLessonStore(...).CreateLesson(...)` directly with product-authored identity and
triples.

#### SemStreams E2E

- The ops scenario describes itself as:
  `emit_lesson → proposed → promote → brief injection` at
  `test/e2e/scenarios/ops/scenario.go:127-160`.
- Its nine stages are at
  `test/e2e/scenarios/ops/scenario.go:193-247`.
- It seeds loop/evidence records directly into KV:
  `test/e2e/scenarios/ops/scenario.go:342-478`.
  The direct KV writer is only evidence setup, not lesson birth.
- It dispatches the lesson-producing loop by user message:
  `test/e2e/scenarios/ops/scenario.go:481-517`.
- It discovers the proposed lesson from graph output:
  `test/e2e/scenarios/ops/scenario.go:692-730`.
- It invokes the typed E2E promotion request:
  `test/e2e/scenarios/ops/scenario.go:733-791`.
- It proves injection via a later loop:
  `test/e2e/scenarios/ops/scenario.go:812-857`.
- The mock scripts three diagnoses and one `emit_lesson`:
  `test/e2e/mock/cmd/main.go:173-301`.
- The lesson call is specifically at
  `test/e2e/mock/cmd/main.go:253-270`.

There is no product-shaped lesson birth stage.

The broader assembled-E2E suite does exercise direct graph-create shapes:

- `test/e2e/scenarios/graph_roundtrip.go:83-180,230-248` uses the public contract-bound mutation client and observes
  verified commit, authoritative read-back, GraphQL convergence, transport trace, and KV evidence. It is product-like
  in client composition, caller-owned identity, and caller-owned triples, but its semantic contract is the local
  `test.fixture.v1` canary, not `agentic.agent_lesson.v1`.
- `test/e2e/scenarios/tiered_structural.go:417-535,1254-1325` uses the internal raw client for three structural
  fixture/assertion entities.
- `test/e2e/scenarios/research-graph/scenario.go:334-390` uses the internal raw client to seed the search entity that
  enables the later research pipeline.

These patterns close the empty category “assembled E2E direct graph creation.” They do not close “assembled E2E
direct lesson-store creation,” because they neither call `LessonStore.CreateLesson` nor exercise lesson identity,
birth triples, proposed status, conflict verification, curation, or later brief injection.

#### Downstream E2E

Semdev’s current branch does have an assembled product proof:

- birth and activation claim:
  `../semdev/test/e2e/standards_journey_test.go:3-16`;
- real runtime and provision:
  `../semdev/test/e2e/standards_journey_test.go:48-66`;
- active status, scope, and evidence assertions:
  `../semdev/test/e2e/standards_journey_test.go:68-102`;
- production resync idempotency:
  `../semdev/test/e2e/standards_journey_test.go:104-118`,
  `../semdev/test/e2e/standards_journey_test.go:164-203`.

That proof belongs to an active sister-repo change, not to SemStreams’ test ladder.

### 5. E2E lesson curation request surface

The only E2E-specific typed lesson operation is promotion:

- subject:
  `e2e.control.lesson.promote` at
  `test/e2e/harness/lessoncuration/contract.go:5-6`;
- request:
  entity ID only at lines 8-11;
- response:
  `Promoted bool` at lines 13-16;
- narrow dependency:
  `Promoter.Promote` at
  `test/e2e/harness/lessoncuration/handler.go:13-17`;
- decode, ID validation, invoke, response:
  `test/e2e/harness/lessoncuration/handler.go:19-45`.

Repository-wide searches found no E2E-specific lesson create/write/birth subject, request, response, handler, or
interface.

### 6. Configured lesson consumers, ops flow, and ADR-080 trigger claims

The shipped repository has manually dispatched configured `emit_lesson` consumers:

- `configs/flows/lesson-example.json:2-6` describes the smallest runnable flow that both emits and receives lessons.
- Its dispatcher stamps `default_role: lesson-example` at lines 94-101, its tools component grants only
  `emit_lesson` at lines 299-310, and graph-ingest is declared as the authority writer at lines 360-361.
- `docs/concepts/32-agent-memory.md:154-214` presents the same file as the worked runnable example.
- `configs/flows/ops-agent.json:98-130,305-328` and
  `configs/flows/ops-agent-test.json:97-127,300-323` dispatch role `ops` and grant `emit_lesson`.

The ops E2E is one such manually dispatched consumer. It does not prove automatic completion-triggered debrief:

- `configs/flows/ops-agent.json:98-145` configures an `agentic-dispatch` whose default role is `ops` and which consumes
  both user messages and `agent.complete.*`.
- `agent.complete.*` is also the loop output at
  `configs/flows/ops-agent.json:245-252`.
- `configs/flows/ops-agent.json:305-328` grants `emit_lesson`.
- No rule processor is present in `ops-agent.json` or `ops-agent-test.json`.
- `rg -n '"role"\s*:\s*"ops"' configs --glob '*.json'` returned zero.
- No checked-in rule dispatches the `ops` role.
- `processor/agentic-dispatch/terminal_settlement.go:155-209` consumes terminal events to settle tracking and publish
  user responses; route-less events are simply settled at lines 192-195.
- The E2E explicitly sends a user message:
  `test/e2e/scenarios/ops/scenario.go:481-517`.

ADR-080 nevertheless says the reference ops flow fires per loop completion:
`docs/adr/080-push-based-agent-memory-and-lesson-artifacts.md:68-75`.

Thus the qualified discrepancy is specifically the absent automatic completion-to-ops dispatch claimed by ADR-080,
not an absence of configured production consumers for the agent writer. It does not alter the direct-write coverage
gap.

Two additional current documentation discrepancies exist:

- `docs/concepts/32-agent-memory.md:165-168` says to run
  `task e2e:agentic` for the ops lesson round trip; the actual tier is
  `task e2e:ops`.
- `taskfiles/e2e/ops.yml:3-5` still says the mock scripts
  `submit_work`; the current mock terminates naturally and documents that
  `submit_work` is not advertised at
  `test/e2e/mock/cmd/main.go:180-211`.

### 7. Adjacent specs, ADRs, changes, and issues

#### Current capability spec

`openspec/specs/agentic-lessons/spec.md` states:

- evidence-cited, content-derived creation:
  lines 38-62;
- 320-byte rejection:
  lines 64-77;
- typed scope grammar:
  lines 79-92;
- `emit_lesson` is the only **agent-facing** creation path:
  lines 94-111;
- product curation lifecycle:
  lines 113-133.

The “only agent-facing” wording does not prohibit direct product creation. The generic phrases “framework SHALL
persist” and “writer MUST reject” cover the lesson semantic contract, while current enforcement resides only in the
tool parser, not in `LessonStore.CreateLesson`. That is the #979 overlap.

#### ADR-080

ADR-080:

- makes lessons evidence-cited, bounded, typed-scope, content-derived, and born proposed:
  `docs/adr/080-push-based-agent-memory-and-lesson-artifacts.md:51-67`;
- assigns debrief emission to the ops role:
  lines 68-75;
- says framework owns the lesson vocabulary and writer while products own category, policy, scope, and trigger
  semantics:
  lines 95-101;
- permanently rejects separate memory storage, query tools, pattern mining, and ungated prompt injection:
  lines 103-114.

#### ADR-097 / active #1029 change

ADR-097 concerns curator contract composition, not lesson birth:

- it retains local composition-root clients and narrow interfaces:
  `docs/adr/097-built-in-lesson-curation-owns-contract-composition.md:25-37`;
- `NewNATSLessonCurator` remains retired:
  lines 33-34;
- it adds no NATS factory, wire, storage, or global catalog:
  lines 39-48.

The active change confirms:

- no NATS curator factory:
  `openspec/changes/own-lesson-curator-contract/proposal.md:23-30`;
- no bespoke agent/persona/role/framework agent:
  proposal line 21 and
  `openspec/changes/own-lesson-curator-contract/specs/agentic-lessons/spec.md:10-13`;
- tasks 10 and 11 remain open for downstream communication and adoption evidence:
  `openspec/changes/own-lesson-curator-contract/tasks.md:14-15`.

Issue #1030 shares the existing promotion handler and curator composition but is a birth-coverage question. It must
not silently reopen the retired curator factory decision.

#### Issue #979

Issue #979 is open and directly overlaps the raw store surface:

- direct `CreateLesson` bypasses writer gates;
- content identity is private;
- constants are private.

Those findings are confirmed by current code and semdev’s mirrors. Per the assigned non-goal, #1030 does not
authorize resolving #979 unless a coverage proof cannot be expressed without doing so.

#### Issue #844

Issue #844 is closed. It documented that `e2e:ops` promotion failed while the tier reported success. PR #943 restored the
production-target composition and an exact beta.160 candidate passed all nine stages.

Current task behavior is fail-closed:

- the E2E CLI returns 1 on `result.Success == false`:
  `cmd/e2e/main.go:492-526`;
- the ops task captures the exit status, tears down, and exits with it:
  `taskfiles/e2e/ops.yml:18-30`.

The old `ignore_error` defect is not present.

#### Issue #769 and CI ladder

Issue #769 remains open and proposes nightly `e2e:semantic` plus `e2e:agentic`; it does not include `e2e:ops`.

Current ladder:

- per-PR E2E runs statistical only:
  `.github/workflows/e2e-ladder.yml:60-77`;
- sister-validation’s SemStreams comparison is dispatch-only core:
  `.github/workflows/sister-validation.yml:142-162`;
- `task e2e:all` runs core, inference tiers, and agentic, but not ops:
  `Taskfile.yml:182-187`;
- `rg -n 'e2e:ops' .github` returned zero.

Therefore no current CI workflow or `e2e:all` executes the only SemStreams lesson E2E.

### 8. Historical and archived decisions

#### Original lesson substrate

Commit
`338e847e8b7e823a14d803c97bddeb9acc107ff2`
introduced both `LessonStore/CreateLesson/NewNATSLessonStore` and the lesson E2E.

Archived task truth shows the intended test split:

- task 3.4 required production tool-wire integration:
  `openspec/changes/archive/2026-07-19-agent-memory-lesson-substrate/tasks.md:47-49`;
- task 5.2 required ops-loop emit, proposed, promotion, and later brief injection:
  lines 99-107;
- recorded proof explicitly says “loop-1 emits”:
  lines 109-118.

The archived spec’s creation scenarios are all phrased through `emit_lesson`:
`openspec/changes/archive/2026-07-19-agent-memory-lesson-substrate/specs/agentic-lessons/spec.md:13-29`.

No archived task or scenario required a product/operator direct birth proof.

#### Promotion control history

Before the projection-client migration, the ops scenario constructed
`NewNATSLessonCurator` locally. Commits `ff1b162c` and `9a48638d` moved promotion behind the E2E-only typed request
handler when curation adopted contract-bound projection clients.

Commit `9a48638d` deliberately removed `NewNATSLessonCurator`; ADR-097 records the bounded zero-reference/parity reason:
`docs/adr/097-built-in-lesson-curation-owns-contract-composition.md:16-23`.

That retired helper concerned lifecycle curation over legacy adapters. It was not a direct lesson-birth constructor or
E2E write hook.

#### Empty historical categories

Exact history searches:

```text
git log -S'NewNATSLessonStore' -- processor/agentic-tools/emit_lesson.go
→ only 338e847e (2026-07-19 introduction)

git log -S'CreateLesson(ctx' -- processor/agentic-tools/emit_lesson.go
→ only 338e847e (2026-07-19 introduction)

git log --all -G'Subject(Create|Write|Birth).*Lesson|e2e\.control\.lesson\.(create|write|birth)' \
  -- test/e2e cmd/e2e-semstreams
→ no commits

git log --all -G'New(NATS)?Lesson(Writer|Creator)|CreateLessonRequest|WriteLessonRequest' \
  -- processor test/e2e cmd docs openspec
→ no commits
```

No prior direct-write E2E hook was found. No `NewLessonWriter`, `NewLessonCreator`, typed `CreateLessonRequest`,
`WriteLessonRequest`, or rejected public lesson-birth helper was found. The one historical public helper decision is
the unrelated curator factory.

## Same-class collision table

The issue’s suggested territory could add or alter an E2E communication operation, so the existing same-class owners
are inventoried without selecting among them.

| Dimension | Exported direct Go store | Canonical graph mutation operation | Agent lesson tool lane | E2E lesson promotion control |
|---|---|---|---|---|
| Semantic class | Purpose-named lesson strict birth plus status read | Generic typed entity strict birth | Model/tool-authored lesson preparation and birth | Operator-style lesson lifecycle promotion |
| Owners | `processor/agentic-tools` `LessonStore` and `natsLessonStore` (`emit_lesson.go:112-129,187-245`) | `internal/graphmutation`, `pkg/projection`, graph-ingest (`protocol.go:10-45`; `mutation_client.go:132-173`; `canonical_mutations.go:199-257`) | `EmitLessonExecutor`, builtin executor registration, agentic-tools component (`emit_lesson.go:131-171,444-558`; `register_emit_lesson.go:12-32`) | `test/e2e/harness/lessoncuration` plus E2E composition root (`contract.go:1-16`; `handler.go:13-45`; `cmd/e2e-semstreams/main.go:152-167`) |
| Catalogs | Exported Go interface only; no subject/config/payload catalog | Admitted operation enum and subject resolver; public projection contracts | Tool registry; `allowed_tools` in `lesson-example`, `ops-agent`, and `ops-agent-test`; tool schema | One constant subject and two Go structs; no config or registry |
| Status | Returns `created`; separate status read may return found with empty status | Typed mutation response/receipt and KV revision | Tool result reports lesson ID, created, persisted status (`emit_lesson.go:426-442,537-572`) | Boolean `Promoted`; graph status is authoritative |
| Lifecycle | Per-call library object; constructor has no start/stop | Graph-ingest subscriptions start with component runtime | Agentic-tools component consumes work; executor shared across loops; per-loop cap map | Subscribed at E2E boot and explicitly unsubscribed at root shutdown |
| Ownership | Client of graph-ingest; no owner token or lease | Graph-ingest is sole physical ENTITY_STATES writer | Client of graph-ingest; content namespace makes identical agent calls converge | Client of contract-bound graph runtime; test handler never becomes physical owner |
| Readers | Store exact-reads conflict identity and status; semdev calls status | Response caller; public authoritative reader | Agent receives tool result; integration reads KV | Ops scenario decodes response, then queries graph |
| Writers | Agent executor in SemStreams; semdev standards sync externally | Every admitted graph-create producer, including store, graph-roundtrip projection client, and raw E2E clients | Mock/model-generated tool call; builtin executor writes through store; three shipped flows allow it | Ops E2E scenario sends promotion request; curator reconciles lifecycle |
| Recovery/failure | Strict conflict exact-read verifies four identity fields; non-conflict errors return; no hidden create retry | One wire request; commit-unknown is explicit; strict existing-ID conflict | Identical repeat derives same ID; integration proves no second KV revision | Plain request/reply, no replay; handler returns classified errors; #844 task now propagates scenario failure |
| Durable state | No separate store; entity lands in `ENTITY_STATES` | `ENTITY_STATES` current authority | Same lesson entity | Same entity, lifecycle group only |

No second durable lesson store, bucket, payload type, or recovery ledger was found.

### Assembled-E2E direct-create collision detail

| Dimension | Public contract-bound graph roundtrip | Internal raw structural creates | Internal raw research seed |
|---|---|---|---|
| Semantic class | Product-like generic contract-bound create/reconcile/read canary | Generic authority fixture births proving no hierarchy/stub and event-time behavior | Generic search fixture birth before agentic research dispatch |
| Writer | `GraphRoundTripProbe` calls `projection.MutationClient.Create` (`graph_roundtrip.go:83-120,230-248`) | Three `graphmutation.Client.Create` calls (`tiered_structural.go:417-535,1254-1285`) | `injectParentTask` calls `graphmutation.Client.Create` (`research-graph/scenario.go:334-368`) |
| Contract/admission | Caller constructs a named exact entity/type/predicate contract; public client validates it before the admitted create | No projection contract; raw request relies on graph-ingest’s generic validation | No projection contract; raw request relies on graph-ingest’s generic validation |
| Reader/observer | Public authoritative reader, two GraphQL views, Message Logger mutation trace, and Message Logger KV query (`graph_roundtrip.go:149-180`) | Response entity/revision plus exact reader for first two; temporal search polling for third (`tiered_structural.go:455-473,511-534,1288-1325`) | Returned entity/revision recorded; deterministic search responder and later pipeline use the seed ID (`research-graph/scenario.go:161-172,367-390`) |
| Status | Requires `projection.CommitVerified`, nonzero authoritative KV revision, replacement convergence, and correlated trace/KV evidence | First two retain create KV revision; temporal create ignores receipt and treats subsequent query results as acceptance | Retains returned entity ID and KV revision in scenario details |
| Lifecycle | Per-probe client; create then reconcile in one bounded run; standalone scenario owns and closes NATS connection (`graph_roundtrip_scenario.go:31-66`); shared by every tier | Per-stage raw client; unique IDs for first two, one fixed event-time fixture ID; no entity delete/cleanup; tier scenario owns the shared NATS client (`tiered.go:183-222,556-565`) | Scenario initializes unique or controlled seed ID; seed precedes task dispatch; scenario unsubscribes and closes its NATS client (`research-graph/scenario.go:159-199`) |
| Recovery/failure | Unique trace-derived ID; one create call; no retry of ambiguous create; bounded GraphQL/trace polling adds observation, not redelivery | One create call per fixture; errors fail the stage; bounded temporal poll handles derived-index lag only | One create call; errors fail before task dispatch; no create replay or conflict interpretation |
| Lesson relevance | Demonstrates the generic public adopter mechanics, but no lesson type, contract, birth gates, status read, curation, or injection | Raw fixture seeding only; no lesson semantics | Raw fixture seeding only; no lesson semantics |

The semantic recheck triggers no additional same-class lesson operation. It adds one generic product-like public
construction precedent and two raw harness fixture classes to the collision inventory; none changes the lesson-store
consumer census.

## Consumer-at-birth inventory

Present consumers of current outward surfaces:

- `LessonStore`:
  - SemStreams: only `EmitLessonExecutor`.
  - External observed consumer: semdev standards sync.
- `NewNATSLessonStore`:
  - SemStreams production: `registerEmitLesson`.
  - External observed consumer: semdev `NewProvisionSync`.
- `LessonProjectionContract`:
  - SemStreams integration proof.
  - Intended external migration consumer recorded by #1029; active change task 11 says adoption evidence is not yet
    recorded.
- `projection.MutationClient.Create` in assembled E2E:
  - standalone core graph-roundtrip canary;
  - every tiered graph-roundtrip stage.
- Internal `graphmutation.Client.Create` in assembled E2E:
  - three structural stages;
  - research-graph seed setup.
- `NewLessonCurator`:
  - SemStreams E2E composition.
  - SemStreams tests.
  - semdev product composition.
- `e2e.control.lesson.promote`:
  - only the ops E2E scenario.
- Direct product lesson birth E2E request:
  - no symbol exists;
  - therefore no present consumer exists to census.

## Adopter seam inventory

Specific adopter: a semdev product developer implementing repo-declared standards as lessons, who has not opened
`emit_lesson.go`.

### What they must know today

1. Birth identity mechanics:

   - the entity must be
     `{org}.{platform}.agent.lesson.record.{id}`;
   - the message type is `agentic.agent_lesson.v1`;
   - idempotent conflict verification compares category, summary, sorted evidence, and sorted applies-to;
   - if they want content-derived IDs, they must reproduce the private separator encoding or establish an equivalent
     field-consistent namespace.

2. Complete birth construction:

   - the product supplies the triples itself;
   - status must be `proposed`;
   - created-at, summary, detail, injection form, evidence, applies-to, polarity, severity, and optional attribution
     need product decisions;
   - every triple subject must equal the entity ID;
   - triple source/timestamp/confidence are caller-owned on the raw store path.

3. Writer safety gates:

   - at least one well-formed evidence ID;
   - at least one typed scope key;
   - `id:` scope has at least three segments;
   - injection form is at most 320 bytes;
   - control bytes must be rejected because injection is rendered verbatim;
   - polarity/severity values must align with matcher expectations.

4. Ordering and sequencing:

   - the cited evidence entity must exist before promotion;
   - the store birth precedes status read/promotion;
   - the product must include `LessonProjectionContract()` in its local mutation client;
   - it must inject only reconciler/reader capabilities into `NewLessonCurator`;
   - promotion refusal leaves the lesson proposed;
   - only active, matching lessons reach later briefs.

5. Conflict and retry meaning:

   - `created=false` means an existing entity passed field-based identity verification;
   - a same ID with a different message type or identity field set is a hard collision;
   - the constructor can defer a nil/bad-client failure to the first call;
   - generic mutation commit ambiguity is not an automatic retry signal.

This is materially more than two correctness facts.

The repository does give this developer two adjacent examples, but neither removes those obligations:

- `configs/flows/lesson-example.json` and `docs/concepts/32-agent-memory.md:154-214` show how to choose the agent/tool
  lane, in which the framework derives identity and triples from intent.
- `test/e2e/scenarios/graph_roundtrip.go:83-180,230-248` shows how an external-style caller constructs a public
  contract-bound generic create and observes its actual commit and read-back.

The first is not product-authored direct creation; the second has only a local fixture contract and does not teach the
lesson-specific recipe.

### What happens if they do nothing

- If they do not call a lesson writer at all, no lesson entity is created.
- If they use `LessonStore.CreateLesson` with structurally valid but semantically malformed fresh triples,
  graph-ingest can accept them because the store does not run agent writer gates.
- Missing evidence or missing applies-to can be created on a fresh ID; later promotion refuses missing evidence, while
  missing scope means the lesson cannot match a brief.
- An over-320 or control-byte-bearing injection form can be stored through the current direct path and later rendered
  verbatim if activated.
- A non-`proposed` birth status can be supplied directly.
- An omitted or malformed created-at value weakens deterministic ordering.
- A copied identity encoding can silently drift and turn intended idempotency into duplicates or collisions.
- A copied projection contract can silently drift unless the adopter moves to the new public snapshot.
- If they never promote, the lesson remains non-injectable.
- A transient lesson read during brief assembly silently omits the lesson for that dispatch.

### Where they find out

| Fact | Current discovery rank |
|---|---|
| Entity/message-type helpers | Exported Go API; invalid identity parts panic |
| `LessonStore` methods | Compile-time interface |
| Configured agent emit/receive lane | Shipped `lesson-example` flow and worked concept example |
| Full product birth recipe | No SemStreams product example; private executor code and downstream design |
| Generic contract-bound direct-create mechanics | Public graph-roundtrip E2E canary; not lesson-specific |
| 320-byte value | Agent tool schema/docs; not exported as a product constant |
| Control-byte rule | Private parser/docs; not enforced by direct store |
| Identity byte encoding | Private source; semdev mirror |
| Field-based conflict semantics | Private source/runtime error |
| Required proposed status and birth predicates | Spec/ADR/private builder |
| Evidence existence before promotion | Typed runtime refusal and docs |
| Projection contract composition | Public snapshot docs and constructor validation |
| Bad store constructor dependency | Late runtime error; semdev adds its own boot guards |
| Wrong scope causing no injection | No direct writer error; later silent non-match |
| Transient injection read | Warning log plus silent one-dispatch omission |

Generic client composition and the alternative agent writer are discoverable in shipped examples. Several
lesson-specific direct-writer correctness facts remain discoverable only at doc, private-source, runtime-error, log,
or nowhere-before-silent-behavior levels.

### What they should have to know

For the outward lesson-birth seam, the product developer’s irreducible product knowledge is:

- the lesson content;
- the product’s category and applicability policy;
- the evidence it cites;
- whether and when product policy promotes it.

The gap between that and today’s inventory is the framework-owned birth mechanics, validation limits, identity
encoding, predicate construction, status initialization, and ordering metadata that the adopter currently predicts or
mirrors.

### Prediction-shaped obligations

The direct caller currently predicts framework-owned facts before acting:

- exact identity encoding;
- the 320-byte bound;
- control-byte safety;
- canonical born status;
- the canonical birth predicate set;
- the matcher’s recognized severity/scope spellings;
- source/timestamp/confidence conventions.

The current direct store observes only generic graph-create success or conflict. It does not observe and classify
those lesson-semantic outcomes for the adopter. This is the concrete adopter-seam finding; it does not itself choose a
target.

## Explicit non-goals preserved by this inventory

- No bespoke agent.
- No new LLM persona or prompt role.
- No new framework agent type.
- No change to the current mock’s model role.
- No unrelated ops observability or reporting work.
- No repair of the absent automatic completion-to-ops trigger.
- No sister-repository write or migration.
- No reopening of `NewNATSLessonCurator`.
- No #979 authorization/hardening work unless later evidence proves a coverage-only path cannot be expressed without
  it.
- No new payload, bucket, stream, store, or graph authority.
- No claim that downstream semdev’s current unmerged E2E substitutes for a SemStreams gate.

## Exact search log

```text
git rev-parse HEAD main origin/main
→ all c5e7255298da0be9dc2823e15e134fe32c77adb3

git status --short
→ empty

gh pr list --state open --json ...
→ []

find openspec/changes ... ! -name archive
→ openspec/changes/own-lesson-curator-contract

rg -n "LessonStore|CreateLesson\(|NewNATSLessonStore" . --glob '*.go'
→ interface/implementation/executor registration, fake tests, two executor-backed integrations; no SemStreams direct product caller

rg -n "NewNATSLessonStore|CreateLesson\(" test cmd --glob '*.go'
→ zero direct lesson births

rg -n '"emit_lesson"|default_role|smallest|SMALLEST' \
  configs/flows/lesson-example.json configs/flows/ops-agent.json configs/flows/ops-agent-test.json
→ lesson-example describes the smallest emit/receive flow, uses default_role lesson-example, and grants only emit_lesson; both ops flows use default_role ops and grant emit_lesson

rg -n 'CreateEntityRequest|projection\.CreateMutation|\.Create\(' \
  test/e2e cmd/e2e cmd/e2e-semstreams --glob '*.go'
→ public projection Create in graph_roundtrip; three internal raw Creates in tiered_structural; one internal raw Create in research-graph; unrelated os.Create and component.Manager.Create hits

rg -n 'graph\.mutation\.entity\.create|CreateEntity|SubjectFamily.*Create|ResolveSubject\([^\n]*Create' \
  test/e2e cmd/e2e cmd/e2e-semstreams --glob '*.go'
→ the same graph-roundtrip trace decoder plus the four raw request constructions; no additional direct subject publisher

rg -n "e2e\.control|SubscribeForRequests\(" test/e2e/harness cmd/e2e-semstreams
→ only e2e.control.lesson.promote

find test/e2e/harness -type f
→ only test/e2e/harness/lessoncuration/{contract,handler,handler_test,handler_integration_test}.go

rg -n '"role"\s*:\s*"ops"' configs --glob '*.json'
→ zero

rg -n 'processor/rule|"name"\s*:\s*"rule"' configs/flows/ops-agent*.json
→ zero

rg -n "e2e:ops" .github
→ zero

rg -n "e2e:ops|e2e:all" Taskfile.yml taskfiles/e2e
→ ops is standalone; e2e:all omits it

rg -n "NewNATSLessonStore|CreateLesson\(" ../semdev
→ production wiring and standards sync direct call plus design/docs/tests

git log -S'NewNATSLessonStore' -- processor/agentic-tools/emit_lesson.go
→ only 338e847e introduction

git log -S'CreateLesson(ctx' -- processor/agentic-tools/emit_lesson.go
→ only 338e847e introduction

git log --all -G'Subject(Create|Write|Birth).*Lesson|e2e\.control\.lesson\.(create|write|birth)' \
  -- test/e2e cmd/e2e-semstreams
→ no commits

git log --all -G'New(NATS)?Lesson(Writer|Creator)|CreateLessonRequest|WriteLessonRequest' \
  -- processor test/e2e cmd docs openspec
→ no commits

git log --all -S'NewNATSLessonCurator' -- .
→ introduced in 338e847e, deliberately removed in 9a48638d; later hits are archival/documentation references
```

## Open evidence questions for inventory review

1. The issue’s phrase “product/operator write lane” maps today to an exported Go interface used by semdev, but
   SemStreams’ own constructor documentation still frames it as an executor dependency. The inventory finds real
   external use but no SemStreams declaration that `LessonStore` is a fully prepared product birth API.

2. Semdev’s current product E2E exists on an active sister branch, not a stable SemStreams-owned gate. Whether owner
   review treats it as supporting evidence or merely the motivating adopter observation remains a ruling.

3. The current semdev comments claim direct-store gate enforcement that current SemStreams code does not perform.
   That mismatch is factual #979 overlap; this inventory does not resolve whether #1030’s eventual coverage should
   lock current behavior or wait on separate hardening.

4. No historical direct-write E2E hook or rejected birth helper was found. The only retired lesson constructor was the
   curator factory, which owned different semantics.

5. A public contract-bound assembled-E2E create precedent exists, but it declares only an exact fixture contract.
   Whether owner review treats that as sufficient generic harness precedent or requires lesson-purpose naming remains
   a later ruling; it does not establish current lesson birth coverage.
