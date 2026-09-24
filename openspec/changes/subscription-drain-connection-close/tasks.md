## 1. Unit tests on the fake (`natsclient/subscription_test.go`), red first

- [ ] 1.1 Change the fake: add `SetClosedHandler` (stores the handler) and `fireClosed()` (calls it once), and drop
      the `status` channel, `StatusChanged`, and `statuses`. `Unsubscribe` and `closeOnDrain` mark the fake invalid
      and do not fire the handler; each test calls `fireClosed()` explicitly.
- [ ] 1.2 Add cases:
      - close mid-drain: `fireClosed()` while `Drain` is pending gives `nil`;
      - already closed: the fake is invalid after the handle is built, native `Drain` returns `nil`, then
        `fireClosed()` gives `nil`;
      - native `ErrConnectionClosed`: after `drainCalled`, assert with a non-blocking select that `Drain` is still
        blocked; `fireClosed()` then gives `nil`;
      - join: the fake is invalid but the handler has not fired, so `Drain` stays blocked (checked with a
        non-blocking select after `drainCalled`); `fireClosed()` then gives `nil`.
- [ ] 1.3 Keep the born-invalid case (`:91`) returning `ErrBadSubscription` with zero native `Drain` calls. Give it a
      bounded ctx (not `context.Background()`), so mutation 4.3 fails instead of hanging.
- [ ] 1.4 Migrate `:63`, `:120`, `:149` and `:162` from `native.status` to `fireClosed()`. Drop the `statuses`
      assertion at `:78`. Migrate `:81`: native `Drain` is now called once and the test fires the handler. Migrate
      `:100` so the handler fires. Keep `:111`, the sticky other native error.

## 2. Integration tests (`natsclient`, `//go:build integration`), deterministic with no sleeps

- [ ] 2.1 Pending-close test:
      - the handler closes `entered`, then blocks on `release`;
      - publish and `Flush`, then wait for `entered`;
      - start `Drain` with a 10s ctx, then wait on native `StatusChanged(nats.SubscriptionDraining)` (emitted in
        `unsubscribe`, `nats.go:5325`) so the drain is really pending;
      - close the native connection with `GetNativeConnection().Close()`, not `Client.Close`, which goes through
        `drainAndCloseConnection`/`drainConnection`;
      - assert `Drain` has not returned; close `release`; require `nil`, and require that the handler had already
        returned.
- [ ] 2.2 Already-closed join test:
      - the handler closes `entered`, then blocks on `release`;
      - publish and `Flush`, then wait for `entered`;
      - close the native connection with `GetNativeConnection().Close()`;
      - call `Drain` with a 10s ctx in a goroutine; native `Drain` returns `ErrConnectionClosed`, so this takes that
        branch;
      - assert `Drain` has not returned; close `release`; require `nil`, and require that the handler had returned
        first.
- [ ] 2.3 Close-before-`Drain` test across the four public factories, following
      `subscription_integration_test.go:15`: close the native connection, then require `Drain` to return `nil`.
- [ ] 2.4 In `processor/agentic-tools/outcomes_integration_test.go`, change `:272` to `require.NoError`, and rewrite
      the comment blocks at `:253-261` and `:267-271` to state the new contract.

## 3. Implementation (`natsclient/client.go`)

- [ ] 3.1 Change `nativeSubscription` (`:746-751`) as design D1 describes: add `SetClosedHandler(func(string))`
      and remove `StatusChanged`.
- [ ] 3.2 In `Subscription`/`newSubscription`, replace `closed` and `drainComplete` with `done`, `doneOnce` and
      `closedAuthority`. Register the non-blocking handler, then record `IsValid()` (design D1 and D2).
- [ ] 3.3 Implement the simplified `Drain` per design D3, and reword its doc comment (`:778-780`) per design D4.
- [ ] 3.4 In `handleClosed` (`:1586-1596`), log per design D4: Warn when `m.closed.Load()` is false (unrequested
      close), Debug when it is true (requested close). Add a unit test that calls `handleClosed` with a recording
      logger, following `client_async_error_test.go:117`, and covers both branches:
      - flag unset: exactly one Warn line naming the possible drop;
      - flag set: the Debug line and no Warn line.
- [ ] 3.5 Run each package's focused tests, then `task lint`, `go test -race ./natsclient/...`, and the
      `natsclient` and `processor/agentic-tools` integration tests (`-tags=integration`).

## 4. Mutation evidence, kept in the PR

Commit the logs under this change directory or inline them in a PR comment; the scratchpad alone does not count.
Each mutation uses a `cp` backup whose restoration is verified by checksum.

- [ ] 4.1 Revert the wait to `StatusChanged(SubscriptionClosed)`: 2.1 must go red.
- [ ] 4.2 Return early on `ErrConnectionClosed` without waiting on `done`: 2.2's "has not returned" assertion and
      1.2's native `ErrConnectionClosed` case must go red.
- [ ] 4.3 Treat `closedAuthority == false` as a terminal wait: 1.3 must go red (through its bounded ctx, not a hang).
- [ ] 4.4 Kill each `handleClosed` branch separately:
      - drop the flag check so every close logs Warn: 3.4's requested-close case must go red;
      - skip the Warn emit: 3.4's unrequested-close case must go red.
- [ ] 4.5 Record in the PR that the registration/`IsValid` ordering (design D2) is covered by argument, because it
      cannot be tested deterministically.

## 5. Review and archive

- [ ] 5.1 Record the `semstreams-reviewer` verdict on PR #1373, with a per-ruling conformance table mapping each
      ruling to `file:line`. The table has these rows:
      - rulings 1-3 from [issuecomment-5814983813](https://github.com/C360Studio/semstreams/issues/1372#issuecomment-5814983813);
      - ruling 4 as amended in round 2 ([issuecomment-5817564164](https://github.com/C360Studio/semstreams/issues/1372#issuecomment-5817564164),
        item 2);
      - a **CLARIFICATION** row, not a DEVIATION row, for the design D2 window (a close after `SetClosedHandler` but
        before the `IsValid` read returns `ErrBadSubscription`), citing round 2, item 1.
- [ ] 5.2 As the last content commit, run `openspec archive subscription-drain-connection-close`. It seeds
      `openspec/specs/nats-subscription-lifecycle/spec.md`; confirm the new spec keeps the real Purpose from the
      delta, not the `TBD` placeholder.
