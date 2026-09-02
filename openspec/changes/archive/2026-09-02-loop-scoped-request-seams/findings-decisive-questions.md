# The two decisive questions, answered

base: `0a40ddf3` (verified in the claim worktree by the orchestrating session, independently of
`inventory-attach.md`, which reached the same answers by its own route)

#1227's owner comment names two questions as decisive for the fix design and says neither was
conclusively settled. Both are now answered, each pin read directly rather than relayed.

## Q1 — Does the dispatch `LoopTracker` rehydrate from durable state on restart?

**No.** And the source already says so, on the attach seam itself.

- `processor/agentic-dispatch/loop_tracker.go:118-136` — both constructors `make()` empty maps; no seeding.
- `processor/agentic-dispatch/component.go:337-403` (`Start`) — no KV or bucket read before
  `started = true` (`:392`); only `setupSubscriptions` (`:386`).
- `processor/agentic-dispatch/http.go:750-753` — *"Loop must exist in dispatch's tracker. A 404 here
  means we either never saw the loop (e.g., process restart lost the in-memory tracker before this
  request) or the loop has been removed."*
- Negative-result search for any rehydration spelling in `processor/agentic-dispatch/` → 2 hits, both
  comments about an unrelated activity-streaming feature.

**Consequence for #1227's fix design**: the concern that a naive "token must exist in the tracker"
check would refuse every legitimate post-restart resume is real — but it is not hypothetical and not
introduced by such a check. **The HTTP attach path already refuses post-restart resume with a 404
today**, and `commands.go:86-97` already returns "Loop %s not found" with no distinction between
"never existed" and "restart lost it". An existence check would not create that failure; it would
make the existing one classified and observable.

**Not a collision, but a convergence**: PR #1159 (`codex/gh1146-agentic-loop-restart`) touches
**zero `.go` files** — 9 files, all under `openspec/changes/agentic-loop-restart-safety/`. It is a
design proposal that independently reaches the same conclusion and proposes as target state:
*"`LoopTracker` and pending-approval caches SHALL NOT be authority. Dispatch SHALL reconstruct them
from current `AGENT_LOOPS` facts after replacement and SHALL perform exact read-through for explicit
LoopID operations."* That read-through is the precondition #1227's ownership check needs. **Two open
claims are approaching one seam from opposite sides** — restart-safety and authorization — and this
is an owner sequencing question, not something either claim should settle alone.

## Q2 — Does legitimate continuation preserve conversation context?

**No. Continuing a loop by `reply_to` discards the conversation** and starts a fresh one that reuses
the loop ID. Verified end to end:

1. `processor/agentic-dispatch/component.go:920-925` — `loopID = msg.ReplyTo`, taken directly. No
   existence check; a `reply_to` naming a loop that never existed is simply created.
2. `processor/agentic-dispatch/component.go:830-845` (`buildTaskMessage`) — carries
   `Prompt: msg.Content` and the resume anchors. **`Context` is never populated.**
3. `processor/agentic-loop/handlers.go:832-836` — `task.LoopID != ""` → `CreateLoopWithID`.
4. `processor/agentic-loop/state.go:171-180` — overwrites `m.loops[loopID]`,
   `m.pendingTools[loopID]`, and `m.contextManagers[loopID]` unconditionally. Its own doc comment
   (`state.go:143-149`) states it: *"the map write below OVERWRITES an existing record and its
   context manager."*
5. `processor/agentic-loop/handlers.go:872-885` — the fresh manager receives the assembled system
   prompt and the new user prompt. Nothing else.
6. `processor/agentic-loop/handlers.go:890-899` — the only other context source is
   `task.Context` (`RegionGraphEntities`, graph-entity content), which dispatch never sets.
   `ContextManager` has **no** restore/load method at all (`context_manager.go` census: 0 hits for
   any rehydration spelling).

`HandleTask`'s own doc comment reads *"processes an incoming task message and creates a new loop"* —
the code is doing what it says; the attach seam is calling a create.

### Two consequences the issue did not anticipate

- **AutoContinue rides the same path.** `component.go:924` resolves `GetActiveLoop(UserID, ChannelID)`
  into the same `loopID`, so the default multi-turn path reaches the same overwrite. `GetActiveLoop`
  filters to non-terminal loops (`loop_tracker.go:209-231`), so auto-continue attaches only to a loop
  that is **still running** — meaning the overwrite of `pendingTools` and `contextManagers` lands on
  live in-flight state, not on a finished conversation.
- **This is not only the hijack case.** #1227 asked whether the overwrite damages the happy path. It
  does. That reclassifies part of #1227 from hardening to a defect.

### What is NOT established, and matters

Whether carrying conversation across turns is the **framework's** job at all. A product could supply
`task.Context` itself, in which case the framework is correctly delegating conversation memory across
the product boundary (`openspec/project.md`) and only the in-flight overwrite is indefensible. No
in-tree producer sets `task.Context` on a continuation, but this repo is a framework and its consumers
are sister repos — **not searched here** (sisters are read-only inventory, and a sister sweep sizes a
migration note, never gates a design). This is an owner question, stated plainly rather than assumed
either way.
