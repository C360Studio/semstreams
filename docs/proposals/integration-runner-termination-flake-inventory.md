# Integration runner termination flake inventory

Status: inventory for independent review

Repository baseline: `35a64ee19ad86f14bd2a1fc6fe0b39984e169a35`

## Observed failure

The full `go test -race ./...` gate failed once in
`TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock`:

```text
test-private rmdir did not refuse live helper: err=<nil>
output=.../rmdir: line 8: 6: Bad file descriptor
```

The exact isolated test subsequently passed 100/100 in 100.666 seconds:

```text
go test -race ./test/testinfra \
  -run '^TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock$' \
  -count=100
```

This failure is distinct from the NATS/Docker symptoms tracked in #736 and #1054. The untagged contract test uses a
fake toolchain and starts no NATS server or Docker container.

## Existing proof surface

- The test installs private `date` and `rmdir` wrappers at
  `test/testinfra/integration_runner_contract_test.go:228-246`.
- Four pipes carry parent readiness, TERM acknowledgement, helper release, and post-reap lock removal at
  `test/testinfra/integration_runner_contract_test.go:247-258`.
- The runner receives those descriptors through `exec.Cmd.ExtraFiles` at
  `test/testinfra/integration_runner_contract_test.go:263-277`.
- After TERM acknowledgement, the test reads the unchanged owner record, checks the helper PID, and launches a separate
  `rmdir` probe at `test/testinfra/integration_runner_contract_test.go:301-324`.
- The helper installs its inherited descriptors and release reader at
  `test/testinfra/integration_runner_contract_test.go:355-383`, then acknowledges TERM and joins the release reader at
  `:391-397`.
- Production cleanup grants a one-second integer-clock TERM grace, then SIGKILLs and exactly waits for a still-running
  child at `scripts/run-integration-tests.sh:135-160`.
- Production releases the token-checked lock only after child cleanup at `scripts/run-integration-tests.sh:106-121`
  and `:227-230`.
- One goroutine owns `Cmd.Wait`; callers join its retained result at
  `test/testinfra/integration_runner_contract_test.go:779-830`.

The missing fact is a test-controlled milestone that prevents production KILL escalation while the test performs its
post-ack assertions. TERM acknowledgement does not grant that stability.

## Measured causal sequence

The observed output supports this sequence:

1. The helper acknowledges TERM. The test still sees the same owner record and a live PID.
2. Runner cleanup concurrently reaches its integer-clock grace deadline. Near a second boundary, the effective grace
   can be only a small fraction of one second.
3. The runner SIGKILLs and exactly waits for the still-blocked helper.
4. The test's independently spawned `rmdir` probe copies `command.Env` but not `command.ExtraFiles`.
5. The wrapper's second `kill -0` sees the helper gone and reaches `printf 'R' >&6`.
6. Descriptor 6 is absent in the independent probe, producing `Bad file descriptor`.
7. The wrapper continues into real `rmdir`. `err=<nil>` means the runner had already removed the owner file and the
   probe won the interval before the runner's own `rmdir`.

This is consistent with correct production ordering: exact child reap, owner-file removal, then directory removal. It
falsifies the test's assumption that the acknowledged helper remains alive until the test explicitly releases it.

Normal release-pipe EOF is not a supported cause: the writer remains open until explicit release or cleanup after the
failed assertion. PID reuse is unsubstantiated and does not explain the exact wrapper path as directly.

## Same-class collision inventory

| Fact | Existing owner and spelling |
|---|---|
| Pull ownership | Bash stores `$!` and validates the exact PID against `jobs -pr` at script lines 125-130 and 168. |
| TERM/KILL policy | Bash owns TERM, bounded grace, job-table recheck, KILL, and exact wait at script lines 135-160. |
| Lock ownership | The token-bearing owner file governs release at script lines 106-121. |
| Cleanup order | Pull termination and reap precede lock release at script lines 227-230. |
| Parent-ready status | The private `date` wrapper writes fd 3; the termination test reads `parentReadyReader`. |
| TERM status | The helper writes fd 4 at test lines 391-394; the termination test reads `termAckReader`. |
| Helper release | The helper reads inherited fd 5; the termination and pre-TERM tests own the corresponding writer. |
| Reap-before-rmdir status | The wrapper checks PID absence and writes fd 6; the test reads `reapAckReader`. |
| Runner completion | One goroutine owns `Cmd.Wait`; callers read the retained result. |

`rg -n 'newCommandWaiter\(' test/testinfra/integration_runner_contract_test.go` finds exactly four owners:

### Termination ordering owner, lines 283-353

Catalog/status: four inherited pipe pairs, owner/PID files, `ProcessState`, and the waiter result. The parent reads
readiness, TERM, and reap signals and writes helper release.

Lifecycle/recovery: cleanup closes release, then `killAndWait` joins the sole waiter. The normal path signals TERM,
probes the lock, releases the helper, joins the runner, reads reap acknowledgement, and proves process/lock absence.

### Pre-TERM helper-release owner, lines 401-465

Catalog/status: two `/dev/null` fd placeholders, one inherited release reader, one parent release writer, PID file,
`ProcessState`, and the waiter result.

Lifecycle/recovery: cleanup closes release and calls `killAndWait`. The normal path closes release before TERM, waits,
then proves the exact helper PID is gone.

### Waiter timeout/reap owner, lines 467-493

Catalog/status: a `sleep` process, its exact PID, timeout error, `ProcessState`, and the retained killed result. It has
no inherited pipes.

Lifecycle/recovery: cleanup calls `killAndWait`. The test observes bounded wait timeout, kills and joins through the
same owner, proves PID absence, then proves repeated wait retains the killed result.

### Host-lock holder owner, lines 515-574

Catalog/status: holder release file, lock owner file, output buffer, derived command context, `ProcessState`, and the
waiter result. It has no inherited pipes.

Lifecycle/recovery: cleanup writes the release file, cancels the command context, then calls `killAndWait`. The test
separately runs a contender and proves bounded lock diagnostics.

Adjacent cases also cover successful pull and pull timeout without `commandWaiter`. The newly failed premise is
specific to the deliberate post-TERM blocked-helper proof, but any correction must preserve the shared one-waiter
semantics and must not disrupt the three other recovery shapes.

## Contract and policy inventory

- Archived accepted proof inventory/design:
  `openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/testinfra-interrupt-inventory.md` and
  `testinfra-interrupt-design.md`.
- That design requires a blocked helper, production-shaped runner, test-private synchronization, no production hook,
  no sleep or timeout increase, and one `Cmd.Wait` owner. It did not reconcile the production KILL escalation with the
  test's post-ack work.
- The single-waiter contract is also recorded at
  `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md:238-241`.
- Testing policy requires bounded causal synchronization and rejects arbitrary sleeps or timeout inflation at
  `docs/contributing/01-testing.md:185-193`.
- Repository and GitHub searches found no current issue or PR for this exact test/signature.

## Consumer and adopter seam

No new exported symbol, production environment variable, port, subject, bucket, payload, or config field is present.
Exact production callers and documentation are:

- CI invokes the runner at `.github/workflows/ci.yml:105-106`.
- Task owns full and focused lanes at `taskfiles/test.yml:14-27`.
- Contributor guidance defines the additive tagged command and host contract at
  `docs/contributing/01-testing.md:41-71`.
- Operations guidance requires the runner for full and focused packages at
  `docs/operations/23-natsclient-test-helpers.md:29-50`.

A developer should know only the runner's documented lock, wait-budget, refresh, and termination behavior. They should
not know about helper PIDs, fd numbers, fake wrappers, grace-window choreography, or `Cmd.Wait` ownership. If they do
nothing, runtime behavior remains TERM, bounded escalation, exact reap, then token-checked lock release.

The prediction gap is test-private: the test predicts that an acknowledged child will remain alive across several
subsequent operations, while the runner owns and observes the actual grace deadline.

## Context and lifecycle audit

- The failing termination test and production shell retain no `context.Context`.
- `commandWaiter` retains only `*exec.Cmd`, a done channel, and the result.
- Exactly one goroutine calls `Cmd.Wait`; cleanup kills only if necessary and joins that same result.
- Helper signal and release goroutines converge before helper return.
- The adjacent host-lock owner creates `context.WithCancel(context.Background())` at test lines 526-527 and feeds it
  to `exec.CommandContext`. This existing test-only root is same-class context debt and is outside the observed failure;
  it is not precedent for a correction.
- No production root context, stored cancel function, context-provider surface, or continuing work is implicated.

## Evidence-derived boundaries

- The failure does not justify changing `scripts/run-integration-tests.sh`, its escalation budget, Task, CI, or public
  runner environment.
- Any correction remains within test synchronization unless separate evidence establishes a production defect.
- Exact PID ownership, token-checked lock release, the one-owner `Cmd.Wait` contract, bounded diagnostics, and no-sleep
  policy remain binding constraints.
- A correction cannot assume scheduler latency or inflate a timeout.
- No outward-facing surface or sister-repository change is indicated.
- The adjacent host-lock test's root context is recorded debt, not authorization to add or retain another root.

## Open design questions

1. Does the proof need both the unchanged-owner assertion and a separately executed negative `rmdir` mutation while the
   helper is alive?
2. Which causal milestone can prove the intended ordering without assuming survival beyond production's grace?
3. Should the private probe inherit the reap-ack descriptor, or should the proof avoid executing the wrapper's
   post-reap path before the runner owns it?
4. Can omission resistance deterministically force the old live-child assumption to fail without sleeps or scheduler
   pressure?

This artifact inventories current truth only. It does not select a fix.
