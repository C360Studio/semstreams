> **UNAPPROVED — DESIGN-ONLY.** This change is a durable investigation baton,
> not an accepted target state. It contains no spec delta and authorizes no
> runtime implementation or spec promotion.

## Why

GS-01 must make authority reads, authority recovery, and graph-ingest instance
safety predictable without adding another owner beside behavior that already
exists. A prior design attempt started from a prompted mechanism instead of an
independent repository inventory. It proposed `GRAPH_INGEST_ACTIVE`, but review
found that `GRAPH_STATUS` and graph-ingest already occupy that semantic territory.
The proposed NATS CLI requirement was also withdrawn.

The failed premise invalidates the design that followed it. Prior GS-01 acceptance is revoked/not granted. A fresh,
problem-only inventory has now completed independent review with `INVENTORY PASS`. Design is next; no options,
recommendation, target state, spec delta, or implementation task has been accepted or written.

## What Changes

- Establish this change as the durable GS-01 baton while investigation and
  design are unapproved.
- Record the accepted process gates: fresh inventory, independent inventory
  review, design, independent pre-owner design review, and explicit owner
  acceptance.
- Preserve the reviewed repository inventory and `INVENTORY PASS` evidence as the
  only accepted GS-01 evidence so far.
- Keep the design placeholder ready for options and a recommendation produced
  from that inventory, followed by independent pre-owner design review.
- Keep task truth explicit: inventory is reviewed, while design/recommendation
  and implementation remain pending.

## Non-goals

- No architecture recommendation, target state, API, data model, bucket, subject,
  status key, lease, catalog, lifecycle mechanism, or coordination primitive.
- No `GRAPH_INGEST_ACTIVE` or replacement mechanism.
- No requirement to use the NATS CLI for recovery, validation, or operation.
- No current-spec edit, change-spec delta, runtime code, test, migration, or
  compatibility behavior.
- No claim that authority-read, recovery, or graph-ingest instance-safety design
  has passed review or received owner approval.
- No change to any downstream `sem*` product. This design-only baton is consumed
  only by SemStreams maintainers and reviewers.

## Impact

- **Runtime impact:** none.
- **Spec impact:** none; no capability delta exists in this change.
- **Process impact:** the architect and reviewer contracts enforce separate
  inventory and design gates.
- **Program impact:** GS-01 remains the sole active increment. Reviewed inventory
  is complete; design is the next executable phase.
- **Promotion rule:** runtime work and spec promotion remain prohibited until the
  independently reviewed design receives explicit owner acceptance. After
  acceptance, this same GS-01 change gains the capability deltas and TDD tasks;
  no second change is opened.
