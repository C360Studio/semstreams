# #1222 core scenario slice pause checkpoint

Parent requested a pause immediately after the frozen Result/Writer round-one review. No source edits, test runs, formatting, or fixes occurred after that request.

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams` on `codex/gh1222-required-e2e-proof`. The frozen five Result/Writer files remain untouched by this slice. Files changed in the new core slice:

- `test/e2e/scenarios/core_health.go` SHA256 `7e4151cbcd704303ce52d9ac926830a978472155c6fb4436a88366669b10fe5a`
- `test/e2e/scenarios/core_dataflow.go` SHA256 `853a1322fdde394ccb4f218f39da83ce8765e3a9a901ce5080e3af2e0308669f`
- New `test/e2e/scenarios/core_evidence_test.go` SHA256 `ebc8d28592ee3e8c0896c5cff2c1e9cfa3a62b04414eb0a689e39d0db883e947`

The only tests actually run for this slice were the two initial behavioral RED controls, before the implementation edits: `go test ./test/e2e/scenarios -run 'TestCoreHealthRecordsComponentObservation|TestCoreDataflowSentInputHasRunIdentity' -count=1 -v`, exit 1. The health test observed no named component check; the dataflow test observed sent packets without a run ID. Retained output: `/private/tmp/semstreams-1222-core-red.log` SHA256 `5f273d6da32241a757812b8de618ca56c10e28d0609ce0a12c8a97f3cc2f10c6`. No GREEN, formatting, compile, or race claim is made for the current core bytes.

Current partial implementation adds `EvidenceRunID` and `EvidenceMemberID` to `CoreHealthConfig` and `CoreDataflowConfig`; both expose `CheckRequirements`. Health records the configured component assertion. Dataflow retains successfully sent sequence/value/timestamp, sends a run marker, removes component-only fallback, reads file output without a 20-line head cap, and adds a `payload.data` pass-through validator plus count/content/identity observations. These edits are incomplete and unverified. The current tests still construct default configs without identities, so they need the agreed explicit test identities before a GREEN attempt. The dataflow validator has no adversarial tests yet; current source may have syntax, formatting, or semantic defects. Do not cite it as passed or review-ready.

Runner agent confirmed constructor bridge fields `EvidenceRunID` and `EvidenceMemberID` and member IDs `core-health`, `core-dataflow`. On resume: incorporate formal Result/Writer review findings first as directed by root; then re-open core diff, complete TDD tests for exact sent-record comparisons (distinct count, malformed selected JSON, foreign/unsent identity, changed value, retrieval failure), format/compile/focused/race, mutation evidence, and send runner the stable core config API. Do not edit Claude #1404 files or frozen Result/Writer files until root releases review hold.
