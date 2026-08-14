# nats-client-diagnostics Specification

## Purpose
TBD - created by archiving change attribute-nats-subscription-errors. Update Purpose after archive.
## Requirements
### Requirement: Subscription-bearing asynchronous NATS errors MUST be attributable

The natsclient connection-wide asynchronous error handler MUST emit an ERROR record preserving the original error.
When nats.go supplies a subscription, the record MUST include its subscribed subject and MUST include its queue group
only when nonempty.

For an error matching `nats.ErrSlowConsumer`, the handler MUST query `Dropped()` exactly once and report the
subscription's cumulative known dropped-message count when that query succeeds. If the query fails, the record MUST
omit `dropped` and report `dropped_available=false`.

The handler MUST NOT infer attribution from subjects, query pending depth, high-water values, or limits, record a
connection failure, or change circuit state, connection status, health, readiness, or callbacks. A nil-subscription
callback MUST preserve the generic error-only record.

#### Scenario: Slow consumer identifies the affected subscription

- **GIVEN** nats.go reports `ErrSlowConsumer` with a subscription whose subject is `agent.loop.>` and whose known
  dropped count is 7
- **WHEN** natsclient handles the asynchronous error
- **THEN** one ERROR record contains the original error, `subject=agent.loop.>`, and `dropped=7`
- **AND** it contains no queue field when the subscription has no queue group

#### Scenario: Queue subscription is named

- **GIVEN** an asynchronous error carries a subscription with queue `workers`
- **WHEN** natsclient handles the error
- **THEN** the ERROR record contains `subject` and `queue=workers`

#### Scenario: Drop count is unavailable

- **GIVEN** a slow-consumer error whose subscription rejects `Dropped()`
- **WHEN** natsclient handles the error
- **THEN** the ERROR record omits `dropped`
- **AND** contains `dropped_available=false`

#### Scenario: Generic asynchronous error remains generic

- **GIVEN** an asynchronous NATS error with no subscription
- **WHEN** natsclient handles the error
- **THEN** the ERROR record contains the original error
- **AND** contains no subject, queue, dropped, pending, or limit field

#### Scenario: Diagnostics do not redefine connection health

- **GIVEN** a connected client receives a subscription-bearing asynchronous error
- **WHEN** the handler returns
- **THEN** failure count, circuit state, connection status, health, and callbacks are unchanged

### Requirement: Product E2E MUST prove assembled slow-consumer attribution

A separate tagged E2E assembly of `cmd/semstreams` MUST deliberately overflow a known core-NATS subscription, drive
the installed production asynchronous-error handler, and externally observe exactly one configured-local JSON record.
The record MUST contain the original slow-consumer error, subscription subject, queue, and exact cumulative known
dropped-message count. The existing `natsclient` ERROR counter MUST corroborate exact-one emission.

The proof MUST report assertions actually executed on both success and partial failure. It MUST use bounded explicit
synchronization and MUST be mutation-sensitive to the attributed fields. The untagged release assembly MUST expose no
fixture behavior, pending-limit control, or test communication surface.

#### Scenario: Tagged product assembly attributes exact known drops

- **GIVEN** the disposable tagged `cmd/semstreams` assembly and its private gated subscription fixture
- **WHEN** one message is admitted and exactly eight additional messages overflow its pending limit
- **THEN** external JSON output contains exactly one `component=natsclient` slow-consumer ERROR for the fixture
  subject and queue with `dropped=8`
- **AND** no drop-unavailable fallback is present
- **AND** the existing matching ERROR counter equals one
- **AND** the scenario reports the number of assertions actually executed

#### Scenario: Untagged product assembly remains inert

- **GIVEN** the ordinary untagged `cmd/semstreams` production assembly
- **WHEN** it connects its NATS client
- **THEN** no E2E fixture behavior activates
- **AND** no E2E config, endpoint, production/public/control subject contract, stream, bucket, logger, client, or slog
  handler is introduced
