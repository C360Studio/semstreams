# Issue #1292 focused source execution

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
