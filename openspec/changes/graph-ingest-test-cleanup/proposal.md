# Change: remediate graph-ingest test cleanup

## Why

Issue #1423 executes the graph-ingest batch of #1417 after shared test support landed in #1419.
The audit identified 32 unbounded terminal-cleanup entries across 16 test files. A finalizer that cannot
finish can obscure an earlier assertion failure until the package timeout. These entries are liabilities;
they are not evidence of 32 reproduced hangs.

## What Changes

This initial claim authorizes inventory of the exact current entries, their resource ownership, operation
and terminal contexts, Stop results, and component/substrate ordering. Apply the accepted #1417 cleanup
contracts after independent inventory review. Implementation choices and any spec delta remain pending
that evidence; this claim does not approve an unmeasured production change.

## Impact

- Primary surface: existing `processor/graph-ingest` tests and package-local test support.
- Guard surface: remove repaired exact identities from `test/testinfra/cleanup_baseline.json` without
  weakening admission or approving unresolved replacements.
- Preserve production lifecycle/API-contract behavior, causal regressions, and canonical runner ownership.
- #1293 retains gate runtime/cancellation/visibility; #1411 and #1412 retain their production scopes.
