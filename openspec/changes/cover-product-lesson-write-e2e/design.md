# Issue #1030 Corrected Independent-Path Design

Status: owner-accepted after independent correction design review and implemented in PR #1038.

Repository baseline: `c5e7255298da0be9dc2823e15e134fe32c77adb3`.

The accepted correction is
`docs/proposals/gh1030-product-lesson-write-e2e-design-correction.md` at SHA-256
`299c1adfa94a13af551fc34729f2374a5707fc88d6baf70f2345497ef0f1b8ff`. It supersedes the earlier ops-extension
design, which remains historical evidence in `docs/proposals/gh1030-product-lesson-write-e2e-design.md` and is not
part of this change's target state.

## Decision

Use a standalone `lessons` E2E scenario and `task e2e:lessons` over the unchanged production-target core compose
stack. Compose `NewNATSLessonStore`, a local projection client containing `LessonProjectionContract`,
`NewLessonCurator`, `NewNATSLessonReader`, and `lessonmatch.Match` over one scenario-owned `NATSValidationClient`.

This path tests the external product boundary directly. It does not add an agent loop merely to observe reader and
matcher behavior that production types expose deterministically.

## Stages and assertions

The scenario has exactly three ordered stages:

1. `create-and-prove-proposed`
2. `promote-and-prove-match`
3. `recreate-and-prove-convergence`

Successful completion requires `AssertionsRun == 3`. The stage runner counts only completed stages and stops after a
failure.

The proof covers valid direct product birth, proposed-state matcher exclusion, evidence-gated promotion, exact active
matcher inclusion, identical recreate convergence, full authoritative tuple preservation including datatype, and
revision-fenced cleanup of exact tracked IDs.

## Fixture and contract ownership

The product lesson uses fixed category `retention-policy`, scope `tag:product-lesson-e2e`, summary
`Scope retention sweeps to entity-owned buckets.`, evidence ID
`c360.streamkit-pure.test.fixture.evidence.product-lesson`, and exact lesson ID
`c360.streamkit-pure.agent.lesson.record.54b545de-8f18-5419-b996-220d3c992c5c`. The opaque token corresponds to the
current private content-derived identity algorithm; the scenario neither implements nor exports that algorithm.

The private evidence contract is `e2e.lessons.evidence`, uses message type `test.fixture.v1`, exact entity pattern
`c360.streamkit-pure.test.fixture.evidence.product-lesson`, birth predicate `vocabulary.DCTermsTitle`, and indexing
profile `control`. Every authoritative comparator includes the complete supplied tuple, including empty datatype.

The scenario owns one NATS client. Store, projection client, curator, reader, and matcher do not add another connection,
watcher, subscription, goroutine, cancel authority, bucket, stream, subject, handler, or payload. Cleanup uses
authoritative read plus generic revision-fenced delete for only the scenario-tracked IDs.

## Boundaries

- No agent loop, ops scenario, mock, role, persona, prompt, user message, diagnosis, or reportable-condition change.
- No flow config, compose, curation handler, production API, wire path, capability spec, ADR, or CI/nightly change.
- No #979 policy, #1029 downstream-adoption claim, or sister-repository mutation.
- The assembled acceptance boundary is production reader plus deterministic matcher eligibility; it does not claim a
  later assembled agent-loop brief.

## Implementation conformance

| Accepted correction | Implementation evidence |
|---|---|
| Standalone direct-product path | `test/e2e/scenarios/lessons` owns the three-stage scenario; `cmd/e2e` and `task e2e:lessons` expose it. |
| One NATS owner and existing production types | Scenario setup composes the existing store, projection client, curator, reader, and matcher over its validation client. |
| Full semantic preservation | Scenario comparators cover message type and complete tuple fields, including datatype, across birth, promotion, and recreate. |
| No private identity algorithm export | The fixture carries one opaque precomputed lesson ID and the scenario adds no production identity API. |
| Exact cleanup | Teardown authoritatively reads and revision-fenced deletes only tracked lesson and evidence IDs. |
| No agent/ops expansion | The implementation does not change ops/deep-research scenarios or configs, mocks, prompts, roles, diagnoses, or reportable conditions. |

## Verification evidence

- `go test -race ./...`, `task lint`, and `go build ./...` passed.
- `task schema:generate` passed with no tracked drift under `schemas/` or `specs/`.
- `git diff --check` and `openspec validate cover-product-lesson-write-e2e --strict` passed.
- A complete `task test:integration` rerun passed after one recorded unrelated Docker mapped-port timeout.
- The final `task e2e:lessons` run passed with the production-target core healthy, all three stages complete,
  `assertions_run=3`, `evidence_resolved=1`, exact cleanup, and compose teardown.
- The independent SemStreams implementation reviewer recorded `FINAL APPROVE` before integration.
