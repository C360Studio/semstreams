# Local validation

The final implementation snapshot is `evidence/final-source.json`, SHA256
`3a9b31e446490985399d5e8af0ac36111fe9b03a475239ffe88b6342af953c7e`, on local planning HEAD
`c9870a71e425445c5147a017c50c2f55b56e6f20` with the exact recorded working-tree changes. All 25 source hashes
were checked before and after the run. Baseline SHA256 is
`909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615` (297 entries / 90 resolutions).

`PATH=/private/tmp/gh1064-task-tools:$PATH task check:push` **PASS**, exit 0, 884.787 seconds wall time.
Task version: 3.53.1; Go version: 1.26.4 darwin/arm64. The exact status and complete output are retained in
`evidence/check-push-attempt3-status.json` and `evidence/check-push-attempt3.log` (SHA256
`14c06781b9e0b6f984f0cc72459ed4be465c52703f010a01b1dd1e14fad28aec`).

This gate ran cleanup admission, lint, build, integration/live_llm-tagged vet, schema generation/drift, contract tests,
the full ordinary race suite, and the canonical additive integration runner with race detection and its host lock.
The changed graph-ingest package passed integration in 50.736 seconds. The ordinary pass used valid Go cache entries
where reported; the integration pass supplied its canonical uncached execution. No live-LLM run or E2E tier is claimed.
No production/API/wire change triggers an additional breaking-change E2E requirement.

Two earlier attempts failed for concrete defects and are retained: context-parameter lint (13.199 seconds), then
four moved fixture annotation coordinates (208.448 seconds). Both were fixed and independently reviewed before this
successful run. This was not an unmodified rerun over a known flake. Integration did not start in either failed attempt.

Pre-archive `openspec validate --all --strict` also passed. Final archive/spec citation and contract checks, plus the
narrow archive review, are recorded separately after synchronization. Hosted CI is separate from local validation.

## PR handoff

Draft PR #1424 declares `Closes #1423`, references #1417/#1419, and carries `implemented-by: Sol`.
The reviewable PR description is prepared with source debt counts, completed local evidence and native limitations.
The parent candidate record is posted at
https://github.com/C360Studio/semstreams/issues/1417#issuecomment-5894050416.
It explicitly distinguishes the reviewed branch's 297-entry remainder from main's pre-merge 329 entries, keeps
#1417 open, and names the next refreshed rule batch plus remaining direct lifecycle-probe waits.
This handoff claims neither a merge nor issue closure. Hosted checks will be evaluated on the pushed final head.
