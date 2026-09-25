# Service test listener ownership

## Why

Issue #1120 records tests binding a TCP port, closing the listener, and asking the production server to bind the
same number later. Another process can take the released port. Independent inventory review confirmed the
18 calls named by the issue and nine more calls through equivalent helpers in the same service/metric owners.

## What Changes

The owner accepted the independently reviewed design on 2026-09-25; implementation is now in progress.
The independently reviewed inventory identifies 27 calls across six test files. The design proposes one additive
`metric.Server.StartWithListener` method, accurate active-listener reporting through existing `Address`, private
health/pprof/service test seams, and tests that retain their acquired listeners through production cleanup.
Startup tests would report an early StartAll error while waiting for child entry, avoiding a misleading timeout.

## Impact

The proposed scope is the existing service health, shared HTTP, startup-observability, pprof, standalone metrics,
and concrete metric-server owners and tests. Current disabled/default port meanings and ownership ordering remain
constraints. Adjacent UDP, websocket, and maxdelivery helpers and agentic safe-restart claims have separate scope.

`inventory.md` is the frozen inventory checkpoint. `design.md` defines the proposed ownership contract, alternatives,
and verification slice. `review.md` records exact reviewed identities. Independent design review and explicit owner
acceptance are recorded in `review.md`. Broader E2E/composer API work remains with #1301.
