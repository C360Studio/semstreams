# Resumed validation — 2026-09-30

The frozen continuation is based on `b9af725e`; the enclosing checkpoint commit preserves the reviewed source.
The original accepted design is unchanged. The owner-approved launcher-history limit is in `../acceptance.md`.

## Independent review

The SemStreams reviewer approved the fallback, agentic, core/Task/reporting and documentation slices after fixes.
Closed findings: emitted model label, real tool-stage sensitivity, Task versus CLI report selection, fixture-phase
application identity, and isolated early-boot cleanup propagation. The two new nonsecret environment keys retain
unknown-key rejection. The final selector correction only marks the two newly adopted scenarios required.

## Executed checks

- Full race checks passed for Writer, scenarios, agentic and config. The same command found two obsolete CLI selector
  expectations; after their correction, the complete `cmd/e2e` race package passed (2.045 seconds).
- Seven actual-Task fixture cases passed, including bootstrap build/cleanup/port failures, post-init failure,
  mixed child markers, application phase selection and isolated cleanup failure. The existing statistical fixture
  passed all 17 cases. These are offline wrapper controls, not application E2E proof.
- Four Writer fuzz targets and the Result observation target passed bounded three-second native exploration.
  The Writer fuzz snapshot precedes the two-key metadata allowlist extension; full Writer race coverage follows it.
- Strict OpenSpec validation passed 60 items; all 485 spec-property citations resolved. `git diff --check` passed.
- Agentic's actual tool-stage warning-nil mutation produced baseline/mutant/restored test exits 0/1/0. Missing,
  foreign and failed results hit the intended stage-error assertion; the healthy control survived. Source hashes
  matched after restoration. A separate retained package race run passed in 1.497 seconds.
- An isolated Task copy with a deliberately failed child exited 201/0/201 before/during/after discarding the child
  exit. The restored checksum matched. This is a process-level sensitivity control, not a claimed mutant run of
  the complete Python fixture suite.

Raw logs, mutation patches and hash receipts are retained in the recovery owner's durable artifact directory,
`recovery-1222/continuation`, alongside the original recovery archive. The root race log retains the initial two
selector failures; the subsequent CLI log records their successful correction. Historical mutation transcript
excerpts have not been relabeled as saved raw logs.

## Assembled run and failed-artifact correction

At clean commit `6b495b7767738df0f7118b18a8f9970356d6264f`, `task e2e:core-inference-agentic`
ran against the explicit mock LLM from 11:04:23 to 11:18:40 UTC on 2026-09-30 (856.857 seconds).
Source hashes were unchanged. Composite `run-1790766265909549000-9213-1` exited 201: core, structural,
statistical and semantic required proof passed; agentic required proof failed. Semantic retained two nonfatal
community-ground-truth warnings; required-proof success is not a claim that every diagnostic passed.

Agentic task `e2e-agentic-1790767118980794000`, loop `ad834a6c-7332-4f1d-a451-26e57eb72043`,
reached terminal state, but controlled tool call `call_21727c17` completed with status `failed`. The subsequent
streaming observation was missing. Live request/result capture subsequently confirmed `error_kind=not_found` for the fixed, unseeded sensor ID;
no flake attribution is made.
The complete artifacts and source receipt are retained in `continuation/e2e-composite/` under the recovery archive.

The failed child Task report existed but its parent lacked the path because the shell helper suppressed its marker
on nonzero finalization. The reviewed correction links an existing terminal report or retained initial envelope
while preserving failure status, and never names an absent artifact. The old helper reproduced the missing link in
an isolated fixture; the updated actual-Task fixture passed all 10 cases. Shell syntax and diff checks passed.
Reviewer approval covers helper SHA-256 `d2410d1f62adb585599b86075cbcf16b75c49b45570379d7283140e968f5582e`
and fixture SHA-256 `42ddc241854628785d9bdde8e1b2fc7fadf1376fc7a9129d537279eb0541e3e5`.
Both RED and GREEN fixture logs are retained in `continuation/`. This correction does not repair the tool failure.

## Corrected agentic fixture

The seven-file correction targets the exact model-endpoint entity whose presence the scenario already verifies in
`ENTITY_STATES`. The agentic-only mock preset quotes that observed ID into its request and yields after receiving
the first tool result. It preserves the correlated successful-tool requirement. Independent review approved this
bounded correction, including its first-turn completion control; the agentic and mock package race checks passed.
Source patch, seven-file hash list, original `not_found` capture and post-fix output are retained in
`continuation/agentic-postfix/`.

The separate real `task e2e:agentic` passed at the reviewed dirty snapshot based on `02ac7ecd`.
Task run `run-1790768801453318000-14542-1` and CLI run `run-1790768829712345000-14649-1` both finalized
complete, required proof passed, all scenarios passed, zero errors and zero warnings, at 11:52:46 UTC on 2026-09-30.
Live TOOL capture contains the exact requested endpoint entity in the matching successful result; cleanup completed.
The earlier composite stays failed, and these reports retain their original source and run identities.

The reviewer accepted the retained four-family application proof plus this corrected agentic Task as the required
per-family assembled evidence. The scoped fixture/reporting corrections do not require repeating the unchanged
semantic application path. This is not an all-green composite claim.

## Slow-consumer and push-gate checkpoint

At clean `75aa4509`, `task e2e:slow-consumer` passed in 20.751 seconds with unchanged source hashes. Task run
`run-1790769316666097000-15933-1` and child `run-1790769335409984000-16037-1` both report complete required
proof, all scenarios passed and zero errors/warnings. The Task removed its stack. Artifacts and the exact source
receipt are retained in `continuation/e2e-slow-consumer/`.

The first `task check:push` attempt at that source exited 201 after 25.007 seconds at the cleanup guard:
`evidence_stage_test.go` registered an unresolved `responder.Close` method value. A one-line explicit typed-call
closure preserves the same cleanup operation and order, and the existing cleanup guard passes. This is a fixture
representation correction, not a guard exemption or an observed runtime leak. The failed gate log remains in
`continuation/check-push/`; later attempts use separate directories so the original failure is not overwritten.
The second full gate at `db3f8cd6` passed the cleanup guard and stopped at revive after 14.355 seconds on two
function-length warnings. The bounded correction extracts existing Writer time normalization and CLI run preparation/
completion into private helpers, preserving their order and failures. Affected-package revive and CLI/Writer race
checks pass; independent review found no behavior changes and retained the prior per-family E2E disposition.
The second gate log is retained in `continuation/check-push-round2/`; the next attempt has its own directory.
Strict OpenSpec validation passed 60 items, and all 488 current property citations resolve.

## Published recovery and approved CI retention

The complete local `task check:push` passed at `c441841d` in 891.229 seconds, with source hashes unchanged.
The entity-ID audit passed 1,337 candidates after the mock switched from invalid placeholders to direct binding of
validated request-carried IDs. Bounded independent review and focused mock/agentic race, lint and vet passed.
That checkpoint was published to draft PR #1406; the worktree was clean.

The owner then explicitly approved retaining the statistical and slow-consumer jobs' JSON reports and logs as
GitHub Actions artifacts. The workflow adds `always()` uploads for the named result/member/manifest JSON files,
task/log files and child stdout. Copied application binaries, report inputs and unrelated results are excluded.
The allowlist matches files from the retained local slow-consumer and composite runs. The later hosted upload and
download verification is recorded below; authorization alone was not treated as proof.

The first hosted implementation E2E run (`36719273975`, source `c441841d`) failed both jobs. The console reaches
Task report finalization but omits the underlying command log. The failure log is retained in the recovery artifact
directory; no infrastructure or flake attribution is made from that incomplete output.

## Failed finalizer diagnostics

The pinned CI Task version, 3.53.1, reproduced a separate output-loss defect in an isolated copy of the original
structural wrapper: Task exited 201 and emitted its retained report marker, but omitted the runtime-only Compose
stdout marker. The reproduction script exits zero only after asserting that defect; it is not a successful Task run.
Seven adopted wrappers now capture finalizer status with `|| report_exit=$?` before printing the captured log.
Cleanup ordering and command/cleanup/report failure precedence are unchanged.

The strengthened actual-Task fixture suite passed all 10 cases on Task 3.53.1; the release guard passed under
`-race` in 1.187 seconds. Task listing, Python syntax and diff checks passed. The focused RED/GREEN logs and source
hashes are retained in `continuation/ci-retention-finalizer/`. The underlying failure of that first hosted E2E run
remains unattributed. Later passing runs do not establish its root cause.

## Final pre-archive verification

At published branch head `a2a71a08b06741c177a22e525cce4a365375511c`, the complete local
`task check:push` passed in 887.29 seconds using Task 3.53.1 with source hashes unchanged. The raw log and receipt
are retained in `recovery-1222/continuation/check-push-round6/`. The prior failed gate attempts remain separate.

Hosted [CI run 36724318557](https://github.com/C360Studio/semstreams/actions/runs/36724318557) passed all checks,
including its Test job. Hosted [E2E run 36724318564](https://github.com/C360Studio/semstreams/actions/runs/36724318564)
passed both statistical and slow-consumer jobs. GitHub checked out tested merge
`e07383b048ef0e4c1b91d65d15a6c3ca9a3512fb`, whose parents were main `5457b345` and branch `a2a71a08`;
the reports' `source_sha` correctly records the merge commit. These are results for that published branch head and
tested merge, not for the later archive commit.

The downloaded [statistical artifact](https://github.com/C360Studio/semstreams/actions/runs/36724318564/artifacts/11102538231)
and [slow-consumer artifact](https://github.com/C360Studio/semstreams/actions/runs/36724318564/artifacts/11101528780)
each contain exactly eight allowlisted report, manifest and log files. All four Task/CLI reports are complete,
required-proof-passed and exited zero. Parent/child identities and manifest/log digests match the retained bytes.
Statistical CLI has four nonfatal warnings; the other three reports have none. Downloaded ZIPs, raw hosted logs and
`verification.json` are retained under `recovery-1222/continuation/hosted-a2a71a08/`. This establishes observed
hosted retention; it does not relabel the earlier failed composite or first hosted run as passing.

The owner [superseded the keep-open hold for #1421](https://github.com/C360Studio/semstreams/issues/1421#issuecomment-5912919455);
the issue was closed at 14:07:29 UTC on 2026-09-30. This disposition does not claim a root cause for the historical
recurrence. The accepted outer-launcher-argv limit and bounded Writer guard-removal mutation deferrals remain
unchanged; exact sensitivity for those refused mutations is UNVERIFIED. Final archive/spec review and fresh hosted
checks after publication remain outside this pre-archive proof.
