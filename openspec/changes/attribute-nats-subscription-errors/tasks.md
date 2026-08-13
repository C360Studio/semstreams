# Tasks: Attribute asynchronous NATS subscription errors

## 1. Accepted inventory and design

- [x] 1.1 Record the sole async-error owner, existing diagnostics surfaces, present consumer, and adopter seam.
- [x] 1.2 Obtain owner acceptance of R1-R12 and the `nats-client-diagnostics` target on 2026-08-12.
- [x] 1.3 Materialize the accepted proposal, design, capability delta, and conservative task truth.

## 2. TDD implementation

- [x] 2.1 Add behavior tests for nil-subscription shape, ordinary subject/queue attribution, wrapped slow-consumer
  classification, unavailable dropped count, absent snapshots, and unchanged runtime/callback state.
- [x] 2.2 Observe focused unit RED against the pre-change handler: the tests reached behavior assertions and failed
  because subscription fields were absent.
- [x] 2.3 Minimally extend the existing private handler with subject/queue attribution and one slow-consumer
  `Dropped()` query; add no adjacent surface.
- [x] 2.4 Pass focused unit and unit-race GREEN.

## 3. Real-NATS proof

- [x] 3.1 Add the integration-tagged real-NATS test with a gated connection callback, blocked handler, test-only
  pending limit of one, fixed publish count, exact bounded drop polling, and no arbitrary sleeps.
- [x] 3.2 Observe real-NATS race RED against the pre-change handler: the callback and exact drop observation succeeded,
  then the log assertion failed because `subject` was absent.
- [x] 3.3 Pass focused real-NATS race GREEN and prove logged cumulative drops equal the exact independent observation
  and are greater than one.

## 4. Verification and review

- [x] 4.1 Run full `go test ./natsclient` and `go test -race ./natsclient`.
- [x] 4.2 Run full `go test -race -tags=integration ./natsclient`.
- [x] 4.3 Run `task lint`, integration/live-LLM vet, repository-wide `go test -race ./...`, CI-equivalent
  integration, build, schema no-drift, and contract gates.
- [x] 4.4 Pass `openspec validate attribute-nats-subscription-errors --strict`.
- [x] 4.5 Complete independent SemStreams reviewer approval with no findings.
- [x] 4.6 Keep product E2E proof unclaimed; [#954](https://github.com/C360Studio/semstreams/issues/954) remains the
  recorded coverage gap.
- [x] 4.7 Keep primary-binary logger composition out of scope and record it in
  [#955](https://github.com/C360Studio/semstreams/issues/955).

## Executed TDD evidence

- RED unit: `env GOCACHE=/private/tmp/semstreams-gh950-gocache go test ./natsclient -run
  '^TestClientHandleError' -count=1` — failed at subject/queue/drop-unavailable assertions against the original
  error-only handler.
- RED real NATS: `env GOCACHE=/private/tmp/semstreams-gh950-gocache go test -race -tags=integration ./natsclient -run
  '^TestIntegration_ClientHandleErrorLogsObservedSlowConsumerDropCount$' -count=1` — reached the production callback
  and failed at the missing `subject` assertion.
- GREEN focused unit: the same unit command passed; the `-race` variant also passed.
- GREEN focused real NATS: the same integration command passed under `-race`.
- Final gates: `task lint`, `go vet -tags=integration ./...`, `go vet -tags=live_llm ./...`, `go test -race ./...`,
  `task test:integration`, `task build`, `task schema:generate` plus no drift in `schemas/` or `specs/`, and
  `go test ./test/contract/...` all passed.
- Independent SemStreams implementation review returned `APPROVE` with no findings after repeated focused unit/race
  and five consecutive real-NATS race passes.

Product E2E attribution remains unclaimed; #954 is the durable coverage-gap artifact.
