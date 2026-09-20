# agentic-loop Delta

## ADDED Requirements

### Requirement: Loop-state authority has one port declaration

Agentic-loop SHALL select its loop-state bucket only from the normalized KV-write facts of its admitted output
named `loops`. The default SHALL remain AGENT_LOOPS. `DeclarePorts` and `NewComponent` SHALL share the existing
configuration derivation.

The loop-side exported `Config.LoopsBucket`, JSON setting `loops_bucket`, default and generated-schema entry
SHALL be removed. Any supplied top-level `loops_bucket` key SHALL fail configuration admission regardless of
value, including a value equal to the port bucket. The error SHALL name the retired key and canonical replacement.
No compatibility alias, ignored value or raw-name precedence SHALL remain.

Research common-bucket validation SHALL obtain agentic-loop's effective bucket through its existing `DeclarePorts`
and canonical port facts. It SHALL compare that bucket against tools and research stages using their unchanged
current selections. It SHALL NOT change those owners' provisioning or execution behavior.

#### Scenario: Default and custom port identities agree across entry points

- **WHEN** valid default configuration or a valid custom `loops` KV-write override is decoded
- **THEN** DeclarePorts, NewComponent and loop initialization select the same bucket
- **AND** no raw bucket field or consumer-local default selects another bucket

#### Scenario: Removed JSON key is never ignored

- **WHEN** configuration supplies `loops_bucket`, including null, empty, default-valued or matching-port values
- **THEN** DeclarePorts and NewComponent return a configuration error naming the key and canonical replacement
- **AND** no bucket or dependent work is acquired

#### Scenario: Research compares actual loop declaration

- **GIVEN** a selected research capability whose tools and stages name bucket A
- **WHEN** agentic-loop's effective loops port names bucket B
- **THEN** existing composition validation refuses when A and B differ and identifies the conflicting owners and values
- **AND** matching custom declarations pass without changing research runtime behavior

#### Scenario: Invalid loop port is not repaired

- **WHEN** effective `loops` configuration cannot resolve as a valid output KV-write bucket
- **THEN** configuration admission fails with component and port context
- **AND** no literal fallback, raw configuration value or partial declaration repairs it

### Requirement: Approval lifetime is bounded by loop-state authority

Agentic-loop approval timeout SHALL default to 12h when `approval_timeout` is omitted.
A supplied value SHALL be a JSON string parsing as a Go duration satisfying `0 < timeout <= 12h`.
Explicit empty, null, non-string, malformed, zero, negative and above-12h values SHALL fail configuration admission
before dependent loop work. Invalid values SHALL NOT be defaulted or clamped.

Startup SHALL additionally require the actual loop-state authority policy defined by
"Loop-state authority is acquired and observed before loop work."
Scalar timeout validation SHALL NOT substitute for observed KV policy, nor SHALL a shorter timeout admit a bucket
with a different TTL.

The 12h maximum and observed TTL24h provide nominal grace only. They SHALL NOT be represented as a guarantee of
successful continuation, timeout application or settlement before expiry. This configuration rule SHALL NOT reset,
shorten or otherwise rewrite an already retained pending approval deadline.

#### Scenario: Timeout is omitted

- **WHEN** configuration omits `approval_timeout`
- **THEN** its effective value is 12h
- **AND** startup still observes and admits the actual loop-state authority before dependent work

#### Scenario: Explicit valid duration reaches the inclusive limit

- **WHEN** a supplied duration is positive and no greater than 12h
- **THEN** scalar timeout validation accepts it, including exactly 12h
- **AND** the configured value is preserved without clamping

#### Scenario: Explicit invalid or excessive timeout is refused

- **WHEN** a supplied value is empty, null, non-string, malformed, zero, negative or greater than 12h
- **THEN** configuration admission fails with the field, offending value and allowed duration range
- **AND** no approval wait, bucket acquisition or dependent consumer starts

#### Scenario: Replacement preserves a retained deadline

- **GIVEN** a valid retained pending approval and different valid replacement configuration
- **WHEN** replacement restores its deadline
- **THEN** the retained RequestedAt and Timeout are unchanged
- **AND** this startup configuration rule does not select or apply an approval decision

### Requirement: Loop-state authority is acquired and observed before loop work

Agentic-loop SHALL use its admitted `loops` KV-write bucket and call internal `loopbucket.AcquireOwner`.
The helper SHALL get first, create only for typed `jetstream.ErrBucketNotFound`, propagate every other lookup
failure without creation, and perform exactly one get after typed `jetstream.ErrBucketExists`.
Creation SHALL declare History 10, TTL 24h and nonbinding MaxBytes.

After get, create or race-get, actual status/backing-stream observation SHALL establish History exactly 10,
TTL exactly 24h and MaxBytes `<=0`. Failed or incomplete observation SHALL refuse admission.
Drift SHALL be refused without update or reconciliation, with observed and required policy values in the error.
I/O failures SHALL preserve their cause.

Only after both observed policy and the effective approval-lifetime requirement pass SHALL the component publish
the handle, perform approval-deadline discovery, allocate dependent consumers/query subscriptions or start its sweeper.
The operation SHALL use the Start-derived context and existing failed-Start rollback.
Trajectory-audit degradation SHALL remain a separate nonblocking policy.

#### Scenario: Two owners race to create a fresh bucket

- **WHEN** two processes acquire the same absent bucket with matching declaration
- **THEN** one create wins and the other gets the existing bucket
- **AND** both observe matching actual policy before dependent work

#### Scenario: Retained or race-winning policy drift exists

- **WHEN** actual History, TTL, or MaxBytes differs
- **THEN** startup refuses without updating the bucket
- **AND** publishes no handle and allocates no dependent work

#### Scenario: Lookup fails for a reason other than absence

- **WHEN** initial lookup returns permission, timeout, transport, or another non-not-found error
- **THEN** acquisition returns it and calls CreateKeyValue zero times

#### Scenario: Concurrent create wins between lookup and create

- **WHEN** CreateKeyValue returns typed ErrBucketExists
- **THEN** acquisition performs exactly one KeyValue get and validates the winner

#### Scenario: Policy observation fails

- **WHEN** status or required backing-policy observation fails or supplies incomplete evidence
- **THEN** startup returns an error rather than treating missing values as matching policy
- **AND** no authority handle, deadline discovery or dependent work is published or started

#### Scenario: Admission refusal precedes dependent allocation

- **WHEN** loop authority or approval-lifetime admission fails
- **THEN** the component remains not ready and returns the failure through existing rollback
- **AND** deadline discovery, task/response/result/signal/approval/verdict consumers and query subscriptions have not started
- **AND** no approval sweeper is running

#### Scenario: Trajectory failure retains its separate policy

- **GIVEN** loop authority and approval lifetime are admitted
- **WHEN** trajectory audit storage is incompatible or unavailable
- **THEN** the existing observable audit degradation policy remains nonblocking
- **AND** it does not weaken loop-authority admission
