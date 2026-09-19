# Tasks: stable request, call and loop identity

## 1. Land the reviewed checkpoints

- [x] 1.1 Cherry-pick `fd0277c2` — dispatch recovers task identity on redelivery
      (`processor/agentic-dispatch/task_recovery.go`, `http.go`, `component.go`, `agentic/user_types.go`)
- [x] 1.2 Cherry-pick `3d6cab9f` — stable tool execution identity: ExecutionID, CallOrdinal, RequestID on every
      `ToolCall` and `ToolResult` (`processor/agentic-loop/execution_identity.go`, `processor/agentic-tools/outcomes.go`,
      `processor/rule/actions.go`, `schemas/agentic-loop.v1.json`)
- [x] 1.3 Cherry-pick `78d54986` — cancel publishes through synchronous JetStream and requires PubAck before its
      source settles
- [x] 1.4 Cherry-pick `af829616` — agentic-model settles provider work from retained responses; `handleRequest`
      returns a classified `DeliveryDecision`
- [x] 1.5 Drop every `openspec/changes/agentic-loop-restart-safety/` hunk from the picks; the Codex change's
      evidence files stay on the closed branch

## 2. Deterministic RequestID (owner ruling Q4, #1330)

- [x] 2.1 `LoopManager.GenerateRequestID` mints `<loopID>:req:<iteration>:<retry>` from state the manager holds
      (`processor/agentic-loop/state.go`)
- [x] 2.2 Test: the same logical request minted twice is byte-identical; a later iteration is not
      (`TestRequestIDIsDeterministicPerIteration`)
- [x] 2.3 Test: a truncation retry of iteration N is `:N:1`, and forward progress clears the ordinal
      (`TestRequestIDCarriesTheTruncationRetryOrdinal`)
- [x] 2.4 Test: the loop prefix, `ExtractLoopIDFromRequest`, and `agent.response.<requestID>` resolution are
      unchanged (`TestRequestIDKeepsTheLoopPrefixAndSubjectGrammar`)
- [x] 2.5 Mutation evidence: uuid-suffix revert kills 2.2 and 2.3; `Iterations` without the `+1` kills 2.2 and 2.3;
      a constant retry ordinal kills 2.3. Baselines restored by checksum
- [x] 2.6 Migration note records the suffix change (`docs/operations/migration-beta162-to-beta163.md`)

## 3. `Nats-Msg-Id` on every request publish (owner ruling Q5, #1330)

- [x] 3.1 `PublishedMessage` carries `MsgID`; `publishResults` routes through `PublishToStreamWithMsgID`
      (`processor/agentic-loop/handlers.go`, `component.go`)
- [x] 3.2 All three `agent.request` publish sites stamp their RequestID (`handlers.go` task birth, truncation
      retry, tools complete) — the same three sites that mint
- [x] 3.3 Integration test: inside a configured `Duplicates` window the second publish stores one message, while a
      publication with no MsgID still repeats
      (`TestIntegrationStampedRequestIDDeduplicatesInsideTheWindow`)
- [x] 3.4 Integration test: a republished RequestID with no window available is answered from the retained
      response, provider call count one
      (`TestIntegrationRepublishedRequestIDReusesRetainedResponseOutsideAnyWindow`)
- [x] 3.5 Mutation evidence: publishing without the MsgID kills 3.3. Baseline restored by checksum

## 4. Observe the provider-settlement classifications

- [x] 4.1 Test: an undecodable request Terminates and performs no retained lookup
      (`TestUnparseableRequestTerminatesWithoutRetainedLookup`)
- [x] 4.2 Test: unresolvable endpoint publishes its error response before source ACK, with no provider call
      (`TestIntegrationEndpointResolutionFailurePublishesErrorBeforeSourceAck`)
- [x] 4.3 Provider-call failure and post-provider publish failure are covered by the picked
      `TestIntegrationProviderErrorPubAckPrecedesSourceAck` and
      `TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain`

## 5. Gates

- [x] 5.1 `task lint` 0, `task schema:generate` with no drift, `task openspec:validate` 0 (56/56),
      `task spec:properties` 0 (176/176), `go test -race ./processor/agentic-tools/... ./processor/agentic-loop/...` 0
- [x] 5.2 `task check:push` 0 on the round-1 head, zero FAIL lines, 310 `ok`, `[INTEGRATION] tests complete`
- [ ] 5.3 Implementation review resolved; archive as the final content commit

## 6. Review round 1

- [x] 6.1 The ruled correlation gate is observed at the unit tier:
      `TestUncorrelatedToolCallTerminatesBeforeLedgerOrExecutor` drives four shapes — missing execution id,
      missing request id, zero call ordinal, none at all — through `handleToolDelivery`. The outcome store is
      armed to fail every `Get`, so a delivery that reached the ledger would come back Retry rather than
      Terminate: the fixture pins the ORDERING (terminate-before-execute), not the refusal alone. Each case
      asserts Terminate, a permanent error naming the correlation check, zero executor invocations, zero
      publishes and one counted error
- [x] 6.2 The governance verdict subject break is recorded in
      `docs/operations/migration-beta162-to-beta163.md`: the old→new subject table, the `$message.execution_id`
      rule token, the demux site (`processor/agentic-loop/component.go:2327`), the waiter key
      (`governance_dispatcher.go:401`), the fail-closed symptom (`:485` — an enforce-mode upgrade rejects every
      call until the rules are edited) and the `audit` mode escape hatch
- [x] 6.3 Q4 injectivity is proven by a handler-path test, not by reading:
      `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` runs one loop through birth → tool call → tools
      complete → truncation retry and asserts the RequestIDs that reach the wire (`:req:1:0`, `:req:2:0`,
      `:req:2:1`). All three prior tests drove `LoopManager.GenerateRequestID` directly
- [x] 6.4 The specs follow the code, not the brief: a fingerprint conflict Terminates (the Quarantine claim is
      gone, and the difference is material — `natsclient/delivery_settlement.go:407-408` sets `ownerStopNeeded`
      on Quarantine only), and the agentic-governance delta no longer claims an unimplemented quarantine on
      conflicting proposal correlation. It states the four real dispositions and that `ProposalFingerprint` is
      carried, not verified
- [x] 6.5 `ProposalFingerprint` IS read (the rule echo path and the loop-side decode at `component.go:2391`), so
      it is tested rather than deleted: `TestProposalFingerprintIsCarriedAndNotVerified` pins the deterministic
      digest, the verdict round-trip, and that a DISAGREEING fingerprint still Acks — the honest statement of
      "carried, not verified". `docs/operations/17-tool-call-governance.md:324` softened to match
- [x] 6.6 Declared deviations written into `design.md`: no `<iteration>:<retry>` parse helper (an exported parser
      with no present consumer is phantom surface, and L4 owns the durable retry input), the sticky-error-response
      residual, and the `design.md:52` overclaim corrected. `governance_dispatcher.go:165` `EffectiveCallID` is
      documented as test-only
- [x] 6.7 Mutation evidence (`cp` backup + `md5 -q` verified restore, no stash, no checkout;
      `agentic-tools/component.go` baseline `8f70187c…`, `agentic-loop/handlers.go` baseline `abe5c294…`, both
      restored and `git status --porcelain` empty afterwards):

      | mutation | test that dies |
      |---|---|
      | delete the whole `validateToolExecutionCorrelation` block from `handleToolDelivery` | all four `TestUncorrelatedToolCallTerminatesBeforeLedgerOrExecutor` cases at `outcomes_test.go:418` — "an uncorrelated call can never become correlated by redelivery" |
      | replace the tools-complete mint with the literal `loopID + ":req:1:0"` | `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` at `request_identity_mint_test.go:107` — continuation RequestID `:req:1:0`, want `:req:2:0` |

## 7. Review round 2 (APPROVE with residuals)

- [x] 7.1 The spec sentence "agentic-loop decodes it as audit context" is made true rather than reworded: the
      decode at `component.go:2392` had no reader, so `auditDispatcher.HandleVerdict` now carries
      `proposal_fingerprint` as a `slog` attribute on its observed-verdict line
      (`governance_dispatcher.go:342`). The verdict-side field is KEPT — it is the wire token the rule echoes
- [x] 7.2 The verification deferral now names its home the way the retry-ordinal residual names L4's
      `PublishedRequestID`: no layer of this stack verifies the fingerprint. L4 (#1330) carries durable LOOP
      state, not durable per-call proposal state, so absent a new issue claiming it the fingerprint is an audit
      token only — stated in `specs/agentic-governance/spec.md` and at the test's head comment
- [x] 7.3 Migration-note pin drift fixed: the waiter registration is `governance_dispatcher.go:413`
      (`:401` had drifted onto a comment line, and the fingerprint attribute moved it again). Every pin in that
      section was re-derived with `sed -n '<n>p'` on the shipping head — `component.go:2327`
      (`executionID := payload.effectiveExecutionID()`), `governance_dispatcher.go:413`
      (`channels[call.ExecutionID] = d.registerWaiter(...)`), and the fail-closed wait re-pinned from `:485` to
      the timer branch `:491-497`, whose `:497` is the quoted symptom string. A one-line note tells the next
      reader to re-derive rather than trust
- [x] 7.4 The retry-ordinal RESET now has a wire assertion, not only a manager-level one:
      `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` takes one more turn — forward progress after the
      truncation retry mints `:req:3:0`, not `:3:1`
- [x] 7.5 Mutation evidence (`cp` + `md5 -q`; `governance_dispatcher.go` baseline `d8e74bb5…`, `handlers.go`
      baseline `abe5c294…`, both restored, porcelain empty):

      | mutation | test that dies |
      |---|---|
      | drop the `proposal_fingerprint` attribute from the audit verdict line | `TestProposalFingerprintIsCarriedAndNotVerified/audit_mode_reads_the_decoded_fingerprint_onto_its_verdict_line` at `proposal_fingerprint_test.go:137` — expected `sha256:audited-digest`, actual `<nil>` |
      | delete the `StatusToolCall` forward-progress `ResetTruncationRetry` at `handlers.go:1255` | `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` at `request_identity_mint_test.go:170` — post-retry continuation `:req:3:1`, want `:req:3:0` |

## 8. Rebase onto L1 (`20fe8d09`, superseded by `73b54da9`) and review round 3

- [x] 8.1 `git rebase --onto 20fe8d09 0053183d` replayed this branch's own fourteen commits onto the reviewed L1
      head. Old head `87828b06` → `5188b9c9`; `backup/gh1328-stable-identity-pre-rebase-20260919` holds the
      pre-rebase tip. **Seven** files conflicted (the PR body's prose said five over a seven-row table; corrected):
      `governance_dispatcher.go` twice (interface doc + `HandleVerdict` signature; waiter-miss return),
      `component.go` twice (tool-result lookup; `handleToolCallVerdictMessage`), `agentic-tools/component.go`
      (approval-required publish), `agentic-model/component.go` (consumer callback), and
      `docs/operations/migration-beta162-to-beta163.md` (two sections kept in order). Every resolution keeps both
      sides' behaviour; the table is in the PR body
- [x] 8.2 The rebase's real finding, carried in `5188b9c9`: L1 recovered the loop from the structured `call_id`,
      but under execution identity that is an opaque digest carrying no loop, so **every waiter-less verdict would
      have Acked and been lost**. `VerdictPayload.effectiveLoopID` closes it
- [x] 8.3 `effectiveLoopID` reduced to the two sources this tree actually produces: `loop_id` as the approve
      action echoes it (`processor/rule/actions.go:2215-2217`, `configs/agentic.json:293-296`) and the RequestID
      grammar `<loopID>:req:<iteration>:<retry>`, top-level or under `properties` — the publish-action shape every
      canonical reject rule in `docs/operations/17-tool-call-governance.md` uses. A `properties.loop_id` tier and
      a `<loopID>:tool:` call-id tier are deleted: no rule template in this tree or its docs writes the first, and
      `GenerateToolCallID` has no caller, so every call id on the wire is provider-authored and `looptoken.Valid`
      refuses it. A fallback nothing produces is untested code on a settlement path
- [x] 8.4 Deleting those tiers exposed the case they hid. `classifyMissingLoop("")` returns `loopPresenceStale`
      (`loop_presence.go:67-70`), so a verdict carrying neither identity acknowledged exactly like a settled loop.
      `settleVerdictWithoutWaiter` now **Terminates** it as malformed input, with a reason label on the existing
      counter (`unrecoverable_loop_identity` beside `missing_waiter` — no new metric) and a `WarnContext` audit
      line carrying `execution_id`, the only identity such a payload still has, plus a hint naming the two shapes
      a rule may echo
- [x] 8.5 Observers, one per claim: `TestVerdictWithoutWaiterSettlesByRecordNotByWaiterMap` gains the malformed
      case (Terminate, `ErrNoGovernanceWaiter`, +1 on `unrecoverable_loop_identity` and no change to
      `missing_waiter`) and the publish-action case whose loop rides only `properties.request_id`;
      `TestVerdictPayload_EffectiveAccessors` gains `effectiveLoopID`/`effectiveExecutionID` rows for both wire
      shapes and a row asserting the two removed tiers resolve to `""`. Counter assertions are **deltas**, not
      absolutes: `getMetrics` is a `metricsOnce` package singleton shared by the whole test binary
- [x] 8.6 `tool_results_dropped_total` gains its first label observer.
      `TestLateToolResultForSettledLoopIsExpectedDrop` ran with `c.metrics = nil`; it now asserts the
      `stale_execution` delta is 1 and `loop_held_elsewhere` 0, so renaming the constant an operator alerts on
      cannot pass silently. `metrics.go` enumerates both emitted reasons on `recordToolResultDropped`, and the
      migration note records that the governance counter went from one series to one per reason
- [x] 8.7 The last live `stale_callid` citation
      (`docs/proposals/pattern-classification-2026-09/inventory-agentic-loop.md:43`) is retired in place: the pin
      is left unedited because that file is line-pinned at its own base `32aeddf7`, and a note beneath it records
      that the emitted reason is now `stale_execution` and that no alert should be written against the old word.
      The two remaining occurrences are inside `openspec/changes/archive/`, which is frozen history
- [x] 8.7b `docs/operations/17-tool-call-governance.md` caught up to the Q4 grammar it documents: three payload
      examples still showed `:req:request-uuid` (the pre-Q4 UUID suffix) and the `$message.request_id` token row
      described it as "the provider request identity" with no shape. The row now carries
      `<loop_id>:req:<iteration>:<retry>` and tells a rule author *why* to echo it — it is the only place the loop
      survives on a rule that does not echo `loop_id`, and a waiter-less verdict carrying neither is terminated
- [x] 8.8 Migration-note pins re-derived again with `sed -n '<n>p'` on this head — the round-2 values all drifted
      when `effectiveLoopID` gained its doc comment: demux `component.go:2327` → `:2480`, waiter key
      `governance_dispatcher.go:413` → `:465`, fail-closed wait `:491-497` → `:544-549`. This supersedes 7.3
- [x] 8.9 Gates measured on this head, not carried forward: `task openspec:validate` 0 — **55 passed, 0 failed**,
      not the 56 recorded at 5.1 and 7.x, because L0's change archived into live spec; `task spec:properties` 0 —
      **194/194**, not the 176/176 recorded at 5.1, because this branch's own citations and L1's retargeted ones
      are both counted now
- [x] 8.10 Mutation evidence (`cp` backup + `md5 -q` verified restore, no stash, no checkout;
      `governance_dispatcher.go` baseline `ad206c37…`, `component.go` baseline `b6d17327…`, both restored and
      `git status --porcelain` empty afterwards):

      | mutation | test that dies |
      |---|---|
      | `effectiveLoopID` reads only top-level `RequestID` (delete the `Properties["request_id"]` fallback) | `TestVerdictPayload_EffectiveAccessors/nested_shape_(publish_action)` at `governance_dispatcher_test.go:600` — expected the loop token, actual `""`; and `TestVerdictWithoutWaiterSettlesByRecordNotByWaiterMap/the_publish-action_shape_finds_the_loop_under_properties` at `missing_loop_settlement_test.go:354` — expected `0x2` (Retry), actual `0x3` (Terminate) |
      | delete the whole unrecoverable-identity guard from `settleVerdictWithoutWaiter` | `…/a_verdict_with_no_recoverable_loop_identity_terminates_as_malformed` at `missing_loop_settlement_test.go:309` — "An error is expected but got nil", which is the silent Ack this task removed |
      | `recordToolResultDropped("stale_execution")` reverts to `"stale_callid"` | `TestLateToolResultForSettledLoopIsExpectedDrop` at `terminal_release_test.go:446` — `tool_results_dropped_total{reason=stale_execution} delta = 0, want 1` |

- [ ] 8.11 **A second rebase is owed and deliberately held.** L1 was rewritten after this branch replayed onto it:
      `20fe8d09` → `73b54da9`, same commit subjects but not the same content (`git diff --stat 20fe8d09 73b54da9`
      = 21 files, +358/-46 — `agentic/state.go`, `terminal_settlement.go`, `paused_state_removal_test.go` and the
      migration note among them, which is the L0.5 reader-narrowing revert plus `LoopEntity.Validate()`). PR #1335
      therefore reads `CONFLICTING` against its base, and GitHub creates no `pull_request` check runs for a
      conflicting PR, so the last E2E Ladder artifact is the green one on `5188b9c9`. The round-3 fixes are pushed
      green on the `20fe8d09` line as instructed; the `--onto` replay waits for the final L1 head rather than
      chasing a moving one
