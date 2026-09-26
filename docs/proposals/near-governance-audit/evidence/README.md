# Preserved NEAR audit evidence

Start with the [canonical golden instrument](../../../contributing/08-golden-edge-agent.md) and
[run issue #1315](https://github.com/C360Studio/semstreams/issues/1315). They own the procedure and execution work.
This directory preserves source evidence; it is not another handoff or task tracker.

## Authority and historical package

The [owner decision](https://github.com/C360Studio/semstreams/issues/1367#issuecomment-5811785203) adopts the bounded
P2/N1 extension and publication of v1.0.0-beta.164 as the run trigger. The audit has not run. The candidate v1
objective remains unadopted.

The reviewed [design](../design.md), [golden draft](../golden-draft.md), and [planning inventory](../inventory.md)
are frozen historical evidence. Their pending-adoption wording describes the earlier review checkpoint and is
superseded by the owner decision. Their bytes and adjacent checksum files are preserved. See the
[review record](../review.md) for the exact reviewed identities and subsequent decision provenance.

## Runtime exploration

[runtime-governance-inventory.txt](runtime-governance-inventory.txt) preserves the original enumeration byte for byte.
The preserved runtime-governance inventory is earlier exploratory evidence from the `c4a79fd5` baseline.
Its full baseline is retained in the artifact. The coordinating session reported mechanical verification of
966 pins across 1,039 lines; no independent completeness review is claimed. Its recorded SHA-256 is
`64bfe1d71588ef1ce96b57f5e7300099cbf114d6131a2f627f1983c878db49ec`.
It is historical source evidence, not a current-runtime assessment or the reviewed planning inventory.

## Source snapshots and verification

[SHA256SUMS](SHA256SUMS) records the eleven preserved evidence files using repository-relative paths. The
[planning checksum](../inventory.md.sha256) and [design manifest](../design-package.sha256) retain the original
reviewed package identities. Verify from the repository root; no session or temporary-directory source is required.

| Preserved artifact | Provenance and scope |
|---|---|
| [Owner decision](owner-decision.json) | Exact GitHub comment authorizing adoption and preparation, linked above. |
| [Prerequisite sequence](prerequisite-sequence.json) | Owner-transcribed restart/composition/tag sequence on #1362. |
| [Beta.164 milestone](beta164-milestone.json) | Milestone #4 metadata captured before execution. |
| [Beta.164 issues](beta164-issues.json) | Milestone issue snapshot; not a later completion claim. |
| [Releases](releases.json) | Release-list snapshot; execution must verify the live selected release. |
| [Capture time](captured-at.txt) | GitHub batch captured at 2026-09-24T09:49:04Z. |
| [Preparation checks](preparation-check-push.txt) | Full local pre-push log; integration did not run because the shared host lock was held. |
| [Historical pins](historical-pin-verification.txt) | Numbered literals checked against each artifact's declared Git base. |
| [Pre-adoption pins](runtime-inventory-current-verification.txt) | Runtime enumeration verifier result before canonical adoption. |
| [Reviewed package CI](reviewed-package-ci.json) | Hosted checks for historical package commit 952041882caff65a0e0fe102a7796219d220efaf. |

The historical pin check matched planning 46/46 and runtime exploration 966/966 with zero mismatches.
The separate pre-adoption check matched 966/966 at 952041882caff65a0e0fe102a7796219d220efaf. These establish literal
pin fidelity only, not completeness, runtime conformance, or human usability. The adopted golden may intentionally
differ from historical pins; those pins are preserved against their declared bases rather than silently refreshed.

The CI snapshot records successful hosted checks, including both E2E checks, for the historical reviewed package.
It does not establish a verification result for the later adoption revision or mean the NEAR audit has run.
Live prerequisite evidence remains on the [sequencing comment][sequence] and [beta.164 milestone][milestone];
release publication remains on the [repository releases][releases].

[sequence]: https://github.com/C360Studio/semstreams/issues/1362#issuecomment-5797928199
[milestone]: https://github.com/C360Studio/semstreams/milestone/4
[releases]: https://github.com/C360Studio/semstreams/releases

The coordinator associates the retained preparation check-push log with baseline
952041882caff65a0e0fe102a7796219d220efaf and canonical SHA-256
adcb08439864ce50d4ac2aae267711fa5dd813edf61df50adc13adcc774e72f0. The coordinator reported exit 201 after build, lint,
tagged vet, schema, contract, and unit-race stages passed. Integration was NOT RUN: the shared host lock was
held by PID 42141 at an elapsed 230 seconds. It was not queued or retried. Runtime code was unchanged; subsequent
evidence/index/review edits do not convert these checks into current-runtime or NEAR audit proof.
