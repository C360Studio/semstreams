# Implementation conformance: Align StandardLifecycleTests

Status: independent implementation re-review round 2 returned `APPROVE` with no findings on 2026-08-22. Hosted CI
and integration/issue closure are pending.

Accepted architecture:

- Inventory SHA-256: `8a9b788c07396710d3540e0330e4bbe93b5b8a74c402cbde43c6e8f50747fe7d`
  (`INVENTORY PASS`).
- Design SHA-256: `b1906e949e9d731ded36f936630264f6c3fa360fae2ae9775a398f52506d6a37`
  (`DESIGN REVIEW PASS`, owner-accepted R1-R8).

## Ruling-to-implementation map

| Ruling | Binding result | Final evidence | Result |
|---|---|---|---|
| R1 — portable floor, unchanged factory | Keep `LifecycleFactory` unchanged and make the shared suite a portable minimum rather than a resource-specific fault harness. | `component/lifecycle_test_suite.go:15-33` retains `func() LifecycleComponent`, names the portable floor, and delegates resource-specific proof to focused owner tests. | CONFORMS |
| R2 — corrected shared assertions | Separate controlled Stop with live Start authority from accepted-parent cancellation; retain only the accepted portable assertions and fresh-instance smoke. | `component/lifecycle_test_suite.go:36-142` defines the portable cases and error paths; `:144-189` uses fresh instances only. UDP causally proves accepted-parent completion before separate Stop at `input/udp/udp_lifecycle_test.go:128-152`. | CONFORMS |
| R3 — failed Start remains owner-specific | Keep only safe Stop after pre-action Start rejection in the shared suite; add no exported failure harness. | `component/lifecycle_test_suite.go:115-142` covers pre-canceled/pre-expired rejection and safe Stop. UDP bind-failure ownership is local at `input/udp/udp_lifecycle_test.go:260-287`. No exported hook was added. | CONFORMS |
| R4 — owner-specific Stop order | Clarify caller-owned lifetime and caller-bounded owner ordering without universal cancel-before-drain, replay, rejoin, reinitialize, or restart promises. | `component/lifecycle.go:43-52` states the portable contract. Capability truth is `specs/component-lifecycle/spec.md:3-45`. | CONFORMS |
| R5 — UDP owner correction | Publish private cancel/completion authority before launch; let the Start owner finalize and close completion; permit only the first Stop to observe completion against its caller context; preserve no-rejoin and honest health. | Private fields are `input/udp/udp.go:131-139`; health observes running plus exact socket state at `:354-370`; Start publishes authority and its goroutine cancels/finalizes/closes completion at `:425-484`; first-Stop-only observation is `:518-545`. Owner proofs are `input/udp/udp_lifecycle_test.go:91-287`. | CONFORMS |
| R6 — aligned adopters remain production-stable | Make no gateway/http or graph-index production change and preserve verification-only lifecycle/helper consumers. | `git diff --exit-code -- gateway/http/http_lifecycle_test.go processor/graph-index/lifecycle_integration_test.go processor/graph-index/lifecycle_order_test.go processor/graph-index/failed_start_subscription_test.go output/websocket/websocket_test.go` returned zero. Focused package and graph-index integration commands below passed. | CONFORMS |
| R7 — dedicated current capability | Add only a `component-lifecycle` delta; do not extend service-shutdown, workflow lifecycle, or runtime-config truth. | `specs/component-lifecycle/spec.md:1-60` contains the sole capability delta and strict validation passes. | CONFORMS |
| R8 — adjacent boundaries | Add no service, sister-repository, config, schema, wire, durable-state, payload, query, agent/LLM/persona/role, or adjacent-issue claim. | Final production/test diff names only `component/lifecycle.go`, `component/lifecycle_test_suite.go`, `input/udp/udp.go`, and `input/udp/udp_lifecycle_test.go`; schema generation recorded zero schema/spec drift. | CONFORMS |

No `DEVIATION` is recorded.

## RED/GREEN record

Initial no-rejoin RED:

```text
go test ./input/udp -run TestUDPInput_StopDoesNotRejoinAfterCallerBoundWins -count=1
FAIL: later Stop waited for the running generation
```

The implementation replaced later-Stop observation with first-Stop-only cancel/completion authority at
`input/udp/udp.go:518-545`.

Implementation review found that natural read-loop exit did not yet release the derived cancellation linkage. The
review-fix RED was:

```text
go test ./input/udp -run TestUDPInput_NaturalOwnerExitReleasesDerivedCancel -count=1
FAIL: Start owner did not release the derived parent-cancellation linkage
```

The Start-owned finalizer now invokes the exact derived cancel on every owner exit at `input/udp/udp.go:463-466`.
`input/udp/udp_lifecycle_test.go:154-193` proves the parent linkage is released on natural non-Stop exit.

Final focused GREEN, independently rerun by the technical writer:

```text
go test -race ./component ./gateway/http ./input/udp ./output/websocket -count=1
ok github.com/c360studio/semstreams/component
ok github.com/c360studio/semstreams/gateway/http
ok github.com/c360studio/semstreams/input/udp
ok github.com/c360studio/semstreams/output/websocket

go test -race ./input/udp \
  -run 'TestUDPInput_(ControlledStopWithLiveParentFinalizesOnce|AcceptedStartParentCancellationIsObservable|NaturalOwnerExitReleasesDerivedCancel|StopDoesNotRejoinAfterCallerBoundWins|FailedBindLeavesNoRuntimeAuthority)$' \
  -count=10
ok github.com/c360studio/semstreams/input/udp

go test -race -tags=integration ./processor/graph-index \
  -run TestGraphIndex_ComprehensiveLifecycle -count=1
ok github.com/c360studio/semstreams/processor/graph-index
```

## Implementation-review corrections

1. Parent-cancellation proof originally called Stop immediately after canceling the accepted parent, so Stop could
   have caused the observed exit. The corrected test captures completion, cancels the parent, awaits the Start-owned
   completion channel and WaitGroup, and only then calls a separately bounded Stop at
   `input/udp/udp_lifecycle_test.go:128-152`.
2. Natural owner exit originally left the `context.WithCancel` parent linkage retained. The Start-owned finalizer now
   calls its exact local cancel before state/resource finalization at `input/udp/udp.go:463-481`; the private observed
   parent test at `input/udp/udp_lifecycle_test.go:154-193` proves release without Stop.

Both corrections preserve no stored `context.Context`, no invented root, no Stop-launched waiter or detached cleanup,
no generic finalizer framework, and no replay or later-Stop rejoin.

## Recorded local gates

Developer-recorded final candidate results:

| Command | Result |
|---|---|
| `task lint` | PASS |
| `go test -race ./...` | PASS |
| `task test:integration` | PASS |
| `go build ./...` | PASS |
| `task schema:generate` | PASS; zero `schemas/` or `specs/` drift |
| `go test ./test/contract/...` | PASS |
| `git diff --check` | PASS |
| `openspec validate align-standard-lifecycle-tests --strict --no-interactive` | PASS |

### Post-review final-candidate rerun

After the review corrections and round-2 `APPROVE`, root reran the local final candidate with approved Docker access:

| Command | Final-candidate result |
|---|---|
| `task test:integration` | PASS; `[INTEGRATION] tests complete` |
| `task schema:generate` | PASS; zero `schemas/` or `specs/` diff |
| `task lint` | PASS |
| `go build ./...` | PASS |
| `go test ./test/contract/...` | PASS |
| `openspec validate align-standard-lifecycle-tests --strict --no-interactive` | PASS |
| diff and schema checks | PASS |

The final-candidate integration package timings were UDP 9.736s, graph-index 95.217s, graph-ingest 88.338s,
agentic-tools 78.549s, rule 85.123s, and service 69.579s.

## Independent implementation re-review

Round 2 returned `APPROVE` with no findings. The independent reviewer recorded:

| Verification | Round-2 result |
|---|---|
| Focused package race tests for component, gateway/http, input/udp, and output/websocket | PASS |
| Corrected UDP owner proofs under `-race -count=50` | PASS |
| `go test -race ./...` | PASS |
| `openspec validate align-standard-lifecycle-tests --strict --no-interactive` | PASS |
| `git diff --check` | PASS |

Graph-index integration was unavailable in round 2 because the review sandbox could not access Docker. This is not
recorded as a round-2 pass. The same graph-index lifecycle integration passed independently in review round 1 and in
the developer and technical-writer evidence recorded above.

Hosted CI remains pending and unverified. Integration and issue closure remain pending.
