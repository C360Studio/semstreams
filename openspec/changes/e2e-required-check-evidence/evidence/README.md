# Evidence checkpoints

## Current checkpoint

[The proposed design](../design.md) passed [independent design review](design-review.md).
Its SHA-256 is `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`.
[Inventory review](inventory-review.md) also passed after its bounded correction.
The owner subsequently accepted this design; [acceptance](../acceptance.md) is the current authority.
The reviews themselves remain advisory and do not establish implementation completion.

The design and review package is saved locally in the claim's isolated worktree. PR #1406 still has only the original
claim commit; no later push or full push gate is claimed. The source tree remains at `12ae6333`, with changes confined
to this OpenSpec directory. [The coordination refresh](coordination-refresh.json) records #1402's later merge and
active #1403/#1404 ownership. The implementation must reconcile that newer base before shared source edits.

Artifact checks after the corrected design: 1,451 source pins match their declared Git revisions; current inventory
and design manifests match; JSON parses; no whitespace or conflict-marker errors were found. Later task-truth edits
intentionally differ from the frozen baseline's task-document pins. Historical verification reads each declared
revision with `git show`, rather than asserting that old task checkboxes describe today's progress.

Latest `openspec validate --all --strict --no-interactive`: exit 1, 57 specs pass and the delta-less change fails;
[log](openspec.log). Adding an unaccepted spec delta merely to clear this check would violate the design gate.

The inventory review covers the exact files named by `inventory-manifest.sha256`, relative to the parent change.
The source baseline is `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`; the earlier survey seed uses
`fe9482b7f336e575317cfb45fd1ad7c40baf7904`. Only the initial claim documents differ between those revisions.

- [Focused inventory](../inventory.md) adds the measured execution and adopter boundaries.
- [Adopter seams](../adopter-seams.md) follows author, runner and evidence-consumer defaults.
- [Gate inventory](gate-inventory.md) preserves the earlier survey's task, workflow and binary enumeration.
- [Structural inventory](structural-inventory.md) records references, outcomes, consumers and query limitations.
- [Adjacent claims](adjacent-claims.json) snapshots live issue identities, placements and #1222 acceptance.

The original seed SHA-256 is `77d4d86dc932dd42315597269be9ed32d3a3610267d19cf9696a38419b25f045`.
Its copied form removes eight empty-line pins rejected by the canonical verifier; all 571 nonempty pins remain.
Original scratch paths in the evidence identify its provenance. These in-tree copies are the retained artifacts.
Pin verification establishes source agreement; the independent review assesses enumeration completeness.

## Verification before design

Local commands ran at source HEAD `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`, before inventory materialization:

- `task build:default`: exit 0; [log](build.log).
- `task lint`: exit 0; [log](lint.log).

Hosted checks for that same claim head completed on 2026-09-27:

- [E2E Ladder](https://github.com/C360Studio/semstreams/actions/runs/36326127728): success.
- [CI](https://github.com/C360Studio/semstreams/actions/runs/36326127730): Build, Schema Validation,
  Tier 1 API Compatibility and Test passed. Lint failed only at strict OpenSpec validation;
  the aggregate CI Status Check consequently failed.
- Strict validation reported 57 current specs passed and the delta-less claim failed. This is the initial-claim
  condition documented in `.agents/protocol.md`; the PR remains draft and is not merge-ready.

No local integration, Docker, E2E or paid-model run was started. These checks do not prove the proposed behavior,
which has not been designed or implemented at this checkpoint. Full push/implementation gates remain outstanding.
