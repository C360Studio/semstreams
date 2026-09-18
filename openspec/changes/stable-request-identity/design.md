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

## Declared residuals

- **The retry ordinal is process-local.** After a process replacement mid-iteration the counter is zero, so a
  retry minted by the replacement reads `:N:0` rather than `:N:1` — a different name for the same logical work,
  which costs one extra provider call and nothing else. Deriving it durably is L4's, from
  `LoopEntity.PublishedRequestID` (#1330, ruling Q4: "the durable input for the retry ordinal is
  `PublishedRequestID` itself").
- **A continuation admitted while a request is in flight reuses that request's name.** `attachContinuation`
  refuses a terminal loop, a loop with pending tools, and a loop awaiting approval, but not a loop whose model
  request is outstanding — and both requests sit at the same `Iterations`. Inside the duplicate window the
  continuation's publish is rejected; outside it, agentic-model answers from the retained response. Either way the
  continuation's added turn is not lost: it is in the loop's context manager and rides the next iteration's
  request. The consequence is a deferred turn, not a dropped one, and it replaces today's shape, which is two
  concurrent provider calls answering one loop. Making the outstanding request's identity durable is L4's.

## Declared cost

The dedup test declares a 30s `Duplicates` window on its own stream. No shipped stream configuration is changed by
this layer: whatever window an operator's AGENT stream carries (the NATS server default is 2m when unset) is what
applies, and the guarantee does not depend on it.
