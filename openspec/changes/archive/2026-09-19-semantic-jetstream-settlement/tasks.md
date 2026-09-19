# Tasks: semantic JetStream settlement

Tasks record work when it happens. No task asserts a post-merge fact; CI and merge own that proof.

## 1. Reviewed gates and claim

- [x] 1.1 Materialize the accepted C2 inventory checkpoint and collision boundary.
- [x] 1.2 Complete independent inventory review: `INVENTORY PASS`.
- [x] 1.3 Complete C6 options/design and independent review: `DESIGN PASS`.
- [x] 1.4 Record explicit owner approval on #759.
- [x] 1.5 File and reconcile #1155 as the process-replacement admission gate.
- [x] 1.6 Committed the OpenSpec proposal first as `12878610`, pushed the isolated branch, and opened draft PR #1156
      with the original claim and `implemented-by: Sol`. That claim was superseded: PR #1156 and its branch are
      abandoned and the live claim is draft PR #1331 (§6.1), which carries the same thirteen commits byte-identical
      on `main`.
- [x] 1.7 Complete the 2026-09-02 inventory rebaseline, Stage A design reconciliation, independent design review, and
      independently reviewed owner-approved greenfield staging amendment.

## 2. TDD additive foundation

- [x] 2.1 Characterize every legacy ACK, 30-second retry, Term, 5-second cancellation, InProgress, and error-chain path.
- [x] 2.2 Add the initial RED contract for all five DeliveryDecision constants, zero/unknown decisions, the
      error-last work result, per-delivery and nil payloads, every valid/invalid tuple, error unwrapping, typed panic
      quarantine, and no disposition constructor family. The shipped callback contract is
      `DeliveryWork(context.Context, []byte)` (see 2.9).
- [x] 2.3 Add the complete DeliveryResult decision/handling truth table: exact requested-decision preservation, typed
      causes, cause reachability, local-method predicates, quarantine, and OwnerStopRequired. The constant-false
      `ServerConfirmed` accessor was withdrawn on review: no production reader, and its own doc warned against its
      affordance. Absence of the accessor is now the guarantee.
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
- [x] 2.9 An exported `DeliveryAttempt` observation was built, then withdrawn on review evidence: no production
      binding in this stack reads it — every callback on `origin/codex/gh1146-agentic-loop-restart` binds `_`, so it
      was zero-consumer exported surface on Tier 1 `natsclient`. The shipped signature is
      `DeliveryWork(context.Context, []byte)`. Metadata validation stays (2.10/2.11); only the caller-visible
      attempt is gone.
- [x] 2.10 Add failing tests for metadata error, nil metadata, and zero delivery number. Assert typed
      `DeliveryMetadataUnavailableError`, cause reachability, Quarantine, OwnerStopRequired, one Metadata call, and
      zero Data, work, heartbeat, or terminal calls. These hold unchanged after the 2.9 withdrawal.
- [x] 2.11 Implement metadata validation before Data/work, migrate the three policy bindings through local wrappers
      that leave domain handlers unchanged, migrate settlement fakes, preserve C8/C9, and prove panic, cancellation,
      control-loss, and every started task still join under valid metadata.
- [x] 2.12 Add the exact shrinking AST zero-growth guard for `ConsumeWithHeartbeat`; docs/examples advertise only
      the permanent typed API. The guard is not an API allowlist, current capability, compatibility promise, or
      merge authority. No `Deprecated:` marker ships: the owner ruled 2026-09-18 that a greenfield repository offers
      no deprecation period, so the helper's doc names the PR that deletes it (#1249) and
      `docs/operations/migration-beta162-to-beta163.md` names the removal for adopters.

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
- [x] 4.2 Encode tools done matrix: completed-outcome plus result PubAck ACK; typed proven-pre-effect or
      already-durable replay publication Retry; immutable poison Term; post-execution outcome-Create ambiguity
      Quarantine; any unclassified error Quarantine, never Retry.
- [x] 4.3 Migrate tools one binding to the permanent typed API and exact-owner control-loss reaction.
- [x] 4.4 Encode both dispatch terminal done matrices: deterministic response PubAck ACK; typed proven pre-publish
      failure Retry; owner-shutdown cancellation observed before any publish Retry; immutable terminal/route poison
      Term; unknown publish outcome Quarantine before MaxDeliver=0 retry; any unclassified error Quarantine, never
      Retry. In agentic-tools the same shutdown class needs no separate arm: it surfaces through the typed
      pre-effect ledger read, and a cancellation after execution lands on the ambiguous-Create arm.
- [x] 4.5 Migrate dispatch two bindings to the permanent typed API and exact-owner control-loss reaction.
- [x] 4.6 At the foundation checkpoint, assert branch-staged model/loop/AgentRun source, config, settlement,
      cancellation, logs, and health remain unchanged before their separately reviewed migrations.
- [x] 4.7 Prove metadata-unavailable results close admission and drain the exact tools or dispatch handle outside
      callback, including callback-before-handle ordering. Drain, not stop, is the recorded choice; each delivery the
      closed admission then refuses emits a log line and a lane-labelled counter rather than dropping silently.
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
- [x] 5.6 Run `GOFLAGS=-mod=readonly task e2e:agentic`: PASS in 2m03.999s with clean
      teardown; completed replay had
      one executor effect, tools BackOff redelivered at 15s with two quarantine attempts, and dispatch emitted one
      replacement response.
- [x] 5.7 With a real durable consumer, prove an explicit semantic Retry produces a server-counted second delivery of
      the same stream sequence, and that the framework exposes no delivery count to work (2.9 withdrawal).

## 6. Landing

Landing choreography — undraft, CI, squash, issue closure — lives on PR #1331's checklist, not here (owner ruling
on #1230, Option 1). These tasks assert only branch-checkable facts.

- [x] 6.1 Re-land the reviewed foundation directly on `main`: worktree branch `claude/gh759-semantic-settlement`,
      draft PR #1331, thirteen commits cherry-picked from the abandoned `codex/gh759-semantic-settlement` head.
      Identity verified at `0ccb1a74`: `git diff 417beae5 0ccb1a74 --stat` over all 54 files PR #1156 touched is
      empty. PR #1156 and its branch are abandoned; no task in this change targets them.
- [x] 6.2 Record the stack above this PR. Each child carries its own claim, review, and archive, and none closes
      #759: L0.5 is #1239 (PR #1332, base `claude/gh759-semantic-settlement`) and L1 is #1327 (PR #1334, base
      `claude/gh1239-signal-vocabulary`). This change's spec deltas cover the L0 foundation only.
- [x] 6.3 Record that removing `ConsumeWithHeartbeat` belongs to the PR that migrates its last production caller —
      the #1249 AgentRun layer — and not to this change (owner ruling 2026-09-18 on #759). No deprecation period is
      offered to adopters; `docs/operations/migration-beta162-to-beta163.md` names the removal, and the AST ratchet
      in `natsclient/consumer_policy_callsite_test.go` forbids any new caller until then.
- [x] 6.4 Apply every SemStreams implementation-review finding on PR #1331 and obtain re-review of the fixes.
      Four rounds: the SemStreams implementation review (four MAJOR, four MINOR), the owner's no-deprecation ruling,
      and two owner-run cross-agent rounds (three P2 then one P2). Round 3 at `c3a15cad` reports no remaining
      actionable findings.
- [x] 6.5 Archive `semantic-jetstream-settlement` as PR #1331's final content commit, with no later content commit.
      The narrow archive/spec-sync review of that commit is owed and belongs to the reviewer, not to this checkbox
      (this section asserts only branch-checkable facts).
