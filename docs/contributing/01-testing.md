# Testing Policy and Patterns

This page is the canonical testing policy for SemStreams. It defines where evidence belongs, when Docker-backed
infrastructure is justified, and the wall-clock and isolation rules for new tests. The
[natsclient test-helper guide](../operations/23-natsclient-test-helpers.md) contains implementation examples and MUST
not redefine this policy.

## Testing Discipline

> Prompts focus attention. Contracts state what must hold. Property tests search for counterexamples.
> Mutation tests check whether our tests detect meaningful faults.

The working formulation above comes from the SemDev/SemStreams testing-policy discussion; SemStreams adoption is
tracked in [#1313](https://github.com/C360Studio/semstreams/issues/1313).

For each material behavior change, ask: **What would we observe if we were wrong, and have we actually looked?**
Choose checks according to the failure's consequence and shape. Test counts and global mutation scores do not answer
that question. Passing checks increase confidence within their stated scope; they do not establish general correctness.

The author, developer, reviewer, and handoff method below is language-neutral and reusable across the sem*
repositories. Each repository owns its adoption, commands, and additional obligations. This policy governs SemStreams
development; it does not establish product runtime enforcement, a testing dependency, or a supported-language claim.

### Author: Identify What the Check Must Distinguish

Before implementation, record these in the change's existing design or handoff:

- **Requirement:** cite the accepted requirement and its revision, including the applicable OpenSpec requirement.
- **Plausible violation:** describe a concrete implementation mistake that would break it.
- **Expected-result source:** explain how the expectation follows independently from the requirement, a small
  reference model, or an external contract. Repeating the implementation's algorithm reproduces its assumptions.
- **Observation:** name the contract-visible result or persisted state that distinguishes the violation from correct
  behavior, including forbidden side effects. A successful response can coexist with an incorrect write or emission.

For a 256-byte maximum, otherwise-valid inputs at 255, 256, and 257 bytes distinguish a rejection at the exact bound.
The requirement supplies the expected result; a broad random range may never exercise that boundary.

### Developer: Choose and Exercise Useful Checks

Use the smallest combination that addresses the failure shape:

- **Examples:** retain concrete regressions and explicit boundaries. Observe the intended failing assertion before
  implementing the fix or new behavior, following the developer contract's TDD requirements.
- **Properties:** search generated inputs or operation sequences for violations of a cited invariant. Derive
  generators from the input contract and expectations independently from the implementation. Include relevant bounds,
  empty cases, ordering, and repeated operations deliberately.
- **Fuzzing:** explore inputs with a named behavioral assertion and retain useful cases. Freedom from crashes alone
  supports a narrower claim than grammar conformance. Distinguish corpus replay from exploratory execution.
- **Targeted mutations:** apply the [mutation criteria](#when-targeted-mutation-evidence-is-required), introduce a
  plausible fault, and check whether the relevant assertion notices. There is no per-change mutation quota.

Address required acceptance and required rejection where applicable, including forbidden side effects. Self-consistency
does not establish conformance: a parser can round-trip invalid inputs consistently, and a rejection check can pass
against an implementation that rejects everything. Generate relevant invalid classes as well as valid ones, or cover
them with explicit cases; state which obligations each check actually exercises.

Use the [lowest sufficient tier](#choose-the-lowest-sufficient-tier) that can expose the claimed violation. An offline
regression may need production-boundary evidence alongside it. A helper test cannot establish wiring, delivery,
persistence, or API-to-UI behavior its environment does not exercise. Focused experiments supplement required suites.

Use existing repository tools and declared commands. Tool adoption or new automated gates require a separately scoped
change. This discipline does not select a mutation runner, require a library, or introduce a score threshold.

### When Targeted Mutation Evidence Is Required

For each material behavior change, record whether the following criteria apply in the existing issue, design, or
PR record. Targeted mutation evidence is required when the change:

- Changes enforcement of a consequential invariant whose violation could silently corrupt state, lose accepted
  work, permit unauthorized behavior, or leave owned work running after shutdown.
- Fixes a regression that the existing checks allowed to pass.
- Has a concrete, plausible fault identified by the author or reviewer whose detection by the relevant assertions
  remains uncertain.

Name the applicable criterion, requirement, and fault; select the bounded experiment needed to address that risk.
When none applies, a brief rationale is sufficient. Documentation-only changes do not trigger experiments merely
because they describe testing. No particular mutation library, repository-wide scan, or score threshold is required.

When a criterion applies, perform the controlled experiment below and preserve evidence that the baseline passed,
the mutant compiled and reached the selected test, the intended assertion detected the violation, and the restored
baseline passed. Include the mutation, commands, source snapshot, and observed results in the PR. Existing evidence
can satisfy this requirement when it covers the same fault and reviewed source/checks; link it rather than repeating
an experiment. A checkbox saying that mutation testing was done is insufficient.

Record survivors and invalid or inconclusive outcomes explicitly; none establishes detection. If the experiment
cannot be completed, record the constraint, unresolved risk, and proposed deferral. The reviewer must explicitly
accept or reject the deferral with reasons; an unaccepted or rejected deferral leaves the evidence requirement unmet.
An accepted deferral records remaining risk and does not establish detection. Required experiments and their evidence
are part of the issue's acceptance and PR review, not a new CI gate. They establish sensitivity at the recorded
revision and do not automatically detect later weakening of tests.

### Establish Sensitivity to a Selected Mutation

Perform a bounded experiment in a disposable copy, or preserve an exact backup of every affected file. In SemStreams,
follow the developer/reviewer contracts' `cp` backup and checksum restoration procedure; do not use Git restoration or
stash commands. Keep concurrent and pre-existing work intact. During each comparison, change only the implementation
under assessment; keep test code, generators, expectations, fixtures, and runner configuration fixed.

1. The unmodified baseline passes the selected checks, and the intended tests actually execute.
2. Apply a named, relevant mutation. The mutant builds and reaches the selected test.
3. The relevant assertion fails because it observes the intended violation.
4. Restore the original bytes, verify checksums, and rerun the selected checks successfully.

If checks change after investigating a survivor, establish a new passing baseline and repeat the experiment. Do not
restore over concurrent edits; isolate the experiment when exclusive ownership of affected files cannot be maintained.

Record the source revision and the experiment snapshot, including relevant uncommitted and untracked files; identify
their contents with a retained patch/files and checksums or an equivalent reproducible artifact. Record the mutation
location and operation, selected tests and commands, runner version/configuration, relevant environment, observed
results, and replay seed or shrunk counterexample when available. Capture enough output to distinguish the intended
assertion failure from a broken runner. Baseline, mutant, and restored runs must refer to the same recorded checks.

Retain small experiment snapshots inline in the PR record; attach larger snapshots, including patches, relevant files,
and checksums, to the PR and link them from the handoff. Session scratchpads and private agent memory are not durable
evidence homes.

For a generated failure, replay the same input or operation sequence (or a stable seed) against the mutant and the
original implementation before recording detection. If that comparison cannot be reproduced, record it as
inconclusive rather than attributing a difference between random samples to the mutation.

Preserve a replayable witness for important generated discoveries. If generator or tool changes make replay unstable,
retain the case as a deterministic regression test. Label retained mutation witnesses as synthetic: they do not show
that the original implementation contained the bug. Follow the package's corpus-curation guidance when retaining cases.

Record execution outcomes separately from reviewer assessments:

- **Detected:** the valid mutant caused the intended assertion failure, with passing baseline and restored checks.
- **Survived:** the selected checks did not detect the mutation. Investigate missing inputs, assertions, or scope.
- **Invalid:** the mutant could not build or was otherwise ineligible for this experiment.
- **Inconclusive:** an error, external timeout, skipped or unselected test, or unrelated failure did not establish
  detection by the intended assertion. A bounded assertion that observes a required termination failure can establish
  detection; an outer runner timeout alone cannot.

Assessments such as equivalent, outside the claimed scope, or deferred do not replace those observations. An
equivalence assessment names the applicable contract and input domain, its reasoning, and its reviewer; passing the
selected tests is not the argument. Unresolved survivors remain unresolved. A runner's zero exit may mean its
evaluation succeeded; inspect the report. A nonzero exit alone does not establish detection of the intended fault.

### Reviewer: Challenge the Relationship

Read the cited requirement and ask whether an implementation could pass the checks while violating it. Also ask:
**What relevant obligation or failure mode is missing from the checks entirely?** Confirm the expected-result source
is independent, generators reach relevant boundaries, and assertions observe required effects. Review survivors and
decisions to defer mutation checks. A generator being able to reach a boundary does not ensure a particular run did;
explicit boundary cases or retained witnesses provide dependable checks for those cases.

Independently apply the [mutation criteria](#when-targeted-mutation-evidence-is-required); do not rely only on the
issue author's selection. Where a criterion applies, require the controlled evidence or explicitly accept or reject
a recorded deferral with reasons and its remaining risk. Missing applicability reasoning, an unaccepted deferral,
or an unsupported detection claim is a finding.

Distinguish measurements from judgments. Output can establish an observed assertion failure; whether it adequately
challenges a requirement remains a review assessment. An equivalence argument or a reviewer finding no counterexample
is not proof of correctness. Findings strengthen checks or identify uncertainty. If evidence challenges the accepted
requirement itself, surface a specification gap through the existing owner decision process; do not silently weaken
the requirement to fit the implementation.

### Handoff: Preserve the Limits of the Evidence

Use the existing change, review, or PR record to summarize the requirement, plausible violation, independent
expectation, distinguishing observation, checks actually run, snapshot, commands, results, and replay information.
For selected mutations, include baseline, validity, assertion result, and restored-baseline evidence. Preserve
deferred checks, unresolved survivors, and reviewer assessments with their reasons. Link existing artifacts instead
of duplicating them.

Say which fault a check detected and what remains untested. Planned checks remain pending until executed. Adoption of
this guidance does not establish automated enforcement or narrow any existing verification or release gate.

### SemStreams Application

The [developer contract](../../.agents/contracts/semstreams-developer.md#test-and-operational-fidelity) owns the exact
`// spec:` citation format, native fuzz obligations, boundary requirements, and specification-gap handling. The
[reviewer contract](../../.agents/contracts/semstreams-reviewer.md#test-fidelity) applies those requirements
independently.
`task spec:properties` checks citation resolution; it does not validate the property's meaning or sensitivity.

Use [the entity-ID properties](../../pkg/types/entity_id_prop_test.go) and their
[curated boundary witness](../../pkg/types/testdata/rapid/README.md) as recorded examples. Their acceptance,
classified-rejection, and self-consistency checks protect different obligations; none subsumes the others.

The tiers and commands below, race checks for changed concurrency, production constructor/registry/codec/wire evidence,
real-NATS checks where its semantics matter, and breaking-change E2E requirements continue to apply. SemDev's
constitutional, offline-pin, clean-room, and product-delivery requirements remain local to SemDev; sister repositories
may adopt this method through their own owners. No sister runtime or workflow is changed by this policy.

## Choose the Lowest Sufficient Tier

- **Unit:** one function, type, or in-process component behavior. Use `*_test.go` and
  `Test<Type>_<Behavior>`. Unit tests MUST NOT use Docker, real NATS, network services, or fixed ports.
- **Integration:** one production boundary or component wire. Use `*_integration_test.go`,
  `TestIntegration_<Behavior>`, and `//go:build integration`. A test MAY use one real dependency when its semantics
  are the assertion.
- **Live provider:** a real paid or local model provider. Use `*_live_test.go` and a provider-specific build tag such
  as `live_llm`. Tests MUST skip cleanly when credentials or the service are absent.
- **End to end:** deployed binaries and a cross-component path. Put these tests under `test/e2e` and invoke them
  through `task e2e:*`. A tier MAY use the stack it declares.

A test MUST use the lowest tier that can prove the behavior. Real NATS is justified for JetStream delivery, KV
watch/replay, persistence, consumer, or production-wire behavior. JSON mapping, validation, state transitions, retry
policy, and time calculations normally belong in unit tests with injected dependencies.

Do not move a test upward merely because its production code uses NATS. Conversely, do not replace a production-wire
regression with a mock that cannot reproduce the contract being protected.

Breaking migrations have an additional hard requirement: run the relevant E2E tier before the breaking commit lands.
See [End-to-End Testing](02-e2e-tests.md).

## Canonical Commands

Use repository tasks for full suites so local and CI behavior stay aligned:

```bash
task test                 # Unit suite
task test:race            # Unit suite with the race detector
task test:integration     # Docker-backed integration suite, race detector, fresh evidence
task check:push           # Full pre-push gate
```

The canonical integration runner uses `-race -failfast -tags=integration -timeout=20m -count=1 ./...`. The
`integration` constraint is additive: this single command runs ordinary unit tests plus integration-tagged tests.
CI therefore runs the canonical tagged suite once instead of first repeating `go test -race ./...`. `-failfast` stops
avoidable work after a test failure. The runner does not override Go's package parallelism; changes to concurrency
MUST be supported by measured container, CPU, memory, and duration evidence.

For focused iteration, preserve the same flags:

```bash
scripts/run-integration-tests.sh ./processor/graph-index/...
```

Do not invoke focused integration packages with `go test` directly. The runner is the single owner of flags, Docker
preflight, the host lock, and the Reaper policy. Go's `-timeout=20m` is a per-package timeout, not a whole-suite
deadline. A local aggregate run has no additional whole-suite deadline and can be interrupted by the caller. CI's
25-minute outer job timeout is the whole-job and process-tree bound, including setup and cleanup. The 20-minute
per-package value is transitional and is not a budget for a new test.

### Integration Runner Host Contract

Every full or focused integration invocation acquires `/tmp/semstreams-integration.lock` before touching Docker. Lock
contention fails immediately by default and reports the current owner. A caller that deliberately wants to wait MAY
set `SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS` to an integer from 1 through 3600; that value is a bounded wait budget,
not permission to wait indefinitely. The fixed host path makes independent worktrees and shells contend on the same
Docker resource.

After acquiring the lock, the runner verifies Docker, exports `TESTCONTAINERS_RYUK_DISABLED=false`, and inspects the
repository-pinned `nats:2.14-alpine` image. A cached image is used without a network pull. The runner pulls only when
that exact tag is absent; it does not silently refresh an existing cache on every test run. To deliberately refresh a
cached image, set `SEMSTREAMS_INTEGRATION_REFRESH_IMAGE=1`. The canonical runner performs that pull under the same host
lock and bounds it to five minutes, so refresh cannot race another integration invocation or hang indefinitely.

Helper cleanup remains the primary cleanup path. Ryuk is enabled as crash safety, not as a replacement for bounded,
observable `t.Cleanup` teardown.

## Test Independence

Each test MUST be independently repeatable, order-independent, and safe after a preceding failure. A test MUST NOT
depend on another test's stream, bucket, durable consumer, global logger, temporary file, or goroutine cleanup.

Use table-driven tests when cases share one behavioral contract:

```go
func TestCondition_Evaluate(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name string
        got  any
        want bool
    }{
        {name: "matching value", got: "active", want: true},
        {name: "different value", got: "idle", want: false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            condition := Condition{Operator: "eq", Value: "active"}
            assert.Equal(t, tt.want, condition.Evaluate(tt.got))
        })
    }
}
```

Unit tests SHOULD use `t.Parallel()` when they do not mutate process globals. Integration tests MUST NOT use
`t.Parallel()` unless every mutable external resource is independently named and the package remains within its
approved container budget.

## Container Contract

### Justification and Count

Zero containers is preferred. A new top-level integration test MAY obtain one live, usable NATS container only when
real NATS or JetStream behavior is part of the assertion. The test or PR MUST state which production semantic a fake
could not prove.

A test MUST NOT have more than one live NATS container without an approved exception. The canonical helper has one
narrow sequential recovery: after internal readiness, if at least one successful Docker Inspect snapshot showed
a configured required mapped port absent during the single 10-second mapping budget, the parent context remains live,
and cleanup succeeds, it may start one replacement. The default required set is only 4222/tcp. `WithMonitoring()` adds
8222/tcp to the container command, exposed ports, required set, and recovery eligibility. Inspect failures alone do not
authorize replacement. The helper permits at most two starts, never two live containers, and MUST NOT replace an
attempt after startup, host lookup, connection, readiness, resource, cancellation, or cleanup failure. Callers cannot
request or reproduce this recovery themselves. Reusing a package-wide container is also an exception, not the default
optimization. State pollution is more damaging than container startup cost.

### Isolation and Naming

A fresh container is the default when production requires fixed bucket, stream, or ObjectStore names. When resources
can be named without changing production behavior, derive their names from a canonical sanitized test name plus a
run-unique suffix. This applies to:

- KV buckets and keys;
- streams, subjects, durables, and consumer names;
- ObjectStore buckets and object keys;
- component, flow, and entity identifiers;
- temporary directories and listener addresses.

Tests MUST ask the operating system for an available port, such as `127.0.0.1:0`. They MUST NOT predict a port from a
fixed range, process ID, timestamp, or random number.

A shared-container exception MUST prove one of these isolation strategies:

1. every mutable resource is uniquely named per test; or
2. setup restores a known empty state and cleanup verifies that no state, consumer, subscription, or goroutine leaked.

Serial execution alone is not isolation. `WithBucketPrefix` alone is not proof when components also create fixed
streams, subjects, consumers, or ObjectStores.

### Readiness

Container readiness MUST observe an internal service signal before resolving host-side ports. For NATS, the canonical
helper owns the internal ready signal. Only after that signal may it resolve `Host`, then observe the configured
required port set from one Docker Inspect snapshot per poll under a single 10-second budget with context cancellation.
Unconfigured ports are ignored. Monitoring assertions MUST request `WithMonitoring()`; without it, the helper does not
start or expose the monitoring listener and `TestClient.MonitoringURL` is empty.

Test code MUST NOT call `testcontainers.GenericContainer`, `ForListeningPort`, `ForHTTP`, `Host`, or `MappedPort`
directly. Those calls belong in the canonical test substrate. Every helper setup failure reports its attempt, phase,
original cause, parent-context state, container ID when one was returned, and any cleanup cause. A required-port
observation failure MUST additionally report:

- total observation attempts;
- the shared mapping budget and elapsed time;
- the last successful observation's attempt and missing ports, when one exists; and
- the last Inspect error and its attempt, when one exists.

An internal signal, mapped-port resolution, client connection, and application readiness are separate phases. A
successful phase MUST NOT be treated as proof that a later phase is ready.

### Cleanup and Reaper

`natsclient.NewTestClient(t, ...)` owns its client and container and registers `t.Cleanup`. Callers MUST NOT add a
second `defer tc.Terminate()` or terminate the underlying container directly. Tests remain responsible for component,
subscription, file, and goroutine cleanup.

Explicit cleanup MUST close or drain the client, terminate the container under a bounded context, and report failures.
The testcontainers Reaper is crash safety, not primary cleanup. Task and CI MUST use one documented Reaper policy.
Disabling the Reaper requires an approved, expiring exception and a post-run container-leak check.

`NewTestClient(&testing.T{}, ...)` is forbidden. A rare approved `TestMain` owner MUST use the error-returning shared
helper, preserve `m.Run()`'s exit code, and terminate its infrastructure on every reachable path.

## Synchronization and Contexts

Tests MUST NOT use `time.Sleep` to wait for readiness, delivery, retries, cleanup, or state convergence. A longer sleep
does not make an observation causal.

Prefer, in order:

1. a channel, callback, `WaitGroup`, or explicit ready/done signal;
2. an injected clock or retry hook for time-dependent unit behavior;
3. bounded observation polling when the system exposes no signal.

Polling MUST have a narrow deadline and report the last value and last error on failure. It MUST NOT silently turn a
missing producer, subscriber, or component into a full-window timeout.

Test I/O contexts MUST derive from `t.Context()` and then narrow the deadline:

```go
func testContext(t *testing.T, timeout time.Duration) context.Context {
    t.Helper()
    ctx, cancel := context.WithTimeout(t.Context(), timeout)
    t.Cleanup(cancel)
    return ctx
}
```

Helpers MUST accept the caller's context. `NewTestClient` setup derives from `t.Context()` and narrows it for container
startup, mapped-port resolution, connection, and initial resource creation.

Cleanup is the intentional exception. The Go test runner cancels `t.Context()` before invoking registered cleanup
functions, so client close and container termination cannot derive from it. Each cleanup operation receives its own
bounded `context.Background()` child. Those independent contexts preserve the measured 10-second cleanup ceiling
even after test cancellation; they are not a general license to use `context.Background()` for test I/O.

## Budgets for New Tests

Budgets make resource use reviewable. They are defaults for new evidence; existing packages are migrated by the
ratchet policy below.

| Scope | Target | Hard ceiling without exception |
|---|---:|---:|
| Unit top-level test under `-race` | 1s | 5s |
| Integration top-level test on a warm host | 30s | 3m |
| Live, usable NATS containers per top-level test | 0 | 1 |
| Mapped-port resolution | immediate | 10s |
| Cleanup | immediate | 10s |
| Integration package | 3m | 5m |

Container image download is measured separately from test execution. E2E tiers MUST declare their expected duration
and CI ceiling in the task or workflow that owns the tier.

A new test MUST NOT increase an existing package's container count or wall-clock baseline without explaining the new
evidence and receiving exception approval. Raising a timeout is not an acceptable fix for an unexplained regression.

## Failure Evidence

Failures involving asynchronous or external state MUST identify the condition, elapsed time, attempts, last observed
value, last error, and relevant resource names. Container-start failures SHOULD include bounded container logs. Tests
using randomness MUST print the seed.

CI duration and container-count reporting is migration work, described below. Until it is implemented, reviewers MUST
request local timing evidence for tests that create containers, add polling, or approach a budget.

## Exceptions

An exception requires all of the following:

- the exact test and rule being waived;
- why the evidence cannot be produced within the default contract;
- measured wall-clock and resource evidence;
- the isolation and cleanup strategy;
- a linked issue or decision, an owner, and an expiry or removal condition; and
- approval from the owner and `semstreams-reviewer`.

An exception MUST be narrow. It MUST NOT authorize blanket package serialization, unbounded contexts, arbitrary
sleeps, or shared mutable state. Expired exceptions are invalid.

## Existing-Suite Migration Policy

The policy above applies immediately to new or substantially rewritten tests. Existing violations are migrated in
priority order; touching a nearby test MUST NOT make the baseline worse.

### P0: Substrate and Unit-Layer Integrity

1. Complete internal-signal readiness and bounded mapped-port resolution in the canonical helper.
2. Route legacy direct testcontainers helpers through that substrate.
3. Move Docker-backed cases out of untagged unit files.
4. Replace fabricated `testing.T` values and make cleanup bounded and observable.
5. Reconcile the Task and CI Reaper policy.

### P1: Highest-Churn Packages

Measure container starts and duration, then address packages with the most startups and sleeps. First move cases that
do not need real NATS down to unit tests. Next remove duplicate containers within one logical test. Replace sleeps with
signals or bounded diagnostic observation.

Audit shared containers for all mutable resource classes. Keep a fresh per-test container when reset cannot faithfully
restore production state; do not consolidate merely to improve elapsed time.

### P2: Suite Shape and Ratchets

1. Normalize integration and live-provider file/test naming.
2. Avoid rerunning the complete unit suite as integration evidence once selectors can be verified.
3. Add resource-name helpers where they preserve production behavior.
4. Ratchet package duration and container baselines downward from measured evidence.

Blanket `-p 1` and blanket conversion to shared containers are explicitly out of scope.

## Enforcement Status and Backlog

The following AST-backed guards have landed and run directly in CI:

- container starts in test files require an integration build constraint;
- `GenericContainer`, `ForListeningPort`, `ForHTTP`, `Host`, and `MappedPort` calls are restricted to the canonical
  container substrate;
- fabricated `&testing.T{}` values are rejected; and
- exact existing `time.Sleep` calls in integration files are ratcheted.

The guard verifies that it scanned a non-trivial repository surface, and every category has positive and negative
fixtures. A zero-match scan is therefore not accepted as proof that the repository is clean.

Fabricated `testing.T` values, untagged container starts, and direct container APIs are zero-debt categories. They fail
immediately and cannot be added to the baseline. The only recorded debt is 305 legacy integration sleeps.

`test/testinfra/policy_baseline.json` identifies each of those sleeps by category, file, function, call, and ordinal.
It is shrink-only and manually maintained: removing a live sleep requires removing its now-stale entry, while a new
or moved sleep fails CI. The baseline MUST NOT acquire new entries.

The following policy remains review-enforced rather than structurally enforced:

- integration and live-provider file/test naming;
- exception ownership and expiry;
- per-test and per-package durations;
- container counts and complete external-resource naming; and
- general `t.Context()` use.

A helper-specific test may structurally protect `NewTestClient` setup and cleanup context behavior, but it does not
enforce context use across the wider test suite. Duration and container-count reporting remain migration backlog.

## Focused Patterns

### Graphable Implementations

Test deterministic IDs and complete triples without infrastructure. Use registered three-part predicates from the
[vocabulary](../../vocabulary/README.md); this illustrative reading emits `sensor.temperature.celsius`:

```go
func TestSensorReading_Graphable(t *testing.T) {
    t.Parallel()

    reading := SensorReading{
        DeviceID:   "sensor-042",
        SensorType: "temperature",
        Value:      21.5,
        OrgID:      "acme",
        Platform:   "logistics",
    }

    assert.Equal(t,
        "acme.logistics.sensor.environmental.temperature.sensor-042",
        reading.EntityID())
    assert.Contains(t, reading.Triples(), Triple{
        Subject:   reading.EntityID(),
        Predicate: "sensor.temperature.celsius",
        Object:    21.5,
    })
}
```

### Race Detection

All changed concurrency paths MUST pass the race detector. Use atomics, mutexes, channels, or ownership transfer; do
not make a race disappear by serializing the entire suite.

```bash
task test:race
task test:integration
```

### Benchmarks and Coverage

Benchmarks measure a stated operation and MUST exclude setup with `b.ResetTimer()` or `b.StopTimer()`. Coverage is a
diagnostic, not a substitute for critical-path and edge-case evidence.

```bash
go test -bench=. -benchmem ./processor/graph/clustering/...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Critical paths SHOULD retain at least 80% behavioral coverage, with explicit error, nil, empty, boundary, cancellation,
and concurrency cases where relevant.

## Review Checklist

- Is this the lowest sufficient tier?
- If it creates a container, which real service semantic requires it?
- Are all mutable resources isolated, including consumers and ObjectStores?
- Does readiness observe facts in order rather than predict host state?
- Are waits causal, bounded, and diagnostic?
- Do setup and test I/O contexts derive from `t.Context()`, with independent bounded contexts reserved for cleanup?
- Is cleanup single-owner, bounded, and observable?
- Does the test stay within wall-clock, container, and concurrency budgets?
- Is any exception narrow, measured, approved, and expiring?

## Related Documentation

- [natsclient Test Helpers](../operations/23-natsclient-test-helpers.md) — Docker-backed NATS implementation patterns
- [End-to-End Testing](02-e2e-tests.md) — deployed-stack evidence and tier commands
- [Contract Testing](04-contract-testing.md) — cross-package contract checks
- [NATS Request and Retry](../operations/07-nats-request-retry.md) — classified request/reply behavior
