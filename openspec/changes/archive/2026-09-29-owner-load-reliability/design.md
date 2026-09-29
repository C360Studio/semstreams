# Owner-load harness failure ownership

Status: design draft from semstreams-architect, 2026-09-29; awaiting independent review and acceptance.

This slice repairs demonstrated harness defects. The original #1421 five-second listing expiry and subsequent
fifteen-second drain timeout remain unexplained. The native experiment reproduced backpressure but observed prompt
framework failure and successful close; it does not support changing production listing or drain behavior.

## Scope and alternatives

| Option | Benefit | Limitation |
| --- | --- | --- |
| Diagnostics only | Smallest diff; improves recurrence evidence | Retains seed deadlock and success-only cleanup |
| Private helpers for this harness | Makes failure and completion testable | Limited integration-test extraction |
| General async testing framework | Could serve other packages | Unproven abstraction and expanded scope |

Choose ordinary private test-only helpers used by the existing harness and its focused proofs. Adopt lexical
ownership: install finalization before starting work, preserve the original failure, and observe completion.
No general lifecycle registry, exported API or production primitive is needed.

Preserve workload sizes, worker counts, queue capacity, seed concurrency, repetition counts, percentile budgets
and the framework's five-second deadline. No listing retries. The exact-set gap remains tracked in #1293.

## Implementation boundary

Source ownership is limited to:

- `processor/graph-index/owner_filter_load_integration_test.go`
- `processor/graph-index/owner_filter_load_helpers_test.go`
- `processor/graph-index/owner_filter_load_helpers_unit_test.go`

The new files are untagged NATS-free test helpers and proofs. Move only shared test-only types/calculations needed
to compile them. Integration calls use these same helpers with operations directly invoking existing stores and
streams. No production dispatcher, SDK, natsclient, runner or generic policy change is authorized.

Documentation is limited to this change's artifacts and graph-index delta, plus exact cleanup-guard evidence from
touched sites if needed. Do not widen cleanup baselines or resolve unrelated census debt.

## Required behavior

### Seed failure

Replace the undrained error channel with one retained first failure. Keep 32 workers and the 256-row queue.
Producer and workers share a child of the supplied context; queue submission observes cancellation. A worker
records its error, cancels the phase and returns. Siblings stop taking further work and finish admitted operations.
The producer alone closes input. Return after observed worker completion, or add an explicit cleanup-bound failure.
Error publication must never depend on a channel drained only after joining. Callers assert after the helper
returns; worker callbacks return errors and never call Fatal or FailNow.

### Concurrent phase

Extract an error-returning helper with a lexical finalizer installed before dispatcher, sampler or churn starts.
All use children of the phase context. Churn no longer outlives the phase through the longer outer context.
The first listing, sampler, churn, submission or result-validation failure retains its error and captures evidence,
stops admission and cancels work, joins all three owners, then returns with any cleanup error joined rather than
replacing the original. Already admitted operations remain owned through cancellation.

The sole submitter closes each dispatcher input lane exactly once only after its submission loop has finished or
ceased. Closing never races Submit, and this private instance is never reused or restarted. The existing dispatcher
handles closed input; cancellation remains the failure escape. Result
collection selects on results, first failure, cancellation and dispatcher completion; on completion, drain already
published results before declaring one missing. Remove unconditional expected-count receives. Preserve submission
order latency records and all existing success assertions. Stop sampling after finite listing/churn completes.
After a worker-shape subtest fails, the outer harness stops admitting later shapes.

Use one shared ten-second terminal join budget, immediately bounded after detaching from the supplied parent.
A terminal deadline reports unresolved ownership, never completion; do not admit later phases after expiry.

### Consumer convergence

Replace require.Eventually with synchronous context-aware polling. Each fixture retains its five-second window
and twenty-millisecond interval. Every Info call receives that finite convergence context. Retain last count,
last Info error and attempt count; expiry reports these with the context cause. Preserve allowance for transient
Info errors within that window; this adds no listing retry. Derive the existing fifteen-minute harness context
from t.Context without changing its duration.

### Failure evidence

Capture one primary failure before harness cancellation/cleanup: phase, fixture/bucket, applicable filter,
operation identity, elapsed operation time, actual caller-context deadline/cause, original wrapped error preserving
errors.Is, and bounded local goroutine stacks with an explicit truncation marker. The framework's internally derived
KV deadline is not exposed; label the caller deadline accurately and report the configured default separately.
No added operation timer/assertion, network probe, file service or monitoring goroutine. Publish through existing
test logging; cancellation-induced secondary errors never overwrite the primary failure. Include measured listings
and final convergence failures as well as seed/concurrent failures. Expected negative probes stay expected outcomes.

## Proof and validation

The proofs invoke the same helpers as the integration harness through controlled operation callbacks.

1. Seed failure under queued work: more rows than old queue/error capacities; gate active workers, release one
   designated error, observe cancellation and every completion, and preserve the error.
2. Active listing failure: establish sibling listing, sampler and churn activity before releasing a designated
   listing error. Assert capture precedes cancellation, siblings observe cancellation, and all completion signals
   are observed before return.
3. Early exit and missing result: exercise admission cancellation and completed dispatcher with a missing result;
   require an immediate classified error rather than an outer timeout.
4. Bounded convergence: a controlled Info observes the supplied deadline and blocks until cancellation. Verify
   callback completion and retained last error/count. Parent cancellation avoids a five-second test wait.
5. Healthy path: unchanged row/job counts, completion, and submission-order measurement.
6. Terminal expiry: a controlled callback observes cancellation but waits at an independently releasable gate.
   A short private proof budget must return the primary sentinel joined with a terminal-bound error, name the
   unresolved owner and prevent successor admission. Register fixture recovery before starting the helper; recovery
   releases and joins that callback. Read worker-owned state only after its done signal. This exercises the same
   helper as integration without waiting ten seconds or pretending unresolved work has joined.

Use explicit gates, not sleeps. PBT decision: named examples suffice for these finite lifecycle obligations;
controlled schedules exercise each required exit, with externally supplied errors and completion gates as oracles.
They do not prove every scheduler interleaving or the historical native failure.

Mutation criteria apply to owned shutdown work. Temporarily omit one owner's completion join in the shared helper
while the active-listing proof deliberately holds that owner after it observes cancellation. The proof must detect
return before that owner completes through an explicit assertion, not the package alarm. The fixture registers
independent release-and-join recovery before starting work. Keep original-error and capture-before-cancel checks.
Do not use a first-error cancellation bypass that the lexical finalizer can immediately rescue. Preserve via cp,
retain diff/commands/source hashes, restore and verify checksum.

Run focused NATS-free proofs under -race, then one ordinary CI-profile owner-load run through the canonical runner.
Complete required push gates and independent implementation review. No full workload profile or stress repetitions
without a new discriminating question.

## Temporary native diagnostic

After exact-source correction and review, preserve the source as evidence/native-kv-lifecycle-diagnostic.go.txt,
with checksum, run identities and copy-back reproduction instructions. Remove its compiled natsclient fixture only
after its writer releases it. The experiment remains reproducible evidence rather than permanent skipped coverage.

## Proposed graph-index spec delta

Add this under specs/graph-index/spec.md only after review and acceptance:

### Requirement: Owner-load failures retain evidence and release harness work

The owner-load harness SHALL retain its original failure and capture local diagnostic evidence before initiating
harness cancellation or cleanup. Evidence SHALL identify phase, operation, applicable filter, elapsed time, caller
deadline and original error.

Seed and concurrent-phase work SHALL have lexical cancellation and completion ownership. The harness SHALL stop
further admission after observing a failure, cancel remaining work and observe owned completion within a finite
terminal budget. Expiry SHALL report unresolved ownership and prevent subsequent phases.

Consumer-baseline polling SHALL pass its finite convergence context to each Info operation and retain the last
observation and error. These requirements SHALL NOT relax the framework KV deadline, introduce listing retries,
or change owner-load workload and latency budgets.

#### Scenario: Seed failure while work is queued

- **GIVEN** seed workers are active and additional rows are queued
- **WHEN** a seed operation fails
- **THEN** the harness retains that error and stops further admission
- **AND** error publication cannot depend on draining an error channel after joining
- **AND** it observes worker completion or reports unresolved ownership.

#### Scenario: Failure during concurrent owner listing

- **GIVEN** listing, sampling and churn work are active
- **WHEN** an operation or result validation fails
- **THEN** primary failure evidence is captured before initiating cancellation
- **AND** owned work is canceled and its completion observed
- **AND** cleanup errors do not replace the primary failure.

#### Scenario: Consumer-baseline observation blocks

- **GIVEN** the harness is waiting for temporary consumers to return to baseline
- **WHEN** its convergence context ends
- **THEN** Info receives that cancellation
- **AND** failure evidence retains last count, error and attempt count.

## Claim limits and remaining work

This patch can claim bounded, owned and diagnosable demonstrated harness failures. It cannot claim repair of the
original listing expiry or drain timeout. Closing #1421 on this narrower outcome requires an explicit owner
disposition of the unresolved historical cause. No new ruling is needed to design the authorized test repair.

After diagnostic review: obtain design review/acceptance; implement helpers and deterministic proofs/mutation;
retain the diagnostic as an artifact; run focused and required gates; obtain implementation review; reconcile
cleanup guard, specs/tasks and historical-cause disposition before archive/merge. No production semantics delta.
