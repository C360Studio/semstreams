# Tasks — post-G tag-safety closeout

The #855 and #875 runtime corrections and their independent reviews are complete. No other runtime or release task is
complete. Every nontrivial implementation slice receives independent `semstreams-reviewer` approval before integration.

## P. Promotion and evidence

- [x] P.1 Record the accepted inventory, exact baseline, SHA-256, adopter seam, collision result, and owner-approved
  Option 2 boundary.
- [x] P.2 Materialize the initial seven-file OpenSpec package; record its independent `DESIGN FAIL` without claiming
  approval.
- [x] P.3 Apply every design-review correction, add the disposition/evidence artifacts, regenerate the acyclic package content root `manifest.sha256`, run strict OpenSpec validation, and verify exact package scope. No manifest-covered artifact may require the manifest's digest.
- [x] P.4 Obtain independent `DESIGN PASS` on the exact checksum-addressed corrected package.
- [x] P.5 Record final owner rulings without widening runtime authority.

## A. #855 incomplete community candidate

- [x] A.1 Replace the permanent-partial success test with a failing regression that seeds a prior partition, rejects
  one permanent-invalid candidate, permits writable sibling saves, and asserts classified error plus zero prune.
- [x] A.2 Assert `Prune` and prune-driven `Delete` are never called for an incomplete run; seed an overlapping entity mapping and prove successful candidate writes may overwrite it, so the test does not claim rollback or an unmixed stale superset.
- [x] A.3 Add a partial-mapping regression in which the community record and an earlier entity mapping write succeed before a later mapping write fails; assert classified error, zero prune/delete, mixed projection allowed, and no complete-success accounting.
- [x] A.4 Retain and pass all-permanent classification, transient no-prune, complete candidate SHALL-attempt-prune, nonfatal prune-failure, and empty candidate SHALL-attempt-Prune(nil) tests.
- [x] A.5 Continue save attempts after record-local permanent rejection, then return the wrapped existing classified
  error whenever any candidate was rejected.
- [x] A.6 Prevent higher-level construction and prune after any incomplete level.
- [x] A.7 Add component coverage proving incomplete detection performs no processed/activity/duration/complete
  accounting and no structural/anomaly pass.
- [x] A.8 Add no payload preflight, chunking, member cap, manifest, bucket, status producer, or configuration.
- [x] A.9 Run focused race tests and real-NATS storage/prune integration tests.
- [x] A.10 Obtain independent SemStreams implementation review.

## B. #875 instance-exact storage resolution

- [x] B.1 Replace contrary component/worker fallback tests with exact-match, foreign-miss, inline continuation,
  no-inline skip, deregistration-race, and resolved-read-failure regressions.
- [x] B.2 Make component admission depend only on exact current `StoreRegistry` membership.
- [x] B.3 Remove worker `contentStore` fallback, `WithContentStore`, and graph-embedding's duplicate direct ObjectStore
  construction/ownership/close path.
- [x] B.4 Preserve the existing store-read declaration and injected registry lifecycle without port/schema redesign.
- [x] B.5 Route an ordinary exact miss through the existing loud exclusion and inline path.
- [x] B.6 Route a worker-time registry miss caused by deregistration to content-unresolved plus identity-only/no-text
  behavior, never `SaveFailed` or failed/degraded accounting.
- [x] B.7 Preserve resolved exact-store Open/Read failure as bounded content failure and degraded readiness.
- [x] B.8 Update ADR-063, dependency comments, metrics/log wording, tests, and operator guidance to one exact-name
  authority with no fallback or shim.
- [x] B.9 Run focused race and integration tests, including live register/deregister ordering.
- [x] B.10 Obtain independent SemStreams implementation review.

## C. Research execute/fusion proof

- [ ] C.1 Preserve the current direct fixture and all positive/negative assertions.
- [ ] C.2 Add an explicit isolated mock/scenario mode for a deterministic non-trivial route using `walk_seeds` with
  the controlled candidate-index seed.
- [ ] C.3 Assert execute completion, positive evidence count, controlled evidence identity, assessment completion and
  sufficiency, terminal synthesis, exact evidence references, completion envelope, and R6 continuation.
- [ ] C.4 Prove the fixture traverses production `executeAll` and `fusion.Fuse`; do not use a unit-only hook.
- [ ] C.5 Run both isolated modes under the existing `task e2e:research-graph`; add no top-level tier/task family.
- [ ] C.6 Keep rules, subjects, payloads, components, and production fusion behavior unchanged.
- [ ] C.7 Obtain independent SemStreams review of fixture determinism and assertions.

## D. Truth and disposition

- [ ] D.1 Correct ADR-063's registry-miss fallback ruling to the owner-approved exact-instance decision.
- [ ] D.2 Correct ADR-068's retired predicate/context/catalog assumptions to current physical ownership and cleanup.
- [ ] D.3 Annotate suspended `semantic-tier-split` premise status without unfreezing it or completing tasks.
- [ ] D.4 Correct stale `e2e-ladder.yml` and `sister-validation.yml` comments without workflow behavior changes.
- [ ] D.5 Complete `disposition-ledger.md` for #301, #844, and #860 with binding owner decision, candidate SHA, retained or replacement coverage, exact command/result provenance, and evidence pointer.
- [ ] D.6 Complete the same ledger fields for DI-01 through DI-04, #619, #672, spatial/temporal malformed aggregate and cleanup findings, #839/#857, and #829 without implementing them in this change.
- [ ] D.7 Publish release notes and migration guidance naming the fallback clean break and accepted limitations.
- [ ] D.8 Obtain technical-writer and independent SemStreams review of exact truth propagation.
- [ ] D.9 Regenerate the package manifest after every truth or task edit.

## E. Candidate proof and tag

- [ ] E.1 Create `candidate-evidence.md` for one clean exact candidate SHA and record clean generated schemas/specs.
- [ ] E.2 Record focused tests, lint, full race, integration, schema/no-drift, contracts, and strict OpenSpec with exact
  command, UTC start/end, exit/result, runner identity, and log/artifact checksum.
- [ ] E.3 Record statistical, agentic, deep-research, both research branches, and every retained advertised
  crud-tools/ops/rule path with the same provenance fields.
- [ ] E.4 Record semantic E2E polls every 30–60 seconds with `/readyz`, counters, stage/timestamp progress, and abort
  evidence if authoritative state proves the run wedged.
- [ ] E.5 Treat any code, spec, evidence, manifest, or task correction as a new candidate requiring applicable proof.
- [ ] E.6 Record independent reviewer identity/result and exact reviewed candidate SHA.
- [ ] E.7 Record exact-SHA GitHub CI run/check identities and green results.
- [ ] E.8 Record #827 owner, scheduled boundary, result, and halt/migration outcome if the pre-v1 window closes.
- [ ] E.9 Record tag name and resolved SHA, binary version and checksum, container reference/digest and reported version.
- [ ] E.10 Regenerate the acyclic `manifest.sha256` content root, verify every entry, publish the exact tag/migration notice, and hand downstream migration to adopters. Record any manifest digest only outside the manifest-covered in-tree artifacts.
