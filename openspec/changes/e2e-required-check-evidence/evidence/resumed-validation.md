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

## Remaining proof

The separate adopted slow-consumer Task and full push gate have not run on this continuation. CI retention is still awaiting explicit upload
authorization after automatic approval rejected the prepared workflow edit. Earlier bounded Writer mutation
deferrals remain unchanged. No merge-readiness, full implementation completion or issue closure is claimed.
