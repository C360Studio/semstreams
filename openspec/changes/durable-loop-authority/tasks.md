# Tasks: durable loop authority and port-owned loop bucket

## 1. Replay the two loop-authority commits

- [x] 1.1 Cherry-pick `5e0e2259` — dispatch answers active-loop and admission questions from `AGENT_LOOPS`;
      `loop_tracker.go` and `loop_tracker_test.go` deleted (1,358 lines)
- [x] 1.2 Cherry-pick `68c14c8e` — the `loops` KV-write port owns bucket selection, `loops_bucket` retired,
      approval wait defaults to and is capped at 12h, bucket policy observed at startup
- [x] 1.3 Drop every `openspec/changes/agentic-loop-restart-safety/` hunk; the Codex change's evidence stays on
      the closed branch
- [x] 1.4 Resolve each conflict toward the L2 tree, carrying no sequential chat and nothing from
      `settlement_recovery.go`; every resolution and every deliberately-dropped behaviour is recorded in `design.md`

## 2. Port the approval gate that did not conflict cleanly

- [x] 2.1 `handleLoopApproval` reads current durable authority instead of the deleted tracker's pending cache
      (`processor/agentic-dispatch/http.go`), refusing unreadable/incoherent records as 503 and non-awaiting
      records as 409, without the non-carried `execution_id` echo
- [x] 2.2 Tests follow: `TestHandleLoopApproval_CurrentAuthority`,
      `TestHandleLoopApproval_FailedPublishPreservesPendingApproval` and `TestApprovalIsNotOwnerScoped` assert
      against the persisted record and that admission/publication never mutate it

## 3. Fixtures the base's auto-continue default exposes

- [x] 3.1 `newSeamTestComponent`, `newLoopTokenTestComponent` and `newRestartIdentityDispatch` opt out of
      auto-continue; all three build a Component directly with no view running and test refusal precedence, token
      validation, or task identity — never continuation. The reason is written at each site, and the one subtest
      that does want continuation builds an activity component over the real KV and opts back in
- [x] 3.2 `restart_identity_integration_test.go` drops the `independent_default` mode and the `prior_messages`
      assertion, which name a field this base does not have

## 4. Migration

- [x] 4.1 `docs/operations/migration-beta162-to-beta163.md` records the retired `loops_bucket`, the 12h approval
      cap, the tracker removal and its Go-caller impact, and the exact bucket-policy refusal text
- [x] 4.2 Adopter inventory measured read-only on semspec `5a9496ee`: thirteen `agentic-loop` configs carry the
      retired key, and each file's second `loops_bucket` belongs to semspec's own `recovery-consumer` and must not
      be touched
- [x] 4.3 `docs/concepts/17-approval-flow.md` states the 12h default and ceiling

## 6. Review round 1

- [x] 6.1 `openspec/specs/agentic-dispatch/spec.md:72-124` "Loop existence and ownership are merged facts, never
      process memory alone" is RETIRED by a `## REMOVED Requirements` block with Reason and Migration, not left as
      current truth the code refutes. REMOVED rather than MODIFIED because the heading itself is false once the
      tracker is gone: there are no merged facts to reword. Its replacement, "Loop existence and ownership come from
      durable authority alone", is in the ADDED block and carries every surviving obligation
- [x] 6.2 All eleven `// spec:` annotations retargeted to the new heading, and each checked BY HAND against the
      scenario it proves — `spec:properties` resolving a heading is not agreement. The mapping:

      | test | scenario it proves |
      |---|---|
      | `TestContinuationAfterReplacementIsAdmittedFromDurableRecord` | a continuation after a process replacement is admitted from the durable record |
      | `TestPreviouslyObservedLoopWithoutDurableRecordIsRefused` | a loop this process admitted before, whose record is now gone, is not found |
      | `TestUnreadableDurableRecordRefusesTransient` | an unreadable durable record refuses as transient |
      | `TestPriorAdmissionDoesNotBypassADurableReadFailure` | a prior admission is no fallback for a later read failure |
      | `TestCurrentOwnerReplacesPreviouslyObservedOwner` | only the current record establishes ownership |
      | `TestGateTerminalAuthorityRefusesContinuation` | terminal authority refuses continuation and stays readable |
      | `TestGateReportsExactCurrentStateWithoutMutatingAuthority` | a read reports the exact state and mutates nothing |
      | `TestStatusReportsTheRecordedStateNotAFabricatedRunning` | a read reports the exact state and mutates nothing |
      | `TestGateRefusesInvalidCurrentAuthorityBeforeOwnership` | invalid authority refuses before ownership is considered |
      | `TestLoopAdmissionValidatesPersistedAuthority` | invalid authority refuses before ownership is considered |
      | `TestReadSeamsAnswerFromTheDurableRecordAfterReplacement` | every read seam answers from the record after replacement |
      | `TestStatusReportsIterationProgressAndAgeFromTheRecord` | /status reports iteration progress and age from the record |

      `TestPreviouslyObservedLoopWithoutDurableRecordIsRefused` is the one that asserted the exact NEGATION of its
      old citation ("a live loop with no durable record is admitted from the tracker"); a comment at the site now
      says so, so the correction cannot be lost in a later sweep
- [x] 6.3 `/status` answers `Iterations n/m` and `Age` from the durable record again: `loopFacts` carries
      `Iterations`, `MaxIterations` and `StartedAt`, fed by `persistedLoopFacts`. Age has no substitute clock — an
      absent `StartedAt` is reported as absent rather than filled from the KV revision timestamp, which advances on
      every iteration. `TestStatusReportsIterationProgressAndAgeFromTheRecord` covers both arms
- [x] 6.4 The approval handler decides on the RECORDED STATE before any property of `PendingApproval`: a readable
      non-awaiting record is 409 regardless of a stale pending block, and 503 is reserved for records that cannot be
      read or do not validate. Clearing the pending block on a terminal transition is L4's and the reason is in
      `design.md`. Three table cases cover executing, cancelled and failed
- [x] 6.5 `POST /message` and `GET /loops` answer an unavailable view with a fixed client phrase; the wrapped
      internal detail goes to the correlated log line. `TestUnavailableLoopViewAnswersWithoutFrameworkInternals`
      asserts the body carries no `Component.currentLoopSnapshot` prefix on the DEFAULT `auto_continue` path
- [x] 6.6 Two unrecorded wire changes recorded: `LoopInfo.CreatedAt` now prefers `LoopEntity.StartedAt` over the KV
      revision timestamp (migration note), and `docs/advanced/08-agentic-components.md` no longer documents the
      deleted `semstreams_router_active_loops` gauge. The unreachable `loopLookupConflict` vocabulary is a declared
      residual in `design.md`, not deleted, because removing it edits the generated OpenAPI surface

## 5. Gates

- [ ] 5.1 `task lint`, `task test`, `task schema:generate` with no drift, `task openspec:validate`,
      `task spec:properties`
- [ ] 5.2 `task check:push`
- [ ] 5.3 `task e2e:agentic` on the final head — required, both commits are BREAKING. Exit 0,
      `assertions_run=15`, `duration=2m4.80s`, all 17 stages green including
      `verify-stage-a-process-replacement` (78.6s, `dispatch_replacement_user_responses:1`) and
      `walk-approval-path` (`approval_listing_matched:2`)
- [ ] 5.4 Implementation review resolved; stack rebased onto the reviewed L1 head; archive as the final content commit
