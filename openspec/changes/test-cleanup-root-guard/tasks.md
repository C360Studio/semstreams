# Tasks

## 1. Inventory and design

- [x] 1.1 Produce a repository-first, line-pinned inventory of cleanup ownership, receiver/context classification,
  existing analyzers and baseline mechanisms, relevant current specs, and adjacent claims.
- [x] 1.2 Obtain independent INVENTORY PASS on the exact inventory artifact.
- [x] 1.3 Draft options, costs, classification and identity contracts, unknown-case handling, and verification plan.
- [x] 1.4 Obtain independent design review and reconcile with the owner's accepted issue requirements.
- [x] 1.5 Materialize the accepted specification delta and bounded implementation tasks.

## 2. Delivery

- [x] 2.1 Implement the accepted census and guard with test-first evidence and independently reviewed baseline.
- [x] 2.2 Record measured remediation batches and evidence limits; perform only accepted package repairs.
- [x] 2.3 Complete implementation review, focused verification, and required local preflight.
- [ ] 2.4 Archive the completed change and synchronize its capability specification as the last content commit.

## Implementation ownership and evidence

Inventory review passed at SHA-256 `18ae6cf5377c44c5e554f16499327dce9aeb5a06bcce2fc741141e86a4fb7683`.
Design revision 2 passed independent review at SHA-256
`a97d4145cc02afaeeed8d6051ca3e8ae6d54f2b369d793fbf0cb5a5838134be5`.
Exact snapshots and findings are retained in PR #1414 comments 5873595698, 5873745710, and 5873950793.
The accepted revision-2 design snapshot is also retained in the PR record.
Strict validation of the materialized `test-cleanup-policy` delta passed before implementation.

The developer owns the new `test/testinfra` classifier, typed fixtures, and census/report. The coordinator writes
baseline entries only from independently approved exact source records.
The coordinator owns the shared guard script, Task/CI/runner admission, existing runner fixture adaptations,
actual-entry-point proof, and documentation. Neither role approves the production baseline merely by generating it.
Task 2.1 is complete: the final classifier and exact manifest have passed independent source review;
current-source full preflight and archive remain separate tasks.

The census-driven scope correction passed independent design review and is recorded in PR #1414 comment 5874372507.
The coordinator accepted the correction against #1064's existing guard scope; no owner waiver or baseline approval
was inferred. The admission slice separately passed scoped implementation review. The final manifest has 334 debt entries and 86 exact source-backed resolutions,
with 445 canonical dependency fingerprints checked. The reconciled 1,402-site guard passes. Final classifier source
review is approved; full local preflight passed; archive/spec synchronization remains the final content step. See `evidence.md`
and `review/final-manifest-review.md` for hashes, executed checks, and the limits of each approval.
