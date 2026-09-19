# Tasks: settlement after durable effect

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof.

## 1. Claim

- [x] 1.1 Open draft PR against `claude/gh1239-signal-vocabulary` (L0.5) with `Closes #1327` and
      `implemented-by: opus` in the body.
- [x] 1.2 Record this layer's provenance: the code and test commits are re-landed from
      `origin/codex/gh1146-agentic-loop-restart` (`104f4ef8`, `674d6f32`, `fe00f7cf`, `79b0f29f`); the
      fast-delivery experiment and its revert (`bef0b372`…`16c31f89`) net to zero code and are dropped.

## 2. Settlement after the durable effect

- [x] 2.1 Add `natsclient.SettleDelivery` as the settlement-only half of the typed owner: validate the closed
      decision/error tuple, attempt at most one terminal method, own no work, context, heartbeat, or lifecycle.
- [x] 2.2 Move agentic-loop's cancel-signal, approval-response, approved-verdict, and rejected-verdict callbacks
      onto it, joining all delivery-derived work before the decision is formed.
- [x] 2.3 Move agentic-dispatch's `user.message`, `agent.created`, and `agent.approval_pending` callbacks onto it.
- [x] 2.4 Move agentic-governance's task, request, and response validation callbacks onto it.
- [x] 2.5 Prove the truth table and the fail-closed tuples: `TestSettleDeliveryDecisionTruthTable`,
      `TestSettleDeliveryInvalidTuplesAndNilMessageFailClosed`.
- [x] 2.6 Prove publication-before-ack on the production callbacks, against real NATS where the effect is durable:
      `TestIntegrationLoopSignalAndApprovalCallbacksCommitBeforeAck`,
      `TestIntegrationGovernanceProductionCallbacksPublishBeforeAck`,
      `TestDispatchProductionCallbacksDoNotAckFalseDone`,
      `TestResponseAndToolResultPersistenceFailureCannotAck`,
      `TestPersistHandlerResultReturnsPublicationFailureBeforeTerminalRelease`,
      `TestRequiredLoopStatePersistenceReturnsErrors`,
      `TestLoopCancellationUnknownPublicationQuarantinesWithoutReleasingTransientState`.
- [x] 2.7 Prove malformed inputs terminate rather than ack on all three components:
      `TestLoopProductionCallbacksTerminateMalformedNonHeartbeatInputs`,
      `TestDispatchProductionCallbacksTerminateMalformedNonHeartbeatInputs`,
      `TestGovernanceProductionCallbacksTerminateMalformedInputs`.

## 3. Delivery work joins before settlement

- [x] 3.1 Cancel-and-join, never cancel-and-return, on budget expiry:
      `TestRunWithBudgetWaitsForCooperativeWorkToJoinAfterCancellation`.
- [x] 3.2 Prove the approval-rejection graph write joins before the callback settles:
      `TestIntegrationApprovalRejectionJoinsCancelledGraphRequestBeforeQuarantine`.

## 4. Fatal delivery-owner health latch

- [x] 4.1 Add the private per-lane `deliveryLaneAdmission` latch and the per-binding drain observer.
- [x] 4.2 Latch the first cause once, across lanes, without overwrite or recount:
      `TestDeliveryFatalHealthKeepsFirstCauseAcrossLanes`.
- [x] 4.3 Prove panic and unavailable-metadata paths stop the exact owner:
      `TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner`,
      `TestGovernanceProductionCallbackPanicLatchesFirstFatalAndDrainsExactOwner`,
      `TestLoopUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner`,
      `TestModelUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner`,
      `TestLoopSetupWiresMetadataFailureToAcquiredOwner`, `TestModelSetupWiresMetadataFailureToAcquiredOwner`.
- [x] 4.4 Prove delivery work observes only its context and its bytes. The observation is the `DeliveryWork`
      signature itself — `func(context.Context, []byte) (DeliveryDecision, error)`
      (`natsclient/delivery_settlement.go:35`), held by `TestDeliveryDecisionConstants`. The exported
      `DeliveryAttempt` view this task first cited was withdrawn with its test before this change began
      (`openspec/changes/archive/2026-09-19-semantic-jetstream-settlement/tasks.md` 2.9); neither the type nor
      `TestDeliveryAttemptExposesOnlyImmutableAttemptObservation` exists in the tree, so the citation was to
      nothing.

## 5. Heartbeat lease validation and retry floor

- [x] 5.1 Validate heartbeat against the observed lease before consumer allocation, returning a typed error and
      allocating nothing: `TestLongRunningLoopHeartbeatPolicyValidatedBeforeConsumerAcquisition`,
      `TestModelHeartbeatPolicyValidatedBeforeConsumerAcquisition`.
- [x] 5.2 Move the loop default heartbeat to 15s and declare agentic-model's port AckWait/heartbeat explicitly:
      `TestLoopDefaultConsumerDeclaresValidHeartbeatPolicy`, `TestModelDefaultPortDeclaresValidHeartbeatPolicy`,
      `TestPostFoundationBAgenticModelHeartbeatPolicyAmendmentIsExact`.
- [x] 5.3 Apply the `max_deliver` floor of 2 and refuse an explicit lower value:
      `TestLoopMaxDeliverCoversFixedBackOffBeforeConsumerAcquisition`,
      `TestResolveConfigAppliesLoopMaxDeliverFloor`.
- [x] 5.4 Hold every shipped fixture to the new policy: `TestShippedLoopFixturesResolveValidHeartbeatPolicy`,
      `TestShippedModelFixturesResolveValidHeartbeatPolicy`; update the nine `configs/**` files and
      `schemas/agentic-loop.v1.json` in lockstep.

## 6. Review round 1

- [x] 6.1 Classify a missing loop from its record, never from process memory, and use it at every site that
      previously Acked on a memory miss: `TestLoopIDFromStructuredIDAcceptsOnlyMintedTokens`,
      `TestClassifyMissingLoopWithoutBucketIsUnknownNotStale`, `TestClassifyMissingLoopWithoutTokenIsStale`,
      `TestClassifyMissingLoopReadsTheLoopsBucketNotMemory_Integration`,
      `TestUncorrelatedResponseSettlesByRecordNotByMemory`,
      `TestUncorrelatedToolResultSettlesByRecordNotByMemory`,
      `TestVerdictWithoutWaiterSettlesByRecordNotByWaiterMap`, `TestUncancellableLoopSettlesWithoutRetrying`.
- [x] 6.2 Terminate an undecodable or wrongly-typed input instead of acknowledging it:
      `TestUndecodableHeartbeatLaneInputTerminatesRatherThanAcking`,
      `TestWrongPayloadTypeOnHeartbeatLaneTerminatesRatherThanAcking`.
- [x] 6.3 Quarantine a handler result that fails at either phase rather than replaying returned PubAcks — the
      pre-publish half too, per 8c.1: the whole-entity write is replayable but the delivery is not, so a
      redelivery meets the handler's terminal guard and acknowledges an empty result.
      `TestIntegrationPartialPublishQuarantinesRatherThanRetrying` asserts both halves.
- [x] 6.4 Bound retry on the four non-heartbeat lanes with a validated BackOff/`max_deliver` floor and a delayed
      NAK: `TestNonHeartbeatLanesAcquireABoundedConsumer`,
      `TestNonHeartbeatLaneRefusesSingleDeliveryBeforeAllocation`,
      `TestSettleDeliveryWithRetryPerformsTheDelayedNak`.
- [x] 6.5 Give agentic-governance the same private `delivery_owner.go` trio the other three carry, replacing its
      inline admission locals.
- [x] 6.6 Record the declared residuals the round surfaced in `design.md`: synchronous `runWithBudget` under
      ADR-049, the void-returning graph writers, agentic-model's absent panic wrapper, and the four-way duplication
      of the delivery-owner trio.

## 7. Review round 2

- [x] 7.1 MODIFY `jetstream-consumer-policy`'s *shared settlement remains stateless and heartbeat-specific* so the
      archived truth matches the exported surface, restating its existing scenario verbatim; record in `design.md`
      (D8) that the block targets L0's delta text and why the stacking order makes that safe.
- [x] 7.2 Delete the unreachable `maxDeliver == 0` floor and correct the withdrawn "0 = unlimited" premise in the
      production doc comment, `proposal.md` and the test's own comment: `component.GetConsumerConfig` defaults
      MaxDeliver to 3, so the missing piece was the schedule, not the ceiling.
- [x] 7.3 Observe the delayed NAK at the call site, not only in the primitive:
      `TestNonHeartbeatLaneRetrySettlesAsADelayedNak`.
- [x] 7.4 Observe the publish-phase fatal classification and the arm ORDER without a broker:
      `TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified`,
      `TestHeartbeatWorkMapsFatalToQuarantineAheadOfPermanentAndRetry`.
- [x] 7.5 Correct the missing-waiter branch's "decision deliberately unset" comment: it returns Quarantine, the
      most severe decision, and is safe only because its one caller checks `ErrNoGovernanceWaiter`.

## 8. Gates

- [x] 8.1 `task lint`, `task test`, `task schema:generate` with empty `schemas/`/`specs/` drift,
      `task openspec:validate`, `task spec:properties`, `task check:push` — commands and exit codes recorded in
      the PR body.
- [x] 8.2 Run `task e2e:agentic` on the landing head and record every stage's pass/fail verbatim in the PR body,
      including the process-replacement recovery stages this layer does not implement.

## 8b. Durability judge round (2026-09-19)

- [x] 8b.1 Strike **cancel signal** from the `agentic-dispatch` delta's PubAck clause. The clause required
      synchronous JetStream PubAck for the cancel signal, but `processor/agentic-dispatch/commands.go:179`
      publishes it with core-NATS `c.natsClient.Publish` (`natsclient/client.go:858-864`, no JetStream context,
      no PubAck), and this change does not touch that file. Its home is L2 `23f7eb08`, which changes exactly that
      line to `PublishToStream`. Residual recorded in `design.md`; spec follows code at each layer. The
      loop-lane cancel clauses (`specs/agentic-loop/spec.md:10,32,109-113`) are unaffected — they govern the
      loop's handling of an **admitted** signal, not dispatch's publication of it
- [x] 8b.2 `TestDispatchProductionCallbacksDoNotAckFalseDone/failed user-response publication retries, never acks`
      — the response-PubAck gate (`component.go:1284` → Ack `:910`) had no observer, because the `sendResponseFn`
      seam short-circuits `sendResponse` before the publish. The new case drives the production `sendResponse`
      through the unknown-command path, where the user response is the only required publication, and asserts
      Retry with zero Acks. Mutation: returning Ack from the non-fatal arm of `handleUserMessage` makes exactly
      this case red

## 8c. Cross-agent implementation round 1 (2026-09-19)

- [x] 8c.1 (R1, blocking) A stamp-phase failure in `persistHandlerResult` classified as Retry, but the redelivery
      does not reach the loop the first attempt left: a complete model response has already moved the loop and
      built its completion record, and the redelivered response meets the terminal guard
      (`handlers.go:1179-1185`) and returns an empty result, so the second attempt writes the loop key, publishes
      nothing and ACKs. The phase now wraps `errs.WrapFatal` like the publish phase. Observed by
      `TestResponseAndToolResultPersistenceFailureCannotAck`. Production no longer reaches that guard at all: the
      first delivery quarantines, the lane latches, and `consumeAdmittedDelivery:80-82` refuses the redelivery —
      which the test asserts before it drives the counterfactual (same bytes, healed bucket, an admission that had
      not latched) and shows the loop key written with no `COMPLETE_` record. Mutation: return the stamp error
      unwrapped → both subtests red
- [x] 8c.2 (R1 sibling) `handleLoopFailure` was void and `publishFailureEvents` logged its failed PubAcks, so
      `handleResponseMessage` returned nil for a business failure nothing downstream could observe.
      `persistFailureState` was still log-only while its two siblings returned errors. All three report now; the
      caller quarantines an unestablished failure and retries only the case where the transition itself did not
      take, since nothing was written then. Mutation: swallow the established error → the operational
      spawn-identity test red
- [x] 8c.3 (R1 sibling, #1343) The tool-result handler-error branch logged and returned nil. A terminal result —
      `HandleToolResult`'s timeout branch builds one, with its failure record and publications — now goes through
      `persistHandlerResult` and settles on that write; a non-terminal error quarantines; cancellation retries so a
      clean stop cannot latch a false ownership fatal. **Narrowed by 8e.3**: only a cancellation observed before
      the handler mutated anything. `TestToolResultHandlerFailureSettlesOnTheDurableRecord`.
      Mutation: restore the swallow → two subtests red. This closes #1343 rather than scoping the loop delta
      around it
- [x] 8c.4 (R2) A user acknowledgement that failed after the task took its PubAck (`component.go:1136`) returned
      an ordinary error, and the redelivery mints a new task UUID at `:1093` with no dedup id — and a second loop
      when `auto_continue=false`. Now fatal. `TestIntegrationPublishedTaskWithFailedResponseQuarantines` is an
      integration test because a unit seam cannot produce a successful task publish followed by a failed response:
      both go through one client. L2 `15825335` (recover task identity on redelivery) is where identity-preserving
      replay lands and may relax this to Retry
- [x] 8c.5 (R3) `sendResponse` returns its publication error, and the two live HTTP callers
      (`http.go:283`, `:420`) discarded it — quieter than before, because the log line the old sendResponse
      emitted lived inside it. Both observe it through `noteUnpublishedResponse` (log at the old level with the
      lane named, plus `response_publish_failures_total{lane}`), leaving the accepted HTTP operation alone.
      `sendUserResponseForLoop` took the same observation rather than staying the one silent discard
- [x] 8c.6 (R4) Three artifacts still described the withdrawn `DeliveryAttempt` surface: the tuning guide's
      example, the `agentic-model` delta's observation scenario, and task 4.4's citation of a deleted test. All
      three now describe `DeliveryWork(ctx, []byte)`
- [x] 8c.7 (R5) `model_responses_dropped_total` and `tool_results_dropped_total` counted every retry of a
      live-elsewhere or unreadable record as a drop, against this change's own delta. The counters keep the stale
      case and lose the retry; both help texts now say what `signals_dropped_total` already said.
      `TestRetriedInputsAreNotCountedAsDrops`. Mutation: restore the response increment → two subtests red,
      restore the tool-result one → one; all three only when both are restored
- [x] 8c.8 (R6) Spec follows code: the governance delta said Retry where `component.go:444-447` quarantines; the
      dispatch delta said one cause across all owners where `component.go:321-339` keeps three latches,
      concatenates causes and preserves the terminal-only status; the dispatch delta's task/response scenario said
      Retry, which 8c.4 changed; and the loop delta's six-class guarantee is now true for every class except task
      intake, which is named as an exemption with resumable intake as its precondition
- [ ] 8c.9 (R6c residual, owner) Task intake's own swallows are unconverted and now named in the loop delta:
      `component.go:1279-1282` (decode), `:1284-1288` (wrong payload type), `:1304-1318` (handler failure),
      `:1387` (first publication) and `:1390` (loop-state write) all log and ACK. This is not a classification
      change — `HandleTask` dedups a redelivered task against the loop its first delivery created
      (`handlers.go` `HasActiveLoopForTask`) and `handleTaskMessage:1320-1327` then acknowledges without
      publishing — so it needs resumable intake, which `rememberPendingTaskResult` already prototypes for the
      transient-lineage case. Filed as **#1345** (beta.163, `class:swallowed-degrade`, placement candidate L4) and
      cited from the loop delta's exemption requirement and `design.md`; the layer placement is the owner's

## 8d. Cross-agent implementation round 4 (2026-09-19)

- [x] 8d.1 (B2, blocking) The model-response lane's `handleLoopFailure` wiring (`component.go:1519-1525`) had no
      test of its own: reverting it to `_ = c.handleLoopFailure(...); return nil` left the whole package green
      under both tag sets, because the only kill ran through `handleSpawnIdentityFailure` (the task lane) and the
      two response-lane callers in `terminal_release_test.go:420,478` discard the return by construction.
      `TestResponseHandlerFailureSettlesOnTheDurableRecord` drives the production callback through
      `consumeAdmittedDelivery` on `agent.response`. Mutation: the same revert → the quarantine subtest red
      (`expected 0x4, actual 0x1`)
- [x] 8d.2 (B1, blocking) R2's Quarantine had no declared residual. `design.md` now names L2 `15825335`
      (`fix(agentic-dispatch): recover task identity on redelivery`) as the home of identity-preserving replay and
      says the relaxation to Retry is a decision to be taken there, in the same shape as the cancel-signal
      residual
- [x] 8d.3 (MEDIUM-1) `component.go:976` — the command lane's post-effect response failure — was Retry by
      default. It stays Retry by decision: the redelivery is effect-free, because the gate finds the loop terminal
      and answers without publishing a second signal (`commands.go:136-148`), and a signal that races the loop's
      own settlement is dropped effect-free by the loop's cancel owner. Named in `design.md` D7 and in the
      dispatch delta; `TestIntegrationPublishedCancelWithFailedResponseRetries` holds the Retry and the
      effect-free redelivery. **Narrowed by 8e.1**: that argument holds only for the named form
- [x] 8d.4 (MEDIUM-2) The loop delta's heading claimed "All six loop input classes" two requirements above its own
      task-intake exemption. Renamed to *Loop input classes settle after owner-specific durable done* across the
      delta and its 16 citations
- [x] 8d.5 (MEDIUM-3) Two of the three `response_publish_failures_total{lane}` label values were unobserved. Each
      is now asserted once, by one test per lane: `http_command` by
      `TestHTTPResponsePublicationFailureIsObservedWithoutChangingTheResult` (8c.5), `loop_user_channel` by
      `TestLoopUserChannelResponseFailureNamesItsOwnLane`, and `http_submission` by
      `TestIntegrationHTTPSubmissionResponseFailureNamesItsOwnLane` — an integration test because an unconnected
      client fails the task publish before the response copy can be the thing that fails
- [x] 8d.6 (MEDIUM-4) #1345 is cited from the loop delta's exemption requirement, `design.md`'s task-intake
      residual and 8c.9, so the exemption points at a tracked home rather than prose
- [x] 8d.7 (NITs) 8c.1's counterfactual wording and 8c.7's mutation arithmetic corrected; `http.go:22` said "two"
      HTTP lanes over a three-constant block; and the terminal-business-failure scenario the round-3
      spawn-identity edit left uncited is cited by the new `response_handler_failure_test.go`, which is where that
      scenario is now observed on the response lane

## 8e. Cross-agent implementation round 3 (2026-09-19)

- [x] 8e.1 (R1, P1) 8d.3's effect-free argument assumed the target stays fixed across a redelivery, which is only
      true of `/cancel <loop_id>`. `handleCommand:941-951` resolves a bare `/cancel` from the tracker, and
      `GetActiveLoop` (`loop_tracker.go:204-226`) prefers the channel's loop only while it is non-terminal, then
      falls back to the user's most recent loop — so this delivery's own effect (loop A terminal) is what makes
      the redelivery resolve to a live loop B and cancel it. That form now quarantines; the named form keeps
      Retry. No durable selection record: that would write on every bare command for a rare path, and L4 (#1330)
      is where identity-preserving replay removes the need. `TestIntegrationBareCancelWithFailedResponseQuarantines`
      — two live loops, one user, redelivery conditional on the decision because that is what production does.
      Mutation: drop the tracker conjunct → the test sees B signalled. **Narrowed by 8f.1**: provenance alone was
      not the predicate
- [x] 8e.2 (R2, P1) `persistResultState`'s `FailureState` branch stamped graph triples and never wrote
      `COMPLETE_<loopID>`, so the tool-result timeout route published `agent.failed` and ACKed with no terminal
      record for any KV watcher to read. `persistFailureState` (`component.go:2271`) now runs before the stamp,
      error propagated, mirroring the completion branch. Census of the four `result.FailureState =` writers, which
      is what proves the new write cannot double-publish a failure event:

      | Writer | Branch | Returns | Reaches `persistResultState` | Reaches `publishFailureEvents` | Event published by |
      |---|---|---|---|---|---|
      | `handlers.go:1173` | `HandleModelResponse` timeout | `(result, fatal)` | no — the error routes to `handleLoopFailure` | yes (`component.go:1655`) | `publishFailureEvents`; this result's own messages are dropped with it |
      | `handlers.go:1983` | `failLoop`, from `StatusError` and `StatusLengthTruncated` | `nil` whenever `FailureState` is set — its one error return precedes the assignment | yes | no | `publishResults` |
      | `handlers.go:2243` | `HandleToolResult` timeout | `(result, fatal)` → `settleFailedToolResult` terminal | yes | no | `publishResults` |
      | `handlers.go:2500` | `handleToolsComplete` max-iterations | `(*result, nil)` | yes | no | `publishResults` |

      The two columns are disjoint: the only writer that reaches `publishFailureEvents` is the only one that does
      not reach `persistResultState`, so the record is written exactly once and the event published exactly once
      on every path. `TestIntegrationTerminalFailureRecordPrecedesItsPublication` (a broker, so "nothing was
      published" is a measurement rather than a nil client) plus the record assertion added to
      `TestToolResultHandlerFailureSettlesOnTheDurableRecord`. Mutation: remove the `persistFailureState` call →
      the presence assertions fail and the failure event is published with no record
- [x] 8e.3 (R3, P1) `settleFailedToolResult` retried ANY `context.Canceled`/`DeadlineExceeded`, but
      `HandleToolResult` checks its context three times and two of them (`handlers.go:2472`, `:2555`, inside
      `handleToolsComplete`) run after `StoreToolResult`, `RemovePendingTool`, `IncrementIteration` and
      `GetAndClearToolResults`. The first check (`:2209`) now returns `errCancelledBeforeMutation` wrapped around
      the context error — so every `errors.Is(err, context.Canceled)` reader is unaffected — and only that marker
      retries; every other cancellation is `errs.WrapFatal` → Quarantine.
      `TestToolResultCancellationRetriesOnlyBeforeMutation` drives the post-mutation case through the production
      `TodoReader` seam (`prependIterationContext` runs two lines before the `:2555` check) and asserts the
      iteration ADVANCED, so the fixture is proven post-mutation rather than assumed. Mutations: return a plain
      `ctx.Err()` at `:2209` → the pre-mutation subtest flips to Quarantine; drop the marker check in
      `settleFailedToolResult` → the post-mutation subtest flips to Retry. The cost and the two topology facts
      that size it are a declared residual in `design.md`
- [x] 8e.4 (R4, P2) Three published contracts contradicted the implemented behaviour, and OpenSpec's syntax and
      citation checks cannot see a behavioural contradiction. `proposal.md:45-47` said the stamp phase "stays
      retryable" where D7 and `persistHandlerResult` quarantine; task 6.3 said "a pre-publish failure still
      retries" while citing the test that now requires Quarantine; the dispatch delta's unauthorized-input
      scenario promised a deterministic error "before termination" where every `ResponseID` on that lane is
      `uuid.New()` (`component.go:918`, `:931`, `:956`, `:970`, `:1088`, `commands.go` ×9) and a published refusal
      Acks. All three now say what the code does. The sweep behind them was
      `grep -n -iE 'retr|determinis'` over the whole change directory: the remaining hits are accurate, including
      two that read as suspect and are not — the terminal lane's response identity really is source-derived
      (`terminal_settlement.go:17,219`, `terminal-user-response:<source_message_id>`), and the loop's terminal
      event really is keyed by loop id rather than a minted UUID. Response identity for the rest of the lanes is
      L2's (#1328), and the delta now says so rather than claiming it

## 8f. Cross-agent implementation round 4 (2026-09-19)

- [x] 8f.1 (HIGH-1) 8e.1's arm keyed on where the target came from, which is true of every argument-less command
      while `auto_continue` is on — `/help`, `/loops`, a bare `/status` — and of the three arms of bare `/cancel`
      that publish nothing (no active loop, gate refusal, already settled). Each fell through to the same
      `sendResponse`, so a failed response on a read-only command quarantined and latched the whole `user.message`
      lane, with a cause naming a loop it never touched. The predicate is now two conjuncts: this delivery
      PUBLISHED a signal AND its target was resolved rather than named. The published half is recorded at the
      publish site (`commands.go:185`) on a per-delivery recorder carried in the context (`command_effect.go`),
      never inferred from the command name or the response text. It is not a `CommandHandler` return value
      because `processor/agentic-dispatch` is Tier 1 and that type is exported — the reviewer offered the
      signature change and the ruling allowed it, but it would break every adopter that registers a command to
      carry a fact the publish site already has. `TestEffectFreeCommandWithFailedResponseRetries` (three
      subtests, each asserting the lane is not latched). Mutations: drop the published conjunct → the `/help`
      subtest quarantines and latches; drop the tracker conjunct →
      `TestIntegrationPublishedCancelWithFailedResponseRetries` quarantines
- [x] 8f.2 (HIGH-2) The breaking-change E2E evidence predated all three round-3 production commits, and R3
      re-classifies the clean-stop path `verify-stage-a-process-replacement` drives
      (`test/e2e/scenarios/agentic/scenario.go:251`). `task e2e:agentic` re-run at the head carrying the HIGH-1
      fix; tier, head, exit code and duration are in the PR body's gate table, replacing the `20fe8d09` claim
- [x] 8f.3 (NIT-1, NIT-2) Every pin in `design.md`'s R3 residual and the PR body regenerated with
      `sed -n "${n}p"` after the code settled, never transcribed; the 139-character delta line re-wrapped

## 9. Landing

- [ ] 9.1 Complete SemStreams implementation review and resolve findings.
- [ ] 9.2 Archive this change (`openspec archive settle-after-durable-effect`) as the final content commit and
      obtain the narrow reviewer check of the archive/spec sync.
