# Issue #1292 lint refactor and refreshed proof evidence

Candidate HEAD before the uncommitted helper extraction: `5c26519dda66ab8db3c49ccdc94be01054299447`.
The only source change is `processor/graph-index/reconciliation_model_helpers_test.go`: mandatory activation moved
from `runModelHistory` into `runModelPrefix`. Production sources, the named tests, and the property test are unchanged.
No test semantics, action bounds, or production timing were changed. The reviewer separately approved the extraction.

## Final source identity

| File | SHA-256 |
| --- | --- |
| `processor/graph-index/reconciliation_model_helpers_test.go` | `bbfdf4f859ab0a7ec4eebc5de97d6373e84e1a0e4e3a8b5e48fbe8c05bf11528` |
| `processor/graph-index/reconciliation_model_test.go` | `69aecde621c73224fd2e3a9638b69c72bb46e1162a63b83e1fcf421c7ad74397` |
| `processor/graph-index/reconciliation_prop_test.go` | `c2d5dea64bbc63312637f8fa8f3a83a8efd1af90bef350cbd63ac46ce3e1b0d5` |

`final-copy-hashes.txt` also records the three unchanged production source SHA-256 values and binds the six checked
files in the disposable copy after restoration to the final candidate bytes. `checksums.txt` records before/after MD5
per mutation.
No `RAPID_*` environment override was present (`env | rg '^RAPID_'` returned no match).

## Source-focused checks

The host denied the usual Go build cache path, so these commands used the writable cache below. The initial bare
`task lint` failed before vet on that sandbox access; rerunning with `GOCACHE` passed. This is an environment issue,
not a revive/test failure.

```bash
GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache task lint
task spec:properties
git diff --check
```

`lint.log`: exit 0, including vet, fmt, revive, fixed-port and SDK guards. `spec-properties.log`: exit 0 with all
404/404 property citations resolved. `git diff --check`: exit 0.

Each source-focused race command below exited 0, and all four named tests passed. The process timeout is a safety
bound; the real elapsed top-level property times in the logs are the five-second budget evidence.

```bash
GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache go test ./processor/graph-index \
  -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v \
  -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s
```

`source-seed1292.log`: 100 valid checks, property 2.31s; activation cold 100, replacement 100, ownership 100,
stale 173, duplicate 196, failure 136, repair 136, hydration 149.

```bash
GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache go test ./processor/graph-index \
  -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v \
  -rapid.checks=100 -rapid.seed=1293 -rapid.shrinktime=3s
```

`source-seed1293.log`: 100 valid checks, property 2.24s; activation cold 100, replacement 100, ownership 100,
stale 158, duplicate 201, failure 139, repair 139, hydration 141.

```bash
GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache go test ./processor/graph-index \
  -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v -rapid.seed=1292
```

`source-default100.log`: no check-count override; Rapid and driver both report 100 checks, property 2.33s.

## Isolated mutation run

`copy/` was created by `git archive HEAD`, then the three final test files were copied from the active worktree.
The script `run_mutations.sh` gives the exact replacement text and commands. It ran one mutation at a time under
`-race`, fixed seed 1292, 100 checks, `-rapid.nofailfile`; each mutation used `cp` backup, pre/post MD5, a compiling
mutant that failed an intended assertion, and a restored 100-check pass. `copy-baseline.log` passed before mutations.

| Mutation | Patch | Mutant assertion | Restored log |
| --- | --- | --- | --- |
| Omit stale-row Delete | `stale.patch` | `stale.mutant.log`: A-to-B predicate A survives | `stale.restored.log` |
| Latch on enumeration alone | `bootstrap.patch` | `bootstrap.mutant.log`: cold 0/2 gate proceeds | `bootstrap.restored.log` |
| Suppress failed-write mark | `failure.patch` | `failure.mutant.log`: Ready/failed=0 after persistent Put | `failure.restored.log` |
| Complete older work through observed high | `watermark.patch` | `watermark.mutant.log`: indexed 10 while new revision remains pending | `watermark.restored.log` |

The separate test-only model mismatch `synthetic.patch` returned an actual semantic error from inside the bubble.
`synthetic.mutant.log` shows Rapid reducing it to a one-action `modelClear` suffix; `synthetic.replay.log` reproduces
the same action and assertion at seed 1292. `synthetic.restored.log` and `synthetic.replay-restored.log` both pass
after checksum restoration. This proves the error reaches Rapid outside the bubble and can shrink/replay.

## Updated line pins

`reconciliation_model_helpers_test.go:608` begins the mandatory prefix; its activation evidence is at
`:639`, `:648`, `:655`, `:680`, `:685`, `:689-690`, and `:697`. `runModelHistory` now begins at `:701`, calls the
prefix at `:707`, drives bounded generated suffix actions at `:716`, and requires activated observations at `:773`.
The independent oracle (`:52-58`, `:108`), production projection seam (`:316-325`), exact query comparison
(`:402-449`), stale watermark boundary (`:392-399`), fault/repair (`:506-538`), and hydration (`:541-603`)
retained their earlier pins. The bounded generator and Rapid/synctest boundary remain
`reconciliation_prop_test.go:34-129`; named witnesses remain `reconciliation_model_test.go:17`, `:40`, `:82`, `:114`.
