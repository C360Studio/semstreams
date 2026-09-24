# Design: subscription-drain-connection-close (#1372)

- Baseline: `5748fe85`, nats.go v1.52.0 (`go.mod:12`).
- Owner rulings are on #1372.
- This is the minimal cut. It replaces an earlier, larger design reviewed at `4bf18aeb` (see git history).

## The defect

- `Subscription.Drain` waits on `StatusChanged(nats.SubscriptionClosed)` (`natsclient/client.go:766`).
- In nats.go v1.52.0, only `removeSub` emits that status (`nats.go:5009`).
- `checkDrained` returns early on `nc.IsClosed()` (`nats.go:5232`) without calling `removeSub`.
- So when the connection closes during a pending drain, the status never arrives and `Drain` waits until the
  caller's context ends.

## The fix

```go
func newSubscription(sub nativeSubscription) *Subscription {
	s := &Subscription{sub: sub, done: make(chan struct{})}
	sub.SetClosedHandler(func(string) { s.closeDone() })
	if !sub.IsValid() {
		s.closeDone() // closed before the handler was set; see "Deliberately not handled" below
	}
	return s
}

func (s *Subscription) Drain(ctx context.Context) error {
	// existing nil-ctx and nil-subscription guards stay
	s.drainOnce.Do(func() {
		if err := s.sub.Drain(); !errors.Is(err, nats.ErrConnectionClosed) {
			s.drainErr = err
		}
	})
	if s.drainErr != nil {
		return s.drainErr
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

- `closeDone` closes `done` through a `sync.Once`.
- The closed handler runs on nats.go's delivery goroutine, so it must not block, and closing a channel does not.

Why this is correct:

- **Callbacks are joined on every path.** For async subscriptions, nats.go calls the closed handler only from
  `waitForMsgs`, after its loop exits:
  - on connection close, `Conn.close` marks the sub closed and signals it; the loop breaks at `nats.go:3625`;
  - then the handler is read and called at `nats.go:3654-3658`, after any in-flight callback has returned.
- **Native `Drain` returns one of three things** (`nats.go:5066-5077`, `unsubscribe` at `:5306-5316`):
  - `nil`: a normal drain, or already unsubscribed;
  - `ErrConnectionClosed`: the connection is gone, which is terminal;
  - `ErrBadSubscription`: only when the sub has no connection, which the two construction sites cannot produce.
    If a future nats.go returns it, it stays sticky, as any other error does.
- **The context still wins,** so `Stop` stays caller-bounded (`component-lifecycle/spec.md:34`).

Both construction sites create async subscriptions: `client.go:838`/`:854` and `request.go:359`/`:427`.

Behavior changes (the owner ruled 1 and 3 on #1372):

- **A connection closed mid-drain, or before `Drain`:** returns `nil` after the in-flight callback returns.
  - Before: `DeadlineExceeded`, a hang, or `ErrBadSubscription`, depending on timing.
  - Ten callers keep the handle and retry on any error, and today the first error is permanent inside `drainOnce`.
    Under the old behavior those retries could never succeed.
- **`Drain` after an external `Unsubscribe`:** now waits for the in-flight callback.
  - Before: it returned as soon as `removeSub` emitted its status, possibly while a callback was still running.
  - The premises at `graph-index/spec.md:611` and `service/message_logger.go:898-899` assume this wait.

## Deliberately not handled

This section is evidence for the next agent. Each case below was found, designed for, reviewed, and cut. Do not
re-add handling for one of them without new evidence that it happens in practice. "Could happen by reading
nats.go" was already known.

The first design (`87c1e1a3` → `4bf18aeb`) covered all of these cases:

- It took one architect pass, two writer passes, and three design-review rounds: about 800K subagent tokens
  before any code existed.
- For comparison, the defect itself is fixed by the roughly 20 lines above.
- The owner reviewed the result and judged it unlikely to be idiomatic or easy to understand. The rulings are
  on #1372.

| Case | How likely | What happens with this fix | What the cut design did |
|---|---|---|---|
| The connection closes after `SetClosedHandler` is registered and before `IsValid` is read in `newSubscription` | A window of a few instructions during construction. Never observed. | `done` is closed at construction, so `Drain` returns `nil` without waiting for a callback that could, in principle, still be running. Accepted: no messages can have been delivered to a handle whose constructor has not returned. | A `closedAuthority` flag and a sticky `ErrBadSubscription` path, plus a ruling clarification to cover the gap between "born invalid" and "closed during registration". |
| Messages queued but not delivered when the connection dies | Normal for core NATS, which is at-most-once. The count is unknowable after close (`Subscription.Pending` refuses, `nats.go:5589-5597`). | Discarded silently. The doc comment says so. The connection's death already reaches health through `handleClosed` (`client.go:1586`). | A Warn line in `handleClosed`, branching on whether `Client.Close` requested the close, with a race note and a second edge note for `Connect`'s abandoned dials. |
| Native `Drain` returns `ErrBadSubscription` | Unreachable from the two construction sites, because `sub.conn` is never nil'd in v1.52.0. | Sticky error, like any other native error. | A sentinel list kept "for upgrade safety". |
| The subscription reaches its message limit (`AutoUnsubscribe`) | Unreachable, because no SemStreams handle exposes `AutoUnsubscribe`. | Not relevant. | A spec clause and a parenthetical. |
| A nats.go upgrade changes when the closed handler fires | Possible on any upgrade. | The integration test in `tasks.md` 2.1 goes red. | The same guard, plus prose about which paths the guard does not cover. |

If one of these starts happening, the evidence to bring is:

- a failing run or production log that shows it;
- the nats.go version.

A reading of nats.go that shows it *could* happen is not new evidence.
