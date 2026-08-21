## 1. Accepted scope and landed implementation

- [x] 1.1 Record and independently approve the PR #990 truth-reset inventory.
- [x] 1.2 Record the owner-approved boot-only disposition.
- [x] 1.3 Record ADR-096 for Flow diagrams as authoring rather than lifecycle authority.
- [x] 1.4 Land fixed boot composition and authoring-only Flow behavior in
  `8117858367e1cc9d1dc434d211989e7a2ed1e552` through PR #997.
- [x] 1.5 Reconcile the implementation conformance ledger to current main and obtain independent review.

## 2. Current implementation truth

- [x] 2.1 ComponentManager reads one existing configuration value during construction and composes only that fixed
  enabled component set.
- [x] 2.2 Post-construction component and model-registry writes do not mutate running component identity, membership, or
  configuration.
- [x] 2.3 Registry admits boot declarations, seals, exposes defensive declaration values, and retains no runtime
  component handle.
- [x] 2.4 Generic runtime component-config PUT, `watch_config`, replacement, reconciliation, and removal surfaces are
  absent.
- [x] 2.5 Flow retains authoring CRUD, validation, and compilation without runtime lifecycle state.
- [x] 2.6 Explicit publication validates, compiles, sorts component names, performs sequential upserts, never infers
  deletion, and reports exact persistence progress plus reboot requirement.
- [x] 2.7 Flow lifecycle routes, tools, state, telemetry, logs, and streams are absent without aliases or a replacement
  monitor.
- [x] 2.8 PR #997 claims no Rule or readiness implementation; no Rule or readiness delta is retained.

## 3. Archive proof

- [x] 3.1 Run and record focused unit and integration race tests for the implemented boot and Flow behavior.
- [x] 3.2 Run and record a real process-boundary proof: process A commits desired component configuration and remains
  unchanged; after A exits, process B starts against the same durable NATS state and composes those candidates.
- [x] 3.3 Run and record relevant core and CRUD E2E against current main.
- [x] 3.4 Record that PR #997 has no durable pre-merge E2E artifact and that current E2E is post-merge evidence only;
  do not claim the historical timing gate retroactively.
- [x] 3.5 Run and record repository lint, `go test -race ./...`, contract tests, schema generation/no-drift, and strict
  OpenSpec validation.
- [x] 3.6 Obtain independent review of the final implementation ledger, task truth, retained deltas, and archive diff.
- [x] 3.7 Promote exactly the five retained capability deltas and archive this change.
