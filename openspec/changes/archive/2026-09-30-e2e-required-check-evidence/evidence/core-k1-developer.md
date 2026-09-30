# #1222 core K1 correction checkpoint

Worktree HEAD `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Review finding: `/private/tmp/semstreams-1222-core-review.md` K1. In
`validateCorePassThrough`, a selected malformed record now fails identity as
unproven; foreign, missing or unsent identity also fails content as unproven.
The count remains an independent distinct-valid comparison.

| File | SHA-256 |
| --- | --- |
| test/e2e/scenarios/core_health.go | 2d391ec5b041b19799736fd203787a3b97c0684c8aecf7775f82aa9e75a8c31d |
| test/e2e/scenarios/core_dataflow.go | 0d1adc4f63a4add7497bdcdd38d0c85b28ecf5340a46d52155fe85814d12ca13 |
| test/e2e/scenarios/core_evidence_test.go | 658378d89250e48d8168387abfb9a1e4c1676155d7da06789f2ed110eb9fe3af |

The new test drives `executeValidateProcessing` through the production
ObservabilityClient's Docker command seam using a controlled local fake
executable, then calls `FinalizeChecks`. It includes a valid selected line
meeting the distinct count and one malformed, unsent, foreign or missing-ID
line. Before the fix all four cases failed the intended named-observation
assertion (exit 1), retained RED
`/private/tmp/semstreams-1222-core-k1-red.log` SHA-256
`5cda6e790c2196df52d1a2baf49199346e06b09a2ab8ed2bff49de255b13558f`.
After the fix the pure validator and bridge tests passed (exit 0), log
`/private/tmp/semstreams-1222-core-k1-green.log` SHA-256
`58bb24e579f75c9a2b402335a039e74b6e54c910d4d66560635510b711903c33`.
Focused health/UDP/validator/bridge tests passed under `go test -race` with
ephemeral localhost binding escalation (exit 0), log
`/private/tmp/semstreams-1222-core-k1-race.log` SHA-256
`bb8a95e9b077b735f39ac22bc2551a902664481ab5d9c03f03f9cd7678c1d8cf`.

Mutation: copied `core_dataflow.go` to `/private/tmp/semstreams-1222-core-k1.go.bak`,
both SHA-256 `0d1adc4f63a4add7497bdcdd38d0c85b28ecf5340a46d52155fe85814d12ca13`.
Removing the unsent-sequence content-unproven issue made the bridge test fail
on a false passed content observation, exit 1, log
`/private/tmp/semstreams-1222-core-k1-mutant-red.log` SHA-256
`bea68b222f38dc859f5165574aec1bceec85a74e7594090c4d186be45b13656d`.
Restored by `cp`; source and backup SHA matched. Restored bridge test exit 0,
log `/private/tmp/semstreams-1222-core-k1-restored-green.log` SHA-256
`e712efffca456c81c0006d48689954b32d62e5bc66c2cd8aae8f67629c20860f`.
`git diff --check` on these three files exit 0. No Docker, integration,
full-package or assembled E2E run is claimed.
