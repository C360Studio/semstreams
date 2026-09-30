# #1222 inference adoption review correction

Base fe6e2cc03e16f5db47e293f55939548572f204cc; accepted design SHA-256 a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c. No commit, push, Docker or broad gate.

## Corrections

- I1 (`tiered.go`): semantic-fallback with supplied evidence IDs no longer calls DeclareChecks. Its catalog remains empty and the runner bridge must treat that known nonrequired selection as execution-only/unattested; task_reporting owns cmd/e2e and was notified. Any other supplied-ID variant still attempts declaration; unknown/empty required catalog refuses. `TestTieredFallbackHasNoRequiredCatalog` checks catalog/refusal; the actual resolver/constructor/lifecycle bridge test is task_reporting-owned and pending here.
- I2 (`tiered_required_evidence_test.go`): real HTTP fixture now decodes GraphQL request and returns hits only for POST semanticSearch with independent literal query `What documents mention forklift safety?` and limit 10. The foreign-authority hit test exercises `executeVerifySearchQuality` through `search.Executor` HTTP and named Result recording. Exact full ID remains required. A compiled suffix-acceptance mutant at the actual comparison passes foreign authority, and both helper and actual-stage negative assertions fail. This is the accepted wrong-identity sensitivity control; earlier forced-false exploratory mutant is superseded for the claim.
- I3 (`core_slow_consumer.go`): drop-available condition now checks map key presence. Observation evidence says expected `absent` and actual presence/value. Table test runs absent, present null and present false through the existing eleven-condition recorder and FinalizeChecks. Absent completes; null/false retain nine observations with the ninth failed, and finalization fails.
- I1/I3 source changes did not change production graph/runtime API, storage or query surface. Existing fatal tier stages and B0/B2 diagnostic paths remain unchanged.

## Retained focused evidence

Final source SHA-256:

```
8a48b4bcaf1e85f26a3d456f8ca6864f958473636f312ba8234248ea21edb3a8  tiered.go
fc8a5c603a5e765b4dcbe58e02f7043b7bd6ab68d7dbba5e7b5e3fd771db08b2  tiered_structural.go
991aac171c6b809682e07013fd68ae3719274a9a0cb9043152f96e0ec020a298  validate_infra.go
b270b6386cb2abe0f278c1036209808bebc4a67df07ea04de18afeee6dcd3b41  validate_search.go
2f349e4ae11d1521a4576bc2f4850f2f5811043dbe1115096d971d23c01eb8b9  core_slow_consumer.go
78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423  tiered_required_evidence.go
ceeb724ca7e00c2f0f66b9523e8cc5de3f7821407d35bd0cf0b821d3c89504d7  tiered_required_evidence_test.go
```

Commands and logs:

- `go test ./test/e2e/scenarios -run 'TestTieredControlledSearch|TestTieredFallbackHasNoRequiredCatalog|TestSlowConsumerDropAvailabilityRequiresAbsentKey|TestSlowConsumerIndividualConditionsRetainPartialFailure|TestAssertSlowConsumerObservation|TestStructuralZeroRunRecordsNamedObservation' -count=1` exited 0; `/private/tmp/semstreams-1222-inference-review-green.log` SHA-256 d06557d24df87948085d0c3f85c8d0ffe24a0b4928e1c7947f53dbea71408cc3.
- Same command with `-race` exited 0; `/private/tmp/semstreams-1222-inference-review-race.log` SHA-256 044f976b6f600372227192aa12834e8dc349070bfcf96b0a5735e6af3af36811.
- Final compiled mutant: backed up tiered_required_evidence.go with `cp`; source and backup SHA-256 78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423. Exact temporary replacement: `hit.EntityID == expected` -> `strings.HasSuffix(hit.EntityID, ".document.content.operations.doc-ops-001")`, mutant source SHA-256 6815d7ff83a86c8a85655a9bd1d1561d8b2aded70c761a4c6cb12f62345cfb9f. Test source remained SHA-256 ceeb724ca7e00c2f0f66b9523e8cc5de3f7821407d35bd0cf0b821d3c89504d7. `go test ./test/e2e/scenarios -run 'TestTieredControlledSearchRequiresExactFixtureIdentity|TestTieredControlledSearchRecordsActualQueryResponse' -count=1` exited 1 at `wrong_identity` and actual HTTP-stage `foreign_identity`, because foreign identity incorrectly passed; `/private/tmp/semstreams-1222-inference-review-mutant-final.log` SHA-256 824eb208a292f7c9a5d0520ee1ec3605b7af98f1fbc90dd504f4dce8bbc2428a.
- Restored from cp backup; source and backup SHA-256 matched 78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423. Same focused command exited 0; `/private/tmp/semstreams-1222-inference-review-restored-final.log` SHA-256 042cef319db3d846bc7aa20c861d7cd01fef49b0943266cf3a7a6b0185fa09b1.
- `git diff --check` on owned tracked scenario files passed; no output. The earlier broad-tail edit rejection did not recur; no workaround used.

PBT/fuzz remains inapplicable to these private finite comparisons: no new external parser or numeric grammar. The HTTP fixture is a controlled consumer-path check, not assembled ingestion/query proof. Actual fallback CLI bridge, independent re-review, Docker/assembled E2E and full gates remain outside this slice.
