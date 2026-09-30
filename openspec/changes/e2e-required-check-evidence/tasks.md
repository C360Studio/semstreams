# Tasks

## Inventory and design

- [x] Produce the surface inventory at the exact baseline, including existing selectors, outcomes, writers,
      diagnostics, consumers, contracts, active claims and the same problem shapes under other spellings.
- [x] Produce the adopter seam inventory for scenario authors, runner/CI users and evidence consumers.
- [x] Obtain independent INVENTORY PASS and preserve the reviewed checkpoint.
- [x] After inventory review, frame bounded options and costs, draft the design and state its invariants.
- [x] Obtain explicit owner acceptance of the independently reviewed design before implementation or spec deltas.

## Implementation and proof

- [ ] HOLD: implement the remaining agreed behavior and failure controls after the owner resumes this effort.
- [ ] Reconcile the active config/recovery changes before editing shared scenario/authority files.
- [ ] Update canonical testing documentation and the accepted capability delta to match implemented behavior.
- [ ] Complete appropriate verification and independent implementation review, preserving exact evidence and limits.
- [ ] Reconcile and archive the accepted change with spec synchronization as the final content commit.

## Current checkpoint

HOLD on the owner's 2026-09-27 instruction: "once latest fixes are in let's pause". Current review fixes are
implemented and bounded re-reviews are complete. A remaining fallback authority panic is recorded below. No additional
Task/composite adoption or assembled
runs are authorized during this hold. The accepted design remains authorized by `acceptance.md`; no gate is waived.

Local HEAD is `fe6e2cc03e16f5db47e293f55939548572f204cc`, including main through `3dc4ccbe`.
Branch: `codex/gh1222-required-e2e-proof`. Worktree:
`/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
The branch is four commits ahead of its upstream. Current implementation, accepted delta, documentation and evidence
are uncommitted/unpushed. Draft PR #1406 remains at claim head `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`.
Required push gates have not run. The local worktree is required for pickup; the remote claim does not contain it.

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

All implementation and review agents have stopped; no operation from this effort remains running. The final dirty
source/document snapshot is `evidence/pause-final-source.sha256`. Retained reports/logs are local to this worktree;
`evidence/pause-final-artifacts.sha256` identifies the final correction artifacts. Earlier pause records are historical.

## Remaining review finding

I1 remains HIGH in [CLI round-three review](evidence/cli-review-round3.md): the selectable `semantic-fallback` path
passes its behavior variant to `EffectiveTierAuthority`, whose registered authority table rejects it with a panic.
The path predates the declaration fix. The current bridge test stops at constructor/catalog classification; it does
not run the real fallback lifecycle. The Task selects statistical Compose configuration, so reconcile deployment
authority and the held #1404 surface before fixing/testing this path. No Docker reproduction or owner ruling on
that authority choice is claimed. The work is paused with this finding open, not approved for integration.

## Preserved partial work and remaining boundaries

Private CLI/report provenance and six default Task bodies are edited. The helper and extracted shells pass syntax
checks; Task listing and focused package checks pass. Docker execution, Task lifecycle/failure propagation and the
full report/provenance surface remain unreviewed or unverified. Core report initialization currently refuses pending
held minted/graph scenario adoption; preidentity no-record evidence is still missing. The five-family composite,
`e2e:all`/`e2e:tiers` adoption and composite app provenance are unimplemented.

Automatic approval rejected exporting all of `test/e2e/results/` from `.github/workflows/e2e-ladder.yml`, citing
possible sensitive logs, manifests, metadata or binaries without specific authorization. No workflow edit landed;
CI retention remains blocked. No bypass was attempted.

#1404 / #1188 remains open at remote `ada5c46a`. Its latest PR record reports local `a319048e`, Q8(b) ruled and
implemented locally, reviewer round 3 in flight, and a race-gate failure from #1397 awaiting its owner's disposition.
The overlapping agentic scenario, tier-authority and platform-identity files remain untouched here. Recheck live
ownership and landed base before editing them. No shared-file ownership transfer is inferred.

Remaining work includes held scenario adoption, Task/composite completion, representative failure controls,
exploratory fuzzing where required, assembled E2E, full push gates, final review and archive/spec synchronization.
Known required-job flake #1397 is not waived for this PR. Earlier GREEN and claim-head hosted checks do not prove
this dirty implementation. Task truth remains 5/10; no implementation-complete or merge-ready claim is made.
