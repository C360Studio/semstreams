# Design — authority reads and graph-ingest safety (GS-01)

> **REVISION 35 REJECTED — REVISION 36 INVENTORY ACTIVE.** Owner redirection on 2026-08-05 rejected revision 35 as a
> target design because disaster-recovery mechanisms displaced the original graph read/write foundation. This change is
> inventory-only again. It contains no accepted target state, capability delta, or runtime authorization.

## Owner correction

Revision 35 remains reviewed correction evidence. Its `DESIGN REVIEW PASS` means the artifact was internally coherent;
it does not make the design owner-approved. The owner accepted these audit findings:

- Stable physical tuple brackets satisfied revision 35's proposed checkpoint invariant; the invariant itself and its
  forensic actor-exclusivity/signing machinery are not program requirements.
- Closed maintenance NATS, generated NKeys, CONNZ/`/jsz` protocols, strict pinned response schemas, signed envelopes,
  the owned snapshot wire adapter, universal runtime credential migration, and multi-layer recovery gates were scope
  escalation.
- The general admitted authority value-plus-revision read and graph-ingest instance-safety pillars were displaced.
- Lifecycle History, index families, and suffix ownership must retain their assigned later increments.
- Operational durability is not a SemStreams capability: clustered NATS remains supported, and edge/offline operators
  maintain infrastructure backups as checkpoints. No framework checkpoint, restore, attestation, recovery gate, or
  recovery orchestration is in scope.

No runtime work may implement revision 35.

## Active inventory evidence

- [Revision-36 scope audit](scope-audit-r36.md): exact inventory at SHA-256
  `eca90d2eaafec75f02fa3a0ae243a95e8614daaa9dde385a1247fdd345a3ef02`.
- [Revision-36 inventory review](scope-audit-r36-review.md): independent `INVENTORY PASS`.
- [Reviewed fifth-pass inventory](reviewed-fifth-pass-inventory.md): original fresh repository inventory and
  `INVENTORY PASS`.
- [Reviewed revision-35 contract](reviewed-recovery-contract-r35.md): rejected target retained as correction evidence.
- [Native snapshot probe](native-snapshot-probe.md): owner-rejected historical mechanism measurement only.
- [Suffix inventory](suffix-inventory-addendum.md) and [review](suffix-inventory-review.md): current-state evidence
  only;
  future ownership remains unselected.

## Canonical GS-01 problem boundary

The corrected canonical program assigns three obligations:

1. One admitted exact authority result carrying canonical value and same-entry KV revision with classified outcomes.
2. Fail-closed authority read validation while preserving normal writer availability.
3. Graph-ingest single-active enforcement or accepted active/active proof.

The active change directory retains its historical name, but "recovery" means correction of architectural drift. It
does not authorize operational disaster-recovery work.

Revision 36 must preserve the existing increment map:

- GS-02: mutation outcomes and write seams.
- GS-03/GS-13: lifecycle H1-history declaration and documentation truth.
- GS-04–GS-10: owner-specific derived views.
- GS-05: current suffix capability disposition.
- GS-12: public query/front-door retirement or consolidation.

## Process gate

1. Materialize the revision-36 inventory-only scope handoff. Complete.
2. Obtain independent `INVENTORY PASS` on the exact artifact. Complete.
3. Frame genuine options, including do nothing and extension of existing owners. Active.
4. Obtain independent `DESIGN REVIEW PASS` on the exact revision-36 design.
5. Obtain explicit owner acceptance.
6. Only then add capability deltas, TDD tasks, or runtime code.

No revision-36 target design exists yet.
