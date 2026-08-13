# Asynchronous NATS subscription-error attribution design

## Accepted evidence and scope

- Accepted inventory: `docs/proposals/gh950-slow-consumer-attribution-inventory.md`, body SHA-256
  `4e95a7623271621cce8aa8123d41c1ce20cf4c3f87c4d4a99e5219fc2d23e6b3`.
- Accepted complete design: `docs/proposals/gh950-slow-consumer-attribution-design.md`, body SHA-256
  `68bd465a93b4c010f0d4d8a6d09228a52bd5dde107e32701f341a8062c782c8b`.
- Owner accepted rulings R1-R12 on 2026-08-12.

`Client.handleError` remains the single owner. It already receives `*nats.Subscription` from the sole
`nats.ErrorHandler` registration and already owns the one `ERROR` / `NATS error` record. The change extends that
record directly; it adds no sibling classifier, diagnostics API, or runtime-state path.

## Adopter seam

The adopter is a developer outside this repository writing a SemStreams component that uses `natsclient.Subscribe`
or a KV watcher.

- **What must they know?** Nothing new. nats.go supplies subscription identity and cumulative known drops to the
  framework callback.
- **What happens if they do nothing?** Existing code continues to compile and run. When an async error has a
  subscription carrier, its existing ERROR record now identifies the subscribed subject and optional queue; known
  slow-consumer drops are included when available.
- **Where do they find out?** In the existing structured NATS error log and this capability spec.
- **What should they have to know?** Nothing about callback registration, pending-limit snapshots, drop querying,
  metrics, or logger composition. The framework observes the real event after it occurs.

## Behavior

The handler starts with the original `error` attribute. A nil subscription follows the existing generic path. A
nonnil subscription adds its subscribed pattern as `subject` and adds `queue` only when nonempty. Identity applies to
every subscription-bearing async error.

Only `errors.Is(err, nats.ErrSlowConsumer)` enters the drop branch. That branch calls `Dropped()` once. Success adds
the cumulative integer as `dropped`; failure omits `dropped` and adds `dropped_available=false`. The handler does not
read pending depth, high-water values, or limits, and it neither records a failure nor invokes or changes callbacks,
status, health, readiness, or circuit state.

The real-NATS proof gates the connection callback before delegating to `handleError`, blocks an actual async
subscription handler, sets its test-only pending limit to one, publishes a fixed additional count, and waits with
bounded context polling for the exact cumulative dropped count. Only then does it release the production handler and
compare the logged count with the independently observed count. No production pending-limit surface is added.

## Binding ruling conformance

| Ruling | Exact implementation/test evidence | Deviation |
|---|---|---|
| R1 — every nonnil subscription gets subject attribution | `natsclient/client.go:1707-1708`; ordinary-error tests at `natsclient/client_async_error_test.go:80-99` | None |
| R2 — only wrapped slow-consumer errors query drops | `natsclient/client.go:1712-1713`; ordinary artificial subscriptions prove no drop-unavailable field at `natsclient/client_async_error_test.go:80-99,127-137` | None |
| R3 — subject always; nonempty queue only | `natsclient/client.go:1707-1711`; `natsclient/client_async_error_test.go:80-99`; real-NATS queue omission at `natsclient/client_async_error_integration_test.go:115-119` | None |
| R4 — failed drop query omits count and reports unavailable | `natsclient/client.go:1713-1717`; closed/unbound subscription proof at `natsclient/client_async_error_test.go:100-108,127-132` | None |
| R5 — no pending/limit snapshots | Production handler is exhausted at `natsclient/client.go:1705-1724`; absent-field proof at `natsclient/client_async_error_test.go:133-137` | None |
| R6 — nil subscription preserves error-only shape | `natsclient/client.go:1706-1707,1722`; exact attribute-count proof at `natsclient/client_async_error_test.go:76-79,120-132` | None |
| R7 — no runtime/callback mutation | Production handler is exhausted at `natsclient/client.go:1705-1724`; state/callback proof at `natsclient/client_async_error_test.go:142-176` | None |
| R8 — no metric/export/config/status/health surface | Production diff is confined to the existing private handler at `natsclient/client.go:1705-1724`; tests add package-local helpers only | None |
| R9 — no pending-limit or logger-wiring expansion | Production diff is confined to `natsclient/client.go:1705-1724`; test-only real-NATS setup at `natsclient/client_async_error_integration_test.go:28-72`; logger composition is tracked by #955 | None |
| R10 — add `nats-client-diagnostics`; no ADR | `openspec/changes/attribute-nats-subscription-errors/specs/nats-client-diagnostics/spec.md`; no ADR file is part of the change | None |
| R11 — unit and synchronized real-NATS production-handler proof | `natsclient/client_async_error_test.go:69-177`; `natsclient/client_async_error_integration_test.go:24-164` | None |
| R12 — #954 remains the product E2E gap | `openspec/changes/attribute-nats-subscription-errors/proposal.md`; `openspec/changes/attribute-nats-subscription-errors/tasks.md` | None |

There are no deviations from the owner-approved rulings.
