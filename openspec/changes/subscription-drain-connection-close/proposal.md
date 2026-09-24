## Why

`natsclient.(*Subscription).Drain` waits for a `SubscriptionClosed` status that nats.go never emits when the
connection closes. A drain that is pending when its connection closes therefore returns only when the caller's ctx
ends ([#1372](https://github.com/C360Studio/semstreams/issues/1372)). Connections tend to close during process
shutdown, and that is exactly when every component that drains a subscription runs `Stop`. Each such `Stop` spends
its whole budget and returns `context.DeadlineExceeded` for a subscription that is already gone. Under
`context.Background()` the wait is a hang, and that is how #1370 surfaced it.

## What Changes

- `Subscription.Drain` completes on the subscription's native terminal state instead of on a status event. It
  waits on the native closed handler, which nats.go calls after the subscription's in-flight callback has
  returned. nats.go calls it on a normal drain, an external unsubscribe, the max-reached path, and a connection
  close.
- Drain returns `nil` when the connection closes, on every path, including a connection that was already closed
  before `Drain` was called. A subscription that was already invalid when the wrapper was built (closed before the
  closed handler was registered) still returns `nats.ErrBadSubscription`. A native drain error other than the
  terminal sentinels is still returned.
- Drain after an external `Unsubscribe` now joins the in-flight callback. Today it returns while the callback may
  still be running.
- `Client.handleClosed` logs one Warn line saying that messages queued but not yet delivered on core subscriptions
  may be dropped. There is no metric (owner ruling 4).
- The `Drain` doc comment states the connection-close contract: callbacks are still joined, and queued-but-undelivered
  messages are discarded (core NATS at-most-once).
- The #1371 test assertion that expected `ErrBadSubscription` from a `Stop` after the connection closed now expects
  no error.

The signature, the exported surface, and the ctx-wins and rejoin semantics are unchanged. This is not a breaking
change: the commit class is `fix(natsclient)` with no `!`, and no E2E tier is owed (owner ruling 3).

## Capabilities

### New Capabilities

- `nats-subscription-lifecycle`: the completion contract of a core NATS subscription handle's `Drain`. It defines
  when Drain returns, which callbacks it joins, and what it reports when the subscription or its connection is
  already gone.

### Modified Capabilities

None. `openspec/specs/jetstream-consumer-policy/spec.md:312` ("`Subscription.Drain(context.Context)` behavior
remains unchanged by this capability") stays as written (owner ruling 2). This change sets `Drain`'s behavior in its
own capability.

## Non-goals

- Counting or recovering messages dropped at a connection close. Core NATS is at-most-once, and after a close the
  native `Pending` returns `ErrBadSubscription`, so the dropped count cannot be read.
- A metric for the dropped-message signal (owner ruling 4: a log line only, for now).
- JetStream consumer handles. `ConsumeContext.Closed()` already fires on connection loss (#1372 evidence), so the
  delivery-lane `binding.Closed()` waits are not affected.
- `Client.Close` and connection-level drain (`jetstream-consumer-policy`).
- A typed "connection closed during drain" error (owner ruling 1 chose `nil`).

## Impact

- Code: `natsclient/client.go` (`nativeSubscription`, `Subscription`, `newSubscription`, `Drain` and its doc
  comment, `handleClosed`); `natsclient/subscription_test.go`; a new `natsclient` integration test;
  `processor/agentic-tools/outcomes_integration_test.go:253-274`.
- Callers: 29 non-test `Subscription.Drain` references (gopls), in processors, outputs, storage, examples, and
  `service/message_logger.go`. None branches on the error value. Each one's Stop now returns promptly and cleanly
  when the connection closes under it.
- Consumers: every `sem*` product that composes SemStreams components gets this through component `Stop`. A
  read-only scan of the local sister checkouts on 2026-09-24 found `natsclient.Subscription` held in semboids,
  semdev, semdragon, semmachina, semops, semsage, semsource and semspec, and `ErrBadSubscription` named in none of
  them. No migration note is owed.
- Dependencies: none added. The contract rests on nats.go v1.52.0's `SetClosedHandler` timing, which the new
  integration test guards.
