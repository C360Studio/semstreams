# Final implementation verification

Reviewed code commit: d20c85575a99700e85794ba61fd1e66646582402.
The three reviewed Go source hashes stayed unchanged throughout verification. Evidence packaging and removal of
an extra spec-delta EOF blank line did not change Go code or requirement semantics.

## Completed gate stages

`PATH=/private/tmp/gh1064-task-tools:$PATH task check:push` completed cleanup admission, full lint/build, both
integration/live_llm tagged vet passes, schema generation/drift, contract tests and full unit race tests. It then
stopped at integration lock admission with process exit 201 (nested integration stage exit 1), before executing
that stage's integration tests. This command is not reported as a single successful invocation.

The identified lock owner was PID 66205, Claude's #1426 worktree, with an active canonical integration run.
No lock bypass, resource cleanup or interference occurred. Only the unexecuted stage was resumed:

```bash
SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS=600 PATH=/private/tmp/gh1064-task-tools:$PATH task test:integration
```

The same canonical runner acquired the lock after the other run finished, then completed its full additive unit
and integration suite with `-race -failfast -tags=integration -timeout=20m -count=1 -p 2 ./...`, exit 0.
Graph-index passed in 43.549 seconds. All nine owner-filter distributions were recorded. No test failure was
retried; the sole repeated admission/preflight step followed known host contention. Completed earlier stages were
not repeated just to obtain a single green wrapper invocation.

Exact raw logs are members of execution-logs.zip, with checksums in execution-logs-manifest.json:

- full-push-gate-before-lock.txt — completed early stages and lock-admission refusal.
- full-integration-resumed.txt — bounded lock wait and successful remaining full stage.

Nine focused NATS-free race proofs and controlled failure/mutation evidence are separately recorded in
owner-load-harness-review-correction.md. Independent implementation review approved the source. PBT uses named
controlled schedules for these lifecycle exits; no general scheduler-interleaving or historical-cause proof is made.
The cleanup guard required no new baseline approval. Historical #1421 cause stays open.

## Final-source owner-load observations

These are CI-profile regression observations, not supervised activation evidence or re-derived percentile budgets.

```text
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=predicate-owner reps=5 p50=703.625µs p95=745.916µs p99=745.916µs max=762.083µs submitted=762.083µs,684.709µs,745.916µs,698.625µs,703.625µs
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=predicate-forward reps=5 p50=58.590875ms p95=59.338917ms p99=59.338917ms max=64.294917ms submitted=58.240416ms,58.590875ms,58.462292ms,59.338917ms,64.294917ms
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=name-owner reps=5 p50=911.917µs p95=940.875µs p99=940.875µs max=1.683459ms submitted=1.683459ms,940.875µs,874.042µs,911.917µs,883.75µs
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=name-forward reps=5 p50=72.813ms p95=74.230792ms p99=74.230792ms max=74.49975ms submitted=74.230792ms,74.49975ms,72.611041ms,70.287334ms,72.813ms
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=incoming-owner reps=5 p50=903.834µs p95=950.25µs p99=950.25µs max=969.583µs submitted=969.583µs,950.25µs,903.834µs,796.167µs,871.083µs
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=incoming-forward reps=5 p50=67.656167ms p95=67.667208ms p99=67.667208ms max=68.039709ms submitted=67.656167ms,65.48875ms,65.468667ms,67.667208ms,68.039709ms
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=incoming reps=5 p50=1.507584ms p95=2.336583ms p99=2.336583ms max=3.50925ms submitted=2.336583ms,1.375958ms,3.50925ms,1.507584ms,1.259625ms
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=predicate reps=5 p50=1.893125ms p95=2.6855ms p99=2.6855ms max=3.263583ms submitted=2.6855ms,1.893125ms,3.263583ms,1.528958ms,1.341083ms
test=TestIntegration_OwnerFilterLoadHarness/workers-4 phase=latency filter=name reps=5 p50=1.479458ms p95=1.562041ms p99=1.562041ms max=1.57925ms submitted=1.479458ms,1.403042ms,1.562041ms,1.57925ms,941.833µs
```
