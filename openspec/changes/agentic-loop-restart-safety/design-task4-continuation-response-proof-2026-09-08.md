# Design: task 4 causal settlement without a recovery state machine

## Review gate and scope

This design proceeds from the independently accepted
`inventory-task4-continuation-response-proof-2026-09-08.md`, SHA-256
`96c7530cb7102b6df3333b24ed5003c9c010d17f618f582116e8f6cc9ad31df4`; its verifier passed 345/345 pins.

Task 4 owns only durable settlement of TaskMessage and AgentResponse inputs, the loop accepting-boundary poison checks
required to prevent permanent redelivery, and ordering of task-birth observations. It adds no replay cache, stream
scan, history, supervisor, KV bucket, outbox, ledger, deterministic ID, second state owner, or generic state-machine
runtime.

Task 5 retains tool-effect idempotency, completed outcomes, and ordered batch reconstruction. Task 6 retains
approval-response reconstruction. Task 7 retains docs/E2E completion. Tasks 9.5–9.7 retain rule-producer admission and
publication. #1244 retains the complete loop transition/refusal set and any future design for a reachable durable
attachable state; task 4 removes the current unsafe different-TaskID attach path.

## Options

### A. Do nothing

Keep process-only `pendingTaskResults`, uncorrelated loop-generated AgentRequest bytes, Retry for busy continuations,
permissive loop RequestID parsing, swallowed approval-gate errors, and pre-durable birth observations.

Cost: replacement can lose a continuation prompt or apply a response twice; permanent poison can exhaust delivery; a
refused task has no durable consequence; metrics overcount continuations. This does not meet task 4.

### B. Reorder all task outputs before LoopEntity persistence

A retained request could precede the state marker and make task recovery simpler.

Cost: agentic-model can answer immediately while the loop is not yet readable. With MaxDeliver 2, the fast response
can exhaust before authority appears. This ordering was explicitly rejected by the inventory evidence.

### C. Add durable progress state

Add `LastAppliedRequestID`, task history, a replay ledger, an outbox, or a separate recovery bucket.

Cost: another state owner and reconciliation protocol, plus publish-before-marker ambiguity. It duplicates facts
already present in exact retained outputs and current loop authority. This violates the approved streams-first
direction.

### D. Remove continuation now (recommended after review)

Treat a task whose LoopID already exists and whose TaskID differs from the loop's current TaskID as a refused attempt
to continue that loop. A task with the same LoopID and same TaskID remains ordinary redelivery/recovery and uses exact
retained evidence. This is the only presently safe distinction that requires no new lifecycle state.

Cost: the advertised continuation feature is removed in this greenfield release. A future continuation capability
must first define a reachable, durable idle/attachable state and its transition contract under #1244.

### E. Preserve continuation by adding a new durable attachable state

Add an explicit idle/attachable lifecycle state, then accept a different TaskID only from that state.

Cost: this is new state and transition behavior, outside task 4 and not yet evidenced. It belongs in #1244 if product
need justifies it.

## Recommendation requiring owner choice

Choose D now. Independent same-TaskID delivery from different-TaskID reuse makes redelivery and continuation
indistinguishable unless another durable fact is added. The review found no safe reachable continuation boundary in
the present loop lifecycle: an active loop is awaiting model, tools, or approval, and a settled loop is terminal.
Accepting a new task in any of those states either overwrites exact-latest request evidence or requires a queue/state
machine already rejected.

This is the one binding owner choice in this correction: **remove continuation now, or explicitly authorize new
durable attachable state design under #1244.** The remainder of task 4 does not depend on preserving continuation.

## Applied decision skills

- `orchestration-check`: this remains component execution. Agentic-loop validates, publishes, and settles one
  delivery; no rule chain, workflow layer, or lifecycle-owned execution state is added.
- `kv-or-stream`: a refusal is the durable outcome of queued TaskMessage work and must survive until its source
  settles, so it belongs on the existing AGENT JetStream stream. KV would invent current-state authority and has no
  processing ACK.
- `new-payload`: the refusal must be a registered agentic payload with alias-based JSON methods,
  `agentic.RegisterPayloads`, control indexing profile, production-decoder round trip, explicit built-in registration
  coverage, and schema-generation verification if the port schema changes.
- `entity-or-bucket` is not triggered: the design adds no durable state store. The refusal is a work outcome, not a
  graph fact or private current-state bucket.

## Target contract and invariants

### Task recovery

`AgentRequest` gains two optional loop-protocol fields:

- `TaskID string json:"task_id,omitempty"`
- `PredecessorRequestID string json:"predecessor_request_id,omitempty"`

Every request constructed by agentic-loop carries the current `LoopEntity.TaskID`. A request produced by applying
model response R1 carries R1's exact RequestID as `PredecessorRequestID`. A task-born request carries no predecessor.
Generic AgentRequest validation does not require either field, and direct agentic-model clients may omit both.

Keep nonterminal LoopEntity Put before model request publication. Remove `pendingTaskResults`; it is not authority.
Task identity has two cases only:

1. Same LoopID and same TaskID is redelivery. Read current LoopEntity, exact retained request, created event, and
   refusal outcome; matching evidence is reused, missing ordinary output is rebuilt/published, transient reads Retry,
   and nonempty conflicts Quarantine.
2. Same LoopID and different TaskID is not attached. If current authority is terminal, publish refusal code
   `loop_terminal`; otherwise publish `continuation_unsupported`. Await refusal PubAck, then ACK the TaskMessage. Do
   not mutate LoopEntity.TaskID, context, request routing, trajectory, graph birth, or metrics.

A latest request with the delivered TaskID proves request publication for that task when role/model/LoopID also
match. Fresh birth also requires a matching created event. Confirmed request absence is a fresh-birth partial boundary
only when current LoopEntity maps the delivered TaskID. `MaxAckPending=1` means another task cannot arrive before the
current task settles. Exact retained request/refusal conflicts Quarantine; unresolved reads Retry; missing ordinary
publications repeat and receive PubAck before task ACK.

### Response application proof

For delivered response R1, ACK as already applied only when one of these exact proofs exists:

- final terminal LoopEntity marker plus retained request R1;
- latest retained successor request whose `PredecessorRequestID` is R1 and whose TaskID/LoopID/role/model agree;
- durable AwaitingApproval plus exact PendingApproval correlation to R1 and one retained R1 call;
- in the same process, one atomic read-only LoopManager check proving every normalized R1 tool call appears exactly
  once across stored results, the active pending route, and queued suffix, with matching execution ID, call ID, name,
  arguments, and ordinal, and with no R1 extras or duplicates.

The atomic check reads existing maps under one lock; it creates no state and is never restart authority. If no proof
exists after replacement, replay R1's ordinary publications with the same execution identities and wait for PubAck.
Task 5 owns whether executor effects can be reused and ordered batches reconstructed. MaxAckPending=1 means an
ACK-confirmed older response cannot redeliver after a later response, so immediate predecessor correlation is
sufficient; no request history is added.

### Durable task refusal

Add registered `agentic.TaskRefusedEvent` (`agentic.task_refused.v1`) carrying:

- raw `LoopID` — canonical loop token;
- raw `TaskID` — nonempty source task identity;
- `Code` — closed values `continuation_unsupported` or `loop_terminal`;
- `RefusedAt` — nonzero timestamp.

Publish on `agent.task_refused.<task-key>`, where `task-key` is the lowercase hexadecimal SHA-256 digest of the raw
TaskID bytes, computed only inside agentic-loop. The digest is a NATS-safe address, not identity and not collision
authority. Exact recovery validates the payload's raw TaskID and LoopID; a digest hit with different raw identity is a
correlation conflict and Quarantines. Adopters subscribe to the declared `agent.task_refused.*` wildcard and read the
registered payload; they never compute or reproduce `task-key`, and no exported hashing helper is added.

Before refusing a redelivered task, exact-read the internally derived subject. Matching raw TaskID, LoopID, and code
proves refusal committed; absent evidence permits repeat publication; malformed retained bytes or conflicting raw
identity/code Quarantine. Publication failure Retries; PubAck precedes source ACK. No queue or new state exists.

### Permanent malformed versus conflict

One private canonical loop-request parser replaces both permissive request-ID interpreters. It validates
`<canonical-loop-uuid>:req:<v4-uuid>` before warm-map lookup or durable read. The generic AgentResponse DTO remains
compatible with direct clients.

At the loop accepting boundary, malformed/empty structured RequestID, StatusToolCall with an empty nested ToolCall.ID,
and ToolResult with empty Name are permanent structural poison: Terminate before lookup, mutation, trajectory,
publication, or metric. A valid nonempty identity that conflicts with warm mapping, LoopEntity, retained request,
predecessor, or execution correlation quarantines. Transient/unresolved authority reads retry.

### Approval-gate failure

Every `gateForApproval` failure propagates through HandleToolResult to the delivery disposition;
`checkApprovalGate` cannot turn it into `true,nil`.

- `BeginAwaitingApproval` rejection or race: discard speculative process state and exact-reread LoopEntity.
  Unreadable/transient authority Retries. Authority still in the eligible pre-transition state Retries from reread
  state. Nonempty contradictory/impossible state Quarantines. Awaiting/terminal state is not ACK proof by itself;
  task 5/6 lane-specific proof remains required.
- `UpdateLoop` failure after process mutation: discard the mutated process copy and exact-reread LoopEntity. Apply the
  same classification as `BeginAwaitingApproval`; never continue with or persist the speculative copy.
- ApprovalPending marshal failure: no reread can make framework-generated invalid bytes valid. Quarantine, stop the
  exact owner, and do not persist, publish, or ACK.
- ApprovalPending subject-resolution failure: resolved component configuration is internally inconsistent.
  Quarantine, stop the exact owner, and do not persist, publish, or ACK.
- LoopEntity persistence failure: current authority did not commit the transition. Retry after discarding speculative
  process state.
- ApprovalPending publication without PubAck: required ordinary output is not proven. Retry; publication may repeat.

An authoritative reread resolves lifecycle races without another state owner. ACK is permitted only when the lane's
already-approved exact applied proof is independently present, never because the process mutation or log exists. Task
6 still owns approval-result reconstruction.

### Birth observations

`Created` means actual fresh loop birth, never “this task has outputs.” Graph birth and created event occur once for
the birth task. Only after LoopEntity persistence and all required birth PubAcks succeed may the loop emit its created
log, increment `loops_created`/`active_loops`, or record the business loop-start observation. Refused attempts emit
none of them. Terminal business observations occur after the final terminal marker. Metrics remain process observations;
no exactly-once metric state is added. Tests prove one fresh birth plus N refused continuations plus one terminal
returns the active gauge to zero.

## Adopter seam

A direct AgentRequest client must know nothing new and does nothing; omitted TaskID/predecessor remains valid. A
component that transparently forwards loop-protocol AgentRequest must preserve unknown optional fields. TaskMessage
publishers continue supplying raw stable LoopID/TaskID. A publisher receives a typed durable refusal through the
existing AGENT wildcard or its framework adapter and branches on payload code; it does not predict loop state,
compute the subject hash, or retry automatically. Migration notes state that different-TaskID same-LoopID
continuation is removed; callers start a new loop with a new LoopID, while same-TaskID retransmission remains
recovery. Correctness failures surface as registered typed runtime outcomes, not logs or documentation alone.

## TDD slices and acceptance

1. **Request provenance RED/GREEN**
   - Production-decoder round trip preserves optional TaskID and predecessor; omission remains valid.
   - Every loop constructor stamps current TaskID; only response-produced successors stamp exact immediate predecessor.
   - Direct client fixtures with product-local RequestID and no loop fields remain green.
2. **Task replacement matrix**
   - Crash after LoopEntity Put/before any output: same task rebuilds fresh request and created event.
   - Crash after request PubAck/before created-event PubAck: reuse request, publish only missing created event.
   - Same LoopID/same TaskID is recovered as redelivery; different TaskID is never attached or rebound.
   - Matching current TaskID reuses retained request; role/model/LoopID or created-event conflict Quarantines.
   - Keep the real-NATS MaxAckPending=1 test proving task N blocks N+1 until server-confirmed ACK.
3. **Refusal outcome**
   - Registry/schema/alias JSON round trip for `TaskRefusedEvent`.
   - Nonterminal different-TaskID publishes `continuation_unsupported`; terminal publishes `loop_terminal`; PubAck
     precedes ACK.
   - Dotted, wildcard-like, whitespace-containing, Unicode, and very long valid TaskIDs produce one safe fixed-length
     subject token and preserve raw identity in the payload.
   - PubAck failure Retries; matching retained refusal ACKs without mutation; a forced digest hit with different raw
     TaskID or LoopID Quarantines.
   - Tests assert no exported digest helper, adopter-side digest computation, queued continuation, or hidden retry.
4. **Response replacement matrix**
   - Terminal marker plus R1 request, and successor carrying predecessor R1, each ACK a redelivery without duplicate
     work.
   - Atomic live partition accepts exact result/active/queue partitions and rejects omissions, extras, duplicates, and
     metadata conflicts.
   - Cold absence repeats stable execution publications; no process snapshot is treated as durable proof.
   - AwaitingApproval plus exact pending correlation proves R1 applied.
5. **Boundary classification and approval error paths**
   - Warm and cold malformed RequestID terminate before lookup/mutation; parseable mapping conflict quarantines.
   - Empty ToolCall.ID and loop-consumed ToolResult.Name terminate before side effects.
   - Every approval failure table row proves the exact disposition, zero false-success ACK, speculative-state discard,
     and authoritative reread on lifecycle race.
6. **Birth balance**
   - Fresh birth + multiple refused continuation attempts + terminal yields one created count, one active increment,
     one decrement, and zero residual gauge.
   - No business birth signal precedes required PubAcks; spawn-failure/terminal signals follow their final marker.
7. **Integration gate**
   - Race tests for affected packages, strict OpenSpec, schema generation, contract tests, and serialized
     `task e2e:agentic` before a BREAKING landing.

## Conformance to recorded rulings

- Settlement first; streams first: every ACK names a committed lane consequence; ordinary missing output is replayed
  through AGENT.
- If exact evidence exists, use it; otherwise make the call/publication again: exact request, refusal, terminal,
  successor, approval, or live-partition proof is reused; absence cold-replays stable work.
- Durable continuation refusal then ACK; no queue: registered digest-addressed refusal retains raw identity and
  receives PubAck before source ACK; no deferred turn ownership exists.
- No supervisor/KV state machine: no bucket, ledger, history, scan, outbox, or second owner is added.
- Greenfield break/fix with migration notes: loop-only wire additions are optional for direct clients; forwarders and
  TaskMessage publishers receive explicit migration notes.
- #1244 owns transition discipline: task 4 removes the unsafe continuation path; any future durable attachable state
  and its transitions require #1244 design and owner approval.

## Explicit non-goals and fences

- No exactly-once claim, replay cache, deterministic request ID, durable applied-response marker, or generic correlation
  framework.
- No pause/resume restoration, durable continuation queue, implicit retry scheduler, or workflow engine.
- No change to generic AgentResponse validation or direct provider-client RequestID grammar.
- No tool-effect/result authority or ordered-batch reconstruction (task 5).
- No approval-response continuation reconstruction (task 6).
- No rule publisher admission/payload work (tasks 9.5–9.7).
- No complete transition-set ruling, new idle state, or future continuation design (#1244).
- No binding OpenSpec/task/docs delta until independent DESIGN PASS and explicit owner acceptance of this complete
  design.
