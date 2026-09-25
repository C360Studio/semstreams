## Why

`natsclient.(*Subscription).Drain` waits for nats.go to report `SubscriptionClosed`. In nats.go v1.52.0 that status
comes only from `removeSub`, and `checkDrained` skips `removeSub` when the connection is closed. So a drain that is
pending when the connection closes waits out the caller's whole context and returns `DeadlineExceeded`. Under
`context.Background()` it hangs forever, which is how #1370 found it. Every component that drains a subscription
stops during process shutdown, and that is exactly when connections die (#1372).

## What Changes

- `Subscription` waits on nats.go's closed handler (`SetClosedHandler`) instead of the `SubscriptionClosed`
  status. nats.go calls that handler once, after the subscription's delivery goroutine exits. By then every
  callback has returned, whether the subscription was drained, unsubscribed, or lost its connection.
- `Drain` treats a native `nats.ErrConnectionClosed` as terminal rather than as a failure. On connection loss it
  returns `nil` once the in-flight callback returns. The caller's context still wins.
- The `Drain` doc comment says that on connection loss, queued-but-undelivered messages are discarded (core NATS
  is at-most-once).

## Capabilities

### New Capabilities

- `nats-subscription-lifecycle`: when `Subscription.Drain` returns, and what it reports.

### Modified Capabilities

None. `jetstream-consumer-policy/spec.md:312` stays literally true, because that capability did not change `Drain`.

## Non-goals

These were designed, reviewed, and deliberately cut. The reasons are in `design.md` § Deliberately not handled.

- Special handling for a connection that closes while the subscription is being constructed.
- A log line or metric for messages dropped on connection loss.
- Exhaustive mutation coverage of every nats.go terminal path.

## Impact

- `natsclient/client.go`: `newSubscription`, `Subscription.Drain`, its doc comment, and the unexported
  `nativeSubscription` interface. No exported API changes.
- `natsclient/subscription_test.go` and one new integration test.
- `processor/agentic-tools/outcomes_integration_test.go:272`: the assertion `#1371` added becomes `NoError`.
- Commit class `fix(natsclient)`, with no `!` and no E2E tier (owner ruling 3). No sister repo reads the error
  values that change, so no migration note is owed.
