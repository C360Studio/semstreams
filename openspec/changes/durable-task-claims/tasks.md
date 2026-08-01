# Tasks — durable-task-claims (gh#807)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Claim store and record

- [ ] 1.1 Declare `AGENT_TASK_CLAIMS` in `configs/agentic.json` and
      `configs/research-graph-e2e.json` (D3 retention; claims outlive AGENT retention) and
      wire bucket create-or-bind in agentic-loop component start beside `AGENT_LOOPS`.
- [ ] 1.2 Define the claim record type in `agentic/` (`task_id`, `loop_id`, `request_id`,
      `task_hash`, `claimed_at`, `claimant`) with payload-registry registration per
      `/new-payload` if it crosses a wire; failing round-trip test first.
- [ ] 1.3 Implement the canonical hash basis (D4): one pinned function zeroing volatile
      envelope fields; round-trip test publishes → decodes → asserts hash stability, and a
      mutation check removes one zeroed field to confirm the test FAILS.
- [ ] 1.4 Implement claim create/read over `kv.Create`/`Get`, mapping `ErrKVKeyExists` to the
      claim-conflict path. Integration test: two concurrent claimers, exactly one winner,
      loser reads winner's record (spec scenario "Two concurrent claimers").

## 2. Claim-gated acceptance in agentic-loop

- [ ] 2.1 Reorder `handleTaskMessage`: preflight validation → mint LoopID + initial
      RequestID → claim create → existing spawn path. Claim happens only after validation so
      invalid tasks never claim (design risk 2).
- [ ] 2.2 Implement the loser protocol (D2): hash mismatch → typed rejection with stable
      code; loop present → short-circuit returning claimed LoopID **including terminal
      loops**; loop absent → resume under claimed identity.
- [ ] 2.3 Failing tests first for all three loser paths, plus: restart redelivery (fresh
      LoopManager, claim present → no second loop), different-replica semantics (two
      components, one bucket), terminal redelivery no-op. These pin the spec scenarios in the
      `agentic-loop` delta.
- [ ] 2.4 Crash-window integration test: claim committed, loop NOT persisted, redeliver →
      loop created under claimed LoopID, initial request under claimed RequestID. Mutation
      check: make resume mint fresh IDs and confirm the test FAILS (the test must detect a
      dropped step, not a non-nil return).
- [ ] 2.5 Narrow `HasActiveLoopForTask` to fast-path only; the claim decides. Verify no
      caller treats the in-memory answer as authoritative (grep for the consumer).
- [ ] 2.6 Orphan visibility: metric on resume attempts finding claim-without-loop
      (design risk 2).

## 3. Deduplication identity at the stream layer

- [ ] 3.1 Stamp `Nats-Msg-Id = TaskID` at all three task publishers
      (`agentic-dispatch/component.go` bus path, `http.go` sync path, `rule/actions.go`) via
      `PublishToStreamWithMsgID`. Sweep ALL emitters of the migrated write-verb — grep for
      `agent.task` publishes beyond these three before claiming the set is closed.
- [ ] 3.2 Stamp `Nats-Msg-Id = <initial RequestID>` on the initial `agent.request` publish
      (all mint sites: `handlers.go:917`, continuation sites if they publish initial-shaped
      requests — enumerate from the owning component, not this list).
- [ ] 3.3 Set explicit `duplicates` on the AGENT stream in both configs (D5). Integration
      test: duplicate publish with same MsgID within window stores one copy.
- [ ] 3.4 `task schema:generate` + `git diff schemas/ specs/` clean; commit regenerated
      schemas if the claim type or configs surface in them.

## 4. Provider idempotency hand-off (staged — separable, may land as its own PR)

- [ ] 4.1 Design the per-request key carriage in `model/wire` (open question in design.md:
      context value vs options struct vs request field) — record the decision in design.md
      before implementing.
- [ ] 4.2 Thread the claimed initial RequestID as the idempotency key for providers that
      support one; test asserts the header reaches the HTTP request for both wire clients
      (chat + responses).
- [ ] 4.3 Reply on gh#807 with the shipped contract and the D2 residual bound so SemMachina
      can finish `mystery-companion-acceptance` 8.5 honestly (communicate, do not edit —
      sister repos are hands-off).

## 5. Gates

- [ ] 5.1 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration`.
- [ ] 5.2 BOTH suites: `go test -race ./...` AND
      `go test -race -tags=integration -p 2 -count=1 ./...`; grep `^FAIL` (pipeline exit
      codes report the tail stage).
- [ ] 5.3 `go test ./test/contract/...`.
- [ ] 5.4 `task e2e:agentic` — the tier on the touched path. Confirm the tier can actually
      fail before trusting green (gh#811 lesson).
- [ ] 5.5 `semstreams-reviewer` pass on the full diff.
- [ ] 5.6 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 5.7 Owner CONFIRM-CLOSE before closing gh#807.
- [ ] 5.8 Archive hygiene: `agentic-task-claims` gets its Purpose from the delta (written);
      confirm `agentic-loop`'s Purpose does not regress to a stub.
