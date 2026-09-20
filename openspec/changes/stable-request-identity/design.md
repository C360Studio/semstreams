# Design: stable request, call and loop identity

The Codex design this layer implements is not carried here. It lives on the closed branch at
`codex/gh1146-agentic-loop-restart`, `openspec/changes/agentic-loop-restart-safety/design.md` at `af829616`,
together with its three inventory passes. This file records only what that design does not: the RequestID grammar
the owner ruled on 2026-09-18, and the two declared residuals it leaves.

## The RequestID grammar (owner ruling Q4, #1330)

`<loopID>:req:<iteration>:<retry>`.

- `iteration` is the 1-based ordinal of the request within its loop: `LoopEntity.Iterations` at mint time, plus
  one. A loop's first request is `:1:0`. `handleToolsComplete` increments `Iterations` before it mints, so the
  request following a tool batch takes the next ordinal.
- `retry` is the within-iteration truncation-retry ordinal, read from the process-local counter
  `IncrementTruncationRetry` advances and `ResetTruncationRetry` clears. A compaction retry of iteration N is
  `:N:1`; the retry budget is exactly one, so the axis is 0 or 1 today.

Both inputs are facts `LoopManager` already holds, so `GenerateRequestID(loopID)` keeps its signature and derives
them itself. No call site computes an ordinal, and none can disagree with the state the loop is actually in — the
arithmetic at the three mint sites (`handlers.go`, task birth / truncation retry / tools complete) differs, and
passing a local through would have minted `:1:0` twice on every loop.

The `<loopID>:req:` prefix is unchanged. `ExtractLoopIDFromRequest` splits on `:req:`; semspec splits a RequestID
on the FIRST colon (measured). Both recover the loop token unchanged; only a consumer that parsed the suffix as a
UUID is affected, which is the one line this change adds to the migration note.

## What `Nats-Msg-Id` is and is not (owner ruling Q5, #1330)

Every `agent.request` publish stamps the deterministic RequestID as `Nats-Msg-Id` through the existing
`natsclient.PublishToStreamWithMsgID`. Where the stream declares a `Duplicates` window, the server rejects the
second publish of the same logical request outright.

**The window is a bonus; the retained-response reuse is the guarantee.** Dedup holds only inside a configured
window, and a redelivery after a long outage — exactly the restart case this wave exists for — falls outside it.
What holds unconditionally is agentic-model's exact retained-response read before every provider call: a matching
committed `AgentResponse` for that RequestID acknowledges the redelivery without invoking the provider. The
`#759` anti-goal stands: `Nats-Msg-Id` is bounded suppression, never permanent identity or proof of publication.
The tests say it in that order — `TestIntegrationRepublishedRequestIDReusesRetainedResponseOutsideAnyWindow`
proves the guarantee with no window in play; the window test is the bonus.

## One outstanding request per loop (review round 1, F1)

The Q4 grammar `<loopID>:req:<iteration>:<retry>` is unique only if the loop mints at most one request per
`(iteration, retry)` pair. Both ordinals move when the LOOP advances — `IncrementIteration` runs on the
tool-results path, the truncation counter on the retry path — so a second request minted while the first is still
outstanding carries the first's name and, with `Nats-Msg-Id` stamped from it, is dropped by the duplicate window.
That is what a continuation admitted mid-request did: `attachContinuation` refused a terminal loop, a loop with
pending tools and a loop awaiting approval, but not a loop that was waiting on a model.

The answer is NOT a third identity segment. A third segment would mean the loop can have two requests in flight,
and then a response's RequestID no longer tells the loop which turn it answers. The invariant is enforced instead
— for deliveries this component processes in order, which is the scope of every sentence below and the scope the
spec delta's SHALL now carries. The window that escapes it is a declared residual (§ Declared residuals, "The
admission check and the mint are not one critical section"):

- **Knowing.** Two maps, because "is a model answer owed" and "which request is live" are different questions and
  the difference is a real window. `LoopManager.outstandingRequests` (loopID → requestID) is written by
  `TrackRequest`, which every model-request publish site already calls, and cleared by `SettleRequest` when the
  response arrives; it decides whether a continuation defers. `LoopManager.currentRequests` is written by the same
  call and NOT cleared by `SettleRequest`; it is what the superseded-response guard compares against. A tool-call
  response settles its request while the loop stays on that iteration waiting for executors, so the outstanding
  mark is empty for the whole tool phase — an identity check keyed on that emptiness admits an earlier request's
  redelivered completion and settles the loop with the previous task's answer while the carried turn is still
  being worked (owner review round 2, reproduced sequentially). `requestToLoop` could answer neither: its only
  delete is `releaseLoop`, so it is append-only for the loop's life and records "published", never "outstanding"
  or "newest".
- **Deferring.** `attachContinuation` returns a `deferred` flag instead of refusing. `HandleTask` does everything
  it normally does — the turn into the context, the caches the next request reads — and skips only the publish.
  The result carries `Deferred`, so the task delivery can tell it from a dedup; it Acks after the loop-entity Put,
  which is the effect it owns.
- **Carrying.** `LoopEntity.PendingContinuation` says a turn is waiting. A completion that meets it advances the
  loop instead of settling, on BOTH shapes a completion takes — terminal model text and a terminal tool result
  (`StopLoop`, which the framework's own `decide` executor returns). The terminal-tool arm drains the accumulated
  tool results into the conversation before carrying, which the completing path never had to do: the carried
  request must send the terminal tool's own message, or its assistant tool call is an unpaired orphan and
  `RepairToolPairs` drops the call rather than send a broken pair. `LoopCompletedEvent.Decision` does not travel
  on that iteration, and should not: the field is the typed decision of the terminal that ENDED the loop, and
  this one did not end it — the call and its result stay in the trajectory and in the conversation, and the
  completion that does end the loop carries its own terminal. A completion response that meets a pending turn
  advances the same way: `carryDeferredContinuation` increments the iteration and calls
  `publishIterationRequest`, the one home both this path and the tool-results path use to build the next
  ITERATION's request. It is not the only site that mints a request, though — the truncation retry
  (`emitRetryRequest`) re-asks at the same iteration from the same context, and the birth request mints too — so
  the deferral bookkeeping does not live at any build site. It lives in `TrackRequest`, the call all three already
  make: every request that goes out is built from the context the turn was written into, so every request carries
  the turn, and putting the bookkeeping where the mark is taken is what makes that true of three paths instead of
  two.
- **Knowing it was sent.** `TrackRequest` RECORDS the carrier — `LoopEntity.PendingContinuationRequestID` — it
  does not clear the marker, and `HasPendingContinuation` means pending AND uncarried. The ordering is why.
  `persistHandlerResult` stamps the entity (`persistResultState`) and only then emits the results
  (`publishResults`), and a publish-phase failure is commit-unknown: the delivery quarantines with the request's
  durability unknown. A marker cleared at BUILD is therefore durably clear about a send that may never have
  happened, and the one fact that could re-carry the user's turn is gone from the only record recovery reads.
  Recording the carrier satisfies both obligations at once — nothing carries the turn twice, because a request
  already names it; and a quarantined publish leaves "pending, carried by `<loopID>:req:N+1:0`" durable for L4's
  replay. The clear moves to `SettleRequest`, where a response for that request is the first proof the send
  happened, and it runs before the completion logic, so a completion for the carrying request settles the loop
  normally. A turn admitted while a carrier is outstanding resets the carrier to empty in `attachContinuation`:
  that turn is in no request's body, so the next completion must carry it.

Settlement is untouched. A deferred task Acks exactly where a deduplicated one did; the carried request travels in
an ordinary non-terminal `HandlerResult` through `persistHandlerResult`, so a stamp or publish failure quarantines
under L1's existing rule rather than under a new one.

## L1's residuals that name this layer

L1 (#1327, squash-merged as `94cd8e4c`) left two residuals naming commits of this branch, and its promoted spec
deferred one identity decision to #1328. All three are answered here.

- **The cancel signal's PubAck** (L1's archived `design.md:220`). Answered in code: `handleCancelCommand`
  publishes the signal through `natsclient.PublishToStream` (`processor/agentic-dispatch/commands.go:185`), so the
  signal has synchronous PubAck before `noteSignalPublished` records it and before the command's user response is
  attempted. L1 recorded the published fact at the publication site so exactly this could tighten without moving
  the classification.

  Review round 1 then found the door a PubAck leaves open. A publish that FAILS reports what the client
  experienced, not whether the server stored — a lost acknowledgement reads exactly like a signal that never
  arrived — and the transient error that failure returned retried a bare `/cancel` whose target is resolved
  afresh: L1's R1 defect reached through an error instead of through a response. The publish site now records the
  ATTEMPT as well as the publication (`commands.go:184` and `:191`), and a resolved-target command whose attempt
  cannot be accounted for quarantines — unless the error PROVES the client refused it before the bytes left the
  process, which is a fail-closed whitelist rather than a judgement about the server (`command_effect.go`,
  `publishDefinitelyRejected`). A named cancel retries either way: its redelivery re-reads the loop the message
  names and cannot drift onto another. The requirement now names the cancel signal in its PubAck list and carries
  the attempt rule (`specs/agentic-dispatch/spec.md`, MODIFIED).

- **Identity-preserving replay at the post-PubAck submission response** (L1's archived `design.md:298`). L1
  quarantined `handleTaskSubmission`'s arm where the task has PubAck and the acknowledging user response does
  not, on the premise that a redelivery would mint a fresh task UUID and publish it with nothing downstream could
  deduplicate. **That premise no longer holds** — `findRetainedDispatchTask` reads the committed task back by its
  stable TaskID and republishes the same TaskID and LoopID — **and the arm still does not relax to Retry**,
  because identity was not the only effect it repeats. A redelivery re-enters `c.loopTracker.Track`
  (`component.go:1178`), and `Track` replaces the whole `LoopInfo` held under that LoopID
  (`loop_tracker.go:144-153`), so a loop that advanced past `pending` between the two deliveries is reset to
  `pending` under a fresh `CreatedAt`; it also re-fires `recordLoopStarted` (`component.go:1190`) and
  `recordTaskSubmitted` (`component.go:1198`), which are not the same kind of harm: `tasks_submitted_total` counts
  one submission twice, while `recordLoopStarted` increments the `active_loops` **gauge**, and one loop ends once,
  so the second increment is never taken back and the gauge leaks upward. Quarantine is still the honest
  classification, so no L1 test is relaxed and no proof-of-effect-freedom is claimed. Making that re-entry
  idempotent — `Track` merging rather than replacing a LoopID it already holds, and the two counters moving only
  on first commit — changes tracker and gauge behaviour, which is not this layer's subject; it is the precondition
  for the relaxation and is recorded here rather than taken quietly. The call-site comment carries the same
  finding so a reader of the code is not left with L1's falsified premise.

- **Deterministic response identity on the invalid-input lane** (main's `openspec/specs/agentic-dispatch/spec.md`,
  scenario "Invalid user input receives its negative consequence", which reads "extending it to the rest is
  L2's"). Not extended: `ResponseID` on that lane stays minted per publication. Which refusal a message earns is
  decided by which check failed, so two deliveries of one source message can carry different refusals; a
  source-derived identity would give those one name and let a duplicate window suppress the second, which trades a
  duplicate the user can read for a refusal the user never sees. The deterministic source-derived identity stays
  with the terminal lane, where one source has exactly one answer. The scenario's deferral bullet is replaced with
  this disposition rather than left pointing at a layer that has now landed.

## Declared residuals

- **The retry ordinal is process-local.** After a process replacement mid-iteration the counter is zero, so a
  retry minted by the replacement reads `:N:0` rather than `:N:1` — a different name for the same logical work,
  which costs one extra provider call and nothing else. Deriving it durably is L4's, from
  `LoopEntity.PublishedRequestID` (#1330, ruling Q4: "the durable input for the retry ordinal is
  `PublishedRequestID` itself").
- **The deferred continuation's turn is ordered before the response it was admitted behind.** The turn enters the
  context at admission, so a request that carries it reads `[… user(deferred turn), assistant(the answer that was
  outstanding)]` — the two in the order they were written down, not the order they were spoken. This is not new:
  the tool-call path has put an admitted turn ahead of its tool results since intake started attaching (#1227).
  Reordering it means holding the turn outside the context until the response lands, which is a context-manager
  change and not this layer's. Recorded rather than fixed here.
- **A turn admitted at the iteration ceiling is KEPT on the completed record — owner ruling, 2026-09-20 (#1328).**
  `carryDeferredContinuation`'s `ErrMaxIterationsReached` arm completes the loop and WARNs. Through round 2 the
  arm was declared unreachable, and from `HandleModelResponse` (`handlers.go:1424`) it still is: that handler
  returns `WrapFatal` at `:1330-1337` on the same predicate over the same value, and nothing between that check
  and the carry moves `Iterations`. Round 3's terminal-tool carry (`:2504`) is a SECOND caller and it reaches the
  arm: nothing gates iterations between `HandleToolResult`'s entry and that call, the tool lane is at-least-once
  with no request-identity guard, and `RemovePendingTool` tolerates a call that is already gone — so a
  redelivered terminal tool result lands on a loop whose earlier carry already spent the last iteration, with a
  newer turn deferred behind it.
  The arm used to clear the marker. It no longer does: the loop completes, and its durable record keeps
  `PendingContinuation` with an empty `PendingContinuationRequestID` — the fact that this loop ended owing a turn
  no request ever contained. Same shape as the quarantined carry, for the same reason: the one record that could
  recover a user's turn must not be a log line. `LoopManager.ClearPendingContinuation` was the drop and had no
  other caller, so it is deleted with the behaviour (added by this change's own `8734713d`, never released, so no
  Tier 1 surface is withdrawn).
  Kept, but inert. Nothing resurrects a completed loop off the flag: `attachContinuation` refuses a terminal loop
  at `state.go:308-312` before any deferral bookkeeping; `CancelLoop` refuses one at `:1450-1457`; the flag has no
  reader outside `processor/agentic-loop`, where both readers are `HasPendingContinuation` on a live response
  (`handlers.go:1423`, `:2501`); this layer has no restore-from-KV path at all (loop restoration is L4's, #1330);
  and L3's durable reader skips terminal entities outright when it resolves a route (`activeLoop`'s
  `entity.State.IsTerminal()` conjunct — `processor/agentic-dispatch/http_activity.go:329` in **#1329's tree at
  `81a5cabb`**, NOT in this one, where that line is an SSE attach error branch). Observed by
  `TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord`, which asserts both halves — the turn
  survives on the record, and a new task naming the settled loop is refused.
- **Tool messages reach a request in map order, which is not an order.** `GetAndClearToolResults` ranges
  `entity.PendingToolResults` (`state.go:1106-1109`) and appends, so the slice — and therefore the tool messages
  `buildToolMessages` builds from it — comes out in Go's randomized map-iteration order. Pre-existing, and
  harmless while a batch produced one result at a time; the skipped-sibling synthesis is the first change that
  routinely puts two or more tool messages into a single request, so it is the change that makes the exposure
  routine. The pairing is unaffected — `RepairToolPairs` and the provider contract match on `ToolCallID`, not on
  position — but the model reads a batch whose order can differ between two otherwise identical runs.
  NOT fixed here: it is a Go change, which would owe another agentic-tier run and another review round for what
  is, today, a NIT. The sort key already exists and this change is what put it on every call:
  `stampToolExecutionCorrelation` (`execution_identity.go:15-28`) stamps `CallOrdinal` 1..n on the whole batch
  before any of it is dispatched or queued, `synthesizeToolFailure` carries it onto the synthetic
  (`handlers.go:1640`), so sorting the drained results by `CallOrdinal` would make the conversation deterministic
  in one line. `TestATerminalToolCarriesItsOwnResultWhenTheBatchHasQueuedSiblings` asserts the PRESENCE of each
  result by `ToolCallID`, deliberately not their order, so it does not encode today's accident as a guarantee.
- **`dispatchedFromQueue`'s bound has the same shape as the one round 4 removed, and is NOT fixed here.**
  `handlers.go:1801` bounds its dispatch drain at `len(GetPendingTools(loopID)) + 64`, which is derived from the
  PENDING set rather than from the queue it drains. A batch whose first 65-plus calls all fail to dispatch would
  therefore stop with calls still queued, and `handleToolsComplete` would mint the next request from an assistant
  message with unanswered calls — the same repair-away that the skipped-sibling synthesis exists to prevent, one
  path over. It is pre-existing, it needs a string of consecutive dispatch failures rather than an ordinary large
  batch, and the fix is now one call away (`QueuedToolCount`), but changing it is a Go change on a path this
  round did not otherwise touch. Recorded so the next reader does not have to re-derive that the two bounds are
  the same mistake at different odds; checked and found nowhere else on this path (no other constant cap exists
  in `processor/agentic-loop` outside the compaction token budgets).
- **On the COMPLETING terminal-tool path a queued sibling still gets no result.** The carry path now synthesizes
  one per queued call (`synthesizeSkippedQueuedTools`), because the carried request replays the batch and
  `RepairToolPairs` would drop the whole group. The completing path mints no further request, so nothing re-reads
  the batch in this process and the defect is invisible here — but the conversation it persists keeps an assistant
  `tool_call` with no answering message, which a restore or replay would have to repair. Pre-existing, unchanged
  by this round, and deliberately not fixed in it: the fix belongs with whatever restores a loop's context, which
  is L4's (#1330). `drainPendingToolFailures` already covers the other terminal transitions (fail, cancel, max
  iterations); the `StopLoop` completion is the one that does not.
- **A redelivered terminal tool result re-settles an already-complete loop. Pre-existing, examined here, NOT
  fixed here.** This change's own premise — the tool lane is at-least-once, `HandleToolResult` has no
  request-identity guard, and `RemovePendingTool` tolerates a call that is already gone — has a second
  consequence beyond the ceiling arm: when the redelivery lands on a loop the FIRST delivery already completed,
  it completes it again. Reproduced sequentially through the public handler (no concurrency, no restart): a
  second `agent.complete.<loopID>` on the wire, a second completion record, and `completed_at` rewritten on the
  durable record. The outcome is not corrupted — the second completion carries the same values — so the harm is
  a duplicate terminal event and a drifting timestamp.
  Two lines cause it. `LoopEntity.TransitionTo` answers the same-state case `nil` BEFORE its terminal check
  (`agentic/state.go:180-186`), so `complete → complete` is a silent success; and the `StopLoop` branch
  (`handlers.go:2482`) runs ahead of the tool lane's only terminal guard, which is inside the `AllToolsComplete`
  branch at `:2536`. Nothing suppresses it downstream either: `MsgID` is set at exactly three sites
  (`handlers.go:1174`, `:2096`, `:2842`), all model-request mints, so the completion publishes with an empty
  `Nats-Msg-Id` and the stream's Duplicates window never sees it (`PublishedMessage.MsgID`, `handlers.go:47-55`;
  `component.go:2275-2279`). Only `complete → complete` leaks: a loop cancelled while the tool ran refuses the
  late result with `cannot transition from terminal state cancelled` and publishes nothing.
  It is L4's, by the owner's Q6 ruling on tool-lane redelivery classification, and the inventory is placed there
  rather than filed as its own issue:
  https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5751092034. Recorded here so the archived
  change says the tool lane was examined and this is what was found — the model lane's superseded-response guard
  has no counterpart on this lane until durable request identity exists to give it one.
- **The admission check and the mint are not one critical section.** `attachContinuation` (`state.go:333`) reads
  `outstandingRequests` under the manager lock and releases it; `HandleModelResponse` clears the mark at
  `handlers.go:1275` and the carrying request does not re-take it until `TrackRequest` at `handlers.go:2826`,
  after the context write, `maybeCompact`, `IncrementIteration` and the whole request build. `agent.task` and
  `agent.response` are separate JetStream consumers (`component.go:1103-1108`) and nothing in this package
  serializes per loop across them, so a continuation delivered inside that window is admitted against an empty
  mark and both paths mint `…:req:N:0` from the same iteration counter — the identity collision this change
  closes for the serialized case. The reviewer reproduced it deterministically against the real state machine
  with an overlay probe; what is inferred is only the scheduling of two live consumers. The long windows are
  already shut: `attachContinuation` refuses with `ErrLoopBusy` while tools are pending (`state.go:313-317`) and
  while a human approval is outstanding (`:318-322`).

  It is NOT closed here, and deliberately: closing it needs either per-loop serialization across the two
  consumers or a durable check-and-set on the mark, and the second comes for free from L4's
  `LoopEntity.PublishedRequestID` under `Update(revision)` (#1330) — a new concurrency primitive invented in a
  review-fix commit would be the wrong shape and the wrong layer. The inventory is on #1330 for L4's design,
  placed by the coordinator: https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5749352045
- **The outstanding-request registry is process-local.** `LoopManager.outstandingRequests` answers "is this loop
  waiting on a model right now"; `requestToLoop` cannot, because it is append-only for the loop's whole life. A
  process replacement loses it along with the rest of the loop, and the pending-continuation marker on
  `LoopEntity` — with the request that carries it — then persists with nothing to settle it. Restoring both is
  L4's (#1330), with the rest of the loop. The marker surviving a quarantine is what makes that restoration
  possible at all: the durable record says which request was supposed to carry the turn.
- **A loop this process has minted nothing for cannot be identity-checked at all.** The superseded-response guard
  compares a response against `currentRequests[loopID]`, and after a process replacement that map is empty while
  the loop itself is live: the response routed here through `GetLoopForRequestWithRecovery`, which rebuilds
  routing FROM the RequestID and deliberately does not claim the request was minted
  (`registerRequestRoute`). The empty case is therefore let through, and a superseded response delivered to a
  replacement is handled as though it were current. Refusing it instead would strand every live loop across a
  restart, which is the worse failure. Closing it needs durable request identity — L4's
  `LoopEntity.PublishedRequestID` (#1330), the same field that closes the admission/mint window above — and the
  guard is written so that one field replaces the process-local read without moving the check.

## Declared deviations from the brief

- **No exported `<iteration>:<retry>` parse helper.** The brief asked for one; nothing in this tree or any sister
  parses a RequestID suffix, and the framework's own `ExtractLoopIDFromRequest` splits on the first colon and never
  looks past it. An exported parser with zero consumers is phantom surface, and the durable input a parser would
  serve — recovering the retry ordinal after a replacement — is L4's `LoopEntity.PublishedRequestID`, which carries
  the whole previous RequestID rather than requiring the suffix be re-derived. The grammar is documented in the
  migration note for the only consumer class that exists: a log or index that must treat the suffix as opaque.
  **Accepted at review round 2 and recorded on #1328**: L4 adds `internal/looprequest` beside its first reader, so
  the parser is born with a consumer rather than ahead of one.
- **`GenerateRequestID` derives both ordinals instead of taking them from its callers.** Recorded in full in the PR
  body; the short reason is that the three call sites' locals disagree — `handleToolsComplete`'s post-increment
  `newIteration` is 1 for the *second* request — so only manager-held state is injective across all three.
  **Accepted at review round 2.**

## Declared cost

The dedup test declares a 30s `Duplicates` window on its own stream. No shipped stream configuration is changed by
this layer: whatever window an operator's AGENT stream carries (the NATS server default is 2m when unset) is what
applies, and the guarantee does not depend on it.
