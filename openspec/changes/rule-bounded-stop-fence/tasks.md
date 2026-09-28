## 1. Inventory

- [x] 1.1 `inventory-processor.md` at `7a91400a` — Stop path waits, 16 lifecycle fields × writer/reader/guard/goroutine, consumers, tests (`task inventory:verify`: pins=35 ok=35)
- [x] 1.2 `inventory-cron-and-lanes.md` at `7a91400a` — CronScheduler waits and fields, the lane shape repo-wide, sibling idioms, sanctioned-idiom search (`task inventory:verify`: pins=154 ok=154)

- [x] 1.3 Independent inventory review → `INVENTORY PASS` (architect contract step 3) — NOT RUN; waived by the owner 2026-09-28 (docket 1 Q0 (a))

## 2. Design

- [x] 2.1 `design.md` — § 0 rulings verbatim, premises pinned, options with costs, docket cheaper-row-first, acceptance, residuals to file; § 7 coordinator amendments A0–A2
- [x] 2.2 Owner read of the docket; rulings transcribed into § 0 — Owner read 2026-09-28 — Q0 (a), Q-A (2), Q-C (3)

## 3. Implementation (rows from design § 6, amended by § 7; the owner's docket rulings select 3.1/3.2's form)

- [x] 3.1 Q-A: `processor/rule/owner_lane.go` — one unexported lane with close-under-mutex; `Processor` and `CronScheduler` adopt it; `lifecycle_owner_test.go` / `lifecycle_runtime_test.go` field reads updated (if the owner rules Q-A(a): the two in-place moves + fence nil-done parity instead) — commit `4be7cf1e`; T1/T1b in `owner_lane_test.go`
- [x] 3.2 Q-C: `CronScheduler.Stop(ctx context.Context) error`; `cleanup` calls it at step 2 where `:1343-1345` is (A1); delete `cronStopContext`; 10 test sites (if the owner rules Q-C(a): one doc line instead) — commit `fd0af722`; 9 test sites + `cleanup`; `TestCronScheduler_StopSettlesWhenBarrierRacesDispatcherExit` (T3), `TestCronScheduler_StopRejectsNilContext`
- [x] 3.3 Q-B/Q-D doc sentences: `ApplyConfigUpdate`, `UpdateWatchBuckets`, lane `submit`; one comment at the `hotReloadMgr.Stop()` call — commit `e0345f3a`
- [x] 3.4 Q-E: `messageCache` under `rp.mu` at all four sites; `startManagedEntityWatcher` refuses on nil `wg` — commit `06e2de51`; the failed-Start rollback also clears `messageCache` under `rp.mu` (it cleared it through `clearLifecycleHandles` before)
- [x] 3.5 Tests T1–T6 (design § 4) with mutation rows in the PR body (`cp` + checksum) — T1, T1b, T2, T3, T4, T5 green under `go test -race ./processor/rule -run 'TestRuleRuntime|TestRuleStop|TestRuleMessageCache|TestRuleManaged|TestCronScheduler' -count=20`; each mutation row failed its test at `-race -count=3` and was restored by `cp` with matching md5; T6 rewrote the standalone settlement test with synctest; the Register-serialization test keeps a mutex spin because mutex waits are not durably blocking under synctest
- [x] 3.6 Spec delta: `openspec/specs/rule-engine/spec.md` ADDED requirement phrased per A2; `docs/operations/migration-rule-stop-bounded.md` (Q-C row: 0 adopters found) — `openspec validate rule-bounded-stop-fence --strict` valid; migration note is `docs/operations/migration-beta164-to-beta165.md` (repo naming convention), not `migration-rule-stop-bounded.md`
- [x] 3.7 Design § 5 residuals FILED 2026-09-28 by the coordinator after the owner's docket read: #1410 (rule.Processor under StandardLifecycleTests; v1.0.0-beta.165), #1411 (30-file lifecycle copy-paste → framework decision; v1.0.0-rc.1)
- [ ] 3.8 `semstreams-reviewer` round; `task check:push`; `scripts/run-integration-tests.sh ./processor/rule/...` at the final code revision; `task inventory:verify` both files; archive as the last content commit
