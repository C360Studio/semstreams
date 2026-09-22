# Change: agentic-loop recovers redelivered inputs from durable applied facts on `LoopEntity`, not from conversation layout

> Change id `agentic-loop-durable-applied-facts`; closes #1330 (restart-safety L4). Every `file:line` is at `68c14c8e`
> (`origin/codex/gh1146-agentic-loop-restart`, PR #1159) unless prefixed `main:`. Owner rulings: #1330, 2026-09-18, "as
> recommended on all eight" (Q1–Q8) and, in its own comment, terminal outcome adoption (`design.md` § 1, § 5.7). Design-phase claim.

## Why

PR #1159 proves that a redelivered tool result was already applied by **re-rendering** it: `toolResultProvenInLaterRequest`
(`processor/agentic-loop/settlement_recovery.go:686-740`) calls `buildToolMessages` (`:687`) and compares the result to
`request.Messages[index+CallOrdinal]` (`:723`, `:735`). Message formatting is thereby a restart-safety input: a change to
tool-message decoration (`handlers.go:2730-2756`) silently changes what counts as "applied". The shape recurs as content
equality at `settlement_recovery.go:843`, `:763-817` and on the terminal owner (`component.go:1852-1866`), and as `Iterations--`
(`state.go:486-488`), compensating for writing the advanced iteration before publishing its request (`component.go:1795` before `:1798`).

Every fact recovery needs is already durable except one: which request is outstanding. `PendingToolResults`
(`agentic/state.go:54`, keyed by ExecutionID at `state.go:1107-1119`) is the current batch's applied set and rides the
same Put as `Iterations` (`handlers.go:2612`, `component.go:1795 → :2411`). Ruled 2026-09-18: add the one missing fact.

## What changes

1. `LoopEntity.PublishedRequestID` (`published_request_id`): the RequestID whose PubAck preceded the KV update that
   wrote the record. Set at birth and at every request-minting transition; never cleared.
2. The non-terminal carrier (`component.go:1782-1799`) publishes first, then writes with
   `Update(observedRevision)` instead of `Put` (`:2411`); the revision is already read at `:2163` / `:1564`.
3. Identity adoption: before publishing the next request, recovery reads the newest retained message on
   `agent.request.<loopID>` (`settlement_recovery.go:63`) and adopts it when its RequestID is the next ordinal; a process with
   no memory of the loop adopts a newer retained request into the record before classifying any input (`design.md` § 3.6). A
   redelivered terminal adopts the loop's durable terminal (`COMPLETE_` marker, `component.go:1846-1870`) by loop ID + kind
   (#1330, owner ruling 2026-09-18, terminal outcome adoption). Content is never compared.
4. Request publishes stamp `RequestID` as `Nats-Msg-Id` (`main:natsclient/client.go:968`); the duplicates window is a
   bonus, L2's retained-response reuse (`processor/agentic-model/component.go:616-627`) is the guarantee.
5. Two effect-free ACK paths with metric and audit line: a governance verdict redelivered after its waiter is gone
   (`governance_dispatcher.go:557-560`, tool-lane classification) and any input for a loop already terminal in KV.
6. Deleted: the layout/content proofs (the terminal owner's included), the retained-request compares, `Iterations--`,
   `requirePreceding`, their tests (`design.md` § 6); restart claims at `main:docs/concepts/17-approval-flow.md:65-68` corrected.

## Scope boundaries

- Prerequisites: L1 #1327 (callbacks settle after their durable effect), L2 #1328 (stable identity; the RequestID
  grammar `<loopID>:req:<iteration>:<retry>` and the retry ordinal derived from `PublishedRequestID` live there),
  L3 #1329 (durable loop authority, port-owned bucket). L4 is #1330 and assumes all three.
- **Premise correction, recorded:** RequestIDs are UUID-minted at `68c14c8e` (`state.go:1364-1365`) and `af829616`
  (`state.go:1131-1132`); "RequestIDs are deterministic" is L2's target, not a fact. Q4 (#1328) is the fix; L4 lands after it.
- `agentic-model` behaviour is unchanged; no delta for that capability (`design.md` § 8).

## Non-goals (the #1146 anti-goals, verbatim)

- Universal exactly-once processing
- Universal checkpoint/recovery subsystem
- Event sourcing or CQRS
- Generic supervisor, state machine, outbox, or checkpoint bucket
- Operator-selectable recovery mode
- Reconstructing work by scanning all retained events
- Solving unrelated component recovery in this issue

## Declared cost

- **KV record growth:** one string, ≈ 40 + len(loopID) bytes under the L2 grammar. No content is added.
- **Audit-noise residual:** a crash between governance proposals and the update re-proposes on redelivery; neither
  `governance_dispatcher.go:665` nor `processor/rule/publisher.go` stamps a MsgID, so a duplicate proposed/verdict pair lands.
- **Pre-existing bound, residual:** record size is `len(PendingToolResults) × ToolResultMaxBytes`, bounded only by NATS max payload; L4 neither widens nor guards it.
- **Accepted duplicates:** `agent.created` republishes at iteration 0 (Q5); terminal events already republish (`main:openspec/specs/agentic-loop/spec.md:430-446`), which terminal adoption relies on.
- **Adoption drift, residual:** the model answers the adopted retained request body, equal to in-process context up to
  request-time decoration (`handlers.go:2641`); an adopted terminal's content differences are logged, never a disposition.
- **Divergent duplicate, residual:** deleting `settlement_recovery.go:763-817` drops the quarantine at `:799-802`; a
  current-batch duplicate for a stored ExecutionID overwrites by key (`state.go:1119`); the framework producer cannot diverge.
- **Record lifetime vs stream retention, residual (measured):** AGENT_LOOPS is `History: 10, TTL: 24h`, refused otherwise
  (`internal/loopbucket/acquire.go:20,42-43`); an expired record is a gone loop; a post-expiry redelivery takes the
  existing not-observable Retry path (`settlement_recovery.go:488-490`, `:574-575`).
