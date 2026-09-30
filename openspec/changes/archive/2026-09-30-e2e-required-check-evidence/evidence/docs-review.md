# #1222 documentation clarification review

Verdict: APPROVE for the bounded documentation/API-clarification slice as target documentation. This is not
implementation approval, proof that the target behavior or CI artifact retention is implemented, or independent
reapproval of my authored API handoff. Code review remains pending stable snapshots.

Base: fe6e2cc03e16f5db47e293f55939548572f204cc. Accepted design:
 a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c.

## Review history

Round1 CHANGES REQUESTED is preserved byte-for-byte at
`/private/tmp/semstreams-1222-docs-review-round1.md`, SHA256
 bc4f84b0861cf8315b31ebf399913bbe1767e2cdf2c2e527a3bb5d9f3d824f9b.
It contained one HIGH on misleading CI/release/nightly claims and one MEDIUM on diagnostic exit ambiguity.
This recheck is confined to their corrections and file identity. No code/tests/Docker/GitHub operations.

## Exact final documentation identities

- `docs/contributing/01-testing.md`: `81350f804389f61329c3d416788aac1c151a886f3009f1012b2a57f0c19e25cd`
- `docs/contributing/02-e2e-tests.md`: `2d550fcca5123d9bb40d0c16aa905d10ff74ddf25d6466701dd42fdcfa81202c`
- `.agents/skills/semstreams-preflight/SKILL.md`: `e91f4f8cb6c8a2ad7028a5dc3ee9dfccbd07dfcebceeeb5a02965f713e86e701`
- `openspec/changes/e2e-required-check-evidence/implementation-handoff.md`: `7b364cb1d3f19d5327cf7affaf2cfd50742eb19e526fe52ae164d5041a05ec76`

## Closed findings

The testing policy now expressly propagates failed REQUIRED observations and separately preserves diagnostic
outcomes without independently failing required acceptance. This matches Diagnostics remain explicit and preserves
the more detailed bootstrap exceptions through the canonical guide link. MEDIUM closed.

The E2E guide labels the local three-tier list as an example for changed core/graph paths. CI Integration now names
the actual statistical/slow-consumer PR and explicit-dispatch jobs, does not equate them with the local composite,
and states that statistical does not prove core shutdown, structural zero-ML or agentic behavior. It links release
selection/identity/evidence to release-candidate-proof and expressly denies that one semantic run or a green composite
authorizes release. Nightly wording is removed, #1117/#769/#1128 ownership retained, and open CI work explicitly does
not waive relevant local breaking-change evidence. HIGH closed.

The unchanged preflight pointer and appended implementation clarifications retain round1's conforming assessment.
The guide's target claims about report commands, adopted evidence and artifact retention must still be matched to
completed implementation and verification before merge; this documentation verdict does not establish those facts.
Inherited approximate timing remains unmeasured here, with calibration owned by existing work.

## Evidence and limits

Read docs01 updated paragraph64–76, docs02 local selection197–236 and CI/breaking paragraphs387–455; recomputed all
four hashes. The actual workflow comparison remains the immutable baseline read recorded in round1. No active code
was inspected and no checks were executed. Both findings are closed; no remaining blocking/high issue in this
bounded documentation slice.

APPROVE — documentation/API clarification scope only.
