## MODIFIED Requirements

### Requirement: Predicate-forward measurements retain bounded phase evidence

The owner-load harness SHALL retain diagnostic records for the five initial predicate-forward listing attempts in
the default CI profile. Each admitted attempt SHALL identify construction entry and return, the interval between
successful construction return and delegated Stop entry, delegated Stop entry and return, and the caller-observed
operation return. Records SHALL retain observed errors, result count and nil-result status. Missing observations
SHALL remain distinguishable from completed phases. Construction failure without a returned watcher SHALL make
collection and Stop inapplicable. A watcher returned alongside a constructor error SHALL remain production-owned
for Stop and terminal cleanup; its constructor error SHALL remain visible.

The harness SHALL preserve earlier attempt records and the failing attempt through operation errors, result
validation failures, diagnostic failures and fatal assertion exits. Diagnostic failures SHALL remain separate from
the primary failure. Successful completion SHALL retain all five records.

Deadline snapshots SHALL identify the actual framework child deadline, callback and capture timestamps, observed
phase and operation-return marker before and after capture, and truncation. Classification SHALL distinguish capture
before, after or straddling the recorded return marker from unknown or incomplete evidence. Callback scheduling
SHALL NOT be represented as the deadline instant, and context completion or an absent return marker alone SHALL NOT
establish that a native operation was blocked.

Observation SHALL preserve the existing file-backed workload, five-second framework deadline, listing context and
filter, native Updates channel used by the production filtered watcher, synchronous production-owned Stop, typed
errors, nil partial results and assertions.
Operation duration SHALL include synchronous Stop and terminal drain and SHALL be recorded before diagnostic
joining or reporting. Diagnostic callbacks SHALL have lexical finalization and finite completion ownership; unresolved ownership SHALL stop further attempt admission without
replacing the primary failure. The interval after Stop return and before the operation-return marker SHALL remain
identifiable without being represented as an independent observation of Updates closure or a native goroutine join.

#### Scenario: An attempt fails before all repetitions complete

- **GIVEN** earlier predicate-forward attempts have completed
- **WHEN** the next attempt fails or its assertion exits the measurement scope
- **THEN** reporting retains the earlier records and the failing attempt
- **AND** a construction error without a returned watcher records collection and Stop as inapplicable
- **AND** absent timestamps do not imply instantaneous completion.

#### Scenario: Deadline capture runs after operation return

- **GIVEN** the framework child deadline has expired
- **WHEN** the callback captures a snapshot after the recorded operation-return marker
- **THEN** the snapshot is classified as after that marker
- **AND** timestamps and observed phases remain available
- **AND** it is not presented as evidence of the earlier blocked phase.

#### Scenario: Snapshot evidence is incomplete

- **WHEN** required timing observations are missing or the snapshot is truncated
- **THEN** the evidence reports its unknown or incomplete state
- **AND** incomplete evidence does not support an absence claim
- **AND** ordinary cancellation after a successful call does not trigger a deadline snapshot.

#### Scenario: Observation preserves listing behavior

- **WHEN** an observed attempt uses the production KV wrapper
- **THEN** collection and terminal cleanup consume the exact native Updates channel under production ownership
- **AND** the observer supplies no relay or independent drain
- **AND** production retains sole ownership of its once-only synchronous delegated Stop
- **AND** deadline failure remains a typed error with nil partial results
- **AND** complete public-call duration, including synchronous Stop and terminal drain, remains in existing measurements
  and assertions
- **AND** existing workload and latency limits remain unchanged.

#### Scenario: Diagnostic callback does not complete within its terminal budget

- **WHEN** lexical finalization cannot stop or observe completion of the callback within its finite budget
- **THEN** the harness reports unresolved diagnostic ownership and admits no further attempt
- **AND** it retains the original operation or validation failure
- **AND** callback completion, delegated Stop return and native producer completion remain distinct observations.
