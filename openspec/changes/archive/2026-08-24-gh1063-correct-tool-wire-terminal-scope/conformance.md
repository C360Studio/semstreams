# Conformance: GH-1063 tool wire terminal-scope correction

Status: implementation, focused evidence, and the reviewer-directed aggregation-order documentation correction are
complete; independent implementation re-review remains pending. No production, merge-readiness, or tag-readiness
claim is made.

Accepted authority:

- Parent inventory: `INVENTORY PASS`, SHA-256
  `d91a49caa42d027df482c0c8adc4ebe4f290459e5b161a81bf8d11a372662d7a`.
- Parent #1063 design: reviewed and owner accepted, SHA-256
  `56fa9dc95a4dbf6f3f7d121912972e036f7c1c2d55a8834e991eeafa8b37ae7a`.
- Terminal-scope correction: reviewed and owner accepted, pre-acceptance SHA-256
  `22375a461578b6100a96d838d6726c2d4f2f10bedcfe80b483fc7914e9117332`.

| Ruling or obligation | Evidence | Status |
|---|---|---|
| No local parallelism promise | E1: corrected spec and outward docs | PASS |
| MaxAckPending is admission only | E2: policy, component value, tuning guide | PASS |
| Approval pause remains nonterminal | E3: current truth, production, named test | PASS |
| Terminal execution/rejection is correlated and durable | E4: production and selected outcome tests | PASS |
| Causal multiple-call proof | E5: exact ID/content collector | PASS |
| Repeated Docker proof | E6: count-20 selected gate | PASS |
| Timing/sleep debt removed | E7: historical RED, causal GREEN, policy guard | PASS |
| Forced omissions | E8: three failures and checksum restoration | PASS |
| No production behavior change | E9: identical component hash | PASS |
| Durable artifact truth | E10: active change, contracts, strict validation | PASS |

Evidence citations:

- E1: `openspec/specs/agentic-tools/spec.md:519-538`; `processor/agentic-tools/README.md:35-39`;
  `processor/agentic-tools/doc.go:278-286`; `docs/concepts/13-agentic-systems.md:313-324`, which explicitly makes
  source ordinal trajectory/audit evidence rather than a result-aggregation order contract.
- E2: `openspec/specs/jetstream-consumer-policy/spec.md:6-28`; `processor/agentic-tools/component.go:380-408`;
  `docs/advanced/11-jetstream-tuning.md:80-94,217-241`.
- E3: current spec lines 447-450 and 525-528; `processor/agentic-tools/component.go:668-695`; gate 1 below.
- E4: `processor/agentic-tools/component.go:640-665,699-720`; selected race evidence in gate 2.
- E5: `processor/agentic-tools/tools_integration_test.go:545-663` checks exact IDs/content and rejects missing,
  duplicate, unexpected, or error-bearing results without a timing oracle.
- E6: gate 3 below showed 20 starts and 20 passes with no prohibited result state.
- E7: accepted inventory lines 244-246; gates 3 and 5 below.
- E8: forced omissions and exact restoration hashes below.
- E9: `component.go` worktree and HEAD SHA-256 both equal
  `1a6085c8f8a873b9f4cf5259ca899c1816a89d6b319bb56078b7ea6c13a00415`.
- E10: active delta/current projection, gate 6 below, and final diff validation.

## Exact gate evidence

1. Approval pause and same-CallID terminal re-dispatch:

   ```text
   go test -v ./processor/agentic-tools \
     -run '^TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult$' \
     -count=1 -timeout=30s
   ```

   PASS with the exact named test shown, 0.926 s wall time; Go reported 0.249 s. The test proves zero execution and no
   COMPLETED value for the initial phase-distinct pause, one approved execution, one durable terminal value, and replay
   without executor re-invocation.

2. Focused package and selected outcome race evidence:

   ```text
   go test -race ./processor/agentic-tools -count=1 -timeout=120s
   go test -v -race ./processor/agentic-tools \
     -run '^(TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult|TestHandleToolCallPublishFailureReplaysWithoutExecutor|TestHandleToolCallPermanentDispositionTable|TestPersistCompletedOutcomeDispositionTable)$' \
     -count=1 -timeout=60s
   ```

   Both PASS. The package gate took 5.673 s wall time; Go reported 1.634 s. The selected outcome gate took 2.026 s;
   Go reported 1.321 s and showed every named test and disposition subtest passing.

3. Repeated causal real-NATS proof:

   ```text
   go test -v -race -tags=integration ./processor/agentic-tools \
     -run '^TestIntegration_MultipleToolCallsProduceAllResults$' \
     -count=20 -failfast -timeout=90s
   ```

   PASS on the first attempt in 4.453 s wall time; Go reported 2.546 s. Output showed 20 named starts and 20 passes.
   Each iteration admitted three calls and completed only after receiving the exact three IDs and contents; the test's
   guards observed no missing, duplicate, unexpected, or error-bearing result.

4. Historical timing evidence is retained, not retried into success. The accepted inventory records the former test's
   811.816917 ms failure under contention and ten exact runs taking 6.438 s total (about 0.64 s each), demonstrating
   that the former 800 ms assertion both failed on host overhead and accepted serial-compatible execution.

5. Named policy guard:

   ```text
   go test -v ./test/testinfra \
     -run '^TestInfrastructurePolicyGuard$' \
     -count=1 -timeout=30s
   ```

   PASS with the selected test shown in 2.421 s wall time; Go reported 2.209 s. The post-omission-restoration rerun also
   passed in 0.961 s wall time; Go reported 0.937 s.

6. Contract and OpenSpec gates:

   ```text
   go test ./test/contract/... -count=1 -timeout=120s
   openspec validate gh1063-correct-tool-wire-terminal-scope --strict --no-interactive
   openspec validate --all --strict --no-interactive
   ```

   PASS. Contracts took 7.504 s wall time; Go reported 3.970 s. Focused strict validation took 1.101 s. All-artifact
   strict validation took 1.280 s and reported 57 passed, 0 failed. The final post-ledger strict reruns also passed:
   focused in 0.129 s and all-artifact in 0.098 s with 57 passed, 0 failed.

7. Formatting, diff, and production inspection:

   ```text
   gofmt -d processor/agentic-tools/tools_integration_test.go
   git diff --check
   git diff --quiet -- processor/agentic-tools/component.go
   ```

   PASS with no output. The test is formatted, the tracked diff is whitespace-clean, and production `component.go`
   has no worktree delta. Untracked active-change artifacts were also checked with `git diff --no-index --check` and
   produced no whitespace warning.

## Forced omissions and restoration

The shared test and tuning files were backed up before each isolated mutation. Their pre-mutation SHA-256 values were
`c1ffd7fedd8cb777c1ff44ea4fbcc79bd004040c13d04b1bf5e6b392415f91ac` and
`799e850a179fd61054d58e5920e2cf3ae2aeaae8694ff1ef336b6de7bb4c8ba1`, respectively.

- Omitting publication of `call_multiple_2` made the selected real-NATS test fail on its first attempt after 5.02 s
  with `missing call_ids=[call_multiple_2]: context deadline exceeded` (7.526 s command wall time).
- Restoring `time.Sleep(100 * time.Millisecond)` after deleting its baseline entry made the exact named policy guard
  fail in 0.984 s with the full new `integration-time-sleep` violation identity for
  `TestIntegration_MultipleToolCallsProduceAllResults`.
- Restoring the outward sentence ``MaxAckPending=3 allows parallel tools.`` was found by the exact conformance search
  `rg -n 'MaxAckPending=3.*allows parallel tools' ...` at the injected tuning-guide line, so the prohibited claim was
  rejected before evidence could pass.

Both files were restored from their backups. Post-restoration SHA-256 values exactly matched the pre-mutation values;
the policy guard returned GREEN and the prohibited-claim search returned no match.

No deviation is recorded. Independent SemStreams reviewer approval remains required before archive or integration.

Landing: PR #1068 (`dc25bcc0`, 2026-08-24). Archived in PR #1071; spec sync: agentic-tools requirement already
identical in baseline.
