# Integration runner termination flake design

Status: **ACCEPTED — owner approved base design and focused GREEN amendment 2026-08-23**

Repository baseline: `35a64ee19ad86f14bd2a1fc6fe0b39984e169a35`

Accepted inventory: `docs/proposals/integration-runner-termination-flake-inventory.md`, SHA-256
`84fb4cd3b4ac87064685002b6768ab2341c67a25ce0dd4beab3fdaf4d134e517`, independent `INVENTORY PASS`.

Owner acceptance: all seven rulings in this artifact were explicitly approved on 2026-08-23. Option F, the
test-private fake-clock pause, both forced-omission proofs, and the stated production non-deltas are binding
implementation scope. The measured replacement of the aggregate GREEN command was narrowly re-accepted at the current
artifact hash on 2026-08-23 after independent
`AMENDMENT DESIGN REVIEW PASS`. Merge, release, and tag approval remain separate gates.

## Options considered

### Option A: Do nothing

Retains a race between two non-atomic PID observations and the runner's bounded KILL escalation. The isolated 100/100
pass does not invalidate the full-gate failure. Reject.

### Option B: Give the independent probe fd 6

Passing the reap-ack descriptor to the separately launched `rmdir` probe removes the `Bad file descriptor` diagnostic,
but does not keep the helper alive. The probe can still observe post-reap state and remove the lock itself. Reject.

### Option C: Move the negative probe before TERM

The pre-TERM probe reliably finds a live helper, but the post-TERM owner assertion still requires the test goroutine to
run before the grace expires. This reduces the race window but still predicts scheduler latency. Reject.

### Option D: Remove the live-child negative probe

Final-state assertions prove successful completion but weaken mutation resistance: cleanup that removes the lock before
exact wait can regain a false green. Reject.

### Option E: Change production

A production pause hook, public environment variable, longer grace, or clock change alters adopter-visible behavior
solely for a test defect. The inventory found no production ordering failure. Reject.

### Option F: Causally pause the existing private clock boundary

Extend only the private `date` wrapper and helper plumbing:

1. The helper records a private TERM-received sentinel before acknowledging TERM.
2. The first `date +%s` wrapper call after that sentinel signals a new grace-paused pipe and blocks on a release pipe.
3. The parent waits for both TERM acknowledgement and grace pause.
4. While the runner is stopped before computing the KILL deadline and the helper remains blocked, the test proves the
   exact owner bytes, live PID, and negative `rmdir` behavior.
5. The parent releases the helper, then releases the clock wrapper.
6. Runner cleanup resumes, exact-waits, and performs its unchanged token-checked lock release.

This preserves the production-shaped path and removes the scheduler-speed premise. Recommend Option F.

## Measured premises

1. Production cleanup already orders terminate/reap before release at `scripts/run-integration-tests.sh:227-230`.
2. The KILL deadline is computed through `date +%s` at `scripts/run-integration-tests.sh:141-148`.
3. The termination test already injects a private `date` wrapper at
   `test/testinfra/integration_runner_contract_test.go:228-234`.
4. The helper writes TERM acknowledgement before waiting for release at test lines 391-397.
5. The observed failure proves runner KILL/reap and owner removal can occur between the parent's PID check and probe.
6. `exec.Cmd.ExtraFiles` already supplies private causal channels at test lines 247-265.
7. One goroutine owns `Cmd.Wait` at test lines 795-804; all paths join the same result.
8. The exact isolated test passed 100/100 while the full race gate failed once. Frequency is low; the gap is real.
9. The test uses a fake Docker executable and no NATS/Docker infrastructure. #736 and #1054 are not its direct owner.

## Exact target state

Committed runtime scope is only:

```text
test/testinfra/integration_runner_contract_test.go
```

No committed change is permitted to:

```text
scripts/run-integration-tests.sh
.github/workflows/ci.yml
taskfiles/test.yml
production environment/configuration
exported Go APIs
OpenSpec current capability specs
ADRs
sister repositories
```

## Private synchronization

Retain existing fd ownership:

- fd 3: parent readiness;
- fd 4: TERM acknowledgement;
- fd 5: helper release;
- fd 6: post-reap lock-removal acknowledgement.

Add two termination-case-only pipe pairs:

- fd 7: grace-pause acknowledgement from the private `date` wrapper;
- fd 8: grace release from parent to wrapper.

Add test-private paths for the helper's TERM-received sentinel and a one-shot marker ensuring exactly one `date`
wrapper invocation owns the grace pause. Paths and environment names remain confined to the contract test.

## Helper behavior

For the termination case:

1. Install signal handling and release ownership as today.
2. On SIGTERM, write the private TERM sentinel.
3. Write TERM acknowledgement only after the sentinel write succeeds.
4. Continue waiting on the existing helper-release channel.
5. Preserve pre-TERM release/EOF behavior for `TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits`.
6. Add no context, second signal owner, sleep, polling loop, or continuing goroutine.

## Private date-wrapper behavior

The wrapper retains its existing parent-ready behavior. On the first invocation after the TERM sentinel exists:

1. Atomically claim the one-shot marker.
2. Write grace-pause acknowledgement through fd 7.
3. Block reading fd 8.
4. On a release byte or EOF, delegate to real `date`.

Later invocations delegate directly. The wrapper never fabricates time or changes the deadline; it holds the exact
pre-deadline boundary open for the test.

## Parent test behavior

After successful runner start:

1. Register cleanup before any fallible endpoint close.
2. Cleanup closes helper-release and grace-release writers before `killAndWait`.
3. Close every parent endpoint inherited only by the child, including the new endpoints.
4. Wait for parent readiness and capture the exact token-bearing owner bytes.
5. Signal runner SIGTERM.
6. Wait for helper TERM acknowledgement.
7. Wait for grace-pause acknowledgement.
8. Only after both signals, require unchanged owner bytes, exact helper PID presence, and negative `rmdir` refusal with
   exit 73 and the expected diagnostic.
9. Release the helper first.
10. Release the private date wrapper second.
11. Join the existing sole waiter.
12. Preserve exact `*exec.ExitError`, exit 130, exited `ProcessState`, reap acknowledgement, PID absence, and lock
    absence assertions.

The independent probe still does not inherit fd 6. Causal grace pause guarantees it takes the live-PID refusal branch
before reaching fd 6.

## Cleanup convergence

Every early failure after `command.Start` converges through:

1. close helper release;
2. close grace release;
3. kill runner only if still live;
4. join the existing `commandWaiter`;
5. close remaining endpoints through idempotent cleanup.

EOF unblocks both helper and private date wrapper. No second `Cmd.Wait` is introduced.

## Behavior contract

GIVEN Bash retained the exact pull PID and holds its token-bearing lock

AND the helper installed TERM handling

WHEN the runner receives SIGTERM

THEN the helper records TERM receipt before acknowledging it

AND the first grace-deadline clock read pauses before deadline computation

AND while paused, exact owner bytes remain unchanged and the helper PID remains present

AND the private negative `rmdir` probe refuses removal while that PID exists

WHEN the test releases the helper and then the clock wrapper

THEN production cleanup resumes under its unchanged TERM/KILL policy

AND Bash exact-waits the owned PID before token-checked lock removal

AND the runner exits 130 through its existing EXIT trap.

## TDD evidence

### RED

Add the two pipe pairs, sentinel environment, and parent wait for `cleanup grace paused` before wiring the helper
sentinel and date-wrapper producer:

```text
go test -race ./test/testinfra \
  -run '^TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock$' \
  -count=1 -timeout=20s
```

Required RED: bounded failure waiting for the missing grace-pause milestone. Runner timeout is not completion.

### GREEN

Measured implementation cost under `-race`:

| Proof | `-count=10` | Projected `-count=100` | Command budget |
|---|---:|---:|---:|
| Termination ordering | 11.544s | 115.44s | 180s |
| Pre-TERM release | 11.529s | 115.29s | 180s |
| Waiter cleanup | 1.368s | 13.68s | 30s |

The three claims run separately so each has a measured diagnostic budget and any failure identifies its owning proof:

```text
go test -race ./test/testinfra \
  -run '^TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock$' \
  -count=100 -timeout=180s

go test -race ./test/testinfra \
  -run '^TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits$' \
  -count=100 -timeout=180s

go test -race ./test/testinfra \
  -run '^TestCommandWaiter_TimeoutCleanupKillsAndReapsThroughOneOwner$' \
  -count=100 -timeout=30s
```

Required: every command completes 100/100 under `-race`, with no assertion failure or leaked helper, runner, pipe,
process, or lock.

The former aggregate command is not acceptance evidence:

```text
go test -race ./test/testinfra \
  -run 'TerminationReapsPull|PreTERMReleaseExits|TimeoutCleanupKillsAndReaps' \
  -count=100 -timeout=180s
```

It timed out at 180.197s without an assertion failure or leak. The measured combined projection is about 244 seconds,
so its ceiling is below the work requested.

Then run the package once as an integration check:

```text
go test -race ./test/testinfra -count=1 -timeout=120s
```

### Forced omission

Two negative proofs are required:

1. In an isolated temporary mutation, omit the date wrapper's fd 7 pause acknowledgement while retaining the parent
   consumer. The focused test must fail at the bounded missing-milestone assertion. This proves the handshake is
   load-bearing.
2. In an isolated temporary mutation, swap `cleanup_runner` to call `release_lock` before
   `terminate_and_reap_image_pull`. With causal gates present, the focused test must fail because the live-PID wrapper
   refuses `rmdir` and/or the owner record disappears. Reverse the patch immediately and prove the production script
   has no committed diff.

No sleep, timeout increase, scheduler pressure, or rerun-until-green counts as omission evidence.

## Full gates

```text
git diff --check
task lint
go test -race ./...
go test ./test/contract/...
go build ./...
task schema:generate
git diff -- schemas/ specs/
scripts/run-integration-tests.sh
openspec validate --all --strict --no-interactive
```

Required results:

- all commands green once on the completed candidate;
- schema/spec generation produces no uncommitted drift;
- committed diff remains limited to the test file plus owner-approved proposal evidence;
- the production runner exactly matches pre-test content after forced mutation;
- no new `time.Sleep`, root context, exported API, or public environment variable.

## OpenSpec, ADR, and E2E disposition

- OpenSpec capability delta: none. No production behavior changes.
- ADR: none. No irreversible or cross-repository decision.
- E2E: none. This is a Unix process-contract unit test, not a deployed ingest/graph path.
- Canonical decision skills: none trigger; no production communication, payload, query, or orchestration path changes.
- Durable proposal evidence records inventory, accepted design, review, and gates without claiming a release feature.

## Adopter seam outcome

External runner behavior is unchanged. CI, Task, and developers retain the same commands, lock semantics, refresh
behavior, termination status, and diagnostics. They need to know nothing about helper sentinels, fd 7 or fd 8, private
clock pausing, fake wrappers, or cleanup choreography.

Doing nothing as an adopter follows the same production path. Discovery remains compile/test failure for maintainers;
no new documentation burden is imposed on runner users.

## Rollback

Implementation rollback is a reverse patch limited to `test/testinfra/integration_runner_contract_test.go`. It needs no
data migration, compatibility alias, configuration rollback, or downstream action.

Rollback restores the known false-red risk and is not release acceptance. If the design fails review or validation,
keep the tag blocked and return to inventory/design rather than changing production grace without evidence.

## Explicit owner rulings required

1. Accept or reject Option F: a test-private pause at the existing fake-clock boundary.
2. Confirm the exact live-PID negative `rmdir` mutation remains required evidence.
3. Confirm six inherited descriptors in this one Unix-only case are acceptable test complexity.
4. Confirm production runner, grace budget, Task, CI, and public environment remain unchanged.
5. Confirm both forced-omission proofs are required before review.
6. Confirm no OpenSpec capability delta, ADR, or E2E run is required.
7. After independent `DESIGN REVIEW PASS`, explicitly accept the exact materialized design hash before implementation.
