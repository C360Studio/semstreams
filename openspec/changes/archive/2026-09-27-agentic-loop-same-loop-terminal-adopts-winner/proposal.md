# Change: agentic-loop — a same-loop terminal of a different kind adopts the durable winner

> Change id `agentic-loop-same-loop-terminal-adopts-winner`. It claims #1399 (draft PR #1402). Base: `078782b1`.
> Parent epic: #1146. Ruled by the owner on #1146 (issuecomment-5854830449, Q2); `design.md` § 0 carries the ruling.

## Why

A terminal is committed by four sequential durable writes (`processor/agentic-loop/terminal_owner.go`
`commitTerminalSteps`): `COMPLETE_<loopID>` by create-once, the graph stamp, the event, the loop record. When the
create is refused, `createTerminalMarker` reads the saved terminal back and adopts it only when its loop ID AND its kind
match the candidate's. A saved terminal of another kind for the same loop is refused with an error, which
`commitTerminal` wraps Fatal; the lane quarantines the delivery, and the quarantine latches the lane.

Under W2 of #1377 (an approval-timeout sweep terminal whose marker landed and whose publication failed), an approved
terminal tool (`decide`, `StopLoop`) derives a completion that meets the saved `max_iterations` failure as a different
kind. The refusal latches the process's whole `tool.result` lane: loop health reads `delivery ownership lost`, the
handle drains, and tool results for every loop in that process stop until restart; the record never converges. Under
W1 (a lost record compare-and-swap after marker and event landed), a later terminal of another kind likewise
quarantines instead of converging the record.

Once create-once has decided, the loser is stale, not poison. The same-kind branch four lines later already adopts the
saved terminal, republishes its event, re-seats the loop and writes the record.

## What changes

- `createTerminalMarker`: a saved terminal of a different kind for the same loop takes the adopt branch; only a saved
  terminal naming another loop still refuses. The adoption audit line names both kinds.
- `settleTerminal`: an adopted terminal re-seats the in-memory entity's terminal `State` and `Outcome` from the
  adopted kind and clears the losing outcome's fields, so the record is written in the durable terminal's kind.
- Spec: the "different kind … quarantined / first terminal wins" sentences become adoption; W1's convergence reads
  "the loop's next terminal of any kind converges the record"; the W2 latch sentence is removed.
- Docs: `docs/operations/migration-beta162-to-beta163.md` and `processor/agentic-loop/doc.go` follow the spec.

## Non-goals

- The cold cancel arm (`adoptDurableCancel`, `settleUncancellableLoop`): #1362 ruling 1 is untouched — a cancel meeting
  a marker of another kind on a process that does not hold the loop still Retries.
- W3, the deferred-turn write (#1146 Q3, its own PR), and the consolidation design (#1146 Q4, beta.165).
- No new durable fact, bucket, metric, port or exported symbol.

## Consumers

In-tree: every agentic-loop lane that commits a terminal (model response, tool result, approval response, cancel,
the approval-timeout sweeper). Sister repos that read `COMPLETE_<loopID>` or `agent.complete` / `agent.failed`
(semspec's key scan, the dispatch `/activity` SSE, semteams) see the durable terminal republished where they
previously saw nothing; the event is the saved one, so a consumer sees one outcome per loop.

## Impact

- `processor/agentic-loop` only. BREAKING in the documented-contract sense (`fix(agentic-loop)!`): a delivery that
  was quarantined is now acknowledged, and a record that stayed non-terminal is now written terminal.
