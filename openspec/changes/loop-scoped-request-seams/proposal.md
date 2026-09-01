# Change: One admission gate for every request that names a loop

Claim: PR #1231, branch `claude/gh1227-loop-seams`. Closes #1227, #1228, #1225, #1233.
Premises re-pinned at `main@0a40ddf3` across `inventory-attach.md`, `inventory-carriers.md`,
`inventory-precedent.md`, `findings-decisive-questions.md`, and `inventory-verification.md`.

Owner rulings, recorded on #1227 (2026-09-01): scope and sequencing; **preserve, do not restore**;
**trajectory evidence is inadmissible as an execution dependency**; unknown owner **fails closed**; attach to a
terminal loop is **refused**; and the exact ownership model reproduced below. Scope addition, same date: #1233
joins this change. Do not re-open any of them.

## Why

Three open issues sit on one code path and are three holes in the same missing seam. A request arrives naming an
existing loop — continue it, cancel it, signal it, approve a tool call in it, read it — and **no single place
admits it**. Each seam hand-rolls whichever subset of {form, existence, ownership, classified refusal, observed
signal} its author happened to think of, and the issues are the empty subsets.

- **#1227** — a continuation by `reply_to` gets no existence and no ownership check. A second holder of the token
  takes over the tracker record and its `UserID` (`loop_tracker.go:148-157`), so the original user's completion is
  delivered to the second user (`component.go:1163-1171`), and `CreateLoopWithID` overwrites the in-flight
  conversation (`state.go:170-180`).
- **#1228** — loop-token FORM is enforced at exactly four seams. Every other carrier checks non-emptiness, and
  dispatch's own `SignalMessage.Validate` (`processor/agentic-dispatch/loop_tracker.go:594-596`) is an
  unconditional `return nil`. Two carriers the issue body does not name are the URL-path loop id on
  `handleLoopSignal` and `handleLoopApproval` (`http.go:646-647,743-744`).
- **#1225** — a submission whose task fails payload validation is a silent drop: no answer to the submitter, no
  metric, and a leaked `activeLoops` gauge, because `Track` (`component.go:957`) and `recordLoopStarted`
  (`component.go:971`) both run before the marshal that validates (`message/base_message.go:222-226`).

**#1225 and #1233 are the same shape**: terminal state does not release what start acquired. #1225 leaks a
gauge counter on a submission that never became a loop; #1233 leaks the loop's entire in-process footprint on
every loop that ever ran. Both are fixed by putting the release on the path that already owns the transition.

Two measured facts decide the design, and neither was established when the issues were written
(`findings-decisive-questions.md`):

1. **The dispatch tracker does not rehydrate.** Both constructors build empty maps
   (`loop_tracker.go:118-136`); `Start` reads no bucket before marking started (`component.go:337-403`). The
   HTTP attach path's own comment says so (`http.go:750-753`). So an existence check that consults process
   memory alone would refuse every legitimate post-restart resume — but that refusal already happens today, as a
   bare 404.
2. **A continuation discards the conversation.** `CreateLoopWithID` replaces `m.loops`, `m.pendingTools`, and
   `m.contextManagers` unconditionally (`state.go:170-180`), and `HandleTask` re-seeds the fresh manager with the
   system prompt and the new turn only (`handlers.go:875-887`). Nothing merges prior history. AutoContinue rides
   the same path (`component.go:924`), and `GetActiveLoop` filters to non-terminal loops
   (`loop_tracker.go:209-231`), so the overwrite lands on **live** state. That reclassifies part of #1227 from
   hardening to a defect on the happy path.

A third and a fourth were found while verifying the inventories. **The HTTP signal endpoint has never worked.**
`LoopTracker.SendSignal` publishes an `agenticdispatch.SignalMessage` (category `signal_message`,
`loop_tracker.go:600`) on `agent.signal.<loopID>` (`:634`), while agentic-loop's only handler for that port
(`component.go:898`) asserts `*agentic.UserSignal` (category `signal`) at `component.go:2077` and logs
*"Unexpected payload type"* otherwise. The chat `/cancel` command publishes the correct type
(`commands.go:112`). So `POST /loops/{id}/signal` answers `200 {"accepted": true}` and the loop is never
paused, resumed, or cancelled. Two payload types on one subject is one home per interpreted fact, violated.

And **`LoopManager` never releases anything.** `DeleteLoop`
(`state.go:340-354`) is the only site that clears eleven per-loop maps and has zero production callers — its only
references are `recovery_test.go:530` and `state_test.go:202,218,233`. Every loop the process ever runs keeps its
full conversation until the process exits. That is #1233.

The framework already has the answer to all of it, one plane over. `processor/graph-ingest/authority_gate.go` is
a fully realized admission gate: one home called at every seam, structural check ordered first so an authority
reason never masks a malformed candidate (`authority_gate.go:38-42`), classified refusal, one home for the
metric-reason mapping (`:55-72`), and one named log constant a test can pin (`:30-33`). The agentic plane never
adopted it — `processor/agentic-dispatch` has **zero** `errs.Classified*` uses across 34 `errs.` references, the
uncoded `Wrap*` family only. That is the mechanical reason #1225 is a silent drop.

## What changes

- **One admission gate in `processor/agentic-dispatch`**, modelled on `authority_gate.go`: one home called at
  every seam that names a loop; **form, then existence, then ownership**, in that fixed order; a classified
  refusal carrying a `Code` and a `Detail`; one home for the metric-reason mapping; one named log constant; and
  an explicit list of the seams deliberately left ungated with the reason for each. `processor/agentic-dispatch`
  adopts the `errs.Classified*` family.
- **Existence and ownership are decided from merged facts** — the process tracker ∪ the durable `AGENT_LOOPS`
  record — reusing the reconcile shape at `terminal_settlement.go:39-53,56-81` and the existing durable reader at
  `terminal_settlement.go:83-111`. Pure read-through would be unsound: `persistLoopState`
  (`processor/agentic-loop/component.go:1989-2009`) is best-effort and absence is a legitimate state
  (`terminal_settlement.go:296-302`), so a live loop may have no durable record.
- **Form enforcement extended to every remaining carrier** (#1228): the user control signal, the approval
  response, the approval-pending event, dispatch's own control-signal message, and the URL-path loop id on all
  three loop endpoints.
- **A create-vs-exists fence at `LoopManager`** (#1227's other half): `CreateLoopWithID` refuses an existing
  token with a distinguishable already-exists condition instead of overwriting, adopting the **shape** of
  `pkg/lifecycle/manager.go:290-297` and `pkg/errs/errs.go:386`. Task intake branches on it and **attaches**,
  reusing the existing context manager. That is the fix for the continuation-discards-context defect.
- **Terminal release of per-loop in-process state** (#1233), at the component's existing single terminal-release
  point (`trajectory_handler_wiring.go:52-55`), which four production sites already defer to
  (`component.go:1447,1613,1813,2128`) after the terminal observation and terminal graph write have returned.
- **#1225's ordering fix**: track the loop and move the gauge only after the task serializes, and answer the
  submitter with a typed error naming the field on both paths.
- **One control-signal payload on the loop signal subject.** Dispatch publishes `agentic.UserSignal` on both
  lanes; `agenticdispatch.SignalMessage`, its builder, and the `agentic.CategorySignalMessage` token are
  retired. The direction is decided by the code, not by convenience: `SignalMessage` has **one producer and zero
  consumers**, carries no requester identity, no channel route and no signal id — so it cannot satisfy the
  ownership model this change requires of the signal seam — and its `Validate` returns nil unconditionally.
  `UserSignal` is the type the loop actually consumes, the type that is rule-readable
  (`agentic/rule_fields.go:293`), the type the loop package documents (`processor/agentic-loop/doc.go:86`), and
  the type **both sister producers already publish** on this subject.
- **`approve` becomes load-bearing.** `Permissions.Approve` (`config.go:37`) is reachable through
  `hasPermission` (`component.go:1186`) and has zero call sites; the approval endpoint checks nothing. The
  default is `Approve: []string{"*"}` (`config.go:101`), so **no deployment's behaviour changes by default**.

## Consequences an operator will see

**BREAKING, wire behaviour:** a `reply_to` naming a loop that does not exist is now refused rather than silently
minting a loop under the client's token. A canonical UUID still passes the FORM check — that contract is
unchanged — but a token this framework never minted names no loop to continue.
`TestCanonicalReplyToContinuesTheLoop` asserts today's behaviour and is replaced.

**BEHAVIOUR CHANGE, not a type cleanup: `POST /loops/{id}/signal` starts working.** Today it returns
`200 {"accepted": true}` and the loop ignores the message. After this change the same call actually pauses,
resumes, or cancels the loop. An operator or product shell that has been calling this endpoint — and reasonably
concluded from the `200` that it worked — will see loops respond for the first time. Anyone who built a
workaround around its silence (polling, a second cancel path, a rule that force-terminates) now has two things
cancelling the same loop. This is the single most user-visible effect in the change and it needs to lead the
migration note, ahead of the `reply_to` refusal.

**BREAKING, exported Go surface:** `agenticdispatch.SignalMessage` and `agentic.CategorySignalMessage` are
deleted, and `agenticdispatch.RegisterPayloads` — whose only registration was that type — goes with them, along
with its call at `payloadbuiltins/register.go:47`. `LoopTracker.SendSignal`'s signature changes to carry the
requester identity the published payload now requires. No repository in the sister sweep constructs
`SignalMessage`; both sisters that publish on this subject already construct `agentic.UserSignal`
(`semdragon/processor/bossbattle/handler.go:1106`, `semsage/processor/ui-api/http.go:195`), so the retirement
costs them nothing. Those two also become subject to the newly enforced loop-token form check on
`UserSignal.Validate`; both pass it today because their tokens are framework-minted. Recorded here as the
migration note's content — this repository does not change theirs.

## Non-goals

- **Restore across process replacement.** The fence preserves an in-process conversation. Reconstructing one
  whose process was replaced is #1146 / PR #1159 and is not designed here. That claim's dispatch delta re-cuts
  to CITE this change's merged-read requirement rather than own it.
- **Authorization.** These are correctness and accident-prevention guards. `IdentityFromRequest`
  (`identity.go:52-59`) resolves identity from product middleware, else the request body's claimed `user_id`,
  else a fixed default — it is caller-asserted and nothing verifies it. Authorization waits on the beta.166 auth
  milestone (epic #1205).
- **Any read of agent execution evidence.** `AGENT_TRAJECTORIES` and its ObjectStore evidence stay write-only
  from execution's side. No admission decision may depend on them.
- **Re-homing loops onto the Lifecycle harness.** Assessed and ruled a reach; the `ErrAlreadyExists` /
  `ErrRevisionMismatch` **shape** is adopted, the harness is not.
- **Scoping loop reads by owner.** `GET /loops/{id}` gains the form check only.

## Options considered

- **A0 — do nothing.** Leaves a live conversation-destroying overwrite on the ordinary multi-turn path, a
  silent drop with a leaked gauge, an unbounded per-process leak, and an advertised permission that does
  nothing. Rejected.
- **A1 (recommended) — one gate, merged facts, a create-vs-exists fence, terminal release** (this proposal).
- **A2 — three independent fixes, one per issue.** Cheaper to review in isolation and it is what the issues
  literally ask for. Rejected: it produces a fourth, fifth, and sixth hand-rolled subset of the same five
  checks on the same seams, which is the defect the issues are symptoms of. The measured evidence for that is
  the census itself — seven seams, seven different subsets.
- **A3 — pure read-through to `AGENT_LOOPS` at every attach seam** (the shape PR #1159 proposes for
  restart-safety). Rejected as unsound on its own: `persistLoopState` is best-effort and absence is a
  legitimate state, so a live loop may have no durable record and read-through would refuse it. Merge subsumes
  it; #1159's requirement is cited rather than duplicated.
- **A4 — re-home the loop onto `pkg/lifecycle`.** Would buy the create-vs-exists distinction, revision
  fencing, and durable participant state for free. Rejected as a reach: ADR-049 makes participation a property
  of the ENTITY, the loop's state machine is not a workflow phase machine, and the migration is far larger than
  the four issues. The shape transfers; the harness does not.
- **A5 — release per-loop state by calling `DeleteLoop` at the terminal transition.** Rejected in favour of
  the existing single terminal-release point: `DeleteLoop` at the transition would run BEFORE the terminal
  observation and terminal graph write, both of which read the entity it deletes.

## Adopter seam inventory

Answered as a specific person: a developer outside this repo writing a product shell or a chat client against
dispatch's HTTP and channel surfaces, who has never opened any file named above.

### 1. What must they know?

- **One new operational fact:** a loop belongs to the `user_id` that created it — send the same one when you
  continue, cancel, or signal it.
- Two facts that are consequences of it rather than separate debts: an approval is checked against the
  approve list and NOT against ownership (so a reviewer who did not start the loop can approve it — that is the
  point); and `reply_to` must name a loop that exists (you can no longer seed your own loop id).
- One caveat, not an action: `user_id` is asserted, not authenticated. These guards prevent accidents. They do
  not isolate untrusted parties.
- For a **component author** rather than a client: `agenticdispatch.SignalMessage`,
  `agentic.CategorySignalMessage`, and `agenticdispatch.RegisterPayloads` no longer exist. Build
  `agentic.UserSignal` instead — which is what the two sister producers on this subject already do, so this
  debt is owed by nobody currently in the tree.

The raw list is four items, which the contract calls a design finding rather than a documentation task. It
compresses to one because items two and three are entailments of item one and item four requires nothing of
them. Stated honestly: **the adopter carries one new fact.**

### 2. What happens if they do nothing?

Traced for someone who learns none of the above:

| Client behaviour | Today | After |
|---|---|---|
| Sends a consistent `user_id` (the common case) | works | works, unchanged |
| Sends no `user_id` over HTTP | defaults to `http-user` on both create and continue | same default on both, so it still matches — still works |
| Sends the end user's id on create, a service id on continue | silently takes over the loop, and the original user's completion is delivered to the service identity | typed refusal naming the reason; the loop is untouched |
| Authors its own `reply_to` UUID | a loop is silently minted under it | typed refusal: no such loop |
| Resumes by `reply_to` after dispatch was replaced | HTTP endpoints answer 404; the submission path silently forks a NEW loop under the same token | admitted from the durable record and genuinely continued |
| Approves a tool call without the approve permission | admitted — the permission is advertised and unread | refused, unless the default `["*"]` is in force, which it is |
| Calls `POST /loops/{id}/signal` to cancel a loop | `200 accepted:true`, loop keeps running | `200`, and the loop actually cancels — see the consequences section |
| Constructs `agenticdispatch.SignalMessage` | compiles | **compile error** — the type is gone; no in-tree or sister caller does this |

Row five is the important one: today the adopter must **predict** whether dispatch still holds their loop in
memory, and gets a silent fork when they predict wrong. After, the framework observes the durable record. That
prediction is deleted, not documented.

Nothing on this table is a silent loss after the change; every refusal is typed and counted.

### 3. Where do they find out?

For the retired Go surface, the answer is the best rank available: **compile error**. For everything else,
ranked honestly: **typed runtime error**, on the lane they are already listening to — a synchronous HTTP body
with a status code (`400` malformed, `404` absent, `403` not permitted or not owned — new on these endpoints,
`409` terminal, joining the `409` the approval endpoint already returns), or a typed error response on the
response subject for the channel path. Behind that, an operator sees one refusal counter labelled by seam and
reason, and one named WARN string. Nothing lands at "doc" or "nowhere".

### 4. What SHOULD they have to know?

Ideally nothing, and the gap is nameable. The framework cannot OBSERVE who is asking, because identity is
caller-asserted (`identity.go:52-59`); so it must ask the adopter to be consistent, which is a
prediction-shaped ask. That residual gap is exactly the scope of epic #1205, and it is the reason this
capability's spec states in its own words that it is not authorization. What the change does delete is the
other prediction on this surface — whether the loop is still in dispatch's memory — which the framework now
observes for them — and the second: an operator no longer has to know that one of the two documented ways to
cancel a loop silently does nothing. A surface that reports success and does nothing is the worst rank on the
list, below "nowhere", because it actively teaches the wrong fact.

## Premises (each measured; pins in the inventory files)

| # | Premise | Measurement |
|---|---|---|
| P1 | No seam decides existence, ownership, and form in one place | seven-seam census, `inventory-attach.md` §Attach entry points |
| P2 | The dispatch tracker never rehydrates | `loop_tracker.go:118-136`, `component.go:337-403`, `http.go:750-753` |
| P3 | A continuation discards the conversation | `state.go:170-180`, `handlers.go:875-887` |
| P4 | AutoContinue rides the same overwrite, on live state | `component.go:924`, `loop_tracker.go:209-231` |
| P5 | Dispatch uses no classified errors at all | `git grep -c 'errs.Classified' -- processor/agentic-dispatch/` → 0 files |
| P6 | `Track` and the gauge precede the validating marshal | `component.go:957,971,975`; `message/base_message.go:222-226` |
| P7 | `persistLoopState` is best-effort; absence is legitimate | `processor/agentic-loop/component.go:1989-2009`; `terminal_settlement.go:296-302` |
| P8 | The durable record carries the owner, so post-restart ownership is decidable | `agentic/state.go:82-84`; `handlers.go:495` → `state.go:929-940` |
| P9 | `approve` is advertised and unread | `config.go:37`, `component.go:1186`; no call site passes `"approve"` |
| P10 | Defaults admit everyone to approve | `config.go:101` — `Approve: []string{"*"}` |
| P11 | System-lane tasks carry no loop id, so they always mint | `processor/rule/actions.go:1713-1720` builds a task with no `LoopID` |
| P12 | `DeleteLoop` has zero production callers | `git grep DeleteLoop` → declaration + `recovery_test.go:530`, `state_test.go:202,218,233` |
| P13 | A single terminal-release point already exists, correctly placed | `trajectory_handler_wiring.go:35-55`; deferred at `component.go:1447,1613,1813,2128` |
| P14 | The loop-result tool reads the durable record, not the map | `processor/agentic-tools/loop_result.go:34-50` |
| P15 | Approval sweeping cannot lose a candidate to terminal release | `state.go:258` skips any loop not awaiting approval |
| P16 | `SignalMessage` has one producer and zero consumers | producer `loop_tracker.go:621`; `git grep SignalMessage` shows no decode/type-assert anywhere |
| P17 | The loop's only `agent.signal` handler accepts `*agentic.UserSignal` and drops the rest | `component.go:898` → `:2077`, `:2079` |
| P18 | `SignalMessage` carries no identity, route, or signal id | `loop_tracker.go:586-591` — `LoopID`, `Type`, `Reason`, `Timestamp` only |
| P19 | Dispatch registers exactly one payload, and it is the retired type | `processor/agentic-dispatch/payload_registry.go:41-53`; one `reg.Register` call; one caller at `payloadbuiltins/register.go:47` |
| P20 | Both sister producers on this subject already publish `UserSignal` | `semdragon/processor/bossbattle/handler.go:1106`, `semsage/processor/ui-api/http.go:195` (read-only sweep) |

## Capabilities touched

`agentic-dispatch` (**new capability, seeded here** — ADDED ×7), `agentic-loop` (ADDED ×2), `entity-id-contract`
(MODIFIED ×1 — the seam census and the isolation caveat).

## Coordination and gates

- **PR #1159 (Codex, `codex/gh1146-agentic-loop-restart`)** is a separate claim owning restart-safety and
  guaranteed persistence, on hold. Its dispatch delta re-cuts to CITE the merged-read requirement here rather
  than own it. It also seeds an `agentic-dispatch` capability spec; whichever change archives second reconciles
  the one `## Purpose` rather than adding a second.
- **Breaking ⇒ e2e gate:** `task e2e:agentic` covers the dispatch → loop attach path and MUST be green before
  the breaking commit lands.
- Three items need an owner word before implementation starts; they are stated as questions in `design.md`
  §Open questions, not resolved here.
