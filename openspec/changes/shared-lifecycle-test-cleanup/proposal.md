# Change: Own terminal cleanup in shared lifecycle tests

## Why

Issue #1418 is the first bounded repair under #1417. The accepted inventory demonstrates skipped cleanup after early
assertions and Initialize/Start failures, discarded lifecycle errors, and an injected Stop wrapper that also intercepts
the finalizer. Adopters currently compensate with their own cleanup policy; the rule cohort's duplicate fallback also
obscures the difference between running terminal errors and owner-specific failed-Start retry.

## What Changes

Extend existing shared support internally while preserving exported signatures. Transfer each returned instance to a
lexical owner, perform checked finite terminal cleanup before canceling live Start authority, report failures promptly,
and stop new iterations while already-owned work finishes. Keep explicit abort/nil/repeated-Stop contract operations
separate. Finalize error-injection bases directly and apply checked ownership to lifecycle benchmarks.

Use the real rule factory and one existing NATS container to prove component-before-substrate order; remove its
redundant cleanup registry. Reconcile four exact manual-resolution dependency sets without changing cleanup admission.
Add component-lifecycle spec requirements and update the existing testing guidance with the tested ownership pattern.

## Boundaries and evidence

Accepted inventory: immutable checkpoint `32c01357`, SHA256
`1d225f08a8c1148a16e130ef4d00ff04728b932467753086d31558bbf003c424`.
The accompanying design and spec delta received independent DESIGN REVIEW PASS and coordinator acceptance;
`review/design-review.md` records the exact checkpoint and outstanding implementation obligations.
No production rule/config/boot changes, exported helper, generic watchdog, blanket baseline refresh or bulk package
repair. A finite context does not interrupt Stop or prove joining when the bound wins. This slice starts with zero
selected entries among the 334 legacy debt entries; no reduction in that count is promised.

#1417 remains open. #1416 receives an exact source/proof disposition, not an invented reproduction or automatic closure.
#1404 retains its active production ownership; #1293, #1411 and #1412 remain outside this repair.
