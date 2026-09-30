# Shared KV abort ownership

## Why

PR #1435's required hosted Test job failed in the adjacent predicate-layout smoke harness: a filtered KV listing
returned a deadline error and the subsequent client drain exhausted fifteen seconds. The earlier controlled
experiment used a forwarding facade and drained native Keys during cleanup. That cannot establish what the
unmodified framework abort path leaves running without diagnostic assistance.

## What Changes

Prepare one private, opt-in diagnostic using the real NewKVStore path and unchanged native Keys channel. Compare a
transparent control with a constructor-return gate that admits production collection after its actual child expires.
Observe production return, native senders and Client.Close separately, without an independent native channel drain.

The independently reviewed design and inventory are retained byte-for-byte beside this proposal. Their original
/tmp references identify the reviewed artifacts; inventory.md and design.md are the durable copies. The raw hosted
failure is preserved in evidence/ with its provenance and checksum. The previous archived observer change remains
unchanged. This new change re-enters reconciliation after that archive, under existing #1421 and draft PR #1435.

## Scope and authority

The coordinating session selects the reviewed test-only experiment under the user's instruction to continue the
current test-reliability work. This records no new public API or production policy, issue closure, historical-cause
ruling, or merge waiver. Source preparation and proof are authorized now; a native run remains gated on independent
implementation review and the fixed execution allowance. No later native rerun is authorized by this change.

The experiment will be removed from compiled tests after its exact source and evidence are retained, unless a
separately reviewed repair gives a permanent regression test a concrete purpose. A result alone does not fix CI.

## Impact

Private natsclient diagnostic and focused proof files, plus this evidence record. No production, SDK, runner,
existing graph-index harness, latency assertion, timeout, or cleanup-baseline changes in the experiment unit.
#1286 retains its separate budget/unit reconciliation; graph-query #1433/#1434 remains held at inventory.
