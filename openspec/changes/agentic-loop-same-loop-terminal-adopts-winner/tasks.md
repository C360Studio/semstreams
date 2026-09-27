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
      condition (five tests red), the re-seat disabled (eight subtests red), the re-seat forced on the same kind (the
      truncated control red), the failure branch keeping `Result` (two red), and the audit line's `candidate_kind`
      dropped (two red).

## 3. Gates

- [x] 3.1 `task lint`; `go test -race ./processor/agentic-loop/... ./agentic/...`; `go test -tags=integration -race -p 2
      ./processor/agentic-loop/...`; `task schema:generate` with no drift; `openspec validate
      agentic-loop-same-loop-terminal-adopts-winner --strict`; `task spec:properties` — results recorded on PR #1402.
      DONE at `7076af89`: every gate exit 0; `spec:properties` 443/443; the integration suite ran locally with zero
      compose stacks before and after.
- [ ] 3.2 Implementation review recorded on PR #1402.
