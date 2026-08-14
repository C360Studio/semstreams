# Change: Prove slow-consumer attribution through the product assembly

## Why

The real-NATS integration proof establishes the nats.go carrier and production handler, and production-bootstrap
coverage establishes the configured logger graph. No product E2E currently assembles `cmd/semstreams`, deliberately
overflows a known core-NATS subscription, and observes the exact operator-facing JSON attribution from outside the
process. GitHub #954 records this release-gate gap.

## What Changes

- Add one isolated, disposable E2E stack built from `cmd/semstreams` with one E2E-only build tag.
- Add one inert untagged hook and one tagged private probe that uses the existing connected client, temporarily gates
  the installed nats.go error callback, and produces an exact known slow-consumer event.
- Parse configured JSON stdout and corroborate the single diagnostic with the existing log-entry counter.
- Report how many E2E assertions actually executed on both success and partial-failure paths.
- Run the isolated gate per pull request without changing the ordinary `e2e:core` production target.

## Non-goals

- No public or configured pending-limit control; GitHub #586 remains independent.
- No new client, logger, slog handler, metric, endpoint, config key, production/public/control subject contract,
  stream, bucket, or durable state. The tagged fixture's private subject and temporary nats.go callback wrapper are
  test implementation details, not runtime surfaces.
- No alternate main package and no change to the untagged release binary's behavior.
- No arbitrary sleeps and no inference from prose-only log matching.
- No ADR; this is reversible test composition for existing diagnostics contracts.
