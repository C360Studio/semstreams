# Integration runner interrupt inventory

Inventory-only checkpoint for the repeated CI failure
`TestIntegrationRunner_InterruptReapsPullBeforeReleasingLock`. This artifact grants zero lifecycle migration,
restart-proof, release, archive, or tag credit.

Repository baseline: `6d9d754af2f13d0f09145ed34ce81f3d8b013885`.

## Claimed gap and ownership surface

- `scripts/run-integration-tests.sh:39-42` retains whether the lock is held, the pull output path, the exact
  background pull PID, and the timeout flag.
- `scripts/run-integration-tests.sh:106-123` releases only a token-matching lock.
- `scripts/run-integration-tests.sh:125-133` uses the Bash job table and exact stored PID as child-ownership
  evidence.
- `scripts/run-integration-tests.sh:135-162` sends TERM and, when needed, KILL to that exact PID, then waits for
  that PID before clearing it.
- `scripts/run-integration-tests.sh:164-189` starts `docker pull` asynchronously, retains `$!`, and waits for the
  exact child on normal completion.
- `scripts/run-integration-tests.sh:227-233` is the asserted ordering boundary: EXIT cleanup terminates and reaps
  the pull before releasing the lock; INT and TERM request exit 130 and therefore the EXIT trap.
- `test/testinfra/integration_runner_contract_test.go:202-240` starts the runner, observes the child PID file and
  lock owner file, signals the runner, calls a bounded waiter, then asserts the final lock and child states.
- `.github/workflows/ci.yml:91-106` invokes the runner directly. `taskfiles/test.yml:14-27` converges Task callers
  through the same runner.

## Current spellings of the observed facts

- `test/testinfra/integration_runner_contract_test.go:454-472` makes fake `docker pull` write its shell PID and
  `exec /bin/sleep 30`. `exec` preserves the PID Bash recorded, but the child exposes no TERM acknowledgement and
  cannot hold the cleanup boundary open for an ordering assertion.
- `test/testinfra/integration_runner_contract_test.go:511-528` polls files with a deadline. The child writes its
  PID marker; the runner writes its lock-owner marker. Neither marker reports trap dispatch, TERM receipt, child
  reap, or runner exit.
- `test/testinfra/integration_runner_contract_test.go:530-574` gives one goroutine sole `exec.Cmd.Wait` ownership,
  but `commandWaiter.wait` returns one `error` channel for two different facts: completed `Cmd.Wait`, or a locally
  manufactured timeout. `killAndWait` can later kill and rejoin the same command and return the retained Wait
  result.
- `test/testinfra/integration_runner_contract_test.go:233-235` accepts every non-nil waiter error as the expected
  interrupted exit. It therefore treats the exact three-second timeout as proof that Bash exited, then inspects the
  lock while the runner can still be alive.
- PR #1000 run `32184951196`, job `95866323813`, failed this assertion in 3.12 seconds. PR #1001 run
  `32249598728`, job `96057438186`, failed it in 3.11 seconds. Both durations match the three-second waiter deadline.
  Local repeated success is compatibility evidence only; it does not close this branch.
- The `os/exec.Cmd.Wait` contract establishes process exit and completion of configured I/O copies before it
  returns and populates `ProcessState`. A waiter timeout establishes none of those facts.
- `test/testinfra/integration_runner_contract_test.go:615-620` checks final PID absence with `kill -0`. Together
  with final lock absence, that proves only two final states, not that the lock remained held while the child was
  unreaped.
- Adjacent final-state coverage exists at `test/testinfra/integration_runner_contract_test.go:147-200`; the
  waiter timeout/rejoin behavior is asserted at `:242-267`; holder cleanup uses the same waiter at `:289-349`.

## Signal and process facts

- Non-interactive Bash without job control gives asynchronous commands distinct SIGINT/SIGQUIT behavior; the
  background fake pull does not provide the runner's interrupt acknowledgement.
- Bash may defer a trapped signal while it waits for a foreground command. A successful
  `Process.Signal(os.Interrupt)` proves kernel delivery was requested, not that Bash dispatched the trap or exited.
- Signals ignored on shell entry cannot be reset by a trap. Current test evidence does not report the inherited
  disposition, so inherited SIGINT behavior is plausible but unproven.
- None of those unknowns changes the proven defect: timeout and process exit are conflated by the test.
- No evidence shows the production runner violated its terminate/reap-before-release order.

## Adjacent normative claims

- `docs/contributing/01-testing.md:172-180,185-241,375-385` requires single-owner bounded cleanup, causal
  synchronization before polling, and rejects arbitrary sleeps or timeout increases as flake fixes.
- `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md:235-242` already requires exactly one
  goroutine to own `Cmd.Wait`, permits a caller timeout without starting a second waiter, and requires cleanup to kill
  and join through that same completion signal. That accepted test-helper contract constrains this correction; it is
  not precedent for production lifecycle rejoin or result replay.
- `openspec/changes/simplify-one-shot-lifecycle-ownership/recovery-ledger.md:559-572` requires explicit
  synchronization and rejects manufactured rejoin/result replay. This prerequisite earns zero lifecycle credit.
- `rg -n 'run-integration-tests\.sh|integration runner|image_pull|commandWaiter|InterruptReapsPullBeforeReleasingLock'
  openspec/specs docs/adr openspec/changes` found no current capability or ADR owner. Apart from this active inventory
  and ledger, it found archived pass evidence plus the archived single-waiter decision above.
- `rg -n 'GO_WANT_HELPER_PROCESS|ExtraFiles|commandWaiter|waitCommand|assertProcessGone|Process\.Signal' -- '*.go'`
  found no existing helper-process or inherited-file pattern. The command waiter and process assertions are confined
  to `test/testinfra/integration_runner_contract_test.go`.

## Same-class collision table

| Dimension | Existing evidence |
|---|---|
| Semantic class | Cross-process child cleanup and lock-release ordering. |
| Owners | Bash owns pull PID/reap and lock; the Go test owns runner `Cmd.Wait`/kill; fake Docker owns child behavior. |
| Catalog/status | Lock-owner file, child PID file, call log, and output buffer; no TERM-ack or reap-order status. |
| Lifecycle | Bash EXIT trap is authoritative child-wait then lock-release order; Go waiter adds timeout/kill/rejoin; fake child exits on TERM. |
| Readers/writers | Test reads PID and lock; child writes PID; runner writes/removes lock; waiter publishes retained error and done. |
| Recovery | Test cleanup can SIGKILL Bash after timeout, bypassing EXIT cleanup; no exact child-release handshake exists. |
| Collision | The test's timeout is treated as Bash completion and overrides the actual `Cmd.Wait` boundary. |

## Adopter seam inventory

- Exact current callers from `rg -n 'scripts/run-integration-tests\.sh' .github taskfiles docs` are CI at
  `.github/workflows/ci.yml:106`, Task at `taskfiles/test.yml:17,22,27`, and documented direct developer use at
  `docs/contributing/01-testing.md:50` and `docs/operations/23-natsclient-test-helpers.md:32`.
- A developer invoking the canonical runner should know only the documented lock contention, bounded wait, and
  optional refresh behavior. INT or TERM should clean the exact child before releasing the lock.
- Doing nothing follows the same runner path under one lock. This false-red test blocks CI and releases but does not
  change runtime behavior.
- Discovery remains runner logs and testing documentation. Signal inheritance, child PIDs, and test synchronization
  must not become adopter knowledge.
- The adopter should know nothing about fake helper pipes/files or cleanup sequencing internals. Any correction must
  remain test-private and add no production environment knob or exported Go surface.
- The present test asks the caller to predict that a successful signal request plus three seconds means Bash exited.
  The test must instead observe exact milestones.
- The inventory proposes no exported symbol, production config, subject, bucket, or payload. There is therefore no
  new external consumer at birth; any synchronization primitive belongs solely to this contract test.

## Open evidence questions for design

- Whether exact cross-process TERM acknowledgement plus explicit child release is required to prove ordering, or
  whether typed Wait completion alone covers the intended contract.
- Whether an inherited test-private pipe or a filesystem handshake is the smaller causal substrate.
- Whether the manufactured timeout/rejoin/result-replay behavior should be removed from all `commandWaiter` uses or
  narrowed only at the failing assertion.
