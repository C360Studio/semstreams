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

- [x] 4.1 Add the private per-component `deliveryLaneAdmission` latch and the per-binding drain observer.
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

## 6. Gates

- [x] 6.1 `task lint`, `task test`, `task schema:generate` with empty `schemas/`/`specs/` drift,
      `task openspec:validate`, `task spec:properties`, `task check:push` — commands and exit codes recorded in
      the PR body.
- [x] 6.2 Run `task e2e:agentic` on the landing head and record every stage's pass/fail verbatim in the PR body,
      including the process-replacement recovery stages this layer does not implement.

## 7. Landing

- [ ] 7.1 Complete SemStreams implementation review and resolve findings.
- [ ] 7.2 Archive this change (`openspec archive settle-after-durable-effect`) as the final content commit and
      obtain the narrow reviewer check of the archive/spec sync.
