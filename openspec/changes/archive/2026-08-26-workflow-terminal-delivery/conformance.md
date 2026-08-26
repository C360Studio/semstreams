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

| Acceptance criterion (#1094) | Evidence (`file:line` at this head) | Status |
|---|---|---|
| A root handoff (`research`/`autoresearch`) is not emitted as the final user result | Selector `processor/agentic-dispatch/terminal_settlement.go:210`; `terminal_origin_test.go:121` (`…HandoffDecisionOnRoutedLoopPublishesNothing`), `:139` (route-less); omission B | MET |
| Reply correlation survives a rule-spawned multi-loop workflow | Resolver `terminal_settlement.go:347`; `terminal_origin_test.go:218` (3-deep parent walk), `:246` (`…MissingParentFallsBackToRunID`, 4 subtests); omission E | MET |
| Reply correlation survives process restart/recovery | `terminal_origin_integration_test.go:63` — empty tracker, records only in `AGENT_LOOPS`, real NATS; omission C | MET |
| The terminal `respond_direct` / `ask_user` is published as typed `agentic.user_response/v1` to the originating channel, one identity | `terminal_origin_integration_test.go:120` (one message after two deliveries) + `:130` (`Nats-Msg-Id`); `terminal_origin_test.go:380` (redelivery identity) | MET — one stable identity per terminal, NOT exactly-once |
| Internal phase completions are not published to the user channel | `terminal_settlement.go:219-224`; `terminal_origin_test.go:337` | MET |
| Direct response | Projection `terminal_settlement.go:122-129`; `terminal_origin_test.go:164` | MET |
| Clarification | `terminal_settlement.go:126-128`; `terminal_origin_test.go:189` | MET |
| Redelivery | `terminal_origin_test.go:380`; identity unchanged at `terminal_settlement.go:125` | MET |
| Restart-safe settlement | `terminal_origin_integration_test.go:63`; the resolver reads only persisted records (`terminal_settlement.go:347-406`) | MET |
| No flat writer, compatibility subject, alias, bridge, or product-local payload | `grep -rn "user.response" processor/rule configs/` → only the existing reservation guard (`processor/rule/user_response_subject_reservation.go:5`) and port declarations; `git diff --stat processor/rule/actions.go` empty | MET |
| Loop carries the typed decision only for a `decide` terminal | `processor/agentic-loop/handlers.go:1982` (`decisionFromTerminalTool`), `:2040`; `processor/agentic-loop/coordinator_decision_test.go:58`, `:123`; `agentic/coordinator_decision_test.go:28` | MET |
| One classifier, one tool-name home | `agentic/tools.go:315` (`IsUserFacingDecideAction`), `:256` (`DecideToolName`); `agentic/coordinator_decision_test.go:79`; `grep -rn '"decide"' --include='*.go' . \| grep -v _test` → one definition (`agentic/tools.go:256`); the remaining hits are `errs.Wrap` operation labels, executor group keys, and an e2e mock fixture | MET |
| Bounded telemetry | `terminal_settlement.go:274`, `:281`; `terminal_settlement_test.go:234` extended with `handoff` and `origin unresolvable` | MET |
| `publish_agent` unchanged | `processor/rule/actions_test.go:3844`; `git diff --stat processor/rule/actions.go` empty | MET |
| Forced omissions | A–H below, each with verbatim RED and equal before/after checksums | MET, with two measured deviations (A, E) reported |
| Schema no-drift | Gate 6.4 — `task schema:generate && git diff --exit-code schemas/ specs/` clean | MET |
| e2e | Gate 6.6 — `task e2e:agentic` GREEN, 45.1s | MET for the unchanged front-door branch; the chain terminal is #1105 |
| Strict validation | Task 4.5 — `Change 'workflow-terminal-delivery' is valid` | MET |
| Route-less root / severed chain settles `route_less_settled` | `terminal_settlement.go:458` (`originExhaustion.settle`); `terminal_origin_test.go:361` | MET |
| Bucket name observed from the declared `agent_loops` port | `processor/agentic-dispatch/config.go:45`, `:50`, `:119`; `component/port_facts.go` `KVReadBucket`; `terminal_origin_integration_test.go:145`; `grep -n agentLoopsBucket processor/agentic-dispatch/*.go` → 0 hits; omissions D and H | MET |
| C1: missing parent key falls back to `RunID`; typed-first order | `terminal_settlement.go:356-377` (typed-first), `:419-437` (retry at an absent parent); `terminal_origin_test.go:246` (4 subtests incl. the load-sequence assertion); omission E | MET |
| C2: `origin_unresolvable` only after parent chain AND run anchor exhausted; Warn names both | `terminal_settlement.go:458-475`; Warn at `:238`; `terminal_origin_test.go:456` (both fixtures assert the log names the absent loop and the anchor) | MET |
| C3: decision stamped through the tracked-name → `ToolResult.Name` chain | `processor/agentic-loop/handlers.go:1964` (`resolveToolName`, shared with `gateForApproval`); `coordinator_decision_test.go:96`; omission F | MET |
| C4: present `Decision` with empty `Action`/`Reason` fails validation; unknown non-empty is a valid handoff | `agentic/events.go:107-114`; `agentic/coordinator_decision_test.go:111` (`empty_action`, `empty_reason`, `unknown_nonempty_action_valid`); omission G | MET |

Status wording: MET means the named evidence exists and was run at this head. It is not a merge-readiness or
review claim; §7 is untouched.

## Recorded ruling: the walk-end shape (owner item 8)

**Ruling applied:** owner item 8 as ruled on 2026-08-26 — `origin_unresolvable` is settled only after the parent
chain AND every encountered run anchor are exhausted, and it stays **distinct from** `route_less_settled`.

**The shape it decides.** The delta's step 2 previously ended "…is the walk end and settles
`route_less_settled`", which read literally would also cover a walk whose durable `RunID` link resolved to an
ABSENT key before running out of parent links. That reading collapses the two reasons back together in exactly
the case item 8 separated: "there was no origin" (expected; not an alert) versus "the origin could not be
observed" (a retention/persistence alert).

**Implementation (Reading B).** `processor/agentic-dispatch/terminal_settlement.go:458-475`
(`originExhaustion.settle`): a walk that followed every link it had and simply ran out of links settles
`route_less_settled`; a walk in which ANY link resolved to an absent key settles `origin_unresolvable` and its
Warn names both exhaustions.

**Delta amended to match** (`specs/agentic-terminal-events/spec.md`): "…is the walk end; it settles
`route_less_settled` only when no link on the walk resolved to an absent key, otherwise `origin_unresolvable`",
plus the scenario "absent run anchor followed by a linkless walk end is origin-unresolvable".

**Pinned by test, not by prose** — the reviewer measured that mutating `settle()` to Reading A left the suite
green, so the shape now has its own subtest and its own forced omission:
`TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted/absent_run_anchor_then_linkless_end`
(`terminal_origin_test.go`) and omission I below.

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
- Task 3.6 Slice B GREEN commit: `31ef6b55`.
- Task 4.1 `go test -race -count=1 ./processor/rule -run '^TestAction_PublishAgent_CarriesNoChannelFields$'`
  → `--- PASS` / `ok  	github.com/c360studio/semstreams/processor/rule	1.395s`;
  `git diff --stat processor/rule/actions.go` empty.
- Task 4.4 e2e coverage gap filed as **#1105**.
- Task 4.5 `openspec validate workflow-terminal-delivery --strict --no-interactive`
  → `Change 'workflow-terminal-delivery' is valid`.
- Task 3.3 `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart$'`
  → `ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	2.097s`.
- Task 3.4 `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort$'`
  → `ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	2.116s`.
- Task 3.5 `task schema:generate` → `git diff --stat schemas/ specs/` EMPTY (declared port instances are not part
  of the generated component schema).
- E18 `grep -n agentLoopsBucket processor/agentic-dispatch/*.go` → 0 hits (the predicted-name constant is gone).

### §6 gates (run on the branch head that carries this file)

| Gate | Command | Result |
|---|---|---|
| 6.1 | `task lint` | clean — vet, fmt, revive (0 warnings), fixed-port guard, `test/natsclient` ok |
| 6.2 | `go test -race -count=1 ./...` | exit 0 — 153 packages ok, 0 FAIL, 0 race reports |
| 6.3 | `go test -race -count=1 -p 2 -tags=integration ./processor/agentic-dispatch/... ./processor/agentic-loop/... ./agentic/...` | exit 0 after fixing this change's own break in `startProductionTerminalDispatch` (see tasks 6.3) |
| 6.3b | `go test -race -count=1 -tags=integration ./internal/portgrammarcontrol/` | `ok 6.723s` |
| 6.3c | `go vet -tags=integration ./...` | clean |
| 6.4 | `task schema:generate && git diff --exit-code schemas/ specs/` | no drift |
| 6.5 | `go test -count=1 ./test/contract/...` | `ok 2.673s` |
| 6.6 | `task e2e:agentic` | GREEN — `Scenario completed successfully duration=45.11225975s`, wallclock `1:16.13` |
| build | `task build`; `GOOS=linux GOARCH=amd64 go build ./cmd/semstreams` | both OK |
| 4.5 | `openspec validate workflow-terminal-delivery --strict --no-interactive` | `Change 'workflow-terminal-delivery' is valid` |

## Forced omissions

Applied to the committed GREEN tree at `53177dfd`, one at a time, each restored by `cp` from a checksummed copy
before the next. `shasum -a 256` before == after for all five mutated files:

```
12c2257bc27b5fa1401e9fba699e5390cfcd5da5bc219bdebbc76dc4687f934b  processor/agentic-loop/handlers.go
3ccd1e2dddea00ea549b1b4094c961131e04f0be404062949109a232cf2afe8e  agentic/tools.go
5c55d6c62d7370c982c8f2aba621133caf551782b8b1dbdde258a9b1d551c893  agentic/events.go
4c5b802f56079b50c3c57f509032fa662cd60db8efb8697d039abd17874c7fd9  processor/agentic-dispatch/terminal_settlement.go
d162ec0ec54898ef17634c9d449b4dd1c9ad547bbb3c1fcbbf53280999760d91  processor/agentic-dispatch/config.go
```

`git status --porcelain` after the last restore: empty.

### A — carrier: `completion.Decision` assignment removed (`handlers.go:2040`)

```
--- FAIL: TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal (0.00s)
        	Error:      	Expected value not to be nil.
        	Messages:   	decide terminal must carry a typed decision
--- FAIL: TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent (0.00s)
        	Error:      	Expected value not to be nil.
        	Messages:   	tracked-name loss must not demote a decide terminal
FAIL	github.com/c360studio/semstreams/processor/agentic-loop	0.329s
```

**The design's predicted RED set for A is half wrong, and this is the finding, not a pass:** it also named
`TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry`, which stayed GREEN
(`ok  	github.com/c360studio/semstreams/processor/agentic-dispatch	1.376s`). The dispatch unit tests build the
`LoopCompletedEvent` payload directly, so deleting the loop-side carrier cannot reach them. No in-repo test
crosses the loop → dispatch seam; only an e2e tier would, and none does — the gap filed as #1105.

### B — selector: `IsUserFacingDecideAction` returns `true` for every action (`agentic/tools.go:315`)

```
--- FAIL: TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing (0.00s)
        	Error:      	Should be zero, but was 1
        	Messages:   	a routed handoff decision must publish nothing
--- FAIL: TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing (0.00s)
        	Error:      	Should be zero, but was 1
        	Messages:   	a handoff decision never borrows an origin
--- FAIL: TestSettleAgentTerminalRecordsExactlyOneFixedDisposition/handoff (0.00s)
FAIL	github.com/c360studio/semstreams/processor/agentic-dispatch	0.356s
```

The named test failed; the two extra failures are the same class (every handoff assertion).

### C — mapper: `resolveOriginRoute` skips the walk (returns `route_less_settled`)

```
--- FAIL: TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart (0.27s)
        	Error Trace:	.../processor/agentic-dispatch/terminal_origin_integration_test.go:120
        	Error:      	Not equal:
        	Messages:   	two deliveries must leave one response identity
FAIL	github.com/c360studio/semstreams/processor/agentic-dispatch	1.009s
```

(`go vet` additionally reported `terminal_settlement.go:349:2: unreachable code` for the mutation itself.)

### D — carrier: the loops bucket is predicted again (`return "AGENT_LOOPS", nil`)

```
--- FAIL: TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort (0.30s)
        	Error Trace:	.../processor/agentic-dispatch/terminal_origin_integration_test.go:175
        	Error:      	Received unexpected error:
FAIL	github.com/c360studio/semstreams/processor/agentic-dispatch	1.100s
```

### E — mapper (C1): the `RunID` path is deleted, the parent walk kept

```
--- FAIL: TestSettleAgentTerminalMissingParentFallsBackToRunID (0.01s)
    --- FAIL: .../parent_key_absent (0.00s)
            	Messages:   	an absent parent key must not settle while a durable RunID is in hand
    --- FAIL: .../parent_link_empty (0.00s)
            	Messages:   	a severed parent link must not settle while a durable RunID is in hand
    --- FAIL: .../typed_lookup_precedes_parent_walk (0.00s)
            	Messages:   	the run anchor is read first; the parent key is never read
    --- FAIL: .../intermediate_run_anchor_after_absent_parent (0.00s)
--- FAIL: TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted/absent_parent_and_absent_run_anchor (0.00s)
            	Error:      	[]string{"terminal-loop", "evicted-parent"} does not contain "evicted-root"
            	Messages:   	the run anchor must be tried
FAIL	github.com/c360studio/semstreams/processor/agentic-dispatch	0.386s
```

**Deviation from the design's "and only that test":** a second test also detects the omission, because C2's
"only after the parent chain AND every encountered run anchor are exhausted" is asserted by checking that the
run anchor was READ. That is the C2 requirement doing its job, not an over-broad test; every other test in the
3.1 command stayed green.

### F — selector (C3): the terminal tool is resolved from the tracked name only

```
--- FAIL: TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent (0.00s)
        	Error:      	Expected value not to be nil.
        	Messages:   	tracked-name loss must not demote a decide terminal
FAIL	github.com/c360studio/semstreams/processor/agentic-loop	0.346s
```

Exactly the named test, and only it.

### G — guard (C4): the present-decision completeness check is removed from `Validate`

```
--- FAIL: TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason (0.00s)
    --- FAIL: .../empty_action (0.00s)
            	Error:      	An error is expected but got nil.
    --- FAIL: .../empty_reason (0.00s)
            	Error:      	An error is expected but got nil.
FAIL	github.com/c360studio/semstreams/agentic	0.301s
```

Exactly the named test, and only it.

## Review record

### 7.1 Implementation review — `semstreams-reviewer` (Fable) at `00fbdbd2`: APPROVE WITH CHANGES

No blocking finding; every binding ruling item matched in code. Dispositions, all applied at `f1065152`
(commits `e9b73fec`, `f1065152`; CI + E2E Ladder green):

| # | Severity | Finding | Disposition |
|---|---|---|---|
| H1 | HIGH | The implemented walk-end reading (absent run-anchor key + linkless end → `origin_unresolvable`) is the one item 8 requires, but the delta's step-2 sentence still said `route_less_settled`, and no test pinned the shape (a mutation to the other reading stayed green). | Delta sentence amended (`agentic-terminal-events/spec.md`, step 2: settles `route_less_settled` only when no link on the walk resolved to an absent key, otherwise `origin_unresolvable`); pinning subtest added; omission I (Reading-A mutation fails exactly that subtest) recorded above; ruling row cites item 8. |
| M1 | MEDIUM | Route-less run root that continues to a routed ancestor — untested. | Test added (+ severed-ancestry descendant; omission J). |
| M2 | MEDIUM | Synthesized decision leaves `Decision` nil — untested. | Test added. |
| M3 | MEDIUM | Malformed present decision through settlement — untested. | Test added; because `BaseMessage.MarshalJSON` validates, the framework cannot emit that shape, so the fixture splices foreign bytes. |
| M4 | MEDIUM | The loop's unusable-metadata fail-safe reached tasks but not the delta. | Delta amended. |
| M5 | MEDIUM | Handoff log level Debug in code vs Warn in design. | Raised to INFO in code (`terminal_settlement.go:218`), design.md, and `docs/operations/38-agent-terminal-settlement.md`; owner confirmed at the 7.2 round. |
| — | gate | Exported surface `component.PortFacts.KVReadBucket`: PASS — observation-shaped, mirrors `StoreReadBucket`, kind-gated, no default. Five production sites predict the `kv:` prefix of `ResourceID()`. | FILE → #1110 (consolidation). |
| — | note | Omission A (carrier) pinned link by link; the physical loop→dispatch wire is an E2E gap. | Seam test `terminal_loop_seam_test.go` added (a real `agenticloop.MessageHandler` envelope settled by real dispatch); E2E tier → #1105 (follow-up, not a tag blocker). |

### 7.2 Owner-run cross-agent round — Codex at `f1065152`: APPROVE

No actionable findings. Codex verified: the `AGENT_LOOPS` read is observation of component-owned operational
state, not a competing state owner; dispatch remains the owner of terminal routing/settlement; the INFO handoff
trace at `processor/agentic-dispatch/terminal_settlement.go:218` is emitted once per routed handoff settlement,
identifies loop and action, and adds no unbounded metric label. Focused verification at the reviewed head:
`go test -race -count=1 ./agentic ./processor/agentic-loop ./processor/agentic-dispatch` — PASS;
`go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^(TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart|TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort)$'` — PASS.

### 7.4 Archive / spec-sync check

(pending — recorded by the narrow reviewer check after the archive commit)
