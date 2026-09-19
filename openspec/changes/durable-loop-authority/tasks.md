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
- [x] 6.2 All twelve `// spec:` annotations retargeted to the new heading, and each checked BY HAND against the
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

- [x] 6.7 Mutation evidence for the three behavioural fixes (`cp` backup + `md5 -q` verified restore, no stash,
      no checkout; `loop_admission.go` baseline `02a9323a…`, `http.go` baseline `62db2148…`, both restored and
      `git status --porcelain` empty afterwards):

      | mutation | test that dies |
      |---|---|
      | drop `Iterations`/`MaxIterations`/`StartedAt` from `persistedLoopFacts` | `TestStatusReportsIterationProgressAndAgeFromTheRecord/a_record_with_a_start_time_reports_both` — `loop_seams_test.go:770` does not contain `Iterations: 7/19`, `:772` does not contain `Age: 1m30s` |
      | restore the pending-block-before-state ordering in `handleLoopApproval` | `TestHandleLoopApproval_CurrentAuthority` executing / cancelled / failed — `approval_handler_test.go:107`, 503 `"loop record is not readable right now"` where 409 is required |
      | return `err.Error()` on the `GET /loops` refusal again | `TestUnavailableLoopViewAnswersWithoutFrameworkInternals/GET_/loops` — `loop_seams_test.go:875` should not contain `Component.currentLoopSnapshot`, `:877` should not contain `failed:` |

## 5. Gates

- [x] 5.1 Re-run on the round-1 head: `task lint` 0, `task openspec:validate` 0 (57 passed, 0 failed),
      `task spec:properties` 0 (213/213 citations resolve),
      `go test -race -count=1 ./processor/agentic-dispatch/... ./processor/agentic-loop/...` 0 (five packages `ok`,
      no `FAIL`). `task schema:generate` with no drift is covered inside `check:push` by `schema:check-changes`
- [x] 5.2 `task check:push` 0 on the round-1 head: zero `FAIL` lines, 312 `ok`, `[INTEGRATION] tests complete`
- [x] 5.3 `task e2e:agentic` on the final head — required, both commits are BREAKING. Exit 0,
      `assertions_run=15`, `duration=2m4.752573667s`, all 17 stages green including
      `verify-stage-a-process-replacement` (78.679s, `dispatch_replacement_user_responses:1`),
      `verify-durable-tool-replay` (44.652s) and `walk-approval-path` (`approval_listing_matched:2`)
- [ ] 5.4 Implementation review resolved; stack rebased onto the reviewed L1 head; archive as the final content commit

## 7. Rebase onto the reviewed L2 head (`7eaff212`)

- [x] 7.1 `git rebase --onto 7eaff212 5188b9c9` — twelve commits, **nine** file conflicts across two of them.
      Taken to L0.5's side, because its paused-state removal is finished work this layer's older text predates:
      `agentic/state.go` (the `LoopStatePaused` constant stays deleted), `http.go` and `specs/openapi.v3.yaml`
      (L0.5's ten-value loop-state filter, which is its own reviewed fix), and
      `docs/operations/migration-beta162-to-beta163.md` (its measured per-sister section supersedes this layer's
      three sentences saying the constant cleanup "is tracked in #1146"). Taken as a union:
      `loop_admission.go` twice — L0.5's `State` doc comment with this layer's one-source `Terminal`, then
      `Iterations`/`MaxIterations`/`StartedAt` — and `terminal_origin_integration_test.go`, where L0.5 gave the
      origin-walk fixtures `MaxIterations: 3` and this layer gave them canonical loop tokens, both required once
      `looptoken.Valid` guards the reader. `loop_tracker_test.go` is a modify/delete: deleted, since this layer
      deletes the tracker it tests
- [x] 7.2 The writer census in `validatePersistedLoop` is CORRECTED, not carried. L0.5's comment claimed the other
      `AGENT_LOOPS` writers "cannot reach this key at all: they are prefixed". **Two** writers use the bare
      loop-id key: `agentic-loop`'s `persistLoopState` (`processor/agentic-loop/component.go:2235`) and
      graphresearch's research-pipeline record (`frameworkcapabilities/graphresearch/executor.go:267` through
      `register_tool.go:91`, `KVStore.Create` on the bare id). The research record is NOT refused — its id is a
      full canonical UUID (`executor.go:231`), its state is `executing`, and `NewLoopEntity` floors
      `max_iterations` at 20 (`agentic/state.go:256-257`) — and its empty `TaskID` is not a defect, because
      `Validate` requires id, a known state and a positive budget and says nothing about `task_id`
- [x] 7.3 `TestIntegrationInvalidPersistedRecordIsToleratedOnlyBecauseTheTrackerAnswers` is deleted and named as
      the FOURTH deletion in the `## REMOVED Requirements` reason. Its assertion is that a defective record is
      TOLERATED because the tracker answers instead (`facts.Tracked` true, `facts.Persisted` false); with no
      tracker there is nothing to answer, and its surviving sibling
      `TestIntegrationPersistedInvalidStateIsPermanent` already asserts the record's refusal. Only
      `go vet -tags=integration` catches this class — the untagged build never compiles the file
- [x] 7.4 The two `// spec:` citations the removed heading stranded re-home onto "Loop existence and ownership
      come from durable authority alone", whose invalid-authority scenario already reads "stateless,
      unknown-stated or without a positive iteration budget". `task spec:properties` is the check and is green
- [x] 7.5 `recordDeliveryOwnerFatal`'s comment (`processor/agentic-dispatch/component.go:716-723`) named three
      lanes; `agent.created` and `agent.approval_pending` were deleted with the tracker that fed them, and its
      only caller is the `user.message` lane at `:569`
- [x] 7.6 The watch fired and was ruled, not repaired locally:
      `TestIntegrationPersistedInvalidStateIsPermanent` went red because this layer's canonical-token
      precondition (`terminal_settlement.go:133-135`) refuses a fixture keyed `"paused-loop"` for its IDENTITY
      before the state is decoded. L0.5 re-keyed its own fixtures; `a7a2f354` is cherry-picked with `-x` so the
      later rebase onto main drops it by patch-id
- [x] 7.7 One production consequence of the cherry-pick, and it is an improvement rather than a concession: the
      re-keyed test asserts the refusal names bucket AND key, which is what L0.5's inline validation emitted and
      what the three sibling failures in the same function already emit (`access %s`, `read %s/%s`,
      `malformed %s/%s`). This layer's shared helper had dropped the bucket (`invalid loop state %q`), so it
      becomes a method and names it. It does NOT invent one: the requirement forbids a reader carrying a
      bucket-name default of its own, so an unresolvable bucket falls back to the unqualified message rather than
      to a guessed `"AGENT_LOOPS"`
- [x] 7.8 Sibling fixtures re-checked rather than assumed, since a canonical-token precondition refuses early and
      a test can go green for a reason it does not claim. All four non-canonical keys L0.5 flagged were already
      re-keyed by this change's own `d147458c`: `restart-loop` → `…-000000000008` (`:48`), `malformed-loop` →
      `…-000000000009` (`:133`), `expected-loop` → record `…-000000000010` under key `…-000000000011`
      (`:141`, `:144`), `quarantine-loop` → `…-000000000013` (`:479`). Each still reaches the branch it claims:
      `:138` asserts `malformed AGENT_LOOPS/…`, which is the JSON-decode branch AFTER the precondition, and
      `:149` asserts `contains invalid loop identity …`, which is the key/ID branch after it. A sweep of every
      `kv.Put(ctx, "…")` and `ID: "…"` literal in the file found no remaining non-canonical loop key
