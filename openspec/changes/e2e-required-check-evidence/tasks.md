# Tasks

## Inventory and design

- [x] Produce the surface inventory at the exact baseline, including existing selectors, outcomes, writers,
      diagnostics, consumers, contracts, active claims and the same problem shapes under other spellings.
- [x] Produce the adopter seam inventory for scenario authors, runner/CI users and evidence consumers.
- [x] Obtain independent INVENTORY PASS and preserve the reviewed checkpoint.
- [x] After inventory review, frame bounded options and costs, draft the design and state its invariants.
- [x] Obtain explicit owner acceptance of the independently reviewed design before implementation or spec deltas.

## Implementation and proof

- [ ] Implement the remaining agreed behavior and failure controls after current-main reconciliation.
- [ ] Reconcile the active config/recovery changes before editing shared scenario/authority files.
- [ ] Update canonical testing documentation and the accepted capability delta to match implemented behavior.
- [ ] Complete appropriate verification and independent implementation review, preserving exact evidence and limits.
- [ ] Reconcile and archive the accepted change with spec synchronization as the final content commit.

## Current checkpoint

Recovered and claimed by the owner's request on 2026-09-30. Write owner is Codex in chat
`01a0f196-05ca-7693-a29d-414465c3e207`; the previous effort recorded all writers stopped on 2026-09-27.
This recovery supersedes the travel pause for preservation and ownership. The accepted design in `acceptance.md`
remains authoritative; no new design cycle, scope expansion, completed implementation or gate waiver is claimed.

Branch: `codex/gh1222-required-e2e-proof`. Worktree:
`/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
The exact 78-path paused implementation is committed locally at
`4b8204ba0da9a579bfb1ca869b646c36d3110b98`, on the original base through `3dc4ccbe`.
Draft PR #1406 remains at remote claim head `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`.
The implementation is still unpushed because its full push gates have not run. Local access is required.
A separate verified recovery archive preserves all 150 captured files, including ignored evidence logs, and a Git
bundle preserves the local commits; their manifest is retained in this chat's `recovery-1222` artifact directory.

The recovered bytes passed `go test -race ./cmd/e2e ./test/e2e/results ./test/e2e/scenarios -count=1 -timeout=90s -json`
on 2026-09-30 in 8.044 seconds, all three packages successful. This was a unit/race check on the preserved snapshot;
it does not establish assembled E2E, report rollout or push readiness. The command output and summary are retained
with the recovery archive. All 150 captured file hashes matched immediately before the recovery commit.

Next work is reconciliation with current main, then the existing fallback finding and unfinished report adoption.
Main inspected at `1b1accf4`; #1404 and #1427 are merged. Seven touched files changed on main; a read-only text-merge
preview found one conflict in `test/e2e/scenarios/validate_search.go`. Preserve both the required-check identity
observation and main's search-quality verdict. The other six clean text merges still need semantic review.
No merge, rebase, remaining scenario adoption or assembled run was performed during recovery.
Task truth remains 5/10. The 2026-09-27 pause and earlier evidence records below are historical checkpoints.

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
  [CLI re-review](evidence/cli-review-round3.md) approves C5/C6; full-path fallback I1 remains open.

Two Writer guard-removal mutations were refused by automatic approval before edits. The independent Writer review
accepted a bounded deferral, with exact sensitivity still UNVERIFIED. Behavioral controls are not relabeled as
executed mutants, and no other required controls or assembled gates are deferred.

At the 2026-09-27 pause all implementation and review agents stopped. The preserved source/document snapshot is `evidence/pause-final-source.sha256`. Retained reports/logs are local to this worktree;
`evidence/pause-final-artifacts.sha256` identifies the final correction artifacts. Earlier pause records are historical.

## Remaining review finding

I1 remains HIGH in [CLI round-three review](evidence/cli-review-round3.md): the selectable `semantic-fallback` path
passes its behavior variant to `EffectiveTierAuthority`, whose registered authority table rejects it with a panic.
The path predates the declaration fix. The current bridge test stops at constructor/catalog classification; it does
not run the real fallback lifecycle. The Task selects statistical Compose configuration, so reconcile deployment
authority and the held #1404 surface before fixing/testing this path. No Docker reproduction or owner ruling on
that authority choice is claimed. This finding remains open; the recovered work is not approved for integration.

## Preserved partial work and remaining boundaries

Private CLI/report provenance and six default Task bodies are edited. The helper and extracted shells pass syntax
checks; Task listing and focused package checks pass. Docker execution, Task lifecycle/failure propagation and the
full report/provenance surface remain unreviewed or unverified. Core report initialization currently refuses pending
held minted/graph scenario adoption; preidentity no-record evidence is still missing. The five-family composite,
`e2e:all`/`e2e:tiers` adoption and composite app provenance are unimplemented.

Automatic approval rejected exporting all of `test/e2e/results/` from `.github/workflows/e2e-ladder.yml`, citing
possible sensitive logs, manifests, metadata or binaries without specific authorization. No workflow edit landed;
CI retention remains blocked. No bypass was attempted.

#1404 / #1188 merged as `03bd62bb` on 2026-09-29. Its former ownership hold is historical; reconcile the landed
agentic scenario, tier-authority and platform-identity behavior before editing those surfaces. #1427 also merged.
Claude's #1117 / PR #1425 retains semantic CI ownership; this claim retains #1222 required-check evidence.
No transfer of that adjacent work is implied.

Remaining work includes held scenario adoption, Task/composite completion, representative failure controls,
exploratory fuzzing where required, assembled E2E, full push gates, final review and archive/spec synchronization.
The historical #1397 issue is closed. Required-job recurrence #1421 remains open and has no waiver for this PR. Earlier GREEN and claim-head hosted checks do not prove
this dirty implementation. Task truth remains 5/10; no implementation-complete or merge-ready claim is made.
