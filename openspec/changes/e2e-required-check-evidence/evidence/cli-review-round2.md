# #1222 CLI correction review — round 2

Mode: independent bounded implementation re-review, read-only. **APPROVE C1–C4 corrections at this snapshot.**
No new BLOCKING/HIGH finding in this correction review. This is not full CLI/Task adoption, whole-issue acceptance,
or merge approval. No tests, source edits, Git mutations, GitHub writes, Docker or other gates were run here.

## Snapshot

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Base: `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Authority: accepted design `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`, active
e2e-evidence delta and accepted implementation handoff. Compared the original CLI review's C1–C4 mechanisms with
the six corrected files, full tracked main/main_test diff, resolved selectors, real constructor/runner boundaries,
and new tests. All frozen hashes matched before and after:

| File | SHA-256 |
| --- | --- |
| cmd/e2e/main.go | d50e6283d2947e52831ddb401d7a8eb4106eec8dc2ac8417b20d1722ba2b0f52 |
| cmd/e2e/main_test.go | 969e11b27e306ae27cd58bb0094ef74adf82850ab6d5a9e3de9d2519089a5aeb |
| cmd/e2e/runner.go | 73f936d770c8fc496d95b3de7d2f50877bce635bc14aaf6bbd3b3a04001114b4 |
| cmd/e2e/runner_test.go | b980cb684a573b29d67135482112097c285250a5c28bf3607020140ff217c829 |
| cmd/e2e/selection.go | 9c4c5f0d2ceb5017a1f89e24e938f5a0e63b74b33dfc0b8864359c788cfdffa7 |
| cmd/e2e/selection_test.go | 6f6943aa1d66d4ae22aaac36e1cfafd7c27bf724ea8567e477f64d17475f5917 |

Author checkpoint `/private/tmp/semstreams-1222-task-reporting.md` SHA-256 at inspection:
`9cec6e7fba391b670bbbf86600d1551b1a164b6730e7b93191e7cf9f8b5ccf05`.
Writer Task API and report.go/report_test.go are outside this freeze and review; no stable dependency snapshot or
approval of their behavior is inferred.

## Dispositions

**C1 corrected.** selection.go:83–92 canonicalizes minted-authority and supplies the actual preidentity member names.
The constructed Scenario names remain unchanged, and the exact-name guard remains at runner.go:150–153. The seed
and assert CLI spellings are preserved for dispatch, avoiding the tempting but incorrect change to a spelling that
createScenario does not accept. Both aliases remain legacy execution until their separate required adoption is
completed; this correction does not manufacture proof. selection_test.go:29–31 independently specifies expected
names and :63–72 cross-checks the real constructors for all four spellings. The RED log shows the three original
mismatches reaching those assertions; focused GREEN is retained.

**C2 corrected.** runner.go:229–235 finishes the aggregate Writer call and accepts its finalized exit before
serializing optional typed analysis at :240–243. Writer remains the projection owner; no second finalizer was added
to the CLI. The terminal required-write error still returns one. A legacy run without an output directory cannot
emit a stale typed file because saveScenarioAnalysis retains its output-directory condition. A run with output
uses Writer before typed serialization, which repairs both final-validation and teardown failures through the
existing projection. runner_test.go:118–155 now drives those two cases through the actual selected-run path and
inspects the retained Result and standalone typed file. Both RED failures identify the stale typed success, then
focused GREEN is retained. This closes the separate CLI ordering defect, rather than relying solely on R1's
validation-copy correction.

**C3 corrected.** main.go:424 derives tier GraphQL from the caller-selected service base and trims a trailing slash.
main.go:192–201 supplies semantic's default 38180 only when neither the environment nor an explicit --base-url
provided a value; explicit localhost:38080 remains intentional, not mistaken for an absent option. This happens
before main creates its ObservabilityClient, so GraphQL and service observations share the selected base.
Constructor tests at main_test.go:63–84 inspect the actual private tier config consumed by the graph/search path,
covering all three variants and a remote trailing-slash base. Parsing tests at :19–41 cover both semantic forms,
remote explicit base and explicit standard port. Tests that mutate flag.CommandLine/os.Args do not run in parallel
and restore globals with Cleanup. Environment override handling is established statically; there is no explicit
nonempty-environment case in these new tests. This is endpoint wiring evidence, not an observed remote query run.

**C4 corrected.** runner.go:139–144 announces `evidence_status=unattested` for legacy selection before declaration
admission and any Setup. Completion output keeps its existing distinction. runner_test.go:159–175 deliberately
fails Setup and verifies that the unattested announcement precedes the setup message, so a successful completion
cannot accidentally supply the evidence for the test. The RED log shows the old late announcement and GREEN is
retained. Invalid required selections still refuse before execution without suggesting successful proof.

The selected-run path continues to preserve the frozen expectation, nil-result refusal, phase failures, required
persistence failure and Writer-finalized outer exit reviewed in round one. No new public knob, strictness opt-out,
execution owner or exported validator was introduced by these corrections.

## Evidence and limits

Read the raw RED/GREEN artifacts below and computed their hashes:

| Artifact under /private/tmp/ | SHA-256 |
| --- | --- |
| semstreams-1222-cli-c1-red.log | c6985b2f06d653387db17b18e1d00d5c2587e87204287dd3ef98f11f8d7eee00 |
| semstreams-1222-cli-c1-green.log | 4c5ebfe96b12f186eba833ddc9696627c641c1abd1120b19dc636c1e537c950b |
| semstreams-1222-cli-c2-red.log | 7628c9ce3789299c7aba9b84fb96d238834bec767b02848bdd1df54158c39408 |
| semstreams-1222-cli-c2-green.log | 182281ed66f783e7e750560bb2ac5acdaf83afdf97359b94156bc15c5e51cb7c |
| semstreams-1222-cli-c3-red.log | d78019e9869b90a7f0ea2d8569529c755458fc0710625e83250d3cb751bd5f5f |
| semstreams-1222-cli-c3-green.log | 5123215e56c5f995226ce9843fc8b9dba12956e9d3f948df10e762c63154333b |
| semstreams-1222-cli-c4-red.log | 06c119bd8803e22c03a95aac2f9e75fee237df4fcea851b279ffb4fdc4b774f6 |
| semstreams-1222-cli-c4-green.log | 41e592d7818569ac1c6c0a33a372d72b626675e57979036ce36b46e158d57db7 |

Each retained RED is a compiled assertion failure on the intended behavior, not an API-absence or cache-access
failure. The corresponding GREEN files report package success for the author's focused runs. They do not embed
exact argv/dependency hashes, so the command selection comes from the author handoff. These are retained TDD
examples, not a new claim of cp-restored mutation sensitivity or passing whole-package tests at the final snapshot.

The handoff says the full cmd/e2e run first found a synthetic manifest filename overlapping the analysis glob;
runner_test.go:83 now uses `provenance-manifest.json`, avoiding `fixture-*.json`. The handoff then records a rerun
blocked by concurrent Writer Task API symbols. During this review `/private/tmp/semstreams-1222-cli-corrections-green.log`
contained `ok github.com/c360studio/semstreams/cmd/e2e 1.118s`. That one line does not establish its exact command,
selected tests or stable dependency snapshot. Root then relayed the author's reconciliation: the exact command was
`GOCACHE=/private/tmp/semstreams-1222-go-build go test ./cmd/e2e -count=1`, redirected to that log, with exit zero
after Writer compiled. The six CLI hashes were unchanged, but Writer bytes were not pinned before that run.
This establishes author-reported full-package GREEN with an unpinned dependency snapshot. A coherent dependency
freeze plus full-package/race verification remains pending; no exact-snapshot gate claim is made here.

The synthetic provenance fixture now includes retained log/manifest paths with hashes and output_dir, matching
R2's reference shape. Its manifest remains synthetic, not actual application/Compose/fixture proof. Required
constructor identity wiring, core K1, Task child/member/report semantics, legacy comparison disposition, actual
producers, terminal-write-failure controls, remaining mutation evidence and assembled gates remain separate work.
No full application, Task, Docker, race, paid model or hosted-retention success is claimed here.

The existing examples-based PBT decision remains reasonable for these finite aliases and lifecycle outcomes;
Result owns the generated set-acceptance oracle. Required broader sensitivity deferral is accepted only for this
intermediate static correction checkpoint, not final acceptance. gopls workspace loading remained unavailable
because of restricted Go cache access; source-level caller/constructor/consumer traces from round one were used.

**APPROVE C1–C4 corrections only.** Preserve the exact frozen snapshot and evidence, then reconcile stable
dependencies and complete the remaining adoption/proof before final review.
