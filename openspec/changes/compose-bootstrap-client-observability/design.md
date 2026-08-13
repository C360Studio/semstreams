# Bootstrap client observability composition design

## Accepted evidence and scope

- Accepted complete inventory and design: `docs/proposals/gh955-bootstrap-logger-design.md`.
- Accepted inventory body SHA-256:
  `a78e4c894a1c2c1b3819296e561964381e95235402e051ea8ad0a2ad5940a5ba`.
- Independent inventory review returned `INVENTORY PASS`; independent pre-owner review returned `DESIGN PASS` after
  resolving inner-policy ownership, stream-ordering, E2E counter, and public type-identity findings.
- Owner accepted R1-R15 and added logger-identity ruling R16 on 2026-08-13.

Both binaries create one configured local handler before NATS construction. Production composes that handler with the
existing WARN/ERROR counter; E2E keeps local output only. The client logger derives from this graph with
`component=natsclient`, and the client receives it plus the already-created metrics registry through existing options
before `Connect`. The config-manager logger derives from the same local handler with `component=config-manager`.

After config arbitration, the roots revalidate the effective configuration, verify NATS limits, and ensure streams
from effective declarations. Only then may production compose its steady-state process logger with an optional NATS
handler. The client and config-manager retain their non-forwarding logger graphs, making recursion exclusion structural.

One internal log-forwarder policy resolver owns inner decode, INFO defaulting, normalization, and validation for boot
and service construction. The structural outer resolver remains service-agnostic and disabled entries are not decoded.
`service.LogForwarderConfig` remains the same named public type and delegates/translates at the package boundary.

## Binding rulings

- R1-R15 are recorded in the accepted complete design.
- R16 requires every logger graph to reuse the same configured local handler and common base attributes. Component
  children add only required identity/destinations. Shared composition never silently creates or falls back to a new
  logger or handler instance.

No canonical decision skill triggers: the change adds no communication path, orchestration behavior, payload, or query
access. No ADR is warranted because the change is reversible boot composition under ADR-058.
