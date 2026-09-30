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

## Remaining proof

Assembled E2E and the full push gate have not run on this checkpoint. CI retention is still awaiting explicit upload
authorization after automatic approval rejected the prepared workflow edit. Earlier bounded Writer mutation
deferrals remain unchanged. No merge-readiness, full implementation completion or issue closure is claimed.
