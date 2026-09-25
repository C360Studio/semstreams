## 1. Unit tests (`natsclient/subscription_test.go`)

- [x] 1.1 Give the fake `SetClosedHandler` and a `fireClosed()` helper, and drop its status channel. Migrate the
  existing tests to it; where a test's intent changes, the design's behavior-change list says why.
- [x] 1.2 Connection closes mid-drain: native `Drain` returns `nil`, then the fake goes invalid. `Drain` is still
  blocked, and returns `nil` after `fireClosed()`. Use a bounded ctx.
- [x] 1.3 Native `Drain` returns `nats.ErrConnectionClosed`: `Drain` is still blocked, and returns `nil` after
  `fireClosed()`. Use a bounded ctx.

## 2. Integration test (`natsclient`, `//go:build integration`), with no sleeps

- [x] 2.1 Block the handler on `release`. Close the native connection with `GetNativeConnection().Close()`, then
  call `Drain(ctx 10s)`. Assert that `Drain` has not returned. Release the handler, then require `nil` and that the
  handler had already returned. This test also guards against a nats.go upgrade changing when the closed handler
  fires.
- [x] 2.2 `processor/agentic-tools/outcomes_integration_test.go:272`: change the assertion to `NoError` and trim
  the comment block at `:253-261`.

## 3. Implementation (`natsclient/client.go`)

- [x] 3.1 Make the `newSubscription` and `Drain` changes in `design.md`. The unexported `nativeSubscription`
  interface gains `SetClosedHandler` and drops `StatusChanged`.
- [x] 3.2 Rewrite the `Drain` doc comment: callbacks are joined; on connection loss, queued-but-undelivered
  messages are discarded (core NATS is at-most-once).

## 4. Mutation evidence

Inline the mutation diffs and outputs in a PR comment. The scratchpad alone does not count.

- [x] 4.1 Put back the wait on `StatusChanged(SubscriptionClosed)`: 1.2 and 2.1 go red.
- [x] 4.2 Return right after native `ErrConnectionClosed` without waiting on `done`: 1.3 and 2.1 go red.

## 5. Land

- [x] 5.1 Pass the gates `task check:push` runs, then a `semstreams-reviewer` pass.
- [ ] 5.2 Archive and sync the spec as the last content commit.
