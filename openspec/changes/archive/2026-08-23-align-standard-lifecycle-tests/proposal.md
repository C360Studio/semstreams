# Change: Align StandardLifecycleTests with component lifecycle authority

Status: architecture accepted by the owner on 2026-08-22 after independent SemStreams inventory and design review.

Baseline: `43dbf6fb72a9c346750b9c6b96fa8df8165f7bbe`.

Accepted evidence:

- `docs/proposals/gh1022-standard-lifecycle-tests-inventory.md`, SHA-256
  `8a9b788c07396710d3540e0330e4bbe93b5b8a74c402cbde43c6e8f50747fe7d` (`INVENTORY PASS`).
- `docs/proposals/gh1022-standard-lifecycle-tests-design.md`, SHA-256
  `b1906e949e9d731ded36f936630264f6c3fa360fae2ae9775a398f52506d6a37` (`DESIGN REVIEW PASS`).
- Owner acceptance of reviewed rulings R1-R8, including the corrected controlled-Stop and UDP completion-observation
  boundaries.

Final implementation evidence and correction propagation are recorded in `conformance.md`.

## Why

The exported shared suite advertises concurrent Initialize, concurrent Stop result sharing/replay, later-Stop rejoin,
and post-Stop reinitialization that the public interface does not promise and ADR-095 rejects for running-owner
authority. It also omits separate portable coverage for controlled finite Stop while Start authority remains live and
for accepted Start-parent cancellation followed by a bounded Stop.

UDP retains the one measured later-Stop rejoin path among current suite adopters. The interface comment also implies
cancel-before-cleanup even though native owner protocols can require admission drain while callback authority remains
live.

## What changes

- Narrow `StandardLifecycleTests` to the portable `LifecycleComponent` floor without changing `LifecycleFactory`.
- Clarify owner-specific terminal ordering in `LifecycleComponent` GoDoc.
- Replace UDP's later-Stop rejoin behavior with one-shot cancellation and a private Start-owned completion channel
  selected only by the first caller-bounded Stop.
- Keep synchronous UDP state/resource finalization owned by the Start goroutine and add deterministic UDP owner tests.
- Seed current `component-lifecycle` capability truth.

## Production ownership

- `component/lifecycle.go`
- `input/udp/udp.go`

Expected test ownership is `component/lifecycle_test_suite.go` and `input/udp/udp_lifecycle_test.go`. Existing
gateway/http, graph-index, and output/websocket tests are verification-only.

## Non-goals

- No lifecycle method signature or exported test-factory change.
- No production change to gateway/http, graph-index, service, ComponentManager, or sister repositories.
- No shared lifecycle wrapper, generic finalizer framework, exported fault harness, or public cleanup state.
- No stored production context, invented context root, Stop-launched waiter, or detached cleanup goroutine.
- No concurrent Stop executor election, result replay, later running-generation rejoin, reinitialization, or
  restartable-instance promise.
- No config, schema, wire contract, NATS subject/bucket/stream, persistent-state, payload, or query change.
- No extension of `service-shutdown`, the workflow `lifecycle` capability, or `component-runtime-config`.
- No resolution of #867, #1012, or #1013 and no tag-readiness claim.
- No agent, LLM, persona, role, prompt, model call, ops agent, or E2E scenario.
