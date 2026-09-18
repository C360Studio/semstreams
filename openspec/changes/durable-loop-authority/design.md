# Design: durable loop authority and port-owned loop bucket

The design this layer implements is not carried here. It lives on the closed branch at
`codex/gh1146-agentic-loop-restart`, `openspec/changes/agentic-loop-restart-safety/design.md` at `68c14c8e`, with
its R8 review and task record. This file carries only what that design cannot: what changed when the two commits
were replayed onto a base that does not have the four approval-recovery commits or the sequential-chat commit
between them.

## What was deliberately not carried

`5e0e2259` and `68c14c8e` were authored above `14ae0437` (sequential chat, auto-continue becomes opt-in) and above
`2125c337` / `52064acd` / `615997c6` / `99fabd2e` (approval recovery, approval replay bound to its execution).
Those commits belong to other layers of the #1146 takeover and are not in this stack. Three of their behaviours
reached into the two commits replayed here and were removed at the conflict:

- **`prior_messages` on user, HTTP and task messages.** The field does not exist at this base, so the two refusal
  branches `5e0e2259` added for it, the `independent_default` integration mode, and the README/migration prose that
  describes independent chat turns were dropped. The classified-error return `processCommandSync` gained is kept:
  it is this commit's own, and it exists because auto-continue now resolves through a view that can be unavailable.
- **The `execution_id` echo on `POST /loops/{id}/approval`.** `68c14c8e`'s tree requires the client to echo the
  displayed pending ExecutionID (400 when omitted, 409 when stale). That requirement is `99fabd2e`'s, not this
  layer's, and carrying it would ship an unannounced breaking API change to every approval client. The approval
  handler here reads current durable authority — which is this layer's change — and takes CallID from it, with no
  echo field. `TestHandleLoopApproval_RequiresCurrentExecutionEcho` was dropped with it.
- **Approval-recovery test files** (`approval_replacement_integration_test.go`,
  `approval_timeout_recovery_test.go`, the `approval_restart` e2e scenario) exist only on the non-carried commits;
  the two replayed commits' edits to them had nothing to apply to.

## The one behaviour this base surfaces that the source tree hid

`auto_continue` still defaults to **true** here; on the source tree `14ae0437` had already flipped it to false. With
the tracker gone, auto-continue resolution reads the shared view, so a dispatch whose view is not ready now refuses
a submission (503) instead of silently starting a second loop under the same route. That is `5e0e2259`'s own stated
contract — "AutoContinue also refuses unavailable truth with 503 instead of starting new work" — but at this base it
applies to the default configuration rather than an opt-in one. Three fixtures that construct a component directly
instead of starting one (`newSeamTestComponent`, `newLoopTokenTestComponent`, `newRestartIdentityDispatch`) now set
`AutoContinue = false`: they test refusal precedence, token validation and task identity, never continuation, and
the one subtest that wants continuation builds an activity component over the real KV and opts back in.

## Review round 1: two refusals whose classification this layer owns, and one it does not

**The approval handler now decides on state before `PendingApproval`.** Nothing clears the pending block on a
transition out of `awaiting_approval` — `LoopEntity.TransitionTo` and `LoopManager.CancelLoop` both leave it, and
`persistLoopState` marshals the whole entity — so a loop cancelled mid-approval lands `state: cancelled` *with* a
pending block. `loopOpApprove` deliberately skips the gate's terminal check, so that record reaches the handler, and
reading the combination as incoherence answered 503 "loop record is not readable right now" for a record that read
perfectly. A polling client would retry a permanent state forever. State first makes it the 409 it is.

**Clearing the pending block on a terminal transition is deliberately NOT done here.** It is the other half of the
same defect and it belongs to the layer that owns terminal and adopt transitions (L4, #1330): this change writes no
`LoopEntity` at all, so adding the first write to `CancelLoop` from a dispatch-side fix would put a mutation in the
wrong owner. Dispatch's own answer is correct without it, which is why the fix divides here.

**An unavailable loop view answers with a fixed phrase.** `errs.Wrap` renders `"<Type>.<Op>: <what> failed: <cause>"`,
so returning `err.Error()` from `POST /message` and `GET /loops` shipped
`Component.currentLoopSnapshot: loop projection unavailable failed: …` as the HTTP body — and because
`auto_continue` defaults to true here, a dispatch whose view is still warming reaches it on the *default*
configuration. Internal type and method names are not a client contract; they moved to the log line.

## Declared residuals

- **`loopLookupConflict` and `codeLoopOwnerConflict` are unreachable.** `lookupLoop` has exactly three producers
  (`loop_admission.go:299,:301,:303`) and none of them is the conflict outcome, because there is no second source
  left to conflict with. The vocabulary, the `/loops/{id}` `"500"` OpenAPI response at `http.go:1100` and
  `loop_seams_test.go:630` are retained rather than deleted in this round: removing them edits the generated OpenAPI
  surface, which is a separate reviewable change from the one this PR is. Whoever removes them should do all three
  together.

## Declared cost

The stack's L1 (#1327) is receiving review fixes after this branch was cut. This layer was built on the L1 head
`0053183d` deliberately, so a rebase of the whole stack onto the reviewed L1 head is owed before merge. Nothing here
depends on the fixes' shape; the rebase is bookkeeping, not redesign.
