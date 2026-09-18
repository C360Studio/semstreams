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
applies to the default configuration rather than an opt-in one. Two unit fixtures that construct a component with no
view (`newSeamTestComponent`, `newLoopTokenTestComponent`) now set `AutoContinue = false`: they test refusal
precedence and token validation, never continuation, and a test that wants continuation supplies a view.

## Declared cost

The stack's L1 (#1327) is receiving review fixes after this branch was cut. This layer was built on the L1 head
`0053183d` deliberately, so a rebase of the whole stack onto the reviewed L1 head is owed before merge. Nothing here
depends on the fixes' shape; the rebase is bookkeeping, not redesign.
