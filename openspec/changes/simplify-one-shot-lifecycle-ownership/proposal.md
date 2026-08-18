# Change: Simplify one-shot lifecycle ownership

## Why

The active restart-safe target mixes terminal running Stop with failed-Start cleanup re-entry. It consequently requires
a stateful managed-consumer wrapper, resumable Stop/delete operations, retained results, lifecycle-local backlog, and
Client child catalogs despite no measured production caller for concurrent or resumable terminal Stop.

Failed Start does have real retained cleanup obligations. The target must preserve that authority while making normal
controlled shutdown follow direct native ownership and one terminal process lifetime.

## What changes

- Supersede only ADR-094's managed-consumer, resumable running-Stop, drain-and-delete, name-routed child-catalog, and
  retained repeated-result mechanics through ADR-095.
- Return exact native `jetstream.ConsumeContext` ownership after all fallible setup and before delivery begins.
- Make running Stop one-shot and direct: fence, native Drain/Shutdown, exact Closed, cancel, owner join, cleanup.
- Preserve `startDone`, bounded failed-Start rollback, retained `cleanupPending` authority, and reject another Start
  until cleanup completes.
- Reject duplicate local durable identity instead of stopping or replacing the incumbent.
- Separate exact backlog observation and namespace-scoped fixture/admin deletion from lifecycle handles.
- Retire `Generation`, `Operation`, `ManagedConsumer`, `DrainAndDelete`, Client child catalogs, name-routed lifecycle,
  the five `DeleteConsumerOnStop` knobs, and retained lifecycle result replay after their ordered migrations.
- Keep pre-pool graph poison outside the keyed guard, preserve its existing counted ACK-drop policy, and keep zero
  backlog distinct from semantic completeness.
- Require each effect lane to declare stable idempotency, durable progress/outbox, or explicit external at-most-once
  limits; use `DoubleAck(ctx)` only for paths with a declared server-confirmation SLO.
- Preserve boot-only composition, raw-root retirement, always-exit controlled shutdown, dirty recovery, and both
  controlled and dirty proof gates from PR #984.

## Capabilities

### New capability

- `restart-safe-shutdown`: corrected owner ordering, lifecycle/topology separation, settlement limits, and proof gates.

### Modified capabilities

- `service-shutdown`: completed repeated Stop is nil/no-op; failed Start retains cleanup authority.
- `jetstream-consumer-policy`: consumption commits once and returns native ownership; duplicates reject; observation is
  independent.
- `graph-ingest`: pre-pool poison and keyed durable convergence have distinct disposition and proof boundaries.

## Impact

- **Breaking target:** no SemStreams managed lifecycle wrapper, lifecycle catalog, deletion knob, or name-routed Stop.
- **Adopters:** retain one native handle; do not derive consumer names, backlog formulas, deletion policy, generation,
  or rejoin state.
- **Runtime truth:** this contract-only change claims no implementation or proof completion.
- **Release:** controlled and dirty real-process evidence remain mandatory before the breaking lifecycle lands.
- **Recovery authority:** [`recovery-ledger.md`](recovery-ledger.md) is the durable execution record, current baseline,
  completion vocabulary, and archive/tag gate for this change. A merged design or green document validation is not
  runtime completion.
