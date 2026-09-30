# Fallback authority and main reconciliation evidence

Commit: `b9af725ec13307eeb4bcec528ac980bdf8d17adf` (merge parents
`97f060c4bef88f3d75af70585b073cacb1a9d085` and
`1b1accf4ea4ea878c26236b5a9e6cb83d2d89d7a`). This note transcribes the
tool output captured in the recovery chat. It is an excerpt, not a saved raw log.

Before the fallback fix, this command exited 1:

```text
go test ./test/e2e/scenarios -run 'TestTieredFallback(ReadsStatisticalDeploymentAuthority|GraphProbeUsesStatisticalDeploymentAuthority)$' -count=1 -timeout=30s
--- FAIL: TestTieredFallbackReadsStatisticalDeploymentAuthority (0.02s)
panic: e2e config: no deployment authority registered for tier variant "semantic-fallback" [recovered, repanicked]
... config.TierAuthorityStem ... test/e2e/config/tier_authority.go:58
... config.EffectiveTierAuthority ... test/e2e/config/tier_authority.go:161
... (*TieredScenario).Execute ... test/e2e/scenarios/tiered.go:619
FAIL github.com/c360studio/semstreams/test/e2e/scenarios 0.437s
```

After mapping the fallback behavior selector to the statistical Compose
deployment for authority reads, the focused command exited 0. The four-package
race command initially exposed an existing controlled-search fixture that
answered only one of eight queries; main's newly merged search-quality gate
correctly failed the other seven. The fixture was completed with deterministic
answers for all eight default queries. The final command exited 0:

```text
go test -race ./cmd/e2e ./test/e2e/results ./test/e2e/scenarios ./test/e2e/config -count=1 -timeout=90s
ok github.com/c360studio/semstreams/cmd/e2e 3.039s
ok github.com/c360studio/semstreams/test/e2e/results 2.182s
ok github.com/c360studio/semstreams/test/e2e/scenarios 5.508s
ok github.com/c360studio/semstreams/test/e2e/config 1.736s
```

Committed source blob IDs: `tiered.go` `c712494603469cb7bafd9a09a65b76f91db79e4d`,
`tiered_required_evidence_test.go` `996be4238bc0fb476d39c934f1f0c50337a043a7`,
`validate_search.go` `11c30d731e233427c3e724f3de3aabf2d6bd0b8f`.

Scope: in-process NATS and HTTP fixtures exercised actual `Execute` authority
resolution, a mismatched deployment, and the graph probe's authority read.
No Docker E2E or full push gate was run for this slice.
