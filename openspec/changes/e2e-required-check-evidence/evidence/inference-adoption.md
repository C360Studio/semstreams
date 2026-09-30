# #1222 inference and slow-consumer adoption checkpoint

Base: fe6e2cc03e16f5db47e293f55939548572f204cc. Accepted design SHA-256: a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c. Worktree: /Users/coby/.codex/worktrees/e2e-required-proof/semstreams. No commit or push.

## Changed surface and consumers

- TieredConfig and SlowConsumerAttributionConfig carry EvidenceRunID/EvidenceMemberID. cmd/e2e constructor wiring is owned separately by task_reporting; scenario Execute now declares before observations when identity is supplied.
- TieredScenario.CheckRequirements names components and graph-roundtrip identity for structural/statistical/semantic, structural zero-embedding and zero-clustering execution, statistical controlled-search fixture identity, and semantic semembed plus controlled-search identity. Unknown/empty variant returns no required catalog so the runner must refuse required selection.
- Live component inventory, graph roundtrip probe, structural Prometheus metric+component checks, real GraphQL search results, and semantic semembed comparison record named observations. Graph roundtrip compares the observed canary entity ID with the observed authority and trace-derived expected ID. Controlled search compares one actual query response with the exact fixture ID under the observed authority, not a substring or aggregate count. Existing fatal stages remain fatal. B0/B2 diagnostics untouched.
- Slow-consumer declares its eleven existing conditions individually. Each evaluated condition records expected/actual summaries; the first failed condition and earlier passes remain in the Result. A pre-observation error records the first required condition as failed; the others remain missing.
- New private tiered helper file and narrowly scoped tests consume CheckRequirement/CheckObservation. No production or graph API added. Current consumers are cmd/e2e's optional declaration bridge and Result finalizer/writer; no durable state or new query surface.

## Bounded conformance table

| Accepted ruling | Implementing site |
|---|---|
| Declare selected named set before observations | tiered.go:549; core_slow_consumer.go:78 |
| Required component membership from live inventory | validate_infra.go:20-79 |
| Graph roundtrip named identity from actual probe | tiered.go:422-464 |
| Structural zero execution, not stored communities | tiered_structural.go:53-79; existing validateTierMustNotRun |
| Controlled fixture in actual query response | validate_search.go:20-52; tiered_required_evidence.go:49-91 |
| Semantic semembed actual availability | validate_search.go:507-515 |
| Slow-consumer existing per-condition assertions | core_slow_consumer.go:209-259 |
| Preserve existing fatal checks and B0/B2 diagnostics | tiered.go stage loop unchanged; no B0/B2 edit |

## Local evidence

- Initial RED: focused go test failed compilation on missing controlledSearchObservation, slow-consumer identity fields and CheckRequirements. Raw output exists only in this agent's tool transcript; no retained log file.
- Frozen-source GREEN: `go test ./test/e2e/scenarios -run 'TestTieredControlledSearch|TestStructuralZeroRunRecordsNamedObservation|TestSlowConsumerIndividual|TestAssertSlowConsumerObservation|TestExecuteValidateZero' -count=1`; exit 0, 1.099s. Raw log `/private/tmp/semstreams-1222-inference-focused-green.log`, SHA-256 `e101c7dc53fc119563b93354cae4abaf6cb412ef4b09d1d4d9a5361d4777d941`.
- Frozen-source race GREEN: same command with `-race`; exit 0, 18.602s. Raw log `/private/tmp/semstreams-1222-inference-focused-race.log`, SHA-256 `b495f29b620b7507c46b12293921bb48d14edd4a9f663f485546e85839033143`.
- Exploratory compiled mutation: cp backup of tiered_required_evidence.go; baseline SHA-256 `78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423`. Replaced `hit.EntityID == expected` with `false`; focused test exited 1 because healthy exact-identity cases failed. This is **over-rejection of valid identity**, not the accepted omitted/weakened identity-assertion sensitivity control. Mutant output exists only in this agent's tool transcript; no retained raw log file. Source restored via cp; source and backup SHA-256 matched the baseline. Restored focused test exited 0; its output exists only in the tool transcript. The accepted negative-control mutation (wrong/empty identity incorrectly passes, or query/recording skipped) remains **UNVERIFIED** and is owed before completion.
- `git diff --check` on owned tracked scenario files passed; output is empty in the tool transcript. One auto-review rejected a broad text replacement from assertSlowConsumerObservation to EOF as too destructive; it made no edit. Subsequent exact patches applied after tail inspection.

Final source SHA-256:

```
453eddc4477d6384c95419f62cb24f43414d659b73d77c33dec4b4001cd2c1cf  tiered.go
fc8a5c603a5e765b4dcbe58e02f7043b7bd6ab68d7dbba5e7b5e3fd771db08b2  tiered_structural.go
991aac171c6b809682e07013fd68ae3719274a9a0cb9043152f96e0ec020a298  validate_infra.go
b270b6386cb2abe0f278c1036209808bebc4a67df07ea04de18afeee6dcd3b41  validate_search.go
edcd0efc22347a46fbe2c08c6330b400a3bb2b63541a04711bca4286e4e408ec  core_slow_consumer.go
78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423  tiered_required_evidence.go
a771c38e1192f04cc3c4f95393a47587a00cdfbd81fa9272221d50f85118ed1e  tiered_required_evidence_test.go
```

Limits: no Docker/assembled E2E, broad package gate, or full push gate run. The accepted missing/weakened identity mutation remains open. CLI wiring, independent implementation review, and host gate evidence remain with root/other owner. PBT/fuzz: no new parsing surface or numerical grammar; targeted deterministic identity and failure controls fit the stated invariant. External checkpoint is not an in-tree CI artifact; task/document gate claims should remain UNVERIFIED until root captures retained artifacts.
