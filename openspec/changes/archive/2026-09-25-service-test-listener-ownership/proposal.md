# Service test listener ownership

## Why

Issue #1120 records tests binding a TCP port, closing the listener, and asking the production server to bind the
same number later. Another process can take the released port. Independent inventory review confirmed the
18 calls named by the issue and nine more calls through equivalent helpers in the same service/metric owners.

## What Changes

The 27 bind-close-rebind calls across six test files are replaced with retained listeners or acquisition-refusal
observations. Shared HTTP tests use the existing ephemeral acquisition path and observe its assigned port.
Health and pprof tests use private seams while exercising the real serving and cleanup paths.

The additive `metric.Server.StartWithListener` method transfers listener ownership only on successful return.
Existing `Address` reports the actual owned endpoint, including correctly escaped scoped IPv6 addresses, and
retains its configured fallback before acquisition and after terminal cleanup. Standalone and Manager-owned
metrics share one private bridge into the concrete Server lifecycle. Startup tests surface early StartAll errors
while waiting for child entry or listener acquisition and release test gates before joining cleanup.

## Impact

The scope is the existing service health, shared HTTP, startup-observability, pprof, standalone metrics, and
concrete metric-server owners and tests. Disabled/default port meanings, native Start behavior, configured TLS,
context ancestry, startup ordering, and terminal ownership are preserved. No schema or E2E composition change is
introduced. Broader reusable E2E/composer API work remains with #1301; adjacent UDP, websocket, maxdelivery, and
agentic safe-restart work remains separate.

The owner accepted the independently reviewed design on 2026-09-25. `inventory.md` and `design.md` retain their
exact reviewed historical identities; `review.md` records acceptance, corrections, and review outcomes.
`conformance.md` maps accepted constraints to implementation. `evidence.md` preserves behavioral red/green checks,
controlled mutation observations, restoration checksums, and focused race results.
