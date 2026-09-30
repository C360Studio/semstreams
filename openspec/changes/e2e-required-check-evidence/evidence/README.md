# Evidence checkpoints

## Recovered checkpoint

The owner requested recovery and claiming on 2026-09-30. [Tasks](../tasks.md) identify the current Codex write owner,
worktree, remaining work and gates. The exact 78-path paused implementation is now committed locally at
`4b8204ba0da9a579bfb1ca869b646c36d3110b98`. The draft PR's remote head remains the initial claim; implementation
publication awaits the required gates. The 2026-09-27 pause is historical, not evidence of a current competing writer.

The [final source/document hashes](pause-final-source.sha256) identify the preserved implementation before recovery
record updates. The [final correction artifact hashes](pause-final-artifacts.sha256) identify the historical reports
and raw logs. All 150 files in the separate recovery archive matched their manifest before committing the snapshot.
The archive also retains ignored logs and local commits. Later task/evidence-index updates do not alter source code.

The recovered snapshot passed the three focused E2E Go packages with `-race` on 2026-09-30; see tasks for the exact
command and evidence location. This does not complete the pending Docker, Task propagation or broader report review.
The older [14-file pause snapshot](paused-source.sha256) and review records below retain their original limits.

## Latest corrections and their limits

- Result R1/R2: [author](result-fixes-round2.md), [approved review](implementation-review-round2.md).
- CLI C1-C4: [author](cli-corrections-round2.md), [digests](cli-corrections-round2.sha256),
  [approved review](cli-review-round2.md).
- Core K1: [author](core-k1-developer.md), [approved review](core-review-round2.md).
- Writer: [W1](w1.md), [W2](w2.md), [W1 resolved](w1-review.md), [W2 approved](w2-review.md).
- Inference: [corrections](inference-review-fix.md),
  [I2/I3 and scenario-side I1 approved](inference-review-round2.md).
- CLI C5/C6 and I1 bridge: [author](task-reporting.md),
  [C5/C6 approved; I1 remains HIGH](cli-review-round3.md).

The remaining I1 finding is a deterministic source trace: the real `semantic-fallback` execution path reaches an
unregistered authority variant and panics. It predates the declaration correction. The bridge test proves selection
and classification only. Reconcile the statistical Compose deployment and Claude's held authority work before
repairing the real lifecycle. The review did not execute Docker or claim an observed composed failure.

Writer W2 has retained [behavioral RED](w2-red.log), [package GREEN](w2-green.log) and [race GREEN](w2-race.log).
Failed terminal foreign identities/duplicates remain diagnostic while initialized declarations retain authority;
allowed later aggregate corrections no longer inherit the malformed submission as their declaration baseline.

Inference has [focused GREEN](inference-review-green.log), [race GREEN](inference-review-race.log),
[compiled weakening mutation RED](inference-review-mutant-final.log) and [restored
GREEN](inference-review-restored-final.log).
The HTTP fixture independently checks the controlled query/limit; accepting only a matching suffix makes both helper
and actual HTTP-stage foreign-identity assertions fail. Exact source and cp-backup restoration hashes match.
This supersedes the earlier forced-false experiment, which only tested over-rejection of healthy input.
It is not assembled ingestion/query proof or exploratory fuzzing.

CLI C5 has [initial-declaration RED](cli-c5-red.log) and [GREEN](cli-c5-green.log).
C6 has [false-execution RED](cli-execution-false-red.log) and [focused GREEN](report-provenance-green.log).
[Final CLI and Writer package checks](reporting-final-go-test.log) passed at the frozen source hashes.
The [selected-child provenance check](cli-child-provenance.log) validates retained log/manifest bytes and parent slot.
The reporter checkpoint records shell syntax, Task-listing, formatting and whitespace checks; these do not prove
Task execution, cleanup or exit propagation. Broad report/provenance and Task implementation review remains due.

Two Writer guard-removal mutations were refused by automatic approval before edits. The [consolidated
review](writer-review-round1.md)
explicitly accepts bounded deferral with reasons and remaining risk. Exact sensitivity for those experiments remains
UNVERIFIED; behavioral pre-fix RED and preservation controls are not relabeled as executed mutations.
No other required control, producer proof or assembled gate is waived.

Automatic approval also rejected broad workflow export of the entire E2E results directory, citing potential
sensitive logs, manifests, metadata or binaries without specific authorization. No workflow edit landed and no bypass
was attempted. CI artifact retention remains blocked, separately from the local evidence preserved here.

Core's [duplicate-count mutation](core-duplicate-mutant-red.log), [restoration](core-duplicate-restored-green.log),
and K1 bridge controls remain bounded core evidence. No Docker/integration/assembled E2E or implementation push gate
has run. Full coverage and release readiness are not established. Partial Task wrappers remain preserved; the
five-family composite and remaining scenario adoption are unfinished. #1397 is closed; #1421 has no waiver here.

## Design and historical checkpoints

The [design](../design.md) passed [independent review](design-review.md); its accepted SHA-256 is
`a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`.
[Owner acceptance](../acceptance.md) is current authority. Pending-acceptance language in frozen artifacts is
historical.
The accepted design was committed locally at `b1e8cad8`; implementation base `fe6e2cc0` integrates main through
`3dc4ccbef32e87096c6d998fde7e76e896cf2f3c` (#1402 and #1403).

Earlier [Result](result-developer.md), [CLI](runner-developer.md), [CLI pause](runner-pause.md),
[core pause](scenario-pause.md), [Task API](task-api-developer.md), [Writer](writer-consolidated.md) and
[inference](inference-adoption.md) reports retain their original snapshots. Later records above supersede their
pending/approval claims. First Result behavioral RED output was not separately retained. Initial inference RED was
missing-symbol compilation; it is not behavior evidence. Generated properties and fuzz seed replay do not establish
exploratory fuzzing. Historical GREEN does not prove later edits.

[Writer shape advice](writer-shape-review.md), [Task API advice](task-report-api-clarification.md) and
[manifest advice](manifest-shape-advice.md) with [assessment](manifest-shape-review.md) informed the accepted
implementation handoff. They are architecture advice, not implementation or owner-acceptance substitutes.
[Documentation review](docs-review.md) approved the bounded proposed guidance after [round
one](docs-review-round1.md);
later source conformance still needs final review.

The active delta and 57 current specs passed strict validation: 58 passed, 0 failed in
[post-acceptance validation](openspec-accepted.log). This is historical validation at the accepted delta checkpoint.

## Inventory and initial claim

[Inventory review](inventory-review.md) passed before design. The [focused inventory](../inventory.md),
[adopter seams](../adopter-seams.md), [gate inventory](gate-inventory.md),
[structural inventory](structural-inventory.md) and [adjacent claims](adjacent-claims.json) retain the baseline.
The inventory manifest is relative to the parent change. Source baseline is
`12ae633381b8b8b26c333efe5f5c8691cfa47fb4`; the survey seed uses
`fe9482b7f336e575317cfb45fd1ad7c40baf7904`. Historical pin verification checked 1,451 pins at their declared revisions,
not against later task truth. Original seed SHA-256:
`77d4d86dc932dd42315597269be9ed32d3a3610267d19cf9696a38419b25f045`.

At the initial claim head, [build](build.log) and [lint](lint.log) passed locally;
[hosted E2E Ladder](https://github.com/C360Studio/semstreams/actions/runs/36326127728) passed.
[Hosted CI](https://github.com/C360Studio/semstreams/actions/runs/36326127730) failed only the protocol-expected
strict OpenSpec check for the delta-less claim. These runs do not validate current implementation.
