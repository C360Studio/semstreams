# Design: subscription-drain-connection-close (#1372)

> Baseline: `main` at `5748fe85`, with nats.go v1.52.0 (`go.mod:12`). Every `client.go`, `request.go`, and test pin
> below is at `5748fe85`. Every `nats.go:` pin is `$(go env GOMODCACHE)/github.com/nats-io/nats.go@v1.52.0/nats.go`.
> Design: architect design O2, reviewed pre-owner as "revise, no BLOCKING or HIGH". Owner rulings:
> [#1372, 2026-09-24](https://github.com/C360Studio/semstreams/issues/1372#issuecomment-5814983813); round 2, the
> same day ([issuecomment-5817564164](https://github.com/C360Studio/semstreams/issues/1372#issuecomment-5817564164)):
> the D2 window is a clarification of ruling 1, and ruling 4's Warn is amended to unrequested closes only.

## Context

The motivation is in proposal.md § Why. This section covers the mechanism.

- `Subscription.Drain` (`client.go:781`) waits on `closed`, the channel returned by
  `sub.StatusChanged(nats.SubscriptionClosed)` (`client.go:766`).
- nats.go emits `SubscriptionClosed` from exactly one place, `removeSub` (`nats.go:5009`).
- `checkDrained` returns early at `nats.go:5232` when `nc.IsClosed()` is true, and it does so without calling
  `removeSub`.
- `Conn.close` marks each subscription closed (`s.closed = true`, `:5999`; `s.connClosed = true`, `:6001`) and
  signals `pCond` (`:6003-6004`). It emits no subscription status.
- The result: a drain that is pending when the connection closes waits out the caller's whole ctx.

nats.go already has a native completion signal with the right timing. `SetClosedHandler` (`nats.go:5283-5287`)
stores `pDone`. On an async subscription, `pDone` is called only from `waitForMsgs`: the loop breaks at `:3625`, and
`pDone` is read and called at `:3654-3658`. That happens after any in-flight message callback (`mcb`) has returned.
nats.go reaches it on four paths:

- a normal drain (`checkDrained` → `removeSub` sets `closed` and broadcasts);
- an external `Unsubscribe` (`unsubscribe` → `removeSub`);
- the max-reached path (`:3634-3639`);
- a connection close (`:5999`/`:6003-6004`).

`removeSub` and `close` call `pDone` directly only for non-async subscriptions (`:5001-5006`, and the `s.typ !=
AsyncSubscription` guard in `close`).

### Premises (all independently verified by the reviewer; pins re-checked for this document)

- **P1.** On the close path, `pDone` fires after `mcb` returns. `waitForMsgs` observes `closed` under `s.mu` only
  between deliveries (`:3609`), and it breaks at `:3625` before reading `pDone` at `:3654`.
- **P2.** Only async subscriptions are wrapped. `newSubscription` is called from `client.go:854`, wrapping
  `m.conn.Subscribe` at `:838`, and from `request.go:427`, wrapping `conn.Subscribe` at `:359`. No other non-test
  call site exists (`git grep -n "newSubscription("`).
- **P3.** Nothing else writes `pDone` on these subscriptions. The only store in nats.go is `SetClosedHandler`
  (`:5285`). `kv.go:1164` and `object.go:1131` write it on their own watcher subscriptions. SemStreams has no
  `SetClosedHandler` call (`git grep -n SetClosedHandler` matches only a comment at
  `processor/graph-ingest/component.go:1312`), and the wrapper keeps the native handle unexported.
- **P4.** No production caller branches on `Drain`'s error value. gopls finds 29 non-test references to
  `(*Subscription).Drain`. Ten of them keep the handle on any error and retry, so a sticky non-nil result leaves
  them stuck with it on every retry.
- **P5.** `IsValid()` is false exactly when `s.closed` is true. `IsValid` returns `s.conn != nil && !s.closed`
  (`:5046-5053`), and nothing in nats.go sets a subscription's `conn` to nil (the only `.conn = nil` writes,
  `:3445` and `:5972`, are the connection's `net.Conn`).

### Inventory additions (found during review, not in #1372)

1. `handleClosed` (`client.go:1586-1596`) and `setStatus` (`client.go:234-236`) log and meter nothing. A connection
   close that drops queued messages leaves no trace today.
2. The external-Unsubscribe path does not join callbacks today. `removeSub` emits `SubscriptionClosed` (`:5009`)
   while `mcb` may still be running on the `waitForMsgs` goroutine. So today's `Drain` after an `Unsubscribe`
   (`client.go:789-796`, `closed` already fired) returns while a callback may still be running.
3. In v1.52.0, native `Drain` (`nats.go:5066-5077`) returns only one of three results. It returns
   `ErrBadSubscription` when the receiver or its `conn` is nil (`:5068`, `:5074`). It returns
   `ErrConnectionClosed` when the connection is closed (`:5306-5307`). Otherwise it returns `nil`, including for an
   already-unsubscribed subscription (`:5314-5316`, `:5350`).
4. Messages dropped at a close cannot be counted afterwards. `Pending` returns `ErrBadSubscription` once `s.closed`
   is set (`:5589-5597`).

## Goals / Non-Goals

**Goals:** `Drain` completes on the native terminal state on every path. It joins the in-flight callback on every
path. It returns `nil` on connection close (owner ruling 1). The existing ctx-wins, rejoin, and single-initiation
semantics are unchanged.

**Non-Goals:** No new exported surface, no goroutine, and no retained context. No metric (owner ruling 4). No change
to `Client.Close` or to JetStream consumer handles (proposal § Non-goals).

## Decisions

### D1. Replace the status channel with the native closed handler

`newSubscription` registers `sub.SetClosedHandler(func(string) { s.doneOnce.Do(func() { close(s.done) }) })`, then
records `closedAuthority = sub.IsValid()`. The handler runs on the subscription's own `waitForMsgs` goroutine, so it
must not block. A `sync.Once`-guarded channel close meets that. The unexported `nativeSubscription` interface
(`client.go:746-751`) gains `SetClosedHandler(func(string))` and drops `StatusChanged`.

**Alternatives considered.**

- Keep `StatusChanged` and add a connection-closed watch (`nc.IsClosed()` or the connection's closed callback), as
  #1372 suggested. That observes the connection, not the subscription. It would return while a callback is still
  running, and it adds a second signal where the native one already exists.
- Poll `IsValid`. That misses the callback join the same way, and it adds a timing knob.

### D2. Registration ordering

`SetClosedHandler` and `IsValid` are separate critical sections on the same `s.mu`. Suppose `IsValid` observes
`!closed`. Then every later write of `closed = true`, in `removeSub` (`:5008`) or in `close` (`:5999`), acquires
`s.mu` after the handler store. `waitForMsgs` reads `closed` under `s.mu` and then reads `pDone` under `s.mu` again
(`:3654`), so that read also comes after the handler store and sees the handler.

When `IsValid` observes `closed`, the subscription was already terminal before the wrapper could prove the handler
would fire. `closedAuthority` is then false, and `Drain` returns `ErrBadSubscription` as it does today. That is
conservative: a close that lands between the two critical sections gives `ErrBadSubscription` even though the
handler may still fire. The wrapper cannot tell this window apart from a subscription born invalid. The owner
accepted it as a clarification of ruling 1, not a deviation (round 2, item 1), and the spec's "already invalid when
its handle was created" includes the handle's `IsValid` read. This ordering cannot be tested deterministically, so it
is recorded here as an argument (tasks.md 4.5).

### D3. Simplified Drain

`Drain` keeps the existing `drainOnce`:

1. If `!closedAuthority`, record `ErrBadSubscription`. The result is sticky, as today.
2. Otherwise call native `Drain` once. `ErrConnectionClosed` and `ErrBadSubscription` are terminal outcomes: clear
   them and wait. `ErrBadSubscription` cannot occur here given P5, but it stays in the sentinel list in case an
   upgraded nats.go adds a path that returns it. Any other native error is sticky.
3. Outside the once, return a sticky error when one is recorded. Otherwise `select { <-s.done; <-ctx.Done() }`.

The `IsValid` pre-check at `client.go:789-796` is dropped, because native `Drain` covers both already-closed cases.
After an external unsubscribe it returns `nil` (`:5314-5316`). On a closed connection it returns
`ErrConnectionClosed` (`:5306-5307`). Both then wait on `done`.

ctx wins, a ctx error is never stored, a later call rejoins the same drain, and there is exactly one native
initiation, all as today (`client.go:808-822`). The `drainComplete` field disappears because `done` carries that
fact.

### D4. Observability (owner ruling 4, amended in round 2)

`handleClosed` branches on the client's closed flag. For an **unrequested** close (the flag is not set), it logs one
Warn line: the connection closed, and messages queued but not yet delivered on core subscriptions may have been
dropped. For a **requested** close (`Client.Close` set the flag), it logs the same fact at Debug. A Warn on every clean
shutdown and every test cleanup would train readers to ignore it, and unrequested closes are where queued messages
are actually lost. The line goes where the loss happens, whether or not anyone drains. There is no metric. The
developer contract's log-and-metric rule for a declared drop is satisfied by this owner ruling, not waived silently.
The count is not observable anyway (inventory 4).

The flag is `closed atomic.Bool` (`client.go:148`), so `handleClosed` reads it with `Load()` and needs no lock.
`Client.Close` stores `true` at `client.go:586`, under `closeMu`, before it calls `drainAndCloseConnection` at
`client.go:605`. nats.go dispatches the connection closed handler only after that native drain or close, so the store
is sequenced before the `Load` and a requested close reads `true`. Nothing stores `false` (`grep -n "closed.Store"`
finds only `:586`). The connection-loss watchdog already reads the same flag this way (`client.go:1569`). A close that
the server or network causes while `Client.Close` is racing in is logged at Debug. That is acceptable: the caller
asked for the close.

The `Drain` doc comment (`client.go:778-780`) is reworded. On a connection close, `Drain` still joins the in-flight
callback and returns `nil`. Queued-but-undelivered messages are discarded (core NATS at-most-once).

## Behavior changes

1. **Drain on an already-closed connection now waits for an in-flight callback.** Today it returns
   `ErrBadSubscription` immediately (`client.go:789-796`). It now returns `nil` after the callback returns. The
   wait is bounded by the per-message timeout (`client.go:840`, 30s, for `Subscribe`; `requestHandlerTimeout` at
   `request.go:370` for `SubscribeForRequests`) or by the caller's ctx. This reverses the assertion at
   `processor/agentic-tools/outcomes_integration_test.go:272` and its comments at `:253-271` (owner ruling 1).
2. **Drain after an external Unsubscribe now joins the callback** (inventory 2). This honors
   `openspec/specs/graph-index/spec.md:598` ("attempt native Drain … while callback authority remains live") and
   `:611`. It also honors the premise at `service/message_logger.go:898-899` ("Drain while the subscription and
   runtime contexts remain live so admitted callbacks can finish").

## Risks / Trade-offs

- **Strongest case against: dropped queued messages are reported as `nil`.** A caller cannot tell a complete drain
  from a close that discarded queued messages. The owner ruled `nil` anyway (ruling 1), for three reasons. Core
  NATS is at-most-once, so the loss is the transport's contract and not a drain failure. No caller branches on the
  value (P4). A non-nil sticky result would leave the ten retrying callers stuck on it. The loss is declared once,
  where it happens, by the `handleClosed` Warn (D4).
- **[nats.go upgrade changes when `pDone` fires]** → The integration tests (tasks.md 2.1, 2.2, 2.3) drive the real
  close-mid-drain and close-before-Drain paths and fail if `Drain` stops completing or stops joining on them.
  Keeping `ErrBadSubscription` in the terminal sentinel list (D3) covers a new native return path.
- **[A slow callback lengthens Stop]** → Bounded by the per-message timeout or the caller's ctx (behavior change 1).
  The caller's ctx still wins.
- **[Close between `SetClosedHandler` and `IsValid`]** → Returns `ErrBadSubscription` as today (D2). The window is
  one mutex handoff at construction.

## Migration Plan

None. The signature and exported surface are unchanged, and no sister names `ErrBadSubscription` (proposal §
Impact). Rollback is a revert of the `fix(natsclient)` commit.

## Residual

A nats.go upgrade could change when `SetClosedHandler` fires. Against real NATS, the integration tests guard only
the two connection-close paths: close mid-drain (tasks.md 2.1) and close before `Drain`, including the callback join
(2.2, 2.3). The normal-drain, external-unsubscribe and max-reached paths rest on the unit tests' fake and on the
nats.go reading in § Context. D3's sentinel list absorbs a new terminal error. No issue is filed for this: it is the
design's recorded residual.
