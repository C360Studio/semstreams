# Rule test cleanup implementation evidence — #1428 / draft #1429

Temporary evidence paths are retained by basename in `evidence/implementation-evidence.zip`.
The later `evidence/vet-correction-evidence.zip` supersedes its implementation report and rule source manifest
and contains the final external-test correction evidence. Adjacent manifests record member hashes.
The final 16-file source identity is `evidence/implementation-source-current.json`.

Source freeze: 2026-09-29 after all owner/proof edits and exact mutation restoration. The 16-file SHA256 source manifest is `/private/tmp/gh1428-rule-source-manifest.json`; its B/H rows pin current source, not the accepted historical inventory. Accepted inventory/ledger remain unchanged (`047be916`, design checkpoint `e185dd8b`, coordinator acceptance `0d888375`). Only `processor/rule` test sources changed. The root agent owns the reviewed cleanup baseline, broad guard, OpenSpec, commit/push and PR steps.

## Conformance of the accepted B/H population

B00–B23 are the 24 exact legacy cleanup identities. The owner column points to concrete acquisition or, for B04, the already-owned scheduler's Start. The finalizer column points to provisional helper cleanup or caller lexical cleanup; B04 delegates finalization to every H01–H24 caller. H01–H37 are all 37 accepted physical helper calls. For scheduler H01–H24 the owner finalizer is installed *before* the helper Start; for returning helpers H25–H37 it is the first action *after* receipt. The old `Stop(context.Background())` cleanup identities are absent from these B roots. Deliberate Stop/abort/repeat probes and the separate debounce no-resource-leak adjacency retain their original native calls.

| Case | Accepted declaration | Concrete owner | Lexical/provisional finalizer |
|---|---|---|---|
| B00 | newRunScopeHarness | processor/rule/actions_run_scope_integration_test.go:126 | processor/rule/actions_run_scope_integration_test.go:127 |
| B01 | startCronProcessorForTest | processor/rule/cron_scheduler_integration_test.go:84 | processor/rule/cron_scheduler_integration_test.go:85 |
| B02 | TestCronScheduler_StartActuallyFiresFromRobfig | processor/rule/cron_scheduler_test.go:592 | processor/rule/cron_scheduler_test.go:593 |
| B03 | TestCronScheduler_StartTwiceFails | processor/rule/cron_scheduler_test.go:235 | processor/rule/cron_scheduler_test.go:236 |
| B04 | startSchedulerForTest | processor/rule/cron_scheduler_test.go:96 | caller H01–H24 |
| B05 | TestIntegration_Processor_DebounceNonZero_CoalescingSetCreated | processor/rule/entity_watcher_debounce_integration_test.go:225 | processor/rule/entity_watcher_debounce_integration_test.go:228 |
| B06 | TestIntegration_Processor_DebounceZero_ConfigValidation | processor/rule/entity_watcher_debounce_integration_test.go:365 | processor/rule/entity_watcher_debounce_integration_test.go:368 |
| B07 | TestIntegration_Processor_DebounceZero_EdgeCases | processor/rule/entity_watcher_debounce_integration_test.go:440 | processor/rule/entity_watcher_debounce_integration_test.go:443 |
| B08 | TestIntegration_Processor_DebounceZero_ImmediateProcessing | processor/rule/entity_watcher_debounce_integration_test.go:123 | processor/rule/entity_watcher_debounce_integration_test.go:126 |
| B09 | TestIntegration_Processor_DebounceZero_NoCoalescingSet | processor/rule/entity_watcher_debounce_integration_test.go:66 | processor/rule/entity_watcher_debounce_integration_test.go:69 |
| B10 | TestIntegration_Processor_DebounceZero_NoTickerSpinning | processor/rule/entity_watcher_debounce_integration_test.go:172 | processor/rule/entity_watcher_debounce_integration_test.go:175 |
| B11 | TestIntegration_Processor_DebounceZero_Transition | processor/rule/entity_watcher_debounce_integration_test.go:312 | processor/rule/entity_watcher_debounce_integration_test.go:315 |
| B12 | TestEntityWatcherHardeningRealNATS | processor/rule/entity_watcher_hardening_integration_test.go:58 | processor/rule/entity_watcher_hardening_integration_test.go:61 |
| B13 | TestEntityWatcher_BoundedEvaluations | processor/rule/entity_watcher_integration_test.go:410 | processor/rule/entity_watcher_integration_test.go:413 |
| B14 | TestEntityWatcher_RuleTriggerDebouncing | processor/rule/entity_watcher_integration_test.go:111 | processor/rule/entity_watcher_integration_test.go:114 |
| B15 | TestIntegration_DynamicRuleCRUD | processor/rule/rule_integration_test.go:246 | processor/rule/rule_integration_test.go:249 |
| B16 | TestIntegration_DynamicWatchPatterns | processor/rule/rule_integration_test.go:553 | processor/rule/rule_integration_test.go:556 |
| B17 | TestIntegration_GraphIntegration | processor/rule/rule_integration_test.go:704 | processor/rule/rule_integration_test.go:707 |
| B18 | TestIntegration_KVEntityStateWatch | processor/rule/rule_integration_test.go:133 | processor/rule/rule_integration_test.go:136 |
| B19 | TestIntegration_PrometheusMetrics | processor/rule/rule_integration_test.go:453 | processor/rule/rule_integration_test.go:456 |
| B20 | TestIntegration_TransitionOperator_UpdateKV | processor/rule/rule_integration_test.go:865 | processor/rule/rule_integration_test.go:868 |
| B21 | TestEntityWatcher_DeletedEntityCleansRuleState | processor/rule/state_cleanup_integration_test.go:43 | processor/rule/state_cleanup_integration_test.go:46 |
| B22 | TestStatefulEvaluator_Integration | processor/rule/stateful_integration_test.go:29 | processor/rule/stateful_integration_test.go:32 |
| B23 | newRevisionClaimHarness | processor/rule/triple_mutator_revision_integration_test.go:70 | processor/rule/triple_mutator_revision_integration_test.go:71 |

| Caller | Accepted declaration | Current helper call | Owner finalizer |
|---|---|---|---|
| H01 | TestCronScheduler_FireDispatchesAllActions | processor/rule/cron_scheduler_test.go:346 | processor/rule/cron_scheduler_test.go:334 |
| H02 | TestCronScheduler_GraphGuardBlocksDispatch | processor/rule/cron_scheduler_test.go:388 | processor/rule/cron_scheduler_test.go:387 |
| H03 | TestCronScheduler_FireFireEveryNGate | processor/rule/cron_scheduler_test.go:419 | processor/rule/cron_scheduler_test.go:411 |
| H04 | TestCronScheduler_FireCooldownGate | processor/rule/cron_scheduler_test.go:444 | processor/rule/cron_scheduler_test.go:436 |
| H05 | TestCronScheduler_FireInflightGuard | processor/rule/cron_scheduler_test.go:470 | processor/rule/cron_scheduler_test.go:463 |
| H06 | TestCronScheduler_FirePanicRecover | processor/rule/cron_scheduler_test.go:503 | processor/rule/cron_scheduler_test.go:493 |
| H07 | TestCronScheduler_FireUnknownRuleIsNoop | processor/rule/cron_scheduler_test.go:521 | processor/rule/cron_scheduler_test.go:520 |
| H08 | TestCronScheduler_FireSurvivesActionError | processor/rule/cron_scheduler_test.go:548 | processor/rule/cron_scheduler_test.go:538 |
| H09 | TestCronScheduler_FirePersistsLastFiredRecord | processor/rule/cron_scheduler_test.go:675 | processor/rule/cron_scheduler_test.go:670 |
| H10 | TestCronScheduler_FireWithNilTrackerNoops | processor/rule/cron_scheduler_test.go:709 | processor/rule/cron_scheduler_test.go:704 |
| H11 | TestCronScheduler_FireOverwritesPriorRecord | processor/rule/cron_scheduler_test.go:729 | processor/rule/cron_scheduler_test.go:724 |
| H12 | TestCronScheduler_RestoreFromTracker_EnforcesCooldownAcrossRestart | processor/rule/cron_scheduler_test.go:865 | processor/rule/cron_scheduler_test.go:858 |
| H13 | TestCronScheduler_FireBuildsScheduleContext | processor/rule/cron_scheduler_test.go:901 | processor/rule/cron_scheduler_test.go:893 |
| H14 | TestCronScheduler_FireScheduleContextSeesPriorFire | processor/rule/cron_scheduler_test.go:938 | processor/rule/cron_scheduler_test.go:933 |
| H15 | TestCronScheduler_Metrics_FireSuccessRecorded | processor/rule/cron_scheduler_test.go:1034 | processor/rule/cron_scheduler_test.go:1029 |
| H16 | TestCronScheduler_Metrics_FireErrorRecorded | processor/rule/cron_scheduler_test.go:1059 | processor/rule/cron_scheduler_test.go:1054 |
| H17 | TestCronScheduler_Metrics_FirePanicRecordedAsPanic | processor/rule/cron_scheduler_test.go:1088 | processor/rule/cron_scheduler_test.go:1081 |
| H18 | TestCronScheduler_Metrics_CooldownSkippedRecorded | processor/rule/cron_scheduler_test.go:1112 | processor/rule/cron_scheduler_test.go:1105 |
| H19 | TestCronScheduler_Metrics_FireUpdatesNextFireGauge | processor/rule/cron_scheduler_test.go:1143 | processor/rule/cron_scheduler_test.go:1136 |
| H20 | TestCronScheduler_Metrics_PanicWinsOverPartialSuccess | processor/rule/cron_scheduler_test.go:1199 | processor/rule/cron_scheduler_test.go:1188 |
| H21 | TestCronScheduler_Metrics_CooldownSkippedDoesNotUpdateNextFire | processor/rule/cron_scheduler_test.go:1266 | processor/rule/cron_scheduler_test.go:1259 |
| H22 | TestCronScheduler_Metrics_InflightSkippedRecorded | processor/rule/cron_scheduler_test.go:1301 | processor/rule/cron_scheduler_test.go:1296 |
| H23 | TestCronFire_DenyShortCircuits | processor/rule/cron_scheduler_test.go:1416 | processor/rule/cron_scheduler_test.go:1405 |
| H24 | TestCronFire_DeniedStatusDistinctFromError | processor/rule/cron_scheduler_test.go:1450 | processor/rule/cron_scheduler_test.go:1440 |
| H25 | TestIntegration_CronRule_FiresOnSchedule | processor/rule/cron_scheduler_integration_test.go:134 | processor/rule/cron_scheduler_integration_test.go:135 |
| H26 | TestIntegration_CronRule_PersistsLastFiredToKV | processor/rule/cron_scheduler_integration_test.go:185 | processor/rule/cron_scheduler_integration_test.go:186 |
| H27 | TestIntegration_CronRule_DetectsMissedFiresAcrossRestart / first | processor/rule/cron_scheduler_integration_test.go:256 | processor/rule/cron_scheduler_integration_test.go:257 |
| H28 | TestIntegration_CronRule_DetectsMissedFiresAcrossRestart / second | processor/rule/cron_scheduler_integration_test.go:288 | processor/rule/cron_scheduler_integration_test.go:289 |
| H29 | TestIntegration_CronRule_CooldownAcrossRestart / first | processor/rule/cron_scheduler_integration_test.go:348 | processor/rule/cron_scheduler_integration_test.go:349 |
| H30 | TestIntegration_CronRule_CooldownAcrossRestart / second | processor/rule/cron_scheduler_integration_test.go:363 | processor/rule/cron_scheduler_integration_test.go:364 |
| H31 | TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite | processor/rule/actions_run_scope_integration_test.go:211 | processor/rule/actions_run_scope_integration_test.go:212 |
| H32 | TestRunScopeNewForEachOnOneImportCountsPerDispatchNotPerEntity | processor/rule/actions_run_scope_integration_test.go:289 | processor/rule/actions_run_scope_integration_test.go:290 |
| H33 | TestForeignFiringSkipLogNamesEveryDeclinedWrite | processor/rule/actions_run_scope_integration_test.go:367 | processor/rule/actions_run_scope_integration_test.go:368 |
| H34 | TestForeignFiringSkipLogSurvivesAPublishFailure | processor/rule/actions_run_scope_integration_test.go:410 | processor/rule/actions_run_scope_integration_test.go:411 |
| H35 | TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin | processor/rule/actions_run_scope_integration_test.go:435 | processor/rule/actions_run_scope_integration_test.go:436 |
| H36 | TestTripleMutator_SuppressedAddDoesNotClaimAnotherWritersRevision | processor/rule/triple_mutator_revision_integration_test.go:163 | processor/rule/triple_mutator_revision_integration_test.go:164 |
| H37 | TestTripleMutator_NoOpRemoveDoesNotClaimAnotherWritersRevision | processor/rule/triple_mutator_revision_integration_test.go:197 | processor/rule/triple_mutator_revision_integration_test.go:198 |


## Owner contract and actual native proof

`test_owner_support_test.go` contains private concrete Processor and CronScheduler receivers. `test_graph_ingest_owner_integration_test.go` and `test_owner_external_integration_test.go` contain the corresponding graph-ingest and external `rule_test` adapters. Each owner records `attempted` before its concrete Stop, keeps only a private Start cancel function, supplies a fresh five-second detached terminal child, joins native return with terminal/operation context errors, and runs Stop synchronously before private cancellation. Returning helpers add `provisionalFinish`/`transfer`. The external adapter is reached by the executed external cases below. No production/exported surface or generic Stop registry was added.

| Assertion / activation | Native seam and observed signal | Executed evidence |
|---|---|---|
| Processor setup escape | `TestProcessorTestOwnerProvisionalAndTransferredNativeStop/setup_escape`: a non-transferred concrete Processor reaches native `Processor.Stop`; `attempted` and terminal state observed. | Final focused race unit log below. |
| Processor transfer and substrate order | `caller_lexical_finalizer`: accepted native command completes; deferred owner finalizer closes Processor lane and reaches terminal state before an outer substrate marker. | Final focused race unit log below. |
| Processor admitted work and cancellation order | `TestProcessorTestOwnerStopSettlesAdmittedCommandBeforeStartCancel`: command callback admission, native command-lane fence, live callback context before release, submitter completion, Stop return and lane join. Failure exits release and join test-owned work using fresh finite cleanup authority. | Final focused race unit log below. |
| Scheduler admitted fire and cancellation order | `TestCronSchedulerTestOwnerJoinsAdmittedFireBeforeStartCancel`: real `CronScheduler.fire` invokes blocking executor; native Stop enters terminal phase while action remains admitted; private Start child remains live until release, then fire/Stop join and child cancels. | Final focused race unit log below. |
| Scheduler concrete error and once-only decision | `TestCronSchedulerTestOwnerExpiredStopIsFinalAttempt`: virtual five-second native Stop deadline under `synctest`, actual context error, fallback skips, second explicit owner Stop refused; test releases/joins its blocked fire. Expiry is not reported as a joined component. | Final focused race unit log below. |
| Actual returning helper setup escape | `TestRevisionClaimHarnessSetupEscapeRunsConcreteStop`: injected setup panic inside actual `newRevisionClaimHarness` after graph-ingest Start and before transfer; its provisional defer makes one native graph-ingest Stop, and operation authority remains live. | Selected canonical integration log below; omission mutant kills its attempted-state assertion. |
| External package adapter | Six `TestIntegration_*` external `rule_test` cases plus two external entity-watcher cases ran with real NATS; owner is acquired before Initialize and uses the accepted Start phase scope. | Selected canonical integration log below. |
| Explicit phase fences | Both cron restart cases observe first concrete Stop before stale record/second acquisition and the hardening case observes Stop before post-stop counters; fallbacks skip attempted owners. | Selected canonical integration log below. |

## Exact tests and activation

- Pre-change coordinator baseline (passing, not sensitivity): `go test -race ./processor/rule -run '^(TestCronScheduler_|TestCronFire_|TestRuleStopDeadlineArmCancelsAndJoinsCoordinator$)' -count=1 -timeout=60s`, `openspec/changes/rule-test-cleanup/review/evidence/prechange-rule-unit.json`.
- Final focused unit command: `go test -race ./processor/rule -run '^(TestCronSchedulerTestOwner|TestProcessorTestOwner|TestCronScheduler_|TestCronFire_|TestRuleStopDeadlineArmCancelsAndJoinsCoordinator|TestRuleMessageCacheOneGuard|TestRuleManagedWatcherSpawnRefusedAfterRuntimeEnd)' -count=1 -timeout=60s`; PASS, package 2.075s; `/private/tmp/gh1428-rule-final-focused-unit.log`. Includes #1283 owner-lane/deadline/cache/watcher regression names. No full repository race claim.
- Canonical selected integration command: `scripts/run-integration-tests.sh ./processor/rule -run '^(TestRunScope|TestForeignFiringSkipLog|TestTripleMutator_|TestRevisionClaimHarness|TestIntegration_CronRule_|TestIntegration_Processor_Debounce|TestEntityWatcherHardeningRealNATS|TestEntityWatcher_(RuleTriggerDebouncing|BoundedEvaluations|DeletedEntityCleansRuleState)|TestIntegration_(KVEntityStateWatch|DynamicRuleCRUD|PrometheusMetrics|DynamicWatchPatterns|GraphIntegration|TransitionOperator_UpdateKV)|TestStatefulEvaluator_Integration|TestIntegration_RuleReadiness_|TestIntegration_RuleStopAfterAcceptedStartParentCancellation)' -v`; PASS, 34 top-level passes, zero skips/fails, package 31.489s; `/private/tmp/gh1428-rule-selected-integration.log`. The script owns the host lock and real NATS. It covers the 21 integration-tagged B roots and H25–H37, both graph-ingest harness families, cron restarts, hardening, all eight external owner roots, #1062 readiness/accepted-parent cancellation, and B21/B22 state assertions. H01–H24 and B02–B04 execute in the focused unit command.
- Actual-helper baseline selection before its mutant: `scripts/run-integration-tests.sh ./processor/rule -run '^(TestIntegration_(KVEntityStateWatch|DynamicRuleCRUD|PrometheusMetrics|DynamicWatchPatterns|GraphIntegration|TransitionOperator_UpdateKV)|TestRevisionClaimHarnessSetupEscapeRunsConcreteStop)$'`; PASS, package 7.940s, `/private/tmp/gh1428-external-phase-focused.log`. Its selector explicitly includes `TestRevisionClaimHarnessSetupEscapeRunsConcreteStop`; the later selected `-v` run supplies the independently visible `--- PASS:` name after restoration.
- Tagged compile prior to broad selection: `scripts/run-integration-tests.sh ./processor/rule -run '^$'`, PASS/no tests executed; `/private/tmp/gh1428-rule-integration-compile2.log`. This is compile evidence only.

Executed selected top-level cases (from `--- PASS:` records):

- `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`
- `TestRunScopeNewForEachOnOneImportCountsPerDispatchNotPerEntity`
- `TestForeignFiringSkipLogNamesEveryDeclinedWrite`
- `TestForeignFiringSkipLogSurvivesAPublishFailure`
- `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`
- `TestIntegration_CronRule_FiresOnSchedule`
- `TestIntegration_CronRule_PersistsLastFiredToKV`
- `TestIntegration_CronRule_DetectsMissedFiresAcrossRestart`
- `TestIntegration_CronRule_CooldownAcrossRestart`
- `TestIntegration_Processor_DebounceZero_NoCoalescingSet`
- `TestIntegration_Processor_DebounceZero_ImmediateProcessing`
- `TestIntegration_Processor_DebounceZero_NoTickerSpinning`
- `TestIntegration_Processor_DebounceNonZero_CoalescingSetCreated`
- `TestIntegration_Processor_DebounceZero_Transition`
- `TestIntegration_Processor_DebounceZero_ConfigValidation`
- `TestIntegration_Processor_DebounceZero_EdgeCases`
- `TestIntegration_Processor_DebounceZero_NoResourceLeak`
- `TestEntityWatcherHardeningRealNATS`
- `TestEntityWatcher_DeletedEntityCleansRuleState`
- `TestStatefulEvaluator_Integration`
- `TestRevisionClaimHarnessSetupEscapeRunsConcreteStop`
- `TestTripleMutator_SuppressedAddDoesNotClaimAnotherWritersRevision`
- `TestTripleMutator_NoOpRemoveDoesNotClaimAnotherWritersRevision`
- `TestEntityWatcher_RuleTriggerDebouncing`
- `TestEntityWatcher_BoundedEvaluations`
- `TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo`
- `TestIntegration_RuleReadiness_NonEmptyReplayReportsScope`
- `TestIntegration_RuleStopAfterAcceptedStartParentCancellation`
- `TestIntegration_KVEntityStateWatch`
- `TestIntegration_DynamicRuleCRUD`
- `TestIntegration_PrometheusMetrics`
- `TestIntegration_DynamicWatchPatterns`
- `TestIntegration_GraphIntegration`
- `TestIntegration_TransitionOperator_UpdateKV`

## Targeted mutation sensitivity

PBT decision: deterministic named histories are sufficient for this ownership/transfer order contract, as accepted in design. The independent oracle is the existing `test-cleanup-policy` lexical ownership scenarios and controlled/abort distinction. Random action histories would add little activation assurance for these three precise faults. The shared #1419 and graph-ingest #1424 established lexical owner shape is linked at `openspec/changes/archive/2026-09-29-shared-lifecycle-test-cleanup/review/implementation-evidence.md` and `openspec/changes/archive/2026-09-29-graph-ingest-test-cleanup/review/implementation-review.md`; this batch independently tests its rule-specific concrete seams.

Every mutation edited one implementation/fixture-owner site while assertions stayed fixed. `cp` backup hashes matched source before mutation and restored source after. The actual helper mutation's proof file SHA256 stayed `582e1611d142c2f9820c500ec81f92fd962854e5525afccad0f090b73598bdd6`; both scheduler reruns' proof file SHA256 stayed `2fd252859cbc2e681aa91794505cd6243159308af2c9e25e5ee1709673c8b103`. No timeout, invalid compile, survivor or unjoined gate was counted as a kill.

| Fault | Mutant / exact patch | Intended assertion | Outcome and restoration |
|---|---|---|---|
| Omit actual helper provisional finalizer | `1e8652e9ddcf96f9de5ae5c81cd1d6e3f3c7b3ea9e183daa6e28b2dd0950c333`; `/private/tmp/gh1428-final-provisional_omission.patch` | `actual revision helper did not finish provisional concrete graph-ingest owner` | Compiled integration exit 1 at assertion; `/private/tmp/gh1428-mutant-provisional_omission.log`. Baseline/restored logs `/private/tmp/gh1428-external-phase-focused.log` and `/private/tmp/gh1428-rule-selected-integration.log`; source restored `426f8c3616f12b9b09a1f5acee955b3900807914fd3b01305035a2e510dc0e3c`. |
| pre_stop_start_cancel | `3e0189957d61a6cc30ae2fbb26763cba139aceffb41d66c6a143a39915319c9f`; `/private/tmp/gh1428-final-pre_stop_start_cancel.patch` | `Start child canceled before native Stop settled fire` | Compiled unit exit 1 at assertion; `/private/tmp/gh1428-final-pre_stop_start_cancel.log`. Baseline `/private/tmp/gh1428-scheduler-mutation-baseline.log`, restored `/private/tmp/gh1428-scheduler-mutation-restored.log` PASS; source restored `ffa9a2a8a1a0800f256982f260b973f08edc942b6fc1f9509c19d73a8c580bd8`. |
| attempt_only_on_success | `1ac6caa059aa0eaa92d5169649a9a29a3f3feae75d9697830677a3e43c9bfa63`; `/private/tmp/gh1428-final-attempt_only_on_success.patch` | `second explicit owner Stop` | Compiled unit exit 1 at assertion; `/private/tmp/gh1428-final-attempt_only_on_success.log`. Baseline `/private/tmp/gh1428-scheduler-mutation-baseline.log`, restored `/private/tmp/gh1428-scheduler-mutation-restored.log` PASS; source restored `ffa9a2a8a1a0800f256982f260b973f08edc942b6fc1f9509c19d73a8c580bd8`. |

Full machine-readable records: `/private/tmp/gh1428-final-helper-mutation.json`, `/private/tmp/gh1428-final-scheduler-mutations.json`; original one-at-a-time raw run is `/private/tmp/gh1428-mutations.json`. The two scheduler mutants were rerun after bounded proof-wait corrections. No repeated real-NATS helper mutant was needed because its source, assertion and proof SHA256 remained unchanged.

## Limits and remaining gates

Five seconds is a supplied cooperative Stop context, not a generic native wall-clock promise. Processor watcher Stop and cache Close are contextless; its post-deadline command-lane receive can still wait for a command ignoring its own context. The scheduler expiry witness deliberately does not claim component join after an error. `NewTestClient` keeps sole substrate cleanup, and shared clients/subscriptions now report termination/unsubscribe errors. The two cron restart observation subscriptions run through checked testing cleanup after lexical component finalization. Existing test sleep probes were retained as accepted adjacency, not used as new proof readiness signals.

This handoff does not assert the 297-to-273 baseline result, final reviewed-resolution count, actual cleanup guard, full `task check:push`, CI, archive, or merge. Root owns those checks and exact baseline edit after independent source/proof review. The accepted #1421 merge hold still applies to #1429. No issue was filed by this implementation slice.

## Tagged vet correction after the first broad gate

The initial `task check:push` stopped at `go vet -tags=integration ./...` before runtime tests. Vet identified six possible cancel-function leaks in external rule cases: a closure captured a cancel variable assigned after `Initialize`. The single-file correction uses an early `defer owner.finish(setupCtx, t)` before any fallible setup. After Initialize, each original 10/15-second execution context registers `defer cancel()` and then `defer owner.finish(executionCtx, t)`. LIFO runs the narrow finalizer before its cancel; the early finalizer then sees the attempted state and skips. A setup escape before the execution context exists uses the early outer finalizer. Outer I/O remains independent of private Start cancellation, and the original execution-phase budget boundary is retained.

| Case | Function | Owner acquired | Early setup finalizer | Execution cancel | Narrow finalizer |
|---|---|---|---|---|---|
| B15 | TestIntegration_DynamicRuleCRUD | processor/rule/rule_integration_test.go:246 | processor/rule/rule_integration_test.go:249 | processor/rule/rule_integration_test.go:262 | processor/rule/rule_integration_test.go:263 |
| B16 | TestIntegration_DynamicWatchPatterns | processor/rule/rule_integration_test.go:553 | processor/rule/rule_integration_test.go:556 | processor/rule/rule_integration_test.go:568 | processor/rule/rule_integration_test.go:569 |
| B17 | TestIntegration_GraphIntegration | processor/rule/rule_integration_test.go:704 | processor/rule/rule_integration_test.go:707 | processor/rule/rule_integration_test.go:720 | processor/rule/rule_integration_test.go:721 |
| B18 | TestIntegration_KVEntityStateWatch | processor/rule/rule_integration_test.go:133 | processor/rule/rule_integration_test.go:136 | processor/rule/rule_integration_test.go:150 | processor/rule/rule_integration_test.go:151 |
| B19 | TestIntegration_PrometheusMetrics | processor/rule/rule_integration_test.go:453 | processor/rule/rule_integration_test.go:456 | processor/rule/rule_integration_test.go:469 | processor/rule/rule_integration_test.go:470 |
| B20 | TestIntegration_TransitionOperator_UpdateKV | processor/rule/rule_integration_test.go:865 | processor/rule/rule_integration_test.go:868 | processor/rule/rule_integration_test.go:880 | processor/rule/rule_integration_test.go:881 |

`go vet -tags=integration ./processor/rule` PASS (exit 0), `/private/tmp/gh1428-rule-tagged-vet-after.log`. The canonical runner command `scripts/run-integration-tests.sh ./processor/rule -run '^(TestIntegration_(KVEntityStateWatch|DynamicRuleCRUD|PrometheusMetrics|DynamicWatchPatterns|GraphIntegration|TransitionOperator_UpdateKV)|TestRevisionClaimHarnessSetupEscapeRunsConcreteStop)$' -v` PASS: seven named top-level tests, zero skips/fails, package 9.270s; `/private/tmp/gh1428-vet-fix-external-integration.log`. Source SHA256: `af3e0fb27a9f3324ef50d5a9300dbcd56313625b3236d58e4387b31ef0349e5b`; original single-file backup: `/private/tmp/gh1428-rule-vet-before.bak` SHA256 `bd1555af8fa008af9c08da1ceb86502f4e30f0079d955c4338b5011770efab4e`. All three mutation sites and their assertion files are unchanged, so earlier mutation conclusions remain scoped to the same bytes. The earlier 34-case integration log remains historical evidence for the pre-vet-fix source; the seven-case rerun is the corrected-source evidence for B15–B20.
