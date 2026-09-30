# #1222 Task report Writer API checkpoint

Worktree HEAD `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Bounded slice after reviewed R1/R2 snapshot; see accepted
`openspec/changes/e2e-required-check-evidence/implementation-handoff.md` for
the API rationale. Immediate consumer is the Task reporter in `cmd/e2e`.

`TestRun` now retains paired parent/slot identity and observed command/cleanup
exit values. `TestRunConfig` snapshots child expectations and task-status
applicability. `Writer.VerifyChild` reads one report's exact bytes, hashes them,
and verifies complete proof against parent, slot, selection, task-status scope
and exact member set. `WriteMember`/`LoadMember` bind one no-clobber member file
to the initialized aggregate's declaration snapshot; failed partial members
remain readable. `Compare` emits both runs' evidence dispositions.

| File | SHA-256 |
| --- | --- |
| test/e2e/results/writer.go | 497b469f43edc798c21d724f9d24770f39bb13adf2bc3dbf0a0165b5e32573d0 |
| test/e2e/results/writer_task.go | 9cd44e3d888868570d272b7d131479693d228593244191bb093f8d1eb2ac8645 |
| test/e2e/results/writer_test.go | 60262a27fe72f3affe1b60919b59963f5395df4c7aa30dfdf71c8a8f02195fbf |
| test/e2e/results/writer_task_test.go | 2e7f88a12b950115d90688d20665d1a9ee9157c54be6af60c2e9ffaf9248f370 |

Initial TDD RED was missing-symbol compilation, exit 1; retained
`/private/tmp/semstreams-1222-task-api-red.log` SHA-256
`f33d3839be1e00b35e3f2ab6100bbefe88fdfd2cc630397e2a938668e76e6959`.
It is not a behavior-level mutation result. `GOCACHE=/private/tmp/semstreams-1222-go-cache
go test ./test/e2e/results -count=1 -v` exit 0, log
`/private/tmp/semstreams-1222-task-api-green.log` SHA-256
`67f3168a0da1a4291bb4cc9408695878402a309df3b71e0d53e4c674b4cc6f51`.
The same package with `-race` exit 0, log
`/private/tmp/semstreams-1222-task-api-race.log` SHA-256
`58a023ecc27af53ca90145bffb0a2174023ff5d9e6403446becac0f325f21776`.
`git diff --check` on these four files exit 0. Tests include exact child
binding, absent child, terminal status failures, initialized scope stability,
write-once failed member, passing member, forged complete reader omissions,
legacy comparison disposition, and native fuzz seed replay for child/member
decode entry points.

Open: independent Task API review, manifest persistence API shape, actual
reporter/Task producer integration and retained constituent proof, targeted
behavioral mutation, full gate validation. No Docker, integration, commit or
push was run here.
