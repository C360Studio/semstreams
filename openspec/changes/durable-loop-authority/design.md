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

## Why the MODIFIED block reads ahead of `openspec/specs/`

The `## MODIFIED Requirements` block for "Every dispatch durable input settles through its owner" restates **L2's
delta text** (`openspec/changes/stable-request-identity/specs/agentic-dispatch/spec.md`), not the text currently in
`openspec/specs/agentic-dispatch/spec.md`. This change archives after #1328, so L2's block is the spec that will be
current when this one applies; restating main's would silently revert L2's four edits. An archiver reading this
ahead of L2's merge should expect the two clauses that differ from main — the cancel signal in the PubAck list and
the response-identity disposition on the invalid-input lane — to already be there.

Four of its eight scenarios change. Two of them only replace vocabulary this change deletes — "resolved from the
tracker" becomes "resolved from durable loop authority", and "tracker and gauge state remain unchanged" becomes the
obligation that survives the tracker and the `active_loops` gauge being gone. The other two change a stated REASON
because this change makes the old one false. No outcome moves in any of the four:

- *Task publication succeeds but user response fails* quarantines for one surviving effect, the submission
  counter, rather than two: retiring the tracker removes the tracked `LoopInfo` being replaced under a loop that
  had advanced.
- *A cancel command whose target was resolved rather than named* quarantines because the message does not carry
  the identity the delivery acted on. It no longer quarantines because the resolution would *fall through to the
  user's next live loop*: `activeLoop` matches an exact user/channel route and refuses ambiguity, with no
  user-scoped fallback, so the hazard L1 named cannot occur. The narrower obligation is stated so a later widening
  back to a user-scoped fallback fails the spec and not just a test.

## Declared residuals

- **`loopLookupConflict` and `codeLoopOwnerConflict` are unreachable.** `lookupLoop` has exactly three producers
  (`loop_admission.go:315,:317,:319`) and none of them is the conflict outcome, because there is no second source
  left to conflict with. The vocabulary (`loop_admission.go:31,:170,:228-229`), the `/loops/{id}` `"500"` OpenAPI
  response at `http.go:1143` — the one under `"/loops/{id}"` at `:1123`, NOT the `/loops/{id}/approval` `"500"` at
  `:1182` — and `loop_seams_test.go:630` are retained rather than deleted in this round: removing them edits the
  generated OpenAPI surface, which is a separate reviewable change from the one this PR is. Whoever removes them
  should do all three together.

  Every pin in this bullet was re-derived with `sed -n '<n>p'` against the head it ships on, not transcribed: the
  first three had already drifted by 16 lines within this PR's own round-1 commit. Re-derive before acting on
  them — a stale pin in a "remove these together" note defeats the note.

- **`loopStatusFromFacts`'s empty-state fallback is unreachable** (`commands.go:82-83`). It is the vestige of the
  retired scenario "a record carrying no state is reported as unknown": `validatePersistedLoop` now refuses a record
  whose state fails `isValidLoopState`, so a `loopFacts` reaching `/status` always carries one. Kept as a cheap
  defence rather than deleted, because the alternative is printing an empty field if a future caller builds
  `loopFacts` without validating first. Noted here so it is a recorded vestige, not an unexplained branch.

- **Whether one surviving effect still warrants Quarantine is not decided here.** L1 chose Quarantine for the
  "task published, user response failed" arm when a redelivery would repeat two effects: the submission counter and
  the tracked `LoopInfo` being replaced under a loop that had advanced. On this head the second is gone — loop state
  is durable and its write is idempotent — so a redelivery repeats only `tasks_submitted_total`. The classification
  is carried unchanged rather than relaxed, because relaxing an owner-stop to a retry is a durability decision and
  this change's subject is the authority for loop identity, not the settlement grade of the dispatch response lane.
  `processor/agentic-dispatch/component.go`'s post-PubAck comment points here. A change that owns that arm should
  decide it; a rebase must not.

## Declared cost

This layer was cut on the L1 head `0053183d`, before L1 (#1327) and L2 (#1328) took their review rounds. That debt
is now paid: this branch is rebased onto the reviewed L2 head, which itself sits on the L1 squash on main. Two
carried premises were falsified by the layers below and are corrected in place rather than left standing — L1's
two-conjunct justification for the Quarantine arm (above), and L1's "falls through to the user's next live loop"
reason for quarantining a resolved-target cancel. In both cases the outcome is unchanged and only the stated reason
moves; the residual above records the one that is still open.

PR #1338 stays based on `claude/gh1328-stable-identity` until L2 merges to main.
