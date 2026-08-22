# Tasks

- [x] 1. Materialize the accepted inventory at SHA-256
      `8a9b788c07396710d3540e0330e4bbe93b5b8a74c402cbde43c6e8f50747fe7d` and record independent `INVENTORY PASS`.
- [x] 2. Materialize the corrected design at SHA-256
      `b1906e949e9d731ded36f936630264f6c3fa360fae2ae9775a398f52506d6a37`, record independent
      `DESIGN REVIEW PASS`, and record owner acceptance of reviewed rulings R1-R8.
- [x] 3. Materialize the accepted proposal, design, conservative task truth, and `component-lifecycle` capability
      delta; strict-validate the OpenSpec change.
- [x] 4. Correct the shared suite to prove a controlled finite Stop while Start authority remains live and a
      separate fresh-instance accepted-parent-cancellation path followed by bounded Stop.
- [x] 5. RED: add deterministic UDP owner tests for normal Stop, accepted-parent cancellation, completed repeat,
      blocked-join caller error, immediate later-Stop no-op, post-release Start-owner finalization/resource/health
      truth, failed bind cleanup, and exactly one teardown without sleeps.
- [x] 6. Clarify `LifecycleComponent` GoDoc and narrow `StandardLifecycleTests` without changing lifecycle signatures
      or `LifecycleFactory`.
- [x] 7. Migrate UDP to private cancel, exact socket fence, and a private Start-owned completion channel published
      before read-loop launch and closed only after synchronous Start-owner finalization. Permit only the first Stop
      that consumes cancel authority to select completion against its exact caller context; add no stored context,
      invented root, Stop-launched waiter, detached cleanup, generic finalizer, replay, or later rejoin.
- [x] 8. Verify gateway/http and graph-index need no production change, preserve graph-index owner proofs and
      output/websocket helper/benchmark compatibility, and audit zero forbidden service, sister, config, schema,
      wire, durable-state, payload, query, agent, LLM, persona, role, prompt, model, ops-agent, or scenario changes.
- [x] 9. Run focused race tests for component, gateway/http, input/udp, and output/websocket; run the graph-index
      real-NATS lifecycle integration; repeat the deterministic UDP no-rejoin/post-release proof under
      `-race -count=10`.
- [x] 10. Run `task lint`, `go test -race ./...`, `task test:integration`, `go build ./...`,
      `task schema:generate` with zero schema/spec drift, `go test ./test/contract/...`, and `git diff --check`.
- [x] 11. Run `openspec validate align-standard-lifecycle-tests --strict --no-interactive` on the final candidate and
      obtain fully green hosted CI evidence for reviewed implementation commit `97a3ea23` on PR #1048.
- [x] 12. Obtain independent SemStreams implementation and recorded-verification approval before integration
      (`APPROVE`, round 2, no findings).
- [x] 13. PR #1048's reviewed implementation commit `97a3ea23` is fully CI-green, and its `Closes #1022` linkage is
      configured to close the issue atomically on merge. This task-truth-only evidence follow-up changes no
      implementation, and current-head branch-protection CI must pass before merge. This does not claim that the PR
      has merged, that #1022 has closed, or that adjacent service-lifecycle, restart, #867/#1012/#1013, tag-readiness,
      adopter, agent, LLM, persona, role, prompt, model, ops-agent, or scenario work is complete.
