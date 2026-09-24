## Purpose

The completion contract of a core NATS subscription handle returned by the SemStreams client: when its `Drain`
returns, which message callbacks it joins first, and what it reports when the subscription or its connection is
already gone.

## ADDED Requirements

### Requirement: Subscription Drain completes on native terminal state

`Subscription.Drain(ctx)` SHALL return only after the native subscription has reached its terminal state and its
in-flight message callback has returned, or when `ctx` ends, whichever comes first.

The native terminal state covers a completed drain, an external unsubscribe, reaching the subscription's message
limit, and the connection closing. A connection close SHALL NOT turn into a wait for `ctx`. When the terminal state
is reached, `Drain` MUST return `nil`, including when the connection closed before `Drain` was called. Messages that
were queued but not yet delivered when the connection closed are discarded (core NATS at-most-once), and `Drain`
does not report them.

`Drain` MUST start at most one native drain per subscription. When `ctx` ends first, `Drain` MUST return the ctx
error without recording it as the drain's outcome, and a later call MUST rejoin the same native drain. A subscription
that was already invalid when its handle was created MUST return `nats.ErrBadSubscription` without waiting, on every
call. A native drain error other than connection-closed or bad-subscription MUST be returned on every call.

#### Scenario: Normal drain joins its callback

- **GIVEN** a subscription whose callback is processing a message
- **WHEN** `Drain` is called with a live connection
- **THEN** `Drain` does not return before the callback returns
- **AND** `Drain` returns `nil` once the native drain completes

#### Scenario: Connection closes mid-drain with a callback in flight

- **GIVEN** a subscription whose callback is blocked processing a message
- **AND** a `Drain` call whose native drain is pending
- **WHEN** the underlying connection closes
- **THEN** `Drain` does not return before the blocked callback returns
- **AND** `Drain` returns `nil` after the callback returns, before its ctx ends

#### Scenario: Connection already closed before Drain

- **GIVEN** a subscription created on a live connection
- **AND** the connection has since closed
- **WHEN** `Drain` is called
- **THEN** `Drain` returns `nil`

#### Scenario: External unsubscribe then Drain joins the callback

- **GIVEN** a subscription whose callback is processing a message
- **AND** the subscription has been unsubscribed directly
- **WHEN** `Drain` is called
- **THEN** `Drain` does not return before the in-flight callback returns
- **AND** `Drain` returns `nil`

#### Scenario: Caller ctx ends first and a later call rejoins

- **GIVEN** a `Drain` call whose native drain has not completed
- **WHEN** the caller's ctx ends
- **THEN** that call returns the ctx error
- **AND** a later `Drain` call starts no second native drain and returns `nil` once the first native drain completes

#### Scenario: Born-invalid subscription reports ErrBadSubscription

- **GIVEN** a subscription handle whose native subscription was already closed when the handle was created
- **WHEN** `Drain` is called
- **THEN** `Drain` returns `nats.ErrBadSubscription` without waiting
- **AND** every later call returns the same error

#### Scenario: Another native drain error is preserved

- **GIVEN** a native drain that fails with an error other than connection-closed or bad-subscription
- **WHEN** `Drain` is called
- **THEN** `Drain` returns that error
- **AND** every later call returns that error
