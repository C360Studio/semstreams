# #1397 runner test implementation evidence

Claim baseline: `234200f3` (`codex/gh1397-runner-synchronization`). These commands were run in the claim worktree
on 2026-09-28. `GOCACHE=/private/tmp/semstreams-gh1397-gocache` kept Go build writes out of the managed worktree.
The final commit SHA and hosted gates belong in the PR record after review. The working source checksum after the
probe correction is `md5 -q test/testinfra/integration_runner_contract_test.go` →
`87186d432ecdb43e0462b87b9974aea7`.

## Requirement and check choice

The accepted [repair plan](../repair-plan.md) requires the real runner test to preserve the exact token-bearing lock
until its owned pull child is terminated and reaped, while healthy scheduling delay succeeds and missing progress or
early child exit fails with a bounded phase diagnostic. A premature `release_lock`, omitted termination, a live child
holding pipe output, or a silent helper are concrete violations. The expected lock token, PID liveness and process exit
come from the runner contract and OS process state, independently of the observer implementation.

PBT decision: named examples sufficient. The relevant histories are the healthy delayed rendezvous, child exit before
the first signal, live child without a first signal, live-parent EOF, early parent exit with a descendant retaining a
signal writer, buffered final acknowledgement after exit, retained stdout, parent exit before helper cleanup, and
an unrelated live process named by a stale PID file. These are specific controlled orderings, not an input grammar
or a general state-machine law. The examples exercise the real
runner script and fake toolchain; they do not establish arbitrary process-tree behavior or production rule shutdown.

The 35s whole-fixture containment bound is based on the fixture's existing 30s pull watchdog, the runner's 1s
termination grace, and 4s reserved for shell scheduling. It is a failure ceiling, not a performance threshold.
Cleanup separately allows at most 6s: 2s to release and join the runner, 2s after forced parent kill, and 2s to
observe helper absence through the fixture-owned release gate. No PID-file lookup authorizes signaling the helper.
Measured under `-race`, the old healthy
fixture took 1.37s. The controlled 3.2s healthy delay took 4.26s during development and 4.75s in the focused final
case. This adds about 3.2s to this one existing contract test; it does not add a Docker container. Host contention
outside these observations remains unmeasured.

## RED and GREEN

1. Before implementation, setting `SEMSTREAMS_TEST_READY_DELAY_SECONDS=3.2` in the existing fake date wrapper and
   retaining its four 3s waits gave:

   ```bash
   GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock$' -count=1 -v
   ```

   → FAIL in 3.54s: `wait for parent retained pull PID: read |0: i/o timeout`.
2. A focused run of the changed tests before the final owned-PID observer addition:

   ```bash
   GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestIntegrationRunner_(TerminationReapsPullBeforeReleasingLock|ReadySignalFailuresAreDiagnosticAndReaped)$|^TestCommandWaiter_(TimeoutCleanupKillsAndReapsThroughOneOwner|RetainedOutputReportsIncompleteCleanup)$' -count=1 -v
   ```

   → PASS in 10.545s. Healthy delayed 4.75s; early exit 0.55s; missing signal with injected 300ms observer
   deadline and cleanup 1.96s; retained output 2.01s. The early-exit diagnostic includes the phase and EOF or exit;
   the test separately confirms the runner's exit status;
   the missing-signal diagnostic includes phase, elapsed, deadline, runner PID and last read error.
3. Before independent review, after restoring the runner script and adding the owned-PID observer,
   `GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -count=1`
   → PASS, package 27.337s. `git diff --check` → PASS.

The runner fixture uses file-backed stdout/stderr, so `Cmd.Wait` does not wait for a copy goroutine when a descendant
retains those descriptors. `commandWaiter.killAndWait` now returns a bounded incomplete-cleanup error if `Wait` does
not finish after kill. A controlled child holding stdout proved this error, then releasing that child let the sole
waiter finish. Cleanup checks `ProcessState` only after `waiter.done` closes and reports a PID-file read failure rather
than claiming child absence.

## First independent review correction (historical)

[Implementation review 1](../implementation-review-1.md) requested three HIGH corrections. Each was reproduced and
repaired in this same test file:

1. `TestObserveRunnerSignal_TerminalPaths` was RED under the first observer: live EOF and exited parent with a
   descendant retaining the writer each failed their 1s containment guard; buffered final acknowledgement passed.
   The corrected observer returns on EOF immediately, closes and joins the pending reader on early parent exit, and
   allows only the final acknowledgement to consume a buffered byte after exit. The exact focused race command was

   ```bash
   GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestObserveRunnerSignal_TerminalPaths$' -count=1 -v -timeout=20s
   ```

   → PASS in 0.03s for all three subtests. Negative results arrived while their controlled process or writer was
   still held; cleanup released it afterward.
2. Every `killAndWait` caller now reports an incomplete `Wait`; the existing killed-command assertion rejects a
   timeout before reading `ProcessState`. The retained-output control still observes a real incomplete join, then
   releases the child and observes the same sole waiter finish under `-race`. A deliberate nonzero killed-process
   exit remains distinct from an unfinished Wait.
3. The first correction released gates, joined the runner, observed helper absence, then attempted PID-file-based
   kill if needed. A synthetic parent-exit control demonstrated absence inside cleanup. The exact command was

   ```bash
   GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestRunnerFixtureCleanup_EscalatesHeldHelperAfterParentExit$' -count=1 -v
   ```

   → PASS in 2.04s. Second review rejected the PID-file kill as unsafe; the current implementation below supersedes
   this step and removes that test.

The post-correction focused command was:

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestIntegrationRunner_(TerminationReapsPullBeforeReleasingLock|ReadySignalFailuresAreDiagnosticAndReaped|HostLockHasBoundedContentionDiagnostics)$|^TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits$|^TestObserveRunnerSignal_TerminalPaths$|^TestRunnerFixtureCleanup_EscalatesHeldHelperAfterParentExit$|^TestCommandWaiter_' -count=1 -v -timeout=60s
```

It selected
`TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock`,
`TestIntegrationRunner_ReadySignalFailuresAreDiagnosticAndReaped`,
`TestIntegrationRunner_HostLockHasBoundedContentionDiagnostics`,
`TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits`, `TestObserveRunnerSignal_TerminalPaths`,
`TestRunnerFixtureCleanup_EscalatesHeldHelperAfterParentExit`, and all `TestCommandWaiter_` tests.
The first-correction replay after releasing the observed process handle passed in 15.132s. The delayed healthy case
took 4.84s,
early child exit 0.48s, missing signal 2.05s, terminal controls 0.04s, held-helper escalation 2.07s, and retained-output
join 2.01s. `git diff --check` passed. This is historical evidence before the second correction.

## Second independent review correction (current)

The terminal controls now use only their 5s context as the failure boundary. Each calls the observer synchronously,
checks that the context was not exhausted, and confirms its controlled parent or descendant is still held when the
negative result arrives. A test-scoped output-file helper reduced the main fixture below Revive's 80-statement
limit; `readRunnerSignal` now accepts context first.

`cleanupRunnerAndPull` closes only fixture-owned release gates, joins the runner, and observes the recorded helper
PID absent for 2s. A still-live PID produces `ownership unproven, refusing to signal`. The stale-PID control records
an unrelated, deliberately held process in the PID file; cleanup returns that refusal while the process remains
alive, and its actual test owner releases and joins it afterward. The early-parent-exit control separately calls
cleanup before returning and proves its cooperative helper is absent inside cleanup.

The final focused race command was:

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache go test -race ./test/testinfra -run '^TestIntegrationRunner_(TerminationReapsPullBeforeReleasingLock|ReadySignalFailuresAreDiagnosticAndReaped|HostLockHasBoundedContentionDiagnostics)$|^TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits$|^TestObserveRunnerSignal_TerminalPaths$|^TestRunnerFixtureCleanup_RefusesUnprovenPID$|^TestCommandWaiter_' -count=1 -v -timeout=60s
```

It passed in 14.414s: delayed healthy 4.10s, early child exit 0.58s, missing signal 1.98s, terminal controls
0.04s, stale-PID refusal 2.03s, retained-output join 2.01s, and lock-holder control 1.32s.

`GOCACHE=/private/tmp/semstreams-gh1397-gocache GOTMPDIR=/private/tmp task lint:default` passed (exit 0), including
vet, fmt, Revive, fixed-port guard, NATS KV pin checks and the request guard test. This resolved the two observed
`task check:push` lint failures; root owns the later full gate. `git diff --check` passed. The runner script has no
persistent diff and retains MD5 `ef0dde707bec1ab6a39d5ad7293896fe`.

Automatic approval review rejected a proposed mutation that would temporarily reintroduce `Process.Kill` on the
ownership-unproven PID. The stated reason was unacceptable risk of terminating an unrelated process, and it forbade
an indirect workaround. The patch was not applied or run. A later non-signaling diagnostic mutation was restored
without execution when the coordinator stopped further experiments; restoration matched the test-file checksum above.
The current negative control is observed behavior, not mutation sensitivity to the prohibited PID-kill fault.
That mutation criterion remains an explicit deferral for independent reviewer judgment.

## Fixture-owned rmdir probe containment correction

The main termination fixture's private `rmdir` probe now uses `exec.CommandContext(fixtureCtx, ...)` and a dedicated
file-backed output in its own `t.TempDir()`. It still runs the same fake `rmdir` and checks exit 73 plus the live-helper
refusal. A controlled fake probe using the same constructor writes a readiness byte, remains held on a private pipe,
and then must join with observed process state after fixture-context cancellation. Its owner uses the existing
`commandWaiter` and releases its hold in cleanup. The controlled shell proves cancellation of this constructed
command path, while the main test proves the refusal contract; it does not simulate an uncooperative descendant of
the probe.

The final focused replay was:

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache GOTMPDIR=/private/tmp go test -race ./test/testinfra -run 'TestIntegrationRunner_(TerminationReapsPullBeforeReleasingLock|RmdirProbeHonorsFixtureCancellation)$' -count=1 -v
```

→ PASS, main fixture 4.00s, canceled probe 0.17s, package 5.479s. After a synchronous `Start` and ownership handoff
to `commandWaiter`, the probe control closes inherited parent descriptors, observes readiness through the existing
signal observer, cancels its context while the child is still held, and waits up to 2s for the terminal join before
reading process state. The earlier goroutine-based control exposed an inherited-descriptor/`Start` race under
`-race` and was replaced; it is not the final control.

For one controlled mutation, `cp` backed up the test file and the pre-mutation MD5 was
`87186d432ecdb43e0462b87b9974aea7`. Replacing only `exec.CommandContext(ctx, binary, lockDir)` with
`exec.Command(binary, lockDir)` in `newRunnerRmdirProbe` made

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache GOTMPDIR=/private/tmp go test -race ./test/testinfra -run '^TestIntegrationRunner_RmdirProbeHonorsFixtureCancellation$' -count=1 -v
```

→ FAIL in 2.15s: `canceled rmdir probe did not join within terminal bound: command did not exit within 2s`.
The test-owned hold gate was released during cleanup, and no unfinished waiter was reported. `cp` restored the
backup; MD5 matched `87186d432ecdb43e0462b87b9974aea7`. No runner-script mutation accompanied this check.

Final corrected-state checks:

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache GOTMPDIR=/private/tmp go test -race ./test/testinfra -count=1
GOCACHE=/private/tmp/semstreams-gh1397-gocache GOTMPDIR=/private/tmp task lint:default
git diff --check
```

All passed (exit 0): testinfra reported package 27.409s; lint included vet, fmt, Revive, fixed-port and request
guards, and the NATS KV pin checks. The runner script remains at MD5 `ef0dde707bec1ab6a39d5ad7293896fe` with no
persistent diff. The PR owner retains the interrupted full gate, integration runner, and hosted CI checks.

## Controlled mutation sensitivity

Each experiment changed only `scripts/run-integration-tests.sh` in this worktree, after
`cp scripts/run-integration-tests.sh /private/tmp/semstreams-gh1397-runner-script.bak`. Before and after **each**
experiment, `md5 -q scripts/run-integration-tests.sh` was
`ef0dde707bec1ab6a39d5ad7293896fe`. Restoration used `cp` from the backup, never Git discard commands.
The selected check was the real-script delayed healthy test under `-race`, with `-timeout=45s` for experiment
containment.

1. Move `release_lock` before `terminate_and_reap_image_pull` in `cleanup_runner`: FAIL in 4.15s, owner file
   missing at the blocked-child assertion. The exact lock check detects premature release.
2. Delete `wait "$owned_pid"` in `terminate_and_reap_image_pull`: SURVIVED in 4.45s. An explicit shell `wait`
   line is not itself the observable contract. Bash may have reaped the completed child by another path; that
   explanation is an inference, not a measured process trace. No structural assertion was added.
3. Return immediately from `terminate_and_reap_image_pull`: FAIL in 3.71s with `runner exited while owned pull
   helper 29233 was still alive`; cleanup also reported the live helper. This detects loss of child termination
   before lock release. A later `kill -0 29233` returned no such process after fixture cleanup.

The semantic return-early mutant was rerun after the observer and cleanup correction because its assertion path
changed. The same real-script test failed in 4.70s: `runner exited while owned pull helper 39745 was still alive`
at the TERM acknowledgement. Fixture cleanup established helper absence without an unresolved cleanup error. The
script was restored from the backup and checksum again matched `ef0dde707bec1ab6a39d5ad7293896fe`. The lock-order
mutation and explicit-wait survivor did not traverse the corrected path, so they were not repeated.

Limits: the runner-script mutations establish sensitivity to named order and live-child faults at the recorded source
state. They do not prove every shell implementation of reaping, the prohibited PID-kill mutation, or the distinct
#1283 production shutdown fix. The final full integration runner, repository-wide race gate and hosted CI remain
with the PR owner after implementation review.
