# Review: complete terminal correction before application

## Exact input and verdict

Independent semstreams-reviewer verdict: **CHANGES REQUESTED — not approved for application.**

Reviewed patch SHA-256 `c172d899cd2b9c0d9ad6d7f994ac17edbff392d8e5d6afbec941086b80ac03b4` contains
four production files (+410/-295) and their test/caller adaptations, 17 files total. All 17 live source baseline
hashes matched. The proposed four production files matched the earlier frozen complete-code snapshot exactly.
No source was applied, compiled or executed. This is an exact-code application review, not runtime or merge proof.

The complete input, candidate strings, baseline manifest, review map and pre-application source archive are preserved
under the existing rescue checkpoint's `r6-state-contract.ZYhKkH/complete-terminal-review.aI6iTg` directory.
All line references below are proposed candidate lines, not current worktree locations.

## Findings

### BLOCKING: pre-birth refusal retains speculative admission

`processor/agentic-loop/component.go:1482` — validation, marshal, Create and collision-read failures return without
releasing the active loop. Neither task caller at 1339–1340 or 1353–1354 cleans it up. Redelivery can then bypass
durable recovery and ACK through active-task deduplication at 1273 and 1303–1307 despite failed durable birth.

Use the existing release owner on every refusal exit. Preserve invalid-serialization classification and context:
1488–1490 currently returns a raw marshal error, producing Retry rather than the expected Terminate.

Refutation checked: cleanup at 1500 covers only collision mismatch; other early returns never reach handleLoopFailure.
The submitted `spawn_identity_failure_test.go:228`–237 already requires classified error context and cleanup.
Add a Create-failure/redelivery control proving the second delivery cannot receive a deduplication ACK.

### MEDIUM: terminal cancellation silently skips requested work

`processor/agentic-loop/component.go:2471` — effect-free inapplicable ACK is permitted, but the new branch emits
neither a reasoned skip signal nor metric. The generic Processing signal debug message does not identify the refusal.

Use the existing signal owner for an observable skip and test it. Do not repair COMPLETE or alter terminal authority.

## Unchanged boundary

No other HIGH/BLOCKING issue was confirmed in the reviewed selection, revision, correlation or approval-decision routes.
Correct the exact patch and obtain re-review before application; runtime evidence follows application approval.
The owner-approved scope and first-R6 checkpoint remain intact. R3 remains separately held.

## Corrected application checkpoint

The reviewer subsequently returned **APPROVE FOR APPLICATION** for patch SHA-256
`d514d48e94b1e56f0293d9ce95f5c94dad4b4497f27629d4a0aff8fd0fa2a0ba`. Its exact delta from the initial
candidate changes component.go by +3/-2 lines and adds one Create-failure/redelivery test. The entry defer releases
every pre-birth refusal; invalid serialization retains its classification and context. The reviewer verified all
17 source baselines and the two-file delta. The MEDIUM cancellation-observability finding remains open, not waived.

Root applied that exact patch after approval. Patch context placed an unchanged test-declaration block earlier in
approval_recovery_test.go; the developer restored its reviewed order with apply_patch. All 17 applied files then
matched the reviewed candidate text and SHA-256 fingerprints exactly. No production adjustment was needed for order.

The first affected-package run found one further production caller defect: handleCancelSignal drops the helper's
DeliveryDecision, so unknown terminal publication is reclassified as Retry by handleSignalMessage. Its existing
Quarantine/join/no-ACK/no-NAK/no-TERM tests remain authoritative. The private return-value correction and faithful
fixture adaptations passed the follow-up review recorded below and are now applied. No new runtime or storage was
added. The diagnostic choice was subsequently resolved by the owner continuation recorded below.

## Runtime evidence on applied V2

Logs are preserved in `complete-terminal-review.aI6iTg/evidence` beside the source/input backup. All runs below
used the dirty applied V2 tree, not published HEAD. They do not establish whole-R2/R6 completion or a push gate.

| Command / scope | Result | Log SHA-256 |
| --- | --- | --- |
| `go test -race ./processor/agentic-loop -run '^(TestTerminalSelection\|TestCompletionCodec\|TestSpawnBirth\|TestHandleSpawnIdentityFailure\|TestOperational)' -count=1 -v` | PASS, 1.587s | `a699a65edd4fe3bb8cb48e95e17ef11e6bbba3e94dd1b81660bafa092dea78c9` |
| `go test -race ./agentic ./processor/agentic-loop ./processor/agentic-dispatch ./frameworkcapabilities/graphresearch -count=1` | agentic 5.156s, dispatch 2.156s, research 1.941s PASS; loop 2.522s FAIL | `ca6ef5e69f8697ca7d173219c04e64c47eea619e4b55c0a8bd15a738cfe916d8` |
| Native cancellation-overwrite control | PASS, 4.204s | `25f079df91124dc9fc8045670668d91f6c1dd40d6bb4aa1b84033dd5c06e88c6` |
| Four native approval/final-marker controls | PASS, 34.100s | `8255a6b278c3e62247b78bb31f8c6c966cc83904fdbfc0e99b62f745be2b5266` |

The affected-package log's Go result is FAIL; a shell-pipeline setup mistake reported zero despite that result.
No successful gate is claimed from it. The following native commands used `set -euo pipefail` and completed with
successful exit status through the repository's shared-host lock; all owned containers joined and were cleaned up.

```bash
scripts/run-integration-tests.sh ./processor/agentic-loop \
  -run '^TestIntegrationMissingApprovalEvidenceCannotOverwriteCancellation$' -v
scripts/run-integration-tests.sh ./processor/agentic-loop \
  -run '^(TestIntegrationApprovalRetainedAbsenceFailsAfterReplacement|TestIntegrationApprovalRejectionJoinsCancelledGraphRequestBeforeQuarantine|TestIntegrationLoopSignalAndApprovalCallbacksCommitBeforeAck|TestIntegrationTerminalMarkerFailureRedeliversAfterComponentReplacement)$' -v
```

The motivating overwrite control preserves cancellation bytes/revision 2, writes no COMPLETE or failure event,
and releases stale process state. The additional controls include both request/response absence after component
replacement, warm/cold approval cancellation with joined work and no settlement, native signal/approval ACK-order
checks, and final-marker failure followed by component replacement and source redelivery. They are not an E2E rerun.

## Follow-up review and fixture proof

The private cancellation follow-up (`6d4bd477ba6f7aaa`) received CHANGES REQUESTED. It correctly forwards the terminal
owner's explicit decision but must not change the earlier signal-specific classification of local installation
failure. GetLoop and CreateLoopWithID have separate locks: a valid concurrent installation collision is retryable,
not a reason to terminate the cancellation input. Correct that existing routing; no new lock or authority is needed.

The independent two-file fixture repair (`310a668f6dcf4c98`) received APPROVE FOR APPLICATION and is applied.
Each state-table row now starts with its own loop. The timeout-at-cap case has its own authority store and cannot
overwrite its parent's already-selected max-iterations outcome. All existing refusal assertions remain, with added
bytes/revision preservation checks. The focused race run passed (1.636s):

```bash
go test -race ./processor/agentic-loop \
  -run '^(TestLoopManager_StateTransition|TestColdTerminalToolResultRequiresExactAppliedEvidence)$' -count=1 -v
```

Its log is `fixture-repairs-applied.log` in the same evidence checkpoint, SHA-256
`4d3ef2d0cb255e53439f3ae5b6cf7b822a4d9c9285daac8c67b1961fc1c6501f`.

The revised cancellation patch (`55e06d2c81dfc8631d98c83b839c613aa3725de6ef59f67033524c0b9d5e9d7f`)
received APPROVE FOR APPLICATION. It preserves the prior signal-specific nonfatal Retry/fatal Quarantine policy
before effects and forwards the terminal owner's explicit decision unchanged. The production delta is +24/-17.
Its added duplicate-loop-error test is explicitly classification-only, not concurrent-install scheduling proof.
All five applied file hashes matched the independently reviewed candidate JSON exactly. No remaining HIGH/BLOCKING
finding was identified in this application review; the MEDIUM observability finding and separate R3 hold remain.

The full four-package race re-run on the applied correction passed without exclusions:

```bash
go test -race ./agentic ./processor/agentic-loop ./processor/agentic-dispatch \
  ./frameworkcapabilities/graphresearch -count=1
```

Results: agentic 5.529s, loop 3.347s, dispatch 2.973s, research 2.073s. The log is
`affected-packages-followup-v2.log`, SHA-256
`cdd9ee1bd563acba5fa6feea43d4f03e0868e52211216d5c9a4711eeddf464a6`. This supersedes the earlier loop FAIL
for these affected packages, not the whole-repository push gate.

Six native controls on the corrected tree also passed (37.356s), using the shared integration lock and `-race`:

```bash
scripts/run-integration-tests.sh ./processor/agentic-loop \
  -run '^(TestIntegrationMissingApprovalEvidenceCannotOverwriteCancellation|TestIntegrationApprovalRetainedAbsenceFailsAfterReplacement|TestIntegrationApprovalRejectionJoinsCancelledGraphRequestBeforeQuarantine|TestIntegrationLoopSignalAndApprovalCallbacksCommitBeforeAck|TestIntegrationTerminalMarkerFailureRedeliversAfterComponentReplacement|TestTerminalStampCarriesObservedAuditLoss_Integration)$' -v
```

All six top-level tests ran and passed; none was skipped. The log is `native-terminal-followup-v2.log`, SHA-256
`1c3295f177d46af6ddad1eeae49e71dbd1ae05e2ec9145d7dd7e6a0b9435b65c`. The final-marker replacement test's
30.43s includes its expected redelivery wait. All owned tests/containers joined and the shared lock was released.
This is focused native proof, not full integration or an E2E tier.

## Post-runtime verdict

Independent reviewer: **APPROVE — tested core slice only, with the MEDIUM cancellation-observability finding held
open.** All 19 applicable candidate hashes and both separate fixture corrections match current source; all four
affected packages and six native tests pass, with matching log hashes and no exclusions/skips. The review preserves
committed cancellation, joined quarantine/no settlement, required publication before final marker, and replacement
redelivery. No remaining HIGH/BLOCKING source finding was identified.

The reviewer also identified two historical task statements that still described current proof as RED. Root marked
the command as historical and qualified the earlier no-GREEN checkpoint. No runtime or acceptance criterion changed.
At that core-only checkpoint R2/R6 remained unchecked, R3 held, and full push/E2E gates unrun. The later diagnostic,
E2E and R2 closeout evidence is below; none is whole-PR or merge approval. Source and evidence are preserved in
`after-terminal-followup-source.tar.gz` in the existing backup,
SHA-256 `ff4bd5205525f2f459f33374aa58e8a2533c4636f81a149452bb39f3c5e63917`; that archive predates only this
post-runtime prose/task-truth correction, not any subsequent runtime edit.

## Terminal-cancellation diagnostic continuation

The owner replied `continue` to the recommendation for one diagnostic counter plus a reasoned log, with no change
to cancellation or ACK behavior. [Issue comment 5654482198](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5654482198)
records that bounded authority. Implement in the existing terminal-cancel branch and metrics owner, with no identity
labels or additional mechanism. Tests must observe both signals, unchanged durable authority/selected completion,
and unchanged settlement. This authorization did not resolve the review finding or R3 by itself.

The exact three-file patch `f3d565c620f14b34f8a306e2f95e3904f4a81884df3c57b6ece0b76110bc77f0` passed
independent pre-application review. Root first applied metrics/tests without emission: terminal rows failed their
independent log and counter assertions; storage/error controls passed. Applying the reviewed five emission lines
made all four rows pass (1.547s). All three final candidate hashes match. The complete production addition is
14 lines, including counter declaration and registration through both existing metrics paths.

Full affected-package race runs pass without exclusions: agentic 5.299s, loop 3.533s, dispatch 2.185s and research
1.962s. Independent post-runtime verdict: **APPROVE — MEDIUM cancellation-observability finding resolved.**
The operator note documents the real metric name and per-delivery meaning; no caller/configuration change is needed.

Evidence from the dirty applied diagnostic tree in `gh1146-cancel-diagnostic.CE97IU`:

| Log | SHA-256 |
| --- | --- |
| `diagnostic-red.log` | `2e1b7155c61e4304f18e7d44c659f54c709ece2d3fa51093cb90dfce4325921b` |
| `diagnostic-green.log` | `ee926154d4fb4c43e0e71c57c427ef42fb5c53c72017eb2788b38082eb4a9b62` |
| `affected-packages-green.log` | `ac4b51bcfd48d6588081303eaed7c07af01799fbedab090dc7ff81e9f828cbb5` |

The bounded R2 closeout review identifies refreshed execution of eleven existing native approval/replay cases plus
the existing `walk-approval-after-restart` OS-process E2E stage as the remaining evidence before presenting R3.
It identified no new coverage design: validator/poison/contention rows remain current unit-race evidence, and the
prior six native controls cover retained absence, cancellation preservation, joined quarantine and callback order.
This is not a ruling to remove the approval Store, nor completion of R4–R10 or the final combined candidate gate.

The eleven native cases were refreshed on the applied diagnostic tree: all eleven ran and passed, none skipped,
in 197.971s (six use real approximately 30-second redelivery intervals). The log is `native-r2-refresh.log`, SHA-256
`7ed9dc169fa19b7414d5f0f8a01bee6ea6f68c184196a928b3746916e63e4b20`.

```bash
scripts/run-integration-tests.sh ./processor/agentic-loop \
  -run '^TestIntegration(Approval(AfterLoopAndDispatchReplacement|TimeoutAfterLoopAndDispatchReplacement|ReplacementIgnoresOlderSameCallIDResponse|RequiredResult(RedeliversAfter(ClosedGate|LaterHistory|ModifiedGate|Timeout)|RetriesMatchingPendingPrompt))|ModifiedApprovalAfterLoopAndDispatchReplacement|RejectedApprovalAfterLoopAndDispatchReplacement|AppliedApprovalRedeliversAfterOwnerReplacement)$' -v
```

The existing mock-provider agentic E2E tier passed in 2m32.4378s with `assertions_run=16`. The actual
`walk-approval-after-restart` stage ran (6518ms), alongside ordinary approval, cancellation, retained tool replay
and process replacement. Command: `AGENTIC_LLM_URL=http://mock-llm:8080/v1 task e2e:agentic`. Log:
`e2e-agentic.log`, SHA-256 `0299e8950a4ee5bc1bc53aff86e7463c8ce571a6ee0b3b8bd68c4f903ad77ee1`.

Native test containers joined and the shared integration lock was released before E2E started. The E2E task removed
its three owned containers, temporary NATS volume and network afterward; no owned test job remains. Logs and exact
diagnostic inputs are copied to the existing `complete-terminal-review.aI6iTg` evidence/input backup. The full
repository push gate has not been rerun for this corrected tree; this E2E success does not satisfy that entire gate.

An active-loop gauge of -1 after replacement was observed separately from settlement correctness and recorded on
[existing #1242](https://github.com/C360Studio/semstreams/issues/1242#issuecomment-5654575077). The passing E2E does not
validate that gauge, and this change does not absorb its metrics-authority work.

## R2 closeout verdict

Independent reviewer: **R2 CLOSEOUT PASS.** The bounded obligation map is satisfied by the current unit-race,
retained-absence/cancellation-preservation, diagnostic, eleven refreshed native replay cases and OS-process restart
E2E evidence. Applied runtime fingerprints still match. Root may mark only R2 complete and present the separate R3
owner choice. This does not revoke the Store ruling, complete R4–R10 or R6, or establish a whole-PR/push/merge gate.

## R3 advisory, not an owner ruling

The bounded semstreams-judge check recommends retiring only the additional approval-continuation Store plan,
using existing loop KV and exact retained request/response evidence, with explicit `continuation_unavailable` for
confirmed absence. Its confidence is high for this evidence-bounded guarantee, not for surviving evidence eviction.

The strongest case against is material: required AGENT evidence can disappear while pending authority survives,
potentially before the approval deadline. The current admission design excludes approval lifetime, so the 12-hour
approval default and 24-hour loop-KV retention do not by themselves guarantee 12-hour request/response retention.
Neither the tests nor this advisory establish full-duration retention across every permitted configuration.

R3 remains held. The owner must choose whether to supersede only ApprovalContinuationV1, its Store configuration,
digest/cleanup and deliberate evidence-eviction survival claims, preserving existing Retry/Quarantine/confirmed-
absence boundaries, default deadline, KV-retention validation, DiscardNew/admission and unrelated rulings.

### Bounded late-gate contract check

A read-only semstreams-judge check supports retaining the existing current-authority boundary: selection fixes the
eventual terminal outcome, while the final marker commits terminal authority. Before that marker, a separately
eligible approval gate still requires its own correlation, revision-conditioned write and prompt PubAck. Withheld
ACK alone never authorizes mutation. This follows the change spec's terminal requirement (398–413), current-authority
contract (106–147), and admitted approval evidence reads (613–646); it is advisory interpretation, not an owner ruling.

The strongest contrary concern is an approval prompt after cancellation selection. An immediate selection-time
freeze would change the accepted evidence boundary; this fixture cannot silently add one. Already-committed
cancellation must still preserve its bytes/revision, as proved by the separate native overwrite regression. The judge
did not run tests or establish eventual cancellation progress after an intervening gate revision changes.

## R3 owner retirement and documentation review

The owner subsequently replied **"approved retire"**, recorded in
[comment 5654729986](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5654729986).
This resolves the preceding R3 advisory/hold: retire only the additional approval-continuation Store plan, not the
whole earlier ruling, other Store uses, timeout, KV-retention validation, DiscardNew/admission or later amendments.
Existing KV/exact-message recovery retains explicit `continuation_unavailable` for confirmed required-evidence
absence, including before the approval deadline. No post-eviction success guarantee is introduced.

Independent implementation/documentation conformance review returned **APPROVE — R3 alone may remain checked**
for the exact five-file patch `dedecace53aa27116ac344d956c304cc6f0ca48858e762fd42447d1f8ac82e76`.
The reviewer verified the ruling, all candidate hashes, unchanged runtime fingerprints, preserved dated provenance,
and prepared issue/PR text. No approval-continuation implementation/configuration/schema exists to delete; the
trajectory Store is unrelated. The reviewed active files are proposal.md, design.md, tasks.md, the agentic-loop
capability delta and docs/concepts/17-approval-flow.md. No remaining findings.

Strict OpenSpec validation passes 55/55; queue 10/22 flags only the existing R11 combined-proof gate. Diff whitespace
checks pass. Go/config/schema diff SHA-256 remains
`c9627d339e1b9fc2c14322d2e230579babf7ff86f6bf6edda2befe66734829e2` before and after this documentation-only change.
Runtime/native/E2E tests were not rerun; the R2 evidence above remains attached to its tested source.
Exact before-files, review patch and shared-text copies are in `/private/tmp/gh1146-r3-retirement.NS2qdb`, backed up
under the existing `complete-terminal-review.aI6iTg` directory beside the claim worktree. R4–R15 and final PR gates
remain open. No commit, push, merge, rebase, archive or issue closure occurred.
