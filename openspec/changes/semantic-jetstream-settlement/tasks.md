# Tasks: semantic JetStream settlement

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof.

## 1. Reviewed gates and claim

- [x] 1.1 Materialize the accepted C2 inventory checkpoint and collision boundary.
- [x] 1.2 Complete independent inventory review: `INVENTORY PASS`.
- [x] 1.3 Complete C6 options/design and independent review: `DESIGN PASS`.
- [x] 1.4 Record explicit owner approval on #759.
- [x] 1.5 File and reconcile #1155 as the process-replacement admission gate.
- [x] 1.6 Committed the OpenSpec proposal first as `12878610`, pushed the isolated branch, and opened draft PR #1156
      with `Closes #759` and `implemented-by: Sol`.

## 2. TDD additive foundation

- [x] 2.1 Characterize every legacy ACK, 30-second retry, Term, 5-second cancellation, InProgress, and error-chain path.
- [x] 2.2 Add pre-implementation tests for all five DeliveryDecision constants, zero/unknown decisions, the
      exact error-last `DeliveryWork(context.Context, []byte)` signature, per-delivery and nil payloads, every
      valid/invalid tuple, error unwrapping, typed panic quarantine, and the absence of a disposition constructor family.
- [x] 2.3 Add the complete DeliveryResult decision/handling truth table: exact requested-decision preservation, typed
      causes, cause reachability, local-method predicates, false server confirmation, quarantine, and
      OwnerStopRequired.
- [x] 2.4 Add pre-implementation retry-policy tests for zero, immediate Nak, fixed delayed Nak, nonpositive delay, and preservation
      of semantic cause across local method success/failure.
- [x] 2.5 Add pre-implementation heartbeat-policy tests for nil/ended context, nil work, invalid retry,
      heartbeat/AckWait/BackOff
      bounds, equality, canonical default, defensive copy, and zero runtime defense before Data or any message method.
- [x] 2.6 Add exact current/target nine-binding configuration tests and same-config validation/acquisition conformance.
- [x] 2.7 Implement DeliveryDecision/DeliveryWork, policies, one Data extraction per admitted delivery, private message
      ownership, cancel/join/interpret, and permanent `ConsumeDeliveryWithHeartbeat` using only a private terminal
      method executor.
- [x] 2.8 Prove `ConsumeWithHeartbeat`, `TerminateDelivery(error) error`, and `PermanentDeliveryError`
      characterization unchanged after private executor extraction.
- [ ] 2.9 Add the deprecation notice and exact shrinking AST allowlist for `ConsumeWithHeartbeat` only; docs/examples
      advertise only the permanent typed API.

## 3. TDD owner-private control loss

- [x] 3.1 Build a test-only owner harness; add no shared production gate.
- [x] 3.2 Test callback-before-handle fatal buffering, capacity one, concurrent admission, and already-admitted completion.
- [x] 3.3 Test post-latch callbacks perform no work, heartbeat, Ack, Nak, delayed Nak, or Term.
- [x] 3.4 Test InProgress failure with joined Ack/Retry/Terminate/Quarantine preserves meaning, attempts no terminal
      method, sets OwnerStopRequired, and stops the exact handle outside callback.
- [x] 3.5 Test terminal method error alone stays unknown/not-confirmed and does not latch the lane.
- [x] 3.6 Test ordinary Stop and fatal shutdown share one private once path and the observer joins Stop.

## 4. Stage A — tools and dispatch

- [x] 4.1 Change tools heartbeat default 120s→5s while preserving AckWait 300s and BackOff 15s/60s.
- [x] 4.2 Encode tools done matrix: completed-outcome plus result PubAck ACK; completed replay publication Retry;
      immutable poison Term; post-execution outcome-Create ambiguity Quarantine.
- [x] 4.3 Migrate tools one binding to the permanent typed API and exact-owner control-loss reaction.
- [x] 4.4 Encode both dispatch terminal done matrices: deterministic response PubAck ACK; proven pre-publish failure
      Retry; immutable terminal/route poison Term; unknown publish outcome Quarantine before MaxDeliver=0 retry.
- [x] 4.5 Migrate dispatch two bindings to the permanent typed API and exact-owner control-loss reaction.
- [x] 4.6 Assert held model/loop/AgentRun source, config, settlement, cancellation, logs, and health remain unchanged.
- [ ] 4.7 Replace builder-only tests with permanent policy/API integration tests, recheck zero adopters, obtain the
      approved Stage A gate, and remove `NewDurableHandler` without alias.

## 5. Real-NATS and #1155 Stage A

- [ ] 5.1 Prove healthy InProgress renewal prevents overlap and stopped renewal follows BackOff independently of
      semantic retry, using scaled integration timing.
- [x] 5.2 Assert production tools configuration keeps BackOff 15s/60s and heartbeat 5s.
- [ ] 5.3 Replace SemStreams while retaining NATS; prove tools first redelivery follows the 15-second class, completed
      replay publishes without a second executor effect, and ambiguous post-effect state quarantines.
- [ ] 5.4 Prove dispatch replacement produces no duplicate user response and ambiguous publication never enters
      unlimited retry.
- [ ] 5.5 Prove owner-fatal control loss, post-latch refusal, exact-handle shutdown, and reconstructed ordinary ownership.
- [ ] 5.6 Run `task e2e:agentic` after Stage A with clean teardown and record the exact result.

## 6. Non-authorizing binding gates

- [ ] 6.1 BLOCKED model: require a then-current line-addressable inventory/design addendum, `entity-or-bucket` for any
      durable provider outcome, independent reviews, named owner acceptance, and #1155 paid-execution replay proof.
- [ ] 6.2 BLOCKED loop task: require its own current addendum, reviews, named owner acceptance, returned durability,
      deterministic outputs, rehydration, and replacement proof.
- [ ] 6.3 BLOCKED loop response: require its own current addendum, reviews, named owner acceptance, returned durability,
      deterministic outputs, rehydration, and replacement proof.
- [ ] 6.4 BLOCKED loop tool-result: require its own current addendum, reviews, named owner acceptance, returned
      durability, deterministic outputs, rehydration, and replacement proof.
- [ ] 6.5 BLOCKED AgentRun complete: wait for #1148, rebase/re-inventory, then require its own handler/replay addendum,
      independent reviews, named owner acceptance, and #1155 proof.
- [ ] 6.6 BLOCKED AgentRun error terminal: wait for #1148, rebase/re-inventory, then require its own handler/replay addendum,
      independent reviews, named owner acceptance, and #1155 proof.

## 7. Legacy removal and final verification

- [ ] 7.1 Shrink the exact legacy allowlist with every accepted migration; apply model60/loop15 timing only then.
- [ ] 7.2 Reconcile SemStreams-owned migration instructions for measured sister callers without sister mutation.
- [ ] 7.3 BLOCKED legacy removal: require all six held-binding gates complete, zero repository callers, sister migration
      reconciliation or explicit coordinated break ruling, full #1155 and agentic E2E proof, and later owner approval.
- [ ] 7.4 Run focused race tests, repository lint/race/integration/schema/contracts, and residual API/context searches.
- [ ] 7.5 Reconcile OpenSpec task truth and archive as the final content commit, followed by narrow archive/spec-sync review.
