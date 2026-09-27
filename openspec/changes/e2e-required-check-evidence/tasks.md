# Tasks

## Inventory and design

- [x] Produce the surface inventory at the exact baseline, including existing selectors, outcomes, writers,
      diagnostics, consumers, contracts, active claims and the same problem shapes under other spellings.
- [x] Produce the adopter seam inventory for scenario authors, runner/CI users and evidence consumers.
- [x] Obtain independent INVENTORY PASS and preserve the reviewed checkpoint.
- [x] After inventory review, frame bounded options and costs, draft the design and state its invariants.
- [x] Obtain explicit owner acceptance of the independently reviewed design before implementation or spec deltas.

## Implementation and proof

- [ ] After acceptance, implement the agreed behavior and representative failure controls through existing surfaces.
- [ ] Reconcile the active config/recovery changes before editing shared scenario/authority files.
- [ ] Update canonical testing documentation and the accepted capability delta to match implemented behavior.
- [ ] Complete appropriate verification and independent implementation review, preserving exact evidence and limits.
- [ ] Reconcile and archive the accepted change with spec synchronization as the final content commit.

## Current checkpoint

The owner accepted the independently reviewed design; `acceptance.md` records the exact design/review identity
and issue decision. Implementation and its spec delta may proceed. The frozen design and review artifacts retain
their historical wording; this current checkpoint supersedes their pending-owner-acceptance status.

Reconcile newer main and live ownership before shared edits. #1404 / #1188 is still active; begin the isolated
Result/Writer/CLI surfaces. Full verification, implementation review and archive/spec synchronization remain open.
