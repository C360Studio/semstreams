# Tasks

- [x] 1. Materialize the independent-path correction design, record its SHA-256, obtain independent pre-owner
      `DESIGN PASS`, and record owner acceptance before implementation.
- [x] 2. RED: add an actual `lessons` scenario stage-plan test requiring exactly `create-and-prove-proposed`,
      `promote-and-prove-match`, and `recreate-and-prove-convergence`; prove early failure is uncounted and stops later
      stages.
- [x] 3. RED: add the valid caller fixture and authoritative full-tuple comparators for proposed, active, and
      post-recreate states, including message type, every tuple field, empty datatype, optional-attribution absence,
      and lifecycle-sibling absence.
- [x] 4. RED: add reader/matcher coverage proving proposed exclusion, exact active inclusion, and unchanged inclusion
      after identical recreate.
- [x] 5. RED: add resource tests for one NATS owner, post-connect setup rollback, exact-ID cleanup, revision fencing,
      and joined primary/cleanup errors.
- [x] 6. Add `test/e2e/scenarios/lessons` and compose the existing direct store, local projection client, narrow
      curator, production lesson reader, and deterministic matcher over the scenario-owned validation client.
- [x] 7. Seed exact evidence through private contract `e2e.lessons.evidence`; create the fixed-input,
      opaque-precomputed-ID product lesson directly and prove proposed authority plus matcher exclusion without
      implementing or exporting the private identity algorithm.
- [x] 8. Promote through the local curator, prove full non-lifecycle preservation and exact active matcher inclusion,
      then repeat the identical create and require convergence without overwriting active state.
- [x] 9. Clean only tracked lesson/evidence IDs through authoritative read plus generic revision-fenced delete; use no
      raw KV and make no claim that the lesson contract authorizes deletion.
- [x] 10. Register `lessons` in `cmd/e2e`, add `task e2e:lessons`, and reuse `docker/compose/e2e.yml` plus
      `configs/protocol-flow.json` unchanged.
- [x] 11. Correct `docs/concepts/32-agent-memory.md` to name `task e2e:lessons` as the direct product
      birth/lifecycle/reader-matcher gate without claiming an assembled agent loop.
- [x] 12. Verify no changes to ops/deep-research configs or scenarios, E2E mocks, curation handler, diagnosis,
      reportable conditions, #979, #1029 downstream adoption, capability specs, ADRs, CI/nightly, or sister repos.
- [x] 13. Run focused `lessons` and `cmd/e2e` tests, including the `lessons` race test.
- [x] 14. Run `task lint`, `go test -race ./...`, `go build ./...`, `task test:integration`,
      `task schema:generate`, and `git diff --check`; record no unintended schema drift.
- [x] 15. Run `openspec validate cover-product-lesson-write-e2e --strict`.
- [x] 16. Run `task e2e:lessons`; require success, `AssertionsRun == 3`, authoritative and matcher evidence,
      nonzero failure propagation, exact-ID cleanup, and compose teardown.
- [x] 17. Record PR #1038 with `Closes #1030` and independent SemStreams implementation reviewer `FINAL APPROVE`.
