# Graph-index reference-model review record

## Inventory review

Independent role: `semstreams-reviewer`; model `gpt-6-astra`, high reasoning; September 26, 2026.
Baseline: `316170b1cf26e19a329626857819d582bcf30721`.

Initial verdict: **INVENTORY PASS** on the 76-pin artifact, SHA-256
`5f347cbd6858a90c8092a2dedcfaf0bd4549e44d43df06b3c12755e64cbafad5`.
The reviewer independently rederived authority, source ownership, ordered reconciliation, repair, watermark
and readiness seams and reproduced the scoped zero-match searches. No blocking inventory gaps were found.

The reviewer traced the legacy deletion paragraph to `6b51d21a`, preceding accepted ADR-077 section 5 and the
source-owned requirement promoted in `6d02cdab`. The existing accepted decision governs test design; no new
owner ruling is necessary. Preserve the stale paragraph's inconsistency without introducing new guarantees
or claiming activation/performance evidence.

The coordinator incorporated four literal ADR pins and the authority-history supplement. Narrow independent
verification returned **INVENTORY PASS** for the materialized `inventory.md`, SHA-256
`ec2b8ef0431b6e426e93b787d8edb7eda27cb11fab09c752fccbfbd051059677`.
The coordinator's `task inventory:verify -- docs/proposals/graph-index-reference-model/inventory.md` passed
80/80 pins, zero drift/moved/ambiguous/malformed/unparsed entries.

Scope: inventory only. No design verdict, runtime tests, Docker operations, or reviewer writes.
Protected claims remained untouched. Subsequent design review is recorded below; implementation evidence remains pending.

## Design review and timing correction

Initial design SHA-256 `c542617b9580cf5aa8f744fd23544a19e49a8428644ecc1f75733a821cb87486` received
**DESIGN CHANGES REQUESTED**. The reviewer identified a concrete budget violation: persistent failure in all
100 cases implies at least 7.5 seconds of real retry waits, above the five-second new-unit-test ceiling.
The initial smaller command budget could not discharge the normal-suite cost. The reviewer also required
explicit second-seed evidence because Rapid's successful summary does not expose an automatically chosen seed.

The architect's narrow timing supplement was independently verified as **INVENTORY PASS** at baseline
`a0c11028a6c3e946f4041f73e5581c981a3ff707`, SHA-256
`bbdeb043a2f51b17ea5c5e0ea0609a0d63c302b69d910e5bb740bd392bbce871`.
Mechanical verification passed all 16 repository pins. Existing synctest use and external local-source hashes
were checked. Naively calling Rapid inside a bubble was rejected because it calls the unsupported T.Deadline.

Revised design SHA-256 `8771681248940da84675d4448d3124039b4777a2cfc18ce652d0d4455380518e` received
**DESIGN REVIEW PASS**. Rapid draws/asserts outside each fresh bubble; the synchronous driver returns copied
observations/errors, allowing shrinking/replay outside virtual time. Production retry delays stay unchanged.
The design requires explicit seeds and actual default-100, race-enabled measurements under the normal unit ceiling.

A coordinator-only compatibility spike supported this composition and intended mismatch detection; it is not
repository property, runtime-budget, or broker evidence. Actual-property execution, mutation/restoration, replay,
independent implementation review and broker confirmation remain required.

All review verdicts were read-only, with no tests or writes by the reviewer. They imply no new behavioral owner
approval. Existing owner-approved issue #1292 and the current instruction to continue authorize this proof-only
scope; a runtime or contract change remains outside it.
