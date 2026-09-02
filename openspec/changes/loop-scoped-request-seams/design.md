# Design: loop-scoped request seams

base: `0a40ddf3`

Read `proposal.md` first for the why, the options, and the adopter seam inventory. This file holds the seam
census the target state is built on, the shape of each piece, the invariants a property harness may be authored
from, and the questions that need an owner word before implementation starts.

## The seam census — why one gate

Every seam that today accepts a caller-controlled loop token, and which of the five checks it performs.
`F` form · `E` existence · `O` ownership · `C` classified refusal · `S` observed signal (metric or typed answer).

| Seam | Site | F | E | O | C | S |
|---|---|---|---|---|---|---|
| channel submission (`reply_to` / auto-continue) | `component.go:920-931` | yes | no | no | no | typed response, no metric |
| HTTP submission (`reply_to` / auto-continue) | `http.go:296-311` | yes | no | no | no | typed response, no metric |
| `/cancel` command | `commands.go:53-96` | no | yes | partial | no | typed response, no metric |
| `/status` command | `commands.go:148-172` | no | yes | **no** | no | typed response, no metric |
| `GET /loops/{id}` | `http.go:602-621` | no | yes | **no** | no | status code only |
| `POST /loops/{id}/signal` | `http.go:642-658` | no | yes | **no** | no | status code only |
| `POST /loops/{id}/approval` | `http.go:739-757` | no | yes | **no** | no | status code only |
| `LoopManager.CreateLoopWithID` | `state.go:151-183` | yes | **no** | n/a | no | none |

Six different subsets across eight seams. `canUserControlLoop` (`component.go:1204-1216`) is the only ownership
code in the package and has exactly one caller, `commands.go:73`. That is the shape the gate replaces.

## Piece 1 — the admission gate (#1228 + #1225)

Modelled element-for-element on `processor/graph-ingest/authority_gate.go`.

| Element | graph-ingest precedent | This design |
|---|---|---|
| one home, every seam | `authorizeSubject` `:51`, comment `:38-42` | one `admit…` entry point in a new `loop_admission.go` |
| structural first | `:41-42` | form → existence → ownership, fixed |
| classified refusal | `ValidateEntityIDAuthority` → `errs.ClassifiedCodeDetail` | same constructor, `pkg/errs/errs.go:356` |
| one metric-reason home | `authorityMetricReason` `:55-72` | one mapper, refusal → reason label |
| one named log string | `authorityRejectionLogMessage` `:30-33` | one constant, pinned by test |
| explicit carve-outs | `:44-46` (`@id` objects are never gated) | the ungated-seam list in the spec |
| meter exactly once | `recordDirectAuthorityRejection` `:106-129` | seams return through the gate; no second count |

**Refusal vocabulary.** Codes are the machine-readable half; the metric reason label is a separate, operator-facing
spelling with exactly one mapping home, precisely as `authority_gate.go:11-16` argues. The `Detail` map carries
the seam and the failing field. Reasons: malformed token, absent loop, terminal loop, not owned, not permitted,
conflicting owner, transient unread record.

**Metric.** One `CounterVec` in the existing `router` subsystem, labelled by seam and reason — the same shape as
`mutation_rejections_total{arrival, reason}` (`processor/graph-ingest/component.go:137-141`). Consumer at birth:
the operator, and the spec scenarios that pin the count. New HTTP status codes ride the existing
`recordHTTPRequest` label, so they need no new series.

**#1225's ordering.** Track the loop and move the gauge only after the task message serializes — serialization is
where `TaskMessage.Validate` runs (`message/base_message.go:222-226`), so today both precede the validation that
can fail. Moving them after the marshal and before the publish is safe with respect to the approval-pending
arrival buffer, which `Track` drains (`loop_tracker.go:161-173`) and which exists precisely to absorb that race.
The alternative — untrack and decrement on failure — is a compensating action that a later branch can skip, and
is rejected for that reason.

## Piece 2 — merged existence and ownership (#1227)

The union of the process tracker and the durable record. Neither is authority alone: the tracker is empty after
a replacement (P2), and the durable record may be absent for a live loop because `persistLoopState` is
best-effort (P7). Present in EITHER ⇒ exists.

Reuse, do not re-derive: `getSnapshot` (`loop_tracker.go:197-208`) for the immutable process read — the raw
pointer races concurrent create/approval updates, which is why that method exists; `loadPersistedLoop`
(`terminal_settlement.go:83-111`) for the durable read; `isLoopRecordAbsent` (`terminal_settlement.go:300-302`)
for the absence-versus-failure distinction; `mergeRouteField` (`terminal_settlement.go:39-53`) for reconciling
the owner across the two observations. The bucket name is observed through the declared KV read port
(`config.go:45-51`), never a constant.

Degradation, stated because a design that leaves it implicit gets it wrong: tracker hit ⇒ admit (a durable
failure is irrelevant, the owner is already known); tracker miss + key absent ⇒ not-found; tracker miss +
durable read failing for any other reason ⇒ **transient** refusal, never admit.

**Ownership model — exactly as ruled, no extension.** `continue` requires requester == the loop's owner;
`cancel` requires requester == owner OR in `cancel_any`; `approve` requires membership in the approve list with
**ownership not consulted**; `GET` checks form only. Unknown owner fails closed.

> The ruled model also named a `signal` operation, carrying the same rule as `cancel`. It is gone: owner ruling
> 12 deleted `POST /loops/{id}/signal` rather than repairing it (Piece 5). No other seam ever produced that
> operation, so the gate's vocabulary retires with the endpoint.

**The two lanes.** The user lane is dispatch. The system lane is the rule engine's `publishAgentOnce`
(`processor/rule/actions.go:1691`) and graph-research's `agent.task.research_continuation`
(`frameworkcapabilities/graphresearch/register.go:120-123`, `configs/rules/research-graph/05-continuation.json:24`).
System-lane tasks carry **no** `LoopID` (`actions.go:1713-1720` builds a task with `TaskID`, `Role`, `Model`,
`Prompt`, `WorkflowSlug`, `WorkflowStep` only), so they always mint and never traverse the gate. They must not
be refused for having no owner.

## Piece 3 — the create-vs-exists fence (#1227's other half)

`CreateLoopWithID` keeps its form check first (`state.go:152`) and gains an already-exists refusal before the
three map writes at `state.go:170,171,180`. The shape is `pkg/lifecycle/manager.go:290-297` +
`pkg/lifecycle/errors.go:44-50`: a sentinel a caller branches on, consumed exactly as
`agentic/agentrun/agentrun.go:318` and `gateway/lifecycle-gateway/handlers.go:650` already consume it. The
harness itself is not adopted.

`HandleTask` (`handlers.go:800-841`) branches on it: an already-exists condition means **attach**. Attaching
reuses the loop's existing context manager rather than the freshly constructed one, so the conversation is
preserved; it does not re-add the system prompt (`handlers.go:876-881`) and does not clear the pending-tool set.
A terminal existing loop is refused, not attached.

**Redelivery dedup must survive.** Intake dedupes on `TaskID` via `HasActiveLoopForTask` (`state.go:187-198`),
and dispatch mints a fresh `TaskID` per submission (`component.go:951`, `http.go:330`), so the dedup cannot fire
across two distinct submissions naming one loop — which is why the fence is needed at all. After an attach, the
loop entity's task association must reflect the continuation, or a redelivery of that continuation is processed
twice. The spec requires the property; the mechanism is the implementer's.

## Piece 4 — terminal release of per-loop state (#1233)

`LoopManager` holds eleven per-loop maps. `DeleteLoop` (`state.go:340-354`) clears them and has zero production
callers (P12), so a process retains every conversation it has ever run.

**The release does not go at the terminal transition.** It goes at `releaseLoopTransientState`
(`trajectory_handler_wiring.go:52-55`), which already exists, is already the single terminal-release point by
its own doc comment (`:38-42`: *"the single terminal-release point so a future terminal path cannot release one
and leak the other"*), is already idempotent, and is already deferred by all four production terminal paths
(`component.go:1447`, `:1613`, `:1813`, `:2128`) **after** the terminal observation and terminal graph write,
both of which read the entity. Putting the release anywhere else re-derives placement reasoning that is already
written down and already correct.

**What still reads `LoopManager` after terminal — enumerated, not assumed.**

| Reader | Site | Source | Effect of release |
|---|---|---|---|
| terminal trajectory observation | `component.go:1463`, `trajectory_handler_wiring.go:81` | in-memory | runs BEFORE the deferred release; unaffected |
| terminal graph stamp / durable persist | `component.go:1617-1625`, `:1989-2009` | in-memory | runs BEFORE the deferred release; unaffected |
| loop-result tool (`read_loop_result`) | `processor/agentic-tools/loop_result.go:34-50` | **durable** `AGENT_LOOPS` | unaffected |
| trajectory query | `component.go:2014` | **durable** KV | unaffected |
| approval-timeout sweeper | `state.go:253-278` | in-memory | unaffected: it skips any loop not `awaiting_approval` (`:258`), and a terminal loop is never a candidate |
| dispatch completion routing | `terminal_settlement.go` | tracker + **durable** | different process; unaffected |
| task-redelivery dedup | `state.go:187-198` | in-memory | strictly improved — it already filters to non-terminal |
| **late/duplicate approval response** | `approval_response_handler.go:57-75` | in-memory | **changes**: `ResolveApprovalIfPending` (`state.go:321-323`) errors on an absent loop, where a present-but-terminal loop returns a clean stale-drop |
| **late tool result / late model response** | `component.go:1806-1817` | in-memory | **changes**: an absent loop errors where a terminal one is handled |

So the release is admissible only under one invariant, and the spec states it: **after a loop settles, absence
of its in-process entity is indistinguishable from presence in a terminal state.** The three late-arrival
readers must treat not-found as an expected settled-drop. This is not a new burden — that exact case is already
reachable today whenever a process replacement precedes the late message — but it is currently handled by
accident rather than by contract, and the release makes it common.

Interaction with piece 3, stated so it is not later read as a defect: once a settled loop's entity is released,
a direct create against its token no longer collides in process memory. The refusal of an attach to a settled
loop is therefore owned by the gate, which decides from the durable record; the in-process fence is defence in
depth for the window before release.

## Invariants

Stated as properties over all inputs, each with its spec home. These are the only admissible source for a
property or fuzz harness; a property authored later by reading the implementation proves nothing.

| # | Invariant | Spec home |
|---|---|---|
| I1 | For every seam and every input, a malformed token is refused with the form reason, whatever the loop's existence or ownership | `agentic-dispatch` — one-gate requirement, first two scenarios |
| I2 | For every input, an absent loop is refused with the not-found reason, whatever the requester | same requirement, second scenario |
| I3 | For every refused request, exactly one refusal counter increment occurs | same requirement, third scenario |
| I4 | No refused request changes any loop's recorded owner, or any active-loop index | ownership requirement, first scenario |
| I5 | For every submission **refused before publication** — form, validation, or addressing — the tracker and the active-loops gauge are unchanged from before it | refused-submission requirement |
| I6 | For every accepted continuation, the loop's context-manager identity is the same object as before | `agentic-loop` — fence requirement, first scenario |
| I7 | For every refused create, all three per-loop maps hold the values they held before the call | fence requirement, second scenario |
| I8 | After terminal release, for every reader, an absent loop and a present terminal loop produce the same observable outcome | `agentic-loop` — release requirement, third scenario |
| I9 | Terminal release is idempotent: applying it n times equals applying it once | release requirement, first scenario |
| I10 | Every message published on `agent.signal.<loop_id>`, from any lane, decodes to exactly one payload type | `agentic-dispatch` — control-signal requirement, first scenario |
| I11 | For every refused cancel request, nothing is published on the loop's signal subject | control-signal requirement, fourth scenario |

## Decision skills applied

- **`/kv-or-stream`** — no new communication path. The gate reads an existing KV bucket through an already
  declared read port; no subject and no stream is added. Outcome: not triggered beyond confirming that.
- **`/entity-or-bucket`** — no new durable state. Ownership is read from facts that already exist in
  `AGENT_LOOPS` (`agentic/state.go:82-84`, written via `handlers.go:495` → `state.go:929-940`). Outcome: no new
  bucket, no new triple.
- **`/orchestration-check`** — the gate is component-internal admission, not orchestration. No rule, no
  lifecycle participation, no new primitive. Outcome: component boundary, correct.
- **`/new-payload`** — no new message type. Four existing payload `Validate` methods gain a check.
- **`/query-pattern`** — no new query access.

## Context ownership

No production struct retains a `context.Context`. Every seam the gate is called from already receives one:
`handleTaskSubmission(ctx, …)` `component.go:905`, `handleCommand(ctx, …)` `:743`,
`processTaskSubmissionSync(ctx, …)` `http.go:282`, `processCommandSync(ctx, …)` `http.go:205`, and each HTTP
handler via `withRequestID(w, r)`. The gate takes `ctx` as its first argument for the durable read. Terminal
release takes none. No root is created anywhere in this change.

## Rulings (owner, 2026-09-01, on #1227) — settled, do not re-litigate

Three questions were raised by this design pass. All three were ruled the same day, verbatim: *"1. confirm
fail-closed 2. confirm 3. fold it in"*. They are recorded here as answers so a later reader does not reopen
them.

**R1 — a user-lane request naming a system-lane (ownerless) loop is REFUSED. Fail-closed, confirmed.**
Ruling 4 (unknown owner ⇒ fail closed) and the two-lane ruling (a system-lane loop must not be refused for
having no owner) appear to meet here; they do not. The two-lane ruling is about system-lane **traffic** — a
rule-spawned or research-continuation task, which is published straight to `agent.task.*` and never traverses
the user-lane gate at all (`processor/rule/actions.go:1713-1720` builds a task with no `LoopID`). It is not a
licence to admit user-lane requests to ownerless loops. So a user continuing, cancelling, or signalling a
rule-spawned loop by its token is refused, and a system-lane task is never owner-checked because it never
reaches the check.

**R2 — `Permissions.CancelOwn` keeps exactly one home, the command lane. Confirmed as designed.**
The ruled model for `cancel`/`signal` is owner OR `cancel_any` and does not mention `CancelOwn`. Today
`CancelOwn` is consulted twice: as the `/cancel` command's declared permission (`commands.go:22` →
`component.go:759` → `component.go:1182`) and again inside `canUserControlLoop` (`component.go:1216`). The
second consult is deleted with `canUserControlLoop`; the first stays. **Accepted consequence, recorded so it is
not read later as an oversight:** `CancelOwn: false` still denies the `/cancel` chat command. The second lane
this sentence originally contrasted it with, `POST /loops/{id}/signal`, no longer exists (ruling 12), so the
command lane is now the only lane that consults `CancelOwn` at all.

**R3 — the signal payload unification is FOLDED IN.** See Piece 5 below.

## Piece 5 — one control-signal payload on the loop signal subject (folded in by R3)

> **Superseded in part by owner ruling 12 (2026-09-01): the endpoint is DELETED, not repaired.** The retirement
> of `SignalMessage` below stands unchanged and is what landed — the defect was always *two payload types on one
> subject*, and deleting the only producer of the second one fixes it. What did **not** land is the repair of
> `SendSignal` and the endpoint; see *What replaced the repair* at the end of this piece. The evidence below is
> kept because it is the measurement the ruling rests on.

Two payload types travel `agent.signal.<loop_id>`:

| | `agentic.UserSignal` | `agenticdispatch.SignalMessage` |
|---|---|---|
| category | `signal` (`agentic/constants.go:13`) | `signal_message` (`agentic/constants.go:24`) |
| declared | `agentic/user_types.go:111-160` | `processor/agentic-dispatch/loop_tracker.go:585-615` |
| fields | signal id, type, loop id, user id, channel type, channel id, payload, timestamp | loop id, type, reason, timestamp |
| `Validate` | signal id, type against a closed verb set, loop id, user id (`user_types.go:124-142`) | `return nil` (`loop_tracker.go:594-596`) |
| producers | `commands.go:112`; **sisters** `semdragon/…/bossbattle/handler.go:1106`, `semsage/…/ui-api/http.go:195` | `loop_tracker.go:621` — one, dispatch's own HTTP lane |
| consumers | `component.go:2077` → `handleCancelSignal` `:2106`, `handlePauseSignal` `:2192`, `handleResumeSignal` `:2232` | **none** |
| rule-readable | yes — `agentic/rule_fields.go:293` | no |
| registered by | `agentic/payload_registry.go:36` | `processor/agentic-dispatch/payload_registry.go:41-53` |

**Direction: retire `SignalMessage`.** Four reasons from the code, none of them convenience:

1. **Zero consumers.** Retiring it removes no reader. Repairing it would repair something nothing reads.
2. **It cannot satisfy this change's own ownership requirement.** It has no user id, no channel route, and no
   signal id. Keeping it means either growing it into a copy of `UserSignal` or exempting the signal seam from
   the ownership model — and R2 keeps the seam inside the model.
3. **`UserSignal` is the consumed, rule-readable, documented type** (`processor/agentic-loop/doc.go:86`).
4. **The outside world already agrees.** Both sister producers on this subject construct `UserSignal`. Dispatch
   is the outlier, not the standard.

**What retires:** `SignalMessage` and its four methods, `buildSignalMessage`
(`processor/agentic-dispatch/payload_registry.go:12-37`), `agenticdispatch.RegisterPayloads` (its only
registration was that type), its call at `payloadbuiltins/register.go:47`, and the
`agentic.CategorySignalMessage` token (`agentic/constants.go:24`). Per the payload-registry checklist there is
no `init()` and no singleton to unwind — the registration is one explicit call from one composition root, which
is exactly why removing it is a three-line change and not a migration.

**What replaced the repair.** `SendSignal` was to publish a `UserSignal` built from the requester identity, the
loop's merged channel route, and the endpoint's `reason`. That work was implemented and pushed
(`c3998558`, `bec06d23`) before ruling 12 replaced it with deletion, and it is gone again. The reasoning that
decided it, from the measurement:

- **`cancel` was already served.** `POST /message` with `/cancel <loop_id>` routes `processMessageSync` →
  `processCommandSync` → `handleCancelCommand`, publishing a correct `UserSignal`. Same HTTP surface, already
  permissioned, working the whole time — which is why nobody noticed the endpoint was dead.
- **`pause` and `resume` were never implemented.** `handlePauseSignal` sets `entity.PauseRequested = true`; that
  field is written twice (`component.go:2225,2266`) and read nowhere. Filed as **#1239**.
- So the endpoint had one real verb with a working alternative, and two that were advertised-absent. Deleting it
  removes adopter surface instead of adding the `user_id` field the repair would have needed on `SignalRequest`,
  and dissolves the identity question that field raised.

`LoopTracker.SendSignal` goes with it (its only production caller was the handler), as do the gate's
`seamHTTPLoopSignal` and `loopOpSignal` tokens. `TestLoopSignalEndpointIsGone` pins the absence on both the route
table and the generated OpenAPI document, because a reintroduction can arrive on either surface alone.

**Two interpreters of "which signal verbs exist", left alone deliberately.** `UserSignal.Validate` admits seven
verbs (`user_types.go:139-141`); the loop's switch handles three and warns on the rest
(`component.go:2091-2103`). These are different layers — payload grammar ⊇ handler coverage — not two spellings
of one fact. Recorded so the next reader does not "unify" them into a behaviour change nobody asked for. (A third
interpreter, the deleted endpoint's own `pause|resume|cancel` allow-list, went with it under ruling 12; of the
three verbs it named, two are the unimplemented pair now tracked by **#1239**.)

**Effect on the gate design.** The repair would have made the signal seam load-bearing; the deletion removes the
seam instead. What survives from that analysis is the sequencing rule it produced, and it still governed the work:
the unification could not land before the gate, or the seam would have gone live ungated for the length of that
window. §4 landed at `660ab88a`, ahead of both the repair and its replacement.

## Superseded — the questions as originally raised

Ruled above on 2026-09-01. Retained only as the record of what was asked.

### As raised

1. **A user-lane request naming a system-lane loop.** Ruling 4 (unknown owner ⇒ fail closed) and the two-lane
   ruling (a system-lane loop must not be refused for having no owner) meet here. The design implements
   fail-closed: a user continuing or cancelling a rule-spawned, ownerless loop by its token is REFUSED. Reading
   the two-lane ruling as being about system-lane *traffic* — which never reaches the gate — makes them
   consistent. Confirm, because the alternative reading admits it.

2. **`Permissions.CancelOwn` across the two lanes.** The ruled model for `cancel`/`signal` is owner OR
   `cancel_any`; it does not mention `CancelOwn`. Today `CancelOwn` is consulted twice: once as the `/cancel`
   command's declared permission (`commands.go:22` → `component.go:759` → `component.go:1182-1183`) and again
   inside `canUserControlLoop` (`component.go:1216`). The design keeps the first — one home for the fact — and
   drops the second, which implements the ruling verbatim. Consequence: `CancelOwn: false` still denies the
   `/cancel` command, and does NOT deny the HTTP signal endpoint, which has never consulted it. Confirm, or rule
   that `CancelOwn` gates both lanes.

3. **`POST /loops/{id}/signal` is already inert, and this change would gate a path that does nothing.** Found
   while verifying the inventories, not previously recorded. That endpoint publishes a
   `agenticdispatch.SignalMessage` (`loop_tracker.go:616-648`, category `signal_message`) on
   `agent.signal.<loopID>`, but agentic-loop's only handler for that port asserts the payload is an
   `*agentic.UserSignal` (`processor/agentic-loop/component.go:2077-2081`, category `signal`) and logs
   *"Unexpected payload type"* and returns otherwise. The chat `/cancel` command publishes the correct type
   (`commands.go:112`). So the HTTP signal endpoint answers `200 Accepted: true` and the loop drops the message.
   It is the same subject carrying two payload types — one home per interpreted fact, violated. It sits exactly
   on a seam this change gates. Rule: fold the type unification into this change, or file it and gate the seam
   as-is.
