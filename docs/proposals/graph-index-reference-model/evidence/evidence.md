# Issue #1292 graph-index model sensitivity evidence

The focused unit and mutation results below describe the pre-lint-refactor helper. Current final-byte proof is
recorded in [the lint-refactor refresh](lint-refactor/evidence.md). Existing broker confirmation remains valid:
the extraction changes only a unit-test driver and leaves its production/broker test sources unchanged.

Isolated disposable source copy: `/tmp/semstreams-gh1292-model-mutations.TILVxn`.
Created with `git archive HEAD | tar -x -C <copy>`, then copied only the three new tests from the active worktree.
No mutation was applied to the active worktree. `GOCACHE` was set to `<copy>/gocache` because the sandbox denied
access to the usual host build cache from this copy. `RAPID_*` environment overrides were absent.

## Fixed source and test bytes

| File | SHA-256 | Backup and restore MD5 |
| --- | --- | --- |
| `processor/graph-index/owner_reconcile.go` | `e46c460cb95e8a5c81c3087399d55ee3c8d9c25a8a25ad9502a99eeec047ecf4` | `fed28aeec5ddd71ef78fd4e3fe4218b0` |
| `processor/graph-index/watermark.go` | `876ef3c147367e7cc525690fc3519347fecc11ea63b9abf3c0c308644c45ae75` | `5a102bad1fd58c0fa544d70b72d09333` |
| `processor/graph-index/component.go` | `21bbb25cf02d1bdc3c30b5bae966c99a8ba0abfd14e3d7840dd4f9b6970036cf` | `416491cec8f3a2c617acdbf830deff8c` |
| `processor/graph-index/reconciliation_model_helpers_test.go` | `88b74689b3703094282577e21c893b51666722beb08856033092a52efba6db27` | `f6e0a99fcc817d84d1adbbc3a9a40947` |
| `processor/graph-index/reconciliation_model_test.go` | `69aecde621c73224fd2e3a9638b69c72bb46e1162a63b83e1fcf421c7ad74397` | unchanged in copy |
| `processor/graph-index/reconciliation_prop_test.go` | `c2d5dea64bbc63312637f8fa8f3a83a8efd1af90bef350cbd63ac46ce3e1b0d5` | unchanged in copy |

Before each mutation, `cp <source> <copy>/<basename>.bak` and `md5 -q <source>` were run. The source was restored
with `cp <backup> <source>` and `md5 -q <source>` matched the value above. The final six SHA-256 values in the copy
match the active worktree. Production sources are exactly the original archive bytes.

## Exact fixed command

From the copy, each baseline/mutant/restored run used:

```bash
GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache \
  go test ./processor/graph-index \
  -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v \
  -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s -rapid.nofailfile
```

The stale-row replay and both synthetic-oracle runs narrowed `-run` to
`^TestPropGraphIndexReconciliation$`; all other flags and seed were identical. Exit status 0 indicates pass;
exit status 1 indicates a compiling mutant caught by the intended
assertion, with no compile error in the log. The `-rapid.nofailfile` flag prevents synthetic evidence from leaving
a replay file in the repository.

| Mutation | Exact patch | Mutant log (exit 1) | Intended assertion | Restored log (exit 0) |
| --- | --- | --- | --- | --- |
| Omit stale-row Delete | `mutant-stale.patch` | `final-mutant-stale.log` | A-to-B predicate A survivor; property `history=[]` | `final-restored-stale.log` |
| Latch on enumeration alone | `mutant-bootstrap.patch` | `final-mutant-bootstrap.log` | cold before work proceeds with `BootstrapComplete=true`, indexed 0/target 2; property `history=[]` | `final-restored-bootstrap.log` |
| Suppress required-write failure mark | `mutant-failure.patch` | `final-mutant-failure.log` | persistent Put failure leaves `State=ready`, failed count 0 at revision 12; property `history=[]` | `final-restored-failure.log` |
| Complete older work through observed high | `mutant-watermark.patch` | `final-mutant-watermark.log` | older work drains newest pending revision, indexed 10 instead of 9; property `history=[]` | `final-restored-watermark.log` |

The shared pre-mutation baseline is `final-baseline.log` (exit 0). A second same-seed run of the stale-row mutant
(`final-replay-stale.log`, exit 1) reproduced the same intended A-to-B predicate assertion and zero-action suffix;
`final-restored-replay.log` then passed after checksum restoration.

## Rapid shrink and replay across the synctest boundary

To exercise actual generated-action shrinking, a separate synthetic **test-only** oracle mismatch was injected
after a nonempty suffix. It flips one expected literal fact without altering authority. Its exact patch is
`mutant-synthetic-oracle.patch`; no production file changed. `final-mutant-synthetic-oracle.log` (exit 1) shows the
driver error reaching Rapid outside the bubble and a one-action shrunk history (`suffixLength: 1`, clear entity 0).
`final-replay-synthetic-oracle.log` (exit 1) has the same action and intended predicate mismatch at seed 1292.
`final-restored-synthetic-oracle.log` (exit 0) passed after the helper's MD5 matched its backup.

Earlier superseded-test-byte runs (`baseline.log`, `mutant-stale.log`, `restored-stale.log`) are retained for audit,
but only the `final-*` logs above support the final test bytes.
