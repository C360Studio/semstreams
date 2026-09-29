# Shared lifecycle cleanup implementation evidence

Checkpoint: dirty implementation over `32c013574eb82981188bd7ed79c9a924fcb99ecb`, 2026-09-29.
The source identities below, rather than the older committed HEAD, identify the tested implementation.
Independent implementation and metadata acceptance is recorded separately in `implementation-review.md`.

## Source and scope

| Source | Final SHA256 |
|---|---|
| component/lifecycle_test_suite.go | 6d1c84672111de2828ef1d2042ad39d52aad40c257c64b464beeb2871685745c |
| component/lifecycle_test_support_test.go | 11074b40566779d8979539cc5266d2cb8f8f4481d7b345991823a10e71d91bb6 |
| processor/rule/lifecycle_integration_test.go | 0d6dced7a2d32c98350d80a782850b153afbe9b5c2285af773c30b23c4d413d6 |

Exported test-support signatures, production lifecycle behavior and analyzer semantics are retained. The ordinary
suite retains its five-second terminal budget; finite five-second work authority is new. Benchmarks retain their
original one-second terminal budget through the same private ownership path. Focused costs are measured below. The owner records a synchronous concrete terminal attempt; a finite supplied context cannot
interrupt an implementation that ignores cancellation. A returned abort does not prove every internal worker joined.

## Causal proof and measured cost

| Invariant | Independent observation |
|---|---|
| Early failure finalizes its returned instance | Self-reexecuted failing NoLeaks child reports one factory handoff and one concrete Stop. Original helper failed this proof. |
| Assertion exit precedes substrate teardown | Failing Initialize child observes its lexical Stop before the substrate marker. |
| Live Start survives controlled Stop | Normal-cycle fake checks live accepted authority at Stop; exact cancellation-order mutation fails it. |
| Cancellation follows returned Stop | Gated live peer observes Stop, worker join and ended Start authority after parent failure reporting. |
| No implicit repeat after a concrete attempt | Transition cases distinguish terminal errors, bound expiry and abort; the portable/adopter suite covers explicit completed repeat. |
| Abort cause belongs to concrete result | Nil and wrong errors fail the abort expectation; only preserved caller cause satisfies it. |
| Operation and cleanup failures remain visible | Joined error assertions retain both independent sentinels. |
| Wrapper failure cannot consume base cleanup | All injection modes count base Stop; prerequisite Initialize/Start failures are observed through failing children. |
| Workers join on success and missing progress | One shared two-second fixture context bounds both gates; omitted Start fails with a named diagnostic after worker join. |
| Benchmarks check each owned iteration | Deliberately failing benchmark child observes the original operation error and a concrete Stop event/count. |
| Real adopter finalization precedes NATS cleanup | Rule forwards exact arguments/results and observes finite nonnil Stop entry/return for every factory handoff before TestClient cleanup. |

The fatal Initialize child proves that particular Goexit path; it does not alone prove a fatal assertion after an
accepted Start. Separate normal-cycle, Start-error, live-peer and mutation cases establish the reviewed combined
ownership evidence. Cooperative Stop deadline tests make no claim about uncooperative Stop containment.

Children reexecute the current test binary, have no subprocess descendants, use file-backed output and one synchronous
Cmd.Run/Wait owner with a four-second context. No shell or nested Go build is used. Child-entry top-level tests return
immediately in the parent process and are not independent behavioral passes.

The ec7c5382 checkpoint focused race execution reported 15 top-level cases, including those child entries; the slowest actual parent
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
# ec7c5382 support checkpoint, b822f4b6 proof source; exit 0:
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
Focused integrations preceded only a context-parameter-order signature correction in shared support; focused
race covered the ec7c5382 snapshot. The later contract-table correction and its focused checks are recorded below;
the corrected full pre-push gate remains pending until separately recorded.

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
controls used suite SHA256 `ec7c53826c9cb8409778346fcd2619a85d31677f85bb93234998d69a86f48ac3`, restored MD5 `ada3dca9c2e4f15771661178c682d3db`, and passed restored race tests.
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

## Initial full gate and correction

The first `PATH=/private/tmp/gh1064-task-tools:$PATH task check:push` stopped after 28.108s with Task exit201
(underlying contract test exit1). Lint, build, tagged vet and schema drift passed before
`TestProductionStructsRetainNoContext` rejected the `start` context-provider field in the portable-error test table.
Because shared support is compiled in a non-`_test.go` file, this structural contract applies to that table too.
The failure is retained in `evidence/preflight-initial-contract-failure.log.txt`; unit race and full integration
were not reached. Correction and exact re-review are required; a rerun alone is not accepted as a fix.

The correction removes the context-provider table field and constructs each rejected input locally. It is
representation-only: context choices, early cancellation, ownership and assertion behavior are retained. Targeted
contract proof passed in 1.537s. Focused race after correction passed component (1.593s), HTTP (1.289s), UDP (1.832s)
and WebSocket (1.568s); pinned focused revive passed. The correction does not alter the earlier mutations' ownership,
ordering, injection or error-retention targets; their original checkpoint limitations remain explicit. Independent
review approved the one affected declaration fingerprint and explanation, with all 334 debt entries unchanged.

Exact correction commands (red and green contract invocations use the same selection):

```bash
go test -count=1 ./test/contract -run '^TestProductionStructsRetainNoContext$'
go test -race -count=1 ./component ./gateway/http ./input/udp ./output/websocket \
  -run '^(TestSharedLifecycle|TestHTTPGateway_ComprehensiveLifecycle|TestUDPInput_ComprehensiveLifecycle|TestWebSocketOutput_ErrorInjection)'
go tool revive -config revive.toml -formatter friendly ./component/... ./processor/rule/...
```

Shared records updated after independent review:

- #1417 batch progress: https://github.com/C360Studio/semstreams/issues/1417#issuecomment-5890924814
- #1416 source/proof disposition: https://github.com/C360Studio/semstreams/issues/1416#issuecomment-5890925182

Both remain open. The parent update names graph-ingest as the next default package batch and explicitly retains 334
entries. The rule issue update preserves the distinction between its unobserved hypothesis and demonstrated fixes.

## Interrupted validation after benchmark budget review

The corrected-context full gate at `93c40965` passed lint/build/tagged vet/schema/contract, repository-wide unit
race and the early integration packages. It was intentionally stopped after 624.451s when independent review
confirmed an unjustified benchmark cleanup-budget increase (one second to the shared five seconds). This is an
interrupted run, not a full pass or a spontaneous test failure. The archived log and status are retained as
`evidence/preflight-interrupted-budget-review.*`. The final source must retain the benchmark's one-second budget
and obtain targeted proof, exact guard reconciliation and final validation.

Task acknowledged TERM but its integration child continued. The coordinator then stopped the verified owned
Go test process tree; the runner exited and all observed owned test processes were gone. No other session's
process or Docker resources were stopped. This observed cancellation-propagation gap belongs with #1293's existing
common-gate/failure-visibility work, not an assertion that this PR fixes runner cancellation.

The cancellation observation was recorded for consideration in #1293 without changing its scope or placement:
https://github.com/C360Studio/semstreams/issues/1293#issuecomment-5891112448. The observation concerns direct TERM
to the Task PID only; terminal process-group Ctrl-C, GitHub cancellation and desktop-stop behavior were not tested.

## Final benchmark budget proof

Final source hashes are in the first table. The private owner defaults to the ordinary five-second Stop budget;
benchmark setup selects the original one-second Stop budget before finalization is installed. Causal RED observed
actual deadlines near 4.99998s in both Initialize fallback and explicit Stop. GREEN preserves one-second benchmark
cleanup, ordinary five-second cleanup, concrete Stop errors and exactly one concrete attempt. The initial combined
benchmark proof was split into three independent top-level cases before the final race measurement.

`go test -race -count=1 -json ./component -run '^TestSharedLifecycle'` passed at this final checkpoint: 19 top-level
cases, maximum 1.06s, package 3.779s. The two healthy benchmark children account for 1.04s/1.06s observations; the
concrete Stop error child took 0.04s. These exceed the one-second target slightly but remain below the existing
five-second ceiling; no test budget was increased to accommodate them. They inspect supplied deadlines rather
than sleeping until expiry. Context-ownership contract passed 1.520s; pinned focused revive and diff checks passed.

Three exact manual records needed dependency refresh after the budget field/constructor/helper changed; their
identities, site fingerprints and classifications remain unchanged. Independent review preserves all 334 debt
entries and the other 86 resolutions. The final installed-guard/full-gate evidence is recorded separately below.

Final approved-budget installation passed the real cleanup guard, exit zero, package 7.942s
(`evidence/final-installed-guard.log.txt`). The final inventory verifier passed 63/63 pins.

## Final full preflight

`PATH=/private/tmp/gh1064-task-tools:$PATH task check:push` passed, exit zero, in **914.322s (15m14s)** on
Go 1.26.4 darwin/arm64 with pinned Task 3.53.1, at commit `0c805e58` plus the prepared archive/spec synchronization.
The source and baseline hashes in this record were verified unchanged after execution. This run passed cleanup
admission, lint, build, default/integration/live_llm vet, schema generation with no drift, contract tests, unit race
and the canonical additive integration race suite. Unit output includes normal Go cache hits; integration uses
`-count=1 -p 2` under the runner's host lock. The log ends with `[INTEGRATION] tests complete`.

The final real WebSocket benchmark adopter also passed all four modes at exactly one iteration, package 1.327s,
using the restored one-second terminal budget. Exact command is the benchmark smoke command recorded above.
Strict OpenSpec validation passed 58/58 specs, property references resolved 445/445 and inventory pins passed 63/63.
The worktree's OpenSpec queue is empty after archive preparation. No E2E tier is claimed or required for this
nonbreaking test-support repair; exported APIs and production lifecycle behavior are unchanged.

Raw results are retained under `evidence/final-*` and included in the SHA256 manifest. The earlier 28.108s contract
failure and 624.451s intentional interruption remain separate historical outcomes. Neither is relabeled as a pass.
Hosted CI remains a separate current-head requirement after push; these records make no post-merge claim.
