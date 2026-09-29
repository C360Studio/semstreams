# Shared lifecycle cleanup implementation evidence

Checkpoint: dirty implementation over `32c013574eb82981188bd7ed79c9a924fcb99ecb`, 2026-09-29.
The source identities below, rather than the older committed HEAD, identify the tested implementation.
Independent implementation and metadata acceptance is recorded separately in `implementation-review.md`.

## Source and scope

| Source | Final SHA256 |
|---|---|
| component/lifecycle_test_suite.go | ec7c53826c9cb8409778346fcd2619a85d31677f85bb93234998d69a86f48ac3 |
| component/lifecycle_test_support_test.go | b822f4b634747142ad70ba85fa05ab4670738a59208a215c5eb6f414d7c2f824 |
| processor/rule/lifecycle_integration_test.go | 0d6dced7a2d32c98350d80a782850b153afbe9b5c2285af773c30b23c4d413d6 |

Exported test-support signatures are retained. No production lifecycle behavior, analyzer semantics or shared timeout
budget is changed. The owner records a synchronous concrete terminal attempt; a finite supplied context cannot
interrupt an implementation that ignores cancellation. A returned abort does not prove every internal worker joined.

## Causal proof and measured cost

| Invariant | Independent observation |
|---|---|
| Early failure finalizes its returned instance | Self-reexecuted failing NoLeaks child reports one factory handoff and one concrete Stop. Original helper failed this proof. |
| Assertion exit precedes substrate teardown | Failing Initialize child observes its lexical Stop before the substrate marker. |
| Live Start survives controlled Stop | Normal-cycle fake checks live accepted authority at Stop; exact cancellation-order mutation fails it. |
| Cancellation follows returned Stop | Gated live peer observes Stop, worker join and ended Start authority after parent failure reporting. |
| No implicit repeat after a concrete attempt | Transition cases distinguish terminal errors, bound expiry, explicit abort and explicit completed repeat. |
| Abort cause belongs to concrete result | Nil and wrong errors fail the abort expectation; only preserved caller cause satisfies it. |
| Operation and cleanup failures remain visible | Joined error assertions retain both independent sentinels. |
| Wrapper failure cannot consume base cleanup | All injection modes count base Stop; prerequisite Initialize/Start failures are observed through failing children. |
| Workers join on success and missing progress | One shared two-second fixture context bounds both gates; omitted Start fails with a named diagnostic after worker join. |
| Benchmarks check each owned iteration | Deliberately failing benchmark child observes operation and finalizer diagnostics. |
| Real adopter finalization precedes NATS cleanup | Rule forwards exact arguments/results and observes finite nonnil Stop entry/return for every factory handoff before TestClient cleanup. |

The fatal Initialize child proves that particular Goexit path; it does not alone prove a fatal assertion after an
accepted Start. Separate normal-cycle, Start-error, live-peer and mutation cases establish the reviewed combined
ownership evidence. Cooperative Stop deadline tests make no claim about uncooperative Stop containment.

Children reexecute the current test binary, have no subprocess descendants, use file-backed output and one synchronous
Cmd.Run/Wait owner with a four-second context. No shell or nested Go build is used. Child-entry top-level tests return
immediately in the parent process and are not independent behavioral passes.

Final focused race execution reported 15 top-level cases, including those child entries; the slowest actual parent
proof took 0.06s, package 1.677s. This is an observed local warm run (Go 1.26.4, darwin/arm64); no cold-cache guarantee
is inferred. The deliberately omitted-Start mutation failed in 2.01s (package 2.368s) with every worker joined.
Rule's selected integration took 2.772s package / 7.63s command wall time, graph-index 5.029s / 8.18s. Both canonical
runner invocations completed. Rule retains one TestClient/container owner; no separate container-leak experiment was
performed. The complete pre-push gate is recorded separately and is not implied by these focused results.

## Exact focused commands

All commands run from the claim worktree. The retained logs in `evidence/` preserve failures and passes; `sha256.json`
identifies their bytes. Empty revive output is paired with the developer's observed exit status zero.

```bash
# Original RED (exit 1), then repaired GREEN (exit 0):
go test -count=1 ./component -run '^TestSharedLifecycle(EarlyFailureFinalizes|InjectionFinalizesBase)$'
# Final frozen Go sources, exit 0:
go test -race -count=1 -json ./component -run '^TestSharedLifecycle'
go test -race -count=1 ./gateway/http ./input/udp ./output/websocket \
  -run '^(TestHTTPGateway_ComprehensiveLifecycle|TestUDPInput_ComprehensiveLifecycle|TestWebSocketOutput_ErrorInjection)$'
go test -race -count=1 ./output/websocket -run '^$' \
  -bench '^BenchmarkWebSocketOutput_Lifecycle$' -benchtime=1x
scripts/run-integration-tests.sh -run '^TestIntegration_RuleStandardLifecycle$' ./processor/rule
scripts/run-integration-tests.sh -run '^TestGraphIndex_ComprehensiveLifecycle$' ./processor/graph-index
```

The first RED reports skipped early-exit cleanup and wrapper bypass; the first GREEN passed in 0.383s. HTTP, UDP and
WebSocket focused race packages passed in 1.347s, 1.592s and 1.771s. All four benchmark modes ran exactly one iteration
and passed (package 1.364s). Rule and graph-index runs used the canonical lock-owning integration runner and race.
Focused integrations preceded only a context-parameter-order signature correction in shared support; final focused
race and the full pre-push gate cover the final source snapshot.

## Mutation sensitivity and restoration

The retained `.sh.txt` files record exact mutations, commands and causal-output checks; they are evidence, not CI
entrypoints. Each mutation required a failing exit and its named assertion, then restored a cp backup with checksum
verification and reran the relevant green proof. An ambiguous initial replacement attempt stopped before test
execution and is excluded from the mutation count.

| Mutation | Causal failure | Source checkpoint |
|---|---|---|
| Remove cycle ownership finalizer | terminal calls = 0 | c6670f88 |
| Cancel in workContext before Start | normal-cycle error | c6670f88 |
| Omit cancellation after Stop | live peer terminal/post-return cancellation | c6670f88 |
| Finalize injection wrapper instead of base | concrete Stop calls = 0 | c6670f88 |
| Discard cleanup error | lost operation or cleanup error | c6670f88 |
| Cancel accepted Start immediately before concrete Stop | accepted authority ended before controlled Stop | ec7c5382 |
| Omit accepted Start | missing accepted Start or reported peer failure; all workers joined | ec7c5382 / b822f4b6 |

The first five used suite SHA256 `c6670f886ef4be775c5a2a47276366522e1bd9414d82d049313ae561f557ca8f`,
restored MD5 `565a41db8b83ef7f2fd204b36b3567ff`. The later source correction placed context first in helper signatures;
those five runs are not presented as final-hash executions. The exact accepted-Start ordering and missing-Start
controls used final suite SHA256 above, restored MD5 `ada3dca9c2e4f15771661178c682d3db`, and passed restored race tests.
The earlier premature-cancel mutation is specifically before Start; the later mutation covers the original ordering.

## Linked issue truth

#1417 retains all 334 package cleanup debt entries. This slice fixes shared ownership and its real rule adopter;
it does not claim a reduction in that package backlog. Four old manual resolutions require exact reconciliation;
replacement evidence and approval are recorded in `implementation-review.md` and `approved-cleanup-records.json`.
The installed guard passed, exit zero, package 7.660s (`evidence/installed-guard.log.txt`). No analyzer relaxation or blind approval refresh is used.

#1416 was filed as an unobserved condition. The accepted inventory refutes its broad returning-running-Stop premise:
running Stop becomes terminal even when it returns cleanup error, so completed repeated Stop is nil. The old NoLeaks
path already stopped before cancelStart. Retained failed-Start cleanup and exits before a terminal attempt are distinct.
This implementation removes the cohort fallback and proves per-instance finalization before substrate teardown;
it does not claim reproduction of the hypothesized flake or authority to close #1416. #1404 production work is untouched.
