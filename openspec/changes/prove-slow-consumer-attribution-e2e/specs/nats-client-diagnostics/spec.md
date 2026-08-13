# nats-client-diagnostics Delta

## ADDED Requirements

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
