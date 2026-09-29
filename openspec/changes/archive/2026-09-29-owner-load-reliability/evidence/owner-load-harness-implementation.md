# Owner-load harness implementation evidence

Historical round-one record. Independent implementation review found fixture-recovery and cancellation defects in
this source. The corrected exact-source results and current conformance pins are in
`owner-load-harness-review-correction.md`. The table and hashes below refer to round one only.

Date: 2026-09-29. Design accepted at `design.md` SHA-256
`20b701ec51d5e8c32b6084eba93eed9911e1ca75985a4a7896f74ec7c4dd10e7`.

The implementation is limited to the owner-load integration test and two untagged, NATS-free test files.
The seed helper retains one primary error while 32 workers and a 256-row queue stop admission and join. The
concurrent helper owns dispatcher, sampler, and churn in one lexical finalizer with a shared ten-second terminal
join budget; a missing result is classified after dispatcher completion. Consumer convergence gives every Info
call the same finite five-second context. The framework KV deadline and load/latency profile values are unchanged.

| Accepted requirement | Implementation / proof |
| --- | --- |
| First seed failure stops admission, retains error, joins workers | `owner_filter_load_helpers_test.go:61`, `owner_filter_load_integration_test.go:261`, queued proof at `owner_filter_load_helpers_unit_test.go:13` |
| Concurrent dispatcher/sampler/churn have one lexical finalizer and shared bound | `owner_filter_load_helpers_test.go:185`, terminal proof at `owner_filter_load_helpers_unit_test.go:252` |
| Results are selected and missing results classified after dispatcher completion | `owner_filter_load_helpers_test.go:312`, missing/healthy proof at `owner_filter_load_helpers_unit_test.go:151` |
| Failure is captured before harness cancellation with caller deadline and bounded stacks | `owner_filter_load_helpers_test.go:23`, active-owner proof at `owner_filter_load_helpers_unit_test.go:65` |
| Each consumer Info call receives its finite convergence context | `owner_filter_load_helpers_test.go:379`, `owner_filter_load_integration_test.go:374`, proof at `owner_filter_load_helpers_unit_test.go:172` |
| First failed worker shape stops later shapes | `owner_filter_load_integration_test.go:170` |
| Real workload uses these helpers and retains original profiles/deadline | `owner_filter_load_integration_test.go:261`, `:309`, `natsclient/kv.go:39` |

## Focused proof

Scaffolding compile red: `go test ./processor/graph-index -run '^TestOwnerLoadSeedQueuedFailureJoins$' -count=1`
exited 1 with `undefined: ownerLoadSeed`. This was **not** a behavior-level red and is not counted as mutation
evidence. The first completed seed implementation passed the focused `-race` test in 1.497s.

Round-one NATS-free command:

```text
go test -race ./processor/graph-index -run '^TestOwnerLoad(SeedQueuedFailureJoins|ConcurrentFailureJoinsActiveOwners|ConcurrentMissingResultAndHealthyOrder|ConvergeCancellationRetainsObservation|ConcurrentAdmissionCancellation|ConcurrentTerminalExpiryRetainsPrimary)$' -count=1
ok github.com/c360studio/semstreams/processor/graph-index 1.556s
exit 0
```

The six named tests use finite, controlled schedules; no randomized PBT generator is warranted for these
ownership exits. They cover queued seed error, active listing failure with sibling owners, healthy submission-order
samples, missing result, blocked admission cancellation, finite Info cancellation, and terminal expiry with
independent fixture recovery. They do not prove all scheduler interleavings or the historical #1421 cause.

## Join mutation

Source backup created by `cp processor/graph-index/owner_filter_load_helpers_test.go
/private/tmp/gh1421-owner-load-helpers-before-join-mutant.go`. Backup/source SHA-256 before mutation:
`fc8e63ac40b80d1368c32f9439eb769adc8ece16a43d8198126300fc21ff43ea`.

Valid mutant omitted only the sampler completion owner from the final `ownerLoadJoin` call and retained an explicit
no-op read of `samplerDone` to keep the source compiling. Reconstructed mutant bytes are at
`/private/tmp/gh1421-owner-load-helpers-omitted-join-mutant.go`, SHA-256
`5f75a4ed60f6f585788c894a3fb0fa91ddd7b6682bd422a4b9272e9f76444295`. The exact mutation diff is in
`owner-load-omitted-join.diff`. Initial deletion without the no-op was a compile-invalid candidate, discarded before
the behavioral test. The valid mutant command was:

```text
go test -race ./processor/graph-index -run '^TestOwnerLoadConcurrentFailureJoinsActiveOwners$' -count=1
--- FAIL: TestOwnerLoadConcurrentFailureJoinsActiveOwners (0.00s)
    owner_filter_load_helpers_unit_test.go:133: returned before sampler joined: list fixture=0 serial=0: designated listing failure
FAIL github.com/c360studio/semstreams/processor/graph-index 0.448s
exit 1
```

This round-one mutant reached the intended assertion, but that assertion aborted before releasing the held sampler.
Process exit did **not** prove fixture join. A corrected exact-source mutation and independent recovery receipt are
in `owner-load-harness-review-correction.md`. Restoration used `cp` from the backup,
not git reset/restore. Backup/restored source SHA-256 both matched
`fc8e63ac40b80d1368c32f9439eb769adc8ece16a43d8198126300fc21ff43ea`; the selected `-race` proof then
passed in 1.463s. Subsequent focused corrections changed helper source, but not the sampler finalizer or the
active-listing proof mechanism. The final focused command above passed on the corrected source.

## Native integration applicability

The **one** canonical CI-profile owner run used:

```text
GRAPH_INDEX_OWNER_FILTER_FULL=0 ./scripts/run-integration-tests.sh ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$'
```

It exited 0: Docker-backed tagged `-race` package test 5.839s; runner wall time 9.28s; all nine owner/forward/
concurrent submission-order distributions were recorded verbatim in `owner-load-harness-gate-log.txt`.
The predecessor's complete source snapshot/hash was not retained, so this is a limited historical observation,
**not** exact-source integration proof for the final three hashes below. A first unprivileged runner invocation
failed at `docker info` with Docker socket
permission denied, before tests; the authorized rerun above passed. The post-run corrections explicitly check
cancelled context before admission, attribute sampler errors to the failing fixture, and measure elapsed Info/final
listing operations. They do not change workload, production callbacks, budgets, or sampler join ownership. The
coordinator's required full push gate will compile and execute the final integration source; no second focused
native run was launched. The later result-collector extraction was semantics-preserving and passed focused `-race`
proofs and pinned revive. `go vet -tags=integration ./processor/graph-index` exited 0 on final source without
running tagged tests.

Round-one source SHA-256:

```text
5686655e0462a7fd92815b5c7ed38f8e930e1e8a14e33a88ee026d8131cdd9c4  processor/graph-index/owner_filter_load_integration_test.go
b8909c6e4ce913b9a5ca16f1b44bd83a634195f54ac44da97a6f7b126cb0afb8  processor/graph-index/owner_filter_load_helpers_test.go
065754807af5f9f131b03ac353ba79acb8cb4a5dadc21560d608d5fbae9a1f64  processor/graph-index/owner_filter_load_helpers_unit_test.go
```

`go tool revive -config revive.toml -formatter friendly ./processor/graph-index` and `git diff --check`
both exited 0. The original #1421 listing expiry/drain timeout remains unexplained; this change
claims bounded, owned and diagnosable harness failures, not a production repair or historical root cause.
