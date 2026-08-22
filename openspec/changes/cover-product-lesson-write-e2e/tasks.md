# Tasks

## Superseded ops-extension tasks — do not implement

- [ ] 1. RED: factor the actual ops scenario stage plan into a test-inspectable method and assert the exact original
      nine ordered names followed by `create-product-lesson`, `promote-and-recreate-product-lesson`, and
      `inject-and-verify-product-lesson`; do not use a synthetic twelve-stage slice as the count proof.
- [ ] 2. RED: add the valid product fixture and full-authoritative-comparison tests covering message type, entity ID,
      category, polarity, severity, proposed status, created-at, summary, detail, injection form, evidence, applies-to,
      subject, source, timestamp, confidence, optional-attribution absence, and lifecycle-sibling absence.
- [ ] 3. Retain the synthetic `runOpsStages` early-failure test proving a failed stage is not counted and later stages
      do not execute.
- [ ] 4. Compose `NewNATSLessonStore` and one local projection client over the existing `s.nats.Client()`; create no
      second NATS connection, close owner, handler, request subject, payload, bucket, stream, or runtime subscription;
      add focused coverage proving setup closes the validation client if later store/projection setup fails.
- [ ] 5. Add the caller-owned valid lesson fixture with an opaque precomputed identity token and no copied identity
      algorithm, exported limit, invalid-input case, agent attribution, role, persona, or prompt contract.
- [ ] 6. Append `create-product-lesson` as stage 10 and prove direct `CreateLesson` returns created, status reads
      proposed, and authoritative state exactly equals the supplied message type and complete happy-path triple set.
- [ ] 7. Append `promote-and-recreate-product-lesson` as stage 11, reuse the existing Promote contract unchanged,
      prove active lifecycle plus full non-lifecycle tuple preservation, repeat the exact original create, require
      `created == false`, and prove active state and the full tuple set remain unchanged.
- [ ] 8. Extend the ops mock cursor with one distinct product-injection marker after the existing agent-injection entry
      and before the never-match terminator; add a focused order test and update the preset narrative to three loops;
      add no new role, persona, tool, flow config, or production prompt.
- [ ] 9. Append `inject-and-verify-product-lesson` as stage 12, require the third ops user message to complete
      successfully, and prove the distinct mock finding is observable through the existing admitted HTTP graph read
      only after the active direct-product injection form reaches loop three.
- [ ] 10. Track only the exact scenario-created lesson IDs and clean them through generic authoritative read plus
      revision-fenced `Delete`; record cleanup failures as warnings, add no direct lesson KV deletion, and state that
      `LessonProjectionContract` validates client construction but neither scopes nor authorizes delete.
- [ ] 11. Correct `docs/concepts/32-agent-memory.md` to name `task e2e:ops` and both covered writer lanes; correct the
      stale `submit_work` narrative in `taskfiles/e2e/ops.yml`; update scenario/package descriptions to both lanes and
      twelve stages, and change no runtime estimate without measurement.
- [ ] 12. Run focused unit and race gates:
      `go test ./test/e2e/scenarios/ops`, `go test ./test/e2e/mock/...`, and
      `go test -race ./test/e2e/scenarios/ops ./test/e2e/mock/...`.
- [ ] 13. Run repository gates: `task lint`, `go test -race ./...`, `task test:integration`, `task schema:generate`,
      and `git diff --check`; record that schema generation produced no unintended drift.
- [ ] 14. Run `openspec validate cover-product-lesson-write-e2e --strict` and record a green result with no capability
      spec delta and no ADR.
- [ ] 15. Run `task e2e:ops`; require success, `AssertionsRun == 12`, all original and new stage metrics, complete
      authoritative tuple evidence, direct create/proposed/promote/re-create/active results, distinct product injection
      proof, nonzero failure propagation, and Docker teardown.
- [ ] 16. Obtain independent SemStreams reviewer approval for the implementation and recorded verification before
      integration.
- [ ] 17. Close #1030 only after every task above is complete; do not claim #979, automatic ops triggering, reporting,
      CI/nightly coverage, #1029 downstream adoption, or sister-repository work is complete.

## Corrected implementation tasks

- [x] 18. Materialize the independent-path correction design, record its SHA-256, obtain independent pre-owner
      DESIGN PASS, and record owner acceptance before implementation.
- [x] 19. RED: add an actual `lessons` scenario stage-plan test requiring exactly
      `create-and-prove-proposed`, `promote-and-prove-match`, and
      `recreate-and-prove-convergence`; add early-failure accounting proving the failed stage is uncounted and later
      stages do not run.
- [x] 20. RED: add the valid caller fixture and authoritative full-tuple comparators for proposed, active, and
      post-recreate states, covering message type, entity ID, every predicate/object, subject, context, source,
      timestamp, confidence, expected empty datatype, expiry, optional-attribution absence, and lifecycle-sibling
      absence.
- [x] 21. RED: add reader/matcher coverage proving proposed exclusion, exact active inclusion, and unchanged inclusion
      after identical recreate.
- [x] 22. RED: add resource tests for one NATS owner, setup rollback after post-connect composition failure,
      tracked-exact-ID cleanup, revision fencing, and joined primary/cleanup errors.
- [x] 23. Add `test/e2e/scenarios/lessons` and compose `NewNATSLessonStore`, one local projection client containing
      `LessonProjectionContract`, `NewLessonCurator`, `NewNATSLessonReader`, and `lessonmatch.Match` over the existing
      validation client.
- [x] 24. Seed exact evidence ID `c360.streamkit-pure.test.fixture.evidence.product-lesson` through private contract
      `e2e.lessons.evidence`, with message type `test.fixture.v1`, the exact entity pattern, registered birth predicate
      `vocabulary.DCTermsTitle`, indexing profile `control`, and one exact valid title tuple; create the fixed-input,
      opaque-precomputed-ID product lesson directly and prove proposed authority plus matcher exclusion without
      implementing or exporting the private identity algorithm.
- [x] 25. Promote through the local curator, prove full non-lifecycle preservation and exact active matcher inclusion,
      including every tuple's expected empty datatype, then repeat the identical create and require
      `created == false` with active state and datatype-preserving full tuples preserved.
- [x] 26. Clean only tracked lesson/evidence IDs through authoritative read plus generic revision-fenced Delete; use
      no raw KV and do not claim the lesson contract authorizes deletion.
- [x] 27. Register `lessons` in `cmd/e2e`, add `task e2e:lessons`, and reuse `docker/compose/e2e.yml` plus
      `configs/protocol-flow.json` unchanged.
- [x] 28. Correct `docs/concepts/32-agent-memory.md` to name `task e2e:lessons` as the direct product
      birth/lifecycle/reader-matcher gate without claiming an assembled agent loop.
- [x] 29. Verify no changes to ops/deep-research configs or scenarios, E2E mocks, curation handler, diagnosis,
      reportable conditions, #979, #1029 tasks 10–11, capability specs, ADRs, CI/nightly, or sister repositories.
- [x] 30. Run `go test ./test/e2e/scenarios/lessons`, `go test -race ./test/e2e/scenarios/lessons`, and
      `go test ./cmd/e2e`.
- [x] 31. Run `task lint`, `go test -race ./...`, `task test:integration`, `task schema:generate`, and
      `git diff --check`; record no unintended schema drift.
- [x] 32. Run `openspec validate cover-product-lesson-write-e2e --strict`.
- [x] 33. Run `task e2e:lessons`; require success, `AssertionsRun == 3`, all authoritative and matcher evidence,
      nonzero failure propagation, exact-ID cleanup, and compose teardown.
- [x] 34. Obtain independent SemStreams implementation review approval before integration (`FINAL APPROVE`).
- [ ] 35. Close #1030 only within the corrected completion boundary; make no ops-agent, deep-research,
      `task e2e:ops`, diagnosis, reportable-condition, #979, #1029-adoption, CI/nightly, or sister-work completion claim.
