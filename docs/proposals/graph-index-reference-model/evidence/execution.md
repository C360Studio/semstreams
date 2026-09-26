# Issue #1292 focused source execution

The focused unit and mutation results below describe the pre-lint-refactor helper. Current final-byte proof is
recorded in [the lint-refactor refresh](lint-refactor/evidence.md). Existing broker confirmation remains valid:
the extraction changes only a unit-test driver and leaves its production/broker test sources unchanged.

Candidate HEAD at execution: `a1307297b600cfc763761aeca5450e858297487f`.
Executed from `/Users/coby/Code/c360/semstreams-wt/codex/gh1292-graph-index-properties` against these test SHA-256 values:

| Test file | SHA-256 |
| --- | --- |
| `reconciliation_model_helpers_test.go` | `88b74689b3703094282577e21c893b51666722beb08856033092a52efba6db27` |
| `reconciliation_model_test.go` | `69aecde621c73224fd2e3a9638b69c72bb46e1162a63b83e1fcf421c7ad74397` |
| `reconciliation_prop_test.go` | `c2d5dea64bbc63312637f8fa8f3a83a8efd1af90bef350cbd63ac46ce3e1b0d5` |

`env | rg '^RAPID_'` returned no matching environment variables. Each command exited 0; full redirected output is
in the named log. `-timeout=120s` is a process safety bound, while the observed per-test real time below is the
five-second unit budget evidence.

## Explicit seed 1292

```bash
go test ./processor/graph-index -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s \
  > /tmp/semstreams-gh1292-model-mutations.TILVxn/source-seed1292.log 2>&1
```

`source-seed1292.log`: 100 valid property checks in 2.29s; activation cold 100, replacement 100, ownership 100,
stale 173, duplicate 196, failure 136, repair 136, hydration 149. All four named tests passed; longest named
test 0.03s.

## Explicit seed 1293

```bash
go test ./processor/graph-index -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v -rapid.checks=100 -rapid.seed=1293 -rapid.shrinktime=3s \
  > /tmp/semstreams-gh1292-model-mutations.TILVxn/source-seed1293.log 2>&1
```

`source-seed1293.log`: 100 valid property checks in 2.25s; activation cold 100, replacement 100, ownership 100,
stale 158, duplicate 201, failure 139, repair 139, hydration 141. All four named tests passed; longest named
test 0.02s.

## Normal default check count

```bash
go test ./processor/graph-index -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v -rapid.seed=1292 \
  > /tmp/semstreams-gh1292-model-mutations.TILVxn/source-default100.log 2>&1
```

`source-default100.log`: Rapid reported its normal 100 checks and the driver counted 100 valid checks, property
time 2.40s; all named tests passed. This command supplied no `-rapid.checks` override and used no short mode.

`task spec:properties` passed 404/404 citations; `git diff --check` exited 0. Mutation and shrink evidence is
indexed separately in `evidence.md` in this directory.

## Existing real-NATS confirmation

Code candidate `684f2ead7574a5c3c10b7fab5f2800148798236d` includes #1395 and preserves all reviewed source/test hashes.
The existing broker witnesses ran through the canonical host-locked runner:

```bash
scripts/run-integration-tests.sh -v \
  -run '^TestIntegration_Replacement(WatcherWatermarkPublicParityAndRestart|PartialFailureWithholdsUntilOrderedRepair)$' \
  ./processor/graph-index
```

`broker-confirmation.log` records both tests executing and passing: replacement/watermark/public-query parity with
a fresh owner (0.82s), and partial-write failure/refusal/ordered repair (0.51s). The package passed in 3.452s,
including race instrumentation; runner exit was 0. Its containers were removed during cleanup.

This confirms the existing broker assembly witnesses alongside the generated unit model. It does not turn the
unit model into generated broker histories or establish abrupt process/broker-crash recovery. The short run
briefly overlapped a newly started E2E run; no isolated performance claim is made. The full model gate remains
held until competing heavy local work ends.
