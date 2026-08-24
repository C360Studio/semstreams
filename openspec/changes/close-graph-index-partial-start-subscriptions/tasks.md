# Tasks

- [x] 1. Materialize the accepted inventory at SHA-256
      `00de531276a2e13e5509bae8168d83794b36947c2a0211829481ad6cbe1f2b1f`, record its independent `INVENTORY PASS`,
      and materialize the owner-accepted design at SHA-256
      `d8574c69664734ead58e7c81b31787cfb6a880a180440cfef2fce1207d6008bc`.
- [x] 2. RED: add one focused graph-index test file with deterministic real-NATS subscription-2 failure proving the
      admitted outgoing callback completes before cancellation and failed Start returns only after rollback resolves.
- [x] 3. RED: prove rollback expiry retains exactly the acquired outgoing subscription and cleanup authority, rejects
      another Start, and later `Stop(callerCtx)` completes cleanup and becomes a repeated no-op.
- [x] 4. RED: prove successful rollback permits the existing direct same-instance Start, observes the exact eight
      canonical subjects once for the committed run, serves exactly one outgoing response, and leaves no responder
      after Stop.
- [x] 5. Add the package-private concrete `subscribeForRequests` field in `processor/graph-index/component.go` and
      select it in `processor/graph-index/query.go` only when nonnil, preserving the production fallback, subjects,
      handlers, and order.
- [x] 6. Replace the graph-index-local failed-Start cleanup root/timeout with
      `lifecyclecleanup.RollbackFailedStart(parent, rollback)` while preserving drain-before-cancel, exact joins,
      synchronous cleanup, joined errors, and retained handles on incomplete rollback.
- [x] 7. GREEN: run `go test -race ./processor/graph-index` and record all three causal paths green without arbitrary
      sleeps.
- [x] 8. Verify the diff changes production only in `processor/graph-index/component.go` and
      `processor/graph-index/query.go`, uses one focused graph-index test file, and changes no service, sibling,
      `natsclient`, public, adopter, LLM, persona, prompt, runtime-agent, configuration, schema, or wire surface.
- [x] 9. Run `task lint`, `go test -race ./...`, `task test:integration`, `task schema:generate`, and
      `git diff --check`; record zero unintended schema/spec drift.
- [x] 10. Run `openspec validate close-graph-index-partial-start-subscriptions --strict`.
- [x] 11. Obtain independent SemStreams implementation and recorded-verification approval before integration
      (`FINAL APPROVE`, no findings).
- [x] 12. PR #1040 at the current reviewed implementation is fully CI-green and merge-ready; its `Closes #989`
      linkage will close the issue atomically on merge. This does not claim that the PR has merged yet or that any
      adjacent lifecycle, manager, sibling-owner, adopter, LLM, persona, prompt, model, or runtime-agent work is
      complete.
