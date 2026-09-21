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

- [x] 5.1 Re-run on the round-1 head: `task lint` 0, `task openspec:validate` 0, `task spec:properties` 0,
      `go test -race -count=1 ./processor/agentic-dispatch/... ./processor/agentic-loop/...` 0 (five packages `ok`,
      no `FAIL`). `task schema:generate` with no drift is covered inside `check:push` by `schema:check-changes`.
      The counts that used to sit on this line — 57 passed and 213/213 — were the round-1 base's and are not
      re-stated here, because a count carried across two rebases is not a measurement: § 7.9 carries the numbers
      measured on the head that ships (56/56 and 232/232)
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
      loop-id key: `agentic-loop`'s `persistLoopState` (`processor/agentic-loop/component.go:2266`) and
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
- [x] 7.9 Second-delta review (APPROVE with residuals). The unqualified fallback inside `validatePersistedLoop`
      had **no producer**: every call site holds a bucket a successful resolution in the same instance produced,
      `loopsBucketFromPorts` is pure over `c.config.Ports` and that field is never reassigned after `Configure`,
      and the reviewer's panic probe on the arm left the package green. Removed by passing the already-observed
      bucket in — `validatePersistedLoop(bucket, loopID, persisted)`, a free function again — which deletes the
      dead arm and the second resolution together. `loadPersistedLoop` resolves once ahead of both paths; the
      activity view carries the name its own handle was opened under on `activityViewCommand`, because
      `graphview.WatcherSource` is deliberately narrow (`WatchAll` only) and widening a Tier 1 interface to carry
      a name for an error message is not worth it. The message format the cherry-picked test asserts
      (`invalid %s/%s: %w`) is unchanged, and the projection's `undecodable` message gains the same qualification
      so the threaded value has more than one consumer. Gate counts re-measured on this head: `openspec:validate`
      **56/56**, `spec:properties` **232/232**; § 5.1's 57 and 213/213 were the round-1 base's and are retired
      there rather than restated
- [x] 7.10 Census pins re-derived with `sed -n "${n}p"` after `a49607bc` shifted `agentic-loop/component.go` by
      +31: `persistLoopState`'s bare-key `Put` is `:2266` (was `:2235`) and the three `COMPLETE_` writers are
      `:2188`, `:2215`, `:2239` (were `:2157`, `:2184`, `:2208`), in both the code comment and § 7.2. The
      graphresearch pins still hold byte-identical (`executor.go:231`, `:267`, `register_tool.go:91`,
      `agentic/state.go:256-257`)

## 8. Rebase onto the rewritten L2 head (`e0f0bdf2`)

L2 (#1328) took its own review round and was rebased onto main, so `7eaff212` — the head § 7 replayed onto — no
longer exists on the branch. This section supersedes § 7's pins and gate counts; § 7 is left as the record of that
round, not restated.

- [x] 8.1 `git rebase --onto origin/claude/gh1328-stable-identity 7eaff212`, backup ref
      `refs/backup/gh1329-pre-l2main-rebase-20260919` = `b7b3de13`. **Sixteen** own commits in, **fifteen** out:
      the `cherry-pick -x` of L0.5's fixture re-key (`a7a2f354`, carried as `9d190ca5`) dropped by patch-id, as
      § 7.6 predicted it would once the rebase reached a base that already carried L0.5. The commit object is NOT
      an ancestor of `origin/main` — `git merge-base --is-ancestor a7a2f354 origin/main` exits **1**, and
      `git branch -a --contains` lists only `claude/gh1239-signal-vocabulary` — which is exactly what a squash
      merge produces: L0.5 landed as `8820ac51` (#1339), carrying the content. The evidence that nothing was lost
      is the tree, not the ancestry: `9d190ca5` touched `terminal_settlement_integration_test.go` and nothing
      else, and that file is byte-identical across the rebase (`md5 17aae63b7a8f6e2b0e710c2bb04ec399` at both
      `b7b3de13` and the post-rebase head). The `:191` failure-message fix the brief asked to preserve is NOT in
      that pick — it is its own commit (`9cb40d64` before the rebase), which replays intact
- [x] 8.2 Every edit under `openspec/changes/settle-after-durable-effect/` dropped: that change is archived on
      main. What this layer still needs from it is a `## MODIFIED Requirements` block here instead
- [x] 8.3 `handleTaskSubmission`'s post-PubAck comment carried three repeated effects. TWO are now false:
      identity, made recoverable by #1328's `findRetainedDispatchTask`, and the tracked `LoopInfo` replaced under
      an advanced loop, retired with the tracker by this change (`recordLoopStarted` and the `active_loops` gauge
      are both gone from this package — `grep -rn 'recordLoopStarted\|activeLoops' processor/agentic-dispatch/`
      returns only `intent_classifier.go`'s unrelated parameter). Only `tasks_submitted_total` survives. The
      Quarantine classification is CARRIED, not relaxed; the open question is a declared residual in `design.md`
- [x] 8.4 Three test files reached for `c.loopTracker`. Re-homed onto durable authority through a new
      `seedCurrentLoops` helper that drives the real `runActivityViewControl` loop and waits for
      `view.WaitCaughtUp`, so the fixture proves the route production takes. Two of them needed the view running
      at all — an argument-less command now resolves its target through `activeLoop`, which fails with "activity
      view lifecycle is not running" before reaching the assertion
- [x] 8.5 `delivery_owner_test.go`'s "unaccepted pending projection retries" subtest deleted, not re-homed: its
      whole discrimination was tracker-vs-durable. L1's "failed user-response publication retries, never acks"
      subtest kept unchanged. The bare-cancel integration fixture now seeds loopA on `session-a` and loopB on
      `session-b` and its header records WHY B is unreachable: `activeLoop` matches an exact user/channel route
      with no user-scoped fallback
- [x] 8.6 L2's counterfactual in `task_submission_settlement_integration_test.go` keeps `tasks_submitted_total == 2`
      and its `Equal(TaskID)` assertion and loses only its tracker assertions; the three fixtures L2 re-homed onto
      ExecutionID (`tool_result_handler_failure_test.go`, `terminal_release_test.go`,
      `terminal_failure_record_integration_test.go`) keep L2's shape untouched
- [x] 8.7 A `## MODIFIED Requirements` block for "Every dispatch durable input settles through its owner"
      restates **L2's delta text**, not `openspec/specs/`'s, because this change archives after #1328. FOUR of its
      eight scenarios change and no outcome moves: two only replace vocabulary this change deletes ("resolved from
      the tracker", "tracker and gauge state remain unchanged"), two change a stated REASON this change makes
      false. `design.md` § "Why the MODIFIED block reads ahead of `openspec/specs/`" says so for the archiver. The
      other four scenarios are byte-identical to L2's, checked by `diff -u` of the two blocks rather than by eye
- [x] 8.8 Census pins re-derived with `sed -n "${n}p"` on this head, superseding § 7.2 and § 7.10:
      `persistLoopState` is `processor/agentic-loop/component.go:2389` and its bare-key `Put` is `:2404`; the
      three `COMPLETE_` key constructions are `:2321`, `:2352`, `:2376`. `register_tool.go:92`,
      `executor.go:231`/`:248`/`:267` and `agentic/state.go:256-258` still hold byte-identical
- [x] 8.9 Gates on this head, exit codes read: `task lint` 0; `go test -race -count=1` on `agentic-loop` and
      `agentic-dispatch` 0; `openspec validate durable-loop-authority --strict` 0; `task openspec:validate`
      **57/57**; `task spec:properties` **244/244**; `task schema:generate` then
      `git diff --exit-code schemas/ specs/` 0. Twenty tests the three layers share run BY NAME with `-v` under
      `-tags=integration -race -count=1 -p 2` — nineteen integration plus the unit
      `TestEffectFreeCommandWithFailedResponseRetries`, one of the three the brief required to keep its meaning —
      and every one reports `--- PASS`, read per test rather than from the package `ok`, including
      `TestIntegrationPersistedInvalidStateIsPermanent`, which § 7.6 recorded RED and which the dropped
      cherry-pick is no longer needed to fix

## 9. Review round on the rebase (1 HIGH, 4 MEDIUM, 1 NIT — no functional defect)

The reviewer found the rebase mechanics correct, every L1/L2 guarantee intact under mutation, `seedCurrentLoops` a
production seam, and the `## MODIFIED Requirements` block exact. Every finding was a claim-accuracy one: the code
does the right thing and said the wrong thing about why.

- [x] 9.1 **HIGH.** The Quarantine arm's only in-code justification cited `GetActiveLoop` and
      `loop_tracker.go:204-226` — a symbol and a file THIS change deletes — and described a fall-through to the
      user's most recent loop that `activeLoop` refuses. `design.md` § "Declared cost" claimed that premise was
      "corrected in place"; the correction had reached the spec delta and the test header but not the comment an
      implementer reads, so the change shipped two accounts and the stale one was in the code. Rewritten to the
      mechanism that survives: the message does not carry the identity the delivery acted on, `activeLoop` kills
      the cross-channel form of the hazard, and what remains is **same-route rebirth** — a loop started on THIS
      user/channel route between the two deliveries is current when the redelivery reads and would be cancelled
      having never been named. The classification does not move
- [x] 9.2 **MEDIUM.** Eight `file:line` pins were staled by this change's own edits — five that were exact at L2,
      one **out of range** (`component.go:1565-1569` against a 1,406-line file), and one written by the rebase
      round that recorded "pins re-derived" in § 8.8. All re-derived with `sed -n "${n}p"` on this head, then
      re-derived AGAIN after the comment rewrite, which shifted `component.go` by +4:

      | site | was | now | resolves to |
      |---|---|---|---|
      | `command_effect.go:16` | `commands.go:185` | `commands.go:187` | `noteSignalPublished(ctx)` |
      | `component.go` conjunct 1 | `commands.go:185` | `commands.go:187` | `noteSignalPublished(ctx)` |
      | `delivery_owner_test.go` | `commands.go:185` | `commands.go:187` | `noteSignalPublished(ctx)` |
      | `delivery_owner_test.go` | `commands.go:179` | `commands.go:181` | `c.natsClient.PublishToStream(ctx, subject, signalData)` |
      | `task_submission_settlement_integration_test.go` | `commands.go:179` | `commands.go:181` | same |
      | `component.go` conjunct 2 | `:945-955` (inside its own comment) | `:875-890` | `loopID := ""` … closing `}` of the resolution block |
      | `command_effect.go:30` | `component.go:1565-1569` (out of range) | `component.go:1398-1402` | `handler := func(exec CommandExecutor) CommandHandler {` … `}(executor)` |
      | `task_submission_settlement_integration_test.go` | `http_activity.go:311-328` | `http_activity.go:321-339` | `func (c *Component) activeLoop(…)` … its closing `}` |

      Three non-Go pins in the same comment block were swept too and all still land:
      `release/tier1-packages.txt:74`, `.github/workflows/ci.yml:236-238`, `taskfiles/apicompat.yml:9-12`.
      `commands.go:136-148` was checked and left: it is a range that still covers the `facts.Terminal` gate
- [x] 9.3 **MEDIUM.** § 8.1's "Verified rather than assumed: `a7a2f354` is reachable from `origin/main`" was
      **false** — `git merge-base --is-ancestor` exits 1, because L0.5 squash-landed as `8820ac51`. Corrected in
      place to the proof actually held: the byte-identical fixture file across the rebase. A phrase asserting
      verification is the one a later reader trusts without re-checking, so it costs more wrong than a plain claim
- [x] 9.4 **MEDIUM.** The reworded carry commit cites **#1138** for the paused-state removal; the issue is
      **#1239** (PR #1339, which #1138 is unrelated to). Not amended: rewriting branch history a second time to
      fix a citation is worse than the citation. The coordinator authors the squash body with `--body-file`, so
      branch messages do not reach `main`; the miscitation is recorded in PR #1338's provenance section instead.
      It has not propagated — `git grep 1138` over this change directory and both packages returns nothing
- [x] 9.5 **MEDIUM.** `TestIntegrationBareCancelWithFailedResponseQuarantines`'s header credited the wrong
      assertion. The reviewer proved by mutation (drop the `ChannelID` conjunct from `activeLoop`) that a widening
      makes loop A and loop B BOTH match the route, so `activeLoop` refuses with `loop_route_ambiguous` and the
      command errors before publishing: what dies is the Quarantine assertion at `:436` and "the first delivery
      cancelled this channel's loop" at `:439`. The `require.NotContains` at `:449` — the assertion the header
      named as the guard — stays GREEN under that mutation, and under a classification mutation too, because its
      whole block is inside `if decision == Retry`. Header rewritten to credit `:436`/`:439` and to name `:449` as
      the inert belt-and-braces check, so a later author does not read it as the test's teeth. The fixture's second
      session stays: it is what makes a widened resolver ambiguous
- [x] 9.6 **NIT.** The spec delta says "resolved from durable loop authority" while the code still said "tracker"
      in the present tense at five sites. `targetFromTracker` renamed to `targetResolved` and all five rewritten.
      The package's other tracker mentions were checked and are correctly past-tense
- [x] 9.7 Gates after the round, exit codes read: `task lint` 0; `go test -race -count=1
      ./processor/agentic-dispatch/` 0; the three named dispatch tests by name under
      `-tags=integration -race -count=1 -p 2 -v`, all PASS; `openspec validate durable-loop-authority --strict` 0;
      `task spec:properties` 244/244; `task schema:generate` then `git diff --exit-code schemas/ specs/` 0

## 10. Rebase onto L2's round-1 head (`7b6a47fd`)

L2 (#1328) took a six-finding Codex round and one of those findings — F3, the cancel-signal ambiguity arm — lands in
`processor/agentic-dispatch`, the package this change rewrites. Nineteen own commits in, nineteen out, plus one
re-home commit. Backup ref `refs/backup/gh1329-pre-l2round1-rebase-20260920` = `072c5b4c`.

- [x] 10.1 `git rebase --onto 7b6a47fd e0f0bdf2` — four of the nineteen conflicted, nine file conflicts in total.
      `d6992612` (the durable-authority commit) took three: `component.go` resolved to the deletion, because L2's
      F2 edit added fields to a `handleAgentApprovalPending` this change retires; `loop_tracker.go` resolved to the
      delete; `http.go` kept this change's durable read AND L2's echo, which is the only resolution that is not a
      loss either way (10.2). `2d6983d9` and `db563f9d` were one hunk each. `a124852a` was four stale line pins
- [x] 10.2 **The approval echo had to be re-homed, not merged.** L2's F2 makes the HTTP approval endpoint echo the
      gated `ExecutionID` and `RequestID` onto the `ApprovalResponse`, because the loop now authorises on execution
      identity and refuses a response that omits it. F2 read them from `LoopTracker.GetPendingApproval`; this
      change deletes the tracker. The endpoint now takes them from `persisted.PendingApproval` — the record it
      already reads on every decision — and `publishApprovalResponse` takes an `agentic.PendingApprovalState`
      rather than the wire projection, so the durable record is the single source for all three fields and the
      `/loops` DTO is untouched. The dispatch delta said only "obtains CallID"; it now states the echo, since the
      loop's refusal makes it a correctness property and not a detail
- [x] 10.3 **F3's arm was re-homed too** (`9dd48384`). The conjunct is `targetResolved`, this layer's name for it
      (§ 9.6); `targetFromTracker` no longer compiles. The helper's justification cited `GetActiveLoop` falling
      through to another channel's loop — the exact stale account § 9.1 removed from the sibling arm — and is
      rewritten to same-route rebirth. `TestBareCancelWithUnconfirmedSignalQuarantines` put its two loops on two
      channels, a case `activeLoop` cannot produce; both now sit on session-a and the counterfactual drives the
      redelivery through a second lane against the world the first attempt left (A settled, B born on that route),
      sharing the recorded signals. Its header names the teeth and the inert assertion, as § 9.5 required of its
      sibling. Mutations, on this head: deleting `noteSignalAttempt` flips `0x4` → `0x2` AND the counterfactual
      then signals B (`should not contain "d9428888-…"`); dropping the `publishDefinitelyRejected` conjunct flips
      the proven-refusal subtest `0x2` → `0x4`
- [x] 10.4 Pins re-derived with `sed -n "${n}p"` on this head. F3 inserted the attempt record and the
      `publishSignal` seam into `commands.go`, so every § 9.2 row that pointed into that file moved again, and one
      no longer resolves to the line it names:

      | site | § 9.2 said | now | resolves to |
      |---|---|---|---|
      | `command_effect.go:22` | `commands.go:187` | `commands.go:193` | `noteSignalPublished(ctx)` |
      | `component.go:941` conjunct 1 | `commands.go:187` | `commands.go:193` | `noteSignalPublished(ctx)` |
      | `delivery_owner_test.go:324` | `commands.go:187` | `commands.go:193` | `noteSignalPublished(ctx)` |
      | `delivery_owner_test.go:432` | `commands.go:181` | `commands.go:187` | `c.publishSignal(ctx, subject, signalData)` |
      | `task_submission_settlement_integration_test.go:141` | `commands.go:181` | `commands.go:187` | same |
      | `component.go:964` | `commands.go:136-148` | `commands.go:142-152` | `if facts.Terminal {` … its closing `}` |
      | `component.go:942` conjunct 2 | `:875-890` | `:882-897` | `loopID := ""` … the closing `}` of the resolution block |
      | `command_effect.go:36` | `component.go:1398-1402` | `component.go:1413-1417` | `handler := func(exec CommandExecutor) CommandHandler {` … `}(executor)` |

      `http_activity.go:321-339` still lands. `design.md` § "Declared cost" carried four pins that were ALREADY
      stale before this rebase — `loop_admission.go:315,:317,:319` for the three `lookupLoop` producers (they are
      at `:324,:326,:328`) and `:170,:228-229` for the conflict vocabulary (`:179,:237-238`) — and two this
      rebase moved, the OpenAPI `"500"` responses at `http.go:1143`/`:1123`/`:1182`, now `:1166`/`:1146`/`:1205`.
      All six re-derived. The four stale ones were never in the § 9.2 sweep, which covered the comment block only

## 11. Rebase onto L2's round-2 head (`b292f605`)

- [x] 11.1 Backup ref `refs/backup/gh1329-pre-l2round2-rebase-20260920` = `bdf956d2`, then
      `git rebase --onto` L2's round-2 head. 23 commits replayed; ONE conflict, in
      `processor/agentic-dispatch/component.go`'s two-conjunct comment block — L2 round 2 re-derived its pins
      (`:945-955`→`:953-963`, `loop_tracker.go:204-226`→`:212-233`) in the very block this change rewrites to
      cite durable authority instead. Resolved to THIS layer's text: the tracker pins it re-derived name a file
      this change deletes, so carrying them would re-introduce the claim § 9's HIGH removed. The self-pin
      `:882-897` and `http_activity.go:321-339` both still resolve on the rebased head
- [x] 11.2 L2's whitelist change carried by the rebase: `nats.ErrConnectionClosed` is off
      `publishDefinitelyRejected`, because the same sentinel also comes back post-write from
      `RequestMsgWithContext`. The SUBTEST that proves it did not carry cleanly — L2 wrote it against the tracker
      fixture this change replaced — and is re-homed onto `cancelAmbiguityWorld`, seeding the session-a route and
      driving the counterfactual redelivery against a second world where A has settled and B was born on the same
      route. `TestBareCancelWithUnconfirmedSignalQuarantines` is four subtests again, all green
- [x] 11.3 Pins verified rather than assumed: `processor/agentic-dispatch/component.go`, `commands.go`,
      `http.go` and `loop_admission.go` are byte-identical across the rebase (`git diff <backup> HEAD -- <file>`),
      so every pin § 9.2 and § 10 re-derived still lands. `command_effect.go` grew 17 comment lines ABOVE none of
      its pinned sites (`:22`, `:36`, both before the whitelist). The five `processor/agentic-loop/component.go`
      pins into `handlers.go` that L2 re-derived arrive with L2's own commit and resolve here too
- [x] 11.4 Re-based again onto L2's docs head `91242905` (backup ref
      `refs/backup/gh1329-pre-l2docs-rebase-20260920` = `cad02887`). 24 commits, NO conflict: that round is
      documentation and comments only, and the one comment block this change rewrites was already resolved to
      this layer's text at 11.1. Nothing in this change restates L2's one-outstanding-request SHALL or pins the
      two lines its new residual names, so no text here goes stale with it. `go build ./...` 0 and
      `go test -race -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0 on the
      rebased head
- [x] 11.5 Re-based again onto L2's test head `d127adeb` (backup ref
      `refs/backup/gh1329-pre-l2tests-rebase-20260920` = `d0c70e80`). 25 commits, NO conflict: that round is
      test-only — two new observations in `superseded_response_test.go` and `continuation_deferral_test.go` plus
      its own § 12.5 record — and this change touches neither file, nor `state.go`, `handlers.go` or `metrics.go`,
      which is where their subjects and their mutations live. One real coupling, and it holds: L2's new
      superseded-response fixture builds its component through `releaseTestComponent`, the helper § 6 here
      extended to stamp declared output ports, and it passes on the rebased head. `go build ./...` 0,
      `go test -race -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/` 0, `task test` 0,
      `task lint` 0, `task spec:properties` 262/262, `task openspec:validate` 57/57, no schema drift
- [x] 11.6 Re-based again onto L2's round-3 head `4f91f32d` (backup ref
      `refs/backup/gh1329-pre-l2round3-rebase-20260920` = `48bbbcb1`), 26 commits, head `48fef98d`. TWO
      conflicts, both the predicted approval-handler pair. `processor/agentic-dispatch/http.go`: L2 round 3
      requires `execution_id` on `ApprovalRequest` and refuses a mismatch against the gate the tracker reports;
      this change reads the gate from durable state instead. Resolved to BOTH — L2's required-field 400 (it runs
      before any state read, so it is layer-independent), then this change's `loadPersistedLoop` → 503
      unreadable → 409 not-awaiting, and only then the identity check, now against `gated.ExecutionID` off the
      persisted `PendingApproval`. Order matters and is deliberate: a stale POST at a loop that has since
      completed answers 409 "loop not awaiting approval", not 409 "execution is not the approval pending", and
      neither reaches a publish
- [x] 11.7 `approvalIdentityMismatch` changed signature in the resolution, `(requested string, pending
      PendingApprovalInfo)` → `(requested, pending string)`: L2's argument is the tracker projection this change
      deletes. It stays a named function rather than an inline comparison so the two layers keep one shape. Its
      `pending != ""` arm is L2's and is UNREACHABLE here — the incoherence check above already answers 503 for a
      record awaiting approval with an empty `ExecutionID` (`approval_handler_test.go`'s "pending without
      execution identity is unreadable" row pins it) — which is also why L2's third test,
      `TestApprovalAgainstAGateWithNoExecutionIdentityIsAccepted`, was DROPPED in the resolution rather than
      re-homed: on durable authority that world cannot be built through the handler
- [x] 11.8 L2's other two P1-2 tests re-homed onto durable authority in the same resolution:
      `TestStaleApprovalPOSTIsRefusedAgainstTheGateThatIsPending` and
      `TestApprovalWithoutAnExecutionIdentityIsRefused` now build their worlds with a local `gatedRecord(loopID,
      callID, executionID)` and `withPersistedLoops`, drive the production mux through `RegisterHTTPHandlers`,
      and still assert 409/400 with nothing published. Every body in `approval_handler_test.go` gained the
      required `execution_id`, naming `approvalTestExecutionID`, and the e2e approval walk sends
      `gatedExecutionID` (`test/e2e/scenarios/agentic/approval_signal.go:222`) so the 200 path keeps proving the
      echo
- [x] 11.9 Gates on `48fef98d`, the head this record then commits on top of, docs only:
      `go build ./...` 0, `task lint` 0, `task test` 155 ok / 0 FAIL / 0 cached,
      `go test -race -count=1 ./agentic/... ./processor/agentic-loop/... ./processor/agentic-dispatch/...` 0,
      `go vet -tags=e2e ./test/e2e/...` and `go vet -tags=integration ./processor/agentic-dispatch/` 0,
      `task spec:properties` 266/266, `task openspec:validate` 57/57, `task schema:generate` no drift
- [x] 11.10 Re-based again onto L2's round-4 head `86462b52` (backup ref
      `refs/backup/gh1329-pre-l2round4-rebase-20260920` = `077ea84f`). 27 commits, **no conflict**. That round is
      the owner's ceiling ruling plus records: it edits `processor/agentic-loop/{handlers.go,state.go,
      continuation_deferral_test.go}`, L2's own change directory, and the `execution_id` subsection of the
      migration note — none of which this change touches, and the migration sections this change adds sit
      elsewhere in that file. One coupling checked rather than assumed: L2 deletes
      `LoopManager.ClearPendingContinuation` with the behaviour it implemented, and nothing under
      `processor/agentic-dispatch/` references it or `PendingContinuation` at all (grep, stderr visible), so the
      retirement crosses the layer boundary without touching this one. `git diff 077ea84f b34a24d0` is exactly
      L2's seven files, +358/-37
- [x] 11.11 Gates on `b34a24d0`: `go build ./...` 0; `task lint` 0; `task test` 155 `ok` / 0 `FAIL`;
      `go test -race -count=1 ./agentic/... ./processor/agentic-loop/... ./processor/agentic-dispatch/...` 0;
      `go vet -tags=integration ./processor/agentic-dispatch/` and `go vet -tags=e2e ./test/e2e/...` 0;
      `openspec validate durable-loop-authority --strict` 0; `task openspec:validate` 57/57;
      `task spec:properties` **267/267**; `task schema:generate` no drift
- [x] 11.12 Re-based again onto L2's round-5 docs head `7069ea0a` (backup ref
      `refs/backup/gh1329-pre-l2round5-rebase-20260920` = `81a5cabb`). 28 commits, **no conflict**: that round
      edits only L2's own `design.md` and `tasks.md` — the duplicate terminal-delivery residual it declares is
      `agentic-loop`'s tool lane, and this change touches neither that lane nor those files. One line of it
      points HERE: L2's pin for "the durable reader skips terminal entities" is now qualified as living in this
      branch's tree, `activeLoop`'s `entity.State.IsTerminal()` conjunct at
      `processor/agentic-dispatch/http_activity.go:329`, which still reads back exactly on the rebased head.
      Gates: `go build ./...` 0; `task lint` 0;
      `go test -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0;
      `openspec validate durable-loop-authority --strict` 0; `task openspec:validate` 57/57;
      `task spec:properties` **267/267**
- [x] 11.13 Re-based again onto L2's round-6 head `8f1b2e4c` (backup ref
      `refs/backup/gh1329-pre-l2round6-rebase-20260920` = `015141c6`). 29 commits, **no conflict**. That round
      answers the owner's Codex round 3: a terminal tool whose assistant batch still had queued siblings carried
      a request with no tool messages at all, because `RepairToolPairs` drops a group with one unanswered call.
      It is entirely inside `agentic-loop`'s tool phase — `handlers.go`, its test, L2's spec delta and records —
      and this change touches none of it. Checked rather than assumed: nothing under `processor/agentic-dispatch/`
      references `synthesizeToolFailure`, `ClearQueuedTools`, `DequeueToolCall` or `RepairToolPairs`.
      Gates on `99d50874`: `go build ./...` 0; `task lint` 0;
      `go test -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0;
      `go test -race -count=1` over the same three 0; `openspec validate durable-loop-authority --strict` 0;
      `task openspec:validate` 57/57; `task spec:properties` **268/268**
- [x] 11.14 Re-based again onto L2's round-7 docs head `7e93ea22` (backup ref
      `refs/backup/gh1329-pre-l2round7-rebase-20260920` = `f445e4b5`). 30 commits, **no conflict**: that round
      closes three review NITs in L2's own `design.md` and `tasks.md` — two drifted `agentic-loop` pins, an
      honesty qualification on the tier precheck, and a new residual about `GetAndClearToolResults` returning
      tool results in map order. None of it is code and none of it names a file this change touches.
      Gates on `4507fa8d`: `go build ./...` 0; `task lint` 0;
      `go test -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0;
      `openspec validate durable-loop-authority --strict` 0; `task openspec:validate` 57/57;
      `task spec:properties` **268/268**
- [x] 11.15 Re-based again onto L2's round-8 head `4e7ddaec` (backup ref
      `refs/backup/gh1329-pre-l2round8-rebase-20260920` = `e5eb13e2`). 31 commits, **no conflict**. That round
      answers the owner's Codex round 4: L2's own defensive constant truncated the skipped-sibling drain at 256,
      so a 258-call batch left one call unanswered and `RepairToolPairs` dropped the group again. The bound is
      now the queue's own length, read through a new `LoopManager.QueuedToolCount`. All of it is inside
      `agentic-loop` — `handlers.go`, `state.go`, its test and L2's records — and nothing under
      `processor/agentic-dispatch/` references either symbol (grep, exit 1, stderr visible).
      Gates on `50cc38b2`: `go build ./...` 0; `task lint` 0;
      `go test -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0;
      `openspec validate durable-loop-authority --strict` 0; `task openspec:validate` 57/57;
      `task spec:properties` **269/269**
- [x] 11.16 Re-based again onto L2's round-9 head `cf59499b` (backup ref
      `refs/backup/gh1329-pre-l2round9-rebase-20260921` = `7cbcf3e4`). 32 commits, **no conflict**. That round
      fixes the residual L2's own round 8 recorded rather than shipped: `dispatchedFromQueue` bounded its
      dispatch drain at `len(GetPendingTools)+64`, a number from a different set than the one it drains, so a
      batch whose queued calls all fail to dispatch stopped at 64 and left the rest queued and unanswered. It is
      now bounded by `QueuedToolCount`, the accessor round 8 added. All of it is inside `agentic-loop` —
      `handlers.go`, a new `dispatch_drain_test.go`, and L2's records — and nothing under
      `processor/agentic-dispatch/` references `dispatchedFromQueue` or `QueuedToolCount` (grep, exit 1, stderr
      visible).
      Gates on `32f9b513`: `go build ./...` 0; `task lint` 0;
      `go test -count=1 ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0;
      `openspec validate durable-loop-authority --strict` 0; `task openspec:validate` 57/57;
      `task spec:properties` **270/270** (L2's 235 plus this change's own 35 — the round's new citation arrives
      tracked here, so it is counted)
- [x] 11.17 Re-based again onto L2's round-10 head `1bfac84d` (backup ref
      `refs/backup/gh1329-pre-l2round10-rebase-20260921` = `31306dc5`). 33 commits, **no conflict**. That round
      is L2's answer to its internal review round 6 (APPROVE, 1 MEDIUM, 2 NIT) and changes **no code**: both
      queue-drain bounds justified themselves with a false premise — `DequeueToolCall` is not the only shrinker
      and the lanes are not serialized per loop — so the comments now say the entry length bounds that drain
      only and the post-drain re-read is a reachable guard, not an assertion. Two `§ 16` pins re-derived and one
      sweep claim narrowed. Nothing under `processor/agentic-dispatch/` is touched by it (`git diff --stat` on
      the commit is `handlers.go` comments and L2's own records). Gates on `4e0770e5`, the rebased head this
      record commits on top of (docs only): `go build ./...` 0; `task lint` 0; `go test -count=1
      ./processor/agentic-loop/ ./processor/agentic-dispatch/ ./agentic/` 0; `openspec validate
      durable-loop-authority --strict` 0; `task openspec:validate` 57/57; `task spec:properties` 270/270
- [x] 11.18 **Re-based onto `main` — L2 has landed.** PR #1335 squash-merged as `fbd3c173` (`feat(agentic)!:`), #1328
      closed, `openspec/changes/archive/2026-09-21-stable-request-identity/` on main, and the
      `claude/gh1328-stable-identity` branch deleted; #1338 was retargeted to `main` before the merge. Backup
      ref `refs/backup/gh1329-pre-main-rebase-20260921` = `99417cc1`, then `git rebase --onto origin/main
      1bfac84d` — 34 commits (this change's 33 plus the round-10 rebase record), **no conflict**, no code hunk
      touched. Every § 8-11 line above that names `origin/claude/gh1328-stable-identity` is kept as written: it
      records a command that was run against a branch that existed then. **The MODIFIED block needed
      reconciling, and did not announce itself.** `openspec validate durable-loop-authority --strict` passes
      both before and after, so the check that caught it was a `diff` of this change's restated requirement
      against the now-current `openspec/specs/agentic-dispatch/spec.md`. L2's later review rounds had moved its
      own delta after this block was written, so the block was restating an older L2 text: it was missing the
      retry rule's unaccounted-attempt extension AND the whole scenario "A resolved cancel's signal publish
      fails without proving refusal". A MODIFIED block that omits a scenario deletes it. Both are folded
      forward, the preamble now diffs byte-identical against the baseline, and every remaining difference is one
      of the four scenario edits this change owns. The one non-verbatim fold is the re-added scenario's hazard
      clause, which would otherwise have contradicted the route-scoping this change states one scenario earlier;
      `design.md` § "What the MODIFIED block restates, and what the L2 merge changed in it" is rewritten to
      record all of it, and its "PR #1338 stays based on …" line is replaced. Gates on the content that ships —
      the rebased head `60333285` plus this commit's reconciliation, measured in the working tree before it was
      committed, so the only later change is this record's own markdown: `go build ./...` 0; `task lint` 0;
      `task test` **155 `ok` / 0 `FAIL`**; `go test -race -count=1 ./agentic/... ./processor/agentic-loop/...
      ./processor/agentic-dispatch/...` 0; `go vet -tags=integration ./processor/agentic-loop/
      ./processor/agentic-dispatch/` 0; `openspec validate durable-loop-authority --strict` 0; `task
      openspec:validate` 56/56 (55 specs plus this change — L2's is archived now and archives are not
      validated); `task spec:properties` 270/270; `task schema:generate` + `git status --porcelain schemas/
      specs/` 0, empty; `git diff --check` 0
