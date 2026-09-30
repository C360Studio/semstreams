# #1222 Result/Writer R1–R2 checkpoint

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams` at HEAD
`fe6e2cc03e16f5db47e293f55939548572f204cc`. These five files are frozen for
independent static re-review after the R1/R2 correction:

| File | SHA-256 |
| --- | --- |
| test/e2e/scenarios/scenario.go | 55944b35bb995c94b72d2f3376c78d170d4bbb8b128e2f7a3149f2ec1c5248e4 |
| test/e2e/scenarios/evidence.go | 5ffa1c2f0416a3bdcd672f03c1ad4952cfa9998bd9c68e6ad270285f61069c60 |
| test/e2e/scenarios/evidence_test.go | ba466d6166d5cc6424a3ca608c36c43706b93cf26b99622ccb4c0849b0c9ac84 |
| test/e2e/results/writer.go | d958b35ee54ace7df3ca4c6bb0d11b01e7322989d87c4f251e7efd88dcd8a61e |
| test/e2e/results/writer_test.go | 60262a27fe72f3affe1b60919b59963f5395df4c7aa30dfdf71c8a8f02195fbf |

R1: `checkRequiredEvidence` now detaches `Structured` from its validation copy.
Terminal `WriteRun` finalizes the retained adopted Result before projecting
typed metadata, while preserving an already false execution outcome even if all
named checks pass. Tests cover direct validation, false without `Error` or
`Errors`, missing observation, and persisted round trips.

R2: complete proof now needs `log_path`/`log_sha256` and
`artifact_manifest_path`/`artifact_manifest_sha256`. Writer records the actual
absolute `output_dir`; the common WriteRun/LoadRun predicate checks reference
shape and digests. Historical readers do not reopen machine-local paths. Test
fixtures retain synthetic reference files and actual digests. Real Task/CLI
artifact production and content verification remain in later slices.

Behavioral RED was observed from `go test ./test/e2e/results -run
'TestRequiredProofValidationDoesNotMutateTypedOutcome|TestWriteRunKeepsFalseExecutionOutcomeAcrossRoundTrip|TestRequiredRunNeedsRetainedReferences'
-count=1 -v`: all three tests failed on the intended assertions before fixes.
`TestWriteRunProjectsMissingObservationFailure` also failed on its intended
assertion before the retained-finalization fix. The raw RED output was displayed
by the tool but was not written to a file; it is not a reproducible raw artifact.

GREEN: `GOCACHE=/private/tmp/semstreams-1222-go-cache go test
./test/e2e/results -count=1 -v` exit 0, full output
`/private/tmp/semstreams-1222-result-writer-green.log` SHA-256
`b90389b6f0f13747457365e319ccf4d0e0d1c4979e5ec8affdaa73215c73ee6d`.
Focused scenarios evidence tests (including two Rapid properties and fuzz seed
replay) exit 0, `/private/tmp/semstreams-1222-result-scenario-green.log` SHA-256
`132862a66093fef59068b577711338c4188d13744f9f2d00bc4e825567805421`.
`git diff --check` on the five files exit 0. Full scenarios package tests
compiled but could not bind `httptest` local listeners under the sandbox;
the failed log is `/private/tmp/semstreams-1222-result-r1r2-green.log`.

Open gates: independent R1/R2 re-review, actual manifest/log producer and
content checks, member/child Task API and report dispatch, core scenario proof,
mutation evidence, race and release gates. No Docker, integration, heavy E2E,
commit or push was run in this slice.

## Subsequent core checkpoint (separate from frozen R1/R2 files)

The paused core files were formatted and their tests supplied explicit
`EvidenceRunID`/`EvidenceMemberID`. New pure validator cases cover distinct
count, duplicate output, malformed selected JSON, unsent/foreign identity,
changed values, and pass-through values below and above 50. The focused core
health, UDP input and pure validator tests pass; the localhost tests needed
`require_escalated` because the default sandbox denied ephemeral listeners.
The same focused set passed under `go test -race` (exit 0, log
`/private/tmp/semstreams-1222-core-race.log` SHA-256
`a784d5f381911ec7b6ceb1dc9df26f0565bddf57eaae895945937212c5856348`).
The frozen Writer package also passed `go test -race` (exit 0, log
`/private/tmp/semstreams-1222-result-writer-race.log` SHA-256
`2d66f71af611fa73817fa9de6563d51bacb39009e76157bfeda1f6bee135b3d2`).

Core mutation: copied `core_dataflow.go` to
`/private/tmp/semstreams-1222-core_dataflow.go.bak`; both had SHA-256
`0ad581f8d56e1fadd620a3094e821b81b4c376535da1ed3e9259fa900f188c0d`.
The valid mutant changed `if !seen[*data.Sequence]` to `if true`.
`TestCorePassThroughRequiresDistinctSentContentAndIdentity/duplicate_cannot_inflate`
failed on 2 versus 1 distinct sequence, exit 1; raw log
`/private/tmp/semstreams-1222-core-mutant-red.log` SHA-256
`6af38eaf4400e05fb79730cb9aaa5536869411083391b66c143defe6e9670dce`.
Restored by `cp`; source and backup SHA matched; the exact focused test then
passed, exit 0, log `/private/tmp/semstreams-1222-core-restored-green.log`.
