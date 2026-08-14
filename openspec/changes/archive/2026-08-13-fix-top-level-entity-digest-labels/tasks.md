# Tasks: Complete top-level EntityDigest labels

## 1. Accepted inventory and design

- [x] 1.1 Materialize the complete inventory at body SHA
      `892d44fa1d8b8f412b32d62c79026dc4d82e3176c9f42c4cc3efd506933b3d17` and obtain `INVENTORY PASS`.
- [x] 1.2 Materialize the corrected design at body SHA
      `1840aede76ed9006acc61f27344def6c22bc53444027095a3772733cdf5475e7` and obtain `DESIGN PASS`.
- [x] 1.3 Record owner approval of the reviewed design on 2026-08-13.

## 2. TDD implementation

- [x] 2.1 Add behavior tests for ID-joined display text, reordered and partial batch responses, ordinary hydration
      failure, authoritative graph-state poison, and exact label-resolution order.
- [x] 2.2 Observe focused RED against the pre-change producers and preserve its reproducible command/output.
- [x] 2.3 Resolve auto-summary's final bounded IDs through `resolveEntityLabels` and `buildEntityDigests` without
      changing its existing relevance semantics.
- [x] 2.4 Enrich direct fallback's existing rows with type/label only, preserving duplicate rows and per-row relevance.
- [x] 2.5 Assert exactly one added `entityBatch` request per affected branch and no request for already-correct
      LocalSearch branches.
- [x] 2.6 Use explicit callbacks/channels in integration tests; add no arbitrary sleeps.

## 3. Product-path proof

- [x] 3.1 Measure and record a deterministic titled semantic fixture that enters forced compact results with
      `summarizeThreshold=1`.
- [x] 3.2 Strengthen the existing semantic known-answer E2E to require that exact term in `EntityDigest.Label`, not its
      ID, without editing or activating `semantic-tier-split`.
- [x] 3.3 Rerun the complete semantic E2E after the exact ID-plus-label assertion is present and record 48/48 GREEN.

## 4. Verification and review

- [x] 4.1 Run focused graph-query tests and `go test -race ./processor/graph-query`.
- [x] 4.2 Run `go test ./test/contract/...` and strict OpenSpec validation.
- [x] 4.3 Run schema generation and confirm no generated drift.
- [x] 4.4 Run `task lint`, repository-wide race tests, build, and the tightened semantic E2E tier.
- [x] 4.5 Complete the binding-ruling conformance table with exact `file:line` evidence and run the correction-
      propagation sweep.
- [x] 4.6 Obtain independent SemStreams implementation review with no unresolved findings.

## Executed evidence

- RED: `go test ./processor/graph-query -run
  'Test(GlobalSearchAutoSummaryHydratesFinalDigestLabelsByID|SearchGraphDirectFallbackEnrichesExistingRowsWithOneIDJoinedBatch|SearchGraphDirectFallbackHydrationFailureAndPoison|LocalSearchDoesNotAddASecondLabelHydrationBatch)$'
  -count=1` failed because both new producer tests observed zero top-level `entityBatch` requests; the LocalSearch
  control did not fail. Production-file backups were restored and verified by matching MD5 checksums before the
  implementation was applied.
- Focused GREEN: the expanded graph-query behavior set passed with `-count=1`.
- Package race GREEN: `go test -race ./processor/graph-query -count=1` passed in 2.912s.
- Scenario race GREEN: `go test -race ./test/e2e/scenarios -count=1` passed in 2.375s.
- Contract GREEN: `go test ./test/contract/...` passed in 2.145s. Focused vet and `git diff --check` were clean.
- Schema generation GREEN with no diff under `schemas/` or `specs/`.
- `task lint` and `task build` passed. Repository-wide `go test -race ./... -count=1` passed outside the sandbox;
  the sandboxed attempt failed only because localhost listener binds were prohibited.
- Measurement run before the final exact-label assertion was added: semantic E2E passed 48/48 in 11m00s with
  known-answer 3/3 at `summarizeThreshold=1`. A read-only synchronized query then measured
  `c360.logistics.content.document.operations.doc-ops-001` with exact label `Forklift Operation Manual` and relevance
  0.8110. The final scenario requires that exact ID-plus-label pair.
- Final post-assertion semantic E2E: `task e2e:semantic` passed 48/48 in 11m23s. The decisive
  `validate-globalsearch-known-answer` stage passed in 1m35s with three probes and zero failures. Structured evidence
  was written to `cmd/e2e/test/e2e/results/semantic-20260813-123602.json`; the Compose stack then tore down cleanly.
- Independent SemStreams implementation review returned `APPROVE` with no findings after verifying the complete
  diff, accepted hashes, conformance rows, focused/race/contract tests, strict OpenSpec, and conservative pre-final-E2E
  task truth.
