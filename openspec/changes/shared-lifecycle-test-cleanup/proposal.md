# Change: Own terminal cleanup in shared lifecycle tests

## Why

Issue #1418 is the first bounded repair under #1417. The shared lifecycle suite has terminal finalizers without a
finite context or checked result, and its real rule adopter has a fallback cleanup/Start-cancellation ordering
question tracked in #1416. A good shared pattern must preserve the lifecycle contract and make failing cleanup
observable without leaving owned work behind or tearing down its substrate first.

## What Changes

This initial claim requests an inventory and independently reviewed design for the shared suite and its real rule
adopter. The inventory must distinguish deliberate contract operations from terminal cleanup, account for early
assertions and worker-created instances, and re-derive the #1416 condition before recommending a correction.

No helper API, timeout value, runtime lifecycle change, or baseline approval is selected by this claim.

## Boundaries

The parent #1417 remains open for all package repairs. #1416 retains its current issue and milestone until its exact
condition is reconciled. #1293 retains CI plumbing, #1411 production lifecycle consolidation, and #1412 nil-context
behavior. No blanket timeout substitution, generic watchdog, ignored unexpected cleanup error, or production rewrite.

## Impact

Expected inventory includes `component/lifecycle_test_suite.go`, its adopters and fixtures, the rule lifecycle
integration fixture, current cleanup-guard evidence dependencies, and existing testing guidance. Any changes to an
exported test-support API require adopter evidence and explicit design review before implementation.
