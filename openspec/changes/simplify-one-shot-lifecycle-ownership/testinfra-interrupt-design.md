# Integration runner interrupt design

Design checkpoint based on accepted inventory SHA-256
`b183233d955680d4f00fcb8749b7a4b09370fa24479f79d476f8427093024737`. This testinfra prerequisite grants zero
lifecycle migration, restart-proof, release, archive, or tag credit.

## Options considered

### A. Do nothing

Zero diff, but PR #1001 remains blocked by a repeated false-red. The test continues to accept its own three-second
timeout as if Bash exited and still does not prove child-before-lock ordering. Reject.

### B. Distinguish waiter timeout only

Make timeout a typed error and require actual runner exit before final-state assertions. This is the smallest textual
change and fixes the misleading diagnostic, but SIGINT/trap progress remains unobserved and final states do not prove
the claimed ordering. Reject as insufficient evidence.

### C. Filesystem child handshake

Have a fake pull acknowledge TERM and block on a release file or FIFO. This can prove the ordering without production
changes, but ordinary files require polling and a FIFO adds platform-specific cleanup. Reject in favor of native
causal signals.

### D. Test-private inherited-pipe handshake

Re-exec the Go test binary as the fake pull and pass test-private inherited pipes for parent readiness, child TERM
acknowledgement, and child release. This adds Unix-only test plumbing in one file but creates exact causal milestones,
uses no sleeps or retry polling, and adds no production/adopter surface. Recommend the narrowed D1 form below.

- **D1, preserve the existing sole waiter:** retain `commandWaiter` as the one `Cmd.Wait` owner required by the
  archived testinfra decision. Add a typed timeout distinction; timeout is always a hard diagnostic failure. Do not
  alter the holder test or manufacture a second Wait. This is the recommended bounded fix.
- **D2, retire the waiter across adjacent tests:** replace all uses with new command ownership. This broadens a
  one-case CI correction into unrelated holder and cleanup behavior and risks violating the archived single-waiter
  contract. Reject for this prerequisite.

### E. Production runner hook

Add a production environment variable, log marker, or readiness hook. This would be easiest for the test to observe
but creates an adopter surface solely for testing when no production cleanup-order defect is evidenced. Reject.

## Recommendation and premises

Adopt D1.

1. PR #1000 run `32184951196`, job `95866323813`, and PR #1001 run `32249598728`, job `96057438186`, failed after
   3.12 and 3.11 seconds. `commandWaiter.wait` has a three-second deadline and the test accepts every non-nil error.
2. `scripts/run-integration-tests.sh:227-230` already orders terminate/reap before lock release, and `:158-161`
   exact-waits the stored PID. No script correction is evidenced.
3. `test/testinfra/integration_runner_contract_test.go:454-470` exposes no child TERM acknowledgement or release
   boundary, so the current test cannot hold the ordering boundary open.
4. The child-written PID marker does not alone prove Bash has retained `$!`. The first post-launch `date` call occurs
   after that assignment, so a test-private fake `date` can publish parent readiness only after it sees the helper PID
   marker.
5. A successful `Process.Signal` is not proof a non-interactive Bash dispatched its trap. The runner already handles
   SIGTERM, which is the stable controlled-termination signal for this deterministic contract test.
6. Actual `Cmd.Wait` completion proves Bash exited after its EXIT trap. A local waiter timeout proves no process fact.
7. `docs/contributing/01-testing.md:185-216,235-241` requires causal ready/done signals and rejects sleeps or timeout
   increases as flake fixes.
8. `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md:235-242` requires exactly one goroutine
   to own `Cmd.Wait`; caller timeout and cleanup must converge on that same completion signal.

## Exact target state

Runtime test-code scope is only `test/testinfra/integration_runner_contract_test.go`. No change is allowed to
`scripts/run-integration-tests.sh`, production environment, Task, CI workflow, exported Go APIs, payloads, subjects,
buckets, production lifecycle counts, or sister repositories.

1. Rename the case to describe the stable signal contract:
   `TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock`.
2. Add a conventional re-exec helper in the test binary, enabled only by a private test environment marker. The
   helper installs `signal.Notify` for `syscall.SIGTERM`, writes the existing fake pull PID marker, acknowledges TERM
   through an inherited pipe, blocks on an inherited release pipe, and exits only after release.
3. In this case only, make fake `docker pull` `exec` that helper. `exec` preserves the exact PID Bash owns. All other
   fake Docker cases remain unchanged.
4. Pass test-private pipes through `exec.Cmd.ExtraFiles` for parent-ready, TERM-ack, child-release, and the exact
   lock-removal reap check. Close every unused parent endpoint immediately after Start and all remaining endpoints in
   cleanup.
5. Add a test-private fake `date` wrapper active only for this case. Once the helper PID marker exists, its first
   post-pull call writes parent-ready, then delegates to the real date. Because the runner calls `date` only after
   retaining `$!` on this path, the byte proves both helper readiness and parent PID ownership without a production
   hook.
6. After parent-ready, signal the runner with `syscall.SIGTERM`. Read the child's TERM-ack byte. While the child is
   deliberately blocked and alive, assert that the exact token-bearing lock owner still exists and the child PID is
   live.
7. Release the child. Wait through the existing sole `commandWaiter`. Require an actual `*exec.ExitError`, exit code
   130, and populated exited `ProcessState`; a typed waiter timeout is a hard failure, never accepted as completion.
8. Add a test-private `rmdir` wrapper active only for this case. At the exact attempt to remove `lockDir`, read the
   exact helper PID and call `kill -0`. A live or zombie child still has a process-table entry, so the wrapper refuses
   removal when that check succeeds. Only after the PID is absent does it acknowledge the reap check through the
   inherited pipe and delegate to the real `rmdir`. This makes final lock absence sensitive to a mutation that moves
   lock removal after child exit but before Bash `wait` reaps it.
9. After actual runner exit, require the reap-check acknowledgement, then assert the child PID and lock directory are
   absent.
10. Early-failure cleanup closes the child-release endpoint first, kills the runner only if needed, and joins through
   the same waiter. It never starts a second `Cmd.Wait`.
11. Keep the existing three-second pipe/wait ceilings only as deadlock diagnostics. Do not increase them. Add no
    `time.Sleep`, Eventually loop, retry, or repeated-CI-rerun requirement.

## Behavior contract

GIVEN the runner holds its token-matching lock, Bash retained the exact pull PID, and the fake pull installed TERM
handling

WHEN the runner receives SIGTERM

THEN cleanup sends TERM to the exact child

AND after the child acknowledges TERM but before it exits, the token-bearing lock still exists

WHEN the test releases the child

THEN the child exits and Bash exact-waits and reaps it

AND the test-private exact `rmdir` boundary refuses lock removal while that PID still exists, including as a zombie

AND only the post-reap PID absence permits lock removal

AND the runner exits 130 through the existing EXIT trap.

## TDD and verification

The two cited CI failures are the preserved regression red. The rewritten causal case must fail before the helper is
wired because it cannot observe parent-ready or TERM acknowledgement; it must never treat timeout as completion.

Green gates:

```text
go test -race ./test/testinfra -run \
  'TestIntegrationRunner_(TerminationReapsPullBeforeReleasingLock|HostLockHasBoundedContentionDiagnostics)|TestCommandWaiter' \
  -count=1
go test -race ./test/testinfra -count=1
git diff --check
```

The policy guard must find no new integration `time.Sleep`. A negative mutation that attempts the exact lock `rmdir`
while the helper PID still exists must be refused, proving the boundary check covers live and zombie process-table
state. Normal PR #1001 CI runs once after the implementation; an unchanged failing test is not rerun as evidence.

## Adopter seam outcome

External behavior is unchanged. Developers and CI invoke the canonical runner exactly as documented; they learn no
PID, inherited FD, helper marker, timeout, or fake-process detail. The test observes framework-owned milestones rather
than asking callers to predict trap completion. No canonical decision skill triggers because this adds no production
communication path, payload, query, or orchestration primitive.
