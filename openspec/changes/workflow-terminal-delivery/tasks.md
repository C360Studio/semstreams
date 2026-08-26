# Tasks: workflow-terminal-delivery (#1094)

Conventions bound by this file: every test command spells `-race`, `-count=1`, `-run '^…$'`, and
`-tags=integration` where the test is tagged; every response decoded in a test is decoded into a fresh value;
each forced omission is applied to a committed GREEN tree and restored by `cp` from a checksummed copy; the PR
body is a published layer and states `implemented-by: <model>`.

## 0. Accepted records

- [ ] 0.1 Record the `INVENTORY PASS` and owner-accepted design body SHA-256 values in `design.md`.
- [ ] 0.2 Record each owner ruling on design §II.9 items 1–11 in the `design.md` table; implement the recommended
  default for any item ruled "as recommended".
- [ ] 0.3 Record the `INVENTORY PASS WITH DIVERGENCES` review and its seven corrections (design §I.6) in
  `conformance.md`.

## 1. Claim

- [ ] 1.1 Create the worktree `git worktree add ../semstreams-wt/claude/gh1094-workflow-terminal-delivery -b
  claude/gh1094-workflow-terminal-delivery origin/main`; commit this change directory as the first commit; push;
  open a draft PR whose body carries `Closes #1094` and `implemented-by: <model>`.

## 2. Slice A — typed decision on the completion event (agentic, agentic-loop, agentic-tools)

- [ ] 2.1 RED: add `TestLoopCompletedEventDecisionRoundTrip` (agentic) decoding a marshalled event with
  `decision` through the production registry into a fresh `message.BaseMessage`; add
  `TestIsUserFacingDecideActionTable` (agentic) covering `respond_direct`, `ask_user`, `autoresearch`,
  `research`, `needs_clarification`, `""`, and case variants; run
  `go test -race -count=1 ./agentic -run '^(TestLoopCompletedEventDecisionRoundTrip|TestIsUserFacingDecideActionTable)$'`
  and record the compile/assert output verbatim in `conformance.md` (this is the RED-capture task).
- [ ] 2.2 GREEN: add `agentic.CoordinatorDecision`, `agentic.DecideToolName`, `agentic.DecideActionRespondDirect`,
  `agentic.DecideActionAskUser`, `agentic.IsUserFacingDecideAction`; add `Decision *CoordinatorDecision
  \`json:"decision,omitempty"\`` to `LoopCompletedEvent`; re-run 2.1's command to GREEN.
- [ ] 2.3 Add `TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal` and
  `TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal` (agentic-loop) driving a `StopLoop` tool result
  whose tracked tool name is `decide` (metadata `action`/`reason` present) and one whose name is `submit_work`;
  assert the published completion envelope decodes into a fresh `LoopCompletedEvent` with `Decision` set / nil.
  Command: `go test -race -count=1 ./processor/agentic-loop -run '^(TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal|TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal)$'`.
- [ ] 2.4 Implement 2.3: thread the terminal tool result (name via `GetToolName`, metadata) into
  `handleCompleteResponse`; populate `completion.Decision`; replace the `"decide"` literals at `handlers.go:1921`
  and `:1935` and `agentictools.DecideToolName` with `agentic.DecideToolName`; copy `Decision` onto
  `agentterminal.Event`.
- [ ] 2.5 Run `task schema:generate`; commit the regenerated `schemas/agentic-loop.v1.json` with the code; record
  `git diff --stat schemas/ specs/` in `conformance.md`.
- [ ] 2.6 Commit Slice A GREEN before any mutation check; record the commit SHA in `conformance.md`.

## 3. Slice B — selection and origin resolution (agentic-dispatch)

- [ ] 3.1 Add unit tests, each decoding the captured response into a fresh `agentic.UserResponse` value:
  `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing`,
  `TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing`,
  `TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason`,
  `TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin`,
  `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry` (3-deep `ParentLoopID` chain served by
  `loadPersistedLoopFn`, tracker empty),
  `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByRunIDWhenParentAbsent`,
  `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess`,
  `TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess` (terminal record with empty
  `ParentLoopID` and `RunID` and no route → reason `route_less_settled`, no origin walk reason),
  `TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery`,
  `TestResolveOriginRouteBoundsHopsAndDetectsCycles`,
  `TestResolveOriginRouteMissingAncestorSettlesOriginUnresolvable`,
  `TestResolveOriginRouteTransientReadDelaysNak`,
  `TestResolveOriginRouteMalformedAncestorIsPermanent`;
  extend `requireOneTerminalReason`/`terminalReasonSnapshot` and
  `TestSettleAgentTerminalRecordsExactlyOneFixedDisposition` with `handoff_settled` and `origin_unresolvable`.
  Command: `go test -race -count=1 ./processor/agentic-dispatch -run '^(TestSettleAgentTerminal.*|TestResolveOriginRoute.*)$'`.
- [ ] 3.2 Implement `resolveOriginRoute` over `loadPersistedLoop` (next = `ParentLoopID`, else `RunID` ≠ self;
  32 hops; visited set; walk end with no route → `route_less_settled`; absent key, cycle, or bound →
  `origin_unresolvable` with the Warn `origin ancestor <loopID> not observable in AGENT_LOOPS (expired or never
  persisted)`), the decision-driven selection in `settleAgentTerminal`, the `prompt` projection for `ask_user`,
  `Content = Decision.Reason` for reply decisions, and the two new bounded reasons; re-run 3.1 to GREEN.
- [ ] 3.3 Add `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` (`//go:build
  integration`): write root (routed, `http`/`origin-1`), intermediate, and terminal records to `AGENT_LOOPS` with
  `ParentLoopID` links; empty tracker; settle a `respond_direct` completion twice; assert `USER_TERMINAL` contains
  exactly one message on `user.response.http.origin-1` whose `Nats-Msg-Id` is `terminal-user-response:<source id>`
  and whose payload decodes into a fresh `agentic.UserResponse` with `Type=result`, `Content=<reason>`,
  `InReplyTo=<terminal loop id>`. Command:
  `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart$'`.
- [ ] 3.4 Add `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort` (`//go:build integration`):
  bind dispatch's `agent_loops` input port to bucket `AGENT_LOOPS_ALT`, write a routed loop record there only,
  settle its completion, and assert the response decodes into a fresh `agentic.UserResponse` on the record's route.
  Command:
  `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort$'`.
- [ ] 3.5 Implement the declared `agent_loops` `KVReadPort` in `config.go`; resolve the bucket from the port in
  `loadPersistedLoop` and the `/activity` reader; delete the constant at `http_activity.go:20`; run
  `task schema:generate` and commit `schemas/agentic-dispatch.v1.json` with it; re-run 3.4 to GREEN.
- [ ] 3.6 Commit Slice B GREEN before any mutation check; record the commit SHA in `conformance.md`.

## 4. Slice C — guards, docs, spec truth

- [ ] 4.1 Add `TestAction_PublishAgent_CarriesNoChannelFields` (rule) asserting the published `TaskMessage`
  decodes into a fresh value with empty `ChannelType`, `ChannelID`, `UserID` for a loop-entity-triggered spawn.
  Command: `go test -race -count=1 ./processor/rule -run '^TestAction_PublishAgent_CarriesNoChannelFields$'`.
- [ ] 4.2 Update `docs/operations/38-agent-terminal-settlement.md`, `processor/agentic-dispatch/README.md`
  (settlement paragraph and reason table), and `docs/concepts/25-phased-agentic-chains.md` (one sentence naming
  the reserved reply actions); add the release-note paragraph naming the routed-handoff behaviour change.
- [ ] 4.3 If owner item 1 is ruled "reserved names", set `docs/adr/101-coordinator-reply-vocabulary-and-workflow-terminal-delivery.md`
  to status Accepted; otherwise delete the draft and record the ruling in `design.md`.
- [ ] 4.4 File the e2e coverage-gap issue "no e2e drives a rule-spawned chain's user-facing terminal" and record
  its number in `conformance.md`.
- [ ] 4.5 Run `openspec validate workflow-terminal-delivery --strict --no-interactive` and record the output.

## 5. Forced omissions (after the GREEN commits of 2.6 and 3.6)

- [ ] 5.1 Checksum `processor/agentic-loop/handlers.go`, `agentic/tools.go` (or wherever the classifier lands),
  `processor/agentic-dispatch/terminal_settlement.go`, and `processor/agentic-dispatch/http_activity.go` with
  `shasum -a 256`; keep `cp` copies in the scratchpad.
- [ ] 5.2 Omission A (carrier): remove the `completion.Decision` assignment; run 2.3's and 3.1's commands; record
  the assertion output verbatim (the named tests must not pass); restore by `cp`; re-checksum equal.
- [ ] 5.3 Omission B (selector): make `IsUserFacingDecideAction` return `true`; run 3.1's command; record the
  assertion output for `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing` (it must not pass); restore; re-checksum.
- [ ] 5.4 Omission C (mapper): make `resolveOriginRoute` return an empty route; run 3.3's command; record the
  assertion output (it must not pass); restore; re-checksum.
- [ ] 5.5 Omission D (carrier): resolve the bucket from a hardcoded `"AGENT_LOOPS"` again; run 3.4's command; record
  the assertion output (it must not pass); restore; re-checksum.

## 6. Gates (run what CI runs, both suites)

- [ ] 6.1 `task lint`.
- [ ] 6.2 `go test -race -count=1 ./...`.
- [ ] 6.3 `go test -race -count=1 -p 2 -tags=integration ./processor/agentic-dispatch/... ./processor/agentic-loop/... ./agentic/...`.
- [ ] 6.4 `task schema:generate && git diff --exit-code schemas/ specs/`.
- [ ] 6.5 `go test -count=1 ./test/contract/...`.
- [ ] 6.6 `task e2e:agentic` (touches the terminal delivery wire); record duration and result.

## 7. Land (AGENTS.md order)

- [ ] 7.1 Implementation review by `semstreams-reviewer`; record the verdict and every finding's disposition in
  `conformance.md`.
- [ ] 7.2 Owner-run cross-agent round where the owner asks for it; fixes and re-review recorded.
- [ ] 7.3 Archive: `openspec archive workflow-terminal-delivery` plus spec sync as the final content commit.
- [ ] 7.4 Narrow reviewer check of the archive/spec sync recorded in `conformance.md`.
- [ ] 7.5 Undraft the PR; confirm the body still carries `Closes #1094` and `implemented-by: <model>`.
