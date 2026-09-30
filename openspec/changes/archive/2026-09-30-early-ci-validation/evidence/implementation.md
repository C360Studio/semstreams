# Focused implementation evidence

Source base: `b99af72aa43beefa3e1d3958984288f1e15696bf`. This is an ordering change; hosted runtime
is not inferred from the local fixture. Historical hosted timings are separately in `baseline.json`.

Before the source edit, `PATH=/private/tmp/gh1064-task-tools:$PATH go test -count=1 -run '^TestEarly'
./test/testinfra` failed in 1.47s on intended behavior: no dedicated OpenSpec job, aggregate `needs`
excluded it, and both real `check:push` fixture variants reached Lint without invoking OpenSpec.

After the edit, these exact commands passed:

| Command | Result |
| --- | --- |
| `PATH=/private/tmp/gh1064-task-tools:$PATH go test -count=1 -v -run '^(TestEarly.*\|TestCleanupAdmissionUniformWiring\|TestCleanupAdmissionCILintWiring\|TestIntegrationRunner_TaskAndCIConverge)$' ./test/testinfra` | PASS, package 1.172s; `focused.log.txt` |
| `PATH=/private/tmp/gh1064-task-tools:$PATH go test -count=1 -v -run '^TestCleanupAdmissionEntryPoints$/(clean\|new-debt)/^check-push$' ./test/testinfra` | PASS, package 1.386s; real cleanup guard fixture, `cleanup-fixture.log.txt` |
| `PATH=/private/tmp/gh1064-task-tools:$PATH task --silent openspec:validate` | PASS, 60/60 items, 0.546s tool wall time |
| `git diff --check` | PASS |

The actual Task fixture observed invalid OpenSpec refusal before the guard or next command (0.24s), and
valid OpenSpec admission through the existing real cleanup analysis to the next command (0.69s). The
aggregate script passed with six successes and failed for failed, cancelled, skipped, missing and unknown
required results. Independent Build failure also failed it.

Selected mutation proof, with `cp` backup of `.github/workflows/ci.yml` and MD5 before/after each
restoration `03333bc059c55c801d245412ec974908`:

| Temporary fault | Exact intended failure |
| --- | --- |
| Remove `test.needs: [openspec]` | `TestEarlyCIValidationAdmission`: `CI Test must wait for successful validation: needs=[]` |
| Remove OpenSpec result clause from aggregate shell | `TestEarlyCIValidationStatus`: `aggregate does not inspect openspec` |

Both faults were restored byte-for-byte before the passing commands. No integration, E2E, or full
`check:push` suite ran in this implementation slice.

Final source SHA-256:

| File | SHA-256 |
| --- | --- |
| `.github/workflows/ci.yml` | `f0d61130bee76d8b70bccabfadea318d0d2333895b274be356d535ed37ded59c` |
| `Taskfile.yml` | `c59de23d40db92637dc18e187f053611d7fd751a10976f3dbe9de199f30a8658` |
| `taskfiles/openspec.yml` | `de63a2def8995dd50278c2ff381415a21a0049d310a110953655409179d95f1f` |
| `test/testinfra/early_ci_validation_test.go` | `eba056e70c0cf80ace1b3663315be83b87f3339905ac99ec4d0b7c3cb8dba8e8` |
| `test/testinfra/cleanup_admission_test.go` | `8ff1c963d36692da13544f8eec718251cbcfe845c8acb119a71b301554339827` |
