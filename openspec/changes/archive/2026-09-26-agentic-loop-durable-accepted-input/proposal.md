# Change: agentic-loop durable accepted input — the deferred turn, the task prompt and the accepted task survive replacement

> Change id `agentic-loop-durable-accepted-input`. It claims #1365 and #1345 as one design (ruling 3, #1146
> issuecomment-5828511934). Base: `9e5d8455`. Parent epic: #1146 (Lane A, step 3, after #1376 at `9e5d8455`).
>
> **Design phase.** This first commit is the claim. The design is owed in two steps: a line-pinned inventory of every
> reader and writer of the three facts and of the five intake failure sites (explorer), then the design bound to the
> transition-result table (architect). One Tier 1 design review and owner acceptance precede implementation.
>
> **Rulings applied, none reopened:**
> - #1146 issuecomment-5828511934 rulings 2, 3 and 5: the obligations are rows of the `agentic-loop` transition-result
>   table (one MODIFIED requirement, no new requirements); #1365 and #1345 are one design, one Tier 1 review, one
>   migration section, one or two PRs; implementation serializes with other shared loop changes and precedes #1377.
> - #1330 Q2/Q8 (2026-09-22) stand as the historical acceptance of L4a: the marker-only limitation was the honest
>   sentence for its window; the field is this change.
> - Owner standing rule (2026-09-22, simple over edge-case code): the documented alternative is listed first in every
>   docket row; a design that ratchets complexity up returns to the owner.

## Why

Three facts, to be verified against `9e5d8455` by the inventory:

- **The deferred turn is a marker, not the text.** `PendingContinuation` on `agentic.LoopEntity` records THAT a
  continuation was admitted while a request was outstanding. A loop rebuilt after process replacement (L4a, PR #1361)
  sees the marker, has no text to carry, clears it with a warning and tells the user to re-send (#1365).
- **The task prompt is the one per-loop cache a rebuild does not restore.** `taskPrompts` has no record field behind
  it; after a rebuild `LoopCompletedEvent.Prompt`, `LoopFailedEvent.Prompt` and `recoverEmptyContext` read it empty,
  the last falling back to the literal "Continue with the task." (#1365, #1330 Q8).
- **Task intake is the one loop input class that still logs and ACKs.** Five failure sites in the intake lane consume
  the delivery and lose the task; Retry cannot help because `HandleTask` dedups a redelivery against the loop the
  first delivery created, and Quarantine would latch the whole task lane on one bad task. The lane needs a durable
  record of the accepted task that a redelivery can resume from; `rememberPendingTaskResult` / `pendingTaskResult`
  prototype it in-tree (#1345).

## What changes

- **Durable accepted-input facts on `agentic.LoopEntity`** (Tier 1, ADR-106): the accepted deferred turn's text,
  written where the marker is set and cleared where it clears, replayed on rebuild after the retained conversation;
  the task prompt, written at birth and restored beside the other caches; the resumable intake record the task lane
  settles against. Names and representation are the design's to decide against the inventory; the proposal fixes only
  that they are additive record fields, not a new bucket, store or stream.
- **The task-intake lane settles after its durable effect**, like every other loop input class since L1: a
  redelivered task resumes from the accepted record instead of being deduplicated into silence.
- **One migration section** for the record shape and the changed intake disposition; **spec text** that describes
  exactly the proved behaviour and its retention/size bounds.

## What does not change

No immutable input-source binding for fresh roles (#1352), no general context snapshot, no indefinite reconstruction,
no new durable store, no framework-wide declaration (#1145/#1147, beta.165). Public `LoopState` vocabulary stays with
#1314. The transition-result contract's rows for these two obligations are filled, not rewritten.

## Impact

- `agentic` (Tier 1): additive fields on `LoopEntity`; the apidiff guard must report additions only.
- `processor/agentic-loop`: the marker's set/clear sites, `restoreLoopFromRequest`, the task-prompt cache and its three
  readers, the five intake failure sites and `HandleTask`'s dedup.
- Record size: the deferred turn's text rides on a KV record — a stated bound (payload-size class #857), decided in
  the design, never an unbounded field.
- Sister repositories: read-only inventory sizes the migration note (owner 2026-08-30); nothing is edited there.
- E2E: the intake disposition becomes observable, so `task e2e:agentic` is the final validation of the landed diff.
