# Proposal — durable-task-claims (gh#807)

## Why

A redelivered `agent.task` message can bill a second LLM call for the same logical task. The
only dedupe that exists today is a process-local, in-memory scan
(`HasActiveLoopForTask`, `processor/agentic-loop/state.go:166-178`) that dies on restart, does
not exist on a second replica, and excludes terminal loops — so a task redelivered after its
loop completes, after a crash, or to a different instance creates a second loop, a second
initial request, and a second provider charge. Nothing durable records the task→loop mapping
anywhere: `AGENT_LOOPS` is loop-ID-keyed, and `TaskID` lives only inside the serialized value.

The stream layer does not close this either: none of the three in-repo task publishers
(dispatch bus `processor/agentic-dispatch/component.go:733`, dispatch HTTP `http.go:346`, rule
engine `processor/rule/actions.go:1728`) stamps `Nats-Msg-Id`, and no stream in the repo
declares a `duplicates` window, so the server's 2-minute default is the entire dedupe horizon —
shorter than any restart/recovery replay. Even where SemMachina stamps `Nats-Msg-Id` client-side
(gh#807's opening premise), AGENT runs `MaxBytes` + `DiscardOld`: eviction plus a NATS restart,
or an operator purge, removes both the task and the duplicate evidence, and a redelivered
upstream trigger is then accepted and billed again.

gh#807 asks for durable idempotency keyed by TaskID independent of retained message bytes.
SemMachina's `mystery-companion-acceptance` task 8.5 states the full need: literal at-most-one
provider call across a process crash requires a durable **atomic TaskID → LoopID → initial
RequestID claim** and **idempotent initial request publication**.

## What Changes

- **A durable task-claim record**, written with atomic KV `Create` (the "exactly one winner"
  primitive graph-ingest already uses at `mutations.go:589`; conflict surfaces as the existing
  `natsclient.ErrKVKeyExists` sentinel) in a new claims bucket keyed by `TaskID`. The claim
  binds the whole identity chain at claim time: `{LoopID, initial RequestID, task-bytes hash,
  claimed-at}`. Claims are write-once.
- **Claim-before-side-effect in the loop's task path**: `handleTaskMessage` claims before
  creating a loop or publishing the initial `agent.request`. A claim conflict is not an error —
  the loser reads the winning claim and either short-circuits (loop already progressing /
  terminal) or **resumes idempotently** using the claim's LoopID and RequestID, so a crash
  between claim and publish is recovered by redelivery without minting new identity.
- **Idempotent initial request publication**: the initial `agent.request` publish is stamped
  with `Nats-Msg-Id` derived from the claimed initial RequestID, so the recovery republication
  and the original collapse server-side within the duplicates window, and identity (not bytes
  retention) collapses them beyond it.
- **Same-TaskID/different-bytes is a typed rejection**: the claim carries a hash of the task
  bytes; a claim hit with a different hash is refused with a stable classified error (gh#807
  explicitly asks for this case to be defined).
- **Task publishers stamp `Nats-Msg-Id = TaskID`** (all three mint sites) and the AGENT stream
  declaration gains an explicit `duplicates` window — defense-in-depth at the stream layer,
  correctness no longer depends on it.
- **`HasActiveLoopForTask`'s role narrows**: the durable claim becomes the authority for "has
  this task been accepted"; the in-memory scan remains only as a fast path, never the deciding
  one.
- **Provider idempotency hand-off (scoped)**: the claimed initial RequestID is the durable
  identity a provider idempotency key can be derived from. The wire clients already accept
  `ExtraHeaders` (`model/wire/client.go:38-40`) but only as a per-endpoint static set; carrying
  a per-request key needs a request-path signature change and is staged as its own task group,
  deliverable separately.

## Capabilities

### New Capabilities

- `agentic-task-claims`: the durable TaskID claim contract — atomicity, identity binding
  (TaskID → LoopID → initial RequestID), survival semantics (message eviction, stream restart,
  operator purge of AGENT), retention/cleanup, same-ID/different-bytes behavior, concurrent
  claimer behavior, and the recovery/resume protocol on redelivery.

### Modified Capabilities

- `agentic-loop`: task acceptance changes from in-memory dedupe to claim-gated acceptance; the
  initial request publication becomes idempotent by identity. (Spec exists;
  requirement-level change.)

## Impact

- `processor/agentic-loop` — task handling path (`component.go:983-1088`,
  `handlers.go:792-800`), loop/request ID minting (`state.go`), new claim store wiring.
- `processor/agentic-dispatch` + `processor/rule` — `Nats-Msg-Id` stamping on task publish
  (three sites; `PublishToStreamWithMsgID` already exists, `natsclient/client.go:930`).
- `configs/agentic.json`, `configs/research-graph-e2e.json` — AGENT stream `duplicates` window;
  new claims bucket declaration.
- `model/wire` (staged, separable) — per-request idempotency-key plumbing.
- **Billing-adjacent behavior change**: duplicate task deliveries that today create a second
  loop will instead resume or no-op. Consumers reading `HandlerResult.Created` semantics are
  unaffected (the flag already distinguishes creation from dedupe).
- Consumers: **SemMachina** (mystery-companion acceptance task 8.5 is the blocked downstream;
  its deterministic per-generation TaskID framing is the intended producer contract), and any
  sister publishing persona tasks (SemTeams/SemSpec-shaped consumers).

## Non-goals

- **Universal at-most-once billing.** The claim closes the identified crash/eviction windows;
  a provider-side charge for a request whose response was lost is out of scope until the
  provider idempotency hand-off lands (staged task group; gh#807 records this bound).
- **Deterministic TaskID minting for in-repo producers.** The rule engine mints
  `rule-<entity>-<UnixNano>` (`actions.go:1492`) — a re-fired rule action produces a *new*
  TaskID by construction, which a TaskID-keyed claim cannot and should not collapse.
  Deterministic minting is the producer's contract (SemMachina already honors it); changing
  rule-engine minting is its own decision, recorded here as out of scope.
- **A general claims/fencing framework.** This is a TaskID-scoped KV claim, deliberately
  shaped like the existing `kv.Create` precedents (graph-ingest strict create, graphresearch
  loop create) — not a lease system, not the graph-entity CAS surface (that is gh#689/gh#851's
  territory).
- **Reworking recovery generations.** SemMachina's generation framing (new generation = new
  TaskID = intentional new execution) stays producer-side; the framework sees distinct TaskIDs.
