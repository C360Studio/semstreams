# agentic-loop Delta

## ADDED Requirements

### Requirement: A logical model request has one deterministic identity

Agentic-loop SHALL mint every `agent.request` RequestID as `<loopID>:req:<iteration>:<retry>`, where `iteration` is
the 1-based ordinal of the request within its loop and `retry` is the within-iteration truncation-retry ordinal.
Minting the same logical request twice SHALL yield the same RequestID. The `<loopID>:req:` prefix SHALL be
preserved so loop recovery from a RequestID and the `agent.response.<requestID>` subject are unchanged.

Every `agent.request` publication SHALL stamp its RequestID as `Nats-Msg-Id`. Server-side duplicate suppression
inside the stream's configured window is a bounded convenience and SHALL NOT be treated as the mechanism that
prevents repeated provider work; that mechanism is the retained-response rule in `agentic-model`.

Because the ordinals move only when the loop advances, agentic-loop SHALL hold at most ONE outstanding
`agent.request` per loop ACROSS DELIVERIES IT PROCESSES IN ORDER. A continuation admitted while a request is
outstanding SHALL be neither refused nor published: its turn is added to the loop's context and recorded on the
loop entity as pending, and the outstanding response SHALL carry it into the next iteration's request rather than
settling the loop.

A loop completes on terminal model text OR on a tool result that terminates it, and BOTH are completions for this
rule: a terminal tool answered while a turn is pending SHALL carry the turn rather than settle, and the carried
request SHALL contain that tool's own result, because a request holding an assistant tool call with no answering
tool message is a broken pair. Only a loop with nothing deferred completes on either path.

The qualifier is the truth about this layer, not a softening. The admission check and the mint are separate
critical sections, and `agent.task` and `agent.response` are separate JetStream consumers, so a continuation
delivered between a response clearing the mark and the carrying request re-taking it is admitted against an empty
mark and both paths mint the same identity. Closing that window needs either per-loop serialization across the
two consumers or a durable check-and-set on the mark; neither is this layer's, and until one exists the guarantee
SHALL NOT be stated unqualified.

A response naming a request other than the loop's CURRENT one — the newest request minted for it — SHALL change
nothing and settle as handled, counted under a reason label. The identity check is required because carrying a turn
leaves the loop non-terminal: the terminal guard that absorbs a redelivered completion does not apply, so without
it a redelivery would settle a loop whose carrying request is still in flight.

Current, not merely outstanding. A tool-call response settles its request while the loop stays on that iteration
waiting for executors or a human approval, so for that whole window the loop is waiting on no model response at
all; a check keyed on that emptiness admits an EARLIER request's redelivered completion, which then settles the
loop with the previous task's answer while the request carrying the user's newer turn is still being worked. A
redelivery of the current request SHALL still be handled, because it is the answer the loop is owed. A response
arriving for a loop this process has minted no request for SHALL also be handled: that is the process-replacement
case, where the routing was rebuilt from the RequestID rather than from a mint, and deciding it needs durable
request identity.

The record of a pending continuation SHALL name the request that carries it, and SHALL NOT be cleared before that
request's response arrives. The loop entity is persisted before its publications are emitted, so a marker cleared
when the carrying request is BUILT is durably clear about a publication whose durability is unknown; naming the
carrier keeps the turn recoverable while still preventing a second carry.

A turn that cannot be carried at all — the loop is at its iteration ceiling when the completion arrives — SHALL
leave the loop completing and SHALL be retained on the record with no carrier named, rather than cleared. A turn
no request ever contained is otherwise recoverable only from a log line. The retained record SHALL NOT make the
settled loop continuable: a task naming a terminal loop is refused as it always was.

#### Scenario: The same logical request is minted twice

- **WHEN** a task redelivers and agentic-loop mints the request for a loop whose iteration and retry ordinals have
  not moved
- **THEN** the RequestID is byte-identical to the one minted the first time
- **AND** a request minted at a different iteration is a different RequestID

#### Scenario: A truncation retry names its own attempt

- **WHEN** a length-truncated response at iteration N triggers the compaction retry
- **THEN** the retry's RequestID is `<loopID>:req:N:1`
- **AND** forward progress clears the retry ordinal back to 0 for the next iteration

#### Scenario: A consumer reads the loop token from a RequestID

- **WHEN** any consumer recovers the loop token from a RequestID, by the `:req:` separator or by the first colon
- **THEN** it reads the same loop token the framework minted
- **AND** the resolved `agent.response.<requestID>` subject is a single valid NATS token

#### Scenario: A continuation is admitted while a request is outstanding

- **WHEN** a task naming a live loop is admitted before that loop's outstanding `agent.request` has been answered
- **THEN** no `agent.request` is published for it, because a request minted now would carry the outstanding
  request's RequestID and `Nats-Msg-Id` with different bytes
- **AND** the continuation is not refused: its turn is added to the loop's context and the loop entity records a
  pending continuation
- **AND** the task delivery is acknowledged, because the loop entity write is the effect it owns

#### Scenario: A completion response meets a deferred continuation

- **WHEN** the outstanding response would complete the loop and a continuation is pending
- **THEN** the loop does NOT complete: no completion record is built and no `agent.complete` is published
- **AND** the loop advances one iteration and publishes `<loopID>:req:N+1:0` carrying the deferred turn
- **AND** the loop entity records that request as the carrier, on this path and on the tool-results path alike, and
  the pending record is cleared when that request's response arrives

#### Scenario: A terminal tool answers a loop that has a deferred continuation

- **WHEN** a tool result that terminates the loop arrives while a continuation is pending
- **THEN** the loop does NOT complete: no completion record is built and no `agent.complete` is published
- **AND** the loop advances one iteration and publishes `<loopID>:req:N+1:0` carrying both the deferred turn and
  the terminal tool's own result
- **AND** a terminal tool answering a loop with nothing deferred completes it exactly as before

#### Scenario: A turn is deferred and its carrying request cannot be confirmed

- **WHEN** the publication carrying a deferred turn fails with unknown durability and the delivery is quarantined
- **THEN** the persisted loop entity still records the turn as pending and names the request that was to carry it
- **AND** no second request is minted for the same turn while that record names a carrier

#### Scenario: A turn is deferred behind a completion the loop has no iteration left to answer

- **WHEN** a completion arrives for a loop at its iteration ceiling with a continuation turn pending
- **THEN** the loop completes and warns that the turn was not carried
- **AND** the persisted loop entity still records the turn as pending, with no carrier named
- **AND** a task naming that settled loop is refused rather than continuing it

#### Scenario: A response arrives for a request the loop has moved on from

- **WHEN** a model response names a request other than the loop's current one, including while the loop waits on
  tools or an approval and is therefore waiting on no model response at all
- **THEN** the loop is not advanced, not completed, and publishes nothing
- **AND** the delivery is acknowledged and counted under a drop reason, because redelivering it cannot help
- **AND** a redelivery of the CURRENT request is still handled, because it is the answer the loop is owed
- **AND** a response for a loop this process has minted no request for is still handled, because refusing it would
  strand a live loop whose process was replaced

#### Scenario: A duplicate request publish meets a configured window

- **WHEN** the same RequestID is published twice to a stream that declares a `Duplicates` window, inside that window
- **THEN** the server rejects the second publish and one message is stored on the subject
- **AND** a publication carrying no `Nats-Msg-Id` still repeats, because at-least-once is unchanged

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

A human approval authorises one EXECUTION, not one provider CallID. The approval-pending event SHALL carry the gated
call's execution identity, and an approval response SHALL echo it. A loop SHALL resolve a pending approval only on a
response whose execution identity matches the pending one, and SHALL refuse a response that carries none against a
pending approval that has one — there SHALL be no fallback to provider CallID. Every responder SHALL echo it,
including the approval-timeout sweeper's synthetic rejection.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

#### Scenario: An earlier approval is replayed against a later call with the same provider CallID

- **WHEN** a loop gates a second call whose provider CallID repeats an earlier, already-approved call's, and the
  earlier approval response is delivered again
- **THEN** the loop refuses it as stale and dispatches nothing
- **AND** the loop stays awaiting approval with the later call still pending, so a real decision on it can arrive

#### Scenario: An approval response carries no execution identity

- **WHEN** a response naming the pending call's provider CallID arrives with no execution identity, against a
  pending approval that has one
- **THEN** the loop refuses it rather than matching on provider CallID
- **AND** a pending approval minted before execution identity existed still resolves on provider CallID, because it
  carries none to match

### Requirement: Loop task, request, and tool work use only required correlation

For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL
validate their mapping and SHALL reject a conflicting mapping. Provider work SHALL carry a stable RequestID. Tool
work SHALL carry the framework execution identity derived from RequestID, provider CallID, and positive call ordinal.

Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.
Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT
be treated as permanent identity or proof of publication. Exact retained reads SHALL exist only at named boundaries
where they prevent repeating non-repeatable work or prove a lane-specific durable transition already applied.

#### Scenario: Task mapping is stable across redelivery

- **WHEN** a task redelivers after its LoopEntity or initial request committed
- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping
- **AND** any required ordinary publication may repeat and receives PubAck before source ACK

#### Scenario: One task identity names two loops

- **WHEN** a task whose TaskID already names a running loop arrives carrying a DIFFERENT LoopID
- **THEN** agentic-loop quarantines the source delivery rather than answering with the loop already running, whose
  conversation the message does not name
- **AND** the comparison reads the token the PRODUCER sent, so the same task carrying no loop token is still
  deduplicated to the running loop — including a lineage task for which intake reserved a fresh prospective
  identity on this delivery, which is a redelivery and not a second loop

#### Scenario: Request or execution correlation conflicts

- **WHEN** one RequestID or framework execution identity names conflicting required correlation
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either mapping

#### Scenario: Ordinary required publication repeats

- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat
- **THEN** the duplicate is an admitted at-least-once outcome
- **AND** consumers use the lane's required correlation and durable transition rules
