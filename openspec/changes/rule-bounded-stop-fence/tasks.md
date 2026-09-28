## 1. Inventory

- [x] 1.1 `inventory-processor.md` at `7a91400a` — Stop path waits, 16 lifecycle fields × writer/reader/guard/goroutine, consumers, tests (`task inventory:verify`: pins=35 ok=35)
- [x] 1.2 `inventory-cron-and-lanes.md` at `7a91400a` — CronScheduler waits and fields, the lane shape repo-wide, sibling idioms, sanctioned-idiom search (`task inventory:verify`: pins=154 ok=154)

## 2. Design

- [ ] 2.1 `design.md` — § 0 rulings verbatim, premises pinned, options with costs, docket cheaper-row-first, acceptance, residuals to file
- [ ] 2.2 Owner read of the docket; rulings transcribed into § 0

## 3. Implementation (rows are filled from the accepted design)

- [ ] 3.1 The fix, per the accepted option
- [ ] 3.2 Deterministic `synctest` tests with mutation evidence rows
- [ ] 3.3 Independent review (`semstreams-reviewer`)
- [ ] 3.4 `scripts/run-integration-tests.sh ./processor/rule/...` green at the final code revision
