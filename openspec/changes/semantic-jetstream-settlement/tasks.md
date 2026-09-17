# Tasks: semantic JetStream settlement

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof.

## 1. Reviewed gates and claim

- [x] 1.1 Materialize the accepted C2 inventory checkpoint and collision boundary.
- [x] 1.2 Complete independent inventory review: `INVENTORY PASS`.
- [x] 1.3 Complete C6 options/design and independent review: `DESIGN PASS`.
- [x] 1.4 Record explicit owner approval on #759.
- [x] 1.5 File and reconcile #1155 as the process-replacement admission gate.
- [x] 1.6 Committed the OpenSpec proposal first as `12878610`, pushed the isolated branch, and opened draft PR #1156
      with the original claim and `implemented-by: Sol`; the accepted greenfield amendment governs the staging and
      final closing body from this point forward.
- [x] 1.7 Complete the 2026-09-02 inventory rebaseline, Stage A design reconciliation, independent design review, and
      independently reviewed owner-approved greenfield staging amendment.

## 2. TDD additive foundation

- [x] 2.1 Characterize every legacy ACK, 30-second retry, Term, 5-second cancellation, InProgress, and error-chain path.
- [x] 2.2 Add the initial RED contract for all five DeliveryDecision constants, zero/unknown decisions, the
      error-last work result, per-delivery and nil payloads, every valid/invalid tuple, error unwrapping, typed panic
      quarantine, and no disposition constructor family. Task 2.9 supersedes the initial callback signature with the
      final `DeliveryWork(context.Context, DeliveryAttempt, []byte)` contract before implementation.
- [x] 2.3 Add the complete DeliveryResult decision/handling truth table: exact requested-decision preservation, typed
      causes, cause reachability, local-method predicates, false server confirmation, quarantine, and
      OwnerStopRequired.
- [x] 2.4 Add pre-implementation retry-policy tests for zero, immediate Nak, fixed delayed Nak, nonpositive delay,
      and preservation of semantic cause across local method success/failure.
- [x] 2.5 Add pre-implementation heartbeat-policy tests for nil/ended context, nil work, invalid retry,
      heartbeat/AckWait/BackOff
      bounds, equality, canonical default, defensive copy, and zero runtime defense before Data or any message method.
- [x] 2.6 Add exact current/target nine-binding configuration tests and same-config validation/acquisition conformance.
- [x] 2.7 Implement DeliveryDecision/DeliveryWork, policies, one Data extraction per admitted delivery, private message
      ownership, cancel/join/interpret, and permanent `ConsumeDeliveryWithHeartbeat` using only a private terminal
      method executor.
- [x] 2.8 Prove `ConsumeWithHeartbeat`, `TerminateDelivery(error) error`, and `PermanentDeliveryError`
      characterization unchanged after private executor extraction.
- [x] 2.9 Add failing tests for opaque `DeliveryAttempt`, exact
      `DeliveryWork(context.Context, DeliveryAttempt, []byte)` signature, nil-impossible value semantics, zero
      behavior, first delivery, second delivery, and conservative crash-before-call redelivery.
- [x] 2.10 Add failing tests for metadata error, nil metadata, and zero delivery number. Assert typed
      `DeliveryMetadataUnavailableError`, cause reachability, Quarantine, OwnerStopRequired, one Metadata call, and
      zero Data, work, heartbeat, or terminal calls.
- [x] 2.11 Implement metadata observation before Data/work, migrate the three policy bindings through local wrappers
      that leave domain handlers unchanged, migrate settlement fakes, preserve C8/C9, and prove panic, cancellation,
      control-loss, and every started task still join under valid metadata.
- [x] 2.12 Add the deprecation notice and exact shrinking AST zero-growth staging guard for
      `ConsumeWithHeartbeat`; docs/examples advertise only the permanent typed API. The guard is not an API
      allowlist, current capability, compatibility promise, or merge authority.

## 3. TDD owner-private control loss

- [x] 3.1 Build a test-only owner harness; add no shared production gate.
- [x] 3.2 Test callback-before-handle fatal buffering, capacity one, concurrent admission, and already-admitted
      completion.
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
- [x] 4.6 At the foundation checkpoint, assert branch-staged model/loop/AgentRun source, config, settlement,
      cancellation, logs, and health remain unchanged before their separately reviewed migrations.
- [x] 4.7 Prove metadata-unavailable results close admission and drain the exact tools or dispatch handle outside
      callback, including callback-before-handle ordering.
- [x] 4.8 Replace builder-only tests with permanent policy/API integration tests, recheck zero adopters, obtain the
      approved Stage A gate, and remove `NewDurableHandler` without alias.
- [x] 4.9 Add the reviewed `gated-dag-dispatch` delta: correct PubAck ambiguity, preserve deterministic
      `Nats-Msg-Id`/dedupe-window authority, and remove generic nil/error and heartbeat mechanics from the domain
      capability.
- [x] 4.10 Materialize `docs/operations/migration-gated-dag-semantic-settlement.md` from the accepted SemSpec and
      SemDragon checkpoints. Record registration, enablement, current definition of done, exact-handle gap, and
      owner-specific typed migration without sister mutation.
- [x] 4.11 Add `docs/concepts/33-semantic-settlement.md` with the message pump, lease watchdog, owner-defined done,
      disposition, happy path, and process-replacement pattern without teaching the staged legacy API.
- [x] 4.12 Correct `docs/operations/migration-restart-safe-nats-client.md` for owner-specific done and the atomic
      default-branch cutover.

## 5. Real-NATS and #1155 Stage A

- [x] 5.1 Prove healthy InProgress renewal prevents overlap and stopped renewal follows BackOff independently of
      semantic retry, using scaled integration timing.
- [x] 5.2 Assert production tools configuration keeps BackOff 15s/60s and heartbeat 5s.
- [x] 5.3 Replace SemStreams while retaining NATS; prove tools first redelivery follows the 15-second class, completed
      replay publishes without a second executor effect, and ambiguous post-effect state quarantines.
- [x] 5.4 Prove dispatch replacement produces no duplicate user response and ambiguous publication never enters
      unlimited retry.
- [x] 5.5 Prove owner-fatal control loss, post-latch refusal, exact-handle shutdown, and reconstructed ordinary
      ownership.
- [x] 5.6 Run `GOFLAGS=-mod=readonly task e2e:agentic`: PASS after DeliveryAttempt admission in 2m03.999s with clean
      teardown; completed replay had
      one executor effect, tools BackOff redelivered at 15s with two quarantine attempts, and dispatch emitted one
      replacement response.
- [x] 5.7 With a real durable consumer, observe Number 1 on first delivery and Number 2 with `IsRedelivery` after
      explicit retry or missing settlement.

## 6. Non-default staged integrations

- [ ] 6.1 Rebase #759 onto current `main`, preserve merged #1245 coverage, complete the foundation/docs/spec
      reconciliation, review it, and record the pushed remote parent full SHA as `F`.
- [ ] 6.2 Retarget PR #1159 to base `codex/gh759-semantic-settlement`, rebase its branch onto exact `F`, and verify its
      merge base and diff before implementation.
- [ ] 6.3 Correct #1146 proposal/design/tasks for exact `F`, the non-default base, full-scope fast-lane gate, and
      AgentRun transfer before its implementation, review, or archive.
- [ ] 6.4 Confirm #1146 implementation/proof, complete-claim implementation review, owner-requested cross-agent review,
      fixes/re-review, final-content archive, and narrow archive/spec-sync review are recorded in that order.
- [ ] 6.5 After hosted #1159 integration, confirm its reviewed content, archive, and current-spec sync are present on
      #759; record its reviewed head and staging merge SHA without representing #1146 as closed.
- [ ] 6.6 Record the updated remote #759 head as `A`; create `codex/gh1249-agentrun-fanout-settlement` from exact `A`,
      commit its proposal first, and open a draft PR based on `codex/gh759-semantic-settlement` with `Closes #1249`.
- [ ] 6.7 Correct #1249 proposal/design/tasks for exact `A`, the non-default base, and complete AgentRun transfer before
      its implementation, review, or archive.
- [ ] 6.8 Confirm independent inventory/design review and owner acceptance precede AgentRun implementation.
- [ ] 6.9 Confirm #1249 implementation/proof, complete-claim implementation review, owner-requested cross-agent review,
      fixes/re-review, final-content archive, and narrow archive/spec-sync review are recorded in that order.
- [ ] 6.10 After hosted #1249 integration, confirm its reviewed content, archive, and current-spec sync are present on
       #759; record its reviewed head and staging merge SHA without representing #1249 as closed.

## 7. Final zero-caller cutover

- [ ] 7.1 Fast-forward the #759 worktree after each hosted child merge; do not recreate reviewed integrations through
      cherry-pick or local merge.
- [ ] 7.2 Shrink the branch-staging zero-growth guard after #1146 and #1249; never describe it as an API allowlist.
- [ ] 7.3 Prove zero production `ConsumeWithHeartbeat` calls, remove the exported helper without alias, and replace the
      staging guard with final absence conformance.
- [ ] 7.4 Prove all nine original bindings use their accepted typed settlement contracts and no binding was migrated
      through mechanical nil/error conversion.
- [ ] 7.5 Reconcile SemStreams-owned sister migration instructions and the temporary branch-only adopter seam.
- [ ] 7.6 Complete every #1155 replacement-proof row and run focused race, full race/integration, lint, build, schema,
      contracts, gated-DAG, and serialized agentic E2E gates.
- [ ] 7.7 After zero-caller/removal/full-proof gates pass, replace PR #1156 staging `Refs #759` with `Closes #759` and
      add `Closes #1146`, `Closes #1249`, and `Closes #1155` before implementation review of the complete claim set.
- [ ] 7.8 Obtain SemStreams implementation review of the complete integrated code and claim set.
- [ ] 7.9 Obtain the owner-requested cross-agent review.
- [ ] 7.10 Apply every finding and repeat implementation and cross-agent review until accepted.
- [ ] 7.11 Confirm #1146 and #1249 are archived and their current-spec sync is present.
- [ ] 7.12 Archive `semantic-jetstream-settlement` as PR #1156's final content commit.
- [ ] 7.13 Obtain narrow integrated archive/spec-sync review and make no later content commit.
