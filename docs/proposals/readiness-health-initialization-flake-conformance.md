# Readiness health-initialization flake conformance

Status: **IMPLEMENTATION APPROVED — unrelated full-repository race failure remains outstanding**

Issue: [#1059](https://github.com/C360Studio/semstreams/issues/1059)

## Baseline and accepted artifacts

The production contract baseline is `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`. The implementation worktree
started from `35a64ee19ad86f14bd2a1fc6fe0b39984e169a35`; the intervening committed changes do not modify
`service/` production files.

The accepted artifacts are:

- `docs/proposals/readiness-health-initialization-flake-inventory.md`, current SHA-256
  `dcd3b6ddc814134d8a18d4bb3ff8aaec98e43e7bee71b66f570b7dd23551f5c7`, independently reviewed as
  `INVENTORY PASS`;
- `docs/proposals/readiness-health-initialization-flake-design.md`, current accepted-artifact SHA-256
  `de6b3fb86ebbb5a3aedba193d757a83e952a7582556ffafdec4fc341a8f228dc`. The independent design review
  passed the pre-acceptance artifact at SHA-256
  `fb85f6b88eb3ef40011390cea8bd8fa4efb3c230b59c1c23aa4db7bbe40e738c`; the owner then accepted all
  five rulings on 2026-08-23.

## Exact implementation delta

The implementation changes one existing test file:

- `service/startup_observability_test.go`: 95 insertions and one deletion relative to worktree HEAD.

This conformance record is the only implementation documentation added by this step. The accepted inventory and
design are planning artifacts, not production changes.

No production Go file, exported symbol, constructor, interface, configuration, payload, persistence surface, subject,
bucket, OpenSpec file, ADR, schema, or E2E test changes in this implementation.

## Accepted-contract mapping

- `StartAll` success and boot commitment do not imply every asynchronous initial health observation completed.
  `TestReadinessWaitsForInitialServiceHealthObservation` calls real `Manager.StartAll`, confirms `bootCommitted`, and
  holds the ComponentManager health check behind an explicit channel gate after it enters.
- `/readyz` remains fail-closed while initial service health is unobserved. The companion test invokes the real
  readiness handler while the health check is blocked and requires HTTP 503 with `NOT READY`.
- Readiness becomes healthy only after the real check completes and health is stored. The test releases the check,
  waits for `OnHealthChange(true)`, confirms `cm.IsHealthy()`, and then requires HTTP 200 with `READY`.
  `BaseService.performHealthCheck` stores health before invoking the callback.
- Tests that require initial health establish it causally. `TestReadinessIncludesHealthyNonLifecycleDiscoverables`
  installs `OnHealthChange` before `cm.Start`, waits for the healthy callback, and confirms `cm.IsHealthy()` before
  committing startup and asserting HTTP 200.
- Synchronization does not depend on sleeps, polling, or scheduler availability. Both tests use channels and
  `sync.Once`. Two-second `time.After` cases are test-safety failure bounds; they do not determine readiness.
- Cleanup cannot strand the gated monitor. The companion test registers cleanup before `StartAll`; cleanup releases
  the gate through `sync.Once`, calls `StopAll` with a bounded terminal context, and only then cancels the retained
  lifecycle context. The amended original test also uses a bounded terminal Stop context.
- Runtime behavior and adopter obligations remain unchanged. All code changes are in
  `service/startup_observability_test.go`; no optimistic health seed or synchronous production check was introduced.

## RED and GREEN evidence

The initial results in this section are developer-reported. The independent reviewer subsequently reran the accepted
focused and package proofs.

RED on the unchanged test and scheduler-constrained baseline:

```text
GOMAXPROCS=1 go test -race ./service \
  -run '^TestReadinessIncludesHealthyNonLifecycleDiscoverables$' \
  -count=10

Result: failed 10/10; expected HTTP 200, observed HTTP 503.
```

GREEN on the amended test plus the new deterministic companion test:

```text
GOMAXPROCS=1 go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

Result: PASS (4.889s).
```

GREEN under the default scheduler:

```text
go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

Result: PASS (4.684s).
```

GREEN package run:

```text
go test -race ./service

Result: PASS (6.709s).
```

Formatting evidence:

```text
git diff --check

Result: clean.
```

## Independent implementation review

The independent `semstreams-reviewer` returned `APPROVED`. The reviewer reran:

```text
GOMAXPROCS=1 go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

Result: PASS.
```

```text
go test -race ./service \
  -run 'TestReadinessIncludesHealthyNonLifecycleDiscoverables|TestReadinessWaitsForInitialServiceHealthObservation' \
  -count=100

Result: PASS.
```

```text
go test -race ./service

Result: PASS (6.813s).
```

```text
git diff --check

Result: clean.
```

The review approved the test-only implementation against the accepted contract. It does not convert an unrelated
repository-wide gate failure into a pass.

## Repository gate results

The following root gates passed:

```text
task lint
Result: PASS.

task test:integration
Result: PASS across the canonical uncapped integration-tagged suite.

go build ./...
Result: PASS.

task schema:generate
Result: PASS.

git diff --exit-code -- schemas/ specs/
Result: PASS; no schema or spec delta.

go test ./test/contract/...
Result: PASS.
```

The full repository race gate did not pass:

```text
go test -race ./...

Result: FAIL; test/testinfra.TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock failed once.
```

That failure is outside the #1059 implementation surface. It remains an unresolved candidate gate; this record does
not claim a green full-repository race run.

## Non-deltas and release scope

The implementation preserves the accepted fail-closed readiness contract. It adds no product behavior, OpenSpec
requirement, schema artifact, or cross-component dataflow. No new E2E obligation arises from this test-only correction,
and this record claims no E2E execution. The next tag's exact-candidate E2E proof remains a separate release gate.

## Outstanding mandatory gates

The #1059 implementation and its focused/package proofs are independently approved. The exact candidate is not yet
complete: `go test -race ./...` still requires a green run after the unrelated
`test/testinfra.TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock` failure is resolved or shown clean on a
valid rerun. The next tag's separate exact-candidate E2E gate also remains outside this conformance claim.
