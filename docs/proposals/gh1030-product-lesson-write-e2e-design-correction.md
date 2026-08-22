# Issue #1030 design correction — independent product lesson E2E

Status: accepted by the owner on 2026-08-22 after independent SemStreams CORRECTION DESIGN PASS.

Repository baseline: `c5e7255298da0be9dc2823e15e134fe32c77adb3`.

This artifact supersedes the unimplemented ops-extension design at
`docs/proposals/gh1030-product-lesson-write-e2e-design.md`, SHA-256
`e55324ae80fbf2d43f62535d7867bf6190c054a7498c9320fa64eb8dae313a36`.

The accepted inventory at
`docs/proposals/gh1030-product-lesson-write-e2e-inventory.md`, SHA-256
`dccdb50e8e2875ce1cec2b0e2b3f44c15a451a4a6454626e6daeaf2ef1c2c634`, remains the evidence base except where this
correction records new owner rulings and re-inventories the independent non-agent path.

Implementation did not begin before correction review and owner acceptance.

## Accepted correction constraints

The owner has ruled:

1. SemStreams ships no bespoke ops agent; products compose their own. The same framework/product boundary applies to
   the deep-research demo.
2. The lesson substrate remains framework-owned and semdev consumes it directly.
3. Correct independent product-lane coverage in #1030 is a prerequisite for later ops-agent retirement.
4. Reportable conditions replace bespoke framework agents in other work, but #1030 adds no reportable-condition
   design or implementation.
5. Retirement of `emit_diagnosis` and `ops.diagnosis.*` remains undecided pending a sister-consumer ask. #1030 leaves
   diagnosis untouched.
6. The fate of `task e2e:ops` is undecided. #1030 neither retires, renames, repurposes, nor makes it the acceptance
   gate.

These constraints supersede the former decision to append product stages 10–12 to the ops scenario.

## Inventory correction

### Claimed gap

Current E2E source still has no product-shaped direct lesson producer or non-agent lesson reader:

```text
rg -n "NewNATSLessonStore|CreateLesson\(|NewNATSLessonReader|lessonmatch.Match|NewLessonCurator" \
  test/e2e cmd/e2e --glob '*.go'
→ zero results
```

No independent lessons scenario, task, stack, or flow exists:

```text
rg -n 'e2e:lessons|scenario lessons|"lessons"' \
  Taskfile.yml taskfiles/e2e docker/compose configs cmd/e2e test/e2e \
  --glob '*.yml' --glob '*.json' --glob '*.go'
→ zero results
```

The gap is unchanged: no assembled E2E caller constructs `NewNATSLessonStore` and invokes `CreateLesson` with
caller-authored identity, message type, and triples.

The corrected gap is narrower than the original issue premise. Configured agent-side `emit_lesson` consumers exist,
but they are not the adopted direct-product lane. The missing coverage does not justify preserving or extending a
framework-owned ops-agent scenario.

### Existing product birth surface

`LessonStore` exposes `CreateLesson` and `ReadLessonStatus` at
`processor/agentic-tools/emit_lesson.go:112-129`.

`NewNATSLessonStore` builds the concrete store over a caller-owned `*natsclient.Client` at
`processor/agentic-tools/emit_lesson.go:187-202`.

Fresh birth sends the caller's message type and complete triple slice through canonical graph creation at
`processor/agentic-tools/emit_lesson.go:204-217`.

An existing-ID conflict exact-reads authority and verifies message type plus the four current identity dimensions at
`processor/agentic-tools/emit_lesson.go:219-245`.

Issue #979 owns any decision about direct-writer validation, malformed input, identity dimensions, or public
constants. Issue #1030 exercises one valid current caller shape and makes no policy ruling.

### Existing lifecycle surface

`LessonProjectionContract()` exports an independent canonical lesson contract snapshot at
`processor/agentic-tools/lesson_promotion.go:50-52`.

`NewLessonCurator` retains the narrow `PredicateReconciler` and `AuthoritativeReader` composition at
`processor/agentic-tools/lesson_promotion.go:31-62`.

`Promote` exact-reads the lesson, resolves every cited evidence entity, refuses unresolved evidence, and reconciles
proposed to active at `processor/agentic-tools/lesson_promotion.go:64-115`.

The complete lifecycle group is reconciled through the contract-bound client at
`processor/agentic-tools/lesson_promotion.go:161-180`.

ADR-097 requires composition-root-local clients and keeps `NewNATSLessonCurator` retired:
`docs/adr/097-built-in-lesson-curation-owns-contract-composition.md:25-43`.

### Existing non-agent reader and matcher

`NewNATSLessonReader` is the narrow production reader over the existing graph prefix-query surface at
`processor/agentic-loop/lessons.go:33-60`.

`ReadLessons` returns the matcher-relevant lesson projection using existing request/reply and adds no caller-visible
raw subject handling at `processor/agentic-loop/lessons.go:62-100`.

`AgentLessonRecordPrefix` owns the five-part lesson prefix spelling at
`agentic/agent_lesson_entity.go:77-92`.

`lessonmatch.Match` is the pure deterministic selector over caller-supplied candidates and scope at
`processor/agentic-loop/lessonmatch/lessonmatch.go:54-127`.

It excludes every non-active lesson, applies typed scope matching, orders deterministically, and bounds the result at
`processor/agentic-loop/lessonmatch/lessonmatch.go:125-171`.

Production brief assembly performs exactly:

```text
AgentLessonRecordPrefix
→ LessonReader.ReadLessons
→ lessonmatch.Match
→ renderLessonBlock
```

at `processor/agentic-loop/handlers.go:702-735`.

The final renderer is private. Existing unit coverage proves a matching active matcher result is rendered into the
brief at `processor/agentic-loop/lessons_test.go:64-92`.

No existing exported non-agent surface assembles the final prompt block. Exporting one solely for E2E would be a new
phantom surface.

### Existing assembled runtime independent of ops

The core compose stack runs the production binary, not the E2E agent composition:
`docker/compose/e2e.yml:43-64`.

It exposes NATS at the standard E2E endpoint and uses `configs/protocol-flow.json`:
`docker/compose/e2e.yml:16-24,58-75`.

That configuration contains an enabled graph-ingest component with the typed graph mutation request input at
`configs/protocol-flow.json:344-360`.

The standalone graph-roundtrip scenario already proves an external runner can own one `NATSValidationClient`,
construct a public projection client, mutate authority, and close the connection:
`test/e2e/scenarios/graph_roundtrip_scenario.go:10-66` and
`test/e2e/scenarios/graph_roundtrip.go:99-180,230-248`.

`NATSValidationClient.Client()` exposes the live underlying client while retaining sole close ownership at
`test/e2e/client/nats.go:58-107`.

The core task already owns production-image build, readiness, and eventual `down -v`:
`taskfiles/e2e/core.yml:3-13,49-50`.

A lesson scenario can reuse this stack without modifying its flow config or compose file.

### Ops surfaces excluded by the correction

The current ops scenario is selected separately at `cmd/e2e/main.go:447-452`.

Its task runs the ops-specific compose stack and mock LLM at `taskfiles/e2e/ops.yml:3-30`.

The stack selects `configs/flows/ops-agent-test.json`, the E2E binary, and the ops mock preset at
`docker/compose/ops.yml:47-94`.

The old design's stage-10–12 plan, mock cursor extension, user message, diagnosis marker, and `task e2e:ops` gate are
all superseded. The correction touches none of these surfaces.

### Spec and ADR collision

The current lesson spec still says `emit_lesson` is the only agent-facing creation path on the ops seam and that
active lessons reach an agent brief at `openspec/specs/agentic-lessons/spec.md:73-91,135-169`.

ADR-080 assigns debrief to the ops role and brief assembly to the framework:
`docs/adr/080-push-based-agent-memory-and-lesson-artifacts.md`, decisions 4 and 5.

The new owner ruling anticipates separate retirement work, but #1030 is only its prerequisite. Changing those
present-tense capability claims here would conflate coverage with retirement. Therefore #1030 adds no capability-spec
or ADR delta.

ADR-027's `emit_diagnosis` and `ops.diagnosis.*` claims remain current until their separate consumer inventory and
owner decision. #1030 neither validates nor retires them.

### Current history to preserve

The previous accepted design is unimplemented and has SHA-256
`e55324ae80fbf2d43f62535d7867bf6190c054a7498c9320fa64eb8dae313a36`.

The previous active OpenSpec design is valid and has SHA-256
`73518a0d8ce6753b57653e80e208219fcd11593ec92b3a1b4d0a20538cd46585`.

Both receive explicit supersession notices. Their ops-extension bodies remain historical evidence and are not
silently rewritten.

## Adopter seam inventory

The specific adopter is an external product developer, such as semdev, writing a composition root without opening
SemStreams internals.

### What they must know today

They must:

1. Use their existing connected `*natsclient.Client` to construct `NewNATSLessonStore`.
2. Supply a valid six-part lesson entity ID, `AgentLessonMessageType`, and the current happy-path lesson triples.
3. Construct their local projection client with `LessonProjectionContract()` and inject its narrow reconciler/reader
   capabilities into `NewLessonCurator`.
4. Promote only after cited evidence exists.
5. Use the framework-owned lesson prefix reader and deterministic matcher when they need the same eligibility
   semantics as brief assembly.

Items 1–4 are existing adopter debt, not new #1030 surface. #979 may later reduce the birth-shape debt. #1029 already
removed copied projection-contract literals.

### What happens if they do nothing

Their production behavior does not change. Existing direct lesson writers continue compiling and running.

Before #1030, SemStreams lacks an assembled regression gate for their path. A framework change can break product
birth, curation, retrieval, or matching while agent-only coverage remains green.

After the corrected test lands, doing nothing incurs no migration. The new test observes the existing surfaces on the
adopter's behalf.

### Where they find out

- Constructor and method shapes: compile-time Go API.
- Mutation/client contract failures: typed runtime errors.
- Lifecycle requirements: `agentic-lessons` spec and ADR-097.
- Correct assembled regression task: `task --list` and the agent-memory guide.
- No new product configuration, subject, stream, bucket, role, prompt, or persona is introduced.

### What they should have to know

They should learn nothing new because of #1030. The scenario should reproduce the existing adopter shape, not add
another one.

The test must not ask the adopter to predict a subject, bucket, readiness state, or retry deadline. The task waits for
the real compose health checks; the Go clients act through existing typed surfaces and observe authoritative results.

## Options

### Option 0 — Do nothing

Cost: the direct product lane remains unproved and ops-agent retirement lacks an independent lesson-substrate gate.

This does not satisfy #1030 or the accepted prerequisite ruling.

### Option A — Retain the accepted ops stage-10–12 extension

Cost: product coverage remains coupled to an ops role, mock LLM cursor, user-message dispatch, diagnosis entity, and
the unresolved `task e2e:ops` lifecycle.

Deleting the framework ops demo would delete the product lesson gate. This conflicts directly with the new owner
rulings.

### Option B — Add `lessons` to the existing core-all scenario list

Cost: no new task or stack, but the lesson gate becomes an implicit part of `task e2e:core`; its assertion identity is
less discoverable, and core-all ordering can accidentally become a fixture dependency.

This is independent of ops but does not provide a named prerequisite gate.

### Option C — Add a new minimal lesson flow config and compose stack

Cost: strongest physical isolation, but adds a flow config, compose file, ports, images, task cleanup surface, and
another minimal-runtime declaration duplicating graph-ingest composition.

The lesson test needs no runtime behavior absent from the existing production core stack. A second config would be
another spelling of core graph composition.

### Option D — Standalone `lessons` scenario and task

Add a named `lessons` scenario and `task e2e:lessons`, but reuse `docker/compose/e2e.yml` and
`configs/protocol-flow.json` unchanged.

Cost: one scenario package, one CLI registration, one taskfile/include, focused tests, and one docs correction. It
shares the core stack's ports and cannot run concurrently with `task e2e:core`.

Benefit: it is independently runnable, uses the production binary, has no agent or mock dependency, and survives
deletion of every ops-specific artifact.

## Recommendation

Choose option D.

The new task is a named prerequisite gate while reusing one existing assembled runtime. It adds no production API, no
E2E control handler, no config, no compose file, and no communication primitive.

## Decision-skill outcomes

### `query-pattern`

The caller is an external-style Go runner acting as a product composition root.

Use only admitted purpose-specific surfaces:

- `NewNATSLessonStore.CreateLesson` for birth;
- local `projection.MutationClient` plus `LessonProjectionContract` for authority and cleanup;
- `NewLessonCurator` for lifecycle;
- `NewNATSLessonReader` for lesson retrieval; and
- `lessonmatch.Match` for deterministic brief-input eligibility.

No raw KV, direct subject, generic embedded graph query client, HTTP fallback, MCP assumption, or new remote adapter is
admitted.

### `kv-or-stream`

No new communication path is required.

Birth and lifecycle reuse existing typed graph request/reply. Retrieval reuses the existing graph-ingest prefix query.
The matcher is in-process and pure.

Adding a new E2E request handler would be an ephemeral Core NATS request/reply rather than KV or JetStream, but it is
unnecessary because the external runner can compose the existing Go surfaces directly. Therefore no KV, stream,
subject, handler, payload, subscription, or durability decision is added.

## Corrected target flow

```text
task e2e:lessons
  → existing docker/compose/e2e.yml
  → production cmd/semstreams + configs/protocol-flow.json
  → one scenario-owned NATSValidationClient
  → seed one exact scenario-owned evidence fixture through public projection Create
  → NewNATSLessonStore(client).CreateLesson(valid caller lesson)
  → authoritative full-tuple proof + proposed matcher exclusion
  → local NewLessonCurator(projection client, projection client).Promote
  → authoritative active proof
  → NewNATSLessonReader.ReadLessons
  → lessonmatch.Match(unique product scope)
  → exact included lesson ID + injection form
  → identical CreateLesson
  → created=false + active/full-tuple/match preservation
  → exact-ID revision-fenced cleanup
```

No agent loop runs.

## Issue suggestion supersession

The issue suggested proving that the lesson reaches a subsequent loop brief. Under the accepted correction
constraints, literally satisfying that phrase requires running an agent loop or exposing a new prompt-assembly
adapter.

Neither is warranted:

- running a loop reintroduces the agent/model/persona surface the correction removes;
- exporting the private renderer or MessageHandler assembly solely for E2E creates a zero-production-consumer
  surface.

The corrected assembled acceptance boundary is:

1. prove the active product lesson is retrieved by the production `LessonReader`;
2. prove the production `lessonmatch.Match` returns its exact entity ID and injection form for the matching scope; and
3. retain existing unit coverage proving that matcher result is rendered into the loop brief.

The owner must explicitly supersede “subsequent loop brief in one assembled run” with “assembled reader/matcher
eligibility plus the existing matcher-to-brief unit seam.” The result must not be reported as an assembled agent-loop
proof.

## Scenario composition

Add `test/e2e/scenarios/lessons/scenario.go` with a private `Config` containing:

- NATS URL;
- org and platform matching the selected core fixture; and
- operation timeout.

`DefaultConfig` uses the existing core E2E endpoint and platform identity from
`test/e2e/config/constants.go:6-20` and `configs/protocol-flow.json:3-9`.

`Scenario.Setup(ctx)`:

1. opens one `NATSValidationClient`;
2. constructs one local projection client over `s.nats.Client()` with:
   - `LessonProjectionContract()`; and
   - one scenario-local exact evidence-fixture contract;
3. constructs the non-owning lesson store, reader, and curator over that same client;
4. closes the validation client before returning if later composition fails; and
5. stores no context and starts no goroutine, watcher, subscription, or cancel authority.

The private evidence contract follows the existing graph-roundtrip projection precedent and is fully fixed:

```text
Name:            e2e.lessons.evidence
MessageType:     test.fixture.v1
EntityPattern:   c360.streamkit-pure.test.fixture.evidence.product-lesson
BirthPredicates: [vocabulary.DCTermsTitle]
IndexingProfile: control
Groups:          none
```

`vocabulary.DCTermsTitle` is already registered. The exact created evidence entity is:

```text
ID:          c360.streamkit-pure.test.fixture.evidence.product-lesson
MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"}
Version:     1
UpdatedAt:   fixed fixture timestamp
```

Its one exact valid birth tuple is:

```text
Subject:    c360.streamkit-pure.test.fixture.evidence.product-lesson
Predicate:  vocabulary.DCTermsTitle
Object:     product lesson E2E evidence
Source:     e2e-product-lesson
Context:    e2e-lessons-evidence-create
Timestamp:  fixed fixture timestamp
Confidence: 1.0
Datatype:   ""
ExpiresAt:  nil
```

The Create metadata repeats request ID `e2e-lessons-evidence-create`, source `e2e-product-lesson`, and the fixed
fixture timestamp so projection canonicalization preserves that exact tuple. The contract is test-local, exact-ID
scoped, and used only to create the scenario-owned evidence entity. It is not exported and does not become framework
vocabulary.

No changes are made to `test/e2e/harness/lessoncuration`, because direct curator composition is the product shape.

## Caller-authored fixture

Use these fixed identity inputs:

- category: `retention-policy`;
- applies-to scope: `tag:product-lesson-e2e`;
- summary: `Scope retention sweeps to entity-owned buckets.`; and
- evidence ID: `c360.streamkit-pure.test.fixture.evidence.product-lesson`.

Use opaque precomputed identity token `54b545de-8f18-5419-b996-220d3c992c5c`, yielding exact lesson ID
`c360.streamkit-pure.agent.lesson.record.54b545de-8f18-5419-b996-220d3c992c5c` through `AgentLessonEntityID`.

The token is known to correspond to exactly the fixed category, sorted scope, summary, and sorted evidence inputs
under the current private content-derived identity algorithm. The scenario does not implement, copy, or export that
algorithm. This preserves the current content-derived capability truth while leaving #979's API and identity design
unsettled.

The lesson carries:

- `AgentLessonMessageType`;
- the fixed category;
- polarity;
- severity;
- proposed status;
- RFC3339 created-at;
- the fixed summary;
- detail;
- a unique bounded injection form;
- the fixed exact evidence fixture ID; and
- the fixed `tag:product-lesson-e2e` scope.

Every caller-authored triple has the exact lesson subject, source `e2e-product-lesson`, one fixed timestamp, confidence
`1.0`, expected empty datatype, and the same context/expiry shape.

`agent.lesson.observed-role` and `agent.action.executed-by` are absent because this is product code, not an agent tool
call.

The fixture is structurally valid and makes no assertion that malformed direct input is accepted.

## Stage plan and assertion count

The actual scenario stage plan contains exactly three ordered stages:

1. `create-and-prove-proposed`
2. `promote-and-prove-match`
3. `recreate-and-prove-convergence`

`AssertionsRun` increments only after a complete stage succeeds. A failure leaves the failed stage uncounted, stops
later stages, populates the result error, and makes the runner exit nonzero.

The assembled success count is exactly three.

### Stage 1 — create and prove proposed

- Seed the distinct evidence fixture first through public projection Create.
- Call `CreateLesson` exactly once.
- Require `created == true`.
- Require `ReadLessonStatus == proposed`.
- Authoritatively compare:
  - exact entity ID;
  - exact message type;
  - the complete caller-supplied triple multiset;
  - subject, predicate, object, context, source, timestamp, confidence, expected empty datatype, and expiry;
  - absence of optional attribution; and
  - absence of retired-at and superseded-by.
- Read through `NewNATSLessonReader`.
- Require the target lesson appears exactly once as proposed.
- Run `lessonmatch.Match` with the unique product scope and require zero included lessons.

A commit-unknown or unavailable birth fails the stage. It is not blindly retried.

### Stage 2 — promote and prove match

- Call the local `LessonCurator.Promote` exactly once.
- Require status active and proposed absent.
- Require retired-at and superseded-by absent.
- Require the complete non-lifecycle tuple multiset remains unchanged, including source, timestamp, confidence, and
  expected empty datatype.
- Read through `NewNATSLessonReader`.
- Run `lessonmatch.Match`.
- Require `MatchedCount == 1`, `IncludedCount == 1`, and one included item with the exact target entity ID and
  injection form.

No user message, model response, tool call, prompt, diagnosis, or agent-produced lesson participates.

### Stage 3 — identical recreate and convergence

- Repeat the exact original `CreateLesson` call only after verified promotion.
- Require `created == false`.
- Require status remains active.
- Require message type and the complete non-lifecycle tuple multiset, including expected empty datatype, remain
  unchanged.
- Require the matcher result remains exactly the same one-item result.

The second create is an idempotency assertion, not transport recovery, and varies no ignored field. It makes no #979
conflict-identity ruling.

## Cleanup and failure behavior

Track the exact evidence and lesson IDs before their mutation attempts so commit-unknown outcomes can still be
inspected during cleanup.

Cleanup runs before a successful result is finalized:

1. authoritative-read each tracked ID;
2. treat typed not-found as already clean;
3. delete each present entity with its observed revision through generic `Delete`;
4. never claim `LessonProjectionContract` scopes or authorizes Delete; and
5. use no direct lesson KV deletion.

Primary and cleanup errors are joined so cleanup cannot hide the stage failure. A cleanup failure after otherwise
successful stages fails the scenario rather than leaving silent state.

`Scenario.Teardown(ctx)` closes the one validation client. The shared runner currently logs Teardown errors as
warnings at `cmd/e2e/main.go:504-508`; #1030 does not change that cross-scenario policy. `docker compose down -v` is
the final task recovery boundary.

Setup failure after the NATS connection opens closes that client before returning and joins any close error with the
setup error.

## Exact artifact delta

### Add

- `test/e2e/scenarios/lessons/scenario.go`
- `test/e2e/scenarios/lessons/scenario_test.go`
- `taskfiles/e2e/lessons.yml`
- this correction artifact

### Modify

- `cmd/e2e/main.go`
  - import the lessons scenario;
  - list `e2e:lessons` and the `lessons` scenario;
  - dispatch `--scenario lessons`.
- `Taskfile.yml`
  - include `taskfiles/e2e/lessons.yml` as `e2e:lessons`.
- `docs/concepts/32-agent-memory.md`
  - name `task e2e:lessons` as the direct product birth/lifecycle/reader-matcher gate;
  - do not claim it runs an agent or proves a later assembled loop;
  - leave the configured agent example as an example, not the product gate.
- the current #1030 design/OpenSpec artifacts
  - mark the ops-extension plan superseded;
  - retain its body as history;
  - append corrected implementation truth and tasks.

### No change

- `configs/protocol-flow.json`
- every `configs/flows/*` file
- every persona or prompt
- `docker/compose/e2e.yml`
- `docker/compose/ops.yml`
- `test/e2e/scenarios/ops/*`
- `test/e2e/mock/*`
- `test/e2e/harness/lessoncuration/*`
- `cmd/e2e-semstreams`
- `openspec/specs/agentic-lessons/spec.md`
- ADR-027, ADR-080, or ADR-097
- `openspec/changes/own-lesson-curator-contract/tasks.md`
- CI/nightly/e2e-all wiring
- sister repositories

## TDD sequence

### RED

1. Add a test requiring the actual `Scenario.stages()` plan to contain the exact three names above.
2. Add an early-failure test proving the failed stage is uncounted and later stages do not run.
3. Add fixture tests requiring the fixed identity inputs, opaque precomputed ID, exact valid message type, and complete
   caller-authored tuple set including expected datatype.
4. Add authoritative comparators for proposed, active, and post-recreate states that compare `Datatype` with every
   other tuple field.
5. Add matcher tests proving proposed exclusion and exact active inclusion.
6. Add resource tests proving:
   - one NATS owner;
   - setup rollback after post-connect composition failure;
   - exact-ID revision-fenced cleanup;
   - joined primary and cleanup errors; and
   - no cleanup of untracked IDs.
7. Add CLI dispatch/list tests if the current CLI test seam supports them; otherwise the focused scenario tests and
   command smoke provide the RED.

### GREEN

1. Compose existing clients over the one scenario connection.
2. Implement the three-stage plan.
3. Register the scenario in the CLI.
4. Add the standalone task reusing the core compose stack.
5. Correct the concept guide.

### Refactor guard

Keep the fixture, authoritative comparator, stage runner, and cleanup helpers private to the E2E scenario. Export no
production or test helper solely for future reuse.

## Verification

Focused:

```text
go test ./test/e2e/scenarios/lessons
go test -race ./test/e2e/scenarios/lessons
go test ./cmd/e2e
```

Repository:

```text
task lint
go test -race ./...
task test:integration
task schema:generate
git diff --check
openspec validate cover-product-lesson-write-e2e --strict
```

Assembled:

```text
task e2e:lessons
```

The assembled gate requires:

- production-target core stack healthy;
- direct create returns true;
- proposed authority and matcher exclusion proved;
- promotion succeeds with the scenario-owned evidence resolved;
- active authority and exact matcher inclusion proved;
- identical recreate returns false without overwriting active status;
- full tuple evidence recorded;
- `AssertionsRun == 3`;
- failures exit nonzero;
- exact-ID cleanup completes; and
- compose teardown completes.

No `task e2e:ops`, CI, nightly, `e2e:all`, or sister-validation change is part of #1030.

## Completion boundary

Issue #1030 is complete when the independent direct-product path above is reviewed and green.

It becomes valid prerequisite evidence for a later ops-agent retirement change. It does not itself:

- retire an ops or deep-research demo;
- decide the fate of `task e2e:ops`;
- retire `emit_diagnosis` or `ops.diagnosis.*`;
- introduce reportable conditions;
- repair automatic debrief triggering;
- settle #979;
- reopen #1029;
- change CI/nightly scope; or
- claim sister adoption.

## Proposed owner correction rulings

1. Mark the prior ops-stage-10–12 design and its OpenSpec tasks superseded before implementation.
2. Add a standalone `lessons` scenario and `task e2e:lessons`.
3. Reuse the production-target core compose stack and `configs/protocol-flow.json` unchanged.
4. Add no lesson-specific flow config, compose stack, port allocation, mock, or E2E handler.
5. Use one scenario-owned `NATSValidationClient`; all lesson clients are non-owning views over it.
6. Birth through `NewNATSLessonStore.CreateLesson` with one valid caller-authored message type and complete tuple set.
7. Compose `NewLessonCurator` from one local projection client containing `LessonProjectionContract`; keep
   `NewNATSLessonCurator` retired.
8. Prove proposed exclusion and active inclusion through `NewNATSLessonReader` plus `lessonmatch.Match`.
9. Supersede the issue's literal later-loop assembled proof with reader/matcher eligibility plus the existing
   matcher-to-brief unit seam; do not claim an assembled loop proof.
10. Compare the complete happy-path semantic tuple set, including expected datatype, at birth and preserve every
    non-lifecycle tuple through promotion and identical recreate.
11. Repeat the exact original create only after promotion; require `created == false` and active state preservation.
12. Seed one distinct scenario-owned evidence entity through a local exact projection contract and remove it during
    exact-ID cleanup.
13. Use generic authoritative read plus revision-fenced Delete for tracked IDs only; the lesson contract does not
    authorize deletion.
14. Add no raw KV, raw subject, request handler, payload, stream, bucket, or remote adapter.
15. Leave #979 and all #1029 rulings unchanged, including tasks 10–11.
16. Leave ops/deep-research retirement, reportable conditions, diagnosis retirement, and `task e2e:ops` disposition
    to separate owner-reviewed changes.
17. Require `task e2e:lessons`, focused/race/repository gates, strict OpenSpec, and independent SemStreams reviewer
    approval; add no CI/nightly scope.
