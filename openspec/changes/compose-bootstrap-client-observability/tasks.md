# Tasks: Compose bootstrap client observability

## 1. Accepted inventory and design

- [x] 1.1 Materialize the complete logger, handler, metrics, binary, test, spec, and adopter-seam inventory.
- [x] 1.2 Obtain independent `INVENTORY PASS` after correcting recursion scope, SafeConfig conflict, and constructor
  census findings.
- [x] 1.3 Materialize options, target sequence, test contract, and rulings R1-R15.
- [x] 1.4 Obtain independent `DESIGN PASS` after correcting policy ownership, effective stream ordering, E2E counter
  semantics, and public Go type identity.
- [x] 1.5 Record owner acceptance plus R16 identical-output/shared-handler constraint on 2026-08-13.

## 2. TDD implementation

- [x] 2.1 Add failing shared-construction tests for configured local-output identity, component identity, exact-once
  production counting, E2E no-counter-handler behavior, and pre-connect metric registration.
- [x] 2.2 Add failing effective-`SafeConfig` forwarding-policy and stream-order tests.
- [x] 2.3 Add the internal log-forwarder policy owner and preserve the named public service type.
- [x] 2.4 Add shared Phase-A logging/client/config helpers without implicit logger construction.
- [x] 2.5 Rewire both primary binaries through the shared helpers and effective configuration.
- [x] 2.6 Pass focused unit and unit-race GREEN with no arbitrary sleeps.

## 3. Real construction and recursion evidence

- [x] 3.1 Add synchronized real-NATS proof that client diagnostics reach production local output/counter but do not
  publish to `logs.>` through the same client.
- [x] 3.2 Prove E2E registers client metric families before connection while leaving `LogEntriesTotal` unchanged.
- [x] 3.3 Add a half-migration guard covering both primary binary construction entries.

## 4. Documentation, verification, and review

- [x] 4.1 Correct stale documented log subject order and update boot-composition documentation.
- [x] 4.2 Record exact per-ruling implementation evidence and any owner-approved deviation.
- [x] 4.3 Run focused race, repository race, lint, integration, build, schema no-drift, contract, strict OpenSpec, and
  `task e2e:core` gates.
- [x] 4.4 Obtain independent SemStreams implementation review approval before integration.
