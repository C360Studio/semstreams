# Conformance: workflow-terminal-delivery (#1094)

Status: skeleton. No production, merge-readiness, or tag-readiness claim is made. Every row is filled from a
command run on the claimed branch; a row without recorded output is not evidence.

Accepted authority:

- Inventory checkpoint: `INVENTORY PASS` SHA-256 `<pending>`.
- Owner-accepted design: SHA-256 `<pending>`.
- Owner rulings on design §II.9 items 1–9: recorded in `design.md`.

| Acceptance criterion (#1094) | Evidence | Status |
|---|---|---|
| A root handoff (`research`/`autoresearch`) is not emitted as the final user result | E1: `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing`, `…OnRouteLessLoopPublishesNothing` | PENDING |
| Reply correlation survives a rule-spawned multi-loop workflow | E2: `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry`, `…ByRunIDWhenParentAbsent` | PENDING |
| Reply correlation survives process restart/recovery | E3: `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` (empty tracker; AGENT_LOOPS only) | PENDING |
| The terminal `respond_direct` / `ask_user` is published as typed `agentic.user_response/v1` to the originating channel, one identity | E4: E3's single-message assertion after two deliveries; `TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery` | PENDING |
| Internal phase completions are not published to the user channel | E5: `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess` | PENDING |
| Direct response | E6: `TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason` | PENDING |
| Clarification | E7: `TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin` | PENDING |
| Redelivery | E4 | PENDING |
| Restart-safe settlement | E3 | PENDING |
| No flat writer, compatibility subject, alias, bridge, or product-local payload | E8: `grep -rn "user.response" processor/rule configs/` output; existing `user-response-subject-ownership` guards; diff inspection | PENDING |
| Loop carries the typed decision only for a `decide` terminal | E9: `TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal`, `…LeavesDecisionNilForNonDecideTerminal`, `TestLoopCompletedEventDecisionRoundTrip` | PENDING |
| One classifier, one tool-name home | E10: `TestIsUserFacingDecideActionTable`; `grep -rn '"decide"' --include='*.go' . \| grep -v _test` shows one definition | PENDING |
| Bounded telemetry | E11: `TestSettleAgentTerminalRecordsExactlyOneFixedDisposition` with `handoff_settled`, `origin_unresolvable` | PENDING |
| `publish_agent` unchanged | E12: `TestAction_PublishAgent_CarriesNoChannelFields`; `git diff --stat processor/rule/actions.go` empty | PENDING |
| Forced omissions | E13: three RED captures with restoration checksums (tasks 5.2–5.4) | PENDING |
| Schema no-drift | E14: task 6.4 output | PENDING |
| e2e | E15: task 6.6 output | PENDING |
| Strict validation | E16: task 4.5 output | PENDING |

## Exact gate evidence

(filled per task; commands are the ones spelled in `tasks.md`)

## Forced omissions

(one block per omission: file, checksum before, RED output, `cp` restore, checksum after)

## Review record

(reviewer verdicts and dispositions; archive/spec-sync check)
