# Tasks: Prove slow-consumer attribution through the product assembly

## 1. Accepted inventory and design

- [x] 1.1 Record the assembled behavior, E2E topology, deterministic-control gap, observation path, assertion
  accounting, current owner, present consumer, and adopter seam.
- [x] 1.2 Obtain independent `INVENTORY PASS` and `DESIGN PASS`.
- [x] 1.3 Obtain owner acceptance of R1-R15 on 2026-08-13 with the narrow-implementation condition.
- [x] 1.4 Archive completed #950 and #961 changes into current specs without rewriting completed task history.
- [x] 1.5 Materialize the accepted proposal, design, capability delta, tasks, and per-ruling evidence skeleton.

## 2. TDD implementation

- [x] 2.1 Add behavior-level failing tests for dynamic success/partial-failure assertion counts, JSON diagnostic
  parsing, tagged/default build shape, and synchronized probe behavior.
- [x] 2.2 Record reproducible focused RED evidence against the pre-change assembly.
- [x] 2.3 Add the minimum one-hook/default/tagged implementation, isolated scenario/stack/task, and CI gate.
- [x] 2.4 Pass focused unit and race GREEN without arbitrary sleeps.

## 3. Product and mutation evidence

- [x] 3.1 Pass the isolated product E2E twice from clean disposable stacks.
- [x] 3.2 Falsify one attributed production field, observe isolated E2E RED, and restore by `cp` with matching checksum.
- [x] 3.3 Record reproducible per-ruling file:line evidence and correct every superseded claim.

## 4. Verification and review

- [x] 4.1 Run lint, repository race, integration, build, schema twice/no drift, contract, and strict OpenSpec gates.
- [x] 4.2 Pass ordinary `task e2e:core` unchanged and the relevant E2E Ladder gate.
- [x] 4.3 Obtain independent SemStreams reviewer approval before integration.

## Executed evidence

- RED untagged/scenario: `env GOCACHE=/private/tmp/semstreams-gh954-gocache go test ./cmd/semstreams
  ./cmd/e2e ./test/e2e/scenarios` failed because the new hook, result field, parser, and assertion functions were
  undefined against the pre-change assembly.
- RED tagged probe: `env GOCACHE=/private/tmp/semstreams-gh954-gocache go test -tags=e2e_slow_consumer
  ./internal/e2eslowconsumer` failed because `Run` was undefined.
- Focused GREEN: the scenario/assertion-count tests, untagged hook/build-contract tests, tagged and untagged
  `cmd/semstreams` builds passed. `go test -race -tags=e2e_slow_consumer ./internal/e2eslowconsumer -run
  TestRunUsesAndRestoresInstalledErrorHandler -count=1` passed in 1.417s.
- Isolated product GREEN: two clean `task e2e:slow-consumer` runs passed with `assertions_run=11`,
  `known_dropped=8`, scenario durations 35.728ms and 42.271ms, and clean compose teardown. After a lint-only hook
  placement correction, a third post-restoration run passed in 49.651ms with the same values and teardown.
- Mutation RED: after backing up `natsclient/client.go`, changing the structured key `subject` to `subject_mutated`
  made `task e2e:slow-consumer` fail after its bounded observation window with `assertions_run=0`. The task tore down
  cleanly. The source backup, pre-mutation source, and `cp`-restored source all had MD5
  `bb9790520cc51e6fba141bc04b72b8cd`; the restored production file had no diff.
- Final code gates: `task lint`, `go test -race ./...`, `task test:integration`, `task build`, schema generation twice
  with no `schemas/` or `specs/` drift, `go test ./test/contract/...`, `openspec validate --all --strict` (50/50),
  and `git diff --check` all passed.
- Final product gates: ordinary untagged `task e2e:core` passed 3/3 and cleanly tore down. The existing per-PR
  `task e2e:statistical` gate passed all 41 stages in 29.184s and cleanly tore down.
- Independent SemStreams implementation review returned `APPROVE` with no blocking, high, medium, or nit findings.
  The reviewer independently verified R1-R15, tag isolation, callback restoration/delegation, parser behavior,
  assertion accounting, archive history, and absence of new public/config/control/durable surfaces. Focused tests and
  five consecutive tagged real-NATS race runs passed during review.
