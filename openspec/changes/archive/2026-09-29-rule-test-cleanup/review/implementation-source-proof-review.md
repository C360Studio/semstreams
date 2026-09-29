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

## Correction re-review — final source/proof slice

This addendum supersedes the HIGH finding and provisional experiment limitations above for the final source snapshot. Final full implementation verdict is still pending baseline/guard and the complete durable manifest.

Final source manifest: `/private/tmp/gh1428-implementation-source-final.json`, SHA256 `53be1e8c8ef8a31ebd1d7f14ba127f33dab1375a99e0234c81885a904c1a0ed0`. All 16 hashes independently match current bytes. Only the two proof files changed: scheduler proof `2fd252859cbc2e681aa91794505cd6243159308af2c9e25e5ee1709673c8b103`; Processor proof `75120f151f50d8a53bab76f50a1f114ef4550540c5f393d957d9abd5704e4611`.

**The HIGH proof-observation finding is closed.** Scheduler release protection is registered before its first admission receive. Both scheduler proofs and the Processor proofs use explicit finite admission/completion observations, including the previously unbounded body and failure-cleanup receives. Error cleanup releases test-owned gates before joining. Received completion remains the positive oracle; expiration emits a failure instead of being counted as joined. The intentional blocked action/command itself still waits on its release gate, whose test owner now protects it before observing admission. The adapter's synchronous native Stop retains its accepted cooperative deadline semantics; this review does not invent a universal five-second native wall-clock guarantee.

Final mutation records are `/private/tmp/gh1428-final-scheduler-mutations.json` and `/private/tmp/gh1428-final-helper-mutation.json`. The reviewer independently applied each retained unified patch to its retained baseline bytes in memory and verified every reconstructed mutant SHA256, backup SHA256, and restored current-source SHA256 against the records. No repository file was modified. All three checks passed. Scheduler mutation records now pin the final proof hash, closing the earlier proof-version mismatch.

The scheduler baseline and restored runs pass with the same named `^TestCronSchedulerTestOwner` race command. Final premature-cancel failure reaches proof line87; final attempted-on-success failure reaches171/173 and shows the extra native Stop attempts. Both failures are assertion failures without race, compile, or outer-timeout substitution. The unchanged actual-helper witness reaches its intended omitted-finalizer assertion in the retained mutant log; its restored execution is explicitly named and passing in the final integration log. These three selected faults sufficiently test the accepted adaptation risks; the named-example PBT assessment remains accepted, with unchanged limits.

The reviewer counted 34 top-level PASS records and zero SKIP records in `/private/tmp/gh1428-rule-selected-integration.log`, package elapsed31.489seconds. Named execution includes the five run-scope callers, both revision callers, the actual revision-helper escape witness, both cron restart tests, hardening, all seven debounce root cases and their subtests, dedicated state-cleanup/stateful fixtures, and the migrated external-package cases. This proves the external adapter executed; it is more than tagged compilation. Three retained readiness/abort cases and the adjacent resource-leak case also pass. Existing ordinary scheduler caller coverage remains separate from this integration count.

The final focused race log records a passing package result in2.075seconds. Its exact selection command and durable assertion-activation table belong in the final manifest; the package-only text alone is not a count of named assertions. Similarly, the helper record's nonverbose baseline `/private/tmp/gh1428-external-phase-focused.log` passes in7.940seconds; retain its original seven-case selection command to make the helper's baseline activation auditable. These are final packet provenance details, not unresolved source defects.

Source/proof slice: APPROVE. No open HIGH finding remains in this reviewed slice. Full implementation approval is pending the exact baseline classifications, final manifest and required gate evidence; the #1421 merge hold remains unchanged.

## Durable packet and baseline reconciliation

Independent exact baseline review is recorded in `/private/tmp/gh1428-manifest-review.md`, SHA256 `a2b9be1ffa378131d9b9062a1695cda3317d0c1585738ba13506da731cf2f8ce`. It approves only candidate `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82`: exact24 B-root deletions, all273 retained entries and90 existing resolutions unchanged, four independently source-reviewed non-lifecycle cancellation callbacks appended. Four successful independent gopls reference queries confirm the callback assignments and lack of competing targets. Installation/full guard remains a separate evidence step.

The durable `review/implementation-evidence.md` packet at SHA256 `17bd46cfcc708616615bd66ca6a0721175a0c3d4b415294448bfccb1dca77bee` provides the missing exact focused-unit and helper-baseline selectors. The latter explicitly includes the actual helper escape witness; this closes the earlier nonverbose baseline provenance limitation. Final focused-unit selection includes both proof families, all scheduler caller cases, and the named native deadline/cache/watcher regression checks. No skipped branch or test-name mismatch was found in those selections.

Archive `review/evidence/implementation-evidence.zip`, SHA256 `00114fa6a8e26e84996501df7ed4f3940406321ba775cd0410b6b5674b8ccb2e`, and manifest SHA256 `0d2ee3e2929421cdbcb1b1bfb9d3eaab3b75614e07e96290cc1fc56d6536cf39` passed independent archive hash, exact member-set,34/34 member hash/size, and original-byte-equality checks. All121 B/H source path-line pins resolve; helper calls and finalizer pins have the expected role. Source manifests cover the reviewed implementation, while historical inventory and ledger remain unchanged.

One minor evidence-prose correction was sent to the coordinator: the integration bullet's “covers B00–B23/H25–H37” must exclude B02–B04, which execute in the unit command as its following sentence already states. This does not change measured coverage or require rerunning tests.

Tasks reviewed at SHA256 `8ab560f35caa30ca764140d85553f93282d452abfffcb9b2f959dfe1280151fa` correctly leave implementation/reconciliation duties unchecked pending coordinator updates, and do not claim a future hosted pass, archive, or merge. The evidence packet explicitly separates focused proof from broad preflight, actual installed guard, and the #1421 merge hold. Those pending steps remain pending rather than being converted into unsupported approval claims.

## Installed candidate and provisional implementation verdict

The installed `test/testinfra/cleanup_baseline.json` is byte-identical to approved candidate SHA256 `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82`. The unchanged actual TestCleanupRootGuard passes: durable `guard-approved.txt` SHA256 `15f10c55c907275b77491db6f94c56381f8fab7db547d4a715206f01b00d4b6f`; `guard-approved-status.json` SHA256 `6f898a69abb6cf2b88ef440d8bc1b6b289effcb8c2ec184ba4ef3bc443efd54c`. Status records the exact go-test command, exit0, wall7.805seconds; the log names the test, its PASS, package7.622seconds and census2360sources/2357typed/1361sites/976exclusions. No guard/analyzer/script edit is present. This completes canonical acceptance of the reviewed dependency fingerprints and exact reconciliation.

The integration coverage prose is corrected to21 integration-tagged B roots, with B02–B04 explicitly assigned to unit execution. Current implementation-evidence.md SHA256 is `17bd46cfcc708616615bd66ca6a0721175a0c3d4b415294448bfccb1dca77bee`. Tasks3.1–3.8 and4.1–4.5 are now checked consistently with reviewed implementation, focused proof and actual guard; remaining broad/preflight/archive/handoff tasks remain unchecked. Current tasks hash is `68cab9cd65fc92e026f2c597a684dc39fbc511a94f5af8087413319a8817b796`. A minor checkpoint-wording clarification for task2.2 was sent to the coordinator: distinguish design e185dd8b from coordinator acceptance0d888375 as the evidence document now does. It is not a source defect.

PROVISIONAL IMPLEMENTATION APPROVE: source, assertion reachability, controlled mutations, B/H coverage, concrete-native limits, exact baseline diff and actual cleanup guard are reviewed with no open blocking/high findings. This is not merge readiness. Broad task check:push evidence and final archive/spec reconciliation remain pending; verify archive is the last content commit and reviewed source remains identical. The #1421 required-Test merge hold remains binding.

## Tagged-vet correction re-review

SOURCE APPROVE for the single-file vet correction: `processor/rule/rule_integration_test.go` SHA256 `af3e0fb27a9f3324ef50d5a9300dbcd56313625b3236d58e4387b31ef0349e5b`. The full corrected16-file manifest is `/private/tmp/gh1428-vet-corrected-source-manifest.json`, SHA256 `fbaf28c90d9b646a43d41a641b5e5e1b5d497f8d8dda4faad7eac4418e533607`; all hashes independently match, and only this file differs from the previously reviewed snapshot. Approved baseline SHA256 remains `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82`.

The six changed cases install an early direct setup-authority finalizer immediately after nonnil acquisition. After successful Initialize, each creates the original10/15-second execution phase, registers its direct cancel defer, then registers the narrow owner finalizer before Start. LIFO therefore invokes native Stop with the accepted phase parent before canceling that parent. The same owner's pre-call attempted flag makes the earlier setup fallback skip after success, native error, or panic. A constructor/Initialize assertion escape still reaches the early fallback before any execution context exists. Separate outer KV/I/O authority and substrate/subscription cleanup ordering remain intact. This avoids the captured/reassigned-cancel pattern flagged by tagged vet without reopening either earlier authority/budget finding.

The developer's exact addendum is `/private/tmp/gh1428-vet-fix-addendum.md`, SHA256 `a1815fcc7ad3654714a4424872f9e80deaa4be587fd09b3d1c3f721b6d37d833`. The corrected canonical integration log SHA256 is `1d2a3f73fea3db6892fc47c7241357ea9e997e69c91cf9be8f54fe6efddc18ea`; independently counted seven named passes, zero skips/fails, package9.270seconds. It executes all six corrected external cases and the actual-helper escape witness. Package-specific tagged vet is reported passing in the addendum; its empty output file alone does not establish an exit code, so the restarted fullgate remains the authoritative broad tagged-vet check.

All concrete-owner, proof-assertion and selected mutation-site bytes are unchanged. Existing independent mutation conclusions remain applicable to those exact bytes; no new mutation run is required for this direct-defer wiring correction. The earlier34-case integration log is retained as historical evidence, with the seven-case rerun supplying current evidence for the six changed cases. No additional source finding. Provisional implementation approval resumes for this corrected snapshot; broad gate, final durable packet refresh and archive/spec reconciliation remain pending.

## Refreshed durable packet check during broad integration

Reviewed refreshed implementation-evidence.md SHA256 `7c99f33e53037e00988d4bcb23cd723399de4ad421a341467453824266145350`, vet-fix-addendum.md SHA256 `a1815fcc7ad3654714a4424872f9e80deaa4be587fd09b3d1c3f721b6d37d833`, final-handoff.md SHA256 `ff1ad809c36b9e312b2f8e731e437e895bdbef702f7b1a547324b68bc7c217a0`, and tasks SHA256 `459cf66cd4709199f2208cf55b302dcd87b21855697655a4e4da045492e04ee8`.

`evidence/implementation-source-current.json` equals corrected manifest SHA256 `fbaf28c90d9b646a43d41a641b5e5e1b5d497f8d8dda4faad7eac4418e533607`; all16 current source hashes match. The correction archive SHA256 `746377b99563082fb1411cc253c5b19a8f3e1aefee926115e822e08c7bd59390` passes archive hash, exact6-member set, each member size/hash and original byte equality. All145 refreshed B/H and additional vet-correction source pins resolve; finalizer/cancel pins have the intended roles.

The packet now clearly distinguishes the original34-case run from corrected seven-case execution, retains the first gate's tagged-vet failure, and leaves the full gate in progress without a green claim. Tasks distinguish the design checkpoint from acceptance, keep sections3/4 complete and section5 pending. The handoff preserves native deadline limitations, exact273/94 branch totals, and the #1421 hold/#1404-waiver boundary. The stated39 graph-query debt count matches the preserved original baseline; this review does not authorize that next batch.

One minor durable-evidence omission was sent to the coordinator: `gh1428-rule-vet-before.bak`, cited in the addendum with SHA256 `bd1555af8fa008af9c08da1ceb86502f4e30f0079d955c4338b5011770efab4e`, is absent from both archives. Preserve the existing backup in the correction archive/member manifest or stop describing it as retained evidence. No source or test rerun is needed. Final gate/archive verdict remains pending.

The missing-backup packet item is closed. Updated correction archive SHA256 `a69d30ad10fdb84c49ed14b84e67780589c0fca48fc08ddbeb15af61e0aea404` independently passes all7 member hash/size/original-byte checks, including the exact pre-vet source backup hash above. No remaining packet correction is requested. Broad integration and final archive reconciliation remain pending; source approval is unchanged.
