# Owner-load harness review correction

Date: 2026-09-29. This supersedes the source and mutation-readiness claims in
`owner-load-harness-implementation.md`; the earlier native run remains a historical predecessor observation.
The final source hashes below include a subsequent bounded convergence correction. No second native integration run was made; the coordinator's
full push gate remains the exact-source native proof.

| Accepted requirement / review correction | Current source and proof |
| --- | --- |
| Seed error/cancellation stops admission, retains primary, joins workers, rejects cancellation at completion | `processor/graph-index/owner_filter_load_helpers_test.go:61`; queued and completion proofs in `owner_filter_load_helpers_unit_test.go:14` and `:71` |
| Concurrent dispatcher/sampler/churn finalizer has one shared terminal bound; actual child context supplies fault cause | `owner_filter_load_helpers_test.go:186`, `:23`; active/terminal proofs in `owner_filter_load_helpers_unit_test.go:104` and `:381` |
| Original fixture-name/serial dispatcher hash keys and workload stay fixed | `owner_filter_load_integration_test.go:309`, `:318`; `owner_filter_load_helpers_test.go:203` |
| Dispatcher completion drains buffered results and classifies missing results; successful submissions retain order | `owner_filter_load_helpers_test.go:318`; healthy/missing proof in `owner_filter_load_helpers_unit_test.go:229` |
| All fixture gates have pre-start, idempotent release plus finite helper/callback join on assertion failure | `owner_filter_load_helpers_unit_test.go:28`, `:164`, `:423`; exact-source mutation receipt below |
| Finite consumer convergence uses actual polling context for Info and failure snapshot | `owner_filter_load_helpers_test.go:385`, `owner_filter_load_integration_test.go:375`, `:385`; proof in `owner_filter_load_helpers_unit_test.go:291` |
| Failed worker shape prevents later shapes; real NATS calls use the same helpers and unchanged profile/deadline | `owner_filter_load_integration_test.go:170`, `:261`, `:309`; predecessor native log plus pending full push gate |

Focused final-source command:

```text
go test -race ./processor/graph-index -run '^TestOwnerLoad(SeedQueuedFailureJoins|SeedCancellationAtCompletion|ConcurrentFailureJoinsActiveOwners|ConcurrentMissingResultAndHealthyOrder|ConcurrentCancellationAtCompletion|ConvergeCancellationRetainsObservation|ConcurrentAdmissionCancellation|ConcurrentTerminalExpiryRetainsPrimary)$' -count=1
ok  github.com/c360studio/semstreams/processor/graph-index 1.503s
exit 0
```

The seed cancellation-at-completion proof cancels the real parent context within the final Put callback. The
helper's common post-select completion check must then reject success whether worker completion or cancellation
wins the select. The concurrent listing/churn callback schedules retain `context.Canceled` and fault evidence;
they may take the earlier collection cancellation arm and do not independently establish that the final churn-done
success branch was selected. This is a proof limit, not a historical #1421 finding. The final success branch also
checks the phase context explicitly. The polling proof observes the finite child context and its cause before the
poll owner returns.

Join mutation on the immediate predecessor to the convergence-only correction:

```text
cp processor/graph-index/owner_filter_load_helpers_test.go /private/tmp/gh1421-owner-load-helpers-final-join-backup.go
backup/source SHA-256 acd951f9cc7de045120a40a34d3b9ba142ee14d92142185f2c6b6291ca139995
```

The valid mutant removes only the sampler completion owner from `ownerLoadJoin`, retaining a no-op read of its
channel so it compiles. Its SHA-256 is `55d500b032ac76db9bcb095b13029f0b6c2cc33c2f1c47e9d2f1ba6532cf768c`;
the exact diff is `owner-load-final-omitted-join.diff` and reconstructed bytes are at
`/private/tmp/gh1421-owner-load-helpers-final-omitted-join-mutant.go`.

```text
go test -race -v ./processor/graph-index -run '^TestOwnerLoadConcurrentFailureJoinsActiveOwners$' -count=1
exit 1 (expected assertion, compiled mutant)
=== RUN   TestOwnerLoadConcurrentFailureJoinsActiveOwners
    owner_filter_load_helpers_unit_test.go:205: returned before sampler joined: list fixture=0 serial=0: designated listing failure
    owner_filter_load_helpers_unit_test.go:176: fixture recovery joined helper and active sampler
--- FAIL: TestOwnerLoadConcurrentFailureJoinsActiveOwners (0.00s)
FAIL
FAIL github.com/c360studio/semstreams/processor/graph-index 0.460s
```

The cleanup log follows the intended assertion failure, demonstrating that test-owned release and join still ran.
The source was restored with `cp /private/tmp/gh1421-owner-load-helpers-final-join-backup.go
processor/graph-index/owner_filter_load_helpers_test.go`; restored/backup SHA-256 both
`acd951f9cc7de045120a40a34d3b9ba142ee14d92142185f2c6b6291ca139995`.
The same selected command then passed with `fixture recovery joined helper and active sampler` in 1.474s.

Round-two source SHA-256 before the convergence-only correction:

```text
6e6314b0dee72fa66dd5c3891c599dcc49bcfc82f7ad106c1290a2dfacb474bc  processor/graph-index/owner_filter_load_integration_test.go
acd951f9cc7de045120a40a34d3b9ba142ee14d92142185f2c6b6291ca139995  processor/graph-index/owner_filter_load_helpers_test.go
52b71c7df660bc00687bebaf6b5dcc729aa3f23e189cf0abca28e1076d55c9e9  processor/graph-index/owner_filter_load_helpers_unit_test.go
```

`go tool revive -config revive.toml -formatter friendly ./processor/graph-index`,
`go vet -tags=integration ./processor/graph-index`, and `git diff --check` all exited 0 on this source.
The original listing expiry and drain timeout remain unexplained. This correction repairs harness ownership and
failure evidence only; exact-source native integration and the independent re-review remain open gates.

## Convergence-at-expiry correction on final source

The prior polling helper accepted a baseline count even when its finite context expired during the Info callback.
The new `TestOwnerLoadConvergeRejectsExpiredBaseline` cancels the real parent inside Info, returns the baseline
count with nil Info error, and requires a cancellation result with attempts=1, last_count=2, last_error=nil and the
poll context's canceled cause. Against the prior helper it failed as intended:

```text
go test -race ./processor/graph-index -run '^TestOwnerLoadConvergeRejectsExpiredBaseline$' -count=1
--- FAIL: TestOwnerLoadConvergeRejectsExpiredBaseline (0.00s)
    owner_filter_load_helpers_unit_test.go:351:
        Error Trace: /Users/coby/.codex/worktrees/gh1421-owner-load/semstreams/processor/graph-index/owner_filter_load_helpers_unit_test.go:351
        Error:      Expected error with "context canceled" in chain but got nil.
        Test:       TestOwnerLoadConvergeRejectsExpiredBaseline
FAIL
FAIL github.com/c360studio/semstreams/processor/graph-index 0.498s
exit 1
```

The minimal correction requires `ctx.Err()==nil` before convergence success. It did not touch the finalizer or
the active-owner proof, so the join mutation's assertion and recovery mechanism remain applicable; the mutation
source hash above is not claimed to equal this final helper hash. Final focused result:

```text
go test -race ./processor/graph-index -run '^TestOwnerLoad(SeedQueuedFailureJoins|SeedCancellationAtCompletion|ConcurrentFailureJoinsActiveOwners|ConcurrentMissingResultAndHealthyOrder|ConcurrentCancellationAtCompletion|ConvergeCancellationRetainsObservation|ConvergeRejectsExpiredBaseline|ConcurrentAdmissionCancellation|ConcurrentTerminalExpiryRetainsPrimary)$' -count=1
ok  github.com/c360studio/semstreams/processor/graph-index 1.520s
exit 0
```

Post-expiry-guard SHA-256 before the poll-loop admission correction:

```text
6e6314b0dee72fa66dd5c3891c599dcc49bcfc82f7ad106c1290a2dfacb474bc  processor/graph-index/owner_filter_load_integration_test.go
3e44e5147472b989c8e8228d9a2920d4e143521d83691868c1bfcdcd241a27d6  processor/graph-index/owner_filter_load_helpers_test.go
46eedba50b155dd06c731a24e1c4ea2dfe40bcb17f2a66366bc4225d01ab2caa  processor/graph-index/owner_filter_load_helpers_unit_test.go
```

Pinned package revive, integration-tag vet and `git diff --check` exited 0 on that source. Exact-source
native integration and independent re-review remain the coordinator's gates.

## Poll-loop cancellation admission correction

Review found that if the ticker and cancellation were both ready after Info returned, select could choose the
ticker and call Info again on an expired context. The same finite convergence context is now checked before each
Info call and immediately after each return. Both exits use one failure function, retaining the last count, Info
error, attempt count and context cause. The existing controlled single-entry polling proofs remain green; the
baseline-at-expiry behavior red above remains the intended pre-fix observation. No timer abstraction or listing
retry was added.

```text
go test -race ./processor/graph-index -run '^TestOwnerLoad(SeedQueuedFailureJoins|SeedCancellationAtCompletion|ConcurrentFailureJoinsActiveOwners|ConcurrentMissingResultAndHealthyOrder|ConcurrentCancellationAtCompletion|ConvergeCancellationRetainsObservation|ConvergeRejectsExpiredBaseline|ConcurrentAdmissionCancellation|ConcurrentTerminalExpiryRetainsPrimary)$' -count=1
ok  github.com/c360studio/semstreams/processor/graph-index 1.511s
exit 0
```

Final current SHA-256:

```text
6e6314b0dee72fa66dd5c3891c599dcc49bcfc82f7ad106c1290a2dfacb474bc  processor/graph-index/owner_filter_load_integration_test.go
bf7de818aac434617315e1b7dad07047591b9a2635e944d650c816b5e78b2c67  processor/graph-index/owner_filter_load_helpers_test.go
46eedba50b155dd06c731a24e1c4ea2dfe40bcb17f2a66366bc4225d01ab2caa  processor/graph-index/owner_filter_load_helpers_unit_test.go
```

Pinned package revive, sequential `go vet -tags=integration ./processor/graph-index`, and `git diff --check`
exited 0. A concurrent vet attempt hit a local go-build cache permission error while the race test built;
sequential rerun succeeded. The join finalizer and active-owner proof did not change after the mutation receipt.
