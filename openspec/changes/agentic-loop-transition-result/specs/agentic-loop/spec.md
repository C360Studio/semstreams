# agentic-loop — delta (#1376, transition-result contract)

> One MODIFIED requirement (ruling 2, #1146 issuecomment-5828511934): it restates
> `openspec/specs/agentic-loop/spec.md:886-982` (`f15a528e`) in full — the requirement text and all ten existing
> scenarios verbatim — and adds the `(result, error)` table's rows as scenarios plus the two sentences that carry them.
> The ORDER authority, "The loop record names its outstanding request" (`spec.md:1467`), is cited, not modified.
> Two additions depend on owner questions in `design.md` § 0: the sentence beginning "A result whose state is terminal"
> and the scenario "A terminal-shaped result with no terminal event is refused" are **OQ1 (b)**; the scenario "An
> approval answer that fails after its gate resolved is a partial effect" is **OQ2 (b)**. Under (a) each becomes a
> statement of today's disposition, named in that scenario's own text. The scenario "A tool result whose loop was
> released mid-delivery is quarantined" records today's disposition (OQ3 (a)).

## MODIFIED Requirements

### Requirement: Loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners, and SHALL NOT positively acknowledge a delivery on a converted
class before that lane's durable effect has committed. Task intake is classified through the same owner but is not
converted here; the requirement below names it. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
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

A handler's `(result, error)` pair SHALL be read once, at the loop owner, as exactly one of four transitions, and no
lane SHALL decide any of them on its own: a **refusal** — an error with no terminal event in the result — commits
nothing, and the error's class is the disposition (Retry, Terminate or Quarantine, as the lane's classification
already derives it); a **terminal guard** — a result the handler marked as owned elsewhere — is settled by the loop's
durable record, acknowledged when the record is absent or terminal and retried when it is live or unreadable; an
**applied** transition — a result with no error — is committed by the carrier in the order the result's shape implies:
birth (the record by create-once, then the first request), gate (the record, then the approval request), ordinary
advance (every publication, then the record by compare-and-swap) or terminal (the terminal owner's order); a **failed
terminal** — an error accompanying a result that carries a completion or failure event — is the loop's settlement: the
terminal owner commits it, the delivery settles on that commit, and the error's class is not read. Which order an
applied result takes SHALL follow from the result's shape, never from an argument its caller passes. A result whose
state is terminal and which carries neither a completion nor a failure event SHALL be refused as a fatal,
commit-unknown failure before any record is written, with the loop released from memory: no terminal record is written
with no `COMPLETE_<loopID>` and no terminal event behind it.

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

#### Scenario: A handler's result and error are read once, as one of four transitions

- **WHEN** the model-response, tool-result or approval-response lane, or the approval-timeout sweeper, receives a
  `(result, error)` pair from its handler
- **THEN** the pair is exactly one of: a refusal, a terminal guard, an applied transition, or a failed terminal
- **AND** the failed-terminal reading is made by one owner-side decision that the approval lane, the sweeper and the
  tool lane's failed-result path all call, so no lane carries its own copy of that decision

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

#### Scenario: A non-terminal result with an error commits nothing, and the error's class is the disposition

- **WHEN** a handler returns a result that carries no terminal event together with an error that is not one of the
  model lane's classification sentinels
- **THEN** the tool lane quarantines it, unless the error proves the cancellation happened before any mutation, which
  is retried
- **AND** the model lane fails the loop through the terminal owner — reason `max_iterations` for the budget sentinel,
  `timeout` for the loop deadline, `handler_error` otherwise — and the delivery settles on that commit; a loop that
  cannot be transitioned at all is retried
- **AND** the approval lane settles by class: fatal is quarantined, invalid is terminated, anything else is retried

#### Scenario: An applied result takes the order its shape implies, never the order its caller asks

- **WHEN** a handler returns a result with no error
- **THEN** a result that created the loop is written by create-once before its first request is published; a result
  that gates the loop for approval is written before its approval request is published; any other non-terminal result
  publishes every output first and writes the record by compare-and-swap after; a result carrying a completion or
  failure event goes to the terminal owner
- **AND** the carrier takes no order argument, so a lane cannot select an order for a shape

#### Scenario: A terminal-shaped result with no terminal event is refused

- **WHEN** a result's state is `complete` or `failed` but it carries neither a completion event nor a failure event,
  whether or not an error accompanies it — a completion whose event could not be built or published, or a failure
  whose event could not be built because the loop was released meanwhile
- **THEN** the carrier refuses it as a fatal, commit-unknown failure before writing the record, releases the loop from
  memory, and the delivery is quarantined
- **AND** no record is written terminal with no `COMPLETE_<loopID>` and no terminal event behind it

#### Scenario: A terminal-guard result is settled by the record, whichever lane produced it

- **WHEN** a handler answers a delivery with an effect-free result because the loop is already terminal in memory
- **THEN** the loop's record decides: absent or terminal is acknowledged and counted as the lane's drop; live or
  unreadable is retried, because the terminal in memory may be a commit still in flight on another lane
- **AND** a guard result is never returned together with an error, so the failed-terminal decision never reads one

#### Scenario: A refusal before any mutation is retried; a refusal naming invalid input is terminated

- **WHEN** a handler refuses before it touched the loop — the delivery context was cancelled first, or an approval
  answer fails validation
- **THEN** the cancelled model response or tool result is retried; the invalid approval answer is terminated; a task
  refused for either reason keeps the log-and-acknowledge exemption named under "Task intake is the one loop input
  class this layer does not convert"

#### Scenario: An approval answer that fails after its gate resolved is a partial effect

- **GIVEN** an approval answer that won the resolve, so the gate is cleared in memory and the loop restored to its
  prior state
- **WHEN** the loop cannot be re-read, or the approved call cannot be dispatched
- **THEN** the delivery is quarantined, not retried: a retry would find no gate, report the answer as stale and
  acknowledge it, leaving the record gated and the loop un-gated in memory with nothing outstanding

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
  failed publish releases the loop and is retried
- **WHEN** it returns only the loop identifier of an active loop
- **THEN** the task is acknowledged as a duplicate
- **WHEN** it returns a deferred continuation
- **THEN** the pending-continuation marker is written; a lost compare-and-swap is retried, any other write failure is
  best-effort and the task is acknowledged
- **WHEN** it returns an error
- **THEN** the failure is logged and the delivery acknowledged, the exemption tracked as issue #1345; when that issue
  converts the lane, the error rows take the class-derived disposition and no other row here moves

#### Scenario: The deferred continuation's replacement behaviour is owed to #1365

- **GIVEN** a deferred continuation whose marker reached the record
- **WHEN** a replacement process rebuilds the loop
- **THEN** today the turn's text and the task prompt are not recoverable: the marker is cleared with a warning
- **AND** issue #1365, with #1345, owns making the rebuilt loop recover them from durable accepted-input facts; that
  changes this row's replacement behaviour and nothing else in this requirement

#### Scenario: A durable terminal with a non-terminal record is owed to #1377

- **GIVEN** a terminal whose `COMPLETE_<loopID>` and event landed and whose record write lost its compare-and-swap, or
  an approval-timeout sweep terminal whose marker landed and whose publication failed
- **THEN** the commitment is known for the marker and unknown for the record; a redelivered input meeting the marker
  adopts it by loop identifier and terminal kind through the terminal owner; a timer is never redelivered
- **AND** issue #1377 owns making the record converge on the durable terminal through the declared recovery path,
  adding no row and changing no disposition here

#### Scenario: A bare publication carrier is not a transition

- **WHEN** the cancel lane, the model lane's failure path, the gate re-publication for a redelivered
  `approval_required` result, or the cold cancel adoption builds a `HandlerResult` holding only a loop identifier and
  messages
- **THEN** it is a publication carrier for the terminal owner or for the publish step, not a transition: no
  `(result, error)` pair is read from it, the terminal ones are committed by the terminal owner in its order, and the
  governance-verdict lane reads no `HandlerResult` at all
