# Service test listener ownership

## Why

Issue #1120 records service tests selecting a TCP port by binding, closing the listener, and binding the same
number later. The kernel may allocate the released port to another process before the service starts. The
existing inventory identifies 18 calls in four test files; this claim will verify the listener owners and test
paths before selecting a repair.

## What Changes

Inventory and design phase only. No runtime behavior, public API, configuration meaning, or spec delta is
proposed in this checkpoint. The scope is #1120; the agentic safe-restart hardening has separate claims.

## Impact

Candidate surfaces are service health/shared HTTP tests, startup-observability tests, pprof tests, and the
listener ownership paths they exercise in `service` and `metric`. Independent inventory review precedes
options and design. Any required contract change follows independent design review and owner acceptance.
