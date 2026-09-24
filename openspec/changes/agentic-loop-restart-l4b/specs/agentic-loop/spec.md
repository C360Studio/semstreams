# agentic-loop — delta (#1362, restart-safety L4b)

> The MODIFIED block restates the requirement at `openspec/specs/agentic-loop/spec.md:1451-1652` (`c4a79fd5`) in
> full: every existing scenario verbatim, plus three carried from the L4a archive. Gate-order text is variant A (write
> → publish for gates): task 1.6's test showed the cold branch acknowledges an answer that outruns its gate, so the
> uniform order is not taken. Owner rulings: #1362 issuecomment-5799118983.

## MODIFIED Requirements

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
same carrier, order, and compare-and-swap as an operator's rejection, writing neither the request name nor the
advance when its publication fails; the sweeper's own write failures are logged, not counted. At loop birth the
record SHALL be written before the first request is published. An update that CREATES an approval gate, on whichever
lane produces it, SHALL be written before the gate is published: a gate published before it is written leaves a human
an approval request with no durable gate behind it. A terminal outcome SHALL be committed as `COMPLETE_<loopID>` by
create-once before its terminal event is published, and the loop entity's terminal state SHALL be written after that
event, the approval-timeout sweeper's automatic rejection included. A redelivered
terminal input SHALL adopt the loop's durable terminal by loop ID and terminal kind. On that commit path a durable
terminal of a different kind from the one the redelivered input derives SHALL be quarantined, not adopted: the first
terminal wins. A redelivered cancel that reaches a process not holding the loop, whose record is live and whose durable
terminal is a cancel, SHALL adopt that cancel; when that durable terminal is a completion or a failure, the cancel SHALL
be retried, not quarantined, because the loop's own terminal redelivery writes the record terminal. A terminal whose
record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed is not reconciled: the
loop may keep running under a durable terminal and a published event, and its own later terminal is quarantined. An
approval-timeout sweep whose `max_iterations` terminal commits `COMPLETE_<loopID>` and then fails to publish is not
reconciled either: a timer is never redelivered, the record stays `awaiting_approval`, and a later human answer is
applied cold on a loop that already has a durable failed terminal. A
redelivered input whose `request_id` is older than `published_request_id` SHALL be acknowledged without effect; one whose
`request_id` is newer SHALL be retried until the record names it; one whose `request_id` is not a request of the loop
SHALL be quarantined. A redelivered tool result whose `request_id` equals `published_request_id` and whose execution
is already named in `pending_tool_results` is a replay of applied work and SHALL be acknowledged without effect, with
the batch's unfinished executions left untouched; ordering cannot decide that case, because the batch is the current
request's. An `approval_required` result stored there by an approval gate is a placeholder, not an answer: it counts as
applied only against another `approval_required` result. The approved call's own result SHALL be applied, and SHALL
be retried while the record still holds the gate for that execution. A redelivered `approval_required` result whose
gate the record still holds SHALL re-publish that gate's approval request from the record and be acknowledged; a
re-publication that fails SHALL be retried. A process with no memory of the loop SHALL, before classifying a redelivered model response, tool result,
or approval response, read the newest retained request for the loop and, when it is newer than
`published_request_id`, adopt it into the record by identity first. An approval response whose loop's retained
request or its response is confirmed absent SHALL fail the loop with reason `continuation_unavailable`; an unreadable
stream SHALL be retried. Where this requirement classifies a redelivered input as applied or inapplicable, that
classification SHALL take precedence over the live-record retry of "A loop absent from process memory is settled from
its record". Recovery SHALL never compare rendered messages, result content, or terminal content to decide whether an
input was applied.

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
