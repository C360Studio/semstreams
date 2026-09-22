# agentic-loop — delta

> Delta for #1330 (restart-safety **L4a**). L4b (#1362) carries its own delta; this delta states L4a only — the text
> it takes over is held verbatim in `tasks.md` § "Delta text carried to L4b (#1362)".
> The MODIFIED block restates the requirement at `openspec/specs/agentic-loop/spec.md:201-226` (`b7ce8727`) in full,
> including both of its scenarios. Amended 2026-09-22 to the owner's rulings on #1330 (`design.md` § 1).

## ADDED Requirements

### Requirement: The loop record names its outstanding request
The `AGENT_LOOPS` record of a non-terminal loop SHALL carry `published_request_id`, the `RequestID` of the
`AgentRequest` whose PubAck preceded the KV update that wrote the record, and every redelivered model response and
tool result SHALL be classified against that field and against `pending_tool_results` rather than against retained
conversation content.

The following SHALL hold for every record of a non-terminal loop `L`:

- `published_request_id = R` implies, while the record exists, that an `AgentRequest{RequestID: R, LoopID: L}` is
  durably retained on `agent.request.L`.
- Every `pending_tool_results[e]` whose `request_id` is `R` names an execution ID derived from a tool call of the
  retained response for `R` (membership only; no rendered content is compared).
- `iterations` changes only in an update whose `published_request_id` also changes.
- `pending_approval`, when present, names `request_id = published_request_id`.

The non-terminal record SHALL be written with a compare-and-swap update against the revision observed when the
delivery was admitted. On the model-response and tool-result lanes that update SHALL follow the PubAck of every
output the new record implies; at loop birth the record SHALL be written before the first request is published. The
approval lane and the approval-timeout sweeper keep their present write-then-publish order until #1362. A redelivered
input whose `request_id` is older than `published_request_id` SHALL be acknowledged without effect; one whose
`request_id` is newer SHALL be retried until the record names it; one whose `request_id` is not a request of the loop
SHALL be quarantined. A process with no memory of the loop SHALL, before classifying any redelivered input other than
a task, read the newest retained request for the loop and, when it is newer than `published_request_id`, adopt it
into the record by identity first. Recovery SHALL never compare rendered messages or result content to decide whether
an input was applied.

An approval deadline is process-local and is not a durable fact: a replaced process SHALL re-arm no approval deadline,
and a loop in `awaiting_approval` SHALL stay in `awaiting_approval` until the approval is answered or the loop is
cancelled.

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

#### Scenario: A replaced process re-arms no approval deadline

- **GIVEN** a loop whose record is `awaiting_approval` and whose process was replaced
- **WHEN** the replacement starts and holds no in-memory approval deadline for that loop
- **THEN** no approval deadline is re-armed and no record is written, and the loop stays `awaiting_approval` until the
  approval is answered or the loop is cancelled

#### Scenario: A stale tool result is acknowledged without effect

- **GIVEN** a running loop whose record names `R(N+1)`
- **WHEN** a tool result carrying `request_id = R(N)` is redelivered
- **THEN** it is acknowledged, no message is published, the record is not written, and the inapplicable-result metric
  and audit log line are emitted

#### Scenario: A response that outruns the record update retries

- **GIVEN** a loop whose record names `R` while `R(N+1)` has been published and its record update has not landed
- **WHEN** the model response for `R(N+1)` is delivered
- **THEN** the delivery is retried as not yet observable, and no effect is applied

#### Scenario: A terminal loop receives a result it cannot prove it applied

- **GIVEN** a loop whose record is terminal
- **WHEN** a tool result for that loop is redelivered and is not present in `pending_tool_results`
- **THEN** it is acknowledged without effect, the inapplicable-result metric increments, and an audit log line names
  the loop, execution, and terminal state

#### Scenario: A task redelivered at iteration zero republishes the first request

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1` and an empty `pending_tool_results`
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** `R1` is rebuilt from the task, published with `Nats-Msg-Id = R1`, the in-process conversation is rebuilt,
  and the task is acknowledged

## MODIFIED Requirements

### Requirement: In-flight state MUST NOT be derived from the acknowledgement floor
The in-flight answer SHALL be sourced from the consumer's outstanding-work bookkeeping
(`NumPending + NumAckPending`) and SHALL NOT be computed from `AckFloor`.

`AckFloor` was measured against both deployed NATS versions and found to misreport in **both**
directions: it does not advance past a `MaxDeliver`-exhausted message, so it sits behind that message
while the consumer is idle; and on the next unrelated ack it leaps *past* the never-applied message.
It therefore never means "everything at or below this is durably handled". The rejection and its
measurement are recorded in ADR-088. This requirement exists so the disproven approach cannot be
reintroduced as an optimization.

A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler
transitions a loop out of `state=running`, so a crashed process leaves a stale `running` entry
indistinguishable from live work. `published_request_id` and `pending_tool_results` are settlement facts
for classifying a redelivered input; they SHALL NOT be read as an in-flight answer.

#### Scenario: A poison-exhausted message does not freeze the in-flight answer

- **GIVEN** a task message that has exhausted `MaxDeliver` and was never applied
- **WHEN** a caller asks whether work is outstanding
- **THEN** the answer reflects genuine outstanding work, not a floor stalled behind that message

#### Scenario: A crashed process does not read as work in flight

- **GIVEN** a loop record left at `state=running` by a process that crashed mid-task
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, not from the stale record

#### Scenario: A record naming a published request is not an in-flight answer

- **GIVEN** a loop record with `published_request_id = R` and a non-empty `pending_tool_results`, left by a process
  that crashed after publishing `R`
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, and neither `published_request_id` nor
  `pending_tool_results` is read to produce it
