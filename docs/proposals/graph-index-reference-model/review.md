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

## Successful status-reader limitation

Implementation inspection found that successful BucketLastSeq acquisition requires an SDK concrete type with
private construction state. No supported successful in-memory status fixture exists in the inspected SDK/test
surface. Unsafe layout access was rejected. The architect recorded the supported existing unit projection seam
in status-seam.md, SHA-256 `542e5ca819747dff59b989975f22d664297e11f1f535ce22ca1f4119d7a93e56`;
its eight repository pins passed mechanical verification.

The narrow correction received **DESIGN REVIEW PASS**, full design SHA-256
`866efc6c5796c963fcf8285680fd0c847e1285cf0db3b6d5e7461107289b1d7b`.
It uses real reconciliation/watermark/failure outcomes through existing production readiness projections and latch.
Cold readiness is observed through the canonical gate. Actual handlers prove exact sets after bootstrap and
classified refusal after injected write failure. The early-bootstrap mutation now targets the production latch.

This supersedes the initial proposed successful status-reader adapter and direct computeIndexStatus unit claim.
Actual computeIndexStatus assembly, server LastSeq and published-status evidence remain required in the existing
real-NATS confirmation. No runtime/helper change, SDK shim or new behavioral approval is implied.

## Implementation review

Independent implementation review initially requested two corrections: stale-work parity did not assert the
pending revision boundary, and hydration chose its head-bearing owner using map iteration. Both were corrected:
completed parity requires exact caught-up revisions, the older/newer work boundaries are asserted separately,
and hydration selects the lowest live owner deterministically. Narrow hash comparison confirmed no unrelated edits.

Final verdict: **APPROVE** at a1307297 plus the three new test files. Approved SHA-256 values:

| Test file | SHA-256 |
| --- | --- |
| reconciliation_model_helpers_test.go | 88b74689b3703094282577e21c893b51666722beb08856033092a52efba6db27 |
| reconciliation_model_test.go | 69aecde621c73224fd2e3a9638b69c72bb46e1162a63b83e1fcf421c7ad74397 |
| reconciliation_prop_test.go | c2d5dea64bbc63312637f8fa8f3a83a8efd1af90bef350cbd63ac46ce3e1b0d5 |

The reviewer inspected all three files and independently verified the raw patches, logs, commands and restored
source checksums. Seeds 1292/1293 and default100 passed under race in 2.29/2.25/2.40 seconds. All four production
mutations triggered their intended assertions. A synthetic suffix mismatch shrank to one action, replayed the same
assertion and passed after restoration. The reviewer ran no tests and wrote no files.

[Execution evidence](evidence/execution.md) and [sensitivity evidence](evidence/evidence.md) retain exact commands,
source identities, patches and logs. SHA256SUMS binds the persisted copies to the reviewed artifacts.
Full pre-push gates and existing real-NATS confirmation remain pending at this checkpoint.

## Broker confirmation checkpoint

The existing real-NATS replacement/restart and failure/repair witnesses subsequently passed through the canonical
runner on code candidate 684f2ead, with both named executions retained in evidence/broker-confirmation.log.
No test or runtime source changed after implementation approval. This supplies the previously pending broker
confirmation; the model claim's full pre-push gate and hosted CI remain pending.
