# Graph-query cleanup tasks

- [x] Reconcile exact cleanup identities and adjacent owners in a line-pinned inventory; obtain independent INVENTORY PASS.
- [x] Refresh inventory against resumed main and receive independent review.
- [ ] Review and accept a bounded design with concrete failure proofs and current-spec delta.
- [ ] Implement the reviewed test-only ownership changes with test-first failure evidence.
- [ ] Reconcile the exact guard manifest without weakening unrelated decisions or debt admission.
- [ ] Pass focused race/integration proofs and the canonical required local gate stages.
- [ ] Obtain independent implementation review and resolve findings.
- [ ] Reconcile task/spec truth, archive as the last content commit and obtain archive/spec review.

## Current work

The #1435 repair is merged after its explicit owner waiver. Preserve the accepted inventory and refresh only
intervening facts before design review. #1421 remains open; the waiver does not transfer to this PR. No source
cleanup has been implemented and no graph-query debt count has changed.
