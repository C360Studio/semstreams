# Ooze pilot review checkpoints

## Inventory

Independent SemStreams reviewer: INVENTORY PASS for the bounded local evaluation surface.
Reviewed SHA256: `388d93485bddf879dee8ab37b75bb9567fbe6ccb150b6291ba3a445c097225aa`.
Baseline: `84fc01e46c104d890bffe32b0046e72f006df454`.

The reviewer independently confirmed validator owners, distinct test oracles, shared production-constant
limitations, witness handling, optional shutdown boundaries, and adjacent claims. Attempts to refute
absence and scope claims found no missing owner. Upstream qualification and experimental results were
outside this verdict.

Mechanical verification found 60 valid pins and 19 unparsed prose bullets. Root converted those bullets
to numbered items without changing their content. The corrected inventory SHA256 is
`b9ecc720ef153d292931137bf15c5a031c834462b7e14b2d4231eda6468d1f5c`.
`task inventory:verify -- docs/experiments/ooze-pilot/inventory.md` then passed:
`pins=60 ok=60 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`.

## Additional root measurements

- Installed toolchain: `go version go1.26.4 darwin/arm64`.
- Internal test dependency closure from `go list -deps -test ./pkg/types`: `pkg/types`, `pkg/errs`, `pkg/retry`.
- Full paginated changed-path inventory of PR #1159 has no pilot-directory overlap; its entity-ID spec
  delta remains adjacent work. The other active claims are recorded in the inventory.
- Before experiments, the process census showed no running Go compiler or test process; Docker was idle.
  This is a point-in-time observation, not a reservation of shared CPU.
- Initial claim CI passed at `228e0a546f411e168bab7b0cb5fed1aa91db33db`:
  [CI run](https://github.com/C360Studio/semstreams/actions/runs/35093868384) and
  [E2E run](https://github.com/C360Studio/semstreams/actions/runs/35093868414).
  These checks cover the empty claim, not later experiment artifacts.

## Execution plan

Independent SemStreams reviewer: DESIGN REVIEW PASS, no blocking corrections.
Reviewed plan SHA256: `40575dea3f68a92ca2b6136cccf81c4e65237d8071a11d89a9a63ece665c6bd9`.
The reviewer confirmed the corrected inventory identity and all 13 upstream source hashes.
Attempted refutation covered command failures misclassified as detections, symlink write-through,
escaped child process groups, differing generated inputs, and zero selections. The plan addresses each
through isolated controls, independent classification, paired replay, and bounded owned cleanup.
The verdict covers experimental method, not results or adoption.

The user previously authorized this bounded pilot, conditional on noninterference with existing claims.
The reviewed plan stays within that scope. No production or CI adoption is authorized by this checkpoint.

## Final implementation and evidence review

Independent SemStreams reviewer: APPROVE, no unresolved findings.
Reviewed all 15 staged files. SHA256 of `git diff --cached --binary origin/main` at review:
`8a05c0de6de02ea6f639b4606210a169dbc00566eb510cfc2d9d963e7c476053`.
This paragraph records that verdict after review and is not part of that earlier diff digest.
Evidence archive SHA256: `0c3e9a4ff2918ea56eb60406f146360bc3adef9e99d43cc294cd8a6f5bd577d7`.

The reviewer independently verified all 821 archived files against raw evidence and checked for unsafe
archive paths; all 30 source files against the pinned baseline and unchanged worktree; ten 0/1/0
manual replays and exact-input witnesses; selected-test execution and timing totals. The integer
self-comparison equivalence argument is valid over every Go `int`.

Cleanup and source-drift findings are resolved, including the Python-driver interruption/finally path.
Compile failures, unrelated exits, timeouts, the failing zero-mutation baseline, and unmeasured human
cost are accurately disclosed. The recommendation to defer adoption is supported by the evidence.
Python syntax and staged whitespace checks passed; the reviewer did not rerun mutation experiments.

## User constraint conformance

| Constraint | Evidence |
|---|---|
| Do not interfere with current claims | Only this directory is staged; separate claimed worktree; source copies from pinned Git; process groups and temporary paths limited to this pilot. |
| Evaluate utility before adoption | README recommendation and results; no production/root dependency/CI change. |
| Keep evidence reproducible | Pinned harness, exact commands, source/runner hashes, complete byte-verified archive, paired replay. |
