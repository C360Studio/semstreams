# Change: agentic-loop committed terminal recovery — a committed terminal outcome governs later inputs and timers

> Change id `agentic-loop-committed-terminal-recovery`. It claims #1377. Base: `9e5d8455`. Parent epic: #1146
> (Lane A, step 4, last; design after #1376 at `9e5d8455`, implementation after the durable-accepted-input change,
> PR #1387).
>
> **Design phase.** This first commit is the claim. The design is owed in two steps: a probe that OBSERVES the
> recorded windows on production entry paths (developer, tests as evidence), then the per-window docket (architect):
> window · path (a/b/c) · existing durable fact used · what is guaranteed · what is only bounded. Independent design
> review and owner acceptance precede implementation.
>
> **Rulings applied, none reopened:**
> - #1146 issuecomment-5828511934 ruling 4: done-when bullet 3 reads as the epic's wording; the docket is, in order,
>   (a) the documented bound with no code, (b) the existing approval sweeper adopting a durable terminal it finds under
>   a gated record, (c) anything needing a new durable fact or coordination, which is its own owner question. A second
>   design round or any new durable authority stops and returns to the owner with option (a).
> - #1377 issuecomment-5828356994: the two #1366 residuals are the starting point for a stronger contract, not
>   retroactive defects of PR #1366. #1155 keeps its close-with-#1366 scope.
> - Codex caution (#1377 issuecomment-5838471641) and the coordinating session's read (issuecomment-5838780631): an
>   option-(a) window is an accepted limitation, not the epic's first exit clause; any (a) row is an explicit owner
>   amendment of that clause on #1146, never a quiet pass.

## Why

Four windows, all recorded on #1377 against `763b33dd`/`9e5d8455`, none yet observed by a test on the production entry
path:

- **W1, lost record CAS.** The terminal marker and event survive a record CAS lost to a newer request while the record
  stays live (#1366 accepted residual; `component.go` terminal handling).
- **W2, timer-driven terminal publication fails after commitment**, with no source delivery available to replay the
  work (#1366 accepted residual; `approval_sweeper.go`).
- **W3, held-loop cancel/approval race.** A cancel lands while the loop is held, `AddPendingTool` succeeds, and the
  approval result publishes a `tool.execute` for a cancelled loop and CAS-writes a cancelled record outside the
  terminal owner (#1377 issuecomment-5833255119). Reachable without process replacement; it breaks the epic's first
  exit clause outright, so it is a defect, not an edge case.
- **W4, benign cancel race quarantines the approval lane.** The sibling interleaving where the cancel released the loop
  first: `marshalLoopRecord` cannot read the released loop, the failure wraps Fatal and the delivery is quarantined,
  latching the lane on a benign race (same comment).

## What changes

- **Observed counterexamples first.** Lane-level tests drive W3 and W4 through the production approval and signal
  callbacks with explicit interleaving; their output is this change's inventory. If forcing the interleaving needs a
  production hook, that cost is recorded and returned to the owner before any design.
- **A per-window docket**, one row per window, listing option (a) first, the existing durable fact each (b) path would
  adopt (terminal marker, gated record), the bound, and the smallest-fix candidate for W3 at the existing seam.
- **Implementation of the accepted rows only**, last in Lane A, with the tests the epic's exit criteria require:
  terminal commitment plus concurrent request advancement, publication failure, record-CAS loss, process replacement,
  and successful controls.

## What does not change

No universal exactly-once external effects, active/active guarantee, indefinite reconstruction, full stream replay,
generic supervisor/outbox/checkpoint bucket, or recovery of every parked timer at startup (#1377 non-goals). No new
durable authority without an owner question (ruling 4). #1365/#1345 own accepted-input durability; #1374 owns terminal
reasons and accounting (its `loops_failed_total{reason}` / `active_loops` seam is this change's observation seam).

## Impact

- `processor/agentic-loop` only, serialized after PR #1387; BREAKING gate walks the approval path, so `task e2e:agentic`
  is required on the final diff (#1238's stages are on main for it).
- Spec and migration text replace the two #1366 residual sentences with the proved behaviour and the stated bounds;
  the epic's per-window table on #1146 is written from the same docket.
