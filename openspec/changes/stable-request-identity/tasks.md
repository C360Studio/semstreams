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
      | delete the `StatusToolCall` forward-progress `ResetTruncationRetry` at `handlers.go:1372` | `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` at `request_identity_mint_test.go:170` — post-retry continuation `:req:3:1`, want `:req:3:0` |

## 8. Rebase onto L1 (`20fe8d09` → `fdd645b5`) and review round 3

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
      `stale_execution` delta is 1, so renaming the constant an operator alerts on cannot pass silently. The
      rebase onto L1 (#1327) retired this layer's second reason: a result naming a loop another process holds is
      warned and retried rather than counted, so `metrics.go` documents the one reason that is emitted and why
      the other is not. The migration note records that the governance counter went from one series to one per
      reason
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
- [x] 8.9 Gates measured on the head that ships, not carried forward: `task openspec:validate` 0 — **55 passed,
      0 failed**, not the 56 recorded at 5.1 and 7.x, because L0's change archived into live spec;
      `task spec:properties` 0 — **196/196**, not the 176/176 recorded at 5.1 (this branch's own citations and
      L1's retargeted ones are both counted) and not the 194/194 measured on the interim `20fe8d09` base, because
      the final L1 head adds two more. A count carried across a rebase is not a measurement
- [x] 8.10 Mutation evidence (`cp` backup + `md5 -q` verified restore, no stash, no checkout;
      `governance_dispatcher.go` baseline `ad206c37…`, `component.go` baseline `b6d17327…`, both restored and
      `git status --porcelain` empty afterwards):

      | mutation | test that dies |
      |---|---|
      | `effectiveLoopID` reads only top-level `RequestID` (delete the `Properties["request_id"]` fallback) | `TestVerdictPayload_EffectiveAccessors/nested_shape_(publish_action)` at `governance_dispatcher_test.go:600` — expected the loop token, actual `""`; and `TestVerdictWithoutWaiterSettlesByRecordNotByWaiterMap/the_publish-action_shape_finds_the_loop_under_properties` at `missing_loop_settlement_test.go:354` — expected `0x2` (Retry), actual `0x3` (Terminate) |
      | delete the whole unrecoverable-identity guard from `settleVerdictWithoutWaiter` | `…/a_verdict_with_no_recoverable_loop_identity_terminates_as_malformed` at `missing_loop_settlement_test.go:309` — "An error is expected but got nil", which is the silent Ack this task removed |
      | `recordToolResultDropped("stale_execution")` reverts to `"stale_callid"` | `TestLateToolResultForSettledLoopIsExpectedDrop` at `terminal_release_test.go:446` — `tool_results_dropped_total{reason=stale_execution} delta = 0, want 1` |

- [x] 8.11 **The second replay landed.** L1 moved twice after this branch first replayed onto it: `20fe8d09` →
      `73b54da9` → `fdd645b5` (same commit subjects, different content each time — the L0.5 reader-narrowing
      revert, then `LoopEntity.Validate()` on the dispatch reader with ancestry fixtures carrying
      `MaxIterations: 3`). Round 3 was pushed green on the `20fe8d09` line as instructed rather than chased onto a
      moving head, and the replay ran once the final head was named: `git rebase --onto fdd645b5 20fe8d09`,
      eighteen commits, **zero conflicts**. Old head `8b05a0ef`/`5791e6c4` → `b63df50f`; backup refs
      `backup/gh1328-stable-identity-pre-rebase-20260919` (`87828b06`) and
      `backup/gh1328-stable-identity-round3-20260919` (`5791e6c4`)
- [x] 8.12 The cross-layer watch did **not** fire. L0.5 now runs full `LoopEntity.Validate()` on the same dispatch
      reader this stack uses, so the three tests named as a design-disagreement tripwire were run by name on the
      rebased head rather than inferred from a green suite:
      `TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart` PASS (0.30s) and
      `TestIntegrationPersistedInvalidStateIsPermanent` PASS (0.24s) under `-tags=integration`, and
      `paused_state_removal_test.go`'s single case `TestTransitionLoopRefusesPaused` PASS. A suite-level `ok` would
      not have distinguished "passed" from "not compiled into this run"

## 9. Replay onto main after L1 squashed

- [x] 9.1 L1 (#1327, PR #1334) squash-merged to main as `94cd8e4c` and its change `settle-after-durable-effect`
      archived there, so this branch replayed onto `origin/main` rather than onto L1's branch:
      `git rebase --onto origin/main fdd645b5`, **19 commits**, one conflicted commit (`5b5a1e65`) in three files.
      Old head `7eaff212` → `ecab1a72`; backup ref `refs/backup/gh1328-pre-main-rebase-20260919`. Every edit under
      `openspec/changes/settle-after-durable-effect/` in the replayed range belonged to L1's own commits and
      dropped with them — `git log f4d66934..<old head> --name-only` on that path lists no commit of this change,
      so nothing of this change's needed re-homing from that directory
- [x] 9.2 The one conflict, resolved by the rule "keep L1's classification, re-home this change's identity on top
      of it": `component.go`'s `settleToolResultWithoutLoop` default arm keeps L1's warn-not-counted text and
      gains this change's `execution_id` attribute; `metrics.go`'s `toolResultsDropped` help string keeps L1's two
      sentences with this change's "execution ID" wording; `governance_dispatcher.go` keeps this change's
      two-tier `effectiveLoopID`
- [x] 9.3 L1's invariants verified present on the replayed head rather than assumed: `withCommandEffect` at
      `handleCommand` entry (`component.go:926`), `targetFromTracker` at `:957`/`:962`, the two-conjunct
      Quarantine arm at `:1040`, `noteSignalPublished` immediately after the cancel `PublishToStream`
      (`commands.go:191`), `errCancelledBeforeMutation`, and `persistFailureState` before the stamp (`:1893`).
      Pins re-derived at round 2 with `sed -n`; the four `component.go` ones had drifted by 7-15 lines inside this
      change's own commits
- [x] 9.4 **The replay broke two of L1's tests and the race unit gate caught it.**
      `TestToolResultHandlerFailureSettlesOnTheDurableRecord` (four subtests) and
      `TestToolResultCancellationRetriesOnlyBeforeMutation` (two) all read decision `0x1` where L1 asserts `0x4`
      or `0x2`: L1's fixtures build a `ToolResult` carrying only the provider `CallID`, and this change routes the
      lane on framework execution identity (`component.go:2064`), so every result settled as an expected drop
      without ever reaching the handler — the assertions were not failing on their subject, they were passing over
      a lane that never ran. Repaired in the fixtures, not in the classification: the results now carry both ids
      (the loop's pending-tool set is still keyed by `CallID`, `handlers.go:2437`), and the dispatch-driven
      fixture reads the minted identity back out of the routing map production writes (`dispatchedExecutionID`)
      rather than re-deriving it from `deriveToolExecutionID`'s inputs. All six subtests verified **by name** with
      `-v`, since a package-level `ok` cannot distinguish passed from not-compiled-in
- [x] 9.5 L1's two `design.md` residuals naming this branch are answered in `design.md` § "L1's residuals that
      name this layer", not left for a reader to infer: the cancel-signal PubAck is owned here (the signal is a
      `PublishToStream`, `commands.go:185`) and the requirement text now names it; the post-PubAck submission
      response arm is **not** relaxed from Quarantine to Retry, because recovering task identity removes only one
      of the effects a redelivery repeats — `Track` replaces the whole `LoopInfo` (`loop_tracker.go:144-153`) and
      the two started counters re-fire. L1's call-site comment, whose premise ("a redelivery mints a fresh task
      UUID") this change falsifies, was rewritten in place rather than left to mislead. Main's deferral bullet
      ("extending it to the rest is L2's") is answered with a disposition: the invalid-input lane keeps its
      per-publication response id
- [x] 9.6 `tool_results_dropped_total{reason=loop_held_elsewhere}` **never reached main** and is removed from
      `metrics.go`'s enumeration here. Both of its producers lived on L1's discarded pre-round-1 line
      (`fdd645b5`); `git log --oneline -S loop_held_elsewhere origin/main -- .` returns nothing, and L1's merged
      form settles both held-elsewhere arms as warned-and-retried. So no operator ever ran a build that emitted
      it, nothing was removed from an operator's view, and no migration note is owed — this is a doc comment and a
      test assertion being brought back in line with the producers that exist, not a retired series. The zero-delta
      assertion in `TestLateToolResultForSettledLoopIsExpectedDrop` goes with it: an assertion on a label with no
      producer is a test that cannot fail
- [x] 9.7 Migration-note pin drift from the replay: the demux pin moved `component.go:2480` → `:2618`, re-derived
      with `sed -n`. The waiter key (`governance_dispatcher.go:465`) and the fail-closed enforce wait (`:544-549`)
      were re-read on this head and are unchanged
- [x] 9.8 Mutation evidence for the re-homed counterfactual (`cp` backup + `md5 -q` verified restore, no stash, no
      checkout; `component.go` baseline `7fb1195f…`, `task_recovery.go` baseline `1e442f18…`, both restored and
      `git status --porcelain` empty afterwards). Each mutation names the half of the claim it kills, so "the
      identity is recovered" and "the tracking is not" are separately falsifiable rather than one green:

      | mutation | assertion that dies |
      |---|---|
      | `stableDispatchTaskID` returns `dispatch-` + a fresh UUID (L1's world) | `tasks[0].TaskID == tasks[1].TaskID` — "the redelivery republishes the committed task identity, which downstream deduplicates" |
      | `handleTaskSubmission` discards the recovery (`prepared, found = preparedDispatchTask{}, false`) at the call site | `tasks[0].LoopID == tasks[1].LoopID` — the stable TaskID alone keeps the subject, so only the LoopID assertion falls, which is exactly the half `findRetainedDispatchTask` owns |
      | `Track` and `recordLoopStarted` run only when the task was not recovered | `"pending" == tracker.Get(loopID).State` — expected `"pending"`, actual `"exploring"`: the reset this arm quarantines for is real, and an idempotent re-entry would remove it |

- [x] 9.9 A third L1 test, `TestIntegrationTerminalFailureRecordPrecedesItsPublication`, carried the same
      CallID-only fixture and failed the same way — and it stayed hidden through one whole gate, because the
      integration runner stopped at `agentic-dispatch` before reaching `agentic-loop`. Its first subtest asserts
      Ack, which an expected settled-drop also returns, so only the `COMPLETE_<loopID>` check exposed a lane that
      never ran. `grep -rn "TrackToolCall(" --include='*_test.go'` then confirmed no fourth fixture of that shape
      remains, and all five agentic packages were run under `-tags=integration -race` rather than the one package
      the failure named

- [x] 9.10 Gates measured on the head that ships (§ 8.9's rule applied to this round, since both surviving counts
      were wrong for it): `openspec validate --all --strict` 0 — **56 passed, 0 failed**, not the 55 at § 8.9,
      because this round's MODIFIED block is a new delta item; `openspec validate stable-request-identity
      --strict` 0; `task spec:properties` 0 — **208/208**, not the 196/196 at § 8.9; `task lint` 0;
      `task schema:generate` 0 with `git diff --exit-code schemas/ specs/` 0. **`task check:push` exit 201**, red
      only in the integration phase and only in `natsclient`, twice, on container startup:
      `create container: reaper: context deadline exceeded`, then `resolve required mapped ports: … inspect
      container port snapshot: … context deadline exceeded`. Every earlier phase was green — build, `go vet
      ./...`, `go fmt`, revive, the fixed-port and raw-Request guards, `go vet -tags=integration ./...`,
      `-tags=live_llm`, schema generate with a clean drift check, `./test/contract/...`, `go test -race ./...` —
      and so were all five agentic packages under `-tags=integration -race` (loop 38.160s, dispatch 78.360s,
      tools 59.533s, model 19.877s, governance 2.750s)
- [x] 9.11 The `natsclient` red is not this branch's, and the reason first given for that was false. This branch
      **does** touch that package: `d6fe545d` edits `natsclient/publish_msgid_integration_test.go` (+16/-3). The
      attribution survives on better evidence. Both failures are container *startup* — reaper timeout and mapped
      ports never resolving — which happen before any test body runs, so no edit to a test file can reach them.
      The edited test passes 3/3 in isolation here (2.67s, 0.67s, 0.70s; the first carries container warm-up) and
      its one wall-clock assertion polls a 250ms `Duplicates` window under a 3s `require.Eventually` budget, 12x
      headroom. `natsclient` as a whole passes in isolation on this head (`ok … 78.369s`), Docker reported zero
      containers between runs, and the shape is open #736 ("The integration suite oversubscribes Docker under
      package parallelism"). Not rerun to green: CI
      [35468157590](https://github.com/C360Studio/semstreams/actions/runs/35468157590) is green on all six jobs at
      `41615bdc`, and its Test job runs the same `scripts/run-integration-tests.sh`
- [x] 9.12 `docs/operations/migration-beta162-to-beta163.md` records the one operator-visible metric change this
      round had left undocumented: `tool_results_dropped_total{reason}`'s only value moved `stale_callid` →
      `stale_execution`. The old value shipped from **beta.46** (`913cf209`, 2026-05-06) and is in 116 tags
      through beta.162, so a selector naming it is valid PromQL that reads zero forever with no error — the same
      `class:advertised-absent` shape this change's own § 9.6 names, applied to the one label with an operator
      behind it. No note is owed for `loop_held_elsewhere`: it never reached main (§ 9.6)

## 10. Codex review round 4 (PR #1335 round 1, six findings)

- [x] 10.1 **F1 [P1] — a continuation admitted mid-request reused the outstanding request's identity** (`8734713d`).
      The Q4 grammar is unique only if the loop mints one request per `(iteration, retry)`, and both ordinals move
      when the LOOP advances, so a continuation admitted while a request was outstanding minted the outstanding
      request's name and its `Nats-Msg-Id` — dropped by the duplicate window. `LoopManager.outstandingRequests`
      (written by `TrackRequest`, cleared by `SettleRequest` on a matching request id) answers "waiting on a model
      right now", which `requestToLoop` cannot: its only delete is `releaseLoop`, so it records "published", never
      "outstanding". `attachContinuation` returns `deferred` instead of refusing; `HandleTask` does everything but
      the publish and marks `LoopEntity.PendingContinuation`; the completion path carries the turn through
      `carryDeferredContinuation` → `publishIterationRequest`. Tests:
      `TestContinuationBehindAnOutstandingRequestPublishesNothing`,
      `TestDeferredContinuationIsCarriedByTheCompletionResponse`, `TestDeferredContinuationRidesTheToolCallPath`.
      No STOP was owed: `handleTaskMessage` already discards `persistLoopState`'s error and ACKs, so a deferred
      task settles no earlier than a deduplicated one — strictly later, in fact: the dedup path returns before
      `recordTrajectoryObservations` and `persistLoopState`, the deferred path runs both.
      **Amended at round 2** (§ 11.1-11.3): `publishIterationRequest` was never the one home — the truncation
      retry and the birth request mint too — so the bookkeeping moved to `TrackRequest`; the marker now records
      its carrier instead of clearing at build; and a redelivered carried completion is refused by request
      identity
- [x] 10.2 **F2 [P1] — human approval matched by provider CallID, not execution identity** (`c300c9bc`).
      `ApprovalPendingEvent` and `ApprovalResponse` gain `ExecutionID`/`RequestID` (additive — `task
      api:compat:report` lists both payloads under *Compatible changes*); the gate stamps them, the HTTP path
      echoes them through `LoopTracker.GetPendingApproval`, and the timeout sweeper's synthetic auto-reject carries
      them off the loop's own pending state. `ResolveApprovalIfPending` matches on ExecutionID whenever the pending
      state carries one, with NO fallback to CallID. Tests:
      `TestReplayedApprovalDoesNotAuthoriseALaterCallWithTheSameProviderCallID`,
      `TestApprovalWithoutExecutionIdentityIsRefusedAgainstAGatedCall`,
      `TestApprovalPendingEventCarriesTheGatedExecutionIdentity`,
      `TestExpiredApprovalCandidateCarriesTheGatedExecutionIdentity`. Mutation: matching on CallID only → both
      refusal tests fail. `TestIntegration_ApprovalFlow_Approve` went red on exactly this rule — its fixture
      answered with CallID alone — and now echoes the pending event's execution identity over the wire, the way a
      real approval UI must (`3fbdd21c`). Mutation: `+ "-mutated"` on the pending event's `ExecutionID` → the
      wire-identity assertion and the re-dispatch both fail. **The payload half is additive; the change is not.**
      `(*LoopManager).ResolveApprovalIfPending`'s signature changes on a Tier 1 exported type — one incompatible
      change, deliberate, because the compatible alternative is the CallID-only matcher the ruling banned. It is
      in the migration note beside F4's `HandleVerdict` break; under ADR-106 the Tier 1 count descends to zero
      before RC, so both are named here rather than only the compatible payload additions. `task
      api:compat:report` exits 0 in report mode whatever the count — the number has to be read out of the log
- [x] 10.3 **F3 [P1] — an ambiguous cancel publish failure retried an inferred target** (`2e00d00c`). The recorder
      gains the ATTEMPT (`commands.go:184`, before the publish) beside the publication (`:191`). A command whose
      target was resolved rather than named and whose attempt is unaccounted for returns Fatal → Quarantine. The
      exception is a PROVEN refusal, a fail-closed whitelist of errors the client returns before the bytes leave
      the process: `natsclient.ErrCircuitOpen` (`natsclient/client.go:972-974`), `natsclient.ErrNotConnected`
      (`:976-978`) and the sentinels `nats.Conn.publish` returns before its first write (nats.go v1.52.0 `:4426`,
      `:4434`, `:4438`, `:4445`, `:4455`, `:4463`, `:4470`). `jetstream.ErrNoStreamResponse`
      (`jetstream/publish.go:244-246`) and a server `*jetstream.APIError` (`:255-257`) are deliberately NOT on it —
      neither contract says the store did not run. Test:
      `TestBareCancelWithUnconfirmedSignalQuarantines`, four subtests. Mutations: delete
      `noteSignalAttempt` → the quarantine subtest flips AND its counterfactual cancels loop B; drop the
      proven-refusal conjunct → the refusal subtest flips.
      **Amended at round 2** (§ 11.4): `nats.ErrConnectionClosed` (`:4450`) came OFF the list — the same sentinel
      also returns post-write from `RequestMsgWithContext` (`context.go:70`)
- [x] 10.4 **F4 [P2] — the audit fingerprint was decoded from raw bytes; both production shapes logged empty**
      (`bbb4eae6`). `HandleVerdict` takes the decoded `VerdictPayload`, so the Component's existing
      `decodeVerdictPayload` normalization is the only decode, and the audit line reads every field through the
      top-level-then-`properties` fall-through. The naked-JSON observer test is
      REPLACED: `TestProposalFingerprintIsCarriedAndNotVerified` now drives the approve-action envelope and the
      publish-action map, built as `processor/rule/actions.go` builds them, through
      `handleToolCallVerdictMessage`. Mutations: top-level reads instead of the
      accessors → the publish-action shape and the waiter's rule id fail; re-unmarshalling the wire bytes → the
      envelope shape fails. BREAKING on a Tier 1 exported interface; zero sister implementers (grep across all
      nine); migration note carries the signature.
      **Corrected at round 2** (§ 11.6): the commit's second justification — "in enforce mode the same defect
      emptied the REASON the model is told" — is FALSE and had been published in four places. `EffectiveReason()`
      already fell through to `properties`, and the shape that loses everything is the approve action, which
      cannot carry a rejection
- [x] 10.5 **F5 [P2] — three spec contradictions, one of them a missing behaviour** (`554581f6`). (c) One TaskID
      naming two LoopIDs now quarantines. The comparison lives at the delivery seam and reads the token the
      PRODUCER sent, not `task.LoopID`: `preflightDecodedTask` reserves a fresh prospective UUID on every delivery
      of a lineage task that named no loop, so reading the field classified an ordinary redelivery as a conflict —
      which `TestPreflightGeneratedIdentityPreservesHandleTaskDedup` and
      `TestTransientLineageWriteNAKsThenResumesPendingSpawnOnRedelivery` caught, and which
      `TestTaskNamingADifferentLoopThanItsTaskIDQuarantines` now pins in all four arms. (b) The governance delta's
      "Retry for every absent waiter" is replaced by the three dispositions `settleVerdictWithoutWaiter` actually
      takes, one scenario each. (a) Three MODIFIED blocks restate the agentic-tools baseline requirements the
      ADDED-only delta contradicted, every scenario carried over. That third one was not only spec drift:
      `component.ToolRegistryReader` — the framework-wide executor contract — still named `ToolCall.ID` as the
      downstream idempotency key while `agentictools.ToolExecutor` had moved to `ExecutionID`. Mutations: delete
      the refusal call → the quarantine subtest flips; compare `task.LoopID` → the lineage-exemption subtest flips
- [x] 10.6 **F6 [P2] — two migration instructions that do not work as written** (`4b27d3cc`). Routing reads
      `execution_id` from the payload, never the subject, so "replace the suffix" left a publish-action rule
      losing every verdict; and a pre-upgrade `ToolCall` is terminated before the ledger read
      (`processor/agentic-tools/component.go:731`, ahead of `:744`), so it does not re-execute and deleting the
      outcome bucket cannot repair it. Both now cite the cutover contract where it is written
      (`docs/adr/104-unique-platform-authority.md:104-105`). The verdict handler's own stale `component.go:805`
      pin is re-derived to `:1094`
- [x] 10.7 Hygiene: the four EOF blank lines `git diff --check origin/main...HEAD` flagged in the spec deltas are
      stripped (folded into 10.5)

## 11. Internal review round 2 (PR #1335, two reviewers: loop half and dispatch half)

- [x] 11.1 **B1 [BLOCKING] — a redelivered CARRIED completion completed the loop** (`3e662a28`). F1 leaves a
      carried completion NON-TERMINAL at iteration N+1, which is exactly what takes its redelivery out of reach of
      the terminal guard (`handlers.go:1305`) — the guard `persistHandlerResult`'s classification rationale is
      written on. The redelivery found no deferral left to carry and settled the loop while `:req:N+1:0`, holding
      the user's turn, was still in flight; that request's answer would then be dropped as terminal. The guard is
      request identity: a response whose RequestID is not the loop's outstanding request is refused, Acked, and
      counted under `model_responses_dropped_total{reason="superseded_request"}`. The empty case is deliberately
      let through — a NAK/retry of the FIRST delivery arrives after the mark was cleared and is not stale. Test:
      `TestRedeliveredCarriedCompletionDoesNotCompleteTheLoop`, driven through the delivery seam. Mutation:
      `false &&` on the identity conjunct → "the redelivery completed a loop whose carried request is still in
      flight". 28 in-package fixtures invented RequestIDs like `"req-001"`; production routes a response to its
      loop BY its RequestID, so those were states production cannot produce — they now ask the handler what the
      loop published (`OutstandingRequestForTest`). Corrected at § 12.4: in two of the 28 the answer is `""`,
      because at that point the loop is waiting on nothing, so "they now answer the request the loop is actually
      waiting on" holds for 26 and the other two take the guard's deliberate empty carve-out
- [x] 11.2 **H1 — `emitRetryRequest` was a third request-building site** (`e2260b2b`). The truncation retry builds
      from `cm.GetContext()`, which already holds the deferred turn, and never ended the deferral: the completion
      that answered the retry deferred again and spent an iteration re-asking with a context that had gained
      nothing. The bookkeeping moved out of `publishIterationRequest` into `TrackRequest`, the call all three
      minting sites already make, so "every request that goes out carries the turn" is true by construction rather
      than by each new site remembering a fourth call. `design.md`'s "the ONE home" claim and the function's doc
      comment now say what it actually is. Test: `TestTruncationRetryCarriesTheDeferredTurn`. Mutation: clear back
      in `publishIterationRequest` only → the completion minted `:req:2:0` instead of settling
- [x] 11.3 **H2 — the marker was cleared at build and persisted before the publish that justified it**
      (`cdcf9c5a`). `persistResultState` stamps the entity before `publishResults`, and a publish-phase failure is
      commit-unknown → Quarantine, so the durable record said "nothing deferred" about a send that may never have
      happened. `LoopEntity.PendingContinuationRequestID` records WHICH request carries the turn;
      `HasPendingContinuation` means pending AND uncarried; `SettleRequest` clears both when that request's
      response arrives — the first moment the send is a fact, and before the completion logic, so a completion for
      the carrier settles normally. A turn admitted while a carrier is outstanding resets the carrier, because
      that turn is in no request's body. Test:
      `TestQuarantinedCarryLeavesTheDeferredTurnInTheDurableRecord`. Mutation: clear at build → the quarantined
      record forgot the waiting turn
- [x] 11.4 **Dispatch H1 — `nats.ErrConnectionClosed` was not a proven definite rejection** (`a9eca9d8`).
      `errors.Is` sees the sentinel, not the site: `nats.Conn.publish` returns it pre-write (`nats.go:4450`) and
      `RequestMsgWithContext` returns the same sentinel post-write (`context.go:70`) when
      `clearPendingRequestCalls` (`nats.go:5925-5932`) closes the reply channel on close or ForceReconnect. The
      sync publish path is `js.PublishMsg` → `RequestMsgWithContext`; `UseOldRequestStyle` is never set in this
      tree. A connection dropped after the signal went out was read as a refusal, retried, and cancelled a loop
      the message never named. Removed; the doc comment now says membership is a property of the SENTINEL.
      `m.JetStream()`'s refusal (`natsclient/client.go:980-983`) stays off for the opposite reason: genuinely
      pre-write, but a `fmt.Errorf` with no sentinel behind it (`client.go:885`), so recognising it would mean
      matching error text — omitting it over-quarantines, the direction this list fails on purpose. Test: fourth
      subtest of `TestBareCancelWithUnconfirmedSignalQuarantines`. Mutation: put it back → Retry (`0x2`) and the
      counterfactual cancels loop B
- [x] 11.5 **Loop MEDIUMs 1, 2, 5** (`62dc6cd4`). `SettleRequest`'s two id-matches were load-bearing and untested
      — an unconditional delete passed every other test in this package — and are now pinned in both directions
      (`TestSettleRequestOnlyClearsTheRequestItNames`, `TestSettleRequestEndsTheDeferralOnlyForTheCarrier`).
      `GetLoopForRequestWithRecovery` called `TrackRequest` to repair routing, so a READ announced that the loop
      was waiting on a request its caller had just answered; routing registration and the outstanding mark are
      separate writes now (`registerRequestRoute`), pinned by
      `TestRequestRecoveryRepairsRoutingWithoutMarkingOutstanding`. `HasOutstandingRequest`, which had no
      consumer, became `OutstandingRequest` and is consumed by 11.1's guard. Mutations: all three reproduced
- [x] 11.6 **Loop MEDIUM 4 — F4's second justification was false and published in four places** (`89bca953`).
      `VerdictPayload.Properties` carries the json tag `properties`, so the old raw unmarshal populated it and
      `EffectiveReason()` — pre-existing, unchanged by `bbb4eae6` — already found the publish-action reason; and
      the envelope shape that does lose everything is the approve action, which hardcodes
      `"decision": "approved"` (`processor/rule/actions.go:2198`) while `executeDeny` publishes nothing. No
      rule-engine rejection can ride it. The spec delta, the migration note and the `HandleVerdict` doc comment
      now say what WAS lost — every field read at the top level: all of them for the envelope, the audit
      fingerprint and the rule id for the raw map. The subtest that named the false defect passed against it and
      is re-pointed at the rule id, which the old top-level read dropped. Mutation: `verdict.RuleID` instead of
      `verdict.effectiveRuleID()` → the waiter is handed `""`
- [x] 11.7 **Records, pins and NITs** (this commit). Five stale pins re-derived with `sed -n`: § 9.3's three
      `component.go` pins (`:919`→`:926`, `:950`/`:955`→`:957`/`:962`, `:1025`→`:1040`) and the two in the comment
      block F3 rewrote (`component.go:1010` `:945-955`→`:953-963`, `:1016` `loop_tracker.go:204-226`→`:212-233`).
      Round 2's own line moves were swept the same way: five in-code pins in `processor/agentic-loop/component.go`
      into `handlers.go` — the terminal guard (`:1179-1185`→`:1305-1310`), the model-response timeout
      (`:1173`→`:1299`), the tool-result timeout branch (`:2245-2259`→`:2402-2417`) and the three context checks
      (`:2209`→`:2367`, `:2472`/`:2555`→`:2638`/`:2747`) — plus § 9.4's `handlers.go:2303`→`:2437` and § 9.1's
      mutation-table `handlers.go:1255`→`:1372`. The tools-complete wording now also says which of the two
      context checks is in `handleToolsComplete` and which is in the `publishIterationRequest` it calls.
      NIT 1: the `ErrMaxIterationsReached` arm in `carryDeferredContinuation` is unreachable from its only caller
      (`handlers.go:1313` fails the delivery on the same predicate first) — kept as a guard and the declared
      residual now says so instead of describing it as reachable. NIT 2: § 10.1's "settles exactly where a
      deduplicated one did" is corrected to "strictly later". NIT 3: a deferred continuation recorded no
      trajectory evidence naming its TaskID, so "which task contributed this turn" was answerable only from a log
      — against `openspec/project.md` § Purpose and ADR-098; it now emits one observation correlated on the task
      (`TestDeferredContinuationRecordsTheTaskThatContributedTheTurn`). NIT 4: `doc.go`'s embedder example calls
      `HandleTask` directly, which is BELOW the conflict guard at the delivery seam — the example says so now.
      Dispatch NIT-2: `docs/operations/17-tool-call-governance.md:322` and `:372` still framed `execution_id` as a
      subject concern in the table an operator reads while verdicts are being terminated.
      The published target truth moved with the code: the agentic-loop delta's identity requirement gains the
      superseded-response rule and the "record the carrier, clear at settle" rule, with two new scenarios (an
      unconfirmed carry, and a response the loop is not waiting on), and its old "the pending marker is cleared
      when that request is built" AND-clause is replaced. The migration note's
      `model_responses_dropped_total{reason}` entry now names both reason values, because
      `superseded_request` is new operator-visible behaviour and a redelivery is its commonest cause
- [x] 11.8 **Gates, measured on the head that ships** (§ 8.9's rule: a count carried across a round is not a
      measurement). `task lint` 0; `openspec validate stable-request-identity --strict` 0;
      `openspec validate --all --strict` 0 (56 passed, 0 failed); `task spec:properties` 0 (224/224 — round 1
      measured 217/217, round 2 added seven citations; the checker reads TRACKED test files only, so the count
      moves when a new test file is committed, not when it is written); `task schema:generate` 0 with
      `git diff --exit-code schemas/ specs/` 0; `go run ./cmd/entity-id-audit .` 0 (1332 structured candidates);
      `git diff --check origin/main...HEAD` 0; `go test -race -count=1` over `./agentic/`,
      `./processor/agentic-loop/`, `./processor/agentic-dispatch/`, `./processor/agentic-tools/`,
      `./processor/agentic-model/`, `./processor/rule/` 0. This round touched no integration-tagged file, so the
      integration suite ran once, through the canonical runner and its host lock inside `task check:push` — exit
      0. CI run and job count recorded in the PR body with the head sha

## 12. Internal review of round 2 (1 HIGH, 3 MEDIUM, 4 NIT, no code defect; 12.1-12.4 are docs and comments, 12.5 lands the two owed tests)

The review verified every round-2 fix and reproduced every mutation verbatim on `b292f605`, and ran two of its
own: moving `SettleRequest` ahead of the guard is GREEN (and is PROVABLY equivalent — `carrier != "" ⟹ carrier ==
outstanding`, because `TrackRequest` writes both under one lock), while dropping the `outstanding != ""` conjunct
is RED in two pre-existing tests, so the empty carve-out is load-bearing and covered.

- [x] 12.1 **HIGH-1 — "at most ONE outstanding `agent.request` per loop" holds only for serialized deliveries.**
      `attachContinuation` reads the mark under the manager lock and releases it (`state.go:317`);
      `HandleModelResponse` clears it at `handlers.go:1258` and the carrying request does not re-take it until
      `TrackRequest` at `handlers.go:2771`. `agent.task` and `agent.response` are separate JetStream consumers
      (`component.go:1103-1108`) with no per-loop serialization between them, so a continuation delivered inside
      that window is admitted against an empty mark and both paths mint `…:req:N:0`. Reproduced deterministically
      by the reviewer with an overlay probe against the real state machine; the scheduling of two live consumers
      is inferred, the state transition is not. NOT a regression — before `8734713d` every mid-request
      continuation collided and F1 closed the serialized case — but the delta published the narrowed behaviour as
      an unqualified SHALL in target truth that archives. **Ruled: qualify, do not add a concurrency primitive.**
      The SHALL now reads "ACROSS DELIVERIES IT PROCESSES IN ORDER" with the reason stated beneath it, `design.md`
      scopes "the invariant is enforced instead" the same way, and the window is a declared residual naming its
      two lines and its owner: L4's `LoopEntity.PublishedRequestID` under `Update(revision)` is the durable
      check-and-set that closes it (#1330, inventory placed by the coordinator at
      https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5749352045)
- [x] 12.2 **MEDIUM-1 — a FIFTH publication of the withdrawn F4 claim survived, in code.**
      `governance_dispatcher.go:637-640`, the comment `bbb4eae6` put above the enforce-mode channel send, still
      read "reading it off the wrong level of a publish-action verdict … is a rejection with no reason on it".
      Rewritten to what § 11.6 established. The round-1 review enumerated four sites and the withdrawal inherited
      the miss; the tree is now swept for a sixth — `grep -riE 'no reason on it|reason the model is told|rejection
      reached a model|wrong level|refused with'` over `*.go` and `*.md` returns only unrelated hits and § 11.6's
      own record of the claim as FALSE
- [x] 12.3 **MEDIUM-3 — the BLOCKING finding's own premise pin was wrong when written.**
      `superseded_response_test.go:65` cited `component.go:1902-1906`, which is the `publishResults` rationale;
      the sentence it means is `:1889-1892`. Not line drift: `git show 3e662a28:…` has the same miss. Re-derived
- [x] 12.4 **NITs.** NIT-1: the guard comment pinned AckWait/MaxDeliver/BackOff at `config.go:405`, the port
      declaration — they are declared at `config.go:169-171` and resolved for this lane at `component.go:956-992`.
      NIT-2: two of the 28 re-homed fixtures (`truncation_branch_test.go`'s post-progress truncation and
      `handlers_test.go`'s stale response to a terminal loop) read `""`, because at those points the loop is
      waiting on nothing; both now say so at the site. Asking the handler is still the right question — the answer
      is the empty carve-out, and inventing a name would put them back in a state production cannot produce — but
      `3e662a28`'s "all 28 now answer the request the loop is actually waiting on" is inaccurate for those two and
      is corrected here
- [x] 12.5 **NIT-3 and NIT-4 — CLOSED by the two tests they asked for.** Test-only: no production file changes.
      NIT-3 → `TestSupersededResponseDoesNotSettleATimedOutLoop` (`superseded_response_test.go`). The fixture
      leaves a loop waiting on `…:req:2:0` with its deadline already past (`SetTimeout(loopID, -time.Second)`,
      so `IsTimedOut` is true without the test waiting on a clock) and delivers the answer to the superseded
      `…:req:1:0` through the real lane. Observed: the delivery ACKs, the drop is counted under
      `superseded_request`, the persisted record keeps its state, its iteration, an empty `Outcome` and an empty
      `CompletedAt`, only the loop key is written (a `COMPLETE_` key would mean the loop had been terminated),
      and the loop still waits on `…:req:2:0`. The component's `natsClient` is constructed and never connected,
      so any publication this delivery attempted would surface as a Quarantine — the ACK is the "publishes
      nothing" assertion.
      **Mutation: `handlers.go` lines 1221-1254, the guard block, moved below the timeout arm's closing brace at
      line 1301** — the ordering NIT-3 names, and the only direction the move can go, because the timeout arm
      references `result`, which is declared after the guard. RED at `superseded_response_test.go:190`:
      `Not equal: expected: 0x1 actual: 0x4` — Ack became Quarantine, because the timeout arm failed the loop on
      a delivery that does not address it, published failure events and could not confirm them. Package-wide
      under that mutation exactly ONE test fails, this one.
      NIT-4 → `TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns` (`continuation_deferral_test.go`).
      Turn one is carried by `…:req:2:0`; turn two is admitted while that request is still in flight; the record
      must then name NO carrier and read as pending-and-uncarried again, and `…:req:2:0`'s completion must carry
      turn two into `…:req:3:0` rather than settle, with turn one still in the conversation that request sends.
      **Mutation: `state.go:323`, `entity.PendingContinuationRequestID = ""` in `attachContinuation`, deleted.**
      RED at `continuation_deferral_test.go:389`: `the record still names "<loopID>:req:2:0" as the carrier of a
      turn minted after it; that request's response would end the deferral and turn two would never be sent`.
      NIT-4's premise is confirmed rather than assumed: package-wide under that mutation exactly ONE test fails,
      this one — before it, that line was killed by nothing. A probe run (not committed) that silenced only the
      carrier assertion shows what the mutation actually costs: `the loop settled with turn two admitted and
      never asked; state=complete`.
      One test-fidelity fix rode along, surfaced by the new test: `getMetrics` is a package singleton
      (`metrics.go:77`, `metricsOnce`), so the superseded-drop counter accumulates across the whole test binary.
      `TestRedeliveredCarriedCompletionDoesNotCompleteTheLoop` asserted the absolute `1.0` and was green only
      because it incremented first; both tests now assert a DELTA of one through `supersededDrops`, which is the
      observation each of them actually owns

## 13. Owner's Codex review round 2 (PR #1335 on `d127adeb`, three P1, all accepted)

Every finding is a sequential reproduction against production handlers — no concurrency, no restart, none of them
the declared cross-consumer residual. Round-1 fixes confirmed by the same review; 8/8 hosted checks were green at
that head, which is the point: these are behaviours no gate was watching.

- [x] 13.1 **P1-1 — the identity guard let an empty OUTSTANDING mark through, and the mark is empty for a whole
      phase of every tool-using loop.** A tool-call response settles its request, so the loop sits on that
      iteration waiting for executors or an approval with no model answer owed. The guard's empty carve-out was
      written for one case — a first delivery's NAK arriving after its own mark was cleared — and admitted this
      one for free: task one starts request one, task two defers behind it, request one's completion carries task
      two into request two, request two answers with a tool call and settles, request one's completion redelivers,
      meets an empty mark and completes the loop. Task TWO, with task ONE's answer, while the tool carrying the
      user's turn is still running. **Fix as ruled:** `LoopManager.currentRequests` beside `outstandingRequests` —
      same writer (`TrackRequest`, the call every publish site already makes), NOT cleared by `SettleRequest`,
      dropped with the loop in `DeleteLoop` — and the guard compares against `CurrentRequest`. No durable field:
      durable request identity is L4's `PublishedRequestID` (#1330). No iteration-ordinal comparison either, which
      would re-admit a redelivered `:req:N:0` against a truncation retry's `:req:N:1`. A redelivery of the current
      request still passes, and so does an empty CURRENT — now meaning only "this process minted nothing", the
      process-replacement case, declared in `design.md` § Declared residuals against #1330.
      Test: `TestRedeliveredCompletionIsRefusedWhileTheCarrierWaitsOnTools`
      (`superseded_while_tools_pending_test.go`), the review's repro through the public handler, asserting the
      refusal is counted as `superseded_request`, the durable record's state, iteration, task identity, outcome
      and completion are untouched, and the live iteration's tool result still advances the loop to `:req:3:0`.
      **Mutation: `handlers.go:1253`, `CurrentRequest` → `OutstandingRequest` — the emptiness condition restored.**
      RED at `superseded_while_tools_pending_test.go:85`: `the redelivery completed the loop while its tool was
      still running; state=complete`, which is the finding verbatim. Package-wide under that mutation exactly ONE
      test fails, the new one.
      Two fixtures that read `""` on purpose (§ 12.4 NIT-2) now name the loop's current request through a new
      `CurrentRequestForTest` seam. `handlers_test.go`'s terminal-loop stale response is the one that mattered:
      under the new guard a response naming `""` is dropped as superseded one guard EARLIER than the terminal
      guard it exists to test, so it would have stayed green for the wrong reason
- [x] 13.3 **P1-3 — a terminal tool with a deferred turn settled the loop silently.** The carry check protected
      `StatusComplete` only; `toolResult.StopLoop` ran the completion path with no pending check at all, and the
      framework's own `decide` executor returns `StopLoop: true`, so this is a production shape rather than a
      hypothetical. The review observed `state=complete pendingContinuation=true carrier=""` with no request that
      had ever contained the accepted turn. **Fix as ruled:** carry, for consistency with the text path and with
      the spec's SHALL — a completion is a completion whichever way the model says it — and only a loop with
      nothing deferred completes there.
      One thing the text path never needed: accumulated tool results reach the conversation in
      `handleToolsComplete`, which the completing path never ran. Carrying without that drain puts an assistant
      tool call into the carried request with no tool message answering it, and `RepairToolPairs` drops the call
      the model just made. The drain is now one home, `absorbToolResultsIntoContext`, called from both paths.
      `LoopCompletedEvent.Decision` deliberately does not travel on the carried iteration: the field is the typed
      decision of the terminal that ENDED the loop (`agentic/events.go:88-91`), and this one did not end it — the
      call and its result stay in the trajectory and in the conversation. Checked against the ruling's stop-and-ask
      condition: nothing requires the typed payload to be preserved AS a completion event, so no refusal event and
      no new surface was added.
      Test: `TestDeferredContinuationIsCarriedByATerminalTool` (`continuation_deferral_test.go`) — deferred turn,
      terminal tool, then: not terminal, no completion record, no `agent.complete`, `:req:2:0` carrying BOTH the
      turn and the terminal tool's own result, the carrier recorded, and the loop settling normally once that
      request is answered.
      **Mutation 1: `handlers.go:2501-2511`, the terminal-path carry block, deleted.** RED at
      `continuation_deferral_test.go:490`: `the terminal tool settled a loop with an admitted turn; state=complete`
      — the finding verbatim. **Mutation 2: `handlers.go:2503`, the `absorbToolResultsIntoContext(loopID, cm)`
      call alone, deleted.** RED at `continuation_deferral_test.go:509`: `the carried request does not carry the
      terminal tool's own result "the first thing is decided"; its assistant tool_call travels unpaired`. Each
      mutation fails exactly ONE test package-wide, this one.
      Spec: the SHALL now names both completion shapes and the tool-message pairing, with a new scenario
      "A terminal tool answers a loop that has a deferred continuation"
