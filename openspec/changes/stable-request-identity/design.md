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
and then a response's RequestID no longer tells the loop which turn it answers. The invariant is enforced instead:

- **Knowing.** `LoopManager.outstandingRequests` (loopID → requestID) is written by `TrackRequest`, which every
  model-request publish site already calls, and cleared by `SettleRequest` when the response arrives. It matches on
  the request ID, so a late response for a superseded request cannot clear a newer request's mark.
  `requestToLoop` could not answer this: its only delete is `releaseLoop`, so it is append-only for the loop's
  life and records "published", never "outstanding".
- **Deferring.** `attachContinuation` returns a `deferred` flag instead of refusing. `HandleTask` does everything
  it normally does — the turn into the context, the caches the next request reads — and skips only the publish.
  The result carries `Deferred`, so the task delivery can tell it from a dedup; it Acks after the loop-entity Put,
  which is the effect it owns.
- **Carrying.** `LoopEntity.PendingContinuation` says a turn is waiting. A completion response that meets it
  advances the loop instead of settling: `carryDeferredContinuation` increments the iteration and calls
  `publishIterationRequest`, the one home both this path and the tool-results path now use to build the next
  request. The marker clears there, which is the moment the turn stops being deferred.

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
- **A deferred turn at an exhausted iteration budget is dropped, loudly.** When the outstanding response completes
  and the loop has no iteration left, the turn cannot be carried: the loop completes and a WARN names the dropped
  turn. Failing the loop instead would turn a successful completion into a failure because a later message
  arrived, which is worse for the user than the WARN.
- **The outstanding-request registry is process-local.** `LoopManager.outstandingRequests` answers "is this loop
  waiting on a model right now"; `requestToLoop` cannot, because it is append-only for the loop's whole life. A
  process replacement loses it along with the rest of the loop, and the pending-continuation marker on
  `LoopEntity` then persists with nothing to clear it. Restoring both is L4's (#1330), with the rest of the loop.

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
