# Issue #1030 Accepted Implementation Design

Status: independent-path correction accepted by the owner on 2026-08-22 after independent SemStreams CORRECTION
DESIGN PASS.

The former ops-extension body below is superseded before implementation and retained as accepted history. It MUST NOT
be implemented. The active correction is
`docs/proposals/gh1030-product-lesson-write-e2e-design-correction.md`, SHA-256
`299c1adfa94a13af551fc34729f2374a5707fc88d6baf70f2345497ef0f1b8ff`, which received independent review and owner
acceptance before implementation.

Repository baseline: `c5e7255298da0be9dc2823e15e134fe32c77adb3`.

The complete accepted design is
`docs/proposals/gh1030-product-lesson-write-e2e-design.md` at SHA-256
`e55324ae80fbf2d43f62535d7867bf6190c054a7498c9320fa64eb8dae313a36`.

This artifact is the active-change implementation truth. If it conflicts with the immutable accepted design, the
accepted design controls and implementation stops for owner review.

## Accepted target

Extend the existing ops scenario with an external-style direct-store lane after the current nine stages:

1. `create-product-lesson`
2. `promote-and-recreate-product-lesson`
3. `inject-and-verify-product-lesson`

The scenario's final assertion count is twelve. The original nine stage names and order remain unchanged.

## Existing surfaces only

- Birth uses `NewNATSLessonStore(s.nats.Client()).CreateLesson`.
- Status uses `LessonStore.ReadLessonStatus` plus public authoritative read.
- Promotion uses the existing `e2e.control.lesson.promote` request/response unchanged.
- Independent persisted graph evidence uses the scenario's existing admitted HTTP graph read.
- Cleanup uses the public projection client's generic `ReadAuthoritative` and revision-fenced `Delete` for exact
  scenario-tracked IDs.

No new create subject, adapter, handler, runtime subscription, connection, payload, bucket, stream, store, or graph
owner is introduced.

## Connection and resource ownership

- `Scenario.Setup(ctx)` continues to create one `NATSValidationClient`.
- The store and projection client both reuse `s.nats.Client()` and are non-owning.
- The projection client is constructed with `LessonProjectionContract()` so the snapshot is validated at client
  construction.
- `DeleteMutation` has no contract field. The snapshot does not scope or authorize cleanup deletion.
- Cleanup safety comes from a closed set of exact scenario-tracked lesson IDs and observed revision fencing.
- `Scenario.Teardown(ctx)` closes the one validation client with the supplied context.
- If store or projection-client setup fails after the validation client connects, setup closes that client before
  returning; focused coverage proves the rollback and single close authority.
- The scenario stores no context and adds no goroutine, watcher, subscription, or cancel authority.

## Caller-authored fixture

The fixture supplies:

- `agentic.AgentLessonMessageType()`;
- a six-part `AgentLessonEntityID` with an opaque precomputed product fixture token;
- valid category, polarity, severity, and exactly `proposed` birth status;
- RFC3339 created-at, summary, detail, and a distinct bounded control-byte-free injection form;
- resolvable `SeedLoop1ID` evidence and `tag:ops` applicability;
- identical subject, source `e2e-product-lesson`, timestamp, and confidence `1.0` on every triple.

Optional loop attribution is absent because the writer is product code, not an agent loop. The fixture adds no public
identity algorithm, exported limit, or malformed-input assertion.

## Full authoritative proof

At initial birth, authoritative state MUST equal:

- the caller-supplied message type; and
- the complete supplied triple multiset, including subject, predicate, object, source, timestamp, and confidence.

The created-at object MUST equal the caller's RFC3339 value. No optional attribution or lifecycle sibling may appear.

After promotion, authoritative state MUST retain the exact caller-supplied non-lifecycle tuples—category, polarity,
severity, created-at, summary, detail, injection form, evidence, and applies-to—including provenance, timestamp, and
confidence. Proposed status MUST be absent; exactly one active status MUST exist; retired-at and superseded-by MUST be
absent.

After an identical post-promotion `CreateLesson`, `created` MUST be false, active status MUST remain, and the same
message type and full non-lifecycle tuple set MUST remain authoritative. The test varies no ignored field and therefore
does not settle #979 conflict-identity policy.

## Real stage-plan TDD

Factor the production ordered stage slice into a test-inspectable `(*Scenario).stages()` method and make `Execute`
consume it. The RED test inspects that actual plan and requires the exact original nine names followed by the three
accepted product-stage names. A synthetic `successfulOpsStages(12)` slice is not acceptance evidence.

Retain the small synthetic early-failure test only for `runOpsStages` behavior: a failed stage remains uncounted and
later stages do not run.

## Mock and injection proof

The current ops mock cursor remains on the existing agent-injection marker through loop two. Insert one distinct
direct-product injection marker after that entry and before the never-match terminator. Loop three can fire the new
entry only when brief assembly contains the active product lesson's injection form. The entry emits a distinct
diagnosis finding that the scenario observes through the existing HTTP graph read.

A focused mock-order test MUST prove the product marker follows the original agent-injection marker and precedes the
never-match terminator. Package, scenario, and mock preset narratives MUST describe both writer lanes, twelve stages,
and three loops after the implementation exists.

No role, persona, tool, flow configuration, or production prompt changes.

## Failure, retry, and cleanup

- Send the first create once. Commit-unknown, unavailable, or classified failure fails the stage; do not blind-retry.
- Request promotion once. Bounded polling observes the requested transition and never resends it.
- Perform the second create only after verified birth and verified promotion. It is the idempotency assertion, not
  transport recovery.
- Every operation receives the current stage context and every poll honors `ctx.Done()` plus `CompleteTimeout`.
- Exact-read each tracked lesson during deferred cleanup and generic-delete it at the observed revision.
- Missing means clean. Revision conflict, commit-unknown, or other cleanup error becomes a warning and cannot hide the
  primary stage result.
- The normal `task e2e:ops` `docker compose down -v` remains the final recovery boundary.
- Existing raw KV synthetic-loop fixtures are unchanged and are not precedent for the lesson path.

## Documentation and specification truth

- Correct `docs/concepts/32-agent-memory.md` from `task e2e:agentic` to `task e2e:ops` and name both covered writer
  lanes.
- Correct only the stale `submit_work` narrative in `taskfiles/e2e/ops.yml`; measure before changing a runtime estimate.
- Add no capability spec delta: the change proves current valid birth/lifecycle/injection behavior and must not encode
  #979 malformed-input policy as current truth.
- Add no ADR: no architecture or cross-repository contract changes.
- Do not change `own-lesson-curator-contract` tasks 10–11.

## Required gates

```text
go test ./test/e2e/scenarios/ops
go test ./test/e2e/mock/...
go test -race ./test/e2e/scenarios/ops ./test/e2e/mock/...
task lint
go test -race ./...
task test:integration
task schema:generate
git diff --check
openspec validate cover-product-lesson-write-e2e --strict
task e2e:ops
```

The assembled result MUST be successful with `AssertionsRun == 12`, all original stage metrics, all three new stage
metrics, verified direct create/proposed/promotion/re-create/active results, the full authoritative tuple proof, the
third ops user message completing successfully, the distinct product-injection finding, nonzero failure propagation,
and Docker teardown.

No CI/nightly wiring is part of this change.

## Accepted owner rulings

1. Use `NewNATSLessonStore` over `s.nats.Client()`; add no second connection or create adapter.
2. Extend the existing ops scenario.
3. Preserve nine stages and append three, totaling twelve.
4. Reuse the Promote contract unchanged.
5. Compare the complete valid caller-authored semantic set; make no malformed-input ruling.
6. Use an opaque fixture identity without adding an identity algorithm.
7. Verify identical post-promotion create convergence and active-state preservation.
8. Use generic revision-fenced cleanup for exact tracked IDs; the projection contract does not authorize deletion.
9. Preserve all #1029 composition rulings.
10. Leave #979 policy unresolved.
11. Update task documentation/OpenSpec truth without a capability-spec or ADR delta.
12. Require `task e2e:ops`; add no CI/nightly scope.
13. Make no sister-repository changes or adoption claims.

## Corrected implementation truth

The corrected path is a standalone `lessons` scenario and `task e2e:lessons` over the unchanged production-target
core compose stack. It uses one scenario-owned `NATSValidationClient` to compose `NewNATSLessonStore`, a local
projection client containing `LessonProjectionContract`, `NewLessonCurator`, `NewNATSLessonReader`, and
`lessonmatch.Match`.

The scenario has exactly three stages: `create-and-prove-proposed`, `promote-and-prove-match`, and
`recreate-and-prove-convergence`; successful completion requires `AssertionsRun == 3`.

The lesson fixture uses fixed category `retention-policy`, scope `tag:product-lesson-e2e`, summary
`Scope retention sweeps to entity-owned buckets.`, and evidence ID
`c360.streamkit-pure.test.fixture.evidence.product-lesson`. Its opaque precomputed identity token is
`54b545de-8f18-5419-b996-220d3c992c5c`, yielding exact lesson ID
`c360.streamkit-pure.agent.lesson.record.54b545de-8f18-5419-b996-220d3c992c5c`. That token is known to correspond to
exactly those four inputs under the current private content-derived identity algorithm. The scenario neither
implements nor exports that algorithm. This preserves current content-derived capability truth without deciding
Issue #979's API or identity design.

The private evidence contract is named `e2e.lessons.evidence`, uses message type `test.fixture.v1`, exact entity
pattern `c360.streamkit-pure.test.fixture.evidence.product-lesson`, birth predicate `vocabulary.DCTermsTitle`, and
indexing profile `control`. Its exact created entity uses the same ID, message type
`message.Type{Domain: "test", Category: "fixture", Version: "v1"}`, version 1, and one title triple whose object is
`product lesson E2E evidence`, source is `e2e-product-lesson`, context is `e2e-lessons-evidence-create`, timestamp is
the fixed fixture timestamp, confidence is `1.0`, datatype is empty, and expiry is nil.

Every lesson-state comparator includes `Datatype`. The expected datatype for every caller-authored lesson triple is
the exact supplied empty string. Birth compares it with every other complete tuple field; promotion and identical
recreate preserve it for every non-lifecycle tuple.

The issue's later-loop suggestion is superseded by assembled production-reader plus deterministic-matcher eligibility
and the existing matcher-to-brief unit seam. No agent loop, mock, role, persona, prompt, user message, diagnosis, or
reportable-condition surface participates.

No config, compose, E2E curation handler, capability spec, ADR, production API, wire path, CI/nightly lane, #979
policy, #1029 ruling, or sister-repository state changes.

## Corrected verification evidence

Repository verification on the final candidate is green:

- `go test -race ./...`, `task lint`, and `go build ./...` passed.
- `task schema:generate` passed with zero tracked drift under `schemas/` or `specs/`.
- `git diff --check` and `openspec validate cover-product-lesson-write-e2e --strict` passed.
- The first full `task test:integration` run had one unrelated Docker mapped-port timeout in the existing
  `natsclient TestUpdateWithRetryRev_ReportsRevisionOnCreatePath`. Its exact isolated retry passed in 2.046 seconds,
  and the complete integration suite then passed on rerun.

The first assembled `task e2e:lessons` run failed during setup because first-party vocabulary registration was
missing; its deferred compose cleanup removed the containers, network, and volume. The focused fix calls canonical
`builtins.Register` and includes coverage for that registration. The second run against the exact corrected candidate
passed with:

- the production-target core stack healthy;
- all three direct-product stages complete and `assertions_run=3`;
- scenario-owned promotion evidence resolved exactly once (`evidence_resolved=1`);
- exact tracked lesson/evidence cleanup complete; and
- compose containers, network, and volume removed.
