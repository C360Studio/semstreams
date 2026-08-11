# Tasks — post-G tag-safety closeout

The #855 and #875 runtime corrections, deterministic research proof, and truth/disposition slice are complete. The
bounded pre-candidate correction is implemented, focused-green, and independently approved.
Candidate selection and proof have not begun. Every nontrivial implementation slice receives independent
`semstreams-reviewer` approval before integration.

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

- [x] C.1 Preserve the current direct fixture and all positive/negative assertions.
- [x] C.2 Add an explicit isolated mock/scenario mode for a deterministic non-trivial route using `walk_seeds` with
  the controlled candidate-index seed.
- [x] C.3 Assert execute completion, positive evidence count, controlled evidence identity, assessment completion and
  sufficiency, terminal synthesis, exact evidence references, completion envelope, and R6 continuation.
- [x] C.4 Prove the fixture traverses production `executeAll` and `fusion.Fuse`; do not use a unit-only hook.
- [x] C.5 Run both isolated modes under the existing `task e2e:research-graph`; add no top-level tier/task family.
- [x] C.6 Keep rules, subjects, payloads, components, and production fusion behavior unchanged.
- [x] C.7 Obtain independent SemStreams review of fixture determinism and assertions.

## D. Truth and disposition

- [x] D.1 Correct ADR-063's registry-miss fallback ruling to the owner-approved exact-instance decision.
- [x] D.2 Correct ADR-068's retired predicate/context/catalog assumptions to current physical ownership and cleanup.
- [x] D.3 Annotate suspended `semantic-tier-split` premise status without unfreezing it or completing tasks.
- [x] D.4 Correct stale `e2e-ladder.yml` and `sister-validation.yml` comments without workflow behavior changes.
- [x] D.5 Record #301, #844, and #860 as retained exact-candidate gates. D authorizes no fix or coverage transfer if a
  candidate result is red.
- [x] D.6 Record the binding accepted/deferred decisions for DI-01 through DI-04, #619, #672, temporal malformed and
  reverse cleanup, #839, #857, and #829 without implementing them in this change.
- [x] D.7 Publish version-independent migration guidance for the exact-instance clean break and disclosed
  limitations. Exact candidate/tag guidance remains product-Release-only.
- [x] D.8 Obtain technical-writer and independent SemStreams review of exact truth propagation.
- [x] D.9 Regenerate the package manifest as the final in-tree preparation step before candidate selection.

## PC. Bounded pre-candidate correction

- [x] PC.1 Make #860's crud-tools metrics/rule assertion fail closed: an unreachable scrape, missing required
  active-rule baseline, hot-reload timeout, or missing post-increment result fails the scenario. An absent
  pre-increment CounterVec label series remains an observed zero after collector reachability is established.
- [x] PC.2 Give one test helper sole ownership of `Cmd.Wait` across timeout and cleanup; add targeted race coverage
  proving cleanup kills and reaps through that owner and repeated observation preserves the result.
- [x] PC.3 Bind cache-disabled exact-candidate Go commands and include both core graph packages in focused proof.
- [x] PC.4 Run the full focused `go test -count=1 -race` command bound in `candidate-evidence.md` and record its green
  result before candidate selection.
- [x] PC.5 Obtain independent SemStreams review of the bounded test-truth and evidence-contract correction.

## E. Candidate proof and tag

- [ ] E.1 Select one clean immutable candidate SHA. Do not edit the candidate tree after selection.
- [ ] E.2 Verify the already-generated package manifest and collect candidate identity/cleanliness evidence using the
  in-tree `candidate-evidence.md` schema.
- [ ] E.3 Run and record the bound focused, lint, full-race, integration, schema-generation, schema-no-drift,
  contract, and strict OpenSpec commands with exact provenance. Every `go test` command uses `-count=1`; the focused
  command covers core graph packages as well as processor wrappers and support/scenario packages.
- [ ] E.4 Run and record statistical, agentic, deep-research, the single research direct-plus-execute invocation,
  crud-tools for distinct #301/#860 assertions, and ops for #844.
- [ ] E.5 Run semantic E2E with recorded 30–60 second `/readyz`, authoritative-counter, and stage-timestamp polling;
  abort and fail proof when authoritative state proves the run wedged.
- [ ] E.6 Record independent review and exact-SHA green GitHub CI in candidate proof.
- [ ] E.7 Confirm #827's named operator, tag-boundary window, and planned wipe/reseed action. Do not execute it yet.
- [ ] E.8 After every pre-tag gate is green, create the non-product `candidate-proof-<fullSHA>` Release at that SHA,
  publish its immutable proof asset, and record release-owner tag authorization. A red gate rejects the candidate
  without a failed proof Release. The asset does not require its own URL or digest. Any correction selects a new
  candidate and invalidates affected proof, review, and CI.
- [ ] E.9 Create the product tag at the approved SHA, publish binary/container artifacts, and execute coordinated #827
  immediately at the tag boundary. If its window closes, halt and convert the operation to an explicit migration.
- [ ] E.10 Verify, do not regenerate, `manifest.sha256`; publish the separate immutable product-Release attestation
  and tag-specific Release notes; then hand downstream migration to adopters.
