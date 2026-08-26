# Conformance: workflow-terminal-delivery (#1094)

Status: skeleton. No production, merge-readiness, or tag-readiness claim is made. Every row is filled from a
command run on the claimed branch; a row without recorded output is not evidence.

Accepted authority:

- Inventory checkpoint: `INVENTORY PASS WITH DIVERGENCES` on revision 1 (PR #1098, `01b0f37f`). Its seven
  corrections are enumerated in design §I.6 of `docs/proposals/gh1094-workflow-terminal-delivery-design.md`
  (revision 2, `4995b831`): (1) `respond_direct` has 3 test-fixture hits, not 0; (2) the undeclared dispatch
  `agent_loops` port is a predicted-name seam, promoted from hygiene to R8; (3) the dedup window is the USER
  `duplicates` declaration clamped to MaxAge, not a bare 2m default; (4) an absent ancestor key also covers a
  best-effort `persistLoopState` Put that never succeeded; (5) line-drift corrections; (6) the route-less-root and
  severed-hop walk ends are named and dispositioned; (7) four owner facts added (two-plane walker asymmetry,
  completion plumbing is new, synthesized decisions never populate `Decision`, `ask_user` on the cancelled lane).
- Implemented revision body SHA-256, MEASURED on this branch (`awk 'f{print} /^## Complete handoff body$/{f=1}'
  docs/proposals/gh1094-workflow-terminal-delivery-design.md | shasum -a 256`):
  `27d44cb16e708888e15a90ac67c930ba1742932f5df427930564f21a264d8018` — identical to the value recorded at the
  document head. This is a measurement of WHICH bytes were implemented, NOT an owner acceptance signature; owner
  acceptance of revision 3 as a whole is PENDING and is not self-certifiable here.
- Owner ruling 2026-08-26 (owner-run Codex round on PR #1098, recorded on the issue): items 1–7 and 9–10 accepted as
  recommended; 8 accepted conditionally (C2); 11 — AGENT_LOOPS plane accepted, traversal corrected (C1, R4′);
  binding corrections C1–C4 folded in revision 3. Recorded per item in `design.md`.

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
| Route-less root / severed chain settles `route_less_settled` | E17: `TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess` | PENDING |
| Bucket name observed from the declared `agent_loops` port | E18: `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort`; `grep -n agentLoopsBucket processor/agentic-dispatch/*.go` empty | PENDING |
| C1: missing parent key falls back to `RunID`; typed-first order | E19: `TestSettleAgentTerminalMissingParentFallsBackToRunID` (three subtests); omission E (task 5.6) | PENDING |
| C2: `origin_unresolvable` only after parent chain AND run anchor exhausted; Warn names both | E20: `TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted` | PENDING |
| C3: decision stamped through the tracked-name → `ToolResult.Name` chain | E21: `TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent`; omission F (task 5.7) | PENDING |
| C4: present `Decision` with empty `Action`/`Reason` fails validation; unknown non-empty is a valid handoff | E22: `TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason`; omission G (task 5.8) | PENDING |

## RED captures (task 2.1, task 2.3)

### 2.1 — `agentic` (compile RED: the type, field, and constants did not exist)

```
$ go test -race -count=1 ./agentic -run '^(TestLoopCompletedEventDecisionRoundTrip|TestIsUserFacingDecideActionTable|TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason)$'
# github.com/c360studio/semstreams/agentic_test [github.com/c360studio/semstreams/agentic.test]
agentic/coordinator_decision_test.go:39:3: unknown field Decision in struct literal of type "github.com/c360studio/semstreams/agentic".LoopCompletedEvent
agentic/coordinator_decision_test.go:39:22: undefined: agentic.CoordinatorDecision
agentic/coordinator_decision_test.go:40:20: undefined: agentic.DecideActionRespondDirect
agentic/coordinator_decision_test.go:52:24: got.Decision undefined (type *"github.com/c360studio/semstreams/agentic".LoopCompletedEvent has no field or method Decision)
agentic/coordinator_decision_test.go:53:26: undefined: agentic.DecideActionRespondDirect
agentic/coordinator_decision_test.go:53:57: got.Decision undefined (type *"github.com/c360studio/semstreams/agentic".LoopCompletedEvent has no field or method Decision)
agentic/coordinator_decision_test.go:54:52: got.Decision undefined (type *"github.com/c360studio/semstreams/agentic".LoopCompletedEvent has no field or method Decision)
agentic/coordinator_decision_test.go:75:25: bareGot.Decision undefined (type *"github.com/c360studio/semstreams/agentic".LoopCompletedEvent has no field or method Decision)
agentic/coordinator_decision_test.go:86:20: undefined: agentic.DecideActionRespondDirect
agentic/coordinator_decision_test.go:87:20: undefined: agentic.DecideActionAskUser
agentic/coordinator_decision_test.go:87:20: too many errors
FAIL	github.com/c360studio/semstreams/agentic [build failed]
FAIL
```

GREEN after 2.2: `ok  	github.com/c360studio/semstreams/agentic	1.367s`.

### 2.3 — `processor/agentic-loop` (assertion RED: the carrier was not populated)

```
$ go test -race -count=1 ./processor/agentic-loop -run '^(TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal|TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent|TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal)$'
--- FAIL: TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal (0.00s)
    coordinator_decision_test.go:89:
        	Error Trace:	.../processor/agentic-loop/coordinator_decision_test.go:89
        	Error:      	Expected value not to be nil.
        	Test:       	TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal
        	Messages:   	decide terminal must carry a typed decision
--- FAIL: TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent (0.00s)
    coordinator_decision_test.go:118:
        	Error Trace:	.../processor/agentic-loop/coordinator_decision_test.go:118
        	Error:      	Expected value not to be nil.
        	Test:       	TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent
        	Messages:   	tracked-name loss must not demote a decide terminal
FAIL
FAIL	github.com/c360studio/semstreams/processor/agentic-loop	0.337s
FAIL
```

`TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal` passed at RED and stays green after 2.4 — its
discriminating power is proven by forced omission A, not by this run.

GREEN after 2.4: `ok  	github.com/c360studio/semstreams/processor/agentic-loop	1.367s`.

### 3.1 — `processor/agentic-dispatch` (assertion RED: no selector, no resolver)

Full run in the branch history; the FAIL headers and assertion lines verbatim:

```
$ go test -race -count=1 ./processor/agentic-dispatch -run '^(TestSettleAgentTerminal.*|TestResolveOriginRoute.*)$'
--- FAIL: TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing (0.00s)
        	Error:      	Should be zero, but was 1
        	Messages:   	a routed handoff decision must publish nothing
--- FAIL: TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing (0.00s)
        	Error:      	Not equal:
--- FAIL: TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason (0.00s)
        	Error:      	Not equal:
        	Messages:   	content is the decision reason, not the decision JSON
--- FAIL: TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin (0.00s)
        	Error:      	Not equal:
--- FAIL: TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry (0.00s)
        	Error:      	Not equal:
--- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID (0.01s)
    --- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID/parent_key_absent (0.00s)
            	Error:      	Not equal:
            	Messages:   	an absent parent key must not settle while a durable RunID is in hand
    --- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID/parent_link_empty (0.00s)
            	Error:      	Not equal:
            	Messages:   	a severed parent link must not settle while a durable RunID is in hand
    --- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID/typed_lookup_precedes_parent_walk (0.00s)
            	Error:      	Not equal:
    --- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID/intermediate_run_anchor_after_absent_parent (0.00s)
            	Error:      	Not equal:
--- FAIL: TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery (0.00s)
        	Error:      	Not equal:
--- FAIL: TestResolveOriginRouteBoundsHopsAndDetectsCycles (0.00s)
    --- FAIL: TestResolveOriginRouteBoundsHopsAndDetectsCycles/cycle (0.00s)
            	Error:      	Not equal:
    --- FAIL: TestResolveOriginRouteBoundsHopsAndDetectsCycles/hop_bound (0.00s)
            	Error:      	Not equal:
--- FAIL: TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted (0.00s)
    --- FAIL: TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted/absent_parent_and_absent_run_anchor (0.00s)
            	Error:      	[]string{"terminal-loop"} does not contain "evicted-root"
            	Messages:   	the run anchor must be tried
    --- FAIL: TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted/absent_parent_and_no_run_anchor (0.00s)
            	Error:      	Not equal:
--- FAIL: TestResolveOriginRouteTransientReadDelaysNak (0.00s)
        	Error:      	An error is expected but got nil.
--- FAIL: TestResolveOriginRouteMalformedAncestorIsPermanent (0.00s)
    --- FAIL: TestResolveOriginRouteMalformedAncestorIsPermanent/malformed_record (0.00s)
            	Error:      	An error is expected but got nil.
    --- FAIL: TestResolveOriginRouteMalformedAncestorIsPermanent/partial_route_on_ancestor (0.00s)
            	Error:      	An error is expected but got nil.
--- FAIL: TestSettleAgentTerminalRecordsExactlyOneFixedDisposition (0.01s)
    --- FAIL: TestSettleAgentTerminalRecordsExactlyOneFixedDisposition/handoff (0.00s)
            	Error:      	Not equal:
    --- FAIL: TestSettleAgentTerminalRecordsExactlyOneFixedDisposition/origin_unresolvable (0.00s)
            	Error:      	Not equal:
FAIL
FAIL	github.com/c360studio/semstreams/processor/agentic-dispatch	0.337s
FAIL
```

Two of the named tests — `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess` and
`TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess` — PASSED at RED, because
`route_less_settled` is today's behaviour for both. They are not evidence on their own; forced omissions A and C
are what prove they discriminate.

GREEN after 3.2: `ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	1.428s`.

## Exact gate evidence

(filled per task; commands are the ones spelled in `tasks.md`)

- Task 2.5 `task schema:generate` → `git diff --stat schemas/ specs/` EMPTY (no generated-schema surface exists for
  a payload field; see tasks.md 2.5 for the measurement).
- Task 2.6 Slice A GREEN commit: `704b67ee`.
- Task 3.3 `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart$'`
  → `ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	2.097s`.
- Task 3.4 `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort$'`
  → `ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	2.116s`.
- Task 3.5 `task schema:generate` → `git diff --stat schemas/ specs/` EMPTY (declared port instances are not part
  of the generated component schema).
- E18 `grep -n agentLoopsBucket processor/agentic-dispatch/*.go` → 0 hits (the predicted-name constant is gone).

## Forced omissions

(one block per omission: file, checksum before, RED output, `cp` restore, checksum after)

## Review record

(reviewer verdicts and dispositions; archive/spec-sync check)
