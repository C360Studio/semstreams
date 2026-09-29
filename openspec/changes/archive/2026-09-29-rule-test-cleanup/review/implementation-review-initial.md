# Implementation review — #1428 / #1429

Status: preliminary source/proof review; final baseline and execution packet pending.
Mode: implementation review. Read-only; no tests, Docker, repository edits or commits.

## Exact review identity

HEAD `0d888375e332ac915ba55af50668cd6c0cad699b` plus the 16 files recorded in `/private/tmp/gh1428-implementation-source-snapshot.json`, SHA256 `082da8cf8e57404fa8602639be1b400a979378b20ff70b65211592f39eeba62c`. All 16 current source hashes independently matched the manifest. Accepted design SHA256 is `c2206a3471e0fc5c3f21f966422856b4ee1adc7fa9b02b0352f361ac9cb82e78`.

Earlier wiring review and correction re-review apply to the matching files. New review covers the three proof files and the revision helper's setup-fault injection. No inventory sweep was repeated. The two earlier external context findings remain closed at the identical corrected file hash.

## HIGH findings

### HIGH processor/rule/test_owner_proof_test.go:32 — Proof observation and failure cleanup can block without a test deadline

Mechanism: the controlled scheduler test receives `exec.started` before installing its test-owned release/join defer. If admission disappears, the operation context expires but this receive never observes it. After Stop starts, both the main path (73–74) and the deferred cleanup (39, 42) also use raw receives. A missing completion therefore prevents return, including after a Fatal assertion that was supposed to unwind and release the fixture. Supplying deadlines to Start/Stop does not put a deadline on these independent channel operations.

The same failure shape remains in `test_processor_owner_proof_test.go:77` (`completed`) and 172/177 (`submitted`/`stopped`). Its early release protection is improved, but deferred bounded joins cannot run while the body is stuck on a raw receive. These are containment holes in the newly added tests, not allegations that the unchanged native implementations currently stall.

Smallest correction: install release/join protection before the first possible blocking admission observation; give each body and failure-cleanup observation an explicit finite containment bound; release test-owned gates before joining. Preserve positive completion signals as the oracle and report a timeout as unresolved ownership, never successful completion. Do not retry native Stop or turn the fixture adapter into asynchronous abandonment. Synchronize before inspecting owner fields if a Stop goroutine can remain live.

Verification/refutation: read all three proof files, native CronScheduler.Stop/awaitStop, Processor.Stop/cleanup and owner-lane run/fence paths. The current native methods usually deliver these channels, explaining the passing run; that does not make a regression-proof receive bounded. The scheduler expiry test's synctest bubble has virtual-time/deadlock semantics and is not conflated with the wall-clock controlled test. No new mutation was run by the reviewer. Accepted design requires explicit release/join and release-before-join on error paths; testing policy distinguishes a supplied deadline from native completion and rejects an outer runner timeout as sensitivity evidence.

## Native proof assessment

- Processor setup escape invokes the concrete never-started native Stop and observes terminal state. The transferred case opens the existing real accepted-Start/owner-lane seam, executes a command, and observes lane completion plus terminal state before its substrate marker. The admitted-command case holds a real native command, observes the fence, checks live command authority, releases it, and observes native Stop/lane completion. This is useful owner-local production-seam evidence, not full NATS boot evidence.
- Scheduler controlled proof uses the real scheduler with the existing blocking executor. It observes action admission, native stopping state, live Start child before release, native Stop result, and later Start-child cancellation. The separate synctest expiry case observes the actual Stop deadline error, fallback behavior, and second-owner-attempt refusal, then releases the held action. It does not claim that the expired native Stop joined all work.
- The integration witness reaches the actual revision helper after concrete graph-ingest Start and setup, injects a panic before transfer, and observes the provisional owner's attempted/nontransferred state. The concrete owner method synchronously invokes and checks graph-ingest Stop during unwind. This narrow test-only hook introduces no production/exported seam; the two ordinary helper callers preserve their existing successful transfer path.
- No fake Stop implementation substitutes for native behavior. Private owner duplication follows the accepted concrete-type and external-package boundary. The proof does not establish a universal wall-clock return guarantee for production Stop, nor execution of all 37 migrated callers.

## PBT and mutation assessment

The accepted examples-based PBT rationale remains appropriate: the changed behavior is a finite set of ownership-order decisions, with deterministic setup exit, transfer/body exit, explicit terminal fence/restart, concrete failed Stop/fallback, and narrower/terminal authority expiry. The independent expectation comes from `test-cleanup-policy / Lexical ownership of lifecycle test fixtures`, not merely agreement with adapter flags. Native admission/fence/release/completion observations complement the bookkeeping checks. No new grammar, revision law, or production state machine warrants a generated-history suite here. This is an assessment of the named scope, not generated property execution.

Targeted mutation remains required because existing cleanup debt passed prior checks and an error could leave owned work active. The three selected faults match the accepted minimum: actual-helper provisional omission, premature private Start cancellation, and setting attempted only after success. The supplied logs genuinely reach their intended assertions:

| Fault | Observed assertion | Result |
|---|---|---|
| provisional omission | actual helper owner remains attempted=false/transferred=false | test fails at integration proof line28, not runner timeout |
| premature cancel | admitted scheduler fire context is canceled before settlement | test fails at scheduler proof line69 |
| attempt only on success | fallback re-enters native Stop, then second explicit call returns deadline rather than refusal | test fails at scheduler proof109/111 |

The log evidence supports those observed failures. Full controlled-experiment certification remains pending the final packet: `/private/tmp/gh1428-mutations.json` names a scheduler proof hash `5f729de1604220a08a6f97e810e0c84b91fb600f672c38c588e5a0e255ab3b0f`, whereas the reviewed file is `93e323fab426480113df42b29218cf3e0337c441118eeddf9f9937bab7148d78`. Retain the exact experiment source/checks and mutation operation/patch, map any later proof change, and supply the passing baseline/restored selected command identities. The current record retains equal before/after implementation hashes and original .bak files, but not the exact mutant bytes or a reproducing mutation script. This was communicated as planned packet reconciliation, not an extra completed-work defect. Proof fixes may require renewed sensitivity evidence at the final checks.

`/private/tmp/gh1428-rule-focused-unit.log` records a passing rule package run (2.646 seconds). The log alone does not identify its command or named executed assertions, so it is not independently promoted to complete restored-test evidence.

## Evidence hashes

- mutations.json: `d57a3e243c4fc9f8118f4d7cff0afa1763eaa2eff86a2e01b06ce30a042940f1`
- focused unit log: `a38b3a81a8e60869db1cd5c23fb799400bf20d61793e59404998fcb11d129f79`
- provisional omission log: `a92d324400f7c0f50861b9adc89a20205284535f077e3ae1713768a2d828af7d`
- premature cancel log: `793c17e9d749d420c37d5b06633ae4084f86057de23f2fb2df00bbed2894fcb4`
- attempt-on-success log: `dce96211aca06a7cc795c53cf6e8f71a0ad2c2058c8c23a23ad4c4657cba2b33`

## Pending review boundary

Final guard output/baseline candidate, focused real integration, complete execution/assertion manifest, durable evidence packaging and required verification gates are still being assembled. Their planned absence at this intermediate stage is not reported as a defect. Existing 90 approvals/fingerprints must be preserved, expected remaining debt is 273, and only exact independently reviewed native non-lifecycle cancellation classifications may be added if required. The #1421 required-Test merge hold remains unchanged.

CHANGES REQUESTED for the HIGH unbounded proof-observation/failure-cleanup finding. Final implementation approval withheld pending its correction and the complete immutable evidence packet.
