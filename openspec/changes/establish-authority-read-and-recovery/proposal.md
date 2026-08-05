> **DESIGN REVIEW PASS — OWNER ACCEPTANCE PENDING.** This remains a design-only baton. It contains no capability delta
> and authorizes no runtime implementation or spec promotion.

## Why

GS-01 must make authority reads, authority recovery, and graph-ingest instance
safety predictable without adding another owner beside behavior that already
exists. A prior design attempt started from a prompted mechanism instead of an
independent repository inventory. It proposed `GRAPH_INGEST_ACTIVE`, but review
found that `GRAPH_STATUS` and graph-ingest already occupy that semantic territory.
The proposed NATS CLI requirement was also withdrawn.

The failed premise invalidated the design that followed it. The fresh inventory and suffix addendum have
`INVENTORY PASS`. The exact revision-35 contract now has independent `DESIGN REVIEW PASS`; explicit owner acceptance,
capability deltas, and implementation remain pending.

## What Changes

- Establish this change as the durable GS-01 baton while investigation and
  design are unapproved.
- Record the accepted process gates: fresh inventory, independent inventory
  review, design, independent pre-owner design review, and explicit owner
  acceptance.
- Preserve the reviewed repository inventory, suffix addendum, design evidence,
  rejected revisions, and independent review findings.
- Preserve the exact content-addressed revision-35 contract and reviewer approval.
- Present its bounded owner rulings without treating review as owner acceptance.
- Keep task truth explicit: inventory and design review are complete; owner decision, capability deltas, and
  implementation remain pending.

## Non-goals

- No owner-approved target state or runtime surface.
- No `GRAPH_INGEST_ACTIVE` or replacement mechanism.
- No requirement to use the NATS CLI for recovery, validation, or operation.
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
- **Program impact:** GS-01 remains the sole active increment. Inventory and design review are complete; owner decision
  is active.
- **Promotion rule:** runtime work and spec promotion remain prohibited until the
  independently reviewed design receives explicit owner acceptance. After
  acceptance, this same GS-01 change gains the capability deltas and TDD tasks;
  no second change is opened.
