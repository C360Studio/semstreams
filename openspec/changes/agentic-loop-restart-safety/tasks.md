# Tasks: agentic-loop restart-safe settlement

## 0. Accepted gates

- [x] 0.1 Complete the file:line surface, lane, state, lifecycle, and adopter inventory.
- [x] 0.2 Receive independent `INVENTORY PASS` for SHA-256
  `70603493e56887c3e355dcf9087891e03cf7ea7764454fcf528e0686b1bdfe9d`.
- [x] 0.3 Receive independent `DESIGN REVIEW PASS` at `b8e2c031` and explicit owner acceptance on #1146.
- [x] 0.4 Remove `skip_specs: true` and materialize draft capability deltas.

## 1. Blocking settlement foundation

- [ ] 1.1 Hold implementation until #759 merges.
- [ ] 1.2 Confirm every touched consumer uses #759's accepted `DeliveryResult` contract and native delivery owner.
- [ ] 1.3 Confirm #1155 real-NATS replacement proof is available and reusable.
- [ ] 1.4 Reconcile the design against merged #759; stop for reinventory if the surface differs materially.
- [ ] 1.5 Add immutable `DeliveryAttempt` observation to #759 without exposing native message or settlement methods.
- [x] 1.6 Obtain design review and owner acceptance for the #759 addendum before #1146 model work.
- [ ] 1.7 Quarantine and stop the exact owner when delivery metadata is unavailable.
- [ ] 1.8 Test first delivery, second delivery, crash-before-call false unknown, and unavailable metadata.

## 2. Stable identity and replay helpers

- [ ] 2.1 Define deterministic TaskID, LoopID, RequestID, execution identity, output identity, and fingerprints.
- [ ] 2.2 Preserve provider CallID; add framework RequestID and execution correlation.
- [ ] 2.3 Evolve `TOOL_CALL_OUTCOMES` identity without adding a second outcome authority.
- [ ] 2.4 Add exact committed-message lookup and collision validation for requests, responses, and verdicts.
- [ ] 2.5 Add deterministic `Nats-Msg-Id` to every required output.

## 3. Provider settlement

- [ ] 3.1 Add `fail_commit_unknown`, `at_least_once`, and admitted `provider_reconcile` policies.
- [ ] 3.2 Default to `fail_commit_unknown` and publish a typed machine-readable `AgentResponse` failure.
- [ ] 3.3 Reconcile an existing committed matching response before provider invocation.
- [ ] 3.4 Return explicit settlement for parse, resolution, invocation, error-response, and publication paths.
- [ ] 3.5 Add real-NATS replacement failpoints around invocation, return, response commit, and source ACK.
- [ ] 3.6 Add closed `AgentResponseFailureKind` validation.
- [ ] 3.7 Emit `provider_commit_unknown` only with error status.
- [ ] 3.8 Reject unknown enum values and prohibit classification through error-string parsing.

## 4. Loop task and response settlement

- [ ] 4.1 Replace void task and response adapters with typed delivery work.
- [ ] 4.2 Add direct `LoopEntity` read-through by LoopID.
- [ ] 4.3 Settle task birth, initial request, created event, and terminal failures at the delivery boundary.
- [ ] 4.4 Reconstruct response context and configuration from committed request material.
- [ ] 4.5 Classify exact duplicate, proven applied, missing, and conflicting response identities.
- [ ] 4.6 Route every required KV, Store, and publication failure into settlement.

## 5. Tool-result continuation

- [ ] 5.1 Stamp RequestID and framework execution identity on `ToolCall` and `ToolResult`.
- [ ] 5.2 Reconstruct ordered tool batches from committed request, response, and pending results.
- [ ] 5.3 Replace `stale_callid` log-and-drop with proven ACK, Retry, Terminate, or Quarantine.
- [ ] 5.4 Persist each accepted result before publishing the next tool or request.
- [ ] 5.5 Prove replacement between every result persistence and downstream PubAck boundary.
- [ ] 5.6 Reuse `TOOL_CALL_OUTCOMES`; add no claimed or in-progress tool ledger.

## 6. Approval continuation and dispatch

- [ ] 6.1 Define and register `ApprovalContinuationV1` using the payload-registry checklist.
- [ ] 6.2 Wire every required composition root and add production-decoder round-trip coverage.
- [ ] 6.3 Store and verify continuation through `StoreRegistry` before ACKing approval-required results.
- [ ] 6.4 Add typed continuation reference and applied-decision fingerprint to `PendingApprovalState`.
- [ ] 6.5 Keep approve or modify evidence until the approved `ToolResult` arrives.
- [ ] 6.6 Reconstruct configured approval deadlines from current `AGENT_LOOPS` after replacement.
- [ ] 6.7 Rebuild dispatch `LoopTracker` from `AGENT_LOOPS` and add exact HTTP read-through.
- [ ] 6.8 Test pending, approve, modify, reject, timeout, duplicate, and conflicting decisions across replacement.
- [x] 6.9 Record owner choice for finite approval timeout rather than a new reference authority.
- [ ] 6.10 If finite is selected, reject zero, empty, and over-retention approval timeout when gating is enabled.
- [x] 6.11 Record the owner-selected 12-hour finite default.
- [ ] 6.12 Test expired entity and permanently missing continuation behavior.
- [ ] 6.13 Implement canonical payload digest, deterministic key, get-before-put, and read-back verification.
- [ ] 6.14 Test matching reuse, malformed and semantic collision, lost Put reply, and transient Get.
- [ ] 6.15 Add best-effort post-dependency cleanup metrics; add no scanner or reaper.
- [ ] 6.16 Leave the payload indexing profile empty and projection contracts nil.
- [ ] 6.17 Census every composition root and document downstream `payloadbuiltins` adoption.
- [ ] 6.18 Build the dispatch projection off-path and install only after initial snapshot completion.
- [ ] 6.19 Mark AutoContinue unavailable on initial or live-watch interruption.
- [ ] 6.20 Preserve explicit LoopID exact reads during incomplete AutoContinue hydration.
- [ ] 6.21 Test complete-empty, complete-unique, complete-ambiguous, interrupted, and stale-terminal hydration.

## 7. Signals and projections

- [ ] 7.1 Convert signal handling to typed settlement with explicit happy and sad definitions.
- [ ] 7.2 Make cancel wait for `COMPLETE_` state and terminal PubAck.
- [ ] 7.3 Treat created and approval-pending consumers as reconstructable projections, not authority.
- [ ] 7.4 Test AutoContinue and approval HTTP after replacement with empty process caches.

## 8. Governance correlation proof

- [ ] 8.1 Add stable proposal identity and fingerprint without changing #1140 policy content.
- [ ] 8.2 Replace missing-waiter completion with validated retained-verdict recovery.
- [ ] 8.3 Test replacement before proposal, after proposal, after verdict ACK, and before tool publication.
- [ ] 8.4 If retained verdict and response redelivery succeed, add no durable governance state.
- [ ] 8.5 If they fail, stop with the named failpoint for a new owner ruling; do not invent a bucket.

## 9. AGENT replay admissibility

- [x] 9.1 Record owner choice to require observed `DiscardNew` for restart-safe admission.
- [ ] 9.2 Read actual `StreamInfo` before starting recovery-dependent consumers.
- [ ] 9.3 Compute the ordinary horizon from framework-owned timeout and consumer policy.
- [ ] 9.4 If strong admission is selected, reject `DiscardOld` and other early-eviction bounds.
- [ ] 9.5 Test `DiscardOld`, `DiscardNew`, insufficient MaxAge, full-stream backpressure, and missing evidence.
- [ ] 9.6 Document migration, capacity, and backpressure cost.

## 10. Context and lifecycle closure

- [ ] 10.1 Remove return-before-join behavior from `runWithBudget`.
- [ ] 10.2 Remove return-before-join behavior from trajectory batch recording.
- [ ] 10.3 Prove callback cancellation cancels and joins before settlement.
- [ ] 10.4 Prove `Stop` joins every task spawned by touched deliveries.

## 11. Verification and documentation

- [ ] 11.1 Add table-driven unit tests for every lane's happy and sad disposition.
- [ ] 11.2 Add real-NATS process-replacement tests using #1155.
- [ ] 11.3 Serialize and run the relevant agentic E2E tier.
- [ ] 11.4 Correct the false restart claims identified in the accepted inventory.
- [ ] 11.5 Document provider ambiguity, Store requirements, metrics, and external-executor migration.
- [ ] 11.6 Obtain SemStreams reviewer approval.
- [ ] 11.7 Obtain owner-run cross-agent review.
- [ ] 11.8 Archive as the final content commit and obtain narrow archive and spec-sync review.

## Hold: AgentRun

- [ ] H.1 After #1148 merges, reinventory AgentRun against the accepted baseline.
- [ ] H.2 Add AgentRun only through a separately reviewed and owner-accepted design delta.
