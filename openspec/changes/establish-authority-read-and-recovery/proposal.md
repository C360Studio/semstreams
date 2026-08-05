> **REVISION 35 REJECTED — REVISION 36 INVENTORY ACTIVE.** Owner redirection rejected revision 35 as a target. This is
> again an inventory-only baton with no accepted target, capability delta, runtime implementation, or spec promotion.

## Why

GS-01 must make authority reads and graph-ingest instance safety predictable
without adding another owner beside behavior that already exists. A prior
design attempt started from a prompted mechanism instead of an
independent repository inventory. It proposed `GRAPH_INGEST_ACTIVE`, but review
found that `GRAPH_STATUS` and graph-ingest already occupy that semantic territory.
The proposed NATS CLI requirement was also withdrawn.

The fresh inventory and suffix addendum have `INVENTORY PASS`. Revision 35 later passed design review but over-scoped
disaster recovery and displaced exact-read plus graph-ingest-safety obligations. The owner rejected it on 2026-08-05.
Revision 36 returned to inventory and received independent `INVENTORY PASS`. Replacement design is now active; it
remains unapproved.

## What Changes

- Establish this change as the durable GS-01 baton while investigation and
  design are unapproved.
- Record the accepted process gates: fresh inventory, independent inventory
  review, design, independent pre-owner design review, and explicit owner
  acceptance.
- Preserve the reviewed repository inventory, suffix addendum, design evidence,
  rejected revisions, and independent review findings.
- Preserve revision 35 as content-addressed correction evidence, not target state.
- Preserve the exact revision-36 scope-loss inventory and independent inventory review.
- Keep task truth explicit: revision-36 inventory review, design, owner decision, capability deltas, and implementation
  remain gated in that order.

## Non-goals

- No owner-approved target state or runtime surface.
- No `GRAPH_INGEST_ACTIVE` or replacement mechanism.
- No requirement to use the NATS CLI for recovery, validation, or operation.
- No SemStreams checkpoint, backup, restore, attestation, recovery gate, or recovery-orchestration subsystem.
- No restriction to single-node NATS; edge/offline backup checkpoints remain operator-owned documentation guidance.
- No current-spec edit, change-spec delta, runtime code, test, migration, or
  compatibility behavior.
- No claim that reviewer clearance is owner approval.
- No change to any downstream `sem*` product. This design-only baton is consumed
  only by SemStreams maintainers and reviewers.

## Impact

- **Runtime impact:** none.
- **Spec impact:** none; no capability delta exists in this change.
- **Process impact:** the architect and reviewer contracts enforce separate
  inventory and design gates.
- **Program impact:** GS-01 remains the sole active increment. Revision-36 design is active after inventory pass.
- **Promotion rule:** runtime work and spec promotion remain prohibited until the
  independently reviewed design receives explicit owner acceptance. After
  acceptance, this same GS-01 change gains the capability deltas and TDD tasks;
  no second change is opened.
