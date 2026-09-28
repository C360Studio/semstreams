# Change: a bounded rule Stop must not block on an orphaned fence barrier — inventory first, then the fix

## Why

#1283: `openspec/specs/component-lifecycle/spec.md` requires that a running component's Stop be caller-bounded.
`processor/rule` violates it on main. The runtime-command lane in `processor.go` closes `coordinatorDone` only after
`failQueuedRuntimeCommands` has released `commandMu`; a `fenceRuntimeCommands` that takes the mutex inside that window
appends a barrier nothing will ever drain, and `settleRuntimeCommandFence`'s deadline arm then blocks on it forever.
`CronScheduler` carries a second copy of the same lane with the same window (`cron_scheduler.go`), and that copy is the
frame PR #1404's required Test job hung on for 20 minutes at `ada5c46a` (`processor.go:1394` waiting on `cronDone`,
`cron_scheduler.go:500` waiting on the barrier). #1274 fixed four ownership paths and said in its commit message that
it does not claim to fix these.

The owner widened the read on 2026-09-28: "rules and cron might be pretty rough when we start looking. let's look at
the processor top and down and ensure it's playing well in the framework using established patterns and idioms. we
have a lot of debt we are clearing from early work where agents were very willing to roll their own." The inventories
measure that: the serialized owner-lane shape exists exactly twice in the repository, both in `processor/rule`, both
from `c7ca5d0f` (2026-08-20); the only hand-rolled `context.Context` in the repository is `cronStopContext`; the
copy-pasted lifecycle state machine is in 30 non-test files and is the repository-wide idiom, not a rule-processor
defect. Neither `pkg/lifecycle` (workflow-entity harness) nor `pkg/dispatch` (parallel fan-out) is a serialized owner
lane, and `component/` carries no shared Stop helper.

## What changes

In order, each gated by the one before it:

1. Two line-pinned inventories at `7a91400a` (`inventory-processor.md`, `inventory-cron-and-lanes.md`), both green
   under `task inventory:verify`.
2. A design (`design.md`): the fix shape across both lanes and the hot-reload manager's Stop, the contract of the
   contextless submit paths, the cron scheduler's Stop signature, residuals to file rather than fix, and a
   deterministic test plan. Options carry costs; the cheaper row comes first in every docket question.
3. The implementation the accepted design names, with `synctest` tests that enter the deadline lane at zero
   wall-clock cost, an independent review, and the CI-faithful integration gate before merge.

Scope constraints carried from #1283: no supervisor, state machine, durable state, or public API.

## Impact

- `processor/rule` only. Any change to an exported signature on `CronScheduler` is named in the design as an
  exported-surface change with a migration row, never assumed safe from an in-repo caller count.
- Unblocks PR #1404 (#1188) docket 5 Q10, which asked the owner to waive or fix #1283 for that merge.
