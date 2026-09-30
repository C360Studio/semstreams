# #1222 inference correction re-review

Mode: bounded, read-only implementation re-review of the seven inference/slow-consumer
scenario files named below, against `/private/tmp/semstreams-1222-inference-review.md`
and the accepted `e2e-required-check-evidence` target. Base
`fe6e2cc03e16f5db47e293f55939548572f204cc`. No source edits, tests, Docker,
Git mutations or GitHub actions were performed by this reviewer. CLI bridge source
is concurrently reporter-owned and was inspected only as an adjacent consumer.

## Disposition

**APPROVE the bounded seven-file inference corrections.** I2 and I3 are resolved.
I1's scenario-side refusal is resolved, but whole-path fallback lifecycle proof is
still pending the separate CLI bridge checkpoint/review; this verdict does not
certify I1 end to end or the assembled E2E feature.

- **I1 scenario side resolved:** `tiered.go:563–568` bypasses `DeclareChecks` only
  for explicit `semantic-fallback`; supplied IDs on required/other variants still
  call the strict declaration boundary. `tiered_required_evidence.go:11–16`
  returns no catalog for fallback/unknown, and the new catalog test confirms
  strict `DeclareChecks` rejects an empty required set. `Execute` then proceeds
  to authority resolution and stages (`tiered.go:570–592`), retaining legacy
  unattested behavior at the runner's nonadopted path. Current adjacent
  `cmd/e2e/runner.go:24–33` treats an empty catalog as nonadopted, and its test
  resolves/constructs fallback; those evolving CLI bytes were not frozen here.
  No retained test runs the real fallback scenario through Setup, Execute and
  Teardown with supplied IDs, so the prior requested actual lifecycle bridge
  remains **UNVERIFIED** and belongs to the reporter checkpoint.
- **I2 resolved:** `TestTieredControlledSearchRecordsActualQueryResponse`
  (`tiered_required_evidence_test.go:55–96`) serves a local GraphQL endpoint,
  decodes the POST query and variables, and supplies a fixture or foreign full
  entity ID. `executeVerifySearchQuality` uses the real search Executor HTTP
  path (`validate_search.go:20–52`) and records the named observation. The
  independent literal controlled query and limit prevent a no-query shortcut.
  The author's temporary exact-ID-to-suffix mutant has source SHA-256
  `6815d7ff83a86c8a85655a9bd1d1561d8b2aded70c761a4c6cb12f62345cfb9f`:
  I reproduced that digest in memory from the retained backup with exactly one
  replacement, without editing source. Its compiled log reaches both the helper
  `wrong_identity` assertion and actual HTTP-stage `foreign_identity` assertion;
  both fail on wrongful `passed`/nil. The restored source and both retained cp
  backups match SHA-256 `78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423`,
  and the restored test log reports pass. This proves sensitivity to the bounded
  weakened-identity acceptance mutation. It is not assembled ingestion proof.
- **I3 resolved:** `core_slow_consumer.go:216,230–233` uses map key presence,
  with `expected=absent` and `actual=present=<bool>,value=<value>` in the named
  observation. `TestSlowConsumerDropAvailabilityRequiresAbsentKey`
  (`tiered_required_evidence_test.go:124–163`) drives absent, present-null and
  present-false through the existing recorder/finalizer. Absence completes;
  both present forms retain the ninth failed observation and finalization fails.
  Earlier conditions remain observable; later unevaluated checks remain missing.

## Exact snapshot and evidence

All seven current SHA-256 values match the author checkpoint before this report:

| File under `test/e2e/scenarios/` | SHA-256 |
| --- | --- |
| `tiered.go` | `8a48b4bcaf1e85f26a3d456f8ca6864f958473636f312ba8234248ea21edb3a8` |
| `tiered_structural.go` | `fc8a5c603a5e765b4dcbe58e02f7043b7bd6ab68d7dbba5e7b5e3fd771db08b2` |
| `validate_infra.go` | `991aac171c6b809682e07013fd68ae3719274a9a0cb9043152f96e0ec020a298` |
| `validate_search.go` | `b270b6386cb2abe0f278c1036209808bebc4a67df07ea04de18afeee6dcd3b41` |
| `core_slow_consumer.go` | `2f349e4ae11d1521a4576bc2f4850f2f5811043dbe1115096d971d23c01eb8b9` |
| `tiered_required_evidence.go` | `78751562110e6313984f69a93f870b434dbd89d2a649b10986ef027a8c5e4423` |
| `tiered_required_evidence_test.go` | `ceeb724ca7e00c2f0f66b9523e8cc5de3f7821407d35bd0cf0b821d3c89504d7` |

Retained logs under `/private/tmp/semstreams-1222-inference-review-` match the
author's hashes: `green.log` `d06557d24df87948085d0c3f85c8d0ffe24a0b4928e1c7947f53dbea71408cc3`;
`race.log` `044f976b6f600372227192aa12834e8dc349070bfcf96b0a5735e6af3af36811`;
`mutant-final.log` `824eb208a292f7c9a5d0520ee1ec3605b7af98f1fbc90dd504f4dce8bbc2428a`;
`restored-final.log` `042cef319db3d846bc7aa20c861d7cd01fef49b0943266cf3a7a6b0185fa09b1`.
The author checkpoint records the exact focused and race commands; these single-line
green logs themselves do not record argv. The mutation log independently displays
the two intended assertion failures. No claim of fuzz exploration is made.

Current `tasks.md:68–70` still says inference corrections are in progress and calls
the accepted mutation sensitivity unverified; task truth needs reconciliation
before final integration. This is separate from the bounded scenario-code verdict.
