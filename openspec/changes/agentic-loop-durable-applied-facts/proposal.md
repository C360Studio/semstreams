# Change: agentic-loop recovers redelivered inputs from durable applied facts on `LoopEntity`, not from conversation layout

> Change id `agentic-loop-durable-applied-facts`; claims #1330, narrowed to **L4a** by the owner's ruling of
> 2026-09-22 (OQ7). **L4b is #1362**: the approval lane, the verdict after waiter loss, the single terminal owner and
> route-ambiguity metering, with their tests. Pins are at `b7ce8727` (`origin/main`) through `inventory.md` and
> `reconciliation.md`; a `68c14c8e` pin (`origin/codex/gh1146-agentic-loop-restart`, PR #1159 — never merged) appears
> only where the text explains history. Owner rulings: #1330, 2026-09-18, "as recommended on all eight" (Q1–Q8) and,
> in its own comment, terminal outcome adoption; #1330, 2026-09-22, the reconciliation docket and its simplicity
> re-read (`design.md` § 1). Design-phase claim.

## Why

PR #1159 (never merged; `68c14c8e`) proved that a redelivered tool result was already applied by **re-rendering** it:
`toolResultProvenInLaterRequest` (`settlement_recovery.go:686-740`) called `buildToolMessages` and compared the result
to `request.Messages[index+CallOrdinal]`. Message formatting was thereby a restart-safety input: a change to
tool-message decoration silently changed what counted as "applied". That layer never landed, so **on `main` there is
nothing to prune** — what this change lands is the positive content the design was accepted for. What `main` does have
is the gap the layer was answering: the loop record names no outstanding request, so after a process replacement the
superseded-response guard reads an empty process map (`handlers.go:1253`, L2's declared residual `state.go:955-961`),
the tool lane stores a redelivered result by key before any check (`handlers.go:2546`), and the carrier writes the
advanced record before publishing its request (`component.go:1947` before `:1959`).

Every fact recovery needs is already durable except one: which request is outstanding. `PendingToolResults`
(`agentic/state.go:57`, keyed by ExecutionID at `state.go:1075-1082`, drained by the advance itself at
`state.go:1107-1121`) is the current batch's applied set and rides the same `Put` as `Iterations`
(`component.go:1947` → `:2483`). Ruled 2026-09-18: add the one missing fact.

## What changes

1. `LoopEntity.PublishedRequestID` (`published_request_id`): the RequestID whose PubAck preceded the KV update that
   wrote the record. Set at birth and at every request-minting transition; never cleared.
2. The non-terminal carrier (`component.go:1923`) publishes first, then writes with `Update(observedRevision)`
   (`natsclient/kv.go:231`) instead of `Put` (`component.go:2483`). No lane holds a revision on `main` — all four
   writers `Put` and discard it — so the process retains the revision its own last write returned, birth writes by
   `Create`, and a CAS loss releases the loop's process state before it retries (owner ruling 2026-09-22, OQ3).
3. Identity adoption: before publishing the next request, recovery reads the newest retained message on
   `agent.request.<loopID>` (a new reader, built here) and adopts it when its RequestID is the next ordinal; a process
   with no memory of the loop adopts a newer retained request into the record before classifying any input
   (`design.md` § 3.6). A redelivered terminal adopts the loop's durable terminal (`COMPLETE_` marker) by loop ID +
   kind (#1330, owner ruling 2026-09-18, terminal outcome adoption) — that half ships with L4b (#1362). Content is
   never compared.
4. Request publishes stamp `RequestID` as `Nats-Msg-Id` — **already shipped by L2** (`component.go:2328` routes every
   publish through `PublishToStreamWithMsgID`, `natsclient/client.go:963`; the three mints stamp it at
   `handlers.go:1174`, `:2194`, `:2958`), so L4a verifies the window collapse rather than building it. The duplicates
   window is a bonus; L2's retained-response reuse (`processor/agentic-model/component.go:633-643`) is the guarantee.
5. Two effect-free ACK paths with metric and audit line: any input for a loop already terminal in KV (L4a, at
   `component.go:2195` warm and `:2296` cold, with two new reason values on the existing `tool_results_dropped_total`)
   and a governance verdict redelivered after its waiter is gone (`governance_dispatcher.go:337`,
   `component.go:2759-2780` — L4b, #1362).
6. One deletion, not a pruning: `IncrementTruncationRetry` / `ResetTruncationRetry` (`state.go:466`, `:477`) with
   their callers, replaced by the retry ordinal parsed from `PublishedRequestID`. The predecessor's layout and content
   proofs, retained-request compares, `Iterations--` and `requirePreceding` were never built on `main`, so they are not
   deleted — they are simply never written (`design.md` § 6). Restart claims at `docs/concepts/17-approval-flow.md:65`
   are corrected.

## Scope boundaries

- Prerequisites: L1 #1327 (callbacks settle after their durable effect), L2 #1328 (stable identity; the RequestID
  grammar `<loopID>:req:<iteration>:<retry>` and the retry ordinal derived from `PublishedRequestID` live there),
  L3 #1329 (durable loop authority, port-owned bucket). L4 is #1330 and assumes all three.
- **Premise correction, recorded:** RequestIDs were UUID-minted at `68c14c8e` (`state.go:1364-1365`) and `af829616`
  (`state.go:1131-1132`); "RequestIDs are deterministic" was L2's target when this was written and is now a fact on
  `main` (`state.go:1339-1347`, `%s:req:%d:%d`). L4a builds the first reader of that grammar.
- **Scope, 2026-09-22:** this change is L4a. #1362 (L4b) carries the approval lane's cold branch,
  `continuation_unavailable`, the verdict-after-waiter-loss classification, the single terminal owner and
  route-ambiguity metering; `tasks.md` § "Moved to L4b (#1362)" lists every moved task.
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
  `governance_dispatcher.go:727` nor `processor/rule/publisher.go` stamps a MsgID, so a duplicate proposed/verdict pair lands.
- **Pre-existing bound, residual:** record size is `len(PendingToolResults) × ToolResultMaxBytes`, bounded only by NATS max payload; L4 neither widens nor guards it.
- **Accepted duplicates:** `agent.created` republishes at iteration 0 (Q5); terminal events already republish (`openspec/specs/agentic-loop/spec.md:430-446`), which terminal adoption relies on.
- **Adoption drift, residual:** the model answers the adopted retained request body, equal to in-process context up to
  request-time decoration (the request builder in `handlers.go`); an adopted terminal's content differences are logged,
  never a disposition.
- **Divergent duplicate, residual:** the predecessor's quarantine (`settlement_recovery.go:799-802`) is not built, so a
  current-batch duplicate for a stored ExecutionID overwrites by key; the framework producer cannot diverge
  (`processor/agentic-tools/component.go:710-713`, one outcome per execution ID).
- **Record lifetime vs stream retention, residual (measured):** AGENT_LOOPS is `History: 10, TTL: 24h`, refused otherwise
  (`processor/agentic-loop/internal/loopbucket/acquire.go:20,42-43`); an expired record is a gone loop; a post-expiry
  redelivery takes the existing not-observable Retry path (the cold arms `component.go:1700` and `:2292`, via
  `loop_presence.go:66-94`).
- **At-least-once counter, documented (owner ruling 2026-09-22, OQ4):** `tasks_submitted_total`
  (`processor/agentic-dispatch/metrics.go:112`) counts a replayed task submission again. The counter is at-least-once
  under redelivery; the migration note says so and a test pins it, rather than a new arm in the task lane.
- **No approval-deadline hydration, documented (owner ruling 2026-09-22, OQ2):** a replaced process re-arms no approval
  deadline; the loop stays `awaiting_approval` until answered or cancelled. It is a spec scenario and a migration-note
  line, not a startup path.
