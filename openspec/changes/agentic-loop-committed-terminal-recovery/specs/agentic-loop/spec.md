# agentic-loop — delta (#1377, committed terminal recovery)

> Two MODIFIED requirements (design OQ6; ruling 2's cost called out there). Block 1 restates
> `openspec/specs/agentic-loop/spec.md:886-1103` (`6876fe51`) — "Loop input classes settle after owner-specific
> durable done", #1376's table, all 22 scenarios verbatim — appending one sentence to its partial-effect paragraph and
> changing four scenarios' clauses (titles unchanged): the order scenario, the terminal-guard scenario, the
> released-after-resolve scenario (design OQ3 (ii)), and the "owed to #1377" row, which is discharged. Block 2 restates
> `spec.md:1587-1861` — "The loop record names its outstanding request", all 22 scenarios verbatim — replacing the two
> "not reconciled" residual sentences (`spec.md:1617-1623`, #1362 issuecomment-5808903072 / issuecomment-5809906669)
> with the proved behaviour and its stated bounds, adding the carrier's terminal-in-memory refusal, its render-time
> refusal (design § 3.1, § 3.2) and the publication in flight as a bound, and adding six scenarios (design § 2). Every
> scenario states the design's recommended answers (OQ1 (a), OQ2 (a), OQ3 (ii), OQ4 the check, OQ7 (a)); the texts under
> OQ2 (b) and OQ3 (i) are named in `design.md` § 2. PR #1387 also MODIFIES block 1's requirement and lands first: block
> 1 is re-based on the synced spec before archive (tasks 0.2).

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
business failure SHALL be positively acknowledged only once its failed loop state, its terminal record and its failure
events have committed. A non-terminal result whose loop is terminal in memory, or no longer held, when the carrier is
entered, when it names the request it published, or when it renders the loop's record is not this delivery's partial
effect: the carrier publishes nothing further, writes nothing, and the loop's record decides the delivery.

An error accompanying a result that carries a completion or failure event SHALL be read as that terminal's cause,
never as the delivery's disposition: the terminal is the loop's settlement, the terminal owner commits it in its order,
and the delivery settles on that commit — acknowledged once it lands, retried on a lost compare-and-swap, quarantined
on any other commit failure. That reading SHALL be the same on every lane that produces such a result, the
approval-timeout sweeper included, and the error's class SHALL NOT be read for it. Every other error keeps its lane's
own disposition, as the scenarios below record it per lane. The carrier order an applied result takes — birth, gate,
ordinary advance or terminal — SHALL be a function of the result's shape alone: the same shape takes the same order on
every lane that produces it.

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
- **AND** a non-terminal result whose loop is terminal in memory or no longer held when the carrier is entered, when it
  names the request it published, or when it renders the loop's record, publishes nothing further and writes nothing
  on every lane: the record decides it

#### Scenario: A terminal-guard result is settled by the record, whichever lane produced it

- **WHEN** a handler answers a delivery with an effect-free result because the loop is already terminal in memory
- **THEN** the loop's record decides: absent or terminal is acknowledged and counted as the lane's drop; live or
  unreadable is retried, because the terminal in memory may be a commit still in flight on another lane
- **AND** a guard result is never returned together with an error, so the failed-terminal reading never meets one
- **AND** the carrier settles a non-terminal result the same way when it finds the loop terminal in memory or no longer
  held — a case the handler's guard could not see because the loop moved after the handler returned — counting an
  acknowledged drop under the tool-result family's terminal reason on every lane, and retrying a live record

#### Scenario: A refusal before any mutation is retried; a refusal naming invalid input is terminated

- **WHEN** a handler refuses before it touched the loop — the delivery context was cancelled first, or an approval
  answer fails validation
- **THEN** the cancelled model response or tool result is retried; the invalid approval answer is terminated; a task
  refused for either reason keeps the log-and-acknowledge exemption named under "Task intake is the one loop input
  class this layer does not convert"

#### Scenario: An approval answer whose loop was released after its gate resolved is recovered cold

- **GIVEN** an approval answer that won the resolve, so the gate is cleared in memory
- **WHEN** the loop is released before the handler re-reads it, so the handler returns an empty result with a
  not-found error
- **THEN** the delivery takes the cold branch on that first delivery: it reads the still-gated record, rebuilds the loop
  and applies the answer exactly as the process that gated the loop would have; a released loop whose record is
  already terminal is acknowledged as inapplicable

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
- **AND** the recovery path is declared under `The loop record names its outstanding request`: a same-kind later
  terminal adopts the marker and converges the record, and a sweeper terminal is adopted on the next answer to its
  gate; a cancel of that gated loop is retried to exhaustion; neither adds a row nor changes a row's disposition
  here — the carrier's refusal widens the terminal-guard row's condition

### Requirement: The loop record names its outstanding request

The `AGENT_LOOPS` record of a non-terminal loop SHALL carry `published_request_id`, the `RequestID` of the
`AgentRequest` whose PubAck preceded the KV update that wrote the record, and every redelivered model response, tool
result, approval response, and governance verdict SHALL be classified against that field and against
`pending_tool_results` rather than against retained conversation content.

The following SHALL hold for every record of a non-terminal loop `L`:

- `published_request_id = R` implies, while the record exists, that an `AgentRequest{RequestID: R, LoopID: L}` is
  durably retained on `agent.request.L`.
- Every `pending_tool_results[e]` whose `request_id` is `R` names an execution ID derived from a tool call of the
  retained response for `R` (membership only; no rendered content is compared).
- `iterations` changes only in an update whose `published_request_id` also changes.
- `pending_approval`, when present, names `request_id = published_request_id`.

The non-terminal record SHALL be written with a compare-and-swap update against the revision observed when the
delivery was admitted. On the model-response, tool-result, and approval-response lanes that update SHALL follow the
PubAck of every output the new record implies, and the approval-timeout sweeper's automatic rejection SHALL take the
same carrier, order, and compare-and-swap as an operator's rejection, writing neither the request name nor the advance
when its publication fails; the sweeper's own write failures are logged, not counted. At loop birth the record SHALL
be written before the first request is published. An update that CREATES an approval gate, on whichever lane produces
it, SHALL be written before the gate is published: a gate published before it is written leaves a human an approval
request with no durable gate behind it. A terminal outcome SHALL be committed as `COMPLETE_<loopID>` by create-once
before its terminal event is published, and the loop entity's terminal state SHALL be written after that event, the
approval-timeout sweeper's automatic rejection included. The terminal transition SHALL clear the loop's approval gate,
so a terminal record carries neither `pending_approval` nor `state_before_approval`. A redelivered terminal input
SHALL adopt the loop's durable terminal by loop ID and terminal kind. On that commit path a durable terminal of a
different kind from the one the redelivered input derives SHALL be quarantined, not adopted: the first terminal wins.
A redelivered cancel that reaches a process not holding the loop, whose record is live and whose durable terminal is a
cancel, SHALL adopt that cancel; when that durable terminal is a completion or a failure, the cancel SHALL be retried,
not quarantined, because the loop's own terminal redelivery writes the record terminal. A terminal whose record update
loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves a durable terminal and a
published event over a live record — whether the record moved under a second process, under this process's own
adoption of a newer retained request for a loop it still held, or under a spawn-path birth failure with a
producer-supplied loop ID. The record converges at the loop's next terminal commit in whichever process holds it next:
a terminal of the same kind adopts the durable terminal, republishes it and writes the record from it; a terminal of a
different kind is refused and quarantined, the first terminal wins. Until then the loop runs on under a durable
terminal, bounded only by its own remaining iteration budget and `timeout_at`, with no time bound while `timeout_at`
is zero or the loop is gated. An approval-timeout sweep terminal (its `max_iterations` auto-reject, or the loop's own
timeout) that commits `COMPLETE_<loopID>` and then fails to publish leaves a durable failed terminal under a record
that stays `awaiting_approval`: a timer is never redelivered, and the record converges only on the next answer to that
gate. A reject, and any answer to a loop past its own deadline, dispatches nothing: the rebuilt loop re-derives the
failure and adopts the durable terminal. An approve of a loop at its iteration cap dispatches the approved call once,
and the durable terminal is adopted when that call's result completes the batch. A cancel of that loop is retried
until the signal consumer's redelivery budget is exhausted and is observed there, never applied, because the cold
cancel arm adopts only a cancel marker. A non-terminal result that reaches the carrier after its loop went terminal in
memory, or after the loop was released, SHALL publish nothing and write nothing, and a non-terminal result whose loop
is terminal in memory or no longer held when the carrier names the request it published or renders its record SHALL
NOT be written: the loop's record decides the delivery exactly as it decides a terminal-guard result — a terminal or
absent record is acknowledged without effect, a live one is retried, and the redelivery is classified by its lane
against the record. A terminal that lands between the carrier's check and its publication lets that one publication
out, and the durable terminal may be created before that publication's PubAck; the published call's result is
acknowledged without effect on the terminal loop. A redelivered input whose `request_id` is older than
`published_request_id` SHALL be acknowledged without effect; one whose `request_id` is newer SHALL be retried until
the record names it; one whose `request_id` is not a request of the loop SHALL be quarantined, except that such a
governance verdict SHALL be terminated, so that one misconfigured verdict rule terminates its own deliveries
(JetStream Term: never redelivered, no dead-letter copy) without stopping the verdict lane. A redelivered tool result
whose `request_id` equals `published_request_id` and whose execution is already named in `pending_tool_results` is a
replay of applied work and SHALL be acknowledged without effect, with the batch's unfinished executions left
untouched; ordering cannot decide that case, because the batch is the current request's. An `approval_required` result
stored there by an approval gate is a placeholder, not an answer: it counts as applied only against another
`approval_required` result. The approved call's own result SHALL be applied, and SHALL be retried while the record
still holds the gate for that execution. A redelivered `approval_required` result whose gate the record still holds
SHALL re-publish that gate's approval request from the record and be acknowledged; a re-publication that fails SHALL
be retried. A governance verdict that reaches no waiter, and whose `request_id`, when present, is a request of its
loop, SHALL be acknowledged without effect when its execution is named in `pending_tool_results`, an approval gate's
`approval_required` placeholder included, because a gated call is dispatched only after its verdict was consumed; one
whose loop record is absent or terminal SHALL be acknowledged; one with no `request_id` SHALL be classified on that
membership alone; and a current verdict whose execution is not named SHALL be retried. A process with no memory of the
loop SHALL, before classifying a redelivered model response, tool result, or approval response, read the newest
retained request for the loop and, when it is newer than `published_request_id`, adopt it into the record by identity
first. An approval response whose loop's retained request or its response is confirmed absent SHALL fail the loop with
reason `continuation_unavailable`; an unreadable stream SHALL be retried. Where this requirement classifies a
redelivered input as applied or inapplicable, that classification SHALL take precedence over the live-record retry of
"A loop absent from process memory is settled from its record". Recovery SHALL never compare rendered messages, result
content, or terminal content to decide whether an input was applied.

A deferred continuation is durable as a MARKER only. Where the record carries `pending_continuation` with an empty
`pending_continuation_request_id`, the admitted turn's text was never inside a retained request and is not
recoverable: a rebuild SHALL clear the marker and warn, and SHALL NOT synthesise the turn. Neither the turn nor the
loop's task prompt is carried by the record, so a loop rebuilt FROM that record and its retained request — the
model-response and tool-result cold arms — publishes its terminal event with an empty `prompt`; a loop rebuilt from a
redelivered task runs the ordinary birth path and keeps the prompt that task carries.
A continuation reaches only a loop some process holds: a task whose `task_id` differs from the one the live record
carries SHALL be refused — acknowledged without effect, with a warning naming both tasks and a counted intake
rejection — and SHALL NOT be acknowledged as an applied task nor used to rebuild the loop's first request, because
the record can answer only for the task it belongs to. The turn must be re-sent once a redelivered input has rebuilt
the loop.

An approval deadline is process-local and is not a durable fact: a replaced process SHALL re-arm no approval deadline
at startup, and a loop in `awaiting_approval` SHALL stay in `awaiting_approval` until the approval is answered or the
loop is cancelled. A loop the replacement later rebuilds for a redelivered input carries its record's own approval
deadline from then on.

#### Scenario: The next request was published but the record was not updated (W4, tool lane)

- **GIVEN** a running loop whose record names request `R` at iteration `N` and whose last tool result of `R`'s batch
  was applied, and the process published `R(N+1)` and crashed before the record update
- **WHEN** that tool result is redelivered
- **THEN** the loop classifies it as current, re-stores it idempotently, mints `R(N+1)`, finds a retained request
  with that exact `RequestID` on `agent.request.<loopID>`, adopts it without republishing, writes the record with
  `iterations = N+1` and `published_request_id = R(N+1)`, and acknowledges

#### Scenario: A cold replacement adopts the newest retained request before classifying a redelivered result

- **GIVEN** a loop whose record names `R` at iteration `N`, whose newest retained request is `R(N+1)` (published before
  the process crashed, record not updated), and a replacement process with no memory of the loop
- **WHEN** the last tool result of `R`'s batch is redelivered to the replacement
- **THEN** the replacement first writes the record to `R(N+1)` under compare-and-swap (`published_request_id`,
  `iterations = N+1`, `pending_tool_results` empty, any `pending_approval` cleared with `state = running`), then
  classifies the result as older, acknowledges it, and publishes nothing

#### Scenario: A rebuilt loop keeps its record's deadline

- **GIVEN** a loop whose record carries a `timeout_at` that has already passed, and a replacement process with no
  memory of the loop
- **WHEN** an input naming the request the record names is delivered to the replacement
- **THEN** the replacement rebuilds the loop with the record's original `timeout_at` — the deadline is neither
  refreshed nor extended by the time the process was down — and the rebuilt loop fails on that delivery, writing the
  record terminal and publishing a loop-failed event carrying the timeout reason, and the delivery is acknowledged

#### Scenario: An approval answer does not outlive its loop's deadline

- **GIVEN** a loop in `awaiting_approval` whose `timeout_at` has already passed, whose approval deadline has not, held
  by this process or by none
- **WHEN** an approve, modify or reject answering its gate is delivered
- **THEN** no tool call is dispatched; the loop fails with the timeout reason through the terminal owner —
  `COMPLETE_<loopID>`, the loop-failed event, and the record written terminal with its gate cleared — and the delivery
  is acknowledged

#### Scenario: A task rebuilt at iteration zero keeps its record's deadline

- **GIVEN** a loop record at `iterations = 0` naming `published_request_id = R1`, whose `timeout_at` has already
  passed, and a replacement process with no memory of the loop
- **WHEN** the task message is redelivered to the replacement and `R1` is rebuilt from it
- **THEN** the rebuilt loop carries the record's own `started_at` and `timeout_at` — this reconstruction runs the
  ordinary birth path, which would otherwise stamp a fresh budget — so the answer to `R1` settles the loop on the
  timeout instead of running it

#### Scenario: A replaced process re-arms no approval deadline

- **GIVEN** a loop whose record is `awaiting_approval` and whose process was replaced
- **WHEN** the replacement starts and holds no in-memory approval deadline for that loop
- **THEN** no approval deadline is re-armed and no record is written, and the loop stays `awaiting_approval` until the
  approval is answered or the loop is cancelled

#### Scenario: An approval timeout whose rejection could not be published leaves the record as it was

- **GIVEN** a loop gated on a human approval whose deadline has passed, whose record names `R` with the gate on it,
  and whose stream retains `R`
- **WHEN** the sweep's auto-reject mints `R(N+1)` and its publication fails while the record is still writable
- **THEN** neither the new request name nor the iteration it implies is written: the record still names `R` with its
  approval gate intact, and a redelivered result of the gated batch is still classified against it — a record naming a
  request the stream does not retain is refused by every later cold read, so the advance is committed only behind its
  PubAck

#### Scenario: A stale tool result is acknowledged without effect

- **GIVEN** a running loop whose record names `R(N+1)`
- **WHEN** a tool result carrying `request_id = R(N)` is redelivered
- **THEN** it is acknowledged, no message is published, the record is not written, and the inapplicable-result metric
  and audit log line are emitted

#### Scenario: A response that outruns the record update retries

- **GIVEN** a loop whose record names `R` while `R(N+1)` has been published and its record update has not landed
- **WHEN** the model response for `R(N+1)` is delivered
- **THEN** the delivery is retried as not yet observable, and no effect is applied

#### Scenario: A second delivery of an applied response is acknowledged without effect

- **GIVEN** a running loop whose record names `R`, whose model response for `R` has been applied by the process
  holding it, and which is therefore waiting on no request
- **WHEN** the response for `R` is delivered to that process a second time
- **THEN** it is acknowledged without effect, no message is published, the record is not written, and the
  model-response drop metric and an audit log line name the loop and the request — ordering cannot decide this case,
  because both deliveries name the request the record names, and re-applying it would append the assistant turn a
  second time and re-dispatch the batch it already dispatched

#### Scenario: A terminal loop receives a result it cannot prove it applied

- **GIVEN** a loop whose record is terminal
- **WHEN** a tool result for that loop is redelivered, whether or not its execution is still named in
  `pending_tool_results`
- **THEN** it is acknowledged without effect, the inapplicable-result metric increments, and an audit log line names
  the loop, execution, and terminal state — membership is deliberately not consulted, because a terminal loop can
  apply nothing either way and re-deriving which side of the settlement this result fell on would change nothing
  this delivery can do

#### Scenario: A task redelivered at iteration zero publishes the first request nothing retains

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1`, an empty `pending_tool_results`, a
  `task_id` that is the redelivered task's, and the stream retains no request for the loop
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** `R1` is rebuilt from the task and published with `Nats-Msg-Id = R1`, the in-process conversation is
  rebuilt, and the task is acknowledged

#### Scenario: A task redelivered over a first request the stream retains is acknowledged without effect

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1`, an empty `pending_tool_results`, a
  `task_id` that is the redelivered task's, and a request the stream retains for the loop
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** it is acknowledged without effect: no request is published, no record is written, and no loop is seated in
  memory — a retained request means `R1` went out, answered or not, so this delivery has nothing to republish, and
  the loop is rebuilt by the lane that owns its outstanding work: its own response, or the first tool result of the
  batch that response dispatched

#### Scenario: A task redelivered over a first batch that already applied something is acknowledged without effect

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1` and a non-empty `pending_tool_results`
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** it is acknowledged without effect: no request is published, no record is written, and no loop is seated in
  memory — `iterations` alone is not evidence of an untouched birth, because a loop advances its iteration only when a
  whole tool batch is in, and seating a fresh loop would leave the batch's remaining results with no execution to
  route to

#### Scenario: A continuation naming a loop no process holds is refused

- **GIVEN** a live loop record whose `task_id` is `T1`, and a replacement process with no memory of the loop
- **WHEN** a task `T2` naming that loop is delivered to the replacement
- **THEN** it is acknowledged without effect whatever the record's `iterations`, `published_request_id` and
  `pending_tool_results` say: no request is published, no record is written, no loop is seated in memory, and a
  warning plus a counted intake rejection name the record's task and the arriving one — the turn's text is in no
  durable place, so it must be re-sent once a redelivered input has rebuilt the loop

#### Scenario: A tool result the record already applied is replayed

- **GIVEN** a loop record naming `published_request_id = R` whose `pending_tool_results` already contains execution
  `e`, and a process with no memory of the loop
- **WHEN** a tool result carrying `request_id = R` and execution `e` is redelivered — its acknowledgement was lost
- **THEN** it is acknowledged without effect before any rebuild is attempted, the inapplicable-result metric and an
  audit log line name the loop and the execution, the record is not written, and the unfinished siblings of `e`'s
  batch are untouched and still recoverable by their own arrival

#### Scenario: A continuation deferred while a request is unpublished writes only its marker

- **GIVEN** a loop whose tool batch has just completed, so the process has advanced the loop in memory and minted its
  next request `R(N+1)` but that request's PubAck has not landed, and a record still naming `R(N)` at `iterations = N`
  with the completed batch's applied executions
- **WHEN** a continuation is admitted to that loop and deferred behind the outstanding request
- **THEN** the record it writes carries `pending_continuation = true` with an empty `pending_continuation_request_id`
  and leaves `published_request_id`, `iterations` and `pending_tool_results` exactly as it read them — the advance is
  committed by the write that follows the PubAck of the request it implies, so no record ever counts an iteration
  against a request the stream does not retain

#### Scenario: A rebuilt loop clears a deferred turn whose text it cannot recover

- **GIVEN** a loop record with `pending_continuation = true` and an empty `pending_continuation_request_id` — a
  continuation admitted while the loop's request was outstanding, whose text lived only in the replaced process
- **WHEN** a replacement rebuilds the loop from that record and its retained request
- **THEN** the rebuilt loop's marker is cleared and a warning names the loop, the next completion settles the loop
  instead of spending an iteration re-asking the model with a context that gained nothing, and the completion event
  it publishes carries an empty `prompt`, because a rebuilt loop recovers neither the deferred turn's text nor its
  task prompt — both must be re-sent by the caller

#### Scenario: A cold replacement adopts past a rejection-minted request and acknowledges the stale approval response

- **GIVEN** a loop `awaiting_approval` at `R` whose gate was rejected, where the rejection minted and published `R(N+1)`
  and the process crashed before the record was updated
- **WHEN** the approval response is redelivered to a replacement process with no memory of the loop
- **THEN** the replacement writes the record to `R(N+1)` with the gate cleared and `state = running` under
  compare-and-swap, classifies the approval response as inapplicable, acknowledges it, and publishes nothing; the
  inapplicable-result metric and an audit log line name the loop

#### Scenario: A governance verdict redelivered after its waiter is gone

- **GIVEN** a loop that restarted after proposing execution `e` under request `R` and later applied `e`'s result
- **WHEN** the verdict for `e` is redelivered and no waiter exists
- **THEN** the loop reads its record, finds `e` in `pending_tool_results` or `R` older than
  `published_request_id`, and acknowledges the verdict without dispatching it

#### Scenario: A redelivered terminal adopts the published terminal by identity

- **GIVEN** a loop whose durable terminal (`COMPLETE_<loopID>`) exists and whose record is not yet terminal, because the
  process crashed after publishing the terminal event and before the record update
- **WHEN** the input that produced the terminal is redelivered and this delivery derives a terminal whose content differs
- **THEN** the loop adopts the durable terminal by loop ID and terminal kind, publishes it, writes the record terminal under
  compare-and-swap, acknowledges, and logs the content difference at the audit line without retrying or quarantining

#### Scenario: A governance verdict naming a request of another loop is terminated and the verdict lane keeps consuming

- **GIVEN** a loop `L` whose record is live and names a non-empty `published_request_id`, and a governance verdict
  that reaches no waiter whose `loop_id` is `L` and whose `request_id` is non-empty, differs from the record's, and is
  not a request of `L` (an empty side orders as unnamed and is decided on membership: `loop_classification.go:58-60`)
- **WHEN** the verdict is delivered
- **THEN** it is terminated (JetStream Term: never redelivered, no dead-letter copy — the Error log line carries its
  execution, loop and request identities), counted once under `missing_waiter` and once under `foreign_request`, and
  never acknowledged as applied, because the request is checked before membership; the verdict consumer is not
  stopped, so one misconfigured verdict rule terminates only its own deliveries and later verdicts are still consumed

#### Scenario: A terminal whose record write was lost converges at the loop's next terminal of the same kind

- **GIVEN** a loop held by process A whose terminal committed `COMPLETE_<loopID>` and published its event, and whose
  record write lost its compare-and-swap because process B had rebuilt the loop from a redelivered input and advanced
  the record to a later request — or because A's own cold adoption of a newer retained request wrote the loop it still
  held
- **WHEN** the loop reaches its own terminal of the same kind in the process that holds it next
- **THEN** the terminal owner there adopts the durable terminal by loop ID and kind, republishes the saved event, writes
  the record terminal under compare-and-swap and counts the terminal once; the input that produced A's terminal, when
  redelivered, is acknowledged as older; and between the lost write and that terminal the loop ran ordinary work under
  a durable terminal, bounded by its remaining iteration budget and `timeout_at` (no time bound while `timeout_at` is
  zero or the loop is gated)

#### Scenario: A terminal of a different kind meeting a durable terminal is refused

- **GIVEN** the same loop, with `COMPLETE_<loopID>` holding a completion
- **WHEN** the loop fails instead
- **THEN** the failure is refused rather than adopted — the marker is not overwritten, no event is published, the record
  is not written — and the delivery is quarantined: the first terminal wins

#### Scenario: A sweeper terminal whose publication failed is adopted on the next answer to its gate

- **GIVEN** a loop gated on a human approval, at its iteration cap or past its own deadline, whose approval-timeout sweep
  committed `COMPLETE_<loopID>` and then failed to publish, so the record stays `awaiting_approval` and no process
  holds the loop
- **WHEN** a reject, or any answer to the loop past its own deadline, is delivered to a process with no memory of the
  loop
- **THEN** the loop is rebuilt from its record, nothing is dispatched, the rebuilt loop re-derives the failure and the
  terminal owner adopts the durable terminal, republishes it and writes the record terminal with the gate cleared, and
  the answer is acknowledged
- **WHEN** an approve is delivered instead, for a loop at its iteration cap
- **THEN** the approved call is dispatched once; when its result completes the batch the loop re-derives the
  `max_iterations` failure, the terminal owner adopts the durable terminal, and the record is written terminal
- **WHEN** a cancel signal for that loop is delivered instead
- **THEN** it is retried — the cold cancel arm adopts only a cancel marker — until the signal consumer's redelivery
  budget is exhausted and recorded in the MaxDeliver ledger; the record stays `awaiting_approval` and converges only on
  an answer

#### Scenario: A cancel that lands while a non-terminal result is on its way to the carrier publishes and writes nothing

- **GIVEN** a loop held by this process, gated on a human approval, whose approve was resolved and whose call was
  registered as pending but not yet published
- **WHEN** a cancel signal cancels the loop in memory before the approval's result reaches the carrier, and the cancel
  lane's terminal commit is still in flight
- **THEN** the carrier publishes no `tool.execute` and writes no record; the approval delivery is retried; once the
  cancel lane has written the cancelled record and released the loop, the redelivered answer reads that record, is
  acknowledged as inapplicable and counted, and the record has exactly one terminal writer

#### Scenario: A loop released while a non-terminal result is on its way to the carrier is settled by its record

- **GIVEN** the same gated loop with its approve mid-dispatch
- **WHEN** the cancel lane commits its terminal, writes the cancelled record and releases the loop before the approval's
  result reaches the carrier, or between the carrier's publication and its record write
- **THEN** the carrier finds no held loop, reads the record, and acknowledges the approval without effect — nothing is
  published after the carrier's check, the record's revision is unchanged, the delivery is neither quarantined nor
  terminated, loop health stays healthy and the approval lane keeps consuming — and the next valid answer on that lane
  dispatches its call

#### Scenario: A cancel that lands after the carrier's check lets one publication out and its record write is refused

- **GIVEN** a held loop whose non-terminal result passed the carrier's check
- **WHEN** a cancel cancels the loop in memory after that check and before the carrier's record write — before the
  terminal owner has created `COMPLETE_<loopID>`, or after the owner has written the cancelled record and not yet
  released the loop
- **THEN** the publication that was in flight is retained on the stream and the durable terminal may be created before
  its PubAck; the carrier's write finds the loop terminal in memory and writes nothing, so no cancelled record exists
  before the marker and the cancelled record has exactly one writer; the record decides the delivery — retried while
  live, acknowledged once terminal — and the published call's result, when it arrives, is acknowledged without effect
  on the terminal loop
