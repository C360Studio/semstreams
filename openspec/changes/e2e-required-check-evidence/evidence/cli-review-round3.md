# #1222 CLI C5/C6 and fallback bridge bounded review

Mode: read-only implementation re-review of C5 initialized declarations, C6 explicit unsuccessful Execute outcome, and I1 tiered fallback CLI bridge. Worktree `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`; base `fe6e2cc03e16f5db47e293f55939548572f204cc`. Task shell/composite/report API completeness, assembled Docker and full gates are outside this verdict. No source edit or reviewer-run test.

## Disposition

**C5 and C6 corrected. CHANGES REQUESTED for I1.** The current fallback bridge classifies `semantic-fallback` as execution-only/unattested, but the real scenario Execute path panics before it can complete that legacy execution.

### HIGH test/e2e/config/tier_authority.go:49 — fallback execution panics on unregistered authority variant

Mechanism: `resolveScenarioSelection` preserves `semantic-fallback` as a legacy tiered member (`cmd/e2e/selection.go:36-42`), `createScenario` places that exact string in `TieredConfig.Variant` (`main.go:450-473`), and `scenarioChecks` correctly treats its empty catalog as unattested (`runner.go:22-32`). `TieredScenario.Execute` skips DeclareChecks for fallback, then passes `variant == "semantic-fallback"` into `config.EffectiveTierAuthority` (`tiered.go:565-583`). That calls `TierAuthorityStem`, whose table contains only structural/statistical/semantic and explicitly panics on unknown variants (`tier_authority.go:25-53`). The real `e2e:semantic:fallback` Task selects this variant (`taskfiles/e2e/semantic.yml:129-131`). The scenario thus cannot finish as an execution-only legacy run; the process panics. `TestTieredFallbackResolverConstructorDeclarationBridge` (`runner_test.go:68-93`) stops after resolver/constructor/catalog inspection and never calls Setup or Execute. The generic `legacyEmptyCatalogScenario` lifecycle test (`runner_test.go:37-66`) is not the tiered fallback path.

Fix: reconcile the fallback scenario's deployment authority variant with the actually selected Compose configuration before its authority reads and graph-roundtrip probe, while retaining the `semantic-fallback` behavior selector and unattested disposition. Make the actual fallback lifecycle path fail cleanly or complete without panic, and add a bridge test that reaches it. The current Task uses the statistical Compose profile and `configs/statistical.json` (`docker/compose/tiered.yml:196-208`), despite its prose saying semantic config; confirm intended authority with the owner rather than guessing a different stem. The held tier-authority file belongs to #1404; coordinate ownership before a fix there.

Verification/refutation: this is a deterministic source trace through an explicit panic, not a reviewer-executed Docker reproduction. Checked the registered authority table and the real Task/Compose command to try to refute reachability. The current bridge unit test establishes selection and classification only; it cannot refute Execute panic. This path predates the current declaration fix, but the accepted I1 fallback compatibility claim remains unproved and the publicly selectable task is affected.

## Verified bounded corrections

C5: `runSelectedScenarios` declares exact run/member/CheckRequirements into each initial Result slot before Writer.WriteRun and before Setup (`runner.go:145-158,220-241`). The Setup callback test loads persisted JSON and checks selected identity and check membership (`runner_test.go:186-215`). Retained behavioral RED shows empty initial Scenarios; focused GREEN and final package run pass. Unknown required catalog still refuses before Setup (`runner.go:152-155`), consistent with the accepted empty-required rule.

C6: `executeScenario` adds an execution failure when an adopted scenario returns `Success=false` with passed observations and nil Go error (`runner.go:57-65`). FinalizeChecks sees the appended Error and fails; Writer retains the failed terminal child and nonzero outcome. The test drives the selected runner, Writer and loaded terminal artifact (`runner_test.go:217-236`). Retained RED shows exit 0 before correction; current package test passes. The legacy branch separately checks unsuccessful execution (`runner.go:88-94`). No finding on C5/C6 within this scope.

## Frozen evidence verified

Current relevant source SHA-256 matches `/private/tmp/semstreams-1222-task-reporting.md`:

| File | SHA-256 |
| --- | --- |
| `cmd/e2e/main.go` | `d9e61f4f9a271dccb1c69b271ab1cd7f91207da2ff28f5ca5809e819ac62a8f5` |
| `cmd/e2e/main_test.go` | `282db7bb6f5471c0dde379128b95c7936a82af0dad4446dcb204d148e3c8f04d` |
| `cmd/e2e/runner.go` | `9790a9a27052592a94854ac4e0d1b793b29af52bac930129034c9258e5762c5e` |
| `cmd/e2e/runner_test.go` | `6d113caeb35bb1e769eaed1fa44ea8412984dcda6808575e9373d1edde5a769c` |
| `cmd/e2e/selection.go` | `9c4c5f0d2ceb5017a1f89e24e938f5a0e63b74b33dfc0b8864359c788cfdffa7` |
| `cmd/e2e/selection_test.go` | `6f6943aa1d66d4ae22aaac36e1cfafd7c27bf724ea8567e477f64d17475f5917` |

Retained logs read and SHA-256 verified: C5 RED `/private/tmp/semstreams-1222-cli-c5-red.log` `7584add2e24d3f8088f80861a4448d3d135acc8c1aed01839b1eee7e2394182b`; C5 GREEN `/private/tmp/semstreams-1222-cli-c5-green.log` `17030588e0bbd00b5f2bb9167b4cfc08a474db734f5f9a34b352094da17482e7`; C6 RED `/private/tmp/semstreams-1222-cli-execution-false-red.log` `e63749c8c11dff2987709352c33bd664806672645f35114047f4ffb48d488f90`; focused GREEN `/private/tmp/semstreams-1222-report-provenance-green.log` `6c3fd38b2d6ad2ecb78e0af13d8fc5196b50536f219c54493f7473da52c80ea5`; final `go test ./cmd/e2e ./test/e2e/results -count=1` `/private/tmp/semstreams-1222-reporting-final-go-test.log` `33db6a95a44a886f14714d536d381003501082b1cae95bab547a073b12865be7` exited 0. Green logs are package summaries rather than per-test traces; exact commands and earlier RED are recorded in the author checkpoint. No assembled fallback proof is available.
