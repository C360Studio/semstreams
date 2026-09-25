# Change: agentic-loop transition-result contract — one owner for `(result, error)` interpretation

> Change id `agentic-loop-transition-result`. It claims #1376. Base: `f15a528e`. Parent epic: #1146 (Lane A, step 2).
>
> **Design phase.** This first commit is the claim. The design is owed in two steps: a line-pinned inventory of the
> `(HandlerResult, error)` combinations the loop actually produces (explorer), then the contract table (architect).
> Independent review and owner acceptance of the table precede implementation (#1376 step 3). Implementation waits
> for #1374 to merge (ruling 1 below).
>
> **Rulings applied, none reopened:**
> - #1146 issuecomment-5828357926 (scope and placement, 2026-09-25) and issuecomment-5828511934 (rulings 1–5,
>   2026-09-25). Ruling 2 bounds this change: the contract is a table over measured combinations, one MODIFIED
>   requirement; a design pass that yields new requirements instead of rows stops and returns to the owner.
> - The L4b rulings on #1362 stand untouched, including the two recorded residuals; those are #1377's.

## Why

Three facts, verified at `763b33dd` and unchanged at `f15a528e`:

- **The `order` parameter of `persistHandlerResult` is dead.** `processor/agentic-loop/component.go:2245` returns on
  its terminal branch (`:2262`) before `order` is read (`:2283`), and every `writeThenPublish` caller —
  `approval_response_handler.go:221`, `approval_sweeper.go:123`, `component.go:2664` — sits behind
  `result.State.IsTerminal()`. Every non-terminal call is therefore `publishThenWrite`; the gate case is decided by
  the result's state, not the caller. The order a result takes is a property of the result, and the parameter
  pretends otherwise.
- **The "error with a terminal result" decision is copied, not owned.** The guard
  `err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere` appears verbatim at
  `approval_response_handler.go:210` and `approval_sweeper.go:113`, with a third variant in `settleFailedToolResult`
  (`component.go:2660`). Each site decides on its own that a business failure carrying a populated terminal result is
  the loop's settlement. A fourth lane that forgets the guard drops a terminal (L4b found exactly that on the approval
  lane and the sweeper).
- **`HandlerResult` carries independent flags and an optional terminal payload** (`handlers.go:59`: `State`,
  `RetryScheduled`, `MaxIterationsReached`, `Deferred`, `Created`, `CompletionState`, …). Which combinations a lane may
  return, and what each means for commitment and delivery disposition, is nowhere written down.

## What changes

- **The contract: a table, one MODIFIED requirement on `agentic-loop`.** Rows are the `(result, error)`
  combinations actually produced on the task, model, tool, approval, signal, verdict and timer entry paths, each with a
  file:line producer. Columns: business outcome, delivery disposition (ack / retry / quarantine), commitment
  (known / unknown), the one owner that commits, the publication-order obligation the result shape implies (birth,
  gate, ordinary advance, terminal), and replacement behaviour. The obligations for #1377 (terminal consistency),
  #1365 and #1345 (durable accepted input) are one row each; nothing is implemented for them here.
- **One private decision at the loop owner.** A helper interprets `(result, err)` once and the three sites call it.
  Typed errors and boundary validation where the table shows a combination that must not occur. No new exported API;
  `agentic` (Tier 1) is untouched.
- **`carrierOrder` and the `order` parameter are removed.** Birth, gate, ordinary-advance and terminal orders are
  preserved exactly; they are already decided by the result, and the tests that pin them stay.
- **#1374 keeps accounting.** Terminal metrics and failure reasons are #1374's, landing first; this change preserves
  whatever it lands.

## Not in this change

No universal state enum, generic recovery runtime, new mode or knob, checkpoint or outbox bucket, event-history replay
or Temporal migration. The public `LoopState` vocabulary stays with #1314 (RC). Framework-wide generalisation stays
with #1145/#1147 (beta.165). Stronger terminal recovery is #1377, after this change.

## Impact

- **Specs.** `agentic-loop` MODIFIED, one requirement whose scenarios are the table's rows.
- **Adopters.** None expected: no wire, KV or metric change. If the table finds a produced combination whose
  interpretation must change, that row is an owner question before implementation and a line in
  `docs/operations/migration-beta162-to-beta163.md`.
- **Breaking.** Not expected. If any row changes observable behaviour, `task e2e:agentic` gates the merge.

## Stop point

After the inventory and the table land on this branch, the design goes to independent review, then to the owner.
Implementation starts only after that acceptance and after #1374 has merged.

Links: #1376, #1146 (epic), #1374 (first), #1377 (next), #1365 + #1345 (one design, rows here), #1314, #1145/#1147.
