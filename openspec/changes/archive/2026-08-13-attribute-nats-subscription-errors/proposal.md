# Change: Attribute asynchronous NATS subscription errors

## Why

The NATS connection-wide asynchronous error callback receives the exact subscription involved in an inbound
delivery error, but `natsclient` currently discards that carrier and logs only the error. At scale, operators can see
that messages were dropped without knowing which subject or queue was affected. GitHub #950 records this measured
operator diagnosis gap.

## What changes

- Keep `Client.handleError` as the single connection-wide async-error owner.
- Add the subscription subject to every subscription-bearing error record and the queue only when it is nonempty.
- For errors matching `nats.ErrSlowConsumer`, add the cumulative known dropped-message count when `Dropped()`
  succeeds; otherwise report `dropped_available=false` without a second error record.
- Preserve the nil-subscription generic record and every existing runtime-state and callback behavior.
- Add unit and real-NATS race-tested coverage through the production handler.

## Impact

- New capability: `nats-client-diagnostics`.
- Production code: `natsclient/client.go` only.
- No metric, exported symbol, configuration, status/health field, pending-limit control, logger-wiring change, or ADR.
  Primary-binary logger composition is tracked separately by #955.
- Pending-limit ergonomics remain on #586. Product E2E coverage remains the recorded gap in #954; the direct
  real-NATS integration is the falsifiable production-carrier proof for this change.
