# Tasks: workflow-terminal-delivery (#1094)

Conventions bound by this file: every test command spells `-race`, `-count=1`, `-run '^…$'`, and
`-tags=integration` where the test is tagged; every response decoded in a test is decoded into a fresh value;
each forced omission is applied to a committed GREEN tree and restored by `cp` from a checksummed copy; the PR
body is a published layer and states `implemented-by: <model>`.

## 0. Accepted records

- [x] 0.1 Record the `INVENTORY PASS` and owner-accepted design body SHA-256 values in `design.md`.
  - MEASURED revision-3 body SHA-256 `27d44cb16e708888e15a90ac67c930ba1742932f5df427930564f21a264d8018`
    (`awk 'f{print} /^## Complete handoff body$/{f=1}' docs/proposals/gh1094-workflow-terminal-delivery-design.md |
    shasum -a 256`), equal to the value at the document head. Recorded in `design.md` as a measurement of the
    implemented bytes; owner acceptance of the revision as a whole stays PENDING and is NOT self-certified here.
- [x] 0.2 Record the owner ruling of 2026-08-26 (1–7, 9–10 accepted; 8 conditional; 11 corrected — R4′) and the
  C1–C4 corrections in `conformance.md`; implement the ruled shape.
  - The ruling and C1–C4 are recorded in `conformance.md` (Accepted authority) and per item in `design.md`; the
    implementation rows below carry the `file:line` for each of C1–C4.
- [x] 0.3 Record the `INVENTORY PASS WITH DIVERGENCES` review and its seven corrections (design §I.6) in
  `conformance.md`.
  - All seven enumerated inline in `conformance.md` (Accepted authority), not by pointer.

## 1. Claim

- [ ] 1.1 Create the worktree `git worktree add ../semstreams-wt/claude/gh1094-workflow-terminal-delivery -b
  claude/gh1094-workflow-terminal-delivery origin/main`; commit this change directory as the first commit; push;
  open a draft PR whose body carries `Closes #1094` and `implemented-by: <model>`.

## 2. Slice A — typed decision on the completion event (agentic, agentic-loop, agentic-tools)

- [x] 2.1 RED: add `TestLoopCompletedEventDecisionRoundTrip` (agentic) decoding a marshalled event with
  `decision` through the production registry into a fresh `message.BaseMessage`; add
  `TestIsUserFacingDecideActionTable` (agentic) covering `respond_direct`, `ask_user`, `autoresearch`,
  `research`, `needs_clarification`, `""`, and case variants; add
  `TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason` (agentic; subtests `empty_action`,
  `empty_reason`, `unknown_nonempty_action_valid`); run
  `go test -race -count=1 ./agentic -run '^(TestLoopCompletedEventDecisionRoundTrip|TestIsUserFacingDecideActionTable|TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason)$'`
  and record the compile/assert output verbatim in `conformance.md` (this is the RED-capture task).
  - RED captured (compile failure — the type, field, and constants do not exist); verbatim in `conformance.md`
    under "Forced omissions / RED captures".
- [x] 2.2 GREEN: add `agentic.CoordinatorDecision`, `agentic.DecideToolName`, `agentic.DecideActionRespondDirect`,
  `agentic.DecideActionAskUser`, `agentic.IsUserFacingDecideAction`; add `Decision *CoordinatorDecision
  \`json:"decision,omitempty"\`` to `LoopCompletedEvent`; make `LoopCompletedEvent.Validate` reject a present
  `Decision` with empty `Action` or `Reason`; re-run 2.1's command to GREEN.
  - `agentic/tools.go:250-322`: `DecideToolName` (`:256`), `DecideActionRespondDirect`/`DecideActionAskUser`
    (`:270`, `:275`), `MetadataKeyDecideAction`/`MetadataKeyDecideReason` (`:286`, `:290`),
    `CoordinatorDecision` (`:305`), `IsUserFacingDecideAction` (`:315`); `agentic/events.go:90` (field),
    `:107-114` (Validate guard).
    `go test -race -count=1 ./agentic -run '^(TestLoopCompletedEventDecisionRoundTrip|TestIsUserFacingDecideActionTable|TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason)$'`
    → `ok github.com/c360studio/semstreams/agentic 1.367s`.
- [x] 2.3 Add `TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal`,
  `TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent` (no tracked name for the
  CallID; `toolResult.Name == "decide"`), and `TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal`
  (agentic-loop) driving `StopLoop` tool results (metadata `action`/`reason` present for decide; `submit_work` for the
  nil case); assert the published completion envelope decodes into a fresh `LoopCompletedEvent` with `Decision`
  set / set / nil. Command:
  `go test -race -count=1 ./processor/agentic-loop -run '^(TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal|TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent|TestHandleCompleteResponseLeavesDecisionNilForNonDecideTerminal)$'`.
  - RED (before 2.4): both decide tests failed on the assertion, the nil test passed —
    `--- FAIL: TestHandleCompleteResponseStampsTypedDecisionFromDecideTerminal (0.00s)` /
    `Error: Expected value not to be nil.` / `Messages: decide terminal must carry a typed decision`, and
    `--- FAIL: TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent (0.00s)` /
    `Messages: tracked-name loss must not demote a decide terminal`. Verbatim in `conformance.md`.
    After 2.4: `ok github.com/c360studio/semstreams/processor/agentic-loop 1.367s`.
    Tests: `processor/agentic-loop/coordinator_decision_test.go`.
- [x] 2.4 Implement 2.3: thread the terminal tool result (name via `GetToolName`, metadata) into
  `handleCompleteResponse`; resolve the tool name with the existing chain `GetToolName(callID)` →
  `toolResult.Name` (`handlers.go:2241-2245`); populate `completion.Decision`; replace the `"decide"` literals at
  `handlers.go:1921` and `:1935` and `agentictools.DecideToolName` with `agentic.DecideToolName`; copy `Decision`
  onto `agentterminal.Event`.
  - `processor/agentic-loop/handlers.go:1964-1976` `resolveToolName` — ONE home for the tracked-name →
    `toolResult.Name` chain, now also used by `gateForApproval` (`:2287`) which spelled it inline before;
    `:1982-2005` `decisionFromTerminalTool`; `:2040` stamps `completion.Decision`; the terminal tool result is
    threaded into `handleCompleteResponse` at `:2214` (StopLoop) and explicitly `nil` at `:1194` (model text).
  - Literals replaced: `hasDecideToolCall` and `decideToolAvailable` now compare `agentic.DecideToolName`;
    `processor/agentic-tools/decide.go:76` is now `const DecideToolName = agentic.DecideToolName` (the exported
    agentic-tools spelling is kept as an alias so no adopter symbol is removed); `decide.go:436-437` writes the
    decide result metadata under the shared key constants.
  - `internal/agentterminal/terminal.go:97` carries `Decision`; `:144` copies it on the succeeded lane only.
- [~] 2.5 Run `task schema:generate`; commit the regenerated `schemas/agentic-loop.v1.json` with the code; record
  `git diff --stat schemas/ specs/` in `conformance.md`.
  - RUN, but the premise measures FALSE: `task schema:generate` produced NO diff —
    `git diff --stat schemas/ specs/` is EMPTY. `schemas/agentic-loop.v1.json` is the component CONFIG schema
    emitted by `cmd/openapi-generator`, not a payload schema; `LoopCompletedEvent` appears in neither `schemas/`
    nor `specs/openapi.v3.yaml` (`grep -n LoopCompletedEvent specs/openapi.v3.yaml` → 0 hits). The additive
    `decision` field therefore has NO generated-schema surface to regenerate. Its wire contract is pinned instead
    by the production-decoder round-trip `TestLoopCompletedEventDecisionRoundTrip`. Nothing is committed because
    nothing changed; the no-drift gate is task 6.4.
- [x] 2.6 Commit Slice A GREEN before any mutation check; record the commit SHA in `conformance.md`.
  - `704b67ee` — `feat(agentic): carry a decide terminal's typed decision on the completion event (#1094)`.

## 3. Slice B — selection and origin resolution (agentic-dispatch)

- [x] 3.1 Add unit tests, each decoding the captured response into a fresh `agentic.UserResponse` value:
  `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing`,
  `TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing`,
  `TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason`,
  `TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin`,
  `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry` (3-deep `ParentLoopID` chain served by
  `loadPersistedLoopFn`, tracker empty),
  `TestSettleAgentTerminalMissingParentFallsBackToRunID` (subtests `parent_key_absent`: terminal → absent parent
  key, `terminal.RunID` → observable routed root → delivered; `parent_link_empty`; `typed_lookup_precedes_parent_walk`:
  a sequence-recording `loadPersistedLoopFn` sees `[terminal, root]` and never the parent key),
  `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess`,
  `TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess` (terminal record with empty
  `ParentLoopID` and `RunID` and no route → reason `route_less_settled`, no origin walk reason),
  `TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery`,
  `TestResolveOriginRouteBoundsHopsAndDetectsCycles`,
  `TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted` (absent parent + absent `RunID`
  key; absent parent + no `RunID` on the path; each asserts the Warn names both exhaustions),
  `TestResolveOriginRouteTransientReadDelaysNak`,
  `TestResolveOriginRouteMalformedAncestorIsPermanent`;
  extend `requireOneTerminalReason`/`terminalReasonSnapshot` and
  `TestSettleAgentTerminalRecordsExactlyOneFixedDisposition` with `handoff_settled` and `origin_unresolvable`.
  Command: `go test -race -count=1 ./processor/agentic-dispatch -run '^(TestSettleAgentTerminal.*|TestResolveOriginRoute.*)$'`.
  - All in `processor/agentic-dispatch/terminal_origin_test.go`; the closed reason list gains `handoff_settled`
    and `origin_unresolvable` in both `requireOneTerminalReason` and `terminalReasonSnapshot`, and
    `TestSettleAgentTerminalRecordsExactlyOneFixedDisposition` gains the `handoff` and `origin unresolvable`
    cases (`terminal_settlement_test.go`).
  - RED before 3.2: 12 tests / 21 subtests failed on assertions (verbatim in `conformance.md`), e.g.
    `--- FAIL: TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing` /
    `Error: Should be zero, but was 1` / `Messages: a routed handoff decision must publish nothing`.
    `TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess` and
    `…ReplyDecisionWithRouteLessRootSettlesRouteLess` passed at RED (today's behaviour); their discriminating
    power is proven by the forced omissions, not by this run.
  - GREEN after 3.2: `ok github.com/c360studio/semstreams/processor/agentic-dispatch 1.428s`.
  - ADDITION beyond the named subtests: `TestSettleAgentTerminalMissingParentFallsBackToRunID` carries a FOURTH
    subtest `intermediate_run_anchor_after_absent_parent` — without it the C1 retry *inside* the walk (an
    intermediate record carrying a run anchor the terminal did not) has no test at all; the three named subtests
    only exercise the typed-first lookup. Same test, so omission E's "only that test fails" still holds.
- [x] 3.2 Implement `resolveOriginRoute` over `loadPersistedLoop` in the ruled order: typed-first `RunID` → run root
  (routed → origin; present route-less → walk parents from the root; absent → walk from the terminal); parent walk
  to the nearest routed ancestor; at an absent parent key try the current record's untried `RunID` before settling;
  32 hops, visited set; walk end with no links and no route → `route_less_settled`; parent chain AND run anchor
  exhausted, cycle, or bound → `origin_unresolvable` with the Warn `origin_unresolvable: parent chain ended at absent
  <loopID>; run anchor <RunID> absent | none`. Then the decision-driven selection in `settleAgentTerminal`, the
  `prompt` projection for `ask_user`, `Content = Decision.Reason` for reply decisions, and the two new bounded
  reasons; re-run 3.1 to GREEN.
  - `processor/agentic-dispatch/terminal_settlement.go` — `resolveOriginRoute` (`:347`), `recordRoute` (`:302`),
    `isLoopRecordAbsent` (`:295`), `originExhaustion.settle` (`:458`), `maxOriginHops` (`:265`), and the bounded
    reason constants (`:274`, `:281`, `:287`). Selection is at `:210` (`handoff_settled` first) and `:219-245`
    (origin resolution for a route-less reply decision); the `prompt`/reason projection is at `:122-129`.
- [x] 3.3 Add `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` (`//go:build
  integration`): write root (routed, `http`/`origin-1`), intermediate, and terminal records to `AGENT_LOOPS` with
  `ParentLoopID` links; empty tracker; settle a `respond_direct` completion twice; assert `USER_TERMINAL` contains
  exactly one message on `user.response.http.origin-1` whose `Nats-Msg-Id` is `terminal-user-response:<source id>`
  and whose payload decodes into a fresh `agentic.UserResponse` with `Type=result`, `Content=<reason>`,
  `InReplyTo=<terminal loop id>`. Command:
  `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart$'`
  → `ok github.com/c360studio/semstreams/processor/agentic-dispatch 2.097s`
  (`processor/agentic-dispatch/terminal_origin_integration_test.go:63`).
- [x] 3.4 Add `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort` (`//go:build integration`):
  bind dispatch's `agent_loops` input port to bucket `AGENT_LOOPS_ALT`, write a routed loop record there only,
  settle its completion, and assert the response decodes into a fresh `agentic.UserResponse` on the record's route.
  Command:
  `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort$'`
  → `ok github.com/c360studio/semstreams/processor/agentic-dispatch 2.116s`
  (`terminal_origin_integration_test.go:145`; the port is bound through the production JSON override + merge path,
  `:33-44`).
- [~] 3.5 Implement the declared `agent_loops` `KVReadPort` in `config.go`; resolve the bucket from the port in
  `loadPersistedLoop` and the `/activity` reader; delete the constant at `http_activity.go:20`; run
  `task schema:generate` and commit `schemas/agentic-dispatch.v1.json` with it; re-run 3.4 to GREEN.
  - DONE: port declared at `processor/agentic-dispatch/config.go:119-122`; `agentLoopsPortName` (`:45`) and
    `loopsBucketFromPorts` (`:50`) are the one resolver, reached through `c.loopsBucketName()` (`:72`);
    `loadPersistedLoop` resolves the bucket at `terminal_settlement.go:90` and the `/activity` reader at
    `http_activity.go:197`; the constant at the old `http_activity.go:20` is DELETED —
    `grep -n agentLoopsBucket processor/agentic-dispatch/*.go` → 0 hits. Guard: `config_test.go:24-33` and
    `TestLoopsBucketFromPortsObservesTheDeclaredBinding` (undeclared is an error, never a default).
  - NOT DONE, and cannot be: `task schema:generate` produced NO diff, so there is no
    `schemas/agentic-dispatch.v1.json` change to commit. Declared PORT INSTANCES are not part of the generated
    component schema — it emits the generic port-kind `oneOf`, never the default port list
    (`grep -c "agent.complete" schemas/agentic-dispatch.v1.json` → 0 at the baseline, i.e. no existing port
    instance appears either). Same measurement as task 2.5.
- [x] 3.6 Commit Slice B GREEN before any mutation check; record the commit SHA in `conformance.md`.
  - `31ef6b55` — `feat(agentic-dispatch): deliver a workflow's answer to its origin, never its handoff (#1094)`.
- [x] 3.7 GRAMMAR-COLLISION AUDIT for R8's new port token (added mid-flight; CI-found, not designed).
  The design's file map did not name `internal/portgrammarcontrol`, and CI went red on the Slice B commits —
  runs `33003490848` @ `e7148acc` and `33003744544` @ `53177dfd` — with
  `go test -race -tags=integration -count=1 ./internal/portgrammarcontrol/` failing twice:
  - `TestRuntimePortGrammarCompleteness` (`runtime_completeness_test.go:219`):
    `processor/agentic-dispatch/config.go:58 type-switches a port Config projection`. Only
    `component/port_codec.go` and `component/port_facts.go` may interpret a concrete port config
    (`canonicalPortProjectionOwners`, `runtime_completeness_test.go:45-48`). FIXED by extending the canonical
    owner rather than exempting dispatch: `component/port_facts.go` gains the `kvReadBucket` projection field,
    `kvReadPortFacts` populates it, and `PortFacts.KVReadBucket() (string, bool)` is the accessor —
    the `StoreReadBucket` precedent exactly. `loopsBucketFromPorts` now goes
    `definition.Resolve(DirectionInput)` → `port.Facts()` → `facts.KVReadBucket()`.
    **NOTE for the exported-surface gate: `component.PortFacts.KVReadBucket` is NEW exported framework surface
    that the accepted design never named.** It has one consumer at birth (dispatch's two persisted-loop readers)
    and no default: `ok=false` is an error at the call site, never a fallback to `AGENT_LOOPS`.
    The coordinator's suggestion to mirror how agentic-tools reads its own `agent_loops` bucket does NOT apply —
    MEASURED: agentic-tools declares the port but never reads it; the name reaches the executor from a separate
    config key with its own default (`processor/agentic-tools/config.go:31`, `executors/register.go:60,145`).
    There was no accessor to mirror, which is why one had to be added to the canonical owner.
  - `TestFoundationBTargetCompleteness` (`target_test.go:137`):
    `processor/agentic-dispatch/config.go target Go identities differ: <dynamic>|KVReadPort=1/0` and
    `canonical Go PortDefinition identities=136, want 135`. The census is frozen and amended only through named
    `postFoundationB*` variables. FIXED with `postFoundationBWorkflowTerminalGoIdentityAdditions`
    (`target_test.go`), one file, one identity, no retirement, plus
    `TestPostFoundationBWorkflowTerminalAmendmentIsExact` so the amendment cannot become a general licence: it
    pins the file, the single identity, that dispatch declares exactly one kv-read input bound to `AGENT_LOOPS`
    through the accessor, and that `http_activity.go` never regrows a predicted constant. The count was NOT
    relaxed and the port was NOT deleted (R8 is owner-ruled, item 10).
  - `go test -race -count=1 -tags=integration ./internal/portgrammarcontrol/`
    → `ok github.com/c360studio/semstreams/internal/portgrammarcontrol 6.723s`; `task lint` clean.

## 4. Slice C — guards, docs, spec truth

- [x] 4.1 Add `TestAction_PublishAgent_CarriesNoChannelFields` (rule) asserting the published `TaskMessage`
  decodes into a fresh value with empty `ChannelType`, `ChannelID`, `UserID` for a loop-entity-triggered spawn.
  Command: `go test -race -count=1 ./processor/rule -run '^TestAction_PublishAgent_CarriesNoChannelFields$'`
  → `--- PASS: TestAction_PublishAgent_CarriesNoChannelFields (0.00s)` /
  `ok github.com/c360studio/semstreams/processor/rule 1.395s`. `processor/rule/actions_test.go` (end of file);
  it also pins that the spawn DOES carry ancestry (`ParentLoopID`), which is what origin resolution reads.
  `git diff --stat processor/rule/actions.go` → empty (no production change).
- [x] 4.2 Update `docs/operations/38-agent-terminal-settlement.md`, `processor/agentic-dispatch/README.md`
  (settlement paragraph and reason table), and `docs/concepts/25-phased-agentic-chains.md` (one sentence naming
  the reserved reply actions); add the release-note paragraph naming the routed-handoff behaviour change.
  - `docs/operations/38-agent-terminal-settlement.md`: release-note paragraph naming the behaviour change (top of
    the file), a new "Which terminal is the user's answer" section with the selection table and the origin-
    resolution order, the `route_less_settled` vs `origin_unresolvable` operational distinction, the declared
    `agent_loops` port, and the AGENT_LOOPS horizon added to the bounded-guarantee section.
  - `processor/agentic-dispatch/README.md`: settlement paragraph rewritten for decision-driven selection plus the
    full closed reason table.
  - `docs/concepts/25-phased-agentic-chains.md`: the reserved-vocabulary paragraph after the chain-encapsulation
    passage.
- [x] 4.3 If owner item 1 is ruled "reserved names", set `docs/adr/101-coordinator-reply-vocabulary-and-workflow-terminal-delivery.md`
  to status Accepted; otherwise delete the draft and record the ruling in `design.md`.
  - Owner item 1 was ruled "reserved names", so the ADR stays and its Status is Accepted (2026-08-26). Two stale
    sentences from the draft were corrected in the same pass: the self-contradicting "Status flips to Accepted
    when the change lands", and the Consequences bullet that still put the walker's plane to the owner (ruled:
    the AGENT_LOOPS plane).
- [x] 4.4 File the e2e coverage-gap issue "no e2e drives a rule-spawned chain's user-facing terminal" and record
  its number in `conformance.md`.
  - **#1105** — "e2e: no tier drives a rule-spawned chain's user-facing terminal to user.response"
    (`area:e2e`, `area:agentic`, `type:test`, `class:e2e-gap`). Filing records the gap; it does not discharge
    #1094's guarantee.
- [x] 4.5 Run `openspec validate workflow-terminal-delivery --strict --no-interactive` and record the output.
  - `Change 'workflow-terminal-delivery' is valid` (re-run at the head that carries the §6 gate results).

## 5. Forced omissions (after the GREEN commits of 2.6 and 3.6)

- [x] 5.1 Checksum `processor/agentic-loop/handlers.go`, `agentic/tools.go` (or wherever the classifier lands),
  `agentic/events.go`, `processor/agentic-dispatch/terminal_settlement.go`, and
  `processor/agentic-dispatch/http_activity.go` with `shasum -a 256`; keep `cp` copies in the scratchpad.
  - Done at the GREEN commit `53177dfd`. The fifth file is `processor/agentic-dispatch/config.go`, NOT
    `http_activity.go`: R8 moved the bucket resolution there, and `http_activity.go` now only calls
    `c.loopsBucketName()`. All five sums recorded in `conformance.md`; before == after for every one.
- [~] 5.2 Omission A (carrier): remove the `completion.Decision` assignment; run 2.3's and 3.1's commands; record
  the assertion output verbatim (the named tests must not pass); restore by `cp`; re-checksum equal.
  - APPLIED and RESTORED (checksum equal). 2.3's two decide tests failed as required. But 3.1 stayed GREEN:
    the design named `TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry` as an omission-A
    detector and it is NOT one — the dispatch unit tests construct the completion payload directly, so no
    in-repo test crosses the loop → dispatch seam. Recorded as a measured MISS, not smoothed over; the seam is
    exactly the e2e gap filed as #1105. Verbatim output in `conformance.md`.
- [x] 5.3 Omission B (selector): make `IsUserFacingDecideAction` return `true`; run 3.1's command; record the
  assertion output for `TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing` (it must not pass); restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). The named test failed (`Should be zero, but was 1`), together with
    the route-less handoff test and the `handoff` disposition subtest — the same assertion class.
- [x] 5.4 Omission C (mapper): make `resolveOriginRoute` return an empty route; run 3.3's command; record the
  assertion output (it must not pass); restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart`
    failed at the single-identity assertion (no response was published at all).
- [x] 5.5 Omission D (carrier): resolve the bucket from a hardcoded `"AGENT_LOOPS"` again; run 3.4's command; record
  the assertion output (it must not pass); restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). `TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort`
    failed: reading the predicted bucket errors, so settlement returns instead of publishing.
- [~] 5.6 Omission E (mapper, C1): delete the `RunID` path of the resolver (typed-first lookup and the retry at an
  absent parent), keeping the parent walk; run 3.1's command; record that
  `TestSettleAgentTerminalMissingParentFallsBackToRunID` (all three subtests) does not pass and every other 3.1
  test does; restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). All four subtests of the named test failed. The "every other 3.1 test
    passes" half does NOT hold: `TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted/absent_parent_and_absent_run_anchor`
    also failed, because C2's "only after the parent chain AND every encountered run anchor are exhausted" is
    asserted by checking the run anchor was READ — deleting the `RunID` path removes that read. Reported rather
    than papered over; the extra detector is the C2 requirement, not an over-broad test.
- [x] 5.7 Omission F (selector, C3): resolve the terminal tool from the tracked name only; run 2.3's command; record
  that `TestHandleCompleteResponseStampsDecisionFromToolResultNameWhenTrackedNameAbsent` alone does not pass;
  restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). Exactly that test failed; the other two in the 2.3 command passed.
- [x] 5.8 Omission G (guard, C4): remove the `Decision` check from `LoopCompletedEvent.Validate`; run 2.1's command;
  record that `TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason` alone does not pass;
  restore; re-checksum.
  - APPLIED and RESTORED (checksum equal). Exactly that test failed (`empty_action`, `empty_reason`); the
    round-trip and classifier tests passed.
- [x] 5.9 Omission H (projection, added with task 3.7): delete the kv-read bucket projection in
  `component/port_facts.go`'s `kvReadPortFacts`, so `PortFacts.KVReadBucket` always reports false; run the
  amendment exactness test and 3.4's command; restore; re-checksum.
  - APPLIED and RESTORED (`shasum -a 256 component/port_facts.go` =
    `3c4f0433eacce39c9e3465255d97c2ca87c6df40e83efd6e70768defaf242a1b` before and after).
    `go test -count=1 ./internal/portgrammarcontrol/ -run '^TestPostFoundationBWorkflowTerminalAmendmentIsExact$'`
    → `--- FAIL: TestPostFoundationBWorkflowTerminalAmendmentIsExact (0.00s)` /
    `target_test.go:325: dispatch declares 0 kv-read inputs, want 1`.
    `go test -race -count=1 -tags=integration ./processor/agentic-dispatch -run '^TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort$'`
    → `--- FAIL: … Received unexpected error:` (the bucket cannot be resolved, so nothing is published).
    The accessor is load-bearing on both the grammar side and the delivery side.

## 6. Gates (run what CI runs, both suites)

- [x] 6.1 `task lint`.
  - Clean: `go vet ./...`, `go fmt ./...`, `go tool revive -config revive.toml -formatter friendly ./...` (0
    warnings), the fixed-port guard, and `go test ./test/natsclient/` → `ok`. Working tree unchanged by `go fmt`.
- [x] 6.2 `go test -race -count=1 ./...`.
  - exit 0; 153 packages `ok`, zero `FAIL`, zero race reports.
- [x] 6.3 `go test -race -count=1 -p 2 -tags=integration ./processor/agentic-dispatch/... ./processor/agentic-loop/... ./agentic/...`.
  - FIRST RUN FAILED, and the failure was this change's: `TestIntegrationInvalidTerminalIsTerminated` panicked with
    `interface conversion: component.Portable is component.KVReadPort, not component.JetStreamPort`
    (`terminal_settlement_integration_test.go:331`). `startProductionTerminalDispatch` rebinds every declared input
    to a test stream through an unchecked type assertion, and R8 added the first non-JetStream input. Fixed in the
    helper (comma-ok; the KV port keeps its bucket) — a second grammar-collision the unit suite could not see,
    because the helper is integration-tagged.
  - RERUN exit 0: `ok processor/agentic-dispatch 40.844s`, `ok processor/agentic-loop 28.535s`,
    `ok agentic 1.341s`, `ok agentic/agentrun 9.456s` (+ lessonmatch, prompt, identity, research).
  - Also run: `go test -race -count=1 -tags=integration ./internal/portgrammarcontrol/` → `ok 6.723s`;
    `go vet -tags=integration ./...` → clean.
- [x] 6.4 `task schema:generate && git diff --exit-code schemas/ specs/`.
  - No drift (exit 0, empty diff). Neither the additive `decision` payload field nor the declared `agent_loops`
    port has a generated-schema surface — see tasks 2.5 and 3.5.
- [x] 6.5 `go test -count=1 ./test/contract/...`.
  - `ok github.com/c360studio/semstreams/test/contract 2.673s`.
- [x] 6.6 `task e2e:agentic` (touches the terminal delivery wire); record duration and result.
  - GREEN. `Scenario completed successfully duration=45.11225975s`; total wallclock `1:16.13`. The tier's
    `verify-terminal-response` step (4ms) exercises the unchanged no-decision branch end to end
    (`user.response.e2e.<taskID>` + `terminal-user-response:<source id>`), which is the regression this gate is
    for; it does NOT cover a rule-spawned chain terminal — that gap is #1105.
  - Build gates alongside it: `task build` → `Built bin/semstreams`; CI cross-compile
    `GOOS=linux GOARCH=amd64 go build ./cmd/semstreams` → OK (42 MB binary).

## 7. Land (AGENTS.md order)

- [ ] 7.1 Implementation review by `semstreams-reviewer`; record the verdict and every finding's disposition in
  `conformance.md`.
- [ ] 7.2 Owner-run cross-agent round where the owner asks for it; fixes and re-review recorded.
- [ ] 7.3 Archive: `openspec archive workflow-terminal-delivery` plus spec sync as the final content commit.
- [ ] 7.4 Narrow reviewer check of the archive/spec sync recorded in `conformance.md`.
- [ ] 7.5 Undraft the PR; confirm the body still carries `Closes #1094` and `implemented-by: <model>`.
