# Independent implementation review

Reviewer: `semstreams-reviewer` (`test_reliability_review`), 2026-09-29.

**IMPLEMENTATION APPROVE**

The reviewer verified final source-freeze SHA256
`a3ea3c6fcdd762e299dfe37a7349b49eef7d2db262fbc9636947f72dfc64ec25`: all 25 source hashes match. Its only changes
from the preceding reviewed snapshot are the approved join-containment and immediate-readiness corrections.
Installed baseline `ab8f51cb3fccf5536b8602f206c62c8536256eface79c517753418082b91e502` matches the separately
approved reconciliation; its final installed guard passed in 8.508 seconds.

Approval covers the Go implementation, exact baseline changes, policy delta and focused proof record. Source-only
skipped repairs, compositional completion evidence and native Stop limitations remain accurately distinguished.
Full canonical gates and final archive/spec synchronization review remain separate obligations.

## Findings resolved before approval

Earlier bounded reviews caught the stale-cache reader's assertion-exit ownership, helper transfer proof fidelity
and explicit-fence error admission; their corrections and limits are recorded in the companion review files.

Full review found two bare failure-path waits and an unobserved timeout exit in the new owned-closure proof. The
corrected example releases Closed once on every exit and independently bounds the owned Stop join. It reports
incomplete cleanup explicitly. Result reads synchronize through stopDone; there is no second terminal attempt.
The corrected targeted race case passed in 1.270 seconds package time (`evidence/owner-join-final-race.jsonl`).
This is one selected case, not another run of every owner example.

The readiness fence now immediately refuses an unjoined status producer after successful Stop. Healthy selection,
the compiling omitted-fence mutation, and checksum-restored selection all ran through the canonical integration
runner. The mutant reached the intended assertion in 3.250 seconds wall time. The exact edit, source identities,
command and outcome are in `evidence/readiness-immediate-mutant.json`; its healthy/restored logs are retained.

The reviewer approved the exact one-record manual classification separately, before root installation. There are
297 preserved legacy entries / 90 manual resolutions: 32 exact graph-ingest entries removed, 89 old resolutions
unchanged, one verified native cancel callback added. No analyzer or guard relaxation was made.

## Accepted limits

Existing production-owner tests, new synchronous wrapper examples and real integration observations provide
separate completion/order witnesses. The Closed-only new example does not alone prove keyed or substrate order.
The six existing skips remain, and the three repaired skipped source bodies are not runtime proof.
Separate cleanup-duration measurement is deferred with the reviewer's acceptance; failure latencies and observed
completion support this bounded repair without a latency-distribution, speedup or total native wall-time claim.
The policy delta explicitly retains unchanged legacy debt; it does not certify those fixtures as compliant.

## Subsequent gate result

The first full push attempt stopped at lint after 13.199 seconds, before integration: `context-as-argument` rejected
the private finish/provisionalFinish signatures. The failure is retained in `evidence/check-push-attempt1-status.json`
and its log. A mechanical context-first signature/caller correction and exact dependency refresh are required;
the source hashes above remain the reviewed pre-correction snapshot, not the final push identity.

## Final mechanical correction review

**Mechanical correction and exact baseline refresh APPROVED**, same independent reviewer, 2026-09-29.

All 25 source hashes in `evidence/context-first-final-source.json` (SHA256
`5e1fabfe88d20d76d2c705d37d6098a9b6049cfafac40ea912198fdc06bbc1c7`) match. Its 24 changed files contain only two
context-first signature changes and 84 matching argument reorderings. Behavior and test oracles are unchanged.
Focused race passed in 1.505 seconds; scoped pinned revive and integration-tagged compilation passed.
The reviewer requires no repeated Docker or mutation run for this mechanical correction.

Approved and installed baseline SHA256 is now
`909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`. Its sole change from the previously approved
manifest is the canonical `graphIngestTestOwner.finish` dependency fingerprint
`7c09d338f13fda8611e6eb4c42456ff8f2750f48323a0fcf071f3174621a40e3`; classification and every other record are
unchanged. The full gate restarts from cleanup admission on this reviewed source. This section supersedes the
pre-correction source identity above, while retaining its review and failure history.

## Source annotation correction

The second full gate passed cleanup admission, lint, build, tagged vet, schema and contracts, then failed the
repository entity-ID annotation audit. Four malformed-ID fixture coordinates across two files had moved when cleanup source was
edited, leaving their line-number annotations stale. The gate completed failed in 208.448 seconds; integration
never started. It had already ended before root's cancellation attempt, so no signal was sent.
The exact outcome is retained in `evidence/check-push-attempt2-status.json` and its log. This is a source-annotation
failure requiring an exact fix, not a flake or a successful rerun.

## Final annotation review

**Annotation correction APPROVED**, same independent reviewer, 2026-09-29. Packet
`evidence/audit-annotations-correction.json` records exactly four existing comment line-number substitutions across
two files. Each corrected line and unchanged column points to the intended malformed-ID fixture. No code, value
or classification changed. The focused full-repository audit passed in 1.846 seconds; no repeated Docker or mutation
experiment is required for comment-only corrections.

The final 25-file source identity is `evidence/final-source.json`, SHA256
`3a9b31e446490985399d5e8af0ac36111fe9b03a475239ffe88b6342af953c7e`. Only the component and hierarchy test-file hashes
changed from the preceding approved freeze; baseline remains `909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`.
This is the current implementation checkpoint for full validation and archive review.

The observed common-gate failure-visibility gap is recorded on existing #1293:
https://github.com/C360Studio/semstreams/issues/1293#issuecomment-5893736177.
