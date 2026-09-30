# #1222 core K1 correction review

Mode: bounded implementation re-review; read-only source. Worktree
`/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`, base
`fe6e2cc03e16f5db47e293f55939548572f204cc`. This supplements the original core review;
unchanged conformance findings are not repeated. Accepted design SHA-256:
`a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`.

## Disposition

**K1 HIGH: resolved.** At `test/e2e/scenarios/core_dataflow.go:370`, malformed selected
JSON now records identity as unproven. At lines 376 and 382, missing/foreign identity
and an unsent sequence record content as unproven. Each branch supplies a nonempty
reason. The production bridge at lines 324–328 therefore records failed named
observations and returns an error instead of deriving a sibling pass from an empty
issue slice. This satisfies the accepted requirement that an expected comparison
must actually occur before its observation passes.

Attempted refutation: sufficient good output must not mask a bad selected record,
and a failed sibling must not unnecessarily fail an independently satisfied count.
The new test at `core_evidence_test.go:121` supplies one valid output plus each of
malformed, unsent, foreign, and missing-sequence records. It constructs the real
scenario and ObservabilityClient, controls the Docker executable boundary, calls
`executeValidateProcessing`, then `FinalizeChecks`. Assertions at lines 155–171
require failure overall, count passed, and both content/identity failed with reasons.
These are observable required-check outcomes, not just private issue-slice assertions.
The count still measures distinct valid successfully sent sequences; valid records
and the existing no-health-fallback path are unchanged. No new actionable finding.

## Frozen source

Hashes matched the author checkpoint before and after review:

| File under `test/e2e/scenarios/` | SHA-256 |
| --- | --- |
| core_health.go | 2d391ec5b041b19799736fd203787a3b97c0684c8aecf7775f82aa9e75a8c31d |
| core_dataflow.go | 0d1adc4f63a4add7497bdcdd38d0c85b28ecf5340a46d52155fe85814d12ca13 |
| core_evidence_test.go | 658378d89250e48d8168387abfb9a1e4c1676155d7da06789f2ed110eb9fe3af |

Checkpoint `/private/tmp/semstreams-1222-core-k1.md` SHA-256:
`febe016f7f365e98951395db2bfa3550bc050f8bf7cc2625d1c1bb6c725a1875`.
The in-tree `evidence/core-k1-developer.md` records the same source and evidence claims.

## Retained evidence and limits

All five local raw logs were read and their hashes verified. Names below have prefix
`/private/tmp/semstreams-1222-core-k1-` and suffix `.log`:

| Log | SHA-256 |
| --- | --- |
| red | 5cda6e790c2196df52d1a2baf49199346e06b09a2ab8ed2bff49de255b13558f |
| green | 58bb24e579f75c9a2b402335a039e74b6e54c910d4d66560635510b711903c33 |
| race | bb8a95e9b077b735f39ac22bc2551a902664481ab5d9c03f03f9cd7678c1d8cf |
| mutant-red | bea68b222f38dc859f5165574aec1bceec85a74e7594090c4d186be45b13656d |
| restored-green | e712efffca456c81c0006d48689954b32d62e5bc66c2cd8aae8f67629c20860f |

Initial RED reaches the intended named-observation assertion in all four cases.
GREEN passes the validator and bridge cases. The race log passes focused
health/UDP/validator/bridge tests; `-race` invocation is recorded in the author
checkpoint, not embedded in raw output. Removing the unsent-sequence content issue
produces a compiled behavioral RED on false passed content: **DETECTED** for this
bounded mutant. Restored GREEN passes all four bridge cases. The retained `cp`
backup `/private/tmp/semstreams-1222-core-k1.go.bak` and current dataflow source both
hash to the frozen dataflow SHA above. This supports restoration, not a broader
mutation-coverage claim. Fixed examples are appropriate for these finite early-exit
branches; the correction adds no exported parser or new generated-input domain.

No reviewer test/gate runs or source edits. Fake Docker proves the observation bridge,
not an assembled container pipeline. Dependencies were not frozen by this three-file
review. Full-package, integration, outer CLI/Task persistence and assembled E2E proof
remain outside this verdict. Raw logs are currently local artifacts referenced by the
in-tree checkpoint; durable gate evidence must be retained before integration.

**APPROVE — bounded core K1 correction and reviewed core slice.** This is not whole
feature approval or merge readiness.
