# Tasks — agentic-loop-same-loop-terminal-adopts-winner (#1399)

Base: main `078782b1`. Design: `design.md`; the ruling is § 0. **No task asserts a post-merge fact.**

## 1. The terminal owner adopts a same-loop terminal of any kind

- [x] 1.1 `createTerminalMarker` (`processor/agentic-loop/terminal_owner.go`): only a foreign loop ID refuses; a
      same-loop saved terminal of a different kind is adopted; the adoption Warn line names `kind` and
      `candidate_kind`.
- [x] 1.2 `settleTerminal` (`processor/agentic-loop/state.go`): re-seat `State` and `Outcome` from the adopted kind and
      clear the losing outcome's fields; the comment no longer claims adoption requires a matching kind.
- [x] 1.3 Comments and docs follow: the `component.go` carrier comment, `doc.go` COMPLETE_ section,
      `docs/operations/migration-beta162-to-beta163.md`.

## 2. Tests

- [x] 2.1 W2 on the production lanes: the latch test inverted (`approval_cap_sweep_integration_test.go`) and the
      unit-level `decide` arm of the cap test.
- [x] 2.2 W1's different-kind arm (`lost_terminal_record_integration_test.go`).
- [x] 2.3 `terminal_owner_test.go`: the other-kind arm adopts; the foreign-loop-ID arm refuses; `settleTerminal`
      re-seats each adopted kind.
- [x] 2.4 #1362 H1 (`terminal_tool_redelivery_integration_test.go`): the completion redelivered first adopts the
      crashed cancel.
- [x] 2.5 Mutation evidence per site (the refusal condition restored; the re-seat removed), recorded on PR #1402.
      DONE at `7076af89`: five mutants, each killed and each restored by `cp` with a matching md5 — the refusal
      condition (five tests red), the re-seat disabled (nine tests and subtests red), the re-seat forced on
      the same kind (the truncated control red), the failure branch keeping `Result` (two red), and the audit line's
      `candidate_kind` dropped (two red). Review amendment 4, at `78829542`: deleting the failure branch's
      `entity.CancelledBy, entity.CancelledAt = "", time.Time{}` (`state.go:2036`) turns
      `TestSettleTerminalReSeatsTheEntityToTheAdoptedKind/a_saved_failure_adopted_over_a_cancel` red ("Should be
      empty, but was operator"); restored by `cp`, md5 `7ad5b0a4bdac808219804fa237bff19d` before and after.

## 3. Gates

- [x] 3.1 `task lint`; `go test -race ./processor/agentic-loop/... ./agentic/...`; `go test -tags=integration -race -p 2
      ./processor/agentic-loop/...`; `task schema:generate` with no drift; `openspec validate
      agentic-loop-same-loop-terminal-adopts-winner --strict`; `task spec:properties` — results recorded on PR #1402.
      DONE at `7076af89`: every gate exit 0; `spec:properties` 443/443; the integration suite ran locally with zero
      compose stacks before and after.
- [x] 3.2 Implementation review recorded on PR #1402. DONE: round 1 PASS WITH AMENDMENTS at `3e474529` (no BLOCKING or
      HIGH), posted as PR #1402 issuecomment-5856563315; fix pass `78829542` and `6d07286c`.
- [x] 3.3 BREAKING gate: `task e2e:agentic` green at the final code revision. DONE at `6d07286c`: "Scenario completed
      successfully" duration=5m35.79s, assertions_run=20, task_exit=0, compose stacks 0 before and after (run by the
      coordinator).
