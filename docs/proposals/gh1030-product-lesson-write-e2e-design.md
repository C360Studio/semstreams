# Issue #1030 design — product-shaped lesson-write E2E coverage

Status: superseded before implementation on 2026-08-22 by the owner's independent-product-path correction.

This accepted ops-extension design is retained as immutable decision history. It MUST NOT be implemented.
The correction is `gh1030-product-lesson-write-e2e-design-correction.md`, SHA-256
`299c1adfa94a13af551fc34729f2374a5707fc88d6baf70f2345497ef0f1b8ff`; that artifact requires independent pre-owner
design review and owner acceptance before implementation.

Repository baseline: `c5e7255298da0be9dc2823e15e134fe32c77adb3`.

Accepted evidence base: [Issue #1030 inventory](gh1030-product-lesson-write-e2e-inventory.md), SHA-256
`dccdb50e8e2875ce1cec2b0e2b3f44c15a451a4a6454626e6daeaf2ef1c2c634`. The accepted inventory remains the
verbatim, line-addressable surface and adopter-seam record; this artifact does not revise it.

## Decision requested

Add SemStreams-owned assembled-runtime coverage for the current external adopter shape:

```text
external E2E runner → natsclient.Client → NewNATSLessonStore → CreateLesson
→ proposed → existing typed Promote control → active → later brief injection
```

The recommended boundary is test-only. It adds no production API, subject, handler, payload, bucket, stream, agent,
persona, prompt role, policy, or graph authority.

## Measured premises

| Premise | Measurement |
|---|---|
| No SemStreams E2E or integration producer directly calls `LessonStore.CreateLesson` with caller-authored lesson identity and triples | Accepted inventory lines 37-92 and 306-411; the direct-store semantic search returned no direct lesson births |
| The current external adopter does call the exported store directly | Accepted inventory lines 217-249 cites semdev `wiring.go:31-100` and `sync.go:193-284` |
| The covered model/tool writer already has shipped configured consumers | Accepted inventory lines 119-136 and 432-474 cites `lesson-example`, `ops-agent`, and `ops-agent-test` |
| `NewNATSLessonStore` is an existing purpose-specific typed Go surface | `processor/agentic-tools/emit_lesson.go:112-129,187-245` |
| The store accepts caller identity, message type, and triples, strict-creates, exact-reads an existing entity, and compares four identity fields on conflict | `processor/agentic-tools/emit_lesson.go:204-313` |
| The E2E runtime already owns the typed promotion adapter and its subscription lifecycle | `test/e2e/harness/lessoncuration/contract.go:5-16`, `handler.go:13-45`, and `cmd/e2e-semstreams/main.go:152-167,263-287` |
| The ops scenario already proves the agent lane through proposed, promotion, and later injection in nine counted stages | `test/e2e/scenarios/ops/scenario.go:193-247,692-857`; `scenario_test.go:11-68` |
| The ops mock sequence can park between loops on a marker that is absent until a later active lesson is injected | `test/e2e/mock/cmd/main.go:167-211,271-301` |
| The public projection client supports exact authoritative read and generic revision-fenced delete; `DeleteMutation` carries no contract name | `pkg/projection/mutation_client.go:233-259`; `mutation_types.go:60-65` |
| #1029 requires a composition-root-local projection client built from `LessonProjectionContract`, while `NewNATSLessonCurator` remains retired | ADR-097 lines 25-48 and `openspec/changes/own-lesson-curator-contract/specs/agentic-lessons/spec.md:1-40` |
| Direct-store semantic hardening and public identity/constants are #979 territory | Accepted inventory lines 532-544 and 918-936 |
| No current CI workflow and no `e2e:all` target runs `e2e:ops` | Accepted inventory lines 557-573 |

## Design boundaries

### In scope

- One product-shaped E2E caller constructed outside the assembled SemStreams runtime.
- A direct call to the existing `LessonStore.CreateLesson` with caller-authored, semantically valid triples.
- Observation of born `proposed` status through `LessonStore.ReadLessonStatus` and the existing admitted graph read.
- Promotion through the existing typed E2E Promote control.
- An intentional identical re-create after promotion to prove store conflict convergence and preservation of active
  status.
- A later ops loop whose brief carries a unique direct-product lesson injection marker.
- Typed cleanup of exact scenario-tracked lesson IDs, using generic authoritative read and revision-fenced delete on
  a composition-root-local projection client.
- Exact fail-closed stage and assertion accounting.

### Explicit non-goals

- No bespoke framework agent.
- No new agent role, persona, persona fragment, LLM prompt contract, or prompt role.
- No new framework agent type.
- No automatic completion-to-ops trigger repair.
- No ops observability or reporting change.
- No new production or E2E create subject unless the owner rejects the existing direct Go seam.
- No raw KV write/read/delete or raw subject fallback for the new lesson path.
- No change to #979 semantic-hardening policy, identity policy, validation ownership, or exported constants.
- No assertion that malformed, evidence-free, over-bound, control-byte-bearing, wrongly scoped, or non-proposed input
  is valid.
- No change to #1029's public contract snapshot, local-client composition, narrow curator capabilities, or retired
  `NewNATSLessonCurator` ruling.
- No new payload registration, bucket, stream, store, graph owner, or durable recovery ledger.
- No CI/nightly/e2e-ladder expansion.
- No sister-repository edits, issues, comments, branches, or migration work.

## Options considered

### Option 0 — Do nothing

Keep the agent/tool E2E and downstream semdev proof as the only assembled evidence.

Costs:

- SemStreams does not protect its exported direct lesson-store path against assembled-runtime drift.
- A regression can leave the agent tool green while the first product-shaped writer breaks.
- Downstream semdev remains the only product-level proof, and its active branch is not a SemStreams release gate.
- The adopter-facing behavior and #979 debt remain exactly as inventoried.

This option has no implementation cost but does not close #1030.

### Option A — External runner uses the existing direct lesson store

The E2E scenario reuses the standard `*natsclient.Client` already owned by its `NATSValidationClient`, constructs
`NewNATSLessonStore`, supplies its own lesson entity ID and triples, and calls `CreateLesson` against the assembled
runtime. It then uses the existing typed Promote control and the existing agentic brief/injection proof mechanism.

Benefits:

- Exercises the exact exported surface and ownership split used by the observed product adopter.
- Keeps graph-ingest as the sole physical `ENTITY_STATES` writer.
- Adds no runtime handler or new wire contract.
- Detects constructor, graph-create, exact-read, conflict convergence, status-read, promotion, and brief-injection
  drift in one assembled proof.
- Makes no new outward API promise: every called surface already exists and has a present consumer.

Costs:

- The test must author a valid lesson triple set, which reflects today's adopter burden.
- The ops scenario and mock gain one more loop and three more load-bearing stages.
- The test proves the happy path only; it cannot resolve or normatively classify #979 invalid-input behavior.

### Option B — Add an E2E-only typed create adapter in the runtime

Add an `e2e.control.lesson.create` request/response, a narrow `Creator` dependency, a handler under
`test/e2e/harness/lessoncuration`, and a second root subscription in `cmd/e2e-semstreams`.

Benefits:

- Mirrors the existing Promote control shape.
- Keeps JSON request/reply diagnostics explicit and could centralize fixture decoding.

Costs:

- It tests a new proxy rather than the product's direct `NewNATSLessonStore` use.
- It adds a subject, request, response, handler, subscription lifecycle, handler tests, and root resource solely to
  avoid calling an already-exported surface.
- It creates a zero-production-consumer adapter and a second spelling of lesson creation.
- It hides whether an external caller can construct and operate the current store directly.

`query-pattern` permits a named operation-specific typed adapter when an embedded operation lacks one. Here the
purpose-specific Go adapter already exists, so the condition for Option B is absent.

### Option C — Reuse generic projection/entity-create E2E lanes

Two variants exist:

1. Construct `projection.MutationClient` with `LessonProjectionContract()` and call generic `Create`.
2. Use the internal raw `graphmutation.Client.Create` pattern from structural/research fixtures.

The public projection variant is admitted and product-like, but it proves a different outward surface. It bypasses
`NewNATSLessonStore`, `LessonStore.ReadLessonStatus`, and the store's exact-read conflict convergence. The internal raw
variant is not available to an external product and is fixture seeding, not an adopter seam.

Neither variant closes the concrete `LessonStore` drift named by #1030. Raw KV and raw subjects are not alternatives.

### Option D — Scenario organization

#### D1 — Add a separate product-lesson scenario

This gives isolated timing and result reporting, but it requires a new CLI scenario name, scenario registration,
mock preset/cursor lifecycle, task invocation, duplicated component/evidence setup, and either a second stack run or
coordination with the ops scenario.

#### D2 — Extend the existing ops scenario

The existing scenario already owns the evidence fixtures, product/operator promotion request, role scope, mock
injection proof, fail-closed stage runner, and Docker tier. Three named product stages can run after the current
agent-lane injection proof. The mock cursor naturally parks on the new direct-product injection marker between loop
two and loop three.

The cost is a broader ops scenario, mitigated by distinct `product-lesson-*` stage names, constants, result keys, and
tests. It avoids every new scenario/config/task/compose surface from D1.

## Recommendation

Choose Option A organized as D2: extend the existing ops scenario with an external-runner direct-store path.

Do not add the Option B create adapter. Do not use generic projection create for birth. Reuse the validation client's
underlying standard `*natsclient.Client` for the lesson store and one local public projection client used only for
exact observation and generic revision-fenced test cleanup.

This is the smallest target that closes the measured gap: it tests the actual existing product seam, reuses the one
existing typed promotion operation, and adds no phantom runtime surface.

## Decision-skill outcomes

### `query-pattern`

Caller classification: an external-style Go product runner operating against an assembled SemStreams process.

- Birth: use the existing purpose-specific typed Go adapter, `LessonStore` from `NewNATSLessonStore`.
- Promotion: use the existing named typed E2E Promote adapter.
- Status: use `LessonStore.ReadLessonStatus`; use the admitted HTTP graph read only for independent persisted-triple
  evidence already used by the scenario.
- Cleanup: use `projection.MutationClient.ReadAuthoritative` plus generic revision-fenced `Delete` for exact tracked
  IDs. Construct the local client with `LessonProjectionContract()` to validate the snapshot at client construction;
  the snapshot does not scope or authorize `Delete`.
- Do not read, write, or delete `ENTITY_STATES` directly for the new path.
- Do not publish the generic graph-mutation subject manually.
- MCP is not an implemented graph contract and is irrelevant.

Outcome: no new query or mutation adapter is required.

### `kv-or-stream`

The recommendation adds no communication path, bucket, or stream. It reuses existing synchronous graph mutation
request/reply, exact authority reads, the existing Promote request/reply, and the existing user/tool streams.

For completeness, Option B's hypothetical create request evaluates as follows:

| Test | Result |
|---|---|
| Restart | The E2E caller should fail and report; it should neither rehydrate a fact nor resume old work after restart |
| Fan-out vs queue | Exactly one E2E runtime handler should answer one caller |
| Processing time | One strict create is fast; the caller waits for its classified result |
| Nature | It is a request to create, not a current fact |

KV is wrong because there is no declarative current request state to hydrate or fan out. A JetStream work stream is
also disproportionate because this test control cannot outlive its caller, must return one immediate classified
result, and must not redeliver after the scenario has failed or restarted. If Option B were required, ephemeral Core
NATS request/reply would match the existing Promote control and graph mutation RPC better than KV or JetStream. It is
not required under the recommendation.

### Other decision skills

- `orchestration-check` is not triggered: the change adds test stages that invoke and observe existing components; it
  adds no production rule, workflow, component behavior, or state owner.
- `new-payload` is not triggered: the recommendation adds no message or payload type.

## Target test flow

The existing nine-stage agent/tool proof remains unchanged in order. After its injection proof succeeds, append:

- Stage 10: `create-product-lesson`
- Stage 11: `promote-and-recreate-product-lesson`
- Stage 12: `inject-and-verify-product-lesson`

Sequence:

```text
existing stages 1–9
    │
    ├─ external runner CreateLesson(valid caller ID/type/triples)
    │      ├─ created == true
    │      └─ ReadLessonStatus == proposed
    │
    ├─ existing e2e.control.lesson.promote
    │      ├─ response.Promoted == true
    │      ├─ ReadLessonStatus == active
    │      ├─ identical CreateLesson == created false
    │      └─ ReadLessonStatus remains active
    │
    └─ existing user-message dispatch
           └─ mock sees unique product injection form in assembled brief
                  └─ emits unique diagnosis finding observed through HTTP graph read
```

The store's post-promotion identical create is an intentional semantic convergence assertion, not a retry after
ambiguous transport failure.

## Caller-authored lesson fixture

The direct caller supplies one semantically valid happy-path lesson. It uses:

- entity type: `agentic.AgentLessonMessageType()`;
- entity ID: `agentic.AgentLessonEntityID("c360", "ops", <opaque precomputed product fixture token>)`;
- category: a product-owned open value distinct from the mock-authored lesson;
- polarity: one documented closed value;
- severity: one documented closed value;
- status: exactly `proposed`;
- created-at: one RFC3339 UTC birth value;
- summary, detail, and a unique direct-product injection form below the documented bound and without control bytes;
- evidence: `SeedLoop1ID`, which the existing scenario has already made resolvable before the direct create;
- applies-to: `tag:ops`, matching the later loop's derived scope;
- identical subject, source `e2e-product-lesson`, timestamp, and confidence `1.0` on every triple.

The optional agent-loop attribution predicates are omitted because this caller is product code, not a running agent
loop. That matches the observed adopter shape.

The fixture token is opaque and precomputed. The test does not add a hashing helper, reproduce the private separator
encoding, assert a canonical external identity algorithm, or export an identity constant. It tests the current
caller-supplied ID seam without deciding #979.

The fixture MUST NOT contain an invalid enum, missing evidence, missing scope, over-bound injection text, control
bytes, a non-proposed birth state, or a foreign triple subject. Passing the test says only that this valid caller
shape works; it says nothing about malformed-input acceptance or rejection.

## Exact assertions

### Stage 10 — `create-product-lesson`

1. The existing validation client's `s.nats.Client()` and `NewNATSLessonStore` are non-nil.
2. The single direct `CreateLesson` call returns `created == true` and no error.
3. `ReadLessonStatus` returns `status == "proposed"`, `found == true`, and no error.
4. `projection.MutationClient.ReadAuthoritative` returns the exact entity ID and
   `agentic.AgentLessonMessageType()`.
5. The authoritative entity's complete triple multiset exactly equals the caller-supplied happy-path set: category,
   polarity, severity, proposed status, created-at, summary, detail, injection form, evidence, and applies-to.
6. Exact tuple comparison includes subject, predicate, object, source, triple timestamp, and confidence for every
   supplied triple. The `agent.lesson.created-at` object is also compared to the supplied RFC3339 value.
7. No optional agent-loop attribution or unexpected lifecycle sibling predicate is present.
8. The admitted HTTP graph read independently finds exactly one status object for this subject and it is `proposed`.
9. The persisted entity ID has the six-part `*.*.agent.lesson.record.*` shape.
10. Result details record the entity ID, `created: true`, and born status.

### Stage 11 — `promote-and-recreate-product-lesson`

1. The existing typed Promote request names the captured product lesson ID.
2. The response decodes and reports `Promoted == true`.
3. A bounded status read observes exactly one `active` value.
4. An authoritative read still reports the caller-supplied message type and every caller-supplied non-lifecycle birth
   tuple exactly: category, polarity, severity, created-at, summary, detail, injection form, evidence, and applies-to,
   including each tuple's source, timestamp, and confidence.
5. The caller-supplied proposed status tuple is absent, exactly one active status exists, and no retired-at or
   superseded-by sibling exists.
6. Calling `CreateLesson` again with the exact original message type and triple slice returns `created == false` and
   no error.
7. `ReadLessonStatus` still returns `active`; the proposed status carried by the repeated request did not overwrite
   lifecycle state.
8. A second authoritative comparison after the identical create still reports the same message type, complete
   non-lifecycle birth tuples, and single active lifecycle state.
9. The graph still exposes one lesson entity at that ID.

This stage does not vary ignored fields and therefore does not freeze which fields #979 may later include in conflict
identity. It does require full persistence of one valid caller-supplied semantic set; that is happy-path evidence, not
a malformed-input policy ruling.

### Stage 12 — `inject-and-verify-product-lesson`

1. A third ops user message completes successfully.
2. Brief assembly includes the unique direct-product injection form because the lesson is active and scoped
   `tag:ops`.
3. The mock entry keyed to that exact form emits a unique product-injection proof diagnosis.
4. The admitted HTTP graph read observes that exact finding.
5. Result details record the proof subject and `product_lesson_injection_confirmed: true`.

The first lesson's injection form is also active, but cannot satisfy this assertion because the new mock cursor and
finding use a distinct direct-product marker.

## Lifecycle, ownership, failure, and cleanup

### Resource ownership

- `Scenario.Setup(ctx)` creates only the existing `NATSValidationClient`.
- The scenario stores client handles, never `context.Context`.
- `NewNATSLessonStore(s.nats.Client())` creates a non-owning store over the validation client's standard
  `*natsclient.Client`.
- The same `s.nats.Client()` constructs one local `projection.MutationClient` with
  `LessonProjectionContract()`. The snapshot is validated at construction; it does not scope the client's generic
  read or delete operations.
- There is one scenario-owned NATS connection and one close authority. The store and projection client do not close
  it.
- The E2E runtime continues to own its existing graph subscriptions and Promote subscription.
- `Scenario.Teardown(ctx)` closes only the validation client with the supplied teardown context.
- If store or projection-client construction fails during setup, setup closes the validation client before returning.
- No new goroutine, watcher, subscription, cancel function, or retained context is added to the scenario.

### Failure and retry

- Every create, status read, promotion request, and authoritative read receives the current stage context.
- The first create is sent once. A classified commit-unknown/unavailable/error fails stage 10; the test never blindly
  retries a mutation whose commitment is unknown.
- Promotion is requested once. Polling observes the already-requested state transition; it does not resend it.
- The second create occurs only after a verified first create and verified promotion. It is the tested idempotent
  operation, not transport recovery.
- All polls use the scenario's existing `CompleteTimeout` and honor `ctx.Done()`.
- A failed stage remains uncounted, later stages do not run, and the E2E CLI returns nonzero through the existing
  fail-closed task wrapper.

### Cleanup

- Track only exact lesson IDs captured or constructed by this scenario: the existing agent-authored lesson ID and the
  product-authored lesson ID.
- In deferred scenario cleanup, use the local public projection client to exact-read each tracked ID and call generic
  `Delete` with that exact ID and observed revision.
- `DeleteMutation` has no projection-contract field. `LessonProjectionContract()` validates client construction but
  does not constrain or authorize deletion. Cleanup safety comes from the closed tracked-ID set plus revision fencing,
  not from the lesson contract.
- A missing entity is already clean. A revision conflict, commit-unknown, or other cleanup failure becomes a result
  warning; cleanup does not hide the primary stage failure.
- Do not add direct `ENTITY_STATES` deletion for either lesson.
- The normal `task e2e:ops` path still runs `docker compose down -v`, which is the final recovery boundary for any
  test artifact left after a failed or ambiguous cleanup.
- Existing raw KV seeding/cleanup for synthetic loop fixtures is unchanged and is not precedent for the new path.

This cleanup is test-only. It does not change the production rule that retired/superseded lessons remain durable.

## Collision disposition

| Existing surface | Disposition |
|---|---|
| Exported `LessonStore` | Exercised directly; unchanged |
| Agent `emit_lesson` lane | Existing nine-stage proof retained; no replacement or prompt change |
| Public projection `Create` | Not used for lesson birth because it would test the wrong outward seam |
| Internal raw graph create | Not used |
| Existing typed Promote control | Reused and parameterized for the second lesson; unchanged wire contract |
| `LessonProjectionContract` | Reused to validate runner-local client construction; it does not scope generic exact read or delete |
| `NewLessonCurator` narrow capabilities | Runtime composition remains unchanged |
| Retired `NewNATSLessonCurator` | Remains absent |
| #979 | No invalid-input or identity-policy ruling; remains separate |
| #818 | No generic immutable-birth or graph-ingest policy change |
| Ops automatic trigger claim | Not repaired or tested |
| Generic graph-roundtrip E2E | Remains the generic contract-bound canary; not duplicated |

No additional same-class owner, durable primitive, runtime coordinator, or communication path is introduced.

## Adopter seam after this change

Specific adopter: a semdev developer constructing repository standards as lessons through `LessonStore`, who has
never opened `emit_lesson.go`.

### What they must know

This coverage-only change does not alter their compile-time API or remove the current knowledge burden. They still
must supply identity, message type, valid birth triples, proposed status, evidence, scope, provenance, and sequencing;
they still compose the public lesson projection snapshot locally for curator work.

### What happens if they do nothing

Their product behavior is unchanged. The new SemStreams E2E becomes a release regression signal for the valid direct
path, but no runtime behavior, default, or validation gate changes. Existing drift risks described by #979 remain.

### Where they find out

- The updated concept guide names both the agent/tool writer and the direct product/store proof.
- The E2E scenario is executable, line-addressable example code for valid direct construction.
- Compile-time interfaces and typed errors remain the strongest runtime discovery mechanisms.
- The test does not pretend to document malformed-input policy that the store does not currently own.

### What they should have to know

They should ultimately know only product content, taxonomy/scope policy, evidence, and promotion policy. The remaining
framework-mechanics gap is #979 design work, not #1030 coverage work.

### New adopter bill

None. No exported symbol, config, subject, payload, or behavioral knob is added. The E2E fixture copies current valid
mechanics inside SemStreams test code so releases can observe the existing bill; it does not shift that bill to a new
surface or claim to pay it down.

## TDD sequence

### RED

1. Factor the production scenario's actual ordered stage slice into `func (s *Scenario) stages() []opsStage`, and make
   `Execute` consume that function.
2. Add `TestScenarioStages` that inspects `NewScenario(...).stages()` without executing the stage functions and
   requires the exact ordered names:
   - the existing nine names unchanged;
   - `create-product-lesson` at position 10;
   - `promote-and-recreate-product-lesson` at position 11;
   - `inject-and-verify-product-lesson` at position 12.
   This test is RED against the actual nine-stage plan. A synthetic `successfulOpsStages(12)` slice is not acceptance
   evidence and MUST NOT be used for the count RED.
3. Retain the existing small synthetic early-failure test for `runOpsStages`; it still proves a failed stage is not
   counted and later stages do not run.
4. Add a focused fixture test that expects a product lesson birth builder to return:
   - the lesson message type and six-part entity ID;
   - one proposed status;
   - the exact valid category, polarity, severity, created-at, summary, detail, injection form, evidence, and scope;
   - identical subjects, source, timestamp, and confidence;
   - no optional agent-loop attribution.
   The undefined builder is the first compile-time RED.
5. Add a mock-sequence contract test or focused preset test that expects the direct-product injection marker to occur
   after the original injection marker and before the never-match terminator.
6. Run the existing `task e2e:ops` before implementation and retain the baseline nine-stage result; the new twelve-
   stage acceptance is not yet satisfiable.

### GREEN

1. Construct the store and projection client over `s.nats.Client()` with no second connection or close owner.
2. Add the valid caller-authored fixture without exporting production constants or copying the private identity
   algorithm.
3. Generalize the existing promotion request helper to accept a lesson ID while retaining the same subject and
   request/response structs.
4. Add the three named stages after the existing stage nine.
5. Extend the ops mock cursor with exactly one distinct product-injection entry before the terminator; add no role,
   persona, or production prompt.
6. Add generic typed revision-fenced lesson cleanup for the exact tracked IDs.
7. Make focused unit tests green, then run the assembled ops tier and require all twelve stages.

### Refactor guard

- Keep agent-lane and product-lane constants/result keys distinct.
- Share only mechanical helpers such as parameterized promotion, status polling, and subject-filtered triple
  assertions.
- Do not make a generic lesson fixture/writer production package; the fixture is test-local evidence.

## Exact artifact delta

### Test code

1. `test/e2e/scenarios/ops/scenario.go`
   - add non-owning `LessonStore` and local projection-client fields over `s.nats.Client()`;
   - add direct-product fixture constants/state;
   - add valid triple construction;
   - factor the actual ordered stage plan into a test-inspectable method;
   - add stages 10–12 and exact result details;
   - parameterize promotion/status/full-authoritative assertions by lesson ID;
   - use generic revision-fenced lesson cleanup for exact tracked IDs;
   - update package/description comments from the two-loop/nine-stage proof to both writer lanes and twelve stages.
2. `test/e2e/scenarios/ops/scenario_test.go`
   - assert the real stage plan's exact twelve ordered names;
   - retain early-failure accounting;
   - add fixture-shape and full authoritative-comparison helper tests.
3. `test/e2e/mock/cmd/main.go`
   - update the ops preset narrative from two loops to three;
   - insert one product-injection-form entry after the existing injection proof and before the never-match terminator;
   - emit a distinct product proof finding;
   - do not add a role, persona, tool, or production prompt.

No production Go file, runtime composition root, config flow, compose file, handler, contract, payload, schema, subject,
bucket, or stream changes.

### Documentation

1. `docs/concepts/32-agent-memory.md`
   - correct the existing command from `task e2e:agentic` to `task e2e:ops`;
   - state that the tier proves both configured `emit_lesson` birth and external-style direct `LessonStore` birth;
   - keep #979 validation/identity policy out of the guide.
2. `taskfiles/e2e/ops.yml`
   - correct the stale `submit_work` comment;
   - update the task description only after measuring the new observed runtime;
   - leave task commands, teardown, ports, and CI inclusion unchanged.

### OpenSpec task truth

Create a bounded coverage change, proposed ID `cover-product-lesson-write-e2e`, containing:

- `proposal.md`: the SemStreams-owned direct-store E2E gap, existing agent lane, and explicit non-goals;
- `design.md`: this accepted design or a link to its immutable artifact/hash;
- `tasks.md`: real stage-plan RED, fixture/full-authority RED, shared-client runner composition, three E2E stages,
  mock marker, exact-ID generic cleanup, docs, and verification evidence.

No capability spec delta is recommended. The current lesson spec already defines valid lesson birth, proposed/active
lifecycle, evidence gating, and brief injection. Adding a requirement that `LessonStore` reject or accept particular
malformed direct inputs would freeze #979 policy; adding a requirement about a test harness would confuse capability
truth with verification mechanics.

Do not edit or close tasks 10–11 in `own-lesson-curator-contract`; downstream snapshot adoption remains separate.

### ADR and release truth

- No ADR change: no irreversible architecture or cross-repo contract changes.
- Do not edit ADR-080's automatic trigger claim under #1030; that repair is an explicit non-goal.
- Do not edit ADR-097; its snapshot/local-client/narrow-curator ruling is consumed as-is.
- No migration guide or release note is required because no production or outward contract changes.
- The change is non-breaking and requires no fresh-state cutover.

## Verification gates

### Focused

```text
go test ./test/e2e/scenarios/ops
go test ./test/e2e/mock/...
go test -race ./test/e2e/scenarios/ops ./test/e2e/mock/...
```

### Repository regression

```text
task lint
go test -race ./...
task test:integration
task schema:generate
git diff --check
openspec validate cover-product-lesson-write-e2e --strict
```

Schema generation must produce no uncommitted schema/spec drift beyond the accepted OpenSpec artifacts.

### Required assembled gate

```text
task e2e:ops
```

Acceptance requires:

- result success;
- `AssertionsRun == 12`;
- all original nine stage metrics present;
- all three new product-stage metrics present;
- direct create `true`, born status `proposed`, promotion `true`, identical re-create `false`, persisted status
  `active`, and distinct product injection proof recorded;
- authoritative message type and the complete caller-supplied happy-path triple set verified at birth, after promotion,
  and after identical re-create, with the lifecycle status difference handled explicitly;
- task exit nonzero on any failed stage;
- Docker teardown completes.

No other E2E tier is proportional. No CI/nightly wiring is added by this issue. If the owner separately wants
`e2e:ops` automated, that remains #769/CI-ladder scope.

## Issue completion boundary

Issue #1030 is complete only when:

1. The external E2E runner constructs `NewNATSLessonStore` over the existing validation client's standard
   `*natsclient.Client`, with no second connection owner.
2. It calls `CreateLesson` directly with caller-owned valid identity, message type, and triples.
3. It authoritatively proves the message type and complete persisted happy-path tuple set, including polarity,
   severity, detail, created-at, source, timestamp, confidence, and proposed status.
4. It promotes through the existing typed Promote control.
5. It proves an identical post-promotion create converges without overwriting active status.
6. It proves the unique direct-product injection form reaches a later assembled loop brief.
7. All twelve ops stages and their assertion counts are fail-closed.
8. The one scenario-owned NATS client closes, and exact-ID generic typed cleanup does not hide primary failures.
9. Focused, race, integration, lint, schema, strict OpenSpec, and `task e2e:ops` gates are recorded green.
10. Documentation names the correct E2E task and both covered writer lanes.

The issue is not evidence that #979, the automatic ops trigger, ops reporting, CI/nightly scheduling, or downstream
adoption is complete.

## Owner acceptance

The owner accepted all thirteen rulings exactly as written on 2026-08-22:

1. **Existing surface:** Use `NewNATSLessonStore` directly from the external E2E runner over `s.nats.Client()`; add no
   second NATS connection or create adapter.
2. **Organization:** Extend the existing ops scenario rather than add a separate scenario/config/task.
3. **Count:** Preserve the original nine stages and append exactly three, making `AssertionsRun == 12`.
4. **Promotion:** Reuse the existing `e2e.control.lesson.promote` contract unchanged.
5. **Birth fixture:** Exercise and authoritatively compare one fully valid caller-authored message type and complete
   triple set, including polarity, severity, detail, created-at, source, timestamp, and confidence; make no
   malformed-input assertion.
6. **Identity:** Use an opaque precomputed product fixture token and add no external identity algorithm or constant.
7. **Idempotency:** Repeat the exact original create only after verified promotion; require `created == false` and
   active status preservation without varying non-identity fields.
8. **Cleanup:** Use a runner-local projection client for generic authoritative read and revision-fenced delete of the
   exact tracked IDs; `LessonProjectionContract` validates client construction but does not scope or authorize delete;
   add no direct KV cleanup for lessons.
9. **#1029:** Keep local client composition, narrow curator injection, and retired `NewNATSLessonCurator` unchanged.
10. **#979:** Leave validation, content identity, constants, and malformed-input policy unresolved by this issue.
11. **Docs/spec:** Correct the task guide and record OpenSpec task truth, but add no capability-spec or ADR delta.
12. **Automation:** Require `task e2e:ops` as the implementation gate without adding CI/nightly scope.
13. **Repository boundary:** Make no sister-repository change or claim of downstream adoption.

The SemStreams developer may implement only these accepted rulings through the project role contract and required
review gates.
