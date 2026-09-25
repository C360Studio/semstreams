# agentic-loop — delta (#1365 + #1345, durable accepted input)

> One MODIFIED requirement (ruling 2/3, #1146 issuecomment-5828511934) plus one REMOVED requirement. The MODIFIED block
> restates `openspec/specs/agentic-loop/spec.md:886-1102` (`9e5d8455`) in full — the requirement text and all
> twenty-two existing scenarios under their exact headings (openspec 1.7.0 refuses a MODIFIED block that omits a
> current scenario) — changes the bodies of three (`A refusal before any mutation is retried…`, `The task lane's
> results settle…`, `The deferred continuation's replacement behaviour is owed to #1365`), adds four, and carries in
> verbatim the three scenarios of the removed requirement that were never about the exemption. The REMOVED
> requirement is `Task intake is the one loop input class this layer does not convert` (`spec.md:1104-1143`): its
> premise ends with this change, and two of its sentences were already false at `9e5d8455` (inventory § Fact 4).
> Its one sentence that still states a fact (`spec.md:1112-1113`, the transient-lineage resume) is carried into the
> MODIFIED task-lane scenario. Two existing headings read false after archive (`…its errors stay exempt`, `…is owed to
> #1365`): openspec cannot rename a scenario inside a MODIFIED block, and ruling 2 forbids the REMOVED + ADDED pair
> that could — design § 0 OQ0 names this as ruling 2's cost. Scenario bodies assume design § 0's pre-selections: OQ1
> (a) one text field, OQ2 (a) the observed ceiling over the whole record, OQ3 (a) a busy refusal stays acknowledged,
> OQ4 (a) a logged duplicate in the one window the record cannot tell apart, OQ5 (a) `task_prompt` is birth-only.
> Each alternative names the clause it would change in design § 0.

## MODIFIED Requirements

### Requirement: Loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners, and SHALL NOT positively acknowledge a delivery on a converted
class before that lane's durable effect has committed. Task intake is classified through the same owner and, since
#1345, settles by the same rule. Task, response, and tool-result SHALL use the permanent typed heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or work-owning no-heartbeat
adapter.

Each non-heartbeat physical subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; agentic-loop
SHALL NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout.

Decode, correlation, KV, Store, transition, and required publication failures on a converted class SHALL NOT become
successful callback completion. ACK means the lane-specific durable transition or defined refusal and every required
PubAck completed; Retry means stable identity and reconciliation make re-execution safe; Terminate means permanently
invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure
prevents a safe choice.

A failure that arrives after a handler has already moved its loop in memory SHALL be treated as a partial effect and
quarantined, never retried: the redelivery does not reach the loop the first attempt left, so the handler answers it
from the loop's new terminal state and the result the first attempt built cannot be rebuilt. A loop's terminal
business failure SHALL be positively acknowledged only once its failed loop state, its terminal record and its
failure events have committed.

An error accompanying a result that carries a completion or failure event SHALL be read as that terminal's cause,
never as the delivery's disposition: the terminal is the loop's settlement, the terminal owner commits it in its order,
and the delivery settles on that commit — acknowledged once it lands, retried on a lost compare-and-swap, quarantined
on any other commit failure. That reading SHALL be the same on every lane that produces such a result, the
approval-timeout sweeper included, and the error's class SHALL NOT be read for it. Every other error keeps its lane's
own disposition, as the scenarios below record it per lane. The carrier order an applied result takes — birth, gate,
ordinary advance or terminal — SHALL be a function of the result's shape alone: the same shape takes the same order on
every lane that produces it.

The input a task delivery was accepted with SHALL be a fact of the loop record it was accepted into, never of the
process that accepted it: `task_prompt` carries the prompt of the task that bore the loop from the birth write on
and is never rewritten, and `pending_continuation_prompt` carries the text of a deferred turn from the write that
sets its marker to the write that clears it. A rebuild that seats the record restores both and replays an uncarried
turn after the retained conversation, so a rebuilt loop's next request SHALL carry it — once, except in the one
window the record cannot tell apart (a carrier minted after the turn whose record write was lost), where it is
carried twice and logged, and never zero. A task delivery that fails before any durable effect SHALL settle by its
error's class — malformed or invalid terminated, transient retried — and SHALL NOT be acknowledged except as one of
the lane's defined refusals, each named below: a duplicate, an applied task, an unheld continuation, and a
continuation of a loop with work in flight, which stays acknowledged because a Retry would park the whole task lane
(MaxAckPending 1) on a turn no redelivery can fix. A task whose loop record exists SHALL resume from that record on
redelivery; the birth-failure and transient-lineage paths of the same lane settle on their durable effect and are not
exempt. The WHOLE record shares the wire's payload ceiling, every field summed; a record the NATS payload ceiling
refuses is not supported, and the refusal is permanent at birth.

#### Scenario: Required output publication fails

- **WHEN** a handler computes a transition
- **AND** a required publication does not receive PubAck
- **THEN** the source is not positively acknowledged
- **AND** its disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the deterministic terminal event receives PubAck before source ACK

#### Scenario: A malformed non-heartbeat input is terminated, never acknowledged as done

- **WHEN** a production non-heartbeat callback receives an input it cannot decode or correlate
- **THEN** it returns Terminate with a non-nil cause
- **AND** no warning-only return becomes ACK

#### Scenario: A malformed heartbeat-lane input is terminated, never acknowledged as done

- **WHEN** a production heartbeat-lane callback receives bytes that do not decode, or that decode to a payload type
  the lane does not handle
- **THEN** the failure is classified as a permanent delivery error and the binding terminates the delivery
- **AND** the delivery is neither acknowledged nor retried

#### Scenario: A handler result fails before any of its publications

- **WHEN** a handler has moved its loop in memory and the loop-state, terminal-record or graph write then fails
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the delivery is not retried into a handler whose terminal guard would answer it with an empty result

#### Scenario: A terminal business failure cannot be recorded

- **WHEN** a handler error fails its loop and the failed loop state, terminal record or failure event does not commit
- **THEN** the source is not positively acknowledged
- **AND** a loop that could not be transitioned at all is retried rather than quarantined, because no effect was
  written and the redelivery is settled from the loop record

#### Scenario: A handler result fails after some of its publications have returned PubAck

- **WHEN** a handler result's state has been stamped and its publication phase then fails partway
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the owner latches its health fatal and drains that lane, rather than redelivering a callback that would
  republish results whose PubAcks already returned

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** handler recovery returns a non-nil fatal-classified error
- **AND** the production delivery callback returns Quarantine without persistence or settlement
- **AND** the exact owner stops and drains
- **AND** the panic is never rewritten to nil

#### Scenario: Loop delivery metadata is unavailable

- **WHEN** a loop settlement adapter cannot observe native delivery metadata
- **THEN** it invokes no loop work and makes no heartbeat or settlement call
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** drains the exact consume handle
- **AND** loop health becomes negative with the exact cause and one error-count increment

#### Scenario: The first fatal result latches health before the handle drains

- **WHEN** any loop delivery owner produces its first result requiring owner stop
- **THEN** health synchronously reports `Healthy=false`, status `delivery ownership lost`, the exact cause in
  `LastError`, and exactly one increment of the existing error count, before owner-stop observation drains the
  exact handle
- **AND** a later fatal result in the same or another lane neither overwrites nor recounts that first cause
- **AND** the latch itself adds no metric family, public state, durable state, or communication path

#### Scenario: A populated terminal result that arrives with an error is the loop's settlement

- **GIVEN** a loop past its own deadline
- **WHEN** `HandleToolResult`, `HandleApprovalResponse` or the approval-timeout sweep's auto-reject returns the failed
  state, the loop-failed event and its publication together with a fatal error
- **THEN** the error's class is not read: the terminal owner commits `COMPLETE_<loopID>`, the graph stamp, the event and
  the record in that order, and the delivery is acknowledged on a committed terminal, retried on a lost
  compare-and-swap, and quarantined on any other commit failure
- **AND** the sweeper, which has no delivery to classify, logs a commit that did not land and echoes no rejection

#### Scenario: The model lane re-derives the same failure from the error

- **GIVEN** the same deadline, noticed by `HandleModelResponse`
- **WHEN** it returns the populated failed result together with the timeout error
- **THEN** the lane commits a failure of the same kind and the `timeout` reason through the terminal owner, built from
  the error rather than from the handed result, and the delivery settles on that commit exactly as on the other lanes
- **AND** the handed result's own event is not the one published; the two carry the same reason

#### Scenario: A non-terminal result with an error settles on its lane's own disposition

- **WHEN** a handler returns a result that carries no terminal event together with an error that is not one of the
  model lane's classification sentinels
- **THEN** the tool lane quarantines it whatever the error's class, unless the error proves the cancellation happened
  before any mutation, which is retried
- **AND** the model lane fails the loop through the terminal owner — reason `max_iterations` for the budget sentinel,
  `timeout` for the loop deadline, `handler_error` otherwise — and the delivery settles on that commit; a loop that
  cannot be transitioned at all is retried
- **AND** the approval lane settles by class: fatal is quarantined, invalid is terminated, anything else is retried

#### Scenario: The same result shape takes the same order on every lane

- **WHEN** a handler returns a result with no error
- **THEN** a result that created the loop is written by create-once before its first request is published; a result
  that gates the loop for approval is written before its approval request is published; any other non-terminal result
  publishes every output first and writes the record by compare-and-swap after; a result carrying a completion or
  failure event goes to the terminal owner
- **AND** a gate, which only the tool-result lane creates, is written before it is published, and an ordinary
  advance produced on the model, tool or approval lane or by the sweep publishes before it writes on every one of
  them

#### Scenario: A terminal-guard result is settled by the record, whichever lane produced it

- **WHEN** a handler answers a delivery with an effect-free result because the loop is already terminal in memory
- **THEN** the loop's record decides: absent or terminal is acknowledged and counted as the lane's drop; live or
  unreadable is retried, because the terminal in memory may be a commit still in flight on another lane
- **AND** a guard result is never returned together with an error, so the failed-terminal reading never meets one

#### Scenario: A refusal before any mutation is retried; a refusal naming invalid input is terminated

- **WHEN** a handler refuses before it touched the loop — the delivery context was cancelled first, an approval
  answer fails validation, or a task names a depth at or past its limit
- **THEN** the cancelled model response, tool result or task is retried; the invalid approval answer and the
  over-depth task are terminated; nothing was registered for any of them

#### Scenario: An approval answer whose loop was released after its gate resolved is recovered cold

- **GIVEN** an approval answer that won the resolve, so the gate is cleared in memory
- **WHEN** the loop is released before the handler re-reads it, so the handler returns an empty result with a
  not-found error
- **THEN** the delivery is retried; the redelivery finds no loop in memory, reads the still-gated record, rebuilds the
  loop and applies the answer exactly as the process that gated the loop would have

#### Scenario: A tool result whose loop was released mid-delivery is quarantined

- **GIVEN** a tool result whose routing entry resolved a loop this process holds
- **WHEN** the loop is released between that lookup and the handler's own read of it, so the handler returns an empty
  result with a not-found error
- **THEN** the delivery is quarantined, as any non-terminal handler error on this lane is; the released loop's record
  is what a later cold delivery reads

#### Scenario: The model lane's classification refusals are settled from the record

- **WHEN** `HandleModelResponse` refuses a response as superseded or already applied
- **THEN** it is acknowledged without effect
- **WHEN** it refuses the response as naming a request that is not the loop's
- **THEN** it is quarantined
- **WHEN** it refuses the response as newer than the request the record names
- **THEN** it is retried until the record names it, with the loop left exactly as it was

#### Scenario: The task lane's results settle on their own owner, and its errors stay exempt

- **WHEN** `HandleTask` returns a result that created the loop
- **THEN** the task lane writes the record by create-once, then publishes the first request; a refused create or a
  failed publish releases the loop and is retried, and the redelivery finds the record and republishes the request
  it names rather than birthing a second loop
- **WHEN** it returns only the loop identifier of an active loop
- **THEN** the task is acknowledged as a duplicate
- **WHEN** it returns a deferred continuation
- **THEN** the pending-continuation marker and the turn's text are written together by one compare-and-swap onto the
  record the lane read; a lost compare-and-swap is retried, any other write failure is logged as best-effort and
  the task is acknowledged with the turn held in process memory only
- **WHEN** it returns an error
- **THEN** the delivery takes the disposition the lane's policy derives from the error's class: a refusal naming
  invalid input — an over-depth task, a continuation of a settled loop — is terminated, and every other error is
  retried, except a continuation refused because its loop has work in flight, which stays acknowledged as a defined
  refusal: a Retry parks the whole task lane, which runs at MaxAckPending 1, for the redelivery budget on a turn no
  redelivery can fix, and the turn must be re-sent. No production path fails after a birth registered its loop; one
  that did would be retried into the duplicate acknowledgement of the loop it left registered. The exemption this
  heading names ended with #1345
- **AND** the birth-failure and transient-lineage paths of the same lane settle on their durable effect: a failed
  graph birth establishes the loop's terminal before the delivery is acknowledged, and a transient lineage write
  remembers the spawn result so its redelivery resumes it rather than deduplicating it

#### Scenario: The deferred continuation's replacement behaviour is owed to #1365

- **GIVEN** a deferred continuation whose marker and text reached the record, and whose carrier is still empty
- **WHEN** a replacement process rebuilds the loop from that record and the request the record names
- **THEN** the retained conversation is replayed and the turn's text is appended after it as the user's turn, the
  marker is kept, and the next completion carries the turn into the loop's next request exactly once; the request
  that carries it is named as its carrier, and the marker, its carrier and its text clear when that request settles
- **AND** a record whose marker names a carrier is left as it is, because the retained request carries the turn; a
  record whose marker is uncarried but carries no text is cleared with a warning, as before this change
- **AND** a rebuild that finds a request newer than the one the record names adopts it and leaves the marker as it
  found it, then replays the text after that request's conversation: once when the request was minted before the
  turn (a turn deferred behind a request tracked but not yet on the record), twice when the request already carries
  it (a carrier whose record write was lost) — the record cannot tell the two apart, the replay is logged naming the
  loop and the request, and the turn is never lost

#### Scenario: A durable terminal with a non-terminal record is owed to #1377

- **GIVEN** a terminal whose `COMPLETE_<loopID>` and event landed and whose record write lost its compare-and-swap, or
  an approval-timeout sweep terminal whose marker landed and whose publication failed
- **THEN** the commitment is known for the marker and unknown for the record; a redelivered input meeting the marker
  adopts it by loop identifier and terminal kind through the terminal owner; a timer is never redelivered
- **AND** issue #1377 owns making the record converge on the durable terminal through the declared recovery path,
  adding no row and changing no disposition here

#### Scenario: A rebuilt loop's terminal event carries the prompt its record accepted

- **GIVEN** a loop whose record carries `task_prompt` from its birth write
- **WHEN** a replacement process rebuilds the loop from the record and a retained request, and the loop later
  completes or fails, or its context is emptied by repair
- **THEN** `LoopCompletedEvent.Prompt` and `LoopFailedEvent.Prompt` carry the record's prompt, and the empty-context
  recovery re-injects it rather than its placeholder
- **AND** the prompt is the one that bore the loop: birth renders it and no later write rewrites it — a
  continuation's turn is the record's pending text or its retained request, never its prompt — and a record with no
  prompt leaves the readers reading empty, as before this change

#### Scenario: A malformed task is terminated, never acknowledged as done

- **WHEN** the task lane receives bytes that do not decode, or that decode to a payload type the lane does not handle
- **THEN** the delivery is terminated with a non-nil cause and is neither acknowledged nor retried, exactly as the
  response and tool-result lanes terminate theirs
- **AND** no record makes such a delivery resumable: the bytes will never decode

#### Scenario: A task that fails before anything is registered is redelivered into a fresh birth

- **WHEN** a task's handler refuses before it registered a loop — the delivery context was cancelled, or a create
  failed — and the delivery is retried
- **THEN** the redelivery is a birth: it finds no record and no loop in memory, is not deduplicated, and creates the
  loop
- **AND** a task that fails after its record exists is redelivered into the cold fork, which republishes the request
  the record names and writes nothing; the record written before the first publication is the resumable fact, and
  no second one is kept

#### Scenario: A loop record the payload ceiling refuses is not retried

- **WHEN** the NATS client refuses a record write because the whole rendered record — every field summed, the prompt
  and a deferred turn's text included — exceeds the server's payload ceiling
- **THEN** at birth the refusal is permanent: the loop is released and the task is terminated with the loop id and
  the record's size in the cause, never retried into the same refusal on a lane that runs at MaxAckPending 1
- **AND** at the deferred turn's marker write the refusal is logged with the size, the text is dropped from the
  in-memory entity so later record writes fit, and the delivery is acknowledged as any other best-effort marker
  write failure: the turn is in the loop's context and is carried by the next request, and it is not durable
- **AND** at a carrier write the refusal quarantines the delivery as any other carrier write failure does; the
  record's text fields count toward it, so a turn that fit its own message fits the record unless the record's
  other fields have filled it

#### Scenario: A tool result is cancelled after the loop has advanced

- **WHEN** delivery work for a tool result is cancelled after the handler has stored the result, advanced the
  iteration, or drained accumulated results
- **THEN** the delivery quarantines, because a replay meets a loop that has already moved
- **AND** only a cancellation the handler observed before it touched anything is retried, so a clean stop that
  mutated nothing does not latch delivery ownership lost

#### Scenario: A terminal failure's record is written before its event is published

- **WHEN** a terminal handler result carries a failure state
- **THEN** `COMPLETE_<loopID>` is written before the graph stamp and before any failure event is published
- **AND** a record write that fails quarantines the delivery with nothing published, so no watcher reads a failure
  event with no terminal record behind it

#### Scenario: A loop-execution birth failure is not exempt

- **WHEN** a task's graph birth or lineage write fails
- **THEN** the loop's terminal business failure is established before the delivery is acknowledged
- **AND** a failure that could not be recorded quarantines instead

## REMOVED Requirements

### Requirement: Task intake is the one loop input class this layer does not convert

**Reason**: the requirement existed to name an exemption from the rule above it ("Task intake SHALL be named as an
exemption rather than left to the absence of a scenario, because the rule above reads as covering it"). This change
converts the lane, so the rule above covers it and the exemption has no content. Two of its sentences were already
false at `9e5d8455`: a failed first publication and a failed loop-state write have retried, not acknowledged, since
the record-before-publish reordering (`processor/agentic-loop/component.go:1699-1745`, inventory § Fact 3), and the
scenario `The task lane's results settle on their own owner…` in the same file said so nine lines above it. Its three
scenarios that were not about the exemption — `A tool result is cancelled after the loop has advanced`, `A terminal
failure's record is written before its event is published`, `A loop-execution birth failure is not exempt` — move
verbatim into the MODIFIED requirement above, as does its sentence "the birth-failure and transient-lineage paths of
the same lane are NOT exempt" (the `pendingTaskResult` resume), now an AND-clause of the task-lane scenario; `A task
delivery fails after its loop exists` is superseded by the rewritten task-lane scenario and the two added intake
scenarios.
