# Owner-load implementation delta map

base: 0d4d6173a0f3c7d72e4b23ae42b336b175ff1b1e

This map records the reviewed implementation snapshot below. The base is the comparison commit; the exact
working source hashes identify the new pins. It is not a replacement inventory or a new completeness verdict.

The accepted inventory.md remains byte-for-byte historical evidence at e811399e with SHA-256
`d0872671356ee5505087160054d890384c13ceb19d4364d7fc2745a3b01fd3a1`. Its 164/164 verification applies to that
pre-change source, not the repaired harness. The implementation review covers this deliberately changed surface.
No other production or SDK surface from that inventory changes. Historical cause remains unresolved.

## Reviewed snapshot

`6e6314b0dee72fa66dd5c3891c599dcc49bcfc82f7ad106c1290a2dfacb474bc` — processor/graph-index/owner_filter_load_integration_test.go

`bf7de818aac434617315e1b7dad07047591b9a2635e944d650c816b5e78b2c67` — processor/graph-index/owner_filter_load_helpers_test.go

`46eedba50b155dd06c731a24e1c4ea2dfe40bcb17f2a66366bc4225d01ab2caa` — processor/graph-index/owner_filter_load_helpers_unit_test.go

## Harness wiring

Seed admission now cancels on the first error and owns worker completion. Concurrent work uses the same private
helpers as the deterministic proofs. Original fixture-name/serial keys, workload and latency settings remain.
Consumer convergence owns one finite context; actual operation context and original errors feed local diagnostics.
Later worker shapes are not admitted after a failed subtest.

- `processor/graph-index/owner_filter_load_integration_test.go:152` — `ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)`
- `processor/graph-index/owner_filter_load_integration_test.go:170` — `if !t.Run(fmt.Sprintf("workers-%d", workers), func(t *testing.T) {`
- `processor/graph-index/owner_filter_load_integration_test.go:261` — `seedErr := ownerLoadSeed(ctx, rows, 32, 256, ownerLoadTerminalBudget,`
- `processor/graph-index/owner_filter_load_integration_test.go:309` — `outcome, phaseErr := ownerLoadConcurrent(ctx, ownerLoadConcurrentOptions{`
- `processor/graph-index/owner_filter_load_integration_test.go:318` — `Key: func(job ownerLoadJob) string { return fmt.Sprintf("%s-%d", fixtures[job.Fixture].name, job.Serial) },`
- `processor/graph-index/owner_filter_load_integration_test.go:564` — `func logOwnerLoadFault(t *testing.T, fault ownerLoadFault) {`

## Private lifecycle and diagnostics

- `processor/graph-index/owner_filter_load_helpers_test.go:23` — `func ownerLoadFaultAt(ctx context.Context, phase, fixture, bucket, filter, operation string, elapsed time.Duration, err error) ownerLoadFault {`
- `processor/graph-index/owner_filter_load_helpers_test.go:39` — `func ownerLoadJoin(parent context.Context, budget time.Duration, owners ...ownerLoadJoinOwner) error {`
- `processor/graph-index/owner_filter_load_helpers_test.go:61` — `func ownerLoadSeed[T any](parent context.Context, rows []T, workers, queueSize int, budget time.Duration,`
- `processor/graph-index/owner_filter_load_helpers_test.go:178` — `func ownerLoadQueueDepth[T any](dispatcher *keyedDispatcher[T]) int {`
- `processor/graph-index/owner_filter_load_helpers_test.go:186` — `func ownerLoadConcurrent(parent context.Context, options ownerLoadConcurrentOptions) (out ownerLoadConcurrentOutcome, retErr error) {`
- `processor/graph-index/owner_filter_load_helpers_test.go:318` — `func ownerLoadCollectResults(ctx context.Context, options ownerLoadConcurrentOptions,`
- `processor/graph-index/owner_filter_load_helpers_test.go:385` — `func ownerLoadConverge(parent context.Context, window, interval time.Duration, baseline int,`

## Controlled proofs

- `processor/graph-index/owner_filter_load_helpers_unit_test.go:14` — `func TestOwnerLoadSeedQueuedFailureJoins(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:71` — `func TestOwnerLoadSeedCancellationAtCompletion(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:104` — `func TestOwnerLoadConcurrentFailureJoinsActiveOwners(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:229` — `func TestOwnerLoadConcurrentMissingResultAndHealthyOrder(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:250` — `func TestOwnerLoadConcurrentCancellationAtCompletion(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:291` — `func TestOwnerLoadConvergeCancellationRetainsObservation(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:341` — `func TestOwnerLoadConvergeRejectsExpiredBaseline(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:358` — `func TestOwnerLoadConcurrentAdmissionCancellation(t *testing.T) {`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go:398` — `func TestOwnerLoadConcurrentTerminalExpiryRetainsPrimary(t *testing.T) {`

The concurrent cancellation proof can use an earlier collection arm; it does not claim deterministic activation
of the final churn-completion branch. The explicit success check is also inspected in review. The selected
omitted-join mutation reaches an early-return assertion, then independent fixture recovery joins the held sampler.
Later convergence-only changes leave that mutation mechanism unchanged, with exact predecessor attribution.
