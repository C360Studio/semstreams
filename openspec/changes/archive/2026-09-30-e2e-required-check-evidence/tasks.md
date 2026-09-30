# Tasks

## Inventory and design

- [x] Produce the surface inventory at the exact baseline, including existing selectors, outcomes, writers,
      diagnostics, consumers, contracts, active claims and the same problem shapes under other spellings.
- [x] Produce the adopter seam inventory for scenario authors, runner/CI users and evidence consumers.
- [x] Obtain independent INVENTORY PASS and preserve the reviewed checkpoint.
- [x] After inventory review, frame bounded options and costs, draft the design and state its invariants.
- [x] Obtain explicit owner acceptance of the independently reviewed design before implementation or spec deltas.

## Implementation and proof

- [x] Implement the remaining agreed behavior and failure controls after current-main reconciliation.
- [x] Reconcile the active config/recovery changes before editing shared scenario/authority files.
- [x] Update canonical testing documentation and the accepted capability delta to match implemented behavior.
- [x] Complete appropriate verification and independent implementation review, preserving exact evidence and limits.
- [x] Reconcile and archive the accepted change with spec synchronization.

## Final pre-archive checkpoint — 2026-09-30

The accepted implementation is complete at published branch head `a2a71a08`. The full local `task check:push`
passed in 887.29 seconds on pinned Task 3.53.1 with source unchanged. Hosted CI run
[36724318557](https://github.com/C360Studio/semstreams/actions/runs/36724318557) and both jobs in hosted E2E run
[36724318564](https://github.com/C360Studio/semstreams/actions/runs/36724318564) passed. GitHub tested merge
`e07383b048ef0e4c1b91d65d15a6c3ca9a3512fb` (parents `5457b345` and `a2a71a08`); the retained E2E reports
correctly name that tested merge as their source SHA, not the branch head. The two downloaded artifacts contain eight
allowlisted files each. All four Task/CLI reports have complete required proof, and their manifest/log digests match.
The statistical CLI report retains four nonfatal warnings; the other three reports retain none. See
[final validation](evidence/resumed-validation.md) and the durable
`recovery-1222/continuation/hosted-a2a71a08/verification.json`.

The earlier `6b495b77` five-family composite and first hosted E2E attempt at `c441841d` remain failed historical runs;
the composite's four passing families plus corrected agentic and slow-consumer Tasks establish the reviewed per-family
proof. The first hosted attempt's underlying command failure was not attributed. Writer guard-removal mutation
sensitivity remains UNVERIFIED under the accepted bounded deferral, and original outer Task launcher argv remains
explicitly unavailable under the owner-approved limit. Owner decision
[#1421](https://github.com/C360Studio/semstreams/issues/1421#issuecomment-5912919455) superseded its keep-open hold;
the issue was closed at 14:07:29 UTC without a root-cause claim. These checks predate the archive commit, which still
requires final review and fresh hosted checks after publication. The archive/spec synchronization is the intended
final content commit; its commit and publication remain with the PR owner.

## Recovery history

Recovered and claimed by the owner's request on 2026-09-30. Write owner is Codex in chat
`01a0f196-05ca-7693-a29d-414465c3e207`; the previous effort recorded all writers stopped on 2026-09-27.
This recovery superseded the travel pause for preservation and ownership. The accepted design in `acceptance.md`
remains authoritative; at this checkpoint no new design cycle, scope expansion, completed implementation or gate
waiver was claimed.

Branch: `codex/gh1222-required-e2e-proof`. Worktree:
`/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
The exact 78-path paused implementation is committed locally at
`4b8204ba0da9a579bfb1ca869b646c36d3110b98`, on the original base through `3dc4ccbe`.
Draft PR #1406 was opened at claim head `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`.
Implementation was later published after the full push gate; the PR records its published checkpoint and gate result.
A separate verified recovery archive preserves all 150 captured files, including ignored evidence logs, and a Git
bundle preserves the local commits; their manifest is retained in this chat's `recovery-1222` artifact directory.

The recovered bytes passed `go test -race ./cmd/e2e ./test/e2e/results ./test/e2e/scenarios -count=1 -timeout=90s -json`
on 2026-09-30 in 8.044 seconds, all three packages successful. This was a unit/race check on the preserved snapshot;
it does not establish assembled E2E, report rollout or push readiness. The command output and summary are retained
with the recovery archive. All 150 captured file hashes matched immediately before the recovery commit.

Main through `1b1accf4` was merged at `b9af725e`. The seven overlapping files are reconciled; the single
`validate_search.go` conflict retains both exact required identity and main's search-quality verdict.
The fallback authority correction is included in that commit, with a regression test that reproduced the original
panic and a passing four-package race check. See [the bounded evidence](evidence/fallback-main-reconciliation.md),
which identifies transcript excerpts accurately rather than claiming saved raw logs.

Continuation has implemented core/Task and agentic adoption. The agentic/fallback source slice has independent
approval after correcting the emitted model label and adding an actual tool-stage failure control. Core/report
review found and corrected Task-child report selection, active application phase identity and isolated cleanup
propagation. Both implementation slices and the documentation now have bounded independent approval.
See [the continuation evidence](evidence/resumed-validation.md). This chat owns the assembled and push gates.
The accepted design remains in force with the launcher-history addendum in `acceptance.md`. On 2026-09-30 the owner
approved retaining the statistical and slow-consumer jobs' JSON reports and logs as GitHub Actions artifacts.
The workflow now includes that bounded retention: named reports, manifests and task/child logs only, excluding
binaries and unrelated results. The prior upload-authorization hold is cleared; hosted retention proof is above.

The owner approved explicitly incomplete outer Task launcher history while retaining actual test/child argv,
source/configuration identity and observed results. Implementation must distinguish the resolved Task target from
observed command arguments; the active delta and guide record this narrow limit.

At the earlier checkpoint, task truth remained 7/10 pending final CI correction and verification. The
implementation was published
at `c441841d` after a complete local push gate passed in 891.229 seconds with unchanged source hashes.
The first hosted implementation E2E jobs failed; their console logs stopped at report finalization without exposing
the underlying command error. This remains a historical failed run, not an attributed flake.
The 2026-09-27 pause and earlier evidence records below are historical checkpoints.

## Completed bounded corrections

- Result/Writer R1/R2: [approved re-review](evidence/implementation-review-round2.md).
- CLI C1-C4: [approved re-review](evidence/cli-review-round2.md).
- Core K1: [approved re-review](evidence/core-review-round2.md), with behavioral and restored mutation evidence.
- Writer W1: [resolved review](evidence/w1-review.md). W2 preserves initialized declaration authority after malformed
  terminal submissions: [correction](evidence/w2.md), [approved re-review](evidence/w2-review.md).
- Inference I1/I2/I3: [correction and exact hashes](evidence/inference-review-fix.md). Actual HTTP-stage negative
  control detects a foreign-identity acceptance mutant; restoration checksum matches. Focused and race checks pass.
  [Bounded re-review](evidence/inference-review-round2.md) approves I2/I3 and the scenario-side I1 fix.
- CLI C5/C6 and fallback bridge: [frozen checkpoint](evidence/task-reporting.md). Initial declarations are retained
  before Setup; an unsuccessful Execute result remains unsuccessful. CLI and Writer package checks pass.
  [CLI re-review](evidence/cli-review-round3.md) approves C5/C6. Full-path fallback I1 was still open at that
  historical checkpoint; the continuation correction and bounded review now close it.

Two Writer guard-removal mutations were refused by automatic approval before edits. The independent Writer review
accepted a bounded deferral, with exact sensitivity still UNVERIFIED. Behavioral controls are not relabeled as
executed mutants, and no other required controls or assembled gates are deferred.

At the 2026-09-27 pause all implementation and review agents stopped. The preserved source/document snapshot is `evidence/pause-final-source.sha256`. Retained reports/logs are local to this worktree;
`evidence/pause-final-artifacts.sha256` identifies the final correction artifacts. Earlier pause records are historical.

## Corrected fallback finding

The historical HIGH I1 finding reached an unregistered authority variant from `semantic-fallback` execution.
Commit `b9af725e` preserves the distinct fallback behavior and uses statistical deployment authority, matching the
actual Task/Compose profile, in both Execute and the graph probe. Regression coverage includes the real authority
read, a wrong deployment refusal and the graph probe. Independent bounded review approved this correction on
2026-09-30; that approval does not establish assembled E2E or approve the remaining reporting rollout.

## Preserved partial work and remaining boundaries

The CLI/report helper and six direct Task wrappers now include core minted/graph/preidentity adoption, the five-family
composite, selected `all`/`tiers` scope, verified composite child provenance and exact executed Task-child arguments.
Actual Task fixtures cover failure propagation and cleanup ordering: ten cases pass after the assembled run exposed
a missing failed-child artifact link. The helper correction preserves the failure exit and links only real retained
reports; independent review approved it. All 17 existing statistical bind fixtures also passed.

The five-family composite ran at clean checkpoint `6b495b77` with unchanged source hashes and exited 201 after
856.857 seconds. Core, structural, statistical and semantic required checks passed. Agentic failed because the
controlled tool call completed with status `failed`; streaming proof was consequently missing. This remains a failed
composite. A separate corrected agentic Task now passes: the mock queries the exact model-endpoint entity already
verified in the active deployment, replacing its unseeded fixed sensor ID. The seven-file fixture correction has
independent approval; strict tool success remains required. The separate slow-consumer Task also passed at
`75aa4509` in 20.751 seconds, with complete required reports and unchanged source hashes. The initial full push
gate stopped on an unresolved fixture cleanup method value; the one-line typed-call correction passes the cleanup
guard. Full push-gate attempts and outcomes are retained separately and summarized in the PR.

Before the two-key provenance allowlist extension, Result/Writer passed four native fuzz targets with bounded three-second exploration, and the
Result observation target also passed exploration. Strict OpenSpec validation passed all 60 items; all 485 spec
citations resolved. Raw logs and source hashes are retained under this chat's `recovery-1222/continuation` directory.
Agentic's actual tool-stage warning-nil mutation produced baseline/mutant/restored exits 0/1/0; its source and fixture
hashes, patch and raw logs are retained there, along with the passing package race log. Prior helper-only mutation
results are not substituted for this stage-boundary evidence.
Automatic approval rejected exporting all of `test/e2e/results/` from `.github/workflows/e2e-ladder.yml`, citing
possible sensitive logs, manifests, metadata or binaries without specific authorization. No workflow edit landed
at that historical checkpoint. The later explicit owner approval above authorizes the bounded two-job allowlist.

#1404 / #1188 merged as `03bd62bb` on 2026-09-29. Its former ownership hold is historical; reconcile the landed
agentic scenario, tier-authority and platform-identity behavior before editing those surfaces. #1427 also merged.
Claude's #1117 / PR #1425 retains semantic CI ownership; this claim retains #1222 required-check evidence.
No transfer of that adjacent work is implied.

The historical #1397 issue is closed. The former #1421 hold was superseded by the owner's linked decision and the
issue is closed; its root cause is not claimed. The final pre-archive checkpoint above supersedes these earlier
verification limits. Final archive review and post-publication hosted checks remain for the landing PR.
