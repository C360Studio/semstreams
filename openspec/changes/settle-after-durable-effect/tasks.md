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
- [x] 4.4 Prove the delivery-attempt view stays immutable and bounded:
      `TestDeliveryAttemptExposesOnlyImmutableAttemptObservation`.

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
- [x] 6.3 Quarantine a partially published handler result rather than replaying returned PubAcks, while a
      pre-publish failure still retries: `TestIntegrationPartialPublishQuarantinesRatherThanRetrying`.
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

## 9. Landing

- [ ] 9.1 Complete SemStreams implementation review and resolve findings.
- [ ] 9.2 Archive this change (`openspec archive settle-after-durable-effect`) as the final content commit and
      obtain the narrow reviewer check of the archive/spec sync.
