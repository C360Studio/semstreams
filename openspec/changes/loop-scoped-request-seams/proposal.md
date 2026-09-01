# Loop-scoped request seams

> **Status: investigation, not yet a proposal.** No target state is written here. The owner's framing
> (2026-09-01) is that #1227, #1228 and #1225 may share one cause — *"an abstraction or pattern missing,
> with individual files/components hand rolling something we should provide a level or two up"* — and that
> this must be settled **before** any of the three is implemented. This document holds the question; the
> inventory holds the evidence; a proposal is written only if the evidence supports one.

## The question

Three open issues sit on one code path (`processor/agentic-dispatch`, `processor/agentic-loop`), and each
is about something not being done in one place:

- **#1227** — a request that *attaches to an existing loop* by `reply_to` gets no ownership or existence
  check. Authorization lives scattered across `Permissions.SubmitTask`, an auto-continue `(UserID, ChannelID)`
  scoping branch, and `canUserControlLoop`.
- **#1228** — loop-token *form* validation is enforced at exactly four call sites of `internal/looptoken.Valid`;
  other carriers of the same token (`UserSignal`, `ApprovalResponse`, control requests) check only non-emptiness.
- **#1225** — a `Validate` failure on a submission path is a silent drop: no response, no metric, a leaked gauge.

Read together: **no single seam owns admitting a request that names an existing loop.** Each carrier
hand-rolls some subset of {form, existence, ownership, classified refusal, observed signal}, and the
issues are the holes where a subset is empty.

Whether that reading is correct is exactly what this change must establish — including the null result,
that these are three unrelated fixes and no primitive is missing.

## Deliverables, in order

1. A line-pinned inventory of every seam that accepts a request naming an existing loop, recording per
   seam which of the five checks it performs.
2. Two empirical answers the issues flag as decisive and neither verifies (#1227, owner comment):
   whether the dispatch `LoopTracker` rehydrates from durable state on restart, and whether legitimate
   continuation preserves conversation context given `CreateLoopWithID` replaces the context manager.
3. A design read over that inventory: is a primitive missing, what is it, where does it live, and does it
   subsume the three issues — with the strongest case against.
4. Owner ruling. Only then a target state.

## Known collision

PR #1159 (Codex, draft — `codex/gh1146-agentic-loop-restart`, "preserve durable work across process
restart") is open over `processor/agentic-loop`. Deliverable 2's restart question may be answered, or
changed, by that branch. The inventory reads it rather than deriving restart behaviour from `main` alone.
