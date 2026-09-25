# nats-subscription-lifecycle Specification

## Purpose
When a SemStreams core NATS subscription handle's `Drain` returns, and what it reports, including when the
connection is lost.

Edge cases that were designed for and deliberately not handled (#1372) are listed, with their likelihood and what re-adding them needs, in
`openspec/changes/archive/2026-09-25-subscription-drain-connection-close/design.md` § Deliberately not handled.
Read that before adding handling for a case found by reading nats.go source.
## Requirements
### Requirement: Subscription Drain returns after its callbacks, including on connection loss

`Subscription.Drain(ctx)` SHALL return only after the subscription's delivery goroutine has exited and its
in-flight callback has returned, or when `ctx` ends first. A connection that closes before or during the drain
SHALL count as a completed drain, not a failure. Once the delivery goroutine has exited, `Drain` returns `nil`. If
`ctx` ends first, it returns the context error. A native drain error other than a closed connection is returned
unchanged.

#### Scenario: Normal drain joins its callback

- **GIVEN** a subscription whose callback is running
- **WHEN** `Drain` is called on a live connection
- **THEN** `Drain` does not return before the callback returns
- **AND** `Drain` returns `nil`

#### Scenario: Connection closes while a drain is pending

- **GIVEN** a pending drain whose callback is running
- **WHEN** the connection closes
- **THEN** `Drain` does not return before the callback returns
- **AND** `Drain` returns `nil` before its context ends

#### Scenario: Connection already closed before Drain

- **GIVEN** a subscription whose connection has closed while its callback is running
- **WHEN** `Drain` is called
- **THEN** `Drain` does not return before the callback returns
- **AND** `Drain` returns `nil`

#### Scenario: Caller context ends first

- **GIVEN** a drain whose callback does not return
- **WHEN** the caller's context ends
- **THEN** `Drain` returns the context error

